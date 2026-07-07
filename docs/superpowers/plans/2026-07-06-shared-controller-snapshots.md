# Shared Controller Snapshots Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add shared in-memory Kamatera server and Kubernetes node snapshots, and make node deletion depend on the Kamatera server snapshot.

**Architecture:** Add small mutex-protected stores for server and node snapshots. Register a Kamatera polling runnable and a separate Node watcher reconciler with the existing controller manager. Modify the existing delete reconciler to read the server snapshot and delete only when the matching server is present with `power=off`.

**Tech Stack:** Go 1.25.3, controller-runtime v0.22.4, Kubernetes `corev1.Node`, Kamatera REST API, standard library `sync`, `path/filepath`, `strings`, and `time`.

## Global Constraints

- Work in the current directory and branch; do not create a worktree.
- Keep deletion conservative: absent or unknown Kamatera server state must not trigger Kubernetes Node deletion.
- Kamatera server list interval defaults to `1m`.
- Empty Kamatera datacenter filter means all datacenters.
- Empty Kamatera server name glob means all names.
- Always track `node.Spec.Unschedulable`.
- Default tracked taints are `ToBeDeletedByClusterAutoscaler` and `DeletionCandidateOfClusterAutoscaler`.
- Default tracked annotations are none.
- Do not add new external dependencies.
- Do not commit unless the user explicitly requests it.

---

## File Structure

- Create `internal/controller/server_state_store.go`: Kamatera server snapshot type, filtering, mutex-protected store, and diff helpers.
- Create `internal/controller/server_state_store_test.go`: unit tests for filtering, store lookup, and server diff events.
- Create `internal/controller/kamatera_servers_controller.go`: manager runnable that polls Kamatera `GET /service/servers`, updates `ServerStateStore`, and logs events.
- Create `internal/controller/kamatera_servers_controller_test.go`: unit tests for controller polling behavior using a mock Kamatera client.
- Modify `internal/controller/kamatera_api_client.go`: extend `kamateraAPIClient` with `ListServers(ctx context.Context) ([]KamateraServer, error)`.
- Modify `internal/controller/kamatera_api_client_rest.go`: implement `GET /service/servers` parsing.
- Modify `internal/controller/kamatera_api_client_test.go`: update mock client to implement `ListServers`.
- Create `internal/controller/node_state_store.go`: node snapshot type, tracked taint/annotation extraction, mutex-protected store, and diff helpers.
- Create `internal/controller/node_state_store_test.go`: unit tests for node snapshot extraction and diff events.
- Create `internal/controller/node_list_controller.go`: controller-runtime reconciler for node list tracking with a distinct controller name.
- Create `internal/controller/node_list_controller_test.go`: tests for node add/update/delete behavior where practical with fake client and direct reconciler calls.
- Modify `internal/controller/node_delete_controller.go`: replace direct `IsServerRunning` decision with `ServerStateStore` lookup while preserving NotReady timing and control-plane protection.
- Modify `internal/controller/node_delete_controller_test.go`: update delete tests for present/off, present/on, and absent server snapshot behavior.
- Modify `cmd/controller/main.go`: add flags, parse config, create shared stores, register new controllers and runnable.
- Modify `README.md`: document new behavior and flags.
- Modify `deploy/deployment.yaml`: include example args for the new optional flags only when useful; keep defaults minimal.

---

### Task 1: Kamatera Server Snapshot Store

**Files:**
- Create: `internal/controller/server_state_store.go`
- Create: `internal/controller/server_state_store_test.go`

**Interfaces:**
- Produces: `type KamateraServer struct { Name string; Datacenter string; Power string }`
- Produces: `type ServerFilter struct { Datacenters map[string]struct{}; NameGlob string }`
- Produces: `func NewServerFilter(datacentersCSV string, nameGlob string) (ServerFilter, error)`
- Produces: `func (f ServerFilter) Match(server KamateraServer) bool`
- Produces: `type ServerStateStore struct { ... }`
- Produces: `func NewServerStateStore() *ServerStateStore`
- Produces: `func (s *ServerStateStore) Replace(servers []KamateraServer) ServerStateDiff`
- Produces: `func (s *ServerStateStore) Get(name string) (KamateraServer, bool)`
- Produces: `type ServerStateDiff struct { Initial bool; Current []KamateraServer; Added []KamateraServer; Removed []KamateraServer; PowerChanged []ServerPowerChange }`
- Produces: `type ServerPowerChange struct { Name string; Datacenter string; OldPower string; NewPower string }`

