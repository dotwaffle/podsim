# Distribution and operations

## Embedded server build

Generate all browser files before an embedded build:

```sh
go generate ./...
go build -trimpath -tags=embed_assets -o podsim-server ./cmd/serve
```

The executable contains the HTML, CSS, JavaScript, and gzip WASM files.
It does not contain an uncompressed WASM module.
This keeps the executable at about 41 MB, not 65 MB.
The server decompresses the module in memory only for a client without gzip or for a Range request.
It does not need a `dist` directory at runtime.
The `-dir` option overrides embedded files for development.
Without the `embed_assets` build tag and without `-dir`, the server reads the `dist` directory in the working directory.
Production builds keep Go and WASM debug information.

The server sends browser files with `Cache-Control: no-cache` and API responses with `Cache-Control: no-store`.
The API error responses, for example HTTP 404 for an unknown `/api` path, also have `Cache-Control: no-store`.
An error response for the WASM module also has `Cache-Control: no-store`.
A browser must check a cached file with the server before it uses the file, so a reload after an upgrade loads the new files.
Files from `-dir` or `dist` have a modification time, and the server answers `304 Not Modified` when a file did not change.
The WASM module has only an ETag that comes from its content, and no `Last-Modified` header.
Thus a new module with the same modification time gets a full response.
Embedded files have no modification time, so browsers download them again on each load.
The WASM module is an exception, because a browser can check its ETag.

The application serves `GET /healthz` without reading simulation state.
The response is `200 OK` with the body `ok` and a newline.

### Compression and the WASM module

State stream messages always use gzip.
HTTP snapshots and browser assets use gzip when the client supports it.
Range responses remain uncompressed.

The build keeps only `podsim.wasm.gz`, which it compresses at the maximum level one time.
The server sends these bytes to browsers that accept gzip.
For a client without gzip or for a Range request, the server decompresses the module on the first request and keeps it in memory.
Byte ranges then use the offsets of the uncompressed module.
The build writes `podsim.wasm.gz` to a temporary file and then renames it, so a running server never reads a partial module.

The server sends the module only from `podsim.wasm.gz`, also when the directory has a `podsim.wasm` file.
A `-dir` directory without `podsim.wasm.gz` answers `404` for the module, so the page then shows that it could not start.

## Build ID

At startup, the server makes a build ID from the browser files that it serves.
It hashes the path, the size, and the content of each file with SHA-256.
The build ID is the first 16 hex characters of the hash.
An embedded build and a `-dir` directory with the same files get the same ID.
Different browser files get a different ID.
The startup record `Open Podsim in your browser` gives the ID in `build`.

State frames carry the build ID.
When the ID changes after a server restart, the simulation view in each open browser page reloads and gets the new browser files.
If the editor has not opened in the page, the full page reloads.
If the editor has opened in the page, only the simulation reloads.
The editor keeps its state, and the page and the editor keep the old browser files until you reload the page.
The simulation tells the page which version of the page messages it uses.
If the new simulation does not use the version of the page, the page shows its own **Edit scenario** and **Download debug state** controls in the top left corner.
Then the result of a debug capture shows next to these controls.
When a simulation that uses the version of the page then starts, it shows the last failure, or a download result that is less than 3 s old.
An editor that you opened as `editor.html` does not reload.
If the server cannot read the browser files, it logs `Browser build ID unavailable` as a warning and uses a random ID.
Then the simulation view reloads after each server restart.

## Container image

The Check workflow publishes `ghcr.io/dotwaffle/podsim` from version tags and manual workflow runs.
A push to `main` runs the checks and publishes no image.
The workflow publishes only after every check job passes.
It builds `linux/amd64` and `linux/arm64` images with ko.
Each image gets the commit SHA as a tag.
Images from a version tag also get the name of that tag and the `latest` tag.
The image uses the Chainguard static base and runs as UID and GID 65532.
Ko attaches an SPDX software bill of materials by default.

The default `-addr` value is `127.0.0.1:8080`.
A published container port cannot reach a loopback address.
Run the image with the application port bound on all container interfaces:

```sh
docker run --rm -p 8080:8080 ghcr.io/dotwaffle/podsim:latest -addr :8080
```

The `-project` option needs an existing project file.
Mount its directory with write access for UID 65532.
Without write access, the server rejects project applies, demand changes, and rewinds that restore a project.
For each of these changes, the server replaces the file with compact JSON of 32 MiB or less.
The new file belongs to UID 65532 and has mode 0600.

The `-state` option needs a directory that UID 65532 can write.
Make the directory on the host, then mount it in the container:

```sh
sudo install -d -o 65532 -g 65532 -m 0700 /srv/podsim-state
docker run --rm -p 8080:8080 -v /srv/podsim-state:/var/lib/podsim --stop-timeout 30 \
  ghcr.io/dotwaffle/podsim:latest -addr :8080 -state file:///var/lib/podsim
```

If the server can open files in the directory but cannot write to it, startup stops with a `write startup session state` error.
But if the server rejects the saved state, the move of the file fails first.
The server then starts with saving off and logs a `Move rejected session state` error.
If the server cannot read `session.json.gz`, it starts with saving off and logs a `Read saved session state` error.
For example, this occurs when the server has no access to the directory.
On Kubernetes, set `securityContext.fsGroup` to 65532, so that the server can write to the volume.
Use one replica and the `Recreate` strategy, because only one server can use a state location:

```yaml
spec:
  replicas: 1
  strategy:
    type: Recreate
  template:
    spec:
      terminationGracePeriodSeconds: 30
      securityContext:
        fsGroup: 65532
      containers:
        - name: podsim
          image: ghcr.io/dotwaffle/podsim:latest
          args: ["-addr", ":8080", "-state", "file:///var/lib/podsim"]
          volumeMounts:
            - name: state
              mountPath: /var/lib/podsim
      volumes:
        - name: state
          persistentVolumeClaim:
            claimName: podsim-state
```

## TLS-terminating reverse proxies

Without `-public-origin`, browser commands and WebSocket upgrades must use the request Host and the backend connection scheme.
A proxy that terminates HTTPS and forwards plaintext HTTP needs an explicit public origin:

```sh
podsim-server -addr :8080 -public-origin https://podsim.example.com
```

Use one absolute HTTP or HTTPS origin with an authority only.
The server rejects credentials, paths (including a trailing slash), queries, fragments, invalid hostnames, and ports outside 1 through 65535 at startup.
Use ASCII DNS names or punycode for internationalized names.
IPv6 literals need brackets and cannot include a zone identifier.
Hostname case, equivalent IPv6 notation, and the scheme's default port are normalized.
An explicit trailing DNS dot remains significant.

When configured, both the request Host and browser Origin must match that public authority.
The Origin must also use the configured scheme.
Host default ports are interpreted with the configured public scheme, regardless of the backend transport.
The proxy must preserve that public Host instead of replacing it with an internal backend authority.
Duplicate, multiple, malformed, and `null` Origin values are rejected.
Native clients may omit Origin, but their Host must still match when configured.

`Forwarded`, `X-Forwarded-Proto`, and `X-Forwarded-Host` do not affect these checks.
The setting does not provide TLS, authenticate clients, or authorize other public names.
Use the deployment's canonical HTTPS origin for a TLS-terminating proxy such as Fly Proxy.
The setting removes the backend-scheme mismatch identified in the local proxy audit.

## Session state

