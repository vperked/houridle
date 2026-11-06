package steamworks

import (
	"errors"
	"time"
)

// Accessor names for ISteamUserStats across SDK versions, newest first.
var userStatsAccessors = []string{
	"SteamAPI_SteamUserStats_v013",
	"SteamAPI_SteamUserStats_v012",
	"SteamAPI_SteamUserStats_v011",
}

// Achievement is one of a game's achievements as Steam reports it for you.
type Achievement struct {
	ID          string `json:"id"` // API name, e.g. "ACH_WIN_ONE_GAME"
	Name        string `json:"name"`
	Description string `json:"description"`
	Hidden      bool   `json:"hidden"`
	Unlocked    bool   `json:"unlocked"`
}

// UserStats wraps the ISteamUserStats interface.
type UserStats struct {
	api  *API
	self uintptr
}

// UserStats returns the stats interface for the current app.
func (a *API) UserStats() (*UserStats, error) {
	for _, name := range userStatsAccessors {
		if accessor, ok := a.lib.lookup(name); ok {
			if self := accessor(); self != 0 {
				return &UserStats{api: a, self: self}, nil
			}
		}
	}
	return nil, &Error{CodeLoad, "this Steam API library has no ISteamUserStats interface houridle recognizes"}
}

// call invokes SteamAPI_ISteamUserStats_<method>(self, args...).
func (s *UserStats) call(method string, args ...uintptr) (uintptr, error) {
	fn, err := s.api.function("SteamAPI_ISteamUserStats_" + method)
	if err != nil {
		return 0, err
	}
	return fn(append([]uintptr{s.self}, args...)...), nil
}

// Load waits for Steam to deliver your stats, which newer SDKs request
// automatically at init (older ones need RequestCurrentStats). It returns the
// number of achievements; 0 after the timeout means the game has none.
func (s *UserStats) Load(timeout time.Duration) (int, error) {
	if request, ok := s.api.lib.lookup("SteamAPI_ISteamUserStats_RequestCurrentStats"); ok {
		request(s.self)
	}
	deadline := time.Now().Add(timeout)
	for {
		s.api.RunCallbacks()
		n, err := s.call("GetNumAchievements")
		if err != nil {
			return 0, err
		}
		if uint32(n) > 0 || time.Now().After(deadline) {
			return int(uint32(n)), nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Achievements lists every achievement with its display text and your state.
func (s *UserStats) Achievements(count int) ([]Achievement, error) {
	out := make([]Achievement, 0, count)
	for i := range count {
		r, err := s.call("GetAchievementName", uintptr(i))
		if err != nil {
			return nil, err
		}
		id := cString(r)
		if id == "" {
			continue
		}
		a := Achievement{
			ID:          id,
			Name:        s.attribute(id, "name"),
			Description: s.attribute(id, "desc"),
			Hidden:      s.attribute(id, "hidden") == "1",
		}
		if a.Name == "" {
			a.Name = id
		}
		var args cArgs
		achieved := args.buffer(1)
		r, err = s.call("GetAchievement", args.str(id), args.addr(achieved))
		args.done()
		if err == nil && boolResult(r) {
			a.Unlocked = achieved[0] != 0
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *UserStats) attribute(id, key string) string {
	var args cArgs
	r, err := s.call("GetAchievementDisplayAttribute", args.str(id), args.str(key))
	args.done()
	if err != nil {
		return ""
	}
	return cString(r)
}

// SetUnlocked unlocks or locks one achievement locally. Call Store to save.
func (s *UserStats) SetUnlocked(id string, unlocked bool) error {
	method := "ClearAchievement"
	if unlocked {
		method = "SetAchievement"
	}
	var args cArgs
	r, err := s.call(method, args.str(id))
	args.done()
	if err != nil {
		return err
	}
	if !boolResult(r) {
		return errors.New("rejected by Steam (the game may protect this achievement)")
	}
	return nil
}

// Store saves changes to Steam and pumps callbacks briefly so the upload
// finishes before the process exits.
func (s *UserStats) Store() error {
	r, err := s.call("StoreStats")
	if err != nil {
		return err
	}
	if !boolResult(r) {
		return errors.New("couldn't save the changes to Steam")
	}
	for range 20 {
		s.api.RunCallbacks()
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}
