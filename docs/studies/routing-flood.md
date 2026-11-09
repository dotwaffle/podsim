# Routing on the LondonFull flood

This study asks whether a congestion-aware route choice changes the tail of pods that wait to enter King's Cross (`940GZZLUKSX`) in the flood of the [station limits study](station-limits.md).
In this screen, no arm raises entries or completed orders.
`congestion` changes 44 to 54% of assigned routes and spreads the use of the approaches.
The number of slow pods bound for the station stays the same, the queue moves to other links, and entries fall from a range of 7.10 to 7.47 a minute to a range of 6.47 to 6.88.
`queue` installs no changed route, with or without a reroute at junctions and with 327 or 636 pods.
Those arms have the same state hash as their baseline at each simulated minute.
A junction reroute on the `congestion` signal does change routes, 10,450 to 11,883 times in a run.
It moves the queue as `congestion` does, and its entries stay below the baseline in each seed.

This is a three-seed screen.
It does not meet the adoption gates of [experimental-adoption.md](../design/experimental-adoption.md), which require seeds 1 through 10 and all supported demand bands.
It qualifies no setting.
No default, preset, contract, or simulation rule changes with this study.

## Method

The source is `aaedb7eb` plus scratch commits.
These commits do not land, and no test, lint, or race task ran on them.
They take the probe of the [station limits study](station-limits.md) and add the restore, the junction pass, and more counters.

The projects come from `scenario -preset london-full`, with 327 pods, and the same command with `-station-pods 2`, with 636 pods.
The flood and its schedule IDs are those of the station limits study, with one process for each seed:

```sh
compare -project base.json -pattern destination -focus 940GZZLUKSX \
  -loads 3s -duration 60m -arrivals-for 60m -seeds 1 \
  -redistribution-policies on -platoon-policies virtual \
  -pickup-reassignment on -workers 1 -format json
```

The environment selects the arm.

| Arm | Project | Setting |
| --- | --- | --- |
| Baseline | 327 pods | None. Free flow. |
| Congestion | 327 pods | `STUDY_ROUTING=congestion` |
| Queue | 327 pods | `STUDY_ROUTING=queue` |
| Junction | 327 pods | `STUDY_ROUTING=queue STUDY_JUNCTION=1` |
| Baseline 636 | 636 pods | None. Free flow. |
| Queue 636 | 636 pods | `STUDY_ROUTING=queue` |
| Junction congestion | 327 pods | `STUDY_ROUTING=congestion STUDY_JUNCTION=1` |

Queue is the better of the two policies on entries and completed orders in each seed, so queue is the arm with 636 pods.
Each cell gives seeds 1 / 2 / 3.
Rates and tails cover minutes 20 to 60, and the probe samples each second.

### Restored parts

A full revert of `87cc31b5` is not small: it touches 77 files.
The restore takes these parts from the removal:

- The route search cost, `routeLaneExtra`, with `extraCost` and `discharge`.
- `assignedRoute`, the congestion route, costs, and memo, with their clone, reset, and epoch code.
- `queueRoute` and `queueCost`, with the guards of 15 s, 5%, and 1.2 times the free-flow time.
- The costed route in `sendPickupMatching`, and the detour check in `legRoute`.
- The congestion route under the routing view.

The restore leaves out these parts:

- `predictive`, which is not an arm.
- The compare flag and the report column, because the environment selects the policy.
- The search counters of the removed policies.
- The route cost offset in `bank_routes.go`, because the counter `banks` is 0 in these projects, so `bankRoute` does not run.
- The two gates that turned pickup reassignment off under a costed policy.

The last item is a settings difference from the removed code.
The flood runs with reassignment on, so the gates would change two settings in one arm.
Reassignment stayed active: `congestion` has 22 / 18 / 36 swaps, and the baseline has 13 / 14 / 12.
Swap routes stay free flow, as they were.
The arm with the gates did not run.

### Junction arm

