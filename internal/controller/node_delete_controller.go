package controller

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultNotReadyDuration             = 15 * time.Minute
	defaultServerRunningRecheckInterval = 5 * time.Minute
)

// NodeReconciler deletes Node objects that have been NotReady for longer than
// NotReadyDuration and whose matching Kamatera server is present in the
// snapshot with power=off.
//
// The reconciler is intentionally minimal: it does not cordon/drain, and it
// refuses to delete control-plane nodes unless AllowControlPlane is set.
//
// This controller is meant to run in-cluster.
type NodeReconciler struct {
	client.Client

	NotReadyDuration             time.Duration
	ServerRunningRecheckInterval time.Duration

	AllowControlPlane bool

	Now func() time.Time

	Log logr.Logger

	ServerStore *ServerStateStore
	Matcher     NameMatcher

	ExtraLogValues []interface{}
}

// Reconcile implements the reconciliation loop for Node objects.
// it deletes Nodes that have been NotReady for longer than NotReadyDuration
// and whose matching Kamatera server is present in the snapshot with power=off.
func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("node", req.Name)

	var node corev1.Node
	if err := r.Get(ctx, req.NamespacedName, &node); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Skip if the Node is being deleted.
	if node.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}

	// Skip control-plane nodes if not allowed.
	if !r.AllowControlPlane && isControlPlaneNode(&node) {
		logger.V(2).Info("skipping deletion of control-plane node", r.ExtraLogValues...)
		return ctrl.Result{}, nil
	}

	// Determine current time, for testing
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}

	notReadyDuration := r.NotReadyDuration
	if notReadyDuration <= 0 {
		notReadyDuration = defaultNotReadyDuration
	}

	serverRunningRecheckInterval := r.ServerRunningRecheckInterval
	if serverRunningRecheckInterval <= 0 {
		serverRunningRecheckInterval = defaultServerRunningRecheckInterval
	}

	readyCondition := nodeReadyCondition(&node)
	if readyCondition != nil && readyCondition.Status == corev1.ConditionTrue {
		return ctrl.Result{}, nil
	}

	notReadySince := nodeNotReadySince(&node, readyCondition, now)
	notReadyFor := now.Sub(notReadySince)
	if notReadyFor < 0 {
		notReadyFor = 0
	}
	if notReadyFor < notReadyDuration {
		requeueAfter := notReadyDuration - notReadyFor
		logger.V(1).Info("node not ready yet", append(r.ExtraLogValues, "notReadyFor", notReadyFor, "requeueAfter", requeueAfter)...)
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	if r.ServerStore == nil {
		logger.Info("node is NotReady but Kamatera server snapshot is unavailable", r.ExtraLogValues...)
		return ctrl.Result{RequeueAfter: serverRunningRecheckInterval}, nil
	}
	server, ok := r.Matcher.FindServerForNode(node.Name, r.ServerStore)
	if !ok {
		logger.Info("node is NotReady but matching Kamatera server is absent from snapshot", r.ExtraLogValues...)
		return ctrl.Result{RequeueAfter: serverRunningRecheckInterval}, nil
	}
	if server.Power != "off" {
		logger.V(1).Info("node is NotReady but Kamatera server is not powered off", append(r.ExtraLogValues, "power", server.Power)...)
		return ctrl.Result{RequeueAfter: serverRunningRecheckInterval}, nil
	}

	if err := r.Delete(ctx, &node); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info(
		"deleted node due to NotReady timeout and stopped Kamatera server",
		append(r.ExtraLogValues, "notReadyFor", notReadyFor, "name", node.Name)...,
	)
	return ctrl.Result{}, nil
}

func nodeNotReadySince(node *corev1.Node, readyCondition *corev1.NodeCondition, now time.Time) time.Time {
	if readyCondition != nil {
		notReadySince := readyCondition.LastTransitionTime.Time
		if !notReadySince.IsZero() {
			return notReadySince
		}
	}
	if node != nil && !node.CreationTimestamp.IsZero() {
		return node.CreationTimestamp.Time
	}
	return now
}

func nodeReadyCondition(node *corev1.Node) *corev1.NodeCondition {
	if node == nil {
		return nil
	}

	for i := range node.Status.Conditions {
		if node.Status.Conditions[i].Type == corev1.NodeReady {
			return &node.Status.Conditions[i]
		}
	}

	return nil
}

func nodeReadyStatus(obj client.Object) corev1.ConditionStatus {
	node, ok := obj.(*corev1.Node)
	if !ok {
		return corev1.ConditionUnknown
	}

	condition := nodeReadyCondition(node)
	if condition == nil {
		return corev1.ConditionUnknown
	}

	return condition.Status
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
