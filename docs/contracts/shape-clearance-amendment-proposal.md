# Shape clearance amendment proposal

Status: proposed on 2026-11-16, for the maintainer's approval.
On 2026-11-16, the maintainer approved the [shape clearance](../design/shape-clearance.md) design, with D1-B, D3-A, D5 included, D6-B, D12-A, D2 for all classes, D4 at 20 m, D7 as the network field `shapeClearance`, D8, D9, and D11.
Section 5 of the design lists the contract changes, and each one needs maintainer approval before the build.
This proposal gives the text of each change.
It changes no contract and no design.

## Conventions

- Each line reference is for `212c1b68`.
- Each amendment quotes the current text exactly, in a fenced block, with its lines.
  The replacement text is also in a fenced block, so that a later commit can paste it with its indentation.
- `<date>` is the date of the approval, in the ISO form of the notes, for example 2026-11-15.
  The "Amended through" line uses the long form, for example November 15, 2026.
- Each amended document that has an "Amended through" line changes it to the approval date, as `faed74fd` did.
  These are the service, Express physical, incident suspension, tick-rate, and standee contracts.
- Since D1-B is approved, this proposal drops each D1-A alternative of section 5.
- Each new rule applies only when the network sets `shapeClearance`, and a network without the field does not change.
  SC12 records a condition on evidence, and the replacement sentence of SC1 keeps its meaning without the field.

## Amendments

### SC1: Service contract, pair rule

Section 5 row: service contract, clearance layer A, `:50`.

Current text, `docs/contracts/service-contract-proposals.md:50`:

```markdown
  Every other pair of small pods keeps 12 meters, and a pair with a large pod keeps 20 meters.
```

Insert these lines before `:50`, after the lateral merge lines `:47` to `:49`:

```markdown
  When the network sets `shapeClearance`, the [shape clearance](../design/shape-clearance.md) rule applies to pods of each class.
  A following pair is two pods on one lane, or on two lanes where one ends at the node where the other starts.
  A following pair keeps its current rule: layer A on a qualifying lane, and otherwise 12 meters, or 20 meters with a large pod.
  A merge or split pair is two lanes that both end at one node or both start at it, as for a lateral merge pair.
  A lane that admits a large class or has a geometry tail can be part of a merge or split pair.
  A pair whose body range reaches the far end of a lane is not a merge or split pair.
  The junction of the node orders the two pods of a merge or split pair.
  A shape pair is two lanes on one plane that share no node, with disjoint swept regions.
  The network audit sweeps the grown body of each lane over the lane and over the join turns at both of its ends.
  When the two regions meet, or the sweep does not resolve, the pair keeps 12 meters, or 20 meters with a large pod.
  The network audit checks each pair of such a network, small pairs included, also without banks, large classes, maneuver groups, or depots.
  No resource orders the two pods of a shape pair.
  Two pods on a merge, split, or shape pair keep the side-by-side distance of their classes: 3.1 meters for two small pods, 3.55 meters for a small pod and a large pod, and 4.0 meters for two large pods.
  The grown body of a lane is the union of the bodies of the classes that the lane admits.
  When the fleet has a large pod, the planes of each pod are those of its envelope.
  The network audit takes these planes at each position on a path, not for the whole lane.
  Where these planes of two paths overlap, the audit applies the shape pair test in place of the 20 meter distance.
  In such a network, `lateralMerges` has no further effect.
```

Replace `:50` with:

```markdown
  Every other pair of small pods keeps 12 meters, and every other pair with a large pod keeps 20 meters.
```

The sentence must change, because its second half applies to each pair with a large pod.

### SC2: Service contract, guideway-bound assumption

Section 5 row: service contract, `:46`.

Current text, `docs/contracts/service-contract-proposals.md:45` to `:46`:

```markdown
  Two small pods on a certified lateral pair of depot lanes (`internal/sim/depot_lateral.go`) keep `W(v) = 3.1 + v²/2` meters, where `v` is the higher lane limit of the pair.
  That rule assumes guideway-bound pods: a pod stays within 0.25 meters of its lane and follows its heading.
```

Insert these lines after the SC1 lines, before `:50`:

```markdown
  Merge, split, and shape pairs assume guideway-bound pods at line speed: the reference of a pod stays within 0.25 meters of its lane, its heading follows the lane, and a fault stops it on its lane.
  Their distance has no speed term.
  Depot lateral pairs keep `W(v)`.
```

This is decision D3-A, with the assumption of section 3.4, item 4, of the design.

### SC3: Service contract, debris partners

Section 5 row: service contract, `:51` to `:52`.

Current text, `docs/contracts/service-contract-proposals.md:51` to `:53`:

```markdown
  Debris clearance stays 12 meters, and debris on a depot lane also blocks the cells of the neighbor depot lanes within 12 meters.
  Debris on a lane of a lateral merge pair also blocks the cells of the partner lane within 12 meters.
  Amended 2026-11-15, by maintainer decision: the lateral merge pair and its debris rule, behind the `lateralMerges` network field.
```

Insert this line after `:52`:

```markdown
  When the network sets `shapeClearance`, debris on a lane of a merge, split, or shape pair also blocks the cells of the partner lane within 12 meters.
```

The words "merge, split, or" depend on open question 3.
If the answer is shape partners only, as section 3.5 of the design names them, the line is this:

```markdown
  When the network sets `shapeClearance`, debris on a lane of a shape pair also blocks the cells of the partner lane within 12 meters.
```

Insert this line after `:53`, and keep only the parts that the maintainer approves:

```markdown
  Amended <date>, by maintainer decision: the following, merge, split, and shape pairs of each class, their guideway-bound assumption, their debris rule, and the envelope planes of the audit, behind the `shapeClearance` network field.
```

### SC4: Express physical contract, 20 m clearance

Section 5 row: Express physical contract, `:206`.

Current text, `docs/contracts/express-physical-encoding-contract-proposal.md:206`:

```markdown
Retain 20 m interaction clearance when either participant is large.
```

Insert these lines after `:206`:

```markdown
When the network sets `shapeClearance`, the 20 m clearance applies to a following pair: two pods on one lane, or on two lanes where one ends at the node where the other starts.
A merge, split, or shape pair with a large pod keeps the side-by-side distance of its two classes, 3.55 m with a small pod and 4.0 m with a large pod, as the [shape clearance](../design/shape-clearance.md) design defines.
```

### SC5: Express physical contract, union body

Section 5 row: Express physical contract, `:207`.

Current text, `docs/contracts/express-physical-encoding-contract-proposal.md:207`:

```markdown
Prepare resources for the largest admitted class even when no Express pod starts in the fleet.
```

Insert these lines after `:207`:

```markdown
When the network sets `shapeClearance`, the static extents of a merge, split, or shape pair use the union body of the classes that each lane admits, and not the body of the largest class.
The union body holds the body of each admitted class.
```

Insert this line after `:210`, the end of the paragraph, and keep only the parts that the maintainer approves:

```markdown
Amended <date>, by maintainer decision: the following pairs keep 20 m, and the merge, split, and shape pairs of the shape clearance design use the side-by-side distance and the union body, behind the `shapeClearance` network field.
```

### SC6: Express physical contract, Physical geometry gate

Section 5 row: qualification gate "Physical geometry", `:481`.

Current text, `docs/contracts/express-physical-encoding-contract-proposal.md:481`:

```markdown
| Physical geometry | Centered 10 m body within the 6 m envelope, 40 m lane boundary, every 20 m actual cell boundary, positive curve projection, short or nonfinite paths, crossings, merges, banks, parking, and all occupied planes. |
```

The gate table stays.
Insert a new subsection after `:529`, the last line of the London Express amendments, as amendments E1 to E8 do:

```markdown
### Shape clearance amendments

The maintainer approved these amendments on <date>.
They apply when the network sets `shapeClearance`, as the [shape clearance](../design/shape-clearance.md) design defines.

- SC6, Physical geometry: the centered 10 m body stays the test of the envelope, which following pairs, through junction pairs, and the envelope planes keep.
  A merge, split, or shape pair certifies the 6 m by 2.5 m class body only, with the union body for a lane that admits a small class and a large class.
  A class body longer than 6 m needs new sweeps and new disk bounds.
  Cover two straight lanes side by side for each pairing of admitted classes, at the side-by-side distance and 0.01 m over it, and lanes at 3, 15, 45, and 90 degrees that share no node.
  Cover the union body in both class orders, with Group in place of Express, and on mixed lanes whose initial fleet holds only small pods.
  Cover tangency, the disk bounds, and the join turns.
```

