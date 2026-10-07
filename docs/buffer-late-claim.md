# Later station-buffer berth claims

Status: station buffers and compact station queues were removed on October 7, 2026.
Measurements showed that they lowered station entry throughput.
This record stays as the decision record.
The sections after Station entry throughput describe the removed implementation as it was at each measurement.

Delaying unlinked heads reduces sampled berth-claim time by less than one percent in the Acton screen.
It also reduces completions and exceeds individual service limits.

## Station entry throughput

Every LondonFull station has one generated access lane from its diverge to its entry.
The lane is 139 m long, has a speed limit of 14 m/s, and holds one pod at a time.
The next pod stops about 56 m before the diverge with "Pod ahead" until the leader clears the whole lane, then starts from standstill.

The scratch harness is not kept.
It ran the user's LondonFull project (platoon limit 4, pickup reassignment on) with 60 orders per minute to one station.
Each run covered 30 simulated minutes, and the first 10 minutes are excluded.
The first arms ran at source commit `2a9d6b9`, and the later arms at `0f5ae61`.

| Station | Buffers | Entries per minute | Headway p50 / p90, seconds |
| --- | --- | ---: | --- |
| King's Cross 940GZZLUKSX | off | 4.48 | 11.0 / 17.1 |
| King's Cross | on, compact spacing | 3.99 | 13.0 / 19.1 |
| King's Cross | on, ordinary spacing | 3.87 | not kept |
| Waterloo 940GZZLUWLO | off | 4.40 to 4.50 | 14.0 / 18.5 (4.40 run) |

