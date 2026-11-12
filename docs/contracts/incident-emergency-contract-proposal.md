# Incident emergency contract proposal

Status: Approved revision 7, October 6, 2026.
Amended through 2026-11-11.
Git history holds the dated amendment notes and the earlier text.

The maintainer approved product choices P1 to P22 of section 18.2 as proposed, on October 5, 2026, at 22:08Z, and decided P23 on October 5, 2026, at 23:59Z.
The contract authorizes no implementation, default change, or deployment.
Line references are at `481c145`.

This contract is stage 3 of the staged incident redesign: the emergency vertical slice on ordinary pods.
A rider in an ordinary pod starts an emergency.
The pod goes to the station where it can unload soonest, and every party leaves it there.

It builds on the API of the approved stage 1 contract, [incident-service-transitions-contract-proposal.md](incident-service-transitions-contract-proposal.md), section 13, and on the approved stage 2 contract, [incident-suspension-contract-proposal.md](incident-suspension-contract-proposal.md), section 15.
It changes neither contract.

Every `file:line` reference below is for `481c145`.
At that commit, stage 1 is complete, and stage 2 patches 1 to 3 have landed: the blocked route graph view (`internal/sim/blocked_routes.go`), the fault records with the fault stage, and the suspension of a faulted pod with its gates, braking, evacuation, and hold release (`internal/sim/faults.go`).
Stage 2 patches 4 to 9 have not landed: debris, the rules for obstructed pods with the endpoint reroute and the claim surrender, and the formats.
This contract cites those parts by stage 2 section, not by `file:line`.
Stage 3 starts after stage 2 patch 9, except patch 1 of section 16, which can land at any time.

## 1. Scope

### 1.1 What stage 3 delivers

1. Emergency records: one record for each pod in an emergency, with a party, a start tick, and a phase that each tick derives again (section 4).
2. The emergency start at a command boundary, and an emergency stage in `Step` that withdraws the pod, starts the unload at a berth, or binds the pod to an emergency station (section 5).
3. Deferral of platoon members until their link ends (section 5.6).
4. The station choice: the soonest estimated arrival over the reachable passenger stations, chosen once (section 9.1).
5. An emergency tier in admission that wins free resources and never revokes a grant (section 9.3).
6. The fix of review finding Q7 (section 5.8).
   It went with physical coupling on October 6, 2026.
7. The settings, the command, and the emergency members of each format (sections 10 and 11).
   The emergency marker was removed on November 4, 2026 (section 11.1).

### 1.2 Out of scope

| Item | Stage |
| --- | --- |
| A group exit for a platoon member before the end of its link | Stage 6 |
| Reverse motion of an emergency pod | Stage 5 |
| Refuge travel (purpose 2), and P3, P4, and P11 beyond their stage 3 defaults | A later stage, if the maintainer chooses it (section 18.2) |
| The scenario rate `perHour` above 0 | Stage 7 |
| The full metric set | Stage 7 |
| The revocation of a grant or a claim of another pod | Never (decision 1) |

The maintainer stopped the incident redesign after stage 3 on October 6, 2026, so stages 4 to 6 are out of scope, and the deferrals and refusals that this contract leaves to them stay in place.
The maintainer reopened stage 7 on November 4, 2026, under [the stage 7 contract](incident-stage7-contract-proposal.md), as option B with amendments A1 to A4.
The rules G1 to G16 of section 3 of that document, and its amendments, apply to this contract.
Text below that limits `perHour` to 0 describes stage 3, and the stage 7 contract defines the rate.

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
   Revision 7 extends the discovery skip to a pod that a record names, by the maintainer decision of October 6, 2026.
8. No build tags.
   Exported test entries are allowed, and section 13 lists each one.

Physical coupling was removed on October 6, 2026, so decisions 2 and 7 have no subject now.
On November 4, 2026, [the marker removal contract](marker-removal-contract-proposal.md) replaced the switch of decision 6: emergencies are on in every fleet but the demo fleet, with no project switch (section 10.1).

The product brief records the earlier user decisions of October 4, 2026 (`6227cf7:PROJECT_BRIEF.md:687-711`).
Decisions 1 and 3 above narrow two of them: "moves ahead of entry queues" becomes decision 1, and "a new pod" becomes decision 3.

### 1.4 Capability matrix

| Target | Stage 3 | Later |
| --- | --- | --- |
| Ordinary occupied pod, `Traveling`, that `divertStart` accepts | Accepted. The pod binds to an emergency station (section 5.4). | |
| Ordinary occupied pod, `Traveling`, in its arrival chain | Accepted. The pod stays deferred until it arrives, and then unloads at that berth. | |
| Ordinary occupied pod at a berth: `Boarding`, `Unloading`, or `Continuing` | Accepted. The unload starts at once at that berth. | |
| Platoon leader or follower | Accepted, deferred. The link drains, and the pod acts after the link ends (section 5.6). | Stage 6 adds a group exit. |
| Faulted pod that carries passengers | Accepted, deferred while the fault lasts (section 5.7, product choice P16). | |
| Pod that carries no passenger, also an evacuated faulted pod | Refused with `pod carries no passenger`. | |
| Pod that already has an emergency | Refused with `pod already has an emergency`. | |
| `perHour` above 0 | Refused | Stage 7 |

Every accepted target gets its record and `emergencyHold` at once.

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
| The digest registry | Stage 1, section 11.3 | New extension numbers. |
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
| Group member | A platoon leader or follower (`coupled`, `internal/sim/platoon.go` `(*vehicle).coupled`). |
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

This table records the audit at `481c145`.
Physical coupling was removed on October 6, 2026, station buffers and compact station queues on October 7, 2026, and the markers on November 4, 2026.
The opt-in Compact station added in October 2026 (maneuver groups, `internal/sim/maneuver_*.go` and `internal/sim/station_maneuvers.go`) is a different feature, and this removal does not cover it.
Station entry runs, an early berth choice, and coast-in were added on October 7, 2026.
A platoon run can end on the entry lane of the destination station of both pods, and it never continues past the entry node of that station.
With platoons on, a pod that is not of a large class can try its berth choice before the usual point, and an early try that finds no berth does not refuse the pod.
With platoons on, a pod that waits for a contested station diverge can coast: a speed ceiling lowers only its commanded speed.
The incident refusal of platoon members and the emergency drain rule are unchanged.
The `coupling_*.go` rows and the `bufferClaimCanYield` row name code that went with them, and the coupling, approach, and compact refusals that other rows name are gone.

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
| `emergenciesOn bool` | The switch of the start and the emergency stage. The session sets it for every fleet but the demo fleet (section 10.1). | Kept, as `faultsOn` (`internal/sim/simulation.go` `Simulation`) | Copied |
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
| Station choices for one record | One commit. One choice at the start when the pod is divertible then, and one each 1 s of simulated time while the pod is deferred and divertible. | Section 9.1 |

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
3. Call `withdrawService(v, emergencyHold)`.
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
2. This step deferred a coupling or approach member.
   It went with physical coupling on October 6, 2026.