### SC7: Express physical contract, Independent safety oracle gate

Section 5 row: qualification gate "Independent safety oracle", `:483`.

Current text, `docs/contracts/express-physical-encoding-contract-proposal.md:483`:

```markdown
| Independent safety oracle | Following and crossing bodies, retained resource identities, both class orders, Express/Group mixtures, small neighbors, and compact groups. Oracle failures must identify tick, route location, bodies, and resource owner. |
```

The gate table stays.
Add this item to the subsection of SC6:

```markdown
- SC7, Independent safety oracle: the oracle also checks shape pairs, and merge and split pairs, with the side-by-side distance of the classes of the two pods.
  An oriented body oracle tests the grown bodies of these pairs at each tick.
  An independent body-and-ownership oracle runs at each tick of a merge test and of a restored continuation.
  For each merge or split pair whose oriented grown bodies are not disjoint, it requires that one pod holds the junction of the node and the other pod is outside its extent.
  A failure names the tick, the pods, the lanes, the bodies, and the owner of the junction.
  Its cases are a stopped pod, a faulted pod, an emergency journey, and a pod on a join turn at the end of a lane, each in both class orders.
```

When the maintainer approves only one of SC6 and SC7, the subsection keeps only that item.

### SC8: Group physics design, 20 m rule

Section 5 row: group physics design, Geometry and ownership, `:30`.

Current text, `docs/design/group-physics-design.md:30` and `:33` to `:34`:

```markdown
An interaction with a large class requires at least 20 meters between reference positions on overlapping physical planes.
```

```markdown
Network preparation checks the largest admitted class, even when the fleet contains only small pods.
It also checks small lanes that meet a large-admitting lane or berth node.
```

Insert these lines after `:30`:

```markdown
When the network sets `shapeClearance`, this distance applies to a following pair: two pods on one lane, or on two lanes where one ends at the node where the other starts.
A merge, split, or shape pair with a large pod keeps the side-by-side distance of its two classes, 3.55 meters with a small pod and 4.0 meters with a large pod, as the [shape clearance](shape-clearance.md) design defines.
The overlapping physical planes are those of the envelope of "Body planes" below.
```

Insert these lines after `:34`:

```markdown
When the network sets `shapeClearance`, the static extents of a merge, split, or shape pair use the union body of the classes that each lane admits, and not the body of the largest class.
Amended <date>, by maintainer decision: the following, merge, split, and shape pairs of the shape clearance design, behind the `shapeClearance` network field.
```

`:33` is the analog of `:207` of the Express physical contract, so it gets the union body sentence of SC5.

### SC9: Group physics design, plane reach of the audit

Section 5 row: group physics design, body planes, marked unchanged under D1-B.
That row is correct for the oracle exemption at `:68` to `:70`, but not for the audit at `:71` to `:75` (see "Corrections to section 5").

Current text, `docs/design/group-physics-design.md:71` to `:75`:

```markdown
The network audit applies the same rule before a run: a large pair of lanes gets the exemption only when every cross-pair of the lanes, their incident lanes, and the berths at their From nodes is on different planes.
The audit uses the whole lane, so a pass that is far from the join on a lane that is incident to a shared plane is also refused.
The audit returns this refusal as a `PlaneReachError`, and a caller reports it after its other checks (the path, length, and cell checks of preparation, the placements of a fleet, and the other checks of a project), so that it does not hide an earlier refusal.
The route window of a pod can reach further than an incident lane, because the front bound is the largest `6 * projection bound / 20` of the lanes of the route, and each lane stores it beside its tail.
The audit does not cover that case, so the observer can report a pair that the audit accepted.
```

Insert these lines after `:75`:

```markdown
When the network sets `shapeClearance`, the audit checks each pair, small pairs included, also in a network without banks, large classes, maneuver groups, or depots.
It takes the planes of the envelope at each position on a path, and not for the whole lane.
A lane has the planes of the lanes into its From node in its first 20 meters, or the longer tail of those lanes.
When its From node is a berth node, the lane also has that berth and the lanes at its node, up to the origin tail of the lane.
A lane has the planes of the lanes out of its To node in its last 6 meters, or the longer front bound of the lane.
A berth has its own plane and the planes of the lanes at its node.
The audit does not take the other lanes at these nodes.
Where these planes of two paths overlap on a part of each path, the pair is not exempt on those parts.
On those parts, the pair is a shape pair when the swept bodies of the two paths are disjoint, and otherwise it keeps 20 meters.
The order of the refusals and their text do not change.
Amended <date>, by maintainer decision: the envelope planes of the audit, behind the `shapeClearance` network field.
```

The oracle exemption at `:68` to `:70` does not change, because D1-B keeps the envelope of each pod.
Open question 1 asks whether `:74` to `:75` stay true when the network sets the field.

### SC10: Incident suspension contract, debris footprint

Section 5 row: incident suspension contract, section 7.2.

Current text, `docs/contracts/incident-suspension-contract-proposal.md:506` to `:508`:

```markdown
A lateral neighbor is a depot lane that the lateral certification pairs with `l` (`internal/sim/depot_lateral.go`).
When the network sets `lateralMerges`, a lateral neighbor is also a lane that forms a lateral merge pair with `l` ([lateral merge zones](../design/lateral-merge-zones.md)).
A lateral neighbor can be closer than `Clearance` to `l`.
```

Insert this line after `:507`:

```markdown
When the network sets `shapeClearance`, a lateral neighbor is also a lane that forms a merge or split pair or a shape pair with `l` ([shape clearance](../design/shape-clearance.md)).
```

Insert this line after `:516`:

```markdown
Amended <date>, by maintainer decision: a lateral neighbor also includes the partner lanes of a merge, split, or shape pair when the network sets `shapeClearance`.
```

SC10 and SC3 state one rule in two contracts.
The words "a merge or split pair or" depend on open question 3.
If the answer is shape partners only, the inserted line is this:

```markdown
When the network sets `shapeClearance`, a lateral neighbor is also a lane that forms a shape pair with `l` ([shape clearance](../design/shape-clearance.md)).
```

The note then says "the partner lanes of a shape pair".

### SC11: Tick-rate contract, section 1.5

Section 5 row: tick-rate contract, sections 1.2 and 1.5.

Current text, `docs/contracts/tick-rate-contract-proposal.md:93` to `:97`:

```markdown
A lateral pair uses `lateralClearance`, which is 3.1 m plus `v²/2`, at the higher speed limit of the pair (`internal/sim/depot_lateral.go:33`).
The sweep of a lateral pair and the maneuver sweep of a bay run at network preparation, over continuous geometry, with no time term.
A lateral merge pair of a network that sets `lateralMerges` keeps `lateralClearance(0)`, which is 3.1 m, with no speed term (`internal/sim/lateral_merges.go`).
The junction of its node orders the two pods, and its body sweep runs at network preparation, over continuous geometry, with no time term.
Amended 2026-11-15, by maintainer decision: the lateral merge pairs of the [lateral merge zones](../design/lateral-merge-zones.md) design.
```

Insert these lines after `:97`:

```markdown
A merge or split pair of a network that sets `shapeClearance` keeps the side-by-side distance of the classes of its two pods, 3.1 m, 3.55 m, or 4.0 m, with no speed term ([shape clearance](../design/shape-clearance.md)).
The junction of its node orders the two pods, and its body sweep runs at network preparation, over continuous geometry, with no time term.
A shape pair of such a network keeps the same distance, with no speed term.
Its sweep runs at network preparation, over continuous geometry, with no time term.
The sweep proves that the grown bodies are disjoint at each pair of positions on the two lanes, so no resource orders the two pods.
Amended <date>, by maintainer decision: the merge, split, and shape pairs of the shape clearance design.
```

Section 1.2 needs no change.
Its text at `:60` is "The large-class clearance of 20 m follows the same argument."
The argument is a path gap along one route, and SC1 keeps 20 m for a following pair.

### SC12: Express London qualification, evidence of D

