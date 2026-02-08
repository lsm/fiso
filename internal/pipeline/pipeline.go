package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/ext"
	"github.com/lsm/fiso/internal/dlq"
	"github.com/lsm/fiso/internal/interceptor"
	"github.com/lsm/fiso/internal/sink"
	"github.com/lsm/fiso/internal/source"
	"github.com/lsm/fiso/internal/transform"
)

// CloudEventsOverrides allows customizing CloudEvent envelope fields per CloudEvents v1.0 spec.
// All fields support CEL expressions evaluated against the ORIGINAL input event (before
// any transformations). This ensures CloudEvent metadata reflects the source event
// characteristics, not internal processing artifacts.
//
// CEL expressions:
//
//	id: 'data.eventId + "-" + data.CTN'               // Combine fields for idempotency
//	type: 'data.amount > 1000 ? "high-value" : "standard"'  // Conditional type
//	source: '"service-" + data.region'                // Dynamic source
//	subject: 'data.customerId'                        // Extract field
//	data: 'data.payload'                              // Use specific nested field as data
//	datacontenttype: '"application/json"'             // Static content type
//	dataschema: '"https://example.com/schemas/v1/" + data.type + ".json"'  // Dynamic schema
//
// Literal values (non-CEL):
//
//	source: "my-service"    // Static string
//	type: "order.created"   // Static type
type CloudEventsOverrides struct {
	ID              string // CloudEvent ID (for idempotency/deduplication)
	Source          string // CloudEvent source
	Type            string // CloudEvent type
	Subject         string // CloudEvent subject (optional)
	Data            string // CloudEvent data (if empty, uses transformed payload)
	DataContentType string // CloudEvent datacontenttype (optional, default: application/json)
	DataSchema      string // CloudEvent dataschema (optional)
}

// Config holds pipeline configuration.
type Config struct {
	FlowName        string
	EventType       string // CloudEvent type (e.g., "order.created")
	PropagateErrors bool   // When true, return processing errors to the source handler.
	CloudEvents     *CloudEventsOverrides
}

// Pipeline orchestrates the source → transform → interceptors → sink flow.
type Pipeline struct {
	config       Config
	source       source.Source
	transformer  transform.Transformer
	interceptors *interceptor.Chain
	sink         sink.Sink
	dlq          *dlq.Handler
	logger       *slog.Logger
	// Compiled CEL programs for CloudEvent overrides (nil if using JSONPath)
	ceIDProgram              cel.Program
	ceSourceProgram          cel.Program
	ceTypeProgram            cel.Program
	ceSubjectProgram         cel.Program
	ceDataProgram            cel.Program
	ceDataContentTypeProgram cel.Program
	ceDataSchemaProgram      cel.Program
}

// New creates a new Pipeline. If transformer is nil, events pass through untransformed.
// If chain is nil, no interceptors are applied.
func New(cfg Config, src source.Source, tr transform.Transformer, sk sink.Sink, dlqHandler *dlq.Handler, chain *interceptor.Chain) *Pipeline {
	p := &Pipeline{
		config:       cfg,
		source:       src,
		transformer:  tr,
		interceptors: chain,
		sink:         sk,
		dlq:          dlqHandler,
		logger:       slog.Default(),
	}

	// Compile CEL programs for CloudEvent overrides (if they're CEL expressions, not JSONPath)
	if cfg.CloudEvents != nil {
		p.ceIDProgram, _ = compileCELExpression(cfg.CloudEvents.ID)
		p.ceSourceProgram, _ = compileCELExpression(cfg.CloudEvents.Source)
		p.ceTypeProgram, _ = compileCELExpression(cfg.CloudEvents.Type)
		p.ceSubjectProgram, _ = compileCELExpression(cfg.CloudEvents.Subject)
		p.ceDataProgram, _ = compileCELExpression(cfg.CloudEvents.Data)
		p.ceDataContentTypeProgram, _ = compileCELExpression(cfg.CloudEvents.DataContentType)
		p.ceDataSchemaProgram, _ = compileCELExpression(cfg.CloudEvents.DataSchema)
	}

	return p
}

// Run starts the pipeline. Blocks until ctx is cancelled.
func (p *Pipeline) Run(ctx context.Context) error {
	p.logger.Info("starting pipeline", "flow", p.config.FlowName)

	return p.source.Start(ctx, func(ctx context.Context, evt source.Event) error {
		if err := p.processEvent(ctx, evt); err != nil {
			p.logger.Error("event processing failed, sending to DLQ",
				"flow", p.config.FlowName,
				"topic", evt.Topic,
				"offset", evt.Offset,
				"error", err,
			)
			if p.config.PropagateErrors {
				return err
			}
		}
		return nil
	})
}

