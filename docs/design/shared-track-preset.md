# Shared-track Express preset

Status: proposed on 2026-11-16, revised on 2026-11-16 after review (section 10).
On 2026-11-16, the maintainer approved the contract amendment of this design as ST1 to ST15 of the [shared-track amendment proposal](../contracts/shared-track-amendment-proposal.md), and the SX rows as the [Express shared-track qualification](../contracts/express-shared-track-qualification-proposal.md) contract, and the text was applied.
The design itself still waits for its build.

This design is decision D11 of the approved [shape clearance](shape-clearance.md) design.
D11 keeps project D (`london-full-express`) as it is, and it adds a new opt-in preset with Express on shared main-line track between Waterloo and Bank.
This design gives the measurements on the current code, the changes that remove each refusal, the preset, the proposed qualification rows, and the decisions for the maintainer.
It also proposes a front bound for each lane in the envelope reach index, in place of the bound of the whole network.
No code changes until the maintainer approves this design.

Summary:

- On the default LondonFull network with Express on the route, the built audit refuses 14 pairs, and 10 of them are closer than 4.0 m.
  With a front bound for each lane, it refuses 7 pairs, and 5 of them are closer than 4.0 m.
- The tight hubs have 32 Express lanes of 25.0 m or 25.5 m, under the 40 m of a large lane.
  This blocker is larger than the one of the shape clearance design, which measured the wide stations.
- A set of geometry changes removes each refusal and each short lane in a copy of the network.
  With the front bound for each lane, the audit then refuses no pair.
  With the bound of the whole network, it refuses 7 pairs, unless two more junction lanes go.
- The maintainer approved S1-A: the front bound for each lane in the index, with the seed, and a local front in the oracle, behind the `shapeClearance` field.
  Section 3.1 proves that the local oracle holds each plane under the body of a large pod, with axiom A1, which the maintainer accepted as a rule of the model.
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
With the front bound of each lane, it gives the 7 refusals that the shape clearance design gives for a front reach of 6 m.
The wide network gives the same refusals.
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
With the front bound of each lane, it refuses the first 7 pairs, 5 under 4.0 m, and it has 39 shape pairs.
The seed and the small-pod rules of section 3.1 give the same 7 refusals.
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
The built index takes 66.42 m for each lane (`reachFront`, `internal/sim/envelope_reach.go:217`).
The bound `F(L)` of section 3.1, without the seed, is 6.01 m or more on 6 lanes: 66.42 m on `move-05-01` and `london-link-350-ab-2`, 33.62 m on `move-06-08` and `london-link-098-ba-2`, and 23.81 m on `move-08-02` and `london-link-100-ba-2`.
A lane before a curved junction lane gets its bound, because a pod at the end of the lane can enter the curve.
The bound `F'(L)`, with the seed, is 6.01 m or more on 31 lanes, because each lane out of the To node of such a lane also takes its bound.
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
There, the audit refuses 14 pairs, 11 under 4.0 m, with either front bound.
It has 17 shape pairs with the bound of the whole network and 14 with the bound of each lane.
The ramps split link 100 into lanes of 40.0 m, and 46 lanes are short.
Preparation refuses `london-link-100-ba-1` at 40.0 m by rounding.
The ramp junctions are not a better base for this preset.

## 3. Changes that remove the refusals

### 3.1 Front bound for each lane

#### Definition

Let `front(M)` be the front bound of lane M from the tail index, and 0 when it has none.
Under the field, `front(M)` also includes the small-pod front of a curved lane with no geometry tail ("Small pods of a mixed pair" below).
For each lane L, the bound `F(L)` is the least value that is at least `max(6 m, front(L))`, and at least `front(M)` for each lane M that starts at a node within `F(L)` of the To node of L.
The distance is the shortest walk along lanes, with `envelopeReachSlack`.
A walk in order of distance from the To node computes it: start with `max(6 m, front(L))`, raise the bound with the front of each lane out of each settled node, and stop at the first node whose distance is the bound plus the slack or more.
The bound is at most the bound of the whole network, so the walk is finite.

