package operator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lsm/fiso/api/v1alpha1"
)

type mockClient struct {
	flows          map[string]*v1alpha1.FlowDefinition
	links          map[string]*v1alpha1.LinkTarget
	getFlowErr     error
	getLinkErr     error
	updateFlowErr  error
	updateLinkErr  error
	lastFlowStatus *v1alpha1.FlowDefinitionStatus
	lastLinkStatus *v1alpha1.LinkTargetStatus
}

func newMockClient() *mockClient {
	return &mockClient{
		flows: make(map[string]*v1alpha1.FlowDefinition),
		links: make(map[string]*v1alpha1.LinkTarget),
	}
}

func (m *mockClient) GetFlowDefinition(_ context.Context, namespace, name string) (*v1alpha1.FlowDefinition, error) {
	if m.getFlowErr != nil {
		return nil, m.getFlowErr
	}
	key := namespace + "/" + name
	fd, ok := m.flows[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return fd, nil
}

func (m *mockClient) UpdateFlowDefinitionStatus(_ context.Context, fd *v1alpha1.FlowDefinition) error {
	m.lastFlowStatus = &fd.Status
	return m.updateFlowErr
}

func (m *mockClient) GetLinkTarget(_ context.Context, namespace, name string) (*v1alpha1.LinkTarget, error) {
	if m.getLinkErr != nil {
		return nil, m.getLinkErr
	}
	key := namespace + "/" + name
	lt, ok := m.links[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return lt, nil
}

func (m *mockClient) UpdateLinkTargetStatus(_ context.Context, lt *v1alpha1.LinkTarget) error {
	m.lastLinkStatus = &lt.Status
	return m.updateLinkErr
}

func TestFlowReconciler_ValidFlow(t *testing.T) {
	mc := newMockClient()
	mc.flows["default/test-flow"] = &v1alpha1.FlowDefinition{
		ObjectMeta: v1alpha1.ObjectMeta{Name: "test-flow", Namespace: "default"},
		Spec: v1alpha1.FlowDefinitionSpec{
			Source: v1alpha1.SourceSpec{Type: "kafka"},
			Sink:   v1alpha1.SinkSpec{Type: "http"},
		},
	}

	r := NewFlowReconciler(mc, nil)
	err := r.Reconcile(context.Background(), ReconcileRequest{Namespace: "default", Name: "test-flow"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastFlowStatus.Phase != "Ready" {
		t.Errorf("expected phase 'Ready', got %q", mc.lastFlowStatus.Phase)
	}
}

func TestFlowReconciler_InvalidSource(t *testing.T) {
	mc := newMockClient()
	mc.flows["default/bad-flow"] = &v1alpha1.FlowDefinition{
		ObjectMeta: v1alpha1.ObjectMeta{Name: "bad-flow", Namespace: "default"},
		Spec: v1alpha1.FlowDefinitionSpec{
			Source: v1alpha1.SourceSpec{Type: "unknown"},
			Sink:   v1alpha1.SinkSpec{Type: "http"},
		},
	}

	r := NewFlowReconciler(mc, nil)
	err := r.Reconcile(context.Background(), ReconcileRequest{Namespace: "default", Name: "bad-flow"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastFlowStatus.Phase != "Error" {
		t.Errorf("expected phase 'Error', got %q", mc.lastFlowStatus.Phase)
	}
	if !strings.Contains(mc.lastFlowStatus.Message, "unsupported source type") {
		t.Errorf("expected validation error message, got %q", mc.lastFlowStatus.Message)
	}
}

func TestFlowReconciler_MissingSourceType(t *testing.T) {
	mc := newMockClient()
	mc.flows["default/empty"] = &v1alpha1.FlowDefinition{
		ObjectMeta: v1alpha1.ObjectMeta{Name: "empty", Namespace: "default"},
		Spec: v1alpha1.FlowDefinitionSpec{
			Source: v1alpha1.SourceSpec{},
			Sink:   v1alpha1.SinkSpec{Type: "http"},
		},
	}

	r := NewFlowReconciler(mc, nil)
	_ = r.Reconcile(context.Background(), ReconcileRequest{Namespace: "default", Name: "empty"})
	if mc.lastFlowStatus.Phase != "Error" {
		t.Errorf("expected phase 'Error', got %q", mc.lastFlowStatus.Phase)
	}
}

func TestFlowReconciler_GetError(t *testing.T) {
	mc := newMockClient()
	mc.getFlowErr = errors.New("api server down")

	r := NewFlowReconciler(mc, nil)
	err := r.Reconcile(context.Background(), ReconcileRequest{Namespace: "default", Name: "flow"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "api server down") {
		t.Errorf("expected api error, got %q", err.Error())
	}
}

func TestFlowReconciler_UpdateError(t *testing.T) {
	mc := newMockClient()
	mc.flows["default/flow"] = &v1alpha1.FlowDefinition{
		ObjectMeta: v1alpha1.ObjectMeta{Name: "flow", Namespace: "default"},
		Spec: v1alpha1.FlowDefinitionSpec{
			Source: v1alpha1.SourceSpec{Type: "kafka"},
			Sink:   v1alpha1.SinkSpec{Type: "http"},
		},
	}
	mc.updateFlowErr = errors.New("conflict")

	r := NewFlowReconciler(mc, nil)
	err := r.Reconcile(context.Background(), ReconcileRequest{Namespace: "default", Name: "flow"})
	if err == nil {
		t.Fatal("expected error on update failure")
	}
}

func TestFlowReconciler_GRPCSourceTemporalSink(t *testing.T) {
	mc := newMockClient()
	mc.flows["default/grpc-flow"] = &v1alpha1.FlowDefinition{
		ObjectMeta: v1alpha1.ObjectMeta{Name: "grpc-flow", Namespace: "default"},
		Spec: v1alpha1.FlowDefinitionSpec{
			Source: v1alpha1.SourceSpec{Type: "grpc"},
			Sink:   v1alpha1.SinkSpec{Type: "temporal"},
		},
	}

	r := NewFlowReconciler(mc, nil)
	err := r.Reconcile(context.Background(), ReconcileRequest{Namespace: "default", Name: "grpc-flow"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastFlowStatus.Phase != "Ready" {
		t.Errorf("expected phase 'Ready', got %q", mc.lastFlowStatus.Phase)
	}
}

func TestLinkReconciler_ValidTarget(t *testing.T) {
	mc := newMockClient()
	mc.links["default/crm"] = &v1alpha1.LinkTarget{
		ObjectMeta: v1alpha1.ObjectMeta{Name: "crm", Namespace: "default"},
		Spec: v1alpha1.LinkTargetSpec{
			Protocol: "https",
			Host:     "crm.example.com",
		},
	}

	r := NewLinkReconciler(mc, nil)
	err := r.Reconcile(context.Background(), ReconcileRequest{Namespace: "default", Name: "crm"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastLinkStatus.Phase != "Ready" {
		t.Errorf("expected phase 'Ready', got %q", mc.lastLinkStatus.Phase)
	}
}

func TestLinkReconciler_MissingHost(t *testing.T) {
	mc := newMockClient()
	mc.links["default/bad"] = &v1alpha1.LinkTarget{
		ObjectMeta: v1alpha1.ObjectMeta{Name: "bad", Namespace: "default"},
		Spec: v1alpha1.LinkTargetSpec{
			Protocol: "https",
		},
	}

	r := NewLinkReconciler(mc, nil)
	_ = r.Reconcile(context.Background(), ReconcileRequest{Namespace: "default", Name: "bad"})
	if mc.lastLinkStatus.Phase != "Error" {
		t.Errorf("expected phase 'Error', got %q", mc.lastLinkStatus.Phase)
	}
}

func TestLinkReconciler_InvalidProtocol(t *testing.T) {
	mc := newMockClient()
	mc.links["default/bad"] = &v1alpha1.LinkTarget{
		ObjectMeta: v1alpha1.ObjectMeta{Name: "bad", Namespace: "default"},
		Spec: v1alpha1.LinkTargetSpec{
			Protocol: "ftp",
			Host:     "example.com",
		},
	}

	r := NewLinkReconciler(mc, nil)
	_ = r.Reconcile(context.Background(), ReconcileRequest{Namespace: "default", Name: "bad"})
	if mc.lastLinkStatus.Phase != "Error" {
		t.Errorf("expected phase 'Error', got %q", mc.lastLinkStatus.Phase)
	}
}

func TestLinkReconciler_GetError(t *testing.T) {
	mc := newMockClient()
	mc.getLinkErr = errors.New("timeout")

	r := NewLinkReconciler(mc, nil)
	err := r.Reconcile(context.Background(), ReconcileRequest{Namespace: "default", Name: "target"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLinkReconciler_UpdateError(t *testing.T) {
	mc := newMockClient()
	mc.links["default/target"] = &v1alpha1.LinkTarget{
		ObjectMeta: v1alpha1.ObjectMeta{Name: "target", Namespace: "default"},
		Spec: v1alpha1.LinkTargetSpec{
			Protocol: "https",
			Host:     "example.com",
		},
	}
	mc.updateLinkErr = errors.New("conflict")

	r := NewLinkReconciler(mc, nil)
	err := r.Reconcile(context.Background(), ReconcileRequest{Namespace: "default", Name: "target"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFlowReconciler_InvalidSinkType(t *testing.T) {
	mc := newMockClient()
	mc.flows["default/bad-sink"] = &v1alpha1.FlowDefinition{
		ObjectMeta: v1alpha1.ObjectMeta{Name: "bad-sink", Namespace: "default"},
		Spec: v1alpha1.FlowDefinitionSpec{
			Source: v1alpha1.SourceSpec{Type: "kafka"},
			Sink:   v1alpha1.SinkSpec{Type: "redis"},
		},
	}

	r := NewFlowReconciler(mc, nil)
	_ = r.Reconcile(context.Background(), ReconcileRequest{Namespace: "default", Name: "bad-sink"})
	if mc.lastFlowStatus.Phase != "Error" {
		t.Errorf("expected phase 'Error', got %q", mc.lastFlowStatus.Phase)
	}
	if !strings.Contains(mc.lastFlowStatus.Message, "unsupported sink type") {
		t.Errorf("expected unsupported sink type error, got %q", mc.lastFlowStatus.Message)
	}
}

func TestFlowReconciler_MissingSinkType(t *testing.T) {
	mc := newMockClient()
	mc.flows["default/no-sink"] = &v1alpha1.FlowDefinition{
		ObjectMeta: v1alpha1.ObjectMeta{Name: "no-sink", Namespace: "default"},
		Spec: v1alpha1.FlowDefinitionSpec{
			Source: v1alpha1.SourceSpec{Type: "kafka"},
			Sink:   v1alpha1.SinkSpec{},
		},
	}

	r := NewFlowReconciler(mc, nil)
	_ = r.Reconcile(context.Background(), ReconcileRequest{Namespace: "default", Name: "no-sink"})
	if mc.lastFlowStatus.Phase != "Error" {
		t.Errorf("expected phase 'Error', got %q", mc.lastFlowStatus.Phase)
	}
	if !strings.Contains(mc.lastFlowStatus.Message, "sink type is required") {
		t.Errorf("expected sink type required error, got %q", mc.lastFlowStatus.Message)
	}
}

func TestLinkReconciler_MissingProtocol(t *testing.T) {
	mc := newMockClient()
	mc.links["default/no-proto"] = &v1alpha1.LinkTarget{
		ObjectMeta: v1alpha1.ObjectMeta{Name: "no-proto", Namespace: "default"},
		Spec: v1alpha1.LinkTargetSpec{
			Host: "example.com",
		},
	}

	r := NewLinkReconciler(mc, nil)
	_ = r.Reconcile(context.Background(), ReconcileRequest{Namespace: "default", Name: "no-proto"})
	if mc.lastLinkStatus.Phase != "Error" {
		t.Errorf("expected phase 'Error', got %q", mc.lastLinkStatus.Phase)
	}
	if !strings.Contains(mc.lastLinkStatus.Message, "protocol is required") {
		t.Errorf("expected protocol required error, got %q", mc.lastLinkStatus.Message)
	}
}

func TestFlowReconciler_ValidationUpdateError(t *testing.T) {
	mc := newMockClient()
	mc.flows["default/bad"] = &v1alpha1.FlowDefinition{
		ObjectMeta: v1alpha1.ObjectMeta{Name: "bad", Namespace: "default"},
		Spec: v1alpha1.FlowDefinitionSpec{
			Source: v1alpha1.SourceSpec{Type: "unknown"},
			Sink:   v1alpha1.SinkSpec{Type: "http"},
		},
	}
	mc.updateFlowErr = errors.New("update failed")

	r := NewFlowReconciler(mc, nil)
	// Should not return error — validation errors are not requeued
	err := r.Reconcile(context.Background(), ReconcileRequest{Namespace: "default", Name: "bad"})
	if err != nil {
		t.Fatalf("expected nil (validation errors not requeued), got %v", err)
	}
}

func TestLinkReconciler_ValidationUpdateError(t *testing.T) {
	mc := newMockClient()
	mc.links["default/bad"] = &v1alpha1.LinkTarget{
		ObjectMeta: v1alpha1.ObjectMeta{Name: "bad", Namespace: "default"},
		Spec: v1alpha1.LinkTargetSpec{
			Protocol: "ftp",
			Host:     "example.com",
		},
	}
	mc.updateLinkErr = errors.New("update failed")

	r := NewLinkReconciler(mc, nil)
	err := r.Reconcile(context.Background(), ReconcileRequest{Namespace: "default", Name: "bad"})
	if err != nil {
		t.Fatalf("expected nil (validation errors not requeued), got %v", err)
	}
}

func TestLinkReconciler_GRPCProtocol(t *testing.T) {
	mc := newMockClient()
	mc.links["default/grpc-svc"] = &v1alpha1.LinkTarget{
		ObjectMeta: v1alpha1.ObjectMeta{Name: "grpc-svc", Namespace: "default"},
		Spec: v1alpha1.LinkTargetSpec{
			Protocol: "grpc",
			Host:     "grpc.example.com",
		},
	}

	r := NewLinkReconciler(mc, nil)
	err := r.Reconcile(context.Background(), ReconcileRequest{Namespace: "default", Name: "grpc-svc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mc.lastLinkStatus.Phase != "Ready" {
		t.Errorf("expected phase 'Ready', got %q", mc.lastLinkStatus.Phase)
	}
}
