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
go run ./cmd/controller -kubeconfig=...
```
