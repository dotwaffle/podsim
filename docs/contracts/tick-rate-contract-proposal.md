# Tick rate contract proposal

Status: proposal, awaiting the maintainer.
Nothing in this document is approved, and no product code changes with it.
`sim.TicksPerSecond` stays at 60 until the maintainer rules.
It was drafted against `8bbae7a`.
Each `file:line` reference is for that tree.
All evidence comes from scratch builds that changed the constant and did not land.

## Summary

| Question | Answer |
| --- | --- |
| Is the separation proof sound at 30 Hz? | Yes. No safety rule needs a change. |
| Is the separation proof sound at 20 Hz? | Yes. No safety rule needs a change. |
| Does a lower rate change the service? | In the compare arms, served orders match by seed at each rate. One short soak-driver arm differs by up to 2.9 percent in one seed. Saturated headway rises by at most 0.7 percent at 30 Hz and 1.3 percent at 20 Hz, on the corridor test. |
| Does a lower rate make `shared-cpu-1x` work? | No. With one client, the server used 13.0, 10.7, and 9.8 percent of a core at 60, 30, and 20 Hz, on the development host, and the quota is 6.25 percent. Publication uses most of this, and it does not depend on the tick rate. |
| What does a lower rate save? | In the compare arms, simulation CPU falls to about 0.6 at 30 Hz and about 0.5 at 20 Hz. In the soak driver, the CPU of each tick does not change, so soak CPU falls to about 0.5 and 0.33. |
| Recommendation | 30 Hz, before the soak, with the patch list of section 5. See section 5.4 for the reason, and for what it does not solve. |

## 1. The separation proof

The proof is sound at both rates.
Each safety bound is a statement about route distance, speed, or geometry, and each `dt` term in the code cancels in the bound.
Sections 1.1 to 1.6 give the argument, section 1.7 gives the check on running code, and section 1.8 says what a longer step loses.

Notation: `a` is the acceleration of 2 m/s², `dt` is 1 / `TicksPerSecond`, `x` is the route distance, `v` is the speed, and `L` is the limit.
`S = x + v²/(2a)` is the stop point of a pod.

### 1.1 Motion kernel

`moveStep` (`internal/sim/move_step.go:15`) is the only motion step.
It computes `safe = sqrt(a²dt² + 2a(L - x)) - a·dt`, which is the root of `c·dt + c²/(2a) = L - x`.
The new position is `x + c·dt` and the new speed is `c`, for a command `c` of at most `safe`.
So the new stop point is `x + c·dt + c²/(2a)`, and it is at most `L` for every `dt`.

The same root gives the braking bound.
If `S` is at most `L`, then `safe` is at least `keepSpeed(v) = sqrt(v² + a²dt²) - a·dt` (`internal/sim/coast.go:248`), and that is at least `v - a·dt`.
So a pod never brakes harder than `a`, and its stop point does not move back, for every `dt`.
The ceiling `v + a·dt` and the lane limit are both at least `keepSpeed(v)`, so they do not break this.
The snap at `L - x < 1e-5` moves a pod by less than 0.01 mm, and it does not depend on `dt`.

`speedBeforeLane` (`internal/sim/traffic.go:873`) has the same form.
It solves `c·dt + c²/(2a) = remaining + limit²/(2a)`, and its scan reach includes `speed·dt`.
A step that crosses the lane start has `c·dt` above `remaining`, so `c` is at most `limit` at the first position on that lane.
This also holds for every `dt`.

### 1.2 The 12 m clearance

A pod stops by the end of its last owned cell, by section 1.1.
A pod keeps a cell until its position is the cell end plus the retention (`releaseDistance`, `internal/sim/traffic.go:948`).
Admission reads the state before movement, and a released resource is free on the next tick.
So when a follower owns a cell, the previous owner is already past the cell end by the retention, and it only moves forward.

The path gap is then at least the retention plus the stopping distance of the follower.
This argument has distance only.
Tails, turns, conflict sections, and the lane gap audit are geometry that `PrepareNetwork` computes, and they do not read `TicksPerSecond`.
The large-class clearance of 20 m follows the same argument.

### 1.3 The low-speed rule

For a qualifying lane, `compactClearance(v) = 5 + 2u`, with `u = v²/(2a)`, and `R = (5 + 2u*)/cos(T/2) - u*` (`internal/sim/retention.go:31`, `:111`).
A follower at speed `v` has its position at least `u` before the cell end, and the pod ahead is at least `R` past it.
With `u` at most `u*`, the straight-line gap is at least `(R + u)·cos(T/2)`.
This is at least `5 + u* + u`, and that is at least `5 + 2u`.

The bound `u*` comes from the lane speed limits (`newLaneRetention`, `internal/sim/retention.go:113`).
Section 1.1 keeps each speed at or below the limit of its lane at each tick, for every `dt`.
So the rule holds as written at 30 and at 20 Hz.

### 1.4 Platoon clearance

`platoonCaps` (`internal/sim/platoon.go:744`) sets `limit = S_leader - clearance`, and a cap of `max(min(own, limit), limit - 0.5·v)`.
The cap uses the state before movement.
A link forms only with `S_follower` at most `limit` (`internal/sim/platoon.go:714`), so `own` is at most `limit` from then on, and the cap is at most `limit`.

The induction has two steps, and neither reads `dt`.

1. After a tick, `S_follower` is at most the old `S_leader - clearance`, by section 1.1.
   The stop point of the leader does not move back, so this is at most the new `S_leader - clearance`.
2. Take the speeds after the tick.
   If the follower is at least as fast as the leader, step 1 gives a path gap of at least the clearance.
   If the follower is slower, the gap grew in this tick, because each pod moved its new speed times `dt`.

`platoonReactionSeconds` is in seconds, and `linkClearance` is geometry plus a margin of 0.01 m.
The rounding in this proof is near 1e-12 m, so the margin still holds.

### 1.5 Depots and bays

