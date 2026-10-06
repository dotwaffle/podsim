# LondonFull controller sustained-load study

Buffers and pickup reassignment remain off by default.
At 10 requests per minute, late backlogs stay nearly flat in these six-hour arrival windows.
Every policy grows a backlog at 15 and 20 requests per minute.
Reassignment improves average service under those higher loads, but some matched requests wait much longer.
These finite runs do not establish indefinite capacity or justify default adoption.

## Inputs and checks

The frozen runtime uses source `bab8559`, after the pickup buffer, reassignment, and remote berth-claim fixes.
Later performance changes have separate qualification records.
The Full project contains 269 passenger stations, three Parking sites, and 287 initial pods.
Demand uses the 2024 endpoint AM Peak profile throughout the arrival window.
Each rate runs seeds 1 and 2 with baseline, buffers, reassignment, and both, for 24 arms.
Ordered schedules match exactly within each rate and seed.

Arrivals continue for six simulated hours, followed by up to one hour without arrivals.
The target rates of 10, 15, and 20 per minute schedule 3,599, 5,399, and 7,199 requests.
The comparison queue limit is 1,000,000 so offered demand is not skipped.
The live queue limit remains 200.
Sharing and redistribution are off, routing is free-flow, and virtual platoons have a four-pod limit.

All accepted IDs reconcile with completed, aboard, or pending records.
No request is skipped.
Safety, speed limits, and request conservation pass once per simulated second.
Simulation state restores physically at three and six hours without demotions, requeues, or dropped parties.
Each restored copy continues for 60 seconds without new arrivals, with both controllers disabled and checks every tick.
The measured simulation retains its original clock and owners.
Four one-hour arrival pilots also match production results exactly and pass the once-per-second checks.
These checks do not prove indefinite deadlock freedom.

Two initial 20/min runs reached their 880-second test deadlines while still computing.
The continuation repeats those two cases and starts four previously unstarted cases with the same binary and inputs.
All six pass with longer limits.
Original timeout evidence remains separate from the successful continuation.
Concurrent functional jobs make wall times unsuitable for CPU claims.

## Completion and late backlog

Late backlog change measures outstanding orders from hour three to hour six, divided by 180 minutes.
Positive values mean arrivals outpace completions over that interval.
Completion counts include the recovery period.

| Offered per minute | Policy | Completed / accepted, seed 1 | Completed / accepted, seed 2 | Late backlog orders/min, seeds 1 / 2 |
| ---: | --- | ---: | ---: | ---: |
| 10 | Baseline | 3,598 / 3,599 | 3,599 / 3,599 | +0.04 / -0.12 |
| 10 | Buffers | 3,599 / 3,599 | 3,599 / 3,599 | +0.01 / -0.10 |
| 10 | Reassignment | 3,598 / 3,599 | 3,599 / 3,599 | -0.01 / -0.07 |
| 10 | Both | 3,599 / 3,599 | 3,599 / 3,599 | -0.01 / -0.10 |
| 15 | Baseline | 4,996 / 5,399 | 5,042 / 5,399 | +3.04 / +3.12 |
| 15 | Buffers | 5,057 / 5,399 | 5,068 / 5,399 | +2.92 / +3.09 |
| 15 | Reassignment | 5,273 / 5,399 | 5,290 / 5,399 | +2.33 / +2.29 |
| 15 | Both | 5,311 / 5,399 | 5,310 / 5,399 | +2.14 / +2.32 |
| 20 | Baseline | 4,990 / 7,199 | 4,949 / 7,199 | +8.12 / +8.29 |
| 20 | Buffers | 4,999 / 7,199 | 4,932 / 7,199 | +8.06 / +8.33 |
| 20 | Reassignment | 5,275 / 7,199 | 5,269 / 7,199 | +7.28 / +7.38 |
| 20 | Both | 5,301 / 7,199 | 5,295 / 7,199 | +7.38 / +7.34 |

At 10/min, request 3,447 remains aboard at the cap in seed 1 baseline and reassignment runs.
Every request has boarded in those two runs.
The remaining arms finish all requests.
Small late-backlog changes over two seeds do not establish steady-state stability.

At 15/min, both controllers reduce the backlog growth rate but do not stop it.
At 20/min, buffers alone provide little completion benefit and complete fewer requests in seed 2.
The combined policy is not consistently better than reassignment alone across every late-window metric.

## Waits, matched requests, and censoring

Mean and p95 wait include exact request-to-boarding waits and elapsed ages for requests still waiting at the cap.
An elapsed age is a lower bound, not an eventual pickup time.
Completed journey comparisons use only requests completed in both arms.
Exact wait comparisons use only requests that boarded in both arms, including parties still aboard at the cap.

At 15/min, baseline mean wait-or-age is 2,228 and 2,038 seconds for seeds 1 and 2.
Buffers give 2,018 and 1,952 seconds, reassignment gives 1,585 and 1,522, and both give 1,434 and 1,442.
These aggregate reductions coexist with individual regressions.
For exact both-boarded waits, buffer-only seed 2 delays one matched request by 3,069.53 seconds.
In 20/min reassignment seed 1, the largest matched increase is 5,211.60 seconds.
These are individual differences, not differences between two aggregate maxima.

Some candidate requests remain unboarded after their baseline counterparts have boarded.
Their excess age proves a minimum wait regression even though the final wait is unknown.
Buffer-only runs contain 11 and 14 such cases at 15/min, and 18 and 31 at 20/min.
Other mixed completed, aboard, and pending cohorts remain explicit in the metadata.
The descriptive 30/120/300-second counts are not approved adoption limits.

