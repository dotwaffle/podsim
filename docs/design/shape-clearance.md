# Shape clearance

Status: proposed on 2026-11-15, for the maintainer's approval.
Nothing in this design is built.
Code references are for `5fe3cc0b`.

This design answers two questions of the maintainer.
The first is how Express can run on shared main-line track, so that a new hub pair needs little new geometry.
The second is why lanes must stay 12 m apart, when only pods on a common path need that gap.
It proposes one rule, shape clearance, for all classes, and three applications of it.
The rule and each application have separate decisions in section 6, so that each can be approved alone.
The [lateral merge zones](lateral-merge-zones.md) design is the first, narrower step of this rule.

The measurements change the premise of the Express question.
On LondonFull, no refusal of a shared Express route comes from two lanes on one plane that are closer than 20 m.
Each refusal comes from the plane reach of the bank audit, which treats a large pod as present on every plane at both ends of its lane.
When a large pod has the plane of its lane only, as a small pod has, the audit refuses no pair of the 12 routes and 15 hubs measured, at 20 m or at 12 m.
The remaining blocker is the 40 m length of a large lane, which the berth links of each hub at station scale 0.5 do not meet.

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
A large pod has an envelope of locations (`appendVehicleSafetyLocations`, `internal/sim/envelope_safety.go:30`).
The envelope holds each route lane from 20 m, or the longer tail, behind the reference to the front bound ahead of it (`:57`), the origin berth until release (`:73`), and each lane at a berth node (`:83`).
The pair is exempt only when every cross-pair of the two envelopes is on different planes (`envelopeLocationsSeparated`, `:89`).

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
| Bank audit | `bank_geometry.go:149` | With banks, maneuver groups, or depots, each nonincident pair on one plane needs 12 m. LondonFull has maneuver groups and depots, so it runs this audit on all pairs. |
| Lane length | `LaneMinimumLength`, and `docs/model.md:193` | A lane needs 24 m, except slow lanes, depot chains, and maneuver group lanes. |
| Berth pitch | `minimumBerthPitch`, `internal/scenarios/layout_audit.go:17` | A berth row is at least 25 m from the next, because a chain link is a lane of 24 m or more. |
| Junction zones | `junction_conflicts.go:53` | Isotropic 12 m extents, except lateral merge pairs. |
| Debris | `internal/sim/debris.go:137`, incident contract section 7.2 | Debris blocks the cells within 12 m. |

The 24 m lane minimum and the 25 m pitch follow the gap along a path, not the gap across.
Section 2.6 shows that the 12 m gap across lanes does not bind in LondonFull stations at scale 0.5.

## 2. Measurements

All counts come from scratch probes on 2026-11-15 at `5fe3cc0b`, which did not land.
The commands are in section 2.11.
Values that come from a formula or from reasoning, and not from a probe, are marked inferred.

The base is the default LondonFull preset: station scale 0.5, `lateralMerges` set, 5,740 nodes, 9,024 lanes, and network SHA-256 `441d6f3f70b16e7bcf433e315ec6735c2449e2de066975ccc1683aa95798362d`.
For each pair, the probe finds the free-flow route from the exit of one hub to the entry of the other in each direction, through no other station.
It sets the classes Legacy, Compact, and Express on the route lanes, on each lane of the two hubs except the roads off the route, and on the hubs and their berths.
It then runs a copy of `validateBankGeometry` that records every refused pair, in place of the first one.
On project D, `london-full-express`, the copy refuses no pair, as the audit does.

### 2.1 What refuses a shared route

Each column counts the refused lane and berth pairs of a pair of hubs under one plane rule and one clearance.

- Whole reach: the audit of today (section 1.2).
- Tail reach: a lane has the planes at its From node only in its first 20 m, or the longer tail of the lane before it, and the planes at its To node only in its last 6 m.
- Body reach: the planes of the lanes into the From node in the first 6 m, and of the lanes out of the To node in the last 6 m.
- Lane plane: a large path has the plane of its own lane or berth only, as a small path has.

