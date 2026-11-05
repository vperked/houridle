package config

import (
	"flag"
	"strings"
)

// FlagsFirst moves the flags in args (with their values) ahead of the plain
// arguments, keeping each group's order. Go's flag parsing stops at the first
// plain argument, so without this `houridle 730 -stop 3h` would read -stop as
// an app ID. Which flags take a value comes from fs. Everything after "--"
// stays a plain argument.
func FlagsFirst(fs *flag.FlagSet, args []string) []string {
	var flags, plain []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			plain = append(plain, args[i+1:]...)
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			plain = append(plain, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.TrimLeft(arg, "-")
		if strings.Contains(name, "=") {
			continue // -stop=3h carries its own value
		}
		if takesValue(fs, name) && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	if len(plain) == 0 {
		return flags
	}
	return append(append(flags, "--"), plain...)
}

// takesValue reports whether the flag called name needs a value after it
// (it isn't a boolean flag like -setup). Unknown flags are left for the
// parser to complain about.
func takesValue(fs *flag.FlagSet, name string) bool {
	f := fs.Lookup(name)
	if f == nil {
		return false
	}
	if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
		return false
	}
	return true
}
