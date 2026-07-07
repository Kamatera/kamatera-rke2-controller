package controller

import "testing"

func TestServerFilterMatchesDatacenterAndGlob(t *testing.T) {
	filter, err := NewServerFilter("EU, US", "cwmc-*")
	if err != nil {
		t.Fatalf("new filter: %v", err)
	}

	tests := []struct {
		name   string
		server KamateraServer
		want   bool
	}{
		{name: "matching datacenter and name", server: KamateraServer{Name: "cwmc-worker1", Datacenter: "EU"}, want: true},
		{name: "different datacenter", server: KamateraServer{Name: "cwmc-worker1", Datacenter: "IL"}, want: false},
		{name: "different name", server: KamateraServer{Name: "other-worker1", Datacenter: "EU"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := filter.Match(tt.server); got != tt.want {
				t.Fatalf("Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServerFilterEmptyValuesMatchAll(t *testing.T) {
	filter, err := NewServerFilter("", "")
	if err != nil {
		t.Fatalf("new filter: %v", err)
	}
	if !filter.Match(KamateraServer{Name: "anything", Datacenter: "EU"}) {
		t.Fatalf("expected empty filter to match all servers")
	}
}

func TestServerFilterRejectsInvalidGlob(t *testing.T) {
	_, err := NewServerFilter("", "[")
	if err == nil {
		t.Fatalf("expected invalid glob error")
	}
}

func TestServerStateStoreReplaceInitialAndChanges(t *testing.T) {
	store := NewServerStateStore()

	initial := store.Replace([]KamateraServer{{Name: "node-1", Datacenter: "EU", Power: "on"}})
	if !initial.Initial {
		t.Fatalf("expected initial diff")
	}
	if len(initial.Current) != 1 || initial.Current[0].Name != "node-1" {
		t.Fatalf("unexpected initial current: %+v", initial.Current)
	}

	changed := store.Replace([]KamateraServer{{Name: "node-1", Datacenter: "EU", Power: "off"}, {Name: "node-2", Datacenter: "EU", Power: "on"}})
	if changed.Initial {
		t.Fatalf("did not expect second diff to be initial")
	}
	if len(changed.Added) != 1 || changed.Added[0].Name != "node-2" {
		t.Fatalf("unexpected added: %+v", changed.Added)
	}
	if len(changed.PowerChanged) != 1 || changed.PowerChanged[0].OldPower != "on" || changed.PowerChanged[0].NewPower != "off" {
		t.Fatalf("unexpected power changes: %+v", changed.PowerChanged)
	}

	removed := store.Replace([]KamateraServer{{Name: "node-2", Datacenter: "EU", Power: "on"}})
	if len(removed.Removed) != 1 || removed.Removed[0].Name != "node-1" {
		t.Fatalf("unexpected removed: %+v", removed.Removed)
	}

	server, ok := store.Get("node-2")
	if !ok || server.Power != "on" {
		t.Fatalf("unexpected get: server=%+v ok=%v", server, ok)
	}
}

func TestServerStateStoreGetTreatsDuplicateServerNamesAsAmbiguous(t *testing.T) {
	store := NewServerStateStore()
	store.Replace([]KamateraServer{
		{Name: "node-1", Datacenter: "EU", Power: "off"},
		{Name: "node-1", Datacenter: "US", Power: "on"},
	})

	if server, ok := store.Get("node-1"); ok {
		t.Fatalf("expected duplicate server name to be ambiguous, got %+v", server)
	}
}
