# Incident suspension contract proposal

Status: approved, revision 5, October 5, 2026, by the coordinator under the maintainer delegation of October 5, 2026, after blind Astra review rounds and an adversarial check.
The approval changes no binding decision and no product choice beyond the maintainer-approved amendment D3 and the stage 2 defaults.
Line references are at `3c77045`.

Status note, October 6, 2026: the maintainer stopped the incident redesign after stage 3.
Stages 4 to 7 are out of scope.
Text that names these stages describes refusals that stay in place.

This contract is stage 2 of the staged incident redesign.
It defines ordinary suspension and obstruction: a pod fault that stops a pod on a lane or at a berth, debris on an empty lane segment, routing around both, and the rules for the healthy pods that they obstruct.
It builds only on the API of the approved stage 1 contract, [incident-service-transitions-contract-proposal.md](incident-service-transitions-contract-proposal.md), section 13, with one narrow amendment (section 1.6).

Every `file:line` reference below is for `3c77045`.
At that commit, stage 1 patches 1 to 7 have landed: claim kinds, the digest registry, the incident marker and its propagation, the incident ID counter, service withdrawal with its gates, pickup release with the exclusion that holds until the trip boards, the leg origin, and the interrupted outcome.
Stage 1 patches 8 and 9 have not landed: the operational destination and the stage 1 save and stream members.
This contract cites those parts by stage 1 section, not by `file:line`.
Stage 2 starts after stage 1 patch 9.

Revision 2 folded the first external review (section 21).
Revision 3 narrows the rules for obstructed pods after the second review, with the shape that the maintainer approved on October 5, 2026 at 17:43Z.
Revision 4 folds the third review, and revision 5 folds the check of revision 4.
Section 22 maps each finding of the later reviews to its resolution.

## 1. Scope

### 1.1 What stage 2 delivers

1. Pod faults: controlled braking to rest with one braking step for motion and for every motion proof, immobility, evacuation after a configurable delay, and recovery after the clear (sections 5 and 6).
2. Debris faults on a free lane segment that no pod claims (section 7).
3. Blocked routing: route searches that avoid the lanes and berths of active faults, with static order admission and static structural checks (section 8).
4. Rules for obstructed pods, which stay in service: one route-only reroute to the current endpoint, pickup unbinding through dispatch, the surrender of unused service claims, and wait reports that each tick derives again (section 9).
5. The fault marker, project settings, commands, and the fault members of each format (sections 12 and 13).
6. A qualification gate set for a later default-on decision (section 17).

### 1.2 Out of scope

| Item | Stage | Stage 2 rule |
| --- | --- | --- |
| Rider emergency | 3 | No change. `emergencyHold` stays unused. |
| Refuge policy | 3 | Stage 2 sends no pod to a refuge. Stage 1 purpose 2 (refuge travel) stays an unused stage 1 capability. |
| Fault on a coupling member or an approach member | 4 | Refused before any change, with `fault target is not supported`. |
| Reverse motion | 5 | A pod with no forward path waits, and the wait is reported (section 9.6). |
| Fault on a platoon leader, a platoon follower, or a compact queue member | 6 | Refused before any change, with `fault target is not supported`. |
| Release of unused grants of a stopped pod | 5 or later | Grants stay until a certified release transition exists (section 6.4, product choice P9). |
| Scenario rate above 0, the full policy, scenarios, controls, metrics, and debris drawing | 7 | The settings validator refuses `perHour` above 0. Section 12.2 specifies the sampler mechanism for stage 7. |

Each refusal happens before the first write.
No accepted fault depends on a later stage.

### 1.3 Binding user decisions

These decisions come from `PROJECT_BRIEF.md`, section "Vehicle fault or accident", and from the maintainer decisions of October 5, 2026.
This contract does not change them.

1. A fault brakes the pod in a controlled way to rest.
   It does not stop the pod at once.
2. Debris inside the stopping distance of a pod is refused.
3. A pod behind a fault that has no forward path waits, and the wait is reported.
   Reverse motion arrives in stage 5.
   The nearest usable junction is acceptable as a reverse target there.
4. Evacuation counts from fault onset, but it never starts before the pod is at rest.
   Every evacuated party is interrupted.
   The default delay is 300 simulated seconds, and a scenario can set another delay.
5. A fault can have a duration.
   It clears at the end of the duration or at a user clear, whichever is first.
6. A fault starts from a game control on a pod, from a scenario rate with a duration distribution, or from a protocol command.
   The scenario rate is a product choice: this contract specifies the mechanism and keeps the rate at 0.
7. A fault can be on a lane, at a berth (the berth is blocked), or in a station entry queue.
   In stage 2, the pods behind a fault in an entry queue change route or wait.
   Debris can block an empty lane segment.
8. The feature is off by default.
   A command when the scenario does not enable it gets `command_rejected`.
9. Released pending pickups go to other pods (stage 1, section 5).
   The pickup exclusion holds until the trip boards, and swaps, transfers, and reassignment never move a trip onto its excluded pod (maintainer amendment of October 5, 2026).
10. A damaged or invalid save moves aside, and no code recovers part of it (`AGENTS.md`).
    A physical restore that cannot keep an active pod fault fails as a tier, with no partial recovery of that record (maintainer decision of October 5, 2026, section 12.7).
11. Healthy pods are never withdrawn, and stage 2 has no refuge (maintainer decision of October 5, 2026, 17:43Z).

### 1.4 Capability matrix

| Target | Stage 2 | Later |
| --- | --- | --- |
| Ordinary pod, `Traveling`, on a lane or in a station entry queue | Accepted | |
| Ordinary pod at a berth: `Idle`, `Boarding`, `Unloading`, `Continuing`, `DepartingEmpty` | Accepted | |
| Buffered pod (`v.buffered`) | Accepted. It keeps its buffer membership and its grants (section 6.4). | |
| Pod with an operational purpose (stage 1, section 9) | Accepted | |
| Pod that has `faultHold` and no record, during its fault recovery | Accepted. The hold is reused (section 5.2). | |
| Pod that already has a pod fault | Refused with `pod already has a fault` (product choice P5). | |
| Coupling member (`couplingID != ""`) or approach member (`couplingApproachMember`) | Refused | Stage 4 |
| Platoon leader (`follower != 0`) or follower (`link.leader != 0`) | Refused | Stage 6 |
| Compact queue member | Refused | Stage 6 |
| Debris on a free segment that no pod and no group claims | Accepted | |
| Debris that meets a pod body, a pod claim, another fault, or a group claim | Refused (product choice P1) | |
| Debris on the remaining route of a coupling or approach member, also beyond its grants | Refused | Stage 4 |
| Debris on a berth or a berth node | Refused | Later stage, if chosen |
| `perHour` above 0 | Refused | Stage 7 |

### 1.5 Stage 1 parts used

| Stage 1 part | Section | Use here |
| --- | --- | --- |
| `withdrawService`, `restoreService` | 4.2 | Fault start and fault clear |
| `releasePickups` with the exclusion | 5.1, 5.2 | Through the first hold, at fault start only |
| `claimKind` | 6.1 | Debris refusal messages, the claim surrender (section 9.5), and tests |
| `evacuate`, with lane evacuation to purpose 3 | 8.2, 9.5 | Evacuation of a faulted pod at rest, and its fault recovery on the original route |
| The interrupted outcome | 8 | Evacuated parties |
| `legOrigin` | 7.1 | Pickup access (section 9.4) |
| `nextIncidentID`, `SetIncidentGeneration` | 11.4 | Fault IDs |
| The incident marker and the digest registry | 11.2, 11.3 | The fault marker requires the incident marker. New extension numbers. |
| Restore tiers | 9.6 | Section 12.7 |
| The joint byte budget, as amended for the until-boarding exclusion | 11.7 | Section 13.6 |

Stage 2 does not call `setOperationalDestination`, `resumeFromRefuge`, or `rebindOperationalOwner`.

### 1.6 Amendment to stage 1

The maintainer approved this amendment on October 5, 2026 at 17:43Z, as decision D3 of the second review.

Stage 1 section 4.3 says that a withdrawn pod keeps its grants and owners.
Stage 1 section 5.4 says that a withdrawn pod keeps its claims until a later stage changes its operational destination.
Stage 2 adds one exception to both rules:

- After fault withdrawal, a pod can give up each resource of its destination berth that it owns and that `claimKind` (`internal/sim/claim_kinds.go` `(*Simulation).claimKind`) classifies as `claimService`.
- The release uses `releaseOwned` (`internal/sim/traffic.go` `(*Simulation).releaseOwned`).
- The pod keeps every other claim and grant, its route, its destination, and its purpose.
- `withdrawService` and `evacuate` do not change, and neither one releases a claim.
- Recovery takes the berth again through ordinary admission, and waits when the berth is not free.

The exception has two callers: fault start (section 5.2, effect 3) for a pod with an active record, and the claim surrender (section 9.5) for a pod in its fault recovery after the clear.
`claimService` holds only for an empty pod with a relocation that is not at its destination berth (`internal/sim/claim_kinds.go` `(*vehicle).claimService`), so the exception never releases a claim of an occupied pod.
`claimKind` tests the physical kinds first, so a destination resource inside a stopping grant is never `claimService`.
Stage 1 W3 does not change: the pod releases only its own claims, and it authorizes no yield of another pod.

## 2. Terms

| Term | Meaning |
| --- | --- |
| Fault | One active incident record of kind pod or debris. |
| Pod fault, or mechanical fault | A record on one pod. The pod is the faulted pod. The record ends at its duration or at a clear, whatever holds the pod has. |
| Fault recovery | The empty recovery (stage 1 purpose 3) that lane evacuation starts with owner `faultHold`. It continues the original route. It can outlive the record. It is not a fault. |
| Debris | A record on a lane segment `[fromMeters, toMeters]` with no pod and no claim in it. |
| Fault footprint | The set of resources that a fault blocks (sections 6.5 and 7.2). |
| Blocked set | The lanes and berths that meet an active fault footprint (section 8.1). |
| Epoch | One value of the blocked set. A change of the set starts a new epoch. |
| Fault stage | The new place in `Step`, after the unloading loop and before `dispatch` (section 5.5). |
| Fault cap | The route distance at which a faulted pod stops, fixed at onset (section 6.1). |
| Fault braking step | The one motion step of a faulted pod, used by `move` and by every motion proof (section 6.1). |
| At rest | `Pod.Speed == 0`. |
| Healthy pod | A pod without a pod fault. Stage 2 never withdraws a healthy pod. A pod in its fault recovery is healthy, and it keeps the `faultHold` of its fault. |
| Obstructed pod | A healthy pod whose remaining route has a blocked lane, or whose current continuation a fault refuses. |
| Endpoint | The last node of the route of a pod: its destination berth, or a station entry when the pod has no destination berth yet. |
| Incident outstanding | An active record exists, or a pod has `faultHold` (section 9.5). |

## 3. Existing source boundaries