A depot chain lane uses the rule of section 1.3, with a pitch of 6 m and a speed from `DepotSpeed` (`internal/sim/depot_network.go:58`).
A lateral pair uses `lateralClearance`, which is 3.1 m plus `v²/2`, at the higher speed limit of the pair (`internal/sim/depot_lateral.go:33`).
The sweep of a lateral pair and the maneuver sweep of a bay run at network preparation, over continuous geometry, with no time term.
The reversal dwell is `TicksPerSecond` ticks, which is 1 s at each rate.

### 1.6 Faults

`faultMoveStep` runs the same kernel with the limit `min(limit, faultCap)`, and the cap is the stop point at onset.
So section 1.1 covers it.
Debris and a stopped pod block by resources, which section 1.2 covers.

### 1.7 Check on running code

A scratch oracle ran after each tick, in a `cmd/compare` build, on LondonFull for 30 simulated minutes with depots.
It ran the KSX flood and the am-peak profile, each with virtual platoons, and it ran the profile with faults and emergencies at 10 an hour.
The oracle does not share the movement code.
It reads the state and runs `SafetyObservation.Check`.
It compares the stop point with the reservation end and with its previous value.
It compares the speed with the lane limit, the speed change with `a·dt`, and each link with its two margins.

| Rate | Runs | Ticks | Link ticks | Violations | Smallest gap | Smallest link stop margin | Smallest link path margin | Largest overrun | Speed over limit | Largest backward move | Largest braking | Largest step |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 60 Hz | 7 | 756,000 | 251,781 | 0 | 6.000 m | 0.000 m | 0.000 m | 3.6e-12 m | 0 m/s | 3.6e-12 m | 1.998 m/s² | 0.233 m |
| 30 Hz | 9 | 486,000 | 118,864 | 0 | 6.000 m | 0.000 m | 0.000 m | 3.6e-12 m | 0 m/s | 3.6e-12 m | 1.995 m/s² | 0.467 m |
| 20 Hz | 9 | 324,000 | 79,271 | 0 | 6.000 m | 0.000 m | 0.000 m | 3.6e-12 m | 0 m/s | 3.6e-12 m | 1.993 m/s² | 0.700 m |

No run reported a violation.
The smallest gap of 6.000 m is the depot slot pitch, and it is the same at each rate.
The link margin reaches 0 and does not pass it, at each rate, so the bound is tight in the same way at each rate.
Each overrun and each backward move is near 4e-12 m, which is rounding.

The physical test family also gives evidence.
The Express, Large, and lane-speed motion tests have independent checks of body gap, owned stopping, and speed change.
They fail at 30 and 20 Hz only because of a `2.0/60` literal in the test.
With the literals changed to `TicksPerSecond` in scratch, all of these tests pass at both rates, without `-short`.
The reversal dwell tests also pass with this change.
The soak driver also ran with its every-tick checks (`CheckContract`, the transition rules, and `Check`) at each rate, as section 4.4 records.

### 1.8 What a longer step loses

No item below breaks a bound.

| Item | Code | 60 Hz | 30 Hz | 20 Hz | Effect |
| --- | --- | ---: | ---: | ---: | --- |
| Largest travel in one tick at 14 m/s | `move_step.go:21` | 0.233 m | 0.467 m | 0.700 m | Measured. The shortest lane is at least 2R meters, and a step can cross more than one cell, so this stays safe. |
| Observation between two ticks | `safety.go:33` | 2.3 mm | 9.1 mm | 20.4 mm | `Check` sees only ticks. This is the most that the gap of two pods can dip between two ticks at a closing speed of 28 m/s, at a 12 m gap. No proof depends on it. |
| Reservation lookahead | `simulation.go:16` | 33 ms, 0.47 m | 67 ms, 0.93 m | 100 ms, 1.40 m | Two ticks of travel at 14 m/s. A pod asks for track earlier, and it holds that track longer. See decision 3. |
| Handover delay | `simulation.go:903` | 17 ms | 33 ms | 50 ms | A released resource is free on the next tick. |
| Time resolution of records and estimates | `emergency_choice.go:199`, `:259` | 17 ms | 33 ms | 50 ms | More emergency station choices tie on a tick, and the station order then decides. Compare intervals truncate to a whole tick, so a 6.666667 s interval is 6.650 s at 20 Hz. |
| Coast release | `coast.go:156` | 17 ms | 33 ms | 50 ms | One more tick of margin. |

The corridor test shows the sum of these effects at saturation (`TestMergeCorridorHeadway`, `TestMergeCorridorPlatoonHeadway`).
These tests pin the values at 60 Hz, so they fail at both lower rates.

| Case | 60 Hz | 30 Hz | 20 Hz | 30 Hz change | 20 Hz change |
| --- | ---: | ---: | ---: | ---: | ---: |
| One stream through a 30 degree merge, no platoons | 7.217 s | 7.233 s | 7.200 s | +0.2% | -0.2% |
| One stream through a lane boundary, no platoons | 7.217 s | 7.233 s | 7.200 s | +0.2% | -0.2% |
| Straight lane, no platoons | 6.011 s | 6.017 s | 6.025 s | +0.1% | +0.2% |
| Two streams, 15 degree merge, no platoons | 10.783 s | 10.800 s | 10.800 s | +0.2% | +0.2% |
| Two streams, 30 degree merge, no platoons | 8.650 s | 8.633 s | 8.650 s | -0.2% | +0.0% |
| Two streams, 90 degree merge, no platoons | 8.650 s | 8.633 s | 8.650 s | -0.2% | +0.0% |
| One stream through a lane boundary, platoons of 2 | 4.120 s | 4.133 s | 4.146 s | +0.3% | +0.6% |
| One stream through a lane boundary, platoons of 4 | 2.517 s | 2.531 s | 2.546 s | +0.6% | +1.2% |
| Straight lane, platoons of 2 | 3.758 s | 3.771 s | 3.782 s | +0.3% | +0.6% |
| Straight lane, platoons of 4 | 2.415 s | 2.431 s | 2.446 s | +0.7% | +1.3% |
| Two streams, 15 degree merge, platoons of 2 | 6.017 s | 6.017 s | 6.025 s | +0.0% | +0.1% |
| Two streams, 15 degree merge, platoons of 4 | 3.511 s | 3.523 s | 3.534 s | +0.3% | +0.7% |
| Two streams, 30 degree merge, platoons of 2 | 4.925 s | 4.942 s | 4.938 s | +0.3% | +0.3% |
| Two streams, 30 degree merge, platoons of 4 | 2.991 s | 3.003 s | 3.016 s | +0.4% | +0.8% |
| Two streams, 90 degree merge, platoons of 2 | 5.014 s | 5.023 s | 5.031 s | +0.2% | +0.3% |
| Two streams, 90 degree merge, platoons of 4 | 3.131 s | 3.145 s | 3.154 s | +0.4% | +0.7% |

