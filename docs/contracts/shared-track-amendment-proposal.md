# Shared-track amendment proposal

Status: proposed on 2026-11-16.

## Summary

Decision D11 of the approved [shape clearance](../design/shape-clearance.md) design keeps project D (`london-full-express`) as it is.
It adds a new opt-in preset, `london-full-shared-express`, with Express on the shared main-line track of link 100 between Waterloo and Bank.
The [shared-track](../design/shared-track-preset.md) design gives the preset and decisions S1 to S6, and the maintainer decided each of them.

Under S1-A, when the network sets `shapeClearance`, the envelope reach index takes a front bound for each lane, with a seed from the lanes behind.
The oracle takes a local front in place of the largest front bound of the route.
Two small-pod rules close the gaps of the small pod of a mixed pair.
The proof of section 3.1 of the design rests on axiom A1, which the maintainer accepted as a rule of the model on 2026-11-16.

Approved texts state that the envelope takes the largest front bound of the route, that the plane exemption does not change, and that a berth holds only the lanes at its node.
Under the field, these statements become false.
No approved text states A1.
So this proposal gives the text of each change, and the maintainer approves it before it is applied.
It also gives the SX qualification rows of the preset, under decision S6-A, and the E rows of D do not change.

## Conventions

- Each line reference is for `83330588`.
- Each amendment quotes the current text exactly, in a fenced block, with its lines.
  The replacement text is also in a fenced block, so that a later commit can paste it with its indentation.
- `<date>` is the date of the approval, in the ISO form of the notes, for example 2026-11-17.
  The "Amended through" line uses the long form, for example November 17, 2026.
- The service contract (`:4`) and the standee contract (`:5`) change their "Amended through" line to the approval date.
- The shape clearance design gets this line after `:3`, its status line:

  ```markdown
  Amended on <date>, by maintainer decision: rule A1 and the envelope of decision S1 of the [shared-track](shared-track-preset.md) design, in amendments ST1, ST5 to ST8, ST10, and ST15 of the [shared-track amendment proposal](../contracts/shared-track-amendment-proposal.md).
  ```

- Each changed rule applies only when the network sets `shapeClearance`.
  A network without the field, and project D, keep the current text in force.
  Some targets already sit in a branch of the field: the service contract `:75`, the group physics design `:86` and `:88`, and the standee contract `:313`.
  These amendments replace the text in place.
  Each new sentence in the other targets starts with a condition on the field.
- ST1 states A1 under the field, as each other changed rule.
- Each term has one definition.
  ST1 defines A1 and the support of a body.
  ST3 defines the local front, rule 1, rule 2, the cluster, and the reach bound.
  The other amendments refer to these definitions.

## Amendments

### ST1: Shape clearance design, rule A1

The design names no target for A1.
Its section 3.1 cites [LondonCentral](../london.md), lines 135 to 137, for the meaning of separation groups.
That document is a guide, and not approved text.
The approved text that defines planes by separation groups is section 1.1 of the shape clearance design.

Current text, `docs/design/shape-clearance.md:58` to `:59`:

```markdown
A pair is exempt when its two pods are on different planes.
Two locations are on different planes when their separation groups differ and they share no node (`safetyLocationsSeparated`, `internal/sim/safety.go:332`).
```

Insert these lines after `:59`:

```markdown
When the network sets `shapeClearance`, rule A1 of the model applies.
A1: two bodies do not overlap when each lane under each body has a nonempty separation group, each lane under one body is in a different group from each lane under the other body, and no lane under one body shares a node with a lane under the other.
The lanes under a body are its support: the connected part of the route that is under the body and holds the reference of the pod.
A part of the route that leaves the body and comes back is not in the support.
The network stores no level, so A1 is a declaration of the author, and validation does not check the groups against the geometry.
A1 also assumes that the vertical gap between two levels is enough for each admitted body.
For a body on one lane, A1 is the test of `safetyLocationsSeparated` on two locations.
For a body over a node, A1 is the reason that D1-B adds the lanes ahead and behind.
A network without the field, project D included, keeps the envelope of this section and its requirements, and none of its rules depends on A1.
Stated <date>, by maintainer decision: rule A1, which the maintainer accepted as a rule of the model on 2026-11-16 (decision S1 of the [shared-track](shared-track-preset.md) design).
```

