# Shared-track Express preset

Status: proposed on 2026-11-16, revised on 2026-11-16 after review (section 10).
On 2026-11-16, the maintainer approved the contract amendment of this design as ST1 to ST15 of the [shared-track amendment proposal](../contracts/shared-track-amendment-proposal.md), and the SX rows as the [Express shared-track qualification](../contracts/express-shared-track-qualification-proposal.md) contract, and the text was applied.
On 2026-11-16, the maintainer approved ST16 to ST24 of the [second shared-track amendment proposal](../contracts/shared-track-amendment-2-proposal.md), for the shape of section 3.1, and the text was applied.
Build record of 2026-11-16: slices 0 to 5 of section 7 are built, and the commit ids are in that section.
The preset is `london-full-shared-express`, with 5,790 nodes, 9,089 lanes, and 343 pods.
Note of 2026-11-20: slice 6 is done.
The [Express shared-track qualification](../contracts/express-shared-track-qualification-proposal.md) contract records the evidence of each row, and the preset is qualified.
The CI soak and `soak:full` run the preset.
Slice 7, the study batch, is open.
Amended 2026-11-20, by maintainer decision: the shape clearance rule applies to each network, and the network fields `lateralMerges` and `shapeClearance` are removed.
Note of 2026-11-20: the [fillets proposal](../contracts/london-fillets-proposal.md) changes the roads of LondonFull.
D does not change.
Note of 2026-11-20: the fields are removed, and S1, A1, and the compiled reach apply to each network.
The text below records the design with the field.

This design is decision D11 of the approved [shape clearance](shape-clearance.md) design.
D11 keeps project D (`london-full-express`) as it is, and it adds a new opt-in preset with Express on shared main-line track between Waterloo and Bank.
This design gives the measurements on the current code, the changes that remove each refusal, the preset, the proposed qualification rows, and the decisions for the maintainer.
It also proposes a front bound for each lane in the envelope reach index, in place of the bound of the whole network.
Slices 1 to 3 of section 7 changed the code after the approval.

Summary:

- On the default LondonFull network with Express on the route, the built audit refuses 14 pairs, and 10 of them are closer than 4.0 m.
  With the index for each lane of section 3.1, it refuses the same 7 pairs, and 5 of them are closer than 4.0 m (section 2.2).
- The tight hubs have 32 Express lanes of 25.0 m or 25.5 m, under the 40 m of a large lane.
  This blocker is larger than the one of the shape clearance design, which measured the wide stations.
- A set of geometry changes removes each refusal and each short lane in a copy of the network.
  With the front bound for each lane, the audit then refuses no pair.
  With the bound of the whole network, it refuses 7 pairs, unless two more junction lanes go.
- The maintainer approved S1-A: a front bound for each lane in the index, behind the `shapeClearance` field.
  On 2026-11-16, after a code review of slice 1, the maintainer chose the anchor front of section 3.1, which does not recurse, and an oracle that reads the compiled index.
  Section 3.1 proves that the oracle holds each plane under the body of a large pod, with axiom A1, which the maintainer accepted as a rule of the model.
- Section 3.1 also closes the gaps of the small pod of a mixed pair under the field, with two rules that it proves for the grown small body and measures.
- The committed study tests of section 8 reproduce each number of sections 2 and 3, and they pin the input and network hashes.

## 1. Goal and scope

The goal is a preset that runs Express on the lanes of link 100, the Waterloo and City route, between the two hubs.
D stays at scale 1 with its dedicated trunks, so the two projects can be compared.

The scope is one route in each direction, and the two hubs:

- Waterloo to Bank: `940GZZLUWLO-access-out`, `940GZZLUWLO-road-out-01`, `london-link-100-ba-1`, `london-link-100-ba-2`, `940GZZLUBNK-road-in-08`, and `940GZZLUBNK-access-in`.
- Bank to Waterloo: `940GZZLUBNK-access-out`, `940GZZLUBNK-road-out-08`, `london-link-100-ab-1`, `london-link-100-ab-2`, `940GZZLUWLO-road-in-01`, and `940GZZLUWLO-access-in`.
- Each lane of the two hubs, except the roads that are not on the route, and the hubs and their berths.

Each link lane is 955.6 m long.
The route passes no other station.
The changes of section 3 also touch lanes that are not on the route: junction lanes and roads at the two hubs, and no lane elsewhere.

The design depends on these decisions:

- The approved shape clearance rule: D1-B, D3-A, D5 included, D6-B, D12-A, D2 for all classes, D4 at 20 m, and D7, the network field `shapeClearance`.
- D8: a parking comb of 40 m by 40 m beside each origin hub.
- D9: the first hub pair is Waterloo and Bank.
- D11: D stays, and the shared-track preset is new.
- The maintainer decisions for this draft: the preset builds on LondonFull with tight stations, at station scale 0.5, with `shapeClearance` set.
  A tight station does not use the station scale (`LondonOptions.StationScale`, `internal/scenarios/london.go:197`).
  So scale 0.5 is the LondonFull default, and it changes no station of this preset.
- The maintainer decisions of 2026-11-16 after review: A1 is a stated rule of the model, S1-A goes ahead with the proof, and the small-pod gaps close in D11 under the field.

## 2. Measurements

### 2.1 Method

All measurements are at `7bebe01e`, on the default LondonFull network: tight stations, `lateralMerges` set, 5,740 nodes, 9,024 lanes, network SHA-256 `e194426aa95104424a6365370f014c166f25d0572199f9b875e07bb58f2e880e`.
The study tests of section 8 (`internal/sim/shared_track_study_test.go`) do these steps:

1. Set the classes Legacy, Compact, and Express on the route lanes, as `TestShapeClearanceStudyRoutes` does (section 2.11 of the shape clearance design).
2. Set `shapeClearance`.
3. Run a copy of `auditBankGeometry` that records each refusal, in place of the first one, with the closest distance of the parts that are not separated.
4. Run the same copy with an envelope reach index whose ahead walk takes the front bound of each lane (section 3.1), in place of the bound of the whole network.

The study pins the SHA-256 of each input file and of each network that it builds, and the counts of refusals and shape pairs of each audit run.
A change to one of these pinned values fails the study.
The other numbers of sections 2 and 3 are in the log of the study, and the containment test fails on each failure of the proposal.
Two checks tie the copy to the code: its first refusal is the error of `ValidateBankGeometry`, and its spans with the built bound are the spans of the built index.
The copy gives the counts of "Counts of the built audit" in section 2.2 of the shape clearance design: 40 shape pairs and 14 refusals with the built bound.
With the index for each lane, it gives the 7 refusals that the shape clearance design gives for a front reach of 6 m (section 2.2).
The wide network gives the same refusals.
Since the road fillets of 2026-11-20, the tight input of the study has filleted roads and gives 9 refusals.
The wide input keeps straight roads (`-road-fillets=false`), as the preset does, and gives these 7 refusals.
For each refusal, the study runs the shape sweep, and each sweep finds a collision.
The free-flow routes are 2,171.5 m from Waterloo to Bank and 2,102.8 m back, from the station exit to the station entry.

### 2.2 Refusals

Each refusal has the text `nonincident paths "A" and "B" are less than 20 meters apart`, with the two lanes in audit order.
The distance is the smallest distance of the parts that the reach does not separate.
"Bound" tells which front bound refuses the pair: "both", or "whole network" only.
"Cause" names the plane that one reach holds and that shares a node with a plane of the other reach.

| A | B | Distance | Place | Bound | Cause |
| --- | --- | ---: | --- | --- | --- |
| `940GZZLUWLO-road-in-05` | `london-link-100-ab-2` | 0.00 m | Waterloo | Both | The road crosses the link 4.9 m before the end of the link. The last 6 m of the link hold `road-in-01`, which shares the diverge with `road-in-05`. |
| `london-link-350-ab-2` | `london-link-100-ba-1` | 0.00 m | Links | Both | The links cross 27.7 m from the start of `ba-1`. The first 64.5 m of `ba-1` hold `940GZZLUWLO-move-05-01`, whose tail is 64.5 m. It shares `london-link-350-b-arrival` with `london-link-350-ab-2`. |
| `940GZZLUWLO-move-05-05` | `london-link-100-ba-1` | 2.24 m | Waterloo | Both | The same tail of `move-05-01`, which shares `london-link-350-b-arrival` with `move-05-05`. |
| `london-link-098-ab-1` | `940GZZLUBNK-road-in-08` | 2.50 m | Bank | Both | The first 20 m of `link-098-ab-1` hold `940GZZLUBNK-move-08-06`, which shares `london-link-100-a-arrival` with `road-in-08`. The line of `link-098-ab-1` passes 0.30 m from the Bank diverge, 23.5 m from its start. |
| `london-link-098-ab-1` | `940GZZLUBNK-access-in` | 3.47 m | Bank | Both | The first 20 m of `link-098-ab-1` hold the junction lanes `move-0X-06`, which share their portals with the roads `road-in-0X`. The first 20 m of `access-in` hold each `road-in-0X`. |
| `london-link-098-ba-2` | `940GZZLUBNK-road-in-08` | 4.55 m | Bank | Both | The last 33.62 m of `link-098-ba-2` hold `road-in-06`, which shares the diverge with `road-in-08`. The end of `link-098-ba-2` is 4.55 m from `road-in-08`. |
| `940GZZLUWLO-road-out-01` | `london-link-350-ab-2` | 6.26 m | Waterloo | Both | The last 66.42 m of `link-350-ab-2` hold `move-05-01`, which shares `london-link-100-b-departure` with `road-out-01`. |
| `940GZZLUWLO-move-01-06` | `940GZZLUWLO-road-out-01` | 0.00 m | Waterloo | Whole network | The reach with the front bound of 66.42 m holds a plane that shares a node with the other reach. With the bound of each lane, the pair is separated. |
| `940GZZLUWLO-road-out-01` | `940GZZLUWLO-move-01-04` | 0.00 m | Waterloo | Whole network | As above. |
| `940GZZLUWLO-road-out-01` | `940GZZLUWLO-move-01-03` | 0.00 m | Waterloo | Whole network | As above. |
| `940GZZLUWLO-road-out-01` | `940GZZLUWLO-move-01-05` | 2.58 m | Waterloo | Whole network | As above. |
| `940GZZLUWLO-move-01-02` | `940GZZLUWLO-road-out-01` | 3.09 m | Waterloo | Whole network | As above. |
| `940GZZLUBNK-road-in-08` | `940GZZLUBNK-move-03-08` | 4.37 m | Bank | Whole network | As above. |
| `940GZZLUBNK-road-in-08` | `940GZZLUBNK-move-02-08` | 5.17 m | Bank | Whole network | As above. |

With the front bound of the whole network, the audit refuses 14 pairs, 10 under 4.0 m, and it has 40 shape pairs.
With the index for each lane of section 3.1, it refuses the first 7 pairs, 5 under 4.0 m, and it has 39 shape pairs.
The small-pod rules give the same 7 refusals.

Note of 2026-11-16: before the paths through the published lane of section 3.1, the index for each lane refused 11 more pairs, 18 pairs, 6 under 4.0 m, with 76 shape pairs.
Each of the 11 more pairs had a part of 1 mm or less at an end of a lane.
There, the anchors within the slack of the published position cross a node, and the envelope of an anchor on the other side held a sibling lane of the diverge or the merge:

- `road-in-08` and each of `940GZZLUBNK-move-06-01` to `move-06-08`, 4.54 m and 4.55 m: the first 1 mm of each `move-06-0X` holds `road-in-06`, which the end of `london-link-098-ba-2` holds.
  This is the pose of the pair `london-link-098-ba-2` and `road-in-08` above.
- `road-out-01` and each of `940GZZLUWLO-move-05-03` and `move-05-04`, 6.26 m: the first 1 mm of each holds `move-05-01`, which the end of `london-link-350-ab-2` holds.
  This is the pose of the pair `road-out-01` and `london-link-350-ab-2` above.
- `road-out-01` and `940GZZLUWLO-move-05-05`, 2.24 m: the last 1 mm of `road-out-01` holds `move-05-01`, which the start of `london-link-100-ba-1` holds.
  This is the pose of the pair `move-05-05` and `london-link-100-ba-1` above.

