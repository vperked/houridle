package steamauth

import "strings"

// Environment variables that let `houridle login` run without a keyboard.
const (
	EnvUser      = "HOURIDLE_USER"       // Steam account name
	EnvPassword  = "HOURIDLE_PASSWORD"   // its password
	EnvGuardCode = "HOURIDLE_GUARD_CODE" // optional one-time Steam Guard code
)

// EnvCredentials are sign-in details taken from the environment.
type EnvCredentials struct {
	User      string
	Password  string
	GuardCode string // may be empty
}

// CredentialsFromEnv reads the credentials from getenv (os.Getenv in real
// use). ok is false unless both the account name and password are set.
func CredentialsFromEnv(getenv func(string) string) (c EnvCredentials, ok bool) {
	c = EnvCredentials{
		User:      strings.TrimSpace(getenv(EnvUser)),
		Password:  getenv(EnvPassword),
		GuardCode: strings.TrimSpace(getenv(EnvGuardCode)),
	}
	return c, c.User != "" && c.Password != ""
}
