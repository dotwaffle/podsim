# First-person ride design

Status: the maintainer approved this design on 2026-11-14.
Milestone M1 is built, and the maintainer accepted it in Chrome on 2026-11-14: the frame rate on LondonFull was good, and the only console error was the missing favicon.
Milestone M2 is built, and the maintainer accepted it in Chrome on 2026-11-14, with a note that the look needs more work.
Milestone M3a is built, and the maintainer accepted it in Chrome on 2026-11-14, after the fixes for the map flicker, the black sky, and the map sharpness.
Milestone M3b is built, and the maintainer accepted it in Chrome on 2026-11-14, after a north-up mini-map, damped turns, a route progress bar, a nearest stop, sharper shadows, and station labels.
Milestone M4 is built, and the maintainer accepted it in Chrome on 2026-11-14.
The station levels are built, and the maintainer accepted them in Chrome on 2026-11-14.
The cabin HUD and the textures are built, and the maintainer accepted them in Chrome on 2026-11-15.
The fix that keeps the cabin front of a 6 m pod at its 4 m size is built, and the maintainer accepted it in Chrome on 2026-11-15.
The [rounded joins](#rounded-joins) are built, and the maintainer accepted them in Chrome on LondonFull on 2026-11-20.
The [visual polish](#visual-polish) of 2026-11-20 is built, and it waits for a check in Chrome.

## Goal

The ride page shows the shared session from a seat at the front of one pod.
It is a showcase: it must look good and be fun to watch.
It does not replace the 2D viewer, and the 2D viewer stays the tool for network analysis.

A later passenger mode lets the user order a trip, wait at the origin station, ride the assigned pod, and arrive at the destination.
This design includes a lite version of that mode, with no walking.

## Decisions

The maintainer made these decisions on 2026-11-14.

| Topic | Decision |
| --- | --- |
| Purpose | A showcase. Visual quality has priority over analysis features. |
| Rendering | Babylon.js draws the scene. A small Go WASM module reuses `internal/remote` for the stream, the decode checks, and the motion buffer. |
| Babylon source | A pinned CDN script tag with a subresource-integrity hash. |
| Placement | A separate page, `/ride.html`, with its own WASM module. The 2D viewer changes only by the **Ride** button. |
| Speed | The page shows the session speed. A "Ride at 1x" button sends the existing `speed` command, which changes the speed for all viewers. |
| Height | All guideway is at one height. At a crossing, pods and beams of the two lanes intersect. |
| Ground | Map tiles on a flat ground plane when the project has a geographic reference, and plain ground otherwise. |
| Models | Procedural meshes at M1 to M4. A generated glTF pod model, `web/pod.glb`, replaced the procedural pod later. The repository holds no authored model files. |
| Pod choice | A pod list, a `?pod=ID` URL, and an auto mode that moves to another pod after a drop-off. |
| Camera | The front seat, mouse look that returns to forward, and a chase camera. |
| HUD | An HTML overlay with speed and state, a trip panel, a mini-map, and a session strip. |
| Passenger | A lite flow: choose stations, send `trip`, wait, ride, and arrive. No walking. |
| Scale | LondonFull is the target from the first milestone. |
| 2D viewer link | Not in the prototype. After M4, the maintainer chose (2026-11-14) a **Ride** button in the browser pod inspector, which opens `/ride.html?pod=<id>` in a new tab. The desktop client has no button. |

## Architecture

### Parts

| Part | Owns |
| --- | --- |
| `cmd/ride` | The `js/wasm` entry point. It starts the client and registers the bridge functions. |
| `internal/ride` | The bridge logic: poses, headings, HUD data, the auto mode choice, the ETA, and the passenger state machine. It has no `syscall/js` code, so native tests cover it. |
| `internal/remote` | The existing client and motion buffer, with no change. |
| `web/ride.html`, `web/ride.js` | The Babylon scene, the cameras, the HTML HUD, the mini-map, and the input. |

`ride.html` loads `wasm_exec.js` and `stream.js`, because the `js` build of `internal/remote` calls `PodsimStream.inflate`.
It also loads `tiles.js` for the ground tiles and Babylon.js 9.36.0 (Apache-2.0) from `https://cdn.babylonjs.com/v9.36.0/babylon.js`.
That file is 8.9 MB before compression.
The [dependency notes](dependencies.md#babylonjs) record the version and the integrity hash.

`internal/remote` does not link Ebitengine, so `ride.wasm` is smaller than `podsim.wasm`.
The `_js.go` and `_native.go` file split follows the pattern of `internal/view`.
No build tags are added.

### Bridge

Go owns the WebSocket connection, the topology fetch, the decode, and the commands.
JavaScript calls into Go and does not decode the stream.
The bridge copies data into typed arrays, so JavaScript does not get one object for each pod in each frame.

| Function | Call rate | Returns |
| --- | --- | --- |
| `ride.topology()` | When `generation` changes | JSON: lanes with end points, control point, speed limit, the distances of the two nodes from the mainline, and the trims of the two ends, the pieces of the rounded joins, the levels, stations with berth positions, the largest party size of a new order, `geo`, and `map`. The module starts the client for the page origin when it loads. |
| `ride.frame(out)` | Each animation frame | It fills a `Float64Array` with 6 numbers for each pod: x, y, heading, speed, class index, and the beam top under the pod. It returns the pod count, the generation, a pod list version, the connection state, and the connection error. |
| `ride.ids()` | When the pod list version changes | The pod IDs in the order of `frame`. |
| `ride.pods()` | When the pod list version changes, and when the user opens the pod list | The ID, class, activity, and first trip of each pod, in the order of the latest state. |
| `ride.hud(podID)` | 4 times a second | JSON: class, activity, speed, lane speed limit, wait reason, `blockedBy`, riders aboard, next stop, route progress, session speed and pause, connection, and the outcome of the last command. |
| `ride.nextPod(podID)` | In auto mode, when the page moves to another pod | The pod that auto mode rides after `podID`, or an empty string. |
| `ride.requestTrip(origin, destination, party, shared)` | On user action | It sends a `trip` order with on-demand service and starts the passenger flow. It returns the error text when the client cannot send the order, or when the page follows a trip already. |
| `ride.passenger()` | 4 times a second | JSON: the phase of the passenger flow, the order ID, the origin, the destination, the station of the platform camera, the pod, and the stop message. |
| `ride.endTrip()` | On user action, and 10 seconds after the arrival | Nothing. The page stops following the order, and the order stays in the session. |
| `ride.setSpeed(speed)` | On user action | It sends a `speed` command for the session that the page shows, through the retry-safe command path of `remote.Client`. It returns the error text when the client cannot send the command. The HUD shows the reply. |

`frame` uses `remote.Motion.Sample`, so the 3D view has the same 250 ms delay as the 2D view.

### Heading

`sim.Pod` has no heading.
`remote.Motion.Sample` interpolates the position and the speed along the route, but it keeps `laneID` and `laneDistance` of the earlier frame.
Thus `internal/ride` keeps an anchor position and a heading for each pod.
When the pod is 10 cm or more from its anchor, the heading becomes the direction from the anchor to the pod, and the pod position becomes the new anchor.
A pod that moved less keeps its heading, so a stopped or a slow pod does not turn, and the result does not depend on the frame rate.
A pod on a reverse lane of a bay faces against its direction of travel, so its heading turns by pi.
A new pod, or a pod that moved more than 50 m in one frame, gets the tangent of its lane at `laneDistance`.
A pod at a berth has no lane, so it gets the end direction of a lane that arrives at the berth node and is not a reverse lane.
A lane is a line or a quadratic Bezier curve with one control point, so the tangent has an exact formula.
LondonFull has about 2,650 joins of two mainline lanes that turn by more than 0.5 rad, and pods pass them at full speed.
The yaw of the seat camera, the chase camera, the other pod meshes, and the mini-map arrow follows the heading as a critically damped spring.
A turn starts and ends with a low rate, and the peak rate is about 74% of the start rate of a first-order filter with the same lag.
On a steady turn, the yaw lags by 350 ms for the seat camera and the mini-map arrow, 600 ms for the chase camera, and 200 ms for the other pods.
In the M1 check, the seat camera used 150 ms, and the maintainer found the turns smooth but a bit quick.
In the M3b check, a first-order filter started each turn at its full rate, and the maintainer asked for damping at the start of the turn as well.

### Coordinates

The 2D view draws world y down the screen, so world y points south.
Babylon uses a left-handed system with y up.
The page maps world (x, y) to Babylon (x, h, -y), so +x is east and +z is north.
Babylon stores vertex positions as float32.
At 20 km from the origin, float32 resolution is about 2 mm, and the seat camera is less than 1 m from the cabin, so the cabin can jitter.
LondonFull extends about 34 km west of its origin.
The page creates the Babylon engine with `useLargeWorldRendering`, which draws the scene relative to the camera.
The cabin is one mesh at the ridden pod, and the large-world option draws it relative to the camera, so float32 resolution does not move it.
The beams, the ground, and the pod instances keep absolute positions, and the large-world option can miss some of them.
The M1 acceptance includes a ride near x = -33 km with no shimmer at the edge of the beam ahead of the pod.
If the beam edge shimmers, the page builds each mesh around a local origin, and the bridge subtracts an origin near the camera in float64 before it fills the array.
A test pins the axis mapping with a station pair of known compass direction.

## Scene

- Guideway: one box-section beam for each lane, extruded along the line or curve, at a fixed height of 5 m on tapered pillars about every 60 m.
  A lane beam stops at the trims of its ends, and each piece of a [rounded join](#rounded-joins) has its own beam.
  The page merges the beams into a few meshes, so LondonFull does not have 20,000 draw calls.
  The [visual polish](#visual-polish) tapers the beam sides and adds a light rail along the beam tops.
  Vertex colors make the running surface on top darker than the concrete sides.
- Pods: the page loads `web/pod.glb`, a glTF model that `internal/cmd/podmodel` writes.
  The model is 4 m long, 1.5 m wide, and 2.1 m tall, with the parts `shell`, `glass`, `gear`, `cabin`, and `seat`, an `eye` node at the seat, and four screen nodes.
  The section [Cabin HUD and textures](#cabin-hud-and-textures) gives the screens and the textures.
  The body is a loft of rounded sections, with side windows above a belt line, a glass dome at each end, and a pillar ring at the middle and near each end.
  The cabin is the inside of the shell, 4 cm in, and a dashboard.
  For each class, the page stretches the parts to the class length and gives the shell the class color.
  The shell, the glass, and the gear merge into one mesh with one submesh for each material, and thin instances draw all pods of a class in a few calls.
  The page reads the GLB file itself, because it needs only a few accessors of one buffer, so it does not load the Babylon glTF loader.
  glTF is right-handed and Babylon is left-handed, so the page changes the sign of x.
  With the same index order, the triangles then wind as the Babylon meshes do, and a Node test checks each triangle of the file.
  If the file does not load, the page draws procedural pods and a cabin frame that moves with the camera.
  The visual polish adds sky reflections and lights to the pods.
- Stations: a platform slab under the berths of each station.
  A pool of 12 name signs goes to the stations within 900 m of the camera, because a texture for each of 600 stations uses too much memory.
  The visual polish gives the signs the style of the cabin screens.
- Ground: map tiles when `geo` is present, from the tile source of `web/tiles.js`, with the OpenStreetMap credit on the page.
  The ride page does not need the `map` setting that the 2D view needs, but it refuses a `map` setting that is not valid.
  Three rings surround the ridden pod: zoom 18 tiles to 300 m, zoom 16 tiles to 1.2 km, and zoom 14 tiles to the fog distance.
  A zoom 18 pixel is about 0.37 m at London, and the tiles have 16x anisotropic filtering, because the camera sees the ground at a low angle.
  In the second M3a check, zoom 17 tiles near the pod looked pixelated.
  The page asks for at most 12 new tiles each second, nearest ring first.
  The sky, the plain ground, and the rings from far to near draw first, in that order, and write no depth.
  Nothing is below the ground, so the other meshes draw over them, and the layers cannot fight in the depth buffer.
  In the first M3a check, layers a few centimeters apart with a depth offset flickered.
  Without `geo`, the ground is a plain textured plane.
- Light and sky: a warm sun as a directional light, a hemispheric fill light, a sky dome with a vertical gradient, and distance fog in the horizon color.
  Fog hides the far guideway, so the draw distance can stay at about 3 km.
  The visual polish adds a haze, a glow around the sun, an exponential fog, and a dusk light.
- Shadows: one 4,096-pixel shadow map covers a 320 m square, with texels of about 8 cm and high-quality PCF filtering.
  The square is centered 120 m ahead of the camera, along the horizontal view direction, so the shadows reach about 280 m ahead and 40 m behind.
  The light moves in whole texels across its view, so the shadow edges do not crawl when the camera moves.
  In the M3b check, a 2,048-pixel map with medium filtering gave jagged edges.
  A 4,096-pixel map over a 260 m square around the camera fixed the edges, but the shadows stopped about 130 m ahead, and the maintainer asked for twice the distance.
  Four cascaded shadow maps reached 260 m, but on the maintainer's GPU each cascade boundary drew a thin line across the ground.
  The line stayed with no cascade blend, with no stabilization, with no depth clamp, and with no PCF, so the page does not use cascades.
  The pods, the beams, and the pillars cast shadows, and the ground, the tiles, the beams, and the platforms receive them.
- Image: Babylon's default rendering pipeline with 4x multisampling, FXAA, and ACES tone mapping.
  In the check of the cascaded shadows, a weak bloom (threshold 0.85, weight 0.2) spread the bright map tiles into soft blotches on the edges of the dark cabin pillars, so the page has no bloom.
  The visual polish adds a vignette and more color saturation in the same pass.
- Crossings: the simulation does not control a crossing of two lanes with no shared node, so a pod passes through a pod on the other lane.
  On LondonFull with the wide stations of station scale 0.5, 6,712 such crossings are less than 3.2 m apart in height after the station levels: 2,905 between mainline lanes, 3,097 between a mainline lane and a station lane, and 710 between station lanes.
  The tight stations of November 16, 2026, do not change the mainline lanes, and nobody counted the crossings of their station lanes again.
  Most mainline crossings are in hubs: a ring of nodes where each entry joins each exit by a straight chord, so the chords cross in the middle.
  Of the mainline crossings, the median one has 132 other mainline crossings within 150 m, and only 11 have none, so a flyover for each crossing does not fit.
- Hubs: the page groups mainline lanes that cross with no shared node, and a group with at least 10 crossings and 4 lanes is a hub.
  A lane longer than 400 m passes a hub, so it is not part of one.
  A hub draws as one deck over the convex hull of the lane end points, 5 cm below the beam top, with a pillar at each corner, in place of the chord beams.
  The pods stay at the mainline height.
  On LondonFull, 37 hubs hold 2,694 of the 2,905 mainline crossings, a typical hub is an octagon 125 m wide, and the search takes about 35 ms in Node.
  The maintainer chose the deck on 2026-11-14, and the real fix of a hub belongs to the [London junction design](london-junctions.md), because it changes the model.

## Cameras and input

- Seat camera: at the eye of the pod model, a little behind the front dome, facing forward.
  While the user holds the mouse button, mouse movement turns the view.
  On release, the view returns to forward with a 300 ms time constant.
- Chase camera: 14 m behind the pod and 5 m above its roof.
  The `C` key switches between the seat camera and the chase camera.
  The chase view draws the ridden pod, and the seat view draws the cabin of the ridden pod.
  The cabin and the eye turn with the seat yaw, so the cabin does not move in the view.
  The sun does not light the cabin, and an emissive color stands in for the light from the windows.
- Platform camera: at a station, used by the passenger flow.
  It stands 22 m to the side of the line through the first and the last berth, 10 m back, and 4 m above the guideway, and it looks at the center of the berths.
  Mouse look works in this view too.

## Pod choice

The page opens on the pod in `?pod=ID`, or else shows the pod list.
The list shows the ID, the class, the activity, and the origin and destination of the first rider.

In auto mode, after the last rider of the ridden pod leaves, the page moves to another pod with a fade.
The Auto box and `?auto=1` turn on auto mode.
When the user turns on auto mode on a pod with no riders, the page moves at once.
It takes the nearest pod that carries riders.
If there is no such pod, it takes the nearest pod that a pending trip waits for.
If there is neither, it stays on the current pod.
Ties go to the lowest pod ID, so the choice is repeatable for a given state.

If the ridden pod leaves the fleet, for example after a project apply, the page shows the pod list.

## HUD

- Speed and state: the speed, the speed limit of the lane, the activity, and the wait reason with `blockedBy` when the pod is stopped.
- Trip panel: the party size, origin, and destination of each party aboard, the next stop, the remaining distance, and an ETA.
  The next stop is the first stop of the pod, or else the destination of the first party aboard.
  The client gets the route window as `Vehicle.Presentation`, and the ETA uses the lengths and speed limits of its lanes from the pod lane on, with no waits.
  If the route continues past the window, the panel shows "over" the window distance and time.
  A bar shows the progress along the current route: the distance behind the pod over the route length in the window.
  Each leg of a shared ride is its own route, so the bar starts again at each stop.
- Nearest stop: the name of the nearest passenger station to the ridden pod and its distance, so the rider can get their bearings.
  The center of the berths of a station is its position, and parking-only stations do not count.
  The maintainer asked for the bar and the nearest stop in the M3b check.
- Mini-map: a round 2D canvas that shows 1.5 km around the ridden pod, with north up.
  The first version showed 600 m, and in the M3b check the maintainer asked for a wider view.
  Up to 16 station labels show the station name without a part in brackets, cut at a word boundary to at most 14 characters.
  The project has no short station codes, and the LondonFull station IDs are NaPTAN codes such as `940GZZDLCAN`.
  Nearer stations get their labels first, and a label that overlaps another label or the pod arrow is not drawn.
  It shows the lanes, the stations, the other pods in their class colors, and a north mark, and it draws 10 times a second.
  A grid of 500 m cells holds the lane segments, so a draw reads only the segments near the pod.
  On LondonFull, the grid takes 16 ms to build, and a draw reads about 460 of the 11,509 segments in less than 1 ms.
- Session strip: the session speed, the pause state, the connection state, the "Ride at 1x" button, and the outcome of the last command.
  The diagnostics line shows the frame rate.

## Passenger flow

The flow uses the existing `trip` command and the `pending` and `riders` data of the stream.
It needs no server change.

1. Menu: the **Trip** button opens the menu, where the user chooses an origin station, a destination station, a party size, and the consent.
   The origin starts at the nearest stop to the view.
   On an `express-v1` project, the page sends an on-demand trip with the `orderContract` marker.
2. Requested: the page sends `trip` and keeps the `orderID` from the acknowledgment.
3. Waiting: the platform camera shows the origin station.
   The HUD shows the assigned `podID` of the pending order.
   Pickup reassignment can change the `podID`, and the HUD follows the change.
4. Riding: when the order is in the `riders` of a pod with `boardedTick`, the seat camera moves to that pod.
5. Transfer: when the order waits at `legFrom`, the flow goes back to Waiting at that station.
6. Arrived: when the rider has `completed`, the platform camera shows the destination station for 10 seconds.
   Then the flow returns to the menu.

The flow stops and shows a message in these cases:

- The server rejects the `trip`, for example during the demo or when the queue holds 200 orders.
- The order is not in `pending` or in any `riders`, and it did not complete.
  A reset, a rewind, a project apply, an interruption, or a restore can cause this.
- The epoch or the generation changes.

The page logs each state change of the flow to the browser console.

The page finds the order in `pending` first, then aboard a pod, and then in the completed riders, because a party that changes pods keeps a completed record on its first pod.
A party that is in the `riders` of a pod without `boardedTick` waits for that pod at its station.
Until the state reaches the revision of the reply that accepted the order, a missing order does not stop the flow.
A change between the platform camera and the seat camera fades to black and back.
During a trip, the pod list and auto mode are off.
The **Leave trip** button stops the flow, and the order stays in the session, because the protocol has no command to cancel an order.

## Failure modes

| Case | Behavior |
| --- | --- |
| The CDN script does not load or fails its integrity check | The page shows "Babylon.js did not load" and the CDN URL. The 2D viewer is not affected. |
| WebGL is not available | The page shows a message and does not start the client. |
| The stream disconnects | `remote.Client` reconnects. The pods stop moving, and the session strip shows the connection state. |
| A new generation or epoch | The page fetches the topology again, rebuilds the scene, and keeps the ridden pod if it still exists. |
| The ridden pod is in a fault or an emergency | The HUD shows the wait reason. The camera stays on the pod. |

## Observability

The session strip shows the frame rate.
A diagnostics panel shows the frame time, the time of `ride.frame`, the draw calls, the pod count, and the stream rate from `web/stream.js`.
The passenger flow logs its state changes.

## Build and checks

- `internal/cmd/buildweb` builds `ride.wasm.gz` from `./cmd/ride` and copies `ride.html`, `ride.js`, `pod.glb`, and the files in `web/textures`.
- `go run ./internal/cmd/podmodel` writes `web/pod.glb`.
  `TestModelFile` fails when the file is not the output of the generator, and `go test ./internal/cmd/podmodel -run TestModelFile -update-model` writes it again.
- The wasm dependency check in `mise.toml` adds `./cmd/ride`, so that `ride.wasm` does not link cloud storage or the preset data.
- `govulncheck` for `js/wasm` adds `./cmd/ride`.
- Go tests in `internal/ride`: the heading on lines and curves, the frame packing, the HUD data, the ETA, the auto mode choice, and each step and stop case of the passenger flow.
- Go tests in `internal/cmd/podmodel`: the GLB structure, the node names, the bounds of the model, the winding of each triangle, what the rider sees from the eye in each direction, the screens, and the texture coordinates.
- Go tests in `internal/cmd/texgen`: the texture files, the tiling, and the brightness of the textures.
- Node tests (`mise run test:web`): the beam path points, the coordinate mapping, the camera smoothing, the HUD formatting, the read of the pod model, the cabin screens, the stop ladder, the texture fallback, the overlay layout, and the parts of the visual polish.
- CI has no GPU, so no test draws the scene.
- The maintainer checks each milestone in desktop Chrome.
  The performance target is 60 frames per second on LondonFull at 1x on the maintainer's GPU.
  Software rendering is not evidence for this target.

## Milestones

1. M1: the bridge, `/ride.html` with Babylon, the guideway, the pods, the stations, plain ground, and the seat camera on a chosen pod.
   Acceptance: LondonFull at 60 frames per second, no camera yaw jump at lane joins, and no beam shimmer at the far edge of the network.
2. M2: the HUD speed and state, the trip panel, the session strip, mouse look, and the chase camera.
3. M3a: the look: map tiles, the sky, shadows, station signs, the pod and beam detail, and the image pipeline.
   The maintainer chose on 2026-11-14 to do the look before the rest of M3.
4. M3b: the mini-map and auto mode.
5. M4: the passenger flow.

## Station levels

The maintainer chose on 2026-11-14 to lower the stations after M4, because the station lanes at the guideway height looked messy.
The levels are built, and the maintainer accepted them in Chrome on 2026-11-14.
On LondonFull, 5,216 of the 9,024 lanes are station lanes, a median of 15 for each station, and 272 lanes have a reverse twin on the same nodes.
The page derives each height from the network, and the model stores no elevation.

- A mainline node is a node of a lane with no station.
  The distance of a node is the shortest path to a mainline node over station lanes, in either direction, at most the ramp length.
  A node that no mainline node reaches has the ramp length.
- The beam top is 5 m at distance 0 and 1 m at the ramp length of 120 m.
  Between them, the height follows a smoothstep curve, so a ramp starts and ends level, and its steepest grade is 5%.
- A point on a lane has the distance of its two nodes, interpolated by the fraction of the lane length.
  A pod at a berth has the distance of the berth node.
- On LondonFull, the berth nodes that are not on a mainline node are 105.5 m to 269.6 m from the mainline.
  9 berths, each at a different station, are less than the ramp length from the mainline, so they are on a ramp.
  The other berths are at the station level.
  With wide stations, these berths were 188 m to 383 m from the mainline, and each was at the station level.
  68 berths are on mainline nodes, and they stay at the guideway height.
  The 3 station lanes between two mainline nodes are through lanes of banks, and they stay at the guideway height too.
- Go computes the heights in `internal/ride`.
  The topology gives the two node distances of each lane and the level values, and `ride.frame` gives the height of each pod.
  The page draws the beams with the same curve, and a test fixture that both test suites read pins the two versions of the curve.
- A lane with a reverse twin on the same nodes, with the same control point, draws once.
- A beam with its bottom less than 1 m above the ground has no pillars.
  Each other pillar reaches the bottom of its beam.
- The platform slabs, the station signs, and the platform camera use the height of their station: the mean height of its berths.
- The pods stay level on a ramp.
  At a grade of at most 5%, a pitch of less than 3 degrees does not show.
- On LondonFull, 292 of the 312 stations are at the station level, against 301 with wide stations, and 136 pairs of twin lanes draw once.
  A pod on a ramp is within 1 cm of the beam that the page draws under it.
  The beams and the pillars take about 240 ms to build in Node.

## Rounded joins

On the ride page, the beams and the pods round the corners where lanes join at a node.
The change is for display only.
The simulation, the protocol, the 2D view, the lane lengths, and the route distances keep the sharp joins.

- `internal/ride` computes the joins, and the topology gives them to the page.
- For each node, the bridge compares the end direction of each arriving lane with the start direction of each leaving lane.
  A curved lane uses the exact tangent of its quadratic curve.
  Where the control point of a curve is on an end, the tangent there is zero, and the bridge uses its limit: the direction from the start to the end of the curve.
  Only a lane with no length has no direction.
- A node gets rounded joins when at least one pair turns by 3 to 150 degrees.
  A node keeps its sharp joins when it is a berth node, when a pair turns by more than 150 degrees, as at a reversal or a bay end, or when a reverse lane of a maneuver group touches it.
  A lane and its reverse twin turn by 180 degrees, so a twin pair never gets a rounded join.
- At a rounded node, each lane end gets a trim of 8 m, or 40% of the lane length when that is shorter, in whole millimeters, so the two trims of a lane do not overlap.
- Each pair of an arriving and a leaving lane gets a piece: a quadratic curve from the trim point of the arriving lane to the trim point of the leaving lane.
  The trim points are on the sample lines that the simulation moves a pod on.
  The control point is where the tangent lines at the two trim points meet, ahead of the start, behind the end, and at most twice the trims from either end.
  Two straight lanes meet at the node, so the node is the control point, also when the two lanes are on one line.
  A pair that turns by less than 3 degrees gets a piece too, so a pod that goes straight on has a beam under it.
- Each piece must continue the direction of its two lanes, within 1e-6 rad at both ends.
  When one piece of a node does not, the whole node keeps its sharp joins, with no trims and no pieces.
  On LondonFull, no node fails this check.
- The topology gives the two trims of each lane and, for each piece, the two lane indexes and the control point, rounded to 1 mm.
  The page finds the two ends of a piece at the trims on the sample lines, as the bridge does, so a lane beam meets its piece to float precision.
  The rounded control point moves a piece by less than 1 mm from the curve that the bridge puts the pods on.
- The page draws each lane beam between its trims, and one beam for each piece.
  The pillars stay under the lane beams, so the pieces have no pillars.
  A piece to or from a hub lane does not draw, because the hub deck replaces the hub lanes, and the other lane then keeps its full length at that node, so its beam meets the deck.
- A point at a fraction of the arc length of a piece has the beam top of the point at the same fraction of the two lane parts that the piece replaces.
- `remote.Motion.Sample` keeps the lane and the lane distance of the earlier frame, so the bridge finds the lane and the distance of the sampled position on the route of the pod.
  The search starts at the lane of the pod and goes forward along the route window, at most 100 m.
  When the position is at two places of that part of the route, or the route holds the pod lane more than once, the pod keeps its sampled position.
- A pod in the trim at the end of its lane, with a next lane on its route, moves to the piece to that lane.
  A pod in the trim at the start of its lane, with a previous lane on its route or from the frame before, moves to the piece from that lane.
  The progress along the two lane parts maps to the same fraction of the arc length of the piece.
  A pod with no known lane on the other side of the join keeps its simulated position.
  It keeps that position until it leaves the trims of that join, also when the lane on the other side becomes known, so it does not jump onto the piece.
- The heading follows the drawn position, so a pod turns along the piece.
  A new pod on a piece, or a pod after a jump, takes the direction of the piece.
  The height of a pod uses the lane and the distance of its sampled position, as before.
- At a 90 degree join of two straight lanes with 8 m trims, a pod half way along the piece is 2.83 m from its simulated position at the node.
  Elsewhere on the piece the offset is smaller.
- On LondonFull, 4,084 of the 5,740 nodes get rounded joins, 8,664 of the 9,024 lanes get a trim, and the topology has 10,380 pieces.
  No control point is more than 0.61 m from its node.
  The trims and the pieces add about 0.74 MB to the 2.1 MB of topology JSON.
- A test fixture that both test suites read, `internal/ride/testdata/fillets.json`, pins the trims and the pieces of joins of curved lanes on a ramp and of a lane whose trim is not a whole number of millimeters before the rounding.
- In a hub, a pod still rounds the corner from an approach lane onto a hub lane, but the approach beam stays straight to the node.
  The bridge does not know the hubs, because the page finds them.

## Cabin HUD and textures

The maintainer asked on 2026-11-15 for a HUD that looks more like a game and for textures that make the scene more appealing.
The maintainer made these decisions on 2026-11-15.
The HUD is an original design, and it copies the look of no game.

| Topic | Decision |
| --- | --- |
| Style | A racing layout: a large speed readout with an arc gauge, a round mini-map, a next-stop banner with the ETA, and a stop ladder for the route progress. |
| Placement | In the seat view, the readouts are in the cabin. A head-up banner on the windscreen shows the next stop and the ETA. Screens on the dashboard show the speed gauge, the mini-map, the stop ladder, the wait reason, and the riders aboard. |
| Controls | The buttons and pickers stay a slim screen overlay, restyled to match: the pod picker, auto mode, **Ride at 1x**, the trip menu, and the session strip. |
| Chase view | The chase view shows the same readouts as a screen overlay in the racing layout. |
| Textures | Generated in the repository, as `web/pod.glb` is. No photographs and no third-party images. |

### HUD

- The data does not change: the `ride.hud` fields, the ETA, the route window, the nearest stop, and the mini-map data stay as they are.
- The page draws each cabin screen on a canvas and shows it as a dynamic texture on a plane in the cabin.
  It draws the screens when the HUD data changes, at most 4 times a second, and the mini-map 10 times a second.
  The page needs no Babylon GUI script.
- The pod model gets named screen nodes on the dashboard and a banner node at the top of the windscreen, so the model owns their places.
  The model tests pin the names and check that the screens do not hide the view ahead.
- The screens are emissive, so they stay readable at night and in shade.
  They move with the cabin when the user looks around.
- A text that does not fit its screen is cut at a word boundary, as the mini-map labels are.

### Textures

- A generator in the repository writes small tiling textures as PNG files under `web/`, with the standard library only.
  A test fails when a file does not match the generator output, as `TestModelFile` does for the pod model.
- The textures: concrete for the beams, the pillars, and the station decks; panel lines and a livery stripe for the pod shell; a fabric weave for the seats; and a matte grain for the dashboard.
- The pod model gets texture coordinates for the shell and the cabin.
- The class color of a pod still tints its shell.
- The page falls back to the flat colors when a texture does not load.

### Checks

- Go tests: the texture files match the generator, each texture tiles without a seam, and the model has the screen nodes and texture coordinates in range.
- Node tests: the layout of each screen, the text cut, the stop ladder, and the overlay layout of the chase view.
- The maintainer checks the look in Chrome.

### Built

The cabin HUD and the textures are built, and the maintainer has not checked them in Chrome yet.

The pod model has these screen nodes, after the `eye` node.
Each node has a translation, a rotation, and a scale, and no mesh.
In the frame of its node, a screen is the unit square in the x-y plane, scaled by the node scale, with its face toward -z.
The page builds each screen from these values, with the x sign change.
A longer class does not change the place of a screen from the eye.

| Node | Size | Place, from the eye | Readouts |
| --- | --- | --- | --- |
| `mapScreen` | 0.18 m by 0.18 m | 0.6 m away, 31 degrees left, 20 degrees down | The mini-map. |
| `speedScreen` | 0.27 m by 0.16 m | 0.6 m away, ahead, 23 degrees down | The speed gauge, the speed, the lane limit, the activity, and the pod. |
| `routeScreen` | 0.24 m by 0.16 m | 0.6 m away, 31 degrees right, 20 degrees down | The distance, the stop ladder, the wait reason with `blockedBy`, the riders aboard, and the nearest stop. |
| `banner` | 0.6 m by 0.12 m | 0.85 m away, ahead, 19 degrees up | The next stop and the time to it, on a band that the view shows through. |

- The dashboard is wider and higher than before, and each dashboard screen has a frame with a stand in the dashboard.
- The screens are below the sight line 0.2 down from ahead, and the banner is below the sight line 0.5 up through the front dome.
  The sight lines of the model tests do not change.
  A model test checks that the rider sees each screen corner first on its sight line, so no part hides a screen, and no screen is outside the cabin.
- The new `seat` part has the seat of the rider and a bench at the rear that faces forward.
  The seat backs stay below the eye, so the rider sees out of the rear window.
- A class that is longer than the 4 m model stretches only behind a cut at z = -0.2 m of the model.
  The cut is between the back of the rider seat and the rear bench.
- Ahead of the cut, the parts move to the new front and keep their shape.
  So the eye, the dashboard, the screens, the banner, the rider seat, and the middle pillar have the same place from the front of the pod in each class.
- Behind the cut, the shell, the glass, the gear, the cabin, and the bench stretch to the new rear.
  In a 6 m pod, the stretch there is about 2.1, and the textures stretch with the parts.
  The bench is then about 1 m deep, and the rear nose is about 1.9 m long.
- The 4 m class does not change.
- `STRETCH_CUT` in `web/ride.js` and `stretchCut` in the model tests have the same value.
  `internal/cmd/podmodel/testdata/stretch.json` has sample values of the stretch, and a node test compares the page with them.
  `go test ./internal/cmd/podmodel -run TestStretchFixture -update-stretch` writes the file again.
- The sight-line test and the screen test of the model run for the 4 m class and the 6 m class, with the same sight lines.
  A test checks that the eye sees each screen corner at the same place in both classes.
- A screen canvas has 2,000 pixels for each meter of the screen size, for example 540 by 320 pixels for `speedScreen`.
- The page draws the screens when the data changes, at most 4 times a second, and the map screen 10 times a second.
  The map screen and the overlay map use the same draw function, which takes the canvas size.
- A station name that does not fit first loses the part in brackets.
  Then the word cut of the mini-map labels makes a text shorter.
- The route screen shows the wait reason with `blockedBy` in brackets.
  When the line does not fit, `blockedBy` is cut first, so the reason stays whole.
- With many stops, the rows of the ladder get smaller, so they do not overlap.
- The overlay of the chase view and of the platform camera has the next stop and the time at the top center, the stop ladder under it, the speed gauge at the bottom center, the round mini-map at the bottom left, and the wait reason, the parties aboard, and the nearest stop at the bottom right.
- The controls are at the top left, with the trip status under them.
  The session strip is at the top right in all views, because the seat view also needs the session state.
- The seat view uses the overlay when the pod model did not load, because the model has the screen nodes.
- The stop ladder uses the data that the page has: the next stop, and the origin and the destination of each party aboard.
  The bridge does not send the stops of the route window.
  The stops behind the pod are the origins of the parties aboard, at most two.
  After the next stop come the other destinations of the parties aboard, at most three.
  The order is the order of the riders.
  With at most one stop behind and none after the next stop, the ladder is a bar from the origin to the next stop.
  The bar shows the progress of the current route.

The texture generator is `internal/cmd/texgen`, and `go run ./internal/cmd/texgen` writes the files.
`TestTextureFiles` compares the pixels of each file with the generator output, so a change of the PNG compressor in a new Go release does not fail it.
`go test ./internal/cmd/texgen -run TestTextureFiles -update-textures` writes the files again.
The noise uses integer arithmetic only, so each machine gives the same pixels.
Each texture is gray, and the page multiplies it by the color of the surface.

| File | Size | Bytes | Surfaces |
| --- | --- | --- | --- |
| `web/textures/concrete.png` | 256 by 256 | 40,505 | The beams, the hub decks, the pillars, and the station decks. One repeat is 4 m, except on the pillars. |
| `web/textures/panel.png` | 256 by 256 | 6,936 | The pod shell, tinted by the class color. One repeat is 1 m along the pod and the full height. A dark livery stripe with two thin lines is below the windows, and a panel seam is at the middle of each repeat. |
| `web/textures/fabric.png` | 128 by 128 | 11,476 | The seats. One repeat is 0.25 m. |
| `web/textures/dash.png` | 128 by 128 | 9,730 | The cabin inside and the dashboard. One repeat is 0.5 m. |

- The four textures have 68,647 bytes, and `web/pod.glb` grew from 85,016 to 117,352 bytes.
- Each part of the model has texture coordinates.
  The glass and the gear have zero coordinates, because Babylon merges only meshes with the same vertex data.
- The concrete goes once around each pillar, so it has no seam, with the same scale up the pillar.
  On the pillars, one repeat is about 2 m.
- The beams and the hub decks get texture coordinates in meters from the start of each lane or the first point of the deck, so the values stay small far from the origin.
- A texture goes to its materials only after it loads.
  When it does not load, the page logs a warning, and the surfaces keep their flat colors.

## Visual polish

After M2, the maintainer noted that the look needs more work.
On 2026-11-20, the maintainer asked for a ride page that is prettier, more futuristic, and more realistic, with a good frame rate on LondonFull.
The polish is a set of small parts, and each part has a URL switch, so the maintainer can compare the look in Chrome.

- Each switch is on, and `name=0` turns it off, for example `/ride.html?pod=01&rail=0`.
  `polish=0` turns all of them off and gives the look from before the polish.
- `lookOptions` in `web/ride.js` reads the switches, and a Node test pins them.
- No part adds an asset file, a script, a light, or a pass of the image pipeline.
  The textures of the polish are typed arrays that `web/ride.js` makes at load, and Node tests check them.
- No part adds per-frame CPU work that grows with the lane count.
  The GPU draws the rail vertices in each frame, as it draws the beams.
- The counts are for LondonFull, from the topology of the preset in Node.
  Before the polish, the page draws 8,220 lanes, 9,045 pieces, and 1,182,064 beam vertices.

| Switch | Part | Cost |
| --- | --- | --- |
| `tint` | The map tiles get a cool gray diffuse color, so the guideway stands out over the bright map. | One material color. No draw call, vertex, or per-frame work. |
| `rail` | A strip in the HUD accent color, 12 cm wide, runs along the middle of each beam top, 5 cm above it. | One draw call, one unlit material, 295,516 vertices, 260,986 triangles, and about 6.7 MB of GPU buffers. No shadow work. About 140 ms more to build in Node. |
| `taper` | The beam bottom is 0.9 m wide under the 1.4 m top, so the sides slope in. | The same faces and vertices as the box beam. |
| `sky` | The sky has a lighter haze at the horizon, a deeper blue at the top, and a warm glow around the sun. The fog is exponential squared in the haze color, and 4% of a color stays at 3 km. | The sky dome has 48 segments in place of 24: about 3,700 more vertices in the same draw call. The fog costs the same. |
| `gloss` | The pod shells and windows reflect a cube texture of the sky, with a Fresnel term. | A cube texture of 32 texels on a side, about 33 KB with mipmaps, made once for each topology. One cube sample for each pixel of a shell or a window. No draw call. |
| `lights` | The pod model gets a white band across the front nose, a red band across the rear nose, and a thin accent line along each side, in the livery stripe. | An emissive texture of 64 by 512 texels, about 175 KB with mipmaps. One texture sample for each shell pixel. No geometry or draw call. |
| `signs` | The station signs have the dark panel of the cabin screens, an accent bar, and a stop mark. A long name loses the part in brackets, then words at its end. | A sign draws only when it moves to another station, as before. |
| `fx` | A weak vignette darkens the corners of a 16:9 view by about 21% and the cabin screens by about 8%, and the color curves raise the saturation a little. | Uniforms of the existing image processing pass. No new pass. |
| `fov` | The seat view widens from 1.1 rad when the pod stands to 1.18 rad at 20 m/s, with a time constant of 0.5 s. | A few operations in each frame. |

- `time=dusk` gives a warm sun 15 degrees above the horizon in the west-southwest, a cool fill light, a warm haze, and a strong glow.
  The day stays the default, because the bridge has no clock of the day.
  Dusk needs the polished sky, so `sky=0` gives the day.
  With the low sun, the 320 m shadow map covers about 1.2 km of ground along the sun direction.
  Thus the shadows are softer in that direction.
- The rail is real geometry with a small depth offset.
  A light line in the beam texture would fight in the depth buffer at each fork, where the tops of two beams overlap.
  An edge line of one branch would also cross the running surface of the other, and a center rail splits cleanly at a fork.
- The cube faces of `gloss` follow the WebGL cube lookup, which Babylon also uses.
  A Node test checks the face directions against that lookup.
  If the windows show the ground above the horizon, the faces are upside down in the browser.
- The lights of `lights` use the shell texture coordinates of `internal/cmd/podmodel`.
  There, u is z in meters, and the middle of the livery stripe is at v = 0.5.
  A Node test pins this mapping against `web/pod.glb`.
  The procedural pods have no such coordinates, so they get no lights.
- The reflections and the lights add to the lit color after the lighting, and the sum stops at white.
  Thus the light shells reflect little, and shade does not dim the lights.
- The seat view only widens, because a narrower view cuts off the cabin screens.
  There is no head bob.

These parts were judged and left out:

- Bloom or a glow layer: in an earlier check, bloom spread the bright map tiles over the cabin pillars.
  The sky, the cabin, the screens, and the signs use emissive color in place of light, so a glow would light them too.
- A light line on the beam edges: it fights at forks, as the list above says.
  The tapered sides face down, so the seat view does not see a line on them.
- Platform edge lights and canopies: the platform slab is a box around the berths, aligned with the map axes and not with the berths.
  Lights on its edges show the box, and a canopy can block the platform camera.
- A time of day from the session clock: the bridge sends no clock of the day.

## Later items

- Grade separation of the crossings that are not in a hub: 211 between mainline lanes, and the crossings of station lanes near a diverge.
  The [London junction design](london-junctions.md) can remove most of them if the presets adopt ramps.
- Walking between a start location and a station.
- Stored elevation stays out of scope, as the [brief](../../PROJECT_BRIEF.md#4-scope-limits) says.
