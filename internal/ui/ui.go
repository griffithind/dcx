// Package ui provides terminal output utilities using pterm.
package ui

import (
	"io"
	"os"
	"sync"

	"github.com/pterm/pterm"
)

// Verbosity represents the output verbosity level.
type Verbosity int

const (
	VerbosityQuiet   Verbosity = -1
	VerbosityNormal  Verbosity = 0
	VerbosityVerbose Verbosity = 1
)

// Config holds UI configuration.
type Config struct {
	Verbosity Verbosity
	NoColor   bool
	Writer    io.Writer
	ErrWriter io.Writer
}

var (
	config   Config
	configMu sync.Mutex
)

func init() {
	config = Config{
		Verbosity: VerbosityNormal,
		NoColor:   false,
		Writer:    os.Stdout,
		ErrWriter: os.Stderr,
	}
}

// Configure sets up the UI with the given configuration.
func Configure(cfg Config) {
	configMu.Lock()
	defer configMu.Unlock()

	if cfg.Writer == nil {
		cfg.Writer = os.Stdout
	}
	if cfg.ErrWriter == nil {
		cfg.ErrWriter = os.Stderr
	}

	config = cfg

	if cfg.NoColor {
		pterm.DisableColor()
	} else {
		pterm.EnableColor()
	}

	pterm.SetDefaultOutput(cfg.Writer)
}

// IsQuiet returns true if quiet mode is enabled.
func IsQuiet() bool {
	configMu.Lock()
	defer configMu.Unlock()
	return config.Verbosity == VerbosityQuiet
}

// IsVerbose returns true if verbose mode is enabled.
func IsVerbose() bool {
	configMu.Lock()
	defer configMu.Unlock()
	return config.Verbosity == VerbosityVerbose
}

// Writer returns the configured output writer.
func Writer() io.Writer {
	configMu.Lock()
	defer configMu.Unlock()
	return config.Writer
}

// ErrWriter returns the configured error writer.
func ErrWriter() io.Writer {
	configMu.Lock()
	defer configMu.Unlock()
	return config.ErrWriter
}

// Success prints a success message if not in quiet mode.
func Success(format string, args ...interface{}) {
	if IsQuiet() {
		return
	}
	pterm.Success.Printf(format+"\n", args...)
}

// Error prints an error message (always shown, even in quiet mode).
func Error(format string, args ...interface{}) {
	pterm.Error.WithWriter(ErrWriter()).Printf(format+"\n", args...)
}

// Warning prints a warning message if not in quiet mode.
func Warning(format string, args ...interface{}) {
	if IsQuiet() {
		return
	}
	pterm.Warning.WithWriter(ErrWriter()).Printf(format+"\n", args...)
}

// Info prints an info message if not in quiet mode.
func Info(format string, args ...interface{}) {
	if IsQuiet() {
		return
	}
	pterm.Info.Printf(format+"\n", args...)
}

// Verbose prints a message only in verbose mode.
func Verbose(format string, args ...interface{}) {
	if !IsVerbose() {
		return
	}
	pterm.FgGray.Printf(format+"\n", args...)
}

// Print prints a message if not in quiet mode.
func Print(format string, args ...interface{}) {
	if IsQuiet() {
		return
	}
	pterm.Printf(format, args...)
}

// Println prints a line if not in quiet mode.
func Println(args ...interface{}) {
	if IsQuiet() {
		return
	}
	pterm.Println(args...)
}

// Printf prints a formatted line if not in quiet mode.
func Printf(format string, args ...interface{}) {
	if IsQuiet() {
		return
	}
	pterm.Printf(format+"\n", args...)
}

// RenderTable renders a table with headers and rows.
// Does nothing in quiet mode.
func RenderTable(headers []string, rows [][]string) error {
	if IsQuiet() {
		return nil
	}
	data := pterm.TableData{headers}
	for _, row := range rows {
		data = append(data, row)
	}
	return pterm.DefaultTable.WithHasHeader().WithData(data).Render()
}

// Spinner wraps pterm spinner with quiet mode support.
type Spinner struct {
	printer *pterm.SpinnerPrinter
}

// StartSpinner starts a spinner with the given message.
// Returns a no-op spinner in quiet mode.
func StartSpinner(message string) *Spinner {
	if IsQuiet() {
		return &Spinner{}
	}
	s, _ := pterm.DefaultSpinner.Start(message)
	return &Spinner{printer: s}
}

// Success stops the spinner with a success message.
func (s *Spinner) Success(message string) {
	if s.printer != nil {
		s.printer.Success(message)
	}
}

// Fail stops the spinner with a failure message.
func (s *Spinner) Fail(message string) {
	if s.printer != nil {
		s.printer.Fail(message)
	}
}

// UpdateText updates the spinner text.
func (s *Spinner) UpdateText(message string) {
	if s.printer != nil {
		s.printer.UpdateText(message)
	}
}

// Stop stops the spinner without a message.
func (s *Spinner) Stop() {
	if s.printer != nil {
		s.printer.Stop()
	}
}
