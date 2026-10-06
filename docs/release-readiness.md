# Release readiness

Status: physical coupling qualification remains incomplete on October 5, 2026.
This report pins `7c970c265a0a28019870147c3301bbce10e61e0c`.
At this source, one saved-state version and one stream version serve every project kind.
Hosted Check, including its image publication job, passed for this source.
All four bounded browser performance candidates were rejected and closed.
The maintainer's standing instruction holds the unfinished coupling release.
This report does not authorize deployment, default adoption, or a cap change.

The [measurement record](measurements/release-readiness.json) is unchanged in this update.
It still pins the earlier `8181495` source, its source-specific evidence, and its archived failures.
This update read the pinned source in a detached worktree.
It did not build the source or run tests.

Later sources change several statements of this report.
They add the incident, fault, and emergency markers, and the incident redesign stops at stage 3.
The `64b4f3f` source raises the stream and HTTP state cap to 65 MiB and the compressed message cap to 66 MiB.
The [qualification](qualification.md#coupling-format-qualification) records the later coupling format and incident checks of item 6.

## Hosted checks and published image

[Check](https://github.com/dotwaffle/podsim/actions/runs/37318001608) passed in 14m00s, from 13:35:33 to 13:49:33 UTC.
All six check jobs and the publish job concluded with success.
The Check workflow runs six check jobs in parallel on separate hosted runners.
Its publish job needs all six check jobs and does not run for pull requests.
The `2140d40` and `31322b5` sources split the check, and `2138790` moved publication into Check.

| Job | Seconds |
| --- | ---: |
| `test:race:sim-other` | 726 |
| `test:race:sim-stations` | 384 |
| `test:race:other` | 376 |
| `test:race:session` | 312 |
| `check:static` | 167 |
| `qualify` | 44 |
| `publish` | 101 |

The `check:static` job runs bounds, web tests, lint, vulnerability checks, builds, and embedded tests.
The `qualify` job runs only the two Station 19 drain tests, and `069bafb` gave it a 30-minute test timeout.
The `7c970c2` source reduced the local wall time of `TestScale100Station19BurstDrainsSafely` from 125.3 to 11.7 seconds.
The `b70e46e` source runs 48 session tests in parallel.
The publish job started at 13:47:51 UTC, after the last check job completed.
The published image is `ghcr.io/dotwaffle/podsim@sha256:8b60067be3eb2a298c381b033c1438fc6771dc4bb5cb045c25b6a3723b3398b9`.
Package version 1339020137 carries the complete commit tag and `latest` at this digest.
Use the digest because `latest` can move.

The previous `5078afd` [Check](https://github.com/dotwaffle/podsim/actions/runs/37316095300) passed in 13m55s, from 13:20:57 to 13:34:52 UTC.
Its jobs took 748 seconds for `test:race:sim-other`, 499 for `test:race:other`, and 285 for `test:race:sim-stations`.
They took 247 seconds for `test:race:session`, 211 for `check:static`, and 153 for `qualify`.
The earlier `8181495` Check ran all tasks in one 1,236-second job, and its race task took 1,198.77 seconds.
Its publication ran in a separate workflow that did not wait for Check.
Those results remain archived in the measurement record.
Job durations vary between runs and runners.
They do not establish a causal speedup against earlier runs or qualify a later source.

The audit did not pull or execute the image, inspect an OCI manifest, or test an ARM runtime.
Publication success does not establish hosted health or deployment readiness.
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
The legal 65 MiB payload, production 30-second deadline, stalled-reader error, and cancellation controls remain.

The plain `test:bounds` task in `check:static` retains every maximum checkpoint shape assertion.
In the CI split qualification of the measurement record, the 80 MiB guard rejected the 1,265,591,604-byte counting envelope and accepted the 72,104,395-byte fitting envelope.
This update did not measure those sizes again.
Only this serial shape proof excludes race instrumentation in its package.
The `test:embedded` task runs the root and `cmd/serve` suites with embedded assets.
In the session package it runs only the worst-case save size test, three widest Express adapter tests, the packed text wire cost test, and the maximum stream encoding test.
Bounded application tests and concurrency controls remain under the four race tasks.
`TestComposedWorstCaseFormats` skips under the race detector, and the `test:embedded` pattern does not name it.
Thus no hosted Check job at the pinned source runs the composed worst-case format proof.
The later `43e1361` adds it to the `test:embedded` pattern.
The source audit and qualification retain exact assertion-body comparisons and the prior tool failures.

## Formats and versions

The `248f26e` source collapsed project versions to one version, 1.
A feature is allowed when its fields are present.
Express needs the `orderContract` marker `express-v1`.
Trains need the `couplingContract` marker `compact-pair-v1`.
The server refuses project versions 2 through 5 and does not migrate them.

Item 7 merged the save and stream families into one family for every project kind.
Every session writes saved-state version 9, and the server reads only version 9 (`9de7a3b`).
Hello 6 is the only stream version (`5f1adac`).
`GET /api/state` has one media type, `application/vnd.podsim.state-6+json`, for every project kind.
The contract markers select the optional members and the array bounds of each format.
The order text is packed for every project kind, and no format has a `textEncoding` member.
An intact saved state of version 1 through 8, or of version 10 or later, moves aside with `unsupported_version`.
A damaged or invalid saved state moves aside with `invalid_state`, and the server starts a new session.
A saved state of more than 80 MiB is the only file that the server keeps.
Saving then stops, and startup fails.
A restore counts the waiting orders and the requeued riders together against the queue bound of the contract (`e5933ce`).
The [operations guide](operations.md#session-state) and the [protocol](protocol.md) give the full rules.

The `96bcb9f` source gives every JSON format member a lowerCamel name.
Their decoders match member names by exact case.
Each saved state with the earlier names has a version before 9, so the server moves it aside with `unsupported_version`.
The editor refuses a project file with the earlier names.

The [composed worst-case record](measurements/composed-worst-case-formats.json) from `ee5851f` measures one fixture for each shape and format.
Each fixture has every landed member at its widest at the same time.
The values are independent maxima, not reachable states.
Every fixture fits its cap.
The narrowest stream shape is the Express with coupling HTTP state, with 1,252,396 bytes below the 64 MiB cap.
The narrowest save shape is Express with coupling, with 6,836,569 bytes below the 80 MiB cap.

## Express operating limits

The [Express browser qualification](express-browser-qualification.md) covers the opt-in `express-v1` contract.
The [native](express-native-qualification.md) and [wire](express-wire-qualification.md) records preserve foundation behavior and physical limits.
They do not certify every curve, service workload, browser, or hardware configuration.
Express remains opt-in through the `express-v1` marker.
Its party capacity remains 20, outstanding-order limit 8,600, and fleet and registry limit 300.
In each format, the `express-v1` marker selects bounds of 8,600 waiting or pending orders and 20 riders per pod.
Without the marker, the bounds are 2,600 orders and 8 riders per pod.
Current defaults and physical constants remain unchanged.
The saved-state limit remains 80 MiB.
At the pinned source, the stream and HTTP state limit is 64 MiB for every project kind.
The stream binary limit is 65 MiB at that source.
Manual admission retains its 200-request queue limit.

Physical session restore resets ordinary speed and reconstructs future grants.
It does not preserve an exact trajectory.
The widest encoded assets exercise independent field shapes, not reachable traffic or unique order conservation.

The widest Chromium run reached 2,760,204 KiB of summed browser process RSS, about 2.63 GiB.
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

## Physical coupling remains incomplete

The [physical coupling contract](physical-coupling-contract-proposal.md) defines the opt-in `compact-pair-v1` profile.
The `2fba224` source introduced the phase-1 coupling geometry, profile, and body layer.
The `12848c9` source added the qualified inactive typed-owner migration.
The `8181495` source added six qualified private reservation files.
The `03806d1` source added the private Compact pair motion engine.
The `dfa2528` source added native trains, native group export and restore, and the bounded save 8 and stream 5 contracts.

At the pinned source, coupling runs live in a project that meets four conditions:

- It has the `compact-pair-v1` marker.
- Its `couplingEnabled` option is on.
- It has authored coupling sites and corridors.
- Virtual platooning is selected.

The `couplingEnabled` option is off by default, and a project without the marker never forms a train.
In a qualifying project, Step discovers pairs of Compact pods on certified straight corridors without authored groups.
Each pair forms a train, runs through the six train phases, and retires.
Both cabins of a train are empty, or both are occupied.
With the coupling marker, saved-state version 9 and stream hello 6 carry train membership and body geometry.
Native export and physical restore carry committed trains, and logical recovery refuses them.
A coupling fault pauses the simulation, stops publication, and keeps the last valid observation.
The HTTP state of a coupling project carries the coupling marker and uses the one state media type.
The editor converts a project to trains, edits coupling sites and corridors, and reads the live state of train projects.
The view draws each train and interpolates a connected train as one rigid move.
Turning trains off drains the current trains without a reset.

Two fixes close safety faults that stopped Step.
The `66b0c65` source keeps a coupled member's receiving berth claim from buffer and redistribution yields.
The `1977a53` source refuses an ordinary platoon link behind a coupled member.
Review found that fault in the existing coupling code.
The `16eab09` source also keeps the receiving claims of an approach member, so passenger traffic no longer stops a pair from forming.
The `8b326a1` source gives a resumed coupling leg the speed cap of its original start lane.
The `af2e942`, `8591bad`, and `e1f3b65` sources extend the qualification tests.
They cover policy-off parity and drainage at every tick, a blocked split exit, same-tick pair candidates on a shared junction, and recovery after a coupled berth yield.

Part (a) of coupling qualification item 6, the simulation qualification tests, landed with `e1f3b65`.
Part (b) closed in `1427ddb` with an enforced corridor speed bound.
Coupling geometry validation refuses a site or corridor lane faster than 360 m/s (`MaxCouplingCorridorSpeed`).
At that bound, the body sweep check refuses each pose that the pair connector check refuses.
`TestCouplingPairConnectorBoxNotDominatedAtHighTravel` still records the gap at 1,200 m/s, above the bound.
Item 6 has two open parts:

- Part (c): the format-dependent gates must qualify again on the single save and stream family.
  This work is in progress and is not in the pinned source.
- Part (d): the simulation evidence must run again after incident stage 4.

The full coupling implementation is not qualified.

## Incident service transitions

The [incident service transitions contract](incident-service-transitions-contract-proposal.md) is stage 1 of the staged incident plan.
The maintainer approved it on October 5, 2026, in `482d93d`.
It defines the service transitions that vehicle faults and rider emergencies share.
The `5078afd` source revised its format section for saved-state version 9 and hello 6.
Section 15 of the contract lists nine patches.
Patch 1, the claim classification and its baseline fix, landed in `6b8eb62` and `16eab09`.
Part of patch 4 landed.
The `2e307ed` source gates the supply paths of section 4.3 and the passenger claim yields on a service hold.
The `2ff0912` source adds the departure backlog path, which the maintainer approved as a new row.
At the pinned source, no production path sets a hold, so every pod stays in service.
The withdrawal operations of section 4.2, the contract checks, and patches 2, 3, and 5 through 9 are pending.

The contract byte budget uses the composed worst-case record.
After stage 1, the contract estimates that the Express with coupling HTTP state is about 237,204 bytes over the 64 MiB cap.
The contract records a maintainer decision: stage 1 patch 9 measures that shape and raises the stream and HTTP cap only if the shape is over it.

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

Archived [shared forecast](forecast-shared-service-screen.md), [Group service](group-fleet-service-screen.md), [compact queues](compact-queue-speed-fixed-screen.md), and [policy reruns](policy-failures.md) retain failed service gates.
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
The published image builds the server, not the car CLI.

Hosted Check and publication passed for the pinned source.
Browser performance work closed with rejection.
Coupling qualification item 6 parts (c) and (d) remain open, and the maintainer holds the unfinished coupling release.
Incident stage 1 has patch 1 and part of patch 4, and its other patches are pending.
At the pinned source, no hosted Check job runs the composed worst-case format proof; `43e1361` adds it to `test:embedded`.
Maximum Express costs limit responsive operating claims within the opt-in qualification.
Forecast and archived service failures still block experimental adoption and capacity claims.
No acceptance waiver follows from a successful build or image publication.

The README records the existing 10 MiB project limit and 21 MiB editor import limit.
This update changes neither runtime limit.
The [dependency review](dependencies.md) retains its source-specific advisory and filesystem trust-boundary notes.
The vulnerability task in the passing `check:static` job did not replace those notes with a security exception.

Deployment remains separately authorized.
Target health, storage, origins, resource limits, and rollback need a target-specific review.
No deployment, credential, default, or cap change occurred in this update.