Reason: the proof of section 3.1 of the shared-track design rests on A1, and the maintainer accepted it as a rule of the model.
The local front of ST3 depends on A1, and the local front applies only under the field.
So A1 applies only under the field, as each other changed rule does, and a network without the field keeps its envelope and the requirements of that envelope.
A1 has the same test as `safetyLocationsSeparated` (`internal/sim/safety.go:347`): each group nonempty, the groups different, and no shared node.
`(*separationIndex).pair` (`:266`) applies that test at `:277` to each pair of small pods.
The quoted line cites `:332`, the line of `safetyLocationsSeparated` at `5fe3cc0b`, the code commit of that design.

### ST2: Group physics design, route window of the front bound

Current text, `docs/design/group-physics-design.md:79` to `:80`:

```markdown
The route window of a pod can reach further than an incident lane, because the front bound is the largest `6 * projection bound / 20` of the lanes of the route, and each lane stores it beside its tail.
The audit does not cover that case, so the observer can report a pair that the audit accepted.
```

Replace `:79` with:

```markdown
When the network does not set `shapeClearance`, the route window of a pod can reach further than an incident lane, because the front bound is the largest `6 * projection bound / 20` of the lanes of the route, and each lane stores it beside its tail.
```

`:80` stays.

Reason: under the field, the route window takes the local front of ST3, and not the largest front bound of the route.
ST14 states the window of the changed oracle at `:94`.

### ST3: Group physics design, front of the envelope reach

This amendment defines the terms of S1-A.
The definitions go here because `:81` to `:96` is the branch of the field that defines the envelope reach.

Current text, `docs/design/group-physics-design.md:86`:

```markdown
It includes the lanes ahead on the route, up to the largest front bound of the lanes of the route, also more than one lane ahead.
```

Replace `:86` with:

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
The reach bound of L is the largest of `F(L)` and of `F(P)` for each lane P into the From node of L.
The reach bound is at most the largest front bound of the network.
The reach applies rule 2 to each lane and each berth that it holds.
```

`:87` to `:89` stay.
Insert these lines after `:89`:

```markdown
The [shared-track](shared-track-preset.md) design proves that the reach holds each plane of the envelope, and that the envelope holds each lane of the support of a body, under rule A1 of the [shape clearance](shape-clearance.md) design.
The proof covers a body inside a disk of 5.404 meters around the reference for a large pod, and 4.7269 meters for a small pod, with the guideway tolerance of 0.25 meters.
These disks hold the 10 meter gate body, the grown large body, and the grown small body of the shape rule, but not a body that fills the whole 6 meter envelope disk.
The proof does not cover a support that passes a maneuver group lane and then a lane with a reduced retention, both wholly in the disk.
A test puts these bodies at sampled poses on test networks.
It shows that each lane of the support is in the envelope, and that each plane of the envelope is in the reach.
The test also shifts each route distance and each published position by two `restoreTolerance` steps each way.
```

Insert this line after `:96`:

```markdown
Amended <date>, by maintainer decision: the local front of the envelope, the small-pod rules, and the reach bound of each lane, behind the `shapeClearance` network field.
```

Reason: S1-A replaces the largest front bound of the route with the reach bound in the index, and with the local front in the oracle (sections 3.1 and 6 of the design).
`:81` to `:96` apply only under the field, so a network without the field keeps the current envelope.
The scope lines carry the limits of the proof from sections 3.1 and 9 of the design.

### ST4: Service contract, envelope of the oracle

Current text, `docs/contracts/service-contract-proposals.md:75`:

```markdown
  When the fleet has a large pod, the oracle keeps the envelope of each pod, so the plane exemption of two pods does not change.
```

Replace `:75` with:

```markdown
  When the fleet has a large pod, the oracle keeps an envelope of planes for each pod, with the local front and the small-pod rules of the [group physics design](../design/group-physics-design.md).
  A pair with at least one large pod keeps the plane exemption only when each cross-pair of the two envelopes is on different planes.
  A pair of two small pods keeps the plane exemption when the locations of the two pods are on different planes, as before.
  Under rule A1 of the [shape clearance](../design/shape-clearance.md) design, the envelope holds each lane under the body, for the bodies and within the limits that the group physics design states.
  The envelope can drop a plane of the largest front bound of the route that is not under the body.
```

Insert this line after `:85`:

```markdown
  Amended <date>, by maintainer decision: the local front and the small-pod rules of the oracle envelope, and rule A1, behind the `shapeClearance` network field.
