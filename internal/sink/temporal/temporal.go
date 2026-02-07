package temporal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/ext"
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

// ParamConfig defines a single typed workflow parameter extracted from the event.
type ParamConfig struct {
	Expr string // CEL expression to extract the value from event data
}

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
	Params         []ParamConfig // Typed workflow parameters (when set, replaces raw bytes)
}

// Sink delivers events by starting or signalling Temporal workflows.
type Sink struct {
	client        WorkflowClient
	config        Config
	timeout       time.Duration
	paramPrograms []cel.Program // Compiled CEL programs for typed params
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

	// Compile CEL programs for typed params
	var paramPrograms []cel.Program
	if len(cfg.Params) > 0 {
		env, err := cel.NewEnv(
			cel.Variable("data", cel.DynType),
			ext.Strings(),
			ext.Encoders(),
			ext.Math(),
		)
		if err != nil {
			return nil, fmt.Errorf("cel env: %w", err)
		}

		paramPrograms = make([]cel.Program, len(cfg.Params))
		for i, param := range cfg.Params {
			ast, issues := env.Compile(param.Expr)
			if issues != nil && issues.Err() != nil {
				return nil, fmt.Errorf("compile param[%d] %q: %w", i, param.Expr, issues.Err())
			}
			prg, err := env.Program(ast)
			if err != nil {
				return nil, fmt.Errorf("program param[%d]: %w", i, err)
			}
			paramPrograms[i] = prg
		}
	}

	return &Sink{
		client:        client,
		config:        cfg,
		timeout:       timeout,
		paramPrograms: paramPrograms,
	}, nil
}

// Deliver sends an event to Temporal as a workflow start or signal.
func (s *Sink) Deliver(ctx context.Context, event []byte, headers map[string]string) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	workflowID := s.resolveWorkflowID(event)

	// Determine workflow arguments: typed params or raw bytes
	var args []interface{}
	if len(s.paramPrograms) > 0 {
		// Parse event data for CEL evaluation
		var data map[string]interface{}
		if err := json.Unmarshal(event, &data); err != nil {
			return fmt.Errorf("parse event for params: %w", err)
		}
		activation := map[string]interface{}{"data": data}

		// Evaluate each param expression
		args = make([]interface{}, len(s.paramPrograms))
		for i, prg := range s.paramPrograms {
			out, _, err := prg.Eval(activation)
			if err != nil {
				return fmt.Errorf("eval param[%d]: %w", i, err)
			}
			args[i] = toNative(out)
		}
	} else {
		// Legacy: pass raw event bytes
		args = []interface{}{event}
	}

	switch s.config.Mode {
	case ModeStart:
		opts := StartWorkflowOptions{
			ID:        workflowID,
			TaskQueue: s.config.TaskQueue,
		}
		_, err := s.client.ExecuteWorkflow(ctx, opts, s.config.WorkflowType, args...)
		if err != nil {
			return fmt.Errorf("start workflow %s: %w", workflowID, err)
		}
		return nil

	case ModeSignal:
		// For signals, pass args as a single value (first arg) or the whole slice
		var signalArg interface{}
		if len(args) == 1 {
			signalArg = args[0]
		} else {
			signalArg = args
		}
		err := s.client.SignalWorkflow(ctx, workflowID, "", s.config.SignalName, signalArg)
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

// toNative recursively converts CEL ref.Val types to native Go types
// that can be serialized by the Temporal SDK.
func toNative(val interface{}) interface{} {
	// Handle null values first
	if _, ok := val.(types.Null); ok {
		return nil
	}

	switch v := val.(type) {
	case traits.Mapper:
		it := v.Iterator()
		m := make(map[string]interface{})
		for it.HasNext() == types.True {
			key := it.Next()
			value := v.Get(key)
			m[fmt.Sprint(key.Value())] = toNative(value)
		}
		return m
	case traits.Lister:
		it := v.Iterator()
		var list []interface{}
		for it.HasNext() == types.True {
			elem := it.Next()
			list = append(list, toNative(elem))
		}
		return list
	default:
		if rv, ok := val.(types.Int); ok {
			return int64(rv)
		}
		if rv, ok := val.(types.Double); ok {
			return float64(rv)
		}
		if rv, ok := val.(types.String); ok {
			return string(rv)
		}
		if rv, ok := val.(types.Bool); ok {
			return bool(rv)
		}
		// Fall back to Value() for other ref.Val types
		if rv, ok := val.(interface{ Value() interface{} }); ok {
			return rv.Value()
		}
		return val
	}
}
