# London road fillets proposal

Status: PROPOSED, awaiting maintainer approval.
No code, pin, or contract text changes before the maintainer approves the decisions of section 9.
Date: 2026-11-20.
Line references are for `468d1dd6`.
"Study" numbers are from a scratch probe of 2026-11-20 at `151416ae`, and the probe files are deleted.
"Rerun" numbers are from an independent scratch script on the project files that `cmd/scenario` writes at `468d1dd6`, and that script is deleted.
The study converted more lane kinds than this proposal does, so each study number says which lanes it converted.

## Summary

On 2026-11-20, the maintainer chose to fillet only two kinds of lanes on LondonFull and on `london-full-shared-express`:

- The station roads, `<station>-road-in-NN` and `<station>-road-out-NN`.
- The roads of the three Parking facilities, which the study calls the depot fans (section 1.2).

A fillet is a quadratic curve that leaves a road tangent to the lane before it and joins the lane after it at the same heading.
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

The control point `C` is where the ray from `a` along `u` meets the ray into `b` along `v`: `C = a + s u = b - t v`, with `s > 0` and `t > 0`.
The road then leaves `a` at heading `u` and reaches `b` at heading `v`, so both joins have no kink.
A quadratic turns in one direction only, so its total turn is the angle from `u` to `v`.
This is at most the sum of the two kinks that it replaces.

The rule leaves a road as it is in these cases:

- The headings are parallel, so the rays do not meet (study tolerance `|u × v| < 1e-9`).
- The rays meet behind `a` or behind `b` (`s` or `t` at most 1e-6).
  The rerun finds 132 such roads on LondonFull and 128 on the preset.
- One end is already tangent, within 1 degree (15 roads on LondonFull, 14 on the preset).
- The road already has a control point, as `940GZZLUWLO-road-in-05` on the preset (`london_full_shared.go:302` to `:319`).

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
| Validation, the preset | All but the through lanes | 3 refusals "lane ... has an unsupported large-vehicle path shape" (`prepared.go:186`), for `940GZZLUBNK-road-in-06`, `940GZZLUBNK-road-out-05`, and `940GZZLUWLO-road-out-02` |
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
The two Express hubs keep 14 m/s on each lane when their roads keep their chords (decision FL-D5), so links toward the hubs stay possible.
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

### 3.3 Lateral pairs and junction conflict zones

Since the [always-on proposal](shape-clearance-always-on-proposal.md), `newMergeLateral` (`lateral_merges.go:41`) builds the lateral pairs of each network.
A pair is lateral when all of these are true:

- Both lanes end at the node, or both start there (`lateralMergePair`, `:158`).
- Neither lane is a depot or maneuver group lane (`:45` to `:47`).
- The body range of neither lane reaches its far end (clause 5, `lateralMergeExtent`, `:249`, and `reachesFar`, `:273`).

Each other pair gets the isotropic extent of `conflictExtentWithClearance` (`junction_conflicts.go:144`), through `buildJunctionConflicts` (`:33`).
`newLaneCells` (`traffic.go:150`) adds the junction resource to each cell that meets an extent.

The fillet changes these pairs at four places:

- At the diverge node, each road-in of a station now arrives at the heading of `access-in`.
  The road-in lanes form converging pairs with each other, and they now meet at a zero angle.
- At the merge node, each road-out now leaves at the heading of `access-out`, so the road-out lanes form diverging pairs at a zero angle.
- At an arrival portal, the road-in now leaves at the heading of the link, and it forms diverging pairs with the junction moves of that portal at new angles.
- At a departure portal, the road-out and the moves into that portal form converging pairs at new angles.

Near a node where two lanes meet at a zero angle, their bodies stay close for a longer distance.
So a lateral extent can grow, and a pair can fail clause 5 and fall back to the isotropic extent.
Slice 0 counts the lateral pairs, the pairs that clause 5 excludes, and the zone lengths by the method of section 1 of the [lateral merge zones](../design/lateral-merge-zones.md) design, before and after.

### 3.4 Shape pairs and the bank audit

