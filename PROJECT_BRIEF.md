# Podsim: Project Brief and Research

**Status:** The initial usable 2D version was accepted on September 21, 2026.

The browser supports local map backgrounds, scale calibration, network editing,
project persistence, manual and automatic demand, pod dispatch, local traffic
control, inspection, pod following, and fleet-use statistics.
The server owns one shared simulation session for all connected browsers.
Optional empty-pod redistribution is available but remains off by default.
The first railway-hub capacity experiment is complete.
Detailed station maneuvers, protocol compaction, and the other experiments in
Section 6 remain later work.
See [README.md](README.md) for controls, validation commands, and current model limits.

This document records the project direction, initial feature scope, architecture, effort estimates, and research.
The initial scope and policies are a starting point for implementation planning.
Future experiments are separate possibilities, not initial delivery commitments.

## 1. Product vision

Build a browser-based playground for designing and observing personal rapid transit networks.

The user imports a map, draws guideways, places stations, and watches pods serve passenger journeys.
The main interest is network behavior: routing, demand, dispatch, congestion, and station capacity.

Use **Go and Ebitengine, compiled to WebAssembly**.
Target desktop Chrome with mouse and keyboard.
The browser renders the application and sends commands to a Go server.
The server owns the simulation clock and sends snapshots to connected browsers.

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
- Create explicit junctions. Crossing lines do not connect automatically.
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
- Select a pod to inspect its destination, route, and current activity.
- Follow a pod with the map camera.
- Inspect station queues and berth occupancy.
- Show completed journeys, waiting times, and fleet utilization.
- Explain why a selected pod is waiting.

Network edits require a stopped run.
Applying edits resets simulation state while preserving the design.

### Station modeling direction

Physical station layouts are an intended capability, not merely a possible visual upgrade.
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
- Save the scenario configuration rather than an exact running simulation checkpoint.

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

Ebitengine owns drawing and interaction.
A small browser adapter handles file selection and downloads.

Define a clear boundary between user commands, simulation updates, and state exposed for display.
Avoid a general plugin system in the first version.

### Typed client protocol