func (p *Pipeline) processEvent(ctx context.Context, evt source.Event) error {
	originalPayload := evt.Value // preserve for CE field resolution
	payload := evt.Value

	// Transform
	if p.transformer != nil {
		transformed, err := p.transformer.Transform(ctx, payload)
		if err != nil {
			p.sendToDLQ(ctx, evt, "TRANSFORM_FAILED", err.Error())
			return err
		}
		payload = transformed
	}

	// Run interceptors
	if p.interceptors != nil && p.interceptors.Len() > 0 {
		req := &interceptor.Request{
			Payload:   payload,
			Headers:   evt.Headers,
			Direction: interceptor.Inbound,
		}
		result, err := p.interceptors.Process(ctx, req)
		if err != nil {
			p.sendToDLQ(ctx, evt, "INTERCEPTOR_FAILED", err.Error())
			return err
		}
		payload = result.Payload
	}

	// Wrap in CloudEvent (skip if already in CE format)
	var wrapped []byte
	var err error
	if isCloudEvent(payload) {
		// Already a CloudEvent, pass through (optionally apply overrides)
		wrapped, err = p.passOrMergeCloudEvent(payload, originalPayload)
	} else {
		wrapped, err = p.wrapCloudEvent(payload, originalPayload)
	}
	if err != nil {
		p.sendToDLQ(ctx, evt, "CLOUDEVENT_WRAP_FAILED", err.Error())
		return err
	}

	// Deliver to sink
	headers := map[string]string{
		"Content-Type": "application/cloudevents+json",
	}
	if err := p.sink.Deliver(ctx, wrapped, headers); err != nil {
		p.sendToDLQ(ctx, evt, "SINK_DELIVERY_FAILED", err.Error())
		return err
	}

	return nil
}

func (p *Pipeline) wrapCloudEvent(data, originalInput []byte) ([]byte, error) {
	eventType := p.config.EventType
	if eventType == "" {
		eventType = "fiso.event"
	}
	ceSource := "fiso-flow/" + p.config.FlowName
	ceDataContentType := "application/json"
	var ceID string
	var ceSubject string
	var ceDataSchema string
	var ceData []byte

	// Apply CloudEvents overrides
	// IMPORTANT: All overrides are resolved against the ORIGINAL input (before transforms),
	// not the transformed output. This ensures CloudEvent metadata reflects the source event.
	//
	// Supports CEL expressions:
	//   CEL:     'data.eventId + "-" + data.CTN'
	//   Literal: "my-static-value"
	if p.config.CloudEvents != nil {
		var parsed map[string]interface{}
		if err := json.Unmarshal(originalInput, &parsed); err != nil {
			// If we can't parse original input, use overrides as literals only
			parsed = nil
		}

		// Resolve ID
		if p.config.CloudEvents.ID != "" {
			if p.ceIDProgram != nil {
				ceID = evaluateCELExpression(p.ceIDProgram, parsed)
			} else {
				ceID = p.config.CloudEvents.ID
			}
		}

		// Resolve Source
		if p.config.CloudEvents.Source != "" {
			if p.ceSourceProgram != nil {
				ceSource = evaluateCELExpression(p.ceSourceProgram, parsed)
			} else {
				ceSource = p.config.CloudEvents.Source
			}
		}

		// Resolve Type
		if p.config.CloudEvents.Type != "" {
			if p.ceTypeProgram != nil {
				eventType = evaluateCELExpression(p.ceTypeProgram, parsed)
			} else {
				eventType = p.config.CloudEvents.Type
			}
		}

		// Resolve Subject
		if p.config.CloudEvents.Subject != "" {
			if p.ceSubjectProgram != nil {
				ceSubject = evaluateCELExpression(p.ceSubjectProgram, parsed)
			} else {
				ceSubject = p.config.CloudEvents.Subject
			}
		}

		// Resolve Data (if specified, otherwise use transformed payload)
		if p.config.CloudEvents.Data != "" {
			var dataValue interface{}
			if p.ceDataProgram != nil {
				dataValue = evaluateCELValue(p.ceDataProgram, parsed)
			} else {
				dataValue = p.config.CloudEvents.Data
			}
			// Marshal the extracted value to JSON
			marshaled, err := json.Marshal(dataValue)
			if err == nil {
				ceData = marshaled
			} else {
				// Fallback to using the data field expression as a literal string
				ceData = []byte(`"` + p.config.CloudEvents.Data + `"`)
			}
		}

		// Resolve DataContentType
		if p.config.CloudEvents.DataContentType != "" {
			if p.ceDataContentTypeProgram != nil {
				ceDataContentType = evaluateCELExpression(p.ceDataContentTypeProgram, parsed)
			} else {
				ceDataContentType = p.config.CloudEvents.DataContentType
			}
		}

		// Resolve DataSchema
		if p.config.CloudEvents.DataSchema != "" {
			if p.ceDataSchemaProgram != nil {
				ceDataSchema = evaluateCELExpression(p.ceDataSchemaProgram, parsed)
			} else {
				ceDataSchema = p.config.CloudEvents.DataSchema
			}
		}
	}

	// Use transformed payload if Data not explicitly set
	if ceData == nil {
		ceData = data
	}

	// Create CloudEvent using SDK
	event := cloudevents.NewEvent()
	if ceID != "" {
		event.SetID(ceID)
	}
	event.SetSource(ceSource)
	event.SetType(eventType)
	event.SetTime(time.Now().UTC())
	if ceSubject != "" {
		event.SetSubject(ceSubject)
	}
	if ceDataSchema != "" {
		event.SetDataSchema(ceDataSchema)
	}

	// Set data with appropriate content type
	if err := event.SetData(ceDataContentType, json.RawMessage(ceData)); err != nil {
		return nil, fmt.Errorf("set event data: %w", err)
	}

	return json.Marshal(event)
}

