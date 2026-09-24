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

The first build downloads Go dependencies.
The build creates static files in `dist/`, including the matching Go WebAssembly runtime and the gzip WASM file `podsim.wasm.gz`.

The network view fills the browser window and renders at the display pixel density.
Resize the window to give the map more space.
Map navigation and layout stay local to each browser.

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

This file contains the `project` object from `/api/project`.
The server writes compact JSON without indentation.
Before the server applies a setting change, it saves the file with an atomic replacement.
If the save fails, the server rejects the change.
A rewind that restores the project of a save point also rewrites this file.

Validation limits each project to 4 MiB in this compact form, also after a demand change.
This limit lets the server read the file at the next start.

The browser export wraps the `project` object as `scenario` and can also contain a background image.
**Import JSON** accepts the browser export and the server file, such as the output of `mise run scenario`.
`-project` does not accept the browser export.

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
- Before it replies to a project apply, or to a rewind that restores a project.
- A final time at a graceful shutdown.

The server waits at most 2 seconds for the save before a reply.
If that save fails or takes more than 2 seconds, the reply tells the client.
The simulation view or the editor then shows a warning.
A demand change, a project apply, and a rewind that restores a project also start a save about 1 second later.

The saved state holds the project, the pods, the order queue, the demand stream, and the statistics.
It also holds the playback speed, the pause state, and the last command sequence of each client.
It does not hold save points or command receipts.
After a stop without the final save, the restored session can be up to about 60 seconds old.

At startup, the server restores the saved session with one of these tiers:

| Tier | Behavior |
| --- | --- |
| `physical` | The pods keep their positions and start again from rest. A pod that cannot keep its position goes to a free berth. |
| `logical` | The pods start again at their initial berths. Parties that were unloading count as completed. Other parties in pods go back to the order queue. |
| `empty` | The server does not use the saved state and starts a new session. Except after a read failure, it moves the file aside. |

State frames give the result in `restore`.

With `-project`, the project file has priority:

- If the saved project is different, the server starts a new session with the project file.
- If only the demand settings are different, the server restores the saved session and applies the demand settings of the project file.
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
The browser uses normalized gzip JSON frames.

See [distribution and operations](docs/operations.md) for build and runtime settings, the saved session state, graceful shutdown, save point memory use, and save point logs.

## Controls

### Inspect a pod

- Select a pod number button, or click a pod on the map, to inspect it.
- With more than five pods, select **‹** or **›** to show the previous or next five.
  The text between the arrows gives the page number and the number of pages, for example **3 / 19**.
- Compact fleet numbers match the pod buttons and the map.
  Inspection also shows the full pod ID.
- The selected pod shows its activity, speed in whole km/h, occupancy, route, and local waiting reason.
- The display distinguishes a pod ahead, conflicting junction traffic, an occupied berth, and unavailable parking.

### Pod colors and shapes

- Pod colors show their purpose: idle, pickup, passenger service, parking, redistribution, or other empty travel.
- The colors stay different with protanopia, deuteranopia, and tritanopia.
- An idle pod and a pod in passenger service are filled discs.
  A pod that moves empty is a ring.
  An idle pod is white, so it looks different from the gray node dots and berth rings.
- A berth ring shows the color of a moving or busy pod in the berth.
  A berth with an idle pod keeps the gray ring.
  The pod number shows above and to the right of the pod, outside the berth ring.
- An amber ring around a pod marks a waiting pod.
  A white ring marks the selected pod.
- The map legend shows each color and shape.
- While the selected pod is not idle, the map shows its route in the pod color.
  The route shows above the stations and their labels, and below the pods.
- Light arrows on the lanes show the direction of travel.
  Where two arrows in about the same direction overlap, the map shows only one of them.

### Map navigation

| Action | Control |
| --- | --- |
| Zoom at the pointer | Scroll over the map |
| Pan | Drag the map |
| Zoom at the map center | **+** and **−** |
| Show the whole network | **Fit** |
| Keep the selected pod centered | **Follow** or **F** |

