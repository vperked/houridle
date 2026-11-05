package tests

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"houridle/backend/steamauth"
	"houridle/backend/steamcm"
	"houridle/backend/ui"
)

// ---------- a server's own trouble moves on to the next server ----------

func TestTryAnotherServerIsHonoured(t *testing.T) {
	for _, code := range []int{48, 20, 10} { // TryAnotherCM, ServiceUnavailable, Busy
		cm := newFakeCM(t, alice, 1)
		cm.logonResults = []int{code}
		s, err := connectAlice(t, cm)
		if err != nil {
			t.Fatalf("eresult %d: the next server would have taken it: %v", code, err)
		}
		s.Close()
		if n := len(cm.messages(5514)); n != 2 {
			t.Errorf("eresult %d: expected 2 sign-in attempts, got %d", code, n)
		}
	}
}

func TestEveryServerBusyIsReported(t *testing.T) {
	cm := newFakeCM(t, alice, 48)
	_, err := connectAlice(t, cm)
	var le *steamcm.LogonError
	if !errors.As(err, &le) || le.EResult != 48 || le.AuthFailed() {
		t.Fatalf("got %v", err)
	}
}

func TestHangUpDuringSignInTriesTheNextServer(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	cm.hangUpFirst = 1
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatalf("one server hanging up must not stop the sign-in: %v", err)
	}
	s.Close()
	if n := len(cm.messages(5514)); n != 2 {
		t.Errorf("expected 2 sign-in attempts, got %d", n)
	}
}

func TestEveryServerHangingUpIsANetworkError(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	cm.hangUpFirst = 100
	_, err := connectAlice(t, cm)
	if !errors.Is(err, steamauth.ErrNetwork) || strings.Count(err.Error(), "couldn't reach Steam") != 1 {
		t.Fatalf("got %v", err)
	}
}

// ---------- a refusal followed by a hang-up is still a refusal ----------

func TestRefusalThenHangUpIsStillARefusal(t *testing.T) {
	cm := newFakeCM(t, alice, steamauth.EResultAccessDenied)
	cm.closeAfterLogon = true
	for i := 0; i < 20; i++ {
		_, err := connectAlice(t, cm)
		var le *steamcm.LogonError
		if !errors.As(err, &le) || !le.AuthFailed() {
			t.Fatalf("try %d: a dead sign-in was reported as %v", i, err)
		}
	}
}

// ---------- playing elsewhere, reported with the sign-in itself ----------

func playingState(blocked bool) []byte {
	return frame(9600, alice, 4242, new(steamauth.Encoder).Bool(2, blocked).Varint(3, 440).Out())
}

// Run with -race: the messages after the sign-in answer used to race the
// session ID being set.
func TestPlayingStateWithTheSignInIsOrdered(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	cm.afterLogon = [][]byte{playingState(true), playingState(false)}
	var mu sync.Mutex
	var seen []bool
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := steamcm.Connect(ctx, steamcm.Config{
		SteamID: alice, RefreshToken: fakeToken(alice, "client", farFuture),
		OnPlaying: func(blocked bool, _ uint32) {
			mu.Lock()
			seen = append(seen, blocked)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	waitFor(t, "both changes", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) == 2
	})
	mu.Lock()
	if !seen[0] || seen[1] {
		t.Errorf("changes arrived as %v", seen)
	}
	mu.Unlock()
	if s.Blocked() {
		t.Error("the other device stopped; the session should not be blocked")
	}
	if err := s.SetGames([]uint32{730}); err != nil {
		t.Fatal(err)
	}
	<-cm.gotGames
	for _, m := range cm.messages(742) {
		if m.Header.Int(2) != 4242 {
			t.Errorf("games sent with session ID %d", m.Header.Int(2))
		}
	}
}

// ---------- a connection that silently died is noticed ----------

func shortPings(t *testing.T) {
	oldEvery, oldTimeout := steamcm.PingEvery, steamcm.PingTimeout
	steamcm.PingEvery, steamcm.PingTimeout = 50*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { steamcm.PingEvery, steamcm.PingTimeout = oldEvery, oldTimeout })
}

