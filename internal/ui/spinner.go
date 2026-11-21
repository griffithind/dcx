package ui

import (
	"io"
	"sync"
	"time"
)

// The spinner on stderr, if one is running. Progress written meanwhile
// clears its line first; the spinner redraws on its next tick.
var (
	spinnerMu sync.Mutex
	active    *Spinner
)

// progress writes s to stderr, clearing a running spinner's line first.
func progress(s string) {
	spinnerMu.Lock()
	defer spinnerMu.Unlock()
	w := ErrWriter()
	if active != nil {
		_, _ = io.WriteString(w, "\r\x1b[K")
	}
	_, _ = io.WriteString(w, s)
}

// Spinner animates a message on stderr while something runs.
type Spinner struct {
	message string
	done    chan struct{}
	stopped sync.WaitGroup
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// StartSpinner starts a spinner on stderr with the given message. In quiet
// mode it does nothing; when stderr is not a terminal it prints the
// message once instead of animating (no \r junk in logs and pipes).
func StartSpinner(message string) *Spinner {
	if IsQuiet() {
		return &Spinner{}
	}
	if !isTerminal(ErrWriter()) {
		Info("%s", message)
		return &Spinner{}
	}
	s := &Spinner{message: message, done: make(chan struct{})}
	spinnerMu.Lock()
	active = s
	spinnerMu.Unlock()
	s.stopped.Add(1)
	go s.run()
	return s
}

func (s *Spinner) run() {
	defer s.stopped.Done()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for i := 0; ; i++ {
		spinnerMu.Lock()
		_, _ = io.WriteString(ErrWriter(), "\r\x1b[K"+style("36", spinnerFrames[i%len(spinnerFrames)])+" "+s.message)
		spinnerMu.Unlock()
		select {
		case <-s.done:
			return
		case <-tick.C:
		}
	}
}

// Stop stops the spinner without a message (the caller reports the
// outcome, e.g. by returning an error).
func (s *Spinner) Stop() {
	if s.done == nil {
		return
	}
	close(s.done)
	s.stopped.Wait()
	s.done = nil
	spinnerMu.Lock()
	_, _ = io.WriteString(ErrWriter(), "\r\x1b[K")
	active = nil
	spinnerMu.Unlock()
}

// Success stops the spinner with a success message.
func (s *Spinner) Success(message string) {
	if s.done != nil {
		s.Stop()
		Success("%s", message)
	}
}

// Fail stops the spinner with a failure message.
func (s *Spinner) Fail(message string) {
	if s.done != nil {
		s.Stop()
		Error("%s", message)
	}
}
