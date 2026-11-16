# Second shared-track amendment proposal

Status: approved by the maintainer on 2026-11-16, and applied.
The maintainer approved ST16 to ST23, and then ST24, which changes the replacement texts of ST16 and ST18 before they are applied.
Applied on 2026-11-16: the text of each ST item is in its target document, with the ST24 versions of the texts of ST16 and ST18, and each `<date>` is 2026-11-16.
Files changed: `docs/design/group-physics-design.md` (ST16 with ST24), `docs/contracts/service-contract-proposals.md` (ST17), `docs/design/shape-clearance.md` (ST18 with ST24, and ST19), `docs/contracts/express-standees-contract-proposal.md` (ST20), `docs/contracts/shape-clearance-amendment-proposal.md` (ST21 notes), `docs/contracts/express-shared-track-qualification-proposal.md` (ST22), `docs/contracts/shared-track-amendment-proposal.md` (ST23 note), `docs/design/shared-track-preset.md`, and `README.md`.
The "Amended through" lines of the service contract and the standee contract already gave November 16, 2026.
The text below records the text as approved.

## Summary

The [shared-track amendment proposal](shared-track-amendment-proposal.md) applied the text of decision S1 of the [shared-track](../design/shared-track-preset.md) design on 2026-11-16.
That text defines a local front in the oracle, a recursive bound `F(L)`, a seed of lanes behind, and a reach bound of each lane.
A code review of slice 1 found two blockers in the seed and the bound (section 10 of the design).
On 2026-11-16, the maintainer chose another shape, which section 3.1 of the design now gives:

- The anchor front of a lane L is `B(L) = max(6 m, front(L))`, where `front(L)` includes rule 1.
  It does not take the front of another lane.
- The index is compiled one time, at preparation.
  The envelope reach of a published position is the union of the envelopes of each anchor within the slack of it, also across nodes, each with its own anchor front.
  Each bound rounds outward.
- The oracle reads the span of the index at the published position of the pod, and selects the route lanes, the origin unit before the origin release, and the berth of the pod.
  Rule 2 applies after the selection.

The oracle is then in the index by construction.
A1, rules 1 and 2, the physical proof, the small-pod exclusion, and D6 do not change.
Several applied texts now state the local front, `F(L)`, the seed, or the reach bound, so they are false under the field.
This proposal quotes each of them and gives its replacement.
Without the field, no text changes, and project D keeps its envelope.

## Conventions

- Each line reference is for `5d8afa00`, with the pending change of 2026-11-16 to the seed (ST3 note below) in the working tree.
- Each amendment quotes the current text exactly, in a fenced block, with its lines.
  The replacement text is also in a fenced block.
- `<date>` is the date of the approval, in the ISO form of the notes.
  The "Amended through" line uses the long form.
- The numbers continue after ST15 of the first proposal.
- Each changed rule applies only when the network sets `shapeClearance`, as in the first proposal.
- ST16 defines the anchor front, the anchors of a published position, and the selection.
  The other amendments refer to these definitions.

## Amendments

### ST16: Group physics design, envelope and reach under the field

Current text, `docs/design/group-physics-design.md:86` to `:98`:

```markdown
When the network sets `shapeClearance` and the fleet has a large pod, the envelope of a pod takes a local front in place of the largest front bound of the route.
At a route distance x, the local front is the least value that is at least 6 meters, and at least the front bound of each route lane that ends at x minus the slack or later and starts at x plus the local front or earlier.
The slack is `envelopeReachSlack`, 1 millimeter.
Rule 1: a curved lane with no geometry tail has the front bound `min(6 / (c (1 - conflictSlack)), 16.2 meters)`, where c is the minimum projection of a segment of the lane on its chord, and 16.2 meters when c is 0 or less.
Rule 2: when an envelope holds a lane of a maneuver group, it also holds each lane at a node of the cluster of that lane.
A cluster is a maneuver group with each group that shares a node with it, and so on.
Rule 2 applies to the envelope of a traveling pod, of a pod before its origin release, and of a pod at a berth.
The reach includes the lanes ahead on the route, up to the reach bound of the lane, also more than one lane ahead.
For a lane L, `F(L)` is the least value that is at least 6 meters and at least the front bound of L, and at least the front bound of each lane that starts at a node within `F(L)` of the To node of L.
This distance is the shortest walk along lanes, with the slack.
The reach bound of L is the largest of `F(L)` and of `F(P)` for each lane P of the seed of L.
The seed of L holds each lane whose To node is less than two slacks from the From node of L, by the shortest walk back along lanes.
The reach bound is at most the largest front bound of the network.
```

