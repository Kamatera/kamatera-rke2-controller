package controller

import (
	"context"
	"os"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	defaultNotReadyDuration             = 15 * time.Minute
	defaultServerRunningRecheckInterval = 5 * time.Minute
)

// NodeReconciler deletes Node objects that have been NotReady for longer than
// NotReadyDuration and whose corresponding Kamatera server is not running.
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

	kamateraAPIClient kamateraAPIClient
}

// Reconcile implements the reconciliation loop for Node objects.
// it deletes Nodes that have been NotReady for longer than NotReadyDuration
// and whose corresponding Kamatera server is not running.
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
		logger.Info("skipping deletion of control-plane node")
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

	// could not get ready condition, nothing to do
	readyCondition := nodeReadyCondition(&node)
	if readyCondition == nil {
		return ctrl.Result{}, nil
	}

	// node is ready, nothing to do
	if readyCondition.Status == corev1.ConditionTrue {
		return ctrl.Result{}, nil
	}

	notReadySince := readyCondition.LastTransitionTime.Time
	if notReadySince.IsZero() {
		notReadySince = now
	}

	notReadyFor := now.Sub(notReadySince)
	if notReadyFor < notReadyDuration {
		requeueAfter := notReadyDuration - notReadyFor
		logger.V(1).Info("node not ready yet", "notReadyFor", notReadyFor, "requeueAfter", requeueAfter)
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	isServerRunning, err := r.kamateraAPIClient.IsServerRunning(ctx, node.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if isServerRunning {
		logger.V(1).Info("node is NotReady but Kamatera server is still running")
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
		"notReadyFor", notReadyFor,
		"name", node.Name,
	)
	return ctrl.Result{}, nil
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

// shouldReconcile determines whether the given object should trigger a reconciliation.
// It returns true if the object is a Node that is not being deleted and is NotReady.
func (r *NodeReconciler) shouldReconcile(obj client.Object) bool {
	node, ok := obj.(*corev1.Node)
	if !ok {
		return false
	}
	if node.DeletionTimestamp != nil {
		return false
	}

	readyCondition := nodeReadyCondition(node)
	if readyCondition == nil {
		return false
	}

	return readyCondition.Status != corev1.ConditionTrue
}

func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Log.GetSink() == nil {
		r.Log = ctrl.Log.WithName("controllers").WithName("Node")
	}

	kamateraApiUrl := os.Getenv("KAMATERA_API_URL")
	if kamateraApiUrl == "" {
		kamateraApiUrl = "https://cloudcli.cloudwm.com"
	}
	r.kamateraAPIClient = buildKamateraAPIClient(
		os.Getenv("KAMATERA_API_CLIENT_ID"),
		os.Getenv("KAMATERA_API_SECRET"),
		kamateraApiUrl,
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return r.shouldReconcile(e.Object)
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				// Reconcile if the Node's Ready status has changed.
				return nodeReadyStatus(e.ObjectNew) != nodeReadyStatus(e.ObjectOld)
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
