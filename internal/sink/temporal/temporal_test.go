package temporal

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// mockWorkflowRun implements WorkflowRun for testing.
type mockWorkflowRun struct {
	id    string
	runID string
}

func (m *mockWorkflowRun) GetID() string    { return m.id }
func (m *mockWorkflowRun) GetRunID() string { return m.runID }

// mockClient implements WorkflowClient for testing.
type mockClient struct {
	executeErr     error
	signalErr      error
	closed         bool
	lastOpts       StartWorkflowOptions
	lastWorkflow   string
	lastArgs       []interface{}
	lastSignalID   string
	lastSignalArg  interface{}
	lastSignalName string
}

func (m *mockClient) ExecuteWorkflow(_ context.Context, opts StartWorkflowOptions, workflow string, args ...interface{}) (WorkflowRun, error) {
	m.lastOpts = opts
	m.lastWorkflow = workflow
	m.lastArgs = args
	if m.executeErr != nil {
		return nil, m.executeErr
	}
	return &mockWorkflowRun{id: opts.ID, runID: "run-123"}, nil
}

func (m *mockClient) SignalWorkflow(_ context.Context, workflowID, _ string, signalName string, arg interface{}) error {
	m.lastSignalID = workflowID
	m.lastSignalName = signalName
	m.lastSignalArg = arg
	return m.signalErr
}

func (m *mockClient) Close() {
	m.closed = true
}