King's Cross completed 121 orders with buffers off and 105 with buffers on and ordinary spacing.
A free, unreserved berth existed in every sample while a pod queued, so the entry lane limits throughput, not the berths.
Buffer links almost never formed.
With compact spacing, two pods were on the entry lane in 5 of 18,000 samples.
The platoon entry runs, the early berth choice, and coast-in that replaced the buffers are measured in [station entry throughput](qualification.md#station-entry-throughput).

## Candidate and checks

The first candidate required every head to reach the fixed stopping frontier before seeking a berth.
Existing linked-drain and assigned-pickup staging tests failed.
That candidate remains rejected.
Its failures do not justify weaker tests or ownership checks.

The narrower candidate delays only heads without a follower.
It permits the existing complete berth-path grant within 24 meters of the stopping frontier.
Linked heads keep the existing grant timing and draining coordination.
The candidate preserves physical clearance, complete suffix ownership, atomic rollback, and saved certificates.
It changes no geometry, project, save, wire format, or default.

All focused existing buffer, platoon, and restore tests pass for the narrower candidate.
Additional tests check unchanged saved state and owners before the threshold, full grants at the frontier, and safe completion.
Four dense pilots pass safety checks, full physical restores, and nil-observer result equality.
The candidate-off pilot matches every measured evidence field in the current buffer-off pilot.
These fields include offers, timings, pending requests, saved-restore records, check counts, and claim episodes.

## Frozen Acton screen

The comparison uses current source `943fd74` and the current builtin LondonFull fleet and geometry.
The historical Acton mixed schedule retains seed 3, four station pairs, and 16 offers per minute.
Each primary arm receives the same 1,920 offers over two hours and has a fixed three-hour cap.
The queue limit is one million.
Sharing, reassignment, and positioning remain off.
Routing uses free-flow costs and virtual platoons permit four pods.
All offers are accepted, and every arm remains overloaded at the cap.

Incoming-claim episodes require an empty berth with a traveling owner whose route ends at that berth.
The observer samples once per simulated second.
The mean and p95 exclude right-censored episodes and report their count separately.

| Arm | Completed | Unfinished | Completed claim episodes | Censored episodes | Mean claim, seconds | Claim p95, seconds |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Current buffers off | 1,029 | 891 | 622 | 0 | 7.51 | 9 |
| Current buffers on | 907 | 1,013 | 559 | 1 | 25.01 | 28 |
| Later unlinked claims on | 893 | 1,027 | 554 | 1 | 24.82 | 28 |

The candidate decreases the sampled mean by 0.19 seconds, or 0.76%, against current buffers on.
It completes 14 fewer requests than current buffers on and 136 fewer than buffers off.
Its claim p95 remains unchanged.
The claim-duration result does not establish a useful service gain.
These observations do not isolate every source of the buffer regression.

## Matched service and unfinished requests

Journey time starts at the request and ends at alighting.
Mean pickup wait includes elapsed waits for pending requests.
Mean journey and journey p95 include completed requests only.
The individual comparisons preserve offered-event indices and report unmatched endpoints as censored observations.

| Arm | Mean pickup, seconds | Mean journey, seconds | Journey p95, seconds |
| --- | ---: | ---: | ---: |
| Current buffers off | 3,478.04 | 3,461.52 | 6,511.88 |
| Current buffers on | 3,977.74 | 3,858.73 | 7,165.70 |
| Later unlinked claims on | 4,014.72 | 3,880.52 | 7,087.48 |

Against current buffers on, the candidate passes the aggregate 2% limits.
It still exceeds individual pickup limits for 165 jointly boarded offers and journey limits for 211 jointly completed offers.
It also fails to complete some offers that current buffers on completes by the fixed cap.
Censored requests cannot pass an individual gate through omission.
Against buffers off, the candidate also fails the aggregate limits.
No comparison establishes qualification or a default change.

The short candidate-on pilot completes 292 requests, against 298 with current buffers on and 320 with buffers off.
Its sampled claim mean is 24.17 seconds, against 24.23 with current buffers on.
The longer screen retains this negative result rather than extending an unchanged broad qualification matrix.

## Evidence limits

Primary arms check separation, per-lane speed, ownership, and request conservation once per simulated second.
Physical restores at one and two hours retain full saved pod records, routes, poses, riders, admission ages, and certificates.
Each restored copy passes a separate 60-second continuation with dense checks and new buffers and swaps disabled.
Those copies do not prove full drainage or replay future offered demand.
The focused tests establish complete disabled-policy drainage.

One-second claim samples can miss short episodes or merge changes between samples.
An episode ending does not prove cancellation.
Concurrent functional runs provide no CPU comparison.
This selected overloaded fixture does not describe all station layouts or establish sustained capacity.

Independent candidate, helper, runner, and evidence reviews pass.
The raw measurement data is in git history.
The sections below retain the earlier Acton station-speed trials and the Central terminus burst measurements.
The [experimental gates](experimental-adoption.md) define the separate qualification requirements.

## Acton buffer discharge and station speed

Buffers claim berths earlier, but each incoming claim lasts longer.
Lower station speeds further reduce completions in this fixture and lengthen each claim.
Neither the buffer default nor a lower station speed is supported.

All arms use frozen production source `e285e69` and the Acton mixed-burst fixture.
They accept the same 1,920 requests over two hours and permit a three-hour cutoff.
Sharing, pickup reassignment, and positioning remain off.
Routing uses free-flow costs, virtual platoons permit four pods, and the queue limit is one million.
The 14 m/s diagnostic reproduces the earlier buffer arms exactly.
The speed trials change only the limits on Acton's 19 station-tagged lanes: approaches, berth links, the bypass, and exits.
These are station-wide sensitivity trials, not isolated tests of following gaps.

The observer samples incoming berth claims once per simulated second.
The table excludes right-censored episodes from duration statistics.
Grant examples keep only the first 30 events, so they can omit later grants; aggregate counters are complete.

| Speed, m/s | Buffers | Completed | Unfinished | Completed claim episodes | Censored episodes | Mean claim, seconds | Claim P95, seconds | Buffer grants |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 14 | Off | 1,029 | 891 | 622 | 0 | 7.51 | 9 | 0 |
| 14 | On | 907 | 1,013 | 559 | 1 | 25.01 | 28 | 557 |
| 10 | Off | 815 | 1,105 | 495 | 0 | 7.14 | 8 | 0 |
| 10 | On | 797 | 1,123 | 488 | 2 | 30.44 | 35 | 486 |
| 7 | Off | 621 | 1,299 | 389 | 0 | 7.10 | 8 | 0 |
| 7 | On | 613 | 1,307 | 389 | 1 | 39.79 | 46 | 388 |

At 14 m/s, the buffer makes 557 grants from 384,727 calls.
The mean remaining free-flow travel at a grant is 18.75 seconds, rising to 25.55 and 36.52 seconds at 10 and 7 m/s.
The longer sampled claims accompany fewer completions.
This association does not isolate all causes of the buffer regression.

At 10 m/s, completions decrease by 214 with buffers off and 110 with buffers on.
At 7 m/s, the decreases are 408 and 294.
All six arms remain overloaded.
Lower speeds reduce braking distance, but the trial retains the track-cell geometry, 12-meter physical clearance, and the fixed stopping frontier.
Lower limits also lengthen approach and exit travel and change route costs.
A smaller gap at lower speed does not establish higher throughput for this geometry and safety model.
Keep the existing speed limits and reservations.
A future buffer change must preserve the complete-path safety contract and show better service on matched individual cohorts.

## Terminus burst service

Train-sized bursts increase pickup waits at Euston and Paddington in this Central fixture.
Buffers raise pickup waits in all eight unlimited-queue comparisons, including steady demand.
The buffer default remains off.

The fixture uses source `4e36e9f` and the frozen mirrored LondonCentral project.
Each station has two passenger berths.
Both seeds offer 239 outbound parties during a 20-minute arrival window, with a 90-minute run cap.
Steady arrivals occur every five seconds.
Burst arrivals deliver 120 parties at five seconds and 119 parties at 605 seconds.
The 32 cells vary station, seed, buffers, queue limit, and burst size independently.
The queue limit is the live limit of 200 pending requests or the study limit of one million.
Sharing, reassignment, and redistribution are off, and virtual platoons allow four pods.
All 32 cells pass the safety, speed, and conservation checks once per simulated second.
Each passes physical restores at 600 and 1,200 seconds, each followed by a 60-second continuation without new buffer admissions.

All unlimited-queue cells serve all 239 parties and drain within the cap.
The table compares unlimited-queue burst cells with identical accepted requests.
Times use simulated seconds.

| Station | Seed | Buffers | Mean pickup wait | P95 pickup wait | Run ends |
| --- | ---: | --- | ---: | ---: | ---: |
| Euston | 1 | Off | 940.5 | 1,810.1 | 3,235 |
| Euston | 1 | On | 1,919.2 | 3,556.5 | 4,840 |
| Euston | 2 | Off | 977.7 | 1,806.1 | 3,535 |
| Euston | 2 | On | 1,919.2 | 3,556.5 | 5,017 |
| Paddington | 1 | Off | 1,660.0 | 3,151.1 | 4,566 |
| Paddington | 1 | On | 1,947.1 | 3,657.9 | 5,094 |
| Paddington | 2 | Off | 1,658.4 | 3,144.4 | 4,592 |
| Paddington | 2 | On | 1,940.9 | 3,645.4 | 5,107 |

With buffers off, steady mean waits range from 701.1 to 789.0 seconds at Euston and about 1,407 seconds at Paddington.
Buffers also increase these steady waits.
The bounded queue skips nine burst requests in every buffer-on cell.
Buffer-off cells skip zero at Euston, four at Paddington seed 1, and three at Paddington seed 2.
Steady cells skip none.
Do not compare bounded-queue wait means as though every arm served the same requests.

In unlimited burst cells, incoming berth claims account for 24.7-27.1% of Euston berth samples with buffers off and 59.9-62.1% with buffers on.
Paddington changes from 22.4-22.6% to 58.4-58.7%.
These fractions use each arm's full run duration, so they describe reservation states, not occupancy over one common window.
Free berths also coexist with pending requests for much of each run.
This does not prove that a local idle pod can serve those requests, because pending parties can have assigned pickups elsewhere.

The measurements make incoming berth claims and pickup supply the next diagnostic targets.
They establish no cause and do not justify a clearance change.
Two stations and two seeds do not qualify a default change or a LondonFull capacity claim.
The fixture has finite outbound demand with no inbound passenger service or background traffic.
The production oracle compares result aggregates, not individual timing parity.
