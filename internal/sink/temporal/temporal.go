package temporal

import (
	"context"
	"encoding/json"
	"errors"
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

// TLSConfig defines TLS settings for Temporal connections.
type TLSConfig struct {
	Disabled   bool   `yaml:"disabled,omitempty"` // Explicitly disable TLS (for dev/test)
	Enabled    bool   `yaml:"enabled"`
	CAFile     string `yaml:"caFile,omitempty"`
	CertFile   string `yaml:"certFile,omitempty"` // For mTLS
	KeyFile    string `yaml:"keyFile,omitempty"`  // For mTLS
	SkipVerify bool   `yaml:"skipVerify,omitempty"`
}

// AuthConfig defines authentication for Temporal connections.
type AuthConfig struct {
	APIKey    string      `yaml:"apiKey,omitempty"`    // Static API key
	APIKeyEnv string      `yaml:"apiKeyEnv,omitempty"` // Env var name for dynamic API key (enables rotation)
	TokenFile string      `yaml:"tokenFile,omitempty"` // Bearer token read from file per-request
	OIDC      *OIDCConfig `yaml:"oidc,omitempty"`      // OIDC client credentials flow
}

// OIDCConfig defines OIDC client credentials flow for token acquisition.
type OIDCConfig struct {
	TokenURL        string   `yaml:"tokenURL"`                  // Token endpoint (e.g. https://login.microsoftonline.com/{tenantID}/oauth2/v2.0/token)
	ClientID        string   `yaml:"clientID"`                  // OAuth2 client ID
	ClientSecret    string   `yaml:"clientSecret,omitempty"`    // OAuth2 client secret
	ClientSecretEnv string   `yaml:"clientSecretEnv,omitempty"` // Read client secret from env var
	Scopes          []string `yaml:"scopes,omitempty"`          // OAuth2 scopes
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
	TLS            TLSConfig     // Optional TLS configuration
	Auth           AuthConfig    // Optional auth configuration
}

// Validate checks the Temporal configuration for errors.
func (c *Config) Validate() error {
	var errs []error

	// TLS validation
	if c.TLS.CertFile != "" && c.TLS.KeyFile == "" {
		errs = append(errs, errors.New("tls.keyFile is required when certFile is specified"))
	}
	if c.TLS.KeyFile != "" && c.TLS.CertFile == "" {
		errs = append(errs, errors.New("tls.certFile is required when keyFile is specified"))
	}

	// Count configured auth methods — only one is allowed
	authCount := 0
	if c.Auth.APIKey != "" {
		authCount++
	}
	if c.Auth.APIKeyEnv != "" {
		authCount++
	}
	if c.Auth.TokenFile != "" {
		authCount++
	}
	if c.Auth.OIDC != nil {
		authCount++
	}
	if c.TLS.CertFile != "" && c.TLS.KeyFile != "" {
		authCount++
	}
	if authCount > 1 {
		errs = append(errs, errors.New("only one auth method allowed: use one of apiKey, apiKeyEnv, tokenFile, oidc, or mTLS client certificates"))
	}

	// OIDC validation
	if c.Auth.OIDC != nil {
		if c.Auth.OIDC.TokenURL == "" {
			errs = append(errs, errors.New("auth.oidc.tokenURL is required"))
		}
		if c.Auth.OIDC.ClientID == "" {
			errs = append(errs, errors.New("auth.oidc.clientID is required"))
		}
		if c.Auth.OIDC.ClientSecret == "" && c.Auth.OIDC.ClientSecretEnv == "" {
			errs = append(errs, errors.New("auth.oidc requires either clientSecret or clientSecretEnv"))
		}
		if c.Auth.OIDC.ClientSecret != "" && c.Auth.OIDC.ClientSecretEnv != "" {
			errs = append(errs, errors.New("auth.oidc.clientSecret and clientSecretEnv are mutually exclusive"))
		}
	}

	return errors.Join(errs...)
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
// The event is expected to be in CloudEvents format (JSON).
// When no params are configured, the entire CloudEvent is sent as a structured map,
// allowing Java/Kotlin workflows to deserialize it directly.
func (s *Sink) Deliver(ctx context.Context, event []byte, headers map[string]string) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// Parse the CloudEvent for workflow ID resolution and args
	var eventData map[string]interface{}
	if err := json.Unmarshal(event, &eventData); err != nil {
		return fmt.Errorf("parse cloudevent: %w", err)
	}

	workflowID := s.resolveWorkflowID(eventData)

	// Determine workflow arguments: typed params or the whole CloudEvent
	var args []interface{}
	if len(s.paramPrograms) > 0 {
		// CEL activation exposes the CloudEvent structure:
		// - data.specversion, data.type, data.source → CE metadata
		// - data.data.orderId → fields inside CE's data payload
		activation := map[string]interface{}{"data": eventData}

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
		// Send the entire CloudEvent as a structured map.
		// Java/Kotlin workflows can define a CloudEvent input class to receive this.
		args = []interface{}{eventData}
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
// Supports {{.field}} and {{.nested.field}} syntax to extract fields from CloudEvent.
// Examples:
//   - {{.id}} → CloudEvent ID
//   - {{.type}} → CloudEvent type
//   - {{.data.orderId}} → field inside CloudEvent's data payload
func (s *Sink) resolveWorkflowID(eventData map[string]interface{}) string {
	expr := s.config.WorkflowIDExpr
	if expr == "" {
		return fmt.Sprintf("%s-%d", s.config.WorkflowType, time.Now().UnixNano())
	}

	if !strings.Contains(expr, "{{") {
		return expr
	}

	result := expr
	// Find all {{.path}} placeholders and resolve them
	for {
		start := strings.Index(result, "{{.")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "}}")
		if end == -1 {
			break
		}
		end += start + 2

		placeholder := result[start:end]
		path := result[start+3 : end-2] // Extract "field" or "nested.field" from "{{.field}}"

		value := resolveNestedField(eventData, path)
		result = strings.Replace(result, placeholder, value, 1)
	}
	return result
}

// resolveNestedField resolves a dot-separated path in a nested map.
// Examples: "id" → data["id"], "data.orderId" → data["data"]["orderId"]
func resolveNestedField(data map[string]interface{}, path string) string {
	parts := strings.Split(path, ".")
	var current interface{} = data

	for _, part := range parts {
		if m, ok := current.(map[string]interface{}); ok {
			current = m[part]
		} else {
			return "" // Path doesn't exist
		}
	}

	if current == nil {
		return ""
	}
	return fmt.Sprintf("%v", current)
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