| Pair | Route, each way | Stations passed | Whole reach, 20 m (today) | Whole reach, 12 m | Tail reach, 20 m | Body reach, 20 m | Body reach, 12 m | Body reach, 8.65 m | Lane plane, 20 m or 12 m |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Waterloo and Bank | 2,247 m and 2,178 m | 0 | 105 | 70 | 95 | 37 | 17 | 6 | 0 |
| Paddington and Liverpool Street | 7,438 m and 7,455 m | 8 | 285 | 200 | 258 | 119 | 60 | 36 | 0 |
| Waterloo and Canary Wharf | 7,275 m and 7,203 m | 4 | 78 | 66 | 67 | 23 | 14 | 7 | 0 |
| Tottenham Court Road and Liverpool Street | 3,791 m and 3,741 m | 4 | 168 | 133 | 143 | 56 | 29 | 20 | 0 |
| Victoria and Oxford Circus | 2,303 m and 2,318 m | 1 | 118 | 90 | 104 | 36 | 15 | 12 | 0 |
| London Bridge and Canary Wharf | 5,426 m and 5,409 m | 2 | 18 | 17 | 14 | 3 | 2 | 2 | 0 |
| London Bridge and Waterloo | 2,024 m and 2,090 m | 1 | 71 | 57 | 61 | 21 | 12 | 5 | 0 |
| King's Cross St. Pancras and Victoria | 4,603 m and 4,628 m | 4 | 214 | 183 | 185 | 80 | 55 | 29 | 0 |
| King's Cross St. Pancras and Liverpool Street | 3,535 m and 3,447 m | 3 | 111 | 83 | 88 | 23 | 6 | 3 | 0 |
| King's Cross St. Pancras and Waterloo | 4,041 m and 3,976 m | 6 | 159 | 128 | 130 | 44 | 18 | 8 | 0 |
| Paddington and King's Cross St. Pancras | 4,218 m and 4,209 m | 4 | 216 | 147 | 196 | 97 | 52 | 35 | 0 |
| Euston and Waterloo | 3,827 m and 3,696 m | 6 | 113 | 96 | 88 | 28 | 14 | 8 | 0 |

Each refusal in the table is a reach refusal.
No refused pair is on one plane: the two directions of a link share a plane and are 36 m apart (`londonTrackOffset` of 18 m, `internal/scenarios/london.go:20`).
A smaller clearance alone clears 6% to 33% of the refusals, for example 35 of 105 for Waterloo and Bank.
Body reach alone clears 55% to 83%, and with 12 m it clears 74% to 95%.
Only the lane plane rule clears all of them.
The route of Waterloo and Bank is the Waterloo & City link, with no station between the hubs.
No route lane is curved, and the front bound of each route is 6 m.

### 2.2 The residuals of body reach

These are the refusals of body reach at 20 m, by the smallest distance of the refused pieces.
A pair under 4.0 m fails the Express side-by-side minimum of section 3.2, so no body test clears it.
A pair at 8.65 m or more passes the disk bound of section 3.2, so a body test clears it (inferred from the disk bound, because no body sweep ran).
The pairs between need the oriented sweep.

| Pair | Under 4.0 m | 4.0 to 8.65 m | 8.65 to 12 m | 12 to 20 m | Where |
| --- | ---: | ---: | ---: | ---: | --- |
| Waterloo and Bank | 2 | 4 | 11 | 20 | Bank 21, Waterloo 14, links 2 |
| Paddington and Liverpool Street | 29 | 7 | 24 | 59 | Baker Street 47, King's Cross 39, Paddington 17, links 10, Liverpool Street 4, Moorgate 2 |
| Waterloo and Canary Wharf | 3 | 4 | 7 | 9 | Waterloo 12, London Bridge 7, Canary Wharf 2, links 2 |
| Tottenham Court Road and Liverpool Street | 14 | 6 | 9 | 27 | Bank 39, Holborn 9, Liverpool Street 3, Tottenham Court Road 2, links 3 |
| Victoria and Oxford Circus | 10 | 2 | 3 | 21 | Green Park 16, Oxford Circus 15, Victoria 3, links 2 |
| London Bridge and Canary Wharf | 1 | 1 | 0 | 1 | Canary Wharf 2, London Bridge 1 |
| London Bridge and Waterloo | 2 | 3 | 7 | 9 | Waterloo 12, London Bridge 7, links 2 |
| King's Cross St. Pancras and Victoria | 23 | 6 | 26 | 25 | King's Cross 25, Oxford Circus 20, Green Park 16, Euston 5, Warren Street 5, Victoria 3, links 6 |
| King's Cross St. Pancras and Liverpool Street | 1 | 2 | 3 | 17 | King's Cross 16, Liverpool Street 4, Moorgate 2, links 1 |
| King's Cross St. Pancras and Waterloo | 5 | 3 | 10 | 26 | King's Cross 14, Holborn 9, Waterloo 7, Charing Cross 5, Leicester Square 4, Embankment 3, links 2 |
| Paddington and King's Cross St. Pancras | 29 | 6 | 17 | 45 | Baker Street 47, King's Cross 24, Paddington 17, links 9 |
| Euston and Waterloo | 4 | 4 | 6 | 14 | Waterloo 7, Charing Cross 5, Warren Street 5, Embankment 3, Euston 2, Leicester Square 2, Tottenham Court Road 2, links 2 |

