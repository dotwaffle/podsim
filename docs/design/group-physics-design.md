# Group pod physics

The group class supports eight seats, parties through eight, and a six-meter body length.
The model uses a centered six-meter-radius envelope, approved by the user on October 3, 2026.
Express is unavailable for physical operation unless a project selects the opt-in `express-v1` order contract (see [the Express qualification](../studies/express-qualification.md)).

Station buffers and compact station queues were removed on October 7, 2026, because measurements showed that they lowered station entry throughput.

This model does not certify a manufactured vehicle, steering geometry, or seating arrangement.
No station, fleet, speed, or default policy changes automatically.

## Class dimensions

The route position of a large class is the center of its body envelope.
Small-class position behavior is unchanged.

| Class | Seats | Standee places | Length | Maximum width | Envelope radius |
| --- | ---: | ---: | ---: | ---: | ---: |
| Group | 8 | 0 | 6 m | 2.5 m | 6 m |
| Express | 12 | 8 | 6 m | 2.5 m | 6 m |

These dimensions are simulation assumptions, not measured vehicle specifications.
A centered 6-by-2.5-meter rectangle has a half-diagonal of about 3.25 meters, so the six-meter circle encloses it in every orientation.
Both classes use the same circle to reduce the mixed-class cases to prove.
A later per-class envelope can use less space after separate qualification.
The approval does not authorize default adoption, deployment, real vehicle certification, or a detailed axle and steering model.

## Geometry and ownership

An interaction with a large class requires at least 20 meters between reference positions on overlapping physical planes.
A merge, split, or shape pair with a large pod keeps the side-by-side distance of its two classes, 3.55 meters with a small pod and 4.0 meters with a large pod, as the [shape clearance](shape-clearance.md) design defines.
Each other pair with a large pod keeps the 20 meter clearance, a following pair included: two pods on one lane, or on two lanes where one ends at the node where the other starts.
The overlapping physical planes are those of the envelope of "Body planes" below.
Large-admitting lanes require at least 40 meters of length and actual cells of at least 20 meters.
Station and berth masks must both admit the class.
Network preparation checks the largest admitted class, even when the fleet contains only small pods.
It also checks small lanes that meet a large-admitting lane or berth node.
The static extents of a merge, split, or shape pair use the union body of the classes that each lane admits, and not the body of the largest class.
Amended 2026-11-16, by maintainer decision: the following, merge, split, and shape pairs of the shape clearance design, behind the `shapeClearance` network field.

A 20-meter path gap on a curve can give less than 20 meters of physical separation.
Preparation derives a larger retention tail from the actual movement polyline.
Let `c` be the minimum projection of a unit segment tangent onto the unit lane chord.
The projection bound is `20 / (c * (1 - conflictSlack))`.
Every relevant nonzero segment must advance on the chord axis.
Preparation rejects folded paths, zero chords, and nonfinite bounds.
Straight paths retain a tail of exactly 20 meters.
For curved paths, the tail is `min(projection bound, max(20, grid(scan + h)))`, where `h` is the larger of 0.25 meters and 1/1024 of the lane length, and `grid` rounds up to a multiple of `h`.
The value `scan` is the largest path gap of a pair closer than `20 + h/2 + 1 mm`, with the rear position in steps of `h` over the lane and the lane end, and the front position at the last close point.
A walk finds that point from the lane end, and each step back is the distance over that limit, with a minimum step of 1 mm.
A lane that passes a fixed budget of distance evaluations in this walk takes the projection bound.
The tail is above each path gap of a pair closer than 20 meters.
Inside the budget, it is under the lane length plus `2h`, and a tail that exceeds the lane means that the lane holds one pod.

Each immutable lane-cell index stores its tail and its independent start-node tail.
Endpoint ownership covers every cell that overlaps the applicable endpoint tail.
Reservation, release, route trimming, origin retention, and cold restore use these same bounds.
An unrelated destination node cannot enlarge the retained origin.
Junction conflict intervals already contain their clearance expansion, so release does not add another tail.

Old-only lanes retain the existing 24-meter minimum, 12-meter clearance, and release arithmetic.
Large and mixed virtual links are invalid.
An ordinary compact group beside a large pod must satisfy the larger outside separation bound.

## Body planes

The native safety observation captures private, owned plane arrays when the fleet contains a large pod.
It binds each array to the complete pod value from that observation.
The route window includes the retained rear and a front bound that encloses the body.
A stationary berth includes its raw plane and every incident lane plane.
A departing pod retains its raw origin berth plane until origin release.