Replace `:86` to `:98` with:

```markdown
When the network sets `shapeClearance` and the fleet has a large pod, the envelope of a pod comes from the envelope reach, and not from the largest front bound of the route.
The slack is `envelopeReachSlack`, 1 millimeter.
Rule 1: a curved lane with no geometry tail has the front bound `min(6 / (c (1 - conflictSlack)), 16.2 meters)`, where c is the minimum projection of a segment of the lane on its chord, and 16.2 meters when c is 0 or less.
Rule 2: when an envelope holds a lane of a maneuver group, it also holds each lane at a node of the cluster of that lane.
A cluster is a maneuver group with each group that shares a node with it, and so on.
Rule 2 applies to the envelope of a traveling pod, of a pod before its origin release, and of a pod at a berth.
The anchor front of a lane L is the larger of 6 meters and the front bound of L, with rule 1.
It does not take the front bound of another lane.
An anchor on L holds L, each lane behind up to its retained tail, the origin berths and the lanes at their nodes up to the origin tail, and each lane that starts within the anchor front of L ahead of the position, by the shortest walk along lanes.
Each bound has the slack, and rounds outward.
The reach of a published lane position is the union of the planes of each anchor within the slack of that position, also across one or more nodes, each anchor with its own anchor front.
The reach is compiled once, when the network is prepared.
The envelope of a traveling pod is the part of the reach at its published lane position that holds the lanes of its route, and the origin berth and the lanes at its node before the origin release.
The envelope of a pod at a berth is the reach of the berth.
Rule 2 then applies to the envelope.
```

`:99` to `:100` stay.

Current text, `docs/design/group-physics-design.md:114`:

```markdown
In such a network, the oracle takes the local front, so a front bound of a route lane that ends before the route distance of the pod minus the slack, or that starts past the local front, does not extend the route window.
```

Replace `:114` with:

```markdown
In such a network, the oracle takes its envelope from the reach at the published lane position, so the front bound of a route lane extends the route window only from an anchor on that lane.
```

Current text, `docs/design/group-physics-design.md:118`:

```markdown
Amended 2026-11-16, by maintainer decision: the local front of the envelope, the small-pod rules, and the reach bound of each lane, behind the `shapeClearance` network field.
```

Insert this line after `:118`:

```markdown
Amended <date>, by maintainer decision: the anchor front of each lane, the compiled reach, and the envelope that reads the reach, in place of the local front and the reach bound, behind the `shapeClearance` network field.
```

Reason: the local front, `F(L)`, the seed, and the reach bound are removed.
The oracle no longer has a route window of its own: it reads the reach, so the reach holds each plane of the envelope by construction.
The sentence at `:115` stays true.
`:97` is the wider seed that the maintainer approved on 2026-11-16 after the first blocker, and the new shape replaces it too.

### ST17: Service contract, envelope of the oracle

Current text, `docs/contracts/service-contract-proposals.md:75`:

```markdown
  When the fleet has a large pod, the oracle keeps an envelope of planes for each pod, with the local front and the small-pod rules of the [group physics design](../design/group-physics-design.md).
```

Replace `:75` with:

```markdown
  When the fleet has a large pod, the oracle keeps an envelope of planes for each pod, from the envelope reach at the published lane position, with the small-pod rules of the [group physics design](../design/group-physics-design.md).
```

Insert this line after `:90`:

```markdown
  Amended <date>, by maintainer decision: the oracle envelope from the compiled envelope reach, in place of the local front, behind the `shapeClearance` network field.
```

The "Amended through" line at `:4` changes to the approval date.

Reason: the oracle no longer takes a local front.
`:76` to `:79` stay true: the exemption rule does not change, the envelope holds each lane under the body, and it can drop a plane of the largest front bound of the route.

### ST18: Shape clearance design, front of the envelope and records of the index

Current text, `docs/design/shape-clearance.md:162`:

```markdown
With the field, the envelope takes the local front of the [group physics design](group-physics-design.md).
```

Replace `:162` with:

```markdown
With the field, the envelope comes from the envelope reach at the published lane position, with the anchor front of each lane of the [group physics design](group-physics-design.md).
```

Current text, `docs/design/shape-clearance.md:167`:

```markdown
When the network sets `shapeClearance`, the index of decision S1 of the [shared-track](shared-track-preset.md) design takes the reach bound of each lane of the [group physics design](group-physics-design.md) in place of this bound.
```

Replace `:167` with:

```markdown
When the network sets `shapeClearance`, the index of decision S1 of the [shared-track](shared-track-preset.md) design takes the anchor front of each lane of the [group physics design](group-physics-design.md) in place of this bound.
```

Current text, `docs/design/shape-clearance.md:245`:

```markdown
When the network sets `shapeClearance`, the index of decision S1 of the shared-track design takes the reach bound of each lane, and on this network it gives the 7 refusals of the next line (section 2.2 of that design).
```

Replace `:245` with:

```markdown
When the network sets `shapeClearance`, the index of decision S1 of the shared-track design takes the anchor front of each lane, and on this network it gives the 7 refusals of the next line and 11 more (section 2.2 of that design).
The 11 more pairs are the poses of 3 of the 7 at a node, where the anchors within the slack take a lane of a diverge or a merge next to the route.
```

Reason: the reach bound is removed.
The study of section 8 of the shared-track design measured 18 refusals, 6 under 4.0 m, on this network with the new index.
Without the union of the anchors at the nodes, it gives the 7 refusals again, so the union is the only cause.

### ST19: Shape clearance design, `pair`, `largeSafety`, and section 3.4, item 5

Current text, `docs/design/shape-clearance.md:544`:

```markdown
   When the network sets `shapeClearance`, the envelope takes the local front and the small-pod rules of the [group physics design](group-physics-design.md).
```

Replace `:544` with:

```markdown
   When the network sets `shapeClearance`, the envelope comes from the envelope reach, with the small-pod rules of the [group physics design](group-physics-design.md).
```

Current text, `docs/design/shape-clearance.md:564`:

```markdown
| `pair`, `safety.go:261` | Under D1-B, when the field is set, the envelopes take the local front and the small-pod rules of the [group physics design](group-physics-design.md), by decision S1 of the [shared-track](shared-track-preset.md) design. Under D1-A, large pods use their lane location. A shape pair or a merge pair from the tables needs the side-by-side distance of its two classes. |
```

Replace `:564` with:

```markdown
| `pair`, `safety.go:261` | Under D1-B, when the field is set, the envelopes come from the envelope reach, with the small-pod rules of the [group physics design](group-physics-design.md), by decision S1 of the [shared-track](shared-track-preset.md) design. Under D1-A, large pods use their lane location. A shape pair or a merge pair from the tables needs the side-by-side distance of its two classes. |
```

Current text, `docs/design/shape-clearance.md:565`:

```markdown
| `largeSafety`, `envelope_safety.go:13` | Under D1-B, when the field is set, `appendVehicleSafetyLocations` and its reference construction take the local front and the small-pod rules. Without the field, no change. Under D1-A, not used for the plane test when the field is set. |
```

Replace `:565` with:

```markdown
| `largeSafety`, `envelope_safety.go:13` | Under D1-B, when the field is set, `appendVehicleSafetyLocations` reads the compiled envelope reach, and its reference construction computes the same envelope from the definition, with the small-pod rules. Without the field, no change. Under D1-A, not used for the plane test when the field is set. |
```

The shape clearance design gets this line after its status line at `:4`:

```markdown
Amended on <date>, by maintainer decision: the envelope of decision S1 from the compiled envelope reach, in amendments ST18 and ST19 of the [second shared-track amendment proposal](../contracts/shared-track-amendment-2-proposal.md).
```

Reason: the local front is removed.
`:545` and `:546` stay true.

### ST20: Standee contract, front bound gap

Current text, `docs/contracts/express-standees-contract-proposal.md:313` to `:316`:

```markdown
   When the network sets `shapeClearance`, the oracle takes the local front of the [group physics design](../design/group-physics-design.md), and not the largest front bound of the route.
   A front bound of a route lane that ends before the route distance of the pod minus the slack, or that starts past the local front, does not extend the window.
   The envelope reach of the audit holds the window of this changed oracle, so the audit covers it.
   Amended 2026-11-16, by maintainer decision: the local front of the oracle envelope, behind the `shapeClearance` network field.
```

Replace `:313` to `:315` with:

```markdown
   When the network sets `shapeClearance`, the oracle takes its envelope from the envelope reach of the [group physics design](../design/group-physics-design.md), and not from the largest front bound of the route.
   The front bound of a route lane extends the window only from an anchor on that lane.
   The audit takes the same envelope reach, so it holds each plane of the oracle, and the audit covers the window.
```

`:316` stays.
Insert this line after `:316`:

```markdown
   Amended <date>, by maintainer decision: the oracle envelope from the compiled envelope reach, behind the `shapeClearance` network field.
```

The "Amended through" line at `:5` changes to the approval date.

Reason: the local front is removed, and the containment now holds by construction.

