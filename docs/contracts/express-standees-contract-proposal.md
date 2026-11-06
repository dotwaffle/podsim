# Express standees contract proposal

Status: decisions 1 to 13 accepted by the maintainer on 2026-11-05, as recommended.
Status 2026-11-05: the tick rate is now 30 Hz, `stateVersion` is 11, `StreamVersion` is 9, and the golden files are `state_v11.json` and `state_v11_members.txt`, so the values of 10, 8, and `state_v10` below are history.
Status 2026-11-05: the plan cap sits in the ordinary move step, so the fault step does not take it, and the floor at `speed - max × dt` also covers the result of `speedBeforeLane` (section 3.2).
The restore bound of 180 ticks in section 6 is 3 s, which is 90 ticks at 30 Hz.
The maintainer also approved the revision of sections 4, 5, and 7.1 after the external review of section 4.10.
No product code changes with this document.
It was drafted against `b2262f6`, and each `file:line` reference is for that tree.
The [evidence record](../studies/express-standees.md) holds the research, the sources, and the measurements.
All evidence comes from scratch overlays that did not land.

## Summary

| Question | Answer |
| --- | --- |
| Can Express become a 6 m vehicle with standees? | Yes. A body of 6 m, 12 seats, and 8 standee places keep the total of 20, so no count limit, byte limit, or format member changes. |
| Does the lower envelope need a new separation rule? | No. Each pod stops inside the cells that it owns, so the separation does not depend on the braking of another pod. The kernel, the reservation horizon, and the estimates need a value for each class. |
| Is the shorter curve tail sound? | The same-lane bound of section 4.3 holds, and the external review confirmed it. That review also found a fault at plane transitions in the existing audit, independent of the tail, and the claim of section 4.2 depends on its fix. The proof is a written argument with numeric checks, and it is not machine checked. Section 4.7 lists what it does not cover. |
| What does the tail do for `rail-hub`? | With all classes, seed 1 to 3 served 98, 99, and 102, against 1, 15, and 3 with the old tail and 102, 104, and 106 for the plain preset. The 2-hour run served 426 against 429. There were no separation violations. |
| Is the deadlock gone or moved? | It is gone on `rail-hub` in these runs, because the tails of its fans fall to at most 22.12 m. The new tail stays under the lane length plus `2h`, which is 0.5 m on a lane of up to 256 m and at most 0.2 percent of a longer lane, unless the lane passes the budget of section 4.1, so a pod cannot hold a station exit past the next station. Other ring locks stay possible, as `docs/model.md` says of each saturated network. |
| Is a separate deadlock rule necessary? | Not on this evidence. Section 4.8 keeps the warning and adds a progress gate. |
| Recommendation | Land the audit fix of section 4.5, then part 2 by itself. Then land part 1 with a planned braking of 0.8 m/s², a maximum service braking of 1.15 m/s², and an acceleration of 1.3 m/s², after decisions 5 to 10. |

This proposal also reads "per-class envelope" in two ways.
The motion envelope is the acceleration and braking of a class, and section 3 changes it.
The body envelope is the 6 m radius circle, and section 2.3 keeps it.

## 1. Why

