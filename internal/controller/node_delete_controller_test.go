package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/go-logr/logr"
)

func TestNodeReconciler_DoesNotDeleteWithoutLabel(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add to scheme: %v", err)
	}

	node := &corev1.Node{}
	node.Name = "node-1"

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

	r := &NodeReconciler{
		Client:         c,
		DeleteLabelKey: "kamatera.io/delete",
		Log:            logr.Discard(),
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: node.Name}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var got corev1.Node
	if err := c.Get(context.Background(), types.NamespacedName{Name: node.Name}, &got); err != nil {
		t.Fatalf("expected node to still exist: %v", err)
	}
}

func TestNodeReconciler_DeletesWhenLabelMatches(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add to scheme: %v", err)
	}

	node := &corev1.Node{}
	node.Name = "node-1"
	node.Labels = map[string]string{"kamatera.io/delete": "true"}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

	r := &NodeReconciler{
		Client:         c,
		DeleteLabelKey: "kamatera.io/delete",
		Log:            logr.Discard(),
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

func TestNodeReconciler_DoesNotDeleteControlPlaneNode(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add to scheme: %v", err)
	}

	node := &corev1.Node{}
	node.Name = "node-1"
	node.Labels = map[string]string{
		"kamatera.io/delete":                    "true",
		"node-role.kubernetes.io/control-plane": "",
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

	r := &NodeReconciler{
		Client:         c,
		DeleteLabelKey: "kamatera.io/delete",
		Log:            logr.Discard(),
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
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add to scheme: %v", err)
	}

	node := &corev1.Node{}
	node.Name = "node-1"
	node.Labels = map[string]string{"kamatera.io/delete": "true"}
	node.Spec.Taints = []corev1.Taint{{Key: "node-role.kubernetes.io/control-plane"}}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

	r := &NodeReconciler{
		Client:         c,
		DeleteLabelKey: "kamatera.io/delete",
		Log:            logr.Discard(),
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

func TestNodeReconciler_RequiresLabelValueWhenConfigured(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add to scheme: %v", err)
	}

	node := &corev1.Node{}
	node.Name = "node-1"
	node.Labels = map[string]string{"kamatera.io/delete": "true"}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()

	r := &NodeReconciler{
		Client:           c,
		DeleteLabelKey:   "kamatera.io/delete",
		DeleteLabelValue: "yes",
		Log:              logr.Discard(),
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: node.Name}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var got corev1.Node
	if err := c.Get(context.Background(), types.NamespacedName{Name: node.Name}, &got); err != nil {
		t.Fatalf("expected node to still exist: %v", err)
	}
}
