// Package ui is dcx's terminal output: data on stdout, progress on stderr,
// ANSI colour only on a terminal.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

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
	config   = Config{Verbosity: VerbosityNormal, Writer: os.Stdout, ErrWriter: os.Stderr}
	color    bool
	configMu sync.Mutex
)

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
	color = !cfg.NoColor && colorWanted(cfg.Writer)
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

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// IsVerbose reports whether -v was given.
func IsVerbose() bool {
	configMu.Lock()
	defer configMu.Unlock()
	return config.Verbosity == VerbosityVerbose
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

// Debug prints a diagnostic line (the docker commands dcx runs) to stderr,
// only with -v.
func Debug(format string, args ...interface{}) {
	if IsVerbose() {
		progress(Dim(fmt.Sprintf(format, args...)) + "\n")
	}
}

// Info prints a progress line ("Creating container...") to stderr unless
// in quiet mode. Library progress (report.Reporter) goes through it.
// Progress and warnings go to stderr so stdout carries only the data a
// command was asked for (`dcx config > out.json`, `dcx list | ...`).
func Info(format string, args ...interface{}) {
	if !IsQuiet() {
		progress(fmt.Sprintf(format, args...) + "\n")
	}
}

// Success prints a success message to stderr if not in quiet mode.
func Success(format string, args ...interface{}) {
	if !IsQuiet() {
		progress(labelled(" SUCCESS ", "30;42", fmt.Sprintf(format, args...)))
	}
}

// Warning prints a warning message if not in quiet mode.
func Warning(format string, args ...interface{}) {
	if !IsQuiet() {
		progress(labelled(" WARNING ", "30;43", fmt.Sprintf(format, args...)))
	}
}

// Error prints an error message (always shown, even in quiet mode).
func Error(format string, args ...interface{}) {
	progress(labelled("  ERROR  ", "97;41", fmt.Sprintf(format, args...)))
}

// PrintError prints a command's error (always shown).
func PrintError(err error) {
	if err != nil {
		progress(style("31", "✗") + " " + err.Error() + "\n")
	}
}

// labelled is message after a coloured label, continuation lines indented
// under the message.
func labelled(label, code, message string) string {
	indent := "\n" + strings.Repeat(" ", len(label)+1)
	return style(code, label) + " " + strings.ReplaceAll(message, "\n", indent) + "\n"
}

// Println prints a line of output the user asked for (status, lists,
// reports). Data is printed even in quiet mode, which only silences
// progress.
func Println(args ...interface{}) {
	_, _ = fmt.Fprintln(Writer(), args...)
}

// Printf prints a formatted line of output the user asked for (see
// Println).
func Printf(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(Writer(), format+"\n", args...)
}

// RenderTable renders a table with headers and rows (data: printed even in
// quiet mode): columns separated by " │ ", a rule under the header.
func RenderTable(headers []string, rows [][]string) error {
	widths := make([]int, len(headers))
	for _, row := range append([][]string{headers}, rows...) {
		for i, cell := range row {
			if i < len(widths) {
				widths[i] = max(widths[i], visibleWidth(cell))
			}
		}
	}
	line := func(cells []string, cellStyle string) string {
		var b strings.Builder
		for i, w := range widths {
			if i > 0 {
				b.WriteString(style("90", " │ "))
			}
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			b.WriteString(style(cellStyle, cell) + strings.Repeat(" ", w-visibleWidth(cell)))
		}
		return b.String() + "\n"
	}
	total := 3 * (len(widths) - 1)
	for _, w := range widths {
		total += w
	}
	var b strings.Builder
	b.WriteString(line(headers, "1"))
	b.WriteString(style("90", strings.Repeat("─", total)) + "\n")
	for _, row := range rows {
		b.WriteString(line(row, ""))
	}
	b.WriteString("\n")
	_, err := io.WriteString(Writer(), b.String())
	return err
}

// visibleWidth is the number of runes s shows, ANSI escapes excluded.
func visibleWidth(s string) int {
	n, escape := 0, false
	for _, r := range s {
		switch {
		case escape:
			escape = r != 'm'
		case r == '\x1b':
			escape = true
		default:
			n++
		}
	}
	return n
}