## 2. Test failures by cause

Each test ran in its own process, so a panic stops one test only.
This gets past the panic of the earlier screen.
That panic was `TestReversalDwellResets`, where a literal of 30 ticks is never reached at 30 Hz, and a helper calls `t.Fatal` on the parent test from a subtest.

The run covers all 2,388 top-level tests of the packages that have tests, without `-short`.
A failing test stops at its first failure, so subtest counts are a lower bound.
The count of top-level tests is complete under these conditions.

| Rate | Tests run | Failed |
| ---: | ---: | ---: |
| 60 Hz | 2,388 | 1 |
| 30 Hz | 2,388 | 86 |
| 20 Hz | 2,388 | 90 |

The failure at 60 Hz is `TestEmergencyChoiceLatency`, a wall-clock gate, under the same load.
It passed by itself at each rate.

| Cause | 30 Hz | 20 Hz | Tests |
| --- | ---: | ---: | --- |
| Literal tick counts in the test | 68 | 68 | Table below. |
| Golden and digest pins | 10 | 10 | `TestCurrentStateGolden`, `TestStreamMotionGoldenBytes`, `TestTripBaseline`, `TestIncidentBaseline`, `TestStationFeatureOffSwitchBaseline`, `TestNoRatePlanOutputIsPinned`, `TestNoRateFormsArePinned`, `TestNoRateOutputIsPinned`, `TestFaultSamplerTrace`, `TestEmergencySamplerTrace` |
| Saved-format bound that scales with the rate | 2 | 5 | `TestDecodeStateFileAcceptsLimits`, `TestFaultStartInTicks`. At 20 Hz also `TestBoardingRecordsPhysicalRestore`, `TestExportStateLimitsRoutes`, `TestRestoreClearsInvalidTripBindings`. |
| Wall-clock literal | 3 | 3 | `TestClockCatchUp`, `TestRunStopsTickingAfterClose`, `TestSpeedMonitor` |
| Real behavior change | 2 | 2 | `TestMergeCorridorHeadway`, `TestMergeCorridorPlatoonHeadway` |
| Fixture search for a byte cap | 0 | 1 | `TestEncodeCapsAtCallers`. It fails again by itself at 20 Hz. Its compressed-save case finds no text length that gives the exact cap. I did not find the cause, and this is not a bound failure. |
| Host load | 1 | 1 | `TestPackedTextWireCost` at 30 Hz, and `TestEmergencyChoiceLatency` at 20 Hz. Each failed under a load average above 40, and each passed in a later run by itself. |

| Package | Literal tests | Example |
| --- | ---: | --- |
| `internal/sim` | 26 | 14 Express and 3 Large motion tests (`2.0/60`), `TestGuardedGate`, `TestNextIncidentTick` (`median at rate 60: 1248, want 2496`), `TestFaultDurationTicks`, `TestReversalDwellResets` |
| `internal/session` | 18 | Demand tests that step 3,600 times for a minute, `TestDailyDemandFractionalRestoreAndClone` (`budget != 3599`), five `TestSampler*` tests, four tests on `incidentMemberFile` |
| `cmd/compare` | 8 | Energy tests with 1 J a tick, `TestDailyPositioningBoundary`, `TestRequestTimeStats` |
| `internal/parkride` | 6 | Ledger tests with 60 and 180 ticks |
| `internal/view` | 4 | `TestRunStatus`, `TestCommandResultNotices` (`notice ticks = 90, want 180`) |
| `internal/rail` | 3 | `c.Advance(599, nil)` |
| `internal/project` | 2 | `TestDepartureOfferTickInterpolation` |
| `cmd/parkride` | 1 | `TestDurationAndRequiredOptions` |

I sorted each failed test by the message at its first failing line, and I read the test source where the message was not enough.
For the literal group, only the motion and reversal tests have proof by a scratch fix and a pass.
The others could hide a real change behind the first literal, and only the patch of section 5.1 shows that.
A test that passes can also hide a change, as section 3.1 shows for the two cadences.

## 3. Hard-coded tick values

### 3.1 Product code

| Value | Location | Meaning at 60 Hz | If left at 30 Hz, at 20 Hz | Change |
| --- | --- | --- | --- | --- |
| `emergencyChoiceTicks = 60` | `internal/sim/emergency_choice.go:13` | 1 s | 2 s, 3 s. No test fails. | `TicksPerSecond`, with a contract amendment. |
| `rerouteIntervalTicks = 300` | `internal/sim/endpoint_reroute.go:8` | 5 s | 10 s, 15 s. No test fails. Its comment names the 5 s cadence of `congestionRouteRefreshTicks`, which scales. | `5 * TicksPerSecond`, with a contract amendment. |
| `* 60` in the rail check | `internal/editormodel/check_rail.go:29`, `:55`, `:57`, `:106` | Ticks for each second | The editor checks the same-tick release limit at 60 Hz, and the server checks it at the new rate. No test fails. | `sim.TicksPerSecond`. |
| `60 ticks = 1 second` | `web/offline.js:287`, `:308` | Report text | The text is false. | Change the text. |
| `ebiten.SetTPS(sim.TicksPerSecond)` | `cmd/podsim/main.go:33` | Input and view updates at 60 Hz | Input and view updates at 30 Hz, 20 Hz. | An own view constant of 60. |
| `noticeDuration = 3 * sim.TicksPerSecond` | `internal/view/notice.go:12` | 3 s of view ticks | Correct only while the view rate is the tick rate. | Use the view constant. |
| `wakeBudget = 20 ms` | `internal/session/speed.go:15` | Longer than the wake period of 16.7 ms | The clock runs at most 60 percent, 40 percent of the time. See section 3.2. | Decision 7. |