| Area | Anchor | Fact that this contract relies on |
| --- | --- | --- |
| Tick order | `internal/sim/simulation.go` `(*Simulation).Step` | `tick++` comes first. The unloading loop follows. `dispatch` comes next, then `swapPickups`, `redistribute`, `formPlatoons`, `admit`, `clearBlockedBerths`, `formCompactQueues`, `platoonCaps`, and `planNativeCouplingTick`. The departure branch follows, then `moveAndMeasure` and `releaseCleared`. |
| Approach discovery | `internal/sim/coupling_approach_runtime.go` `(*Simulation).discoverCouplingApproaches` | Runs before `tick++` (`internal/sim/simulation.go` `(*Simulation).Step`). `couplingApproachMember` is in `internal/sim/coupling_approach_runtime.go`. |
| Approach route | `internal/sim/coupling_approach_context.go` `prepareCouplingApproach` | An approach binds a corridor of its route beyond its current grants. |
| Constants | `internal/sim/simulation.go` `TicksPerSecond`, `acceleration`, `Clearance` | `TicksPerSecond = 60`, `acceleration = 2.0`, `Clearance = 12`. |
| Wait reasons | `internal/sim/simulation.go` `WaitReason`, `NoWait`, `ParkingUnavailable` | Fixed strings. |
| Vehicle fields | `internal/sim/simulation.go` `vehicle` | `withdrawn`, `nextRelease`, `platoonCap`, `link`, `follower`. |
| Arrival | `internal/sim/simulation.go` `(*Simulation).arrive` | An empty move (`RelocatingTo != ""`) becomes idle, also when the pod carries riders. |
| Admission | `internal/sim/traffic.go` `(*Simulation).admit` | Coupling members are skipped. `assignTerminalBerth` can fail before any wait reason. The wait reason is reset. |
| Grant | `internal/sim/traffic.go` `(*Simulation).grant` | The owner loop sets `BlockedBy = owner.String()`. |
| Grant span | `internal/sim/traffic.go` `reservationEnd` | Closes a span over junction runs. |
| Route install | `internal/sim/traffic.go` `(*Simulation).setVehicleRoute` | Replaces the route and rebuilds the blocks and block starts. Keeps the buffer membership when the last lane is the same. |
| Motion | `internal/sim/traffic.go` `(*Simulation).move` | `limit = blocks.end(reservedThrough)`, lowered by `platoonCap` for a follower. The kernel call is in the same function. |
| Motion kernel | `internal/sim/coupling_native_foreign.go` `ordinaryMoveStep` | Brakes to any limit with `safe = sqrt(a²dt² + 2a·available) - a·dt` and snaps to the limit within `1e-5`. |
| Stopping distance | `internal/sim/platoon.go` `stoppingDistance` | `speed² / (2 × acceleration)`. |
| Release boundary | `internal/sim/traffic.go` `(*Simulation).releaseCleared`; `(*Simulation).releaseOwned` | A pod that is not traveling releases every entry of `routeReleases` except its berth. |
| Owner release | `internal/sim/platoon.go` `(*Simulation).releaseRouteResource` | The one path that frees a resource of an ordinary pod. |
| Claim kinds | `internal/sim/claim_kinds.go` `(*Simulation).claimKind`, `(*Simulation).revocable`, `(*vehicle).claimService` | The ordered tests. Only `claimService` is revocable. `claimService` is an unused destination claim of an empty pod with a relocation. |
| Claim yield | `internal/sim/redistribution.go` `(*Simulation).redistribute`, `(*Simulation).yieldRelocationClaims` | `yieldRelocationClaims` releases a revocable destination claim and keeps `RelocatingTo`. A released pod that yields calls `parkReleased`. |
| Released parking | `internal/sim/released.go` `(*Simulation).parkReleased`, `(*Simulation).parkUnclaimedReleased`; `internal/sim/dispatch.go` `(*Simulation).dispatch` | `parkUnclaimedReleased` runs at the end of `dispatch` and parks each released in-service pod that does not own its destination berth. |
| Saved claims | `internal/sim/state.go` `SavedPod`; `internal/sim/state_physical.go` `(*physicalRestore).claimDestinations` | `ClaimsDestination` records a relocation claim. `claimDestinations` restores only a saved claim. |
| Foreign proof facts | `internal/sim/coupling_native_foreign_fleet.go` `nativeForeignFact`, `nativeForeignLimit` | `nativeForeignLimit` applies `fact.cap` only when `link.leader != 0`. |
| Foreign proof check | `internal/sim/coupling_native_foreign_proof.go` `(*nativeForeignTick).prepareProof`, `(*nativeForeignTick).checkPredecessorCap`, `checkNativeForeignSweep` | The proof predicts `ordinaryMoveStep` and checks the raw step. The stationary branch accepts `Idle`, `Boarding`, `Unloading`, `Continuing`, and `DepartingEmpty` at a canonical berth. `checkPredecessorCap` binds the follower cap. |
| Owner kinds | `internal/sim/resource_owner.go` `(resourceOwner).String`, `groupOwnerKind`, `podOwnerKind` | `podOwnerKind` and `groupOwnerKind`. `String` has a default for an unknown kind. |
| Retained owners | `internal/sim/ownership.go` `(*Simulation).retainedOwners`; `internal/sim/state_physical.go` `(*physicalRestore).restoreTrip` | `verifyRestore` requires the owners to equal `retainedOwners()`, which covers pods and groups only. |
| Physical restore | `internal/sim/state_physical.go` `(*physicalRestore).placeTravelingPod`, `(*vehicle).footprint`, `(*physicalRestore).separate`, `(*physicalRestore).placeDemoted` | A traveling pod restores at speed 0 with `through = reservationEnd(&v.blocks, current)` and `v.footprint(through, distance)`. The tier can demote a pod to a berth and requeue its riders. |
| Diversion | `internal/sim/diversion.go` `(*Simulation).divertStart`; `(*Simulation).sendPickupMatching`; `(*Simulation).redirect` | `divertStart` refuses coupling, platoon, and compact members and arrival-chain prefixes. Its arrival-chain test calls `stationPathForClass`. `redirect` releases destination claims and sets `RelocatingTo`. |
| Pickup candidates | `internal/sim/diversion.go` `(*Simulation).pickupRouteWithAssignments`; `(*Simulation).pickupCandidate`; `(*Simulation).candidateRoutePartsMatching` | A candidate route is the `divertStart` prefix plus a searched suffix. The candidate test refuses a pod that is already assigned. |
| Leg routes | `internal/sim/drop_offs.go` `(*Simulation).legRoute`; `(*Simulation).rerouteKeepsDetours`; `internal/sim/bank_detours.go` `(*Simulation).plannedRiderBankDetour`; `internal/sim/dispatch.go` `(*Simulation).board`; `internal/sim/riders.go` `(*Simulation).continueJourney` | `rerouteKeepsDetours` assumes a berth end. `legRoute` checks an entry end with `detourStart.entry`. `continueJourney` returns with no change and no wait reason when no route exists. |
| Route search | `internal/sim/network.go` `networkRouteInput`, `(Network).routeIndexedWithWork`; `internal/sim/route_targets.go` `(Network).routeTargets`; `internal/sim/route_work.go` `(*Simulation).searchRoute`; `internal/sim/bank_routes.go` `(Network).bankRoute`, `(Network).bankNearest` | `routeIndexedWithWork` tests `laneAllows`. `routeTargets` tests it. Bank searches take the graph from their caller. |
| Route graph | `internal/sim/network.go` `routeGraph` | `routeGraph` holds the class masks. |
| Route caches | `internal/sim/routes.go` `(*Simulation).cachedRouteForClass`, `(*Simulation).cacheStationRoutesForClass`, `(*Simulation).stationPathForClass`, `(*Simulation).cacheRoute` | `cachedRouteForClass` returns a cached success or failure before any search. `stationPathForClass` also reads a cache. `cacheStationRoutesForClass` searches several targets at once. |
| Other graph users | `internal/sim/class_routes.go` `(*Simulation).preferredFleetSource`; `internal/sim/positioning.go` `(*Simulation).guardedBumpToDeficit`, `(*Simulation).guardedBumpToParking`; `internal/sim/released.go` `(*Simulation).nearestFreeBerth` | Pass `s.graph` directly. |
| Static graph users | `internal/sim/trip_admission.go` `(*Simulation).SetExpressServices`, `networkStationsConnected`; `internal/sim/station_maneuvers.go` `inferStationLaneRoles` | Express service validation, `networkStationsConnected`, and station maneuver certification. |
| Station connectivity | `internal/sim/class_routes.go` `(*Simulation).stationsConnectedForClass` | `stationsConnectedForClass` uses the route cache. `podFitsRequest` (`internal/sim/trip_admission.go` `(*Simulation).podFitsRequest`) calls it. Order validation calls `podFitsRequest` in `(*Simulation).validateTripOptions`. |
| Stop cache | `internal/sim/drop_offs.go` `(*Simulation).stationsOnRouteForClass` | `routeStations`. `approachStations` is a static index. |
| Berth choice | `internal/sim/berths.go` `(*Simulation).stationRouteByLoad`; `internal/sim/berth_choice.go` `(*Simulation).assignTerminalBerth`, `(*Simulation).reevaluateTerminalBerth`, `(*Simulation).berthAvailableFor`; `internal/sim/berth_continuation.go` `(*Simulation).berthFilterForVehicle` | `stationRouteByLoad` skips a berth without a route. `reevaluateTerminalBerth` (called in `internal/sim/traffic.go` `(*Simulation).admit`) moves an uncommitted passenger or assigned pod to another free berth of the same station when its berth is not available. |
| Detour baselines | `internal/sim/riders.go` `(*Simulation).alight`, `(*Simulation).completeRider`, `(*Simulation).directDistanceForClass`; `internal/sim/rider_detours.go` `(*Simulation).plannedArrivalDetour`; `internal/sim/drop_offs.go` `(*Simulation).plannedBerthDetour`, `(*Simulation).routeMetersForClass`; `internal/sim/bank_detours.go` `(*Simulation).plannedRiderBankDetour` | `directDistanceForClass` and the legacy denominator (`routeMetersForClass` plus the station path) are the direct distances of the detour ratios. |
| Pickup berth fit | `internal/sim/trip_admission.go` `(*Simulation).pickupBerthFitsRequest`; `internal/sim/berth_continuation.go` `(*Simulation).berthFilterForStops`, `(*Simulation).berthFilterForVehicle`; `internal/sim/pickup_estimate.go` `(*Simulation).waitForFinishingPod`, `(*Simulation).finishEstimate` | `pickupBerthFitsRequest` checks the class and the onward route from a berth. `berthFilterForStops` returns nil on a network without class restrictions. `finishEstimate` gives the node where a busy pod becomes available; for a pod with an assigned passenger leg it uses the cached `trip.route` and can return `Berths[0]` without a route. |
| Buffer admission | `internal/sim/station_buffer.go` `(*Simulation).bufferHead`, `(*Simulation).grantBufferedHead`; `internal/sim/station_buffer_claim.go` `(*Simulation).bufferBerthClaims`, `(*Simulation).bufferClaimCanYield` | `grantBufferedHead` tests each candidate berth with its complete station path. The transfer moves only berth claims. `bufferClaimCanYield` refuses a withdrawn head. |
| Dispatch | `internal/sim/dispatch.go` `(*Simulation).dispatch` | Unbinding when `podFitsRequest` fails uses the singular `releasePickup` and clears the cached route and berth only under Express. The pass ends with `parkUnclaimedReleased`. |
| Pickup holds and swaps | `internal/sim/pickup_estimate.go` `(*Simulation).waitForFinishingPod`; `(*Simulation).keepHold`; `internal/sim/pickup_swaps.go` `(*Simulation).swapEligible`; `internal/sim/pickup_reassignment.go` `(*Simulation).reassignPickup` | The finishing-pod hold, the hold refresh, and swaps and transfers. |
| Departure | `internal/sim/riders.go` `departs` | `Boarding`, `DepartingEmpty`, `Continuing`. |
| Berth clearing | `internal/sim/parking.go` `(*Simulation).clearBlockedBerths`; `(*Simulation).startEmptyMove` | Skips a pod that is not in service. Writes `ParkingUnavailable`. |
| Platoons | `internal/sim/platoon.go` `(*Simulation).formPlatoons`, `(*Simulation).canLink`, `(*Simulation).tryLink` | `formPlatoons`, `canLink`, `tryLink`. |
| Compact queues | `internal/sim/station_compact_queue.go` `(*Simulation).compactEntry`, `(*Simulation).formCompactQueues` | `compactEntry`, `formCompactQueues`. |
| Withdrawal | `internal/sim/service_withdrawal.go` `faultHold`, `emergencyHold`, `knownServiceHolds`, `(*Simulation).withdrawService` | The hold bits, `withdrawService` (with the pickup release of the first hold), `restoreService`. |
| Pickup release | `internal/sim/released.go` `(*Simulation).releasePickup`; `(*Simulation).releasePickups` | Singular release for dispatch, bulk release for withdrawal. |
| Incident ID | `internal/sim/incident.go` `(*Simulation).SetIncidentGeneration`, `(*Simulation).nextIncidentID` | `SetIncidentGeneration`, `nextIncidentID`, which increments with no check. |
| Saved state | `internal/sim/state.go` `SavedState`, `SavedPod`, `restoreState`, `(*Simulation).ExportState`; `internal/sim/state_physical.go` `(SavedState).validateCounters` | `SavedState`, `SavedPod` (no wait reason, only `waiting` and `waitSince`), `ExportState`, `savedStart`, `validateCounters`. |
| Clone | `internal/sim/clone.go` `(*Simulation).Clone` | Copies the simulation for checkpoints. |
| State contract | `internal/sim/state_contract.go` `checkPodFlags` | `CheckContract` validates a state. It does not compare two states. `observe` runs the monitor. |
| Reset | `internal/sim/simulation.go` `Simulation` | `Reset`. |
| Session commands | `internal/session/session.go` `Command`, `Reply`, `(*Session).apply`, `sameExceptCouplingEnabled`; `internal/session/receipt.go` `digestCommand` | `Command`, `Reply`, `apply` with its coupling refusal, `sameExceptCouplingEnabled`, `digestCommand`. |
| Demand RNG | `internal/session/demand.go` `newDemand` | The demand PCG. |
| Digest registry | `internal/session/receipt.go` `digestExtensions` | `digestExtensions`, with `N = 1` for `IncidentContract`. |
| Project | `internal/project/config.go` `MaxPods`, `MaxLanes`, `widestDemand`, `Config`, `Validate`; `internal/project/service.go` `scanProjectFields` | `MaxPods = 300`, `MaxLanes = 8000`, the size estimate, `IncidentContract`, `Validate`, the marker scan. |
| Stream | `internal/session/stream_codec.go` `MaxStreamJSON`, `vehicleMetadata`, `frameGroups` | `MaxStreamJSON`, `vehicleMetadata`, the delta groups. |
| Prescan | `internal/session/format_limits.go` `savedLimits`, `streamLimits` | `savedLimits`, `streamLimits`. |
| Protocol | `internal/session/protocol.go` `TopologySnapshot`, `SimulationFrame`, `frameState`, `incidentFrameBinding` | `TopologySnapshot`, `SimulationFrame`, `frameState`, `incidentFrameBinding`. |
| Caps | `internal/session/persist.go` `MaxStateBytes`; `internal/session/http.go` `MaxCommandBytes` | `MaxStateBytes` is 80 MiB. `MaxCommandBytes` is 4 MiB. |
| Readers that refuse the marker | `cmd/compare/main.go` `readProject`; `internal/parkride/foundation.go` `CheckFoundationProject`; `internal/parkride/checkpoint_scan.go` `(*checkpointScanner).object` | Refuse a project with the incident marker. |
| Pod inspector | `internal/view/game.go` `(*Game).inspectionRows` (at 17ab489) | Rows of the selected pod. |
| Web markers | `web/editor.js:1275`, `web/shell.js:147` | `markersAgree` and the incident marker. |

## 4. State

### 4.1 Records

```go
type faultKind uint8

const (
	podFault faultKind = iota
	debrisFault
)

// faultRecord is one active fault. The ID is i<generation>.<serial>.
type faultRecord struct {
	generation, serial uint64
	kind               faultKind
	start              int64 // tick at creation
	end                int64 // 0: until cleared; otherwise clears when tick >= end
	pod                int   // index in s.vehicles, podFault only
	lane               int   // lane index, debrisFault only
	from, to           float64
}
```

`Simulation` gets:

| Field | Saved | Meaning |
| --- | --- | --- |
| `faultsOn bool`, `faultSettings` | In the project | Set from the project marker and the `faults` object. |
| `faults []faultRecord` | Yes | Active records in serial order. |
| `faultCounters` | Yes | Section 12.5. |
| `blocked` | No | The blocked set: `lanes []bool`, `berths` as a resource set, and `by map[resource]string`, the fault ID for each footprint resource. |
| `rerouteDue bool` | No | Section 9.2. |
| `faultReleased []resource` | No | Debris resources released in the fault stage. Empty at every boundary. |
| `staticConnected map[routeKey]bool` | No | Sections 8.4 and 8.5. A cache of the network. |
| `staticRoutes map[routeKey]routeResult` | No | Section 8.5. Static routes for the detour baselines. A cache of the network. |

`vehicle` gets:

| Field | Saved | Meaning |
| --- | --- | --- |
| `faulted bool` | Derived from the records | The pod has a pod fault. |
| `faultCap float64` | Derived | The route distance at which a faulted traveling pod stops. Fixed from onset to clear. |

No field records an obstruction.
Section 9.6 derives each wait report from the current state, so no state needs a cleanup.

`evacuateTick` is `start + evacuationSeconds × 60`.
It is derived and not saved.
The phase of a pod fault is derived:

| Phase | Test |
| --- | --- |
| `braking` | `Pod.Speed > 0` |
| `stopped` | At rest, and the pod has an active rider, or `tick < evacuateTick` |
| `evacuated` | At rest, `tick >= evacuateTick`, and the pod has no active rider |

Debris has no phase.

### 4.2 Owner kind

`resource_owner.go` gets `faultOwnerKind`, after `groupOwnerKind`.
`resourceOwner{kind: faultOwnerKind, id: <fault ID>}` owns debris resources.
`String` returns the fault ID for this kind.
`podID` returns no identity for it, as for a group.
A pod fault owns no resource: the pod keeps its own owners.

### 4.3 Limits

| Limit | Value | Reason |
| --- | ---: | --- |
| Pod faults | One per pod, so at most `MaxPods` (300) | A pod has one mechanical cause (product choice P5). |
| Debris faults | 64 | Bounds the format and the owner checks (product choice P1). |
| Records | 364 | The sum. |
| Debris length | 50 m | Product choice P1. |
| Duration | 1 to 86,400 s | The command range of the parked contract. |
| Serial | Below `math.MaxUint64` | `nextIncidentID` must not wrap (section 5.2). |

## 5. Fault lifecycle

### 5.1 Operations

```go
// startPodFault starts a pod fault on v. duration is 0 for a fault
// without an end.
func (s *Simulation) startPodFault(v *vehicle, duration int64) (string, error)

// startDebris starts a debris fault on a lane segment.
func (s *Simulation) startDebris(lane int, from, to float64, duration int64) (string, error)

// clearFault ends an active fault.
func (s *Simulation) clearFault(id string) error
```

The public entry `Simulation.Fault(FaultRequest) (string, error)` and `Simulation.ClearFault(id string) error` call these at a command boundary and then call `observe`.
Each operation checks every precondition before its first write, and returns an error with no change when one fails (section 11).

### 5.2 Pod fault start

Preconditions, in this order, with the error of section 12.4:

1. `faultsOn`.
2. `duration` is 0 or from 1 to 86,400 seconds.
3. Time and identity arithmetic does not overflow: `s.tick + duration × 60` and `s.tick + evacuationSeconds × 60` fit in `int64`, and `incidentSerial < math.MaxUint64`.
4. The pod exists.
5. The pod has no pod fault.
6. The pod is a supported target (section 1.4): `couplingID == ""`, not `couplingApproachMember`, `follower == 0`, `link.leader == 0`, and not a compact queue member.
7. The activity is `Traveling`, or the pod is at a berth with `Idle`, `Boarding`, `Unloading`, `Continuing`, or `DepartingEmpty`.
8. No dispatch pass is in progress.

Effect, in one call:

1. `id := nextIncidentID()`.
   Append the record with `start = s.tick`, and `end = s.tick + duration × 60` when `duration > 0`.
2. When `v.withdrawn&faultHold == 0`, call `withdrawService(v, faultHold)`.
   The first hold releases the pending pickups of `v` with the exclusion (stage 1, section 5.1).
   When `v` has `faultHold` from its fault recovery, the hold is reused and no second release happens.
3. For each resource of `berthResources(v.destination)` that `v` owns and that `claimKind` classifies as `claimService`, call `releaseOwned` (section 1.6).
4. `v.faulted = true`.
   When `v` is `Traveling`, `v.faultCap = min(v.distance + stoppingDistance(v.Pod.Speed), v.blocks.end(v.reservedThrough))`.
5. `v.pending = -1`.
6. Rebuild the blocked set (section 8.3).
7. Add 1 to `started` (section 12.5).

The grant end bounds the cap, because a pod can always stop inside its grants.
The cap is the stop of controlled braking at `acceleration` (decision 1, section 6.1).
The pod keeps its route, its destination, its grants, its riders, and its purpose.
After effect 3, a faulted pod owns no unused service claim on a remote berth, so each berth that a faulted pod holds is in its footprint or inside its stopping grants.

### 5.3 Debris start

Section 7.3 lists the preconditions, which include the arithmetic check of section 5.2.
Effect, in one call:

1. `id := nextIncidentID()`, and append the record.
2. Set the fault owner on each resource of the footprint.
   Each one is free by precondition.
3. Rebuild the blocked set.
4. Add 1 to `started`.

### 5.4 Clear

`clearFault(id)` preconditions: `faultsOn`, and `id` names an active record.
A record that is no longer active, also one of an earlier generation, gets `unknown fault`.

The record is the mechanical fault.
Its lifetime does not depend on any hold or purpose: the clear removes it at once, also when the pod still needs `faultHold` for a fault recovery.

| Kind | Effect |
| --- | --- |
| Pod | Remove the record. `v.faulted = false` and `v.faultCap = 0`. When `v.op.owner != faultHold`, call `restoreService(v, faultHold)`. Otherwise the hold stays for the fault recovery, and the hold release rule releases it after the recovery arrives (section 5.6). |
| Debris | Remove the record. Release every resource that the fault owns. At a command boundary the release is immediate. In the fault stage the resources go to `faultReleased`, and `releaseCleared` releases them at the end of the tick. |
| Both | Rebuild the blocked set. For each pod whose `BlockedBy` is the cleared fault ID, set the wait reason to `NoWait` and `BlockedBy` to empty. Add 1 to `cleared`. |

The reset of `BlockedBy` keeps a paused command boundary from publishing the ID of a removed record.
The next admission derives each wait again (section 9.6).

A pod that is braking at the clear has no cap after it.
It continues inside its grants with the ordinary step.
The ordinary step brakes or cruises inside the same grants, so this transition needs no proof beyond the existing one.
A pod at rest with riders aboard is admitted again by `admit` and continues its route.
A pod at a berth runs its phase timer again and departs as usual.
An evacuated pod at a berth is idle, and in service once its hold is released.
An evacuated pod on a lane continues its original route empty, under purpose 3 (stage 1, section 9.5).
It does not search for a free berth.

### 5.5 Fault stage

The fault stage is the one policy place in `Step` that stage 1 allows (stage 1, section 10).
It runs after the unloading loop (`internal/sim/simulation.go` `(*Simulation).Step`) and before `dispatch`, and only when `faultsOn`.
Its steps, in this order:

1. Clear each record with `end != 0 && end <= s.tick`, in serial order.
2. Evacuate each faulted pod with `s.tick >= evacuateTick`, at rest, and with an active rider, through `evacuate(v)` (stage 1, section 8.2).
   Add 1 to `evacuations`.
3. Run the reroute pass when it is due (section 9.2).
4. Apply the hold release rule (section 5.6).
5. For each healthy pod whose wait reason is "Blocked by incident" or "No forward route", add 1 to `faultWaitTicks`.
   These are the reasons of the previous tick (section 9.6).

Step 1 runs before step 2, so a fault with `end <= evacuateTick` never evacuates, and a clear wins a same-tick tie with evacuation.
Step 2 needs rest, so evacuation never starts before rest (decision 4).
A pod that reaches rest after `evacuateTick` is evacuated in the first fault stage after it reaches rest.

### 5.6 Hold release rule

`faultHold` is released when the pod has no pod fault, `v.op.owner != faultHold`, and the pod is not a coupling or approach member.
The release calls `restoreService`.
The hold is not released while it owns a purpose, so W5 holds at every step.
An empty recovery releases its hold in the first fault stage after its arrival clears the purpose.

## 6. Motion suspension

### 6.1 Fault braking step

```go
// faultMoveStep is the motion step of a faulted pod. It runs the ordinary
// kernel with limit min(limit, cap), and the commanded speed never exceeds
// speed.
func faultMoveStep(blocks *blockList, lane int, distance, speed, limit, cap float64) ordinaryMoveResult
```

`move` (`internal/sim/traffic.go` `(*Simulation).move`) calls `faultMoveStep` in place of `ordinaryMoveStep` when `v.faulted`.
The native foreign fact (`internal/sim/coupling_native_foreign_fleet.go` `nativeForeignFact`) gets two members, `faulted` and `faultCap`.
`nativeForeignLimit`, the proof prediction (`internal/sim/coupling_native_foreign_proof.go` `(*nativeForeignTick).prepareProof`), and the raw check (`checkNativeForeignSweep`) call `faultMoveStep` with the fact values when `fact.faulted`.
The proof checks the two members in the same way that `checkPredecessorCap` checks the follower cap.