3. When `v.withdrawn&emergencyHold == 0`, call `withdrawService(v, emergencyHold)`.
4. When `v.op.purpose != opService`, return.
   The pod is bound or unloading, and `arrive` and the unloading loop do the rest.
5. Let `mask = 1 << i`, where `i` is the index of the party in `v.Riders`.
6. When the pod is at a berth with activity `Boarding`, `Continuing`, or `Unloading`, call `startOperationalUnload(v, emergencyHold, mask)` and return.
   When the call refuses, the pod is a platoon member, and it stays deferred.
7. When the pod is `Traveling`, `divertStart` accepts it, and either the call comes from the start or `(s.tick - r.start) % TicksPerSecond == 0`, run the station choice of section 9.1.
   When it finds a pair `(S, b)`, call `setOperationalDestination(v, operationalTarget{purpose: opEmergencyUnload, owner: emergencyHold, interrupt: mask, station: S, berth: b})`.
8. Otherwise the pod stays deferred, and it keeps its route.

Each operation in steps 3, 6, and 7 checks its own preconditions before any change.
A refusal leaves the pod deferred and changes nothing, and the next stage tries again.

A deferred pod that cannot divert keeps its current route.
In its arrival chain, it arrives at its current berth, and the next emergency stage starts the unload there.
`startOperationalUnload` keeps a `phaseTicks` of 1 or more, so the pod does not wait a second unloading interval.

The cadence of 1 s of simulated time in step 7 bounds the cost of the station choice (section 9.1).
A pod that becomes divertible after its start waits for less than 1 s of simulated time for the choice.

### 5.5 Record end and hold release

`endEmergency(i)` removes record `i`, adds 1 to `ended`, and, when `v.withdrawn&emergencyHold != 0`, calls `restoreService(v, emergencyHold)`.

The record ends in these cases, and in no other:

| Case | Cause |
| --- | --- |
| The pod carries no passenger. | `finishOperationalUnload` ended the unload, a fault evacuation interrupted every rider, or every rider left at its stops while the pod was deferred. |
| The record is deferred, and the party is not an active rider. | The party left the pod while the pod was deferred, at an ordinary alight or by an interruption (product choice P17). |
| A logical restore, or a reset. | Section 10.7. |

`restoreService` cannot fail at a record end:

- The purpose is not owned by `emergencyHold`.
  Purpose 1 needs an active rider (stage 1, section 9.3), and both transitions that remove the last rider clear purpose 1: `settleIdleAtBerth` and `evacuateLane`.
  In the deferred case, the purpose is 0.
- The emergency stage runs outside a dispatch pass.

A faulted pod keeps `faultHold` after the end, so it stays withdrawn.
The stage 2 hold release rule (`releaseFaultHolds`, `internal/sim/faults.go` `(*Simulation).faultStage`) releases `faultHold` later.

### 5.6 Group members

The pod finishes its group motion first, and the emergency acts after the pod leaves the group.

| Group | Rule | End of the wait |
| --- | --- | --- |
| Platoon leader or follower | `maintainLink` sets `draining` on each tick when either end has a record, and it does not call `extendLink`. `tryLink` refuses a pair with a record at either end. | The link ends when the follower holds no resource of a pod ahead (`internal/sim/platoon.go` `(*Simulation).maintainLink`). The pod is then divertible. |

Only `extendLink` clears `draining`, and the link rule sets it again on each tick, so the rule keeps no state of its own.
A restore needs no new member for it.

The rules of this section for coupling members and approach members went with physical coupling on October 6, 2026.
The rule for compact queue members went with the compact station queues on October 7, 2026.

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

This section defined the fix of review finding Q7.
Coupling discovery and adoption skipped a withdrawn pod and a pod with an operational purpose, discovery also skipped a pod that a record names, and invariant E6 kept each such pod out of a coupling group.
The fix landed as patch 1 of section 16, and it was not gated on the emergency marker.
Physical coupling was removed on October 6, 2026, and the discovery skip, the adoption guard, the approach reason, and E6 went with it.

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
| `withdrawService(v, emergencyHold)` | Not faulted, `emergencyHold` not set, in the emergency stage or at a command boundary | Every precondition. It cannot refuse. |
| `startOperationalUnload(v, emergencyHold, mask)` | Purpose 0, at a berth, `Boarding`, `Continuing`, or `Unloading`, with the hold, and `mask` names the active party | All but the platoon test. A refusal leaves the pod deferred. |
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
| Start | Preflight, ID, record and counter, `withdrawService`, `advanceEmergency`. |
| Advance | `withdrawService` when the hold is missing, then one of `startOperationalUnload` or the station choice and `setOperationalDestination`, both under one routing view (section 9.1). The choice and the view write no simulation state other than the no-candidate memo of section 9.1. |
| Record end | Record removal and counter, then `restoreService` when the hold is set. |

The later step of a composite cannot fail after the first succeeds, or its failure leaves a valid deferred state:

- `withdrawService` changes no route, destination, or owner (stage 1, W4), so it changes no input of the advance.
- A refusal of `startOperationalUnload` or `setOperationalDestination` changes nothing, and the record stays deferred.
  A deferred record with the hold is a valid state (invariants E1 to E8).
- `restoreService` cannot fail at a record end (section 5.5).

Refusal oracle.
Each refusal test clones the simulation before the call and compares the clone with the state after it: the exported state, `owners`, every `routeReleases` and `nextRelease`, the waiting trips with their bindings and exclusions, `incidentSerial`, the records, the counters, the holds, and the purposes, as stage 2, section 11, specifies.
The oracle compares the free-flow route memo, `routes` and `routeOrder`, with a copy taken before the call, because `Clone` does not copy that memo (`internal/sim/clone.go` `(*Simulation).Clone`).
A station choice that finds no candidate passes the oracle, apart from the no-candidate memo of section 9.1.
A choice that binds the pod can make every change of a successful stage 1 installation (`internal/sim/operational.go` `(*Simulation).setOperationalDestination`): the route, the destination, the purpose, and the derived route indexes of the pod, the release of its revocable claims on the berth of its old destination, and the pending and wait fields.
In that case, both route memos still stay equal.
The search counters of section 15, the search work arrays, and the no-candidate memo are not compared.

## 8. Invariants

