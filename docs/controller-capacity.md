# Expanded controller and LondonFull capacity screen

Pickup reassignment alone improves throughput in the selected overloaded Acton workload.
Buffers reduce throughput in that workload, including when reassignment is enabled.
Every active-policy comparison retains individual request regressions.
The longer LondonFull AM baselines do not drain by seven hours at 13, 14, or 15 requests per minute.
This screen supports no default adoption or capacity-limit increase.

## Source, coverage, and checks

All jobs use frozen source `a54281e` and the existing LondonFull project.
Four dense pilots compare observer and production results exactly.
All 38 primary jobs pass their safety, speed, and unique-order checks once per simulated second.
Each primary job also passes two physical restores with unchanged bindings, poses, admission ages, and saved platoon records.
Each restored copy runs 60 seconds of dense checks with new buffer and pickup-reassignment admissions disabled.
No restore demotes, requeues, or drops an order.

The controller screen covers Full Morning10/min, AM12/min, and PM14/min with seed 3.
Each offers three hours of demand and permits a four-hour cap.
It compares buffers off/on and pickup reassignment off/on at queue limits of 200 and 1,000,000.
These 24 arms retain free-flow routing, virtual platoons with four pods, sharing off, and positioning off.

Eight additional factorial arms use an Acton mixed-arrival fixture with seed 3, two hours of arrivals, and a three-hour cap.
Every minute offers four requests for each direction: Turnham Green to Acton, Ealing Common to Acton, Acton to Turnham Green, and Acton to Hammersmith.
The schedule permutes the four pair groups deterministically each minute.
The 16-request-per-minute fixture is a local overload stress test, not a London demand band.
Its job pattern is `acton-mixed`, while its internal result pattern remains `balanced` for demand weights.

Six separate capacity baselines offer six hours of Full AM demand at 13, 14, and 15 requests per minute, with seeds 3 and 4.
They permit a seven-hour cap and retain both controllers off.
Their longer observation window must remain separate from the shorter controller comparisons.
All owned jobs stopped successfully, and the scratch binary was removed.
Concurrent functional runs provide no CPU comparison.

## Full demand-band controller comparisons

None of the 24 Full controller arms skips a request at either queue limit.
For each controller setting, queue 200 and queue 1,000,000 produce exact result and check parity.
The queue-limit comparison therefore provides no extra throughput benefit in these selected cells.
All AM and PM controller arms drain within four hours.
Each Morning arm retains two or three unfinished requests at the cap.

The following changes use requests that board in both arms at queue 1,000,000.
The baseline has both controllers off.
Queue 200 repeats these effects exactly for the selected Full cells.

| Band and rate/min | Candidate | Matched mean wait change, seconds | Largest added wait, seconds | Proposed wait-limit exceedances |
| --- | --- | ---: | ---: | ---: |
| Morning10 | Buffers | -6.51 | 1,541.08 | 393 |
| Morning10 | Reassignment | -4.74 | 1,537.97 | 321 |
| Morning10 | Both | -8.66 | 1,163.48 | 396 |
| AM12 | Buffers | -19.26 | 1,000.05 | 323 |
| AM12 | Reassignment | -2.66 | 1,007.22 | 259 |
| AM12 | Both | -21.07 | 1,143.35 | 348 |
| PM14 | Buffers | -12.74 | 1,446.97 | 881 |
| PM14 | Reassignment | -32.92 | 1,447.13 | 755 |
| PM14 | Both | -43.82 | 1,584.63 | 750 |

These mean improvements coexist with substantial individual regressions.
The user approved the [individual limits](experimental-adoption.md) on October 1.
Morning's unfinished requests also prevent interpreting completed-only journey statistics as full-cohort outcomes.
The metadata retains the exact status transitions and all jointly accepted unfinished requests.

The Full factorials record no fixed entry-platoon member ticks.
This counter measures linked station-entry platoons, not ordinary buffer admissions.
Zero linked-member ticks do not imply that enabling ordinary buffers had no effect.
The Acton buffer arms record fixed entry-platoon activity, as retained in the arm CSV.
Neither observation qualifies the controllers across all stations or demand bands.

## Acton overload and queue limits

All arms offer the same 1,920 requests and reach the three-hour cap.
The table retains completed, unfinished, and skipped requests separately.

| Queue limit | Controllers | Completed | Unfinished | Skipped |
| ---: | --- | ---: | ---: | ---: |
| 200 | Off | 989 | 69 | 862 |
| 200 | Buffers | 880 | 65 | 975 |
| 200 | Reassignment | 1,072 | 52 | 796 |
| 200 | Both | 857 | 69 | 994 |
| 1,000,000 | Off | 1,029 | 891 | 0 |
| 1,000,000 | Buffers | 907 | 1,013 | 0 |
| 1,000,000 | Reassignment | 1,099 | 821 | 0 |
| 1,000,000 | Both | 893 | 1,027 | 0 |

