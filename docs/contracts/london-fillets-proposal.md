# London road fillets proposal

Status: APPROVED by the maintainer on 2026-11-20, with decisions FL-D1 to FL-D9 (section 9).
The maintainer approved stage 1 of decision FL-D4: slices 0 to 3 and the evidence of LondonFull.
Stage 2 waits until the maintainer reads the evidence of LondonFull.
Slice 0 ran, and slices 1 to 3 landed.
Section 8.1 records the evidence of stage 1.
On 2026-11-21, the maintainer approved two corrections of section 2.1: the count of roads with rays behind an end, and the case of a vector with no direction (section 8.1).
An adversarial review on 2026-11-20 found 2 major and 6 minor text defects, and this text includes the fixes.
Date: 2026-11-20.
Line references are for `ddb2233a`.
"Study" numbers are from a scratch probe of 2026-11-20 at `151416ae`, and the probe files are deleted.
"Rerun" numbers are from an independent scratch script on the project files that `cmd/scenario` writes at `468d1dd6`, and that script is deleted.
The study converted more lane kinds than this proposal does, so each study number says which lanes it converted.

## Summary

On 2026-11-20, the maintainer chose to fillet only two kinds of lanes on LondonFull and on `london-full-shared-express`:

- The station roads, `<station>-road-in-NN` and `<station>-road-out-NN`.
- The roads of the three Parking facilities, which the study calls the depot fans (section 1.2).

A fillet is a quadratic curve that leaves a road tangent to the lane before it and joins the lane after it at the same heading.
The tangency is that of the analytic curves (section 2.1).
The speed limit of a fillet comes from its smallest radius, by the ramp formula of the [London junction](../design/london-junctions.md) design.
Project D, `london-full-express`, keeps its chords, because its approved contract freezes its geometry (`london-junctions.md:34`).

The rule removes almost all kinks at the station roads.
It has three costs that the study did not measure:

- About half of the converted roads have a smallest radius below 12.5 m, which is under the 5 m/s floor of the ramp design (section 2.3).
- A virtual platoon needs one speed limit on the whole route ahead and on each lane of the destination station (`platoon.go:622`).
  After the rule, 282 of the 309 passenger stations of LondonFull have a road slower than 14 m/s, so the code predicts that no platoon forms toward them (section 3.1).
- On the shared-track preset, the rows SX1 to SX13 apply to the pinned network hash of the preset, so a change to the preset reopens its qualification (section 6).

## Conventions

- Decision IDs have the prefix FL-D, and amendment IDs have the prefix FL-A.
- `<date>` is the date of the approval.
- "The preset" is `london-full-shared-express`.
- A count "in, out" gives the road-in lanes, then the road-out lanes.

## 1. Goal and scope

### 1.1 Goal

Each London station joins its links with straight roads today.
A road-in goes from the arrival portal on the link to the diverge node of the station (`london.go:938` to `:942`).
A road-out goes from the merge node to the departure portal (`:954` to `:958`).
Most roads have a kink at each end, because the road leaves the link at the angle of its chord.
The study found 1,346 "portal road to link" kinks and 1,282 "station road/access" kinks on the preset.
This proposal replaces each such road with a tangent curve, so that a pod drawn on the road turns smoothly.

### 1.2 Lanes in scope

| Kind | IDs | Converted on LondonFull (rerun) | Converted on the preset (rerun) |
| --- | --- | ---: | ---: |
| Station road-in | `<station>-road-in-NN` | 639 of 716 | 642 of 716 |
| Station road-out | `<station>-road-out-NN` | 646 of 716 | 645 of 716 |
| Parking facility roads | `parking-{west,north,east}-road-in-NN` and `-road-out-NN` | 22 of 24 | 22 of 24 |

The counts of this table use the node rule of the study (section 2.1).
The study calls the Parking facility roads "depot fans" and counts 22 of them, with 6 below a radius of 12.5 m.
The rerun finds that these 22 lanes are the road-in and road-out lanes of the three Parking facilities.
The depot lanes `parking-*-fan-N` and `parking-*-throat-N` (`depot_station.go:181`, `:198`) already have a control point, and the study left them as they are.
Decision FL-D1 asks whether the scope also takes the fan and throat lanes.

The Compact stations of LondonFull have roads of the same form, so their roads are in scope.
The lane and node counts do not change: a fillet changes the control point and the speed limit of a lane that exists.

### 1.3 Non-goals

| Item | Reason from the study |
| --- | --- |
| Through lanes | At each tight station the fillet is a hairpin with a radius of about 5 m, 89 m longer than the chord. It breaks the 12 m rule against the berth rows: validation refused about one through lane in each tight station. They need a station redesign. On D, no through lane converts. |
| U-turn moves (about 715) | The two tangents are parallel, so a single quadratic cannot join them. They need two curves with a new node, or the ramp design. |
| Other junction moves | About 80 moves grow by more than 1.5 times. Two moves (`WLO-move-03-04` and `EUS-move-01-03`) bulge by about 1.9 km and cause each new layout conflict of the study. A move fillet needs a cap on its legs first. |
| Berth rows, and the access-to-berth-chain joins | 1,404 berth row kinks and 602 access-to-berth-chain kinks. One end is already tangent, or the node has several tangents, so they need split nodes or a curved berth chain. |
| Access lanes | Several tangents meet at the diverge and merge nodes, so the rule cannot fillet them. Their joins become smooth when the roads are filleted. |
| Compact bays, and the 62 short-chord curved moves | Reversals above 170 degrees, or lanes that are already curved and not tangent. |
| Express lanes: the gate and bank lanes of D, the `xp-` lanes, and the Express parking roads | D keeps its chords, because its approved contract fixes its geometry (`express-london-qualification-proposal.md:29`, `london-junctions.md:34`). The Express parking roads of the preset are Express lanes of the S4 parking. |
| LondonCentral | The maintainer named LondonFull and the preset only. LondonCentral uses the same `addLondonStation` (`london.go:907`), so the rule is an option that LondonCentral does not set (section 4). |
| A longer station throat | Realistic speeds at tight stations need a longer throat, which is a station shape change. D's wide stations have a median road radius of 22 m. |
| The `-junctions ramps` study option | It is not a preset. The option has no effect with ramps, so the ramps study inputs keep their pins (section 4). |

## 2. The fillet rule

### 2.1 Geometry

Let a road go from node `a` to node `b`.
The rule takes two headings by the role of the neighbor lanes, and not by the count of lanes at a node:

| Road | Heading `u` at `a` | Heading `v` at `b` |
| --- | --- | --- |
| Road-in | The end heading of the `london-link-` lane that ends at the arrival portal | The start heading of `<station>-access-in` |
| Road-out | The end heading of `<station>-access-out` | The start heading of the `london-link-` lane that starts at the departure portal |
| Parking road | As for the station roads of the facility | As for the station roads of the facility |

`u` and `v` are the normalized analytic tangents at the ends of the neighbor lanes.
A straight lane has the direction of its chord at both ends.
A lane with a control point `C` has the start tangent `C - from` and the end tangent `to - C`.
If `C` is at that end of the lane, the tangent is the direction of the chord.

The control point `C` is where the ray from `a` along `u` meets the ray into `b` along `v`: `C = a + s u = b - t v`, with `s > 0` and `t > 0`.
The analytic road then leaves `a` at heading `u` and reaches `b` at heading `v`, so both joins of the analytic curves have no kink.
The simulator samples each curved lane as 64 straight segments (`lanePointsBetween`, `network.go:202`, through `Network.Polyline`, `:179`).
The lateral pairs take their headings from the first and the last segment (`depot_lateral.go:281`).
So each join of the sampled paths keeps a small residual angle.
Slice 0 measures the residual join angles on `Network.Polyline`.
A quadratic turns in one direction only, so its total turn is the angle from `u` to `v`.
This is at most the sum of the two kinks that it replaces.

The rule leaves a road as it is in these cases:

- A neighbor lane is missing, or `u`, `v`, or the chord from `a` to `b` has no direction: its length is zero or not finite.
- The headings are parallel, so the rays do not meet (study tolerance `|u × v| < 1e-9`).
- The rays meet behind `a` or behind `b` (`s` or `t` at most 1e-6).
  The code finds 134 such roads on LondonFull, 2 of them Parking roads, and the rerun finds 128 on the preset.
- One end is already tangent, within 1 degree (15 roads on LondonFull, 14 on the preset).
- The road already has a control point.
  The pass runs on LondonFull before `apply`, so `940GZZLUWLO-road-in-05` has no control point when the pass reads it.
  On the preset, `apply` gives that road its curve (`london_full_shared.go:302` to `:319`, section 3.7).

