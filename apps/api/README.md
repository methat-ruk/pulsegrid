# PulseGrid API

This directory owns the initial Go/Fiber API process. It currently exposes
only lifecycle and health contracts; product APIs are introduced by later
feature plans.

## Requirements

The repository pins Go 1.27.1 in the root `.go-version`. With `goenv` initialized
in the shell, entering this repository automatically selects that version.

## Run locally

From this directory:

```sh
PULSEGRID_ENV=development go run ./cmd/api
```

The default listener is `127.0.0.1:8080`. To use a local dotenv file, copy the
reviewed example and keep the environment selector explicit:

```sh
cp .env.development.example .env.development
PULSEGRID_ENV=development go run ./cmd/api
```

Check the lifecycle endpoints:

```sh
curl -i http://127.0.0.1:8080/health/live
curl -i http://127.0.0.1:8080/health/ready
```

The machine-readable operational contract and response semantics are in the
[API documentation index](../../docs/api/README.md) and
[OpenAPI contract](api/openapi/operational.yaml). Product-domain APIs are not
implemented by this foundation.

## Test

```sh
PULSEGRID_ENV=test go test ./...
PULSEGRID_ENV=test go vet ./...
gofmt -d $(find . -name '*.go' -type f)
```

Test mode uses isolated defaults and never loads a development dotenv file.
Production mode reads process-injected values only; do not copy a production
file containing credentials into the repository.
