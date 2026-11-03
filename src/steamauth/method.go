package steamauth

import (
	"fmt"
	"strings"
)

// Method is how a person signs in.
type Method int

const (
	MethodAsk      Method = iota // not decided: ask them
	MethodQR                     // scan a QR code with the Steam mobile app
	MethodPassword               // account name and password, then Steam Guard
)

func (m Method) String() string {
	switch m {
	case MethodQR:
		return "qr"
	case MethodPassword:
		return "password"
	}
	return "ask"
}

// ParseMethod reads a -method value.
func ParseMethod(s string) (Method, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return MethodAsk, nil
	case "qr", "qrcode", "qr-code":
		return MethodQR, nil
	case "password", "pass", "credentials":
		return MethodPassword, nil
	}
	return MethodAsk, fmt.Errorf("unknown login method %q: use qr or password", s)
}

// PickMethod decides the sign-in method from what the person asked for.
// Explicit choices win and must not contradict each other; otherwise
// credentials in the environment or an account name imply a password
// sign-in. MethodAsk means nothing was chosen, so the caller should ask.
func PickMethod(method string, qrFlag, passwordFlag bool, user string, haveEnvCreds bool) (Method, error) {
	chosen, err := ParseMethod(method)
	if err != nil {
		return MethodAsk, err
	}
	set := func(m Method) error {
		if chosen != MethodAsk && chosen != m {
			return fmt.Errorf("choose one login method: -qr, -password or -method")
		}
		chosen = m
		return nil
	}
	if qrFlag {
		if err := set(MethodQR); err != nil {
			return MethodAsk, err
		}
	}
	if passwordFlag {
		if err := set(MethodPassword); err != nil {
			return MethodAsk, err
		}
	}
	if chosen != MethodAsk {
		return chosen, nil
	}
	if user != "" || haveEnvCreds {
		return MethodPassword, nil
	}
	return MethodAsk, nil
}
