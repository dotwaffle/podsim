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