- [ ] **Step 1: Write failing tests for filters and diffs**

Add `internal/controller/server_state_store_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/controller -run 'TestServerFilter|TestServerStateStore'`

Expected: FAIL with undefined symbols such as `NewServerFilter`, `KamateraServer`, or `NewServerStateStore`.

- [ ] **Step 3: Implement the server state store**

Add `internal/controller/server_state_store.go`:

```go
package controller

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type KamateraServer struct {
	Name       string
	Datacenter string
	Power      string
}

type ServerFilter struct {
	Datacenters map[string]struct{}
	NameGlob    string
}

func NewServerFilter(datacentersCSV string, nameGlob string) (ServerFilter, error) {
	filter := ServerFilter{Datacenters: map[string]struct{}{}, NameGlob: strings.TrimSpace(nameGlob)}
	for _, datacenter := range strings.Split(datacentersCSV, ",") {
		datacenter = strings.TrimSpace(datacenter)
		if datacenter == "" {
			continue
		}
		filter.Datacenters[datacenter] = struct{}{}
	}
	if filter.NameGlob != "" {
		if _, err := filepath.Match(filter.NameGlob, ""); err != nil {
			return ServerFilter{}, err
		}
	}
	return filter, nil
}

func (f ServerFilter) Match(server KamateraServer) bool {
	if len(f.Datacenters) > 0 {
		if _, ok := f.Datacenters[server.Datacenter]; !ok {
			return false
		}
	}
	if f.NameGlob != "" {
		matched, err := filepath.Match(f.NameGlob, server.Name)
		if err != nil || !matched {
			return false
		}
	}
	return true
}

type ServerStateStore struct {
	mu          sync.RWMutex
	initialized bool
	servers     map[string]KamateraServer
}

type ServerStateDiff struct {
	Initial      bool
	Current      []KamateraServer
	Added        []KamateraServer
	Removed      []KamateraServer
	PowerChanged []ServerPowerChange
}

type ServerPowerChange struct {
	Name       string
	Datacenter string
	OldPower   string
	NewPower   string
}

func NewServerStateStore() *ServerStateStore {
	return &ServerStateStore{servers: map[string]KamateraServer{}}
}

func (s *ServerStateStore) Replace(servers []KamateraServer) ServerStateDiff {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := map[string]KamateraServer{}
	for _, server := range servers {
		next[server.Name] = server
	}

	diff := ServerStateDiff{Initial: !s.initialized, Current: sortedServers(next)}
	if s.initialized {
		for name, server := range next {
			previous, ok := s.servers[name]
			if !ok {
				diff.Added = append(diff.Added, server)
				continue
			}
			if previous.Power != server.Power {
				diff.PowerChanged = append(diff.PowerChanged, ServerPowerChange{Name: name, Datacenter: server.Datacenter, OldPower: previous.Power, NewPower: server.Power})
			}
		}
		for name, server := range s.servers {
			if _, ok := next[name]; !ok {
				diff.Removed = append(diff.Removed, server)
			}
		}
	}

	s.initialized = true
	s.servers = next
	sortServers(diff.Added)
	sortServers(diff.Removed)
	sort.Slice(diff.PowerChanged, func(i, j int) bool { return diff.PowerChanged[i].Name < diff.PowerChanged[j].Name })
	return diff
}

func (s *ServerStateStore) Get(name string) (KamateraServer, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	server, ok := s.servers[name]
	return server, ok
}

func sortedServers(servers map[string]KamateraServer) []KamateraServer {
	items := make([]KamateraServer, 0, len(servers))
	for _, server := range servers {
		items = append(items, server)
	}
	sortServers(items)
	return items
}

func sortServers(servers []KamateraServer) {
	sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/controller -run 'TestServerFilter|TestServerStateStore'`

Expected: PASS.

---

### Task 2: Kamatera Server List API And Polling Controller

