package observability

import (
	"log/slog"
	"os"
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

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"invalid", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseLogLevel(tt.input)
			if got != tt.expected {
				t.Errorf("ParseLogLevel(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestGetLogLevel(t *testing.T) {
	// Save and restore env var
	origEnv := os.Getenv("FISO_LOG_LEVEL")
	defer os.Setenv("FISO_LOG_LEVEL", origEnv)

	tests := []struct {
		name      string
		flagLevel string
		envLevel  string
		expected  slog.Level
	}{
		{"flag takes precedence", "debug", "error", slog.LevelDebug},
		{"env used when flag empty", "", "warn", slog.LevelWarn},
		{"default when both empty", "", "", slog.LevelInfo},
		{"flag overrides env", "error", "debug", slog.LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("FISO_LOG_LEVEL", tt.envLevel)
			got := GetLogLevel(tt.flagLevel)
			if got != tt.expected {
				t.Errorf("GetLogLevel(%q) = %v, want %v (env=%q)", tt.flagLevel, got, tt.expected, tt.envLevel)
			}
		})
	}
}
