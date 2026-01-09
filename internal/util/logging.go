package util

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
)

// LogLevel represents the severity of a log message.
// Kept for backwards compatibility.
type LogLevel int

const (
	// LevelDebug is for detailed debugging information.
	LevelDebug LogLevel = iota
	// LevelInfo is for general operational information.
	LevelInfo
	// LevelWarn is for warning messages.
	LevelWarn
	// LevelError is for error messages.
	LevelError
)

// String returns the string representation of a log level.
func (l LogLevel) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// toSlogLevel converts our LogLevel to slog.Level.
func (l LogLevel) toSlogLevel() slog.Level {
	switch l {
	case LevelDebug:
		return slog.LevelDebug
	case LevelInfo:
		return slog.LevelInfo
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

var (
	mu       sync.RWMutex
	logLevel = new(slog.LevelVar)
	logger   *slog.Logger
)

func init() {
	// Initialize with a text handler writing to stderr
	logLevel.Set(slog.LevelInfo)
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Simplify the output format to match the old logger
			if a.Key == slog.TimeKey {
				// Format time as HH:MM:SS
				if t, ok := a.Value.Any().(interface{ Format(string) string }); ok {
					return slog.String(slog.TimeKey, t.Format("15:04:05"))
				}
			}
			return a
		},
	})
	logger = slog.New(handler)
}

// Logger wraps slog for backwards compatibility.
// Deprecated: Use slog directly for new code.
type Logger struct {
	mu      sync.Mutex
	out     io.Writer
	level   LogLevel
	verbose bool
	slogger *slog.Logger
}

// DefaultLogger is the default logger instance.
// Deprecated: Use package-level functions or slog directly.
var DefaultLogger = &Logger{
	out:     os.Stderr,
	level:   LevelInfo,
	slogger: logger,
}

// NewLogger creates a new logger.
// Deprecated: Use slog.New() for new code.
func NewLogger(out io.Writer, level LogLevel) *Logger {
	levelVar := new(slog.LevelVar)
	levelVar.Set(level.toSlogLevel())

	handler := slog.NewTextHandler(out, &slog.HandlerOptions{
		Level: levelVar,
	})

	return &Logger{
		out:     out,
		level:   level,
		slogger: slog.New(handler),
	}
}

// SetVerbose enables or disables verbose output.
func (l *Logger) SetVerbose(verbose bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.verbose = verbose
	if verbose {
		l.level = LevelDebug
	} else {
		l.level = LevelInfo
	}
}

// SetLevel sets the minimum log level.
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// Debug logs a debug message.
func (l *Logger) Debug(format string, args ...interface{}) {
	if l.slogger != nil {
		l.slogger.Debug(fmt.Sprintf(format, args...))
	}
}

// Info logs an info message.
func (l *Logger) Info(format string, args ...interface{}) {
	if l.slogger != nil {
		l.slogger.Info(fmt.Sprintf(format, args...))
	}
}

// Warn logs a warning message.
func (l *Logger) Warn(format string, args ...interface{}) {
	if l.slogger != nil {
		l.slogger.Warn(fmt.Sprintf(format, args...))
	}
}

// Error logs an error message.
func (l *Logger) Error(format string, args ...interface{}) {
	if l.slogger != nil {
		l.slogger.Error(fmt.Sprintf(format, args...))
	}
}

// Package-level convenience functions

// Debug logs a debug message using the default logger.
func Debug(format string, args ...interface{}) {
	mu.RLock()
	defer mu.RUnlock()
	logger.Debug(fmt.Sprintf(format, args...))
}

// Info logs an info message using the default logger.
func Info(format string, args ...interface{}) {
	mu.RLock()
	defer mu.RUnlock()
	logger.Info(fmt.Sprintf(format, args...))
}

// Warn logs a warning message using the default logger.
func Warn(format string, args ...interface{}) {
	mu.RLock()
	defer mu.RUnlock()
	logger.Warn(fmt.Sprintf(format, args...))
}

// Error logs an error message using the default logger.
func Error(format string, args ...interface{}) {
	mu.RLock()
	defer mu.RUnlock()
	logger.Error(fmt.Sprintf(format, args...))
}

// SetVerbose enables or disables verbose output on the default logger.
func SetVerbose(verbose bool) {
	mu.Lock()
	defer mu.Unlock()
	if verbose {
		logLevel.Set(slog.LevelDebug)
	} else {
		logLevel.Set(slog.LevelInfo)
	}
	DefaultLogger.SetVerbose(verbose)
}

// Slog returns the underlying slog.Logger for structured logging.
// Use this for new code that wants structured key-value logging.
func Slog() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return logger
}

// WithContext returns a logger with context for structured logging.
func WithContext(ctx context.Context) *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return logger
}

// With returns a logger with additional attributes.
func With(args ...any) *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return logger.With(args...)
}
