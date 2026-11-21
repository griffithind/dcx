package ui

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
		return style("32", state)
	case "stopped", "exited":
		return style("33", state)
	case "error", "dead", "broken":
		return style("31", state)
	default:
		return style("90", state)
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
		return style("32", Symbols.CheckPass) + " " + message
	case CheckResultFail:
		return style("31", Symbols.CheckFail) + " " + message
	case CheckResultWarn:
		return style("33", Symbols.CheckWarn) + " " + message
	case CheckResultSkip:
		return style("90", Symbols.CheckSkip) + " " + style("90", message)
	default:
		return message
	}
}

// FormatLabel formats a label with consistent styling.
func FormatLabel(label, value string) string {
	return style("34", label+":") + " " + value
}

// Bold returns bold text.
func Bold(text string) string {
	return style("1", text)
}

// Dim returns dimmed text.
func Dim(text string) string {
	return style("90", text)
}

// Code returns code-styled text.
func Code(text string) string {
	return style("36", text)
}

// style wraps text in an ANSI SGR code when colour is on.
func style(code, text string) string {
	configMu.Lock()
	on := color
	configMu.Unlock()
	if !on || code == "" || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}
