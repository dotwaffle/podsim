# User guide

This guide tells you how to run the simulation and how to use the scenario editor.

## Browser display

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

## Connection diagnostics

Open **Connection diagnostics** at the bottom right of the simulation to inspect its stream.
The panel shows received payload rate, applied updates, mean receive-to-apply time, state and heartbeat age, and reconnect attempts.
An HTTP round-trip probe runs every five seconds while the panel is visible.
Rates exclude HTTP assets and network headers.
Apply time excludes rendering.
State age increases normally while paused.
One-way delay and socket backlog are not measured.

## Controls

### Inspect a pod

- Select a pod number button, or click a pod on the map, to inspect it.
- With more than five pods, select **‹** or **›** to show the previous or next five.
  The text between the arrows gives the page number and the number of pages, for example **3 / 19**.
- Compact fleet numbers match the pod buttons and the map.
  Every number in one fleet has the same width, for example **07** in a fleet of 8 and **007** in a fleet of 287.
  Inspection also shows the full pod ID.
- On a London network, a moving pod's map label gives its next stop under its fleet number, for example **>EUS**.
  With riders aboard, a third line gives the leg origin of the first rider, for example **<KGX**.
- The selected pod shows its activity, speed in whole km/h, occupancy, route, and local waiting reason.
- The display distinguishes a pod ahead, conflicting junction traffic, an occupied berth, and unavailable parking.
  The waiting reason names the blocking pod by its fleet number, for example **Pod ahead / pod 02**.
