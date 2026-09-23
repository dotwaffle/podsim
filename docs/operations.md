# Distribution and operations

## Embedded server build

Generate all browser files before an embedded build:

```sh
go generate ./...
go build -trimpath -tags=embed_assets -o podsim-server ./cmd/serve
```

The executable contains the HTML, CSS, JavaScript, raw WASM, and gzip WASM files.
It does not need a `dist` directory at runtime.
The `-dir` option overrides embedded files for development.
Production builds keep Go and WASM debug information.

The server sends browser files with `Cache-Control: no-cache` and API responses with `Cache-Control: no-store`.
A browser must check a cached file with the server before it uses the file, so a reload after an upgrade loads the new files.
Files from `-dir` have a modification time, and the server answers `304 Not Modified` when a file did not change.
Embedded files have no modification time, so browsers download them again on each load.

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
When the ID changes after a server restart, open browser pages reload and get the new browser files.
If the server cannot read the browser files, it logs `Browser build ID unavailable` as a warning and uses a random ID.
Then open pages reload after each server restart.

## Container image

The container workflow publishes `ghcr.io/dotwaffle/podsim` from `main`,
version tags, and manual workflow runs.
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
For each of these changes, the server replaces the file with compact JSON of at most 4 MiB.

The `-state` option needs a directory that UID 65532 can write.
Make the directory on the host, then mount it in the container:

```sh
sudo install -d -o 65532 -g 65532 -m 0700 /srv/podsim-state
docker run --rm -p 8080:8080 -v /srv/podsim-state:/var/lib/podsim --stop-timeout 30 \
  ghcr.io/dotwaffle/podsim:latest -addr :8080 -state file:///var/lib/podsim
```

If the server cannot write to the directory, startup stops with a `write startup session state` error.
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

## Session state

The `-state` option keeps the shared session across server restarts.
Its value is a bucket URL. This server build has only the `file://` driver.
Without `-state`, the server does not save or read a session state.

A `file://` URL needs an empty host and an absolute path, for example `file:///var/lib/podsim`.
`file://var/lib/podsim` is not valid, because `var` is then the host.
The only query parameter is `prefix`. The server puts it in front of each file name.
For example, `prefix=podsim/` puts the files in the `podsim` subdirectory, and `prefix=podsim-` gives `podsim-session.json.gz`.
The prefix can contain only letters, digits, `.`, `_`, `-`, and `/`. It cannot contain `..` or `__`.
Do not put credentials in the URL.
The server logs and errors show the URL without user information and query values.
But the error text of a storage driver can contain the full URL.

The server makes a missing directory with mode 0700.
Each file gets mode 0600.
The server syncs each file and its directory before it continues.
Startup stops with an error when the URL is not valid, when the server cannot open the location, or when the first save fails.

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
- About 1 second after a demand change, a project apply, or a rewind that restores a project.
- At a graceful shutdown. This is the final save.

A save copies the state while it holds the session lock.
It encodes, compresses, and writes the copy after it releases the lock.
A failed write keeps the old file.
After a stop without a final save, the next start restores the last saved state, which can be up to about 60 seconds old.

At startup, the server reads `session.json.gz` and restores the session with one of these tiers:

- `physical`: The pods keep their lane positions and start again at speed 0. The server makes the track reservations again. A pod that conflicts with another pod, or that has a route that the server cannot restore, goes to a free berth. Its passengers board again at their origin, or go back to the queue.
- `logical`: The server uses this tier when the physical tier fails. The pods start again at their initial berths. Parties in pods go back to the queue. Parties that were unloading count as completed.
- `empty`: The server does not use the saved state and starts a new session. Except after a read failure, it moves `session.json.gz` to a rejected file.

The reason for an `empty` start is `project_changed`, `unsupported_version`, `invalid_state`, `too_large`, `restore_loop`, or `unreadable`.
`too_large` means more than 16 MiB, compressed or decompressed.
A file from a newer server version gets `unsupported_version`. Thus after a downgrade, the older server moves the file aside.
With `-project`, the project file has priority, and a saved state with a different project gets `project_changed`.
Without `-project`, the server restores the saved project.
When the server does not use the saved state, the new session uses the project file.
Without `-project`, it uses the saved project if that project is valid, and otherwise the example project.
After `restore_loop`, a server without `-project` uses the example project.

If the read fails or takes more than 30 seconds, the server starts an empty session with reason `unreadable` and does not save.
The file stays for the next start.
If the move of a rejected file fails, the server also does not save.
In both cases, the server logs an error. Correct the fault, then restart the server.

The startup save records the number of restores since the last periodic or final save.
If the server stops without a periodic or final save after a restore, the next start uses only the `logical` tier, with reason `restore_loop`.
After two such stops, the next start moves the file aside with reason `restore_loop`.
This stops a crash loop that a saved state causes.
A stop for another cause also counts, for example a `SIGKILL` before the first periodic save, about 60 seconds after the start.
When startup fails after the restore, for example because a listen address is in use, the server makes a final save before it stops.
Thus a failed startup does not count as a restore.

The server keeps the saved epoch only after a final save and a `physical` or `logical` restore.
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

A file from a newer version needs a server of that version or later.

To start a new session:

1. Stop the server.
2. Delete `session.json.gz`.
3. Start the server.

## Memory limit

The server sets the Go memory limit to 90 percent of the detected cgroup limit.
Set `GOMEMLIMIT` to use an explicit Go memory limit.
Set `AUTOMEMLIMIT` to change the ratio or disable automatic detection.

Save points keep copies of the simulation in memory.
A London save point uses about 6.2 MB after the live simulation continues from it.
The server keeps at most 8 save points, so they use about 50 MB with one London project.
A save point that holds a replaced London project uses about 4.6 MB more.
If each of the 8 save points holds its own London project, they use about 85 MB.

