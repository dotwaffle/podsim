# Reference

This document gives the exact project file format, its limits, and the command-line tools.
The [user guide](user-guide.md) covers the browser controls, and the [simulation model](model.md) explains the behavior that these settings select.

## Project file

A project file holds the design and the settings of a scenario, and it does not hold the exact running state.
Save points hold the exact state, but they stay in server memory and end when the server stops, also with `-state`.
The saved session state of `-state` is a separate file (see [session state](operations.md#session-state)).

### Two forms

A project travels in one of two forms.

| Form | Content | Accepted by |
| --- | --- | --- |
| Server file | The `project` object from `/api/project`, as `mise run scenario` writes it. | `-project`, **Import JSON**, the compare command, and `projectcheck`. |
| Browser export | The `project` object as `scenario`, with an optional background image. | **Import JSON** only. `-project` does not accept it. |

When a file leaves out an optional setting, such as the party limit, the shared ride mode, or the platoon limit, **Import JSON** uses the default value.

With `-project`, the server reads an existing file at startup and keeps it up to date.
It writes compact JSON without indentation.
Before the server applies a setting change, it saves the file with an atomic replacement, and if the save fails, it rejects the change.
A rewind that restores the project of a save point also rewrites the file.
Validation limits each project to 32 MiB in this compact form, also after a demand change, so that the server can read the file at the next start.

### Top-level members

| Member | Value | Absent means |
| --- | --- | --- |
| `version` | The project version, now 1. | Required |
| `name` | The scenario name. | Required |
| `network` | `nodes`, `lanes`, and `stations`, with optional `maneuverGroups` and `depots`. | Required |
| `fleet` | One placement for each pod: `id`, `stationID`, `berthID`, and an optional `class`. | Required |
| `demand` | The demand settings: `enabled`, `perMinute`, `pattern`, `seed`, and the optional `destination`, `profile`, `band`, and `dailyStartMinute`. | Required |
| `redistribution` | A Boolean that turns on [guarded positioning](model.md#redistribution). The presets and the example project set it to true. | Off |
| `demandProfiles` | Origin-destination [demand profiles](#demand-profiles). | None |
| `railArrivals`, `railDepartures` | Fixed train plans. See [scheduled rail arrivals](rail-arrivals.md) and [scheduled train connections](rail-connections.md). | None |
| `sharedRidePartyLimit` | The most [parties](model.md#parties) in one pod, from 1 to 8. | 1 |
| `sharedRideMode` | `drop-offs` or `destination`. | `drop-offs` |
| `sharedRideMaxStops` | The stops before the last stop in `drop-offs` mode, from 1 to 7. | 3 |
| `generatedOrderConsent` | `private` or `shared`, the consent of each generated order. | `private` |
| `onboardPickups` | A Boolean that lets an occupied pod pick up parties. It needs a party limit above 1 and `drop-offs` mode. | Off |
| `pickupReassignment` | A Boolean for [pickup reassignment](#pickup-reassignment). The presets and the example project set it to true. | Off |
| `platoonLimit` | The most pods in one [virtual platoon](model.md#virtual-platoons), from 2 to 4. A value of 0 turns platoons off. | Off |
| `geo` | The [geographic reference](#geographic-reference). | No reference |
| `map` | The [live backdrop](#live-backdrop). | No backdrop |
| `faults` | [Fault settings](operations.md#faults). Faults are on with or without this member. | The default values |
| `emergencies` | [Emergency settings](operations.md#emergencies). Emergencies are on with or without this member. | The default values |
| `orderContract` | `express-v1`, which selects the [Express](model.md#vehicle-classes) order bounds. | Plain bounds |
| `expressServices` | Directed hub pairs for Express, at most 300. | None |

### Generated order consent

The member `generatedOrderConsent` says whether generated passengers can share a pod, and it sits beside the shared ride settings.
With `shared`, each order that the demand run offers has shared consent, with all patterns, including `rail-arrivals` and `rail-services`.
Any other value is refused with `generated order consent must be "private" or "shared"`.
A value of `shared` with a party limit of 1 is valid and pools nothing.

The setting changes with the project, so it takes effect when an apply rebuilds the demand run.
The live `demand` command does not carry it, and a demand run that this command rebuilds keeps the consent of the project.
An apply that changes only this member resets the run, as any project apply does, so waiting and riding orders end.

Traffic demo orders stay private.
The compare command ignores this member, and it keeps its own `-sharing-consent` flag, so that a project file cannot change a study without a trace (see [sharing consent](compare-sharing-consent.md)).
A rider of a `rail-services` connection can share a pod, so a detour can make that rider miss the connection, and the connection counts report the miss.

The editor sets this member with the **Generated passengers** select, in the shared ride section.

### Demand profiles

A demand profile lists weighted flows between stations for named time bands.
It has the members `id`, `name`, `bands`, `stations`, and `flows`.
The `stations` member lists each station that a flow names one time.
Each flow is an array: the index in `stations` of the origin, the index of the destination, and then one weight for each band.
For example, with the stations `["harbor","garden"]` and two bands, the flow `[0,1,3,0.25]` goes from `harbor` to `garden`.
An index is an integer from 0, with no fraction and no exponent.

A project is not valid when its station list has a station two times or a station that no flow names.
The server writes the stations in the order of their first use in the flows.

### Geographic reference

The optional `geo` member places world position 0, 0 on the Earth, for the map tools only, because simulation physics does not use it.
It has `latitude` and `longitude` in degrees, `projection` with the value `equirectangular`, and `radius` with the value 6371000.
The latitude must be from -80 to 80 degrees, and the longitude must be from -180 to 180 degrees.

A point at latitude `lat` and longitude `lon` is at x = R cos(lat0) (lon - lon0) and y = -R (lat - lat0), with lat0 and lon0 the reference, the angles in radians, and R the radius.
Thus x increases to the east, and y increases to the south.

### Live backdrop

The optional `map` member selects the live backdrop, for example `{"provider":"osm","opacity":0.45}`, and it needs `geo`.
Leaving out `map` keeps the plain background.

## Validation and limits

The server and the editor both validate a project, and the server checks it again at each apply.
The editor checks do not include the limits of the lane pairs, the coordinates, and the track cells, so the server checks these when you apply the project.

| Item | Limit |
| --- | --- |
| Project JSON, in compact form | 32 MiB |
| Stations | 600 |
| Nodes | 12,000 |
| Lanes | 20,000 |
| Pods | 1,000 (600 with Express) |
| Berths in a station | 200 |
| Lanes at a node, counted at the start node and at the end node of each lane | 64 |
| Lane pairs, totaled over all nodes | 250,000, for example about 62 nodes with 64 lanes each |
| Coordinate of a node position or a lane control point | -100,000 to 100,000 meters |
| Track cells | About 30 meters each, at least 2 in each lane, and at most 64,000 in all, for example about 1,900 km of lanes |
| Lane length | At least 24 meters. See the exceptions below. |
| Maneuver groups | 8, each with at most 64 lanes, 64 reverse lanes, and 8 berths |
| Depot lanes | 8 for each depot |
| Demand profiles | 8, each with at most 24 bands and 400,000 flows |
| ID | At most 64 bytes of A-Z, a-z, 0-9, `.`, `+`, and `-` |
| Name | At most 80 bytes with no control character |

At each node, the simulator compares each ordered pair of two different lanes at the node when it starts, and that is the count of lane pairs.
Two lanes cannot have the same start node, end node, and path.

Some lanes can be shorter than 24 meters.
A lane of a [maneuver group](model.md#compact-bays) has no minimum length, but its length must be more than 0.
A chain lane of a [depot](model.md#parking-depots) needs only its reduced retention.
A [qualifying slow lane](model.md#slow-lanes) needs 2R meters.
A lane that admits a large class needs at least 40 meters.

Validation also checks routes, because each pair of passenger stations must be connected through the berths.
A route between passenger stations goes from each berth of one station to each berth of the other station, and it can use all lanes.
A berth route goes from the station entry to the berth, or from the berth to the station exit.
It can use a chain of lanes, as in the London stations, and it cannot pass through the entry, exit, or berth node of a station.
A route to a depot slot can pass through the other slots of its depot lane.
For each class of the fleet that has a parking goal, each passenger berth that accepts the class also needs a route to a parking goal and a route from one, for a pod of that class.
A parking goal is a berth of a parking station that accepts the class, or the entry of a depot that accepts it.
Without such a route, an idle pod of the class cannot leave the berth for parking when another pod needs the berth.
The error gives the count of the berths with such a gap and names the first.
Validation further checks pod placement and resource IDs.

Some limits apply to the project in transit:

| Item | Limit |
| --- | --- |
| Command body | 16 MiB. The editor sends the project in one command, and it compresses a command of more than 64 KiB with gzip. |
| Command JSON after decompression | 32 MiB plus 64 KiB |
| Topology JSON | 32 MiB plus 4 KiB (`topology exceeds supported limit`) |

The topology limit is a guard that a valid project does not reach.
A topology at the node, lane, and station limits, with the widest IDs and names, has about 9 MB, where the topology JSON writes each `<`, `>`, and `&` of a name as 6 bytes.

The editor has limits of its own for imports and images (see [Import and export](user-guide.md#import-and-export) and [Import an image](user-guide.md#import-an-image)).

## Compare command

The compare command runs a seeded demand schedule against several policies and reports the differences.
Each pair of arms uses identical orders, so a difference in the results comes from the policy.
This example runs the London preset at 3 orders per minute with redistribution off and on:

```sh
mise run scenario -- -preset london-central -output /tmp/podsim-london.json
mise run compare -- -project /tmp/podsim-london.json -pattern profile -bands am-peak -focus 940GZZLUEUS -loads 20s -duration 30m -redistribution-policies off,on
```

The report gives:

- Demand throughput, backlog, drain time, fleet use, and pickup wait.
- Journey time from order to alight, and the 95th percentile wait and journey time.
- Completed and remaining journeys, and skipped orders.
- Peak queues and berth use at the focus station.
- Passenger and empty travel, loaded-distance percentage, and positioning moves.

Schedule IDs identify the identical orders in each off and on pair, and each arm starts with the same fleet.
Pending orders contribute their elapsed wait at the end of the measurement window.
The `on` redistribution policy is guarded positioning, as in the server, with two differences.
The command gives the policy no configured rate, so the rate gate reads the mean rate of the accepted orders.
At the first skipped arrival, the command turns the policy off for the rest of the arm.
See [qualification results](studies/qualification.md#guarded-positioning-in-the-london-sweep).

The command uses its own policy flags, and it ignores `redistribution`, `pickupReassignment`, the other experimental settings, and `generatedOrderConsent` in the input project.
With no policy flags, it runs redistribution and pickup reassignment on, as the presets do.
It keeps `faults` and `emergencies`.
An arm of a project with a rate has both operations on and runs the samplers, from the seed of the arm, so arms with one seed get one event schedule.
An interrupted order is terminal and it is not served, so it does not count as remaining or as completed.
To measure the delay that incidents cause, run one project with a rate and one with none, on one seed list.

### Demand patterns

`-pattern` selects one pattern, and its default is `hotspot`.

| Pattern | Orders |
| --- | --- |
| `balanced` | Between all passenger stations. |
| `destination` | From the other passenger stations to the focus station. |
| `hotspot` | Favor the focus station as an origin, with the weight 6 against 2 for each other station. Positioning reads the same weights. |
| `bursty-hotspot` | As `hotspot`, in bursts of `-burst-size` orders at the same simulated time. |
| `hub-burst` | From the focus station, in bursts of `-burst-size` orders. |
| `profile` | The origin-destination bands of a project demand profile. |
| `profile-daily` | The project profile with its authored clock and rates. See [daily demand](daily-demand.md). |
| `rail-arrivals` | The fixed arrival plan. See [scheduled rail arrivals](rail-arrivals.md). |
| `rail-services` | The arrival and departure plans. See [scheduled train connections](rail-connections.md). |

The focus station is the `-focus` station, then the `demand.destination` of the project, then a station with the ID `market`, then the last passenger station.

### Flags

| Flag | Effect |
| --- | --- |
| `-project scenario.json` | Test another network. |
| `-pattern`, `-patterns all` | The demand pattern, or a comma list or all of them. |
| `-seed`, `-seeds 1,2,3` | The seed, default 1, or a list of seeds. |
| `-loads 30s,45s,60s` | The intervals between orders to run. |
| `-request-every` | The interval between orders without `-loads`. Default 45s. |
| `-duration` | The length of the measurement window. Default 30m. |
| `-arrivals-for` | Stop new orders before the measurement ends. |
| `-stop-when-drained` | Stop an arm after all accepted orders complete. |
| `-adaptive-limit` | With `-stop-when-drained`, find capacity limits with fewer arms. |
| `-past-limit` | The number of rates that an adaptive group runs after its limit. Default 1. |
| `-bands all` | With `-pattern profile`, the bands to run. |
| `-daily-start-minute 420` | Start a daily comparison at 07:00 simulated time. |
| `-focus` | The station of the focus metrics in the report, and the station that the focused patterns favor. |
| `-burst-size` | Group burst-pattern orders at the same simulated time. Default 3. |
| `-queue-limit` | Change the limit of 200 pending orders. At the limit, the comparison skips new arrivals. |
| `-workers` | Run independent arms concurrently. Reports keep their deterministic order. |
| `-format json` or `-format csv` | Machine-readable results. The default is a table. |
| `-output` | Write the report to a file. |
| `-sharing-limits 1,4` | Compare shared ride party limits. |
| `-sharing-modes drop-offs,destination` | Compare the shared ride modes. Default `drop-offs`. A limit of 1 runs one time and shows the destination mode, because no party joins a pod. |
| `-sharing-max-stops` | The stop limit of the drop-offs mode, from 1 to 7. Default 3. |
| `-sharing-consent shared` | Give every offered party the consent `private` (default) or `shared`. The command ignores `generatedOrderConsent` in the project. See [sharing consent](compare-sharing-consent.md). |
| `-onboard-pickups off,on` | Compare occupied pickup policies. `on` needs a party limit above 1 and the drop-offs mode. |
| `-redistribution-policies off,on` | Select the positioning policies. The default is `on`, which is guarded positioning. |
| `-wait-rules current,strict,none` | Compare the [wait rules](model.md#wait-rules) of a finishing pod. |
| `-platoon-policies off,virtual` | The experimental platoon comparison. `virtual` lets queued pods follow the pod ahead at a short gap. |
| `-pickup-reassignment off,on` | Compare independent pickup reassignment settings. The default is on. A table or CSV column appears only when the flag is given. |
| `-energy-file` | Add energy estimates from an authored energy model file. See [energy model design](design/energy-model-design.md). |
| `-fleet-classes plain,express` | Compare the project with an Express form of it. See [Express against plain](#express-against-plain). |

With `-adaptive-limit`, arms that differ only in offered rate and seed form a group.
Each group runs its rates from the lowest offered rate, with all seeds of a rate together.
After the first rate at which a seed does not drain, the group runs `-past-limit` more rates and skips the rest.
The report omits the skipped arms, and the other rows are identical to the rows of a full run.

When `GOGC` is not set, the command sets the Go GC percent to 400.
An earlier LondonCentral measurement used about 14% less CPU and about 120 MB more memory than at the default of 100, and the September 29 review kept 400 after testing both London presets.
CPU and memory costs depend on the workload, and these measurements do not set the server GC policy.
The reports do not change, and you can set `GOGC` to use another value.

### Pickup reassignment

Pickup reassignment is on in the presets and the example project, and off in a project file that omits the member.
The editor has a control for it.
**Pause and apply** activates the draft setting in the server, and the compare command uses only `-pickup-reassignment`.
Without the flag, pickup reassignment is on, and JSON output includes its policy value and statistics.
The flag adds a policy column to table and CSV output.
JSON output records controller work, swaps, transfers, and predicted seconds saved.
These predictions exclude future traffic, so they do not establish a realized service improvement.

### Express against plain

`-fleet-classes` selects the fleets to run, in list order, and each fleet runs on each schedule.
Without the flag, the arms and the report format stay unchanged.

| Name | Arm |
| --- | --- |
| `plain` | The project as the file gives it. The arm ignores an `orderContract` marker, as a run with no flag does. |
| `express` | The same project with all classes on each lane, station, and berth, an Express pod in each placement, and the marker `express-v1`. |

The command makes the `express` form in memory and checks it with the project validation.
A project that fails this check stops the command with the validation error.
`compact-station` fails on its Compact bays, and `london-central` and `london-full` fail on their depots.

| Refused combination | Reason |
| --- | --- |
| `express` with no `-project` | The built-in network is not a project, so the validation cannot run. |
| `express` with `-energy-file` | An energy file cannot hold an Express profile. |
| `express` with `-platoon-policies virtual` | An Express pod does not link, so the arm would repeat the `off` arm. |
| `plain` with a project that has an Express pod | A plain arm cannot start the pod. |

The Express arm of a pair has `served_percent_of_plain`: 100 times its served orders divided by the served orders of the plain arm.
A pair has the same schedule ID and differs only in the fleet.
The value is absent when the plain arm did not run, as after `-adaptive-limit` skips it, and when the plain arm served no order.
The table shows `-` in these cases.

The percent is not a capacity measure.
Each order has one passenger, so an arm does not use the standee places, and the difference also includes the effect of the large-class tails on each lane.
An arm does not run the separation oracle, and `TestExpressArmSeparation` runs it on `rail-hub`.
The command does not apply `expressServices`.
See [Express against plain](studies/express-standees.md#express-against-plain) for the recorded results.

### Express London screen

`go run ./cmd/compare screen` runs the screen of the [Express London adoption screen proposal](contracts/express-london-adoption-screen-proposal.md).
It ignores the other flags of the command.
It runs `LF` (`london-full`), `D0` (`london-full-express` without its Express pods, its Express services, and its order contract), and `D` (`london-full-express`) on the same offered events.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-seeds` | `1,2,3` | The seeds. |
| `-bands` | `busy,quiet` | The LondonFull bands. `busy` and `quiet` select the bands with the most and the fewest observed journeys a minute. |
| `-configs` | `LF,D0,D` | The configurations. |
| `-arrivals` | `2h` | The simulated time with offered events. |
| `-clear` | `2h` | The longest simulated time after the arrivals. A run stops when every accepted order is terminal. |
| `-background-per-minute` | 0 | The background requests a minute. 0 uses the rate of the LondonFull preset. |
| `-queue-limit` | 200 | The live queue limit. It admits or refuses a whole event. |
| `-workers` | 3 | The arms that run at the same time. |
| `-check-every` | 1 | The simulated seconds between two checks of the safety oracle, the state contract, and the order accounting. |
| `-output` | none | The file of the JSON report. |

A seed gives the background requests of the band, as the `profile` pattern does, and a separate hub stream.
The hub stream offers a shared party of 1 to 20 passengers between Waterloo and Bank each 15 s on average, three in four from Waterloo.
In `D`, a hub party is one Express order.
In an arm without Express, it splits before admission into orders of at most the largest new party of a pod in the fleet, 4 for Compact.
The wait of an event ends when its last order boards, and its journey when its last order finishes.

The command prints a line for each arm and a line for each gate of each pair against `LF`.
Only the pairs of `D` decide the screen.
The command exits with status 3 when a deciding gate fails.

### Report columns

Some columns show only when you ask for what they report.

| Column or field | Present when |
| --- | --- |
| `wait_rule` | You give `-wait-rules`. |
| `platoon_policy` | You give `-platoon-policies`. |
| `sharing_mode` | Always. It gives the sharing mode of each arm. |
| `pickup_reassignment`, `pickup_reassignment_stats` | Always in JSON. The table and CSV add a policy column only when you give `-pickup-reassignment`. |
| `fleet_class`, `served_percent_of_plain` | You give `-fleet-classes`. The table names them `FLEET` and `SERVED % PLAIN`. |
| `interrupted`, `fault_started`, `fault_skipped`, `emergency_started`, `emergency_skipped` | The project has a fault rate or an emergency rate above 0. |

The JSON report has `schema_version` 16.
It is 17 with an `onboard_pickups` column, 18 with energy estimates, and 19 with `fleet_class`.

#### Waits and journeys

These columns give the waits and journeys of the passengers.
The command reads a separate record for each party from the simulation.
Each party that joins a shared ride has its own wait and journey, its boarding time is the time of the join, and its journey ends when the pod completes unloading at its stop.

| Column | Definition |
| --- | --- |
| `wait_average_seconds` | The mean wait from order to boarding. The set has each party that boarded, and each order that is pending at the end of the arm with its elapsed wait. |
| `wait_maximum_seconds` | The longest wait in the wait set. |
| `wait_p95_seconds` | The 95th percentile of the wait set. |
| `journey_average_seconds` | The mean journey time from order to alight. The set has each party that alighted at its destination before the end of the arm. |
| `journey_p95_seconds` | The 95th percentile of the journey set. |
| `journey_maximum_seconds` | The longest journey time in the journey set. |

A party alights when its pod completes unloading at the destination berth, and at that tick, `served` counts the party.
Each percentile is the nearest-rank value: sort the n values, and use the value at position ceil(0.95 n).
A column with no values is 0.
The journey set does not have the parties that are pending or aboard at the end of the arm, so for an arm that does not drain, the journey columns do not include the longest journeys.

#### Distances

These columns give the distances of the parties that alighted.

| Column | Definition |
| --- | --- |
| `occupancy` | The rider distance of the parties that alighted, divided by the sum of the longest rider distance in each sharing group. |
| `rider_distance_meters` | The sum of the distances that the parties that alighted rode, from the berth where they boarded to the berth where they alighted. |
| `direct_distance_meters` | The sum of the free-flow distances of the same parties between the same two berths. |
| `detour_ratio_mean` | The mean, over the same parties, of the ratio of the rider distance to the free-flow distance. |
| `detour_ratio_max` | The largest ratio for one party. |
| `intermediate_stops` | The number of stops where parties alighted and the pod continued with other parties. |

A sharing group is the party that boarded the pod first and the parties that joined it.
The occupancy is the rider distance of the completed parties divided by the sum of the longest completed rider distance in each sharing group.
Without sharing, the occupancy is 1.
It equals the mean number of parties aboard, weighted by distance, when each group boards at one berth.
With occupied pickups at different berths, it is an approximation.
The value is 0 when no party alighted.
It does not include the parts of pod journeys after the last party that alighted before the end of the arm.

The free-flow distance is the length of the shortest route from the berth where the party boarded to the entry of its destination station, and then along the station path to the berth where it alighted.
A detour ratio of 1 means that the pod took this route, and a route around a full station or a stop for another party makes the ratio larger than 1.

#### Stopped pods

These columns give the time that pods spend stopped, in pod-seconds.
A pod is stopped when it has a wait reason and its speed is less than 0.1 m/s, and a pod at a berth that waits for track admission also has a wait reason.
The command examines the pods at each whole simulated second, and each stopped pod adds 1 second.

| Column | Definition |
| --- | --- |
| `stopped_pod_seconds` | The time of all stopped pods, for each wait reason. |
| `junction_wait_seconds` | The time of the stopped pods with the wait reason "Junction traffic". |
| `track_wait_seconds` | The time of the stopped pods with the wait reason "Pod ahead". |

The other stopped pods wait for a berth or for parking, brake or stop for a fault, wait because of an incident, or have no forward route, so `stopped_pod_seconds` is at least the sum of the other two columns.
The `peak_stopped_vehicles` column counts only the pods with a speed of less than 0.01 m/s, so it can count fewer pods.

#### Busiest node

These columns give the busiest node.
A pod passes a node when it enters a lane that starts at the node, so a pod that leaves a berth passes the berth node, and a pod that stops at the end of its route does not pass that node.
The simulation records each pass at the tick at which the pod enters the lane, and the command reads these records.

| Column | Definition |
| --- | --- |
| `peak_node_throughput_per_minute` | The most passes of one node in 60 seconds. For each pass at time t, the window holds the passes of the same node after t - 60 s and at or before t. |
| `peak_node` | The ID of the node with that count. When two nodes have the same count, it is the lower node ID in byte order. It is empty when no pod passes a node. |

#### Platoon time

The `platoon_time_percent` column gives the time that pods travel in a platoon.
The command examines the pods at each whole simulated second.
Each traveling pod adds 1 second to the travel time, and each traveling pod that has a pod ahead or a pod behind in its platoon also adds 1 second to the platoon time.
The value is the platoon time divided by the travel time, as a percentage.
It is 0 when no pod travels, and always 0 without the `virtual` platoon policy.

## Generated scenarios

The scenario command writes a repeatable project file, which you can give to the server or to the compare command:

```sh
mise run scenario -- -preset scale100 -output /tmp/podsim-scale100.json
mise run serve -- -project /tmp/podsim-scale100.json
```

Generated files hold raw server settings.
The editor can export the loaded scenario with optional local background data.
See [qualification results](studies/qualification.md) for safety checks, performance measurements, and redistribution limits.

### Presets

| Preset | Network | Pods |
| --- | --- | --- |
| `small` | A ring with 16 passenger berths and 4 parking berths. | 12 |
| `busy` | A ring with 35 passenger berths and 10 parking berths. | 32 |
| `parking-constrained` | A ring with 20 passenger berths and 1 parking berth. | 20 |
| `rail-hub` | Six passenger stations with six berths each, and 12 parking berths with 12 initial reserve pods. | 30 |
| `rail-hub-incidents` | `rail-hub`, with 10 faults and 10 emergencies an hour. Each fault lasts 120 s, the debris share is 0.25, and debris is 2 m long. Its project name is the name of `rail-hub`. | 30 |
| `scale100` | A connected grid of 19 passenger stations and one parking station, with 138 berths. | 100 |
| `london-central` | The [LondonCentral qualification network](london.md): 96 passenger stations, three depots, and 242 berths. | 114 Compact |
| `london-full` | [LondonFull](london-full.md): 309 Tube and DLR passenger sites, three depots, and 770 berths. | 327 Compact |
| `london-full-express` | LondonFull with Waterloo and Bank in two banks each, dedicated Express lanes, Express parking, and an Express service in each direction between the two stations. The capacity flags do not apply. | 327 Compact, 16 Express |
| `independent-banks` | A planar station with two independent gates and a separate parking station. | 3 |
| `compact-station` | A station with four Compact bays, in a loop with an ordinary station of three berths. | 6 Compact |

The `rail-hub` preset supports the recorded finite-arrival station-capacity experiment:

```sh
mise run scenario -- -preset rail-hub -output /tmp/podsim-rail-hub.json
mise run compare -- -project /tmp/podsim-rail-hub.json -pattern hub-burst -duration 30m -arrivals-for 5m -request-every 5s -burst-size 12 -seeds 1,2,3,4,5
```

The `scale100` preset separates incoming and outgoing traffic.
Each station has separate road connections for arrival and departure, 600 meters apart, and parallel arrival and departure lanes serve successive berth rows.
The grid has alternate routes and explicit junctions, and parking extends outside the road grid, which shortens the conflict sections at the parking entrance.
Demand starts off, so configure and start it from the **Demand** panel.

LondonCentral uses TfL station locations and topology, real station names, directed twin lanes, and berths outside the through lanes.
It keeps the central qualification network and sets `platoonLimit` to 4, so the server runs it with virtual platoons.
Its `geo` member is the reference of its projection, latitude 51.5074 and longitude -0.1278.
Its parking stations are [depots](model.md#parking-depots), and seven of its stations have [Compact bays](model.md#compact-bays), which are the Compact stations of LondonFull without Tower Gateway.

LondonFull starts with a configured rate of 10 orders per minute, and demand starts off.
West India Quay is left out for layout clearance, and Canary Wharf DLR and Jubilee stay separate stations.
A short DLR smoke run does not establish sustainable capacity, and the earlier finite-arrival studies used the Tube-only preset.
Its three parking stations are depots, and eight of its stations are Compact stations (see [depots and Compact stations](london-full.md#depots-and-compact-stations), which gives the depot length limit, the capacity rule, and the measurements).
On either London preset, `-parking-lanes 0 -compact-stations none` gives ordinary parking stations and ordinary stations.

The `london-full-express` preset is project D of the [Express hub fixture](studies/express-hub-fixture.md), under the `express-v1` order contract.
Waterloo keeps 3 berths in its first bank and Bank keeps 2, and each has a second bank of 2 berths.
The 16 Express pods start at `express-parking`, a parking station east of North London Parking that only Express pods use and that is not a depot.
Each bank of Waterloo and Bank has Express routes to and from `express-parking`.
Generated demand does not choose an Express service, so the services carry only the trip orders that choose them.
The preset is opt-in, and the [qualification proposal](contracts/express-london-qualification-proposal.md) gives its status.

The `compact-station` preset shows the bays at work.
Its bays are at 25 degrees with a 6 m pitch.
Pods wait at a holding node and go through a shared aisle one at a time to park at rest in a bay.
A pod that leaves backs out of its bay, stops for 1 second at the aisle, and drives forward to a receiving node.
The lanes of the bays and the aisle have a speed limit of 2 m/s.

```sh
mise run scenario -- -preset compact-station -output /tmp/podsim-compact.json
```

### Capacity flags

The capacity flags change the berths, the initial pods, and the berth pitch of a preset.
A flag that is not on the command line keeps the preset value, so the output without flags does not change.
They do not apply to `independent-banks` or `compact-station`.

| Flag | Effect |
| --- | --- |
| `-station-berths N` | Set the berths of each passenger station. |
| `-parking-berths N` | Set the berths of each parking station, or the slots of each lane of a depot. |
| `-parking-lanes N` | Ring and London presets. Make each parking station a [depot](model.md#parking-depots) with 1 to 8 lanes. `-parking-berths` then sets the slots of each lane, and `-berths` sets the slots of each lane of one London parking station. A London depot is at most 200 meters long. The London presets have depots of 2 lanes of 6 slots by default, and `-parking-lanes 0` gives ordinary parking stations with 12 berths. |
| `-berths ID=N,ID=N` | Set the berths of single stations by station ID, such as `station-19` or the ID of a TfL station. |
| `-station-pods N` | London presets only. Set the initial pods of each passenger station. |
| `-parking-pods N` | London presets only. Set the initial pods of each parking station. |
| `-berth-pitch M` | Set the distance in meters between two berths of a station. The minimum is 25. |
| `-compact-stations ID,ID` | London presets only. Give at most 8 passenger stations the four bays of the [compact station](model.md#compact-bays), in place of the default list. `none` gives no Compact stations. With a Compact station, each pod is Compact. `-berths` must not name a Compact station. |

```sh
mise run scenario -- -preset scale100 -berths station-19=8 -berth-pitch 68 -output /tmp/podsim-station19-8.json
mise run scenario -- -preset london-central -station-berths 3 -berths 940GZZLUEMB=2 -parking-lanes 0 -parking-berths 24 -output /tmp/podsim-london-3.json
```

A ring station has a 30-meter pitch, and a `scale100`, `london-central`, or `london-full` station has a 75-meter pitch.
The ring and `scale100` presets put the initial pods round robin on the passenger stations, and a full station gets no more pods.
With changed capacity, the project name ends with "(custom capacity)".

A changed preset gets a layout check.
If two lanes are too near each other, the command writes no project and gives an error that names the station and the lanes.
In a ring or `scale100` network, two lanes without a common node must be at least 12 meters apart on the paths that the pods follow.
For the London layout rules, see [London capacity options](london.md#capacity-options).
An unknown station ID, a berth count out of range, more pods than berths, or a network over the project limits also gives an error.

| Station | Berths |
| --- | --- |
| London station | 1 to 200 |
| Ring or mesh station | 1 to 62, because its entry and exit nodes have a lane for each berth, and a node can have at most 64 lanes |
| Ring depot | At most 200 slots |

The first lane of a ring depot is 12 meters from the through lane, its lanes are 6 meters apart, its slots are 6 meters apart, and the initial parking pods fill the heads of the lanes first.
Its run lanes and throats run at 2 m/s, so that the lane gap audit accepts the 6-meter spacing.
The layout check skips the depot lanes, because the lane gap audit of the server checks them.

### Incident flags

These flags write the [`faults`](operations.md#faults) and [`emergencies`](operations.md#emergencies) members.
A flag that is not on the command line writes nothing, so the output keeps the preset value.

| Flag | Writes |
| --- | --- |
| `-fault-rate` | `faults.perHour` |
| `-debris-share` | `faults.debrisShare`. It needs `-fault-rate`. |
| `-debris-meters` | `faults.debrisMeters`. It needs `-fault-rate`. |
| `-fault-duration` | `faults.duration`, as `fixed:S`, `uniform:MIN:MAX`, or `exponential:MIN:MAX:MEAN`. It needs `-fault-rate`. |
| `-evacuation-seconds` | `faults.evacuationSeconds` |
| `-emergency-rate` | `emergencies.perHour` |

### Summary line

The command writes one summary line to standard error.
The line gives the preset, the passenger and parking berths, the pods, the nodes and lanes with their limits, the output bytes, the SHA-256 of the network, and the number of soft layout conflicts.
A soft conflict does not stop the command.
The London presets can have soft conflicts, for example berths that are nearer to the TfL position of another station.

## Other tools

| Tool | Purpose |
| --- | --- |
| `cmd/projectcheck` | Checks a local project file. See [project check](project-check.md). |
| `cmd/parkride` | Runs finite car itineraries around pod journeys. See [its instructions](../internal/parkride/README.md) and the [offline workflow](offline-workflow.md). |
| `tools/londonfull` | Prepares the LondonFull source data. See [its instructions](../tools/londonfull/README.md). |
