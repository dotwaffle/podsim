# Distribution and operations

## Embedded server build

Generate all browser files before an embedded build:

```sh
go generate ./...
go build -trimpath -tags=embed_assets -o podsim-server ./cmd/serve
```

The executable contains the HTML, CSS, JavaScript, and gzip WASM files.
It does not contain an uncompressed WASM module.
This keeps the executable at about 35 MB, not 64 MB.
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

The container workflow publishes `ghcr.io/dotwaffle/podsim` from `main`, version tags, and manual workflow runs.
It builds `linux/amd64` and `linux/arm64` images with ko.
Each image gets the commit SHA as a tag.
Images from `main` also get the `latest` tag.
Images from a version tag also get the name of that tag.
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
For each of these changes, the server replaces the file with compact JSON of 10 MiB or less.
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
It does not establish deployment readiness or grant deployment approval.

## Session state

The `-state` option keeps the shared session across server restarts.
Its value is a bucket URL.
This server build has only the `file://` driver.
Without `-state`, the server does not save or read a session state.
The saved state holds the last command sequence of each client, but not the save points or the command receipts.
A restart clears those.

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
  A pod in a [virtual platoon](../README.md#virtual-platoons) keeps its link to the pod ahead, with the same run of lanes, turn, and clearance.
  The tier checks each saved link against the network and the other pods, and a link that is not valid fails the tier.
  The file does not keep the platoon limit, so the restore uses the `platoonLimit` of the project.
  The file keeps the shared ride mode.
  A file without the mode restores in the default `drop-offs` mode.
- `logical`: The server uses this tier with reason `physical_failed` when the `physical` tier fails.
  It also uses it with reason `restore_loop`, as described below.
  The pods start again at their initial berths.
  Parties that were unloading at their stop count as completed, if the pod is at a berth of a passenger station in the network.
  Each other party in a pod goes back to the queue as one order.
- `empty`: The server does not use the saved state and starts a new session.
  Except after a read failure, it moves `session.json.gz` to a rejected file.

Ordinary sessions write saved-state version 2.
An experimental station buffer session writes version 3 while admissions are enabled or buffer members remain.
This server accepts both versions.
Version 3 adds the optional pod field `stationBuffered` and permits validated berthless occupancy of a station holding lane.
Restore keeps those members draining but does not enable new buffer admissions.
Version 2 rejects the field, including an explicit `false` value.
The writer returns to version 2 after buffers are disabled and all members drain.
An older server rejects version 3 with `unsupported_version` and moves the file aside.
Keep a copy before a downgrade.
See the [station buffer contract](station-buffer-state-proposal.md) for the experimental limits.
Project files, commands, and WebSocket frames retain their existing formats.

The reason for an `empty` start is `project_changed`, `unsupported_version`, `invalid_state`, `too_large`, `restore_loop`, or `unreadable`.
`too_large` means more than 80 MiB, compressed or decompressed.
A file with another format version gets `unsupported_version`.
Until the first release, an added optional member with a safe zero value keeps the format version.
The file leaves out the member when its value is zero.
An older server restores a file without the member, but it gets `invalid_state` for a file with the member and moves that file aside.
Each other change to the members of the file gets a new format version.
Thus after a downgrade past such a change, the older server moves the file aside.
With `-project`, the project file has priority, and a saved state with a different project gets `project_changed`.
But when only the demand settings are different, the server restores the saved state.
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
A project near the 10 MiB file limit needs more memory.

Each project apply also allocates memory for a short time.
The server decompresses, checks, and decodes the command, then validates and starts the project.
An apply of a project with 7.2 MiB of JSON allocated about 190 MB and took about 0.55 s.
The server applies one gzip command or one plain command of more than 1 MiB at a time, and other such commands wait.
At most 4 such command bodies of 4 MiB or less are in memory, so they use at most 16 MiB, and the server replies 503 to more.
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

HTTP telemetry excludes the diagnostic `/api/state` endpoint.
The simulation view receives shared WebSocket publications from `/api/state/stream`.
It also excludes `/healthz`.
Other HTTP requests include route-based server traces and metrics.

Runtime metrics report memory, allocations, goroutines, processor limits, and the Go memory limit.
Session gauges report the simulation tick, journeys, pods, stopped pods, distance, pickup wait, and save points.
`podsim.pod.active` counts the pods with assigned work: a trip that is not complete, or a pickup that a pending request names.
An empty move to parking or for redistribution is not work, and a pod that finished its trip is not active.
The `podsim.stream.*` gauges expose connections, full and delta publication counts, compressed bytes, retained bytes, and history messages.
`history_first` and `history_last` report the retained sequence span.
`outstanding_messages`, `outstanding_bytes`, and `ack_age_ms` report aggregate delivery credit and the oldest pending acknowledgment age.
`resync_history` and `resync_source` count the fixed resynchronization reasons.
These metrics have no client labels.
A stream admission failure returns HTTP 503 with `Retry-After`.
Clients reconnect with backoff and do not switch to polling.

`podsim.checkpoint.retained` is the number of save points in memory.
Compare it with the runtime memory metrics to see the memory that save points use.
A reset, a demo, a project apply, or a rewind can decrease `podsim.simulation.tick`, `podsim.journey.submitted`, `podsim.journey.completed`, `podsim.travel.passenger.distance`, and `podsim.travel.empty.distance`.
These metrics are gauges, not counters, so do not use `rate()` on them.

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

Stream pressure recovery reports aggregate `podsim.stream.pressure_sheds` and `podsim.stream.encoding_bytes` metrics.
The first counts canceled writer leases.
The second reports the single compressed encoding waiting for admission.
