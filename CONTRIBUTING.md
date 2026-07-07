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

Run against an existing cluster

```bash
export KUBECONFIG=/path/to/kubeconfig
export KAMATERA_API_CLIENT_ID=
export KAMATERA_API_SECRET=
go run ./cmd/controller -kubeconfig $KUBECONFIG
```

## E2E / Integration Tests

E2E / Integration tests are under `tests/` directory and written using python with `pytest`

To run the tests ensure you have the following prerequisites:

* env vars for connection to Kamatera API, must use a dedicated test account to prevent accidental deletion of production servers:
    * `KAMATERA_API_CLIENT_ID`, `KAMATERA_API_SECRET=`
* SSH key pair in `~/.ssh/id_rsa` and `~/.ssh/id_rsa.pub` - will be used to access created servers.
* `cloudcli` binary in your PATH

Run the following to verify before running the E2E/integration tests:

```bash
export CLOUDCLI_APICLIENTID=$KAMATERA_API_CLIENT_ID
export CLOUDCLI_APISECRET=$KAMATERA_API_SECRET
cloudcli server list
# verify the list is empty, or that the servers are not production servers.
```

Run the tests:

```bash
cd tests/
uv run pytest -svvx
```
