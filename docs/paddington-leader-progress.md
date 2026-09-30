# Paddington leader progress

Mirrored geometry increases stationary follower waits while predecessors lack the requested reservation span.
At those decisions, the predecessor usually waits for one ordinary track cell or for its own predecessor.
The probe finds no reservation defect or safe reason to weaken the shared-resource guard.
The physical motion of downstream resource owners remains unresolved.

## Method

This follows the [reservation diagnosis](paddington-reservation-diagnosis.md).
Eight arms replay earlier and mirrored Central geometry, platoons off or virtual, and seeds 1 and 2.
Each schedules 3,599 requests during six hours at 10 requests per minute, with a seven-hour cap.
Schedules match within each seed.
Buffers, reassignment, redistribution, and sharing are off.
Routing is free-flow; virtual platoons have a four-pod limit.

The test-only observer records actual admission branches for all pods.
It selects predecessor-frontier rejections from Paddington station lanes and their incoming mainline lanes.
It captures the rejected predecessor frontier before later intents can change it.
After all intents finish, it records the predecessor's actual decision, its age, and its requested span.
Pure block lookups preserve production cursors.
The original probe that changed lookup caches is retained separately and excluded.

A stationary rejection tick means the selected follower had speed below 0.01 m/s at admission.
Its duration is one sixtieth of a simulated second.
A contiguous episode has the same follower, predecessor, and rejected frontier, without missing rejection ticks.
An episode ends when any of those conditions changes, even if traffic still delays the pod.
Thus, episode lengths are not complete journey delay or total traffic-stop durations.
The observer retains the longest 20 episodes per arm.

Both short geometry pilots match current production results exactly.
The four virtual arms also match every historical result field, ordered schedule, and predecessor-frontier attempt count.
Safety, speed limits, and active-request accounting pass once per simulated second.
Final request IDs reconcile, and no request is skipped.
This probe adds no physical-restore qualification.
Its runtime is source `bbcb9e8` with diagnostic-only overlays.

## Stationary frontier waits

| Seed | Geometry | Rejection attempts | Stationary rejection pod-seconds | Contiguous stationary episodes | Longest episode, seconds |
| --- | --- | ---: | ---: | ---: | ---: |
| 1 | Earlier | 906,552 | 7,448.52 | 9,748 | 18.57 |
| 1 | Mirrored | 8,228,945 | 85,622.45 | 71,766 | 23.78 |
| 2 | Earlier | 873,684 | 7,165.35 | 9,463 | 18.58 |
| 2 | Mirrored | 6,285,076 | 65,025.05 | 55,800 | 24.23 |

Stationary rejection pod-seconds increase by factors of 11.50 and 9.08.
The increase involves more episodes as well as longer episodes.
These counts cover the selected failure condition, not all stopped pods or all passenger delay.
They do not add to the earlier once-per-second stopped-pod census.

Every selected predecessor decision is from the same tick.
In mirrored seed 1, 4,116,661 decisions deny ordinary track ownership on `london-link-067-ab-2`; 4,111,675 hit another shared guard.
Seed 2 records 3,127,158 ordinary track denials and 3,157,432 shared guards.
The remaining 609 and 486 predecessor decisions record a successful grant in the same tick.
Such a grant does not necessarily cover the span that the follower needs.

Every selected mirrored predecessor requests at most 30 meters, rounded up to a ten-meter bin.
The ordinary cell on the dominant lane measures 28.56 meters.
Earlier geometry also has a small number of spans in the 160-meter bin, but they disappear from these mirrored observations.
Oversized complete groups do not explain these immediate predecessor attempts.
This does not exclude downstream conflict geometry affecting the pods that already own those cells.

## Observed owner chains and remaining cause

The probe follows denial owners through same-tick observed decisions, up to 32 pods.
It stops at missing or older observations, a successful grant, a cycle, or the depth limit.
These are observed dependencies, not predictions or proof of a persistent deadlock.

About 99.5% of mirrored chains stop at a pod whose most recent observed decision is an earlier successful grant.
The remainder reach a successful grant in the current tick.
No selected chain reaches the cycle or depth limits.
The observation therefore cannot attribute the delay to a current denied reservation at that terminal pod.
A later probe would need its current movement boundary, braking limit, physical predecessor gap, and station phase.

The platoon-off controls also limit the interpretation of service effects.
Earlier geometry completes 2,893 and 2,896 requests with virtual platoons, versus 2,638 and 2,705 with platoons off.
Mirrored geometry completes 2,660 and 2,760 with virtual platoons, versus 2,646 and 2,709 with platoons off.
The earlier platoon benefit becomes much smaller after mirroring, but this probe does not isolate one geometric parameter.
The existing node-position intervention remains the geometry evidence.

No production admission, clearance, priority, certificate, saved-state, or default setting changes.
Both seeds retain unfinished requests; these comparisons do not establish sustained capacity.
Concurrent functional checks make wall time unsuitable for CPU claims.

[Arm totals](measurements/paddington-leader-progress.csv) retain completion, waits, episode counts, and durations.
[Metadata and dependency summaries](measurements/paddington-leader-progress.json) retain frozen and raw hashes, decision counts, span bins, and longest episodes.
Raw results and source copies remain in `~/.cache/agents/podsim/paddington-progress-20260930/`.
