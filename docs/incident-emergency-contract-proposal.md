# Incident emergency contract proposal

Status: Approved revision 7, October 6, 2026.
The maintainer approved product choices P1 to P22 of section 18.2 as proposed, on October 5, 2026, at 22:08Z, and decided P23 on October 5, 2026, at 23:59Z.
The contract authorizes no implementation, default change, or deployment.
Line references are at `481c145`.

Physical coupling was removed on October 6, 2026.
Clauses about the couplingContract marker, coupling sites and corridors, coupling approaches, and coupled trains and their members no longer apply.
Where a rule lists a coupling case with other cases, only the coupling case is removed.
Platoon, compact queue, and station group rules are unchanged.

Station buffers and compact station queues were removed on October 7, 2026, because measurements showed that they lowered station entry throughput.
Clauses about buffered pods, buffer heads, buffer claims, compact queue members, and compact certificates no longer apply.
Where a rule lists a buffer or compact case with other cases, only that case is removed.
The sentence above that keeps compact queue rules unchanged no longer covers them.
The function `checkCompactFields`, which the restore order below names, no longer exists.

Station entry runs, an early berth choice, and coast-in were added on October 7, 2026.
The sentence above that keeps platoon rules unchanged no longer covers them.
A platoon run can end on the entry lane of the destination station of both pods, and it never continues past the entry node of that station.
With platoons on, a pod that is not of a large class can try its berth choice before the usual point, and an early try that finds no berth does not refuse the pod.
With platoons on, a pod that waits for a contested station diverge can coast: a speed ceiling lowers only its commanded speed.
The incident refusal of platoon members and the emergency drain rule are unchanged.

Status note, October 6, 2026: the maintainer stopped the incident redesign after stage 3.
Stages 4 to 7 are out of scope.
Text that names these stages describes refusals that stay in place.

This contract is stage 3 of the staged incident redesign: the emergency vertical slice on ordinary pods.
A rider in an ordinary pod starts an emergency.
The pod goes to the station where it can unload soonest, and every party leaves it there.
A pod in a train stays in the train until its committed split site, and the emergency acts on it after the split.

It builds on the API of the approved stage 1 contract, [incident-service-transitions-contract-proposal.md](incident-service-transitions-contract-proposal.md), section 13, and on the approved stage 2 contract, [incident-suspension-contract-proposal.md](incident-suspension-contract-proposal.md), section 15.
It changes neither contract.

Every `file:line` reference below is for `481c145`.
At that commit, stage 1 is complete, and stage 2 patches 1 to 3 have landed: the blocked route graph view (`internal/sim/blocked_routes.go`), the fault records with the fault stage, and the suspension of a faulted pod with its gates, braking, evacuation, and hold release (`internal/sim/faults.go`).
Stage 2 patches 4 to 9 have not landed: debris, the rules for obstructed pods with the endpoint reroute and the claim surrender, and the formats.
This contract cites those parts by stage 2 section, not by `file:line`.
Stage 3 starts after stage 2 patch 9, except patch 1 of section 16, which can land at any time.

Revision 2 folds the first external review (section 19.1).
Revision 3 folds the adversarial review (section 19.2).
Revision 4 folds the confirmation review (section 19.3).
Revision 5 folds the second confirmation check (section 19.4).
Revision 6 folds the maintainer decision on the latency of the station choice (product choice P23, section 19.5).
Revision 7 folds the maintainer decision on the discovery skip for emergency records (section 19.6).

## 1. Scope

### 1.1 What stage 3 delivers

1. Emergency records: one record for each pod in an emergency, with a party, a start tick, and a phase that each tick derives again (section 4).
2. The emergency start at a command boundary, and an emergency stage in `Step` that withdraws the pod, starts the unload at a berth, or binds the pod to an emergency station (section 5).
3. Deferral of coupling, approach, platoon, and compact members until they leave their group, with the train split at its committed split site (section 5.6).
4. The station choice: the soonest estimated arrival over the reachable passenger stations, chosen once (section 9.1).
5. An emergency tier in admission that wins free resources and never revokes a grant (section 9.3).
6. The fix of review finding Q7: coupling discovery and adoption skip a withdrawn pod and a pod with an operational purpose, and discovery also skips a pod that a record names (section 5.8).
7. The emergency marker, the settings, the command, and the emergency members of each format (sections 10 and 11).

### 1.2 Out of scope

| Item | Stage |
| --- | --- |
| An interruption of a physical train before its split site | Stage 4 |
| A group exit for a platoon or compact member before the end of its link or group | Stage 6 |
| Reverse motion of an emergency pod | Stage 5 |
| Refuge travel (purpose 2), and P3, P4, and P11 beyond their stage 3 defaults | A later stage, if the maintainer chooses it (section 18.2) |
| The scenario rate `perHour` above 0 | Stage 7 |
| The full metric set | Stage 7 |
| The revocation of a grant, a claim, or a buffer position of another pod | Never (decision 1) |

### 1.3 Binding user decisions

These decisions come from the incident redesign approval of October 5, 2026.
They are not open to change in this contract.

1. The emergency pod wins free resources and alternate entries.
   It never revokes the grants of other pods.
2. A train splits at its committed split site before the emergency acts on the member.
3. A transferred party can ride any pod, also the emergency pod again.
4. Released pending pickups go to other pods, with the stage 1 exclusion that holds until boarding.
5. Evacuation never happens before rest, and every evacuated party is interrupted.
6. Every stage is off by default.
   With the stage off, trajectories, bytes, digests, and the RNG sequence stay the same.
   A target that the stage does not support is refused before any change.
7. Coupling discovery and adoption skip a withdrawn pod and a pod with an operational purpose (review finding Q7 and the patch 8 note: a purpose 3 pod adopted into coupling keeps its purpose until the retirement and the next arrival, so the train delays its recovery).
   Stage 3 owns this fix.
   Revision 7 extends the discovery skip to a pod that a record names, by the maintainer decision of October 6, 2026 (section 19.6).
8. No build tags.
   Exported test entries are allowed, and section 13 lists each one.

The product brief records the earlier user decisions of October 4, 2026 (`PROJECT_BRIEF.md:687-711`).
Decisions 1 and 3 above narrow two of them: "moves ahead of entry queues" becomes decision 1, and "a new pod" becomes decision 3.

### 1.4 Capability matrix

| Target | Stage 3 | Later |
| --- | --- | --- |
| Ordinary occupied pod, `Traveling`, that `divertStart` accepts | Accepted. The pod binds to an emergency station (section 5.4). | |
| Ordinary occupied pod, `Traveling`, in its arrival chain or in a station buffer | Accepted. The pod stays deferred until it arrives, and then unloads at that berth. | |
| Ordinary occupied pod at a berth: `Boarding`, `Unloading`, or `Continuing` | Accepted. The unload starts at once at that berth. | |
| Platoon leader or follower | Accepted, deferred. The link drains, and the pod acts after the link ends (section 5.6). | Stage 6 adds a group exit. |
| Compact queue member | Accepted, deferred until the pod leaves the group. | Stage 6 |
| Coupling member (`couplingID != ""`) | Accepted, deferred until the split at its committed split site. | Stage 4 |
| Approach member (`couplingApproachMember`) | Accepted. The approach aborts, and the pod then acts as an ordinary pod (section 5.6, product choice P15). | Stage 4 |
| Faulted pod that carries passengers | Accepted, deferred while the fault lasts (section 5.7, product choice P16). | |
| Pod that carries no passenger, also an evacuated faulted pod | Refused with `pod carries no passenger`. | |
| Pod that already has an emergency | Refused with `pod already has an emergency`. | |
| `perHour` above 0 | Refused | Stage 7 |

Every accepted target gets its record and `emergencyHold` at once, except a coupling or approach member, which gets the hold after it leaves the group (section 5.6).

### 1.5 Stage 1 and stage 2 parts used

| Part | Source | Use here |
| --- | --- | --- |
| `withdrawService`, `restoreService` with `emergencyHold` | Stage 1, section 4.2 | Emergency start and record end |
| `releasePickups` with the exclusion that holds until boarding | Stage 1, sections 5.1 and 5.2 | Through the first hold (decision 4) |
| `setOperationalDestination` with purpose 1 | Stage 1, section 9.3 | Binding to an emergency station |
| `startOperationalUnload` | Stage 1, section 9.3 | Unload at the current berth |
| `finishOperationalUnload`, `transferRider`, the interrupted outcome | Stage 1, sections 7.3, 8, and 9.5 | The outcomes of the unload. The party is interrupted, and every other party completes or is transferred (decision 3). |
| `continuationFeasible` | Stage 1, section 13 | Not used by the default station rule. Product choice P13 can use it. |
| `nextIncidentID` | Stage 1, section 11.4 | Emergency IDs |
| The incident marker and the digest registry | Stage 1, sections 11.2 and 11.3 | The emergency marker requires the incident marker. New extension numbers. |
| Restore tiers | Stage 1, section 9.6 | Section 10.7 |
| The joint save allocation, with 65,536 bytes for emergency records | Stage 1, section 11.7 | Section 11.6 |
| `routingGraph`, `berthBlocked` | Stage 2, section 8, landed (`internal/sim/blocked_routes.go` `(*Simulation).berthBlocked`, `(*Simulation).routingGraph`) | Candidate berths and routes |
| The fault stage with evacuation and the hold release rule | Stage 2, sections 5.5 and 5.6, landed (`internal/sim/faults.go` `(*Simulation).removeFault`, `(*Simulation).faultStage` (hold release)) | Overlap with faults (section 5.7) |
| The gates of a faulted pod | Stage 2, section 6.2, landed (`internal/sim/simulation.go` `(*Simulation).Step`, `internal/sim/traffic.go` `(*Simulation).admit`) | Overlap with faults (section 5.7) |
| The endpoint reroute | Stage 2, section 9.3 | Its rider detour check does not apply to purpose 1 (section 9.2). |
| `incidentOutstanding` and the claim surrender | Stage 2, sections 9.5 and 15 | The gate also counts emergencies (section 9.4). |
| F11 | Stage 2, section 10, landed in `checkFaults` (`internal/sim/faults.go` `(*Simulation).checkFaults`) | Extended to `emergencyHold` (section 8). |
| Digest `N = 4`, `Command.PodID` | Stage 2, section 13.2 | The `emergency` command reuses it. |

Stage 3 does not call `resumeFromRefuge`, `rebindOperationalOwner`, or `evacuate`.

## 2. Terms

| Term | Meaning |
| --- | --- |
| Record | One active emergency: `emergencyRecord` of section 4.1. |
| Party | The rider order that started the emergency. `finishOperationalUnload` interrupts it. |
| Emergency pod | A pod that a record names. |
| Deferred | The record pod has purpose 0. It waits for a berth, for a divertible state, or for the end of its group. |
| Bound | The record pod has purpose 1 and is not yet at the berth of that purpose. |
| Unloading | The record pod has purpose 1 and unloads at the berth. |
| Divertible | `divertStart` (`internal/sim/diversion.go` `(*Simulation).divertStart`) accepts the pod. |
| Group member | A coupling member, an approach member, a platoon leader or follower (`coupled`, `internal/sim/platoon.go` `(*vehicle).coupled`), or a compact queue member. |
| Emergency stage | The step of section 5.3, at the policy place of `Step`. |
| Estimate | `E` of section 9.1, in seconds. |

## 3. Existing source boundaries

| Source | Fact | Consequence for stage 3 |
| --- | --- | --- |
| `internal/sim/simulation.go` `(*Simulation).Step` | The unloading loop runs before the fault stage, which runs before `dispatch`. | The emergency stage runs right after the fault stage and before `dispatch`. |
| `internal/sim/simulation.go` `(*Simulation).Step` | The unloading loop calls `finishOperationalUnload` for purpose 1 at phase 0. | The unload outcomes need no stage 3 code. |
| `internal/sim/simulation.go` `(*Simulation).arrive` | Purpose 1 removes the station from `Stops`. Only the `RelocatingTo` branch clears `op`. | A bound pod starts its unload in `arrive`. |
| `internal/sim/traffic.go` `(*Simulation).publishVehicleTravel` | The single caller of `arrive`. | Every arrival, also from a buffer or a compact queue, takes the purpose 1 branch. |
| `internal/sim/service_withdrawal.go` `(*Simulation).withdrawService` | `withdrawService` refuses a coupling or approach member and runs `releasePickups` at the first hold. | A group member gets its hold after the split or the abort. |
| `internal/sim/service_withdrawal.go` `(*Simulation).restoreService` | `restoreService` refuses the owner hold of a purpose and a coupling member. | The record end releases the hold only when the purpose has ended. |
| `internal/sim/operational.go` `(*Simulation).operationalMember` | Refuses a coupling, approach, platoon, or compact member. | `startOperationalUnload` refuses a group member. |
| `internal/sim/operational.go` `(*Simulation).setOperationalDestination` | Keeps the `divertStart` prefix, installs `v.Route[:prefix]` plus `assignedRoute(v, from, berth.Node)`, and leaves `RelocatingTo` empty for purpose 1. | The estimate uses the same route that the call installs (section 9.1). |
| `internal/sim/operational.go` `(*Simulation).startOperationalUnload` | Works at a berth for `Boarding`, `Continuing`, and `Unloading`. Keeps `phaseTicks` when the pod already unloads with `phaseTicks >= 1`. | A pod at a berth unloads there, with no route search. |
| `internal/sim/operational.go` `(*Simulation).finishOperationalUnload` | The interrupt mask wins over `To == StationID`. Each other rider completes or is transferred. Then `settleIdleAtBerth` clears `op` and keeps the holds. | The record ends in the next emergency stage, which is in the same tick. |
| `internal/sim/operational.go` `(*Simulation).evacuateLane` | Replaces any purpose with purpose 3 owned by `faultHold`. | An evacuated emergency pod has no rider and no purpose 1. |
| `internal/sim/diversion.go` `(*Simulation).divertStart` | Refuses coupling, platoon, and compact members, and the arrival chain. | A pod that `divertStart` refuses stays deferred. |
| `internal/sim/coupling_approach_runtime.go` `(*Simulation).discoverCouplingApproaches` | The skip tests test only coupling and approach membership. | The Q7 skip goes there. |
| `internal/sim/coupling_approach_runtime.go` `(*Simulation).prepareCouplingAdoption` | Builds the adoption from the approach context. A denial continues the loop (`internal/sim/coupling_step.go` `(*Simulation).planNativeCouplingTick`). | The Q7 adoption guard goes there. |
| `internal/sim/coupling_approach_context.go` `(*couplingApproachContext).changed` | A non-empty reason brakes and aborts the approach (`internal/sim/coupling_approach.go` `planCouplingApproach`, `brakeApproach` in `(*couplingApproachContext).brakeApproach`). | An emergency on an approach member aborts the approach. |
| `internal/sim/coupling_step.go` `(*Simulation).finishNativeCoupling` | `finishNativeCoupling` clears `couplingID` at the split. | The next emergency stage withdraws the pod. |
| `internal/sim/platoon.go` `(*Simulation).maintainLink` | Only `extendLink` clears `draining`. A link ends when it drains and the follower holds no resource of a pod ahead. | A link with an emergency pod drains and is not extended. |
| `internal/sim/platoon.go` `(*Simulation).tryLink` | Forms a new link. | It refuses an emergency pod at either end. |
| `internal/sim/traffic.go` `intent`, `compareAdmission` | `intent` and `compareAdmission`: aged intents first, then priority, then age, then ID. | The emergency tier goes before the age rule. |
| `internal/sim/traffic.go` `(*Simulation).grant` | The owner loop returns before any write when another pod owns a resource. | A denied emergency grant writes nothing and takes no owned resource. |
| `internal/sim/berth_choice.go` `(*Simulation).reevaluateTerminalBerth` | Treats an occupied pod as a passenger route, filters with `berthFilterForVehicle`, and checks `rerouteKeepsDetours` against `v.Stops`. | Purpose 1 needs the class filter only and no detour check (section 9.2). |
| `internal/sim/berth_continuation.go` `(*Simulation).berthFilterForVehicle` | Filters berths by the onward stops of the riders. | Purpose 1 has no onward stop. |
| `internal/sim/routing_policy.go` `(*Simulation).queueDischarge`, `queueDelay` | `queueDischarge` counts stopped pods with a wait reason on each lane, and `queueDelay` is `max(0, discharge - at)`. | The queue term of the estimate. |
| `internal/sim/station_buffer_claim.go` `(*Simulation).bufferClaimCanYield` | A withdrawn head makes no other pod yield. | Stage 3 does not change it. |
| `internal/sim/state_physical.go` `(*physicalRestore).placeDemoted` | Demotion of a purpose 1 pod interrupts the marked riders and requeues the others. | A demotion of an emergency pod fails the physical tier instead (section 10.7). |
| `internal/sim/state_logical.go` `restoreLogical` | The logical tier builds a new fleet with no purposes, and it copies each saved hold. | Every record ends, and the tier releases `emergencyHold` (section 10.7). |
| `internal/sim/state.go` `restoreState` | Checks of saved fields that run before either restore tier. A failure rejects the save with no fallback. | The emergency checks of section 11.5 go there. |
| `internal/session/receipt.go` `digestExtensions` | Only `N = 1` has landed. Stage 2 reserves `N = 2` to `N = 9` (stage 2, section 13.2). | Stage 3 takes `N = 10` to `N = 12` and reuses `N = 4`. |
| `internal/session/stream_codec.go` `MaxStreamJSON`, `MaxStreamMessage` | `MaxStreamJSON` is 65 MiB, and `MaxStreamMessage` is 66 MiB. | The stream budget of section 11.6. |