The index takes `F'(L)`: the largest of `F(L)` and `F(P)` for each lane P into the From node of L.
This seed covers a pod that a restore or the slack puts at the end of P while the index reads the start of L.
The ahead walk of `laneItems` (`envelope_reach.go:294`) then uses `F'(L)` in place of the bound of the whole network.
The behind walk does not change, because it already tests each lane against its own retained tail (`:282`) and each origin against the origin tail of its node (`:287`).

#### The oracle must change too

The oracle takes the largest front bound of all lanes of the route of the pod, behind the pod and far ahead of it (`appendVehicleSafetyLocations`, `internal/sim/envelope_safety.go:51` to `:56`).
`TestEnvelopeReachCurves` pins this with its case "two lanes ahead from the front bound of a lane behind" (`internal/sim/envelope_reach_test.go:446`).
So a front bound for each lane alone does not hold the envelope.
An index that holds the built oracle must take, for each lane, the largest front of any lane that can share a route with it.
In a strongly connected network, that is the bound of the whole network, as the comment of `reachFront` says.
The containment test of this design measures this.
The built oracle has planes that the index for each lane does not hold at 1,083 poses on the network of section 2.1.
The counts are 1,500 poses on the network of section 3.5 and 752 poses on the curves network.
For example, a small pod on `london-link-100-ab-2` at 921.96 m, on a route from `move-06-08`, has `940GZZLUWLO-move-01-06` in its envelope 33.62 m before the end of the lane.

So, when the network sets `shapeClearance`, the oracle takes a local front.
At a route distance x, the front is the least value that is at least 6 m and at least the front bound of each route lane that ends at x minus the slack or later and starts at x plus the front or earlier.
One pass over the route lanes in order computes it, because a lane starts after each lane before it.
Under the field, the oracle also applies the small-pod rules of "Small pods of a mixed pair" below.
They apply to the envelope of a traveling pod, of a pod before its origin release, and of a pod at a berth.
Without the field, the oracle and its reference construction (`envelope_safety_reference_test.go:25`) do not change, so D and each other project keep their envelopes.

#### Why the index holds the changed envelope

Take a pod on a route through L, at position p on L, at route distance x.
The changed oracle holds a lane M ahead when the route distance from x to the start of M is at most the local front f(x).

1. f(x) is at most `F'(L)`.
   The lanes in its pass are the lane before L when x is within the slack of the start of L, L itself, and route lanes ahead of L.
   The lane before L adds at most `F(P)`, which the seed holds.
   For a lane ahead, the route distance from the To node of L to its start is at least the shortest walk distance.
   So each such lane is within the walk of `F(L)`, and its front is at most `F(L)`, by induction over the pass.
   When the pass includes P, the same argument from the To node of P gives `F(P)`.
2. The index holds M at p.
   The item of M starts at `length(L) + d - F'(L) - slack`, where d is the shortest walk distance (`envelope_reach.go:296`).
   The oracle holds M only when `length(L) - p + r <= f(x) <= F'(L)`, where r is the route distance and `r >= d`.
   So p is at or after the start of the item.
3. Restore slack: a restored route distance differs from the published lane position by at most two `restoreTolerance` steps of 1e-6 m (`envelope_reach.go:15`).
   The slack of 1e-3 m covers this, as today.
   At a node, the published lane can be L while the route distance is at the end of P.
   The seed covers this case.
4. Retained tails and the origin: the behind walk and its items do not change.
5. Join turns: `F'(L)` is at least 6 m, as the bound of the whole network is.
   So the lanes out of the To node stay in the last 6 m or more of L, and the sweep keeps the join turn in each range that holds the end of the lane (`clipShapeSteps`, `internal/sim/shape_pairs.go:162`).
6. Classes: the walk takes each lane out of each node, whatever its classes, so it covers the envelope of the small pod of a mixed pair.
7. Clusters: where the index holds a maneuver group lane over a range of positions, it also holds each lane of its cluster over that range.
   The berth items of the index get the same lanes.
   So the index holds the planes that the cluster rule adds to the oracle.

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
The proof obligation is that, for each route lane M that meets C(x), the envelope at x holds M.
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