The junction pass took less than the limit of about 60 minutes to build.
Every 5 s, in fleet order, it visits each traveling pod that has no fault.
A pod that `divertStart` refuses, or that has a link, keeps its route.
For the others, when the next node where the route can change has more than one lane out, the pass calls `endpointRoute`, the search of the incident reroute.
That search keeps each granted lane and calls `assignedRoute`, so the signal and the guards are those of `queue`.
A route that differs from the current route replaces it.
This arm uses the queue signal, and it ran after the queue arm had shown no changed route at assignment.

The first junction runs showed no linked pods, because the pass skipped them before the count.
Three reruns with corrected counters have the same state hash as the first runs, and the tables use the reruns.

### Checks

The separation oracle ran at each tick on all seeds of the arms with a costed policy: `SafetyObservation.Check` found no failure in 108,000 ticks.
The smallest gap in each arm is 6.000 m, which is the certified depot pair.
The baselines ran without the oracle.

The baseline gives the rows of the station limits study: 378 / 370 / 386 completed, 7.12 / 7.10 / 7.47 entries a minute, and a tail mean of 521 / 442 / 360 m.
Baseline 636 also gives its row: 384 / 378 / 380 completed, 7.10 / 7.20 / 7.20 entries a minute, and a tail mean of 1,673 / 1,356 / 1,083 m.

The probe hashes the lane, berth, activity, distance, speed, and route length of each pod at each simulated minute.
For determinism, a run of `congestion` seed 1 that stopped at 15 minutes, with the oracle off, has the same 15 hashes as the full run.
The queue and junction arms repeat the hashes of the baseline runs in each seed.
That run and the three reruns are the only runs outside the grid of 6 arms and 3 seeds, apart from the later junction arm on the congestion signal.

## Results

