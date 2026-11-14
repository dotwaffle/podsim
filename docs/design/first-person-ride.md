# First-person ride design

Status: the maintainer approved this design on 2026-11-14.
Milestone M1 is built, and the maintainer accepted it in Chrome on 2026-11-14: the frame rate on LondonFull was good, and the only console error was the missing favicon.
Milestone M2 is built, and the maintainer accepted it in Chrome on 2026-11-14, with a note that the look needs more work.
Milestone M3a is built and waits for the maintainer's check in Chrome.
M3b and M4 are not built.

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
| Placement | A separate page, `/ride.html`, with its own WASM module. The 2D viewer does not change. |
| Speed | The page shows the session speed. A "Ride at 1x" button sends the existing `speed` command, which changes the speed for all viewers. |
| Height | All guideway is at one height. At a crossing, pods and beams of the two lanes intersect. |
| Ground | Map tiles on a flat ground plane when the project has a geographic reference, and plain ground otherwise. |
| Models | Procedural meshes. No model files. |
| Pod choice | A pod list, a `?pod=ID` URL, and an auto mode that moves to another pod after a drop-off. |
| Camera | The front seat, mouse look that returns to forward, and a chase camera. |
| HUD | An HTML overlay with speed and state, a trip panel, a mini-map, and a session strip. |
| Passenger | A lite flow: choose stations, send `trip`, wait, ride, and arrive. No walking. |
| Scale | LondonFull is the target from the first milestone. |
| 2D viewer link | Not in the prototype. |

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
| `ride.start(serverURL)` | Once | Nothing. It starts the client. |
| `ride.topology()` | When `generation` changes | JSON: lanes with end points, control point, and speed limit, stations with berth positions, `geo`, and `map`. |
| `ride.frame(out)` | Each animation frame | It fills a `Float64Array` with 5 numbers for each pod: x, y, heading, speed, and class index. It returns the pod count, the generation, a pod list version, the connection state, and the connection error. |
| `ride.ids()` | When the pod list version changes | The pod IDs in the order of `frame`. |
| `ride.pods()` | When the pod list version changes, and when the user opens the pod list | The ID, class, activity, and first trip of each pod, in the order of the latest state. |
| `ride.hud(podID)` | 4 times a second | JSON: class, activity, speed, lane speed limit, wait reason, `blockedBy`, riders aboard, next stop, remaining route, session speed and pause, connection, and the outcome of the last command. |
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
The time constant of the yaw is 350 ms for the seat camera and 600 ms for the chase camera.
The other pod meshes use the same smoothing, with 200 ms as the time constant.
In the M1 check, the seat camera used 150 ms, and the maintainer found the turns smooth but a bit quick.

### Coordinates

The 2D view draws world y down the screen, so world y points south.
Babylon uses a left-handed system with y up.
The page maps world (x, y) to Babylon (x, h, -y), so +x is east and +z is north.
Babylon stores vertex positions as float32.
At 20 km from the origin, float32 resolution is about 2 mm, and the seat camera is less than 1 m from the cabin, so the cabin can jitter.
LondonFull extends about 34 km west of its origin.
The page creates the Babylon engine with `useLargeWorldRendering`, which draws the scene relative to the camera.
The cabin moves with the camera, so float32 resolution does not move it.
The beams, the ground, and the pod instances keep absolute positions, and the large-world option can miss some of them.
The M1 acceptance includes a ride near x = -33 km with no shimmer at the edge of the beam ahead of the pod.
If the beam edge shimmers, the page builds each mesh around a local origin, and the bridge subtracts an origin near the camera in float64 before it fills the array.
A test pins the axis mapping with a station pair of known compass direction.

## Scene

- Guideway: one box-section beam for each lane, extruded along the line or curve, at a fixed height of 5 m on tapered pillars about every 60 m.
  The page merges the beams into a few meshes, so LondonFull does not have 20,000 draw calls.
  Vertex colors make the running surface on top darker than the concrete sides.
- Pods: for each class, a rounded shell in the class color, a band of dark glass, and a running gear, sized from the class profiles.
  The parts merge into one mesh with one submesh for each material, and thin instances draw all pods of a class in a few calls.
