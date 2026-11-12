# Incident service transitions contract proposal

Status: approved on 2026-10-05, with the proposed default for each open question in section 18.
Amended through 2026-11-12.
Git history holds the dated amendment notes and the earlier text.

Revised for item 7: one save family (version 9) and one stream family (hello 6, hello 7 since the motion-only deltas).

This contract is stage 1 of the staged incident redesign.
It defines the service transitions that vehicle faults and rider emergencies share.
It adds no motion change and no feature policy.
Later stages call the operations below, and each later stage is a thin policy on top of them.

The audited source is `069bafb`.
Its Go sources are identical to `5c90c9f`, so line references from the parked fault and emergency reviews are valid here.
Every `file:line` reference below is for `069bafb`, except where a reference names another commit.

## 1. Scope

### 1.1 What stage 1 delivers

Stage 1 delivers seven parts:

1. Service withdrawal: one operation, with an explicit inverse, that removes a pod from every supply path (section 4).
2. Pickup release: an order operation that returns the pending pickups of a pod to dispatch, with a persisted exclusion of that pod (section 5).
3. Claim kinds: one classification, beside the reservation code, that decides which claims can be revoked (section 6).
4. Order continuation: a current leg origin that is separate from the immutable order origin, with a complete reader inventory (section 7).
5. Interrupted outcome: a terminal order outcome that is not a completion (section 8).
6. Operational destination: a physical destination and purpose that are separate from the service stops of the riders (section 9).
7. Shared format extension: one digest registry, one ID scheme, strict decoding, and one joint byte budget (section 11).

Section 12 states off-state identity.
Section 13 lists the API that later stages call.
Section 14 is the test plan.

### 1.2 Out of scope

- Motion suspension and braking holds.
  Stage 2 owns them.
- Reverse travel, train interruption, and group exits.
  They were the work of stages 4 to 6.
  The maintainer stopped the incident redesign after stage 3 on October 6, 2026, so stages 4 to 6 are out of scope, and the refusals that this contract leaves to them stay in place.
- The policies that decide when to call these operations.
  Stages 2, 3, and 7 own them.
  The maintainer reopened stage 7 on November 4, 2026, under [the stage 7 contract](incident-stage7-contract-proposal.md), as option B with amendments A1 to A4.
  The rules G1 to G16 of section 3 of that document, and its amendments, apply to this contract.
- Commands, game controls, scenario rates, and metrics panels.

Stage 1 is exercised through native tests.
It exposes no command.

### 1.3 Binding user decisions