Deceleration bound.
At onset, `available = cap - distance = stoppingDistance(v)` for speed `v`, unless the grant end is nearer.
The kernel gives `safe = sqrt(a²dt² + v²) - a·dt`.
`sqrt(a²dt² + v²)` lies between `v` and `v + a·dt`, so `v - a·dt <= safe <= v`.
The speed therefore falls by at most `acceleration × dt` in a tick, and never rises.
`safe` solves `safe·dt + safe²/(2a) = available`, so after the step the remaining distance is `stoppingDistance(safe)`, and the same bound holds at the next tick.
A lower speed limit of a lane ahead (`speedBeforeLane`) can lower the command further, as it does for every pod.
The ceiling at `speed` removes a rounding rise above `v`.
The snap at `1e-5` brings the pod to rest at the cap.

Invariant F3 (section 10): the cap is set only at onset, which is at a command boundary or in the fault stage, both before `planNativeCouplingTick` (`internal/sim/simulation.go` `(*Simulation).Step`) captures the facts.
A coupling train next to a braking faulted pod therefore sees the same step that the pod takes.
A test runs a train beside a braking faulted pod and checks that the prediction equals the publication at every tick.

### 6.2 Gates

Each gate tests `v.faulted`.
Each one is false for every pod when no record exists, so off-state behavior does not change.
No gate tests `v.withdrawn`: a pod in its fault recovery is withdrawn, and it must move to finish purpose 3 (stage 1, section 9.5).

| Path | Anchor | Gate |
| --- | --- | --- |
| Admission | `internal/sim/traffic.go` `(*Simulation).admit` | Skips a faulted pod, after the coupling skip, and writes its wait report (section 9.6). |
| Buffer head | `internal/sim/station_buffer.go` `(*Simulation).bufferHead`, `(*Simulation).grantBufferedHead` | A faulted pod makes no berth-grant attempt as a buffer head. The pods behind it in the buffer cannot pass it, because its retained tail and lane position keep the queue order. |
| Phase timer and unloading | `internal/sim/simulation.go` `(*Simulation).Step` | Skips a faulted pod. `phaseTicks` does not run down, and `alight` does not run. |
| Departure | `internal/sim/simulation.go` `(*Simulation).Step` | A faulted pod does not depart. |
| Motion | `internal/sim/traffic.go` `(*Simulation).move` | `faultMoveStep` (section 6.1). |
| Platoon links | `internal/sim/platoon.go` `(*Simulation).canLink`, `(*Simulation).tryLink` | `canLink` and `tryLink` refuse a faulted pod as leader and as follower, and refuse a link whose run has a blocked lane. |
| Compact entry | `internal/sim/station_compact_queue.go` `(*Simulation).compactEntry` | `compactEntry` refuses a faulted pod. |
| Approach discovery | `internal/sim/coupling_approach_runtime.go` `(*Simulation).discoverCouplingApproaches` | Skips a pair with a faulted pod, and a pair whose remaining route has a blocked lane. |
| Berth clearing | `internal/sim/parking.go` `(*Simulation).clearBlockedBerths` | A faulted pod is withdrawn, so the existing test skips it. |

A faulted pod at a berth that is `Unloading` keeps its riders until the clear or the evacuation.
A faulted pod that is `Boarding` does not depart.
This is decision 1 at a berth: the pod stops where it is.

### 6.3 Arrival during braking

A braking pod whose cap equals the end of its route arrives through `arrive` as usual.
The fault stays: `faulted` and the record do not change.
The gates of section 6.2 then hold the pod at the berth, and the unloading gate suppresses ordinary alighting.
`arrive` rebuilds the blocked set before it returns, because the footprint changes from the lane to the berth (section 6.5).

### 6.4 Grants at rest

A faulted pod keeps every grant that it owns, at rest and while braking.
It releases passed resources only at their release distances, through the existing release boundary.
A faulted pod at rest stays `Traveling`, because a pod that is not traveling releases its track claims (`internal/sim/traffic.go` `(*Simulation).releaseVehicleResources`).
A buffered faulted pod keeps its buffer membership and its claims.

No stage 2 operation revokes a grant.
A release of the unused grants of a stopped pod needs a certified physical transition, which stage 2 does not define (product choice P9).
The cost is that pods behind a faulted pod cannot use the track inside its unused grants until the clear.
The blocked set already routes them around it.

### 6.5 Pod fault footprint

| Pod state | Footprint |
| --- | --- |
| `Traveling` | `v.footprint(v.reservedThrough, v.faultCap)`: each resource of the granted blocks that the pod holds at its cap. `reservedThrough` and the cap do not change while the fault lasts, so the footprint is fixed. |
| At a berth | `berthResources(berth)` (`internal/sim/state_physical.go` `berthResources`). The berth is blocked. |

The footprint is computed from the pod state and is not saved.
The pod owns every resource of its footprint, because the footprint is inside its grants.
A pod fault in a station entry queue blocks the entry lane, so the pods behind it change route or wait (decision 7, section 9).

## 7. Debris

### 7.1 Model

Debris is an obstacle on an empty lane segment.
It has no body and no motion.
It owns the resources of its footprint through the fault owner (section 4.2), so admission refuses them to every pod.
Debris never revokes an existing claim: it starts only on resources that are free and that no pod claims.

### 7.2 Footprint

The footprint `F` of debris on lane `l` with segment `[from, to]` is the union of the resources of each cell of `l` that meets `[from - Clearance, to + Clearance]`.
The cells come from the shared lane cells (`internal/sim/traffic.go` `newLaneCells`).
A cell at a lane end includes its node and junction resources (`newLaneCells`), so debris near a junction blocks the junction and each lane that shares it.
`F` is a function of the network and the segment.
It is computed again at restore and is not saved.

### 7.3 Start preconditions

In this order, with the error of section 12.4, all before the ID is taken and before any owner write:

1. `faultsOn`.
2. `duration` is 0 or from 1 to 86,400 seconds.
3. The arithmetic check of section 5.2, precondition 3.
4. The lane exists.
5. `from` and `to` are finite, `0 <= from < to <= length`, and `to - from <= 50`.
   `F` contains no berth resource and no berth node resource.
6. Fewer than 64 debris faults are active.
7. `F` meets no footprint of an active fault.
8. Each resource of `F` has no owner of any kind, and no pod has a `routeReleases` entry for it.
   A resource under a pod body gives `debris overlaps a pod or another fault`.
   Any other claimed resource gives `debris meets a reserved resource`.
9. No coupling group claim or preserved claim names a resource of `F`.
   No coupling member and no approach member has a lane of its remaining route that meets `F`.
   The remaining route runs from the current lane to the end of the route, also past the grants, and includes the corridor that an approach binds (`internal/sim/coupling_approach_context.go` `prepareCouplingApproach`).
   Otherwise the fault is not supported in stage 2.
10. No dispatch pass is in progress.

Decision 2 follows from precondition 8.
A pod can always stop inside its grants, and no pod has a grant in `F`, so no pod has `F` inside its stopping distance.
The cells of `F` extend `Clearance` past the segment, so a pod that stops at its grant end keeps its clearance from the debris.

Precondition 8 refuses more than decision 2 needs: it also refuses a claim beyond the stopping distance.
This avoids any revocation in stage 2 (product choice P1).

### 7.4 Ownership

At start, every resource of `F` goes to the fault owner.
No pod owns a resource of `F` while the debris is active, because admission refuses a resource with another owner (`internal/sim/traffic.go` `(*Simulation).grant`).
So `releaseRouteResource` (`internal/sim/platoon.go` `(*Simulation).releaseRouteResource`) never frees a resource of `F`, and the buffer transfer (`internal/sim/station_buffer.go` `(*Simulation).grantBufferedHead`) moves only berth claims, which `F` does not have.

### 7.5 Clear

Section 5.4 defines the clear.
The released resources become free at the release boundary of the tick, or at once at a command boundary.
A pod that waits for them is admitted at the next `admit`.

### 7.6 Restore and retained owners

`retainedOwners` (`internal/sim/ownership.go` `(*Simulation).retainedOwners`) gets one more step: for each debris record, each resource of `F` gets the fault owner.
`verifyRestore` (`internal/sim/state_physical.go` `(*Simulation).verifyRestore`) then covers debris ownership.
Physical restore places the debris first and the pods after it.
A restored pod whose ownership meets `F` is a conflict, and the save is invalid.
A saved debris record whose segment fails precondition 5 against the saved project, or whose footprint meets another record, also makes the save invalid (section 13.5).

## 8. Blocked routing

### 8.1 Blocked set

A lane is blocked when any resource of any cell of the lane is in an active fault footprint.
A berth is blocked when its berth resource or its berth node resource is in an active fault footprint.
A berth with a faulted pod is therefore blocked, and a lane that shares a junction with a footprint is blocked.

The simulation keeps a static index from each resource to the lanes whose cells hold it.
It is built once from the lane cells, with the network indexes (`internal/sim/routes.go` `(*Simulation).ensureNetworkIndexes`), and it is a cache of the network.

`blocked.by` maps each footprint resource to the fault ID.
The wait reports of section 9.6 use it.

### 8.2 Route searches

`routeGraph` (`internal/sim/network.go` `routeGraph`) gets `blocked []bool`, indexed by lane.
It is nil in the static graph.
`laneOpen(lane)` returns `blocked == nil || !blocked[lane]`.
`routeIndexedWithWork` tests it beside `laneAllows` (`internal/sim/network.go` `(Network).routeIndexedWithWork`), and `routeTargets` tests it beside `laneAllows` (`internal/sim/route_targets.go` `(Network).routeTargets`).
Bank searches (`internal/sim/bank_routes.go` `(Network).bankRoute`, `(Network).bankNearest`) take the graph from their caller and call `routeIndexedWithWork`, so they get the same test.
The last cell of the lane into a berth holds the berth node resource, so the lane into a blocked berth is blocked, and no route ends at a blocked berth.

`s.routingGraph()` returns a copy of `s.graph` with `blocked` set to the current blocked lanes.
With no active fault, it returns `s.graph` unchanged.

| Reader | Anchor | Graph |
| --- | --- | --- |
| `searchRoute`, and through it `cachedRouteForClass`, `assignedRoute`, `congestionRouteForClass`, `stationPathForClass`, `legRoute` | `internal/sim/route_work.go` `(*Simulation).searchRoute` | `routingGraph` |
| `cacheStationRoutesForClass`, the direct multi-target search | `internal/sim/routes.go` `(*Simulation).cacheStationRoutesForClass` | `routingGraph` |
| Class route searches | `internal/sim/class_routes.go` `(*Simulation).preferredFleetSource` | `routingGraph` |
| Positioning searches | `internal/sim/positioning.go` `(*Simulation).guardedBumpToDeficit`, `(*Simulation).guardedBumpToParking` | `routingGraph` |
| `nearestFreeBerth` | `internal/sim/released.go` `(*Simulation).nearestFreeBerth` | `routingGraph`. It also skips blocked berths. |
| `stationRouteByLoad` | `internal/sim/berths.go` `(*Simulation).stationRouteByLoad` | Skips blocked berths. Its searches use `routingGraph`. |
| Arrival-chain test of `divertStart` | `internal/sim/diversion.go` `(*Simulation).divertStart` | `s.graph`, static (section 8.5) |
| `directDistanceForClass` | `internal/sim/riders.go` `(*Simulation).directDistanceForClass` | `s.graph`, static (section 8.5) |
| Legacy detour denominator: `routeMetersForClass` and the station path of the direct distance | `internal/sim/drop_offs.go` `(*Simulation).plannedBerthDetour`, `(*Simulation).routeMetersForClass` | `s.graph`, static (section 8.5) |
| Express service validation | `internal/sim/trip_admission.go` `(*Simulation).SetExpressServices` | `s.graph`, static |
| `networkStationsConnected` | `internal/sim/trip_admission.go` `networkStationsConnected` | `s.graph`, static |
| Station maneuver certification | `internal/sim/station_maneuvers.go` `inferStationLaneRoles` | Static |
| Network bounds, pickup lower bounds, and validation | Unchanged | `s.graph`, static |

The blocked set is per simulation.
It is not written into the shared prepared network.

### 8.3 Rebuild and caches

The rebuild is synchronous.
Each operation that changes a footprint rebuilds the blocked set and clears the caches before it returns: pod fault start, debris start, each clear, and `arrive` of a faulted pod.
No reader can see a cached route of an earlier epoch, also a cached failure, because the clear happens inside the operation that changes the set.

The rebuild computes the footprint of each record (sections 6.5 and 7.2) and then the blocked lanes and berths.
When the result differs from the previous blocked set, a new epoch starts:

- Clear `routes`, `routeOrder`, `congestionRoutes`, `congestionRouteCosts`, and `routeStations` (`internal/sim/routes.go` `(*Simulation).ensureNetworkIndexes`, `internal/sim/drop_offs.go` `(*Simulation).stationsOnRouteForClass`).
  These hold successes and failures alike.
- Set `rerouteDue = true`.

`staticConnected`, `staticRoutes`, and `pickupBounds` stay.
They depend only on the network: `pickupBounds` are free-flow lower bounds, and a blocked lane can only raise a route cost.
`approachStations` is a static index and stays.
Trip routes that dispatch has stored are not checked: `board` computes the leg route again (`internal/sim/dispatch.go` `(*Simulation).board`).
The dispatch pass caches are rebuilt at each pass (`internal/sim/dispatch.go` `(*Simulation).dispatch`).

A footprint changes at no other event: a braking pod keeps a fixed cap and fixed grants, and debris is fixed.

### 8.4 Order admission stays static

`podFitsRequest` (`internal/sim/trip_admission.go` `(*Simulation).podFitsRequest`) decides order admission (`(*Simulation).validateTripOptions`) and the dispatch reason through `stationsConnectedForClass` (`internal/sim/class_routes.go` `(*Simulation).stationsConnectedForClass`).
With a blocked route cache, a fault would refuse new orders and unbind bound trips (`internal/sim/dispatch.go` `(*Simulation).dispatch`).
So while the blocked set is not empty, `stationsConnectedForClass` reads `staticConnected`.
`staticConnected` caches, by origin berth, destination berth, and route class, whether `s.graph` has a route.
It uses the same two searches as `cachedRouteForClass` (`internal/sim/routes.go` `(*Simulation).cachedRouteForClass`).
With an empty blocked set, `stationsConnectedForClass` uses the route cache as today.
The two give the same answer when no lane is blocked, so the switch changes no result.
The switch reads the blocked set that the last rebuild wrote, so it changes in the same operation as the caches.

A trip to a station that a fault cuts off is accepted and waits.
Dispatch assigns it only to a pod with a route, so it stays pending until a route exists.
Pickup access for an existing binding is a separate test, used only while the blocked set is not empty (section 9.4).

### 8.5 Structural checks stay static

Three checks decide structure or a baseline, not travel, and they read the static graph:

- Order admission connectivity (section 8.4).
- The arrival-chain test of `divertStart` (`internal/sim/diversion.go` `(*Simulation).divertStart`).
  It asks whether the remaining endpoint of a restored route lies inside the arrival chain of its station.
  It calls a static form of `stationPathForClass` that searches `s.graph` and caches the result in `staticConnected`, under the station form of `routeKey`.
  A blocked lane must not turn this safety check into permission to divert.
- The rider detour baselines.
  `directDistanceForClass` (`internal/sim/riders.go` `(*Simulation).directDistanceForClass`) and the legacy denominator of `plannedBerthDetour` (`routeMetersForClass` in `internal/sim/drop_offs.go` `(*Simulation).plannedBerthDetour` and the station path in `direct+meters`) measure the trip that the rider ordered.
  While the blocked set is not empty, they search `s.graph` and cache the result in `staticRoutes`.
  A bypass must not raise the denominator: otherwise a 250 m bypass of a 100 m direct route scores near 1 and passes the 1.5 limit.
  This covers each caller: the planned detours (`internal/sim/rider_detours.go` `(*Simulation).plannedArrivalDetour`, `internal/sim/bank_detours.go` `(*Simulation).plannedRiderBankDetour`, `internal/sim/drop_offs.go` `(*Simulation).plannedBerthDetour`) and the realized detour at alighting (`internal/sim/riders.go` `(*Simulation).alight`, `(*Simulation).completeRider`).
  The arrival distance, the candidate travel, and the onward feasibility stay operational.

Every other search is an operational search and uses `routingGraph`, as section 8.2 lists.
With an empty blocked set, each static form and its cached form give the same answer, so the change has no off-state effect.

## 9. Obstructed pods

### 9.1 Rules

- Stage 2 never withdraws a healthy pod and gives it no hold.
- The route of a healthy pod changes for an incident only through the endpoint reroute (section 9.3).
  Ordinary terminal reevaluation is exempt: it stays active and can move an uncommitted pod to another free, reachable berth of the same station (section 9.3).
- A pickup bound to a healthy pod ends for an incident only through dispatch (section 9.4).
- A healthy pod gives up a claim for an incident only through its own surrender of an unused service claim (section 9.5).
- Each wait report is derived again at each tick (section 9.6).
- Recovery after the last clear is live only under the conditions of section 9.7.
- Riders aboard a healthy pod leave it only at their stops.
  Only a pod fault evacuates riders.

The rules keep no state for each pod, so no later event has to clear one.

### 9.2 Reroute pass

The fault stage runs the reroute pass when `rerouteDue` is set, and every 300 ticks while the blocked set is not empty (product choice P2).
A new epoch and a physical restore set `rerouteDue`.
The pass clears it.
300 ticks is the cadence of `refreshCongestionCosts` (`internal/sim/routes.go` `(*Simulation).refreshCongestionCosts`).
The cadence retries the pods that the last pass skipped: a berth departure that admission had already granted, and a pod that was a platoon or compact member.
After the last clear, the blocked set is empty, no remaining route has a blocked lane, and the pass has no work.

The pass visits the pods in pod ID order.
It visits a pod when all of these hold:

- The pod is healthy.
  A pod in its fault recovery is healthy, and the pass visits it.
- The pod is not a coupling member, an approach member, a platoon member, or a compact member.
- The pod is `Traveling` or `DepartingEmpty`, or it is `Boarding` or `Continuing` at a berth with `reservedThrough < 0`.
- Its remaining route, from its current lane to its endpoint, has a blocked lane.

For each visited pod, the pass calls `rerouteToEndpoint(v)`.
A skipped platoon or compact member waits behind the blocked set, and stage 6 adds its reroute.
A skipped coupling or approach member waits behind a faulted pod as it waits behind any stopped pod, and stage 4 adds its reroute.
Debris never meets its claims or its remaining route, by precondition 9 of section 7.3.
The cost of a pass is one endpoint evaluation for each visited pod.
Each evaluation runs a bounded number of searches: the free-flow, policy, bank, and detour searches.
Pickup access can also evaluate an endpoint between passes.