- The line above the map names the map input: **CLICK POD / CLICK FROM / SHIFT+CLICK TO / TAB POD / SCROLL ZOOM / DRAG PAN**.
- The scale bar in the lower-left corner of the map shows a round distance of 1, 2, or 5 times a power of ten, for example 500 m, 1 km, or 2 km.
- Map navigation stays local to your browser.
- Clicking or dragging the map, or selecting **Fit**, stops following.
- **Reset** and **Rewind** keep the map view when the network does not change.
- Zoom in to see individual berths in crowded stations.
  Until then, such a station shows one marker.
  The map does not show the idle pods in the station, except the selected pod.

### Station labels

- Overview labels show occupied berths and nonzero entrance and exit queues.
- Expanded labels show occupied, reserved-empty, and free berths.
  These counts sum to the station capacity.
- Expanded labels and berth numbers go on the side of the berths away from the station siding.
  They go on a different side when that side covers a much shorter length of lanes.
  They stay inside the map, off the berth rings, and off other expanded labels and berth numbers.
  A label without a free place does not show until you zoom or pan.
  Each text line has a dark backing, so a lane under the text does not make it hard to read.
  On a network with more than 30 stations, a pod label that covers an expanded label does not show, except the label of the selected pod.
- Expanded entrance labels show stopped and approaching pods.
  Exit labels show stopped departing pods.
- Queue counts include dedicated access spurs, but exclude general road traffic.
- A reserved-empty berth can still have a departing pod that holds its clearance resource.
- Berth labels distinguish a parked pod from an admitted arrival.

On a network with more than 30 stations, the map can omit an overview label where it covers another label or a station marker.
On such a network, only the selected pod has a map label until you zoom in to four times the **Fit** scale.
See [the London qualification network](docs/london.md) for these rules.

### Order a journey

- Under **ORDER A JOURNEY**, select **From** and **To**, then **Order [Enter]**.
  Pod selection affects inspection only.
- Click a station on the map to make it **From**.
  Shift+click a station to make it **To**.
  The station row then shows the page with that station.
  A click on a pod selects the pod, also when the pod is near a station.
- When the stations do not fit in one row, select **‹** or **›** to show the other stations, or click the station on the map.
- When **From** and **To** are the same station, **Order [Enter]** is disabled.
  The line below **From** and **To** then shows **Choose a different destination.** in amber.
- An idle local pod serves the order.
  Otherwise, the nearest available empty pod comes to collect the passenger.
- Orders wait when no pod is available.
- Each accepted order shows its order number and opens **Orders**.
  For 3 s, the **Order [Enter]** button reads **Order accepted** while **From** and **To** show the stations of that order.
- Open **Orders** to see queued and active journeys and their status.
  **Orders** shows five orders on each page in a window of the minimum size.
  A taller window shows more orders on each page.
  When there are more orders, use the **‹** and **›** arrows to go to the previous or next page.
  The text between the arrows gives the page number and the number of pages.
  The order status names a pod by its fleet number, which is the label of its pod button.
- Boarding takes three simulated seconds.
  Unloading takes two.

### Statistics

- Pickup wait statistics show the average and maximum seconds since reset.
  Pending orders contribute their elapsed wait.
- Wait ends when boarding starts, so it includes empty-pod travel to pickup.
- Fleet use shows the percentage of pods with assigned or active work and the percentage currently in passenger service.
- The run status beside the title shows the playback speed, the simulated time, and the number of completed journeys.
  While the run is paused, the run status is amber and starts with **PAUSED**.

### Playback

- Use **Pause**, **Resume**, **Reset**, and **Speed** to control playback.
- **Speed** cycles through 1x, 2x, 4x, and 8x.
- A paused simulation accepts and assigns requests but does not advance until you resume it.

### Reset

- Reset restores the saved scenario fleet and demand settings, clears requests and reservations, and returns to 1x playback.
  It keeps the pause state.
- A reset affects every browser, so it needs a second press.
- The first press of **Reset** or **Shift+R** does not reset the session.
  It shows **Select Reset again within 3 s to reset the shared session.**
- A second press within 3 s resets the session and shows **Session reset.**

### Save points and rewind

- Select **Save point** to keep an exact copy of the simulation, the demand stream, and the project settings in server memory.
- Select **Rewind** to return to the latest save point.
  The button shows the simulated time of that save point.
- A rewind leaves the session paused and keeps the playback speed.
- The session is shared, so a rewind affects every browser.
  For this reason, **Save point** and **Rewind** have no keyboard shortcuts.