The study took the headings by the count of tangents at a node.
On the preset, the Express parking roads end and start at the portals of link 100 at Waterloo, so that rule blocked `940GZZLUWLO-road-in-01` and `940GZZLUWLO-road-out-01`.
The role rule above can fillet them, but no run has measured that case (see decision FL-D5).

### 2.2 Speed

`londonRampSpeed(from, control, to)` (`london_ramps.go:366`) gives `min(14, sqrt(2 r))` m/s, where `r` is the smallest radius of the curve in closed form.
The lateral acceleration is `londonRampAcceleration`, 2.0 m/s² (`:23`).
The function has no floor.
The ramp design applies its floor of 5 m/s (`londonRampMinimumSpeed`, `:30`) by dropping the ramp (`:239`, `:552`, and `london-junctions.md:46` to `:48`).
A road cannot drop, because it is the only way from its arm into the station.

The study raised each speed to the floor: `min(14, max(5, sqrt(2 r)))`.
This proposal calls that the clamp.
At 5 m/s, the clamp gives a lateral acceleration of `25 / r` m/s²: 2.0 at 12.5 m, 5.0 at 5 m, and 25 at 1 m.
The simulator has no lateral acceleration rule: "Each lane has an explicit speed limit, and bends add no other speed limits" (`docs/model.md:14`).

### 2.3 Tight stations, with a radius below 12.5 m

The tight station shape sets the road radius.
The shorter leg of a road fillet at a tight station is about 17 to 18 m at p10 to p25 (study), so many radii come out near 10 m.

| Item | LondonFull (rerun) | The preset (rerun, study) | D, for reference only (rerun, study) |
| --- | ---: | ---: | ---: |
| Converted station roads, in, out | 639, 646 | 642, 645 (study: same) | 679, 667 (study: 1,346 in all) |
| Median radius, in, out | 13.2 m, 19.5 m | 16.6 m, 22.0 m (study: 17 m, 22 m) | 21.8 m, 21.9 m (study: 22 m) |
| Roads with a radius below 12.5 m, in, out | 319, 306 | 316, 302 (study: same) | 96, 112 (study: 208) |
| Roads with a radius below 5 m | 61 | 56 | not measured |
| Smallest radius | 0.04 m, `940GZZLUWLO-road-out-02` | 0.53 m, `940GZZLUCTN-road-in-03` | not measured |
| Median speed limit, in, out | 5.14 m/s, 6.25 m/s | 5.76 m/s, 6.63 m/s (study: 5.8, 6.6) | 6.60 m/s, 6.61 m/s |
| Station roads that keep 14 m/s | 0 | 2 | 58 |
| Parking roads converted, and below 12.5 m | 22, 6 | 22, 6 (study: same) | 22, 6 |

The clamp does not change a median above 5 m/s.
The study said that LondonFull is within 1 percent of the preset.
For the roads, the rerun does not agree: the road-in median radius is 13.2 m on LondonFull and 16.6 m on the preset.
The study gives no LondonFull row for the roads, so this proposal uses the rerun for LondonFull.

Decision FL-D2 chooses what a road with a radius below 12.5 m gets:

- The clamp: a curve at 5 m/s, as the study measured.
- No fillet: the road keeps its chord at 14 m/s, as a ramp below the floor drops.
  About 625 of 1,285 station roads on LondonFull keep their kinks.
- No floor: `sqrt(2 r)`, down to 0.29 m/s on LondonFull.

On the preset, the three refusals of the study (section 2.4) are roads with radii of 3.7 m, 5.2 m, and 3.5 m (rerun), so each is below 12.5 m.

### 2.4 What the study measured

The study did not run the checks on the scope of this proposal.
Each check result below converted the junction moves too, and most also converted the through lanes.

| Measurement | Lanes converted | Result |
| --- | --- | --- |
| Kinks touching road-in lanes, on the preset | Road-in only | 1,308 before, 90 after |
| Kinks touching road-out lanes, on the preset | Road-out only | 1,322 before, 88 after |
| Kinks touching Parking roads, on the preset | Parking roads only | 62 before, 20 after |
| Validation, LondonFull | All but the through lanes | No refusal after 1 move went back to straight. 2 layout conflicts, both from the bulge of `WLO-move-03-04`. |
| Validation, the preset | All but the through lanes | 3 refusals "lane ... has an unsupported large-vehicle path shape" (`prepared.go:187`), for `940GZZLUBNK-road-in-06`, `940GZZLUBNK-road-out-05`, and `940GZZLUWLO-road-out-02` |
| Shape pairs and tail warnings, the preset | All but the through lanes, and the 3 roads above straight | 33 shape pairs, against the pinned 47. The study traced the change to the road fillets at the Express hubs. 1 tail warning, as pinned. |
| Free-flow route time, 100 passenger pairs, the preset | All, with the clamp | Median +19.6 s (+1.5 percent), p90 +28.3 s (+4.8 percent), maximum +56 s (+15 percent). Most of the cost is the roads at 5 to 7 m/s at each end of a trip. |
| Free-flow route time, Waterloo to Bank, LondonFull | All, with the clamp | +31.3 s, from 155 s to 186 s |

No roads-only result exists for validation, the layout audit, the bank audit, or route time.
The count of 33 shape pairs is not a target pin.
Slice 0 measures the scope of this proposal (section 8).

## 3. Effects that the build must check

The study did not measure these effects.
Each item says where the code decides it.

### 3.1 Platoons and one speed limit

The model requires one speed limit on each route of a platoon link (`docs/model.md:274`).
`tryLink` (`platoon.go:567`) checks it at `:582`, and the restore checks it again at `state_physical.go:731`.
`oneSpeedLimit` (`platoon.go:622`) needs each lane of the route ahead, from the current lane, to have the limit of the current lane.
It also needs the lowest and the highest limit of the lanes of the destination station to equal that limit (`:628`).
`newPlatoonIndexData` collects those limits from each lane with a `StationID` (`:779` to `:783`), and each road has the `StationID` of its station (`london.go:941`, `:957`).

So a pod links only when each lane to its destination, and each lane of the destination station, has one speed limit.
Today each London lane with a `stationID` has 14 m/s (rerun).
After the rule, with the ramp speed, these passenger stations get a road slower than 14 m/s (rerun):

| Preset | Passenger stations | With a slower road, with the clamp | With a slower road, when a road below 12.5 m keeps its chord |
| --- | ---: | ---: | ---: |
| LondonFull | 309 | 282 | 280 |
| The preset, with the hub roads filleted | 309 | 282 | 280 |

LondonFull sets a platoon limit of 4 (`london_full.go:144`).
The code predicts that platoon links form only toward the 27 to 29 passenger stations with no slower road.
The rerun did not check which stations are platoon destinations today.
The 8 Compact stations have aisle and bay lanes at 2 m/s with no `stationID`, and the Parking facilities have depot lanes.
These lanes can already stop a link toward those stations, but the route can grow after the link forms (`platoon.go:618` to `:621`), so this is not verified.
On the preset, `TestSharedExpressEnvelopeRun` restores at the first platoon link, and the policies-off check of SX4 needs a platoon link (`express-shared-track-qualification-proposal.md:221`, `:234` to `:237`).
Under decision FL-D5, the preset sets each hub road back to its chord and to 14 m/s before `apply` (section 3.7).
So each lane of the two Express hubs keeps 14 m/s, and links toward the hubs stay possible.
Slice 0 counts the platoon links of a 60-minute run of each preset, before and after.
Decision FL-D3 asks the maintainer to choose the speed rule with this effect known.

### 3.2 Speed limit of a whole lane

The speed limit applies to the whole lane, so a fillet with a small radius slows the whole road.

- `moveStep` caps the command at the limit of the lane (`move_step.go:20`).
- `speedBeforeLane` brakes a pod before the start of a slower lane (`traffic.go:919`).
- The route search costs a lane at `length / speed limit` (`laneSeconds`, `network.go:488`).
  A station with roads of different speeds can get a different arrival arm, and a different route.
- Bank routes (`bank_routes.go:290`) and the pickup estimate (`pickup_estimate.go:369` to `:374`) read the same limit.
- The slow-lane retention of `retention.go:115` and `:165` applies only to a straight lane below 3.74 m/s, so a curved fillet never gets it.
- `admissionRequest` sets the reservation horizon from the larger of the pod speed and the limit of the current lane (`claim_surrender.go:107`).
  A slower road shortens the reservation ahead of a pod on it.
- The coasting hold distance uses the limit of the lane of the denied request (`coast.go:164`).
- `coastRelease` uses the limit of the current lane of each releaser for the time to its release distance (`coast.go:191`).

The paired runs of the compare study (FL-D6) check each effect of this list on LondonFull.

### 3.3 Lateral pairs and junction conflict zones

Since the [always-on proposal](shape-clearance-always-on-proposal.md), `newMergeLateral` (`lateral_merges.go:41`) builds the lateral pairs of each network.
A pair is lateral when all of these are true:

- Both lanes end at the node, or both start there (`lateralMergePair`, `:158`).
- Neither lane is a depot or maneuver group lane (`:45` to `:47`).
- The body range of neither lane reaches its far end (clause 5, `lateralMergeExtent`, `:249`, and `reachesFar`, `:273`).

Each other pair gets the isotropic extent of `conflictExtentWithClearance` (`junction_conflicts.go:144`), through `buildJunctionConflicts` (`:33`).
`newLaneCells` (`traffic.go:150`) adds the junction resource to each cell that meets an extent.

The fillet changes these pairs at four places:

- At the diverge node, each filleted road-in of a station now arrives at the analytic heading of `access-in`.
  The road-in lanes form converging pairs with each other, and the filleted ones now meet with one analytic tangent.
  On the sampled paths, they meet at the residual angle of section 2.1.
- At the merge node, each filleted road-out now leaves at the analytic heading of `access-out`, so the road-out lanes form diverging pairs with one analytic tangent.
- At an arrival portal, the road-in now leaves at the heading of the link, and it forms diverging pairs with the junction moves of that portal at new angles.
- At a departure portal, the road-out and the moves into that portal form converging pairs at new angles.

Near a node where two lanes meet at a residual angle near zero, their bodies stay close for a longer distance.
So a lateral extent can grow, and a pair can fail clause 5 and fall back to the isotropic extent.
Slice 0 counts the lateral pairs, the pairs that clause 5 excludes, and the zone lengths by the method of section 1 of the [lateral merge zones](../design/lateral-merge-zones.md) design, before and after.

### 3.4 Shape pairs and the bank audit

`shapePairs` (`shape_pairs.go:200`) certifies pairs of lanes that share no node.
`auditBankGeometry` (`bank_geometry.go:168`) skips a pair that shares a node, a certified pair, and a pair that a plane exempts (`:231` to `:246`).
It refuses each other pair that comes closer than its clearance, with the text of `planeGapText` (`:289`).
A lateral pair or a shape pair uses the clearance of its table (`pieces`, `:296`).
Each other pair uses `pairPieces` (`retention.go:288`), under the retention rules.
It needs 12 m (`Clearance`) for two small paths, or 20 m (`largeClearance`) when either path is large.
For two small paths, a retention window can lower the clearance of a piece to `compactClearance` (`retention.go:31`).
The road-in and the road-out of one link now both run along the link near its portals, so they can form new shape pairs or new refusals.
`TestLondonFullSharedExpress` pins 47 shape pairs on the preset (`london_full_shared_test.go:39`), and `TestSharedExpressShapePairSweep` certifies each of them (SX2).

### 3.5 The front bound of rule 1

`envelopeFront` (`profile_geometry.go:190`) gives a curved lane with no geometry tail the front bound `min(6 / (c (1 - conflictSlack)), 16.2)`.
`c` is the least projection of a segment of the lane on its chord (`chordProjection`, `:153`), and 16.2 m is `smallFrontCap` (`:182`).
A straight road with no geometry tail has no such front, and its anchor front is 6 m.
A fillet with a turn of 90 degrees and equal legs has `c = 0.71` and a front of 8.5 m.
A fillet with `c` of 0.37 or less gets the cap of 16.2 m.
The front is read at `traffic.go:211`, `:497`, and `envelope_reach.go:202`.
A larger anchor front adds planes to the envelope reach, so it changes rule A1, the D6 oracle, and the audit of section 3.4.

### 3.6 Tails of large paths

`indexGeometryTails` (`profile_geometry.go:59`) gives the tail bounds of each lane.
Preparation refuses a lane whose bound is not finite (`prepared.go:185` to `:187`).
On the preset, the three refusals of section 2.4 come from this check.
`LaneTailOverruns` (`profile_geometry.go:103`) gives the tail warnings, and `tail_warnings_test.go:190` requires no warning on LondonFull.

### 3.7 The hubs of the preset

`sharedTrackGeometry.apply` (`london_full_shared.go:241`) moves the diverge, merge, entry, exit, and berth nodes of Waterloo and Bank after LondonFull is built (`:273` to `:300`).
It then curves `940GZZLUWLO-road-in-05` (`:302` to `:319`).
It sets only the `Control` field of that road, and it changes no speed limit.
A hub road that LondonFull fillets is therefore not tangent after `apply`, and it keeps the speed limit of its fillet.

Under decision FL-D5, the preset keeps its hub roads straight.
Before it calls `apply`, it sets `Control` to nil and `SpeedLimit` to 14 m/s on each road-in and road-out lane of Waterloo and Bank.
`apply` then gives `940GZZLUWLO-road-in-05` its curve again.
To clear the control points after `apply` is not sufficient.
That order removes the curve of `940GZZLUWLO-road-in-05`, and the hub roads keep their fillet speeds.
`oneSpeedLimit` then refuses each platoon link toward a hub, because the lanes of the destination station have different limits (`platoon.go:628`).

### 3.8 Other readers of the road shape

The London junction design lists the readers of the portal shape (`london-junctions.md:126` to `:130`).
The fillet moves no node, so these readers keep their inputs:

- The collapsed station anchors take the mean start of the approach lanes, which are the road-in lanes (`internal/view/anchor.go:36` to `:59`).
- The label ranks count the approach lanes (`anchor.go:122`).
- The size check counts nodes and lanes.

The ride page rounds the joins at a node, for display only (`internal/ride/fillet.go:10` to `:24`).
`roundNode` (`:116`) rounds the whole node when one pair of an arriving and a leaving lane turns by 3 degrees or more, and no pair turns by more than 150 degrees.
`nodePieces` (`:97`) then builds a piece, with trims, for each pair at the node.
A filleted road with no analytic turn at a node therefore gets a piece and trims when another lane at that node turns.
This occurs where a filleted road meets a road that keeps its chord (FL-D2) or a junction move.
Slice 0 checks the node-wide rounding and the trims at each node where filleted roads and chords meet.
The ride goldens (`internal/ride/testdata/fillets.json`, `levels.json`) use test networks, not London, so they do not move.

### 3.9 Debris footprints

`debrisFootprint` (`debris.go:103`) takes the cells of the lane near the debris segment.
It then collects the lateral neighbors of the lane: its depot lateral partners, its lateral merge partners, and its shape partners (`:125`).
It adds each cell of a neighbor that comes closer than 12 m (`Clearance`) to the segment (`:137`).
`debrisSegment` refuses a segment whose footprint holds a berth resource (`:86`).
A fillet changes the lateral and shape pair tables (sections 3.3 and 3.4), so it changes the footprint of debris on a changed road and on its partners.
The blocked set (`faults.go:336`), the owner rebuild (`ownership.go:43`), the release (`debris.go:235`), and the check of invariants F6 and F7 (`faults.go:654`) compute the footprint again.
The incident evidence therefore includes a check of the debris footprint on each changed road and on its partners.
The check covers a new fault, a restore, and the recovery chain.

## 4. Build shape

- `LondonOptions` gets a field `RoadFillets` (`london.go:162`).
  Its zero value keeps the roads straight.
- `londonFullOptions` (`london_full.go:87`) sets it.
- `expressBaseOptions` (`london_full_express.go:60`) clears it, so D does not change.
- `DefaultLondonCentralOptions` does not set it, so LondonCentral does not change.
- With `Junctions: LondonRamps`, the field has no effect.
- `londonCapacity.equal` compares the field, so a build with another value is not the cached preset.
- The fillet is one pass over the finished network, by the road IDs that `addLondonStation` writes (`london.go:938`, `:954`).
  By decision FL-D5, the preset sets each hub road back to `Control` nil and `SpeedLimit` 14 m/s before `sharedTrackGeometry.apply`, which then curves `940GZZLUWLO-road-in-05` again (section 3.7).
- `cmd/scenario` gets a flag `-road-fillets` for the London presets (decision FL-D8), and refuses it for D, as it refuses `-junctions`.

The existing pins of section 5.1 show that LondonCentral and D do not change.

## 5. Pins, goldens, records, and gates

"Routine" means a pin or a golden that moves in the commit that changes the geometry, with the reason in the commit message.
"Qualification" means contract evidence that the maintainer reads.

### 5.1 Pins that must not move

