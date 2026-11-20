# Shape clearance amendment proposal

Status: approved by the maintainer on 2026-11-16, and applied.
Note of 2026-11-20: the [always-on proposal](shape-clearance-always-on-proposal.md) removes the field, and each rule of SC1 to SC16 applies to each network.
The conditional clauses below are records.
The target documents carry the text of AO1 to AO36 and AO40 to AO44.
Revised on 2026-11-16 after an adversarial review (see "Review").
On 2026-11-16, the maintainer approved the [shape clearance](../design/shape-clearance.md) design, with D1-B, D3-A, D5 included, D6-B, D12-A, D2 for all classes, D4 at 20 m, D7 as the network field `shapeClearance`, D8, D9, and D11.
Section 5 of the design lists the contract changes.
This proposal gives the text of each change, and the text is now in each target document.

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
- Each term has one definition.
  SC1 defines the following, merge, split, and shape pairs, the separated positions, and shape certification.
  SC9 defines the envelope reach.
  The other amendments refer to these definitions.

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
  The certified pairs and the skip rules of the maneuver groups come first and do not change.
  The depot lateral pairs keep `W(v)`, and clearance layer A does not change.
  A following pair is two pods on one lane, or on two lanes where one ends at the node where the other starts.
  A following pair keeps its current rule: layer A on a qualifying lane, and otherwise 12 meters, or 20 meters with a large pod.
  A merge or split pair is two lanes that meet clauses 1, 2, 4, and 5 of the [lateral merge zones](../design/lateral-merge-zones.md) design.
  The two lanes both end at one node or both start at it, neither lane is a loop, and the two lanes do not share both endpoints.
  Neither lane is in a maneuver group or a depot, and the body range does not reach the far end of either lane.
  A lane that admits a large class or has a geometry tail can be part of a merge or split pair.
  The junction of the node orders the two pods of a merge or split pair.
  Its extent is the body range with the union body, and the node range of the last or first 12 meters of the lane.
  A position on a lane has the plane of the lane when neither lane of a pair admits a large class.
  Otherwise it has the planes of its envelope reach, as the [group physics design](../design/group-physics-design.md) defines it under the field.
  Two positions, one on each of two paths, are separated when each plane of one and each plane of the other are different planes.
  At separated positions, the plane exemption holds as before.
  Shape certification sweeps the grown body of each of two lanes that share no node over the lane and over the join turns at both of its ends.
  It certifies the two lanes as a shape pair when the grown bodies are disjoint at each pair of positions that are not separated.
  Two lanes on one plane have no separated positions, so their two swept regions must be disjoint.
  When the grown bodies meet at such a pair of positions, or the sweep does not resolve, the two lanes are not a shape pair.
  The network audit then requires 12 meters, or 20 meters when either lane admits a large class, at each pair of positions that are not separated.
  Shape certification is for two lanes only, and a pair with a berth keeps its current rule.
  The network audit checks each pair of such a network, small pairs included, also without banks, large classes, maneuver groups, or depots.
  No resource orders the two pods of a shape pair.
  Two pods on a merge, split, or shape pair keep the side-by-side distance of their classes: 3.1 meters for two small pods, 3.55 meters for a small pod and a large pod, and 4.0 meters for two large pods.
  The grown body of a lane is the union of the bodies of the classes that the lane admits.
  When the fleet has a large pod, the oracle keeps the envelope of each pod, so the plane exemption of two pods does not change.
  The qualification of the field, for pods of each class, runs the body-and-ownership oracle of the design at each tick of a merge test and of a restored continuation.
  In such a network, `lateralMerges` has no further effect.
```

Replace `:50` with:

```markdown
  Every other pair of small pods keeps 12 meters, and every other pair with a large pod keeps 20 meters.