### 9.3 Endpoint reroute

```go
// endpointRoute returns a forward route for v to the endpoint of its
// current route that avoids the blocked set. The route starts with the
// lanes that v must keep. It writes nothing.
func (s *Simulation) endpointRoute(v *vehicle) ([]Lane, bool)

// rerouteToEndpoint installs the route of endpointRoute. It changes only
// the route and the indexes derived from it.
func (s *Simulation) rerouteToEndpoint(v *vehicle) bool
```

`endpointRoute` steps:

1. Start.
   For `Traveling` and `DepartingEmpty`, `prefix, from, ok := s.divertStart(v)` (`internal/sim/diversion.go` `(*Simulation).divertStart`).
   When `divertStart` refuses, the pod is in its arrival chain or in a group, and the result is false.
   For `Boarding` and `Continuing` at a berth with `reservedThrough < 0`, `prefix = 0` and `from = v.origin.Node`, the current berth, as the first branch of `divertStart` gives for a pod with no grant.
2. Kept prefix.
   `kept := v.Route[:prefix]`, the committed prefix from route index 0.
   When a lane of `kept` from the current lane on is blocked, the result is false: the pod is trapped, and stage 5 adds reverse motion.
3. Target.
   When the route ends at the destination berth, the target is that berth node.
   Otherwise the route ends at a station entry, and the target is that entry node.
   The target never changes, so the endpoint kind, the destination berth, and the destination station stay.
4. Suffix.
   `suffix` is the route from `from` to the target by `assignedRoute` (`internal/sim/routes.go` `(*Simulation).assignedRoute`), which searches `routingGraph` (section 8.2).
   When the target is a blocked berth, no route ends at it, and the result is false.
5. Route.
   `route := append(slices.Clone(kept), suffix...)`.
6. Checks.
   No lane of `route` from the current lane on is blocked.
   For a pod with riders, the rider detour limits hold for the actual endpoint.
   A berth end uses the test of `rerouteKeepsDetours` (`internal/sim/drop_offs.go` `(*Simulation).rerouteKeepsDetours`) with the destination berth.
   An entry end uses the same test with `detourStart.entry = station.routeEntry(route, Berth{})` and no berth, as `legRoute` builds it (`(*Simulation).legRoute`), so a banked station checks the entry of the new route (`internal/sim/bank_detours.go` `(*Simulation).plannedRiderBankDetour`).
   The detour baselines of the test are static (section 8.5).
   For a buffered pod, the last lane of `route` is the last lane of `v.Route`.
   `setVehicleRoute` clears `buffered` and `bufferBerth` when the last lane changes (`internal/sim/traffic.go` `(*Simulation).setVehicleRoute`), so a route that reaches the same entry through another last lane is refused.
   When a check fails, the result is false.

`rerouteToEndpoint(v)` calls `setVehicleRoute(v, route)` (`internal/sim/traffic.go` `(*Simulation).setVehicleRoute`), sets `v.pending = -1`, and adds 1 to `reroutes`.
It writes nothing else.
The pod keeps its activity, its phase state with `phaseTicks` and `StationPhase`, `reservedThrough`, its distance, `routeReleases`, its owners, its riders, `Stops`, `RelocatingTo`, `Rebalancing`, `released`, its purpose, its pickup bindings, and its buffer membership.
The prefix keeps the same lanes, so the blocks up to `reservedThrough` and their owners stay valid, as they do for `redirect`.
When `endpointRoute` fails, the pod keeps its route, admission stops it at the boundary of the blocked set, and the wait is reported (section 9.6).

The reroute does not use `redirect` (`internal/sim/diversion.go` `(*Simulation).redirect`).
`redirect` releases destination claims, sets `RelocatingTo`, and clears `Rebalancing`, and `arrive` then treats an occupied pod as an empty move and makes it idle with its riders aboard (`internal/sim/simulation.go` `(*Simulation).arrive`).

The endpoint reroute never changes the berth.
Ordinary terminal reevaluation stays active during incidents.
`reevaluateTerminalBerth` (`internal/sim/berth_choice.go` `(*Simulation).reevaluateTerminalBerth`, called in `internal/sim/traffic.go` `(*Simulation).admit`) can move an uncommitted passenger or assigned pod whose berth is not available to another free berth of the same station.
Its station path comes from `stationPathForClass` on `routingGraph`, and `berthAvailableFor` refuses a blocked berth, because the fault owns the berth resources.
The pod keeps its destination station, its riders, and its `Stops`, and only the berth changes, as it does today.
A pod waits with "Blocked by incident" only when no reachable, compatible berth of its station is free.

### 9.4 Pickup access

```go
// pickupAccess reports whether pod v, bound to request, has an executable
// forward continuation to a berth of request.legOrigin(). It is true when
// the blocked set is empty.
func (s *Simulation) pickupAccess(v *vehicle, request Request) bool
```

With `origin := request.legOrigin()` (stage 1, section 7.1), the test uses these terms:

- The current route of the pod is executable when its remaining route has no blocked lane, or when `endpointRoute(v)` gives a route.
- A compatible pickup berth is a berth `b` of `origin` that is not blocked, that `berthFilterForStops(class, []string{request.To})` (`internal/sim/berth_continuation.go` `(*Simulation).berthFilterForStops`) accepts, and for which `pickupBerthFitsRequest(v, request, b)` (`internal/sim/trip_admission.go` `(*Simulation).pickupBerthFitsRequest`) holds.
  Its onward route to `request.To` uses `routingGraph`, so the onward feasibility is operational.
- A reachable end berth of the pod is a berth where its current work can end:
  - for an executable route with a berth end, the destination berth, when it is not blocked;
  - for an executable route with an entry end, each berth of the destination station that is not blocked, that the berth filter of the pod accepts, that the entry serves (on a banked station, `station.berthEntry(berth)` is that entry), and that has a station path from the entry on `routingGraph`;
  - for a route that is not executable, none.

The rules in order:

1. The blocked set is empty: true.
2. The pod is `Idle` at a berth of `origin`: true.
   A blocked onward leg stays a destination access wait in `board`, as the boarding gate below says.
3. The pod travels to `origin` (`RelocatingTo == origin`): true when a reachable end berth is a compatible pickup berth.
   An open route to an entry passes only when the entry serves a compatible pickup berth that is not blocked.
   An open existing route passes also when `divertStart` refuses a diversion.
4. Otherwise, the pod is idle at another station or finishes other work first.
   It is true when the current route, if any, is executable and a route on `routingGraph` exists from the node where the pod becomes available to a compatible pickup berth.
   That node is the current berth node of an idle pod, and the node of `finishEstimate` (`internal/sim/pickup_estimate.go` `(*Simulation).finishEstimate`) for a busy pod, which reaches a berth that is not blocked through `stationRouteByLoad` and includes the later stops.
   The search is `stationRouteByLoad` (`internal/sim/berths.go` `(*Simulation).stationRouteByLoad`) to `origin` with `noBerthLoad` and the compatible pickup berths as its filter.
   For a pod with an assigned passenger leg, rule 4 does not trust the two shortcuts of `finishEstimate` (`internal/sim/pickup_estimate.go` `(*Simulation).finishEstimate`).
   Its continuation check searches the passenger leg on `routingGraph`, from the berth of that leg's origin to `request.To` of that leg, in place of the cached `trip.route`.
   It takes the final berth from `stationRouteByLoad` with `noBerthLoad`, which skips a blocked berth, in place of `Berths[0]`.
   When either search fails, rule 4 is false.
   The check runs only while the blocked set is not empty, and `finishEstimate` does not change, so the off-state branch stays byte-identical.
5. Otherwise: false.

The test asks only whether a complete forward continuation exists.
It does not look at resource owners or at the time to arrive, so ordinary traffic, an occupied berth, and a worse arrival time never unbind a trip.

Dispatch unbinding.
In `dispatch` (`internal/sim/dispatch.go` `(*Simulation).dispatch`), the unbinding condition becomes `!podFitsRequest(v, request) || !pickupAccess(v, request)`.
When only `pickupAccess` fails, the unbinding:

- calls the singular `releasePickup(v)` (`internal/sim/released.go` `(*Simulation).releasePickup`), as the existing branch does;
- clears `request.PodID`, `trip.route`, and `trip.destination` under both order contracts (the existing branch clears the last two only under Express);
- keeps the order ID, its queue position, its deferral budget, and an existing `excludedPod`;
- creates no exclusion, and calls neither `withdrawService` nor `releasePickups`.

The pod stays in service.
The same pass can bind the trip to another pod, or to the same pod through a route that the gates below accept.
A pickup pod whose pickup berth is blocked fails rule 3, because that berth is its endpoint.
Dispatch then unbinds the trip, and ordinary dispatch can send the same empty pod to another berth of the station through `sendPickupMatching`, which redirects an empty pod.

Compatible pickup berths in assignment.
While the blocked set is not empty, each pickup search and each pickup installation accepts only compatible pickup berths.
`pickupBerthFilter(v, request)` returns `berthFilterForStops(class, []string{request.To})` unchanged when the blocked set is empty.
Otherwise it returns a filter that also requires a berth that is not blocked and that passes `pickupBerthFitsRequest`, also on a network without class restrictions, where `berthFilterForStops` returns nil (`internal/sim/berth_continuation.go` `(*Simulation).berthFilterForStops`).
It replaces the pickup filter at each site:

- candidate selection, `candidateRouteForRequest` (`internal/sim/berth_continuation.go` `(*Simulation).candidateRouteForRequest`);
- installation, `sendPickupForRequest` (`internal/sim/diversion.go` `(*Simulation).sendPickupForRequest`), so the berth that `sendPickupMatching` installs is compatible;
- the berth filter of an assigned pickup pod in `berthFilterForVehicle` (`internal/sim/berth_continuation.go` `(*Simulation).berthFilterForVehicle`), which terminal reevaluation uses, also without class restrictions;
- the finishing-pod hold searches (`internal/sim/pickup_estimate.go` `(*Simulation).waitForFinishingPod`);
- `assignedPickupFitsRequest` (`internal/sim/trip_admission.go` `(*Simulation).assignedPickupFitsRequest`).

A pod idle at a berth of `origin` keeps the exception of rule 2: local pickups and boarding do not use the filter, and a blocked onward leg stays a destination access wait.
So a pod that fails rule 3 cannot win the same trip again through a berth that fails the predicate.

Consistent gates, each true when the blocked set is empty:

| Path | Anchor | Gate |
| --- | --- | --- |
| New assignments | `internal/sim/diversion.go` `(*Simulation).pickupCandidate`; `(*Simulation).candidateRoutePartsMatching` | A candidate whose `divertStart` prefix has a blocked lane from the current lane on is refused. The suffix comes from `routingGraph` and ends at a compatible pickup berth. |
| Finishing-pod holds | `internal/sim/pickup_estimate.go` `(*Simulation).waitForFinishingPod`; `(*Simulation).keepHold` | The held pod must pass `pickupAccess`. |
| Swaps and transfers | `internal/sim/pickup_swaps.go` `(*Simulation).swapEligible`, which `reassignPickup` (`internal/sim/pickup_reassignment.go` `(*Simulation).reassignPickup`) uses | The receiving pod must pass `pickupAccess`. |
| Boarding | `internal/sim/dispatch.go` `(*Simulation).board` | The pod is at the origin, so access holds. Its leg route comes from `routingGraph`. A failed leg keeps the trip with "Waiting for destination access", as today. |

These gates keep dispatch from binding the trip again to the same inaccessible pod.
With an empty blocked set, `pickupAccess` and every gate are true, so the dispatch decisions do not change.

### 9.5 Claim surrender

Gate.
`incidentOutstanding()` is true when an active record exists or a pod has `faultHold`.
It is false without the marker, and false with the marker and no fault.
After the last clear, a pod in its fault recovery keeps `faultHold` until its arrival clears purpose 3 and the hold release rule releases the hold, so the gate stays on through the recovery.

Place.
The surrender is the first phase of `admit` (`internal/sim/traffic.go` `(*Simulation).admit`), and it runs only when the gate is on.
It is not a new place in `Step`, and it calls no stage 1 operation.
It follows every claim producer of the tick: `dispatch` with its parking at the end of the pass (`internal/sim/dispatch.go` `(*Simulation).dispatch`), `swapPickups`, `redistribute` with `parkReleased` (`internal/sim/redistribution.go` `(*Simulation).yieldRelocationClaims`) and guarded positioning, and `formPlatoons`, which writes no berth claim.
No parking claim runs between the surrender and the grants of the same `admit`.
`clearBlockedBerths` runs after `admit` (`internal/sim/simulation.go` `(*Simulation).Step`), so its claims meet the surrender of the next tick.

First phase, for each pod in pod ID order:

1. Skip a faulted pod, because fault start has given up its service claims (section 5.2).
   Skip a coupling member, an approach member, a platoon member, and a compact member.
2. Skip the pod unless it owns a resource of `berthResources(v.destination)` that `claimKind` classifies as `claimService`.
   Only an empty pod with a relocation qualifies.
3. Derive whether the pod waits.
   Build the request that the second phase would build for the pod, with no write and no arbitration, and test it against the current owners.
   The pod waits when one of these holds:
   - A resource of the span that it would request has an owner other than the pod.
   - Its terminal berth choice finds no berth, by a form of `assignTerminalBerth` (`internal/sim/berth_choice.go` `(*Simulation).assignTerminalBerth`) that writes nothing.
   - It is a buffer head, and no candidate berth of `grantBufferedHead` (`internal/sim/station_buffer.go` `(*Simulation).grantBufferedHead`) has a complete station path whose resources are free of other owners.
   The derivation needs no incident owner: a pod behind a healthy pod that waits at a fault also waits.
4. When the pod waits, call `releaseOwned` for each such `claimService` resource.
   A pod in its fault recovery uses the amendment of section 1.6.

The second phase is the existing admission, with terminal reevaluation (`internal/sim/traffic.go` `(*Simulation).admit`), against the new ownership.

The surrender keeps the route, the destination, `RelocatingTo`, and every other claim and grant.
The saved `ClaimsDestination` flag records the result, as it records a yield of `yieldRelocationClaims`, so `claimDestinations` restores no surrendered claim.
The buffer rollback of `grantBufferedHead` and the rules of `bufferClaimCanYield` (`internal/sim/station_buffer_claim.go` `(*Simulation).bufferClaimCanYield`) do not change.
A withdrawn head still makes no other pod yield: a berth becomes free only by the choice of its own owner.

The deadlock of the second review (finding N1) ends as follows.
A buffered head faults before it claims a berth, is evacuated, and clears.
Its purpose 3 then needs the only reachable berth.
An empty pod behind the head owns an unused claim on that berth.
The empty pod waits behind the head, so the first phase of the next `admit` gives up its claim.
The second phase then grants the berth to the head through the complete-path grant.
Neither pod takes a grant from the other.

Parking again after a surrender.
A released in-service pod that gave up its claim no longer owns its destination berth.
`parkUnclaimedReleased` (`internal/sim/released.go` `(*Simulation).parkUnclaimedReleased`) therefore parks it again at the end of the next `dispatch`, and it claims a berth again.
When the pod still waits, the next first phase gives up that claim again.
This repeats at most once in each tick for each released pod that waits, with one `nearestFreeBerth` search each time, and the claim is free at each admission.
The repetition is deterministic, and it ends when the pod no longer waits or when the gate turns off.

### 9.6 Wait reports

Each report is derived from the current attempt of the pod, at the place where the code writes a wait reason today.

| Reason | Written by | When | `BlockedBy` |
| --- | --- | --- | --- |
| "Fault braking" | The admission skip of a faulted pod (`internal/sim/traffic.go` `(*Simulation).admit`) | The pod is faulted and `Pod.Speed > 0`. | Its own fault ID |
| "Fault stopped" | The same skip | The pod is faulted and at rest. | Its own fault ID |
| "Blocked by incident" | The owner loop of `grant` (`internal/sim/traffic.go` `(*Simulation).grant`) | The refused resource has a fault owner, or a faulted pod owns it. | The fault ID of that owner |
| "Blocked by incident" | The buffer head paths (`internal/sim/station_buffer.go` `(*Simulation).bufferHead`, `(*Simulation).grantBufferedHead`) | The blocking owner is a fault owner or a faulted pod. | The fault ID of that owner |
| "Blocked by incident" | Admission, at a failed terminal berth choice (`internal/sim/traffic.go` `(*Simulation).admit`) | A berth of the destination station is blocked. | The fault ID of the blocked berth with the lowest serial |
| "No forward route" | `continueJourney` (`internal/sim/riders.go` `(*Simulation).continueJourney`) | The next-leg search fails while the blocked set is not empty. | Empty, because a failed search names no single fault |
| The existing reasons | The existing code | Every other case | The existing value |

A pod behind a healthy pod that waits reports "Pod ahead" with that pod, which is its immediate blocker.
When the terminal berth choice fails while the blocked set is not empty, admission writes the report before it returns, so the failed attempt replaces the report of the previous tick.
With an empty blocked set, admission writes what it writes today.

Each tick writes each report again, so no report outlives its cause, and arrival, idle settlement, and a clear need no cleanup step.
A clear also resets each `BlockedBy` that names the cleared fault (section 5.4), so a paused command boundary does not show a removed record.
The fault ID in `BlockedBy` names a record of the `faults` group, which gives the pod or lane and the onset tick (section 13.4).
`cmd/compare` ignores unknown wait reasons (`cmd/compare/traffic.go` `(*trafficWaits).sampleWaits`), and it refuses the marker in any case.
The pod inspector (`internal/view/game.go` `(*Game).inspectionRows` (at 17ab489)) shows the reason and `BlockedBy` with its existing rows.

### 9.7 Recovery and liveness

After the last clear, recovery completes when no new incident starts, each pending continuation has an admissible forward route, receiving capacity becomes available, and physical reservation dependencies drain under fair admission.
Phase timers must end, and new work must not starve the recovery.
Graph reachability alone gives neither completion nor a deadline.
When a condition fails, the system stays safe and reports the wait (section 9.6).

| Wait | Event that ends it |
| --- | --- |
| Incident boundary | The last clear removes the boundary and the caches of the old epoch. |
| Failed next-leg search | `continueJourney` runs again at each tick. |
| Unused berth claim in front of a recovery | Its waiting owner gives it up before the grants (section 9.5). |
| Track or junction occupied | The owner moves and releases its passed resources (`internal/sim/traffic.go` `(*Simulation).releasePassedResources`). |
| Boarding or unloading | The phase timer ends. |
| Fault recovery after the clear | Arrival clears purpose 3, and the hold release rule restores service. |
| Healthy empty pod at the berth | `clearBlockedBerths` moves it when parking is available. |
| Blocked destination berth of a healthy pod | Terminal reevaluation moves an uncommitted pod to another free, reachable berth of the same station. Otherwise the clear of the fault at that berth ends the wait. |
| Settled idle pod | No wait remains. |