So each of these refusals was the pose of a refusal of the first 7 pairs, at a node, with the name of a lane next to it.
With the union of the anchors removed, the study gave the first 7 pairs and 39 shape pairs again, so the union was the only cause.
The paths through the published lane remove these 11 refusals.
No refusal includes a berth or a berth link.

### 2.3 Front bounds

The front bound of a lane is `6 * projection bound / 20` (`internal/sim/profile_geometry.go:83`).
Only a lane at a node of a large lane has one, and it is zero on each other lane.
On this network, 27 lanes have a front bound over 6 m, and 24 of them are less than 6.01 m.
The other three are junction lanes:

| Lane | Front bound | Tail | Length | Movement | Station pairs whose free-flow route uses it |
| --- | ---: | ---: | ---: | --- | ---: |
| `940GZZLUWLO-move-05-01` | 66.42 m | 64.5 m | 64.2 m | Link 350 in to link 100 out, at adjacent portals 11 m apart | 0 |
| `940GZZLUBNK-move-06-08` | 33.62 m | 57.5 m | 67.4 m | Link 098 in to link 100 out | 8 |
| `940GZZLUBNK-move-08-02` | 23.81 m | 39.5 m | 39.1 m | Link 100 in to link 094 out | 10 |

`move-08-06` has no large front, and 8 station pairs use it.
The built index takes 66.42 m for each lane (`reachFront`, `internal/sim/envelope_reach.go:263`).
The anchor front `B(L)` of section 3.1, without the small-pod rules, is 6.01 m or more on the 3 junction lanes of the table only.
A lane before a curved junction lane keeps 6 m.
A pod within the slack of its end also has the anchors at the start of the curve, so the index holds the planes of the curve there.
The route counts are over the 97,032 pairs of station exit and station entry, with the free-flow cost of `laneSeconds`.

### 2.4 Lane length

Under the 40 m of a large lane (`LaneMinimumLength`), the tight network has 33 route and hub lanes:

- At each hub, `access-in` and `access-out` at 25.5 m.
- At each hub, each berth row lane `-in` and `-out`, and each row link `-arrival-link` and `-departure-link`, at 25.0 m: 8 at Bank and 20 at Waterloo.
- `940GZZLUWLO-road-in-01` at 34.3 m.

Preparation refuses the first: `lane "940GZZLUBNK-access-in" must be at least 40 meters long`.
On the wide network, 11 lanes are short: 10 berth links of 37.5 m and the same road.
There, preparation refuses `940GZZLUBNK-02-arrival-link`.
So the blockers of the shape clearance summary still apply on the tight network, and the tight shape adds 22 more short lanes.
The short road is short on both networks, so it does not depend on the station shape.

### 2.5 Ramp junctions

The study also runs on `-junctions ramps`.
There, the audit refuses 14 pairs, 11 under 4.0 m, with the bound of the whole network, and 17 shape pairs.
With the index for each lane, it refuses 14 pairs, 11 under 4.0 m, and it has 14 shape pairs.
Before the paths through the published lane of section 3.1, it refused one more pair, `road-in-08` and `940GZZLUBNK-ramp-06-07` at 4.54 m, with a part of 1 mm at the start of the ramp, as in section 2.2, and it had 15 shape pairs.
The ramps split link 100 into lanes of 40.0 m, and 46 lanes are short.
Preparation refuses `london-link-100-ba-1` at 40.0 m by rounding.
The ramp junctions are not a better base for this preset.

## 3. Changes that remove the refusals

### 3.1 Front bound for each lane

#### Definition

Note of 2026-11-16: the first build of slice 1 used a recursive front bound `F(L)` with a seed of lanes behind, and a local front in the oracle.
A code review found two blockers.
First, a group lane shorter than the slack let a lane two lanes back reach the local front, and the seed missed it.
Second, the wider seed of the fix fails at an exact slack bound, for example a chain P, L, Q, M, N where the walk distance to N equals the bound plus the slack as a float64.
Each fix of the closure added a case that the next review broke.
On 2026-11-16, the maintainer chose the shape of this section: a front that does not recurse, an index compiled once, and an oracle that reads the index.
The recursive bound, the seed, and the local front are removed.

Let `front(L)` be the front bound of lane L from the tail index, and 0 when it has none.
Under the field, `front(L)` also includes rule 1 of "Small pods of a mixed pair" below: a curved lane with no geometry tail gets `min(6 / (c (1 - conflictSlack)), 16.2 m)`.
The anchor front of L is `B(L) = max(6 m, front(L))` (`anchorFront`, `internal/sim/envelope_reach.go:276`).
It does not take the front of another lane.

An anchor is a lane and a position on it.
The envelope of an anchor on L at position p has these items, each a closed range of p:

- L itself, over the whole lane.
- Behind: each lane P whose To node is at walk distance d back from the From node of L, while `p <= (tail(P) - d) + u`, with the tail at least 20 m.
- Origin: each berth at a node N at walk distance d back, and each lane at N, while `p <= (originAt(N) - d) + u`.
  These items carry N as their origin unit.
- Ahead: each lane M out of a node at walk distance d from the To node of L, while `p >= ((length(L) + d) - B(L)) - u`.

u is `envelopeReachSlack`, 1 mm.
The walk distance is the least sum along lanes, added left to right in float64 (`reachWalk`).
A walk takes each node with `d <= fl(budget + u)`.
The operands are added in the order of the formulas above, so a test can compute each bound to the bit.

A published position (L, p) is the pod position that the protocol and the saved state carry: `Pod.LaneID` and `Pod.LaneDistance`.
A restore puts the route distance within two `restoreTolerance` steps, 2e-6 m, of the published position, so the pod can be on another lane across a node.
The anchors of (L, p) are each anchor within walk distance u of it, also across one or more nodes and short lanes.
The envelope reach of (L, p) is the union of the envelopes of these anchors, each with its own B and its own distance origin:

- The anchor on L at p.
- While `p <= u - d`, for each lane A into a node at walk distance d back from the From node of L, the items of the anchor on A along the paths through L that hold a position in `[max(0, length(A) - u), length(A)]`.
- While `p >= (length(L) + d) - u`, for each lane A out of a node at walk distance d from the To node of L, the items of the anchor on A along the paths through L that hold a position in `[0, min(u, length(A))]`.

Each neighbor term uses the items of the anchor on A only, and not the union at A, so the union does not recurse (`localItems`, `:628`).

Rule 2 of "Small pods of a mixed pair" applies to each span: when a span holds a maneuver group lane, it also holds each lane at a node of its cluster (`closure`, `:407`).

`compileEnvelopeReach` (`:327`) builds the spans of each lane and each berth one time, in `newPreparedNetwork`, when the network sets `shapeClearance`.
The spans are immutable after that.
The audit builds its own index with the same code.
A berth span holds the berth and each lane at its node, with the node as origin unit, and rule 2.

#### Paths through the published lane

Let near be the nodes within walk distance u of L on the side of A: back from the From node of L when A is before L, and ahead from the To node of L when A is after L.
The items of the anchor on A along the paths through L are these (`throughItems`):

- A, L, and each lane from a node of near to a node of near.
- When A is before L: the behind and origin items of A, from the From node of A, as in the envelope of an anchor.
  The ahead items of A come from the walk ahead from the To node of L, with the walk distance `(d + length(L)) + w`, where w is the least sum from the To node of L.
- When A is after L: the ahead items of A, from the To node of A, as in the envelope of an anchor.
  The berths and the lanes at each node of near with a berth, with that node as their origin unit, over the whole lane.
  The behind and origin items of A come from the walk back from the From node of L, with the walk distance `(d + length(L)) + w`.

So the walk of A on the side of L starts at the far end of L, and does not branch at a node of near.
A lane of a diverge or a merge at a node of near, other than L and the lanes between near nodes, is not an item.

Soundness.
The premise is that the published position (L, p) is a position on the route, and that the anchor, the route distance, is on the route at most u from it.
`publishVehicleTravel` and `publishDeparture` (`internal/sim/traffic.go:846` and `:838`) set `Pod.LaneID` and `Pod.LaneDistance` from the route lane of the block of the route distance, so after them the two positions are the same.
A restore takes the lane of the saved route index, refuses a saved lane ID that differs, and derives the route distance from the saved lane distance, with the saved distance only within `restoreTolerance` (`placeTravelingPod` and `restoredDistance`, `internal/sim/state_physical.go`).
So after a restore the gap is at most two `restoreTolerance` steps, 2e-6 m.

One case does not publish.
A pod with no destination that reaches the end of its route returns at `traffic.go:864`, after it sets the route distance and before it publishes.
`blockIndex` can then pass one or more short last lanes, and the published position stays where the last publication put it, on L or on a lane before the last lane.
The gap is at most the last step of the pod:

- A step reaches its limit only by the endpoint snap: the step is `min(available, command dt)`, the command is at most `safe(available)`, and `safe(a) dt < a` for each a > 0 (`moveStep`, `internal/sim/move_step.go:16`).
  The snap sets the limit when less than 1e-5 m is left (`move_step.go:28`).
- So the last step a satisfies `a - safe(a) dt < 1e-5 m`, with `safe(a) = sqrt(b^2 dt^2 + 2 b a) - b dt`, `dt = 1/30 s` (`TicksPerSecond`, `internal/sim/simulation.go:11`), and b the maximum braking: 2 m/s^2, or 1.15 m/s^2 for Express (`motionEnvelopeFor`, `internal/sim/motion_envelope.go:23`).
  `a - safe(a) dt` grows with a, so a is less than 2.21e-4 m for b = 2 and 1.70e-4 m for b = 1.15.
- The pod stops at the route end with speed 0, so the gap does not grow after that step.
  A restore of the pod derives its route distance from the published lane distance, so the gap after a restore is at most 2e-6 m.

So the gap is at most 2.21e-4 m, less than u, also when the step passes several short lanes.
The anchor is on the route, ahead of the published position, and the lanes between them are on the route, so the argument below holds with this gap.
So when the anchor is on A before L, the route goes from A to L through lanes whose nodes are at most u from the From node of L, which are nodes of near, and then on through L.
The lanes of the route ahead of the anchor are A, the lanes between near nodes, L, and the lanes after the To node of L.
The route distance from the anchor to a node after L is at least `d + length(L) + w`, so the ahead item of each such lane starts no later than the item of the route.
When the anchor is on A after L, the same holds for the lanes behind.
An origin unit at a node of near is held over the whole lane, which is more than the definition needs.
So each lane that the envelope of the anchor holds on the route of the pod is an item along the paths through L.
A lane that is not on these paths is not on the route of a pod published on L, and the route selection of the oracle drops it in each case.
This argument uses only the published lane, and not the route, so the audit index, which has no route, uses it too.
Two pods published on lanes L1 and L2 have their routes through L1 and L2, so the audit pairs only items of paths that the two pods can have.

The review proposed a path ID on each item, a check at selection that the path is a contiguous part of the route, and an audit that pairs only items with compatible paths.
That rule keeps each item in the index.
The rule of this section evaluates the provenance one time, at compile time, for the published lane, so the index needs no path IDs, and the audit needs no pairing rule of its own.
The index is then not a superset of the index without the rule.
The oracle selects from a smaller span, so it can lose a lane that only the removed walks reached, also a lane of the route.
For example, with a route that comes back to a diverge in a loop and takes the sibling later, the span at the start of L no longer holds the sibling (`TestEnvelopeAnchorDiverge`).
That lane is not within the tail or the front of the anchor along the route, so the envelope does not need it.
On the network of section 2.1, the oracle holds 146,221 planes for large pods, as before the rule, and 3,835,568 for small pods, 2.196 for each pose as before.
This is a measurement.
`TestEnvelopeAnchorRouteCoverage` checks the soundness argument with a construction that does not use the walks: it measures the tails and fronts along the route only.

Cost.
The rule adds no storage to an item and no work in the oracle.
At preparation, each anchor A next to L walks one time on its own side and one time from the far end of L, in place of two walks from A.
The set near comes from the walk of the slack, which the union already runs, and the lanes between its nodes take one pass over near with a mark for each node.
On the network of section 2.1, the compile takes 39 ms for 27,815 spans with 136,303 locations, against 45 ms for 37,694 spans with 200,303 locations without the rule.
On a chain of 150 lanes of 0.01 mm, each lane has at most 150 near nodes and 150 anchors, so a lane takes at most 150 walks and a pass of 150 nodes with their lanes.
`TestEnvelopeAnchorShortChain` builds that chain in 0.37 s with the race detector off.
The walks and the order of the items are those of the envelope of an anchor, with the same float64 additions, so the index is deterministic.