### ST21: Shape clearance amendment proposal, notes

Current text, `docs/contracts/shape-clearance-amendment-proposal.md:96`:

```markdown
Note of 2026-11-16: under the field, amendment ST4 of the [shared-track amendment proposal](shared-track-amendment-proposal.md) replaces the line at `:66` in the service contract, because the oracle envelope takes the local front and the small-pod rules.
```

Current text, `docs/contracts/shape-clearance-amendment-proposal.md:323` to `:324`:

```markdown
The route window of the oracle takes the local front, and not the largest front bound of the route (`:282`, `:302`, and `:315`).
The reach takes the reach bound of each lane (`:294`), and a berth also holds the clusters of rule 2 (`:296`).
```

Current text, `docs/contracts/shape-clearance-amendment-proposal.md:501`:

```markdown
Note of 2026-11-16: amendment ST11 of the [shared-track amendment proposal](shared-track-amendment-proposal.md) replaces the line at `:487` in the standee contract, because the oracle takes the local front under the field.
```

Current text, `docs/contracts/shape-clearance-amendment-proposal.md:755`:

```markdown
   Note of 2026-11-16: decision S1 of the shared-track design replaces this answer with the reach bound of each lane and the local front of the oracle, by amendment ST3 of the [shared-track amendment proposal](shared-track-amendment-proposal.md).
```

These lines are dated records, so they stay.
Insert this line after each of `:96`, `:324`, `:501`, and `:755`, with the same indentation as the line before it:

```markdown
Note of <date>: the [second shared-track amendment proposal](shared-track-amendment-2-proposal.md) replaces the local front and the reach bound with the anchor front of each lane and an oracle that reads the compiled envelope reach, by amendments ST16, ST17, and ST20.
```

Reason: the notes describe the shape of the first proposal, which the change removes.

### ST22: Express shared-track qualification, SX3

Current text, `docs/contracts/express-shared-track-qualification-proposal.md:44`:

```markdown
  Under decision S1, the indexed and the reference envelope constructions take the local front and the small-pod rules, and they give the same locations in the same order.
```

Replace `:44` with:

```markdown
  Under decision S1, the indexed construction reads the compiled envelope reach, and the reference construction computes the envelope again from the definition of the [group physics design](../design/group-physics-design.md), with the small-pod rules.
  The reference reads no span, item, or bound of the compiled reach.
  The two constructions give the same locations in the same order.
```

Insert this line after `:5`:

```markdown
Amended <date>, by maintainer decision: SX3 compares the compiled envelope reach with an independent reference, in amendment ST22 of the [second shared-track amendment proposal](shared-track-amendment-2-proposal.md).
```

Reason: the oracle reads the index, so a parity of the oracle with the index proves nothing.
The reference must reconstruct the envelope without the code that it checks.

### ST23: First shared-track amendment proposal, seed note

Current text, `docs/contracts/shared-track-amendment-proposal.md:7` to `:11`:

```markdown
Note of 2026-11-16: a code review of slice 1 found a case with a lane of a maneuver group shorter than the slack, where the seed of ST3 misses a lane behind that lane.
The maintainer approved a wider seed on 2026-11-16, and the applied text of ST3 in `docs/design/group-physics-design.md` changed.
The old sentence was "The reach bound of L is the largest of `F(L)` and of `F(P)` for each lane P into the From node of L."
The new sentences are "The reach bound of L is the largest of `F(L)` and of `F(P)` for each lane P of the seed of L." and "The seed of L holds each lane whose To node is less than two slacks from the From node of L, by the shortest walk back along lanes."
The ST3 text below records the text as first approved.
```

Insert this line after `:11`:

```markdown
Note of <date>: the [second shared-track amendment proposal](shared-track-amendment-2-proposal.md) replaces the seed, the reach bound, and the local front of ST3 to ST7, ST11, ST12, ST14, and ST15, by amendments ST16 to ST22.
```

Reason: the wider seed did not hold at an exact slack bound, the second blocker of the code review.
The record stays, and the note points to the shape that replaces it.

### ST24: Paths through the published lane

Status: approved by the maintainer on 2026-11-16, and applied with ST16 to ST23.
ST24 changes two replacement texts of ST16 and ST18, before they are applied.