| Pin | File and line |
| --- | --- |
| D network SHA-256 `3aa0a2eb...` | `london_full_shared_test.go:261`, `cmd/scenario/main_test.go:131` |
| D output SHA-256 | `cmd/scenario/main_test.go:124` |
| D project output | `layout_audit_test.go:32` |
| D network fixture | `internal/sim/testdata/express_network.json.gz`, `TestExpressNetworkFixture` (`express_network_fixture_test.go:23`) |
| D behavior | `internal/session/testdata/london_express_parity.json` |
| LondonCentral output and network | `cmd/scenario/main_test.go:122`, `:129`, `:182` to `:186`, `:354`, `:429`, `layout_audit_test.go:30`, `london_full_test.go:188` to `:189`, and `london_options_test.go:67` |
| LondonCentral fixture | The "London" network of `internal/sim/testdata/scenario_networks.json.gz` (`conflict_fixture_test.go:34`) |
| LondonCentral behavior | The `london-central` entry of `internal/session/testdata/preset_behavior_parity.json` |
| The ramps study arms | `cmd/scenario/main_test.go:430`, `:434`, and `internal/sim/shared_track_study_test.go:60`, `:70`, and the ramps audit counts `:84` to `:85` |
| D project fixture | `internal/sim/testdata/express_project.json`, `TestExpressProjectFixture` (`express_project_fixture_test.go:20`) |
| Gate-off tables of LondonCentral, the rings, and D | `internal/sim/testdata/gate_off_tables.json`, `TestGateOffTables` (`gate_off_tables_test.go:24`) |

### 5.2 LondonFull pins that move

| Pin | File and line | Kind |
| --- | --- | --- |
| Network SHA-256 `69baad6a...` | `london_full_shared_test.go:260`, `cmd/scenario/main_test.go:130` | Routine |
| Output SHA-256 | `cmd/scenario/main_test.go:123` | Routine |
| Project output | `layout_audit_test.go:31` (`TestPresetOutputIsPinned`) | Routine |
| Wide stations | `london_options_test.go:68`, and `cmd/scenario/main_test.go:355` to `:356`, `:433` | Routine |
| Berth pitch 50 | `cmd/scenario/main_test.go:382` | Routine |
| Opt-out variants | `cmd/scenario/main_test.go:188`, `:190`, `:192` (`TestRunLondonOptOut`) | Routine |
| Study inputs, built networks, and audit counts of the tight and wide inputs | `internal/sim/shared_track_study_test.go`: the input hashes `:58`, `:59`, the built network hashes `:66` to `:69`, and the counts of refusals and shape pairs `:76` to `:83`, which `:729` and `:750` enforce | Routine, in slice 2. The study is opt-in, so slice 2 runs it with `PODSIM_SHARED_TRACK_STUDY` set to get the new values. |
| Behavior | The `london-full` entry of `preset_behavior_parity.json` (`TestPresetBehaviorParity`, `internal/session/preset_parity_test.go:128`) | Routine |
| No tail warning | `internal/project/tail_warnings_test.go:190` | Must still pass |
| Validation, layout audit, bank audit | `TestLondonFullPreset` (`london_full_test.go:19`), `TestAuditPresetsHaveNoHardConflicts` (`layout_audit_test.go:49`), `TestTailPairCoverage` (`tail_certificate_test.go:512`) | Must still pass |
| Byte limits | `TestRunWritesImportableLondonFull` (`cmd/scenario/main_test.go:682`), `TestScenarioJSONFitsFileLimit` (`:629`) | Must still pass. A fillet adds a control point and a speed limit that is not an integer: about 74 bytes for each lane in compact JSON, and about 117 bytes in the indented file of `cmd/scenario`. |
| Fleet qualification | `mise run qualify:london` (`TestLondonFleetQualification`), which builds from `DefaultLondonFullOptions` | Qualification record |

### 5.3 Pins of the preset that move

| Pin | File and line | Kind |
| --- | --- | --- |
| Network SHA-256 `5961c702...` | `london_full_shared_test.go:265` | Routine |
| Network before the parking `09b8fdea...` | `london_full_shared_test.go:264` | Routine |
| The cached build equals the generated build | `london_full_shared_test.go:248` | Must still pass |
| 47 shape pairs and the tail warning on `940GZZLUBNK-move-08-02` | `london_full_shared_test.go:39` to `:47` | Qualification (SX2). A new count needs the maintainer, because SX2 certifies each shape pair. |
| Layout and parking | `TestLondonFullSharedExpressLayout` (`:208`), `TestLondonFullSharedExpressParking` (`:92`) | Must still pass |
| Routes | `TestLondonFullSharedExpressRoutes` (`:283`) | Qualification (SX2) |
| SX fixture | `internal/sim/testdata/shared_express_project.json.gz`, `TestSharedExpressFixture` (`shared_express_fixture_test.go:38`), with `-update-shared-express` | Routine. Each SX test in `internal/sim` reads it. |
| Behavior | The `london-full-shared-express` entry of `preset_behavior_parity.json` | Routine |
| The tests of `mise run test:shared-express` | `mise.toml:108` | Qualification (SX2 to SX10) |

Today the "tight 3.5" hash of the study (`shared_track_study_test.go:67`) is equal to the "before the parking" hash of the preset (`london_full_shared_test.go:264`).
The study reads the LondonFull file of `cmd/scenario` and builds its own copy of the construction of the preset.
So in slice 2, "tight 3.5" changes with the other study pins of section 5.2, and the "before the parking" hash stays frozen until slice 4.
The study copy does not set the hub roads back to their chords (section 3.7), so the two hashes stay different after slice 4 too.
Slice 2 changes the comments that say the two networks are the same (`london_full_shared_test.go:235` to `:237`, `:262` to `:263`).

### 5.4 Live documents with numbers that move

- `docs/london-full.md:86` to `:98` (the shape table and its build note), `:118` to `:119`, `:132`, `:190` to `:191`, and `:301` to `:302`.
- `docs/studies/station-scale.md:138`, which gives the current LondonFull network SHA-256.
  It gets one sentence for the new hash.
- `docs/design/london-junctions.md`: a status line (FL-A7).

### 5.5 Records that stay

These are dated records, and the build does not edit them:

- `docs/design/shared-track-preset.md:64`, `:698`, `:731`, `:740`, `:824` to `:825`, and `docs/design/shape-clearance.md:134`.
  They give hashes from before the always-on proposal.
- The evidence of `express-shared-track-qualification-proposal.md`, from `:150`.
- The studies that measure LondonFull or the preset, for example `docs/studies/london-junctions.md`, `station-scale.md`, `london-full-postfix.md`, `platoon-followup.md`, `platoon-coupling-comparison.md`, and `qualification.md`.
  Their numbers describe the straight roads, and route times, waits, drains, and platoon counts become stale.

### 5.6 Contract text

Each row needs maintainer approval, as AGENTS.md requires for an approved contract.
A row for an applied record adds a "Note of <date>" line, as the always-on proposal did, and does not rewrite the text.

| ID | File and line | Current text (short) | Proposed text |
| --- | --- | --- | --- |
| FL-A1 | `express-shared-track-qualification-proposal.md:29` to `:31` (SX1) | "LondonFull, D, their fleets, and their defaults do not change, and their network hashes stay." | Add: "Since <date>, the roads of LondonFull and of the preset are filleted (fillets proposal), so their network hashes and their behavior changed. D did not change." |
| FL-A2 | `express-shared-track-qualification-proposal.md:22` | "The rows apply to the geometry whose network hash the preset test pins after slice 3 of the design." | Add: "Since <date>, the rows apply to the geometry of the fillets proposal, and the Evidence section records the reruns." |
| FL-A3 | `express-shared-track-qualification-proposal.md:5` to `:6`, and the status table `:124` to `:133` | "...so each row is closed and the preset is qualified." | Add after `:6`: "Note of <date>: the fillets proposal changes the geometry of the preset, so the preset is not qualified until the rows run again on the new geometry. The Chrome checks of SX6, SX7, and SX9 of 2026-11-20 stand (decision FL-D9)." |
| FL-A4 | `express-physical-encoding-contract-proposal.md:512` to `:513` (E1) | "Ordinary LondonFull, its fleet, and its defaults do not change." | Add: "Note of <date>: the fillets proposal changes the roads of ordinary LondonFull. It does not change D." |
| FL-A5 | `express-london-qualification-proposal.md:19`, `:140` (E1) | "It does not change ordinary LondonFull, its fleet, or its defaults." | Add the note of FL-A4 after the status lines. |
| FL-A6 | `shared-track-amendment-proposal.md:535`, and `docs/design/shared-track-preset.md:844`, `:873` | "LondonFull, D, ... do not change, and their network hashes stay." | Add one note after the status lines of each file: "Note of <date>: the fillets proposal changes the roads of LondonFull and of the preset. D does not change." |
| FL-A7 | `docs/design/london-junctions.md:3` to `:9` | Status lines | Add: "Since <date>, LondonFull and the shared-track preset fillet their station roads (fillets proposal). The junction moves keep their chords, and Express keeps its chords." |

`docs/model.md:14` stays true: a fillet has an explicit speed limit.

### 5.7 Gates

