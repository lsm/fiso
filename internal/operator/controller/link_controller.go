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

// LinkTargetReconciler reconciles LinkTarget CRDs using controller-runtime.
type LinkTargetReconciler struct {
	client.Client
	Logger *slog.Logger
}

// Reconcile handles a LinkTarget reconciliation event.
func (r *LinkTargetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("namespace", req.Namespace, "name", req.Name)
	logger.Info("reconciling LinkTarget")

	var lt fisov1alpha1.LinkTargetCR
	if err := r.Get(ctx, req.NamespacedName, &lt); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("LinkTarget deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get LinkTarget: %w", err)
	}

	// Validate using shared validation logic
	if err := operator.ValidateLinkSpec(&lt.Spec); err != nil {
		lt.Status.Phase = "Error"
		lt.Status.Message = err.Error()
		if updateErr := r.Status().Update(ctx, &lt); updateErr != nil {
			logger.Error("failed to update error status", "error", updateErr)
			return ctrl.Result{}, fmt.Errorf("update error status: %w", updateErr)
		}
		return ctrl.Result{}, nil
	}

	// Mark as Ready
	lt.Status.Phase = "Ready"
	lt.Status.Message = "Link target validated and active"
	if err := r.Status().Update(ctx, &lt); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}

	logger.Info("LinkTarget reconciled", "phase", lt.Status.Phase)
	return ctrl.Result{}, nil
}

// SetupWithManager registers the controller with the manager.
func (r *LinkTargetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fisov1alpha1.LinkTargetCR{}).
		Complete(r)
}