#### Why the local oracle holds the body

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
  f(x) is at least `front(L)`, because L holds x.
  So M starts at most f(x) ahead of x, and the oracle holds M.
- The previous route lane P, when the support passes the From node of L.
  The behind walk does not change, and it holds P while x is within max(20 m, tail(P)) of the From node.
  The arc is at most 18.45 m long (B2), which is less than 20 m, so the oracle holds P.

A join turn does not change the lemma, because B1 does not depend on the angle at a node.
A cycle or a returning path does not change it, because the support meets one node at most.
The origin tail is at least 20 m, so it holds the origin berth over the whole behind support.
The proof does not use the bound of the whole network.

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
   That is at most the front, so the local front holds the next route lane.
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

- A traveling pod: the lanes of the route that the local front and the tails hold.
- A pod before its origin release: the origin berth and the lanes at its node, as today, and the cluster of each group lane among them.
- A pod at a berth: the berth and the lanes at its node, and the cluster of each group lane among them.

The index applies rule 2 to each item of a lane span and to each berth span (item 7 above).

The first draft proposed another rule for maneuver group lanes: hold the next route lane while each node up to it is within 6 m of the reference.
It is not measured, and its test of plan distance has no form in an index of path distances (inferred), so this design does not use it.
The proof does not cover a support that passes a maneuver group lane and then a lane with a reduced retention, both wholly in D(x).
There, the window of the second lane need not hold x, and the cluster of the first lane does not hold the lane after the second.

On the network of section 2.1, rule 1 applies to 288 curved lanes with no geometry tail, and 60 of them get the cap of 16.2 m.
Rule 2 applies to 312 maneuver group lanes, with 328 lanes at a node of a cluster.
The network has 48 lanes with a reduced retention, and the shortest is 6.00 m.
With the two rules, `F'(L)` is 6.01 m or more on 821 lanes of the network of section 2.1, on 819 lanes of section 3.5, and on 798 lanes of section 3.6.
The audit with the bound for each lane gives the same refusals with and without the rules (section 2.2 and the effect table below).

#### Measured containment

The containment test of section 8 measures the proof on sampled poses.
It does not replace the proof, and its poses are samples.

The test puts oriented bodies at poses along each lane and across each join turn.
The poses are each 0.5 m of the first and last 16 m of a lane, the middle, and each position at which the oracle or the index adds or drops a plane.
Each body is also moved by the guideway tolerance, 0.25 m, to each side.
At each pose, the test computes the local oracle and the built oracle at the route distance and at the route distance shifted by two `restoreTolerance` steps each way.
It reads the index at the published position with the same shifts.
The bodies are the gate body and the grown large body for a large pod, and the small body of the sweep and the grown small body for a small pod.

For each pose, the test computes the support of each body: the connected part of the route polyline in the body footprint that holds the reference.
It requires each support lane in the local oracle with the small-pod rules at each shift, and each plane of that oracle in the index with the seed and the rules.
It also classifies each route lane off the support that enters the body.
Either the oracle tests the lane through a plane of the envelope that is not separated from it, or A1 declares the lane separated from each plane of the envelope.

Each route window is long: at least the retained tail behind and the front bound plus 60 m ahead, and up to 256 lanes.
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

On the London networks, each large lane gets windows of at least 1,100 m behind and ahead, so that the route maximum of the built oracle can come from far lanes.
The test fails on a failure of the proposal, on a control under its minimum count, and on a coverage count under its minimum.

Results:

- Failures of the proposal: 0 on each network, for each of the four bodies.
  No support lane is missing from the local oracle, no plane of the local oracle is missing from the index, and no support reaches the end of a window where the route can go on.
- The support of a large body never meets more than one node.
  On the network of section 2.1, it meets one lane at 128,706 body poses and two lanes at 89,394 body poses of the gate body.
- The support of the small body meets more than one node at 18,169 body poses with a maneuver group lane, and at 252 body poses with no maneuver group lane.
- Off-support lanes in the gate body on the network of section 2.1: 624 body poses where A1 declares the lane separated, and 84 where the oracle tests it through the envelope.
  An example is a pod on the Bank road `road-in-08` whose route comes back on `road-out-08`.
  On the network of section 3.5, the counts are 384 and 264.
  The loop and bridged loop shapes give 72 and 69 such body poses under A1, and the hairpin gives 75 that the oracle tests.
