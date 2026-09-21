# Podsim

A browser playground for personal rapid transit networks, built with Go and Ebitengine.

This prototype runs two pods on a supplied network with three passenger stations and a two-space parking station.
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
The build creates static files in `dist/`, including the matching Go WebAssembly runtime.
The first build downloads Go dependencies.
Keep the server running while using the application.
All browsers connected to this server share one in-memory session.
Refreshing a browser retains the session. Restarting the server resets it.

To select a different listen address:

```sh
mise run serve -- -addr 127.0.0.1:8081
```

## Controls

- Select **Run traffic demo** to reset and run the supplied merge-and-berth experiment.
- Select a **Pod** button, or click a pod on the map, to inspect it.
- Select **From** and **To**, then **Request journey**. Pod selection affects inspection only.
- An idle local pod serves the request. Otherwise, the nearest available empty pod comes to collect the passenger.
- Requests wait when no pod is available. Open **Orders** to see queued and active journeys and their status.
- Each accepted request shows its order number and briefly changes the request button to **Order accepted**.
- Pickup wait statistics show average and maximum seconds since reset. Pending orders contribute their elapsed wait.
- Wait ends when boarding starts, so it includes empty-pod travel to pickup.
- Use **Pause**, **Resume**, **Reset**, and **Speed** to control playback.
- Keyboard: **1–3** select destinations, **Enter** requests, **Tab** selects the next pod, **D** starts the demo.
- **Space** pauses, **R** resets, and **S** changes speed.
- Reset restores pod 01 at Harbor and pod 02 at Garden, clears requests and reservations, and returns to 1x playback.

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
The four pods remain available after it ends. **Reset** restores the normal two-pod fleet.

### Passenger demand

Open **Demand** to select the rate, traffic pattern, and seed, then select **Start demand**.
Rates use simulated minutes, so 8x playback generates orders eight times faster in wall time.
Balanced traffic chooses among all passenger stations. Market-bound traffic sends Harbor and Garden passengers to Market.
Arrivals have equal time intervals. The seed determines the station choices.
The same seed, settings, initial state, and manual actions produce the same run.
Starting demand or changing enabled settings restarts the stream and its counters.
Pause stops both movement and arrivals. Reset and the traffic demo disable demand.
The queue holds up to 200 pending orders. Full queues skip generated arrivals and reject new manual orders.
Skipped arrivals appear in the Demand panel and do not accumulate for a later burst.

## Scope and model

The Go core uses fixed 60 Hz steps and world coordinates in meters.
Routing chooses the shortest free-flow travel time on directed lanes.
Equal-cost routes use scenario order for deterministic results.
Automatic demand is optional and starts disabled.

One party contains one passenger and uses one pod.
Passengers request travel between stations independently of pod selection.
Dispatch considers requests in submission order. An idle local pod serves the oldest waiting passenger.
Otherwise it compares idle pods and empty pods that can divert from parking, using estimated pickup time and pod ID as the tie-breaker.
It can wait for a busy pod whose committed work should finish soon enough to reach pickup at least two seconds earlier.
These estimates include travel, acceleration/braking allowances, boarding, and unloading. They do not predict traffic delays.
Deliberate waiting lasts at most 30 simulated seconds before using an available pod. Existing traffic rules still apply.
Assigned pickup pods can depart while the berth is occupied and queue on its approach.
They claim the berth through local admission, not a remote reservation. No passenger boards outside a berth.
If pickup pods arrive out of order, the first available pod takes the oldest passenger at that station.
The other pod retains a pickup at the same station for the later order.
An empty pod heading to parking can divert for a pickup. It preserves all committed track and releases its unused parking claim.
A pod already committed to the parking inlet finishes that maneuver before returning to service.
Empty pickup travel retains the original request ID and does not count as a passenger journey.
The fixed demo can still submit journeys directly to specific pods.
Pods accelerate and brake within their admitted track distance.
They extend reservations to cover the stopping distance at the lane speed plus two simulation ticks of travel.
On clear track, this allows steady cruising at 14 m/s across block boundaries.
The local controller divides lanes into exclusive blocks of up to 30 meters.
It retains trailing blocks until a four-meter pod and eight-meter gap have cleared.
Each junction has one conflict resource, acquired together with downstream space.
The oldest local admission request wins, with pod ID as the tie-breaker.
Admission uses the state before movement. Released resources become available on the next tick.