```

Reason: `:75` sits in the branch of the field that starts at `:50`.
Under S1-A, the envelope changes under the field, so the sentence "the plane exemption of two pods does not change" is false.
The rule of the exemption does not change, and the second and third lines state it.
`(*separationIndex).pair` (`internal/sim/safety.go:266`) uses the envelopes only when the class pair clearance is over 12 meters, at `:274` to `:275`, and the locations of the two pods otherwise, at `:277`.

### ST5: Shape clearance design, front of the probe and of the envelope

Current text, `docs/design/shape-clearance.md:147`:

```markdown
The probe takes the front bound of a lane from that lane only, while the envelope takes the largest front bound of the route.
```

Replace `:147` with:

```markdown
The probe takes the front bound of a lane from that lane only.
When the network does not set `shapeClearance`, the envelope takes the largest front bound of the route, as it did when the probe ran.
With the field, the envelope takes the local front of the [group physics design](group-physics-design.md).
```

Reason: the design marks the second half as true only without the field.
The line records the probe, so the replacement keeps what the envelope was when the probe ran.

### ST6: Shape clearance design, section 3.4, item 5

Current text, `docs/design/shape-clearance.md:524`:

```markdown
5. Under D1-B, the planes of each pod are those of today's envelope, so the plane exemption does not change.
```

Replace `:524` with:

```markdown
5. Under D1-B, the planes of each pod are those of its envelope.
   When the network sets `shapeClearance`, the envelope takes the local front and the small-pod rules of the [group physics design](group-physics-design.md).
   Under rule A1 of section 1.1, the envelope then holds each lane under the body, for the bodies and within the limits that the group physics design states.
   So the plane exemption keeps its meaning, and the envelope can drop a plane of the largest front bound of the route that is not under the body.
```

`:525` to `:528` stay.

Reason: under S1-A, the envelope of a network with the field is not today's envelope.
`:526` holds as written, because the reach bound of ST3 holds each plane of the local envelope.

### ST7: Shape clearance design, section 3.5, `pair` and `largeSafety`

Current text, `docs/design/shape-clearance.md:542` to `:543`:

```markdown
| `pair`, `safety.go:261` | Under D1-B, the envelopes stay. Under D1-A, large pods use their lane location. A shape pair or a merge pair from the tables needs the side-by-side distance of its two classes. |
| `largeSafety`, `envelope_safety.go:13` | Under D1-B, no change. Under D1-A, not used for the plane test when the field is set. |
```

Replace `:542` to `:543` with:

```markdown
| `pair`, `safety.go:261` | Under D1-B, when the field is set, the envelopes take the local front and the small-pod rules of the [group physics design](group-physics-design.md), by decision S1 of the [shared-track](shared-track-preset.md) design. Under D1-A, large pods use their lane location. A shape pair or a merge pair from the tables needs the side-by-side distance of its two classes. |
| `largeSafety`, `envelope_safety.go:13` | Under D1-B, when the field is set, `appendVehicleSafetyLocations` and its reference construction take the local front and the small-pod rules. Without the field, no change. Under D1-A, not used for the plane test when the field is set. |
```

Reason: `largeSafety` also runs for a network without the field, such as D, so each row states the condition.
Under S1-A, `appendVehicleSafetyLocations` changes under the field, and the reference construction changes with it (section 3.1 of the design).

### ST8: Shape clearance design, berths and lanes off the route

Current text, `docs/design/shape-clearance.md:137` to `:139`:

```markdown
  A berth has its own plane and the planes of the lanes at its node.
  The envelope adds only the lanes of the route of the pod (`envelope_safety.go:55` to `:68`), so the rule does not take the other lanes at these nodes.
  This is the rule of D1-B.
```

Insert these lines after `:139`:

```markdown
  When the network sets `shapeClearance`, rule 2 of the [group physics design](group-physics-design.md) also adds each lane at a node of the cluster of a held maneuver group lane, also a lane off the route, and a berth gets the same lanes.
  The counts of this section come from the rule above, without rule 2.