A pair counts at the station of its first lane, or of its second lane when the first is a link.
Most residuals are at station portals.
A lane that ends at a portal crosses, within a few meters of the portal, a lane that joins a plane of that portal.
On Paddington and Liverpool Street, the largest counts are at junctions on the way, Baker Street and King's Cross, and not at the hubs.
A residual under 4 m needs new geometry, or a resource that orders the two pods before a body reaches the joint.

### 2.3 Each hub alone

The probe also set the classes on all lanes of one hub, roads included, with no route.

| Hub | Whole reach, 20 m | Body reach, 20 m | Body reach, 12 m | Body reach, 8.65 m | Body reach residuals at 20 m, under 4.0 / 4.0 to 8.65 / 8.65 to 12 / 12 to 20 m | Large lanes under 40 m |
| --- | ---: | ---: | ---: | ---: | --- | --- |
| Paddington | 105 | 10 | 4 | 4 | 3 / 1 / 0 / 6 | 6 berth links of 37.5 m |
| Euston | 68 | 13 | 11 | 9 | 9 / 0 / 2 / 2 | 4 berth links |
| King's Cross St. Pancras | 288 | 25 | 13 | 9 | 4 / 5 / 4 / 12 | 8 berth links, `road-in-02` 36.1 m, `road-out-04` 38.8 m |
| Liverpool Street | 108 | 14 | 6 | 4 | 1 / 3 / 2 / 8 | 6 berth links |
| London Bridge | 49 | 5 | 2 | 2 | 2 / 0 / 0 / 3 | 6 berth links |
| Waterloo | 180 | 16 | 12 | 8 | 6 / 2 / 4 / 4 | 8 berth links, `road-in-01` 34.3 m, `road-out-02` 30.0 m |
| Victoria | 44 | 4 | 3 | 2 | 2 / 0 / 1 / 1 | 6 berth links |
| Charing Cross | 18 | 2 | 1 | 0 | 0 / 0 / 1 / 1 | 4 berth links |
| Cannon Street | 3 | 0 | 0 | 0 | 0 / 0 / 0 / 0 | 2 berth links |
| Tower Hill | 20 | 3 | 0 | 0 | 0 / 0 / 0 / 3 | 4 berth links |
| Marylebone | 3 | 0 | 0 | 0 | 0 / 0 / 0 / 0 | 2 berth links |
| Bank and Monument | 476 | 37 | 21 | 15 | 8 / 7 / 6 / 16 | 2 berth links |
| Canary Wharf | 9 | 3 | 2 | 2 | 0 / 2 / 0 / 1 | 6 berth links |
| Tottenham Court Road | 44 | 4 | 4 | 3 | 2 / 1 / 1 / 0 | 6 berth links |
| Oxford Circus | 203 | 23 | 13 | 12 | 7 / 5 / 1 / 10 | 6 berth links |

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

The probe compared the junction tables and the lateral merge pairs of the base with those of a network with one shared route.

| Pair | Lanes with a longer zone | Zone length added | On Express lanes | On other lanes | Lateral merge pairs |
| --- | ---: | ---: | ---: | ---: | --- |
| Waterloo and Bank | 99 | 2,355 m | 711 m | 1,644 m | 11,483 to 10,975 |
| Paddington and Liverpool Street | 251 | 6,598 m | 1,736 m | 4,862 m | 11,483 to 10,597 |
| London Bridge and Canary Wharf | 95 | 1,953 m | 1,022 m | 931 m | 11,483 to 11,353 |

