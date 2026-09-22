# Podsim

A browser playground for personal rapid transit networks, built with Go and Ebitengine.

The supplied scenario starts two pods on a network with three passenger stations and a two-space parking station.
Use the scenario editor to change the network, fleet, berth capacity, and demand settings.

Select **Download debug state** above the simulation to save a timestamped JSON capture of the current server state.
It includes the network, pod positions and routes, queues, berth reservations, and demand settings without pausing the run.
Share this file when reporting congestion or other unexpected behavior. It is a diagnostic capture, not a reloadable project or checkpoint.
Request journeys or run the supplied four-pod traffic demo to inspect merge and station queues.
The network includes a branch, a bypass, a merge, and station berths outside the through lanes.

## Run

Install mise, then prepare the project tools:

```sh
mise trust
mise install
```
From the project directory, run:

```sh
mise run serve
```

Open http://127.0.0.1:8080 in desktop Chrome.
The network view fills the browser window and renders at the display pixel density.
Resize the window to give the map more space. Map navigation and layout stay local to each browser.
The build creates static files in `dist/`, including the matching Go WebAssembly runtime and a precompressed WASM file.
The first build downloads Go dependencies.
Keep the server running while using the application.
All browsers connected to this server share one in-memory session.
Refreshing a browser retains the session. Restarting the server resets the running simulation.

To select a different listen address:

```sh
mise run serve -- -addr 127.0.0.1:8081
```

To load and save server settings, provide an existing scenario JSON file:

```sh
mise run serve -- -project scenario.json
```

This file contains the `project` object from `/api/project`.
The server saves accepted setting changes with an atomic file replacement.
The browser export wraps this object as `scenario` and can also contain a background image.

## Production server

`go generate ./...` builds the browser application and gzip WASM asset.
An `embed_assets` server build contains all browser files in one executable.
The project also includes a non-root, multiarchitecture ko image and a GHCR publishing workflow.
The server provides `/healthz`, opt-in pprof on a separate listener, and opt-in OTLP telemetry.
The browser uses normalized gzip JSON frames.
See [distribution and operations](docs/operations.md) for build and runtime settings.

## Controls

- Open **Scenario**, select **Example traffic sequence**, then select **Start example sequence**.
- Select a **Pod** button, or click a pod on the map, to inspect it.
- Compact fleet numbers match the pod buttons and map. Inspection also shows the full pod ID.
- Pod colors show their purpose: idle, pickup, passenger service, parking, redistribution, or other empty travel.
- The map legend explains the colors. An amber ring marks waiting pods, and a white ring marks the selected pod.
- Scroll over the map to zoom at the pointer. Drag the map to pan. Use **Fit** to show the whole network.
- Map navigation stays local to your browser. Zoom in to see individual berths in crowded stations.
- Select **Follow** or press **F** to keep the selected pod centered. Dragging the map or selecting **Fit** stops following.
- Overview labels show occupied berths and nonzero entrance and exit queues.
- Expanded labels show occupied, reserved-empty, and free berths. These counts sum to station capacity.
- Expanded entrance labels show stopped and approaching pods. Exit labels show stopped departing pods.
  Queue counts include dedicated access spurs, but exclude general road traffic.
  A reserved-empty berth can still have a departing pod that holds its clearance resource.
- Select **From** and **To**, then **Order**. Pod selection affects inspection only.
- An idle local pod serves the request. Otherwise, the nearest available empty pod comes to collect the passenger.
- Requests wait when no pod is available. Open **Orders** to see queued and active journeys and their status.
- Each accepted request shows its order number and briefly changes the request button to **Order accepted**.
- Pickup wait statistics show average and maximum seconds since reset. Pending orders contribute their elapsed wait.
- Wait ends when boarding starts, so it includes empty-pod travel to pickup.
- Fleet use shows the percentage of pods with assigned or active work and the percentage currently in passenger service.
- Use **Pause**, **Resume**, **Reset**, and **Speed** to control playback.
- Keyboard: **Enter** submits an order and **Tab** selects the next pod.
- **Space** pauses, **R** resets, **S** changes speed, and **F** toggles pod following.
- Reset restores the saved scenario fleet and demand settings, clears requests and reservations, and returns to 1x playback.

Boarding takes three simulated seconds. Unloading takes two.
The selected pod shows its activity, speed in whole km/h, occupancy, route, and local waiting reason.
The display distinguishes a pod ahead, conflicting junction traffic, an occupied berth, and unavailable parking.
Berth labels distinguish a parked pod from an admitted arrival.
A paused simulation accepts and assigns requests but does not advance until you resume it.

### Traffic demo

