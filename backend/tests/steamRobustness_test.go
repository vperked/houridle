package tests

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"houridle/backend/client/steam"
	"houridle/backend/online/steamauth"
	"houridle/backend/online/steamcm"
	"houridle/backend/run/idler"
)

// ---------- fuzzing: nothing Steam (or anyone) sends may panic ----------

func FuzzDecode(f *testing.F) {
	f.Add([]byte{})
	f.Add(new(steamauth.Encoder).Varint(1, 5).String(2, "hi").Fixed64(3, 9).Out())
	f.Add([]byte{0x0a, 0xff, 0xff, 0xff, 0xff, 0x0f})
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := steamauth.Decode(data)
		if err != nil {
			return
		}
		for n := 1; n < 40; n++ { // every accessor must be safe on any field
			d.Uint(5)
			d.Int(5)
			d.Str(5)
			d.Raw(5)
			d.Float(5)
			d.Bool(5)
			d.Messages(5)
			_ = n
		}
	})
}

func FuzzParseToken(f *testing.F) {
	f.Add(fakeToken(alice, "client", farFuture))
	f.Add("a.b.c")
	f.Add("..")
	f.Add("e30.e30.e30")
	f.Fuzz(func(t *testing.T, s string) {
		if info, err := steamauth.ParseToken(s); err == nil && !steamauth.ValidSteamID(info.SteamID) {
			t.Fatalf("accepted a token with an invalid SteamID: %q", s)
		}
	})
}

// hostileCM answers a sign-in with whatever frames the test supplies.
func hostileCM(t *testing.T, reply func() [][]byte) {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/dir", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"response":{"serverlist":[{"endpoint":"` + strings.TrimPrefix(srv.URL, "http://") + `","type":"websockets"}]}}`))
	})
	mux.HandleFunc("/cmsocket/", func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		ws.SetReadLimit(1 << 20)
		for {
			_, data, err := ws.Read(r.Context())
			if err != nil {
				return
			}
			if binary.LittleEndian.Uint32(data)&^0x80000000 == 5514 {
				for _, f := range reply() {
					ws.Write(r.Context(), websocket.MessageBinary, f)
				}
			}
		}
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	oldDir, oldURL := steamcm.DirectoryURL, steamcm.ServerURL
	steamcm.DirectoryURL = srv.URL + "/dir"
	steamcm.ServerURL = func(e string) string { return "ws://" + e + "/cmsocket/" }
	t.Cleanup(func() { steamcm.DirectoryURL, steamcm.ServerURL = oldDir, oldURL })
}

func TestHostileServerCannotCrashOrHangTheClient(t *testing.T) {
	zeros := func(n int) []byte {
		var b bytes.Buffer
		zw := gzip.NewWriter(&b)
		zw.Write(make([]byte, n))
		zw.Close()
		return b.Bytes()
	}
	nested := frame(1, 0, 0, new(steamauth.Encoder).Varint(1, 0).Bytes(2, []byte{}).Out())
	for i := 0; i < 6; i++ {
		var n [4]byte
		binary.LittleEndian.PutUint32(n[:], uint32(len(nested)))
		nested = frame(1, 0, 0, new(steamauth.Encoder).Varint(1, 0).Bytes(2, append(n[:], nested...)).Out())
	}
	cases := map[string][][]byte{
		"too short":        {{1, 2, 3}},
		"header too long":  {append([]byte{0xe0, 0x14, 0, 0x80, 0xff, 0xff, 0xff, 0x7f}, 1, 2)},
		"garbage header":   {append([]byte{0xe0, 0x14, 0, 0x80, 4, 0, 0, 0}, 0xff, 0xff, 0xff, 0xff)},
		"claims huge":      {frame(1, 0, 0, new(steamauth.Encoder).Varint(1, 1<<40).Bytes(2, []byte("x")).Out())},
		"not gzip":         {frame(1, 0, 0, new(steamauth.Encoder).Varint(1, 10).Bytes(2, []byte("not gzip")).Out())},
		"gzip bomb":        {frame(1, 0, 0, new(steamauth.Encoder).Varint(1, 1<<20).Bytes(2, zeros(64<<20)).Out())},
		"truncated batch":  {multi(frame(751, alice, 1, nil))[:30]},
		"deep nesting":     {nested},
		"empty logon body": {frame(751, alice, 1, nil)}, // no eresult: Steam's default is failure
	}
	for name, frames := range cases {
		t.Run(name, func(t *testing.T) {
			hostileCM(t, func() [][]byte { return frames })
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			start := time.Now()
			s, err := steamcm.Connect(ctx, steamcm.Config{SteamID: alice, RefreshToken: fakeToken(alice, "client", farFuture)})
			if err == nil {
				s.Close()
				t.Fatal("garbage must not count as a successful sign-in")
			}
			if ctx.Err() != nil {
				t.Fatalf("hung until the deadline: %v", err)
			}
			if time.Since(start) > 10*time.Second {
				t.Errorf("took %v", time.Since(start))
			}
		})
	}
}

