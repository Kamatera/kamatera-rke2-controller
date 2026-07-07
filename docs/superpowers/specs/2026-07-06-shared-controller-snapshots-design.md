# Shared Controller Snapshots Design

## Goal

Add two state-tracking controllers and modify node deletion so it uses shared in-memory controller snapshots instead of making a direct per-node Kamatera server status call.

## Scope

- Add a Kamatera servers list controller that polls `GET /service/servers` every configurable interval, defaulting to `1m`.
- Add configurable Kamatera server filters for datacenters and server-name glob matching.
- Add a Kubernetes nodes list controller that tracks all cluster Nodes and logs changes.
- Track `node.Spec.Unschedulable`, selected node taints, and selected node annotations as part of each node snapshot.
- Modify the existing node delete controller to delete only when the matching Kamatera server is present in the shared server snapshot and has `power=off`.
- Preserve the current safety behavior: absent or unknown Kamatera server state must not trigger deletion.

## Architecture

The implementation will use shared in-memory snapshot stores guarded by mutexes. The process already runs as a single controller-manager Deployment with optional leader election, so local memory is sufficient for logging change events and for delete decisions made by the same running process.

`ServerStateStore` stores Kamatera servers keyed by server name. Each entry includes at least `name`, `datacenter`, and `power`. The Kamatera servers list controller owns writes to this store and the node delete controller reads from it.

`NodeStateStore` stores Kubernetes Nodes keyed by node name. Each entry includes the Ready condition, deletion timestamp presence, `spec.unschedulable`, configured tracked taints, and configured tracked annotations. A new Node watcher/reconciler owns writes to this store and logs node lifecycle events. The delete controller does not need to read the node store for the initial implementation because it already receives individual Node reconcile requests from controller-runtime.

## Kamatera Servers List Controller

The controller runs as a controller-runtime `Runnable` registered with the manager. On each interval it calls `GET /service/servers`, parses the response, applies filters, updates `ServerStateStore`, and logs differences from the previous snapshot.

Filters:

- `-kamatera-server-datacenters`: comma-separated datacenters to include. Empty means all datacenters.
- `-kamatera-server-name-glob`: glob pattern for server names. Empty means all names.
- `-kamatera-server-list-interval`: poll interval, default `1m`.

Logged events:

- First successful poll: log all filtered servers.
- Later polls: log `server added`, `server removed`, and `server power changed`.

Kamatera API failures are logged and do not clear the previous snapshot. This avoids unsafe deletion caused by transient API errors.

## Kubernetes Nodes List Controller

The controller watches all `corev1.Node` objects through controller-runtime and updates `NodeStateStore`. It uses a distinct controller name from the existing node delete reconciler so both controllers can watch Nodes independently. It compares new observations against the previous snapshot and logs events.

Tracked node metadata configuration:

- `-node-tracked-taints`: comma-separated taint keys to track in node snapshots and change logs.
- `-node-tracked-annotations`: comma-separated annotation keys to track in node snapshots and change logs.
- Default tracked taints: `ToBeDeletedByClusterAutoscaler` and `DeletionCandidateOfClusterAutoscaler`.
- Default tracked annotations: none.
- `spec.unschedulable` is always tracked and is not configurable.

Logged events:

- Initial observed set: log all known nodes.
- Node create: `node added`.
- Node with `DeletionTimestamp`: `node delete requested`.
- Node delete event: `node deleted`.
- Ready condition status change: `node ready condition changed`.
- `spec.unschedulable` change: `node unschedulable changed`.
- Configured tracked taint add/remove/value/effect change: `node tracked taint changed`.
- Configured tracked annotation add/remove/value change: `node tracked annotation changed`.

RBAC already grants `get`, `list`, `watch`, and `delete` for Nodes, so no additional Kubernetes permissions are expected.

## Node Delete Controller Changes

The existing NotReady duration, control-plane protection, and requeue behavior remain in place.

After a Node has been NotReady longer than `-not-ready-duration`, the controller checks `ServerStateStore` by `node.Name`:

- If the server is present and `power=off`, delete the Kubernetes Node.
- If the server is present and power is any value other than `off`, do not delete and requeue after `ServerRunningRecheckInterval`.
- If the server is absent from the snapshot, do not delete and requeue after `ServerRunningRecheckInterval`.

This keeps deletion at least as conservative as the current direct `IsServerRunning` behavior.

## Error Handling And Safety

- Kamatera list API errors are logged and the last known server snapshot remains active.
- Invalid Kamatera response shape returns an error for that poll and does not update the snapshot.
- Empty filters mean include all servers.
- Invalid glob patterns fail startup validation.
- Empty tracked taint or annotation entries are ignored after trimming whitespace.
- The node delete controller never deletes based on missing Kamatera state.

## Testing

Add or update Go tests for:

- Kamatera `GET /service/servers` parsing.
- Datacenter and glob filtering.
- Server snapshot diff events for added, removed, and power changed.
- Node snapshot event detection for added, delete requested, deleted, and Ready condition changed.
- Node snapshot event detection for `spec.unschedulable` changes.
- Node snapshot event detection for configured tracked taint changes, including the default cluster autoscaler taints.
- Node snapshot event detection for configured tracked annotation changes.
- Node deletion when server is present with `power=off`.
- No node deletion when server is present with `power=on`.
- No node deletion when server is absent from the snapshot.

Validation commands:

- `go test ./...`
- `go build ./cmd/controller`

## Operational Impact

The controller will make one Kamatera server-list API request per interval instead of one server-info API request per eligible NotReady node. This reduces API calls during multiple node failures and centralizes Kamatera state observation.

Restarting the controller loses prior snapshots. On restart, the first successful poll logs the current filtered server list and the node watcher logs its initial observed nodes. This is acceptable because the snapshots are operational state, not desired state.

## Rollout And Rollback

Rollout is the existing Deployment update path. Existing flags continue to work. New flags are optional and default to current broad behavior.

Rollback is a Git revert or redeploying the previous image. Since no CRDs or persisted state are introduced, rollback has no data migration.
