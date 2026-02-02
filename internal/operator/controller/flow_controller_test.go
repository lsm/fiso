package controller

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fisov1alpha1 "github.com/lsm/fiso/api/v1alpha1"
)

func newFlowScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = fisov1alpha1.AddToScheme(s)
	return s
}

func TestFlowReconciler_ValidFlow(t *testing.T) {
	fd := &fisov1alpha1.FlowDefinitionCR{
		ObjectMeta: metav1.ObjectMeta{Name: "test-flow", Namespace: "default"},
		Spec: fisov1alpha1.FlowDefinitionSpec{
			Source: fisov1alpha1.SourceSpec{Type: "kafka"},
			Sink:   fisov1alpha1.SinkSpec{Type: "http"},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(newFlowScheme()).
		WithObjects(fd).
		WithStatusSubresource(fd).
		Build()

	r := &FlowDefinitionReconciler{Client: client, Logger: slog.Default()}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-flow", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue")
	}

	// Verify status was updated
	var updated fisov1alpha1.FlowDefinitionCR
	if err := client.Get(context.Background(), types.NamespacedName{Name: "test-flow", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get updated resource: %v", err)
	}
	if updated.Status.Phase != "Ready" {
		t.Errorf("expected phase 'Ready', got %q", updated.Status.Phase)
	}
}

func TestFlowReconciler_InvalidSource(t *testing.T) {
	fd := &fisov1alpha1.FlowDefinitionCR{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-flow", Namespace: "default"},
		Spec: fisov1alpha1.FlowDefinitionSpec{
			Source: fisov1alpha1.SourceSpec{Type: "unknown"},
			Sink:   fisov1alpha1.SinkSpec{Type: "http"},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(newFlowScheme()).
		WithObjects(fd).
		WithStatusSubresource(fd).
		Build()

	r := &FlowDefinitionReconciler{Client: client, Logger: slog.Default()}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "bad-flow", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for validation error")
	}

	var updated fisov1alpha1.FlowDefinitionCR
	if err := client.Get(context.Background(), types.NamespacedName{Name: "bad-flow", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get: %v", err)
	}
	if updated.Status.Phase != "Error" {
		t.Errorf("expected phase 'Error', got %q", updated.Status.Phase)
	}
}

func TestFlowReconciler_InvalidSink(t *testing.T) {
	fd := &fisov1alpha1.FlowDefinitionCR{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-sink", Namespace: "default"},
		Spec: fisov1alpha1.FlowDefinitionSpec{
			Source: fisov1alpha1.SourceSpec{Type: "kafka"},
			Sink:   fisov1alpha1.SinkSpec{Type: "redis"},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(newFlowScheme()).
		WithObjects(fd).
		WithStatusSubresource(fd).
		Build()

	r := &FlowDefinitionReconciler{Client: client, Logger: slog.Default()}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "bad-sink", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue")
	}

	var updated fisov1alpha1.FlowDefinitionCR
	_ = client.Get(context.Background(), types.NamespacedName{Name: "bad-sink", Namespace: "default"}, &updated)
	if updated.Status.Phase != "Error" {
		t.Errorf("expected phase 'Error', got %q", updated.Status.Phase)
	}
}

func TestFlowReconciler_MissingSourceType(t *testing.T) {
	fd := &fisov1alpha1.FlowDefinitionCR{
		ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: "default"},
		Spec: fisov1alpha1.FlowDefinitionSpec{
			Source: fisov1alpha1.SourceSpec{},
			Sink:   fisov1alpha1.SinkSpec{Type: "http"},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(newFlowScheme()).
		WithObjects(fd).
		WithStatusSubresource(fd).
		Build()

	r := &FlowDefinitionReconciler{Client: client, Logger: slog.Default()}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "empty", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var updated fisov1alpha1.FlowDefinitionCR
	_ = client.Get(context.Background(), types.NamespacedName{Name: "empty", Namespace: "default"}, &updated)
	if updated.Status.Phase != "Error" {
		t.Errorf("expected phase 'Error', got %q", updated.Status.Phase)
	}
}

func TestFlowReconciler_MissingSinkType(t *testing.T) {
	fd := &fisov1alpha1.FlowDefinitionCR{
		ObjectMeta: metav1.ObjectMeta{Name: "no-sink", Namespace: "default"},
		Spec: fisov1alpha1.FlowDefinitionSpec{
			Source: fisov1alpha1.SourceSpec{Type: "kafka"},
			Sink:   fisov1alpha1.SinkSpec{},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(newFlowScheme()).
		WithObjects(fd).
		WithStatusSubresource(fd).
		Build()

	r := &FlowDefinitionReconciler{Client: client, Logger: slog.Default()}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "no-sink", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var updated fisov1alpha1.FlowDefinitionCR
	_ = client.Get(context.Background(), types.NamespacedName{Name: "no-sink", Namespace: "default"}, &updated)
	if updated.Status.Phase != "Error" {
		t.Errorf("expected phase 'Error', got %q", updated.Status.Phase)
	}
}

func TestFlowReconciler_NotFound(t *testing.T) {
	client := fake.NewClientBuilder().
		WithScheme(newFlowScheme()).
		Build()

	r := &FlowDefinitionReconciler{Client: client, Logger: slog.Default()}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "deleted", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error for not-found: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue")
	}
}

func TestFlowReconciler_HTTPSource(t *testing.T) {
	fd := &fisov1alpha1.FlowDefinitionCR{
		ObjectMeta: metav1.ObjectMeta{Name: "http-flow", Namespace: "default"},
		Spec: fisov1alpha1.FlowDefinitionSpec{
			Source: fisov1alpha1.SourceSpec{Type: "http"},
			Sink:   fisov1alpha1.SinkSpec{Type: "http"},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(newFlowScheme()).
		WithObjects(fd).
		WithStatusSubresource(fd).
		Build()

	r := &FlowDefinitionReconciler{Client: client, Logger: slog.Default()}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "http-flow", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var updated fisov1alpha1.FlowDefinitionCR
	_ = client.Get(context.Background(), types.NamespacedName{Name: "http-flow", Namespace: "default"}, &updated)
	if updated.Status.Phase != "Ready" {
		t.Errorf("expected phase 'Ready', got %q", updated.Status.Phase)
	}
}

func TestFlowReconciler_GRPCSourceTemporalSink(t *testing.T) {
	fd := &fisov1alpha1.FlowDefinitionCR{
		ObjectMeta: metav1.ObjectMeta{Name: "grpc-flow", Namespace: "default"},
		Spec: fisov1alpha1.FlowDefinitionSpec{
			Source: fisov1alpha1.SourceSpec{Type: "grpc"},
			Sink:   fisov1alpha1.SinkSpec{Type: "temporal"},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(newFlowScheme()).
		WithObjects(fd).
		WithStatusSubresource(fd).
		Build()

	r := &FlowDefinitionReconciler{Client: client, Logger: slog.Default()}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "grpc-flow", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var updated fisov1alpha1.FlowDefinitionCR
	_ = client.Get(context.Background(), types.NamespacedName{Name: "grpc-flow", Namespace: "default"}, &updated)
	if updated.Status.Phase != "Ready" {
		t.Errorf("expected phase 'Ready', got %q", updated.Status.Phase)
	}
}

// failingStatusClient wraps a client.Client and makes status updates fail
type failingStatusClient struct {
	client.Client
	failOnUpdate bool
}

func (c *failingStatusClient) Status() client.StatusWriter {
	return &failingStatusWriter{
		StatusWriter: c.Client.Status(),
		failOnUpdate: c.failOnUpdate,
	}
}

type failingStatusWriter struct {
	client.StatusWriter
	failOnUpdate bool
}

func (w *failingStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	if w.failOnUpdate {
		return errors.New("simulated status update failure")
	}
	return w.StatusWriter.Update(ctx, obj, opts...)
}

func (w *failingStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	if w.failOnUpdate {
		return errors.New("simulated status patch failure")
	}
	return w.StatusWriter.Patch(ctx, obj, patch, opts...)
}

func TestFlowReconciler_StatusUpdateFailureOnSuccess(t *testing.T) {
	fd := &fisov1alpha1.FlowDefinitionCR{
		ObjectMeta: metav1.ObjectMeta{Name: "test-flow", Namespace: "default"},
		Spec: fisov1alpha1.FlowDefinitionSpec{
			Source: fisov1alpha1.SourceSpec{Type: "kafka"},
			Sink:   fisov1alpha1.SinkSpec{Type: "http"},
		},
	}

	baseClient := fake.NewClientBuilder().
		WithScheme(newFlowScheme()).
		WithObjects(fd).
		WithStatusSubresource(fd).
		Build()

	failingClient := &failingStatusClient{
		Client:       baseClient,
		failOnUpdate: true,
	}

	r := &FlowDefinitionReconciler{Client: failingClient, Logger: slog.Default()}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-flow", Namespace: "default"},
	})
	if err == nil {
		t.Fatal("expected error from status update failure")
	}
	if err.Error() != "update status: simulated status update failure" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestFlowReconciler_StatusUpdateFailureOnValidationError(t *testing.T) {
	fd := &fisov1alpha1.FlowDefinitionCR{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-flow", Namespace: "default"},
		Spec: fisov1alpha1.FlowDefinitionSpec{
			Source: fisov1alpha1.SourceSpec{Type: "unknown"},
			Sink:   fisov1alpha1.SinkSpec{Type: "http"},
		},
	}

	baseClient := fake.NewClientBuilder().
		WithScheme(newFlowScheme()).
		WithObjects(fd).
		WithStatusSubresource(fd).
		Build()

	failingClient := &failingStatusClient{
		Client:       baseClient,
		failOnUpdate: true,
	}

	r := &FlowDefinitionReconciler{Client: failingClient, Logger: slog.Default()}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "bad-flow", Namespace: "default"},
	})
	if err == nil {
		t.Fatal("expected error from status update failure")
	}
	if err.Error() != "update error status: simulated status update failure" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestFlowReconciler_GetError(t *testing.T) {
	// Create client without the object to trigger a non-NotFound error
	// We'll use an interceptor to force a generic error
	baseClient := fake.NewClientBuilder().
		WithScheme(newFlowScheme()).
		Build()

	// Create a wrapper that returns a generic error on Get
	errorClient := &errorOnGetClient{
		Client: baseClient,
	}

	r := &FlowDefinitionReconciler{Client: errorClient, Logger: slog.Default()}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "test-flow", Namespace: "default"},
	})
	if err == nil {
		t.Fatal("expected error from Get failure")
	}
	if err.Error() != "get FlowDefinition: simulated get error" {
		t.Errorf("unexpected error message: %v", err)
	}
}

// errorOnGetClient simulates a generic Get error (not NotFound)
type errorOnGetClient struct {
	client.Client
}

func (c *errorOnGetClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return errors.New("simulated get error")
}
