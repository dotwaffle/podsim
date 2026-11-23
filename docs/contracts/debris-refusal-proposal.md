# Debris refusal proposal

Status: draft, not approved.
Date: 2026-11-23.
Line references are for `11302d32`.
No code changed for this draft.
A scratch prototype gave the outcomes of section 3, and it is not committed.

## Summary

A restore can refuse an exported save with `invalid_state`, and then it starts a new session.
It happens when debris lies in the part of the route that the restore reserves for a pod and the live pod does not hold (case M2 of the [exact restore coordinates proposal](exact-restore-coordinates-proposal.md), section 8.1).
Version 13 does the same.

This proposal measures the case and gives four options (decision DR-D1).
No run reached the pod state of the case through exported calls (section 2).
The recommendation is A: such a pod fails its placement, as it does when another pod holds the resource.
In the two cases that A covers, the save then restored in the `logical` tier, so the pods lose their positions and the fault records drop.
The change amends an approved contract (section 5).

## 1. The case

The live pod waits unbound at the entry of a full depot, and no berth elsewhere is open.
A trip command then diverts it.
The redirect keeps the old grant, which ends at the old route end.
On the new route, the restore derives a frontier two blocks further (`internal/sim/state_motion.go:131`).
Without debris, the pod gets the next grant on the next tick, and the two frontiers agree again.
With debris in those blocks, the grant is denied, so the difference stays as long as the debris does.

The restore then finds a debris resource in the footprint of the pod (`internal/sim/state_physical.go:1146`).
It returns `errInvalidFaults`, so `restoreState` does not try the `logical` tier (`internal/sim/state.go:469`), and the session gets `invalid_state` (`internal/session/persist.go:570`).

The fixture makes the pod state with test helpers (`withdrawService`, `prepareEmptyMove`, and `installEmptyMove`).
Only the trip and the debris go through exported calls.
In the fixture, `Fault` starts the debris on `parking-through` from 39 m to 40 m, and the save is refused 400 ticks later.

The state has a second cause before the first tick.
Right after the trip command, the published lane distance is about 11 micrometers behind the route distance (case D3).
The restored pod then meets debris in the footprint up to its own block.
This fits a cell that the live pod released and that debris now owns, but the resource was not traced.
A paused session would stay there.

## 2. Measurement

The probe ran in a scratch copy of `11302d32`, driven by `cmd/compare`.
On each tick it counted the traveling pods with a live frontier below the derived frontier, and the blocks between them that hold debris.
It restored the save with the inputs of `persist.go` on each tick with such debris, and every 300 ticks.
A positive control on the fixture of `TestDepotDivertedFollowUp` found the pod, with and without debris.
A second control counted 1 pod at the full entry before the trip command.

Each arm ran seeds 1, 2, and 3 for one simulated hour, except london-central.
The demand was `balanced`, with a request every 20 s.

| Arm | Faults per hour / debris share / duration | Pod-ticks | Ticks of a wait at a full entry | Pod-ticks below the derived frontier | Restores, all in the first physical restore with no demotion | Faults started |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| rail-hub-incidents, as the preset | 10 / 0.25 / 120 s | 5,717,122 | 0 | 0 | 1,080 | 34 |
| rail-hub-incidents, 2 depot lanes of 12 slots | 10 / 0.25 / 120 s | 5,776,786 | 0 | 0 | 1,080 | 33 |
| The ring, 3 depot lanes of 4 slots, 47 pods on 48 berths | 10 / 0.25 / 120 s | 5,771,752 | 0 | 0 | 1,080 | 33 |
| parking-constrained, 2 depot lanes of 2 slots, 23 pods on 24 berths | 10 / 0.25 / 120 s | 5,198,142 | 0 | 0 | 1,080 | 34 |
| As above, 20 pods, all faults debris | 60 / 1.0 / 600 s | 2,527,517 | 0 | 0 | 1,080 | 147 |
| london-central with incident rates, 15 min | 10 / 0.25 / 120 s | 1,257,837 | 0 | 0 | 270 | 5 |
| Total | | 26,249,156 | 0 | 0 | 5,670 | 286 |

No restore was refused, and no pod had a live frontier below the derived frontier, in any arm.
The probe did not count reroutes.
The redirect at a full depot entry is the only source that this work found.

A random test on the depot network of the fixture used only exported calls: 60 seeds of 10 simulated minutes, with 5 to 8 pods, random trips, and random pod faults.
It found no wait at a full entry and no pod below the derived frontier.
This does not prove that the state is unreachable.

The limits of this measurement:

- The sampled preset arms never held a pod at a full depot entry, so they never had the precondition.
  The zero holds for these arms, and it gives no frequency for a fleet that fills a depot and every berth.
- The probe restores every 300 ticks, so it can miss a refusal from the second cause of section 1 between two restores.
- The arms start debris from the sampler and no operator command, and no arm pauses.