With `-state`, each save of the session state allocates memory for a short time.
A London save with 20 orders per minute, after 15 simulated minutes, allocates about 8.5 MB.
About 8 MB of this is JSON work on the 1.6 MB project.
The encoder checks and formats the project text again when it adds the project to the file.
The compressed file is about 320 KB.
A project near the 4 MiB file limit needs more memory.

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

HTTP telemetry excludes `/api/state` because each client polls it at 20 Hz.
It also excludes `/healthz`.
Other HTTP requests include route-based server traces and metrics.

Runtime metrics report memory, allocations, goroutines, processor limits, and the Go memory limit.
Session gauges report the simulation tick, journeys, pods, stopped pods, distance, pickup wait, and save points.
`podsim.checkpoint.retained` is the number of save points in memory.
Compare it with the runtime memory metrics to see the memory that save points use.
A reset, a demo, a project apply, or a rewind can decrease `podsim.simulation.tick`, `podsim.journey.submitted`, `podsim.journey.completed`, `podsim.travel.passenger.distance`, and `podsim.travel.empty.distance`.
These metrics are gauges, not counters, so do not use `rate()` on them.

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

With `-state`, the server writes these log records at startup:

- `Opened session state store` (INFO) gives the redacted `url` and the `location`, the path in front of each file name.
- `Removed temporary state files` (INFO) gives the `count` of deleted temporary files. `Remove temporary state files` (WARN) gives the `error` when the list or a delete fails. Startup continues.
- `No saved session state` (INFO) means that the location has no `session.json.gz`. The server starts a new session.
- `Restored session` (INFO) gives the `tier`, the `reason`, and the counts `demoted`, `requeued`, `dropped`, `droppedParties`, `overCap`, and `overBudget`.
  It also gives the saved `tick`, `epochKept`, `final`, `savedAt`, `savedBuild`, the current `build`, and `restoreAttempts`.
  `bytes` is the compressed size. `duration` is the time from the read to the end of the startup save.
  After a failed physical tier, `physicalError` tells why it failed.
- `Demoted pod` (DEBUG) gives each `pod` that the physical tier moved to a berth.
- `Rejected saved session state` (WARN) gives the `reason` and the `error`.
- `Restore failed with a panic` (ERROR) gives the `panic` and the `stack`. The server then rejects the file with reason `invalid_state`.
- `Read saved session state` (ERROR) means that the read failed or timed out. It gives the `error` and `saving=false`.
- `Move rejected session state` (ERROR) means that the move of a rejected file failed. It gives the `error` and `saving=false`.
- `Prune rejected session state` (WARN) gives the `error` when the server cannot delete an old rejected file. The next rejection tries again.
- `Backed up saved session state` (INFO) and `Back up saved session state` (WARN, with the `error`) tell the result of the copy to `session.previous.json.gz`. A failed copy does not stop the restore.
- `Stop during startup` (INFO) means that a signal came during the read of the saved state. It gives the `cause` and the `error`. The server stops without serving and without a save.

It writes these records for each save:

- `Saved session state` (DEBUG) is a startup or periodic save. It gives the `kind`, the compressed size in `bytes`, the `revision`, and the `tick`.
  It also gives three durations: `lock` to copy the state under the session lock, `encode`, and `write`.
- `Saved final session state` (INFO) is the final save, with the same attributes. A startup that fails after the restore also makes a final save.
- `Save session state` (WARN) is a failed save. It gives the `kind`, the `error`, the `cause` of a timeout, and `failures`, the number of failed saves in sequence.
  A state that is too large gives ERROR, because each later save also fails.

It writes these records at shutdown:

- `State saver did not stop` (WARN) means that the saver did not stop in 1 second. It gives the `timeout`.
- `Skipped final save` (WARN) gives the `reason`. `clock` means that the clock did not stop, `saver` means that the saver did not stop, and `off` means that saving is off.
- `Final save failed` (WARN) means that the final save failed or did not return in 6 seconds. It gives the `error` and the `cause`.
  The cause of a timeout is `final save of the session state timed out`.
  When the store returned an error, the session also logs `Save session state`.
- `Close session state store` (WARN) gives the `error` of the close of the store.

## Graceful shutdown

The server starts a graceful shutdown when it gets `SIGINT` or `SIGTERM`, or when a listener fails.
It logs `Stop accepting commands` with the cause.
Then it stops the simulation clock and rejects new commands with HTTP 409 and the `server_stopping` error code.
A command that is already in progress completes.
An exact retry of the last command from a client still gets the stored reply.

Then the server closes its listeners and does not accept new requests.
Requests that are already in progress, including reads, get up to 5 seconds to complete.
Next, the server waits up to 5 seconds for the clock goroutine to return.

With `-state`, the server then stops the state saver and waits up to 1 second for it.
A periodic save in progress stops, and the old file stays.
If the clock and the saver stopped, the server saves the session state a last time.
This final save gets 5 seconds.
The server waits up to 6 seconds for it, because a blocked file system call can continue after the timeout.
A final save that fails or times out keeps the old file.
If the clock did not stop, the state can still change. If the saver did not stop, its write can still use the store.
In both cases, the server does not make a final save.
Without a final save, the next start restores the last periodic save with a new epoch.

Last, the server flushes telemetry for up to 5 seconds.

With `-state`, a shutdown can take up to about 22 seconds.
This is 5 seconds for the requests, 5 for the clock, 1 for the saver, 6 for the final save, and 5 for telemetry.
`docker stop` waits only 10 seconds by default, then it kills the process.
Use `docker stop -t 30`, or `--stop-timeout 30` with `docker run`.
On Kubernetes, keep `terminationGracePeriodSeconds` at 30 or more.