#### Endpoint rounding

Each bound rounds outward, and each end of a range has one owner:

- An item is closed at both ends.
  The code stores the end as `math.Nextafter(to, +Inf)`, exclusive.
- The cuts of the spans are the item bounds in `(0, length]`.
  So a bound at the length of the lane makes a last span `[length, length]` when an item starts there.
- The walk limit is `math.Nextafter(budget + u, +Inf)`, exclusive (`inclusiveLimit`, `:488`), so a node at exactly `fl(budget + u)` is in the walk.
- The neighbor terms use the same closed form: `p <= u - d` and `p >= (length(L) + d) - u`.
- A walk from the far end of L adds the base `d + length(L)` to each least sum w, as `(d + length(L)) + w`.

#### The oracle

Without the field, the oracle and its reference construction do not change, so D and each other project keep their envelopes.

With the field, `appendVehicleSafetyLocations` (`internal/sim/envelope_safety.go:33`) reads the compiled span at the published position and selects from it (`appendSpanEnvelope`, `:152`):

- A traveling pod reads the span of `Pod.LaneID` at `Pod.LaneDistance`.
  The route distance and the route prefix do not change the result.
- A pod at a berth reads the span of its berth.
- A lane is selected when it is a lane of the route of the pod, by lane identity.
- A lane or a berth with an origin unit is selected when that unit is the origin node of a traveling pod before its origin release, or the node of the berth of a pod at a berth.
  The berth must also be the origin berth or the berth of the pod.
- Rule 2 then applies to the selected planes (`appendClusters`).

The planes come in the order of their location numbers, each plane one time, then the clusters.

#### Why containment is structural

The oracle takes its planes from the span of the index at the published position.
Selection keeps a subset of the span, and rule 2 of a subset is a subset of rule 2 of the span, which the span holds.
So each plane of the oracle is in the index, by construction, at each position and with no proof about walks or seeds.
The audit index runs the same code on the same network, so it holds the same spans.

Two checks guard the code that this argument does not cover:

- The reference construction of SX3 (`newEnvelopeReference`, `internal/sim/envelope_safety_reference_test.go:52`) computes the envelope again from the definition above.
  It reads no span, item, or bound of the production index.
  It walks by node ID with its own shortest sums, and takes the lane lengths from `lanePointsBetween`.
  It must give the same locations in the same order as the oracle.
- The support tests of the next sections put oriented bodies at poses and require each lane under the body in the oracle.
  They do not use the index.

#### Axiom A1

The maintainer accepted A1 as a stated rule of the model on 2026-11-16.
A1 applies only when the network sets `shapeClearance`.
A network without the field, project D included, keeps its envelope and the requirements of that envelope.

A1: two bodies do not overlap when each lane under each body has a nonempty separation group, each lane under one body is in a different group from each lane under the other body, and no lane under one shares a node with a lane under the other.
This is the test of `safetyLocationsSeparated` (`internal/sim/safety.go:347`), applied to each cross pair of the lanes under the two bodies.

The network stores no level.
"Different groups declare that unrelated paths can cross at different physical levels" ([London](../london.md), lines 135 to 137).
Validation does not check the groups against the geometry, so A1 is a declaration by the author.
For a body on one lane, A1 is the rule that `pair` applies to each small pair today (`internal/sim/safety.go:277`, with `safetyLocationsSeparated` at `:347`).
For a body that straddles a node, A1 is the reason that D1-B adds the lanes ahead and behind.

The lanes under a body are its support.
Let D(x) be a disk of radius r around the lane point at route distance x.
The support C(x) is the connected part of the route that is in D(x) and holds x.
The proof obligation is that, for each route lane M that meets C(x), the envelope holds M.
The proof uses the anchor of the pod only, on L at the position of x.
The other anchors of the uncertainty can only add planes.
The route distance x is within 2e-6 m of the published position, and each item bound has the slack u of 1e-3 m, so the anchor at the published position holds each item that the anchor at x holds.
A part of the route that leaves D(x) and comes back is not in the support.
If such a part is on a plane that is separated from each support lane, A1 puts it on another level.
If it is on a plane that is not separated from the support, the oracle tests the two pods through the support.

The counterexample of the review (section 10, item 1) is not a plane under the body.
There, a straight lane L ends 40 m ahead of the reference, the next lane turns back, and its To node Q is next to the pod in plan.
The support is in L, because it ends r ahead of x on a straight lane.
When L and a lane out of Q have nonempty groups that differ, the lane out of Q is on another level under A1, and a small pod on L is exempt against it today at any plan distance.
The built oracle holds that plane only when a lane of the route has a front of 40 m or more.
So the bound of the whole network holds it only by over-approximation.

Without A1, a body touches each plane whose track comes into D(x) in plan, also after the route leaves D(x).
A route can come back after any path distance, so no front of path distance holds such a plane, and the route maximum does not either.

#### Why the oracle holds the body

Facts about lanes:

- F1: a lane is straight, or a quadratic curve that `lanePointsBetween` samples with 65 points (`internal/sim/network.go:213`).
  In the frame of the parabola axis, the coordinate across the axis is linear in the curve parameter, and the coordinate along the axis has one minimum.
- F2: a lane that admits a large class is at least 40 m long (`LaneMinimumLength`, `internal/sim/profile_geometry.go:36`).
  Preparation applies this to each lane outside a maneuver group (`internal/sim/prepared.go:185`), and a maneuver group lane admits only Compact.
- F3: each lane on the route of a large pod admits its class, by the compatibility rule of the [service contract](../contracts/service-contract-proposals.md), line 92, and by the class test of the route search (`classTestFor`, `internal/sim/network.go:320`).
- F4: for a lane with a tail, `front = 6 / (c (1 - conflictSlack))`, or 6 m when the lane is straight (`profile_geometry.go:83`).
  c is the minimum projection of a segment on the chord of the lane, and `conflictSlack` is 1e-9.

Bounds, for a disk of radius r:

- B1: a whole lane in the disk is at most (2 + 2√2) r long.
  The length is at most the change across the axis plus the total change along it.
  Each end gives at most √2 r, and the minimum along the axis is at most r below the center.
- B2: an arc of one lane from x to an end of the lane, in D(x), is at most (2 + √2) r long, by the proof of B1 with one end at the center.
- B3: an arc of lane L from x to a node at most r from x is at most r / c long, because each segment of L advances at least c times its length on the chord axis.

For a large pod, r is ρ = 5.404 m.
The 10 m by 2.5 m gate body of SC6 has a half-diagonal of 5.154 m, and the guideway tolerance adds 0.25 m.
D(x) then also holds the grown large body of the shape rule, 8 m by 4 m, and the 6 m by 2.5 m class body.
B1 gives 26.09 m, and B2 gives 18.45 m.

Lemma: the support of a large pod meets at most one node.
If C(x) meets two nodes, it holds the whole route lane between them.
That lane is at least 40 m long (F2, F3), and a lane in D(x) is at most 26.09 m long (B1).
So the support is L, and at most one of these:

- The next route lane M, when the support passes the To node of L.
  The arc from x to that node is at most ρ / c long (B3), which is less than `6 / c`, which is at most `front(L)` (F4).
  So `length(L) - p <= B(L)`, and the ahead item of M, with d = 0, holds p.
  M is on the route, so the oracle selects it.
- The previous route lane P, when the support passes the From node of L.
  The behind item of P holds p while p is within max(20 m, tail(P)) of the From node.
  The arc is at most 18.45 m long (B2), which is less than 20 m, so the item holds p, and the oracle selects P.

A join turn does not change the lemma, because B1 does not depend on the angle at a node.
A cycle or a returning path does not change it, because the support meets one node at most.
The origin tail is at least 20 m, so it holds the origin berth over the whole behind support before the origin release.
The proof does not use the bound of the whole network, or the front of another lane.

Scope: the proof covers a body inside the disk of radius 5.404 m.
For a body that fills the whole 6 m envelope disk, B2 gives 20.49 m behind, which is above the 20 m floor of the behind walk.
SC6 tests the envelope with the 10 m gate body, which is inside the 5.404 m disk, so the proof covers the gate of SC6.

#### Small pods of a mixed pair

A small pod has an envelope when the fleet has a large pod (`largeSafety`, `envelope_safety.go:13`).
The proof uses the grown small body of the shape rule (`classShapeBody`, `internal/sim/shape_pairs.go:45`): 3.0 m behind, 4.2 m ahead, and 1.55 m in half-width.
Its corner radius is 4.4769 m, and the guideway tolerance adds 0.25 m, so r = 4.7269 m.
This disk also holds the small body of the sweep, whose corner radius is 3.85 m.
B1 gives 22.82 m, and B2 gives 16.14 m.

| Case | Status |
| --- | --- |
| The support meets two nodes on lanes of 24 m or more | Cannot occur: a lane in the disk is at most 22.82 m long (B1). A small lane outside a maneuver group is at least 24 m long, unless it has a reduced retention (next row). |
| The support meets two nodes, and the first lane wholly in D(x) has a reduced retention | Cannot occur, by the validated window of such a lane (below). |
| The support meets two nodes on maneuver group lanes | Held by rule 2 below. Without it, this is a gap: these lanes need only a positive length. |
| The support passes the To node, and the lane has a tail | Held: B3 gives at most `front(L)`. |
| The support passes the To node, and the lane is straight with no tail | Held: the arc is at most 4.73 m, under the 6 m floor. |
| The support passes the To node, and the lane is curved with no tail | Held by rule 1 below. Without it, this is a gap when c is under r / 6, about 0.79. |
| The support passes the From node | Held: B2 gives at most 16.14 m, under the 20 m floor of the behind walk. |

The row of lanes with a reduced retention uses the validated window of such a lane (`newLaneRetention`, `internal/sim/retention.go:92` to `:112`).
A depot chain lane shorter than 24 m also has a reduced retention, because `chainMinimum` gives the full minimum to each other lane.
A lane M with a reduced retention is straight, and it is at least R long, where R is at least 5 m (`reducedRetention`, `retention.go:228`).
The window of M holds the lane before it and each lane up to 24 m after its end, and each node of the window has one lane in and one lane out.
The total turn of the window, with each join turn and each curve (`turnSum`, `internal/sim/platoon.go:109`), is at most 90 degrees.
So no segment of the window points against the direction of M.

Let M be the first route lane ahead of L that is wholly in D(x).
Then L is the lane before M, and the arc of L from x to the start of M is in D(x), so it is at most 16.14 m long (B2).
That arc is in the window of M, so it does not move back along the direction of M.
So the end of M is at least the length of M, 5 m or more, from x along that direction, and it is outside D(x).
M is not wholly in D(x), which contradicts the choice of M.
Behind, the same argument holds for the first lane P behind L that is wholly in D(x), because the window of P holds the 24 m after its end, and so the arc of L up to x.
This is a tighter bound from the validated geometry, so this design does not raise the front of these lanes.

This design proposes two rules, which the oracle and the index apply under the field:

1. A curved lane with no geometry tail gets the front `min(6 / (c (1 - conflictSlack)), 16.2 m)`, and 16.2 m when c is 0 or less.
   The arc from x to the To node is in D(x), so it is at most r / c long (B3) and at most 16.14 m long (B2).
   That is at most `B(L)`, so the ahead item of the next route lane holds the position.
2. When the envelope holds a maneuver group lane, it also holds each lane at a node of its cluster.
   A cluster is a maneuver group with each group that shares a node with it, and so on.
   If the support meets two nodes, each lane between them is in D(x), so it is a group lane or a lane with a reduced retention.
   The first lane past L is held by the other rows, or by this rule when L is a group lane.
   When it is a group lane, the next lane starts at a node of its cluster, so the rule holds that lane.
   A group lane that shares a node with a held group lane is in the same cluster, so the argument repeats along the support, ahead and behind.