| ID | Invariant |
| --- | --- |
| E1 | Records are in strictly increasing serial order, at most one record names each pod, there are at most `MaxEmergencies` records, and `0 <= start <= s.tick`. Each serial is positive and at most `incidentSerial`, and no fault record and emergency record have one serial. |
| E2 | With `emergenciesOn`, a pod with `emergencyHold` has a record. |
| E3 | The party of a bound or unloading record is an active rider of its pod, and `v.op.interrupt` is exactly the bit of that rider. |
| E4 | With `emergenciesOn`, a pod with purpose 1 has `op.owner == emergencyHold` and a record. |
| E5 | The pod of a bound or unloading record carries passengers. |
| E6 | Removed with physical coupling on October 6, 2026. It kept each coupling member and each approach member free of a hold and of a purpose. |
| E7 | With emergencies off, no record exists. |
| E8 | The pod of a record has no active rider when its purpose is not 0 or 1. |
| F11, state | With `faultsOn` and `emergenciesOn`, `withdrawn` is a subset of `faultHold | emergencyHold` for every pod. With `faultsOn` alone, the landed check stays: `faultHold` is the only hold (`internal/sim/faults.go` `(*Simulation).checkFaults`). |
| F11, transition | With `emergenciesOn`, a pod gains `emergencyHold` only in `startEmergency`, which is the start of `Emergency` and of the sampler (stage 7 contract, rule G9), or in `advanceEmergency`, and only while a record names it. A pod keeps `emergencyHold` until its record ends. |

E2 and E4 hold only with `emergenciesOn`, as F11 holds only with `faultsOn`: with emergencies off, the stage 1 test entry `IncidentForTest` (`internal/sim/incident_entry.go` `(*Simulation).IncidentForTest`) can set `emergencyHold` and purpose 1 with another owner, and the stage 1 rules W1 to W5 govern them.
`CheckContract` (`internal/sim/state_contract.go` `(*Simulation).CheckContract`) checks E1 to E5, E7, E8, and the state part of F11 after each tick and each command.
`CheckContract` reads one state, so it cannot check where a hold came from.
A transition helper checks the transition part of F11: it compares each pod before and after each tick and each command.
It fails when a pod gains `emergencyHold` and no record names it after the change, and when a pod loses `emergencyHold` while its record stays.
The rule that only the two operations set the hold is a code property: they are the only callers of `withdrawService(v, emergencyHold)`, and a source test lists the callers.
The record end tests check the end conditions of section 5.5.

Production policy.
`CheckIncidentPolicy(input RestoreStateInput) error` is the production policy validator.
It reads only the saved state.
The session restore runs the validator before either tier, as a step of `restoreSteps` (`internal/session/persist.go` `restoreSteps`) that `NewFromStore` sets.
A failure is `invalid_state` with no logical fallback, and the save moves aside.

Each production restore runs with emergencies on, and the session turns them off after the restore for a demo fleet.
With emergencies on, `checkSavedEmergencies` rejects a saved pod with hold 2 (`emergencyHold`) and no record, and a saved pod with purpose 1 and no record, by E2 and E4 (section 11.5).
By [the marker removal contract](marker-removal-contract-proposal.md), the validator refuses the states that no owner can release.
For each pod, in this order:

- `saved pod %s has a refuge`;
- `saved pod %s has an empty recovery with the emergency owner`.

Then, for a save of the traffic demo:

- `saved demo pod %s has a service hold or an operational purpose`;
- `saved demo fleet has fault or emergency state`.

The marker design had two other refusals, for hold 2 and for purpose 1 without the emergency marker.
They went with the markers on November 4, 2026.

The stage 1 primitive validator (`checkPodOperational`, `internal/sim/state_contract.go` `checkPodOperational`), `sim.RestoreState`, `CheckContract`, and `IncidentForTest` do not change.
The stage 1 fixtures therefore still make these states and restore them through `sim.RestoreState`, with emergencies off.
The fixture exception is in one place: the session restores of `internal/session/incident_save_test.go` that load such a save call `newFromStore` with no policy step, and a session test checks that `NewFromStore` rejects the same saves.
The stream and HTTP decoders keep the stage 1 rules, because the stage 1 stream tests of that file send frames with hold 2 and purpose 1 and no record, and only a fixture makes such a frame.

The table below records the mutations at landing.
The E6 row went with physical coupling, and the E7 and Policy rows name the marker design.

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
Each record pod gets `emergencyHold` at its start (section 5.2).
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
`E(S, b)` is the cost of the lanes of `R`, with queue delays, started at the position of the pod on `R` (`internal/sim/emergency_choice.go` `(*stationChoice).advance`):

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
     Each step of `E` adds `fl(σ(l) + δ)` with `δ >= 0` (`internal/sim/emergency_choice.go` `(*stationChoice).advance`), and `fl(σ(l) + δ) >= σ(l)`.
     The value of `δ` depends on the rounded accumulator, but it is never negative, so by induction on the steps, the computed `E` is at least `B`.
     A recursive sum of `mU + 1` nonnegative floats is at least `(1 - γ(mU)) ·` its exact sum, so `E >= (1 - γ(3n)) · (P + Σ σ(l))` over `U`.
   - Upper side.
     The tree relaxes each lane of a shortest path `π` of fewer than `n` lanes, so its computed `d(b.Node)` is at most the recursive float sum along `π`, which is at most `(1 + γn) · Σ σ(l)` over `π`, and `Σ σ(l)` over `π` is at most `Σ σ(l)` over `U`.
     `L(S) = fl(P + min d)`, so `L(S) <= (1 + u)(1 + γn) · (P + Σ σ(l)) <= (1 + γ(n+1)) · (P + Σ σ(l))` over `U`.
   - Together, `L(S) <= E · (1 + γ(n+1)) / (1 - γ(3n))`.
     With `n <= 12,000`, the node limit, this factor is less than `1 + 6e-12`.
     `lower(L(S))` is at most `L(S) · (1 - 1e-9) · (1 + u)`, and `(1 + 6e-12)(1 - 1e-9)(1 + u) < 1`, so `lower(L(S))` is at most the computed `E` of each candidate of `S`, and the stop rule never removes a station that the exhaustive scan could choose.
   - This proof does not use the prefix length, because `P` is shared.
     The term guard of pruning step 6 still holds, and its constant does not change: it keeps `4p + 7n + 2k <= 90,000` (with `p` the prefix length and `k` the pod count), which bounds every accumulation of the choice, and `γ(90,000) < 1.0e-11` is still more than 100 times below the margin.
     It also covers an implementation that sums `E` in one pass from the position.
     When the guard fails, the choice does not prune, so no bound is needed.
