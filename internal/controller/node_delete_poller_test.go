package controller

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNodeDeletePollerPollDeletesMatchedPoweredOffUnknownNode(t *testing.T) {
	scheme := newTestScheme(t)
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	node := &corev1.Node{}
	node.Name = "node-1"
	node.Status.Conditions = []corev1.NodeCondition{{
		Type:               corev1.NodeReady,
		Status:             corev1.ConditionUnknown,
		LastTransitionTime: metav1.NewTime(now.Add(-20 * time.Minute)),
	}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()
	nodeStore := NewNodeStateStore()
	nodeStore.Replace(NewNodeSnapshot(node, nil, nil))
	serverStore := NewServerStateStore()
	serverStore.Replace([]KamateraServer{{Name: node.Name, Datacenter: "EU", Power: "off"}})

	poller := &NodeDeletePoller{
		NodeStore: nodeStore,
		Reconciler: &NodeReconciler{
			Client:           c,
			ServerStore:      serverStore,
			NotReadyDuration: 15 * time.Minute,
			Now:              func() time.Time { return now },
			Log:              logr.Discard(),
		},
		Log: logr.Discard(),
	}
	if err := poller.poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}

	var got corev1.Node
	err := c.Get(context.Background(), types.NamespacedName{Name: node.Name}, &got)
	if err == nil || !apierrors.IsNotFound(err) {
		t.Fatalf("expected node to be deleted by poller, got err=%v", err)
	}
}

func TestNodeDeletePollerPollOnlyProcessesStoredNodes(t *testing.T) {
	scheme := newTestScheme(t)
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	storedNode := &corev1.Node{}
	storedNode.Name = "stored-node"
	storedNode.Status.Conditions = []corev1.NodeCondition{{
		Type:               corev1.NodeReady,
		Status:             corev1.ConditionUnknown,
		LastTransitionTime: metav1.NewTime(now.Add(-20 * time.Minute)),
	}}
	unstoredNode := &corev1.Node{}
	unstoredNode.Name = "unstored-node"
	unstoredNode.Status.Conditions = []corev1.NodeCondition{{
		Type:               corev1.NodeReady,
		Status:             corev1.ConditionUnknown,
		LastTransitionTime: metav1.NewTime(now.Add(-20 * time.Minute)),
	}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(storedNode, unstoredNode).Build()
	nodeStore := NewNodeStateStore()
	nodeStore.Replace(NewNodeSnapshot(storedNode, nil, nil))
	serverStore := NewServerStateStore()
	serverStore.Replace([]KamateraServer{
		{Name: storedNode.Name, Datacenter: "EU", Power: "off"},
		{Name: unstoredNode.Name, Datacenter: "EU", Power: "off"},
	})

	poller := &NodeDeletePoller{
		NodeStore: nodeStore,
		Reconciler: &NodeReconciler{
			Client:           c,
			ServerStore:      serverStore,
			NotReadyDuration: 15 * time.Minute,
			Now:              func() time.Time { return now },
			Log:              logr.Discard(),
		},
		Log: logr.Discard(),
	}
	if err := poller.poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}

	var got corev1.Node
	if err := c.Get(context.Background(), types.NamespacedName{Name: storedNode.Name}, &got); err == nil || !apierrors.IsNotFound(err) {
		t.Fatalf("expected stored node to be deleted, got err=%v", err)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Name: unstoredNode.Name}, &got); err != nil {
		t.Fatalf("expected unstored node to remain: %v", err)
	}
}

func TestNodeDeletePollerDefaultInterval(t *testing.T) {
	if got := (&NodeDeletePoller{}).interval(); got != time.Minute {
		t.Fatalf("expected default poll interval 1m, got %v", got)
	}
}

func TestNodeDeletePollerRequiresLeaderElection(t *testing.T) {
	if !(&NodeDeletePoller{}).NeedLeaderElection() {
		t.Fatalf("expected NodeDeletePoller to require leader election")
	}
}

func TestNodeDeletePollerStartDoesNotPollBeforeInterval(t *testing.T) {
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
	serverStore := NewServerStateStore()
	serverStore.Replace([]KamateraServer{{Name: node.Name, Datacenter: "EU", Power: "off"}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	poller := &NodeDeletePoller{
		PollInterval: time.Hour,
		Reconciler: &NodeReconciler{
			Client:           c,
			ServerStore:      serverStore,
			NotReadyDuration: 15 * time.Minute,
			Now:              func() time.Time { return now },
			Log:              logr.Discard(),
		},
		Log: logr.Discard(),
	}
	if err := poller.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	var got corev1.Node
	if err := c.Get(context.Background(), types.NamespacedName{Name: node.Name}, &got); err != nil {
		t.Fatalf("expected node to remain before first poll interval: %v", err)
	}
}
