# Later station-buffer berth claims

Delaying unlinked heads reduces sampled berth-claim time by less than one percent in the Acton screen.
It also reduces completions and exceeds individual service limits.
Keep the existing buffer implementation and leave buffers off by default.

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
[Measurements](measurements/buffer-late-claim.json) retain hashes, results, limit exceedances, unfinished cohorts, and sampled-claim totals.
The [earlier station-speed study](station-buffer-speed.md) retains the negative speed trials.
The [experimental gates](experimental-adoption.md) define the separate qualification requirements.