A pod at a berth has the berth envelope: the berth and each lane at its node.
A lane at the node that is wholly in D(x) is at most 16.14 m long (B2).
So it is a maneuver group lane, which rule 2 covers, or a straight lane with a reduced retention.
A lane with a reduced retention is at least 5 m long, so its other node is outside D(x).
So the support of a body at a berth holds a second node only through a maneuver group lane.

Rule 2 applies to each location of each kind of envelope:

- A traveling pod: the lanes of the route that the span holds.
- A pod before its origin release: the origin berth and the lanes at its node, as today, and the cluster of each group lane among them.
- A pod at a berth: the berth and the lanes at its node, and the cluster of each group lane among them.

The index applies rule 2 to each lane span and to each berth span.

The first draft proposed another rule for maneuver group lanes: hold the next route lane while each node up to it is within 6 m of the reference.
It is not measured, and its test of plan distance has no form in an index of path distances (inferred), so this design does not use it.
The proof does not cover a support that passes a maneuver group lane and then a lane with a reduced retention, both wholly in D(x).
There, the window of the second lane need not hold x, and the cluster of the first lane does not hold the lane after the second.

On the network of section 2.1, rule 1 applies to 288 curved lanes with no geometry tail, and 60 of them get the cap of 16.2 m.
Rule 2 applies to 312 maneuver group lanes, with 328 lanes at a node of a cluster.
The network has 48 lanes with a reduced retention, and the shortest is 6.00 m.
With the two rules, `B(L)` is 6.01 m or more on 243 lanes of the network of section 2.1, on 243 lanes of section 3.5, and on 241 lanes of section 3.6.
Without the rules, it is 6.01 m or more on 3 lanes of the networks of sections 2.1 and 3.5, and on 1 lane of section 3.6.
The audit with the index for each lane gives the same refusals with and without the rules (section 2.2 and the effect table below).

#### Measured containment

The containment test of section 8 measures the proof on sampled poses.
It does not replace the proof, and its poses are samples.

The test puts oriented bodies at poses along each lane and across each join turn.
The poses are each 0.5 m of the first and last 16 m of a lane, the middle, and each position at which the oracle or the index adds or drops a plane.
Each body is also moved by the guideway tolerance, 0.25 m, to each side.
At each pose, the test reads the oracle at each published position within two `restoreTolerance` steps each way, also on the lane next to the pose across a node.
The bodies are the gate body and the grown large body for a large pod, and the small body of the sweep and the grown small body for a small pod.

For each pose, the test computes the support of each body: the connected part of the route polyline in the body footprint that holds the reference.
It requires each support lane in the oracle at each published position.
It also requires each plane of the oracle in the span of the index at that position, and on the small networks it requires the oracle to equal the reference construction.
It also classifies each route lane off the support that enters the body.
Either the oracle tests the lane through a plane of the envelope that is not separated from it, or A1 declares the lane separated from each plane of the envelope.

Each route window is long: at least the retained tail behind and the anchor front plus 60 m ahead, and up to 256 lanes.
A route does not use a lane two times, so a route cannot go on at a node whose lanes that admit the class are all on the route.
A support that reaches the end of a window where the route can go on is a failure.
A support that reaches the held origin node of a pod before its origin release is counted, and not checked further, because the envelope holds each lane at that node.

The test also drives each route out of each berth with the origin held up to the origin tail of its node.
It checks each berth with a stationary pod: the berth envelope in the index, and the body at the berth node at the heading of each lane at the node.

The test runs on these networks:

- The four turn-back shapes of the disk study (hairpin, triangle, loop, and bridged loop), with small lanes and with large lanes.
- A hook fixture of small lanes.
- The curves network of `TestEnvelopeReachCurves`.
- The networks of sections 2.1 and 3.5.

On the London networks, each large lane gets windows of at least 1,100 m behind and ahead, so that the route maximum of the earlier oracle can come from far lanes.
The test fails on a failure of the proposal, on a control under its minimum count, and on a coverage count under its minimum.

Results:

- Failures of the proposal: 0 on each network, for each of the four bodies.
  No support lane is missing from the oracle, no plane of the oracle is missing from the index, and no support reaches the end of a window where the route can go on.
- The support of a large body never meets more than one node.
  On the network of section 2.1, it meets one lane at 130,050 body poses and two lanes at 89,394 body poses of the gate body.
- The support of the small body meets more than one node at 17,593 body poses with a maneuver group lane, and at 252 body poses with no maneuver group lane.
- Off-support lanes in the gate body on the network of section 2.1: 624 body poses where A1 declares the lane separated, and 84 where the oracle tests it through the envelope.
  An example is a pod on the Bank road `road-in-08` whose route comes back on `road-out-08`.
  On the network of section 3.5, the counts are 384 and 264.
  The loop and bridged loop shapes give 72 and 69 such body poses under A1, and the hairpin gives 75 that the oracle tests.
- Poses on the network of section 2.1: 73,148 large and 1,746,917 small.
  Of these, 345 large and 44,161 small poses have the origin held.
  The test also checks 7 large berths and 770 small berths with a stationary pod.
  At 192 body poses at a berth, for example at `940GZZDLTWG-bay-1`, the support of a small body meets a second node through a maneuver group lane, and rule 2 holds it.
  On the network of section 3.5, the poses are 81,942 large and 1,749,752 small.

Controls show that the test can fail, and the test requires each control to fail at least once.
Each control is an oracle that reads an index with one part of section 3.1 removed:

| Control | Misses |
| --- | --- |
| Oracle without the small-pod rules, small body | 40 support misses on each London network, at the Docklands Light Railway bays of `940GZZDLTWG`, through maneuver group lanes, and 6 on the hook fixture, on the curved lane |
| Oracle without the small-pod rules, grown small body | 384 support misses on each London network, and 12 on the hook fixture |
| Oracle with the anchor front 6 m on each lane, both small bodies | 6 and 12 support misses on the hook fixture |
| Berth envelope without rule 2, both small bodies | 528 support misses on each London network, for example at `940GZZDLTWG-bay-1` |

The controls without the small-pod rules miss less than in the first build of slice 1, 1,136 and 4,192, because the anchors at the nodes hold more lanes of the bays.
The test also counts the misses of the earlier oracles, without a minimum:

| Earlier oracle | Misses |
| --- | --- |
| Oracle with the route maximum, the oracle without the field | 1,136 support misses of the small body and 3,736 of the grown small body on each London network, and 8 and 15 on the hook fixture |
| Berth envelope without the field | 528 support misses of each small body on each London network |

On the London networks, the small bodies miss no support lane on a curved lane with no tail, with or without the rules.
The hook fixture has a lane of 35.1 m with c = 0.00, so it gets the cap of 16.2 m.

The test also counts the planes of the oracle at each pose, which `Check` tests.
The mean count for each pose:

| Network | Pod | Route maximum | First build of slice 1 | Section 3.1 |
| --- | --- | ---: | ---: | ---: |
| Section 2.1 | Large | 1.997 | 1.989 | 1.999 |
| Section 2.1 | Small | 1.913 | 2.193 | 2.196 |
| Section 3.5 | Large | 1.712 | 1.705 | 1.712 |
| Section 3.5 | Small | 1.905 | 2.184 | 2.187 |

For a small pod, the small-pod rules add most of the planes.
For a large pod on the network of section 2.1, the oracle holds 146,221 planes over 73,148 poses, and the route maximum 146,093.
The oracle drops the planes of the route maximum past the anchor front, and the slack of 1 mm on each bound and the anchors at the nodes add route lanes that the route maximum holds only 1 mm later (inferred).

#### Effect

| Network | Front bound | Refusals | Under 4.0 m | Shape pairs |
| --- | --- | ---: | ---: | ---: |
| LondonFull, Express on the route | Whole network, 66.42 m | 14 | 10 | 40 |
| LondonFull, Express on the route | Each lane, first build of slice 1 | 7 | 5 | 39 |
| LondonFull, Express on the route | Each lane, section 3.1, with or without the small-pod rules | 7 | 5 | 39 |
| Section 3.5, all changes | Whole network, 33.62 m | 7 | 5 | 39 |
| Section 3.5, all changes | Each lane, first build of slice 1 | 0 | 0 | 38 |
| Section 3.5, all changes | Each lane, section 3.1, with or without the small-pod rules | 0 | 0 | 38 |
| Section 3.6, the fallback | Each lane, section 3.1 | 0 | 0 | 23 |
| Section 3.6, the fallback | Whole network, 7.96 m | 0 | 0 | 23 |
| Section 3.6, the fallback | Whole network with the small-pod rules, 16.20 m | 4 | 2 | 21 |

The wide network gives the counts of the network of section 2.1.
The ramp network gives 14 refusals with the first build of slice 1 and with section 3.1, with 14 shape pairs (section 2.5).
The index for each lane is not a subset of the index of the whole network any more: on 8,760 lanes of the network of section 2.1, a span holds a plane that the whole index does not hold at that position.
These are the planes of the anchors at the nodes.

#### Approved texts that change

For large pods, S1-A does not weaken a safety property that an approved text proves.
The planes that it drops are outside the support of the body, under A1.
For small pods, the rules add planes that the route maximum held only by chance.
S1-A does weaken the literal claims that the plane exemption does not change, and it changes the `Check` oracle under the field.
Project D does not set the field, so its E rows do not change.

These approved texts need an amendment, which slice 0 of section 7 proposes:

| Text | Why |
| --- | --- |
| [Group physics design](group-physics-design.md), lines 79 to 80: "The route window of a pod can reach further than an incident lane, because the front bound is the largest `6 * projection bound / 20` of the lanes of the route" | True only without the field. |
| Group physics design, line 86: "It includes the lanes ahead on the route, up to the largest front bound of the lanes of the route, also more than one lane ahead." | The reach takes `F'(L)` under the field. |
| [Service contract](../contracts/service-contract-proposals.md), line 75: "When the fleet has a large pod, the oracle keeps the envelope of each pod, so the plane exemption of two pods does not change." | The envelope changes under the field. |
| [Shape clearance](shape-clearance.md), line 147: "while the envelope takes the largest front bound of the route" | True only without the field. |
| Shape clearance, line 524, section 3.4, item 5: "Under D1-B, the planes of each pod are those of today's envelope, so the plane exemption does not change." | The envelope changes under the field. |
| Shape clearance, lines 542 and 543, the rows of `pair` and `largeSafety` in section 3.5: "the envelopes stay" and "no change" | `appendVehicleSafetyLocations` changes under the field. |
| Shape clearance, lines 137 to 139: "A berth has its own plane and the planes of the lanes at its node. The envelope adds only the lanes of the route of the pod (`envelope_safety.go:55` to `:68`), so the rule does not take the other lanes at these nodes." | Under the field, rule 2 adds each lane at a node of the cluster of a held group lane, also lanes off the route. |
| Group physics design, line 88: "A berth has its own plane and the planes of the lanes at its node." | Under the field, the berth envelope and its span in the index also hold the clusters of the group lanes at the node. |
| Shape clearance, line 762, the D1 row: "It keeps every plane that the oracle checks today" | S1-A drops planes of the route maximum. |
| [Standees contract](../contracts/express-standees-contract-proposal.md), line 313: "the envelope reach of the audit holds the largest front bound of each route through a lane, so the audit covers that case" | Under the field, the oracle no longer has that window, so the case does not occur. |
| [Shape clearance amendment](../contracts/shape-clearance-amendment-proposal.md), lines 294 and 743 | These record the applied SC9 text and its open question 1. They get a dated note. |

These texts stay true:

- Group physics design, line 69: "a front bound that encloses the body".
  Section 3.1 proves it for a body inside the disk of the proof.
- Group physics design, line 94: the gap of the route window stays only in a network without the field.
- [Express physical contract](../contracts/express-physical-encoding-contract-proposal.md), line 486, the Physical geometry gate, and line 541, SC6.
- Shape clearance, line 62: "to the front bound ahead of it".
  The anchor front is a front bound.
- Shape clearance, line 136: "in its last 6 m, or the longer front bound of the lane".
- Standees contract, lines 291 and 309 to 312, as records for a network without the field.

The code comments of `envelopeReach` and `reachFront` (`internal/sim/envelope_reach.go:27` to `:30`, `:212` to `:216`) change in slice 1.

