# Client protocol

## Decision

Use normalized gzip JSON for the browser client.

The normalized protocol removes the network and complete lane objects from the
20 Hz state response. The client fetches topology only when the project
revision changes.

The project evaluated ConnectRPC and binary Protocol Buffers, then removed the
implementation. No application client used the service. The small remaining
bandwidth and CPU savings did not justify a second protocol implementation.

## Message boundaries

The JSON API uses four message boundaries:

| Boundary | JSON endpoint | Purpose |
| --- | --- | --- |
| Topology | `GET /api/topology` | Network geometry for one project revision. |
| State | `GET /api/state` | Controls, demand, queues, berths, metrics, and dynamic vehicle fields. |
| Project | `GET /api/project` | Complete editable scenario data. |
| Command | `POST /api/command` | Retry-safe mutation and compact acknowledgment. |

State frames contain ordered lane IDs for vehicle routes. They do not contain
lane objects or network geometry. The Go client caches topology by session
epoch and project revision, then reconstructs the presentation state. It
rejects a frame if matching topology is not available.

Command acknowledgments contain the accepted state revision, project revision,
generation, optional order ID, optional checkpoint ID, and a stable error code.
They do not repeat a state frame. Exact retries return the original
acknowledgment. Expired and conflicting sequences retain their previous
behavior.

## Save points

The `checkpoint` action saves the simulation and the demand stream in server
memory. Its acknowledgment gives the new save point ID in `checkpoint`. The
first ID is 1. The server does not use an ID again in the same epoch. This is
also true after a reset, a demo, or a rewind.

The `rewind` action restores one save point. The command must give the ID in
`checkpoint`. There is no default save point. Other actions ignore this field.
A rewind keeps the epoch, the playback speed, and the save points. It
increases the revision and the generation by one, and pauses the session.

State frames list the retained save points in `checkpoints`, oldest first. Each
item has an `id` and a `tick`. The server keeps at most 8 save points. At the
limit, a new save point removes the oldest one. A frame without save points
omits the `checkpoints` key, so the payload measurements below stay correct.

A rewind does not roll back the command receipts. An exact retry of a rewind
gets the stored acknowledgment and does not rewind again. This is also true
after another client resumes the session.

Save points are in memory only. A server restart clears them. A rewind to an
unknown or removed ID gets `command_rejected`. In this version, a rewind to a
save point from before a project or demand change also gets
`command_rejected`.

## Payload measurements

The comparison used equivalent states with 200 accepted requests. It used the
Scale100 and London projects. Both formats used gzip level 1. These are
deterministic codec samples, not network throughput limits.

| Fixture | Format | Raw frame | Gzip frame | Traffic at 20 Hz |
| --- | --- | ---: | ---: | ---: |
| Scale100 | Previous JSON shape | 394,962 B | 44,444 B | 0.89 MB/s |
| Scale100 | Normalized JSON | 116,556 B | 11,600 B | 0.23 MB/s |
| Scale100 | Binary Protobuf | 65,399 B | 9,775 B | 0.20 MB/s |
| London | Previous JSON shape | 743,060 B | 94,779 B | 1.90 MB/s |
| London | Normalized JSON | 132,915 B | 17,513 B | 0.35 MB/s |
| London | Binary Protobuf | 71,143 B | 15,704 B | 0.31 MB/s |

For London, normalization reduces the gzip frame by 81.5%. Binary Protobuf
reduces the normalized frame by another 10.3%. Its raw frame is 46.5% smaller,
but gzip removes most of that difference.

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
decode allocates about 255 KB, compared with 277 KB for JSON. It creates 4,893
allocations instead of 3,513 because of generated nested message pointers.

Level-1 gzip compression took 0.310 ms for the London JSON frame and 0.285 ms
for the Protobuf frame. At 20 Hz, marshal and compression use about 15.5 ms of
CPU per wall second for JSON and 8.4 ms for Protobuf. The saving is about 0.7%
of one full CPU core per connected client.

The gzip-level comparison used five more one-second runs for the London frame.

| Fixture | Gzip level | Frame size | Compression time |
| --- | ---: | ---: | ---: |
| Scale100 | 1 | 11,600 B | Not measured |
| Scale100 | 6 | 7,921 B | Not measured |
| London | 1 | 17,513 B | 0.318 ms |
| London | 6 | 14,499 B | 0.640 ms |

Level 6 saves 3,014 bytes, or 17.2%, for each London frame. It takes about twice
the compression CPU. At 20 Hz, it adds about 6.4 ms of CPU per wall second and
saves about 60 KB/s for each client. The server uses level 1 because CPU is the
tighter resource on the proposed shared-CPU deployment.

These measurements exclude frame construction, Protobuf conversion, HTTP work,
decompression, rendering, and simulation work. They do not show the fraction
of total application CPU.

The codec data is in
[`measurements/protocol-normalized-codec.csv`](measurements/protocol-normalized-codec.csv).

## Removed ConnectRPC experiment

The experiment defined matching topology, state, project, and command methods.
It used Connect-Go `v2.0.0-alpha.1`, generated Go clients and handlers, and
binary Protobuf. Tests verified state reconstruction, project conversion,
command retries, and invalid commands.

The experiment added 4,055 checked-in lines, including 3,065 generated lines.
It increased the unembedded server binary by 782,126 bytes, or 2.9%. No
application client used the RPC service, so it provided no live CPU or traffic
saving. Commit `cf50eca` preserves the implementation and benchmark source.

Reconsider a typed RPC protocol when at least one condition is true:

- Remote use makes a 10% to 16% gzip saving material.
- Multiple viewers make JSON codec CPU significant.
- External clients need a generated schema.
- The application needs streaming or gRPC compatibility.

Measure browser decode and state-application time before a cutover. Add deltas
or streaming only if normalized complete frames become a measured limit.