`shapePairs` (`shape_pairs.go:200`) certifies pairs of lanes that share no node.
`auditBankGeometry` (`bank_geometry.go:168`) refuses an uncertified pair closer than 12 m, with the text of `planeGapText` (`:289`).
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
Preparation refuses a lane whose bound is not finite (`prepared.go:184` to `:186`).
On the preset, the three refusals of section 2.4 come from this check.
`LaneTailOverruns` (`profile_geometry.go:103`) gives the tail warnings, and `tail_warnings_test.go:190` requires no warning on LondonFull.

### 3.7 The hubs of the preset

`sharedTrackGeometry.apply` (`london_full_shared.go:241`) moves the diverge, merge, entry, exit, and berth nodes of Waterloo and Bank after LondonFull is built (`:273` to `:300`).
It then curves `940GZZLUWLO-road-in-05` (`:302` to `:319`).
A hub road that LondonFull fillets is therefore not tangent after `apply`.
The preset must fillet its hub roads again after `apply`, or keep them straight (decision FL-D5).

### 3.8 Other readers of the road shape

The London junction design lists the readers of the portal shape (`london-junctions.md:126` to `:130`).
The fillet moves no node, so these readers keep their inputs:

- The collapsed station anchors take the mean start of the approach lanes, which are the road-in lanes (`internal/view/anchor.go:36` to `:59`).
- The label ranks count the approach lanes (`anchor.go:122`).
- The size check counts nodes and lanes.

The ride page rounds each join with a turn of 3 to 150 degrees, for display only (`internal/ride/fillet.go:10` to `:24`).
After the build, a filleted road joins with no turn, so the ride page does not round it.
The ride goldens (`internal/ride/testdata/fillets.json`, `levels.json`) use test networks, not London, so they do not move.

## 4. Build shape

- `LondonOptions` gets a field `RoadFillets` (`london.go:162`).
  Its zero value keeps the roads straight.
- `londonFullOptions` (`london_full.go:87`) sets it.
- `expressBaseOptions` (`london_full_express.go:60`) clears it, so D does not change.
- `DefaultLondonCentralOptions` does not set it, so LondonCentral does not change.
- With `Junctions: LondonRamps`, the field has no effect.
- `londonCapacity.equal` compares the field, so a build with another value is not the cached preset.
- The fillet is one pass over the finished network, by the road IDs that `addLondonStation` writes (`london.go:938`, `:954`).
  The preset runs the pass again on its hub roads after `apply`, or clears their control points, by decision FL-D5.
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
| The ramps study arms | `cmd/scenario/main_test.go:430`, `:434`, and `internal/sim/shared_track_study_test.go:60`, `:70` |
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
| Study inputs | `internal/sim/shared_track_study_test.go:58`, `:59`, and the built networks `:66` to `:69` | Routine. The study is opt-in. |
| Behavior | The `london-full` entry of `preset_behavior_parity.json` (`TestPresetBehaviorParity`, `internal/session/preset_parity_test.go:128`) | Routine |
| No tail warning | `internal/project/tail_warnings_test.go:190` | Must still pass |
| Validation, layout audit, bank audit | `TestLondonFullPreset` (`london_full_test.go:19`), `TestAuditPresetsHaveNoHardConflicts` (`layout_audit_test.go:49`), `TestTailPairCoverage` (`tail_certificate_test.go:512`) | Must still pass |
| Byte limits | `TestRunWritesImportableLondonFull` (`cmd/scenario/main_test.go:682`), `TestScenarioJSONFitsFileLimit` (`:629`) | Must still pass. A fillet adds a control point and a speed limit that is not an integer, under 100 bytes for each lane. |
| Fleet qualification | `mise run qualify:london` (`TestLondonFleetQualification`), which builds from `DefaultLondonFullOptions` | Qualification record |

### 5.3 Pins of the preset that move