The demo starts four pods: pod 01 at Harbor, pod 02 at Garden, and pods 03 and 04 in Parking.
Pod 01 leaves Harbor for Market.
A supplied trigger starts pod 02 from Garden when pod 01 reaches a defined point on the bypass.
Both pods contend at the merge, then arrive at Market's single berth.
The second arrival waits on the station inlet while the first pod clears its berth.

The same trigger adds Harbor-to-Garden and Garden-to-Harbor orders, bringing the parked pods into service.
After two journeys finish, four more orders join the queue: Market to Harbor, Market to Garden, Harbor to Market, and Garden to Market.
Normal dispatch and traffic rules handle these orders.
The demo ends after eight passenger journeys and all empty moves finish.

Use 8x speed to see the experiment in about 41 seconds, or slow playback to inspect a queue.
Starting the demo resets the current run and disables automatic demand. Manual requests are disabled during the demo.
The four pods remain available after it ends. **Reset** restores the configured fleet.
The supplied traffic demo requires the unchanged example network and fleet.

### Passenger demand

Open **Demand** to select the rate, traffic pattern, and seed, then select **Start demand**.
Rates use simulated minutes, so 8x playback generates orders eight times faster in wall time.
Balanced traffic chooses among all passenger stations. Market-bound traffic sends Harbor and Garden passengers to Market.
Projects can also include weighted origin-destination profiles with named time
bands.
The London preset includes eight TfL bands and selects AM peak by default.
The selected band remains active until the demand settings change.
Arrivals have equal time intervals. The seed determines the station choices.
The same seed, settings, initial state, and manual actions produce the same run.
Starting demand or changing enabled settings restarts the stream and its counters.
Pause stops both movement and arrivals. Reset restores configured demand. The traffic demo temporarily disables demand.
The queue holds up to 200 pending orders. Full queues skip generated arrivals and reject new manual orders.
Skipped arrivals appear in the Demand panel and do not accumulate for a later burst.

## Scenario editor

Select **Edit scenario** above the simulation to open the editor.
The draft stays local until you select **Pause and apply**.
Applying a valid draft resets the shared simulation and leaves it paused.
If another browser changes the project, the server rejects stale edits and preserves the draft.
Export the draft before reloading a newer server project.

- Create stations and explicit junctions, then connect their nodes with directed guideways.
- Select paired lanes to add both directions. Crossing lines do not create a junction.
- Select a guideway to adjust its curve and speed in km/h.
- Set station berth capacity and place initial pods in free berths.
- Set the passenger rate, pattern, OD profile, time band, seed, and redistribution option.
- Use undo and redo for draft changes. Pan empty space and use the wheel to zoom.
- Import a PNG or JPEG background. Calibrate two points with a known distance in meters.
- Export JSON to save the scenario and optional background. Import JSON to restore a draft.

Project files save the design and settings, not an exact running checkpoint.
Malformed or unsupported files do not replace the draft.
Validation checks routes between passenger stations, pod placement, resource IDs, and the 24-meter minimum lane length.

### Demand and policy comparisons

Redistribution is off by default. Passenger assignments take priority over empty positioning.
Before a pod moves, redistribution reserves a free destination berth. A cooldown limits repeated moves.
A remote reservation yields to passenger traffic until the empty pod enters the final admitted block.
Admitted track and physical berth ownership remain protected.
Demand weights forecast pickup locations.
This policy can increase waiting or empty travel when demand differs from the forecast.

Run the same seeded demand schedule with redistribution off and on:

```sh
mise run compare -- -seed 7 -duration 10m -request-every 60s
```

The report includes demand throughput, backlog, drain time, fleet use, pickup
wait, completed and remaining journeys, passenger and empty travel,
loaded-distance percentage, and positioning moves.
The default comparison uses a Market-heavy pickup forecast and identical initial fleets.
Use `-patterns all -seeds 1,2,3 -loads 30s,45s,60s` for a paired matrix.
Use `-format json` or `-format csv` to save results, and `-project scenario.json` to test another network.
Schedule IDs identify the identical requests used for each off/on pair.
The synthetic patterns are balanced, destination, hotspot, bursty-hotspot, and
hub-burst. Use `-pattern profile -bands all` with a project demand profile to
run its origin-destination bands.
Use `-arrivals-for` to stop new requests before the measurement ends.
Use `-stop-when-drained` to stop an arm after all accepted requests complete.
Use `-workers` to run independent arms concurrently. Reports retain their
deterministic order.
Use `-burst-size` to group burst-pattern requests at the same simulated time.
Use `-sharing-limits 1,4` to compare same-destination party limits.
Use `-routing-policies free-flow,congestion` for the experimental route-cost A/B.
Use `-redistribution-policies off` to hold redistribution fixed.
Pending requests contribute their elapsed wait at the end of the measurement window.

### Generated scenarios

Create a repeatable server scenario:

```sh
mise run scenario -- -preset scale100 -output /tmp/podsim-scale100.json
mise run serve -- -project /tmp/podsim-scale100.json
```