Keep the current HTTP and JSON protocol while its measured traffic remains
manageable. Before changing the wire contract, evaluate
[ConnectRPC](https://connectrpc.com/) with Protocol Buffers as the schema source
for generated Go and browser clients. Connect supports binary Protocol Buffers,
JSON, compression, and browser-compatible RPC.

Do not treat binary encoding alone as the complete optimization. The current
client repeatedly receives a complete state, including static network data and
routes. Compare a normalized or delta state protocol with binary complete
snapshots, and measure response size, server cost, and browser update cost.
Preserve the current authoritative server and retry-safe command identity.

### Separate passengers, vehicles, and service policies

Keep passenger requests and party size separate from vehicles and assignments.
A request describes a desired journey. A vehicle describes the pod available to serve it.
The service policy determines how requests use vehicles.

Keep demand generation, dispatch, route choice, and physical movement separate:

- Demand generation determines when and where parties request journeys.
- Dispatch assigns vehicles to requests and decides where empty pods should wait.
- Route choice selects a path through the network.
- Physical movement enforces speed, spacing, junction access, and station capacity.

These boundaries allow later experiments to change one policy while retaining the same network and movement model.
Use explicit types and functions. Add interfaces when there is a concrete need for interchangeable policies.
Do not implement mixed fleets, transfers, or platooning to establish these boundaries.

### What a later Three.js version would involve

A future browser renderer could consume snapshots from the same Go simulation server.

**Reusable:** network data, routing, demand, dispatch, movement rules, statistics, and most tests.

**New work:** JavaScript or TypeScript presentation code, 3D track geometry, pod models, cameras, selection, and scenery.

Ebitengine's drawing code would not automatically become Three.js code.
A first-person ride would therefore be a substantial presentation extension, while retaining the simulation investment.

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

Keep multiplayer, live map services, realistic scenery, legacy simulator file import, and advanced fleet optimization outside the first release.
The experiments in Section 6 also remain outside the initial scope.
Their effort has not been estimated, except where an optional stage appears in the table above.

## 5. Acceptance and validation

### Simulation tests

- Pods choose valid routes through one-way and paired-lane networks.
- Unreachable journeys produce a clear result.
- Competing pods pass through a merge without overlap.
- A full station does not accept more pods than its capacity permits.
- A finite fleet leaves excess demand waiting.
- Empty-pod dispatch eventually serves waiting parties in a feasible, uncongested test network.
- Identical scenarios and seeds produce repeatable outcomes.
- Changing playback speed preserves simulation results at equal simulated times.

### Browser checks

- Import an image, calibrate scale, draw a network, and complete a journey.
- Export and reload the project with its background intact.
- Exercise undo, invalid input, pause, reset, and pod following.
- Run a proposed baseline of 20 stations and 100 pods.
- Record hardware, frame rate, and simulation update cost before establishing performance guarantees.

**Acceptance status:** Complete for the initial usable version on September 21,
2026. A combined browser run imported a PNG, calibrated 200 meters, exercised
drawing and undo, edited and applied the network, exported and reloaded the
project with its background, submitted a journey through the simulation UI,
and observed its completion. It also toggled pod following and reported no
browser errors. Separate browser checks cover invalid input, reset, stale edit
conflicts, and the 20-station, 100-pod scenario. The hardware and performance
record is in [docs/qualification.md](docs/qualification.md).

## 6. Future extensions and experiments

The following ideas extend the map workflow and network simulation.
They can all be explored in 2D.
These are proposed experiments and design considerations, not verified Podsim capabilities.

### Geographic map import

Allow the user to select an area of a real location and import it directly as a map background.
OpenStreetMap is a candidate source, not a selected integration provider.

The intended workflow is:

1. Find a location on a map.
2. Draw a rectangle around the area to use.
3. Import the background with its geographic bounds and automatic scale.
4. Draw the pod network over that background.

Preserve the geographic bounds and coordinate transformation with the project.
Convert geographic coordinates into local world units with an appropriate map projection.
This avoids manual screenshot measurement while keeping simulation distances independent of display pixels.
Retain manual scale calibration for ordinary image files without geographic metadata.

OpenStreetMap distinguishes [raw geographic data exports from rendered map images](https://wiki.openstreetmap.org/wiki/Export).
Start by studying a rendered background with geographic metadata.
Importing streets and buildings as editable geographic objects would be a separate extension.
Imported streets do not automatically become pod guideways.
Aerial imagery would require a separate imagery source.

Choose a source that supports the intended area export and saved-project use, and preserve its attribution.
The public OSM tile endpoint is not a bulk or offline export service, as its [tile usage policy](https://operations.osmfoundation.org/policies/tiles/) explains.
Provider choice, export limits, and any hosting needs remain implementation decisions for this later feature.

### Congestion-aware routing

Compare the initial shortest expected travel-time policy with a policy that accounts for observed queues and delays.
An alternative route may be longer but faster under the current load.

Use smoothed travel-time estimates and reconsider routes at suitable junctions.
Investigate whether repeated route changes cause pods to switch between alternatives or simply move congestion elsewhere.
Keep route choice separate from the movement rules that prevent conflicting access to track and junctions.

Useful comparisons include completed journeys, journey-time distributions, queue lengths, and empty-pod travel.
Repeat each policy comparison with the same demand and random seeds.
SUMO's [taxi dispatch documentation](https://sumo.dlr.de/docs/Simulation/Taxi.html) provides examples of dispatch using current, smoothed travel times.

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

### Railway and park-and-ride hubs

Point-to-point pod journeys can already concentrate at a hub.
The important extensions are time-dependent demand, transfer delays, and connections to scheduled services.

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

### Suggested first extension experiment

After the initial simulator works, build a railway-hub scenario with a burst of arriving passengers.
Compare immediate personal departures, same-destination sharing, and advance positioning of empty pods.
Then add an alternative route to test congestion-aware routing.

This provides a small, observable experiment before introducing mixed fleets or platoons.
The sequence is a recommendation, not a committed roadmap.

**Status:** The repeatable railway-hub preset, finite burst schedule, station
capacity measurements, and advance-positioning comparison are complete.
The five-seed experiment served all demand with either policy. Advance
positioning reduced mean pickup wait by about four seconds, added 45.2 km of
empty travel, and reduced loaded distance from 45.96% to 42.15%.
See [docs/qualification.md](docs/qualification.md#rail-hub-burst-experiment).
Same-destination sharing and the alternative-route experiment remain future work.

## 7. Research and reference tools

Research to date covers documentation, papers, and archive metadata.
The referenced simulators have not been run as part of this investigation.
Historical descriptions do not establish current availability or compatibility.

### Closest research paper

Ingmar Andréasson's 2009 paper, [Extending PRT Capabilities](https://www.advancedtransit.org/wp-content/uploads/2011/08/Extending-PRT-capabilities.pdf), is a short introduction to several proposed extensions.
It covers routing around overloaded links, shared rides after train arrivals, empty-pod platoons, and coupled pods on faster guideways.
It also discusses preparing pods before trains arrive and arranging transfer stations.

The paper reports experiments using PRTsim, with train formation implemented as pairs of vehicles.
Its capacity results depend on historical model assumptions and should be treated as experiment ideas, not general performance guarantees.

### Tools to study

| Reference | What to study | Context and limits |
|---|---|---|
| [Podaris](https://support.podaris.com/podarisplan-overview) | Map editing, transport layers, service planning, and scenario comparison | Useful interface and planning reference. Its documented demand simulations concern mode and route choices. |
| [Homerick's PRT-Sim](https://www.inist.org/library/2010-12-00.Homerick.PRT-Sim%20MicroSimulator%20for%20PRT.UCSC.pdf) | Separation of simulation and controllers, scheduled shared services, and editor screenshots | Historical open-source project. The 2010 thesis describes both PRT and conventional scheduled-service controllers. |
| [Andréasson's PRTsim](https://www.advancedtransit.org/wp-content/uploads/2011/08/Extending-PRT-capabilities.pdf) | Congestion avoidance, rail-transfer surges, shared rides, and coupled vehicles | Distinct from Homerick's PRT-Sim. Study published experiments. Current public access has not been established. |
| [SUMO](https://sumo.dlr.de/docs/Simulation/Taxi.html) | Dispatch, shared rides, capacity limits, travel-time estimates, and external control algorithms | Practical reference for operational behavior. Its broader road-traffic model is beyond Podsim's initial needs. |
| [Plexe](https://plexe.car2x.org/) | Platoon formation, cooperative maneuvers, dynamics, and control | A framework extending SUMO and Veins. Relevant when studying platooning. |
| [MATSim DRT](https://github.com/matsim-org/matsim-libs/blob/main/contribs/drt/README.md) | Shared taxis or minibuses, pooling, and additional pickups | Useful reference for shared-service policies. |
| [PRT Consulting](https://prtconsulting.com/personal-rapid-transit-simulation.html) | Station analysis, demand matrices, operating characteristics, and comparison between models | Describes NETSIMMOD and use of Andréasson's PRTsim. A consultancy reference rather than a confirmed public software download. |

Podaris documents [PRT-specific layers](https://support.podaris.com/personal-rapid-transit) and [demand simulations](https://support.podaris.com/simulations).
Its [2019 article](https://blog.podaris.com/prt/) separately describes integration with a vendor's detailed microsimulator.
Do not assume that its planning tools reproduce individual pod movement and traffic control in the way proposed for Podsim.

### Historical archives and further leads

- The [University of Washington catalog](https://faculty.washington.edu/jbs/itrans/simu.htm) was last modified on September 27, 2013.
  It lists Hermes, BeamEd, RUF, and other simulators worth investigating if further examples are needed.
  Its availability claims need fresh verification.
- The [Google Code PRT-Sim archive](https://code.google.com/archive/p/prt-sim/) did not render through the research browser.
  Its [project metadata](https://storage.googleapis.com/google-code-archive/v2/code.google.com/prt-sim/project.json) and [source index](https://storage.googleapis.com/google-code-archive/v2/code.google.com/prt-sim/source-page-1.json) remained accessible.
  Metadata describes an alpha-stage Python control-system testbed with GPLv3 source.
  No legacy code has been installed or reused.
- [SUMOPy documentation](https://sumo.dlr.de/docs/Contributed/SUMOPy.html), now describing hybridPY, lists support for PRT services.
  Schweizer and Rupi's 2017 paper, [Personal Rapid Transit simulations with SUMO](https://cris.unibo.it/handle/11585/631734), is a further reading lead.
  The full proceedings PDF could not be retrieved through the research browser, so detailed results remain unreviewed.

### Original simulator and browser technology

- [Original ATS/CityMobil tutorial](https://ultraglobalprt.com/about-us/library/ultra-simulator/)
- [Historical course page and simulator download](https://jdlm.info/emat20005/)
- [2010 ATS/CityMobil ZIP](https://jdlm.info/emat20005/atscitymobil-20101123.zip)
- [Ebitengine browser deployment](https://ebitengine.org/en/documents/webassembly.html)
- [Go WebAssembly support](https://go.dev/wiki/WebAssembly)
- [Slow Roads technical case study](https://web.dev/case-studies/slow-roads)

The ATS/CityMobil ZIP downloaded successfully during the initial investigation.
It contained a launcher, Java components, and bundled case studies. The program was not executed.

### Suggested study order

1. Read Andréasson's short paper for concrete network experiments.
2. Read Homerick's architecture discussion and inspect the editor screenshots.
3. Study SUMO's dispatch and intermodal examples.
4. Use Podaris for editor and planning ideas.
5. Return to MATSim and Plexe when shared-service and platooning experiments become relevant.

This brief is independent of any implementation framework.
Its initial feature groups and acceptance scenarios can become a focused PRD or implementation tasks in the workflow selected later.
