# Shape clearance

Status: approved on 2026-11-16.
Proposed on 2026-11-15, and revised on 2026-11-16 after an adversarial review (section 9).
Build record: on 2026-11-16, the rule of parts 1 to 5 was built behind the `shapeClearance` network field.
The commits are `2c918f46` (the field and digest extension 17, with no effect), `4b09997e` and `f6fd4091` (the envelope reach index by position, built on demand), `29af1248` (shape certification, the shape pair table, the all-pairs audit, the oracle, and debris), `33607784` (the independent body oracle for merge pairs), `4805ba4a` (merge and split pairs admit large and tailed lanes with the union body), `e515829b` (the editor runs the server lane check under the field), and `3d71de3d` (the independent body oracle for shape pairs).
Not built: the section 7 row "routes of section 2.1 with residual geometry fixed", which needs the D11 shared-track geometry, and the applications of section 4 (the D8 Express Parking comb, the D9 demand hubs, and the D11 preset).
The body oracle is stricter than the D6 text.
It fails on any body overlap on a merge pair, because a pod outside its extent clears the other lane.
So the accepted case of the D6 text, where one pod holds the junction and the other is outside its extent, cannot overlap when the extent table is right.
Code references are for `5fe3cc0b`.
The code that they name is the same at `623b19ec`.

This design answers two questions of the maintainer.
The first is how Express can run on shared main-line track, so that a new hub pair needs little new geometry.
The second is why lanes must stay 12 m apart, when only pods on a common path need that gap.
It proposes one rule, shape clearance, for all classes, and three applications of it.
The decisions are in section 6.
Some decisions depend on others, and section 6.1 gives the dependencies and the combinations that this design defines.
The [lateral merge zones](lateral-merge-zones.md) design is the first, narrower step of this rule.

The measurements change the premise of the Express question.
On LondonFull, no bank-audit refusal of a shared Express route comes from two lanes on one plane that are closer than 20 m.
Each refusal comes from the plane reach of the bank audit, which treats a large pod as present on every plane at both ends of its lane.

The recommended plane rule (D1-B) keeps every plane that the envelope of the oracle can hold, but by position on the path and not for the whole lane.
Where two such reaches overlap, the pair gets a swept body test in place of the 20 m distance.
Under this rule, residual refusals remain on each measured route (section 2.2).
On Waterloo and Bank, 46 pairs are closer than 20 m.
Of these, 5 are closer than 4.0 m, and they need new geometry or a resource that orders the pods.
8 more are between 4.0 m and 9.31 m, and the oriented sweep decides them.
On the 12 routes, the counts are 137 pairs under 4.0 m and 63 pairs between 4.0 m and 9.31 m.
Some pairs count on more than one route, so these sums overstate the work.
Most are at station portals and at junctions on the way.
So shared track needs some geometry work at each hub pair.

The other option, D1-A, puts a large pod on the plane of its lane only, as a small pod is.
Under D1-A, the audit refuses no pair of the 12 routes and 15 hubs measured.
But D1-A permits projected body overlap across planes by assumption, and its zero count is not evidence of physical safety.

Two other blockers do not depend on the plane rule:

- Berth links: at station scale 0.5, each berth link after the first row is 37.5 m long, under the 40 m of a large lane.
  Each route has 10 to 16 such links.
- Short roads: four roads at Waterloo and King's Cross are under 40 m, and four routes use one of them.
  The route of Waterloo and Bank uses `940GZZLUWLO-road-in-01` (34.3 m).

## 1. Current behavior

### 1.1 The two clearances

`Clearance` is `4.0 + 8.0` m: a 4 m pod and an 8 m gap (`internal/sim/simulation.go:20`).
`largeClearance` is 20 m, and `largeEnvelopeRadius` is 6 m (`internal/sim/profile_geometry.go:8`): two 6 m radii and the same 8 m gap.
A pair with a large class keeps 20 m, and other pairs keep 12 m (`classPairClearance`, `profile_geometry.go:20`, and `classSetPairClearance`, `:27`).
Both values are center distances, so they are isotropic.
Two pods side by side on parallel lanes need the same distance as two pods on one lane.

A pair is exempt when its two pods are on different planes.
Two locations are on different planes when their separation groups differ and they share no node (`safetyLocationsSeparated`, `internal/sim/safety.go:332`).
A small pod has one location: the lane or the berth of its reference (`indexSafety`, `internal/sim/prepared.go:230`).
When the fleet has a large pod, each pod has an envelope of locations (`largeSafety` and `appendVehicleSafetyLocations`, `internal/sim/envelope_safety.go:13` and `:30`).
The envelope holds each route lane from 20 m, or the longer tail, behind the reference to the front bound ahead of it (`:57`), the origin berth until release (`:73`), and each lane at a berth node (`:83`).
A pair with a large pod is exempt only when every cross-pair of the two envelopes is on different planes (`envelopeLocationsSeparated`, `:89`).
This includes the envelope of the small pod of a mixed pair (`(*separationIndex).pair`, `safety.go:261`).

### 1.2 Where the 20 m rule applies

