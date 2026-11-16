# Series berths

Status: parked on 2026-11-16, after the review in section 8.
Proposed on 2026-11-16.
Nothing in this design is built.
Code references are for `b054fedc`.

This design answers decision D10 of the [shape clearance](shape-clearance.md#6-decisions) design.
For D10, the maintainer asked for a design of berths in series: several berths one behind another on one platform lane, as in real people-mover and PRT stations.
Pods enter the platform in order, stop, and leave in order, or pass a stopped pod on a bypass.

The proposal reuses the depot lane.
A depot lane is already a chain of slots in series, with a reduced retention, a binding in arrival order, and a restore that keeps the order (section 1.5).
The recommended change lets a passenger station be a depot, with passenger rules for boarding, departure, dispatch, and emergencies.
The decisions are in section 5.

The measurements change the premise of the question.
A platform at the depot pitch of 6 m cuts the median path from the diverge to a berth from 195.7 m to about 121 m (inferred, section 2.3).
But a platform lane runs below 3.74 m/s, so a pod needs about 44 to 53 s from the diverge to a platform slot, against 15 to 20 s to a berth row today (inferred).
With a faster fan and faster lanes after the platform, the time is about 24 to 32 s (inferred).
One platform discharges about 2 to 3 pods a minute when each pod leaves through the head (measured on a test fixture, section 2.2).
The arrival rate of a platform was not measured.
So series berths save depth and lane length, but each stop costs more time, and a busy station needs more than one platform.

On 2026-11-16, while this design was written, the maintainer made tight stations the London default ([station scale study](../studies/station-scale.md)).
Tight stations set each part of a berth row station to 25 m, and the median path from the diverge to a berth is about 101 m.
That is shorter than the about 121 m of a platform, and a berth row keeps the 15 to 20 s time of today.
The comparison in this design is against the berth rows before that change.

## 1. Current behavior

### 1.1 Why a berth route cannot pass another berth node

`docs/reference.md:139` to `:141` state the rule.
A berth route goes from the station entry to the berth, or from the berth to the station exit.
It cannot pass through the entry, exit, or berth node of a station.
A route to a depot slot can pass through the other slots of its depot lane.

The code:

- `stationForbidden` (`internal/sim/network.go:716`) puts each entry, exit, bank gate, and berth node of each station in one set (`:724` to `:726`).
- `stationRoutesExist` (`network.go:700`) looks for a route from the entry to each berth and from each berth to the exit with that set as forbidden nodes.
  `validateStationRoutes` (`network.go:686`) refuses a station without these routes with `station %q needs entry, exit, and through lanes` (`:691`).
- `stationPathForClass` (`internal/sim/routes.go:118`) uses the same set for each station path at run time (`:123`).
  The terminal berth choice and the terminal reevaluation use this search (`internal/sim/berth_choice.go:114`).
- `inferStationLaneRoles` (`internal/sim/station_maneuvers.go:24`) gives the lanes of these routes the roles berth access and departure.
- In a bank, `bankPathLanes` (`internal/sim/station_banks.go:269`) refuses a local lane with a gate or a berth node inside the path (`:281`).
- The editor has its own copy of the station route check (`internal/editormodel/checks.go:444`).
- `depotForbidden` (`internal/sim/depot_network.go:129`) removes the slot nodes of one depot lane from the set for a route to a slot of that lane.
  It is the only exception.

The rule came with `f227fe51` (2026-09-21), which let station paths pass branch nodes "without crossing other station boundaries".
A berth node is a station boundary in this sense.
Two reasons support the rule.
First, a pod at a berth owns the berth node (section 1.2), so a path through that node waits for the pod to leave.
Second, a station has berths outside its through lane so that a stopping pod does not block the pods that pass (`docs/model.md:297`).
A path through another berth node gives up that property for the pods on the path.
So the rule keeps one berth from blocking the access to another berth.

The road routes are different.
A route between stations can use all lanes, and `89e1e1d4` makes new routes avoid the berths of other stations when a compatible path exists.

### 1.2 Berths, berth resources, and berth choice

- A berth has two resources: the berth and its node (`berthResources`, `internal/sim/state_physical.go:609`).
- The last cell of the lane into a berth node holds the berth (`newLaneCells`, `internal/sim/traffic.go:162` to `:167`).
  The first cell of a lane holds its From node, and the last cell holds its To node (`:169`, `:172`).
- A pod keeps the From node of a lane until it is the retention past the lane start (`releaseDistance`, `traffic.go:954`).
  It keeps a junction to the end of each cell that holds it (`:950`).
- A grant that a berth resource refuses gives the wait reason `Berth occupied`, and a node or a junction gives `Junction traffic` (`refuseGrant`, `traffic.go:726`).
- A passenger pod routes to the station entry with no berth.
  It chooses the least-assigned reachable berth on its final lane (`assignTerminalBerth`, `berth_choice.go:9`, and `stationRouteByLoad`, `internal/sim/berths.go:66`).
  With platoons on, a small pod can choose earlier (`earlyBerthChoice`, `berth_choice.go:68`).
  It can change to a free alternate berth before it reserves the next branch (`reevaluateTerminalBerth`, `berth_choice.go:87`).
- `berthLoad` (`berths.go:136`) counts the moving pods that go to a berth, the waiting trips for it, and its owners.
- An idle empty pod leaves its berth when it blocks a pod that must stop at that berth or pass through it (`clearBlockedBerths`, `internal/sim/parking.go:19`).
  `entersBerth` (`parking.go:102`) tests whether the rest of the route of the waiting pod holds the berth resource.
- Empty moves, guarded positioning, and the blocker clear claim a remote berth when they start.
  The [berth claim deadlock](../contracts/berth-claim-deadlock-proposal.md) contract found that a remote claim behind unclaimed pods on one access lane makes a circular wait, "because the order on a single access lane is the order of arrival".

A London station at scale 0.5 has berth rows (`addLondonStation`, `internal/scenarios/london.go:959` to `:977`).
Each row has an arrival link, a berth lane in, a berth lane out, and a departure link, all at 14 m/s.
The rows are 37.5 m apart, and `londonStationShape.nodes` (`internal/scenarios/london_heading.go:205`) places them.

### 1.3 Station maneuvers and lane roles

A lane has one station role.
`inferStationLaneRoles` gives each lane the first role that it finds, in berth order, arrival first (`station_maneuvers.go:51` to `:63`, `setStationLaneRole` at `:68`).
`routeStationPhase` (`station_maneuvers.go:106`) reads the phase from the role of the lane, except on a maneuver group lane, where the phase comes from the route.
A lane that is both an arrival path and a departure path thus shows one phase for both movements.
In a bank, the arrival paths and departure paths must have the berth access and departure roles (`bankPathLanes`, `station_banks.go:277`), so one lane cannot serve both.

### 1.4 Retention and slow chains

A qualifying lane needs less than 12 m behind a pod (`newLaneRetention`, `internal/sim/retention.go:113`, conditions at `:104` to `:112`).
The lane must be straight and slower than `reducedClearanceSpeed`, about 3.74 m/s (`retention.go:22`).
Its window runs from the upstream stop reach to 24 m past the lane end (`window`, `retention.go:163`).
Each node of the window needs one incoming and one outgoing lane (`chainLane`, `retention.go:216`).
`R = (5 + 2u*)/cos(T/2) - u*` (`reducedRetention`, `retention.go:228`), where `u*` is the upstream stop distance, at most 3.5 m, and `T` is the total turn of the window.
The follower keeps `compactClearance(v) = 4 + min(8, 1 + 2u)` (`retention.go:31`).

So a branch node anywhere in the window, also up to 24 m past the lane end, stops the lane from qualifying.
`TestDepotChainQualification` (`internal/sim/depot_chain_test.go:59`) shows this for a lead-out that ends at the exit.

### 1.5 Depots, and what a depot chain proves

A depot is a parking station of 1 to 8 queue lanes (`docs/model.md:361` to `:399`).
Each depot lane is a fan from the entry, a straight lead-in to the tail slot, a chain lane from each slot to the slot ahead, a straight lead-out from the head slot, and a throat path to the exit (`depotPath`, `depot_network.go:38`).

- **Pitch and speed.**
  `DepotSpeed(pitch) = sqrt(a(pitch - 5.01))` (`depot_network.go:58`) is the speed at which a chain lane of that pitch qualifies.
  The generator uses a pitch of 6 m (`internal/scenarios/depot_station.go:16`), which gives 1.407 m/s.
- **Retention.**
  A chain lane needs only its retention as its length (`chainMinimum`, `depot_network.go:84`).
  Each cell of a short chain lane holds the slot at its end and both of its nodes (`everyCell`, `depot_network.go:98`), so two pods are never on one chain lane.
- **Routes.**
  A route to a slot can pass the other slots of its lane (`depotForbidden`).
- **Order.**
  A pod that goes to a depot has the entry as its destination.
  It binds a slot when it owns the last cell of the incoming lane, so the depot binds at most one pod in each tick, in arrival order (`bindDepotArrivals`, `internal/sim/depot.go:177`).
  It takes the slot behind the rearmost taken slot of the lane with the fewest pods (`depotChoice`, `depot.go:113`).
  A lane with a faulted or withdrawn pod takes no pod (`depotTarget`, `depot.go:88`).
  `depotGoal` (`depot.go:133`) admits a pod only when the free slots outnumber the unbound pods that go to the entry.
- **Departure.**
  Only the head can leave.
  Dispatch takes the head that parked first (`depotOldestHead`, `depot.go:215`, and `depotGate`, `depot.go:253`).
  An idle pod moves up when the slot ahead is free (`advanceDepots`, `depot.go:289`).
- **Diversion and targets.**
  A pod whose destination is a slot cannot divert (`divertStart`, `internal/sim/diversion.go:114`).
  An operational target, such as an emergency station, cannot be a depot station (`checkOperationalTarget`, `internal/sim/operational.go:181`).
- **Lateral pairs.**
  Two slow lanes of one depot need `W(v) = 3.1 + v²/2` m when a swept-body check clears them (`docs/station-banks.md`, "Depot lanes").
  The generator puts depot lanes 6 m apart (`depot_station.go:17`).
- **Restore.**
  A demoted pod can take only the slot behind the rearmost taken slot, and only when no pod of the lane moves (`depotSlotOpen`, `internal/sim/depot_restore.go:32`).
- **Exclusions.**
  A depot station must be a parking station with no banks.
  Its slots admit no large class.
  A depot lane cannot join a platoon run, and it is not a lateral merge lane (`internal/sim/lateral_merges.go:44`).

The tick-rate contract (section 1.7) ran an oracle on LondonFull with depots at 20, 30, and 60 Hz, and the smallest gap was the 6.000 m pitch, with no violation.
`docs/model.md:399` records that the depots served within four orders of ordinary parking at 60 orders per minute.
So a depot chain proves three things: slots in series at a 6 m pitch are safe under the current rules, a route can pass slots of its own lane, and binding at the entry keeps the order of the lane.

## 2. Measurements

Each value comes from a scratch probe at `b054fedc`, deleted after the run.
Section 2.4 gives the probes, so that they can be built again.
A value marked "inferred" is a calculation, not a simulator run.

### 2.1 London stations

LondonFull at scale 0.5:

| Item | Value |
| --- | --- |
| Passenger stations with berth rows | 301 |
| Berths at these stations | 702 |
| Stations with 2, 3, 4, and 5 berths | 216, 72, 11, 2 |
| Compact stations, 4 bays each | 8 |
| Depots | 3, with 36 slots |
| Diverge to berth, median | 195.7 m |
| Diverge to berth, smallest and largest | 158.2 m, 308.2 m |
| Lane from the diverge to the entry | 63.25 m at each station |
| Initial pods at each passenger station | 1 |

The share of a station is its largest share of trip ends over the six demand bands, as in `docs/london-full.md` (Compact stations).
A trip end is about one visit to a berth.

| Berths | Stations | Median share | Largest share | Trip ends a minute at 15 requests a minute, median and largest | Largest at 60 requests a minute |
| ---: | ---: | ---: | ---: | --- | ---: |
| 2 | 216 | 0.0038 | 0.0554 | 0.06, 0.83 | 3.32 |
| 3 | 72 | 0.0131 | 0.0333 | 0.20, 0.50 | 2.00 |
| 4 | 19 | 0.0399 | 0.0597 | 0.60, 0.90 | 3.58 |
| 5 | 2 | 0.0720 | 0.0720 | 1.08, 1.08 | 4.32 |

The row of 4 berths includes the 8 Compact stations.
The largest share with 2 berths is that of Bank, which the generator keeps at 2 berths.

### 2.2 Depot chains today

The test fixture `depotNetwork` (`internal/sim/depot_fixture_test.go:43`) has one depot lane 12 m from the through lane.
Its fan is a curve of 27.55 m, and its lead-out is 24.01 m.
The generator lead-out is 12.01 m (`depot_station.go:28`), with a run lane of 24.01 m and a throat at 2 m/s.

The probe first gave a passenger station the fixture depot lane as a berth row:

| Network | Result |
| --- | --- |
| Slots of one lane as the berths of a passenger station, with no depot member | `station "parking" needs entry, exit, and through lanes` |
| The same, with the depot member | `depot station "parking" must be a parking station with no banks` |

Retention by pitch, with each lane of the depot lane at `DepotSpeed(pitch)` and the lead-in at its minimum:

| Pitch | Speed | Chain retention | Lead-in minimum |
| ---: | ---: | ---: | ---: |
| 6.0 m | 1.407 m/s | 5.495 m | 11.97 m |
| 7.0 m | 1.995 m/s | 5.995 m | 13.13 m |
| 8.0 m | 2.445 m/s | 6.495 m | 14.29 m |
| 9.5 m | 2.997 m/s | 7.245 m | 16.04 m |
| 11.0 m | 3.461 m/s | 7.995 m | 17.78 m |

Speed of the fan, at a pitch of 6 m:

| Fan speed | Shortest tested lead-in that validates | Minimum in the refusal | Lead-in retention | Refusal of a shorter lead-in |
| ---: | ---: | ---: | ---: | --- |
| 1.407 m/s | 11.99 m | 11.97 m | 5.983 m | |
| 3 m/s | 16.10 m | 16.05 m | 8.024 m | Lane minimum |
| 5 m/s | 19.10 m | 18.96 m | 9.478 m | Lane minimum, or fan and chain less than 12 m apart |
| 8 m/s | 19.10 m | 18.96 m | 9.478 m | The same |
| 14 m/s | 24.01 m | 24 m | Does not qualify | Lane minimum of 24 m |

The chain retention stays 5.495 m in each case.
A fast fan does not change the chain, because the lead-in is longer than the stop reach of the fan.

A lead-out at 5 or 14 m/s fails the lane gap audit: `nonincident paths "parking-out-1" and "parking-chain-1-2" are less than 12 meters apart`.
The audit treats two lanes on one line that share no node as a lateral pair.
So the first 12.01 m after the head slot must be slow, as in the generator and in `TestDepotSplitLeadOut` (`internal/sim/depot_network_test.go:310`).

Timing on the fixture, at a pitch of 6 m:

| Event | Time |
| --- | ---: |
| A move-up of one slot, with the slot ahead free | 5.07 s |
| The head releases the berth and the node of slot 1 | 4.63 s |
| The head releases the junction of slot 1, 24.01 m lead-out | 17.43 s |
| The head releases the junction of slot 1, lead-out split at 12.01 m | 8.93 s |
| 4 pods: the pods at slots 2, 3, and 4 are each one slot ahead | 22.50, 27.57, 32.63 s |

The pod at slot 2 waits with `Junction traffic` until the head leaves the lead-out.
Each cell of the lead-out holds the junction of the slot 1 node, because the through pair at that node has a 12 m extent, and a junction releases at the end of its cell.
So the head-of-lane headway depends on the length and the speed of the lead-out, not on the release of the berth.
The [kinematic model](#23-path-and-time-of-a-series-station) of section 2.3 gives 17.41 s for the 24.01 m case, against 17.43 s measured.

Discharge through the head: 3 pods at slots 1 to 3 start to leave at the same tick, each to another station.
The times are when each pod takes the junction of slot 1.

| Lanes after the head | Pod 1 | Pod 2 | Pod 3 | Departures a minute |
| --- | ---: | ---: | ---: | ---: |
| Fixture: lead-out 24.01 m, throat at 5 m/s | 0 s | 28.00 s | 60.23 s | About 2.0 |
| Lead-out 12.01 m, run 24.01 m and throat at 5 m/s | 0 s | 24.23 s | 52.70 s | About 2.3 |
| Lead-out 12.01 m, run 24.01 m and throat at 14 m/s | 0 s | 18.37 s | 40.97 s | About 2.9 |

In the last case, the head releases the slot 1 junction at 8.93 s, but pod 2 starts at 18.37 s.
At 12 s, pod 2 waits for the junctions of the nodes at the end of the lead-out, at the end of the run lane, and at the exit.
Each of these nodes has one lane in and one lane out, or a merge, and each junction holds the cells next to it.
Thus the grant of pod 2 takes one continuous run of junction cells up to the exit.

Faults and debris on the fixture, with 3 pods at slots 1 to 3:

| Incident | Result |
| --- | --- |
| Pod fault on the pod at slot 2 | Blocked lanes: the chain lanes into and out of slot 2. Blocked berths: slot 2. |
| Debris in the middle of the lead-in, a chain lane, or the lead-out | `invalid debris segment` |

So a fault at slot `k` closes the access to each slot ahead of `k` and the exit of each slot behind `k`.
Debris cannot start on a lane whose footprint holds a berth node (`debrisSegment`, `internal/sim/debris.go:86`), and each lane of a depot lane near a slot holds one.

### 2.3 Path and time of a series station

These values are inferred.
They use the lengths of `depotGeometry` (`depot_station.go:167`) for one depot lane, with the 63.25 m lane from the diverge of section 2.1.
No passenger station of this shape was validated.
A London depot station has its entry 57 to 66 m from the station axis, not 50 m, so the lane from the diverge would be a few meters longer.

| Slots | Entry to slot, head to tail | Slot to exit, head to tail | Length along the station |
| ---: | --- | --- | ---: |
| 2 | 57.56 m, 51.56 m | 63.57 m, 69.57 m | 114.03 m |
| 3 | 63.56 m to 51.56 m | 63.57 m to 75.57 m | 120.03 m |
| 4 | 69.56 m to 51.56 m | 63.57 m to 81.57 m | 126.03 m |
| 5 | 75.56 m to 51.56 m | 63.57 m to 87.57 m | 132.03 m |

With one platform of `N` slots in place of `N` berth rows at each of the 301 stations, the median path from the diverge to a berth is 120.8 m, against 195.7 m.
With a lead-in of 19.1 m (fan at 8 m/s), it is 115.9 m.
The platform is 12 m outside the through lane, so the station reaches about 152 m from its center, against 222.5 m for two berth rows (inferred from `entryDepth` and `depotAcross`).
It is 114 to 132 m long, against 100 m from the entry to the exit today.
For comparison, the [shape clearance](shape-clearance.md#27-stations) design gives 183.2 m for rows at 25 m, and about 97 m for rows with each access lane at 24 m (inferred, not validated).

The times come from a kinematic model with acceleration and braking of 2 m/s².
A pod arrives at the diverge at 14 m/s and stops at the berth.
A departing pod starts at rest and ends at the merge.
For a move-up, the model gives 4.97 s, against 5.07 s measured.
For the release of the junction after a 24.01 m lead-out, it gives 17.41 s, against 17.43 s measured.

| Layout | Arrival, distance and time | Departure, distance and time |
| --- | --- | --- |
| Berth rows today, row 1 | 158.2 m, 14.8 s | 158.2 m, 14.8 s |
| Berth rows today, row 2 | 195.8 m, 17.5 s | 195.8 m, 17.5 s |
| Berth rows today, row 3 | 233.2 m, 20.2 s | 233.2 m, 20.2 s |
| Generator depot speeds, 2 slots, tail and head | 114.8 m 44.3 s, 120.8 m 48.6 s | 132.8 m 46.1 s, 126.8 m 41.8 s |
| Generator depot speeds, 3 slots, tail to head | 114.8 m 44.3 s to 126.8 m 52.9 s | 138.8 m 50.3 s to 126.8 m 41.8 s |
| Faster lanes, 2 slots, tail and head | 109.9 m 23.9 s, 115.9 m 28.2 s | 132.8 m 24.2 s, 126.8 m 19.9 s |
| Faster lanes, 3 slots, tail to head | 109.9 m 23.9 s to 121.9 m 32.4 s | 138.8 m 28.4 s to 126.8 m 19.9 s |

"Faster lanes" is a fan at 8 m/s with a lead-in of 19.1 m, and a run lane and a throat at 14 m/s after a slow lead-out of 12.01 m.
The fan and the lead-in validated on the fixture (section 2.2).
The run lane and the throat at 14 m/s validated on the fixture, but not on a London station.
The model has no wait for other pods.

The segments of the model, as length and speed limit, from the diverge or to the merge:

| Layout | Arrival | Departure |
| --- | --- | --- |
| Berth rows, row `k` | 63.25 m at 14, `45 + 37.5(k - 1)` m at 14, 50 m at 14 | 50 m at 14, `45 + 37.5(k - 1)` m at 14, 63.25 m at 14 |
| Generator depot speeds | 63.25 m at 14, 27.55 m at 1.407, 24.01 m at 1.407, then 6 m at 1.407 for each slot passed | 6 m at 1.407 for each slot passed, 12.01 m at 1.407, 24.01 m at 2, 27.55 m at 2, 63.25 m at 14 |
| Faster lanes | 63.25 m at 14, 27.55 m at 8, 19.1 m at 1.407, then 6 m at 1.407 for each slot passed | 6 m at 1.407 for each slot passed, 12.01 m at 1.407, 24.01 m at 14, 27.55 m at 14, 63.25 m at 14 |

The speeds are in m/s.
An arrival starts at 14 m/s and ends at rest, and a departure starts at rest.

### 2.4 Commands

The probes were untracked files: `internal/sim/zz_series_probe_test.go` and `internal/scenarios/zz_series_probe_test.go`.
They ran from the repository with this command, each in less than 2 s:

```sh
mise exec -- go -C <worktree> test ./internal/sim ./internal/scenarios -run 'TestZZSeries' -v -count=1
```

| Test | What it did |
| --- | --- |
| `TestZZSeriesPassengerRefused` | `shortDepotNetwork(1, 3)` with `ParkingOnly` false, once without `Depots` and once with it, then `PrepareNetwork` |
| `TestZZSeriesRetentionByPitch` | `depotNetwork` with 1 lane of 4 slots at each pitch, each fan, lead-in, chain, and lead-out lane at `DepotSpeed(pitch)`, and the lead-in at `2*reducedRetention(stopDistance(v), atan(2*Clearance/24)) + 0.01`. It printed `retention.of`, `Length`, and `chainMinimum`. |
| `TestZZSeriesFanSpeed`, `TestZZSeriesFastFan` | `depotNetwork` with 1 lane of 4 slots at a pitch of 6 m, the fan speed changed, and the lead-in at 16.1, 19.1, 24.01, 30, and 50 m until `PrepareNetwork` passed |
| `TestZZSeriesFastLeadOut` | `shortDepotNetwork(1, 3)` with the lead-out at 5 and 14 m/s |
| `TestZZSeriesTiming` | `NewFleet` on `shortDepotNetwork(1, 3)` with 2 pods. `startEmptyMove` of the head to `harbor` and of the pod at slot 2 to slot 1 in the same tick. The owners of the slot 1 resources, the wait reasons, and `Check` at each tick. Then a move-up alone. |
| `TestZZSeriesCascade` | 4 pods in `shortDepotNetwork(1, 4)`, the head sent to `harbor`, and `advanceDepots` through `Step` |
| `TestZZSeriesDischarge` | 3 pods in a lane of 3 slots, each sent to `harbor`, `garden`, and `market` at the same tick, with the owner of the slot 1 junction at each tick. The split variants used `depotLayout{leadOut: 3*Clearance + 0.02}` and `splitLeadOut(&network, 1, Clearance + 0.01)`, with the run and the throat at 5 or 14 m/s. |
| `TestZZSeriesFault` | `SetFaults(true, ...)`, `startPodFault` on the pod at slot 2, then `s.blocked`, then `startDebris` on 1 m in the middle of each lane of the depot lane |
| `TestZZSeriesLondon`, `TestZZSeriesLondonDemand` | `LondonFull()`, the berths of each station with an `-access-in` lane, the lengths of `-access-in`, `-arrival-link`, and `-in` with `Network.Length`, the fleet by station, and the shares as in `londonFullBerths` |
| `TestZZSeriesGeneratorGeometry` | `depotGeometry` for 1 lane of 2 to 5 slots, with `Network.Length` on each lane |

The kinematic model ran on a grid of 1 cm, with a forward pass at 2 m/s² and the lane limits and a backward pass at 2 m/s² to the stop.

## 3. Options

### 3.1 Topology

| Option | Shape | What it needs | Pros | Cons |
| --- | --- | --- | --- | --- |
| A. One platform, no passing | A depot lane with passenger slots. The station through lane is the bypass for pods that do not stop. | The depot geometry and the depot rules, with the parking-only refusal lifted (section 4) | The safety rules of depots hold. Shortest change. | A pod waits for each pod ahead of it to leave (section 3.4). |
| B. Parallel platforms | Two or more depot lanes, 6 m apart, at one station | Option A, with the depot lateral rule between lanes | A pod can stop at another platform when one is blocked. The depot lateral rule already holds. | Wider. Each extra platform adds a fan and a throat. |
| C. A bypass lane with a switch after each slot | Each slot has its own exit to a bypass beside the platform | A branching retention rule: each switch node fails `chainLane`, also up to 24 m downstream | Pods leave in any order | Without a new rule, each lane between two switches needs 24 m, so the pitch is at least 24 m, which is no better than rows at 25 m. The bypass and the platform are a lateral pair. |

Option C needs the branching rule that shape clearance D10 deferred.
This design does not propose that rule.

### 3.2 Berth choice and binding

| Option | Rule | Pros | Cons |
| --- | --- | --- | --- |
| A. Bind at the entry | A pod goes to the station entry. It binds a slot when it owns the last cell of the incoming lane: the slot behind the rearmost taken slot of the platform with the fewest pods (`bindDepotArrivals`). | One pod binds in each tick, in arrival order. A pod never needs to pass a taken slot. | A pod learns its slot late. Remote claims on slots stop (section 4.3). The route ends at the entry until the binding, so a platoon run cannot grow onto the entry lane (section 3.6). |
| B. Free choice on the final lane | The current late choice and terminal reevaluation, with slot routes through other slots | Small change in the choice code | Two pods on the access lane can choose out of order. A pod that claims slot 1 behind a pod that goes to slot 2 makes the circular wait of the berth claim deadlock contract: the front pod waits for the slot 1 resources in the chain cells, and the claimant waits for the node of slot 2. |
| C. Front only | Each pod goes to slot 1, and the others queue on the lead-in | No choice | Slots 2 to `N` are only a queue. This is the station buffer that was removed on 2026-10-07 ([later station-buffer claims](../studies/buffer-late-claim.md)). |

### 3.3 Where pods board

| Option | Rule | Pros | Cons |
| --- | --- | --- | --- |
| A. Any slot | A pod unloads and boards at the slot where it stops | No extra move. Each slot is a berth. | Passengers wait for pods at several positions. The model has no position of a passenger on a platform, so this has no cost in the simulator. |
| B. Unload anywhere, board at the head | A pod unloads at its slot, moves up as an empty pod, and boards at slot 1 | One boarding point, as at some real stations | A second stop for each pod, and each move-up takes 5.07 s. A boarding pod at slot 1 blocks each pod behind it. |

### 3.4 Departure order and blocking

With option A of section 3.1, a pod leaves through each slot ahead of it.
The resources give the order: the chain cells hold the slot ahead and both nodes (`everyCell`).
A pod at a slot owns the slot and its node, so a pod behind it cannot take the chain cell.
The rule needs no departure queue of its own.

Blocking cases:

1. **A pod at a rear slot finishes first.**
   It waits for each pod ahead to leave.
   A Compact pod boards in 3 s and unloads in 2 s (`internal/sim/simulation.go:12`), so the wait for a loaded pod ahead is short.
   The discharge interval of section 2.2, about 18 to 32 s for each pod ahead, is the larger cost.
2. **An idle pod ahead.**
   This is the most frequent case, because an idle pod waits at a berth until a trip or a clear moves it.
   The refused grant names a berth resource, so the pod behind waits with `Berth occupied` (`refuseGrant`).
   `clearBlockedBerths` then moves the idle pod when the waiting pod has riders, a relocation, or a trip, because `entersBerth` finds the slot in its route.
   The idle pod can only leave forward.
   If it has no parking and no free berth, it stays, and the loaded pod waits with `No parking available`.
   This is the same liveness gap as at a berth on a single access lane today.
   An idle empty pod also moves up when the slot ahead is free (`advanceDepots`), so idle pods gather at the head, where they leave first.
3. **A pod behind a faulted pod.**
   Section 2.2 shows that the fault blocks the chain lanes on both sides of the slot.
   Each healthy pod behind it stays until the fault clears.
   Riders aboard a healthy pod leave only at their stops, and stage 2 has no refuge (incident suspension contract, section 1.3, decision 11, and section 9.1).
   So riders who boarded at this platform wait in their pod for the fault.
   Each pod ahead of the faulted pod can leave.
   An arrival binds only a platform with no faulted or withdrawn pod (`depotTarget`), so it goes to another platform or waits at the entry.

### 3.5 Faults and emergencies

- **Pod fault at a slot.**
  The footprint is the berth and its node (suspension contract, section 6.5).
  By section 8.1, each lane whose cells hold one of them is blocked, which is the chain lane into the slot and the chain lane out of it.
  Routes to slots ahead and from slots behind fail, and the endpoint reroute and terminal reevaluation find no other slot on that platform.
- **Pod fault on a platform lane.**
  The footprint is each resource of the granted blocks, which holds the slots and nodes of those cells.
- **Debris.**
  Debris on a berth or a berth node is refused (suspension contract, section 1.4).
  Each lane near a slot holds a slot node, so no debris starts on a platform (section 2.2).
- **Emergencies.**
  A pod at a slot unloads there, as at any berth (emergency contract, section 1.4).
  A traveling pod chooses a station by the estimate of section 9.1, which takes candidate berths of each entry group and a route to each berth node.
  For a platform, a route to one slot node says nothing about the slot that the pod will bind.
  The candidate should be the platform, with the route to the entry and then to the slot that `depotChoice` gives in the current state, as an estimate.
  `checkOperationalTarget` refuses a depot station as a target today (`operational.go:181`), so a passenger platform station needs that refusal lifted for purpose 1.
  Today an emergency pod cannot bind a slot.
  It is out of service, and `bindDepotArrivals` skips a pod that is not in service (`depot.go:186`).
  `depotLaneView` also marks a lane as stopped for a pod that is not in service (`depot.go:63`), so an emergency unload at a slot would close the platform to arrivals.
  Both gates must test for a fault only on a platform.
  The admission tier of section 9.3 does not change.
  An emergency pod gets a free resource first, but it does not pass a pod on the lead-in, and it binds in arrival order.
  Section 9.5 of that contract makes no claim for a berth that never frees, so a platform blocked by a fault is inside its stated limits.
- **Random faults (stage 7).**
  The sampler can fault a pod at a slot.
  A platform station then loses the slots ahead of the fault for arrivals and the slots behind it for departures, for the length of the fault.
  The stage 7 rows were measured on presets without platforms, so a preset with platforms needs its own evidence.

### 3.6 Interactions

- **Banks.**
  A chain lane is both an arrival path and a departure path, and `bankPathLanes` needs one role for each (`station_banks.go:277`).
  A depot station has no banks (`validateDepots`, `depot_network.go:157`).
  This design keeps that exclusion.
- **Maneuver groups.**
  A maneuver group belongs to a Compact station, and a platform station is a depot station.
  A station cannot be both.
- **Lateral merges.**
  Clause 4 of the [lateral merge zones](lateral-merge-zones.md) design excludes depot lanes (`lateral_merges.go:44`), so platform lanes keep the isotropic junction extents.
  The fan and the through lane diverge at the entry, and they keep the isotropic extent too.
- **Shape clearance.**
  The approved rule is not built.
  Under part 4, the platform could be about 3.1 m from the through lane in place of 12 m, if the swept bodies are disjoint.
  This design does not depend on it.
- **Platoons and coasting.**
  A depot lane never joins a platoon run, and no run continues past the entry node of its destination station (`docs/model.md:264` to `:266`).
  So platform arrivals are not platoons.
  A run can grow onto the entry lane only when both routes continue after it (`sharedLane`, `internal/sim/platoon.go:487`).
  Under D2-A, the route of a pod ends at the entry until it binds a slot, so the entry lane joins no run.
  `earlyBerthChoice` exists to give the route that next lane (`berth_choice.go:59` to `:67`).
  The [station buffer record](../studies/buffer-late-claim.md#station-entry-throughput) credits the entry runs, the early choice, and coast-in together with a rise at King's Cross from 4.48 to about 6.6 entries a minute.
  A platform station can lose part of that rise.
  The study measures the entries a minute at the busiest stations.
- **Station phases.**
  Section 1.3 gives each chain lane one role.
  A pod that leaves a rear slot through the chain lanes ahead would show **Accessing berth**.
  The phase of a platform lane should come from the route, as for maneuver group lanes.
- **Large classes.**
  Depot slots admit no large class, and Express pods cannot select a small-only depot lane (`docs/contracts/express-standees-contract-proposal.md:365`).
  A large class keeps 20 m and gets no reduced retention, so a platform of large pods would need lanes of at least 40 m.
  This design keeps platforms small-only.

## 4. Proposed rule

### 4.1 Rule

This is the recommended combination of section 5.

1. A passenger station can have a depot member (D1-A).
   Each depot lane of a passenger station is a platform.
   The geometry checks, the class rules, the lateral rule, and the exclusions of depots apply.
   Each berth of the station is a slot (D9-A).
2. A pod that goes to a platform station routes to the entry.
   It binds a slot in arrival order, as `bindDepotArrivals` does (D2-A).
   A slot is taken when a pod stands at it in any activity, or when a pod names it as its destination.
   The depot view counts only an idle pod at a slot (`depotSlotOf`, `depot.go:35`), so a boarding or unloading pod would read as a free slot.
   Remote claims on slots are not made: an empty move, guarded positioning, a blocker clear, a pickup, or an operational target that goes to a platform station goes to the entry.
   `depotGoal` decides whether the station can take the pod.
3. A pod boards and unloads at the slot where it stands (D3-A).
4. A pod at a slot can leave when the resources ahead are free.
   The rule "only the head can leave" stays for parking depots.
   For a platform, dispatch takes only an idle pod with no standing pod ahead of it, and then the pod that waited longest.
5. An idle empty pod moves up as in a depot.
   `clearBlockedBerths` moves an idle pod that blocks a loaded pod behind it (D8-A).
6. A healthy pod behind a faulted pod waits for the clear (D6-A).
   Only a faulted pod closes a platform to arrivals.
   A pod withdrawn for an emergency does not, and it can bind a slot (D7-A).
7. The phase of a pod on a platform lane comes from its route.
8. Platforms have no passing inside the platform (D4).
   A station can have up to 8 platforms, as a depot can have up to 8 lanes.

### 4.2 Safety argument

The rule adds no new pair kind and no new clearance.

1. A chain lane qualifies under the current rule (section 1.4), and each chain cell holds the slot ahead and both nodes.
   The following gap is that of section 1.3 of the tick-rate contract, as for a depot.
2. Two platforms of one station are depot lanes, so their lateral pairs keep `W(v)`, or 12 m when the sweep does not clear them.
3. The platform and the through lane keep 12 m, as depot lane 1 does today.
4. A passing pod holds the slot and the node of each slot that it passes, in the cells of the chain lanes.
   So it never passes a pod that stands at a slot.
5. Binding in arrival order, the rearmost-slot rule, and the taken slots of section 4.1 make each bound slot reachable when the pods ahead stop at their slots.
   No pod waits for a resource that a pod behind it holds, so the platform adds no circular wait.
   An idle pod that cannot leave can still stop the pods behind it (section 3.4, case 2).
6. Restore keeps the order of each lane, with the changed order check of section 4.3 and `depotSlotOpen`.

### 4.3 Places that change

| Place | Change |
| --- | --- |
| `validateDepots` (`depot_network.go:157`) and `newDepotIndex` | Accept a passenger station. Keep the refusal of banks. The refusal text `must be a parking station with no banks` changes, and the test that pins the order and the text changes with it. |
| `depotForbidden`, `stationRoutesExist`, `inferStationLaneRoles`, editor `checks.go:444` | No change expected: they already let a route pass the slots of its own lane. Not run on a passenger station. |
| `docs/reference.md:139` to `:141` | A route to a slot of a platform can pass the other slots of its platform. |
| `assignTerminalBerth`, `earlyBerthChoice`, `reevaluateTerminalBerth`, `stationRouteByLoad` | A platform station gets no berth choice on the final lane. The pod binds at the entry. |
| `bindDepotArrivals`, `depotChoice`, `depotGoal` | Also for passenger pods. The binding sets the stop of the riders at the slot. On a platform, the binding takes a pod with an emergency hold, and only a faulted pod stops a lane. |
| `depotSlotOf`, `depotLaneView` (`depot.go:35`, `:50`) | On a platform, a pod that stands at a slot in any activity takes it. |
| `prepareEmptyMove`, `guardedClear`, `clearToPassengerBerth`, dispatch of pickups | A platform station is a goal at its entry, as a depot entry is a parking goal. |
| `depotOldestHead`, `depotGate` | For a platform: the idle pod with no standing pod ahead of it. |
| `advanceDepots` | Also on platforms. A pod with riders, a trip, or a boarding phase does not move up. |
| `checkOperationalTarget` (`operational.go:181`) and the emergency station choice (`emergency_choice.go`) | A platform station is a target for purpose 1. The candidate is the platform, at its entry. |
| `routeStationPhase` (`station_maneuvers.go:106`) | The phase of a platform lane comes from the route. |
| `divertStart` (`diversion.go:114`) | No change: a bound pod does not divert. |
| `SafetyObservation.Check` | No change. The chain and the lateral pairs use the depot tables. |
| `CheckContract` | A new check: on each platform, the slots of the standing and bound pods are in arrival order, and no bound slot is ahead of a standing pod. |
| Physical restore, `checkSavedDepotFields` and `checkSavedDepotOrder` (`depot_restore.go:76`, `:122`) | Change. The order check reads only idle pods at slots and pods with a slot as their destination. On a platform it must also read each pod that stands at a slot in any activity, and each pod on a platform lane. A new refusal goes after the current depot refusals, with its own text, and a test pins the order. `depotSlotOpen` must count the same pods. |
| Retention, `newLaneRetention`, `chainMinimum`, `everyCell` | No change. |
| Run-time station path searches (`routes.go:123`, `blocked_routes.go:166`), station pair connectivity, parking gaps | Not verified. These searches use the whole forbidden set with no depot exemption. A departure from a slot uses a road route, which can pass berth nodes. The probe did not run them on a passenger platform. |
| Saved state, stream, HTTP state | No new member. `stateVersion` 12 and `StreamVersion` 10 stay. The goldens do not change. |
| `cmd/scenario` and the London generator | A flag that builds passenger stations as platforms, with the speeds of D5 |
| `docs/model.md:297` to `:305`, `:361` to `:399`, `docs/station-banks.md`, `docs/london-full.md` | Text for platforms |

The editor keeps depots, refuses station actions on a depot station, and runs the full server validation for a project with a depot (`docs/model.md:396`, `docs/station-layout-editor.md:30`).
This stays true for platform stations.
The editor cannot make a platform.

### 4.4 Contract changes

Each change needs maintainer approval.
This design does not change a contract document.

| Document and section | Effect |
| --- | --- |
| Service contract, clearance layer A (`docs/contracts/service-contract-proposals.md:40` to `:51`) | Holds. A platform chain is a qualifying chain, and platform lanes are depot lanes. |
| Service contract, compatibility (`service-contract-proposals.md:57` to `:60`) | Holds. Slots admit small classes only. |
| Tick-rate contract, sections 1.3, 1.5, and 1.7 | Holds. Section 1.5 covers depot chain lanes. A later amendment can say that a depot lane can be a platform. |
| Incident suspension contract, sections 6.5, 7.2, and 8.1 | Hold. The footprint and the blocked set follow the resources. |
| Incident suspension contract, section 9.7, liveness table | Amend: add the wait "Healthy pod behind a stopped pod on a platform", ended by the departure of the pod ahead or the clear of the fault. |
| Incident suspension contract, section 1.3, decision 11, and section 9.1 | Hold under D6-A. Under D6-B, amend: a rider who has not left the boarding station can leave a trapped pod. |
| Incident emergency contract, section 9.1 | Amend: the candidate of a platform station is the platform, with the route to the entry and the slot of `depotChoice` in the current state. |
| Incident emergency contract, section 9.2 | Amend: terminal reevaluation does not run for a pod bound to a platform station. The pod binds at the entry. |
| Incident emergency contract, section 9.5 | Amend: add the wait "Bound to a platform, no slot", ended by a free slot or a clear. The row "Bound, berth occupied" names terminal reevaluation, which does not apply on a platform. |
| Incident suspension contract, sections 9.1 and 9.3, and the 9.7 row "Blocked destination berth of a healthy pod" | Amend: terminal reevaluation does not move a pod bound to a platform slot, and `divertStart` refuses it, so the endpoint reroute keeps it. The wait ends at the clear. |
| Service transitions contract, `:149` to `:151` | Holds as text. The early berth choice and the entry run do not apply at a platform station (section 3.6). |
| Berth claim deadlock contract | Holds. A platform takes no remote claim, so the cycle of that contract cannot form on it. |
| Express physical contract, `:217`, and Express standees contract, depots | Hold. Platforms are small-only. |
| Incident stage 7 contract, soak rows | Hold for the current presets. A preset with platforms is a new project and needs its own evidence. |

### 4.5 Compatibility

The rule adds no network member and no setting.
A network with no depot on a passenger station runs as today.
The `cmd/scenario` flag of section 4.3 is off by default, so the preset hashes, the command digests, and the goldens in `internal/session/testdata` do not change.
`london-full-express` does not use the flag, so project D does not change.
The Express contract refuses large classes in depot slots, so an Express pod never binds a platform slot.
No migration is needed before v1.

## 5. Decisions

| ID | Decision | Options | Recommendation |
| --- | --- | --- | --- |
| D1 | Format of a platform | A: a depot member on a passenger station. B: a new `platforms` network member, with `omitzero` and the next digest extension. | A. No format change, and the contract text on depot lanes holds as written. B needs extension 18 if shape clearance takes 17, and each contract line on depot lanes needs "or platform lanes". The cost of A is the name. Before v1, a rename is cheap. |
| D2 | Berth choice | A: bind at the entry in arrival order. B: free choice on the final lane. C: front only. | A. B makes the cycle of the berth claim deadlock contract. C is the removed station buffer. |
| D3 | Where pods board | A: any slot. B: unload anywhere, board at the head. | A. B adds a stop and a 5.07 s move-up to each visit. |
| D4 | Passing | A: none in a platform, the through lane for pods that do not stop. B: A, and parallel platforms. C: a bypass with a switch after each slot. | B. C needs a branching retention rule and gives no gain over rows at 25 m without it. |
| D5 | Platform speeds | A: generator depot speeds. B: fan at 8 m/s with a lead-in of 19.1 m, run and throat at 14 m/s. C: a pitch of 9.5 m at 3.0 m/s. | B in the study, with A as a control. Against rows today, A adds about 30 s to each arrival and each departure, and B about 5 to 12 s (inferred). C later. |
| D6 | Pods behind a faulted pod | A: they wait for the clear. B: riders of a trapped pod at their boarding station can leave it. | A. It keeps decision 11 of the suspension contract. Batch 2 of the study measures the cost. |
| D7 | Emergencies | A: the platform is a candidate at its entry, and the pod binds in order. B: an emergency pod claims a slot when it chooses the station. | A. B is a remote claim, and it can make the claim cycle. A needs the two service gates of `depot.go:63` and `:186` changed on platforms. |
| D8 | Idle pods ahead | A: move-up and the current blocker clear. B: idle pods leave a platform at once for parking. | A. B adds empty moves when the station is quiet. |
| D9 | Mixed stations | A: a platform station has only slots. B: berth rows and platforms in one station. | A, as for depots. |
| D10 | Junction runs after the head | A: keep. B: no through-pair junction at a node with one lane in and one lane out. | A in this design. B would cut the discharge interval of section 2.2, but it changes the junction rule for every network, so it needs its own design. |
| D11 | London layout | A: one platform of `N` slots. B: one platform for 2 or 3 berths, two platforms for 4 or 5. C: two platforms at each station. | B as the second study arm. A gives the shortest station. At 15 requests a minute, each station is below the discharge rate of one platform. At 60 requests a minute, the stations with 4 or 5 berths reach 3.6 to 4.3 trip ends a minute, which is more than one platform discharges. This compares visits with a departure rate only. Arrivals share the platform, and the rate with the 2 m/s run lane of D5-A was not measured. |

### 5.1 Dependencies

| Decision | Depends on | Why |
| --- | --- | --- |
| D1 | Nothing | It sets the format. |
| D2 | D1 | The binding is the depot binding. Under D1-B, the same code moves to the new member. |
| D3, D8 | D2 | Boarding and move-up use the slot order of D2. |
| D4-B | D1 | Parallel platforms use the depot lateral rule. |
| D4-C | Shape clearance D10 | It needs the branching retention rule. |
| D5 | D4 | The lane gap audit sets the slow lead-out for each platform. |
| D6, D7 | D2 | Both read the binding and the blocked set. |
| D7 | Emergency contract amendment | Section 9.1 changes. |
| D11 | D4, D5, and the station generator study | The generator study of rows at 25 m and short access lanes, which runs apart from this design, sets the base for the comparison. |
| D1-B only | Shape clearance D7 | Its field takes extension 17. |

D1, D2, D3, and D7 together define the rule.
D5, D10, and D11 are layout and tuning choices, and each can be approved alone.

## 6. Test plan

Unit tests:

- Validation: a passenger station with one platform of 3 slots validates.
  The same station with a bank, a large class on a slot, or a berth that is not a slot is refused, with the order and the text of the depot refusals.
- Routes: the station path to each slot passes the slots behind it, and a route from a slot passes the slots ahead.
  A route to a slot of another platform of the station does not pass a slot.
  The editor check gives the same result.
- Binding: two pods on the access lane bind slots in arrival order, also when the second pod is older in admission.
  A pod with no free slot waits at the entry.
- Departure: a pod at slot 2 that finishes before the pod at slot 1 leaves after it.
  The resources refuse the grant with `Berth occupied`, and `Check` passes at each tick.
- Idle pod ahead: an idle empty pod at slot 1 moves when the loaded pod at slot 2 must pass it.
  With no parking and no free berth, the loaded pod waits with `No parking available`.
- Fault: a pod fault at slot 2 of 3 blocks the two chain lanes and slot 2.
  The pod at slot 1 leaves, the pod at slot 3 waits, and an arrival binds another platform or waits.
  After the clear, all pods leave.
- Emergency: an emergency on a pod at slot 2 unloads there.
  A traveling emergency pod chooses a platform station and binds in arrival order.
- Phase: a pod that leaves slot 3 shows **Departing berth** on the chain lanes.
- Restore: a save with pods at slots 1 and 3 and a bound pod restores with the same order.
  A demoted pod takes only the slot behind the rearmost taken slot.
- Dispatch: dispatch does not take an idle pod at slot 2 while a pod stands at slot 1.
- Taken slots: a pod that boards at slot 1 takes it, and an arrival binds slot 2.
- Emergency: an emergency unload at slot 1 does not close the platform, and a traveling emergency pod binds a slot.
- Restore order: a save with a boarding pod at slot 1 and a pod bound to slot 1 is refused with the new platform refusal, after the current depot refusals.
- `CheckContract`: a state with a bound slot ahead of a standing pod fails.
- Lane gap audit: a lead-out faster than `reducedClearanceSpeed` within 12 m of a chain lane is refused, as section 2.2 found.

Mutations, with the table of the `verify-by-mutation` skill:

| Mutation | Test that fails |
| --- | --- |
| Bind at the final lane in place of the entry | Binding order test |
| Bind the frontmost free slot, also ahead of a taken slot | Binding order test and the `CheckContract` test |
| Chain cells without the slot ahead | Departure test (`Check` fails) |
| Dispatch takes any idle pod of a platform | Dispatch test |
| `clearBlockedBerths` skips a pod that passes the berth | Idle pod ahead test |
| A faulted pod does not close its platform to arrivals | Fault test |
| Remove the platform target of an emergency | Emergency test |
| Phase from the lane role on a platform | Phase test |
| Restore places a demoted pod at any free slot | Restore test |
| `depotSlotOf` counts only idle pods on a platform | Taken slots test, restore order test |
| The binding skips a pod that is not in service | Emergency test |

The byte proofs of `AGENTS.md` run again.
The widest project may not hold a full depot member on each station, so the proofs must show that a passenger station with a depot member stays inside the limits.

## 7. Study plan

The method is that of the [station scale study](../studies/station-scale.md#method): LondonFull at scale 0.5, the AM peak at 15 requests per minute, 60 minutes of arrivals and at most 60 more, sharing and redistribution off, free-flow routing, and virtual platoons.
`cmd/scenario` gets a flag that builds passenger stations as platforms, with the layout of D11 and the speeds of D5.
The study records means over seeds and paired changes against the base.

1. Timed pilot: the base and arm P1 of batch 1, seed 1.
   Record the time and the memory (the scale study took about 31 s and 650 MB for each run).
   Record the slot waits, the binding waits at the entry, and the `No parking available` time.
   Record the entries a minute at the five busiest stations, as in the [station buffer record](../studies/buffer-late-claim.md#station-entry-throughput).
2. Batch 1, seeds 1 to 3, 4 arms:
   - Base: berth rows at scale 0.5, or the result of the station generator study if it is adopted first.
   - P1: one platform at each station, generator depot speeds (D5-A, D11-A).
   - P2: one platform at each station, faster lanes (D5-B, D11-A).
   - P3: one platform for 2 or 3 berths, two for 4 or 5, faster lanes (D5-B, D11-B).
3. Batch 2, seeds 1 to 3, 2 arms: the base and the best platform arm of batch 1, each with the stage 7 fault and emergency rates of the incident soak.
   This measures D6.
4. One 30-minute run of the best arm with `Check`, `CheckContract`, and the platform order check at each tick, with the smallest gap on a platform.

Batch 1 has 4 arms and batch 2 has 2, so the study stays inside the cap of 6 arms and 3 seeds.
The criteria are those of the station scale study: drained seeds, mean wait, mean journey, track and junction wait, and stopped pod time.
An arm at 60 requests per minute, or a pitch of 9.5 m (D5-C), comes back as a proposal.

## 8. Review

An adversarial review on 2026-11-16 found two blockers and eight major gaps.
The maintainer parked the design with this record, and made no decision of section 5.

1. Blocker: an emergency pod bound to a platform entry has no destination berth, which section 9.4 of the service transitions contract and `state_contract.go:173` require of a traveling purpose-1 pod.
   Sections 9.3, 9.4, and the purpose-1 restore row of section 9.6 would need amendments, with round trips before, during, and after the binding.
2. Major: the depot refusal at `depot_network.go:195` is part of the format, so "no format change" holds only for the serialized members.
   No new member or digest extension is needed, but each changed refusal needs an audit and new fixtures.
3. Blocker: the binding at `depot.go:154` knows an unbound arrival only through `RelocatingTo`, and a failed binding falls back to `parkReleased`, which can send the pod to another station.
   Loaded arrivals, assigned pickups, relocations, emergencies, and fault recovery each need their own binding, no-slot, cancellation, and restore rules.
4. Major: `routes.go:123` and `blocked_routes.go:166` keep every berth node forbidden, so a search from the entry to the head fails past a tail slot.
   Reachability, continuation, estimates, and slot binding need separate rules, and pickup access must stay independent of occupancy (suspension contract section 9.4).
5. Major: an emergency candidate that depends on free slots breaks the no-candidate memo of `emergency_choice.go:104`, whose proof assumes candidates that do not depend on occupancy.
6. Major: local dispatch (`dispatch.go:479`) and guarded positioning (`positioning.go:542`) do not pass through `depotGate`, so the dispatch restriction of section 4 does not reach them.
7. Major: the no-cycle argument covers slot order only.
   An intermediate stop with phase zero, a blocked departure, or exhausted parking can hold a platform past a fault clear.
8. Major: `depotSlotOf` finds a pod through its destination, so a departing pod that faults on the lead-out does not close its platform.
9. Major: no two-platform layout with fast exits was validated, so D5 and D11 depend on a geometry that does not exist yet.
10. Major: the comparison uses the berth rows before tight stations.
   With tight stations, a 101 m path takes about 10.7 s (inferred), and the discharge of 2 to 3 pods a minute is a burst figure, not a sustained rate.
11. Minor: debris is refused only for segments that hold a berth or a berth node (`debris.go:74`), so fans, runs, and throats can hold debris.
12. Minor: the dependency table omits D9 and D10, and section 5 does not separate a proof that applies from evidence that must be measured again.

The review found no case for platforms at London stations now that tight stations are the default.
A small prototype could suit a quiet station of small pods, where the width of the station or the storage of pods matters more than the time of a stop.