- Poses on the network of section 2.1: 72,700 large and 1,752,543 small.
  Of these, 341 large and 44,157 small poses have the origin held.
  The test also checks 7 large berths and 770 small berths with a stationary pod.
  At 192 body poses at a berth, for example at `940GZZDLTWG-bay-1`, the support of a small body meets a second node through a maneuver group lane, and rule 2 holds it.
  On the network of section 3.5, the poses are 81,852 large and 1,755,754 small.

Controls show that the test can fail, and the test requires each control to fail at least once:

| Control | Misses |
| --- | --- |
| Index without the seed, against the local oracle | 12 index misses on the curves network, at the start of lane `short` |
| Index for each lane, against the built oracle with the route maximum | 1,083 on the network of section 2.1, 1,500 on section 3.5, and 752 on the curves network |
| Local oracle without the small-pod rules, small body | 1,136 support misses on each London network, at the Docklands Light Railway bays of `940GZZDLTWG`, through maneuver group lanes, and 8 on the hook fixture, on the curved lane |
| Local oracle without the small-pod rules, grown small body | 4,192 support misses on each London network, and 15 on the hook fixture |
| Built oracle with the route maximum, both small bodies | The same misses as the line above for each body |
| Berth envelope without rule 2, both small bodies, and the built berth envelope | 528 support misses on each London network, for example at `940GZZDLTWG-bay-1` |

On the London networks, the small bodies miss no support lane on a curved lane with no tail, with or without the rules.
The hook fixture has a lane of 35.1 m with c = 0.00, so it gets the cap of 16.2 m.

#### Effect

| Network | Front bound | Refusals | Under 4.0 m | Shape pairs |
| --- | --- | ---: | ---: | ---: |
| LondonFull, Express on the route | Whole network, 66.42 m | 14 | 10 | 40 |
| LondonFull, Express on the route | Each lane, with or without the seed or the small-pod rules | 7 | 5 | 39 |
| Section 3.5, all changes | Whole network, 33.62 m | 7 | 5 | 39 |
| Section 3.5, all changes | Each lane, with or without the seed or the small-pod rules | 0 | 0 | 38 |
| Section 3.6, the fallback | Whole network, 7.96 m | 0 | 0 | 23 |
| Section 3.6, the fallback | Whole network with the small-pod rules, 16.20 m | 4 | 2 | 21 |

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
  The local front is a front bound.
- Shape clearance, line 136: "in its last 6 m, or the longer front bound of the lane".
- Standees contract, lines 291 and 309 to 312, as records for a network without the field.

The code comments of `envelopeReach` and `reachFront` (`internal/sim/envelope_reach.go:27` to `:30`, `:212` to `:216`) change in slice 1.

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

- The audit with the front bound for each lane refuses no pair, with or without the seed or the small-pod rules.
- No route or hub lane is under 40 m, and preparation passes.
- The layout audit of the generator finds no conflict.
- `projectcheck` at `7bebe01e`, with the order contract `express-v1`, both Express services, and 4 Express pods at free hub berths, stops at the plane reach refusal of `move-01-06` and `road-out-01`.
  Project validation reports this refusal after each other check (`internal/project/config.go:335`), so each other check passes.
  The refusal comes from the bound of the whole network, 33.62 m from `move-06-08`.
  The audit copy with the bound for each lane refuses no pair, so the refusal goes with the change of section 3.1 (inferred, because `projectcheck` cannot run with that change before slice 1).
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

## 4. The preset

| Item | Value |
| --- | --- |
| Name | `london-full-shared-express`, with the project name "LondonFull Shared Express" |
| Base | The default LondonFull, tight stations, station scale 0.5, `lateralMerges` set. `lateralMerges` has no further effect with `shapeClearance`. |
| Network | `shapeClearance` set. The classes Legacy, Compact, and Express on the route lanes, on each lane of the two hubs except the roads off the route, and on the hubs and their berths. The changes of section 3.5. |
| Order contract | `express-v1` |
| Services | `xs-WLO-BNK` and `xs-BNK-WLO`, party limit 20, as in D |
| Express fleet | 16 Express pods, as in D, in the parking of decision S4 |
| Flags | None. The capacity flags do not apply, as for D, so the geometry of the qualification does not change. |

