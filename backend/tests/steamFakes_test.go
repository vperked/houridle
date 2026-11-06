package tests

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"houridle/backend/online/steamauth"
	"houridle/backend/online/steamcm"
)

// Two accounts, in Steam's SteamID64 form.
const (
	alice uint64 = 76561197960265729 + 1000
	bob   uint64 = 76561197960265729 + 2000
)

// fakeToken builds a JWT-shaped Steam token. Only its claims matter: the
// real signature is checked by Steam, which the fakes stand in for.
func fakeToken(steamID uint64, aud string, exp int64) string {
	enc := base64.RawURLEncoding.EncodeToString
	claims := fmt.Sprintf(`{"sub":"%d","aud":["%s"],"exp":%d}`, steamID, aud, exp)
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(claims)) + "." + enc([]byte("sig"))
}

const farFuture = 4102444800 // 2100

// ---------- fake Web API ----------

type apiCall struct {
	Method string
	Form   string // raw input_protobuf_encoded, base64
	Req    *steamauth.Decoded
}

type apiReply struct {
	EResult int
	Status  int
	Body    []byte
}

// fakeAPI answers IAuthenticationService calls with whatever handle returns.
type fakeAPI struct {
	*httptest.Server
	mu     sync.Mutex
	calls  []apiCall
	handle func(c apiCall) apiReply
}

func newFakeAPI(t *testing.T, handle func(c apiCall) apiReply) *fakeAPI {
	t.Helper()
	f := &fakeAPI{handle: handle}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) != 3 || parts[0] != "IAuthenticationService" {
			http.NotFound(w, r)
			return
		}
		form := r.FormValue("input_protobuf_encoded")
		raw, _ := base64.StdEncoding.DecodeString(form)
		req, err := steamauth.Decode(raw)
		if err != nil {
			http.Error(w, "bad protobuf", 400)
			return
		}
		call := apiCall{Method: parts[1], Form: form, Req: req}
		f.mu.Lock()
		f.calls = append(f.calls, call)
		f.mu.Unlock()
		reply := f.handle(call)
		if reply.EResult == 0 {
			reply.EResult = 1
		}
		w.Header().Set("X-Eresult", fmt.Sprint(reply.EResult))
		if reply.Status != 0 {
			w.WriteHeader(reply.Status)
		}
		w.Write(reply.Body)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeAPI) client() *steamauth.Client {
	return &steamauth.Client{HTTP: f.Server.Client(), BaseURL: f.URL}
}

func (f *fakeAPI) called(method string) []apiCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []apiCall
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// ---------- fake connection server ----------

// frame builds one protobuf message the way Steam sends it.
func frame(emsg uint32, steamID uint64, sessionID int32, body []byte) []byte {
	hdr := new(steamauth.Encoder)
	if steamID != 0 {
		hdr.Fixed64(1, steamID)
	}
	if sessionID != 0 {
		hdr.Varint(2, uint64(sessionID))
	}
	h := hdr.Out()
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf, emsg|0x80000000)
	binary.LittleEndian.PutUint32(buf[4:], uint32(len(h)))
	return append(append(buf, h...), body...)
}

// multi packs messages into a compressed batch, as Steam does.
func multi(frames ...[]byte) []byte {
	var inner bytes.Buffer
	for _, f := range frames {
		var n [4]byte
		binary.LittleEndian.PutUint32(n[:], uint32(len(f)))
		inner.Write(n[:])
		inner.Write(f)
	}
	var zipped bytes.Buffer
	zw := gzip.NewWriter(&zipped)
	zw.Write(inner.Bytes())
	zw.Close()
	body := new(steamauth.Encoder).Varint(1, uint64(inner.Len())).Bytes(2, zipped.Bytes()).Out()
	return frame(1, 0, 0, body)
}

// received is a message the fake server got from houridle.
type received struct {
	EMsg   uint32
	Header *steamauth.Decoded
	Body   *steamauth.Decoded
}