## 4. State

### 4.1 Records

```go
// emergencyRecord is one active emergency. Its ID is i<generation>.<serial>.
type emergencyRecord struct {
	generation, serial uint64
	// start is the tick of the start.
	start int64
	// pod is the index in Simulation.vehicles of the emergency pod.
	pod int
	// order is the order ID of the party. It is an active rider of the
	// pod while the record is bound or unloading.
	order int
}
```

`Simulation` gets these members:

| Member | Meaning | Reset | Clone |
| --- | --- | --- | --- |
| `emergenciesOn bool` | The emergency marker is set. It enables the start and the emergency stage. | Kept, as `faultsOn` (`internal/sim/simulation.go` `Simulation`) | Copied |
| `emergencies []emergencyRecord` | The active records, in serial order. | Cleared | Deep copy |
| `emergencyCounters` | The counters of section 10.5. | Cleared | Copied |
| `emergencyMisses []emergencyMiss` | The no-candidate memo of section 9.1, one entry for each record with a proven no-candidate result. Not saved. | Cleared | Not copied: a clone starts with no memo, which changes no result |
| `routeView *routeView` | The routing view of one station choice (section 9.1). Not saved. | Nil outside `advanceEmergency` | Nil outside `advanceEmergency` |
| Search counters | The graph searches by kind, for the tests and the gates of section 15. Not saved, and no rule reads them. | Not cleared | Copied |

`emergencyMisses` and the search counters are excluded from the internal-state equality of the clone tests, because a clone drops the memo and so makes other searches with the same trajectory.
`internal/sim/clone_test.go` gets matching entries: `emergencyMisses` gets `cloneDrop` in `cloneRules` and `persistReset` in `persistRules`, the search counters get `persistReset`, and `stripCaches` clears both, so `sameState` does not compare them.

Records share `incidentSerial` with fault records, so the IDs of both kinds never collide.
The record holds the order ID, not the rider index, because rider indexes change while a deferred pod still lets riders alight or board.
The interrupt mask is resolved from the order ID when the pod binds (section 5.4).

No pod member is added.
`emergencyHold` and `op` already exist (`internal/sim/simulation.go` `vehicle`; stage 1, section 9.2).

### 4.2 Derived phase

The phase is not stored.
Each reader derives it from the pod:

| Phase | Condition |
| --- | --- |
| `deferred` | `v.op.purpose == opService` |
| `bound` | `v.op.purpose == opEmergencyUnload` and the pod is not `Unloading` at `v.destination` |
| `unloading` | `v.op.purpose == opEmergencyUnload`, `Unloading`, and `v.Pod.BerthID == v.destination.ID` |

A record pod with another purpose has no active rider (invariant E8), and the next emergency stage ends its record (section 5.5).
No commitment flag, switch count, or decision window exists.
A save holds the purpose in the pod tuple (stage 1, section 11.5), so a restore keeps the phase without a new member.

### 4.3 Limits

| Limit | Value | Basis |
| --- | ---: | --- |
| Records for one pod | 1 | Precondition 5 of section 6.1 |
| Active records, `MaxEmergencies` | 4 | Product choice P19 |
| Decoder limit for records | `MaxEmergencies` | Section 11.5 |
| Byte budget basis | 300 records | Section 11.6. The budget holds for any cap up to the pod limit. |
| Station choices for one record | One commit. One choice at the start when the pod is divertible then, and one on each 60th tick while the pod is deferred and divertible. | Section 9.1 |

## 5. Lifecycle and operations

### 5.1 Operations

| Operation | Place | Effect |
| --- | --- | --- |
| `Emergency(podID, orderID)` | Command boundary, under the session lock | Section 5.2 |
| `emergencyStage()` | `Step`, after the fault stage and before `dispatch` | Section 5.3 |
| `advanceEmergency(r)` | The start and the emergency stage | Section 5.4 |
| `endEmergency(i)` | The emergency stage | Section 5.5 |

No other place changes a record.

### 5.2 Start

`Emergency(podID string, orderID int) (string, error)` checks the preconditions of section 6.1 in order, and returns the first error with no change.
Then it runs these steps:

1. `id := s.nextIncidentID()`.
   The arithmetic preflight of precondition 8 makes this step safe.
2. Append the record with `start = s.tick` and the order ID of the party.
   Add 1 to `started`.
3. When the pod is not a coupling or approach member, call `withdrawService(v, emergencyHold)`.
   This is the first hold, unless the pod is faulted, so `releasePickups` releases every pending pickup of the pod with the exclusion that holds until boarding (decision 4).
4. Call `advanceEmergency` for the record once (section 5.4).
5. Call `observe` (`internal/sim/state_contract.go` `(*Simulation).observe`) once.

The reply is the record ID.
A paused session accepts the command.
The pod acts when the session runs again, except that step 4 already starts the unload or binds the station.

### 5.3 Emergency stage

The emergency stage runs in `Step` right after the fault stage (`internal/sim/simulation.go` `(*Simulation).Step`) and before `dispatch`, only when `emergenciesOn`:

```go
if s.faultsOn {
	s.faultStage()
}
if s.emergenciesOn {
	s.emergencyStage()
}
```

This is the one policy place of stage 1, section 10, with a second gated call.
Stage 2, section 5.5, does not change.

For each record, in serial order:

1. End the record by section 5.5 when the pod carries no passenger.
2. End the record by section 5.5 when the phase is `deferred` and the party is not an active rider of the pod.
3. Otherwise call `advanceEmergency`.

Then add the number of remaining records to `emergencyTicks`.

The stage runs after the unloading loop, so an unload that ends in this tick has already run `finishOperationalUnload`, and the record ends in the same tick.
The stage runs after the fault stage, so an evacuation in this tick has already removed the riders, and the record ends in the same tick.
The stage runs before `dispatch`, so a pod released at the record end is supply in the same tick, and a transferred party can ride it again (decision 3).

### 5.4 Advance

`advanceEmergency(r)` for the pod `v` of record `r`:

1. When `v.faulted` (`internal/sim/simulation.go` `vehicle`), return.
   The pod is deferred while the fault lasts (section 5.7).
2. When `v.couplingID != ""` or `couplingApproachMember(v.Pod.ID)`, return.
   The pod is deferred until its split or the end of the approach (section 5.6).
3. When `v.withdrawn&emergencyHold == 0`, call `withdrawService(v, emergencyHold)`.
4. When `v.op.purpose != opService`, return.
   The pod is bound or unloading, and `arrive` and the unloading loop do the rest.
5. Let `mask = 1 << i`, where `i` is the index of the party in `v.Riders`.
6. When the pod is at a berth with activity `Boarding`, `Continuing`, or `Unloading`, call `startOperationalUnload(v, emergencyHold, mask)` and return.
   When the call refuses, the pod is a platoon or compact member, and it stays deferred.
7. When the pod is `Traveling`, `divertStart` accepts it, and either the call comes from the start or `(s.tick - r.start) % 60 == 0`, run the station choice of section 9.1.
   When it finds a pair `(S, b)`, call `setOperationalDestination(v, operationalTarget{purpose: opEmergencyUnload, owner: emergencyHold, interrupt: mask, station: S, berth: b})`.
8. Otherwise the pod stays deferred, and it keeps its route.

Each operation in steps 3, 6, and 7 checks its own preconditions before any change.
A refusal leaves the pod deferred and changes nothing, and the next stage tries again.

A deferred pod that cannot divert keeps its current route.
In its arrival chain, it arrives at its current berth, and the next emergency stage starts the unload there.
`startOperationalUnload` keeps a `phaseTicks` of 1 or more, so the pod does not wait a second unloading interval.

The 60-tick cadence of step 7 bounds the cost of the station choice (section 9.1).
A pod that becomes divertible after its start waits at most 59 ticks for the choice.

### 5.5 Record end and hold release

`endEmergency(i)` removes record `i`, adds 1 to `ended`, and, when `v.withdrawn&emergencyHold != 0`, calls `restoreService(v, emergencyHold)`.

The record ends in these cases, and in no other:

| Case | Cause |
| --- | --- |
| The pod carries no passenger. | `finishOperationalUnload` ended the unload, a fault evacuation interrupted every rider, or every rider left at its stops while the pod was deferred. |
| The record is deferred, and the party is not an active rider. | The party left the pod while the pod was deferred, for example at an ordinary alight of a compact member at its berth, or by an interruption (product choice P17). |
| A logical restore, or a reset. | Section 10.7. |

`restoreService` cannot fail at a record end:

- The purpose is not owned by `emergencyHold`.
  Purpose 1 needs an active rider (stage 1, section 9.3), and both transitions that remove the last rider clear purpose 1: `settleIdleAtBerth` and `evacuateLane`.
  In the deferred case, the purpose is 0.
- The pod is not a coupling member: a pod with `emergencyHold` never becomes one (invariant E6), and a deferred coupling member has no hold.
- The emergency stage runs outside a dispatch pass.

A faulted pod keeps `faultHold` after the end, so it stays withdrawn.
The stage 2 hold release rule (`releaseFaultHolds`, `internal/sim/faults.go` `(*Simulation).faultStage`) releases `faultHold` later.

### 5.6 Group members

The pod finishes its group motion first, and the emergency acts after the pod leaves the group.

| Group | Rule | End of the wait |
| --- | --- | --- |
| Coupling member | No change to the train. The pod is not withdrawn, because `withdrawService` refuses it (`internal/sim/service_withdrawal.go` `(*Simulation).withdrawService`). | The split at the committed split site clears `couplingID` (`internal/sim/coupling_step.go` `(*Simulation).finishNativeCoupling`) at the end of a tick. Coupling discovery at the start of the next tick skips the pod, because a record names it (section 5.8). The emergency stage of that tick withdraws the pod before `dispatch` and `formPlatoons`. |
| Approach member | `couplingApproachContext.changed` returns the reason "approach member has an emergency" when either member has a record. `planCouplingApproach` then brakes and aborts the approach through `brakeApproach` (`internal/sim/coupling_approach.go` `(*couplingApproachContext).brakeApproach`). | The approach ends. Coupling discovery skips the pod (section 5.8), and the next emergency stage withdraws it. A rear member that is still a platoon follower then follows the platoon rule. |
| Platoon leader or follower | `maintainLink` sets `draining` on each tick when either end has a record, and it does not call `extendLink`. `tryLink` refuses a pair with a record at either end. | The link ends when the follower holds no resource of a pod ahead (`internal/sim/platoon.go` `(*Simulation).maintainLink`). The pod is then divertible. |
| Compact queue member | No change to the group. `divertStart` and `startOperationalUnload` refuse the pod. | The pod leaves the group. Stage 6 adds an exit. |

Only `extendLink` clears `draining`, and the link rule sets it again on each tick, so the rule keeps no state of its own.
A restore needs no new member for it.

A deferred coupling member keeps `withdrawn == 0` until its retirement.
No rider alights in the train: a coupling plan ends its handoff before the receiving boundary of each member (`internal/sim/coupling_motion_context.go` `(*couplingMotionContext).bindExits`).
The sequence is retirement, then arrival:

1. `finishNativeCoupling` retires the members at the split site, clears `couplingID`, and keeps the retained resources (`internal/sim/coupling_step.go` `(*Simulation).finishNativeCoupling`).
2. Coupling discovery of the next tick skips the pod, because a record names it (section 5.8).
   The emergency stage of that tick withdraws the pod, and the first hold releases each pending pickup of the pod with the exclusion (decision 4).
3. The pod continues on its ordinary route.
   When `divertStart` accepts it, the station choice runs on its cadence.
   Otherwise the pod arrives through `arrive` (`internal/sim/traffic.go` `(*Simulation).publishVehicleTravel`), and the next emergency stage starts the unload at that berth.

### 5.7 Faults and emergencies

Stage 2 accepts a fault on a pod with an operational purpose (stage 2, section 1.4).
Stage 3 accepts an emergency on a faulted pod that carries passengers (product choice P16).
The emergency stage skips a faulted pod (section 5.4, step 1).

| Emergency phase at fault start | While the fault lasts | Clear before evacuation | Evacuation at rest |
| --- | --- | --- | --- |
| Deferred | The pod brakes to rest and stays deferred. | The next emergency stage acts on the pod. | `evacuate` interrupts every rider (decision 5). The record ends in the same tick. |
| Bound | The pod brakes to rest and keeps purpose 1 and its berth. | The pod continues to its emergency berth. | `evacuateLane` replaces purpose 1 with purpose 3 owned by `faultHold`. The record ends in the same tick, and `emergencyHold` is released. |
| Unloading | The phase timer stops (`internal/sim/simulation.go` `(*Simulation).Step`). | The timer runs again, and the unload finishes. | `evacuate` at the berth interrupts every rider, and `settleIdleAtBerth` clears purpose 1. The record ends in the same tick. |

| Fault state at emergency start | Result |
| --- | --- |
| Braking or stopped, with riders | Accepted. The record is deferred. `withdrawService(emergencyHold)` adds the second hold and releases nothing. |
| Evacuated, on its empty recovery or at the berth | Refused: the pod carries no passenger. |

A bound pod that a fault obstructs is a healthy pod for stage 2.
The endpoint reroute (stage 2, section 9.3) keeps its emergency berth, without the rider detour check (section 9.2).

### 5.8 Q7 fix

The fix lands in patch 1 of section 16.
The maintainer chose to fix the bug in stage 3, so the fix is not gated on the emergency marker.
It changes only runs in which a withdrawn pod or a pod with a purpose would have been discovered or adopted for coupling, and such a pod exists only with the incident or fault marker (section 12).
The record term of revision 7 changes only runs in which a pod that a record names would have been discovered, and a record exists only with the emergency marker.

| Site | Change |
| --- | --- |
| `discoverCouplingApproaches` (`internal/sim/coupling_approach_runtime.go` `(*Simulation).discoverCouplingApproaches`) | Skip a front or rear pod with `withdrawn != 0` or `op.purpose != opService`, or that a record names (`emergencyOf`). |
| `prepareCouplingAdoption` (`internal/sim/coupling_approach_runtime.go` `(*Simulation).prepareCouplingAdoption`) | Return `couplingDenied("member is out of service")` when either member has `withdrawn != 0` or `op.purpose != opService`, before `planCouplingReservation`. The denial continues the adoption loop (`internal/sim/coupling_step.go` `(*Simulation).planNativeCouplingTick`). |
| `couplingApproachContext.changed` (`internal/sim/coupling_approach_context.go` `(*couplingApproachContext).changed`) | Return a reason when either member is withdrawn, has a purpose, or has a record. |
| `CheckContract` | Invariant E6. |

Stage 2 skips a faulted pod at discovery (`internal/sim/coupling_approach_runtime.go` `(*Simulation).discoverCouplingApproaches`).
The Q7 skip is wider: it also covers a pod in its fault recovery (purpose 3), which is withdrawn and not faulted, and a pod with `emergencyHold`.

The discovery skip also covers a pod that a record names (revision 7).
Discovery runs at the start of `Step`, before the emergency stage.
A group member with a record has no hold until it leaves its group (section 5.6), so it has no hold at that discovery.
Without the record term, discovery could recruit such a pod when it is the front or the rear of a platoon pair at that discovery.
Then `changed` aborts that approach at the approach plan of the same tick, and the pod gets its hold after the approach ends.
After a split, the members have no platoon link (`internal/sim/coupling_step.go` `(*Simulation).finishNativeCoupling`), and a new link forms only after the emergency stage, so no observed run reached this case.
The term is a guard for a pod that has a platoon link at that discovery.
With the term, the emergency stage of the same tick withdraws the pod.
Adoption needs no record term: `changed` aborts each approach whose member has a record before the pair is ready for adoption.
A record exists only with the emergency marker, so the term changes nothing without the marker (section 12).

The reason is recruitment and adoption alone.
A train member is outside every incident transition: `withdrawService` and `restoreService` refuse it (`internal/sim/service_withdrawal.go` `(*Simulation).withdrawService`, `(*Simulation).restoreService`), the operational operations refuse it (`internal/sim/operational.go` `(*Simulation).operationalMember`), and the hold release rule skips it (`internal/sim/faults.go` `(*Simulation).faultStage`).
A pod that is withdrawn or has a purpose is already inside an incident transition, so a train that recruits it holds that transition until the retirement.
After the retirement, the pod arrives through `arrive` (`internal/sim/traffic.go` `(*Simulation).publishVehicleTravel`), which clears purpose 3 (`internal/sim/simulation.go` `(*Simulation).arrive`), so the purpose ends then.
The fix removes that delay and keeps each out-of-service pod out of a train.

Saves.
E6 holds at every boundary, and the pre-tier checks of section 11.5 apply it to each save.
A save that holds a withdrawn pod or a pod with a purpose in a coupling group is invalid, also without the emergency marker.
Such a save moves aside, and the server starts a new session; there is no compatibility before v1 (`AGENTS.md`).

