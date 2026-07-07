# kamatera-rke2-controller

Kubernetes controller for managing Kamatera RKE2 clusters.

Provides the following functionality:

- **Track Kamatera server list state** by polling `GET /service/servers`, filtering by datacenter/name, and logging server add/remove/power changes.
- **Track Kubernetes Node state** and log node add/delete-request/delete/Ready/unschedulable/tracked-taint/tracked-annotation changes.
- **Match Kubernetes Nodes to Kamatera servers** with exact names by default, or with configurable one-way name templates.
- **Log current Node and Kamatera server snapshots** on a configurable interval, combining matched node/server pairs into one log line.
- **Delete Kubernetes `Node` objects** on a polling interval when they have been anything other than `Ready=True` for longer than a configured duration and a server snapshot is available where their matching Kamatera server is absent or has `power=off`.

## Configuration Flags

- `-not-ready-duration` (default: `15m`)
  - Minimum time a Node must be anything other than `Ready=True` before deletion is considered.
- `-node-delete-poll-interval` (default: `1m`)
  - Interval for polling Kubernetes Nodes for deletion eligibility.
- `-snapshots-log-interval` (default: `1m`)
  - Interval for logging current Node and Kamatera server snapshots. Matched node/server pairs are logged on one line; unmatched nodes or servers are logged separately.
- `-allow-control-plane` (default: `false`)
  - When `false`, the controller refuses to delete Nodes that have a control-plane label/taint.
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
- `-match-node-to-server-template` (default: empty)
  - Template applied to a Node name before comparing it to Kamatera server names. Must contain exactly one `%s`. Example: `kamatera-%s` matches Node `worker1` to server `kamatera-worker1`.
- `-match-server-to-node-template` (default: empty)
  - Template applied to a Kamatera server name before comparing it to Node names. Must contain exactly one `%s`. Example: `kamatera-%s` matches server `worker1` to Node `kamatera-worker1`.

Only one of `-match-node-to-server-template` and `-match-server-to-node-template` can be specified. If neither is specified, matching is exact: Node name equals Kamatera server name.

## Logs

By default, it logs informative actions and server/node changes.

To get mode verbose debug logs of every action by the controllers set `-zap-log-level 2`

## Deploy

Example manifests are provided in `deploy/`.

## Contributing

See `CONTRIBUTING.md`.