| Place | Code | Effect on a shared Express route |
| --- | --- | --- |
| Oracle | `(*separationIndex).pair`, `safety.go:266` to `:272` | A pair with a large pod needs 20 m unless the envelopes are separated. |
| Bank audit, distance | `validateBankGeometry`, `internal/sim/bank_geometry.go:136` to `:152` | Each nonincident pair with a large path needs 20 m, unless the plane exemption holds. |
| Bank audit, plane reach | `reachSeparated`, `bank_geometry.go:230`, and `newPlaneReach`, `internal/sim/plane_reach.go:45` | The exemption of a large pair needs every lane at both end nodes of both paths, and the berths at the From nodes, on different planes. It uses the whole lane. The refusal is a `PlaneReachError` (`bank_geometry.go:186`), which preparation holds to the end (`internal/sim/prepared.go:168`). This is refusal 3 of the [Express London study](../studies/express-london.md#refusals). |
| Lane length and cells | `LaneMinimumLength`, `profile_geometry.go:36`, and `validateLargeLaneCells`, `prepared.go:194` | A large lane needs 40 m, and each cell needs 20 m. |
| Geometry tails | `indexGeometryTails`, `profile_geometry.go:59`, and `prepared.go:174` | Each lane at a node of a large lane gets a tail of at least 20 m. A folded lane refuses with "unsupported large-vehicle path shape". |
| Cells and origin | `newLaneCells`, `internal/sim/traffic.go:154`, and `originTailFrom`, `traffic.go:922` | A large lane retains its cells for 20 m, and a large pod keeps its origin for 20 m. |
| Junction zones | `buildJunctionConflicts`, `internal/sim/junction_conflicts.go:53` | A pair at a node with a large lane gets the isotropic 20 m extent. |
| Lateral merges and depot pairs | `lateral_merges.go:43`, `depot_lateral.go:231` | A lane with a large class or a tail is not eligible, so the pairs near the route lose the shorter zones. |
| Retention layer A | `pairPieces`, `internal/sim/retention.go:291`, and the windows of `newLaneRetention`, `retention.go:113` | No reduced retention near a large lane. |
| Depots | `validateContractNetworkRecords`, `internal/sim/order_contract_bounds.go:74`, and `newDepotIndex`, `internal/sim/depot_network.go:226` | A depot slot cannot admit a large class. |
| Placement | `validateInitialSeparation`, `prepared.go:338` | Initial large pods need 20 m. |
| Platoons and coasting | `platoon.go:547`, `:573`, `coast.go:131`, `berth_choice.go:69` | A large pod does not link or coast. These do not depend on the distance. |

Express in project D has dedicated straight trunks, each lane in the plane of its trunk (`connectExpress`, `internal/scenarios/london_full_express.go:295`).
A trunk crosses London lanes on its own plane, and its lanes meet no London node, so the plane reach of each trunk lane holds only its own trunk.
That is why D passes the audit.
The dedicated lanes, the gate stubs, the bend at Bank, and the parking trunks are the geometry that each new pair would repeat ([layout attempts](../studies/express-hub-fixture.md#layout-attempts)).

### 1.3 Where the 12 m rule applies

| Place | Code | Effect |
| --- | --- | --- |
| Oracle | `pair`, `safety.go:266` | Each small pair on one plane needs 12 m, except a window pair, a depot lateral pair, and a lateral merge pair. |
| Bank audit | `bank_geometry.go:85` and `:146` | The audit runs only with banks, large classes, maneuver groups, or depots. With banks or maneuver groups, it checks each nonincident pair on one plane. With only large classes or depots, it skips small pairs outside the depots. LondonFull has maneuver groups and depots, so it runs this audit on all pairs. |
| Lane length | `LaneMinimumLength`, and `docs/model.md:193` | A lane needs 24 m, except slow lanes, depot chains, and maneuver group lanes. |
| Berth pitch | `minimumBerthPitch`, `internal/scenarios/layout_audit.go:17` | A berth row is at least 25 m from the next, because a chain link is a lane of 24 m or more. |
| Junction zones | `junction_conflicts.go:53` | Isotropic 12 m extents, except lateral merge pairs. |
| Debris | `internal/sim/debris.go:137`, incident contract section 7.2 | Debris blocks the cells within 12 m. |

The 24 m lane minimum and the 25 m pitch follow the gap along a path, not the gap across.
Section 2.6 shows that the 12 m gap across lanes does not bind in LondonFull stations at scale 0.5.

## 2. Measurements

Sections 2.1 to 2.4 come from the study probe `internal/sim/shape_clearance_study_test.go`, run on 2026-11-16 at `623b19ec`.
Section 2.11 gives the commands.
The probe reproduces the counts of the deleted scratch probe of 2026-11-15 for the whole reach, the body reach, and the lane plane rules, count for count in each table cell.
The old table also had a "tail reach" column, which the probe does not reproduce.
A variant of the probe that takes all lanes at both nodes gives 95 on Waterloo and Bank, as the old column did, but it differs from the old column by up to 16 pairs on the other routes.
So the old rule is not reconciled.
This revision replaces it with the envelope reach rule, which follows the envelope of the oracle.

Sections 2.5 to 2.10 come from scratch probes on 2026-11-15 at `5fe3cc0b`, which are deleted.
No committed probe reproduces them.
D1 does not rest on them.
D5 uses section 2.5, D8 uses section 2.8, D9 uses section 2.10, and D10 uses sections 2.6 and 2.7.
Values that come from a formula or from reasoning, and not from a probe, are marked inferred.

The base is the default LondonFull preset: station scale 0.5, `lateralMerges` set, 5,740 nodes, 9,024 lanes, and network SHA-256 `441d6f3f70b16e7bcf433e315ec6735c2449e2de066975ccc1683aa95798362d`.
Since November 16, 2026, `-wide-stations` gives this network, because the default stations are tight.
For each pair, the probe finds the free-flow route from the exit of one hub to the entry of the other in each direction, through no other station.
It sets the classes Legacy, Compact, and Express on the route lanes, on each lane of the two hubs except the roads off the route, and on the hubs and their berths.
It then runs a copy of the pair scan of `validateBankGeometry`.
For each pair with a large path, the copy records the smallest distance under each plane rule, in place of the first refusal.
On project D, `london-full-express`, the copy refuses no pair under any rule, as the audit does.

### 2.1 What refuses a shared route

Each column counts the lane and berth pairs of a pair of hubs that one plane rule refuses at one clearance.
These are bank-audit refusals.
Route validation has other refusals, such as the lane length of section 2.4, which these columns do not count.

- Whole reach: the audit of today (section 1.2).
- Envelope reach: the planes that the envelope of section 1.1 can hold at each position, for any route through the lane.
  A lane has the planes of the lanes into its From node in its first 20 m, or the longer tail of those lanes.
  When its From node is a berth node, the lane also has that berth and the lanes at its node, up to the origin tail of the lane.
  A lane has the planes of the lanes out of its To node in its last 6 m, or the longer front bound of the lane.
  A berth has its own plane and the planes of the lanes at its node.
  The envelope adds only the lanes of the route of the pod (`envelope_safety.go:55` to `:68`), so the rule does not take the other lanes at these nodes.
  This is the rule of D1-B.
- Body reach: the planes of the lanes into the From node in the first 6 m, and of the lanes out of the To node in the last 6 m.
  It drops the retained tail and the origin berth.
  This is the rule of D1-C.
- Lane plane: a large path has the plane of its own lane or berth only, as a small path has.
  This is the rule of D1-A.

The envelope of the oracle also holds the small pod of a mixed pair, so the probe gives each rule to small paths too.
The probe takes the front bound of a lane from that lane only, while the envelope takes the largest front bound of the route.
It also adds the lanes of one node back only.
On the measured routes, no lane is curved and each tail is 20 m, which is shorter than each lane, so these limits do not change a count (inferred).
The built envelope reach index takes the largest front bound of the network for each lane, and that changes the counts (section 2.2, "Counts of the built audit").

| Pair | Route, each way | Stations passed | Whole reach, 20 m (today) | Whole reach, 12 m | Envelope reach, 20 m | Envelope reach, 12 m | Body reach, 20 m | Body reach, 12 m | Lane plane, 20 m or 12 m |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Waterloo and Bank | 2,247 m and 2,178 m | 0 | 105 | 70 | 46 | 22 | 37 | 17 | 0 |
| Paddington and Liverpool Street | 7,438 m and 7,455 m | 8 | 285 | 200 | 129 | 62 | 119 | 60 | 0 |
| Waterloo and Canary Wharf | 7,275 m and 7,203 m | 4 | 78 | 66 | 25 | 14 | 23 | 14 | 0 |
| Tottenham Court Road and Liverpool Street | 3,791 m and 3,741 m | 4 | 168 | 133 | 59 | 32 | 56 | 29 | 0 |
| Victoria and Oxford Circus | 2,303 m and 2,318 m | 1 | 118 | 90 | 43 | 16 | 36 | 15 | 0 |
| London Bridge and Canary Wharf | 5,426 m and 5,409 m | 2 | 18 | 17 | 3 | 2 | 3 | 2 | 0 |
| London Bridge and Waterloo | 2,024 m and 2,090 m | 1 | 71 | 57 | 23 | 12 | 21 | 12 | 0 |
| King's Cross St. Pancras and Victoria | 4,603 m and 4,628 m | 4 | 214 | 183 | 93 | 55 | 80 | 55 | 0 |
| King's Cross St. Pancras and Liverpool Street | 3,535 m and 3,447 m | 3 | 111 | 83 | 27 | 6 | 23 | 6 | 0 |
| King's Cross St. Pancras and Waterloo | 4,041 m and 3,976 m | 6 | 159 | 128 | 54 | 18 | 44 | 18 | 0 |
| Paddington and King's Cross St. Pancras | 4,218 m and 4,209 m | 4 | 216 | 147 | 106 | 54 | 97 | 52 | 0 |
| Euston and Waterloo | 3,827 m and 3,696 m | 6 | 113 | 96 | 37 | 14 | 28 | 14 | 0 |

Each refusal in the table is a reach refusal.
No refused pair is on one plane: the two directions of a link share a plane and are 36 m apart (`londonTrackOffset` of 18 m, `internal/scenarios/london.go:20`).
A smaller clearance alone clears 6% to 33% of the refusals, for example 35 of 105 for Waterloo and Bank.
Envelope reach alone clears 51% to 83%, and body reach alone clears 55% to 83%.
Only the lane plane rule clears all of them.
The route of Waterloo and Bank is the Waterloo and City link, with no station between the hubs.
No route lane is curved, and the front bound of each route is 6 m.

### 2.2 The residuals of envelope reach and body reach

These are the refusals at 20 m, by the smallest distance between the parts of the two paths whose planes overlap.
Each cell gives the envelope reach count first and the body reach count second.

The bounds come from section 3.2.
A pair under 4.0 m fails the side-by-side minimum when both lanes admit Express, and a pair under 3.55 m fails it for each pairing of classes.
So no body test clears such a pair, except a pair from 3.55 m to 4.0 m with a small-only lane.
Four residuals of the table are in that range, at 3.72 m and 3.98 m, on four routes.
The route lanes admit small classes and Express, so the union body of section 3.2 applies, and its disk bound is 9.31 m.
A pair at 9.31 m or more passes the disk bound, so a body test clears it (inferred from the disk bound, because no body sweep ran).
The pairs between need the oriented sweep.
The column from 8.96 m to 9.31 m holds the pairs that the per-class bound of 8.96 m clears and the union bound does not.

| Pair | Under 4.0 m | 4.0 to 8.96 m | 8.96 to 9.31 m | 9.31 to 12 m | 12 to 20 m | Where, envelope reach |
| --- | --- | --- | --- | --- | --- | --- |
| Waterloo and Bank | 5 / 2 | 5 / 6 | 3 / 3 | 9 / 6 | 24 / 20 | Bank 29, Waterloo 15, links 2 |
| Paddington and Liverpool Street | 30 / 29 | 11 / 10 | 3 / 3 | 18 / 18 | 67 / 59 | Baker Street 50, King's Cross 42, Paddington 17, links 11, Liverpool Street 7, Moorgate 2 |
| Waterloo and Canary Wharf | 4 / 3 | 3 / 4 | 0 / 0 | 7 / 7 | 11 / 9 | Waterloo 14, London Bridge 7, Canary Wharf 2, links 2 |
| Tottenham Court Road and Liverpool Street | 21 / 14 | 3 / 6 | 1 / 1 | 7 / 8 | 27 / 27 | Bank 41, Holborn 10, Liverpool Street 3, links 3, Tottenham Court Road 2 |
| Victoria and Oxford Circus | 10 / 10 | 2 / 2 | 0 / 0 | 4 / 3 | 27 / 21 | Green Park 19, Oxford Circus 18, Victoria 4, links 2 |
| London Bridge and Canary Wharf | 1 / 1 | 1 / 1 | 0 / 0 | 0 / 0 | 1 / 1 | Canary Wharf 2, London Bridge 1 |
| London Bridge and Waterloo | 3 / 2 | 2 / 3 | 0 / 0 | 7 / 7 | 11 / 9 | Waterloo 14, London Bridge 7, links 2 |
| King's Cross St. Pancras and Victoria | 23 / 23 | 6 / 6 | 0 / 0 | 26 / 26 | 38 / 25 | King's Cross 33, Oxford Circus 21, Green Park 19, links 6, Euston 5, Warren Street 5, Victoria 4 |
| King's Cross St. Pancras and Liverpool Street | 1 / 1 | 3 / 3 | 1 / 1 | 1 / 1 | 21 / 17 | King's Cross 17, Liverpool Street 7, Moorgate 2, links 1 |
| King's Cross St. Pancras and Waterloo | 5 / 5 | 4 / 4 | 2 / 2 | 7 / 7 | 36 / 26 | King's Cross 14, Waterloo 13, Holborn 10, Leicester Square 6, Charing Cross 5, Embankment 4, links 2 |
| Paddington and King's Cross St. Pancras | 30 / 29 | 9 / 8 | 0 / 0 | 15 / 15 | 52 / 45 | Baker Street 50, King's Cross 29, Paddington 17, links 10 |
| Euston and Waterloo | 4 / 4 | 4 / 4 | 0 / 0 | 6 / 6 | 23 / 14 | Waterloo 13, Warren Street 6, Charing Cross 5, Embankment 4, Tottenham Court Road 3, Euston 2, Leicester Square 2, links 2 |

A pair counts at the station of its first lane, or of its second lane when the first is a link.
A junction movement lane counts at the station that its ID names.
Most residuals are at station portals.
A lane that ends at a portal crosses, within a few meters of the portal, a lane that joins a plane of that portal.
On Paddington and Liverpool Street, the largest counts are at junctions on the way, Baker Street and King's Cross, and not at the hubs.
A residual under 4.0 m needs new geometry, or a resource that orders the two pods before a body reaches the joint.

On Waterloo and Bank, the 13 envelope residuals under 9.31 m are these:

- Under 4.0 m: `940GZZLUWLO-road-in-05` crosses `london-link-100-ab-2`, and `london-link-350-ab-2` crosses `london-link-100-ba-1`.
  `940GZZLUWLO-move-05-05` is 2.24 m from `london-link-100-ba-1`.
  `london-link-098-ab-1` is 2.50 m from `940GZZLUBNK-road-in-08` and 3.47 m from `940GZZLUBNK-access-in`.
- From 4.0 m to 9.31 m: 6 more pairs at Waterloo and 2 at Bank, from 4.55 m to 9.27 m.

Counts of the built audit: the built audit gives higher counts than this section on Waterloo and Bank.
On the tight LondonFull network, the variant of section 2.11 with `shapeClearance` gives 40 shape pairs and 14 refusals.
10 of the refusals are under 4.0 m.
The wide network gives the same counts.
Each refusal is a reach refusal whose body sweep finds a collision.
No refusal includes a berth or a berth link.
The difference comes from the front reach.
The study probe takes the front bound of each lane, which is 6 m on this route.
The envelope reach index takes the largest front bound of the network, which is 66.42 m on LondonFull, from the curved lane `940GZZLUWLO-move-05-01`.
With a front reach of 6 m, the audit gives 7 refusals: the 5 pairs under 4.0 m of this section, and 2 pairs at 4.55 m and 6.26 m.
The longer front reach adds 7 refusals at the two portals of link 100, and 5 of them are under 4.0 m.
On project D, the Waterloo b bank takes the road to link 100, so the route runs 3,547 m through 5 stations.
There, the study probe gives 18 pairs under 4.0 m, and the audit gives 28 refusals, 24 of them under 4.0 m.

### 2.3 Each hub alone

The probe also set the classes on all lanes of one hub, roads included, with no route.

| Hub | Whole reach, 20 m | Envelope reach, 20 m | Envelope reach, 12 m | Body reach, 20 m | Body reach, 12 m | Envelope reach residuals at 20 m, under 4.0 / 4.0 to 9.31 / 9.31 to 20 m | Large lanes under 40 m |
| --- | ---: | ---: | ---: | ---: | ---: | --- | --- |
| Paddington | 105 | 15 | 4 | 10 | 4 | 3 / 1 / 11 | 6 berth links of 37.5 m |
| Euston | 68 | 23 | 13 | 13 | 11 | 9 / 0 / 14 | 4 berth links |
| King's Cross St. Pancras | 288 | 43 | 14 | 25 | 13 | 4 / 6 / 33 | 8 berth links, `road-in-02` 36.1 m, `road-out-04` 38.8 m |
| Liverpool Street | 108 | 20 | 6 | 14 | 6 | 1 / 4 / 15 | 6 berth links |
| London Bridge | 49 | 10 | 2 | 5 | 2 | 2 / 0 / 8 | 6 berth links |
| Waterloo | 180 | 28 | 13 | 16 | 12 | 6 / 4 / 18 | 8 berth links, `road-in-01` 34.3 m, `road-out-02` 30.0 m |
| Victoria | 44 | 10 | 3 | 4 | 3 | 2 / 0 / 8 | 6 berth links |
| Charing Cross | 18 | 3 | 1 | 2 | 1 | 0 / 0 / 3 | 4 berth links |
| Cannon Street | 3 | 0 | 0 | 0 | 0 | 0 / 0 / 0 | 2 berth links |
| Tower Hill | 20 | 6 | 0 | 3 | 0 | 0 / 0 / 6 | 4 berth links |
| Marylebone | 3 | 0 | 0 | 0 | 0 | 0 / 0 / 0 | 2 berth links |
| Bank and Monument | 476 | 57 | 27 | 37 | 21 | 15 / 5 / 37 | 2 berth links |
| Canary Wharf | 9 | 3 | 2 | 3 | 2 | 0 / 2 / 1 | 6 berth links |
| Tottenham Court Road | 44 | 9 | 4 | 4 | 4 | 2 / 1 / 6 | 6 berth links |
| Oxford Circus | 203 | 36 | 16 | 23 | 13 | 8 / 4 / 24 | 6 berth links |

The lane plane rule refuses no pair at any hub, at 20 m or 12 m.
No hub has a folded lane.
Fenchurch Street is not a LondonFull site.
Tower Hill is its nearest site, and this design uses Tower Hill in its place.

### 2.4 Lane length

At station scale 0.5, the pitch of the berth rows is 37.5 m, so each berth link after the first row is 37.5 m long.
That is under the 40 m of a large lane, so each hub with two or more berth rows refuses Express on its berth links.
The routes of section 2.1 have 10 to 16 such berth links each.
Four roads are also under 40 m: two at Waterloo and two at King's Cross.
Four routes use one of them: Waterloo and Bank uses `940GZZLUWLO-road-in-01` (34.3 m), King's Cross St. Pancras and Waterloo and Euston and Waterloo use `940GZZLUWLO-road-out-02` (30.0 m), and King's Cross St. Pancras and Victoria uses `940GZZLUKSX-road-out-04` (38.8 m).
A road runs from a portal to the throat, and the station scale does not appear to change it, so these four roads are short at each scale (inferred, not measured at another scale).
Each cell of these lanes is also under 20 m.

### 2.5 Cost to Compact pods

This measurement is from a deleted scratch probe.
It compared the junction tables and the lateral merge pairs of the base with those of a network with one shared route.

| Pair | Lanes with a longer zone | Zone length added | On Express lanes | On other lanes | Lateral merge pairs |
| --- | ---: | ---: | ---: | ---: | --- |
| Waterloo and Bank | 99 | 2,355 m | 711 m | 1,644 m | 11,483 to 10,975 |
| Paddington and Liverpool Street | 251 | 6,598 m | 1,736 m | 4,862 m | 11,483 to 10,597 |
| London Bridge and Canary Wharf | 95 | 1,953 m | 1,022 m | 931 m | 11,483 to 11,353 |

Most of the added zone length is on lanes that Express does not use, because each lane at a node of a large lane gets a tail and loses its lateral merge pairs.
The effect on wait and journey times is not measured.

### 2.6 Gaps between lanes

These counts are from a deleted scratch probe.
They cover all of LondonFull, for nonincident pairs that share no node.

On one plane, the pairs that are closer than 40 m are these:

| Kind | Under 12 m | 12 to 30 m | 30 to 40 m | Smallest |
| --- | ---: | ---: | ---: | --- |
| Lanes of one passenger station | 0 | 0 | 2,806 | 37.50 m, the pitch |
| The two directions of a link | 0 | 0 | 1,432 | 36.00 m |
| Depot lanes and slots | 464 | 608 | 140 | 6.00 m |
| Other lane pairs | 0 | 792 | 200 | 12.01 m, at Regent's Park, a Compact station |
| Other pairs with a berth | 0 | 80 | 2,029 | 12.01 m, at Regent's Park |

So no ordinary passenger station has two lanes on one plane closer than its pitch.
The 12 m gap across lanes does not set the size of an ordinary London station at scale 0.5.
It binds only where the generator puts lanes at its floor, as at the bays of Regent's Park at 12.01 m and in the depots.
The "other" pairs were not classified further.

On different planes, the pairs that are closer than 20 m are these:

| Kind | Under 4 m | 4 to 8.65 m | 8.65 to 12 m | 12 to 20 m |
| --- | ---: | ---: | ---: | ---: |
| Link and link | 115 | 27 | 29 | 63 |
| Link and station lane | 616 | 326 | 312 | 808 |
| Station lane and station lane | 6,977 | 486 | 438 | 1,081 |

The bins use the earlier bound of 8.65 m, and the probe is deleted, so they are not binned again at 8.96 m or 9.31 m.
A station lane here includes the junction movement lanes and the roads, which cross each other on their own planes.
These pairs need no clearance under the lane plane rule.
Under a reach rule, a pair of them refuses when a large lane makes its reaches overlap.

### 2.7 Stations

This measurement is from a deleted scratch probe.

| Layout | Pitch | Validates | Berths measured | Median diverge to berth |
| --- | ---: | --- | ---: | ---: |
| LondonFull, scale 0.5 | 37.5 m | Yes | 702 | 195.7 m |
| The same with `BerthPitch` 50 | 25.0 m | Yes | 702 | 183.2 m |

The pitch of 25 m is the floor of `minimumBerthPitch`, and it validates today.
It saves 12.5 m on the median path, because 216 of 309 stations have two berths.
Most of the path is the fixed part (inferred from the shape constants of `internal/scenarios/london_heading.go:205`): about 63 m from the diverge to the entry, 45 m from the entry to the first row, and 50 m on the berth lane.
With each of these three lanes at the 24 m minimum, the median would be about 97 m at pitch 25 m (inferred, not built, and not validated).
The throat stays 80 m from the junction, which keeps the access lanes off the junction guideways ([station scale study](../studies/station-scale.md#shape)).

Since November 16, 2026, the default stations are tight, and `-wide-stations` gives the scale 0.5 layout of this table.
A tight station has each access lane, the berth lane, and the pitch at 25 m, and its median path from the diverge to a berth is 100.5 m ([tight stations](../studies/station-scale.md#tight-stations)).

### 2.8 Express Parking

This measurement is from a deleted scratch probe.
It replaced the parking of D with a comb of 16 berths, built as `addExpressParking` builds it, and ran project validation.

| Row pitch | Berth lane length | Footprint | Validates |
| ---: | ---: | --- | --- |
| 60 m (D) | 100 m | 200 m by 960 m | Yes |
| 40 m | 40 m | 80 m by 640 m | Yes |

The 40 m comb is valid under the current rules, and it uses about one quarter of the area.
Its pitch is the chain link length, so the 40 m minimum of a large lane binds, and a rule across lanes does not make it smaller.
The probe kept the gate stubs of the parking, so its exit stub is longer than in D.

Straight-line distances from the center of each parking site to each hub, in km:

| Site | Waterloo | Bank | Paddington | Liverpool Street | London Bridge | Canary Wharf | Victoria | Oxford Circus | King's Cross |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Express Parking of D | 6.8 | 5.8 | 7.5 | 5.4 | 6.7 | 9.1 | 8.2 | 6.3 | 4.1 |
| North depot | 6.8 | 5.8 | 7.3 | 5.5 | 6.7 | 9.3 | 8.1 | 6.1 | 4.0 |
| West depot | 7.6 | 9.3 | 4.1 | 10.1 | 9.5 | 14.3 | 5.3 | 5.9 | 7.9 |
| East depot | 6.1 | 4.3 | 9.8 | 3.5 | 4.3 | 2.8 | 8.4 | 7.8 | 6.4 |

The route from the parking of D to Waterloo is 7,063.6 m ([hub fixture](../studies/express-hub-fixture.md#layout-attempts)).

### 2.9 Project D at scale 0.5

This measurement is from a deleted scratch probe.
It built D on LondonFull at scale 0.5 and ran project validation.
It refuses with `lane "940GZZLUBNK-road-in-05" must be at least 24 meters long`.
`addExpressBanks` assumes scale 1: it takes the station axis from an entry-to-exit distance of 200 m, and it places the second bank at fixed distances of up to 360 m (`london_full_express.go:150` to `:155`, `:169`, and `:180`).
Six Express lanes are under 40 m: the 37.5 m berth links of the rows that stay at Waterloo and Bank.
Validation stops at its first refusal, so the plane reach of D at scale 0.5 is not measured.
At scale 1, the same probe validates D.

### 2.10 Demand of the hub pairs

These values are from a deleted scratch probe that read the `flows` of the project profile.
From the Tuesday-to-Thursday matrix of the LondonFull profile, journeys in both directions over all bands, and the AM peak rate of each direction, first from the first hub of the pair:

| Pair | Journeys | AM peak, passengers a minute |
| --- | ---: | --- |
| Waterloo and Bank | 23,931 | 54.8 and 0.6 |
| Paddington and Liverpool Street | 12,347 | 15.6 and 4.4 |
| Waterloo and Canary Wharf | 11,767 | 23.4 and 2.2 |
| Liverpool Street and Tottenham Court Road | 11,582 | 7.2 and 1.8 |
| Victoria and Oxford Circus | 11,565 | 12.2 and 0.9 |
| London Bridge and Canary Wharf | 11,120 | 19.6 and 3.1 |
| London Bridge and Waterloo | 9,821 | 2.4 and 7.8 |
| King's Cross St. Pancras and Victoria | 7,675 | 4.8 and 4.2 |
| King's Cross St. Pancras and Liverpool Street | 7,411 | 5.6 and 3.7 |
| King's Cross St. Pancras and Waterloo | 6,688 | 2.2 and 6.2 |
| Paddington and King's Cross St. Pancras | 6,576 | 4.4 and 2.6 |
| Euston and Waterloo | 5,648 | 3.6 and 3.6 |

The last six are the largest pairs of two termini after Paddington and Liverpool Street.
Charing Cross, Cannon Street, Tower Hill, and Marylebone each have 3,310 journeys or fewer with each other hub.

### 2.11 Commands

The study probe is `internal/sim/shape_clearance_study_test.go`.
Its tests skip unless `PODSIM_SHAPE_STUDY` holds the absolute path of a project file.
The path must be absolute, because `go test` runs in the package directory.
`TestShapeClearanceStudyBase` runs the audit copy on an unchanged project and fails if it refuses a pair.
`TestShapeClearanceStudyRoutes` gives sections 2.1, 2.2, and 2.4, and the pairs under 9.31 m of each route.
`TestShapeClearanceStudyHubs` gives section 2.3, with the large lanes under 40 m of each hub.
Each test takes less than 10 s.

The project files are about 15 MB each, so the commands write them to a scratch directory.

```sh
D=$(mktemp -d)
go run ./cmd/scenario -preset london-full -wide-stations -output "$D/lf.json"
go run ./cmd/scenario -preset london-full-express -output "$D/lfx.json"
PODSIM_SHAPE_STUDY="$D/lfx.json" go test ./internal/sim -run 'TestShapeClearanceStudyBase$' -count=1 -v
PODSIM_SHAPE_STUDY="$D/lf.json" go test ./internal/sim -run 'TestShapeClearanceStudyRoutes$' -count=1 -v
PODSIM_SHAPE_STUDY="$D/lf.json" go test ./internal/sim -run 'TestShapeClearanceStudyHubs$' -count=1 -v
rm -r "$D"
```

The deleted probes of sections 2.5 to 2.10 were `internal/sim/zz_xshared_probe_test.go` and `internal/scenarios/zz_xshared_probe_test.go`.

## 3. Proposed rule

### 3.1 Two kinds of pairs

A following pair is two pods where one can come up behind the other on a common path.
This is two pods on one lane, or on two lanes where one ends at the node where the other starts.
A lateral pair is any other pair on one plane: two lanes that merge into one node, two lanes that split from one node, and two lanes that share no node.

The rule keeps the isotropic gap for following pairs, along the direction of travel.
It gives lateral pairs a body test.
The tick-rate contract proves the following gap with distance only (section 1.2), and this design does not change that proof.

### 3.2 Bodies

| Body | Behind the reference | Ahead | Each side | Source |
| --- | ---: | ---: | ---: | --- |
| Small | 2.5 m | 3.7 m | 1.05 m | `lateralRear`, `sweepFront`, `sweepHalfWidth` (`depot_lateral.go:22`, `maneuver_certify.go:17`): a 4.0 m by 1.6 m body, 0.5 m end margin, 0.25 m side margin |
| Express and Group | 3.5 m | 3.5 m | 1.5 m | A centered 6 m by 2.5 m body (`order_contract.go:56`, `vehicle_class.go:46`, and the class table of the group physics design), with the same margins |
| Union | 3.5 m | 3.7 m | 1.5 m | The union of the two bodies above, for a lane that admits a small class and a large class |

The small body and the large body are not nested.
The small body reaches 0.2 m further ahead.
The large body reaches 1.0 m further behind and 0.45 m further to each side.
So a static extent of a lane that admits both kinds of class uses the union body, and not the body of the largest class.

The growth is rectangular, as in `lateralBody` and `newSweepBody` (`depot_lateral.go:39`, `maneuver_certify.go:107`).
Each face of each body moves out by half of a margin of 1 m, so each body is 1 m longer and 1 m wider.
The grown corner radii are `hypot(4.2, 1.55)` = 4.477 m for small, `hypot(4, 2)` = 4.473 m for Express, and `hypot(4.2, 2)` = 4.652 m for the union, each rounded up.

| Pair | Side by side, center distance | Disk bound, rounded up to 0.001 m | Disk bound, rounded up to 0.01 m |
| --- | ---: | ---: | ---: |
| Small and small | 3.1 m | 8.954 m | 8.96 m |
| Express and small | 3.55 m | 8.950 m | 8.95 m |
| Express and Express | 4.0 m | 8.945 m | 8.95 m |
| Union and small | 3.55 m | 9.129 m | 9.13 m |
| Union and Express | 4.0 m | 9.125 m | 9.13 m |
| Union and union | 4.0 m | 9.304 m | 9.31 m |

The side-by-side value is the sum of the two grown half widths.
Two disjoint grown bodies keep their centers at least this far apart.
Two straight parallel lanes at exactly this distance give bodies that touch, so they are not a shape pair (see tangency below).
A shape pair of two straight lanes needs more than this distance, for example 0.01 m more.
The disk bound is the sum of the two grown corner radii.
Two references at the disk bound or more have disjoint bodies at all headings.

The 6 m envelope radius of the contract is a common bound for Group and Express, and the group physics design allows a smaller per-class envelope after qualification.
The Physical geometry gate of the Express physical contract (`:481`) tests a centered 10 m body within the 6 m envelope.
That gate stays a test of the envelope, which following pairs, through junction pairs, and the planes of D1-B keep.
The body tests of this design use the 6 m class body, which is shorter than the 10 m gate body.
So section 5 states that a merge, split, or shape pair certifies the class body only.
A class body longer than 6 m needs new sweeps and new disk bounds.

Tangency: the sweep certifies a pair only when a separating axis shows a gap of more than `sweepGuard` (1e-7 m) between the grown bodies at each pose (`maneuver_certify.go:402`).
Two grown bodies that touch are not disjoint.
A sweep that stops at its tolerance of 1e-3 m (`sweepPotential`) or at its work allowance (`sweepUnresolved`) does not certify the pair.
Such a pair keeps the isotropic rule, as a depot lateral pair and a lateral merge pair do (`depot_lateral.go:138`, `lateral_merges.go:278`).
The oracle accepts a pair at its side-by-side distance less `separationTolerance` (1e-6 m), so an Express pair at 4.0 m passes and at 3.99 m fails.

### 3.3 The rule

Shape clearance has five parts.

1. **Planes.**
   The plane rule is decision D1.
   Under D1-B, the recommendation, the oracle keeps the envelope of section 1.1 for each pod of a fleet with a large pod.
   The audit replaces the whole-lane reach with the envelope reach of section 2.1.
   Where the envelope reaches of two paths overlap on a part of each path, the pair is not exempt on those parts, and part 4 applies to them.
   Under D1-A, a pod of each class is on the plane of the lane or the berth of its reference, and the audit drops the plane reach.
2. **Following pairs.**
   They keep 12 m, or 20 m with a large pod.
   The lane minimum, the cells, the tails, the origin retention, and the junction extents of through pairs do not change.
3. **Merge and split pairs.**
   The lateral merge rule applies to each class.
   The body of each lane is the union of the bodies of the classes that the lane admits.
   Clause 3 of that design drops: a large lane and a lane with a tail can form a pair.
   Clause 5 stays: a pair whose body range reaches the far end of a lane keeps the isotropic extent.
4. **Pairs that share no node.**
   The audit sweeps the grown union body of each lane over the lane and over each join turn at both ends of the lane, as clause 5 encloses a join turn.
   If the two swept regions are disjoint, the pair is a shape pair.
   If they meet, or the sweep does not resolve, the pair keeps 12 m or 20 m, and the audit refuses it if it is closer.
5. **Audit coverage.**
   When the network sets the field, the bank audit runs on each pair, small pairs included (decision D12, option A).

Each shape pair goes into a table, as the depot lateral pairs and the lateral merge pairs do (`depotLateral`, `depot_lateral.go:47`).
The oracle reads the table.
A shape pair, or a merge or split pair, needs the side-by-side distance of the classes of its two pods, and each other pair keeps its rule.
The static extents use the union body of the lanes, and the oracle threshold uses the bodies of the actual pods.
The union body holds each class body, so a pair that the union sweep clears is clear for each pair of admitted classes.

### 3.4 Safety argument

1. A following pair keeps the path gap of section 1.2 of the tick-rate contract.
   Parts 2 and 3 do not change the cells, the tails, or the release.
2. A merge or split pair keeps the argument of section 3 of the lateral merge design: the junction of the node orders the pods, and a pod outside its extent clears each body position on the other lane.
   The union body changes only the body range.
   This argument holds only while the junction orders the pods.
   Two grown Express bodies near one line on a shallow merge overlap at center distances up to about 8 m.
   So `Check` at 4.0 m does not see such an overlap, and D6 adds an independent oracle.
3. A shape pair is safe at every pair of positions.
   The audit proves that the grown union bodies are disjoint for each position on one lane and each position on the other, so the result does not depend on time, speed, or which pod is where.
   No resource is needed to order the pods.
   This holds at each tick rate, as section 1.2 of the tick-rate contract requires.
   It holds only for pairs that the audit checks, which is why part 5 runs the audit on each pair.
4. The argument assumes a guideway-bound pod: its reference stays within 0.25 m of its lane, its heading follows the lane, and a fault stops it on its lane.
   Lateral merges assume this at line speed for pairs that a junction orders.
   This rule extends it to pairs that no resource orders, at line speed.
   The depot rule `3.1 + v²/2` keeps a stop term for the same kind of pair, and that term is 101 m at 14 m/s.
   Decision D3 chooses between the two.
5. Under D1-B, the planes of each pod are those of today's envelope, so the plane exemption does not change.
   The audit takes the envelope reach by position, and not by whole lane.
   Each envelope plane of a pod at a position is in the reach of that part of its path, so the audit covers each pair of envelopes that the oracle can see.
   Where the reaches overlap, part 4 replaces the 20 m distance with the body test.
   The retained tail, the unreleased origin berth, and the incident planes at berths stay in the envelope, and their resources stay retained.
6. Under D1-A, the plane exemption uses the reference location only.
   The body of a large pod reaches 3.5 m behind and 1.5 m to the side of its reference, and the small body reaches 3.7 m ahead.
   So D1-A permits projected body overlap across planes by assumption.
   It also drops the envelope of the small pod of a mixed pair.
   This is an assumption of the plane model, and not a derived result.
   The zero count of D1-A in section 2.1 is not evidence of physical safety.
7. The audit has no braking term, because a pod stops inside the cells that it owns.
   The class rates of Express do not change a lateral result.

### 3.5 Places that change

| Place | Change |
| --- | --- |
| `pair`, `safety.go:261` | Under D1-B, the envelopes stay. Under D1-A, large pods use their lane location. A shape pair or a merge pair from the tables needs the side-by-side distance of its two classes. |
| `largeSafety`, `envelope_safety.go:13` | Under D1-B, no change. Under D1-A, not used for the plane test when the field is set. |
| `validateBankGeometry`, `bank_geometry.go:82` | Under D1-B, envelope reach in place of whole reach, with part 4 on the overlapping parts. Under D1-A, no plane reach. When the field is set, the early return at `:85` and the skip of small pairs at `:146` do not apply (D12). |
| `newPlaneReach`, `plane_reach.go:45` | Under D1-B, an envelope reach index by part of a path. Under D1-A, not used when the field is set. |
| `newMergeLateral`, `lateral_merges.go:35` | Eligibility takes large lanes and lanes with tails, with the union body of the admitted classes of each lane. |
| Shape pair table | New, built at preparation beside `newDepotLateral`. |
| Debris footprint, `debris.go:137` | The shape partners within 12 m join the footprint, as the lateral merge partners do. |
| Physical restore, `state_physical.go` | Follows `Check`, with the shape table. |
| `docs/model.md:37`, `:38`, and the junctions section | Text changes. |

### 3.6 Gate

The rule goes behind a network field, `shapeClearance`, with `omitzero` and `digest:"ext=17"`.
17 is the next free extension number (`internal/session/digest_extension_test.go:438`).
A network field with a safe zero value keeps `stateVersion` and `StreamVersion`, as the lateral merge field did.
When the field is set, `lateralMerges` has no further effect, because part 3 includes it.
`london-full-express` does not set the field, so D does not change.
The field and `express-v1` are independent, so a plain project can use the rule for small pods only.
Under D12 option A, such a project then runs the all-pairs audit.

### 3.7 Interactions

- Banks: a bank is a station of berths and lanes, so its lanes follow parts 2 to 4.
  The [station banks](../station-banks.md) document keeps its audit order and refusal text.
- Maneuver groups: their certified pairs and their skip rules come first and do not change.
- Depots: the depot lateral pairs keep `3.1 + v²/2`.
  D3 changes them only if the maintainer extends D3 to depots explicitly.
  The refusal of a large depot slot stays, unless D8 lifts it.
- Lateral merges: part 3 is that rule for each class.
- Retention layer A: no change, because it is a following rule.
- Platoons and coasting: no change.
- The order of the refusals and their text in restore, scan, and stream checks stay.
  The audit refuses fewer pairs, and its text for a refused pair does not change.

## 4. Applications

### 4.1 Express on shared track

A new pair needs the route lanes and its hubs to admit Express, and berth links of 40 m at its hubs.
It needs no dedicated trunk, gate stub, or bend.

Under D1-B, each measured route has residual refusals (section 2.2).
Each pair under 4.0 m needs new geometry, or a resource that orders the two pods.
Each pair between 4.0 m and 9.31 m needs the oriented sweep, and new geometry if the sweep does not clear it.
Most residuals are lanes that cross, or come close, within a few meters of a portal or a junction node.
The geometry work can move the crossing out of the envelope reach of the node, or put the two lanes on paths that share a node, so that the junction orders them (inferred, not built).

| Route | Pairs under 4.0 m | Pairs from 4.0 m to 9.31 m |
| --- | ---: | ---: |
| Waterloo and Bank | 5 | 8 |
| Paddington and Liverpool Street | 30 | 14 |
| Waterloo and Canary Wharf | 4 | 3 |
| Tottenham Court Road and Liverpool Street | 21 | 4 |
| Victoria and Oxford Circus | 10 | 2 |
| London Bridge and Canary Wharf | 1 | 1 |
| London Bridge and Waterloo | 3 | 2 |
| King's Cross St. Pancras and Victoria | 23 | 6 |
| King's Cross St. Pancras and Liverpool Street | 1 | 4 |
| King's Cross St. Pancras and Waterloo | 5 | 6 |
| Paddington and King's Cross St. Pancras | 30 | 9 |
| Euston and Waterloo | 4 | 4 |

The routes share some pairs, for example at Baker Street on the two Paddington routes, so the sums overstate the work for a set of routes.
Under D1-A, no route has a residual.

The berth links need one of these changes:

- Hub stations at a larger pitch: `BerthPitch` 82 at scale 0.5 gives 41 m links, only at the hubs (inferred, not built).
  A per-station pitch is a generator change and not a rule change.
- A gap of 14 m along paths for a centered 6 m body and the 8 m gap of `Clearance`, with a lane minimum of 28 m (decision D4).

The roads under 40 m at Waterloo and King's Cross need new geometry, or the Express route must avoid them.
The route of Waterloo and Bank uses `940GZZLUWLO-road-in-01`, so the first stage needs a longer road or another portal at Waterloo.

Express and Compact pods share the route lanes, and Compact pods keep their current routes.
The cost to Compact pods is in section 2.5: longer junction zones near the route and fewer lateral merge pairs.
D5 removes most of it, because part 3 lets large lanes and lanes with tails form lateral merge pairs.
Behind an Express pod, a Compact pod cannot link a platoon, and it keeps 20 m.
The effect on Compact wait and journey times is not measured.

Hub banks are optional.
D has a second bank at each hub, and the study of [banks on the fixture](../studies/express-hub-fixture.md#banks-on-the-fixture) found about 10% more hub passengers with banks at Waterloo, at one seed.
This design does not require banks at the new hubs.

Under D1-B, the staging order follows the demand and the residuals:

1. Waterloo and Bank on shared track, in place of the dedicated trunks of D.
   It has the largest demand, a route of 2.2 km, and no station between the hubs.
   It is the direct comparison with D.
   Its 13 residuals are at Waterloo and Bank.
2. London Bridge and Canary Wharf, and Waterloo and Canary Wharf.
   They have the next largest AM directions after Waterloo and Bank (19.6 and 23.4 passengers a minute).
   They have 2 and 7 residuals.
3. Victoria and Oxford Circus, London Bridge and Waterloo, and King's Cross St. Pancras and Liverpool Street.
4. The routes with the most residuals under 4.0 m, at the junctions of Baker Street, King's Cross, Bank, Green Park, and Oxford Circus: Paddington and Liverpool Street, Paddington and King's Cross St. Pancras, King's Cross St. Pancras and Victoria, and Tottenham Court Road and Liverpool Street.
   King's Cross St. Pancras and Waterloo and Euston and Waterloo can come before these.

Under D1-A, the geometry does not set the order, and the order is that of section 2.10.
Charing Cross, Cannon Street, Tower Hill, and Marylebone have little demand to other hubs, and they are not in the first set.

### 4.2 Express Parking

The AM peak is tidal (54.8 against 0.6 passengers a minute from Waterloo to Bank), so each empty Express pod goes back to the origin hub.
The parking of D is 6.8 km in a straight line from Waterloo.

Proposal:

- One parking beside each origin hub of the first set: Waterloo first, then London Bridge and Paddington.
- Each parking is a comb at a pitch of 40 m with berth lanes of 40 m, sized to the fleet of its services.
  For 16 berths that is 80 m by 640 m, and a scratch probe validated it on D (section 2.8).
  For 8 berths it is 80 m by 320 m (inferred).
- The comb sits outward from the through lane of its hub, as a London depot does (`addLondonStation`, `london.go:895`).
  The site of each comb is not checked against the layout audit, so its position is inferred.

A smaller parking needs a large depot: queue lanes with slots in series.
The refusal of a large depot slot (`order_contract_bounds.go:81`) and the depot rule for small slots would both change.
With the Express body and a depot speed of 1.41 m/s, a slot is about 8 m to 9 m long, and two depot lanes are about 5.7 m apart (inferred from the depot formulas with the Express body and the Express braking of 1.15 m/s²).
Eight slots in each of two lanes would fit in about 100 m by 30 m (inferred).
This is decision D8.

### 4.3 Stations

The 12 m gap across lanes does not bind in a London station at scale 0.5 (section 2.6).
At scale 0.5, the pitch is 37.5 m, and the floor of 25 m comes from the 24 m lane minimum, which is a following rule, and the 1 m allowance of the generator (`layout_audit.go:17`, `london.go:348`).

These layouts work under the current rules, and they do not need shape clearance:

| Layout | What it needs | Pitch or length |
| --- | --- | --- |
| Rows at 25 m | `BerthPitch` 50 at scale 0.5. It validates today (section 2.7). | Median path from the diverge to a berth 183.2 m, against 195.7 m |
| Short access lanes | A generator change: each access lane at the 24 m minimum | Median path about 97 m (inferred, not built, and not validated) |

The [tight station study](../studies/station-scale.md#tight-stations) built both layouts together, with each part at 25 m, and measured a median path of 100.5 m.
Since November 16, 2026, tight stations are the default of LondonCentral and LondonFull.

This layout needs shape clearance:

| Layout | What it needs | Pitch or length |
| --- | --- | --- |
| Two platforms side by side | Part 4 for the two platform lanes, which share no node | More than 3.1 m between the lanes when both admit small classes only, and more than 4.0 m when either admits Express, for example 3.11 m and 4.01 m |

These layouts need a new topology or a new retention rule, and shape clearance is not enough:

| Layout | Why the current rules refuse it | What it needs |
| --- | --- | --- |
| Rows closer than 25 m | A chain link between rows is a lane under 24 m. | A shorter lane minimum for chain links, as the depot chain retention gives depots |
| Rows at a slow chain | A qualifying slow lane needs one incoming and one outgoing lane at each node of its window (`chainLane`, `retention.go:216`). A berth row branches at those nodes, so it fails at any speed. R also depends on the upstream stop reach and the total turn of the window, and not on the lane speed alone. | A new retention rule for branching rows, or a topology that passes qualification, before a pitch is given |
| Berths in series on one platform lane | `docs/reference.md:140` refuses a berth route that passes through another berth node. It also needs an order of departure on the platform. | A new route rule. Depot slots work this way at 6 m. |

A station layout change is a generator change and a new preset hash, and it needs its own study.
The station scale study of 0.4 against 0.5, which runs apart from this design, can set the base for it.

### 4.4 Project D and the new hubs at scale 0.5

D stays at scale 1, because row E2 freezes its geometry.
To build it at scale 0.5, as the default LondonFull is, these changes are needed:

- E2 changes, with maintainer approval, because the geometry of D changes.
- `addExpressBanks` scales its offsets with the station (section 2.9).
- The berth links of the rows that stay at the hubs are 40 m or more by a hub pitch, or 28 m or more under the 14 m gap of D4.
- The plane reach of D at 0.5 is measured after the first two changes.

With shared track, the trunks of D are not needed, and D at scale 0.5 becomes the first stage of section 4.1.
The new hubs use the default LondonFull at scale 0.5, with the hub pitch of section 4.1.

## 5. Contract changes

On 2026-11-16, the maintainer approved these changes as amendments SC1 to SC15 of the [shape clearance amendment proposal](../contracts/shape-clearance-amendment-proposal.md).
This design does not change a contract document.

| Document and section | Current text | Proposed change |
| --- | --- | --- |
| Service contract, clearance layer A (`docs/contracts/service-contract-proposals.md:50`), last amended in `faed74fd` | "Every other pair of small pods keeps 12 meters, and a pair with a large pod keeps 20 meters." | When the network sets `shapeClearance`, a following pair keeps 12 m, or 20 m with a large pod. A merge or split pair keeps the side-by-side distance of its classes, and the junction orders it. A pair that shares no node and whose swept bodies are disjoint keeps the same distance. The planes of a pod follow D1. |
| Service contract, `:46` and `:51` to `:52` | The guideway-bound assumption for depot and lateral merge pairs, and the debris partners | Name the assumption for shape pairs at line speed (D3). Debris also blocks the cells of shape partners within 12 m. |
| Express physical contract, Physical contract (`docs/contracts/express-physical-encoding-contract-proposal.md:206`) | "Retain 20 m interaction clearance when either participant is large." | Keep 20 m for following pairs. A lateral pair with a large pod follows the shape rule when the network sets the field. |
| Express physical contract, `:207` | "Prepare resources for the largest admitted class even when no Express pod starts in the fleet." | Static extents of lateral pairs use the union body of the admitted classes, not the largest class (section 3.2). |
| Express physical contract, `:217` | Check merges, crossings, plane transitions, banks, parking, and neighboring berths by actual paths and resources | Under D1-B, unchanged. Under D1-A, add: the plane of a large pod is the plane of its lane or berth when the field is set, and this permits projected body overlap across planes. |
| Express physical contract, `:219` | "There is no blanket certification for curves or authored junctions." | Unchanged. The audit proves each shape pair on its actual paths, and a pair that it cannot prove keeps the isotropic clearance. |
| Express physical contract, qualification gate "Physical geometry" (`:481`) | Centered 10 m body within the 6 m envelope, and all occupied planes | The 10 m body stays the test of the envelope. Add: a merge, split, or shape pair certifies the 6 m by 2.5 m class body only, with the union body for mixed lanes. Add the sweep cases of section 7. |
| Express physical contract, qualification gate "Independent safety oracle" (`:483`) | Following and crossing bodies, retained resource identities, both class orders, mixtures | Add the shape pairs and the body-and-ownership oracle of D6, in qualification and in restored continuation. |
| Express physical contract, amendment E2 (`:509`) | D has dedicated Express lanes, the current geometry and class rules stay | Unchanged for D. A new project with shared track needs its own row. |
| Group physics design, Geometry and ownership (`docs/design/group-physics-design.md:30`) | "An interaction with a large class requires at least 20 meters between reference positions on overlapping physical planes." | Not a contract, but approved text, so it needs an explicit amendment. When the network sets the field, a following pair keeps 20 m. A merge, split, or shape pair with a large pod keeps the side-by-side distance of its classes. The overlapping planes are those of the envelope under D1-B, or of the lane under D1-A. |
| Group physics design, body planes | A large pair is exempt only when every cross-pair of the envelopes is on different planes | Unchanged under D1-B. Changes under D1-A. |
| Incident suspension contract, section 7.2 (`docs/contracts/incident-suspension-contract-proposal.md:504`) | Lateral neighbors are depot and lateral merge partners | Add the shape partners. |
| Tick-rate contract, sections 1.2 and 1.5 (`docs/contracts/tick-rate-contract-proposal.md:50`, `:84`) | 1.2 holds. 1.5 lists the depot and lateral merge pairs. | 1.2 holds. 1.5 gets a line for shape pairs, with no time term. |

### 5.1 Effect on the qualification of D and of a new preset

The rows E1 to E8 of the [Express London qualification](../contracts/express-london-qualification-proposal.md#amendments) are contract requirements, and the field does not change their text.
The evidence for a row is for one project and one behavior.
So this section separates the requirement from the evidence.

D does not set the field.
D keeps its evidence only if a test shows behavior parity: with the field built and unset, D gives the same saved states and streams at its checkpoints as the build before the change (section 7).
Without that test, each row of D needs its evidence again.

A new shared-track preset is a new project.
Its rows need their own evidence, even where the limits and the requirements do not change.

| Row | Requirement with the field | Evidence of D, field unset | Evidence of a new shared-track preset |
| --- | --- | --- | --- |
| E1 | Unchanged. E1 names `london-full-express` only. | Holds with parity | Needs its own row, or E1 extended to name it |
| E2 | Unchanged for D. A new preset has no dedicated lanes. | Holds with parity | Needs its own row, with its geometry |
| E3 | Unchanged, and the D6 oracle added for merge pairs | Holds with parity | New: the oracle has the shape table, the envelope reach, and the D6 oracle |
| E4 | Unchanged | Holds with parity | New: the checkpoints restore with the shape table, and the D6 oracle runs in restored continuation |
| E5 | Unchanged | The byte proofs run again, because the worst-case project gets one more member | New, on the new geometry |
| E6 | Unchanged | Holds with parity | New: the topology changes |
| E7 | Unchanged | Holds with parity | New: CPU and memory of the new geometry |
| E8 | Unchanged: the limits, whole-party admission, immutable service identity, history rollover, and conservation over repeated recovery | Holds with parity | New: directed incident and recovery coverage on the new preset. Unchanged limits do not transfer the evidence. |
| S1 | Unchanged: the standee Fault, Dwell, and Restore and stream rows | Holds with parity | New: the rows run again on the new preset with standees, mixed traffic, and emergencies, and with its banks if it has any |
| A15 | Unchanged: the London soak row | Holds with parity | New: the preset is a named project of the soak row, with the same branch shape |
| A16 | Unchanged: the CI soak and the release soak | Holds with parity | New: a CI case of one simulated hour, and 5 seeds of 6 simulated hours before a release tag, with the full check and the D6 oracle at each tick |
| A17 | Unchanged: the performance and latency rows | Holds with parity | New: the preset is in the measurements that apply |
| A18 | Unchanged: the formats row | The byte proof runs again, as for E5 | New, as for E5 |
| B1 | Unchanged: the station banks formats and restore | Holds with parity | New, only if the preset has banks |

## 6. Decisions

| ID | Decision | Options | Recommendation |
| --- | --- | --- | --- |
| D1 | Plane of a large pod | A: the plane of its lane or berth, as for a small pod. B: envelope reach: the oracle keeps the envelope, and the audit takes its planes by part of a path, with the body test where they overlap. C: body reach, 6 m at each end, which drops the retained tail and the origin berth from the envelope. D: today's reach, with a smaller distance. | B. It keeps every plane that the oracle checks today, and it does not rest on refusals going away. It leaves 3 to 129 residual pairs at 20 m on each route, of which 1 to 30 are under 4.0 m (section 2.2), so each route needs some geometry work (section 4.1). A refuses no measured pair, but it permits projected body overlap across planes by assumption. C refuses fewer pairs than B, but it needs a proof that the retained tail and the origin berth do not need their planes. D clears at most a third of the refusals at 12 m. |
| D2 | Scope of classes | Large pods only. All classes. | All classes, as the maintainer asked. Small pods change only for parts 3 and 4. |
| D3 | Lateral pairs that no resource orders | A: side-by-side distance, under the guideway-bound assumption at line speed. B: the depot term `3.1 + v²/2`. C: B above a speed, A below it. | A, with the assumption stated as for lateral merges. B gives 101 m at 14 m/s, which is larger than today. D3 does not change depot pairs unless the maintainer extends it to depots. |
| D4 | Gap along paths for a large pod | Keep 20 m. 14 m: a 6 m centered body and the 8 m gap. | Keep 20 m now. Use a hub pitch for the 40 m links. 14 m changes the tails, the cells, the lane minimum (to 28 m), and the curve bounds, and it needs its own design. |
| D5 | Merge and split pairs with a large lane or a tail | Include them in part 3, with the union body. Keep them isotropic. | Include them. Most of the cost of section 2.5 is on lanes that lose their lateral merge pairs. This needs the union body of section 3.2 and the D6 oracle. |
| D6 | Oracle test for lateral pairs | A: center distance at the side-by-side value in `Check`, with an oriented body oracle in the tests. B: A, and an independent body-and-ownership oracle for merge pairs in qualification and in restored continuation. C: an oriented body test in `Check`. | B. `Check` keeps the center distance, as for lateral merges. The added oracle finds a merge pair whose bodies overlap while the junction does not order them. |
| D7 | Gate | Network field `shapeClearance`. No gate. | The field, so that D, the digests, and the goldens stay. |
| D8 | Express Parking | A: a comb of 40 m by 40 m beside each origin hub. B: keep the parking of D. C: a large depot. | A for the first stages. C later, with its own design, because it changes the depot rules. |
| D9 | First hub pairs | By demand. By demand and residuals. | Under D1-B, by demand and residuals, as in section 4.1. Under D1-A, by demand. |
| D10 | Stations | Rows at 25 m and short access lanes, under the current rules. Platforms side by side, with shape clearance. Slow chains for branching rows, or berths in series, with a new rule. | A generator study of the first two first, after the scale study. Branching slow chains and series berths need their own designs. |
| D11 | D at scale 0.5 | Rebuild D at 0.5 with shared track. Keep D at scale 1 and its trunks. | Keep D as it is. Build the shared-track Waterloo and Bank at 0.5 as a new opt-in preset, and compare the two. |
| D12 | Audit coverage when the field is set | A: the field makes the bank audit run on each pair, small-only pairs included. B: the audit keeps its current scope, and the guarantee of part 4 holds only in networks that run the all-pairs audit. | A, for the maintainer to decide. See below. |

D12 and the lane-end decision: the audit skips networks without banks, large classes, maneuver groups, or depots (`bank_geometry.go:85`).
It also skips small pairs outside the depots when the network has no banks and no maneuver groups (`:146`).
In section 11 of the [lane-end protection](lane-end-protection.md#11-build-attempt-and-decision) design, the maintainer chose option D: the gap stays in plain projects, with a record.
That choice followed the C2 build, in which the all-pairs audit on every network refused about 20 fixture builders and failed 73 tests.
Option A of D12 runs the all-pairs audit only in a network that sets `shapeClearance`.
A network without the field does not change, so option A does not reverse decision D.
A plain project that sets the field and has a close pair that shares no node is refused.
Under option B, the design records the same gap that decision D records, and part 4 makes no claim for unaudited networks.

On 2026-11-16, the maintainer chose the recommended option of each decision.
The rule is the recommended combination: D1-B, D3-A, D5 included, D6-B, and D12-A.
D2 covers all classes, D4 keeps 20 m along the path, and D7 is the network field `shapeClearance`.
D8 is a parking comb beside each origin hub, D9 orders the pairs by demand and residuals, and D11 keeps D and adds a shared-track preset.
For D10, the maintainer asked for both a generator study of the rows at 25 m and the short access lanes, and a design of berths in series.
On 2026-11-16, the maintainer approved the contract amendments of section 5, as the [shape clearance amendment proposal](../contracts/shape-clearance-amendment-proposal.md) gives them.

### 6.1 Dependencies

| Decision | Depends on | Why |
| --- | --- | --- |
| D1 | Nothing | It sets part 1. Same-plane shape clearance (part 4) does not depend on the plane rule. |
| D3 | D1 | Under D1-B, more pairs reach part 4, so D3 applies to more pairs. D3 changes depots only if it is extended there. |
| D5 | Section 3.2 and D6 | A merge pair with a large lane needs the union body and the D6 oracle. |
| D6 | D5 | The oracle covers the merge pairs that D5 admits. |
| D9 | D1 | The residuals set the order under D1-B only. |
| D12 | Part 4 | Without option A, part 4 holds only in audited networks. |
| D8, option C | D3 and a depot design | A large depot changes the depot rules. |
| D11 | D7 and D1 | D stays unchanged only behind the field. The shared-track preset needs the residual geometry of D1-B. |

One field sets parts 1 to 5 together.
So D1, D3, D5, D6, and D12 each choose a behavior of the field, and not a separate gate.
This design defines and tests two combinations:

- Recommended: D1-B, D3-A, D5 included, D6-B, and D12-A.
- D1-A, D3-A, D5 included, D6-B, and D12-A.

Another combination needs its own test plan before approval.
Each other decision (D2, D4, D7, D8, D10, D11) can be approved alone.

## 7. Test plan

Unit tests:

- Shape pairs: two straight lanes side by side for each pairing of admitted classes (small only, Express only, and both), at the side-by-side value and 0.01 m over it, and lanes at 3, 15, 45, and 90 degrees that share no node.
  At the side-by-side value the bodies touch, and the pair is not a shape pair.
  At 0.01 m over it, the pair is a shape pair.
  The table holds a pair only when the swept bodies are disjoint.
- Union body: a lane that admits Compact and Express against a lane that admits each.
  A case where only the Compact front reaches the other lane, and a case where only the Express rear or side reaches it.
  Both class orders, Group in place of Express, and mixed lanes whose initial fleet holds only small pods.
  The static extents and the table do not depend on the initial fleet.
- Tangency: a pair whose grown bodies touch is not a shape pair.
  A sweep that ends as potential or unresolved keeps the isotropic rule.
- Disk bounds: two lanes at the disk bound of each class pair need no sweep, and the result equals the sweep.
- Join turns: a lane whose end turn brings the body within the side-by-side distance of the other lane is not a shape pair.
- Envelope reach (D1-B): for each lane of a test network and positions along it, the planes of `appendVehicleSafetyLocations` for a pod on each route through the lane are in the envelope reach of the audit at that position.
- Plane rule (D1-A only): a large pod at a portal, with its body over the next lane, is on the plane of its own lane.
- Following pairs: one lane, and two lanes through a node, keep 12 m and 20 m.
- Merge pairs with a large lane: the extent against a brute-force body reference with the union body.
  Clause 5 still excludes a pair whose body range reaches the far end.
- Audit coverage (D12-A): a plain network with the field and two small lanes that share no node and come within 12 m is refused, and the same network without the field is not.
- Gate off: every table in `testdata/scenario_networks.json.gz` and D equal the current reference.
- Behavior parity of D: with the field built and unset, D gives the same saved-state and stream bytes at each of its checkpoints as the build before the change.
- Audit: each route of section 2.1 with its residual geometry fixed passes with the field set, and the order and text of the refusals of a bad pair do not change.
- Debris on a lane of a shape pair blocks the cells of the partner within 12 m.
- Two streams on parallel lanes 4.01 m apart, each lane admitting Compact and Express, one stream of Express pods and one of Compact pods, for 15 simulated minutes, with `Check` and the oriented body oracle at each tick.

The body-and-ownership oracle of D6 runs at each tick of a merge test and of a restored continuation.
For each merge or split pair, it tests the oriented grown bodies of the two pods.
When the bodies are not disjoint, it requires that one pod holds the junction of the node and the other is outside its extent.
It names the tick, the pods, the lanes, the bodies, and the owner of the junction.
Its cases are a stopped pod, a faulted pod, an emergency journey, and a pod on a join turn at the end of a lane, each in both class orders.

`Check` alone does not detect these failures:

- A merge pair whose bodies overlap at a center distance above the side-by-side value, when the junction does not order the pods.
  This includes a grant that gives the junction to both pods, and a release before the end of the extent.
- A shape pair that the table holds in error, if the bodies overlap above the side-by-side value.
- After a restore, `Check` repeats the same test, so physical restore has the same limits.

Mutations, with the table of the `verify-by-mutation` skill:

| Mutation | Test that fails |
| --- | --- |
| Shape pair without the join turns | Join turn test |
| Small body for an Express pod | Side-by-side test at 3.55 m and 4.0 m |
| Express body for a mixed lane | Union body test, Compact front case |
| Small body for a mixed lane | Union body test, Express rear and side cases |
| Shape distance for a following pair | Following pair test |
| Envelope reach without the retained tail | Envelope reach test |
| Envelope planes dropped when the field is set (D1-B) | Envelope reach test |
| Remove the exclusion of a junction grant, so that two routes own it | D6 oracle on the merge test |
| Release the junction at the start of the extent | D6 oracle on the merge test and on the restored continuation |
| Clause 5 dropped for large lanes | Merge pair far-end test |
| Audit trigger of the field removed | Audit coverage test |
| Potential sweep result taken as clear | Tangency test |
| Field ignored | Gate-off reference test |
| Shape partners left out of the debris footprint | Debris test |

The byte proofs of AGENTS.md run again, because the worst-case project gets one more member.

## 8. Study plan

The study uses the method of the [London junction study](../studies/london-junctions.md#method), AM peak, with the Express hub overlay of the [hub fixture](../studies/express-hub-fixture.md#source-and-method) for the Express arms.
`cmd/scenario` gets a flag that sets the field.
The study records means over seeds and paired changes.

1. Timed pilot: LondonFull with the field off and on, seed 1, no Express.
   Record the time and the memory, and the junction waits against the off arm.
2. Batch 1, the rule for small pods: LondonFull off and on, seeds 1 to 3.
   This tests D2 and D5 with no Express pod.
3. Batch 2, shared track: D as it is, and Waterloo and Bank on shared track at scale 0.5 with 16 Express pods and a 40 m comb beside Waterloo, seeds 1 to 3.
   The shared-track project first gets the geometry for its residuals under D1-B (section 4.1).
   Add the shared-track project with no Express pods as a control.
4. One 30-minute run of the shared-track project with `Check`, the oriented body oracle, and the D6 oracle at each tick, with the smallest gap of each kind of pair.
5. If batch 2 passes the adoption gates, a later batch adds London Bridge and Canary Wharf, and Waterloo and Canary Wharf, as one arm.

Each batch has at most 3 arms and 3 seeds, inside the cap of 6 arms and 3 seeds.
A station layout arm, and a large depot arm, come back as proposals.

## 9. Review

An adversarial review checked the first version of this proposal.
It found 1 blocker, 6 major problems, and 3 minor problems.
This revision checked each finding against the code at `623b19ec` and accepts all ten.

1. Blocker: D1-A removed the protection of the envelope without a proof that replaces it.
   The code confirms it: the envelope holds route lanes ahead, the retained tail, the unreleased origin berth, and the incident planes at berths (`envelope_safety.go:50` to `:85`), and it applies to the small pod of a mixed pair (`safety.go:269`).
   The disk comparison did not hold, because the bodies are not nested.
   Changed: section 2.1 separates the whole-lane overapproximation from the occupied planes, with the envelope reach rule.
   D1-B, envelope reach, is the recommendation, and D1-A states that it permits projected body overlap across planes.
   The summary, section 3.4, and section 4.1 give the residual geometry work.
   The 6 m body reach is now option C, because it drops the retained tail and the origin berth.
2. Major: the disk bounds did not enclose the grown bodies.
   The code grows each face (`lateralBody`, `newSweepBody`), so the corner radius grows by more than 0.5 m.
   Changed: section 3.2 gives the exact bounds, rounded up, and the union body bound of 9.31 m.
   Section 2.2 bins the residuals again at 4.0 m, 8.96 m, and 9.31 m from the probe.
   Section 3.2 defines tangency and keeps the fail-closed handling of potential and unresolved sweeps.
3. Major: center distance in `Check` cannot find overlapping merge bodies.
   Two grown Express bodies, each 8 m long, near one line overlap at center distances up to about 8 m.
   The approved D6 of lateral merges has the same property for small pairs at 3.1 m.
   Changed: D6 option B adds an independent body-and-ownership oracle in qualification and restored continuation, with the cases and the mutations that the review named.
   Section 7 states what `Check` does not detect.
4. Major: the audit does not run on each eligible plain project.
   The code confirms the early return (`bank_geometry.go:85`) and the skip of small pairs (`:146`).
   Changed: part 5 and D12.
   Option A makes the field run the all-pairs audit, and section 6 explains why this does not reverse lane-end decision D.
   The choice is for the maintainer.
5. Major: a lane that admits both kinds of class needs the union body.
   Changed: section 3.2 adds the union body, part 3 and part 4 use it for static extents, and the oracle threshold uses the classes of the two pods.
   Section 7 adds the tests and mutations that the review named.
6. Major: the qualification changes were not complete.
   The code and the contract confirm the E8 clauses and the rows S1, A15 to A18, and B1, and the 20 m rule of the group physics design.
   Changed: section 5 adds the Group amendment, the `:207` row, and the statement on the 10 m gate body.
   Section 5.1 separates the requirements from the evidence, adds the rows, and makes the evidence of D depend on a parity test.
7. Major: branching berth rows cannot be slow chains.
   `chainLane` needs one incoming and one outgoing lane at each window node (`retention.go:216`), and R depends on the upstream stop reach and the total turn (`reducedRetention`, `retention.go:228`).
   Changed: section 4.3 puts this layout with those that need a new topology or retention rule, and it gives no pitch for it.
8. Minor: the counts were not reproducible.
   Changed: the study probe `internal/sim/shape_clearance_study_test.go` is restored, and section 2.11 gives its commands.
   It reproduces the whole reach, body reach, and lane plane counts of the first version, and the counts of section 2.4.
   It does not reproduce the old tail reach column, which this revision drops.
   Sections 2.5 to 2.10 are marked as from deleted probes, and D1 does not rest on them.
   The text says "bank-audit refusals", and the summary lists berth links and short roads apart.
9. Minor: the decisions were not independent.
   Changed: section 6.1 gives the dependencies and the two combinations that this design defines.
   Section 4.4 now gives the 28 m lane minimum of D4.
10. Minor: section 4.3 gave existing station options to shape clearance.
    The code confirms the pitch floor (`layout_audit.go:17`, `london.go:348`).
    Changed: section 4.3 separates the layouts that work today, the layout that needs shape clearance, and the layouts that need a new rule.
    The path of about 97 m is marked as not validated.
