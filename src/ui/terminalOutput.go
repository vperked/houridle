// Package ui is houridle's terminal interface: colors and layout helpers, the
// live dashboard, the pickers, and plain-text fallbacks.
//
// Talking to the console itself differs per OS (consoleWindows.go,
// consoleLinux.go); everything else in this package is shared.
package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	altScreenOn  = "\x1b[?1049h\x1b[?25l" // switch to alternate screen, hide cursor
	altScreenOff = "\x1b[?25h\x1b[?1049l" // show cursor, back to normal screen
	cursorHome   = "\x1b[H"
	clearLine    = "\x1b[K"
	clearBelow   = "\x1b[J"

	maxContentWidth = 76
	minContentWidth = 16
	appIDWidth      = 8
)

// styled is true when stdout is a console that understands ANSI codes. It
// turns on colors, the live dashboard and the interactive picker.
var styled bool

var stdinReader = bufio.NewReader(os.Stdin)

// Init checks what the console can do and turns on colors where supported.
func Init() { styled = enableColors() }

// Styled reports whether the full interface (colors, dashboard, picker) is on.
func Styled() bool { return styled }

// CanPrompt reports whether houridle can ask the user something (input is a
// console, not a service or a pipe).
func CanPrompt() bool { return stdinIsTerminal() }

// contentWidth is the usable width inside the 2-cell left margin, leaving a
// little room on the right so lines never wrap.
func contentWidth(termW int) int {
	return max(minContentWidth, min(termW-4, maxContentWidth))
}

// ---------- palette ----------

func paint(code, s string) string {
	if !styled || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func accent(s string) string  { return paint("38;5;117", s) }
func success(s string) string { return paint("38;5;114", s) }
func warning(s string) string { return paint("38;5;221", s) }
func danger(s string) string  { return paint("38;5;203", s) }
func muted(s string) string   { return paint("38;5;245", s) }
func bold(s string) string    { return paint("1", s) }

// ---------- layout helpers (operate on plain text, before coloring) ----------

// fit truncates s to w cells (ending in …) and pads it to exactly w.
func fit(s string, w int) string {
	s = clip(s, w)
	return s + strings.Repeat(" ", max(0, w-utf8.RuneCountInString(s)))
}

// clip truncates s to w cells, ending in … when cut.
func clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= w {
		return s
	}
	r := []rune(s)
	return string(r[:w-1]) + "…"
}

// screen builds one full-screen frame. Each frame starts at the top-left and
// overwrites the previous one in place, which avoids flicker.
type screen struct{ b strings.Builder }

// line adds one line, clearing whatever was left on it from the last frame.
// Lines end in \r\n because raw mode on Linux doesn't add the \r itself.
func (s *screen) line(text string) {
	if s.b.Len() == 0 {
		s.b.WriteString(cursorHome)
	}
	s.b.WriteString(text)
	s.b.WriteString(clearLine + "\r\n")
}

// String returns the frame, clearing any old lines below it.
func (s *screen) String() string { return s.b.String() + clearBelow }

// banner is the "◆ houridle  subtitle" heading, clipped to width.
func banner(subtitle string, width int) string {
	const nameW = len("◆ houridle") - 2 // ◆ is 3 bytes, 1 cell
	out := "  " + accent("◆") + " " + bold("houridle")
	if room := width - nameW - 2; room >= 4 && subtitle != "" {
		out += "  " + muted(clip(subtitle, room))
	}
	return out
}

// ---------- plain output ----------

// Logf prints a timestamped line. Used when there's no dashboard.
func Logf(format string, a ...any) {
	fmt.Printf("  %s  %s\n", muted(time.Now().Format("15:04:05")), fmt.Sprintf(format, a...))
}

// Notice prints a quiet one-line message.
func Notice(msg string) { fmt.Println("\n  " + muted(msg) + "\n") }

// Progress prints a "working on it" line before a slow step.
func Progress(msg string) { fmt.Println("\n  " + accent(spinnerFrames[0]) + " " + muted(msg+"…")) }

// Warn prints a highlighted one-line warning.
func Warn(msg string) { fmt.Println("  " + warning("! "+msg)) }

func readLine(prompt string) (string, error) {
	fmt.Print(prompt)
	return stdinReader.ReadString('\n')
}

// errorsInMessageBox routes Fatal to a message box, for the console-less tray process.
var errorsInMessageBox bool

// UseMessageBoxForErrors makes Fatal show a message box instead of printing.
func UseMessageBoxForErrors() { errorsInMessageBox = true }

// Fatal prints an error and, when run by double-click, keeps the window open.
// In tray mode it shows a message box instead. Returns the exit code.
func Fatal(err error) int {
	if errorsInMessageBox {
		showErrorBox(err.Error())
		return 1
	}
	fmt.Fprintf(os.Stderr, "\n  %s %s\n\n", danger("✕"), err.Error())
	if stdinIsTerminal() && launchedByDoubleClick() {
		readLine(muted("  press enter to close"))
	}
	return 1
}

func hours(minutes int) string { return fmt.Sprintf("%.1f h", float64(minutes)/60) }

func clock(d time.Duration) string {
	s := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, (s%3600)/60, s%60)
}
