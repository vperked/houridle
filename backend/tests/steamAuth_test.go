package tests

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"houridle/backend/online/steamauth"
	"houridle/backend/run/handover"
	"houridle/backend/views/ui"
)

func TestParseToken(t *testing.T) {
	tok := fakeToken(alice, "client", farFuture)
	info, err := steamauth.ParseToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	if info.SteamID != alice || !info.CanLogOn() || info.Expired(time.Now()) {
		t.Errorf("unexpected token info: %+v", info)
	}
	if !info.Expired(time.Unix(farFuture+1, 0)) {
		t.Error("token should be expired after its exp")
	}
	if web, _ := steamauth.ParseToken(fakeToken(alice, "web", farFuture)); web.CanLogOn() {
		t.Error("a web token can't sign in to the client network")
	}
	for _, bad := range []string{"", "a.b", "a.b.c", fakeToken(5, "client", 1), fakeToken(1<<60, "client", 1)} {
		if _, err := steamauth.ParseToken(bad); err == nil {
			t.Errorf("ParseToken(%q) should fail", bad)
		}
	}
}

func newStore(t *testing.T) *steamauth.Store {
	return &steamauth.Store{Path: filepath.Join(t.TempDir(), "houridle", "session.dat")}
}

func aliceSession() steamauth.Session {
	return steamauth.Session{AccountName: "alice", SteamID: alice, RefreshToken: fakeToken(alice, "client", farFuture)}
}

func TestStoreRoundTripIsPrivate(t *testing.T) {
	store := newStore(t)
	if _, err := store.Load(); !errors.Is(err, steamauth.ErrNotLoggedIn) {
		t.Fatalf("empty store: %v", err)
	}
	want := aliceSession()
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.SteamID != alice || got.AccountName != "alice" || got.RefreshToken != want.RefreshToken {
		t.Errorf("round trip changed the session: %v", got)
	}

	if runtime.GOOS != "windows" { // Windows protects the file with DPAPI instead
		fi, err := os.Stat(store.Path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("session file mode is %v, want 0600", fi.Mode().Perm())
		}
		di, _ := os.Stat(filepath.Dir(store.Path))
		if di.Mode().Perm() != 0o700 {
			t.Errorf("session folder mode is %v, want 0700", di.Mode().Perm())
		}
	} else {
		raw, _ := os.ReadFile(store.Path)
		if strings.Contains(string(raw), want.RefreshToken) || strings.Contains(string(raw), "alice") {
			t.Error("on Windows the session file should be encrypted")
		}
	}

	if err := store.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, steamauth.ErrNotLoggedIn) {
		t.Errorf("after Delete: %v", err)
	}
	if err := store.Delete(); err != nil {
		t.Errorf("deleting twice should be fine: %v", err)
	}
}

func TestStoreRefusesATokenForAnotherAccount(t *testing.T) {
	store := newStore(t)
	mixed := aliceSession()
	mixed.RefreshToken = fakeToken(bob, "client", farFuture)
	if err := store.Save(mixed); err == nil {
		t.Fatal("saving bob's token under alice's SteamID must fail")
	}
	if _, err := os.Stat(store.Path); err == nil {
		t.Error("nothing should have been written")
	}
}

