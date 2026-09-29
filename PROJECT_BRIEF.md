# Podsim: Project Brief and Research

**Status:** The project accepted the initial usable 2D version on September 21, 2026.

The browser supports local map backgrounds, scale calibration, network editing, and project persistence.
It also supports manual and automatic demand, pod dispatch, local traffic control, inspection, pod following, and fleet-use statistics.
The server owns one shared simulation session for all connected browsers.
Any browser can keep an exact save point of the running simulation in server memory and rewind the session to the latest save point.
With the `-state` option, the server also saves the shared session to disk and restores it after a restart when it can.
Optional redistribution runs guarded positioning, which moves idle empty pods only at a low demand rate.
It remains off by default.

The rail-hub, London capacity envelope, same-destination sharing, drop-offs sharing, and first congestion-aware routing experiments are complete.
The screen of the queue routing policy and the platoon screening are also complete.
The first station-maneuver slice is complete: station lanes have explicit roles, pod snapshots expose the current phase, and the inspector names the maneuver.
The scenario command sets the berths and berth pitch of the generated presets and the initial pods of both London presets.
A layout check rejects lanes that come too near.
Other station geometry options and the other experiments in Section 6 remain later work.
See [README.md](README.md) for controls, validation commands, and current model limits.

**Current work status, September 29, 2026:**

LondonCentral (`london-central`) retains the central qualification network and its 2019 demand.
[LondonFull](docs/london-full.md) adds 269 Tube sites with 2024 endpoint demand.
Its 302-arm study tested 60 minutes of arrivals with up to 60 minutes to recover.
No fleet candidate established an all-ten recovery improvement at its next tested rate.
These finite tests do not establish a sustainable capacity envelope.
The original `london` selector is now `london-central`.

