# Test timing and parallel execution

The September 30 audit found a fixed timeout in the session suite.
Its stalled WebSocket test waited for the full production write deadline.
Several independent network timing tests also ran serially.

The production write deadline remains thirty seconds.
A private writer interface lets a test inspect that deadline without waiting for it.
Real WebSocket tests still send a maximum-size gzip message through TCP backpressure.
They use shorter injected deadlines for slow and stalled readers.
Independent network timing tests run in parallel, and each owns its session, server, connections, and contexts.
Large encoding fixtures and the heap-bound state-file test remain serial.

## Measurements

These were single observations on a Ryzen 5 3600 with Go 1.27.1, `GOMAXPROCS=4`, and `-parallel=4`.

| Session suite | Elapsed seconds |
| --- | ---: |
| Original deadlines and serial network tests | 49.868 |
| Shorter injected test deadlines | 22.308 |
| Parallel network tests, final source | 16.733 |

The final observation is about 66% shorter than the original run.
No production timeout or protocol changed.

The sim suite took 28.461 seconds on almost four active cores, with about 52 milliseconds of mutex contention.
A global lock does not explain its duration.
Immutable network preparation accounted for about 2.4% of its CPU, and prepared-network reuse already avoids repeated geometry preparation.
The audit added no general fixture cache and shares no mutable simulation state.

## CI test selection

The `test:race` task runs every test.
The `qualify` task runs only the two Station 19 drain tests and the two emergency choice latency tests of `internal/sim`, which skip under the race detector.
`TestEmergencyNoCandidateLatency` runs only when `PODSIM_QUALIFY=1`, which the `qualify` task sets for its sim command.
The `test:embedded` task runs the full root and `cmd/serve` suites with the `embed_assets` tag.
In the session package, it runs only the tests of its `-run` pattern, which skip under the race detector and under `-short`.
The plain proofs `TestPlainStateFileWorstCaseSize` and `TestPlainStreamMaximumEncoding` are among them.
A new test that skips under the race detector must be added to the `-run` pattern of one of these tasks.
A pattern that matches no test passes.

## CI jobs

One early Check run took 25 to 34 minutes on a 4-CPU runner, because all eight tasks ran at the same time.
The workflow now runs five jobs on separate runners.
`test:race` depends on `test:race:sim`, `test:race:session`, and `test:race:other`.
`test:race:other` runs every package except `internal/sim` and `internal/session`, so a new package needs no task change.
`check:static` runs the remaining tasks of `check`.
`mise run check` still runs all of them on one machine, one after another.
`TestStreamLargeRouteWire` checks five publications, at the first sequence, the digit-width changes, and the largest sequence.

## Local loop

`mise run test:quick` runs `go test -short ./...`.
Under `-short`, each sim test that calls `skipLong` skips.
These are the long scenarios, the parity tests that compare with a full scan or a reference, and the soak tests.
The session tests of `test:embedded`, `TestStreamLondonWire`, `TestMaximalRequeueRoundTrip`, and nine `cmd/compare` experiment tests skip too.
No CI task passes `-short`, so the race tasks and `test:embedded` still run all of these tests.
The gates cut `go test -short -count=1 ./...` from 129.8 s to 36.0 s of wall time, with the load changing the times more than the CPU times.

The soak monitors check the state contract with a `contractSampler`: at the first observation, every 60 ticks, and at each event.
`CheckContract` exports the whole state, so a per-tick check took about a third of the time of the long monitored tests.
The nine slowest monitored tests fell from 37.9 s to 22.2 s at 60 ticks, and sampling at pod and count changes costs almost nothing.
These runs were at 60 Hz, so 60 ticks were one second.
The sampler now checks every 30 ticks, which is again one second at 30 Hz.
Tests of order accounting or short transitions keep the per-tick check through `monitorContractEachTick`: `TestRestoreReportsUnaccountedOrders`, `TestSharedRidesAccountForEachOrder`, `TestJoinCensusCountsPartyThatBoardsDuringDwell`, `TestLegOriginPickupState`, and `TestRestoreLogicalCompletesOnlyAtAStation`.
The fault and operational tests that use `checkFaultsEachTick` and `checkEachTick` keep it as well.

The `internal/sim` race tests run as one task, `test:race:sim`.
After the station buffer removal, the split into station and other tasks no longer paid off.

## Test work reuse

Traffic tests reuse one snapshot for checks made at the same tick.
Motion tests keep a private pod buffer for each trajectory, and every capture still copies each pod before the next step.
The maximum requeue tests share only an immutable project and encoded seed, and every restore case starts its own session.
The emergency no-candidate latency test prepares its network once, and each case still builds a new fleet and runs all of its warm-up ticks.
Guarded invariants reuse three independently owned maps, and all 90,000 simulated ticks, safety observations, and ownership checks remain.
Route-free snapshots omit only routes that these checks do not inspect.

A combined local validation run can skip the session tests selected by `test:embedded` and the tests selected by `qualify`, and keep their dedicated gates.
Do not add that exclusion to `mise run test` or the race tasks.
