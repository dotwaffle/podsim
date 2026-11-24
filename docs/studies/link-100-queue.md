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

## What it does not show

- The release form with the full check at each tick, the `emergencies` arm, or the settle against a reference queue.
- Another preset.
  In the earlier pilot, `LF` with this rule completed fewer orders: 350 against 368 in seed 1, 397 against 431 in seed 2, and 350 against 361 in seed 3.
  That load stops `LF` in each arm.
- That a pod that gives up its place to another pod always gets a berth.
  With `far`, a distant pod can lose its berth and choose again.
- An effect on the junctions, which are the larger limit.
  A change of the station geometry or of the reserved zone was not in this study.
