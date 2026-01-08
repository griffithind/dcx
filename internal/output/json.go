package output

import (
	"encoding/json"
	"io"
)

// JSONOutput provides structured JSON output helpers.
type JSONOutput struct {
	writer io.Writer
}

// NewJSONOutput creates a new JSON output helper.
func NewJSONOutput(w io.Writer) *JSONOutput {
	return &JSONOutput{writer: w}
}

// Write writes a value as pretty-printed JSON.
func (j *JSONOutput) Write(v interface{}) error {
	enc := json.NewEncoder(j.writer)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// WriteCompact writes a value as compact JSON (single line).
func (j *JSONOutput) WriteCompact(v interface{}) error {
	enc := json.NewEncoder(j.writer)
	return enc.Encode(v)
}

// WriteArray writes an array of values with newlines between items.
func (j *JSONOutput) WriteArray(items []interface{}) error {
	for _, item := range items {
		if err := j.WriteCompact(item); err != nil {
			return err
		}
	}
	return nil
}

// StatusResponse represents a standard status response.
type StatusResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// ErrorResponse represents a standard error response.
type ErrorResponse struct {
	Error   string            `json:"error"`
	Code    string            `json:"code,omitempty"`
	Message string            `json:"message,omitempty"`
	Hint    string            `json:"hint,omitempty"`
	Context map[string]string `json:"context,omitempty"`
}

// ListResponse represents a standard list response wrapper.
type ListResponse struct {
	Items interface{} `json:"items"`
	Count int         `json:"count"`
}

// WriteStatus writes a status response.
func (j *JSONOutput) WriteStatus(status, message string) error {
	return j.Write(StatusResponse{
		Status:  status,
		Message: message,
	})
}

// WriteError writes an error response.
func (j *JSONOutput) WriteError(err error) error {
	resp := ErrorResponse{
		Error: err.Error(),
	}

	// If it's a DCXError, include additional context
	if dcxErr, ok := AsDCXError(err); ok {
		resp.Code = dcxErr.Code
		resp.Message = dcxErr.Message
		resp.Hint = dcxErr.Hint
		resp.Context = dcxErr.Context
	}

	return j.Write(resp)
}

// WriteList writes a list response.
func (j *JSONOutput) WriteList(items interface{}, count int) error {
	return j.Write(ListResponse{
		Items: items,
		Count: count,
	})
}

// DCXError is imported from errors package for JSON formatting.
// We re-declare the interface here to avoid circular imports.
type dcxError interface {
	error
	GetCode() string
	GetMessage() string
	GetHint() string
	GetContext() map[string]string
}

// DCXErrorInfo holds error info for JSON output.
type DCXErrorInfo struct {
	Code    string
	Message string
	Hint    string
	Context map[string]string
}

// AsDCXError attempts to extract DCXError info from an error.
func AsDCXError(err error) (*DCXErrorInfo, bool) {
	// Use type assertion with interface matching
	type hasCode interface {
		Error() string
	}
	type hasCategory interface {
		Error() string
	}

	// Check if it has the expected fields via reflection or direct assertion
	// Since we can't import errors package, we check for the struct fields directly
	if e, ok := err.(interface {
		Error() string
		Unwrap() error
	}); ok {
		// Extract info from the error string format [category/code] message
		errStr := e.Error()
		if len(errStr) > 0 && errStr[0] == '[' {
			// It's likely a DCXError
			return &DCXErrorInfo{
				Message: errStr,
			}, true
		}
	}

	return nil, false
}