The builder is a function in `internal/scenarios` that changes a LondonFull project, as `londonFullExpress` changes the base of D.
It moves the hub nodes, turns Bank, sets the control point, and removes the two junction lanes after the generator runs.
So the generator, LondonFull, LondonCentral, and D do not change, and their network hashes stay.

Differences:

| | LondonFull | D, `london-full-express` | This preset |
| --- | --- | --- | --- |
| Stations | Tight | Wide, scale 1, two banks at each hub | Tight, the two hubs at 41 m parts |
| Express track | None | Dedicated trunks, gate stubs, a bend at Bank | Link 100, shared with Compact pods |
| Express parking | None | `express-parking`, 6.8 km from Waterloo | Decision S4 |
| `shapeClearance` | Not set | Not set | Set |
| `lateralMerges` | Set | Not set | Set, with no further effect |
| Junction lanes | All chords | All chords | Two chords removed |

Effect on existing presets: none expected.
The sim change of section 3.1 is behind the field, and no current preset sets it.
The gate-off reference test and the parity test of D in section 7 prove this.

## 5. Qualification

This section gives proposed text only.
A contract proposal follows after approval.
The rows follow E1 to E8 of the [Express London qualification](../contracts/express-london-qualification-proposal.md#amendments) and section 5.1 of the shape clearance design.
The limits and the requirements of each row do not change, but the evidence of D does not transfer.
The prefix SX is a working name.

| Row | Based on | Proposed requirement |
| --- | --- | --- |
| SX1 | E1, Foundation parity | Qualification includes the opt-in preset `london-full-shared-express`. LondonFull, D, their fleets, and their defaults do not change, and their network hashes stay. |
| SX2 | E2, Route and station eligibility, and Physical geometry | Express runs on the lanes of link 100 and the four roads of section 1, shared with Compact pods, with no dedicated lane. The geometry is that of section 3.5. The audit refuses no pair. Qualification covers every hub berth and both directions. A merge, split, or shape pair certifies the 6 m by 2.5 m class body only (SC6). |
| SX3 | E3, Motion and ownership, and Independent safety oracle | The oracle checks the mixed fleet at each tick, with fault stops and emergency journeys, the shape table, the envelope reach, and the D6 oracle. Under decision S1, the indexed and the reference envelope constructions take the local front and the small-pod rules, and they give the same locations in the same order. |
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
| S1 | Front bound of the envelope reach | A: the bound for each lane of section 3.1, with the seed, and the local front in the oracle behind the field. This amends the approved texts of section 3.1. B: keep the bound of the whole network, and remove `move-06-08` and `move-08-02` too (section 3.6). | Decided on 2026-11-16: A, with the proof of section 3.1 and A1 as a rule of the model. |
| S2 | Hub shape | A: tight, each part 41 m. B: wide at scale 0.5 with a hub pitch of 82. C: D4 at 14 m. | A, measured. B is not measured, and C does not remove the short lanes. |
| S3 | Geometry at the portals | A: the Waterloo throat at 90 m, the curve of `road-in-05`, Bank turned +5 degrees, and `move-05-01` and `move-08-06` removed. B: a new ordering resource for two lanes that share no node. | A, measured. B needs a new rule and holds Compact pods on other lines. |
| S4 | Express parking | A: a comb of 16 berths at 40 m by 40 m beside Waterloo, as D8-A says. B: Express pods start at hub berths, with no parking. C: the parking of D, which needs its trunks. | A, after a slice measures its site against the layout audit and the bank audit. The comb adds large lanes near Waterloo, with new tails, fronts, and reach overlaps, so the result of section 3.5 does not cover it. B is measured only with 4 pods at the 5 free hub berths, and it takes hub berths from Compact pods. C brings back the trunks that shared track removes. |
| S5 | Preset name and flags | A: the preset `london-full-shared-express`, with no capacity flags, as D. B: a flag on `london-full` that adds the shared route. | A. The qualification then has one geometry, and the network hash pins it. B lets a user combine the route with other capacity flags, but each combination is a new geometry with no evidence. |
| S6 | Qualification | A: the SX rows of section 5, in a new contract proposal. B: E1 to E8 extended to name both presets. | A. The evidence of D does not transfer, and new rows keep the approved E rows of D unchanged. B gives fewer rows, but it changes approved contract text for D. |

S1 is decided.
On 2026-11-16, the maintainer chose option A for S2 to S6.
S4-A needs its own measurement before the preset is final.

## 7. Test plan and slices

Tests of the front bound for each lane:

- Bound: on each network of `testdata/scenario_networks.json.gz`, D, and the preset, `F'(L)` is at most the bound of the whole network.
  Each span of the per-lane index is a subset of the span of an index with the bound of the whole network, when both indexes take the same small-pod fronts and the same cluster rule.
- Equality: on each lane whose `F'(L)` equals the bound of the whole network, the two indexes of the bound test give the same spans.
  The test runs on a network with a large front, such as the preset before `move-05-01` goes, so it covers the walk of the bound.
  The study measures both tests, and no location is outside the whole index.
  There is no difference on the lanes whose bound is the bound of the whole network: 9 lanes of the network of section 2.1, 11 of section 3.5, and 461 of section 3.6.
- Change from the built index: a separate test compares the per-lane index with the small-pod rules against the built index, and lists the changes by lane.
  The expected additions are the lanes of a cluster wherever a group lane is held, in lane spans and in berth spans.
  The lanes ahead within a small-pod front are also additions where that front is more than the built bound.
  The expected removals are the lanes ahead past `F'(L)`.
  The study measures additions on 200 lanes and removals on 8,919 lanes of the network of section 2.1, 320 and 8,707 on section 3.5, and 789 and 8,233 on section 3.6.
- Containment: `TestEnvelopeReachHoldsEnvelope` with the local oracle and the small-pod rules under the field, on the curves network, the Express fixture, the scenario networks, and the preset.
  It checks route distances shifted by two `restoreTolerance` steps each way, and published positions shifted the same way.
  The case "two lanes ahead from the front bound of a lane behind" stays, and it now checks the local oracle.
- Field off: the oracle and the reference construction give the envelopes of today on D and on each scenario network, and `TestBerthEnvelopeMatchesReference` passes.
- Parity of D: the saved-state and stream bytes of D at each checkpoint equal those of the build before the change.
- Audit: the preset refuses no pair, the LondonFull network with Express on the route gives the 7 refusals of section 2.2, and the order and text of a refusal do not change.

Tests of the body proof, in place of the disk walk of the first draft.
Each test puts the oriented body at the reference, computes the support, and requires each support lane in the local oracle and in the index span.
It also lists the route lanes that enter the body outside the support, and requires that A1 declares each one separated or that the oracle tests it through the envelope.
Each requirement also runs with the route distance and the published position shifted by two `restoreTolerance` steps each way, and with the body moved by the guideway tolerance to each side.
A support that reaches the end of a route window where the route can go on fails the test.
Each control of the test must fail at least once, and each coverage count must be more than zero.
The study test `TestSharedTrackStudyContainment` is the model for these tests.

| Test | Fixture | Requirement |
| --- | --- | --- |
| `TestEnvelopeSupportHairpin` | A straight lane into a large quadratic lane of at least 40 m whose two nodes are 5 m apart, and a lane out of its To node in another group | Positions on the straight lane near its end hold the hairpin lane. Positions on the hairpin lane whose support reaches its From node hold the straight lane, through the behind walk. |
| `TestEnvelopeSupportReturn` | Straight lanes with join turns of 90 degrees that come back beside the first lane, 40 m behind its end, with a portal in a separate group | The support at the reference is the first lane only. Both oracles exempt a pod on the portal lane. The same pair with the portal lane in the group of the first lane is not exempt. |
| `TestEnvelopeSupportCurvedJoin` | A quadratic large lane with c of about 0.5 into a straight lane | Each position whose support reaches the join holds the next lane, also more than 6 m before the join. |
| `TestEnvelopeSupportOneNode` | Each large lane of the curves network, the Express fixture, the scenario networks, D, and the preset | No large lane is in a disk of radius 5.404 m: its smallest enclosing circle is larger. This guards F1 and F2. |
| `TestEnvelopeSupportMixedSmall` | Small pods of a mixed fleet on the same networks, the hook fixture, and LondonFull with its Docklands Light Railway bays, traveling, before the origin release, and at a berth | The support of the grown small body is held in each case of section 3.1 with the small-pod rules. |
| `TestEnvelopeSupportChain` | Each lane with a reduced retention on the scenario networks, D, the preset, and a depot chain fixture with joins that turn 45 degrees | No lane with a reduced retention is 4.7269 m long or less, and no such lane next to a route lane is wholly in the disk of the proof. This guards the chain row of section 3.1. |
| `TestEnvelopeReachHoldsEnvelope` | The cases above | No change. |

Mutations, with the table of the `verify-by-mutation` skill:

| Mutation | Test that fails |
| --- | --- |
| No seed from the lanes into the From node | Containment on the curves network, at the start of `short` (measured: 12 misses) |
| The bound is `max(6 m, front(L))`, with no walk | Containment on a route that ends a lane before a curved lane, such as `link-350-ab-2` before `move-05-01` (inferred) |
| The oracle keeps the route maximum under the field, and the index takes the bound for each lane | Containment, on the network of section 2.1 (measured: 1,083 misses) and on the curves network (measured: 752 misses) |
| The local front ignores the front of the lane that holds x, and takes 6 m | `TestEnvelopeSupportCurvedJoin` (inferred) |
| No small-pod front on curved lanes with no tail | `TestEnvelopeSupportMixedSmall` on the hook fixture (measured: 15 misses of the grown small body) |
| No cluster rule on maneuver group lanes | `TestEnvelopeSupportMixedSmall` at the bays of `940GZZDLTWG` (measured: 4,192 misses of the grown small body, and 528 at the berths) |
| The turn limit of a retention window is 180 degrees in place of 90 degrees | `TestEnvelopeSupportChain` on the depot chain fixture (inferred, the fixture is not built) |
| The large lane minimum is 20 m in place of 40 m | `TestEnvelopeSupportOneNode`, on a fixture with a large hairpin lane of 22 m after a join turn of 180 degrees (inferred, the fixture is not built) |
| The tail floor of the behind walk is 15 m in place of 20 m | `TestEnvelopeSupportHairpin`, if its fixture has a behind support over 15 m (inferred, the fixture is not built) |
| The test takes each route lane in the footprint, and not the support | `TestEnvelopeSupportReturn` fails with both oracles (inferred). This control shows that the test separates the two readings. |
| The oracle takes the local front without the field | Field off and parity of D (inferred) |
| The walk without the slack | Containment with shifted route distances (inferred) |

Slices:

0. An amendment proposal for the approved texts of section 3.1, for maintainer approval.
1. `sim`: the bound for each lane in the index, the local front and the small-pod rules in the oracle and in the reference construction behind the field, with the tests and mutations above.
2. `scenarios`: the preset builder, the `cmd/scenario` preset, and tests that pin the audit result and the unchanged hashes of LondonFull and D.
3. `scenarios`: the Express parking of S4, with its site measured by the layout audit and the bank audit.
   The test that pins the network hash of the preset comes after this slice, and so does the qualification evidence.
4. Done in this revision: the study tests of section 8.
5. `docs`: `docs/reference.md`, `docs/london-full.md`, `docs/operations.md` for the soak presets, and the build record of this design.
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
go run ./cmd/scenario -preset london-full -wide-stations -output "$D/lfw.json"
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
  `Check`, the D6 oracle, and the effect on wait and journey times need slices 2 and 6.
  The small-pod rules add planes to the envelope of a small pod, so `Check` can test more pairs by distance, and that effect is not measured.
- The site of the Express parking of S4-A is not measured, and its large lanes can add refusals.
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
- `projectcheck` with the change of section 3.1 is inferred from the audit copy, because the change is not built.

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
