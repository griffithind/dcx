package ui

// CobraOutWriter wraps stdout for Cobra, respecting quiet mode.
// It delegates to ui.Writer() at write-time, so it automatically uses
// the configured writer after ui.Configure() is called.
type CobraOutWriter struct{}

// NewCobraOutWriter creates a new Cobra stdout writer.
func NewCobraOutWriter() *CobraOutWriter {
	return &CobraOutWriter{}
}

func (w *CobraOutWriter) Write(p []byte) (n int, err error) {
	if IsQuiet() {
		return len(p), nil // Suppress in quiet mode
	}
	return Writer().Write(p)
}

// CobraErrWriter wraps stderr for Cobra.
// It delegates to ui.ErrWriter() at write-time, so it automatically uses
// the configured writer after ui.Configure() is called.
type CobraErrWriter struct{}

// NewCobraErrWriter creates a new Cobra stderr writer.
func NewCobraErrWriter() *CobraErrWriter {
	return &CobraErrWriter{}
}

func (w *CobraErrWriter) Write(p []byte) (n int, err error) {
	return ErrWriter().Write(p) // Errors always pass through
}