These values derive from the constant and need no edit: `boardingTicks`, `unloadingTicks`, `defaultReservationLookaheadSeconds`, `congestionRouteRefreshTicks`, `reversalDwellTicks`, `admissionAgeTicks`, `pickupSwapCooldownTicks`, `redistributionCooldownTicks`, `guardedCheckTicks`, `guardedLivenessTicks`, `maxDispatchDeferral`, `incidentHourTicks`, `demandBudgetLimit`, and each `dt` in `move_step.go`, `traffic.go`, and `coast.go`.

`streamCaptureInterval` (`internal/session/stream.go:30`) and the motion delay of 150 ms (`internal/remote/motion.go:12`) are wall-clock values and stay.
The test helper `soakReplayTicks = 600` (`internal/session/incident_soak_test.go:36`) is 10 s at 60 Hz and needs a decision with the soak.

### 3.2 Clock wake budget

This finding is arithmetic, and I did not measure it on a quiet host.
`Session.Run` wakes at the tick rate (`internal/session/session.go:388`), and one wake spends at most 20 milliseconds.
At speed `n`, a wake owes `n` steps at each rate, so the highest speed is 20 ms over the step time.
At 60 Hz the wake period is shorter than the budget, so a core is the limit.
At lower rates the budget is the limit, and the highest live speed does not rise with the lower rate.

With the recorded flood step of 465 µs, the budget gives 43x at each rate.
A full core would give 36x, 72x, and 108x.
So the lower rate gives no higher live speed without a change to the budget or to the wake period.

### 3.3 Saved formats and the protocol

| Item | Effect |
| --- | --- |
| `stateVersion` 10 (`internal/session/state_file.go:29`) | Must change to 11. These members hold ticks: `simulation.tick`, `totalWaitTicks`, `maxWaitTicks`, `totalJourneyTicks`, `maxJourneyTicks`, `nextRedistributionTick`, `phaseTicks`, `requestedTick`, `boardedTick`, `deferUntil`, `deferCheck`, each fault and emergency `start`, `end`, `nextTick`, and counter, the rail `alightedTick`, `demand.budget`, and the checkpoint ticks. |
| Why the version must change | A 60 Hz save has valid numbers at a lower rate, with a different meaning. The restore refuses only a value above a scaled bound, for example `phase 73 is out of range` at 20 Hz. Otherwise it accepts the save with a wrong clock. |
| `StreamVersion` 8 (`internal/session/stream_codec.go:24`) | Must change to 9. The client converts ticks to seconds and to the travel bound (`internal/remote/motion.go:102`) with its own constant. |
| No rate member | The save and the stream stay without a `ticksPerSecond` member, because the rate is a build constant. See decision 5. |
| Project format | No change. No project member holds ticks. Rail times, fault durations, and evacuation times are seconds. |
| `MaxRailRelease` of 200 at one tick (`internal/project/rail_arrivals.go:19`) | Validation can change. Departure windows put more offers on one tick at a lower rate, so a project near the limit can fail at the new rate. Arrivals at whole seconds do not change. All presets validate. |
| Goldens | `internal/session/testdata/state_v10.json` and `state_v10_members.txt` take the new version name. `stream_motion_delta.json` changes. The plain stream goldens pass at both rates, and they change only by the version. |
| Plain byte bounds | `TestPlainStateFileWorstCaseSize` and `TestPlainStreamMaximumEncoding` pass at 30 and at 20 Hz. No bound grows, because a lower rate only lowers tick values, and the proofs use `MaxCounter` widths. |
| Express byte sizes | Express has no byte proof, and this does not change that. |
| Demand budget | The limit falls from 3,599 to 1,799 or 1,199, with the same width of 4 digits. |
| Protocol text | `docs/protocol.md:138`, `docs/protocol.md:641`, and the version text change. No message changes its shape. |

### 3.4 Approved contracts

Each row needs maintainer approval.
This proposal does not change any of them.

| Contract | Text | Lines |
| --- | --- | --- |
| Suspension | `TicksPerSecond = 60` in the constants table | `incident-suspension-contract-proposal.md:188` |
| Suspension | `× 60` in `evacuateTick` and in `end`, and in the overflow rule | `:299`, `:358`, `:368` |
| Suspension | The sampler interval with `216000`, and product choice P2 of 300 ticks | `:1100`, `:717`, `:720`, `:1679` |
| Suspension | Parity at every 600th tick, and a 600-tick replay | `:1422`, `:1518`, `:1612` |
| Emergency | The 60-tick cadence and `% 60` | `incident-emergency-contract-proposal.md:253`, `:328`, `:339`, `:340`, `:756`, `:769`, `:921`, `:1317` |
| Emergency | The sampler interval with `216000`, and parity at every 600th tick | `:970`, `:1263`, `:1430` |
| Stage 7 | The interval with `216000`, and `ceil(seconds × 60)` | `incident-stage7-contract-proposal.md:261`, `:267` |
| Stage 7 | Parity at every 600th tick, and the soak of about 1.06e9 ticks | `:490`, `:523` |
| Service transitions | Parity at every 600th tick | `incident-service-transitions-contract-proposal.md:1563` |
| Express | `60 Hz ticks` | `express-physical-encoding-contract-proposal.md:224` |