Section 5.1 of the design gives the effect on the qualification rows.
The field does not change the text of a row, so no row needs an amendment.
The rows live in the qualification proposal (`docs/contracts/express-london-qualification-proposal.md:136` to `:149`) and in their folded copies: the physical contract `:506` to `:529`, the standee contract `:455` to `:457`, the stage 7 contract `:634` to `:645`, and `docs/station-banks.md:116` to `:118`.
None of these texts changes.

The evidence of D depends on a test of the build, so SC12 records that condition in the qualification proposal only.
Insert these lines after `docs/contracts/express-london-qualification-proposal.md:149`, the last row of the amendments table:

```markdown

D does not set `shapeClearance`, and the field does not change the text of a row above.
D keeps its evidence for these rows only if a test shows behavior parity.
With the field built and unset, D must give the same saved-state and stream bytes at each of its checkpoints as the build before the change.
Without that test, each row of D needs its evidence again.
The byte proofs of E5 and A18 run again, because the worst-case project gets one more member.
A shared-track preset is a new project, and its rows need their own evidence, also where the limits and the requirements do not change.
Amended <date>, by maintainer decision: the evidence of D under the [shape clearance](../design/shape-clearance.md) design.
```

The D6 oracle that section 5.1 adds to E3 for a project with the field comes from SC7.
E3 names D only, so its text does not change.

### SC13: Express standees contract, the 20 m proof

Section 5 does not name this contract (see "Documents that section 5 omits").

Current text, `docs/contracts/express-standees-contract-proposal.md:73`:

```markdown
The 6 m radius circle and the 20 m pair clearance stay.
```

Insert this line after `:73`:

```markdown
When the network sets `shapeClearance`, the 20 m pair clearance stays for following pairs, and a merge, split, or shape pair keeps the side-by-side distance of its two classes, as the [shape clearance](../design/shape-clearance.md) design defines.
```

Current text, `docs/contracts/express-standees-contract-proposal.md:212` to `:214`:

```markdown
Each lane has a set of locations: the lane, its incident lanes, and the berths at its `From` node.
Take pods `x` and `y`, with at least one of a large class, and suppose that some pair from their two sets is on one plane.
Then the centers are at least 20 m apart at each tick, less the tolerance of 1 µm that the audit (`internal/sim/bank_geometry.go:174`) and the observer (`internal/sim/safety.go:206`) both permit.
```

Insert these lines after `:218`, the end of section 4.2:

```markdown
When the network sets `shapeClearance`, this claim holds for case A, for a through pair of case B, and for a pair of case C whose swept bodies meet or whose sweep does not resolve.
A merge or split pair of case B, and a shape pair of case C, keep the side-by-side distance of the classes of the two pods instead, by section 3.4 of the [shape clearance](../design/shape-clearance.md) design.
In case C, the set of locations of a lane is then the set of envelope planes at each position on the lane, and not the whole lane with its incident lanes.
Amended <date>, by maintainer decision: the claim of the 20 m proof under the `shapeClearance` network field.
```

Sections 4.4 and 4.5 (`:239` to `:243`, `:251` to `:258`) then read under the amended claim of section 4.2, so they keep their text.

### SC14: Lateral merge zones design, clause 3

Section 5 does not name this design.
Part 3 of the shape clearance design drops its clause 3 when the field is set.
The group physics design gets an amendment for the same reason (section 5: "Not a contract, but approved text, so it needs an explicit amendment").

Current text, `docs/design/lateral-merge-zones.md:72` and `:80`:

```markdown
3. Both lanes are small: no large class and no geometry tail, as for the depot `small` member (`internal/sim/depot_lateral.go:70`).
```

```markdown
Pairs with a large class keep 20 m, as the Express contract requires (`docs/contracts/express-physical-encoding-contract-proposal.md:206`).
```

Insert these lines after `:80`:

```markdown
When the network sets `shapeClearance`, the [shape clearance](shape-clearance.md) design applies this rule to each class, and `lateralMerges` has no further effect.
Clause 3 does not apply, so a lane with a large class or a geometry tail can form a pair.
The body of each lane is the union of the bodies of the classes that the lane admits, and a pair keeps the side-by-side distance of the classes of its two pods.
Clause 5 stays.
Amended <date>, by maintainer decision: the rule for each class, behind the `shapeClearance` network field.
```

The worktree of `/tmp/build/podsim/docfix/wt` edits this file, so the line numbers can move before the paste.
Use the quoted text as the anchor.

