package controller

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// NodeReconciler deletes Node objects that match the configured label.
//
// The reconciler is intentionally minimal: it does not cordon/drain, and it
// refuses to delete control-plane nodes unless AllowControlPlane is set.
//
// This controller is meant to run in-cluster.
type NodeReconciler struct {
	client.Client

	DeleteLabelKey   string
	DeleteLabelValue string

	AllowControlPlane bool

	Log logr.Logger
}

func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("node", req.Name)

	var node corev1.Node
	if err := r.Get(ctx, req.NamespacedName, &node); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if node.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}

	if !r.matchesDeleteLabel(&node) {
		return ctrl.Result{}, nil
	}

	if !r.AllowControlPlane && isControlPlaneNode(&node) {
		logger.Info("skipping deletion of control-plane node")
		return ctrl.Result{}, nil
	}

	if err := r.Delete(ctx, &node); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("deleted node due to delete label", "labelKey", r.DeleteLabelKey, "labelValue", r.DeleteLabelValue)
	return ctrl.Result{}, nil
}

func (r *NodeReconciler) matchesDeleteLabel(node *corev1.Node) bool {
	if r.DeleteLabelKey == "" {
		return false
	}
	if node == nil || node.Labels == nil {
		return false
	}

	labelValue, ok := node.Labels[r.DeleteLabelKey]
	if !ok {
		return false
	}

	if r.DeleteLabelValue == "" {
		return true
	}
	return labelValue == r.DeleteLabelValue
}

func isControlPlaneNode(node *corev1.Node) bool {
	if node == nil {
		return false
	}

	controlPlaneKeys := map[string]struct{}{
		"node-role.kubernetes.io/control-plane": {},
		"node-role.kubernetes.io/master":        {},
		"node-role.kubernetes.io/etcd":          {},
	}

	if node.Labels != nil {
		for key := range controlPlaneKeys {
			if _, ok := node.Labels[key]; ok {
				return true
			}
		}
	}

	for _, taint := range node.Spec.Taints {
		if _, ok := controlPlaneKeys[taint.Key]; ok {
			return true
		}
	}

	return false
}

func (r *NodeReconciler) shouldReconcile(obj client.Object) bool {
	node, ok := obj.(*corev1.Node)
	if !ok {
		return false
	}
	if node.DeletionTimestamp != nil {
		return false
	}
	return r.matchesDeleteLabel(node)
}

func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Log.GetSink() == nil {
		r.Log = ctrl.Log.WithName("controllers").WithName("Node")
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return r.shouldReconcile(e.Object)
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return r.shouldReconcile(e.ObjectNew) || r.shouldReconcile(e.ObjectOld)
			},
			DeleteFunc: func(_ event.DeleteEvent) bool {
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				return r.shouldReconcile(e.Object)
			},
		}).
		Complete(r)
}
