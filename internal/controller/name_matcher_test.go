package controller

import "testing"

func TestNewNameMatcherRejectsBothTemplates(t *testing.T) {
	_, err := NewNameMatcher("server-%s", "node-%s")
	if err == nil {
		t.Fatalf("expected error when both matching templates are configured")
	}
}

func TestNameMatcherMatchesExactByDefault(t *testing.T) {
	matcher, err := NewNameMatcher("", "")
	if err != nil {
		t.Fatalf("new matcher: %v", err)
	}
	if !matcher.Match("node-1", "node-1") {
		t.Fatalf("expected exact node/server names to match")
	}
	if matcher.Match("node-1", "server-node-1") {
		t.Fatalf("did not expect different names to match without template")
	}
}

func TestNameMatcherMatchesNodeToServerTemplate(t *testing.T) {
	matcher, err := NewNameMatcher("kamatera-%s", "")
	if err != nil {
		t.Fatalf("new matcher: %v", err)
	}
	if !matcher.Match("worker1", "kamatera-worker1") {
		t.Fatalf("expected node-to-server template to match")
	}
}

func TestNameMatcherMatchesServerToNodeTemplate(t *testing.T) {
	matcher, err := NewNameMatcher("", "kamatera-%s")
	if err != nil {
		t.Fatalf("new matcher: %v", err)
	}
	if !matcher.Match("kamatera-worker1", "worker1") {
		t.Fatalf("expected server-to-node template to match")
	}
}

func TestNameMatcherRejectsTemplateWithoutStringPlaceholder(t *testing.T) {
	_, err := NewNameMatcher("kamatera-node", "")
	if err == nil {
		t.Fatalf("expected template without %%s placeholder to fail validation")
	}
}

func TestNameMatcherFindServerForNode(t *testing.T) {
	matcher, err := NewNameMatcher("kamatera-%s", "")
	if err != nil {
		t.Fatalf("new matcher: %v", err)
	}
	store := NewServerStateStore()
	store.Replace([]KamateraServer{
		{Name: "kamatera-worker1", Datacenter: "EU", Power: "off"},
		{Name: "kamatera-worker2", Datacenter: "EU", Power: "on"},
	})

	server, ok := matcher.FindServerForNode("worker1", store)
	if !ok {
		t.Fatalf("expected server match")
	}
	if server.Name != "kamatera-worker1" || server.Power != "off" {
		t.Fatalf("unexpected matched server: %+v", server)
	}
}

func TestNameMatcherFindServerForNodeTreatsDuplicateExactMatchesAsAmbiguous(t *testing.T) {
	matcher, err := NewNameMatcher("", "")
	if err != nil {
		t.Fatalf("new matcher: %v", err)
	}
	store := NewServerStateStore()
	store.Replace([]KamateraServer{
		{Name: "worker1", Datacenter: "EU", Power: "off"},
		{Name: "worker1", Datacenter: "US", Power: "on"},
	})

	if server, ok := matcher.FindServerForNode("worker1", store); ok {
		t.Fatalf("expected duplicate exact matches to be ambiguous, got %+v", server)
	}
}

func TestNameMatcherFindServerForNodeTreatsDuplicateServerToNodeMatchesAsAmbiguous(t *testing.T) {
	matcher, err := NewNameMatcher("", "node-%s")
	if err != nil {
		t.Fatalf("new matcher: %v", err)
	}
	store := NewServerStateStore()
	store.Replace([]KamateraServer{
		{Name: "worker1", Datacenter: "EU", Power: "off"},
		{Name: "worker1", Datacenter: "US", Power: "on"},
	})

	if server, ok := matcher.FindServerForNode("node-worker1", store); ok {
		t.Fatalf("expected duplicate server-to-node matches to be ambiguous, got %+v", server)
	}
}
