# LondonFull pickup supply at the AM boundary

The selected AM14 and AM15 workloads exhaust the available fleet while most pods keep moving.
Their growing backlogs do not coincide with substantial junction or station-departure waits.
The AM13 seven-hour cutoff leaves one long passenger trip in each seed.
Extended recovery completes all requests without changing the arrivals or policies.
Those two cutoff misses do not establish sustained overload at AM13.

## Replay and checks

The diagnostic freezes production source `e285e69` and the existing 287-pod LondonFull project.
It repeats the six [capacity baselines](controller-capacity.md#longer-am-capacity-baselines): AM13, AM14, and AM15, each with seeds 3 and 4.
Each offers six hours of demand and allows a seven-hour recovery cutoff.
Buffers, pickup reassignment, sharing, and positioning remain off.
Routing uses free-flow costs and virtual platoons allow four pods.
The queue limit is one million, and every offered request enters each arm.

All six arms match the earlier `a54281e` results, schedules, accepted requests, timing records, pending records, station reports, and check counts exactly.
The observer samples fleet activity, pending-request state, and berth ownership once per simulated second.
It also records selected request histories when their observed pod, lane, phase, wait, or dispatch reason changes.
These histories are sampled observations, not exact transition events.

All primary arms pass safety, speed, and unique-order checks once per simulated second.
Physical restores at three and six hours retain poses, bindings, admission ages, and saved platoon records.
Each restored copy passes a separate 60-second continuation with dense checks.
Two short Acton pilots compare diagnostic and production results exactly and check every tick.
Observer tests check state purity and berth census totals.
The same diagnostic also repeats two Acton buffer arms with exact historical parity.
Concurrent functional runs do not provide CPU comparisons.

## Fleet use during the later arrival window

The table reports mean pod counts from samples at three hours through the second before six hours.
Idle pods exclude pods already bound to requests.
The stopped count includes traveling pods below 0.01 meters per second, with or without a wait reason.
Small sampled stop counts do not exclude slow movement, brief unsampled stops, or speed limits.

| Rate/min | Seed | Idle available | Pickup travel phase | Passenger travel phase | Stopped traveling pods | Pending requests | Unassigned requests |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 13 | 3 | 35.81 | 87.14 | 158.30 | 0.278 | 90.73 | 3.59 |
| 13 | 4 | 38.97 | 85.59 | 156.76 | 0.309 | 88.83 | 3.24 |
| 14 | 3 | 0.02 | 141.32 | 144.56 | 0.431 | 343.97 | 202.64 |
| 14 | 4 | 4.02 | 127.97 | 153.11 | 0.444 | 188.89 | 60.92 |
| 15 | 3 | 0.00 | 143.39 | 142.56 | 0.470 | 641.04 | 497.65 |
| 15 | 4 | 0.00 | 144.64 | 141.28 | 0.459 | 705.27 | 560.63 |

Pickup and passenger columns count traveling activity, including its small stopped fraction.
Other empty moves, boarding, and unloading account for the remaining fleet.
No sampled berth state reports a blocked loaded departure during these AM windows.

At AM14 seed 3, nearly every pod carries a passenger or travels to a pickup.
Most pending-request samples have no assigned pod and report waiting for an available pod.
At AM15, both seeds retain almost no idle availability.
The pickup fleet grows without a corresponding increase in passenger travel.
The earlier results also record more empty distance and positive late backlog growth.
Together, these observations make pickup supply and empty-trip use the next capacity targets.
They do not prove an optimal dispatch policy or show that adding pods would solve the workload.

Waterloo leads the pending-request census at AM14 seed 3.
Other busy origins include King's Cross St. Pancras, Victoria, Paddington, London Bridge, and Stratford.
A station can accumulate requests while its berths remain free because the assigned or available pods are elsewhere.
These records do not justify new berth banks or weaker reservations at those stations.

## AM13 recovery tails

| Seed | Request | Pickup origin | Sampled moving pickup seconds | Sampled finishing-pod hold seconds | Sampled stopped pickup seconds | Remaining passenger free-flow travel near cutoff |
| ---: | ---: | --- | ---: | ---: | ---: | ---: |
| 3 | 4593 | Chalfont & Latimer | 2,257 | 30 | 0 | About 25 minutes |
| 4 | 4675 | Chalfont & Latimer | 2,604 | 29 | 0 | About 30 minutes |

Both requests board after long pickups and keep moving with passengers at the seven-hour cutoff.
The remaining travel estimates sum untraveled route lengths divided by lane speed limits.
They exclude future traffic delays, acceleration, braking, and unloading.
The last recorded lane transition can precede the cutoff, so the estimates are approximate.

The existing timing records give pickup waits of 2,286.70 and 2,633.58 seconds.
The sampled finishing holds account for only a small part of those waits.
No selected pickup or passenger sample records a traffic wait.
An eight-hour recovery allowance confirms completion of all 4,695 requests in each seed.
Seed 3 ends at 26,688 simulated seconds, and seed 4 ends at 26,952 seconds.
They require 5,088 and 5,352 seconds of recovery after arrivals stop.
The accepted requests and schedule remain identical to the seven-hour runs.
Only the final request's timing record changes in each arm.
Physical restores at three, six, and seven hours pass, including dense continuation checks.
Their long moving pickups remain a service problem despite eventual completion.

## Evidence and remaining work

[Fleet census](measurements/london-full-fleet-census.csv) retains each replay's early, late, and recovery windows, including the separate Acton stress fixture.
[Metadata](measurements/london-full-capacity-diagnosis.json) retains exact replay checks, source and binary identities, selected tails, and aggregate census rows.
[Extended recovery](measurements/london-full-recovery.json) retains the final timings and checks from `prediction-diagnosis-extra-20261001/`.

Next compare bounded dispatch or fleet-use candidates against the same accepted requests.
Retain individual delays and unfinished requests alongside aggregate throughput.
The existing [strict finishing-wait rule](finishing-wait-capacity.md) reduces completions in all four selected AM14/AM15 cells.
The Acton buffer regression requires separate grant and release histories because it has substantial traffic queues.
These selected AM cells do not qualify all demand bands or authorize a default or capacity-limit change.
