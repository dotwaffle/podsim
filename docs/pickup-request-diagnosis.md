# Pickup swaps: matched request diagnosis

The two higher completed-journey maxima in the sustained swap study come from requests that remain unfinished with swaps off.
Those requests already have longer elapsed journeys at the cutoff than their enabled completion times.
The larger completed maximum therefore does not establish harm to those same requests.
Other requests do become slower: about 15% and 8% of the shared completed cohorts worsen in these two cases.
The experimental policy remains disabled by default.

## Method and validation

The study repeats seed 2 at 15 and 20 requests per simulated minute, with swaps off and on.
It uses the same mirrored LondonFull geometry, 287 pods, AM peak demand, six-hour arrival window, and seven-hour cap.
Sharing, redistribution, and station buffers remain off.
Virtual platoons have a four-pod limit and routing is free-flow.
Only test-helper exports change.
They add final request timings and pending requests without changing the simulation.

All four aggregate result records match the [sustained swap study](pickup-swap-sustained.md) exactly.
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

## Shared cohorts

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

## Requests that set the maxima

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

## Limits and evidence

Every arm still has unfinished work and growing late-arrival backlog.
These results do not establish sustainable capacity, universal benefit, or policy adoption.
They cover two paired schedules, not every demand band, seed, fleet, or station layout.
Physical restore evidence has the same controller-history limits as the original study.
Functional runs overlap other diagnostic work, so their wall times do not measure CPU overhead or playback speed.

The raw measurement data is in git history.