| Pin | File and line | Kind |
| --- | --- | --- |
| Network SHA-256 `5961c702...` | `london_full_shared_test.go:265` | Routine |
| Network before the parking `09b8fdea...` | `london_full_shared_test.go:264`, `shared_track_study_test.go:67` | Routine |
| The cached build equals the generated build | `london_full_shared_test.go:248` | Must still pass |
| 47 shape pairs and the tail warning on `940GZZLUBNK-move-08-02` | `london_full_shared_test.go:39` to `:47` | Qualification (SX2). A new count needs the maintainer, because SX2 certifies each shape pair. |
| Layout and parking | `TestLondonFullSharedExpressLayout` (`:208`), `TestLondonFullSharedExpressParking` (`:92`) | Must still pass |
| Routes | `TestLondonFullSharedExpressRoutes` (`:283`) | Qualification (SX2) |
| SX fixture | `internal/sim/testdata/shared_express_project.json.gz`, `TestSharedExpressFixture` (`shared_express_fixture_test.go:38`), with `-update-shared-express` | Routine. Each SX test in `internal/sim` reads it. |
| Behavior | The `london-full-shared-express` entry of `preset_behavior_parity.json` | Routine |
| The tests of `mise run test:shared-express` | `mise.toml:108` | Qualification (SX2 to SX10) |

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
| FL-A3 | `express-shared-track-qualification-proposal.md:5` to `:6`, and the status table `:124` to `:133` | "...so each row is closed and the preset is qualified." | Add after `:6`: "Note of <date>: the fillets proposal changes the geometry of the preset, so the preset is not qualified until the rows run again on the new geometry, with the Chrome checks of SX6, SX7, and SX9 (decision FL-D9)." |
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
| Chrome | SX6, SX7, SX9 | The maintainer, on the final build (decision FL-D9) |

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
| SX6, SX7, SX9 | The Go tests run again. The Chrome checks need the maintainer (FL-D9). |
| SX8 | The incident fixtures and the recovery chain run again. |
| SX10, SX11 | The London soak row, the CI soak, and the release soak run again. |
| SX12 | The timed runs run again. |

The proposal asks the maintainer to decide three things:

- Whether the preset changes at all, and when (FL-D4).
- Whether the hub roads of the preset change (FL-D5).
- Whether the Chrome checks repeat (FL-D9).

If the preset does not change, it stops deriving from the default LondonFull.
It then builds on a LondonFull without fillets, and the test at `london_full_shared_test.go:248` compares it with that build.

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
| 0 | None: a scratch measurement | Scratch tree | Build the rule with the decisions of section 9. On LondonFull and the preset, run validation, the layout audit, the bank audit, the shape pair count, and the tail warnings. Count the lateral pairs, the clause 5 exclusions, and the zone lengths (section 3.3). Count the platoon links of a 60-minute run (section 3.1). Measure the free-flow route times of the study, roads only. Each stop of section 7 goes to the maintainer before slice 1. |
| 1 | `scenarios: Add an option to fillet the London roads` | `internal/scenarios`, `cmd/scenario` | The option of section 4, off for each preset, and the flag of FL-D8. Unit tests: tangency at both ends, the speed of FL-D2 and FL-D3, each case that the rule leaves straight, no effect with ramps, and `londonCapacity.equal`. Each test is checked by mutation. No pin moves. |
| 2 | `scenarios: Fillet the roads of LondonFull` | `internal/scenarios`, tests of `cmd/scenario`, `internal/sim`, `internal/session` | LondonFull sets the option. Under FL-D4 B, the preset builds on LondonFull without fillets. The pins of section 5.2 move. The pins of sections 5.1 and 5.3 do not move. |
| 3 | `docs: Describe the filleted LondonFull roads` | `docs/` | Section 5.4 for LondonFull. `mise run lint` |
| | Evidence of LondonFull | | The compare study of FL-D6, `mise run qualify:london`, and `mise run check` on the tip of slice 3. The maintainer reads the result before slice 4 (FL-D4 B). |
| 4 | `scenarios: Fillet the roads of the shared-track preset` | `internal/scenarios`, `internal/sim/testdata`, tests of `internal/sim`, `internal/session`, `internal/remote` | The preset builds on the filleted LondonFull. The hub roads follow FL-D5. The pins of section 5.3 move, except the shape pair count, which needs the maintainer. |
| 5 | `contracts: Apply the fillet amendments` | `docs/contracts/`, `docs/design/` | Rows FL-A1 to FL-A7, after maintainer approval. `mise run lint` |
| 6 | `contracts: Record the evidence of the filleted preset` | `docs/contracts/` | The gates of section 5.7 in this order: `mise run test:shared-express` and `mise run test:london-express`, then `mise run check`, then the London soak row, then the SX12 timed runs and the SX7 server measurement on a quiet host, then one timing seed per preset, then `mise run soak:full`. The records go in the SX proposal and in this proposal. The Chrome checks follow (FL-D9). |