| Gate | Why | Command |
| --- | --- | --- |
| Unit and pin tests | Each slice | `go build ./...`, `go vet ./...`, `mise run lint`, `go fix -diff` on the changed packages, `go test ./...`, `mise run test:web` |
| LondonFull compare study | Behavior of LondonFull (decision FL-D6) | `cmd/compare` with the method of the [station scale study](../studies/station-scale.md), at 15 requests per minute and one higher rate, seeds 1 to 3, with a timed pilot first. The arms use `-platoon-policies virtual`, as that study does (`station-scale.md:151`), because `cmd/compare` does not read `platoonLimit` (`docs/model.md:271`). |
| D unchanged | Section 5.1 | `mise run test:london-express`, and the D pins of `go test ./internal/scenarios ./cmd/scenario` |
| The preset | SX2 to SX10 | `mise run test:shared-express` |
| CI | A16, SX11 | `mise run check` on the tip |
| London soak row | SX10 | `PODSIM_SOAK_PRESETS=london-full-shared-express mise run soak` |
| Release soak | A19, SX11 | One timing seed per preset first, then `mise run soak:full` with 2 seeds |
| Timed runs | SX12 | The runs of the SX12 record: `TestIncidentSoakTimed` with `PODSIM_SOAK_GATE=1`, `TestSampledTickLatency`, `TestEmergencyChoiceLatency`, and `TestIncidentWorstCaseStepTime` |
| Server cost | SX7 | The server measurement of the SX7 record, 4 cases |
| Byte bounds | SX5, SX13 | `TestLondonSharedExpressByteBounds`, and `mise run test:embedded` |
| Edit times | Validation cost of more curved lanes | `TestDraftEditTimes` with the protocol of section 6.1 of the always-on proposal, on `london-full` and the preset, against its limit of 1.25 times the value at the base commit of the build |
| Chrome | SX6, SX7, SX9 | None. The checks of 2026-11-20 stand (decision FL-D9). |

## 6. The shared-track qualification

A change to the preset reopens its qualification.
The rows apply to the geometry whose network hash the preset test pins (`express-shared-track-qualification-proposal.md:22`).
After the always-on proposal, the hash of the preset changed, but its behavior did not, and the parity test showed it (SX1, `:30`).
A fillet changes behavior, so no parity argument carries the evidence.

| Row | Effect of the fillet |
| --- | --- |
| SX1 | The LondonFull hash and the hash of the preset change. FL-A1. |
| SX2 | Audit result, shape pairs, tail warning, routes, and the shape pair sweep run again. |
| SX3 | The envelope run, the envelope tests, and the support tests run again. |
| SX4 | The checkpoint script takes each state at its first sight, so it adapts. The platoon restore and the policies-off platooning check need a platoon link (section 3.1). |
| SX5, SX13 | The proofs pad a worst-case project, which does not change. The measured sizes of the preset run again. |
| SX6, SX7, SX9 | The Go tests run again. The Chrome checks of 2026-11-20 stand (FL-D9). |
| SX8 | The incident fixtures and the recovery chain run again, with the debris footprint check of section 3.9. |
| SX10, SX11 | The London soak row, the CI soak, and the release soak run again. |
| SX12 | The timed runs run again. |

The maintainer decided three things on 2026-11-20:

- The preset changes in a second stage, after the maintainer accepts the LondonFull evidence (FL-D4).
- The hub roads of the preset keep their chords (FL-D5).
- The Chrome checks do not repeat (FL-D9).

If the preset does not change, it stops deriving from the default LondonFull.
It then builds on a LondonFull without fillets, and the test at `london_full_shared_test.go:248` compares it with that build.
Each construction of the preset uses that one base (section 8, slice 2).

## 7. Risks and stop conditions

| Condition | Action |
| --- | --- |
| A pin of section 5.1 moves | Stop. D or LondonCentral changed. |
| Slice 0 finds a validation refusal, a hard layout conflict, a bank audit refusal, or a tail warning on LondonFull | Stop. Report the lane and the check. |
| Slice 0 finds a refusal on the preset, or a shape pair count other than 47, or a tail warning other than the pinned one | Stop. Report the pairs. A new count is pinned only with maintainer approval. |
| Slice 0 finds no platoon link in a 60-minute run of LondonFull or the preset | Report it with decision FL-D3, before slice 2. |
| The LondonFull compare study fails the criterion of FL-D6 | Stop. The maintainer chooses between the options of FL-D2 and FL-D3 again. |
| Any oracle violation: `Check`, the D6 oracle, or the envelope parity test | Stop. |
| The SX4 policies-off platooning check cannot reach a platoon link | Stop for that check, and bring it to the maintainer. |
| A late record in a soak, or a failed latency gate after one rerun alone | Stop, and report it with the trace. |
| An edit time above its limit | Stop. |
| A byte bound over a cap | Stop. No cap changes without a decision. |
| The release soak does not fit the 2-hour limit | The maintainer chooses, as for AO-D8. |

## 8. Build slices and order of evidence

Each slice passes `go build ./...`, `go vet ./...`, `mise run lint`, `go fix -diff` on the changed packages, `go test ./...`, and `mise run test:web` by itself.

| # | Commit subject (draft) | Packages | Tests and evidence |
| ---: | --- | --- | --- |
| 0 | None: a scratch measurement | Scratch tree | Build the rule with the decisions of section 9. On LondonFull and the preset, run validation, the layout audit, the bank audit, the shape pair count, and the tail warnings. Count the lateral pairs, the clause 5 exclusions, and the zone lengths (section 3.3). Count the platoon links of a 60-minute run (section 3.1). Measure the residual join angles on `Network.Polyline` (section 2.1). Check the ride rounding and trims of section 3.8, and the debris footprints of section 3.9. Measure the free-flow route times of the study, roads only. Each stop of section 7 goes to the maintainer before slice 1. |
| 1 | `scenarios: Add an option to fillet the London roads` | `internal/scenarios`, `cmd/scenario` | The option of section 4, off for each preset, and the flag of FL-D8. Unit tests: analytic tangency at both ends, the speed of FL-D2 and FL-D3, each case that the rule leaves straight, no effect with ramps, and `londonCapacity.equal`. Each test is checked by mutation. No pin moves. |
| 2 | `scenarios: Fillet the roads of LondonFull` | `internal/scenarios`, tests of `cmd/scenario`, `internal/sim`, `internal/session` | LondonFull sets the option. Under FL-D4 B, the preset builds on LondonFull without fillets. Each construction of the preset uses that one base: the generator `GenerateLondonFullSharedExpress` (`london_full_shared.go:63`), and the tests that build it from `LondonFull()`. These tests are `TestLondonFullSharedExpress` (`london_full_shared_test.go:27` to `:28`), the parking, layout, and routes tests (`:94`, `:210`, `:285`), the cached build and the build before the parking (`:240`, `:251`), and `TestSharedExpressFixture` (`shared_express_fixture_test.go:40`). The other packages build the preset through the generator. The pins of section 5.2 move, and the comments of section 5.3 change. The pins of sections 5.1 and 5.3 do not move. |
| 3 | `docs: Describe the filleted LondonFull roads` | `docs/` | Section 5.4 for LondonFull. `mise run lint` |
| | Evidence of LondonFull | | The compare study of FL-D6, `mise run qualify:london`, and `mise run check` on the tip of slice 3. The maintainer reads the result before slice 4 (FL-D4 B). |
| 4 | `scenarios: Fillet the roads of the shared-track preset` | `internal/scenarios`, `internal/sim/testdata`, tests of `internal/sim`, `internal/session`, `internal/remote` | The preset builds on the filleted LondonFull. Each construction of slice 2 goes back to `GenerateLondonFull` or `LondonFull()`, and the unfilleted base goes. The hub roads follow FL-D5 and section 3.7. The pins of section 5.3 move, except the shape pair count, which needs the maintainer. |
| 5 | `contracts: Apply the fillet amendments` | `docs/contracts/`, `docs/design/` | Rows FL-A1 to FL-A7, after maintainer approval. `mise run lint` |
| 6 | `contracts: Record the evidence of the filleted preset` | `docs/contracts/` | The gates of section 5.7 in this order: `mise run test:shared-express` and `mise run test:london-express`, then `mise run check`, then the London soak row, then the SX12 timed runs and the SX7 server measurement on a quiet host, then one timing seed per preset, then `mise run soak:full`. The records go in the SX proposal and in this proposal. The Chrome checks do not repeat (FL-D9). |

Run the race task of each changed package once on the tip of slice 4: `mise run test:race:sim`, `test:race:session`, and `test:race:other`.
Under FL-D4 A, slices 2 and 4 are one commit, and the LondonFull evidence runs with the evidence of slice 6.
Under FL-D4 C, slices 4 and 6 do not happen, and slice 5 has FL-A4, FL-A5, FL-A7, and an SX1 note that the preset keeps its geometry.

