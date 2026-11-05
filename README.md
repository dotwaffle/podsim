# Podsim

Podsim is a deterministic simulator of personal rapid transit networks, where small driverless pods carry each party directly between stations on one-way lanes.
You build a network in a browser editor, order journeys or generate demand, and watch where pods wait and why.
The main interest is network behavior: routing, dispatch, congestion, and station capacity.
A Go server owns the simulation, an Ebitengine view compiled to WebAssembly shows it, and the same project, seed, and commands give the same run.
Podsim is a hobby simulator, and it does not certify or predict a real transport system.

The supplied scenario starts two pods on a network with three passenger stations and a two-berth parking station.
The network has a branch, a bypass, a merge, and station berths outside the through lanes.

## Quick start

Install [mise](https://mise.jdx.dev/), then prepare the project tools:

```sh
mise trust
mise install
```

From the project directory, start the server:

```sh
mise run serve
```

Open <http://127.0.0.1:8080> in desktop Chrome, and keep the server running while you use the application.
Desktop Chrome is the browser target, so Firefox and Safari validation are not required.
Current development uses a local server, and hosted deployment checks are release-stage work.

The first build downloads the Go dependencies.
The build creates static files in `dist/`, including the matching Go WebAssembly runtime and the gzip WASM file `podsim.wasm.gz`.

From there, order a journey under **ORDER A JOURNEY**, or open **Demand** and select **Start traffic demo** to see merge and station queues.
Select **Edit scenario** to change the network.
The [user guide](docs/user-guide.md) explains each control.

## Clients

All browsers on one server share one session, so a command from one browser affects all of them, and a browser refresh keeps the session.

The native desktop client connects to the same session:

```sh
mise exec -- go run ./cmd/podsim
```

Use `-server` to select another server URL.
The desktop client has no scenario editor or debug capture, so use a browser for them.
A browser gets shared gzip JSON state deltas over WebSocket (see the [client protocol](docs/protocol.md)).

## Server options

Pass server flags after `--`, for example `mise run serve -- -addr 127.0.0.1:8081`.

| Flag | Effect | Default |
| --- | --- | --- |
| `-addr` | The HTTP listen address. | `127.0.0.1:8080` |
| `-project` | An existing project JSON file to load and save. | The example project, not saved |
| `-state` | A `file://` URL of a directory that keeps the session across restarts. | Off |
| `-dir` | A directory of browser files, in place of the embedded ones. It must hold `podsim.wasm.gz`: the server does not read a `podsim.wasm` file, and it answers 404 for the module without the gzip file. | Embedded files with `embed_assets`, otherwise `dist` |
| `-public-origin` | The public origin of the server behind a [TLS-terminating proxy](docs/operations.md#tls-terminating-reverse-proxies). | The request origin |
| `-pprof-addr` | A separate listen address for [pprof](docs/operations.md#pprof). | Off |
| `-geocoding-url` | A Nominatim-compatible endpoint for [place search](docs/user-guide.md#find-a-place). An empty value turns search off. | `https://nominatim.openstreetmap.org/search` |

### Project file

To load and save the project in a file, give the server an existing project JSON file:

```sh
mise run serve -- -project scenario.json
```

The server saves the file before it applies a setting change, and it rejects the change if the save fails.
This file is the `project` object from `/api/project`, and `-project` does not accept a browser export.
See the [reference](docs/reference.md#project-file) for the format and its limits.

### Session state

Without `-state`, a server restart resets the running simulation.
To keep the running session across restarts, give a directory as a `file://` URL with an absolute path:

```sh
mise run serve -- -state file:///var/lib/podsim
```

The server makes the directory if it does not exist, and the server user needs write access to it.
The saved state holds the project, the pods, the order queue, the demand stream, the statistics, the playback speed, the pause state, and the last command sequence of each client.
It does not hold save points or command receipts.

The server saves at startup, every 60 seconds while the session changes, about 1 second after a demand change, before it replies to a project apply or a rewind that restores a project, and at a graceful shutdown.
It waits at most 2 seconds for the save before a reply, and if the save fails or takes longer, the reply tells the client and the view or the editor shows a warning.
After a stop without the final save, the restored session can be up to about 60 seconds old.

At startup, the server restores the session with one of three tiers, `physical`, `logical`, or `empty`, and state frames give the result in `restore`.
The `physical` tier keeps the pod positions, the `logical` tier starts the pods again at their initial berths, and the `empty` tier starts a new session.

With `-project`, the project file has priority.
If the saved project differs, the server starts a new session with the file.
If only demand or experimental policy settings differ, the server restores the session and applies the settings of the file, unless the saved traffic demo is running, which starts a new session.
Without `-project`, the server restores the saved project.

See [session state](docs/operations.md#session-state) for the file names, the restore rules, and the recovery steps, and [session state logs](docs/operations.md#session-state-logs) for the logs.

### Production server

`go generate ./...` builds the browser application and the gzip WASM file.
An `embed_assets` server build holds all browser files in one executable of about 41 MB.
The project also has a non-root, multiarchitecture ko image and a workflow that publishes it to GHCR.
The server provides `/healthz`, opt-in pprof on a separate listener, and opt-in OTLP telemetry.
See [distribution and operations](docs/operations.md) for the build and runtime settings, graceful shutdown, save point memory use, and logs.

## What it does

The browser application covers the whole loop from drawing a network to reading its delays:

- **Network editor**: stations, junctions, curved lanes, berths, and a fleet, with undo, live checks, map backgrounds, and JSON import and export.
- **Simulation**: finite fleets, local traffic control, separate station berths, and a wait reason for each stopped pod.
- **Demand**: manual orders, rate-based and profile traffic, daily profiles, and rail arrivals and departures.
- **Service policies**: shared rides, virtual platoons, and redistribution of empty pods, all opt-in.
- **Incidents**: pod faults, debris, and rider emergencies, on in each session outside the traffic demo.
- **Shared sessions**: one server for all browsers, exact save points, rewind, and restart with `-state`.
- **Experiments**: generated [scenarios](docs/reference.md#generated-scenarios) up to the 309-site LondonFull network, and seeded [policy comparisons](docs/reference.md#compare-command) with reports.

Generated passengers ride alone unless the project sets `generatedOrderConsent` to `shared`, and demo orders are always private.

## Documents

Start with the guides at the top of `docs/`.
The folders hold the binding contracts, the designs, and the evidence behind them.
[PROJECT_BRIEF.md](PROJECT_BRIEF.md) gives the goals and the architecture, and [AGENTS.md](AGENTS.md) gives the rules for coding agents.

### Guides

| Document | Purpose |
| --- | --- |
| [User guide](docs/user-guide.md) | Running the simulation and using the editor. |
| [Reference](docs/reference.md) | The project format, its limits, and the command-line tools. |
| [Simulation model](docs/model.md) | How the simulation behaves. |
| [Distribution and operations](docs/operations.md) | The server, saved state, logs, code layout, tasks, and CI. |
| [Client protocol](docs/protocol.md) | The HTTP and WebSocket protocol. |
| [Repeating daily demand](docs/daily-demand.md), [rail arrivals](docs/rail-arrivals.md), [train connections](docs/rail-connections.md) | Demand plans. |
| [LondonCentral](docs/london.md), [LondonFull](docs/london-full.md) | The London networks and their limits. |
| [Station layout controls](docs/station-layout-editor.md), [independent banks](docs/station-banks.md) | Station geometry. |
| [Project check](docs/project-check.md), [offline workflow](docs/offline-workflow.md), [comparison consent](docs/compare-sharing-consent.md) | Tools and study settings. |

### Contracts

An approved contract in `docs/contracts/` is binding, and each document gives its status.

| Document | Subject |
| --- | --- |
| [Service contracts](docs/contracts/service-contract-proposals.md), [service byte limits](docs/contracts/service-byte-contract-proposal.md) | Order consent, vehicle classes, and byte caps. |
| [Express physical and encoding](docs/contracts/express-physical-encoding-contract-proposal.md) | The opt-in `express-v1` format. |
| [Service transitions](docs/contracts/incident-service-transitions-contract-proposal.md), [suspension](docs/contracts/incident-suspension-contract-proposal.md), [emergency](docs/contracts/incident-emergency-contract-proposal.md) | The incident stages. |
| [Marker removal](docs/contracts/marker-removal-contract-proposal.md) | Faults and emergencies with no project switch. |
| [Generated-order consent](docs/contracts/generated-order-consent-contract-proposal.md) | The `generatedOrderConsent` setting. |
| [Tick rate](docs/contracts/tick-rate-contract-proposal.md) | A lower tick rate. A proposal, not approved. |
| [Express standees](docs/contracts/express-standees-contract-proposal.md) | A 6 m Express with standees, and a curve tail from the lane polyline. The maintainer accepted its decisions. The seat and standee split, the motion envelope, and the polyline tail are built. |
| [Incident stage 7](docs/contracts/incident-stage7-contract-proposal.md) | Random faults and emergencies. Implemented. The soak of amendment A6 ran, and its Progress row missed. The Suspension median row is open. |

### Designs

`docs/design/` holds [adoption gates](docs/design/experimental-adoption.md), [release readiness](docs/design/release-readiness.md), [dependency notes](docs/design/dependencies.md), and the designs for [car journeys](docs/design/car-journey-design.md), the [energy estimate](docs/design/energy-model-design.md), [Group pods](docs/design/group-physics-design.md), and [onboard pickups](docs/design/onboard-pickup-design.md).

### Studies

[Qualification results](docs/studies/qualification.md) is the main record, and its [measurement reports](docs/studies/qualification.md#measurement-reports) table says what each study establishes.

| Area | Studies |
| --- | --- |
| LondonFull | [Post-fix capacity](docs/studies/london-full-postfix.md), [pickup supply](docs/studies/pickup-supply.md), [mirrored layout](docs/studies/station-mirror-load.md) |
| Dispatch and routing | [Pickup reassignment](docs/studies/pickup-reassignment.md), [sustained pickup swaps](docs/studies/pickup-swap-sustained.md), [predictive routing](docs/studies/predictive-routing.md), [berth routes](docs/studies/berth-route-preference.md), [policy failures](docs/studies/policy-failures.md) |
| Platoons and following | [Platoon follow-up](docs/studies/platoon-followup.md), [coupled trains](docs/studies/platoon-coupling-comparison.md), [close-up following](docs/studies/close-up-measurement.md), [station buffers](docs/studies/buffer-late-claim.md) |
| Express | [Express qualification](docs/studies/express-qualification.md), [standees evidence](docs/studies/express-standees.md) |
| Performance | [Live admission](docs/studies/admission-live-performance.md), [pickup route search](docs/studies/pickup-routing-performance.md), [study replays](docs/studies/study-performance.md), [test timing](docs/studies/test-speed.md), [policy restarts](docs/studies/experimental-policy-restarts.md), [incident soak timing](docs/studies/incident-soak.md) |

`docs/measurements/` holds the JSON and CSV data of these records, for example [the performance samples](docs/measurements/performance.json).

## Glossary

| Term | Meaning |
| --- | --- |
| Order | The request of one party to travel between two stations. The protocol action is `trip`, and the compare command says request. |
| Party | The passengers of one order. A party never splits. |
| Journey | The ride of a party from boarding to alighting. |
| Consent | Whether a party allows other parties in its pod: `private` or `shared`. |
| Pod | One vehicle. Its class sets its seats, its standee places, and its body length. |
| Large class | The `group` and `express` classes, which keep 20 m of clearance. |
| Lane | A one-way path between two nodes. The editor controls call it a guideway. |
| Track cell | An exclusive piece of a lane, up to 30 m long. A pod reserves cells before it moves. |
| Junction | A named node where lanes meet. Crossing lines do not make one. |
| Berth | A place in a station where one pod stops, outside the through lane. |
| Parking station | A station that serves no passengers, where idle pods wait. |
| Depot | A parking station made of queue lanes of slots. |
| Compact bay | An angled berth for Compact pods, in a group of lanes called a maneuver group. |
| Virtual platoon | Separate pods that follow closely and share track cells. |
| Redistribution | Moving idle empty pods toward busy stations, by the guarded positioning policy. |
| Project | The saved design and settings of a scenario. A preset is a project that the scenario command generates. |
| Session | The one running simulation that all clients of a server share. |
| Save point | An exact copy of the session in server memory. |
| Arm | One run of the compare command with one set of policies. |
