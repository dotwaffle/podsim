# Shared-track Express preset

Status: proposed on 2026-11-16, and on hold after review (section 10).

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
- A set of geometry changes removes each refusal and each short lane in a scratch copy of the network.
  With the front bound for each lane, the audit then refuses no pair.
  With the bound of the whole network, it refuses 7 pairs, unless two more junction lanes go.
- The front bound for each lane holds the envelope only if the oracle also takes a local front bound.
  The built oracle takes the largest front bound of the whole route, behind the pod too, and a measured containment test fails against it.
  So the proposal changes the oracle behind the field, and that amends approved contract text.

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

## 2. Measurements

### 2.1 Method

All measurements are at `3db8ce39`, on the default LondonFull network: tight stations, `lateralMerges` set, 5,740 nodes, 9,024 lanes, network SHA-256 `e194426aa95104424a6365370f014c166f25d0572199f9b875e07bb58f2e880e`.
A scratch probe, now deleted, did these steps:

1. Set the classes Legacy, Compact, and Express on the route lanes, as `TestShapeClearanceStudyRoutes` does (section 2.11 of the shape clearance design).
2. Set `shapeClearance`.
3. Run a copy of `auditBankGeometry` that records each refusal, in place of the first one, with the closest distance of the parts that are not separated.
4. Run the same copy with an envelope reach index whose ahead walk takes the front bound of each lane (section 3.1), in place of the bound of the whole network.

The copy reproduces the counts of "Counts of the built audit" in section 2.2 of the shape clearance design: 40 shape pairs and 14 refusals with the built bound.
With the front bound of each lane, it gives the 7 refusals that the shape clearance design gives for a front reach of 6 m.
The wide network gives the same refusals.
For each refusal, the probe ran the shape sweep, and each sweep found a collision.
The free-flow routes are 2,171 m from Waterloo to Bank and 2,103 m back, from the station exit to the station entry.

### 2.2 Refusals

Each refusal has the text `nonincident paths "A" and "B" are less than 20 meters apart`, with the two lanes in audit order.
The distance is the smallest distance of the parts that the reach does not separate.
"Bound" tells which front bound refuses the pair: "both", or "whole network" only.
"Cause" names the plane that one reach holds and that shares a node with a plane of the other reach.

| A | B | Distance | Place | Bound | Cause |
| --- | --- | ---: | --- | --- | --- |
| `940GZZLUWLO-road-in-05` | `london-link-100-ab-2` | 0.00 m | Waterloo | Both | The road crosses the link 4.8 m before the end of the link. The last 6 m of the link hold `road-in-01`, which shares the diverge with `road-in-05`. |
| `london-link-350-ab-2` | `london-link-100-ba-1` | 0.00 m | Links | Both | The links cross 27.8 m from the start of `ba-1`. The first 64.5 m of `ba-1` hold `940GZZLUWLO-move-05-01`, whose tail is 64.5 m. It shares `london-link-350-b-arrival` with `london-link-350-ab-2`. |
| `940GZZLUWLO-move-05-05` | `london-link-100-ba-1` | 2.24 m | Waterloo | Both | The same tail of `move-05-01`, which shares `london-link-350-b-arrival` with `move-05-05`. |
| `london-link-098-ab-1` | `940GZZLUBNK-road-in-08` | 2.50 m | Bank | Both | The first 20 m of `link-098-ab-1` hold `940GZZLUBNK-move-08-06`, which shares `london-link-100-a-arrival` with `road-in-08`. The line of `link-098-ab-1` passes 0.30 m from the Bank diverge, 23.5 m from its start. |
| `london-link-098-ab-1` | `940GZZLUBNK-access-in` | 3.47 m | Bank | Both | The first 20 m of `link-098-ab-1` hold the junction lanes `move-0X-06`, which share their portals with the roads `road-in-0X`. The first 20 m of `access-in` hold each `road-in-0X`. |
| `london-link-098-ba-2` | `940GZZLUBNK-road-in-08` | 4.55 m | Bank | Both | The last 33.62 m of `link-098-ba-2` hold `road-in-06`, which shares the diverge with `road-in-08`. The end of `link-098-ba-2` is 4.55 m from `road-in-08`. |
| `940GZZLUWLO-road-out-01` | `london-link-350-ab-2` | 6.26 m | Waterloo | Both | The last 66.42 m of `link-350-ab-2` hold `move-05-01`, which shares `london-link-100-b-departure` with `road-out-01`. |
| `940GZZLUWLO-road-out-01` | `940GZZLUWLO-move-01-06` | 0.00 m | Waterloo | Whole network | The reach with the front bound of 66.42 m holds a plane that shares a node with the other reach. With the bound of each lane, the pair is separated. |
| `940GZZLUWLO-road-out-01` | `940GZZLUWLO-move-01-04` | 0.00 m | Waterloo | Whole network | As above. |
| `940GZZLUWLO-road-out-01` | `940GZZLUWLO-move-01-03` | 0.00 m | Waterloo | Whole network | As above. |
| `940GZZLUWLO-road-out-01` | `940GZZLUWLO-move-01-05` | 2.58 m | Waterloo | Whole network | As above. |
| `940GZZLUWLO-move-01-02` | `940GZZLUWLO-road-out-01` | 3.09 m | Waterloo | Whole network | As above. |
| `940GZZLUBNK-road-in-08` | `940GZZLUBNK-move-03-08` | 4.37 m | Bank | Whole network | As above. |
| `940GZZLUBNK-road-in-08` | `940GZZLUBNK-move-02-08` | 5.17 m | Bank | Whole network | As above. |

