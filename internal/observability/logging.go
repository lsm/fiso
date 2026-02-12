package observability

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger creates a structured JSON logger for Fiso components.
func NewLogger(component string, level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	return slog.New(handler).With("component", component)
}

// ParseLogLevel parses a log level string into slog.Level.
// Accepts: debug, info, warn, error (case-insensitive).
// Returns LevelInfo if the input is invalid or empty.
func ParseLogLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// GetLogLevel returns the effective log level from CLI flag and environment variable.
// CLI flag takes precedence over FISO_LOG_LEVEL environment variable.
func GetLogLevel(flagLevel string) slog.Level {
	if flagLevel != "" {
		return ParseLogLevel(flagLevel)
	}
	if envLevel := os.Getenv("FISO_LOG_LEVEL"); envLevel != "" {
		return ParseLogLevel(envLevel)
	}
	return slog.LevelInfo
}
