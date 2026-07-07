package controller

import (
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
)

func TestSnapshotLoggerLogsMatchedAndUnmatchedSnapshots(t *testing.T) {
	serverStore := NewServerStateStore()
	serverStore.Replace([]KamateraServer{
		{Name: "server-node-1", Datacenter: "EU", Power: "on"},
		{Name: "server-only", Datacenter: "EU", Power: "off"},
	})
	nodeStore := NewNodeStateStore()
	nodeStore.Replace(NodeSnapshot{Name: "node-1", Ready: corev1.ConditionTrue, Taints: map[string]TrackedTaint{}, Annotations: map[string]string{}})
	nodeStore.Replace(NodeSnapshot{Name: "node-only", Ready: corev1.ConditionUnknown, Taints: map[string]TrackedTaint{}, Annotations: map[string]string{}})
	matcher, err := NewNameMatcher("server-%s", "")
	if err != nil {
		t.Fatalf("new matcher: %v", err)
	}
	sink := &recordingLogSink{}
	logger := SnapshotLogger{ServerStore: serverStore, NodeStore: nodeStore, Matcher: matcher, Log: logr.New(sink)}

	logger.logSnapshots()

	if len(sink.messages) != 3 {
		t.Fatalf("expected 3 snapshot log lines, got %d: %v", len(sink.messages), sink.messages)
	}
	if sink.messages[0] != "snapshot node/server match" {
		t.Fatalf("expected first line to be matched snapshot, got %q", sink.messages[0])
	}
	matched := logValuesMap(sink.values[0])
	if matched["nodeName"] != "node-1" || matched["nodeReady"] != corev1.ConditionTrue || matched["serverName"] != "server-node-1" || matched["serverPower"] != "on" {
		t.Fatalf("unexpected matched log fields: %#v", matched)
	}
	if sink.messages[1] != "snapshot node unmatched" {
		t.Fatalf("expected second line to be unmatched node, got %q", sink.messages[1])
	}
	unmatchedNode := logValuesMap(sink.values[1])
	if unmatchedNode["nodeName"] != "node-only" || unmatchedNode["nodeReady"] != corev1.ConditionUnknown {
		t.Fatalf("unexpected unmatched node log fields: %#v", unmatchedNode)
	}
	if _, ok := unmatchedNode["serverPower"]; ok {
		t.Fatalf("did not expect server fields on unmatched node line: %#v", unmatchedNode)
	}
	if sink.messages[2] != "snapshot server unmatched" {
		t.Fatalf("expected third line to be unmatched server, got %q", sink.messages[2])
	}
	unmatchedServer := logValuesMap(sink.values[2])
	if unmatchedServer["serverName"] != "server-only" || unmatchedServer["serverPower"] != "off" {
		t.Fatalf("unexpected unmatched server log fields: %#v", unmatchedServer)
	}
	if _, ok := unmatchedServer["nodeReady"]; ok {
		t.Fatalf("did not expect node fields on unmatched server line: %#v", unmatchedServer)
	}
}

func TestSnapshotLoggerLogsAmbiguousDuplicateServersAsUnmatched(t *testing.T) {
	serverStore := NewServerStateStore()
	serverStore.Replace([]KamateraServer{
		{Name: "node-1", Datacenter: "EU", Power: "on"},
		{Name: "node-1", Datacenter: "US", Power: "off"},
	})
	nodeStore := NewNodeStateStore()
	nodeStore.Replace(NodeSnapshot{Name: "node-1", Ready: corev1.ConditionFalse, Taints: map[string]TrackedTaint{}, Annotations: map[string]string{}})
	sink := &recordingLogSink{}
	logger := SnapshotLogger{ServerStore: serverStore, NodeStore: nodeStore, Log: logr.New(sink)}

	logger.logSnapshots()

	if len(sink.messages) != 3 {
		t.Fatalf("expected unmatched node plus both ambiguous servers, got %d lines: %v", len(sink.messages), sink.messages)
	}
	if sink.messages[0] != "snapshot node unmatched" {
		t.Fatalf("expected node to be unmatched, got %q", sink.messages[0])
	}
	seenServers := map[string]bool{}
	for _, values := range sink.values[1:] {
		mapped := logValuesMap(values)
		seenServers[mapped["serverName"].(string)+"/"+mapped["serverPower"].(string)] = true
	}
	if !seenServers["node-1/on"] || !seenServers["node-1/off"] {
		t.Fatalf("expected both duplicate servers to be logged unmatched, got %#v", seenServers)
	}
}

func TestSnapshotLoggerDefaultInterval(t *testing.T) {
	if got := (&SnapshotLogger{}).interval(); got != time.Minute {
		t.Fatalf("expected default snapshot log interval 1m, got %v", got)
	}
}

func TestSnapshotLoggerRequiresLeaderElection(t *testing.T) {
	if !(&SnapshotLogger{}).NeedLeaderElection() {
		t.Fatalf("expected SnapshotLogger to require leader election")
	}
}
