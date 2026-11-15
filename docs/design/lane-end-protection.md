# Lane-end protection

Status: proposed.
On 2026-11-15, the maintainer chose C2 for L2, kept the separation-group exemption for L1, and chose the pass right after the existing audit for L6.
L4 and L5 are open.
Code references are for `959912c7`.
The counts come from a scratch test at `959912c7`, which did not land.
A number in a table with the "Measured" heading, or a statement marked "measured", comes from that test.
A statement marked "inferred" comes from the code, and no test ran it.

## 1. Problem

The [lateral merge zones](lateral-merge-zones.md) design found a gap at lane ends.
Lanes A and B form a junction pair at node N.
The 12 m range of A against B can reach the far end of A, which is the end away from N.
On the adjoining lane C, which joins A at that far end, a pod holds no resource of N.
So a pod on C can come closer than 12 m to a pod on B.
For a diverging pair, the pod releases N at the end of its last cell on A.
For a converging pair, C comes before A, and the pod on C does not hold N yet.

The trigger is the 12 m range, also for a lateral merge pair.
C and B are not a lateral merge pair, so `Check` asks for 12 m between their pods.
Clause 5 of the lateral merge design tests only the body range on A, so it does not close this gap.
The isotropic extent also stops at the lane end, so the gap is the same with `lateralMerges` set or unset.
The design says that on London the two lanes are in different separation groups.
This proposal measures that claim and gives the options to close the gap.

## 2. Current behavior

- `forEachJunctionPair` (`internal/sim/junction_conflicts.go:120`) compares only lanes that share a node.
  The comment at `:28` says that unconnected crossings stay outside junction control.
- `conflictExtentWithClearance` (`junction_conflicts.go:144`) clips the extent to the lane, with a 0.5 m pad (`:181`).
- `newLaneCells` (`internal/sim/traffic.go:181`) adds junction N only to cells of lanes at N.
- `releaseDistance` (`traffic.go:949`) releases a junction at the cell end (`:950`).
- `reservationEnd` (`traffic.go:763`) takes the first cell of the next lane (`:776`).
  That cell holds N only when its lane is also at N.
- `newMergeLateral` (`internal/sim/lateral_merges.go:69`) drops each pair that fails clause 5.
- `SafetyObservation.Check` skips two lanes in different nonempty separation groups with no shared node (`internal/sim/safety.go:332`).
- The bank audit (`internal/sim/bank_geometry.go:82`) refuses two paths with no shared node that are closer than their clearance.
  `prepareNetworkTails` runs it before the lane length checks (`internal/sim/prepared.go:169`).
  It returns early without banks, large classes, maneuver groups, or depots (`bank_geometry.go:85`).
  Without banks or maneuver groups, it checks only pairs with a large path or a depot path (`:146`).
  It skips pairs in different separation groups (`:157`).
- `auditPlanarLayout` (`internal/scenarios/layout_audit.go:203`) checks 12 m between lanes with no shared node.
  It runs for a changed ring preset (`internal/scenarios/scenarios.go:235`) and in a test of the ring presets (`layout_audit_test.go:66`).
  Project validation does not run it.

Thus the gap is one case of a wider rule.
Two lanes that share no node get no junction resource.
Only separation groups and the audits keep their pods apart.

## 3. Measurement

The scratch test built each preset with `cmd/scenario` at `959912c7`.
For each pair side, it took the extent that `buildJunctionConflicts` builds.
For a lateral side, it also took the isotropic 12 m range.
A side reaches the far end when either range touches the far end.
For each such side and each adjoining lane C, the test measured three values:

- Isotropic reach: the distance from the boundary to the far end of the range of C within 12 m of B.
- Body reach: the same distance for the lateral body sweep of C against B.
- Class: the rule that keeps the pods on C and B apart today.

| Class | Meaning | Code |
| --- | --- | --- |
| Shared node | C and B share a node M, so the pair at M orders their pods. | `junction_conflicts.go:120` |
| Maneuver group | C and B are in one maneuver group. Each cell holds the group junction. | `maneuver_groups.go:114`, `traffic.go:215`, `traffic.go:697` |
| Depot | C or B is a depot lane. The bank audit checks the pair. | `bank_geometry.go:146` |
| Groups | C and B are in different nonempty groups and share no node. `Check` and the audit skip the pair. | `safety.go:332`, `bank_geometry.go:157` |
| Same plane | Every other case. | None |