From the [project brief](../../PROJECT_BRIEF.md#rider-emergency-stop), October 4, 2026:

- An emergency pod drops all of its later work.
  Its pending pickups go back to dispatch as ordinary orders and keep their original request times.
- All parties in an emergency pod unload at the emergency station.
  Each other party gets a new pod from there to its original destination and keeps its order identity.
- After a fault delay, riders leave the stopped pod, and their trips end as interrupted.
- Each feature is off by default.
  With every incident switch off, behavior and bytes do not change.

From the user, through the coordinator, October 4, 2026:

1. The persisted other-pod exclusion applies only to pending pickups that are released from an affected pod and are not yet boarded.
   Those pickups must go to other pods.
   A party that was aboard and is transferred after an emergency unload can use any pod, including the emergency pod once it is free.
   Transferred parties carry no exclusion state.
2. Emergency priority means winning free resources first, and alternate entries.
   It never revokes the claims of other vehicles.
   Stage 1 therefore has no priority-driven revocation API.
   The claim classification is still needed for pickup release and for existing yield paths.
3. A coupled train with an emergency splits at its already committed split site.
   Stage 1 operations refuse coupling members, and the stage 3 policy waits for the split.
   Physical coupling was removed on October 6, 2026, so this decision has no subject now.

From the approved stage plan:

- Released pickups go to other pods.
- Compact the encodings where reasonable, and raise the save cap if needed.

### 1.4 Format baseline

The format sections target the formats after three approved roadmap items land:

- Item 4: one project version, with features gated by markers.
- Item 5: exact-case JSON with lowerCamel members everywhere.
- Item 7: save version 9 and hello 6, in the version 8 and hello 5 member layouts without `textEncoding`, one HTTP state envelope and media type, and packed order text for every project kind.

Member names below are the post-item-5 names.
Implementation of section 11 starts after those items land.
If one of them changes shape, section 11 must be revised first.
Sections 4 to 10 do not depend on the formats.
On November 4, 2026, [the marker removal contract](marker-removal-contract-proposal.md) removed the incident, fault, and emergency markers, so no marker gates a member of this contract.
The saved state is now version 12, and the stream is version 10.

## 2. Terms

| Term | Meaning |
| --- | --- |
| Hold | One cause of a withdrawal. Stage 1 knows two holds: fault and emergency. |
| Withdrawn pod | A pod with at least one hold. It is not supply for any order. |
| Pending pickup | A waiting trip that is bound to a pod or holds for it (section 5.1). |
| Continuation | A waiting trip with `boarded` true: a transferred party or a rider that a restore requeued. |
| Stranded order | A transferred order with no certified path from its leg origin (section 7.6). |
| Excluded pod | The pod that a released pickup must not get. Stored on the waiting trip. |
| Order origin | `Request.From`. It never changes after acceptance. |
| Leg origin | The station where the party boards, or will board, its current pod. `legOrigin()` returns `LegFrom` when present, otherwise `From`. |
| Transfer | A party leaves a pod before its destination and goes back to the queue with a new leg origin. |
| Interrupted | A terminal order outcome. The order is not complete, not queued, and not aboard. |
| Purpose | The reason for the physical destination of a pod: service, emergency unloading, refuge travel, or empty recovery. |
| Service stops | `Vehicle.Stops`: the destinations of the active riders. |
| Physical destination | `destinationStation` and `destination` of a pod. In service they agree with `Stops[0]`. For other purposes they do not need to. |
| Release boundary | `releaseCleared` in `internal/sim/simulation.go` `(*Simulation).Step`. Resources released during a tick are not reused before the next tick. |

## 3. Existing source boundaries

| Source | Audited behavior | Consequence |
| --- | --- | --- |
| `internal/sim/simulation.go` `vehicle` | A pod bundles riders, stops, physical destination, relocation flags, and route ownership. | Stage 1 adds a hold set and a purpose to it. |
| `internal/sim/dispatch.go` `waitingTrip` | A trip has a request, a cached route and berth, and deferral fields. | Stage 1 adds the excluded pod. |
| `internal/sim/simulation.go` `Request` | One struct holds the order identity and the pod binding. | Stage 1 adds `LegFrom`. |
| `internal/sim/dispatch.go` `(*Simulation).dispatch` | Dispatch unbinds a trip itself: it clears `PodID`, and under the Express contract also the route and berth. | Pickup release reuses this unbinding, plus the deferral fields. |
| `internal/sim/released.go` `(*Simulation).releasePickup`, `releasable` | `releasePickup` only sets `released` on a releasable empty pod. | It is not an order operation. Section 5 adds one. |
| `internal/sim/diversion.go` `(*Simulation).pickupCandidate` | The common test of most pickup selection paths. | The main withdrawal gate. |
| `internal/sim/pickup_estimate.go` `(*Simulation).waitForFinishingPod` | Holds a trip for any busy pod that fits, and records it in `deferPodID`. Several trips can name the same pod. | Withdrawal ends these holds. Exclusion applies to them. |
| `internal/sim/station_buffer_claim.go` `(*Simulation).bufferClaimCanYield` | Decides whether a remote empty claim yields to a passenger head. It does not check coupling commitments. | The claim classification replaces its predicate. |
| `internal/sim/redistribution.go` `(*Simulation).yieldRelocationClaims` | Releases the destination claims of a relocating pod when a passenger arrives there. It does not check coupling commitments. | Same classification. |
| `internal/sim/coupling_reservation.go` `(*couplingReservationPlan).preserveReceivingClaims` | A train keeps individually tagged receiving claims of its members. | These claims are committed. They never yield. |
| `internal/sim/coupling_motion_owners.go` `sealCouplingMotionOwners` | A preserved claim with another owner is a motion invariant failure. | Same consequence. |
| `internal/sim/state_contract.go` `phaseRules` | Both restore tiers and live checks apply one rule per pod phase. Traveling occupied pods need `Stops[0] == DestinationStation` (`checkPodStops`). | Each purpose gets its own stop rule. |
| `internal/sim/state_contract.go` `(SavedState).checkContract` | `unaccounted = RequestID - Completed - held`. | Interrupted orders enter this balance. |
| `internal/sim/riders.go` `(*Simulation).alight` | Completes riders and writes one `StepCompletion` each. | Interruption writes none. |
| `internal/sim/simulation.go` `(*Simulation).arrive` | A pod with `RelocatingTo` becomes idle without an unloading interval. | Arrival actions per purpose are added here. |
| `internal/sim/simulation.go` `(*Simulation).Step` | The unloading loop runs `alight` and `continueJourney` at the end of each interval. | The end of an emergency unload runs here. |
| `internal/sim/diversion.go` `(*Simulation).divertStart` | Refuses coupling members, platoon members, compact members, and pods inside the arrival chain. | Operational routes use the same prefix rule and the same refusals. |
| `internal/sim/diversion.go` `(*Simulation).redirect` | Sets `RelocatingTo` and releases the destination claims at once. | Occupied pods must not use it. |
| `internal/sim/traffic.go` `(*Simulation).releaseVehicleResources` | A pod that is not traveling releases every retained route resource except its berth at the release boundary. | A pod that becomes idle at a berth releases unused grants at the boundary. |
| `internal/session/receipt.go` `(*digestWriter).value` | Hashes every field, including zero fields and the nested project. | New command or project fields need the digest extension. |
| `internal/session/checkpoint.go` `(*Session).rewind` | Rewind installs the cloned simulation and increments the generation. | Incident IDs carry the generation. |
| `internal/rail/connections.go` `(*Connections).Advance` | Consumes completions. A pending record without an active request is invalid (`validateRecord`). | Interruption needs its own rail outcome, delivered before any save (section 8.5). |
| `internal/session/persist.go` `MaxStateBytes` | 80 MiB for raw and compressed saves. | Section 11.7 budgets against it. |

This table records the audit at `069bafb`.
`MaxStateBytes` is now 128 MiB.
Physical coupling was removed on October 6, 2026, and station buffers and compact station queues on October 7, 2026.
The opt-in Compact station added in October 2026 (maneuver groups, `internal/sim/maneuver_*.go` and `internal/sim/station_maneuvers.go`) is a different feature, and this removal does not cover it.
Station entry runs, an early berth choice, and coast-in were added on October 7, 2026.
A platoon run can end on the entry lane of the destination station of both pods, and it never continues past the entry node of that station.
With platoons on, a pod that is not of a large class can try its berth choice before the usual point, and an early try that finds no berth does not refuse the pod.
With platoons on, a pod that waits for a contested station diverge can coast: a speed ceiling lowers only its commanded speed.
The incident refusal of platoon members and the emergency drain rule are unchanged.
The `coupling_reservation.go`, `coupling_motion_owners.go`, and `station_buffer_claim.go` rows name code that went with them, and `divertStart` now refuses platoon members, pods of a depot lane, and pods inside the arrival chain.

## 4. Service withdrawal

### 4.1 State

```go
// serviceHold is one cause that withdraws a pod from service.
type serviceHold uint8

const (
	faultHold serviceHold = 1 << iota
	emergencyHold
	knownServiceHolds = faultHold | emergencyHold
)
```

`vehicle` gets `withdrawn serviceHold`.
A pod is in service when `withdrawn == 0`.
Each hold is a separate bit, so one cause cannot clear the hold of another cause.
A later cause kind takes the next bit.

### 4.2 Operations

```go
// withdrawService adds hold to v. The first hold also releases the
// pending pickups of v (section 5).
func (s *Simulation) withdrawService(v *vehicle, hold serviceHold) error

// restoreService removes hold from v. It is the inverse of withdrawService
// for supply membership.
func (s *Simulation) restoreService(v *vehicle, hold serviceHold) error

// rebindOperationalOwner moves the operational purpose of v to another
// hold that v also has.
func (s *Simulation) rebindOperationalOwner(v *vehicle, to serviceHold) error
```

`withdrawService` preconditions:

- `hold` is exactly one bit of `knownServiceHolds`.
- `v.withdrawn&hold == 0`.
- No dispatch pass is in progress.
  The callers run at a command boundary or at a fixed place in `Step` outside `dispatch`.

`withdrawService` effect, in one call:

1. `first := v.withdrawn == 0`.
2. `v.withdrawn |= hold`.
3. When `first` is true, call `releasePickups(v)` (section 5.1).

`restoreService` preconditions:

- `hold` is exactly one bit of `knownServiceHolds`, as for `withdrawService`.
  A zero value, an unknown bit, and a mask of two holds are refused.
- `v.withdrawn&hold != 0`.
- `v.op.owner != hold`.
  This applies also when `v` has other holds.
  A purpose always names a hold that `v` has (invariant W5).

`restoreService` effect: `v.withdrawn &^= hold`.
It changes nothing else.

No stage 1 operation removes a hold.
Arrival actions and `settleIdleAtBerth` clear the purpose and keep every hold (section 9.5).
The policy that owns a cause calls `restoreService` after the purpose is clear.

`rebindOperationalOwner` preconditions: `v.op.purpose != opService`, `to` is one bit, `v.withdrawn&to != 0`, and `to != v.op.owner`.
Effect: `v.op.owner = to`.
A recovery that outlives the cause that started it uses this transition.
For example, when an emergency ends while its empty recovery travels and a fault hold is also set, the policy calls `rebindOperationalOwner(v, faultHold)` and then `restoreService(v, emergencyHold)`.
When no other hold is set, the emergency hold stays until the recovery arrives and the arrival clears the purpose (section 9.5).
The policy then calls `restoreService(v, emergencyHold)`.

Inverse property: for every state `x` and hold `h` that meet the preconditions, `restoreService(withdrawService(x, h), h)` has the same supply membership, route, physical destination, and ownership as `x`.
Two order-side effects of the first hold are not reverted:

- Released pickups stay released, and their exclusions stay, as the binding decision requires.
- A pod that `releasePickups` released keeps its `released` flag (`internal/sim/released.go` `(*Simulation).releasePickup`).
  This is the only flag that differs from `x`.
  The next dispatch pass after the restore parks the pod through the existing path (`internal/sim/released.go` `(*Simulation).parkUnclaimedReleased`).

### 4.3 Supply paths

`podFitsRequest` (`internal/sim/trip_admission.go` `(*Simulation).podFitsRequest`) does not get the gate.
It also decides order admission (`internal/sim/trip_admission.go` `(*Simulation).validateTripOptions`) and the dispatch reason through `hasFittingPod` (`(*Simulation).hasFittingPod`; `internal/sim/dispatch.go` `(*Simulation).dispatch`).
Admission stays static, as it is today, so a withdrawal never refuses a new order.

Each path below gets the test `v.withdrawn == 0`:

| Path | Anchor | Gate |
| --- | --- | --- |
| Pickup candidates | `internal/sim/diversion.go` `(*Simulation).pickupCandidate` | Returns false. This covers `pickupCandidates` (`internal/sim/dispatch.go` `(*Simulation).pickupCandidates`), `pickupPodMatching`, `pickupAvailable` (`internal/sim/pickup_estimate.go` `(*Simulation).pickupAvailable`), `pickupRouteWithAssignments` (`internal/sim/diversion.go` `(*Simulation).pickupRouteWithAssignments`), `sendPickupMatching` (`internal/sim/diversion.go` `(*Simulation).sendPickupMatching`), and `freePickupAlternative` (`internal/sim/pickup_reassignment.go` `(*Simulation).freePickupAlternative`). |
| Local pickups | `internal/sim/dispatch.go` `(*Simulation).localPickupForRequest` | Skips the pod. This covers `localPickup` and `localPickupForRequest`. |
| Shared-ride joins | `internal/sim/dispatch.go` `(*Simulation).boardingPods` | Skips the pod. This covers `joinSharedRide`. |
| Onboard pickups | `internal/sim/onboard_pickups.go` `(*Simulation).onboardPickupReady` | Returns false. |
| Promotion | `internal/sim/dispatch.go` `(*Simulation).promoteReadyPickup` | The ready pod must be in service. Invariant W2 makes this hold already. The test is defensive. |
| Finishing-pod holds | `internal/sim/pickup_estimate.go` `(*Simulation).waitForFinishingPod` | A withdrawn pod is not a candidate. A hold that names a withdrawn pod ends. |
| Hold refresh | `internal/sim/pickup_estimate.go` `(*Simulation).keepHold` | Same. |
| Pickup swaps and transfers | `internal/sim/pickup_swaps.go` `(*Simulation).swapEligible` | Returns false. `reassignPickup` (`internal/sim/pickup_reassignment.go` `(*Simulation).reassignPickup`) uses it. |
| Manual journey | `internal/sim/simulation.go` `(*Simulation).RequestJourneyOptions` | Returns `ErrBusy`. |
| Guarded supply | `internal/sim/positioning.go` `(*Simulation).guardedSupply` | A withdrawn pod adds no idle count, no relocating supply, and no inbound supply. Its destination berth stays busy. |
| Guarded candidates | `internal/sim/positioning.go` `(*Simulation).guardedCandidates` | Skips the pod. This covers `positionGuarded`. |
| Berth clearing | `internal/sim/parking.go` `(*Simulation).clearBlockedBerths` | A withdrawn idle pod is never moved as a blocker. The arrival keeps `BerthOccupied`. |
| Released parking | `internal/sim/released.go` `(*Simulation).parkUnclaimedReleased` | Skips the pod. |
| Claim yield to passengers | `internal/sim/redistribution.go` `(*Simulation).yieldRelocationClaims` | Skips a withdrawn relocating pod. |
| Passenger arrivals | `internal/sim/redistribution.go` `(*Simulation).passengerArrivals` | A withdrawn pod with riders is not a passenger arrival, so it cannot make a relocation claim yield (`(*Simulation).yieldRelocationClaims`). |

Berth accounting does not change.
A withdrawn pod keeps its grants and owners, and admission treats its requests as today.
Section 6.2 gives the reason: decision 2 allows no yield for emergency demand.

### 4.4 Caches

- The dispatch pass caches (`internal/sim/dispatch.go` `(*dispatchPass).begin`) are rebuilt by `begin` at each `dispatch` call (`(*Simulation).dispatch`).
  The operations do not run during a pass, so no pass cache can hold a withdrawn pod.
- `stationPickupBounds` (`internal/sim/pickup_bounds.go` `(*Simulation).stationPickupBounds`) caches free-flow travel bounds.
  They do not depend on supply, so they stay.
- Route caches depend only on the network and the class.
  They stay.
- The swap cooldown map (`internal/sim/pickup_swaps.go` `pickupSwapController`) is advisory.
  It stays.
- The admission pickup flag (`internal/sim/traffic.go` `(*Simulation).admit`) is computed from the waiting trips at each `admit`.
  A released pickup loses its pickup priority at the next admission.

### 4.5 Invariants

- W1: `v.withdrawn &^ knownServiceHolds == 0`.
- W2: no waiting trip has `request.PodID` equal to a withdrawn pod, and no waiting trip has `deferPodID` equal to a withdrawn pod.
  Section 5.1 clears active holds and stale deferral metadata alike.
- W3: no path in section 4.3 selects, moves, or counts a withdrawn pod as supply, and no withdrawn pod authorizes a claim yield.
- W4: a withdrawal changes no route, no physical destination, no speed, and no owner.
- W5: `v.op.purpose != opService` implies that `v.op.owner` is one bit and `v.withdrawn&v.op.owner != 0`.

`CheckContract` (`internal/sim/state_contract.go` `(*Simulation).CheckContract`) checks W1, W2, and W5 through the saved form after each tick and each command.

## 5. Pickup release

### 5.1 Operation

```go
// releasePickups returns every pending pickup of v to dispatch, and clears
// stale deferral metadata that names v. A released trip that never boarded
// excludes v until it boards. A later release replaces the exclusion. It
// changes no resource owner.
// It reports the number of released trips.
func (s *Simulation) releasePickups(v *vehicle) int
```

A waiting trip relates to `v` in one of four ways.
The cases are exclusive and are tested in this order:

| Case | Test | Meaning |
| --- | --- | --- |
| Bound | `request.PodID == v.Pod.ID` | `v` is assigned to the trip. |
| Active hold | `request.PodID == ""`, `deferPodID == v.Pod.ID`, and either `deferUntil == 0` or `s.tick < deferUntil` | The trip waits for `v` to finish (`internal/sim/pickup_estimate.go` `(*Simulation).waitForFinishingPod`). |
| Stale deferral | `deferPodID == v.Pod.ID`, and not an active hold | Metadata of an earlier hold. Normal assignment writes `PodID` and leaves the deferral fields (`internal/sim/dispatch.go` `(*Simulation).dispatch`), so a trip bound to pod C can still name `v`. A hold past its deadline is also stale. |
| Unrelated | Anything else | No change. |

For each trip in queue order, in one call:

| Case | Effect |
| --- | --- |
| Bound | Clear `request.PodID`, `request.DispatchReason`, `route`, `destination`, `deferCheck`, and `deferPodID`. When `boarded` is false, set `excludedPod = v.Pod.ID`. |
| Active hold | Clear `request.DispatchReason`, `deferCheck`, and `deferPodID`. When `boarded` is false, set `excludedPod = v.Pod.ID`. |
| Stale deferral | Clear `deferCheck` and `deferPodID` only. The binding to another pod, the route, and the berth stay. No exclusion. |

The route and berth clearing extends the dispatch unbinding in `internal/sim/dispatch.go` `(*Simulation).dispatch` to both order contracts.
`deferUntil` stays in every case, so a trip does not get a new hold budget.

A released trip keeps its queue position, `ID`, `From`, `To`, `LegFrom`, `RequestedTick`, `BoardedTick`, `boarded`, `deferUntil`, and order options.

When a trip was bound, call `releasePickup(v)` (`internal/sim/released.go` `(*Simulation).releasePickup`) once after the loop.
That keeps the existing `released` semantics for an empty pod on its way.

Only `releasePickups` reads the stale case, and only `withdrawService` calls `releasePickups`.
The off-state path keeps stale metadata as today, so off-state bytes do not change.

### 5.2 Exclusion lifecycle

`waitingTrip` (`internal/sim/dispatch.go` `waitingTrip`) gets `excludedPod string`.

The exclusion applies only to never-boarded pickups: `boarded` is false.
A continuation has `boarded` true.
This covers a party that a transfer requeues (section 7.3) and a rider that a restore requeues (`internal/sim/state_physical.go` `requeuedTrip`).
A continuation never gets an exclusion, also when a later withdrawal releases it.
This is decision 1.

| Event | Effect |
| --- | --- |
| `releasePickups(v)`, bound or active hold, `boarded` false | Sets `excludedPod = v.Pod.ID`. This replaces an earlier exclusion. |
| `releasePickups(v)`, `boarded` true | No exclusion. |
| A pod other than `excludedPod` receives the assignment | No change. |
| A swap, a transfer, or a reassignment moves the trip to a pod other than `excludedPod` | No change. |
| The trip boards, joins a shared ride, or joins by an onboard pickup | The trip leaves the queue, so the exclusion ends with it. |
| `restoreService` of the excluded pod | No change. |
| Physical restore clears an invalid `PodID` (`internal/sim/state_physical.go` `(*physicalRestore).restoreTrip`) | No change. |
| Logical restore unbinds the trip (`internal/sim/state_logical.go` `(*Simulation).unboundTrip`) | No change. |
| Checkpoint and rewind | `Clone` copies the trip by value (`internal/sim/clone.go` `(*Simulation).Clone`). |

The exclusion holds until the trip boards.
No path binds the trip to the excluded pod, moves the trip to it, or holds the trip for it.
A trip with an exclusion can have another pod, and it can hold for another pod.
`waitForFinishingPod` (`internal/sim/pickup_estimate.go` `(*Simulation).waitForFinishingPod`) does not choose the excluded pod as the finishing pod.
`waitForFinishingPod` and `keepHold` end a hold that names the excluded pod.
X1 makes that second test unreachable, so it is defensive.

A second release from another pod replaces the exclusion: the newest release wins.
After that release, the trip can get the first excluded pod.
One field holds the exclusion, so the representation does not grow.

The alternative keeps a set of excluded pods.
It costs up to one index per withdrawn pod for each trip.
This contract does not use it.

All writes of a new pod binding go through one helper:

```go
// assignPickup binds trip to v. Its callers never pass the excluded pod
// of the trip. It keeps the exclusion.
func assignPickup(trip *waitingTrip, v *vehicle)
```

It replaces the five binding writes: `internal/sim/dispatch.go` `(*Simulation).dispatch`, `(*Simulation).promoteReadyPickup`, `internal/sim/pickup_swaps.go` `(*Simulation).tryPickupSwap`, and `internal/sim/pickup_reassignment.go` `(*Simulation).tryPickupTransfer`.
It does not clear the deferral fields, so the off-state bytes do not change.

Invariants:

- X1: `excludedPod != ""` implies `boarded == false`, `request.PodID != excludedPod`, and `deferPodID != excludedPod`.
- X2: `excludedPod != ""` implies that `excludedPod` names a pod of the fleet.

Swaps, transfers, and reassignment work only on trips with a bound pod (`internal/sim/pickup_swaps.go` `(*Simulation).swapAssignments`, `(*Simulation).swapEligible`).
Such a trip can have an exclusion, so these paths get exclusion gates (section 5.3).
A test asserts X1 and X2 on every tick.

### 5.3 Exclusion gates

| Path | Anchor | Gate |
| --- | --- | --- |
| Remote selection | `internal/sim/dispatch.go` `(*Simulation).pickupPodMatching` | Skips the excluded pod. The function gets an `excluded string` argument from `pickupPodForRequest`. |
| Selection cache key | `internal/sim/dispatch.go` `(*Simulation).dispatch`, `pass.optionPickups` | The key becomes `dispatchKey{options: request.dispatchOptions(), excluded: trip.excludedPod}`. Two trips with equal options but different exclusions or leg origins cannot share a cached pod. |
| Local selection | `internal/sim/dispatch.go` `(*Simulation).localPickupForRequest` | Skips the excluded pod. |
| Promotion | `internal/sim/dispatch.go` `(*Simulation).promoteReadyPickup` | Skips a ready pod equal to `trip.excludedPod`. |
| Promotion, later trip | `internal/sim/dispatch.go` `(*Simulation).promoteReadyPickup` | Skips the swap when the current pod of the trip equals the exclusion of the later trip. |
| Shared-ride join | `internal/sim/dispatch.go` `(*Simulation).joinSharedRide` | Skips the excluded pod. |
| Onboard pickup | `internal/sim/onboard_pickups.go` `(*Simulation).joinOnboardPickup` | Skips the excluded pod. |
| Finishing-pod hold | `internal/sim/pickup_estimate.go` `(*Simulation).waitForFinishingPod` | Skips the excluded pod as a finishing pod. |
| Hold refresh | `internal/sim/pickup_estimate.go` `(*Simulation).waitForFinishingPod`, `(*Simulation).keepHold` | Ends a hold that names the excluded pod. X1 makes this unreachable. The test is defensive. |
| Hold reason | `internal/sim/pickup_estimate.go` `(*Simulation).pickupAvailable` | Skips the excluded pod, as `pickupPodForRequest` does, so `keepHold` sets the reason of the full pass. |
| Pickup swap | `internal/sim/pickup_reassignment.go` `(*Simulation).checkPickupPair` | Refuses a pair when either trip would get the pod that it excludes. `tryPickupSwap` (`internal/sim/pickup_swaps.go` `(*Simulation).tryPickupSwap`) runs only after this test. |
| Pickup transfer | `internal/sim/pickup_reassignment.go` `(*Simulation).checkPickupPair` | Refuses a free alternative that the trip excludes. `tryPickupTransfer` runs only after this test. |
| Reassignment | `internal/sim/pickup_reassignment.go` `(*Simulation).reassignPickup` | Uses `checkPickupPair`, so the swap and transfer gates apply. |
| Boarding | `internal/sim/dispatch.go` `(*Simulation).board` | Returns `ErrPartyAdmission` for the excluded pod. The gates above make this unreachable. The test is defensive. |

When the excluded pod is the only pod that fits, the trip waits with the existing reason "Waiting for an available pod".

### 5.4 Physical claims

Pickup release changes no resource owner.

A pickup assignment never claims a berth ahead of admission.
`sendPickupMatching` (`internal/sim/diversion.go` `(*Simulation).sendPickupMatching`) and an empty move with `reserveBerth` false (`internal/sim/parking.go` `(*Simulation).installEmptyMove`) write no owner.
The berth of a pickup becomes owned only through admission grants, which are stopping grants.
Parking and rebalancing moves do claim a berth ahead (`internal/sim/released.go` `(*Simulation).parkReleased`, `internal/sim/parking.go` `(*Simulation).installEmptyMove`), but those moves are not pickups.

An in-service releasable pod then follows the existing path: `releasePickup`, then `parkUnclaimedReleased`, then `redirect` (`internal/sim/diversion.go` `(*Simulation).redirect`).
`redirect` already releases the old destination claims in the dispatch stage, before `admit`.
That is existing behavior, so the release-boundary objection of fault review round 1, finding 8, does not apply to it.

A withdrawn pod is skipped by `parkUnclaimedReleased` (section 4.3).
It keeps its route, its physical destination, and its claims until a later stage changes its operational destination (section 9).

### 5.5 Tests

- An empty pod on its pickup route, an idle pod bound at the pickup station, and a pod named by three active holds.
  Each pod is withdrawn.
  Each never-boarded trip keeps `ID`, `RequestedTick`, `deferUntil`, and its queue index, and has `excludedPod` set.
  No owner changes.
- A trip bound to pod `C` with stale `deferPodID` `B`.
  Withdraw `B`.
  The trip stays bound to `C`, has no exclusion, and has empty `deferPodID`.
- A hold past its `deferUntil` that names `B`.
  Withdraw `B`.
  The trip gets no exclusion.
- A transferred trip and a restore-requeued trip, each bound to pod `B`.
  Withdraw `B`.
  Neither trip gets an exclusion.
- A trip whose best finishing pod is its excluded pod.
  The trip gets no hold for that pod.
- A trip released from `A` holds for `B`, and then `B` is withdrawn.
  The exclusion names `B`, and the hold ends.
  The trip can then get `A`, and it never gets `B`.
- For each gate in section 5.3, a fixture in which the excluded pod is the best candidate.
  The trip gets another pod or waits, and keeps its exclusion.
- A trip excluded from pod `A` and later assigned to pod `B`.
  The exclusion stays until the trip boards `B`, also after `A` is in service again.
- A trip excluded from pod `A` and bound to pod `B`, with pickup swaps on.
  No swap, transfer, or reassignment gives the trip `A`.
- X1 and X2 on every tick of the dispatch, swap, and onboard pickup suites with withdrawals injected.

## 6. Claim kinds

### 6.1 Classification

A new file, `internal/sim/claim_kinds.go`, holds the only answer to the question "can this claim be revoked".

```go
// claimKind classifies one resource that a pod owns or retains.
type claimKind uint8

const (
	claimNotHeld  claimKind = iota // v neither owns nor retains r
	claimOccupied                  // under the body of v
	claimStopping                  // granted track that v needs to stop
	claimRetained                  // tail retention of v
	claimLent                      // a follower of v retains r
	claimService                   // unused destination claim of an empty move
	claimOther                     // any other owned resource
)

func (s *Simulation) claimKind(v *vehicle, r resource) claimKind

// revocable reports whether a claim can be released without a physical
// transition.
func (s *Simulation) revocable(v *vehicle, r resource) bool {
	return s.claimKind(v, r) == claimService
}
```

The tests run in this order, and the first match wins:

| Kind | Test | Source of the rule |
| --- | --- | --- |
| `claimNotHeld` | The owner is not `v`, and `v.routeReleases` has no entry for `r`. | `internal/sim/resource_owner.go` `(resourceOwner).isPod` |
| `claimOccupied` | `r` is in `v.footprint` at the current distance, or is a resource of the berth where `v` is, or of its origin berth before `originReleased`. | `internal/sim/state_physical.go` `(*vehicle).footprint`, `internal/sim/traffic.go` `(*Simulation).releaseVehicleResources` |
| `claimStopping` | `r` is in the reserved span `[0, reservedThrough]`. | `internal/sim/redistribution.go` `(*Simulation).relocationDestinationAdmitted` (at 482d93d), `internal/sim/station_buffer_claim.go` `(*Simulation).bufferClaimCanYield` |
| `claimRetained` | `v.routeReleases[r] > v.distance`. | `internal/sim/station_buffer_claim.go` `(*Simulation).bufferClaimCanYield` |
| `claimLent` | A follower of `v` retains `r` past its distance. | `internal/sim/station_buffer_claim.go` `(*Simulation).bufferClaimCanYield` |
| `claimService` | `r` is a resource of `v.destination`, `v.RelocatingTo != ""`, `v` is empty and does not carry passengers, and `v` is not at that berth. | `internal/sim/station_buffer_claim.go` `(*Simulation).bufferClaimCanYield` |
| `claimOther` | Anything else. | |

The rule sources that name `bufferClaimCanYield` are for `069bafb`; that function went with the station buffers on October 7, 2026.
The kind `claimCommitted`, for coupling and approach commitments, went with physical coupling on October 6, 2026.

Occupied berths, stopping grants, and borrowed track are never revocable.
Borrowed track is owned by a pod ahead, so the borrower never holds it as its own claim, and its owner sees it as `claimLent`.

### 6.2 Callers

| Caller | Today | With the classification |
| --- | --- | --- |
| `yieldRelocationClaims` (`internal/sim/redistribution.go` `(*Simulation).yieldRelocationClaims`) | Releases both destination claims when a passenger arrival conflicts, one claim is held, and the destination is not admitted. | Skips a withdrawn relocating pod. The conflict test uses `passengerArrivals`, which no longer counts withdrawn pods (`(*Simulation).passengerArrivals`). Releases each destination claim for which `revocable` is true. |
| `setOperationalDestination` (section 9.3) | New. | Releases each old destination claim of `v` itself for which `revocable` is true, as `redirect` does today. |

Pickup release (section 5) releases no claim, so it is not a caller.

Decision 2 means that emergency demand never authorizes a yield of another pod's claim.
One existing path gives that authority to passenger demand, and it excludes withdrawn pods:

- A withdrawn pod with riders is not a passenger arrival (`internal/sim/redistribution.go` `(*Simulation).passengerArrivals`), so it cannot make a relocating pod release its destination claims in `(*Simulation).yieldRelocationClaims`.
  The waiting-trip arrivals in `(*Simulation).passengerArrivals` cannot name a withdrawn pod, by W2.

Physical berth accounting does not change.
A withdrawn pod keeps its grants and owners, and admission treats its requests as today.
It wins a berth only when the berth is free.

### 6.3 Baseline fix

This section defined a baseline fix.
The `claimCommitted` test closed the gap of emergency review round 1, finding 4: an empty coupled member or an approach member could pass the yield predicates of `bufferClaimCanYield` and `yieldRelocationClaims`.
Commit 66b0c65 added the coupling ID test to both callers, and commit 16eab09 landed the rest of the fix, including the approach-member case.
Physical coupling was removed on October 6, 2026, and station buffers on October 7, 2026, so the test and its first caller are gone.

## 7. Order continuation

### 7.1 Fields and presence

`Request` (`internal/sim/simulation.go` `Request`) and `SavedRequest` (`internal/sim/state.go` `SavedRequest`) each get `LegFrom string`, at the same position.
The two types convert into each other (`internal/sim/state.go` `restoreState`, `(*Simulation).exportPod`; `internal/session/express_text.go` `encodePackedRequest`), so their field sets must stay equal.

```go
// legOrigin returns the station where the party boards its current pod.
func (r Request) legOrigin() string { return cmp.Or(r.LegFrom, r.From) }

// dispatchOptions is options with the leg origin as From. Dispatch caches
// and pickup tests use it. Identity checks keep options.
func (r Request) dispatchOptions() TripOptions
```

Presence is separate from any index:

- Native: `LegFrom == ""` means absent.
  Station IDs are never empty (`internal/sim/order_options.go` `NormalizeTripOptionsWithOrderContract`), so the empty string cannot name a station.
- Save: the member `legFrom` is a station index.
  Absence is the absence of the member.
  Index 0 is a valid present value and is always written when present.
- Stream and HTTP state: the member `legFrom` is a station ID, omitted when absent.

Rules:

- `From` and `To` never change after acceptance.
- Only a transfer sets `LegFrom` (section 7.3).
  A transfer sets it also when it equals `From`.
- `LegFrom != To`.
- `LegFrom` names a passenger station.
- Once set, `LegFrom` does not change until the next transfer of the same order, and it is never cleared.

### 7.2 Completed history

A completed rider keeps `LegFrom`, and the save writes it for completed riders too.
No reader infers a leg origin.

The alternatives were rejected:

- Dropping `LegFrom` at completion breaks a reader.
  The boarding tuple decoder resolves each berth index against the origin station of that rider, completed or not (`internal/session/boarding_state.go` `(boardingSource).encodePodContract`, `(*stateFile).resolveBoardings`).
  Each rider has a boarding record, so no origin rule exists for a pod without boarding records.
- Inferring the leg origin from the journey origin is lossy.
  `JourneyOrigin` is saved only while passengers remain (`internal/sim/state.go` `(*Simulation).exportPod`), so an idle pod can keep completed riders from one station and a journey origin at another station.
  Emergency review round 2, finding K, reported this.

An ordinary multi-stop journey never sets `LegFrom`, so its history saves and restores with the bytes of today.

### 7.3 Transfer

A transfer is an internal step of a composite operation (section 9.5).
It is not callable alone, because the pod is not valid between steps.

```go
// transferRider moves the active rider at index from v to the queue with
// leg origin station.
func (s *Simulation) transferRider(v *vehicle, index int, station string) error

// continuationFeasible reports whether some fleet pod fits request with
// leg origin station. Policies use it. Stage 1 operations do not.
func (s *Simulation) continuationFeasible(request Request, station string) bool
```

Preconditions of `transferRider`:

- The rider at `index` is active.
- `station` is a passenger station, and `station != rider.To`.

The foundation does not require a feasible continuation, and it never interrupts a party as a fallback.
The binding decision says that each other party is dispatched again and keeps its order.
A transfer that has no feasible continuation makes a stranded order (section 7.6).

`continuationFeasible` returns `hasFittingPod` (`internal/sim/trip_admission.go` `(*Simulation).hasFittingPod`) for the request with `LegFrom = station`.
A later policy may use it as a precondition when it selects an unloading station.
Station selection is policy work for stage 3.

Effect:

1. Remove the rider from `Riders` and its aligned entry from `Boardings`.
2. Build the trip with `requeuedTrip` (`internal/sim/state_physical.go` `requeuedTrip`): `boarded` is true, `PodID` and `DispatchReason` are empty.
3. Set `request.LegFrom = station`.
4. Insert the trip before the first waiting trip with a larger order ID, as `restoreWaiting` does (`internal/sim/state_physical.go` `(*physicalRestore).restoreWaiting`).
5. Set no exclusion.

A transferred trip bypasses `QueueLimit` (`internal/session/session.go` `QueueLimit`, `(*Session).apply`), as a restore requeue does.
The saved queue bound still holds.
A new order enters only while fewer than `QueueLimit` orders wait, and a transfer moves an order from a pod to the queue.
So waiting plus aboard stays at most `QueueLimit + MaxSharedRideParties * maxSavedPods` (`internal/session/state_file.go` `maxSavedTrips`), and the Express outstanding count does not change.

### 7.4 Reader inventory

The inventory covers every non-test reader of an order origin in `internal`, `cmd`, and `web` at `069bafb`.
A search of `web` found no reader of an order origin; `web/editor.js` reads only lane endpoints.
Lane, corridor, offer, flow, service, safety-location, and speed-reduction `From` fields are not order origins.
`cmd/compare` reads only offer and flow origins (`cmd/compare/rail.go` `railDemandSchedule`, `railServiceSchedule`; `cmd/compare/main.go` `weightedProfileFlows`, `demandWeights`).

Class "Identity" keeps `From`.
Class "Physical" uses `legOrigin()`.
Class "Both" checks `From` and `LegFrom`.

| Reader | Use | Class |
| --- | --- | --- |
| `internal/sim/dispatch.go` `(*Simulation).dispatch` | Idle-pod check before promotion | Physical |
| `internal/sim/dispatch.go` `(*Simulation).dispatch` | Local pickup test | Physical |
| `internal/sim/dispatch.go` `(*Simulation).dispatch` | `optionPickups` cache key | Physical: `dispatchOptions()` plus the exclusion (section 5.3) |
| `internal/sim/dispatch.go` `(*Simulation).dispatch` | Pod away from pickup | Physical |
| `internal/sim/dispatch.go` `(*Simulation).dispatch` | Board now | Physical |
| `internal/sim/dispatch.go` `(*Simulation).localPickupForRequest` | `localPickupForRequest` | Physical |
| `internal/sim/dispatch.go` `(*Simulation).pickupPodForRequest` | `pickupPodForRequest` | Physical |
| `internal/sim/dispatch.go` `(*Simulation).board` | `board` origin berth | Physical |
| `internal/sim/dispatch.go` `(*Simulation).joinSharedRide` | `joinSharedRide` boarding pods by station | Physical |
| `internal/sim/dispatch.go` `(*Simulation).promoteReadyPickup` | `promoteReadyPickup` | Physical |
| `internal/sim/trip_admission.go` `(*Simulation).SetExpressServices` | `SetExpressServices` service check | Identity |
| `internal/sim/trip_admission.go` `(*Simulation).validateTripOptions` | `validateTripOptions` for a new order | Identity |
| `internal/sim/trip_admission.go` `requestFromOptions` | `requestFromOptions` writes `From` | Identity |
| `internal/sim/trip_admission.go` `(Request).options` | `options()` | Identity |
| `internal/sim/trip_admission.go` `(*Simulation).podFitsRequest` | `serviceMatches` in `podFitsRequest` | Identity |
| `internal/sim/trip_admission.go` `(*Simulation).podFitsRequest` | Party admission in `podFitsRequest` | Identity |
| `internal/sim/trip_admission.go` `(*Simulation).podFitsRequest` | Connectivity in `podFitsRequest` | Physical |
| `internal/sim/trip_admission.go` `(*Simulation).pickupBerthFitsRequest` | `pickupBerthFitsRequest` | Physical |
| `internal/sim/trip_admission.go` `(*Simulation).assignedPickupFitsRequest` | `assignedPickupFitsRequest` | Physical |
| `internal/sim/trip_admission.go` `(*Simulation).canJoin` | `canJoin` party facts | Identity |
| `internal/sim/order_options.go` `NormalizeTripOptionsWithOrderContract` | Option normalization | Identity |
| `internal/sim/order_options.go` `checkActiveParty` | Express pair of co-riders | Identity |
| `internal/sim/order_validation.go` `validSavedOptionsWithOrderContract` | UTF-8 of order text | Both: also `LegFrom` |
| `internal/sim/order_validation.go` `checkSavedAdmissionWithOrderContract` | Saved party admission | Identity |
| `internal/sim/order_contract.go` `checkSavedServiceLimits` | Express pod party admission | Identity |
| `internal/sim/order_contract_restore.go` `checkContractRestoreSemantics` | Restore path fit and its `contractRouteKey` | Physical: key and stations use `legOrigin()` |
| `internal/sim/order_contract_restore.go` `checkContractRestoreSemantics` | Option normalization | Identity |
| `internal/sim/order_contract_restore.go` `checkContractRestoreSemantics` | Rider path fit | Physical |
| `internal/sim/state.go` `restoreState` | Restore service check | Identity |
| `internal/sim/state.go` `restoreState`, `(*Simulation).exportPod` | Request and saved request conversion | Both: field sets stay equal |
| `internal/sim/state_contract.go` `(SavedState).validTrip` | `validTrip`: `From != To` | Both: also `LegFrom != To` |
| `internal/sim/state_contract.go` `(SavedState).checkPodRiders` | Rider validity: `From != To` | Both: also `LegFrom != To` |
| `internal/sim/state_contract.go` `(SavedState).checkPodRiders` | `boardsHere` | Physical |
| `internal/sim/state_logical.go` `(*Simulation).queueTrips` | Passenger stations of a queued trip | Both |
| `internal/sim/state_physical.go` `(*physicalRestore).decodePod` | Journey origin against `boardingStation` | Physical |
| `internal/sim/state_physical.go` `(*physicalRestore).passengerRiders` | Passenger stations of a rider | Both |
| `internal/sim/state_physical.go` `(*physicalRestore).restoreWaiting` | Passenger stations of a queued trip | Both |
| `internal/sim/state_boarding.go` `checkBoardingBerths` | `checkBoardingBerths` | Physical |
| `internal/sim/riders.go` `(*vehicle).boardingStation` | `boardingStation` | Physical |
| `internal/sim/boarding_records.go` `(*Simulation).riderOrigin` | `riderOrigin` | Physical |
| `internal/sim/onboard_pickups.go` `(*Simulation).onboardPickupReady` | `onboardPickupReady` | Physical |
| `internal/sim/onboard_pickups.go` `(*Simulation).pickupBoardingRecords` | Boarding record creation | Physical |
| `internal/sim/positioning.go` `(*Simulation).guardedSupply` | Guarded supply by waiting trip | Physical |
| `internal/sim/pickup_estimate.go` `(*Simulation).waitForFinishingPod` | `waitForFinishingPod` | Physical |
| `internal/sim/pickup_estimate.go` `(*Simulation).keepHold` | `keepHold` | Physical |
| `internal/sim/pickup_reassignment.go` `(*Simulation).checkPickupPair` | Same-origin pair test | Physical |
| `internal/sim/pickup_reassignment.go` `(*Simulation).tryPickupTransfer` | Transfer move to the pickup | Physical |
| `internal/sim/pickup_swaps.go` `(*Simulation).swapEligible` | `swapEligible` | Physical |
| `internal/sim/pickup_swaps.go` `(*Simulation).tryPickupSwap` | Swap redirection | Physical |
| `internal/sim/diversion.go` `(*Simulation).sendPickupForRequest` | `sendPickupForRequest` | Physical |
| `internal/sim/berth_continuation.go` `(*Simulation).berthFilterForVehicle` | Pickup berth filter | Physical |
| `internal/sim/berth_continuation.go` `(*Simulation).candidateRouteForRequest` | `candidateRouteForRequest` | Physical |
| `internal/sim/simulation.go` `(*Simulation).RequestJourneyOptions` | `RequestJourneyOptions` new order at the pod station | Identity |
| `internal/session/express_orders.go` `orderOptions` | `orderOptions` | Identity |
| `internal/session/express_orders.go` `(*StreamAssembler).expressService` | Express service pair | Identity |
| `internal/session/express_orders.go` `(*StreamAssembler).passengerPath` | Stream passenger path and its `passengerPathKey` | Physical: the key `from` is `legOrigin()` |
| `internal/session/express_orders.go` `(*StreamAssembler).findPassengerPath` | `findPassengerPath` | Physical |
| `internal/session/express_text.go` `encodePackedRequest`, `encodePackedSavedRequest` (at 482d93d) | Packed text encode, stream and save | Both: `LegFrom` joins the stream field list. The save writes an index (section 11.5). |
| `internal/session/express_text.go` `decodePackedRequest`, `decodePackedSavedRequest` (at 482d93d) | Packed text decode | Both: same |
| `internal/session/express_text.go` `scanPackedOrders` | `scanPackedOrders` member list | Both: add `legFrom` for stream envelopes |
| `internal/session/boarding_state.go` `(boardingSource).encodePodContract` | Boarding tuple encode: berth index per station | Physical |
| `internal/session/boarding_state.go` `(*stateFile).resolveBoardings` | Boarding tuple decode | Physical |
| `internal/session/stream_boardings.go` `(*StreamAssembler).vehicleBoardings` | Stream boarding berth station | Physical |
| `internal/session/stream_frame.go` `(*StreamAssembler).references` | Stream station references | Both |
| `internal/rail/connections.go` `RestoreConnections` | Saved record offer check | Identity |
| `internal/rail/connections.go` `validateRecord` | Record against its request | Identity |
| `internal/parkride/checkpoint_restore.go` `validateConservation` | Ledger request identity | Identity |
| `internal/parkride/run.go` `preflight` | Options of new itinerary orders | Identity |
| `internal/parkride/ledger.go` `(*ledger).offer` | Options of a return order | Identity |
| `internal/view/orders.go` `(*Game).orderLabels` | Order row origin | Both: `From`, plus "Transfer at" with `LegFrom` |
| `internal/view/game.go` `(*Game).visibleCollapsedStationLabels` (at 482d93d) | Label preference for rider stations | Physical |
| `internal/view/game.go` `journeyStations` (at 482d93d) | `journeyStations` | Physical |

Each rider has a boarding record, so the inventory has no row for an origin rule without boarding records or for `legacyBoardingRecords`, and `checkBoardingBerths` runs before either tier for each pod with riders.
The rows for the coupling cabin facts and the buffered pickup test went with physical coupling and the station buffers.

Identity keys:

- `pass.optionPickups` (`internal/sim/dispatch.go` `(*Simulation).dispatch`): physical, with the exclusion.
- `passengerPathKey` (`internal/session/express_orders.go` `passengerPathKey`): physical.
- `contractRouteKey` (`internal/sim/order_contract_restore.go` `contractRouteKey`, `checkContractRestoreSemantics`): physical.
- Express co-rider pair (`internal/sim/order_options.go` `checkActiveParty`) and service registry pair (`internal/sim/trip_admission.go` `serviceMatches`): identity.
- Rail record match (`internal/rail/connections.go` `validateRecord`) and park-and-ride binding (`internal/parkride/checkpoint_restore.go` `validateConservation`): identity.

### 7.5 Codec directions

| Direction | Rule |
| --- | --- |
| Save encode | The session adapter writes `legFrom` as the index of `LegFrom` in the saved project `network.stations`, as it writes boarding berth indexes (`internal/session/boarding_state.go` `(boardingSource).encodePodContract`). |
| Save decode | The adapter maps the index back to the station ID before `RestoreState`. `RestoreState` never receives an index. Out of range is `invalid_state`. |
| Stream and HTTP encode | `legFrom` is the station ID. It is packed with the other order text (`encodePackedRequest`, `internal/session/express_text.go`). |
| Stream decode | Unpacked with the other text (`decodePackedRequest`). The validator requires a passenger station (`internal/session/stream_frame.go` `(*StreamAssembler).references`). |
| Boarding tuples | Both directions resolve the berth index against `legOrigin()` of the rider. |

### 7.6 Stranded transferred orders

A waiting trip is stranded when `request.LegFrom != ""` and no fleet class that admits the party has a certified passenger path from `LegFrom` to `To`.
A class admits the party when the profile, party size, and Express-class tests of `internal/sim/order_contract_restore.go` `checkContractRestoreSemantics` pass for the original `From` and `To`.
Under the plain contract, project validation connects each pair of passenger stations (`internal/sim/riders.go` `(*Simulation).continueJourney`), so only the Express contract can make a stranded order.

Live behavior needs no change.
`podFitsRequest` checks connectivity from the leg origin (`internal/sim/trip_admission.go` `(*Simulation).podFitsRequest`, changed by section 7.4), so no pod fits.
Dispatch binds no pod and starts no hold, and the trip shows the reason "Waiting for a certified vehicle that fits this party and route" (`internal/sim/dispatch.go` `(*Simulation).dispatch`).
The trip waits until a project apply changes the fleet or the network through the existing rebuild path.
Stage 1 adds no timeout.

A stranded trip has this exact shape:

- S1: `boarded` is true, `request.LegFrom` is present, and `LegFrom` and `To` are passenger stations of the network.
  A never-boarded request cannot use the exception, also when it has `LegFrom`.
- S2: `request.PodID == ""`, `route` is empty, `destination` is empty, `deferPodID == ""`, `deferCheck == 0`, and `excludedPod == ""`.
- S3: the original service identity is valid.
  `From` and `To` are passenger stations, and some fleet class admits the party.
  When `Service == ExpressServiceChoice`, the Express service pair on `From` and `To` exists (`internal/sim/trip_admission.go` `serviceMatches`).
  An on-demand order under the Express contract needs no registry pair, as today (`internal/session/express_orders.go` `(*StreamAssembler).expressService`).

Validator exception, native restore (`internal/sim/order_contract_restore.go` `checkContractRestoreSemantics`):

1. Split `fits` (`checkContractRestoreSemantics`) into `admits(class, options)`, which keeps the profile, party, and Express-class tests of `fits`, and `pathFits(class, from, to)`, which keeps the cached connectivity test of `fits` with `from = legOrigin()`.
2. For each waiting trip, require `admits` for some fleet class, as today.
3. When some admitting class also has `pathFits`, accept, as today.
4. Otherwise accept only when the trip meets S1, S2, and S3.
   Else return the existing error in `checkContractRestoreSemantics`.
5. The assigned-route check in `checkContractRestoreSemantics` does not change.
   A stranded trip has no route, so it never reaches that check.
6. The rider check in `checkContractRestoreSemantics` does not change.
   A rider never uses the exception, because it boarded a pod that fits.

Validator exception, stream (`internal/session/express_orders.go` `(*StreamAssembler).expressOrders`):

1. The compatibility loop in `(*StreamAssembler).expressOrders` keeps both tests for a normal pending order.
2. When no vehicle passes both tests, accept only when `r.LegFrom != ""`, `r.PodID == ""`, `a.stations[r.LegFrom]` and `a.stations[r.To]` are true, `a.expressService(r)` succeeds, which checks the registry pair only for the Express service choice, and some vehicle passes the party admission test of `(*StreamAssembler).expressOrders` alone.
   Else return the existing error in `(*StreamAssembler).expressOrders`.
3. Active riders in `(*StreamAssembler).expressOrders` do not change.

No other validator gets an exception.
A trip with `LegFrom` that has a pod, a route, a deferral, or an exclusion, and no path, is still rejected.

## 8. Interrupted outcome

### 8.1 State

`Simulation` gets three fields:

- `interrupted int`: orders that ended interrupted.
- `interruptedPassengers int`: the sum of their party sizes.
- `undelivered []int`: order IDs interrupted since the last drain.

```go
// DrainInterruptions returns the order IDs interrupted since the last call
// and clears them.
func (s *Simulation) DrainInterruptions() []int
```

`Step` does not clear `undelivered`, so an interruption is never lost between ticks.
Section 8.5 defines who drains it.
`Clone` copies it (`internal/sim/clone.go` `(*Simulation).Clone` is the model).
`Reset` clears all three (`internal/sim/simulation.go` `(*Simulation).Reset`).
`undelivered` is not saved.
`ExportState` (`internal/sim/state.go` `(*Simulation).ExportState`) does not change.
The session save and publication paths call `deliverInterruptions` before they read the state.
Under D1 (section 8.5) that call has nothing to drain, so a save can never hold an interruption that rail has not seen.
`CheckContract` does not check `undelivered`, because `observe` runs inside the operation, before the session drains.

### 8.2 Operations

```go
// interruptRider ends the order of the active rider at index as
// interrupted. Internal step of a composite operation.
func (s *Simulation) interruptRider(v *vehicle, index int)

// evacuate interrupts every active rider of a stopped pod with a fault
// hold. It completes no rider.
func (s *Simulation) evacuate(v *vehicle) error
```

`interruptRider` effect:

1. Remove the rider from `Riders` and its aligned entry from `Boardings`.
2. Add 1 to `interrupted` and the party size to `interruptedPassengers`.
3. Append the order ID to `undelivered`.
4. Write no `StepCompletion`.
   Add nothing to `completed`, `journeys`, the journey totals, the rider distance, the direct distance, the detour ratio, or `requestCompletions`.
5. `interruptRider` does not change `riddenBase`.
   The composite operation freezes the cumulative distance (below).

`evacuate` is the fault path.
The binding decision says that riders leave the stopped pod and their trips end as interrupted.
This applies to every active rider, also a rider whose destination is the station of the berth.

`evacuate` preconditions:

- `v.withdrawn&faultHold != 0` and `v.Pod.Speed == 0`.
- `v` is not a platoon member.
- At a berth: activity `Idle`, `Boarding`, `Continuing`, or `Unloading`.
- On a lane: activity `Traveling`, and `v` carries passengers.
  An empty withdrawn pod uses `setOperationalDestination` with purpose 3 instead.

`evacuate` effect, in one call:

1. `ridden := v.riddenMeters()` (`internal/sim/riders.go` `(*vehicle).riddenMeters`), before any outcome.
2. Call `interruptRider` for each active rider.
3. Freeze the distance (below).
4. At a berth, call `settleIdleAtBerth(v)` (below).
5. On a lane, start empty recovery (section 9.5, "Lane evacuation").

Distance freeze.
Each composite that ends riders captures `ridden` once, before its first outcome, as `alight` does (`internal/sim/riders.go` `(*Simulation).alight`).
After the last outcome, when `Boardings` is not empty, it sets `v.riddenBase = ridden`.
Each pod with riders has boarding records.
When the outcomes interrupt or transfer every rider and no completed history is aboard, no record remains, and the composite does not set `riddenBase` (`internal/sim/operational.go` `finishOperationalUnload` and `evacuate`); then no retained baseline needs the bound.
When completed rider history and its records remain after the last outcome, `Boardings` is not empty, and the composite sets `riddenBase`.
This applies whatever the outcome of the last rider is: completion, interruption, or transfer.
Once no rider is aboard, `riddenMeters` returns `riddenBase` alone (`(*vehicle).riddenMeters`), so `settleIdleAtBerth` can zero `distance` and each retained boarding baseline stays at or below `RiddenMeters`.
The save validator needs that bound (`internal/sim/state_boarding.go` `checkSavedBoardingsWithOrderContract`).
The stream does not send `riddenMeters`, so the stream validator (`internal/session/stream_boardings.go` `validateVehicleBoardingsContract`) does not check it.
`completeRider(v, index, ridden)` takes the captured value, so completions use the same distance as `alight`.

`settleIdleAtBerth(v)` gives the complete idle state at the current berth.
It uses the field set of `placeDemoted` (`internal/sim/state_physical.go` `(*physicalRestore).placeDemoted`) and `moveTo`, but it keeps owners:

| Field | Value |
| --- | --- |
| `Pod` | `ID`, `Class`, berth `Position`, `Activity` `Idle`, `StationID`, `BerthID`, `StationPhase` `AtBerth`, `ManeuverStationID`, as `arrive` builds it (`internal/sim/simulation.go` `(*Simulation).arrive`) with `Occupied` false. Speed, wait reason, and blocker are zero. |
| `phaseTicks`, `blockIndex`, `distance` | 0. The idle rule needs phase 0 (`internal/sim/state_contract.go` `phaseRule`, `checkPodFlags`). |
| `reservedThrough`, `pending` | -1. |
| `Stops` | nil. |
| `op` | Cleared, also when `v` held at a refuge or unloaded for an emergency. The holds stay, and W5 holds trivially. |
| `RelocatingTo`, `Rebalancing`, `released` | Empty, false, false. |
| `origin`, `destination`, `destinationStation` | Empty, the current berth, and the current station. |
| Route | `replaceRoute(nil)` and empty blocks, as `placeDemoted` does (`(*physicalRestore).placeDemoted`). |
| `Riders`, `Boardings` | Completed history stays, with aligned records. When no rider remains, both are nil. |
| Owners and `routeReleases` | Unchanged. The berth stays owned. Other grants go at the release boundary through the path for a pod that is not traveling (`internal/sim/traffic.go` `(*Simulation).releaseVehicleResources`). That path walks `routeReleases` (`(*Simulation).releaseRouteResourcesExcept`), not the cleared blocks. |

`withdrawn` is unchanged.
The pod stays out of service until the policy calls `restoreService`.

### 8.3 Conservation

The order balance becomes:

```text
RequestID = Completed + Interrupted + queued + aboard + unaccounted
```

| Place | Change |
| --- | --- |
| `checkContract` (`internal/sim/state_contract.go` `(SavedState).checkContract`) | `unaccounted = RequestID - Completed - Interrupted - held`. |
| `validateCounters` (`internal/sim/state_physical.go` `(SavedState).validateCounters`) | `Interrupted >= 0`, `InterruptedPassengers >= Interrupted`, `Completed + Interrupted <= RequestID`. |
| `reconcileOrders` (`internal/sim/state_contract.go` `(*Simulation).reconcileOrders`) | New place "interrupted" for `RestoreResult.Interrupted`. The count check adds `s.interrupted == state.Interrupted + len(interrupted)`. |
| `verifyRestore` (`internal/sim/state_physical.go` `(*Simulation).verifyRestore`) | Same counts. |
| `setSavedCounters` and `restoreCounters` (`internal/sim/state_physical.go` `(*physicalRestore).restoreCounters`) | Copy both counters. |

`RestoreResult` (`internal/sim/state.go` `RestoreResult`) gets `Interrupted []int`.

### 8.4 Metrics

- `Snapshot` (`internal/sim/simulation.go` `Snapshot`) gets `interrupted` and `interruptedPassengers`, each omitted at zero.
- Wait: unchanged.
  An interrupted order already recorded its boarding.
- Journey, rider distance, direct distance, and detour: the order is excluded.
- Passenger and empty pod distance: unchanged, because they measure the pod (`internal/sim/redistribution.go` `(*Simulation).moveAndMeasure`).
- Telemetry: `podsim.orders.interrupted` beside the pending gauge (`internal/telemetry/telemetry.go` `registerSessionMetrics`), through `Session.Metrics` (`internal/session/session.go` `(*Session).Metrics`).
- Any stream check that balances submitted orders adds `interrupted`.

### 8.5 Delivery and rail records

Terminal outcomes reach rail atomically under the session lock.
Every simulation step and every command already runs with `s.mu` held (`internal/session/speed.go` `(*Session).liveBatch`, `(*Session).step`; `internal/session/session.go` `(*Session).applyCommand`).
State reads for publication and saves also take `s.mu` (`internal/session/session.go` `(*Session).State`).
The session drains `undelivered` at two places, before it releases the lock:

1. In `Session.step` (`internal/session/speed.go` `(*Session).step`), immediately after `Simulation.Step` and before any other statement.
   Delivery thus runs before `demand.step` and its `Connections.Advance` (`internal/session/demand.go` `(*demandRun).step`).
   The order matters: `Advance` marks an unresolved record `missed` when its departure deadline passes (`internal/rail/connections.go` `(*Connections).Advance`), and `Interrupt` leaves a `missed` record unchanged.
   An interruption on the departure tick must reach rail first, so the record ends `unserved` with reason `interrupted`.
2. At the end of `Session.apply` (`internal/session/session.go` `(*Session).apply`), for every command, before `applyCommand` returns.
   Thus the save before the reply (`(*Session).Apply`) and the next publication see the delivered state.

```go
// deliverInterruptions sends the drained interruptions to the consumers.
// The caller holds mu.
func (s *Session) deliverInterruptions()
```

`deliverInterruptions` calls `Connections.Interrupt`, then refreshes `demand.state.Connections` from `Counts()`, as `demand.step` does (`internal/session/demand.go` `(*demandRun).step`).

Invariant D1: when `s.mu` is released, `undelivered` is empty, and rail has seen each interruption.
As a second guard, the save path (`internal/session/persist.go` `(*Session).SaveState`) and the state read for publication (`internal/session/session.go` `(*Session).advance`) call `deliverInterruptions` before they read the simulation.
Under D1 those calls drain nothing.
A save, a publication, an HTTP state read, and a checkpoint therefore never see an order that is gone from the simulation while its rail record is pending.
`cmd/compare` has no lock and no commands.
It drains after each `Step`, beside its `Advance` call (`cmd/compare/main.go` `run`).

`Connections` gets a new method.
`Advance` (`internal/rail/connections.go` `(*Connections).Advance`) does not change, so the off-state completion path does not change.

```go
// Interrupt ends the records of interrupted orders.
func (c *Connections) Interrupt(tick int64, orders []int)
```

| Record state | Effect |
| --- | --- |
| `pending` | Outcome `unserved`, reason `interrupted`. `unresolved` decreases by one and `unserved` increases by one. `alightedTick` stays -1. The deadline entry is skipped later, because the outcome is not `pending` (`(*Connections).Advance`). |
| `missed` | No change. |
| `made` | Not possible: a made record needs an alighting. |

`validateRecord` accepts `unserved` with reason `interrupted` for a positive request ID, with no active binding.
`ReconcileRestore` gives the same outcome for each ID in `RestoreResult.Interrupted`.

### 8.6 Park-and-ride

The ledger outcome `interrupted` is defined now and lands with the first policy that can interrupt a park-and-ride party:

- The ledger consumes the interruptions that `deliverInterruptions` drains, in `advance` (`internal/parkride/ledger.go` `(*ledger).advance`).
- An interrupted outward or return order ends the itinerary through `end` with outcome `interrupted`.
- The car stays held, with the rule of `stranded` (`(*ledger).offer`; `internal/parkride/checkpoint_restore.go` `validateConservation`).
- `validateConservation` accepts the outcome (`internal/parkride/checkpoint_restore.go` `validateConservation`).

Stage 1 has no policy, so no park-and-ride party can be interrupted.
A car run refuses a project with a fault rate or an emergency rate, with `car runs do not support a fault or emergency rate`, so no later stage interrupts one either.

### 8.7 Restore

| Case | Rule |
| --- | --- |
| Checkpoint and rewind | `Clone` keeps the counters. A checkpoint is a command (`internal/session/session.go` `(*Session).apply`, `internal/session/checkpoint.go` `(*Session).captureCheckpoint`). By D1 `undelivered` is empty when that command starts, and the command interrupts no order. |
| Physical tier | Keeps the counters. Restore-time interruption is defined per purpose in section 9.6. |
| Logical tier | Keeps the counters and adds the restore-time interruptions of section 9.6. |
| Restore result | `Interrupted` lists the restore-time interruptions in order ID order. |

## 9. Operational destination

### 9.1 Purposes

| Purpose | Code | Who sets it | Arrival action |
| --- | --- | --- | --- |
| Service | 0, absent | Default | Existing `arrive` and unloading. |
| Emergency unloading | 1 | Stage 3 | Unload every active rider at the berth. A marked rider is interrupted. An unmarked rider at its destination completes. Each other rider is transferred. Then idle. The holds stay. |
| Refuge travel | 2 | Stage 2 | Hold at the berth with the riders aboard. No rider leaves. Resume on request. |
| Empty recovery | 3 | Stages 2 and 3, and lane evacuation | Become idle at the berth. The holds stay. |

### 9.2 State

```go
type opPurpose uint8

const (
	opService opPurpose = iota
	opEmergencyUnload
	opRefuge
	opEmptyRecovery
)

// operationalDestination is the purpose of the physical destination.
// destination and destinationStation of the pod hold the place.
type operationalDestination struct {
	purpose opPurpose
	// owner is the hold that owns the purpose. Each purpose other than
	// service needs one held bit (invariant W5).
	owner serviceHold
	// interrupt marks, by index in Riders, the active riders whose orders
	// end interrupted at an emergency unload. Bit i is rider i.
	interrupt uint32
}
```

`vehicle` gets `op operationalDestination`.
`interrupt` fits every class, because a pod stores at most 20 riders (`orderBounds`, `internal/session/format_limits.go` `(contractMarkers).orderBounds` at `475cc85`).
The rider order is fixed while a purpose is set, because only the arrival action and `evacuate` remove riders, and both clear the purpose.

### 9.3 Operations

```go
// setOperationalDestination gives a traveling pod a new physical
// destination berth with a purpose. It keeps the divertStart prefix.
func (s *Simulation) setOperationalDestination(v *vehicle, to operationalTarget) error

// startOperationalUnload starts an emergency unload at the berth where v is.
func (s *Simulation) startOperationalUnload(v *vehicle, owner serviceHold, interrupt uint32) error

// resumeFromRefuge ends a refuge hold and continues to the next stop.
func (s *Simulation) resumeFromRefuge(v *vehicle) error
```

`operationalTarget` holds the purpose, the owner, the interrupt set, the station, and the berth.

`setOperationalDestination` preconditions, checked before any change:

- `v.Pod.Activity` is `Traveling` or `DepartingEmpty`.
- `divertStart(v)` (`internal/sim/diversion.go` `(*Simulation).divertStart`) reports a prefix.
  This refuses platoon members and pods inside the arrival chain.
- `owner` is a single bit held by `v`, for every purpose.
- Purpose 1 or 2: `v` carries passengers.
  Purpose 3: `v` is empty.
- Purpose 1: the station is a passenger station, and `interrupt` names only active riders.
- Purpose 2: the station is not in `v.Stops`.
  It can be a parking station.
- The berth allows the class of `v`, and `assignedRoute(v, from, berth.Node)` finds a route from the divert node.

`setOperationalDestination` effect, in one call:

1. For each resource of the old destination berth with `revocable(v, r)`, call `releaseOwned` (`internal/sim/traffic.go` `(*Simulation).releaseOwned`).
   This is the release of `redirect` (`internal/sim/diversion.go` `(*Simulation).redirect`), limited to service claims of `v`.
2. Install `v.Route[:prefix]` plus the new suffix with `setVehicleRoute` (`internal/sim/traffic.go` `(*Simulation).setVehicleRoute`).
   The prefix starts at route index 0, so distance, block indexes, and the reserved span keep their meaning.
3. Set `destination`, `destinationStation`, and `op`.
4. Purpose 3: set `RelocatingTo` to the station, and clear `Rebalancing` and `released`.
   Purposes 1 and 2: `RelocatingTo` stays empty, so `arrive` does not take the empty-move branch (`internal/sim/simulation.go` `(*Simulation).arrive`).
5. Set `pending = -1` and clear the wait reason, as `redirect` does (`internal/sim/diversion.go` `(*Simulation).redirect`).

`Stops` does not change.
Each rider keeps its `To`.

`startOperationalUnload` preconditions:

- `v` is at a berth with activity `Boarding`, `Continuing`, or `Unloading`.
- `v` carries passengers, and `v` is not a platoon member.
- The berth is at a passenger station.
- `owner` is a held bit, and `interrupt` names only active riders.

Its effect is the arrival of section 9.5 at the current berth, in one call.
The call sets activity `Unloading`, `Occupied` true, `StationPhase` `AtBerth`, `destination` the current berth, `destinationStation` the current station, and `reservedThrough = -1`.
It removes the current station from `Stops` wherever it is.
`phaseTicks` becomes the unloading phase of the class for the passengers aboard (`unloadingPhase`), which is `unloadingTicks` for a seat-only class, or stays when the pod already unloads with `phaseTicks >= 1`.
Unused grants go at the release boundary (`internal/sim/traffic.go` `(*Simulation).releaseVehicleResources`).
For a pod that starts at a berth, this addresses finding A of emergency review round 2: the stops and the zero interval each have a rule.

`resumeFromRefuge` preconditions: purpose 2, at the refuge berth.
Effect: clear `op`, then run `continueJourney` (`internal/sim/riders.go` `(*Simulation).continueJourney`).
When `continueJourney` finds no route, the call restores `op` and returns an error, so the pod keeps purpose 2.

### 9.4 Stop rules and place rules

`SavedPod` gets the purpose, the owner, and the interrupt set (section 11.5).
`phaseOf` (`internal/sim/state_contract.go` `phaseOf`) and `ruleForPod` read them.
`checkPod` applies the result in live checks and in both restore tiers.

A new stop rule, `serviceStops`, requires `Stops` to hold one stop for each destination of the active riders, without the `Stops[0] == DestinationStation` clause of `routeStops`.

| Saved pod | Phase | Rule |
| --- | --- | --- |
| Traveling, occupied, purpose 1 | `phaseTravelingOccupied` | `serviceStops`. `hasDestination`. `DestinationStation` is a passenger station. |
| Traveling, occupied, purpose 2 | `phaseTravelingOccupied` | `serviceStops`. `hasDestination`. `DestinationStation` is not in `Stops`. |
| Unloading, purpose 1 | New `phaseUnloadingOperational` | Active riders, history, occupied, at berth, `atDestination`. `PhaseTicks` from 1 to the largest unloading phase of the class (`maxUnloadingPhase`), which is `unloadingTicks` for a seat-only class. `laterStops`: the station of the pod is not in `Stops`. Passenger station. |
| Unloading, purpose 2 | New `phaseRefugeHolding` | Active riders, history, occupied, at berth, `atDestination`. `PhaseTicks` is 0. `laterStops`. Passenger or parking station. |
| Traveling or departing, empty, purpose 3 | `phaseTravelingEmpty` or `phaseDepartingEmpty` | The existing relocation rule. `Rebalancing` and `Released` are false. The route ends at the destination berth or at an entry of `DestinationStation`. |
| Idle, boarding, or continuing, any purpose | | Invalid. Arrival and `settleIdleAtBerth` clear the purpose. |
| Purpose 0 | Existing phases | Unchanged. |

For every purpose other than 0, W5 holds.
`interrupt` is 0 unless the purpose is 1, and each set bit names an active rider.

The completed-history rule (`internal/sim/state_contract.go` `checkPodStops`) does not change.
The operational station is not added to `Stops`, so a stop that matches completed history cannot appear.
This closes emergency review round 2, finding A, first case.

Current-berth validation becomes purpose-aware.
`checkBoardingBerths` (`internal/sim/state_boarding.go` `checkBoardingBerths`) rejects a parking berth for any pod with active riders.
The test becomes `rule.active && station.ParkingOnly && phase != phaseRefugeHolding`.
The rider origin test in `checkBoardingBerths` does not change: each recorded rider still needs a passenger station at its leg origin.
Emergency unloading keeps the passenger-station requirement through its phase rule and through the precondition of `startOperationalUnload`.

Refuge holding reuses the activity `unloading`, with purpose 2 and phase 0.
The pod is at a berth with riders aboard, as an intermediate unloading pod that waits for a route is (`internal/sim/state_contract.go` `phaseRules`).
This keeps `savedActivities` and `activityCode` (`internal/sim/state.go` `activityCode`) unchanged.
The pod shows the wait reason "Holding at refuge".
A new activity code is the alternative.
It needs a format change in each family, so this contract does not use it.

### 9.5 Arrival and phase changes

Each phase change happens inside the call that causes it.
No per-tick pass repairs a state after publication.
No transition removes a hold.
Each transition that ends a purpose clears `op` and keeps the holds, so W5 holds at every step.
The policy calls `restoreService` later.
A faulted pod therefore stays out of service after its recovery arrives, until the fault clears.

| Place | Transition |
| --- | --- |
| `arrive` (`internal/sim/simulation.go` `(*Simulation).arrive`), purpose 1 | Activity `Unloading`, `phaseTicks` the unloading phase of the class for the passengers aboard. Remove the station from `Stops` wherever it is. |
| `arrive`, purpose 2 | Activity `Unloading`, `phaseTicks = 0`, wait reason "Holding at refuge". `Stops` unchanged. |
| `arrive`, purpose 3 | The existing empty-move branch (`(*Simulation).arrive`), then clear `op`. |
| Unloading loop (`internal/sim/simulation.go` `(*Simulation).Step`), purpose 1 at phase 0 | `finishOperationalUnload(v)`, in place of `alight` and `continueJourney`. |
| Unloading loop, purpose 2 | Skip. The pod holds. |
| `resumeFromRefuge` | Purpose 2 to service, activity `Continuing`. |
| `evacuate` at a berth | `settleIdleAtBerth` clears `op`. The holds stay. |
| `evacuate` on a lane | Lane evacuation, below. |

`finishOperationalUnload(v)` captures `ridden := v.riddenMeters()` first.
Then it does, in one call, for each active rider in `Riders` order:

1. Bit set in `interrupt`: `interruptRider`.
   The mask wins over `To == StationID`.
2. Otherwise, `To == StationID`: `completeRider(v, index, ridden)`.
3. Otherwise: `transferRider(v, index, StationID)`.
   The transfer always happens.
   A party without a feasible continuation becomes a stranded order (section 7.6).

Then it freezes the distance (section 8.2), and calls `settleIdleAtBerth(v)`, which clears `op`.
The unload runs before `dispatch` (`internal/sim/simulation.go` `(*Simulation).Step`), so dispatch can serve the transferred trips in the same tick.
The indexes refer to the rider order before the loop; the implementation removes riders after it decides each outcome.

Lane evacuation sets `Occupied` false, `Stops` nil, `RelocatingTo = destinationStation`, `Rebalancing` and `released` false, and `op = {purpose: opEmptyRecovery, owner: faultHold}`.
It replaces any earlier purpose.
It keeps the route, distance, block state, and owners.
The pod then continues on one of two route shapes:

| Route shape | Continuation |
| --- | --- |
| Ends at the destination berth | Unchanged. `arrive` takes the empty-move branch. |
| Ends at a station entry | `assignTerminalBerth` (`internal/sim/berth_choice.go` `(*Simulation).assignTerminalBerth`) treats purpose 3 as a passenger route. The `passenger` test adds `v.op.purpose == opEmptyRecovery`. The berth choice then runs as for a passenger pod. |

### 9.6 Restore tiers

| Purpose | Physical, pod keeps its place | Physical, pod demoted (`internal/sim/state_physical.go` `(*physicalRestore).demote`, `(*physicalRestore).placeDemoted`) | Logical (`internal/sim/state_logical.go` `restoreLogical`) |
| --- | --- | --- | --- |
| 1, traveling | Keep `op`. Route must end at the destination berth (`internal/sim/state_physical.go` `(*physicalRestore).routeEndsMatch`). | Interrupt the riders in `interrupt`. Requeue the others (`(*physicalRestore).requeue`). Never `boardAgain`. Clear `op`. | Interrupt the riders in `interrupt`. Requeue the others. Clear `op`. |
| 1, unloading | Keep. | As the traveling row, if a demotion reaches the pod. | A marked rider is interrupted, also when its `To` is the station. Each unmarked rider follows the existing rule (`restoreLogical`). |
| 2, traveling or holding | Keep. A holding pod can be at a parking berth (section 9.4). | Existing demotion. Clear `op`. | Existing rule. A parking berth is not a passenger berth (`internal/sim/state_logical.go` `restoreLogical`), so every active rider is requeued. Clear `op`. |
| 3 | Keep. `routeEndsMatch` (`internal/sim/state_physical.go` `(*physicalRestore).routeEndsMatch`) accepts a berth end and an entry end with `RelocatingTo == DestinationStation`. | Existing demotion. Clear `op`. | Clear `op`. |

In both tiers:

- Holds are kept.
  Each later stage decides whether its own records survive a restore, and releases its hold when they do not.
- `LegFrom` is kept on every rider and every trip.
  A requeued rider with `LegFrom` goes back to its leg origin.
- Exclusions are kept.
- `RestoreResult.Interrupted` lists every restore-time interruption.
  The restore delivers them through `ReconcileRestore`, not through `undelivered`.

## 10. Atomicity

Each operation in section 13 is one call.
It checks every precondition before its first write, and returns an error with no change when one fails.
After it returns, the state contract holds.

The calls happen only at these places:

- At a command boundary, under the session lock.
  The public entry calls `observe` (`internal/sim/state_contract.go` `(*Simulation).observe`) once, after the operation.
  The session then delivers interruptions before it releases the lock (section 8.5).
- Inside `Step`, at the unloading loop (`internal/sim/simulation.go` `(*Simulation).Step`) and inside `arrive`.
- Later stages may add one policy place in `Step`.
  It must be after the unloading loop and before `dispatch` (`(*Simulation).Step`), and not inside a dispatch pass.

Composite operations order their internal steps so that the pod is valid at return:

| Composite | Internal steps |
| --- | --- |
| `withdrawService` | Hold bit, then `releasePickups`. |
| `evacuate`, at a berth | `interruptRider` for every active rider, then `settleIdleAtBerth`. No rider completes. |
| `evacuate`, on a lane | `interruptRider` for every active rider, then lane evacuation with purpose 3. No rider completes. |
| `finishOperationalUnload` | Capture `ridden`, then `interruptRider`, `completeRider`, or `transferRider` per rider, then the distance freeze, then `settleIdleAtBerth`. |
| `setOperationalDestination` | Revocable claim release, route, destination, purpose. |
| Purpose 3 arrival | Empty-move branch, then clear `op`. |
| `rebindOperationalOwner` then `restoreService` | The policy calls both at one command boundary or one `Step` place. |

## 11. Shared format extension

### 11.1 Target formats

This section targets the formats after items 4, 5, and 7.
Members use post-item-5 lowerCamel names.
References in this section to item 7 code are for `475cc85`.
The save is version 9.
Its array limits come from `savedLimits` (`internal/session/format_limits.go` `savedLimits`), which selects the order bounds by marker (`(contractMarkers).orderBounds`).
The stream is hello version 7, with limits from `streamLimits`.
The HTTP state has one envelope and one media type.

### 11.2 Marker and its propagation

This section defined the project marker `incidentContract: "incident-v1"`, its copies in the topology and in the full frame, the agreement check, and the raw presence scan.
On November 4, 2026, [the marker removal contract](marker-removal-contract-proposal.md) removed the marker and each presence rule.
A project, a saved state, a stream envelope, or an HTTP state with an `incidentContract` member now has an unknown member.
`scanIncidentMembers` keeps its refusals of null and empty values, and the rules on the values of the stage 1 members stay (section 11.6).

Delta groups for the stage 1 stream members (`internal/session/stream_codec.go` `frameGroups`):

| Member | Delta group |
| --- | --- |
| `interrupted`, `interruptedPassengers` | New group `incident`. The `global` group does not change, so off-state bytes do not change. |
| Vehicle `withdrawn`, `operational` | The vehicle `metadata` group: `vehicleMetadata` gets both members, omitted at zero. |
| Order `legFrom` | The `pending` group for waiting orders, and the vehicle `riders` group (`VehicleDelta`) for riders. |

### 11.3 Digest extension registry

`digestWriter.value` (`internal/session/receipt.go` `(*digestWriter).value`) hashes every field, including zero fields and the nested project.
A new field would change every digest, also with `omitempty`.

Rule:

- A struct field with the tag `digest:"ext=N"` is an extension field.
  `N` is a positive integer, unique in the whole `Command` type tree, including `project.Config`.
- One table in `receipt.go` lists every `N` with its field path.
  Stage 1 takes `N = 1` for `project.Config.IncidentContract`.
  Each later field takes the next free number.
  The marker removal retired the numbers 1, 2, and 10 with the markers, and they stay retired.
- An extension field may sit only at a fixed path: not inside a slice or a map element.
  A test walks the `Command` type tree and fails on a duplicate `N`, a missing table entry, or a tagged field under a slice or map.
- The main walk skips each extension field and writes nothing for it, not even a nil mark.
  `value` reads the `StructField` from `v.Fields()` (`(*digestWriter).value`) to see the tag.
- The walk collects each extension field that is set, as the pair `(N, value)`, in walk order.
  A field is set when `reflect.Value.IsZero` is false.
  A nil pointer or nil slice is not set.
  A non-nil empty slice is set and encodes as `1, 0` (`(*digestWriter).value`).
- After the complete main walk, at the root only, when at least one pair exists, the writer appends `uvarint(count)` and then each pair as `uvarint(N)` followed by the existing value encoding.
  With no pair, it appends nothing.

Prefix-freeness argument:

1. For a fixed Go type, the main encoding is prefix-free.
   Each scalar is a varint with a known end (`(*digestWriter).uint`).
   Each string and slice writes its length (`(*digestWriter).value`, `digestWriter`).
   Each pointer and slice writes a presence mark (`(*digestWriter).value`).
   Struct fields form a fixed sequence (`(*digestWriter).value`).
   Skipping extension fields keeps the sequence fixed, because the skip depends only on the type.
2. So for two commands `a` and `b`, the input `M(a) T(a)` equals `M(b) T(b)` only when `M(a) = M(b)`, because neither main encoding is a proper prefix of the other.
3. Then `T(a) = T(b)`.
   An empty trailer differs from a nonempty one.
   A nonempty trailer is self-delimiting: a count, then pairs of a varint and a self-delimiting value.
   Equal trailers mean the same set fields, by `N`, with the same values.
   `N` identifies the path, because tagged fields sit only at fixed paths.
4. A command with no set extension field writes exactly the bytes of today, so its digest does not change.

The claim is about equal hash inputs.
It does not claim that SHA-256 is injective.
Nil and empty stay distinct, which agrees with `reflect.DeepEqual` and with the contract in `commandDigest`.

Tests:

- A baseline file pins the digest of one command per action, including `project` commands with the plain and Express example projects.
  Item 7 patch 0 created it (`internal/session/testdata/command_digests.txt`).
  Stage 1 reuses it.
- Each extension field set alone changes the digest.
- Two commands that differ only in which extension field is set have different digests.
- A nil and an empty tagged slice have different digests.
- The existing nil, pointer, signed-zero, and NaN tests (`internal/session/command_limits_test.go` `TestDigestMatchesDeepEqual`) pass with extension combinations.

### 11.4 Incident identity

One ID scheme serves every incident record of every later stage:

```text
i<generation>.<serial>
```

- `generation` is the session generation.
  It increases at each project apply that rebuilds the fleet, each rewind, and each restart (`internal/session/session.go` `(*Session).startProject`, `(*Session).apply`, `(*Session).applyProject`; `internal/session/checkpoint.go` `(*Session).rewind`; `internal/session/persist.go` `(*Session).installRestored`).
  The session passes it to the simulation with `SetIncidentGeneration` after each change.
- `serial` is a simulation counter, `incidentSerial`.
  It increases by one for each new record and is saved.
  `SetIncidentGeneration` does not reset it.

A rewind restores the cloned serial and increments the generation, so a record made after a rewind gets a new prefix.
A delayed command that names a record of the abandoned timeline finds no record.
Inside one generation the serial only increases, so an ID is unique in its epoch.
Simulation event order uses the serial only, so a run is deterministic for a given generation.

Stage 1 lands the counter, the hook, and the format member.
It makes no record.

### 11.5 Members

Save members (version 9):

| Path | Shape | Presence |
| --- | --- | --- |
| `/simulation/interrupted` | Integer | Omitted at 0. |
| `/simulation/interruptedPassengers` | Integer | Omitted at 0. |
| `/simulation/incidentSerial` | Integer | Omitted at 0. |
| `/simulation/pods/*/withdrawn` | Integer, hold bits | Omitted at 0. |
| `/simulation/pods/*/operational` | `[purpose, owner]`, or `[1, owner, interrupt]` | Omitted for service. `interrupt` is omitted at 0. |
| `/simulation/pods/*/riders/*/legFrom` | Station index | Omitted when absent. Index 0 is written when present. |
| `/simulation/waiting/*/request/legFrom` | Station index | Same. |
| `/simulation/waiting/*/excludedPod` | Index into `/simulation/pods` | Omitted when absent. Index 0 is written when present. |

Station indexes refer to the saved project `network.stations`.
Pod indexes refer to `/simulation/pods`, which is in pod ID order (`internal/sim/state.go` `SavedState`).
The native types keep IDs.
The session adapter converts in both directions, as it does for boarding records (`internal/sim/state.go` `SavedPod`; `internal/session/boarding_state.go` `(boardingSource).encodePodContract`, `(*stateFile).resolveBoardings`).
The adapter wire type uses a pointer for each index, so that presence and index 0 stay distinct.

Stream and HTTP state members:

| Path | Shape | Delta group |
| --- | --- | --- |
| `.../simulation/pending/*/legFrom` | Station ID, packed | `pending` |
| `.../simulation/vehicles/*/riders/*/legFrom` | Same | Vehicle `riders` |
| `.../simulation/vehicles/*/withdrawn` | Integer | Vehicle `metadata` |
| `.../simulation/vehicles/*/operational` | `emergency-unload`, `refuge`, or `empty-recovery` | Vehicle `metadata` |
| `.../simulation/interrupted`, `.../simulation/interruptedPassengers` | Integers | `incident` |

`withdrawn` and `operational` are `VehicleFrame` members (`internal/session/protocol.go` `VehicleFrame`) taken from `Vehicle` (`internal/sim/simulation.go` `Vehicle`), not `Pod` fields.
The pending group is replaced whole under `/delta/groups/pending`, without a `value` wrapper (`internal/session/stream_codec.go` `StreamDelta`).
The exclusion is not in the stream.
It is dispatch state, as `deferPodID` is.

### 11.6 Strict decoding and prescan

- Every decoder rejects unknown members, as today.
- Every decoder rejects a stage 1 member that is `null` or an empty string.
- The save decoder rejects:
  - an unknown hold bit (W1);
  - an `operational` tuple of the wrong length, with an unknown purpose, or with an owner that is not one held bit (W5);
  - an `interrupt` bit that names no active rider;
  - a `legFrom` out of range, equal to `to`, or at a parking station;
  - an `excludedPod` out of range, on a trip with `boarded`, or equal to the `podID` or `deferPodID` of its trip (X1);
  - an Express waiting trip without a path that does not meet S1 to S3 (section 7.6).
  Each failure is `invalid_state` for the whole save.
- The stream decoder rejects an unknown purpose name, an unknown `legFrom` station, and a hold value with an unknown bit.
- The only new array is `/simulation/pods/*/operational`, with an explicit limit of 3.
  Without it, the array would get the fallback limit of 65,536 elements (`internal/session/state_file.go` `stateJSONLimits`, `(jsonLimits).arrayLimit`).
  The stream adds no array.
- Prescan paths are derived from real envelopes.
  `TestFormatArraysHaveLimits` encodes small save, full, delta, and HTTP fixtures, walks every array in them, and fails when an array path has no explicit limit.
  The array audit exists from item 7 patch 4 (`516b797`), and stage 1 extends it.
  This also checks the post-item-5 renames of existing paths.
- Each new limit has a test at the limit, at the limit plus one before typed decoding, at a deeper nesting, and with a gzip body that expands past the byte cap.

### 11.7 Joint byte budget

This section is a record.
The measurements keep their basis of 300 pods and a save cap of 80 MiB, and nobody measured them again.
The plain fleet limit is now 1,000 pods, and the save cap is 128 MiB.
The composed worst-case fixtures, their record, and the landing gate of this section were deleted on October 7, 2026.
The coupling shapes of the tables went with physical coupling on October 6, 2026.

Measured headroom at item 7 patch 8 (`ee5851f`), on October 5, 2026.
The record was `docs/measurements/composed-worst-case-formats.json`, made by `TestComposedWorstCaseFormats` with `PODSIM_COMPOSED_FORMATS_RECORD` set.
Stage 1 patch 9 writes the record again with the stage 1 members.
The two tables of measured headroom after stage 1 give its values.
The fixtures use independent maxima, not reachable states.
The save cap is 83,886,080 bytes (80 MiB), and the stream and HTTP cap at this measurement is 67,108,864 bytes (64 MiB).
Since 64b4f3f, the stream and HTTP cap is 68,157,440 bytes (65 MiB).
The HTTP headroom includes the topology member at its cap of 10,489,856 bytes.

| Shape | Save headroom | Full frame headroom | Delta headroom | HTTP headroom |
| --- | ---: | ---: | ---: | ---: |
| Plain | 27,986,414 | 32,687,488 | 32,563,990 | 22,210,170 |
| Coupling | 27,913,571 | 31,949,972 | 31,826,461 | 21,472,654 |
| Express | 6,836,663 | 12,467,230 | 12,334,161 | 1,989,912 |
| Express with coupling | 6,836,569 | 11,729,714 | 11,596,632 | 1,252,396 |

Packed order text removed the escaped plain IDs, so the plain save is no longer the narrowest shape.
The direct native-ID form of the typed compact fixture stays over the cap (`internal/session/compact_state_bytes_test.go`).
That is why every stage 1 save member uses indexes.

Stage 1 growth per shape, with the widest encodings:

| Member | Widest encoding | Bytes | Plain count | Plain total | Express count | Express total |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| Rider `legFrom` | `,"legFrom":299` | 14 | 2,400 | 33,600 | 6,000 | 84,000 |
| Waiting `legFrom` | `,"legFrom":299` | 14 | 2,600 | 36,400 | 8,600 | 120,400 |
| Waiting `excludedPod` | `,"excludedPod":299` | 18 | 2,600 | 46,800 | 8,600 | 154,800 |
| `withdrawn` | `,"withdrawn":255` | 16 | 300 | 4,800 | 300 | 4,800 |
| `operational` | `,"operational":[1,128,255]`, Express `[1,128,1048575]` | 26, 30 | 300 | 7,800 | 300 | 9,000 |
| Counters and serial | `interrupted` 34, `interruptedPassengers` 44, `incidentSerial` 38 | 116 | 1 | 116 | 1 | 116 |
| Marker | in the project member | 0 | | 0 | | 0 |
| Total | | | | 129,516 | | 373,116 |

Notes on the counts:

- Plain: 300 pods with 8 riders each, and 2,600 waiting trips (`internal/session/state_file.go` `maxSavedTrips`).
  `maxSavedTrips` is now `200 + 8 * 1,000 = 8,200`.
  Express: 20 stored riders per pod and 8,600 waiting trips (`orderBounds`, `internal/session/format_limits.go` `(contractMarkers).orderBounds` at `475cc85`).
- The station index has at most 3 digits, because a project has at most 300 stations (`internal/project/config.go` `MaxPods`).
  The pod index has at most 3 digits for 300 pods (`MaxPods`).
  A project now has at most 600 stations and 1,000 pods, so both indexes still have at most 3 digits.
- The exclusion holds until the trip boards (maintainer decision of October 5, 2026), so a trip can carry both `podID` and `excludedPod`.
  The table counts the exclusion on every waiting trip.
- Rider counts include completed history, because history keeps `legFrom` (section 7.2).

Headroom after stage 1, measured by the composed fixtures of stage 1 patch 9:

| Shape | Before | Stage 1 | After |
| --- | ---: | ---: | ---: |
| Plain save | 27,986,414 | 88,118 | 27,898,296 |
| Coupling save | 27,913,571 | 88,118 | 27,825,453 |
| Express save | 6,836,663 | 240,518 | 6,596,145 |
| Express with coupling save | 6,836,569 | 240,518 | 6,596,051 |

Stage 1 fits under the save cap in every shape.

Stream growth: packed order text adds about 101 bytes for each `legFrom`.
That is about 505,000 bytes for 5,000 plain orders, and about 1,474,600 bytes for 14,600 Express orders.
Vehicle fields add about 15,000 bytes.
`TestStreamMaximumEncoding` and the composed fixtures cover the plain stream.

Measured by the composed fixtures of stage 1 patch 9:

| Shape | HTTP headroom before | Stage 1 | After, 64 MiB cap | After, 65 MiB cap |
| --- | ---: | ---: | ---: | ---: |
| Plain | 22,210,170 | 519,211 | 21,690,959 | 22,739,535 |
| Coupling | 21,472,654 | 519,211 | 20,953,443 | 22,002,019 |
| Express | 1,989,912 | 1,488,811 | 501,101 | 1,549,677 |
| Express with coupling | 1,252,396 | 1,488,811 | 236,415 over the cap | 812,161 |

The Express with coupling HTTP state does not fit after stage 1.
By the maintainer decision of October 5, 2026, a composed shape over its cap raises that cap just enough to fit, with the composed measurement as evidence.
Stage 1 patch 9 measures the composed shapes with its members, and raises the stream and HTTP cap if a shape is over it.

Cap raise of stage 1 patch 9, in whole MiB:

| Cap | Before | After | Evidence |
| --- | ---: | ---: | --- |
| Stream and HTTP JSON, `MaxStreamJSON` | 67,108,864 | 68,157,440 | The measured Express with coupling HTTP state has a bound of 67,345,279 bytes. |
| Stream gzip message, `MaxStreamMessage` | 68,157,440 | 69,206,016 | It stays 1 MiB above the JSON cap, for the expansion of stored blocks. |

Fault and emergency records of later stages get a stream and HTTP allocation at stage 0, beside the save allocation.

Joint save allocation.
The narrowest save shape, Express with coupling, keeps 6,836,569 bytes before stage 1.

| Allocation, narrowest save shape | Bytes |
| --- | ---: |
| Stage 1, this contract | 373,116 |
| Emergency records, stage 3 | 65,536 |
| Fault records and recovery state, stages 2, 4, 5, 6 | 4,194,304 |
| Reserve | 2,203,613 |

At this measurement the save kept 80 MiB.
The save cap is now 128 MiB.
`internal/session/compact_state_bytes_test.go`, the guard that proved the direct native-ID form over the cap, was deleted with the other worst-case byte proofs on October 7, 2026.

Landing gate:

The gate was one composed worst-case fixture for each save shape and each stream envelope, with the stage 1 members and each landed feature at their widest at the same time.
Each fixture was checked for raw size, gzip size, and the JSON scan limits of both limit sets.
The gate was deleted with the composed fixtures.
The byte proofs that `AGENTS.md` names now bound the saved state and the stream.

## 12. Off-state identity

The incident marker that this section gated on was removed on November 4, 2026, by [the marker removal contract](marker-removal-contract-proposal.md).
The off state is now a run in which no operation of this contract runs.
In that run each stage 1 member is zero or absent, so trajectories, save bytes, stream bytes, command digests, and the RNG sequence stay identical.
Stage 1 adds no random source.
The demand PCG (`internal/session/demand.go` `newDemand`) is unchanged.

| Change | Off-state effect | Treatment |
| --- | --- | --- |
| `claimKind` for the other existing conditions | Same results as the inline predicates. | Refactor in the same patch, with equality tests. |
| `completeRider` split from `alight` | None. | Refactor. |
| Withdrawal gates, including `passengerArrivals` | `withdrawn == 0` for every pod, so every test passes as today. | Gated by state. |
| Exclusion gates, the excluded-pod hold gate, and `assignPickup` | `excludedPod == ""` for every trip. `assignPickup` writes the same fields as the five writes it replaces. | Gated by state. |
| Stale deferral clearing | Runs only inside `releasePickups`. | Gated by state. |
| `dispatchOptions` cache key | Equals `options()` when `LegFrom` is absent; the exclusion part is empty. | Gated by state. |
| `legOrigin` readers | Equal `From` when `LegFrom` is absent. | Gated by state. |
| Stranded-order validator exception | Needs `LegFrom`. | Gated by state. |
| Purpose branches in `arrive`, the unloading loop, `assignTerminalBerth`, and the phase rules | Purpose 0 takes the existing branches. | Gated by state. |
| Conservation term `Interrupted` | 0. | Gated by state. |
| `deliverInterruptions` and `Connections.Interrupt` | Nothing to drain. `Advance` does not change. | Gated by state. |
| Save and stream members | Omitted at zero or absent. | Gated by state. |
| Digest extension | No tagged field set, so no trailer. | Gated by value. |
| Cap raise, if decided | Accepts larger files. Changes no bytes. | Own format patch at stage 0. |

Gates for every patch, as defined at approval, when the marker existed:

- Format goldens of the single family match byte for byte with the marker absent.
- The digest baseline matches with the marker absent.
- Matched-seed runs on LondonCentral and the rail-hub preset, with the marker absent and present, give identical exported simulation states at every 10 s of simulated time, apart from the marker.

## 13. API for later stages

| Function | Caller | Preconditions |
| --- | --- | --- |
| `withdrawService(v, hold)` | Fault start (stage 2), emergency start (stage 3) | Section 4.2. |
| `restoreService(v, hold)` | Fault clear, emergency end | Section 4.2. Refused for the owner hold of a purpose. |
| `rebindOperationalOwner(v, to)` | A recovery that outlives its cause | Section 4.2. |
| `releasePickups(v)` | Only through `withdrawService` | Section 5.1. |
| `claimKind(v, r)`, `revocable(v, r)` | Yield paths and section 9.3 | None. Read only. |
| `continuationFeasible(request, station)` | Station choice for an emergency unload (stage 3) | None. Read only. |
| `evacuate(v)` | Fault evacuation (stage 2) | Section 8.2. |
| `setOperationalDestination(v, to)` | Refuge (stage 2), emergency station (stage 3), recovery (stages 2 and 3) | Section 9.3. |
| `startOperationalUnload(v, owner, interrupt)` | Emergency at a berth (stage 3) | Section 9.3. |
| `resumeFromRefuge(v)` | Refuge recheck (stage 2) | Section 9.3. |
| `DrainInterruptions()`, `Connections.Interrupt` | The session and `cmd/compare` | Section 8.5. |
| `nextIncidentID()`, `SetIncidentGeneration(g)` | Every record of stages 2 and later; the session | Section 11.4. |
| `InterruptRider(podID, orderID)` | Test entry: the session tests | An active rider whose destination another active rider of its pod shares. No platoon member, and no dispatch pass. |
| `IncidentForTest(podID, operation)` | Test entry: the session save and stream tests of each incident state | No dispatch pass. `operation.Kind` names one stage 1 operation: withdraw, restore, destination, unload, resume, or evacuate. |

Stage 1 exports only `DrainInterruptions`, `Connections.Interrupt`, and `SetIncidentGeneration`, and the test entries `InterruptRider` and `IncidentForTest`.
The test entry `FailStepForTest` went with the step-fault seam of the compact station queues on October 7, 2026.
The maintainer approved the test entries on 2026-10-05.
Later stages call the other functions from inside `internal/sim`.

Preconditions that later stages must meet before they call:

- The operations refuse a platoon member.
  Stage 6, which would separate it, is out of scope.
- A moving pod is not evacuated until it is at rest; stage 2 brings it to rest.
- A policy that prefers a station where each party can continue may test `continuationFeasible` before it selects the station.
  The foundation transfers each party in any case (section 7.3).
- A policy that ends a cause while its purpose is active calls `rebindOperationalOwner` first, or waits for the arrival that clears the purpose.

## 14. Test plan

### 14.1 Unit tests

| Area | Fixture | Invariant |
| --- | --- | --- |
| Withdrawal gates | One fixture per row of section 4.3, with the withdrawn pod as the best choice | The path skips the pod. |
| Withdrawal inverse | Withdraw and restore with each hold, and with both holds in both orders | Supply membership, route, physical destination, and owners equal the start. Released trips stay released. Only `released` differs. |
| Hold independence | Fault and emergency holds on one pod; restore one | The pod stays withdrawn. |
| Owner hold removal | Both holds set, purpose 1 owned by the emergency hold | `restoreService(emergencyHold)` fails with no change. After `rebindOperationalOwner(faultHold)` it succeeds. W5 holds. |
| Hold mask rejection | Both holds set, purpose 1 owned by the emergency hold | `restoreService` with the mask of both holds, with 0, and with an unknown bit each fail. The pod, its holds, and `op` do not change, and `CheckContract` (`internal/sim/state_contract.go` `(*Simulation).CheckContract`) passes after each call. |
| Pickup release | Section 5.5 | Section 5.5. |
| Exclusion gates | Section 5.3, one fixture per row | The excluded pod never receives the trip. |
| Cache key | Two trips with equal options and different exclusions in one pass | Different pods or one waits. |
| No yield for emergency demand | An empty relocating pod holds an ordinary revocable destination claim. A withdrawn pod with riders goes to that berth, as a traveling arrival. | The claim stays. The withdrawn pod takes a free berth or waits. Control: the same fixture with an in-service passenger pod releases the claim. |
| Claim kinds | One resource per kind, including a follower retention and an admitted destination | `claimKind` returns the kind. Only `claimService` is revocable. |
| Claim refactor equality | The existing yield suites | Same results. |
| Leg origin readers | One test per "Physical" or "Both" row of section 7.4 | The reader uses `LegFrom`. |
| Leg origin at index 0 | Transfer to the station with index 0, also when it equals `From` | Saved with `legFrom: 0`. Restores present. |
| Emergency unload outcomes | Four riders: one marked, one unmarked at its destination, two others | One interruption, one completion, two transferred trips in order ID position, `boarded` true, no exclusion. |
| Distance freeze, mixed unload | Recorded riders with nonzero boarding baselines: an onboard pickup at 50 m completes at the station, a marked rider is interrupted, and the last rider by index, with the largest baseline, transfers | After `settleIdleAtBerth`, `RiddenMeters` equals the cumulative distance captured before the first outcome. Every retained baseline is at or below it. The save validator (`internal/sim/state_boarding.go` `checkSavedBoardingsWithOrderContract`) and the stream validator (`internal/session/stream_boardings.go` `validateVehicleBoardingsContract`) accept the pod. Evacuation variants at a berth and on a lane add an already completed rider with a retained boarding record, because `evacuate` interrupts every active rider; the same assertion applies to them. |
| Stranded transfer | Express party unloaded at a station with no Express path to `To` | Transferred, not interrupted. Waits with the certified-vehicle reason. Save, both restore tiers, and a stream frame accept it. The same trip with a `podID`, a route, a `deferPodID`, or an `excludedPod` is rejected in `internal/sim/order_contract_restore.go` `checkContractRestoreSemantics` and `internal/session/express_orders.go` `(*StreamAssembler).expressOrders`. |
| Interruption | Rider with a rail binding | No `StepCompletion`. Rail `unserved`, reason `interrupted`. Counters and conservation. |
| Departure-tick delivery | A session with a rail-bound order whose departure deadline is the next tick. A `Step` interrupts the rider on that tick. | The record is `unserved` with reason `interrupted`, not `missed`. `demand.state.Connections` equals `Connections.Counts()` after the tick. |
| Berth evacuation | Pods at a berth in `Boarding` with phase above 0 and a grant ahead, `Continuing`, `Unloading` with phase above 0, purpose 1 unloading, and refuge holding. Each has a rider whose destination is the station. | Every active rider is interrupted. No completion. Every field of the `settleIdleAtBerth` table. The idle rule passes. Each grant ahead is free after the release boundary. |
| Lane evacuation | One pod per route shape of section 9.5: berth end and entry end | Every active rider is interrupted. The pod reaches a berth and becomes idle. The purpose clears at arrival, and the fault hold stays. |
| Purposes | Each purpose: install, travel, arrive, act | Section 9.5. Phase rules hold each tick. |
| Parking refuge | Riders with boarding records hold at a parking berth | Live check passes. Physical restore keeps the pod. Logical restore requeues every active rider. |
| Refuge resume | Route exists, and route missing | `Continuing`, or purpose 2 kept with an error. |
| Start at berth | Boarding, continuing, and unloading pods with granted track | Section 9.3. Grants free at the release boundary. |
| Atomicity | Each precondition failure of each operation | No change to pods, queue, owners, or counters. |
| Digest | Section 11.3 | Section 11.3. |
| Incident IDs | Create a record ID, rewind, create another | New generation prefix. No reuse. |

### 14.2 Restore tier tests

- Each row and column of section 9.6, with order IDs and counters checked, not only the balance.
- A logical restore of an emergency unload at the destination of the marked party: interrupted, not completed.
- Purpose 3 with each route shape of section 9.5, through the physical tier.
- A multi-stop journey with no incident: physical and logical round trips equal the off-state result.
- History: a transferred party completes, then the pod saves idle; `legFrom` survives on the completed rider, and the boarding tuple resolves.

### 14.3 Codec tests

- Golden bytes and the digest baseline with no incident.
- Each stage 1 member as `null` or as an empty string: rejected.
- Each rejection rule of section 11.6.
- Express packed `legFrom` in full and delta frames; `scanPackedOrders` rejects non-canonical text in it.
- Each delta group of section 11.2 carries its member, and the `global` group bytes are unchanged.
- The prescan tests of section 11.6.

### 14.4 Composed byte tests

The composed byte tests ran the landing gate of section 11.7 and asserted its table values as allocations.
They were deleted with the composed fixtures on October 7, 2026.

### 14.5 Command-boundary and end-of-tick saves

For each new state, the test saves at a paused command boundary right after the operation, and at the end of the tick of each transition.
Each save passes `CheckContract`, both restore tiers or the restore table, and an export equality check after a physical restore.

| State | Command boundary | End of tick |
| --- | --- | --- |
| Withdrawn idle, traveling, and at berth | After `withdrawService` | After the next dispatch |
| Excluded trip | After `releasePickups` | After an assignment to another pod |
| `LegFrom` waiting, aboard, and in history | After a transfer | After boarding and after completion |
| Stranded order | After a transfer | After the next dispatch |
| Purpose 1 traveling and unloading | After `setOperationalDestination` and `startOperationalUnload` | After `arrive`, and after `finishOperationalUnload` |
| Purpose 2 traveling and holding | After `setOperationalDestination` | After `arrive`, and after `resumeFromRefuge` |
| Purpose 3 traveling | After `evacuate` on a lane | After `arrive` |
| Interrupted counters | After `evacuate` | After `finishOperationalUnload` |

Full session saves, not only `Simulation.CheckContract`:

- A paused session with a rail-bound order.
  A session test helper runs `evacuate` under `s.mu` and then the same epilogue as `Session.apply`.
  Then `SaveState` writes the complete save.
  The save restores, the rail record is `unserved` with reason `interrupted`, and `validateRecord` (`internal/rail/connections.go` `validateRecord`) accepts it.
- A publication right after the command shows the same rail counts as the save.

### 14.6 Mutation targets

Each mutation needs a passing control, a compiled mutant, and a failing test.
The table records the targets at landing.
The rows for `bufferClaimCanYield`, the coupling test of `claimKind`, buffer membership, the Compact pause return, the marker, and the marker agreement check name code that was removed later.

| Mutation | Call site |
| --- | --- |
| Drop the withdrawal test | `pickupCandidate`, `internal/sim/diversion.go` `(*Simulation).pickupCandidate` |
| Drop the withdrawal test | `localPickupForRequest`, `internal/sim/dispatch.go` `(*Simulation).localPickupForRequest` |
| Drop the withdrawal test | `boardingPods`, `internal/sim/dispatch.go` `(*Simulation).boardingPods` |
| Drop the withdrawal test | `waitForFinishingPod` loop, `internal/sim/pickup_estimate.go` `(*Simulation).waitForFinishingPod` |
| Drop the withdrawal test | `guardedSupply`, `internal/sim/positioning.go` `(*Simulation).guardedSupply` |
| Drop the withdrawal test | `clearBlockedBerths`, `internal/sim/parking.go` `(*Simulation).clearBlockedBerths` |
| Drop the withdrawal test | `parkUnclaimedReleased`, `internal/sim/released.go` `(*Simulation).parkUnclaimedReleased` |
| Count a withdrawn pod as a passenger arrival | `passengerArrivals`, `internal/sim/redistribution.go` `(*Simulation).passengerArrivals` |
| Let a withdrawn head pass the head test | `bufferClaimCanYield`, `internal/sim/station_buffer_claim.go` `(*Simulation).bufferClaimCanYield` |
| Leave `deferPodID` set in `releasePickups` | New `releasePickups` |
| Unbind a trip with stale deferral metadata | New `releasePickups` |
| Set an exclusion on a boarded trip | New `releasePickups` |
| Allow a hold for the excluded pod | `waitForFinishingPod`, `internal/sim/pickup_estimate.go` `(*Simulation).waitForFinishingPod` |
| Keep a hold that names the excluded pod | `waitForFinishingPod`, `internal/sim/pickup_estimate.go` `(*Simulation).waitForFinishingPod`, and `keepHold`, `(*Simulation).keepHold` |
| Skip the exclusion in `pickupPodMatching` | `internal/sim/dispatch.go` `(*Simulation).pickupPodMatching` |
| Key the cache by `options()` | `internal/sim/dispatch.go` `(*Simulation).dispatch` |
| Skip the exclusion in promotion | `internal/sim/dispatch.go` `(*Simulation).promoteReadyPickup` |
| Skip the exclusion of the later trip in promotion | `internal/sim/dispatch.go` `(*Simulation).promoteReadyPickup` |
| Skip the exclusion in `joinSharedRide` | `internal/sim/dispatch.go` `(*Simulation).joinSharedRide` |
| Clear the exclusion in `assignPickup` | New `assignPickup` |
| Keep the old exclusion on a second release | New `releasePickups` |
| Skip the exclusion of either trip in a pickup swap | `checkPickupPair`, `internal/sim/pickup_reassignment.go` `(*Simulation).checkPickupPair` |
| Skip the exclusion in a pickup transfer | `checkPickupPair`, `internal/sim/pickup_reassignment.go` `(*Simulation).checkPickupPair` |
| Drop the coupling test from `claimKind` | New `claimKind` |
| Release a stopping grant as a service claim | New `claimKind` |
| Use `From` in `board` | `internal/sim/dispatch.go` `(*Simulation).board` |
| Use `From` in the boarding tuple decoder | `internal/session/boarding_state.go` `(*stateFile).resolveBoardings` |
| Infer `legFrom` for completed riders | `internal/sim/state_physical.go` `(*physicalRestore).decodePod` |
| Omit `legFrom` at index 0 | Save adapter |
| Interrupt a party without a feasible continuation | New `finishOperationalUnload` |
| Accept a stranded trip that has a pod or a route | `internal/sim/order_contract_restore.go` `checkContractRestoreSemantics`, `internal/session/express_orders.go` `(*StreamAssembler).expressOrders` |
| Write a `StepCompletion` for an interrupted rider | New `interruptRider` |
| Complete a destination rider in `evacuate` | New `evacuate` |
| Keep `phaseTicks` or `op` in `settleIdleAtBerth` | New `settleIdleAtBerth` |
| Omit `Interrupted` from the balance | `internal/sim/state_contract.go` `(SavedState).checkContract` |
| Skip delivery on the Compact pause return | `internal/session/speed.go` `(*Session).step` |
| Deliver after `demand.step` | `internal/session/speed.go` `(*Session).step` |
| Keep the old counts after delivery | New `deliverInterruptions` |
| Freeze the distance only when the last outcome is an interruption | New `finishOperationalUnload` |
| Accept a `restoreService` mask of two holds | New `restoreService` |
| Accept a stranded trip with `boarded` false or `deferCheck` set | `internal/sim/order_contract_restore.go` `checkContractRestoreSemantics` |
| Apply a markerless delta with an explicit zero `withdrawn` | `ApplyStream`, `internal/session/stream_codec.go` `ApplyStream` |
| Skip delivery at the end of a command | `internal/session/session.go` `(*Session).apply` |
| Complete a marked rider at its destination | New `finishOperationalUnload` |
| Allow `restoreService` of an owner hold | New `restoreService` |
| Drop purpose 3 from the terminal berth choice | `internal/sim/berth_choice.go` `(*Simulation).assignTerminalBerth` |
| Drop purpose 3 from buffer membership | `internal/sim/state_physical.go` `(*physicalRestore).buildRoutes` |
| Drop the refuge exemption | `internal/sim/state_boarding.go` `checkBoardingBerths` |
| Set `RelocatingTo` for purpose 1 | New `setOperationalDestination` |
| Keep `Stops[0] == DestinationStation` for purposes 1 and 2 | `internal/sim/state_contract.go` `checkPodStops` |
| Change the purpose in a per-tick pass after `arrive` | `internal/sim/simulation.go` `(*Simulation).arrive` |
| Hash a skipped extension field as a nil mark | `internal/session/receipt.go` `digestWriter` |
| Emit a stage 1 member without the marker | Save and stream encoders |
| Skip the marker agreement check | `frameState`, `internal/session/protocol.go` `frameState` |
| Drop the generation from incident IDs | New `nextIncidentID` |
| Remove the prescan limit of `operational` | Save limits |

## 15. Patch order

Each patch compiles and passes the full suite on its own.
The list records the order at landing, and sections 6.3, 11.2, and 11.7 name what was removed later.

1. Claim classification and the coupling guard (section 6.3).
   Baseline fix.
2. Digest extension registry, with no tagged field.
   It reuses the item 7 digest baseline file.
3. Incident marker, digest tag, marker propagation, and incident ID counter.
4. Service withdrawal, its gates, and the yield exclusions of section 6.2.
5. Pickup release, stale deferral clearing, and the exclusion.
6. Leg origin, the reader changes, transfer, and the stranded-order exception.
7. Interrupted outcome, conservation, session delivery, and rail.
8. Operational destination, owner rules, phase rules, arrival actions, and evacuation.
9. Save and stream members, strict decoding, prescan limits, and the composed fixtures.

The off-state gates of section 12 run after each patch from patch 2.

## 18. Open questions

| # | Question | Approved default |
| --- | --- | --- |
| 1 | Does stage 1 add its own project marker, `incidentContract`? | Yes. Feature markers require it. The marker removal contract removed the marker on November 4, 2026. |
| 2 | Do active finishing-pod holds on an affected pod count as pending pickups that get the exclusion? | Yes. The hold waits for that pod. |
| 3 | Can an excluded trip hold for a finishing pod? | Yes, for any pod except the excluded pod. The exclusion holds until the trip boards (maintainer decision of October 5, 2026). |
| 4 | What happens when the excluded pod is the only pod that fits? | The trip waits until another pod fits. The binding decision allows no exception. |
| 5 | Does a stranded order wait without a time limit in stage 1? | Yes. A later policy can choose stations that avoid it. |
| 6 | Does a marked emergency party complete when the station is its destination? | No. The mark wins, for one rule and one metric. |
| 7 | Does refuge holding get its own activity code? | No. It reuses `unloading` with purpose 2 and phase 0. |
| 8 | Does an emergency unload or an empty recovery release its owner hold on arrival? | No. Arrival clears the purpose. The policy calls `restoreService`, so a faulted pod stays withdrawn until the fault clears. |
| 9 | Do completed riders keep `legFrom`? | Yes. It is saved and never inferred. |
| 10 | Do park-and-ride runs and `cmd/compare` accept the incident marker in stage 1? | No. The ledger outcome `interrupted` lands with the first policy that can interrupt a park-and-ride party. The marker was removed on November 4, 2026 (section 11.2). |
| 11 | Is the save cap raised, and when? | Item 7 kept 80 MiB, and the cap is now 128 MiB. The item 7 patch 8 fixtures leave at least 6,463,453 save bytes after stage 1. Stage 0 decides the stream and HTTP allocation on the same fixtures. A composed shape over its cap raises that cap just enough (maintainer, October 5, 2026). |
| 12 | Does a restore keep holds? | Yes. Each later stage releases its own hold when its records do not survive. |
| 13 | Is the exclusion visible to clients? | No. The dispatch reason covers it. |
| 14 | Can a refuge be a parking station? | Yes, when the berth allows the class. Emergency unloading needs a passenger station. |
| 15 | Does the session deliver interruptions on the coupling error return of `Session.step`? | Yes. Delivery runs right after `Simulation.Step`, before every return path and before `demand.step`. The coupling error return went with physical coupling on October 6, 2026. |