```

The sentence must change, because its second half applies to each pair with a large pod.

These lines follow the design:

- Precedence: section 3.7 keeps the maneuver certificates and skip rules first, keeps `3.1 + v²/2` for the depot lateral pairs, and keeps retention layer A.
  The audit and the oracle apply them in that order today (`internal/sim/bank_geometry.go:154` and `:203`, `internal/sim/safety.go:262` to `:290`).
  A certified depot lateral pair can be two lanes that share no node (`internal/sim/depot_lateral.go:8` to `:19`, and the depot row of design `:274`), so without these lines it could also be a shape pair.
- Merge and split pairs: part 3 drops clause 3 only, and the code applies clauses 1, 2, and 4 (`lateralMergePair`, `internal/sim/lateral_merges.go:141`, and `eligible`, `:42`).
- Node range: section 3.4, item 2, says that the union body changes only the body range.
  The node range stays 12 m (`lateral-merge-zones.md:87`, and `sweep.bound = length - Clearance`, `lateral_merges.go:234`).
  D4 keeps the other 20 m protections.
- Separated positions: part 1 under D1-B applies part 4 only where the envelope reaches of two paths overlap (design `:462`).
  The audit gives the envelope planes only to a pair with a large path (`internal/sim/bank_geometry.go:221` to `:236`), and section 2.1 gives each rule to the small path of such a pair (design `:141`).
- Fallback: part 4 keeps 12 m or 20 m for a pair that the sweep does not certify (design `:475`).
- Lanes only: part 4 sweeps lane bodies and join turns (design `:473`).
  The design defines no body or heading for a berth, and its layout of two platforms side by side uses two platform lanes (design `:661`).
- D6 oracle: D2 covers all classes, and section 7 runs the oracle at each tick of a merge test and of a restored continuation, with no Express condition.
  SC7 puts the same oracle in the Express gate, and this line keeps it for a plain project with the field.

Note of 2026-11-16: under the field, amendment ST4 of the [shared-track amendment proposal](shared-track-amendment-proposal.md) replaces the line at `:66` in the service contract, because the oracle envelope takes the local front and the small-pod rules.
Note of 2026-11-16: the [second shared-track amendment proposal](shared-track-amendment-2-proposal.md) replaces the local front and the reach bound with the anchor front of each lane and an oracle that reads the compiled envelope reach, by amendments ST16, ST17, and ST20.

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
```

This is decision D3-A, with the assumption of section 3.4, item 4, of the design.
The SC1 lines state that depot lateral pairs keep `W(v)`, so SC2 does not repeat it.

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

Insert this line after `:53`, and keep only the parts that the maintainer approves:

```markdown
  Amended <date>, by maintainer decision: the following, merge, split, and shape pairs of each class, their guideway-bound assumption, their debris rule, and the envelope planes of the audit, behind the `shapeClearance` network field.
```

Part 3 of the design applies the lateral merge rule to each class.
That rule includes its debris partners (decision D5 of the lateral merge zones design, and the shared table of `newMergeLateral`, `internal/sim/lateral_merges.go:30` to `:34`).
Section 3.5 of the design adds the shape partners.
So the footprint holds the merge, split, and shape partners.
With shape partners only, `lateralMerges` would still change the footprint when both fields are set, which section 3.6 excludes.

### SC4: Express physical contract, 20 m clearance

Section 5 row: Express physical contract, `:206`.

Current text, `docs/contracts/express-physical-encoding-contract-proposal.md:206`:

```markdown
Retain 20 m interaction clearance when either participant is large.
```

Insert these lines after `:206`:

```markdown
When the network sets `shapeClearance`, a merge, split, or shape pair with a large pod keeps the side-by-side distance of its two classes, 3.55 m with a small pod and 4.0 m with a large pod, as the [shape clearance](../design/shape-clearance.md) design defines.
Each other pair keeps the 20 m clearance, a following pair included: two pods on one lane, or on two lanes where one ends at the node where the other starts.
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
Amended <date>, by maintainer decision: the merge, split, and shape pairs of the shape clearance design use the side-by-side distance and the union body, and each other pair keeps 20 m, behind the `shapeClearance` network field.
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
SC1 states the same body-and-ownership oracle for pods of each class, so this gate does not limit it to Express.

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
When the network sets `shapeClearance`, a merge, split, or shape pair with a large pod keeps the side-by-side distance of its two classes, 3.55 meters with a small pod and 4.0 meters with a large pod, as the [shape clearance](shape-clearance.md) design defines.
Each other pair with a large pod keeps the 20 meter clearance, a following pair included: two pods on one lane, or on two lanes where one ends at the node where the other starts.
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
For a pair with a large path, it takes the planes of each path by position, and not for the whole lane.
The envelope reach of a position on a lane holds each plane that the envelope of a pod at that position can hold, on each route through the lane.
The reach can hold more planes than such an envelope, but not fewer.
This includes the lanes behind the position on the route, each up to its retained tail, also more than one lane back.
It includes the lanes ahead on the route, up to the largest front bound of the lanes of the route, also more than one lane ahead.
It includes the origin berth of the route and the lanes at its node, at each position before the origin release.
A berth has its own plane and the planes of the lanes at its node.
A test shows, for each lane of a test network and positions along it, that the planes of `appendVehicleSafetyLocations` for a pod on each route through the lane are in the envelope reach at that position.
At positions that the service contract calls separated, the pair is exempt, as before.
At the other positions, two lanes keep the shape certification of the [service contract](../contracts/service-contract-proposals.md).
Two lanes that it does not certify, and a pair with a berth, keep 12 meters, or 20 meters when either path admits a large class, at those positions.
The certified pairs and the skip rules of the maneuver groups, the depot lateral pairs, and the retention rules come first and do not change.
In such a network, the envelope reach covers the route window of the case above, so that case stays a gap only in a network without the field.
The order of the refusals and their text do not change.
Amended <date>, by maintainer decision: the envelope planes of the audit, behind the `shapeClearance` network field.
```

The oracle exemption at `:68` to `:70` does not change, because D1-B keeps the envelope of each pod.

The envelope reach follows the safety requirement of the design, and not the probe:

- Section 3.4, item 5, says that each envelope plane of a pod at a position is in the reach of that part of its path, so the audit covers each pair of envelopes that the oracle can see.
  The envelope reach test of section 7 (design `:810`) checks this containment, and the line above states it.
- Section 2.1 describes the reach that the probe measured, with the front bound of the lane only and the lanes of one node back.
  The design names these as limits of the probe (`:142` to `:144`).
- The code takes the largest front bound of the route, and tests each route lane against its own retained tail (`internal/sim/envelope_safety.go:50` to `:69`).
  The unreleased origin adds the origin berth and the lanes at its node (`:70` to `:85`).

`:74` to `:75` record the gap of the front bound for a network without the field.
The containment above closes it for a network with the field.
Note of 2026-11-16: decision S1 of the [shared-track](../design/shared-track-preset.md) design changes the SC9 text and its reasons under the field.
The route window of the oracle takes the local front, and not the largest front bound of the route (`:282`, `:302`, and `:315`).
The reach takes the reach bound of each lane (`:294`), and a berth also holds the clusters of rule 2 (`:296`).
Note of 2026-11-16: the [second shared-track amendment proposal](shared-track-amendment-2-proposal.md) replaces the local front and the reach bound with the anchor front of each lane and an oracle that reads the compiled envelope reach, by amendments ST16, ST17, and ST20.
The oracle envelope changes, so the claim of `:307` holds only in a network without the field.
Amendments ST2, ST3, ST9, and ST14 of the [shared-track amendment proposal](shared-track-amendment-proposal.md) give the applied text.

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
SC3 gives the reason for the merge and split partners.

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
Its sweep is the shape certification of the [service contract](service-contract-proposals.md), and it runs at network preparation, over continuous geometry, with no time term.
The sweep proves that the grown bodies are disjoint at each pair of positions that are not separated, and the plane exemption holds at the other positions.
So no resource orders the two pods.
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
For a network without the field, the claim of section 4.2 and its proof in sections 4.3 to 4.5 do not change.
With the field, sections 4.4 and 4.5 each get a branch, because their premises change for merge, split, and shape pairs.

