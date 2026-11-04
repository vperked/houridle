//go:build windows || linux

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"houridle/src/handover"
	"houridle/src/steamauth"
	"houridle/src/steamcm"
	"houridle/src/ui"
)

const (
	loginTimeout   = 5 * time.Minute
	codeAttempts   = 3
	deviceName     = "houridle"
	checkTimeout   = 20 * time.Second
	accountCommand = "account"
)

// commands are the account subcommands, run as `houridle <command>`.
var commands = map[string]func(args []string) int{
	"login":        runLogin,
	"logout":       runLogout,
	"status":       runStatus,
	accountCommand: runAccount,
}

// ---------- login ----------

func runLogin(args []string) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	methodFlag := fs.String("method", "", `how to sign in: "qr" or "password" (asked if not given)`)
	qrFlag := fs.Bool("qr", false, "sign in by scanning a QR code with the Steam mobile app")
	password := fs.Bool("password", false, "sign in with an account name and password")
	user := fs.String("user", "", "Steam account name (password sign-in)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ui.Init()
	env, haveEnv := steamauth.CredentialsFromEnv(os.Getenv)
	// The secrets now live only in this process's memory: remove them from
	// the environment so nothing we start inherits them.
	os.Unsetenv(steamauth.EnvPassword)
	os.Unsetenv(steamauth.EnvGuardCode)
	if haveEnv && *user == "" {
		*user = env.User
	}

	method, err := steamauth.PickMethod(*methodFlag, *qrFlag, *password, *user, haveEnv)
	if err != nil {
		return ui.Fatal(err)
	}
	if method == steamauth.MethodAsk {
		if !ui.CanPrompt() {
			return ui.Fatal(errors.New("houridle login needs a terminal to ask how to sign in; run it in a console, or pass -qr or -password (or set HOURIDLE_USER and HOURIDLE_PASSWORD)"))
		}
		if method, err = askMethod(); err != nil {
			return ui.Fatal(err)
		}
	}
	if method == steamauth.MethodQR && !ui.CanPrompt() {
		return ui.Fatal(errors.New("a QR code needs a terminal to show it in"))
	}
	mgr, err := steamauth.NewManager()
	if err != nil {
		return ui.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()

	var res *steamauth.Result
	if method == steamauth.MethodPassword {
		res, err = loginWithPassword(ctx, mgr.Client, *user, env)
	} else {
		res, err = loginWithQR(ctx, mgr.Client)
	}
	if err != nil {
		return ui.Fatal(loginError(err))
	}

	err = mgr.Remember(res, func(old steamauth.Session) bool {
		return ui.Confirm(fmt.Sprintf("%s is already signed in. Replace it with %s?", describe(old), res.AccountName))
	})
	switch {
	case errors.Is(err, steamauth.ErrDifferentAccount):
		ui.Notice("kept the existing sign-in, nothing changed")
		return 0
	case err != nil:
		return ui.Fatal(fmt.Errorf("signed in, but couldn't save it: %w", err))
	}
	fmt.Println()
	ui.Success("Signed in as " + describeResult(res))
	ui.Notice("Saved on this computer. Now run: houridle start")
	return 0
}

// askMethod lets the person pick how to sign in.
func askMethod() (steamauth.Method, error) {
	fmt.Println()
	choice, err := ui.Choose("How do you want to sign in?", []string{
		"QR code" + ui.Dim("  scan with the Steam mobile app (recommended: no password typed here)"),
		"Account name and password" + ui.Dim("  then approve Steam Guard"),
	})
	if err != nil {
		return steamauth.MethodAsk, errors.New("no sign-in method chosen")
	}
	if choice == 1 {
		return steamauth.MethodPassword, nil
	}
	return steamauth.MethodQR, nil
}

// loginWithQR shows a QR code for the Steam mobile app to scan.
func loginWithQR(ctx context.Context, c *steamauth.Client) (*steamauth.Result, error) {
	ui.Progress("Asking Steam for a sign-in code")
	p, err := c.BeginQR(ctx, deviceName)
	if err != nil {
		return nil, err
	}
	shown := ""
	for {
		if p.ChallengeURL != shown {
			shown = p.ChallengeURL
			drawQR(shown)
		}
		res, err := c.Poll(ctx, p)
		if err != nil || res != nil {
			return res, err
		}
		if !sleep(ctx, p.Interval) {
			return nil, ctx.Err()
		}
	}
}

func drawQR(url string) {
	fmt.Println()
	ui.Info("Scan this with the Steam mobile app:")
	ui.Info("Steam app → the Steam Guard tab → Sign in with a QR code")
	fmt.Println()
	if art := ui.QR(url); art != "" {
		fmt.Print(art)
	} else {
		ui.Info("(this terminal can't draw it; open this link in the Steam app instead)")
	}
	fmt.Println()
	ui.Info(ui.Dim(url))
	ui.Info(ui.Dim("Waiting for you to approve it in the app… (Ctrl+C to cancel)"))
}

// loginWithPassword signs in with a username and password, then completes
// whatever Steam Guard asks for.
func loginWithPassword(ctx context.Context, c *steamauth.Client, user string, env steamauth.EnvCredentials) (*steamauth.Result, error) {
	var err error
	if user == "" {
		if user, err = ui.ReadLine("Steam account name: "); err != nil || user == "" {
			return nil, errors.New("no account name given")
		}
	}
	pass := env.Password
	if pass == "" || env.User != user { // never pair an env password with another account name
		if !ui.CanPrompt() {
			return nil, errors.New("no password given")
		}
		if pass, err = ui.ReadSecret("Password (hidden): "); err != nil || pass == "" {
			return nil, errors.New("no password given")
		}
	}
	ui.Progress("Signing in")
	p, err := c.BeginPassword(ctx, user, pass, deviceName)
	pass = "" // the sealed copy is gone too; don't keep the plain one
	if err != nil {
		return nil, err
	}

	if kind, hint, ok := p.NeedsCode(); ok {
		if err := answerCode(ctx, c, p, kind, hint, env.GuardCode); err != nil {
			return nil, err
		}
	} else if p.CanConfirm(steamauth.GuardDeviceConfirmation) {
		ui.Info("Open the Steam mobile app and approve this sign-in.")
	} else if p.CanConfirm(steamauth.GuardEmailConfirmation) {
		ui.Info("Steam emailed you a link. Open it to approve this sign-in.")
	}

	for {
		res, err := c.Poll(ctx, p)
		if err != nil || res != nil {
			if res != nil && res.AccountName == "" {
				res.AccountName = user
			}
			return res, err
		}
		if !sleep(ctx, p.Interval) {
			return nil, ctx.Err()
		}
	}
}

// answerCode asks for a Steam Guard code. With a phone approval also on
// offer, an empty answer means "I'll approve it in the app".
func answerCode(ctx context.Context, c *steamauth.Client, p *steamauth.Pending, kind steamauth.GuardType, hint, preset string) error {
	where := "the Steam mobile app"
	if kind == steamauth.GuardEmailCode {
		where = "your email"
		if hint != "" {
			where += " (" + hint + ")"
		}
	}
	alt := p.CanConfirm(steamauth.GuardDeviceConfirmation)
	if preset != "" { // from HOURIDLE_GUARD_CODE: one try, they are single-use
		return c.SubmitCode(ctx, p, kind, strings.ToUpper(preset))
	}
	if !ui.CanPrompt() {
		if alt {
			ui.Info("Open the Steam mobile app and approve this sign-in.")
			return nil
		}
		return errors.New("Steam wants a Steam Guard code: set HOURIDLE_GUARD_CODE, or run houridle login in a terminal")
	}
	for attempt := 1; attempt <= codeAttempts; attempt++ {
		prompt := "Steam Guard code from " + where + ": "
		if alt {
			prompt = "Steam Guard code from " + where + ", or Enter to approve in the app: "
		}
		code, err := ui.ReadLine(prompt)
		if err != nil {
			return errors.New("no Steam Guard code given")
		}
		if code == "" && alt {
			ui.Info("Open the Steam mobile app and approve this sign-in.")
			return nil
		}
		err = c.SubmitCode(ctx, p, kind, strings.ToUpper(code))
		if err == nil {
			return nil
		}
		var se *steamauth.Error
		if !errors.As(err, &se) || attempt == codeAttempts {
			return err
		}
		ui.Warn(steamauth.EResultText(se.EResult) + ", try again")
	}
	return nil
}

// loginError turns a failed sign-in into advice.
func loginError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return errors.New("cancelled, nothing was saved")
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("timed out waiting for the sign-in to be approved; nothing was saved")
	case errors.Is(err, steamauth.ErrNetwork):
		return fmt.Errorf("%w. Check your internet connection and try again", err)
	}
	return fmt.Errorf("sign-in failed: %w", err)
}