The maintainer chose on 2026-11-16 to remove the 11 refusals of ST18 in slice 1, by the route provenance of the anchors at the nodes.
Section 3.1 of the design, "Paths through the published lane", gives the rule and its soundness argument.
A pod published on a lane L has L on its route, and its anchor, the route distance, is on the route within the slack of the published position.
The gap is 0 after each publication, at most 2e-6 m after a restore, and at most 2.21e-4 m at the end of a route, where the pod does not publish its last step (section 3.1 of the design).
So the anchors across a node take only the paths through L: the walk of an anchor on the side of L starts at the far end of L, and does not branch at a node within the slack of L.
The rule uses only the published lane, so the audit uses it too, and the 11 refusals fall.

Replacement text of ST16, the sentence of the reach:

```markdown
The reach of a published lane position is the union of the planes of each anchor within the slack of that position, also across one or more nodes, each anchor with its own anchor front.
```

Change it to:

```markdown
The reach of a published lane position is the union of the planes of each anchor within the slack of that position, also across one or more nodes, each anchor with its own anchor front.
An anchor on another lane holds only the planes along the paths through the published lane: its walk on the side of the published lane starts at the far end of that lane, with the distance through it.
```

Replacement text of ST18 for `docs/design/shape-clearance.md:245`:

```markdown
When the network sets `shapeClearance`, the index of decision S1 of the shared-track design takes the anchor front of each lane, and on this network it gives the 7 refusals of the next line and 11 more (section 2.2 of that design).
The 11 more pairs are the poses of 3 of the 7 at a node, where the anchors within the slack take a lane of a diverge or a merge next to the route.
```

Change it to:

```markdown
When the network sets `shapeClearance`, the index of decision S1 of the shared-track design takes the anchor front of each lane, and on this network it gives the 7 refusals of the next line (section 2.2 of that design).
```

Reason of ST18:

```markdown
Reason: the reach bound is removed.
The study of section 8 of the shared-track design measured 18 refusals, 6 under 4.0 m, on this network with the new index.
Without the union of the anchors at the nodes, it gives the 7 refusals again, so the union is the only cause.
```

Change it to:

```markdown
Reason: the reach bound is removed.
The study of section 8 of the shared-track design measured 7 refusals, 5 under 4.0 m, on this network with the new index.
```

The ST18 row of the approval table, "the new refusal count", then refers to the count of 7.

Reason: with the paths through the published lane, the study gives 7 refusals, 5 under 4.0 m, and 39 shape pairs on this network, and 14 refusals with 14 shape pairs on the ramp network.
On the study networks, the oracle holds the same number of planes as before the rule.
The oracle can lose a lane that only the removed walks reached, but such a lane is not within the tail or the front of the anchor along the route.
The other texts of ST16 to ST23 stay true.

## Approved texts that stay true

- Group physics design, `:83` to `:85`: the reach holds each plane of the envelope of each route through the lane.
  The oracle reads the reach, so this holds by construction.
- Group physics design, `:99` to `:100`: rule 2 in the reach, and the origin berth before the origin release.
- Group physics design, `:101` to `:109`: the test and the proof under A1.
  The proof now takes the anchor front of the lane of the pod only (section 3.1 of the design).
- Group physics design, `:115`: the gap of the route window stays only in a network without the field.
- Service contract, `:76` to `:79`: the plane exemption rule, A1, and the dropped planes of the route maximum.
- Shape clearance design, `:545` to `:546`: the audit takes the envelope reach by position.
- Shape clearance design, `:784`, the D1 row: S1 drops planes of the route maximum that are not under the body.
- Express shared-track qualification, the other SX rows.

## Texts outside the design list

- `README.md:170` describes the first proposal as "a local front and a reach bound for each lane".
  It is a guide, so it changes with the build and needs no approval.
- The comments of `internal/sim/envelope_reach.go` and `internal/sim/envelope_safety.go` change with slice 1.

## Approval

| ID | Document | Change |
| --- | --- | --- |
| ST16 | Group physics design | The anchor front, the compiled reach, and the envelope that reads it, in place of the local front, `F(L)`, the seed, and the reach bound, under the field. |
| ST17 | Service contract | The oracle envelope from the reach, under the field. |
| ST18 | Shape clearance design | The front of the envelope, and the records of the index with the anchor front and the new refusal count. |
| ST19 | Shape clearance design | Section 3.4, item 5, and section 3.5: the envelope from the reach. |
| ST20 | Standee contract | The front bound gap under the envelope from the reach. |
| ST21 | Shape clearance amendment proposal | Dated notes on the records of the first proposal. |
| ST22 | Express shared-track qualification | SX3 with an independent reference construction. |
| ST23 | First shared-track amendment proposal | A dated note on the seed record. |
| ST24 | ST16 and ST18 of this proposal | The anchors across a node take only the paths through the published lane, and the record of 7 refusals. |