The adoption guard does not go in `couplingMemberEligibility` (`internal/sim/coupling_reservation.go` `couplingMemberEligibility`), because `prepareRemainingCouplingMotion` (`internal/sim/coupling_remaining.go` `prepareRemainingCouplingMotion`) also calls it for a train that already exists.
A deferred coupling member with a record has no hold and no purpose, so the train continues.

## 6. Preconditions

### 6.1 Start

`Emergency` checks these preconditions in this order:

| # | Precondition | Error |
| ---: | --- | --- |
| 1 | `emergenciesOn` | `emergencies are not enabled` |
| 2 | A pod has `podID`. | `unknown pod` |
| 3 | No dispatch pass is active. | Internal error. A command never runs inside a pass. |
| 4 | `v.carriesPassengers()` (`internal/sim/riders.go` `(*vehicle).carriesPassengers`) | `pod carries no passenger` |
| 5 | No record names the pod. | `pod already has an emergency` |
| 6 | `len(s.emergencies) < MaxEmergencies` | `emergency limit reached` |
| 7 | `orderID == 0`, or `orderID` names an active rider of the pod. With `orderID == 0`, the party is the first active rider in `Riders` order. | `order is not aboard the pod` |
| 8 | `incidentSerial < math.MaxUint64` | `incident limit reached` |

Every target that passes these checks is supported (section 1.4).
The group and fault rules of sections 5.6 and 5.7 defer a pod; they never refuse it.

### 6.2 Advance

`advanceEmergency` calls only stage 1 operations, each with its own preconditions (stage 1, sections 4.2 and 9.3).
It calls each one only in the state that section 5.4 names:

| Call | State in which it is called | Preconditions that the state already meets |
| --- | --- | --- |
| `withdrawService(v, emergencyHold)` | Not faulted, not a coupling or approach member, `emergencyHold` not set, in the emergency stage or at a command boundary | Every precondition. It cannot refuse. |
| `startOperationalUnload(v, emergencyHold, mask)` | Purpose 0, at a berth, `Boarding`, `Continuing`, or `Unloading`, with the hold, and `mask` names the active party | All but the platoon and compact tests. A refusal leaves the pod deferred. |
| `setOperationalDestination(v, target)` | Purpose 0, `Traveling`, divertible, with the hold, and the pair of section 9.1 | All. The pair has a route by the same `assignedRoute` call that the operation makes, from the same state, under the same routing view (section 9.1). |

An occupied pod at a berth is at a passenger station, because only refuge holds riders at a parking berth (stage 1, section 9.4), and stage 3 sets no refuge.

## 7. Atomicity

Each operation checks every precondition before its first write, and returns an error with no change when one fails.
The first write of a start is `nextIncidentID`, so the arithmetic check runs before it.
After an operation returns, the state contract holds.

The emergency operations run only at these places:

- At a command boundary, under the session lock: `Emergency`.
  The entry calls `observe` once, after the operation.
  The session delivers interruptions before it releases the lock (stage 1, section 8.5); a start creates none.
- In the emergency stage (section 5.3).

Composite steps:

| Composite | Internal steps |
| --- | --- |
| Start | Preflight, ID, record and counter, `withdrawService` when the pod is not a coupling or approach member, `advanceEmergency`. |
| Advance | `withdrawService` when the hold is missing, then one of `startOperationalUnload` or the station choice and `setOperationalDestination`, both under one routing view (section 9.1). The choice and the view write no simulation state other than the no-candidate memo of section 9.1. |
| Record end | Record removal and counter, then `restoreService` when the hold is set. |

The later step of a composite cannot fail after the first succeeds, or its failure leaves a valid deferred state:

- `withdrawService` changes no route, destination, or owner (stage 1, W4), so it changes no input of the advance.
- A refusal of `startOperationalUnload` or `setOperationalDestination` changes nothing, and the record stays deferred.
  A deferred record with the hold is a valid state (invariants E1 to E8).
- `restoreService` cannot fail at a record end (section 5.5).

Refusal oracle.
Each refusal test clones the simulation before the call and compares the clone with the state after it: the exported state, `owners`, every `routeReleases` and `nextRelease`, the waiting trips with their bindings and exclusions, `incidentSerial`, the records, the counters, the holds, and the purposes, as stage 2, section 11, specifies.
The oracle also compares the routing-policy state: `congestionRouteCosts`, `nextCongestionRouteRefresh`, `congestionRoutes`, `predictiveQueues`, `predictivePodQueues`, and `predictiveQueueTick` (`internal/sim/clone.go` `(*Simulation).Clone`).
It also compares the free-flow route memo, `routes` and `routeOrder`, with a copy taken before the call, because `Clone` does not copy that memo (`internal/sim/clone.go` `(*Simulation).Clone`).
A station choice that finds no candidate passes the oracle, apart from the no-candidate memo of section 9.1.
A choice that binds the pod can make every change of a successful stage 1 installation (`internal/sim/operational.go` `(*Simulation).setOperationalDestination`): the route, the destination, the purpose, and the derived route indexes of the pod, the release of its revocable claims on the berth of its old destination, the buffer fields, and the pending and wait fields.
In that case, the routing-policy state and both route memos still stay equal.
The search counters of section 15, the search work arrays, and the no-candidate memo are not compared.

## 8. Invariants

| ID | Invariant |
| --- | --- |
| E1 | Records are in strictly increasing serial order, at most one record names each pod, there are at most `MaxEmergencies` records, and `0 <= start <= s.tick`. Each serial is positive and at most `incidentSerial`, and no fault record and emergency record have one serial. |
| E2 | With `emergenciesOn`, a pod with `emergencyHold` has a record. |
| E3 | The party of a bound or unloading record is an active rider of its pod, and `v.op.interrupt` is exactly the bit of that rider. |
| E4 | With `emergenciesOn`, a pod with purpose 1 has `op.owner == emergencyHold` and a record. |
| E5 | The pod of a bound or unloading record carries passengers. |
| E6 | No coupling member and no approach member has `withdrawn != 0` or `op.purpose != opService`. |
| E7 | Without the emergency marker, no record exists. |
| E8 | The pod of a record has no active rider when its purpose is not 0 or 1. |
| F11, state | With `faultsOn` and `emergenciesOn`, `withdrawn` is a subset of `faultHold | emergencyHold` for every pod. With `faultsOn` alone, the landed check stays: `faultHold` is the only hold (`internal/sim/faults.go` `(*Simulation).checkFaults`). |
| F11, transition | With `emergenciesOn`, a pod gains `emergencyHold` only in `Emergency` or in `advanceEmergency`, and only while a record names it. A pod keeps `emergencyHold` until its record ends. |

E2 and E4 hold only with the marker, as F11 holds only with `faultsOn`: without the marker, the stage 1 test entry `IncidentForTest` (`internal/sim/incident_entry.go` `(*Simulation).IncidentForTest`) can set `emergencyHold` and purpose 1 with another owner, and the stage 1 rules W1 to W5 govern them.
`CheckContract` (`internal/sim/state_contract.go` `(*Simulation).CheckContract`) checks E1 to E8 and the state part of F11 after each tick and each command.
`CheckContract` reads one state, so it cannot check where a hold came from.
A transition helper checks the transition part of F11: it compares each pod before and after each tick and each command.
It fails when a pod gains `emergencyHold` and no record names it after the change, and when a pod loses `emergencyHold` while its record stays.
The rule that only the two operations set the hold is a code property: they are the only callers of `withdrawService(v, emergencyHold)`, and a source test lists the callers.
The record end tests check the end conditions of section 5.5.

Production policy.
E2 and E4 hold only with the marker, so an incident save without the emergency marker that has `emergencyHold` or purpose 1 passes them and W1 to W5.
`CheckIncidentPolicy(input RestoreStateInput) error` is the production policy validator.
It reads only the saved state.
With the incident marker and without the emergency marker, it rejects:

- a saved pod with hold 2 (`emergencyHold`) in its withdrawn set or as its owner;
- a saved pod with purpose 1, because no record exists without the marker (E7).

With the marker, `checkSavedEmergencies` rejects the same states by E2 and E4 (section 11.5).
No production path makes these states without the marker: the `Emergency` command needs the marker, and `IncidentForTest` has no production caller.
The session restore runs the validator before either tier, as a step of `restoreSteps` (`internal/session/persist.go` `restoreSteps`) that `NewFromStore` sets.
A failure is `invalid_state` with no logical fallback, and the save moves aside.

The stage 1 primitive validator (`checkPodOperational`, `internal/sim/state_contract.go` `checkPodOperational`), `sim.RestoreState`, `CheckContract`, and `IncidentForTest` do not change.
The stage 1 fixtures therefore still make these states and restore them through `sim.RestoreState`.
The fixture exception is in one place: the session restores of `internal/session/incident_save_test.go` that load such a save call `newFromStore` with no policy step, and a session test checks that `NewFromStore` rejects the same saves.
The stream and HTTP decoders keep the stage 1 rules, because the stage 1 stream tests of that file send frames with hold 2 and purpose 1 without the marker, and only a fixture makes such a frame.

| Invariant | Mutation | Check that must fail |
| --- | --- | --- |
| E1 | Append a second record for one pod. | `CheckContract`: duplicate pod. |
| E1 | Append a record out of serial order, or with serial 0. | `CheckContract`: serial order, positive serial. |
| E1 | Give an emergency record the serial of a fault record. | `CheckContract`: shared serial. |
| E1 | Skip the cap check at the start. | Start refusal test, then `CheckContract`. |
| E2 | Remove a record and keep `emergencyHold` (orphan hold). | `CheckContract`. |
| E2 | End a record without `restoreService`. | `CheckContract` after the end. |
| E3 | Bind with the bit of another rider. | `CheckContract`: wrong interrupt bit. |
| E4 | Bind purpose 1 with owner `faultHold`. | `CheckContract`: wrong purpose owner. |
| E4 | End a record while the pod keeps purpose 1. | `CheckContract`. |
| E5 | Clear the riders of a bound pod and keep purpose 1. | `CheckContract`: E5. |
| Record end | Keep a bound record after `evacuateLane`. | The post-stage assertion of the evacuation test: after the emergency stage of the evacuation tick, the record is absent and the pod has no `emergencyHold`. E5 does not detect it, because `evacuateLane` replaces purpose 1 with purpose 3 (`internal/sim/operational.go` `(*Simulation).evacuateLane`), so the record is no longer bound. |
| E6 | Drop the Q7 discovery skip or the adoption guard. | `CheckContract` in the Q7 recruitment test. |
| E7 | Start an emergency without the marker. | `CheckContract` and the off-state golden. |
| E8 | Give a record pod purpose 2 with an active rider and a held owner. | `CheckContract`: E8. `checkPodOperational` accepts this pod. |
| E8 | Drop the E8 rule from `checkSavedEmergencies`. | Pre-tier restore test with such a record and a physical error that would fall back to the logical tier. |
| Policy | Drop the policy step from `NewFromStore`. | Session restore test: a save with hold 2 and no emergency marker restores. |
| Policy | Run the policy validator inside `sim.RestoreState`. | The stage 1 restore test of the fixture hold (`internal/sim/operational_test.go` `TestOperationalRestoreTiers`). |
| F11, state | Allow a third hold bit. | `CheckContract`. |
| F11, transition | Set `emergencyHold` without a record. | Transition helper and `CheckContract` (E2). |
| F11, transition | Release `emergencyHold` in the stage while the record stays. | Transition helper. |
| F11, transition | Add a caller of `withdrawService(v, emergencyHold)` outside the two operations. | Caller source test. |

A deferred record can name a party that is no longer an active rider, between an alight or an interruption and the next emergency stage, so E3 and E5 do not cover the deferred phase.
A record pod that is not a coupling or approach member can lack `emergencyHold` only between the end of its group, at the end of a tick, and the next emergency stage.
A test helper checks after each tick that each record pod with no group membership in the previous tick has the hold.

W5 of stage 1 does not change: each purpose has a held owner.

## 9. Routing and priority

### 9.1 Station choice

The default rule is product choice P13: the soonest estimated arrival over the reachable passenger stations, chosen once.

Candidates.
For each station `S` of the network, in network order, that is not `ParkingOnly`:

1. Entry groups.
   With banks, each bank entry of `S` (`berthEntry`, `internal/sim/station_banks.go` `(Station).berthEntry`) is one group, as the berth search of `internal/sim/berths.go` `(*Simulation).stationRouteByLoad` groups them.
   Without banks, `S` is one group.
   A group holds the berths of the entry that allow the class of the pod (`berthAllows`, `internal/sim/class_routes.go` `berthAllows`) and are not blocked (`berthBlocked`, `internal/sim/blocked_routes.go` `(*Simulation).berthBlocked`).
   Its order is the berths that are available to the pod (`berthAvailableTo`, `internal/sim/released.go` `(*Simulation).berthAvailableTo`) first, then the others, each part in `S.Berths` order.
2. Routes.
   `prefix, from := divertStart(v)`.
   For each group, try its berths in order with `suffix, err := assignedRoute(v, from, b.Node)` (`internal/sim/routes.go` `(*Simulation).assignedRoute`).
   The first berth with a route is the candidate of the group.
   A failed route only moves the search to the next berth of the group, as the berth search continues after an unreachable berth (`internal/sim/berths.go` `(*Simulation).stationRouteByLoad`).
3. Exclusion.
   `S` has no candidate only when no group of `S` has one.
4. Route.
   The candidate route is `R = v.Route[:prefix] + suffix`.
   This is the route that `setOperationalDestination` installs for `(S, b)` under the same routing view (`internal/sim/operational.go` `(*Simulation).setOperationalDestination`).

Each candidate pair `(S, b)` gets the estimate below, and the choice is the pair with the lowest `E`.
The order of available berths first selects the candidate within a group.
It is not a term of `E`: the estimate still has no berth term (product choice P13).

Estimate.
`E(S, b)` is the cost function of `queueCost` (`internal/sim/routing_policy.go` `(*Simulation).queueCost`), started at the position of the pod on `R`:

- The position is the route distance `v.distance`.
  `R` keeps the prefix from route index 0, so `v.distance` has the same meaning on `R` as on `v.Route`.
- `c` is the lane of `R` that contains the position, `σ(l)` is the edge seconds of lane `l` in `routingGraph()` (`internal/sim/blocked_routes.go` `(*Simulation).routingGraph`), and `f` is the part of lane `c` after the position, as a fraction of its length.
- The current lane costs `f·σ(c)` plus `queueHeadwaySeconds` (3 s, `internal/sim/routing_policy.go` `queueHeadwaySeconds`) for each pod ahead of the pod on lane `c` that `queueDischarge` would count.
- Each later lane `l` costs `σ(l) + queueDelay(discharge[l], at(l))`, where `discharge` is `queueDischarge(v)` (`(*Simulation).queueDischarge`) and `at(l)` is the cost of the lanes of `R` before `l`, from the position.
- `E(S, b)` is the sum of the costs of the current lane and the later lanes.

The estimate has one time base, the edge seconds of the routing graph, and one coordinate, the route distance on the route that the pod gets.
It has no berth term: a berth that is not free costs nothing in `E`, and the emergency tier and terminal reevaluation take the next free berth of the station (sections 9.2 and 9.3).
It reads no other emergency, so it has no recursion.
It is a ranking heuristic: it gives no unload time guarantee.

Ties.
Compare `ceil(E · TicksPerSecond)` as an integer, then the station index, then the berth index.
The order uses no map iteration and no random value.
The key `(ceil(E · TicksPerSecond), station index, berth index)` is a total order on the candidate pairs, so its minimum does not depend on the order in which the pairs are evaluated.

Pruning.
The choice evaluates the stations in an order that lets it stop early, and its result is the result of the exhaustive scan above.

1. The prefix estimate `P` is the part of `E` that the lanes of `v.Route[:prefix]` contribute, from the position, by the formula of the estimate.
   For a lane of the prefix, `at(l)` reads only earlier lanes, so `P` is the same for every candidate.
   With an empty prefix, `P = 0`.
2. The tree is one shortest-path search from `from` to every berth node of the passenger stations, on `routingGraph()` (`internal/sim/blocked_routes.go` `(*Simulation).routingGraph`), with base edge seconds `σ(l)` and no extra cost.
   It is the existing `routeTargets` (`internal/sim/route_targets.go` `(Network).routeTargets`), with the class of the pod and `terminalBerthsOnly` false.
   It keeps only the lane filters that every route search applies: the class rule (`laneAllows`, `internal/sim/class_routes.go` `(routeGraph).laneAllows`) and the blocked set (`laneOpen`, `internal/sim/network.go` `(routeGraph).laneOpen`).
   It does not have the restrictions that remove lanes or nodes from a route search: `terminalBerthsOnly`, `bankExternal`, `allowedLanes`, `forbidden`, and `ownBerthsOnly` (`internal/sim/network.go` `(Network).routeIndexedWithWork`).
   The tree counts as one graph search.
   When `routeTargets` refuses the start node, the choice evaluates every station without the stop rule.
3. For each station `S`, `L(S) = P + min d(b.Node)` over the berths of the entry groups of `S`, where `d` is the tree distance.
   A berth for which `routeTargets` returns an error, also the error for a node that the class does not allow (`internal/sim/route_targets.go` `(Network).routeTargets`), has no finite `d`.
   When no such berth has a finite `d`, `S` has no candidate, and the choice makes no route search for it.