- The server keeps at most 8 save points in memory.
  At the limit, a new save point removes the oldest one.
- A server restart clears all save points, also with `-state`.
  A reset, the traffic demo, and a project apply keep them.
- Demand changes count as project changes.
  A rewind to a save point from before a demand change restores and saves the earlier demand settings.
  The button then reads **Rewind + project**.
  The notice after the rewind adds **Project settings restored.**

### Keyboard shortcuts

| Key | Action |
| --- | --- |
| **Enter** | Submit an order |
| **Tab** | Select the next pod |
| **Shift+Tab** | Select the previous pod |
| **Space** | Pause or resume |
| **Shift+R** | Reset, with a second press within 3 s |
| **S** | Change speed |
| **F** | Toggle pod following |

The simulation gets the keyboard focus when the page opens and after a click on **Download debug state**.
After a click outside the simulation, click the simulation to use the keyboard shortcuts again.
While the connection works and the simulation has no keyboard focus, the line below the panels shows **Click the simulation to use keyboard shortcuts** in amber.

### Debug capture

Select **Download debug state** above the simulation to save a timestamped JSON capture of the current server state.
The capture includes the network, pod positions and routes, queues, berth reservations, and demand settings.
The download does not pause the run.
Share this file when you report congestion or other unexpected behavior.
It is a diagnostic capture, not a reloadable project or save point.

### Traffic demo

To start the demo, select **Edit scenario**, open **Traffic demo**, then select **Start traffic demo**.
The supplied traffic demo requires the unchanged example network and fleet.

The demo starts four pods: pod 01 at Harbor, pod 02 at Garden, and pods 03 and 04 in Parking.

1. Pod 01 leaves Harbor for Market.
2. A supplied trigger starts pod 02 from Garden when pod 01 reaches a defined point on the bypass.
   The same trigger adds Harbor-to-Garden and Garden-to-Harbor orders, which bring the parked pods into service.
3. Both pods contend at the merge, then arrive at Market's single berth.
   The second arrival waits on the station inlet while the first pod clears its berth.
4. After two journeys finish, four more orders join the queue: Market to Harbor, Market to Garden, Harbor to Market, and Garden to Market.
   Normal dispatch and traffic rules handle these orders.
5. The demo ends after eight passenger journeys and all empty moves finish.

Use 8x speed to see the experiment in about 41 seconds, or slow playback to inspect a queue.

Starting the demo resets the current run and disables automatic demand.
The demo also disables manual requests until it ends.
The four pods remain available after it ends.
**Reset** stops the demo and restores the configured fleet.

### Passenger demand

Open **Demand** to select the rate, traffic pattern, and seed, then select **Start demand**.
Each change in the **Demand** panel saves to the project at once, and the panel shows **Changes save to the project**.

**Rate** cycles through 1, 2, 4, 8, 12, 20, 30, and 60 orders per simulated minute, then starts again at 1.
A project rate that is not in this list, for example 7, shows until the first click.
That click selects the next higher rate in the list, or 1 for a rate above 60.
Rates use simulated minutes, so 8x playback generates orders eight times faster in wall time.

Each click on **Seed** adds 1 to the seed.
Arrivals have equal time intervals.
The seed determines the station choices.
The same seed, settings, initial state, and manual actions produce the same run.

The traffic patterns:

- Balanced traffic chooses among all passenger stations.
- Destination traffic sends passengers from the other passenger stations to one station.
  When you select this pattern in the **Demand** panel, it uses the current **To** station.
  The pattern label shows that station, for example **Market-bound**.
- Projects can also include weighted origin-destination profiles with named time bands.
  The London preset includes eight TfL bands and selects AM peak by default.
  For a profile, the pattern label shows the band ID first and then the profile ID, for example **am-peak / tfl-numbat-2019-midweek**.
  The selected band remains active until the demand settings change.

Starting demand or changing enabled settings restarts the stream and its counters.
Pause stops both movement and arrivals.
Reset restores the configured demand.
The traffic demo disables demand.
Demand stays off after the demo until you select **Start demand**, or until a reset restores the configured demand.

The queue holds up to 200 pending orders.
A full queue skips generated arrivals and rejects new manual orders.
Skipped arrivals appear in the Demand panel and do not accumulate for a later burst.
If the simulation rejects a generated order, the Demand panel shows the last error in amber.