Run the race task of each changed package once on the tip of slice 4: `mise run test:race:sim`, `test:race:session`, and `test:race:other`.
Under FL-D4 A, slices 2 and 4 are one commit, and the LondonFull evidence runs with the evidence of slice 6.
Under FL-D4 C, slices 4 and 6 do not happen, and slice 5 has FL-A4, FL-A5, FL-A7, and an SX1 note that the preset keeps its geometry.

## 9. Decisions for the maintainer

| ID | Question | Options | Recommendation |
| --- | --- | --- | --- |
| FL-D1 | What "depot fans" means | A: the 22 Parking facility roads that the study converted. B: also the depot `fan` and `throat` lanes, which already have a control point and which the depot rules describe as fan and throat paths (`docs/model.md:383`). | A. B is not measured, and it changes depot lanes, which have their own clearance rule. |
| FL-D2 | A road with a smallest radius below 12.5 m | A: the clamp, a curve at 5 m/s. B: no fillet. The road keeps its chord at 14 m/s, as a ramp below the floor drops. C: `sqrt(2 r)` with no floor, down to 0.29 m/s. | B. It keeps the approved floor of the ramp design. The radii go down to 0.04 m, so A labels a near-cusp curve 5 m/s, with up to 581 m/s² of lateral acceleration. The three refusals of the preset are such roads. Cost: about 625 roads on LondonFull keep their kinks. C makes the slowest lanes of the network. |
| FL-D3 | Speed of a fillet, given the platoon rule | A: the ramp speed. By the code, platoons cannot form toward about 280 of the 309 passenger stations. B: shape only. Each fillet keeps 14 m/s, as the moves do today. C: the ramp speed, with a separate model proposal that lets a platoon link end before a slower lane. | A, gated by FL-D6. If LondonFull fails the gate, bring back B and C with the slice 0 counts. B gives the shape without a speed rule, against the approved ramp speed. C changes the platoon model and is a separate contract. |
| FL-D4 | Does the preset change, and when | A: LondonFull and the preset in one build, with the evidence at the end. B: both presets, in two stages: LondonFull first, then the preset after the maintainer accepts the LondonFull evidence. C: LondonFull only, and the preset builds on a LondonFull without fillets. | B. It keeps the scope of the maintainer. The preset reruns SX2 to SX12 and the Chrome checks, which cost the most, only after the LondonFull gate passes. C keeps SX, but the preset then no longer has the stations of LondonFull. |
| FL-D5 | The roads of the two Express hubs, Waterloo and Bank, on the preset | A: fillet them after `apply`. B: keep them straight, and keep the curve of `940GZZLUWLO-road-in-05`. | B. The study traced the 3 refusals and the change from 47 to 33 shape pairs to these roads. Their geometry is the Express route of SX2, and the role rule at the link 100 portals is not measured. |
| FL-D6 | Acceptance criterion for LondonFull | A: the ramps criterion of `london-junctions.md:170` to `:176`, with virtual platoons on. At 15 requests per minute, the fillets drain at least as many seeds as the straight roads, and the mean wait and the mean journey are at most 5 percent longer, as a mean of the paired changes. A drain difference of one seed adds three seeds. B: record only, and the maintainer decides from the numbers. | A. The maintainer approved this criterion for the last junction change. The study estimates a free-flow cost of +1.5 percent at the median and +4.8 percent at p90, before the loss of platoons. |
| FL-D7 | Station roads and Parking roads in one slice | A: one slice. B: two slices. | A. One rule, one option, and one set of pins. B moves each pin of section 5.2 two times. |
| FL-D8 | A `cmd/scenario` flag | A: `-road-fillets` for the London presets, refused for D. B: no flag. The straight arm comes from the base commit. | A. The compare study and later studies get both arms at one commit, as `-junctions` did. |
| FL-D9 | The Chrome checks of SX6, SX7, and SX9 | A: the maintainer repeats them on the final build. B: the earlier checks stand. | A. Changed motion can change the publication traffic and the work of the consumers, as decision AO-D4 found for D. |
