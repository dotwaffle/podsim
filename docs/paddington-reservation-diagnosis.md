# Paddington reservation diagnosis

The mirrored Paddington approach produces many more rejected admission attempts in two Central Early schedules.
Most added platoon-guard rejections occur because the predecessor has not reserved the requested span.
That span remains inside the follower's certificate, and the link is not draining.
This identifies the dominant observed rejection condition, without establishing a safety defect or the geometry's root cause.

## Method and controls

The study follows an earlier Paddington geometry intervention, which the [current-source position trials](paddington-layout.md) repeat.
Four arms replay earlier and mirrored Central geometry with seeds 1 and 2.
Each has six hours of arrivals at 10 requests per simulated minute, followed by a one-hour completion window.
Virtual platoons have a four-pod limit.
Sharing, redistribution, station buffers, and pickup swaps remain off.
Routing is free-flow.

A test-only overlay records the actual admission decision before grant returns.
It records successful grants, the first denied resource, and the shared-resource safety guard separately.
It also records current lane, request class, predecessor or owner lane, and reservation/certificate span.
The scope covers all Paddington station lanes and their incoming mainline lanes, filtered by the pod's current lane.
It does not cover every upstream reservation that reaches Paddington.
Counters measure admission attempts, including repeated attempts by the same pod.
They do not measure blocked seconds, unique pods, or passenger delay.

The runtime source is `bd2bd7c` with diagnostic-only overlays.
All four aggregate result records match the historical mirrored-layout controls exactly.
Ordered request schedules are byte-identical to those controls.
A second overlay splits the shared guard by the first failed coupling condition.
After combining those new categories, every complete original sensor key and attempt count matches the first study exactly.
Both stages pass short production-comparison pilots and independent instrumentation review.
No production grant, project, saved-state, wire, or default changes.

## Actual admission decisions

Counts below cover the selected lanes over each seven-hour run.
The ordinary resource column counts the first denied resource of an attempt.
The shared guard rejects a linked follower that cannot make a coupled grant while it still holds a resource owned by a pod ahead.

| Seed | Geometry | Shared guard attempts | Ordinary resource denials | Successful grants |
| --- | --- | ---: | ---: | ---: |
| 1 | Old | 1,984,580 | 2,275,020 | 45,649 |
| 1 | Current | 10,073,936 | 5,757,730 | 42,515 |
| 2 | Old | 1,930,803 | 2,138,578 | 44,141 |
| 2 | Current | 8,061,944 | 4,766,151 | 41,771 |

Mirrored geometry increases shared-guard attempts by factors of 5.08 and 4.18 for seeds 1 and 2.
Assigned pickup pods contribute about 98.8% and 98.5% of all mirrored denied attempts.
The main location is `london-link-067-ab-2`, toward Paddington.
Ordinary track contention on that lane also increases.
Most mirrored shared-guard attempts name a predecessor on that same mainline lane.
These counts supplement the earlier stopped-pod observations.
They are not a conversion of those observations into measured waiting time.

## First failed coupling condition

The refinement preserves the production short-circuit order.
A draining link fails first.
A span past the certificate fails next.
A predecessor frontier failure occurs only after the requested span passes the certificate checks and the predecessor is traveling.
It means the predecessor has not reserved the corresponding last block of that span.
Categories describe the first failed condition, not every constraint that might also apply.

| Seed | Geometry | Predecessor frontier | Past certificate | Draining |
| --- | --- | ---: | ---: | ---: |
| 1 | Old | 906,552 | 447,382 | 630,646 |
| 1 | Current | 8,228,945 | 983,798 | 861,193 |
| 2 | Old | 873,684 | 433,812 | 623,307 |
| 2 | Current | 6,285,076 | 1,117,531 | 659,337 |

Mirrored predecessor-frontier failures increase by factors of 9.08 and 7.19.
They account for 81.7% and 78.0% of mirrored shared-guard attempts.
The sensor observes no other first-failure category in these runs.
Thus, a turn limit or certificate endpoint alone does not explain most observed guard rejections.
The study does not identify why the predecessor frontier advances less readily.
Possible follow-up measurements include its actual denied resources and conflict-zone lengths at these decisions.

The guard prevents a follower from taking free cells that its predecessor still needs.
Removing it can let that follower block its own predecessor.
These results support investigation of upstream reservation geometry and leader progress, not weaker clearance or arbitrary priority bypasses.
The virtual-platoon default remains unchanged.

## Validation and limits

Each stage has 100,800 once-per-second parent observations across its four arms.
Safety, speed-limit, unique-request accounting, and the final request-timing census pass.
No request is skipped.
The local diagnostic counters do not replace the global stopped-pod census.
Neither stage adds physical-restore qualification.
Earlier restore results remain separate evidence.

Two schedules, bounded durations, and sampled safety do not prove continuous deadlock freedom, indefinite capacity, or behavior on LondonFull.
Repeated attempt counts do not identify distinct episodes or establish proportional passenger harm.
The earlier node-position intervention isolates a Paddington geometry effect.
The present counters narrow its reservation mechanism but do not isolate one geometric parameter.
CPU and wall times are not performance measurements because these functional studies can run concurrently.

The raw measurement data is in git history.