```

Reason: under the field, rule 2 adds lanes off the route to the envelope and to its span in the index.
The lines record the rule of the probe, so this amendment adds a branch and keeps them.

### ST9: Group physics design, berth planes

Current text, `docs/design/group-physics-design.md:88`:

```markdown
A berth has its own plane and the planes of the lanes at its node.
```

Replace `:88` with:

```markdown
A berth has its own plane, the planes of the lanes at its node, and by rule 2 the planes of each lane at a node of the cluster of a maneuver group lane among them.
```

Reason: `:88` sits in the branch of the field.
Under the field, the berth envelope and its span in the index also hold the clusters of the maneuver group lanes at the node (section 3.1 of the design, rule 2 and item 7).

### ST10: Shape clearance design, decision D1

Current text, `docs/design/shape-clearance.md:762`:

```markdown
| D1 | Plane of a large pod | A: the plane of its lane or berth, as for a small pod. B: envelope reach: the oracle keeps the envelope, and the audit takes its planes by part of a path, with the body test where they overlap. C: body reach, 6 m at each end, which drops the retained tail and the origin berth from the envelope. D: today's reach, with a smaller distance. | B. It keeps every plane that the oracle checks today, and it does not rest on refusals going away. It leaves 3 to 129 residual pairs at 20 m on each route, of which 1 to 30 are under 4.0 m (section 2.2), so each route needs some geometry work (section 4.1). A refuses no measured pair, but it permits projected body overlap across planes by assumption. C refuses fewer pairs than B, but it needs a proof that the retained tail and the origin berth do not need their planes. D clears at most a third of the refusals at 12 m. |
```

Replace `:762` with:

```markdown
| D1 | Plane of a large pod | A: the plane of its lane or berth, as for a small pod. B: envelope reach: the oracle keeps the envelope, and the audit takes its planes by part of a path, with the body test where they overlap. C: body reach, 6 m at each end, which drops the retained tail and the origin berth from the envelope. D: today's reach, with a smaller distance. | B. It keeps every plane that the oracle checks today, and it does not rest on refusals going away. When the network sets `shapeClearance`, decision S1 of the [shared-track](shared-track-preset.md) design later drops the planes of the largest front bound of the route that are not under the body, by rule A1 of section 1.1. It leaves 3 to 129 residual pairs at 20 m on each route, of which 1 to 30 are under 4.0 m (section 2.2), so each route needs some geometry work (section 4.1). A refuses no measured pair, but it permits projected body overlap across planes by assumption. C refuses fewer pairs than B, but it needs a proof that the retained tail and the origin berth do not need their planes. D clears at most a third of the refusals at 12 m. |
```

Reason: the row records the decision of 2026-11-16, so it keeps its text and adds the later change.
Unlike option A, S1-A keeps each plane under the body, and A1 is the rule that the dropped planes need.

### ST11: Standee contract, front bound gap

Current text, `docs/contracts/express-standees-contract-proposal.md:313`:

```markdown
   When the network sets `shapeClearance`, the envelope reach of the audit holds the largest front bound of each route through a lane, so the audit covers that case.
```

Replace `:313` with:

```markdown
   When the network sets `shapeClearance`, the oracle takes the local front of the [group physics design](../design/group-physics-design.md), and not the largest front bound of the route.
   A front bound of a route lane that ends before the route distance of the pod minus the slack, or that starts past the local front, does not extend the window.
   The envelope reach of the audit holds the window of this changed oracle, so the audit covers it.
```

Insert this line after the three new lines, at the same indent:

```markdown
   Amended <date>, by maintainer decision: the local front of the oracle envelope, behind the `shapeClearance` network field.
```

Reason: under the field, the reach no longer holds the largest front bound of each route.
`:309` to `:312` stay as the record for a network without the field.

### ST12: Shape clearance amendment proposal, records

The shape clearance amendment proposal is approved and applied, so its text is a record.
The design asks for a dated note at `:294` and `:743`.
This amendment also adds a note at each other record that S1-A, ST4, ST9, ST13, or ST14 makes false under the field.
Each note keeps the record and points to the new rule.

Current text, `docs/contracts/shape-clearance-amendment-proposal.md:66`, in the text of SC1:

```markdown
  When the fleet has a large pod, the oracle keeps the envelope of each pod, so the plane exemption of two pods does not change.
