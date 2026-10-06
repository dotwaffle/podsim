# Admission workspace measurements

Reusing admission storage saves about 805 MiB of allocations per simulated Central hour and 1,284 MiB per Full hour.
Eleven initial case means use 4.2% to 8.7% less process CPU.
A six-pair follow-up supports CPU savings in the remaining case while retaining the original slow run.
The change preserves admission decisions and operating defaults.

## Change and validation

Admission previously created a fresh intent slice and assigned-pickup map on each tick.
The simulation now owns reusable storage for both.
It still collects intents, reads current assignments, assigns priorities, sorts, and grants in the same order.
Each pass clears pod references and map entries before retaining the storage.
Clone and reset discard the workspace.
Restored and prepared fleets allocate their own mutable workspace.
No geometry-derived values remain in retained storage.

The frozen original admission loop provides a full-step state oracle.
Tests cover ordinary traffic, buffers, virtual platoons, both features together, and a corridor that requires actual coupling.
Additional checks cover recursive buffered-head grants, terminal rerouting, stale priorities, large-to-empty reuse, and independent fleet ownership.
Simulation, session, and scenario suites pass, including simulation/session race checks.
Native and WASM builds, vet, lint, and fresh gopls checks pass.
Independent review finds no admission, ownership, or lifecycle blocker.
No project, saved-state, wire, priority, or platoon rule changes.

## Measurement method

The baseline runtime is `42747d5`.
The candidate changes only the private admission workspace.
Both presets generate 12 requests per simulated minute with seed 20260929.
Each process warms up for one simulated hour and measures another hour without the live playback clock.
GOMAXPROCS is 2.
CPU profiles are off.
GOGC values are 100, 200, and 400, with ticks-only and modeled gzip-stream modes.
Each case runs twice, reversing candidate order in the second repetition.
No other agent CPU-heavy work runs during the matrix.

An unrelated Plex transcode uses approximately one of the machine's 12 logical CPUs at launch.
The agent leaves that user process running.
Host contention can affect wall-clock comparisons.
The tables report process CPU for equal simulated work.

Stream mode produces a shared gzip delta every three 60-tick batches.
This models 20 updates per wall-clock second at 60x.
It runs no browser, socket, or persistence writer.
Initial full-frame encoding occurs before timing, followed by an explicit collection.
All 48 arms match their preset's complete final serialized simulation-state digest.
That digest does not prove equivalence at every intermediate tick or for untested workloads.

## Initial paired results

Each row gives means of two processes per variant.
Negative CPU reduction means that the candidate uses more CPU.

| Preset | Mode | GOGC | Baseline CPU | Candidate CPU | CPU reduction | Saved allocations |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| Central | Ticks | 100 | 12.173 s | 11.115 s | 8.69% | 805 MiB |
| Central | Ticks | 200 | 11.689 s | 10.863 s | 7.07% | 805 MiB |
| Central | Ticks | 400 | 11.469 s | 10.699 s | 6.72% | 805 MiB |
| Central | Stream | 100 | 14.809 s | 13.773 s | 7.00% | 802 MiB |
| Central | Stream | 200 | 13.972 s | 13.093 s | 6.29% | 806 MiB |
| Central | Stream | 400 | 13.545 s | 13.765 s | -1.63% | 804 MiB |
| Full | Ticks | 100 | 28.884 s | 27.222 s | 5.75% | 1,284 MiB |
| Full | Ticks | 200 | 28.318 s | 26.892 s | 5.03% | 1,284 MiB |
| Full | Ticks | 400 | 28.112 s | 26.846 s | 4.50% | 1,284 MiB |
| Full | Stream | 100 | 33.764 s | 32.065 s | 5.03% | 1,286 MiB |
| Full | Stream | 200 | 32.515 s | 30.878 s | 5.03% | 1,284 MiB |
| Full | Stream | 400 | 32.063 s | 30.708 s | 4.22% | 1,285 MiB |

Central GOGC 100 collections decrease from 29 to 16 in tick mode and from 51 to 38 in stream mode.
Full GOGC 100 collections decrease from 39 to 26 in tick mode and from 67 to 53 in stream mode.
Sampled peak HeapAlloc does not consistently decrease.
It is neither RSS nor post-collection retained memory.

### Central GOGC 400 follow-up

The original stream pairs have CPU reductions of -8.58% and +5.30%.
Four additional pairs use the same frozen binaries, workloads, and state-digest checks, with alternating order.
All eight added processes pass and retain the same physical-state digest.
Their paired CPU reductions are 5.08%, 5.86%, 6.35%, and 4.98%.

Including all six pairs, baseline CPU averages 13.574 seconds and candidate CPU averages 13.143 seconds.
The resulting mean reduction is 3.17%.
The median paired reduction is 5.19%.
No outlier is removed, and the initial negative row above remains part of the record.
This follow-up supports the allocation change but does not establish a general GOGC recommendation.

## GC setting review

The server retains Go's default GOGC 100.
The comparison command retains GOGC 400 when unset.
Higher GOGC trades fewer collections for more sampled heap in these fixtures.

| Preset | Stream GOGC | Candidate CPU | Sampled peak HeapAlloc | Collections |
| --- | ---: | ---: | ---: | ---: |
| Central | 100 | 13.773 s | 140 MiB | 38 |
| Central | 200 | 13.093 s | 212 MiB | 18 |
| Central | 400 | 13.143 s | 358 MiB | 8 |
| Full | 100 | 32.065 s | 205 MiB | 53 |
| Full | 200 | 30.878 s | 315 MiB | 25 |
| Full | 400 | 30.708 s | 513 MiB | 12 |

Central GOGC 400 uses all six candidate runs here.
Other rows use the initial two runs.
GOGC 200 uses about 40% less sampled heap than 400, with similar stream CPU in these fixtures.
The measured Full CPU difference between 200 and 400 is 0.55%.
These short sequential comparisons do not establish which setting is best for every workload or live browser session.
GOGC 200 merits further testing for memory-limited comparisons.
The existing GOGC 400 comparison setting remains available for historical consistency.
No default change follows from this study.

A separate [live Chrome follow-up](admission-live-performance.md) checks these servers with GC 100 and 400.

## Limits and evidence

Batch latency covers simulation advance only.
Stream construction and compression contribute separately to total CPU and wall time.
The retained means of per-run latency quantiles are not pooled quantiles.
Full p99 batch latency does not consistently decrease despite lower total CPU.
These unpaced measurements do not establish live 60x stability.

Random `serverStart` values can change gzip totals without changing physical state.
Small byte differences do not establish network savings.
Two initial repetitions per case and the targeted follow-up do not qualify all demand rates, seeds, or machines.

[Initial measurements](measurements/admission-work.csv) retain CPU, allocations, collections, pauses, heap, and latency results.
[Per-arm measurements](measurements/admission-work-arms.csv) retain the 48 original processes.
[Metadata](measurements/admission-work.json) records frozen inputs, exact state hashes, and all six follow-up pairs.