4. The choice visits the stations in increasing `(L(S), station index)` order.
   For each visited station, it evaluates every entry group by the candidate rule above.
   It keeps the best key so far, and `bestTick` is its first term, or no value before the first candidate.
5. It stops at the first station with `lowTick(S) > bestTick`, where `lowTick(S) = ceil(lower(L(S)) · TicksPerSecond)` and `lower(x) = x · (1 - 1e-9) - 1e-9`.
   A station with `lowTick(S) == bestTick` is evaluated, because it can win on the station index.
6. The choice uses steps 4 and 5 only when the term guard holds: `4 · len(v.Route[:prefix]) + 7 · len(s.network.Nodes) + 2 · len(s.vehicles) <= 90,000`.
   Otherwise it evaluates every station, as the exhaustive scan does.
   The prefix length is the length of the live route, which `MaxLanes` does not bound: a pod route can grow when the pod diverts or circles a full station, and only a save omits an overlong route (`internal/sim/state.go` `routeLimits`, `(*Simulation).laneIndexes`).

Proof of exactness.

1. Admissibility.
   Let `(S, b)` be a candidate and `U` its suffix.
   `E(S, b) = P + Q`, where `Q` is the cost of the lanes of `U`.
   Each lane of `U` costs at least `σ(l)`.
   A later lane costs `σ(l) + queueDelay(...)`, and `queueDelay` is `max(0, ...)` (`internal/sim/routing_policy.go` `queueDelay`).
   With an empty prefix, the first lane of `U` is the current lane with `f = 1`, and it costs `σ(l)` plus headways that are not negative.
   So `Q >= Σ σ(l)` over `U`.
   Every route search applies the class rule and the blocked set, and each other restriction only removes lanes or nodes, so `U` is a path from `from` to `b.Node` in the tree graph, and `Σ σ(l) >= d(b.Node)`.
   `b` is in an entry group of `S`, so `d(b.Node) >= L(S) - P`, and `E(S, b) >= L(S)`.
2. The bound must use base edge seconds.
   A distance under congestion costs (`internal/sim/routes.go` `(*Simulation).congestionCosts`) or forecast delays (`internal/sim/predictive_routing.go` `(*laneForecast).delay`) adds terms that `E` does not have, so it can be larger than `E` of the installed route, and it is not a lower bound.
3. Stop rule.
   `lower` and `ceil` do not decrease, so `lowTick` does not decrease in the visit order.
   At the stop, each later station `S'` has `lowTick(S') > bestTick`, and each of its candidates has `ceil(E · TicksPerSecond) >= lowTick(S')`, so its key is greater than the best key in the first term.
4. A visited station gives the same candidates as in the exhaustive scan, because the candidate rule and the routing view do not change.
   A station that is not visited has no candidate with a key below the best key.
   The minimum key is therefore the same pair, and `assignedRoute` under one view gives the same suffix.
5. Floating point.
   Let `u = 2^-53`, `γm = m·u / (1 - m·u)`, and `fl` the float64 result of an operation.
   `fl` does not decrease in each argument.
   The choice computes `P` once, and each `E` accumulation starts from that computed `P` at the end of the prefix, so `P` is the same float in `E` and in `L(S)`.
   Let `n` be the node count.
   The suffix `U` of a route search is at most three shortest paths, the source bank, the middle, and the destination bank (`internal/sim/bank_routes.go` `(Network).bankRoute`), so it has `mU < 3n` lanes.
   - Lower side.
     Let `B` be the base-only accumulation `fl(... fl(fl(P + σ1) + σ2) ... + σmU)` over the lanes of `U`.
     Each step of `E` adds `fl(σ(l) + δ)` with `δ >= 0` (`internal/sim/routing_policy.go` `(*Simulation).queueCost`), and `fl(σ(l) + δ) >= σ(l)`.
     The value of `δ` depends on the rounded accumulator, but it is never negative, so by induction on the steps, the computed `E` is at least `B`.
     A recursive sum of `mU + 1` nonnegative floats is at least `(1 - γ(mU)) ·` its exact sum, so `E >= (1 - γ(3n)) · (P + Σ σ(l))` over `U`.
   - Upper side.
     The tree relaxes each lane of a shortest path `π` of fewer than `n` lanes, so its computed `d(b.Node)` is at most the recursive float sum along `π`, which is at most `(1 + γn) · Σ σ(l)` over `π`, and `Σ σ(l)` over `π` is at most `Σ σ(l)` over `U`.
     `L(S) = fl(P + min d)`, so `L(S) <= (1 + u)(1 + γn) · (P + Σ σ(l)) <= (1 + γ(n+1)) · (P + Σ σ(l))` over `U`.
   - Together, `L(S) <= E · (1 + γ(n+1)) / (1 - γ(3n))`.
     With `n <= 5,000`, this factor is less than `1 + 3e-12`.
     `lower(L(S))` is at most `L(S) · (1 - 1e-9) · (1 + u)`, and `(1 + 3e-12)(1 - 1e-9)(1 + u) < 1`, so `lower(L(S))` is at most the computed `E` of each candidate of `S`, and the stop rule never removes a station that the exhaustive scan could choose.
   - This proof does not use the prefix length, because `P` is shared.
     The term guard of pruning step 6 still holds, and its constant does not change: it keeps `4p + 7n + 2k <= 90,000` (with `p` the prefix length and `k` the pod count), which bounds every accumulation of the choice, and `γ(90,000) < 1.0e-11` is still more than 100 times below the margin.
     It also covers an implementation that sums `E` in one pass from the position.
     When the guard fails, the choice does not prune, so no bound is needed.
6. Admissibility needs the position at or before the end of the prefix.
   `divertStart` ends the prefix at the first lane end at or after the reserved end (`internal/sim/diversion.go` `(*Simulation).divertStart`), and without a reservation the prefix is empty and the pod is at the start of `R`.
   The implementation checks this, and when it does not hold, that choice evaluates every station without the stop rule.

Routing view.
`assignedRoute` writes routing-policy state under some policies: congestion routing refreshes its costs, its deadline, and its route memo (`internal/sim/routes.go` `(*Simulation).congestionRouteForClass`), and predictive routing updates its queue history and sampling tick (`internal/sim/predictive_routing.go` `(*Simulation).predictionQueues`).
`Clone` copies that state (`internal/sim/clone.go` `(*Simulation).Clone`), and later route choices read it.
`advanceEmergency` therefore builds one immutable routing view for each choice, and the choice and `setOperationalDestination` both read it:

- `s.routeView` points to the view while it is set.
  `advanceEmergency` sets it before the choice and clears it after `setOperationalDestination` returns, or after a choice that finds no candidate.
  It is nil outside `advanceEmergency`, so no save or clone holds it.
- When `s.routeView` is set, `assignedRoute` and its policy routes read the view, and they write no routing-policy state and no route memo.
- Congestion costs.
  When `congestionRouteCosts` exists, the view uses it as it is, also when the refresh is due.
  Otherwise the view holds a lane-sized array from `congestionCosts()` (`internal/sim/routes.go` `(*Simulation).congestionCosts`), and it does not store it.
- Route memos.
  The view reads the free-flow memo (`s.routes`, `internal/sim/routes.go` `(*Simulation).cachedRouteForClass`).
  It reads the congestion memo only when it uses the stored costs, because each entry depends only on those costs, the network, the class, and the blocked set, and the blocked set rebuild clears both (`internal/sim/blocked_routes.go` `(*Simulation).startRouteEpoch`).
  A miss runs the search, and the view does not write the result.
- Queue discharge.
  The view holds `queueDischarge(v)` (`internal/sim/routing_policy.go` `(*Simulation).queueDischarge`), which reads the pods and writes nothing.
- Predictive state.
  The view holds a lane-sized raw sample, by the rule of `predictionQueues`, and a lane-sized history array.
  When `predictiveQueues` has the lane count, the history array is `max(0, predictiveQueues[i] - h[i])`, where `h` is the per-pod history of the emergency pod in `predictivePodQueues`, or zero when it has none.
  When the history does not exist, the history array is zero, so the forecast uses zero history combined with the current sample.
  The raw sample excludes the emergency pod by the rule of `routeForecasts` (`internal/sim/predictive_routing.go` `(*Simulation).routeForecasts`).
  The view builds the forecasts once from the two arrays and the planned arrivals of the other pods.
  The history is not decayed to the current tick, and `predictiveQueueTick` does not change.

Installation under the view is not an ordinary assignment in the same tick.
An ordinary assignment later in the tick refreshes overdue congestion costs and advances the predictive history before it searches, so it can give another route to the same pod and berth.
The difference is deterministic, because the view is a function of the state at the start of the choice.
Its sums run in lane, pod, or sorted key order, and `congestionCosts` adds 6 s for each owned track lane in map order (`internal/sim/routes.go` `(*Simulation).congestionCosts`), which gives the same sum in any order.
It is safe, because a route grants no resource, and the traffic tier grants each resource of the installed route by the rules for any route.
The next ordinary assignment makes the refresh and the history update that the view did not make, so no update is lost.

The choice and the installation read one view, so the installed suffix equals the suffix of the candidate.
The view can keep the suffix of the chosen pair for the installation, with the same result.
With `s.routeView` nil, the routing code is unchanged, so the view changes nothing without an emergency.

Commitment.
The chosen pair binds the pod, and the station never changes.
`setOperationalDestination` saves the commitment in the purpose of the pod tuple.
Terminal reevaluation can still change the berth within the station (section 9.2).

Cost.
A choice runs at the start, and in the emergency stage only on ticks with `(s.tick - start) % 60 == 0` while the record is deferred and the pod is divertible.
A choice that binds the pod ends the searches of its record, so repeated choices happen only while a deferred, divertible pod finds no candidate.
The bound counts graph searches:

| Unit | Bound | Source |
| --- | ---: | --- |
| Graph searches in one route search | 4: a same-bank local attempt that fails, then the source bank, the middle, and the destination bank | `internal/sim/bank_routes.go` `(Network).bankRoute` |
| Route searches in one `assignedRoute` call, free flow | 2: the terminal-berth search and its fallback | `internal/sim/routes.go` `(*Simulation).cachedRouteForClass` |
| Route searches in one call, congestion, queue, or predictive | 3: the free-flow pair and the policy search | `internal/sim/routes.go` `(*Simulation).congestionRouteForClass`, `internal/sim/routing_policy.go` `(*Simulation).queueRoute`, `internal/sim/predictive_routing.go` `(*Simulation).predictiveRoute` |
| Graph searches in one `assignedRoute` call | 12 | The rows above |
| Graph searches for the tree | 1 for each choice | Pruning, step 2 |
| Calls in one choice | One for each tried berth, plus one in `setOperationalDestination` | Steps 2 and 4 of the candidates |
| Tried berths in one choice | At most the compatible, unblocked berths of the passenger stations that the tree reaches. Each berth is a network node, so at most 5,000. | `internal/project/config.go` `MaxBerths` |
| Choices in one tick | `MaxEmergencies` = 4. Records that started on ticks with one remainder modulo 60 choose on the same ticks. | Section 4.3 |

The worst tick therefore makes at most `4 · (1 + 5,001 · 12) = 240,052` graph searches, on a network of at most 5,000 nodes and 8,000 lanes (`internal/project/config.go` `MaxNodes`, `MaxLanes`).
Pruning does not lower this bound, because in a network where the tree reaches every berth and the restricted searches refuse each one, no station is pruned.
The view does not write the free-flow memo, so the installation repeats the searches of the chosen pair unless the view keeps its suffix.

Latency.
One search on a network at the project limits was measured with the existing search code:

- Network: a one-way street grid of 50 by 100 nodes, so 5,000 nodes and 7,375 lanes.
  Rows alternate direction, a vertical street runs on each even column with alternating direction, and the speeds are 14, 14, and 10 m/s in turn.
- Search: from one corner to the far corner of the grid, by `routeIndexed` (fresh work arrays) and by `routeIndexedWithWork` (reused work arrays).
  The unreachable case blocks the lanes into the target, so the search visits every node that it can reach, as the tree does.
- Machine: AMD Ryzen 5 3600, Go 1.27.1, at `nice -n 19`, with a load average of about 14 on 12 threads.
  The load makes these times high.

| Case | `GOMAXPROCS` | Fresh | Reused |
| --- | ---: | ---: | ---: |
| Reachable | 2 | 0.71 to 0.95 ms | 0.53 to 0.57 ms |
| Unreachable, full visit | 2 | 0.58 to 0.61 ms | 0.51 to 0.52 ms |
| Reachable | 1 | 0.53 to 0.54 ms | |
| Unreachable, full visit | 1 | 0.53 to 0.55 ms | |

For scale, the recorded benchmark of the same code on 400 nodes and 1,520 lanes takes 13.5 to 15.7 µs (`BenchmarkRouteSearchWork`, `internal/sim/route_work_test.go` `BenchmarkRouteSearchWork`).
The CI runner class (`ubuntu-26.04`) was not measured.

Threshold: the worst choice tick takes at most 50 ms on the CI runner class (product choice P23).
That is three tick intervals of 16.7 ms (`internal/session/session.go` `(*Session).Run`), and the session holds its lock while it advances (`(*Session).advance`).
At the slowest measured search, 0.95 ms, 50 ms is about 52 graph searches, and at 0.51 ms it is about 98:

| Tick | Graph searches | Time at 0.51 to 0.95 ms | Within 50 ms |
| --- | ---: | ---: | --- |
| One record, unbanked, free flow, one station tried, cold memo | 1 + 1 + 1 = 3 | 1.5 to 2.9 ms | Yes |
| Four records, no station reachable in the tree | 4 | 2.0 to 3.8 ms | Yes |
| Four records, four stations each, 12 searches for each call | 4 · (1 + 4 · 12 + 12) = 244 | 124 to 232 ms | No |
| The bound | 240,052 | 122 to 228 s | No |

Pruning makes most ticks small, but the exhaustive worst case cannot meet the threshold.
The maintainer chose to keep P13 exact and to gate the realistic cases (product choice P23).
Section 15 gates the threshold on LondonCentral and the rail-hub preset with four emergencies on one tick, and on the delayed-routes fixture at the project limits.
The adversarial no-candidate fixture is reported only, and never gated.
On such a network, a pathological first choice can stall one tick.
The no-candidate memo stops the stall from repeating while the inputs of the memo key do not change.

When no station has a candidate, the pod keeps its route and stays deferred, and the wait is the wait of product choice P11.

No-candidate memo.
A proven no-candidate result of a choice is kept, so that a retry with the same inputs makes no search.
The memo changes no result.
It reduces the work of the retries only, and the latency of the first choice does not change.

- Entry: `emergencyMiss{serial uint64, from int, class VehicleClass}`, in `s.emergencyMisses`, one entry at most for each record.
  `from` is the node index of the divert start, and `class` is the class of the pod.
- Write: a choice that completes and finds no candidate replaces the entry of its record.
  A choice finds no candidate with or without pruning only when it has evaluated every station that the tree reaches, so the result is proven.
  A choice that finds a candidate removes the entry.
  The memo never holds a winner, and a refusal of `setOperationalDestination` writes no entry.
- Read: on a cadence tick, after the ordinary checks of section 5.4, step 7, a choice whose record has an entry with the same `from` and class returns no candidate and makes no search.
  Retries stay on the cadence.
- Clear: `startRouteEpoch` (`internal/sim/blocked_routes.go` `(*Simulation).startRouteEpoch`), which a change of the blocked lanes or berths calls, clears the memo with the route caches.
  A rebuild of the network indexes (`ensureNetworkIndexes`, `internal/sim/routes.go` `(*Simulation).ensureNetworkIndexes`), `Reset`, a project replacement, and a restore also clear it, and a record end removes the entry of its record.

Proof that the memo changes no result.
A station has a candidate when a berth of one of its entry groups has a route.

1. The groups and their berths depend only on the network (passenger and parking status, berth nodes, banks, and class permissions), the class of the pod, and the blocked berths.
   Berth availability changes the order of the berths in a group, and not the set, because each group also tries the berths that are not available.
2. Under every policy, `assignedRoute` returns an error exactly when the free-flow route returns an error.
   Congestion routing falls back to the free-flow route when its search fails (`internal/sim/routes.go` `(*Simulation).congestionRouteForClass`), and its search uses the graph of the free-flow terminal search, so it succeeds only when that search succeeds.
   Queue and predictive routing return the free-flow error, and otherwise a route (`internal/sim/routing_policy.go` `(*Simulation).queueRoute`, `internal/sim/predictive_routing.go` `(*Simulation).predictiveRoute`).
3. The free-flow route depends only on `from`, the berth node, the class, the network, and the blocked set.
   The suffix starts at `from`, so a change of the position with the same `from` changes no route.
4. Queue samples, congestion costs, predictive history, planned arrivals, and the routing policy change costs and not the existence of a route, so they need no invalidation.
   Each other input is in the key or clears the memo.

#### 9.1.1 Budgeted scan, rejected

Revisions 3 to 5 proposed a budgeted scan that spread one choice over several ticks with a cursor in the record.
The maintainer considered it and rejected it on 2026-10-05, because it changes the result of product choice P13.
The contract has no cursor, no per-tick search budget, and no save bytes for them.

