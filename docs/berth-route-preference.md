# Routes without intermediate berths

New simulation routes prefer paths that do not cross an intermediate berth.
Only the exact origin and destination berth nodes can appear on a preferred route.
If no preferred path exists, that destination keeps its unrestricted shortest path.
The public `Network.Route` method retains its original unrestricted behavior.

A Stratford diagnosis of request 1324 in the [12/min seed-4 comparison](london-full-controller-sustained.md#intermediate-rates) captured two empty pods waiting for each other at an intermediate berth.
An arriving pod held the berth ahead of a passing pod, which blocked that arrival.
A separate diversion issue allowed a released passenger-station pod to leave a committed arrival chain.
The fix protects passenger arrival chains with the existing parking commitment rule.
It retains diversion before commitment and preserves current and reserved route prefixes.

## Selection and compatibility

Direct routes and batched station routes use the same preferred paths and tie order.
A batched search stops at every intermediate berth, including another target's berth.
Fallback targets retain a separate unrestricted search tree.
One target's fallback never replaces another target's preferred route.
Congestion and queue routing use the same berth restriction and free-flow fallback.
Station-local arrival and departure searches keep their existing constraints.

Free-berth selection ranks each destination by its own preferred or fallback free-flow cost.
A preferred route is not a global priority tier over all fallback routes.
Guarded redistribution uses those costs for candidate ranking and its inclusive 180-second limit.
It resolves preferred reachability before applying the limit.
A 300-second preferred route cannot activate a 60-second legacy fallback.

A reverse search finds candidate source costs in batches.
Floating-point addition order can change close ties or the reach boundary.
The search checks close contenders forward before selecting a source.
Reverse trees never populate the forward route cache.

Valid authored maps without an independent through path remain reachable through the legacy fallback.
Existing physical saves retain their committed routes, including old routes that cross intermediate berths.
The fix does not repair those routes during restore.
Berth ownership, reservation groups, project files, save versions, wire fields, and policy defaults stay unchanged.

## Validation and search cost

Regression tests cover live and restored arrival-chain boundaries, sibling berths, legacy authored maps, and old committed physical routes.
Independent references check direct, batched, nearest-destination, and nearest-source costs.
Additional regressions reject over-limit redistribution moves and select another eligible candidate.
A synthetic rounding fixture checks the exact forward tie and reach decision.
Directed reverse-tree tests check route continuity and per-target fallback.

A 100-station fixture has 400 berths.
Two 200-millisecond benchmark repetitions use GOMAXPROCS 2 on the local Ryzen 5 3600.
The old unrestricted nearest-destination search takes 0.93-1.03 microseconds and allocates about 5.7 KB per operation.
The preferred search takes 22.6-23.4 microseconds and allocates about 54.7 KB.
The reverse preferred-source search takes 26.0-27.7 microseconds and allocates about 65.1 KB.
These are search measurements, not LondonFull simulation throughput.
Many equal-cost sources can increase the forward shortlist.

An initial scan of cached forward routes costs about 5.4 milliseconds and 5 MB on a cold source check.
It costs about 0.61 milliseconds with a warm cache.
The batched reverse search replaces that scan.

Full plain tests, lint, vet, native and WASM builds, and fresh gopls checks pass.
Race tests pass for sim, session, view, and statestore.
Independent review found no correctness blocker.
The three guarded-selection regressions fail the original selection logic.
The rounding regression fails when forward checks are removed.

## Limits

Legacy fallback paths and existing committed routes can still cross occupied berths.
The fix does not establish complete cycle prevention or sustainable passenger capacity.
Selected matched-request and sustained LondonFull comparisons will assess the changed histories.
Buffers and reassignment remain off by default.

## Stratford request 1324

Source `89e1e1d` includes the route preference and the passenger arrival-chain commitment fix.
It uses the unchanged Full project, AM Peak demand, four-pod virtual platoons, and sharing and redistribution off.
The study queue limit is 1,000,000 and the live queue limit remains 200.
Eight full arms offer six hours of arrivals with a seven-hour cap.
They cover 12/min seeds 1 and 4 with baseline or reassignment, and 14/min seeds 1 and 2 with baseline or both controllers.
Every arm passes the per-second checks and physical restores at three and six hours.

The earlier cached histories use `bab8559`.
A `60d523e` pre-routing witness exactly reproduces the cached 12/min seed-4 reassignment result, so it bridges the diagnosed case only.
The other seven comparisons are cached-source versus post-routing comparisons.
Changed histories measure the combined routing and commitment patch, without separating either component.

Late backlog change counts outstanding orders from hour three to hour six, divided by 180 minutes.

| Nominal rate/min | Seed | Policy | Cached late growth/min | New late growth/min | New completed / accepted | New pending / aboard |
| ---: | ---: | --- | ---: | ---: | ---: | ---: |
| 12 | 1 | Baseline | -0.006 | -0.056 | 4,317 / 4,319 | 0 / 2 |
| 12 | 1 | Reassignment | -0.044 | -0.083 | 4,317 / 4,319 | 0 / 2 |
| 12 | 4 | Baseline | -0.100 | -0.078 | 4,319 / 4,319 | 0 / 0 |
| 12 | 4 | Reassignment | -0.094 | -0.094 | 4,319 / 4,319 | 0 / 0 |
| 14 | 1 | Baseline | -0.050 | +0.000 | 5,040 / 5,042 | 0 / 2 |
| 14 | 1 | Both | -0.044 | -0.039 | 5,041 / 5,042 | 0 / 1 |
| 14 | 2 | Baseline | +1.683 | +0.139 | 5,039 / 5,042 | 0 / 3 |
| 14 | 2 | Both | -0.028 | -0.011 | 5,040 / 5,042 | 0 / 2 |

The 14/min seed-2 baseline completes 5,039 requests, compared with 5,017 in the cached history.
Its late backlog growth decreases from 1.683 to 0.139 orders per minute, still positive, with three parties aboard at the cap.
The separate 12/min Stratford case does not identify the cause of this improvement.
These finite windows do not establish indefinite capacity or cover other demand bands.

Controller comparisons use the new-source baseline.
Exact deltas include only requests that board in both arms, and a negative mean means the controller arm boards them sooner.

| Nominal rate/min | Seed | Exact mean delta, seconds | Requests over 300 seconds slower | Largest increase, seconds |
| ---: | ---: | ---: | ---: | ---: |
| 12 | 1 | -2.52 | 106 | 1,520.42 |
| 12 | 4 | -3.22 | 113 | 1,629.73 |
| 14 | 1 | -22.77 | 186 | 1,557.60 |
| 14 | 2 | -48.02 | 179 | 1,525.55 |

Better matched means coexist with individual increases of about 25-27 minutes.
Neither these deltas nor the controller means establish an individual-service guarantee.
Buffers and reassignment stay off by default.

The original request 1324 waited 481.02 seconds in baseline and 5,885.75 seconds with reassignment.
The new waits are 446.30 and 664.10 seconds, so reassignment still adds 217.80 seconds.
A read-only frame replay exactly matches the new result.
Request 1324 boards pod 112 at tick 437046, on a pickup route from Tower Hill to Leyton that contains only the Leyton destination berth.
The replay records 664 pending boundary samples: 29 unassigned, one stopped departure sample, and no berth-occupied samples.
There is no direct reassignment record for request 1324.
The earlier pod-123/pod-025 Stratford cycle does not appear in the new history.
This does not establish cycle prevention for every pod, legacy fallback, or previously saved committed route.
The changed fleet history also prevents attributing this result to one earlier dispatch decision.

The raw measurement data of these studies is in git history.