// isCloudEvent checks if the payload is already in CloudEvents format.
// A valid CloudEvent must have specversion, type, and source fields.
func isCloudEvent(data []byte) bool {
	var ce struct {
		SpecVersion string `json:"specversion"`
		Type        string `json:"type"`
		Source      string `json:"source"`
	}
	if err := json.Unmarshal(data, &ce); err != nil {
		return false
	}
	// CloudEvents spec requires specversion, type, and source
	return ce.SpecVersion != "" && ce.Type != "" && ce.Source != ""
}

// passOrMergeCloudEvent handles events that are already in CloudEvent format.
// If no overrides are configured, it passes through unchanged.
// If overrides are configured, it merges them into the existing CloudEvent.
func (p *Pipeline) passOrMergeCloudEvent(data, originalInput []byte) ([]byte, error) {
	// If no overrides configured, pass through unchanged
	if p.config.CloudEvents == nil {
		return data, nil
	}

	// Parse the existing CloudEvent
	var existingCE map[string]interface{}
	if err := json.Unmarshal(data, &existingCE); err != nil {
		return nil, fmt.Errorf("parse existing cloudevent: %w", err)
	}

	// Parse original input for CEL evaluation (use the CE's data field if available)
	var parsed map[string]interface{}
	if ceData, ok := existingCE["data"]; ok {
		if dataMap, ok := ceData.(map[string]interface{}); ok {
			parsed = dataMap
		}
	}
	if parsed == nil {
		// Fallback: try to parse originalInput directly
		_ = json.Unmarshal(originalInput, &parsed)
	}

	// Apply overrides
	if p.config.CloudEvents.ID != "" {
		if p.ceIDProgram != nil {
			if id := evaluateCELExpression(p.ceIDProgram, parsed); id != "" {
				existingCE["id"] = id
			}
		} else {
			existingCE["id"] = p.config.CloudEvents.ID
		}
	}

	if p.config.CloudEvents.Source != "" {
		if p.ceSourceProgram != nil {
			if source := evaluateCELExpression(p.ceSourceProgram, parsed); source != "" {
				existingCE["source"] = source
			}
		} else {
			existingCE["source"] = p.config.CloudEvents.Source
		}
	}

	if p.config.CloudEvents.Type != "" {
		if p.ceTypeProgram != nil {
			if t := evaluateCELExpression(p.ceTypeProgram, parsed); t != "" {
				existingCE["type"] = t
			}
		} else {
			existingCE["type"] = p.config.CloudEvents.Type
		}
	}

	if p.config.CloudEvents.Subject != "" {
		if p.ceSubjectProgram != nil {
			if subj := evaluateCELExpression(p.ceSubjectProgram, parsed); subj != "" {
				existingCE["subject"] = subj
			}
		} else {
			existingCE["subject"] = p.config.CloudEvents.Subject
		}
	}

	if p.config.CloudEvents.Data != "" {
		if p.ceDataProgram != nil {
			if dataVal := evaluateCELValue(p.ceDataProgram, parsed); dataVal != nil {
				existingCE["data"] = dataVal
			}
		} else {
			existingCE["data"] = p.config.CloudEvents.Data
		}
	}

	if p.config.CloudEvents.DataContentType != "" {
		if p.ceDataContentTypeProgram != nil {
			if ct := evaluateCELExpression(p.ceDataContentTypeProgram, parsed); ct != "" {
				existingCE["datacontenttype"] = ct
			}
		} else {
			existingCE["datacontenttype"] = p.config.CloudEvents.DataContentType
		}
	}

	if p.config.CloudEvents.DataSchema != "" {
		if p.ceDataSchemaProgram != nil {
			if ds := evaluateCELExpression(p.ceDataSchemaProgram, parsed); ds != "" {
				existingCE["dataschema"] = ds
			}
		} else {
			existingCE["dataschema"] = p.config.CloudEvents.DataSchema
		}
	}

	return json.Marshal(existingCE)
}

