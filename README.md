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
See [dependency review notes](docs/dependencies.md) for the recorded gRPC advisory discrepancy and state-store lint annotations.
The build creates static files in `dist/`, including the matching Go WebAssembly runtime and the gzip WASM file `podsim.wasm.gz`.

The network view fills the browser window and renders at the display pixel density.
On a very wide screen at a high pixel density, the lines of a small network can show without antialiasing.
This occurs when the antialiasing image, which is twice as wide as the screen, would not fit in the largest GPU texture, for example in a 2560 by 1440 window at a device pixel ratio of 2 on a GPU with an 8192 pixel limit.
When the full window at the display pixel density would not fit in the largest GPU texture, the view renders at a lower pixel density and scales the image to the window.
The browser can also make the WebGL drawing buffer smaller than the window at the display pixel density.
For example, Chrome gives a 7436 by 4461 drawing buffer to a 4000 by 2400 window at a device pixel ratio of 2, because it keeps the drawing buffer at about 33.2 million pixels or less.
Chrome keeps each side within the GPU limit before it applies the area limit.
Thus, on a GPU with an 8192 pixel limit, a 4200 by 2400 window at a device pixel ratio of 2 gets a 7524 by 4409 drawing buffer.
That drawing buffer does not have the proportions of the window.
The view then renders at the size of the drawing buffer, so that it fills the window without black bars.
The view also corrects the cursor and touch positions for the smaller drawing buffer, so that clicks and taps hit the controls.
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

Validation limits each project to 10 MiB in this compact form, also after a demand change.
This limit lets the server read the file at the next start.

The browser export wraps the `project` object as `scenario` and can also contain a background image.
**Import JSON** accepts the browser export and the server file, such as the output of `mise run scenario`.
When the file leaves out an optional setting, such as the party limit, the shared ride mode, or the platoon limit, the import uses the default value.
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
Open **Connection diagnostics** at the bottom right of the simulation to inspect its stream.
The panel shows received payload rate, applied updates, mean receive-to-apply time, state and heartbeat age, and reconnect attempts.
An HTTP round-trip probe runs every five seconds while the panel is visible.
Rates exclude HTTP assets and network headers.
Apply time excludes rendering.
State age increases normally while paused.
One-way delay and socket backlog are not measured.

The browser receives shared gzip JSON state deltas over WebSocket.

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
  The waiting reason names the blocking pod by its fleet number, for example **Pod ahead / pod 02**.