| Measure | Baseline | Congestion | Queue | Junction | Baseline 636 | Queue 636 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Offered | 1,199 each | 1,199 each | 1,199 each | 1,199 each | 1,199 each | 1,199 each |
| Skipped | 430 / 425 / 416 | 439 / 442 / 437 | 430 / 425 / 416 | 430 / 425 / 416 | 149 / 139 / 119 | 149 / 139 / 119 |
| Accepted | 769 / 774 / 783 | 760 / 757 / 762 | 769 / 774 / 783 | 769 / 774 / 783 | 1,050 / 1,060 / 1,080 | 1,050 / 1,060 / 1,080 |
| Boarded | 569 / 574 / 584 | 560 / 559 / 563 | 569 / 574 / 584 | 569 / 574 / 584 | 850 / 860 / 880 | 850 / 860 / 880 |
| Completed | 378 / 370 / 386 | 352 / 349 / 360 | 378 / 370 / 386 | 378 / 370 / 386 | 384 / 378 / 380 | 384 / 378 / 380 |
| Entries a minute | 7.12 / 7.10 / 7.47 | 6.55 / 6.47 / 6.88 | 7.12 / 7.10 / 7.47 | 7.12 / 7.10 / 7.47 | 7.10 / 7.20 / 7.20 | 7.10 / 7.20 / 7.20 |
| Exits a minute | 6.70 / 6.50 / 6.67 | 6.12 / 6.03 / 6.28 | 6.70 / 6.50 / 6.67 | 6.70 / 6.50 / 6.67 | 6.55 / 6.80 / 6.58 | 6.55 / 6.80 / 6.58 |
| Entry headway p50, s | 2.7 / 3.3 / 2.4 | 2.4 / 2.2 / 2.2 | 2.7 / 3.3 / 2.4 | 2.7 / 3.3 / 2.4 | 9.3 / 5.7 / 3.7 | 9.3 / 5.7 / 3.7 |
| Linked entries, % | 91 / 91 / 93 | 94 / 95 / 96 | 91 / 91 / 93 | 91 / 91 / 93 | 88 / 89 / 92 | 88 / 89 / 92 |
| Wait mean, s | 641 / 645 / 650 | 650 / 659 / 666 | 641 / 645 / 650 | 641 / 645 / 650 | 279 / 276 / 285 | 279 / 276 / 285 |
| Wait p95, s | 1,815 / 1,893 / 1,790 | 1,875 / 1,911 / 1,904 | 1,815 / 1,893 / 1,790 | 1,815 / 1,893 / 1,790 | 1,107 / 1,035 / 1,043 | 1,107 / 1,035 / 1,043 |
| Journey mean, s | 1,329 / 1,351 / 1,350 | 1,320 / 1,351 / 1,332 | 1,329 / 1,351 / 1,350 | 1,329 / 1,351 / 1,350 | 1,193 / 1,239 / 1,176 | 1,193 / 1,239 / 1,176 |
| Journey p95, s | 2,515 / 2,626 / 2,580 | 2,503 / 2,666 / 2,603 | 2,515 / 2,626 / 2,580 | 2,515 / 2,626 / 2,580 | 2,485 / 2,583 / 2,581 | 2,485 / 2,583 / 2,581 |
| Tail mean, m | 521 / 442 / 360 | 613 / 690 / 531 | 521 / 442 / 360 | 521 / 442 / 360 | 1,673 / 1,356 / 1,083 | 1,673 / 1,356 / 1,083 |
| Tail p95, m | 1,029 / 1,150 / 762 | 926 / 1,023 / 812 | 1,029 / 1,150 / 762 | 1,029 / 1,150 / 762 | 3,078 / 2,936 / 3,065 | 3,078 / 2,936 / 3,065 |
| Tail mean, pods | 50 / 39 / 31 | 69 / 80 / 55 | 50 / 39 / 31 | 50 / 39 / 31 | 193 / 165 / 122 | 193 / 165 / 122 |
| Stopped, 1000 pod-seconds | 219 / 243 / 163 | 241 / 257 / 189 | 219 / 243 / 163 | 219 / 243 / 163 | 499 / 555 / 484 | 499 / 555 / 484 |
| Free berth with a queue, % | 99.5 / 100.0 / 99.8 | 99.7 / 99.9 / 99.8 | 99.5 / 100.0 / 99.8 | 99.5 / 100.0 / 99.8 | 61.3 / 67.1 / 59.4 | 61.3 / 67.1 / 59.4 |
| Passenger distance, km | 5,760 / 5,564 / 5,937 | 5,810 / 5,654 / 5,916 | 5,760 / 5,564 / 5,937 | 5,760 / 5,564 / 5,937 | 8,615 / 8,354 / 8,999 | 8,615 / 8,354 / 8,999 |
| Empty distance, km | 4,150 / 3,972 / 4,900 | 3,860 / 3,804 / 4,539 | 4,150 / 3,972 / 4,900 | 4,150 / 3,972 / 4,900 | 5,026 / 4,731 / 5,141 | 5,026 / 4,731 / 5,141 |
| Largest detour ratio | 1.00 / 1.00 / 1.00 | 1.59 / 2.89 / 1.54 | 1.00 / 1.00 / 1.00 | 1.00 / 1.00 / 1.00 | 1.00 / 1.00 / 1.00 | 1.00 / 1.00 / 1.00 |

`congestion` changes 599 / 531 / 667 of 1,204 / 1,197 / 1,228 assigned routes, and 219 / 179 / 254 of these are for pods bound for the station.
Entries with `congestion` fall by 0.57 / 0.63 / 0.59 a minute, and completed orders fall by 26 / 21 / 26.
This study did not measure why entries fall.
The largest detour ratio with `congestion` is above 1.5 in each seed.
These arms have no shared parties, so the shared detour gate does not apply, and the mean ratio is 1.04 / 1.04 / 1.03.

## Where the queue sits

Each approach is one `road-in` lane, and each `road-in` lane starts at the end of one network link.
The shares count pods that enter each `road-in` lane in the window.
The effective count is the inverse of the sum of the squared shares.