6. Admissibility needs the position at or before the end of the prefix.
   `divertStart` ends the prefix at the first lane end at or after the reserved end (`internal/sim/diversion.go` `(*Simulation).divertStart`), and without a reservation the prefix is empty and the pod is at the start of `R`.
   The implementation checks this, and when it does not hold, that choice evaluates every station without the stop rule.

Routing view.
`advanceEmergency` builds one immutable routing view for each choice, and the choice and `setOperationalDestination` both read it:

- `s.routeView` points to the view while it is set.
  `advanceEmergency` sets it before the choice and clears it after `setOperationalDestination` returns, or after a choice that finds no candidate.
  It is nil outside `advanceEmergency`, so no save or clone holds it.
- When `s.routeView` is set, `assignedRoute` reads the view, and it writes no route memo.
- Route memos.
  The view reads the free-flow memo (`s.routes`, `internal/sim/routes.go` `(*Simulation).cachedRouteForClass`).
  A miss runs the search, and the view does not write the result.
- Queue discharge.
  The view holds `queueDischarge(v)` (`internal/sim/routing_policy.go` `(*Simulation).queueDischarge`), which reads the pods and writes nothing.

The choice and the installation read one view, so the installed suffix equals the suffix of the candidate.
The view can keep the suffix of the chosen pair for the installation, with the same result.
With `s.routeView` nil, the routing code is unchanged, so the view changes nothing without an emergency.

Commitment.
The chosen pair binds the pod, and the station never changes.
`setOperationalDestination` saves the commitment in the purpose of the pod tuple.
Terminal reevaluation can still change the berth within the station (section 9.2).

Cost.
A choice runs at the start, and in the emergency stage only on ticks with `(s.tick - start) % TicksPerSecond == 0` while the record is deferred and the pod is divertible.
A choice that binds the pod ends the searches of its record, so repeated choices happen only while a deferred, divertible pod finds no candidate.
The bound counts graph searches:

| Unit | Bound | Source |
| --- | ---: | --- |
| Graph searches in one route search | 4: a same-bank local attempt that fails, then the source bank, the middle, and the destination bank | `internal/sim/bank_routes.go` `(Network).bankRoute` |
| Route searches in one `assignedRoute` call, free flow | 2: the terminal-berth search and its fallback | `internal/sim/routes.go` `(*Simulation).cachedRouteForClass` |
| Graph searches in one `assignedRoute` call | 12 | The rows above |
| Graph searches for the tree | 1 for each choice | Pruning, step 2 |
| Calls in one choice | One for each tried berth, plus one in `setOperationalDestination` | Steps 2 and 4 of the candidates |
| Tried berths in one choice | At most the compatible, unblocked berths of the passenger stations that the tree reaches. Each berth is a network node, so at most 12,000. | `internal/project/config.go` `MaxBerths` |
| Choices in one tick | `MaxEmergencies` = 4. Records that started on ticks with one remainder modulo `TicksPerSecond` choose on the same ticks. | Section 4.3 |

The worst tick therefore makes at most `4 · (1 + 12,001 · 12) = 576,052` graph searches, on a network of at most 12,000 nodes and 20,000 lanes (`internal/project/config.go` `MaxNodes`, `MaxLanes`).
At 12,000 nodes and 1,000 pods, the guard term `7n + 2k` is 86,000, so on such a network a choice prunes only with a prefix of at most 1,000 lanes.
The preset networks are much smaller.
The search measurement below and `BenchmarkEmergencyLimitSearch` keep the grid of 5,000 nodes, which was the node limit at the time of the measurement, when the bound was `4 · (1 + 5,001 · 12) = 240,052` graph searches.
Pruning does not lower this bound, because in a network where the tree reaches every berth and the restricted searches refuse each one, no station is pruned.
The view does not write the free-flow memo, so the installation repeats the searches of the chosen pair unless the view keeps its suffix.

Latency.
One search on a network at the project limits of that time was measured with the existing search code:

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
  `Reset`, a project replacement, and a restore also clear it, and a record end removes the entry of its record.
  Since November 5, 2026, the network indexes are built once with the prepared network, so no rebuild clears the memo.

Proof that the memo changes no result.
A station has a candidate when a berth of one of its entry groups has a route.

1. The groups and their berths depend only on the network (passenger and parking status, berth nodes, banks, and class permissions), the class of the pod, and the blocked berths.
   Berth availability changes the order of the berths in a group, and not the set, because each group also tries the berths that are not available.
2. `assignedRoute` is the free-flow route, so it returns an error exactly when that route returns an error.
3. The free-flow route depends only on `from`, the berth node, the class, the network, and the blocked set.
   The suffix starts at `from`, so a change of the position with the same `from` changes no route.
4. Queue samples change costs and not the existence of a route, so they need no invalidation.
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
| Deferred platoon member | The follower holds no resource of a pod ahead, and the link ends. |
| Deferred, divertible, cadence | The next tick with `(s.tick - start) % TicksPerSecond == 0`. |
| Deferred, no candidate station | A clear or an owner movement that opens a route; see P11. |
| Deferred, faulted | The clear or the evacuation. |
| Deferred, in the arrival chain | Arrival at the current berth. |
| Bound, waiting for a resource | Its owner moves and releases it. |
| Bound, berth occupied | The phase timer of the pod at the berth ends, and the berth clears, terminal reevaluation moves the pod to another free berth, or `clearBlockedBerths` moves an idle pod whose trip cannot board (suspension contract section 9.4). |
| Unloading | The phase timer ends. |

## 10. Settings, commands, and session events

### 10.1 Project settings

```json
"emergencies": {"perHour": 0}
```

| Member | Type | Range | Default |
| --- | --- | --- | --- |
| `perHour` | number | 0 in stage 3. 0 to 60 from stage 7. | 0 |

Rules:

- `emergencies` is optional.
  An absent member and `"emergencies": {}` both give the rate 0.
- The markers `incidentContract` and `emergencyContract` were removed on November 4, 2026 (section 11.1), and a project with one has an unknown member.
- Unknown members and null values are refused.
- `perHour` above 0 is refused in stage 3.
- `Validate` adds the widest `emergencies` object to its size estimate, as for `faults` (stage 2, section 12.1).
- The field scan (`internal/project/service.go` `scanProjectFields`) records `emergencies`.
- `web/editor.js` keeps the member of a loaded project and writes it back unchanged.
  The editor gets no control for it in stage 3.

The simulation gets `SetEmergencies(enabled bool) error`, beside `SetFaults` (`internal/sim/faults.go` `(*Simulation).SetFaults`).
It refuses to turn emergencies off while a record exists.
The session turns emergencies on for every fleet but the demo fleet (`project.ConfigureExperiments`).

