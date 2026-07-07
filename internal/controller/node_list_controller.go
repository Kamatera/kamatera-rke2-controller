package controller

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NodeListReconciler struct {
	client.Client
	Store              *NodeStateStore
	ServerStore        *ServerStateStore
	Matcher            NameMatcher
	TrackedTaints      map[string]struct{}
	TrackedAnnotations map[string]struct{}

	Log logr.Logger
}

func (r *NodeListReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("node", req.Name)
	var node corev1.Node
	if err := r.Get(ctx, req.NamespacedName, &node); err != nil {
		if apierrors.IsNotFound(err) {
			if previous, ok := r.Store.Delete(req.Name); ok {
				trackedTaints := r.TrackedTaints
				if trackedTaints == nil {
					trackedTaints = parseTrackedKeys(defaultTrackedTaintsCSV)
				}
				matchedServer, matched := r.Matcher.FindServerForNode(previous.Name, r.ServerStore)
				logger.Info("node deleted", nodeLogValues(previous, trackedTaints, r.TrackedAnnotations, matchedServer, matched)...)
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.V(2).Info("node reconcile")

	trackedTaints := r.TrackedTaints
	if trackedTaints == nil {
		trackedTaints = parseTrackedKeys(defaultTrackedTaintsCSV)
	}
	snapshot := NewNodeSnapshot(&node, trackedTaints, r.TrackedAnnotations)
	diff := r.Store.Replace(snapshot)
	matchedServer, matched := r.Matcher.FindServerForNode(node.Name, r.ServerStore)
	r.logDiff(logger, diff, trackedTaints, r.TrackedAnnotations, matchedServer, matched)
	return ctrl.Result{}, nil
}

func (r *NodeListReconciler) logDiff(logger logr.Logger, diff NodeStateDiff, trackedTaints map[string]struct{}, trackedAnnotations map[string]struct{}, matchedServer KamateraServer, matched bool) {
	values := nodeLogValues(diff.Current, trackedTaints, trackedAnnotations, matchedServer, matched)
	if diff.Added {
		logger.Info("node added", values...)
	}
	if diff.DeleteRequested {
		logger.Info("node delete requested", values...)
	}
	if diff.ReadyChanged {
		logger.Info("node ready condition changed", append(values, "oldReady", diff.Previous.Ready, "newReady", diff.Current.Ready)...)
	}
	if diff.UnschedulableChanged {
		logger.Info("node unschedulable changed", append(values, "oldUnschedulable", diff.Previous.Unschedulable, "newUnschedulable", diff.Current.Unschedulable)...)
	}
	for _, key := range diff.TaintsChanged {
		logger.Info("node tracked taint changed", append(values, "taint", key)...)
	}
	for _, key := range diff.AnnotationsChanged {
		logger.Info("node tracked annotation changed", append(values, "annotation", key)...)
	}
}

func nodeLogValues(snapshot NodeSnapshot, trackedTaints map[string]struct{}, trackedAnnotations map[string]struct{}, matchedServer KamateraServer, matched bool) []interface{} {
	return []interface{}{
		"ready", snapshot.Ready,
		"deleting", snapshot.Deleting,
		"unschedulable", snapshot.Unschedulable,
		"trackedTaints", trackedTaintPresence(snapshot, trackedTaints),
		"trackedAnnotations", trackedAnnotationPresence(snapshot, trackedAnnotations),
		"matchedServer", matched,
		"serverName", matchedServer.Name,
		"serverDatacenter", matchedServer.Datacenter,
		"serverPower", matchedServer.Power,
	}
}

func trackedTaintPresence(snapshot NodeSnapshot, trackedTaints map[string]struct{}) map[string]bool {
	values := map[string]bool{}
	for key := range trackedTaints {
		_, values[key] = snapshot.Taints[key]
	}
	return values
}

func trackedAnnotationPresence(snapshot NodeSnapshot, trackedAnnotations map[string]struct{}) map[string]bool {
	values := map[string]bool{}
	for key := range trackedAnnotations {
		_, values[key] = snapshot.Annotations[key]
	}
	return values
}

func (r *NodeListReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Log.GetSink() == nil {
		r.Log = ctrl.Log.WithName("controllers").WithName("NodeList")
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("node-list").
		For(&corev1.Node{}).
		Complete(r)
}