Admission aging (`internal/sim/traffic.go` `(*Simulation).admit`) gives a request that waited precedence over younger requests, but it does not displace a reservation.
Berth clearing can report `ParkingUnavailable` (`internal/sim/parking.go` `(*Simulation).clearBlockedBerths`).
A reservation cycle or missing receiving capacity therefore has no guaranteed end in stage 2.

## 10. Invariants

| ID | Invariant |
| --- | --- |
| F1 | Each pod record names a pod with `faulted` and `faultHold`. Each pod with `faulted` has exactly one pod record. |
| F2 | A faulted pod is not a coupling member, an approach member, a platoon leader or follower, or a compact member. |
| F3 | `faultCap` is written only at onset and at the clear. A faulted traveling pod has `distance <= faultCap <= blocks.end(reservedThrough)`. The step of a faulted pod in `move` and in every motion proof is `faultMoveStep` with the same frozen inputs. |
| F4 | A faulted pod never gains speed. Once at rest, it keeps its physical pose and speed 0 until the clear. Its `reservedThrough` does not grow while the fault lasts. |
| F5 | Records are in serial order, IDs are unique, at most 64 are debris, start and end are not negative, `end == 0 || end > start`, and `start <= s.tick`. |
| F6 | Debris footprints are disjoint from each other and from each pod-fault footprint. The debris owns each resource of its footprint. |
| F7 | No resource outside the footprint of an active debris has a fault owner. |
| F8 | `faultReleased` is empty at every boundary. |
| F9 | Without the fault marker, no record exists, no pod is faulted, and `blocked` is empty. |
| F10 | The blocked set equals the derivation of section 8.1 from the current records, at every boundary. |
| F11 | `withdrawn` is 0 or `faultHold` for every pod. Stage 2 withdraws a pod only at fault start. Stage 3 extends this rule to `emergencyHold`. |
| F12 | A record exists only while its fault is active. No hold and no purpose keeps a record. A pod with `faultHold` and no record has `op.owner == faultHold`, or it is released in the next fault stage, or it is a coupling or approach member. |

`CheckContract` (`internal/sim/state_contract.go` `(*Simulation).CheckContract`) checks F1, F2, the bounds of F3, and F5 to F11 after each tick and each command.
The clear tests check F12.
F4 and the step identity of F3 are transition properties.
A test helper compares each faulted pod before and after each tick:

- The speed never rises.
- While the pod brakes, its route distance never decreases and stays at most the fixed cap.
- A pod that was already at rest before the tick keeps its physical pose (the lane and the offset on it, or the berth) and speed 0.
- For a pod that was already at rest, the route distance and the cap stay the same while the route coordinates stay the same.
  A berth evacuation calls `settleIdleAtBerth`, which sets the distance to 0 (stage 1, section 8.2), and a clear ends the fault, so each one ends the comparison of route coordinates.

Evacuation timing is a property of the fault stage: a test asserts that each faulted pod with active riders is evacuated in the first fault stage with `s.tick >= evacuateTick` and the pod at rest.

## 11. Atomicity

Each operation checks every precondition before its first write, and returns an error with no change when one fails.
The first write of a fault start is `nextIncidentID`, so the arithmetic check of section 5.2 runs before it.
After an operation returns, the state contract holds.

The operations run only at these places:

- At a command boundary, under the session lock: `Fault` and `ClearFault`.
  The public entry calls `observe` (`internal/sim/state_contract.go` `(*Simulation).observe`) once, after the operation.
  The session delivers interruptions before it releases the lock (stage 1, section 8.5).
- In the fault stage (section 5.5).
- In `arrive`, which only rebuilds the blocked set.
- In `dispatch`, which only unbinds a pickup through the access test (section 9.4).
- In the first phase of `admit`, which only gives up unused service claims (section 9.5).

Composite steps:

| Composite | Internal steps |
| --- | --- |
| Pod fault start | Preflight, record, `withdrawService(faultHold)` when the hold is not set, service claim surrender, cap, rebuild. |
| Debris start | Preflight, record, owners, rebuild. |
| Clear, pod | Record removal, cap removal, `restoreService(faultHold)` when the hold owns no purpose, rebuild, `BlockedBy` reset. |
| Clear, debris | Record removal, resource release (immediate at a boundary, deferred in the fault stage), rebuild, `BlockedBy` reset. |
| Endpoint reroute | `endpointRoute`, which writes nothing, then `setVehicleRoute` and `pending`. |
| Incident unbinding | `releasePickup`, then the binding fields. |
| Claim surrender | The wait derivation, which writes nothing, then `releaseOwned` for each `claimService` resource. |

The later step of a composite cannot fail after the first succeeds, because the first step does not change the inputs of the later one.
`withdrawService` changes no route, destination, or owner (stage 1, W4), and `releasePickups` changes no resource owner (stage 1, section 5.4).
`releaseOwned` of a `claimService` resource frees no resource that motion needs, because `claimKind` puts every stopping, retained, lent, and committed claim before `claimService`.
The implementation checks both sets of preconditions before the first write.

Refusal oracle.
`CheckContract` validates one state; it does not show that a refused operation changed nothing.
Each refusal test clones the simulation before the call and compares the clone with the state after it: the exported state, `owners`, every `routeReleases` and `nextRelease`, the waiting trips with their bindings and exclusions, `incidentSerial`, the records, the counters, the holds, and the purposes.
A test that injects a failure into the later step of each composite uses the same comparison.

## 12. Settings, commands, and session events

### 12.1 Project settings

```json
"incidentContract": "incident-v1",
"faultContract": "fault-v1",
"faults": {
  "evacuationSeconds": 300,
  "perHour": 0,
  "debrisShare": 0,
  "debrisMeters": 2,
  "duration": {"kind": "fixed", "seconds": 600}
}
```

| Member | Type | Range | Default |
| --- | --- | --- | --- |
| `evacuationSeconds` | integer | 0 to 3,600 | 300 |
| `perHour` | number | 0 in stage 2. 0 to 60 from stage 7. | 0 |
| `debrisShare` | number | 0 to 1 | 0 |
| `debrisMeters` | number | 0.5 to 50 | 2 |
| `duration.kind` | string | `fixed`, `uniform`, `exponential` | Required when `perHour > 0` |
| `duration.seconds` | integer | 1 to 86,400 | `fixed` only |
| `duration.minSeconds`, `duration.maxSeconds` | integer | 1 to 86,400, min at most max | `uniform` and `exponential` |
| `duration.meanSeconds` | integer | min to max | `exponential` only |

Rules:

- `faultContract` requires `incidentContract`.
  `Validate` (`internal/project/config.go` `Validate`) refuses the fault marker without it.
- With the marker, `faults` is required.
  Without the marker, `faults` is refused, also as null or an empty object.
- Unknown members, null values, and members that do not match the kind are refused.
- `perHour` above 0 is refused in stage 2 (section 1.2).
- `Validate` adds the widest `faults` object to its size estimate, as it does for `widestDemand` (`internal/project/config.go` `widestDemand`).
- The marker scan (`internal/project/service.go` `scanProjectFields`) records `faultContract` and `faults`, as it does for the incident marker.
- `web/editor.js` keeps both members of a loaded project and writes them back unchanged.
  The editor gets no control for them in stage 2.
- `internal/parkride` and `cmd/compare` keep refusing a project with the incident marker, so they also refuse the fault marker.

### 12.2 Sampler mechanism, for stage 7

Stage 2 does not run the sampler, because `perHour` is 0.
This section fixes the mechanism, so that stage 7 adds the rate without a new format decision.
The rate unit and the duration distributions are product choice P6.

- The sampler belongs to the simulation, so `Clone`, save, and restore cover it without session code.
- It has its own `math/rand/v2` PCG stream.
  It never draws from the demand PCG (`internal/session/demand.go` `newDemand`), and the demand PCG never draws for it.
  Its seed is the first 16 bytes of `SHA-256("podsim-faults-v1" || bigEndian(demand seed))`, as two `uint64` values, as `internal/project/rail_arrivals.go` `RailSchedule` derives per-event values.
  A test runs matched seeds with the sampler on and off and checks that the demand draws are identical.
- Events form a Poisson process at `perHour`.
  After each event, and at start, the sampler draws `u` and sets `nextTick = tick + max(1, ceil(-ln(1-u) × 216000 / perHour))`.
- Each event uses exactly four more draws, in order: kind (debris when `u < debrisShare`), target (pod index over the pods without a fault in pod ID order, or a lane chosen with probability proportional to its length), position (`u × (length - debrisMeters)`), and duration.
- Durations: `fixed` gives `seconds`, `uniform` gives `minSeconds + u × (maxSeconds - minSeconds)`, and `exponential` gives `min(maxSeconds, max(minSeconds, -ln(1-u) × meanSeconds))`, each rounded up to whole ticks.
- An event whose target fails a precondition of section 5.2 or 7.3 is skipped and counted.
  It still uses its draws, so later events do not depend on outcomes.
- The sampler runs in the fault stage, before step 1.
- The sampler state is saved under the fault marker as `/simulation/faults/random` and `/simulation/faults/nextTick`.
  Stage 7 adds these members and their bytes.

### 12.3 Commands

| Action | Members | Effect | Reply |
| --- | --- | --- | --- |
| `fault` | `podID`, optional `durationSeconds` | Starts a pod fault. Without a duration, the fault lasts until cleared. | `faultID` |
| `fault` | `laneID`, `fromMeters`, `toMeters`, optional `durationSeconds` | Starts debris. | `faultID` |
| `clearFault` | `faultID` | Clears an active fault. | |

`podID` and `laneID` are mutually exclusive, and one of them is required.
`fromMeters` and `toMeters` go only with `laneID`.
`durationSeconds` is an integer from 1 to 86,400.
A paused session accepts both actions.
While a coupling fault is retained, both are refused, as other actions are (`internal/session/session.go` `(*Session).apply`).
An exact retry of `fault` or `clearFault` returns the stored reply, as for every command.

`Command` (`internal/session/session.go` `Command`) gets `PodID`, `LaneID`, `FromMeters`, `ToMeters`, `DurationSeconds`, and `FaultID`, each with a digest extension tag (section 13.2).
`Reply` gets `FaultID`, omitted when empty.
Receipt replies are not saved, so the reply member adds no save bytes.

### 12.4 Errors

Every error is `command_rejected` with one of these messages:

| Message | Cause |
| --- | --- |
| `faults are not enabled` | The project has no fault marker. |
| `fault target is not supported` | Section 1.4 refuses the target in stage 2, precondition 9 of section 7.3 fails, or the command has both `podID` and `laneID`, or neither. |
| `unknown pod` | No pod has `podID`. |
| `pod already has a fault` | The pod has a pod fault. |
| `unknown lane` | No lane has `laneID`. |
| `invalid debris segment` | Precondition 5 of section 7.3. |
| `debris overlaps a pod or another fault` | Precondition 7, or a resource of precondition 8 under a pod body. |
| `debris meets a reserved resource` | Any other resource of precondition 8. |
| `debris limit reached` | Precondition 6. |
| `incident limit reached` | Precondition 3 of section 5.2: the arithmetic or the serial would overflow. |
| `unknown fault` | `faultID` names no active record. |
| `invalid fault duration` | `durationSeconds` is out of range. |

A refused command changes nothing (section 11).

### 12.5 Counters

`faultCounters` holds five `int64` counters, each omitted at 0 in every format:

| Counter | Meaning |
| --- | --- |
| `started` | Faults started, of both kinds. |
| `cleared` | Faults cleared, by command or by duration. |
| `evacuations` | Evacuations. The interrupted parties are in the stage 1 counters. |
| `reroutes` | Successful calls of `rerouteToEndpoint` (section 9.3). |
| `faultWaitTicks` | Pod ticks of healthy pods with the reason "Blocked by incident" or "No forward route" (section 5.5). |

Each counter saturates at `math.MaxInt64`: an increment at the maximum leaves the counter at the maximum.
`faultWaitTicks` adds 1 for each pod, with the same saturation.
Saturation never refuses and never fails a transition, so a fault start, a timed clear, an evacuation, and a reroute after a restore with a counter at the maximum all complete.
The full metric set is product choice P8 and lands in stage 7.

### 12.6 Game control

The pod inspector (`internal/view/fault.go`) gets one button when the topology has the fault marker:

- "Fault" on a pod with no fault, sending `fault` with the pod ID and no duration.
- "Clear fault" on a faulted pod, sending `clearFault` with its fault ID from the `faults` group.

The view refuses nothing on its own: a refused command shows the existing command error.
Debris drawing and a debris control land in stage 7.
The timing of the controls is product choice P7.

### 12.7 Session events and restore tiers

| Event | Effect |
| --- | --- |
| Pause | Ticks stop, so durations, evacuation timers, braking, and the reroute cadence stop. Commands still run. |
| Reset | `Reset` (`internal/sim/simulation.go` `(*Simulation).Reset`) clears the records, `faultHold`, the blocked set, and the counters. `incidentSerial` stays, as stage 1 defines. |
| Demo | The demo project has no fault marker. Faults end as on reset. |
| Project apply | Any change to the fault marker or to `faults` fails `sameExceptCouplingEnabled` (`internal/session/session.go` `sameExceptCouplingEnabled`), so the fleet is rebuilt and every fault ends. An identical project keeps the faults. |
| Checkpoint and rewind | `Clone` (`internal/sim/clone.go` `(*Simulation).Clone`) deep-copies the records, the counters, `faulted`, `faultCap`, the blocked set, and `rerouteDue`. A rewind restores them exactly. The new generation gives new records a new ID prefix. |
| Physical restore, every faulted pod keeps its place | Records and counters are restored. Debris is placed before the pods (section 7.6). A faulted traveling pod restores at speed 0 with `faultCap = distance`, as every traveling pod restores at speed 0 (`internal/sim/state_physical.go` `(*physicalRestore).placeTravelingPod`), so a braking fault restores as stopped. Elapsed time is kept through `start` and `end`. After `claimDestinations`, effect 3 of section 5.2 runs again for each faulted pod, so a restored faulted pod owns no service claim. `rerouteDue` is set. |
| Physical restore that would demote a faulted pod (`internal/sim/state_physical.go` `(*physicalRestore).separate`, `(*physicalRestore).placeDemoted`) | The physical tier fails. The restore then follows the fallback rules of stage 1, section 9.6: where a logical fallback is allowed, the logical tier runs, and where it is not, the restore fails as it does today. No single record is cancelled to keep the physical tier. |
| Logical restore | Every record ends. Each pod loses `faultHold` after the stage 1 logical tier clears its purpose (stage 1, section 9.6). Counters are kept. `RestoreResult` gets `DroppedFaults`, the number of records that ended. |
| Invalid incident data | The whole save is `invalid_state` and moves aside (section 13.5). |

## 13. Formats

### 13.1 Marker and its propagation

The project gets one top-level feature marker, `faultContract: "fault-v1"`.
It requires the incident marker (stage 1, section 11.2: later feature markers require it).
It gates every stage 2 member.

| Carrier | Member | Rule |
| --- | --- | --- |
| Save | `/project/faultContract` | Source of truth. `RestoreState` gets it with the other contract inputs. |
| Topology, stream hello, and `GET /api/topology` | `TopologySnapshot.faultContract` (`internal/session/protocol.go` `TopologySnapshot`) | Copied from the project. |
| Full frame and `GET /api/state` | `SimulationFrame.faultContract` (`internal/session/protocol.go` `SimulationFrame`) | Copied from the simulation. |
| Agreement | `frameState` (`internal/session/protocol.go` `frameState`) | A new `faultFrameBinding`, beside `incidentFrameBinding`, rejects a frame whose fault marker differs from the topology marker. |
| Raw presence | `decodeMarkedJSON` and `ApplyStream` | `scanIncidentMembers` (stage 1, section 11.2) also records any stage 2 member name or the `faults` group key, with any value. A delta or a full envelope with one and no fault marker is rejected. |
| Assembler | `StreamAssembler.State` | Rejects a fault marker change inside one stream. |
| Web | `web/editor.js:1275`, `web/shell.js:147` | `markersAgree` also needs the topology and the simulation to have the same fault marker, absent or `fault-v1`, and refuses it at the root, as for the incident marker. |

### 13.2 Digest registry entries

Each new `Command` field and each new project field gets the tag `digest:"ext=N"` and an entry in `digestExtensions` (`internal/session/receipt.go` `digestExtensions`):

| N | Field path |
| ---: | --- |
| 2 | `Command.Project.FaultContract` |
| 3 | `Command.Project.Faults` |
| 4 | `Command.PodID` |
| 5 | `Command.LaneID` |
| 6 | `Command.FromMeters` |
| 7 | `Command.ToMeters` |
| 8 | `Command.DurationSeconds` |
| 9 | `Command.FaultID` |

Each field sits at a fixed path, not inside a slice or a map, as stage 1 section 11.3 requires.
`Faults` is a pointer to a struct, so the extension pair encodes the whole object with the existing value encoding.
The fields inside `Faults` are not tagged: the main walk skips `Faults` as a whole and never reaches them.
A command with no set extension field keeps its digest.
Stage 3 reuses `N = 4` for its emergency command.

### 13.3 Save members

| Path | Shape | Presence |
| --- | --- | --- |
| `/project/faultContract` | `fault-v1` | With the marker only. |
| `/project/faults` | Object, section 12.1 | Required with the marker. Refused without it. |
| `/simulation/faults` | `{"records": [...], "counters": {...}}` | With the marker only. Omitted when there is no record and every counter is 0. |
| `/simulation/faults/records/*` | Pod: `[generation, serial, start, end, 0, pod]`. Debris: `[generation, serial, start, end, 1, lane, from, to]`. | `records` is omitted when empty. |
| `/simulation/faults/counters` | Object of the counters of section 12.5 | Each counter is omitted at 0. `counters` is omitted when every counter is 0. |

`pod` is an index into `/simulation/pods`.
`lane` is an index into the saved project `network.lanes`.
The tuples use indexes for the reason of stage 1 section 11.7: escaped IDs do not fit the save budget.
The adapter converts between indexes and native IDs, as stage 1 does.