### 10.2 Sampler mechanism, for stage 7

Stage 3 does not run the sampler, because `perHour` is 0.
This section fixes the mechanism, so that stage 7 adds the rate without a new format decision.

- The sampler belongs to the simulation, with its own `math/rand/v2` PCG stream.
  It never draws from the demand PCG or from the fault sampler.
  Its seed is the first 16 bytes of `SHA-256("podsim-emergencies-v1" || bigEndian(demand seed))`, as two `uint64` values, as stage 2, section 12.2, derives the fault seed.
- Events form a Poisson process at `perHour` for the whole fleet.
  After each event, and at start, the sampler draws `u` and sets `nextTick = tick + max(1, ceil(-ln(1-u) × (3600 × TicksPerSecond) / perHour))`.
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
| `emergencies are not enabled` | Emergencies are off, as in the demo fleet. |
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

The pod inspector (`internal/view/game.go` `(*Game).inspectionRows` (at 6227cf7)) gets one button when the frame does not have `operationsOff`: "Emergency" on a pod that carries passengers and has no record.
It sends `emergency` with the pod ID and no `orderID`, so the party is the first active rider.
The view refuses nothing on its own: a refused command shows the existing command error.
The control is product choice P21.

### 10.7 Session events and restore tiers

| Event | Effect |
| --- | --- |
| Pause | Ticks stop, so the stage and the cadence stop. Commands still run. |
| Reset | `Reset` (`internal/sim/simulation.go` `(*Simulation).Reset`) clears the records, `emergencyHold`, and the counters. `incidentSerial` stays. |
| Demo | The demo fleet has emergencies off. Records end as on reset. |
| Project apply | Any change to `emergencies` fails `sameProject` (`internal/session/persist.go` `sameProject`), so the fleet is rebuilt and every record ends. An identical project keeps the records. |
| Checkpoint and rewind | `Clone` (`internal/sim/clone.go` `(*Simulation).Clone`) deep-copies the records and the counters. A rewind restores them exactly. The new generation gives new records a new ID prefix. |
| Physical restore, every record pod keeps its place | Records and counters are restored, after the pre-tier checks of section 11.5. The phase comes from the restored purpose. A deferred record runs its advance in the next emergency stage. |
| Physical restore that would demote a record pod (`internal/sim/state_physical.go` `(*physicalRestore).placeDemoted`) | The physical tier fails. The restore then follows the fallback rules of stage 1, section 9.6, as stage 2, section 12.7, does for a faulted pod. No single record is cancelled to keep the physical tier. |
| Logical restore | Every record ends. The stage 1 logical tier does not change: it builds a fleet with no purposes, handles a saved purpose 1 (stage 1, section 9.6), and copies each saved hold (`internal/sim/state_logical.go` `restoreLogical`). With emergencies on, the tier then calls `restoreService(v, emergencyHold)` for each pod with the hold, after the purposes are cleared, so no withdrawn pod is left without a record (E2). With emergencies off, `sim.RestoreState` keeps the holds as stage 1 restores them, and only the stage 1 fixture tests reach that path. Counters are kept. `RestoreResult` (`internal/sim/state.go` `RestoreResult`) gets `DroppedEmergencies`, the number of records that ended. |
| Invalid emergency data, or a save that the policy validator rejects | The pre-tier checks reject the whole save as `invalid_state`, with no logical fallback, and it moves aside (section 11.5). |

## 11. Formats

### 11.1 Marker and its propagation

This section defined the feature marker `emergencyContract: "emergency-v1"`, which required the incident marker and gated every stage 3 member.
It also defined the copies of the marker in the topology and in the full frame, the agreement check, the raw presence scan, and the web marker check.
On November 4, 2026, [the marker removal contract](marker-removal-contract-proposal.md) removed the marker and each presence rule.
A project, a saved state, a stream envelope, or an HTTP state with an `emergencyContract` member now has an unknown member.
A frame of a fleet with emergencies off has `operationsOff`, and such a frame with an emergency record or counter is refused with `stream state with operations off contains an emergency member`.

### 11.2 Digest registry entries

| N | Field path |
| ---: | --- |
| 4 | `Command.PodID`, from stage 2, section 13.2 |
| 10 | Retired with the marker. It was `Command.Project.EmergencyContract`. |
| 11 | `Command.Project.Emergencies` |
| 12 | `Command.OrderID` |

Each field sits at a fixed path, as stage 1, section 11.3, requires.
`Emergencies` is a pointer to a struct, so the extension pair encodes the whole object, and its inner field is not tagged.
A command with no set extension field keeps its digest.
`digestExtensions` (`internal/session/receipt.go` `digestExtensions`) gets the three new rows after the stage 2 rows.

### 11.3 Save members

| Path | Shape | Presence |
| --- | --- | --- |
| `/project/emergencies` | Object, section 10.1 | Optional. |
| `/simulation/emergencies` | `{"records": [...], "counters": {...}}` | Omitted only when there is no record, every counter is 0, and the sampler is not live. A live sampler saves `random` and `nextTick` (stage 7 section 4.2). |
| `/simulation/emergencies/records/*` | `[generation, serial, start, pod, order]` | `records` is omitted when empty. |
| `/simulation/emergencies/counters` | Object of the counters of section 10.5 | Each counter is omitted at 0. `counters` is omitted when every counter is 0. |

`pod` is an index into `/simulation/pods`, and `order` is the order ID of the party.
The tuple uses an index for the reason of stage 1, section 11.7.
Every tuple element is always present, so index 0 is never omitted.
The phase, the mask, and the commitment are in the pod tuple of stage 1 (`withdrawn` and `operational`), so no other member is needed.

### 11.4 Stream and HTTP state members

| Path | Shape | Delta group |
| --- | --- | --- |
| `.../simulation/emergencies` | `{"active": [...], "counters": {...}}` | New group `emergencies`. |

Each element of `active`, in serial order:

```json
{"id": "i3.21", "podID": "p7", "orderID": 412, "phase": "bound", "startTick": 1200}
```

- Every member is required.
- `phase` is `deferred`, `bound`, or `unloading` (section 4.2).
- The group changes only at a start, at a phase change, at an end, and at a counter change.
  `emergencyTicks` changes on each tick while a record exists, as a counter of the group.
- `active` and `counters` are omitted when empty, so the group with no emergency is `{}`.

Vehicles get no new member.
`withdrawn` and `operational` already show the hold and `emergency-unload` (`internal/sim/simulation.go` `Vehicle`).

### 11.5 Strict decoding and prescan

