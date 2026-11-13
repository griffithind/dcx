// Package report defines how library code reports progress to the user.
//
// Only the CLI decides where messages go (terminal, nowhere for --json,
// a recorder in tests). Library packages receive a Reporter and never print
// directly, so machine-readable output and quiet mode cannot be corrupted by
// a stray fmt.Printf deep in the stack.
package report

import (
	"fmt"
	"sync"
)

// Reporter receives user-facing progress messages.
type Reporter interface {
	// Infof reports normal progress ("Building image: ...").
	Infof(format string, args ...any)
	// Warnf reports a problem that does not stop the operation.
	Warnf(format string, args ...any)
	// Errorf reports a failure the user should see even in quiet mode.
	Errorf(format string, args ...any)
}

type discard struct{}

func (discard) Infof(string, ...any)  {}
func (discard) Warnf(string, ...any)  {}
func (discard) Errorf(string, ...any) {}

// Discard drops every message.
var Discard Reporter = discard{}

// OrDiscard returns r, or Discard when r is nil.
func OrDiscard(r Reporter) Reporter {
	if r == nil {
		return Discard
	}
	return r
}

// Level classifies a recorded message.
type Level string

// Message levels.
const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Message is one recorded report.
type Message struct {
	Level Level
	Text  string
}

// Recorder stores messages in memory; intended for tests.
type Recorder struct {
	mu       sync.Mutex
	messages []Message
}

func (r *Recorder) add(l Level, format string, args []any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, Message{Level: l, Text: fmt.Sprintf(format, args...)})
}

// Infof implements Reporter.
func (r *Recorder) Infof(format string, args ...any) { r.add(LevelInfo, format, args) }

// Warnf implements Reporter.
func (r *Recorder) Warnf(format string, args ...any) { r.add(LevelWarn, format, args) }

// Errorf implements Reporter.
func (r *Recorder) Errorf(format string, args ...any) { r.add(LevelError, format, args) }

// Messages returns a copy of everything recorded so far.
func (r *Recorder) Messages() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Message(nil), r.messages...)
}
