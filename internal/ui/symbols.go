package ui

import "github.com/pterm/pterm"

// Symbols provides consistent symbols for CLI output.
var Symbols = struct {
	// Check results
	CheckPass string
	CheckFail string
	CheckWarn string
	CheckSkip string

	// List formatting
	Bullet string
}{
	CheckPass: "✓",
	CheckFail: "✗",
	CheckWarn: "!",
	CheckSkip: "-",
	Bullet:    "•",
}

// StateColor returns colored text for a container state.
func StateColor(state string) string {
	switch state {
	case "running":
		return pterm.FgGreen.Sprint(state)
	case "stopped", "exited":
		return pterm.FgYellow.Sprint(state)
	case "error", "dead", "broken":
		return pterm.FgRed.Sprint(state)
	default:
		return pterm.FgGray.Sprint(state)
	}
}

// CheckResult represents a check result for formatting.
type CheckResult int

const (
	CheckResultPass CheckResult = iota
	CheckResultFail
	CheckResultWarn
	CheckResultSkip
)

// FormatCheck formats a check result with symbol and color.
func FormatCheck(result CheckResult, message string) string {
	switch result {
	case CheckResultPass:
		return pterm.FgGreen.Sprint(Symbols.CheckPass) + " " + message
	case CheckResultFail:
		return pterm.FgRed.Sprint(Symbols.CheckFail) + " " + message
	case CheckResultWarn:
		return pterm.FgYellow.Sprint(Symbols.CheckWarn) + " " + message
	case CheckResultSkip:
		return pterm.FgGray.Sprint(Symbols.CheckSkip) + " " + pterm.FgGray.Sprint(message)
	default:
		return message
	}
}

// FormatLabel formats a label with consistent styling.
func FormatLabel(label, value string) string {
	return pterm.FgBlue.Sprint(label+":") + " " + value
}

// Bold returns bold text.
func Bold(text string) string {
	return pterm.Bold.Sprint(text)
}

// Dim returns dimmed text.
func Dim(text string) string {
	return pterm.FgGray.Sprint(text)
}

// Code returns code-styled text.
func Code(text string) string {
	return pterm.FgCyan.Sprint(text)
}