The editor imports georeferenced PNG or JPEG backgrounds with stored bounds, projection, attribution, and an atomic undo step.
The optional Overpass adapter imports an attributed OSM schematic background from a user-selected HTTPS endpoint.
It has no default endpoint and does not create guideways or editable OSM vectors.
See [georeferenced backgrounds](README.md#georeferenced-background).

The live viewer uses shared gzip WebSocket deltas, and explicit public-origin configuration supports a TLS-terminating proxy.
Prepared network geometry speeds repeated isolated restores without changing the saved format or removing restore assertions.
The renderer batches station summaries, and presentation snapshots reuse call-local lane marks.
The restore and rendering optimizations preserve simulation behavior and existing protocol semantics.

Sharing remains off by default.
The opt-in `reassign-existing` join policy and guarded-positioning follow-up still fail adoption rules.
These results do not reverse the earlier adoption of the drop-offs mode when sharing is enabled.
Larger pods and congestion-aware routing remain parked.
Local Chromium software-rendering checks cover the viewer, editor, and high-latency streams.
Desktop Chrome is the required browser for current development.
Firefox and Safari validation are not required.
The simulator currently runs on the local machine.
Fly deployment and hosted-origin validation are deferred until release preparation.
Software-rendering measurements do not establish physical-GPU performance.

This document records the project direction, initial feature scope, architecture, effort estimates, and research.
The initial scope and policies were the starting point for implementation planning.
Future experiments are separate possibilities, not initial delivery commitments.

## 1. Product vision

Build a browser-based playground for designing and observing personal rapid transit networks.

The user imports a map, draws guideways, places stations, and watches pods serve passenger journeys.
The main interest is network behavior: routing, demand, dispatch, congestion, and station capacity.

Use **Go and Ebitengine, compiled to WebAssembly**.
Target desktop Chrome with mouse and keyboard.
The browser renders the application and sends commands to a Go server.
The server owns the simulation clock and sends shared gzip JSON state updates over WebSocket.

This is a hobby simulator.
Engineering certification, construction planning, and accurate predictions for real transport systems are outside its purpose.

### First-version success

The user can build a small network over a familiar map, run it, understand where delays occur, and change the design.

The experience should remain useful and enjoyable without a future 3D version.

### First playable milestone

Start with simulation on a supplied small network before building the map editor.
Include a branch, a merge, and stations with berths separate from through traffic.
Provide demand controls and enough inspection to explain pod movement and waiting.

Use local queues: pods depart when local conditions permit, then slow or wait for occupied track, merges, or station access.
Do not require a reserved timetable for the entire journey before departure.
Local admission and spacing rules must still prevent conflicting movements.

Map import and network editing follow this first simulation milestone.
They remain part of the initial usable version described below.

## 2. Initial feature scope

### R1: Map background

- Import a local PNG or JPEG.
- Set scale by marking two points and entering their real-world distance.
- Pan, zoom, and adjust background opacity.
- Support networks without a background image.
- Keep imported images local to the browser.

Local PNG/JPEG import remains part of the initial usable version after the simulation-first milestone.
Direct geographic area import is a later extension described in Section 6.

### R2: Network editor

- Create, name, move, and delete stations.
- Draw and adjust curved guideways.
- Create explicit junctions.
  Crossing lines do not connect automatically.
- Support one-way guideways and paired lanes for two-way travel.
- Display direction arrows.
- Support undo and redo.
- Explain invalid connections and unreachable destinations before a run.

**Initial default:** Each direction uses a separate lane.
Opposing traffic on shared single track is a later feature.

### R3: Simulation

- Configure a finite fleet, station berth capacity, and passenger demand.
- Treat each demand event as one traveling party requiring one pod.
- Support automatic demand and manually requested station-to-station journeys.
- Route occupied pods to their destination without intermediate passenger stops.
- Dispatch empty pods to waiting parties.
- Model boarding time, unloading time, acceleration, braking, and speed limits.
- Prevent overlapping pods and conflicting movement through junctions.
- Keep station berths separate from through traffic.
- Expose congestion and unmet demand instead of silently adding vehicles.

Initial routing uses shortest expected travel time without congestion prediction.
Initial dispatch chooses the nearest available, reachable empty pod.

These are understandable starting policies, not attempts to optimize the entire network.
One party per pod and no intermediate stops are initial service policies, not permanent limits of the simulation model.

### R4: Controls and inspection

- Start, pause, resume, reset, and change simulation speed.
- Save the running state and rewind to the latest save point.
- Select a pod to inspect its destination, route, and current activity.
- Follow a pod with the map camera.
- Inspect station queues and berth occupancy.
- Show completed journeys, waiting times, and fleet use.
- Explain why a selected pod is waiting.

Applying network edits requires a paused run.
Applying edits resets simulation state while preserving the design.
A rewind to a save point from before an applied edit or a demand change restores the earlier design and settings.

### Station modeling direction

Physical station layouts are an intended capability, not only a possible visual upgrade.
Schematic station treatment is acceptable for the first milestone.
The initial model must allow later entry-lane, berth-access, and exit movements without replacing the entire station abstraction.

Give berths and station entry/exit connections stable identities.
Keep station capacity, passenger queues, and berth occupancy separate from the station's map symbol.
Treat omitted internal maneuvers as explicit simulation simplifications.
Do not represent every station permanently as one point with a single boarding timer.

Detailed station geometry and movement rules can follow the first milestone.
Keep internal path lengths and movement conflicts possible in the model even when the first milestone abstracts them.

### R5: Persistence

- Export and import a portable project containing the network, settings, and optional background image.
- Version the project format.
- Report malformed or unsupported files without replacing the current project.
- Include a small example network.
- Save the scenario configuration in project files, rather than the exact running state.
- With the `-project` server option, load the server project from an existing file.
  For each project apply, demand change, or rewind that restores a project, save the changed project to that file.
- Keep at most eight exact save points of the running simulation in server memory only.
  A server restart clears them.
  At the limit, a new save point removes the oldest one.
- With the `-state` server option, save the live shared session to disk and restore it on a best-effort basis after a server restart.
  The `physical` restore tier keeps the pod positions and moves a traveling pod that cannot keep its position to a free berth.
  When the `physical` tier fails, the `logical` tier starts the pods again at their initial berths.
  Parties that were unloading count as completed, and the other parties in pods go back to the queue.
  The saved state does not hold save points or command receipts.

## 3. Architecture and future 3D

### Keep the simulation independent

Use ordinary Go packages for:

- Network geometry and connectivity.
- Routing and dispatch.
- Vehicle movement and traffic control.
- Passenger demand and statistics.
- Project data and validation.

The simulation core must not import Ebitengine or browser APIs.
Store geometry in world units, not screen coordinates.

Advance simulation through fixed time steps.
Keep rendering independent of simulation speed.
A saved scenario and random seed should produce repeatable results.

Ebitengine draws the simulation view and handles its controls.
A separate HTML and JavaScript page provides the scenario editor, background images, and project import and export.

Define a clear boundary between user commands, simulation updates, and state exposed for display.
Avoid a general plugin system in the first version.

### Typed client protocol

Use shared gzip JSON full baselines and deltas over WebSocket for the simulation view.
Keep commands, topology, and editor data on HTTP.
Require native browser gzip decompression, with no HTTP polling fallback.
Preserve the authoritative server and retry-safe command identity.

The September 22 evaluation normalized recurring JSON state, then compared it with ConnectRPC and binary Protocol Buffers.
Protobuf reduced the London gzip frame by another 10.3%.
At 20 Hz, its encoding and compression would save about 0.7% of one CPU core per client.

No application client used the experimental service.
The project removed it to avoid a second protocol implementation and an alpha runtime dependency.
Review this choice if remote traffic, multiple viewers, or external clients make a typed RPC protocol useful.
See [the protocol evaluation](docs/protocol.md).

### Separate passengers, vehicles, and service policies

Keep passenger requests and party size separate from vehicles and assignments.
A request describes a desired journey.
A vehicle describes the pod available to serve it.
The service policy determines how requests use vehicles.

Keep demand generation, dispatch, route choice, and physical movement separate:

- Demand generation determines when and where parties request journeys.
- Dispatch assigns vehicles to requests and decides where empty pods should wait.
- Route choice selects a path through the network.
- Physical movement enforces speed, spacing, junction access, and station capacity.

These boundaries allow later experiments to change one policy while retaining the same network and movement model.
Use explicit types and functions.
Add interfaces when there is a concrete need for interchangeable policies.
Do not implement mixed fleets, transfers, or platooning to establish these boundaries.

### What a later Three.js version would involve

A future browser renderer could consume snapshots from the same Go simulation server.

**Reusable:** network data, routing, demand, dispatch, movement rules, statistics, and most tests.

**New work:** JavaScript or TypeScript presentation code, 3D track geometry, pod models, cameras, selection, and scenery.

Ebitengine's drawing code would not automatically become Three.js code.
A first-person ride would therefore be a substantial presentation extension, but it would reuse the simulation.

Elevation and terrain are outside the initial model.
Adding them later may require changes to geometry and saved projects.
Do not build speculative 3D infrastructure now.

## 4. Delivery stages and effort

Estimates cover one developer, including debugging and tests.
Browser development experience and interface polish will affect the range.
Refine these ranges after the first browser prototype.
Detailed station layouts beyond the initial schematic treatment need a separate scope and estimate.

| Stage | Deliverable | Estimated effort |
|---|---|---:|
| Playable experiment | Supplied network, demand controls, routing, local queues, moving pods, inspection | 20-40 hours |
| Usable 2D version | Curves, undo, persistence, finite fleet, demand, dispatch, traffic control, statistics | 80-160 hours total |
| Optional shared track | Opposing traffic, reservations, fairness, deadlock handling | Additional 40-100 hours |
| Optional basic 3D client | Three.js rendering, ride camera, pods, stations, live map | Additional 60-140 hours |
| Optional scenery | Terrain, buildings, trees, atmosphere | Separate project-sized effort |

The 3D estimate includes introducing a second renderer and browser integration.
Significant JavaScript learning may extend it.

At eight hours per week, the usable 2D version represents roughly 10-20 weeks.
The first experiment should provide something playable much earlier.

Keep multiplayer features beyond one shared session, live map services, realistic scenery, legacy simulator file import, and advanced fleet optimization outside the first release.
The experiments in Section 6 also remain outside the initial scope.
This brief does not estimate their effort, except where an optional stage appears in the table above.

## 5. Acceptance and validation

### Simulation tests

- Pods choose valid routes through one-way and paired-lane networks.
- Unreachable journeys produce a clear result.
- Competing pods pass through a merge without overlap.
- A full station does not accept more pods than its capacity permits.
- A finite fleet leaves excess demand waiting.
- Empty-pod dispatch eventually serves waiting parties in a feasible, uncongested test network.
- Identical scenarios and seeds produce repeatable outcomes.
- A rewind to a save point replays exactly.
  The same actions after each rewind produce the same outcomes.
- Changing playback speed preserves simulation results at equal simulated times.

### Browser checks

- Import an image, calibrate scale, draw a network, and complete a journey.
- Export and reload the project with its background intact.
- Exercise undo, invalid input, pause, reset, and pod following.
- Select Save point, run the simulation, then select Rewind.
  The session must pause at the saved time.
- Start the server with `-state`, and run the simulation with pods and orders for at least 60 s.
  Stop the server with Ctrl-C, then start it again with the same command.
  The `Restored session` log record must give `tier=physical` and `epochKept=true`.
  The pods must continue from the same positions, and the open page must keep its session.
- Run a proposed baseline of 20 stations and 100 pods.
- Record hardware, frame rate, and simulation update cost before establishing performance guarantees.

**Acceptance status:** Complete for the initial usable version on September 21, 2026.
A combined browser run imported a PNG, calibrated 200 meters, and exercised drawing and undo.
It edited the network, exported and reloaded the project with its background, and then applied the network.
It submitted a journey through the simulation UI and observed its completion.
It also toggled pod following and reported no browser errors.

Separate browser checks cover invalid input, reset, stale edit conflicts, and the 20-station, 100-pod scenario.
The hardware and performance record is in [docs/qualification.md](docs/qualification.md).
Two later commits made route search and dispatch faster, and the simulation output did not change.
Commit `40fc98f` finds the routes to all berths of a station with one search, and commit `80c47dc` skips dispatch scans at stations with no idle pod.
From commit `40fc98f` to commit `80c47dc`, a heavy London compare arm fell from 10.1 to 7.1 seconds of wall time.
See [route search and dispatch scans](docs/qualification.md#route-search-and-dispatch-scans).

The project added the save point, rewind, and `-state` restart checks above on September 23, 2026, after this acceptance.

## 6. Future extensions and experiments

The following ideas extend the map workflow and network simulation.
None of them needs 3D.
These are proposed experiments and design considerations.
Status notes record the parts that Podsim now implements.

### Geographic map import

The editor supports local georeferenced images and an optional Overpass adapter for OSM schematic backgrounds.
The adapter takes a user-selected HTTPS endpoint and geographic bounds.
The browser requests bounded raw geometry and draws an attributed background image.
It does not download rendered map tiles or convert streets into pod guideways.

The current workflow is:

1. Enter the endpoint and geographic bounds in the editor.
2. Import the schematic background and inspect any omitted-geometry diagnostics.
3. Draw the pod network over the background.

Projects retain the bounds, coordinate frame, source, and attribution through save, export, and undo.
Ordinary image files still support manual scale calibration.
Incomplete or ambiguous geometry is omitted with diagnostics.
Malformed or oversized responses reject the import without changing the draft.
There is no default provider, server proxy, automatic retry, locator map, or geocoder.

Local synthetic, browser, and mutation tests passed.
The first small public-provider probe returned HTTP 504, so validation stopped before the larger queries.
This leaves public-service availability unqualified, not a demonstrated importer defect.
A new bounded probe requires a separate request budget.
See [georeferenced backgrounds](README.md#georeferenced-background) for the current controls and limits.

Location search and drawing an import rectangle on a locator map remain possible UI extensions.
Editable geographic objects, automatic guideway generation, and aerial imagery require separate designs.
Users must choose a provider that permits their intended requests and saved-project use.
The adapter preserves attribution and export notices but does not supply a provider agreement.

**London presets:** LondonCentral supplies a generated geographic network independently of the background importer.
It uses a normalized TfL topology snapshot for 96 passenger stations, their real names and locations, and 127 unique adjacencies.
The preset adds twin directed guideways, off-line berths, and three Parking facilities.
The local projection uses meters.

A separate normalized 2019 midweek NUMBAT profile provides 8,474 in-scope OD pairs across eight time bands.
The portable London project includes these bands, and the live session can generate requests from a selected band.
The fixed 40-request AM peak sample completed every request.

Directional portals now keep opposite guideways and unrelated corridors on separate station resources.
A high-load A/B run reduced peak stopped pods from 60 to three.
The portal network drained all 199 requests, while the old network left five requests after 60 simulated minutes.

A capacity sweep measured the portal network across all eight bands, 15 offered rates, and three seeds.
Recovery limits range from 7 requests per minute in Early to 14 in Interpeak and Evening.
In every band except Early, the results suggest that the 114-pod fleet, not track congestion, sets the limit.
See [docs/london.md](docs/london.md) and [docs/qualification.md](docs/qualification.md#london-capacity-envelope).

The network does not include a background map or stored tunnel depth.
The optional Overpass adapter supplies schematic backgrounds.
Automatic guideway generation and editable OSM vectors remain future work.
Local georeferenced image import is implemented and preserves frame and attribution data.
LondonFull is a separate generated preset with 269 passenger sites and 2024 endpoint demand.
Its [302-arm study](docs/london-full.md#finite-arrival-study-september-29-2026) used 60 minutes of arrivals and up to 60 minutes to recover.
No fleet candidate met the all-ten recovery criterion at its next tested rate.
These finite tests do not establish sustainable capacity or replace the LondonCentral measurements above.

Explicit separation groups distinguish unrelated grade-separated paths.
Directional portals retain geometric checks at real diverges and merges.
The London AM peak sample now runs the separation oracle once per simulated second.

Each station lane also identifies its approach, entry, berth access, through, departure, or exit role.
Snapshots and the pod inspector use these roles to report station maneuvers without changing the existing traffic controller.

### Congestion-aware routing

Compare the initial shortest expected travel-time policy with a policy that accounts for observed queues and delays.
An alternative route may be longer but faster under the current load.

Use smoothed travel-time estimates and reconsider routes at suitable junctions.
Investigate whether repeated route changes cause pods to switch between alternatives or only move congestion elsewhere.
Keep route choice separate from the movement rules that prevent conflicting access to track and junctions.

Useful comparisons include completed journeys, journey-time distributions, queue lengths, and empty-pod travel.
Repeat each policy comparison with the same demand and random seeds.
SUMO's [taxi dispatch documentation](https://sumo.dlr.de/docs/Simulation/Taxi.html) provides examples of dispatch using current, smoothed travel times.

**Status:** A first experimental policy added costs for owned track and stopped pods when it assigned a route.
It served fewer requests than free-flow routing, which remains the default.
See [docs/qualification.md](docs/qualification.md#congestion-aware-routing-experiment).

Both costed policies now apply costs only to the route that a pod gets, and their routes do not go through the berths of a third station.
A second policy, `queue`, adds the part of each queue that remains when the pod gets to it.
In rail-hub, scale100, London-192, and the London envelope, each `queue` result is equal to free-flow, so the policy is not adopted.
Most delays that it sees are on the departure lane of the pod, which no route can avoid.
The guarded `congestion` arm serves more requests in the congested London-192 Early band, but it lowers two London band limits.
Congestion-aware routing is parked.
Free-flow routing stays the default, and the `congestion` arm keeps its two guards.
The plan made a cost from the planned routes of the pods the next candidate, but only if platoons or shared rides do not relieve the congested Early band.
Platoons relieve it.
The platoon A/B with 198 pods raises the Early limit from 10 to at least 12 requests per minute.
The London preset with `platoonLimit` 4 raises it from 7 to 10 requests per minute.
Thus the condition of that candidate is not met, and the planned-route cost stays parked with the other routing work.
See [docs/qualification.md](docs/qualification.md#queue-routing-screen).

### Mixed vehicle capacities and shared rides

Explore a fleet with personal pods and a smaller number of larger shared vehicles.
Larger vehicles need suitable demand and service policies.
Additional seats alone do not improve the initial one-party-per-pod service.

A useful progression is:

1. Allow several parties with the same destination to share a pod.
2. Use larger pods for busy hub-to-hub journeys.
3. Allow shared journeys with intermediate pickups and drop-offs.

Study the tradeoff between waiting for more passengers and leaving immediately with empty seats.
For multiple destinations, include limits on detours and additional stops.
Account for vehicle length, capacity, acceleration, boarding time, and berth compatibility.

Keep passenger capacity distinct from the number of parties aboard.
Compare passenger throughput, waiting, occupancy, and empty running across fleet mixes.
MATSim's [demand-responsive transport module](https://github.com/matsim-org/matsim-libs/blob/main/contribs/drt/README.md) supports shared taxis or minibuses and additional pickups during occupied journeys.

**Status:** Step 1 is available as the `destination` shared ride mode.
In this mode, a limit of two to eight parties lets a party join a pod that is still boarding at the same origin for the same destination.
The default limit of one disables sharing.
See [docs/qualification.md](docs/qualification.md#same-destination-sharing).

The drop-offs mode, a part of step 3, is the default mode when sharing is on.
A party can join a boarding pod that passes its destination, or a pod that can add that destination as its last stop.
The pod stops at each destination on its way.
The stops do not change after the pod departs, and a pod with passengers does not pick up parties.
A pod adds a stop only when the planned detour ratio of each party, new or aboard, is 1.5 or less.
In London at a limit of 4, the mode raises five 60-minute band limits and lowers the journey time, the wait, and the empty distance in each band.
The first measurement did not meet two of the seven adoption rules, because five arms ended late and the largest detour ratio was 1.509.
With the detour cap, the mode meets the seven rules on the full envelope with seeds 1 to 3 and with 10 seeds at the rates near the band limits.
On 2026-09-28, the user made drop-offs the default mode, and same-destination sharing stays available as an option.
See [docs/qualification.md](docs/qualification.md#drop-offs-sharing-in-london).

### Railway and park-and-ride hubs

Point-to-point pod journeys can already concentrate at a hub.
The extensions to study are time-dependent demand, transfer delays, and connections to scheduled services.

Compare these invented demand scenarios:

- Twelve passengers arrive each minute.
- A train delivers 120 passengers every ten minutes.

Both produce the same average demand.
The experiment should compare queues, berth requirements, and the benefit of positioning empty pods before a train arrives.

Initially, represent a train arrival as an event that releases passenger parties into the hub after a walking delay.
Represent outbound trains with departure times and a minimum transfer time.
This allows connection experiments without first implementing a full railway simulator.

For a park-and-ride hub, use time-varying arrivals and departures to represent morning and evening flows.
Later experiments can add parking capacity or explicit journeys by car.

Study loading areas, vehicle storage, and station exits as separate possible bottlenecks.
Measure queue-clearance time, waiting-time distributions, missed connections, and unmet demand.
SUMO's [intermodal routing documentation](https://sumo.dlr.de/docs/IntermodalRouting.html) describes journeys with walking, waiting, and multiple transport modes.

### Platoons and coupled pod trains

Distinguish two models:

- A virtual platoon consists of separate pods that coordinate their movement.
- A coupled group consists of pods physically connected into a train.

Both require rules for formation, splitting, merging, and compatible routes.
Track spacing within a group separately from spacing between groups.
Include the space and time needed to assemble a group and separate it before destinations diverge.

Compare faster travel against the delay incurred while waiting to assemble a group.
Test whether longer groups obstruct merges or station access.
Reduced headway must follow an explicit control model rather than a capacity multiplier.

Measure passenger throughput, travel time, and empty running first.
Energy comparisons require an energy model with explicit assumptions about speed, resistance, acceleration, and braking.
Do not infer energy savings from shorter spacing alone.

[Plexe](https://plexe.car2x.org/) provides examples of cooperative maneuvers, vehicle dynamics, and platoon control.
It is a research reference, not a proposed dependency for Podsim.

**Status:** Virtual platoons are a project option, `platoonLimit`, and they are off by default.
A slow pod in a queue can follow the pod ahead at a short gap and share its track cells, in platoons of up to 4 pods.
Each link keeps a fixed certificate of a run of lanes that turns 120 degrees or less, and its clearance follows from that turn.
On the synthetic corridor, platoons of 4 give 2.5 to 3.1 times the flow of single pods.
A London sweep with 198 pods found that track and junction flow limit the Early band at 9 to 12 requests per minute.
On that load, platoons raise the Early limit from 10 to at least 12 requests per minute.
The AM peak and PM peak limits do not change.
The A/B meets the seven adoption rules of the design.
Seeds 4 to 10 and the rail-hub checks of the design are not run.
The editor sets the option, the state frame gives the platoon of each pod, and the view draws a line between the pods of a platoon.
A saved state keeps the links.
The London preset turns platoons on with a limit of 4 pods.
In the London capacity envelope with 114 pods, they raise the Early limit from 7 to 10 requests per minute, and no band limit falls.
See [docs/qualification.md](docs/qualification.md#platoon-screening) and [the envelope with platoons](docs/qualification.md#with-virtual-platoons).

### Suggested first extension experiment

After the initial simulator works, build a railway-hub scenario with a burst of arriving passengers.
Compare immediate personal departures, same-destination sharing, and advance positioning of empty pods.
Then add an alternative route to test congestion-aware routing.

This provides a small, observable experiment before introducing mixed fleets or platoons.
The sequence is a recommendation, not a committed roadmap.

**Status:** The repeatable `rail-hub` preset, finite burst schedule, station capacity measurements, and advance-positioning comparison are complete.
The five-seed experiment served all demand with either policy.
The experiment used the weighted redistribution policy, which moved empty pods toward the demand weights at any load.
Guarded positioning later replaced that policy.
The weighted policy reduced mean pickup wait by about four seconds, added 45.2 km of empty travel, and reduced loaded distance from 45.96% to 42.15%.
See [docs/qualification.md](docs/qualification.md#rail-hub-burst-experiment).

With redistribution off, the four-party sharing arm reduced mean wait by 65%, queue clearance by 53%, and empty travel by 49%.
It served every request.

In a three-seed alternate-route experiment, the first occupied-track snapshot-cost policy served 1.33 fewer requests on average than free-flow routing.
It also increased mean wait and added empty travel.
Free-flow routing remains the default.

## 7. Research and reference tools

Research to date covers documentation, papers, and archive metadata.
This investigation did not run the referenced simulators.
Historical descriptions do not establish current availability or compatibility.

### Closest research paper

Ingmar Andréasson's 2009 paper, [Extending PRT Capabilities](https://www.advancedtransit.org/wp-content/uploads/2011/08/Extending-PRT-capabilities.pdf), is a short introduction to several proposed extensions.
It covers routing around overloaded links, shared rides after train arrivals, empty-pod platoons, and coupled pods on faster guideways.
It also discusses preparing pods before trains arrive and arranging transfer stations.

The paper reports experiments using PRTsim, with train formation implemented as pairs of vehicles.
Its capacity results depend on historical model assumptions.
Treat them as experiment ideas, not general performance guarantees.

### Tools to study

| Reference | What to study | Context and limits |
|---|---|---|
| [Podaris](https://support.podaris.com/podarisplan-overview) | Map editing, transport layers, service planning, and scenario comparison | Useful interface and planning reference. Its documented demand simulations concern mode and route choices. |
| [Homerick's PRT-Sim](https://www.inist.org/library/2010-12-00.Homerick.PRT-Sim%20MicroSimulator%20for%20PRT.UCSC.pdf) | Separation of simulation and controllers, scheduled shared services, and editor screenshots | Historical open-source project. The 2010 thesis describes both PRT and conventional scheduled-service controllers. |
| [Andréasson's PRTsim](https://www.advancedtransit.org/wp-content/uploads/2011/08/Extending-PRT-capabilities.pdf) | Congestion avoidance, rail-transfer surges, shared rides, and coupled vehicles | Distinct from Homerick's PRT-Sim. Study published experiments. Research to date did not establish current public access. |
| [SUMO](https://sumo.dlr.de/docs/Simulation/Taxi.html) | Dispatch, shared rides, capacity limits, travel-time estimates, and external control algorithms | Practical reference for operational behavior. Its broader road-traffic model is beyond Podsim's initial needs. |
| [Plexe](https://plexe.car2x.org/) | Platoon formation, cooperative maneuvers, dynamics, and control | A framework extending SUMO and Veins. Relevant when studying platooning. |
| [MATSim DRT](https://github.com/matsim-org/matsim-libs/blob/main/contribs/drt/README.md) | Shared taxis or minibuses, pooling, and additional pickups | Useful reference for shared-service policies. |
| [PRT Consulting](https://prtconsulting.com/personal-rapid-transit-simulation.html) | Station analysis, demand matrices, operating characteristics, and comparison between models | Describes NETSIMMOD and use of Andréasson's PRTsim. A consultancy reference rather than a confirmed public software download. |

Podaris documents [PRT-specific layers](https://support.podaris.com/personal-rapid-transit) and [demand simulations](https://support.podaris.com/simulations).
Its [2019 article](https://blog.podaris.com/prt/) separately describes integration with a vendor's detailed microsimulator.
Do not assume that its planning tools reproduce individual pod movement and traffic control in the way proposed for Podsim.

### Historical archives and further leads

- The [University of Washington catalog](https://faculty.washington.edu/jbs/itrans/simu.htm) was last modified on September 27, 2013.
  It lists Hermes, BeamEd, RUF, and other simulators to investigate if the project needs further examples.
  Its availability claims need fresh verification.
- The [Google Code PRT-Sim archive](https://code.google.com/archive/p/prt-sim/) did not render through the research browser.
  Its [project metadata](https://storage.googleapis.com/google-code-archive/v2/code.google.com/prt-sim/project.json) and [source index](https://storage.googleapis.com/google-code-archive/v2/code.google.com/prt-sim/source-page-1.json) remained accessible.
  Metadata describes an alpha-stage Python control-system testbed with GPLv3 source.
  The project has not installed or reused any legacy code.
- [SUMOPy documentation](https://sumo.dlr.de/docs/Contributed/SUMOPy.html), now describing hybridPY, lists support for PRT services.
  Schweizer and Rupi's 2017 paper, [Personal Rapid Transit simulations with SUMO](https://cris.unibo.it/handle/11585/631734), is a further reading lead.
  The research browser could not retrieve the full proceedings PDF, so the detailed results remain unreviewed.

### Original simulator and browser technology

- [Original ATS/CityMobil tutorial](https://ultraglobalprt.com/about-us/library/ultra-simulator/)
- [Historical course page and simulator download](https://jdlm.info/emat20005/)
- [2010 ATS/CityMobil ZIP](https://jdlm.info/emat20005/atscitymobil-20101123.zip)
- [Ebitengine browser deployment](https://ebitengine.org/en/documents/webassembly.html)
- [Go WebAssembly support](https://go.dev/wiki/WebAssembly)
- [Slow Roads technical case study](https://web.dev/case-studies/slow-roads)

The ATS/CityMobil ZIP downloaded successfully during the initial investigation.
It contained a launcher, Java components, and bundled case studies.
The investigation did not run the program.

### Suggested study order

1. Read Andréasson's short paper for concrete network experiments.
2. Read Homerick's architecture discussion and inspect the editor screenshots.
3. Study SUMO's dispatch and intermodal examples.
4. Use Podaris for editor and planning ideas.
5. Return to MATSim and Plexe when shared-service and platooning experiments become relevant.

The initial feature groups and acceptance scenarios defined the initial usable version.
Section 5 records its acceptance.
