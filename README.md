# Podsim

A browser playground for personal rapid transit networks, built with Go and Ebitengine.

The supplied scenario starts two pods on a network with three passenger stations and a two-berth parking station.
The network includes a branch, a bypass, a merge, and station berths outside the through lanes.
Request journeys, or run the supplied four-pod traffic demo, to inspect merge and station queues.
Use the scenario editor to change the network, fleet, berth capacity, and demand settings.

## Run

### Install and start

Install mise, then prepare the project tools:

```sh
mise trust
mise install
```

From the project directory, run:

```sh
mise run serve
```

Open <http://127.0.0.1:8080> in desktop Chrome.
Keep the server running while you use the application.
Desktop Chrome is the current browser target.
Firefox and Safari validation are not required.
Current development uses a local server.
Hosted deployment checks are release-stage work.

The first build downloads Go dependencies.
See [dependency review notes](docs/design/dependencies.md) for the recorded gRPC advisory discrepancy and state-store lint annotations.
The build creates static files in `dist/`, including the matching Go WebAssembly runtime and the gzip WASM file `podsim.wasm.gz`.

### Shared session

All browsers connected to this server share one in-memory session.
A browser refresh keeps the session.
Without `-state`, a server restart resets the running simulation.
With `-state`, the server saves the session and restores it after a restart.
The session state section below describes this.

### Desktop client

The native desktop client connects to the same server session.
Run `mise exec -- go run ./cmd/podsim`.
Use `-server` to select another server URL.
The desktop client does not include the scenario editor or the debug capture.
Use a browser for them.

### Listen address

To select a different listen address:

```sh
mise run serve -- -addr 127.0.0.1:8081
```

### Project file

To load and save server settings, provide an existing project JSON file:

```sh
mise run serve -- -project scenario.json
```

### Session state

To keep the running session across server restarts, give a state directory as a `file://` URL with an absolute path:

```sh
mise run serve -- -state file:///var/lib/podsim
```

`-state` is off by default.
Without it, the server does not save or read a session state.
The server makes the directory if it does not exist.
The server user needs write access to it.

The server saves the session state at these times:

- At startup.
- Every 60 seconds while the session changes.
- Before it replies to a project apply that changes the project, or to a rewind that restores a project.
- A final time at a graceful shutdown.

The server waits at most 2 seconds for the save before a reply.
If that save fails or takes more than 2 seconds, the reply tells the client.
The simulation view or the editor then shows a warning.
A demand change, a project apply that changes the project, and a rewind that restores a project also start a save about 1 second later.

The saved state holds the project, the pods, the order queue, the demand stream, and the statistics.
It also holds the playback speed, the pause state, and the last command sequence of each client.
It does not hold save points or command receipts.
After a stop without the final save, the restored session can be up to about 60 seconds old.

At startup, the server restores the saved session with one of these tiers:

| Tier | Behavior |
| --- | --- |
| `physical` | The pods keep their positions and start again from rest. A traveling pod that cannot keep its position goes to a free berth. |
| `logical` | The pods start again at their initial berths. Parties that were unloading at their stop count as completed. Each other party in a pod goes back to the order queue as one order. |
| `empty` | The server does not use the saved state and starts a new session. Except after a read failure, it moves the file aside. |

State frames give the result in `restore`.

With `-project`, the project file has priority:

- If the saved project is different, the server starts a new session with the project file.
- If only demand or experimental policy settings differ, the server restores the session and applies the project settings.
  While the saved traffic demo runs, the server cannot change the demand settings.
  It then starts a new session with the project file.

Without `-project`, the server restores the saved project.

See [session state](docs/operations.md#session-state) for the file names and the recovery steps.
See [session state logs](docs/operations.md#session-state-logs) for the logs.

## Production server

`go generate ./...` builds the browser application and the gzip WASM file.
An `embed_assets` server build contains all browser files in one executable.
The executable holds only the gzip WASM file, so it is about 35 MB and not 64 MB.

The project also includes a non-root, multiarchitecture ko image and a GHCR publishing workflow.
The server provides `/healthz`, opt-in pprof on a separate listener, and opt-in OTLP telemetry.

The browser receives shared gzip JSON state deltas over WebSocket.

See [distribution and operations](docs/operations.md) for build and runtime settings, the saved session state, graceful shutdown, save point memory use, and save point logs.

## Documents

- [User guide](docs/user-guide.md): running the simulation and using the scenario editor.
- [Reference](docs/reference.md): the project file format and the command-line tools.
- [Simulation model](docs/model.md): how the simulation behaves.
- [Distribution and operations](docs/operations.md): the server, the code layout, the tasks, and continuous integration.
- [Measurement reports](docs/studies/qualification.md#measurement-reports): the recorded studies.

See [the client protocol](docs/protocol.md) for the API, save point commands, payload measurements, and the Protobuf evaluation.
See [the project brief](PROJECT_BRIEF.md) for the wider scope and research.
