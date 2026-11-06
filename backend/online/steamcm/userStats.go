package steamcm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"houridle/backend/online/steamauth"
)

// Achievement is one of a game's achievements, as the signed-in account
// has it.
type Achievement struct {
	ID          string // API name, e.g. "ACH_WIN_ONE_GAME"
	Name        string
	Description string
	Hidden      bool
	Unlocked    bool
	// Protected achievements are set only by the game's own servers: Steam
	// refuses a change to them (and, with them, the whole save).
	Protected bool

	statID uint32 // achievements are bits of a stat
	bit    uint
}

// UserStats is a game's achievements for the account, plus what Steam
// needs to accept changes to them.
type UserStats struct {
	AppID        uint32
	Achievements []Achievement
	crc          uint32
	values       map[uint32]uint32 // stat ID -> value, for bit stats
}

const statsTimeout = 30 * time.Second

// UserStats loads a game's achievements for the signed-in account. Like
// Steam's own client, it asks for the game's schema along with the stats.
func (s *Session) UserStats(ctx context.Context, appID uint32) (*UserStats, error) {
	req := new(steamauth.Encoder).
		Fixed64(1, uint64(appID)).
		Varint(3, uint64(uint32(0xFFFFFFFF))). // schema_local_version -1: send the schema
		Fixed64(4, s.cfg.SteamID)
	m, err := s.request(ctx, emsgClientGetUserStats, req.Out(), emsgClientGetUserStatsResponse)
	if err != nil {
		return nil, err
	}
	resp, err := steamauth.Decode(m.Body)
	if err != nil {
		return nil, err
	}
	if code := statsResult(resp); code != steamauth.EResultOK {
		return nil, statsError(code, "load achievements")
	}
	st := &UserStats{AppID: appID, crc: uint32(resp.Uint(3)), values: map[uint32]uint32{}}
	for _, stat := range resp.Messages(5) {
		st.values[uint32(stat.Uint(1))] = uint32(stat.Uint(2))
	}
	schema := resp.Raw(4)
	if len(schema) == 0 {
		return st, nil // no achievements
	}
	kv, err := ParseKV(schema)
	if err != nil {
		return nil, fmt.Errorf("couldn't read the achievement list: %w", err)
	}
	st.Achievements = achievementsFromSchema(kv, st.values)
	return st, nil
}

// achievementsFromSchema lists the bit-stat achievements in schema order.
func achievementsFromSchema(root *KV, values map[uint32]uint32) []Achievement {
	var out []Achievement
	if len(root.Children) == 0 {
		return nil
	}
	stats := root.Children[0].Child("stats") // the root key is the app ID
	if stats == nil {
		return nil
	}
	for _, stat := range stats.Children {
		// Achievements are the stats with bits. Steam writes their type as
		// "ACHIEVEMENTS" in current schemas and as 4 (or 5) in older ones;
		// the bits are what matter.
		id, err := strconv.ParseUint(stat.Key, 10, 32)
		if err != nil {
			continue
		}
		bits := stat.Child("bits")
		if bits == nil {
			continue
		}
		for _, b := range bits.Children {
			bit, err := strconv.ParseUint(b.Str("bit"), 10, 8)
			if err != nil {
				if bit, err = strconv.ParseUint(b.Key, 10, 8); err != nil || bit > 31 {
					continue
				}
			}
			if bit > 31 {
				continue
			}
			display := b.Child("display")
			a := Achievement{
				Protected:   protected(stat) || protected(b),
				ID:          b.Str("name"),
				Name:        localized(display.Child("name")),
				Description: localized(display.Child("desc")),
				Hidden:      display.Str("hidden") == "1",
				Unlocked:    values[uint32(id)]&(1<<bit) != 0,
				statID:      uint32(id),
				bit:         uint(bit),
			}
			if a.ID == "" {
				continue
			}
			if a.Name == "" {
				a.Name = a.ID
			}
			out = append(out, a)
		}
	}
	return out
}

// protected reports whether a schema entry is set only by game servers:
// its permission says "game server" (1) or "official game server" (2).
// Steam refuses changes to those from anywhere else.
func protected(k *KV) bool {
	p, _ := strconv.Atoi(k.Str("permission"))
	return p&3 != 0
}

