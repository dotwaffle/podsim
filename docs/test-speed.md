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

The local `analysis.json` records individual stages and their limits.

## CI test selection

The October 4 CI review found two plain commands that repeated tests of the race task.
The `test:embedded` task ran the full session suite with the `embed_assets` tag.
The session package and its test dependencies compile the same files with and without that tag.
The `qualify` task ran the full scenario suite without the race detector.

The `test:race` task still runs every test.
The `qualify` task now runs only the two Station 19 drain tests and the two emergency choice latency tests of `internal/sim`, which skip under the race detector.
`TestEmergencyNoCandidateLatency` runs only when `PODSIM_QUALIFY=1`, which the `qualify` task sets for its sim command.
`TestEmergencyNoCandidateSearchCounts` checks one tick under every routing policy, cold and warm, on eight stations with 13 berths each.
This small fixture has 160 nodes, 264 lanes, and 52 pods, and runs in normal, short, and race tests.
The `test:embedded` task runs the full root and `cmd/serve` suites with the tag.
In the session package, it runs only `TestPackedTextWireCost`, `TestSaveCapRejectsAtomically`, `TestDecodeCapsAtCallers`, `TestEncodeCapsAtCallers`, and `TestTopologyPreflightAtCallers`.
These tests skip under the race detector and under `-short`.
On October 7, 2026, the maximum codec tests, `TestComposedWorstCaseFormats`, and `TestStreamMaximumEncoding` were deleted with the other worst-case byte proofs.
A new test that skips under the race detector must be added to the `-run` pattern of one of these tasks.
A pattern that matches no test passes.

## CI jobs

On October 4, one Check run took 25 to 34 minutes on a 4-CPU runner.
All eight tasks of `mise run check` ran at the same time on that runner.
The race tests of `internal/sim` took 920 to 1,250 seconds, and those of `internal/session` took 720 to 987 seconds.
Two runs of the same source differed by about 35 percent between Azure regions.

The workflow now runs five jobs on separate runners.
`test:race` depends on every `test:race:*` task: `test:race:sim`, `test:race:session`, and `test:race:other`.
`test:race:other` runs every package except `internal/sim` and `internal/session`, so a new package needs no task change.

On the first warm run, `test:race:sim` took 16 minutes, and every other job took 8.5 minutes or less.
The station and reassignment tests took 439 of 885 seconds of the local race test time, so they formed one of the two sim tasks.
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
Under `-short`, each sim test that calls `skipLong` skips.
At the time of these runs, 53 sim tests called it.
Each of them took one second or more in a serial run without the race detector, and together they took 235.5 of 294.0 seconds.
They are the long scenarios, the parity tests that compare with a full scan or a reference, and the soak tests.
In the session package, the eight session tests of `test:embedded`, `TestStreamLondonWire`, and `TestMaximalRequeueRoundTrip` skip under `-short`.
In `internal/parkride`, six checkpoint tests skip under `-short`.
They took 42.9 of 44.1 seconds in a serial run.
In `cmd/compare`, nine experiment tests skip under `-short`.
They took 19.3 of 25.5 seconds in a serial run.
No CI task passes `-short`.
The race tasks, `test:embedded`, and `test:bounds` still run all of these tests.

| `go test -short -count=1 ./...` | Wall | User CPU | Load (start / end) |
| --- | ---: | ---: | --- |
| Before the sim and session gates | 129.8 s | 1,091 s | 10.8 / 22.1 |
| After the sim and session gates | 64.5 s | 440 s | 8.5 / 22.8 |
| Before the parkride, compare, and other session gates | 79.8 s | 501 s | 12.1 / 21.0 |
| After the parkride, compare, and other session gates | 36.0 s | 322 s | 6.5 / 12.9 |

After the sim and session gates, `internal/sim` took 52.4 seconds instead of 112.8, and `internal/session` took 37.1 seconds instead of 104.6.
`internal/parkride` then set the wall time, at 62.9 seconds.
In the last run, `internal/sim` took 34.1 seconds, `internal/session` 28.0 seconds, and `internal/project` and `internal/scenarios` 22.9 seconds each.
`cmd/compare` took 8.4 seconds.
The longest tests that still run are the two Station 19 drain tests in `internal/scenarios`, at 22.8 and 18.6 seconds.

The full `go test -count=1 ./...` took 188.2 seconds and 1,140 CPU seconds at load 22.8 to 26.9.
After the second set of gates, it took 134.4 seconds and 1,134 CPU seconds at load 12.9 to 18.6.

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

The `internal/sim` race tests ran as two tasks, `test:race:sim-stations` and `test:race:sim-other`, from a split on the pattern `^Test(Reassign|Station)`.
After the station buffer removal and the move of the full no-candidate latency report to `qualify`, a cold-cache CI run on October 7, 2026 took 1 minute for `test:race:sim-stations` and 11 minutes for `test:race:sim-other`.
The two tasks are now one task, `test:race:sim`.
