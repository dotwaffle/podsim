# First-person ride design

Status: the maintainer approved this design on 2026-11-14.
Nothing in this document is built.

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
| `ride.hud(podID)` | About 4 times a second | JSON: activity, wait reason, `blockedBy`, lane speed limit, riders, stops, remaining distance, ETA, and session data. |
| `ride.submit(command)` | On user action | A promise of the acknowledgment. It uses the retry-safe command path of `remote.Client`. |

`frame` uses `remote.Motion.Sample`, so the 3D view has the same 250 ms delay as the 2D view.

### Heading

`sim.Pod` has no heading.
`remote.Motion.Sample` interpolates the position and the speed along the route, but it keeps `laneID` and `laneDistance` of the earlier frame.
Thus `internal/ride` keeps the sampled position of each pod from the previous animation frame.
The heading is the direction from that position to the new position.
If the pod moved less than 1 cm, the heading is the tangent of the lane at `laneDistance`, or the earlier heading if the pod has no lane.
A lane is a line or a quadratic Bezier curve with one control point, so the tangent has an exact formula.
The seat camera smooths yaw with a 150 ms time constant, because two lanes can meet at an angle.
The chase camera uses a longer time constant.

### Coordinates

The 2D view draws world y down the screen, so world y points south.
Babylon uses a left-handed system with y up.
The page maps world (x, y) to Babylon (x, h, -y), so +x is east and +z is north.
Babylon stores vertex positions as float32.
At 20 km from the origin, float32 resolution is about 2 mm, and the seat camera is less than 1 m from the cabin, so the cabin can jitter.
LondonFull extends about 34 km west of its origin.
The page creates the Babylon engine with `useLargeWorldRendering`, which draws the scene relative to the camera.
The M1 acceptance includes a ride at the far edge of LondonFull with no visible cabin jitter.
If the cabin jitters, the bridge subtracts an origin near the camera in float64 before it fills the array, and the page moves the scene by the same origin.
A test pins the axis mapping with a station pair of known compass direction.

## Scene

- Guideway: one box-section beam for each lane, extruded along the line or curve, at a fixed height of 5 m on simple pillars.
  The page merges the beams into a few meshes, so LondonFull does not have 20,000 draw calls.
- Pods: one rounded box for each class, sized from the class profiles, with glass and a class color.
  Thin instances draw all pods of a class in one draw call.
- Stations: a platform slab under the berths of each station.
  Name signs come in M3, near the camera only, because a texture for each of 600 stations uses too much memory.
- Ground: map tiles when `geo` is present, with the same tile source and attribution as `web/tiles.js`.
  Otherwise, a plain textured plane.
- Light and sky: a sun as a directional light, a hemispheric fill light, a sky color, and distance fog.
  A gradient sky comes in M3.
  Fog hides the far guideway, so the draw distance can stay at about 3 km.
- Shadows: only the pods and beams near the camera cast shadows.
  Shadows come in M3, because one shadow map over the whole network has too little resolution.
- Crossings: the beams and pods of two crossing lanes intersect.
  Visual grade separation is a later item.

## Cameras and input

- Seat camera: at the front of the ridden pod, facing forward.
  While the user holds the mouse button, mouse movement turns the view.
  On release, the view returns to forward.
- Chase camera: behind and above the pod.
  The `C` key switches between the seat camera and the chase camera.
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
- Trip panel: the origin and destination names, the party size and the riders, the next stop for a shared ride, the remaining distance, and an ETA.
  The client gets the lanes of the route window as `Vehicle.Route`, and the ETA uses their lengths and speed limits.
  If the route window does not reach the destination, the panel shows "more than" the window distance.
- Mini-map: a 2D canvas with the network, the ridden pod, and the pods near it.
- Session strip: the session speed, the "Ride at 1x" button, the pause state, the connection state, and the frame rate.

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
   Acceptance: LondonFull at 60 frames per second, no camera yaw jump at lane joins, and no cabin jitter at the far edge of the network.
2. M2: the HUD speed and state, the trip panel, the session strip, mouse look, and the chase camera.
3. M3: map tiles, the mini-map, auto mode, station signs, shadows, and a gradient sky.
4. M4: the passenger flow.

## Later items

- Visual grade separation at crossings.
- A "Ride" button in the 2D pod inspector.
- A glTF pod model.
- Walking between a start location and a station.
- Stored elevation stays out of scope, as the [brief](../../PROJECT_BRIEF.md#4-scope-limits) says.