### 9.2 Purpose 1 amendments

Three readers treat an occupied pod as a pod with onward stops.
A purpose 1 pod has no onward stop: every party leaves at the emergency station.

| Reader | Change for `v.op.purpose == opEmergencyUnload` |
| --- | --- |
| `berthFilterForVehicle` (`internal/sim/berth_continuation.go` `(*Simulation).berthFilterForVehicle`) | Returns `nil` when the network has no class restrictions, and otherwise a filter that tests only `berthAllows` for the class of the pod. |
| `reevaluateTerminalBerth` (`internal/sim/berth_choice.go` `(*Simulation).reevaluateTerminalBerth`) | Skips the `rerouteKeepsDetours` test. |
| The endpoint reroute (stage 2, section 9.3, check 6) | Skips the rider detour check. The kept prefix and the blocked lane checks stay. |

Without these changes, the detour test runs against `v.Stops`, which no longer leads through the emergency station, and it can refuse every berth.
Each change reads only the purpose, which is 0 for every pod without a record, so off-state behavior does not change.

Terminal reevaluation stays active for a bound pod.
It moves the pod to another free, reachable berth of the emergency station before the pod commits to its branch, as it does for any passenger route.

### 9.3 Admission priority

`intent` (`internal/sim/traffic.go` `intent`) gets `emergency uint64`: the serial of the record of the pod, or 0.
`admit` sets it beside `priority` (`internal/sim/traffic.go` `(*Simulation).admit`) from the record list.
`compareAdmission` uses this order:

1. An intent with `emergency != 0` before an intent without it, also before an aged intent.
2. Among emergency intents, the lower serial, then the lower pod ID (product choice P18).
3. The existing order for every other intent, unchanged.

`grant` processes the intents in this order, so an emergency pod gets a free junction, track cell, entry resource, or berth before each other pod that requests it in the same tick.
When another pod owns a requested resource, `grant` returns before any write (`internal/sim/traffic.go` `(*Simulation).grant`), so the emergency pod waits, and no grant is revoked (decision 1).
A deferred record pod also gets the tier, because it moves toward the berth where it unloads.

"Alternate entries" in decision 1 is the station choice: the estimate compares the stations and the entries of their routes, and terminal reevaluation compares the free berths of one station.
No new mechanism changes a buffer position or a compact queue order.
`bufferClaimCanYield` (`internal/sim/station_buffer_claim.go` `(*Simulation).bufferClaimCanYield`) does not change, so a buffered emergency pod waits for its turn as the buffer head.

### 9.4 Claim surrender gate

`incidentOutstanding()` (stage 2, section 9.5) is also true when an emergency record exists or a pod has `emergencyHold`.
This is the extension that stage 2, section 15, names.
A pod with an empty relocation that waits and owns an unused service claim on the berth of a bound pod then gives the claim up before the grants of the same `admit`.
The surrender itself does not change.

### 9.5 Liveness

The emergency tier removes the 10-second age protection (`internal/sim/traffic.go` `(*Simulation).admit`) of each intent that competes with an emergency intent.
This contract makes only the following claims:

- A denied emergency grant writes nothing.
  It takes no resource that another pod owns, and it does not make another pod leave its reserved blocks.
- The cap of `MaxEmergencies` bounds the number of intents in the tier at each tick.
  It does not bound the time for which a record stays.
- An emergency pod finishes its unload when its route admits it under the conditions of stage 2, section 9.7: owners move and release their resources, receiving capacity becomes available, and phase timers end.
  A reservation cycle or a berth that never frees has no guaranteed end in stage 3.
- When no record starts after a tick `t0`, and each record from `t0` ends, the tier is empty after the last end, and ordinary admission returns to its existing order.
- With a sustained start rate, no finite wait is claimed for ordinary intents.

When a condition fails, the system stays safe, and the pod reports its existing wait reason.
The `emergencies` group shows the phase of each record (section 11.4).

| Wait of an emergency pod | Event that ends it |
| --- | --- |
| Deferred coupling member | The split at the committed split site. |
| Deferred approach member | The abort braking ends. |
| Deferred platoon member | The follower holds no resource of a pod ahead, and the link ends. |
| Deferred compact member | The pod leaves the group. |
| Deferred, divertible, cadence | The next tick with `(s.tick - start) % 60 == 0`. |
| Deferred, no candidate station | A clear or an owner movement that opens a route; see P11. |
| Deferred, faulted | The clear or the evacuation. |
| Deferred, in the arrival chain | Arrival at the current berth. |
| Bound, waiting for a resource | Its owner moves and releases it. |
| Bound, berth occupied | The phase timer of the pod at the berth ends, and the berth clears, or terminal reevaluation moves the pod to another free berth. |
| Unloading | The phase timer ends. |

## 10. Settings, commands, and session events

### 10.1 Project settings

```json
"incidentContract": "incident-v1",
"emergencyContract": "emergency-v1",
"emergencies": {"perHour": 0}
```

| Member | Type | Range | Default |
| --- | --- | --- | --- |
| `perHour` | number | 0 in stage 3. 0 to 60 from stage 7. | 0 |

Rules:

- `emergencyContract` requires `incidentContract`.
  `Validate` (`internal/project/config.go` `Validate`) refuses the emergency marker without it.
  It does not require the fault marker.
- With the marker, `emergencies` is required.
  Without the marker, `emergencies` is refused, also as null or an empty object.
- Unknown members and null values are refused.
- `perHour` above 0 is refused in stage 3.
- `Validate` adds the widest `emergencies` object to its size estimate, as for `faults` (stage 2, section 12.1).
- The marker scan (`internal/project/service.go` `scanProjectFields`) records `emergencyContract` and `emergencies`, as it does for the incident marker.
- `web/editor.js` keeps both members of a loaded project and writes them back unchanged.
  The editor gets no control for them in stage 3.
- `internal/parkride` and `cmd/compare` keep refusing a project with the incident marker, so they also refuse the emergency marker.

The simulation gets `SetEmergencies(enabled bool) error`, beside `SetFaults` (`internal/sim/faults.go` `(*Simulation).SetFaults`).
It refuses to turn emergencies off while a record exists.

### 10.2 Sampler mechanism, for stage 7

Stage 3 does not run the sampler, because `perHour` is 0.
This section fixes the mechanism, so that stage 7 adds the rate without a new format decision.

- The sampler belongs to the simulation, with its own `math/rand/v2` PCG stream.
  It never draws from the demand PCG or from the fault sampler.
  Its seed is the first 16 bytes of `SHA-256("podsim-emergencies-v1" || bigEndian(demand seed))`, as two `uint64` values, as stage 2, section 12.2, derives the fault seed.
- Events form a Poisson process at `perHour` for the whole fleet.
  After each event, and at start, the sampler draws `u` and sets `nextTick = tick + max(1, ceil(-ln(1-u) × 216000 / perHour))`.
- Each event uses exactly two more draws: the pod, as an index over the pods in pod ID order, and the party, as an index over the active riders of that pod in `Riders` order.
- An event whose pod fails a precondition of section 6.1 is skipped and counted.
  It still uses its draws, so later events do not depend on outcomes.
- The sampler runs in the emergency stage, before the records.
- Stage 7 adds the sampler members `/simulation/emergencies/random` and `/simulation/emergencies/nextTick` and their bytes.

### 10.3 Commands

| Action | Members | Effect | Reply |
| --- | --- | --- | --- |
| `emergency` | `podID`, optional `orderID` | Starts an emergency (section 5.2). | `emergencyID` |

A paused session accepts the action.
While a coupling fault is retained, the action is refused, as other actions are (`internal/session/session.go` `(*Session).apply`).
An exact retry returns the stored reply, as for every command.

`Command` (`internal/session/session.go` `Command`) gets `OrderID int` with `json:"orderID,omitzero"` and a digest tag (section 11.2).
`PodID` is the field of stage 2, section 12.3.
`Reply` gets `EmergencyID`, omitted when empty.
Receipt replies are not saved, so the reply member adds no save bytes.

There is no command that cancels an emergency (product choice P22).

### 10.4 Errors

Every error is `command_rejected` with one of these messages:

| Message | Cause |
| --- | --- |
| `emergencies are not enabled` | The project has no emergency marker. |
| `unknown pod` | No pod has `podID`, or the command has no `podID`. |
| `pod carries no passenger` | Precondition 4. |
| `pod already has an emergency` | Precondition 5. |
| `emergency limit reached` | Precondition 6. |
| `order is not aboard the pod` | Precondition 7. |
| `incident limit reached` | Precondition 8. |

A refused command changes nothing (section 7).

### 10.5 Counters

`emergencyCounters` holds three `int64` counters, each omitted at 0 in every format:

| Counter | Meaning |
| --- | --- |
| `started` | Records started. |
| `ended` | Records ended by section 5.5, not by a restore or a reset. |
| `emergencyTicks` | The sum over the emergency stages of the number of records after the stage. |

Each counter saturates at `math.MaxInt64`, as the fault counters do (stage 2, section 12.5).
`emergencyTicks / ended` is the mean record duration in ticks while no record is active.
The interrupted party and the transferred parties are in the stage 1 counters.
The full metric set is product choice P20 and lands in stage 7.

### 10.6 Game control

The pod inspector (`internal/view/game.go` `(*Game).inspectionRows` (at 6227cf7)) gets one button when the topology has the emergency marker: "Emergency" on a pod that carries passengers and has no record.
It sends `emergency` with the pod ID and no `orderID`, so the party is the first active rider.
The view refuses nothing on its own: a refused command shows the existing command error.
The control is product choice P21.

### 10.7 Session events and restore tiers

| Event | Effect |
| --- | --- |
| Pause | Ticks stop, so the stage and the cadence stop. Commands still run. |
| Reset | `Reset` (`internal/sim/simulation.go` `(*Simulation).Reset`) clears the records, `emergencyHold`, and the counters. `incidentSerial` stays. |
| Demo | The demo project has no emergency marker. Records end as on reset. |
| Project apply | Any change to the emergency marker or to `emergencies` fails `sameExceptCouplingEnabled` (`internal/session/session.go` `sameExceptCouplingEnabled`), so the fleet is rebuilt and every record ends. An identical project keeps the records. |
| Checkpoint and rewind | `Clone` (`internal/sim/clone.go` `(*Simulation).Clone`) deep-copies the records and the counters. A rewind restores them exactly. The new generation gives new records a new ID prefix. |
| Physical restore, every record pod keeps its place | Records and counters are restored, after the pre-tier checks of section 11.5. The phase comes from the restored purpose. A deferred record runs its advance in the next emergency stage. |
| Physical restore that would demote a record pod (`internal/sim/state_physical.go` `(*physicalRestore).placeDemoted`) | The physical tier fails. The restore then follows the fallback rules of stage 1, section 9.6, as stage 2, section 12.7, does for a faulted pod. No single record is cancelled to keep the physical tier. |
| Logical restore | Every record ends. The stage 1 logical tier does not change: it builds a fleet with no purposes, handles a saved purpose 1 (stage 1, section 9.6), and copies each saved hold (`internal/sim/state_logical.go` `restoreLogical`). With the emergency marker, the tier then calls `restoreService(v, emergencyHold)` for each pod with the hold, after the purposes are cleared, so no withdrawn pod is left without a record (E2). Without the marker, `sim.RestoreState` keeps the holds as stage 1 restores them, but the session policy validator rejects such a save before either tier (section 8), so only the stage 1 fixture tests reach that path. Counters are kept. `RestoreResult` (`internal/sim/state.go` `RestoreResult`) gets `DroppedEmergencies`, the number of records that ended. |
| Invalid emergency data, a save that breaks E6, or a save that the policy validator rejects | The pre-tier checks reject the whole save as `invalid_state`, with no logical fallback, and it moves aside (section 11.5). |

## 11. Formats

### 11.1 Marker and its propagation

The project gets one top-level feature marker, `emergencyContract: "emergency-v1"`.
It requires the incident marker (stage 1, section 11.2).
It gates every stage 3 member.

| Carrier | Member | Rule |
| --- | --- | --- |
| Save | `/project/emergencyContract` | Source of truth. `RestoreState` gets it with the other contract inputs. |
| Topology, stream hello, and `GET /api/topology` | `TopologySnapshot.emergencyContract` (`internal/session/protocol.go` `TopologySnapshot`) | Copied from the project. |
| Full frame and `GET /api/state` | `SimulationFrame.emergencyContract` (`internal/session/protocol.go` `SimulationFrame`) | Copied from the simulation. |
| Agreement | A new `emergencyFrameBinding`, beside `incidentFrameBinding` (`internal/session/protocol.go` `incidentFrameBinding`) | Rejects a frame whose emergency marker differs from the topology marker. |
| Raw presence | `scanIncidentMembers` (`internal/session/incident_stream.go` `scanIncidentMembers`) | Also records the `emergencies` member name and group key, with any value. A delta or a full envelope with one and no emergency marker is rejected. |
| Assembler | `StreamAssembler.State` | Rejects an emergency marker change inside one stream. |
| Web | `markersAgree` in `web/editor.js` and `web/shell.js` | Also needs the topology and the simulation to have the same emergency marker, absent or `emergency-v1`. |

### 11.2 Digest registry entries

| N | Field path |
| ---: | --- |
| 4 | `Command.PodID`, from stage 2, section 13.2 |
| 10 | `Command.Project.EmergencyContract` |
| 11 | `Command.Project.Emergencies` |
| 12 | `Command.OrderID` |

Each field sits at a fixed path, as stage 1, section 11.3, requires.
`Emergencies` is a pointer to a struct, so the extension pair encodes the whole object, and its inner field is not tagged.
A command with no set extension field keeps its digest.
`digestExtensions` (`internal/session/receipt.go` `digestExtensions`) gets the three new rows after the stage 2 rows.

### 11.3 Save members

| Path | Shape | Presence |
| --- | --- | --- |
| `/project/emergencyContract` | `emergency-v1` | With the marker only. |
| `/project/emergencies` | Object, section 10.1 | Required with the marker. Refused without it. |
| `/simulation/emergencies` | `{"records": [...], "counters": {...}}` | With the marker only. Omitted when there is no record and every counter is 0. |
| `/simulation/emergencies/records/*` | `[generation, serial, start, pod, order]` | `records` is omitted when empty. |
| `/simulation/emergencies/counters` | Object of the counters of section 10.5 | Each counter is omitted at 0. `counters` is omitted when every counter is 0. |

`pod` is an index into `/simulation/pods`, and `order` is the order ID of the party.
The tuple uses an index for the reason of stage 1, section 11.7.
Every tuple element is always present, so index 0 is never omitted.
The phase, the mask, and the commitment are in the pod tuple of stage 1 (`withdrawn` and `operational`), so no other member is needed.

### 11.4 Stream and HTTP state members

| Path | Shape | Delta group |
| --- | --- | --- |
| `.../simulation/emergencyContract` | `emergency-v1` | None. Full frames only. |
| `.../simulation/emergencies` | `{"active": [...], "counters": {...}}` | New group `emergencies`, present only with the emergency marker. |

Each element of `active`, in serial order:

```json
{"id": "i3.21", "podID": "p7", "orderID": 412, "phase": "bound", "startTick": 1200}
```

- Every member is required.
- `phase` is `deferred`, `bound`, or `unloading` (section 4.2).
- The group changes only at a start, at a phase change, at an end, and at a counter change.
  `emergencyTicks` changes on each tick while a record exists, as a counter of the group.
- `active` and `counters` are omitted when empty, so the group with the marker and no emergency is `{}`.

Vehicles get no new member.
`withdrawn` and `operational` already show the hold and `emergency-unload` (`internal/sim/simulation.go` `Vehicle`).

### 11.5 Strict decoding and prescan

Every decoder rejects unknown members.
Without the emergency marker, each stage 3 member is rejected, also as an explicit null, an empty object, or an empty array.

Pre-tier checks.
`checkSavedEmergencies(input)` runs before either restore tier, beside `checkSavedBankRoutes` and `checkCompactFields` (`internal/sim/state.go` `restoreState`).
It reads only the saved records, the saved pod tuples, and the saved coupling groups, so the logical tier cannot remove the evidence that it needs.
A failure rejects the whole save as `invalid_state`, with no logical fallback, and the save moves aside.

With the marker, it rejects:

- a tuple that does not have 5 numbers;
- a pod index out of range, or two records for one pod;
- records not in strictly increasing serial order, a serial of 0, or a serial above `incidentSerial`;
- a serial that a fault record also has;
- a negative `start`, a `start` above the saved tick, or a negative counter;
- an `order` that is not positive;
- more than `MaxEmergencies` records;
- a saved pod with hold 2 (`emergencyHold`) and no record (E2);
- a saved pod with purpose 1 whose owner is not 2 or that no record names (E4);
- a record whose pod has purpose 1 and whose party is not an active rider of the saved pod, or whose interrupt set is not exactly the bit of the party (E3);
- a record whose pod has purpose 1 and no active rider (E5);
- a record whose pod has a purpose other than 0 or 1 and an active rider (E8).

With or without the marker, it rejects a save in which a member of a saved coupling group has a hold or a purpose (E6).
Such a save was valid before stage 3 (`internal/sim/coupling_restore.go` `checkCouplingRestoreInput`), and there is no compatibility before v1 (section 5.8).
Approaches are not saved, so E6 needs no approach check in a save.

