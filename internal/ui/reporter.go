package ui

import "github.com/griffithind/dcx/internal/report"

// terminalReporter routes library progress messages to the terminal,
// honouring quiet mode the same way the rest of the ui package does.
type terminalReporter struct{}

func (terminalReporter) Infof(format string, args ...any)  { Info(format, args...) }
func (terminalReporter) Warnf(format string, args ...any)  { Warning(format, args...) }
func (terminalReporter) Errorf(format string, args ...any) { Error(format, args...) }

// Reporter returns a report.Reporter that prints to the terminal.
func Reporter() report.Reporter { return terminalReporter{} }
