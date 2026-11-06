package ui

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
	"rsc.io/qr"
)

// ReadLine asks a question and returns the trimmed answer.
func ReadLine(prompt string) (string, error) {
	answer, err := readLine("  " + accent("◆") + " " + prompt)
	return strings.TrimSpace(answer), err
}

// ReadSecret asks for something private, such as a password. Nothing is
// shown as it is typed.
func ReadSecret(prompt string) (string, error) {
	fmt.Print("  " + accent("◆") + " " + prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(b), err
}

func ReadLineContext(ctx context.Context, prompt string) (string, error) {
	return readContext(ctx, func() (string, error) { return ReadLine(prompt) })
}

func ReadSecretContext(ctx context.Context, prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	state, _ := term.GetState(fd)
	answer, err := readContext(ctx, func() (string, error) { return ReadSecret(prompt) })
	if ctx.Err() != nil && answer == "" {
		if state != nil {
			term.Restore(fd, state)
		}
		fmt.Println()
	}
	return answer, err
}

func readContext(ctx context.Context, read func() (string, error)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	type result struct {
		answer string
		err    error
	}
	got := make(chan result, 1)
	go func() {
		answer, err := read()
		got <- result{answer, err}
	}()
	select {
	case r := <-got:
		return r.answer, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// ParseChoice turns an answer to a numbered menu of n items into a 0-based
// index. An empty answer picks the first item.
func ParseChoice(answer string, n int) (int, bool) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return 0, n > 0
	}
	v, err := strconv.Atoi(answer)
	if err != nil || v < 1 || v > n {
		return 0, false
	}
	return v - 1, true
}

// Choose shows a numbered menu and returns the 0-based pick. Enter picks
// the first item. It asks again after an invalid answer.
func Choose(prompt string, options []string) (int, error) {
	fmt.Println("  " + accent("◆") + " " + bold(prompt))
	for i, o := range options {
		fmt.Printf("    %s %s\n", accent(fmt.Sprint(i+1)+")"), o)
	}
	for {
		answer, err := readLine("  " + muted(fmt.Sprintf("choose 1-%d (Enter = 1): ", len(options))))
		if err != nil {
			return 0, err
		}
		if i, ok := ParseChoice(answer, len(options)); ok {
			return i, nil
		}
		fmt.Println("  " + warning(fmt.Sprintf("type a number from 1 to %d", len(options))))
	}
}

// Confirm asks a yes/no question; anything but y or yes is no.
func Confirm(prompt string) bool {
	answer, err := ReadLine(prompt + muted(" [y/N] "))
	if err != nil {
		return false
	}
	answer = strings.ToLower(answer)
	return answer == "y" || answer == "yes"
}

// ConfirmYes asks a yes/no question where Enter means yes.
func ConfirmYes(prompt string) bool {
	answer, err := ReadLine(prompt + muted(" [Y/n] "))
	if err != nil {
		return false
	}
	answer = strings.ToLower(answer)
	return answer == "" || answer == "y" || answer == "yes"
}

// Success prints a green one-line result.
func Success(msg string) { fmt.Println("  " + success("✓") + " " + msg) }

// Info prints a plain one-line message.
func Info(msg string) { fmt.Println("  " + msg) }

// Field prints an aligned "label  value" line for the status screens.
func Field(label, value string) { fmt.Printf("  %s %s\n", muted(fit(label, 14)), value) }

// Good, Bad and Dim color a value for Field.
func Good(s string) string { return success(s) }
func Bad(s string) string  { return danger(s) }
func Dim(s string) string  { return muted(s) }

// QR returns text that draws a QR code for data in the terminal: black on a
// white square, two rows of modules per line, so it scans the same on dark
// and light themes. It is empty when the terminal can't show colors.
func QR(data string) string {
	if !styled {
		return ""
	}
	code, err := qr.Encode(data, qr.L)
	if err != nil {
		return ""
	}
	const quiet = 2
	dark := func(x, y int) bool {
		if x < 0 || y < 0 || x >= code.Size || y >= code.Size {
			return false
		}
		return code.Black(x, y)
	}
	var b strings.Builder
	for y := -quiet; y < code.Size+quiet; y += 2 {
		b.WriteString("  \x1b[30;47m")
		for x := -quiet; x < code.Size+quiet; x++ {
			top, bottom := dark(x, y), dark(x, y+1)
			switch {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\x1b[0m\n")
	}
	return b.String()
}