### SC15: Station banks document, audit text

Section 5 does not name this document.
It is a guide and not a contract, but it holds the approved amendment B1, and the [bank load choice](bank-load-choice-proposal.md) proposal asked for approval of a change to its rule text.
The maintainer can approve SC15, or treat it as a documentation change of the build (open question 6).

Current text, `docs/station-banks.md:54` to `:60`:

```markdown
A pair with a large class needs 20 meters.
Such a pair skips the audit by plane only when each location that either path can occupy is on a different plane from each location of the other path.
The locations of a lane are the lane, its incident lanes, and each berth at its From node.
The audit reports a pair that only these locations refuse after its scan, and a network, a fleet, or a project reports that refusal after its other checks.
The locations of a berth are the berth and its incident lanes.
Maneuver groups and depots (`internal/sim/depot.go`) use the same audit.
With a depot and no bank, the audit checks only the pairs with a depot lane or a slot.
```

Insert these lines after `:60`:

```markdown
When the network sets `shapeClearance`, the audit checks each pair, small pairs included, also in a network without banks, large classes, maneuver groups, or depots.
The locations of a path are then the envelope planes at each position on the path, and not the whole lane with its incident lanes ([shape clearance](design/shape-clearance.md)).
When two paths that share no node have disjoint swept bodies, the pair is a shape pair, and the audit does not refuse it.
Otherwise the pair needs 12 meters, or 20 meters with a large class.
The order of the refusals and their text do not change.
```

## Rows with no change

These rows of section 5 need no amendment under D1-B:

- Express physical contract, `:217`: "Check merges, crossings, plane transitions, banks, parking, and neighboring berths using their actual paths and resource identities."
  Under D1-B, the audit checks the envelope planes and the body sweep on the actual paths, so the text holds.
- Express physical contract, `:219`: "There is no blanket certification for curves or authored junctions."
  The audit proves each shape pair on its actual paths, and a pair that it cannot prove keeps the isotropic clearance.
- Express physical contract, amendment E2, `:509` to `:511`: "The current geometry and class rules stay."
  D does not set the field (D11), so E2 holds for D.
  A new project with shared track needs its own row.
- Group physics design, body planes, `:68` to `:70`: the plane exemption of the oracle.
  D1-B keeps the envelope of each pod, so the exemption does not change.
  The audit text at `:71` to `:75` changes, in SC9.
- Tick-rate contract, section 1.2, `:50` to `:60`: a path-gap argument along one route, and SC1 keeps 20 m for a following pair.

These rows of the Express physical contract are not in section 5, and D4 keeps them: `:209` ("Retain lane minimum 40 m and check every actual cell's offsets for at least 20 m."), `:212` to `:213` (the retained tail of at least 20 m), and `:236` ("Test the retained 20 m influence of a large neighbor without changing those compact contracts.").
The Physical restore gate at `:484` ("Check safe mixed continuation.") needs no change, because SC7 runs the oracles in restored continuation.

## Corrections to section 5

Items 1, 2, and 4 correct a line or a quote.
The other items correct the scope of a row.

1. Incident suspension contract: section 5 cites `:504` for "Lateral neighbors are depot and lateral merge partners".
   `:504` is the first sentence of the footprint.
   The definition of a lateral neighbor is at `:506` to `:507`, and the amendment note is at `:516`.
2. Tick-rate contract: section 5 cites `:84` for section 1.5.
   `:84` is in section 1.4, the platoon proof.
   Section 1.5 starts at `:90`, and its lateral pair lines are `:93` to `:97`.
3. Tick-rate contract, section 1.5: section 5 adds a line for shape pairs only.
   Under D5, a merge or split pair with a large lane keeps 3.55 m or 4.0 m, not the 3.1 m of `:95`.
   So section 1.5 also needs a line for merge and split pairs, in SC11.
4. Express physical contract, `:217`, `:481`, `:483`, and `:509`: section 5 gives a paraphrase as the current text.
   This proposal quotes the exact lines.
5. Group physics design, body planes: section 5 marks the row unchanged under D1-B.
   The oracle exemption at `:68` to `:70` does not change.
   The audit at `:71` to `:72` uses the whole lane, which D1-B replaces with the envelope planes by position, so SC9 amends it.