func TestStoreRejectsDamagedFile(t *testing.T) {
	store := newStore(t)
	if err := store.Save(aliceSession()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path, []byte("not a session"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, steamauth.ErrCorrupt) {
		t.Errorf("damaged file: got %v, want ErrCorrupt", err)
	}
}

func TestSessionNeverPrintsItsToken(t *testing.T) {
	s := aliceSession()
	//lint:ignore S1025 every way of printing it is the point
	for _, out := range []string{fmt.Sprint(s), fmt.Sprintf("%v", s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s), fmt.Sprintf("%s", s)} {
		if strings.Contains(out, s.RefreshToken) || strings.Contains(out, "eyJ") {
			t.Errorf("session printed its token: %s", out)
		}
	}
}

func TestRememberWontSwitchAccountsQuietly(t *testing.T) {
	mgr := &steamauth.Manager{Store: newStore(t)}
	if err := mgr.Store.Save(aliceSession()); err != nil {
		t.Fatal(err)
	}
	bobResult := &steamauth.Result{AccountName: "bob", SteamID: bob, RefreshToken: fakeToken(bob, "client", farFuture)}

	if err := mgr.Remember(bobResult, nil); !errors.Is(err, steamauth.ErrDifferentAccount) {
		t.Fatalf("no confirmation: got %v", err)
	}
	if err := mgr.Remember(bobResult, func(steamauth.Session) bool { return false }); !errors.Is(err, steamauth.ErrDifferentAccount) {
		t.Fatalf("declined: got %v", err)
	}
	if s, _ := mgr.Store.Load(); s.SteamID != alice {
		t.Fatal("alice's session was replaced without consent")
	}
	if err := mgr.Remember(bobResult, func(old steamauth.Session) bool { return old.SteamID == alice }); err != nil {
		t.Fatal(err)
	}
	if s, _ := mgr.Store.Load(); s.SteamID != bob {
		t.Error("bob's session should have replaced alice's once confirmed")
	}
	// The same account can always refresh its own session, no question asked.
	if err := mgr.Remember(bobResult, nil); err != nil {
		t.Errorf("same account: %v", err)
	}
}

// ---------- sign-in against the fake Web API ----------

func TestQRLoginWaitsThenReturnsTheSession(t *testing.T) {
	refresh := fakeToken(alice, "client", farFuture)
	polls := 0
	api := newFakeAPI(t, func(c apiCall) apiReply {
		switch c.Method {
		case "BeginAuthSessionViaQR":
			return apiReply{Body: new(steamauth.Encoder).Varint(1, 77).String(2, "https://s.team/q/1/77").Bytes(3, []byte("req")).Out()}
		case "PollAuthSessionStatus":
			polls++
			if polls == 1 { // not scanned yet; Steam rotates the code
				return apiReply{Body: new(steamauth.Encoder).String(2, "https://s.team/q/1/78").Out()}
			}
			return apiReply{Body: new(steamauth.Encoder).String(3, refresh).String(6, "alice").Out()}
		}
		return apiReply{EResult: 2}
	})
	c := api.client()
	ctx := context.Background()

	p, err := c.BeginQR(ctx, "houridle")
	if err != nil {
		t.Fatal(err)
	}
	if p.ChallengeURL != "https://s.team/q/1/77" || p.Interval != 5*time.Second {
		t.Errorf("pending: %+v", p)
	}
	// It asked for a Steam *client* token, the kind that can sign in to the network.
	if got := api.called("BeginAuthSessionViaQR")[0].Req.Uint(2); got != 1 {
		t.Errorf("platform type = %d, want 1 (SteamClient)", got)
	}

	res, err := c.Poll(ctx, p)
	if err != nil || res != nil {
		t.Fatalf("first poll: %v %v", res, err)
	}
	if p.ChallengeURL != "https://s.team/q/1/78" {
		t.Error("the rotated QR code should replace the old one")
	}
	res, err = c.Poll(ctx, p)
	if err != nil || res == nil {
		t.Fatalf("second poll: %v %v", res, err)
	}
	if res.SteamID != alice || res.AccountName != "alice" || res.RefreshToken != refresh {
		t.Errorf("result: %v", res)
	}
}

func TestPollRejectsATokenForAnotherAccount(t *testing.T) {
	api := newFakeAPI(t, func(c apiCall) apiReply {
		return apiReply{Body: new(steamauth.Encoder).String(3, fakeToken(bob, "client", farFuture)).Out()}
	})
	p := &steamauth.Pending{ClientID: 1, RequestID: []byte("r"), SteamID: alice}
	if res, err := api.client().Poll(context.Background(), p); err == nil || res != nil {
		t.Fatalf("a token for bob must not complete alice's sign-in: %v %v", res, err)
	}
}

func TestPollRejectsAWebToken(t *testing.T) {
	api := newFakeAPI(t, func(c apiCall) apiReply {
		return apiReply{Body: new(steamauth.Encoder).String(3, fakeToken(alice, "web", farFuture)).Out()}
	})
	p := &steamauth.Pending{ClientID: 1, RequestID: []byte("r")}
	if _, err := api.client().Poll(context.Background(), p); err == nil {
		t.Fatal("a token that can't sign in to the client network is no use")
	}
}

func TestPasswordLoginSealsThePasswordAndSurfacesGuard(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const password = "correct horse battery staple"
	api := newFakeAPI(t, func(c apiCall) apiReply {
		switch c.Method {
		case "GetPasswordRSAPublicKey":
			return apiReply{Body: new(steamauth.Encoder).
				String(1, key.N.Text(16)).String(2, big.NewInt(int64(key.E)).Text(16)).Varint(3, 1234).Out()}
		case "BeginAuthSessionViaCredentials":
			confirm := func(kind uint64) *steamauth.Encoder { return new(steamauth.Encoder).Varint(1, kind) }
			return apiReply{Body: new(steamauth.Encoder).
				Varint(1, 5).Bytes(2, []byte("req")).
				Message(4, confirm(3)).Message(4, confirm(4)).
				Fixed64(5, alice).Out()}
		}
		return apiReply{EResult: 2}
	})
	p, err := api.client().BeginPassword(context.Background(), "alice", password, "houridle")
	if err != nil {
		t.Fatal(err)
	}
	if p.SteamID != alice {
		t.Errorf("steamid = %d", p.SteamID)
	}
	if kind, _, ok := p.NeedsCode(); !ok || kind != steamauth.GuardDeviceCode {
		t.Errorf("should ask for the mobile code, got %v %v", kind, ok)
	}
	if !p.CanConfirm(steamauth.GuardDeviceConfirmation) || p.CanConfirm(steamauth.GuardEmailConfirmation) {
		t.Error("confirmation types are wrong")
	}

	// The password must reach Steam only RSA-sealed.
	call := api.called("BeginAuthSessionViaCredentials")[0]
	if strings.Contains(call.Form, base64.StdEncoding.EncodeToString([]byte(password))) {
		t.Error("the password was sent unsealed")
	}
	sealed, err := base64.StdEncoding.DecodeString(call.Req.Str(3))
	if err != nil {
		t.Fatal(err)
	}
	//lint:ignore SA1019 checks what Steam receives, which is PKCS #1 v1.5 (see BeginPassword)
	plain, err := rsa.DecryptPKCS1v15(nil, key, sealed)
	if err != nil || string(plain) != password {
		t.Errorf("sealed password doesn't decrypt to the password: %v", err)
	}
	if call.Req.Uint(4) != 1234 {
		t.Error("the key timestamp must be echoed back")
	}
}

func TestSteamFailuresAreExplained(t *testing.T) {
	api := newFakeAPI(t, func(c apiCall) apiReply { return apiReply{EResult: steamauth.EResultInvalidPassword} })
	_, err := api.client().BeginQR(context.Background(), "x")
	var se *steamauth.Error
	if !errors.As(err, &se) || se.EResult != 5 || !strings.Contains(se.Error(), "password") {
		t.Errorf("got %v", err)
	}
}

func TestNetworkTroubleIsNotAnAuthFailure(t *testing.T) {
	api := newFakeAPI(t, func(c apiCall) apiReply { return apiReply{Status: 503} })
	if _, err := api.client().BeginQR(context.Background(), "x"); !errors.Is(err, steamauth.ErrNetwork) {
		t.Errorf("HTTP 503 should be a network error, got %v", err)
	}
	api.Close()
	_, err := api.client().BeginQR(context.Background(), "x")
	if !errors.Is(err, steamauth.ErrNetwork) {
		t.Errorf("a refused connection should be a network error, got %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "input_protobuf_encoded") {
		t.Error("the request (which could hold a password) leaked into the error")
	}
}

// ---------- keeping the session fresh ----------

func managerWith(t *testing.T, _ *fakeAPI, saved steamauth.Session) *steamauth.Manager {
	t.Helper()
	mgr := &steamauth.Manager{Client: steamauth.NewClient(), Store: newStore(t)}
	if err := mgr.Store.Save(saved); err != nil {
		t.Fatal(err)
	}
	return mgr
}

func TestCurrentNeedsASession(t *testing.T) {
	mgr := &steamauth.Manager{Client: steamauth.NewClient(), Store: newStore(t)}
	if _, err := mgr.Current(); !errors.Is(err, steamauth.ErrNotLoggedIn) {
		t.Errorf("got %v", err)
	}
}

func TestExpiredTokenIsCaughtWithoutAskingSteam(t *testing.T) {
	saved := aliceSession()
	saved.RefreshToken = fakeToken(alice, "client", 1000)
	mgr := managerWith(t, nil, saved)
	if _, err := mgr.Current(); !errors.Is(err, steamauth.ErrSessionExpired) {
		t.Errorf("got %v", err)
	}
}

func TestAdoptStoresARenewedToken(t *testing.T) {
	mgr := managerWith(t, nil, aliceSession())
	renewed := fakeToken(alice, "client", farFuture+1000)
	sess, _ := mgr.Current()
	got, err := mgr.Adopt(sess, renewed)
	if err != nil || got.RefreshToken != renewed {
		t.Fatalf("%v %v", got, err)
	}
	if saved, _ := mgr.Store.Load(); saved.RefreshToken != renewed {
		t.Error("the renewed token should have been saved")
	}
}

func TestAdoptIgnoresNothingAndRefusesOtherAccounts(t *testing.T) {
	mgr := managerWith(t, nil, aliceSession())
	sess, _ := mgr.Current()
	for _, same := range []string{"", sess.RefreshToken} {
		if got, err := mgr.Adopt(sess, same); err != nil || got.RefreshToken != sess.RefreshToken {
			t.Errorf("an empty or unchanged token is a no-op, got %v %v", got, err)
		}
	}
	for _, bad := range []string{fakeToken(bob, "client", farFuture), fakeToken(alice, "web", farFuture), "junk"} {
		if _, err := mgr.Adopt(sess, bad); err == nil {
			t.Errorf("token %q must be refused", bad)
		}
	}
	if saved, _ := mgr.Store.Load(); saved.RefreshToken != sess.RefreshToken || saved.SteamID != alice {
		t.Error("alice's session must be untouched")
	}
}

func TestHandoverKeepsSigningInWithTheSavedSession(t *testing.T) {
	defer func() { handover.SessionMode = false }()
	stop := time.Time{}
	plain := handover.Args(true, "/lib.so", stop, []uint32{730})
	if plain[0] != "-tray" || slices.Contains(plain, "start") {
		t.Errorf("normal mode args changed: %v", plain)
	}
	handover.SessionMode = true
	got := handover.Args(true, "/lib.so", stop, []uint32{730})
	if got[0] != "start" || got[1] != "-tray" || slices.Contains(got, "-dll") || got[len(got)-1] != "730" {
		t.Errorf("session mode args: %v", got)
	}
}

func TestCredentialsFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	c, ok := steamauth.CredentialsFromEnv(env(map[string]string{
		"HOURIDLE_USER": " alice ", "HOURIDLE_PASSWORD": " p w ", "HOURIDLE_GUARD_CODE": "ab12c\n",
	}))
	if !ok || c.User != "alice" || c.Password != " p w " || c.GuardCode != "ab12c" {
		t.Errorf("got %+v %v (the password must be kept exactly, spaces and all)", c, ok)
	}
	for _, m := range []map[string]string{
		{}, {"HOURIDLE_USER": "alice"}, {"HOURIDLE_PASSWORD": "x"}, {"HOURIDLE_USER": "  ", "HOURIDLE_PASSWORD": "x"},
	} {
		if _, ok := steamauth.CredentialsFromEnv(env(m)); ok {
			t.Errorf("%v should not count as credentials", m)
		}
	}
}

func TestPickMethod(t *testing.T) {
	const (
		qr  = steamauth.MethodQR
		pw  = steamauth.MethodPassword
		ask = steamauth.MethodAsk
	)
	cases := []struct {
		name     string
		method   string
		qrF, pwF bool
		user     string
		env      bool
		want     steamauth.Method
		wantErr  bool
	}{
		{"nothing chosen: ask", "", false, false, "", false, ask, false},
		{"-qr", "", true, false, "", false, qr, false},
		{"-password", "", false, true, "", false, pw, false},
		{"-method qr", "qr", false, false, "", false, qr, false},
		{"-method PASSWORD", "PASSWORD", false, false, "", false, pw, false},
		{"-user implies password", "", false, false, "alice", false, pw, false},
		{"env credentials imply password", "", false, false, "", true, pw, false},
		{"explicit -qr beats env credentials", "", true, false, "", true, qr, false},
		{"-method qr beats env credentials", "qr", false, false, "", true, qr, false},
		{"-qr and -password conflict", "", true, true, "", false, ask, true},
		{"-method contradicts flag", "qr", false, true, "", false, ask, true},
		{"unknown method", "carrier-pigeon", false, false, "", false, ask, true},
	}
	for _, c := range cases {
		got, err := steamauth.PickMethod(c.method, c.qrF, c.pwF, c.user, c.env)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("%s: got %v, %v; want %v (error: %v)", c.name, got, err, c.want, c.wantErr)
		}
	}
}

func TestMenuChoice(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want int
		ok   bool
	}{{"", 2, 0, true}, {"1", 2, 0, true}, {" 2 \n", 2, 1, true}, {"3", 2, 0, false}, {"0", 2, 0, false}, {"-1", 2, 0, false}, {"qr", 2, 0, false}, {"", 0, 0, false}} {
		got, ok := ui.ParseChoice(c.in, c.n)
		if got != c.want || ok != c.ok {
			t.Errorf("ParseChoice(%q, %d) = %d, %v; want %d, %v", c.in, c.n, got, ok, c.want, c.ok)
		}
	}
}
