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
	id := s.resolveWorkflowID([]byte("{}"))
	if id != "static-id" {
		t.Errorf("expected 'static-id', got %q", id)
	}
}

func TestResolveWorkflowID_EmptyExpr(t *testing.T) {
	s := &Sink{config: Config{WorkflowType: "TestWF"}}
	id := s.resolveWorkflowID([]byte("{}"))
	if !strings.HasPrefix(id, "TestWF-") {
		t.Errorf("expected ID starting with 'TestWF-', got %q", id)
	}
}

func TestResolveWorkflowID_InvalidJSON(t *testing.T) {
	s := &Sink{config: Config{WorkflowIDExpr: "wf-{{.id}}"}}
	id := s.resolveWorkflowID([]byte("not json"))
	if id != "wf-{{.id}}" {
		t.Errorf("expected raw template on invalid JSON, got %q", id)
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

func TestDeliver_NoParams_BackwardsCompatible(t *testing.T) {
	mc := &mockClient{}
	s, _ := NewSink(mc, Config{
		TaskQueue:      "test-queue",
		WorkflowType:   "TestWorkflow",
		WorkflowIDExpr: "order-{{.orderId}}",
		// No Params - should pass raw bytes
	})

	event, _ := json.Marshal(map[string]interface{}{"orderId": "12345"})
	err := s.Deliver(context.Background(), event, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify raw bytes were passed (backwards compatibility)
	if len(mc.lastArgs) != 1 {
		t.Fatalf("expected 1 arg (raw bytes), got %d", len(mc.lastArgs))
	}
	if _, ok := mc.lastArgs[0].([]byte); !ok {
		t.Errorf("expected first arg to be []byte, got %T", mc.lastArgs[0])
	}
}
