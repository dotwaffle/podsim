# Podsim: Project Brief

This brief gives the current goals and architecture of Podsim.
The [README](README.md) tells how to run it, and the [document index](README.md#documents) lists the guides, contracts, designs, and studies.
Git history keeps the earlier plans, status notes, and effort estimates.

## 1. Product vision

Podsim is a browser-based playground for designing and observing personal rapid transit networks.
The user draws guideways over a map, places stations, and watches pods serve passenger journeys.
The main interest is network behavior: routing, demand, dispatch, congestion, and station capacity.

Podsim is a hobby simulator.
Engineering certification, construction planning, and accurate predictions for real transport systems are outside its purpose.

The test of success is that a user can build a small network over a familiar map, run it, understand where delays occur, and change the design.
The experience must stay useful and enjoyable without a future 3D version.

Desktop Chrome with mouse and keyboard is the target, so Firefox and Safari validation are not required.
The simulator runs on a local machine, and hosted deployment and origin validation wait for release preparation.
Software-rendering measurements do not establish physical GPU performance.

## 2. Principles

- **Show the problem**: the simulation exposes congestion and unmet demand, and it does not add vehicles to hide them.
- **Local queues**: pods depart when local conditions permit, then slow or wait for occupied track, merges, or station access.
  A journey needs no reserved timetable, and local admission and spacing rules prevent conflicting movements.
- **Explicit crossings**: crossing lines do not connect, and each direction uses a separate lane.
- **Stations are places**: berths and station entry and exit connections have stable identities, separate from the map symbol of the station, and berths stay separate from through traffic.
- **Repeatable runs**: a saved scenario and a seed give repeatable results, and changing playback speed does not change them.
- **Simple defaults**: routing uses the shortest free-flow travel time, and dispatch uses the nearest available pod.
  One party per pod and no intermediate stops are the default service, and they are policies and not limits of the model.
- **Evidence before a default**: an experimental policy stays opt-in until it passes the [adoption gates](docs/design/experimental-adoption.md), and a study records its limits.

## 3. Architecture

The server owns one simulation session for all clients, and the clients only show it and send commands.
The [simulation model](docs/model.md) gives the behavior, the [client protocol](docs/protocol.md) gives the messages, and the [operations guide](docs/operations.md#code-layout) gives the code layout.

### Simulation core

Ordinary Go packages hold the network, routing, dispatch, movement, traffic control, demand, statistics, and project validation.
The simulation core does not import Ebitengine, browser APIs, or I/O.
It stores geometry in world units, advances in fixed time steps, and keeps rendering independent of simulation speed.
Commands, simulation updates, and the state exposed for display have a clear boundary.
There is no general plugin system.

Passengers, vehicles, and service policies stay separate.
An order describes a desired journey, a pod is the vehicle available to serve it, and the service policy decides how orders use pods.
Demand, dispatch, route choice, and movement also stay separate, so that an experiment can change one policy and keep the same network and movement model:

- Demand generation decides when and where parties order journeys.
- Dispatch assigns pods to orders and decides where empty pods wait.
- Route choice selects a path through the network.
- Physical movement enforces speed, spacing, junction access, and station capacity.

The code uses explicit types and functions, and it adds an interface when there is a concrete need for interchangeable policies.

### Clients and protocol

Ebitengine draws the simulation view and handles its controls, in the browser through WebAssembly and on the desktop as a native client.
The view gets shared gzip JSON baselines and deltas over WebSocket, and it needs native browser gzip, with no HTTP polling fallback.
Commands, topology, and editor data use HTTP, and the server keeps retry-safe command identity.
The September 22 evaluation found that Protocol Buffers would reduce a London gzip frame by another 10.3% and save about 0.7% of one CPU core per client at 20 Hz, so the project removed the experimental service.
Review this choice if remote traffic, multiple viewers, or external clients make a typed RPC protocol useful.

### Editor

The scenario editor is a separate HTML and JavaScript page with a Go/WASM worker, which does its checks off the main browser thread.

| Part | Owns |
| --- | --- |
| Go worker | Project checks, settings, fleet and rail edits, geometry edit proposals, demand profile construction, place-search projection, live-map and geographic-reference edits, image placement and calibration, normalization of drafts, and the undo and redo timeline with background metadata. |
| JavaScript | Browser controls, drawing, storage, image bytes and decoding, storage pins, license metadata, drawing copies by Go snapshot ID, and drag previews. |

The worker keeps changed project branches and reuses topology and demand flow checks until their inputs change.
JavaScript orders asynchronous edits, keeps newer typing when an older worker reply arrives, keeps queued input, and waits for pending edits before apply, export, undo, or redo.
Image imports prepare any history trim before publication and keep the image table within 128 MiB.
A worker failure blocks an apply and keeps the visible draft available for export.

### Persistence

A project file holds the design and settings, and it has a versioned format.
A save point is an exact copy of the running simulation in server memory, and a server restart clears it.
The `-state` saved state keeps the live session across a restart on a best-effort basis, and the [restore rules](docs/operations.md#session-state) say what it keeps.
A damaged saved state moves aside and the server starts a new session, with no recovery.
Applying network edits resets the simulation and keeps the design.

## 4. Scope limits

These stay out of the project until someone decides otherwise.

| Item | Status |
| --- | --- |
| Multiplayer beyond one shared session, realistic scenery, legacy simulator file import, and advanced fleet optimization | Not planned. |
| Editable geographic objects, automatic guideway generation, editable OSM vectors, and aerial imagery | Shelved at the maintainer's request. |
| Opposing traffic on shared single track | Not built. It needs reservations, fairness, and deadlock handling. |
| A 3D client | Not built. Do not build speculative 3D infrastructure now. |
| Stored tunnel depth, elevation, and terrain | Not in the model. Adding them can change geometry and saved projects. |

A later Three.js renderer could read snapshots from the same Go server.
The network data, routing, demand, dispatch, movement rules, statistics, and most tests would stay.
JavaScript or TypeScript presentation code, 3D track geometry, pod models, cameras, selection, and scenery would be new, and Ebitengine drawing code would not carry over.

## 5. Current experiments

Each study records what it establishes, and no study authorizes a default change until it passes the gates.
The defaults stay: free-flow routing, one party per pod, no platoons on a project that does not set them, and no redistribution.
The pickup and busy-station service problems stay unresolved.

| Area | Status | Record |
| --- | --- | --- |
| Congestion-aware routing | Free-flow routing is the only policy. `congestion`, `queue`, and `predictive` had no service gain and were removed. Rerouting at junctions is not built. | [Queue routing screen](docs/studies/qualification.md#queue-routing-screen), [predictive routing](docs/studies/predictive-routing.md) |
| Shared rides | `destination` and `drop-offs` are opt-in, and `drop-offs` is the default mode when sharing is on. `reassign-existing` fails the adoption rules. Waiting for more passengers against leaving at once is not studied. | [Drop-offs sharing](docs/studies/qualification.md#drop-offs-sharing-in-london), [policy failures](docs/studies/policy-failures.md) |
| Mixed vehicle classes | Legacy, Compact, Group, and opt-in Express pods exist, and no default changes. | [Group physics](docs/design/group-physics-design.md), [Express qualification](docs/studies/express-qualification.md) |
| Pickup reassignment | Opt-in, off by default. It lowers mean waits in tested pairs and has individual tail regressions. | [Sustained swaps](docs/studies/pickup-swap-sustained.md) |
| Rail and park-and-ride hubs | Rail arrivals, train connections, and daily park-and-ride profiles are in the live simulation. Forecast positioning stays in the compare command. Car journeys and parking capacity are offline in `cmd/parkride`. | [Rail arrivals](docs/rail-arrivals.md), [train connections](docs/rail-connections.md), [car journeys](docs/design/car-journey-design.md) |
| Virtual platoons | A project option, and LondonCentral and LondonFull turn it on. The expanded study fails the recovery and no-harm rules. | [Platoon follow-up](docs/studies/platoon-followup.md) |
| Stations | Compact bays and depots are on the London presets, and independent banks are available. Station buffers were removed on October 7, 2026, because they lowered entry throughput. | [Compact bays](docs/model.md#compact-bays), [depots](docs/model.md#parking-depots), [buffers](docs/studies/buffer-late-claim.md#station-entry-throughput) |
| London capacity | Finite tests show no sustainable capacity envelope. LondonFull grows backlogs at 15 and 20 orders per minute, and no fleet candidate met the all-ten recovery criterion. | [LondonCentral envelope](docs/studies/qualification.md#london-capacity-envelope), [LondonFull post-fix](docs/studies/london-full-postfix.md) |

## 6. Incidents

Faults and emergencies are always on, with no project switch, as the [marker removal contract](docs/contracts/marker-removal-contract-proposal.md) sets.
The exception is the traffic demo: they are off during it and stay off until a reset or a project apply.
Stages 1 to 3 of the incident work landed, and stages 4 to 6 were dropped with no contracts.
The maintainer reopened stage 7 on November 4, 2026, for the scenario rates, under [its contract](docs/contracts/incident-stage7-contract-proposal.md).
Its code landed, its [soak](docs/studies/incident-soak.md) has not run, the soak size is open, and the Suspension median row of its qualification is open.

### Rider emergency stop

A rider in a pod presses an emergency button, for example for a medical emergency, and the pod goes at once to the station where it can unload soonest.
That station is not always the nearest one, because an entry queue on the path can make a farther station faster, so the estimate uses current reservations and queues as well as distance.
The `emergency` command and the **Emergency** button start an emergency (see [emergencies](docs/operations.md#emergencies) and the [contract](docs/contracts/incident-emergency-contract-proposal.md)).

- All parties in the pod unload at that station.
  The party that started the emergency is interrupted, and each other party continues to its original destination and keeps its order identity.
- The pod gets free junctions, track cells, and berths first.
  It never takes an owned resource, and it never forces another pod to reverse or to leave a committed path.
- The pod drops all of its later work, and its pending pickups go back to dispatch as ordinary orders with their original request times.
- There is no scenario emergency rate yet.

### Vehicle fault or accident

A pod stops and cannot move until the fault ends, for example after a collision with debris or a vehicle fault.
The stopped pod blocks its lane, and other pods route around it or wait.
The `fault` command and the **Fault** button start a fault (see [faults](docs/operations.md#faults) and the [contract](docs/contracts/incident-suspension-contract-proposal.md)).

- A fault can have a duration, and it clears when the duration ends or when the user clears it, whichever is first.
- Riders leave the stopped pod after 300 simulated seconds by default, a project can set another delay, and their orders end interrupted.
- A fault can also block a lane segment with no pod in it, as debris.
- Pods do not reverse, because the simulator moves pods only forward on directed lanes, and reverse movement has no contract.
- There is no scenario fault rate yet.

Open question: how the metrics show the effect of an incident on service.

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

SUMO's [intermodal routing documentation](https://sumo.dlr.de/docs/IntermodalRouting.html) describes journeys with walking, waiting, and multiple transport modes, which matters for further hub studies.

### Historical archives and further leads

- The [University of Washington catalog](https://faculty.washington.edu/jbs/itrans/simu.htm) was last modified on September 27, 2013.
  It lists Hermes, BeamEd, RUF, and other simulators to investigate if the project needs further examples.
  Its availability claims need fresh verification.
- The [Google Code PRT-Sim archive](https://code.google.com/archive/p/prt-sim/) has [project metadata](https://storage.googleapis.com/google-code-archive/v2/code.google.com/prt-sim/project.json) and a [source index](https://storage.googleapis.com/google-code-archive/v2/code.google.com/prt-sim/source-page-1.json).
  Metadata describes an alpha-stage Python control-system testbed with GPLv3 source.
  The project has not installed or reused any legacy code.
- [SUMOPy documentation](https://sumo.dlr.de/docs/Contributed/SUMOPy.html), now describing hybridPY, lists support for PRT services.
  Schweizer and Rupi's 2017 paper, [Personal Rapid Transit simulations with SUMO](https://cris.unibo.it/handle/11585/631734), is a further reading lead.
  Its detailed results remain unreviewed.

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