Every decoder rejects unknown members.
Every decoder rejects a stage 3 member that is an explicit null, and the save decoder rejects an empty `emergencies` member.

Pre-tier checks.
`checkSavedEmergencies(input)` runs before either restore tier, beside `checkSavedBankRoutes` (`internal/sim/state.go` `restoreState`).
It reads only the saved records and the saved pod tuples, so the logical tier cannot remove the evidence that it needs.
A failure rejects the whole save as `invalid_state`, with no logical fallback, and the save moves aside.

With emergencies off, it rejects a save that has an `emergencies` member, with `saved emergencies need emergencies on`.
With emergencies on, it rejects:

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

`checkBoardingFields` runs before `checkSavedEmergencies`, and it checks the boarding records, the phase, and the berth of each pod with riders.
For such a pod, its error therefore comes first in the cases of E4, E5, and E8, for example `pod 01 boarding records: operational purpose 3 does not agree with activity traveling` for E8.
The save still gets `invalid_state` before either tier.

The session runs `CheckIncidentPolicy` before either tier (section 8).

When the base frame has `operationsOff`, the stream decoder rejects each replacement `emergencies` group, and this includes `{}`, with `stream state with operations off contains an emergency member`.

The stream and HTTP decoders apply these rules to a full frame and to an `emergencies` replacement group alike:

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

This section is a record.
The budget keeps its 300-record basis, and nobody measured it again.
The plain fleet limit is now 1,000 pods, Express keeps 600 pods, and the save cap is 128 MiB.
The composed fixtures and the allocation test of section 14.4 were deleted on October 7, 2026, so the Formats gate of section 15 has no composed fixture.
The coupling shapes of the tables went with physical coupling on October 6, 2026, and the marker bytes with the markers on November 4, 2026.

The budget uses 300 records, the pod limit at that time (`internal/project/config.go` `MaxPods`), so it holds for any cap that product choice P19 selects.
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

The emergency marker that this section gated on was removed on November 4, 2026, by [the marker removal contract](marker-removal-contract-proposal.md).
Emergencies are now on in every fleet but the demo fleet.
The off state is a run with no emergency record and an emergency rate of 0.
In that run the emergency code changes no trajectory and no saved-state byte.
The marker removal contract requires saved-byte identity for a plain run with no incident command, and its measurements report identical `cmd/compare` output.
The Q7 exception of this section went with physical coupling on October 6, 2026.
Stage 3 adds no random source.

| Change | Off-state effect | Treatment |
| --- | --- | --- |
| Emergency stage | Runs only with `emergenciesOn`, and it changes nothing with no record and a rate of 0. | Gated by state. |
| `intent.emergency` and the tier in `compareAdmission` | 0 for every intent without a record, so the sort is unchanged. | Gated by state. |
| `maintainLink` and `tryLink` record tests | No record. | Gated by state. |
| Routing view | Built only inside `advanceEmergency`. With `s.routeView` nil, the routing code is unchanged. | Gated by state. |
| No-candidate memo | Written only by a station choice. | Gated by state. |
| Purpose 1 amendments of section 9.2 | Purpose 1 needs a record. | Gated by state. |
| Claim surrender gate | No record and no `emergencyHold`. | Gated by state. |
| Save, stream, and HTTP members | With no record and every counter at 0, the save member is omitted and the stream group is `{}`. | Gated by state. |
| Digest tags | No tagged field set, so no new pair. | Gated by value. |

Gates for every patch, as defined at approval, when the marker and physical coupling existed.
The Off parity requirement stays in force under sections 5.1.5 and 5.1.9 of the stage 7 contract, and amendment A1 of that contract removes only Marker-only parity:

- The format goldens match byte for byte with the emergency marker absent.
- The digest baseline (`internal/session/testdata/command_digests.txt`) matches with the marker absent.
- Matched-seed runs on LondonCentral and the rail-hub preset, with the marker absent and present, give identical exported simulation states at every 10 s of simulated time, apart from the marker.
- Patch 1 (Q7) also runs the matched-seed check with the fault marker and no fault, which must give identical states.

## 13. API for later stages

| Function or state | Caller | Preconditions |
| --- | --- | --- |
| `Emergency(podID, orderID)` | The session command; the stage 7 sampler through `startEmergency` (stage 7 contract, rule G9) | Section 6.1. |
| `advanceEmergency(r)` | Stage 6 would replace the platoon wait with a group exit. Stages 4 to 6 are out of scope (section 1.2). | Section 5.4. A later stage removes a deferral only with its own certified transition and tests. |
| The station choice of section 9.1 | Stage 5, if a reverse target needs an unload station. Stage 5 is out of scope (section 1.2). | Runs under one routing view, and writes no simulation state other than the no-candidate memo. |
| `intent.emergency` and the tier | None. The stage 4 use for a train went with physical coupling. | Section 9.3. |
| `emergencyHold` in F11 and in `incidentOutstanding` | Every later stage. | Section 8, section 9.4. |

Exported entries:

| Entry | Kind | Use |
| --- | --- | --- |
| `Simulation.Emergency(podID string, orderID int) (string, error)` | Public | The session command. |
| `Simulation.SetEmergencies(enabled bool) error` | Public | The session, for every fleet but the demo fleet. |
| `SavedState` and frame members of section 11 | Public | The session codecs. |
| `RestoreResult.DroppedEmergencies int` | Public | The session restore report. |
| `CheckIncidentPolicy(input RestoreStateInput) error` | Public | The policy step of the session restore (section 8). |

Stage 3 adds no test entry.
Its session tests start emergencies through the `emergency` command, and its `internal/sim` tests call the operations directly.

Preconditions that later stages must meet:

- A stage that withdraws a pod for a new cause amends F11 first.
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
| Routing view | The choice and the installation leave both route memos unchanged, and the installed suffix equals the candidate suffix. |
| Pruning | A randomized test compares the pruned scan with the exhaustive scan in 1,000 cases from fixed seeds. Each case has a random network with banked, unbanked, and `ParkingOnly` stations, a class rule, a blocked set, stopped pods that make queues, and the emergency pod at a random divertible position. Some cases have equal tick keys at several stations. Both scans run from clones of one state and give the same pair and the same installed route. The pruned scan makes fewer route searches in at least half the cases, so the stop rule runs. |
| Long prefix | A pod route with a prefix of more than 20,000 lane occurrences on a repeated loop: with the term guard holding, the pruned and the exhaustive scans give the same pair, and with a prefix that fails the guard, the choice evaluates every station and gives the same pair. |
| Warm-memo clone parity | A fixture with a filled no-candidate memo is cloned, and the clone starts with no memo. Both run one continuation. `sameState`, which excludes `emergencyMisses` and the search counters, holds after each step, and the exported trajectories are identical at every tick. |
| No-candidate memo | After a no-candidate choice, the next cadence tick with the same `from` and class makes no search. A new `from`, a blocked-set change, a network change, `Reset`, a project replacement, and a restore each make the next choice search again. A choice that finds a candidate removes the entry, and a refused installation writes none. A randomized run with and without the memo gives the same choices on every tick. |
| Bank searches | A source on the departure path of a bank and a target berth in the same bank, where the local search fails: the counters show 4 graph searches in the route search, the failed local attempt and then the departure, middle, and arrival searches, and at most 12 in one `assignedRoute` call. |
| Arrival chain | A pod that cannot divert arrives at its current berth and unloads there in the next stage. |
| Cadence | A pod that becomes divertible chooses at the next tick with `(tick - start) % TicksPerSecond == 0`, and not before. |
| No candidate | Every station unreachable: the pod keeps its route and stays deferred. With the no-candidate memo, the next cadence ticks make no search until `from` or the class changes or the memo is cleared (section 9.1). |
| Outcomes | The party is interrupted, also at its own destination. Each other party completes at its destination or is transferred. A transferred party can board the emergency pod again after the end. |
| End | The record ends in the tick of the unload, the hold is released, and the pod is supply in the same `dispatch`. |
| Party leaves | A faulted pod with a deferred record: `InterruptRider` interrupts the party, and the next emergency stage ends the record before the faulted skip. |
| Platoon | A link with a record at either end drains and is never extended. A new link with a record pod is refused. The pod binds after the link ends. A restore during the drain keeps draining. |
| Faults | Each cell of the tables of section 5.7. A clear before evacuation continues the emergency. An evacuation ends the record in the same tick. |
| Priority | Two pods request one free junction in one tick: the emergency pod gets it, also against an aged intent. An owned resource is never taken. Two emergencies: the lower serial first. |
| Amendments | A bound pod whose berth is taken moves to another free berth of the station, and the detour check does not refuse it. The stage 2 endpoint reroute keeps the emergency berth for an obstructed bound pod. |
| Claim surrender | A waiting empty relocation gives up its unused claim on the berth of a bound pod. |
| Counters | Each counter at its maximum saturates. |
| Invariants | E1 to E8 and F11 after every tick and command in each test above. |

### 14.2 Restore tests

| Case | Expected |
| --- | --- |
| Physical restore of each phase, and of a deferred platoon member and a deferred faulted pod | Records, purposes, and holds restored. The next stage continues the emergency. |
| Physical restore that would demote a record pod | The physical tier fails. The fallback follows stage 1, section 9.6. |
| Logical restore with each phase | Every record ends. `DroppedEmergencies` counts them. Counters are kept. Stage 1 rules handle the purposes. No pod keeps `emergencyHold`. |
| Logical restore with emergencies off, through `sim.RestoreState`, of a save with `emergencyHold` from the stage 1 test entry | The hold stays, as the stage 1 test (`internal/sim/operational_test.go` `TestOperationalRestoreTiers`) requires. |
| The same save, and a save with purpose 1 and no record, through `NewFromStore` | `invalid_state` before either tier, by E2 and E4 with emergencies on. The save moves aside. |
| Save with a record whose pod has purpose 2 or 3 and an active rider | `invalid_state` from the pre-tier checks (E8). No logical fallback. |
| Save whose record names a party that is not the marked rider of a purpose 1 pod, with a physical error that would fall back to the logical tier | `invalid_state` from the pre-tier checks. No logical fallback. |
| Save with a serial of 0, or a serial that a fault record also has | `invalid_state`. |
| Save with a record and emergencies off, a pod with `emergencyHold` and no record, two records for one pod, or a serial above `incidentSerial` | `invalid_state`. |
| Rewind after a start | The same record, and a new generation for new IDs. |

For a pod with riders, the error of the E4, E5, and E8 cases can come from the boarding check that runs first (section 11.5).

### 14.3 Codec tests

Each rule of section 11.5 has a rejecting test, for the save, a full frame, an HTTP state, and a delta.
The digest registry test covers `N = 10` to `N = 12`, with `N = 4` shared with the `fault` command.

### 14.4 Composed byte tests

`TestComposedWorstCaseFormats` prescanned and decoded each composed fixture, with the stage 3 members at their widest and `MaxEmergencies` records.
The allocation test `TestEmergencyByteAllocation` encoded 300 widest records and 300 widest stream rows, with no prescan and no decode, and checked the totals of section 11.6 against the allocations.
Both tests were deleted on October 7, 2026.
`TestFormatArraysHaveLimits` and `TestEmergencyStreamArrayLimits` keep the checks of the array limits.

### 14.5 Command-boundary and end-of-tick saves

A save after each start, at each phase change, and at each end restores in the physical tier and passes `CheckContract`.
Paused starts are included.

### 14.6 Mutation targets

