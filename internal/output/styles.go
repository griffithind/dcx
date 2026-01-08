package output

// Symbols provides consistent symbols for CLI output.
var Symbols = struct {
	// Status indicators
	Success string
	Error   string
	Warning string
	Info    string

	// Check results
	CheckPass string
	CheckFail string
	CheckWarn string
	CheckSkip string

	// Progress
	Spinner []string
	Arrow   string
	Bullet  string

	// Formatting
	Separator string
}{
	// Status indicators (using Unicode symbols)
	Success: "✓",
	Error:   "✗",
	Warning: "!",
	Info:    "ℹ",

	// Check results
	CheckPass: "✓",
	CheckFail: "✗",
	CheckWarn: "!",
	CheckSkip: "-",

	// Progress spinner frames
	Spinner: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
	Arrow:   "→",
	Bullet:  "•",

	// Formatting
	Separator: "─",
}

// StatusSymbol returns the appropriate symbol for a status.
func StatusSymbol(success bool) string {
	if success {
		return Symbols.Success
	}
	return Symbols.Error
}

// StateColor returns colored text for a container state.
func StateColor(state string) string {
	c := Color()
	switch state {
	case "running":
		return c.StateRunning(state)
	case "stopped", "exited":
		return c.StateStopped(state)
	case "error", "dead", "broken":
		return c.StateError(state)
	default:
		return c.StateUnknown(state)
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
	c := Color()
	switch result {
	case CheckResultPass:
		return c.CheckPass(Symbols.CheckPass) + " " + message
	case CheckResultFail:
		return c.CheckFail(Symbols.CheckFail) + " " + message
	case CheckResultWarn:
		return c.CheckWarn(Symbols.CheckWarn) + " " + message
	case CheckResultSkip:
		return c.CheckSkip(Symbols.CheckSkip) + " " + c.Dim(message)
	default:
		return message
	}
}

// FormatSuccess formats a success message with symbol.
func FormatSuccess(message string) string {
	c := Color()
	return c.Success(Symbols.Success) + " " + message
}

// FormatError formats an error message with symbol.
func FormatError(message string) string {
	c := Color()
	return c.Error(Symbols.Error) + " " + message
}

// FormatWarning formats a warning message with symbol.
func FormatWarning(message string) string {
	c := Color()
	return c.Warning(Symbols.Warning) + " " + message
}

// FormatInfo formats an info message with symbol.
func FormatInfo(message string) string {
	c := Color()
	return c.Info(Symbols.Info) + " " + message
}

// FormatLabel formats a label with consistent styling.
func FormatLabel(label, value string) string {
	c := Color()
	return c.Label(label+":") + " " + value
}

// FormatHeader formats a section header.
func FormatHeader(text string) string {
	return Color().Header(text)
}

// FormatCode formats code or paths.
func FormatCode(text string) string {
	return Color().Code(text)
}

// FormatDim formats dimmed/secondary text.
func FormatDim(text string) string {
	return Color().Dim(text)
}
