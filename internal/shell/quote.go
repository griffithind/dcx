// Package shell provides the single POSIX shell-quoting implementation used
// across dcx. Every place that builds a command line for /bin/sh must go
// through Quote or Join.
package shell

import "strings"

// isSafe reports whether s can be passed to a POSIX shell unquoted: it is
// non-empty and contains only characters with no special meaning.
func isSafe(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.ContainsRune("@%+=:,./_-", c):
		default:
			return false
		}
	}
	return true
}

// Quote returns s quoted so that a POSIX shell parses it as exactly one word
// with the literal value s. Safe strings are returned unchanged for
// readability; everything else is wrapped in single quotes, inside which no
// character is special except the single quote itself.
func Quote(s string) string {
	if isSafe(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Join quotes each argument and joins them with spaces, producing a command
// line that a POSIX shell splits back into exactly args.
func Join(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = Quote(a)
	}
	return strings.Join(quoted, " ")
}
