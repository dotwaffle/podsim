# Release readiness

Status: release readiness on October 19, 2026.
This report pins `201031792a4c52aad621953b2f55ea897865ec55`, which is on main, as a source that this update read.
At this source, one saved-state version and one stream version serve every project kind.
The local `mise run check` on `43d0e40` passed every task except `TestEmergencyChoiceLatency`, which failed on wall-clock time under host load.
Both emergency latency tests then passed when run alone, in 301 seconds.
The commits after `43d0e40` change only `mise.toml` and documents.
The dependency update `0e842ea` came after this source, and this report does not cover it.
Hosted Check has not run on this source, because the source is not pushed.
No image was published for it.
All four bounded browser performance candidates were rejected and closed.
Physical coupling was removed on October 6, 2026, and the [platoon coupling comparison](platoon-coupling-comparison.md) is the decision record.
This report does not authorize deployment, default adoption, or a cap change.
Deployment, defaults, and caps are unchanged.

The hosted Check results below are for the earlier `0ca0c63a2f19ae32bc5bd0abc8d0fd2ecb81d7ee` source of October 7, 2026.
They do not qualify the pinned source.

## Limits and versions at the pinned source

Each value comes from the constant named here.

| Item | Value | Source |
| --- | ---: | --- |
| Plain fleet limit | 1,000 pods | `MaxPods` in `internal/project/config.go` |
| Plain outstanding-order bound | 8,200 orders | `MaxSavedWaitingTrips` in `internal/sim/state.go` (200 plus 1,000 pods times 8 riders) |
| Express fleet limit | 600 pods | `expressMaxPods` in `internal/sim/order_contract_bounds.go` |
| Express outstanding-order bound | 17,000 orders | `MaxExpressWaitingTrips` in `internal/sim/order_contract.go` |
| Saved-state cap | 128 MiB | `MaxStateBytes` in `internal/session/persist.go` |
| HTTP state cap | 128 MiB | `MaxHTTPStateJSON` in `internal/session/state_http.go` |
| Stream JSON cap | 64 MiB | `MaxStreamJSON` in `internal/session/stream_codec.go` |
| Compressed stream message cap | 64 MiB plus 32,832 bytes | `MaxStreamMessage` in `internal/session/stream_codec.go` |
| Checkpoint cap | 100 MiB | `MaxCheckpointBytes` in `internal/parkride/checkpoint.go` |
| Stations | 600 | `MaxStations` in `internal/project/config.go` |
| Nodes | 12,000 | `MaxNodes` in `internal/project/config.go` |
| Lanes | 20,000 | `MaxLanes` in `internal/project/config.go` |
| Flows in each demand profile | 400,000 | `MaxFlows` in `internal/project/config.go` |
| Project JSON | 32 MiB | `MaxFileBytes` in `internal/project/config.go` |
| Editor import | 43 MiB | `PROJECT_FILE_BYTES` in `web/editor.js` |
| Command body | 16 MiB | `MaxCommandBytes` in `internal/session/http.go` |
| Saved-state version | 9 | `stateVersion` in `internal/session/state_file.go` |
| Stream version | 7 | `StreamVersion` in `internal/session/stream_codec.go` |
| HTTP state media type | `application/vnd.podsim.state-6+json` | `StateMediaType` in `internal/session/state_http.go` |

Without the `express-v1` marker, a pod holds at most 8 riders.
The Express bounds apply only with the marker.

The [measurement record](measurements/release-readiness.json) is unchanged in this update.
It still pins the earlier `8181495` source, its source-specific evidence, and its archived failures.
This update read the pinned source in a detached worktree.
It did not build the source or run tests.
The local check results above come from the run on `43d0e40`.

This source adds the incident, fault, and emergency markers.
The incident redesign stopped at stage 3.
The `64b4f3f` source raised the stream and HTTP state cap to 65 MiB and the compressed message cap to 66 MiB.
Later sources set the caps in the table.

## Hosted checks and image

