package output

import (
	"errors"
	"fmt"
	"io"
	"strings"

	dcxerrors "github.com/griffithind/dcx/internal/errors"
)

// ErrorFormatter provides consistent error formatting.
type ErrorFormatter struct {
	writer io.Writer
	color  *ColorConfig
}

// NewErrorFormatter creates a new error formatter.
func NewErrorFormatter(w io.Writer) *ErrorFormatter {
	return &ErrorFormatter{
		writer: w,
		color:  Color(),
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
	badge := f.color.Apply(
		fmt.Sprintf(" %s ", strings.ToUpper(string(err.Category))),
		BgRed, White, Bold,
	)
	sb.WriteString(badge)
	sb.WriteString(" ")

	// Error message
	sb.WriteString(f.color.Error(err.Message))
	sb.WriteString("\n")

	// Cause (if present)
	if err.Cause != nil {
		sb.WriteString("\n")
		sb.WriteString(f.color.Label("Cause"))
		sb.WriteString(": ")
		sb.WriteString(err.Cause.Error())
		sb.WriteString("\n")
	}

	// Context (if present)
	if len(err.Context) > 0 {
		sb.WriteString("\n")
		sb.WriteString(f.color.Label("Context"))
		sb.WriteString(":\n")
		for k, v := range err.Context {
			sb.WriteString(fmt.Sprintf("  %s: %s\n", f.color.Dim(k), v))
		}
	}

	// Hint (if present)
	if err.Hint != "" {
		sb.WriteString("\n")
		sb.WriteString(f.color.Info(Symbols.Info))
		sb.WriteString(" ")
		sb.WriteString(f.color.Hint(err.Hint))
		sb.WriteString("\n")
	}

	// Documentation URL (if present)
	if err.DocURL != "" {
		sb.WriteString("\n")
		sb.WriteString(f.color.Dim("See: "))
		sb.WriteString(f.color.Code(err.DocURL))
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatGenericError formats a regular error.
func (f *ErrorFormatter) formatGenericError(err error) string {
	return fmt.Sprintf("%s %s\n", f.color.Error(Symbols.Error), err.Error())
}

// Write writes a formatted error to the writer.
func (f *ErrorFormatter) Write(err error) {
	if err == nil {
		return
	}
	fmt.Fprint(f.writer, f.Format(err))
}

// PrintError prints a formatted error using the global output.
func PrintError(err error) {
	if err == nil {
		return
	}

	o := Global()
	formatter := NewErrorFormatter(o.ErrWriter())

	if o.IsJSON() {
		// JSON mode - use JSON error response
		var dcxErr *dcxerrors.DCXError
		if errors.As(err, &dcxErr) {
			resp := ErrorResponse{
				Error:   dcxErr.Error(),
				Code:    dcxErr.Code,
				Message: dcxErr.Message,
				Hint:    dcxErr.Hint,
				Context: dcxErr.Context,
			}
			o.JSON(resp)
		} else {
			resp := ErrorResponse{
				Error: err.Error(),
			}
			o.JSON(resp)
		}
		return
	}

	// Text mode - use formatted output
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
