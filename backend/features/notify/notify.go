// Package notify sends houridle's alerts (sign-in refused, idling stopped,
// recovered) to a webhook, so a server can tell you without you checking
// houridle.log.
//
// Discord and Slack webhooks get the JSON they expect; anything else (ntfy,
// a home-made endpoint) gets the message as plain text in a POST.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	sendTimeout = 15 * time.Second
	// repeatAfter is how long the same message is held back, so a retry
	// loop ("refused, trying again in 5 minutes") alerts once, not hourly.
	repeatAfter = 6 * time.Hour
)

// Notifier posts alerts to one webhook URL. A nil *Notifier does nothing.
type Notifier struct {
	url    string
	kind   string // "discord", "slack" or "text"
	prefix string // e.g. the account name, when there are several

	// Client posts the alerts (a 15-second timeout by default). Replaceable,
	// e.g. by tests that check what each service is sent.
	Client *http.Client
	wg     sync.WaitGroup

	mu   sync.Mutex
	sent map[string]time.Time
}

// New checks the URL and returns a notifier for it. prefix, if not empty,
// starts every message (it names the account).
func New(rawURL, prefix string) (*Notifier, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("%q isn't a webhook URL (it should start with https://)", rawURL)
	}
	kind := "text"
	host := strings.ToLower(u.Hostname())
	switch {
	case (host == "discord.com" || host == "discordapp.com" || strings.HasSuffix(host, ".discord.com")) && strings.Contains(u.Path, "/api/webhooks/"):
		kind = "discord"
	case host == "hooks.slack.com":
		kind = "slack"
	}
	return &Notifier{url: u.String(), kind: kind, prefix: prefix, Client: &http.Client{Timeout: sendTimeout}, sent: map[string]time.Time{}}, nil
}

// Kind is how messages are formatted: "discord", "slack" or "text".
func (n *Notifier) Kind() string { return n.kind }

// Alert sends text in the background, unless the same text was sent within
// the last few hours. It never blocks idling; failures are dropped.
func (n *Notifier) Alert(text string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	if last, ok := n.sent[text]; ok && time.Since(last) < repeatAfter {
		n.mu.Unlock()
		return
	}
	n.sent[text] = time.Now()
	n.mu.Unlock()

	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		defer cancel()
		n.Send(ctx, text)
	}()
}

// Flush waits (briefly) for alerts still being sent, e.g. before exiting.
func (n *Notifier) Flush() {
	if n == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		n.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(sendTimeout):
	}
}

// Send posts one message now and reports what happened (for `houridle
// notify`, which tests the webhook).
func (n *Notifier) Send(ctx context.Context, text string) error {
	if n.prefix != "" {
		text = n.prefix + ": " + text
	}
	text = "houridle · " + text
	var body []byte
	contentType := "application/json"
	switch n.kind {
	case "discord":
		body, _ = json.Marshal(map[string]string{"content": text, "username": "houridle"})
	case "slack":
		body, _ = json.Marshal(map[string]string{"text": text})
	default:
		body, contentType = []byte(text), "text/plain; charset=utf-8"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	if n.kind == "text" {
		req.Header.Set("Title", "houridle") // ntfy shows this as the title
	}
	resp, err := n.Client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // the URL may hold a secret token; don't echo it
		}
		return fmt.Errorf("couldn't reach the webhook: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("the webhook answered HTTP %d", resp.StatusCode)
	}
	return nil
}

// Tracker decides which events become alerts: every error, the first
// success after an error (so you know it recovered), and anything marked
// worth an alert. With no Notifier set it does nothing. Safe for use from
// several goroutines.
type Tracker struct {
	mu      sync.Mutex
	n       *Notifier
	alerted bool
}

// Set changes where alerts go (nil: nowhere).
func (t *Tracker) Set(n *Notifier) {
	t.mu.Lock()
	t.n = n
	t.mu.Unlock()
}

// Event considers one event: bad for errors, good for successes, alert for
// news worth sending either way.
func (t *Tracker) Event(text string, bad, good, alert bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.n == nil {
		return
	}
	switch {
	case alert:
		t.n.Alert(text)
	case bad:
		t.alerted = true
		t.n.Alert(text)
	case good && t.alerted:
		t.alerted = false
		t.n.Alert("Working again: " + text)
	}
}