No member lists resources.
A pod-fault footprint is derived from the restored pod.
A debris footprint is derived from the network and the segment.
`faultCap`, the phase, `evacuateTick`, and the wait reports are derived or computed again (sections 4.1, 9.6, and 12.7).

### 13.4 Stream and HTTP state members

| Path | Shape | Delta group |
| --- | --- | --- |
| `.../simulation/faultContract` | `fault-v1` | None. Full frames only. |
| `.../simulation/faults` | `{"active": [...], "counters": {...}}` | New group `faults`, present only with the fault marker, as the `coupling` group is present only with the coupling contract (`internal/session/stream_codec.go` `frameGroups`). |

Each element of `active`, in serial order:

```json
{"id": "i3.17", "kind": "pod", "podID": "p7", "phase": "stopped",
  "startTick": 1200, "endTick": 37200, "evacuateTick": 19200}
{"id": "i3.18", "kind": "debris", "laneID": "l12", "fromMeters": 40, "toMeters": 42,
  "startTick": 1300}
```

| Member | Pod record | Debris record |
| --- | --- | --- |
| `id`, `kind`, `startTick` | Required | Required |
| `podID`, `phase`, `evacuateTick` | Required | Forbidden |
| `laneID`, `fromMeters`, `toMeters` | Forbidden | Required |
| `endTick` | Optional, omitted for a fault without an end | Optional |

- `phase` is `braking`, `stopped`, or `evacuated` (section 4.1).
- Records hold ticks, not countdowns, so the group changes only at fault events, at phase changes, and at counter changes.
- `active` and `counters` are omitted when empty, so the group with the marker and no fault is `{}`.

Vehicles get no new member.
The wait reason and `BlockedBy` are existing members.
The longest new wait reason, "Blocked by incident", is shorter than the existing "No parking available", and a fault ID of 44 bytes with quotes is shorter than the widest escaped pod ID of 386 bytes in `BlockedBy`, so neither changes a worst case.

### 13.5 Strict decoding and prescan

Every decoder rejects unknown members, as today.
Without the fault marker, each stage 2 member is rejected, including an explicit null, an empty object, and an empty array.

With the marker, the save decoder rejects, as `invalid_state` for the whole save:

- a tuple of the wrong length for its kind, or an unknown kind;
- a pod index or lane index out of range, or two pod records for one pod;
- a pod record whose pod does not have `faultHold`, or that breaks F2;
- a debris segment that fails precondition 5 of section 7.3, or footprints that break F6;
- more than 64 debris records;
- records not in strictly increasing serial order, or a serial above `incidentSerial`;
- a negative `start`, `end`, or counter, a `start` above the saved tick, or `end != 0 && end <= start`;
- a record whose `start + evacuationSeconds × 60` overflows `int64`.

With the marker, the stream and HTTP decoders apply these rules to a full frame and to a `faults` replacement group alike:

- the member table of section 13.4: each required member present and not null, each forbidden member absent, and an optional member not null when present;
- `id` of the form `i<uint64>.<uint64>`, unique, with serials strictly increasing in order;
- at most one record for each `podID`, at most 64 debris records, and at most 300 pod records;
- `podID` and `laneID` known in the topology;
- `kind` and `phase` from their lists;
- ticks not negative, `startTick` not above the tick of the frame or delta that carries the record, and `evacuateTick >= startTick`;
- `endTick > startTick`, only when `endTick` is present, because an absent `endTick` is a fault without an end;
- `fromMeters` and `toMeters` finite, `0 <= fromMeters < toMeters`, `toMeters` not above the length of the lane in the topology, and `toMeters - fromMeters <= 50`;
- counters not negative and not null.

A delta that breaks a rule is rejected as a whole, and the accepted base frame stays.

Prescan limits (`internal/session/format_limits.go` `savedLimits`, `streamLimits`):

| Path | Limit | Basis |
| --- | ---: | --- |
| `/simulation/faults/records` | 364 | 300 pod records and 64 debris records |
| `/simulation/faults/records/*` | 8 | The debris tuple |
| `/full/state/simulation/faults/active` | 364 | Same |
| `/frame/state/simulation/faults/active` | 364 | HTTP state |
| `/delta/groups/faults/active` | 364 | The delta group, with no `value` wrapper |
| `/active` | 364 | Only when a scanner reads the replacement group alone, as `scanCouplingReplacement` does for `coupling` |

The prescan bounds allocation only.
The semantic rules above carry the record contract.
The delta path follows `/delta/groups/<name>` (`internal/session/stream_codec.go` `frameGroups`).
The array audit of stage 1 (section 11.6) derives the paths from real envelopes and fails on any array path with no explicit limit.
Each limit has a test at the limit, at the limit plus one before typed decoding, at a deeper nesting, and with a gzip body that expands past the byte cap.

### 13.6 Byte budget

Widest encodings, with `uint64` generation and serial (20 digits), `int64` ticks (19 digits), and a nonnegative `float64` in its shortest form (23 bytes):

| Item | Widest encoding | Bytes | Count | Total |
| --- | --- | ---: | ---: | ---: |
| Pod tuple | `[18446744073709551615,18446744073709551615,9223372036854775807,9223372036854775807,0,299]` | 89 | 300 | 26,700 |
| Debris tuple | `[18446744073709551615,18446744073709551615,9223372036854775807,9223372036854775807,1,7999,2.2250738585072014e-308,2.2250738585072014e-308]` | 138 | 64 | 8,832 |
| Tuple separators | `,` | 1 | 363 | 363 |
| Counters | Five counters of 19 digits | 163 | 1 | 163 |
| Wrapper | `,"faults":{"records":[]` and `,"counters":` and `}` | 36 | 1 | 36 |
| Project marker and `faults` | Inside the project member, which `Validate` bounds | 0 | | 0 |
| Save total | | | | 36,094 |

Save headroom after stage 2, against the amended stage 1 totals (129,516 plain and 373,116 Express, stage 1 section 11.7):

| Shape | After stage 1 | Stage 2 | After stage 2 |
| --- | ---: | ---: | ---: |
| Plain | 27,856,898 | 36,094 | 27,820,804 |
| Coupling | 27,784,055 | 36,094 | 27,747,961 |
| Express | 6,463,547 | 36,094 | 6,427,453 |
| Express with coupling | 6,463,453 | 36,094 | 6,427,359 |

Stage 2 uses 36,094 bytes of the fault allocation of 4,194,304 bytes for stages 2, 4, 5, and 6 (stage 1, section 11.7, with the amended reserve of 2,203,613 bytes).
It requests a sub-allocation of 65,536 bytes, so 4,128,768 bytes stay for stages 4, 5, and 6.
The reserve does not change.

Stream and HTTP growth, with escaped IDs of 386 bytes:

| Item | Bytes | Count | Total |
| --- | ---: | ---: | ---: |
| Pod record with `phase` `evacuated` and three ticks | 576 | 300 | 172,800 |
| Debris record with two floats and two ticks | 597 | 64 | 38,208 |
| Separators, wrapper, and counters | | | 561 |
| `faults` member or group | | | 211,569 |
| Frame marker `,"faultContract":"fault-v1"` | 27 | 1 | 27 |
| Topology marker, HTTP state only | 27 | 1 | 27 |

| Shape | HTTP headroom after stage 1 | Stage 2 | After stage 2 |
| --- | ---: | ---: | ---: |
| Plain | about 21,690,170 | 211,623 | about 21,478,547 |
| Coupling | about 20,952,654 | 211,623 | about 20,741,031 |
| Express | about 500,312 | 211,623 | about 288,689 |
| Express with coupling | about 237,204 over the cap | 211,623 | about 448,827 over the cap |

Full frames and deltas keep more than 10 MB of headroom in every shape after stage 1, so they fit.
The Express with coupling HTTP state is over its cap already after stage 1.
By the maintainer decision of October 5, 2026, a composed shape over its cap raises that cap just enough to fit, with the composed measurement as evidence.
Stage 2 requests a stream and HTTP allocation of 262,144 bytes from the allocation that stage 0 sets for later stages.
The stage 2 format patch measures the composed shapes with every stage 1 and stage 2 member at its widest, and raises the stream and HTTP cap if a shape is over it.

## 14. Off-state identity

With the fault marker absent, trajectories, save bytes, stream bytes, command digests, and the RNG sequence stay identical to the build before stage 2.
`internal/sim` has no random source in stage 2.
The sampler PCG lands in stage 7, has its own stream, and is created only when `perHour > 0`.

With the fault marker present and no fault started, trajectories and the RNG sequence stay identical.
Bytes and digests differ only by:

- The project members `faultContract` and `faults`, the topology and full-frame markers, and the `faults` delta group as `{}`.
- The digest of a command whose supplied project has the fault marker, by the extension pairs of `N = 2` and `N = 3`.

| Change | Off-state effect | Treatment |
| --- | --- | --- |
| Fault stage, with the reroute pass | Runs only with `faultsOn`. The pass runs only with `rerouteDue` or a blocked set that is not empty. | Gated by marker and state. |
| `routingGraph` | Returns `s.graph` when no fault is active. `laneOpen` is true for a nil `blocked`. | Gated by state. |
| `staticConnected` for admission | Read only while the blocked set is not empty. | Gated by state. |
| Static arrival-chain test of `divertStart` | Gives the same answer as the cached search when no lane is blocked. | Equal by construction. |
| Static detour baselines | Read only while the blocked set is not empty. | Gated by state. |
| Gates of section 6.2 and `faultMoveStep` | `faulted` is false for every pod. | Gated by state. |
| Foreign fact members `faulted` and `faultCap` | False and 0. The proof takes the ordinary branch, and the members compare equal. | Gated by state. |
| `pickupAccess` and its gates | True when the blocked set is empty. | Gated by state. |
| `pickupBerthFilter` and the rule 4 continuation check | The filter equals `berthFilterForStops` and the check does not run when the blocked set is empty. | Gated by state. |
| Claim surrender | Runs only while an incident is outstanding: an active record exists or a pod has `faultHold`. | Gated by state. |
| Wait reports | Written only for a faulted pod, a fault owner, a blocked berth, or a blocked set that is not empty. Otherwise the existing writes run. | Gated by state. |
| `BlockedBy` reset at a clear | No clear runs without a record. | Gated by state. |
| `retainedOwners` debris step | No record. | Gated by state. |
| `faultOwnerKind` in `String` | No owner of that kind exists. | Gated by state. |
| Save, stream, and HTTP members | Written only with the marker. | Gated by marker. |
| Digest tags | No tagged field set, so no new pair. | Gated by value. |
| Cap raise, if the measurement needs it | Accepts larger documents. Changes no bytes. | Own format patch. |

Gates for every patch:

- The format goldens match byte for byte with the fault marker absent.
- The digest baseline (`internal/session/testdata/command_digests.txt`) matches with the fault marker absent.
- Matched-seed runs on LondonCentral and the rail-hub preset, with the fault marker absent and present, give identical exported simulation states at every 600th tick, apart from the marker.

## 15. API for later stages

| Function or state | Caller | Preconditions |
| --- | --- | --- |
| `startPodFault(v, duration)` | Stage 4 extends it to trains, stage 6 to platoon and compact members, stage 7 calls it from the sampler. | Section 5.2. Each later stage removes its row of section 1.4 only with its own certified transition. |
| `startDebris(lane, from, to, duration)` | Stage 7 sampler. | Section 7.3. |
| `clearFault(id)` | Commands, durations, and later policies. | Section 5.4. |
| `faultMoveStep` and the foreign fact members | Stage 4 adds a train braking step beside it, under the same rule: one step for motion and proof. | Section 6.1. |
| `routingGraph()` and the blocked set | Stage 3 emergency routing, stage 5 reverse targets. | None. Read only. |
| `endpointRoute(v)`, `rerouteToEndpoint(v)` | Stages 3 and 5, for a reroute that keeps the endpoint. Stage 5 uses a false result with a blocked kept prefix to find reverse candidates. | Section 9.3. |
| `pickupAccess(v, request)` | Stages 3 and 5. | Section 9.4. |
| `incidentOutstanding()` and the claim surrender | Stage 3 adds `emergencyHold` to the gate. | Section 9.5. |
| Wait reports | Stages 3, 5, and 7 add reasons. | Section 9.6. |
| `faultOwnerKind` | Any later obstacle. | Section 4.2. |

Preconditions that later stages must meet:

- A later stage that accepts a new target adds its own off-state, safety, and round-trip tests before it removes the refusal of section 1.4.
- A policy that ends a fault while its fault recovery travels does not call `restoreService(faultHold)`.
  The hold release rule releases the hold after the arrival.
- A stage that releases unused grants of a stopped pod defines the certified transition first (product choice P9).
- A stage that withdraws a pod for a new cause amends F11 first.
- A stage that gives a pod a new destination for an incident defines its own transition: the endpoint reroute never changes an endpoint.
- Stage 7 adds the sampler members and their bytes before it accepts `perHour > 0`.

Exported for the wire: the maintainer allows these exported identifiers of stage 2.
The commands, the saves, the frames, and the view use them across packages.
Each type keeps its exported fields.

- `internal/sim`: `FaultSettings`, `(*Simulation).SetFaults`, `FaultRequest`, `(*Simulation).Fault`, `(*Simulation).ClearFault`, `FaultContract`, `FaultV1Contract`, `ErrUnknownFaultContract`, and `ValidateFaultContracts`.
- `internal/sim`: `FaultView`, `FaultsView`, `FaultCounters`, `FaultKindPod`, `FaultKindDebris`, `FaultPhaseBraking`, `FaultPhaseStopped`, `FaultPhaseEvacuated`, `SavedFaults`, and `SavedFault`.
- `internal/sim` fields: `Snapshot.FaultContract`, `Snapshot.Faults`, `SavedState.Faults`, `RestoreStateInput.FaultContract`, `RestoreStateInput.Faults`, `RestoreResult.DroppedFaults`, and `FleetContracts.FaultContract`.
- `internal/project`: `FaultContract` and `FaultV1Contract`, which are aliases of the `internal/sim` identifiers, `FaultConfig`, `FaultDuration`, `EffectiveFaultSettings`, and the fields `Config.FaultContract` and `Config.Faults`.
- `internal/session`: the `Command` fields `PodID`, `LaneID`, `FromMeters`, `ToMeters`, `DurationSeconds`, and `FaultID`, and the fields `Reply.FaultID`, `TopologySnapshot.FaultContract`, `SimulationFrame.FaultContract`, and `SimulationFrame.Faults`.

## 16. Test plan

### 16.1 Unit tests

- Pod fault on a lane at cruise speed: the pod brakes with a speed drop of at most `acceleration × dt` per tick and rests at `faultCap`.
  The transition helper of section 10 checks at each tick that the cap is unchanged, the speed never rises, and a pod at rest keeps its pose and speed (F3, F4).
- Transition helper during braking: the distance grows at each tick and never passes the cap, and the helper accepts each step.
- Transition helper at a berth evacuation: a faulted pod that arrived with a route distance above 0 is evacuated at the berth; `settleIdleAtBerth` sets the distance to 0; the pose and speed stay, and the helper accepts the step.
- A clear during braking lets the pod continue inside its grants with the ordinary step and no snap.
- Native foreign proof: a train beside a braking faulted pod; the prediction equals the publication at every tick; a changed cap or a changed `faulted` member fails the proof.
- Pod fault at a berth in each accepted activity: the phase timer stops, the pod does not depart, the berth is blocked, a pod with an entry end chooses another berth, and a pod with that berth as its end waits with "Blocked by incident" and the fault ID when it is committed to its inlet or no other compatible berth of the station is free and reachable.
- Arrival during braking: the fault stays, and no rider alights.
- Pod fault in a station entry queue: the faulted head makes no berth-grant attempt; the pods behind it with a divert node reroute to their endpoint; the pods in the arrival chain keep their route and wait with a report.
- Grants at rest: a faulted pod keeps every grant, and its `reservedThrough` does not grow.
- Evacuation: delay 0, 1, and 300 seconds; a pod that reaches rest after `evacuateTick`; a fault with `end <= evacuateTick` never evacuates; every active rider is interrupted, also a rider at its destination berth.
- Evacuated pod on a lane, for each route shape of stage 1 lane evacuation: purpose 3 with `faultHold` on the original route, with no search for a free berth; the clear removes the record at once; the hold stays for the fault recovery; the arrival clears the purpose, and the next fault stage releases the hold (F12).
- Fault on a pod in its fault recovery: the hold is reused, and no second pickup release happens.
- Pickup release at fault start: a bound pickup, an active hold, and a stale deferral, each with the stage 1 exclusion result, and the exclusion kept until the trip boards.
- Service claim at fault start (section 1.6): an empty relocating pod gives up its unused claim on a remote berth; a pod whose destination resource is inside its stopping grant keeps it; an occupied pod keeps every claim; the recovery takes the berth again through admission after the clear.
- Debris start: each precondition of section 7.3 at its limit and one step past it, with each error message.
  This includes a free segment on the unowned future corridor of an existing approach (precondition 9).
- Debris clear at a command boundary and in the fault stage: immediate and deferred release.
- Blocked routing: each reader of section 8.2 avoids a blocked lane, including bank searches and the multi-target search; Express service validation, `networkStationsConnected`, station maneuvers, and the arrival-chain test of `divertStart` stay static.
- Arrival-chain test: a restored pod inside its arrival chain whose station path crosses a blocked lane is still refused by `divertStart`.
- Cache epochs: a cached success and a cached failure, each across a fault start, a clear, and the arrival of a faulted pod, with a read in the unloading loop right after a command; the read sees the new epoch.
- Admission stays static: a new order to a cut-off station is accepted and waits; `podFitsRequest` never unbinds a trip for a blocked lane.
- Endpoint reroute, one fixture for each case: a traveling pod with a berth end; a traveling pod with an entry end at a banked station, where the detour test uses the entry of the new route; a `DepartingEmpty` pod; a `Boarding` pod and a `Continuing` pod at a berth with `reservedThrough < 0`; an occupied pod over its detour limit, which keeps its route; a pod in its arrival chain; a trapped pod with a blocked kept prefix; a blocked destination berth.
  In each case only the route and its derived indexes change, and every field of section 9.3 stays.
  A rerouted occupied pod arrives and unloads its riders; it does not become idle.
- Endpoint reroute of a buffered pod: a bypass that reaches the same entry through another last lane is refused, and the pod keeps its route, its buffer membership, and `bufferBerth`, and waits.
- A pod in its fault recovery whose original route crosses another active fault is rerouted to its endpoint.
- Terminal reevaluation in a full `Step`: an uncommitted occupied pod targets berth B1, a pod fault blocks B1, and B2 of the same station is free; the pod takes B2 and keeps its station, riders, and `Stops`.
  With every berth of the station blocked, the pod waits with "Blocked by incident".
