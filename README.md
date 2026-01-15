# kamatera-rke2-controller

Kubernetes controller for managing Kamatera RKE2 clusters.

Provides the following functionality:

- **Delete Kubernetes `Node` objects** that match a configured label.

This repo is intentionally minimal and does **not** perform VM lifecycle actions in Kamatera.

## Configuration Flags

- `--delete-label-key` (default: `kamatera.io/delete`)
- `--delete-label-value` (default: `true`) 
  - Set to empty (`""`) to match any value as long as the key exists.
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