[Check](https://github.com/dotwaffle/podsim/actions/runs/37633571268) passed in 11m33s, from 14:03:49 to 14:15:22 UTC on October 7, 2026.
All six check jobs concluded with success, and the publish job was skipped.
GitHub Actions caches were cleared before the push, so the jobs ran with a cold cache.
The Check workflow runs six check jobs in parallel on separate hosted runners.
Its publish job needs all six check jobs and does not run for pull requests.
Since `6325cb3`, the publish job runs only for a version tag or a manual run, and a push to main runs the checks only.
The `2140d40` and `31322b5` sources split the check, and `2138790` moved publication into Check.
The later `34088a5` merges the two simulation jobs into one `test:race:sim` job, so the workflow has five check jobs after it.

| Job | Seconds |
| --- | ---: |
| `test:race:sim-other` | 689 |
| `qualify` | 550 |
| `test:race:session` | 501 |
| `test:race:other` | 364 |
| `check:static` | 221 |
| `test:race:sim-stations` | 109 |

The `check:static` job runs bounds, web tests, lint, vulnerability checks, builds, and embedded tests.
The `qualify` job runs the two Station 19 drain tests and the emergency choice latency test.
It also runs the full-limit no-candidate latency test, which runs only when `PODSIM_QUALIFY=1` and so only in this job.
The `069bafb` source gave it a 30-minute test timeout.
The `7c970c2` source reduced the local wall time of `TestScale100Station19BurstDrainsSafely` from 125.3 to 11.7 seconds.
The `b70e46e` source runs 48 session tests in parallel.

The linked Check run published no image for the `0ca0c63` source.
The image of the earlier `7c970c2` source is `ghcr.io/dotwaffle/podsim@sha256:8b60067be3eb2a298c381b033c1438fc6771dc4bb5cb045c25b6a3723b3398b9`.
That image carries no later change and does not qualify the `0ca0c63` source or the pinned source.

The earlier `15047a4` [Check](https://github.com/dotwaffle/podsim/actions/runs/37487384375) passed in 14m24s, from 15:25:37 to 15:40:01 UTC on October 6, 2026.
Its jobs took 864 seconds for `test:race:sim-stations`, 604 for `test:race:sim-other`, and 587 for `test:race:other`.
They took 342 seconds for `test:race:session`, 332 for `check:static`, and 149 for `qualify`.
This report does not record the cache state of that run.

The earlier `7c970c2` [Check](https://github.com/dotwaffle/podsim/actions/runs/37318001608) passed in 14m00s, from 13:35:33 to 13:49:33 UTC, and its publish job took 101 seconds.
Its check jobs took 726 seconds for `test:race:sim-other`, 384 for `test:race:sim-stations`, and 376 for `test:race:other`.
They took 312 seconds for `test:race:session`, 167 for `check:static`, and 44 for `qualify`.

The previous `5078afd` [Check](https://github.com/dotwaffle/podsim/actions/runs/37316095300) passed in 13m55s, from 13:20:57 to 13:34:52 UTC.
Its jobs took 748 seconds for `test:race:sim-other`, 499 for `test:race:other`, and 285 for `test:race:sim-stations`.
They took 247 seconds for `test:race:session`, 211 for `check:static`, and 153 for `qualify`.
The earlier `8181495` Check ran all tasks in one 1,236-second job, and its race task took 1,198.77 seconds.
Its publication ran in a separate workflow that did not wait for Check.
Those results remain archived in the measurement record.
Job durations vary between runs and runners.
The `0ca0c63` run had a cold cache, and the cache state of the earlier runs is not recorded here.
These durations do not establish a causal speedup against earlier runs or qualify a later source.

The audit did not pull or execute any image, inspect an OCI manifest, or test an ARM runtime.
A passing Check does not establish hosted health or deployment readiness.
The existing deployed runtime was not contacted or changed.

## Preserved CI failure and coverage

The earlier `a13bbae` Check failed in `TestStreamSlowWriteBudget/slow_reader`.
It reported `slow write budget 3.926958098s <nil>`.
This was an assertion failure, not a timeout or reported data race.
The old elapsed sample included client reading and result-observation delay after server write completion.

The test repair captures completion time with the writer result.
A compiled caller control passed with a 919.839-millisecond write and result observation after 5.314 seconds.
A mutant that restored the old sample compiled and failed after 5.308 seconds.
The source overlays and original worktree hashes were verified afterward.
The legal maximum-size payload, production 30-second deadline, stalled-reader error, and cancellation controls remain.

The plain `test:bounds` task in `check:static` retains every maximum checkpoint shape assertion.
In the CI split qualification of the measurement record, the earlier 80 MiB guard rejected the 1,265,591,604-byte counting envelope and accepted the 72,104,395-byte fitting envelope.
This update did not measure those sizes again.
Only this serial shape proof excludes race instrumentation in its package.
The `test:embedded` task runs the root and `cmd/serve` suites with embedded assets.
In the session package it runs only the packed text wire cost test, the save cap test, the encode and decode cap tests at their callers, the topology preflight test at its callers, and the two plain byte proofs, `TestPlainStateFileWorstCaseSize` and `TestPlainStreamMaximumEncoding`.
Bounded application tests and concurrency controls remain under the race tasks.
The source audit and qualification retain exact assertion-body comparisons and the prior tool failures.

## Formats and versions

The `248f26e` source collapsed project versions to one version, 1.
A feature is allowed when its fields are present.
Express needs the `orderContract` marker `express-v1`.
The server refuses project versions 2 through 5 and does not migrate them.

Item 7 merged the save and stream families into one family for every project kind.
Every session writes saved-state version 9, and the server reads only version 9 (`9de7a3b`).
Hello 7 is the only stream version (`5f1adac` merged the families, and `aa1f2f0` raised the version from 6 to 7).
`GET /api/state` has one media type, `application/vnd.podsim.state-6+json`, for every project kind.
The contract markers select the optional members and the array bounds of each format.
The order text is packed for every project kind, and no format has a `textEncoding` member.
An intact saved state of version 1 through 8, or of version 10 or later, moves aside with `unsupported_version`.
A damaged or invalid saved state moves aside with `invalid_state`, and the server starts a new session.
A saved state of more than 128 MiB is the only file that the server keeps.
Saving then stops, and startup fails.
A restore counts the waiting orders and the requeued riders together against the queue bound of the contract (`e5933ce`).
The [operations guide](operations.md#session-state) and the [protocol](protocol.md) give the full rules.

The `96bcb9f` source gives every JSON format member a lowerCamel name.
Their decoders match member names by exact case.
Each saved state with the earlier names has a version before 9, so the server moves it aside with `unsupported_version`.
The editor refuses a project file with the earlier names.

The composed worst-case record and the worst-case byte proofs no longer exist.
Two plain proofs remain.
`TestPlainStateFileWorstCaseSize` bounds plain saved-state JSON below the 128 MiB cap.
`TestPlainStreamMaximumEncoding` bounds plain full-stream and HTTP-state JSON below their caps, which are 64 MiB for the stream and 128 MiB for the HTTP state.
Express has no byte proof.
The `test:embedded` task of the `check:static` job runs both proofs.

## Opt-in features since the earlier pin

None of these features changed the saved-state version, the stream version, or the HTTP state media type.
Each one is off unless the project uses it.

- Compact bays (maneuver groups) are off unless the project uses them.
  The `compact-station` preset and both London presets use them, and the `-compact-stations` flag of the scenario command sets the London stations that have them.
  The marker is the network member `maneuverGroups`, which has the digest tag `ext=13` and the `omitzero` JSON tag, so a network without groups writes the same bytes as before.
  The decision record is the README section [Compact bays](../README.md#compact-bays) and the `compact-station` preset section, with the format note in the [operations guide](operations.md).
- Clearance layer A has no marker and no flag.
  It reduces the retention of a straight lane with a speed limit below about 3.74 m/s when the track near the lane qualifies, and every other lane keeps its 12-meter clearance.
  Layer B (close-up following) was measured and not built, as the [close-up measurement](close-up-measurement.md) records.
  The rule is in `internal/sim/retention.go` and the README.
- Queued parking depots are off unless the project uses them.
  Both London presets use them.
  The `-parking-lanes` flag of the scenario command sets them on the ring and London presets, and a custom project can add the network member `depots`.
  The markers are `depots` (`ext=14`, `omitzero`) and the pod member `parkedSince`, which a file writes only for a pod that is idle at a depot slot or moves up in a depot lane.
  The [station banks](station-banks.md#depot-lanes) document and the README section [Parking depots](../README.md#parking-depots) are the decision record.
- The depot lateral rule lets two lanes of one depot sit closer than 12 meters.
  It applies only to depot lane pairs that a swept-body check clears, under the assumption that a guideway keeps each pod on its lane and heading.
  The generated ring depots put the first lane 12 meters from the through lane and each next lane 6 meters further out.
  Debris on a depot lane also blocks the neighbor cells that come closer than 12 meters.
  The [station banks](station-banks.md#depot-lanes) document gives the rule and its assumption.
- The observation and measurement helpers of the editor moved to Go, and the editor offers a measurement overlay and a live station snapshot.
  Both are off until the user selects them, and neither changes the project or any format.
  The README sections [Measure geometry](../README.md#measure-geometry) and [Live station observations](../README.md#live-station-observations) describe them.

The contract documents were amended on October 19, 2026, with maintainer approval.

## Express operating limits

The [Express browser qualification](express-browser-qualification.md) covers the opt-in `express-v1` contract.
The [native](express-native-qualification.md) and [wire](express-wire-qualification.md) records preserve foundation behavior and physical limits.
They do not certify every curve, service workload, browser, or hardware configuration.
Express remains opt-in through the `express-v1` marker.
Its party capacity remains 20 and its service registry limit remains 300.
Its outstanding-order limit is 17,000, and its fleet limit is 600 pods.
The plain fleet limit is 1,000 pods.
In each format, the `express-v1` marker selects bounds of 17,000 waiting or pending orders and 20 riders per pod.
Without the marker, the bounds are 8,200 orders and 8 riders per pod.
Current defaults and physical constants remain unchanged.
The saved-state and HTTP state limits are 128 MiB.
The stream JSON limit is 64 MiB, and the compressed message limit is 64 MiB plus 32,832 bytes.
Manual admission retains its 200-request queue limit.

Physical session restore resets ordinary speed and reconstructs future grants.
It does not preserve an exact trajectory.
The widest encoded assets of the Express qualification exercise independent field shapes, not reachable traffic or unique order conservation.
They kept the earlier 300-pod, 80 MiB basis, and nobody measured them again at the current limits.

On that earlier basis, the widest Chromium run reached 2,760,204 KiB of summed browser process RSS, about 2.63 GiB.
WASM memory reached 2,457,337,856 bytes.
The maximum 20-millisecond heartbeat gap was 14.136 seconds, and HTTP decoding took about 14.43 seconds.
These results establish functional decoding with substantial event-loop blocking, not responsive maximum-size operation.

The wider Node retention test passed with GOGC 100.
It exhausted WASM memory at 4,251,254,784 bytes with GOGC 400.
The failed receipt remains part of the qualification.
No byte limit was raised to avoid it.

## Browser candidates remain unaccepted

The earlier ordinary decoder screen used frozen `3d9534e` source with one LondonFull client, 287 pods, and 12 arrivals per minute.
It ran two 30-second repeats per arm at requested 60x playback using software SwiftShader, local zero RTT, and CDP profiling.
The decoder candidate reduced CPU per message by 3.43% but did not establish a responsiveness gain.
Median arm RAF p95 rose from 50.10 to 58.35 milliseconds, and WASM backing store rose from 238.60 to 247.67 MiB.
The candidate was rejected.

A later white-stamp label candidate failed exact pixel equality in 70 channels, with a maximum channel difference of one.
No appearance exception was granted.
The standalone original-glyph cache passed its bounded pixel controls but failed the ordinary responsiveness screen.
Its paired label CPU reductions were 28.65% and 26.01%.
Median arm RAF p95 was 66.65 milliseconds for both strict and candidate arms.
Median summed long-task time across each 30-second arm rose from 2,147.5 to 2,260.5 milliseconds.

The original-glyph candidate was rejected despite its CPU reduction.
Its maximum-size candidate screen was not run because the ordinary gate failed.
Two software-renderer repeats do not qualify physical GPUs, network delays, or broader workloads.
The strict foundation decoder passed independent native parity and caller controls.
Its native eight-frame median decode time fell from 3.47 to 1.94 milliseconds.
Both browser pairs improved RAF p95 by only 0.1 milliseconds, which failed the frozen precision gate.
The first pair also increased browser RSS.
The standalone strict fusion candidate was rejected.

The final candidate combined strict fusion and the original-glyph cache.
Native suites, race checks, lint, and compiled caller mutations passed.
A fresh private probe passed 360 base pixel cases and 24 surface cases.
The base cases compared 17.28 million pixels with zero changed channels.
Font invalidation and cache bounds also passed.
The performance builds excluded that private probe.

Combined median arm RAF p95 fell from 66.6 to 58.4 milliseconds.
The first paired change was +0.1 milliseconds and failed.
The second was -16.5 milliseconds and passed.
Both pairs passed decoder CPU, label CPU, long-task, WASM backing, RSS, and runtime-error checks.
Acceptance required both paired RAF p95 improvements to exceed 0.1 milliseconds.
The combined candidate was rejected despite its lower median.
No candidate landed, no maximum candidate screen followed, and no unchanged repeat or gate exception occurred.
Browser item 19 closed without an accepted optimization.
These software-renderer screens do not qualify physical GPUs or broader workloads.

## Incident redesign

The maintainer stopped the incident redesign after stage 3.
Stages 1, 2, and 3 landed and are on main.
Stages 4 to 7 were dropped and have no contracts.

- Stage 1 is the [service transitions contract](incident-service-transitions-contract-proposal.md).
  The maintainer approved it on October 5, 2026, in `482d93d`.
  It defines the service withdrawal and pickup release that vehicle faults and rider emergencies share.
- Stage 2 is the [suspension contract](incident-suspension-contract-proposal.md).
  It adds pod faults and debris.
- Stage 3 is the [emergency contract](incident-emergency-contract-proposal.md).
  It adds rider emergencies on ordinary pods.

The `64b4f3f` source applied the stage 1 maintainer decision on the byte budget.
With the stage 1 members at their widest, the widest Express HTTP state, with the coupling members of that time, was 236,415 bytes over the 64 MiB cap.
The commit raised the stream and HTTP cap to 65 MiB and the compressed message cap to 66 MiB.

## Forecast rejection and service boundaries

The private parking-only forecast candidate failed its frozen `3d9534e` seed-1 screen.
The three arms used one identified private executable:

| Arm | Completed requests | Queue skips |
| --- | ---: | ---: |
| Off | 363 | 1,077 |
| Current forecast | 364 | 1,076 |
| Parking-only candidate | 361 | 1,079 |

All arms passed native integrity checks, 1,355,220 total safety ticks, and eight physical restore probes.
The candidate lost 53 off-baseline completed identities and 60 current-forecast completed identities.
It added the same numbers of skips or refusals for those identities.
Identity, individual no-harm, and the required 5% benefit gates failed despite passing aggregate 2% comparisons.

Items 10 and conditional 11 closed with rejection.
No seed-2 study arm, repeated study, tuning, live integration, or adoption followed this failure.
The earlier prefix work remains separate evidence.
Restore probes cover accepted-cohort physical and twin controls, not the original run's complete offered future.

Archived [shared forecast](forecast-shared-service-screen.md), [Group service](group-fleet-service-screen.md), and [policy reruns](policy-failures.md) retain failed service gates.
They do not qualify sustained capacity or default adoption.
The [adoption gates](experimental-adoption.md) still require matched service, loss, recovery, safety, and cost evidence.
Successful safety checks or aggregate results do not waive individual service failures.

## Car continuation and release decision

The [car qualification](car-continuation-qualification.md) requires the same identified executable for checkpoint replay.
The earlier clean `a13bbae` bridge used executable SHA-256 `a20ef6960345fecfc22d6d58816669b10556b53a75a6447f514e58d4d94a193d`.
Four selected cases and 34 independent CLI calls preserved exact checkpoint and future report bytes.
This does not migrate another executable's checkpoints or claim identity for later builds.
Since `96bcb9f`, checkpoint decoding matches member names by exact case.
The version-1 foundation contract does not qualify Express car plans.
The container build covers the server, not the car CLI.

Hosted Check passed for the `0ca0c63` source, and that run published no image.
Hosted Check has not run on the pinned source.
Browser performance work closed with rejection.
The incident redesign ended at stage 3.
The `check:static` job runs the two plain byte proofs in CI.
Maximum Express costs limit responsive operating claims within the opt-in qualification.
Forecast and archived service failures still block experimental adoption and capacity claims.
No acceptance waiver follows from a successful build or a passing Check.

The README records the 32 MiB project limit and the 43 MiB editor import limit.
This update changes neither runtime limit.
The [dependency review](dependencies.md) retains its source-specific advisory and filesystem trust-boundary notes.
The vulnerability task in the passing `check:static` job did not replace those notes with a security exception.

Deployment remains separately authorized.
Target health, storage, origins, resource limits, and rollback need a target-specific review.
No deployment, credential, default, or cap change occurred in this update.