Presets include `small`, `busy`, `parking-constrained`, `rail-hub`, `scale100`,
and `london`.
The rail-hub preset has six passenger stations, 30 pods, six berths per passenger
station, and 12 parking berths with 12 initial reserve pods.
It supports the recorded finite-arrival station-capacity experiment:

```sh
mise run scenario -- -preset rail-hub -output /tmp/podsim-rail-hub.json
mise run compare -- -project /tmp/podsim-rail-hub.json -pattern hub-burst -duration 30m -arrivals-for 5m -request-every 5s -burst-size 12 -seeds 1,2,3,4,5
```

The scale preset uses a connected grid with alternate routes and explicit junctions.
It has 19 passenger stations, one parking station, 138 berths, and 100 pods.
Each station has separate road connections for arrival and departure, 600 meters apart.
Parallel arrival and departure lanes serve successive berth rows. Parking extends outside the road grid.
This geometry separates incoming and outgoing traffic and shortens the parking entrance conflict sections.
Demand starts disabled. Configure and start it from the Demand panel.
Generated files contain raw server settings. The editor can export the loaded scenario with optional local background data.
See [qualification results](docs/qualification.md) for safety checks, performance measurements, and redistribution limits.
The [London qualification network](docs/london.md) uses TfL station locations
and topology, real station names, directed twin guideways, off-line berths, and
three Parking facilities.
Station lanes identify approach, entry, berth access, through, departure, and
exit maneuvers.
The pod inspector reports the current maneuver and station name.
Projects without this optional lane metadata still load; the simulator infers
berth access and departure roles from the station paths.

## Scope and model

### Time, geometry, and routing

The Go core uses fixed 60 Hz steps and world coordinates in meters.
Routing chooses the shortest free-flow travel time on directed lanes.
Equal-cost routes use scenario order for deterministic results.
Automatic demand is optional and starts disabled.

Lanes can be straight or quadratic curves.
The simulator and browser measure each curve along the same sampled path.
Each lane has an explicit speed limit.
Bends do not impose extra speed limits.

### Passenger service and dispatch

One party contains one passenger. By default, each party uses one pod.
An optional limit of two to eight lets unassigned parties join a pod that is
still boarding at the same origin for the same destination.
Sharing does not wait for more parties or add stops.

Passengers request travel between stations independently of pod selection.
Dispatch considers requests in submission order.
An idle local pod serves the oldest waiting passenger.
Otherwise, dispatch compares idle pods and empty pods that can divert from parking.
It uses estimated pickup time, then pod ID, as the tie-breaker.

Dispatch can wait for a busy pod if it should reach pickup at least two seconds earlier.
The estimate includes travel, acceleration, braking, boarding, and unloading.
It does not predict traffic delays.
Dispatch waits for at most 30 simulated seconds before it uses an available pod.

Assigned pickup pods can depart while the berth is occupied and queue on its approach.
They claim the berth through local admission, not a remote reservation.
Passengers board only at a berth.

If pickup pods arrive out of order, the first available pod takes the oldest passenger at that station.
The other pod retains a pickup at the same station for the later order.

An empty pod heading to parking can divert for a pickup.
It preserves all committed track and releases its unused parking claim.
A pod already committed to the parking inlet finishes that maneuver before returning to service.
Empty pickup travel retains the original request ID and does not count as a passenger journey.
The fixed demo can still submit journeys directly to specific pods.

### Track admission and junctions

Pods accelerate and brake within their admitted track distance.
They extend reservations to cover the stopping distance at the lane speed plus two simulation ticks of travel.
On clear track, this allows steady cruising at 14 m/s across block boundaries.

The local controller divides lanes into exclusive blocks of up to 30 meters.
It retains trailing blocks until a four-meter pod and eight-meter gap have cleared.
Each junction has one conflict resource for nearby sections of its incident lanes.
The controller derives these sections from the same geometry used for movement, including the 12-meter clearance.
It acquires each continuous conflict section together, including downstream cells needed to cross lane endpoints.
It releases the conflict resource after the pod reaches the end of that section.

The oldest local admission request wins.
Pod ID breaks a tie.
Admission uses the state before movement.
Released resources become available on the next tick.

The block model is conservative.
It does not model continuous car-following or optimized junction capacity.
The traffic model requires lanes at least 24 meters long.

### Stations and parking

Passenger journeys route to the station entry without a berth assignment.
The controller chooses the least-assigned reachable berth when the pod enters the final station-access lane.
The controller can change this choice before it reserves a berth branch.

A pod reserves its berth before entering the final inlet block.
It keeps the berth through unloading and idle time, until its departure clears the resource.
Other pods can queue on the inlet while through traffic uses the separate through lane.