Express at `b2262f6` has 20 seats in a 10 m body with an acceleration of 2 m/s².
The seat density of 2.00 per meter fits real seated vehicles, but the [research](../studies/express-standees.md#vehicle-sizes) shows that real vehicles for 20 to 24 passengers are 4.7 to 6.0 m long, and that they get there with standees.
To represent such a vehicle, the model needs separate seat and standee counts, a load policy, a dwell that depends on the load, and a motion envelope that a standing passenger can tolerate.

Separately, [the Express qualification](../studies/express-qualification.md#throughput-on-curved-station-lanes) records that `rail-hub` stops with all classes on its lanes.
`largePathTail` (`internal/sim/profile_geometry.go:121`) gives a tail longer than the lane on 70 of its 96 curved lanes, with 299 to 1,933 m on lanes of 189 to 456 m.
A standee vehicle on these lanes would stop in the same way, so it has no use until the tail changes.

## 2. Part 1: the standee profile

### 2.1 Counts

| Value | At `b2262f6` | Proposed | Source |
| --- | --- | --- | --- |
| Body length | 10 m | 6 m | Rivium Gen 2, 6.0 m |
| Seats | 20 | 12 | Rivium Gen 2 |
| Standee places | None | 8 | Planning load 20 of Rivium Gen 2, from 12 seats and 12 standee places |
| Total | 20 | 20 | No change |
| Largest new party under `express-v1` | 20 | 20 | No change |
| Maximum width | 2.5 m | 2.5 m | Real vehicles are 2.03 to 2.10 m. The proof uses the envelope, so a narrower value has no effect. |

`VehicleClassSpec` (`internal/sim/vehicle_class.go:21`) gets `Standees` and a method `Capacity`, which is `Seats + Standees`.
Each capacity check moves to `Capacity`: `internal/sim/order_options.go:149`, `internal/sim/order_options.go:154`, and `internal/session/stream_service.go:218`.
If one of these stays on `Seats`, a stream with a party of 13 is refused, and a test must pin this.
The foundation Express entry (`internal/sim/vehicle_class.go:47`) takes the same counts, and it stays physically unsupported.
Group, Legacy, and Compact have no standees.

### 2.2 Load policy

The recommended policy is the smallest one.
The simulator does not track who sits.
A party boards whole when it fits `Capacity`, seats fill first, and the number of standees is `max(0, passengers - seats)`, which each consumer derives from the riders.
The existing party, stop, join, consent, and service limits stay.
A private on-demand party of 13 to 20 therefore stands in part.

| Option | Benefit | Cost |
| --- | --- | --- |
| Seats first, no setting, recommended | No format member. No new admission path. | A project cannot ask for a seated-only on-demand service. |
| Project setting for standees on on-demand service | A seated service and a crowded service can differ. | A project member, an editor field, a stored consequence for refusals, and a new consent question. |
| Full capacity of 24, with 12 standee places | Matches the vehicle. | Stored riders go from 20 to 24, and the order bound goes from 17,000 to 19,400. Each format limit and `family_limit_tables.json` change. Express has no byte proof to show that this fits. |

### 2.3 Body envelope

The 6 m radius circle and the 20 m pair clearance stay.
A 6 by 2.5 m rectangle has a half-diagonal of 3.25 m, so a smaller radius is possible.
A radius for each class makes `classPairClearance`, `LaneMinimumLength`, the cell size guard, the junction clearance, the bank audit scan, and the envelope front into a table of class pairs.
This has no gain for a Group or Express pod until the mixed cases have a proof, so it is decision 8 and it is out of this slice.

### 2.4 Dwell

Dwell for Express depends on the number of passengers at the door.

| Phase | At `b2262f6` | Proposed for Express |
| --- | --- | --- |
| Boarding | 3 s | 3 s plus 1 s for each passenger after the first of a party or join |
| Unloading | 2 s | 2 s plus 1 s for each alighting passenger after the first |

The largest values are 22 s and 21 s for 20 passengers.
A party of one has the dwell of today.
The flow comes from [TCRP Report 100](../studies/express-standees.md#door-flow), with about 2 s for each stream and two streams.
A join into a boarding pod adds its passengers to `phaseTicks`, and it does not restart the phase.
The phase never exceeds the largest value.
Status 2026-11-05, by the maintainer: the sentence on the join stands over the table.
A party that joins a running phase adds 1 s for each of its passengers, and a party that starts a phase adds 1 s for each passenger after the first.
Seat-only classes keep `boardingTicks` and `unloadingTicks` for each party size.

These sites use the constants: `internal/sim/dispatch.go:660`, `internal/sim/onboard_pickups.go:46`, `internal/sim/operational.go:221`, `internal/sim/simulation.go:926`, `internal/sim/pickup_estimate.go:257`, `internal/sim/pickup_estimate.go:268`, `internal/sim/pickup_estimate.go:298`, `internal/sim/state_physical.go:417`, `internal/sim/state_physical.go:419`, and the phase rules at `internal/sim/state_contract.go:95` to `internal/sim/state_contract.go:161`.
The restore check takes the largest value of the class as its bound, and the order and the text of its refusals stay.
The check does not derive a bound from the riders, because that adds a second refusal.

## 3. Part 1: the motion envelope

### 3.1 Values

| Value | Seat-only classes | Express | Source of the Express value |
| --- | ---: | ---: | --- |
| Acceleration | 2.0 m/s² | 1.3 m/s² | LU 1992 Central, and S Stock and 2009 agree |
| Planned braking | 2.0 m/s² | 0.8 m/s² | DLR B2007 full ATO service brake |
| Maximum service braking | 2.0 m/s² | 1.15 m/s² | LU 1992 Central, S Stock |
| Emergency braking | Not modeled | 1.4 m/s², on record only | LU 1992 Central, S Stock |

I found no credible 1996 Jubilee figure, so the working values stay.
The class has one envelope for each load.
A rule for each load needs a speed bound that changes when a party boards, and the estimates and the restore check then depend on the riders, so I do not recommend it.

At 14 m/s, these values give:

| Rate | Time | Distance |
| --- | ---: | ---: |
| Accelerate at 2.0 m/s² | 7.0 s | 49 m |
| Accelerate at 1.3 m/s² | 10.8 s | 75 m |
| Brake at 1.15 m/s² | 12.2 s | 85 m |
| Brake at 0.8 m/s² | 17.5 s | 122.5 m |

### 3.2 Which rate applies where

| Site | Rate | Why |
| --- | --- | --- |
| Ceiling in `ordinaryMoveStep` (`internal/sim/move_step.go:36`) | Acceleration | The value that the class can apply while passengers stand. |
| Safe root in `moveStep` for the reservation limit (`internal/sim/move_step.go:18`) | Maximum service | A refused grant is the unplanned stop. |
| New plan cap in `moveStep`, toward the route end | Planned, floored at `speed - maximum × dt` | The planned stop at a berth. The floor keeps a diverted pod within the maximum. |
| `speedBeforeLane` (`internal/sim/traffic.go:873`) | Planned | A lower limit ahead is known. |
| `admissionRequest` horizon (`internal/sim/claim_surrender.go:108`) | Maximum service | Reservations do not grow past what the kernel needs. |
| `faultCap` (`internal/sim/faults.go:190`) | Maximum service | Decision 5. |
| `routeSecondsWith` (`internal/sim/pickup_estimate.go:340`) | Acceleration, then planned | An estimate must describe the class. It takes the envelope as an argument. |

Three options exist for braking, and the corridor and the `rail-hub` runs measured each one.

| Option | Saturated headway, straight lane, against 6.685 s | Served, seeds 1 to 3 | Effect |
| --- | ---: | --- | --- |
| One rate of 1.15 m/s² | 8.902 s, +33 percent | 96, 98, 99 | One rate. The value of 0.8 m/s² has no use. |
| One rate of 0.8 m/s² | 10.824 s, +62 percent | 98, 93, 95 | Reservations of 122.5 m at 14 m/s. |
| Plan 0.8 m/s², maximum 1.15 m/s², recommended | 8.902 s, +33 percent | 94, 95, 100 | Uses both working values. It adds one branch to the kernel, which seat-only classes skip. |

The acceleration alone costs +15 percent on this headway, at 7.693 s.
The headway table of the [record](../studies/express-standees.md#saturated-headway) uses all-class lanes, so its absolute values differ from the pins of `TestMergeCorridorHeadway`, which stay for Legacy pods.
Served orders do not separate these options, because the demand of those runs is above the supply of the fleet.
Only the headway does.

### 3.3 Why separation needs no new rule

The safety argument is about ownership and not about relative braking.
A pod moves only inside its grants, and a pod owns a cell alone, so a slower pod behind a faster pod and a faster pod behind a slower pod give the same separation.
The bound for each pod is `distance + speed²/(2 × maximum) ≤ reservation end`.
This is the invariant that the scratch run measured, with the largest excess at 2e-12 m, and that a gate must pin.

These uses of the constant do not change, because a large class cannot reach them:

| Site | Why it stays |
| --- | --- |
| `coast.go` | `internal/sim/coast.go:131` excludes a large class. |
| `platoon.go`, `stoppingDistance` for links | `internal/sim/platoon.go:550`, `internal/sim/platoon.go:579`, and `internal/sim/platoon.go:683` exclude a large class. |
| `retention.go`, `stopDistance`, `reducedClearanceSpeed` | A qualifying chain has no large class. A large pair keeps 20 m at `internal/sim/retention.go:291`. |
| `depot_network.go`, `depot_lateral.go` | `internal/sim/depot_network.go:224` refuses a large class on a slot. |
| Maneuver groups and reversal | `internal/sim/maneuver_groups.go:213` requires the compact class set. |

`stoppingDistance` needs a class argument at its one fault use.
The lemma of the tick rate proposal, section 1.1, holds for each pod with `a` as the maximum service rate.
The plan cap only lowers the command, so the stop point at the maximum stays inside the reservation.
A stop point that moves back matters only to a platoon link, and a large pod has no link.

## 4. Part 2: the curve tail

### 4.1 Candidate

Let `P` be the movement polyline of a lane `L` of length `ℓ`, and let `G` be the supremum of `t - s` over each pair with `|P(t) - P(s)| < 20`.
For a straight lane, the tail stays exactly 20 m.
For a curved lane, the tail is `min(oldBound, max(20, grid(scan + h)))`, with these terms:

| Term | Meaning |
| --- | --- |
| `oldBound` | `20 / (c × (1 - conflictSlack))`, the formula of today, which is itself an upper bound of `G`. |
| `scan` | The largest `t - s` with a distance under `20 + h/2 + f`, for `s` in steps of `h` over the lane, with the lane end included, and for the last such `t`. |
| `grid` | Rounds up to a multiple of `h`. |
| `h` | The larger of 0.25 m and `ℓ / 1024`, so a lane has at most 1,025 samples. A lane of up to 256 m uses 0.25 m. |
| `f` | 1 mm, the smallest step of the walk over `t`. |

The walk starts at the lane end and moves `t` back by `max(d - limit, f)`, where `d` is the distance and `limit` is `20 + h/2 + f`.
The distance along a polyline changes by at most the path distance.
So a step of `d - limit` cannot pass a point under `limit`, and a step of `f` can pass only a point with a distance of at least `limit - f`.
Now take a pair `(s*, t*)` with a distance under 20 m, and a sample `s` within `h/2` of `s*`.
The distance from `s` to `t*` is under `limit - f`, so the walk does not pass `t*`.
Then `G ≤ scan + h/2`, so the tail is strictly above `G`.
A tail must be above `G`, because a tail equal to `G` failed on the narrow arc of `TestLargeGeometryPathProjection` with a gap of 10 m.
The scan does not use convexity or a projection axis.
The argument holds for each positive `h`, so the step can grow with the lane.

The walk counts its distance evaluations.
A lane that passes a fixed budget of evaluations takes `oldBound` for its tail, which is a valid bound.
The budget applies to each lane, so the tail of a lane does not depend on another lane.
Patch 2 fixes the budget from a benchmark, so that the worst-case project prepares in less than 5 s on the native build.
The review gave the reason: the project limits permit about 7.68 million samples at a fixed step of 0.25 m, and one lane can have 918,213 of them.

The refusal of a folded path stays with its text, so the same set of lanes is accepted.
A lane with a chord under 20 m has `G = ℓ`.
The scan is at most `ℓ`, so `grid(scan + h)` is under `ℓ + 2h` on each lane inside the budget, and the old tail had no such limit.

A closed form is also possible: 20 / cos(range / 2), with `range` as the range of the tangent angle.
It is scale free and needs no scan, and it gives at most 71 m on `rail-hub`.
It equals the old value on a symmetric arc, so it gives 263 m on a 204 m hairpin where the scan gives 94 m.
I recommend the scan, and decision 3 records the choice.

### 4.2 Claim

Each lane has a set of locations: the lane, its incident lanes, and the berths at its `From` node.
Take pods `x` and `y`, with at least one of a large class, and suppose that some pair from their two sets is on one plane.
Then the centers are at least 20 m apart at each tick, less the tolerance of 1 µm that the audit (`internal/sim/bank_geometry.go:174`) and the observer (`internal/sim/safety.go:206`) both permit.
The rule is on centers, as in the group physics design, and two envelopes of radius 6 m then have 8 m between them.
The proof has three cases, by the lanes of the two pods.
The tail carries only the first case.
The old bound also addressed only the first case, so its proof did not cover the part of a tail on a later lane either.

### 4.3 Case A: the same lane

Let `x` be at `s` and `y` be at `t > s` on lane `L`.

1. `x` owns the cell `C` that holds `s`, so `s ≤ end(C)`.
2. A cell has one owner, and pods move only forward, so `y` passed `C` and released it before `x` got it.
3. `y` keeps `C` until its route distance is `end(C) + tail` (`internal/sim/traffic.go:956`).
4. While `y` is on `L`, route distance and lane distance differ by a constant, so `t - s ≥ tail > G`.
5. Therefore the distance is at least 20 m.

On a straight lane the tail equals `G`, and the distance equals the path gap, so the result is the same.

Two ends need a further step.
A pod at rest at the berth at the `To` node has released its cells, but each cell with `end > ℓ - tail` holds that node (`internal/sim/traffic.go:169`), so `x` stays in a cell that ends at or before `ℓ - tail`.
A pod that leaves a berth at the `From` node holds that node until `start + fromTail` (`internal/sim/traffic.go:954`), and each first cell holds it (`internal/sim/traffic.go:166`), so the same bound applies from the other end.

### 4.4 Case B: two lanes that share a node

The tail has no role here, and it does not need to.
`buildJunctionConflicts` (`internal/sim/junction_conflicts.go:29`) compares each pair of lanes at each node, with a clearance of 20 m when one mask admits a large class, plus a pad of 0.5 m.
It finds an interval on each lane, and each point outside it is more than 20 m from each point of the other lane.
Each cell that overlaps the interval holds the junction resource of the node, and a pod keeps it until the cell end (`internal/sim/traffic.go:950`).
If both pods are inside their intervals, both need one resource, which has one owner.
If one pod is outside its interval, the distance is above 20 m.

A sharp join, an S-bend across two lanes, and a tail that continues onto the next lane are all in this case.
On the presets, route pairs at or past the tail have chords down to 7.29 m when the front pod is on the next lane, so a path gap alone does not separate them.
The independent check found 0 close samples outside a held junction section, in about 2.7 million close samples.

### 4.5 Case C: two lanes that share no node

`validateBankGeometry` (`internal/sim/bank_geometry.go:128`) refuses a network when a large-admitting path comes within 20 m of a nonincident path, unless each pair of their locations is on two planes.
A location set holds the lane, its incident lanes, and the berths at its `From` node, so a pod near a plane join counts on both planes.
Both pods are inside their polylines, so the distance is at least 20 m.

The audit first gave each lane only its own plane.
That was a fault in the code of today, and section 4.10 gives its numbers.
A separate patch lands the location sets before patch 2, and it has no dependency on the tail.
It reads whole lanes, so it refuses a pass that is far from the join on a lane incident to the shared plane.
A berth is a zero-length path in this audit, so it is in this case too.
This case holds for a leader 2 or more lanes ahead on the route, and for a pod on an unrelated lane, at each path gap.
The independent check found 0 such samples under 20 m, and 0 route pairs under 20 m past the next lane.

### 4.6 Adversarial search

| Attack | Result |
| --- | --- |
| Non-convex lane | A lane is a straight line or one quadratic curve, so it has no inflection. The scan does not need this. |
| S-bend across lanes | Case B. Random networks with opposed bends had 0 violations. |
| Hairpin with a chord above 20 m | The legs come within 20 m near the apex. The scan gives 94 m on a 204 m lane, and a tail of 20 m on such lanes gave 18 violating networks of 49, down to 19.74 m. With the scan there were 0 of 35 and 0 of 22. |
| Chord under 20 m | Found a real fault in the candidate without the margin. With the margin, the lane holds one pod, and Case B or Case C covers the next lanes. |
| Tail onto a later lane at a sharp join | Case B. Sharp straight apexes had 0 violations of 31 networks at a tail of exactly 20 m. |
| Projection axis | The candidate uses no projection. `oldBound` stays only as an upper limit. |
| Adjacent lanes | Case B for a shared node, and Case C for no shared node. |
| Reverse motion | A large class cannot enter a maneuver group (`internal/sim/maneuver_groups.go:213`), and a pod moves only forward on directed lanes. |
| Body front in the plane window | `internal/sim/envelope_safety.go:40` scales the front as `6 × tail / 20`. That is valid for a projection bound and not for a scan bound. Section 5 keeps the front on `oldBound`, so the front does not change. `max(6, tail)` was the first candidate, and a check in the simulation rejected it: its window reached past the body, and the observer reported gaps down to 0.15 m and 35 of 61 runs on a control network. |
| Zero-width junction interval at a cell boundary | The pad of 0.5 m makes the interval of a real close point wider than one sample. |
| Restore | A restore reconstructs the footprint with the same tails, and the new tail is at most the old tail, so a footprint that passed before still passes. The restore tests of the `-short` suite pass, except one that pins a long tail. |
| Floating point | See section 5, on explicit conversions and the rounding grid. |

### 4.7 What the proof does not cover

These are gaps, and I do not hide them behind the case split.

1. Case A step 2 assumes that the front pod passed the cell.
   The argument covers motion.
   For a restore, a physical placement, and a route change, it relies on the existing footprint and release code, which I did not prove again.
2. Case B relies on the existing sampled junction scanner, with its window and its hysteresis.
   I checked its result at a 0.5 m sample on 5 presets, and not on arbitrary networks.
3. The motion oracle has limited power.
   A tail of 0.7 × scan produced no violation in 35 hairpin networks, so only a static scan can gate the bound.
4. No random network and no run here uses separation groups.
   Directed networks with plane joins ran in the check of section 4.10, and the tests of the audit patch pin them.
   A shorter tail narrows the rear window of the observer and the front does not change, so the tail cannot add an observer report.
   The front is a maximum over the route, so one long-tail curve can make the window reach two lanes ahead, and the audit does not cover that case.
   One scratch probe reported 19.996 m there, with the body far from that lane.
   It looks like a false alarm, this is not proved, and the maintainer chose to record it and not change the observer.
5. `scale100` did not finish the pair check inside the 5-minute limit.
   It has no curved lanes.
6. Cross-architecture equality of the scan is not tested, and the maintainer decided that it needs no work now.
   Section 5 gives the limit.
7. The runs use the Group class as a proxy, and no run has a real Express fleet with `express-v1`.
8. The scratch scan had no `grid` and no `f`, and it did not sample the lane end.
   So a final tail can be up to 0.25 m longer than the measured values, and the numbers in this document do not come from the exact rule of section 4.1.

### 4.8 `rail-hub` and the deadlock

The new bound removes the cause on `rail-hub`.
The tails of the fan lanes fall from 299 to 1,933 m to at most 22.12 m, and no curved lane overruns.
A cap of 20 m and the scan give the same result there, so `rail-hub` does not test the difference between them.
The remaining cost is the larger clearance, with 1,967 to 2,565 stopped pod-seconds against 1,209 to 1,337 for the plain preset.

It does not move the deadlock in these runs: none stopped, and the 2-hour run with one trip each 2 s served 426 against 429.
This is not a proof of progress.
The ownership model does not exclude a ring lock, and `docs/model.md` says that the tests establish progress only for feasible supplied scenarios.
The London presets show the other limit: 26 and 50 lanes of at least 40 m stay over length, as U-turn bends with a chord under 20 m, and such a lane holds one pod.
They overrun by less than 0.5 m, so they cannot hold an exit past the next station.

I recommend no new deadlock rule.
The warning of `project.TailWarnings` stays for the lanes that remain, and decision 4 asks about its text.
A new gate pins progress on `rail-hub` with all classes.

The standee envelope changes the braking inputs of this result and not of the proof, because the proof has no braking term.
A pod at 14 m/s holds 85 m ahead at 1.15 m/s², where it holds 49 m at 2.0 m/s².
On `rail-hub` the served counts stayed inside the seed noise, with 0 violations and a long-run count of 201 against 213.
A network of short lanes can behave otherwise, and I did not test one.

### 4.9 Static certificate

Motion tests cannot gate the tail, because a tail of 0.7 × scan produced no violation in 35 hairpin networks.
The gate of section 7.1 is a static certificate in a test, and it uses none of the production bound.

A lane has at most 64 segments, so it has at most 2,080 pairs of segments, at each lane length.
For each pair, the test finds the smallest squared distance between a point at `s` on the first segment and a point at `t` on the second, with `t - s ≥ tail`.
That domain is a rectangle cut by one line, and the distance is a convex quadratic.
So the minimum is the free minimum of the two segments, if that point is in the domain, or else a minimum on the cut line, which has a closed form.
The test requires a result of at least 400 m².
The production code does not run this certificate.

### 4.10 External review

One blind Codex review of section 4 ran on 2026-11-05, as decision 2 asked, and a check in the simulation followed it.
The [evidence record](../studies/express-standees.md) has the numbers.

| Finding | Rating | Result |
| --- | --- | --- |
| The audit does not cover the planes that a pod occupies near a join. | BLOCKER, in the code of today | Real, and independent of the tail. A real simulation reports `SeparationError` down to 14.1 m between centers, at the old tail and at 20 m. No preset reaches it. Section 4.5. |
| A float rounding difference can change the tail at a grid boundary. | MAJOR by the review, MINOR here | The code of today has larger steps on the same inputs. Section 5. |
| Static qualification and worst-case cost are not established. | MAJOR | Accepted. Sections 4.1 and 4.9. |
| The audit and the observer permit a shortfall of 1 µm. | MINOR | Stated in section 4.2. |
| "Between bodies" differs from the requirement. | MINOR | The requirement is on centers, and this document states that. |

The review found the same-lane bound sound in exact arithmetic, and it found no counterexample on a fixed plane at a shared node.

## 5. Rules changed and unchanged

| Rule | Change |
| --- | --- |
| Group physics design: "never reduced to fit that length", and the tail formula | Replaced by section 4.1. Decision 1. |
| Express contract: "Do not shorten those bounds to 20 m" | Replaced by section 4.1. Tails on curves stay above 20 m. Decision 1. |
| Express contract: 20 seats, new party 1 to 20, 10 m body | 12 seats, 8 standee places, party 1 to 20, 6 m body. |
| Express contract: "Motion retains acceleration and braking at 2 m/s²" | The Express values of section 3, and each seat-only class stays. |
| Suspension contract, section 5.2 step 4 and section 6.1 | `faultCap` and "controlled braking at `acceleration`" use the maximum service rate of the class. Decision 5. |
| Envelope front | `6 × oldBound / 20`, the value of today, in place of `6 × tail / 20`. Each lane stores the front beside its tail. |
| Tail arithmetic | Comparisons on squared distances with explicit `float64` conversions, as at `internal/sim/retention.go:26`. A rounding difference can move a tail near a grid line by one step. The determinism claim stays on one CPU type (`docs/model.md:415`), as for `laneBlockCount` and the test of a 40 m lane, which have larger steps on the same inputs. A save moved between architectures is not covered. |
| Tail cost | Keep one tail index for each prepared network and each editor check, because `indexGeometryTails` has 8 call sites and it computes each lane on each call. The step and the budget of section 4.1 bound the work on each lane. |

These stay:

- The 20 m clearance, the 6 m envelope, the 40 m lane minimum, and the 20 m cell minimum.
- The refusal of a folded path, a zero chord, and a nonfinite bound, with its text.
- The audit of nonincident paths and the junction clearance.
- The small-class 12 m clearance, the qualifying chains, the bays, and the depots.
- The exclusion of large classes from virtual links, coast, and maneuver groups.
- The Express fleet of 600 pods, the 17,000 orders, and the 20 stored riders.
- Each number of a seat-only class, with the same float operations in the same order.

### 5.1 Every place that assumes one envelope

| Area | Sites | Effect |
| --- | --- | --- |
| Motion kernel | `internal/sim/move_step.go:15`, `:34`, `:43`, `:59`, `internal/sim/traffic.go:873` | Takes the envelope of the vehicle. |
| Reservation | `internal/sim/claim_surrender.go:108` | Class rate. |
| Faults | `internal/sim/faults.go:190` | Class rate. |
| Estimates | `internal/sim/pickup_estimate.go:306` to `:340`, `internal/sim/diversion.go:159` | Class rates and class dwell. Dispatch results change only with an Express pod. |
| Separation and clearance | `internal/sim/profile_geometry.go:20`, `internal/sim/safety.go:180`, `internal/sim/prepared.go:258`, `internal/sim/junction_conflicts.go:37`, `internal/sim/bank_geometry.go:128`, `internal/sim/retention.go:291` | No change, by section 2.3. |
| Tail consumers | `internal/sim/traffic.go:150`, `:722`, `:901`, `:931`, `:954`, `internal/sim/platoon.go:160`, `internal/sim/envelope_safety.go:40`, `:48`, `internal/sim/retention.go:137`, `internal/sim/depot_lateral.go:218` | New values. A tail is zero in the same cases as today, so the two zero tests stay. |
| Profile and class limits | `internal/sim/vehicle_class.go:47`, `internal/sim/order_contract.go:55`, `:56`, `internal/sim/order_options.go:149`, `:154`, `internal/session/stream_service.go:218` | Section 2.1. |
| Editor | `web/editor.js:302`, `web/editor.js:304`, `internal/editormodel/large_geometry.go:145` | Notice text only. The lane length and the clearance checks stay. The worker pays the scan cost on each check. |
| Client | `internal/view/order_choices.go:16`, `internal/remote/motion.go:102` | The party limit stays 20. The interpolation bound uses speed and not acceleration, so it stays. |
| Compare and energy | `cmd/compare/energy.go`, `docs/design/energy-model-design.md` | No schema change. Energy reads the motion samples, and the author gives the mass with the payload. An Express energy profile needs a new authored mass. |
| Carpark tools | `web/offline.js:111`, `cmd/parkride` | Parties stay at eight. No change. |
| Documents | `docs/model.md:39` to `:58`, `docs/model.md:139`, `docs/model.md:183`, `docs/design/group-physics-design.md:36` to `:49`, `docs/user-guide.md`, `docs/reference.md` | New text. |

## 6. Format and byte effects

| Item | Effect |
| --- | --- |
| Project format | No new member. `orderContract: "express-v1"` keeps its name, and its meaning changes. There is no migration. |
| `stateVersion` 10 | No change. `internal/session/state_file.go:23` asks for a new version only when the members of the file change, and they do not. |
| `StreamVersion` 8 | No change. No message changes shape, and each client derives the standee count from the riders. |
| Goldens | `state_v10.json`, `state_v10_members.txt`, and the `stream_*` files have no profile or envelope value, so they stay. A scratch run with the new tail kept `internal/session` passing. |
| `family_limit_tables.json` | No change, because the counts stay. |
| Saved state from before | A version 10 file stays valid. Its phases are under the new bound, and each restore resets speed to zero. An older build refuses a newer file whose phase is above 180 ticks, and the file moves aside, as the rules allow until v1. |
| Tail | Not saved, so part 2 needs no version change at all. |
| Plain byte proofs | `TestPlainStateFileWorstCaseSize` and `TestPlainStreamMaximumEncoding` do not change, because no count or byte limit in `internal/project/config.go` changes. |
| Express byte sizes | Express has no byte proof, and this does not change that. |
| Refusals | Order and text stay. Only the value of the Express phase bound changes. |

A version bump would refuse earlier saves with no need, so I do not recommend it.
Decision 9 records this.

## 7. Test and gate plan

### 7.1 Part 2

| Gate | Check |
| --- | --- |
| Static certificate | The test of section 4.9 runs on each curved lane of each preset, a hairpin table, and a table at the coordinate limit, with its own polyline code. Each pair at or past the tail has at least 20 m. |
| Tail limits | The tail is at most the old bound and exactly 20 m on a straight lane. It is under `ℓ + 2h` inside the budget, and a lane past the budget takes the old bound. |
| Tail cost | A benchmark of the worst-case project, native and wasm, and a lane at the coordinate limit. |
| Pair coverage | For each preset that prepares with all classes, each close sample on two lanes with a shared node is in a held junction section, and two lanes with no shared node have no close sample. |
| Hairpin motion | A deterministic stopped follower on a hairpin at the last legal cell, with a chord above 20 m. The independent center check runs on each tick. |
| Random curved networks | The scratch generator of the record, as a long test with a fixed seed list. |
| Progress | `rail-hub` with all classes serves at least 90 percent of the plain preset at the same seed, with no pod still at minute 30 of a stopped cycle. Each tick runs the separation oracle. |
| Parity | Each digest and golden of a project with no large class stays. A curved all-class project has new tails only. |
| Tests that change | `TestPlatoonReleaseBoundedEnd/curved`, `TestPlatoonRestoreOldRuleLink`, `TestLaneTailOverruns`, `TestYieldRelocationClaimsKeepsStoppingNode`, `TestTailWarningsCap`, `TestEngineTailWarnings`, `TestDraftTailWarnings`, `TestProjectCheckWarningWriteError`, and `TestProjectCheckTailWarnings`. Each pinned a long tail or a `rail-hub` overrun, so each needs a hairpin fixture or a new number. I read the setup check of `TestYieldRelocationClaimsKeepsStoppingNode` only, so that fixture needs a review. |

| Mutation of part 2 | Expected kill |
| --- | --- |
| `max(20, scan)`, with no `h` | `TestLargeGeometryPathProjection/narrow_monotone_arc`. Observed: gap 10 m. |
| Tail of 20 m on each curve | Static certificate, and hairpin motion. Observed in random networks: 18 of 49 fail. |
| Tail of 0.7 × scan | Static certificate. Random motion does not kill it, as observed. |
| Remove the `To` node from a cell inside the tail | Stationary endpoint test, as today. |
| Remove the junction resource of a lane pair | Sharp apex random network, and pair coverage. |
| Clearance of 12 m in `buildJunctionConflicts` | Pair coverage. |
| Skip the nonincident audit for large lanes | `TestLargeGeometryNonincidentLaneAudit`. |
| Envelope front from the new tail | A new fixture with a separation group at a hairpin. |
| Budget test removed | The test of the lane at the coordinate limit. |

### 7.2 Part 1

| Gate | Check |
| --- | --- |
| Motion ledger | Each tick of each Express journey has a speed rise of at most 1.3 × dt, a speed drop of at most 1.15 × dt, and `distance + speed²/(2 × 1.15) ≤ reservation end`. |
| Planned stop | A clear approach to a berth brakes at no more than 0.8 × dt, apart from the endpoint snap. |
| Mixed following | Express behind Group, Group behind Express, Express behind Legacy, and each order at a merge, on straight and curved lanes, with the independent center check. |
| Capacity | Party sizes 1, 8, 12, 13, 20, and 21. Twenty singletons. A stream with 13 aboard. Standee count derived in each consumer. |
| Dwell | 1 and 20 passengers, a join during boarding, an occupied pickup, a multi-stop unload, an emergency unload, and the restore bound at the largest value and one above it. |
| Estimates | An estimate for an Express pod uses its class, and a seat-only estimate has the same bits. |
| Seat-only parity | `TestPresetOutputIsPinned`, `command_digests.txt`, the corridor pins, and `move_step_test.go` for the old classes. |
| Fault | A fault on Express at speed brakes at most 1.15 × dt and stops by `faultCap`. |
| Restore and stream | Express restore at each activity, full frames and deltas, and the browser game page. |

| Mutation of part 1 | Expected kill |
| --- | --- |
| A check stays on `Seats` at `internal/session/stream_service.go:218` | Stream with 13 aboard. |
| Horizon at 2.0 m/s² for Express | Motion ledger, or a fault test, by an unowned stop point. |
| Safe root at 2.0 m/s² for Express | Motion ledger. |
| `speedBeforeLane` at 1.15 m/s² for Express | Planned stop. |
| Remove the floor of the plan cap | Motion ledger on a diversion. |
| Envelope applied to Group | `internal/sim/large_motion_test.go:74` no longer reaches 2.0 in a pin, and a new test requires it to reach 2.0. |
| Phase bound at 3 s for Express | Restore of a pod in a long boarding. |
| Join restarts the phase | Dwell of a join. |

Each patch also runs `mise run lint`, `go fix -diff` on the changed packages, `go test ./...`, `mise run test:web`, and the race task of its package.

## 8. Patch list

| # | Subject | Content | Estimate |
| ---: | --- | --- | ---: |
| 1 | `contracts: Amend the tail and Express profile rules` | The changes of section 5, in the documents of decision 1, when the maintainer approves them. | 0.25 day |
| 2 | `sim: Bound the curve tail by the lane polyline` | The scan, the step, the budget, the grid, the limits, the single index, the stored front, and the certificate, pair, and hairpin gates. The changed tests. | 1 day |
| 3 | `studies: Record the curve tail result on rail-hub` | The progress gate, and the qualification text. The model text on the warning. | 0.25 day |
| 4 | `sim: Give each vehicle a motion envelope` | One envelope for each class, with the same values for each class. No behavior change, and each digest stays. | 0.75 day |
| 5 | `sim: Split Express capacity into seats and standees` | Section 2.1, with the 6 m body. | 0.5 day |
| 6 | `sim: Give Express the standee envelope` | Section 3, with the plan cap, the estimates, and the fault cap. | 0.75 day |
| 7 | `sim: Scale Express dwell with the load` | Section 2.4, with the restore bound. | 0.75 day |
| 8 | `editormodel: Describe the Express profile` | Notice text, and the web tests. | 0.25 day |
| 9 | `docs: Describe Express standees` | The docs pass, and the qualification record with a real Express fleet. | 0.75 day |

The total is about 5 agent days.
These estimates are mine, and I did no such work to test them.
Patches 2 and 3 can land by themselves, and they help each project with a curve and a large class.

## 9. Risks

| Risk | Response |
| --- | --- |
| The proof has an error in a case that nothing exercised. | The review of section 4.10 found one such fault, outside the tail. The certificate and the independent oracle stay as tests. |
| Random motion cannot detect a short tail. | The certificate is the gate, and the mutation table shows it. |
| The scan cost shows in the editor worker or at preparation. | One index for each network. A curved lane took 0.2 to 16 ms in the scan at 200 m to 10 km with a fixed step, measured under load. The step of section 4.1 limits the samples, and the budget limits the walk. Patch 2 includes a benchmark, and the worst-case project is not yet measured. |
| A longer reservation at 1.15 m/s² fills short lanes and locks a network. | Measured on `rail-hub` only. The progress gate runs with the envelope too. |
| Express headway rises by a third, so Express gives less capacity than the seats suggest. | This is the purpose of the values, and the study reports it. Decision 6 can raise the acceleration. |
| Dwell values come from rail door flow, not from a minibus. | Morgantown gives one cross-check at 20 s. Decision 7 can change the rate, and only one constant changes. |
| The meaning of `express-v1` changes under its name. | No backward compatibility until v1, and no saved number changes meaning. |
| Mixed fleets with two envelopes change dispatch estimates. | Only with an Express pod in the fleet, and seat-only parity gates cover the rest. |

## 10. The tick rate proposal

The [tick rate proposal](tick-rate-contract-proposal.md) is open, and the two can land in either order.

| Item | Interaction |
| --- | --- |
| Proof | The `dt` terms cancel for each class. Its section 1.1 reads with `a` as the maximum service rate of the pod. The plan cap has the same root form, and its floor is the same `speed - a × dt`. |
| Tail | The scan has no time term. |
| Dwell | The proposed values are whole seconds, so `TicksPerSecond` multiplies them with no rounding at 60, 30, and 20 Hz. A rate below 1 s would need a whole number of ticks at each rate. |
| Versions | That proposal takes `stateVersion` 11 and `StreamVersion` 9. This one takes neither, so they do not compete. |
| Corridor pins | Its patch 6 changes the Legacy pins. A new Express pin from this proposal must be written for the rate at its landing. |
| Lookahead | The horizon adds a lookahead of two ticks, as today, and decision 3 of that proposal applies to it. |
| Order | If that proposal is approved soon, land the rate before patch 6, so that the Express pins and the study run once. |

The incident soak of stage 7 uses presets with no Express pod, so this proposal does not change it.

## 11. What I did not verify

- A real Express fleet with `express-v1`.
  Each envelope run used Group as a proxy, in a scratch overlay.
- Load-based dwell.
  It was not built or run.
  `cmd/compare` orders have one passenger, so these runs do not exercise it.
- Class-aware estimates.
  The scratch overlay left `routeSeconds` at the old rates.
- The plan cap on a diversion, as a directed test.
- Separation groups and plane exemptions in motion, other than the directed networks of section 4.10.
- Physical restore in mid-journey on hairpin lanes.
- Any network with short curved lanes and the standee envelope, other than the random generator.
- Any effect on `london-central` or `london-full`, which do not prepare with large classes because of their compact bays.
- Worst-case scan cost, the budget value, the scaled step in motion, and any browser or WASM timing.
- Equality of the scan between `amd64` and `arm64`.
- Service measurements on a quiet host.
  Load was 20 to 48 during these runs, which affects time and not simulated results.
- The research sources that the earlier note could not open, and any 1996 stock figure in a primary source.
- A second external review of the revised section 4.
  The maintainer did not ask for one.

## 12. Decisions for the maintainer

1. Tail rule: Amend the group physics design and the Express contract, so that the tail is the polyline bound of section 4.1 in place of the projection bound.
   These are the approved sentences "never reduced to fit that length" and "Do not shorten those bounds to 20 m".
   Recommendation: approve.
   The new tail is not a cap, and each curve keeps a proven bound above 20 m.
2. External review: Run one Codex review of section 4 before patch 2, with no sight of the earlier result.
   Recommendation: yes, because the proof rests on three existing mechanisms and it is not machine checked.
3. Tail form: Use the scan, the closed form 20 / cos(range / 2), or the smaller of them, with a margin and a grid of 0.25 m, and the envelope front of section 5.
   Recommendation: the scan, limited by the old bound.
   The closed form has a shorter proof, and it gives 263 m where the scan gives 94 m.
4. Deadlock: Add no deadlock rule, keep `project.TailWarnings` when a tail exceeds the lane, and change its text to say that the lane holds one pod.
   Recommendation: yes.
   The tail stops being a cause on `rail-hub`, and the progress gate of section 7.1 detects a regression.
5. Emergency 1.4 m/s²: Keep it on record only, or use it for a fault stop.
   Recommendation: record only.
   A fault stops at the maximum service rate, which needs only the reading of `acceleration` as the class rate in the suspension contract.
   Use of 1.4 m/s² adds a third rate to the kernel, and a pod that stops sooner gains no separation.
6. Motion values: Accept 1.3, 0.8, and 1.15 m/s² for each load of the class, with plan and maximum as in section 3.2.
   Recommendation: accept.
   One rate of 1.15 m/s² is the simple fallback, with the same headway.
7. Dwell: Accept 1 s for each passenger after the first, on top of 3 s and 2 s.
   Recommendation: accept, and measure it in patch 7 with a shared-ride run.
8. Body envelope: Keep 6 m radius and 20 m for Express, and open a separate proposal for a radius of each class.
   Recommendation: keep them in this slice.
9. Counts and format: 6 m body, 12 seats, 8 standee places, total 20, seats-first load policy, no setting, and no version change.
   Recommendation: yes.
   A total of 24 needs a new order bound and has no byte proof.
10. Group: Keep Group at 8 seats and 2.0 m/s².
    Recommendation: keep it, because it has no standees.
11. Folded paths: Keep refusing a path that a chord projection folds, although the scan can bound it.
    Recommendation: keep the refusal, because no preset needs it and the refusal text is pinned.
12. Order of landing: Part 2 first, then the tick rate if it is approved, then part 1.
    Recommendation: yes.
    Part 2 helps today with no version change.
13. Progress gate: Accept 90 percent of the plain preset on `rail-hub` as the gate of section 7.1.
    Recommendation: accept it as a gate on regression only, and not as a capacity claim.
