package output

import (
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ANSI color codes.
const (
	Reset     = "\033[0m"
	Bold      = "\033[1m"
	Dim       = "\033[2m"
	Italic    = "\033[3m"
	Underline = "\033[4m"

	// Foreground colors
	Black   = "\033[30m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"

	// Bright foreground colors
	BrightBlack   = "\033[90m"
	BrightRed     = "\033[91m"
	BrightGreen   = "\033[92m"
	BrightYellow  = "\033[93m"
	BrightBlue    = "\033[94m"
	BrightMagenta = "\033[95m"
	BrightCyan    = "\033[96m"
	BrightWhite   = "\033[97m"

	// Background colors
	BgBlack   = "\033[40m"
	BgRed     = "\033[41m"
	BgGreen   = "\033[42m"
	BgYellow  = "\033[43m"
	BgBlue    = "\033[44m"
	BgMagenta = "\033[45m"
	BgCyan    = "\033[46m"
	BgWhite   = "\033[47m"
)

// ColorConfig holds color configuration and provides coloring methods.
type ColorConfig struct {
	enabled bool
}

// NewColorConfig creates a new ColorConfig with TTY and environment detection.
func NewColorConfig(w io.Writer, forceNoColor bool) *ColorConfig {
	enabled := !forceNoColor && shouldEnableColor(w)
	return &ColorConfig{enabled: enabled}
}

// shouldEnableColor determines if colors should be enabled based on terminal and environment.
func shouldEnableColor(w io.Writer) bool {
	// Check NO_COLOR environment variable (https://no-color.org/)
	if _, exists := os.LookupEnv("NO_COLOR"); exists {
		return false
	}

	// Check TERM=dumb
	if os.Getenv("TERM") == "dumb" {
		return false
	}

	// Check FORCE_COLOR environment variable
	if _, exists := os.LookupEnv("FORCE_COLOR"); exists {
		return true
	}

	// Check if output is a terminal
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}

	return false
}

// Enabled returns whether colors are enabled.
func (c *ColorConfig) Enabled() bool {
	return c.enabled
}

// Apply applies color codes to text if colors are enabled.
func (c *ColorConfig) Apply(text string, codes ...string) string {
	if !c.enabled || len(codes) == 0 {
		return text
	}
	return strings.Join(codes, "") + text + Reset
}

// --- Semantic coloring methods ---

// Bold makes text bold.
func (c *ColorConfig) Bold(text string) string {
	return c.Apply(text, Bold)
}

// Dim makes text dimmed.
func (c *ColorConfig) Dim(text string) string {
	return c.Apply(text, Dim)
}

// Success colors text for success messages.
func (c *ColorConfig) Success(text string) string {
	return c.Apply(text, Green, Bold)
}

// Error colors text for error messages.
func (c *ColorConfig) Error(text string) string {
	return c.Apply(text, Red, Bold)
}

// Warning colors text for warning messages.
func (c *ColorConfig) Warning(text string) string {
	return c.Apply(text, Yellow, Bold)
}

// Info colors text for info messages.
func (c *ColorConfig) Info(text string) string {
	return c.Apply(text, Cyan, Bold)
}

// Hint colors text for hints.
func (c *ColorConfig) Hint(text string) string {
	return c.Apply(text, BrightBlack)
}

// Code colors text for code/paths.
func (c *ColorConfig) Code(text string) string {
	return c.Apply(text, Cyan)
}

// Header colors text for headers.
func (c *ColorConfig) Header(text string) string {
	return c.Apply(text, Bold)
}

// Label colors text for labels/keys.
func (c *ColorConfig) Label(text string) string {
	return c.Apply(text, Blue)
}

// Value colors text for values.
func (c *ColorConfig) Value(text string) string {
	return c.Apply(text, White)
}

// --- Container state colors ---

// StateRunning colors text for running state.
func (c *ColorConfig) StateRunning(text string) string {
	return c.Apply(text, Green)
}

// StateStopped colors text for stopped state.
func (c *ColorConfig) StateStopped(text string) string {
	return c.Apply(text, Yellow)
}

// StateError colors text for error state.
func (c *ColorConfig) StateError(text string) string {
	return c.Apply(text, Red)
}

// StateUnknown colors text for unknown state.
func (c *ColorConfig) StateUnknown(text string) string {
	return c.Apply(text, BrightBlack)
}

// --- Check result colors ---

// CheckPass colors text for passed checks.
func (c *ColorConfig) CheckPass(text string) string {
	return c.Apply(text, Green)
}

// CheckFail colors text for failed checks.
func (c *ColorConfig) CheckFail(text string) string {
	return c.Apply(text, Red)
}

// CheckWarn colors text for warning checks.
func (c *ColorConfig) CheckWarn(text string) string {
	return c.Apply(text, Yellow)
}

// CheckSkip colors text for skipped checks.
func (c *ColorConfig) CheckSkip(text string) string {
	return c.Apply(text, BrightBlack)
}