With the front bound of the whole network, the audit refuses 14 pairs, 10 under 4.0 m, and it has 40 shape pairs.
With the front bound of each lane, it refuses the first 7 pairs, 5 under 4.0 m, and it has 39 shape pairs.
The seed of section 3.1 gives the same 7 refusals.
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

The built index takes 66.42 m for each lane (`reachFront`, `internal/sim/envelope_reach.go:217`).
The front bound of each lane (section 3.1) is over 6.01 m on 6 lanes: 66.42 m on `move-05-01` and `london-link-350-ab-2`, 33.62 m on `move-06-08` and `london-link-098-ba-2`, and 23.81 m on `move-08-02` and `london-link-100-ba-2`.
A lane before a curved junction lane gets its bound, because a pod at the end of the lane can enter the curve.
The route counts are over the 97,032 pairs of station exit and station entry, with the free-flow cost of `laneSeconds`.

### 2.4 Lane length

Under the 40 m of a large lane (`LaneMinimumLength`), the tight network has 33 route and hub lanes:

- At each hub, `access-in` and `access-out` at 25.5 m.
- At each hub, each berth row lane `-in` and `-out`, and each row link `-arrival-link` and `-departure-link`, at 25.0 m: 8 at Bank and 20 at Waterloo.
- `940GZZLUWLO-road-in-01` at 34.3 m.

Preparation refuses the first: `lane "940GZZLUBNK-access-in" must be at least 40 meters long`.
On the wide network, 11 lanes are short: 10 berth links of 37.5 m and the same road.
So the blockers of the shape clearance summary still apply on the tight network, and the tight shape adds 22 more short lanes.
The short road is short on both networks, so it does not depend on the station shape.

### 2.5 Ramp junctions

The probe also ran on `-junctions ramps`.
There, the audit refuses 14 pairs, 11 under 4.0 m, with either front bound, and the ramps split link 100 into lanes of 40.0 m that preparation refuses.
The ramp junctions are not a better base for this preset.

## 3. Changes that remove the refusals

### 3.1 Front bound for each lane

#### Definition

Let `front(M)` be the front bound of lane M from the tail index, and 0 when it has none.
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
The probe measured this: a pod on `london-link-100-ba-2` at 889.16 m, on a route through `move-05-01`, has `940GZZLUBNK-road-in-08` in its envelope, and the index for each lane does not hold it.
The containment test found 3 more failures of the same kind in its route windows, through `move-05-01` and through `move-06-08`.

