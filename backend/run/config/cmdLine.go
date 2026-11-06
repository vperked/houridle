package config

import (
	"flag"
	"strings"
)

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
			continue
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
