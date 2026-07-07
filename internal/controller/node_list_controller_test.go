package controller

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type recordingLogSink struct {
	messages []string
	values   [][]interface{}
}

func (s *recordingLogSink) Init(logr.RuntimeInfo) {}
func (s *recordingLogSink) Enabled(int) bool      { return true }
func (s *recordingLogSink) Info(_ int, msg string, keysAndValues ...interface{}) {
	s.messages = append(s.messages, msg)
	s.values = append(s.values, keysAndValues)
}
func (s *recordingLogSink) Error(error, string, ...interface{})    {}
func (s *recordingLogSink) WithValues(...interface{}) logr.LogSink { return s }
func (s *recordingLogSink) WithName(string) logr.LogSink           { return s }

func TestNodeListReconcilerStoresNodeSnapshot(t *testing.T) {
	scheme := newTestScheme(t)
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Annotations: map[string]string{"track/me": "yes", "ignore/me": "no"}}}
	node.Spec.Unschedulable = true
	node.Spec.Taints = []corev1.Taint{
		{Key: "ToBeDeletedByClusterAutoscaler", Value: "true", Effect: corev1.TaintEffectNoSchedule},
		{Key: "DeletionCandidateOfClusterAutoscaler", Value: "true", Effect: corev1.TaintEffectNoExecute},
		{Key: "ignored", Value: "true", Effect: corev1.TaintEffectNoSchedule},
	}
	node.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).WithStatusSubresource(node).Build()
	store := NewNodeStateStore()

	r := &NodeListReconciler{
		Client:             c,
		Store:              store,
		TrackedAnnotations: parseTrackedKeys("track/me"),
		Log:                logr.Discard(),
	}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: node.Name}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	snapshot, ok := store.Get(node.Name)
	if !ok {
		t.Fatalf("expected node snapshot in store")
	}
	if snapshot.Ready != corev1.ConditionFalse {
		t.Fatalf("expected Ready false, got %s", snapshot.Ready)
	}
	if !snapshot.Unschedulable {
		t.Fatalf("expected unschedulable to be tracked")
	}
	if got := snapshot.Taints["ToBeDeletedByClusterAutoscaler"]; got.Value != "true" || got.Effect != corev1.TaintEffectNoSchedule {
		t.Fatalf("expected default autoscaler taint to be tracked, got %+v", got)
	}
	if got := snapshot.Taints["DeletionCandidateOfClusterAutoscaler"]; got.Value != "true" || got.Effect != corev1.TaintEffectNoExecute {
		t.Fatalf("expected default deletion candidate taint to be tracked, got %+v", got)
	}
	if _, ok := snapshot.Taints["ignored"]; ok {
		t.Fatalf("did not expect ignored taint")
	}
	if snapshot.Annotations["track/me"] != "yes" {
		t.Fatalf("expected tracked annotation")
	}
	if _, ok := snapshot.Annotations["ignore/me"]; ok {
		t.Fatalf("did not expect ignored annotation")
	}
}

func TestNodeListReconcilerDeletesMissingNodeFromStore(t *testing.T) {
	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	store := NewNodeStateStore()
	store.Replace(NodeSnapshot{Name: "node-1", Taints: map[string]TrackedTaint{}, Annotations: map[string]string{}})

	r := &NodeListReconciler{Client: c, Store: store, Log: logr.Discard()}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "node-1"}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if _, ok := store.Delete("node-1"); ok {
		t.Fatalf("expected missing node to be removed from store")
	}
}

func TestNodeListReconcilerLogDiffLogsAddedAndDeleteRequested(t *testing.T) {
	sink := &recordingLogSink{}
	r := &NodeListReconciler{}
	r.logDiff(logr.New(sink), NodeStateDiff{
		Added:           true,
		DeleteRequested: true,
		Current:         NodeSnapshot{Name: "node-1", Deleting: true},
	}, parseTrackedKeys(defaultTrackedTaintsCSV), parseTrackedKeys(""), KamateraServer{}, false)

	want := []string{"node added", "node delete requested"}
	if len(sink.messages) != len(want) {
		t.Fatalf("expected messages %v, got %v", want, sink.messages)
	}
	for i := range want {
		if sink.messages[i] != want[i] {
			t.Fatalf("expected messages %v, got %v", want, sink.messages)
		}
	}
}