func TestNewSink_Valid(t *testing.T) {
	mc := &mockClient{}
	s, err := NewSink(mc, Config{
		TaskQueue:    "test-queue",
		WorkflowType: "TestWorkflow",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil sink")
	}
}

func TestNewSink_NilClient(t *testing.T) {
	_, err := NewSink(nil, Config{
		TaskQueue:    "test-queue",
		WorkflowType: "TestWorkflow",
	})
	if err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestNewSink_MissingTaskQueue(t *testing.T) {
	_, err := NewSink(&mockClient{}, Config{
		WorkflowType: "TestWorkflow",
	})
	if err == nil {
		t.Fatal("expected error for missing task queue")
	}
}

func TestNewSink_MissingWorkflowType(t *testing.T) {
	_, err := NewSink(&mockClient{}, Config{
		TaskQueue: "test-queue",
	})
	if err == nil {
		t.Fatal("expected error for missing workflow type")
	}
}

func TestNewSink_SignalModeMissingSignalName(t *testing.T) {
	_, err := NewSink(&mockClient{}, Config{
		TaskQueue:    "test-queue",
		WorkflowType: "TestWorkflow",
		Mode:         ModeSignal,
	})
	if err == nil {
		t.Fatal("expected error for missing signal name")
	}
}

func TestDeliver_StartMode(t *testing.T) {
	mc := &mockClient{}
	s, _ := NewSink(mc, Config{
		TaskQueue:      "test-queue",
		WorkflowType:   "TestWorkflow",
		WorkflowIDExpr: "order-{{.orderId}}",
	})

	event, _ := json.Marshal(map[string]interface{}{"orderId": "12345"})
	err := s.Deliver(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mc.lastOpts.ID != "order-12345" {
		t.Errorf("expected workflow ID 'order-12345', got %q", mc.lastOpts.ID)
	}
	if mc.lastOpts.TaskQueue != "test-queue" {
		t.Errorf("expected task queue 'test-queue', got %q", mc.lastOpts.TaskQueue)
	}
	if mc.lastWorkflow != "TestWorkflow" {
		t.Errorf("expected workflow 'TestWorkflow', got %q", mc.lastWorkflow)
	}
}

func TestDeliver_SignalMode(t *testing.T) {
	mc := &mockClient{}
	s, _ := NewSink(mc, Config{
		TaskQueue:      "test-queue",
		WorkflowType:   "TestWorkflow",
		Mode:           ModeSignal,
		SignalName:     "event-signal",
		WorkflowIDExpr: "wf-{{.id}}",
	})

	event, _ := json.Marshal(map[string]interface{}{"id": "abc"})
	err := s.Deliver(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mc.lastSignalID != "wf-abc" {
		t.Errorf("expected signal workflow ID 'wf-abc', got %q", mc.lastSignalID)
	}
	if mc.lastSignalName != "event-signal" {
		t.Errorf("expected signal name 'event-signal', got %q", mc.lastSignalName)
	}
}

func TestDeliver_StartError(t *testing.T) {
	mc := &mockClient{executeErr: errors.New("temporal unavailable")}
	s, _ := NewSink(mc, Config{
		TaskQueue:    "test-queue",
		WorkflowType: "TestWorkflow",
	})

	err := s.Deliver(context.Background(), []byte("{}"), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "temporal unavailable") {
		t.Errorf("expected error to contain 'temporal unavailable', got %q", err.Error())
	}
}

func TestDeliver_SignalError(t *testing.T) {
	mc := &mockClient{signalErr: errors.New("signal failed")}
	s, _ := NewSink(mc, Config{
		TaskQueue:      "test-queue",
		WorkflowType:   "TestWorkflow",
		Mode:           ModeSignal,
		SignalName:     "test-signal",
		WorkflowIDExpr: "static-id",
	})

	err := s.Deliver(context.Background(), []byte("{}"), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "signal failed") {
		t.Errorf("expected error to contain 'signal failed', got %q", err.Error())
	}
}

func TestDeliver_UnsupportedMode(t *testing.T) {
	mc := &mockClient{}
	s := &Sink{
		client:  mc,
		config:  Config{Mode: "invalid", TaskQueue: "q", WorkflowType: "W"},
		timeout: 5 * time.Second,
	}
	err := s.Deliver(context.Background(), []byte("{}"), nil)
	if err == nil {
		t.Fatal("expected error for unsupported mode")
	}
}

func TestResolveWorkflowID_StaticExpr(t *testing.T) {
	s := &Sink{config: Config{WorkflowIDExpr: "static-id"}}
	id := s.resolveWorkflowID(map[string]interface{}{})
	if id != "static-id" {
		t.Errorf("expected 'static-id', got %q", id)
	}
}

func TestResolveWorkflowID_EmptyExpr(t *testing.T) {
	s := &Sink{config: Config{WorkflowType: "TestWF"}}
	id := s.resolveWorkflowID(map[string]interface{}{})
	if !strings.HasPrefix(id, "TestWF-") {
		t.Errorf("expected ID starting with 'TestWF-', got %q", id)
	}
}

func TestResolveWorkflowID_NestedField(t *testing.T) {
	s := &Sink{config: Config{WorkflowIDExpr: "order-{{.data.orderId}}"}}
	eventData := map[string]interface{}{
		"specversion": "1.0",
		"type":        "order.created",
		"source":      "test",
		"data": map[string]interface{}{
			"orderId": "12345",
		},
	}
	id := s.resolveWorkflowID(eventData)
	if id != "order-12345" {
		t.Errorf("expected 'order-12345', got %q", id)
	}
}

func TestResolveWorkflowID_TopLevelField(t *testing.T) {
	s := &Sink{config: Config{WorkflowIDExpr: "wf-{{.id}}-{{.type}}"}}
	eventData := map[string]interface{}{
		"specversion": "1.0",
		"type":        "order.created",
		"source":      "test",
		"id":          "ce-123",
	}
	id := s.resolveWorkflowID(eventData)
	if id != "wf-ce-123-order.created" {
		t.Errorf("expected 'wf-ce-123-order.created', got %q", id)
	}
}

func TestResolveWorkflowID_MissingNestedField(t *testing.T) {
	s := &Sink{config: Config{WorkflowIDExpr: "order-{{.data.missing}}"}}
	eventData := map[string]interface{}{
		"data": map[string]interface{}{
			"orderId": "12345",
		},
	}
	id := s.resolveWorkflowID(eventData)
	if id != "order-" {
		t.Errorf("expected 'order-' (empty for missing field), got %q", id)
	}
}

func TestClose(t *testing.T) {
	mc := &mockClient{}
	s, _ := NewSink(mc, Config{
		TaskQueue:    "test-queue",
		WorkflowType: "TestWorkflow",
	})
	if err := s.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mc.closed {
		t.Error("expected client to be closed")
	}
}

func TestDeliver_TypedParams_StartMode(t *testing.T) {
	mc := &mockClient{}
	s, err := NewSink(mc, Config{
		TaskQueue:      "test-queue",
		WorkflowType:   "TestWorkflow",
		WorkflowIDExpr: "order-{{.orderId}}",
		Params: []ParamConfig{
			{Expr: "data.eventId"},
			{Expr: "data.ctn"},
			{Expr: "data.accountId"},
			{Expr: "data.amount"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error creating sink: %v", err)
	}

	event, _ := json.Marshal(map[string]interface{}{
		"orderId":   "12345",
		"eventId":   "evt-001",
		"ctn":       "9876543210",
		"accountId": "acc-999",
		"amount":    150.50,
	})
	err = s.Deliver(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify typed args were passed
	if len(mc.lastArgs) != 4 {
		t.Fatalf("expected 4 args, got %d", len(mc.lastArgs))
	}
	if mc.lastArgs[0] != "evt-001" {
		t.Errorf("expected first arg 'evt-001', got %v", mc.lastArgs[0])
	}
	if mc.lastArgs[1] != "9876543210" {
		t.Errorf("expected second arg '9876543210', got %v", mc.lastArgs[1])
	}
	if mc.lastArgs[2] != "acc-999" {
		t.Errorf("expected third arg 'acc-999', got %v", mc.lastArgs[2])
	}
	if mc.lastArgs[3] != 150.50 {
		t.Errorf("expected fourth arg 150.50, got %v", mc.lastArgs[3])
	}
}

func TestDeliver_TypedParams_SignalMode(t *testing.T) {
	mc := &mockClient{}
	s, err := NewSink(mc, Config{
		TaskQueue:      "test-queue",
		WorkflowType:   "TestWorkflow",
		Mode:           ModeSignal,
		SignalName:     "event-signal",
		WorkflowIDExpr: "wf-{{.workflowId}}",
		Params: []ParamConfig{
			{Expr: "data.eventType"},
			{Expr: "data.payload"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error creating sink: %v", err)
	}

	event, _ := json.Marshal(map[string]interface{}{
		"workflowId": "abc",
		"eventType":  "order.created",
		"payload":    map[string]interface{}{"key": "value"},
	})
	err = s.Deliver(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify signal received args as slice (multiple params)
	args, ok := mc.lastSignalArg.([]interface{})
	if !ok {
		t.Fatalf("expected signal arg to be []interface{}, got %T", mc.lastSignalArg)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 signal args, got %d", len(args))
	}
	if args[0] != "order.created" {
		t.Errorf("expected first signal arg 'order.created', got %v", args[0])
	}
}

func TestDeliver_TypedParams_SingleParam_SignalMode(t *testing.T) {
	mc := &mockClient{}
	s, err := NewSink(mc, Config{
		TaskQueue:      "test-queue",
		WorkflowType:   "TestWorkflow",
		Mode:           ModeSignal,
		SignalName:     "event-signal",
		WorkflowIDExpr: "wf-static",
		Params: []ParamConfig{
			{Expr: "data.eventType"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error creating sink: %v", err)
	}

	event, _ := json.Marshal(map[string]interface{}{
		"eventType": "order.created",
	})
	err = s.Deliver(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify single param is passed directly (not wrapped in slice)
	if mc.lastSignalArg != "order.created" {
		t.Errorf("expected signal arg 'order.created', got %v (type: %T)", mc.lastSignalArg, mc.lastSignalArg)
	}
}

func TestNewSink_InvalidParamExpr(t *testing.T) {
	_, err := NewSink(&mockClient{}, Config{
		TaskQueue:    "test-queue",
		WorkflowType: "TestWorkflow",
		Params: []ParamConfig{
			{Expr: "invalid.syntax[["}, // Invalid CEL expression
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid param expression")
	}
	if !strings.Contains(err.Error(), "compile param") {
		t.Errorf("expected compile error, got: %v", err)
	}
}

func TestDeliver_TypedParams_EvalError(t *testing.T) {
	mc := &mockClient{}
	s, err := NewSink(mc, Config{
		TaskQueue:    "test-queue",
		WorkflowType: "TestWorkflow",
		Params: []ParamConfig{
			{Expr: "data.nonexistent.nested.field"}, // Will fail at runtime
		},
	})
	if err != nil {
		t.Fatalf("unexpected error creating sink: %v", err)
	}

	event := []byte(`{"simple": "value"}`)
	err = s.Deliver(context.Background(), event, nil)
	if err == nil {
		t.Fatal("expected error for param evaluation failure")
	}
}

func TestDeliver_NoParams_SendsCloudEventAsMap(t *testing.T) {
	mc := &mockClient{}
	s, _ := NewSink(mc, Config{
		TaskQueue:      "test-queue",
		WorkflowType:   "TestWorkflow",
		WorkflowIDExpr: "order-{{.data.orderId}}",
		// No Params - should send the entire CloudEvent as a structured map
	})

	// Simulate a CloudEvent structure (as the pipeline would send)
	event, _ := json.Marshal(map[string]interface{}{
		"specversion": "1.0",
		"type":        "order.created",
		"source":      "test-flow",
		"id":          "ce-123",
		"data": map[string]interface{}{
			"orderId": "12345",
		},
	})
	err := s.Deliver(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify CloudEvent map was passed (not raw bytes)
	if len(mc.lastArgs) != 1 {
		t.Fatalf("expected 1 arg (CloudEvent map), got %d", len(mc.lastArgs))
	}
	ceMap, ok := mc.lastArgs[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected first arg to be map[string]interface{}, got %T", mc.lastArgs[0])
	}
	if ceMap["specversion"] != "1.0" {
		t.Errorf("expected specversion '1.0', got %v", ceMap["specversion"])
	}
	if ceMap["type"] != "order.created" {
		t.Errorf("expected type 'order.created', got %v", ceMap["type"])
	}
	data, ok := ceMap["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data to be map, got %T", ceMap["data"])
	}
	if data["orderId"] != "12345" {
		t.Errorf("expected data.orderId '12345', got %v", data["orderId"])
	}
}

func TestDeliver_TypedParams_AllTypes(t *testing.T) {
	mc := &mockClient{}
	s, err := NewSink(mc, Config{
		TaskQueue:    "test-queue",
		WorkflowType: "TestWorkflow",
		Params: []ParamConfig{
			{Expr: "data.strVal"},
			{Expr: "data.intVal"},
			{Expr: "data.floatVal"},
			{Expr: "data.boolVal"},
			{Expr: "data.nullVal"},
			{Expr: "data.listVal"},
			{Expr: "data.mapVal"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error creating sink: %v", err)
	}

	event, _ := json.Marshal(map[string]interface{}{
		"strVal":   "hello",
		"intVal":   42,
		"floatVal": 3.14,
		"boolVal":  true,
		"nullVal":  nil,
		"listVal":  []interface{}{"a", "b", "c"},
		"mapVal":   map[string]interface{}{"nested": "value"},
	})
	err = s.Deliver(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mc.lastArgs) != 7 {
		t.Fatalf("expected 7 args, got %d", len(mc.lastArgs))
	}

	// String
	if mc.lastArgs[0] != "hello" {
		t.Errorf("expected strVal 'hello', got %v", mc.lastArgs[0])
	}
	// Int (JSON numbers are float64 by default)
	if mc.lastArgs[1] != float64(42) {
		t.Errorf("expected intVal 42, got %v (type: %T)", mc.lastArgs[1], mc.lastArgs[1])
	}
	// Float
	if mc.lastArgs[2] != 3.14 {
		t.Errorf("expected floatVal 3.14, got %v", mc.lastArgs[2])
	}
	// Bool
	if mc.lastArgs[3] != true {
		t.Errorf("expected boolVal true, got %v", mc.lastArgs[3])
	}
	// Null
	if mc.lastArgs[4] != nil {
		t.Errorf("expected nullVal nil, got %v", mc.lastArgs[4])
	}
	// List
	list, ok := mc.lastArgs[5].([]interface{})
	if !ok {
		t.Errorf("expected listVal to be []interface{}, got %T", mc.lastArgs[5])
	} else if len(list) != 3 {
		t.Errorf("expected listVal to have 3 elements, got %d", len(list))
	}
	// Map
	m, ok := mc.lastArgs[6].(map[string]interface{})
	if !ok {
		t.Errorf("expected mapVal to be map[string]interface{}, got %T", mc.lastArgs[6])
	} else if m["nested"] != "value" {
		t.Errorf("expected mapVal.nested to be 'value', got %v", m["nested"])
	}
}

func TestDeliver_InvalidEventJSON(t *testing.T) {
	mc := &mockClient{}
	s, err := NewSink(mc, Config{
		TaskQueue:    "test-queue",
		WorkflowType: "TestWorkflow",
		Params: []ParamConfig{
			{Expr: "data.field"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error creating sink: %v", err)
	}

	// Invalid JSON
	err = s.Deliver(context.Background(), []byte("not valid json"), nil)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse cloudevent") {
		t.Errorf("expected parse cloudevent error, got: %v", err)
	}
}

func TestDeliver_SignalMode_CloudEventAsMap(t *testing.T) {
	mc := &mockClient{}
	s, _ := NewSink(mc, Config{
		TaskQueue:      "test-queue",
		WorkflowType:   "TestWorkflow",
		Mode:           ModeSignal,
		SignalName:     "event-signal",
		WorkflowIDExpr: "wf-{{.data.id}}",
		// No Params - sends entire CloudEvent
	})

	event, _ := json.Marshal(map[string]interface{}{
		"specversion": "1.0",
		"type":        "order.created",
		"source":      "test-flow",
		"id":          "ce-123",
		"data": map[string]interface{}{
			"id":      "abc",
			"orderId": "12345",
		},
	})
	err := s.Deliver(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mc.lastSignalID != "wf-abc" {
		t.Errorf("expected signal workflow ID 'wf-abc', got %q", mc.lastSignalID)
	}

	// Signal arg should be the entire CloudEvent as a map
	ceMap, ok := mc.lastSignalArg.(map[string]interface{})
	if !ok {
		t.Fatalf("expected signal arg to be map[string]interface{}, got %T", mc.lastSignalArg)
	}
	if ceMap["type"] != "order.created" {
		t.Errorf("expected type 'order.created', got %v", ceMap["type"])
	}
}

func TestDeliver_TypedParams_CloudEventStructure(t *testing.T) {
	// Test that typed params work with CloudEvent structure
	mc := &mockClient{}
	s, err := NewSink(mc, Config{
		TaskQueue:      "test-queue",
		WorkflowType:   "TestWorkflow",
		WorkflowIDExpr: "order-{{.data.orderId}}",
		Params: []ParamConfig{
			{Expr: "data.type"},         // CE type
			{Expr: "data.data.orderId"}, // Nested data field
			{Expr: "data.data.amount"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error creating sink: %v", err)
	}

	event, _ := json.Marshal(map[string]interface{}{
		"specversion": "1.0",
		"type":        "order.created",
		"source":      "test-flow",
		"id":          "ce-123",
		"data": map[string]interface{}{
			"orderId": "12345",
			"amount":  150.50,
		},
	})
	err = s.Deliver(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mc.lastArgs) != 3 {
		t.Fatalf("expected 3 args, got %d", len(mc.lastArgs))
	}
	if mc.lastArgs[0] != "order.created" {
		t.Errorf("expected first arg 'order.created', got %v", mc.lastArgs[0])
	}
	if mc.lastArgs[1] != "12345" {
		t.Errorf("expected second arg '12345', got %v", mc.lastArgs[1])
	}
	if mc.lastArgs[2] != 150.50 {
		t.Errorf("expected third arg 150.50, got %v", mc.lastArgs[2])
	}
}

func TestResolveNestedField(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		path     string
		expected string
	}{
		{
			name:     "top level field",
			data:     map[string]interface{}{"id": "123"},
			path:     "id",
			expected: "123",
		},
		{
			name: "nested field",
			data: map[string]interface{}{
				"data": map[string]interface{}{"orderId": "456"},
			},
			path:     "data.orderId",
			expected: "456",
		},
		{
			name: "deeply nested field",
			data: map[string]interface{}{
				"data": map[string]interface{}{
					"order": map[string]interface{}{"id": "789"},
				},
			},
			path:     "data.order.id",
			expected: "789",
		},
		{
			name:     "missing field",
			data:     map[string]interface{}{"id": "123"},
			path:     "missing",
			expected: "",
		},
		{
			name: "missing nested field",
			data: map[string]interface{}{
				"data": map[string]interface{}{"orderId": "456"},
			},
			path:     "data.missing",
			expected: "",
		},
		{
			name:     "numeric value",
			data:     map[string]interface{}{"count": 42},
			path:     "count",
			expected: "42",
		},
		{
			name: "non-map in path",
			data: map[string]interface{}{
				"data": "not-a-map",
			},
			path:     "data.field",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveNestedField(tt.data, tt.path)
			if result != tt.expected {
				t.Errorf("resolveNestedField(%v, %q) = %q, want %q", tt.data, tt.path, result, tt.expected)
			}
		})
	}
}

func TestToNative_CELTypes(t *testing.T) {
	// Test toNative with actual CEL types
	mc := &mockClient{}
	s, err := NewSink(mc, Config{
		TaskQueue:    "test-queue",
		WorkflowType: "TestWorkflow",
		Params: []ParamConfig{
			{Expr: "data.intVal"},
			{Expr: "data.doubleVal"},
			{Expr: "data.boolVal"},
			{Expr: "data.strVal"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error creating sink: %v", err)
	}

	event, _ := json.Marshal(map[string]interface{}{
		"intVal":    int64(42),
		"doubleVal": 3.14159,
		"boolVal":   false,
		"strVal":    "test-string",
	})
	err = s.Deliver(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mc.lastArgs) != 4 {
		t.Fatalf("expected 4 args, got %d", len(mc.lastArgs))
	}
	// Int (JSON numbers are float64 by default, CEL may convert)
	if mc.lastArgs[0] != float64(42) {
		t.Errorf("expected intVal 42, got %v (type: %T)", mc.lastArgs[0], mc.lastArgs[0])
	}
	if mc.lastArgs[1] != 3.14159 {
		t.Errorf("expected doubleVal 3.14159, got %v", mc.lastArgs[1])
	}
	if mc.lastArgs[2] != false {
		t.Errorf("expected boolVal false, got %v", mc.lastArgs[2])
	}
	if mc.lastArgs[3] != "test-string" {
		t.Errorf("expected strVal 'test-string', got %v", mc.lastArgs[3])
	}
}