The panel also shows if redistribution is on, the number of redistribution moves, and the distance that pods traveled with no passenger.
For example, the panel shows **Redistribution: on / 3 moves / 15.3 km empty**.

## Scenario editor

Select **Edit scenario** above the simulation to open the editor.
The draft stays local until you select **Pause and apply**.

### Edit the network

- Create stations and explicit junctions, then connect their nodes with directed guideways.
- Select paired lanes to add both directions.
  Crossing lines do not create a junction.
- Select a guideway to adjust its curve and speed in km/h.
- Set the station berth capacity and place initial pods in free berths.
- Drag a station to move it.
  The drag also moves the nodes that only its station lanes use, such as a berth chain.
- **Delete station and connections** also removes these nodes.
  It also removes each demand flow to or from the station in the OD profiles.
  When it removes flows, a notice gives their number, for example **Station deleted.
  12 demand flows removed.**
  Undo restores the station and its demand flows.
- Set the passenger generation option, rate, pattern, destination, OD profile, time band, same-destination party limit, seed, and redistribution option.
- Use undo and redo for draft changes.
- Drag empty space to pan, and use the wheel to zoom.
  Press Escape to cancel drawing or moving an item.
- Select **Fit network** to show the whole network, also a large network such as London.
- Junction ID labels show at 0.5 screen pixels per meter or more.
  The selected junction and the start node of a new guideway always show their ID label.
  That label has a font size of 9 screen pixels or more.
- Import a PNG or JPEG background.
  Select **Calibrate scale**, select two points on the image, enter their distance in meters, then select **Set scale**.
- Export JSON to save the scenario and optional background.
  Import JSON to restore a draft.

### Apply a draft

Applying a valid draft resets the shared simulation and leaves it paused.
If the apply fails after the editor paused the simulation, the editor resumes it.
A simulation that was paused before the apply stays paused.
The editor does not resume a simulation that restarted after the pause, for example after a project apply from another browser.

The editor applies a draft only to the project revision that it loaded.
A project apply or a demand change from any other page makes a new revision.
The apply then fails with a conflict, and the editor keeps the draft.

A rewind to a save point from before a project apply or a demand change restores that project.
The rewind also rewrites the `-project` file.
An open draft then gets an apply conflict.
Reload the page to get the restored project.
Export the draft before you reload a newer server project.

For other failures, the editor shows the reason from the server, for example a project file that the server cannot save.
If the server restarted with a new session, the open editor cannot apply the draft.
Export the draft, reload the page, then import the draft.

### Project files and validation

Project files save the design and settings, not the exact running state.
Save points are exact, but they are in server memory only.
They end when the server stops, also with `-state`.

An import does not replace the draft when the file is malformed, unsupported, or fails validation.
Validation checks routes between passenger stations, berth routes, pod placement, resource IDs, and the 24-meter minimum lane length.
A berth route goes from the station entry to the berth, or from the berth to the station exit.
It can use a chain of lanes, as in the London stations.
It cannot pass through the entry, exit, or berth node of a station.
Validation also limits a project to 4 MiB of compact JSON.
The editor sends the project in one command, and the server accepts a command of at most 2 MiB.

Two editor checks give warnings: a junction with no lanes, and a network section that no lane connects to the other nodes.
The server accepts a project with these warnings, so a warning does not block an apply or an import.
A passenger station in a separate section still gives an error, because the other passenger stations cannot reach it.

If the server project fails these checks, the editor still loads it as the draft.
The **Checks** section lists the errors first, then the warnings.
When the draft has only warnings, the summary tells you that the scenario is ready to apply and gives the number of warnings.

## Command-line tools

### Demand and policy comparisons

Redistribution is off by default.
Passenger assignments take priority over empty positioning.
Before a pod moves, redistribution reserves a free destination berth.
A cooldown limits repeated moves.
A remote reservation yields to passenger traffic until the empty pod enters the final admitted block.
Admitted track and physical berth ownership remain protected.
Demand weights forecast pickup locations.
This policy can increase waiting or empty travel when demand differs from the forecast.

Run the same seeded demand schedule with redistribution off and on:

```sh
mise run compare -- -seed 7 -duration 10m -request-every 60s
```

The report includes:

- Demand throughput, backlog, drain time, fleet use, and pickup wait.
- Completed and remaining journeys, and skipped requests.
- Peak queues and berth use at the focus station.
- Passenger and empty travel, loaded-distance percentage, and positioning moves.

The default comparison uses a Market-heavy pickup forecast and identical initial fleets.
Schedule IDs identify the identical requests used for each off/on pair.
Pending requests contribute their elapsed wait at the end of the measurement window.
The synthetic patterns are balanced, destination, hotspot, bursty-hotspot, and hub-burst.

| Flag | Effect |
| --- | --- |
| `-project scenario.json` | Test another network. |
| `-patterns all -seeds 1,2,3 -loads 30s,45s,60s` | Run a paired matrix. |
| `-format json` or `-format csv` | Machine-readable results. |
| `-output` | Write the report to a file. |
| `-pattern profile -bands all` | Run the origin-destination bands of a project demand profile. |
| `-focus` | Select the station that the destination, hotspot, bursty-hotspot, and hub-burst patterns favor. |
| `-arrivals-for` | Stop new requests before the measurement ends. |
| `-stop-when-drained` | Stop an arm after all accepted requests complete. |
| `-adaptive-limit` | With `-stop-when-drained`, find capacity limits with fewer arms. |
| `-past-limit` | The number of rates that an adaptive group runs after its limit. Default 1. |
| `-workers` | Run independent arms concurrently. Reports keep their deterministic order. |
| `-burst-size` | Group burst-pattern requests at the same simulated time. |
| `-sharing-limits 1,4` | Compare same-destination party limits. |
| `-routing-policies free-flow,congestion` | The experimental route-cost A/B. |
| `-redistribution-policies off` | Hold redistribution fixed. |
| `-wait-rules current,strict,none` | Compare the finishing-pod wait rules from the dispatch section. |
| `-queue-limit` | Change the limit of 200 pending requests. At the limit, the comparison skips new arrivals. |

The report has a `wait_rule` column or JSON field only when you give `-wait-rules`.

With `-adaptive-limit`, arms that differ only in offered rate and seed form a group.
Each group runs its rates from the lowest offered rate, with all seeds of a rate together.
After the first rate at which a seed does not drain, the group runs `-past-limit` more rates and skips the rest.
The report omits the skipped arms.
The other rows are identical to the rows of a full run.

### Generated scenarios

Create a repeatable server scenario:

```sh
mise run scenario -- -preset scale100 -output /tmp/podsim-scale100.json
mise run serve -- -project /tmp/podsim-scale100.json
```

Presets include `small`, `busy`, `parking-constrained`, `rail-hub`, `scale100`, and `london`.
Generated files contain raw server settings.
The editor can export the loaded scenario with optional local background data.
See [qualification results](docs/qualification.md) for safety checks, performance measurements, and redistribution limits.

#### rail-hub

The rail-hub preset has six passenger stations, 30 pods, six berths per passenger station, and 12 parking berths with 12 initial reserve pods.
It supports the recorded finite-arrival station-capacity experiment:

```sh
mise run scenario -- -preset rail-hub -output /tmp/podsim-rail-hub.json
mise run compare -- -project /tmp/podsim-rail-hub.json -pattern hub-burst -duration 30m -arrivals-for 5m -request-every 5s -burst-size 12 -seeds 1,2,3,4,5
```

#### scale100

The scale preset uses a connected grid with alternate routes and explicit junctions.
It has 19 passenger stations, one parking station, 138 berths, and 100 pods.
Each station has separate road connections for arrival and departure, 600 meters apart.
Parallel arrival and departure lanes serve successive berth rows.
Parking extends outside the road grid.
This geometry separates incoming and outgoing traffic and shortens the parking entrance conflict sections.
Demand starts disabled.
Configure and start it from the Demand panel.

#### london

The [London qualification network](docs/london.md) uses TfL station locations and topology, real station names, directed twin guideways, off-line berths, and three Parking facilities.
Station lanes identify approach, entry, berth access, through, departure, and exit maneuvers.
The pod inspector shows the current maneuver in **Station phase** and the station name on the line below it.
Projects without this optional lane metadata still load.
The simulator infers through, berth access, and departure roles from the station paths.

