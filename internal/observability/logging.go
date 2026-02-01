package observability

import (
	"log/slog"
	"os"
)

// NewLogger creates a structured JSON logger for Fiso components.
func NewLogger(component string, level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	return slog.New(handler).With("component", component)
}
