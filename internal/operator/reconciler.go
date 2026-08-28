package operator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/lsm/fiso/api/v1alpha1"
)

// Client abstracts Kubernetes API client operations for testability.
type Client interface {
	// GetFlowDefinition retrieves a FlowDefinition by namespace and name.
	GetFlowDefinition(ctx context.Context, namespace, name string) (*v1alpha1.FlowDefinition, error)
	// UpdateFlowDefinitionStatus updates the status of a FlowDefinition.
	UpdateFlowDefinitionStatus(ctx context.Context, fd *v1alpha1.FlowDefinition) error
	// GetLinkTarget retrieves a LinkTarget by namespace and name.
	GetLinkTarget(ctx context.Context, namespace, name string) (*v1alpha1.LinkTarget, error)
	// UpdateLinkTargetStatus updates the status of a LinkTarget.
	UpdateLinkTargetStatus(ctx context.Context, lt *v1alpha1.LinkTarget) error
}

// ReconcileRequest identifies a resource to reconcile.
type ReconcileRequest struct {
	Namespace string
	Name      string
}

// FlowReconciler reconciles FlowDefinition CRDs.
type FlowReconciler struct {
	client Client
	logger *slog.Logger
}

// NewFlowReconciler creates a new FlowReconciler.
func NewFlowReconciler(client Client, logger *slog.Logger) *FlowReconciler {
	if logger == nil {
		logger = slog.Default()
	}
	return &FlowReconciler{client: client, logger: logger}
}

// Reconcile handles a FlowDefinition reconciliation event.
func (r *FlowReconciler) Reconcile(ctx context.Context, req ReconcileRequest) error {
	r.logger.Info("reconciling flow definition", "namespace", req.Namespace, "name", req.Name)

	fd, err := r.client.GetFlowDefinition(ctx, req.Namespace, req.Name)
	if err != nil {
		return fmt.Errorf("get flow definition: %w", err)
	}

	// Validate the flow definition
	if err := ValidateFlowSpec(&fd.Spec); err != nil {
		fd.Status.Phase = "Error"
		fd.Status.Message = err.Error()
		if updateErr := r.client.UpdateFlowDefinitionStatus(ctx, fd); updateErr != nil {
			r.logger.Error("failed to update status", "error", updateErr)
		}
		return nil // Don't requeue validation errors
	}

	fd.Status.Phase = "Ready"
	fd.Status.Message = "Flow definition validated"
	if err := r.client.UpdateFlowDefinitionStatus(ctx, fd); err != nil {
		return fmt.Errorf("update flow status: %w", err)
	}

	r.logger.Info("flow definition reconciled", "namespace", req.Namespace, "name", req.Name, "phase", fd.Status.Phase)
	return nil
}

func ValidateFlowSpec(spec *v1alpha1.FlowDefinitionSpec) error {
	if spec.Source.Type == "" {
		return fmt.Errorf("source type is required")
	}
	if spec.Sink.Type == "" {
		return fmt.Errorf("sink type is required")
	}
	validSourceTypes := map[string]bool{"kafka": true, "grpc": true, "http": true}
	if !validSourceTypes[spec.Source.Type] {
		return fmt.Errorf("unsupported source type: %s", spec.Source.Type)
	}
	validSinkTypes := map[string]bool{"http": true, "grpc": true, "temporal": true, "kafka": true}
	if !validSinkTypes[spec.Sink.Type] {
		return fmt.Errorf("unsupported sink type: %s", spec.Sink.Type)
	}
	if spec.Sink.Type == "grpc" {
		// CRD config values are strings; enforce the same executable contract as
		// local validation and the Flow builders (ADR 0003).
		if spec.Sink.Config["address"] == "" {
			return fmt.Errorf("sink config: address is required for grpc sink")
		}
		// Presence is checked separately from value: an explicitly empty timeout
		// or tls must be rejected, not treated as omitted.
		if timeout, present := spec.Sink.Config["timeout"]; present {
			d, err := time.ParseDuration(timeout)
			if err != nil {
				return fmt.Errorf("sink config: timeout %q is not a valid duration", timeout)
			}
			if d < 0 {
				return fmt.Errorf("sink config: timeout %q must not be negative", timeout)
			}
			if d == 0 {
				return fmt.Errorf("sink config: timeout %q must be positive", timeout)
			}
		}
		if tlsVal, present := spec.Sink.Config["tls"]; present && tlsVal != "false" {
			return fmt.Errorf("sink config: tls is not supported until gRPC TLS credentials are configurable")
		}
	}
	return nil
}

// LinkReconciler reconciles LinkTarget CRDs.
type LinkReconciler struct {
	client Client
	logger *slog.Logger
}

// NewLinkReconciler creates a new LinkReconciler.
func NewLinkReconciler(client Client, logger *slog.Logger) *LinkReconciler {
	if logger == nil {
		logger = slog.Default()
	}
	return &LinkReconciler{client: client, logger: logger}
}

// Reconcile handles a LinkTarget reconciliation event.
func (r *LinkReconciler) Reconcile(ctx context.Context, req ReconcileRequest) error {
	r.logger.Info("reconciling link target", "namespace", req.Namespace, "name", req.Name)

	lt, err := r.client.GetLinkTarget(ctx, req.Namespace, req.Name)
	if err != nil {
		return fmt.Errorf("get link target: %w", err)
	}

	if err := ValidateLinkSpec(&lt.Spec); err != nil {
		lt.Status.Phase = "Error"
		lt.Status.Message = err.Error()
		if updateErr := r.client.UpdateLinkTargetStatus(ctx, lt); updateErr != nil {
			r.logger.Error("failed to update status", "error", updateErr)
		}
		return nil
	}

	lt.Status.Phase = "Ready"
	lt.Status.Message = "Link target validated"
	if err := r.client.UpdateLinkTargetStatus(ctx, lt); err != nil {
		return fmt.Errorf("update link target status: %w", err)
	}

	r.logger.Info("link target reconciled", "namespace", req.Namespace, "name", req.Name, "phase", lt.Status.Phase)
	return nil
}

func ValidateLinkSpec(spec *v1alpha1.LinkTargetSpec) error {
	if spec.Host == "" {
		return fmt.Errorf("host is required")
	}
	if spec.Protocol == "" {
		return fmt.Errorf("protocol is required")
	}
	// Only protocols the shipped Link runtime can execute; grpc is rejected
	// until a Link gRPC transport contract exists (ADR 0003).
	validProtocols := map[string]bool{"http": true, "https": true}
	if !validProtocols[spec.Protocol] {
		return fmt.Errorf("unsupported protocol: %s", spec.Protocol)
	}
	return nil
}