The [shared-track amendment proposal](../contracts/shared-track-amendment-proposal.md) applied the text of these changes on 2026-11-16.
That text defines the local front, `F(L)`, the seed, and the reach bound, which the change of 2026-11-16 to this section removes.
The [second shared-track amendment proposal](../contracts/shared-track-amendment-2-proposal.md) quotes each applied text that the change makes false, and gives its replacement.

### 3.2 Hub shape

Options:

- A: the tight shape with each part at 41 m at the two hubs: side, throat to entry, entry to the first row, and pitch.
  Each access lane is then 42.45 m, and each berth row lane and row link 41 m.
  Measured: no hub lane is short, and the layout audit of the London generator finds no conflict.
  A part of exactly 40 m gives lanes of 40.0 m that preparation refuses by rounding, so 41 m is the value.
  Cost: the paths from the diverge to the berths grow from 75.5 to 175.5 m today to 124.4 to 288.4 m at Waterloo, and from 75.5 and 100.5 m to 124.4 and 165.4 m at Bank.
- B: the wide shape at scale 0.5 at the two hubs, with a hub pitch of 82 for berth links of 41 m.
  It is not measured on the tight network, and its path to the berths is longer than A (inferred from section 2.7 of the shape clearance design).
- C: D4 at 14 m, with a lane minimum of 28 m.
  The tight lanes of 25 m stay short, so C does not remove the blocker.

Recommendation: A.

### 3.3 Waterloo

Link 100 and link 350 leave Waterloo about 23 degrees apart, and their portals interleave on the junction circle (inferred from the node positions, not from a study test).
So the lanes of the two links cross near the portals, and `move-05-01` is a reversal between adjacent portals.

| Refusal or blocker | Change | Measured |
| --- | --- | --- |
| `road-in-01`, 34.3 m | Throat of Waterloo at 90 m from the junction center, in place of 80 m | `road-in-01` 43.9 m. The other Waterloo roads get 5.8 m to 10.0 m longer. `road-out-02` becomes 39.8 m, and it is off the route, so it needs 24 m only. |
| `road-in-05` and `link-100-ab-2` | Curve `road-in-05` with a control point 30 m from the middle of its chord, on the side away from the junction center | Refusal removed. `road-in-05` is 75.8 m with the curve and the throat at 90 m, against 58.7 m today. With the throat at 90 m and no curve, the pair is 0.55 m apart. Offsets of 20 m and 40 m away from the center, and of 30 m and 40 m toward it, also remove it. 10 m away leaves 5.21 m, 10 m toward leaves 0.00 m, and 20 m toward leaves 2.70 m. |
| `link-350-ab-2` and `link-100-ba-1`, `move-05-05` and `link-100-ba-1`, `road-out-01` and `link-350-ab-2` | Remove `move-05-01` | All three removed. No free-flow station route uses the lane, so no route gets longer. Emergency and depot routes are not measured. |

With the curve, `road-in-05` has a front bound of 7.96 m and a tail of 20.75 m.
The other option for the throat is a control point on `road-in-01`.
It is not measured, and a curve on a large lane gives it a front bound and a tail over the straight values.
The other option for the crossings is a resource that orders the two pods.
None exists for two lanes that share no node, and it would hold Compact pods on link 350 and on the roads.
Recommendation: the three changes of the table.

### 3.4 Bank

The Bank throat is next to link 098: the line of `link-098-ab-1` passes over the diverge, and the end of `link-098-ba-2` is 4.55 m from `road-in-08`.
So the station must turn, and `move-08-06` must go.

The study turns the station about the junction center, with the hub shape of section 3.2 and `move-05-01` removed:

| Turn | Bank junction lanes removed | Bank refusals |
| ---: | --- | --- |
| 0 degrees | None | 3 |
| 0 degrees | `move-08-06` and `move-06-08` | 2 |
| +5, +10, or +15 degrees | None | 1: `link-098-ab-1` and `road-in-08`, 0.00 m |
| +5, +10, or +15 degrees | `move-08-06` | 0 |
| -5 to -20 degrees, and +20 degrees | None | 1 to 4 |

A positive turn increases the heading of the station axis, which is -126.0 degrees today, so the axis turns away from link 098 and toward link 097 (inferred from the node positions).
At +20 degrees, `road-out-08` and `london-link-097-ba-2` refuse.
Each row uses hub parts of 41 m.
The heading search of the generator uses steps of 5 degrees, so +5 degrees is one step.
Removing `move-08-06` makes 7 more station routes longer, and the worst route becomes Waterloo to St. Paul's, at +21.4 s with the other changes (section 3.5).
The other option is an ordering resource, as at Waterloo.
Recommendation: turn Bank by +5 degrees, and remove `move-08-06`.

### 3.5 All changes together

The network of this section has these changes, on the network of section 2.1:

1. The hub shape of section 3.2 at Waterloo and Bank.
2. The Waterloo throat at 90 m.
3. Bank turned by +5 degrees.
4. `road-in-05` with its control point.
5. `move-05-01` and `move-08-06` removed, which leaves 9,022 lanes.

Its network SHA-256 is `4e8503da2e9a4778b7f1310fcba0729dc8c160c2d4055525662042a00df97597`.
Results:

- The audit with the index for each lane refuses no pair, with or without the small-pod rules.
  It has 38 shape pairs, as before the change of 2026-11-16.
  Before the paths through the published lane of section 3.1, it had 80, because the anchors at the nodes added the planes of sibling lanes.
- No route or hub lane is under 40 m, and preparation passes.
- The layout audit of the generator finds no conflict.
- `projectcheck` at `7bebe01e`, with the order contract `express-v1`, both Express services, and 4 Express pods at free hub berths, stops at the plane reach refusal of `move-01-06` and `road-out-01`.
  Project validation reports this refusal after each other check (`internal/project/config.go:335`), so each other check passes.
  The refusal comes from the bound of the whole network, 33.62 m from `move-06-08`.
  With the slice 1 code of 2026-11-16, `projectcheck` accepts the project, with one warning: `940GZZLUBNK-move-08-02` has a tail of 39.5 m on a length of 39.1 m, so it holds one pod.
- The routes are 2,216.0 m from Waterloo to Bank and 2,150.6 m back, about 45 m and 48 m more.

The cost to station routes, over the 97,032 pairs of station exit and station entry, in free-flow seconds:

| Step | Pairs longer | Worst | Total |
| --- | ---: | --- | ---: |
| Hub shapes, Waterloo throat, and Bank turn | 1,242 | +3.4 s, Bank to Waterloo | +1,891.7 s |
| And the curve of `road-in-05` | 1,242 | +3.4 s | +1,895.7 s |
| And `move-05-01` removed | 1,242 | +3.4 s | +1,895.7 s |
| And `move-08-06` removed | 1,249 | +21.4 s, Waterloo to St. Paul's | +2,027.9 s |
| And `move-06-08` and `move-08-02` removed (section 3.6) | 1,264 | +48.4 s, Waterloo to St. Paul's | +2,410.6 s |

No pair becomes unreachable, and no pair gets shorter.
These costs omit the longer paths inside the hubs (section 3.2), and the effect on wait and journey times is not measured.

### 3.6 Fallback without the index change

The maintainer approved S1-A, so this section is a record.
Without the change of section 3.1, the preset can also remove `move-06-08` and `move-08-02`.
The bound of the whole network then falls to 7.96 m, and the built audit refuses no pair, with 23 shape pairs.
With the same Express fleet as section 3.5, `projectcheck` at `7bebe01e` reports the project valid, with 9,020 lanes.
The network SHA-256 is `518049295f97c453ff4c09f18a33ee7afaff234179b7bbc4be56dbaff770b41f`.
The cost is 15 more station routes longer, and a worst case of +48.4 s.
The bound of the whole network grows with each curved lane near a large lane anywhere in the network.
The small-pod rules show this: with them, the bound of the whole network is 16.20 m, and the audit refuses 4 pairs, 2 under 4.0 m.
So the fallback cannot close the small-pod gaps of section 3.1 without new refusals.

### 3.7 Express parking site

This section measures the site of S4-A for slice 3 of section 7.
The measurements are at `c63a5741`, on the preset of slice 2 (network SHA-256 `4e8503da...`), with a comb and 16 Express pods added.
They come from a scratch probe that is not in the repository.
On 2026-11-16, the maintainer accepted parts of 41 m in place of the 40 m of D8-A, asked for the two wedges W1 and W2, and chose P4 when no wedge passed.

The comb is the parking of D (`addExpressParking`), with each part at 41 m: 16 rows, a through lane of 82 m, and berth lanes and row links of 41 m.
Its footprint is 82 m by 656 m.
With parts of 40 m, project validation refuses the project: `lane "xp-01-out" must be at least 40 meters long`.
The station is `express-parking`, parking only, Express only, in the separation group `xp-parking`, with the 16 Express pods at `xp-01` to `xp-16`.
Two lanes connect it, `express-parking-road-in-01` to its entry and `express-parking-road-out-01` from its exit.
Each road has its own separation group and the roles approach and exit, and the two roads do not cross.

The heading is the direction in which the rows follow each other from the through lane.
Waterloo has its junction center at (901, 456), and its berth rows go from y = 325 to y = 120 at a heading of -90 degrees.
Link 100 leaves Waterloo at a heading of -32.6 degrees.

The connections:

- Chain: from the arrival node of the last Waterloo berth, `940GZZLUWLO-05`, and to its departure node.
- Portal, as a London Parking facility: from the arrival portal of link 100 at Waterloo, and to its departure portal.
- Reversed portal: from the departure portal of link 100 at Waterloo, and to its arrival portal.

| Candidate | Entry | Exit | Heading | Connection | Validation | Refusals | Shape pairs | Layout audit, hard | Tail warnings | Parking gaps |
| --- | --- | --- | ---: | --- | --- | ---: | ---: | ---: | ---: | ---: |
| P1 | (942.0, 79.0) | (860.0, 79.0) | -90 | Chain | Valid | 0 | 38 | 137 | 1 | 0 |
| P2 | (1023.5, 73.6) | (976.5, 6.4) | -35 | Chain | Valid | 0 | 38 | 0 | 1 | 0 |
| P3 | (976.5, 6.4) | (1023.5, 73.6) | -35 | Portal | Valid | 0 | 41 | 1 | 1 | 0 |
| P4 | (1023.5, 73.6) | (976.5, 6.4) | -35 | Reversed portal | Valid | 0 | 47 | 1 | 1 | 0 |
| W1 | (805.9, 168.0) | (732.1, 204.0) | -116 | Reversed portal | Refused | 4 | 50 | 17 | 1 | 0 |
| W2 | (1307.7, 344.3) | (1278.3, 267.7) | -21 | Reversed portal | Valid | 0 | 45 | 102 | 1 | 0 |

Express route lengths in meters, from the node of `xp-01` and to it, with the node of the first berth of each hub:

| Candidate | To Waterloo | From Waterloo | To Bank | From Bank |
| --- | ---: | ---: | ---: | ---: |
| P1 | 4,858.5 | 4,858.5 | 2,626.0 | 2,560.6 |
| P2 | 4,980.2 | 4,911.2 | 2,747.6 | 2,613.3 |
| P3 | 4,824.6 | 4,929.8 | 2,592.1 | 2,631.8 |
| P4 | 654.0 | 693.0 | 2,951.9 | 2,925.6 |
| W1 | 543.4 | 634.8 | 2,841.4 | 2,867.3 |
| W2 | 615.0 | 681.5 | 2,912.9 | 2,914.0 |

Details:

- Validation is `projectcheck` with the 16 Express pods.
  Each candidate has 343 pods and 9,089 lanes.
  W1 is refused with its first refusal, `nonincident paths "express-parking-road-out-01" and "express-parking-road-in-01" are less than 20 meters apart`.
- The refusals and the shape pairs are from the audit copy of section 8 with the index of the code.
  `ValidateBankGeometry` refuses no pair of P1 to P4 and W2.
- The tail warning of each candidate is the warning of section 3.5 for `940GZZLUBNK-move-08-02`.
- P1 continues the axis of the Waterloo rows.
  From row 10, the comb crosses link 182 and Temple: the first hard conflict is `lane "xp-10-out" crosses guideway "london-link-182-ab-2"`, and the audit gives 137 hard conflicts and 1 soft conflict.
