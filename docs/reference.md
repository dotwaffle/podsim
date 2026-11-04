# Reference

This document gives the project file format and the command-line tools.

## Project file

This file contains the `project` object from `/api/project`.
The server writes compact JSON without indentation.
Before the server applies a setting change, it saves the file with an atomic replacement.
If the save fails, the server rejects the change.
A rewind that restores the project of a save point also rewrites this file.

Validation limits each project to 32 MiB in this compact form, also after a demand change.
This limit lets the server read the file at the next start.

The browser export wraps the `project` object as `scenario` and can also contain a background image.
**Import JSON** accepts the browser export and the server file, such as the output of `mise run scenario`.
When the file leaves out an optional setting, such as the party limit, the shared ride mode, or the platoon limit, the import uses the default value.
`-project` does not accept the browser export.

## Project files and validation

Project files save the design and settings, not the exact running state.
Save points are exact, but they are in server memory only.
They end when the server stops, also with `-state`.

An import does not replace the draft when the file is malformed, unsupported, or fails validation.
The Go/WASM editor worker checks the draft and imported projects off the main browser thread.
It retains project branches and reuses topology and demand flow checks until their inputs change.
The worker computes changes for scenario names, demand controls, fleet counts, sharing, platoons, and operating flags.
It also adds, edits, and removes rail events and their weighted stations.
Go owns the undo and redo timeline, including background metadata.
The browser retains immutable drawing copies by Go snapshot ID and owns image bytes and storage pins.
Image imports prepare any required history trim before publication and keep the image table within 128 MiB.
If a worker acknowledgment fails, apply stops and the visible draft remains available for export.
The browser keeps queued input and waits for pending edits before apply, export, undo, or redo.
Validation checks routes between passenger stations, berth routes, pod placement, resource IDs, and the 24-meter minimum lane length.
A lane of a [maneuver group](model.md#compact-bays) has no minimum length, but its length must be more than 0.
A chain lane of a [depot](model.md#parking-depots) needs only its reduced retention.
A route between passenger stations goes from each berth of one station to each berth of the other station.
It can use all lanes.
A berth route goes from the station entry to the berth, or from the berth to the station exit.
It can use a chain of lanes, as in the London stations.
It cannot pass through the entry, exit, or berth node of a station.
A route to a depot slot can pass through the other slots of its depot lane.
Validation also limits the compact JSON form of a project to 32 MiB or less.
An ID has only the characters A-Z, a-z, 0-9, `.`, `+`, and `-`, and a name has no control character.
The topology JSON writes each `<`, `>`, and `&` of a name as 6 bytes.
A topology at the node, lane, and station limits, with the widest IDs and names, has about 9 MB, so the server limit of 32 MiB plus 4 KiB (`topology exceeds supported limit`) is a guard that a valid project does not reach.
The editor sends the project in one command, and it compresses a command of more than 64 KiB with gzip.
The server accepts a command body of 16 MiB or less, and 32 MiB plus 64 KiB of command JSON after decompression.
Before the editor pauses the simulation for an apply, it checks the size of the project and shows the limit.
A project can have at most 600 stations, 12,000 nodes, 20,000 lanes, and 1,000 pods.
Each station can have at most 200 berths.
A project can have at most 8 maneuver groups.
A group can have at most 64 lanes, 64 reverse lanes, and 8 berths.
A depot can have at most 8 lanes.
A node can have at most 64 lanes, counted at the start node and at the end node of each lane.
At each node, the simulator compares each ordered pair of two different lanes at the node when it starts.
The total of these pairs over all nodes can be at most 250,000, for example about 62 nodes with 64 lanes each.
Each coordinate of a node position or a lane control point must be from -100,000 to 100,000 meters.
A project can have an optional `geo` member, the geographic reference of the network.
An optional `map` member selects the live backdrop, for example `{"provider":"osm","opacity":0.45}`.
It requires `geo`.
Omitting `map` keeps the existing plain background.
The `geo` member has `latitude` and `longitude` in degrees, `projection` with the value `equirectangular`, and `radius` with the value 6371000.
The latitude must be from -80 to 80 degrees, and the longitude must be from -180 to 180 degrees.
The reference is the place of world position 0, 0.
A point at latitude `lat` and longitude `lon` is at x = R cos(lat0) (lon - lon0) and y = -R (lat - lat0), with lat0 and lon0 the reference, the angles in radians, and R the radius.
Thus x increases to the east, and y increases to the south.
Simulation physics does not use the reference.
The simulator divides each lane into track cells of about 30 meters, with at least 2 cells in each lane.
All lanes together can have at most 64,000 cells, for example about 1,900 km of lanes.
Two lanes cannot have the same start node, end node, and path.
A demand profile can have at most 400,000 flows.
A demand profile has the members `id`, `name`, `bands`, `stations`, and `flows`.
The `stations` member lists each station that a flow names one time.
Each flow is an array: the index in `stations` of the origin, the index of the destination, and then one weight for each band.
For example, with the stations `["harbor","garden"]` and two bands, the flow `[0,1,3,0.25]` goes from `harbor` to `garden`.
An index is an integer from 0, with no fraction and no exponent.
A project is not valid when its station list has a station two times or a station that no flow names.
The server writes the stations in the order of their first use in the flows.
The editor checks do not include the limits of the lane pairs, the coordinates, and the track cells.
The server checks these limits when you apply the project.

## Command-line tools

### Demand and policy comparisons

Run the same seeded demand schedule with redistribution off and on.
This example runs the London preset at 3 requests per minute, where the policy is active:

```sh
mise run scenario -- -preset london-central -output /tmp/podsim-london.json
mise run compare -- -project /tmp/podsim-london.json -pattern profile -bands am-peak -focus 940GZZLUEUS -loads 20s -duration 30m
```

The report includes:

- Demand throughput, backlog, drain time, fleet use, and pickup wait.
- Journey time from request to alight, and the 95th percentile wait and journey time.
- Completed and remaining journeys, and skipped requests.
- Peak queues and berth use at the focus station.
- Passenger and empty travel, loaded-distance percentage, and positioning moves.

The default comparison uses a Market-heavy pickup forecast and identical initial fleets.
Schedule IDs identify the identical requests used for each off/on pair.
The `on` policy is guarded positioning, as in the server.
The compare command gives the policy no configured rate, so the gate reads the mean rate of the accepted requests.
At the first skipped arrival, the compare command turns the policy off for the rest of the arm.
See [qualification results](studies/qualification.md#guarded-positioning-in-the-london-sweep).
Pending requests contribute their elapsed wait at the end of the measurement window.
The synthetic patterns are balanced, destination, hotspot, bursty-hotspot, and hub-burst.
The default `-pattern` is `hotspot`.

| Flag | Effect |
| --- | --- |
| `-project scenario.json` | Test another network. |
| `-patterns all -seeds 1,2,3 -loads 30s,45s,60s` | Run a paired matrix. |
| `-format json` or `-format csv` | Machine-readable results. |
| `-output` | Write the report to a file. |
| `-pattern profile -bands all` | Run the origin-destination bands of a project demand profile. |
| `-pattern profile-daily` | Repeat the project profile with its authored clock and rates. See [daily demand](daily-demand.md). |
| `-daily-start-minute 420` | Start a daily comparison at 07:00 simulated time. |
| `-focus` | Select the station of the focus metrics in the report, and the station that the destination, hotspot, bursty-hotspot, and hub-burst patterns favor. |
| `-duration` | The length of the measurement window. Default 30m. |
| `-request-every` | The interval between requests without `-loads`. Default 45s. |
| `-seed` | The seed without `-seeds`. Default 1. |
| `-arrivals-for` | Stop new requests before the measurement ends. |
| `-stop-when-drained` | Stop an arm after all accepted requests complete. |
| `-adaptive-limit` | With `-stop-when-drained`, find capacity limits with fewer arms. |
| `-past-limit` | The number of rates that an adaptive group runs after its limit. Default 1. |
| `-workers` | Run independent arms concurrently. Reports keep their deterministic order. |
| `-burst-size` | Group burst-pattern requests at the same simulated time. |
| `-sharing-limits 1,4` | Compare shared ride party limits. |
| `-sharing-modes drop-offs,destination` | Compare the shared ride modes. Default `drop-offs`. A limit of 1 runs one time and shows the destination mode, because no party joins a pod. |
| `-sharing-max-stops` | The stop limit of the drop-offs mode, from 1 to 7. Default 3. |
| `-sharing-joins unassigned,reassign-existing` | Compare the shared ride join policies. See [parties](model.md#parties). A limit of 1 shows the given policy, but no party joins a pod. |
| `-routing-policies free-flow,congestion,queue,predictive` | Compare the experimental routing policies. See [routing](model.md#time-geometry-and-routing). |
| `-redistribution-policies off,on` | Select the positioning policies. `on` is guarded positioning. |
| `-wait-rules current,strict,none` | Compare the finishing-pod wait rules from the dispatch section. |
| `-platoon-policies off,virtual` | The experimental platoon A/B. `virtual` lets queued pods follow the pod ahead at a short gap. |
| `-pickup-reassignment off,on` | Compare independent pickup reassignment settings. The default is off. |
| `-queue-limit` | Change the limit of 200 pending requests. At the limit, the comparison skips new arrivals. |
| `-sharing-consent shared` | Give every offered party the consent `private` (default) or `shared`. See [sharing consent](compare-sharing-consent.md). |
| `-onboard-pickups off,on` | Compare occupied pickup policies. `on` needs a party limit above 1 and the drop-offs mode. |
| `-energy-file` | Add energy estimates from an authored energy model file. See [energy model design](design/energy-model-design.md). |
| `-rail-forecast` | Position empty pods for known rail offers. See [scheduled train connections](rail-connections.md). |

The report has a `wait_rule` column or JSON field only when you give `-wait-rules`.
The report has a `platoon_policy` column or JSON field only when you give `-platoon-policies`.
The report has a `sharing_join` column or JSON field only when you give `-sharing-joins`.
Without the option, each arm uses the `unassigned` policy.
The CSV report has the seat screen columns only when a `-sharing-limits` value is above 1.
The JSON report always has them.

The editor has an experimental control for pickup reassignment.
Pause and apply activates the draft settings.
The comparison command uses its explicit policy flags, not the experimental settings in the input project.
Without `-pickup-reassignment`, the policy stays off and the existing report format stays unchanged.
With it, table and CSV output include its policy column, and JSON output includes its off/on value.
With `-pickup-reassignment`, JSON also records controller work, swaps, transfers, and predicted seconds saved.
These predictions exclude future traffic; they do not establish realized service improvement.

The `sharing_mode` column gives the sharing mode of each arm.

With `-adaptive-limit`, arms that differ only in offered rate and seed form a group.
Each group runs its rates from the lowest offered rate, with all seeds of a rate together.
After the first rate at which a seed does not drain, the group runs `-past-limit` more rates and skips the rest.
The report omits the skipped arms.
The other rows are identical to the rows of a full run.

When `GOGC` is not set, the compare command sets the Go GC percent to 400.
An earlier LondonCentral measurement used about 14% less CPU and about 120 MB more memory than at the default of 100.
The September 29 review retained 400 after testing both London presets.
CPU and memory costs depend on the workload, and these measurements do not set the server GC policy.
The reports do not change.
Set `GOGC` to use another value.

#### Report columns

The JSON report has `schema_version` 12.
It is 13 with a pickup reassignment column, 14 with an `onboard_pickups` column, and 15 with energy estimates.
These columns give the waits and journeys of the passengers.

| Column | Definition |
| --- | --- |
| `wait_average_seconds` | The mean wait from request to boarding. The set has each party that boarded, and each request that is pending at the end of the arm with its elapsed wait. |
| `wait_maximum_seconds` | The longest wait in the wait set. |
| `wait_p95_seconds` | The 95th percentile of the wait set. |
| `journey_average_seconds` | The mean journey time from request to alight. The set has each party that alighted at its destination before the end of the arm. |
| `journey_p95_seconds` | The 95th percentile of the journey set. |
| `journey_maximum_seconds` | The longest journey time in the journey set. |

A party alights when its pod completes unloading at the destination berth.
At that tick, `served` counts the party.
Each percentile is the nearest-rank value: sort the n values, and use the value at position ceil(0.95 n).
A column with no values is 0.
The journey set does not have the parties that are pending or aboard at the end of the arm.
Thus, for an arm that does not drain, the journey columns do not include the longest journeys.

The compare command reads a separate record for each party from the simulation.
Each party that joins a shared ride has its own wait and journey.
Its boarding time is the time of the join, and its journey ends when the pod completes unloading at its stop.

These columns give the distances of the parties that alighted.

| Column | Definition |
| --- | --- |
| `occupancy` | The rider distance of the parties that alighted, divided by the occupied distance of their pod journeys. |
| `rider_distance_meters` | The sum of the distances that the parties that alighted rode, from the berth where they boarded to the berth where they alighted. |
| `direct_distance_meters` | The sum of the free-flow distances of the same parties between the same two berths. |
| `detour_ratio_mean` | The mean, over the same parties, of the ratio of the rider distance to the free-flow distance. |
| `detour_ratio_max` | The largest ratio for one party. |
| `intermediate_stops` | The number of stops where parties alighted and the pod continued with other parties. |

A pod journey starts when the first party boards the pod.
All parties of a pod journey board at the same berth.
The occupied distance of a pod journey is the longest rider distance of its parties.
Without sharing, the occupancy is 1.
With sharing, it is the mean number of parties aboard, weighted by distance.
The value is 0 when no party alighted.
The occupancy does not include the parts of pod journeys after the last party that alighted before the end of the arm.

The free-flow distance is the length of the shortest route from the berth where the party boarded to the entry of its destination station, and then along the station path to the berth where it alighted.
The routing policy does not change it.
A detour ratio of 1 means that the pod took this route.
A route around a full station, a route of the routing policy, or a stop for another party makes the ratio larger than 1.

These columns give the time that pods spend stopped, in pod-seconds.
A pod is stopped when it has a wait reason and its speed is less than 0.1 m/s.
A pod at a berth that waits for track admission also has a wait reason.
The compare command examines the pods at each whole simulated second, and each stopped pod adds 1 second.

| Column | Definition |
| --- | --- |
| `stopped_pod_seconds` | The time of all stopped pods, for each wait reason. |
| `junction_wait_seconds` | The time of the stopped pods with the wait reason "Junction traffic". |
| `track_wait_seconds` | The time of the stopped pods with the wait reason "Pod ahead". |

The other stopped pods wait for a berth or for parking, so `stopped_pod_seconds` is at least the sum of the other two columns.
The `peak_stopped_vehicles` column counts only the pods with a speed of less than 0.01 m/s, so it can count fewer pods.

These columns give the busiest node.
A pod passes a node when it enters a lane that starts at the node.
Thus a pod that leaves a berth passes the berth node, and a pod that stops at the end of its route does not pass that node.
The simulation records each pass at the tick at which the pod enters the lane, and the compare command reads these records.

| Column | Definition |
| --- | --- |
| `peak_node_throughput_per_minute` | The most passes of one node in 60 seconds. For each pass at time t, the window holds the passes of the same node after t - 60 s and at or before t. |
| `peak_node` | The ID of the node with that count. When two nodes have the same count, it is the lower node ID in byte order. It is empty when no pod passes a node. |

The `platoon_time_percent` column gives the time that pods travel in a platoon.
The compare command examines the pods at each whole simulated second.
Each traveling pod adds 1 second to the travel time.
Each traveling pod that has a pod ahead or a pod behind in its platoon also adds 1 second to the platoon time.
The value is the platoon time divided by the travel time, as a percentage.
It is 0 when no pod travels, and always 0 without the `virtual` platoon policy.

The seat screen columns follow `shared_parties`.
They show whether the parties that a boarding pod could take are more than its seats.
A party can take a pod when the pod could add it with the rules of the sharing mode: the destination, the stop limit, and the detour cap.
The join census columns count the parties that have a pod on its way to their origin.
A party counts when a boarding pod at its origin could take it with a free seat at one or more dispatch passes.
The census does not check that a new first stop has a route, so it can count a party that a pod could not take.

| Column | Definition |
| --- | --- |
| `full_pod_refusals` | The parties that found a full boarding pod at their origin that could take them, and that joined no pod. Each party counts one time. |
| `full_departures` | The boarding pods that departed with as many parties as the limit. |
| `departure_backlog` | For each departure of a boarding pod, the waiting parties at its origin that the pod could take with a free seat. The column gives the sum over all departures. It also counts a party that has another pod on its way. |
| `departures_demand_over_four` | The departures with more than four parties aboard plus backlog. |
| `departures_over_four_aboard` | The departures with more than four parties aboard. It is 0 at a limit of 4 or less. |
| `join_eligible_assigned` | The parties with a pod on its way that a boarding pod at their origin could take. Each party counts one time. |
| `join_eligible_existing_stop` | The parties of `join_eligible_assigned` that a boarding pod could take with no new stop, because the pod already stops at their destination. In destination mode, it is equal to `join_eligible_assigned`. |
| `join_eligible_added_stop_only` | `join_eligible_assigned` minus `join_eligible_existing_stop`. A boarding pod could take each of these parties only with a new stop. |
| `reassigned_parties` | The parties that joined a boarding pod while they had a pod on its way. Dispatch released that pod. It is 0 with the `unassigned` policy. The census also counts each of these parties in `join_eligible_existing_stop`. |

The columns are 0 when the party limit is 1.

### Generated scenarios

Create a repeatable server scenario:

```sh
mise run scenario -- -preset scale100 -output /tmp/podsim-scale100.json
mise run serve -- -project /tmp/podsim-scale100.json
```

Presets are `small`, `busy`, `parking-constrained`, `rail-hub`, `scale100`, `london-central`, `london-full`, `independent-banks`, and `compact-station`.
The `independent-banks` preset is a planar station with two independent gates and a separate Parking station.
The capacity flags do not apply to `independent-banks` or `compact-station`.
LondonCentral preserves the central qualification network.
[LondonFull](london-full.md) covers 309 Tube and DLR sites with 2024 endpoint demand.
Generated files contain raw server settings.
The editor can export the loaded scenario with optional local background data.
See [qualification results](studies/qualification.md) for safety checks, performance measurements, and redistribution limits.

The capacity flags change the berths, the initial pods, and the berth pitch of a preset.
A flag that is not on the command line keeps the preset value, so the output without flags does not change.

| Flag | Effect |
| --- | --- |
| `-station-berths N` | Set the berths of each passenger station. |
| `-parking-berths N` | Set the berths of each Parking station, or the slots of each lane of a depot. |
| `-parking-lanes N` | Ring and London presets. Make each Parking station a [depot](model.md#parking-depots) with 1 to 8 lanes. `-parking-berths` then sets the slots of each lane, and `-berths` sets the slots of each lane of one London facility. A London depot is at most 200 meters long. The London presets have depots of 2 lanes of 6 slots by default, and `-parking-lanes 0` gives ordinary Parking facilities with 12 berths. |
| `-berths ID=N,ID=N` | Set the berths of single stations by station ID, such as `station-19` or the ID of a TfL station. |
| `-station-pods N` | London presets only. Set the initial pods of each passenger station. |
| `-parking-pods N` | London presets only. Set the initial pods of each Parking facility. |
| `-berth-pitch M` | Set the distance in meters between two berths of a station. The minimum is 25. |
| `-compact-stations ID,ID` | London presets only. Give at most 8 passenger stations the four bays of the [compact station](model.md#compact-bays), in place of the default list. `none` gives no Compact stations. With a Compact station, each pod is Compact. `-berths` must not name a Compact station. |

A ring station has a 30-meter pitch, and a `scale100`, `london-central`, or `london-full` station has a 75-meter pitch.
The ring and `scale100` presets put the initial pods round robin on the passenger stations, and a full station gets no more pods.
With changed capacity, the project name ends with "(custom capacity)".
A layout check then examines the generated network.
If two lanes are too near each other, the command writes no project and gives an error that names the station and the lanes.
In a ring or `scale100` network, two lanes without a common node must be at least 12 meters apart on the paths that the pods follow.
For the London layout rules, see [London capacity options](london.md#capacity-options).
An unknown station ID, a berth count out of range, more pods than berths, or a network over the project limits also gives an error.
A London station can have 1 to 200 berths.
A ring or mesh station can have 1 to 62 berths.
The entry and exit nodes of a ring station have a lane for each berth, and a node can have at most 64 lanes.
A ring depot can have at most 200 slots.
Its first lane is 12 meters from the through lane, its lanes are 6 meters apart, its slots are 6 meters apart, and the initial Parking pods fill the heads of the lanes first.
Its run lanes and throats run at 2 m/s, so that the lane gap audit accepts the 6-meter spacing.
The layout check skips the depot lanes, because the lane gap audit of the server checks them.

The command writes one summary line to standard error.
The line gives the preset, the passenger and Parking berths, the pods, the nodes and lanes with their limits, the output bytes, the SHA-256 of the network, and the number of soft layout conflicts.
A soft conflict does not stop the command.
The London presets can have soft conflicts, for example berths that are nearer to the TfL position of another station.

```sh
mise run scenario -- -preset scale100 -berths station-19=8 -berth-pitch 68 -output /tmp/podsim-station19-8.json
mise run scenario -- -preset london-central -station-berths 3 -berths 940GZZLUEMB=2 -parking-lanes 0 -parking-berths 24 -output /tmp/podsim-london-3.json
```

#### rail-hub

The rail-hub preset has six passenger stations, 30 pods, six berths per passenger station, and 12 parking berths with 12 initial reserve pods.
It supports the recorded finite-arrival station-capacity experiment:

```sh
mise run scenario -- -preset rail-hub -output /tmp/podsim-rail-hub.json
mise run compare -- -project /tmp/podsim-rail-hub.json -pattern hub-burst -duration 30m -arrivals-for 5m -request-every 5s -burst-size 12 -seeds 1,2,3,4,5
```

#### scale100

The scale preset uses a connected grid with alternate routes and explicit junctions.
It has 19 passenger stations, one parking station, 138 berths, and 100 pods.
Each station has separate road connections for arrival and departure, 600 meters apart.
Parallel arrival and departure lanes serve successive berth rows.
Parking extends outside the road grid.
This geometry separates incoming and outgoing traffic and shortens the parking entrance conflict sections.
Demand starts disabled.
Configure and start it from the Demand panel.

#### london-central

The [LondonCentral qualification network](london.md) uses TfL station locations and topology, real station names, directed twin guideways, off-line berths, and three Parking facilities.
It sets `platoonLimit` to 4, so the server runs it with virtual platoons.
Its `geo` member is the reference of its projection, latitude 51.5074 and longitude -0.1278.
Station lanes identify approach, entry, berth access, through, departure, and exit maneuvers.
The pod inspector shows the current maneuver in **Station phase** and the station name on the line below it.
Projects without this optional lane metadata still load.
The simulator infers through, berth access, and departure roles from the station paths.
Its Parking facilities are [depots](model.md#parking-depots), and seven of its stations have [Compact bays](model.md#compact-bays), as in [london-full](#london-full).
Its Compact stations are those of LondonFull without Tower Gateway.
`-parking-lanes 0 -compact-stations none` gives ordinary Parking facilities and ordinary stations.

#### london-full

[LondonFull](london-full.md) includes 309 Tube and DLR passenger sites and three Parking facilities with 2024 endpoint demand.
It starts with 327 Compact pods, 770 berths, and a configured rate of 10 requests per minute.
West India Quay is omitted for layout clearance.
Canary Wharf DLR and Jubilee remain separate stations.
Demand starts disabled.
A short DLR smoke run does not establish sustainable capacity.
The earlier finite-arrival studies used the Tube-only preset.
Its three Parking facilities are depots, and eight of its stations are Compact stations (see [depots and Compact stations](london-full.md#depots-and-compact-stations)).
The flags `-parking-lanes` and `-compact-stations` change these layouts, and `-parking-lanes 0 -compact-stations none` turns them off.
That section gives the depot length limit, the capacity rule for a Compact station, and the measurements.

#### compact-station

The compact-station preset has a station with four [compact bays](model.md#compact-bays) at 25 degrees and a 6 m pitch.
Pods wait at a holding node and go through a shared aisle one at a time to park at rest in a bay.
A pod that leaves backs out of its bay, stops for 1 second at the aisle, and drives forward to a receiving node.
The lanes of the bays and the aisle have a speed limit of 2 m/s.
A loop connects the station to an ordinary station with three berths.
The preset has 6 Compact pods, and the capacity flags do not apply to it.

```sh
mise run scenario -- -preset compact-station -output /tmp/podsim-compact.json
```
