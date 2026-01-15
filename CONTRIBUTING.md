# Contributing

## Prerequisites

- Go 1.25+
- Docker (optional)
- Access to a Kubernetes cluster for manual testing (optional)

## Local development

Run tests:

```bash
make test
```

Build the binary:

```bash
make build
```

Build a container image:

```bash
make docker-build
```

Run against a cluster using your current `KUBECONFIG`:

```bash
go run ./cmd/controller \
  --metrics-bind-address=0 \
  --delete-label-key=kamatera.io/delete \
  --delete-label-value=true
```

## Code style

- Format: `go fmt ./...`
- Keep changes small and focused.
- Prefer adding unit tests for new logic.

## Pull requests

Please ensure:

- `go test ./...` passes
- `go fmt ./...` was run
- Any new flags / behavior are documented in `README.md`
- Any RBAC changes are reflected in `deploy/kamatera-rke2-controller.yaml`

## Reporting issues

If you find a bug or want a feature, open an issue with:

- Expected vs actual behavior
- Controller version/image tag
- Relevant logs
- Cluster version (RKE2/Kubernetes)

## Security

If you believe you’ve found a security issue, please avoid posting exploit details publicly in an issue.
Open a private report or contact the maintainers.