A large pair receives a plane exemption only when every cross-pair of occupied locations proves separation.
Missing arrays, changed pod values, or overlapping incident planes refuse the exemption.
Public center-only locations cannot grant this exemption.
When a tail bound of the network is not finite, the network audit applies the same rule before a run, and preparation then refuses the network: a large pair of lanes gets the exemption only when every cross-pair of the lanes, their incident lanes, and the berths at their From nodes is on different planes.
The audit uses the whole lane, so a pass that is far from the join on a lane that is incident to a shared plane is also refused.
The audit returns this refusal as a `PlaneReachError`, and a caller reports it after its other checks (the path, length, and cell checks of preparation, the placements of a fleet, and the other checks of a project), so that it does not hide an earlier refusal.
The audit checks each pair, small pairs included, in each network.
For a pair with a large path, it takes the planes of each path by position, and not for the whole lane.
The envelope reach of a position on a lane holds each plane that the envelope of a pod at that position can hold, on each route through the lane.
The reach can hold more planes than such an envelope, but not fewer.
This includes the lanes behind the position on the route, each up to its retained tail, also more than one lane back.
When the fleet has a large pod, the envelope of a pod comes from the envelope reach, and not from the largest front bound of the route.
The slack is `envelopeReachSlack`, 1 millimeter.
Rule 1: a curved lane with no geometry tail has the front bound `min(6 / (c (1 - conflictSlack)), 16.2 meters)`, where c is the minimum projection of a segment of the lane on its chord, and 16.2 meters when c is 0 or less.
Rule 2: when an envelope holds a lane of a maneuver group, it also holds each lane at a node of the cluster of that lane.
A cluster is a maneuver group with each group that shares a node with it, and so on.
Rule 2 applies to the envelope of a traveling pod, of a pod before its origin release, and of a pod at a berth.
The anchor front of a lane L is the larger of 6 meters and the front bound of L, with rule 1.
It does not take the front bound of another lane.
An anchor on L holds L, each lane behind up to its retained tail, the origin berths and the lanes at their nodes up to the origin tail, and each lane that starts within the anchor front of L ahead of the position, by the shortest walk along lanes.
Each bound has the slack, and rounds outward.
The reach of a published lane position is the union of the planes of each anchor within the slack of that position, also across one or more nodes, each anchor with its own anchor front.
An anchor on another lane holds only the planes along the paths through the published lane: its walk on the side of the published lane starts at the far end of that lane, with the distance through it.
The reach is compiled once, when the network is prepared.
The envelope of a traveling pod is the part of the reach at its published lane position that holds the lanes of its route, and the origin berth and the lanes at its node before the origin release.
The envelope of a pod at a berth is the reach of the berth.
Rule 2 then applies to the envelope.
The reach applies rule 2 to each lane and each berth that it holds.
It includes the origin berth of the route and the lanes at its node, at each position before the origin release.
A berth has its own plane, the planes of the lanes at its node, and by rule 2 the planes of each lane at a node of the cluster of a maneuver group lane among them.
A test shows, for each lane of a test network and positions along it, that the planes of `appendVehicleSafetyLocations` for a pod on each route through the lane are in the envelope reach at that position.
The [shared-track](shared-track-preset.md) design proves that the reach holds each plane of the envelope, and that the envelope holds each lane of the support of a body, under rule A1 of the [shape clearance](shape-clearance.md) design.
The proof covers a body inside a disk of 5.404 meters around the reference for a large pod, and 4.7269 meters for a small pod, with the guideway tolerance of 0.25 meters.
These disks hold the 10 meter gate body, the grown large body, and the grown small body of the shape rule, but not a body that fills the whole 6 meter envelope disk.
The proof does not cover a support that passes a maneuver group lane and then a lane with a reduced retention, both wholly in the disk.
A test puts these bodies at sampled poses on test networks.
It shows that each lane of the support is in the envelope, and that each plane of the envelope is in the reach.
The test also shifts each route distance and each published position by two `restoreTolerance` steps each way.
At positions that the service contract calls separated, the pair is exempt, as before.
At the other positions, two lanes keep the shape certification of the [service contract](../contracts/service-contract-proposals.md).
Two lanes that it does not certify, and a pair with a berth, keep 12 meters, or 20 meters when either path admits a large class, at those positions.
The certified pairs and the skip rules of the maneuver groups, the depot lateral pairs, and the retention rules come first and do not change.
The oracle takes its envelope from the reach at the published lane position, so the front bound of a route lane extends the route window only from an anchor on that lane.
The envelope reach holds each plane of the route window of the oracle, so the audit covers that window.
The order of the refusals and their text do not change.
Amended 2026-11-16, by maintainer decision: the envelope planes of the audit, behind the `shapeClearance` network field.
Amended 2026-11-16, by maintainer decision: the local front of the envelope, the small-pod rules, and the reach bound of each lane, behind the `shapeClearance` network field.
Amended 2026-11-16, by maintainer decision: the anchor front of each lane, the compiled reach, and the envelope that reads the reach, in place of the local front and the reach bound, behind the `shapeClearance` network field.
Amended 2026-11-20, by maintainer decision: the shape clearance rule applies to each network, and the network fields `lateralMerges` and `shapeClearance` are removed.
The observation uses the existing tick rate and authored speed limits.
Its separation has no braking term, because a pod stops inside the cells that it owns, so the [rates of a class](../model.md#vehicle-classes) do not change it.

## Restore and public consumers

Physical restore reconstructs the current footprint and all unexpired past resources with the larger tails.
It preserves retained berth, node, and junction ownership.
Saves keep the speed and the reservation of each traveling pod.
A pod that cannot stop within its saved reservation restores at rest, and then its future track grants can differ.
Restore rejects large links before either restore tier runs.
Recorded fallback preserves request identities, consent, original times, and distance baselines.

Native saves have no geometry fingerprint.
Native restore validates the supplied geometry, classes, routes, and origin.
The session save resolver binds the state to its saved source project before native restore.
No new project, save, or stream version is required.

The editor checks class-specific lane lengths and pair clearances.
Large drafts also run native preparation, including drafts without station banks.
A failed edit returns no patch.
The stream assembler checks group station and berth admission and all current, motion, and display lane masks.
It rejects group links before class caches or published frames change.

## Qualification evidence

The [measurement record](../measurements/group-physics-qualification.json) pins native sources, checks, trace hashes, and byte maxima.

The focused tests include actual journeys with group, compact, and legacy leaders and followers.
They cover straight and curved motion, stopped following, merges, ordinary and bank stations, and parking.
Every observed tick checks physical separation, speed changes, owned stopping bounds, position continuity, and order conservation.
Cold checkpoints cover passenger service, empty fetch, origin retention, route trimming, and intermediate shared unloading.
At qualification, actual consumers exercised save-6 restore and hello-3 full and replacement-delta frames; the current versions are 11 and 9.

Independent review found a missing raw origin plane during departure.
The corrected observer retains that plane until origin release.
Compiled mutations that remove endpoint extent or derived constructor bounds cause actual motion to violate the 20-meter bound.
Both endpoint regressions measure 19.9939634194 meters at tick 11946.
The tests fail on that measured gap before their resource-cell assertions.

Four complete 36,000-tick ordinary snapshot traces match the pre-change baseline exactly.
Existing ordinary encoded fixtures remain unchanged.
The caps in the table below are those of the tested source; the saved-state cap is now 128 MiB and the stream JSON cap 64 MiB.
Separate maximum encoding fixtures contain actual group class values and use the existing count limits.
They do not claim that every combined maximum is physically reachable.

| Encoding fixture | Raw bytes | Gzip bytes | Cap in bytes |
| --- | ---: | ---: | ---: |
| Historical save | 83,270,697 | 835,937 | 83,886,080 |
| Modern save | 83,240,397 | 840,550 | 83,886,080 |
| Mixed save | 83,255,547 | 839,920 | 83,886,080 |
| Full stream | 62,095,518 | Not applicable | 67,108,864 |
| Replacement delta | 62,228,597 | Not applicable | 67,108,864 |

The save fixtures pass gzip, inflate, decode, source-index resolution, and re-encode checks.
The queue, stored rider, manual submission, fleet, registry, save-byte, and stream-byte limits stay unchanged.
Group qualification does not qualify express operation, a larger encoding, service capacity, or default adoption.

The full repository functional suite, full native race suite, and coupled group save and stream race tests pass.
Vet, lint, fresh Go diagnostics, browser tests, and WASM compilation also pass.
The full save race suite reached its 14-minute timeout inside the maximum-size compact archive decoder of that source.
The regular suite checked those maximum encodings.
The record preserves the timeout and the successful scoped race checks.
