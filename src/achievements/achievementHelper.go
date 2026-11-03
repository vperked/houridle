// Package achievements lists and changes a game's Steam achievements. The
// Steamworks work happens in a helper process (one app per process), driven
// by the parent over JSON lines on stdin/stdout.
package achievements

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"houridle/src/steamworks"
)

// HelperFlag is the command-line flag that makes houridle run as an achievements helper.
const HelperFlag = "achievementsChild"

const (
	linePrefix   = "HOURIDLE " // marks our protocol lines among the DLL's own output
	statsTimeout = 10 * time.Second
	pumpEvery    = 500 * time.Millisecond
)

// listReply is the helper's first message: the achievements, or why it failed.
type listReply struct {
	Achievements []steamworks.Achievement `json:"achievements"`
	Code         string                   `json:"code,omitempty"`
	Error        string                   `json:"error,omitempty"`
}

// changeRequest is the parent's one message: what to unlock and lock.
type changeRequest struct {
	Unlock []string `json:"unlock"`
	Lock   []string `json:"lock"`
}

// Failure is one achievement Steam wouldn't change.
type Failure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

// Result is the helper's reply after applying a change request.
type Result struct {
	Changed int       `json:"changed"`
	Failed  []Failure `json:"failed,omitempty"`
	Error   string    `json:"error,omitempty"`
}

// RunHelper is the helper process: it loads the app's achievements, sends
// them, applies one change request, and exits. Returns the exit code.
func RunHelper(appID uint32, dllPath string) int {
	api, err := steamworks.Init(dllPath, appID)
	if err != nil {
		send(listReply{Code: errorCode(err), Error: err.Error()})
		return 1
	}
	defer api.Shutdown()

	stats, list, err := loadAchievements(api)
	if err != nil {
		send(listReply{Code: errorCode(err), Error: err.Error()})
		return 1
	}
	send(listReply{Achievements: list})

	line, ok := waitForRequest(api)
	if !ok {
		return 0 // the user cancelled
	}
	var req changeRequest
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		send(Result{Error: "bad request from houridle: " + err.Error()})
		return 1
	}
	send(apply(stats, req))
	return 0
}

// loadAchievements fetches the game's achievements and your progress.
func loadAchievements(api *steamworks.API) (*steamworks.UserStats, []steamworks.Achievement, error) {
	stats, err := api.UserStats()
	if err != nil {
		return nil, nil, err
	}
	count, err := stats.Load(statsTimeout)
	if err != nil {
		return nil, nil, err
	}
	list, err := stats.Achievements(count)
	return stats, list, err
}

// waitForRequest waits for the parent's change request, keeping Steam's
// callbacks flowing meanwhile. ok is false if the parent closed stdin.
func waitForRequest(api *steamworks.API) (line string, ok bool) {
	requests := make(chan string, 1)
	go func() {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			close(requests)
			return
		}
		requests <- line
	}()

	ticker := time.NewTicker(pumpEvery)
	defer ticker.Stop()
	for {
		select {
		case line, ok := <-requests:
			return line, ok
		case <-ticker.C:
			api.RunCallbacks()
		}
	}
}

func apply(stats *steamworks.UserStats, req changeRequest) Result {
	var res Result
	change := func(ids []string, unlock bool) {
		for _, id := range ids {
			if err := stats.SetUnlocked(id, unlock); err != nil {
				res.Failed = append(res.Failed, Failure{ID: id, Error: err.Error()})
				continue
			}
			res.Changed++
		}
	}
	change(req.Unlock, true)
	change(req.Lock, false)
	if res.Changed > 0 {
		if err := stats.Store(); err != nil {
			res.Error = err.Error()
		}
	}
	return res
}

func send(v any) {
	data, _ := json.Marshal(v)
	fmt.Println(linePrefix + string(data))
}

func errorCode(err error) string {
	var swErr *steamworks.Error
	if errors.As(err, &swErr) {
		return swErr.Code
	}
	return steamworks.CodeRefused
}
