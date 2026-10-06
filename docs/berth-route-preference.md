# Routes without intermediate berths

New simulation routes prefer paths that do not cross an intermediate berth.
Only the exact origin and destination berth nodes can appear on a preferred route.
If no preferred path exists, that destination keeps its unrestricted shortest path.
The public `Network.Route` method retains its original unrestricted behavior.

A Stratford diagnosis of request 1324 in the [12/min seed-4 comparison](london-full-controller-rate12.md) captured two empty pods waiting for each other at an intermediate berth.
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
