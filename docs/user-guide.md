# User guide

This guide explains how to run the simulation in a browser and how to change a network in the scenario editor.
The [README](../README.md) tells how to start the server.
The [simulation model](model.md) explains why pods behave as they do, and the [reference](reference.md) gives the project format and the command-line tools.

An order is the request of one party to travel between two stations.
A lane is a one-way path between two nodes, and the editor controls call it a guideway.
The [glossary](../README.md#glossary) has the other terms.

## Simulation view

The simulation view shows the shared session.
All browsers on one server see the same pods and orders, and a command from one browser affects all of them.
Your map view, your selected pod, your **From** and **To** stations, and the panel that you have open stay local to your browser.

### Display

The network view fills the browser window and renders at the display pixel density.
Resize the window to give the map more space.
Map labels, such as station names and pod labels, have a font size of 10 CSS pixels or more.
In a window smaller than 1100 by 728 CSS pixels, the rest of the view becomes smaller, but the map labels keep their size.

Very large windows can reach the limits of the GPU, and the view then renders fewer pixels so that it still fills the window.
The lines of a small network can lose their antialiasing, because the antialiasing image is twice as wide as the screen and must fit in the largest GPU texture.
This happens, for example, in a 2560 by 1440 window at a device pixel ratio of 2 on a GPU with an 8192 pixel limit.
When the full window does not fit in the largest GPU texture, the view renders at a lower pixel density and scales the image to the window.

The browser can also make the WebGL drawing buffer smaller than the window.
Chrome keeps each side within the GPU limit, and then keeps the buffer at about 33.2 million pixels or less.
A 4000 by 2400 window at a device pixel ratio of 2 gets a 7436 by 4461 buffer.
On a GPU with an 8192 pixel limit, a 4200 by 2400 window at the same ratio gets a 7524 by 4409 buffer, which does not have the proportions of the window.
The view renders at the size of the buffer, so it fills the window without black bars, and it corrects the cursor and touch positions so that clicks and taps still hit the controls.

### Inspect a pod

Select a pod number button, or click a pod on the map, to inspect it.
With more than five pods, **‹** and **›** show the previous or next five, and the text between the arrows gives the page number and the number of pages, for example **3 / 19**.

Fleet numbers match the pod buttons and the map.
Every number in one fleet has the same width, for example **07** in a fleet of 8 and **007** in a fleet of 287.
The inspector also shows the full pod ID.
On a London network, the map label of a moving pod gives its next stop under its fleet number, for example **>EUS**.
With riders aboard, a third line gives the leg origin of the first rider, for example **<KGX**.

The inspector shows the activity of the selected pod, its speed in whole km/h, its occupancy, its route, and the reason that it waits.
The reason tells a pod ahead from conflicting junction traffic, an occupied berth, and unavailable parking, and it names a blocking pod by fleet number, for example **Pod ahead / pod 02**.
A [fault](model.md#faults) adds four more reasons: **Fault braking**, **Fault stopped**, **Blocked by incident**, and **No forward route**.

The inspector has two incident buttons.
**Fault** starts a fault with no end on the selected pod, and on a faulted pod the button reads **Clear fault** and ends that fault.
**Emergency** shows on a pod that carries passengers and has no emergency, and it starts an [emergency](model.md#emergencies) for the first party aboard.
Neither button shows during the traffic demo, or after the demo until a reset or a project apply.
The buttons do not work while the connection is down or while a command waits for its reply.
The server refuses a pod that it does not support, and the message line shows the error.

### Incidents panel

The **Incidents** tab is in the row with **Orders** and **Demand**, and only one of the three panels is open at a time.
The tab does not show during the traffic demo, or after the demo until a reset or a project apply.
While the panel is open, the **Fault** and **Emergency** buttons of the inspector do not show.

The summary lines give these numbers:

- The active faults and emergencies.
- The sampled and the skipped events of each kind.
- The interrupted orders.
- The mean fault length and the rider delay.
- The mean and the longest emergency length.

A length or a delay shows `-` while it has no value, because these numbers move only when the project has a rate of that kind.

Each active incident has a row, and page arrows show when the rows do not fit:

| Row | Content |
| --- | --- |
| Debris | The lane ID, and **Clear**. |
| Pod fault | The pod, the phase, the end time, and **Clear**. |
| Emergency | The pod and the phase. No command cancels an emergency, so this row has no button. |

**Clear** sends the fault ID of the row that the last frame showed.
When that fault has ended, the server refuses the command, and the message line shows the error.

To place debris:

1. Select **Place debris**.
   The button reads **Click a lane**, and a second press stops.
2. Click a lane on the map.
   The nearest lane within reach gets 2 m of debris at the click, and the debris stays until you clear it.

The click selects no pod and no station, and each click ends the arming.
A click that finds no lane sends nothing, and the message line tells you to press **Place debris** again.
The thin lanes of a collapsed station and the lanes out of view are not targets.
The server refuses debris that meets a reserved resource, and the message line shows the error.

### Pod colors and shapes

Pod colors show what a pod is doing, and the map legend shows each color and shape.
The purposes are idle, pickup, passenger service, parking, redistribution, and other empty travel.
The colors stay different with protanopia, deuteranopia, and tritanopia.

| Mark | Meaning |
| --- | --- |
| White filled disc | An idle pod. The white separates it from the gray node dots and berth rings. |
| Filled disc in a color | A pod in passenger service. |
| Ring in a color | A pod that moves empty. |
| Amber ring around a pod | A waiting pod. |
| White ring around a pod | The selected pod. |
| Red ring around a pod | A pod with a fault or an emergency. This ring replaces the amber ring. |
| Red line and red ring on a lane | Active debris. The ring has a fixed size, so it shows at each zoom level. |
| White line between pods | Each pod of a [virtual platoon](model.md#virtual-platoons) joined to the pod ahead of it. |
| Berth ring in a color | The color of a moving or busy pod in the berth. A berth with an idle pod keeps the gray ring. |

The pod number shows above and to the right of the pod.
The next station code shows below the number when it is known, and on a pickup run this code is the pickup station.
Idle pods show only their number.

While the selected pod is not idle, the map shows its route in the pod color, above the stations and their labels and below the pods.
The server sends only the unique lanes in a motion window of at most 1,024 lane occurrences, so a longer route shows in part.
Light arrows on the lanes show the direction of travel, and where two arrows in about the same direction overlap, the map shows only one of them.

### Map navigation

| Action | Control |
| --- | --- |
| Zoom at the pointer | Scroll over the map |
| Pan | Drag the map |
| Zoom at the map center | **+** and **−** |
| Show the whole network | **Fit** |
| Keep the selected pod centered | **Follow** or **F** |

The line above the map names the map input: **CLICK POD / CLICK FROM / SHIFT+CLICK TO / TAB POD / SCROLL ZOOM / DRAG PAN**.
A click, tap, or drag on the map stops following, and so does **Fit**.
**Reset** and **Rewind** keep the map view when the network does not change.
The scale bar below the lower-left corner of the map shows a round distance of 1, 2, or 5 times a power of ten, for example 500 m, 1 km, or 2 km.

On a touch screen, a tap acts as a click, on the buttons and on the stations in the station row too.
Drag one finger on the map to pan.
Pinch with two fingers on the map to zoom at their midpoint, and move the two fingers to pan.
A drag that starts outside the map does nothing, and a touch that the browser cancels, for example when you switch to a different tab or app, is not a tap.

Zoom in to see the individual berths of a crowded station.
Until then, such a station shows one marker, and the map does not show its idle pods, except the selected pod.
The largest zoom is 40 times the **Fit** scale.
When the berths of a station do not separate at that zoom, the largest zoom rises to one zoom step past the zoom at which they separate, for example for the 6-meter slots of a depot or the Compact bays of a London preset.
Only a station with berths less than 1 meter apart can keep its marker at the largest zoom.

### Station labels

An overview label shows occupied berths and nonzero entrance and exit queues.
An expanded label shows occupied, reserved-empty, and free berths, and these counts sum to the station capacity.
Expanded entrance labels show stopped and approaching pods, and exit labels show stopped departing pods.
Berth labels tell a parked pod from an admitted arrival.

Queue counts include dedicated access spurs and exclude general road traffic.
A reserved-empty berth can still have a departing pod that holds its clearance resource.

Where a full label covers a lane, a short label can take its place.
The short label has the station name and the occupied berths, and when pods stop at the entrance or the exit it adds the **In** and **Out** queue line of the overview label.
It keeps its size when pods arrive, depart, or stop, so it does not move.
The full label shows when it covers only a short length of lanes, for example at a corner, and otherwise the label that covers less of the lanes shows.
Zoom in, and the full label shows when it has room.
At the largest zoom, the map shows only the full label.

Expanded labels and berth numbers go on the side of the berths away from the station siding, unless another side covers a much shorter length of lanes.
They stay inside the map, off the berth rings, and off other expanded labels and berth numbers.
A label without a free place does not show until you zoom or pan.
Each text line has a dark backing, so that a lane under the text does not make it hard to read.

Networks with more than 30 stations hide some labels to keep the map readable.
A pod label that covers an overview label or an expanded label does not show, except the label of the selected pod.
An overview label can go where it covers another label or a station marker.
Only the selected pod has a map label until you zoom in to four times the **Fit** scale.
See [the London qualification network](london.md) for these rules.

### Order a journey

Under **ORDER A JOURNEY**, select **From** and **To**, then **Order [Enter]**.
The selected pod makes no difference to an order, because dispatch chooses the pod.

Station choices are alphabetical by name, and you can type a name or code in either field to filter them.
An exact match takes priority.
Otherwise, select a match or type until only one station matches.
**Tab** moves between the fields, and **Enter** in **To** orders the journey when both fields resolve.
Typing in a field does not activate the playback shortcuts.

London uses the three-letter NaPTAN station suffix, such as **CHX** for Charing Cross.
A DLR station code starts with **D**, such as **DBEC** for Beckton, because some DLR suffixes are also Tube suffixes.
**BPS** and **NEL** identify Battersea Power Station and Nine Elms.
Full station IDs also work, and custom networks can use station names or full IDs.

You can also choose stations on the map.
Click a station to make it **From**, and Shift+click a station to make it **To**.
The station row then shows the page with that station.
A click on a pod selects the pod, also when the pod is near a station.
When the stations do not fit in one row, **‹** and **›** show the others.

When **From** and **To** are the same station, **Order [Enter]** is disabled, and the line below them shows **Choose a different destination.** in amber.

The controls beside the order button set who travels.
**−** and **+** set the size of the party, from 1 to 8, or from 1 to 20 with the Express contract.
**Private party** changes to **Share allowed** when you select it, and the party can then ride with other parties that also allow sharing.
A pod takes a party only when the party fits its [class](model.md#vehicle-classes).
When no pod of the fleet can take the party, the order waits in the queue with the status **Waiting for a certified vehicle that fits this party and route**.

An idle pod at the origin serves the order, and otherwise the nearest available empty pod comes to collect the party.
Orders wait when no pod is available.
Boarding takes three simulated seconds, and unloading takes two.
An Express pod adds one second for each passenger after the first, up to 22 s to board and 21 s to unload (see the [vehicle classes](model.md#vehicle-classes)).

Each accepted order shows its order number and opens **Orders**.
For 3 s, the **Order [Enter]** button reads **Order accepted** while **From** and **To** show the stations of that order.

**Orders** lists queued and active orders with their status, and the status names a pod by its fleet number.
Each party aboard a pod is one active order, also in a shared ride.
The panel shows five orders on each page in a window of the minimum size, and a taller window shows more.
With more orders, **‹** and **›** go to the previous or next page, and the text between the arrows gives the page number and the number of pages.

### Statistics

Pickup wait statistics show the average and maximum seconds since the last reset.
The wait ends when boarding starts, so it includes the empty travel of the pod to the pickup, and pending orders contribute their elapsed wait.
Fleet use shows the percentage of pods with assigned or active work and the percentage now in passenger service.

The run status beside the title shows the playback speed, the simulated time, and the number of completed journeys.
While the run is paused, the status is amber and starts with **PAUSED**.
The number of passenger stations and pods shows below it.

### Playback

**Pause**, **Resume**, **Reset**, and **Speed** control playback.
A paused simulation accepts and assigns orders, but it does not advance until you resume it.

Left-click **Speed** or press **S** to cycle through 1x, 2x, 5x, 15x, 30x, and 60x, and right-click **Speed** to step backward.
The server lowers the speed when it cannot keep up, and it never raises the speed by itself.
It tracks five rolling one-second buckets.
If at least two complete buckets reach less than 90% of the selected speed, it selects the next lower speed and shows a notice.
Each reduction starts a new five-second measurement window, and pauses and project loads do not count toward this window.

### Reset

A reset restores the saved scenario fleet and demand settings, clears the orders and reservations, and returns to 1x playback.
It keeps the pause state.

A reset affects every browser, so it needs a second press.
The first press of **Reset** or **Shift+R** shows **Select Reset again within 3 s to reset the shared session.**
A second press within 3 s resets the session and shows **Session reset.**

### Save points and rewind

A save point keeps an exact copy of the simulation, the demand stream, and the project settings in server memory.
Select **Save point** to make one, and select **Rewind** to return to the latest one.
The button shows the simulated time of that save point.
A rewind leaves the session paused and keeps the playback speed.
Because a rewind affects every browser, **Save point** and **Rewind** have no keyboard shortcuts.

The server keeps at most 8 save points, and at the limit a new save point removes the oldest one.
A reset, the traffic demo, and a project apply keep them.
A server restart clears all of them, also with `-state`.

Demand changes count as project changes.
A rewind to a save point from before a demand change restores and saves the earlier demand settings.
The button then reads **Rewind + project**, and the notice after the rewind adds **Project settings restored.**

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

The simulation gets the keyboard focus when the page opens and when it shows again after the editor.
After a click outside the simulation, click the simulation to use the shortcuts again.
While the connection works, no command waits for the server, and the simulation has no keyboard focus, the line below the panels shows **Click the simulation to use keyboard shortcuts** in amber.

The simulation uses Tab and Shift+Tab to select pods, so the keyboard cannot move the focus out of it.
For keyboard users, the page has controls before the simulation and the editor, hidden until they get the focus.
They are **Edit scenario** and **Download debug state** while the simulation shows, and **Simulation** and **Download debug state** while the editor shows.
To reach them, move the focus to the start of the page, for example from the browser address bar, and press Tab.
While the simulation starts, and after a server change to a simulation that uses other page messages, these controls show in the top left corner.
After you select **Simulation** or **← Simulation** with the keyboard, **Edit scenario** gets the focus in its place.

### Debug capture

**Download debug state** in the header of the simulation saves a timestamped JSON file of the current server state.
The capture holds the network, the pod positions and routes, the queues, the berth reservations, and the demand settings.
The download does not pause the run.
Share this file when you report congestion or other unexpected behavior.
It is a diagnostic capture and not a project or a save point, so you cannot load it.

The line below the journey controls shows the result.
The result of a successful download goes after 3 s.
A failure shows in amber and stays until your next action or the next capture.
While the page shows its own **Download debug state** control in the top left corner, the result shows next to that control, and a failure stays there until the next capture.
The header shows **Download debug state** and **Edit scenario** only in the browser page with the editor, and not in the desktop client or in `game.html` as a separate page.

### Traffic demo

The traffic demo shows merge and station queues on the supplied example network.
It needs the unchanged example network and fleet.
In other projects, the **Demand** panel does not show the button.
For a different scenario, the server rejects the demo, and the line below **From** and **To** shows the reason.

To start it, open **Demand**, then select **Start traffic demo** two times within 3 s.
The demo resets the shared session, so the first press only shows **Select Start traffic demo again within 3 s to reset the shared session and start the demo.**
The button does not work while the connection is lost, while a command waits for the server, or while the demo runs.

The demo starts four pods: pod 01 at Harbor, pod 02 at Garden, and pods 03 and 04 in Parking.

1. Pod 01 leaves Harbor for Market.
2. When pod 01 reaches a defined point on the bypass, a supplied trigger starts pod 02 from Garden.
   The same trigger adds Harbor-to-Garden and Garden-to-Harbor orders, which bring the parked pods into service.
3. Both pods contend at the merge, then arrive at the single berth of Market.
   The second arrival waits on the station inlet while the first pod clears its berth.
4. After two journeys finish, four more orders join the queue: Market to Harbor, Market to Garden, Harbor to Market, and Garden to Market.
   Normal dispatch and traffic rules handle these orders.
5. The demo ends after eight passenger journeys and all empty moves finish.

Use 15x speed to see the experiment in about 22 seconds if the server keeps up, or select a lower speed to inspect a queue.

The demo changes what else the session can do.
Starting it resets the current run and turns automatic demand off, and manual orders do not work until it ends.
The demo uses the shared ride and platoon settings of the project, but its orders are private, so no party joins a pod in the demo.
Faults and emergencies are off during the demo and stay off after it until a reset or a project apply.
The four pods stay available after the demo ends, and **Reset** stops the demo and restores the configured fleet.
Demand stays off after the demo until you select **Start demand**, or until a reset restores the configured demand.
After the demo, order a journey from Harbor or Garden to see a parked pod return for pickup.

### Passenger demand

Automatic demand is optional, and it starts off unless the loaded project turns it on.
Open **Demand** to select the rate, traffic pattern, and seed, then select **Start demand**.
Each change in the panel saves to the project at once, and the panel shows **Changes save to the project**.
**Start traffic demo** at the bottom of the panel does not change the project.
With `-project`, **Start demand** saves this setting in the file, so demand starts again after a server restart.

**Rate** cycles through 1, 2, 4, 8, 12, 20, 30, and 60 orders per simulated minute, then starts again at 1.
A project rate that is not in this list, for example 7, shows until the first click, and that click selects the next higher rate in the list, or 1 for a rate above 60.
Rates use simulated minutes, so 15x playback generates orders fifteen times faster in wall time when the server keeps up.

Each click on **Seed** adds 1 to the seed.
Arrivals have equal time intervals, and the seed determines the station choices.
The same seed, settings, initial state, and manual actions produce the same run.

| Pattern | Who travels |
| --- | --- |
| Balanced | Passengers choose among all passenger stations. |
| Destination | Passengers go from the other passenger stations to one station. In the **Demand** panel, this pattern uses the current **To** station, and its label shows that station, for example **Market-bound**. |
| Profile | A project can include weighted origin-destination profiles with named time bands. LondonCentral has eight TfL bands, LondonFull has its six populated 2024 bands, and both select AM peak by default. The label shows the band ID and then the profile ID, for example **am-peak / tfl-numbat-2019-midweek**. The band stays active until the demand settings change. |
| Daily | A daily profile repeats authored bands across simulated days, and gaps and zero-rate bands generate no traffic. See [repeating daily demand](daily-demand.md). |

Generated passengers ride alone, whatever the party limit says, unless the project sets `generatedOrderConsent` to `shared`.
The **Generated passengers** select in the [editor](#demand-and-ride-settings) sets it.

Starting demand or changing enabled settings restarts the stream and its counters.
Pause stops both movement and arrivals, and a reset restores the configured demand.

The queue holds up to 200 pending orders.
A full queue skips generated arrivals and rejects new manual orders.
Skipped arrivals show in the **Demand** panel, and they do not accumulate for a later burst.
If the simulation rejects a generated order, the panel shows the last error in amber.

The panel also reports [redistribution](model.md#redistribution): whether it is on, the number of moves, and the share of all pod distance since reset that pods traveled with no passenger, for example **Redistribution: on / 3 moves / 15.3% distance empty**.
Redistribution moves pods only at a low demand rate, so the panel can show 0 moves while it is on.

### Connection status

The map tells you when the view has no connection to the server.
Until the first state frame arrives, the map shows **Connecting to server...** and no network.
A lost connection disables commands, dims the map, and shows an amber banner: **Connection lost.
Showing state from N s ago.**
N is the time in seconds since the last state frame.
Reconnection restores the current shared state.

Controls wait for server confirmation.
During the wait, the line below the panels shows **Shared session / waiting for command confirmation**.
**From** and **To** send no command, so you can change them during the wait.

When the server restarts with different browser files, the simulation view in each open page reloads by itself.
After the editor has opened in the page, the editor keeps its state and its old files until you reload the page (see [build ID](operations.md#build-id)).
The desktop client shows a message instead, so restart it to load the new version.

The line below **From** and **To** shows these notices for 3 s:

| Event | Notice |
| --- | --- |
| A server restart with the same build | **Server restarted. Save points cleared.** |
| Another browser resets or rewinds the session, starts the traffic demo, or applies a project | **Another browser reset or rewound the session.** |

A reset or a rewind from this browser shows its own notice instead.
If the server restarts before it applies a reset or a rewind from this browser, this browser can wrongly show that another browser did it.
Each server process sends its own start ID, so the view finds a restart even when a hidden tab did not get the first frames after it.

If a new page reports the session client limit, restart the server.
The limit is 1,024 clients for each session (see [command receipts](operations.md#command-receipts)).

### Connection diagnostics

Open **Connection diagnostics** at the bottom right of the simulation to inspect its stream.
The panel shows the received payload rate, the applied updates, the mean receive-to-apply time, the state and heartbeat age, and the reconnect attempts.
An HTTP round-trip probe runs every five seconds while the panel is visible.
Rates exclude HTTP assets and network headers, and apply time excludes rendering.
State age increases normally while the run is paused.
The panel does not measure one-way delay or socket backlog.

### Page start

While the page downloads the WASM file, it shows the received size in megabytes.
With gzip, the page counts the bytes after decompression, so the count goes up to the WASM size and not to the smaller gzip size.
Without gzip, it shows a percent of the file size.
If the download or the start fails, the page shows **Podsim could not start.** and the reason.
If the program stops after it started, the page shows **Podsim stopped.** and asks you to reload the page.

## Scenario editor

The editor changes a local draft of the project, and the running session does not see the draft until you select **Pause and apply**.
Select **Edit scenario** in the header of the simulation to open it.

The editor opens in the same page, and the simulation stays loaded behind it and keeps reading the shared state.
It does not draw while the editor shows, so it does not slow the editor.
To return, select **← Simulation** or use the browser Back button, and the simulation shows the same map view and selected pod as before.
The editor keeps its state while the simulation shows.
To open the page with the editor, add `#editor` to the address, for example <http://127.0.0.1:8080/#editor>.
You can also open `editor.html` as a separate page, and there **← Simulation** opens the simulation page.

The editor loads the live scenario only the first time that it opens in the page.
Until it has the live scenario and the stored background, it ignores input, except **← Simulation**.
If a part of the load fails, it shows an error notice and then accepts input.
If you change the demand in the simulation after that, **Pause and apply** fails with a conflict, and the **Apply conflict** bar lets you load the live scenario (see [Apply a draft](#apply-a-draft)).

**Offline car and energy** in the editor opens a separate local page (see the [offline workflow](offline-workflow.md)).

### Edit the network

A network has stations, junctions, and the lanes between their nodes.
Create stations and explicit junctions, then connect their nodes with lanes.
Crossing lines do not create a junction.
**Add paired lanes** adds both directions.
Drag empty space to pan, and use the wheel to zoom.
**Fit network** shows the whole network, also a large one such as London.

#### Lanes

A small chevron at the middle of each lane shows its direction, at the same size at each zoom.
A lane shorter than 24 screen pixels shows its chevron only when you select it or put the pointer on it.
Two lanes between the same nodes in opposite directions are a pair, and the map draws each one 4 screen pixels to the right of its direction of travel, so that you can see and select each one.

Select a lane to adjust its curve and its speed in km/h.
**Vehicle classes** sets the pod classes that can use it, and a lane with no class list allows Legacy and Compact pods.

Junction ID labels show at 0.5 screen pixels per meter or more.
The selected junction and the start node of a new lane always show their ID label, at a font size of 9 screen pixels or more.

#### Stations

A station shape is a rectangle along the entry-exit line that holds all nodes of the station.
The entry is a hollow square, and the exit is a filled triangle that points in the direction of travel.

Set the berth capacity of a station and place initial pods in free berths.
**Parking station** makes a station serve no passengers, so that idle pods can park in its berths.
A journey cannot start or end at a parking station, and the demand **Destination** list shows only passenger stations.
The network needs at least two passenger stations.

Drag the station shape to move the station and the nodes that only its station lanes use, such as a berth chain.
Drag an entry, exit, or berth node to move only that node, and its lanes stay attached.

**Bearing (degrees)** turns a selected station around the middle of its entry-exit line, and it moves the same nodes.
The bearing is the direction from the entry to the exit, clockwise from up, so up is 0 degrees and right is 90 degrees.
The project file has no bearing field, because the editor gets the bearing from the entry and exit positions.

**Delete station and connections** removes the station, the same nodes, and each demand flow to or from the station in the OD profiles.
When it removes flows, a notice gives their number, for example **Station deleted.
12 demand flows removed.**
Undo restores the station and its flows.

A generated project with Compact stations and a hand-written project can have station lanes without a station ID and a role.
When the editor loads such a project, it gives each of these lanes a station ID and a role from the station paths, except the lanes of a depot or a bank station.
**Remove** and a station move then accept these lanes, and an export and **Pause and apply** keep the new values.

#### Generated station layouts

The controls for a generated London or scale100 station change its layout as a whole.
Select the station to change its berth pitch, entry and exit spacing, or approach setback, then select **Preview in draft** to update the map with one undo step.
The controls keep the mainline throat fixed and keep the nodes, lanes, berths, and fleet placements of the station.
Unsupported layouts keep manual node editing.
See [station layout controls](station-layout-editor.md) for the limits and validation.

#### Berths

**Add physical berth** and **Remove** work in two ways, depending on the shape of the station.
A berth chain is a station where each berth row has an arrival node, a berth node, and a departure node.
A generated London or scale100 station is a berth chain, except a London depot or Compact station.

On a station that is not a berth chain, such as one that the editor made, a new berth goes 30 m past the last berth.
It sits on the station axis, across the entry-exit line, on the side of the other berths, and it gets a lane from the station entry and a lane to the station exit.
The entry and the exit have a lane for each berth, so the node limit below limits the number of berths.

On a berth chain, **Add physical berth** adds one more row at the end of the chain, with three nodes and four lanes: the arrival link, the lane in, the lane out, and the departure link.
The distance and direction from the last row to the new row are the same as from the row before it to the last row.
With one row, the distance is from the middle of the entry-exit line to the berth node.
The new items get the IDs that the scenario generator gives to one more berth, and the four new lanes are station lanes of that station, also when the lanes of the other rows are not.

The editor refuses a change that would break the network, and a message gives the reason.

| Action | Refused when | The message gives |
| --- | --- | --- |
| **Add physical berth** on any station | A node would get more than 64 lanes | The node and the lane limit |
| **Add physical berth** on a berth chain | A new lane crosses another lane or comes nearer than the clearance of the two lanes to it, which is 12 m for small classes and 20 m for large classes. The check also compares the new lanes with each other. It skips two lanes that share a node. | The new lane, the clearance, and the other lane |
| **Remove** on a berth row | A road lane or a lane of a different station uses the berth node. Each lane of the berth node must be a station lane of that station. | The first such lane, the berth node, and the station. Delete that lane first. |
| **Remove** on the last row of a chain | Another lane uses the arrival or departure node of that row | The first such lane, the arrival or departure node that it uses, and the station. Delete that lane first. |
| **Remove** on a berth of a station with banks | The berth is the only berth of the station | The station |
| **Remove** on a berth of a station with banks | No bank of the station has the berth | The berth and the station |
| **Remove** on the only berth of a bank | A road lane or a lane of a different station uses a node that goes with the bank | The first such lane, the node that it uses, the bank, and the station. Delete that lane first. |
| **Remove** on any berth | The berth node has no finite coordinates | The berth node and the station |

**Remove** removes the berth, its node, and the lanes of its node, and on the last row of a chain it removes all of the row.

#### Undo and focus

Use undo and redo for draft changes (see [Editor keyboard shortcuts](#editor-keyboard-shortcuts)).
Undo and redo keep the selection when the draft still has the selected item.
When the draft no longer has it, the editor clears the selection.

The keyboard focus stays close to the item that you changed when you work from the **Selection** section.
A delete with the pointer does not move the focus, and undo and redo do not move it when it is outside the **Selection** section.

| Keyboard action in the **Selection** section | The focus goes to |
| --- | --- |
| **Remove** on a berth row | **Remove** on the next row, or on the previous row after the last row. With one berth left, its **Remove** is disabled, so **Add physical berth**. |
| **Delete station and connections**, **Delete guideway**, or **Delete junction and connections** | The map |
| The Delete key on a button of the section | The map, when the key deletes the selected item |
| Undo or redo, with the selected item still in the draft | The same button. If the button is **Remove** and the berth is gone or the station has one berth left, the place given for **Remove**. |
| Undo or redo, with the selected item gone | The map |

### Demand and ride settings

The editor sets the same demand as the **Demand** panel, and it sets the rules for shared rides and platoons.
You can set the passenger generation option, rate, pattern, destination, OD profile, time band, party limit, shared ride mode, stop limit, platoon limit, seed, and redistribution option.
The stop limit shows only for the drop-offs mode.

The shared ride section has a **Generated passengers** select with **Private** and **Shared**.
**Shared** writes `generatedOrderConsent` to the project, so that generated passengers can share a pod within the party limit.
**Private** deletes the member, so that an exported project has no trace of it.
The setting takes effect when you apply the draft, and an apply that changes only this setting resets the run, as any project apply does.
The native client has no control for this setting.

Three tools author more complex demand:

- **Create a park-and-ride profile** makes weighted morning departures and evening returns, records one undo step, and selects daily demand (see [repeating daily demand](daily-demand.md)).
- **Rail arrivals** sets train passenger counts, arrival times, walking delays, and weighted destinations.
  Select its demand pattern to release the fixed plan in place of rate-based traffic (see [scheduled rail arrivals](rail-arrivals.md)).
- **Rail departures** sets request windows, outbound train times, transfer times, and weighted origins.
  **Rail arrivals and departures** runs both plans, and the demand inspector shows made, missed, unserved, and unresolved connections (see [scheduled train connections](rail-connections.md)).

The editor also has a control for [pickup reassignment](reference.md#pickup-reassignment), and **Pause and apply** activates the draft setting.
The presets and the local draft of the editor turn on pickup reassignment and redistribution.

The **Faults and emergencies** section sets the scenario rates (see [faults](operations.md#faults) and [emergencies](operations.md#emergencies)).
Its fields are **Faults per hour**, **Debris share of faults**, **Debris length (m)**, **Time to evacuation (s)**, **Fault duration** with the values of its kind, and **Emergencies per hour**.
An empty field keeps its default, and a field that you do not change stays out of the project.
A fault rate above 0 needs a duration.
An apply of a changed value rebuilds the fleet.

### Background images

A background image helps you draw over a map, and it stays in your browser.
The server never gets the image, so it does not follow the live project.
If the live project is at a different place, remove or replace the background.

#### Import an image

Import a PNG or JPEG file of 8 MiB or smaller.
The image can be at most 16,384 pixels on each side and at most 67,108,864 pixels in total.
The editor reads the size from the file header before the browser decodes the image, and it checks the decoded size after that.
It does the same checks for a background in an imported project file and for the stored background.
An image that fails a check gets an error notice, and the editor does not use it.

To set the scale, select **Calibrate scale**, select two points on the image, enter their distance in meters, then select **Set scale**.

The editor keeps the images of the undo history in the memory of the tab, up to 128 MiB.
When a new image takes the total past 128 MiB, the editor removes the oldest undo steps and says how many.
It never removes the present image or the image of **Reset draft**.

#### Stored background

The browser keeps the background image with its calibration and opacity in IndexedDB, apart from the saved draft.
It saves 300 ms after each change, for example an image import, a scale calibration, an opacity change, an undo, or a redo.
The browser keeps one background for each server address, so the background stays after a successful apply and after a reload.

| Action | Effect on the stored background |
| --- | --- |
| **Remove background** | Deletes it. |
| A new image | Replaces it. |
| **Import JSON** | Replaces it with the background of the file. A file with no background removes it. |
| **Reset draft** | Puts back the background of the last load, apply, or import. |
| **Undo** | Brings back a replaced or removed background. |
| **Load live scenario**, **Restore draft**, or a live scenario change from another page | None. |

All editor tabs of one server share the stored background, and the last background change in a tab replaces it.
A tab writes it only after a background change in that tab, so a tab that shows an older background does not replace a newer one, also at a scenario edit or when you leave the page.

The exported project file is the only durable copy, and the **Background** section says so.
It also shows the image memory of the tab and the size of the stored image.
If the browser cannot keep the background, the editor shows an error notice, so export the project to keep it.
When the background has changes that are not saved in the browser, including a removal, the browser asks before you leave or reload the page.
If the stored background is not valid, the editor shows an error notice and keeps it until a new background replaces it.
The editor puts the stored background on the page before it accepts input.

The browser keeps the image in the IndexedDB database `podsim-editor-backgrounds-2`, with a random image key, the placement, the opacity, the frame state, and the frame and the license of the image.

#### Georeferenced background

A georeferenced image is a north-up PNG or JPEG with known bounds in degrees, and the editor makes no network request for it.

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
  The reference puts the first node at its place, and the editor rejects the anchor when the second node is more than 2% of the node distance from its place.
- **Adopt the image center**: the center of the image becomes the reference, and the network does not move.
  The network and the image can then fail to align.

The editor rejects bounds that cross the antimeridian, a latitude past 80 degrees, a frame that is not in the 100,000 m square, and a frame with an east-west scale error of more than 0.5%.
The image, its placement, its frame state, and the `geo` reference change in one undo step.
An edit, a drag, or an opacity change during the import stops the import, and the editor says why.

A frame is **attached** when the placement follows the frame, and **Calibrate scale** is disabled for an attached frame.
When an attached frame is not on its place in the `geo` reference, for example after **Load live scenario** with a different `geo`, the **Background** section shows a warning.
Then select **Place from frame** to move the image onto its frame, or **Detach frame** to keep the placement.
The export keeps the frame state, the frame, and the license in `background.asset`, and an import accepts a frame that is not on its place, with the warning.

When the image has an attribution, the map shows it in a line at the lower right corner, with a link to the copyright URL and to the license URL.
The **Background** section shows the source, the license, the time of the import, the method, and the notice.

### Live OpenStreetMap backdrop

The live backdrop draws OpenStreetMap tiles behind the network, and the project keeps only its settings.
Select **Live OpenStreetMap** in the Background panel, then **Enable live map**.
LondonCentral and LondonFull already have geographic references.
For an empty project, enter the latitude and longitude of world position 0, 0.
For an existing network without a reference, anchor two nodes or explicitly adopt the entered origin, which does not move the network.

Pan and zoom to load the visible tiles, and higher zoom levels show more detail.
Podsim reprojects the tiles into the coordinates of the network, between 80 degrees south and 80 degrees north.
**Map opacity** changes the saved map opacity.
**Remove live map** removes the map settings and keeps the geographic reference and any imported image.
These changes support undo and redo.

Select **Pause and apply** to share the map settings with simulation viewers.
The simulation has a **Show map** toggle, remembered in each browser, which does not change the shared project.
Hidden views, a disabled toggle, and zero opacity stop new tile requests, but requests already sent can finish at the provider.

The browser requests OSM Standard raster tiles directly from `tile.openstreetmap.org`, with normal browser HTTP caching.
Only the visible area loads, with up to four requests in flight per view.
Failed requests do not retry by themselves, so select **Retry failed tiles** to retry them.
The network stays usable when tiles are unavailable.

Projects save the provider, the opacity, and the geographic reference, and no tile pixels, so they need internet access to load the map on another machine.
There is no offline tile download, tile proxy, or automatic lane generation.
An imported PNG or JPEG background can stay in place, and the live map does not change its export or its calibration.

Keep the visible OpenStreetMap attribution when you share a map view.
Network geometry traced from OSM can be subject to the [ODbL](https://www.openstreetmap.org/copyright).
The [OSM tile policy](https://operations.osmfoundation.org/policies/tiles/) governs use of the public tile service.
Automated tests use local tile fixtures and make no requests to that service.

### Find a place

Place search moves the editor map to a named place, and it does not change the project, add stations, or create undo steps.
Enter a place name under **View**, then press Enter or select **Search**, and select a result to pan and zoom the map.
The project needs a geographic reference before search is available.

Search needs the native Podsim server, and the default provider is [Nominatim](https://nominatim.org/).
The server shares one request slot across all clients, with at least one second between upstream requests, and it caches up to 128 successful searches for 24 hours.
Busy searches show a wait message, so press **Search** again after the wait.
Search never runs while you type and never retries by itself.
Do not enter personal or confidential information.
Keep the OpenStreetMap attribution and follow the [Nominatim usage policy](https://operations.osmfoundation.org/policies/nominatim/).

The server flag `-geocoding-url=""` turns off external place search, and another value selects a Nominatim-compatible HTTP or HTTPS search endpoint.
The endpoint cannot contain credentials, query parameters, or a fragment.
The server does not follow redirects or use environment proxy settings for searches.
Multiple server deployments need a shared limiter before they use the public provider.
Tests use a local fake provider and make no public searches.

### Measure geometry

The measurement overlay gives lengths from the draft coordinates, and these are not surveyed distances.
Select **Measure selected geometry** under **View**.
The overlay is off by default, and it does not change the project, the selection, or the undo history.

Select a lane to measure its centerline, or a station to measure the lanes that have its station ID and the distance between consecutive berth centers in berth order.
A straight length is the straight line between the draft coordinates.
A curve length is the sum of 16 straight parts of the curve, so it is an approximation.
Berth spacing is a center-to-center distance and not a clearance between pods.

The rulers follow a drag of the selected geometry, and the text below the toggle gives the same values.
The map shows at most 200 rulers.
Missing or invalid geometry shows as unavailable and not as a zero length.

### Live station observations

This panel reads one snapshot of the server state and reports on one station.
In **Live station observations**, select **Read live snapshot**.
The editor does not poll, and the read does not change the draft or the server.
The response can be large, so **Cancel read** stops a read.
Only one read runs at a time, and a failed or canceled read clears the previous report.

Select a station of the snapshot to show its berths, pods, and pending orders.
The report shows the server start, epoch, project revision, state revision, and simulated tick of the snapshot.
It also tells you when the draft base differs from the snapshot, or when the draft has unsaved changes.
The snapshot can become stale immediately, so read again to update it.

Each berth counts once, as occupied, reserved and empty, or free.
A pod belongs to its maneuver station, else the station of its berth, else the station of its lane, else its reported station, so this does not prove that the pod is inside the station.
The pod categories can overlap.
Pending counts use the transfer origin of an order when it has one, and they count orders and not passengers.
A missing or malformed value shows as unavailable and not as zero.
The panel lists at most 100 pods and 100 reason groups, and the counts include all of them.
The counts do not identify the cause of congestion.

### Checks

The editor checks the draft as you work, so that you see a problem before an apply.
It runs the checks 150 ms after the last draft change, also after an undo or a redo, and **Run checks** runs them at once.
The number of errors shows beside **Pause and apply**, for example **2 problems**, and warnings do not count.
Select the number to show the **Checks** section, which lists the errors first and then the warnings.
When the draft has only warnings, the summary tells you that the scenario is ready to apply and gives the number of warnings.

When the checks find errors, **Pause and apply** does not send the draft and shows the **Checks** section.
The server checks the project again when you apply it, and it also checks the [limits](reference.md#validation-and-limits) that the editor does not check.
If the server project fails these checks, the editor still loads it as the draft, and the section shows the geometry errors that the server reports.

Three checks give warnings and not errors: a junction with no lanes, a network section that no lane connects to the other nodes, and a lane with a [large-class tail](model.md#vehicle-classes) that exceeds its length.
The last check names at most 5 lanes and counts the others, and it runs only when the network has no error.
The server accepts a project with these warnings, so a warning does not block an apply or an import.

A passenger station in a separate section is an error, because the other passenger stations cannot reach it.
The editor reports this by groups of stations, where each station in a group can reach each other station.
The largest group is the main group, and each station outside it is cut off and gives one error, in place of one error for each station pair.
When two or more groups have the largest size, the main group is the one with the fewest missing routes to and from the other passenger stations, and then the one with the first station in the project.
The error tells whether the station cannot reach some passenger stations, some cannot reach it, or both, and it gives the number of these stations, or the station name when there is only one.

Select a message to find its item.
A message that names a station, berth, lane, or junction selects that item.
A berth message selects the station of the berth and marks the berth, a pod message selects the station of the pod, and a message about a cut-off station selects that station.
When the item is off the map or near its edge, or when the map scale is below 0.5 screen pixels per meter, the map centers the item at 0.5 screen pixels per meter or more.
In a narrow window, the page also scrolls to the map.

The checks do not take the keyboard focus from you.
When the focus is not on a message, a new run does not move it.
When it is on a message, it goes to the first of these that exists after the run:

1. The same message, also when its text changes, for example the number of stations in a message about a cut-off station.
2. A different message for the same item.
3. The nearest message that is still in the list, the next one first and then the previous one.
4. The message at the same place in the list, or the last message.
5. The **Checks** heading, when no message that selects an item is left.

### London presets

The **London preset** menu loads LondonCentral or LondonFull into the draft.
The server generates the project when you select it, and this takes a few seconds for LondonFull.
The editor then needs several more seconds to check the LondonFull draft and add it to the undo history.
The draft then works as after **Import JSON**: the preset becomes the base of **Reset draft**, and **Undo** brings back the old draft.
When the draft has scenario changes, the editor asks first.

The draft stays as it was when the generation fails or when the server is generating another preset, and a message gives the reason.
The live simulation does not change until you select **Pause and apply**.

When the server started with `-project`, the page shows a warning after the load: **Pause and apply** overwrites the project file of the server.
The warning stays until a successful apply.

### Import and export

**Export JSON** saves the scenario and the optional background as compact JSON, and **Import JSON** restores a draft from a file.
Import accepts the browser export and the server project file, such as the output of `mise run scenario`.
When the file leaves out an optional setting, such as the party limit, the import uses the default value.
A malformed, unsupported, or invalid file does not replace the draft.

The project file must be 43 MiB or smaller.
This limit holds the largest image in base64 form, the server's 32 MiB project limit, an allowance for other fields, and 128 KiB for the image frame and license, rounded up to a whole MiB.
Thus the editor can import an export with the largest project and the largest background image.

Like the server, the import matches member names exactly.
Unlike the server, which keeps the last value, the import rejects a file that repeats a member name in one object.

### Apply a draft

**Pause and apply** sends the draft to the server, resets the shared simulation, and leaves it paused.
A draft that is the same as the live scenario changes nothing, so a second apply of one draft does not reset the session again (see the `project` action in [the protocol](protocol.md)).
The button is disabled when the draft scenario is the same as the live scenario.
An imported image does not enable it, because the server does not get the image, but live map settings do, because they are part of the scenario.

Before the editor pauses the simulation for an apply, it checks the size of the project and shows the limit.
If the apply fails after the editor paused the simulation, the editor resumes it.
A simulation that was paused before the apply stays paused, and the editor does not resume a simulation that restarted after the pause, for example after a project apply from another browser.

The editor applies a draft only to the project revision that it loaded, so that you do not overwrite a change that you have not seen.
A project apply from another page and a demand change in the simulation each make a new revision, and the apply then fails with a conflict and keeps the draft.
A rewind to a save point from before such a change restores that project and rewrites the `-project` file, so an open draft also gets a conflict.

A server restart after the draft started also gives a conflict, because a restart can restore a different project with the same session epoch and project revision.
This includes a restored draft from before the restart.
To read the live project safely, the editor reads the live state before and after it.
It uses the project only when both state reads show the same server start ID and session epoch, and the project has the project revision of the second read.
Otherwise it reads again, for a maximum of three attempts.

After a conflict, the editor reads the live project again, and the **Apply conflict** bar shows the live project revision N and two actions:

- **Load live scenario** makes the live scenario the draft and the base of **Reset draft**.
  When the draft has scenario changes, the editor asks first.
  The page keeps its background, and **Undo** brings back the replaced draft.
- **Apply over revision N** applies the draft over revision N of the live scenario, after it asks, because the draft replaces the live scenario.
  If the live revision is not N any more, or the server restarted again, the apply fails with a conflict again, and the bar shows the new revision.

Both actions read the live project again, with the session epoch and the server start ID, and the next command of the page then uses the epoch of the running server.
A successful apply, **Load live scenario**, and **Reset draft** hide the bar.
If the editor cannot read the live project after a conflict, the status line tells you and the bar does not show, so select **Pause and apply** to try again.

Other failures show the reason from the server, for example a project file that the server cannot save.
When the server is busy with other large commands, it rejects the apply with HTTP 503, and the editor tells you how many seconds to wait before you apply again.
If an editor worker acknowledgment fails, the apply stops and the visible draft stays available for export.

### Saved draft

The browser saves your draft so that a reload does not lose it.
It saves to IndexedDB 300 ms after the last change, and it keeps one saved draft for each server address.
The saved draft has the scenario, the project revision that it is based on, the session epoch of that revision, and the server start ID of that state.
It does not have the background image, which the browser [stores separately](#stored-background).
After a successful apply, the browser deletes the saved draft.

When the editor loads and the saved draft differs from the live scenario, the **Saved draft** bar shows the revision that the draft is based on.
If the live revision is different, the bar also shows it.
If the draft was saved before the server restarted, the bar warns that **Pause and apply** then stops with a conflict.
The editor compares the server start ID of the draft with the latest live state that it read, and each read updates the bar, for example the read of an apply.
While the bar shows, the editor also reads the live state when its window gets the focus.
A draft saved while the live scenario could not load has no server start ID, so the bar does not show this warning.

The bar has two buttons, and until you select one, the browser does not save new changes.

- **Restore draft** puts the saved draft back, and the undo history starts from it.
  The restored draft is not applied, and it keeps its revision and server start ID.
  **Pause and apply** then replaces the live scenario, also when the draft is based on an older revision, and the status line tells you when it replaces newer live changes.
  If the server restarted after the draft started, the apply fails with a [conflict](#apply-a-draft), and the status line says so.
- **Discard draft** deletes the saved draft, and the open editor state stays.
  If you made changes while the bar showed, the browser saves them as the new draft in the same step.

If you apply while the bar shows, the browser keeps the saved draft, and the bar stays.

If the browser cannot save the draft, for example because IndexedDB is not available, the status line tells you, and a failed save or full storage also shows an error notice.
Export the draft to keep it.
When the draft has changes that are not saved in the browser, the browser asks before you leave or reload the page.

All editor tabs of one server share the saved draft.
When a different tab saves its draft or applies, and the draft of this tab has changes, this tab shows a notice, and the browser asks before you leave or reload the page.
Your next change in this tab saves its draft again, and the other tab then shows the notice.

### Editor keyboard shortcuts

| Key or action | Result |
| --- | --- |
| **Ctrl+Z** | Undo |
| **Ctrl+Shift+Z** or **Ctrl+Y** | Redo |
| **Delete** or **Backspace** | Delete the selected station, lane, or junction |
| **Escape** | Cancel a new lane, the move of an item, or a scale calibration |
| Right-click on the map | Cancel a new lane |

On macOS, use Command in place of Ctrl.
The editor ignores the undo, redo, and delete keys when the keyboard focus is in an input field or a drop-down list.
Delete and Backspace do the same as the delete buttons in the **Selection** section.
For an entry, exit, or berth node, these keys show a notice and do not delete the node, so use the station controls.
After Escape during a move, the item goes back to its position before the move.
