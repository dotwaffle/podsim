# Snapshot allocation comparison

Preallocation reduces allocated bytes by 5.6% to 7.2% in the two selected comparison workloads.
With GC 100, median CPU decreases by about 1.2%.
The change reserves pending-request and vehicle slice capacity before appending each snapshot item.
Empty slices remain nil, and snapshots retain fresh storage and copied routes.
No project, save, wire, or experimental-default contract changes.

## Sources and measurement checks

The baseline is frozen `a54281e`.
The candidate, committed as `2579876`, adds six lines to `Simulation.snapshot` in `internal/sim/simulation.go`.
Every other frozen Go source hash matches.
Both binaries use the same test-only helper and uninstrumented production comparison run.
Explicit absolute overlays produce distinct executable hashes.
Retained assembly verifies both new slice allocations in the candidate.

The first attempt used identical executables despite different source manifests.
Every sample from that attempt is excluded.
The corrected study rebuilds each source and verifies executable hashes before every job.
It also reproduces both executables from the [earlier predictive cost study](predictive-cost.md), preserving that report's provenance.

The corrected study runs three repetitions for each source and configuration.
It reverses source order in the second repetition and changes AM GC order in the third.
Each fresh process uses one core and `GOMEMLIMIT=2048MiB`.
Previous owned studies have stopped, with empty control groups.
Two separate profile jobs do not enter the timing summaries.
All 20 jobs pass.
Within each workload, all timed results, schedule hashes, and tick counts match exactly.

Each job offers 60 minutes of LondonFull demand and executes 90 minutes, or 324,000 simulation steps.
AM uses 12 orders/minute and seed 4 with GC 100 or 400.
Morning uses 14 orders/minute and seed 1 with GC 100.
All arms use free-flow routing, queue limit 1,000,000, and virtual platoons with four pods.
Sharing, buffers, pickup reassignment, and positioning remain off.

CPU and allocation intervals include fleet construction, simulation steps, full snapshots, and comparison reporting.
They exclude project decoding, schedule generation, pre-run collection, and output serialization.
Peak process RSS includes setup costs.
End-of-run heap values do not measure retained live heap.
Allocation profiles use sampling and include setup history.
The comparison takes full snapshots once per simulated second, which differs from live-server publication.
These results do not establish live-server throughput, browser speed, or a 60x playback limit.

## Repeated results

The table shows medians of three timed repetitions.
Allocated bytes cover the full interval and are not simultaneous resident memory.

| Workload | Source | GC | CPU, seconds | Allocated, MiB | Peak RSS, MiB | Collections |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| AM12 seed 4 | Baseline | 100 | 27.200 | 8,549.1 | 185.9 | 167 |
| AM12 seed 4 | Candidate | 100 | 26.886 | 7,932.6 | 183.0 | 154 |
| AM12 seed 4 | Baseline | 400 | 23.907 | 8,549.1 | 429.8 | 39 |
| AM12 seed 4 | Candidate | 400 | 23.995 | 7,932.6 | 416.5 | 36 |
| Morning14 seed 1 | Baseline | 100 | 44.955 | 12,710.3 | 213.4 | 206 |
| Morning14 seed 1 | Candidate | 100 | 44.386 | 11,993.7 | 210.6 | 194 |

AM GC 100 allocates 616.5 MiB less, a 7.21% reduction.
Morning GC 100 allocates 716.6 MiB less, a 5.64% reduction.
Allocated object counts decrease by about 1.4% in both workloads.
GC 100 median CPU decreases by 1.15% in AM and 1.27% in Morning.
The corresponding repetition ranges do not overlap in these samples.
Three repetitions do not establish the same CPU improvement for other workloads.

AM GC 400 retains the allocation reduction but increases median CPU by 0.37%.
Its CPU repetition ranges overlap, so these samples establish no CPU improvement at GC 400.
Peak RSS decreases by about 1.3% to 3.1%, depending on configuration.
The server retains GC 100, supported by the earlier CPU/RSS tradeoff and live-server measurements.

The separate AM allocation profiles attribute about 1.17 GiB directly to baseline snapshot construction and 0.55 GiB to the candidate.
These sampled amounts do not equal the interval allocation totals.
Lane-slice cloning still accounts for about 5.3 GiB in each profile.
The change retains those route copies and the snapshot ownership contract.

## Validation and records

Existing snapshot-isolation, safety-observation, and presentation tests pass.
Independent source, helper, build-provenance, and measurement review finds no blocker.
Full candidate tests, vet, lint, native/WASM builds, and embedded tests pass.
Full sim/session race tests pass, and the validated source hashes remain unchanged.

[Timed samples](measurements/snapshot-allocation-arms.csv) retain CPU, wall time, allocation, GC, heap, and peak RSS fields.
[Repeated summaries](measurements/snapshot-allocation-summary.csv) retain medians and full repetition ranges.
[Metadata](measurements/snapshot-allocation.json) retains source and executable hashes, results, parity checks, and ratios.
Profiles, assembly, helpers, and excluded-attempt records remain under `~/.cache/agents/podsim/`.
The corrected measurement unit stopped successfully, and its scratch executables were removed.
