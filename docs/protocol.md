# Client protocol evaluation

## Decision

Keep the current HTTP and gzip JSON protocol for local use.
Do not add ConnectRPC only to change the encoding.

The next protocol change should normalize the recurring state first.
It should stop sending the network and complete lane objects on every poll.
ConnectRPC is a good later fit for a typed contract and generated Go clients.
Adopt it after the normalized message boundaries are proven, or when remote
multi-client use makes the current contract expensive.

## Current traffic

The Go client starts one request every 50 milliseconds, so it requests at most
20 complete snapshots per second.
Go's HTTP transport requests gzip and decompresses the response before JSON
decoding.
The server uses pooled gzip writers at the fastest compression level.

These loopback samples used the current `scale100` and `london` projects.
The active samples ran automatic demand at 120 requests per simulated minute
and 8x speed for about eight wall-clock seconds.

| Fixture | Sample | Raw state | Gzip state | Gzip traffic at 20 Hz |
| --- | ---: | ---: | ---: | ---: |
| Scale100 | Idle | 138,638 B | 17,405 B | 0.35 MB/s |
| Scale100 | Active | 311,895 B | 40,030 B | 0.80 MB/s |
| London | Idle | 383,182 B | 55,444 B | 1.11 MB/s |
| London | Active | 559,260 B | 77,433 B | 1.55 MB/s |

Gzip reduced the active payloads by 87% and 86% respectively.
That makes the current protocol manageable for one local browser.
The traffic still scales once per connected browser.
For example, the active London sample uses about 12.4 Mbit/s of compressed
response bodies per browser before HTTP overhead.

The complete measurements are in
[`measurements/protocol-payloads.csv`](measurements/protocol-payloads.csv).
They are point samples, not throughput limits.

A native Go benchmark decoded the same captures and encoded the resulting
states on an AMD Ryzen 5 3600:

| Fixture | JSON encode | JSON and gzip encode | JSON decode |
| --- | ---: | ---: | ---: |
| Scale100 active | 1.54 ms | 2.23 ms | 2.83 ms |
| London active | 2.33 ms | 3.60 ms | 4.31 ms |

At 20 Hz, the measured London codec work corresponds to about 72 milliseconds
of server CPU time and 86 milliseconds of native client CPU time per wall-clock
second and client.
London decoding allocated about 970 KB and 8,654 objects per frame.
These figures exclude state copying, HTTP work, decompression, rendering, and
WebAssembly costs.
The benchmark results are in
[`measurements/protocol-codec.csv`](measurements/protocol-codec.csv).
Browser decode and state-application timing still needs browser
instrumentation before a transport changes.

## Repeated data

Every `State` contains the complete network even though that network changes
only when the project revision changes.
The network occupied 101,656 raw bytes in the active Scale100 state and 325,465
raw bytes in the active London state.
Those values were 33% and 58% of the complete raw responses.

Each vehicle also carries its remaining route as complete lane objects.
Routes occupied 155,260 raw bytes in the active Scale100 state and 159,854 raw
bytes in the active London state.
Stable lane IDs can describe the same paths because the client already has the
project network.

Commands repeat the problem.
A command response includes a complete current `State`, even though the
poller also retrieves state.
The measured demand-command responses were 17,400 gzip bytes for Scale100 and
56,364 gzip bytes for London.

Binary Protocol Buffers would reduce field-name and number overhead, but would
not remove these repeated objects.
Gzip also handles repeated JSON text well.
The data shape is therefore the first optimization target.

## Recommended message boundaries

Keep four distinct messages:

1. `TopologySnapshot` contains the project revision and the immutable network
   needed for rendering and route lookup. Fetch it on connection and whenever
   the project revision changes.
2. `EditableProject` contains the complete portable project for the scenario
   editor. Do not send demand matrices, editor settings, or background data to
   a simulation viewer that does not need them.