## Scope and model

### Time, geometry, and routing

The Go core uses fixed 60 Hz steps and world coordinates in meters.
Routing chooses the shortest free-flow travel time on directed lanes.
Equal-cost routes use scenario order for deterministic results.

Lanes can be straight or quadratic curves.
The simulator and browser measure each curve along the same sampled path.
Each lane has an explicit speed limit.
Bends do not impose extra speed limits.

Automatic demand is optional.
It starts disabled unless the loaded project enables it.
With `-project`, **Start demand** saves this setting in the file, so demand starts again after a server restart.

### Passenger service and dispatch

#### Parties

One party contains one passenger.
By default, each party uses one pod.
An optional limit of two to eight lets unassigned parties join a pod that is still boarding at the same origin for the same destination.
Sharing does not wait for more parties or add stops.

#### Dispatch order

Passengers request travel between stations independently of pod selection.
Dispatch considers requests in submission order.
An idle local pod serves the oldest waiting passenger.
Otherwise, dispatch compares idle pods and empty pods that can divert.
An empty pod can divert when it goes to parking, when it moves for redistribution, or when dispatch released it from a pickup.
Dispatch uses the estimated pickup time, then the pod ID, as the tie-breaker.
An idle pod at the pickup station has an estimate of zero, because it boards at its own berth.

#### Wait rules

Dispatch can wait for a busy pod if it should reach the pickup at least two seconds earlier.
The estimate includes travel, acceleration, braking, boarding, and unloading.
It does not predict traffic delays.
Dispatch waits for at most 30 simulated seconds before it uses an available pod.

| Rule | Behavior |
| --- | --- |
| `current` | Waits as described above. The server always uses this rule. |
| `strict` | Waits only when the busy pod can finish and reach the pickup before the 30 seconds end. Compare command only. |
| `none` | Never waits for a busy pod. Compare command only. |

#### Pickup

Assigned pickup pods can depart while the berth is occupied and queue on its approach.
They claim the berth through local admission, not a remote reservation.
Passengers board only at a berth.

If pickup pods arrive out of order, the first available pod takes the oldest passenger at that station.
The other pod retains a pickup at the same station for the later order.

#### Released pods

A pod that becomes idle at the pickup station can take a trip from a pod that is still on its way to the pickup.
Dispatch then releases the other pod, and the released pod can divert for a pickup at once.
A later trip in the same dispatch pass or in a later pass can take it.
If no trip takes it in the same pass, it goes to the nearest free berth.

A free berth has no pod, no claim, and no other pod or waiting trip that goes to it.
The pod keeps its destination when that berth is the nearest free berth.
When two berths have the same route cost, the current destination berth wins.
After it come the other berths of its station, then the berths of the other stations in network order.
The pod reserves the chosen berth, as a pod on its way to parking does.
A pod that has not left its berth can choose that berth and stays there.
A moving pod cannot choose its origin berth until it is at clearance distance from it.

A restore that removes the pod binding of a waiting trip also releases the pod on its way to that pickup.
The next dispatch pass sends that pod to the nearest free berth if no trip takes it.
When a released pod gives its berth claim to a passenger pod, it goes to the nearest free berth at once.

#### Empty travel

An empty pod heading to parking can divert for a pickup.
It preserves all committed track and releases its unused parking claim.
A pod already committed to the parking inlet finishes that maneuver before returning to service.
A released pod also keeps all committed track and finishes a committed inlet first.
Empty pickup travel retains the original request ID and does not count as a passenger journey.
The fixed demo can still submit journeys directly to specific pods.

### Track admission and junctions

Pods accelerate and brake within their admitted track distance.
They extend reservations to cover the stopping distance at the lane speed plus two simulation ticks of travel.
On clear track, this allows steady cruising at 14 m/s across block boundaries.

The local controller divides lanes into exclusive blocks of up to 30 meters.
It retains trailing blocks until a four-meter pod and an eight-meter gap have cleared.

Each junction has one conflict resource for nearby sections of its incident lanes.
The controller derives these sections from the same geometry used for movement, including the 12-meter clearance.
It acquires each continuous conflict section together, including the downstream cells needed to cross lane endpoints.
It releases the conflict resource after the pod reaches the end of that section.