- With [faults](model.md#faults), the waiting reason can also be **Fault braking**, **Fault stopped**, **Blocked by incident**, or **No forward route**.
- The inspector shows a **Fault** button to the right of the activity.
  It does not show the button during the traffic demo, or after the demo until a reset or a project apply.
  **Fault** starts a fault on the selected pod, with no end.
  On a faulted pod, the button shows **Clear fault** and ends that fault.
  The button is not available while the connection is down or a command waits for its reply.
  The server refuses a pod that it does not support, and the message line shows the error.
- The inspector shows an **Emergency** button on a pod that carries passengers and has no emergency, with the same demo rule (see [emergencies](operations.md#emergencies)).
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
- A white line joins each pod in a [virtual platoon](model.md#virtual-platoons) to the pod ahead of it.
- The map legend shows each color and shape.
- While the selected pod is not idle, the map shows its route in the pod color.
  The server view shows only the unique lanes in the motion window of at most 1,024 lane occurrences.
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
- The largest zoom is 40 times the **Fit** scale.
  When the berths of a station do not separate at that zoom, the largest zoom rises to one zoom step past the zoom at which they separate.
  Examples are the 6-meter slots of a depot and the Compact bays of a London preset.
  Only a station with berths less than 1 meter apart can keep its marker at the largest zoom.

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
See [the London qualification network](london.md) for these rules.

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
  A DLR station code starts with **D**, such as **DBEC** for Beckton, because some DLR suffixes are also Tube suffixes.
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
In other projects, the **Demand** panel does not show the button.
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
  See [repeating daily demand](daily-demand.md) for rates, authoring, and restart behavior.

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
See [Demand and policy comparisons](reference.md#demand-and-policy-comparisons).

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
  See [station layout controls](station-layout-editor.md) for limits and validation.
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
- A generated project with Compact stations and a hand-written project can have station lanes without a station ID and a role.
  When the editor loads such a project, it gives each of these lanes a station ID and a role from the station paths, except the lanes of a depot or a bank station.
  Thus **Remove** and a station move accept these lanes.
  An export and **Pause and apply** keep the new values.
- A generated London or scale100 station is a berth chain, except a London depot or Compact station.
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
  See [repeating daily demand](daily-demand.md).
- Use **Rail arrivals** to set train passenger counts, arrival times, walking delays, and weighted destinations.
  Select its demand pattern to release the fixed plan instead of rate-based traffic.
  See [scheduled rail arrivals](rail-arrivals.md) for bounds, restart behavior, and comparison evidence.
- Use **Rail departures** to set request windows, outbound train times, transfer times, and weighted origins.
  Select **Rail arrivals and departures** to run both plans.
  The demand inspector shows made, missed, unserved, and unresolved connections.
  See [scheduled train connections](rail-connections.md) for bounds and comparison controls.
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
  Unlike the server, which keeps the last value, the import rejects a file that repeats a member name in one object.
  The export is compact JSON.
  The project file must be 43 MiB or smaller.
  This limit includes the image in base64 form, the server's 32 MiB project limit, and an allowance for other fields.
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
A draft that was saved while the live scenario could not load has no server start ID, so the bar does not show this warning.
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
If the browser cannot keep the background, the editor shows an error notice.
Then export the project to keep the background.
When the background has changes that are not saved in the browser, the browser asks before you leave or reload the page.
This includes a removed background that the browser has not yet deleted.
The editor puts the stored background on the page before it accepts input.
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
The editor also accepts a PNG or JPEG image as a background.
The live map does not change the export or the calibration of that image.

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

### Measure geometry

Under **View**, select **Measure selected geometry**.
The overlay is off by default, and it does not change the project, the selection, or the undo history.
Select a guideway to measure its centerline.
Select a station to measure the guideways that have its station ID, and the distance between consecutive berth centers in the order of the berths.
A straight length is the straight line between the draft coordinates.
A curve length is the sum of 16 straight parts of the curve, so it is an approximation.
Berth spacing is a center-to-center distance, not a clearance between pods.
The rulers follow a drag of the selected geometry.
The text below the toggle gives the same values.
The map shows at most 200 rulers.
Missing or invalid geometry shows as unavailable, not as a zero length.
Draft coordinates are not surveyed distances.

### Live station observations

In **Live station observations**, select **Read live snapshot** to read the state of the server one time.
The editor does not poll, and the read does not change the draft or the server.
The response can be large.
Select **Cancel read** to stop a read.
Only one read runs at a time.
A failed or canceled read clears the previous report.

Select a station of the snapshot to show its berths, pods, and pending requests.
The report shows the server start, epoch, project revision, state revision, and simulated tick of the snapshot.
It also tells you when the draft base differs from the snapshot, or when the draft has unsaved changes.
The snapshot can become stale immediately, so read again to update it.

Each berth counts once: occupied, reserved and empty, or free.
A pod belongs to its maneuver station, else the station of its berth, else the station of its guideway, else its reported station.
This does not prove that the pod is inside the station.
The pod categories can overlap.
Pending request counts use the transfer origin of a request when it has one.
They count requests, not passengers.
A missing or malformed value shows as unavailable, not as zero.
The panel lists at most 100 pods and 100 reason groups, and the counts include all of them.
The counts do not identify the cause of congestion.

### Apply a draft

**Pause and apply** is disabled when the draft scenario is the same as the live scenario.
An imported-image change does not enable it, because the server does not get the image.
Live map settings are part of the scenario and do enable it.
Applying a valid draft resets the shared simulation and leaves it paused.
Two drafts do not reset it.
A draft that is the same as the live scenario changes nothing.
See the `project` action in [the protocol](protocol.md).
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

### Checks

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

## Page start and connection

### Page start

While the page downloads the WASM file, it shows the received size in megabytes.
With gzip, the page counts the bytes after decompression, so the count goes up to the WASM size and not to the smaller gzip size.
Without gzip, it shows a percent of the file size instead.
If the download or the start fails, the page shows **Podsim could not start.** and the reason.
If the program stops after it started, the page shows **Podsim stopped.** and asks you to reload the page.

### Connection status

Until the first state frame arrives, the map shows **Connecting to server...** and no network.
A lost connection disables commands.
While the connection is lost, the map is dimmed and an amber banner shows **Connection lost.
Showing state from N s ago.**
N is the time in seconds since the last state frame.
Reconnection restores the current shared state.

When the server restarts with different browser files, the simulation view in each open browser page reloads by itself.
After the editor has opened in the page, the editor keeps its state and its old files until you reload the page.
See [build ID](operations.md#build-id).
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