// ---------- no leaks ----------

func settledGoroutines() int {
	for i := 0; i < 20; i++ {
		runtime.GC()
		time.Sleep(25 * time.Millisecond)
	}
	return runtime.NumGoroutine()
}

func TestConnectionsDontLeakGoroutines(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	before := settledGoroutines()
	for i := 0; i < 40; i++ {
		s, err := connectAlice(t, cm)
		if err != nil {
			t.Fatal(err)
		}
		s.SetGames([]uint32{730})
		s.Close()
	}
	if after := settledGoroutines(); after > before+3 {
		t.Errorf("goroutines grew from %d to %d over 40 connections", before, after)
	}
}

func TestSupervisorRestartsDontLeakGoroutines(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	api := newFakeAPI(t, func(c apiCall) apiReply {
		return apiReply{Body: new(steamauth.Encoder).String(1, "a").Out()}
	})
	mgr := managerWith(t, api, aliceSession())
	cycle := func() {
		sup := idler.NewSession([]*steam.Game{{AppID: 730, Name: "CS"}}, mgr, func(idler.Event) {})
		ctx, cancel := context.WithCancel(context.Background())
		sup.Start(ctx)
		<-cm.gotGames
		cancel()
		sup.Wait()
	}
	cycle() // warm up: HTTP keep-alive connections are pooled, not leaked
	before := settledGoroutines()
	for i := 0; i < 15; i++ {
		cycle()
	}
	if after := settledGoroutines(); after > before+3 {
		t.Errorf("goroutines grew from %d to %d over 15 start/stop cycles", before, after)
	}
}

// 32 games is Steam's cap; the sign-in must carry all of them in one go.
func TestThirtyTwoGamesInOneMessage(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ids := make([]uint32, 32)
	for i := range ids {
		ids[i] = uint32(1000 + i)
	}
	s.SetGames(ids)
	if got := <-cm.gotGames; len(got) != 32 || got[31] != 1031 {
		t.Errorf("got %d games", len(got))
	}
}

// A lost connection is retried, and the games come back by themselves.
func TestSupervisorReconnectsAfterADrop(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	api := newFakeAPI(t, func(c apiCall) apiReply {
		return apiReply{Body: new(steamauth.Encoder).String(1, "a").Out()}
	})
	mgr := managerWith(t, api, aliceSession())
	events := make(chan idler.Event, 16)
	sup := idler.NewSession([]*steam.Game{{AppID: 730, Name: "CS"}}, mgr, func(e idler.Event) { events <- e })
	ctx, cancel := context.WithCancel(context.Background())
	sup.Start(ctx)
	defer func() { cancel(); sup.Wait() }()

	<-cm.gotGames
	waitFor(t, "idling", func() bool { return sup.Views()[0].State == idler.StateIdling })

	cm.mu.Lock()
	cm.conn.CloseNow() // the network drops
	cm.mu.Unlock()
	waitFor(t, "the drop to show", func() bool { return sup.Views()[0].State != idler.StateIdling })

	select {
	case g := <-cm.gotGames: // reconnect uses the 5s base backoff
		if len(g) != 1 || g[0] != 730 {
			t.Errorf("after reconnect Steam was told %v", g)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("never reconnected")
	}
	waitFor(t, "idling again", func() bool { return sup.Views()[0].State == idler.StateIdling })
}

// ---------- performance ----------

func BenchmarkDecodeLogonResponse(b *testing.B) {
	msg := new(steamauth.Encoder).Varint(1, 1).Varint(3, 9).Fixed64(20, alice).String(21, "US").Out()
	b.ReportAllocs()
	for b.Loop() {
		d, _ := steamauth.Decode(msg)
		d.Int(1)
	}
}

func BenchmarkEncodeGamesPlayed32(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		e := new(steamauth.Encoder)
		for i := 0; i < 32; i++ {
			e.Message(1, new(steamauth.Encoder).Fixed64(2, uint64(730+i)))
		}
		_ = e.Varint(2, 16).Out()
	}
}

