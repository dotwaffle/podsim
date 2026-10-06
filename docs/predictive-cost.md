# Isolated predictive-routing CPU and allocation costs

The predictive implementation has little measured CPU impact when disabled in these two workloads.
With GC 100, enabling it adds 1.9% to 2.7% median CPU and 4.1% to 5.2% allocated bytes, with identical comparison results.
GC 400 saves about 12% CPU in the AM workload but uses about 2.3 times the peak process RSS.
The server retains its GC 100 default.
These comparison measurements do not establish a live playback-speed limit.

## Workloads and measurement boundaries

The baseline is frozen source `b4cfefe`, and the candidate is `a54281e`.
Both binaries use uninstrumented production source and the same test-only measurement helper.
Each job calls the unchanged production comparison run.
It offers 60 minutes of LondonFull AM12/min seed-4 or Morning14/min seed-1 demand and executes exactly 90 minutes of ticks.
Early drainage does not shorten a run.
Each arm uses free-flow or predictive routing, a queue limit of 1,000,000, and virtual platoons with four pods.
Sharing, buffers, pickup reassignment, and positioning remain off.

Every configuration has three timed repetitions in fresh single-core processes.
The run rotates configuration order within each workload.
The AM workload also compares candidate GC 100 and GC 400.
All jobs use `GOMEMLIMIT=2048MiB`, which can constrain garbage-collector behavior.
The previous owned functional studies have stopped, with empty control groups.
Three separate CPU and allocation profile jobs follow the timed samples.
Their measurements do not enter the timing summaries.

CPU and allocation intervals include fleet construction, simulation steps, snapshots, and comparison reporting.
They exclude project decoding, schedule generation, pre-run collection, and output serialization.
Process peak RSS includes those setup costs.
End-of-run heap values do not measure retained live heap.
Allocation profiles use sampling and include setup history, unlike interval `MemStats` totals.
The production comparison takes full snapshots once per simulated second, which differs from the live server's publication path and cadence.

All 27 jobs pass.
The 24 timed samples execute 324,000 steps each.
Within each workload, all result fields match exactly except the routing label, with identical schedule hashes and tick counts.
The CPU helper does not collect individual request timings or routing-alternative counters.
The [separate service screen](predictive-service.md) retains its own matched-request evidence and zero-alternative finding.

## Repeated measurements

The table reports medians of three timed repetitions.
Allocated bytes cover the full measurement interval, not simultaneous resident memory.

| Workload | Source / routing | GC | CPU, seconds | Allocated, MiB | Peak process RSS, MiB | Collections |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| AM12 seed 4 | Baseline / free-flow | 100 | 27.167 | 8,549.1 | 183.6 | 167 |
| AM12 seed 4 | Candidate / free-flow | 100 | 27.113 | 8,549.1 | 181.4 | 167 |
| AM12 seed 4 | Candidate / predictive | 100 | 27.838 | 8,989.8 | 184.3 | 175 |
| AM12 seed 4 | Candidate / free-flow | 400 | 23.901 | 8,549.1 | 426.5 | 40 |
| AM12 seed 4 | Candidate / predictive | 400 | 24.333 | 8,989.8 | 424.1 | 41 |
| Morning14 seed 1 | Baseline / free-flow | 100 | 44.785 | 12,710.1 | 214.3 | 207 |
| Morning14 seed 1 | Candidate / free-flow | 100 | 44.868 | 12,710.2 | 214.6 | 205 |
| Morning14 seed 1 | Candidate / predictive | 100 | 45.737 | 13,226.4 | 218.0 | 215 |

Disabled-policy median CPU changes by -0.20% in AM and +0.19% in Morning.
The corresponding repetition ranges overlap.
These small differences do not establish a speed improvement.
Enabled prediction adds 2.68% AM CPU and 1.94% Morning CPU.
Its allocated-byte increases are 5.15% and 4.06%.
Identical result fields provide no aggregate service benefit in these cost workloads.

GC 400 reduces candidate free-flow median CPU by 11.85% and predictive CPU by 12.59% in AM.
Peak process RSS increases by factors of 2.35 and 2.30.
This tradeoff does not justify changing the server default.
The [existing live-server GC evidence](admission-live-performance.md) also supports retaining GC 100.
The Morning workload has no GC 400 comparison in this screen.

## Profiles and next candidate

The candidate free-flow allocation profile attributes about 5.36 GiB to lane-slice cloning and 1.12 GiB directly to snapshot construction.
These sampled amounts include setup and do not equal the interval allocation totals above.
Snapshot construction preserves independent returned routes and cannot drop those copies without considering its callers and ownership contract.
The pending and vehicle slices in `a54281e` grow through repeated append allocation.
The matched snapshot comparison reduces allocated bytes by 5.6% to 7.2% with a six-line preallocation change.
It preserves empty nil values and independent snapshot storage.

The CPU profile includes station-phase updates, admission, pickup handling, map lookup, and garbage collection.
Profile percentages are diagnostic samples, not isolated speedups available from changing each function.
No network or browser performance claim follows from these profiles.

The raw measurement data is in git history.
The owned measurement unit stopped successfully.