Without the emergency marker, the session runs `CheckIncidentPolicy` before either tier (section 8).

With the marker, the stream and HTTP decoders apply these rules to a full frame and to an `emergencies` replacement group alike:

- every member of section 11.4 present and not null;
- `id` of the form `i<uint64>.<uint64>`, with a positive serial, unique, and with serials strictly increasing in order;
- no serial shared with a record of the `faults` group of the same frame, or of the assembled state after a delta, when stage 2 has its stream members;
- at most one record for each `podID`, and at most `MaxEmergencies` records;
- `podID` known in the topology, and `orderID` positive;
- `phase` from its list;
- `startTick` not negative and not above the tick of the frame or delta that carries the record;
- counters not negative and not null.

A delta that breaks a rule is rejected as a whole, and the accepted base frame stays.

Prescan limits (`internal/session/format_limits.go` `savedLimits`, `streamLimits`):

| Path | Limit | Basis |
| --- | ---: | --- |
| `/simulation/emergencies/records` | `MaxEmergencies` | The cap |
| `/simulation/emergencies/records/*` | 5 | The tuple |
| `/full/state/simulation/emergencies/active` | `MaxEmergencies` | Same |
| `/frame/state/simulation/emergencies/active` | `MaxEmergencies` | HTTP state |
| `/delta/groups/emergencies/active` | `MaxEmergencies` | The delta group, with no `value` wrapper |
| `/active` | `MaxEmergencies` | Only when a scanner reads the replacement group alone |

The array audit of stage 1, section 11.6, derives the paths from real envelopes and fails on any array path with no explicit limit.
Each limit has a test at the limit, at the limit plus one before typed decoding, and with a gzip body that expands past the byte cap.

### 11.6 Byte budget

Status 2026-10-07: by maintainer decision, the fleet limit is 600 pods and the save cap is 100 MiB.
The budget below keeps its 300-record basis, and nobody measured it again.
The composed fixtures and the allocation test of section 14.4 were deleted, so the Formats gate of section 15 no longer applies.

The budget uses 300 records, the pod limit (`internal/project/config.go` `MaxPods`), so it holds for any cap that product choice P19 selects.
Widest encodings, with `uint64` generation and serial (20 digits), `int64` ticks and order IDs (19 digits), and pod index 299:

| Item | Widest encoding | Bytes | Count | Total |
| --- | --- | ---: | ---: | ---: |
| Record tuple | `[18446744073709551615,18446744073709551615,9223372036854775807,299,9223372036854775807]` | 87 | 300 | 26,100 |
| Tuple separators | `,` | 1 | 299 | 299 |
| Counters | Three counters of 19 digits | 96 | 1 | 96 |
| Wrapper | `,"emergencies":{"records":[]` and `,"counters":` and `}` | 41 | 1 | 41 |
| Project marker and `emergencies` | Inside the project member, which `Validate` bounds | 0 | | 0 |
| Save total | | | | 26,536 |

Save headroom from `docs/measurements/composed-worst-case-formats.json`, which has every stage 1 member at its widest, at the save cap of 83,886,080 bytes, less the stage 2 save total of 36,094 bytes (stage 2, section 13.6):

| Shape | After stage 1 | After stage 2 | Stage 3 | After stage 3 |
| --- | ---: | ---: | ---: | ---: |
| Plain | 27,898,296 | 27,862,202 | 26,536 | 27,835,666 |
| Coupling | 27,825,453 | 27,789,359 | 26,536 | 27,762,823 |
| Express | 6,596,145 | 6,560,051 | 26,536 | 6,533,515 |
| Express with coupling | 6,596,051 | 6,559,957 | 26,536 | 6,533,421 |

Stage 3 uses 26,536 bytes of the emergency allocation of 65,536 bytes (stage 1, section 11.7).

Stream and HTTP growth, with escaped pod IDs of 386 bytes and IDs of 44 bytes with quotes:

| Item | Bytes | Count | Total |
| --- | ---: | ---: | ---: |
| Record with `phase` `unloading`, a 19-digit order ID, and a 19-digit tick | 528 | 300 | 158,400 |
| Separators | 1 | 299 | 299 |
| Counters | 96 | 1 | 96 |
| Wrapper `,"emergencies":{"active":[]`, `,"counters":`, `}` | 40 | 1 | 40 |
| `emergencies` member or group | | | 158,835 |
| Frame marker `,"emergencyContract":"emergency-v1"` | 35 | 1 | 35 |
| Topology marker, HTTP state only | 35 | 1 | 35 |

HTTP state headroom at the stream cap of 68,157,440 bytes, from the same measurement, less the stage 2 growth of 211,623 bytes:

| Shape | After stage 1 | After stage 2 | Stage 3 | After stage 3 |
| --- | ---: | ---: | ---: | ---: |
| Plain | 22,739,535 | 22,527,912 | 158,905 | 22,369,007 |
| Coupling | 22,002,019 | 21,790,396 | 158,905 | 21,631,491 |
| Express | 1,549,677 | 1,338,054 | 158,905 | 1,179,149 |
| Express with coupling | 812,161 | 600,538 | 158,905 | 441,633 |

Against the stage 2 allocation of 262,144 bytes in place of its computed growth, Express with coupling keeps 550,017 bytes after stage 2 and 391,112 bytes after stage 3, so the budget also holds when stage 2 uses its whole allocation.
Full frames and deltas keep more than 10 MB of headroom in every shape after stage 1, so they fit.
Stage 3 requests a stream and HTTP allocation of 196,608 bytes.
The stage 3 format patch measures the composed shapes with every stage 1, stage 2, and stage 3 member at its widest, and lands only when every shape is under its cap.

## 12. Off-state identity

With the emergency marker absent, trajectories, save bytes, stream bytes, command digests, and the RNG sequence stay identical to the build before stage 3, except in runs in which a withdrawn pod or a pod with an operational purpose would have been discovered or adopted for coupling.
Such a pod exists only with the incident or fault marker, so those runs need one of them.
In those runs, the Q7 fix of section 5.8 keeps the pod out of the train, as the maintainer chose.
A save that holds such a pod in a coupling group breaks E6, so it is invalid and moves aside (section 11.5); there is no compatibility before v1.
`internal/sim` has no random source in stage 3.

With the emergency marker present and no emergency started, trajectories and the RNG sequence stay identical.
Bytes and digests differ only by:

- The project members `emergencyContract` and `emergencies`, the topology and full-frame markers, and the `emergencies` delta group as `{}`.
- The digest of a command whose supplied project has the marker, by the extension pairs of `N = 10` and `N = 11`.

| Change | Off-state effect | Treatment |
| --- | --- | --- |
| Emergency stage | Runs only with `emergenciesOn`. | Gated by marker. |
| `intent.emergency` and the tier in `compareAdmission` | 0 for every intent without a record, so the sort is unchanged. | Gated by state. |
| `maintainLink` and `tryLink` record tests | No record. | Gated by state. |
| `changed` approach reason | No record, no hold, and no purpose on an approach member. | Gated by state. |
| Q7 skip in discovery and adoption, and E6 | No pod has a hold or a purpose without an incident. With the incident or fault marker, it changes only runs in which a withdrawn or purpose pod would have been discovered or adopted, which is the bug that it fixes, and it makes a save with such a pod in a coupling group invalid. | Not gated, by the maintainer decision. Own patch, with its own matched-seed check. |
| Record term of the Q7 discovery skip | No record. | Gated by state. |
| Routing view | Built only inside `advanceEmergency`. With `s.routeView` nil, the routing code is unchanged. | Gated by state. |
| No-candidate memo | Written only by a station choice. | Gated by state. |
| Purpose 1 amendments of section 9.2 | Purpose 1 needs a record. | Gated by state. |
| Claim surrender gate | No record and no `emergencyHold`. | Gated by state. |
| Save, stream, and HTTP members | Written only with the marker. | Gated by marker. |
| Digest tags | No tagged field set, so no new pair. | Gated by value. |

Gates for every patch:

- The format goldens match byte for byte with the emergency marker absent.
- The digest baseline (`internal/session/testdata/command_digests.txt`) matches with the marker absent.
- Matched-seed runs on LondonCentral and the rail-hub preset, with the marker absent and present, give identical exported simulation states at every 600th tick, apart from the marker.
- Patch 1 (Q7) also runs the matched-seed check with the fault marker and no fault, which must give identical states.

## 13. API for later stages

| Function or state | Caller | Preconditions |
| --- | --- | --- |
| `Emergency(podID, orderID)` | The session command; stage 7 sampler through the internal start | Section 6.1. |
| `advanceEmergency(r)` | Stage 4 replaces step 2 of section 5.4 for a train member with a certified train interruption. Stage 6 replaces the platoon and compact waits with a group exit. | Section 5.4. A later stage removes a deferral only with its own certified transition and tests. |
| The station choice of section 9.1 | Stage 4, for a member that leaves a train early. Stage 5, if a reverse target needs an unload station. | Runs under one routing view, and writes no simulation state other than the no-candidate memo. |
| `intent.emergency` and the tier | Stage 4 may give a train with an emergency member the tier. | Section 9.3. |
| The Q7 guard and E6 | Every later stage. A stage that adopts a withdrawn pod into a train defines that transition first and amends E6. | Section 5.8. |
| `emergencyHold` in F11 and in `incidentOutstanding` | Every later stage. | Section 8, section 9.4. |

Exported entries:

| Entry | Kind | Use |
| --- | --- | --- |
| `Simulation.Emergency(podID string, orderID int) (string, error)` | Public | The session command. |
| `Simulation.SetEmergencies(enabled bool) error` | Public | The session, from the project marker. |
| `SavedState` and frame members of section 11 | Public | The session codecs. |
| `RestoreResult.DroppedEmergencies int` | Public | The session restore report. |
| `CheckIncidentPolicy(input RestoreStateInput) error` | Public | The policy step of the session restore (section 8). |

Stage 3 adds no test entry.
Its session tests start emergencies through the `emergency` command, and its `internal/sim` tests call the operations directly.

Preconditions that later stages must meet:

- A stage that withdraws a pod for a new cause amends F11 first.
- A stage that lets a withdrawn pod or a pod with a purpose join a train amends E6 first, with the arrival or split rule that clears the purpose.
- Stage 7 adds the sampler members and their bytes before it accepts `perHour > 0`.

## 14. Test plan

### 14.1 Unit tests

| Area | Test |
| --- | --- |
| Start | Each precondition of section 6.1 refuses alone, with the refusal oracle of section 7. A default party is the first active rider. A paused start at a berth starts the unload. |
| Pickups | A start releases every pending pickup of the pod with the exclusion. A released trip is served by another pod and never by the emergency pod before it boards. |
| At a berth | `Boarding`, `Continuing`, and `Unloading` pods unload at once. An `Unloading` pod with `phaseTicks >= 1` keeps its count. |
| Divertible pod | The pod binds to the station of the lowest `E`, with the installed route equal to the candidate route. `RelocatingTo` stays empty. |
| Estimate | A queue on the nearest route makes a farther station win. A pod behind the emergency pod on its lane adds no delay. Ties go to the lower station index, then the lower berth index. Two runs from one state give one choice. |
| Candidates | A station whose first compatible berth has no route but whose second berth has one stays a candidate. A banked station with one unreachable bank and one reachable bank stays a candidate through the reachable bank. An available berth is the candidate of its group before an occupied one. |
| Routing view, first use | Under each routing policy, free flow, congestion, queue, and predictive, with no congestion costs and no predictive history: the choice and the installation leave the routing-policy state of section 7 and both route memos unchanged, and the installed suffix equals the candidate suffix. Under predictive routing, a stopped emergency pod adds no delay to its own lane. |
| Routing view, overdue refresh | The same checks, with the congestion refresh due and a predictive history older than the tick: nothing refreshes, and the view uses the stored costs and history. |
| Routing view, existing history | The same checks, with current costs, a current history, and a per-pod history of the emergency pod, which the view subtracts. |
| Pruning | A randomized test compares the pruned scan with the exhaustive scan in 1,000 cases from fixed seeds. Each case has a random network with banked, unbanked, and `ParkingOnly` stations, a class rule, a blocked set, stopped pods that make queues, a routing policy, and the emergency pod at a random divertible position. Some cases have equal tick keys at several stations. Both scans run from clones of one state and give the same pair and the same installed route. The pruned scan makes fewer route searches in at least half the cases, so the stop rule runs. |
| Long prefix | A pod route with a prefix of more than 8,000 lane occurrences on a repeated loop: with the term guard holding, the pruned and the exhaustive scans give the same pair, and with a prefix that fails the guard, the choice evaluates every station and gives the same pair. |
| Warm-memo clone parity | A fixture with a filled no-candidate memo is cloned, and the clone starts with no memo. Both run one continuation. `sameState`, which excludes `emergencyMisses` and the search counters, holds after each step, and the exported trajectories are identical at every tick. |
| No-candidate memo | After a no-candidate choice, the next cadence tick with the same `from` and class makes no search. A new `from`, a blocked-set change, a network change, `Reset`, a project replacement, and a restore each make the next choice search again. A choice that finds a candidate removes the entry, and a refused installation writes none. A randomized run with and without the memo gives the same choices on every tick. |
| Bank searches | A source on the departure path of a bank and a target berth in the same bank, where the local search fails: the counters show 4 graph searches in the route search, the failed local attempt and then the departure, middle, and arrival searches, and at most 12 in one `assignedRoute` call. |
| Arrival chain | A pod that cannot divert arrives at its current berth and unloads there in the next stage. |
| Cadence | A pod that becomes divertible chooses at the next tick with `(tick - start) % 60 == 0`, and not before. |
| No candidate | Every station unreachable: the pod keeps its route and stays deferred. With the no-candidate memo, the next cadence ticks make no search until `from` or the class changes or the memo is cleared (section 9.1). |
| Outcomes | The party is interrupted, also at its own destination. Each other party completes at its destination or is transferred. A transferred party can board the emergency pod again after the end. |
| End | The record ends in the tick of the unload, the hold is released, and the pod is supply in the same `dispatch`. |
| Party leaves | A faulted pod with a deferred record: `InterruptRider` interrupts the party, and the next emergency stage ends the record before the faulted skip. |
| Coupling member | The train keeps its plan, retires at its committed split site, and no rider alights in the train. The pod is withdrawn in the next stage, before `dispatch`, and its pending pickups go to other pods with the exclusion. In its arrival chain, it arrives through `arrive` and unloads at that berth in the next stage. Otherwise it binds on its cadence. |
| Approach member | An emergency on the front, and one on the rear, each abort the approach. The pod then binds as an ordinary pod. |
| Platoon | A link with a record at either end drains and is never extended. A new link with a record pod is refused. The pod binds after the link ends. A restore during the drain keeps draining. |
| Compact member | The pod waits while in the group and acts after it leaves. |
| Faults | Each cell of the tables of section 5.7. A clear before evacuation continues the emergency. An evacuation ends the record in the same tick. |
| Q7 recruitment | Direct tests of recruitment: a pod in its fault recovery (purpose 3), a pod with `faultHold` and no purpose, and a pod with `emergencyHold`, each as the front and as the rear of a platoon pair on a coupling corridor, are not discovered. A coupling member with a record, in the tick after its split, is not discovered, and the emergency stage of that tick withdraws it. Each test fails without the skip. |
| Q7 adoption | An approach whose member gains a hold, a purpose, or a record aborts through `changed`, and `prepareCouplingAdoption` denies a pair with an out-of-service member, so the pair never forms a group. |
| Q7 restore | A save with a coupling member that has a hold or a purpose is `invalid_state` before either tier. |
| Priority | Two pods request one free junction in one tick: the emergency pod gets it, also against an aged intent. An owned resource is never taken. Two emergencies: the lower serial first. |
| Amendments | A bound pod whose berth is taken moves to another free berth of the station, and the detour check does not refuse it. The stage 2 endpoint reroute keeps the emergency berth for an obstructed bound pod. |
| Claim surrender | A waiting empty relocation gives up its unused claim on the berth of a bound pod. |
| Counters | Each counter at its maximum saturates. |
| Invariants | E1 to E8 and F11 after every tick and command in each test above. |

### 14.2 Restore tests

| Case | Expected |
| --- | --- |
| Physical restore of each phase, and of a deferred coupling, platoon, and faulted member | Records, purposes, and holds restored. The next stage continues the emergency. |
| Physical restore that would demote a record pod | The physical tier fails. The fallback follows stage 1, section 9.6. |
| Logical restore with each phase | Every record ends. `DroppedEmergencies` counts them. Counters are kept. Stage 1 rules handle the purposes. No pod keeps `emergencyHold`. |
| Logical restore without the marker, through `sim.RestoreState`, of a save with `emergencyHold` from the stage 1 test entry | The hold stays, as the stage 1 test (`internal/sim/operational_test.go` `TestOperationalRestoreTiers`) requires. |
| The same save, and a save with purpose 1 and no marker, through `NewFromStore` | `invalid_state` from the policy validator before either tier. The save moves aside. |
| Save with a record whose pod has purpose 2 or 3 and an active rider | `invalid_state` from the pre-tier checks (E8). No logical fallback. |
| Save whose record names a party that is not the marked rider of a purpose 1 pod, with a physical error that would fall back to the logical tier | `invalid_state` from the pre-tier checks. No logical fallback. |
| Save with a serial of 0, or a serial that a fault record also has | `invalid_state`. |
| Save with a record and no marker, a pod with `emergencyHold` and no record, two records for one pod, or a serial above `incidentSerial` | `invalid_state`. |
| Rewind after a start | The same record, and a new generation for new IDs. |