Document text outside contracts also names the rate: `docs/model.md:12`, `docs/offline-workflow.md:78`, `docs/design/energy-model-design.md:30`, and the 16.67 ms budget at `docs/london-full.md:256`.
The studies record 60 Hz results, and they stay as records.

## 4. Measurements

All readings are from an AMD Ryzen 5 3600 with 12 threads, and they are not from a quiet host.
Other sessions used the host, and the load average was from 9 to 110 during these runs.
CPU time changes less than wall time under load, but load does change it, so small differences here mean nothing.
The runs for each rate interleave.

### 4.1 LondonFull, compare virtual

Each arm is LondonFull with 327 pods and redistribution on, for seeds 1, 2, and 3.
Each CPU figure is user plus system time for the process, and it includes about 1.4 s to load the project.
KSX is 20 orders a minute to `940GZZLUKSX`, and the profile is the am-peak band at 10 orders a minute.
Peak memory was at most 482 MB, and it does not depend on the rate.

| Arm | Rate | Finished | CPU s by seed | Mean CPU s | Against 60 Hz | Served by seed | Mean wait | Mean journey |
| --- | ---: | ---: | --- | ---: | ---: | --- | ---: | ---: |
| `ksx` | 60 Hz | 3 of 3 | 21.8 / 27.0 / 27.8 | 25.5 | 1.00 | 139 / 167 / 145 | 243.0 s | 891.2 s |
| `ksx` | 30 Hz | 3 of 3 | 13.6 / 17.2 / 17.3 | 16.1 | 0.63 | 139 / 167 / 145 | 243.8 s | 891.4 s |
| `ksx` | 20 Hz | 3 of 3 | 12.0 / 14.1 / 13.0 | 13.0 | 0.51 | 139 / 167 / 145 | 243.8 s | 891.5 s |
| `ksxv` | 60 Hz | 3 of 3 | 23.4 / 29.4 / 30.0 | 27.6 | 1.00 | 138 / 167 / 145 | 243.7 s | 891.7 s |
| `ksxv` | 30 Hz | 3 of 3 | 14.3 / 18.1 / 18.0 | 16.8 | 0.61 | 138 / 167 / 145 | 243.7 s | 891.8 s |
| `ksxv` | 20 Hz | 3 of 3 | 13.0 / 14.3 / 13.3 | 13.5 | 0.49 | 138 / 167 / 145 | 243.7 s | 891.9 s |
| `prof` | 60 Hz | 3 of 3 | 12.3 / 15.2 / 15.7 | 14.4 | 1.00 | 164 / 153 / 166 | 75.9 s | 593.7 s |
| `prof` | 30 Hz | 3 of 3 | 7.7 / 9.8 / 9.9 | 9.1 | 0.63 | 164 / 153 / 166 | 76.0 s | 593.8 s |
| `prof` | 20 Hz | 3 of 3 | 7.6 / 8.2 / 7.3 | 7.7 | 0.53 | 164 / 153 / 166 | 76.0 s | 593.9 s |
| `profv` | 60 Hz | 3 of 3 | 12.1 / 16.4 / 16.4 | 15.0 | 1.00 | 164 / 153 / 166 | 76.3 s | 594.2 s |
| `profv` | 30 Hz | 3 of 3 | 8.4 / 10.3 / 10.3 | 9.7 | 0.65 | 164 / 153 / 166 | 76.4 s | 594.3 s |
| `profv` | 20 Hz | 3 of 3 | 8.3 / 8.3 / 8.5 | 8.4 | 0.56 | 164 / 153 / 166 | 76.4 s | 594.3 s |
| `ksx60` | 60 Hz | 3 of 3 | 36.7 / 37.9 / 36.0 | 36.9 | 1.00 | 344 / 357 / 358 | 272.0 s | 1437.8 s |
| `ksx60` | 30 Hz | 3 of 3 | 22.6 / 22.2 / 21.2 | 22.0 | 0.60 | 344 / 357 / 358 | 272.5 s | 1438.8 s |
| `ksx60` | 20 Hz | 3 of 3 | 18.1 / 16.4 / 16.1 | 16.9 | 0.46 | 344 / 357 / 358 | 272.5 s | 1438.9 s |
| `ksx60v` | 60 Hz | 3 of 3 | 39.6 / 38.8 / 37.0 | 38.5 | 1.00 | 344 / 357 / 357 | 273.7 s | 1439.3 s |
| `ksx60v` | 30 Hz | 3 of 3 | 24.6 / 23.9 / 23.2 | 23.9 | 0.62 | 344 / 357 / 357 | 273.7 s | 1439.3 s |
| `ksx60v` | 20 Hz | 3 of 3 | 18.9 / 17.7 / 17.2 | 17.9 | 0.47 | 344 / 357 / 357 | 273.8 s | 1439.4 s |
| `railhub` | 60 Hz | 3 of 3 | 2.7 / 2.8 / 2.7 | 2.8 | 1.00 | 59 / 59 / 59 | 537.4 s | 834.8 s |
| `railhub` | 30 Hz | 3 of 3 | 1.4 / 1.5 / 1.4 | 1.4 | 0.51 | 59 / 59 / 59 | 537.6 s | 835.0 s |
| `railhub` | 20 Hz | 3 of 3 | 1.0 / 1.0 / 0.9 | 1.0 | 0.36 | 59 / 59 / 59 | 537.8 s | 835.3 s |
| `incprofv` | 60 Hz | 2 of 3 | 158.3 / cut / 103.3 | 130.8 | 1.00 | 162 / 163 | 70.8 s | 595.7 s |
| `incprofv` | 30 Hz | 3 of 3 | 81.5 / 101.2 / 54.9 | 79.2 | 0.52 | 162 / 153 / 163 | 76.3 s | 593.9 s |
| `incprofv` | 20 Hz | 3 of 3 | 58.6 / 67.1 / 38.9 | 54.9 | 0.37 | 162 / 153 / 163 | 76.3 s | 594.0 s |
| `incksxv` | 60 Hz | 0 of 3 | cut / cut / cut | | | | | |
| `incksxv` | 30 Hz | 0 of 3 | cut / cut / cut | | | | | |
| `incksxv` | 20 Hz | 0 of 3 | cut / cut / cut | | | | | |

