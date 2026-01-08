package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// Spinner provides an animated progress indicator.
type Spinner struct {
	message  string
	writer   io.Writer
	frames   []string
	interval time.Duration
	active   bool
	stopCh   chan struct{}
	doneCh   chan struct{}
	mu       sync.Mutex
	color    *ColorConfig
	isTTY    bool
}

// SpinnerOption configures spinner behavior.
type SpinnerOption func(*Spinner)

// WithSpinnerFrames sets custom spinner frames.
func WithSpinnerFrames(frames []string) SpinnerOption {
	return func(s *Spinner) {
		s.frames = frames
	}
}

// WithSpinnerInterval sets the animation interval.
func WithSpinnerInterval(d time.Duration) SpinnerOption {
	return func(s *Spinner) {
		s.interval = d
	}
}

// NewSpinner creates a new spinner with a message.
func NewSpinner(message string, opts ...SpinnerOption) *Spinner {
	writer := os.Stdout
	isTTY := term.IsTerminal(int(writer.Fd()))

	s := &Spinner{
		message:  message,
		writer:   writer,
		frames:   Symbols.Spinner,
		interval: 80 * time.Millisecond,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		color:    Color(),
		isTTY:    isTTY,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Start begins the spinner animation.
func (s *Spinner) Start() {
	s.mu.Lock()
	if s.active {
		s.mu.Unlock()
		return
	}
	s.active = true
	s.mu.Unlock()

	// For non-TTY, just print the message once
	if !s.isTTY {
		fmt.Fprintf(s.writer, "%s %s\n", Symbols.Bullet, s.message)
		return
	}

	go func() {
		defer close(s.doneCh)
		frame := 0
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-s.stopCh:
				s.clearLine()
				return
			case <-ticker.C:
				s.mu.Lock()
				msg := s.message
				s.mu.Unlock()

				// Clear line and print spinner
				s.clearLine()
				fmt.Fprintf(s.writer, "\r%s %s", s.color.Info(s.frames[frame]), msg)

				frame = (frame + 1) % len(s.frames)
			}
		}
	}()
}

// Stop stops the spinner.
func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.active = false
	s.mu.Unlock()

	if s.isTTY {
		close(s.stopCh)
		<-s.doneCh
	}
}

// StopWithSuccess stops the spinner and shows success message.
func (s *Spinner) StopWithSuccess(message string) {
	s.Stop()
	if s.isTTY {
		fmt.Fprintf(s.writer, "\r%s %s\n", s.color.Success(Symbols.Success), message)
	} else {
		fmt.Fprintf(s.writer, "%s %s\n", Symbols.Success, message)
	}
}

// StopWithError stops the spinner and shows error message.
func (s *Spinner) StopWithError(message string) {
	s.Stop()
	if s.isTTY {
		fmt.Fprintf(s.writer, "\r%s %s\n", s.color.Error(Symbols.Error), message)
	} else {
		fmt.Fprintf(s.writer, "%s %s\n", Symbols.Error, message)
	}
}

// StopWithWarning stops the spinner and shows warning message.
func (s *Spinner) StopWithWarning(message string) {
	s.Stop()
	if s.isTTY {
		fmt.Fprintf(s.writer, "\r%s %s\n", s.color.Warning(Symbols.Warning), message)
	} else {
		fmt.Fprintf(s.writer, "%s %s\n", Symbols.Warning, message)
	}
}

// Update changes the spinner message.
func (s *Spinner) Update(message string) {
	s.mu.Lock()
	s.message = message
	s.mu.Unlock()
}

// clearLine clears the current line.
func (s *Spinner) clearLine() {
	if s.isTTY {
		fmt.Fprint(s.writer, "\r\033[K")
	}
}

// Progress provides a simple progress indicator for multi-step operations.
type Progress struct {
	writer   io.Writer
	color    *ColorConfig
	isTTY    bool
	steps    []string
	current  int
	mu       sync.Mutex
}

// NewProgress creates a new progress indicator.
func NewProgress(steps []string) *Progress {
	writer := os.Stdout
	isTTY := term.IsTerminal(int(writer.Fd()))

	return &Progress{
		writer:  writer,
		color:   Color(),
		isTTY:   isTTY,
		steps:   steps,
		current: -1,
	}
}

// Start begins showing progress for the first step.
func (p *Progress) Start() {
	if len(p.steps) > 0 {
		p.Next()
	}
}

// Next moves to the next step.
func (p *Progress) Next() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.current++
	if p.current < len(p.steps) {
		step := p.steps[p.current]
		progress := fmt.Sprintf("[%d/%d]", p.current+1, len(p.steps))
		fmt.Fprintf(p.writer, "%s %s\n", p.color.Dim(progress), step)
	}
}

// Complete marks all steps as complete.
func (p *Progress) Complete() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := p.current + 1; i < len(p.steps); i++ {
		step := p.steps[i]
		progress := fmt.Sprintf("[%d/%d]", i+1, len(p.steps))
		fmt.Fprintf(p.writer, "%s %s\n", p.color.Dim(progress), step)
	}
}

// Pipeline provides a visual pipeline progress indicator.
type Pipeline struct {
	writer   io.Writer
	color    *ColorConfig
	isTTY    bool
	stages   []string
	current  int
	spinner  *Spinner
	mu       sync.Mutex
}

// NewPipeline creates a new pipeline progress indicator.
func NewPipeline(stages []string) *Pipeline {
	writer := os.Stdout
	isTTY := term.IsTerminal(int(writer.Fd()))

	return &Pipeline{
		writer:  writer,
		color:   Color(),
		isTTY:   isTTY,
		stages:  stages,
		current: -1,
	}
}

// Start begins the pipeline and shows all stages.
func (p *Pipeline) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Show pipeline overview
	stageNames := make([]string, len(p.stages))
	for i, s := range p.stages {
		stageNames[i] = p.color.Dim(s)
	}
	fmt.Fprintf(p.writer, "%s\n\n", strings.Join(stageNames, p.color.Dim(" → ")))
}

// Stage moves to a specific stage.
func (p *Pipeline) Stage(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Stop any running spinner
	if p.spinner != nil {
		p.spinner.Stop()
		p.spinner = nil
	}

	// Mark previous stage complete
	if p.current >= 0 && p.current < len(p.stages) {
		fmt.Fprintf(p.writer, "%s %s\n", p.color.Success(Symbols.Success), p.stages[p.current])
	}

	// Find and start new stage
	for i, s := range p.stages {
		if s == name {
			p.current = i
			p.spinner = NewSpinner(name)
			p.spinner.Start()
			return
		}
	}
}

// Complete finishes the pipeline successfully.
func (p *Pipeline) Complete() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.spinner != nil {
		p.spinner.Stop()
		p.spinner = nil
	}

	if p.current >= 0 && p.current < len(p.stages) {
		fmt.Fprintf(p.writer, "%s %s\n", p.color.Success(Symbols.Success), p.stages[p.current])
	}
}

// Fail marks the pipeline as failed.
func (p *Pipeline) Fail(message string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.spinner != nil {
		p.spinner.StopWithError(message)
		p.spinner = nil
	} else {
		fmt.Fprintf(p.writer, "%s %s\n", p.color.Error(Symbols.Error), message)
	}
}
