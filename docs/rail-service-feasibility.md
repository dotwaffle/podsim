# Rail service feasibility and station banks

Rail Hub serves the selected light workloads within their train deadlines.
Its heavier workload completes every accepted journey but misses most connections.
LondonFull serves every selected connection with longer transfer lead times.
The tested two-bank station layout fails physical separation checks under heavy demand and is rejected.
No policy, layout, or default changed.

## Fixed study conditions

The observer binary came from `ed2ba64`, before daily-demand and place-search changes.
The [measurement record](measurements/rail-service-feasibility.json) retains its hash, fixture hashes, schedule identities, and receipt hashes.
Each arm retained a three-hour observation cap and an exclusive offer-window end at tick 216,001.
Offers through simulated second 3,600 were included.
The run stopped after accepted journeys completed and issued train deadlines resolved, within the original cap.

Each workload used six inbound trains and six outbound trains, with seeds 1 and 2.
Inbound offers arrived at seconds 330, 930, 1,530, 2,130, 2,730, and 3,330.
Outbound windows started at second 360, lasted 240 seconds, and repeated every 600 seconds.
Origins and destinations used the fixture's equal weights.
Each event contained 1, 6, or 24 passengers, giving 12, 72, or 288 total offers.

Rail Hub retained 30 pods and no platoons.
LondonFull retained 287 pods and virtual platoons with a limit of four.
Both retained a 200-order queue, one party per pod, free-flow routing, 14 m/s lane limits, and 12 m clearance.
Redistribution, buffers, pickup reassignment, and rail forecasting were off.
Two single-core workers ran functional arms concurrently.
Their elapsed times do not support CPU comparisons.

## Connections with longer lead times

Rail Hub trains departed 900 seconds after each request window ended.
LondonFull trains departed 3,600 seconds after each window ended.
Both used a 60-second transfer after alighting.
Individual offers therefore had 900-1,140 seconds at Rail Hub and 3,600-3,840 seconds at LondonFull before train departure.
Single-passenger events released at the window start and received the longest lead.

The route-plus-transfer lower bound ranged from 234.08 to 551.00 seconds at Rail Hub and 397.72 to 1,572.06 seconds at LondonFull.
It used the shortest berth-to-berth route at lane limits, plus transfer time.
It omitted pickup, acceleration, dwell, contention, and queues.
These optimistic bounds establish geographic plausibility.
Actual alighting receipts determine connection success.

| Network | Passengers/event | Seed | Completed / offered | Skipped | Made / outbound | Missed |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Rail Hub | 1 | 1 | 12 / 12 | 0 | 6 / 6 | 0 |
| Rail Hub | 1 | 2 | 12 / 12 | 0 | 6 / 6 | 0 |
| Rail Hub | 6 | 1 | 72 / 72 | 0 | 36 / 36 | 0 |
| Rail Hub | 6 | 2 | 72 / 72 | 0 | 36 / 36 | 0 |
| Rail Hub | 24 | 1 | 288 / 288 | 0 | 53 / 144 | 91 |
| Rail Hub | 24 | 2 | 288 / 288 | 0 | 54 / 144 | 90 |
| LondonFull | 1 | 1 | 12 / 12 | 0 | 6 / 6 | 0 |
| LondonFull | 1 | 2 | 12 / 12 | 0 | 6 / 6 | 0 |
| LondonFull | 6 | 1 | 72 / 72 | 0 | 36 / 36 | 0 |
| LondonFull | 6 | 2 | 72 / 72 | 0 | 36 / 36 | 0 |
| LondonFull | 24 | 1 | 288 / 288 | 0 | 144 / 144 | 0 |
| LondonFull | 24 | 2 | 288 / 288 | 0 | 144 / 144 | 0 |

All these arms ended with zero remaining journeys, unserved connections, and unresolved connections.
Missed passengers completed their pod journeys normally.
The smallest readiness margin for LondonFull's 24-passenger events was 1,257.77 seconds in seed 1 and 1,067.98 seconds in seed 2.
These results cover the selected schedules and lead times.
They do not establish an all-day capacity or a minimum feasible train lead.

## Pickup and journey times

Pickup wait runs from request to boarding.
Journey time runs from request to completed alighting, including pickup wait.
The following values include all 288 passengers in each arm.

| Network | Seed | Pickup mean / p95, seconds | Journey mean / p95, seconds |
| --- | ---: | ---: | ---: |
| Rail Hub | 1 | 616.15 / 1,180.12 | 953.76 / 1,603.97 |
| Rail Hub | 2 | 617.83 / 1,099.83 | 951.34 / 1,502.07 |
| LondonFull | 1 | 160.36 / 422.62 | 821.61 / 1,947.35 |
| LondonFull | 2 | 144.85 / 416.17 | 759.80 / 1,880.28 |

Rail Hub's longest late readiness was 1,036.17 seconds in seed 1 and 948.63 seconds in seed 2.
Its 6-passenger events had pickup means of 8.34 and 20.44 seconds, and made every connection.
The heavier workload adds substantial pickup waiting despite spare sampled berth capacity.