- With [faults](#faults), the waiting reason can also be **Fault braking**, **Fault stopped**, **Blocked by incident**, or **No forward route**.
- When the project has the fault marker, the inspector shows a **Fault** button to the right of the activity.
  **Fault** starts a fault on the selected pod, with no end.
  On a faulted pod, the button shows **Clear fault** and ends that fault.
  The button is not available while the connection is down or a command waits for its reply.
  The server refuses a pod that it does not support, and the message line shows the error.
- When the project has the [emergency marker](docs/operations.md#emergencies), the inspector shows an **Emergency** button on a pod that carries passengers and has no emergency.
  **Emergency** starts an emergency for the first party aboard the pod.

### Pod colors and shapes

- Pod colors show their purpose: idle, pickup, passenger service, parking, redistribution, or other empty travel.
- The colors stay different with protanopia, deuteranopia, and tritanopia.
- An idle pod and a pod in passenger service are filled discs.
  A pod that moves empty is a ring.
  An idle pod is white, so it looks different from the gray node dots and berth rings.
- A berth ring shows the color of a moving or busy pod in the berth.
  A berth with an idle pod keeps the gray ring.
  The pod number shows above and to the right of the pod.
  The next station code appears below it when known, including the pickup station on a pickup run.
  Idle pods show only their number.
- An amber ring around a pod marks a waiting pod.
  A white ring marks the selected pod.
- A white line joins each pod in a [virtual platoon](#virtual-platoons) to the pod ahead of it.
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
- On a touch screen, a tap acts as a click, also on the buttons and the stations in the station row.
  Drag one finger on the map to pan.
  Pinch with two fingers on the map to zoom at their midpoint, and move the two fingers to pan.
  A drag that starts outside the map does nothing.
  A touch that the browser cancels, for example when you switch to a different tab or app, is not a tap.
- The scale bar below the lower-left corner of the map shows a round distance of 1, 2, or 5 times a power of ten, for example 500 m, 1 km, or 2 km.
- Map navigation stays local to your browser.
- A click, tap, or drag on the map, or **Fit**, stops following.
- **Reset** and **Rewind** keep the map view when the network does not change.
- Zoom in to see individual berths in crowded stations.
  Until then, such a station shows one marker.
  The map does not show the idle pods in the station, except the selected pod.

### Station labels

- Overview labels show occupied berths and nonzero entrance and exit queues.
- Expanded labels show occupied, reserved-empty, and free berths.
  These counts sum to the station capacity.
  Where the full label covers a lane, a short label can show.
  It has the station name and the occupied berths.
  When pods stop at the entrance or the exit, it also has the **In** and **Out** queue line of the overview label.
  The short label keeps its size when pods arrive, depart, or stop, so it does not move.
  The full label shows when it covers only a short length of lanes, for example at a corner.
  Otherwise, the label that covers less of the lanes shows.
  Zoom in, and the full label shows when it has room.
  At the largest zoom, the map shows only the full label.
- Expanded labels and berth numbers go on the side of the berths away from the station siding.
  They go on a different side when that side covers a much shorter length of lanes.
  They stay inside the map, off the berth rings, and off other expanded labels and berth numbers.
  A label without a free place does not show until you zoom or pan.
  Each text line has a dark backing, so a lane under the text does not make it hard to read.
  On a network with more than 30 stations, a pod label that covers an overview label or an expanded label does not show, except the label of the selected pod.
- Expanded entrance labels show stopped and approaching pods.
  Exit labels show stopped departing pods.
- Queue counts include dedicated access spurs, but exclude general road traffic.
- A reserved-empty berth can still have a departing pod that holds its clearance resource.
- Berth labels distinguish a parked pod from an admitted arrival.
- Map labels, such as station names and pod labels, have a font size of 10 CSS pixels or more.
  In a window smaller than 1100 by 728 CSS pixels, the rest of the view becomes smaller, but the map labels keep their size.

On a network with more than 30 stations, the map can omit an overview label where it covers another label or a station marker.
On such a network, only the selected pod has a map label until you zoom in to four times the **Fit** scale.
See [the London qualification network](docs/london.md) for these rules.

### Order a journey

- Under **ORDER A JOURNEY**, select **From** and **To**, then **Order [Enter]**.
  Pod selection affects inspection only.
- Station choices are alphabetical by name.
  Type a name or code in the **From** and **To** fields to filter them.
  An exact match takes priority.
  Otherwise, select a match or type until only one station matches.
  **Tab** moves between browser fields.
  **Enter** in **To** orders the journey when both fields resolve.
  Typing in a field does not activate playback shortcuts.
  London uses the three-letter NaPTAN station suffix, such as **CHX** for Charing Cross.
  **BPS** and **NEL** identify Battersea Power Station and Nine Elms.
  Full station IDs also work.
  Custom networks can use station names or full IDs.
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
  Each party aboard a pod is one active order, also in a shared ride.
- Boarding takes three simulated seconds.
  Unloading takes two.

### Statistics

- Pickup wait statistics show the average and maximum seconds since reset.
  Pending orders contribute their elapsed wait.
- Wait ends when boarding starts, so it includes empty-pod travel to pickup.
- Fleet use shows the percentage of pods with assigned or active work and the percentage currently in passenger service.
- The run status beside the title shows the playback speed, the simulated time, and the number of completed journeys.
  While the run is paused, the run status is amber and starts with **PAUSED**.
  The number of passenger stations and pods shows below the run status.

### Playback

- Use **Pause**, **Resume**, **Reset**, and **Speed** to control playback.
- Left-click **Speed** or press **S** to cycle through 1x, 2x, 5x, 15x, and 60x.
  Right-click **Speed** to step backward through these choices.
  The server tracks five rolling one-second buckets.
  If at least two complete buckets achieve less than 90% of selected speed, it selects the next lower speed.
  A notice explains the reduction.
  Each reduction starts a new five-second measurement window.
  Pauses and project loads do not count toward this window.
  The server never increases speed automatically.
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

While the simulation shows, it gets the keyboard focus when the page opens and when it shows again after the editor.
The simulation uses Tab and Shift+Tab to select pods, so the keyboard cannot move the focus out of the simulation.
For keyboard users, the page has controls before the simulation and the editor.
While the simulation shows, they are **Edit scenario** and **Download debug state**.
While the editor shows, they are **Simulation** and **Download debug state**.
These controls are hidden until they get the keyboard focus.
To get to them, move the focus to the start of the page, for example from the browser address bar, and press Tab.
While the simulation starts, and after a server change to a simulation that uses other page messages, these controls show in the top left corner.
After you select **Simulation** or **← Simulation** with the keyboard, **Edit scenario** gets the focus in its place.
After a click outside the simulation, click the simulation to use the keyboard shortcuts again.
While the connection works, no command waits for the server, and the simulation has no keyboard focus, the line below the panels shows **Click the simulation to use keyboard shortcuts** in amber.

### Debug capture

Select **Download debug state** in the header of the simulation to save a timestamped JSON capture of the current server state.
The line below the journey controls then shows the result.
While the page shows its own **Download debug state** control in the top left corner, the result shows next to that control.
The result of a successful download goes after 3 s.
A failure shows in amber.
In the line below the journey controls, it stays until your next action.
The result of the next capture also replaces it.
Next to the control in the top left corner, it stays until the next capture.
The header shows **Download debug state** and **Edit scenario** only in the browser page with the editor.
The desktop client and `game.html` as a separate page do not show them.
The capture includes the network, pod positions and routes, queues, berth reservations, and demand settings.
The download does not pause the run.
Share this file when you report congestion or other unexpected behavior.
It is a diagnostic capture, not a reloadable project or save point.

### Traffic demo

To start the demo, open **Demand**, then select **Start traffic demo** two times within 3 s.
The demo resets the shared session, so the first press does not start it.
The first press shows **Select Start traffic demo again within 3 s to reset the shared session and start the demo.**
The button is not available while the connection is lost, while a command waits for the server, or while the demo runs.

The supplied traffic demo requires the unchanged example network and fleet.
The **Demand** panel shows **Example scenario only** beside the button.
For a different scenario, the server rejects the demo, and the line below **From** and **To** shows the reason.

The demo starts four pods: pod 01 at Harbor, pod 02 at Garden, and pods 03 and 04 in Parking.

1. Pod 01 leaves Harbor for Market.
2. A supplied trigger starts pod 02 from Garden when pod 01 reaches a defined point on the bypass.
   The same trigger adds Harbor-to-Garden and Garden-to-Harbor orders, which bring the parked pods into service.
3. Both pods contend at the merge, then arrive at Market's single berth.
   The second arrival waits on the station inlet while the first pod clears its berth.
4. After two journeys finish, four more orders join the queue: Market to Harbor, Market to Garden, Harbor to Market, and Garden to Market.
   Normal dispatch and traffic rules handle these orders.
5. The demo ends after eight passenger journeys and all empty moves finish.

Use 15x speed to see the experiment in about 22 seconds if the server keeps up, or select a lower speed to inspect a queue.

Starting the demo resets the current run and disables automatic demand.
The demo uses the shared ride and platoon settings of the project, so with sharing on, a party can join a pod in the demo.
The demo also disables manual requests until it ends.
The four pods remain available after it ends.
**Reset** stops the demo and restores the configured fleet.

### Passenger demand

Open **Demand** to select the rate, traffic pattern, and seed, then select **Start demand**.
Each change in the **Demand** panel saves to the project at once, and the panel shows **Changes save to the project**.
**Start traffic demo** at the bottom of the panel does not change the project.
See [Traffic demo](#traffic-demo).

**Rate** cycles through 1, 2, 4, 8, 12, 20, 30, and 60 orders per simulated minute, then starts again at 1.
A project rate that is not in this list, for example 7, shows until the first click.
That click selects the next higher rate in the list, or 1 for a rate above 60.
Rates use simulated minutes, so 15x playback generates orders fifteen times faster in wall time when the server keeps up.

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
  LondonCentral includes eight TfL bands, while LondonFull offers its six populated 2024 bands.
  Both select AM peak by default.
  For a profile, the pattern label shows the band ID first and then the profile ID, for example **am-peak / tfl-numbat-2019-midweek**.
  The selected band remains active until the demand settings change.
- Daily profiles repeat authored bands across simulated days.
  Gaps and zero-rate bands generate no traffic.
  See [repeating daily demand](docs/daily-demand.md) for rates, authoring, and restart behavior.

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
Redistribution moves pods only at a low demand rate, so the panel can show 0 moves while it is on.
See [Demand and policy comparisons](#demand-and-policy-comparisons).

## Scenario editor

Select **Edit scenario** in the header of the simulation to open the editor.
The editor opens in the same page.
The simulation stays loaded behind it and continues to read the shared state.
It does not draw while the editor shows, so it does not slow the editor.
To return to the simulation, select **← Simulation** in the editor, or use the browser Back button.
The simulation then shows the same map view and the same selected pod as before.
The editor keeps its state while the simulation shows.
The editor loads the live scenario only the first time that it opens in the page.
Until it has the live scenario and the stored background, the editor ignores input, except **← Simulation**.
If a part of the load fails, the editor shows an error notice and then accepts input.
If you change the demand in the simulation after that, **Pause and apply** fails with a conflict.
The **Apply conflict** bar then lets you load the live scenario.
See [Apply a draft](#apply-a-draft).
To open the page with the editor, add `#editor` to the address, for example <http://127.0.0.1:8080/#editor>.
You can also open `editor.html` as a separate page.
There, **← Simulation** opens the simulation page.
The draft stays local until you select **Pause and apply**.

### Edit the network

- Select a generated London or scale100 station to change its berth pitch, entry/exit spacing, or approach setback.
  Select **Preview in draft** to update the map with one undo step.
  The controls keep the mainline throat fixed and retain the station's nodes, lanes, berths, and fleet placements.
  Unsupported layouts retain manual node editing.
  See [station layout controls](docs/station-layout-editor.md) for limits and validation.
- Create stations and explicit junctions, then connect their nodes with directed guideways.
- Select **Add paired lanes** to add both directions.
  Crossing lines do not create a junction.
- A small chevron at the middle of each guideway shows its direction.
  The chevron has the same size at each zoom.
  A guideway shorter than 24 screen pixels shows its chevron only when you select it or put the pointer on it.
- Two guideways between the same nodes in opposite directions are a pair.
  The map draws each guideway of a pair 4 screen pixels to the right of its direction of travel, so you can see and select each one.
- Select a guideway to adjust its curve and speed in km/h.
- Set the station berth capacity and place initial pods in free berths.
- Select a station and select **Parking station** to make it a parking station.
  A parking station serves no passengers, and idle pods can park in its berths.
  A journey cannot start or end at a parking station, and the demand **Destination** list shows only passenger stations.
  The network needs at least two passenger stations.
- On a station that is not a berth chain, such as a station that the editor made, **Add physical berth** puts the new berth 30 m past the last berth.
  The berth goes on the station axis, across the entry-exit line, on the side of the other berths.
  The berth gets a lane from the station entry and a lane to the station exit.
- On each station, **Remove** on a berth row removes the berth, its node, and the lanes of its node.
  Each lane of the berth node must be a station lane of that station, such as the lanes that **Add physical berth** makes.
  If a road lane or a lane of a different station uses the berth node, the editor does not remove the berth.
  A message then gives the station name, the lanes, and the node.
  Delete these lanes first, then remove the berth.
- An older or hand-written project can have station lanes without a station ID and a role.
  When the editor loads such a project, it gives each of these lanes the station ID and the role that the simulation gives it.
  Thus **Remove** accepts these lanes.
  An export and **Pause and apply** keep the new values.
- A generated London or scale100 station is a berth chain.
  Each berth row has an arrival node, a berth node, and a departure node.
  On a berth chain station, **Add physical berth** adds one more row at the end of the chain.
  The row has three nodes and four lanes: the arrival link, the lane in, the lane out, and the departure link.
  The distance and direction from the last row to the new row are the same as from the row before it to the last row.
  With one row, the distance is from the middle of the entry-exit line to the berth node.
  The new items get the IDs that the scenario generator gives to one more berth.
  The four new lanes are station lanes of that station, also when the lanes of the other rows are not.
  If a new lane crosses another lane or comes nearer than 12 m to it, the editor does not add the berth.
  The check also compares the new lanes with each other, but not two lanes that share a node.
  A message then gives the station name and the two lanes.
  **Remove** on the last row of a chain removes all of the row.
  If another lane uses the arrival or departure node of that row, the editor does not remove the berth.
  A message then gives the station name, the lane, and the node.
- On each station, **Add physical berth** does not add the berth when a node would get more than 64 lanes.
  A message then gives the station name and the node.
  On a station that is not a berth chain, the entry and the exit have a lane for each berth, so this limit applies to the number of berths.
- Drag the station shape to move the station.
  The drag also moves the nodes that only its station lanes use, such as a berth chain.
- **Delete station and connections** also removes these nodes.
  It also removes each demand flow to or from the station in the OD profiles.
  When it removes flows, a notice gives their number, for example **Station deleted.
  12 demand flows removed.**
  Undo restores the station and its demand flows.
- The station shape is a rectangle along the entry-exit line that holds all nodes of the station.
  The entry is a hollow square.
  The exit is a filled triangle that points in the direction of travel.
- Drag an entry, exit, or berth node to move only that node.
  The lanes of the node stay attached to it.
- Select a station and set **Bearing (degrees)** to turn the station around the middle of its entry-exit line.
  The bearing is the direction from the entry to the exit, clockwise from up.
  Up is 0 degrees, and right is 90 degrees.
  The turn also moves the nodes that only its station lanes use.
  Each lane keeps its nodes.
  The project file has no bearing field.
  The editor gets the bearing from the entry and exit positions.
- Set the passenger generation option, rate, pattern, destination, OD profile, time band, party limit, shared ride mode, stop limit, platoon limit, seed, and redistribution option.
  The stop limit shows only for the drop-offs mode.
- Use **Create a park-and-ride profile** for weighted morning departures and evening returns.
  The creator records one undo step and selects daily demand.
  See [repeating daily demand](docs/daily-demand.md).
- Use **Rail arrivals** to set train passenger counts, arrival times, walking delays, and weighted destinations.
  Select its demand pattern to release the fixed plan instead of rate-based traffic.
  See [scheduled rail arrivals](docs/rail-arrivals.md) for bounds, restart behavior, and comparison evidence.
- Use **Rail departures** to set request windows, outbound train times, transfer times, and weighted origins.
  Select **Rail arrivals and departures** to run both plans.
  The demand inspector shows made, missed, unserved, and unresolved connections.
  See [scheduled train connections](docs/rail-connections.md) for bounds and comparison controls.
- Select a guideway to set the pod classes that can use it in **Vehicle classes**.
  A guideway with no class list allows Legacy and Compact pods.
  The Checks section shows the geometry errors that the server reports.
- Use undo and redo for draft changes.
  See [Editor keyboard shortcuts](#editor-keyboard-shortcuts).
- Drag empty space to pan, and use the wheel to zoom.
- When you use a button in the **Selection** section with the keyboard to delete an item, the keyboard focus goes to a control near the deleted item.
  After **Remove** on a berth row, the focus goes to **Remove** on the next row, or on the previous row when you removed the last row.
  When the station has one berth left, its **Remove** button is disabled, so the focus goes to **Add physical berth**.
  After **Delete station and connections**, **Delete guideway**, or **Delete junction and connections**, the focus goes to the map.
  The Delete key does the same when a button in the **Selection** section has the focus.
  After a delete with the pointer, the editor does not move the focus.
- Undo and redo keep the selection when the draft still has the selected item.
  When a button in the **Selection** section has the keyboard focus, the focus stays on that button.
  When the button is **Remove** on a berth row, and the berth is gone or the station has one berth left, the focus moves as after **Remove** on that row.
  When the draft no longer has the selected item, the editor clears the selection, and the focus goes from the **Selection** section to the map.
  Undo and redo do not move the focus when it is not in the **Selection** section.
- Select **Fit network** to show the whole network, also a large network such as London.
- Junction ID labels show at 0.5 screen pixels per meter or more.
  The selected junction and the start node of a new guideway always show their ID label.
  That label has a font size of 9 screen pixels or more.
- Import a PNG or JPEG background.
  The image file must be 8 MiB or smaller.
  The image must be at most 16,384 pixels on each side and at most 67,108,864 pixels in total.
  The editor reads the image size from the PNG or JPEG header before the browser decodes the image.
  Then the browser decodes the image, and the editor checks the decoded size.
  The editor does the same checks for a background in an imported project file and for the stored background.
  If an image fails a check, the editor shows an error notice and does not use the image.
  Select **Calibrate scale**, select two points on the image, enter their distance in meters, then select **Set scale**.
- Select **Import a georeferenced image** to use an image with known bounds.
  See [Georeferenced background](#georeferenced-background).
- Export JSON to save the scenario and optional background.
  Import JSON to restore a draft.
  Like the server, the import matches member names exactly.
  A file that repeats a member name in one object is not valid.
  The export is compact JSON.
  The project file must be 21 MiB or smaller.
  This limit includes the image in base64 form, the server's 10 MiB project limit, and an allowance for other fields.
  It also includes 128 KiB for the image frame and license, rounded up to a whole MiB.
  Thus the editor can import an export with the largest project and the largest background image.
- The editor keeps the images of the undo history in the memory of the tab, up to 128 MiB.
  When a new image makes the images larger than 128 MiB, the editor removes the oldest undo steps and says how many.
  It never removes the present image or the image of **Reset draft**.

### Editor keyboard shortcuts

| Key or action | Result |
| --- | --- |
| **Ctrl+Z** | Undo |
| **Ctrl+Shift+Z** or **Ctrl+Y** | Redo |
| **Delete** or **Backspace** | Delete the selected station, guideway, or junction |
| **Escape** | Cancel a new guideway, the move of an item, or a scale calibration |
| Right-click on the map | Cancel a new guideway |

On macOS, use Command in place of Ctrl.
The editor ignores the undo, redo, and delete keys when the keyboard focus is in an input field or a drop-down list.
Delete and Backspace do the same as the delete buttons in the **Selection** section.
For an entry, exit, or berth node, these keys show a notice and do not delete the node.
Use the station controls to delete these nodes.
After Escape during a move, the item goes back to its position before the move.

### Saved draft

The browser saves the draft in IndexedDB 300 ms after the last change.
The saved draft has the scenario.
It does not have the background image.
See [Stored background](#stored-background).
It also has the project revision that the draft is based on, the session epoch of that revision, and the server start ID of that state.
The browser keeps one saved draft for each server address.
After a successful apply, the browser deletes the saved draft.

When the editor loads, the browser compares the saved draft with the live scenario.
If they are different, the **Saved draft** bar shows the revision that the draft is based on.
If the live revision is different, the bar also shows it.
If the draft was saved before the server restarted, the bar also tells you that **Pause and apply** then stops with a conflict.
The editor compares the server start ID of the draft with the server start ID of the latest live state that it read.
Each read of the live state updates the bar, for example the read of an apply.
While the bar shows, the editor also reads the live state when its window gets the focus, for example when you go back to the editor.
A draft from an older editor has no server start ID, so the bar does not show this warning.
Select **Restore draft** to put the saved draft back.
The undo history then starts from the restored draft.
The restored draft is not applied, and it keeps the revision that it is based on and the server start ID of that state.
**Pause and apply** then replaces the live scenario, also when the draft is based on an older revision.
The status line tells you when it replaces newer live changes.
If the server restarted after the draft started, the apply fails with a conflict, and the status line tells you that instead.
See [Apply a draft](#apply-a-draft).
Select **Discard draft** to delete the saved draft and keep the live scenario.
Until you select one of the two buttons, the browser does not save new changes.
If you apply while the **Saved draft** bar shows, the browser keeps the saved draft, and the bar stays.

If the browser cannot save the draft, for example because IndexedDB is not available, the status line tells you.
A failed save or a full storage also shows an error notice.
Then export the draft to keep it.
When the draft has changes that are not saved in the browser, the browser asks before you leave or reload the page.

All editor tabs of one server share the saved draft.
When a different tab saves its draft or applies, the saved draft of this tab is gone.
This tab then shows a notice, and the browser asks before you leave or reload the page.
Your next change in this tab saves its draft again, and the other tab then shows the notice.

### Stored background

The browser keeps the background image with its calibration and opacity in IndexedDB, apart from the saved draft.
It saves the background 300 ms after each change, for example an image import, a scale calibration, an opacity change, an undo, or a redo.
The browser keeps one background for each server address.
Thus the background stays after a successful apply and after a reload.

- **Remove background** deletes the stored background.
- A new image replaces the stored background.
- **Import JSON** replaces the stored background with the background of the file.
  A file with no background removes it.
- **Reset draft** puts back the background of the last load, apply, or import.
- **Undo** brings back a replaced or removed background.
- **Load live scenario**, **Restore draft**, and a live scenario change from another page do not change the background.

The server does not get the background, so the background does not follow the live project.
If the live project is at a different place, remove or replace the background.
All editor tabs of one server share the stored background.
The last background change in a tab replaces it.
A tab writes the stored background only after a background change in that tab.
Thus a tab that shows an older background does not replace a newer background from another tab, also at a scenario edit or when you leave the page.
If the stored background is not valid, the editor shows an error notice and does not delete it.
The browser keeps it until a new background in the page replaces it.
The browser keeps the image in the IndexedDB database `podsim-editor-backgrounds-2`, with a random image key, the placement, the opacity, the frame state, and the frame and the license of the image.
The editor does not read the background of an older editor, which is in the `podsim-editor-backgrounds` database.
If the browser cannot keep the background, the editor shows an error notice.
Then export the project to keep the background.
When the background has changes that are not saved in the browser, the browser asks before you leave or reload the page.
This includes a removed background that the browser has not yet deleted.
The editor puts the stored background on the page before it accepts input.
The editor ignores the background in a saved draft of an older editor.
The **Background** section shows the image memory of the tab, the size of the stored image, and a notice that the exported project file is the only durable copy.

### Georeferenced background

A georeferenced image is a PNG or JPEG, north up, with known bounds in degrees.
The editor makes no network request for it.

1. Select **Import a georeferenced image**.
2. Choose the image, and type the south, north, west, and east bounds of its outer pixel edges.
3. Choose the projection of the image: **Equirectangular** or **Web Mercator**.
   The editor resamples a Web Mercator image into rows that are linear in latitude, at the meters for each pixel that you type.
   The resampled image can be at most 4,096 pixels on each side.
4. Type the source, the attribution, the license, the license URL, the copyright URL, and a notice.
   Links must be HTTPS URLs.
5. Select **Import**.

The image goes on its frame in the `geo` reference of the project.
A project with no nodes and no `geo` gets a reference at the center of the image.
A project with nodes and no `geo` needs a choice first:

- **Anchor two nodes**: type the IDs of two nodes at least 100 m apart, and the latitude and longitude of each.
  The reference puts the first node at its place.
  The editor rejects the anchor when the second node is more than 2% of the node distance from its place.
- **Adopt the image center**: the center of the image becomes the reference, and the network does not move.
  The network and the image can then fail to align.

The editor rejects bounds that cross the antimeridian, a latitude past 80 degrees, a frame that is not in the 100,000 m square, and a frame with an east-west scale error of more than 0.5%.
The image, its placement, its frame state, and the `geo` reference change in one undo step.
An edit, a drag, or an opacity change during the import stops the import, and the editor says why.

The frame is **attached** when the placement follows the frame.
When an attached frame is not on its place in the `geo` reference, for example after **Load live scenario** with a different `geo`, the **Background** section shows a warning.
Then select **Place from frame** to move the image onto its frame, or **Detach frame** to keep the placement.
**Calibrate scale** is disabled for an attached frame.
The export keeps the frame state, the frame, and the license in `background.asset`, and an import accepts a frame that is not on its place, with the warning.

When the image has an attribution, the map shows it in a line at the lower right corner, with a link to the copyright URL and to the license URL.
The **Background** section shows the source, the license, the time of the import, the method, and the notice.

### Live OpenStreetMap backdrop

Select **Live OpenStreetMap** in the Background panel, then **Enable live map**.
LondonCentral and LondonFull already have geographic references.
For an empty project, enter the latitude and longitude of world position 0, 0.
For an existing network without a reference, anchor two nodes or explicitly adopt the entered origin.
This does not move the network.

Pan and zoom to load the visible map tiles.
Higher zoom levels show more detail.
Podsim reprojects the tiles into the same coordinates as the network, between 80 degrees south and 80 degrees north.
The **Map opacity** control changes the saved map opacity.
**Remove live map** removes the map settings but keeps the geographic reference and any imported image.
These changes support undo and redo.

Select **Pause and apply** to share the map settings with simulation viewers.
The simulation has a **Show map** toggle, remembered in each browser.
It does not change the shared project.
Hidden views, a disabled toggle, and zero opacity stop new tile requests.
Requests already sent can finish at the provider.

The browser requests OSM Standard raster tiles directly from `tile.openstreetmap.org`.
Normal browser HTTP caching applies.
Only the visible area loads, with up to four requests in flight per view.
Failed requests do not retry automatically.
Select **Retry failed tiles** to retry them.
The network remains usable when tiles are unavailable.

Projects save the provider, opacity, and geographic reference, not tile pixels.
They need internet access to load the map on another machine.
There is no offline tile download, tile proxy, or automatic guideway generation.
Existing PNG and JPEG backgrounds, including older schematic imports, remain usable in the editor.
Their image export and calibration behavior do not change.

Keep the visible OpenStreetMap attribution when sharing a map view.
Network geometry traced from OSM can be subject to the [ODbL](https://www.openstreetmap.org/copyright).
The [OSM tile policy](https://operations.osmfoundation.org/policies/tiles/) governs use of the public tile service.
Automated tests use local tile fixtures and make no requests to that service.

### Find a place

Enter a place name under **View**, then press Enter or select **Search**.
Select a result to pan and zoom the editor map.
The project needs a geographic reference before search is available.
Search and selection do not change the project, add stations, or create undo steps.

Place search needs the native Podsim server.
The default provider is [Nominatim](https://nominatim.org/).
The server shares one request slot across all clients, with at least one second between upstream requests.
It caches up to 128 successful searches for 24 hours.
Busy searches show a wait message.
Press **Search** again after the wait.
Search never runs while you type and never retries automatically.
Do not enter personal or confidential information.
Keep the OpenStreetMap attribution and follow the [Nominatim usage policy](https://operations.osmfoundation.org/policies/nominatim/).

Use `-geocoding-url=""` to disable external place search.
To switch providers, set `-geocoding-url` to a Nominatim-compatible HTTP or HTTPS search endpoint.
The endpoint cannot contain credentials, query parameters, or a fragment.
The server does not follow redirects or use environment proxy settings for searches.
Multiple server deployments need a shared limiter before using the public provider.
Tests use a local fake provider and make no public searches.

### Apply a draft

**Pause and apply** is disabled when the draft scenario is the same as the live scenario.
An imported-image change does not enable it, because the server does not get the image.
Live map settings are part of the scenario and do enable it.
Applying a valid draft resets the shared simulation and leaves it paused.
Two drafts do not reset it.
A draft that is the same as the live scenario changes nothing.
See the `project` action in [the protocol](docs/protocol.md).
If the apply fails after the editor paused the simulation, the editor resumes it.
A simulation that was paused before the apply stays paused.
The editor does not resume a simulation that restarted after the pause, for example after a project apply from another browser.

The editor applies a draft only to the project revision that it loaded.
A project apply from another page makes a new revision.
A demand change in the simulation also makes a new revision.
The apply then fails with a conflict, and the editor keeps the draft.
A rewind to a save point from before a project apply or a demand change restores that project.
The rewind also rewrites the `-project` file.
An open draft then also gets an apply conflict.

If the server restarted after the draft started, the apply also fails with a conflict.
This also applies to a restored draft from before the restart, because a restart can restore a different project with the same session epoch and project revision.
To read the live project, the editor reads the live state before and after it.
The editor uses the project only when both state reads show the same server start ID and session epoch, and the project has the project revision of the second state read.
Else it reads the state and the project again, for a maximum of three attempts.

After a conflict, the editor reads the live project again.
The **Apply conflict** bar then shows the live project revision N and two actions:

- **Load live scenario** makes the live scenario the draft and the base of **Reset draft**.
  When the draft has scenario changes, the editor asks first.
  The page keeps its background, and **Undo** brings back the replaced draft.
- **Apply over revision N** applies the draft over revision N of the live scenario.
  The editor asks first, because the draft replaces the live scenario.
  If the live revision is not N any more, or the server restarted again, the apply fails with a conflict again, and the bar shows the new revision.

Both actions read the live project again, with the session epoch and the server start ID.
The next command of the page then uses the epoch of the running server.
A successful apply, **Load live scenario** and **Reset draft** hide the bar.
If the editor cannot read the live project after a conflict, the status line tells you, and the bar does not show.
Select **Pause and apply** to try again.

For other failures, the editor shows the reason from the server, for example a project file that the server cannot save.
When the server is busy with other large commands, it rejects the apply with HTTP 503.
The editor then shows the reason from the server and tells you how many seconds to wait before you apply again.

### Project files and validation

Project files save the design and settings, not the exact running state.
Save points are exact, but they are in server memory only.
They end when the server stops, also with `-state`.

An import does not replace the draft when the file is malformed, unsupported, or fails validation.
The Go/WASM editor worker checks the draft and imported projects off the main browser thread.
It retains project branches and reuses topology and demand flow checks until their inputs change.
The worker computes changes for scenario names, demand controls, fleet counts, sharing, platoons, and operating flags.
It also adds, edits, and removes rail events and their weighted stations.
Go owns the undo and redo timeline, including background metadata.
The browser retains immutable drawing copies by Go snapshot ID and owns image bytes and storage pins.
Image imports prepare any required history trim before publication and keep the image table within 128 MiB.
If a worker acknowledgment fails, apply stops and the visible draft remains available for export.
The browser keeps queued input and waits for pending edits before apply, export, undo, or redo.
Validation checks routes between passenger stations, berth routes, pod placement, resource IDs, and the 24-meter minimum lane length.
A route between passenger stations goes from each berth of one station to each berth of the other station.
It can use all lanes.
A berth route goes from the station entry to the berth, or from the berth to the station exit.
It can use a chain of lanes, as in the London stations.
It cannot pass through the entry, exit, or berth node of a station.
Validation also limits the compact JSON form of a project to 10 MiB or less.
The topology JSON writes each `<`, `>`, and `&` as 6 bytes, so the server refuses a project whose topology is more than 10 MiB plus 4 KiB with `topology exceeds supported limit`.
The editor sends the project in one command, and it compresses a command of more than 64 KiB with gzip.
The server accepts a command body of 4 MiB or less, and 10 MiB plus 64 KiB of command JSON after decompression.
Before the editor pauses the simulation for an apply, it checks the size of the project and shows the limit.
A project can have at most 300 stations, 5,000 nodes, 8,000 lanes, and 300 pods.
Each station can have at most 200 berths.
A node can have at most 64 lanes, counted at the start node and at the end node of each lane.
At each node, the simulator compares each ordered pair of two different lanes at the node when it starts.
The total of these pairs over all nodes can be at most 100,000, for example about 24 nodes with 64 lanes each.
Each coordinate of a node position or a lane control point must be from -100,000 to 100,000 meters.
A project can have an optional `geo` member, the geographic reference of the network.
An optional `map` member selects the live backdrop, for example `{"provider":"osm","opacity":0.45}`.
It requires `geo`.
Omitting `map` keeps the existing plain background.
The `geo` member has `latitude` and `longitude` in degrees, `projection` with the value `equirectangular`, and `radius` with the value 6371000.
The latitude must be from -80 to 80 degrees, and the longitude must be from -180 to 180 degrees.
The reference is the place of world position 0, 0.
A point at latitude `lat` and longitude `lon` is at x = R cos(lat0) (lon - lon0) and y = -R (lat - lat0), with lat0 and lon0 the reference, the angles in radians, and R the radius.
Thus x increases to the east, and y increases to the south.
Simulation physics does not use the reference.
The simulator divides each lane into track cells of about 30 meters, with at least 2 cells in each lane.
All lanes together can have at most 64,000 cells, for example about 1,900 km of lanes.
Two lanes cannot have the same start node, end node, and path.
A demand profile can have at most 65,000 flows.
The editor checks do not include the limits of the lane pairs, the coordinates, and the track cells.
The server checks these limits when you apply the project.

Two editor checks give warnings: a junction with no lanes, and a network section that no lane connects to the other nodes.
The server accepts a project with these warnings, so a warning does not block an apply or an import.
A passenger station in a separate section still gives an error, because the other passenger stations cannot reach it.
The editor puts the passenger stations in groups.
In a group, each station can reach each other station.
The largest group is the main group.
When two or more groups have the largest size, the main group is the group with the fewest missing routes to and from the other passenger stations.
If these groups also have the same number of missing routes, the main group is the group with the first station in the project.
Each station outside the main group is cut off.
It gives one error, not one error for each station pair.
The error tells if the station cannot reach some passenger stations, if some passenger stations cannot reach it, or both.
It gives the number of these stations, or the station name when there is only one.

If the server project fails these checks, the editor still loads it as the draft.
The **Checks** section lists the errors first, then the warnings.
When the draft has only warnings, the summary tells you that the scenario is ready to apply and gives the number of warnings.

The editor runs the checks 150 ms after the last draft change, also after an undo or a redo.
**Run checks** runs them at once.
The number of errors shows beside **Pause and apply**, for example **2 problems**.
Warnings do not count.
Select the number to show the **Checks** section.
When the checks find errors, **Pause and apply** does not send the draft and shows the **Checks** section.
The server checks the project again when you apply it.

Select a message that names a station, berth, lane, or junction to select that item.
A berth message selects the station of the berth and marks the berth.
A pod message selects the station of the pod.
A message about a cut-off station selects that station.
When the item is off the map or near the edge of the map, the map moves so that the item is at the center of the map.
The map also moves when the map scale is below 0.5 screen pixels per meter.
The scale is then 0.5 screen pixels per meter or more.
In a narrow window, the page also scrolls to the map.

When a message in the **Checks** section has the keyboard focus, the focus stays on that message after the checks run again.
When the text of the message changes, for example the number of stations in a message about a cut-off station, the focus stays on the message for the same item.
If the message is gone, the focus goes to a different message for the same item.
If that item has no message, for example after you select it and press Delete to delete it, the focus goes to the nearest message that is still in the list.
The next message comes first, then the previous message.
If no message of the old list is left, the focus goes to the message at the same place in the list, or to the last message.
When no message that selects an item is left, the focus goes to the **Checks** heading.
When the focus is not on a message in the **Checks** section, the checks do not move it.

## Command-line tools

### Demand and policy comparisons

Redistribution is off by default.
When it is on, the simulation runs guarded positioning.
Guarded positioning moves an idle empty pod to a busy station that has no pod.
The demand weights of the stations select the busy stations.
The policy moves pods only while the demand rate is below one request per minute for each 20 pods of the fleet.
While generated demand runs, the server uses the configured rate of the demand.
Otherwise, it uses the mean rate of the requests since the last reset.
Thus a fleet of 20 pods or fewer makes no move while generated demand runs.
The policy stops moves to busy stations when the newest request is more than 180 seconds old, or when a waiting request has no pod.
It also stops these moves when more than two fifths of the fleet works, or when the positioning moves reach the boardings.
While the demand rate is low, the policy also moves an idle pod that blocks a berth.
The pod goes to a free berth of a busy station within 180 seconds of travel, or else to the nearest free Parking berth.
Passenger assignments take priority over empty positioning.
Before a pod moves, the policy reserves a free destination berth, and it leaves another free berth at the station.
A cooldown limits repeated moves.
A remote reservation yields to passenger traffic until the empty pod enters the final admitted block.
Admitted track and physical berth ownership remain protected.

Run the same seeded demand schedule with redistribution off and on.
This example runs the London preset at 3 requests per minute, where the policy is active:

```sh
mise run scenario -- -preset london-central -output /tmp/podsim-london.json
mise run compare -- -project /tmp/podsim-london.json -pattern profile -bands am-peak -focus 940GZZLUEUS -loads 20s -duration 30m
```

The report includes:

- Demand throughput, backlog, drain time, fleet use, and pickup wait.
- Journey time from request to alight, and the 95th percentile wait and journey time.
- Completed and remaining journeys, and skipped requests.
- Peak queues and berth use at the focus station.
- Passenger and empty travel, loaded-distance percentage, and positioning moves.

The default comparison uses a Market-heavy pickup forecast and identical initial fleets.
Schedule IDs identify the identical requests used for each off/on pair.
The `on` policy is guarded positioning, as in the server.
The compare command gives the policy no configured rate, so the gate reads the mean rate of the accepted requests.
At the first skipped arrival, the compare command turns the policy off for the rest of the arm.
See [qualification results](docs/qualification.md#guarded-positioning-in-the-london-sweep).
Pending requests contribute their elapsed wait at the end of the measurement window.
The synthetic patterns are balanced, destination, hotspot, bursty-hotspot, and hub-burst.

| Flag | Effect |
| --- | --- |
| `-project scenario.json` | Test another network. |
| `-patterns all -seeds 1,2,3 -loads 30s,45s,60s` | Run a paired matrix. |
| `-format json` or `-format csv` | Machine-readable results. |
| `-output` | Write the report to a file. |
| `-pattern profile -bands all` | Run the origin-destination bands of a project demand profile. |
| `-pattern profile-daily` | Repeat the project profile with its authored clock and rates. See [daily demand](docs/daily-demand.md). |
| `-daily-start-minute 420` | Start a daily comparison at 07:00 simulated time. |
| `-focus` | Select the station of the focus metrics in the report, and the station that the destination, hotspot, bursty-hotspot, and hub-burst patterns favor. |
| `-duration` | The length of the measurement window. Default 30m. |
| `-request-every` | The interval between requests without `-loads`. Default 45s. |
| `-seed` | The seed without `-seeds`. Default 1. |
| `-arrivals-for` | Stop new requests before the measurement ends. |
| `-stop-when-drained` | Stop an arm after all accepted requests complete. |
| `-adaptive-limit` | With `-stop-when-drained`, find capacity limits with fewer arms. |
| `-past-limit` | The number of rates that an adaptive group runs after its limit. Default 1. |
| `-workers` | Run independent arms concurrently. Reports keep their deterministic order. |
| `-burst-size` | Group burst-pattern requests at the same simulated time. |
| `-sharing-limits 1,4` | Compare shared ride party limits. |
| `-sharing-modes drop-offs,destination` | Compare the shared ride modes. Default `drop-offs`. A limit of 1 runs one time and shows the destination mode, because no party joins a pod. |
| `-sharing-max-stops` | The stop limit of the drop-offs mode, from 1 to 7. Default 3. |
| `-sharing-joins unassigned,reassign-existing` | Compare the shared ride join policies. See [parties](#parties). A limit of 1 shows the given policy, but no party joins a pod. |
| `-routing-policies free-flow,congestion,queue,predictive` | Compare the experimental routing policies. See [routing](#time-geometry-and-routing). |
| `-redistribution-policies off,on` | Select the positioning policies. `on` is guarded positioning. |
| `-wait-rules current,strict,none` | Compare the finishing-pod wait rules from the dispatch section. |
| `-platoon-policies off,virtual` | The experimental platoon A/B. `virtual` lets queued pods follow the pod ahead at a short gap. |
| `-station-buffers off,on` | Compare independent station-buffer admission settings. The default is off. |
| `-pickup-reassignment off,on` | Compare independent pickup reassignment settings. The default is off. |
| `-queue-limit` | Change the limit of 200 pending requests. At the limit, the comparison skips new arrivals. |

The report has a `wait_rule` column or JSON field only when you give `-wait-rules`.
The report has a `platoon_policy` column or JSON field only when you give `-platoon-policies`.
The report has a `sharing_join` column or JSON field only when you give `-sharing-joins`.
Without the option, each arm uses the `unassigned` policy.
The CSV report has the seat screen columns only when a `-sharing-limits` value is above 1.
The JSON report always has them.

The editor has separate experimental controls for station buffers and pickup reassignment.
Pause and apply activates the draft settings.
The comparison command uses its explicit policy flags, not the experimental settings in the input project.
Without either flag, both policies stay off and the existing report format stays unchanged.
With a flag, table and CSV output include its policy column, and JSON output includes its off/on value.
With `-pickup-reassignment`, JSON also records controller work, swaps, transfers, and predicted seconds saved.
These predictions exclude future traffic; they do not establish realized service improvement.

The `sharing_mode` column gives the sharing mode of each arm.

With `-adaptive-limit`, arms that differ only in offered rate and seed form a group.
Each group runs its rates from the lowest offered rate, with all seeds of a rate together.
After the first rate at which a seed does not drain, the group runs `-past-limit` more rates and skips the rest.
The report omits the skipped arms.
The other rows are identical to the rows of a full run.

When `GOGC` is not set, the compare command sets the Go GC percent to 400.
An earlier LondonCentral measurement used about 14% less CPU and about 120 MB more memory than at the default of 100.
The September 29 review retained 400 after testing both London presets.
CPU and memory costs depend on the workload, and these measurements do not set the server GC policy.
The reports do not change.
Set `GOGC` to use another value.

#### Report columns

The JSON report has `schema_version` 12.
It is 13 with a station buffer, pickup reassignment, or station queue spacing column, 14 with an `onboard_pickups` column, and 15 with energy estimates.
These columns give the waits and journeys of the passengers.

| Column | Definition |
| --- | --- |
| `wait_average_seconds` | The mean wait from request to boarding. The set has each party that boarded, and each request that is pending at the end of the arm with its elapsed wait. |
| `wait_maximum_seconds` | The longest wait in the wait set. |
| `wait_p95_seconds` | The 95th percentile of the wait set. |
| `journey_average_seconds` | The mean journey time from request to alight. The set has each party that alighted at its destination before the end of the arm. |
| `journey_p95_seconds` | The 95th percentile of the journey set. |
| `journey_maximum_seconds` | The longest journey time in the journey set. |

A party alights when its pod completes unloading at the destination berth.
At that tick, `served` counts the party.
Each percentile is the nearest-rank value: sort the n values, and use the value at position ceil(0.95 n).
A column with no values is 0.
The journey set does not have the parties that are pending or aboard at the end of the arm.
Thus, for an arm that does not drain, the journey columns do not include the longest journeys.

The compare command reads a separate record for each party from the simulation.
Each party that joins a shared ride has its own wait and journey.
Its boarding time is the time of the join, and its journey ends when the pod completes unloading at its stop.

These columns give the distances of the parties that alighted.

| Column | Definition |
| --- | --- |
| `occupancy` | The rider distance of the parties that alighted, divided by the occupied distance of their pod journeys. |
| `rider_distance_meters` | The sum of the distances that the parties that alighted rode, from the berth where they boarded to the berth where they alighted. |
| `direct_distance_meters` | The sum of the free-flow distances of the same parties between the same two berths. |
| `detour_ratio_mean` | The mean, over the same parties, of the ratio of the rider distance to the free-flow distance. |
| `detour_ratio_max` | The largest ratio for one party. |
| `intermediate_stops` | The number of stops where parties alighted and the pod continued with other parties. |

A pod journey starts when the first party boards the pod.
All parties of a pod journey board at the same berth.
The occupied distance of a pod journey is the longest rider distance of its parties.
Without sharing, the occupancy is 1.
With sharing, it is the mean number of parties aboard, weighted by distance.
The value is 0 when no party alighted.
The occupancy does not include the parts of pod journeys after the last party that alighted before the end of the arm.

The free-flow distance is the length of the shortest route from the berth where the party boarded to the entry of its destination station, and then along the station path to the berth where it alighted.
The routing policy does not change it.
A detour ratio of 1 means that the pod took this route.
A route around a full station, a route of the routing policy, or a stop for another party makes the ratio larger than 1.

These columns give the time that pods spend stopped, in pod-seconds.
A pod is stopped when it has a wait reason and its speed is less than 0.1 m/s.
A pod at a berth that waits for track admission also has a wait reason.
The compare command examines the pods at each whole simulated second, and each stopped pod adds 1 second.

| Column | Definition |
| --- | --- |
| `stopped_pod_seconds` | The time of all stopped pods, for each wait reason. |
| `junction_wait_seconds` | The time of the stopped pods with the wait reason "Junction traffic". |
| `track_wait_seconds` | The time of the stopped pods with the wait reason "Pod ahead". |

The other stopped pods wait for a berth or for parking, so `stopped_pod_seconds` is at least the sum of the other two columns.
The `peak_stopped_vehicles` column counts only the pods with a speed of less than 0.01 m/s, so it can count fewer pods.

These columns give the busiest node.
A pod passes a node when it enters a lane that starts at the node.
Thus a pod that leaves a berth passes the berth node, and a pod that stops at the end of its route does not pass that node.
The simulation records each pass at the tick at which the pod enters the lane, and the compare command reads these records.

| Column | Definition |
| --- | --- |
| `peak_node_throughput_per_minute` | The most passes of one node in 60 seconds. For each pass at time t, the window holds the passes of the same node after t - 60 s and at or before t. |
| `peak_node` | The ID of the node with that count. When two nodes have the same count, it is the lower node ID in byte order. It is empty when no pod passes a node. |

The `coupled_time_percent` column gives the time that pods travel in a platoon.
The compare command examines the pods at each whole simulated second.
Each traveling pod adds 1 second to the travel time.
Each traveling pod that has a pod ahead or a pod behind in its platoon also adds 1 second to the coupled time.
The value is the coupled time divided by the travel time, as a percentage.
It is 0 when no pod travels, and always 0 without the `virtual` platoon policy.

The seat screen columns follow `shared_parties`.
They show whether the parties that a boarding pod could take are more than its seats.
A party can take a pod when the pod could add it with the rules of the sharing mode: the destination, the stop limit, and the detour cap.
The join census columns count the parties that have a pod on its way to their origin.
A party counts when a boarding pod at its origin could take it with a free seat at one or more dispatch passes.
The census does not check that a new first stop has a route, so it can count a party that a pod could not take.

| Column | Definition |
| --- | --- |
| `full_pod_refusals` | The parties that found a full boarding pod at their origin that could take them, and that joined no pod. Each party counts one time. |
| `full_departures` | The boarding pods that departed with as many parties as the limit. |
| `departure_backlog` | For each departure of a boarding pod, the waiting parties at its origin that the pod could take with a free seat. The column gives the sum over all departures. It also counts a party that has another pod on its way. |
| `departures_demand_over_four` | The departures with more than four parties aboard plus backlog. |
| `departures_over_four_aboard` | The departures with more than four parties aboard. It is 0 at a limit of 4 or less. |
| `join_eligible_assigned` | The parties with a pod on its way that a boarding pod at their origin could take. Each party counts one time. |
| `join_eligible_existing_stop` | The parties of `join_eligible_assigned` that a boarding pod could take with no new stop, because the pod already stops at their destination. In destination mode, it is equal to `join_eligible_assigned`. |
| `join_eligible_added_stop_only` | `join_eligible_assigned` minus `join_eligible_existing_stop`. A boarding pod could take each of these parties only with a new stop. |
| `reassigned_parties` | The parties that joined a boarding pod while they had a pod on its way. Dispatch released that pod. It is 0 with the `unassigned` policy. The census also counts each of these parties in `join_eligible_existing_stop`. |

The columns are 0 when the party limit is 1.

### Generated scenarios

Create a repeatable server scenario:

```sh
mise run scenario -- -preset scale100 -output /tmp/podsim-scale100.json
mise run serve -- -project /tmp/podsim-scale100.json
```

Presets include `small`, `busy`, `parking-constrained`, `rail-hub`, `scale100`, `london-central`, and `london-full`.
LondonCentral preserves the central qualification network.
[LondonFull](docs/london-full.md) covers 269 Tube sites with 2024 endpoint demand.
Generated files contain raw server settings.
The editor can export the loaded scenario with optional local background data.
See [qualification results](docs/qualification.md) for safety checks, performance measurements, and redistribution limits.

The capacity flags change the berths, the initial pods, and the berth pitch of a preset.
A flag that is not on the command line keeps the preset value, so the output without flags does not change.

| Flag | Effect |
| --- | --- |
| `-station-berths N` | Set the berths of each passenger station. |
| `-parking-berths N` | Set the berths of each Parking station. |
| `-berths ID=N,ID=N` | Set the berths of single stations by station ID, such as `station-19` or the ID of a TfL station. |
| `-station-pods N` | London presets only. Set the initial pods of each passenger station. |
| `-parking-pods N` | London presets only. Set the initial pods of each Parking facility. |
| `-berth-pitch M` | Set the distance in meters between two berths of a station. The minimum is 25. |

A ring station has a 30-meter pitch, and a `scale100`, `london-central`, or `london-full` station has a 75-meter pitch.
The ring and `scale100` presets put the initial pods round robin on the passenger stations, and a full station gets no more pods.
With changed capacity, the project name ends with "(custom capacity)".
A layout check then examines the generated network.
If two lanes are too near each other, the command writes no project and gives an error that names the station and the lanes.
In a ring or `scale100` network, two lanes without a common node must be at least 12 meters apart on the paths that the pods follow.
For the London layout rules, see [London capacity options](docs/london.md#capacity-options).
An unknown station ID, a berth count out of range, more pods than berths, or a network over the project limits also gives an error.
A London station can have 1 to 200 berths.
A ring or mesh station can have 1 to 62 berths.
The entry and exit nodes of a ring station have a lane for each berth, and a node can have at most 64 lanes.

The command writes one summary line to standard error.
The line gives the preset, the passenger and Parking berths, the pods, the nodes and lanes with their limits, the output bytes, the SHA-256 of the network, and the number of soft layout conflicts.
A soft conflict does not stop the command.
The London presets can have soft conflicts, for example berths that are nearer to the TfL position of another station.

```sh
mise run scenario -- -preset scale100 -berths station-19=8 -berth-pitch 68 -output /tmp/podsim-station19-8.json
mise run scenario -- -preset london-central -station-berths 3 -berths 940GZZLUEMB=2 -parking-berths 24 -output /tmp/podsim-london-3.json
```

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

#### london-central

The [LondonCentral qualification network](docs/london.md) uses TfL station locations and topology, real station names, directed twin guideways, off-line berths, and three Parking facilities.
It sets `platoonLimit` to 4, so the server runs it with virtual platoons.
Its `geo` member is the reference of its projection, latitude 51.5074 and longitude -0.1278.
Station lanes identify approach, entry, berth access, through, departure, and exit maneuvers.
The pod inspector shows the current maneuver in **Station phase** and the station name on the line below it.
Projects without this optional lane metadata still load.
The simulator infers through, berth access, and departure roles from the station paths.

#### london-full

[LondonFull](docs/london-full.md) includes 269 passenger sites and three Parking facilities with 2024 endpoint demand.
It starts with 287 pods, weighted station berths, and a configured rate of 10 requests per minute.
Demand starts disabled.
Its finite-arrival demo checks do not establish sustainable capacity or replace the LondonCentral qualification results.

## Scope and model

### Time, geometry, and routing

The Go core uses fixed 60 Hz steps and world coordinates in meters.
Routing chooses the shortest free-flow travel time on directed lanes.
Equal-cost routes use scenario order for deterministic results.

The compare command has three experimental routing policies.
They change only the route that a pod gets when it starts a journey, a pickup, or an empty move.
Estimates and the choices of dispatch, positioning, and parking use free-flow times.
A route from these policies does not go through the berths of a third station.
When each other route goes through such berths, the pod gets the free-flow route.
The `congestion` policy adds 6 s for each owned track cell of a lane and 20 s for each waiting pod on a lane, and it keeps these costs for 5 s.
The `queue` policy gives each stopped pod on a lane 3 s of queue time.
It adds only the queue time that remains when the pod gets to the lane.
It changes the free-flow route only when the saving is at least 15 s and at least 5%.
The new route must also take at most 1.2 times the free-flow time of the free-flow route.
The [predictive policy](docs/predictive-routing.md) adds smoothed observed queues and planned lane arrivals over a 90-second horizon.
It retains the same saving and detour guards, and remains experimental.
The [selected service screen](docs/predictive-service.md) returns no alternative routes across six matched pairs and establishes no service gain.
The [route-selection diagnosis](docs/predictive-diagnosis.md) explains the guard outcomes and records a congested trial with worse service.

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
An optional limit of two to eight lets waiting parties join a pod that is still boarding at their origin.
The project setting `sharedRideMode` selects the parties that can join:

| Mode | Parties that can join |
| --- | --- |
| `drop-offs` | The default. Parties for a stop of the pod, or for a station that the pod can add as a stop. |
| `destination` | Parties for the destination of the pod. |

In `drop-offs` mode, a boarding pod adds a stop for a new party in one of these conditions:

- The free-flow route from the boarding berth to the last stop passes the start of an approach lane of the station.
  The new stop goes between the other stops in route order.
- The free-flow route from the boarding berth to the station passes each stop in the same order.
  The station becomes the last stop.

The pod adds the stop only when the planned detour ratio of each party, new or aboard, is at most 1.5.
The ratio of a party is the planned distance from the boarding berth to the berth where the party alights, over the free-flow distance to that berth.
The compare column `detour_ratio_max` measures the same ratio.
The pod does not know its berth at each stop before it arrives, so the plan uses the berth that gives the largest ratio.
The conditions and the plan use the free-flow routes with each routing policy.
Each leg of the pod takes the route of the routing policy when that route keeps each party aboard within 1.5.
Otherwise, the leg takes the free-flow route, which the plan used.
The pod changes to another berth at its next stop only when the new route keeps each party aboard within 1.5.
Thus, the measured ratio of each party that boards with a limit of two or more in the `drop-offs` mode is at most 1.5, while these settings do not change.
A new stop before the first stop changes the route of the pod, so the pod adds a stop only before it gets track.
The project setting `sharedRideMaxStops` sets the maximum number of stops before the last stop, from 1 to 7.
The default is 3.
The stops do not change after the pod departs.
At each stop, the parties for that stop alight, and the pod continues to its next stop with the other parties.
While it waits for track at such a stop, its activity is **Continuing**.
Sharing does not wait for more parties, and a pod with passengers does not pick up parties on its way.

The project setting `sharedRideJoin` selects the waiting parties that can join by their pickup state:

| Policy | Parties that can join |
| --- | --- |
| `unassigned` | The default. Parties that have no pod. |
| `reassign-existing` | Also parties that have an empty pod on its way, when the boarding pod already stops at their destination. |

With `reassign-existing`, dispatch releases the pod of a party that joins, as it does when an idle local pod takes a trip.
The stops of the boarding pod do not change, so the parties aboard get no new stop.
A party whose pod is idle at the origin boards that pod.

#### Dispatch order

Passengers request travel between stations independently of pod selection.
Dispatch considers requests in submission order.
An idle local pod serves the oldest waiting passenger.
Otherwise, dispatch compares idle pods and empty pods that can divert.
An empty pod can divert when it goes to parking, when it moves for redistribution, or when dispatch released it from a pickup.
Dispatch uses the estimated pickup time, then the pod ID, as the tie-breaker.
An idle pod at the pickup station has an estimate of zero, because it boards at its own berth.

#### Wait rules

Dispatch can wait for a busy pod if it should reach the pickup more than two seconds earlier.
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

- Requests that waited at least 10 simulated seconds take priority, oldest first.
- Younger requests prioritize pods with passengers, then assigned pickups, then other empty movements.
- Within each class, the oldest local admission request wins.
  Pod ID breaks a tie.
- Existing reservations remain protected.
- Admission uses the state before movement.
- Released resources become available on the next tick.

The block model is conservative.
It does not model continuous car-following or optimized junction capacity.
The traffic model requires lanes at least 24 meters long.

#### Virtual platoons

The project setting `platoonLimit` turns on virtual platoons.
Its value is the maximum number of pods in one platoon, from 2 to 4.
A value of 0, or no value, turns platoons off.
The **Platoons** field of the editor sets the limit.
The compare command does not read this setting.
It uses `-platoon-policies`, and the `virtual` policy has a limit of 4 pods.

With platoons on, a slow pod in a queue can link to the pod ahead on the same lane.
A mainline link needs shared succeeding lanes, and each route must have one speed limit.

Each link certifies a run of lanes that both routes share, from the lane of the follower.
The total turn along the run must be at most 120 degrees.
This total counts each turn in a curved lane and at each lane join one time, with its size and not its sign.
The link clearance is 12 m divided by the cosine of half of the turn, plus 0.01 m.
On such a run, two pods that are at least the clearance apart along the path are at least 12 m apart.
All links of one platoon have the same turn and clearance, and a link keeps them until it ends.
A link forms only when the two pods and their stop points are at least the clearance apart.

A linked pod can reserve the blocks and junction sections that the pods ahead of it in its platoon hold.
It shares only the blocks whose resources each pod releases at least 1 m before the end of the run.
Thus each pod that holds a shared resource is on the run.
It stops at least the clearance behind the stop point of the pod ahead, less the distance of 0.5 s of travel at its speed.
When a pod ahead releases a shared resource, the next pod in the platoon owns it.
Berths are not shared.
A mainline link can add the next shared lane while its total turn stays within the turn of the platoon.
After the last block that it can share, the link drains: the follower waits until the pods ahead pass the shared resources and hand them to it.
Then the link ends.
A link also drains when platooning stops, when one of its pods stops traveling, or when a different pod comes between the two pods.
While a linked pod holds a cell of a pod ahead, it reserves only the cells that its predecessor reserved.
A pod ahead in a platoon does not reserve a resource again while a pod behind it holds that resource.
A linked empty pod cannot divert.
The 12 m separation check does not change.

With experimental station buffers enabled, eligible entry queues can form [fixed local links](docs/station-entry-platoons.md).
These links share only complete interior track cells and cannot grow onto berth branches.
A head can append an exclusive berth suffix while inherited ownership drains.
Saved state keeps these links, and their service benefit remains unqualified.

A saved state keeps each link in the `platoon` field of the follower.
The field gives the predecessor, the run as indexes into the two saved routes, the turn, and whether the link drains.
The restore checks the run, the turn, the speed limits, and the clearance against the network and the pods, and it does not plan the link again.
An invalid complete-lane link fails the physical tier under the existing recovery rules.
An invalid fixed-entry buffer certificate rejects the saved state without logical fallback or partial member demotion.
A link that drains before the save also drains after the restore.
The saved state does not keep the platoon limit.
The restore uses the `platoonLimit` of the project.

### Stations and parking

#### Berth choice

Passenger journeys route to the station entry without a berth assignment.
The controller chooses the least-assigned reachable berth when the next reservation of the pod starts on the final lane of the road route.
It also chooses the berth when that reservation reaches the last block of the road route, as it can on a short final lane.
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
Optional redistribution moves an idle empty pod to a busy station that has no pod.

Parking serves no passengers.
Parked pods return to service automatically when assigned to a pickup request.
After the demo, request a trip from Harbor or Garden to see an available pod return for pickup.

The tests establish progress for feasible supplied scenarios, not for every saturated network.

### Faults

Faults are off by default.
A project with the fault marker can stop a pod with a pod fault, or block a lane segment with debris.
A pod fault brakes the pod to rest, and its pending pickups go to other pods.
After the evacuation delay, the riders leave the pod at rest, and their orders end interrupted.
Other pods route around a fault, or wait.
The **Fault** button of the [pod inspector](#inspect-a-pod) and the `fault` command start a pod fault.
Only the `fault` command starts debris, and the map does not draw debris.
See [faults](docs/operations.md#faults) for the settings and the [fault commands](docs/protocol.md#fault-commands) for the protocol.

### Server and browser

#### Shared state

One server owns the simulation clock, commands, and demand settings.
Browsers receive shared gzip state deltas over WebSocket and show the connection status.
The simulation view requires WebSocket and native gzip decompression, with no HTTP polling fallback.
They fetch the network topology on connection, and after the session epoch, the project revision, or the server start ID changes.
Controls wait for server confirmation.
During the wait, the line below the panels shows **Shared session / waiting for command confirmation**.
**From** and **To** send no command, so you can change them during the wait.

Pod selection, origin, destination, and the open inspection panel stay local to each browser.
Imported background images stay in the editor, in browser IndexedDB storage, and in the exported project file.
Live map settings travel with the project and topology.
Each viewer fetches its visible tiles directly.
The shared simulation receives the network geometry and settings.

#### Motion

The map buffers 150 ms of snapshots and interpolates movement along lanes between updates.
Controls and order status use the latest server state.
Pauses, speed changes, resets, rewinds, the start and end of the traffic demo, and long connection gaps clear buffered motion.
Rendering never predicts movement beyond the latest received position.

#### Compression and the WASM module

State stream messages always use gzip.
HTTP snapshots and browser assets use gzip when the client supports it.
Range responses remain uncompressed.

The build keeps only `podsim.wasm.gz`, which it compresses at the maximum level one time.
The server sends these bytes to browsers that accept gzip.
For a client without gzip or for a Range request, the server decompresses the module on the first request and keeps it in memory.
Byte ranges then use the offsets of the uncompressed module.
The build writes `podsim.wasm.gz` to a temporary file and then renames it, so a running server never reads a partial module.

A `-dir` directory from an older build with only `podsim.wasm` still works.
The server then compresses that file for each gzip request.
If a `-dir` directory has a `podsim.wasm` that is newer than `podsim.wasm.gz`, the server uses `podsim.wasm`.

While the page downloads the WASM file, it shows the received size in megabytes.
With gzip, the page counts the bytes after decompression, so the count goes up to the WASM size and not to the smaller gzip size.
Without gzip, it shows a percent of the file size instead.
If the download or the start fails, the page shows **Podsim could not start.** and the reason.
If the program stops after it started, the page shows **Podsim stopped.** and asks you to reload the page.

#### Connection status

Until the first state frame arrives, the map shows **Connecting to server...** and no network.
A lost connection disables commands.
While the connection is lost, the map is dimmed and an amber banner shows **Connection lost.
Showing state from N s ago.**
N is the time in seconds since the last state frame.
Reconnection restores the current shared state.

When the server restarts with different browser files, the simulation view in each open browser page reloads by itself.
After the editor has opened in the page, the editor keeps its state and its old files until you reload the page.
See [build ID](docs/operations.md#build-id).
The desktop client shows a message instead.
Restart it to load the new version.

The line below **From** and **To** shows these notices for 3 s:

| Event | Notice |
| --- | --- |
| A server restart with the same build | **Server restarted. Save points cleared.** |
| Another browser resets or rewinds the session, starts the traffic demo, or applies a project | **Another browser reset or rewound the session.** |

A reset or a rewind from this browser shows its own notice instead.
If the server restarts before it applies a reset or a rewind from this browser, this browser can incorrectly show that another browser reset or rewound the session.

Each server process sends its own start ID, so the simulation view finds a server restart also when a hidden tab did not get the first frames after it.

#### Command receipts

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
See [server restarts](docs/protocol.md#server-restarts).

See [the client protocol](docs/protocol.md) for the API, save point commands, payload measurements, and the Protobuf evaluation.
See [the project brief](PROJECT_BRIEF.md) for the wider scope and research.

## Code and validation

| Path | Purpose |
| --- | --- |
| `internal/sim` | Network, routing, requests, pod movement, state export and restore, and deterministic tests. |
| `internal/observe` | Station berth and queue metrics for the view and comparison reports. |
| `internal/project` | Versioned scenario settings, validation, and detached copies. |
| `internal/editormodel` | Go editor worker state, checks, edits, normalization, undo history, daily profile construction, and place navigation. |
| `internal/scenarios` | Deterministic scenario presets, including `scale100`, `london-central`, and `london-full`, and qualification tests. |
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
| `web` | Browser page, game loader, and scenario editor. |

### Measurement reports

These reports record tested workloads and their limits.
They do not replace scenario qualification or authorize policy adoption.

| Area | Records | Main limitation |
| --- | --- | --- |
| LondonFull | [Post-fix capacity](docs/london-full-postfix.md), [combined controllers](docs/london-full-controller-sustained.md), [12/min comparison](docs/london-full-controller-rate12.md), [13/min comparison](docs/london-full-controller-rate13.md), [14/min comparison](docs/london-full-controller-rate14.md), [mirrored layout](docs/station-mirror-load.md) | Finite recovery and growing backlogs do not establish sustainable capacity. |
| Pickup swaps and buffers | [Combined qualification](docs/dispatch-policy-qualification.md), [sustained comparison](docs/pickup-swap-sustained.md), [matched requests](docs/pickup-request-diagnosis.md), [service-tail cases](docs/pickup-tail-cases.md), [selected exclusions](docs/pickup-local-intervention.md), [berth-route preference](docs/berth-route-preference.md), [post-routing service](docs/berth-routing-service.md) | Better averages coexist with slower individual requests. Both policies stay off by default. New routes avoid intermediate berths when a compatible path exists. |
| Platoon queues | [Qualification follow-up](docs/platoon-followup.md), [Paddington leader progress](docs/paddington-leader-progress.md), [Paddington movement](docs/paddington-motion.md), [clearance samples](docs/paddington-clearance.md), [selected resource histories](docs/paddington-resource-history.md), [fixed station-entry links](docs/station-entry-platoons.md) | Paddington traces do not justify a clearance change. Fixed entry links are experimental and need station buffers. |
| Terminus throughput | [Burst measurements](docs/terminus-flow.md) | Buffers increase waits in the selected outbound Central bursts. Geometry and supply causes remain diagnostic work. |
| Server performance | [Route search storage](docs/route-search-performance.md), [finishing-pod bounds](docs/finishing-pod-bounds-performance.md), [admission storage](docs/admission-work-performance.md), [live server and GC](docs/admission-live-performance.md), [publisher cadence](docs/publisher-cadence-performance.md) | Live results cover two short repetitions per case. GC defaults remain unchanged. |
| Browser performance | [Compatible stream decoder](docs/stream-decoder-qualification.md), [journey page cache](docs/journey-page-cache-performance.md), [label dimensions](docs/label-measure-cache-performance.md), [label admission](docs/label-admission-performance.md) | Software-rendering results do not predict physical-GPU performance. Decoder gains do not establish lower whole-browser CPU. |
| Experimental adoption | [Proposed gates](docs/experimental-adoption.md) | Individual-tail and capacity thresholds need agreement before a default change. Historical 300-second counts remain diagnostics. |
| Tests and restarts | [Test timing](docs/test-speed.md), [diagnostic study performance](docs/study-performance.md), [experimental policy file restarts](docs/experimental-policy-restarts.md) | Study timings cover one matched LondonFull workload. File tests do not simulate power loss. |

Current-source follow-ups retain their own fixtures and limits:

- [Pickup tails](docs/pickup-postroute-tail.md) trace selected reassignment regressions without finding a new movement defect.
- [Exact pickup histories](docs/pickup-fleet-ablation.md) show how one early swap changes later pickup availability.
- [Policy failures](docs/policy-failures.md) retain selected platoon, sharing, and positioning failures after the routing fixes.
- [Policy fleet histories](docs/policy-fleet-history.md) separate earlier pickup changes, percentile ranks, and same-request delays.
- [Paddington position trials](docs/paddington-layout.md) improve selected full-restoration outcomes, but reject both partial layouts on safety.
- [Controller and capacity trials](docs/controller-capacity.md) retain Acton overload, individual regressions, and unfinished six-hour AM baselines.
- [Capacity diagnosis](docs/london-full-capacity-diagnosis.md) confirms extended AM13 recovery and exhausted AM14/AM15 pickup supply.
- [Strict finishing waits](docs/finishing-wait-capacity.md) reduce completions in four matched capacity cells.
- [Buffer claims and station speed](docs/station-buffer-speed.md) retain the Acton buffer loss and negative lower-speed trials.
- [Reserve and sharing trials](docs/acton-reserve-sharing.md) compare initial empty supply and pooling without establishing sustained capacity.
- [Predictive service trials](docs/predictive-service.md) leave matched request outcomes unchanged.
- [Predictive cost trials](docs/predictive-cost.md) measure CPU, allocation, and GC tradeoffs separately from service outcomes.
- [Snapshot allocation trials](docs/snapshot-allocation.md) measure a 5.6% to 7.2% allocation reduction with unchanged results.
- [Current Chrome and network trials](docs/current-client-network.md) check one and three clients, shared gzip updates, and emulated high latency.

### Tasks

```sh
mise run test
mise run check
```

| Task | What it runs |
| --- | --- |
| `mise run check` | Workflow validation, Markdown checks, race tests, the tests that skip under the race detector, the `test:web` tests, vet, lint, vulnerability checks, the native and browser builds, the embedded server tests, and the checkpoint bound test. |
| `mise run format` | Formats the Go sources and the Markdown files. |
| `mise run test:quick` | `go test -short ./...`. It skips the long tests of `internal/sim`, `internal/session`, `internal/parkride`, `internal/scenarios`, `cmd/compare`, and `cmd/serve`. The race tasks, `test:embedded`, and `test:bounds` run them. |
| `mise run test:web` | Only the editor, loader, and page tests. |
| `mise run qualify` | The two Station 19 drain tests in `internal/scenarios` and the two emergency choice latency tests in `internal/sim`, without the race detector. These tests skip under the race detector, so in `mise run check` only this task runs them. The `test:race` task runs the other tests of these packages. |
| `mise run test:embedded` | The root and `cmd/serve` tests with the `embed_assets` tag. It also runs the session tests that skip under the race detector or check more without it. The `test:race` task runs the other session tests. |
| `mise run benchmark` | 6,000 simulation steps on the 100-pod ring fixture, not on the current `scale100` mesh. |

`mise.toml` tracks Go 1.27, rumdl 0.2, and major versions for the other development tools.
`mise.lock` records the resolved tool downloads.

Some editor tests use Go to generate the `scale100` and `london-central` projects.
If Go is not on `PATH`, `node --test web/editor_test.cjs` skips these tests and gives the reason.
The `test:web` task sets `PODSIM_REQUIRE_GO=1`, so a missing Go makes the task and `mise run check` fail.

### Continuous integration

GitHub Actions runs the same check on pull requests, pushes to `main`, and version tags.
The workflow also supports a manual trigger.
It splits the check into six parallel jobs: `test:race:sim-stations`, `test:race:sim-other`, `test:race:session`, `test:race:other`, `qualify`, and `check:static`.
After all six pass, a push or a manual run publishes the container image.
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
