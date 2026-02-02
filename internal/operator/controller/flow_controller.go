package controller

import (
	"context"
	"fmt"
	"log/slog"

	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fisov1alpha1 "github.com/lsm/fiso/api/v1alpha1"
	"github.com/lsm/fiso/internal/operator"
)

// FlowDefinitionReconciler reconciles FlowDefinition CRDs using controller-runtime.
type FlowDefinitionReconciler struct {
	client.Client
	Logger *slog.Logger
}

// Reconcile handles a FlowDefinition reconciliation event.
func (r *FlowDefinitionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("namespace", req.Namespace, "name", req.Name)
	logger.Info("reconciling FlowDefinition")

	var fd fisov1alpha1.FlowDefinitionCR
	if err := r.Get(ctx, req.NamespacedName, &fd); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("FlowDefinition deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get FlowDefinition: %w", err)
	}

	// Validate using shared validation logic
	if err := operator.ValidateFlowSpec(&fd.Spec); err != nil {
		fd.Status.Phase = "Error"
		fd.Status.Message = err.Error()
		if updateErr := r.Status().Update(ctx, &fd); updateErr != nil {
			logger.Error("failed to update error status", "error", updateErr)
			return ctrl.Result{}, fmt.Errorf("update error status: %w", updateErr)
		}
		return ctrl.Result{}, nil // Don't requeue validation errors
	}

	// Mark as Ready
	fd.Status.Phase = "Ready"
	fd.Status.Message = "Flow definition validated and active"
	if err := r.Status().Update(ctx, &fd); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}

	logger.Info("FlowDefinition reconciled", "phase", fd.Status.Phase)
	return ctrl.Result{}, nil
}

// SetupWithManager registers the controller with the manager.
func (r *FlowDefinitionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fisov1alpha1.FlowDefinitionCR{}).
		Complete(r)
}
