# Test timing and parallel execution

The September 30 audit found a fixed timeout in the session suite.
Its stalled WebSocket test waited for the full production write deadline.
Several independent network timing tests also ran serially.

The production write deadline remains thirty seconds.
A private writer interface lets a test inspect that deadline without waiting for it.
The test checks earlier parent deadlines, cancellation, message forwarding, errors, and child-context cleanup.
Real WebSocket tests still send a legal-size 66 MiB message through TCP backpressure.
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
The four maximum codec tests and `TestStreamMaximumEncoding` also skip under `-short`, like the two measurement tests.
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
The station and reassignment tests took 439 of 885 seconds of the local race test time, so they formed one of the two sim tasks.
Since October 6, the coupling tests are also in that task (see the next section).
`check:static` runs the remaining tasks of `check`.
`mise run check` still runs all of them on one machine.

`TestStreamLargeRouteWire` checked 20 publications of the same shape, with 200 pods and 8,000-lane routes.
Now it checks five publications: the first, the sequences where the width changes from 1 to 2, 9 to 10, and 19 to 20 decimal digits, and the largest sequence.
Each publication must have a smaller delta than the legacy HTTP state.
`BenchmarkStreamLargeRouteWire` repeats the publication and reports bytes and encode time for each publication.
On a local race run, the test took 20.1 seconds, compared with 81.8 seconds before.

## Local loop and race split

The October 6 work changed only test files and `mise.toml`.
These runs used one 12-core machine that other jobs also used, with `nice -n 19`.
The load average is given with each wall time, because the load changed the wall times much more than the CPU times.

### Quick loop

`mise run test:quick` runs `go test -short ./...`.
Under `-short`, 53 sim tests skip through `skipLong`.
Each of them took one second or more in a serial run without the race detector, and together they took 235.5 of 294.0 seconds.
They are the long scenarios, the parity tests that compare with a full scan or a reference, and the soak tests.
In the session package, the four maximum codec tests and `TestStreamMaximumEncoding` skip under `-short`.
No CI task passes `-short`, so the race tasks and `test:embedded` still run all of these tests.

| `go test -short -count=1 ./...` | Wall | User CPU | Load (start / end) |
| --- | ---: | ---: | --- |
| Before | 129.8 s | 1,091 s | 10.8 / 22.1 |
| After | 64.5 s | 440 s | 8.5 / 22.8 |

In the run after the change, `internal/sim` took 52.4 seconds instead of 112.8, and `internal/session` took 37.1 seconds instead of 104.6.
`internal/parkride` now sets the wall time, at 62.9 seconds.
The full `go test -count=1 ./...` took 188.2 seconds and 1,140 CPU seconds at load 22.8 to 26.9.

### Soak monitors

`monitorContract`, `monitorReassign`, and `monitorExclusions` checked the state contract after each tick.
`CheckContract` exports the whole state, so the check took about a third of the time of the long monitored tests.
A `contractSampler` now checks at the first observation, every 60 ticks (one simulated second), and at each event.
An event is a command, a reset, a restore, a Step of a paused simulation, or a tick in which a count of the simulation or the phase of a pod changes.
The other checks of these monitors still run at each observation.

The nine slowest monitored tests took 37.9 seconds serially with the per-tick check.
With the sampled check, they took 29.2 seconds at 10 ticks, 22.5 seconds at 30 ticks, 22.2 seconds at 60 ticks, and 21.9 seconds at 120 ticks.
Without the checks at pod and count changes, the nine tests took 23.6 seconds at 60 ticks, so those checks cost almost nothing.

These tests keep the per-tick check through `monitorContractEachTick`, because they test order accounting or short transitions:

- `TestRestoreReportsUnaccountedOrders`
- `TestSharedRidesAccountForEachOrder`
- `TestJoinCensusCountsPartyThatBoardsDuringDwell`
- `TestLegOriginPickupState`
- `TestRestoreLogicalCompletesOnlyAtAStation`

The fault and operational tests that use `checkFaultsEachTick` and `checkEachTick` also keep the per-tick check.
They compare consecutive ticks, and together they take about 2.5 seconds.

### Race split

The `sim_race_split` pattern is now `^Test(Coupling|Reassign|Station)`.
Before the change, the task that ran the station and reassignment tests used 31 percent of the CPU time of the two tasks.
The coupling tests took 17 percent of the per-test race time of the package.
With them, the matching tests take 50.8 percent of that time.
A 4-CPU CI runner is CPU-bound, so the CPU time of each task sets its duration.

| Race task | Pattern | Wall | CPU | Load (start / end) |
| --- | --- | ---: | ---: | --- |
| `test:race:sim-stations` | `^Test(Station\|Reassign)` | 195.5 s | 1,171 s | 10.6 / 14.0 |
| `test:race:sim-other` | `^Test(Station\|Reassign)` | 327.1 s | 2,606 s | 14.0 / 19.1 |
| `test:race:sim-stations` | `^Test(Coupling\|Reassign\|Station)` | 392.0 s | 1,852 s | 22.8 / 23.2 |
| `test:race:sim-other` | `^Test(Coupling\|Reassign\|Station)` | 271.5 s | 1,904 s | 23.2 / 18.2 |

The before runs include the sampled contract check.
The wall times of the after runs are longer because the machine load was higher.
The CPU time of the longer task fell from 2,606 to 1,904 seconds, by 27 percent.

Raw timing events and the scripts remain in `~/.cache/agents/podsim/test-speed-20261006/timing.tar.gz`.
