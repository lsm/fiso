package temporal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// WorkflowClient abstracts the Temporal SDK client for testability.
type WorkflowClient interface {
	ExecuteWorkflow(ctx context.Context, options StartWorkflowOptions, workflow string, args ...interface{}) (WorkflowRun, error)
	SignalWorkflow(ctx context.Context, workflowID, runID, signalName string, arg interface{}) error
	Close()
}

// StartWorkflowOptions mirrors Temporal client.StartWorkflowOptions.
type StartWorkflowOptions struct {
	ID        string
	TaskQueue string
}

// WorkflowRun represents a started workflow execution.
type WorkflowRun interface {
	GetID() string
	GetRunID() string
}

// Mode determines how the sink interacts with Temporal.
type Mode string

const (
	ModeStart  Mode = "start"
	ModeSignal Mode = "signal"
)

// Config holds Temporal sink configuration.
type Config struct {
	HostPort       string
	Namespace      string
	TaskQueue      string
	WorkflowType   string
	WorkflowIDExpr string // Template for workflow ID, supports {{.field}} from event JSON
	Mode           Mode
	SignalName     string // Required when Mode == ModeSignal
	Timeout        time.Duration
}

// Sink delivers events by starting or signalling Temporal workflows.
type Sink struct {
	client  WorkflowClient
	config  Config
	timeout time.Duration
}

// NewSink creates a new Temporal sink with the given client.
func NewSink(client WorkflowClient, cfg Config) (*Sink, error) {
	if client == nil {
		return nil, fmt.Errorf("temporal client is required")
	}
	if cfg.TaskQueue == "" {
		return nil, fmt.Errorf("task queue is required")
	}
	if cfg.WorkflowType == "" {
		return nil, fmt.Errorf("workflow type is required")
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeStart
	}
	if cfg.Mode == ModeSignal && cfg.SignalName == "" {
		return nil, fmt.Errorf("signal name is required when mode is 'signal'")
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &Sink{
		client:  client,
		config:  cfg,
		timeout: timeout,
	}, nil
}

// Deliver sends an event to Temporal as a workflow start or signal.
func (s *Sink) Deliver(ctx context.Context, event []byte, headers map[string]string) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	workflowID := s.resolveWorkflowID(event)

	switch s.config.Mode {
	case ModeStart:
		opts := StartWorkflowOptions{
			ID:        workflowID,
			TaskQueue: s.config.TaskQueue,
		}
		_, err := s.client.ExecuteWorkflow(ctx, opts, s.config.WorkflowType, event)
		if err != nil {
			return fmt.Errorf("start workflow %s: %w", workflowID, err)
		}
		return nil

	case ModeSignal:
		err := s.client.SignalWorkflow(ctx, workflowID, "", s.config.SignalName, event)
		if err != nil {
			return fmt.Errorf("signal workflow %s: %w", workflowID, err)
		}
		return nil

	default:
		return fmt.Errorf("unsupported mode: %s", s.config.Mode)
	}
}

// Close shuts down the Temporal client.
func (s *Sink) Close() error {
	s.client.Close()
	return nil
}

// resolveWorkflowID resolves the workflow ID from the template expression.
// Supports {{.field}} syntax to extract fields from JSON event payload.
func (s *Sink) resolveWorkflowID(event []byte) string {
	expr := s.config.WorkflowIDExpr
	if expr == "" {
		return fmt.Sprintf("%s-%d", s.config.WorkflowType, time.Now().UnixNano())
	}

	if !strings.Contains(expr, "{{") {
		return expr
	}

	var data map[string]interface{}
	if err := json.Unmarshal(event, &data); err != nil {
		return expr
	}

	result := expr
	for key, val := range data {
		placeholder := "{{." + key + "}}"
		if strings.Contains(result, placeholder) {
			result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", val))
		}
	}
	return result
}