| Approach | Baseline, % | Congestion, % | Baseline 636, % |
| --- | ---: | ---: | ---: |
| `road-in-01`, link 056 | 1 / 0 / 1 | 17 / 16 / 19 | 2 / 1 / 1 |
| `road-in-02`, link 127 | 8 / 10 / 6 | 5 / 7 / 3 | 14 / 13 / 11 |
| `road-in-03`, link 193 | 28 / 27 / 26 | 17 / 18 / 19 | 20 / 25 / 21 |
| `road-in-04`, link 194 | 26 / 26 / 27 | 18 / 18 / 17 | 23 / 20 / 24 |
| `road-in-05`, link 199 | 29 / 28 / 30 | 18 / 19 / 18 | 26 / 27 / 27 |
| `road-in-06`, link 229 | 1 / 1 / 0 | 5 / 4 / 4 | 1 / 1 / 1 |
| `road-in-07`, link 273 | 7 / 8 / 10 | 21 / 18 / 20 | 15 / 14 / 16 |
| Effective count | 4.12 / 4.21 / 4.12 | 5.94 / 6.02 / 5.64 | 4.95 / 4.78 / 4.75 |
| Pods that enter | 284 / 283 / 298 | 262 / 261 / 275 | 286 / 288 / 283 |

The queue and junction arms equal the baseline in this table, and Queue 636 equals Baseline 636.

The next table counts slow pods, below 2 m/s.
A slow pod bound for the station counts at each second, at any distance, and whether or not it is in a chain.
An approach tail is the chain of the tail definition of the station limits study, for the pods that use one approach.

| Measure | Baseline | Congestion | Baseline 636 |
| --- | ---: | ---: | ---: |
| Slow pods bound for the station, mean | 82 / 87 / 58 | 79 / 90 / 61 | 211 / 233 / 201 |
| Within 250 m of the access lane | 18.9 / 19.0 / 18.3 | 30.9 / 33.3 / 29.1 | 23.9 / 23.0 / 20.9 |
| 250 m to 1 km | 61.2 / 59.5 / 38.7 | 47.8 / 55.6 / 31.5 | 91.3 / 99.6 / 92.2 |
| 1 to 3 km | 1.5 / 8.2 / 0.2 | 0.1 / 0.8 / 0.2 | 93.0 / 108.5 / 85.5 |
| Over 3 km | 0.2 / 0.2 / 0.3 | 0.4 / 0.5 / 0.2 | 2.4 / 1.7 / 2.2 |
| Longest approach tail, mean, m | 620 / 651 / 474 | 596 / 659 / 530 | 935 / 867 / 827 |
| Sum of approach tails, mean, m | 1,233 / 1,224 / 981 | 1,542 / 1,672 / 1,372 | 1,851 / 1,689 / 1,590 |
| Slow pods bound for `road-in-01` | 0.1 / 0.0 / 0.1 | 7.8 / 9.6 / 5.3 | 0.2 / 0.1 / 0.1 |
| Slow pods bound for `road-in-05` | 36.5 / 30.1 / 31.8 | 8.6 / 9.9 / 6.5 | 86.5 / 84.2 / 89.3 |
| Slow pods bound for `road-in-07` | 0.7 / 0.8 / 1.0 | 17.8 / 18.4 / 12.6 | 1.7 / 1.2 / 2.1 |
| Slow on `london-link-199-ab-2` | 31.8 / 26.9 / 28.4 | 5.4 / 6.5 / 3.5 | 35.3 / 37.8 / 37.5 |
| Slow on `london-link-273-ba-2` | 0.0 / 0.0 / 0.0 | 14.2 / 13.7 / 8.8 | 0.2 / 0.1 / 0.4 |
| Slow on `london-link-056-ab-2` | 0.1 / 0.0 / 0.1 | 7.7 / 9.5 / 5.2 | 0.2 / 0.1 / 0.1 |
| Slow pods bound elsewhere, mean | 17.8 / 21.6 / 16.2 | 25.1 / 21.5 / 22.4 | 14.8 / 13.6 / 16.7 |

So `congestion` moves the queue and does not shorten it.
The total of slow pods bound for the station stays within 4 pods of the baseline in each seed.
The queue leaves link 199 and sits on links 273 and 056, which the baseline hardly uses.
The longest single tail changes little, and the sum of the tails rises by 25 to 40%.
The mean tail in the definition of the station limits study rises to 531 to 690 m, and its p95 rises in one seed and falls in two.
More of the queue is within 250 m of the access lane.
Slow pods bound elsewhere rise in seeds 1 and 3, and do not change in seed 2.
This study did not measure how a queue on links 273 and 056 delays traffic that does not go to the station, because the flood has none.

