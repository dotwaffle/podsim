# Service after the berth-routing fix

The selected LondonFull runs pass safety, request accounting, and physical-restore checks.
The diagnosed Stratford request avoids its earlier intermediate-berth wait in the new history.
Late backlog growth also decreases in the 14/min seed-2 baseline.
Individual service regressions remain, so buffers and reassignment stay off by default.

## Source and checks

The candidate is `89e1e1d`, which includes the [route preference](berth-route-preference.md) and passenger arrival-chain commitment fix.
It uses the unchanged Full project, AM Peak demand, four-pod virtual platoons, and frozen qualification helpers.
Sharing and redistribution remain off.
The study queue limit is 1,000,000 and accepts every scheduled request.
The live queue limit remains 200.

Two short pilots compare the observer with production exactly and check safety every tick.
Eight full arms offer six hours of arrivals with a seven-hour cap.
They cover 12/min seeds 1 and 4 with baseline or reassignment, and 14/min seeds 1 and 2 with baseline or both controllers.
Every full arm checks safety, speed, and conservation once per simulated second.
Each restores physically at three and six hours, then checks a 60-second disabled-controller continuation every tick.
No request is skipped, demoted, requeued, or dropped during restore.

The earlier cached histories use `bab8559` and the same project, schedules, and qualification helpers.
A separate `60d523e` pre-routing witness exactly reproduces the cached 12/min seed-4 reassignment result, schedule, and every check field.
This includes request timings and reassignment decisions.
That witness bridges the diagnosed case only.
The other seven comparisons remain cached-source versus post-routing comparisons.
Changed histories measure the combined routing and commitment patch, without separating either component's effect.

## Backlog and recovery

Late backlog change counts outstanding orders from hour three to hour six, divided by 180 minutes.
Completed requests include recovery.
Pending and aboard requests remain separate at the observation cap.

| Nominal rate/min | Seed | Policy | Cached late growth/min | New late growth/min | New completed / accepted | New pending / aboard |
| ---: | ---: | --- | ---: | ---: | ---: | ---: |
| 12 | 1 | Baseline | -0.006 | -0.056 | 4,317 / 4,319 | 0 / 2 |
| 12 | 1 | Reassignment | -0.044 | -0.083 | 4,317 / 4,319 | 0 / 2 |
| 12 | 4 | Baseline | -0.100 | -0.078 | 4,319 / 4,319 | 0 / 0 |
| 12 | 4 | Reassignment | -0.094 | -0.094 | 4,319 / 4,319 | 0 / 0 |
| 14 | 1 | Baseline | -0.050 | +0.000 | 5,040 / 5,042 | 0 / 2 |
| 14 | 1 | Both | -0.044 | -0.039 | 5,041 / 5,042 | 0 / 1 |
| 14 | 2 | Baseline | +1.683 | +0.139 | 5,039 / 5,042 | 0 / 3 |
| 14 | 2 | Both | -0.028 | -0.011 | 5,040 / 5,042 | 0 / 2 |

The 14/min seed-2 baseline completes 5,039 requests, compared with 5,017 in the cached history.
Its late backlog growth decreases from 1.683 to 0.139 orders per minute.
It still has positive growth in this window and three parties aboard at the cap.
The combined policy completes 5,040 requests and retains nearly flat late backlog in this seed.
These finite windows do not establish indefinite capacity or cover other demand bands.
The separate 12/min Stratford case does not identify the cause of the 14/min seed-2 improvement.

## Matched service

Controller comparisons below use the new-source baseline, separate from same-policy cached-history comparisons.
Exact deltas include only requests that board in both arms.
A negative mean means the controller arm boards those requests sooner.
The 300-second counts describe outcomes and are not adoption limits.

| Nominal rate/min | Seed | Exact mean delta, seconds | Requests over 300 seconds slower | Largest increase, seconds |
| ---: | ---: | ---: | ---: | ---: |
| 12 | 1 | -2.52 | 106 | 1,520.42 |
| 12 | 4 | -3.22 | 113 | 1,629.73 |
| 14 | 1 | -22.77 | 186 | 1,557.60 |
| 14 | 2 | -48.02 | 179 | 1,525.55 |

Better matched means coexist with individual increases of about 25-27 minutes.
All eight new arms board every accepted request, but some parties remain aboard at the cap.
The metadata retains completion cohorts and unfinished IDs.
Cached histories with pending requests retain their elapsed ages as lower bounds, separate from exact boarded waits.
The paired metadata also retains same-policy before/after deltas.
Neither those deltas nor the controller means establish an individual-service guarantee.

## Stratford request 1324

The [original case](pickup-seed4-tail.md) waited 481.02 seconds in baseline and 5,885.75 seconds with reassignment.
The new waits are 446.30 and 664.10 seconds.
Reassignment therefore still adds 217.80 seconds for this request in the new history.

A read-only frame replay exactly matches the new result, schedule, and inherited check fields.
Request 1324 boards pod 112 at tick 437046.
Its captured pickup route starts at Tower Hill and ends at Leyton.
Only the Leyton destination berth appears on that route.
The replay records 664 pending boundary samples: 29 unassigned, one stopped departure sample, and no berth-occupied samples.
There is no direct reassignment record for request 1324.

The earlier pod-123/pod-025 Stratford cycle does not appear in this request's new history.
This does not establish cycle prevention for every pod, legacy fallback, or previously saved committed route.
The changed fleet history also prevents attributing this result to one earlier dispatch decision.

## Evidence

[Arm totals](measurements/berth-routing-service.csv) retain the selected cached and new results.
[Matched metadata](measurements/berth-routing-service.json) retains source and input hashes, run outcomes, exact cohorts, unfinished IDs, and request-1324 frames.
Raw runs, per-request deltas, frozen helpers, and validation logs remain in `~/.cache/agents/podsim/routing-followup-20260930/`.
All owned study units stopped successfully and their compiled scratch binaries were removed.
Concurrent functional runs do not support CPU timing claims.