**Files:**
- Modify: `internal/controller/kamatera_api_client.go`
- Modify: `internal/controller/kamatera_api_client_rest.go`
- Modify: `internal/controller/kamatera_api_client_test.go`
- Create: `internal/controller/kamatera_servers_controller.go`
- Create: `internal/controller/kamatera_servers_controller_test.go`

**Interfaces:**
- Consumes: `KamateraServer`, `ServerFilter`, `ServerStateStore`
- Produces: `func (c *KamateraApiClientRest) ListServers(ctx context.Context) ([]KamateraServer, error)`
- Produces: `type KamateraServersController struct { Client kamateraAPIClient; Store *ServerStateStore; Filter ServerFilter; Interval time.Duration; Log logr.Logger }`
- Produces: `func (c *KamateraServersController) Start(ctx context.Context) error`
- Produces: `func (c *KamateraServersController) poll(ctx context.Context) error`

- [ ] **Step 1: Write failing tests for list polling**

Add `internal/controller/kamatera_servers_controller_test.go`:

```go
package controller

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/controller -run TestKamateraServersControllerPollFiltersAndStoresServers`

Expected: FAIL with undefined `KamateraServersController` or missing `ListServers` mock method.

- [ ] **Step 3: Extend the Kamatera client interface and mock**

Modify `internal/controller/kamatera_api_client.go` so the interface is:

```go
type kamateraAPIClient interface {
	IsServerRunning(ctx context.Context, name string) (bool, error)
	ListServers(ctx context.Context) ([]KamateraServer, error)
}
```

Modify `internal/controller/kamatera_api_client_test.go` to add:

```go
func (c *kamateraClientMock) ListServers(ctx context.Context) ([]KamateraServer, error) {
	args := c.Called(ctx)
	servers, _ := args.Get(0).([]KamateraServer)
	return servers, args.Error(1)
}
```

- [ ] **Step 4: Implement REST `ListServers`**

Append to `internal/controller/kamatera_api_client_rest.go`:

```go
func (c *KamateraApiClientRest) ListServers(ctx context.Context) ([]KamateraServer, error) {
	gotErrorMessage, res, err := request(
		ctx,
		ProviderConfig{ApiUrl: c.url, ApiClientID: c.clientId, ApiSecret: c.secret},
		"GET",
		"/service/servers",
		nil,
		c.maxRetries,
		c.expSecondsBetweenRetries,
		"No servers found",
	)
	if err != nil {
		return nil, err
	}
	if gotErrorMessage {
		return nil, nil
	}
	serverInfoList, ok := res.([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid servers list format")
	}
	servers := make([]KamateraServer, 0, len(serverInfoList))
	for _, item := range serverInfoList {
		serverInfo, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid server info format")
		}
		name, ok := serverInfo["name"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid server name format")
		}
		datacenter, ok := serverInfo["datacenter"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid server datacenter format")
		}
		power, ok := serverInfo["power"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid server power format")
		}
		servers = append(servers, KamateraServer{Name: name, Datacenter: datacenter, Power: power})
	}
	return servers, nil
}
```

- [ ] **Step 5: Implement polling controller**

Add `internal/controller/kamatera_servers_controller.go`:

```go
package controller

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
)

const defaultKamateraServerListInterval = time.Minute

type KamateraServersController struct {
	Client   kamateraAPIClient
	Store    *ServerStateStore
	Filter   ServerFilter
	Interval time.Duration
	Log      logr.Logger
}

func (c *KamateraServersController) Start(ctx context.Context) error {
	if c.Log.GetSink() == nil {
		c.Log = ctrl.Log.WithName("controllers").WithName("KamateraServers")
	}
	interval := c.Interval
	if interval <= 0 {
		interval = defaultKamateraServerListInterval
	}
	if err := c.poll(ctx); err != nil {
		c.Log.Error(err, "failed to list Kamatera servers")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := c.poll(ctx); err != nil {
				c.Log.Error(err, "failed to list Kamatera servers")
			}
		}
	}
}

func (c *KamateraServersController) poll(ctx context.Context) error {
	servers, err := c.Client.ListServers(ctx)
	if err != nil {
		return err
	}
	filtered := make([]KamateraServer, 0, len(servers))
	for _, server := range servers {
		if c.Filter.Match(server) {
			filtered = append(filtered, server)
		}
	}
	diff := c.Store.Replace(filtered)
	c.logDiff(diff)
	return nil
}

func (c *KamateraServersController) logDiff(diff ServerStateDiff) {
	if diff.Initial {
		for _, server := range diff.Current {
			c.Log.Info("kamatera server observed", "name", server.Name, "datacenter", server.Datacenter, "power", server.Power)
		}
		return
	}
	for _, server := range diff.Added {
		c.Log.Info("server added", "name", server.Name, "datacenter", server.Datacenter, "power", server.Power)
	}
	for _, server := range diff.Removed {
		c.Log.Info("server removed", "name", server.Name, "datacenter", server.Datacenter, "power", server.Power)
	}
	for _, change := range diff.PowerChanged {
		c.Log.Info("server power changed", "name", change.Name, "datacenter", change.Datacenter, "oldPower", change.OldPower, "newPower", change.NewPower)
	}
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/controller -run 'TestKamateraServersController|TestServerFilter|TestServerStateStore'`

Expected: PASS.

---

### Task 3: Node Snapshot Store With Tracked Taints And Annotations

**Files:**
- Create: `internal/controller/node_state_store.go`
- Create: `internal/controller/node_state_store_test.go`

**Interfaces:**
- Produces: `const defaultTrackedTaintsCSV = "ToBeDeletedByClusterAutoscaler,DeletionCandidateOfClusterAutoscaler"`
- Produces: `func parseTrackedKeys(csv string) map[string]struct{}`
- Produces: `type NodeSnapshot struct { Name string; Ready corev1.ConditionStatus; Deleting bool; Unschedulable bool; Taints map[string]TrackedTaint; Annotations map[string]string }`
- Produces: `type TrackedTaint struct { Key string; Value string; Effect corev1.TaintEffect }`
- Produces: `type NodeStateStore struct { ... }`
- Produces: `func NewNodeStateStore() *NodeStateStore`
- Produces: `func NewNodeSnapshot(node *corev1.Node, trackedTaints map[string]struct{}, trackedAnnotations map[string]struct{}) NodeSnapshot`
- Produces: `func (s *NodeStateStore) Replace(snapshot NodeSnapshot) NodeStateDiff`
- Produces: `func (s *NodeStateStore) Delete(name string) (NodeSnapshot, bool)`
- Produces: `type NodeStateDiff struct { Initial bool; Added bool; ReadyChanged bool; DeleteRequested bool; UnschedulableChanged bool; TaintsChanged []string; AnnotationsChanged []string; Current NodeSnapshot; Previous NodeSnapshot }`

- [ ] **Step 1: Write failing node snapshot tests**

Add `internal/controller/node_state_store_test.go`:

```go
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

func TestNewNodeSnapshotDetectsDeletingNode(t *testing.T) {
	now := metav1.Now()
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", DeletionTimestamp: &now}}
	snapshot := NewNodeSnapshot(node, nil, nil)
	if !snapshot.Deleting {
		t.Fatalf("expected deleting snapshot")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/controller -run 'TestNewNodeSnapshot|TestNodeStateStore'`

Expected: FAIL with undefined node snapshot types and functions.

- [ ] **Step 3: Implement node state store**

Add `internal/controller/node_state_store.go`:

```go
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
	mu          sync.RWMutex
	initialized map[string]struct{}
	nodes       map[string]NodeSnapshot
}

type NodeStateDiff struct {
	Initial                bool
	Added                  bool
	ReadyChanged           bool
	DeleteRequested        bool
	UnschedulableChanged   bool
	TaintsChanged          []string
	AnnotationsChanged     []string
	Current                NodeSnapshot
	Previous               NodeSnapshot
}

func NewNodeStateStore() *NodeStateStore {
	return &NodeStateStore{initialized: map[string]struct{}{}, nodes: map[string]NodeSnapshot{}}
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
	diff := NodeStateDiff{Initial: !existed, Added: !existed, Current: snapshot, Previous: previous}
	if existed {
		diff.ReadyChanged = previous.Ready != snapshot.Ready
		diff.DeleteRequested = !previous.Deleting && snapshot.Deleting
		diff.UnschedulableChanged = previous.Unschedulable != snapshot.Unschedulable
		diff.TaintsChanged = changedTaintKeys(previous.Taints, snapshot.Taints)
		diff.AnnotationsChanged = changedAnnotationKeys(previous.Annotations, snapshot.Annotations)
	}
	s.initialized[snapshot.Name] = struct{}{}
	s.nodes[snapshot.Name] = snapshot
	return diff
}

func (s *NodeStateStore) Delete(name string) (NodeSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.nodes[name]
	if ok {
		delete(s.nodes, name)
	}
	return previous, ok
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/controller -run 'TestNewNodeSnapshot|TestNodeStateStore'`

Expected: PASS.

---

### Task 4: Kubernetes Node List Controller

**Files:**
- Create: `internal/controller/node_list_controller.go`
- Create: `internal/controller/node_list_controller_test.go`

**Interfaces:**
- Consumes: `NodeStateStore`, `NewNodeSnapshot`, `parseTrackedKeys`
- Produces: `type NodeListReconciler struct { client.Client; Store *NodeStateStore; TrackedTaints map[string]struct{}; TrackedAnnotations map[string]struct{}; Log logr.Logger }`
- Produces: `func (r *NodeListReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)`
- Produces: `func (r *NodeListReconciler) SetupWithManager(mgr ctrl.Manager) error`

- [ ] **Step 1: Write failing reconciler tests**

Add `internal/controller/node_list_controller_test.go`:

```go
package controller

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNodeListReconcilerStoresNodeSnapshot(t *testing.T) {
	scheme := newTestScheme(t)
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Annotations: map[string]string{"track/me": "yes"}}}
	node.Spec.Unschedulable = true
	node.Spec.Taints = []corev1.Taint{{Key: "ToBeDeletedByClusterAutoscaler", Value: "true", Effect: corev1.TaintEffectNoSchedule}}
	node.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node).WithStatusSubresource(node).Build()
	store := NewNodeStateStore()

	r := &NodeListReconciler{
		Client:             c,
		Store:              store,
		TrackedTaints:      parseTrackedKeys(defaultTrackedTaintsCSV),
		TrackedAnnotations: parseTrackedKeys("track/me"),
		Log:                logr.Discard(),
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: node.Name}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	diff := store.Replace(NewNodeSnapshot(node, r.TrackedTaints, r.TrackedAnnotations))
	if diff.Added {
		t.Fatalf("expected node to already exist in store")
	}
}

func TestNodeListReconcilerDeletesMissingNodeFromStore(t *testing.T) {
	scheme := newTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	store := NewNodeStateStore()
	store.Replace(NodeSnapshot{Name: "node-1", Taints: map[string]TrackedTaint{}, Annotations: map[string]string{}})

	r := &NodeListReconciler{Client: c, Store: store, Log: logr.Discard()}
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "node-1"}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if _, ok := store.Delete("node-1"); ok {
		t.Fatalf("expected missing node to be removed from store")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/controller -run TestNodeListReconciler`

Expected: FAIL with undefined `NodeListReconciler`.

- [ ] **Step 3: Implement node list reconciler**

Add `internal/controller/node_list_controller.go`:

```go
package controller

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NodeListReconciler struct {
	client.Client
	Store              *NodeStateStore
	TrackedTaints      map[string]struct{}
	TrackedAnnotations map[string]struct{}
	Log                logr.Logger
}

func (r *NodeListReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("node", req.Name)
	var node corev1.Node
	if err := r.Get(ctx, req.NamespacedName, &node); err != nil {
		if apierrors.IsNotFound(err) {
			if previous, ok := r.Store.Delete(req.Name); ok {
				logger.Info("node deleted", "ready", previous.Ready, "unschedulable", previous.Unschedulable)
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	snapshot := NewNodeSnapshot(&node, r.TrackedTaints, r.TrackedAnnotations)
	diff := r.Store.Replace(snapshot)
	r.logDiff(logger, diff)
	return ctrl.Result{}, nil
}

func (r *NodeListReconciler) logDiff(logger logr.Logger, diff NodeStateDiff) {
	if diff.Added {
		logger.Info("node added", "ready", diff.Current.Ready, "deleting", diff.Current.Deleting, "unschedulable", diff.Current.Unschedulable)
		return
	}
	if diff.DeleteRequested {
		logger.Info("node delete requested")
	}
	if diff.ReadyChanged {
		logger.Info("node ready condition changed", "oldReady", diff.Previous.Ready, "newReady", diff.Current.Ready)
	}
	if diff.UnschedulableChanged {
		logger.Info("node unschedulable changed", "oldUnschedulable", diff.Previous.Unschedulable, "newUnschedulable", diff.Current.Unschedulable)
	}
	for _, key := range diff.TaintsChanged {
		logger.Info("node tracked taint changed", "taint", key)
	}
	for _, key := range diff.AnnotationsChanged {
		logger.Info("node tracked annotation changed", "annotation", key)
	}
}

func (r *NodeListReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Log.GetSink() == nil {
		r.Log = ctrl.Log.WithName("controllers").WithName("NodeList")
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("node-list").
		For(&corev1.Node{}).
		Complete(r)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/controller -run TestNodeListReconciler`

Expected: PASS.

---

### Task 5: Node Delete Controller Uses Server Snapshot

**Files:**
- Modify: `internal/controller/node_delete_controller.go`
- Modify: `internal/controller/node_delete_controller_test.go`

**Interfaces:**
- Consumes: `ServerStateStore.Get(name string) (KamateraServer, bool)`
- Produces: `NodeReconciler.ServerStore *ServerStateStore`

- [ ] **Step 1: Update tests for snapshot-based deletion**

Modify the existing tests in `internal/controller/node_delete_controller_test.go`:

```go
// In TestNodeReconciler_DeletesWhenNotReadyTooLongAndServerNotRunning,
// remove kamateraClientMock usage and set:
serverStore := NewServerStateStore()
serverStore.Replace([]KamateraServer{{Name: node.Name, Datacenter: "EU", Power: "off"}})

// In the NodeReconciler literal, replace kamateraAPIClient with:
ServerStore: serverStore,
```

```go
// In TestNodeReconciler_DoesNotDeleteWhenServerRunning,
// remove kamateraClientMock usage and set:
serverStore := NewServerStateStore()
serverStore.Replace([]KamateraServer{{Name: node.Name, Datacenter: "EU", Power: "on"}})

// In the NodeReconciler literal, replace kamateraAPIClient with:
ServerStore: serverStore,
```

Add this new test to `internal/controller/node_delete_controller_test.go`:

```go
func TestNodeReconciler_DoesNotDeleteWhenServerAbsentFromSnapshot(t *testing.T) {
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
	r := &NodeReconciler{
		Client:                       c,
		NotReadyDuration:             15 * time.Minute,
		ServerRunningRecheckInterval: 3 * time.Minute,
		Now:                          func() time.Time { return now },
		Log:                          logr.Discard(),
		ServerStore:                  NewServerStateStore(),
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/controller -run 'TestNodeReconciler_DeletesWhenNotReadyTooLongAndServerNotRunning|TestNodeReconciler_DoesNotDeleteWhenServerRunning|TestNodeReconciler_DoesNotDeleteWhenServerAbsentFromSnapshot'`

Expected: FAIL because `NodeReconciler` has no `ServerStore` field or still calls `kamateraAPIClient`.

- [ ] **Step 3: Modify node delete reconciler**

In `internal/controller/node_delete_controller.go`, change `NodeReconciler` fields:

```go
	ServerStore *ServerStateStore

	kamateraAPIClient kamateraAPIClient
```

to:

```go
	ServerStore *ServerStateStore
```

Replace the direct Kamatera check block:

```go
	isServerRunning, err := r.kamateraAPIClient.IsServerRunning(ctx, node.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if isServerRunning {
		logger.V(1).Info("node is NotReady but Kamatera server is still running")
		return ctrl.Result{RequeueAfter: serverRunningRecheckInterval}, nil
	}
```

with:

```go
	server, ok := r.ServerStore.Get(node.Name)
	if !ok {
		logger.Info("node is NotReady but Kamatera server is absent from snapshot")
		return ctrl.Result{RequeueAfter: serverRunningRecheckInterval}, nil
	}
	if server.Power != "off" {
		logger.V(1).Info("node is NotReady but Kamatera server is not powered off", "power", server.Power)
		return ctrl.Result{RequeueAfter: serverRunningRecheckInterval}, nil
	}
```

In `SetupWithManager`, remove the Kamatera API client construction from the delete reconciler. Keep default logger setup and controller registration.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/controller -run TestNodeReconciler`

Expected: PASS.

---

### Task 6: Wire Controllers And Flags In Main

**Files:**
- Modify: `cmd/controller/main.go`
- Modify: `internal/controller/node_delete_controller.go` if nil-store guard is needed

**Interfaces:**
- Consumes: `NewServerFilter`, `NewServerStateStore`, `NewNodeStateStore`, `KamateraServersController`, `NodeListReconciler`, `parseTrackedKeys`, `defaultTrackedTaintsCSV`

- [ ] **Step 1: Write startup validation expectations manually**

There is no current main-package test harness. Use build-time validation for this task and keep parsing logic inside `main.go` minimal.

Expected startup behavior:

- `-kamatera-server-list-interval` must be greater than 0.
- Invalid `-kamatera-server-name-glob` logs an error and exits with status 1.
- `-node-tracked-taints` defaults to `ToBeDeletedByClusterAutoscaler,DeletionCandidateOfClusterAutoscaler`.
- `-node-tracked-annotations` defaults to empty.

- [ ] **Step 2: Modify main flags and controller wiring**

In `cmd/controller/main.go`, add variables:

```go
	var kamateraServerListInterval time.Duration
	var kamateraServerDatacenters string
	var kamateraServerNameGlob string
	var nodeTrackedTaints string
	var nodeTrackedAnnotations string
```

Add flags after the existing node flags:

```go
	flag.DurationVar(&kamateraServerListInterval, "kamatera-server-list-interval", time.Minute, "Interval for polling Kamatera server list.")
	flag.StringVar(&kamateraServerDatacenters, "kamatera-server-datacenters", "", "Comma-separated Kamatera datacenters to include. Empty includes all datacenters.")
	flag.StringVar(&kamateraServerNameGlob, "kamatera-server-name-glob", "", "Glob pattern for Kamatera server names. Empty includes all names.")
	flag.StringVar(&nodeTrackedTaints, "node-tracked-taints", nodecontroller.DefaultTrackedTaintsCSV(), "Comma-separated node taint keys to track in node snapshots.")
	flag.StringVar(&nodeTrackedAnnotations, "node-tracked-annotations", "", "Comma-separated node annotation keys to track in node snapshots.")
```

Add validation after `notReadyDuration` validation:

```go
	if kamateraServerListInterval <= 0 {
		setupLog.Error(nil, "--kamatera-server-list-interval must be greater than 0")
		os.Exit(1)
	}
	serverFilter, err := nodecontroller.NewServerFilter(kamateraServerDatacenters, kamateraServerNameGlob)
	if err != nil {
		setupLog.Error(err, "invalid --kamatera-server-name-glob")
		os.Exit(1)
	}
```

Add exported wrappers in `internal/controller/node_state_store.go` if needed by `main.go`:

```go
func DefaultTrackedTaintsCSV() string {
	return defaultTrackedTaintsCSV
}

func ParseTrackedKeys(csv string) map[string]struct{} {
	return parseTrackedKeys(csv)
}
```

Then in `main.go`, create stores and client before registering controllers:

```go
	serverStore := nodecontroller.NewServerStateStore()
	nodeStore := nodecontroller.NewNodeStateStore()
	kamateraApiUrl := os.Getenv("KAMATERA_API_URL")
	if kamateraApiUrl == "" {
		kamateraApiUrl = "https://cloudcli.cloudwm.com"
	}
	kamateraClient := nodecontroller.BuildKamateraAPIClient(
		os.Getenv("KAMATERA_API_CLIENT_ID"),
		os.Getenv("KAMATERA_API_SECRET"),
		kamateraApiUrl,
	)