## Why queue and junction change nothing

The counters give the answer for each search.
The queue arm itself ran before the search outcome counters existed.
The junction arm has the same assignments, so its counts include them, and the search outcomes come from that arm and Queue 636.

| Counter | Queue | Junction | Queue 636 |
| --- | ---: | ---: | ---: |
| Assignments | 1,234 / 1,235 / 1,274 | 1,234 / 1,235 / 1,274 | 1,831 / 1,827 / 1,875 |
| Pod visits of the pass | None | 200,897 / 200,359 / 200,618 | None |
| Visits with a link | None | 61,147 / 65,182 / 48,588 | None |
| Visits that `divertStart` refuses | None | 5,081 / 5,811 / 5,519 | None |
| Visits at a node with one way out | None | 75,133 / 71,762 / 81,648 | None |
| Junction searches | None | 59,536 / 57,604 / 64,863 | None |
| Searches with a delay on the free-flow route | 171 / 176 / 154 | 2,275 / 2,609 / 1,781 | 133 / 130 / 131 |
| The search returns the free-flow route | Not counted | 2,273 / 2,608 / 1,781 | 133 / 130 / 131 |
| Another route, saving under 15 s | Not counted | 2 / 1 / 0 | 0 / 0 / 0 |
| Another route, over 1.2 times free flow | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 |
| Route taken at assignment | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 |
| Route installed by the pass | None | 0 / 0 / 0 | None |
| Delay under 15 s | Not counted | 1,710 / 1,836 / 1,640 | 131 / 125 / 128 |
| Delay of 15 to 60 s | Not counted | 534 / 770 / 136 | 2 / 3 / 2 |
| Delay over 60 s | Not counted | 31 / 3 / 5 | 0 / 2 / 1 |
| Largest delay, s | Not counted | 77.0 / 62.9 / 62.0 | 39.4 / 66.3 / 117.3 |

The guards are not what stops `queue`.
Out of 6,665 delayed searches in the junction arm, 6,662 return the free-flow route, and the other 3 save at most 5.6 s.
So lower guards would change almost nothing with this signal.
Without the assignment searches of the queue arm, 2.5 to 4.2% of the junction searches see a delay at all.
The signal counts 3 s for each pod below 0.1 m/s with a wait reason on a lane, and it subtracts the travel time to that lane.
The slow counts of the last section use 2 m/s, so the two are different measures.
This study did not test a stronger signal.

For pods bound for the station, 30 to 38% of the visits have a link, 33 to 38% are at a node with one way out, 2 to 3% are refused, and 26 to 30% search.
A link stops a reroute by the rule of `divertStart`, because links depend on the routes.
These shares cover the whole run and each pod bound for the station, not only pods in the tail.

## Matched service

