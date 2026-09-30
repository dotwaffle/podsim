# Pickup route search pruning

The dispatcher evaluates eligible pickup pods in fleet order and keeps the shortest estimated pickup time.
Each candidate can require route searches to several berths at the pickup station.
LondonFull profiling found that these candidate searches dominated demand-triggered stalls.
Computing the final route after choosing a pod took much less time.

The dispatcher now makes one reverse, multi-source Dijkstra search from the pickup station's berths after finding its first feasible candidate.
This gives the minimum free-flow lane time from each network node to any berth at that station.
It ignores berth load, acceleration, and braking.
For a moving pod, the bound also includes its remaining committed route prefix.

A candidate whose bound exceeds the current best pickup time cannot win and needs no full route search.
The comparison leaves a relative rounding margin.
Ties and potentially better candidates still use the original berth selection, route search, and pickup estimate in fleet order.
The simulation caches these bounds by station while its network stays fixed.
Reset retains the cache, Clone starts a new cache, and a graph rebuild clears it.
The cache change has a separate [measurement report](pickup-bounds-cache-performance.md).
No asynchronous worker, dispatch-policy change, or saved-state field is required.

## Controlled measurement

The fixture uses the LondonFull project from before station mirroring, with redistribution enabled and 12 requests per simulated minute.
Each run warms up for ten simulated minutes, then measures another ten minutes, or 36,000 ticks.
Each implementation ran three times on the same machine with the same input.
The junction-priority policy was the same in both implementations.

| Measurement | Before | After |
| --- | ---: | ---: |
| Elapsed time | 6.11-6.13 s | 2.95-2.96 s |
| Median tick | 62.0-62.1 us | 60.6-60.8 us |
| 99th-percentile tick | 446-449 us | 449-451 us |
| Longest tick | 64-71 ms | 17-18 ms |
| Allocated bytes | 2.28 GB | 466 MB |
| Allocation count | About 731,000 | About 481,000 |

The measured interval took about 52% less time and allocated about 80% fewer bytes.
The median and 99th-percentile tick times changed little.
The main improvement was in the expensive route-search ticks.
All six runs produced the same final serialized simulation-state hash.
These short runs do not establish sustained 60x playback or a capacity improvement.

## Correctness checks

Forward searches independently check the reverse lower bounds, including an unreachable node.
A moving-traffic fixture checks that each bound does not exclude a candidate with an equal pickup estimate.
The optimized selector is compared with a full candidate scan for idle and moving pods, with and without congestion routing.
That comparison preserves the complete simulation state except route-cache contents.
Separate cases retain fleet-order ties and candidates inside the rounding margin.

The optimization does not change the existing policy that evaluates candidates with free-flow routes before assigning the selected routing policy.
It does not swap pickup assignments or change routes while a pod is underway.
