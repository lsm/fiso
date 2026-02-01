package observability

import (
	"log/slog"
	"testing"
)

func TestNewLogger(t *testing.T) {
	logger := NewLogger("test-component", slog.LevelInfo)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	// Verify it can log without panicking
	logger.Info("test message", "key", "value")
}

func TestNewLogger_DebugLevel(t *testing.T) {
	logger := NewLogger("debug-component", slog.LevelDebug)
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
	logger.Debug("debug message")
}
