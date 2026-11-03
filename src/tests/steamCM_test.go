package tests

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"houridle/src/idler"
	"houridle/src/steam"
	"houridle/src/steamauth"
	"houridle/src/steamcm"
)

func connectAlice(t *testing.T, cm *fakeCM) (*steamcm.Session, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return steamcm.Connect(ctx, steamcm.Config{SteamID: alice, RefreshToken: fakeToken(alice, "client", farFuture)})
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestConnectSignsInWithTheRefreshToken(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	logon := cm.messages(5514)
	if len(logon) != 1 {
		t.Fatalf("expected one sign-in, got %d", len(logon))
	}
	if logon[0].Header.Uint(1) != alice {
		t.Error("the sign-in must name the account in its header")
	}
	if logon[0].Body.Str(108) != fakeToken(alice, "client", farFuture) {
		t.Error("the refresh token should be sent as the access token")
	}
	if logon[0].Body.Uint(1) == 0 {
		t.Error("protocol version missing")
	}
}

func TestConnectRefusesAnAccountMismatch(t *testing.T) {
	// Steam (or something pretending to be it) says it signed in as bob.
	cm := newFakeCM(t, bob, 1)
	if _, err := connectAlice(t, cm); err == nil || !strings.Contains(err.Error(), "different account") {
		t.Fatalf("got %v", err)
	}
}

func TestConnectWontStartWithAMismatchedToken(t *testing.T) {
	newFakeCM(t, alice, 1)
	_, err := steamcm.Connect(context.Background(), steamcm.Config{SteamID: alice, RefreshToken: fakeToken(bob, "client", farFuture)})
	if err == nil {
		t.Fatal("bob's token must not be used to sign in as alice")
	}
}

func TestConnectExplainsRefusals(t *testing.T) {
	for _, tc := range []struct {
		eresult         int
		authFailed, lim bool
	}{{5, true, false}, {15, true, false}, {27, true, false}, {84, false, true}, {20, false, false}} {
		cm := newFakeCM(t, alice, tc.eresult)
		_, err := connectAlice(t, cm)
		var le *steamcm.LogonError
		if !errors.As(err, &le) || le.EResult != tc.eresult {
			t.Fatalf("eresult %d: got %v", tc.eresult, err)
		}
		if le.AuthFailed() != tc.authFailed || le.RateLimited() != tc.lim {
			t.Errorf("eresult %d: authFailed=%v rateLimited=%v", tc.eresult, le.AuthFailed(), le.RateLimited())
		}
	}
}

func TestSetGamesReportsThemToSteam(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetGames([]uint32{730, 440}); err != nil {
		t.Fatal(err)
	}
	if got := <-cm.gotGames; !slices.Equal(got, []uint64{730, 440}) {
		t.Errorf("Steam was told %v", got)
	}
	m := cm.messages(742)[0]
	if m.Header.Int(2) != int64(cm.sessionID) || m.Header.Uint(1) != alice {
		t.Error("messages after sign-in must carry the session and the account")
	}
}

func TestPlayingElsewherePausesAndResumes(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	events := make(chan bool, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := steamcm.Connect(ctx, steamcm.Config{
		SteamID: alice, RefreshToken: fakeToken(alice, "client", farFuture),
		OnPlaying: func(blocked bool, _ uint32) { events <- blocked },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetGames([]uint32{730})
	<-cm.gotGames

	cm.push(9600, new(steamauth.Encoder).Bool(2, true).Varint(3, 570).Out())
	if !<-events {
		t.Fatal("expected blocked")
	}
	// While someone else plays, houridle must not grab the account back.
	s.SetGames([]uint32{730, 440})
	select {
	case g := <-cm.gotGames:
		t.Fatalf("games sent while blocked: %v", g)
	case <-time.After(200 * time.Millisecond):
	}
	cm.push(9600, new(steamauth.Encoder).Bool(2, false).Out())
	if <-events {
		t.Fatal("expected unblocked")
	}
	if got := <-cm.gotGames; !slices.Equal(got, []uint64{730, 440}) {
		t.Errorf("after the other device stopped, sent %v", got)
	}
}

func TestSteamLoggingUsOffEndsTheSession(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cm.push(757, new(steamauth.Encoder).Varint(1, 6).Out())
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session should end")
	}
	var off *steamcm.LoggedOffError
	if !errors.As(s.Err(), &off) || off.EResult != 6 {
		t.Errorf("got %v", s.Err())
	}
}

// ---------- the whole thing: saved sign-in -> idling ----------

func TestSessionSupervisorIdlesWithTheSavedSignIn(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	api := newFakeAPI(t, func(c apiCall) apiReply {
		return apiReply{Body: new(steamauth.Encoder).String(1, "access").Out()}
	})
	mgr := managerWith(t, api, aliceSession())

	games := []*steam.Game{{AppID: 730, Name: "CS"}, {AppID: 440, Name: "TF2"}}
	events := make(chan idler.Event, 32)
	sup := idler.NewSession(games, mgr, func(e idler.Event) { events <- e })
	ctx, cancel := context.WithCancel(context.Background())
	sup.Start(ctx)

	if got := <-cm.gotGames; !slices.Equal(got, []uint64{730, 440}) {
		t.Fatalf("Steam was told %v", got)
	}
	waitFor(t, "idling", func() bool { return sup.Views()[0].State == idler.StateIdling })
	views := sup.Views()
	if len(views) != 2 || views[1].Game.AppID != 440 || views[1].State != idler.StateIdling {
		t.Errorf("views: %+v", views)
	}

	cancel()
	sup.Wait()
	if sup.Views()[0].State != idler.StateStopped {
		t.Error("should be stopped after Wait")
	}
	// Stopping tells Steam the games are no longer being played.
	waitFor(t, "an empty games list", func() bool {
		select {
		case g := <-cm.gotGames:
			return len(g) == 0
		default:
			return false
		}
	})
	for len(events) > 0 {
		if e := <-events; strings.Contains(e.Text, "eyJ") {
			t.Errorf("an event leaked a token: %q", e.Text)
		}
	}
}

func TestSessionSupervisorReportsARevokedSignIn(t *testing.T) {
	newFakeCM(t, alice, 1)
	api := newFakeAPI(t, func(c apiCall) apiReply { return apiReply{EResult: steamauth.EResultAccessDenied} })
	mgr := managerWith(t, api, aliceSession())

	events := make(chan idler.Event, 8)
	sup := idler.NewSession([]*steam.Game{{AppID: 730, Name: "CS"}}, mgr, func(e idler.Event) { events <- e })
	ctx, cancel := context.WithCancel(context.Background())
	sup.Start(ctx)
	defer func() { cancel(); sup.Wait() }()

	select {
	case e := <-events:
		if e.Level != idler.EventBad || !strings.Contains(e.Text, "houridle login") {
			t.Errorf("event: %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event")
	}
	if v := sup.Views()[0]; v.State != idler.StateFailing || v.Note == "" {
		t.Errorf("view: %+v", v)
	}
}
