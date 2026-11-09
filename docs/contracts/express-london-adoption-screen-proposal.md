# Express London adoption screen proposal

Status: drafted and approved on November 8, 2026.
The maintainer approved decisions 1 to 6 as recommended, with runner option A.
The maintainer also approved the build of the runner and the timed pilot.
The maintainer approved the full screen after the pilot, and it failed, so the work on D adoption stops (see [Results](#results)).
Status 2026-11-08: the approved mapping said orders of at most 8, from `sim.MaxNewPartySize`, but the LondonFull fleet is Compact, which takes a party of at most 4, so an order of 8 had no pod.
The runner splits to the largest party that the fleet takes, and the maintainer confirmed this correction on November 8, 2026.

## Scope

Decision 1 of the [Express London qualification proposal](express-london-qualification-proposal.md) asks for the adoption comparison of project D as a separate proposal, inside the study brief cap of 6 arms and 3 seeds.
The [adoption gates](../design/experimental-adoption.md#evidence-required) need all six London bands, seeds 1 to 10, two queue limits, and isolated policies.
The study plan estimated that full matrix at 84 to 88 days of serial time before optimization.
A screen of 6 arms and 3 seeds cannot meet these gates, and the adoption document says that a screen cannot replace this coverage.

So this proposal is a screen with two possible results:

- A failure of a screen gate stops the work on D adoption, with no full matrix.
- A pass justifies a separate proposal for the full matrix, with a measured cost.

The screen does not adopt D, change a default, or change ordinary LondonFull.

## Configurations

| Name | Project |
| --- | --- |
| `LF` | `london-full`, the baseline. |
| `D0` | `london-full-express` without its 16 Express pods and Express services. It is a diagnostic control for the geometry. |
| `D` | `london-full-express`. |

The comparison of `D` against `LF` decides each gate.
The comparison against `D0` only explains a result, and it cannot hide harm against `LF`.
Redistribution and pickup reassignment stay on, as in the current defaults.
The screen does not isolate the policies.

## Arms and seeds

| Arm | Configuration | Workload |
| --- | --- | --- |
| 1 | `LF` | Busy band |
| 2 | `D0` | Busy band |
| 3 | `D` | Busy band |
| 4 | `LF` | Quiet band |
| 5 | `D0` | Quiet band |
| 6 | `D` | Quiet band |

Each arm runs seeds 1, 2, and 3, for 18 runs.
The busy band and the quiet band are the bands of `LondonFullDemand` with the most and the fewest observed journeys.
The runner prints both names before the pilot, and they do not change after it.
Status 2026-11-08: the runner picks the bands by observed journeys a minute, so the busy band is `pm-peak`.
Both bands use the background rate of the LondonFull preset, 10 requests a minute, so the band changes only the flows.

Each workload adds the hub stream of the D soak: shared hub parties of 1 to 20 passengers between Waterloo and Bank, one each 15 s on average, three in four from Waterloo.
That gives about 42 hub passengers a minute.
Every arm has the same offered events, seed for seed.

Each run offers events for 2 simulated hours, and then it runs without new events until all accepted requests finish, for at most 2 more hours.
The live queue limit is the default of 200.
The study limit of 10,000 is out of scope.

## Split-event mapping

`LF` and `D0` have no Express pod, so they cannot accept a hub party of more than 4 passengers as one order: the Compact class takes a new party of at most 4.
The qualification proposal deferred this mapping to this proposal.

The recommended mapping:

- Before admission, the runner splits a hub event into orders of at most the largest new party of a pod in the fleet, 4 for Compact.
  Each order keeps the identity of its event.
- The runner never splits an order that a session accepted.
- The wait of an event ends when its last passenger boards.
  Its journey ends when its last passenger arrives.
- An event with a refused or unfinished part is partial or censored.
  The report keeps it and counts it apart.
- The report gives request counts and passenger counts apart.

## Screen gates

A screen gate compares `D` with `LF` for the same seed and workload.

| Gate | Pass rule |
| --- | --- |
| Safety and accounting | No separation, speed, ownership, conservation, lost-order, or duplicate-order failure. The oracle runs at each simulated second. |
| Finite recovery | Each run finishes every accepted request inside the 2-hour clear. |
| Live queue loss | `D` refuses no offered event that `LF` accepts. |
| Background pickup wait | Each matched background request that boards in both runs waits at most max(30 s, 5% of its `LF` wait) longer in `D`. |
| Background journey | Each matched background request that finishes in both runs takes at most max(60 s, 5% of its `LF` journey) longer in `D`. |
| Hub benefit | `D` completes more hub passengers than `LF` in each seed and workload. |

These rules come from the [numerical limits](../design/experimental-adoption.md#numerical-limits) of the adoption gates.
The screen reports the aggregate service, per-direction hub results, censored events, and CPU per simulated hour, with no gate on them.
The sustained backlog gate needs 6-hour arrival runs, so it stays out of the screen.

The earlier [hub fixture screen](../studies/express-hub-fixture.md) failed the background pickup gate in seed 2.
The maintainer waived that failure for that screen only, and this screen gets no waiver.

## Runner

The study runner of the hub fixture is on the unmerged branch `study/express-fixture`, at `fa587966`, so its source does not match `main`.

| Option | For | Against |
| --- | --- | --- |
| A. Add the screen to `cmd/compare` | Reproducible from `main`, with source hashes, matched demand schedules, and existing tests. | The largest patch: hub parties, the split mapping, matched-request gates, and censored events are new in `cmd/compare`. |
| B. Rebase the study runner onto `main` | Less new code. | A tool outside the checked tools, with Python analysis, no tests, and no conservation check. |

The recommendation is option A, as one patch for the hub stream, and one for the split mapping and the matched-request report, each with tests.

## Pilot and budget

First, a timed pilot runs one seed of arms 1, 2, and 3 for 30 simulated minutes.
The pilot result gives the cost of the 18 runs.
If the projection exceeds 6 hours of wall time on this host, the screen stops and comes back with a smaller shape.
The runs use nice, a memory cap, and one study at a time.

## Decisions for the maintainer

1. Approve the screen as a stop or continue gate, and not an adoption pass.
2. Approve the six arms: `LF`, `D0`, and `D`, on the busy and the quiet band, with seeds 1 to 3.
3. Approve the split-event mapping above.
4. Approve the screen gates, with no inherited waiver.
5. Choose the runner: option A (recommended) or option B.
6. Approve the pilot rule and the 6-hour budget.

## Results

The full screen ran on November 8, 2026, at `e41c6610`, with the default flags, 3 workers, and nice 10.
It took 29 minutes of wall time.
The busy band is `pm-peak`, and the quiet band is `morning`.

The screen fails.
Each pair of `D` against `LF` fails at least two gates, so by decision 1 the work on D adoption stops, with no full matrix.

| Band | Seed | Recovery | Wait over the limit | Journey over the limit | Hub passengers, `D` and `LF` |
| --- | ---: | --- | ---: | ---: | --- |
| `pm-peak` | 1 | 1 order not finished | 31 | 22, and 1 not finished | 4,393 and 2,638 |
| `pm-peak` | 2 | pass | 34 | 27 | 4,788 and 2,658 |
| `pm-peak` | 3 | pass | 31 | 24 | 4,898 and 2,531 |
| `morning` | 1 | pass | 24 | 22 | 4,393 and 1,971 |
| `morning` | 2 | pass | 21 | 17 | 4,788 and 2,046 |
| `morning` | 3 | 1 order not finished | 14 | 13, and 1 not finished | 4,898 and 2,086 |

All 6 pairs pass the safety and accounting gate, the live queue gate, and the hub gate.
`LF` refuses 217 to 387 events at the queue limit in each run, and `D` refuses none.

The gates compare each matched request, and most requests are faster in `D`.
In `pm-peak` seed 1, `LF` has a mean background wait of 696 s.
On the 1,122 background requests that finish in both runs, `D` is faster by a mean of 510 s and a median of 473 s, but the 99th percentile is 111 s slower and the slowest is 448 s slower.
The 31 requests over the wait limit start at 21 stations, and 8 of them start at Bank.
The screen has no waiver, so these requests fail the gate.

`D0` does not recover in any run: 65 to 371 orders do not finish in the 2-hour clear, and it finishes fewer hub passengers than `LF`.
The pilot found the cause, a mutual block between Bank and Cannon Street.

### Two stalls in `D`

The 2 orders that do not finish in `D` are stalls, and neither is the block of `D0`.
No safety or contract check reports them.

- `pm-peak` seed 1: a Compact pod with a rider to Bank stops on the access lane of bank `b`.
  In front of it, 4 Express pods queue for berth `b01`.
  The first of them waits for the berth, and it names as the holder of the berth a pod that is behind it in the same queue.
- `morning` seed 3: a Compact pod with a rider to Waterloo waits at the end of the access lane of berth `b01` of Waterloo, with the reason "No parking available".
  An Express pod with the state Ready holds that berth and does not leave it.

Both stalls have one cause in the geometry of project `D`.
For the Express class, berths `b01` and `b02` of Waterloo have no route to Express parking and no route from it.
Each other berth of Waterloo and Bank has both routes.
An Express hub trip can end at Waterloo bank `b`, and then the empty Express pod cannot park.
When a Compact pod needs the berth, the only place that the Express pod can go is a berth of Bank.
An empty move reserves its berth when it starts, so 4 such moves can reach the access lane of Bank bank `b` in an order that is not the order of their reservations, which gives the first stall.
When the berths of Bank are full, the Express pod stays at Waterloo, which gives the second stall.
The maintainer chose to add Express links between Waterloo bank `b` and Express parking, and `1f721a5c` adds them.
The [berth claim deadlock proposal](berth-claim-deadlock-proposal.md) gives the rule that made the first stall permanent.

### Rerun on the fixed geometry

The full screen ran again on November 8, 2026, at `1f721a5c`, in 32 minutes.
`D` finishes each accepted order in all 6 runs, so it passes the recovery gate.
It still fails the background wait gate, with 14 to 34 requests over the limit, and the background journey gate, with 13 to 25, in each pair.
The other gates and the hub results do not change, and `D0` does not change, because it has no Express pods.
So the screen still fails, and the work on D adoption stays stopped.

### Probe of `D` at a higher load

The maintainer asked whether `D` can get the block of `D0` at a higher Compact load.
A probe ran `LF` and `D` on the busy band with seeds 1 to 3, at 15, 20, and 30 background requests a minute, with the oracle at each simulated minute.
All 18 runs finished each accepted order, and no run stalled.
At 20 and 30 requests a minute, `D` refuses 115 to 826 events that `LF` accepts, so the live queue gate also fails there.
The queue limit of 200 caps the waiting orders, so at 30 requests a minute most of the added load becomes refusals.
The probe does not show that `D` cannot get the block.

### Study of the slow requests

The maintainer asked which requests fail the wait and journey gates, and why, with no change to the gates or the simulation.
A scratch probe ran the 6 pairs of `LF` and `D` again at `f5dbaca6` and recorded each background request.
It gives the same 150 requests over the wait limit as the screen, out of 6,494 that both runs accepted and boarded.

- The journey failures are wait failures.
  117 of the 119 requests over the journey limit are also over the wait limit, the median ride time is the same in both runs, and no ride is shared in either run.
- Bank is the main place.
  20.6 percent of the requests from Bank are over the wait limit, against 1.9 percent from the other stations, although the median wait from Bank is 171 s in `D` and 393 s in `LF`.
- The start of the run is the main time.
  6.0 percent of the requests offered in minutes 15 to 30 are over the limit, while the queue of `LF` fills up, against 0.9 to 2.6 percent in the other 15-minute periods.
  The median excess wait of a failed request is 148 s, and the largest is 951 s.

A sample of the berths of Bank every 10 s in `pm-peak` seed 2 of `D` shows why Bank is slow for Compact pods.
Express pods hold berth `01` of bank `a` 39 percent of the time and berth `02` 38 percent, mostly to unload large hub parties.
Compact pods hold those berths 3.5 and 0.4 percent of the time, and the 2 berths of bank `b` are in use less than 1 percent of the time.
Bank choice uses the approach cost first, and bank `a` costs less, so Compact pods wait behind Express pods at bank `a` while bank `b` stays empty.

The study does not show the cause of the failures outside Bank.
A change that could move the result, such as a bank choice that counts the pods at each bank, or Express service only at bank `b` of Bank, is a simulation or geometry change for a separate decision.

### Study of the Bank pickups

The [bank load choice](bank-load-choice-proposal.md#result) spreads the Express pods over both banks of Bank, but it does not change the screen counts.
So the maintainer asked for a study of the pickups at Bank, with no change to the gates or the simulation.
A scratch probe ran `LF` and `D` on `pm-peak` seed 1 at `7bbd2987`.
Each 5 s, it recorded each pending background request and the state of its assigned pod.

- `D` is faster at Bank on average.
  Of the 36 background requests from Bank that both runs boarded, the mean wait is 195 s in `D` and 342 s in `LF`, and the median is 160 s and 413 s.
- Each of the 5 requests over the wait limit had an idle Compact pod at Bank in `LF`, so in `LF` the pod did not move before the boarding.
  In `D`, a pod came to Bank from 0.8 to 6.6 km away, and the wait was 164 to 565 s, against 39 to 78 s in `LF` for 4 of them.
- In `D`, an assigned pod is stopped for only 3 percent of the wait, mostly at the access lanes and berths of Bank.

So the earlier study gave the wrong cause.
Compact pods do not wait behind Express pods at Bank.
The failures come from requests that `LF` serves with an idle pod at Bank, while `D` has almost no idle Compact pod there: Compact pods use the berths of Bank less than 2 percent of the time.
The per-request gates count these requests, although `D` serves Bank faster on average.
The study does not show why `D` keeps no idle Compact pod at Bank.

### Study of the Compact pods at Bank

The maintainer asked why `D` has no idle Compact pod at Bank, with no change to the gates or the simulation.
A scratch probe ran `LF` and `D` on `pm-peak` seed 1 at `530065df` and sampled each Compact pod at a berth of Bank each 5 s of the arrivals.

| Arm | Compact unloads at Bank | Mean idle Compact pods at Bank | Idle periods |
| --- | --- | --- | --- |
| `LF` | 164 | 0.01 | 5 |
| `D` | 4 | 0.00 | 1 |

Neither arm keeps idle pods at Bank.
In `LF`, the runner splits the hub parties to Bank into Compact orders, so Compact pods unload at Bank 164 times in 2 hours.
Such a pod is at Bank when a background request from Bank comes in, so it serves the request with no travel, and it is idle for 10 s or less, if at all.
In `D`, Express pods carry the hub parties, so Compact pods unload at Bank only 4 times, and each request from Bank needs a pod from another place.

So the requests from Bank over the limit come from the hub stream of `LF`, not from a fault of `D`.
`LF` uses the hub parties to bring Compact pods to Bank, and the per-request gates compare each request from Bank against that supply.
`D` takes the hub parties off the Compact fleet by design, so the bank choice and the parking cannot give this supply.
A rule that holds Compact pods at Bank could, but that is a separate simulation decision.

### Over-limit requests by origin

To size a gate amendment for the hub stations, a scratch probe ran the 6 pairs again at `785b3968` and counted the requests over the limits by origin.
Its totals are the same as the screen.

| Pair | Bank: matched, wait over, journey over | Waterloo | Other stations |
| --- | --- | --- | --- |
| `pm-peak` seed 1 | 36, 5, 3 | 22, 0, 0 | 1,065, 22, 12 |
| `pm-peak` seed 2 | 44, 12, 12 | 34, 0, 0 | 1,055, 21, 15 |
| `pm-peak` seed 3 | 38, 4, 5 | 35, 0, 0 | 1,031, 28, 21 |
| `morning` seed 1 | 2, 0, 0 | 10, 0, 0 | 1,037, 23, 22 |
| `morning` seed 2 | 1, 0, 0 | 10, 0, 0 | 1,026, 21, 15 |
| `morning` seed 3 | 5, 0, 0 | 19, 0, 0 | 1,024, 16, 15 |

Bank gives 21 of the 152 requests over the wait limit and 20 of the 120 over the journey limit.
The rate at Bank is high, but most failures come from the other stations, at about 2 percent of their requests, in each pair.
So a rule for the hub stations alone does not change the result of any pair, and the cause of the failures at the other stations is still open.

### Study of the failures at the other stations

The maintainer asked for a study of the failures away from the hub stations, with no change to the gates or the simulation.
A scratch probe ran the 6 pairs at `f0218305` and compared the matched background requests from the stations other than Bank and Waterloo.

| Pair | Matched | `D` faster | Mean wait, `LF` and `D` (s) | p95 wait (s) | Over the limit | Excess p50 and max (s) | Refused, `LF` |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `pm-peak` seed 1 | 1,065 | 974 | 703, 182 | 1,350, 446 | 22 | 86, 364 | 217 |
| `pm-peak` seed 2 | 1,055 | 969 | 738, 168 | 1,510, 416 | 21 | 121, 310 | 223 |
| `pm-peak` seed 3 | 1,031 | 950 | 742, 193 | 1,405, 461 | 28 | 128, 260 | 287 |
| `morning` seed 1 | 1,037 | 900 | 1,005, 423 | 2,077, 1,194 | 23 | 250, 951 | 350 |
| `morning` seed 2 | 1,026 | 873 | 1,012, 437 | 2,092, 1,187 | 21 | 114, 833 | 383 |
| `morning` seed 3 | 1,024 | 866 | 1,002, 429 | 2,139, 1,208 | 16 | 250, 1,151 | 387 |

The guess about the queue was wrong in direction.
`LF` carries more load, not less: in `pm-peak` seed 2, from minute 30 to minute 120, `LF` has no idle Compact pod and 100 to 133 background requests pending, and its queue is at the limit.
`D` has 171 to 186 idle Compact pods, almost all at passenger stations, and 19 to 42 background requests pending.

A trace of the 21 failures of `pm-peak` seed 2 gives the difference.
When dispatch first assigned a pod, the median distance of the pod to the station was 3.7 km in `D` and 1.0 km in `LF`, and the median travel time to the pickup was 325 s and 95 s.
In 6 of the 21, a pod of `LF` was already at the station.
13 of the 21 requests came in minutes 15 to 30, while the queue of `LF` filled and its last idle pods went into service.
The probe does not show why dispatch in `D` sent a pod from so far away while other pods were idle.

So `D` serves 85 to 92 percent of the matched requests faster, but about 2 percent of the requests wait longer than the limit, with a median excess of 86 to 250 s in each pair and up to 1,151 s.
The [numerical limits](../design/experimental-adoption.md#numerical-limits) keep the individual limits to reject exactly such regressions of hundreds of seconds, and the design does not allow a change of a limit after a failed result.
So the screen result stands.

### Study of the dispatch choice in `D`

The maintainer asked why dispatch in `D` sends a pod from far away while other pods are idle, with no change to the gates or the simulation.
A scratch probe ran `pm-peak` seed 2 at `82e030fa` and recorded, at each tick, the dispatch reason of each pending background request and the idle Compact pods when dispatch first assigned a pod.

Two causes add up.

- No idle pod is near.
  When dispatch assigned a pod to one of the 21 failing requests, the nearest free idle Compact pod was 2.1 to 5.7 km away for 16 of them, and 0.4 to 1.9 km away for the other 5.
  For all the matched requests of `D`, the median distance to the nearest free idle Compact pod is 2.8 km.
  The idle pods are on the edge of the network, far from the demand:

  | Distance from Charing Cross | Passenger stations | Background requests from minute 15 | Mean idle Compact pods |
  | --- | --- | --- | --- |
  | Less than 3 km | 46 | 424 | 1.1 |
  | 3 to 6 km | 56 | 223 | 6.8 |
  | 6 to 10 km | 74 | 209 | 45.5 |
  | More than 10 km | 133 | 126 | 133.0 |

- Dispatch first waits for a busy pod.
  18 of the 21 failing requests waited 30 s, and 1 waited 10 s, for a busy pod that should finish first, with the `current` [wait rule](../model.md#wait-rules), and then the idle pod went.
  With the same events, `D` has 21 failures at the other stations with the `current` rule, 13 with the `strict` rule, and 11 with the `none` rule.
  `LF` used the `current` rule in each run, and the probe did not run `LF` with the other rules.

`D` carries less load than `LF`, so the pods become idle where the trips end, mostly far from the center, and redistribution does not bring enough of them back.
`LF` has no idle pods, so a pod that finishes a trip in the center takes the next request there.
The wait rule and redistribution are shared policies of all projects, so a change to either is a separate decision for each configuration, with its own evidence.

### Study of redistribution in `D`

The maintainer asked why redistribution leaves the idle Compact pods of `D` at the edge, with no change to the gates or the simulation.
Redistribution in the screen is guarded positioning.
It moves an idle pod only while its gate is open, and the gate is open only when all of these are true:

- The request rate is less than 1 a minute for each 20 pods: 16.4 a minute for `LF` and 17.1 for `D`.
- The newest request is at most 180 s old.
- The positioning moves are fewer than the boardings.
- Each waiting trip has a pod.
- At most 40 percent of the fleet works.

A scratch probe with a temporary accessor for the gate ran `pm-peak` seed 2 at `e404cc2d` and sampled the gate each 30 s of the arrivals.

| Condition | `LF`, share of samples | `D`, share of samples |
| --- | --- | --- |
| Rate low | 27 percent | 100 percent |
| Newest request at most 180 s old | 100 percent | 100 percent |
| Fewer moves than boardings | 100 percent | 100 percent |
| Each waiting trip has a pod | 2 percent | 4 percent |
| At most 40 percent of the fleet works | 5 percent | 22 percent |
| Gate open | 0 percent | 2 percent |

Guarded positioning made no move in either run.
In `D`, the gate stays closed because a waiting trip has no pod at almost each sample, and because more than 40 percent of the fleet works.
A trip that dispatch holds for a finishing pod has no pod during the hold, so the wait rule also keeps the gate closed.

The code has two more limits that the probe did not measure.
A lone idle pod at a passenger station is never a candidate for a move, and a move has a route cost of at most 180 s.
The idle pods of `D` are mostly more than 10 km from the center, so even with an open gate these limits can keep them there.

Guarded positioning is a design for low load, and the screen load is not low by its rules.
Positioning that moves idle pods toward demand at this load is a new shared policy, so it needs its own proposal and its own evidence on each project.

### Sensitivity of the individual limits to one pod move

The [positioning pilot](loaded-positioning-proposal.md#pilot-result) found that about 60 early moves in `LF` put hundreds of requests over the limits.
So the maintainer asked for the effect of a change with no service purpose.
A scratch probe on the branch `study/gate-noise` at `473b20bf` ran the screen workload of `pm-peak`, seeds 1 to 3, with the oracle at each simulated second.
The change moves the first idle Compact pod with no trip, in fleet order, to another free berth of its own station, once.
Each arm is compared with the same configuration with no change.
In this table, the changed waits are those of the matched requests.

| Arm | Seed | Matched | Changed waits | Wait over | Journey over | Mean wait (s) |
| --- | --- | --- | --- | --- | --- | --- |
| `LF`, run again | 1 to 3 | 1,104 to 1,133 | 0 | 0 | 0 | same |
| `LF`, move at minute 1 | 1 | 1,054 | 903 | 438 | 378 | 689 to 721 |
| `LF`, move at minute 1 | 2 | 1,071 | 963 | 437 | 381 | 720 to 736 |
| `LF`, move at minute 1 | 3 | 1,028 | 934 | 364 | 324 | 728 to 716 |
| `LF`, move at minute 30 | 1 | 1,072 | 795 | 319 | 271 | 690 to 698 |
| `LF`, move at minute 30 | 2 | 1,075 | 806 | 331 | 294 | 717 to 707 |
| `LF`, move at minute 30 | 3 | 1,028 | 750 | 334 | 282 | 726 to 731 |
| `D`, move at minute 1 | 1 | 1,199 | 0 | 0 | 0 | 182 to 182 |
| `D`, move at minute 1 | 2 | 1,199 | 390 | 113 | 84 | 172 to 169 |
| `D`, move at minute 1 | 3 | 1,199 | 763 | 286 | 209 | 194 to 195 |

The runs are deterministic: the same run twice gives the same result for each request.
The rows of minute 30 are not at minute 30.
The probe tried the move at each tick from minute 30 until a move started, at 1,868 s, 1,899 s, and 1,844 s.

#### External review

An external review by Codex `gpt-6-astra` on November 9, 2026, found that the record above did not support the claim of a noise floor.
Each finding was checked against the code, and each was accepted.

1. Blocker: the move is not neutral.
   It reserves a berth, and its route has no bound, so it is a physical change to the fleet.
   The record must give its route, its time out of service, its claims, and the pods that it blocks.
2. Major: the time of the move was not fixed, because the probe retried after the target tick.
   The time must be fixed before the run, and "no move" is an outcome.
3. Major: refusals depend on the queue state (`screenOffer` refuses an offer when the pending count passes the queue limit), so one change can cascade into other refusals, and the matched set depends on the arm.
   The record must give the admission differences by event, and a diagnostic with no effective queue limit.
4. Major: the probe ignored censoring.
   `screenCompare` counts a request that did not board against the wait limit.
   The screen has no aggregate gate, and the aggregate limit of 2 percent is only in the [numerical limits](../design/experimental-adoption.md#numerical-limits) of the full gates.
5. Major: a deterministic run that repeats does not show chaos.
   The record can say only that the limits are sensitive to this physical change, and it must trace the first divergence.
6. Blocker: counts from a change to `LF` are not a calibrated null for `D` against `LF`.
   `D` is a different fleet, at a different load, with a different matched set.
   So the claim that the 16 to 33 requests of `D` are below a noise floor is withdrawn, and so is the claim that the counts of `LF` with option B are within it.
7. Major: before any change to the limits, the cause of the sensitivity needs study, for example the tie breaks of dispatch and the 30 s hold, and other loads, bands, and queue limits.

The [earlier null perturbation](../studies/default-flips.md#null-perturbation) of one tick did not spread, and that study also said it was not a noise floor for its counts.

#### Follow-up probe

A second probe on the branch `study/gate-noise-2` at `7d93e47b` answers findings 1 to 4.
The move is at 60 s exactly, with no retry.
Each arm is compared with `screenCompare`, which counts censored requests.
A diagnostic arm of `LF` has a queue limit of 1,000,000, so it refuses nothing.

In each seed and arm, the move took `london-pod-001` from berth `01` to berth `02` of Battersea Power Station, at the edge of the network.
Its route was 2,702 m, and it was idle again after 200 s.
No pod had it as its blocker.
It held the reservation of berth `02` during the move, and the probe did not count the effect of the reservation on the berth choices of other pods.
The over-limit counts at minute 1 are the same as in the first probe, so the first probe made this same move.

In this table, the changed waits are those of all background requests, refused requests included.
The over-limit counts include the censored requests.

| Arm | Seed | Refused, no move and move | Admission differences, first at | Changed waits, first offer | Wait over | Journey over | Live queue loss |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `LF`, queue 200 | 1 | 217 and 228 | 251, 1,506 s | 1,036, 432 s | 438 | 378 | 131 |
| `LF`, queue 200 | 2 | 223 and 224 | 243, 1,290 s | 1,089, 246 s | 437 | 381 | 122 |
| `LF`, queue 200 | 3 | 287 and 275 | 286, 1,631 s | 1,092, 198 s | 364 | 324 | 137 |
| `LF`, no queue limit | 1 | 0 and 0 | 0 | 1,044, 432 s | 193 | 176, 2 not finished | 0 |
| `LF`, no queue limit | 2 | 0 and 0 | 0 | 1,066, 246 s | 151, 2 not boarded | 137, 4 not finished | 0 |
| `LF`, no queue limit | 3 | 0 and 0 | 0 | 1,049, 198 s | 294, 16 not boarded | 271, 15 not finished | 0 |
| `D`, queue 200 | 1 | 0 and 0 | 0 | 0 | 0 | 0 | 0 |
| `D`, queue 200 | 2 | 0 and 0 | 0 | 390, 1,542 s | 113 | 84 | 0 |
| `D`, queue 200 | 3 | 0 and 0 | 0 | 763, 210 s | 286 | 209 | 0 |

- The arms of `LF` with no queue limit end with 84, 335, and 384 open orders with no move, and 68, 283, and 393 with the move, so both fail the finite recovery gate.
  These arms are a diagnostic, not a screen configuration.
- With no refusals, `LF` still has 151 to 294 requests over the wait limit.
  So refusal cascades add to the count at a queue limit of 200, but they are not its source.
- In `LF` seeds 2 and 3 and `D` seed 3, the first changed wait is during the 200 s of the move.
  In `LF` seed 1 it is at 432 s, and in `D` seed 2 at 1,542 s, long after the pod was idle again.
  So a part of the effect comes after the move, perhaps from the new berth of the pod.
  This is an observation of time, not a mechanism, and the trace of the first divergence is still open.

#### Conclusion

In this saturated workload, the individual limits are sensitive to one physical change to one pod: in `LF`, one move of 200 s puts 151 to 438 requests over the wait limit.
This does not give a noise floor, and it is not a null for a comparison of `D` with `LF`.
The counts of `D` with the move are a perturbation of `D` too, so they are not a null either.
The gates do not change, and the screen result for `D` stands: it fails the background wait and journey gates.
The [individual limits amendment](individual-limits-amendment-proposal.md) gives the shapes of a change to the limits for later candidates, and the evidence that it needs.
