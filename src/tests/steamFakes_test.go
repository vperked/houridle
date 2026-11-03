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
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"

	"houridle/src/steamauth"
	"houridle/src/steamcm"
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

	mu       sync.Mutex
	got      []received
	conn     *websocket.Conn
	gotGames chan []uint64
}

// newFakeCM starts the server and points houridle at it for this test.
func newFakeCM(t testing.TB, answerAs uint64, logonResult int) *fakeCM {
	t.Helper()
	f := &fakeCM{t: t, logonResult: logonResult, answerAs: answerAs, sessionID: 4242, gotGames: make(chan []uint64, 256)}
	mux := http.NewServeMux()
	mux.HandleFunc("/cmsocket/", f.serve)
	mux.HandleFunc("/dir", func(w http.ResponseWriter, r *http.Request) {
		host := strings.TrimPrefix(f.URL, "http://")
		json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"serverlist": []map[string]string{
			{"endpoint": host, "type": "websockets"},
			{"endpoint": "ignored.example:1", "type": "netfilter"},
		}}})
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)

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
			resp := new(steamauth.Encoder).Varint(1, uint64(f.logonResult)).Varint(3, 1).Out()
			reply := frame(751, f.answerAs, f.sessionID, resp)
			// Batched and compressed, like the real thing.
			ws.Write(ctx, websocket.MessageBinary, multi(reply))
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

func (f *fakeCM) push(emsg uint32, body []byte) {
	f.mu.Lock()
	ws := f.conn
	f.mu.Unlock()
	ws.Write(context.Background(), websocket.MessageBinary, frame(emsg, alice, f.sessionID, body))
}