Station maneuver roles describe this existing movement and reservation behavior.
They do not add a second station controller or change admission priority.
The controller makes local reservations, not a whole-journey timetable.

Station entry, berth, and exit connections have stable identities.
Passenger and parking stations can have multiple berths.
Berths can connect through intermediate arrival and departure lanes, separate from through traffic.
Station-local paths cannot cross another berth or station.
Each station retains a direct entry-to-exit through lane.

An idle empty pod clears its berth when it blocks a passenger arrival, an assigned pickup pod, or an empty relocation.
It first reserves a free reachable parking berth and retains its origin until physical clearance.
If parking is full or unreachable, it reserves reachable passenger space instead.
It prefers local space that no request targets.
The pod shows "No parking available" only when no reachable physical space exists.

A passenger or pickup pod can choose a free alternate berth before it reserves the next station branch.
A route change preserves all admitted track.
Empty relocations yield unadmitted destination claims when a local passenger or pickup needs the same berth.
Physical ownership and admitted destination resources remain protected.

Empty moves have no boarding or unloading delay and do not count as passenger journeys.
Parking serves no passengers.
Parked pods return to service automatically when assigned to a pickup request.
After the demo, request a trip from Harbor or Garden to see an available pod return for pickup.

The tests establish progress for feasible supplied scenarios, not for every saturated network.
Optional redistribution moves idle empty pods toward configured demand before requests arrive.

### Server and browser

One server owns the simulation clock, commands, and demand settings.
Browsers poll dynamic state frames and show connection status.
They fetch network topology on connection and after a project revision changes.
Controls wait for server confirmation.

The map buffers 150 ms of snapshots and interpolates movement along lanes between updates.
Controls and order status use the latest server state.
Pauses, resets, and long connection gaps clear buffered motion.
Rendering never predicts movement beyond the latest received position.

The server uses gzip for snapshots and browser assets when the client supports it.
Range responses remain uncompressed.
WASM uses a build-time gzip artifact to reduce downloads without repeating compression for each browser.
If that artifact is missing or older than the WASM file, the server compresses the current file during the request.

A lost connection disables commands.
Reconnection restores the current shared state.
The server deduplicates command retries by client and sequence.
Replay records support 1,024 browser loads per server lifetime.
Restart the server if this prototype limit is reached.

Pod selection, origin, destination, and the open inspection panel stay local to each browser.
Background images stay in the editor and exported project file.
The shared simulation receives network geometry and settings.

See [the client protocol](docs/protocol.md) for payload measurements and the Protobuf evaluation.
See [the project brief](PROJECT_BRIEF.md) for the wider scope and research.

## Code and validation

| Path | Purpose |
| --- | --- |
| `internal/sim` | Network, routing, requests, pod movement, and deterministic tests. |
| `internal/project` | Versioned scenario settings, validation, and detached copies. |
| `internal/scenarios` | Deterministic scale fixtures and qualification tests. |
| `internal/session` | Shared clock, command validation, HTTP API, and repeatable demand. |
| `internal/remote` | Snapshot polling, command retries, and connection state. |
| `internal/view` | Ebitengine rendering and input against copied snapshots. |
| `internal/telemetry` | Optional OTLP traces, HTTP metrics, runtime metrics, and session gauges. |
| `internal/cmd/buildweb` | Generated browser files and gzip WASM artifact. |
| `cmd/podsim` | Desktop and WASM entry point. |
| `cmd/serve` | Shared session, browser assets, health checks, and diagnostics. |
| `cmd/compare` | Reproducible policy comparisons. |
| `cmd/scenario` | Generated scenario files. |
| `web` | Browser loader and scenario editor. |

```sh
mise run test
mise run check
```

`mise.toml` tracks Go 1.27 and major versions for the other development tools.
`mise.lock` records the resolved tool downloads.
`mise run check` runs workflow validation, race tests, editor tests, vet, lint, vulnerability checks, and both builds.

GitHub Actions runs the same check on pull requests and pushes to `main`.
The workflow also supports a manual trigger.
New pull-request updates cancel older runs.
Each `main` push keeps its own run.
The workflow uses major-version action tags and installs tools from `mise.lock`.
Go module, build, and lint analysis caches use job-specific keys and refresh after successful runs.
The lint configuration follows Q but omits irrelevant database and protobuf rules.

Validation has five main parts:

- Core tests cover routing, journeys, invalid requests, pause and reset behavior, repeatability, and state isolation.
- Rendering tests cover buffered movement, lane corners, station movement, pause and reset behavior, and stale snapshots.
- HTTP tests cover compression, topology and frame decoding, command
  acknowledgments, WASM responses, byte ranges, health, and diagnostics.
- Traffic tests cover separation, merge contention, berth capacity, stopped queues, through traffic, and eventual progress.
- Browser checks confirm visible movement and working controls. A WASM build alone is not sufficient.