The table records the targets at landing.
The rows for coupling members, the approach, the Q7 skip, `couplingMemberEligibility`, `bufferClaimCanYield`, the stored history of the routing view, and the marker name code that was removed later.

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
| Install with a fresh routing view after the choice | Routing view test: the installed suffix differs. |
| Let a choice write the free-flow memo | Routing view test: the memo differs from its copy. |
| Read the stored history on first use | Routing view first-use test. |
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
| Off parity | The off-state gates of section 12 on every preset. | Byte identity, digest identity, and identical states at every 10 s of simulated time. |
| Marker-only parity | Removed by amendment A1 of the stage 7 contract, because the markers no longer exist. | |
| Safety | A soak driver on LondonCentral and the rail-hub preset that starts emergencies on random occupied pods through the command, at about 10 per simulated hour, for 24 simulated hours per seed, over at least 20 seeds, with faults on and off. | `CheckContract` holds after every tick and command. No owner conflict. |
| Contract | The same soak, with E1 to E8, F11, and W1 to W5 checked after each tick. | No violation. |
| Conservation | The same soak. | `RequestID = Completed + Interrupted + queued + aboard` at every tick. |
| Progress | The same soak. | Each record ends, and each record of an ordinary pod ends within 10 simulated minutes. A run that misses the gate lists each remaining record with its phase and wait reason. |
| Choice | The same soak, with the estimate of the chosen station against the actual time to the unload. | Reported, not gated. |
| Ordinary traffic | Admission wait of ordinary intents against a run without emergencies. | Reported, not gated. |
| Persistence | Saves at random boundaries of the soak, restored in the physical tier, and rewinds to random checkpoints. | Every save restores and passes `CheckContract`, or fails the physical tier only by the demotion rule. Every rewind replays exactly. |
| Performance | Step time of the soak against a run without emergencies, with `MaxEmergencies` records in the cadence search. | Median step time within 10 percent, and no tick above twice the slowest tick of the run without emergencies. |
| Worst choice tick, presets | LondonCentral and the rail-hub preset with four emergencies that start on one tick, cold and warm. | The worst choice tick is at most 50 ms on the CI runner class. |
| Worst choice tick, no candidate | A fixture at the project limits (12,000 nodes, 20,000 lanes, 600 stations) with four deferred, divertible records that started on one tick and find no candidate, so all four search on one cadence tick. The tree reaches the berths, and the bank rules refuse each route search, so pruning skips nothing. If `Validate` allows no such network, the fixture uses the largest count of such berths that it allows, and reports it. Cold, with no route memo, and warm, after 600 ticks of the traffic of the fixture. | Reported against the 50 ms threshold on the CI runner class, cold and warm, and never gated (product choice P23). It is expected to miss, because pruning cannot lower the worst case. The counts of graph searches by kind are reported and within the bound of section 9.1. The counters show failed route searches and a failed same-bank local attempt. |
| Worst choice tick, delayed routes | The same network size, with four records on one cadence tick, reachable berths, and stopped pods that delay the free-flow routes. Cold and warm, as above. | The worst tick is at most 50 ms on the CI runner class, cold and warm. |
| Formats | The composed fixtures of section 14.4 were deleted. By amendment A4 of the stage 7 contract, `TestPlainStateFileWorstCaseSize` and `TestPlainStreamMaximumEncoding` take their place. | Every shape under its cap. |
| Web | The browser shell and editor. | `mise run test:web` passes. |
| Product | The product choices of section 18.2. | Done: the maintainer approved P1 to P22 on October 5, 2026, and decided P23 on October 5, 2026, at 23:59Z. |

Amendments A1 to A4 of the stage 7 contract change these gates: A1 removed the marker-only parity gate and the marker checks of the Web row, and A4 replaced the composed fixtures.

The warm-up of the row "Worst choice tick, no candidate" stays 600 ticks, which is 20 s at 30 Hz, with a trip each 20 ticks, so the count of trips does not change.
The full-limit report of that row runs only in `mise run qualify`.
A small fixture checks the search counts in every test run.

The latency fixtures were measured on October 7, 2026, at the project limits of 600 stations, 12,000 nodes, and 20,000 lanes, with a fleet of 600 pods.
The no-candidate fixture has 600 stations of 13 berths, 12,000 nodes, and 19,800 lanes.
The delayed-routes fixture has 600 stations of one berth, 12,000 nodes, and 19,931 lanes.
On an AMD Ryzen 5 3600 at `nice -n 19` under load, the slowest delayed-routes tick took 10.3 ms, within the 50 ms gate.
The slowest no-candidate tick took 1.0 to 1.6 s, with at most 93,760 graph searches.

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
The list records the order at landing.
Patch 1 went with physical coupling, the approach abort of patch 4 with it, and the marker parts of patches 6 and 7 with the markers.

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

## 18. Requirements and product choices

### 18.1 Settled requirements

A binding decision, an approved contract, or a safety rule settles each row.

| # | Requirement | Source |
| --- | --- | --- |
| R1 | The emergency pod gets free resources first and never takes an owned one. | Decision 1. |
| R2 | A coupling member stays in its train until the split at its committed split site. | Decision 2; stage 1, section 13. Physical coupling was removed on October 6, 2026. |
| R3 | Every party leaves the pod at the emergency station. The party is interrupted. Each other party completes or is transferred, and can ride any pod. | `6227cf7:PROJECT_BRIEF.md:700-701`; decision 3; stage 1, section 9.5. |
| R4 | Pending pickups of the pod go to other pods, with the exclusion until boarding. | Decision 4. |
| R5 | An emergency never evacuates. A fault evacuation, at rest only, interrupts every rider. | Decision 5; stage 2, section 5.5. |
| R6 | The stage is off by default, with off-state parity, and refuses each unsupported target before any change. | Decision 6. |
| R7 | Coupling discovery and adoption skip a withdrawn pod and a pod with a purpose. Discovery also skips a pod that a record names. | Decision 7, revision 7. Physical coupling was removed on October 6, 2026. |
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
| P15 | Approach member | Approved by the maintainer, 2026-10-05. Abort the approach. Moot since physical coupling was removed on October 6, 2026. | The pod is not yet in a train, so decision 2 does not hold it, and the abort reuses the existing path. | Let the pair couple, and defer to the split. |
| P16 | Emergency on a faulted pod | Approved by the maintainer, 2026-10-05. Accept it as deferred. | The rider need is real, and the fault rules stay in charge until the clear or the evacuation. | Refuse with a new error. |
| P17 | The party leaves before the unload | Approved by the maintainer, 2026-10-05. End the record. | The cause has left the pod. | Keep the record, and unload the other parties with no interruption. |
| P18 | Priority among emergencies | Approved by the maintainer, 2026-10-05. The earlier start (lower serial) first. | Deterministic and simple. | The lower `E` first, or the more riders first. |
| P19 | Limits | Approved by the maintainer, 2026-10-05. One record for each pod, and at most 4 active records. | Each record removes the age protection of competing intents, so a small cap limits the effect on ordinary traffic. The budget holds up to 300. | A larger cap, or none. |
| P20 | Metrics | Approved by the maintainer, 2026-10-05. The three counters of section 10.5 and the stage 1 counters. The full set in stage 7. | Enough to check the soak gates. | Time from start to unload as a distribution, and the delay of other riders, now. |
| P21 | UI control | Approved by the maintainer, 2026-10-05. One "Emergency" inspector button with one press, as the stage 2 "Fault" button. | Consistent with stage 2. | A confirmation step, or a party picker. |
| P22 | Cancel | Approved by the maintainer, 2026-10-05. No cancel command. | A cancel needs `rebindOperationalOwner` or a new end rule for each phase. | A `clearEmergency` command for a deferred record. |
| P23 | Latency of the station choice | Decided by the maintainer, 2026-10-05: "Exact P13, gate realistic". P13 stays exact with pruning, the term guard, and the no-candidate memo. The worst choice tick is gated at 50 ms on the CI runner class on LondonCentral and the rail-hub preset with four emergencies on one tick, and on the delayed-routes fixture at the project limits. The adversarial no-candidate fixture is reported only. | The exact rule keeps the approved result of P13. Realistic networks meet the threshold, and on an adversarial network a pathological first choice stalls one tick and the memo stops it from repeating. | The budgeted scan of revisions 3 to 5, rejected because it changes the result of P13. A higher threshold. |
