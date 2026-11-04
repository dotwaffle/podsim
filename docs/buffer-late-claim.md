# Later station-buffer berth claims

Status: station buffers and compact station queues were removed on October 7, 2026.
Measurements showed that they lowered station entry throughput.
This record stays as the decision record.
The sections after Station entry throughput describe the removed implementation as it was at each measurement.

Delaying unlinked heads reduces sampled berth-claim time by less than one percent in the Acton screen.
It also reduces completions and exceeds individual service limits.

## Station entry throughput

Every LondonFull station has one generated access lane, 139 m long, with a 14 m/s limit, that holds one pod at a time.
The next pod stops about 56 m before the diverge until the leader clears the whole lane, then starts from standstill.
A scratch harness ran the user's LondonFull project (platoon limit 4, pickup reassignment on) with 60 orders per minute to one station for 30 minutes, excluding the first 10.

| Station | Buffers | Entries per minute | Headway p50 / p90, seconds |
| --- | --- | ---: | --- |
| King's Cross | off | 4.48 | 11.0 / 17.1 |
| King's Cross | on, compact spacing | 3.99 | 13.0 / 19.1 |
| King's Cross | on, ordinary spacing | 3.87 | not kept |
| Waterloo | off | 4.40 to 4.50 | 14.0 / 18.5 (4.40 run) |

King's Cross completed 121 orders with buffers off and 105 with buffers on and ordinary spacing.
A free, unreserved berth existed in every sample while a pod queued, so the entry lane limits throughput, not the berths.
Buffer links almost never formed: with compact spacing, two pods were on the entry lane in 5 of 18,000 samples.

Three platoon entry changes replaced the buffers: a platoon run can end on the entry lane, an early berth choice for pods of the small classes, and coast-in at a contested diverge.
Together they raised King's Cross to 6.61 and 6.69 entries per minute and Waterloo to 7.11 and 6.73, with 174 to 179 completed orders, on two seed pairs.
The maintainer accepted the two missed targets (King's Cross other-approach p90 of 19.0 s and Waterloo 6.73 entries per minute) on October 7, 2026.
`TestPlatoonEntryBatch` in `internal/sim` pins that an aged rival gets the next free grant after a platoon passes.

## Frozen Acton screen

The source is `943fd74` with the builtin LondonFull fleet.
The Acton mixed schedule (seed 3, four station pairs, 16 offers per minute) gives 1,920 offers over two hours, a three-hour cap, and a queue limit of one million.
Sharing, reassignment, and positioning are off, and every arm stays overloaded at the cap.
The first candidate required every head to reach the stopping frontier before it sought a berth, and it failed the linked-drain and pickup staging tests.
The narrower candidate delays only heads without a follower and keeps the complete-path grant rules.

| Arm | Completed | Mean pickup, s | Journey p95, s | Mean claim, s | Claim p95, s |
| --- | ---: | ---: | ---: | ---: | ---: |
| Current buffers off | 1,029 | 3,478.04 | 6,511.88 | 7.51 | 9 |
| Current buffers on | 907 | 3,977.74 | 7,165.70 | 25.01 | 28 |
| Later unlinked claims on | 893 | 4,014.72 | 7,087.48 | 24.82 | 28 |

The candidate cuts the sampled claim mean by 0.19 s (0.76%) against buffers on.
It completes 14 fewer requests than buffers on and 136 fewer than buffers off.
It passes the aggregate 2% limits against buffers on, but it exceeds the individual limits for 165 jointly boarded offers (pickup) and 211 jointly completed offers (journey).
The claim-duration result shows no service gain, and the candidate is rejected.
One-second samples can miss short claim episodes, and the fixture does not describe all layouts or sustained capacity.

## Acton buffer discharge and station speed

Buffers claim berths earlier, but each incoming claim lasts longer.
Lower station speeds on Acton's 19 station-tagged lanes cut completions further.

| Speed, m/s | Buffers | Completed | Mean claim, seconds |
| ---: | --- | ---: | ---: |
| 14 | Off | 1,029 | 7.51 |
| 14 | On | 907 | 25.01 |
| 10 | Off | 815 | 7.14 |
| 10 | On | 797 | 30.44 |
| 7 | Off | 621 | 7.10 |
| 7 | On | 613 | 39.79 |

All six arms stay overloaded.
A smaller gap at lower speed does not give higher throughput under the track-cell geometry, 12 m clearance, and fixed stopping frontier.
Neither the buffer default nor a lower station speed is supported.

## Terminus burst service

The Central fixture (source `4e36e9f`) offers 239 outbound parties in 20 minutes at Euston and Paddington, in steady and burst forms, with seeds 1 and 2.
Buffers raise pickup waits in all eight unlimited-queue comparisons.
In the burst cells, the buffer-off mean pickup wait is 940.5 to 977.7 s at Euston and 1,658.4 to 1,660.0 s at Paddington.
With buffers on, it is 1,919.2 s at Euston and 1,940.9 to 1,947.1 s at Paddington.
Incoming berth claims hold 24.7% to 27.1% of Euston berth samples with buffers off and 59.9% to 62.1% with buffers on.
The measurements make incoming berth claims and pickup supply the next diagnostic targets, and they establish no cause.
Two stations and two seeds do not qualify a default change.