// localized picks the English text from a schema string, which is either
// plain or a map of languages.
func localized(k *KV) string {
	if k == nil {
		return ""
	}
	if len(k.Children) == 0 {
		return k.Value
	}
	if s := k.Str("english"); s != "" {
		return s
	}
	for _, c := range k.Children {
		if c.Key != "token" && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

// SetAchievements unlocks and locks achievements (by ID) and saves them to
// Steam. It returns the IDs Steam didn't accept, with why.
func (s *Session) SetAchievements(ctx context.Context, st *UserStats, unlock, lock []string) (failed map[string]string, err error) {
	failed = map[string]string{}
	byID := map[string]Achievement{}
	for _, a := range st.Achievements {
		byID[a.ID] = a
	}
	values := map[uint32]uint32{}
	touched := map[uint32][]string{} // stat -> achievement IDs in it
	change := func(ids []string, set bool) {
		for _, id := range ids {
			a, ok := byID[id]
			if !ok {
				failed[id] = "not one of this game's achievements"
				continue
			}
			if a.Protected {
				failed[id] = "only the game's own servers can change this one"
				continue
			}
			v, ok := values[a.statID]
			if !ok {
				v = st.values[a.statID]
			}
			if set {
				v |= 1 << a.bit
			} else {
				v &^= 1 << a.bit
			}
			values[a.statID] = v
			touched[a.statID] = append(touched[a.statID], id)
		}
	}
	change(unlock, true)
	change(lock, false)
	if len(values) == 0 {
		return failed, nil
	}

	ids := make([]uint32, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	req := new(steamauth.Encoder).
		Fixed64(1, uint64(st.AppID)).
		Fixed64(2, s.cfg.SteamID).
		Fixed64(3, s.cfg.SteamID).
		Varint(4, uint64(st.crc)).
		Bool(5, false)
	for _, id := range ids {
		req.Message(6, new(steamauth.Encoder).Varint(1, uint64(id)).Varint(2, uint64(values[id])))
	}
	m, err := s.request(ctx, emsgClientStoreUserStats2, req.Out(), emsgClientStoreUserStatsResponse)
	if err != nil {
		return failed, err
	}
	resp, err := steamauth.Decode(m.Body)
	if err != nil {
		return failed, err
	}
	if code := statsResult(resp); code != steamauth.EResultOK {
		return failed, statsError(code, "save achievements")
	}
	// Keep the in-memory copy current, for a caller that shows it again. A
	// stat Steam refused keeps the value Steam says it has.
	for id, v := range values {
		st.values[id] = v
	}
	for _, f := range resp.Messages(4) {
		stat := uint32(f.Uint(1))
		for _, id := range touched[stat] {
			failed[id] = "Steam reverted it (the game checks this one on its servers)"
		}
		if _, ok := values[stat]; ok {
			st.values[stat] = uint32(f.Uint(2))
		}
	}
	if resp.Has(3) {
		st.crc = uint32(resp.Uint(3))
	}
	for i := range st.Achievements {
		a := &st.Achievements[i]
		a.Unlocked = st.values[a.statID]&(1<<a.bit) != 0
	}
	return failed, nil
}

// statsResult is the eresult of a stats reply (field 2, default Fail).
func statsResult(d *steamauth.Decoded) int {
	if !d.Has(2) {
		return steamauth.EResultFail
	}
	return int(int32(d.Uint(2)))
}

func statsError(code int, what string) error {
	switch code {
	case steamauth.EResultAccessDenied:
		return fmt.Errorf("Steam won't let this account %s for that game (is it owned?) (Steam error %d)", what, code)
	case 8: // InvalidParam
		return fmt.Errorf("Steam refused to %s: it didn't accept one of the changes (Steam error 8)", what)
	}
	return &steamauth.Error{EResult: code, Message: "couldn't " + what}
}

// request sends a message that Steam answers with respEmsg, and waits for
// the answer (matched by job ID, or by message type if Steam leaves the job
// out).
func (s *Session) request(ctx context.Context, emsg uint32, body []byte, respEmsg uint32) (Message, error) {
	ch := make(chan Message, 1)
	s.mu.Lock()
	s.nextJob++
	id := s.nextJob
	s.jobs[id] = ch
	if s.byEmsg == nil {
		s.byEmsg = map[uint32]chan Message{}
	}
	s.byEmsg[respEmsg] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.jobs, id)
		if s.byEmsg[respEmsg] == ch {
			delete(s.byEmsg, respEmsg)
		}
		s.mu.Unlock()
	}()

	sendCtx, cancel := context.WithTimeout(ctx, statsTimeout)
	defer cancel()
	if err := s.c.send(sendCtx, emsg, header{steamID: s.cfg.SteamID, sessionID: s.sessionID, jobSource: id}, body); err != nil {
		s.finish(err)
		return Message{}, fmt.Errorf("%w: %v", steamauth.ErrNetwork, err)
	}
	timer := time.NewTimer(statsTimeout)
	defer timer.Stop()
	select {
	case m := <-ch:
		return m, nil
	case <-s.done:
		return Message{}, fmt.Errorf("%w: connection closed", steamauth.ErrNetwork)
	case <-timer.C:
		return Message{}, errors.New("Steam didn't answer")
	case <-ctx.Done():
		return Message{}, ctx.Err()
	}
}
