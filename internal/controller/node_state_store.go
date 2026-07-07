package controller

import (
	"sort"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
)

const defaultTrackedTaintsCSV = "ToBeDeletedByClusterAutoscaler,DeletionCandidateOfClusterAutoscaler"

type NodeSnapshot struct {
	Name          string
	Ready         corev1.ConditionStatus
	Deleting      bool
	Unschedulable bool
	Taints        map[string]TrackedTaint
	Annotations   map[string]string
}

type TrackedTaint struct {
	Key    string
	Value  string
	Effect corev1.TaintEffect
}

type NodeStateStore struct {
	mu    sync.RWMutex
	nodes map[string]NodeSnapshot
}

type NodeStateDiff struct {
	Initial              bool
	Added                bool
	ReadyChanged         bool
	DeleteRequested      bool
	UnschedulableChanged bool
	TaintsChanged        []string
	AnnotationsChanged   []string
	Current              NodeSnapshot
	Previous             NodeSnapshot
}

func NewNodeStateStore() *NodeStateStore {
	return &NodeStateStore{nodes: map[string]NodeSnapshot{}}
}

func DefaultTrackedTaintsCSV() string {
	return defaultTrackedTaintsCSV
}

func ParseTrackedKeys(csv string) map[string]struct{} {
	return parseTrackedKeys(csv)
}

func parseTrackedKeys(csv string) map[string]struct{} {
	keys := map[string]struct{}{}
	for _, key := range strings.Split(csv, ",") {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		keys[key] = struct{}{}
	}
	return keys
}

func NewNodeSnapshot(node *corev1.Node, trackedTaints map[string]struct{}, trackedAnnotations map[string]struct{}) NodeSnapshot {
	snapshot := NodeSnapshot{
		Name:          node.Name,
		Ready:         nodeReadyStatus(node),
		Deleting:      node.DeletionTimestamp != nil,
		Unschedulable: node.Spec.Unschedulable,
		Taints:        map[string]TrackedTaint{},
		Annotations:   map[string]string{},
	}
	for _, taint := range node.Spec.Taints {
		if _, ok := trackedTaints[taint.Key]; ok {
			snapshot.Taints[taint.Key] = TrackedTaint{Key: taint.Key, Value: taint.Value, Effect: taint.Effect}
		}
	}
	for key, value := range node.Annotations {
		if _, ok := trackedAnnotations[key]; ok {
			snapshot.Annotations[key] = value
		}
	}
	return snapshot
}

func (s *NodeStateStore) Replace(snapshot NodeSnapshot) NodeStateDiff {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.nodes[snapshot.Name]
	current := copyNodeSnapshot(snapshot)
	diff := NodeStateDiff{Initial: !existed, Added: !existed, Current: copyNodeSnapshot(current), Previous: copyNodeSnapshot(previous)}
	diff.DeleteRequested = !previous.Deleting && current.Deleting
	if existed {
		diff.ReadyChanged = previous.Ready != current.Ready
		diff.UnschedulableChanged = previous.Unschedulable != current.Unschedulable
		diff.TaintsChanged = changedTaintKeys(previous.Taints, current.Taints)
		diff.AnnotationsChanged = changedAnnotationKeys(previous.Annotations, current.Annotations)
	}
	s.nodes[snapshot.Name] = current
	return diff
}

func (s *NodeStateStore) Delete(name string) (NodeSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.nodes[name]
	if ok {
		delete(s.nodes, name)
	}
	return copyNodeSnapshot(previous), ok
}

func (s *NodeStateStore) Get(name string) (NodeSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot, ok := s.nodes[name]
	return copyNodeSnapshot(snapshot), ok
}

func (s *NodeStateStore) List() []NodeSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	nodes := make([]NodeSnapshot, 0, len(s.nodes))
	for _, snapshot := range s.nodes {
		nodes = append(nodes, copyNodeSnapshot(snapshot))
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return nodes
}

func copyNodeSnapshot(snapshot NodeSnapshot) NodeSnapshot {
	snapshot.Taints = copyTrackedTaints(snapshot.Taints)
	snapshot.Annotations = copyStringMap(snapshot.Annotations)
	return snapshot
}

func copyTrackedTaints(values map[string]TrackedTaint) map[string]TrackedTaint {
	if values == nil {
		return nil
	}
	copied := make(map[string]TrackedTaint, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func copyStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func changedTaintKeys(previous map[string]TrackedTaint, current map[string]TrackedTaint) []string {
	changed := map[string]struct{}{}
	for key, currentValue := range current {
		if previousValue, ok := previous[key]; !ok || previousValue != currentValue {
			changed[key] = struct{}{}
		}
	}
	for key := range previous {
		if _, ok := current[key]; !ok {
			changed[key] = struct{}{}
		}
	}
	return sortedKeys(changed)
}

func changedAnnotationKeys(previous map[string]string, current map[string]string) []string {
	changed := map[string]struct{}{}
	for key, currentValue := range current {
		if previousValue, ok := previous[key]; !ok || previousValue != currentValue {
			changed[key] = struct{}{}
		}
	}
	for key := range previous {
		if _, ok := current[key]; !ok {
			changed[key] = struct{}{}
		}
	}
	return sortedKeys(changed)
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