```

Add exported wrapper in `internal/controller/kamatera_api_client.go`:

```go
func BuildKamateraAPIClient(clientId string, secret string, url string) kamateraAPIClient {
	return buildKamateraAPIClient(clientId, secret, url)
}
```

Register the Kamatera runnable:

```go
	if err := mgr.Add(&nodecontroller.KamateraServersController{
		Client:   kamateraClient,
		Store:    serverStore,
		Filter:   serverFilter,
		Interval: kamateraServerListInterval,
		Log:      ctrl.Log.WithName("controllers").WithName("KamateraServers"),
	}); err != nil {
		setupLog.Error(err, "unable to add controller", "controller", "KamateraServers")
		os.Exit(1)
	}
```

Register `NodeListReconciler`:

```go
	if err := (&nodecontroller.NodeListReconciler{
		Client:             mgr.GetClient(),
		Store:              nodeStore,
		TrackedTaints:      nodecontroller.ParseTrackedKeys(nodeTrackedTaints),
		TrackedAnnotations: nodecontroller.ParseTrackedKeys(nodeTrackedAnnotations),
		Log:                ctrl.Log.WithName("controllers").WithName("NodeList"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NodeList")
		os.Exit(1)
	}
```

Modify existing `NodeReconciler` registration to include:

```go
		ServerStore:       serverStore,
```

- [ ] **Step 3: Run build to catch wiring errors**

Run: `go build ./cmd/controller`

Expected: PASS.

---

### Task 7: Documentation And Deployment Examples

**Files:**
- Modify: `README.md`
- Modify: `deploy/deployment.yaml`

**Interfaces:**
- Consumes: all new flags from Task 6

- [ ] **Step 1: Update README**

Modify `README.md` functionality list to include:

```markdown
- **Track Kamatera server list state** by polling `GET /service/servers`, filtering by datacenter/name, and logging server add/remove/power changes.
- **Track Kubernetes Node state** and log node add/delete-request/delete/Ready/unschedulable/tracked-taint/tracked-annotation changes.
- **Delete Kubernetes `Node` objects** that have been `NotReady` for longer than a configured duration and whose corresponding Kamatera server is present in the shared snapshot with `power=off`.
```

Add configuration flag docs:

```markdown
- `-kamatera-server-list-interval` (default: `1m`)
  - Interval for polling `GET /service/servers`.
- `-kamatera-server-datacenters` (default: empty)
  - Comma-separated datacenters to include. Empty includes all datacenters.
- `-kamatera-server-name-glob` (default: empty)
  - Glob pattern for server names. Empty includes all names.
- `-node-tracked-taints` (default: `ToBeDeletedByClusterAutoscaler,DeletionCandidateOfClusterAutoscaler`)
  - Comma-separated node taint keys to include in node snapshot change logs.
- `-node-tracked-annotations` (default: empty)
  - Comma-separated node annotation keys to include in node snapshot change logs.
```

- [ ] **Step 2: Update deployment example args**

In `deploy/deployment.yaml`, keep `-not-ready-duration=15m` and add only one representative default-visible arg:

```yaml
            - "-kamatera-server-list-interval=1m"
```

Do not add empty filter args to the manifest.

- [ ] **Step 3: Run full validation**

Run: `go test ./...`

Expected: PASS.

Run: `go build ./cmd/controller`

Expected: PASS.

---

## Self-Review

Spec coverage:

- Kamatera polling, filtering, initial list, and change events are covered by Tasks 1 and 2.
- Kubernetes node tracking, Ready changes, delete requested/deleted, unschedulable, tracked taints, and tracked annotations are covered by Tasks 3 and 4.
- Conservative deletion based on shared server snapshot with present `power=off` only is covered by Task 5.
- Main wiring, flags, validation, and deployment startup behavior are covered by Task 6.
- README and deployment documentation are covered by Task 7.

Placeholder scan:

- No `TBD`, incomplete requirement, or undefined task dependency remains.
- No commit step is included because the global constraint says not to commit unless explicitly requested.

Type consistency:

- Later tasks consume types and functions produced by earlier tasks.
- Exported wrappers are named explicitly where `cmd/controller/main.go` needs access to internal unexported helpers.