Names: `ksx` and `prof` have no platoons, and `ksxv` and `profv` have virtual platoons.
`ksx60` and `ksx60v` have 20 minutes of arrivals in 60 simulated minutes.
`railhub` is the rail-hub preset with a 5-minute burst.
`incprofv` and `incksxv` have faults and emergencies at 10 an hour, faults of 600 seconds, a debris share of 0.25, and platoons.

Simulation CPU falls less than the tick count in most of these arms.
I infer that some work runs for each simulated second and does not scale with the rate, for example the sample of the compare command each second.
I did not profile this split.

The earlier one-seed screen at `3544bce` read 33.1, 18.6, and 10.8 CPU seconds on its flood, and 14.8, 8.6, and 6.3 on its profile.
Its flood completed 158 orders, and this `ksx` arm served 139 on seed 1, so the two floods or trees differ, and I do not merge them.

### 4.2 Service

The rates served the same orders in each seed where all rates finished.
Mean wait and mean journey differ by less than 1 percent in each of these arms.
Stopped pod-seconds differ more, by up to 8 percent in one seed, and I treat this as noise in a small count.
These arms do not hold track at capacity for long, and platoon time is below 1 percent.
The corridor table of section 1.8 is the saturated reading.

### 4.3 Live server

A server ran LondonFull with demand at 10 orders a minute, with one scratch headless client.
The client ran the server at 60x for 70 s to fill the network, and it then set the speed in the table.
A reading is the process CPU over 60 s.
The automatic speed reduction lowered the warm-up speed to between 5x and 15x, because of the load, so warm-up states differ.
That reduction also acted in the 15x and 60x rows, and the ticks each second show this.
These two groups of rows do not measure capacity.

| Rate | Speed asked | Client | Share of a core | Peak memory | Load average at the end | Ticks each second | Mean frame gap | Ticks in a frame |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 60 Hz | 1x | One | 13.0% | 191 MB | 21 | 60.0 | 50.8 ms | 3: 86%, 4: 9% |
| 30 Hz | 1x | One | 10.7% | 195 MB | 20 | 30.0 | 55.3 ms | 1: 44%, 2: 46%, 3: 10% |
| 20 Hz | 1x | One | 9.8% | 188 MB | 39 | 19.4 | 65.2 ms | 1: 74%, 2: 26% |
| 60 Hz | 1x | None | 2.8% | 166 MB | 49 |  |  |  |
| 30 Hz | 1x | None | 1.5% | 157 MB | 57 |  |  |  |
| 20 Hz | 1x | None | 1.3% | 176 MB | 56 |  |  |  |
| 60 Hz | 15x | One | 26.3% | 188 MB | 38 | 707.1 | 52.1 ms | 15: 16%, 44: 7%, 45: 20% |
| 30 Hz | 15x | One | 23.8% | 189 MB | 64 | 442.4 | 53.4 ms | 15: 13%, 30: 15% |
| 20 Hz | 15x | One | 20.1% | 187 MB | 37 | 300.0 | 51.0 ms | 14: 5%, 15: 64%, 16: 6% |
| 60 Hz | 60x | One | 20.1% | 189 MB | 86 | 508.3 | 56.7 ms | 6: 15%, 45: 7% |
| 30 Hz | 60x | One | 23.1% | 191 MB | 64 | 491.3 | 53.6 ms | 5: 5%, 10: 8%, 15: 5%, 30: 7% |
| 20 Hz | 60x | One | 20.0% | 189 MB | 35 | 301.1 | 51.8 ms | 14: 12%, 15: 39%, 16: 13% |

The share that does not depend on the rate is publication, which runs on a wall-clock timer.
At 1x with one client it costs about 10 percent of a core at 60 Hz, and more than the simulation.
So a lower rate saves about 2 to 3 percent of a core at 1x.

