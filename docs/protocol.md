# Client protocol

## Decision

Use normalized gzip JSON for the browser client.

The normalized protocol removes the network and complete lane objects from the 20 Hz state response.
The client fetches topology only when the session epoch, the project revision, or the server start ID changes.

The project evaluated ConnectRPC and binary Protocol Buffers, then removed the implementation.
No application client used the service.
The small remaining bandwidth and CPU savings did not justify a second protocol implementation.

## Message boundaries

The JSON API uses four message boundaries:

| Boundary | JSON endpoint | Purpose |
| --- | --- | --- |
| Topology | `GET /api/topology` | Network geometry for one project revision. |
| State | `GET /api/state` | Controls, demand, queues, berths, metrics, save points, the build, the server start ID, the restore result, and dynamic vehicle fields. |
| Project | `GET /api/project` | Complete editable scenario data. |
| Command | `POST /api/command` | Retry-safe mutation and compact acknowledgment. |

A request for a different `/api` path gets HTTP 404.
A request with a different method gets HTTP 405 and an `Allow` header with the methods of the endpoint.
A `GET` endpoint also accepts `HEAD`.
All `/api` responses have `Cache-Control: no-store`, also the error responses.

State frames contain ordered lane IDs for vehicle routes.
They do not contain lane objects or network geometry.
The Go client caches topology by session epoch, project revision, and server start ID, then reconstructs the presentation state.
It rejects a frame if matching topology is not available.
The client ignores a frame from an epoch that it left.
If the server sends that epoch in all polls for 1 s, the client switches to that epoch again.

Each state frame also has a `revision` and a `generation`.
The revision increases by one at each clock tick while the session runs, and with each accepted command.
The generation increases by one with a reset, a demo, a project apply, a rewind, and a restore of the saved state at a restart.
A new generation makes the Go client clear its buffered motion.

The `redistribution` member of a state frame is true when the project turns on redistribution and the demo does not run.
The simulation then runs guarded positioning.