The proposal: when the network sets `shapeClearance`, the oracle takes a local front.
At a route distance x, the front is the least value that is at least 6 m and at least the front bound of each route lane that ends at x minus the slack or later and starts at x plus the front or earlier.
One pass over the route lanes in order computes it, because a lane starts after each lane before it.
Without the field, the oracle and its reference construction (`envelope_safety_reference_test.go:25`) do not change, so D and each other project keep their envelopes.

This changes approved text, so it needs maintainer approval (decision S1):

- `docs/design/group-physics-design.md:86`: "It includes the lanes ahead on the route, up to the largest front bound of the lanes of the route, also more than one lane ahead."
- `docs/contracts/service-contract-proposals.md:75`: "When the fleet has a large pod, the oracle keeps the envelope of each pod, so the plane exemption of two pods does not change."
- Section 3.4, item 5, of the shape clearance design, which says that D1-B keeps the planes of today's envelope.

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

The physical argument does not change.
A front bound of a lane bounds, by the chord projection of that lane, the path distance in the lane at which a point can come within 6 m of the reference.
The local front keeps the front of each lane between the reference and each lane that the envelope holds.
It drops the fronts of lanes behind the reference and of lanes past the reach, which the argument for the body ahead of the reference does not use.
For a path across two lanes, the argument is the same assumption as today (inferred: no committed test proves that the front encloses the body across lanes).

Measured on the network of section 2.1 with the hub shape of section 3.2 and the Waterloo throat at 90 m, so that preparation passes.
This network keeps `move-05-01` and `move-06-08`, so it has the fronts of 66.42 m and 33.62 m:

- The index for each lane, with the seed, holds the local oracle at each position of 6,284 windows and sampled routes, with route shifts of plus and minus two `restoreTolerance` steps: 0 failures.
  Without the seed it also gives 0 failures there, but on the curves network of `TestEnvelopeReachCurves` it fails 2 times at the start of lane `short`.
  With the seed, the curves network gives 0 failures.
- A disk check walked the route ahead of each sampled position while the path stayed within 6 m of the reference.
  The local oracle and the built oracle missed the same lanes.
  Each miss is on a lane with no geometry tail, where the front is 6 m on a curved lane.
  This is the same for both bounds, and section 9 records it.

#### Effect

| Network | Front bound | Refusals | Under 4.0 m | Shape pairs |
| --- | --- | ---: | ---: | ---: |
| LondonFull, Express on the route | Whole network, 66.42 m | 14 | 10 | 40 |
| LondonFull, Express on the route | Each lane | 7 | 5 | 39 |
| Section 3.5, all changes | Whole network, 33.62 m | 7 | 5 | 39 |
| Section 3.5, all changes | Each lane, with or without the seed | 0 | 0 | 38 |
| Section 3.6, the fallback | Whole network, 7.96 m | 0 | 0 | 23 |

### 3.2 Hub shape

Options:

- A: the tight shape with each part at 41 m at the two hubs: side, throat to entry, entry to the first row, and pitch.
  Each access lane is then 42.4 m, and each berth row lane and row link 41 m.
  Measured: no hub lane is short, and the layout audit of the London generator finds no conflict.
  A part of exactly 40 m gives lanes of 40.0 m that preparation refuses by rounding, so 41 m is the value.
  Cost: the paths from the diverge to the berths grow from 75.5 to 175.5 m today to 124.4 to 288.4 m at Waterloo, and from 75.5 and 100.5 m to 124.4 and 165.4 m at Bank.
- B: the wide shape at scale 0.5 at the two hubs, with a hub pitch of 82 for berth links of 41 m.
  It is not measured on the tight network, and its path to the berths is longer than A (inferred from section 2.7 of the shape clearance design).
- C: D4 at 14 m, with a lane minimum of 28 m.
  The tight lanes of 25 m stay short, so C does not remove the blocker.

Recommendation: A.