The quota of `shared-cpu-1x` is [6.25 percent of a core](https://fly.io/docs/machines/cpu-performance), with a finite burst balance.
On these numbers, the server fits with no client at each rate, and it does not fit with one client at any rate.
A `shared-cpu-2x` machine has twice the quota, as far as I know, and I did not confirm this.
These readings come from the development host, and a Fly core or a Raspberry Pi core is a different core.

The ticks in each frame also change at 1x.
At 60 Hz most frames hold 3 ticks.
At 30 Hz they hold 1, 2, or 3 ticks, and at 20 Hz they hold 1 or 2.
The client places a frame by its arrival time, so an uneven tick count can show as uneven motion.
I did not look at the picture, and decision 8 covers this.

### 4.4 Soak driver

This is `TestIncidentSoak` with `PODSIM_SOAK_*` values, at each rate, for seeds 1, 2, and 3.
Runs are short, so that each fits in 5 minutes, and the queue is not full in the runs with checks.
So these rates are comparisons, and they do not replace the rates of [the timing record](../studies/incident-soak.md).

| Preset | Arm | Checks | Simulated hours | Rate | Finished | Ticks for each CPU second | CPU for each simulated hour, against 60 Hz | Check failures | Completed by seed |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| express | reference | Off | 0.5 | 60 Hz | 3 of 3 | 9,198 | 1.00 | 0 | 2 / 12 / 13 |
| express | reference | Off | 0.5 | 30 Hz | 3 of 3 | 9,404 | 0.49 | 0 | 2 / 12 / 13 |
| express | reference | Off | 0.5 | 20 Hz | 3 of 3 | 8,928 | 0.34 | 0 | 2 / 12 / 13 |
| london-central | incidents | On | 0.05 | 60 Hz | 3 of 3 | 740 | 1.00 | 0 | 3 / 3 / 3 |
| london-central | incidents | On | 0.05 | 30 Hz | 3 of 3 | 711 | 0.52 | 0 | 3 / 3 / 3 |
| london-central | incidents | On | 0.05 | 20 Hz | 3 of 3 | 686 | 0.36 | 0 | 3 / 3 / 3 |
| london-central | reference | Off | 0.5 | 60 Hz | 3 of 3 | 4,644 | 1.00 | 0 | 270 / 242 / 254 |
| london-central | reference | Off | 0.5 | 30 Hz | 3 of 3 | 4,512 | 0.51 | 0 | 271 / 235 / 253 |
| london-central | reference | Off | 0.5 | 20 Hz | 3 of 3 | 4,537 | 0.34 | 0 | 271 / 235 / 257 |
| rail-hub | incidents | On | 0.3 | 60 Hz | 3 of 3 | 1,363 | 1.00 | 0 | 37 / 52 / 27 |
| rail-hub | incidents | On | 0.3 | 30 Hz | 3 of 3 | 1,446 | 0.47 | 0 | 37 / 52 / 27 |
| rail-hub | incidents | On | 0.3 | 20 Hz | 3 of 3 | 1,404 | 0.32 | 0 | 37 / 52 / 27 |
| rail-hub | platoons | On | 0.3 | 60 Hz | 3 of 3 | 1,260 | 1.00 | 0 | 38 / 52 / 27 |
| rail-hub | platoons | On | 0.3 | 30 Hz | 3 of 3 | 1,332 | 0.47 | 0 | 38 / 52 / 27 |
| rail-hub | platoons | On | 0.3 | 20 Hz | 3 of 3 | 1,300 | 0.32 | 0 | 38 / 53 / 27 |

The check failure count is the count of contract, transition, safety, and order balance failures in the log.
Seed 3 of each arm with checks misses a Progress gate at each rate, including 60 Hz, because these runs shorten the settle window.
The completed counts match in the rail-hub arms, except one order in one seed.
The `london-central` reference differs by up to 2.9 percent in one seed, with 242 orders at 60 Hz and 235 at both lower rates.
Half an hour is short, so I do not read a trend in this.

Express is a scratch preset: rail-hub, with all lanes, stations, and berths open to `group` and `express`, and with each pod Express.
It completes few orders, so its reading is weak, and the motion tests of section 1.7 are the better Express evidence.
`cmd/compare` refuses an Express project, so the Express arm cannot run in the compare tables.

In these arms, the CPU for each tick does not change with the rate.
So the soak cost follows the tick count, and it falls further than the compare arms do.

### 4.5 What remains unmeasured

- A quiet host.
- A Fly machine, a Raspberry Pi 4, and a Raspberry Pi 5.
  I did not run on these.
- Live speed capacity.
  The 60x readings in section 4.3 are load-limited, so they do not compare the rates.
- The KSX flood with faults and emergencies at any rate.
  This arm did not finish in the 5-minute limit under the load.
- A browser client, a draw of the picture, and the WebAssembly build at another rate.
- `mise run test:web`, and the race tasks at another rate.

## 5. Effort, risks, and recommendation

### 5.1 Patch list

Patches 1 to 5 change nothing at 60 Hz, so they keep each digest, and each can land and pass by itself.
The rate change itself is patch 6.

| # | Subject | Content | Estimate |
| ---: | --- | --- | ---: |
| 0 | `contracts: Amend the rate in the incident and Express contracts` | The amendments of section 3.4, when the maintainer approves them. Text in seconds where a rule means seconds. | 0.5 day |
| 1 | `sim: Express the incident cadences in seconds` | `emergencyChoiceTicks` and `rerouteIntervalTicks`. | 0.1 day |
| 2 | `editormodel: Use the simulation tick rate in the rail check` | `check_rail.go`, and a test that compares the editor with `project` at the tick rate. | 0.2 day |
| 3 | `view: Run the view at its own update rate` | `SetTPS` and `noticeDuration`. | 0.2 day |
| 4 | `session: Scale the wake budget with the wake period` | Decision 7. | 0.3 day |
| 5 | Test patches, by package | The 68 literal tests, the 3 wall-clock tests, and the 5 bound tests, written from `TicksPerSecond`. Run each at the target rate in scratch. | 2 days |
| 6 | `sim: Step at 30 Hz` | The constant, `stateVersion` 11, `StreamVersion` 9, the goldens, the 10 digest pins, the corridor pins, and the report text of `web/offline.js`. | 0.5 day |
| 7 | `docs: Describe the new tick rate` | The text of section 3.3 and section 3.4, and the version text. The docs pass for the slice. | 0.5 day |

The estimates are mine, and I did no such work to test them.
The total is about 4 to 5 agent days, and it is about the same for 30 Hz and for 20 Hz.
Patch 5 is the large part, and it can also show a real change that a first literal hides.

The change stays a build constant.
A rate for each project or each session needs a member in the project, the save, the stream, and each client.
It also gives a second path through each proof.
I do not recommend it.

### 5.2 Risks

| Risk | Rate | Control |
| --- | --- | --- |
| A literal test hides a real change. | Both | Patch 5 runs each test at the target rate before patch 6. |
| An unnoticed literal stays in product code, as `check_rail.go` was. No test failed for the three product literals of section 3.1. | Both | Patches 1 and 2, and a search in review for `60`, `300`, and `600` near ticks. |
| Motion looks uneven. | Both, about equally in the frame counts of section 4.3 | Decision 8. |
| Service and capacity studies at 60 Hz stop being exact baselines. Adoption gates compare results to them. | Both | New studies state the rate. The change to saturated headway is in section 1.8. |
| Input and the map lag if the view rate stays coupled. | Both | Patch 3. |
| The gain falls short, because publication dominates at 1x on a small host. | Both | Decisions 9 and 10. |
| A step of 50 ms equals the latency gate of 50 ms. A tick that takes longer than its period delays the clock at 1x. | Higher at 20 Hz | The gate stays. The worst no-candidate tick is already reported, and it is above any of these periods. |

### 5.3 The incident soak

The approved soak is 200 runs, about 640 CPU hours, and 54 wall hours on 12 threads, all at 60 Hz.

| Question | Answer |
| --- | --- |
| Does a soak at 60 Hz qualify 30 Hz? | No. Each trace differs, and the contracts gate a build. The proof of section 1 lowers the risk, and it does not replace the gate. |
| What does it need at the new rate? | The same 24.5 simulated hours, which is 2,646,000 ticks at 30 Hz. `soakReplayTicks` stays at 10 s, and the parity and replay text moves to seconds. The approved size of about 1.06e9 ticks in stage 7 becomes about 5.3e8 ticks. |
| What changes in cost? | The soak-driver arms cost 0.47 to 0.52 at 30 Hz and 0.32 to 0.36 at 20 Hz, for each simulated hour. By that ratio, the approved size becomes about 320 CPU hours and 27 wall hours at 30 Hz, and about 215 CPU hours and 18 wall hours at 20 Hz. This is a projection from short runs without a full queue with checks. The same ratio applies to the reduced sizes R1 to R3 of the timing record. |
| What about the order? | If the rate is likely to change, run the soak after patch 6. A soak first costs 640 CPU hours that the rate change then discards. |
| Does the work add cost before the soak? | About 4 to 5 agent days, and one maintainer review of the amendments. It does not choose the soak size, and decision 16 of stage 7 stays open. |

Every-tick checks also run fewer times for each simulated hour.
The safety argument does not depend on the count, because section 1 has no time term.
But a soak finds fewer sampled states at a lower rate, and the table does not count this as a loss or as a gain.

### 5.4 Recommendation

Recommendation: change to 30 Hz, and do it before the soak.
The evidence separates 60 Hz from a lower rate.
It does not force the choice between 30 and 20 Hz, and that part is my judgment.

- Stay at 60 Hz costs nothing now, and it keeps every baseline.
  It keeps the soak at 54 wall hours and the simulation at its current CPU, and small hosts have no gain.
- 30 Hz keeps the proof, and it keeps the service within the corridor change of section 1.8.
  It takes most of the simulation CPU saving in the compare arms, it halves the soak, and it leaves room under the 50 ms gate.
- 20 Hz keeps the proof as well, and its patch list is the same.
  Its further saving over 30 Hz is about 10 percentage points of the 60 Hz CPU in the compare arms, and about 1 percent of a core at 1x.
  It cuts the soak further, from about 27 to about 18 wall hours.
  It has a larger headway change, a coarser time resolution, and a step equal to both the publication interval and the latency gate.
  If the aim is the soak cost and the highest playback speed on a small core, 20 Hz is a fair choice, and I would measure it on that core first.

The change does not by itself make a small host work at 1x with a client.
`shared-cpu-1x` stays out of reach at each rate, on these readings, because of publication.
That needs separate work, which decision 10 asks about.

## 6. Open decisions for the maintainer

1. Rate: Stay at 60 Hz, or change to 30 Hz or to 20 Hz.
   Recommendation: 30 Hz.
2. Order: Change the rate before the soak, or run the soak at 60 Hz.
   Recommendation: the rate first, if decision 1 changes it.
3. Reservation lookahead: Keep it at two ticks, or pin it to 33 ms in seconds.
   The rule in `docs/model.md:180` says two ticks, and the measured headways use two ticks.
   Recommendation: keep two ticks.
4. Contract amendments: Approve the changes of section 3.4, with text in seconds where a rule means seconds: a choice each 1 s, a reroute pass each 5 s, and parity and replay at each 10 s.
   Recommendation: approve them, with the cadences kept in seconds.
5. Format: Change `stateVersion` to 11 and `StreamVersion` to 9, with no rate member, and let an older save move aside.
   Recommendation: yes, by the rule of no compatibility until v1.
6. View rate: Run the Ebiten update at a fixed 60 Hz, independent of the tick rate.
   Recommendation: yes.
7. Wake budget: Set the budget to the wake period at each rate, or keep a fixed wake of 60 Hz with the budget at 20 milliseconds.
   The first has fewer wakes, and the second keeps one code path for the timer.
   Recommendation: the budget follows the period, and it stays 20 ms at 60 Hz.
8. Motion check: Look at the map in a browser at the chosen rate before patch 6.
   If the motion is uneven, place frames by tick and not by arrival time.
   Recommendation: look first, and change the client only if needed.
9. Measure on the target: Run the scratch builds on a Raspberry Pi and on a Fly machine, and on a quiet host, before patch 5.
   A Fly machine is a billable resource and needs a grant.
   Recommendation: yes, at least on one Pi and on the quiet host, because the aim of this work rests on these numbers.
10. Publication: Open separate work on publication cost at 1x, for example the capture interval or the gzip level.
    Recommendation: measure it first, because it is about four times the simulation cost at 1x with one client at 60 Hz.
11. Corridor pins: Accept new pins for `TestMergeCorridorHeadway` and `TestMergeCorridorPlatoonHeadway`, with the change of section 1.8 in the study text.
    Recommendation: yes.

## 7. What I did not verify

- The proof of section 1 is a hand proof with a check on running code, and no one reviewed it.
  The oracle saw the states of these runs only.
- The oracle at 60 Hz finished the arm with faults on seed 3 only, under the load.
  Its other arms finished on all seeds.
- The arm with faults has link ticks on seeds 2 and 3, and none on seed 1.
  I did not confirm that a fault hit a pod in a link.
  The soak `platoons` arm has both settings, with its own checks, and I did not confirm that it formed links during a fault.
- Section 2 sorts the literal tests by the first failing message, as it states.
- The cause of `TestEncodeCapsAtCallers` at 20 Hz.
- All items of section 4.5.
- The fixed work that makes simulation CPU fall less than the tick count.
  I infer it, and I did not profile it.
- The Fly quota comes from the Fly documentation page, and I did not test it.