- Stations: a platform slab under the berths of each station.
  A pool of 12 name signs goes to the stations within 900 m of the camera, because a texture for each of 600 stations uses too much memory.
- Ground: map tiles when `geo` is present, from the tile source of `web/tiles.js`, with the OpenStreetMap credit on the page.
  The ride page does not need the `map` setting that the 2D view needs, but it refuses a `map` setting that is not valid.
  A ring of zoom 17 tiles covers 700 m around the ridden pod, over a ring of zoom 14 tiles that covers the fog distance.
  The page asks for at most 8 new tiles each second.
  Without `geo`, the ground is a plain textured plane.
- Light and sky: a warm sun as a directional light, a hemispheric fill light, a sky dome with a vertical gradient, and distance fog in the horizon color.
  Fog hides the far guideway, so the draw distance can stay at about 3 km.
- Shadows: one 2,048-pixel shadow map covers a 260 m square that follows the camera.
  The pods, the beams, and the pillars cast shadows, and the ground, the tiles, the beams, and the platforms receive them.
- Image: Babylon's default rendering pipeline with 4x multisampling, FXAA, ACES tone mapping, and a weak bloom.
- Crossings: the beams and pods of two crossing lanes intersect.
  Visual grade separation is a later item.

## Cameras and input

- Seat camera: at the front of the ridden pod, facing forward.
  While the user holds the mouse button, mouse movement turns the view.
  On release, the view returns to forward with a 300 ms time constant.
- Chase camera: 14 m behind the pod and 5 m above its roof.
  The `C` key switches between the seat camera and the chase camera.
  The chase view draws the ridden pod, and the seat view draws the cabin frame.
- Platform camera: at a station, used by the passenger flow.

## Pod choice

The page opens on the pod in `?pod=ID`, or else shows the pod list.
The list shows the ID, the class, the activity, and the origin and destination of the first rider.

In auto mode, after the last rider of the ridden pod leaves, the page moves to another pod with a fade.
It takes the nearest pod that carries riders.
If there is no such pod, it takes the nearest pod that goes to a pickup.
If there is neither, it stays on the current pod.
Ties go to the lowest pod ID, so the choice is repeatable for a given state.

If the ridden pod leaves the fleet, for example after a project apply, the page shows the pod list.

## HUD

- Speed and state: the speed, the speed limit of the lane, the activity, and the wait reason with `blockedBy` when the pod is stopped.
- Trip panel: the party size, origin, and destination of each party aboard, the next stop, the remaining distance, and an ETA.
  The next stop is the first stop of the pod, or else the destination of the first party aboard.
  The client gets the route window as `Vehicle.Presentation`, and the ETA uses the lengths and speed limits of its lanes from the pod lane on, with no waits.
  If the route continues past the window, the panel shows "over" the window distance and time.
- Mini-map: a 2D canvas with the network, the ridden pod, and the pods near it.
- Session strip: the session speed, the pause state, the connection state, the "Ride at 1x" button, and the outcome of the last command.
  The diagnostics line shows the frame rate.

## Passenger flow

The flow uses the existing `trip` command and the `pending` and `riders` data of the stream.
It needs no server change.

1. Menu: the user chooses an origin station, a destination station, a party size, and the consent.
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

- `internal/cmd/buildweb` builds `ride.wasm.gz` from `./cmd/ride` and copies `ride.html` and `ride.js`.
- The wasm dependency check in `mise.toml` adds `./cmd/ride`, so that `ride.wasm` does not link cloud storage or the preset data.
- `govulncheck` for `js/wasm` adds `./cmd/ride`.
- Go tests in `internal/ride`: the heading on lines and curves, the frame packing, the HUD data, the ETA, the auto mode choice, and each step and stop case of the passenger flow.
- Node tests (`mise run test:web`): the beam path points, the coordinate mapping, the camera smoothing, and the HUD formatting.
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

## Later items

- Visual grade separation at crossings.
- A "Ride" button in the 2D pod inspector.
- A glTF pod model.
- Walking between a start location and a station.
- Stored elevation stays out of scope, as the [brief](../../PROJECT_BRIEF.md#4-scope-limits) says.