func TestASilentServerEndsTheSession(t *testing.T) {
	shortPings(t)
	cm := newFakeCM(t, alice, 1)
	cm.stallAfterLogon = true
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	select {
	case <-s.Done():
		if !strings.Contains(s.Err().Error(), "stopped answering") {
			t.Errorf("ended with %v", s.Err())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a server that stopped answering pings was never noticed")
	}
}

func TestAnAnsweringServerKeepsTheSession(t *testing.T) {
	shortPings(t)
	cm := newFakeCM(t, alice, 1)
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	time.Sleep(time.Second) // about 20 pings
	if err := s.Err(); err != nil {
		t.Fatalf("a healthy connection was dropped: %v", err)
	}
}

// ---------- nested batches share one decompression budget ----------

func TestNestedBatchesCantMultiplyMemory(t *testing.T) {
	// Each inner batch unzips to 10 MiB: fine alone, but two in one frame
	// go over the 16 MiB a frame may cost.
	var inner bytes.Buffer
	big := frame(703, 0, 0, make([]byte, 10<<20))
	var n [4]byte
	binary.LittleEndian.PutUint32(n[:], uint32(len(big)))
	inner.Write(n[:])
	inner.Write(big)
	var zipped bytes.Buffer
	zw := gzip.NewWriter(&zipped)
	zw.Write(inner.Bytes())
	zw.Close()
	innerMulti := frame(1, 0, 0, new(steamauth.Encoder).Varint(1, uint64(inner.Len())).Bytes(2, zipped.Bytes()).Out())

	var outer bytes.Buffer
	for i := 0; i < 2; i++ {
		binary.LittleEndian.PutUint32(n[:], uint32(len(innerMulti)))
		outer.Write(n[:])
		outer.Write(innerMulti)
	}
	bomb := frame(1, 0, 0, new(steamauth.Encoder).Bytes(2, outer.Bytes()).Out())
	ok := frame(751, alice, 1, new(steamauth.Encoder).Varint(1, 1).Out())

	hostileCM(t, func() [][]byte { return [][]byte{bomb, ok} })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s, err := steamcm.Connect(ctx, steamcm.Config{SteamID: alice, RefreshToken: fakeToken(alice, "client", farFuture)})
	if err == nil {
		s.Close()
		t.Fatal("a frame unzipping to 20 MiB must be refused")
	}
}

// ---------- Steam's poll interval is kept sensible ----------

func TestPollIntervalIsBounded(t *testing.T) {
	cases := map[float32]time.Duration{
		1e30:                 30 * time.Second,
		float32(math.Inf(1)): 30 * time.Second,
		float32(math.NaN()):  5 * time.Second,
		-3:                   5 * time.Second,
		0.2:                  time.Second,
		7:                    7 * time.Second,
	}
	for asked, want := range cases {
		api := newFakeAPI(t, func(c apiCall) apiReply {
			return apiReply{Body: new(steamauth.Encoder).Varint(1, 77).String(2, "https://s.team/q/1/77").Bytes(3, []byte("req")).Float(4, asked).Out()}
		})
		p, err := api.client().BeginQR(context.Background(), "houridle")
		if err != nil {
			t.Fatal(err)
		}
		if p.Interval != want {
			t.Errorf("Steam asked for %v s: waiting %v, want %v", asked, p.Interval, want)
		}
	}
}

// ---------- Adopt explains what was wrong ----------

func TestAdoptSaysWhyItRefused(t *testing.T) {
	mgr := managerWith(t, nil, aliceSession())
	sess := aliceSession()
	cases := map[string]string{
		"not a token":                       "can't read",
		fakeToken(bob, "client", farFuture): "different account",
		fakeToken(alice, "web", farFuture):  "can't sign in",
	}
	for token, want := range cases {
		if _, err := mgr.Adopt(sess, token); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want it to say %q", token, err, want)
		}
	}
}

// ---------- a renewal that can't be saved is still used ----------

// blockSaves makes saving the session fail while loading it still works.
func blockSaves(t *testing.T, store *steamauth.Store) {
	t.Helper()
	if runtime.GOOS == "windows" {
		// An open handle without delete sharing stops the file being replaced.
		f, err := os.Open(store.Path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
	} else {
		if os.Geteuid() == 0 {
			t.Skip("root can write anywhere")
		}
		dir := filepath.Dir(store.Path)
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0o700) })
	}
	if err := store.Save(aliceSession()); err == nil {
		t.Skip("couldn't make saving fail on this system")
	}
}

func TestARenewalThatCantBeSavedIsUsedForTheNextConnection(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	cm.renewTo = fakeToken(alice, "client", farFuture)
	mgr := managerWith(t, nil, nearlyExpiredSession())
	blockSaves(t, mgr.Store)
	events := runSupervisor(t, cm, mgr)

	var warned bool
	deadline := time.After(5 * time.Second)
	for !warned {
		select {
		case e := <-events:
			warned = strings.Contains(e.Text, "couldn't save the renewed sign-in")
		case <-deadline:
			t.Fatal("no warning that the renewed sign-in wasn't saved")
		}
	}
	if s, _ := mgr.Store.Load(); s.RefreshToken != nearlyExpiredSession().RefreshToken {
		t.Fatal("the save was supposed to fail")
	}

	cm.mu.Lock()
	cm.conn.CloseNow() // the network drops; houridle reconnects
	cm.mu.Unlock()
	select {
	case <-cm.gotGames:
	case <-time.After(15 * time.Second):
		t.Fatal("never reconnected")
	}
	logons := cm.messages(5514)
	if len(logons) < 2 {
		t.Fatalf("expected a second sign-in, got %d", len(logons))
	}
	if got := logons[len(logons)-1].Body.Str(108); got != cm.renewTo {
		t.Error("the reconnect went back to the token Steam had replaced")
	}
}

// ---------- prompts give up when cancelled ----------

func TestPromptsStopWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ui.ReadLineContext(ctx, "x: "); !errors.Is(err, context.Canceled) {
		t.Errorf("ReadLineContext: %v", err)
	}
	if _, err := ui.ReadSecretContext(ctx, "x: "); !errors.Is(err, context.Canceled) {
		t.Errorf("ReadSecretContext: %v", err)
	}
}