The comparison follows the [matched rules](../design/experimental-adoption.md#evidence-required) with the individual limits of the gates: 30 s or 5% for a wait, and 60 s or 5% for a journey.
The queue and junction arms, and Queue 636, have the same request ticks as their baselines, so each difference is 0.

| Congestion against the baseline | Seed 1 | Seed 2 | Seed 3 |
| --- | ---: | ---: | ---: |
| Accepted | 769 to 760 | 774 to 757 | 783 to 762 |
| Boarded | 569 to 560 | 574 to 559 | 584 to 563 |
| Completed | 378 to 352 | 370 to 349 | 386 to 360 |
| Jointly boarded | 547 | 545 | 536 |
| Matched wait, s | 459 to 502 | 490 to 499 | 530 to 549 |
| Over the wait limit | 131 | 80 | 97 |
| Better than the baseline by the same limit | 28 | 56 | 55 |
| Jointly completed | 327 | 328 | 342 |
| Matched journey, s | 1,225 to 1,261 | 1,250 to 1,290 | 1,250 to 1,292 |
| Over the journey limit | 112 | 113 | 136 |
| Better than the baseline by the same limit | 78 | 97 | 97 |
| Accepted in the baseline, skipped here | 139 | 124 | 159 |
| Boarded in the baseline only | 22 | 29 | 48 |
| Finished in the baseline only | 51 | 42 | 44 |

The last two rows are censored requests, and the matched means do not count them.
`congestion` fails the individual limits and the live queue gate in this screen, and it shows no benefit.

## Orders from other stations

All offers in this flood have King's Cross as the destination, in each arm and seed.
So no order to another station exists, and the harm to such orders cannot show here.
The only available reading is the origin of each order.

| Congestion against the baseline | Seed 1 | Seed 2 | Seed 3 |
| --- | ---: | ---: | ---: |
| Origin stations with an offer | 301 | 302 | 302 |
| Origins with an order over a limit | 149 | 126 | 148 |
| Origins with jointly boarded orders | 256 | 253 | 254 |
| Mean matched wait worse by over 30 s | 85 | 54 | 62 |
| Mean matched wait better by over 30 s | 20 | 35 | 43 |

No origin stands out: the largest count of orders over a limit at one origin is 3 or 4.
Each origin has about 4 offers, so these rows count small samples.
The queue and junction arms hurt no origin, because they change nothing.

## Earlier records

| Record | Then | Now | Differences | Result |
| --- | --- | --- | --- | --- |
| [Station limits](station-limits.md) baseline | 378 / 370 / 386 completed | The same, with the same entries, exits, and tail | `aaedb7eb` against `1932147`, one process for each seed, and the scratch restore, which is off in this arm. | Reproduces. |
| Station limits, 636 pods | 384 / 378 / 380 completed | The same, with the same entries, exits, and tail | As above | Reproduces. |
| [Queue routing screen](qualification.md#queue-routing-screen), `queue` | Each row equal to free flow, on rail-hub, scale100, London-192, and the London envelope | Each state hash equal to free flow | `d8393c1` against the tip. LondonFull with 327 and 636 pods against those projects. The destination flood against hub bursts and profile bands. 60 minutes of arrivals against 5 or 30. Redistribution on against off. Virtual platoons and reassignment on against neither. | Reproduces. |
| Same screen, `congestion` in the [envelope](../measurements/routing-queue-london-envelope.csv) | Lower limits in the Evening and Night bands, and waits up to 18.6 s above free flow | Entries and completed orders fall, and matched waits rise by 9 to 43 s | As above. Reassignment runs with this policy here, and the removed code refused that combination. | The direction reproduces, on a different measure. |
| Same screen, `congestion` on London-192 Early | At 20 orders a minute, 441.3 to 495.3 served, with 1.216 times the distance | Completed orders fall by 21 to 26, with 0.96 to 0.99 times the distance | As above. London-192 has 192 pods with no parking pods, ten seeds, and a mixed origin and destination profile. | Does not carry over. That gain has no counterpart in this flood. |
| [Congestion experiment](qualification.md#congestion-aware-routing-experiment) on scale100 | 59.00 to 57.67 served with no guards, and later 59.00 to 59.00 with a 1.3 s longer wait with guards | Completed orders fall by 21 to 26 | `b82b788` and `d8393c1` against the tip. The scale100 mesh and a 59-request hub burst against this flood. | The direction reproduces, and the fall here is larger. |
| [Predictive screen](../design/experimental-adoption.md#rejected-candidates) | 40,794 evaluations changed no request | Not run | `predictive` is not an arm. | Not tested. `queue` shows the same outcome here, but it is another policy. |
| [Model text](../model.md) | None of the policies has a service gain on record | No gain in this flood | One flood and three seeds here. `predictive` did not run. | Holds for `congestion` and `queue` in this flood. |

## Junction reroute on the congestion signal

This arm ran after the rest of the study, from a later scratch commit, because the arm on the queue signal installed no route.
It has the same schedule IDs as the baseline, and `SafetyObservation.Check` found no failure in 108,000 ticks on each seed, with a smallest gap of 6.000 m.
The pass refreshes the congestion costs before its searches, because a search under the routing view does not refresh them.
The policy has no saving or detour guard, so a pod can change its route at each pass.

| Measure | Baseline | Congestion | Junction congestion |
| --- | ---: | ---: | ---: |
| Offered | 1,199 each | 1,199 each | 1,199 each |
| Skipped | 430 / 425 / 416 | 439 / 442 / 437 | 436 / 427 / 435 |
| Accepted | 769 / 774 / 783 | 760 / 757 / 762 | 763 / 772 / 764 |
| Boarded | 569 / 574 / 584 | 560 / 559 / 563 | 563 / 572 / 565 |
| Completed | 378 / 370 / 386 | 352 / 349 / 360 | 357 / 364 / 361 |
| Entries a minute | 7.12 / 7.10 / 7.47 | 6.55 / 6.47 / 6.88 | 6.70 / 6.78 / 6.88 |
| Exits a minute | 6.70 / 6.50 / 6.67 | 6.12 / 6.03 / 6.28 | 6.25 / 6.22 / 6.20 |
| Entry headway p50, s | 2.7 / 3.3 / 2.4 | 2.4 / 2.2 / 2.2 | 2.2 / 2.2 / 2.2 |
| Linked entries, % | 91 / 91 / 93 | 94 / 95 / 96 | 94 / 96 / 93 |
| Wait mean, s | 641 / 645 / 650 | 650 / 659 / 666 | 647 / 648 / 665 |
| Wait p95, s | 1,815 / 1,893 / 1,790 | 1,875 / 1,911 / 1,904 | 1,881 / 1,890 / 1,887 |
| Journey mean, s | 1,329 / 1,351 / 1,350 | 1,320 / 1,351 / 1,332 | 1,316 / 1,351 / 1,365 |
| Journey p95, s | 2,515 / 2,626 / 2,580 | 2,503 / 2,666 / 2,603 | 2,484 / 2,579 / 2,615 |
| Tail mean, m | 521 / 442 / 360 | 613 / 690 / 531 | 539 / 582 / 455 |
| Tail p95, m | 1,029 / 1,150 / 762 | 926 / 1,023 / 812 | 745 / 812 / 678 |
| Tail mean, pods | 50 / 39 / 31 | 69 / 80 / 55 | 70 / 74 / 51 |
| Longest approach tail, mean, m | 620 / 651 / 474 | 596 / 659 / 530 | 545 / 625 / 499 |
| Sum of approach tails, mean, m | 1,233 / 1,224 / 981 | 1,542 / 1,672 / 1,372 | 1,546 / 1,702 / 1,362 |
| Slow pods bound for the station, mean | 82 / 87 / 58 | 79 / 90 / 61 | 77 / 85 / 61 |
| Within 250 m of the access lane | 18.9 / 19.0 / 18.3 | 30.9 / 33.3 / 29.1 | 33.4 / 33.9 / 30.8 |
| 250 m to 1 km | 61.2 / 59.5 / 38.7 | 47.8 / 55.6 / 31.5 | 43.5 / 50.1 / 30.0 |
| Slow pods bound elsewhere, mean | 17.8 / 21.6 / 16.2 | 25.1 / 21.5 / 22.4 | 22.4 / 19.6 / 19.9 |
| Slow on `london-link-199-ab-2` | 31.8 / 26.9 / 28.4 | 5.4 / 6.5 / 3.5 | 7.7 / 7.4 / 3.7 |
| Slow on `london-link-273-ba-2` | 0.0 / 0.0 / 0.0 | 14.2 / 13.7 / 8.8 | 12.7 / 14.1 / 10.0 |
| Slow on `london-link-056-ab-2` | 0.1 / 0.0 / 0.1 | 7.7 / 9.5 / 5.2 | 9.2 / 9.1 / 5.7 |
| Effective count of approaches | 4.12 / 4.21 / 4.12 | 5.94 / 6.02 / 5.64 | 5.94 / 6.03 / 5.55 |
| Stopped, 1000 pod-seconds | 219 / 243 / 163 | 241 / 257 / 189 | 225 / 232 / 183 |
| Passenger distance, km | 5,760 / 5,564 / 5,937 | 5,810 / 5,654 / 5,916 | 5,853 / 5,762 / 5,934 |
| Empty distance, km | 4,150 / 3,972 / 4,900 | 3,860 / 3,804 / 4,539 | 4,018 / 4,016 / 4,668 |
| Swaps | 13 / 14 / 12 | 22 / 18 / 36 | 30 / 33 / 24 |
| Largest detour ratio | 1.00 / 1.00 / 1.00 | 1.59 / 2.89 / 1.54 | 1.58 / 1.46 / 1.54 |

| Pass counter | Seed 1 | Seed 2 | Seed 3 |
| --- | ---: | ---: | ---: |
| Pod visits | 200,993 | 200,362 | 200,459 |
| Visits with a link | 58,432 | 59,612 | 48,107 |
| Visits that `divertStart` refuses | 6,276 | 5,745 | 5,668 |
| Visits at a node with one way out | 75,662 | 74,589 | 80,960 |
| Junction searches | 60,623 | 60,416 | 65,724 |
| Routes installed | 10,726 | 10,450 | 11,883 |
| Routes installed for pods bound for the station | 9,092 | 8,975 | 9,940 |
| Assignments | 1,205 | 1,231 | 1,226 |
| Assignments that differ from free flow | 549 | 560 | 567 |

This arm is the test of a reroute in motion, because 17 to 18% of the junction searches install a new route.
Against the baseline, it moves the queue from link 199 to links 273 and 056, as `congestion` does, and the count of slow pods bound for the station stays within 5 pods.
Entries fall by 0.42 / 0.32 / 0.59 a minute, and completed orders fall by 21 / 6 / 25.
The tail is not clearly shorter: the mean rises in each seed, the p95 falls in each seed, and the sum of the approach tails rises by 25 to 39%.

Against `congestion`, entries are 0.15 / 0.31 / 0.00 a minute higher and completed orders are 5 / 15 / 1 higher, and the tail mean is 74 / 108 / 76 m shorter.
Three seeds do not separate these differences from run variation, and the approach shares and the links with the queue are about the same.
This study did not measure why entries fall, or how many route changes reverse an earlier change.

The request records of the baseline and `congestion` runs were deleted with the scratch directory before this arm ran, and no baseline ran again.
So this arm has no matched comparison on requests and no table by origin, and the means above include different sets of requests.
It has no determinism run.

## Recommendation

This screen gives no reason to bring back `congestion` or `queue` for the flood tail.
`queue` does nothing here, and `congestion` moves the queue and costs entries.
The [station limits study](station-limits.md) puts the limit in the junction zones at the station, and each approach ends at the same access lane.
This screen is consistent with that account, because a wider spread of approaches gave no more entries, but it did not measure the cause of the fall.
The change that the station study gives for more entries, a second bank, remains the candidate for that purpose.

A junction reroute gives no reason either.
On the queue signal it installs no route, and on the congestion signal it moves the queue and keeps entries below the baseline in each seed.
This covers one unguarded reroute on one signal, so it does not cover a reroute with a limit on route changes or another signal.

## Proposed arms

These arms did not run.

- `congestion` with 636 pods against the baseline with 636 pods, because Queue 636 changed no route.
- `congestion` with the removed reassignment gates, to show whether reassignment changes the result.
- A flood with background orders to other stations, to read harm to those orders directly.
- The Waterloo flood, with the same baseline and `congestion`.

## Not verified

- The screen has three seeds, one flood pattern, and one queue limit, with no demand band, mixed arrivals, or rail-hub arm.
- The screen has no 120-minute run, so no sustained rate.
- The junction arm on the congestion signal has no matched comparison, no table by origin, and no determinism run.
- Orders to other stations do not exist in this flood, so the origin table is the only reading of harm.
- Delay to traffic on links 273 and 056 that does not go to the station was not measured.
- The cause of the fall in entries with `congestion` was not measured.
- The cause of the largest detour ratios was not traced to a route.
- The empty and occupied split of changed routes has no result: the counter for occupied pods read 0 while rider detours rose, so the study does not use it.
- The restored code has no tests beyond this study, and restore and stream checks did not run on it.
- The oracle covers separation only, and the adoption gates also cover restore, accounting, and cost.
- Waterloo did not run.