- Detour baselines: a fault replaces a 100 m direct route with a 250 m bypass; a rider whose ratio is 2.5 against the static baseline is refused by the 1.5 limit, for a legacy rider and a recorded rider, each with a berth end and an entry end; the realized detour at alighting uses the static baseline.
- Pickup access, under both order contracts, with replacement supply that can reach the origin: a buffered pickup with an open route to the entry of bank A, where every compatible berth of bank A is blocked, loses the trip, and a pod that reaches bank B takes it; a busy pod whose end berth cannot reach the origin loses the trip also when another berth of its end station could reach it; an occupied compatible berth does not unbind a trip.
- Repeated dispatch, under both order contracts, on a network without class restrictions and with replacement supply that can reach the origin: a pickup inlet whose onward exit is blocked fails rule 3; the trip goes to the replacement pod; over many ticks the first pod never wins the trip again, and the binding changes once.
- Rule 4 continuation check: a busy pod whose cached `trip.route` crosses a blocked lane fails the check; a busy pod whose passenger leg ends at a station where `Berths[0]` is blocked and another berth is usable passes it; with an empty blocked set, the result equals `finishEstimate`.
- Pickup access, further cases: a bound pod with an open existing route keeps the trip also where `divertStart` refuses; a bound pod with a blocked kept prefix loses the trip; a transferred order with a route distance above 0 and retained grants uses `legOrigin()`; after the unbinding, the order keeps its ID, queue position, deferral budget, and an existing exclusion, and no new exclusion exists; `trip.route` and `trip.destination` are cleared under both order contracts; the same pass does not bind the trip again to the same inaccessible pod; a pickup pod whose pickup berth is blocked loses the trip and can be sent to another berth; each gate of section 9.4.
- Claim surrender, two pods and one berth (finding N1): a buffered head faults before it claims a berth, is evacuated, and clears; an empty pod behind it owns an unused claim on the only reachable berth; the head reaches the berth, and the recovery ends.
- Claim surrender gate: the gate is off with the marker and no fault, and states match a run without the marker; the gate stays on after the last clear until the hold release; a pod that waits behind a healthy pod that waits at a fault gives up its claim.
- Claim surrender kinds: no `claimCommitted`, `claimOccupied`, `claimStopping`, `claimRetained`, `claimLent`, or `claimOther` resource is released.
- Parking again after a surrender: a released pod that gives up its claim is parked again by the next `dispatch` and gives up the claim again while it waits; a replay from a checkpoint is identical.
- Wait reports: each row of section 9.6, with the reason and `BlockedBy`, including a failed terminal berth choice with every compatible berth blocked, a buffered head blocked by a faulted pod, `continueJourney` with no route, and a follower behind a healthy pod that waits.
  The report changes in the tick when the cause ends, also at arrival and at idle settlement.
  A paused clear publishes no `BlockedBy` of the removed record.
- Hold release rule: the rule of section 5.6, and a coupling member that keeps its hold until the split.
- Refusals: a coupling member, an approach member, a platoon leader, a platoon follower, a compact member, a second fault on a faulted pod, and the arithmetic limits, each compared with the refusal oracle of section 11.
- Atomicity: an injected failure in the later step of each composite of section 11 leaves the state equal to the clone.
- Counter limits: a restore with each counter at `math.MaxInt64`, then a command fault start, a timed clear, an evacuation, a reroute, and a tick with many waiting pods; each counter stays at the maximum, and each transition completes.
- Sampler stream, from stage 7: demand draws are identical with the sampler on and off.

### 16.2 Restore tests

- Physical restore of a braking fault: the pod restores at rest with `faultCap = distance`, and `rerouteDue` is set.
- Physical restore with debris: the debris is placed first and owns its whole footprint.
- Physical restore of a faulted pod with a saved destination claim: the service claim is given up again after `claimDestinations`, and a stopping claim stays.
- Physical restore that would demote a faulted pod: the physical tier fails; with a logical fallback, every record ends, `faultHold` is released, and the counters stay; without one, the restore fails as today.
- Physical restore with a fault recovery under `faultHold`, and with a surrendered claim (`claimsDestination` absent), which restores no claim.
- Logical restore: every record ends, `faultHold` is released, counters stay, and `DroppedFaults` is right.
- Checkpoint and rewind: an exact replay of 600 ticks with active faults, and a delayed `clearFault` with an ID of the abandoned timeline gets `unknown fault`.
- Each decoder rejection of section 13.5 is `invalid_state` for the whole save, including negative ticks and counters and an overflowing evacuation tick.

### 16.3 Codec tests

- Each stage 2 member without the marker is rejected, also as null, `{}`, and `[]`.
- A delta with the `faults` group and no marker is rejected through the raw presence record.
- A frame whose fault marker differs from the topology marker is rejected.
- Each stream rule of section 13.5, through a full frame and through a replacement group: a duplicate ID, two records for one pod, 65 debris records, a forbidden member for the kind, a missing required member, a null required member, a null `endTick`, a negative tick, `startTick` above the frame tick, `toMeters` above the lane length, and `endTick <= startTick`.
- A record without `endTick` is accepted.
- A rejected delta leaves the accepted base frame unchanged.
- The prescan limits of section 13.5, at the limit and one past it, on real envelopes.
- The web marker check accepts and refuses the same cases.

### 16.4 Composed byte tests

- The composed worst-case fixtures of stage 1 get 300 pod records and 64 debris records at their widest, beside every stage 1 member.
- Each save shape passes the 80 MiB cap, raw and compressed.
- Each full frame, delta, and HTTP state passes its cap, raised by the measurement where needed (section 13.6).

### 16.5 Command-boundary and end-of-tick saves

A save at each of these points restores in the physical tier and passes `CheckContract`:

- after a `fault` command on a moving pod, and after one on a pod at a berth;
- after a debris command;
- at the end of the tick in which a faulted pod reaches rest;
- at the end of the tick of an evacuation;
- after a `clearFault` command during braking;
- at the end of a tick with an endpoint reroute;
- at the end of a tick with a claim surrender.

### 16.6 Mutation targets

| Mutation | Test that must fail |
| --- | --- |
| Use `ordinaryMoveStep` for a faulted pod in `move` | Braking test: the pod passes its cap. |
| Use `ordinaryMoveStep` in the proof for a faulted fact | Native foreign proof test. |
| Drop the speed ceiling of `faultMoveStep` | Transition helper: speed rises. |
| Write `faultCap` after onset | Transition helper: cap changes. |
| Let `admit` process a faulted pod | Grants test: `reservedThrough` grows. |
| Test `withdrawn` in place of `faulted` in a motion gate | Fault recovery test: the pod does not move after the clear. |
| Evacuate before rest | Evacuation test with a pod that is still braking at `evacuateTick`. |
| Clear after evacuation in the fault stage | Test with `end == evacuateTick`. |
| Remove the coupling, approach, leader, follower, or compact guard, each one alone | The matching refusal test. |
| Accept a second fault on a faulted pod | Refusal test. |
| Skip the arithmetic preflight | Boundary refusal test with the oracle. |
| Accept debris on a claimed resource | Debris refusal test. |
| Drop the remaining-route test for an approach | Approach corridor test. |
| Use `routingGraph` in `networkStationsConnected` | Static admission test. |
| Use `routingGraph` in the arrival-chain test of `divertStart` | Arrival-chain test. |
| Skip the static connectivity switch | Static admission test: a bound trip is unbound. |
| Rebuild the blocked set lazily | Cache epoch test. |
| Release `faultHold` while it owns purpose 3 | W5 check after the clear. |
| Keep the record while the recovery travels | F12 test. |
| Search a free berth at lane evacuation | Lane evacuation test: the destination changes. |
| Skip the service claim surrender at fault start | Fault start claim test. |
| Call `redirect` from `rerouteToEndpoint` | Occupied reroute test: the pod becomes idle with riders. |
| Search the suffix to another berth | Endpoint test: the destination changes. |
| Skip the kept-prefix check | Trapped pod test. |
| Skip the detour check for an entry end | Banked entry reroute test. |
| Use `request.From` in `pickupAccess` | Transferred order test. |
| Accept an open route to an entry whose compatible berths are all blocked | Bank pickup access test. |
| Search access from any berth of the end station | Reachable end berth access test. |
| Use `berthFilterForStops` alone as the pickup filter during an incident | Repeated dispatch test: the trip is unbound and bound again on each tick. |
| Trust the cached `trip.route` or `Berths[0]` in rule 4 | Rule 4 continuation test. |
| Use `routingGraph` for a detour baseline | Detour baseline test. |
| Gate terminal reevaluation during incidents | Terminal reevaluation `Step` test: the pod stays at B1. |
| Let `endpointRoute` change the last lane of a buffered pod | Buffered reroute test: the buffer membership is lost. |
| Create an exclusion at the incident unbinding | Exclusion test. |
| Unbind through `withdrawService` | Pickup access test: the pod leaves service. |
| Drop the access gate of new assignments | Same-pass binding test. |
| Run the claim surrender without its gate | Marker-only parity test. |
| Gate the claim surrender on active records only | Two-pod one-berth recovery test. |
| Run the claim surrender before `dispatch` | Two-pod one-berth recovery test with parking again. |
| Release a claim of another kind than `claimService` | Claim surrender kinds test. |
| Require an incident owner for a surrender wait | Surrender test behind a healthy pod. |
| Skip the `BlockedBy` reset at a clear | Paused clear test. |
| Wrap a counter at the maximum | Counter limit test. |
| Write a fault member without the marker | Off-state golden. |
| Set a digest tag on a field inside `Faults` | Digest registry test. |
| Use the delta path `/groups/faults/value/active` | Prescan audit. |
| Drop a stream rule of section 13.5 | Matching codec test. |
| Apply `endTick > startTick` to an absent `endTick` | Codec test of a record without an end. |

## 17. Qualification gates

These gates would justify a maintainer decision to enable faults by default, which means that new scenario projects carry the fault marker and the inspector shows the control.
The default rate stays 0 until the maintainer chooses one in stage 7.

| Gate | Measurement | Pass |
| --- | --- | --- |
| Off parity | The off-state gates of section 14 on every preset. | Byte identity, digest identity, and identical states at every 600th tick. |
| Marker-only parity | The same runs with the marker and no fault. | Identical states apart from the marker. |
| Safety | A soak driver on LondonCentral and the rail-hub preset that starts and clears random pod faults and debris through the commands, at about 10 per simulated hour, for 24 simulated hours per seed, over at least 20 seeds. | `CheckContract` holds after every tick and every command. No owner conflict. No pod passes its fault cap or enters a fault footprint. The transition helper of section 10 holds. |
| Contract | The same soak, with the invariants F1 to F12, W1 to W5, and X1 to X2 checked after each tick. | No violation. No pod is withdrawn without `faultHold` (F11). |
| Conservation | The same soak. | `RequestID = Completed + Interrupted + queued + aboard` at every tick. |
| Rail | The same soak. | Each interrupted order with a `pending` rail record ends `unserved` with reason `interrupted`. A record that is already `missed` stays `missed`. An order with no rail record gets none. |
| Progress after the clear | After the last clear of each soak, the run continues with demand. The presets have parking and berth capacity, so the conditions of section 9.7 hold. | Within 30 simulated minutes, no pod has `faultHold`, no pod has an incident wait reason, and the waiting queue returns to the size range of a run without faults. A run that misses the gate lists each remaining wait with its reason and blocker. |
| Persistence | Saves at random command boundaries and tick ends of the soak, restored in the physical tier, and rewinds to random checkpoints. | Every save restores and passes `CheckContract`, or fails the physical tier only by the demotion rule of section 12.7. Every rewind replays exactly. |
| Performance | Step time of the soak against a run without faults, with 64 debris faults and 300 pod faults as the worst case. | Median step time within 10 percent, and no tick above twice the slowest tick of the run without faults. |
| Formats | The composed fixtures of section 16.4. | Every shape under its cap, with the measured cap if it was raised. |
| Web | The browser shell and editor with a marked project. | `mise run test:web` passes, and the marker checks refuse the cases of section 16.3. |
| Product | The product choices of section 20.2. | The maintainer has decided each one. |

The soak driver is a test tool.
It does not need `perHour`, so it runs before stage 7.

## 18. Patch order

Stage 2 starts after stage 1 patch 9.
Each patch compiles and passes the full suite on its own.
The off-state gates of section 14 run after each patch.

1. `sim`: Add the blocked route graph view, the synchronous cache epoch, the static connectivity switch, the static arrival-chain test, and the static detour baselines, with no fault source.
2. `sim`: Add fault records, the fault owner kind, the fault stage, the arithmetic preflight, durations, the saturating counters, `Clone`, and `Reset`.
3. `sim`: Suspend a faulted pod: `faultMoveStep`, the foreign fact members, the gates, the service claim surrender at fault start, evacuation, and the hold release rule.
4. `sim`: Add debris: the footprint, the preconditions, and `retainedOwners`.
5. `sim`: Handle obstructed pods: the wait reports, the reroute pass with `endpointRoute` and `rerouteToEndpoint`, `pickupAccess` with its gates, `pickupBerthFilter`, the rule 4 continuation check, and the claim surrender in `admit`.
6. `project` and `session`: Add the fault marker, the settings, the commands, the errors, and the digest entries.
7. `session` and `web`: Add the save and stream members, strict decoding with the stream rules, the prescan limits, the composed fixtures, the web marker check, and any cap raise that the measurement needs.
8. `view`: Add the inspector fault control.
9. `docs`: Describe faults in `docs/protocol.md` and `docs/operations.md`.

Patches 2 to 5 are internal.
Their tests call the operations directly, as stage 1 tests do.

## 19. Reuse of the parked fault contract

The parked contract is an uncommitted draft, `docs/vehicle-fault-contract-proposal.md` revision 2, against `5c90c9f`.
Its two Codex reviews and the holistic review stopped it.
This contract reuses the parts that fit stage 2, anchored again at `3c77045` and placed on the stage 1 API.

### 19.1 Reused

| Parked section | Here | Change |
| --- | --- | --- |
| 4.1 Blocked set | 8.1 | Lanes and berths as before, with a static resource index and a synchronous rebuild. |
| 4.2 Route searches | 8.2, 8.4 | The `blocked` member on `routeGraph` instead of `networkRouteInput`, so every reader of the graph gets it. Admission stays static through `staticConnected`, which also covers `podFitsRequest`. |
| 4.3 Caches | 8.3 | Same set plus `congestionRouteCosts`, without `approachStations`, which is a static index of the network (`internal/sim/drop_offs.go` `(*Simulation).stationsOnRouteForClass`). |
| 4.4 Replan pass | 9.2, 9.3 | Narrowed to one route-only reroute of a healthy pod to its own endpoint, at the same cadence. No classes, no withdrawal of a healthy pod, and no refuge. Trapped pods wait instead of reversing. |
| 6.1, 6.2 Pod on a lane and at a berth | 5.2, 6 | Same braking cap and berth footprint. The cap enters every motion proof through one step function, from parked section 5.8 item 1. |
| 7.1 State machine | 4.1, 5.5 | Same phases, now derived. Clear before evacuation. Evacuation needs rest. |
| 7.3 Start validation | 7.3, 12.4 | Same checks, with the claim refusal, the remaining-route refusal, and the arithmetic preflight. |
| 7.4 Scenario sampler | 12.2 | Mechanism only, with its own RNG stream. It lands in stage 7. |
| 7.5 Debris footprint | 7.2 | Same footprint from lane cells. Berth resources are excluded. |
| 7.7 Session events | 12.7 | Same table, with stage 1 restore tiers, the hold rule, and the demotion rule. |
| 8.2 Project settings | 12.1 | Same members. `perHour` is refused above 0 until stage 7. |
| 8.3 Commands, errors, and the digest rule | 12.3, 12.4, 13.2 | Same actions and messages, plus `invalid fault duration`, `debris meets a reserved resource`, and `incident limit reached`. The digest rule is the stage 1 registry. |
| 8.6 Prescan limits | 13.5 | The delta path is `/delta/groups/faults/active`, and semantic stream rules carry the record contract. |
| 10.4 Capability matrix | 1.4 | Unsupported targets and positive rates are refused before any change. |

### 19.2 Dropped

| Parked section | Reason |
| --- | --- |
| 4.6 Grant retraction | No stage 2 operation revokes a grant (section 6.4). A certified release is product choice P9. Revision 1 of this contract used it, and the first review moved it out. |
| 7.5 Pending debris resources | Debris starts only on free, unclaimed resources (section 7.3), so no resource passes from a pod to debris. |
| 5 Reverse motion | Stage 5. Rounds 1 and 2 found deadlock and certificate blockers in it. Stage 2 pods wait (decision 3). |
| 6.3 Compact recovery and the predecessor transition | Stage 6. Round 2, finding 1: compact recovery moved a stopped faulted pod. Stage 2 refuses compact and platoon members. |
| 6.4 Physical trains | Stage 4. Rounds 1 and 2 found certificate blockers. Stage 2 refuses coupling and approach members. |
| 4.4 `unbindPickups` | Replaced by stage 1 `withdrawService` and `releasePickups` for a faulted pod, and by the dispatch access test for a healthy pod (section 9.4). Neither changes an owner. |
| 4.5 `refuge` field and `Holding` activity | Stage 2 has no refuge. |
| 7.2 `StepInterruption` and `fault-interrupted` | Replaced by the stage 1 interrupted outcome and rail reason `interrupted`. |
| 7.2 Completion of riders at their destination berth | Decision 4: every evacuated party is interrupted. |
| 7.6 `f<generation>.<serial>` and `SetFaultGeneration` | Replaced by stage 1 IDs and `SetIncidentGeneration`. |
| 8.4 Pod fields `faultID` and `reversing` | No vehicle member is needed. `BlockedBy` and the `faults` group carry the ID. |
| 8.5 Saved IDs of 386 bytes and `reverse` state | Records use indexes. Reverse state is stage 5. |
| 8.5 Saved coordinate origin | Needed only for reverse targets, stage 5. |
| 9 Byte table | Replaced by section 13.6 against stage 1 section 11.7. |
| 4.7 Park-and-ride outcome | Stage 1 defines the ledger outcome. Park-and-ride keeps refusing the marker in stage 2. |

### 19.3 Parked review findings that must not return

