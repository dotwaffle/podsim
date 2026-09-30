# Pickup bound cache measurements

Caching the reverse pickup bounds reduced measured CPU use by 5.7-6.5%.
It removed about 352 MiB of allocations per measured simulation hour.
All 24 runs produced the same serialized simulation-state hash.
The change retains more heap between searches.

## Method

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

## Results

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
Further routing and live Chrome measurements should precede a change to either default.
Both defaults remain unchanged.

The [CSV](measurements/pickup-bounds-cache.csv) records the six means.
The [metadata](measurements/pickup-bounds-cache.json) records identities, raw artifact location, and measurement limits.
These two repetitions do not establish a statistical performance guarantee, sustained 60x playback, or a capacity improvement.
