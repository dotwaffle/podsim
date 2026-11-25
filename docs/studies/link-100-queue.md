# The link 100 queue toward Waterloo

A study of 2026-11-24 on record `i1.17` of the HX11 release soak.
It asks what limits the queue, and if a small change drains it faster.
It is not a qualification, and nothing of it is in the code.

## Method

The runs are the `incidents` arm of the soak on `london-full-shared-express-wharf`, at `39781186`, with the full check once a second, the saves, and the rewinds.
There is no reference run, so the runs have no queue check against one.
Six runs have 6 simulated hours and the clear, on seeds 1, 2, and 3.
A pilot of 45 minutes ran before them on seed 2.

| Arm | Change |
| --- | --- |
| Control | None. |
| `far` | `berthAvailableFor` ignores a traveling pod whose reservation has not reached a lane of its destination station and which owns no resource of its berth. |

`far` is the rule of the [pilot of a smaller berth rule](shared-track-batch.md#a-pilot-of-a-smaller-berth-rule), which is BT-D8 option A.
A probe writes the head of the queue once a second.
The head is the pod on link 100 toward Waterloo, or on its road into the station, that is nearest to the station.
The probe records the first resource that another pod owns in the next reservation of the head, the state of that owner, and the owners of the five berths.
The probe does not change the run: the control gives the same counts and records as the release soak on seeds 1 and 2.

## What limits the queue

In the control, the head is stopped for about half of the run, in seconds of the 21,600 s:

| Measure | Seed 1 | Seed 2 | Seed 3 |
| --- | ---: | ---: | ---: |
| Mean pods in the approach, p95 | 7.7, 13 | 9.4, 16 | 8.4, 15 |
| Head stopped | 11,290 | 10,629 | 11,330 |
| Head stopped at a junction or node | 7,506 | 7,732 | 7,636 |
| Head stopped at an owned berth | 2,479 | 2,553 | 2,920 |
| The same, with another berth that no pod owns | 2,081 | 2,370 | 2,323 |

- Junction conflicts in the station hold the head for about two thirds of its stopped time.
- A berth holds it for most of the rest.
  In 80 to 93 percent of those seconds, another berth has no owner, and the head does not take it.
- In the 45 minute control, the head `express-11` waited 563 s without a break for its berth, until 47 s before `i1.17` started.
  The berth held a pod that boarded and was blocked by an incident.
  In 469 of these seconds another berth had no owner.
  The approach had 12 pods when this wait began, and 16 when it ended.
- During the 707 s after the start, the head was stopped at a junction or node for 451 s, and at a berth for 61 s.
  The head changed 16 times, 1.36 a minute.

So the junctions set the rate at which the queue drains.
The berth refusal adds to the queue: in seed 2 it held a queue that was already 12 pods, and it did not make it.
The record joined the queue that was left.
This corrects the reading of the [trace of `i1.17`](emergency-claim-pilot.md#a-second-late-record), which started at the record and did not see the 563 s before it.

## Results of `far`

| Measure | Seed 1, control and `far` | Seed 2, control and `far` | Seed 3, control and `far` |
| --- | --- | --- | --- |
| Late records | 0, 0 | 1, 0 | 0, 0 |
| Longest record not blocked (s) | 462.1, 479.4 | 699.9, 401.3 | 484.6, 402.0 |
| Completed orders | 4,640, 5,087 | 5,608, 5,766 | 5,062, 5,391 |
| Mean order wait (s) | 762.3, 679.9 | 634.9, 517.4 | 647.2, 588.6 |
| Longest order wait (s) | 4,844, 4,063 | 3,370, 2,861 | 4,906, 4,326 |
| Interrupted orders | 97, 104 | 112, 106 | 125, 126 |
| Fault wait (pod-ticks) | 283,328, 278,787 | 317,065, 292,492 | 479,070, 571,836 |
| Delayed riders (rider-ticks) | 238,719, 238,459 | 272,501, 290,368 | 422,671, 472,586 |
| Settle after the clear (s), limit 1,800 | 15, 402 | 1, 1 | 1, 1 |
| Mean pods in the approach, p95 | 7.7, 13 and 5.5, 10 | 9.4, 16 and 6.3, 10 | 8.4, 15 and 6.1, 11 |
| Head stopped (s) | 11,290, 6,488 | 10,629, 7,140 | 11,330, 7,494 |
| Head stopped at an owned berth, another berth with no owner (s) | 2,081, 247 | 2,370, 127 | 2,323, 34 |

- Completed orders rise by 9.6, 2.8, and 6.5 percent, and the mean wait falls by 10.8, 18.5, and 9.1 percent.
- The approach has 27 to 33 percent fewer pods in the mean.
- No run has a check failure, a demotion, or a late record with `far`.
- `far` is worse in fault wait in seed 3 (+19.4 percent), in delayed riders in seeds 2 and 3 (+6.6 and +11.8 percent), in the longest record of seed 1 (+17.3 s), and in the settle of seed 1 (402 s).
  The study does not show the causes.
- The trajectories differ from minute 3 in seed 2.
  So the runs do not show what `far` does to `i1.17` itself.
  They show that the queue is shorter in each seed.

## Other presets

The same two arms ran on three other presets on 2026-11-25, with the `incidents` arm, 6 simulated hours and the clear, seeds 1, 2, and 3, no probe, and no reference run.
So these runs, too, have no queue check after the clear.
Each cell has the control and then `far`.

| Preset, seed | Completed orders | Mean order wait (s) | Longest record not blocked (s) | Settle (s) | Fault wait (pod-ticks) | Delayed riders (rider-ticks) |
| --- | --- | --- | --- | --- | --- | --- |
| `london-full-shared-express`, 1 | 4,381, 4,576 | 756.5, 720.6 | 406.3, 507.4 | 187, not in 1,800 | 228,278, 342,932 | 205,417, 292,960 |
| `london-full-shared-express`, 2 | 4,808, 5,041 | 689.9, 389.5 | 465.9, 263.8 | 605, 1 | 370,398, 357,405 | 303,557, 275,579 |
| `london-full-shared-express`, 3 | 4,192, 4,785 | 722.0, 539.0 | 506.9, 496.9 | 1, 143 | 438,073, 285,798 | 356,994, 338,644 |
| `london-central`, 1 | 4,129, 4,128 | 997.4, 997.9 | 164.9, 181.5 | 127, 200 | 622,340, 436,090 | 467,999, 204,004 |
| `london-central`, 2 | 4,060, 4,144 | 1,007.8, 989.1 | 223.0, 180.6 | 1, 1 | 379,618, 519,764 | 235,834, 371,590 |
| `london-central`, 3 | 4,076, 4,070 | 1,010.1, 1,008.1 | 213.4, 233.5 | 1, 1 | 550,856, 610,712 | 312,518, 368,860 |
| `rail-hub`, 1 | 634, 637 | 4,629.9, 4,628.9 | 196.6, 200.5 | 120, 77 | 1,093,976, 952,299 | 973,459, 813,464 |
| `rail-hub`, 2 | 675, 713 | 4,428.7, 4,245.5 | 216.5, 213.5 | 1, 391 | 826,309, 886,814 | 687,622, 743,436 |
| `rail-hub`, 3 | 598, 548 | 4,802.0, 5,062.4 | 212.7, 210.9 | 8, 1 | 895,464, 913,966 | 681,110, 706,921 |

- `far` fails one run that the control passes: seed 1 of `london-full-shared-express` misses the Progress gate after the clear.
  At the last reading, 1,800 s after the clear, `london-pod-056` has a fault hold, with no wait reason and no blocker.
  The cause is [below](#the-progress-miss).
- No run has another gate miss, a check failure, a demotion, or a late record.
- On `london-full-shared-express`, completed orders rise by 4.5, 4.8, and 14.1 percent, and the mean wait falls by 4.7, 43.5, and 25.3 percent.
  In seed 1 the fault wait rises by 50.2 percent, the delayed riders by 42.6 percent, and the longest record by 101.1 s.
- On `london-central`, completed orders change by 0.0, +2.1, and -0.1 percent.
- On `rail-hub`, completed orders change by +0.5, +5.6, and -8.4 percent, so three seeds show no direction.
- The fault wait and the delayed riders go both ways on each preset.

## The Progress miss

A second run of seed 1 with `far` on `london-full-shared-express` logged `london-pod-056` every 10 s and each remaining wait every 30 s after the clear.
It gave the same counts and the same miss.

- The pod drove from Epping (berth `940GZZLUEPG-01`) to `940GZZLUHSD`.
  A fault stopped it 5,594 m after the start, and its rider was evacuated on the lane.
  It then had an empty recovery (purpose 3) on its original route, as the [suspension contract](../contracts/incident-suspension-contract-proposal.md) sets for a lane evacuation.
- At the clear, 31,527 m of that route were left.
  At 14 m/s that takes at least 2,252 s, and the gate has 1,800 s.
- In 177 of its 180 readings after the clear, the pod drove at 14 m/s.
  The other three were slower, two of them with "Junction traffic" behind `express-08`.
  It was never stopped in a reading, and it had 6,625 m left in the last one.
- Its hold ends at the arrival, so no berth or junction rule can meet the gate for this pod at that route and speed.
  Another pod, `london-pod-074`, held the gate until between 450 and 480 s, and then this pod alone.

So the miss is a long drive and not a block.
`far` changes which pods have a fault at the clear, and in this run one of them had a route too long for the gate.
One run does not show that `far` makes such a drive more likely.
[N](emergency-claim-pilot.md#the-settle-of-n) had the same kind of wait, 771 s long.

## The narrowed rule

The [proposal](../contracts/distant-berth-target-proposal.md) narrows `far` with a fourth clause: the ignored pod must be able to choose a berth again (`choosesBerth`).
Six runs of it on 2026-11-25 have the method of "Other presets" and the Go source of `39781186`.
The controls are the recorded ones, and the wharf controls had the probe.
Each cell has the control and then the narrowed rule.

| Preset, seed | Late | Completed orders | Mean order wait (s) | Longest record not blocked (s) | Settle (s) | Fault wait (pod-ticks) | Delayed riders (rider-ticks) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Wharf, 1 | 0, 0 | 4,640, 4,848 | 762.3, 719.3 | 462.1, 476.9 | 15, 297 | 283,328, 251,026 | 238,719, 155,894 |
| Wharf, 2 | 1, 0 | 5,608, 5,867 | 634.9, 370.9 | 699.9, 374.4 | 1, 421 | 317,065, 293,126 | 272,501, 288,684 |
| Wharf, 3 | 0, 0 | 5,062, 5,544 | 647.2, 516.0 | 484.6, 496.9 | 1, 1 | 479,070, 391,558 | 422,671, 329,110 |
| `london-full-shared-express`, 1 | 0, 0 | 4,381, 4,224 | 756.5, 785.9 | 406.3, 507.4 | 187, 275 | 228,278, 249,465 | 205,417, 235,329 |
| `london-full-shared-express`, 2 | 0, 0 | 4,808, 5,057 | 689.9, 333.5 | 465.9, 237.2 | 605, 1 | 370,398, 303,521 | 303,557, 170,723 |
| `london-full-shared-express`, 3 | 0, 0 | 4,192, 4,784 | 722.0, 536.4 | 506.9, 571.5 | 1, 1 | 438,073, 370,429 | 356,994, 318,281 |

- All six runs pass, with no late record, check failure, or demotion.
- Wharf completes 4.5, 4.6, and 9.5 percent more orders, and its mean wait falls by 5.6, 41.6, and 20.3 percent.
- `london-full-shared-express` completes -3.6, +5.2, and +14.1 percent, and its mean wait changes by +3.9, -51.7, and -25.7 percent.
  Seed 1 is worse in each measure of the table.
- The longest record rises in four of the six runs, and it is 571.5 s in seed 3 of `london-full-shared-express`, 28.5 s from the limit.
- These are different trajectories, so they do not show the effect on one record.
- Seed 1 of `london-full-shared-express` passes the settle at 275 s.
  The long drive of `far` is not in that run.

## What it does not show

- The release form with the full check at each tick, the `emergencies` arm, or the settle against a reference queue.
- The cause of the higher fault wait and delayed riders in seed 1 of `london-full-shared-express`.
- How often a long recovery drive makes a run miss the Progress gate, with the rule or without it.
- `LF` under its stopping load.
  In the earlier pilot, `LF` with this rule completed fewer orders: 350 against 368 in seed 1, 397 against 431 in seed 2, and 350 against 361 in seed 3.
- That a pod that gives up its place to another pod always gets a berth.
  With `far`, a distant pod can lose its berth and choose again.
- An effect on the junctions, which are the larger limit.
  A change of the station geometry or of the reserved zone was not in this study.