At 10/min, reassignment's matched mean wait change is -0.83 seconds for seed 1 and +1.26 for seed 2.
Buffers and the combined policy improve those means, while still producing long individual regressions.
The [earlier qualification](dispatch-policy-qualification.md) also records mixed effects on stopped time and station queues.
Numeric tail limits, broader demand coverage, and causal investigation remain necessary before any adoption recommendation.

## Intermediate rates

The 12, 13, and 14 per minute studies use the frozen `bab8559` source, Full geometry, AM Peak profile, and checks of this study.
Arrivals run six hours, with up to one hour of recovery and a queue limit of 1,000,000.
Sharing and redistribution are off, routing is free-flow, and virtual platoons have a four-pod limit.
All arms pass the once-per-second checks and the physical restores at three and six hours, with a 60-second disabled-controller continuation.
No additional equivalence pilot runs at these rates.
The 12/min study covers seeds 1 to 4 with four policies and 4,319 requests per arm.
The 13/min study covers seeds 1 and 2 with baseline and both, at 4.6-second intervals (about 13.043/min) and 4,695 requests.
The 14/min study uses the same arms at 257-tick intervals (about 4.283 s) and 5,042 requests.
Neither 13/min nor 14/min tests a controller alone.
Cells below read completed / accepted; late backlog orders/min; mean wait in seconds.
Late backlog change is outstanding orders from hour three to hour six, divided by 180 minutes.

| 12/min seed | Baseline | Reassignment | Buffers | Both |
| ---: | --- | --- | --- | --- |
| 1 | 4,317 / 4,319; -0.006; 349.02 | 4,319 / 4,319; -0.044; 345.78 | 4,317 / 4,319; -0.056; 329.04 | 4,318 / 4,319; -0.067; 319.13 |
| 2 | 4,315 / 4,319; +0.039; 363.25 | 4,315 / 4,319; +0.039; 355.04 | 4,315 / 4,319; +0.000; 333.05 | 4,315 / 4,319; +0.033; 331.51 |
| 3 | 4,318 / 4,319; +0.083; 356.92 | 4,318 / 4,319; +0.094; 353.66 | 4,318 / 4,319; +0.089; 332.22 | 4,318 / 4,319; +0.094; 331.71 |
| 4 | 4,319 / 4,319; -0.100; 358.31 | 4,319 / 4,319; -0.094; 357.73 | 4,319 / 4,319; -0.122; 334.66 | 4,319 / 4,319; -0.128; 331.96 |

Every 12/min request boards before its run ends, so the means are exact waits, and zero to four parties remain aboard at the cap.
Matched mean wait improves by 19.98 to 30.20 s with buffers, 0.58 to 8.21 s with reassignment, and 25.21 to 31.74 s with both.
Every comparison has 83 to 119 requests that wait more than 300 s longer.
The largest increase is request 1324 in seed 4 with reassignment alone: +5,404.73 s, from 481.02 to 5,885.75 s.
It has no recorded direct reassignment, so this is an indirect effect of different fleet histories, not a swap prediction error.

| 13/min seed | Baseline | Both | Mean improvement, s | Largest increase, s | Increases over 300 s |
| ---: | --- | --- | ---: | ---: | ---: |
| 1 | 4,693 / 4,695; -0.044; 371.58 | 4,694 / 4,695; -0.033; 349.55 | 22.03 | 1,638.73 | 136 |
| 2 | 4,692 / 4,695; +0.011; 378.33 | 4,692 / 4,695; -0.028; 357.49 | 20.85 | 2,309.92 | 134 |

At 13/min all requests board, and one to three parties remain aboard at the cap.

| 14/min seed | Baseline | Both |
| ---: | --- | --- |
| 1 | 5,040 / 5,042; -0.050; 398.74 | 5,041 / 5,042; -0.044; 376.38 |
| 2 | 5,017 / 5,042; +1.683; 843.09 | 5,041 / 5,042; -0.028; 397.56 |

The 14/min means are wait-or-age: exact waits plus elapsed lower-bound ages for requests unboarded at the cap.
At seed 2 the baseline ends with 3 pending and 22 aboard, and both ends with 0 and 1.
Seed 1 compares all 5,042 exact waits: the mean improves by 22.36 s, 188 requests wait over 300 s longer, and the largest increase is 1,952.70 s.
Seed 2 compares the 5,039 requests boarded in both arms: the mean improves by 444.29 s, with 94 increases over 300 s and a largest of 1,374.18 s.
The three remaining baseline requests have censored ages, so their eventual waits are unknown.
The pre-routing 14/min seed-2 baseline backlog of +1.683 orders/min is not attributed to the reciprocal resource wait of the 12/min seed-4 run in the [Stratford diagnosis](berth-route-preference.md).

These late windows are nearly flat, unlike every tested arm at 15 and 20/min, except the 14/min seed-2 baseline.
The 300-second counts are descriptive, not approved adoption limits.
The studies make no sustainable-rate claim and do not cover other demand bands or longer arrival windows.
Buffers and pickup reassignment remain off by default.

## Evidence

The raw measurement data is in git history.
This study does not replace the [finite-arrival capacity envelope](london-full-postfix.md).
The [Stratford diagnosis](berth-route-preference.md) identifies an intermediate-berth routing obstruction in the 12/min seed-4 run.
It does not establish the cause of backlogs in the other runs.
