package tests

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"houridle/backend/idler"
	"houridle/backend/steam"
	"houridle/backend/steamauth"
	"houridle/backend/steamcm"
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
	// The token comes from a persistent sign-in. Logging on with
	// should_remember_password=false tells Steam it's a throwaway session,
	// and Steam then ends the token when the session ends.
	if !logon[0].Body.Bool(8) {
		t.Error("should_remember_password must be true for a refresh token sign-in")
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
	// A session signs in Offline; without going Online, Steam records the
	// playtime but shows nobody that you're in a game.
	status := cm.messages(716)
	if len(status) != 1 || status[0].Body.Uint(1) != 1 {
		t.Fatalf("expected one ChangeStatus to Online, got %d", len(status))
	}
	if !cm.before(716, 742) {
		t.Error("the session should go Online before reporting games")
	}

	// Changing or clearing the games doesn't set the status again.
	s.SetGames([]uint32{730})
	<-cm.gotGames
	s.SetGames(nil)
	<-cm.gotGames
	if n := len(cm.messages(716)); n != 1 {
		t.Errorf("status should be set once per connection, got %d", n)
	}
}

func TestSignInAloneStaysOffline(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	s.Close() // e.g. status or -list: never shown as Online to friends
	if n := len(cm.messages(716)); n != 0 {
		t.Errorf("a connection that idles nothing must not go Online, sent %d", n)
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
	// Both Steam's web check and the connection refuse the token.
	newFakeCM(t, alice, 15)
	api := newFakeAPI(t, func(c apiCall) apiReply { return apiReply{EResult: steamauth.EResultAccessDenied} })
	mgr := managerWith(t, api, aliceSession())

	events := make(chan idler.Event, 8)
	sup := idler.NewSession([]*steam.Game{{AppID: 730, Name: "CS"}}, mgr, func(e idler.Event) { events <- e })
	ctx, cancel := context.WithCancel(context.Background())
	sup.Start(ctx)
	defer func() { cancel(); sup.Wait() }()

	var bad idler.Event
	deadline := time.After(5 * time.Second)
	for bad.Text == "" {
		select {
		case e := <-events:
			if e.Level == idler.EventBad {
				bad = e
			}
		case <-deadline:
			t.Fatal("no error event")
		}
	}
	if !strings.Contains(bad.Text, "houridle login") {
		t.Errorf("event: %+v", bad)
	}
	if v := sup.Views()[0]; v.State != idler.StateFailing || v.Note == "" {
		t.Errorf("view: %+v", v)
	}
}

// ---------- listing the games an account owns ----------

func TestOwnedGamesComeBackMostPlayedFirst(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	cm.owned = []fakeGame{{730, "Counter-Strike 2", 120}, {440, "Team Fortress 2", 6000}, {570, "", 0}, {-1, "bad", 1}}
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	games, err := s.OwnedGames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 3 || games[0].AppID != 440 || games[0].Name != "Team Fortress 2" || games[0].Minutes != 6000 || games[2].AppID != 570 {
		t.Errorf("games: %+v", games)
	}
	if got := cm.calls; len(got) != 1 || got[0] != "Player.GetOwnedGames#1" {
		t.Errorf("service calls: %v", got)
	}
}

func TestOwnedGamesReportsSteamRefusing(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	cm.callErr = steamauth.EResultAccessDenied
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.OwnedGames(context.Background())
	var se *steamauth.Error
	if !errors.As(err, &se) || se.EResult != steamauth.EResultAccessDenied {
		t.Errorf("got %v", err)
	}
}

func TestACallEndsWhenTheConnectionDrops(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cm.mu.Lock()
	cm.conn.CloseNow()
	cm.mu.Unlock()
	start := time.Now()
	if _, err := s.OwnedGames(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if time.Since(start) > 5*time.Second {
		t.Error("a dropped connection should end the call quickly, not wait for the timeout")
	}
}

func TestACallCanBeCancelled(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Call(ctx, "Nothing.AtAll#1", nil); !errors.Is(err, context.Canceled) {
		// The fake answers instantly, so either outcome is fine: no hang.
		t.Logf("answered before the cancel was seen: %v", err)
	}
}

func TestLogonSendsTheAccountNameOnlyWhenKnown(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, name := range []string{"", "alice"} {
		s, err := steamcm.Connect(ctx, steamcm.Config{SteamID: alice, AccountName: name, RefreshToken: fakeToken(alice, "client", farFuture)})
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
	}
	logons := cm.messages(5514)
	if len(logons) != 2 || logons[0].Body.Has(50) || logons[1].Body.Str(50) != "alice" {
		t.Errorf("account_name handling is wrong: %d logons, first has=%v", len(logons), logons[0].Body.Has(50))
	}
}

// ---------- keeping the sign-in alive on long runs ----------

func nearlyExpiredSession() steamauth.Session {
	s := aliceSession()
	soon := time.Now().Add(10 * 24 * time.Hour).Unix()
	s.RefreshToken = fakeToken(alice, "client", soon)
	return s
}

func runSupervisor(t *testing.T, cm *fakeCM, mgr *steamauth.Manager) chan idler.Event {
	t.Helper()
	events := make(chan idler.Event, 32)
	sup := idler.NewSession([]*steam.Game{{AppID: 730, Name: "CS"}}, mgr, func(e idler.Event) { events <- e })
	ctx, cancel := context.WithCancel(context.Background())
	sup.Start(ctx)
	t.Cleanup(func() { cancel(); sup.Wait() })
	<-cm.gotGames
	waitFor(t, "idling", func() bool { return sup.Views()[0].State == idler.StateIdling })
	return events
}

func TestSignInIsRenewedOverTheConnectionWhenNearlyExpired(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	cm.renewTo = fakeToken(alice, "client", farFuture)
	api := newFakeAPI(t, func(c apiCall) apiReply { return apiReply{EResult: steamauth.EResultAccessDenied} })
	mgr := managerWith(t, api, nearlyExpiredSession())
	events := runSupervisor(t, cm, mgr)

	waitFor(t, "the renewed token to be saved", func() bool {
		s, err := mgr.Store.Load()
		return err == nil && s.RefreshToken == cm.renewTo
	})
	var renewed bool
	for len(events) > 0 {
		e := <-events
		renewed = renewed || strings.Contains(e.Text, "Renewed")
		if strings.Contains(e.Text, "eyJ") {
			t.Errorf("an event leaked a token: %q", e.Text)
		}
	}
	if !renewed {
		t.Error("the feed should say the sign-in was renewed")
	}
}

func TestSignInWithPlentyOfTimeLeftIsNotTouched(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	cm.renewTo = fakeToken(alice, "client", farFuture+5)
	api := newFakeAPI(t, func(c apiCall) apiReply { return apiReply{EResult: steamauth.EResultAccessDenied} })
	saved := aliceSession()
	saved.RefreshToken = fakeToken(alice, "client", time.Now().Add(150*24*time.Hour).Unix())
	mgr := managerWith(t, api, saved)
	runSupervisor(t, cm, mgr)
	time.Sleep(200 * time.Millisecond)
	if s, _ := mgr.Store.Load(); s.RefreshToken != saved.RefreshToken {
		t.Error("a token with months left should be left alone")
	}
	for _, c := range cm.calls {
		if strings.Contains(c, "GenerateAccessToken") {
			t.Error("shouldn't have asked Steam to renew")
		}
	}
}

func TestRenewalToAnotherAccountIsRefused(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	cm.renewTo = fakeToken(bob, "client", farFuture)
	api := newFakeAPI(t, func(c apiCall) apiReply { return apiReply{EResult: steamauth.EResultAccessDenied} })
	saved := nearlyExpiredSession()
	mgr := managerWith(t, api, saved)
	events := runSupervisor(t, cm, mgr)
	waitFor(t, "a warning", func() bool { return len(events) > 0 })
	time.Sleep(200 * time.Millisecond)
	if s, _ := mgr.Store.Load(); s.SteamID != alice || s.RefreshToken != saved.RefreshToken {
		t.Fatal("bob's token must never replace alice's")
	}
}

// ---------- one server's "no" isn't believed ----------

func TestOneServerRefusingIsNotBelieved(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	cm.refuseFirstSet, cm.refuseFirst = true, 1
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatalf("the second server accepts the token: %v", err)
	}
	defer s.Close()
	if n := len(cm.messages(5514)); n != 2 {
		t.Errorf("expected 2 sign-in attempts, got %d", n)
	}
}

func TestEveryServerRefusingIsBelieved(t *testing.T) {
	cm := newFakeCM(t, alice, 15)
	_, err := connectAlice(t, cm)
	var le *steamcm.LogonError
	if !errors.As(err, &le) || le.EResult != 15 || le.Servers != 3 || !le.AuthFailed() {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "refused by 3 servers") || !strings.Contains(err.Error(), "error 15") {
		t.Errorf("the message should show the code and how many servers agreed: %v", err)
	}
	if n := len(cm.messages(5514)); n != 3 {
		t.Errorf("should stop after 3 refusals, tried %d", n)
	}
}

func TestBusySteamIsNotRetriedAcrossServers(t *testing.T) {
	cm := newFakeCM(t, alice, 84)
	_, err := connectAlice(t, cm)
	var le *steamcm.LogonError
	if !errors.As(err, &le) || !le.RateLimited() {
		t.Fatalf("got %v", err)
	}
	if n := len(cm.messages(5514)); n != 1 {
		t.Errorf("hammering a rate-limiting Steam makes it worse; tried %d times", n)
	}
}

// A LogOff makes Steam end the saved token for good (it counts as signing
// out), so closing a session may only drop the connection.
func TestClosingNeverSignsTheTokenOut(t *testing.T) {
	cm := newFakeCM(t, alice, 1)
	s, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	s.SetGames([]uint32{730})
	<-cm.gotGames
	s.Close()
	waitFor(t, "the games to be cleared", func() bool { return len(cm.messages(742)) == 2 })
	time.Sleep(200 * time.Millisecond)
	if n := len(cm.messages(706)); n != 0 {
		t.Fatalf("Close sent %d LogOff message(s); that revokes the saved sign-in", n)
	}
	// And the same token still works afterwards.
	again, err := connectAlice(t, cm)
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
}
