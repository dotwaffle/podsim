# Late berth binding for empty moves

Status: closed, not pursued.
The maintainer decided this on 2026-11-24, after the synthesis of section 9.
Nothing is built, and no run has tested a candidate of this note.
Two Codex reviews each found two blockers, in different candidate rules.
No candidate of this note is sound as written, and section 4.8 groups the findings by cause.
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
Section 4.5 has the states and the restore matrix.
The event of rule 6 is not designed yet.
Rule 2 changes the readers that the berth claim deadlock contract added, so that cycle must be proved again.

### 4.3 What the HX11 trace can support

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

### 4.5 States and restore matrix

This part is on paper only, and the second review found that it is not sound (section 4.8).
It stays as the record of the attempt.
It makes an empty move with a hint behave as a pickup does today, which has a destination berth and no owner.

A pod is **committed** to a berth when it owns a resource of that berth, or when its reservation reaches a lane that leaves the entry of its destination station.
`divertStart` uses the same boundary.
This reads the reservation, so it needs no saved state.
A pod behind another on one lane cannot be committed before it, because the lane cells belong to the pod in front.
So a committed pod is the one that is ahead on its approach, and "in front" needs no other order.

| State | Meaning | Berth resources | Leaves by |
| --- | --- | --- | --- |
| H0 | `DepartingEmpty`, with a hint | Owner of none | First grant: H1. Trip or other plan takes the pod: redirect. |
| H1 | Moving, not committed, can divert | None | Commitment: H2. Displacement: H4. A trip takes the pod. |
| H2 | Committed, no berth owner yet | None, or some | Grant of the berth cell: H3. It waits as a pickup does, and `clearBlockedBerths` moves an idle blocker. |
| H3 | Granted | Owner | Arrival. |
| H4 | Displaced, and a search is due | None | `parkReleased` finds a place: H1 with a new hint. It finds none: H4 again, after 1 s. |

Displacement: the pod is in H0 or H1, and its berth has an owner other than it, or another pod is committed to that berth, or a fault closed it.
The check runs at the place of `yieldRelocationClaims` in `redistribute`.
The pod searches with `parkReleased`.
Guarded moves, released moves, and plain moves go through one path.
A plain claimant today keeps its route after a yield, so this is a change.

`berthAvailableFor` then refuses a berth for an owner or for a committed pod, and not for a hint.
This also changes pickups and loaded pods, and it is the test that the LF study named.
`berthTargeted` and `closedBerthNodes` keep reading hints, as planning limits for new empty moves only.

The planned bit of `ClaimsDestination`: today it means that the pod owns its destination berth.
Here, a released pod that parked again needs a bit that says that it has a plan, so that `parkUnclaimedReleased` does not search at each pass.
The same member can hold that meaning, as "the pod has a plan for its destination", and the restore then sets the bit and no owner.
The JSON form does not change, and its meaning does, so the state version moves by the project rule for a changed meaning.

| Saved pod | Today | With 4.5 |
| --- | --- | --- |
| H0 with a destination | `hasDestination` holds. `claimDestinations` gives the owner back. | Valid as today. The bit is set, and no owner. |
| H1 | The route must end at the berth (`routeEndsMatch`). The claim comes back if the berth is free. | Valid as today. No owner. The berth can belong to a pod that was restored first, and the pod is then displaced at the first pass. |
| H2, no owner | As a pickup. | As a pickup. `noBerthDemotes` does not apply, because it has a destination. |
| H3 | The reservation gives the owner. | As today. |
| H4 | Not a state: a pod always has a berth or a depot entry. | Has a destination, with the bit off. Restores as a released pod without a claim does. |
| Idle or other | No change. | No change. |
| Demotion (`placeDemoted`) | Candidates are the destination, the origin, then the stations. | As today. |
| Frontier | `derivedFrontier` reads `ClaimsDestination` only at a depot entry, to tell a granted slot from a bound one. | A pod bound to a depot slot must keep the owner meaning there, so the planned bit must not apply to a depot slot. |

Open in this design:

- Full parking: an H4 pod that finds no place, or an H2 pod in front of loaded pods, has no exit.
  A fixture must show that the loaded pods still move, or the design needs a rule for it.
- Linked pods cannot divert, so a linked H1 pod is not displaced until the link ends.
- A pod in H2 that is an emergency target of nobody can still hold the approach for as long as its berth is taken.
- `busyBerths` and guarded supply read hints.
  A displaced pod must leave the count at the same tick.
- The surrender of suspension section 9.5 has nothing to do for these moves, but it still runs for a granted berth that is not used yet, if one exists.

### 4.6 A smaller alternative: an admission yield

The second review found that this is not sound as written (section 4.8).

`grant` can let a request take a berth resource whose owner holds it only as a service claim, when that owner is not committed.
The owner is displaced as in 4.5.
The claim stays as a plan for every other reader, and the saved form and `ClaimsDestination` keep their meaning.

- For: no change of `berthAvailableFor`, of the planned bit, or of the restore.
  It serves emergencies, loaded pods, and pickups with one rule.
- Against: it takes a claim from a pod that did not give it up, so it needs the approval of option A of the [emergency berth claim proposal](../contracts/emergency-berth-claim-proposal.md), for every pod.
  The far pod can be displaced late, on the tick of the request.
- It is option A2 of that proposal, for all pods.

### 4.7 The trace of remote claims

A trace of 2026-11-24 sampled each claim once a second, on one run of each London preset: the `incidents` arm of the soak, seed 1, 4 simulated hours, with the checks, the saves, and the rewinds off.
The patch was a scratch copy of `81738caa`, with no behavior change, and a fixture shows that its detector records a waiter.
"Plain" is a claim that is neither guarded positioning nor a released pod that parked again.