3. `StateFrame` contains the epoch, state revision, project revision,
   generation, controls, demand status, queues, berths, metrics, and dynamic
   vehicle fields. A vehicle route contains ordered lane IDs, not lane objects.
4. `CommandReply` contains the accepted state revision, optional order ID, and
   a typed error. It does not contain a complete state.

Preserve the current command identity: client ID, sequence, and epoch.
It provides safe retries and must not become an incidental transport feature.
Preserve stable station, berth, lane, pod, request, and party IDs across all
messages.

First implement these boundaries in JSON beside compatibility decoding.
This isolates the value of normalization from the value of a new codec and
does not add a code-generation toolchain.

## ConnectRPC fit

ConnectRPC fits the eventual contract well:

- Connect-Go handlers use standard `net/http` paths and handlers, so the RPC
  service can live beside the current endpoints.
- Connect supports its own protocol, gRPC, and gRPC-Web with binary Protobuf or
  JSON messages.
- Connect-Go provides gzip and message-size controls.
- Idempotent unary methods can use HTTP GET.
- The main browser is a Go WebAssembly client, so one generated Go client may
  serve native and browser builds after a small compatibility proof.

The project editor is plain JavaScript and has no Node build pipeline.
Using Connect-ES there would add a JavaScript package and generation workflow.
It is not required for the first migration because project editing can retain
its current JSON endpoint.

The browser Connect transport uses JSON unless binary format is enabled.
Binary mode must therefore be an explicit client setting and an explicit test
arm.

Relevant primary documentation:

- [Connect-Go README](https://github.com/connectrpc/connect-go/blob/main/README.md)
- [Connect-Go configuration](https://github.com/connectrpc/connect-go/blob/main/_autodocs/configuration.md)
- [Connect-ES browser client](https://github.com/connectrpc/connect-es/blob/main/packages/connect-web/README.md)
- [Connect-ES transport defaults](https://github.com/connectrpc/connect-es/blob/main/packages/connect-web/src/connect-transport.ts)

## Staged migration

### Stage 1: Normalize JSON

- Fetch topology data only when `projectRevision` changes.
- Keep complete project data on the editor path.
- Replace vehicle lane objects with ordered lane IDs.
- Return an acknowledgment from commands instead of a complete state.
- Keep periodic complete dynamic frames, revision checks, and motion
  interpolation.
- Measure response bytes, server allocations and CPU, and browser decode and
  apply time.

This stage has the best expected return and the smallest compatibility risk.

### Stage 2: Define and compare the typed schema

- Add Protocol Buffer messages for the four boundaries above.
- Add a Connect service beside `/api/state`, `/api/project`, and
  `/api/command`.
- Prove the generated Go client in desktop and WebAssembly builds.
- Compare normalized gzip JSON, binary Protobuf, and gzip binary Protobuf with
  the same state.
- Retain the old endpoints until browser and retry tests pass against both
  paths.

Do not select the codec from estimated sizes.
Record bytes, marshal and unmarshal time, allocations, and browser frame cost.

### Stage 3: Add deltas only if needed

Use revisioned keyframes and deltas if normalized complete frames remain a
measured limit.
A delta can contain vehicle, queue, berth, order, and metric upserts and
removals.
Send a keyframe after reconnect, a revision gap, reset, generation change, or
project change.

Server streaming can replace polling at this stage, but it is not required for
schema adoption.
Keep a unary keyframe method so a client can recover without restarting the
session.

## Acceptance gates

Before replacing the current endpoints:

- The same seed and commands must produce the same authoritative simulation
  result through both transports.
- Retry deduplication, stale epoch rejection, and stale project revision
  conflicts must retain their current behavior.
- Disconnects and missing deltas must recover from a keyframe.
- Desktop and WebAssembly clients must pass the existing motion, command,
  editor, and browser checks.
- The selected protocol must show a material improvement over normalized gzip
  JSON. Binary encoding is not sufficient by itself.

Until those gates are needed and met, the current transport is adequate for
the project's local-use workload.