### 8.1 Evidence of stage 1

The runs below are from 2026-11-20, with Go 1.27.4.
Each run used `nice -n 10` in a systemd scope with a memory limit of 16 GiB.
The scratch probes of this record are deleted.

#### Slice 0

Slice 0 built the rule in a scratch tree at `7531397e`.
LondonFull had the fillets, and the preset had the fillets with its hub roads straight (FL-D5).
The rule gives the rerun numbers of section 2.3 exactly.
Before the floor of FL-D2 B, it curves 639 road-in lanes, 646 road-out lanes, and 22 of the 24 Parking roads.
With the floor, it curves 676 lanes on LondonFull: 320 road-in lanes, 340 road-out lanes, and 16 Parking roads.
The median speed of a curved road is about 11 m/s.
61 roads have a smallest radius below 5 m, and the smallest radius is 0.043 m, on `940GZZLUWLO-road-out-02`.
These roads keep their chords.
The largest growth of a road length is 78.3 m, and the longest control leg is 151 m, both on `940GZZLUWLO-road-in-02`.
The proposal does not limit either value.

| Item | LondonFull, before and after | The preset, hub roads straight, before and after |
| --- | --- | --- |
| Network SHA-256 | `69baad6a...` and `03d04786...` | `5961c702...` and `cba69149...` |
| Lanes that change | 676, with 9 at the hubs | 667, with none at the hubs |
| Validation, layout audit, bank audit | Pass, with no hard and no soft conflict | Pass, with no hard and no soft conflict |
| Shape pairs | 0 and 0 | 47 and 47 |
| Tail warnings | 0 and 0 | 1 and 1, on `940GZZLUBNK-move-08-02` as pinned |
| Passenger stations with a lane slower than 14 m/s | 0 and 280 of 309 | 0 and 278 of 309 |
| Platoon links in 60 minutes, seeds 1, 2, and 3 | 10, 7, 4, and 0, 0, 0 | 3, 8, 7, and 1, 0, 1, each toward Bank |
| Linked pod-seconds, seeds 1, 2, and 3 | 220, 2,110, 1,348, and 0, 0, 0 | 295, 1,906, 1,298, and 14, 0, 10 |
| `Check` at each second of these runs | 3,600 of 3,600 pass | 3,600 of 3,600 pass |
| Completed trips, seeds 1, 2, and 3 | 659, 674, 645, and 661, 674, 647 | 660, 673, 647, and 656, 678, 647 |
| Lateral pair sides, clause 5 exclusions, lateral pairs | 23,972, 998, 11,487, and 23,972, 1,002, 11,485 | 23,984, 992, 11,496, with no change |
| Residual join angle of a changed road at its start, p50, p90, maximum | 46.8, 56.4, 77.7 degrees, and 0.40, 0.56, 1.64 degrees | The same values |
| Residual join angle of a changed road at its end, p50, p90, maximum | 46.6, 56.5, 89.9 degrees, and 0.40, 0.55, 1.72 degrees | The same, with a maximum of 1.53 degrees |
| Free-flow time from Waterloo to Bank | 155.1 s and 173.2 s | 158.3 s and 158.3 s |
| Free-flow change of 100 seeded pairs, p50, p90, maximum | +3.1 s, +9.7 s, +17.7 s, which is +0.20%, +1.09%, +5.31% | The same values |
| Ride: nodes at changed roads that round | 1,229 and 1,202, with no node kept sharp | 1,216 and 1,189 |
| Debris footprints that change | 4,026 of 8,247 segments, 2,225 larger and 360 smaller | 3,920 of 8,016 segments |
| Debris resources, p50 and maximum | 5 and 23, and 6 and 24 | The same values |
| Debris berth refusals | 0 and 0 | 0 and 0 |

The platoon runs use the am-peak profile at 15 requests per minute and 900 requests, with the settings of a session, without redistribution.
A link is a new pair of a pod and the pod ahead, at samples of 1 s.
The free-flow times go from the station exit to the station entry, at the length of each lane over its speed limit.
The debris rows use three segments of 1 m on each changed road and on each lane near it, at 10%, 50%, and 90% of the lane.

The lateral pair counts do not change much, but the junction zones on the filleted roads get longer.
The method is that of the [lateral merge zones](../design/lateral-merge-zones.md) design, on the changed lanes of LondonFull.

| Lanes and node | Zone p50, p75, p90, before | After |
| --- | --- | --- |
| Road-in lanes at the diverge node | 13 m, 13 m, 37 m | 17.7 m, 71 m, 103 m |
| Road-out lanes at the merge node | 13 m, 13 m, 29 m | 17 m, 85 m, 107 m |
| Road-out lanes at the departure portal, p75 | 13 m | 39 m |
| Road-in lanes at the arrival portal, p75 | 13 m | 38 m |

The stop of section 7 for platoon links fired: LondonFull formed no platoon link in the 60-minute runs of seeds 1, 2, and 3.
Each other stop of section 7 for slice 0 was clear.
The D6 oracle and the envelope parity test did not run in slice 0, because they read the SX fixture, which was not current in the scratch tree.
The maintainer then approved slice 1 and the compare study of FL-D6, with a third arm at 14 m/s.
After the compare study, the maintainer kept FL-D3 A, the ramp speed.

#### Compare study of FL-D6

The study ran at `6a3a77cd`, the commit of slice 1, with the method of the [ramps study](../studies/london-junctions.md).
The arms are:

- (a): LondonFull with straight roads, network `69baad6a...`.
- (b): LondonFull with `-road-fillets`, network `03d04786...`.
- (c): the project of (b), with a speed limit of 14 m/s on each road that has a control point.
  This changes 673 lanes, and the other 3 curved roads already had 14 m/s.

```sh
scenario -preset london-full -output a.json
scenario -preset london-full -road-fillets -output b.json
compare -project <arm>.json -pattern profile -bands am-peak -loads 4s,3s -seeds 1,2,3 \
  -duration 120m -arrivals-for 60m -stop-when-drained -redistribution-policies off \
  -platoon-policies virtual -queue-limit 1000000 -workers 3 -format csv
compare -project <arm>.json -pattern profile -bands am-peak -loads 4s -seeds 4,5,6 \
  -duration 120m -arrivals-for 60m -stop-when-drained -redistribution-policies off \
  -platoon-policies virtual -queue-limit 1000000 -workers 3 -format csv
```

A load of `4s` is 15 requests per minute, and `3s` is 20 requests per minute.
At 15 requests per minute, (b) drained one more seed than (a) on seeds 1 to 3, so seeds 4 to 6 also ran, as FL-D6 requires.
A timed pilot of seed 1 at 15 requests per minute took about 25 s and 780 MB for each arm.
Each arm then took about 65 s with 3 workers, and seeds 4 to 6 took about 26 s more.

At 15 requests per minute, seeds 1 to 6, the criterion of FL-D6:

| Value | (a) | (b) | (c) |
| --- | ---: | ---: | ---: |
| Seeds that drain | 5 of 6 | 6 of 6 | 5 of 6 |
| Mean wait | 196.5 s | 201.9 s | 196.9 s |
| Mean journey | 870.4 s | 883.4 s | 872.8 s |
| Mean of the paired wait changes | | +2.83% | +0.27% |
| Mean of the paired journey changes | | +1.50% | +0.29% |
| `platoon_time_percent` | 0.233 | 0.012 | 0.299 |
| `junction_wait_seconds`, mean of the seeds | 1,037 | 2,140 | 1,418 |
| `track_wait_seconds`, mean of the seeds | 279 | 391 | 326 |
| `stopped_pod_seconds`, mean of the seeds | 1,592 | 2,830 | 2,046 |

Seed 1 of (a) and of (c) ends with one request that is not served.
The paired wait changes of (b) are from +2.0% to +4.3% on each seed, and its paired journey changes are from +1.2% to +1.8%.
(b) passes FL-D6, and (c) also passes.
(b) has platoon time only on seed 1, 0.071%.

At 20 requests per minute, seeds 1 to 3, for information:

| Value | (a) | (b) | (c) |
| --- | ---: | ---: | ---: |
| Requests not served at the end of seeds 1, 2, and 3 | 1, 3, 2 | 6, 3, 4 | 3, 4, 4 |
| Mean wait | 336.1 s | 367.5 s | 350.6 s |
| Mean journey | 1,025.6 s | 1,058.3 s | 1,037.7 s |
| Mean of the paired wait changes | | +9.38% | +4.20% |
| Mean of the paired journey changes | | +3.19% | +1.16% |
| `platoon_time_percent` | 0.382 | 0.000 | 0.510 |
| `junction_wait_seconds`, mean of the seeds | 1,554 | 3,443 | 2,391 |