6. Group physics design: section 5 names `:30` only.
   `:33` ("Network preparation checks the largest admitted class") is the analog of `:207` of the Express physical contract, so SC8 adds the union body sentence after `:34`.
7. Service contract, `:50`: section 5 adds text after the sentence.
   The second half of the sentence, "a pair with a large pod keeps 20 meters", applies to each pair with a large pod, so SC1 also replaces it.
8. Service contract: the row says "The planes of a pod follow D1".
   With D1-B approved, SC1 states the envelope planes.

Each other line number and quote of section 5 matches `212c1b68`: the service contract `:46`, `:50`, and `:51` to `:52`, the Express physical contract `:206`, `:207`, and `:219`, the group physics design `:30`, and the tick-rate contract `:50`.

## Documents that section 5 omits

These documents need an amendment:

- Express standees contract, sections 2.3 and 4.2 (SC13).
  Section 2.3 (`:73`) keeps the 20 m pair clearance with no condition.
  The claim of section 4.2 (`:212` to `:214`) proves 20 m for each pair of a large pod on one plane, with location sets of whole lanes.
  Case B (`:239` to `:243`) uses the 20 m junction extent for each pair at a node, and case C (`:251` to `:258`) uses the whole-lane audit.
  Under D5 and D1-B, merge, split, and shape pairs keep less than 20 m.
- Lateral merge zones design, clause 3 (SC14).
  It is approved text, and part 3 of the design drops clause 3 when the field is set.
- Station banks document, the audit text (SC15).
  `:60` limits the audit to depot pairs when the network has a depot and no bank, and D12-A changes this when the field is set.

These documents need no amendment:

- Service byte contract.
  Its plain bounds "include the independent project or topology cap" (`docs/contracts/service-byte-contract-proposal.md:31` to `:32`).
  `widestProofProject` (`internal/session/plain_byte_proof_test.go:173` to `:200`) sets `LateralMerges` and pads the project to `project.MaxFileBytes`.
  The Express proof uses the same project (`internal/session/express_byte_proof_test.go:77`).
  The stage 7 contract says that "The project member is unchanged in these proofs, because the fixture pads the project to its independent cap" (`:239`).
  So the proofs run again with the new member, and this proposal asserts no new numbers.
- Express physical contract, format rows (`:238` to `:318`).
  The field is a member of the network in the project, and these rows name no network member.
  A network field with a safe zero value keeps `stateVersion` and `StreamVersion` (design section 3.6), as `lateralMerges` did (`docs/operations.md:422`).
- Digest extension 17.
  Section 11.3 of the service transitions contract already says "Each later field takes the next free number" (`:1232`).
  The emergency contract lists only its own entries (`:980` to `:985`).
  `faed74fd` and `613ed82f` show that extension 16 of `lateralMerges` changed no format, digest, or byte contract.
- Service transitions contract.
  It has no clearance or plane rule.
  Its only related text is the digest rule above.
- Incident emergency contract.
  It has no clearance, lateral pair, or plane rule.
  Its digest table (`:980` to `:985`) lists only its own entries.
- Berth claim deadlock proposal.
  It has no clearance, lateral pair, or plane text.
- Station banks row B1 (`docs/station-banks.md:116` to `:118`).
  The field adds no saved bank member and no format version, so B1 holds.
- Stage 7 rows A15 to A18 (`docs/contracts/incident-stage7-contract-proposal.md:634` to `:645`).
  Their text names D, and D does not set the field.
  Their evidence follows SC12.
- Lane-end protection design, decision D.
  D12-A runs the all-pairs audit only in a network that sets the field, so decision D holds for the other networks (design section 6, D12).

These documents change with the build and need no approval: `docs/model.md:37` to `:38` and its junctions section, the network members of `docs/reference.md`, and the saved-state text of `docs/operations.md:422` to `:423`.

## Open questions

1. Front bound of the envelope planes.
   Section 2.1 of the design gives a lane "the longer front bound of the lane", and SC9 uses that text.
   The same section also defines the rule "for any route through the lane", and section 3.4, item 5, says that the audit covers each pair of envelopes that the oracle can see.
   The front bound of the envelope is the largest of the lanes of the route (group physics design `:74`).
   The design does not say which value the audit takes.
   The answer decides whether group physics design `:74` to `:75` and item 4 of section 4.7 of the standee contract (`:294`) stay true when the network sets the field.
