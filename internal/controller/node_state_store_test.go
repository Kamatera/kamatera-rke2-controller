package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewNodeSnapshotTracksUnschedulableTaintsAndAnnotations(t *testing.T) {
	node := &corev1.Node{}
	node.Name = "node-1"
	node.Spec.Unschedulable = true
	node.Spec.Taints = []corev1.Taint{
		{Key: "ToBeDeletedByClusterAutoscaler", Value: "true", Effect: corev1.TaintEffectNoSchedule},
		{Key: "DeletionCandidateOfClusterAutoscaler", Value: "true", Effect: corev1.TaintEffectNoSchedule},
		{Key: "ignored", Value: "true", Effect: corev1.TaintEffectNoSchedule},
	}
	node.Annotations = map[string]string{"track/me": "yes", "ignore/me": "no"}
	node.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}}

	snapshot := NewNodeSnapshot(node, parseTrackedKeys(defaultTrackedTaintsCSV), parseTrackedKeys("track/me"))
	if !snapshot.Unschedulable {
		t.Fatalf("expected unschedulable to be tracked")
	}
	if snapshot.Ready != corev1.ConditionFalse {
		t.Fatalf("expected Ready false, got %s", snapshot.Ready)
	}
	if _, ok := snapshot.Taints["ToBeDeletedByClusterAutoscaler"]; !ok {
		t.Fatalf("expected autoscaler taint to be tracked")
	}
	if _, ok := snapshot.Taints["DeletionCandidateOfClusterAutoscaler"]; !ok {
		t.Fatalf("expected deletion candidate taint to be tracked")
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

func TestDefaultTrackedTaintsCSVExposesAutoscalerDefaults(t *testing.T) {
	if DefaultTrackedTaintsCSV() != "ToBeDeletedByClusterAutoscaler,DeletionCandidateOfClusterAutoscaler" {
		t.Fatalf("unexpected default tracked taints: %q", DefaultTrackedTaintsCSV())
	}
}

func TestParseTrackedKeysExposesCSVParsing(t *testing.T) {
	keys := ParseTrackedKeys(" first,second, first, ")
	if len(keys) != 2 {
		t.Fatalf("expected 2 parsed keys, got %d", len(keys))
	}
	if _, ok := keys["first"]; !ok {
		t.Fatalf("expected first key")
	}
	if _, ok := keys["second"]; !ok {
		t.Fatalf("expected second key")
	}
}

func TestNodeStateStoreDefensivelyCopiesInputSnapshots(t *testing.T) {
	store := NewNodeStateStore()
	snapshot := NodeSnapshot{
		Name:        "node-1",
		Ready:       corev1.ConditionTrue,
		Taints:      map[string]TrackedTaint{"taint-1": {Key: "taint-1", Value: "before", Effect: corev1.TaintEffectNoSchedule}},
		Annotations: map[string]string{"annotation-1": "before"},
	}

	store.Replace(snapshot)
	snapshot.Taints["taint-1"] = TrackedTaint{Key: "taint-1", Value: "after", Effect: corev1.TaintEffectNoSchedule}
	snapshot.Annotations["annotation-1"] = "after"

	diff := store.Replace(NodeSnapshot{
		Name:        "node-1",
		Ready:       corev1.ConditionTrue,
		Taints:      map[string]TrackedTaint{"taint-1": {Key: "taint-1", Value: "before", Effect: corev1.TaintEffectNoSchedule}},
		Annotations: map[string]string{"annotation-1": "before"},
	})
	if len(diff.TaintsChanged) != 0 {
		t.Fatalf("did not expect input taint mutation to affect store, got changes: %+v", diff.TaintsChanged)
	}
	if len(diff.AnnotationsChanged) != 0 {
		t.Fatalf("did not expect input annotation mutation to affect store, got changes: %+v", diff.AnnotationsChanged)
	}
}

func TestNodeStateStoreDefensivelyCopiesReturnedSnapshots(t *testing.T) {
	store := NewNodeStateStore()
	store.Replace(NodeSnapshot{
		Name:        "node-1",
		Ready:       corev1.ConditionTrue,
		Taints:      map[string]TrackedTaint{"taint-1": {Key: "taint-1", Value: "before", Effect: corev1.TaintEffectNoSchedule}},
		Annotations: map[string]string{"annotation-1": "before"},
	})

	diff := store.Replace(NodeSnapshot{
		Name:        "node-1",
		Ready:       corev1.ConditionTrue,
		Taints:      map[string]TrackedTaint{"taint-1": {Key: "taint-1", Value: "before", Effect: corev1.TaintEffectNoSchedule}},
		Annotations: map[string]string{"annotation-1": "before"},
	})
	diff.Current.Taints["taint-1"] = TrackedTaint{Key: "taint-1", Value: "after", Effect: corev1.TaintEffectNoSchedule}
	diff.Current.Annotations["annotation-1"] = "after"
	diff.Previous.Taints["taint-1"] = TrackedTaint{Key: "taint-1", Value: "after", Effect: corev1.TaintEffectNoSchedule}
	diff.Previous.Annotations["annotation-1"] = "after"

	diff = store.Replace(NodeSnapshot{
		Name:        "node-1",
		Ready:       corev1.ConditionTrue,
		Taints:      map[string]TrackedTaint{"taint-1": {Key: "taint-1", Value: "before", Effect: corev1.TaintEffectNoSchedule}},
		Annotations: map[string]string{"annotation-1": "before"},
	})
	if len(diff.TaintsChanged) != 0 {
		t.Fatalf("did not expect returned taint mutation to affect store, got changes: %+v", diff.TaintsChanged)
	}
	if len(diff.AnnotationsChanged) != 0 {
		t.Fatalf("did not expect returned annotation mutation to affect store, got changes: %+v", diff.AnnotationsChanged)
	}

	deleted, ok := store.Delete("node-1")
	if !ok {
		t.Fatalf("expected deleted snapshot")
	}
	deleted.Taints["taint-1"] = TrackedTaint{Key: "taint-1", Value: "after", Effect: corev1.TaintEffectNoSchedule}
	deleted.Annotations["annotation-1"] = "after"
}

func TestNodeStateStoreGetReturnsDefensiveCopy(t *testing.T) {
	store := NewNodeStateStore()
	store.Replace(NodeSnapshot{
		Name:        "node-1",
		Ready:       corev1.ConditionTrue,
		Taints:      map[string]TrackedTaint{"taint-1": {Key: "taint-1", Value: "before", Effect: corev1.TaintEffectNoSchedule}},
		Annotations: map[string]string{"annotation-1": "before"},
	})

	snapshot, ok := store.Get("node-1")
	if !ok {
		t.Fatalf("expected snapshot")
	}
	snapshot.Taints["taint-1"] = TrackedTaint{Key: "taint-1", Value: "after", Effect: corev1.TaintEffectNoSchedule}
	snapshot.Annotations["annotation-1"] = "after"

	snapshot, ok = store.Get("node-1")
	if !ok {
		t.Fatalf("expected snapshot")
	}
	if snapshot.Taints["taint-1"].Value != "before" {
		t.Fatalf("expected Get to return a defensive taint copy, got %+v", snapshot.Taints["taint-1"])
	}
	if snapshot.Annotations["annotation-1"] != "before" {
		t.Fatalf("expected Get to return a defensive annotation copy, got %q", snapshot.Annotations["annotation-1"])
	}
	if _, ok := store.Get("missing"); ok {
		t.Fatalf("did not expect missing snapshot")
	}
}

func TestNodeStateStoreDetectsChanges(t *testing.T) {
	store := NewNodeStateStore()

	initial := NodeSnapshot{Name: "node-1", Ready: corev1.ConditionTrue, Taints: map[string]TrackedTaint{}, Annotations: map[string]string{}}
	diff := store.Replace(initial)
	if !diff.Initial || !diff.Added {
		t.Fatalf("expected initial added diff: %+v", diff)
	}

	next := NodeSnapshot{
		Name:          "node-1",
		Ready:         corev1.ConditionFalse,
		Deleting:      true,
		Unschedulable: true,
		Taints:        map[string]TrackedTaint{"ToBeDeletedByClusterAutoscaler": {Key: "ToBeDeletedByClusterAutoscaler", Value: "true", Effect: corev1.TaintEffectNoSchedule}},
		Annotations:   map[string]string{"track/me": "yes"},
	}
	diff = store.Replace(next)
	if !diff.ReadyChanged || !diff.DeleteRequested || !diff.UnschedulableChanged {
		t.Fatalf("expected ready/delete/unschedulable changes: %+v", diff)
	}
	if len(diff.TaintsChanged) != 1 || diff.TaintsChanged[0] != "ToBeDeletedByClusterAutoscaler" {
		t.Fatalf("unexpected taint changes: %+v", diff.TaintsChanged)
	}
	if len(diff.AnnotationsChanged) != 1 || diff.AnnotationsChanged[0] != "track/me" {
		t.Fatalf("unexpected annotation changes: %+v", diff.AnnotationsChanged)
	}

	deleted, ok := store.Delete("node-1")
	if !ok || deleted.Name != "node-1" {
		t.Fatalf("expected deleted snapshot, got %+v ok=%v", deleted, ok)
	}
	if _, ok := store.Delete("node-1"); ok {
		t.Fatalf("did not expect second delete to find node")
	}
}

func TestNodeStateStoreInitialDeletingNodeSetsDeleteRequested(t *testing.T) {
	store := NewNodeStateStore()

	diff := store.Replace(NodeSnapshot{Name: "node-1", Deleting: true, Taints: map[string]TrackedTaint{}, Annotations: map[string]string{}})
	if !diff.Initial || !diff.Added || !diff.DeleteRequested {
		t.Fatalf("expected initial added deleting node to request delete: %+v", diff)
	}
}

func TestNewNodeSnapshotDetectsDeletingNode(t *testing.T) {
	now := metav1.Now()
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", DeletionTimestamp: &now}}
	snapshot := NewNodeSnapshot(node, nil, nil)
	if !snapshot.Deleting {
		t.Fatalf("expected deleting snapshot")
	}
}
