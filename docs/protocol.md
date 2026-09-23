# Client protocol

## Decision

Use normalized gzip JSON for the browser client.

The normalized protocol removes the network and complete lane objects from the
20 Hz state response. The client fetches topology only when the session epoch
or the project revision changes.

The project evaluated ConnectRPC and binary Protocol Buffers, then removed the
implementation. No application client used the service. The small remaining
bandwidth and CPU savings did not justify a second protocol implementation.

## Message boundaries

The JSON API uses four message boundaries:

| Boundary | JSON endpoint | Purpose |
| --- | --- | --- |
| Topology | `GET /api/topology` | Network geometry for one project revision. |
| State | `GET /api/state` | Controls, demand, queues, berths, metrics, save points, the build, the restore result, and dynamic vehicle fields. |
| Project | `GET /api/project` | Complete editable scenario data. |
| Command | `POST /api/command` | Retry-safe mutation and compact acknowledgment. |

State frames contain ordered lane IDs for vehicle routes. They do not contain
lane objects or network geometry. The Go client caches topology by session
epoch and project revision, then reconstructs the presentation state. It
rejects a frame if matching topology is not available. The client ignores a
frame from an epoch that it left. If the server sends that epoch in all polls
for 1 s, the client switches to that epoch again.

A state frame can contain a `build` string that identifies the server build.
A server without a build ID omits the key. In all future versions of the frame
format, `build` stays a top-level string.

The Go client keeps the first non-empty build that it receives. When a later
frame has a different non-empty build, a browser page reloads and gets the
browser files of the new server. The desktop client shows a message that tells
the user to restart it. The client reads the build even from a frame that it
cannot use. For example, a member can have a type that the client does not
expect, or the topology read for the frame can fail.

A state frame can contain a `restore` object. It tells how a server with
`-state` started the current simulation from its saved session state. A
server without `-state` omits the key. A server that found no saved state
also omits it. A reset, a demo, or a project apply removes the key. A rewind
does not change it.

| Member | Content |
| --- | --- |
| `tier` | `physical`: the pods kept their positions. `logical`: the pods started again at their initial berths. `empty`: the server did not use the saved state. |
| `reason` | Why the tier is not `physical`. For `logical`: `physical_failed` or `restore_loop`. For `empty`: `project_changed`, `unsupported_version`, `invalid_state`, `too_large`, `unreadable`, or `restore_loop`. |
| `demoted` | The number of pods that the `physical` tier moved to a berth. |
| `requeued` | The number of orders that went back to the queue. |
| `dropped` | The number of orders that the restore removed because they were not valid. |

The server omits an empty `reason` and each count of 0.
`restore_loop` means that the server stopped before its first periodic or
final save after a restore. After one such stop, the next start uses the
`logical` tier. After two, the next start does not use the saved state.

Each command contains a `client` ID, a `sequence`, the session `epoch`, and an
`action`. The actions are `trip`, `pause`, `speed`, `reset`, `demo`, `demand`,
`project`, `checkpoint`, and `rewind`.

Command acknowledgments contain the session epoch, state revision, project
revision, generation, optional order ID, optional checkpoint ID, and optional
`projectRestored` flag. They do not repeat a state frame. A rejected command
gets HTTP 409 and an acknowledgment with a stable `errorCode` and an `error`
message.