// fakeCM is a stand-in for Steam's connection servers.
type fakeCM struct {
	*httptest.Server
	t testing.TB

	// logonResult is the eresult to answer a sign-in with (1 = accepted).
	logonResult int
	// answerAs is the SteamID put in the reply's header (the account Steam
	// claims to have signed in).
	answerAs  uint64
	sessionID int32

	mu             sync.Mutex
	got            []received
	conn           *websocket.Conn
	gotGames       chan []uint64
	calls          []string // service methods asked for
	owned          []fakeGame
	callErr        int    // eresult to answer service calls with (0 = fine)
	renewTo        string // refresh token to hand out when asked to renew
	refuseFirstSet bool
	refuseFirst    int // refuse this many sign-ins (with logonResult) before accepting

	logonResults    []int
	hangUpFirst     int
	afterLogon      [][]byte
	closeAfterLogon bool
	stallAfterLogon bool
	quit            chan struct{}

	// Achievements (user stats) for any game asked about.
	schema      []byte            // binary KeyValues schema; nil = no achievements
	statValues  map[uint32]uint32 // stat ID -> value
	statsResult int               // eresult for stats requests (0 = OK)
	revert      []uint32          // stat IDs Steam refuses to store

	licenses     []uint32 // package IDs the account owns (sent after sign-in)
	bought       map[uint32]fakePurchase
	stickerReady bool // a sale-event sticker can be claimed

	family   []fakeGame // games the account's Steam family shares with it (nil: no family)
	cart     []uint32   // package IDs in the store cart, by line (line ID = index+1)
	nextLine uint64
}

// licenseList is a CMsgClientLicenseList body for the account's packages.
func (f *fakeCM) licenseList() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := new(steamauth.Encoder).Varint(1, 1)
	for _, id := range f.licenses {
		l := new(steamauth.Encoder).Varint(1, uint64(id))
		if p, ok := f.bought[id]; ok {
			l.Fixed32(2, uint32(p.at.Unix())).Varint(6, uint64(p.payment))
		}
		b.Message(2, l)
	}
	return b.Out()
}

type fakePurchase struct {
	at      time.Time
	payment uint32
}

// grant adds a package and tells the session, as Steam does after a claim.
func (f *fakeCM) grant(id uint32) {
	f.mu.Lock()
	f.licenses = append(f.licenses, id)
	f.mu.Unlock()
	f.push(780, f.licenseList())
}

// newFakeCM starts the server and points houridle at it for this test.
func newFakeCM(t testing.TB, answerAs uint64, logonResult int) *fakeCM {
	t.Helper()
	f := &fakeCM{t: t, logonResult: logonResult, answerAs: answerAs, sessionID: 4242, gotGames: make(chan []uint64, 256), quit: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/cmsocket/", f.serve)
	mux.HandleFunc("/dir", func(w http.ResponseWriter, r *http.Request) {
		host := strings.TrimPrefix(f.URL, "http://")
		json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"serverlist": []map[string]string{
			{"endpoint": host, "type": "websockets"},
			{"endpoint": host, "type": "websockets"},
			{"endpoint": host, "type": "websockets"},
			{"endpoint": host, "type": "websockets"},
			{"endpoint": "ignored.example:1", "type": "netfilter"},
		}}})
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	t.Cleanup(func() { close(f.quit) })

	oldDir, oldURL := steamcm.DirectoryURL, steamcm.ServerURL
	steamcm.DirectoryURL = f.URL + "/dir"
	steamcm.ServerURL = func(endpoint string) string { return "ws://" + endpoint + "/cmsocket/" }
	t.Cleanup(func() { steamcm.DirectoryURL, steamcm.ServerURL = oldDir, oldURL })
	return f
}