| Finding | Where this contract closes it |
| --- | --- |
| Round 1, 1: digests change | Section 13.2 uses the stage 1 registry. |
| Round 1, 8: release before admission | No stage 2 operation revokes a grant. Debris release in the fault stage waits for `releaseCleared`. |
| Round 1, 9, and round 2, 8 and 9: pickup unbinding | Section 5.2 calls `withdrawService` at the first accepted fault. Section 9.4 unbinds a pickup of a healthy pod only through dispatch. Neither releases a berth claim. |
| Round 1, 10: refuge phase | Stage 2 has no refuge. |
| Round 1, 11, and round 2, 7: prescan | Section 13.5, from real envelopes. |
| Round 1, 12, and round 2, 6: bytes | Section 13.6, with indexes and the measured stage 1 headroom. |
| Round 1, 14: IDs after rewind | Stage 1 IDs. Test in section 16.2. |
| Round 1, 15: stage gates | Section 1.4. Tests in section 16.1. |
| Round 2, 1: a stopped faulted pod moves | F4. Compact members are refused. |
| Holistic review: phase changes inside the causing operation | Section 11, and the synchronous rebuild of section 8.3. |
| Holistic review: service cancellation is not physical release | Withdrawal and debris change no grant. |

## 20. Requirements and product choices

### 20.1 Settled requirements

A binding decision, the approved stage 1 contract, or a safety rule settles each row, so each has no alternative here.

| # | Requirement | Source |
| --- | --- | --- |
| S1 | Evacuation interrupts every active rider of the faulted pod, also a rider at its destination berth. | Decision 4; stage 1, section 8.2. |
| S2 | An evacuated pod on a lane recovers to its old destination on its original route, with no search for a free berth. | Stage 1, section 9.5, lane evacuation; decision 11. |
| S3 | A buffered faulted pod keeps its buffer membership and its grants. | Safety: no certified buffer exit exists. |
| S4 | A faulted pod keeps no pickup: its pickups go to other pods with the exclusion until boarding. A healthy pod loses a pickup only through the dispatch access test, with no new exclusion. | Stage 1, W2 and section 5; decisions 9 and 11. |
| S5 | Order admission stays static while faults are active. | Stage 1, section 4.3. |
| S6 | An obstructed pod keeps the order of its `Stops`. Skipping a stop needs a new stop rule and a later stage. | Stage 1, section 9.4. |
| S7 | Riders never leave a pod that is obstructed but not faulted, except at their stops. | Decision 4 names only the stopped pod. |
| S8 | A logical restore and a physical restore that would demote a faulted pod drop every fault, release `faultHold`, and keep the counters. | Maintainer decision of October 5, 2026; `AGENTS.md`. |
| S9 | Park-and-ride and `cmd/compare` refuse the fault marker. | Stage 1, open question 10. |
| S10 | The fault switch is the `faultContract` marker, which requires `incidentContract`. | Stage 1, section 11.2. |
| S11 | Moot in revision 3: stage 2 has no refuge. | Decision 11. |
| S12 | A pod in its fault recovery can fault again, and its hold is reused. | Stage 1 W5 and the hold rules; no new transition. |
| S13 | Debris on a berth or a berth node is out of stage 2 scope. A later stage can add it with a buffer claim rule. | Scope. |
| S14 | The record ends at its duration or clear, also while a fault recovery needs `faultHold`. | Decision 5. |
| S15 | A healthy pod is never withdrawn, and its route changes for an incident only to its current endpoint. | Decision 11. |
| S16 | Recovery after the last clear is live only under the conditions of section 9.7. Otherwise the system stays safe and reports the wait. | Second review; maintainer decision of October 5, 2026, 17:43Z. |

### 20.2 Product choices

The maintainer accepted the defaults of P1, P2, and P5 to P9 on October 5, 2026.

| # | Choice | Decision | Alternatives that were not chosen |
| --- | --- | --- | --- |
| P1 | Debris limits, and the refusal of claim conflicts | Accepted: at most 64 active debris faults, each at most 50 m long. Debris that meets any claim, also beyond the stopping distance, is refused. | Other limits, which move the byte budget of section 13.6. Accept a claim beyond the stopping distance and wait for its release, which needs a pending-ownership rule. |
| P2 | Reroute cadence | Accepted: every 300 ticks (5 s) while the blocked set is not empty, and at each new epoch and physical restore. | A shorter cadence, at a higher cost. |
| P3 | Refuge preference | Removed in revision 3. A refuge policy is a stage 3 product choice. | |
| P4 | Refuge resume | Removed in revision 3. A refuge policy is a stage 3 product choice. | |
| P5 | A fault on a pod that already has a pod fault | Accepted: refused with `pod already has a fault`. | Extend the duration of the existing record, or start a second, overlapping record. |
| P6 | Scenario rate units and duration distributions | Accepted: faults per simulated hour, with `fixed`, `uniform`, and `exponential` durations. Mechanism only; it lands in stage 7, and the rate is 0. | Other units or distributions. |
| P7 | Inspector controls and UI timing | Accepted: pod "Fault" and "Clear fault" buttons in stage 2. Debris drawing and a debris control in stage 7. | Commands only until stage 7, or debris drawing in stage 2. |
| P8 | Metric set | Accepted: the counters of section 12.5 and the wait reports. The full set in stage 7. Revision 3 removes the `refuges` counter with the refuge, so five counters remain. | More counters now, for example blocked seconds and delayed rider seconds. |
| P9 | Unused grants of a stopped pod | Accepted: kept until a certified release transition exists. | Retract them at rest with a deferred release at `releaseCleared`, as revision 1 specified. This needs its own certification. |
| P10 | A revocable destination claim of a faulted pod | Superseded by the amendment of section 1.6 (D3): the pod gives up its `claimService` claim after fault withdrawal. | |
| P11 | Unreachable occupied service | Removed in revision 3. An occupied healthy pod reroutes to its endpoint or waits. A refuge policy is a stage 3 product choice. | |

## 21. Round 1 resolutions

The first external review had two parts: an adversarial review of revision 1 (findings A1 to A14) and an independent design (items B-A to B-G).

| # | Severity | Finding | Resolution | Sections |
| --- | --- | --- | --- | --- |
| A1 | Blocker | A refuge pod could hold forever after the last clear. | Superseded in revision 3: stage 2 has no refuge (section 22). | 1.2 |
| A2 | Major | Lazy invalidation came after cache hits. | Every footprint change rebuilds the blocked set and clears the caches in the same operation, cached failures and `routeStations` included. The static switch reads the same rebuilt set. | 8.3, 8.4, 16.1 |
| A3 | Major | "Fault ahead" missed several permanent waits. | Superseded in revision 3 by wait reports derived at each tick (section 22). | 9.6 |
| A4 | Major | The bound-pickup reroute went through a path that refuses an assigned pod. | Superseded in revision 3 by the endpoint reroute and the pickup access test (section 22). | 9.3, 9.4 |
| A5 | Major | Occupied rerouting missed entry-ending routes and berth departures. | Superseded in revision 3 by the endpoint reroute, which covers both (section 22). | 9.3 |
| A6 | Major | Committed and trapped pods kept their pickups. | Superseded in revision 3 by the pickup access test (section 22). | 9.4 |
| A7 | Major | Debris had no explicit refusal for the future route of an approach. | Precondition 9 refuses a footprint on the remaining route of a coupling or approach member, past its grants, before any write. | 7.3, 16.1 |
| A8 | Major | Physical restore had no rule for a faulted pod that it would demote. | The physical tier fails; the logical tier then drops every fault where a fallback is allowed. Coordinator decision; the alternative of cancelling one record is rejected. | 12.7, 20.1 S8 |
| A9 | Major | Time and serial arithmetic could fail after a write. | Checked additions and serial exhaustion are preflighted before `nextIncidentID`, with a new error. Decoders reject negative values and an overflowing evacuation tick. Revision 3 adds saturating counters (N7). | 5.2, 7.3, 12.4, 12.5, 13.5 |
| A10 | Major | Stream validation was weaker than the record contract. | Required and forbidden members per kind, unique IDs, per-kind and per-pod counts, numeric bounds, and tick order, for full frames and replacement groups. Revision 3 completes the rules (N8). | 13.4, 13.5, 16.3 |
| A11 | Major | The test oracle could not show refusal atomicity or stopped immobility. | A clone comparison for every refusal, a transition helper for F3 and F4, and a mutation per guard and path. Revision 3 makes the helper compare pose and speed (N9). | 10, 11, 16.1, 16.6 |
| A12 | Major | The rail gate contradicted the stage 1 `missed` rule. | Only `pending` records change; `missed` stays; orders without a record get none. | 17 |
| A13 | Minor | Save headroom used the old stage 1 Express total. | Amended totals 129,516 and 373,116 and the reserve of 2,203,613. Express with coupling keeps 6,427,359 bytes after revision 3. | 13.6 |
| A14 | Minor | Section 20 mixed settled requirements with product choices. | Split into settled requirements (20.1) and product choices (20.2). | 20 |
| B-A | Adopted | One braking calculation for motion and for every foreign proof, frozen at onset before capture; arrival during braking keeps the suspension; the faulted entry-queue head makes no berth-grant attempt. | `faultMoveStep`, the deceleration bound, F3 and F4 with a test, and the buffer head gate. | 6.1, 6.2, 6.3, 10 |
| B-A | Adopted | Keep every grant of a faulted pod until a certified release. | Retraction is dropped from stage 2. | 6.4, 20.2 P9 |
| B-B | Adopted | Debris never revokes a claim. | Debris starts only on free, unclaimed resources, so the stopping-distance rule holds for every pod. Revision 1 retracted grants; that is dropped. | 7.1, 7.3, 7.4 |
| B-C | Adopted | Closure applies to every search, and cached failures and cost snapshots are invalidated. | Bank searches and `congestionRouteCosts` are named. Station maneuvers stay static. | 8.2, 8.3 |
| B-D | Adopted | Separate the timed mechanical fault from the service suspension that keeps `faultHold`. | The record ends at its duration or clear; the fault recovery is the stage 1 purpose with owner `faultHold`. F12. | 2, 5.4, 10 |
| B-D | Rejected | Cancel one pod record on physical demotion. | Coordinator decision: no partial recovery. | 12.7 |
| B-E | Adopted | Report fault braking, fault stopped, blocked by incident, and no forward route, with the incident ID. | Section 9.6. `BlockedBy` holds the fault ID; the `faults` group gives the pod or lane and the onset tick. Revision 3 drops "Holding at refuge". | 9.6, 13.4 |
| B-E | Not adopted | Wait-start tick and affected rider count in each report. | The saved `waitSince` and the rider lists already hold them. No new member. | 9.6 |
| B-F | Adopted | Debris before pods at restore, elapsed timers kept, braking restored as stopped. | Section 12.7. | 7.6, 12.7 |
| B-G | Adopted | The incident sampler has its own RNG stream and never draws from the demand RNG. | Section 12.2, with a matched-seed test. | 12.2, 14 |
| B-B | Not adopted | Snap the debris interval to cell boundaries and exclude node and junction cells. | The footprint already covers whole cells. Junction cells stay allowed, because the claim refusal makes them safe. Berth cells are excluded. | 7.2, 7.3 |

## 22. Later review resolutions

### 22.1 Round 2

The second external review of revision 2 had four parts: an adversarial review (the status of A1 to A14 and new findings N1 to N9), an own analysis, a blind design, and an adversarial check of the points where the designs disagreed (D1 to D3 and check findings 1 to 5).
Two rounds found a blocker in recovery liveness, and the findings shared root causes: retained claims with no owner release, a sticky obstruction state with an extra hold, and a custom reroute for each route shape.
The maintainer approved the narrowed shape and the D3 amendment on October 5, 2026 at 17:43Z.

| # | Severity | Finding | Resolution | Sections |
| --- | --- | --- | --- | --- |
| N1 | Blocker | A buffered recovery could deadlock after every clear: a withdrawn head cannot make a claim yield, and an empty pod behind it kept its claim on the only berth. | The waiting owner gives up its unused service claim in the first phase of `admit`, while an incident is outstanding. Two-pod one-berth test. | 9.5, 16.1 |
| N2 | Major | A retained destination claim of a faulted pod was missing from the classification. | The faulted pod gives up its service claim at fault start (D3). No classification remains. | 1.6, 5.2 |
| N3 | Major | Withdrawal disabled later reroutes. | Healthy pods are never withdrawn, and the reroute pass does not test service membership. | 9.1, 9.2 |
| N4 | Major | The sticky obstruction state had no cleanup at arrival, settlement, or clear. | No sticky state: each tick derives each report again. The clear resets each `BlockedBy` that names the cleared fault. | 5.4, 9.6 |
| N5 | Major | The pickup reroute used `request.From` and an implicit prefix. | Pickup access uses `legOrigin()`. The endpoint reroute builds the kept prefix plus the suffix explicitly. No reroute for pickups alone remains. | 9.3, 9.4 |
| N6 | Major | An entry-ending route could be unaffected while every berth of its station was blocked. | No classification remains. The failed terminal berth choice reports "Blocked by incident", and the pod waits. | 9.6, 9.7 |
| N7 | Major | Counters could overflow after a write. | Every counter saturates at `math.MaxInt64`. Restored-limit tests. | 12.5, 16.1 |
| N8 | Major | Stream validation had undefined and missing cases. | Presence-aware decoding with no null, `endTick > startTick` only when present, `startTick` not above the frame tick, `toMeters` not above the lane length, the same rules for full frames and replacement groups, and the base kept on a rejected delta. | 13.5, 16.3 |
| N9 | Minor | The immobility helper compared route coordinates, not physical motion. | The helper compares pose and speed, and route coordinates only while they stay the same; berth settlement and the clear end that comparison. | 10, 16.1 |
| D1 | Decision | Rerouting of occupied pods. | One route-only installer to the current endpoint, with the kept prefix, the detour limits of the actual endpoint, and no `redirect`. | 9.3 |
| D2 | Decision | Unreachable bound pickups. | A separate access test, used only while the blocked set is not empty. Dispatch unbinds with no new exclusion, and the assignment, hold, swap, and boarding paths use the same test. | 9.4 |
| D3 | Decision | The service claim of a faulted pod. | Approved amendment to stage 1. | 1.6, 5.2 |
| Check 1 | Blocker | The surrender needed a gate that is off in the off state and stays on through the recovery. | The gate is an active record or a pod with `faultHold`. A faulted pod gives up its claim at fault start; a recovery hold does not exempt a pod. No motion gate tests `withdrawn`. | 6.2, 9.5, 14 |
| Check 2 | Blocker | The surrender had to follow every claim producer and cover the complete buffer path. | The first phase of `admit`, after dispatch parking, redistribution, and positioning, with no parking claim before the grants. It covers the terminal berth choice and the complete berth path of a buffer head, and keeps the buffer rollback and yield rules. Parking again after a surrender is stated. | 9.5 |
| Check 3 | Major | An incident owner of the next resource is not a complete wait detector. | The wait derivation covers each attempted continuation, and a pod behind a healthy pod that waits. | 9.5, 9.6 |
| Check 4 | Major | Operational searches had to stay separate from structural checks. | The arrival-chain test of `divertStart` and admission connectivity stay static. The multi-target search is operational. | 8.4, 8.5 |
| Check 5 | Minor | The refuge rationale misstated the purpose mapping. | Purpose 2 stays an unused stage 1 capability. | 1.2, 20.2 |
| Liveness | Required | Graph reachability alone cannot promise recovery. | Conditional liveness with the wait table. | 9.7, 17, 20.1 S16 |

Revision 3 deletes these parts of revision 2, each replaced by a row above: `obstructionHold`, the vehicle fields `obstruction` and `replanned`, the replan classes and goals, `rerouteService`, `reroutePickup`, withdraw and wait, the refuge recheck, the obstruction states, and the refuge rows of sections 11, 12, 16, and 20.

### 22.2 Round 3

The third external review checked revision 3.
It found no blocker, and it confirmed recovery liveness, the safety of D3, the off state, and the formats.

| # | Severity | Finding | Resolution | Sections |
| --- | --- | --- | --- | --- |
| R3-1 | Major | Pickup access accepted an open route to a bank entry whose compatible berths were all blocked, and it searched from any berth of the end station. | Access needs a complete forward continuation from the actual endpoint through a compatible pickup berth, with the bank and onward constraints. A busy pod searches from the node where it becomes available. Owners are ignored. | 9.4, 16.1, 16.6 |
| R3-2 | Major | Operational routing changed the denominator of the detour limit. | The direct-distance and legacy baselines read the static graph while the blocked set is not empty. Candidate travel and onward feasibility stay operational. | 8.2, 8.5, 14, 16.1, 16.6, 18 |
| R3-3 | Major | Terminal reevaluation moved a pod away from its blocked berth. | Not gated, as the coordinator decided: reevaluation moves an uncommitted pod to another free, reachable berth of the same station, which keeps the station, the riders, and the stops. The pod waits only when no such berth is free. | 9.3, 9.5, 9.7, 16.1, 16.6 |
| R3-4 | Minor | The installer could clear the buffer membership. | `endpointRoute` refuses a new last lane for a buffered pod, and the pod waits. | 9.3, 16.1, 16.6 |
| R3-5 | Minor | The transition helper refused valid braking. | Distance equality applies only to a pod already at rest. During braking, the distance never decreases and stays at most the cap. | 10, 16.1 |

### 22.3 Check of revision 4

A Sol check of revision 4 confirmed that R3-2, R3-4, and R3-5 are closed and that the unchanged rule for a pod idle at the origin is sound.

| # | Severity | Finding | Resolution | Sections |
| --- | --- | --- | --- | --- |
| R4-1 | Major | R3-1 was only partly closed: on a network without class restrictions, a pod that failed rule 3 could win the same assignment again on each tick. | `pickupBerthFilter` enforces compatible pickup berths in candidate selection, installation, the vehicle berth filter, the holds, and `assignedPickupFitsRequest`, while the blocked set is not empty. The idle-origin exception stays. Repeated dispatch test and a mutation row. | 9.4, 14, 16.1, 16.6, 18 |
| R4-2 | Minor | Rule 4 trusted the cached `trip.route` and the `Berths[0]` shortcut of `finishEstimate`. | An incident-only continuation check searches the passenger leg and the final berth. The off-state branch does not change. Tests of a stale route and of a blocked first berth. | 9.4, 14, 16.1, 16.6 |
| R4-3 | Minor | Section 9.1 and one fixture still said that a pod with a blocked berth end always waits. | Section 9.1 exempts terminal reevaluation, and the fixture applies to a committed pod or a pod with no free, reachable alternative. | 9.1, 16.1 |