### 3.3 Waterloo

Link 100 and link 350 leave Waterloo about 23 degrees apart, and their portals interleave on the junction circle (inferred from the node positions, not from a probe).
So the lanes of the two links cross near the portals, and `move-05-01` is a reversal between adjacent portals.

| Refusal or blocker | Change | Measured |
| --- | --- | --- |
| `road-in-01`, 34.3 m | Throat of Waterloo at 90 m from the junction center, in place of 80 m | `road-in-01` 43.9 m. The other Waterloo roads get 5.8 m to 10.0 m longer. `road-out-02` becomes 39.8 m, and it is off the route, so it needs 24 m only. |
| `road-in-05` and `link-100-ab-2` | Curve `road-in-05` with a control point 30 m from the middle of its chord, on the side away from the junction center (inferred from the node positions) | Refusal removed. `road-in-05` is 75.8 m with the curve and the throat at 90 m, against 58.7 m today. Offsets of 20 m and 40 m away from the center, and of 30 m and 40 m toward it, also remove it. 10 m away leaves 5.21 m, 10 m toward leaves 0.00 m, and 20 m toward leaves 2.70 m. |
| `link-350-ab-2` and `link-100-ba-1`, `move-05-05` and `link-100-ba-1`, `road-out-01` and `link-350-ab-2` | Remove `move-05-01` | All three removed. No free-flow station route uses the lane, so no route gets longer. Emergency and depot routes are not measured. |

The other option for the throat is a control point on `road-in-01`.
It is not measured, and a curve on a large lane gives it a front bound and a tail over the straight values.
The other option for the crossings is a resource that orders the two pods.
None exists for two lanes that share no node, and it would hold Compact pods on link 350 and on the roads.
Recommendation: the three changes of the table.

### 3.4 Bank

The Bank throat is next to link 098: the line of `link-098-ab-1` passes over the diverge, and the end of `link-098-ba-2` is 4.55 m from `road-in-08`.
So the station must turn, and `move-08-06` must go.

The probe turned the station about the junction center, with the hub shape of section 3.2 and `move-05-01` removed:

| Turn | Bank junction lanes removed | Bank refusals |
| ---: | --- | --- |
| 0 degrees | None | 3 |
| 0 degrees | `move-08-06` and `move-06-08` | 2 |
| +5, +10, or +15 degrees | None | 1: `link-098-ab-1` and `road-in-08`, 0.00 m |
| +5, +10, or +15 degrees | `move-08-06` | 0 |
| -5 to -20 degrees, and +20 degrees | None | 1 to 4 |

A positive turn increases the heading of the station axis, which is about -126 degrees today, so the axis turns away from link 098 and toward link 097 (inferred from the node positions).
The rows at 0, +5, +10, and +15 degrees with no lane removed gave the same Bank refusals with hub parts of 41 m and of 40 m.
The rows with lanes removed used parts of 41 m, and the other rows used parts of 40 m.
The heading search of the generator uses steps of 5 degrees, so +5 degrees is one step.
Removing `move-08-06` makes 7 more station routes longer, and the worst route becomes Waterloo to St. Paul's, at +21.4 s with the other changes (section 3.5).
The other option is an ordering resource, as at Waterloo.
Recommendation: turn Bank by +5 degrees, and remove `move-08-06`.

### 3.5 All changes together

The scratch network has these changes, on the network of section 2.1:

1. The hub shape of section 3.2 at Waterloo and Bank.
2. The Waterloo throat at 90 m.
3. Bank turned by +5 degrees.
4. `road-in-05` with its control point.
5. `move-05-01` and `move-08-06` removed, which leaves 9,022 lanes.

Results:

- The audit with the front bound for each lane refuses no pair, with or without the seed.
- No route or hub lane is under 40 m, and preparation passes.
- The layout audit of the generator finds no conflict.
- `projectcheck` at `3db8ce39`, with the order contract `express-v1`, both Express services, and 4 Express pods at free hub berths, stops at the plane reach refusal of `move-01-06` and `road-out-01`.
  Project validation reports this refusal after each other check (`internal/project/config.go:335`), so each other check passes.
  The refusal comes from the bound of the whole network, 33.62 m from `move-06-08`.
  The audit copy with the bound for each lane refuses no pair, so the refusal goes with the change of section 3.1 (inferred, because `projectcheck` did not run with that change).
- The routes are 2,216.0 m from Waterloo to Bank and 2,150.6 m back, about 45 m and 48 m more.

The cost to station routes, over the 97,032 pairs of station exit and station entry, in free-flow seconds:

| Step | Pairs longer | Worst | Total |
| --- | ---: | --- | ---: |
| Hub shapes, Waterloo throat, and Bank turn | 1,242 | +3.4 s, Bank to Waterloo | +1,891.7 s |
| And the curve of `road-in-05` | 1,242 | +3.4 s | +1,895.7 s |
| And `move-05-01` removed | 1,242 | +3.4 s | +1,895.7 s |
| And `move-08-06` removed | 1,249 | +21.4 s, Waterloo to St. Paul's | +2,027.9 s |
| And `move-06-08` and `move-08-02` removed (section 3.6) | 1,264 | +48.4 s, Waterloo to St. Paul's | +2,410.6 s |

No pair becomes unreachable, and with the changes of section 3.5 no pair gets shorter.
These costs omit the longer paths inside the hubs (section 3.2), and the effect on wait and journey times is not measured.

### 3.6 Fallback without the index change

If the maintainer does not approve the oracle change of section 3.1, the preset can also remove `move-06-08` and `move-08-02`.
The bound of the whole network then falls to 7.96 m, and the built audit refuses no pair, with 23 shape pairs.
With the same Express fleet as section 3.5, `projectcheck` at `3db8ce39` reports the project valid.
The cost is 15 more station routes longer, and a worst case of +48.4 s.
The bound of the whole network grows with each curved lane near a large lane anywhere in the network.
So a later hub pair (D9) can bring back refusals at this pair, which the bound for each lane does not do (inferred).

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
| SX3 | E3, Motion and ownership, and Independent safety oracle | The oracle checks the mixed fleet at each tick, with fault stops and emergency journeys, the shape table, the envelope reach, and the D6 oracle. Under decision S1, the indexed and the reference envelope constructions take the local front, and they give the same locations in the same order. |
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
| S1 | Front bound of the envelope reach | A: the bound for each lane of section 3.1, with the seed, and the local front in the oracle behind the field. This amends the approved text of section 3.1. B: keep the bound of the whole network, and remove `move-06-08` and `move-08-02` too (section 3.6). | A. It keeps two junction lanes that 18 station routes use, and later hub pairs do not raise the bound at this pair. B is valid at `3db8ce39` with no sim change, and it is the fallback if A is not approved. |
| S2 | Hub shape | A: tight, each part 41 m. B: wide at scale 0.5 with a hub pitch of 82. C: D4 at 14 m. | A, measured. B is not measured, and C does not remove the short lanes. |
| S3 | Geometry at the portals | A: the Waterloo throat at 90 m, the curve of `road-in-05`, Bank turned +5 degrees, and `move-05-01` and `move-08-06` removed. B: a new ordering resource for two lanes that share no node. | A, measured. B needs a new rule and holds Compact pods on other lines. |
| S4 | Express parking | A: a comb of 16 berths at 40 m by 40 m beside Waterloo, as D8-A says. B: Express pods start at hub berths, with no parking. C: the parking of D, which needs its trunks. | A, after a slice measures its site against the layout audit and the bank audit. The comb adds large lanes near Waterloo, with new tails, fronts, and reach overlaps, so the result of section 3.5 does not cover it. Under S1-B, a curved lane of the comb can also raise the bound of the whole network. B is measured only with 4 pods at the 5 free hub berths, and it takes hub berths from Compact pods. C brings back the trunks that shared track removes. |
| S5 | Preset name and flags | A: the preset `london-full-shared-express`, with no capacity flags, as D. B: a flag on `london-full` that adds the shared route. | A. The qualification then has one geometry, and the network hash pins it. B lets a user combine the route with other capacity flags, but each combination is a new geometry with no evidence. |
| S6 | Qualification | A: the SX rows of section 5, in a new contract proposal. B: E1 to E8 extended to name both presets. | A. The evidence of D does not transfer, and new rows keep the approved E rows of D unchanged. B gives fewer rows, but it changes approved contract text for D. |