Most of the added zone length is on lanes that Express does not use, because each lane at a node of a large lane gets a tail and loses its lateral merge pairs.
The effect on wait and journey times is not measured.

### 2.6 Gaps between lanes

These counts cover all of LondonFull, for nonincident pairs that share no node.

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

A station lane here includes the junction movement lanes and the roads, which cross each other on their own planes.
These pairs need no clearance under the lane plane rule.
Under a reach rule, a pair of them refuses when a large lane makes its reaches overlap.

### 2.7 Stations

| Layout | Pitch | Validates | Berths measured | Median diverge to berth |
| --- | ---: | --- | ---: | ---: |
| LondonFull, scale 0.5 | 37.5 m | Yes | 702 | 195.7 m |
| The same with `BerthPitch` 50 | 25.0 m | Yes | 702 | 183.2 m |

The pitch of 25 m is the floor of `minimumBerthPitch`, and it validates today.
It saves 12.5 m on the median path, because 216 of 309 stations have two berths.
Most of the path is the fixed part (inferred from the shape constants of `internal/scenarios/london_heading.go:205`): about 63 m from the diverge to the entry, 45 m from the entry to the first row, and 50 m on the berth lane.
With each of these three lanes at the 24 m minimum, the median would be about 97 m at pitch 25 m (inferred, not built).
The throat stays 80 m from the junction, which keeps the access lanes off the junction guideways ([station scale study](../studies/station-scale.md#shape)).

### 2.8 Express Parking

A probe replaced the parking of D with a comb of 16 berths, built as `addExpressParking` builds it, and ran project validation.

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

The probe built D on LondonFull at scale 0.5 and ran project validation.
It refuses with `lane "940GZZLUBNK-road-in-05" must be at least 24 meters long`.
`addExpressBanks` assumes scale 1: it takes the station axis from an entry-to-exit distance of 200 m, and it places the second bank at fixed distances of up to 360 m (`london_full_express.go:150` to `:155`, `:169`, and `:180`).
Six Express lanes are under 40 m: the 37.5 m berth links of the rows that stay at Waterloo and Bank.
Validation stops at its first refusal, so the plane reach of D at scale 0.5 is not measured.
At scale 1, the same probe validates D.

### 2.10 Demand of the hub pairs

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

The probes were untracked test files, `internal/sim/zz_xshared_probe_test.go` and `internal/scenarios/zz_xshared_probe_test.go`, which are deleted.
The projects came from `cmd/scenario -preset london-full` and `-preset london-full-express`.
The sim probe read the network of a project file, rebuilt the audit indexes as `prepareNetworkTails` does, and ran the copy of the audit loop under each plane rule and clearance.
The scenarios probe called `generateLondonFull`, `londonFullExpress`, `LondonFullWith`, and `project.Validate`.
The demand ranks came from the `flows` of the project profile.
Each probe took less than 15 s.

```sh
go run ./cmd/scenario -preset london-full -output lf.json
go run ./cmd/scenario -preset london-full-express -output lfx.json
XS_PROJECT=lfx.json go test ./internal/sim -run 'TestXSharedBase$' -v
XS_PROJECT=lf.json go test ./internal/sim -run 'TestXShared(Pairs|Hubs|CrossGroup|Gaps)$' -v
XS_PROJECT=lf.json XS_ZONES=1 XS_ONLY=WLO-BNK go test ./internal/sim -run 'TestXSharedPairs$' -v
XS_PROJECT=lf.json XS_SHORT=1 XS_ONLY=WLO-BNK go test ./internal/sim -run 'TestXSharedPairs$' -v
XS_SCALE=1 XS_PARK=1 XS_STATIONS=1 go test ./internal/scenarios -run 'TestXShared' -v
```

`XS_SCALE` builds D at scale 1 and at scale 0.5.
`XS_SHORT` lists the route lanes under 40 m, for one pair at a time.

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

Each side of each body grows by half of a margin of 1 m, as in lateral merges.

| Pair | Side by side, center distance | Disk bound |
| --- | ---: | ---: |
| Small and small | 3.1 m | 8.69 m |
| Express and small | 3.55 m | 8.65 m |
| Express and Express | 4.0 m | 8.62 m |

The side-by-side value is the two half widths and the margin.
The disk bound is the radius of each body about its reference, 3.85 m for small and 3.81 m for Express, plus the margin.
The disk of the Express body is inside the disk of the small body, so the Express body reaches no further from its reference than the small body does.
The 6 m envelope radius of the contract is a common bound for Group and Express, and the group physics design allows a smaller per-class envelope after qualification.
The Physical geometry gate of the Express physical contract (`:481`) tests a centered 10 m body within the 6 m envelope.
This design uses the 6 m body of the class profile.
A class body longer than 6 m needs a new check of item 5 of section 3.4 and of the disk bound.

### 3.3 The rule

Shape clearance has four parts.

1. **Planes.**
   A pod of each class is on the plane of the lane or the berth of its reference.
   The large envelope of section 1.1 no longer sets the planes of a large pod.
   The bank audit drops the plane reach.
2. **Following pairs.**
   They keep 12 m, or 20 m with a large pod.
   The lane minimum, the cells, the tails, the origin retention, and the junction extents of through pairs do not change.
3. **Merge and split pairs.**
   The lateral merge rule applies to each class, with the body of each pod from section 3.2.
   Clause 3 of that design drops: a large lane and a lane with a tail can form a pair.
   Clause 5 stays: a pair whose body range reaches the far end of a lane keeps the isotropic extent.
4. **Pairs that share no node.**
   The audit sweeps the grown body of each pod over its lane and over each join turn at both ends of the lane, as clause 5 encloses a join turn.
   If the two swept regions are disjoint, the pair is a shape pair, and its pods keep the side-by-side distance of their classes.
   If they meet, the pair keeps 12 m or 20 m, and the audit refuses it as today.

Each shape pair goes into a table, as the depot lateral pairs and the lateral merge pairs do (`depotLateral`, `depot_lateral.go:47`).
The oracle reads the table: a shape pair needs the side-by-side distance, and every other pair keeps its rule.
The tests also check each shape pair against an oriented body oracle, as the lateral merge tests do.

### 3.4 Safety argument

1. A following pair keeps the path gap of section 1.2 of the tick-rate contract.
   Parts 2 and 3 do not change the cells, the tails, or the release.
2. A merge or split pair keeps the argument of section 3 of the lateral merge design: the junction of the node orders the pods, and a pod outside its extent clears each body position on the other lane.
   The Express body changes only the body range.
3. A shape pair is safe at every pair of positions.
   The audit proves that the grown bodies are disjoint for each position on one lane and each position on the other, so the result does not depend on time, speed, or which pod is where.
   No resource is needed to order the pods.
   This holds at each tick rate, as section 1.2 of the tick-rate contract requires.
4. The argument assumes a guideway-bound pod: its reference stays within 0.25 m of its lane, its heading follows the lane, and a fault stops it on its lane.
   Lateral merges assume this at line speed for pairs that a junction orders.
   This rule extends it to pairs that no resource orders, at line speed.
   The depot rule `3.1 + v²/2` keeps a stop term for the same kind of pair, and that term is 101 m at 14 m/s.
   Decision D3 chooses between the two.
5. The plane rule of part 1 gives a large pod the same plane model as a small pod.
   A small pod is on the plane of its lane while its body reaches 3.7 m ahead of its reference into the next lane.
   The Express body reaches at most 3.5 m along the lane and stays inside the disk of the small body.
   So part 1 accepts for Express the overhang that the model accepts for small pods today.
   This is an assumption of the plane model and not a derived result, as the guideway assumption is.
   Decision D1 chooses it or a body reach that refuses the residuals of section 2.2.
6. The audit has no braking term, because a pod stops inside the cells that it owns.
   The class rates of Express do not change a lateral result.

### 3.5 Places that change

| Place | Change |
| --- | --- |
| `pair`, `safety.go:261` | Large pods use their lane location, not the envelope. A shape pair from the table needs the side-by-side distance. |
| `largeSafety`, `envelope_safety.go:13` | Not used for the plane test when the field is set. It can stay for the tests. |
| `validateBankGeometry`, `bank_geometry.go:82` | No plane reach. A large pair on different planes is exempt as a small pair is. Shape pairs pass with the side-by-side distance. |
| `newPlaneReach`, `plane_reach.go:45` | Not used when the field is set. |
| `newMergeLateral`, `lateral_merges.go:35` | Eligibility takes large lanes and lanes with tails, with the body of section 3.2. |
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

### 3.7 Interactions

- Banks: a bank is a station of berths and lanes, so its lanes follow parts 2 to 4.
  The [station banks](../station-banks.md) document keeps its audit order and refusal text.
- Maneuver groups: their certified pairs and their skip rules come first and do not change.
- Depots: the depot lateral pairs keep `3.1 + v²/2` unless D3 changes them.
  The refusal of a large depot slot stays, unless D8 lifts it.
- Lateral merges: part 3 is that rule for each class.
- Retention layer A: no change, because it is a following rule.
- Platoons and coasting: no change.
- The order of the refusals and their text in restore, scan, and stream checks stay.
  The audit refuses fewer pairs, and its text for a refused pair does not change.

## 4. Applications

### 4.1 Express on shared track

With D1 option A, each measured route passes the audit with the current 20 m along paths.
A new pair then needs the route lanes and its hubs to admit Express, and berth links of 40 m at its hubs.
It needs no dedicated trunk, gate stub, or bend.

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

The first set and the staging order follow the demand, because with D1 option A the geometry does not set the order:

1. Waterloo and Bank on shared track, in place of the dedicated trunks of D.
   It has the largest demand, a route of 2.2 km, and no station between the hubs.
   It is the direct comparison with D.
2. Waterloo and Canary Wharf, and London Bridge and Canary Wharf.
   They have the next largest AM directions after Waterloo and Bank (23.4 and 19.6 passengers a minute), and they reuse Waterloo or share Canary Wharf.
   London Bridge and Canary Wharf has the fewest residuals under every reach rule.
3. Paddington and Liverpool Street, and Victoria and Oxford Circus.
   Paddington and Liverpool Street passes 8 stations, and it adds the most junction zone length (6,598 m).
4. Tottenham Court Road and Liverpool Street, then the pairs of two termini in the order of section 2.10.

Charing Cross, Cannon Street, Tower Hill, and Marylebone have little demand to other hubs, and they are not in the first set.
If D1 takes option B, the residuals of section 2.2 set the order, and London Bridge and Canary Wharf comes first.

### 4.2 Express Parking

The AM peak is tidal (54.8 against 0.6 passengers a minute from Waterloo to Bank), so each empty Express pod goes back to the origin hub.
The parking of D is 6.8 km in a straight line from Waterloo.

Proposal:

- One parking beside each origin hub of the first set: Waterloo first, then London Bridge and Paddington.
- Each parking is a comb at a pitch of 40 m with berth lanes of 40 m, sized to the fleet of its services.
  For 16 berths that is 80 m by 640 m, and the probe validated it on D (section 2.8).
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
The pitch is 37.5 m, and the floor of 25 m comes from the 24 m lane minimum, which is a following rule.
A pitch of 25 m validates today and shortens the median path from the diverge to a berth from 195.7 m to 183.2 m (section 2.7).

Shape clearance makes these layouts possible.
Each needs more than the rule.

| Layout | What it needs | Pitch or length |
| --- | --- | --- |
| Rows at 25 m, short access lanes | Generator only, with lanes at the 24 m minimum | Median path about 97 m (inferred) |
| Rows at a slow chain | Chain links that are qualifying slow lanes (`docs/model.md:195`), at most 3.74 m/s, with no large lane near them. The rows are parallel lanes, and part 4 lets them be closer than 12 m. | 2R is 16.9 m at 3.74 m/s, so a pitch of about 18 m (inferred) |
| Berths in series on one platform lane | Berth routes that pass through other berth nodes, which `docs/reference.md:140` refuses today, and an order of departure on the platform. Depot slots work this way at 6 m. | About 6 m for small pods at 1.41 m/s, from the depot chain rule |
| Two platforms side by side | Part 4 for the two platform lanes | 3.1 m centers for small pods, 4.0 m for Express |

Rows closer than 12 m need chain links under 12 m, so they need the depot chain retention and not only the rule across lanes.
A station layout change is a generator change and a new preset hash, and it needs its own study.
The station scale study of 0.4 against 0.5, which runs apart from this design, can set the base for it.

### 4.4 Project D and the new hubs at scale 0.5

D stays at scale 1, because row E2 freezes its geometry.
To build it at scale 0.5, as the default LondonFull is, these changes are needed:

- E2 changes, with maintainer approval, because the geometry of D changes.
- `addExpressBanks` scales its offsets with the station (section 2.9).
- The berth links of the rows that stay at the hubs are 40 m or more, by a hub pitch or by D4.
- The plane reach of D at 0.5 is measured after the first two changes.

With shared track, the trunks of D are not needed, and D at scale 0.5 becomes the first stage of section 4.1.
The new hubs use the default LondonFull at scale 0.5, with the hub pitch of section 4.1.

## 5. Contract changes

Each change needs maintainer approval.
This design does not change a contract document.

| Document and section | Current text | Proposed change |
| --- | --- | --- |
| Service contract, clearance layer A (`docs/contracts/service-contract-proposals.md:50`), last amended in `faed74fd` | "Every other pair of small pods keeps 12 meters, and a pair with a large pod keeps 20 meters." | When the network sets `shapeClearance`, a following pair keeps 12 m, or 20 m with a large pod. A merge or split pair keeps the side-by-side distance of its classes, and the junction orders it. A pair that shares no node and whose swept bodies are disjoint keeps the same distance. Each pod is on the plane of its lane or berth. |
| Service contract, `:46` and `:51` to `:52` | The guideway-bound assumption for depot and lateral merge pairs, and the debris partners | Name the assumption for shape pairs at line speed (D3). Debris also blocks the cells of shape partners within 12 m. |
| Express physical contract, Physical contract (`docs/contracts/express-physical-encoding-contract-proposal.md:206`) | "Retain 20 m interaction clearance when either participant is large." | Keep 20 m for following pairs. A lateral pair with a large pod follows the shape rule when the network sets the field. |
| Express physical contract, `:217` | Check merges, crossings, plane transitions, banks, parking, and neighboring berths by actual paths and resources | Add: the plane of a large pod is the plane of its lane or berth when the field is set (D1). |
| Express physical contract, `:219` | "There is no blanket certification for curves or authored junctions." | Unchanged. The audit proves each shape pair on its actual paths, and a pair that it cannot prove keeps the isotropic clearance. |
| Express physical contract, qualification gates "Physical geometry" and "Independent safety oracle" (`:481`, `:483`) | 6 m envelope, all occupied planes | Add the shape pairs and the body oracle. |
| Express physical contract, amendment E2 (`:509`) | D has dedicated Express lanes, the current geometry and class rules stay | Unchanged for D. A new project with shared track needs its own row. |
| Incident suspension contract, section 7.2 (`docs/contracts/incident-suspension-contract-proposal.md:504`) | Lateral neighbors are depot and lateral merge partners | Add the shape partners. |
| Tick-rate contract, sections 1.2 and 1.5 (`docs/contracts/tick-rate-contract-proposal.md:50`, `:84`) | 1.2 holds. 1.5 lists the depot and lateral merge pairs. | 1.2 holds. 1.5 gets a line for shape pairs, with no time term. |
| Group physics design, body planes | A large pair is exempt only when every cross-pair of the envelopes is on different planes | Not a contract, but approved text. It changes with D1. |

### 5.1 Effect on the qualification of D

The field is off in `london-full-express`, so rows E1 to E8 keep their evidence.

| Row | With the field off | If D sets the field or is rebuilt on shared track |
| --- | --- | --- |
| E1 | Holds | Holds for ordinary LondonFull only if the field stays off there, or a new row covers it |
| E2 | Holds | Changes: no dedicated lanes, and a new geometry |
| E3 | Holds | Again: the oracle has the shape table and the lane planes |
| E4 | Holds | Again: the checkpoints restore with the shape table |
| E5 | Holds, but the byte proofs run again for the new field | Again on the new geometry |
| E6 | Holds | Again: the topology changes |
| E7 | Holds | Again: CPU and memory of the new geometry |
| E8 | Holds | Holds: the limits do not change |

## 6. Decisions

| ID | Decision | Options | Recommendation |
| --- | --- | --- | --- |
| D1 | Plane of a large pod | A: the plane of its lane or berth, as for a small pod. B: body reach, 6 m at each end. C: today's reach, with a smaller distance. | A. It refuses no measured pair. B leaves 3 to 119 residuals on each route (section 2.1), many under 4 m, which need new geometry. C clears at most a third at 12 m, and at most half at the side-by-side 4.0 m. |
| D2 | Scope of classes | Large pods only. All classes. | All classes, as the maintainer asked. Small pods change only for parts 3 and 4. |
| D3 | Lateral pairs that no resource orders | A: side-by-side distance, under the guideway-bound assumption at line speed. B: the depot term `3.1 + v²/2`. C: B above a speed, A below it. | A, with the assumption stated as for lateral merges. B gives 101 m at 14 m/s, which is larger than today. |
| D4 | Gap along paths for a large pod | Keep 20 m. 14 m: a 6 m centered body and the 8 m gap. | Keep 20 m now. Use a hub pitch for the 40 m links. 14 m changes the tails, the cells, the lane minimum, and the curve bounds, and it needs its own design. |
| D5 | Merge and split pairs with a large lane or a tail | Include them in part 3. Keep them isotropic. | Include them. Most of the cost of section 2.5 is on lanes that lose their lateral merge pairs. |
| D6 | Oracle test for lateral pairs | Center distance at the side-by-side value, with an oriented body oracle in the tests. An oriented body test in `Check`. | Center distance in `Check`, as for lateral merges. |
| D7 | Gate | Network field `shapeClearance`. No gate. | The field, so that D, the digests, and the goldens stay. |
| D8 | Express Parking | A: a comb of 40 m by 40 m beside each origin hub. B: keep the parking of D. C: a large depot. | A for the first stages. C later, with its own design, because it changes the depot rules. |
| D9 | First hub pairs | Section 4.1, by demand. By fewest residuals. | By demand, if D1 takes A. |
| D10 | Stations | Generator: pitch 25 m and short access lanes. Slow chains. Berths in series. | A generator study first, after the scale study. Series berths need their own design. |
| D11 | D at scale 0.5 | Rebuild D at 0.5 with shared track. Keep D at scale 1 and its trunks. | Keep D as it is. Build the shared-track Waterloo and Bank at 0.5 as a new opt-in preset, and compare the two. |

## 7. Test plan

Unit tests:

- Shape pairs: two straight lanes side by side at 3.09 m, 3.1 m, and 4.0 m for each class pair, and lanes at 3, 15, 45, and 90 degrees that share no node.
  The table holds a pair only when the swept bodies are disjoint.
- Join turns: a lane whose end turn brings the body within the side-by-side distance of the other lane is not a shape pair.
- Plane rule: a large pod at a portal, with its body over the next lane, is on the plane of its own lane.
- Following pairs: one lane, and two lanes through a node, keep 12 m and 20 m.
- Merge pairs with a large lane: the extent against a brute-force body reference with the Express body.
  Clause 5 still excludes a pair whose body range reaches the far end.
- Gate off: every table in `testdata/scenario_networks.json.gz` and D equal the current reference.
- Audit: each route of section 2.1 passes with the field set, and the order and text of the refusals of a bad pair do not change.
- Debris on a lane of a shape pair blocks the cells of the partner within 12 m.
- Two streams on parallel lanes 4.0 m apart, one Express and one Compact, for 15 simulated minutes, with `Check` and the oriented body oracle at each tick.

Mutations, with the table of the `verify-by-mutation` skill:

| Mutation | Test that fails |
| --- | --- |
| Shape pair without the join turns | Join turn test |
| Small body for an Express pod | Side-by-side test at 3.55 m and 4.0 m |
| Shape distance for a following pair | Following pair test |
| Envelope planes kept when the field is set | Plane rule test |
| Clause 5 dropped for large lanes | Merge pair far-end test |
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
   Add the shared-track project with no Express pods as a control.
4. One 30-minute run of the shared-track project with `Check` and the oriented body oracle at each tick, with the smallest gap of each kind of pair.
5. If batch 2 passes the adoption gates, a later batch adds Waterloo and Canary Wharf, and London Bridge and Canary Wharf, as one arm.

Each batch has at most 3 arms and 3 seeds, inside the cap of 6 arms and 3 seeds.
A station layout arm, and a large depot arm, come back as proposals.
