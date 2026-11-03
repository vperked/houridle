package steamauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// DefaultBaseURL is Steam's public Web API.
const DefaultBaseURL = "https://api.steampowered.com"

const maxResponseBytes = 1 << 20

// EResult values from Steam that houridle reacts to.
const (
	EResultOK                = 1
	EResultFail              = 2
	EResultInvalidPassword   = 5
	EResultLoggedInElsewhere = 6
	EResultAccessDenied      = 15
	EResultTimeout           = 16
	EResultUnavailable       = 20
	EResultExpired           = 27
	EResultInvalidLoginAuth  = 65
	EResultRateLimited       = 84
	EResultLoginThrottle     = 87
	EResultTwoFactorMismatch = 88
)

// ErrNetwork wraps failures to reach Steam (as opposed to Steam saying no).
var ErrNetwork = errors.New("couldn't reach Steam")

// Error is Steam answering a request with a failure code.
type Error struct {
	EResult int
	Message string // Steam's own message, if any
}

func (e *Error) Error() string {
	text := EResultText(e.EResult)
	if e.Message != "" {
		return fmt.Sprintf("%s (Steam error %d: %s)", text, e.EResult, e.Message)
	}
	return fmt.Sprintf("%s (Steam error %d)", text, e.EResult)
}

// EResultText is a plain-language reason for the codes people hit at login.
func EResultText(code int) string {
	switch code {
	case EResultInvalidPassword:
		return "wrong username or password"
	case EResultInvalidLoginAuth, EResultTwoFactorMismatch:
		return "that Steam Guard code is wrong or expired"
	case EResultRateLimited, EResultLoginThrottle:
		return "Steam is rate limiting sign-ins from this network or account; wait a while and try again"
	case EResultAccessDenied:
		return "Steam denied access"
	case EResultExpired:
		return "the sign-in expired"
	case EResultTimeout:
		return "Steam timed out"
	case EResultUnavailable:
		return "Steam is unavailable right now"
	case EResultLoggedInElsewhere:
		return "this account is signed in elsewhere"
	default:
		return "Steam refused the request"
	}
}

// Client talks to the sign-in part of Steam's Web API.
type Client struct {
	HTTP    *http.Client
	BaseURL string
}

// NewClient returns a client for Steam's real Web API.
func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second}, BaseURL: DefaultBaseURL}
}

// call runs one IAuthenticationService method. The request is a protobuf,
// base64-encoded, the way Steam's own web and mobile clients send it. A
// failure code is returned as *Error; network trouble wraps ErrNetwork.
func (c *Client) call(ctx context.Context, method string, get bool, req []byte) (*Decoded, error) {
	endpoint := fmt.Sprintf("%s/IAuthenticationService/%s/v1/", c.BaseURL, method)
	encoded := base64.StdEncoding.EncodeToString(req)

	var httpReq *http.Request
	var err error
	if get {
		q := url.Values{"input_protobuf_encoded": {encoded}}
		httpReq, err = http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	} else {
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		if err = w.WriteField("input_protobuf_encoded", encoded); err == nil {
			err = w.Close()
		}
		if err == nil {
			httpReq, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
			if err == nil {
				httpReq.Header.Set("Content-Type", w.FormDataContentType())
			}
		}
	}
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "application/x-protobuf")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %v", ErrNetwork, scrub(err))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, scrub(err))
	}

	eresult := EResultOK
	if h := resp.Header.Get("X-Eresult"); h != "" {
		if n, perr := strconv.Atoi(h); perr == nil {
			eresult = n
		}
	}
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("%w: Steam answered HTTP %d", ErrNetwork, resp.StatusCode)
	}
	if eresult != EResultOK {
		return nil, &Error{EResult: eresult, Message: resp.Header.Get("X-Error_message")}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &Error{EResult: EResultFail, Message: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	return Decode(data)
}

// scrub drops the request URL from net/http errors, which would carry the
// encoded request (including a password, for a GET). Only the cause is kept.
func scrub(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