func (f *fakeCM) serve(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	f.mu.Lock()
	f.conn = ws
	f.mu.Unlock()
	ctx := context.Background()
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		emsg := binary.LittleEndian.Uint32(data) &^ 0x80000000
		hl := int(binary.LittleEndian.Uint32(data[4:]))
		hdr, _ := steamauth.Decode(data[8 : 8+hl])
		body, _ := steamauth.Decode(data[8+hl:])
		f.mu.Lock()
		f.got = append(f.got, received{EMsg: emsg, Header: hdr, Body: body})
		f.mu.Unlock()

		switch emsg {
		case 5514: // ClientLogon
			result := f.logonResult
			f.mu.Lock()
			if f.hangUpFirst > 0 {
				f.hangUpFirst--
				f.mu.Unlock()
				ws.CloseNow()
				return
			}
			if len(f.logonResults) > 0 {
				result, f.logonResults = f.logonResults[0], f.logonResults[1:]
			} else if f.refuseFirst > 0 {
				f.refuseFirst--
				if result == 1 {
					result = 15
				}
			} else if f.refuseFirstSet {
				result = 1
			}
			extra, closeAfter, stall := f.afterLogon, f.closeAfterLogon, f.stallAfterLogon
			f.mu.Unlock()
			resp := new(steamauth.Encoder).Varint(1, uint64(result)).Varint(3, 1).Out()
			reply := frame(751, f.answerAs, f.sessionID, resp)
			// Batched and compressed, like the real thing.
			ws.Write(ctx, websocket.MessageBinary, multi(append([][]byte{reply}, extra...)...))
			if closeAfter {
				ws.CloseNow()
				return
			}
			if result == 1 {
				ws.Write(ctx, websocket.MessageBinary, frame(780, alice, f.sessionID, f.licenseList()))
			}
			if stall {
				<-f.quit
				ws.CloseNow()
				return
			}
		case 5572: // ClientRequestFreeLicense: grants every app not owned yet
			resp := new(steamauth.Encoder).Varint(1, 1)
			f.mu.Lock()
			for _, id := range body.Uints(2) {
				if !slices.Contains(f.licenses, uint32(id)+1_000_000) {
					f.licenses = append(f.licenses, uint32(id)+1_000_000)
					resp.Varint(3, id)
				}
			}
			f.mu.Unlock()
			ws.Write(ctx, websocket.MessageBinary, replyTo(5573, f.sessionID, hdr, resp.Out()))
		case 818: // ClientGetUserStats
			ws.Write(ctx, websocket.MessageBinary, f.answerStats(hdr, body))
		case 5466: // ClientStoreUserStats2
			f.mu.Lock()
			for _, st := range body.Messages(6) {
				if f.statValues == nil {
					f.statValues = map[uint32]uint32{}
				}
				if !slices.Contains(f.revert, uint32(st.Uint(1))) {
					f.statValues[uint32(st.Uint(1))] = uint32(st.Uint(2))
				}
			}
			resp := new(steamauth.Encoder).Fixed64(1, body.Uint(1)).Varint(2, 1).Varint(3, 99)
			for _, id := range f.revert {
				resp.Message(4, new(steamauth.Encoder).Varint(1, uint64(id)).Varint(2, uint64(f.statValues[id])))
			}
			f.mu.Unlock()
			ws.Write(ctx, websocket.MessageBinary, replyTo(821, f.sessionID, hdr, resp.Out()))
		case 151: // a service method call
			f.mu.Lock()
			f.calls = append(f.calls, hdr.Str(12))
			f.mu.Unlock()
			ws.Write(ctx, websocket.MessageBinary, f.answerCall(hdr, body))
		case 742: // ClientGamesPlayed
			var ids []uint64
			for _, g := range body.Messages(1) {
				ids = append(ids, g.Uint(2))
			}
			select { // never let a test that ignores these stall the server
			case f.gotGames <- ids:
			default:
			}
		}
	}
}

func (f *fakeCM) messages(emsg uint32) []received {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []received
	for _, m := range f.got {
		if m.EMsg == emsg {
			out = append(out, m)
		}
	}
	return out
}

// before reports whether the first a message arrived before the first b.
func (f *fakeCM) before(a, b uint32) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.got {
		switch m.EMsg {
		case a:
			return true
		case b:
			return false
		}
	}
	return false
}

func (f *fakeCM) push(emsg uint32, body []byte) {
	f.mu.Lock()
	ws := f.conn
	f.mu.Unlock()
	ws.Write(context.Background(), websocket.MessageBinary, frame(emsg, alice, f.sessionID, body))
}

// answerStats is Steam's reply to ClientGetUserStats.
func (f *fakeCM) answerStats(hdr, body *steamauth.Decoded) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := uint64(1)
	if f.statsResult != 0 {
		result = uint64(f.statsResult)
	}
	resp := new(steamauth.Encoder).Fixed64(1, body.Uint(1)).Varint(2, result).Varint(3, 1234)
	if f.schema != nil {
		resp.Bytes(4, f.schema)
	}
	for id, v := range f.statValues {
		resp.Message(5, new(steamauth.Encoder).Varint(1, uint64(id)).Varint(2, uint64(v)))
	}
	return replyTo(819, f.sessionID, hdr, resp.Out())
}

// replyTo frames a reply that names the request's job.
func replyTo(emsg uint32, sessionID int32, req *steamauth.Decoded, body []byte) []byte {
	h := new(steamauth.Encoder).Fixed64(1, alice).Varint(2, uint64(sessionID)).Fixed64(11, req.Uint(10)).Out()
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf, emsg|0x80000000)
	binary.LittleEndian.PutUint32(buf[4:], uint32(len(h)))
	return append(append(buf, h...), body...)
}

type fakeGame struct {
	ID      int
	Name    string
	Minutes int
}

