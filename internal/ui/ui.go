// Package ui provides terminal output utilities using pterm.
package ui

import (
	"io"
	"os"
	"sync"

	"github.com/pterm/pterm"
	"golang.org/x/term"
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

	if cfg.NoColor || !colorWanted(cfg.Writer) {
		pterm.DisableColor()
		pterm.DisableStyling()
	} else {
		pterm.EnableColor()
		pterm.EnableStyling()
	}

	pterm.SetDefaultOutput(cfg.Writer)
}

// colorWanted reports whether output to w should be coloured: it is a
// terminal, NO_COLOR (https://no-color.org) is unset and TERM is not
// "dumb".
func colorWanted(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return isTerminal(w)
}

// interactive reports whether stdout is a terminal.
func interactive() bool {
	return isTerminal(Writer())
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// IsQuiet returns true if quiet mode is enabled.
func IsQuiet() bool {
	configMu.Lock()
	defer configMu.Unlock()
	return config.Verbosity == VerbosityQuiet
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

// Info prints a progress line ("Creating container...") unless in quiet
// mode. Library progress (report.Reporter) goes through it.
func Info(format string, args ...interface{}) {
	if IsQuiet() {
		return
	}
	pterm.Printf(format+"\n", args...)
}

// Println prints a line of output the user asked for (status, lists,
// reports). Data is printed even in quiet mode, which only silences
// progress.
func Println(args ...interface{}) {
	pterm.Println(args...)
}

// Printf prints a formatted line of output the user asked for (see
// Println).
func Printf(format string, args ...interface{}) {
	pterm.Printf(format+"\n", args...)
}

// RenderTable renders a table with headers and rows (data: printed even in
// quiet mode).
func RenderTable(headers []string, rows [][]string) error {
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

// StartSpinner starts a spinner with the given message. In quiet mode it
// does nothing; when stdout is not a terminal it prints the message once
// instead of animating (no \r junk in logs and pipes).
func StartSpinner(message string) *Spinner {
	if IsQuiet() {
		return &Spinner{}
	}
	if !interactive() {
		Info("%s", message)
		return &Spinner{}
	}
	s, _ := pterm.DefaultSpinner.Start(message)
	return &Spinner{printer: s}
}

// Stop stops the spinner without a message (the caller reports the
// outcome, e.g. by returning an error).
func (s *Spinner) Stop() {
	if s.printer != nil {
		_ = s.printer.Stop()
	}
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