A state frame can contain a `build` string.
It is the build ID of the browser files that the server sends.
See [build ID](operations.md#build-id).
A server without a build ID omits the key.
In all future versions of the frame format, `build` stays a top-level string.

The Go client keeps the first non-empty build that it receives.
When a later frame has a different non-empty build, the simulation view in a browser page reloads and gets the browser files of the new server.
See [build ID](operations.md#build-id) for the parts of the page that reload.
The desktop client shows a message that tells the user to restart it.
The client reads the build even from a frame that it cannot use.
For example, a member can have a type that the client does not expect, or the topology read for the frame can fail.

A state frame can contain a `serverStart` string.
The server sets it to a random ID of 16 hexadecimal characters when the server process starts.
The ID does not change until the process stops.
A reset, a demo, a project apply, and a rewind do not change it.
The `-state` option does not save it, so a restored session gets the ID of the new process.
The ID does not change the epoch, the revision, or the generation.
A client can compare the ID with the ID of an earlier frame to find a server restart, also when the epoch stays the same.
A client that does not know the member ignores it.

A state frame can contain a `restore` object.
It tells how a server with `-state` started the current simulation and if it used its saved session state.
A server without `-state` omits the key.
A server that found no saved state also omits it.
A reset, a demo, or a project apply removes the key.
A rewind does not change it.

| Member | Content |
| --- | --- |
| `tier` | `physical`: the pods kept their positions. `logical`: the pods started again at their initial berths. `empty`: the server did not use the saved state. |
| `reason` | Why the tier is not `physical`. For `logical`: `physical_failed` or `restore_loop`. For `empty`: `project_changed`, `unsupported_version`, `invalid_state`, `too_large`, `unreadable`, or `restore_loop`. |
| `demoted` | The number of pods that the `physical` tier moved to a berth. |
| `requeued` | The number of orders that went back to the queue. |
| `dropped` | The number of orders that the restore removed because they were not valid. |
| `unaccounted` | The number of orders that the saved state submitted but did not hold: they were not complete, not in the queue, and not aboard a pod. It includes the orders that an earlier restore dropped. |

The server omits an empty `reason` and each count of 0.
`restore_loop` means that the server stopped after a restore and before a periodic save, a final save, or a save before an acknowledgment.
After one such stop, the next start uses the `logical` tier.
After two, the next start does not use the saved state.

Each item of `simulation.Vehicles` can contain a `Riders` array and a `Stops` array.
`Riders` has one order for each party that boarded the pod or joined it.
It has at most 8 orders.
The first order is the party that boarded the pod.
The orders after it are the parties that joined the pod.
Each order has the members `ID`, `From`, `To`, `PartySize`, `PodID`, `Completed`, `RequestedTick`, `BoardedTick`, and `DispatchReason`.
`BoardedTick` is the simulation tick when the party boarded the pod or joined it.
An order stays in `Riders` with `Completed` set to `true` after the party leaves the pod, until the pod gets a new order.
`Stops` has the IDs of the stations where the pod must stop and that it did not reach, in the sequence of the stops.
A pod omits each key when its array is empty.

A pod in a [virtual platoon](../README.md#virtual-platoons) also has `PlatoonID` and `PlatoonIndex`.
`PlatoonID` is the ID of the first pod of the platoon.
`PlatoonIndex` is the position of the pod in the platoon, from 1 for the first pod.
Thus the pod ahead of a pod with index 3 has the same `PlatoonID` and index 2.
A pod that is not in a platoon omits both keys.
With platoons off, no pod has these keys, so the frame does not change.

The `simulation` object has these ride metrics:

| Member | Content |
| --- | --- |
| `Journey` | `AverageSeconds` and `MaxSeconds` of the time from request to alighting, for the parties that alighted at their destination. |
| `RiderDistanceMeters` | The sum of the distances that the same parties rode, from the berth where they boarded to the berth where they alighted. |
| `DirectDistanceMeters` | The sum of the free-flow distances of the same parties between the same two berths. |
| `MaxDetourRatio` | The largest ratio of the ridden distance to the free-flow distance for one party. |
| `SharedParties` | The number of parties that joined the pod of another party. |
| `SharedRidePartyLimit` | The maximum number of parties in one pod. |

A reset, a demo, and a project apply set the metrics to 0.
A rewind restores the metrics of the save point.
A restore at a restart keeps them.

Each command contains a `client` ID, a `sequence`, the session `epoch`, and an `action`.
The actions are `trip`, `pause`, `speed`, `reset`, `demo`, `demand`, `project`, `checkpoint`, and `rewind`.

The other members depend on the action:

| Action | Members | Effect |
| --- | --- | --- |
| `trip` | `origin`, `destination`: station IDs | Adds an order. The stations must be different, connected passenger stations. The server rejects the order during the demo or when the queue holds 200 orders. |
| `pause` | `paused`: boolean | `true` pauses the session. `false` or an absent member resumes it. |
| `speed` | `speed`: 1, 2, 4, or 8 | Sets the playback speed. |
| `reset` | None | Restores the project fleet and demand settings, and clears the orders. It sets the speed to 1 and keeps the pause state. |
| `demo` | None | Resets the run, starts the traffic demo, disables automatic demand, and sets the speed to 1. It needs the unchanged example network and fleet. |
| `demand` | `demand`: the `demand` object of a project | Replaces the demand settings of the project and increases the project revision. The server rejects it during the demo. |
| `project` | `project`: the `project` object from `GET /api/project`. `projectRevision`: the `revision` from `GET /api/project`, an integer. `serverStart`: optional, the `serverStart` from `GET /api/state` when the project loaded, a string | Replaces the project and increases the project revision. The new fleet starts paused at speed 1. The session must be paused, and `projectRevision` must be the current project revision. When the session is paused and `projectRevision` is not the current project revision, the command gets `stale_project`. When `serverStart` is set and is not the `serverStart` of the server process, the command gets `session_changed`. |
| `checkpoint` | None | Makes a save point. |
| `rewind` | `checkpoint`: save point ID, an integer | Restores the save point and pauses the session. |

In the acknowledgment, `trip` sets `orderID`, `checkpoint` sets `checkpoint`, and a `rewind` that restores a project sets `projectRestored`.
With `-state`, `project` and a `rewind` that restores a project also set `stateSaved`.

Command acknowledgments contain the session epoch, state revision, project revision, generation, optional order ID, optional checkpoint ID, optional `projectRestored` flag, and optional `stateSaved` flag.
They do not repeat a state frame.
A rejected command gets HTTP 409 and an acknowledgment with a stable `errorCode` and an `error` message.
The `error` message has at most 1,024 bytes.
The server cuts a longer message and adds `...` at the end.

The server sets `stateSaved` only when it tried to save the session state before the acknowledgment.
`true` means that the last successful save holds the state at the `revision` of the acknowledgment or at a later revision.
A later state counts, also with commands from other clients, because a restore of it cannot go back to the state before the command.
`false` means that no successful save holds such a state.
This occurs after a save that failed or took more than 2 seconds, or when a graceful shutdown started before a save held the command.

When `stateSaved` is `false`, the server applied the command, but a server crash can undo it until a later save succeeds.
Tell the user.
The server omits `stateSaved` for other commands, for rejected commands, and when it does not save the session state.
See [session state](operations.md#session-state).

The Go client sends an exact retry when a command request fails, gets no reply in 3 s, or gets an HTTP 5xx status.
It sends a command at most three times.
Exact retries return the original acknowledgment, except `stateSaved`.
The server saves the state before each reply to an exact retry of a `project` command or of a `rewind` that restored a project.
After that save, the server sets `stateSaved` with the rule above for the `revision` of the original acknowledgment.
A sequence lower than the last sequence from the same client gets `expired_command`.
The same sequence with a different command gets `sequence_conflict`.
The server compares a SHA-256 digest of the command, so a receipt does not keep the command or its project.
After a server restart, a sequence from before the restart can also get `expired_command`.
See [server restarts](#server-restarts).

A command from another epoch gets `session_changed`.
A `project` command with a `serverStart` that is not the `serverStart` of the server process also gets `session_changed`.
Other actions ignore `serverStart`.
A missing client ID, a client ID that is longer than 100 bytes or is not valid UTF-8, or a zero sequence gets `invalid_command`.
After the server records commands from 1,024 clients, a command from a new client gets `client_limit`.
A command that the server cannot apply gets `command_rejected`.
The exception is a `project` command to a paused session with a `projectRevision` that is not the current project revision.
It gets `stale_project`, so that an editor can tell a stale draft from other failures.
After a graceful shutdown starts, the server rejects new commands with `server_stopping`.

The request must have the `application/json` content type.
The body must be at most 4 MiB and contain one JSON command with no unknown members.
The request can send the body with `Content-Encoding: gzip`.
Then the 4 MiB limit applies to the compressed body, and the command JSON must be at most 8 MiB plus 64 KiB (8,454,144 bytes) after decompression.
A project command that is larger than 4 MiB must use gzip.
A gzip body must have one gzip member and no data after it.
A request with another content encoding gets HTTP 415 with `Accept-Encoding: gzip`.
The server decompresses, decodes, and applies one gzip command or one plain command of more than 1 MiB at a time.
Other such commands wait, and the server decompresses a gzip body only after the wait.
A gzip command, and a plain command with a `Content-Length` of more than 64 KiB or with no `Content-Length`, needs one of 4 admission places before the server reads the body.
The command keeps its place until the server applies it.
When all places are in use, the server replies at once with HTTP 503, `Retry-After: 1`, and a plain text body.
A smaller plain command, such as a pause, does not need a place.
Each array in the body must have no more items than the project limits permit, also for an action that does not use the project.
For example, `project.network.Lanes` can have at most 8,000 items.
Each string and each member name in the body must have at most 1,024 bytes, including the quotes and the escapes.
A request with an `Origin` header must come from the same host and scheme.
A request that breaks these rules gets HTTP 400, 403, 413, 415, or 503 and a plain text body, not an acknowledgment.
A body that is larger than a size limit, before or after decompression, gets HTTP 413, and the text gives the limit in bytes.
These responses also have `Cache-Control: no-store`.
A 403, 415, or 503 reply comes before the server reads the body, and a 413 reply for a body over the 4 MiB limit comes before the end of the body.
The server closes the connection after these replies, so it does not wait for the rest of the body.

A `project` command with more than 200 stations, 4,000 nodes, 8,000 lanes, or 200 pods gets HTTP 400, because these arrays are larger than the limits above.
A station with more than 200 berths also gets HTTP 400.
A `project` command gets `command_rejected` when the project fails validation, for example when a node has more than 64 lanes.
A lane counts at its start node and at its end node.
The total of the lane pairs at the nodes, the node coordinates, and the total of the track cells of the lanes also have limits.
Two lanes with the same start node, end node, and path also give `command_rejected`.
See [project files and validation](../README.md#project-files-and-validation) for all limits.

## Save points

The `checkpoint` action saves the simulation, the demand stream, and the project in server memory.
Its acknowledgment gives the new save point ID in `checkpoint`.
The first ID of a new session is 1.
After a `physical` or `logical` restore, the IDs continue from the saved session.
The server does not use an ID again in the same epoch.
This is also true after a reset, a demo, a project apply, or a rewind.

The `rewind` action restores one save point.
The command must give the ID in `checkpoint`.
There is no default save point.
Other actions ignore this field.
A rewind keeps the epoch, the playback speed, and the save points.
It increases the revision and the generation by one, and pauses the session.

State frames list the retained save points in `checkpoints`, oldest first.
Each item has an `id`, a `tick`, and an optional `restoresProject`.
The server keeps at most 8 save points.
At the limit, a new save point removes the oldest one.
A frame without save points omits the `checkpoints` key, so the key adds no bytes until a save point exists.

`restoresProject` is `true` when the project or demand configuration of the save point is different from the current one.
Each project apply and each demand change counts as a change, even if the values stay the same.
The server omits the key when the value is `false`.

A rewind to such a save point restores its project and increases the project revision by one.
With `-project`, the server also saves the project to that file.
The project revision never goes back to an earlier value, so clients fetch the topology again.
The server also rejects project edits from before the rewind.
The acknowledgment of this rewind has `projectRestored` set to `true`.
A repeated rewind to the same save point does not save or restore the project again, and its acknowledgment omits `projectRestored`.
If the save fails, the server rejects the rewind with `command_rejected`, and nothing changes.

A rewind does not roll back the command receipts.
An exact retry of a rewind gets the stored acknowledgment and does not rewind again.
This is also true after another client resumes the session.

Commands do not name a generation, and the server does not use the generation to match a retry to its receipt.
A command can arrive after another client rewinds, even if its sender has not seen the rewind.
The server then applies the command to the restored state.
An exact retry gets the stored acknowledgment, even if a later rewind removed the effect of the command.
A reset, a demo, and a project apply work the same way.

Save points are in memory only.
A server restart clears them.
The `-state` option does not save them.
A rewind to an unknown or removed ID gets `command_rejected`.

## Server restarts

Without `-state`, a server restart starts a new session with a new epoch.

With `-state`, the server saves the session state and restores it at the next start.
The server keeps the saved epoch only when all of these are true:

- The saved state came from a final save.
- The restore tier is `physical` or `logical`.
- The saved state has fewer than 1,024 clients.

The server makes a final save at a graceful shutdown.
When startup fails after the startup save, for example because a listen address is in use, the server also makes a final save.
See [graceful shutdown](operations.md#graceful-shutdown) for a shutdown without a final save.

After a stop without a final save, the saved state can be older than the frames that clients saw.
In its epoch, a client drops each frame from the same server process with a lower revision.
A kept epoch can repeat revisions and IDs that clients saw, so the server uses a new epoch.
The server also uses a new epoch when it does not use the saved state.

With a kept epoch, the revision and the generation are one more than in the saved state.
The project revision does not change, but the Go client fetches the topology again because the server start ID changed.
But when the `-project` file has different demand settings, the server applies them as a `demand` command does, and the project revision increases by one.
The generation change resets motion.
Save point IDs and order IDs continue from the saved values.
Thus a normal restart does not make the server use an ID again in the epoch.
When an operator restores an older file from a final save, IDs can repeat, and the revision can be lower than the revision that clients saw.
The Go client accepts a frame with a lower revision when its `serverStart` is not empty and is different from the `serverStart` of the last frame.
It then also discards its buffered map motion and fetches the topology again, because a restored save can reuse an epoch and a project revision with other geometry.
Thus it shows the restored state and the restart notice.
The client does not compare the frames of the new process with the revision and the generation in the reply to a command that it sent while it showed a frame of the earlier process.
Thus Rewind becomes available when the state lists a new save point, and a reset from another browser shows its notice.
See [session state](operations.md#session-state).

A startup that fails after the startup save writes the increased revision and generation in its final save.
Thus after one or more failed startups, clients can see an increase of more than one.

The server saves the last sequence of each client, but not the command receipts.
After a restart with a kept epoch, a command with the saved sequence of its client or a lower one gets `expired_command`.
The server got a command with that sequence before the restart, but it did not save the acknowledgment, so it does not apply the command again.
For example, a retried `trip` does not make a second order.
Read the current state to find the result of the first command.
A command with a higher sequence is new.
The saved clients stay in the limit of 1,024 clients.
When the saved state has 1,024 clients, the server uses a new epoch, so that new clients can send commands after the restart.

With a new epoch, clients switch to the new session.
A command from the old epoch gets `session_changed`.
The server does not keep the saved sequences, and the limit of 1,024 clients starts again.

Each restart gives a new `serverStart` ID, with a kept epoch or a new epoch.
A command never changes this ID.
A restored final save can have the same epoch and project revision as an earlier process, but a different project.
Thus a `project` command with the `serverStart` of an earlier process gets `session_changed`, also with a kept epoch.

The Go client tells the user about a restart.
When two frames both have a `serverStart` ID and the IDs are different, the server restarted.
This rule also finds a restart when the client did not get the first frames after the restart, for example in a hidden browser tab.
The client does not show a notice for its first frame.

When one of the two frames has no `serverStart` ID, the server is older, and the client uses the epoch and the restore tier.
Only a restart makes a new epoch.
With a kept epoch, the first frame after the restart has a new generation, a `restore` tier of `physical` or `logical`, and no save points.
A reset, a demo, and a project apply remove the `restore` key, and a rewind keeps the save points.
Thus a command never makes a frame with all three of these properties.

## Payload measurements

The comparison used equivalent states with 200 accepted requests.
It used the Scale100 and London projects from commit `cf50eca`.
Commit `ed5d782` later separated the London station portals by direction.
The current London network has more than twice as many lanes.
The London numbers in this document do not describe the current London project.
Both formats used gzip level 1.
These are deterministic codec samples, not network throughput limits.

| Fixture | Format | Raw frame | Gzip frame | Traffic at 20 Hz |
| --- | --- | ---: | ---: | ---: |
| Scale100 | Previous JSON shape | 394,962 B | 44,444 B | 0.89 MB/s |
| Scale100 | Normalized JSON | 116,556 B | 11,600 B | 0.23 MB/s |
| Scale100 | Binary Protobuf | 65,399 B | 9,775 B | 0.20 MB/s |
| London | Previous JSON shape | 743,060 B | 94,779 B | 1.90 MB/s |
| London | Normalized JSON | 132,915 B | 17,513 B | 0.35 MB/s |
| London | Binary Protobuf | 71,143 B | 15,704 B | 0.31 MB/s |

For London, normalization reduces the gzip frame by 81.5%.
Binary Protobuf reduces the normalized frame by another 10.3%.
Its raw frame is 46.5% smaller, but gzip removes most of that difference.

Topology is a one-time cost per project revision.
London topology was 51,803 gzip bytes as JSON and 46,374 gzip bytes as Protobuf.
The editor project is not part of the polling path.

The payload data is in [`measurements/protocol-normalized.csv`](measurements/protocol-normalized.csv).
The earlier live samples remain in [`measurements/protocol-payloads.csv`](measurements/protocol-payloads.csv).

The `build` key adds 11 bytes plus the length of the build ID to each raw state frame, or 27 bytes for a 16-character ID.
With gzip level 1, sampled frames of the example, Scale100, and London projects grew by about 20 bytes.
The CSV files do not include these samples.
Frames from a server without a build ID do not change.

The `serverStart` key adds 33 bytes to each raw state frame.
Every server sends it.

Commit `1fe7b07` replaced the `Request` and `Parties` members of each vehicle with `Riders` and `Stops`.
Commit `65ce323` added the ride metrics.
The measurement used the same 200 accepted requests with the current Scale100 and London projects.
It encoded the frame at 0 s, 60 s, and 120 s of simulation time, at commit `4f2ad0a` and at commit `adc7e1c`.
The table gives the frames at 120 s.

| Fixture | Party limit | Gzip frame at `4f2ad0a` | Gzip frame at `adc7e1c` | Increase |
| --- | ---: | ---: | ---: | ---: |
| Scale100 | 1 | 13,440 B | 13,854 B | 3.1% |
| Scale100 | 4 | 8,688 B | 10,308 B | 18.6% |
| London | 1 | 21,484 B | 22,076 B | 2.8% |
| London | 4 | 18,301 B | 19,837 B | 8.4% |

With a party limit above 1, each party that joined a pod adds a full order to the frame.
Before, it added only to the `Parties` count.
At 20 Hz, the largest London frame is 0.40 MB/s.
The data is in [`measurements/protocol-riders.csv`](measurements/protocol-riders.csv).

## Codec measurements

The native benchmark ran on an AMD Ryzen 5 3600.
Each value is the median of three one-second benchmark runs.

| Fixture | Operation | Normalized JSON | Binary Protobuf | Protobuf ratio |
| --- | --- | ---: | ---: | ---: |
| Scale100 | Encode | 0.409 ms | 0.121 ms | 3.38x faster |
| Scale100 | Decode | 0.851 ms | 0.272 ms | 3.13x faster |
| London | Encode | 0.464 ms | 0.136 ms | 3.42x faster |
| London | Decode | 0.991 ms | 0.312 ms | 3.18x faster |

Protobuf uses one allocation when encoding.
JSON uses three.
London Protobuf decode allocates about 255 KB, compared with 277 KB for JSON.
It creates 4,893 allocations instead of 3,513 because of generated nested message pointers.

Level-1 gzip compression took 0.310 ms for the London JSON frame and 0.285 ms for the Protobuf frame.
At 20 Hz, marshal and compression use about 15.5 ms of CPU per wall second for JSON and 8.4 ms for Protobuf.
The saving is about 0.7% of one full CPU core per connected client.

The gzip-level comparison used five more one-second runs for the London frame.
The `json_gzip_level_1` and `json_gzip_level_6` rows of [`measurements/protocol-normalized-codec.csv`](measurements/protocol-normalized-codec.csv) come from these runs.

| Fixture | Gzip level | Frame size | Compression time |
| --- | ---: | ---: | ---: |
| Scale100 | 1 | 11,600 B | Not measured |
| Scale100 | 6 | 7,921 B | Not measured |
| London | 1 | 17,513 B | 0.318 ms |
| London | 6 | 14,499 B | 0.640 ms |

Level 6 saves 3,014 bytes, or 17.2%, for each London frame.
It takes about twice the compression CPU.
At 20 Hz, it adds about 6.4 ms of CPU per wall second and saves about 60 KB/s for each client.
The server uses level 1 because CPU is the tighter resource when the server gets only a part of a shared CPU.

These measurements exclude frame construction, Protobuf conversion, HTTP work, decompression, rendering, and simulation work.
They do not show the fraction of total application CPU.

The codec data is in [`measurements/protocol-normalized-codec.csv`](measurements/protocol-normalized-codec.csv).
The earlier codec samples for the previous JSON shape remain in [`measurements/protocol-codec.csv`](measurements/protocol-codec.csv).

## Removed ConnectRPC experiment

The experiment defined matching topology, state, project, and command methods.
It used Connect-Go `v2.0.0-alpha.1`, generated Go clients and handlers, and binary Protobuf.
Tests verified state reconstruction, project conversion, command retries, and invalid commands.

The experiment added 4,055 checked-in lines, including 3,065 generated lines.
It increased the unembedded server binary by 782,126 bytes, or 2.9%.
No application client used the RPC service, so it provided no live CPU or traffic saving.
Commit `cf50eca` preserves the implementation and benchmark source.

Reconsider a typed RPC protocol when at least one condition is true:

- Remote use makes a 10% to 16% gzip saving significant.
- Multiple viewers make JSON codec CPU significant.
- External clients need a generated schema.
- The application needs streaming or gRPC compatibility.

Measure browser decode and state-application time before a protocol change.
Add deltas or streaming only if normalized complete frames become a measured limit.