The cost of (b) is mostly junction wait, which is about 2.1 times that of (a).
This agrees with the longer junction zones of slice 0.
The platoon probe of slice 0, on seeds 1 to 3 of (c), gave 6, 9, and 10 platoon links toward different stations, and each `Check` passed.

#### Slices 1 to 3

| Commit | Content |
| --- | --- |
| `6a3a77cd` | Slice 1: the option, the rule, the `-road-fillets` flag, and the unit tests. 23 mutations of the rule and the option, each killed. No pin moved. |
| `b791cb93` | A test of a road-out between two curved lanes, after a review found that the tests did not tell the chords of the neighbor lanes from their tangents. |
| `3641980c` | Slice 2: LondonFull sets the option, and the preset builds on LondonFull with straight roads. |
| The commit of slice 3 | The live documents of section 5.4 and this record. |

Slice 2 moved these pins of section 5.2:

| Pin | Before | After |
| --- | --- | --- |
| LondonFull network | `69baad6a...` | `03d04786f72780fe4e0e1a65ee90eefcb8e20fc78ddd0c2b0600556cbea79ec5` |
| LondonFull output | `f9b42a16...` | `66be4a220537418a674e8a90a37e9bc8d898633f71158177faecf22b3eedbbaa` |
| LondonFull project output | `b5d70846...` | `a2669dc0cf5eed9ea435e5b2905ea1b84ca781de23d5e0de3ff43f9cf7d12bad` |
| Wide stations, project | `4861d23f...` | `9ec575882c3f108139c9cd05e7305bac44c8de1a50de407760fa408e958a22cd` |
| Wide stations, network | `c8a1cbb1...` | `413119c9bd469ccc50ab755683a6bcc1f27eb78ac5d6b1105b142a482f48b9d3` |
| Wide stations with `-berth-pitch 50` | `2050d857...` | `22067582851892434e1998166192c914cc463a5accd9b57e21fe732d480ed4b2` |
| Tight stations with `-berth-pitch 50` | `f0defafe...` | `9503e65cd30cbe9b861552ec9d96e76b939b0be9e744b7ff85105ddd9ea2e0ec` |
| Opt-out, no depots and no Compact stations | `fa1dc35e...` | `c34a657f750919f107fc917f137a5df851c86c0f83057e9bf1b006cce49d3006` |
| Opt-out, no depots | `a0975e28...` | `697474d1d483e04b703aa2657b60afcd21d973daa6354c5dc7de267b34fec811` |
| Opt-out, no Compact stations | `aa2e5c64...` | `bd9f3d1565c6b1a12f3f76fa0c63872892299c496e991ea8aab86595a3bcd0e2` |
| Study input "tight", file and network | `f9b42a16...`, `69baad6a...` | `66be4a22...`, `03d04786...` |
| Study input "wide", file and network | `55a15f15...`, `c8a1cbb1...` | `80b1bb3c79e20df09beb6d5024957be13fb30324fc2d3adbe0ad95bd795c5a91`, `413119c9...` |
| Study network "tight 2.1" | `8906fd2b...` | `8cbf62c9348af2f1706ccf4c49cb3fea40c9b1537650b3f619707757535e063f` |
| Study network "tight 3.5" | `09b8fdea...` | `41b3f9f521169842cf12c51164651155d410a5cadedd90feabf1da4fbc76b4d7` |
| Study network "tight 3.6" | `750ce680...` | `6ded06170cd20c972e6be7b116af5d00a4ccc10e67dc31758db811fc2cb47ca2` |
| Study network "wide 2.1" | `45ad760f...` | `019262cfdedc1a835082d25ee3bf276c193faa011d4ae0200f073acdc627c8e7` |
| Study refusals, refusals under 4.0 m, shape pairs, "tight 2.1" | 7, 5, 39 | 9, 7, 39 |
| The same, "tight 3.5" | 0, 0, 38 | 3, 2, 35 |
| The same, "tight 3.6" | 0, 0, 23 | 3, 2, 20 |
| The same, "wide 2.1" | 7, 5, 39 | 5, 4, 31 |
| `london-full` record of `preset_behavior_parity.json`, topology | `0e282062...` | `40d35b05d1736491ec9b82affcb3e59bb5ecea3a199fd7334617bb85de7de8a0` |

Each checkpoint digest of the `london-full` record also changed.
The test wrote the record again with `-update`, and the other records of the file did not change.
The ramps study input and its counts did not change.
No pin of section 5.1 or 5.3 moved.

The slice 2 gates passed: `go build ./...`, `go vet ./...`, golangci-lint, `go fix -diff`, `go test ./...` without `-short` in 523 s, and the 415 web tests.
No latency gate failed.
A scratch run of the filleted LondonFull for 20 simulated minutes ran `Check`, the D6 oracle, and the envelope parity test at each of 36,000 ticks, and each passed.
The run had 300 trips, 99,616 pod-ticks on curved roads, and 681,810 pod-ticks on lanes with a lateral pair.
`go test ./...` also ran the D6 oracle and envelope runs of D and the preset, which did not change.

`mise run qualify:london` passed at `1188c43e`, the tip of slice 3, in 89 s.
`mise run check` passed at `1188c43e` in 2,426 s.
Stage 1 then waits for the maintainer to read this evidence (FL-D4 B).

A review of stage 1 found two gaps in this evidence, and two runs at `f2655712` closed them.

The edit times of `london-full` ran in wasm with the protocol of section 6.1 of the always-on proposal, against `b04eac36`, the commit before the fillets.
The runs alternated the two builds for 3 rounds on an AMD Ryzen 5 3600 with GOMAXPROCS 1, Go 1.27.4, and Node 24.22.3, at a load average from 0.3 to 1.5.
The table gives the median of the 3 rounds, in ms.

| Mode and verdict | p95 before | p95 after | Max before | Max after |
| --- | ---: | ---: | ---: | ---: |
| Cold, accepted | 2,110.0 | 2,081.0 | 2,150.8 | 2,170.3 |
| Cold, refused | 1,780.7 | 1,818.1 | 2,070.9 | 1,824.7 |
| Cached, refused | 1,235.4 | 1,199.3 | 1,322.3 | 1,265.1 |
| Cached, accepted | 1,358.7 | 1,364.5 | 1,411.7 | 1,395.0 |

The largest ratio is 1.02 for a p95 and 1.01 for a maximum, against the limit of 1.25.

A scratch probe ran debris incidents on three filleted roads of LondonFull, one at a time and then all three together, with demand and the targeted trips that use each road.
The roads are `940GZZLUKSX-road-out-06`, with the largest growth of the debris footprint (1 to 18 resources), `940GZZLUKSX-road-in-04` near its arrival portal (9 to 17), and `parking-east-road-out-03` (3 to 12).
Each incident saved and restored the session through the store at its start, in its middle, after the clear, and after the recovery, 16 times in all.
Each restore took the physical tier at the same tick and lost no trip.
Over 36,430 ticks, the fault records (F1 to F13, with F6 and F7), `Check`, the D6 oracle, and the fault transitions passed at each tick.
The contract check and the footprint check passed at each second and at each restore.
The footprint check compares the debris footprint, the fault owners, the owner rebuild, and the blocked resources.
The debris records were the same before and after each restore.
With the release of the neighbor cells removed from `releaseDebris`, the probe failed at the clear, so the checks can fail.

#### Findings that differ from the text

- Section 2.1 gives the check for rays behind an end before the check for a tangent end.
  The code does the tangent check first, because only this order gives the rerun counts on the passenger station roads: 15 tangent roads and 132 roads with rays behind an end.
  On all 1,456 roads, this order gives 15 tangent roads and 134 roads with rays behind an end, because 2 Parking roads also have rays behind an end.
  With the check for rays behind an end first, the counts are 9 tangent roads and 140 roads with rays behind an end, because 6 of the 15 tangent roads also have `s` or `t` at most 1e-6.
  The two orders curve the same roads.
- Section 2.1 gave 132 roads with rays behind an end; the code and its test give 134, which count the 2 Parking roads.
  On 2026-11-21, the maintainer approved the change of section 2.1 to 134.
- Section 2.1 did not list a heading or a chord with no direction.
  A review of stage 1 found that such a vector made NaN values that pass each check, and the rule now keeps the chord for it (`londonDirected`).
  No London road has such a vector.
  On 2026-11-21, the maintainer approved the addition of this case to section 2.1.
