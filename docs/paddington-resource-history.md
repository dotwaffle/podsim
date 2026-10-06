# Paddington selected resource histories

All 128 histories reach their original requested frontier within 24 simulated seconds after selection.
The histories show resource release, ownership transfer, and later admission without a route or certificate change.
They give no basis for weakening clearance or the predecessor-coverage guard.

## Method and validation

This extends the [paired clearance samples](paddington-clearance.md) through the follower's next successful frontier grant.
It retains historical source `9cb066f`, before the later berth routing fixes.
The four longer runs use earlier or mirrored Central geometry, seeds 1 and 2, and virtual platoons.
Each offers ten requests per minute for six hours, with a seven-hour cap.
Buffers, reassignment, sharing, and redistribution remain off.

Two short pilots match production result aggregates exactly.
All six runs reproduce their earlier results, ordered schedules, progress, movement, and clearance measurements exactly.
Full-run safety, speed, and request accounting pass once per simulated second.
These histories add no physical-restore qualification.

For each run, the observer selects the first 32 distinct stopped follower/route pairs from the earlier every-sixth-tick rejection probe.
Each history records the original requested span, cell bounds, resource identities, release thresholds, positions, and certificate fields.
Resource identity includes kind, lane or resource ID, and cell index.
The release hook records the actor and resulting owner immediately after `releaseRouteResource` changes ownership.
Other ownership changes are sampled at the follower's admissions and after movement.
Positions are sampled once per second.

Each history ends when the follower reserves its original requested frontier or changes its route.
Limits are 300 simulated seconds and 6,000 events per history, with explicit censoring.
No selected history reaches a limit or remains unfinished.
The observer does not follow every resource until it becomes free.
A retained resource can outlive the successful frontier grant.

Purity tests preserve physical state, ownership, exported state, and lookup cursors.
Regressions check transfer events, final-grant ownership, censoring, idle-route snapshots, and distinct track-cell identities.
Independent review checks the observer and its interpretation limits.
An earlier export omitted cell indexes and was superseded before recording these conclusions.
The corrected observer reran every pilot and full comparison.

## Selection-to-grant intervals and releases

Pilot histories repeat seed 1's early selections and are not additional independent cases.
The table includes only the four longer runs.

| Seed | Geometry | Histories | Mean interval, s | Longest interval, s | Release to free | Ownership transfers |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| 1 | Earlier | 32 | 7.76 | 17.97 | 29 | 16 |
| 1 | Mirrored | 32 | 11.11 | 22.20 | 31 | 11 |
| 2 | Earlier | 32 | 7.81 | 19.03 | 24 | 4 |
| 2 | Mirrored | 32 | 10.66 | 23.73 | 32 | 15 |

Every explicit release has a recorded actor release threshold.
At the hook, the actor is 0.00008–0.07556 meters past that threshold across these cases.
Some resources pass to a follower instead of becoming free.
A release does not imply that every requested resource is available or that the predecessor covers the next frontier.
Recorded release-to-final-grant intervals range from one simulation tick to 17.22 seconds.
That interval includes other dependencies and does not identify one resource as the sole cause of the wait.

For example, mirrored seed 2 first selects pod 065 at 1,437.0 seconds.
Pod 070 releases the selected track cell at 1,448.3167 seconds.
On the next tick, pod 072 owns the cell and pod 065 receives the recorded frontier grant, at 1,448.3333 seconds.
The exported event record identifies the exact track cell and the release actor's position.

No selected route or certificate changes during these histories.
The result describes these chronological selections, not the distribution of all waits or the longest passenger pickup delays.

## Geometry finding and limits

Every selected rejection requests a cell on `london-link-067-ab-2`, upstream of Paddington.
That lane and both endpoint positions are identical in the earlier and mirrored projects.
Its straight length is 314.166 meters, divided into 11 ordinary track cells before conflict resources are considered.
The selected-lane length therefore does not explain the increased rejection count in mirrored geometry.

This does not isolate another geometric parameter.
Changed station access, downstream routes, certificate turns, and wider traffic can affect the queue on this unchanged lane.
The histories cover selection-to-grant intervals, not complete waits from onset or a controlled geometry intervention.
No geometric or safety-rule change follows from them.

The raw measurement data is in git history.
Concurrent diagnostic runs provide no CPU comparison.
