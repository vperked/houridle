// Package steamcm is a small client for Steam's connection servers (CMs),
// enough to sign in with a saved refresh token and report games as being
// played. It is what lets houridle idle without the Steam client running.
//
// It speaks the same websocket protocol Steam's own clients use. Sign-in
// tokens come from the steamauth package; nothing here handles passwords.
package steamcm

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/coder/websocket"

	"houridle/backend/steamauth"
)

// Message numbers (EMsg) used here.
const (
	emsgMulti               = 1
	emsgClientHeartBeat     = 703
	emsgClientChangeStatus  = 716
	emsgClientGamesPlayed   = 742
	emsgClientLogOnResponse = 751
	emsgClientLicenseList   = 780
	emsgClientAccountInfo   = 768
	emsgClientLoggedOff     = 757
	emsgClientLogon         = 5514

	emsgClientGetUserStats           = 818
	emsgClientGetUserStatsResponse   = 819
	emsgClientStoreUserStatsResponse = 821
	emsgClientStoreUserStats2        = 5466
	emsgClientRequestFreeLicense     = 5572
	emsgClientRequestFreeLicenseResp = 5573
	emsgClientItemAnnouncements      = 5576
	emsgPlayingSessionState          = 9600
	emsgKickPlayingSession           = 9601
	emsgClientHello                  = 9805
	emsgServiceMethodCall            = 151
	emsgServiceMethodResp            = 147

	protoMask = 0x80000000
)

// DirectoryURL and ServerURL are Steam's real addresses. They are variables
// only so tests can point houridle at a local fake.
var (
	DirectoryURL = "https://api.steampowered.com/ISteamDirectory/GetCMListForConnect/v1/?cellid=0&cmtype=websockets"
	ServerURL    = func(endpoint string) string { return "wss://" + endpoint + "/cmsocket/" }
)

const (
	dialTimeout    = 15 * time.Second
	maxMessage     = 4 << 20
	maxUnzipped    = 16 << 20
	maxServerTries = 5
)

// Message is one decoded protobuf message from Steam.
type Message struct {
	EMsg   uint32
	Header *steamauth.Decoded
	Body   []byte
}

// ServerCache is a file to keep the last good server list in, so signing
// in still works when Steam's directory doesn't answer. "" keeps none.
var ServerCache string

var (
	serversMu  sync.Mutex
	badServers = map[string]time.Time{} // servers that failed recently, and when
)

const badServerFor = 15 * time.Minute

// markBad remembers that a server failed, so the next sign-ins try others
// first.
func markBad(server string) {
	serversMu.Lock()
	badServers[server] = time.Now()
	serversMu.Unlock()
}

// ListServers returns websocket CM endpoints ("host:port"), best first:
// Steam's current list (or, if Steam's directory can't be reached, the last
// one it gave), with servers that failed recently moved to the end.
func ListServers(ctx context.Context) ([]string, error) {
	list, err := fetchServers(ctx)
	if err == nil {
		if ServerCache != "" {
			if data, jerr := json.Marshal(list); jerr == nil {
				os.WriteFile(ServerCache, data, 0o600)
			}
		}
	} else if ServerCache != "" {
		if data, rerr := os.ReadFile(ServerCache); rerr == nil && json.Unmarshal(data, &list) == nil && len(list) > 0 {
			err = nil
		}
	}
	if err != nil {
		return nil, err
	}
	serversMu.Lock()
	defer serversMu.Unlock()
	var good, bad []string
	for _, s := range list {
		if t, ok := badServers[s]; ok && time.Since(t) < badServerFor {
			bad = append(bad, s)
		} else {
			good = append(good, s)
		}
	}
	return append(good, bad...), nil
}

// SetDirectoryURL changes where the server list comes from (see package
// steamver).
func SetDirectoryURL(u string) {
	serversMu.Lock()
	DirectoryURL = u
	serversMu.Unlock()
}

