// Package output provides a unified output system for consistent CLI formatting.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

// Format represents the output format.
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// Verbosity represents the output verbosity level.
type Verbosity int

const (
	VerbosityQuiet   Verbosity = -1
	VerbosityNormal  Verbosity = 0
	VerbosityVerbose Verbosity = 1
)

// Config holds output configuration.
type Config struct {
	Format    Format
	Verbosity Verbosity
	NoColor   bool
	Writer    io.Writer
	ErrWriter io.Writer
}

// Output provides unified output formatting for the CLI.
type Output struct {
	config Config
	color  *ColorConfig
	mu     sync.Mutex
}

// global is the default output instance.
var global *Output
var globalMu sync.Mutex

func init() {
	global = New(Config{
		Format:    FormatText,
		Verbosity: VerbosityNormal,
		NoColor:   false,
		Writer:    os.Stdout,
		ErrWriter: os.Stderr,
	})
}

// New creates a new Output instance.
func New(cfg Config) *Output {
	if cfg.Writer == nil {
		cfg.Writer = os.Stdout
	}
	if cfg.ErrWriter == nil {
		cfg.ErrWriter = os.Stderr
	}

	o := &Output{
		config: cfg,
	}
	o.color = NewColorConfig(cfg.Writer, cfg.NoColor)
	return o
}

// Configure updates the global output configuration.
func Configure(cfg Config) {
	globalMu.Lock()
	defer globalMu.Unlock()
	global = New(cfg)
}

// Global returns the global output instance.
func Global() *Output {
	globalMu.Lock()
	defer globalMu.Unlock()
	return global
}

// SetFormat sets the output format.
func SetFormat(f Format) {
	globalMu.Lock()
	defer globalMu.Unlock()
	global.config.Format = f
}

// SetVerbosity sets the output verbosity.
func SetVerbosity(v Verbosity) {
	globalMu.Lock()
	defer globalMu.Unlock()
	global.config.Verbosity = v
}

// SetNoColor disables colors.
func SetNoColor(noColor bool) {
	globalMu.Lock()
	defer globalMu.Unlock()
	global.config.NoColor = noColor
	global.color = NewColorConfig(global.config.Writer, noColor)
}

// IsJSON returns true if JSON output is enabled.
func (o *Output) IsJSON() bool {
	return o.config.Format == FormatJSON
}

// IsQuiet returns true if quiet mode is enabled.
func (o *Output) IsQuiet() bool {
	return o.config.Verbosity == VerbosityQuiet
}

// IsVerbose returns true if verbose mode is enabled.
func (o *Output) IsVerbose() bool {
	return o.config.Verbosity == VerbosityVerbose
}

// Color returns the color configuration.
func (o *Output) Color() *ColorConfig {
	return o.color
}

// Writer returns the output writer.
func (o *Output) Writer() io.Writer {
	return o.config.Writer
}

// ErrWriter returns the error writer.
func (o *Output) ErrWriter() io.Writer {
	return o.config.ErrWriter
}

// Print prints a message to stdout.
func (o *Output) Print(format string, args ...interface{}) {
	if o.IsQuiet() {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Fprintf(o.config.Writer, format, args...)
}

// Println prints a message with newline to stdout.
func (o *Output) Println(args ...interface{}) {
	if o.IsQuiet() {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Fprintln(o.config.Writer, args...)
}

// Printf prints a formatted message with newline to stdout.
func (o *Output) Printf(format string, args ...interface{}) {
	if o.IsQuiet() {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Fprintf(o.config.Writer, format+"\n", args...)
}

// Verbose prints a message only in verbose mode.
func (o *Output) Verbose(format string, args ...interface{}) {
	if !o.IsVerbose() {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	fmt.Fprintf(o.config.Writer, format+"\n", args...)
}

// Error prints an error message to stderr.
func (o *Output) Error(format string, args ...interface{}) {
	o.mu.Lock()
	defer o.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(o.config.ErrWriter, "%s %s\n", o.color.Error("Error:"), msg)
}

// Warning prints a warning message to stderr.
func (o *Output) Warning(format string, args ...interface{}) {
	if o.IsQuiet() {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(o.config.ErrWriter, "%s %s\n", o.color.Warning("Warning:"), msg)
}

// Success prints a success message.
func (o *Output) Success(format string, args ...interface{}) {
	if o.IsQuiet() {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(o.config.Writer, "%s %s\n", o.color.Success("Success:"), msg)
}

// Info prints an info message.
func (o *Output) Info(format string, args ...interface{}) {
	if o.IsQuiet() {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(o.config.Writer, "%s %s\n", o.color.Info("Info:"), msg)
}

// JSON outputs data as JSON.
func (o *Output) JSON(v interface{}) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	enc := json.NewEncoder(o.config.Writer)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// JSONCompact outputs data as compact JSON (single line).
func (o *Output) JSONCompact(v interface{}) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	enc := json.NewEncoder(o.config.Writer)
	return enc.Encode(v)
}

// --- Global convenience functions ---

// Print prints a message using the global output.
func Print(format string, args ...interface{}) {
	Global().Print(format, args...)
}

// Println prints a message with newline using the global output.
func Println(args ...interface{}) {
	Global().Println(args...)
}

// Printf prints a formatted message using the global output.
func Printf(format string, args ...interface{}) {
	Global().Printf(format, args...)
}

// Verbose prints a verbose message using the global output.
func Verbose(format string, args ...interface{}) {
	Global().Verbose(format, args...)
}

// Error prints an error message using the global output.
func Error(format string, args ...interface{}) {
	Global().Error(format, args...)
}

// Warning prints a warning message using the global output.
func Warning(format string, args ...interface{}) {
	Global().Warning(format, args...)
}

// Success prints a success message using the global output.
func Success(format string, args ...interface{}) {
	Global().Success(format, args...)
}

// Info prints an info message using the global output.
func Info(format string, args ...interface{}) {
	Global().Info(format, args...)
}

// JSON outputs data as JSON using the global output.
func JSON(v interface{}) error {
	return Global().JSON(v)
}

// IsJSON returns true if JSON output is enabled globally.
func IsJSON() bool {
	return Global().IsJSON()
}

// IsQuiet returns true if quiet mode is enabled globally.
func IsQuiet() bool {
	return Global().IsQuiet()
}

// IsVerbose returns true if verbose mode is enabled globally.
func IsVerbose() bool {
	return Global().IsVerbose()
}

// Color returns the global color configuration.
func Color() *ColorConfig {
	return Global().Color()
}