### 14.3 Codec tests

Each rule of section 11.5 has a rejecting test, for the save, a full frame, an HTTP state, and a delta.
The marker checks of section 11.1 each have a rejecting test.
The digest registry test covers `N = 10` to `N = 12`, with `N = 4` shared with the `fault` command.

### 14.4 Composed byte tests

Status 2026-10-07: `TestComposedWorstCaseFormats` and the allocation test `TestEmergencyByteAllocation` were deleted.
`TestFormatArraysHaveLimits` and `TestEmergencyStreamArrayLimits` keep the checks of the array limits.

`TestComposedWorstCaseFormats` (`internal/session/composed_bytes_test.go` `TestComposedWorstCaseFormats`) prescans and decodes each fixture.
Its fixtures add the stage 3 members at their widest with `MaxEmergencies` records, so every decoder accepts them, and every shape stays under its cap.

A separate encoding-only allocation test encodes 300 widest records and 300 widest stream rows, with no prescan and no decode, and checks the totals of section 11.6 against the allocations.
This keeps the budget valid for any cap up to the pod limit.

### 14.5 Command-boundary and end-of-tick saves

A save after each start, at each phase change, and at each end restores in the physical tier and passes `CheckContract`.
Paused starts are included.

### 14.6 Mutation targets

| Mutation | Test that must fail |
| --- | --- |
| Withdraw a coupling member at the start | Coupling member test: the start refuses. |
| Skip the withdrawal after the split | Coupling member test: the pod gets a pickup after the split. |
| Advance a faulted pod | Fault test: the faulted pod gets a new route. |
| Run the emergency stage before the fault stage | Evacuation test: the record ends a tick late. |
| Run the emergency stage after `dispatch` | End test: the pod is not supply in the tick of the end. |
| Release `emergencyHold` while it owns purpose 1 | W5 check. |
| Keep the record after the unload | End test. |
| Store the rider index in place of the order ID | Party test with a rider that alights before the bind. |
| Compute `E` on a route that starts at the current lane | Estimate test: the installed route differs from the candidate route. |
| Count the pods behind the emergency pod on its lane | Estimate test. |
| Search every tick | Cadence test. |
| Choose one berth for each station before the route test | Candidates test: the station with a reachable second berth is excluded. |
| Let a choice refresh the congestion costs or sample the predictive history | Routing view tests. |
| Install with a fresh routing view after the choice | Routing view test: the installed suffix differs. |
| Let a choice write the free-flow memo | Routing view test: the memo differs from its copy. |
| Read the stored history on first use | Routing view first-use test. |
| Build the tree with congestion costs or forecast delays | Pruning test: a case chooses another pair. |
| Apply the bank or berth rules in the tree | Pruning test: a case chooses another pair. |
| Stop when `lowTick == bestTick` | Pruning test with equal tick keys: the lower station index loses. |
| Bound the term count by `MaxLanes` in place of the live prefix | Long prefix test. |
| Keep the memo after a blocked-set change | Memo test: a berth that a fault clear makes reachable is not chosen on the next cadence tick. |
| Leave `from` out of the memo key | Memo test: the pod does not bind after it moves to a divert node with a route. |
| Memoize a refused installation | Memo test: the record does not try again with the same inputs. |
| Keep the memo after a restore | Memo test: the restored simulation does not search. |
| Validate the records after the logical tier | Pre-tier test with a wrong party. |
| Skip the release of `emergencyHold` in the logical tier | Logical restore test: E2. |
| Accept serial 0 | Decoder and `CheckContract` tests. |
| Change the station after the bind | Commitment test. |
| Use `berthFilterForStops` for purpose 1 | Amendment test: the bound pod finds no berth. |
| Keep the detour check for purpose 1 | Amendment test. |
| Put the tier after the age rule | Priority test with an aged intent. |
| Let the tier take an owned resource | Priority test: the owner's grant changes. |
| Change `bufferClaimCanYield` | Buffer parity test. |
| Drop the `maintainLink` record test | Platoon test: the link is extended. |
| Drop the `tryLink` record test | Platoon test: a new link forms. |
| Drop the `changed` record test | Approach test: the pair couples. |
| Drop the Q7 discovery skip | Q7 test. |
| Drop the record term of the Q7 discovery skip | Q7 test of a member with a record after its split: discovery recruits the pod. |
| Put the adoption guard in `couplingMemberEligibility` | Remaining-motion restore test of a train with a deferred member. |
| Gate the claim surrender on fault records only | Claim surrender test. |
| Accept an emergency on an empty pod | Refusal test. |
| Write an emergency member without the marker | Off-state golden. |
| Tag `OrderID` with `N = 4` | Digest registry test. |
| Wrap a counter at the maximum | Counter test. |
| Use the delta path `/groups/emergencies/value/active` | Prescan audit. |

## 15. Qualification gates

These gates would justify a maintainer decision to enable emergencies by default.
The default rate stays 0 until the maintainer chooses one in stage 7.

| Gate | Measurement | Pass |
| --- | --- | --- |
| Off parity | The off-state gates of section 12 on every preset. | Byte identity, digest identity, and identical states at every 600th tick. |
| Marker-only parity | The same runs with the marker and no emergency. | Identical states apart from the marker. |
| Safety | A soak driver on LondonCentral and the rail-hub preset that starts emergencies on random occupied pods through the command, at about 10 per simulated hour, for 24 simulated hours per seed, over at least 20 seeds, with coupling on and off, and with faults on and off. | `CheckContract` holds after every tick and command. No owner conflict. No coupling fault. |
| Contract | The same soak, with E1 to E8, F11, and W1 to W5 checked after each tick. | No violation. |
| Conservation | The same soak. | `RequestID = Completed + Interrupted + queued + aboard` at every tick. |
| Progress | The same soak. | Each record ends, and each record of an ordinary pod ends within 10 simulated minutes. A run that misses the gate lists each remaining record with its phase and wait reason. |
| Choice | The same soak, with the estimate of the chosen station against the actual time to the unload. | Reported, not gated. |
| Ordinary traffic | Admission wait of ordinary intents against a run without emergencies. | Reported, not gated. |
| Persistence | Saves at random boundaries of the soak, restored in the physical tier, and rewinds to random checkpoints. | Every save restores and passes `CheckContract`, or fails the physical tier only by the demotion rule. Every rewind replays exactly. |
| Performance | Step time of the soak against a run without emergencies, with `MaxEmergencies` records in the cadence search. | Median step time within 10 percent, and no tick above twice the slowest tick of the run without emergencies. |
| Worst choice tick, presets | LondonCentral and the rail-hub preset with four emergencies that start on one tick, under each routing policy, cold and warm. | The worst choice tick is at most 50 ms on the CI runner class. |
| Worst choice tick, no candidate | A fixture at the project limits (5,000 nodes, 8,000 lanes, 300 stations) with four deferred, divertible records that started on one tick and find no candidate, under each routing policy, so all four search on one cadence tick. The tree reaches the berths, and the bank rules refuse each route search, so pruning skips nothing. If `Validate` allows no such network, the fixture uses the largest count of such berths that it allows, and reports it. Cold, with no route memo and no routing-policy state, and warm, after 600 ticks of the traffic of the fixture. | Reported against the 50 ms threshold on the CI runner class, cold and warm, and never gated (product choice P23). It is expected to miss, because pruning cannot lower the worst case. The counts of graph searches by kind are reported and within the bound of section 9.1. The counters show failed route searches and a failed same-bank local attempt. |
| Worst choice tick, delayed routes | The same network size, with four records on one cadence tick, reachable berths, and stopped pods that delay the free-flow routes, under the congestion, queue, and predictive policies. Cold and warm, as above. | The worst tick is at most 50 ms on the CI runner class, cold and warm. The counters show a congestion search with nonzero view costs, a queue search after a delayed free-flow route, and a forecast build and a predictive search after a free-flow cost above its free-flow time. |
| Formats | The composed fixtures of section 14.4. | Every shape under its cap. |
| Web | The browser shell and editor with a marked project. | `mise run test:web` passes, and the marker checks refuse the cases of section 14.3. |
| Product | The product choices of section 18.2. | Done: the maintainer approved P1 to P22 on October 5, 2026, and decided P23 on October 5, 2026, at 23:59Z. |

Status 2026-10-07: the latency fixtures keep a fleet of 300 pods, below the limit of 600, because their networks cannot hold 600 berths.

The search counters are an unexported member of `Simulation`, written by the search code and read by the tests and benchmarks of `internal/sim`.
No rule reads them, and they are not saved.
`BenchmarkEmergencyLimitSearch` in `internal/sim` repeats the measurement of section 9.1 on the grid that it describes, so that the CI runner class can be measured.
The presets and the delayed-routes fixture are gated, and the no-candidate fixture is a report.

The open item of revisions 4 and 5 is closed.
The maintainer decided on 2026-10-05: "Exact P13, gate realistic" (product choice P23).
A pathological first choice on an adversarial network can stall one tick, and the no-candidate memo of section 9.1 stops it from repeating.

## 16. Patch order

Each patch compiles and passes the full suite on its own.
The off-state gates of section 12 run after each patch.

1. `sim`: Skip out-of-service pods in coupling discovery and adoption (Q7), with E6 in `CheckContract`.
   This patch can land before stage 2 patch 3.
2. `sim`: Add emergency records, the start, the emergency stage, the record end, `Clone`, `Reset`, the saturating counters, and E1 to E8 with F11.
   The advance unloads at a berth and otherwise defers.
3. `sim`: Add the station choice with the routing view, the pruning tree with its term guard, the no-candidate memo, and the search counters, the cadence, and the purpose 1 amendments of section 9.2.
4. `sim`: Add the emergency tier in admission, the platoon drain, the approach abort, and the claim surrender gate.
5. `sim`: Add the restore rules of section 10.7 and `CheckIncidentPolicy`.
6. `project` and `session`: Add the emergency marker, the settings, the command, the errors, the digest entries, and the policy step of the session restore with the fixture restores of `internal/session/incident_save_test.go`.
7. `session` and `web`: Add the save and stream members, strict decoding, the prescan limits, the composed fixtures, and the web marker check.
8. `view`: Add the inspector emergency control.
9. `docs`: Describe emergencies in `docs/protocol.md` and `docs/operations.md`.

Patches 2 to 5 are internal.
Their tests call the operations directly.
Stage 2 patch 3 has landed, so patch 2 can use `faulted`.
Patch 3 needs stage 2 patch 5 for the endpoint reroute amendment, and patch 4 needs it for the claim surrender.

## 17. Reuse of the parked emergency contract

The parked contract is the rider emergency contract proposal, round 2, against `5c90c9f`.
It was never committed.
Two Codex review rounds stopped it.
This contract reuses the parts that fit stage 3, anchored again at `236ca65` and placed on the stage 1 and stage 2 API.

### 17.1 Reused

| Parked section | Here | Change |
| --- | --- | --- |
| Start and eligibility | 5.2, 6.1 | Same checks and order, without the demo check. The default party is the first active rider. The later work goes through `withdrawService`. |
| Estimate, `T` and `Q` | 9.1 | One time base, the edge seconds of the routing graph, on the installed route from the route distance. `Q` counts only pods ahead on the current lane, as before. |
| Tie breaks | 9.1 | Whole ticks, then the station index. |
| Platoon draining | 5.6 | Same rule: drain on each tick, no extension, no new link. Round 2 marked it resolved. |
| Admission order | 9.3 | Same tier before the age rule, ordered by serial instead of start tick. |
| Liveness, without the proof | 9.5 | Only the claims that round 2 accepted: no finite wait under a sustained rate, and the cap as an admission limit. |
| `maxActiveEmergencies` = 4 | 4.3 | Now product choice P19. |
| Command, errors, reply | 10.3, 10.4 | Same action and members, on the stage 1 digest registry. |
| Restore outcomes | 10.7 | Replaced by the stage 1 tiers, with the stage 2 demotion rule. |
| Metrics | 10.5 | Three counters. The rest moves to stage 7. |
| Game presentation | 10.6 | One inspector button. |
| Mutation table | 14.6 | Rewritten for the new mechanisms. |

### 17.2 Dropped

| Parked section | Reason |
| --- | --- |
| Berth term `B` | Round 2, finding D: two emergencies at one berth made the estimate recursive. The tier and terminal reevaluation take the next free berth instead. |
| Switching rule, `switches`, and the decision window | Round 2, finding H: commitment after a refused diversion was not saved. Here the purpose 1 tuple is the commitment, and the station never changes. |
| Cost bound by pruning | Replaced by exact pruning, which keeps the result of the exhaustive scan and does not lower the worst-case bound (section 9.1). |
| Emergency reroute and `stops = [S]` | Replaced by `setOperationalDestination` with the stage 1 stop rules. Round 2, findings A and C. |
| Later work by `podFitsRequest` and the exclusion that lasts for the record | Replaced by `withdrawService` and the stage 1 exclusion that holds until boarding. Round 2, finding F. |
| `bufferClaimCanYield` changes and the guarded yield | Round 2, finding B: they changed behavior with the feature off. Decision 1 forbids revocation. |
| Bank and entry yield rules | Decision 1. The station choice compares entries. |
| Order outcomes, `legFrom`, and journey-origin inference | Stage 1, sections 7 and 8. Round 2, findings E, J, and K. |
| State machine with a stored phase | The phase is derived (section 4.2). |
| Separate `emergencyReleasedPickups` counter | The stage 1 release counts. |
| Byte table for one shape | Section 11.6 covers each shape. Round 2, finding L. |
| `N` 1 to 15 for emergencies | The stage 1 registry assigns `N` in order. |

### 17.3 Parked review findings that must not return

| Finding | Where this contract closes it |
| --- | --- |
| Round 1, 1: digests change | Section 11.2 uses the stage 1 registry. |
| Round 2, A: phase rules reject valid transitions | `startOperationalUnload` and the stage 1 stop rules (stage 1, sections 9.3 and 9.4). |
| Round 2, B: off-state change in `bufferClaimCanYield` | No change to it. The Q7 fix is its own patch, with its own parity gate. |
| Round 2, C: mixed coordinates | The estimate runs on the installed route from the route distance (section 9.1). |
| Round 2, D: recursive berth estimates | No berth term. The estimate reads no other emergency. |
| Round 2, E and K: `From` readers and origin inference | Stage 1 leg origin. |
| Round 2, F: the exclusion ends too early | Stage 1 exclusion until boarding. |
| Round 2, G: liveness proof | Section 9.5 states conditions, not a proof. |
| Round 2, H: commitment not saved | The purpose is saved. No window exists. |
| Round 2, I: restore outcomes | Stage 1 tiers, and section 10.7. |
| Round 2, J: zero index omitted | Every tuple element is present (section 11.3). |
| Round 2, L: allocation for one shape | Section 11.6 covers every shape. |
| Parked redirect that set `RelocatingTo` | Purpose 1 leaves `RelocatingTo` empty (stage 1, section 9.3). |
| Non-persistent draining | The rule sets `draining` again on each tick, so a restore keeps it. |

## 18. Requirements and product choices

### 18.1 Settled requirements

A binding decision, an approved contract, or a safety rule settles each row.

| # | Requirement | Source |
| --- | --- | --- |
| R1 | The emergency pod gets free resources first and never takes an owned one. | Decision 1. |
| R2 | A coupling member stays in its train until the split at its committed split site. | Decision 2; stage 1, section 13. |
| R3 | Every party leaves the pod at the emergency station. The party is interrupted. Each other party completes or is transferred, and can ride any pod. | `PROJECT_BRIEF.md:700-701`; decision 3; stage 1, section 9.5. |
| R4 | Pending pickups of the pod go to other pods, with the exclusion until boarding. | Decision 4. |
| R5 | An emergency never evacuates. A fault evacuation, at rest only, interrupts every rider. | Decision 5; stage 2, section 5.5. |
| R6 | The stage is off by default, with off-state parity, and refuses each unsupported target before any change. | Decision 6. |
| R7 | Coupling discovery and adoption skip a withdrawn pod and a pod with a purpose. Discovery also skips a pod that a record names. | Decision 7, revision 7. |
| R8 | No build tags. | Decision 8. |
| R9 | A damaged or invalid save moves aside, with no partial recovery. | `AGENTS.md`. |
| R10 | No compatibility code for old saves or protocols. | `AGENTS.md`. |
| R11 | Liveness holds only under the conditions of section 9.5. Otherwise the system stays safe and reports the wait. | Stage 2, section 9.7; the brief of this stage. |
| R12 | Healthy pods get no hold, and no wait state is stored for each pod. | The brief of this stage. |

### 18.2 Product choices

The maintainer approved choices P1 to P22 as proposed on October 5, 2026, at 22:08Z, and decided P23 on October 5, 2026, at 23:59Z.