// ---------- logout ----------

func runLogout(args []string) int {
	ui.Init()
	mgr, err := steamauth.NewManager()
	if err != nil {
		return ui.Fatal(err)
	}
	sess, loadErr := mgr.Store.Load()
	if errors.Is(loadErr, steamauth.ErrNotLoggedIn) {
		ui.Notice("not signed in, nothing to clear")
		return 0
	}
	if err := mgr.Store.Delete(); err != nil {
		return ui.Fatal(fmt.Errorf("couldn't remove the saved sign-in: %w", err))
	}
	if loadErr == nil {
		ui.Success("Signed out of " + describe(sess) + " on this computer")
	} else {
		ui.Success("Removed the saved sign-in")
	}
	ui.Notice("Steam may keep the old token valid until it expires. To end it everywhere, use \"sign out of all devices\" in your Steam account's security settings.")
	return 0
}

// ---------- status ----------

func runStatus(args []string) int {
	ui.Init()
	mgr, err := steamauth.NewManager()
	if err != nil {
		return ui.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()

	// Check that Steam is reachable while the sign-in is being checked.
	network := make(chan error, 1)
	go func() {
		_, err := steamcm.ListServers(ctx)
		network <- err
	}()

	fmt.Println()
	ui.Field("houridle", "v"+version)
	exit := 0
	sess, err := mgr.Current()
	switch {
	case errors.Is(err, steamauth.ErrNotLoggedIn):
		ui.Field("sign-in", ui.Bad("not signed in")+ui.Dim("  run: houridle login"))
		exit = 1
	case errors.Is(err, steamauth.ErrCorrupt):
		ui.Field("sign-in", ui.Bad("saved sign-in is damaged")+ui.Dim("  run: houridle login"))
		exit = 1
	default:
		ui.Field("account", describe(sess))
		ui.Field("steamid64", steamauth.FormatSteamID(sess.SteamID))
		if info, perr := steamauth.ParseToken(sess.RefreshToken); perr == nil {
			ui.Field("token", fmt.Sprintf("for %s, good until %s", strings.Join(info.Audience, ", "), info.Expires.Format("2006-01-02")))
		}
		_, rerr := mgr.Refresh(ctx)
		exit = reportSession(mgr, rerr)
		if code, checked := checkConnection(ctx, sess); checked {
			exit = code // the connection to Steam's servers is the real test
		}
	}

	if serr := <-network; serr == nil {
		ui.Field("steam network", ui.Good("reachable"))
	} else {
		ui.Field("steam network", ui.Bad("unreachable")+ui.Dim("  "+serr.Error()))
	}
	if handover.Idling() {
		ui.Field("idling", ui.Good("houridle is running"))
	} else {
		ui.Field("idling", ui.Dim("not running"))
	}
	fmt.Println()
	return exit
}

// checkConnection signs in to Steam's servers with the saved sign-in, the
// same way idling does, and reports what Steam says. It is skipped while
// houridle is idling, since a second connection would knock that one off.
// checked is false if it was skipped or Steam couldn't be reached.
func checkConnection(ctx context.Context, sess steamauth.Session) (exit int, checked bool) {
	if handover.Idling() {
		ui.Field("connection", ui.Dim("houridle is idling, so its connection is the check"))
		return 0, false
	}
	cs, err := steamcm.Connect(ctx, steamcm.Config{SteamID: sess.SteamID, AccountName: sess.AccountName, RefreshToken: sess.RefreshToken})
	switch {
	case err == nil:
		cs.Close()
		ui.Field("connection", ui.Good("signed in to Steam's servers"))
		return 0, true
	case errors.Is(err, steamauth.ErrNetwork):
		ui.Field("connection", ui.Dim("couldn't check: "+err.Error()))
		return 0, false
	}
	hint := ""
	var le *steamcm.LogonError
	if errors.As(err, &le) && le.AuthFailed() {
		hint = "  run: houridle login"
	}
	ui.Field("connection", ui.Bad("Steam refused the sign-in: "+err.Error())+ui.Dim(hint))
	return 1, true
}

// reportSession prints the result of checking the saved session with Steam
// and returns the exit code for it.
func reportSession(mgr *steamauth.Manager, err error) int {
	switch {
	case err == nil:
		sess, _ := mgr.Current()
		ui.Field("sign-in", ui.Good("valid")+ui.Dim("  "+expiryText(sess)))
		return 0
	case errors.As(err, new(*steamauth.RejectedError)):
		ui.Field("web check", ui.Dim(err.Error()+" (not conclusive; the connection check below decides)"))
		return 0
	case errors.Is(err, steamauth.ErrSessionExpired):
		ui.Field("sign-in", ui.Bad("expired or revoked")+ui.Dim("  run: houridle login"))
		return 1
	case errors.Is(err, steamauth.ErrNetwork):
		ui.Field("sign-in", ui.Dim("saved, but couldn't check with Steam (offline?)"))
		return 0
	default:
		ui.Field("sign-in", ui.Bad(err.Error()))
		return 1
	}
}

func expiryText(sess steamauth.Session) string {
	if sess.Expires.IsZero() {
		return ""
	}
	days := int(time.Until(sess.Expires).Hours() / 24)
	return fmt.Sprintf("renews itself; token good for about %d more days", days)
}

// ---------- account ----------

func runAccount(args []string) int {
	ui.Init()
	mgr, err := steamauth.NewManager()
	if err != nil {
		return ui.Fatal(err)
	}
	sess, err := mgr.Store.Load()
	if err != nil {
		return ui.Fatal(err)
	}
	name := sess.AccountName
	if name == "" {
		name = "(unknown)"
	}
	fmt.Println()
	ui.Field("username", name)
	ui.Field("steamid64", steamauth.FormatSteamID(sess.SteamID))
	fmt.Println()
	return 0
}

// ---------- helpers ----------

func describe(s steamauth.Session) string {
	if s.AccountName != "" {
		return fmt.Sprintf("%s (%d)", s.AccountName, s.SteamID)
	}
	return fmt.Sprint(s.SteamID)
}

func describeResult(r *steamauth.Result) string {
	if r.AccountName != "" {
		return fmt.Sprintf("%s (%d)", r.AccountName, r.SteamID)
	}
	return fmt.Sprint(r.SteamID)
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
