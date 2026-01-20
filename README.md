# kamatera-rke2-controller

Kubernetes controller for managing Kamatera RKE2 clusters.

Provides the following functionality:

- **Delete Kubernetes `Node` objects** that have been `NotReady` for longer than a configured duration and whose corresponding Kamatera server is not running.

## Configuration Flags

- `-not-ready-duration` (default: `15m`)
  - Minimum time a Node must be `NotReady` before deletion is considered.
- `-allow-control-plane` (default: `false`)
  - When `false`, the controller refuses to delete Nodes that have a control-plane label/taint.

## Deploy

Example manifests are provided in `deploy/`.

## Contributing

See `CONTRIBUTING.md`.
