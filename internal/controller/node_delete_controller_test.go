package controller

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add to scheme: %v", err)
	}

	return scheme
}

func TestNodeReconciler_DoesNotDeleteReadyNode(t *testing.T) {
	scheme := newTestScheme(t)

	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	node := &corev1.Node{}
	node.Name = "node-1"
	node.Status.Conditions = []corev1.NodeCondition{{
		Type:               corev1.NodeReady,
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.NewTime(now.Add(-1 * time.Hour)),
	}}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

	r := &NodeReconciler{
		Client: c,
		Now:    func() time.Time { return now },
		Log:    logr.Discard(),
	}

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: node.Name}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter != 0 {
		t.Fatalf("expected no requeue for Ready node, got %v", res.RequeueAfter)
	}

	var got corev1.Node
	if err := c.Get(context.Background(), types.NamespacedName{Name: node.Name}, &got); err != nil {
		t.Fatalf("expected node to still exist: %v", err)
	}
}

func TestNodeReconciler_RequeuesUntilNotReadyDuration(t *testing.T) {
	scheme := newTestScheme(t)

	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	node := &corev1.Node{}
	node.Name = "node-1"
	node.Status.Conditions = []corev1.NodeCondition{{
		Type:               corev1.NodeReady,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.NewTime(now.Add(-10 * time.Minute)),
	}}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

	r := &NodeReconciler{
		Client:           c,
		NotReadyDuration: 15 * time.Minute,
		Now:              func() time.Time { return now },
		Log:              logr.Discard(),
	}

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: node.Name}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter != 5*time.Minute {
		t.Fatalf("expected requeue after 5m, got %v", res.RequeueAfter)
	}

	var got corev1.Node
	if err := c.Get(context.Background(), types.NamespacedName{Name: node.Name}, &got); err != nil {
		t.Fatalf("expected node to still exist: %v", err)
	}
}

func TestNodeReconciler_DeletesWhenNotReadyTooLongAndServerNotRunning(t *testing.T) {
	scheme := newTestScheme(t)

	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	node := &corev1.Node{}
	node.Name = "node-1"
	node.Status.Conditions = []corev1.NodeCondition{{
		Type:               corev1.NodeReady,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.NewTime(now.Add(-20 * time.Minute)),
	}}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

	kclient := kamateraClientMock{}
	kclient.On("IsServerRunning", context.Background(), node.Name).Return(false, nil)

	r := &NodeReconciler{
		Client:            c,
		NotReadyDuration:  15 * time.Minute,
		Now:               func() time.Time { return now },
		Log:               logr.Discard(),
		kamateraAPIClient: &kclient,
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: node.Name}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var got corev1.Node
	err = c.Get(context.Background(), types.NamespacedName{Name: node.Name}, &got)
	if err == nil || !apierrors.IsNotFound(err) {
		t.Fatalf("expected node to be deleted, got err=%v", err)
	}
}

func TestNodeReconciler_DoesNotDeleteWhenServerRunning(t *testing.T) {
	scheme := newTestScheme(t)

	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	node := &corev1.Node{}
	node.Name = "node-1"
	node.Status.Conditions = []corev1.NodeCondition{{
		Type:               corev1.NodeReady,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.NewTime(now.Add(-20 * time.Minute)),
	}}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

	kclient := kamateraClientMock{}
	kclient.On("IsServerRunning", context.Background(), node.Name).Return(true, nil)

	r := &NodeReconciler{
		Client:                       c,
		NotReadyDuration:             15 * time.Minute,
		ServerRunningRecheckInterval: 3 * time.Minute,
		Now:                          func() time.Time { return now },
		Log:                          logr.Discard(),
		kamateraAPIClient:            &kclient,
	}

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: node.Name}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.RequeueAfter != 3*time.Minute {
		t.Fatalf("expected requeue after 3m, got %v", res.RequeueAfter)
	}

	var got corev1.Node
	if err := c.Get(context.Background(), types.NamespacedName{Name: node.Name}, &got); err != nil {
		t.Fatalf("expected node to still exist: %v", err)
	}
}

func TestNodeReconciler_DoesNotDeleteControlPlaneNode(t *testing.T) {
	scheme := newTestScheme(t)

	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	node := &corev1.Node{}
	node.Name = "node-1"
	node.Labels = map[string]string{"node-role.kubernetes.io/control-plane": ""}
	node.Status.Conditions = []corev1.NodeCondition{{
		Type:               corev1.NodeReady,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.NewTime(now.Add(-20 * time.Minute)),
	}}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

	r := &NodeReconciler{
		Client:           c,
		NotReadyDuration: 15 * time.Minute,
		Now:              func() time.Time { return now },
		Log:              logr.Discard(),
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: node.Name}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var got corev1.Node
	if err := c.Get(context.Background(), types.NamespacedName{Name: node.Name}, &got); err != nil {
		t.Fatalf("expected control-plane node to still exist: %v", err)
	}
}

func TestNodeReconciler_DoesNotDeleteNodeWithControlPlaneTaint(t *testing.T) {
	scheme := newTestScheme(t)

	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	node := &corev1.Node{}
	node.Name = "node-1"
	node.Spec.Taints = []corev1.Taint{{Key: "node-role.kubernetes.io/control-plane"}}
	node.Status.Conditions = []corev1.NodeCondition{{
		Type:               corev1.NodeReady,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.NewTime(now.Add(-20 * time.Minute)),
	}}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

	r := &NodeReconciler{
		Client:           c,
		NotReadyDuration: 15 * time.Minute,
		Now:              func() time.Time { return now },
		Log:              logr.Discard(),
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: node.Name}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var got corev1.Node
	if err := c.Get(context.Background(), types.NamespacedName{Name: node.Name}, &got); err != nil {
		t.Fatalf("expected tainted node to still exist: %v", err)
	}
}
