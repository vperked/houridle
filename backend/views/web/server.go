// Package web serves houridle's status page: which games are idling, the
// hours gained, card drops left and recent events, with a stop button, so
// you can check on a server from your phone.
//
// Every request needs the page's key. Opening the link with ?key=... once
// stores it in a cookie and drops it from the address bar. The page is
// plain HTTP: use it on your own network or over a VPN, not the open
// internet.
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"houridle/backend/features/gamestore"
	"houridle/backend/features/history"
	"houridle/backend/run/idler"
)

// site holds the page, built from frontend/ into page/dist (make page). The
// build isn't committed; without it houridle still compiles and runs, and
// the page says it wasn't built.
//
//go:embed all:page
var site embed.FS

const (
	cookieName  = "houridle_key"
	stopHeader  = "X-Houridle" // a header a page on another site can't send, so it can't press Stop
	readTimeout = 10 * time.Second
)

// Status is what the page shows; the Server asks for it on every refresh.
type Status struct {
	Version string
	Account string
	Started time.Time
	StopAt  time.Time // zero for none
	Views   []idler.View
	Events  []idler.Event
	Stats   *history.Summary // nil if there's no history
	Run     Run              // how this run idles
	Panel   *Panel           // houridle serve's state; nil elsewhere
}

// Run is how a run idles, for the page's summary.
type Run struct {
	SignedIn bool          // idling over a saved sign-in (houridle start), not the Steam client
	Cards    bool          // -cards
	All      bool          // -all
	Claim    bool          // -claim
	Target   time.Duration // -until; 0 for none
	Rotate   time.Duration // turns, with more than 32 games
	Status   string        // how friends see you (signed in only)
	Craft    bool          // -craft
	Events   bool          // -events
}

// Server is the status page for one houridle.
type Server struct {
	Key     string        // required on every request (see NewKey)
	Status  func() Status // the current state
	Stop    func()        // stops idling (the Stop button)
	Control Controller    // houridle serve: everything else the page can do; nil for a status page only

	mu   sync.Mutex
	key  string // the current key (Key, until the page asks for a new one)
	addr string // where Serve listens
	art  gamestore.ArtCache
}

func (s *Server) currentKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.key == "" {
		s.key = s.Key
	}
	return s.key
}

func (s *Server) listenAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

func (s *Server) setKey(key string) {
	s.mu.Lock()
	s.key = key
	s.mu.Unlock()
}

func (s *Server) keyCookie(key string, r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name: cookieName, Value: key, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, MaxAge: 365 * 24 * 3600, Secure: r.TLS != nil,
	}
}

// NewKey makes a random key for the page.
func NewKey() string {
	b := make([]byte, 24)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Handler is the page, its assets, and its API (see controlRoutes).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.Handle("GET /assets/", assets())
	mux.HandleFunc("GET /manifest.webmanifest", s.authed(s.manifest))
	mux.HandleFunc("GET /api/status", s.authed(s.status))
	mux.HandleFunc("GET /api/art", s.authed(jsonHandler(s.gameArt)))
	mux.HandleFunc("POST /api/stop", s.authed(s.changing(s.stop)))
	if s.Control != nil {
		s.controlRoutes(mux)
	}
	return securityHeaders(mux)
}