Current text, `docs/contracts/express-standees-contract-proposal.md:73`:

```markdown
The 6 m radius circle and the 20 m pair clearance stay.
```

Insert this line after `:73`:

```markdown
When the network sets `shapeClearance`, a merge, split, or shape pair keeps the side-by-side distance of its two classes, as the [shape clearance](../design/shape-clearance.md) design defines, and each other pair keeps the 20 m pair clearance.
```

Current text, `docs/contracts/express-standees-contract-proposal.md:212` to `:214`:

```markdown
Each lane has a set of locations: the lane, its incident lanes, and the berths at its `From` node.
Take pods `x` and `y`, with at least one of a large class, and suppose that some pair from their two sets is on one plane.
Then the centers are at least 20 m apart at each tick, less the tolerance of 1 µm that the audit (`internal/sim/bank_geometry.go:174`) and the observer (`internal/sim/safety.go:206`) both permit.
```

Insert these lines after `:218`, the end of section 4.2:

```markdown
When the network sets `shapeClearance`, the set of locations of a pod is its envelope (`internal/sim/envelope_safety.go`), and the claim holds for each pair except two kinds.
A merge or split pair of case B, and a shape pair of case C, keep the side-by-side distance of the classes of the two pods instead, by section 3.4 of the [shape clearance](../design/shape-clearance.md) design.
Case A does not change, and sections 4.4 and 4.5 each give a branch for the field.
Amended <date>, by maintainer decision: the claim of the 20 m proof under the `shapeClearance` network field.
```

Current text, `docs/contracts/express-standees-contract-proposal.md:239` to `:243`:

```markdown
`buildJunctionConflicts` (`internal/sim/junction_conflicts.go:29`) compares each pair of lanes at each node, with a clearance of 20 m when one mask admits a large class, plus a pad of 0.5 m.
It finds an interval on each lane, and each point outside it is more than 20 m from each point of the other lane.
Each cell that overlaps the interval holds the junction resource of the node, and a pod keeps it until the cell end (`internal/sim/traffic.go:950`).
If both pods are inside their intervals, both need one resource, which has one owner.
If one pod is outside its interval, the distance is above 20 m.
```

Insert these lines after `:247`, the end of section 4.4:

```markdown
When the network sets `shapeClearance`, this case holds as written for a through pair, and for each pair at a node that is not a merge or split pair of the service contract.
A merge or split pair has the extent of the [lateral merge zones](../design/lateral-merge-zones.md) design in place of the 20 m interval: the body range with the union body, and the node range of 12 m.
Each cell that overlaps the extent holds the junction resource of the node, so the junction orders the two pods.
A pod outside its extent clears each body position of the other lane, so the grown bodies are disjoint, and the centers are at least the side-by-side distance of the two classes apart.
This holds only while the junction orders the pods, so the body-and-ownership oracle of the [shape clearance](../design/shape-clearance.md) design checks it at each tick of a merge test and of a restored continuation.
```

Current text, `docs/contracts/express-standees-contract-proposal.md:251` to `:253`:

```markdown
`validateBankGeometry` (`internal/sim/bank_geometry.go:128`) refuses a network when a large-admitting path comes within 20 m of a nonincident path, unless each pair of their locations is on two planes.
A location set holds the lane, its incident lanes, and the berths at its `From` node, so a pod near a plane join counts on both planes.
Both pods are inside their polylines, so the distance is at least 20 m.
```

Insert these lines after `:261`, the end of section 4.5:

```markdown
When the network sets `shapeClearance`, the audit takes the envelope reach of the [group physics design](../design/group-physics-design.md) at each position, and not the whole lane.
The envelope reach of a position holds each plane of the envelope of a pod at that position.
So when a location of the envelope of `x` and a location of the envelope of `y` are on one plane, their two positions are not separated.
At such positions, the audit refuses the network when the paths are less than 20 m apart, unless the two lanes are a shape pair of the service contract.
For a shape pair, the sweep proves that the grown bodies are disjoint at each such pair of positions, so the centers are at least the side-by-side distance of the two classes apart.
A pair with a berth is not a shape pair, so it keeps 20 m at such positions.
```