func (p *Pipeline) sendToDLQ(ctx context.Context, evt source.Event, code, message string) {
	info := dlq.FailureInfo{
		OriginalTopic: evt.Topic,
		ErrorCode:     code,
		ErrorMessage:  message,
		FlowName:      p.config.FlowName,
	}
	if err := p.dlq.Send(ctx, evt.Key, evt.Value, info); err != nil {
		p.logger.Error("failed to send to DLQ",
			"flow", p.config.FlowName,
			"error", err,
		)
	}
}

// Shutdown performs graceful shutdown of the pipeline components.
// Closes source, sink, and DLQ in order. Returns all errors joined.
func (p *Pipeline) Shutdown(ctx context.Context) error {
	p.logger.Info("shutting down pipeline", "flow", p.config.FlowName)

	var errs []error

	if err := p.source.Close(); err != nil {
		p.logger.Error("source close error", "flow", p.config.FlowName, "error", err)
		errs = append(errs, fmt.Errorf("source close: %w", err))
	}
	if p.interceptors != nil {
		if err := p.interceptors.Close(); err != nil {
			p.logger.Error("interceptor close error", "flow", p.config.FlowName, "error", err)
			errs = append(errs, fmt.Errorf("interceptor close: %w", err))
		}
	}
	if err := p.sink.Close(); err != nil {
		p.logger.Error("sink close error", "flow", p.config.FlowName, "error", err)
		errs = append(errs, fmt.Errorf("sink close: %w", err))
	}
	if err := p.dlq.Close(); err != nil {
		p.logger.Error("dlq close error", "flow", p.config.FlowName, "error", err)
		errs = append(errs, fmt.Errorf("dlq close: %w", err))
	}

	p.logger.Info("pipeline shutdown complete", "flow", p.config.FlowName)
	return errors.Join(errs...)
}

// compileCELExpression compiles a CEL expression.
// Returns nil if the expression is empty or doesn't contain CEL syntax (treated as literal).
// Simple field access like "data.foo" or complex expressions are both supported.
func compileCELExpression(expr string) (cel.Program, error) {
	if expr == "" {
		return nil, nil
	}

	// Create CEL environment with same variables as transform
	env, err := cel.NewEnv(
		cel.Variable("data", cel.DynType),
		cel.Variable("time", cel.DynType),
		cel.Variable("source", cel.DynType),
		cel.Variable("type", cel.DynType),
		cel.Variable("id", cel.DynType),
		cel.Variable("subject", cel.DynType),
		ext.Strings(),
		ext.Encoders(),
		ext.Math(),
	)
	if err != nil {
		return nil, fmt.Errorf("cel env: %w", err)
	}

	// Compile the expression
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		// If compilation fails, treat as literal value (return nil program)
		return nil, nil
	}

	// Create the program
	prg, err := env.Program(ast)
	if err != nil {
		// If program creation fails, treat as literal value
		return nil, nil
	}

	return prg, nil
}

// evaluateCELExpression evaluates a compiled CEL program against input data.
// Returns empty string if program is nil or evaluation fails.
func evaluateCELExpression(prg cel.Program, inputData map[string]interface{}) string {
	if prg == nil {
		return ""
	}

	// Prepare CEL variables (same structure as transform)
	vars := map[string]interface{}{
		"data": inputData,
		"time": time.Now().Format(time.RFC3339),
	}

	// Evaluate the expression
	out, _, err := prg.Eval(vars)
	if err != nil {
		return "" // Return empty on evaluation error
	}

	// Convert result to string
	if out.Type() == types.StringType {
		return out.Value().(string)
	}
	return fmt.Sprintf("%v", out.Value())
}

// evaluateCELValue evaluates a compiled CEL program against input data.
// Returns the raw value (interface{}) instead of converting to string.
// Returns nil if program is nil or evaluation fails.
func evaluateCELValue(prg cel.Program, inputData map[string]interface{}) interface{} {
	if prg == nil {
		return nil
	}

	// Prepare CEL variables (same structure as transform)
	vars := map[string]interface{}{
		"data": inputData,
		"time": time.Now().Format(time.RFC3339),
	}

	// Evaluate the expression
	out, _, err := prg.Eval(vars)
	if err != nil {
		return nil // Return nil on evaluation error
	}

	// Convert CEL types to native Go types for JSON serialization
	return toNative(out)
}

// toNative recursively converts CEL ref.Val types to native Go types
// that can be serialized to JSON.
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
