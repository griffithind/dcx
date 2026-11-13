package shell

import (
	"os/exec"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestQuote(t *testing.T) {
	tests := map[string]string{
		"":              "''",
		"simple":        "simple",
		"/usr/bin/env":  "/usr/bin/env",
		"a=b,c:d@e%f+g": "a=b,c:d@e%f+g",
		"hello world":   "'hello world'",
		"it's":          `'it'\''s'`,
		"$(id)":         "'$(id)'",
		"`id`":          "'`id`'",
		`back\slash`:    `'back\slash'`,
		"a;b|c&d":       "'a;b|c&d'",
		"*":             "'*'",
		"~":             "'~'",
		"new\nline":     "'new\nline'",
	}
	for in, want := range tests {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %q, want %q", in, got, want)
		}
	}
}

// roundTrip has /bin/sh parse the quoted words and print each argument
// NUL-terminated, returning what the shell actually saw.
func roundTrip(t *testing.T, args []string) []string {
	t.Helper()
	script := `for a in "$@"; do printf '%s\0' "$a"; done`
	out, err := exec.Command("sh", "-c", "set -- "+Join(args)+"; "+script).Output()
	if err != nil {
		t.Fatalf("sh failed for %q: %v", args, err)
	}
	parts := strings.Split(string(out), "\x00")
	return parts[:len(parts)-1]
}

func TestJoinRoundTripsThroughShell(t *testing.T) {
	args := []string{"", "plain", "two words", "it's", `"dq"`, "$HOME", "$(echo pwned)", "`echo pwned`", `a\b`, "*", "glob?[x]", "semi;colon", "tab\there", "new\nline", "!bang", "#hash", "~tilde"}
	got := roundTrip(t, args)
	if len(got) != len(args) {
		t.Fatalf("got %d args, want %d: %q", len(got), len(args), got)
	}
	for i := range args {
		if got[i] != args[i] {
			t.Errorf("arg %d: shell saw %q, want %q", i, got[i], args[i])
		}
	}
}

func FuzzQuoteRoundTrip(f *testing.F) {
	for _, s := range []string{"", "a", "it's", "$(x)", "a b", "\\", "'\"'"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		// The shell cannot carry NUL bytes in arguments, and non-UTF-8 input
		// is irrelevant for our callers.
		if strings.ContainsRune(s, 0) || !utf8.ValidString(s) {
			t.Skip()
		}
		got := roundTrip(t, []string{s})
		if len(got) != 1 || got[0] != s {
			t.Fatalf("Quote(%q) round-tripped to %q", s, got)
		}
	})
}