```

Insert this line after `:94`, the end of the reasons of SC1:

```markdown
Note of <date>: under the field, amendment ST4 of the [shared-track amendment proposal](shared-track-amendment-proposal.md) replaces the line at `:66` in the service contract, because the oracle envelope takes the local front and the small-pod rules.
```

Current text, `docs/contracts/shape-clearance-amendment-proposal.md:282`, in the quote of SC9:

```markdown
The route window of a pod can reach further than an incident lane, because the front bound is the largest `6 * projection bound / 20` of the lanes of the route, and each lane stores it beside its tail.
```

Current text, `docs/contracts/shape-clearance-amendment-proposal.md:294`, in the text of SC9:

```markdown
It includes the lanes ahead on the route, up to the largest front bound of the lanes of the route, also more than one lane ahead.
```

Current text, `docs/contracts/shape-clearance-amendment-proposal.md:296`, in the text of SC9:

```markdown
A berth has its own plane and the planes of the lanes at its node.
```

Current text, `docs/contracts/shape-clearance-amendment-proposal.md:302`, in the text of SC9:

```markdown
In such a network, the envelope reach covers the route window of the case above, so that case stays a gap only in a network without the field.
```

Current text, `docs/contracts/shape-clearance-amendment-proposal.md:307`:

```markdown
The oracle exemption at `:68` to `:70` does not change, because D1-B keeps the envelope of each pod.
```

Current text, `docs/contracts/shape-clearance-amendment-proposal.md:315`:

```markdown
- The code takes the largest front bound of the route, and tests each route lane against its own retained tail (`internal/sim/envelope_safety.go:50` to `:69`).
```

`:282` to `:302` are in fenced blocks that record the applied text, so the note goes after `:319`, the end of the reasons of SC9.
Insert these lines after `:319`:

```markdown
Note of <date>: decision S1 of the [shared-track](../design/shared-track-preset.md) design changes the SC9 text and its reasons under the field.
The route window of the oracle takes the local front, and not the largest front bound of the route (`:282`, `:302`, and `:315`).
The reach takes the reach bound of each lane (`:294`), and a berth also holds the clusters of rule 2 (`:296`).
The oracle envelope changes, so the claim of `:307` holds only in a network without the field.
Amendments ST2, ST3, ST9, and ST14 of the [shared-track amendment proposal](shared-track-amendment-proposal.md) give the applied text.
```

Current text, `docs/contracts/shape-clearance-amendment-proposal.md:487`, in the text of SC13:

```markdown
   When the network sets `shapeClearance`, the envelope reach of the audit holds the largest front bound of each route through a lane, so the audit covers that case.
```

Insert this line after `:493`, the end of the reasons of SC13:

```markdown
Note of <date>: amendment ST11 of the [shared-track amendment proposal](shared-track-amendment-proposal.md) replaces the line at `:487` in the standee contract, because the oracle takes the local front under the field.
```

Current text, `docs/contracts/shape-clearance-amendment-proposal.md:550`, in the text of SC15:

```markdown
The locations of a berth do not change.
```

Insert this line after `:556`, the end of the text of SC15:

```markdown
Note of <date>: amendment ST13 of the [shared-track amendment proposal](shared-track-amendment-proposal.md) replaces the line at `:550` in the station banks document, because a berth also holds the clusters of rule 2 under the field.
```

Current text, `docs/contracts/shape-clearance-amendment-proposal.md:652`:

```markdown
  D1-B keeps the envelope of each pod, so the exemption does not change.
```

Insert this line after `:652`, at the same indent:

```markdown
  Note of <date>: under the field, decision S1 of the shared-track design changes the envelope, by amendment ST4 of the [shared-track amendment proposal](shared-track-amendment-proposal.md).
```

Current text, `docs/contracts/shape-clearance-amendment-proposal.md:743`:

```markdown
1. Front bound of the envelope planes: SC9 takes the largest front bound of each route, by section 3.4, item 5, and the envelope reach test of section 7.
```

Insert this line after `:743`:

```markdown
   Note of <date>: decision S1 of the shared-track design replaces this answer with the reach bound of each lane and the local front of the oracle, by amendment ST3 of the [shared-track amendment proposal](shared-track-amendment-proposal.md).