At queue 200, reassignment alone completes 83 additional requests and accepts 66 additional requests.
Buffers alone complete 109 fewer requests and accept 113 fewer requests.
The combined policy completes 132 fewer requests and accepts 132 fewer requests.
The larger queue removes rejection but retains hundreds of unfinished orders in every arm.
Its late backlog grows by 9.48 to 10.67 requests per minute, depending on the controllers.

With the larger queue, reassignment alone decreases matched boarded-request wait by 469.10 seconds.
Its largest matched wait increase remains 190.98 seconds, with 78 individual-limit exceedances.
Buffers alone increase matched mean wait by 799.55 seconds, and both controllers increase it by 896.31 seconds.
Those wait comparisons exclude requests still awaiting boarding in either arm.
The metadata retains their unfinished status and the separate jointly completed journey cohort.

Increasing the queue limit adds demand to the system and can delay requests accepted by both queue settings.
The largest matched wait increases across queue settings range from 4,018.57 to 5,163.00 seconds.
An all-request average cannot isolate a controller effect when accepted cohorts differ.
Schedule-index joins preserve one-sided acceptance and prevent comparing different orders that happen to share an ID.

## Longer AM capacity baselines

Every capacity arm accepts all offered requests without skips and reaches the seven-hour cap.
Late backlog growth measures the change between three and six hours, divided by 180 minutes.

| Rate/min | Seed | Completed | Aboard | Pending | Late backlog growth, requests/min |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 13 | 3 | 4,694 | 1 | 0 | +0.056 |
| 13 | 4 | 4,694 | 1 | 0 | -0.039 |
| 14 | 3 | 5,000 | 36 | 6 | +2.122 |
| 14 | 4 | 5,028 | 14 | 0 | +1.200 |
| 15 | 3 | 5,022 | 146 | 231 | +3.244 |
| 15 | 4 | 4,973 | 145 | 281 | +3.267 |

At 13/min, each seed misses the complete-drain condition by one passenger journey still aboard.
Those requests have already waited 2,286.70 and 2,633.58 seconds before boarding.
A nearly stable late backlog does not imply that every passenger finishes within the cap.
The 14/min and 15/min arms retain more unfinished requests and positive late backlog growth.
This boundary screen neither raises a qualified rate nor replaces the full demand-band and seed matrix.

## Pickup supply at the AM boundary

A diagnostic replay of the six baselines above (AM13 to AM15, seeds 3 and 4) freezes source `e285e69` and matches the earlier `a54281e` results exactly.
It samples fleet activity, pending requests, and berth ownership once per simulated second.
Controllers, sharing, and positioning are off.
Routing is free-flow, and virtual platoons allow four pods.

| Rate/min | Seed | Idle available | Pickup travel | Passenger travel | Pending | Unassigned |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 13 | 3 | 35.81 | 87.14 | 158.30 | 90.73 | 3.59 |
| 13 | 4 | 38.97 | 85.59 | 156.76 | 88.83 | 3.24 |
| 14 | 3 | 0.02 | 141.32 | 144.56 | 343.97 | 202.64 |
| 14 | 4 | 4.02 | 127.97 | 153.11 | 188.89 | 60.92 |
| 15 | 3 | 0.00 | 143.39 | 142.56 | 641.04 | 497.65 |
| 15 | 4 | 0.00 | 144.64 | 141.28 | 705.27 | 560.63 |

The table gives mean pod and request counts from three hours to the end of arrivals.
AM14 and AM15 exhaust the 287-pod fleet while pods keep moving: stopped traveling pods stay between 0.278 and 0.470, and no sampled berth reports a blocked loaded departure.
Pending requests mostly wait for an available pod, so pickup supply and empty-trip use are the next capacity targets.
This does not prove an optimal dispatch policy or that more pods would solve the workload.
The AM13 seven-hour cutoff leaves one long passenger trip in each seed: request 4593 (pickup wait 2,286.70 s) and request 4675 (2,633.58 s), both from Chalfont & Latimer.
Both keep moving at the cutoff, and sampled finishing holds explain little of their waits.
An eight-hour allowance completes all 4,695 requests in each seed, ending at 26,688 and 26,952 simulated seconds.
These two misses do not establish sustained overload at AM13.
The strict finishing-wait rule reduces completions in all four AM14 and AM15 cells.
The Acton buffer regression needs separate grant and release histories because it has substantial traffic queues.

## Evidence and limits

The raw measurement data is in git history.
The [pickup supply diagnosis](#pickup-supply-at-the-am-boundary) identifies exhausted pickup supply at AM14/AM15 and confirms extended AM13 recovery.
The invalid initial `early` band inputs remain separate from the corrected Full Morning runs.

The result field `wait_average_seconds` combines realized waits with elapsed pending ages and is a lower bound on eventual pickup wait.
Pair fields distinguish that aggregate from actual matched boarded-request wait changes.
Stopped and linked request diagnostics use one-second samples, not complete resource-grant histories.
These selected one-seed controller cells do not establish broad adoption or diagnose every remaining individual tail.