Current text, `docs/contracts/express-standees-contract-proposal.md:294` to `:296`:

```markdown
   The front is a maximum over the route, so one long-tail curve can make the window reach two lanes ahead, and the audit does not cover that case.
   One scratch probe reported 19.996 m there, with the body far from that lane.
   It looks like a false alarm, this is not proved, and the maintainer chose to record it and not change the observer.
```

Insert this line after `:296`:

```markdown
   When the network sets `shapeClearance`, the envelope reach of the audit holds the largest front bound of each route through a lane, so the audit covers that case.
```

These branches follow the design.
Part 2 keeps the junction extents of through pairs (design `:466`), and the lateral merge zones design keeps the isotropic rule for each pair that fails a clause (`lateral-merge-zones.md:77` to `:79`).
Section 3.4, items 2 and 3, give the arguments for merge and split pairs and for shape pairs.
SC9 gives the containment that the case C branch and the item 4 line use.
Note of 2026-11-16: amendment ST11 of the [shared-track amendment proposal](shared-track-amendment-proposal.md) replaces the line at `:487` in the standee contract, because the oracle takes the local front under the field.
Note of 2026-11-16: the [second shared-track amendment proposal](shared-track-amendment-2-proposal.md) replaces the local front and the reach bound with the anchor front of each lane and an oracle that reads the compiled envelope reach, by amendments ST16, ST17, and ST20.

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
Clauses 1, 2, 4, and 5 stay.
The body of each lane is the union of the bodies of the classes that the lane admits, and a pair keeps the side-by-side distance of the classes of its two pods.
The node range stays 12 m, because the union body changes only the body range.
Amended <date>, by maintainer decision: the rule for each class, behind the `shapeClearance` network field.
```

The worktree of `/tmp/build/podsim/docfix/wt` edits this file, so the line numbers can move before the paste.
Use the quoted text as the anchor.

### SC15: Station banks document, audit text

Section 5 does not name this document.
It is a guide and not a contract, but it holds the approved amendment B1, and the [bank load choice](bank-load-choice-proposal.md) proposal asked for approval of a change to its rule text.
This proposal recommends that SC15 is part of this approval batch.
The maintainer can approve it as an amendment or as a documentation change of the build.
In both cases, the station banks document must agree with SC1 and SC9 before the build hands back.

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
For a pair with a large class, the locations of a lane are then the envelope reach at each position on the lane, as the [group physics design](design/group-physics-design.md) defines it, and not the whole lane with its incident lanes.
The locations of a berth do not change.
Two lanes that share no node are a shape pair when the shape certification of the [service contract](contracts/service-contract-proposals.md) proves them, and the audit does not refuse a shape pair.
Each other pair needs 12 meters, or 20 meters when either path admits a large class, at each pair of positions that are not separated.
A pair with a berth is not a shape pair.
The certified pairs and the skip rules of the maneuver groups, the depot lateral pairs, and the retention rules come first and do not change.
The order of the refusals and their text do not change.
```

Note of 2026-11-16: amendment ST13 of the [shared-track amendment proposal](shared-track-amendment-proposal.md) replaces the line at `:550` in the station banks document, because a berth also holds the clusters of rule 2 under the field.

### SC16: Conditions on the earlier rule text

After SC1 to SC15 were applied, a check found earlier sentences that state the rule without the field and have no condition.
The field branches follow them, but each one read as unconditional.
SC16 adds the condition only, and changes no rule.
The maintainer approved it on 2026-11-16, subject to a review that finds no rule change.
The review found that the first text of the Express row also changed the 20 m retention, so the row now keeps it, and the maintainer approved the fixed text.
Line references are for `d9232d88`.

Current text, `docs/contracts/express-standees-contract-proposal.md:268`:

```markdown
It reads whole lanes, so it refuses a pass that is far from the join on a lane incident to the shared plane.
```

Replace with:

```markdown
When the network does not set `shapeClearance`, it reads whole lanes, so it refuses a pass that is far from the join on a lane incident to the shared plane.
```

Current text, `docs/contracts/express-standees-contract-proposal.md:387`:

```markdown
- The 20 m clearance, the 6 m envelope, the 40 m lane minimum, and the 20 m cell minimum.
```

Replace with:

```markdown
- The 20 m clearance, except for the merge, split, and shape pairs of a network that sets `shapeClearance` (sections 2.3 and 4.2), the 6 m envelope, the 40 m lane minimum, and the 20 m cell minimum.
```

Current text, `docs/contracts/express-standees-contract-proposal.md:389`:

```markdown
- The audit of nonincident paths and the junction clearance.
```

Replace with:

```markdown
- The audit of nonincident paths and the junction clearance, with the branches of sections 4.4 and 4.5 when the network sets `shapeClearance`.
```

Current text, `docs/contracts/express-physical-encoding-contract-proposal.md:35`:

```markdown
| Large interaction candidate | Existing 6 m envelope radius, 20 m separation and retention, lanes at least 40 m, actual cells at least 20 m |
```

Replace with:

```markdown
| Large interaction candidate | Existing 6 m envelope radius, 20 m separation (the side-by-side distance for merge, split, and shape pairs when the network sets `shapeClearance`), and 20 m retention, lanes at least 40 m, actual cells at least 20 m |
```

Current text, `docs/design/lateral-merge-zones.md:96`:

```markdown
The center distance that two disjoint grown bodies keep is `2 × 1.05 + 1 = 3.1 m`, which is `lateralClearance(0)` (`depot_lateral.go:34`).
```

Replace with:

```markdown
The center distance that two disjoint grown bodies keep is `2 × 1.05 + 1 = 3.1 m`, which is `lateralClearance(0)` (`depot_lateral.go:34`).
When the network sets `shapeClearance`, the union body and the side-by-side distance of the two classes replace this body and this distance, as the amended clause 3 states.
```

Current text, `docs/design/lateral-merge-zones.md:153`:

```markdown
| Express contract, 20 m and actual paths (`express-physical-encoding-contract-proposal.md:206`, `:217`) | Holds, because clause 3 excludes large pairs. |
```

Replace with:

```markdown
| Express contract, 20 m and actual paths (`express-physical-encoding-contract-proposal.md:206`, `:217`) | Holds without `shapeClearance`, because clause 3 excludes large pairs. With the field, amendment SC4 of the [shape clearance amendments](../contracts/shape-clearance-amendment-proposal.md) changes the 20 m rule. |
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
  Note of 2026-11-16: under the field, decision S1 of the shared-track design changes the envelope, by amendment ST4 of the [shared-track amendment proposal](shared-track-amendment-proposal.md).
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
9. Service contract `:51` to `:52`, and incident suspension contract section 7.2: section 5 adds the shape partners only.
   Part 3 applies the lateral merge rule, with its debris partners, to each class, so SC3 and SC10 also add the merge and split partners.

Each other line number and quote of section 5 matches `212c1b68`: the service contract `:46`, `:50`, and `:51` to `:52`, the Express physical contract `:206`, `:207`, and `:219`, the group physics design `:30`, and the tick-rate contract `:50`.

## Documents that section 5 omits

These documents need an amendment:

- Express standees contract, sections 2.3, 4.2, 4.4, 4.5, and 4.7 (SC13).
  Section 2.3 (`:73`) keeps the 20 m pair clearance with no condition.
  The claim of section 4.2 (`:212` to `:214`) proves 20 m for each pair of a large pod on one plane, with location sets of whole lanes.
  Case B (`:239` to `:243`) uses the 20 m junction extent for each pair at a node, and case C (`:251` to `:258`) uses the whole-lane audit.
  Item 4 of section 4.7 (`:294` to `:296`) records the gap of the front bound.
  Under D5 and D1-B, merge, split, and shape pairs keep less than 20 m.
- Lateral merge zones design, clause 3 (SC14).
  It is approved text, and part 3 of the design drops clause 3 when the field is set.
- Station banks document, the audit text (SC15).
  `:60` limits the audit to depot pairs when the network has a depot and no bank, and D12-A changes this when the field is set.
  `:56` gives a lane the whole-lane locations, and D1-B changes this when the field is set.

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

None.
The first version asked seven questions, and the design answers each of them:

1. Front bound of the envelope planes: SC9 takes the largest front bound of each route, by section 3.4, item 5, and the envelope reach test of section 7.
   Note of 2026-11-16: decision S1 of the shared-track design replaces this answer with the reach bound of each lane and the local front of the oracle, by amendment ST3 of the [shared-track amendment proposal](shared-track-amendment-proposal.md).
   Note of 2026-11-16: the [second shared-track amendment proposal](shared-track-amendment-2-proposal.md) replaces the local front and the reach bound with the anchor front of each lane and an oracle that reads the compiled envelope reach, by amendments ST16, ST17, and ST20.
2. Node range of a merge or split pair with a large lane: 12 m, in SC1 and SC14, by section 3.4, item 2.
3. Debris partners: merge, split, and shape partners, in SC3 and SC10, by part 3 and section 3.5.
4. D6 oracle in a plain project: required, in SC1, by D2 and section 7.
5. Rows of the shared-track preset: a deferred deliverable (see below).
6. Station banks document: an approval choice, not a design gap.
   The recommendation is to approve SC15 in this batch, and the document must agree before the build hands back.
7. Amendment notes: an editorial choice.
   SC8, SC9, and SC14 add dated notes, as the contracts have, so that each change can be traced.

## Deferred deliverables

- Rows of the shared-track preset.
  Section 5.1 of the design says that the preset "Needs its own row, or E1 extended to name it".
  The preset has no name or geometry yet, so this proposal writes no row for it.
  A separate qualification proposal gives its geometry and each row of section 5.1 that applies, before the preset claims qualification.

## Approval

| ID | Document | Change |
| --- | --- | --- |
| SC1 | Service contract | The precedence of the current exceptions, following, merge, split, and shape pairs of each class, separated positions, shape certification for lane pairs, the side-by-side distances, and the D6 oracle for each class. |
| SC2 | Service contract | The guideway-bound assumption at line speed for merge, split, and shape pairs. |
| SC3 | Service contract | Debris blocks the cells of merge, split, and shape partners within 12 meters. |
| SC4 | Express physical contract | The side-by-side distance for lateral pairs with a large pod, and 20 m for each other pair. |
| SC5 | Express physical contract | Union body for the static extents of lateral pairs. |
| SC6 | Express physical contract | Physical geometry gate: the class body for lateral pairs, and the sweep cases. |
| SC7 | Express physical contract | Independent safety oracle gate: shape pairs and the body-and-ownership oracle. |
| SC8 | Group physics design | The side-by-side distance for lateral pairs, 20 m for each other pair, and the union body. |
| SC9 | Group physics design | The envelope reach by position, with its containment test, and the end of the front bound gap for networks with the field. |
| SC10 | Incident suspension contract | Merge, split, and shape partners are lateral neighbors of the debris footprint. |
| SC11 | Tick-rate contract | Section 1.5 lines for merge, split, and shape pairs, with no time term. |
| SC12 | Express London qualification | D keeps its evidence only with a behavior parity test. |
| SC13 | Express standees contract | The proof without the field stays, and cases B and C get branches for the field. |
| SC14 | Lateral merge zones design | Clause 3 does not apply when the field is set, and the node range stays 12 m. |
| SC15 | Station banks document | The audit text for the field. |
| SC16 | Standee contract, Express physical contract, lateral merge zones design | The condition `shapeClearance` on earlier sentences that state the rule without the field. |

Dependencies:

- One field sets parts 1 to 5 of the design together (section 6.1).
  So each of SC1 to SC11, SC13, SC14, and SC15 states a part of one rule, and the build of the field needs all of them.
  If the maintainer approves only some, a binding text contradicts the behavior of the field.
- SC1 defines the pairs, the separated positions, and shape certification.
  SC9 defines the envelope reach that SC1 uses.
  So SC1 and SC9 go together, and SC4, SC5, SC8, SC10, SC11, SC13, SC14, and SC15 use their terms.
- SC2 is the assumption that SC1 needs for shape pairs at line speed, so SC1 and SC2 go together.
- SC3 and SC10 state one debris rule in two contracts, so they go together.
- SC1 includes the D5 merge pairs, which need the D6 oracle (section 6.1).
  SC1 states that oracle for each class, and SC7 puts it in the Express gate, so SC1 and SC7 go together.
- SC6 and SC7 share one new subsection of the Express physical contract.
- SC12 does not depend on the others, and the maintainer can approve it alone.
  Its condition on evidence still applies to the build of the field.
- This proposal recommends that SC15 is part of this batch.
  As an amendment or as a documentation change, the station banks document must agree with SC1 and SC9 before the build hands back.

## Review

An adversarial review checked the first version of this proposal on 2026-11-16.
It found 6 major problems and 1 minor problem.
This revision checked each finding against the design and the code at `212c1b68`, and folds all seven, one of them in part.

1. Major: SC9 kept 20 m for each pair that fails the shape test, small pairs included.
   Design `:475` gives 12 m or 20 m.
   Folded: SC1 gives the fallback for two lanes, and SC9 and SC15 give the same rule for paths, berths included.
   SC4, SC8, and SC13 (`:73`) said that 20 m applies to a following pair, which can read as "only to a following pair".
   They now say that each pair that is not a merge, split, or shape pair keeps 20 m.
2. Major: the shape pair definitions disagreed about planes and sweep scope.
   SC1 said "two lanes on one plane" with disjoint swept regions, and part 1 applies part 4 only where the envelope reaches overlap (design `:462`).
   Folded: SC1 defines separated positions and shape certification once, and SC9, SC11, SC13, and SC15 refer to it.
   The plane exemption holds at separated positions.
3. Major: the precedence of the current exceptions was missing.
   A certified depot lateral pair can be two lanes that share no node (`depot_lateral.go:8`, design `:274`), and the audit applies maneuver certificates and skip rules first (`bank_geometry.go:154`).
   Folded: SC1, SC9, and SC15 state the precedence of design section 3.7.
   SC1 lists clauses 1, 2, 4, and 5 of the lateral merge zones design (`lateral_merges.go:42` and `:141`).
4. Major: SC9 copied the lane-local front bound and the one-node reach of the probe (design `:142`).
   The code takes the largest front bound of the route and tests each route lane against its own tail (`envelope_safety.go:50` to `:69`).
   Folded: SC9 states the containment of section 3.4, item 5, and the test of design `:810`, with multi-lane reach and the unreleased origin.
   SC9 and SC13 close the recorded front bound gap for networks with the field.
5. Major: SC13 kept sections 4.4 and 4.5 of the standee contract, whose premises change with the field.
   `:239` gives each pair at a node a 20 m extent, and `:251` uses the whole-lane audit.
   Folded: the proof without the field stays, and sections 4.4 and 4.5 get branches for the field.
6. Major: SC9 and SC15 used "paths", which includes berths (`bank_geometry.go:111`), and the design sweeps lane bodies only (`:473`).
   Folded in part: shape certification is for two lanes, and a pair with a berth keeps its current rule.
   The review also asked for a scope question about berth pairs.
   This revision adds none, because the design defines no berth body and its side-by-side platform layout uses lanes (`:661`).
7. Minor: the open questions mixed design gaps with settled answers and editorial choices.
   Folded: "Open questions" lists where each answer now is, the preset rows are a deferred deliverable, and the proposal recommends SC15 in this batch.

A confirmation review of the revision on 2026-11-16 found the seven findings resolved, and found one new major problem.
In SC8, "Each other pair keeps this distance" followed the sentence on the side-by-side distances, so it could give a following pair 3.55 m or 4.0 m.
Fixed: each other pair with a large pod keeps the 20 meter clearance.
