# Route search storage measurements

Reusing route search storage removed about 2.14 GiB of allocations per measured simulation hour.
Measured CPU reductions range from 0.5% to 4.0% across the six cells.
All 24 runs produced the same serialized simulation-state hash.
The smallest timing differences need more evidence before they count as reliable speed improvements.

## Change and method

The baseline is `b488de2`, which already caches reverse pickup bounds.
The candidate is `c93bc9a`, which also reuses route search arrays and the heap within each simulation.
Public network searches still use separate storage.
Route outputs retain their own lane slices.
Clones and rebuilt graphs discard the search storage.
Reset retains its capacity.
The change does not alter routes, tie-breaking, protocol fields, or saved state.

Each process uses the generated LondonFull preset with demand at 12 requests per simulated minute and seed 20260929.
It warms up for one simulated hour, then measures another hour.
Each cell runs twice, with candidate order reversed for the second repetition.
GOMAXPROCS is 2.
The run waits for the geometry study to finish, and no other agent CPU-heavy work runs during measurement.
CPU profiles are off during these timings.

The probe advances 60 simulation ticks per batch without the live playback clock.
Stream mode also encodes shared gzip deltas at the modeled equivalent of 20 updates per second at 60x.
It sends no network traffic and runs no browser or persistence writer.

## Results

Values are means of two runs.
CPU reduction compares process CPU time with the cache-only baseline.
Heap values are sampled maxima of Go HeapAlloc, not RSS or retained heap after collection.

| GOGC | Mode | CPU reduction | Candidate elapsed | Baseline peak heap | Candidate peak heap |
| ---: | --- | ---: | ---: | ---: | ---: |
| 100 | Ticks | 3.95% | 30.550 s | 208.4 MiB | 206.3 MiB |
| 100 | Stream | 3.08% | 34.414 s | 206.7 MiB | 207.1 MiB |
| 200 | Ticks | 2.72% | 30.308 s | 313.5 MiB | 309.0 MiB |
| 200 | Stream | 1.46% | 34.060 s | 316.2 MiB | 307.9 MiB |
| 400 | Ticks | 2.14% | 30.307 s | 517.3 MiB | 525.0 MiB |
| 400 | Stream | 0.47% | 34.618 s | 536.4 MiB | 527.4 MiB |

The candidate removes about 258,000 allocations per measured hour in each cell.
It saves 2,194 MiB of allocated bytes, with variation of about 1 MiB across cells.
The candidate's stream allocation volume is about 7.52 GiB per measured simulation hour.
These are cumulative allocated bytes, not the memory held at one time.

Stream bytes vary by less than 0.05% despite identical final simulation states.
The modeled compressed rate remains about 347 kB per wall second at 60x.
This rate excludes full baselines and WebSocket/TCP/TLS overhead.
It does not measure a connection's delivered throughput.

## GC tradeoff

GOGC 200 uses about 308 MiB of sampled peak heap in candidate stream mode, compared with 527 MiB at 400.
Its elapsed time is slightly lower, while process CPU time is nearly equal.
GOGC 100 uses about 207 MiB and about 4.6% more process CPU than 200.
These two repetitions support further evaluation of 200, not an unconditional default change.

The comparison command defaults to 400 when its environment does not set GOGC.
The server keeps Go's default of 100 unless its environment overrides it.
Both defaults remain unchanged.
An earlier live Chrome matrix at `c93bc9a` ended at 60x in every zero-client cell, and LondonCentral reduced to 15x with one client at GOGC 100 and 200 and with three clients at GOGC 100.

The measured batch p99 is about 31-32 ms across candidate cells, despite good aggregate unpaced throughput.
That statistic alone does not determine automatic speed reductions.
The live server evaluates recent wall-time buckets.
These measurements do not establish sustained 60x playback or passenger capacity.

The raw measurement data is in git history.
