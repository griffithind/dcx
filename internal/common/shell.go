package common

import (
	"fmt"
	"strings"
)

// ShellQuote quotes a string for use in shell commands (ENV, ARG, RUN in Dockerfiles).
// It uses double quotes and escapes special characters that have meaning in double-quoted strings.
//
// Escapes: \, ", $, `
//
// Example:
//
//	ShellQuote("hello world")     → "hello world"
//	ShellQuote("$PATH")           → "\$PATH"
//	ShellQuote(`echo "hi"`)       → "echo \"hi\""
func ShellQuote(s string) string {
	// If string contains no special characters, return as-is
	if !strings.ContainsAny(s, " \t\n\"'$`\\!") {
		return s
	}

	// Use double quotes and escape special characters
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "$", "\\$")
	s = strings.ReplaceAll(s, "`", "\\`")

	return fmt.Sprintf("\"%s\"", s)
}

// LabelQuote quotes a string for use in Dockerfile LABEL statements.
// Per the devcontainer spec reference implementation, only ", \, and $ are escaped.
// Backticks don't need escaping in LABEL statements since they're not shell-processed.
//
// This differs from ShellQuote in that it does NOT escape backticks.
//
// Example:
//
//	LabelQuote(`{"key": "value"}`) → "{\"key\": \"value\"}"
func LabelQuote(s string) string {
	// Escape special characters per reference implementation
	// The regex (?=["\\$]) with '\' replacement inserts backslash before ", \, $
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "$", "\\$")

	return fmt.Sprintf("\"%s\"", s)
}

// ShellEscapeSingleQuote escapes a value for use within single-quoted shell strings.
// In single quotes, only single quotes need special handling (by ending the quote,
// adding an escaped quote, and starting a new quote).
//
// Example:
//
//	ShellEscapeSingleQuote("it's")     → "it'\"'\"'s"
//	ShellEscapeSingleQuote("hello")   → "hello"
func ShellEscapeSingleQuote(s string) string {
	// In single quotes, only single quotes need escaping
	// (by ending quote, adding escaped quote, starting new quote)
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "'\"'\"'")
	return s
}
