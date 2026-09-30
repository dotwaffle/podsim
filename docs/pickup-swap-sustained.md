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

[Per-arm results](measurements/pickup-swap-sustained.csv) retain the comparison fields and controller counters.
[Study metadata](measurements/pickup-swap-sustained.json) records source identity, hashes, paired controls, and validation limits.
Raw results and scripts are in `~/.cache/agents/podsim/pickup-swap-sustained-20260930/`.

The later [matched-request diagnosis](pickup-request-diagnosis.md) explains the two larger completed maxima and retains same-request regressions and censored cohorts.
