package util

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
)

var (
	mu       sync.RWMutex
	logLevel = new(slog.LevelVar)
	logger   *slog.Logger
)

func init() {
	logLevel.Set(slog.LevelInfo)
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				if t, ok := a.Value.Any().(interface{ Format(string) string }); ok {
					return slog.String(slog.TimeKey, t.Format("15:04:05"))
				}
			}
			return a
		},
	})
	logger = slog.New(handler)
}

// Warn logs a warning message using the default logger.
func Warn(format string, args ...interface{}) {
	mu.RLock()
	defer mu.RUnlock()
	logger.Warn(fmt.Sprintf(format, args...))
}
