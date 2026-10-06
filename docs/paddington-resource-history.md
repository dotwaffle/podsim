# Paddington reservation and resource histories

Mirrored Paddington geometry produces many more rejected admission attempts in two Central Early schedules.
Most added platoon-guard rejections occur because the predecessor has not reserved the requested span.
Downstream owners usually keep moving, and all 128 selected histories reach their original requested frontier within 24 simulated seconds after selection.
No route or certificate change is involved.
No production rule changes, and the results give no basis for weakening clearance or the predecessor-coverage guard.
The [position trials](paddington-layout.md) remain the causal evidence for the geometry effect.

## Common setup

Each study replays earlier and mirrored Central geometry with seeds 1 and 2.
Each run schedules 3,599 requests over six hours at 10 requests per minute, with a seven-hour cap.
Virtual platoons have a four-pod limit.
Buffers, reassignment, sharing, and redistribution are off, and routing is free-flow.
Each stage uses a test-only observer on its runtime source, with no production grant, project, saved-state, wire, or default change.
Short pilots and every full run match the earlier results, ordered schedules, and earlier measurements exactly.
Safety, speed, and request accounting pass once per simulated second, with no skipped request.
Independent review checks each observer and its limits.
Counts are repeated attempts or weighted samples, not unique pods, complete waits, or passenger delay.

## Reservation diagnosis (source `bd2bd7c`)

The observer covers all Paddington station lanes and their incoming mainline lanes, but not every upstream reservation that reaches Paddington.
The first failed condition follows the production short-circuit order: draining link, then span past the certificate, then predecessor frontier.
A predecessor-frontier failure means the predecessor has not reserved the last block of the span, and the span stays inside the follower's certificate.

| Seed | Geometry | Shared guard attempts | Ordinary resource denials | Successful grants | Predecessor frontier | Past certificate | Draining |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | Old | 1,984,580 | 2,275,020 | 45,649 | 906,552 | 447,382 | 630,646 |
| 1 | Current | 10,073,936 | 5,757,730 | 42,515 | 8,228,945 | 983,798 | 861,193 |
| 2 | Old | 1,930,803 | 2,138,578 | 44,141 | 873,684 | 433,812 | 623,307 |
| 2 | Current | 8,061,944 | 4,766,151 | 41,771 | 6,285,076 | 1,117,531 | 659,337 |

Mirrored geometry increases shared-guard attempts by factors of 5.08 and 4.18, and predecessor-frontier failures by factors of 9.08 and 7.19.
Those failures are 81.7% and 78.0% of mirrored shared-guard attempts, and no other first-failure category appears.
Assigned pickup pods contribute about 98.8% and 98.5% of mirrored denied attempts.
The main location is `london-link-067-ab-2`, toward Paddington, where ordinary track contention also rises.
The study does not explain why the predecessor frontier advances less readily.
The guard stops a follower from taking free cells its predecessor still needs, and removing it can let the follower block its own predecessor.

## Leader progress (source `bbcb9e8`)

This stage adds platoons-off arms and follows each rejected predecessor's decision in the same tick.
A stationary rejection tick has follower speed below 0.01 m/s and lasts one sixtieth of a simulated second.
An episode ends when the follower, predecessor, or rejected frontier changes, so episode lengths are not total delay.

| Seed | Geometry | Rejection attempts | Stationary pod-seconds | Episodes | Longest, s |
| --- | --- | ---: | ---: | ---: | ---: |
| 1 | Earlier | 906,552 | 7,448.52 | 9,748 | 18.57 |
| 1 | Mirrored | 8,228,945 | 85,622.45 | 71,766 | 23.78 |
| 2 | Earlier | 873,684 | 7,165.35 | 9,463 | 18.58 |
| 2 | Mirrored | 6,285,076 | 65,025.05 | 55,800 | 24.23 |

Stationary pod-seconds increase by factors of 11.50 and 9.08, through more episodes as well as longer ones.
In mirrored seed 1, 4,116,661 predecessor decisions deny ordinary track ownership on `london-link-067-ab-2` and 4,111,675 hit another shared guard.
Seed 2 records 3,127,158 and 3,157,432.
Mirrored predecessors request at most 30 meters, and the ordinary cell on that lane measures 28.56 meters, so oversized groups do not explain them.
About 99.5% of owner chains stop at a pod whose latest decision is an earlier successful grant, and none reaches the cycle or depth limit.
Completions with virtual platoons versus platoons off:

| Geometry | Seed 1 virtual | Seed 1 off | Seed 2 virtual | Seed 2 off |
| --- | ---: | ---: | ---: | ---: |
| Earlier | 2,893 | 2,638 | 2,896 | 2,705 |
| Mirrored | 2,660 | 2,646 | 2,760 | 2,709 |

## Movement during waits

The observer samples the terminal owner of each same-tick denial chain once per selected follower rejection.

| Seed | Geometry | Samples | Moving owner | More than 12 m available | Platoon boundary tighter |
| --- | --- | ---: | ---: | ---: | ---: |
| 1 | Earlier | 446,911 | 99.60% | 411,078 | 36,223 |
| 1 | Mirrored | 5,137,347 | 99.25% | 4,053,304 | 1,088,857 |
| 2 | Earlier | 429,921 | 99.45% | 382,356 | 47,989 |
| 2 | Mirrored | 3,901,503 | 99.32% | 3,207,477 | 697,408 |

Every sampled owner stays in Traveling activity, and the calculated next-tick speed permits movement in more than 99% of samples.
Every owner with a traveling predecessor has a mapped gap above 12 meters.
Samples with available distance at most 0.00001 meters are rare, with 8,989 and 5,946 in the mirrored runs.
The data fit moving queues that pass frontier waits upstream.
They do not show a persistent reservation cycle or a deadlock.

## Clearance samples (source `9cb066f`)

This stage samples every sixth tick, which can bias the sample, and rechecks ownership after the same tick's movement and releases.
Release distance is the owner's signed `releaseAt - distance` at rejection, weighted by resource observation.

| Seed | Geometry | Sampled rejections | Predecessor resources | Ancestor resources | Mean release distance, m |
| --- | --- | ---: | ---: | ---: | ---: |
| 1 | Earlier | 74,423 | 51,914 | 35,688 | 11.97 |
| 1 | Mirrored | 856,213 | 591,049 | 443,867 | 11.49 |
| 2 | Earlier | 71,535 | 49,600 | 34,150 | 12.25 |
| 2 | Mirrored | 650,431 | 445,764 | 341,143 | 11.64 |

Requested resources have mean owner release distances of 8.94, 8.42, 8.68, and 8.50 meters in the same row order.
Most requested resources have an owner outside the follower's predecessor chain, and over 99% keep the same owner after the tick.
After all admissions, the predecessor covers the requested frontier in 13, 112, 16, and 93 sampled rejections.
Release distances stay similar across geometries despite the large rise in rejection counts.
The stage excludes the later [berth routing fixes](berth-route-preference.md).

## Resource histories (source `9cb066f`)

The observer selects the first 32 distinct stopped follower and route pairs per run and follows each to its next frontier grant.
Limits are 300 simulated seconds and 6,000 events per history, and no history reaches a limit.
The observer does not follow every resource until it is free.

| Seed | Geometry | Histories | Mean interval, s | Longest interval, s | Release to free | Ownership transfers |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| 1 | Earlier | 32 | 7.76 | 17.97 | 29 | 16 |
| 1 | Mirrored | 32 | 11.11 | 22.20 | 31 | 11 |
| 2 | Earlier | 32 | 7.81 | 19.03 | 24 | 4 |
| 2 | Mirrored | 32 | 10.66 | 23.73 | 32 | 15 |

At the release hook the actor is 0.00008 to 0.07556 meters past its threshold.
Release-to-final-grant intervals range from one tick to 17.22 seconds and include other dependencies.
Every selected rejection requests a cell on `london-link-067-ab-2`, which is identical in both projects, 314.166 meters long, and 11 ordinary cells.
Its length therefore does not explain the extra rejections in mirrored geometry.

## Conclusion and limits

Mirrored geometry lengthens waits through predecessor-frontier rejections on one unchanged lane, while owners keep moving and release distances stay similar.
No stage isolates a geometric parameter.
Station access, downstream routes, certificate turns, and wider traffic can all affect that lane.
Two schedules, bounded durations, and sampled safety do not prove continuous deadlock freedom, sustained capacity, or LondonFull behavior.
The histories cover selection-to-grant intervals, not complete waits from onset.
Some arms keep unfinished requests.
No stage adds physical-restore qualification.
Concurrent diagnostic runs give no CPU comparison.
Fixed station-entry platoons have a separate [contract](station-entry-platoons.md).
The buffer and reassignment defaults remain off.

The raw measurement data of these studies is in git history.