| Measured | LondonCentral chords | LondonCentral ramps | LondonFull chords | LondonFull ramps | `london-full-express` |
| --- | ---: | ---: | ---: | ---: | ---: |
| Junction pair sides | 21,132 | 13,612 | 49,440 | 33,520 | 50,284 |
| Sides that reach the far end | 1,737 | 1,729 | 2,477 | 2,283 | 2,498 |
| Converging, diverging, through | 566, 552, 619 | 575, 557, 597 | 896, 863, 718 | 821, 788, 674 | 897, 881, 720 |
| Of these, lateral sides | 261 | 470 | 493 | 726 | not set |
| Sides that clause 5 excludes | 534 | 318 | 1,006 | 582 | not set |
| Of these, body range reaches on its own side | 346 | 172 | 647 | 321 | 649 |
| Cases (side and adjoining lane) | 2,598 | 3,123 | 3,461 | 4,106 | 3,482 |
| Adjoining lanes | 484 | 1,174 | 648 | 1,741 | 654 |
| Shared node | 840 | 840 | 960 | 960 | 960 |
| Maneuver group | 980 | 980 | 1,120 | 1,120 | 1,120 |
| Depot | 90 | 90 | 90 | 90 | 90 |
| Groups | 639 | 1,164 | 1,235 | 1,880 | 1,256 |
| Groups closer than 12 m | 603 | 1,150 | 1,171 | 1,852 | 1,192 |
| Same plane | 49 | 49 | 56 | 56 | 56 |
| Same plane closer than 12 m | 0 | 0 | 0 | 0 | 0 |
| Isotropic reach, p50, p95, max (m) | 3.7, 15.0, 42.5 | 7.5, 81.6, 337.4 | 6.0, 14.5, 62.6 | 9.6, 102.1, 337.4 | 6.0, 15.0, 101.0 |
| Option A entries with closure | 5,751 | 6,488 | 7,037 | 8,101 | 7,057 |
| Of these, same plane | 5,115 | 5,115 | 5,820 | 5,820 | 5,820 |

The reach row counts the cases without a shared node.
`london-full-express` does not set `lateralMerges`.
Its clause 5 row counts the candidate sides whose own body range reaches the far end.
Each same-plane case is at least 12.01 m from B, inside the 0.5 m sample pad.
Of the lateral sides, 2 on LondonCentral chords and 3 on LondonFull chords reach the far end with the built extent, by the pad.
The other lateral sides reach it only with the 12 m range.

The small, busy, rail-hub, rail-hub-incidents, parking-constrained, scale100, and independent-banks presets have no side that reaches the far end.
The compact-station preset has 144 such sides.
Each of its cases is a shared node, a maneuver group, or at least 12.01 m away.

The design claim is about the excluded pairs.
The next table counts the sides of these pairs that reach the far end, and the lateral sides.

| Measured | Subset | Sides, cases | Groups | Isotropic reach p50, p95, max (m) | Body reach p50, p95, max (m) |
| --- | --- | ---: | ---: | ---: | ---: |
| LondonCentral chords | Excluded, converging | 189, 189 | 189 | 12.1, 18.8, 36.0 | 6.8, 11.2, 31.2 |
| LondonCentral chords | Excluded, diverging | 181, 181 | 181 | 12.5, 18.0, 26.0 | 6.1, 9.4, 14.1 |
| LondonCentral chords | Lateral, converging | 130, 130 | 123 | 5.1, 24.1, 36.0 | 0.0, 15.3, 31.2 |
| LondonCentral chords | Lateral, diverging | 131, 131 | 124 | 5.0, 23.0, 27.0 | 0.0, 15.2, 18.0 |
| LondonCentral ramps | Excluded, converging | 87, 143 | 143 | 26.7, 110.3, 201.5 | 8.1, 92.6, 142.9 |
| LondonCentral ramps | Excluded, diverging | 88, 143 | 143 | 26.5, 103.4, 171.3 | 11.1, 81.9, 153.3 |
| LondonCentral ramps | Lateral, converging | 241, 452 | 445 | 28.2, 102.1, 337.4 | 0.0, 31.8, 246.5 |
| LondonCentral ramps | Lateral, diverging | 229, 440 | 433 | 28.0, 118.0, 302.1 | 0.0, 22.9, 252.3 |
| LondonFull chords | Excluded, converging | 372, 372 | 372 | 11.6, 15.0, 26.0 | 4.3, 9.2, 14.3 |
| LondonFull chords | Excluded, diverging | 340, 340 | 340 | 12.0, 16.0, 36.0 | 4.0, 8.2, 31.2 |
| LondonFull chords | Lateral, converging | 243, 243 | 235 | 5.3, 23.1, 36.0 | 0.0, 16.4, 31.3 |
| LondonFull chords | Lateral, diverging | 250, 250 | 242 | 5.0, 13.0, 36.0 | 0.0, 0.0, 31.2 |
| LondonFull ramps | Excluded, converging | 172, 275 | 275 | 20.2, 133.6, 201.5 | 6.9, 92.6, 153.6 |
| LondonFull ramps | Excluded, diverging | 157, 252 | 252 | 24.5, 119.9, 225.7 | 10.0, 88.9, 171.5 |
| LondonFull ramps | Lateral, converging | 368, 685 | 677 | 29.9, 136.9, 337.4 | 0.0, 40.0, 246.5 |
| LondonFull ramps | Lateral, diverging | 358, 682 | 674 | 27.6, 134.1, 302.1 | 0.0, 28.7, 256.1 |