func fetchServers(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	serversMu.Lock()
	dir := DirectoryURL
	serversMu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dir, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", steamauth.ErrNetwork, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: server list answered HTTP %d", steamauth.ErrNetwork, resp.StatusCode)
	}
	var body struct {
		Response struct {
			ServerList []struct {
				Endpoint string `json:"endpoint"`
				Type     string `json:"type"`
			} `json:"serverlist"`
		} `json:"response"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("%w: unreadable server list", steamauth.ErrNetwork)
	}
	var out []string
	for _, s := range body.Response.ServerList {
		if s.Type == "websockets" && s.Endpoint != "" {
			out = append(out, s.Endpoint)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: Steam gave no servers", steamauth.ErrNetwork)
	}
	return out, nil
}

// conn is one websocket to a CM.
type conn struct {
	ws *websocket.Conn
}

func dial(ctx context.Context, url string) (*conn, error) {
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(maxMessage)
	return &conn{ws: ws}, nil
}

// close drops the connection at once. A polite websocket close handshake can
// wait seconds for a slow server, so shutdown (Ctrl+C) never waits on it.
func (c *conn) close() { c.ws.CloseNow() }

// header holds the message header fields houridle sets.
type header struct {
	steamID   uint64
	sessionID int32
	jobSource uint64 // set on requests that expect a reply
	jobName   string // service method, e.g. "Player.GetOwnedGames#1"
}

// send writes one protobuf message.
func (c *conn) send(ctx context.Context, emsg uint32, h header, body []byte) error {
	enc := new(steamauth.Encoder)
	if h.steamID != 0 {
		enc.Fixed64(1, h.steamID)
	}
	if h.sessionID != 0 {
		enc.Varint(2, uint64(int64(h.sessionID)))
	}
	if h.jobSource != 0 {
		enc.Fixed64(10, h.jobSource)
	}
	if h.jobName != "" {
		enc.String(12, h.jobName)
	}
	hb := enc.Out()

	buf := make([]byte, 8, 8+len(hb)+len(body))
	binary.LittleEndian.PutUint32(buf[0:], emsg|protoMask)
	binary.LittleEndian.PutUint32(buf[4:], uint32(len(hb)))
	buf = append(append(buf, hb...), body...)
	return c.ws.Write(ctx, websocket.MessageBinary, buf)
}

// read returns the next messages from Steam. A websocket frame may hold
// several (Steam batches and compresses them), so it returns a slice.
func (c *conn) read(ctx context.Context) ([]Message, error) {
	_, data, err := c.ws.Read(ctx)
	if err != nil {
		return nil, err
	}
	return parse(data, 0)
}

// parse decodes one message, unpacking batches (EMsg Multi) recursively.
func parse(data []byte, depth int) ([]Message, error) {
	if len(data) < 8 {
		return nil, errors.New("short message from Steam")
	}
	raw := binary.LittleEndian.Uint32(data)
	hdrLen := int(binary.LittleEndian.Uint32(data[4:]))
	if raw&protoMask == 0 {
		return nil, nil // an old-style message; houridle doesn't use any
	}
	if hdrLen < 0 || 8+hdrLen > len(data) {
		return nil, errors.New("bad message header from Steam")
	}
	hdr, err := steamauth.Decode(data[8 : 8+hdrLen])
	if err != nil {
		return nil, err
	}
	m := Message{EMsg: raw &^ protoMask, Header: hdr, Body: data[8+hdrLen:]}
	if m.EMsg != emsgMulti {
		return []Message{m}, nil
	}

	if depth > 2 {
		return nil, errors.New("too many nested batches from Steam")
	}
	multi, err := steamauth.Decode(m.Body)
	if err != nil {
		return nil, err
	}
	payload := multi.Raw(2)
	if size := multi.Uint(1); size > 0 {
		if size > maxUnzipped {
			return nil, errors.New("oversized batch from Steam")
		}
		zr, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		payload, err = io.ReadAll(io.LimitReader(zr, maxUnzipped+1))
		if err != nil || len(payload) > maxUnzipped {
			return nil, errors.New("bad compressed batch from Steam")
		}
	}
	var out []Message
	for len(payload) > 0 {
		if len(payload) < 4 {
			return nil, errors.New("truncated batch from Steam")
		}
		n := int(binary.LittleEndian.Uint32(payload))
		payload = payload[4:]
		if n < 0 || n > len(payload) {
			return nil, errors.New("truncated batch from Steam")
		}
		inner, err := parse(payload[:n], depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, inner...)
		payload = payload[n:]
	}
	return out, nil
}
