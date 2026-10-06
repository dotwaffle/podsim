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

## Live Central follow-up

The isolated CPU improvement did not remove the live Central speed reductions.
Seven of eight runs reduced from 60x to 15x, including all four candidate runs.
This result does not establish a live regression or improvement from the filter change.

This follow-up compares the `c93bc9a` server with `7cd5fe9`.
The server behavior change is the berth-wait filter in `1f6cdb2`.
Both servers use the same catalog-cache client assets and paused Central saved state.
The state starts at tick 216000, with demand at 12 requests per simulated minute and seed 20260929.
Each arm uses one headless Chrome client, GOMAXPROCS 4, and a 60-second observation window.
The second repetition reverses the server order.
Other agent CPU-heavy work remains off during measurement.

| GOGC | Baseline playback, repetitions 1 / 2 | Candidate playback, repetitions 1 / 2 | Baseline reductions | Candidate reductions |
| --- | ---: | ---: | ---: | ---: |
| 100 | 35.99x / 35.91x | 35.97x / 35.96x | 2 of 2 | 2 of 2 |
| 200 | 50.30x / 58.59x | 50.29x / 50.27x | 1 of 2 | 2 of 2 |

Every arm passes the protocol checks, with one socket and one initial full snapshot.
Browser receipt-to-ACK p95 spans `14.3-18.0 ms`.
Gzip inflate p95 spans `0.5-0.6 ms`.
The proxy records at most two outstanding updates and about 21 kB of pending binary messages.
These observations do not identify the cause of the speed reductions.

The browser uses software rendering and profile instrumentation, with a local decoding proxy and normal server persistence.
Speed reductions change the number of simulated ticks and the later traffic cohort.
Process CPU totals therefore cannot measure normalized tick cost in this comparison.
Two repetitions are insufficient to attribute the differing GOGC 200 results to the filter.
The server retains its default GOGC 100, and the comparison command retains GOGC 400 when unset.
The overload sensor is unchanged.

[Arm measurements](measurements/berth-wait-live-arms.csv), [client measurements](measurements/berth-wait-live-pages.csv), and [metadata](measurements/berth-wait-live.json) record this follow-up.