// Serve runs the page on addr until ctx ends. ":8080" listens on every
// network interface.
func (s *Server) Serve(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.addr = addr
	s.mu.Unlock()
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: readTimeout, ReadTimeout: readTimeout, WriteTimeout: readTimeout}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// index serves the page. ?key=... sets the cookie and redirects to a clean
// address, so the key doesn't stay in the address bar or browser history.
func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if key := r.URL.Query().Get("key"); key != "" {
		if !s.keyOK(key) {
			http.Error(w, "That link's key is wrong or old. Run houridle web on the server for the current link.", http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, s.keyCookie(key, r))
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !s.cookieOK(r) {
		http.Error(w, "This page needs its key. Run houridle web on the server and open the link it shows.", http.StatusUnauthorized)
		return
	}
	page, err := site.ReadFile("page/dist/index.html")
	if err != nil {
		http.Error(w, "This houridle was built without its page. Build it with make or build.bat, which build the page first (they need Node.js).", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(page)
}

// manifest describes the page as an app, so a phone can add it to its home
// screen. Its start address carries the key (the home-screen app may not
// share the browser's cookies), so it's only given to a browser that
// already has the key.
func (s *Server) manifest(w http.ResponseWriter, r *http.Request) {
	icon := func(src, size, purpose string) map[string]string {
		return map[string]string{"src": src, "sizes": size, "type": "image/png", "purpose": purpose}
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{
		"name":             "houridle",
		"short_name":       "houridle",
		"description":      "Steam idling, on your phone",
		"start_url":        "/?key=" + url.QueryEscape(s.currentKey()),
		"scope":            "/",
		"display":          "standalone",
		"background_color": "#000000",
		"theme_color":      "#000000",
		"icons": []map[string]string{
			icon("/assets/app-icon-192.png", "192x192", "any"),
			icon("/assets/app-icon-512.png", "512x512", "any"),
			icon("/assets/app-icon-512.png", "512x512", "maskable"),
		},
	})
}

// assets serves the page's script and stylesheet. They hold no data (that
// comes from /api/status, which needs the key), so they need no key, and
// their names change with their contents, so browsers may keep them.
func assets() http.Handler {
	sub, err := fs.Sub(site, "page/dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServerFS(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		files.ServeHTTP(w, r)
	})
}

func (s *Server) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.cookieOK(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) cookieOK(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	return err == nil && s.keyOK(c.Value)
}

func (s *Server) keyOK(key string) bool {
	want := s.currentKey()
	return want != "" && subtle.ConstantTimeCompare([]byte(key), []byte(want)) == 1
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusAccepted)
	go s.Stop() // answer first: stopping ends this server too
}

// statusJSON is the page's data. Durations are in seconds.
type statusJSON struct {
	Version string      `json:"version"`
	Account string      `json:"account"`
	Started time.Time   `json:"started"`
	StopAt  *time.Time  `json:"stopAt,omitempty"`
	Games   []gameJSON  `json:"games"`
	Events  []eventJSON `json:"events"`
	Stats   *statsJSON  `json:"stats,omitempty"`
	Run     runJSON     `json:"run"`
	Panel   *Panel      `json:"panel,omitempty"` // present in houridle serve: the page can control it
}

type runJSON struct {
	SignedIn bool   `json:"signedIn"`
	Cards    bool   `json:"cards,omitempty"`
	All      bool   `json:"all,omitempty"`
	Claim    bool   `json:"claim,omitempty"`
	Target   int64  `json:"target,omitempty"`
	Rotate   int64  `json:"rotate,omitempty"`
	Status   string `json:"status,omitempty"`
	Craft    bool   `json:"craft,omitempty"`
	Events   bool   `json:"events,omitempty"`
}

type gameJSON struct {
	AppID  uint32 `json:"appId"`
	Name   string `json:"name"`
	State  string `json:"state"`
	Note   string `json:"note,omitempty"`
	Idled  int64  `json:"idled"`
	Total  int64  `json:"total,omitempty"`
	Target int64  `json:"target,omitempty"`
	Drops  int    `json:"drops,omitempty"`
}

type eventJSON struct {
	At    time.Time       `json:"at"`
	Level string          `json:"level"`
	Text  string          `json:"text"`
	Kind  idler.EventKind `json:"kind,omitempty"`
}

type statsJSON struct {
	Today int64           `json:"today"`
	Week  int64           `json:"week"`
	Month int64           `json:"month"`
	All   int64           `json:"all"`
	Since string          `json:"since,omitempty"` // first day on record, YYYY-MM-DD
	Days  []dayJSON       `json:"days"`            // oldest first
	Games []gameTotalJSON `json:"games"`           // all time, most first
}

type dayJSON struct {
	Day  string `json:"day"` // YYYY-MM-DD
	Time int64  `json:"time"`
}

type gameTotalJSON struct {
	AppID uint32 `json:"appId"`
	Name  string `json:"name"`
	Week  int64  `json:"week"`
	All   int64  `json:"all"`
}

// maxStatsGames is how many games the History tab lists.
const maxStatsGames = 20

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	st := s.Status()
	out := statusJSON{Version: st.Version, Account: st.Account, Started: st.Started, Games: []gameJSON{}, Events: []eventJSON{}}
	if !st.StopAt.IsZero() {
		out.StopAt = &st.StopAt
	}
	for _, v := range st.Views {
		out.Games = append(out.Games, gameJSON{
			AppID: v.Game.AppID, Name: v.Game.Name, State: stateName(v.State), Note: v.Note,
			Idled: secs(v.Idled), Total: secs(v.Total), Target: secs(v.Target), Drops: max(v.Drops, 0),
		})
	}
	for _, e := range st.Events {
		out.Events = append(out.Events, eventJSON{At: e.At, Level: levelName(e.Level), Text: e.Text, Kind: e.Kind})
	}
	if st.Stats != nil {
		out.Stats = statsOut(st.Stats)
	}
	out.Panel = st.Panel
	out.Run = runJSON{SignedIn: st.Run.SignedIn, Cards: st.Run.Cards, All: st.Run.All, Claim: st.Run.Claim,
		Target: secs(st.Run.Target), Rotate: secs(st.Run.Rotate), Status: st.Run.Status, Craft: st.Run.Craft, Events: st.Run.Events}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(out)
}