```

### ST13: Station banks document, berth locations

Current text, `docs/station-banks.md:63`:

```markdown
The locations of a berth do not change.
```

Replace `:63` with:

```markdown
When the network sets `shapeClearance`, for a pair with a large class, the locations of a berth are the berth, its incident lanes, and by rule 2 of the [group physics design](design/group-physics-design.md) each lane at a node of the cluster of a maneuver group lane among them.
```

Reason: `:63` sits in the branch of the field that SC15 added at `:61`.
Under the field, rule 2 adds the clusters of the maneuver group lanes at the node to the berth span of the index, as ST9 states for the group physics design.
`:58` keeps the locations of a berth for a network without the field.

### ST14: Group physics design, route window under the field

Current text, `docs/design/group-physics-design.md:94`:

```markdown
In such a network, the envelope reach covers the route window of the case above, so that case stays a gap only in a network without the field.
```

Replace `:94` with:

```markdown
In such a network, the oracle takes the local front, so a front bound of a route lane that ends before the route distance of the pod minus the slack, or that starts past the local front, does not extend the route window.
The envelope reach holds each plane of the route window of this changed oracle, so the case above stays a gap only in a network without the field.
```

Reason: the design lists `:94` as a text that stays true.
Its conclusion stays, but its reason changes.
Under the field, the reach no longer holds the window of the largest front bound of the route, and the window of the oracle does not have that reach.

### ST15: Shape clearance design, records of the built index

Current text, `docs/design/shape-clearance.md:150`:

```markdown
The built envelope reach index takes the largest front bound of the network for each lane, and that changes the counts (section 2.2, "Counts of the built audit").
```

Insert this line after `:150`:

```markdown
This sentence records the index as built for this design.
When the network sets `shapeClearance`, the index of decision S1 of the [shared-track](shared-track-preset.md) design takes the reach bound of each lane of the [group physics design](group-physics-design.md) in place of this bound.
```

Current text, `docs/design/shape-clearance.md:226`:

```markdown
The envelope reach index takes the largest front bound of the network, which is 66.42 m on LondonFull, from the curved lane `940GZZLUWLO-move-05-01`.
```

Insert these lines after `:226`:

```markdown
This sentence records the index as built for this design.
When the network sets `shapeClearance`, the index of decision S1 of the shared-track design takes the reach bound of each lane, and on this network it gives the 7 refusals of the next line (section 2.2 of that design).
```

Reason: the two lines describe the index before S1-A, and under the field the index changes.
The added lines keep the record and give the rule of the field, as ST5 does for `:147`.

## SX qualification rows

Decision S6-A puts the qualification of `london-full-shared-express` in new SX rows, in a new contract proposal.
This section gives their text, for the file `docs/contracts/express-shared-track-qualification-proposal.md`.
The rows follow E1 to E8 of the [Express London qualification](express-london-qualification-proposal.md#amendments), S1, A15 to A18, and section 5.1 of the shape clearance design.
The approved E rows of D, and their copies in the physical, standee, and stage 7 contracts and the station banks document, do not change.

Each SX row keeps the limits and the requirements of the row that it follows, and the evidence of D does not transfer.
Where the short text of section 5 of the design omits a clause of that row, the row below carries the clause, and marks it "carried from".
The rows apply to the geometry whose network hash the preset test pins after slice 3 of the design.
That geometry is the network of section 3.5 of the design, with the Express parking of decision S4.
No simulation has run on the preset, so each evidence item below is evidence that the row needs, and none exists yet.

- SX1, Foundation parity (from E1).
  Measures: that the preset is opt-in, and that it changes no other project.
  Pass: qualification includes the opt-in preset `london-full-shared-express`.
  LondonFull, D, their fleets, and their defaults do not change, and their network hashes stay.
  Carried from E1: idle Express pods keep their current behavior.
  Evidence: a test that pins the network hashes of LondonFull and D.
  The field-off test: on D and on each scenario network, the oracle and the reference construction give the envelopes of today, and `TestBerthEnvelopeMatchesReference` passes.
  The parity test of D: the saved-state and stream bytes of D at each checkpoint equal those of the build before the change.
- SX2, Route and station eligibility, and Physical geometry (from E2).
  Measures: the shared route, its geometry, and the class rules on it.
  Pass: Express runs on the lanes of link 100 and the four roads of section 1 of the design, shared with Compact pods, with no dedicated lane.
  The audit refuses no pair, preparation refuses no lane, and the layout audit of the London generator finds no conflict.
  Qualification covers every hub berth and both directions.
  New for decision S4-A, as E2 does for the parking of D: qualification also covers both directions between the hubs and the Express parking.
  A merge, split, or shape pair certifies the 6 m by 2.5 m class body only (SC6).
  Evidence: a test that pins the network hash of the preset and its audit result after slice 3.
  `projectcheck` reports the preset project valid.
  Route tests from each hub berth in both directions, and to and from each parking berth.
  The sweep cases of SC6 on the preset.
- SX3, Motion and ownership, and Independent safety oracle (from E3).
  Measures: the oracle on the mixed fleet of the preset, and the parity of the two envelope constructions.
  Pass: the oracle checks the mixed fleet at each tick, with fault stops and emergency journeys, the shape table, the envelope reach, and the D6 oracle, and it finds no failure.
  Under decision S1, the indexed and the reference envelope constructions take the local front and the small-pod rules, and they give the same locations in the same order.
  Evidence: runs of the preset with the oracle and the D6 oracle at each tick, with faults and emergencies.
  A test that compares the indexed and the reference constructions for each pod at each tick of a run of the preset.
  `TestEnvelopeReachHoldsEnvelope` and the body support tests of section 7 of the design, with the preset among their networks.
- SX4, Physical restore, and Rejection and disabled policies (from E4).
  Measures: restore of the preset at the physical tier.
  Pass: each planned checkpoint restores at the physical tier with no demotion, requeue, drop, or interruption, with the shape table, and the D6 oracle runs in restored continuation.
  Carried from E4: a restored route distance may differ from the saved value by at most `restoreTolerance`, and all other saved pod members are equal.
  Speed resets and rebuilt forward grants are reported apart.
  After a restore with the policies off, the current members finish and no new member forms.
  Evidence: a checkpoint test on the preset, as `TestLondonExpressPhysicalCheckpoints` is for D, with a fault, an emergency, and a pod on the shared lanes among the states.
- SX5, Save bytes and shape (from E5).
  Measures: the byte bounds with the geometry of the preset.
  Pass: the byte proofs run on the new geometry, with mixed-fleet, incident, and emergency members.
  Carried from E5: use the current formats and Express limits.
  The bounds of the save, the full stream, the replacement delta, the HTTP state, the topology, and the gzip expansion hold with the current adapters.
  A bound over a cap needs a new decision.
  Evidence: `TestExpressStateFileWorstCaseSize` and `TestExpressStreamMaximumEncoding` pad a worst-case project to its cap, so they do not take the geometry of the preset.
  The row needs a proof that this worst-case project bounds the preset, or a run of the proofs with the topology of the preset.
  The record gives the measured sizes and their margins against the caps.
- SX6, Stream and public consumers (from E6).
  Measures: the preset through each consumer.
  Pass: the Go remote client, HTTP, WebSocket, WASM, and desktop Chrome, with loads of 12, 13, and 20 riders.
  Carried from E6: topology replacement, sequence recovery, and atomic rejection.
  Evidence: remote load and recovery tests on the preset, as `TestLondonExpressRemoteLoads` and `TestLondonExpressRemoteRecovery` are for D.
  The maintainer does the Chrome check.
- SX7, Resource and consumer cost (from E7).
  Measures: the cost of the preset against LondonFull, D, and the preset without its Express pods.
  Pass: the measurements are recorded, and the maintainer accepts them, as for E7.
  Carried from E7: report CPU, peak resident memory, GC, retained frames, network rate, and browser responsiveness.
  Wire size does not bound decoded memory.
  Evidence: the server measurement of the Express London qualification for each of the four cases, and a maintainer check of the browser in Chrome on the preset.
- SX8, Orders and registry, and Storage and recovery (from E8).
  Measures: the limits of orders and storage, and recovery, on the preset.
  Pass: the current limits, with directed incident and recovery coverage on the preset.
  Carried from E8: 600 pods under `express-v1`, 17,000 pending and outstanding orders, and 20 stored Express riders for each pod.
  Keep whole-party admission, immutable service identity, history rollover, and conservation over repeated recovery.
  Evidence: directed incident fixtures on the preset, as `TestLondonExpressIncidentFixtures` is for D.
- SX9, the standee Fault, Dwell, and Restore and stream rows (from S1).
  Measures: standee motion and dwell on the preset.
  Pass: the rows run on the preset with standees, mixed traffic, and emergencies.
  Carried from S1: the current class motion and dwell bounds apply.
  Adapted from S1, which says that the `rail-hub` evidence does not qualify London: the evidence of `rail-hub` and of D does not qualify the preset.
  Evidence: the runs of these rows of section 7.2 of the standee contract on the preset.
- SX10, the London soak row (from A15).
  Measures: the preset under the soak.
  Pass: the preset is a named project of the London soak row.
  Carried from A15: the branch shape of the London soak row, the A10 checks, and rule A with A13.
  Evidence: a soak run of the row with the preset, with its log.
- SX11, the CI soak and the release soak (from A16).
  Measures: long runs of the preset with the full check.
  Pass: a CI case of one simulated hour, and 5 seeds of 6 simulated hours before a release tag, with the full check and the D6 oracle at each tick.
  Carried from A16: the release runs end with a clear of 30 minutes.
  Evidence: the CI case in the CI tasks, and the logs of the release runs.
- SX12, Performance, Sampled-tick latency, Worst choice tick, and Web (from A17).
  Measures: the performance and latency of the preset.
  Pass: the preset is in the performance and latency measurements that apply.
  Carried from A17: A7, A11, and the current latency limits stay.
  Evidence: the measurements with the preset, recorded beside those of D.
- SX13, Formats (from A18).
  Measures: the byte proof of the formats on the preset.
  Pass: the formats byte proof runs on the preset.
  Carried from A18: its source and its margins are recorded next to the plain proofs.
  Evidence: the proof of SX5, with the same condition on the worst-case project, and with its source and margins.

B1 does not apply, because the preset has no banks.

## Approved texts that stay true

The design lists these texts, and `:94` of the group physics design, which ST14 rewords:

- Group physics design, `:69`: "a front bound that encloses the body".
  Section 3.1 of the design proves it for a body inside the disk of the proof.
- Express physical contract, `:486`, the Physical geometry gate, and `:541`, SC6.
- Shape clearance design, `:62`: "to the front bound ahead of it".
  The local front is a front bound.
- Shape clearance design, `:136`: "in its last 6 m, or the longer front bound of the lane".
- Standee contract, `:291` and `:309` to `:312`, as records for a network without the field.

This proposal also checked these texts, and they stay true:

- Group physics design, `:70` to `:71`: a stationary berth "includes" its planes, and a departing pod "retains" its origin berth plane.
  Rule 2 adds planes and removes none.
- Group physics design, `:83` to `:85`, `:87`, and `:89`: the reach holds each plane of the envelope, and ST3 applies rule 2 to each lane and berth that the reach holds.
- Service contract, `:62` to `:64`: separated positions refer to the envelope reach of the group physics design.
- Service contract, `:92`: the compatibility rule, on which fact F3 of the proof rests.
- Shape clearance design, `:545`: the reach is still an index by part of a path.
- Shape clearance design, `:830`: the envelope reach test, which takes the changed oracle.
- Express physical contract, `:517` to `:518`, E3: it names D, and D does not set the field.
- Tick-rate contract, `:102`: the plane exemption holds at separated positions.
- Express London qualification, `:156`: the SX rows are the evidence of a shared-track preset.

## Texts outside the design list

The design list omits these texts, and this proposal adds an amendment for each:

- Station banks document, `:63`: ST13.
- Group physics design, `:94`: ST14.
- Shape clearance design, `:150` and `:226`: ST15.
- Shape clearance amendment proposal, `:66`, `:282`, `:296`, `:302`, `:307`, `:315`, `:487`, `:550`, and `:652`: the notes of ST12.

`docs/london.md:135` to `:137` states the meaning of separation groups in the guide.
It changes with the build, and needs no approval.

## Approval

| ID | Document | Change |
| --- | --- | --- |
| ST1 | Shape clearance design | Rule A1 and the support of a body, under the field. |
| ST2 | Group physics design | The route window of the largest front bound only without the field. |
| ST3 | Group physics design | The local front, rule 1, rule 2, the reach bound of each lane, and the scope of the proof, under the field. |
| ST4 | Service contract | The oracle envelope with the local front and the small-pod rules, under the field. |
| ST5 | Shape clearance design | The front of the envelope when the probe ran, and with the field. |
| ST6 | Shape clearance design | Section 3.4, item 5: the planes of the changed envelope. |
| ST7 | Shape clearance design | Section 3.5: `pair` and `largeSafety` take the changed envelope. |
| ST8 | Shape clearance design | Rule 2 adds lanes off the route and berth lanes, under the field. |
| ST9 | Group physics design | Berth planes with rule 2. |
| ST10 | Shape clearance design | Decision D1: the later change of S1. |
| ST11 | Standee contract | The front bound gap under the local front. |
| ST12 | Shape clearance amendment proposal | Dated notes on the records of SC1, SC9, SC13, and SC15, and on the answer to open question 1. |
| ST13 | Station banks document | Berth locations with rule 2, under the field. |
| ST14 | Group physics design | The route window of the changed oracle, under the field. |
| ST15 | Shape clearance design | Notes on the records of the built index, with the rule of the field. |
| SX1 to SX13 | New qualification contract proposal | The rows of the preset. |
| Q1 | New qualification contract proposal | The file name `docs/contracts/express-shared-track-qualification-proposal.md`, which this proposal chose. |

Dependencies:

- ST1 states A1, and ST3, ST4, ST6, and ST10 use it.
- ST3 defines the terms that ST2, ST4 to ST9, ST11, ST13, ST14, and ST15 use, so they go together.
- S1-A changes the oracle and the index under one field, so ST2 to ST11, ST13, and ST14 state parts of one rule.
  If the maintainer approves only some, a binding text contradicts the behavior of the field.
- ST13 goes in this batch, because SC15 put the line that it replaces in the same branch of the field.
- ST12 and ST15 add notes to records, and depend on ST3, ST4, ST9, ST11, ST13, and ST14.
- Q1 does not depend on the others.
- The SX rows do not depend on the ST amendments for their text, but SX3 names the local front and the small-pod rules of ST3.

## Review