Exact retries return the original acknowledgment. A sequence lower than the
last sequence from the same client gets `expired_command`. The same sequence
with a different command gets `sequence_conflict`. After a server restart, a
sequence from before the restart can also get `expired_command`. See
[server restarts](#server-restarts).

A command from another epoch gets `session_changed`. A missing client ID, a
client ID that is longer than 100 bytes or is not valid UTF-8, or a zero
sequence gets `invalid_command`.
After the server records commands from 1,024 clients, a command from a new
client gets `client_limit`. A command that the server cannot apply gets
`command_rejected`. After a graceful shutdown starts, the server rejects new
commands with `server_stopping`.

The request must have the `application/json` content type. The body must be
at most 2 MiB and contain one JSON command with no unknown members. A request
with an `Origin` header must come from the same host and scheme. A request
that breaks these rules gets HTTP 400, 403, or 415 and a plain text body, not
an acknowledgment.

## Save points

The `checkpoint` action saves the simulation, the demand stream, and the
project in server memory. Its acknowledgment gives the new save point ID in
`checkpoint`. The first ID of a new session is 1. After a `physical` or
`logical` restore, the IDs continue from the saved session. The server does
not use an ID again in the same epoch. This is also true after a reset, a
demo, a project apply, or a rewind.

The `rewind` action restores one save point. The command must give the ID in
`checkpoint`. There is no default save point. Other actions ignore this field.
A rewind keeps the epoch, the playback speed, and the save points. It
increases the revision and the generation by one, and pauses the session.

State frames list the retained save points in `checkpoints`, oldest first. Each
item has an `id`, a `tick`, and an optional `restoresProject`. The server keeps
at most 8 save points. At the limit, a new save point removes the oldest one. A
frame without save points omits the `checkpoints` key, so the key adds no
bytes until a save point exists.

`restoresProject` is `true` when the project or demand configuration of the
save point is different from the current one. Each project apply and each
demand change counts as a change, even if the values stay the same. The server
omits the key when the value is `false`.

A rewind to such a save point restores its project and increases the project
revision by one. With `-project`, the server also saves the project to that
file. The project revision never goes back to an earlier value, so clients
fetch the topology again. The server also rejects project edits from before
the rewind. The acknowledgment of this rewind has `projectRestored` set to
`true`. A repeated rewind to the same save point does not save or restore the
project again, and its acknowledgment omits `projectRestored`. If the save
fails, the server rejects the rewind with `command_rejected`, and nothing
changes.

A rewind does not roll back the command receipts. An exact retry of a rewind
gets the stored acknowledgment and does not rewind again. This is also true
after another client resumes the session.

Commands do not name a generation, and the server does not use the generation
to match a retry to its receipt. A command can arrive after another client
rewinds, even if its sender has not seen the rewind. The server then applies
the command to the restored state. An exact retry gets the stored
acknowledgment, even if a later rewind removed the effect of the command. A
reset, a demo, and a project apply work the same way.

Save points are in memory only. A server restart clears them. The `-state`
option does not save them. A rewind to an unknown or removed ID gets
`command_rejected`.

## Server restarts

Without `-state`, a server restart starts a new session with a new epoch.

With `-state`, the server saves the session state and restores it at the next
start. The server keeps the saved epoch only when all of these are true:

- The saved state came from a final save.
- The restore tier is `physical` or `logical`.
- The saved state has fewer than 1,024 clients.

The server makes a final save at a graceful shutdown. When startup fails after
the startup save, for example because a listen address is in use, the server
also makes a final save. See
[graceful shutdown](operations.md#graceful-shutdown) for a shutdown without a
final save.

After a stop without a final save, the saved state can be older than the
frames that clients saw. A client drops each frame with a lower revision in
its epoch, so the server uses a new epoch. The server also uses a new epoch
when it does not use the saved state.

With a kept epoch, the revision and the generation are one more than in the
saved state. The project revision does not change, so clients keep their
topology cache. But when the `-project` file has different demand settings,
the server applies them as a `demand` command does, and the project revision
increases by one. The generation change resets motion. Save point IDs and
order IDs continue from the saved values. Thus a normal restart does not make
the server use an ID again in the epoch. When an operator restores an older
file from a final save, IDs can repeat. See
[session state](operations.md#session-state).

A startup that fails after the startup save writes the increased revision and
generation in its final save. Thus after one or more failed startups, clients
can see an increase of more than one.

The server saves the last sequence of each client, but not the command
receipts. After a restart with a kept epoch, a command with the saved
sequence of its client or a lower one gets `expired_command`. The server got
a command with that sequence before the restart, but it did not save the
acknowledgment, so it does not apply the command again. For example, a
retried `trip` does not make a second order. Read the current state to find
the result of the first command. A command with a higher sequence is new.
The saved clients stay in the limit of 1,024 clients. When the saved state
has 1,024 clients, the server uses a new epoch, so that new clients can send
commands after the restart.

With a new epoch, clients switch to the new session. A command from the old
epoch gets `session_changed`. The server does not keep the saved sequences,
and the limit of 1,024 clients starts again.

## Payload measurements

The comparison used equivalent states with 200 accepted requests. It used the
Scale100 and London projects from commit `cf50eca`. Commit `ed5d782` later
separated the London station portals by direction. The current London network
has more than twice as many lanes. The London numbers in this document do not
describe the current London project. Both formats used gzip level 1. These are
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

Topology is a one-time cost per project revision. London topology was 51,803
gzip bytes as JSON and 46,374 gzip bytes as Protobuf. The editor project is not
part of the polling path.

The payload data is in
[`measurements/protocol-normalized.csv`](measurements/protocol-normalized.csv).
The earlier live samples remain in
[`measurements/protocol-payloads.csv`](measurements/protocol-payloads.csv).

The `build` key adds 11 bytes plus the length of the build ID to each raw
state frame, or 27 bytes for a 16-character ID. With gzip level 1, sampled
frames of the example, Scale100, and London projects grew by about 20 bytes.
The CSV files do not include these samples. Frames from a server without a
build ID do not change.

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
The `json_gzip_level_1` and `json_gzip_level_6` rows of
[`measurements/protocol-normalized-codec.csv`](measurements/protocol-normalized-codec.csv)
come from these runs.

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
The earlier codec samples for the previous JSON shape remain in
[`measurements/protocol-codec.csv`](measurements/protocol-codec.csv).

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

- Remote use makes a 10% to 16% gzip saving significant.
- Multiple viewers make JSON codec CPU significant.
- External clients need a generated schema.
- The application needs streaming or gRPC compatibility.

Measure browser decode and state-application time before a protocol change.
Add deltas or streaming only if normalized complete frames become a measured
limit.