A pod reserves its berth before entering the final inlet block.
It keeps the berth through unloading and idle time, until its departure clears the resource.
Other pods can queue on the inlet while through traffic uses the separate through lane.
The controller makes local reservations, not a whole-journey timetable.
Lanes are straight, and bends do not impose extra speed limits.
Station entry, berth, and exit connections have stable identities.
Each passenger station has one berth. The parking station has two.
All berths have direct entry and exit lanes, separate from through traffic.

This block model is conservative. It does not model continuous car-following or optimized junction capacity.
The current traffic model requires lanes at least 24 meters long.
An idle empty pod clears its berth when it blocks a passenger arrival or an assigned pickup pod.
It reserves a free reachable parking berth before departure and retains its origin until physical clearance.
If parking is full or unreachable, the arrival stops and shows "No parking available."
Empty moves have no boarding or unloading delay and do not count as passenger journeys.
Parking serves no passengers. Parked pods return to service automatically when assigned to a pickup request.
After the demo, request a trip from Harbor or Garden to see an available pod return for pickup.
The tests establish progress for feasible supplied scenarios, not for every saturated network.
Proactive empty-pod redistribution before requests and detailed station maneuvers remain future work.
One server owns the simulation clock, commands, and demand settings.
Browsers poll snapshots and show connection status. Controls wait for server confirmation.
The map buffers 150 ms of snapshots and interpolates movement along lanes between updates.
Controls and order status use the latest server state. Pauses, resets, and long connection gaps clear buffered motion.
Rendering never predicts movement beyond the latest received position.
The server uses gzip for snapshots and browser assets when the client supports it. Range responses remain uncompressed.
A lost connection disables commands. Reconnection restores the current shared state.
The server deduplicates command retries by client and sequence.
Replay records support 1,024 browser loads per server lifetime. Restart the server if this prototype limit is reached.
Pod selection, origin, destination, and the open inspection panel stay local to each browser.
Map import and editing follow the simulation milestone.
See [the project brief](PROJECT_BRIEF.md) for the wider scope and research.

## Code and validation

- `internal/sim`: network, routing, requests, pod movement, and deterministic tests.
- `internal/session`: shared clock, command validation, HTTP API, and repeatable demand.
- `internal/remote`: snapshot polling, command retries, and connection state.
- `internal/view`: Ebitengine rendering and input. Commands go to the server. Drawing reads a copied snapshot.
- `cmd/podsim`: desktop and WASM entry point.
- `cmd/serve`: shared session and browser file server.
- `web`: browser loader.

```sh
mise run test
mise run check
```

`mise.toml` specifies the Go, golangci-lint, and actionlint versions.
`mise.lock` records the resolved tool downloads.
It runs workflow validation, race tests, vet, lint, and native and WASM builds.
GitHub Actions runs the same check on pull requests and pushes to main.
The workflow uses pinned actions and installs tools from `mise.lock`.
Go module and build caches use job-specific keys and refresh after successful runs.
Core tests cover route selection, journey completion, invalid requests, pause/reset, repeatability, and state isolation.
Rendering tests cover buffered movement, lane corners, arrival/departure, pause/reset, and stale snapshots.
HTTP tests cover compression negotiation, snapshot decoding, WASM content type, and byte-range responses.
Traffic tests check physical separation each tick, merge contention, berth capacity, stopped-pod queues, through traffic, and eventual progress.
Browser checks must also confirm visible movement and working controls. A WASM build alone does not establish browser behavior.