func BenchmarkParseToken(b *testing.B) {
	tok := fakeToken(alice, "client", farFuture)
	b.ReportAllocs()
	for b.Loop() {
		steamauth.ParseToken(tok)
	}
}

func BenchmarkStoreLoad(b *testing.B) {
	store := &steamauth.Store{Path: b.TempDir() + "/s.dat"}
	store.Save(aliceSession())
	b.ReportAllocs()
	for b.Loop() {
		store.Load()
	}
}

func BenchmarkStoreSave(b *testing.B) {
	store := &steamauth.Store{Path: b.TempDir() + "/s.dat"}
	s := aliceSession()
	b.ReportAllocs()
	for b.Loop() {
		store.Save(s)
	}
}

func BenchmarkSupervisorViews32(b *testing.B) {
	games := make([]*steam.Game, 32)
	for i := range games {
		games[i] = &steam.Game{AppID: uint32(i + 1), Name: "g"}
	}
	sup := idler.NewSession(games, &steamauth.Manager{}, func(idler.Event) {})
	b.ReportAllocs()
	for b.Loop() {
		sup.Views()
	}
}

// BenchmarkConnectAndPlay is a whole sign-in plus games message against the
// local fake, so it measures houridle's own overhead, not Steam's.
func BenchmarkConnectAndPlay(b *testing.B) {
	cm := newFakeCM(b, alice, 1)
	b.ReportAllocs()
	for b.Loop() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		s, err := steamcm.Connect(ctx, steamcm.Config{SteamID: alice, RefreshToken: fakeToken(alice, "client", farFuture)})
		if err != nil {
			b.Fatal(err)
		}
		s.SetGames([]uint32{730})
		<-cm.gotGames
		s.Close()
		cancel()
	}
}

// While idling the only work is a heartbeat every few seconds, so a session
// must cost next to nothing: no busy loops, no growing memory.
func TestIdlingIsCheap(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	api := newFakeAPI(t, func(c apiCall) apiReply {
		return apiReply{Body: new(steamauth.Encoder).String(1, "a").Out()}
	})
	mgr := managerWith(t, api, aliceSession())
	games := make([]*steam.Game, 32)
	for i := range games {
		games[i] = &steam.Game{AppID: uint32(730 + i), Name: "g"}
	}
	sup := idler.NewSession(games, mgr, func(idler.Event) {})
	ctx, cancel := context.WithCancel(context.Background())
	sup.Start(ctx)
	defer func() { cancel(); sup.Wait() }()
	<-cm.gotGames
	waitFor(t, "idling", func() bool { return sup.Views()[0].State == idler.StateIdling })

	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	time.Sleep(3 * time.Second)
	runtime.GC()
	runtime.ReadMemStats(&b)

	beats := len(cm.messages(703))
	allocs := b.Mallocs - a.Mallocs
	t.Logf("3s idle: %d heartbeats seen, %d allocations, heap %d -> %d KiB", beats, allocs, a.HeapAlloc>>10, b.HeapAlloc>>10)
	if allocs > 3000 { // the fake server's own work is in this number too
		t.Errorf("%d allocations in 3s of idling looks like a busy loop", allocs)
	}
	if b.HeapAlloc > a.HeapAlloc+1<<20 {
		t.Errorf("heap grew by %d KiB while idling", (b.HeapAlloc-a.HeapAlloc)>>10)
	}
}