// answerCall builds Steam's reply to a service call: the header carries the
// job it answers and the result, the body the data.
func (f *fakeCM) answerCall(req, body *steamauth.Decoded) []byte {
	hdr := new(steamauth.Encoder).Fixed64(1, alice).Varint(2, uint64(f.sessionID)).Fixed64(11, req.Uint(10))
	result := uint64(1)
	if f.callErr != 0 {
		result = uint64(f.callErr)
	}
	hdr.Varint(13, result)

	resp := new(steamauth.Encoder)
	if req.Str(12) == "Player.GetOwnedGames#1" && body.Uint(1) == alice && body.Bool(2) {
		resp.Varint(1, uint64(len(f.owned)))
		for _, g := range f.owned {
			resp.Message(2, new(steamauth.Encoder).Varint(1, uint64(g.ID)).String(2, g.Name).Varint(4, uint64(g.Minutes)))
		}
	}
	if req.Str(12) == "Authentication.GenerateAccessTokenForApp#1" && body.Uint(2) == alice && body.Uint(3) == 1 && f.renewTo != "" {
		resp.String(1, "access").String(2, f.renewTo)
	}
	switch req.Str(12) {
	case "Store.GetDiscoveryQueue#1":
		if body.Str(2) == "" { // Steam refuses a queue without a country
			result = 2
			hdr = new(steamauth.Encoder).Fixed64(1, alice).Varint(2, uint64(f.sessionID)).Fixed64(11, req.Uint(10)).Varint(13, 2)
		} else {
			resp.Varint(1, 10).Varint(1, 20).Varint(1, 30).String(2, body.Str(2))
		}
	case "Store.SkipDiscoveryQueueItem#1":
		f.mu.Lock()
		f.calls = append(f.calls, fmt.Sprintf("skip %d", body.Uint(2)))
		f.mu.Unlock()
	case "StoreBrowse.GetItems#1":
		names := map[uint64]string{4242: "Free Game Never Played", 4343: "Some DLC"}
		for _, id := range body.Messages(1) {
			item := new(steamauth.Encoder).Varint(1, 0).Varint(2, id.Uint(1)).String(6, names[id.Uint(1)])
			if id.Uint(1) == 4343 {
				item.Varint(10, 4) // DLC
			}
			resp.Message(1, item)
		}
	case "FamilyGroups.GetFamilyGroupForUser#1":
		f.mu.Lock()
		if f.family == nil {
			resp.Bool(2, true) // not in a family
		} else {
			resp.Varint(1, 4242)
		}
		f.mu.Unlock()
	case "FamilyGroups.GetSharedLibraryApps#1":
		f.mu.Lock()
		if body.Uint(1) == 4242 && !body.Bool(2) {
			for _, g := range f.family {
				resp.Message(1, new(steamauth.Encoder).Varint(1, uint64(g.ID)).Fixed64(2, bob).String(6, g.Name))
			}
			// One the owner keeps to themself: not shared.
			resp.Message(1, new(steamauth.Encoder).Varint(1, 77).Fixed64(2, bob).String(6, "Excluded").Varint(10, 3))
		}
		f.mu.Unlock()
	case "AccountCart.GetCart#1":
		f.mu.Lock()
		contents := new(steamauth.Encoder)
		for i, pkg := range f.cart {
			if pkg != 0 {
				contents.Message(1, new(steamauth.Encoder).Varint(1, uint64(i+1)).Varint(3, uint64(pkg)))
			}
		}
		f.mu.Unlock()
		resp.Message(1, contents)
	case "AccountCart.AddItemsToCart#1":
		f.mu.Lock()
		for _, it := range body.Messages(2) {
			f.cart = append(f.cart, uint32(it.Uint(1)))
			resp.Varint(1, uint64(len(f.cart)))
		}
		f.mu.Unlock()
	case "AccountCart.RemoveItemFromCart#1":
		f.mu.Lock()
		if n := int(body.Uint(1)); n >= 1 && n <= len(f.cart) {
			f.cart[n-1] = 0
		}
		f.mu.Unlock()
	case "SaleItemRewards.CanClaimItem#1":
		f.mu.Lock()
		resp.Bool(1, f.stickerReady)
		f.mu.Unlock()
	}
	if req.Str(12) == "Authentication.GenerateAccessTokenForApp#1" && body.Uint(2) == alice && !body.Has(3) {
		resp.String(1, "web-access-token") // no renewal asked: just an access token
	}
	h := hdr.Out()
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf, 147|0x80000000)
	binary.LittleEndian.PutUint32(buf[4:], uint32(len(h)))
	return append(append(buf, h...), resp.Out()...)
}
