# Test timing and parallel execution

The September 30 audit found a fixed timeout in the session suite.
Its stalled WebSocket test waited for the full production write deadline.
Several independent network timing tests also ran serially.

The production write deadline remains thirty seconds.
A private writer interface lets a test inspect that deadline without waiting for it.
The test checks earlier parent deadlines, cancellation, message forwarding, errors, and child-context cleanup.
Real WebSocket tests still send a legal-size 65 MiB message through TCP backpressure.
They use shorter injected deadlines for slow and stalled readers.
Timing starts at writer entry, after payload allocation.

Independent network timing tests now run in parallel.
Each test owns its session, server, connections, and contexts.
Large encoding fixtures and the heap-bound state-file test remain serial.

## Measurements

These plain package runs used Go 1.27.1, `GOMAXPROCS=4`, and `-parallel=4`.
The machine has an AMD Ryzen 5 3600 processor.
Each run used `-count=1` with CPU, mutex, and block profiles.
These are individual observations, not confidence intervals.
Elapsed times for parallel tests overlap and must not be added.

| Session suite | Elapsed seconds |
| --- | ---: |
| Original deadlines and serial network tests | 49.868 |
| Shorter injected test deadlines | 22.308 |
| Parallel network tests, final source | 16.733 |

The final observation is about 66% shorter than the original run.
The original stalled-reader case took 30.03 seconds.
No production timeout or protocol changed.

The sim suite took 28.461 seconds.
Its CPU profile sampled 110.36 CPU seconds over 28.37 seconds, almost four active cores.
The mutex profile recorded about 52 milliseconds of aggregate contention.
This run does not support a global-lock explanation for sim's duration.
Its longest individual tests were guarded invariants and platoon release after reassignment.

Immutable network preparation accounted for about 2.4% of sim's sampled CPU.
Prepared-network reuse already avoids repeated geometry preparation in frequent restore checks.
London presets already use `sync.Once` and return detached project copies.
The audit did not add a general fixture cache or share mutable simulation state.

## Validation and evidence

The full session suite, session and remote race checks, static checks, and focused repeated deadline tests passed.
An independent review checked deadline coverage and fixture ownership.
The final deadline tests also passed five repetitions under the race detector.

Raw timing events, profiles, source hashes, and validation logs remain in `~/.cache/agents/podsim/test-speed-20260930/`.
The cache's `analysis.json` records individual stages and their limits.

## CI test selection

The October 4 CI review found two plain commands that repeated tests of the race task.
The `test:embedded` task ran the full session suite with the `embed_assets` tag.
The session package and its test dependencies compile the same files with and without that tag.
The `qualify` task ran the full scenario suite without the race detector.

The `test:race` task still runs every test.
The `qualify` task now runs only the two Station 19 drain tests, which skip under the race detector.
The `test:embedded` task runs the full root and `cmd/serve` suites with the tag.
In the session package, it runs only the four maximum codec tests, `TestPackedTextWireCost`, `TestComposedWorstCaseFormats`, and `TestStreamMaximumEncoding`.
The first six tests skip under the race detector.
That test does its bounded-scan checks only without the race detector.
A new test that skips under the race detector must be added to the `-run` pattern of one of these tasks.
A pattern that matches no test passes.

## CI jobs

On October 4, one Check run took 25 to 34 minutes on a 4-CPU runner.
All eight tasks of `mise run check` ran at the same time on that runner.
The race tests of `internal/sim` took 920 to 1,250 seconds, and those of `internal/session` took 720 to 987 seconds.
Two runs of the same source differed by about 35 percent between Azure regions.

The workflow now runs six jobs on separate runners.
`test:race` depends on every `test:race:*` task: `test:race:sim-stations`, `test:race:sim-other`, `test:race:session`, and `test:race:other`.
The two `internal/sim` tasks share the `sim_race_split` pattern in `mise.toml`: one runs the matching tests and the other skips them.
`test:race:other` runs every package except `internal/sim` and `internal/session`, so a new package needs no task change.

On the first warm run, `test:race:sim` took 16 minutes, and every other job took 8.5 minutes or less.
The station and reassignment tests took 439 of 885 seconds of the local race test time, so they form one of the two sim tasks.
`check:static` runs the remaining tasks of `check`.
`mise run check` still runs all of them on one machine.

`TestStreamLargeRouteWire` checked 20 publications of the same shape, with 200 pods and 8,000-lane routes.
Now it checks five publications: the first, the sequences where the width changes from 1 to 2, 9 to 10, and 19 to 20 decimal digits, and the largest sequence.
Each publication must have a smaller delta than the legacy HTTP state.
`BenchmarkStreamLargeRouteWire` repeats the publication and reports bytes and encode time for each publication.
On a local race run, the test took 20.1 seconds, compared with 81.8 seconds before.