The `-state` option keeps the shared session across server restarts.
Its value is a bucket URL.
This server build has only the `file://` driver.
Without `-state`, the server does not save or read a session state.
The saved state holds the last command sequence of each client, but not the save points or the command receipts.
A restart clears those.
Clients use stream version 9, including motion-only vehicle deltas, as described in [Client protocol](protocol.md#shared-state-stream).

A `file://` URL needs an empty host and an absolute path, for example `file:///var/lib/podsim`.
`file://var/lib/podsim` is not valid, because `var` is then the host.
The only query parameter is `prefix`.
The server puts it in front of each file name.
For example, `prefix=podsim/` puts the files in the `podsim` subdirectory, and `prefix=podsim-` gives `podsim-session.json.gz`.
The prefix can contain only ASCII letters, digits, `.`, `_`, `-`, and `/`.
It cannot contain `..` or `__`.
It must be a clean relative path.
Thus it cannot start with `/`, contain `//`, or have a part that is only `.`.
Do not put credentials in the URL.
The server logs and errors show the URL without user information and query values.
But the error text of a storage driver can contain the full URL.

The server makes a missing directory with mode 0700.
Each file gets mode 0600.
The server syncs each file and its directory before it continues.
Startup stops with an error when the URL is not valid, when the server cannot open the location, or when the startup save fails.

| File | Content |
| --- | --- |
| `session.json.gz` | The saved session state. It is one JSON object, compressed with gzip, and it contains the project. |
| `session.rejected.<time>.json.gz` | A saved state that the server did not use. `<time>` is the UTC time of the move, for example `20260923T090000.000000000Z`. The server keeps the 3 newest files. |
| `session.previous.json.gz` | A copy of the saved state before a `logical` restore, or before a restore that moved a pod to a berth. The next such restore replaces it. |

The server writes each file through a temporary file that ends in `.tmp`.
At startup, the server deletes the temporary files that an interrupted write left.

The server saves the session state at these times:

- At startup, after the restore.
- Every 60 seconds, when the session changed after the last save.
  The first periodic or command save after the start always writes.
- Before the reply to a project apply, or to a rewind that restores a project.
  This is the command save.
  It counts as a periodic save, but it gets less time.
- About 1 second after a demand change, a project apply, or a rewind that restores a project.
  This save counts as a periodic save.
- At a graceful shutdown, and when startup fails after the startup save.
  This is the final save.

A save copies the state while it holds the session lock.
It encodes, compresses, and writes the copy after it releases the lock.
Each saved pod or waiting trip route has at most 1,024 lanes.
Smaller networks keep the lower bounds: lanes plus nodes for a pod, and nodes for a trip.
The server omits a longer route without changing live routing or motion.
Physical restore moves a pod with an omitted or over-limit route to a free berth.
An over-limit trip loses its route and pod bindings.
The decoder keeps larger route-array caps so long routes do not change the header refusal order.
`TestPlainStateFileWorstCaseSize` bounds plain saved JSON, including a project at its byte cap, below 128 MiB.
Express has no byte proof.
Each startup or periodic save gets 30 seconds.
A command save gets 2 seconds.
This time includes the wait for an earlier save.
The Go client of the simulation view sends an exact retry when it gets no reply in 3 seconds.
The shorter save time lets the reply come before the retry.
An exact retry of a project apply, or of a rewind that restored a project, also makes a command save before its reply.
This save waits for the command save of the first request.
It writes nothing when the session did not change after that save.
If the command save fails or takes more time, the command still succeeds, and the save about 1 second later tries again.
After the command save, the reply shows in `stateSaved` whether a saved state holds the command.
The value is `true` when the last successful save of any kind holds the state at the `revision` of the reply or at a later revision.
An exact retry gets the `revision` of the first reply.
A later state counts, also with changes from other clients, because a restore of it cannot go back to the state before the command.
The value is `false` when no successful save holds such a state, for example after a failed save or a save that took more time.
After a graceful shutdown starts, a command save writes nothing, and the final save can still fail.
Thus a reply then has `true` only when an earlier save, for example the final save, holds such a state.
When the value is `false`, the simulation view and the editor show a warning.
A failed write keeps the old file.
The exception is a failed sync of the directory.
The new file is then in place, but a power loss can bring back the old file.
After a stop without a final save, the next start restores the last saved state, which can be up to about 60 seconds old.

At startup, the server reads `session.json.gz` and restores the session with one of these tiers.
Before each tier, the server checks each saved pod against the rule of its phase.
The rule tells which fields the phase needs and which fields it forbids.
For example, only a boarding, traveling, unloading, or continuing pod can have a party aboard, and its stops must be the stations ahead where its parties leave the pod.
Each order ID must be in one place only: in the queue, or with one pod.
A file that fails these checks gets `invalid_state`.
A queued order that is not valid does not stop the restore.
The tier removes it and counts it as dropped.
After each tier, the server checks each saved order by its ID.
The order must be complete, in the queue, aboard a pod, or dropped, and in one place only.
A tier that loses an order or adds an order fails.
A file can hold fewer orders than it submitted.
The server restores such a file, but it does not make up the missing orders.
It reports them as unaccounted orders at each restore, together with the orders that an earlier restore dropped.

- `physical`: The pods keep their lane positions and start again at speed 0.
  The server makes the track reservations again.
  A traveling pod that conflicts with another pod, or that has a route that the server cannot restore, goes to a free berth.
  The routes of the restore must fit in a budget of track cells.
  The budget is 32 times the cells of the network plus 4 cells for each lane, but at most 256,000 cells.
  When the saved routes do not fit, the pods with the longest routes go to a free berth first.
  The tier fails when two pods at berths conflict, or when a traveling pod finds no free berth.
  Its parties board again at their origin station, or go back to the queue.
  In the `drop-offs` mode, the parties go back to the queue when the stops from the free berth take a party over the detour cap of 1.5.
  A pod in a [virtual platoon](model.md#virtual-platoons) keeps its link to the pod ahead, with the same run of lanes, turn, and clearance.
  The tier checks each saved link against the network and the other pods, and a link that is not valid fails the tier.
  A run can end on the entry lane of the destination station of both pods.
  A run that holds a lane from the entry node of that station, or the entry lane of another station, is not valid.
  A run that holds a lane of a [maneuver group](model.md#compact-bays) is not valid.
  A run that holds a lane of a [depot](model.md#parking-depots) is not valid.
  The file does not keep the platoon limit, so the restore uses the `platoonLimit` of the project.
  The file keeps the shared ride mode.
  A file without the mode restores in the default `drop-offs` mode.
  Each faulted pod keeps its place at speed 0, and each debris record keeps its segment.
  Each fault keeps its start and end ticks.
  When the tier must move a faulted pod to a berth, the tier fails.
  Each emergency keeps its pod, and its phase comes from the purpose of the restored pod.
  When the tier must move the pod of an emergency to a berth, the tier fails.
- `logical`: The server uses this tier with reason `physical_failed` when the `physical` tier fails.
  It also uses it with reason `restore_loop`, as described below.
  The pods start again at their initial berths.
  Parties that were unloading at their stop count as completed, if the pod is at a berth of a passenger station in the network.
  The party of an emergency unload ends interrupted.
  Each other party in a pod goes back to the queue as one order.
  So the server refuses a file whose waiting orders and outstanding parties together exceed the queue bound of its contract: 8,200, or 17,000 with Express.
  An active traffic demo also counts the orders that it has still to submit.
  Every fault ends, and each pod loses its fault hold.
  The fault counters stay.
  Every emergency ends, and each pod loses its emergency hold.
  The emergency counters stay.
- `empty`: The server does not use the saved state and starts a new session.
  Except after a read failure, it moves `session.json.gz` to a rejected file.

Each session writes saved-state version 11, and this server accepts only version 11.
The root `orderContract` marker `express-v1` selects the Express order bounds.
The project and the simulation must have the same marker as the root.
The file can have these incident members, and it omits each one at 0 or when it is absent:

- `simulation.interrupted` and `simulation.interruptedPassengers`: the orders that ended interrupted, and the sum of their party sizes.
- `simulation.incidentSerial`: the serial of the last incident record.
- `withdrawn` of a pod: its service holds, 1 for a fault and 2 for an emergency.
- `operational` of a pod: its operational destination, `[purpose, owner]`, or `[1, owner, interrupt]` for an emergency unload that interrupts riders.
  The purpose is 1 for an emergency unload, 2 for a refuge, and 3 for an empty recovery.
  The owner is one hold of the pod, and each bit of `interrupt` names an active rider by its index.
- `legFrom` of a rider or of a queued order: the station where the party boards its current pod, as an index into `project.network.stations`.
- `excludedPod` of a queued order: the pod that the order must not get, as an index into `simulation.pods`.

Index 0 is a present value.
A leg origin is a passenger station other than the destination.
An order with an excluded pod did not board, and its pod and its hold are not the excluded pod.
The file must not have a null incident member.
A pod with hold 2 or purpose 1 needs an emergency record.

The restore refuses these saves before either tier, because no code can release their holds:

- A pod with a refuge.
- A pod with an empty recovery that the emergency hold owns.
- A demo save with a hold, a purpose, an owner, or an interrupt on a pod, or with `simulation.faults` or `simulation.emergencies`.
  A save is a demo save when it has `simulation.demo`, or when it has more pods than its fleet.

Each of these saves gets `invalid_state`.

The file can have `simulation.faults`, with these members:

- `records`: the active faults, in the order of the fault serial.
  A pod fault is `[generation, serial, start, end, 0, pod]`, and debris is `[generation, serial, start, end, 1, lane, from, to]`.
  `start` and `end` are ticks, and `end` is 0 for a fault without an end.
  `pod` is an index into `simulation.pods`, and `lane` is an index into `project.network.lanes`.
  `from` and `to` are the debris segment in meters.
  The file can have one record for each pod and 64 debris records, so at most 1,064 records.
- `counters`: the fault counters `started`, `cleared`, `evacuations`, `reroutes`, `faultWaitTicks`, `sampled`, `skipped`, `clearedTicks`, and `delayedRiderTicks`, as in the [protocol](protocol.md#fault-members).
- `random` and `nextTick`: the state of the fault sampler.
  `random` is the base64 text of the 20 bytes of its random source, and `nextTick` is the tick of its next event.
  A `nextTick` of 2^53-1 means that no more events come.
  The file has both members exactly when the sampler is live: the project rate is above 0, and the fleet is not the demo fleet.

The file omits `simulation.faults` when no fault is active, each counter is 0, and the fault sampler is not live.
It omits an empty `records` and each counter at 0.
The file must not have a null fault member, or an empty `simulation.faults`.
A `simulation.faults` with only `random` and `nextTick` is not empty.

The file can have `simulation.emergencies`, with these members:

- `records`: the active emergencies, in the order of the incident serial.
  An emergency is `[generation, serial, start, pod, order]`.
  `start` is a tick, `pod` is an index into `simulation.pods`, and `order` is the order ID of the party.
  The file can have at most 4 records.
- `counters`: the emergency counters `started`, `ended`, `emergencyTicks`, `sampled`, `skipped`, `endedTicks`, and `maxTicks`, as in the [protocol](protocol.md#emergency-members).
- `random` and `nextTick`: the state of the emergency sampler, by the rules of the fault sampler.

The file omits `simulation.emergencies` when no emergency is active, each counter is 0, and the emergency sampler is not live.
It omits an empty `records`, each counter at 0, and `counters` when each counter is 0.
The file must not have a null emergency member, or a `simulation.emergencies` with no record, with each counter at 0, and with no sampler member.
Each record must have exactly 5 numbers.

The order text of each queued order and each rider is canonical base64 text, for each project kind.
The file has no `textEncoding` member.
Each saved version stores a version 1 project.
A saved project of version 2 through 5 gets reason `invalid_state`, as other bad saves do.
An intact file of version 1 through 10, or of version 12 or later, gets reason `unsupported_version`.
The server moves it aside and starts a new session.
It does not migrate the file.
A damaged or invalid file gets `invalid_state`, and the server moves it aside and starts a new session.
Examples are a gzip error, a JSON syntax error, a file over a scan limit, a header member of the wrong type, a missing, null, zero, or negative version, a value that the decoder refuses, and a restore that fails.
Until the first release, the removal of a member keeps the version.
Thus a version 11 file with a member of the removed physical coupling feature, for example `couplingContract` or `simulation.couplingGroups`, has an unknown member and gets `invalid_state`.
Likewise, a version 11 file with a member of the removed station buffer or compact queue feature has an unknown member and gets `invalid_state`.
A version 11 file with `incidentContract`, `faultContract`, or `emergencyContract` also has an unknown member and gets `invalid_state`.
Examples are `stationBuffered` or `compactQueue` on a pod, and `stationBuffers` or `stationQueueSpacing` in the embedded project.
A project file with `stationBuffers` or `stationQueueSpacing` also fails as an unknown member.
Until the first release, a narrower set of accepted values also keeps the version.
Each integer of a version 11 file must be from -9007199254740991 to 9007199254740991, which is 2^53-1, as in the [protocol](protocol.md#shared-state-stream).
The demand `seed` is the only exception.
The scan that checks the integers comes after the version check, so an intact file of another version still gets `unsupported_version`.
Within the scan, the depth and the size checks of a value come before its integer check.
A file with a larger integer gets `invalid_state` with the error `JSON integer is out of range`.
A saved revision, project revision, or generation of 2^53-1 also gets `invalid_state`, because the restore adds 1 to it.
After that check, a saved last save point of 2^53-1 gets `invalid_state` with the error `last save point ... is at the largest value`, because the next save point adds 1 to it.
Each ID of a version 11 file has only the characters A-Z, a-z, 0-9, `.`, `+`, and `-`.
Each name and each dispatch reason is UTF-8 text without a control character, which is a C0 character, DEL, or a C1 character.
After the length check of the epoch, an epoch with another character gets `invalid_state` with the error `epoch ... has a character other than A-Z, a-z, 0-9, '.', '+' or '-'`.
After the length check of a client ID, a client ID with another character gets the error `client ... has a character other than A-Z, a-z, 0-9, '.', '+' or '-'`.
After the canonical base64 check, packed order text with another character gets `order ID has a character other than A-Z, a-z, 0-9, '.', '+' or '-'`, and a dispatch reason with a control character gets `order text has a control character`.
The embedded project gets the error of project validation.
The server does not try to recover any part of such a file.
A file of more than 128 MiB is the only exception: the server keeps it, turns saving off, and fails to start.
The server checks the fault records before either tier.
A fault record that is not valid gives `invalid_state` for the whole file, and the server does not try the `logical` tier.
It does not remove one record to keep the others.
Examples are records out of serial order, a serial above `incidentSerial`, a tick out of range, a negative counter, and more than 64 debris records.
Other examples are a pod record for a pod without the fault hold or for a pod in a platoon, and a pod with two records.
A debris segment that is not valid, and debris that meets other debris or a faulted pod, also give `invalid_state`.
In the `physical` tier, a traveling pod that holds a resource of debris gives `invalid_state`.
A file of the traffic demo with a fault record also gives `invalid_state`, because the demo runs without faults.
The server also checks the emergencies before either tier, and an emergency that is not valid gives `invalid_state` for the whole file.
Examples are records out of serial order, a serial of 0 or above `incidentSerial`, and a serial that a fault record also has.
Other examples are a `start` out of range, an order ID that is not positive, a negative counter, and more than 4 records.
The server checks the sampler members of both groups in the same way.
It refuses one of `random` and `nextTick` without the other, both with a rate of 0, and neither with a rate above 0, except in a demo save.
It also refuses a `random` that is not valid, and a `nextTick` at or before the saved tick, except 2^53-1.
A save with more `skipped` than `sampled` events fails after each tier, and it also gets `invalid_state`.
A logical restore of a demo save gives an ordinary fleet, so it starts new samplers from the restored tick.
A pod index out of range, a pod with two records, and a pod with hold 2 and no record also give `invalid_state`.
A pod with purpose 1 must have the owner 2 and a record.
Then the party of the record must be aboard the pod, and `interrupt` must name only that party.
A pod with a record and purpose 2 or 3 must have no party aboard.
Bank-inconsistent retained routes reject restoration before either tier.
See [independent station banks](station-banks.md) for bank membership, routing, and browser editing.
The optional network member `maneuverGroups` of the [compact bays](model.md#compact-bays) is absent from a network without groups.
The optional network member `depots` of the [parking depots](model.md#parking-depots) and the optional pod member `parkedSince` follow the same rule.
A file writes `parkedSince` only for a pod that is idle at a depot slot or moves up in one depot lane.
After the earlier checks, the restore refuses a negative parked tick, and then a parked tick after the saved tick.
Then it refuses a parked tick on a pod that is not parked in a depot lane.
Last, it refuses a depot lane whose pods are not in slot order from the head to the tail.
In the physical tier, a pod that left a slot keeps the lanes of its route that its origin still holds.
A pod bound at the entry keeps its reservation up to the end of the incoming lane, or through its slot when it held that grant.
A pod that the tier must move takes a slot only when no pod of that lane moves, and only the slot behind the rearmost taken slot.
Its parked tick is then the saved tick.
The file does not keep the 1 second stop at a change of the reverse flag.
Thus a pod that was in this stop stops for the full second again after a restore.
An older server rejects version 11 with `unsupported_version` and moves the file aside.
Keep a copy before a downgrade.

Portable project version 1 accepts an optional `pickupReassignment` Boolean setting.
A file that omits it has the setting off, and a canonical export omits it when false.
The presets and the example project turn it on.
The server rejects a non-Boolean value, including `null`.
This setting is not a qualified capacity improvement.
Project load, reset, apply, demo, and checkpoint rewind preserve the selected setting.
After file restore, the effective project controls reassignment.
A startup project can change this setting without replacing valid saved physical state.
Other project identity checks remain in force.
Reassignment cursors, cooldowns, counters, and experiment records reset after file restore.
The saved routes and request bindings remain valid, but future reassignment decisions can differ after restart.
The [file restart checks](studies/experimental-policy-restarts.md) cover pickup reassignment, the rejection of a version 2 file, and canceled or failed-sync saves.
They do not simulate power loss.
The optional project setting does not change command or WebSocket envelope formats.

The reason for an `empty` start is `project_changed`, `unsupported_version`, `invalid_state`, `restore_loop`, or `unreadable`.
A file of more than 128 MiB, compressed or decompressed, does not give an `empty` start, because the server keeps the file and fails to start.
A file with another format version gets `unsupported_version`.
Until the first release, an added optional member with a safe zero value keeps the format version.
The file leaves out the member when its value is zero.
Each other change to the members of the file gets a new format version.
The server matches member names exactly.
With `-project`, the project file has priority, and a saved state with a different project gets `project_changed`.
When only demand, pickup reassignment, or onboard pickup settings differ, the server restores the saved state.
A demand change writes the project file at once and the session state about 1 second later.
Thus a crash between the two writes can leave this difference.
A project apply or a rewind that restores a project also writes the project file at once.
But the server makes the command save before it replies, also to an exact retry.
Thus a crash can leave the difference only before the reply, or after a command save that failed or took more time.
In the second case, the reply has `stateSaved` set to `false`.
When the new project differs from the saved project only in its demand settings, the restore keeps the simulation from before the command.
After the restore, the server applies the demand settings of the project file as a demand change does, and the project revision increases by one.
While the restored traffic demo runs, a demand change is not possible.
Then the saved state gets `project_changed`.
Without `-project`, the server restores the saved project.
When the server does not use the saved state, the new session uses the project file.
Without `-project`, it uses the saved project if the server can decode the file and the project is valid.
Otherwise it uses the example project.
After `restore_loop`, a server without `-project` uses the example project, also when the saved project is valid.

If the read fails or takes more than 30 seconds, the server starts an empty session with reason `unreadable` and does not save.
The file stays for the next start.
If the move of a rejected file fails, the server also does not save.
In both cases, the server logs an error.
Correct the fault, then restart the server.

The startup save records the number of restores since the last periodic or final save.
If the server stops without a periodic or final save after a restore, the next start uses only the `logical` tier, with reason `restore_loop`.
After two such stops, the next start moves the file aside with reason `restore_loop`.
This stops a crash loop that a saved state causes.
A stop for another cause also counts, for example a `SIGKILL` before the first periodic save, about 60 seconds after the start.
When startup fails after the startup save, for example because a listen address is in use, the server makes a final save before it stops.
Thus a failed startup does not count as a restore.

The server keeps the saved epoch only after a final save and a `physical` or `logical` restore.
The saved state must also have fewer than 1,024 clients.
See [server restarts](protocol.md#server-restarts) for the effect on clients.

Only one server can use a state location, which is one directory and prefix.
Two servers write over the file of each other, and the startup of one server can delete a temporary file of the other.
On Kubernetes, use one replica and the `Recreate` strategy.
A rolling update runs the old server and the new server at the same time.

To use a rejected or previous file:

1. Stop the server.
2. If you need the current state, copy `session.json.gz` to another name.
3. Rename the rejected or previous file to `session.json.gz`.
4. Start the server.

A file with another format version needs a server with that format version.
A file that the server rejected with reason `restore_loop` gets the same reason again.

A previous or copied file can come from a final save, and the server can then keep its epoch.
If the server used that epoch after the save, the revision can be lower than the last revision that clients got.
The Go client of the simulation view accepts these state frames because the server start ID changed.
The server rejects a project apply from an editor page that loaded the project before the restart, also when the epoch and the project revision are the same.
Save point IDs and order IDs can also repeat.
Reload open browser pages and restart desktop clients after you use such a file.

To start a new session:

1. Stop the server.
2. Delete `session.json.gz`.
3. Start the server.

## Faults

Faults are on in each session, except during the traffic demo.
A project can set the optional `faults` member, for example:

```json
"faults": {"evacuationSeconds": 300}
```

Each member of `faults` is optional, and an absent `faults` member gives the same values as `{}`:

| Member | Value | Default |
| --- | --- | --- |
| `evacuationSeconds` | An integer from 0 to 3,600. | 300 |
| `perHour` | A number from 0 to 60. A rate above 0 needs a `duration`. | 0 |
| `debrisShare` | A number from 0 to 1. | 0 |
| `debrisMeters` | A number from 0.5 to 50. | 2 |
| `duration` | An object with `kind` and the values of that kind. | None |

`evacuationSeconds` is the time from the start of a pod fault to the evacuation of its riders.
The riders leave the pod only when it is at rest, and each evacuated order ends interrupted.
The other members set the scenario fault rate.
`perHour` is the mean number of attempts to start a scenario fault in each simulated hour.
An attempt that cannot start a fault is skipped, so the real rate can be lower.
At a rate of 0, only the `fault` command starts a fault.
`debrisShare` is the share of scenario faults that are debris on a lane, and `debrisMeters` is the length of that debris.
The `duration` kind is `fixed` with `seconds`, `uniform` with `minSeconds` and `maxSeconds`, or `exponential` with `minSeconds`, `maxSeconds`, and `meanSeconds`.
Each value is an integer from 1 to 86,400 seconds.
`minSeconds` must be at most `maxSeconds`, and `meanSeconds` must be from `minSeconds` to `maxSeconds`.
A member of another kind is refused.

Validation also refuses a null value at any level of `faults`.
A project with `incidentContract`, `faultContract`, or `emergencyContract` has an unknown member.

The size limit of 32 MiB counts `faults` at its widest value, for each project.
The **Faults and emergencies** section of the editor sets these members, and a field that you do not change stays absent.
`cmd/compare` runs a project with a fault rate above 0, and it reports the [incident columns](reference.md#report-columns).
The car runs of `internal/parkride` refuse a fault rate above 0.
With no rate, both tools accept other `faults` settings, and they run with faults off, because they send no fault command.
A change to `faults` is a project change.
A project apply then replaces the fleet, and every fault ends.
With `-project`, a saved state with other fault settings gets `project_changed`.

The `fault` and `clearFault` commands start and end faults (see [faults](protocol.md#faults)).
The pod inspector has a **Fault** button for a pod fault.
The [**Incidents** panel](user-guide.md#incidents-panel) has **Place debris**, which sends the `fault` command, and a **Clear** button for each fault.
The map draws each active debris fault.
A reset ends every fault and sets the fault counters to 0, and it keeps faults on.
A save point keeps the faults and the counters, and a rewind restores them.

The traffic demo runs without faults.
The frames have `operationsOff`, the view does not show the button or the **Incidents** panel, no sampler runs, and a fault command gets `faults are not enabled`.
Faults stay off after the demo until a reset or a project apply.
A restore of the demo fleet also keeps faults off.

The fault counters are in the `faults` member of the frames and the saved state.
The [incident gauges](#opentelemetry) report some of them.

## Emergencies

Emergencies are on in each session, except during the traffic demo.
A project can set the optional `emergencies` member, for example:

```json
"emergencies": {"perHour": 0}
```

`emergencies` has one optional member, and an absent `emergencies` member gives the same value as `{}`:

| Member | Value | Default |
| --- | --- | --- |
| `perHour` | A number from 0 to 60. | 0 |

`perHour` is the mean number of attempts to start a scenario emergency in each simulated hour.
An attempt that cannot start an emergency is skipped, so the real rate can be lower.
At a rate of 0, only the `emergency` command starts an emergency.

Validation also refuses a null value or an unknown member in `emergencies`.

The size limit of 32 MiB counts `emergencies` at its widest value, for each project.
The **Faults and emergencies** section of the editor sets this member, and a field that you do not change stays absent.
`cmd/compare` runs a project with an emergency rate above 0, and it reports the [incident columns](reference.md#report-columns).
The car runs of `internal/parkride` refuse an emergency rate above 0.
With no rate, both tools accept `emergencies`, and they run with emergencies off, because they send no emergency command.
A change to `emergencies` is a project change.
A project apply then replaces the fleet, and every emergency ends.
With `-project`, a saved state with other emergency settings gets `project_changed`.

The `emergency` command starts an emergency (see [emergencies](protocol.md#emergencies)).
The pod inspector has an **Emergency** button for a pod that carries passengers and has no emergency.
The button sends the command without `orderID`, so the party is the first party aboard the pod.
No command cancels one emergency.
A reset ends every emergency and sets the emergency counters to 0, and it keeps emergencies on.
A save point keeps the emergencies and the counters, and a rewind restores them.

The traffic demo runs without emergencies.
The frames have `operationsOff`, the view does not show the button or the **Incidents** panel, no sampler runs, and the `emergency` command gets `emergencies are not enabled`.
Emergencies stay off after the demo until a reset or a project apply.
A restore of the demo fleet also keeps emergencies off.

The emergency counters are in the `emergencies` member of the frames and the saved state.
The [incident gauges](#opentelemetry) report some of them.
An order that an emergency unload interrupts counts in `podsim.orders.interrupted`.

## Memory limit

The server sets the Go memory limit to 90 percent of the detected cgroup limit.
Set `GOMEMLIMIT` to use an explicit Go memory limit.
Set `AUTOMEMLIMIT` to change the ratio or disable automatic detection.

These historical measurements cover LondonCentral.
Save points keep copies of the simulation in memory.
A LondonCentral save point uses about 6.2 MB after the live simulation continues from it.
The server keeps at most 8 save points, so they use about 50 MB with one LondonCentral project.
A save point that holds a replaced LondonCentral project uses about 4.6 MB more.
If each of the 8 save points holds its own LondonCentral project, they use about 85 MB.

With `-state`, each save of the session state allocates memory for a short time.
A LondonCentral save with 20 orders per minute, after 15 simulated minutes, allocates about 8.5 MB.
About 8 MB of this is JSON work on the 1.5 MiB project.
The encoder checks and formats the project text again when it adds the project to the file.
The compressed file is about 320 KB.
A project near the 32 MiB file limit needs more memory.

Each project apply also allocates memory for a short time.
The server decompresses, checks, and decodes the command, then validates and starts the project.
An apply of a project with 7.2 MiB of JSON allocated about 190 MB and took about 0.55 s.

A [preset generation](protocol.md#presets) also uses memory for a short time.
The server builds a new project for each request and does not keep it.
The Go runtime returns the memory to the system in its own time after the reply.
These values are from one server that runs the default project, with 20 MiB of resident memory at idle:

| Request | JSON | Time | Peak resident memory |
| --- | --- | --- | --- |
| `london-central` | 1.2 MB | 0.4 to 0.6 s | 35 to 43 MiB |
| `london-full` | 5.6 MB | 2.2 to 3.7 s | 89 to 101 MiB |
| `project` apply of `london-full` | 5.6 MB | 1.7 s | 120 MiB |

On a quiet session, about 58 MiB of anonymous memory stayed resident for at least 60 s after a `london-full` reply, because no collection ran.
The source data adds 7.2 MB to the server binary, and it does not change the resident memory at idle.
`GOMAXPROCS=1` and `GOMEMLIMIT=128MiB` did not change the peak or the time of a generation on the test host.
With `GOMAXPROCS=1` and the default project, the simulation clock stayed at the then 60 ticks per second during a generation of `london-full`.
This was not measured with a London project running.
The server does one generation at a time, and one large command or generation at a time, so these peaks do not add.
An apply of a gzip project with 31 MiB of JSON, 12,000 nodes, 600 stations, and 359,400 flows allocated about 7.1 GiB in total and took about 9.8 s.
At its peak, the heap was about 310 MiB larger.
The server applies one gzip command or one plain command of more than 1 MiB at a time, and other such commands wait.
At most 4 such command bodies of 16 MiB or less are in memory, so they use at most 64 MiB, and the server replies 503 to more.
Thus two editors that apply large projects at the same time do not double this memory.

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

OpenTelemetry stays off until you set an OTLP endpoint.
The server uses OTLP over HTTP for traces and metrics.
Use the standard OpenTelemetry environment variables:

```sh
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318 ./podsim-server
```

Use `OTEL_TRACES_EXPORTER=none` or `OTEL_METRICS_EXPORTER=none` to disable one signal.
Set `OTEL_SDK_DISABLED=true` to disable both.
Use `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` and `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` when traces and metrics use different collectors.
Without `OTEL_EXPORTER_OTLP_ENDPOINT`, the server exports only the signals that have their own endpoint.
The default service name is `podsim`.
`OTEL_SERVICE_NAME` replaces it.
`OTEL_RESOURCE_ATTRIBUTES` can add deployment identity.

HTTP telemetry excludes the diagnostic `/api/state` endpoint and `/healthz`.
The simulation view receives shared WebSocket publications from `/api/state/stream`.
Other HTTP requests include route-based server traces and metrics.

Runtime metrics report memory, allocations, goroutines, processor limits, and the Go memory limit.
Session gauges report the simulation tick, journeys, pods, stopped pods, distance, pickup wait, and save points.
`podsim.orders.interrupted` counts the orders that ended interrupted, without a completion.
`podsim.pod.active` counts the pods with assigned work: a trip that is not complete, or a pickup that a pending request names.
An empty move to parking or for redistribution is not work, and a pod that finished its trip is not active.
The `podsim.stream.*` gauges expose connections, full and delta publication counts, compressed bytes, retained bytes, and history messages.
`history_first` and `history_last` report the retained sequence span.
`outstanding_messages`, `outstanding_bytes`, and `ack_age_ms` report aggregate delivery credit and the oldest pending acknowledgment age.
`resync_history` and `resync_source` count the fixed resynchronization reasons.
These metrics have no client labels.
A stream admission failure returns HTTP 503 with `Retry-After`.
Clients reconnect with backoff and do not switch to polling.

Stream pressure recovery reports aggregate `podsim.stream.pressure_sheds` and `podsim.stream.encoding_bytes` metrics.
The first counts canceled writer leases.
The second reports the single compressed encoding waiting for admission.

`podsim.checkpoint.retained` is the number of save points in memory.
Compare it with the runtime memory metrics to see the memory that save points use.
A reset, a demo, a project apply, or a rewind can decrease `podsim.simulation.tick`, `podsim.journey.submitted`, `podsim.journey.completed`, `podsim.orders.interrupted`, `podsim.travel.passenger.distance`, `podsim.travel.empty.distance`, and the `podsim.incident.*` gauges.
These metrics are gauges, not counters, so do not use `rate()` on them.

The `podsim.incident.fault.*` and `podsim.incident.emergency.*` gauges report incidents.
`active` is the number of active records, and `started` counts the sampled and manual records.
`sampled` counts the sampler events that came due, and `skipped` counts those with no start.
`podsim.incident.fault.delayed_rider_ticks` is the sum over ticks of the active riders aboard healthy pods with an incident wait.
`sampled`, `skipped`, and `delayed_rider_ticks` stay 0 while no sampler of their kind is live, so a manual fault at a rate of 0 moves only `active` and `started`.

With `-state`, the server also reports the saves of the session state.
Without `-state`, these metrics do not exist.
`podsim.state.saves` is a counter of the saves by `result`, `ok` or `error`.
`podsim.state.size` is the compressed size in bytes of the last good save.
It has no value before the first good save.
`podsim.state.enabled` is 1 while the server saves the session state, and 0 when saving is off.

`podsim.state.unsaved` is the time in seconds since the last good save.
It is 0 when the session did not change after that save.
Before the first good save, it counts from the server start.
While the clock runs, it rises to about 60 seconds between two periodic saves.

Alert when `podsim.state.unsaved` is more than 180 seconds.
This is three periodic save intervals without a good save.
Alert when `podsim.state.enabled` is 0.
The server turns saving off at startup when it cannot read the saved state or cannot move it aside.
A restart then loses the changes after the start.
The `Read saved session state` or `Move rejected session state` log record gives the cause.

The state store uses the gocloud.dev blob package, which also sends spans and metrics through the same providers.
The server keeps them on.
The spans have names such as `gocloud.dev/blob.NewWriter`.
The metrics are `gocloud.dev/blob/latency` in milliseconds, and `gocloud.dev/blob/bytes_read` and `gocloud.dev/blob/bytes_written` in bytes.

The server flushes both providers during graceful shutdown.
An endpoint that is not a valid URL does not stop startup.
The exporter logs a `parse url` error and ignores that value.
Without another valid endpoint, it sends to the default endpoint, `https://localhost:4318`.
An `OTEL_RESOURCE_ATTRIBUTES` item without a value stops startup with an error.
An unreachable collector reports export errors without stopping the simulation.

## Save point logs

The server writes one `INFO` log record for each save point and rewind that it applies.
It does not log rejected commands, exact retries, or commands after shutdown starts.

`Saved checkpoint` gives the `client`, the `checkpoint` ID, the `tick`, and the `projectRevision`.
It also gives the number of `retained` save points.
`evicted` is the ID of the save point that the server removed at the limit, or 0.

`Rewound session` gives the `client`, the `checkpoint` ID, `fromTick`, and `toTick`.
It also gives the `generation` and `projectRevision` after the rewind.
All browsers share one session, so `client` identifies the browser page that rewound the session for all users.
`projectRestored` is true when the save point holds a different project or demand configuration and the rewind restored it.
With `-project`, the rewind also writes that project to the project file.

Both records give `duration`, the time to apply the command under the session lock.

## Session state logs

The server writes only log records at level INFO or higher.
Thus it does not write the DEBUG records in this section.

With `-state`, the server writes these log records at startup:

- `Opened session state store` (INFO) gives the redacted `url` and the `location`, the path in front of each file name.
- `Removed temporary state files` (INFO) gives the `count` of deleted temporary files.
  `Remove temporary state files` (WARN) gives the `error` when the list or a delete fails.
  The list skips a directory that the server cannot read, and gives no error for it.
  Startup continues.
- `No saved session state` (INFO) means that the location has no `session.json.gz`.
  The server starts a new session.
- `Restored session` (INFO) gives the `tier`, the `reason`, and the counts `demoted`, `requeued`, `dropped`, `unaccounted`, `droppedParties`, `overCap`, and `overBudget`.
  When the `logical` tier ended faults, it also gives their number in `droppedFaults`.
  When it ended emergencies, it also gives their number in `droppedEmergencies`.
  The `restore` object of the state frame does not have these counts.
  It also gives the saved `tick`, `epochKept`, `final`, `savedAt`, `savedBuild`, the current `build`, and `restoreAttempts`.
  `overCap` counts the saved routes that were longer than their limit.
  `overBudget` counts the routes that did not fit in the budget of track cells.
  `bytes` is the compressed size.
  `duration` is the time from the read to the end of the startup save.
  After a failed `physical` tier, `physicalError` tells why it failed.
- `Saved session state has unaccounted orders` (WARN) follows `Restored session` when the saved state submitted orders that it did not hold.
  It gives their number in `unaccounted`.
  The server does not make up these orders, so each later restore gives this record again.
- `Applied demand settings of the project file` (INFO) means that the restore used the demand settings of the `-project` file in place of the saved settings.
  It gives the `savedDemand` and the `demand` settings.
- `Demoted pod` (DEBUG) gives each `pod` that the `physical` tier moved to a berth.
- `Rejected saved session state` (WARN) gives the `reason` and the `error`.
- `Restore failed with a panic` (ERROR) gives the `panic` and the `stack`.
  The server then rejects the file with reason `invalid_state`.
- `Preserved saved session state` (ERROR) means that the decompressed saved state has more than 128 MiB.
  It gives the `error` and `saving=false`.
  The server keeps the file and fails to start.
- `Read saved session state` (ERROR) means that the read failed or timed out.
  It gives the `error` and `saving=false`.
- `Move rejected session state` (ERROR) means that the move of a rejected file failed.
  It gives the `error` and `saving=false`.
- `Prune rejected session state` (WARN) gives the `error` when the server cannot delete an old rejected file.
  The next rejection tries again.
- `Backed up saved session state` (INFO) and `Back up saved session state` (WARN, with the `error`) tell the result of the copy to `session.previous.json.gz`.
  A failed copy does not stop the restore.
- `Stop during startup` (INFO) means that a signal came while the server read or restored the saved state, or made the startup save.
  It gives the `cause` and the `error`.
  The server stops without serving and without a save.

It writes these records for each save:

- `Saved session state` (DEBUG) is a startup, periodic, or command save.
  It gives the `kind`, the compressed size in `bytes`, the `revision`, and the `tick`.
  It also gives three durations: `lock` to copy the state under the session lock, `encode`, and `write`.
- `Saved final session state` (INFO) is the final save, with the same attributes.
  A startup that fails after the startup save also makes a final save.
- `Save session state` (WARN) is a failed save.
  It gives the `kind`, the `error`, the `cause` of a timeout, and `failures`, the number of failed saves in sequence.
  The cause of a command save that took more than 2 seconds is `save of the session state before a command reply timed out`.
  A state that is too large gives ERROR, because each later save also fails.

It writes these records at shutdown:

- `State saver did not stop` (WARN) means that the saver did not stop in 1 second.
  It gives the `timeout`.
- `Skipped final save` (WARN) gives the `reason`.
  `clock` means that the clock did not stop, `saver` means that the saver did not stop, and `off` means that saving is off.
- `Final save failed` (WARN) means that the final save failed or did not return in 6 seconds.
  It gives the `error` and the `cause`.
  The cause of a timeout is `final save of the session state timed out`.
  When the store returned an error, the session also logs `Save session state`.
- `Close session state store` (WARN) gives the `error` of the close of the store.

## Graceful shutdown

The server starts a graceful shutdown when it gets `SIGINT` or `SIGTERM`, or when a listener fails.
It logs `Stop accepting commands` with the cause.
Then it stops the simulation clock and rejects new commands with HTTP 409 and the `server_stopping` error code.
A command that is already in progress completes.
An exact retry of the last command from a client still gets the stored acknowledgment.
The server ignores a second `SIGINT` or `SIGTERM` during the shutdown.

Then the server closes its listeners and does not accept new requests.
Requests that are already in progress, including reads, get up to 5 seconds to complete.
Next, the server waits up to 5 seconds for the clock goroutine to return.
If it does not return, the server logs `Simulation clock did not stop` as a warning.

With `-state`, the server then stops the state saver and waits up to 1 second for it.
A periodic save in progress stops, and the old file stays.
If the clock and the saver stopped, the server saves the session state a last time.
This final save gets 5 seconds.
The server waits up to 6 seconds for it, because a blocked file system call can continue after the timeout.
A final save that fails or times out keeps the old file, except after a failed sync of the directory.
If the clock did not stop, the state can still change.
If the saver did not stop, its write can still use the store.
In both cases, the server does not make a final save.
Without a final save, the next start restores the last saved state with a new epoch.

Last, the server flushes telemetry for up to 5 seconds.

With `-state`, a shutdown can take up to about 22 seconds.
This is 5 seconds for the requests, 5 for the clock, 1 for the saver, 6 for the final save, and 5 for telemetry.
`docker stop` waits only 10 seconds by default, then it kills the process.
Use `docker stop -t 30`, or `--stop-timeout 30` with `docker run`.
On Kubernetes, keep `terminationGracePeriodSeconds` at 30 or more.

## Command receipts

The server deduplicates command retries by client and sequence.
The server keeps command receipts for at most 1,024 clients per session.
Each simulation view, editor, and desktop client that sends a command is one client.
A page reload makes new clients.
If a new page reports the session client limit, restart the server.

If the server made its final save at a graceful shutdown and restores the session with the `physical` or `logical` tier, the session continues.
Then a retry of a command from before the restart gets the `expired_command` error, and the server does not apply the command again.
The clients from before the restart also stay in the limit of 1,024 clients.

The session does not continue after a restart at the client limit or after other restarts.
Then the retry gets the `session_changed` error, and the count starts again.
See [server restarts](protocol.md#server-restarts).

## Development

### Code layout

| Path | Purpose |
| --- | --- |
| `internal/sim` | Network, routing, requests, pod movement, state export and restore, and deterministic tests. |
| `internal/observe` | Station berth and queue metrics for the view and comparison reports. |
| `internal/rail` | Pod transfers to scheduled train departures. |
| `internal/parkride` | Offline car journeys around native pod trips. |
| `internal/jsonerr`, `internal/wirename` | Fixed wording for JSON errors, and the JSON member names of the wire and file formats. |
| `internal/project` | Versioned scenario settings, validation, and detached copies. |
| `internal/editormodel` | Go editor worker state, checks, edits, normalization, undo history, daily profile construction, place navigation, and the observation and measurement panels. |
| `internal/scenarios` | Deterministic scenario presets, including `scale100`, `london-central`, and `london-full`, and qualification tests. `cmd/serve` links it for the editor presets, and the WASM build must not link it. |
| `internal/session` | Shared clock, command validation, save points, saved session state, HTTP API, and repeatable demand. |
| `internal/statestore` | Saved session state in a `file://` blob bucket, for the server only. |
| `internal/remote` | Shared state streaming, motion buffering, command retries, and connection state. |
| `internal/view` | Ebitengine rendering and input against copied snapshots. |
| `internal/telemetry` | Optional OTLP traces, HTTP metrics, runtime metrics, session gauges, and session state metrics. |
| `internal/cmd/buildweb` | Generated browser files and gzip WASM file. |
| `cmd/podsim` | Desktop and WASM entry point. |
| `cmd/editormodel` | Separate WASM entry point for the editor worker. |
| `cmd/serve` | Shared session, saved session state, browser assets, health checks, and diagnostics. |
| `cmd/compare` | Reproducible policy comparisons. |
| `cmd/scenario` | Generated scenario files. |
| `cmd/projectcheck` | Validation of a local project file. See [project check](project-check.md). |
| `cmd/parkride` | Finite car itineraries around native pod journeys. |
| `web` | Browser page, game loader, and scenario editor. |

### Tasks

```sh
mise run test
mise run check
```

| Task | What it runs |
| --- | --- |
| `mise run check` | Workflow validation, Markdown checks, race tests, the tests that skip under the race detector, the `test:web` tests, vet, lint, vulnerability checks, the native and browser builds, and the embedded server tests. It runs `check:static`, `qualify`, and the three race tasks one after another, and stops at the first failure. |
| `mise run format` | Formats the Go sources and the Markdown files. |
| `mise run test:quick` | `go test -short ./...`. It skips the long tests. The race tasks, `qualify`, and `test:embedded` run them. |
| `mise run lint` | actionlint, rumdl, `go vet`, golangci-lint, and the check that `podsim.wasm` does not link cloud storage. |
| `mise run check:static` | The checks other than the race tests and `qualify`: `test:web`, `lint`, `vulncheck`, `build`, and `test:embedded`. |
| `mise run test:race` | The `test:race:sim`, `test:race:session`, and `test:race:other` tasks. |
| `mise run test:web` | Only the editor, loader, and page tests. |
| `mise run qualify` | The two Station 19 drain tests in `internal/scenarios`, the two emergency choice tests and `TestSampledTickLatency` in `internal/sim`, and the CI incident soak in `internal/session`, with the full check at each tick, without the race detector. These tests skip under the race detector, so in `mise run check` only this task runs them. The `test:race` task runs the other tests of these packages. |
| `mise run qualify:limits` | `TestSampledTickNoCandidateLatency`, on request. It reports the time at the project limits and gates nothing. |
| `mise run soak` | The incident soak in the routine branch shape. See "Incident soak". |
| `mise run soak:full` | The soak of amendment A6 with the full check at each tick. The limit of 2 hours can cut it, and it lists the seeds that finished. See "Incident soak". |
| `mise run test:embedded` | The root and `cmd/serve` tests with the `embed_assets` tag. It also runs the session tests that skip under the race detector or check more without it. The `test:race` task runs the other session tests. |
| `mise run benchmark` | 6,000 simulation steps on the 100-pod ring fixture, not on the current `scale100` mesh. |

`mise.toml` tracks Go 1.27, rumdl 0.3, and major versions for the other development tools.
`mise.lock` records the resolved tool downloads.

Some editor tests use Go to generate the `scale100` and `london-central` projects.
If Go is not on `PATH`, `node --test web/editor_test.cjs` skips these tests and gives the reason.
The `test:web` task sets `PODSIM_REQUIRE_GO=1`, so a missing Go makes the task and `mise run check` fail.

### Incident soak

`mise run soak` runs `TestIncidentSoak` on `rail-hub` and `london-central`.
Each arm has one warm save, and each branch restores it in a new session and reseeds the driver, the fault and emergency samplers, and demand.
The new streams derive from the seed and the branch, so the same selection gives the same run.
A failure stops only its branch, and the log line of each branch has its Progress and rule A numbers.
In that line, `demoted` counts the saves that fail the `physical` tier because a pod with a fault or an emergency would move to a berth, and `podsDemoted` counts the pods that a `physical` restore moves to a berth.
Any other failure of the `physical` tier stops the branch.
A second log line of each branch gives the rule A evidence: the count of readings on each path of the classifier, and the 5 records with the most blocked time, with their chain ends.
It changes no gate.
The `incidents` and `emergencies` arms run on both presets, and `platoons` runs on `rail-hub`.
Amendment A14 adds `reference-low-demand` and `incidents-low-demand` to the routine soak on `rail-hub` only.
Both use 1 order/min, including the 30-minute warm time.
The 10 incident branches each run for 1.5 simulated hours, then keep the 30-minute Progress window against their own reference range.
Select both names through `PODSIM_SOAK_ARMS` to run only this group with its queue check.
The checks run at the stride of amendment A10: the full check runs at each simulated second and at each event.
Use `mise run soak:full` before a release tag.
After a change to dispatch, routing, or the fault and emergency state machines, use `mise run soak` and `mise run qualify`, which runs `TestIncidentSoakCI`.
The CI soak runs one seed for one simulated hour on `rail-hub`, with the full check at each tick.
The `soak:full` task runs 5 seeds of 6 simulated hours with the full check at each tick.
This run keeps the wall limit of 2 hours, so it can end cut, before each seed finishes.
A run that the limit stops counts only its completed seeds.
The task writes each finished seed to the file of `PODSIM_SOAK_FINISHED`, with its failure state, and it prints that file at the end, also after a cut.

The tasks set a memory cap of 16 GB of address space.
They hold a lock on `/tmp/podsim-soak.lock`, so one host runs one soak at a time.
A second soak waits for 60 seconds, and then it fails with exit code 75.
The wall limit is 2 hours, and a run that the limit stops counts only its completed branches.
`docs/studies/incident-soak.md` has the estimate of the wall time.

These variables change a run:

| Variable | Meaning | Default |
| --- | --- | --- |
| `PODSIM_SOAK_PRESETS` | The presets, with commas. `rail-hub-express` and `rail-hub-express-parties` select the Express projects that the test builds. | `rail-hub,london-central` |
| `PODSIM_SOAK_ARMS` | The arms, with commas. A branch of an arm reads the queue range of a reference arm only when the list holds it. | The arms of the shape and the preset |
| `PODSIM_SOAK_SHAPE` | `branches`, or `seeds` for the shape of amendment A6. | `branches` |
| `PODSIM_SOAK_EVERY_TICK` | `1` runs the full check at each tick. | Off |
| `PODSIM_SOAK_SEEDS` and `PODSIM_SOAK_FIRST_SEED` | The seeds. Each seed has its own warm save. | 1 seed in `branches`, 5 in `seeds`, from seed 1 |
| `PODSIM_SOAK_BRANCHES` | The branches of each arm. | 10 on `rail-hub`, 3 on `london-central`, 1 in `seeds` |
| `PODSIM_SOAK_HOURS` | The length of a branch before the clear. | 1.5 on `rail-hub`, 1 on `london-central`, 6 in `seeds` |
| `PODSIM_SOAK_PARALLEL` | The number of runs at one time. | 3 |
| `PODSIM_SOAK_LIMIT` | The wall limit in seconds. | 7200 |
| `PODSIM_SOAK_LOCK` | The lock file. | `/tmp/podsim-soak.lock` |
| `PODSIM_SOAK_FINISHED` | The file that gets one line for each finished seed. The test adds to it and does not clear it. | Not set, or a new file for each run of `mise run soak:full`, which the task prints |

`PODSIM_SOAK_CHECKS`, `PODSIM_SOAK_SETTLE_MINUTES`, and `PODSIM_SOAK_CHECK_FROM_HOURS` are for timing runs only.

### Continuous integration

GitHub Actions runs the same check on pull requests, pushes to `main`, and version tags.
The workflow also supports a manual trigger.
It splits the check into five parallel jobs: `test:race:sim`, `test:race:session`, `test:race:other`, `qualify`, and `check:static`.
After all five pass, a version tag or a manual run publishes the container image.
A push to `main` runs the checks and publishes no image.
New pull-request updates cancel older runs.
Each `main` push keeps its own run.
The workflow uses major-version action tags and installs tools from `mise.lock`.
Go module, build, and lint analysis caches use keys for each job and task, and refresh after successful runs.

### Lint and package boundaries

The lint configuration adds resource, context, and security linters to the standard golangci-lint set.
It also checks these package boundaries:

- `internal/sim` can import only the standard library.
- `internal/session`, `internal/project`, and `cmd/serve` cannot import the renderer or browser APIs.
- Packages in the browser build cannot import cloud storage packages or `internal/statestore`.
  Lint also lists every package that the WASM build links, and fails if the list has one of these packages or `internal/scenarios`.

rumdl checks the Markdown files with the settings in `.rumdl.toml`.
Each sentence has its own source line, so a prose change does not reflow the lines around it.
Run `mise run format` to fix the layout.

### Test coverage

Validation has six main parts:

- Core tests cover routing, journeys, invalid requests, pause and reset behavior, repeatability, and state isolation.
- Session and server tests cover save points, exact rewind, project restore, saved session state, save point and session state logs, and graceful shutdown.
- Rendering tests cover buffered movement, lane corners, station movement, pause and reset behavior, and stale snapshots.
- HTTP tests cover compression, topology and frame decoding, command acknowledgments, WASM responses, byte ranges, health, and diagnostics.
- Traffic tests cover separation, merge contention, berth capacity, stopped queues, through traffic, and eventual progress.
- Browser checks confirm visible movement and working controls.
  A WASM build alone is not sufficient.
