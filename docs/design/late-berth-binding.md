# Late berth binding for empty moves

Status: draft for the maintainer, 2026-11-24.
Nothing is built or decided, and no run has tested this note.
A Codex review found two blockers in the first candidate rule, and section 4 withdraws it.
The maintainer asked if a pod can get its berth at the station approach and not at the start of its journey, and if idle pods can vacate when a pod joins the approach.
The question comes from the [HX11 trace](../studies/incident-soak.md#the-late-record-of-the-wharf-timing-seed).
The smaller options for the same failure are in the [emergency berth claim proposal](../contracts/emergency-berth-claim-proposal.md).

## 1. Current behavior

### 1.1 When a pod gets a berth

| Move | Berth | Owner of the berth resources before the approach |
| --- | --- | --- |
| Passenger trip | At the final lane of the road route, or when its next reservation reaches the route end (`assignTerminalBerth`), or early with platoons (`earlyBerthChoice`) | None |
| Pickup | At dispatch (`sendPickupMatching`), and `reevaluateTerminalBerth` can change it | None |
| Emergency unload | At the station choice (`setOperationalDestination`), and `reevaluateTerminalBerth` can change it | None |
| Empty recovery after an evacuation | The route, the destination, and the owners of the pod before it (`operational.go`) | Those that it had |
| Operational target of another purpose, also a refuge | At the target (`setOperationalDestination`) | Can claim |
| Pickup transfer, depot move-up | As the pickup or the depot | None |
| Parking in a depot | At the entry (`bindDepotArrivals`) | None |
| Released pod to a berth | At the diversion (`parkReleased`) | The pod |
| Parking in a berth, guarded positioning, guarded bump, berth clearing | At the departure (`startEmptyMove` with `reserveBerth`) | The pod |

The last two rows are the remote claims.
`reserveBerth` has five callers: `park`, `clearToPassengerBerth`, `positionGuarded`, `guardedBumpToDeficit`, and `guardedBumpToParking`.
`parkReleased` writes the owners itself.
A restore also rebuilds claims (`claimDestinations`).
A target, a service claim, and an admission grant are three things: a target writes no owner, a claim is an owner that the pod does not use yet, and a grant is an owner of reserved track.

### 1.2 What a remote claim gives

- The claimant has a place at the end of the move.
- Two exclusive claims on one berth cannot exist at one time.
  Pickups can share a target, and a claim can yield.
- Guarded positioning reads claims and targets as supply and as busy berths (`guardedSupply`, `busyBerths`).
- A claim is saved (`ClaimsDestination`), and a restore gives it back (`claimDestinations`).

### 1.3 What a remote claim costs

- It keeps a free berth from each pod that a yield rule does not cover.
  An emergency pod is the case of the trace.
- `yieldRelocationClaims`, `surrenderWaitingClaims`, `parkUnclaimedReleased`, and the claim part of `claimKind` exist to release claims again.
- It was one of the 3 conditions of the [berth claim deadlock](../contracts/berth-claim-deadlock-proposal.md).

## 2. Idea

An empty move that does not go to a depot goes to a station and not to a berth.
It binds a berth when it reaches the approach, and it owns nothing before that.
This is the depot rule of `13dee8f2`, on stations with more than one entry lane.

An idle empty pod also leaves its berth when a pod on the approach needs it.

## 3. Options

### 3.1 What replaces the claim

| Option | Rule | For | Against |
| --- | --- | --- | --- |
| A | Station target. The move names a station and no berth, and binds at the approach. | No remote owner and no remote target. `berthAvailableFor` then has fewer far targets. | `choosesBerth`, `divertStart`, and the restore need a rule for such a pod. |
| B | Berth target with no owner, as a pickup has today. | Smallest change: drop the owner writes. | `berthAvailableFor` and `berthTargeted` still refuse a berth that a far pod names. The study of the [LF block](../studies/shared-track-batch.md#why-the-head-pod-waits) found this refusal in front of the queue head. |
| C | Keep the claim, and make it expire when the pod is more than a distance from its berth. | Keeps most of the state. | A distance constant, and a claim that comes back. |

### 3.2 What keeps two moves from one place

| Option | Rule | For | Against |
| --- | --- | --- | --- |
| A | A count: a move starts only while the free berths of the station outnumber the other unbound empty moves that go to it (`depotGoal`). | A known rule with a depot precedent. It holds no berth. | A passenger pod can still take the berth, so the count is not a promise. |
| B | No limit, and each pod re-parks when it arrives. | Simplest. | Empty distance, and a queue of empty pods on the approach. |

### 3.3 Where it binds

| Option | Rule | For | Against |
| --- | --- | --- | --- |
| A | The last point before the pod reserves a lane that leaves the station entry, in arrival order. | `divertStart` refuses a diversion after that point, so it is the last point at which the pod can go elsewhere. | A station has several entry lanes and banks, so an order must be defined for pods in one tick. |
| B | The final lane, as passengers. | The existing code. | The pod can be past the point where it can divert. Free choice on the final lane was rejected for platform stations, because it can make a circular wait ([series berths](series-berths.md#32-berth-choice-and-binding)). |

### 3.4 When no berth is free

| Option | Rule | For | Against |
| --- | --- | --- | --- |
| A | The pod goes to the nearest free place (`parkReleased`), and waits where it is when there is none. | The depot rule. | A pod that waits holds road track, and a pod with riders can be behind it. |
| B | A, and an empty pod with no place keeps a parking place that it counted at departure. | The pod always has a place. | It is a remote claim again, on parking only. |

### 3.5 When idle pods vacate

| Option | Rule | For | Against |
| --- | --- | --- | --- |
| A | As today: after an arrival waits with "Berth occupied" for that pod. | No change. | A stop of the arrival. |
| B | When a pod binds, or chooses, a berth that has an idle, unassigned, empty pod in service, that pod starts to leave. | The berth can be free before the arrival stops. | It can move a pod for an arrival that then changes its berth. It needs a place for the idle pod. |
| C | When a loaded pod joins the approach and no berth that it can use is free, one idle pod leaves. | It also covers the sibling berth of the trace. | As B, and it needs a choice of the pod and a limit of one for each arrival. |

## 4. Candidate rules

### 4.1 Withdrawn: a station target with no berth

The first candidate was option A of each of 3.1 to 3.5.
The review withdrew it for these reasons:

- It can make a circular wait.
  An unbound empty pod at the head of an approach must pass `berthAvailableFor`, which refuses a berth that a pickup behind it targets.
  `parkReleased` excludes the same berth.
  Neither pod owns a claim, so the surrender of suspension section 9.5 does not break the wait, and the berth is free all the time.
- A bind point in front of the station entry is not a mechanism that exists for each station.
  The depot rule works because a depot has one incoming lane, no banks, and one class set for each slot.
- The count of 3.2 A is a throttle and not an allocation, because berths differ in classes and banks.
- A released pod with no claim is sent to park again at each pass, and a guarded move loses the yield that ends it.

### 4.2 Direction: a provisional berth

A move keeps a destination berth and a route to it, as today, but the berth is a planning hint.

1. The callers of `reserveBerth`, and `parkReleased`, write no owner.
2. The hint has no exclusion right against a pod that is at the approach: `berthAvailableFor` does not refuse a berth for a hint of a pod that is not at that approach.
   The hint still counts for guarded positioning and for the count of 3.2 A, as a dispatch limit that is not a promise.
3. Admission takes the berth, as for a pickup.
   `reevaluateTerminalBerth` changes a berth before the pod commits to its branch, and it keeps the reserved track.
4. A pod at the approach is in front of a pod behind it.
   The order on the approach decides which of two hints wins, and an emergency intent goes first.
5. A pod whose hint is taken, and that has no other berth, parks again.
   A released pod has an explicit state for "needs a search" that is separate from the ability of dispatch to take it.
6. An idle pod vacates on an approach event (3.5 B or C), through an operation that names the idle pod, and not through `BlockedBy`.

This needs no station-only saved form for each move, because the destination stays.
It is close to 3.1 B with changed readers of the target.
It keeps most of the current restore rules.
The ordering of rule 4, the states of rule 5, and the event of rule 6 are not designed yet.
Rule 2 changes the readers that the berth claim deadlock contract added, so that cycle must be proved again.

### 4.3 What the trace can support

The direction removes the remote owner that the trace shows in front of the emergency pod, if that owner was an empty move of the last two rows of 1.1.
It does not say that the pod is admitted at 45 s, or where the far pod then goes, because a grant fails when any resource of its request has an owner, and an earlier change can move both pods.
These are measurements.

### 4.4 What it does not change

- The decision of October 4, 2026, that emergency priority never revokes a claim: a hint is not a claim.
- The choice of emergency station.
- Emergencies can still wait for an existing grant or for capacity.
  "Does not wait" means that no remote plan of an empty pod makes an emergency wait.
- Depots.
- Pickups, except that rule 2 changes how their targets read.
  The `LF` block has its own study.

## 5. Failure modes to prove

| Case | Risk | Check |
| --- | --- | --- |
| A pickup behind an empty head targets the only free berth | The two wait for each other, with no claim. | This exact fixture, also with an emergency behind the empty head. |
| Every parking place is full | An unbound pod waits on the road in front of loaded pods, and they can wait for a berth that the idle pods cannot leave. | A small network with full parking, full berths, and a loop of approaches, run until all pods move. |
| Two pods reach the bind point in one tick | They bind one berth. | A fixed order: one bind for a station in each tick, in a deterministic order. |
| The station has banks | The free berth is in a bank that the entry lane does not reach. The bank choice is made while the route is built. | A bank fixture with the path to each berth. |
| The pod is linked | A linked pod cannot divert. | Bind before the link reaches the entry, or keep the pod out of a link. |
| The bind point is behind the pod after a restore | The pod cannot bind or divert. | `noBerthDemotes`, as for a pod with no berth that does not choose. |
| A fault closes the berth after the bind | As a pickup today. | The existing fault tests. |
| An emergency pod and an empty pod bind in one tick | The empty pod takes the berth. | The emergency intent binds first. |
| Mixed berth classes or banks | A count passes a pod that cannot use the free berth. | A fixture with a Compact-only berth and one that takes both, and one for each bank. |
| A pod re-parks again and again | Empty distance without progress. | A limit or a cooldown, and a test that the pod ends in a place. |
| A released pod that a trip takes | A trip takes a pod with no berth. | `pickupRouteWithAssignments` from a pod with no destination berth. |

## 6. Contracts and formats

| Place | Change |
| --- | --- |
| Service transitions, sections 4.3 and 6.2 | `yieldRelocationClaims` has less to yield. The rules stay for claims that remain, or go if none does. |
| Suspension, sections 9.4 and 9.5 | Berth clearing needs a new rule for an approach event (9.4). The claim surrender has no subject for these moves (9.5). |
| Berth claim deadlock | Option A of that contract refuses a berth that a moving pod targets. It must be read again for moves with no berth target. |
| Saved state | With 4.2 the destination stays, so a new form is not certain. `ClaimsDestination` is also true after ordinary admission, so it keeps a writer. A departing empty pod with no destination berth is refused outside the depot case, and a traveling one can be demoted or kept by its reservation. Decide the version after the matrix of activity and frontier exists. |
| Golden files | `TestPresetBehaviorParity` and `TestLondonExpressBehaviorParity` can move. Find which outputs change before a new golden set. |
| Qualification | HX, SX, and D rows that simulate traffic run again. |

## 7. Order of work

1. Settle the scope and what an emergency is promised.
2. Trace one unchanged run of each London preset: which moves own a remote berth, how long, how far away the owner is, and who waits for it.
   It is a run with no behavior change.
   It shows if the trace of HX11 is an outlier.
3. Complete the states and the restore matrix of 4.2, and review them.
4. Approve the rule or an experimental exception.
5. Small fixtures of section 5 for order, progress, and restore, each failing under its mutation.
6. A pilot as an uncommitted patch on the incidents arm of HX11 seed 1, and on one `LF` and one wharf run.
7. Find the cause of each parity difference, and then qualify again.

A failure of any safety check stops the work.
If step 2 shows few remote claims with a waiter, option A of the [emergency berth claim proposal](../contracts/emergency-berth-claim-proposal.md) is the better size of change.

## 8. Decisions for the maintainer

| ID | Question | Options | Recommendation |
| --- | --- | --- | --- |
| LB-D1 | Continue | A: step 2 only, then decide. B: steps 2 and 3. C: stop, and use option A of the emergency note. | A, because it is the cheapest step and it decides the size |
| LB-D2 | Direction | 4.2, or another option of section 3 | 4.2 |
| LB-D3 | What an emergency is promised | A: no wait for a remote plan of an empty pod. B: also no wait for capacity. | A, because B needs another capacity rule |
| LB-D4 | Idle pods vacate | 3.5 A, B, or C | Decide after step 2, then C with its own measurement |
| LB-D5 | Pickups | A: only rule 2 of 4.2. B: no target. | A |
