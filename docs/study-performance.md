# Faster diagnostic study replays

The optimized diagnostic helper reduces mean replay wall time by 20.61% and CPU time by 20.45% in one matched LondonFull workload.
Every result field, schedule, and check output remains identical.
The production change replaces a repeated route scan with the existing route index.
The study helper also copies only the route context that its station counters need.

## Profile and changes

The profile uses frozen source `e285e69` and LondonFull AM15 seed 3.
It offers six hours of demand and permits a seven-hour cutoff with the existing 287-pod project.
Sharing, buffers, pickup reassignment, and positioning stay off.
It uses free-flow routing and virtual platoons of four.

Station-phase updates account for 9.91% of sampled CPU time.
The old code scans the route to find the current lane's first occurrence.
The replacement uses the existing first-block lookup and block index.
Repeated lane IDs retain the original first-occurrence behavior.
An unindexed fixture retains its existing fallback.
The change adds no cache, public API, or saved field.

Full snapshots account for 79.45% of sampled allocation space.
Route cloning accounts for 68.62%.
These are cumulative allocation samples, not resident memory.

The study-only snapshot helper retains all ordinary snapshot counters, pods, riders, stops, and pending requests.
Moving pods retain their route's terminal node for the approach count.
Stopped waiting pods retain every lane's ID, origin, and destination for entrance and exit classification.
Station-counter tests cover the stopping threshold, waits, missing lane IDs, and each route lane.
Ownership tests check that mutations cannot affect simulation storage.

The timing trials used a helper outside the repository in the diagnostic overlay.
The production `Snapshot` API retains full routes.
The maintained comparison helper is described below.
The reported combined gain therefore applies to these diagnostic studies.
It is not a claim that the entire normal test suite or server runs 20% faster.

## Sequential matched replay

All timing arms run sequentially with Go 1.27.1, `GOMAXPROCS=1`, `GOGC=100`, and `GOMEMLIMIT=1536MiB`.
Each has a one-core CPU quota and a 4 GiB process-group limit.
No other owned CPU study runs during these comparisons.
The timing trials have no profiler enabled.
Their order retains two baseline and two combined repetitions, with one separate phase-index diagnostic.

| Trial | Changes | Wall seconds | CPU seconds | Peak RSS, MiB |
| --- | --- | ---: | ---: | ---: |
| Baseline 1 | Original | 433.04 | 430.48 | 295.0 |
| Combined 1 | Phase index and study snapshot | 334.43 | 333.65 | 292.2 |
| Phase only | Phase index | 360.83 | 360.62 | 327.1 |
| Combined 2 | Phase index and study snapshot | 315.44 | 315.16 | 293.0 |
| Baseline 2 | Original | 385.54 | 385.10 | 323.0 |

Baseline means are 409.29 wall seconds and 407.79 CPU seconds.
Combined means are 324.94 and 324.41 seconds.
The mean peak RSS decreases by 5.29%, but two repetitions do not establish a memory bound.
The phase-only result is one diagnostic repetition and does not establish a repeatable whole-server percentage.
The range between baseline repetitions also limits precision.

All five arms match every result field, schedule, accepted request, timing, pending record, station report, diagnostic, and check count exactly.
Each retains 25,200 ordinary safety/conservation samples.
Physical restores at three and six hours retain poses, bindings, admission ages, and saved platoon records.
Each restored copy passes 3,600 dense continuation checks.
The corrected helper also passes a short every-tick pilot against production aggregates.
No simulation steps, checks, or restore assertions were removed.

## Route-index benchmark

The isolated lookup benchmark uses the penultimate lane of routes with distinct lane IDs.
It excludes fixture setup and reports the median of five 300 ms repetitions.

| Route lanes | Original ns/op | Indexed ns/op | Speed ratio |
| ---: | ---: | ---: | ---: |
| 16 | 40.14 | 38.85 | 1.03 |
| 64 | 229.80 | 43.58 | 5.27 |
| 256 | 592.60 | 51.11 | 11.59 |

Both versions allocate zero bytes per lookup.
The benefit grows with route length.
These lookup ratios are not whole-simulation speed ratios.

## Snapshot allocation benchmark

A synthetic fixture compares full snapshots with the diagnostic helper.
It contains 287 moving pods with 128 route lanes each, including 32 curves.
The median of five 300 ms repetitions gives these results:

| Snapshot | Microseconds per call | Bytes per call | Allocations per call |
| --- | ---: | ---: | ---: |
| Full | 1,614.16 | 4,957,736 | 9,475 |
| Study context | 37.48 | 140,728 | 291 |

This fixture measures snapshot copying only.
It does not step the simulation or establish whole-workload CPU, memory, or throughput gains.
Stopped waiting pods can retain more route context than these moving pods.

## Parallel studies and evidence

A single simulation preserves sequential tick and dispatch order.
Independent scenarios can run in separate processes.
The functional diagnosis matrix used multiple workers, while performance comparisons used one sequential worker.
Functional concurrency does not supply comparable CPU timings.

[Measurements](measurements/study-performance.json) retain all timing repetitions, exact-output checks, benchmark samples, runtime settings, and source/binary identities.
The full plain suite, vet, lint, native build, WASM generation, and embedded-asset checks pass.
Simulation and session race checks also pass.
The source stays unchanged during the final gates.
Completed timing units terminated successfully and their scratch binaries were removed.
Keep `GOGC=100`.
This study does not establish a new optimal GC setting or justify adopting 400.

## Maintained comparison helper

The comparison command now uses `Simulation.MetricsSnapshot` for its counters.
The method copies every non-route field and returns owned route context with lane ID, origin, and destination only.
Moving or unblocked pods retain the terminal lane.
Stopped waiting pods retain the full ordered route topology.
The original `Snapshot` method still returns full route geometry.
Use it for rendering, routing, saves, and safety checks.

Topology tests cover repeated lanes, empty routes, and the exact stopping threshold.
Ownership tests mutate pending requests, riders, stops, berth records, pods, and route context without changing simulation storage.
Dense replays compare station, network, and comparison counters with full snapshots for single-party and shared rides.
All 22 command arms produce identical complete JSON reports with full and metrics snapshots.
They cover a small ring with buffer/swap/platoon combinations, rail-hub bursts, LondonCentral, and LondonFull.

A separate five-repetition benchmark uses the same synthetic fixture with `GOMAXPROCS=1` and `GOGC=100`.
Its median results are:

| Snapshot | Microseconds per call | Bytes per call | Allocations per call |
| --- | ---: | ---: | ---: |
| Full | 2,194.22 | 4,957,736 | 9,475 |
| Metrics | 50.77 | 140,728 | 291 |

These numbers measure copying in one synthetic fixture, not whole-test or server speed.
The command parity runs use one repetition per arm and do not establish a repeatable speed percentage.
[Maintained-helper measurements](measurements/metrics-snapshot.json) retain source/binary identities, exact-output hashes, benchmark samples, and validation commands.