## 3. Options for DR-D1

The fixture is `divertedAtEntry` with debris on `parking-through`.
Each other berth of that fixture is taken.

| Case | Today | A: demote | B: logical tier | C: clamp |
| --- | --- | --- | --- | --- |
| Grant denied, no pod fault | Refused | `logical` | `logical` (not run) | `physical`, live frontier, request, and owners |
| Pod faulted after the denial | Refused | `logical` | Refused, unless the pre-tier check changes (not run) | `physical`, live frontier and owners |
| No tick after the trip command, no fault | Refused | Refused | `logical` (not run) | Refused |
| Pod faulted, no tick after the trip command | Refused | Refused | Refused, unless the pre-tier check changes (not run) | Refused |

With `LogicalOnly`, the two cases without a pod fault restore in the `logical` tier today, and the two with a fault are refused today.
A and C make the second row restore with `LogicalOnly` too.
Every restored save of A and C passed `CheckContract`.

A prototype of A also ran the short debris, fault, restore, and depot tests of `internal/sim`.
Only the two follow-up subtests that assert the refusal failed.

- **A, demote**: With no saved lead, debris in the derived blocks after the block of the pod fails the placement without an error, and the pod demotes.
  In the fixture no berth is free, because the pod waits only when none is, so the `physical` tier fails and the `logical` tier restores the whole fleet.
  The pods lose their positions, the trip is requeued, and the fault records drop.
  With a free berth at the restore, the pod would demote to it and the fleet would stay in the `physical` tier; that path is not tested, and it needs a berth that frees after the pod started its wait.
  The rule is before the link check, so it also applies to a follower, and a demotion takes the followers of the pod with it.
  A faulted pod fails the `physical` tier (section 12.7 of the incident suspension contract), so the records drop.
- **B, fall to the logical tier**: The refusal loses its `errInvalidFaults` mark.
  It covers the third row too, but it also accepts a file with a pod body on debris, which section 7.6 of the contract calls invalid.
- **C, clamp**: The frontier drops to the last block before the debris, so the restore is exact in the two cases it covers.
  The rule gives a frontier that a live grant can have only after a route change, so its legality depends on the history of the route.
  That is the reason for the withdrawal of the signed lead (EC-D10).
- **D, no change**: The refusal stays, and `docs/operations.md` describes it.
  It costs nothing, and it is reasonable while no exported path to the pod state is known.

## 4. Recommendation

A, for these reasons:

- It replaces a new session with a `logical` restore.
- It is the smallest change that ends the refusal in the two rows it covers.
- A pod that meets a resource of another pod already demotes (`internal/sim/state_physical.go:1172`), so no new trust in saved values is added.
- A crafted file gets no resource from A: the pod holds nothing until it has a berth, or the `logical` tier places the fleet.
- The file is not partly recovered.
  The save is a valid one, and A changes which restore of it runs.

A keeps the refusal for a pod whose own body, up to and with its block, meets debris.
It also keeps the pre-tier check and its place in the order, because `checkSavedFaultFootprints` uses the same placement.

The two no-tick rows stay refused under A.
Their cause is the stale lane distance (D3), and its fix is a separate decision.

## 5. Contract and document changes on approval

Nothing in this section is applied.

- Incident suspension contract, section 7.6: "A restored pod whose ownership meets `F` is a conflict, and the save is invalid" gets the exception of A.
- The same contract, sections 12.7 and 13.5: a note.
- `docs/operations.md:449`: "holds a resource of debris" gets the same exception.
- `EC-D10` of the exact restore coordinates proposal: a note that M2 is fixed in part.

Pins:

| Pin | Change |
| --- | --- |
| `TestRestoreInvalidDebris`, `a pod in the footprint` | Stays: the debris is under the pod body. |
| `TestRestoreMotionHeadFaultRefusal` | Stays: debris under the body of p03. |
| `TestRestoreMotionHeadLogicalFallback` | Stays: the saved lead is more than 0. |
| `TestDepotDivertedFollowUp`, the denied and the faulted-after-denial cases | Change from the refusal to the `logical` tier. |
| `TestDepotDivertedFollowUp`, faulted before a tick | Stays. |
| Error text and order of each refusal | Stay. |

The saved-state version does not change.
The byte proofs do not change, because the format does not change.

## 6. Stops

| Condition | Action |
| --- | --- |
| A file that restores today gets a different result | Stop, and report the file. |
| A pin of section 5 that stays moves | Stop. |
| A restored save fails `CheckContract` | Stop. |

## 7. Decision

| ID | Decision | Options | Recommendation |
| --- | --- | --- | --- |
| DR-D1 | The rule for debris in the derived blocks | A: demote. B: logical tier. C: clamp. D: no change. | A |
| DR-D2 | The no-tick cases from the stale lane distance | A: leave them refused, recorded. B: a separate proposal for D3. | A |
