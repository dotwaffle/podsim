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
The bound cache measurements below cover the cache change.
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

## Bound cache measurements

Caching the reverse pickup bounds reduced measured CPU use by 5.7-6.5%.
It removed about 352 MiB of allocations per measured simulation hour.
All 24 runs produced the same serialized simulation-state hash.
The change retains more heap between searches.

### Method

The baseline uses production code from `bb3dae9`.
The candidate uses the cache in `b488de2`.
Both use one fixed test probe and the generated LondonFull preset.
Demand adds 12 requests per simulated minute with seed 20260929.
Each process warms up for one simulated hour, then measures another hour.
Each cell ran twice, with candidate order reversed in the second repetition.

The probe advances 60 simulation ticks per batch without the live playback clock.
The stream mode also creates shared gzip delta messages at the equivalent of 20 updates per second at 60x.
It sends no network traffic and runs no browser.
GOMAXPROCS is 2.
CPU profiles are off during this paired comparison.

The foreground runner stopped after 16 complete arms.
Those arms passed exit and state-hash checks before reuse.
A local user service completed the eight remaining arms, including a rerun of the interrupted arm.
The binary hashes stayed fixed.

### Results

Values are means of two runs.
Heap values are sampled maxima of Go HeapAlloc, not process RSS or live heap after collection.

| GOGC | Mode | CPU reduction | Cached elapsed | Baseline peak heap | Cached peak heap |
| ---: | --- | ---: | ---: | ---: | ---: |
| 100 | Ticks | 6.48% | 29.457 s | 187.6 MiB | 206.1 MiB |
| 100 | Stream | 6.34% | 32.897 s | 191.7 MiB | 209.5 MiB |
| 200 | Ticks | 6.31% | 29.023 s | 281.9 MiB | 312.7 MiB |
| 200 | Stream | 5.70% | 32.443 s | 289.3 MiB | 311.8 MiB |
| 400 | Ticks | 6.09% | 29.096 s | 482.6 MiB | 525.2 MiB |
| 400 | Stream | 5.80% | 32.271 s | 477.6 MiB | 536.3 MiB |

The shared stream produces about 347 kB per wall second at the requested 60x rate.
The final serialized simulation state is identical across all arms.
Total compressed stream bytes vary by less than 0.03% across these runs.
Warmup ends with about 101 MiB of heap after an explicit collection in the cached runs.
The cache can retain up to 12,000,000 bytes of bound arrays at the project node and station limits.
Map storage adds to that bound.

GOGC 200 uses much less peak heap than 400 in this fixture.
Its cached stream elapsed time differs by about 0.5%, with about 2.8% more process CPU time.
The comparison command defaults to GOGC 400 when its environment does not set a value.
The server does not override Go's default of 100.
A server environment can select another value.
Live Chrome measurements should precede a change to either default.
Both defaults remain unchanged.

The raw measurement data of these studies is in git history.
These two repetitions do not establish a statistical performance guarantee, sustained 60x playback, or a capacity improvement.
