# Distribution and operations

## Embedded server build

Generate all browser files before an embedded build:

```sh
go generate ./...
go build -trimpath -tags=embed_assets -o podsim-server ./cmd/serve
```

The executable contains the HTML, JavaScript, raw WASM, and gzip WASM files.
It does not need a `dist` directory at runtime.
The `-dir` option overrides embedded files for development.
Production builds keep Go and WASM debug information.

The application serves `GET /healthz` without reading simulation state.
The response is `200 OK` with the body `ok`.

## Container image

The container workflow publishes `ghcr.io/dotwaffle/podsim` from `main`,
version tags, and manual workflow runs.
It builds `linux/amd64` and `linux/arm64` images with ko.
The image uses the Chainguard static base and runs as UID and GID 65532.
Ko attaches an SPDX software bill of materials by default.

Run the image with the application port bound on all container interfaces:

```sh
docker run --rm -p 8080:8080 ghcr.io/dotwaffle/podsim:latest -addr :8080
```

The `-project` option needs an existing project file.
Mount its directory with write access for UID 65532 if the server must save changes.

## Memory limit

The server sets the Go memory limit to 90 percent of the detected cgroup limit.
Set `GOMEMLIMIT` to use an explicit Go memory limit.
Set `AUTOMEMLIMIT` to change the ratio or disable automatic detection.

## pprof

The pprof server is off by default.
Set `-pprof-addr` to start it on a separate listener:

```sh
./podsim-server -pprof-addr 127.0.0.1:6060
go tool pprof http://127.0.0.1:6060/debug/pprof/profile
```

Do not expose this listener to an untrusted network.
For a container, publish the diagnostics port only on the host loopback address:

```sh
docker run --rm -p 8080:8080 -p 127.0.0.1:6060:6060 \
  ghcr.io/dotwaffle/podsim:latest -addr :8080 -pprof-addr :6060
```

## OpenTelemetry

OpenTelemetry is off until an OTLP endpoint is configured.
The server uses OTLP over HTTP for traces and metrics.
Use the standard OpenTelemetry environment variables:

```sh
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318 ./podsim-server
```

Use `OTEL_TRACES_EXPORTER=none` or `OTEL_METRICS_EXPORTER=none` to disable one signal.
Use the signal-specific endpoint variables when traces and metrics use different collectors.
`OTEL_SERVICE_NAME` and `OTEL_RESOURCE_ATTRIBUTES` can add deployment identity.

HTTP telemetry excludes `/api/state` because each client polls it at 20 Hz.
It also excludes `/healthz`.
Other HTTP requests include route-based server traces and metrics.
Runtime metrics report memory, allocations, goroutines, processor limits, and the Go memory limit.
Session gauges report journeys, pods, stopped pods, distance, and passenger wait.

The server flushes both providers during graceful shutdown.
Invalid endpoint syntax stops startup with an error.
An unreachable collector reports export errors without stopping the simulation.

## Graceful shutdown

The server starts a graceful shutdown when it gets `SIGINT` or `SIGTERM`, or when a listener fails.
It logs `Stop accepting commands` with the cause.
Then it stops the simulation clock and rejects new commands with HTTP 409 and the `server_stopping` error code.
A command that is already in progress completes.
An exact retry of the last command from a client still gets the stored reply.

Then the server closes its listeners and does not accept new requests.
Requests that are already in progress, including reads, get up to 5 seconds to complete.
Last, the server waits up to 5 seconds for the clock goroutine to return, then flushes telemetry.