Each lateral case that is not in the groups class is a same-plane case at least 12.01 m away.
Thus the claim holds for every excluded pair on the four London networks.
It does not hold for the other sides that reach the far end.
Those cases share a node, are in one maneuver group, involve a depot lane, are in different groups, or are at least 12.01 m apart.

The groups cases have no derived distance.
On LondonFull chords, the median polyline distance from C to B is 5.21 m, and 97 of 1,235 touch or cross B in plan.
On LondonFull ramps, the median is 5.66 m, and 306 of 1,880 touch or cross B in plan.
These are the stacked ramps and the link lanes that the London generator puts in their own groups ([London junctions](london-junctions.md#generator-changes)).
`Check` does not compare these pods.
So the oracle runs of section 1.7 of the tick-rate contract give no evidence for this class.

The scratch test also ran the bank audit with all pairs.
It refused no preset and none of the 9 networks of `internal/sim/testdata/scenario_networks.json.gz`.
It refused these unit-test fixtures:

- `mergeCorridor` (`internal/sim/merge_corridor_test.go:34`), for each side angle.
  Its destination berth fan has 166 same-plane cases closer than 12 m.
  A berth pitch of 30, 35, 40, or 50 m does not fix it.
- `laneEndNetwork` (`internal/sim/lateral_merges_test.go:1199`), without its separation groups.
  This is the example of the lateral merge design: lane C is 1.26 m from B.

`lateralPairNetwork` and `rampNetwork` pass.
The unit tests use other fixtures, and the scratch test did not check them.

## 4. Options

### A. Carry the extent onto the adjoining lanes

Each adjoining lane C gets an extent entry for node N, against B.
B also gets an extent entry for N against C.
The carry repeats while the range covers C to its far end.
Admission then takes N across the boundary, because `reservationEnd` joins the runs.

- Code: about 150 lines in `buildJunctionConflicts` for the closure, and a new reference in the scenario reference test (estimate).
- Determinism and restore: the table is a function of the network.
  Restore gets the owners from the cells (`internal/sim/ownership.go:14`, `internal/sim/state_physical.go:1375`), so the format does not change.
  A save from before the change can restore with a demoted pod, which is allowed before v1.
- Contracts: no clause text changes.
  The debris footprint of section 7.2 of the incident suspension contract then blocks N for debris on C, which changes fault runs.
  Without a network field, Express behavior changes, and its qualification evidence goes stale (`docs/contracts/express-london-qualification-proposal.md:24`).
  A network field changes the format, as option B of section 4 of the lateral merge design did.
- Capacity: with groups ignored, it orders the grade-separated ramps under N.
  On LondonFull ramps, the reach has a p95 of 102.1 m and a maximum of 337.4 m, and the range on B grows by up to 310.1 m (measured).
  With groups respected, it adds 5,820 entries on LondonFull chords (measured).
  On the presets, the other classes already protect all of them, so they change behavior and close nothing.
  On `mergeCorridor`, the entries can move the pinned headways (inferred).
- `MaxJunctionPairs`: the limit no longer bounds the start time.
  A side can carry to 63 lanes at the far node (`MaxNodeLanes`, `internal/project/config.go:45`), with 2 comparisons for each.
  At 250,000 sides, one step costs up to 31.5 million comparisons, 126 times the current bound.
  Geometry bounds the closure, and a step count does not.
  The closure went through up to 9 lanes on the presets (measured).
  A count of the adjoining lanes over all sides, with no geometry, gives 58,686 on LondonFull chords and 59,674 on Express (measured).

### B. Hold the junction past the cell end

The pod keeps N until it is a distance past the cell end.

- Code: about 40 lines in `releaseDistance` and `resourceReleaseDistance` (`traffic.go:901`) (estimate).
  The distance depends on the next lane, and `releaseDistance` has no route information today.
- Coverage: it covers only a far end downstream of N.
  For a converging side, the gap is upstream, where the pod does not hold N yet, so that half needs option A.
  A pod on B outside its extent against A can still be near C, so B also needs the partner entries of option A.
- Determinism and restore: as option A, through `resourceReleaseDistance`.
- Contracts: section 1.2 of the tick-rate contract still holds.
  The text of `docs/model.md` changes.
- Capacity and liveness (inferred): a pod that holds N can wait for cells on C.
  Today a pod takes the whole junction run before it enters, so it does not hold N while it waits.
- `MaxJunctionPairs`: no change.

### C. Refuse the geometry in validation

C1 refuses each pair of an adjoining lane C and a partner B that is closer than the clearance, with the exemptions of the bank audit.
C2 runs the bank audit on all pairs in every network.
C2 removes the early return at `bank_geometry.go:85` and the skip at `:146`.
`Network.ValidateBankGeometry` (`:35`) gets the same change.
It also builds the retention index for every network, because today it builds it only under a condition (`bank_geometry.go:38`).
The editor draft checks and geometry edits keep their own feature gates (`internal/editormodel/checks.go:233`, `edit_geometry.go:102`), so they must also run the audit on a plain network.
Without that change, the editor accepts a draft that project validation then refuses.

- Code: C2 is about 20 lines (estimate).
  `prepareNetworkTails` already builds the retention index for every network (`prepared.go:155`).
  The existing audit keeps its place and its pair order.
  The pairs that C2 adds run in a separate pass, so a pair that fails today stays the first audit refusal.
  A new pass can still come before a later refusal of the same network, such as a lane length, a placement, or a project check.
  Decision L6 asks how to treat that order.
  C1 needs the junction extents in validation, and only `newPreparedNetwork` builds them today.
- Determinism and restore: a valid network runs as before.
  A saved state whose project now fails validation moves aside, as the restore rules require.
- Contracts: no clause text changes.
  C2 makes the 12 m rule of clearance layer A (`docs/contracts/service-contract-proposals.md:50`) a static check for unconnected pairs.
  Step 4 of section 3 of the lateral merge design says "or no check", and that text changes.
  The lateral merge design is approved, so that change needs maintainer approval.
- Capacity: no change on a valid network.
- `MaxJunctionPairs`: no change.
- Lateral merges: in a network without groups, C2 refuses the next lane C only when C and B are closer than the clearance that applies to them, and no exemption applies.
  A range that reaches the far end only by the 0.5 m sample pad does not cause a refusal, because the audit uses the segment distance with no pad.
- Scope: C2 also refuses same-plane lanes with no shared node that cross or come within 12 m away from a lane end.
  `Check` already fails such pods today, so the refusal moves an existing failure to load time.
- Fixtures: C1 and C2 both refuse `mergeCorridor` and `laneEndNetwork` without groups (measured).
  The tests that use `mergeCorridor` need a new destination layout.
  Its berths hold the initial fleet, and no pod uses them in the test (`merge_corridor_test.go:60`), so the headway pins can stay (inferred).

### D. Leave it, recorded

The record says that separation groups and the London audits cover the cases.
The measurement supports this for the presets and Express.
A project without banks or maneuver groups keeps the gap, and only `Check` sees it.
`mergeCorridor` has this gap today.

- Code: none.
- Determinism, restore, capacity, and `MaxJunctionPairs`: no change.
- Contracts: none.
  The text of the lateral merge design changes, which needs maintainer approval.

## 5. Recommendation

Use C2 and D together.
Run the all-pairs bank audit on every network, and keep the separation-group exemption, with a correct record.

Take pods p on C and q on B, where A and B are a pair at N, and C adjoins A at its far end.
With C2, one of these is true of C and B (inferred from the code):

1. They share a node M.
   The pair at M has its own extent, and the argument of section 3 of the lateral merge design applies at M.
2. They are in one maneuver group.
   Each of their cells holds the group junction, and a linked follower cannot share it (`traffic.go:697`).
   So only one pod is on the two lanes at a time.
3. They are in different nonempty groups and share no node.
   The model declares them on different levels (`docs/london.md`: "Different groups declare that unrelated paths can cross at different physical levels").
   This is an assumption, and decision L1 asks the maintainer to keep it.
   The exemption is for small pods.
   A pair with a large pod needs 20 m and the reach rule of `plane_reach.go` (`bank_geometry.go:150`, `:221`).
4. Otherwise, the bank audit passed for the pair.
   Each part of their polylines is at least the clearance of `audit.pieces` apart: 12 m, the depot lateral clearance, or the retention clearance.
   The audit gives each part the clearance that `Check` certifies for it (`bank_geometry.go:66`, `safety.go:261`), and pod positions are on the polylines.
   So p and q keep that clearance at each position, with no time term, at each tick rate.
   The audit and `Check` use the same separation tolerance.
   This case does not extend the audit beyond its reach model.
   The known limit of the observer envelope two lanes ahead (`docs/contracts/express-standees-contract-proposal.md:294`) stays.
   Lane and berth pairs that a certified maneuver exempts (`maneuver_certify.go:49`) are outside these four cases.

These four cases apply to any two lanes.
So they also cover the lane after C and the lane after B, with no recursion.
Cases 1, 2, and 4 hold today on every preset, because the presets run the audit or pass the planar layout audit.
C2 extends case 4 to every project.

Option A with groups ignored costs capacity on ramps and goes against the group model.
Option A with groups respected adds entries that protect nothing new on the presets.
Option B leaves the converging half open and lets a pod hold N while it waits.

## 6. Effects

| Item | C2 and D |
| --- | --- |
| Junction tables and cells, all networks | Same |
| `TestJunctionConflictsMatchReference`, `TestScenarioJunctionConflictsMatchReference` | Same |
| `TestPresetOutputIsPinned`, London hashes | Same |
| `command_digests.txt` | Same, because no command type or example project changes |
| `state_v12.json`, `stream_*` goldens, `stateVersion` 12, `StreamVersion` 10 | Same |
| Byte proofs | Same, because no format or limit changes |
| `TestMergeCorridorHeadway`, `TestMergeCorridorPlatoonHeadway`, and other users of `mergeCorridor` | The fixture needs a new destination layout. The pins can stay (inferred). |
| `TestLateralMergeExcludesLaneEndPairs` | Its fixture needs the separation groups of `TestLateralMergeLaneEndTwoStreams`. |
| Other unit-test fixtures | Not measured. Step 1 of section 9 counts them. |
| Validation refusals | New for a network without banks or maneuver groups. The text is that of `planeGapText` (`bank_geometry.go:196`). The existing audit keeps its order. A network with a new close pair and another fault can get a different first refusal, as decision L6 says. Tests with competing faults pin the order. |
| Start time | The audit runs on every network. Not measured. |
| Junction pair count against `MaxJunctionPairs` | Same |
| Contracts | No clause changes. |
| Lateral merge design | The "Lane ends" paragraph and step 4 of section 3 change. |
| Express | Same. Its network already runs the audit, and it passes (measured). |

## 7. Decisions

| ID | Decision | Options | Recommendation |
| --- | --- | --- | --- |
| L1 | Protection across separation groups | Keep the exemption, deny it when an incident lane joins the two reaches (the rule of `reachSeparated` for large pairs, `bank_geometry.go:158`, `plane_reach.go`), or carry N across groups (option A with groups ignored) | Keep the exemption. The reach rule refuses 1,171 cases on LondonFull chords and 1,852 on ramps, so the London generator must change. Option A costs a reach of up to 337.4 m on ramps. |
| L2 | Same-plane gap in a project without the all-pairs audit | C2, C1, A with groups respected, or D only | C2. It closes the gap with no runtime change. Every preset and the 9 test networks pass, and 2 unit-test fixtures need changes. |
| L3 | If A: bound for `MaxJunctionPairs` | Count the adjoining lanes of each side in the limit, or allow one step and refuse a longer carry | Count them. On LondonFull chords, the count is 58,686, and the total with the 49,440 sides is 108,126, inside the limit. |
| L4 | Clause 5 | Keep it, or replace it with a body carry of option A | Keep it. A replacement needs option A, and on LondonFull ramps the body reach of the excluded sides is up to 171.5 m. |
| L5 | Record | Correct the "Lane ends" paragraph and step 4 of section 3 of the lateral merge design | Correct both when C2 lands. |
| L6 | Refusal order for a network with a new close pair and another fault | Run the new pass after every other check of network, fleet, and project validation, or run it right after the existing audit and accept a new first refusal for such a network | Run it right after the existing audit. Any new refusal changes the first refusal of some network with several faults. Before v1, the order is pinned by new tests and not kept for old projects. This changes a part of the format, so it needs maintainer approval. |

## 8. Test plan

Unit tests:

- The example of section 1 with no groups: preparation refuses B and C, with the text of `planeGapText`.
- The same example with C and B in different groups: preparation passes.
- C with a shared node with B: preparation passes.
- C and B in one maneuver group: preparation passes.
- A depot lane pair at the depot lateral clearance: preparation passes.
- C at 12.01 m from B: preparation passes.
  At 11.99 m, preparation refuses it.
- Competing faults: a new close pair with an old audit pair, with a short lane, with a placement failure, and with a project check failure.
  Each test pins the first refusal that L6 chooses.
- A plain network in the editor: the draft check and a geometry edit refuse the close pair, as project validation does.
- `Network.ValidateBankGeometry` on a fixture with reduced retention gives the same result as preparation.
- `Network.ValidateBankGeometry` gives the same result as preparation for each fixture.
- Every ring preset, the London presets, Express, and each network of `scenario_networks.json.gz` prepare.
- A run of the example geometry with groups, with both lanes busy for 15 simulated minutes and `Check` at each tick.
  This test shows that the exemption does what the record says.

Mutations, with the table of the `verify-by-mutation` skill:

| Mutation | Test that fails |
| --- | --- |
| Keep the early return at `bank_geometry.go:85` | Example refusal |
| Keep the skip at `bank_geometry.go:146` | Example refusal |
| Treat blank groups as separated | Example refusal |
| Apply C2 only in `prepareNetworkTails` | `ValidateBankGeometry` parity |
| Add the 0.5 m sample pad to the audit clearance | 12.01 m case |
| Move the new pass to the other place of L6 | Competing faults |

Each mutation runs on a fixture without groups, so `Check` and the audit can fail on it.

## 9. Verification plan

1. Make a scratch build with C2, then run `go test -short ./...` to count the fixtures that fail to prepare.
   Bring back the list before any test changes.
2. Measure the `NewFleet` time and memory on scale100, LondonFull chords, and a generated network at `MaxLanes`, with and without C2.
3. Run the scratch measurement of section 3 again on the tip, to confirm the counts.
4. Run the merge corridor pins with the new destination layout, to confirm that they stay.
5. No study is necessary, because no valid network changes behavior.

## 10. Review

An Astra review on 2026-11-15 checked this proposal against the code at `959912c7`.
It found the direction sound, and it found four problems, which this text now addresses:

1. Major: the claim that no first refusal changes was not established.
   A new refusal can come before a later refusal of the same network.
   Section 4 C and decision L6 now state this, and the test plan pins the order.
2. Minor: the editor draft checks and geometry edits have their own feature gates, and `ValidateBankGeometry` builds the retention index only under a condition.
   Section 4 C and the test plan now include both.
3. Minor: the safety cases did not name the large-pod rule, the known observer envelope limit, or the maneuver exemptions.
   Section 5 now names them.
4. Minor: a range that reaches the far end does not by itself cause a refusal.
   Section 4 C now says that C2 refuses only a pair that is closer than its clearance.

The review confirmed the static lane-pair argument, the audit coverage of all lanes and berths, the option A bound as a conservative bound, the open converging half of option B, and the restore behavior.
It could not verify the scratch counts, because it did not run code.
