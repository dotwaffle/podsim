# Stratford intermediate-berth service tail

Request 1324 waits 5,404.73 seconds longer with pickup reassignment in the [12/min seed-4 comparison](london-full-controller-rate12.md).
Its assigned pod stops at an intermediate Stratford berth while traveling to Leyton.
A captured snapshot shows reciprocal waits between that pod and the berth's arriving claimant.
The request eventually boards another local pod.
No production fix or reservation change lands with this diagnosis.

## Reproduction

The request travels from Leyton to High Street Kensington.
Baseline and reassignment receive the same 4,319 requests over six hours, followed by up to one hour for recovery.
Buffers, sharing, and redistribution remain off.
The other frozen-study settings and validation checks stay unchanged.

Two read-only replays record pending assignments and their observable lane state once per simulated second.
A third candidate replay retains full pod and blocker snapshots when the selected assignment or wait reason changes.
Rider records supply exact boarding ticks and boarding pod IDs.
All original result, schedule, and qualification-check fields match their earlier runs.
The frame replay also matches all preceding tail and boarding observations.
No observer writes to the simulation, owners, or dispatch cursors.

| Measurement | Baseline | Reassignment |
| --- | ---: | ---: |
| Exact request-to-boarding wait, seconds | 481.02 | 5,885.75 |
| Pending boundary samples | 481 | 5,885 |
| Unassigned samples | 0 | 26 |
| Stopped-traveling samples | 0 | 5,564 |
| Berth-occupied samples | 0 | 5,564 |
| Boarding pod | 058 | 216 |

Pod numbers abbreviate `london-pod-NNN` IDs.
Counts are one-second boundary observations, not exact blocked durations.
The request completes in both arms, so its wait increase is exact.

## Binding changes and the captured cycle

Baseline first tracks pod 162, then pod 058, which collects the request.
The candidate first tracks pod 036 heading to Leyton.
At the next observed assignment change, tick 416520, it tracks pod 123.
There is no experimental reassignment record for request 1324.
The ordinary `promoteReadyPickup` path can swap bindings to serve an older same-origin request first.
The observed transition is consistent with that path, but the replay does not capture the exact binding mutation event.

At tick 416520, both captured pods are empty and stopped on `940GZZLUSTD-02-in`:

| Pod | Lane distance, meters | Empty-move destination | Wait reason | Blocked by |
| --- | ---: | --- | --- | --- |
| 123 | 75 | Leyton | Berth occupied | 025 |
| 025 | 50 | Stratford | Pod ahead | 123 |

Pod 123's route enters Stratford berth `940GZZLUSTD-02` before ending at Leyton berth `940GZZLULYN-01`.
Pod 025's route ends at that Stratford berth.
The two recorded waits form a cycle at this captured instant.
The lane observations retain pod 123's berth-occupied wait against pod 025 for 5,564 samples.
They do not record pod 025's complete state at every later sample.

At tick 750345, request 1324 boards local pod 216.
The last pending sample still names stalled pod 123.
The local collection ends the request's wait without establishing how the reciprocal resource wait later clears.

## Route and dispatch implications

The advisory finishing-pod rule never assigns a busy pod and bounds its initial hold to 30 seconds.
It does not explain the long bound interval in this case.
The free-flow `cachedRoute` search does not restrict intermediate berth nodes.
The existing congestion search supplies `ownBerthsOnly`, then falls back to unrestricted free-flow routing if needed.
`cacheStationRoutes` also fills unrestricted free-flow paths for repeated berth searches.
Those paths require consistent treatment in any fix.

Avoiding intermediate berths is a concrete next routing experiment.
It must preserve directed reachability, valid departure and arrival chains, committed route prefixes, and saved physical routes.
Existing projects can contain station arrangements without an independent through route.
A blanket restriction needs tests for those layouts and a stated compatibility rule.
No claim permits removing berth ownership, shrinking reservation groups, or bypassing the occupied berth.

The local [selected-exclusion study](pickup-local-intervention.md) shows useful swaps in other histories.
This case identifies an additional cause of a large indirect regression.
It does not identify which earlier experimental decision first created pod 123's route or the other claimant's state.
Near-flat network backlog can coexist with a long individual resource wait.
Buffers and reassignment remain off by default.

## Evidence

[Case totals](measurements/pickup-seed4-tail.csv) retain exact waits and sampled counts.
[Detailed evidence](measurements/pickup-seed4-tail.json) retains lane episodes, complete route snapshots, blockers, source hashes, and replay outcomes.
Frozen helpers and raw runs remain in `~/.cache/agents/podsim/pickup-seed4-tail-20260930/`.
