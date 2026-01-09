package ui

import (
	"errors"
	"fmt"
	"io"
	"strings"

	dcxerrors "github.com/griffithind/dcx/internal/errors"
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

	var dcxErr *dcxerrors.DCXError
	if errors.As(err, &dcxErr) {
		return f.formatDCXError(dcxErr)
	}

	return f.formatGenericError(err)
}

// formatDCXError formats a DCXError with full context.
func (f *ErrorFormatter) formatDCXError(err *dcxerrors.DCXError) string {
	var sb strings.Builder

	// Category badge
	badge := pterm.NewStyle(pterm.BgRed, pterm.FgWhite, pterm.Bold).
		Sprintf(" %s ", strings.ToUpper(string(err.Category)))
	sb.WriteString(badge)
	sb.WriteString(" ")

	// Error message
	sb.WriteString(pterm.FgRed.Sprint(err.Message))
	sb.WriteString("\n")

	// Cause (if present)
	if err.Cause != nil {
		sb.WriteString("\n")
		sb.WriteString(pterm.FgBlue.Sprint("Cause"))
		sb.WriteString(": ")
		sb.WriteString(err.Cause.Error())
		sb.WriteString("\n")
	}

	// Context (if present)
	if len(err.Context) > 0 {
		sb.WriteString("\n")
		sb.WriteString(pterm.FgBlue.Sprint("Context"))
		sb.WriteString(":\n")
		for k, v := range err.Context {
			sb.WriteString(fmt.Sprintf("  %s: %s\n", pterm.FgGray.Sprint(k), v))
		}
	}

	// Hint (if present)
	if err.Hint != "" {
		sb.WriteString("\n")
		sb.WriteString(pterm.FgCyan.Sprint("ℹ"))
		sb.WriteString(" ")
		sb.WriteString(pterm.FgGray.Sprint(err.Hint))
		sb.WriteString("\n")
	}

	// Documentation URL (if present)
	if err.DocURL != "" {
		sb.WriteString("\n")
		sb.WriteString(pterm.FgGray.Sprint("See: "))
		sb.WriteString(pterm.FgCyan.Sprint(err.DocURL))
		sb.WriteString("\n")
	}

	return sb.String()
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
	fmt.Fprint(f.writer, f.Format(err))
}

// PrintError prints a formatted error using the global configuration.
func PrintError(err error) {
	if err == nil {
		return
	}

	formatter := NewErrorFormatter(ErrWriter())
	formatter.Write(err)
}

// FormatErrorBrief returns a brief one-line error message.
func FormatErrorBrief(err error) string {
	if err == nil {
		return ""
	}

	var dcxErr *dcxerrors.DCXError
	if errors.As(err, &dcxErr) {
		return fmt.Sprintf("[%s/%s] %s", dcxErr.Category, dcxErr.Code, dcxErr.Message)
	}

	return err.Error()
}

// IsUserError returns true if the error is likely a user error (vs internal error).
func IsUserError(err error) bool {
	if err == nil {
		return false
	}

	var dcxErr *dcxerrors.DCXError
	if errors.As(err, &dcxErr) {
		// Internal errors are not user errors
		return dcxErr.Category != dcxerrors.CategoryInternal
	}

	return true
}