func TestNodeListReconcilerLogDiffIncludesTrackedFieldsAndMatch(t *testing.T) {
	sink := &recordingLogSink{}
	r := &NodeListReconciler{}
	snapshot := NodeSnapshot{
		Name:          "worker1",
		Ready:         corev1.ConditionFalse,
		Unschedulable: true,
		Taints:        map[string]TrackedTaint{"taint/a": {Key: "taint/a"}},
		Annotations:   map[string]string{"annotation/a": "value"},
	}
	r.logDiff(logr.New(sink), NodeStateDiff{
		Added:   true,
		Current: snapshot,
	}, parseTrackedKeys("taint/a,taint/b"), parseTrackedKeys("annotation/a,annotation/b"), KamateraServer{Name: "server1", Datacenter: "EU", Power: "off"}, true)

	if len(sink.values) != 1 {
		t.Fatalf("expected one log line, got %d", len(sink.values))
	}
	values := logValuesMap(sink.values[0])
	if values["ready"] != corev1.ConditionFalse || values["unschedulable"] != true {
		t.Fatalf("expected node fields in log values, got %#v", values)
	}
	if values["matchedServer"] != true || values["serverName"] != "server1" || values["serverPower"] != "off" {
		t.Fatalf("expected matched server fields in log values, got %#v", values)
	}
	taints, ok := values["trackedTaints"].(map[string]bool)
	if !ok || !taints["taint/a"] || taints["taint/b"] {
		t.Fatalf("expected tracked taint booleans, got %#v", values["trackedTaints"])
	}
	annotations, ok := values["trackedAnnotations"].(map[string]bool)
	if !ok || !annotations["annotation/a"] || annotations["annotation/b"] {
		t.Fatalf("expected tracked annotation booleans, got %#v", values["trackedAnnotations"])
	}
}

func logValuesMap(values []interface{}) map[string]interface{} {
	mapped := map[string]interface{}{}
	for i := 0; i+1 < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			continue
		}
		mapped[key] = values[i+1]
	}
	return mapped
}

func TestNodeListReconcilerDoesNotTriggerDeleteForMatchedPoweredOffServer(t *testing.T) {
	scheme := newTestScheme(t)
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker1"}}
	node.Status.Conditions = []corev1.NodeCondition{{
		Type:               corev1.NodeReady,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.NewTime(now.Add(-20 * time.Minute)),
	}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()
	serverStore := NewServerStateStore()
	serverStore.Replace([]KamateraServer{{Name: "kamatera-worker1", Datacenter: "EU", Power: "off"}})
	matcher, err := NewNameMatcher("kamatera-%s", "")
	if err != nil {
		t.Fatalf("new matcher: %v", err)
	}

	r := &NodeListReconciler{
		Client:      c,
		Store:       NewNodeStateStore(),
		ServerStore: serverStore,
		Matcher:     matcher,
		Log:         logr.Discard(),
	}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: node.Name}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var got corev1.Node
	if err := c.Get(context.Background(), types.NamespacedName{Name: node.Name}, &got); err != nil {
		t.Fatalf("expected node list reconcile not to delete node: %v", err)
	}
}

func TestNodeListReconcilerDoesNotTriggerDeleteWhenNoServerMatchExists(t *testing.T) {
	scheme := newTestScheme(t)
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker1"}}
	node.Status.Conditions = []corev1.NodeCondition{{
		Type:               corev1.NodeReady,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.NewTime(now.Add(-20 * time.Minute)),
	}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).Build()
	r := &NodeListReconciler{
		Client:      c,
		Store:       NewNodeStateStore(),
		ServerStore: NewServerStateStore(),
		Log:         logr.Discard(),
	}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: node.Name}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var got corev1.Node
	if err := c.Get(context.Background(), types.NamespacedName{Name: node.Name}, &got); err != nil {
		t.Fatalf("expected node list reconcile not to delete node: %v", err)
	}
}