Admission follows these rules:

- The oldest local admission request wins.
- Pod ID breaks a tie.
- Admission uses the state before movement.
- Released resources become available on the next tick.

The block model is conservative.
It does not model continuous car-following or optimized junction capacity.
The traffic model requires lanes at least 24 meters long.

### Stations and parking

#### Berth choice

Passenger journeys route to the station entry without a berth assignment.
The controller chooses the least-assigned reachable berth when the pod enters the final station-access lane.
The controller can change this choice before it reserves a berth branch.
A passenger or pickup pod can choose a free alternate berth before it reserves the next station branch.
A route change preserves all admitted track.

A pod reserves its berth before it enters the final inlet block.
It keeps the berth through unloading and idle time, until its departure clears the resource.
Other pods can queue on the inlet while through traffic uses the separate through lane.

Station maneuver roles describe this existing movement and reservation behavior.
They do not add a second station controller or change admission priority.
The controller makes local reservations, not a whole-journey timetable.

#### Station layout

Station entry, berth, and exit connections have stable identities.
Passenger and parking stations can have multiple berths.
Berths can connect through intermediate arrival and departure lanes, separate from through traffic.
Station-local paths cannot cross another berth or station.
Each station retains a direct entry-to-exit through lane.

#### Idle pods

An idle empty pod clears its berth when it blocks a passenger arrival, an assigned pickup pod, or an empty relocation.
It also clears its berth for a pod that must pass through that berth to leave the station.
It first reserves a free reachable parking berth and retains its origin until physical clearance.
If parking is full or unreachable, it reserves reachable passenger space instead.
It prefers local space that no request targets.
The pod shows **No parking available** only when no reachable physical space exists.

#### Empty moves and parking

Empty relocations yield unadmitted destination claims when a local passenger or pickup needs the same berth.
Physical ownership and admitted destination resources remain protected.
Empty moves have no boarding or unloading delay and do not count as passenger journeys.
Optional redistribution moves idle empty pods toward configured demand before requests arrive.

Parking serves no passengers.
Parked pods return to service automatically when assigned to a pickup request.
After the demo, request a trip from Harbor or Garden to see an available pod return for pickup.

The tests establish progress for feasible supplied scenarios, not for every saturated network.

### Server and browser

#### Shared state

One server owns the simulation clock, commands, and demand settings.
Browsers poll dynamic state frames and show the connection status.
They fetch the network topology on connection and after a project revision changes.
Controls wait for server confirmation.
During the wait, the line below the panels shows **Shared session / waiting for command confirmation**.
**From** and **To** send no command, so you can change them during the wait.

Pod selection, origin, destination, and the open inspection panel stay local to each browser.
Background images stay in the editor and the exported project file.
The shared simulation receives the network geometry and settings.

#### Motion

The map buffers 150 ms of snapshots and interpolates movement along lanes between updates.
Controls and order status use the latest server state.
Pauses, resets, rewinds, and long connection gaps clear buffered motion.
Rendering never predicts movement beyond the latest received position.

#### Compression and the WASM module

The server uses gzip for snapshots and browser assets when the client supports it.
Range responses remain uncompressed.

The build keeps only `podsim.wasm.gz`, which it compresses at the maximum level one time.
The server sends these bytes to browsers that accept gzip.
For a client without gzip or for a Range request, the server decompresses the module on the first request and keeps it in memory.
Byte ranges then use the offsets of the uncompressed module.
The build writes `podsim.wasm.gz` to a temporary file and then renames it, so a running server never reads a partial module.

A `-dir` directory from an older build with only `podsim.wasm` still works.
The server then compresses that file for each gzip request.
If a `-dir` directory has a `podsim.wasm` that is newer than `podsim.wasm.gz`, the server uses `podsim.wasm`.

#### Connection status

Until the first state frame arrives, the map shows **Connecting to server...** and no network.
A lost connection disables commands.
While the connection is lost, the map is dimmed and an amber banner shows **Connection lost.
Showing state from N s ago.**
N is the time in seconds since the last state frame.
Reconnection restores the current shared state.

When the server restarts with different browser files, open browser pages reload by themselves.
The desktop client shows a message instead.
Restart it to load the new version.

The line below **From** and **To** shows these notices for 3 s:

| Event | Notice |
| --- | --- |
| A server restart with the same build | **Server restarted. Save points cleared.** |
| Another browser resets or rewinds the session, starts the traffic demo, or applies a project | **Another browser reset or rewound the session.** |

A reset or a rewind from this browser shows its own notice instead.

#### Command receipts

The server deduplicates command retries by client and sequence.
The server keeps command receipts for at most 1,024 browser page loads per session.
Only a page that sends a command uses a receipt.
If a new page reports the session client limit, restart the server.

If the server made its final save at a graceful shutdown and restores the session with the `physical` or `logical` tier, the session continues.
Then a retry of a command from before the restart gets the `expired_command` error, and the server does not apply the command again.
The pages from before the restart also stay in the count of 1,024 page loads.

The session does not continue after a restart at the client limit or after other restarts.
Then the retry gets the `session_changed` error, and the count starts again.
See [server restarts](docs/protocol.md#server-restarts).

See [the client protocol](docs/protocol.md) for the API, save point commands, payload measurements, and the Protobuf evaluation.
See [the project brief](PROJECT_BRIEF.md) for the wider scope and research.

## Code and validation

| Path | Purpose |
| --- | --- |
| `internal/sim` | Network, routing, requests, pod movement, state export and restore, and deterministic tests. |
| `internal/observe` | Station berth and queue metrics for the view and comparison reports. |
| `internal/project` | Versioned scenario settings, validation, and detached copies. |
| `internal/scenarios` | Deterministic scenario presets, including `scale100` and `london`, and qualification tests. |
| `internal/session` | Shared clock, command validation, save points, saved session state, HTTP API, and repeatable demand. |
| `internal/statestore` | Saved session state in a `file://` blob bucket, for the server only. |
| `internal/remote` | Snapshot polling, motion buffering, command retries, and connection state. |
| `internal/view` | Ebitengine rendering and input against copied snapshots. |
| `internal/telemetry` | Optional OTLP traces, HTTP metrics, runtime metrics, session gauges, and session state metrics. |
| `internal/cmd/buildweb` | Generated browser files and gzip WASM file. |
| `cmd/podsim` | Desktop and WASM entry point. |
| `cmd/serve` | Shared session, saved session state, browser assets, health checks, and diagnostics. |
| `cmd/compare` | Reproducible policy comparisons. |
| `cmd/scenario` | Generated scenario files. |
| `web` | Browser loader and scenario editor. |

### Tasks

```sh
mise run test
mise run check
```

| Task | What it runs |
| --- | --- |
| `mise run check` | Workflow validation, Markdown checks, race tests, editor tests, vet, lint, vulnerability checks, the native and browser builds, and the embedded server tests. |
| `mise run format` | Formats the Go sources and the Markdown files. |
| `mise run test:web` | Only the editor tests. |
| `mise run qualify` | The `internal/scenarios` qualification tests for scale, safety, and repeatability. |
| `mise run benchmark` | 6,000 simulation steps on the 100-pod ring fixture, not on the current `scale100` mesh. |

`mise.toml` tracks Go 1.27 and major versions for the other development tools.
`mise.lock` records the resolved tool downloads.

Some editor tests use Go to generate the `scale100` and `london` projects.
If Go is not on `PATH`, `node --test web/editor_test.cjs` skips these tests and gives the reason.
The `test:web` task sets `PODSIM_REQUIRE_GO=1`, so a missing Go makes the task and `mise run check` fail.

### Continuous integration

GitHub Actions runs the same check on pull requests and pushes to `main`.
The workflow also supports a manual trigger.
New pull-request updates cancel older runs.
Each `main` push keeps its own run.
The workflow uses major-version action tags and installs tools from `mise.lock`.
Go module, build, and lint analysis caches use job-specific keys and refresh after successful runs.

### Lint and package boundaries

The lint configuration adds resource, context, and security linters to the standard golangci-lint set.
It also checks these package boundaries:

- `internal/sim` can import only the standard library.
- `internal/session`, `internal/project`, and `cmd/serve` cannot import the renderer or browser APIs.
- Packages in the browser build cannot import cloud storage packages or `internal/statestore`.
  Lint also lists every package that the WASM build links, and fails if the list has one of these packages.

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