| Preset | Plain claims to a passenger berth | Seconds, p50 and p95 and max | Distance at the start (m), p50 and max | Plain claims to parking | Released claims, all | Rebalancing claims |
| --- | ---: | --- | --- | ---: | ---: | ---: |
| `london-central` | 0 | | | 0 | 960 | 0 |
| `london-full` | 138 | 525, 948, 2,169 | 8,482, 36,307 | 0 | 304 | 84 |
| `london-full-express` | 135 | 528, 1,109, 1,660 | 9,255, 33,403 | 5 | 306 | 2 |
| `london-full-shared-express` | 200 | 487, 944, 2,181 | 9,277, 42,789 | 2 | 617 | 2 |
| `london-full-shared-express-wharf` | 235 | 511, 1,212, 3,013 | 9,661, 42,169 | 10 | 1,000 | 1 |

- The detector had zero matches in the samples of the 5 runs.
  A waiter is a pod with "Berth occupied" whose destination berth is the claimed one and whose reported blocker is the claimant.
- Emergency pods waited for a berth 0, 3, 12, 29, and 57 s in total, in the order of the table, as sampled counts, and no sample matched a claim.
- The detector is narrow.
  `grant` reports only the first conflicting resource, a conflict on the berth node reads as "Junction occupied", an incident report can replace the claimant, and the detector does not see an indirect queue, a conflict at an intermediate berth, a reroute that avoids the claim, or a wait between samples.
  So the trace does not show that no pod waited because of a claim, and it does not show that claims do no harm.
- It shows that long and far claims are common.
- It did not log why a claim ended, or why a move went to a passenger berth 8 to 9 km away, and a claim open at the end of the run is counted at its length so far.
- The HX11 wait is one record in a wharf run with the checks on, and the trace has one seed and one arm.

### 4.8 Findings of two reviews, by cause

| Cause | Findings | What it means |
| --- | --- | --- |
| The approach has no order in the model. | A bind point exists for depots only. A pod that is committed is not ahead when several approach lanes join. The boundary of `divertStart` is not the station entry alone, with banks and cut routes. | A rule that says "the pod in front takes the berth" needs a new queue or arbitration for each station, with a proof. |
| A claim is three things in the code: an exclusion, a plan, and a marker of the lifecycle of the move. | A released pod without an owner searches at each pass. A guarded move ends on a yield. A hint does not pass `choosesBerth`. `ClaimsDestination` is the owner. `grant` cannot displace a pod whose intent is queued. | Removing the owner needs a separate state for each, in the moves, the restore, and the admission order. |
| Nothing promises a place. | Full parking, a displaced pod at the head of an approach, a count that ignores classes and banks. | Late binding removes the only guarantee that an empty pod has a place, so capacity needs a rule of its own. |
| The evidence is thin. | One waiter in one run, and a detector that misses most kinds of wait. | No measurement shows the benefit for ordinary traffic. |

The line of soundness that the code gives today is this: a claim is released by the existing yield, before `admit`, for a pod that brings a passenger or by the owner itself.
A rule that adds one more pod to that yield, with the same lifecycle, stays inside it.
Option A of the [emergency berth claim proposal](../contracts/emergency-berth-claim-proposal.md) is such a rule, and its cost is a decision of the maintainer and not a new mechanism.

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
The trace of step 2 did not settle the size.
Option A of the [emergency berth claim proposal](../contracts/emergency-berth-claim-proposal.md) is the smaller change.

## 8. Decisions

LB-D1 is closed: the maintainer chose to record the synthesis and not to pursue late binding.
No other decision of this note is open.

## 9. Synthesis

Three opinions were compared: an analysis written first, a blind design with the earlier decisions lifted, and an adversarial check of both.

### 9.1 Agreement

- The full design is not justified by the HX11 record.
  It takes away the only promise that an empty pod has a place, and replaces it with a promise of capacity that nobody has proved.
- The smallest credible fix is option A of the [emergency berth claim proposal](../contracts/emergency-berth-claim-proposal.md).
  No run has shown that it fixes the record.
- `grant` must not redirect a pod, because `admit` has already built the intents.
  A displacement belongs before `admit`, as the existing yield is.

### 9.2 Differences

| Question | First analysis | Blind design | Check |
| --- | --- | --- | --- |
| What promises progress | A static audit that each divert point has a way out | A drain schedule that each admitted move keeps | Neither is needed for HX11. The static audit does not show that resources drain. The cost and permissiveness of the schedule are unknown. |
| Approach order | The owner of the track | An explicit approach controller | Ownership does not give fairness or progress, and a controller is not shown to be necessary. |
| Place to go | A cleared pod takes the first free passenger berth in network order, and the nearest could be better | Not raised | Plausible and unproved, see 9.3 |

### 9.3 The nearest passenger berth

`clearToPassengerBerth` is the last fallback of berth clearing.
It tries the station of the pod, and then the other stations in network order.
So it can send a pod past nearer berths.

- The check found another producer of the same-looking claims: a depot arrival with no eligible slot, which `bindDepotArrivals` sends to `parkReleased`.
  The trace of 4.7 cannot tell them apart.
- That parking is usually full was not measured.
- A variant that keeps the local pass, ranks remote berths by the preferred route cost, and keeps the refusal of a targeted berth needs no change of the claim rules, and needs approval as a change of ordinary traffic.
- It does not solve a block with no eligible place.

A matched pilot of the control, option A, this variant, and both is the next evidence.

### 9.4 What the opinions did not settle

- Whether any rule can promise progress at full parking with loaded pods behind an empty one.
- The other three causes of 4.8.

To reopen: a demonstrated failure of physical capacity after a small fix, and a stated liveness property before any mechanism.
