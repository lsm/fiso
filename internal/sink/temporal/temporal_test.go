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
	executeErr    error
	signalErr     error
	closed        bool
	lastOpts      StartWorkflowOptions
	lastWorkflow  string
	lastArgs      []interface{}
	lastSignalID  string
	lastSignalArg interface{}
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