| # | Choice | Decision | Rationale | Alternatives that were not chosen |
| --- | --- | --- | --- | --- |
| P3 | Refuge preference | Approved by the maintainer, 2026-10-05. No refuge in stage 3. | Purpose 2 needs a held owner (stage 1, W5). An obstructed healthy pod has none, and this stage adds no hold to a healthy pod. An emergency always unloads. | A refuge hold for an emergency pod that has no reachable passenger station, with a new hold bit and amendments to F11 and W5. |
| P4 | Refuge resume | Approved by the maintainer, 2026-10-05. Moot while P3 has no refuge. | Follows P3. | `resumeFromRefuge` on a cadence after the clear, if P3 adds a refuge. |
| P11 | Unreachable occupied service | Approved by the maintainer, 2026-10-05. Keep the stage 2 behavior: the pod waits on its route and reports the wait. An operator can start an emergency, which unloads the pod at a reachable station. | It adds no hold and no policy to a healthy pod, and it gives the operator a remedy. | An automatic emergency after a wait limit, or a refuge (P3). |
| P12 | Trigger and issuer | Approved by the maintainer, 2026-10-05. The `emergency` command and the inspector button in stage 3. The scenario rate in stage 7, with the mechanism of section 10.2. | The user listed all three sources. The rate needs the sampler. | The rate in stage 3. |
| P13 | Station choice rule | Approved by the maintainer, 2026-10-05. The lowest `E` of section 9.1, chosen once, with no berth term and no continuation term. | It follows the brief: soonest unload, with queues and not only distance. One commit needs no saved window. | The parked berth term and switching window, nearest by distance, or a preference for stations where `continuationFeasible` holds for each transferred party. |
| P14 | What riders see | Approved by the maintainer, 2026-10-05. The party's order ends `interrupted`. Other orders complete at their destination or show "Transfer at" (`internal/view/orders.go` `(*Game).orderLabels`) and continue. The default party is the first active rider. | It uses the stage 1 outcomes with no new state. The game control has no rider to pick. | No party, so no order is interrupted. A new outcome kind for the party. |
| P15 | Approach member | Approved by the maintainer, 2026-10-05. Abort the approach. | The pod is not yet in a train, so decision 2 does not hold it, and the abort reuses the existing path. | Let the pair couple, and defer to the split. |
| P16 | Emergency on a faulted pod | Approved by the maintainer, 2026-10-05. Accept it as deferred. | The rider need is real, and the fault rules stay in charge until the clear or the evacuation. | Refuse with a new error. |
| P17 | The party leaves before the unload | Approved by the maintainer, 2026-10-05. End the record. | The cause has left the pod. | Keep the record, and unload the other parties with no interruption. |
| P18 | Priority among emergencies | Approved by the maintainer, 2026-10-05. The earlier start (lower serial) first. | Deterministic and simple. | The lower `E` first, or the more riders first. |
| P19 | Limits | Approved by the maintainer, 2026-10-05. One record for each pod, and at most 4 active records. | Each record removes the age protection of competing intents, so a small cap limits the effect on ordinary traffic. The budget holds up to 300. | A larger cap, or none. |
| P20 | Metrics | Approved by the maintainer, 2026-10-05. The three counters of section 10.5 and the stage 1 counters. The full set in stage 7. | Enough to check the soak gates. | Time from start to unload as a distribution, and the delay of other riders, now. |
| P21 | UI control | Approved by the maintainer, 2026-10-05. One "Emergency" inspector button with one press, as the stage 2 "Fault" button. | Consistent with stage 2. | A confirmation step, or a party picker. |
| P22 | Cancel | Approved by the maintainer, 2026-10-05. No cancel command. | A cancel needs `rebindOperationalOwner` or a new end rule for each phase. | A `clearEmergency` command for a deferred record. |
| P23 | Latency of the station choice | Decided by the maintainer, 2026-10-05: "Exact P13, gate realistic". P13 stays exact with pruning, the term guard, and the no-candidate memo. The worst choice tick is gated at 50 ms on the CI runner class on LondonCentral and the rail-hub preset with four emergencies on one tick, and on the delayed-routes fixture at the project limits. The adversarial no-candidate fixture is reported only. | The exact rule keeps the approved result of P13. Realistic networks meet the threshold, and on an adversarial network a pathological first choice stalls one tick and the memo stops it from repeating. | The budgeted scan of revisions 3 to 5, rejected because it changes the result of P13. A higher threshold. |

## 19. Review resolutions

### 19.1 Round 1

The first blind external review (Astra) checked revision 1 against `236ca65`, and its verdict was to approve after changes.
Revision 2 is rebased on `481c145`, where stage 2 patch 3 has landed, and every `file:line` reference is checked again against that source.
The coordinator decided each finding, and the maintainer approved every product choice as proposed on October 5, 2026, at 22:08Z.
No resolution changes a product choice.

| # | Severity | Finding | Decision | Resolution | Sections |
| --- | --- | --- | --- | --- | --- |
| 1 | Blocker | The unconditional Q7 fix contradicts emergency-off identity. | Change rejected. Q7 stays unconditional, because the maintainer chose to fix the bug in stage 3. The text is fixed. | Section 12 states the exception: runs in which a withdrawn or purpose pod would have been discovered or adopted for coupling, which need the incident or fault marker. A save with such a pod in a coupling group breaks E6 and moves aside. The patch 1 matched-seed check stays. | 5.8, 11.5, 12 |
| 2 | Major | The station choice can exclude a station with a reachable berth or bank. | Accepted. | Each entry group tries its compatible, unblocked berths in order until one has a route, and a station is excluded only when no group has a candidate. The lowest `E` wins. `E` still has no berth term (P13). | 9.1 |
| 3 | Major | The logical tier keeps saved holds, so dropped records leave orphan holds. | Accepted. | With the marker, the logical tier releases `emergencyHold` after it clears the purposes. Without the marker, stage 1 restore behavior does not change. | 3, 10.7 |
| 4 | Major | Emergency validation runs too late for the logical tier. | Accepted. | `checkSavedEmergencies` checks the saved records and pod relations, E2 to E6, before either tier. A failure rejects the whole save with no logical fallback. | 11.5, 14.2 |
| 5 | Major | Serial 0 passes decoding, and serials are not unique across kinds. | Accepted. | Serials are positive, at most `incidentSerial`, and unique across fault and emergency records, in the save checks, in `CheckContract` (E1), and in stream validation. | 8, 11.5 |
| 6 | Major | The station choice writes routing-policy state. | Accepted. | The choice and the installation run under one frozen routing view, which writes no congestion or predictive state. The refusal oracle compares that state. | 7, 9.1 |
| 7 | Minor | The coupled-arrival rationale and the alighting fixture contradict the source. | Accepted. | Section 5.6 states the retirement-then-arrival sequence, and no rider alights in a train. Q7 rests on recruitment and adoption alone, with direct recruitment tests. The claim that a purpose 3 pod in a train never clears its purpose is false: after the retirement, `arrive` clears it. It is removed. | 5.5, 5.6, 5.8, 14.1 |
| 8 | Minor | The search bound counts calls, not search work. | Accepted. | Section 9.1 counts graph searches: preferred and fallback, the policy search, bank subsearches, and the search at installation, with four records on one tick. Section 15 adds a fixture at the project limits with a synchronized cadence. | 9.1, 15 |
| 9 | Minor | The invariant plan is incomplete. | Accepted. | F11 is split into a state predicate for `CheckContract` and a transition requirement for a transition helper and a caller test. An invariant-to-mutation table covers E1 to E7 and F11, with duplicate records, orphan holds, and a wrong purpose owner. E2 and E4 hold with the marker, because the stage 1 test entry can set the hold and purpose 1 without it. | 8, 14.6 |
| 10 | Minor | The composed-format gate cannot accept 300 records. | Accepted. | The decoder round trips use `MaxEmergencies` records. The 300-record totals are an encoding-only allocation test. | 14.4 |

### 19.2 Round 2

The adversarial review (Sol) checked revision 2 against `481c145`, and its verdict was to approve after changes.
The coordinator accepted all seven findings and decided each one.
No resolution changes a product choice: P13 and the unconditional Q7 fix stay.
The review also confirmed the F11 mask, the pre-tier place of `checkSavedEmergencies`, and the determinism of the entry groups.

| # | Severity | Finding | Decision | Resolution | Sections |
| --- | --- | --- | --- | --- | --- |
| 1 | Major | The cadence does not make the scan acceptable in one tick. | Accepted. Resolved by the maintainer decision of 2026-10-05 (P23, section 19.5); revisions 4 and 5 marked it partially resolved. Exact lower-bound pruning, a measured latency, a proposed threshold, and a fallback marked proposed. | The choice prunes with one class-compatible tree of base edge seconds on the blocked graph, with the bank and berth rules relaxed, and with a stated float margin. The text proves that the result is the result of the exhaustive scan, and that only base costs are admissible for `E`. A randomized test compares both scans. One project-limit search takes 0.51 to 0.95 ms under load. The proposed threshold is 50 ms for the worst choice tick on the CI runner class. The exhaustive worst case cannot meet it, so section 9.1.1 proposes a budgeted scan, which the contract does not apply. | 9.1, 9.1.1, 14.1, 14.6, 15 |
| 2 | Major | The worst-tick fixture misses the policy work. | Accepted. | A second project-limit fixture has reachable berths and delayed routes. Both fixtures are measured cold and warm and assert the branches that ran through search counters. Revision 4 corrects this row: the delayed-routes fixture is gated on the threshold, and the no-candidate fixture reports against it. | 15 |
| 3 | Minor | The search bound omits the failed same-bank local attempt. | Accepted. | A route search makes at most 4 graph searches, and an `assignedRoute` call at most 12. A fixture covers the case. | 9.1, 14.1 |
| 4 | Major | The frozen view needs explicit predictive initialization. | Accepted. | An immutable routing view for each choice replaces the flag. It holds the congestion costs, the queue discharge, the lane-sized raw sample and history, and the per-pod history, and it reads the route memos and writes none. An absent history is zero history with the current sample, without the emergency pod. The text states that the installation is not an ordinary assignment in the same tick, and why that is deterministic and safe. Three tests cover first use, an overdue refresh, and an existing history. | 7, 9.1, 12, 13, 14.1, 14.6 |
| 5 | Major | Production restore accepts an incident save with emergency state and no emergency marker. | Accepted. | `CheckIncidentPolicy` rejects hold 2 and purpose 1 without the marker, in a session restore step before either tier. The stage 1 primitive validator, `sim.RestoreState`, `CheckContract`, and `IncidentForTest` do not change. The fixture exception is in the restores of `internal/session/incident_save_test.go`. | 8, 10.7, 11.5, 13, 14.2, 14.6, 16 |
| 6 | Major | A record pod with purpose 2 can keep riders. | Accepted. | E8: the pod of a record has no active rider when its purpose is not 0 or 1, in `CheckContract` and in `checkSavedEmergencies`. | 4.2, 8, 11.5, 14.2, 14.6 |
| 7 | Minor | E5 cannot detect the E5 mutation. | Accepted. | The mutation fails an explicit post-stage assertion of the evacuation test: the record is absent and `emergencyHold` is cleared. E5 has its own mutation. | 8 |

### 19.3 Round 3

The confirmation review (Sol) checked revision 3 against `481c145`, and its verdict was to approve after changes.
It found the relaxed tree, the bound `L = P + min d`, the visit order, the strict stop rule, the routing view, E8, the evacuation assertion, and the place of `CheckIncidentPolicy` sound.
The coordinator accepted every finding and made the no-candidate memo of the review normative.
No resolution changes a product choice, and section 9.1.1 stays proposed.

| # | Severity | Finding | Decision | Resolution | Sections |
| --- | --- | --- | --- | --- | --- |
| 1 | Major | The float proof assumes that a route has at most `MaxLanes` lanes, and a live pod route can be longer. | Accepted. | A term guard computes the term count from the live prefix, the node count, and the pod count, and the choice prunes only when the stated margin covers it. Otherwise it evaluates every station. A test uses a prefix of more than 8,000 lane occurrences. | 9.1, 14.1, 14.6 |
| 2 | Major | The worst-tick latency is still open, and resolution 2 of round 2 calls both fixtures gated. | Accepted. | Round 2 finding 1 is partially resolved. Resolution 2 is corrected: the delayed-routes fixture gates, and the no-candidate fixture reports. Section 15 has an open item for the maintainer: the acceptance of the worst-tick latency of exact P13. Section 9.1.1 is not applied. | 9.1, 15, 19.2 |
| 3 | Minor | The differences of the budgeted scan from P13 are incomplete. | Accepted. | Section 9.1.1 states that `bestTick` is compared with bounds from later views and that the installation recomputes under a later view, and it adds a no-best guard and a final revalidation. It stays proposed. | 9.1.1 |
| 4 | Minor | Aggregate progress does not bound the progress of each record. | Accepted. | Section 9.1.1 states that there is no per-record completion bound under serial scheduling until the proposal specifies a fair allocation. | 9.1.1 |
| 5 | Minor | The oracle of a successful installation leaves out stage 1 installation effects. | Accepted. | The oracle allows every stage 1 installation effect: the release of revocable claims, the buffer fields, and the pending and wait fields. It still requires equal routing-policy state and route memos. | 7 |
| 6 | Minor | Two summaries contradict the normative text. | Accepted. | Section 1.3 describes the recruitment delay, and section 17.2 describes exact pruning with no lower worst-case bound. | 1.3, 17.2 |
| 7 | Answer | A no-candidate memo is sound. | Applied as normative. | `emergencyMisses` keeps a proven no-candidate result by record, `from`, and class. The blocked-set epoch, a network rebuild, `Reset`, a project replacement, and a restore clear it. It never holds a winner or a refused installation. Section 9.1 proves that the other inputs need no invalidation. It reduces the retries only. | 4.1, 7, 9.1, 12, 13, 14.1, 14.6, 16 |

### 19.4 Round 4

The second confirmation check (Sol) checked revision 4 against `481c145`, and its verdict was to approve after changes.
The coordinator accepted its four minor findings.
No resolution changes a product choice.
After this round, the only open item is the maintainer decision on the worst-tick latency of exact P13 (section 15).

| # | Severity | Finding | Decision | Resolution | Sections |
| --- | --- | --- | --- | --- | --- |
| 1 | Minor | The float proof uses a first-order error, and the queue delays depend on the rounded accumulator. | Accepted. | The proof compares the computed `E` with a base-only accumulation from the same computed `P`, and bounds both sides with `γm = m·u/(1 - m·u)`. The term guard still holds, and its constant does not change. | 9.1 |
| 2 | Minor | The clone tests compare internal state that includes the memo and the search counters. | Accepted. | Section 4.1 excludes both from the internal-state equality of the clone tests, `clone_test.go` gets matching rules, and a warm-memo clone parity test compares the exported trajectories. | 4.1, 14.1 |
| 3 | Minor | The no-candidate test expects a search on the next cadence tick. | Accepted. | With the memo, the retries make no search until a key input changes or the memo is cleared. | 14.1 |
| 4 | Minor | A multi-tick scan does not prove a no-candidate result. | Accepted. | The budgeted scan never fills the no-candidate memo. | 9.1.1 |

### 19.5 Maintainer decision on latency

The maintainer answered the open item of section 15 on 2026-10-05: "Exact P13, gate realistic".
Revision 6 folds the decision as product choice P23, and the contract is approved at revision 6.

| Item | Resolution | Sections |
| --- | --- | --- |
| Station choice rule | P13 stays exact, with pruning, the term guard, and the no-candidate memo. | 9.1, 18.2 |
| Gate | The worst choice tick is gated at 50 ms on the CI runner class on LondonCentral and the rail-hub preset with four emergencies on one tick, and on the delayed-routes fixture at the project limits. | 9.1, 15 |
| Adversarial fixture | The no-candidate fixture is reported only, and never gated. A pathological first choice on such a network can stall one tick, and the memo stops it from repeating. | 9.1, 15 |
| Budgeted scan | Rejected by the maintainer, because it changes the result of P13. Section 9.1.1 is a short note, and no other section has its cursor, budget, or save bytes. The rows of sections 19.2 to 19.4 that name it describe the rejected proposal. | 9.1.1 |
| Open item | Closed. Round 2 finding 1 is resolved by this decision. | 15, 19.2 |

### 19.6 Maintainer decision on the discovery skip for records

The maintainer approved this change on 2026-10-06 at 11:25Z.
The coupling incident qualification found that discovery runs before the emergency stage.
A coupling member with a record has no hold in the tick after its split, so discovery could recruit it into an approach.
Section 5.6 said that the emergency stage of that tick withdraws the pod before `dispatch`, which was then false.
No invariant broke: `changed` aborts that approach, and `couplingAttempts` limits the retries.

| Item | Resolution | Sections |
| --- | --- | --- |
| Discovery skip | `discoverCouplingApproaches` also skips a front or rear pod that a record names. | 5.8 |
| Group members | Discovery of the tick after the split or the approach end skips the pod, and the emergency stage of that tick withdraws it. | 5.6 |
| Adoption | No change: `changed` aborts an approach with a record before adoption. | 5.8 |
| Off state | A record needs the emergency marker, so the term is gated by state. | 12 |
| Tests | A recruitment test of a member with a record after its split, with a mutation row. | 14.1, 14.6 |