func statsOut(sum *history.Summary) *statsJSON {
	out := &statsJSON{Today: secs(sum.Today), Week: secs(sum.Week), Month: secs(sum.Month), All: secs(sum.All),
		Days: []dayJSON{}, Games: []gameTotalJSON{}}
	if !sum.Since.IsZero() {
		out.Since = sum.Since.Format("2006-01-02")
	}
	for _, d := range sum.Days {
		out.Days = append(out.Days, dayJSON{Day: d.Day.Format("2006-01-02"), Time: secs(d.Time)})
	}
	week := map[uint32]time.Duration{}
	for _, g := range sum.WeekGames {
		week[g.AppID] = g.Time
	}
	for i, g := range sum.Games {
		if i == maxStatsGames {
			break
		}
		out.Games = append(out.Games, gameTotalJSON{AppID: g.AppID, Name: g.Name, Week: secs(week[g.AppID]), All: secs(g.Time)})
	}
	return out
}

func secs(d time.Duration) int64 { return int64(d / time.Second) }

func stateName(s idler.State) string {
	switch s {
	case idler.StateIdling:
		return "idling"
	case idler.StateStarting:
		return "starting"
	case idler.StateWaiting:
		return "waiting"
	case idler.StateFailing:
		return "failing"
	case idler.StateQueued:
		return "queued"
	case idler.StateDone:
		return "done"
	case idler.StateHeld:
		return "refundable"
	default:
		return "stopped"
	}
}

func levelName(l idler.EventLevel) string {
	switch l {
	case idler.EventGood:
		return "good"
	case idler.EventWarn:
		return "warn"
	case idler.EventBad:
		return "bad"
	default:
		return "info"
	}
}

// securityHeaders keeps the page from being framed or sniffed, and its
// scripts to its own.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// Images may also come from Steam's image servers (game artwork).
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: https://*.steamstatic.com; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// Links are the addresses to open the page at, key included: the local
// network addresses when addr listens on every interface.
func Links(addr, key string) []string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}
	var hosts []string
	if host != "" && host != "0.0.0.0" && host != "::" {
		hosts = []string{host}
	} else {
		ifaces, _ := net.InterfaceAddrs()
		for _, a := range ifaces {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.IsLoopback() || ipn.IP.IsLinkLocalUnicast() || ipn.IP.To4() == nil {
				continue
			}
			hosts = append(hosts, ipn.IP.String())
		}
		if len(hosts) == 0 {
			hosts = []string{"localhost"}
		}
	}
	var out []string
	for _, h := range hosts {
		out = append(out, "http://"+net.JoinHostPort(h, port)+"/?key="+key)
	}
	return out
}

// ParseAddr checks a listen address: ":8080", "8080", "127.0.0.1:8080".
func ParseAddr(s string) (string, error) {
	s = strings.TrimSpace(s)
	if _, err := strconv.Atoi(s); err == nil {
		s = ":" + s
	}
	_, port, err := net.SplitHostPort(s)
	if n, perr := strconv.Atoi(port); err != nil || perr != nil || n < 1 || n > 65535 {
		return "", errors.New(`the status page address should look like ":8080" (every network) or "127.0.0.1:8080" (this computer only)`)
	}
	return s, nil
}

// gameArt is where games' store pictures are (?apps=730,440), looked up
// once a day: newer games keep them at addresses with a version in them.
func (s *Server) gameArt(w http.ResponseWriter, r *http.Request) (any, error) {
	var ids []uint32
	for _, part := range strings.Split(r.URL.Query().Get("apps"), ",") {
		if id, err := strconv.ParseUint(strings.TrimSpace(part), 10, 32); err == nil && id > 0 {
			ids = append(ids, uint32(id))
		}
	}
	if len(ids) == 0 || len(ids) > maxArt {
		return nil, badRequest(fmt.Sprintf("ask for 1 to %d games", maxArt))
	}
	art := s.art.Get(r.Context(), ids)
	out := make(map[string]gamestore.Art, len(art))
	for id, a := range art {
		out[strconv.FormatUint(uint64(id), 10)] = a
	}
	return out, nil
}

const maxArt = 300