- P3 and P4 have one hard conflict each: `940GZZLUWLO-road-in-05`, with the curve of section 3.3, crosses the road of the comb that starts or ends at the arrival portal.
  The London layout audit exempts a crossing of two roads only for a Parking facility of `londonParkingFacilities` and its gateway station.
- With a chain, a pod at a Waterloo berth reaches the comb only through Bank, and a pod in the comb reaches a Waterloo berth only through Bank.
  The arrival row of Waterloo is the way into the comb, and the departure row is the way out.
- With the portal of P3, a pod reaches Waterloo through Bank.
  With the reversed portal of P4, a pod reaches Bank through the through lane of Waterloo.
- P2, P3, and P4 use one site at a heading of -35 degrees, between the Waterloo rows and link 100.
- W1 is in the wedge between link 183 and the Waterloo rows, on the middle of the wedge, 300 m from the junction center.
  From row 9, the comb crosses link 182, and the audit gives 17 hard conflicts.
  Its 4 refusals are its two roads, 1.70 m apart, its road out with `road-in-03` and `road-in-04`, 4.50 m and 4.82 m, and its road in with `london-link-100-ab-2`, 0.00 m.
  With the entry and the exit the other way round, validation refuses `express-parking-road-in-01` and `940GZZLUWLO-move-01-05`, and the layout audit gives 16 hard conflicts.
- W2 is in the wedge between link 100 and link 350, on the middle of the wedge, 420 m from the junction center.
  The comb reaches Southwark: the first hard conflict is `lane "940GZZLUSWK-access-in" is nearer than 36 meters to Express Parking lane "xp-06-arrival-link"`, and the audit gives 102 hard conflicts.
  At 250 m from the junction center, the comb crosses link 100 and link 350, with 102 hard conflicts.
  At both distances, with the entry and the exit the other way round, the two roads cross.
- Other headings in the three wedges are not measured.

No candidate passes with no refusal, no hard layout conflict, and no rule that the designs leave open:

- The designs do not give the connection of the comb to the hub.
  D8-A gives the comb, and section 2.8 of the shape clearance design measured it in place of the parking of D, with its trunks.
- P2 passes each audit, but it uses the berth rows of Waterloo as the way to another station.
  Then a pod on the arrival row can leave Waterloo through the comb with no stop at a Waterloo berth, against the comment of `entersBerth` (`internal/sim/parking.go`) that each way out of a station from its berth access lanes goes through a berth.
  An Express pod in the comb also needs about 5.0 km to start at Waterloo.
- P4 is the only candidate with a short route to Waterloo.
  It needs the comb as a Parking facility of Waterloo in the layout audit, or a change to the curve of `road-in-05`, and it gives each link 100 portal at Waterloo a second lane.
- The comb needs parts of 41 m, not the 40 m of D8-A.

The choice, by the maintainer on 2026-11-16: P4, with the comb as a Parking facility of Waterloo in the layout audit of the preset only (`sharedTrackLayoutInput`), and with no change to the curve of `road-in-05`.
The exemption is that of the London Parking facilities: two roads of the facility and of its gateway station can cross.
The audit input of each other London preset does not change.
The preset of slice 3 (`addSharedTrackParking`) gives these results:

- Network SHA-256 `652d75eb86ce9badf1f310a7a3744c1bc26425abbe4c4d7dcc231864255d23d5`, with 5,790 nodes, 9,089 lanes, and 343 pods.
- The construction before the parking keeps the network SHA-256 `4e8503da...` of section 3.5.
- No refusal and 47 shape pairs, the tail warning of section 3.5, and no parking gap.
- The layout audit of the preset finds no conflict.
  Without the Parking facility entry, it finds the one hard conflict of P4.

## 4. The preset

| Item | Value |
| --- | --- |
| Name | `london-full-shared-express`, with the project name "LondonFull Shared Express" |
| Base | The default LondonFull, tight stations, station scale 0.5, `lateralMerges` set. `lateralMerges` has no further effect with `shapeClearance`. |
| Network | `shapeClearance` set. The classes Legacy, Compact, and Express on the route lanes, on each lane of the two hubs except the roads off the route, and on the hubs and their berths. The changes of section 3.5. |
| Order contract | `express-v1` |
| Services | `xs-WLO-BNK` and `xs-BNK-WLO`, party limit 20, as in D |
| Express fleet | 16 Express pods, as in D, in the parking of decision S4 (section 3.7) |
| Flags | None. The capacity flags do not apply, as for D, so the geometry of the qualification does not change. |

The builder is a function in `internal/scenarios` that changes a LondonFull project, as `londonFullExpress` changes the base of D.
It moves the hub nodes, turns Bank, sets the control point, and removes the two junction lanes after the generator runs.
So the generator, LondonFull, LondonCentral, and D do not change, and their network hashes stay.

Differences:

| | LondonFull | D, `london-full-express` | This preset |
| --- | --- | --- | --- |
| Stations | Tight | Wide, scale 1, two banks at each hub | Tight, the two hubs at 41 m parts |
| Express track | None | Dedicated trunks, gate stubs, a bend at Bank | Link 100, shared with Compact pods |
| Express parking | None | `express-parking`, 6.8 km from Waterloo | `express-parking`, a comb beside Waterloo (section 3.7) |
| `shapeClearance` | Not set | Not set | Set |
| `lateralMerges` | Set | Not set | Set, with no further effect |
| Junction lanes | All chords | All chords | Two chords removed |

Effect on existing presets: none expected.
The sim change of section 3.1 is behind the field, and no current preset sets it.
The gate-off reference test and the parity test of D in section 7 prove this.
`TestLondonFullSharedExpressKeepsPresets` pins the network hashes of LondonFull and D, and `cmd/scenario` has a test that only this preset sets the field.

## 5. Qualification

