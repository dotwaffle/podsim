# First-person ride design

Status: the maintainer approved this design on 2026-11-14.
Milestone M1 is built, and the maintainer accepted it in Chrome on 2026-11-14: the frame rate on LondonFull was good, and the only console error was the missing favicon.
Milestone M2 is built, and the maintainer accepted it in Chrome on 2026-11-14, with a note that the look needs more work.
Milestone M3a is built, and the maintainer accepted it in Chrome on 2026-11-14, after the fixes for the map flicker, the black sky, and the map sharpness.
Milestone M3b is built, and the maintainer accepted it in Chrome on 2026-11-14, after a north-up mini-map, damped turns, a route progress bar, a nearest stop, sharper shadows, and station labels.
Milestone M4 is built, and the maintainer accepted it in Chrome on 2026-11-14.

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
| Models | Procedural meshes. No model files. |
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
| `ride.topology()` | When `generation` changes | JSON: lanes with end points, control point, speed limit, and the distances of the two nodes from the mainline, the levels, stations with berth positions, the largest party size of a new order, `geo`, and `map`. The module starts the client for the page origin when it loads. |
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
  The page merges the beams into a few meshes, so LondonFull does not have 20,000 draw calls.
  Vertex colors make the running surface on top darker than the concrete sides.
- Pods: the page loads `web/pod.glb`, a glTF model that `internal/cmd/podmodel` writes.
  The model is 4 m long, 1.5 m wide, and 2.1 m tall, with the parts `shell`, `glass`, `gear`, and `cabin`, and an `eye` node at the seat.
  The body is a loft of rounded sections, with side windows above a belt line, a glass dome at each end, and a pillar ring at the middle and near each end.
  The cabin is the inside of the shell, 4 cm in, and a dashboard.
  For each class, the page stretches the parts to the class length and gives the shell the class color.
  The shell, the glass, and the gear merge into one mesh with one submesh for each material, and thin instances draw all pods of a class in a few calls.
  The page reads the GLB file itself, because it needs only a few accessors of one buffer, so it does not load the Babylon glTF loader.
  glTF is right-handed and Babylon is left-handed, so the page changes the sign of x.
  With the same index order, the triangles then wind as the Babylon meshes do, and a Node test checks each triangle of the file.
  If the file does not load, the page draws procedural pods and a cabin frame that moves with the camera.
- Stations: a platform slab under the berths of each station.
  A pool of 12 name signs goes to the stations within 900 m of the camera, because a texture for each of 600 stations uses too much memory.
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
- Crossings: the simulation does not control a crossing of two lanes with no shared node, so a pod passes through a pod on the other lane.
  On LondonFull, 6,720 such crossings are less than 3.2 m apart in height after the station levels: 2,905 between mainline lanes, 3,097 between a mainline lane and a station lane, and 718 between station lanes.
  Most mainline crossings are in hubs: a ring of nodes where each entry joins each exit by a straight chord, so the chords cross in the middle.
  A crossing has a median of 132 other crossings within 150 m, and only 11 have none, so a flyover for each crossing does not fit.
- Hubs: the page groups mainline lanes that cross with no shared node, and a group with at least 10 crossings and 4 lanes is a hub.
  A lane longer than 400 m passes a hub, so it is not part of one.
  A hub draws as one deck over the convex hull of the lane end points, 5 cm below the beam top, with a pillar at each corner, in place of the chord beams.
  The pods stay at the mainline height.
  On LondonFull, 37 hubs hold 2,694 of the 2,905 mainline crossings, a typical hub is an octagon 125 m wide, and the search takes about 40 ms in Node.
  The maintainer chose the deck on 2026-11-14, and the real fix of a hub belongs to the station layout study, because it changes the model.

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

- `internal/cmd/buildweb` builds `ride.wasm.gz` from `./cmd/ride` and copies `ride.html`, `ride.js`, and `pod.glb`.
- `go run ./internal/cmd/podmodel` writes `web/pod.glb`.
  `TestModelFile` fails when the file is not the output of the generator, and `go test ./internal/cmd/podmodel -run TestModelFile -update-model` writes it again.
- The wasm dependency check in `mise.toml` adds `./cmd/ride`, so that `ride.wasm` does not link cloud storage or the preset data.
- `govulncheck` for `js/wasm` adds `./cmd/ride`.
- Go tests in `internal/ride`: the heading on lines and curves, the frame packing, the HUD data, the ETA, the auto mode choice, and each step and stop case of the passenger flow.
- Go tests in `internal/cmd/podmodel`: the GLB structure, the node names, the bounds of the model, the winding of each triangle, and what the rider sees from the eye in each direction.
- Node tests (`mise run test:web`): the beam path points, the coordinate mapping, the camera smoothing, the HUD formatting, and the read of the pod model.
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
- On LondonFull, the berth nodes are 354 m to 546 m from the mainline, so each berth is at the station level.
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
- On LondonFull, 301 of the 312 stations are at the station level, and 136 pairs of twin lanes draw once.
  A pod on a ramp is within 1 cm of the beam that the page draws under it.
  The beams and the pillars take about 240 ms to build in Node.

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

## Later items

- Grade separation of the crossings that are not in a hub: about 210 between mainline lanes, and the crossings of station lanes near a diverge.
  The station layout study can remove most of them.
- Walking between a start location and a station.
- Stored elevation stays out of scope, as the [brief](../../PROJECT_BRIEF.md#4-scope-limits) says.