S1 and S3 depend on each other: with S1-B, S3 removes two more junction lanes.
S4-A needs its own measurement before the preset is final.

## 7. Test plan and slices

Tests of the front bound for each lane:

- Bound: on each network of `testdata/scenario_networks.json.gz`, D, and the preset, `F'(L)` is at most the bound of the whole network, and each span of the index is a subset of the span of the built index.
- Equality: on each lane whose `F'(L)` equals the bound of the whole network, the two indexes give the same spans.
  The test runs on a network with a large front, such as the preset before `move-05-01` goes, so it covers the walk of the bound.
- Containment: `TestEnvelopeReachHoldsEnvelope` with the local oracle under the field, on the curves network, the Express fixture, the scenario networks, and the preset.
  It checks route distances shifted by two `restoreTolerance` steps each way, and published positions shifted the same way.
  The case "two lanes ahead from the front bound of a lane behind" stays, and it now checks the local oracle.
- Field off: the oracle and the reference construction give the envelopes of today on D and on each scenario network, and `TestBerthEnvelopeMatchesReference` passes.
- Parity of D: the saved-state and stream bytes of D at each checkpoint equal those of the build before the change.
- Disk: on each lane with a geometry tail, the local oracle holds each route lane ahead that the path reaches within 6 m of the reference.
  The probe found no miss on such lanes.
- Audit: the preset refuses no pair, the LondonFull network with Express on the route gives the 7 refusals of section 2.2, and the order and text of a refusal do not change.

Mutations, with the table of the `verify-by-mutation` skill:

| Mutation | Test that fails |
| --- | --- |
| No seed from the lanes into the From node | Containment on the curves network, at the start of `short` (measured) |
| The bound is `max(6 m, front(L))`, with no walk | Containment on a route that ends a lane before a curved lane, such as `link-350-ab-2` before `move-05-01` (inferred) |
| The oracle keeps the route maximum under the field, and the index takes the bound for each lane | Containment, on the route through `move-05-01` onto `link-100-ba-2` (measured) |
| The oracle takes the local front without the field | Field off and parity of D (inferred) |
| The walk without the slack | Containment with shifted route distances (inferred) |

Slices:

0. An amendment proposal for the three approved texts of section 3.1, for maintainer approval.
1. `sim`: the bound for each lane in the index, the local front in the oracle and in the reference construction behind the field, with the tests and mutations above.
   This needs S1-A.
2. `scenarios`: the preset builder, the `cmd/scenario` preset, and tests that pin the audit result and the unchanged hashes of LondonFull and D.
3. `scenarios`: the Express parking of S4, with its site measured by the layout audit and the bank audit.
   The test that pins the network hash of the preset comes after this slice, and so does the qualification evidence.
4. `sim`: commit the probe of section 8 as a study test, as the shape clearance design did with its study probe, so that the counts of this design can be reproduced.
5. `docs`: `docs/reference.md`, `docs/london-full.md`, `docs/operations.md` for the soak presets, and the build record of this design.
6. The contract proposal of section 5, after approval, and then the evidence of each row.
7. Batch 2 of the study plan of the shape clearance design (section 8).

## 8. Commands

The probe of sections 2 and 3 was `internal/sim/zz_d11_probe_test.go`, with a layout check in `internal/scenarios/zz_d11_probe_test.go`.
Both are deleted, so each number of sections 2.2 to 3.6 comes from a deleted probe, except the counts that the study probe gives.
Slice 4 of section 7 commits the probe as a study test.
These commands make the project files and run the parts that remain:

```sh
D=$(mktemp -d)
go run ./cmd/scenario -preset london-full -output "$D/lf.json"
go run ./cmd/scenario -preset london-full -wide-stations -output "$D/lfw.json"
go run ./cmd/scenario -preset london-full -junctions ramps -output "$D/lfr.json"
PODSIM_SHAPE_STUDY="$D/lf.json" go test ./internal/sim -run 'TestShapeClearanceStudyRoutes$' -count=1 -v
go run ./cmd/projectcheck -project "$D/lf.json"
rm -r "$D"
```

To repeat the deleted probe:

1. Load the network as `shapeStudyNetwork` does, set the classes as `TestShapeClearanceStudyRoutes` does for Waterloo and Bank, and set `ShapeClearance`.
2. Copy `auditBankGeometry`, and record each refusal with the smallest distance of its parts, its place, and the result of `shapeCertifier.certify` on the parts.
3. For the bound for each lane, build `newEnvelopeReach`, then fill each entry of its `lanes` with `spans` of a copy of `laneItems` whose ahead walk uses `F'(L)`, and set it as the index of the lazy envelope reach.
4. For the changes of section 3, move the hub nodes in the frame of each station, set `Control` on `road-in-05`, and delete the lanes.
5. Write the project with the changed network, add `express-v1`, both services, and Express pods at free hub berths, and run `go run ./cmd/projectcheck -project FILE`.
6. For the containment checks, build `newEnvelopeReachProbe`, replace its lane spans as in step 3, and drive `positions` on each window with a copy of `appendVehicleSafetyLocations` that takes the local front.
7. For the route costs, run `searchFrom` from each station exit to all nodes, with the station entries out of the forbidden set, and compare the cost at each entry.

## 9. Problems this design does not solve

- No simulation ran.
  The measurements are static: the audit, preparation, the layout audit, and project validation.
  `Check`, the D6 oracle, and the effect on wait and journey times need slices 2 and 6.
- The site of the Express parking of S4-A is not measured, and its large lanes can add refusals.
- The probe is deleted, so the counts of sections 2.2 to 3.6 cannot be reproduced with a committed test until slice 4 lands.
- The local oracle is an oracle change under the field, which amends approved text (S1).
  The fallback of section 3.6 avoids it at a cost in station routes.
- On lanes with no geometry tail, a curved lane keeps a front of 6 m.
  The disk check found 1,169 pairs of a lane and a route lane ahead that the path reaches within 6 m of the reference while neither oracle holds it.
  The 6 m disk is larger than the body, so this is not a measured body overlap.
  It is the same for both bounds and outside this design.
- The cross-lane part of the front bound argument has no committed test today.
  The disk test of section 7 covers it only on lanes with a geometry tail.

## 10. Review

An adversarial review on 2026-11-16 found one blocker and two major problems.

1. Blocker: section 3 shows that the index for each lane holds the local oracle, but not that it holds the physical body.
   The projection bound of one lane gives no common axis across a join.
   For example, a lane can turn back so that a portal 80 m ahead on the route is next to the pod.
   The network-wide bound can hold that plane, and the local oracle drops it.
   S1-A needs a proof that validation excludes such paths, or a bound that covers them, with returning paths, cycles, several joins, both classes, and the 10 m envelope gate of SC6.
   The disk walk of section 3 stops when the path leaves the disk, so it misses a path that comes back.
2. Major: the 1,169 disk misses of section 9 are not classified.
   A lane that admits a large class always has a geometry tail, so each miss is on a small lane, and the case is a small pod against a large pod.
   The 6 m disk is larger than the grown small body, so some misses are harmless.
   Without the witnesses, the misses are neither a shown collision gap nor a shown artifact.
   They need a classification with oriented bodies and portal locations.
3. Major: the measurements of sections 2.2 to 3.6 come from deleted probes, so they cannot be reproduced.
   The probes must be kept as study tests, with the network hashes, the transformations, and the expected outputs, before the design is approved.
