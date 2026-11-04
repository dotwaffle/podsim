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

Full snapshots account for 79.45% of sampled allocation, and route cloning for 68.62%.
The study-only snapshot helper keeps the counters and copies only the route context that the station counters need.
The timing trials used a helper outside the repository in the diagnostic overlay.
The production `Snapshot` API retains full routes.
The maintained comparison helper is described below.
The reported combined gain therefore applies to these diagnostic studies.
It is not a claim that the entire normal test suite or server runs 20% faster.

## Results

Sequential one-core trials (Go 1.27.1, `GOMAXPROCS=1`, `GOGC=100`) gave a baseline mean of 409.29 s wall and 407.79 s CPU, against 324.94 s and 324.41 s combined.
The mean peak RSS fell by 5.29%, but two repetitions do not set a memory bound.
All arms matched every result field, schedule, timing, pending record, station report, and check count exactly, and no check or restore assertion was removed.
The route-index lookup is 1.03 times faster at 16 route lanes and 11.59 times faster at 256 lanes, with zero allocations.
These lookup ratios are not whole-simulation ratios.
Independent scenarios can run in separate processes, but functional concurrency gives no comparable CPU timings.
Keep `GOGC=100`; the study does not justify 400.

## Maintained comparison helper

The comparison command now uses `Simulation.MetricsSnapshot` for its counters.
The method copies every non-route field and returns owned route context with lane ID, origin, and destination only.
Moving or unblocked pods retain the terminal lane.
Stopped waiting pods retain the full ordered route topology.
The original `Snapshot` method still returns full route geometry.
Use it for rendering, routing, saves, and safety checks.

Tests cover repeated lanes, empty routes, the stopping threshold, and ownership of the returned context.
All 22 command arms produce identical JSON reports with full and metrics snapshots.
On a synthetic fixture of 287 pods with 128 route lanes each, a metrics snapshot takes 50.77 microseconds per call and a full one 2,194.22.
These numbers measure copying in one synthetic fixture, not whole-test or server speed.
The command parity runs use one repetition per arm and do not establish a repeatable speed percentage.
The raw measurement data is in git history.