- Sections 5.2 and 8 do not list four tests that slice 2 changed:
  - `checkLondonParity` (`london_depot_test.go`), which `TestLondonDepots` and `TestLondonCompactStations` use.
    The curve and the speed of a road come from the heading of its access lane, and a depot or Compact layout changes that heading.
    The check now lets a road of a replaced station differ only in its control point and its speed limit, only when the options fillet the roads, and only to the values that the rule gives on the network of the variant.
    Of 15 mutations of the check and of the generator, 14 were killed.
    The mutation of a road of a Compact station survived, because the check already ignores each lane of a Compact station.
  - `TestRunRoadFillets`, `TestLondonRoadFilletsOption`, and `TestLondonRoadFilletsOnLondonFull`, which expected LondonFull without the option.
- Section 5.3 does not say that the "tight 3.5" network of the shared-track study now has 3 refusals.
  The study copy does not set the hub roads back to their chords (section 3.7), so its filleted hub roads refuse `940GZZLUWLO-road-out-01` against `london-link-183-ba-1` at 1.69 m, `940GZZLUWLO-road-in-02` against `london-link-100-ab-2` at 5.26 m, and `940GZZLUBNK-road-out-08` against `london-link-097-ba-2` at 0.00 m.
- Open item for the maintainer: the containment test of the shared-track study now fails on the "wide" input.
  It has no failure of the proposal, but the count "off-support lane in the body, declared separated from the envelope (A1), gate" is 0, against its minimum of 1.
  With straight roads, the count was 624, from large pods on `940GZZLUBNK-road-in-08`, and the fillet moves that road.
  The study is opt-in, so CI does not run it.
  The maintainer chooses a minimum for each input, another A1 case on the "wide" input, or no change.
  On 2026-11-21, the maintainer chose a new "wide" input with the minimum kept.
  The input is now `-preset london-full -wide-stations -road-fillets=false`, the straight roads that the preset builds on (FL-D4 B), and its A1 count is 624 again.
  No input of the study now has wide stations with filleted roads.
- Section 5.2 says that a fillet adds under 100 bytes for each lane.
  This is true only for compact JSON, where the 676 curved lanes add 49,696 bytes, about 73.5 bytes for each lane.
  In the indented file of `cmd/scenario`, which `TestRunWritesImportableLondonFull` and `TestScenarioJSONFitsFileLimit` check, they add 78,764 bytes, about 117 bytes for each lane.
  Both files stay far below the limit of 32 MiB.
  On 2026-11-21, the maintainer approved the correction of section 5.2.
- Open item for the maintainer: the approved texts that say LondonFull and its hashes do not change (FL-A1, FL-A4, FL-A5, FL-A6) are now stale for LondonFull; slice 5 applies them, and the maintainer may want them before stage 2.
  On 2026-11-21, the maintainer approved the LondonFull part of these rows before stage 2.
  FL-A4 and FL-A5 are applied in full, and FL-A1 and FL-A6 have a LondonFull note; slice 5 adds the parts for the preset.

### 8.2 Evidence of stage 2

Slice 4 landed at `ab445aad` on 2026-11-21.
The hub roads of Waterloo and Bank keep their chords at 14 m/s (FL-D5, section 3.7), and the geometry of the preset then curves `940GZZLUWLO-road-in-05` as before.
Without this reset, validation of the preset refuses `940GZZLUWLO-road-out-01` against `london-link-183-ba-1`.
No stop of section 7 fired.
The shape pair count stays at 47, with the same tail warning, so it needs no maintainer decision.
On 2026-11-21, the maintainer chose to give the "tight 3.5" copy of the shared-track study the same reset (`sharedTrackStraightHubs`).
Its network hash is now `a407262a...`, the hash of the preset before the parking, and its audit counts are 0 refusals, 0 under 4.0 m, and 38 shape pairs, as before stage 1.
So the last sentence of section 5.3 and the "tight 3.5" finding of section 8.1 no longer hold.

| Pin of section 5.3 | Before | After |
| --- | --- | --- |
| Network SHA-256 | `5961c702...` | `cba69149...` |
| Network before the parking | `09b8fdea...` | `a407262a...` |
| SX fixture | 262,717 bytes | 298,685 bytes |
| Behavior parity of the preset, topology | `c6a58e80` | `28404d6a` |
| Behavior parity of the preset, ticks | 10,350 and 12,210 | 10,380 and 12,240 |

The gates of slice 6 ran on 2026-11-21 at `5d0e2e98`, which is slice 4 and a change that runs the test tasks of `mise run check` in two parallel lanes.
The runs had no `nice`, in a systemd scope with a memory limit of 32 GiB, with Go 1.27.4.

- `mise run check` passed in 1,535 s.
  It includes `mise run test:shared-express`, `mise run test:london-express`, `qualify`, and the race tasks of each package.
- `mise run test:web` passed in 31 s.
- The London soak row, `PODSIM_SOAK_PRESETS=london-full-shared-express mise run soak`, passed in 691 s.
- `mise run soak:full` with 2 seeds passed for each preset and seed in 6,373 s, in the limit of 7,200 s.

The maintainer skipped the timing seed of each preset for this run on 2026-11-21.
The SX12 timed runs, the SX7 server measurement, and the edit times on the preset did not run, because they need a quiet host.
The Chrome checks do not repeat (FL-D9).

## 9. Decisions for the maintainer

| ID | Question | Options | Decision |
| --- | --- | --- | --- |
| FL-D1 | What "depot fans" means | A: the 22 Parking facility roads that the study converted. B: also the depot `fan` and `throat` lanes, which already have a control point and which the depot rules describe as fan and throat paths (`docs/model.md:383`). | Decided 2026-11-20: A. B is not measured, and it changes depot lanes, which have their own clearance rule. |
| FL-D2 | A road with a smallest radius below 12.5 m | A: the clamp, a curve at 5 m/s. B: no fillet. The road keeps its chord at 14 m/s, as a ramp below the floor drops. C: `sqrt(2 r)` with no floor, down to 0.29 m/s. | Decided 2026-11-20: B. It keeps the approved floor of the ramp design. The radii go down to 0.04 m, so A labels a near-cusp curve 5 m/s, with up to 581 m/s² of lateral acceleration. The three refusals of the preset are such roads. Cost: about 625 roads on LondonFull keep their kinks. C makes the slowest lanes of the network. |
| FL-D3 | Speed of a fillet, given the platoon rule | A: the ramp speed. By the code, platoons cannot form toward about 280 of the 309 passenger stations. B: shape only. Each fillet keeps 14 m/s, as the moves do today. C: the ramp speed, with a separate model proposal that lets a platoon link end before a slower lane. | Decided 2026-11-20: A, gated by FL-D6. If LondonFull fails the gate, bring back B and C with the slice 0 counts. B gives the shape without a speed rule, against the approved ramp speed. C changes the platoon model and is a separate contract. |
| FL-D4 | Does the preset change, and when | A: LondonFull and the preset in one build, with the evidence at the end. B: both presets, in two stages: LondonFull first, then the preset after the maintainer accepts the LondonFull evidence. C: LondonFull only, and the preset builds on a LondonFull without fillets. | Decided 2026-11-20: B. It keeps the scope of the maintainer. The preset reruns SX2 to SX12, which cost the most, only after the LondonFull gate passes. C keeps SX, but the preset then no longer has the stations of LondonFull. |
| FL-D5 | The roads of the two Express hubs, Waterloo and Bank, on the preset | A: fillet them after `apply`. B: keep them straight, and keep the curve of `940GZZLUWLO-road-in-05`. | Decided 2026-11-20: B. The study traced the 3 refusals and the change from 47 to 33 shape pairs to these roads. Their geometry is the Express route of SX2, and the role rule at the link 100 portals is not measured. |
| FL-D6 | Acceptance criterion for LondonFull | A: the ramps criterion of `london-junctions.md:170` to `:176`, with virtual platoons on. At 15 requests per minute, the fillets drain at least as many seeds as the straight roads, and the mean wait and the mean journey are at most 5 percent longer, as a mean of the paired changes. A drain difference of one seed adds three seeds. B: record only, and the maintainer decides from the numbers. | Decided 2026-11-20: A. The maintainer approved this criterion for the last junction change. The study estimates a free-flow cost of +1.5 percent at the median and +4.8 percent at p90, before the loss of platoons. |
| FL-D7 | Station roads and Parking roads in one slice | A: one slice. B: two slices. | Decided 2026-11-20: A. One rule, one option, and one set of pins. B moves each pin of section 5.2 two times. |
| FL-D8 | A `cmd/scenario` flag | A: `-road-fillets` for the London presets, refused for D. B: no flag. The straight arm comes from the base commit. | Decided 2026-11-20: A. The compare study and later studies get both arms at one commit, as `-junctions` did. |
| FL-D9 | The Chrome checks of SX6, SX7, and SX9 | A: the maintainer repeats them on the final build. B: the earlier checks stand. | Decided 2026-11-20: B. The Chrome checks of 2026-11-20 stand. The agent recommended A, because changed motion can change the publication traffic and the work of the consumers, as decision AO-D4 found for D. |
