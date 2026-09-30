# Berth-wait scan measurements

Testing the wait reason before scanning assigned orders reduced measured process CPU by about 12.3% in this LondonFull probe.
All eight runs produced the same serialized simulation-state hash.

`clearBlockedBerths` previously scanned waiting orders before rejecting pods without a berth blocker.
The reordered condition rejects those pods first.
`assigned` is a pure waiting-list scan.
Eligible arrivals and blockers still read current assignments at the same point in the loop.
Earlier clearing moves can affect those later reads.
The change adds no cache and changes no policy, protocol, or saved state.

## Method and results

The baseline is `c93bc9a`, with reusable route search storage.
Each process generates LondonFull with demand at 12 requests per simulated minute and seed 20260929.
It warms up for one simulated hour, then measures another hour without the live playback clock.
GOGC is 200 and GOMAXPROCS is 2.
Each mode has two repetitions, with candidate order reversed for the second repetition.
No other agent CPU-heavy work runs during measurement.
CPU profiles are off.

Stream mode encodes shared gzip deltas at the modeled equivalent of 20 updates per second at 60x.
It runs no browser, network connection, or persistence writer.
Values below are means of two runs.

| Mode | Baseline CPU | Candidate CPU | CPU reduction | Baseline elapsed | Candidate elapsed |
| --- | ---: | ---: | ---: | ---: | ---: |
| Ticks | 33.162 s | 29.039 s | 12.43% | 32.176 s | 28.082 s |
| Stream | 37.622 s | 32.980 s | 12.34% | 36.215 s | 31.561 s |

The CSV heap field is the sampled maximum Go HeapAlloc, not RSS or post-collection retained heap.
The optimization removes redundant scans, not a substantial source of allocations.
These two repetitions do not establish sustained playback speed or passenger capacity.
The simulation and session test suites and static checks pass.
Independent review found the predicate reorder equivalent because the skipped scan has no side effects.

[Measurements](measurements/berth-wait.csv) retain the means and allocation figures.
[Metadata](measurements/berth-wait.json) records binaries, raw hashes, and the exact final-state hash.
The local artifact bundle is `~/.cache/agents/podsim/berth-filter-20260930/`.
