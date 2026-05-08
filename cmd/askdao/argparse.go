package main

import "strings"

// flagsWithValue is the set of long-form flags that consume the next token as
// their value when written as `--flag value` (no `=`). Bool flags are absent.
// splitNameAndFlags uses this so the parser knows that `--from . my-agent`
// pairs `.` with `--from` and only `my-agent` is the positional.
var flagsWithValue = map[string]bool{
	"--from":    true,
	"--harness": true,
	"--dir":     true,
}

// splitNameAndFlags pulls the first non-flag positional out of args (the agent
// `<name>`) so that flags can appear in any order on the command line. Go's
// stdlib flag package stops parsing at the first non-flag argument, which
// makes `init my-agent --auto` silently drop --auto without this helper.
//
// Returns (name, remainingArgs, true) on success; (_, _, false) when no
// positional is present.
func splitNameAndFlags(args []string) (string, []string, bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			// Conventional terminator: name is required before it.
			if i == 0 {
				return "", nil, false
			}
			out := append([]string{}, args[:i]...)
			rest := append(out[1:], args[i+1:]...)
			return out[0], rest, true
		case strings.HasPrefix(a, "--") && flagsWithValue[a]:
			// Skip the flag's value token along with the flag itself.
			i++
		case strings.HasPrefix(a, "-"):
			// Bool-style long flag (--auto) or short flag — no value to skip.
		default:
			out := append([]string{}, args[:i]...)
			out = append(out, args[i+1:]...)
			return a, out, true
		}
	}
	return "", nil, false
}
