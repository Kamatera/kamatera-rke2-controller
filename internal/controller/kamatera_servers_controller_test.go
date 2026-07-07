package controller

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestKamateraServersControllerPollFiltersAndStoresServers(t *testing.T) {
	filter, err := NewServerFilter("EU", "cwmc-*")
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	store := NewServerStateStore()
	kclient := kamateraClientMock{}
	kclient.On("ListServers", context.Background()).Return([]KamateraServer{
		{Name: "cwmc-worker1", Datacenter: "EU", Power: "on"},
		{Name: "other-worker", Datacenter: "EU", Power: "on"},
		{Name: "cwmc-worker2", Datacenter: "US", Power: "off"},
	}, nil)

	controller := KamateraServersController{Client: &kclient, Store: store, Filter: filter, Log: logr.Discard()}
	if err := controller.poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}

	server, ok := store.Get("cwmc-worker1")
	if !ok || server.Power != "on" {
		t.Fatalf("expected matching server in store, got %+v ok=%v", server, ok)
	}
	if _, ok := store.Get("other-worker"); ok {
		t.Fatalf("did not expect non-matching name in store")
	}
	if _, ok := store.Get("cwmc-worker2"); ok {
		t.Fatalf("did not expect non-matching datacenter in store")
	}
}

func TestKamateraServersControllerRequiresLeaderElection(t *testing.T) {
	controller := KamateraServersController{}
	if !controller.NeedLeaderElection() {
		t.Fatalf("expected Kamatera server controller to run only under leader election")
	}
}

func TestKamateraServersControllerLogDiffIncludesMatchedNodeFields(t *testing.T) {
	nodeStore := NewNodeStateStore()
	nodeStore.Replace(NodeSnapshot{Name: "worker1", Ready: "False", Unschedulable: true, Taints: map[string]TrackedTaint{}, Annotations: map[string]string{}})
	matcher, err := NewNameMatcher("kamatera-%s", "")
	if err != nil {
		t.Fatalf("new matcher: %v", err)
	}
	sink := &recordingLogSink{}
	controller := KamateraServersController{NodeStore: nodeStore, Matcher: matcher, Log: logr.New(sink)}

	controller.logDiff(ServerStateDiff{Initial: true, Current: []KamateraServer{{Name: "kamatera-worker1", Datacenter: "EU", Power: "off"}}})

	if len(sink.values) != 1 {
		t.Fatalf("expected one log line, got %d", len(sink.values))
	}
	values := logValuesMap(sink.values[0])
	if values["matchedNode"] != true || values["nodeName"] != "worker1" || values["nodeUnschedulable"] != true {
		t.Fatalf("expected matched node fields in server log values, got %#v", values)
	}
	if values["name"] != "kamatera-worker1" || values["datacenter"] != "EU" || values["power"] != "off" {
		t.Fatalf("expected server fields in log values, got %#v", values)
	}
}

func TestKamateraServersControllerPollDoesNotTriggerDeleteForMatchedPoweredOffNode(t *testing.T) {
	scheme := newTestScheme(t)
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker1"}}
	node.Status.Conditions = []corev1.NodeCondition{{
		Type:               corev1.NodeReady,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.NewTime(now.Add(-20 * time.Minute)),
	}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()
	nodeStore := NewNodeStateStore()
	nodeStore.Replace(NewNodeSnapshot(node, parseTrackedKeys(defaultTrackedTaintsCSV), nil))
	serverStore := NewServerStateStore()
	kclient := kamateraClientMock{}
	kclient.On("ListServers", context.Background()).Return([]KamateraServer{{Name: "kamatera-worker1", Datacenter: "EU", Power: "off"}}, nil)
	matcher, err := NewNameMatcher("kamatera-%s", "")
	if err != nil {
		t.Fatalf("new matcher: %v", err)
	}

	controller := KamateraServersController{
		Client:    &kclient,
		Store:     serverStore,
		NodeStore: nodeStore,
		Matcher:   matcher,
		Log:       logr.Discard(),
	}

	if err := controller.poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}

	var got corev1.Node
	err = kubeClient.Get(context.Background(), types.NamespacedName{Name: node.Name}, &got)
	if err != nil {
		t.Fatalf("expected Kamatera poll not to delete matched node: %v", err)
	}
}

func TestKamateraServersControllerPollDoesNotDeleteUnmatchedNode(t *testing.T) {
	scheme := newTestScheme(t)
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker1"}}
	node.Status.Conditions = []corev1.NodeCondition{{
		Type:               corev1.NodeReady,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.NewTime(now.Add(-20 * time.Minute)),
	}}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()
	nodeStore := NewNodeStateStore()
	nodeStore.Replace(NewNodeSnapshot(node, parseTrackedKeys(defaultTrackedTaintsCSV), nil))
	serverStore := NewServerStateStore()
	kclient := kamateraClientMock{}
	kclient.On("ListServers", context.Background()).Return([]KamateraServer{{Name: "other-worker", Datacenter: "EU", Power: "off"}}, nil)

	controller := KamateraServersController{
		Client:    &kclient,
		Store:     serverStore,
		NodeStore: nodeStore,
		Log:       logr.Discard(),
	}

	if err := controller.poll(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}

	var got corev1.Node
	if err := kubeClient.Get(context.Background(), types.NamespacedName{Name: node.Name}, &got); err != nil {
		t.Fatalf("expected unmatched node to remain: %v", err)
	}
}
