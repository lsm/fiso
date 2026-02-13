package observability

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// NewLogger creates a structured JSON logger for Fiso components.
func NewLogger(component string, level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	return slog.New(handler).With("component", component)
}

// TraceLogger wraps a logger to automatically add trace context from the context.
type TraceLogger struct {
	logger *slog.Logger
}

// NewTraceLogger creates a new TraceLogger that extracts trace_id and span_id from context.
func NewTraceLogger(logger *slog.Logger) *TraceLogger {
	return &TraceLogger{logger: logger}
}

// WithTraceContext returns a logger with trace_id and span_id attributes if a valid span exists in context.
func (l *TraceLogger) WithTraceContext(ctx context.Context) *slog.Logger {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return l.logger
	}

	return l.logger.With(
		"trace_id", span.SpanContext().TraceID().String(),
		"span_id", span.SpanContext().SpanID().String(),
	)
}

// Debug logs at debug level with trace context.
func (l *TraceLogger) Debug(ctx context.Context, msg string, args ...any) {
	l.WithTraceContext(ctx).Debug(msg, args...)
}

// Info logs at info level with trace context.
func (l *TraceLogger) Info(ctx context.Context, msg string, args ...any) {
	l.WithTraceContext(ctx).Info(msg, args...)
}

// Warn logs at warn level with trace context.
func (l *TraceLogger) Warn(ctx context.Context, msg string, args ...any) {
	l.WithTraceContext(ctx).Warn(msg, args...)
}

// Error logs at error level with trace context.
func (l *TraceLogger) Error(ctx context.Context, msg string, args ...any) {
	l.WithTraceContext(ctx).Error(msg, args...)
}

// With returns a new TraceLogger with additional key-value pairs.
func (l *TraceLogger) With(args ...any) *TraceLogger {
	return &TraceLogger{logger: l.logger.With(args...)}
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