2. Node range of a merge or split pair with a large lane.
   The lateral merge zones design gives the last or first 12 m of a lane as the node range (`:87`).
   The shape clearance design does not say if a lane that admits a large class keeps 12 m or uses 20 m.
3. Debris partners of merge and split pairs.
   Section 3.5 of the design names only the shape partners for the debris footprint.
   Section 3.6 says that `lateralMerges` has no further effect when the field is set, because part 3 includes it.
   The design does not say if the merge and split partners of part 3 join the footprint.
   Without them, a network that sets both fields loses the debris partners of its lateral merge pairs.
   SC3 and SC10 give the text for each answer.
4. D6 oracle in a plain project.
   SC7 puts the body-and-ownership oracle in the Express gate, as section 5 does.
   D2 applies the field to small pods too, and no contract row requires the D6 oracle for a plain project that sets the field.
5. Rows of the shared-track preset.
   Section 5.1 says that the preset "Needs its own row, or E1 extended to name it".
   The preset has no name or geometry yet, so this proposal writes no row for it.
   Its rows come with its own qualification proposal.
6. Station banks document.
   SC15 changes a guide.
   The maintainer can approve it as an amendment, or treat it as a documentation change of the build.
7. Amendment notes.
   The group physics design and the lateral merge zones design have no "Amended" notes today.
   SC8, SC9, and SC14 add one, as the contracts do.

## Approval

| ID | Document | Change |
| --- | --- | --- |
| SC1 | Service contract | Following, merge, split, and shape pairs of each class, with the side-by-side distances and the envelope planes. |
| SC2 | Service contract | The guideway-bound assumption at line speed for merge, split, and shape pairs. |
| SC3 | Service contract | Debris blocks the cells of merge, split, and shape partners within 12 meters. |
| SC4 | Express physical contract | 20 m for following pairs, and the side-by-side distance for lateral pairs with a large pod. |
| SC5 | Express physical contract | Union body for the static extents of lateral pairs. |
| SC6 | Express physical contract | Physical geometry gate: the class body for lateral pairs, and the sweep cases. |
| SC7 | Express physical contract | Independent safety oracle gate: shape pairs and the body-and-ownership oracle. |
| SC8 | Group physics design | 20 m for following pairs, the side-by-side distance for lateral pairs, and the union body. |
| SC9 | Group physics design | The audit takes the envelope planes by position, with the body test where they overlap. |
| SC10 | Incident suspension contract | Merge, split, and shape partners are lateral neighbors of the debris footprint. |
| SC11 | Tick-rate contract | Section 1.5 lines for merge, split, and shape pairs, with no time term. |
| SC12 | Express London qualification | D keeps its evidence only with a behavior parity test. |
| SC13 | Express standees contract | The 20 m claim covers following pairs, and lateral pairs keep the side-by-side distance. |
| SC14 | Lateral merge zones design | Clause 3 does not apply when the field is set. |
| SC15 | Station banks document | The audit text for the field. |

Dependencies:

- One field sets parts 1 to 5 of the design together (section 6.1).
  So each of SC1 to SC11, SC13, and SC14 states a part of one rule, and the build of the field needs all of them.
  If the maintainer approves only some, a binding text contradicts the behavior of the field.
- SC2 is the assumption that SC1 needs for shape pairs at line speed, so SC1 and SC2 go together.
- SC3 and SC10 state one debris rule in two contracts, so they go together.
  Both use the pair names of SC1.
- SC4, SC5, SC8, SC11, SC13, and SC14 use the pair names and distances of SC1.
- SC9 gives the envelope planes that SC1 names, so SC1 and SC9 go together.
- SC7 is the D6 oracle that D5 needs (section 6.1), and SC1 includes the D5 merge pairs, so SC1 needs SC7.
- SC6 and SC7 share one new subsection of the Express physical contract.
- SC12 does not depend on the others, and the maintainer can approve it alone.
- SC15 uses the terms of SC1 and SC9, and it can wait for open question 6.
  SC1 and SC9 carry the all-pairs audit of D12-A, so the rule does not depend on SC15.