This section gives proposed text only.
A contract proposal follows after approval.
Note of 2026-11-16: the maintainer approved the rows as the [Express shared-track qualification](../contracts/express-shared-track-qualification-proposal.md), which carries the applied text.
The rows follow E1 to E8 of the [Express London qualification](../contracts/express-london-qualification-proposal.md#amendments) and section 5.1 of the shape clearance design.
The limits and the requirements of each row do not change, but the evidence of D does not transfer.
The prefix SX is a working name.

| Row | Based on | Proposed requirement |
| --- | --- | --- |
| SX1 | E1, Foundation parity | Qualification includes the opt-in preset `london-full-shared-express`. LondonFull, D, their fleets, and their defaults do not change, and their network hashes stay. |
| SX2 | E2, Route and station eligibility, and Physical geometry | Express runs on the lanes of link 100 and the four roads of section 1, shared with Compact pods, with no dedicated lane. The geometry is that of section 3.5. The audit refuses no pair. Qualification covers every hub berth and both directions. A merge, split, or shape pair certifies the 6 m by 2.5 m class body only (SC6). |
| SX3 | E3, Motion and ownership, and Independent safety oracle | The oracle checks the mixed fleet at each tick, with fault stops and emergency journeys, the shape table, the envelope reach, and the D6 oracle. Under decision S1, the indexed construction reads the compiled envelope reach, and an independent reference construction computes the envelope from the definition of section 3.1, with the small-pod rules. They give the same locations in the same order (amendment ST22 of the second shared-track amendment proposal). |
| SX4 | E4, Physical restore | Each planned checkpoint restores at the physical tier with no demotion, requeue, drop, or interruption, with the shape table, and the D6 oracle runs in restored continuation. |
| SX5 | E5, Save bytes and shape | The byte proofs run on the new geometry, with mixed-fleet, incident, and emergency members. |
| SX6 | E6, Stream and public consumers | The Go remote client, HTTP, WebSocket, WASM, and desktop Chrome, with loads of 12, 13, and 20 riders. The maintainer does the Chrome check. |
| SX7 | E7, Resource and consumer cost | Measure the preset, LondonFull, D, and the preset without its Express pods. |
| SX8 | E8, Orders and registry, and Storage and recovery | The current limits, with directed incident and recovery coverage on the preset. |
| SX9 | S1 | The standee Fault, Dwell, and Restore and stream rows run on the preset with standees, mixed traffic, and emergencies. |
| SX10 | A15 | The preset is a named project of the London soak row. |
| SX11 | A16 | A CI case of one simulated hour, and 5 seeds of 6 simulated hours before a release tag, with the full check and the D6 oracle at each tick. |
| SX12 | A17 | The preset is in the performance and latency measurements that apply. |
| SX13 | A18 | The formats byte proof runs on the preset. |

B1 does not apply, because the preset has no banks.

## 6. Decisions

| ID | Decision | Options | Recommendation |
| --- | --- | --- | --- |
| S1 | Front bound of the envelope reach | A: the bound for each lane of section 3.1, with the seed, and the local front in the oracle behind the field. This amends the approved texts of section 3.1. B: keep the bound of the whole network, and remove `move-06-08` and `move-08-02` too (section 3.6). | Decided on 2026-11-16: A, with the proof of section 3.1 and A1 as a rule of the model. On 2026-11-16, after a code review of slice 1, the maintainer replaced the seed and the local front with the anchor front and the compiled index of section 3.1. |
| S2 | Hub shape | A: tight, each part 41 m. B: wide at scale 0.5 with a hub pitch of 82. C: D4 at 14 m. | A, measured. B is not measured, and C does not remove the short lanes. |
| S3 | Geometry at the portals | A: the Waterloo throat at 90 m, the curve of `road-in-05`, Bank turned +5 degrees, and `move-05-01` and `move-08-06` removed. B: a new ordering resource for two lanes that share no node. | A, measured. B needs a new rule and holds Compact pods on other lines. |
| S4 | Express parking | A: a comb of 16 berths at 40 m by 40 m beside Waterloo, as D8-A says. B: Express pods start at hub berths, with no parking. C: the parking of D, which needs its trunks. | A, after a slice measures its site against the layout audit and the bank audit. The comb adds large lanes near Waterloo, with new tails, fronts, and reach overlaps, so the result of section 3.5 does not cover it. B is measured only with 4 pods at the 5 free hub berths, and it takes hub berths from Compact pods. C brings back the trunks that shared track removes. |
| S5 | Preset name and flags | A: the preset `london-full-shared-express`, with no capacity flags, as D. B: a flag on `london-full` that adds the shared route. | A. The qualification then has one geometry, and the network hash pins it. B lets a user combine the route with other capacity flags, but each combination is a new geometry with no evidence. |
| S6 | Qualification | A: the SX rows of section 5, in a new contract proposal. B: E1 to E8 extended to name both presets. | A. The evidence of D does not transfer, and new rows keep the approved E rows of D unchanged. B gives fewer rows, but it changes approved contract text for D. |

S1 is decided.
On 2026-11-16, the maintainer chose option A for S2 to S6.
S4-A needs its own measurement before the preset is final.
Section 3.7 gives it, and on 2026-11-16 the maintainer chose the site P4.

## 7. Test plan and slices

Tests of the index for each lane, in `internal/sim/envelope_anchor_test.go` unless stated:

- Containment by construction: each test of this list also requires each plane of the oracle in the span of the index at the published position (`anchorEnvelope`).
  `TestEnvelopeMatchesReference` and `TestBerthEnvelopeMatchesReference` require the oracle to equal the reference construction of SX3, with the same locations in the same order.
- Anchor front: `TestEnvelopeAnchorFront` pins `B(L)` on the curves network, and shows that lane `short` does not take the front of lane `bend` behind it, except within the slack of its start.
- The first blocker: `TestEnvelopeAnchorSubSlackChain` puts a group lane of 0.5 mm, and of 1.001 mm, between `bend` and `short`.
  The envelope at the start of `short` holds `short2` while `p <= u - length(gap)` and not at the next float64.
- The second blocker: `TestEnvelopeAnchorExactBounds` builds P, L, Q, M, N with the walk sum to N equal to `6 m + u` as a float64, and one float64 more.
  It checks the behind item of P and the ahead item of N at each bound and at the float64 values next to it.
  There, the anchors at the end of L also hold N in both cases, so this test does not isolate the walk limit.
- Walk limit: `TestEnvelopeAnchorWalkLimit` puts the start of N at exactly `fl(16.2 m + u)` from the end of a rule 1 lane, where no anchor at the node holds N.
- A long chain of short lanes: `TestEnvelopeAnchorShortChain`, with 50 and 150 lanes of 0.01 mm.
- A repeated route lane: `TestEnvelopeAnchorRepeatedLane`.
- Restore across a node: `TestEnvelopeAnchorRestoreNode` publishes the start of `short` with the route distance before it.
  `TestEnvelopeAnchorDiverge` also publishes the start of L with the route distance before the node, and the envelope does not change.
- Paths through the published lane: `TestEnvelopeAnchorDiverge` and `TestEnvelopeAnchorMerge` show that the span at a lane end next to a diverge or a merge does not hold the sibling lane, and that a pod whose route takes the sibling holds it.
  The diverge test also has a loop route that comes back to the diverge and takes the sibling later.
  `TestEnvelopeAnchorChainSiblings` puts a sibling at a node inside a chain of two short lanes and a sibling at the From node of L.
  `TestEnvelopeAnchorRouteCoverage` measures the tails and fronts along the route only, on a diverge whose sibling branch is shorter than the path through L, on the chain, and on the loop.
  It requires each route lane that this construction gives in the envelope.
  `TestEnvelopeAnchorRouteEnd` drives a pod with no destination through `move` to the end of its route, across a curved last lane of 2e-5 m.
  In some runs the published position stays on the lane before it, and the test requires a gap of at most u/2, parity with the reference, the route check, and the route lane that only the anchor on the last lane holds.
  `TestEnvelopeAnchorAuditSibling` runs the bank audit on a diverge: a lane next to the start of an Express lane, whose span holds the plane of the sibling, is not refused, and with the plane of the lane behind it is refused.
- Large route prefix: `TestEnvelopeAnchorRoutePrefix` moves each route start by 2^44 m, and the envelope does not change.
- Selection: `TestEnvelopeAnchorSelection` checks the origin unit before and after the origin release, a lane off the route, and a pod at a berth.
  `TestEnvelopeAnchorRuleTwoAfterSelection` checks that rule 2 adds a cluster lane off the route.
- Change from the built index: `TestEnvelopeReachChangeFromBuilt` (`envelope_support_test.go`) compares the index for each lane with the built index, and lists the changes by lane.
  The expected additions are the lanes of a cluster wherever a group lane is held, the lanes ahead within a rule 1 front that is more than the built bound, and the planes of the slack at a bound or a node.
  The expected removals are the lanes ahead past `B(L)`.
  The study measures additions on 8,904 lanes and removals on 8,927 lanes of the network of section 2.1, 9,014 and 8,717 on section 3.5, and 9,020 and 8,641 on section 3.6.
- Field off: `TestEnvelopeFieldOffMatchesBuilt` shows that the oracle and the index do not change without the field.
  `TestGateOffTables` and `TestLondonExpressBehaviorParity` show that the tables and the bytes of D do not change.
  `newPreparedNetwork` compiles the index only when the network sets `shapeClearance`.
- Audit: the preset refuses no pair, the LondonFull network with Express on the route gives the 7 refusals of section 2.2, and the order and text of a refusal do not change.

The first draft of this section had a bound test and an equality test against the index of the whole network.
These tests do not hold for section 3.1: the anchors at the nodes add planes that the index of the whole network does not hold at the same position.
The study logs the count, 8,760 lanes of the network of section 2.1, and it checks only that `B(L)` is at most the bound of the whole network.

Tests of the body proof, in place of the disk walk of the first draft.
Each test puts the oriented body at the reference, computes the support, and requires each support lane in the oracle and each plane of the oracle in the index span.
It also lists the route lanes that enter the body outside the support, and requires that A1 declares each one separated or that the oracle tests it through the envelope.
Each requirement also runs with the route distance and the published position shifted by two `restoreTolerance` steps each way, and with the body moved by the guideway tolerance to each side.
A support that reaches the end of a route window where the route can go on fails the test.
Each control of the test must fail at least once, and each coverage count must be more than zero.
The study test `TestSharedTrackStudyContainment` is the model for these tests.

| Test | Fixture | Requirement |
| --- | --- | --- |
| `TestEnvelopeSupportTurnBack` | A straight lane into a large quadratic lane of at least 40 m whose two nodes are 5 m apart, and a lane out of its To node in another group | Positions on the straight lane near its end hold the hairpin lane. Positions on the hairpin lane whose support reaches its From node hold the straight lane, through the behind walk. |
| `TestEnvelopeSupportReturn` | Straight lanes with join turns of 90 degrees that come back beside the first lane, 40 m behind its end, with a portal in a separate group | The support at the reference is the first lane only. Both oracles exempt a pod on the portal lane. The same pair with the portal lane in the group of the first lane is not exempt. |
| `TestEnvelopeSupportCurves` | A quadratic large lane with c of about 0.5 into a straight lane | Each position whose support reaches the join holds the next lane, also more than 6 m before the join. |
| `TestEnvelopeSupportOneNode` | Each large lane of the curves network, the Express fixture, the scenario networks, D, and the preset | No large lane is in a disk of radius 5.404 m: its smallest enclosing circle is larger. This guards F1 and F2. |
| `TestEnvelopeSupportMixedSmall` | Small pods of a mixed fleet on the same networks, the hook fixture, and LondonFull with its Docklands Light Railway bays, traveling, before the origin release, and at a berth | The support of the grown small body is held in each case of section 3.1 with the small-pod rules. |
| `TestEnvelopeSupportChain` | Each lane with a reduced retention on the scenario networks, D, the preset, and a depot chain fixture with joins that turn 45 degrees | No lane with a reduced retention is 4.7269 m long or less, and no such lane next to a route lane is wholly in the disk of the proof. This guards the chain row of section 3.1. |
| `TestEnvelopeReachHoldsEnvelope` | The cases above | No change. |

Note of 2026-11-16: the build names the first and third rows `TestEnvelopeSupportTurnBack`, which runs the four turn-back shapes with the hairpin among them, and `TestEnvelopeSupportCurves`, which runs the curves network.
The rows keep their fixtures and requirements as the cases that these two tests cover.

Mutations at the call sites, with the table of the `verify-by-mutation` skill.
Each run is `go test -short ./internal/sim -run 'Envelope|SafetyLocation|GateOff|LondonExpressBehaviorParity'`, and the same run with no mutation passes:

| Mutation | Tests that fail |
| --- | --- |
| The anchor front is 6 m on each lane (`anchorFront`) | `TestEnvelopeAnchorFront`, `TestEnvelopeAnchorWalkLimit`, `TestEnvelopeAnchorSubSlackChain`, `TestEnvelopeMatchesReference`, `TestEnvelopeSupportMixedSmall`, and others |
| No anchors within the slack (`localItems` returns the own anchor) | `TestEnvelopeAnchorExactBounds`, `TestEnvelopeAnchorRestoreNode`, `TestEnvelopeAnchorSubSlackChain`, `TestEnvelopeAnchorShortChain`, `TestEnvelopeMatchesReference`, and others |
| The walk limit rounds inward (`inclusiveLimit` returns `budget + u`) | `TestEnvelopeAnchorWalkLimit` |
| The end of a backward anchor term rounds inward (`u - d`, exclusive) | `TestEnvelopeAnchorSubSlackChain`, `TestEnvelopeAnchorShortChain`, `TestEnvelopeAnchorFront` |
| The selection skips the origin unit | `TestEnvelopeAnchorSelection`, `TestEnvelopeMatchesReference`, `TestEnvelopeSupportMixedSmall`, and others |
| The selection skips the berth of a pod at a berth | `TestEnvelopeAnchorSelection`, `TestEnvelopeMatchesReference`, `TestEnvelopeSupportMixedSmall`, and others |
| Rule 2 before the selection, which drops the cluster lanes off the route | `TestEnvelopeAnchorRuleTwoAfterSelection`, `TestEnvelopeAnchorSubSlackChain`, `TestEnvelopeMatchesReference`, `TestEnvelopeSupportMixedSmall`, `TestEnvelopeRuleTwoPairOrders` |
| The index is compiled without the field | `TestEnvelopeFieldOffMatchesBuilt`, `TestEnvelopeRuleTwoPairOrders` |
| No paths through the published lane: each anchor across a node takes its own walks (`throughItems` replaced by `anchorItems`) | `TestEnvelopeAnchorAuditSibling`, `TestEnvelopeAnchorChainSiblings`, `TestEnvelopeAnchorDiverge`, `TestEnvelopeAnchorMerge`, `TestEnvelopeMatchesReference`, and others |
| The path is matched only at the end node of the anchor lane: the walks start at A, and drop only the siblings at that node | `TestEnvelopeAnchorChainSiblings`, `TestEnvelopeMatchesReference`, `TestEnvelopeSupportMixedSmall` |
| The audit index drops the paths, and the compiled index keeps them | `TestEnvelopeAnchorAuditSibling` |
| The walk from the far end of L adds `length(L)` two times | `TestEnvelopeAnchorRouteCoverage`, `TestEnvelopeAnchorFront`, `TestEnvelopeAnchorRestoreNode`, `TestEnvelopeAnchorShortChain`, `TestEnvelopeMatchesReference`, and others |
| No origin units at the near nodes of an anchor after L | `TestEnvelopeReachChangeFromBuilt` |
| The ahead walk of an anchor before L, from the To node of L, starts 1 m further | `TestEnvelopeAnchorRouteCoverage`, by the reference and by the route check on the rule 1 chain |
| The ahead walk of an anchor after L, from the To node of A, starts 1 m further | `TestEnvelopeAnchorRouteCoverage`, by the reference and by the route check on the rule 1 chain, and `TestEnvelopeAnchorExactBounds` |
| The behind walk of an anchor after L, from the From node of L, starts 1 m further | None: equivalent in exact arithmetic. The anchor on L at p reads the same walk from the same node, with the least sum w. It holds a lane P while `p <= (tail(P) - w) + u`, and the item of A needs `0 <= (tail(P) - (d + length(L) + w)) + u`, with `p <= length(L)` and `d >= 0`. The origin items have the same form with `originAt`. In float64, the two tests can differ only by the rounding of these sums. |
| No anchors past the published lane (`localItems` skips the anchors out of the near nodes ahead of L) | `TestEnvelopeAnchorRouteEnd`, by the reference and by the lane that only the anchor on the last lane holds |
| No lanes between near nodes | None: an equivalent mutation. The anchor on L holds each such lane by its tail of at least 20 m behind and its front of at least 6 m ahead, both more than u. |

`TestGateOffTables` and `TestLondonExpressBehaviorParity` pass on the change, and they do not fail when the index is compiled without the field.
They pin the tables and the saved-state and stream bytes of D, which do not read the oracle envelope.

The controls of the containment test cover the small-pod rules: the oracle without them misses support lanes (section 3.1).

Slices:

0. An amendment proposal for the approved texts of section 3.1, for maintainer approval.
   Applied on 2026-11-16 in `5d8afa00`, and the second proposal for the anchor front in `1296c1d7`.
1. `sim`: the index for each lane, compiled at preparation, the oracle that reads it, and the small-pod rules, in the oracle and in the reference construction behind the field, with the tests and mutations above.
   Built on 2026-11-16 in `73a8ae32`, with the anchor front of section 3.1.
2. `scenarios`: the preset builder, the `cmd/scenario` preset, and tests that pin the audit result and the unchanged hashes of LondonFull and D.
   Built on 2026-11-16 in `c63a5741`, with the Express services and no Express pods until slice 3.
3. `scenarios`: the Express parking of S4, with its site measured by the layout audit and the bank audit.
   The test that pins the network hash of the preset comes after this slice, and so does the qualification evidence.
   Built on 2026-11-16 in `07200f86`, with the site P4 of section 3.7.
4. Done in this revision: the study tests of section 8.
5. `docs`: `docs/reference.md`, `docs/london-full.md`, `docs/operations.md` for the soak presets, and the build record of this design.
   Built on 2026-11-16, with the build record of this design.
   `docs/operations.md` needs no change yet, because no soak preset names the new preset until slice 6.
6. The contract proposal of section 5, after approval, and then the evidence of each row.
7. Batch 2 of the study plan of the shape clearance design (section 8).

## 8. Commands

The study tests are `internal/sim/shared_track_study_test.go` and `internal/scenarios/shared_track_study_test.go`.
They skip unless their environment variables are set.

| Test | Gives |
| --- | --- |
| `TestSharedTrackStudyRefusals` | The refusals of sections 2.2, 2.5, and 3.1 with each front bound, the preparation results, and, with `PODSIM_SHARED_TRACK_OUT`, the projects for `projectcheck` |
| `TestSharedTrackStudyGeometry` | The front bounds of sections 2.3 and 3.1, the index comparisons of section 7, the lane lengths of sections 2.4 and 3.2 to 3.5, and the sweeps of sections 3.3 and 3.4 |
| `TestSharedTrackStudyRouteCosts` | The route costs of section 3.5 |
| `TestSharedTrackStudyContainment` | The containment results of section 3.1 |
| `TestSharedTrackStudyLayout` | The layout audit of sections 3.2 and 3.5 |

```sh
D=$(mktemp -d)
mkdir "$D/out"
go run ./cmd/scenario -preset london-full -output "$D/lf.json"
go run ./cmd/scenario -preset london-full -wide-stations -road-fillets=false -output "$D/lfw.json"
go run ./cmd/scenario -preset london-full -junctions ramps -output "$D/lfr.json"
PODSIM_SHARED_TRACK_STUDY="$D/lf.json" PODSIM_SHARED_TRACK_OUT="$D/out" go test ./internal/sim -run 'TestSharedTrackStudy' -count=1 -v
PODSIM_SHARED_TRACK_STUDY="$D/lfw.json" go test ./internal/sim -run 'TestSharedTrackStudyRefusals$' -count=1 -v
PODSIM_SHARED_TRACK_STUDY="$D/lfr.json" go test ./internal/sim -run 'TestSharedTrackStudyRefusals$' -count=1 -v
PODSIM_SHARED_TRACK_LAYOUT="$D/out" go test ./internal/scenarios -run 'TestSharedTrackStudyLayout$' -count=1 -v
go run ./cmd/projectcheck -project "$D/out/tight-3.5.json"
go run ./cmd/projectcheck -project "$D/out/tight-3.6.json"
rm -r "$D"
```

The input files are about 15 MB each, and the written projects about 5.6 MB each.
The tests on the tight input take about 80 s.

## 9. Problems this design does not solve

- No simulation ran.
  The measurements are static: the audit, preparation, the layout audit, project validation, and the containment of poses.
  `Check`, the D6 oracle, and the effect on wait and journey times need slice 6.
  The small-pod rules add planes to the envelope of a small pod, so `Check` can test more pairs by distance, and that effect is not measured.
- The Express parking of S4-A needs a Parking facility entry in the layout audit of the preset, because its road crosses the curved `road-in-05` (section 3.7).
  Slice 3 built it as `sharedTrackLayoutInput`, for this preset only.
  `TestLondonFullSharedExpressLayout` pins the one hard conflict without it.
- A1 is a rule of the model, and no code checks it.
  A1 also assumes that the vertical gap between two levels is enough for each admitted body.
- The proof covers a body inside the disk of radius 5.404 m for a large pod and 4.7269 m for a small pod.
  This holds the gate body, the grown large body, and the grown small body, each with the guideway tolerance.
  It does not cover a body that fills the whole 6 m envelope disk.
- The proof does not cover a support that passes a maneuver group lane and then a lane with a reduced retention, both wholly in the disk.
  The containment test found no miss for such a support, but that is a measurement and not a proof.
- The guideway tolerance of 0.25 m is a physical assumption of the shape clearance design (section 3.4, item 4).
- F3 rests on the service contract and on the class test of the route search, and not on a trace of each caller that builds a route.
- The containment test samples poses, routes, and headings at a berth, so its results are measurements.
  A route window of the test can end where the route cannot go on without a lane that it already uses.
  There, the test does not check the track past the end, because a route does not use a lane two times.
- The slack of 1 mm covers the restore tolerance of two `restoreTolerance` steps, and the rounding of route sums within the coordinate and block limits of `internal/project/config.go`, about 4e-6 m.
  `internal/sim` does not enforce these limits itself, so a network that skips project validation is outside this bound.
- `projectcheck` ran on the projects that the study writes, with the change of section 3.1, and on the preset of slice 3.
  It reports the preset valid, with 343 pods and 9,089 lanes, and the tail warning of section 3.5.
  It runs no simulation.

## 10. Review

An adversarial review on 2026-11-16 found one blocker and two major problems.
A second review of the revision found no blocker and no counterexample to the argument for large pods under A1, and it found two major and two minor problems.
This revision answers each item of both rounds.
The numbers in the responses are those of the current study.

First round:

1. Blocker: section 3 shows that the index for each lane holds the local oracle, but not that it holds the physical body.
   The projection bound of one lane gives no common axis across a join.
   For example, a lane can turn back so that a portal 80 m ahead on the route is next to the pod.
   The network-wide bound can hold that plane, and the local oracle drops it.
   S1-A needs a proof that validation excludes such paths, or a bound that covers them, with returning paths, cycles, several joins, both classes, and the 10 m envelope gate of SC6.
   The disk walk of section 3 stops when the path leaves the disk, so it misses a path that comes back.

   Response: section 3.1 now has the proof, with A1, which the maintainer accepted as a rule of the model.
   Under A1, the body touches only the planes of its support, and the portal of the example is on another level.
   For a large pod, the support meets at most one node, because a large lane is at least 40 m long and a lane in the disk is at most 26.09 m long.
   So returning paths, cycles, and several joins cannot put a second node under the body.
   The proof covers the 10 m gate body of SC6, and the scope limit for the 6 m disk is stated.
   The containment test of section 3.1 replaces the disk walk.
   It computes the support of oriented bodies on the turn-back networks and on the London networks, and it found no failure.

2. Major: the 1,169 disk misses of section 9 are not classified.
   A lane that admits a large class always has a geometry tail, so each miss is on a small lane, and the case is a small pod against a large pod.
   The 6 m disk is larger than the grown small body, so some misses are harmless.
   Without the witnesses, the misses are neither a shown collision gap nor a shown artifact.
   They need a classification with oriented bodies and portal locations.

   Response: the containment test classifies the misses with oriented bodies.
   Without the small-pod rules, the grown small body misses support lanes at the maneuver group bays of `940GZZDLTWG` (4,192 body poses on each London network), and the built oracle misses the same lanes.
   On a curved lane with no tail, London has no miss, and the hook fixture shows the case with 15 misses.
   The maintainer decided to close these gaps in D11 under the field.
   Section 3.1 gives two rules with their proof, and with the rules the test finds no miss.

3. Major: the measurements of sections 2.2 to 3.6 come from deleted probes, so they cannot be reproduced.
   The probes must be kept as study tests, with the network hashes, the transformations, and the expected outputs, before the design is approved.

   Response: the study tests of section 8 reproduce each number of sections 2 and 3, and they pin the input hashes, the built network hashes, and the refusal counts.
   The re-measurement corrected these numbers:

   - The crossing of `road-in-05` is 4.9 m before the end of the link, not 4.8 m.
   - The links cross 27.7 m from the start of `ba-1`, not 27.8 m.
   - The audit order of the pair is `move-01-06`, then `road-out-01`.
   - The bound is 6.01 m or more on 6 lanes for `F(L)` and on 31 lanes for `F'(L)`.
   - The ramp network has 17 shape pairs with the bound of the whole network and 14 with the bound of each lane.

   The re-measurement also found that the small-pod rules raise the bound of the whole network of the fallback to 16.20 m (section 3.6).

Second round:

1. Major: the small-pod proof uses the small body of the sweep, with r = 4.10 m.
   It must cover the grown Compact body of the shape rule with the guideway tolerance, about 4.73 m.
   B1 is then about 22.84 m, below the small lane minimum of 24 m, and B2 about 16.15 m, below the 20 m floor behind.
   The cap of rule 1 must grow, and √2 r = 6.69 m breaks the argument of the 6 m floor for short chains.

   Response: the proof now uses r = 4.7269 m, the corner radius 4.4769 m of `classShapeBody(CompactClass)` and 0.25 m.
   B1 is 22.82 m, B2 is 16.14 m, and the cap of rule 1 is 16.2 m.
   For short chains, this design proves a tighter bound from the validated window of a lane with a reduced retention in the code, and not from the text of the model.
   The window turns at most 90 degrees and holds the lane before such a lane, and the lane is at least 5 m long.
   So the first lane wholly in the disk cannot have a reduced retention, and no front changes.
   The study measured the effect again: the refusals with the bound for each lane do not change, and the fallback with the rules refuses 4 pairs, not 3.

2. Major: the containment study does not enforce its claims.
   Misses of the grown body count as failures.
   Each control must fail with a minimum count, and each coverage count must be more than zero.
   A window must grow until the support leaves the body or the route ends, and a support cut by the window must fail.
   The bodies must also move by the guideway tolerance, the oracle must run at the shifted route distances, and unreleased origins need coverage.

   Response: the study does each of these.
   All four bodies are bodies of the proof, and a miss of any of them fails the study.
   The test requires each control to fail at least once and each coverage count to be more than zero.
   A window grows until the support leaves the body or the route cannot go on, and a support that reaches the end of a window where the route can go on fails.
   Each body also moves 0.25 m to each side, and the oracle runs at each shifted route distance.
   The test drives each route out of each berth with the origin held, and it checks each berth with a stationary pod.
   Section 3.1 labels the results as measurements.

3. Minor: the planned bound and equality tests compare the per-lane index with the built index, which the cluster rule and the small-pod fronts contradict.

   Response: section 7 now compares the per-lane index with an index of the whole network that takes the same small-pod fronts and cluster rule.
   A separate test compares with the built index and lists the expected additions and removals.
   The study measures both comparisons.

4. Minor: the amendment list needs the text of the shape clearance design at lines 137 to 139 and the text of the group physics design at line 88.
   The design must also say how the cluster rule applies to the envelopes of a traveling pod, of a pod at a berth, and of a pod before its origin release.

   Response: section 3.1 adds both texts to the amendment list, and it states the cluster rule for the three kinds of envelope.

Code review of slice 1, 2026-11-16:

1. Blocker: the seed of `F'(L)` held only the lanes into the From node of L.
   Behind a group lane shorter than the slack, a lane two lanes back reaches the local front of a pod at the start of L, and the index does not hold its front.
2. Blocker: the wider seed of the fix is not sound at an exact slack bound.
   In a chain P, L, Q, M, N with the walk sum at the bound plus the slack, the closure of the bound and the walk disagree at the float64 values next to the bound, and the index misses a plane of the oracle.

   Response: the rounds of fixes to the recursive bound kept finding new cases, so the design stepped back.
   The maintainer chose the shape of section 3.1 on 2026-11-16: the anchor front `B(L)` with no recursion, an index compiled once with the anchors within the slack and outward rounding, and an oracle that reads the index at the published position.
   The oracle is then in the index by construction, and the reference construction of SX3 and the support tests check the code independently.
   Both blocker constructions are now tests at their exact bounds (section 7).
   The new shape added 11 refusals on the network of section 2.1, each the pose of an earlier refusal at a node, and the preset still refused no pair.
   The applied texts that the change makes false are in the [second shared-track amendment proposal](../contracts/shared-track-amendment-2-proposal.md).

Review of the rework, 2026-11-16: no finding.
The review agreed that the reference construction is independent of the index, and that the quotes of ST16 to ST23 match.
It noted that the planes of the sibling lanes are conservative, and that their removal needs a provenance of the route through the anchor paths and the selection, not a route filter in an index with no route.

Response: the maintainer approved ST16 to ST23 and chose to remove the 11 refusals in slice 1.
The index takes the paths through the published lane (section 3.1).
That provenance is the published lane, which is on the route of each pod published there, so the index needs no route, and the audit uses the same argument as the oracle.
The review proposed path IDs on the items, a route check at selection, and a pairing rule in the audit.
This rule evaluates the provenance at compile time instead.
The study gives 7 refusals again on the tight and wide networks of section 2.1, and the oracle holds the same number of planes on the study networks.
The rule is ST24 of the second proposal, which the maintainer approved on 2026-11-16, and which is applied.
