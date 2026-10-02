# Pickup availability diagnosis

Status: bounded attribution on main `8b81b6f`, October 2, 2026.
This study changes no simulation behavior or defaults.
The [measurement record](measurements/pickup-availability.json) preserves source, output, and run receipt hashes.

## Result

The worse independent-bank pickup waits come mainly from unavailable fleet supply.
The exact seed-2 decomposition confirms the earlier sampled result.
A shorter assigned traffic hold does not offset the extra time before a pod becomes available.

| Heavy seed 2 | Baseline | Independent banks |
| --- | ---: | ---: |
| Offered orders | 1,440 | 1,440 |
| Accepted and completed | 366 | 360 |
| Skipped at the unchanged queue limit | 1,074 | 1,080 |
| Orders completed by both arms | 312 | 312 |
| Common-order mean pickup wait | 2,496.011 s | 2,607.774 s |

All accepted orders finish within the predeclared three-hour cap.
These overloaded finite runs do not establish sustained capacity.
Compare offers by schedule position and identity, because skipped offers change accepted request IDs.

| Common-order phase | Mean change, banks minus baseline |
| --- | ---: |
| No available pod | +97.410 s |
| Finishing-pod advisory | +1.843 s |
| Assigned moving or dwell | +14.056 s |
| Assigned stationary traffic hold | -1.546 s |
| Total pickup wait | +111.763 s |

The first two phases account for 88.81% of the added pickup wait.
This is an exact tick partition for the 312 common completed orders.
It does not attribute the longer fleet cycle to one route or maneuver.

## Fleet while orders remain unassigned

Sample the complete fleet once per simulated second when at least one origin has an unassigned order.
These are conditional fleet pod-seconds, not request-weighted wait durations.

| Fleet use | Baseline share | Bank share |
| --- | ---: | ---: |
| Traveling with passengers | 55.110% | 54.830% |
| Assigned empty pickup travel | 41.198% | 41.712% |
| Committed empty travel | 2.774% | 2.563% |
| Passenger boarding dwell | 0.550% | 0.528% |
| Passenger unloading dwell | 0.347% | 0.332% |
| Available | 0.006% | 0.020% |

Ready or departing assigned pods account for the small remainder.
The raw observer called boarding dwell `ineligible-empty/Boarding` because `Pod.Occupied` becomes true at departure.
Those pods already have riders.
The measurement summary corrects the label without changing the frozen raw output.

A separate origin-second census finds periods with no fleet candidate and periods with candidates that cannot divert or reach that origin.
The census does not distinguish those latter mechanisms per origin.
It does not prove a routing defect or authorize dropping committed resources.

## Observation and validation

A private Go overlay collects each pending request's phase every tick.
It includes the submission tick and excludes the boarding tick.
For every accepted order in all four arms, phase ticks equal `BoardedTick - RequestedTick` exactly.
The fleet tick total equals 30 times the observed tick count in each arm.
An unresolved final order would need a separate endpoint convention.
This study has no unresolved accepted orders.

The two light pilots each complete 12 of 12 offers without skips.
The baseline pilot compares instrumented and plain results exactly.
Its zero pending wait does not exercise the per-origin route probes.
Both heavy arms reproduce their independently frozen result records and every passenger timing exactly.

The observer can populate route caches through eligibility probes.
A separate native test compares saved state before and after observation, and full state apart from the existing test's route/admission work caches, dispatch buffers, and block cursors.
It then compares measured and unmeasured clone continuations for 60 ticks at each sampled checkpoint.
The test covers current, strict, and disabled finishing-pod rules in a six-pod busy fixture.
It passes.
This is not a disabled-path CPU benchmark.

Safety checks run every live tick: 452,460 baseline ticks and 460,560 bank ticks.
Lane speed checks run every tick.
Saved-contract checks run once per second.
Each heavy arm passes 25 physical restore probes and their 60-tick continuations.
Saved-contract checks do not verify every live owner-map invariant.
Three berth reservation-holder changes occur during physical restore in each heavy arm, as in earlier studies.
No accepted request is demoted, requeued, dropped, or lost at the planned checkpoints.

## Next service target

Do not repeat unchanged bank or inlet-pacing screens to seek a pickup improvement.
Investigate lower empty pickup travel and more useful passenger service per fleet cycle.
Order-time sharing consent and whole-party seat fit are prerequisites for meaningful shared pickup experiments.
A private group must remain private even when a larger pod has spare seats.
Class-compatible stations and routes must constrain larger-pod experiments.

Tighter stopped queues remain useful as a storage target.
The existing [recruitment study](station-buffer-recruitment.md) shows a smaller stopped span, slower settling, and unchanged controlled discharge.
A below-12-meter experiment needs its own numeric, certificate, drain, and restore contract.
Neither storage improvement nor this diagnosis satisfies the [adoption gates](experimental-adoption.md).