Station occupancy and holds were sampled once per simulated second through each arm's own end time.
Means divide the accumulated counts by the recorded sample count.
They are not exact event integrals and do not use a common-duration comparison window.

| Network | Seed | Mean occupied / free berths | Mean approaching pods | Mean stopped entrance / exit pods | Departure junction holds, sampled pod-seconds |
| --- | ---: | ---: | ---: | ---: | ---: |
| Rail Hub | 1 | 1.03 / 4.54 | 6.26 | 0.33 / 0.09 | 338 |
| Rail Hub | 2 | 0.80 / 4.77 | 6.36 | 0.38 / 0.21 | 904 |
| LondonFull | 1 | 2.30 / 1.34 | 4.71 | 0.24 / 0.01 | 7 |
| LondonFull | 2 | 2.36 / 1.27 | 4.44 | 0.26 / 0.02 | 6 |

Free berths exclude occupied berths and empty berths reserved by incoming pods.
Rail Hub has six focus berths.
LondonFull's Paddington has four.
Hold totals include only lanes tagged to the focus station, by role and wait reason.
LondonFull also recorded 1,819 and 1,440 approach-lane junction hold samples.
These observations suggest access and fleet circulation matter, but do not identify a single limiting mechanism.

## Isolated outbound checks

Five separate origins per network each offered one outbound journey, with no inbound traffic and the full existing fleet.
Every journey completed and made its train, with no skip or unresolved outcome.
Only these five selected origins were tested, not every LondonFull station.
The report retains each arm and receipt hash.

## Two-bank station candidate

The candidate moved Rail Hub's six berths into two opposing groups of three.
Each group had separate arrival and departure ladder lanes.
Both still shared the station's original entry and exit nodes.
Route lengths changed, so this was not a fully independent-access bank design or a pure contention-policy comparison.
Fleet, offers, seeds, queue, speed, clearance, and observation cap stayed fixed.

A one-passenger-per-event pilot completed all 12 journeys and made all six trains.
The heavy screen used 120 passengers per event, giving 1,440 planned offers and 720 outbound connections.
Its train lead remained the earlier 300 seconds after each window ended.
Some origins had optimistic route-plus-transfer bounds longer than the shortest offered train lead.
This heavy screen therefore measures overload and layout safety, not the longer-lead feasibility result above.

| Arm | Seed | Completed | Skipped | Made / missed / unserved / unresolved |
| --- | ---: | ---: | ---: | ---: |
| Baseline | 1 | 363 | 1,077 | 0 / 151 / 569 / 0 |
| Baseline | 2 | 366 | 1,074 | 0 / 151 / 569 / 0 |

Both baselines completed every accepted journey safely, but rejected most offers.
The candidate stopped at the first separation failure in each seed.

| Seed | Failure tick | Pods | Measured gap, meters | Offered = accepted + skipped | Future offers |
| ---: | ---: | --- | ---: | --- | ---: |
| 1 | 88,160 | 008 and 017 | 11.71552 | 480 = 239 + 241 | 960 |
| 2 | 115,238 | 027 and 029 | 11.85891 | 720 = 268 + 452 | 720 |

Required clearance was 12 meters.
The failing layouts were rejected without parameter tuning or adoption.
The original helper stopped before it wrote full passenger censuses.
A separate evidence-capture helper reran only those failed arms and reproduced their exact physical failure states.
It retained all planned identities, accepted receipts, rejected indices, and offers not yet issued.
The capture changes do not establish that the candidate is safe.

Candidate connection counts at failure were 0 made, 91 missed, 141 unserved, and 8 unresolved in seed 1.
Seed 2 retained 0 made, 99 missed, 253 unserved, and 8 unresolved.
Those counts cover only outbound offers already issued.
Future offers remain separate, and unfinished accepted journeys remain censored.
Do not compare these truncated arms with completed baselines as a capacity improvement.

## Safety, restoration, and evidence

The observer checked separation and lane speed every tick, and simulation contracts once per simulated second.
Every 300 simulated seconds it exported and physically restored state, checking saved discrete fields and positional tolerances.
It then stepped the restored simulation for 60 ticks and checked safety, contracts, and completion receipts.
Two ordinary clones also produced identical continuation states and receipts.
The observer-parity pilot matched the result without observation hooks.

Physical restore resets moving-pod velocity and can reconstruct berth reservations.
The report records those reservation changes and the largest saved-position difference.
It does not claim that restored future trajectories match uninterrupted runs.
Sampled failure observations can end before the failing tick, even though the captured physical state includes it.

The measurement record contains 25 successful arms and two rejected arms.
Analysis verified fixture and binary hashes, passenger conservation, receipt bindings, connection scoring, and matching baseline/candidate offered populations.
Reproduction needs the cached fixtures and helpers.
The repository summary alone is not a standalone study runner.
No live geometry, controller defaults, byte caps, speed limits, or clearance limits changed.

## Follow-up diagnosis

The [busy-station diagnosis](station-service-diagnosis.md) attributes 77-79% of sampled heavy pickup wait to requests without an eligible assigned pod.
Both captured bank failures came from an unconnected road/bank near-crossing.
A coordinate correction passes the selected two-seed safety screen but does not improve service.
Independent bank contracts remain subject to user approval.
