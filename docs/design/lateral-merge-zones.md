# Lateral merge zones

Status: the maintainer approved this design on 2026-11-15.
The maintainer chose these options: the gated field (D4, option B), merges and splits (D1), a margin of 1 m (D2), the debris partners (D5), and the contract amendments (D7).
D3 and D6 follow the recommendations.
The rule was built behind the `lateralMerges` network field.
On 2026-11-15, the maintainer adopted it for LondonCentral and LondonFull, which set the field.
`london-full-express` did not set it, so its behavior did not change then.
On 2026-11-20, the field was removed, and part 3 of the [shape clearance](shape-clearance.md) design applies this rule to each network, D included.
The [junction study](../studies/london-junctions.md#lateral-merge-zones) has the results with the field set.

Lane ends, found by a review on 2026-11-15: the safety argument of section 3 failed where the body range of a lane reached the far end of the lane.
The extent of a diverging lane stopped at the lane end, and the pod released the junction there (`internal/sim/traffic.go:950`).
On the next lane, which has no resource of the node, the body of the pod could still meet the body of a pod on the partner lane.
For example, lane A is 24 m long, and lane B leaves the same node at 3 degrees: at the end of A, the two lanes are 1.26 m apart.
A converging lane whose body range reaches its start had the same problem on the lane before it.
On 2026-11-15, the maintainer chose to exclude these pairs, as clause 5 of section 2 now says, and the simulator does so.
In LondonFull with lateral merges, 9,362 of 9,944 pair sides with ramps and 22,966 of 23,972 with chords stay lateral.
The far-end test sweeps the body over the last part of the lane, at most 0.5 m long and ending at the far end, and over each join turn there, as a continuous enclosure.
So the pose at which a pod releases the junction is clear of the partner lane, and each contact before that pose is inside the extent.
A second review asked for a test of each pose in the last 0.5 m, which clause 5 does not need, because the extent holds those poses.
The test encloses each join turn at the far end in one rectangle on the axes of the middle heading of the turn.
This rectangle holds more than the body over the turn, so the test can exclude a pair that a test of each angle of the turn keeps lateral.
On LondonFull, 23 pairs are excluded only because of the rectangle, and a test of each angle would keep about 20 of them lateral.
On 2026-11-15, the maintainer chose to keep the rectangle, and the body reference of the tests uses the same rectangle.

The isotropic rule has the same gap, and the exclusion does not remove it.
The isotropic extent of a lane also ends at the lane end, so on the next lane a body can meet a body on the partner lane, with the field set or not.
A scratch run of this geometry on 2026-11-15 gave the same overlap with the field set and unset.
The committed tests do not repeat this run.
The [lane-end protection](lane-end-protection.md) design measured this gap on 2026-11-15.
For each pair that clause 5 excludes, the two lanes on London have different separation groups and no shared node, so the model treats them as separate levels.
The other lanes that join at a far end share a node with the partner lane, are in one maneuver group, are depot lanes, or are at least 12 m away.
London and Express run the all-pairs bank audit, because they have maneuver groups.
The gap stays open only in a project without banks or maneuver groups, where `Check` still reports a close pair.
The maintainer chose to keep this record and to add no check.

This design shortens the junction conflict zones of shallow merges and splits.
It follows the [London junction study](../studies/london-junctions.md#where-pods-wait), which found that pods on ramps wait 1,535 s before join nodes on seed 1, against 68 s with chords.
The study names the zone length as a possible cause and did not check it.
Code references are for `210e31e6`.

## 1. Current behavior

`buildJunctionConflicts` (`internal/sim/junction_conflicts.go:29`) compares each ordered pair of lanes at each node (`forEachJunctionPair`, `:104`).
The pairs at a node are converging (both lanes end there), diverging (both start there), and through (one ends, one starts).
All three kinds use one rule, and loops and lanes with shared endpoints use it too.
`conflictExtentWithClearance` (`:128`) samples the lane every 0.5 m and keeps the range of samples within the clearance of the whole polyline of the other lane, padded by 0.5 m at each end (`:165`).
The clearance is 12 m, or 20 m when either lane admits a large class (`classSetPairClearance`, `internal/sim/profile_geometry.go:27`).
The distance is isotropic, so two straight lanes that meet at an angle `a` conflict for about 12 m / sin(a).

`newLaneCells` (`internal/sim/traffic.go:181`) adds the junction resource of the node to each cell, up to 30 m long, that meets an extent.
`reservationEnd` (`traffic.go:763`) extends a request through each continuous run of cells with that junction, and `grant` (`traffic.go:694`) takes all of them or none.
A pod releases the junction at the end of each cell (`releaseDistance`, `traffic.go:950`).
Independent of the junction, the last cell of a lane holds its To node (`traffic.go:172`), and a pod keeps the From node until 12 m past the lane start (`traffic.go:955`).

A scratch measurement on `210e31e6` gives the union zone on each small lane that ends at a node with another small incoming lane:

| Network | Lanes | Zone p50 | Zone p75 | Zone p90 | Held cells p75 | Held cells p90 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| LondonFull, chords | 5,135 | 14.1 m | 22.5 m | 47.4 m | 31.9 m | 58.8 m |
| LondonFull, ramps | 4,631 | 13.8 m | 32.9 m | 48.9 m | 40.0 m | 56.5 m |

The count includes station, parking, and depot lanes, and it was not split by lane kind.
Thus it shows that chords also have long zones, but not where they are.

## 2. Proposed rule

A pair is a lateral pair when all of these are true:

1. Both lanes end at the node (converging), or both start at it (diverging).
2. Neither lane is a loop, and the two lanes do not share both endpoints (`sharedEndpoints`, `junction_conflicts.go:72`).
3. Both lanes are small: no large class and no geometry tail, as for the depot `small` member (`internal/sim/depot_lateral.go:70`).
   This clause does not apply (see the amendment of section 2).
4. Neither lane is in a maneuver group or a depot.
5. On neither lane does the body range reach the far end of the lane: the start for a converging pair, or the end for a diverging pair.
   A join step at the far end counts as part of the lane.
   Where the body range reaches the far end, the body of a pod can still meet the other body after the pod leaves the lane and releases the junction.
   Such a pair keeps the isotropic rule.

Through pairs, crossings, and every other pair keep the isotropic rule.
Pairs with a large class keep 20 m, as the Express contract requires (`docs/contracts/express-physical-encoding-contract-proposal.md:206`).
The [shape clearance](shape-clearance.md) design applies this rule to each class.
Clause 3 does not apply, so a lane with a large class or a geometry tail can form a pair.
Clauses 1, 2, 4, and 5 stay.
The body of each lane is the union of the bodies of the classes that the lane admits, and a pair keeps the side-by-side distance of the classes of its two pods.
The node range stays 12 m, because the union body changes only the body range.
Amended 2026-11-16, by maintainer decision: the rule for each class, behind the `shapeClearance` network field.
Amended 2026-11-20, by maintainer decision: the shape clearance rule applies to each network, and the network fields `lateralMerges` and `shapeClearance` are removed.

The extent of lane A against lane B in a lateral pair is the union of two ranges:

- The body range: each reference position on A where the grown body at that position, at the heading of A, meets the grown body at some position of B, at the heading of B.
  The body is the small sweep body of the depot rule: 2.5 m behind the reference, 3.7 m ahead, 1.05 m to each side, which includes 0.25 m of tracking (`depot_lateral.go:14`, `internal/sim/maneuver_certify.go:17`).
  Each side grows by half of the margin, 0.5 m.
- The node range: the last 12 m of A for a converging pair, or the first 12 m for a diverging pair.

The range gets the same 0.5 m sample pad as today.
The center distance that two disjoint grown bodies keep is `2 × 1.05 + 1 = 3.1 m`, which is `lateralClearance(0)` (`depot_lateral.go:34`).
The union body and the side-by-side distance of the two classes replace this body and this distance.

The depot formula `W(v) = 3.1 + v²/2` does not carry over.
It is 15.6 m at 5 m/s and 101 m at 14 m/s, so a merge zone would become longer than today.
The depot sweep inherits the layer A margin `1 + 2u` (`depot_lateral.go:16`), and a depot pair has no resource that orders its pods.
In a lateral pair, the junction resource orders the pods, so the rule needs no stop term (section 3).

The same scratch measurement, with a point-distance proxy `W` for the body range, gives the table below.
The proxy did not apply clauses 3 and 4, so it overstates the changed zones.

| Network | `W` | Lane zones with other cells | Zone p75 | Zone p90 | Held cells p75 | Held cells p90 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Ramps | 3.1 m | 1,515 | 13.4 m | 26.4 m | 28.3 m | 40.0 m |
| Ramps | 4.6 m | 870 | 16.2 m | 36.7 m | 29.0 m | 50.6 m |
| Ramps | 8.7 m | 386 | 26.5 m | 43.6 m | 29.9 m | 55.1 m |
| Chords | 4.6 m | 1,756 | 13.2 m | 35.1 m | 27.9 m | 43.2 m |

A point proxy ignores the body length, so the body range is between the 3.1 m and the 4.6 m rows on a shallow merge.
The 8.7 m row is the disk bound of the body without the growth, `2 × hypot(3.7, 1.05) + 1`.
The grown bounds of the [shape clearance design](shape-clearance.md) are larger: 8.96 m for two small pods and 9.31 m for the union body.
The held length at p50 stays 25 m in every row, because the last cell holds the junction in both rules.
Thus the gain is in the cells upstream of the last cell.

## 3. Safety argument

Take a lateral pair A and B at node N, and pods p on A and q on B.

1. A pod whose reference is in its extent owns junction N.
   The cells that meet the extent hold N (`traffic.go:181`), a pod stops by the end of its last owned cell (tick-rate contract, section 1.1), and it keeps N to the end of the cell (`traffic.go:950`).
2. At most one route owns N.
   A linked platoon follower can share the junction of a pod ahead (`traffic.go:697`), but the members of a run are on one route, so they are not on A and B together.
3. Thus p and q are not both in their extents.
   Say q is outside its extent.
   Then the grown body of q clears every body position on A, by the definition of the body range.
   So the grown bodies of p and q are disjoint, and their references are at least 3.1 m apart.
4. The steps cover only lanes that share the node.
   Lanes that share no node keep their rule: a separation group, a depot or bank audit, or no check.
   A network with banks or maneuver groups runs the bank audit on all pairs, and other networks have no check (see the [lane-end protection](lane-end-protection.md) design).
5. No step uses time or speed, so the argument holds at each tick rate, as section 1.2 of the tick-rate contract requires.
6. At the node and after it, the node resource and the follow model give the old spacing.
   The node range of each lane and the To node of the last cell keep one pod at the node.
   The through pairs keep their 12 m extents on the outgoing lane, and the track cells keep 12 m along the path (tick-rate contract, section 1.2).

The argument assumes a guideway-bound pod: its reference stays within 0.25 m of its lane, and a fault stops it on its lane.
Today that assumption applies only below `reducedClearanceSpeed`, about 3.74 m/s, and `docs/station-banks.md` calls it an assumption of the model, not a derived result.
This rule extends it to line speed (decision D2).

These places assume the isotropic 12 m between pods of a junction pair:

| Place | Effect |
| --- | --- |
| `SafetyObservation.Check`, `pair` (`internal/sim/safety.go:258`) | Breaks. Every small pair that is not a window, maneuver, or depot pair needs 12 m. It needs a lateral-pair table, as `index.lateral` at `:279`. `maneuverSafety` sets no keys without maneuver or depot pairs (`internal/sim/maneuver_safety.go:10`), so it must also set them for lateral pairs. |
| `checkTraffic` (`internal/sim/traffic_test.go:302`) | Breaks on a shallow merge. It checks 12 m for all pods. `TestAcuteJunctionLanesPreservePhysicalClearance` (`junction_conflicts_test.go:99`) uses it. |
| Service contract, clearance layer A (`docs/contracts/service-contract-proposals.md:47`) | Breaks: "Every other pair of small pods keeps 12 meters." Needs an amendment. |
| Debris footprint (`docs/contracts/incident-suspension-contract-proposal.md:505`) and "Debris clearance stays 12 meters" (`service-contract-proposals.md:48`) | Breaks. A lateral neighbor is only a depot lane, so a pod on B can pass debris on A at 3.1 m (decision D5). |
| Tick-rate contract, sections 1.2 and 1.5 | Holds. Section 1.2 is a path-gap argument. Section 1.5 lists the depot lateral pairs. A later amendment, with maintainer approval, can add a line for lateral merge pairs. The smallest gap of 6.000 m in section 1.7 is evidence, not a rule. |
| Tick-rate contract, section 1.4 | Holds. Link clearance is along one route. |
| Express contract, 20 m and actual paths (`express-physical-encoding-contract-proposal.md:206`, `:217`) | Holds without `shapeClearance`, because clause 3 excludes large pairs. With the field, amendment SC4 of the [shape clearance amendments](../contracts/shape-clearance-amendment-proposal.md) changes the 20 m rule. |
| Bank audit (`internal/sim/bank_geometry.go:31`) | Holds. It leaves shared endpoints to junction control, which still orders them. |
| Physical restore (`internal/sim/state_physical.go:1061`, `:1372`) | Follows `Check`. Without the lateral table, a gate-on save with a lateral pair nearer than 12 m demotes a pod or fails `verifyRestore`. |
| Layout audits (`internal/scenarios/layout_audit.go:203`, `:372`) | Not affected. They check lanes that share no node. |
| `docs/model.md:37` and `:211` | Text changes. |

## 4. Express and compatibility

The scratch measurement on `london-full-express` with the 3.1 m proxy changes the cells of 1,940 lane zones (972 converging, 968 diverging) and leaves its 1,030 large pairs alone.
Thus a small-pairs-only rule does not keep Express behavior, because Express starts from LondonFull with chords.
The Express qualification freezes the D9 geometry (`docs/contracts/express-london-qualification-proposal.md:24`).
No clause requires byte-identical runs.
But the qualification evidence (the soak row, the checkpoints, the oracle runs) is for the current behavior, and the standee contract lists seat-only parity gates.

| Option | Express behavior | Pros | Cons |
| --- | --- | --- | --- |
| A. Small pairs only, no gate | Changes | One rule. No format change. | Express needs requalification, and the qualification evidence goes stale. |
| B. Gate by a network field, `lateralMerges`, with `omitzero` and `digest:"ext=16"` | Same | Express, digests, and goldens stay. Gate off and gate on run in one build, so the study needs no scratch build. | Two rules in the simulator. A format change: `docs/reference.md`, project validation, the editor round trip, and the digest registry. |
| C. Amend the Express contract and requalify | Changes | One rule. | Cost of the soak and the checkpoint rows again, before the rule has study evidence. |

Note of 2026-11-20: the field is removed ([always-on proposal](../contracts/shape-clearance-always-on-proposal.md)), so option B no longer applies.

Numbers 1, 2, and 10 are retired, and 16 is the next free extension number (`internal/session/digest_extension_test.go:438`).
A network field with a safe zero value keeps `stateVersion` 12 (`internal/session/state_file.go:23`).
The stream frames refer to the network by index (`internal/session/stream_frame.go:65`), and the project that carries the field reaches clients as it does today, so `StreamVersion` stays 10.
The HTTP state and the saved state carry the field only when it is set.

Decision: option B for the study.
On 2026-11-15, the maintainer adopted the rule: LondonCentral and LondonFull set the field, and Express does not.
No migration is needed before v1.

## 5. Effects

| Item | With the gate off | With the gate on |
| --- | --- | --- |
| Junction tables, all networks | Same | Lateral pairs shorter |
| `TestJunctionConflictsMatchReference`, `TestScenarioJunctionConflictsMatchReference` | Pass | The reference uses `Clearance` only, so the scenario test checks each lateral merge pair against a brute-force body reference, and each network also runs with the field cleared. The London entry of `testdata/scenario_networks.json.gz` is the current LondonCentral preset, with the field set. |
| `TestMergeCorridorHeadway`, `TestMergeCorridorPlatoonHeadway` (`internal/sim/merge_corridor_test.go:234`, `:264`) | Same | The 15 degree pins (10.800 s, and 6.017 s and 3.523 s with platoons) are expected to move toward the 90 degree values (8.633 s). The 30 degree cases may not move, because of cell rounding. New gated cases get new pins. |
| `TestPresetOutputIsPinned`, London project hashes (`internal/scenarios/london_full_test.go:187`) | Same | LondonCentral and LondonFull set the field since 88794af4, so their hashes changed. Express stays. |
| `command_digests.txt` | Same | Same, because no command in the file uses a London preset |
| `state_v12.json`, `stream_*` goldens | Same | Same |
| Byte proofs (`TestPlainStateFileWorstCaseSize` and the others in AGENTS.md) | Run again: the worst-case project gets one more member | Same as off |
| `TestAcuteJunctionLanesPreservePhysicalClearance`, other `checkTraffic` callers | Same | Move to `Check` with the lateral table |
| Start time (`MaxJunctionPairs`, `internal/project/config.go:46`) | Same | The body sweep costs more for each lateral pair; measure `NewFleet` on LondonFull |

## 6. Decisions

| ID | Decision | Options | Recommendation |
| --- | --- | --- | --- |
| D1 | Pair scope | Converging only; converging and diverging | Both. The argument is symmetric, and the scratch counts are equal (949 and 953 on chords). |
| D2 | Margin and speed | 1 m, `W` 3.1 m; a fixed 2 m, `W` 4.1 m; a speed term | 1 m, with the guideway assumption stated for line speed. A speed term makes zones longer than today. |
| D3 | Extent method | Body sweep; point distance at the 8.7 m disk bound | Body sweep. The disk bound keeps 386 of about 1,500 changed ramp zones. |
| D4 | Express | A, B, or C of section 4 | B now, then choose at adoption. |
| D5 | Debris | Add lateral partners to the footprint `F`; amend the 12 m debris clause | Add partners, so the debris contract keeps 12 m and fault-free runs do not change. |
| D6 | `Check` test for lateral pairs | Center distance 3.1 m, as for depot pairs; an oriented body test | Center distance in `Check`, and an oriented body oracle in the tests. |
| D7 | Contract text | Amend the service contract clause; a new contract document | Amend `service-contract-proposals.md:47` and section 7.2 of the incident contract, with maintainer approval. |

## 7. Test plan

Unit tests:

- Straight lateral pairs at 3, 6, 10, 15, 21, 30, 60, and 90 degrees, converging and diverging: the extent against a closed form, and never shorter than the node range.
- Quadratic ramps from the ramps generator, against the brute-force body reference.
- Through pairs, loops, shared endpoints, large pairs, lanes with tails, maneuver and depot lanes: the old extent.
  Large pairs and lanes with tails can form a pair.
- Gate off: each table in `scenario_networks.json.gz` equals the current reference.
- `Check`: a lateral pair at 3.1 m passes, at 3.09 m fails; a through pair at 3.1 m fails.
- Two streams into a 6 degree merge for 15 simulated minutes, with `Check` and the oriented body oracle at each tick.
- Debris on A beside B blocks the cells of B within 12 m.

Mutations, with the table of the `verify-by-mutation` skill:

| Mutation | Test that fails |
| --- | --- |
| Drop the body front from the sweep | Body oracle on the 6 degree merge |
| Apply the rule to a large pair | Large-pair extent test |
| Apply the rule to a through pair | Through-pair extent test, `Check` through-pair case |
| Drop the node range | Node-range extent test |
| `Check` uses 3.1 m for every small pair | `Check` through-pair case |
| Ignore the gate | Gate-off reference test |
| Omit the debris partners | Debris test |

## 8. Study plan

The method is that of the [London junction study](../studies/london-junctions.md#method): the AM peak of LondonFull, 60 minutes of arrivals, at most 60 more minutes, sharing and redistribution off, free-flow routing, and virtual platoons.
`cmd/scenario` got a `-lateral-merges` flag that set the field.
After the adoption, the London presets set the field, and `-lateral-merges=false` cleared it.
The flag was removed on 2026-11-20, with the field.

The study records means over seeds and paired changes, not the values of each seed, so the off arms run again.

1. Timed pilot: chords and ramps with the gate off, seed 1 at 15 requests per minute, with the stopped-pod sample of "Where pods wait".
   The junction waits must equal the recorded 1,221 and 2,986 pod-seconds.
   Record the time and the memory (the study pilot took 32 s and 795 MB).
2. Batch 1: chords and ramps, each with the gate off and on, 15 requests per minute, seeds 1 to 3.
3. Batch 2: the same arms, seeds 4 to 6.
4. Batch 3: the same arms, 20 requests per minute, seeds 1 to 3.
5. Seed 1 at 15 requests per minute, ramps on: the stopped-pod sample of "Where pods wait".
6. One 30-minute LondonFull ramps-on run with `Check` and the oriented body oracle at each tick, as section 1.7 of the tick-rate contract, with the smallest lateral-pair gap.

Each batch has 4 arms and 3 seeds, inside the cap of 6 arms and 3 seeds.
A converging-only arm comes back as a proposal if D1 stays open.
The criteria stay those of the study: drained seeds, and the 5% criteria on mean wait and mean journey, against chords with the gate off.
