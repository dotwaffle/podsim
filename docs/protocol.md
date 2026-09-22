# Client protocol

## Decision

Use normalized gzip JSON for the browser client.

The normalized protocol removes the network and complete lane objects from the
20 Hz state response. This change provides most of the available bandwidth
saving without changing codecs. The client fetches topology only when the
project revision changes.

The server also exposes an experimental ConnectRPC service with binary
Protocol Buffers. Keep it for schema validation and measurement, but do not
move the browser client to it yet. The published Connect-Go v2 module is still
an alpha release. On the measured London fixture, gzip binary Protobuf saves
only 10% compared with normalized gzip JSON.

## Message boundaries

The JSON and Protobuf APIs use the same four boundaries:

| Boundary | JSON endpoint | Purpose |
| --- | --- | --- |
| Topology | `GET /api/topology` | Network geometry for one project revision. |
| State | `GET /api/state` | Recurring controls, demand, queues, berths, metrics, and dynamic vehicle fields. |
| Project | `GET /api/project` | Complete editable scenario data. |
| Command | `POST /api/command` | Retry-safe mutation and compact acknowledgment. |

State frames contain ordered lane IDs for vehicle routes. They do not contain
lane objects or network geometry. The Go client caches topology by session
epoch and project revision, then reconstructs the existing presentation state.
It rejects a frame if matching topology is not available.

Command acknowledgments contain the accepted state revision, project revision,
generation, optional order ID, and a stable error code. They do not repeat a
state frame. Exact retries return the original acknowledgment. Expired and
conflicting sequences retain their previous behavior.

The Connect service defines matching `GetTopology`, `GetState`, `GetProject`,
and `Command` methods in `proto/podsim/v1/podsim.proto`. It is mounted beside
the JSON API at `/podsim.v1.PodsimService/*`.

## Payload measurements

The comparison used equivalent states with 200 accepted requests. It used the
Scale100 and London projects. Gzip used level 1, which matches live JSON and
Connect responses. These are deterministic codec samples, not network
throughput limits.

| Fixture | Format | Raw frame | Gzip frame | Traffic at 20 Hz |
| --- | --- | ---: | ---: | ---: |
| Scale100 | Previous JSON shape | 394,962 B | 44,444 B | 0.89 MB/s |
| Scale100 | Normalized JSON | 116,556 B | 11,600 B | 0.23 MB/s |
| Scale100 | Binary Protobuf | 65,399 B | 9,775 B | 0.20 MB/s |
| London | Previous JSON shape | 743,060 B | 94,779 B | 1.90 MB/s |
| London | Normalized JSON | 132,915 B | 17,513 B | 0.35 MB/s |
| London | Binary Protobuf | 71,143 B | 15,704 B | 0.31 MB/s |

For London, normalization reduces the gzip frame by 81.5%. Binary Protobuf then
reduces the normalized gzip frame by another 10.3%. Its raw frame is 46.5%
smaller than normalized JSON, but gzip removes most of that difference.

Topology is a one-time cost per project revision. London topology is 51,803
gzip bytes as JSON and 46,374 gzip bytes as Protobuf. The editor project is not
part of the polling path.

The payload data is in
[`measurements/protocol-normalized.csv`](measurements/protocol-normalized.csv).
The earlier live samples remain in
[`measurements/protocol-payloads.csv`](measurements/protocol-payloads.csv).

## Codec measurements

The native benchmark ran on an AMD Ryzen 5 3600. Each value is the median of
three one-second benchmark runs.

| Fixture | Operation | Normalized JSON | Binary Protobuf | Protobuf ratio |
| --- | --- | ---: | ---: | ---: |
| Scale100 | Encode | 0.409 ms | 0.121 ms | 3.38x faster |
| Scale100 | Decode | 0.851 ms | 0.272 ms | 3.13x faster |
| London | Encode | 0.464 ms | 0.136 ms | 3.42x faster |
| London | Decode | 0.991 ms | 0.312 ms | 3.18x faster |

Protobuf uses one allocation when encoding. JSON uses three. London Protobuf
decode allocates about 255 KB, compared with 277 KB for JSON, but it creates
4,893 allocations instead of 3,513. Generated nested message pointers account
for much of the higher allocation count.

Level-1 gzip compression took 0.310 ms for the London JSON frame and 0.285 ms
for the Protobuf frame. At 20 Hz, marshal and compression use about 15.5 ms of
CPU per wall second for JSON and 8.4 ms for Protobuf. The saving is about 0.7%
of one full CPU core per connected client. It is more material on a fractional
shared CPU allocation.

The gzip-level comparison used five more one-second runs for the London frame.

| Fixture | Gzip level | Frame size | Compression time |
| --- | ---: | ---: | ---: |
| Scale100 | 1 | 11,600 B | Not measured |
| Scale100 | 6 | 7,921 B | Not measured |
| London | 1 | 17,513 B | 0.318 ms |
| London | 6 | 14,499 B | 0.640 ms |

Level 6 saves 3,014 bytes, or 17.2%, for each London frame. It takes about twice
the compression CPU. At 20 Hz, it adds about 6.4 ms of CPU per wall second and
saves about 60 KB/s for each client. The server keeps level 1 because CPU is
the tighter resource on the proposed shared-CPU deployment.

These codec measurements exclude frame construction, Protobuf conversion,
HTTP work, decompression, rendering, and simulation work. They do not show the
fraction of total application CPU.

The codec data is in
[`measurements/protocol-normalized-codec.csv`](measurements/protocol-normalized-codec.csv).
Run the benchmark again with:

```sh
go test ./internal/connectapi -run '^$' -bench 'BenchmarkProtocol(Codecs|Gzip)$' -benchtime=1s -count=3 -benchmem
```

## ConnectRPC evaluation

Connect-Go v2 integrates cleanly with the existing server:

- Generated clients and handlers use the same schema.
- The handler mounts on the existing `net/http` multiplexer.
- Binary Protobuf is the default codec.
- Connect uses pooled level-1 gzip compression.
- The existing OpenTelemetry HTTP wrapper observes the RPC paths.
- The generated packages compile for `js/wasm`.

Tests call the service through the generated binary client. They compare the
reconstructed state and editable project with the JSON model. They also verify
retry deduplication and invalid command handling.

The application client still uses normalized JSON. The plain JavaScript editor
also remains on JSON, so this evaluation does not add a JavaScript package or
generation pipeline.

Schema generation uses Buf and pinned generators from `mise.toml`:

```sh
go generate ./...
buf lint
```

## When to reconsider

Reconsider a client cutover when at least one condition is true:

- Connect-Go v2 reaches a stable release.
- Remote use makes the remaining 10% to 16% gzip saving material.
- Multiple viewers make JSON codec CPU significant.
- External clients need the generated schema more than they need simple HTTP
  inspection.

Measure browser decode and state-application time before a cutover. Add deltas
or streaming only if normalized complete frames become a measured limit.
