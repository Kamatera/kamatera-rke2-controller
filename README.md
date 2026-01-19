# kamatera-rke2-controller

Kubernetes controller for managing Kamatera RKE2 clusters.

Provides the following functionality:

- **Delete Kubernetes `Node` objects** that have been `NotReady` for longer than a configured duration and whose corresponding Kamatera server is not running.
  - Note: Kamatera server lookup is currently stubbed (`isKamateraServerRunning`) and defaults to "running".

This repo is intentionally minimal and does **not** perform VM lifecycle actions in Kamatera.

## Configuration Flags

- `--not-ready-duration` (default: `15m`)
  - Minimum time a Node must be `NotReady` before deletion is considered.
- `--allow-control-plane` (default: `false`)
  - When `false`, the controller refuses to delete Nodes that have a control-plane label/taint.

Controller-runtime flags:

- `--leader-elect`
- `--leader-election-id` (default: `kamatera-rke2-controller.kamatera.io`)
- `--metrics-bind-address` (default: `:8080`, set to `0` to disable)
- `--health-probe-bind-address` (default: `:8081`)

## Deploy

Example manifest is provided in `deploy/kamatera-rke2-controller.yaml`.

## Contributing

See `CONTRIBUTING.md`.
