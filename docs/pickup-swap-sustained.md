# LondonFull pickup-swap load screen

Pickup swaps reduce mean wait, p95 wait, and empty travel in all four paired cases.
They increase maximum request-to-alight time in both seed 2 cases.
Every arm has unfinished work and growing late-arrival backlog.
The controller remains disabled by default.

## Method

The frozen production source is `42747d5`.
The project is the current mirrored LondonFull network from the [mirror study](station-mirror-load.md).
It uses 287 pods, 269 passenger sites, three Parking facilities, and 2024 AM peak endpoint demand.

Each arm receives six hours of arrivals and has one further hour to finish.
Rates are 15 and 20 requests per minute, with seeds 1 and 2.
Each schedule runs with pickup swaps off and on.
Sharing, redistribution, and station buffers remain off.
Routing is free-flow, with virtual platoons limited to four pods.
The [controller limits](pickup-reassignment.md#bounded-experiment) remain unchanged.

All eight arms complete the full seven-hour observation window.
The four disabled results match every field of the earlier mirrored-network control results.
Both variants use byte-identical ordered demand schedules for each rate and seed.
No arm skips a request.
The schedules contain 5,399 requests at 15/minute and 7,199 at 20/minute.

## Results

Each pair below gives swaps off / on.
Wait statistics include elapsed wait for requests that have not boarded by the cutoff.
Those waits can increase after the observation window.
Request-to-alight statistics include completed journeys only.

| Requests/minute | Seed | Completed | Mean wait | p95 wait | Empty travel change | Swaps |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 15 | 1 | 4,996 / 5,282 | 2,228.1 / 1,572.5 s | 4,553.4 / 3,368.0 s | -8.52% | 858 |
| 15 | 2 | 5,042 / 5,264 | 2,038.1 / 1,651.4 s | 4,375.7 / 3,537.3 s | -6.03% | 865 |
| 20 | 1 | 4,990 / 5,261 | 5,258.1 / 4,764.4 s | 9,399.0 / 8,577.0 s | -5.97% | 1,081 |
| 20 | 2 | 4,949 / 5,237 | 5,330.7 / 4,851.1 s | 9,487.9 / 8,667.0 s | -6.06% | 1,073 |

The enabled controller makes 3,877 swaps across the four arms.
Mean wait decreases by 9.00% to 29.42%.
The controller's predicted savings do not establish realized savings for individual requests.

| Requests/minute | Seed | Maximum request-to-alight | Unfinished at cutoff | Late backlog growth | Late completions/minute |
| --- | ---: | ---: | ---: | ---: | ---: |
| 15 | 1 | 8,216.2 / 7,796.8 s | 403 / 117 | +548 / +408 | 11.956 / 12.733 |
| 15 | 2 | 8,196.3 / 9,013.1 s | 357 / 135 | +561 / +421 | 11.883 / 12.661 |
| 20 | 1 | 12,386.2 / 11,966.4 s | 2,209 / 1,938 | +1,462 / +1,346 | 11.878 / 12.522 |
| 20 | 2 | 12,268.0 / 12,872.2 s | 2,250 / 1,962 | +1,493 / +1,349 | 11.706 / 12.506 |

Late backlog growth and throughput cover hours three through six, while arrivals continue.
Swaps improve aggregate completion rates, but every late completion rate remains below its offered rate.
These finite runs do not establish sustained capacity or an acceptable passenger wait limit.
The seed 2 maximum journey regressions are 9.97% at 15/minute and 4.93% at 20/minute.
An aggregate improvement does not establish that every request benefits or that starvation cannot occur.

## Validation and limits

All arms pass 201,600 parent safety, speed, and order-accounting observations, one per simulated second.
The final census checks unique pending requests and rider timings against submitted and completed totals.
These sampled checks do not prove every-tick safety for untested schedules.
Disabled swap counters remain zero.
Every enabled arm scans eligible candidates and commits swaps.

Each arm creates physical restore copies at hours one and five without changing the measured parent.
All 16 copies restore without degraded placement, demoted pods, or requeued or dropped requests.
They preserve pod positions, request bindings, pending requests and routes, admission ages, and saved counters.
Restore normalizes speed and transient wait fields.
Each copy advances for one minute with safety, speed, and order checks on every tick.
Together they pass 57,600 continuation observations.

Restored copies disable pickup swaps because the saved format does not retain controller history.
These runs do not qualify identical future experimental decisions or trajectories after restart.
Checkpoint records do not count swaps before each restore.
The separate `TestPickupSwapImmediateRestoreKeepsAssignments` regression explicitly restores immediately after a successful swap.
It checks the swapped physical bindings and subsequent completion with the controller disabled.

Wall time includes instrumentation and concurrent work.
It does not measure controller overhead or maximum production playback speed.
Further qualification needs individual-request comparisons, additional seeds and loads, and a controlled CPU comparison.
No preset, policy default, saved-state contract, or wire API changes follow from this screen.

The raw measurement data of these studies is in git history.

## Matched request diagnosis

The two higher completed-journey maxima in the sustained swap study come from requests that remain unfinished with swaps off.
Those requests already have longer elapsed journeys at the cutoff than their enabled completion times.
The larger completed maximum therefore does not establish harm to those same requests.
Other requests do become slower: about 15% and 8% of the shared completed cohorts worsen in these two cases.
The experimental policy remains disabled by default.

### Method and validation

The study repeats seed 2 at 15 and 20 requests per simulated minute, with swaps off and on.
It uses the same mirrored LondonFull geometry, 287 pods, AM peak demand, six-hour arrival window, and seven-hour cap.
Sharing, redistribution, and station buffers remain off.
Virtual platoons have a four-pod limit and routing is free-flow.
Only test-helper exports change.
They add final request timings and pending requests without changing the simulation.

All four aggregate result records match the screen above exactly.
Each pair has byte-identical ordered request schedules.
Disabled controller counters remain zero and both enabled arms have nonzero swaps.
Safety, speed-limit, unique-request census, and physical restore checks pass.
The restore copies keep physical assignments but disable the unsaved experimental controller, as in the original study.
They do not promise identical experimental decisions after restart.

The analyzer validates all 12,598 scheduled IDs and submission ticks against their ordered schedule positions.
Every submitted request occurs exactly once in final boarded timings or pending requests.
Completed timing counts match served totals.
Recomputed completed journey mean, nearest-rank p95, and maximum match the original results.
No request is skipped.
The runtime is `bd2bd7c`, with only external test helpers.
No production, project, saved-state, wire, or default changes.

### Shared cohorts

Wait comparisons include only requests that start boarding in both arms.
Journey comparisons include only requests that complete in both arms.
Negative change means swaps on is faster for the same request.
These conditional cohorts are not all-request averages.

| Requests/minute | Boarded in both | Mean wait change | Completed in both | Mean journey change | Completed requests that worsen | Largest journey increase |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 15 | 5,197 | -380.69 s | 5,041 | -360.23 s | 763 (15.1%) | 2105.77 s |
| 20 | 5,088 | -634.81 s | 4,949 | -615.98 s | 400 (8.1%) | 1481.33 s |

The shared-completed p95 of individual journey changes is +172.65 seconds at 15/minute and +43.60 seconds at 20/minute.
These are percentiles of per-request changes, not differences between the aggregate journey p95 values.
The maximum individual worsening is about 35.1 minutes and 24.7 minutes.
A lower average and better completed count do not imply a no-harm policy.
The pairwise free-flow prediction protects its immediate estimate, not realized waiting time under the changed traffic pattern.
These exports do not identify which requests were directly swapped.
They measure the whole policy's effect on each request.

At 15/minute, 223 requests complete only with swaps on, and one completes only with swaps off.
At 20/minute, 288 requests complete only with swaps on, and none completes only with swaps off.
Another 28 and 1,813 requests remain unboarded in both arms.
Other requests remain aboard in one or both arms.
The cohort CSV preserves every status combination.
Pending wait and unfinished journey duration are lower bounds at cutoff, not final service times.

### Requests that set the maxima

Request 3955, Chesham to Stratford, sets the enabled completed maximum at both rates.
It remains aboard with swaps off.
Its disabled elapsed duration already exceeds its enabled completion duration.

| Requests/minute | Request | Off: unfinished journey lower bound | On: completed journey | Boarding wait change |
| --- | ---: | ---: | ---: | ---: |
| 15 | 3955 | 9,380.00 s | 9,013.12 s | -388.75 s |
| 20 | 3955 | 13,335.00 s | 12,872.22 s | -798.83 s |

The result establishes improvement for this request in each tested pair, even though the off completion time is unknown.
It does not remove the shared-cohort regressions above.

At 15/minute, request 4135, Chesham to Victoria, sets the disabled completed maximum.
It also completes with swaps on, but boards 144.67 seconds later and finishes 129.55 seconds later.
Its postboarding duration decreases slightly.
At 20/minute, request 4179, Watford to Elephant & Castle, sets the disabled completed maximum.
It completes in both arms and finishes 1,106.75 seconds earlier with swaps on.

Postboarding duration includes boarding dwell, travel, intermediate movement, and destination unloading.
It is not pure driving time.
Completed maxima omit pending and unfinished requests, so a cutoff can change which request determines the reported maximum.

### Limits and evidence

Every arm still has unfinished work and growing late-arrival backlog.
These results do not establish sustainable capacity, universal benefit, or policy adoption.
They cover two paired schedules, not every demand band, seed, fleet, or station layout.
Physical restore evidence has the same controller-history limits as the original study.
Functional runs overlap other diagnostic work, so their wall times do not measure CPU overhead or playback speed.
