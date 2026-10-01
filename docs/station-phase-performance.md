# Cached station-phase classification

A private block cache reduces mean simulation replay CPU time by 19.21% in one matched LondonFull workload.
Mean server CPU time decreases by 13.67% in the separate one-client 60x Chrome follow-up.
The change preserves station labels, snapshots, saved states, and policy defaults.
These measurements do not establish a gain for every scenario or browser.

## Change and correctness

The baseline is `24aaa69`, with the previously indexed station-phase lookup.
Live profiles identify repeated station-phase classification as 14-20% of cumulative sampled server CPU.
The calculation repeats while a moving pod remains in the same route block.

Each pod now keeps one private phase result for its current block.
A block change recomputes the result.
Every route replacement clears the cache, including replacements at the maximum route-version value.
The berth branch reads current pod fields each time.
A cache hit writes the phase and station ID again, so stale presentation fields cannot survive.

Repeated lane IDs retain the original first-occurrence lookup behavior.
Clones copy the cache value independently, and physical restore derives the phase from restored movement state.
No cache field enters a project, saved-state file, snapshot, or wire message.
The cache adds no mutable shared storage and performs no additional heap allocation per lookup.

Focused tests cover each role, berth arrival and departure, invalid blocks, route replacement, and an exhausted route-version counter.
The traffic demo compares every snapshot against the uncached calculation at every tick.
Periodic physical restores retain poses, bindings, and saved counters, with phases checked against the uncached calculation.
Existing clone and restore suites retain their assertions.

## Fixed-work replay

Six serial arms use the same physical LondonFull fixture at tick 216000, with 287 pods.
Each arm advances exactly 324000 ticks with the same resumed AM12 demand and random state.
The order is baseline, candidate, candidate, baseline, baseline, candidate.
Go 1.27.1, GOMAXPROCS 4, and GOGC 100 apply to every arm.
No profiler, browser, publication, wall pacing, or file save runs inside the measured loop.
No other owned CPU study runs during measurement.

| Implementation | Repetitions | Mean wall seconds | Mean CPU seconds | Mean allocated GiB | Mean peak RSS, MiB |
| --- | ---: | ---: | ---: | ---: | ---: |
| Baseline | 3 | 29.788 | 30.851 | 2.376 | 158.2 |
| Cache | 3 | 23.856 | 24.925 | 2.376 | 157.9 |

Mean wall time decreases by 19.91%, and CPU time decreases by 19.21%.
Allocation counts and bytes are effectively unchanged.
Peak RSS is the process-lifetime high-water mark, including restore.
The small difference does not establish a memory improvement.
Both implementations generate 1800 total requests, with no skipped offers, by tick 540000.

Nine checkpoints per arm compare hashes of full snapshots, saved simulation state, demand, budget, and random state.
All 54 checkpoint hashes match across implementations.
Each checkpoint checks the simulation contract and physical separation.
The timing arms do not perform every-tick lane-speed checks or physical-restore continuations.
Separate correctness suites cover those behaviors.

## Matched live follow-up

Four serial 90-second arms use the same fixture and the same frozen embedded browser assets.
The order is baseline, cache, cache, baseline.
Each arm runs one local Chrome client at requested speed 60x.
Both implementations use GOMAXPROCS 4 and GOGC 100.
The probe and software-rendering limits match the [current playback matrix](live-playback-qualification.md).
The frozen plan retains some metadata from that earlier matrix.
The published evidence labels that history and identifies the patched candidate.
This follow-up reuses no pilot and runs only local clients.

| Implementation | Repetitions | Achieved speed | Mean server CPU seconds | Mean endpoint RSS, MiB |
| --- | ---: | ---: | ---: | ---: |
| Baseline | 2 | 59.982x | 56.405 | 215.8 |
| Cache | 2 | 59.980x | 48.695 | 217.8 |

Mean server CPU time decreases by 13.67%.
These windows have nearly equal simulated work but small completed-tick differences.
They are wall-paced live measurements, not exact fixed-work CPU comparisons.
All four arms complete 1591 journeys and retain speed 60x, with no speed reduction or client error.
Each client uses one connection, one full snapshot, gzip deltas, and matching receive and submitted-ACK counts.

In the first matched profile, phase classification decreases from 8.36 to 0.87 sampled CPU seconds.
Its cumulative CPU share decreases from 15.04% to 1.81%.
The candidate's cache-miss calculation is included in that 0.87-second total.
These samples support the mechanism but do not replace the measured whole-server comparison.

Endpoint RSS increases slightly in the live mean, while the replay mean decreases slightly.
Neither small difference supports a resident-memory improvement claim.
The cache does not reduce publication size, client drawing work, or the offered passenger load.
Server GOGC 100 and comparison-command GOGC 400 remain unchanged.

## Lookup benchmark

Three 300-millisecond repetitions use the penultimate lane of routes with 16, 64, or 256 lanes.
The unchanged-block median decreases from 38.10-48.13 ns to about 2.90-2.93 ns per update.
Both implementations report zero allocations.
This cache-hit microbenchmark excludes route replacement, block changes, and fixture setup.
It is narrower than the simulation and live-server measurements.

## Evidence

[Measurements](measurements/station-phase-cache.json) retain plans, source and binary hashes, checkpoint hashes, individual arms, and reviewed limits.
The [live arm table](measurements/station-phase-live-arms.csv) and [page table](measurements/station-phase-live-pages.csv) retain transport and client observations.
Raw profiles, scripts, source diffs, and independent reviews remain in the external artifact directories named by the measurement paths.
