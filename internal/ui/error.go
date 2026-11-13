package ui

import (
	"fmt"
	"io"

	"github.com/pterm/pterm"
)

// ErrorFormatter provides consistent error formatting.
type ErrorFormatter struct {
	writer io.Writer
}

// NewErrorFormatter creates a new error formatter.
func NewErrorFormatter(w io.Writer) *ErrorFormatter {
	return &ErrorFormatter{
		writer: w,
	}
}

// Format formats an error for display.
func (f *ErrorFormatter) Format(err error) string {
	if err == nil {
		return ""
	}

	return f.formatGenericError(err)
}

// formatGenericError formats a regular error.
func (f *ErrorFormatter) formatGenericError(err error) string {
	return fmt.Sprintf("%s %s\n", pterm.FgRed.Sprint("✗"), err.Error())
}

// Write writes a formatted error to the writer.
func (f *ErrorFormatter) Write(err error) {
	if err == nil {
		return
	}
	_, _ = fmt.Fprint(f.writer, f.Format(err))
}

// PrintError prints a formatted error using the global configuration.
func PrintError(err error) {
	if err == nil {
		return
	}

	formatter := NewErrorFormatter(ErrWriter())
	formatter.Write(err)
}
