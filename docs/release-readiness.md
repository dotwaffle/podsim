# Release readiness

Status: full physical coupling remains incomplete on October 4, 2026.
This report pins `81814954a37f388afdb948037bb7ea051cde1030`.
Hosted Check and image publication passed for this source.
All four bounded browser performance candidates were rejected and closed.
This report does not authorize deployment, default adoption, or a cap change.

The [measurement record](measurements/release-readiness.json) pins source-specific evidence and archived failures.
The retained documentation worktree uses the older `a13bbae` base.
This update did not rebuild current source in that worktree.

## Hosted checks and published image

[Check](https://github.com/dotwaffle/podsim/actions/runs/37181963947) passed from 06:09:10 to 06:29:46 UTC.
All required tasks passed, including bounds, embedded tests, races, qualification, lint, builds, web tests, and vulnerability checks.
The race task took 1,198.77 seconds, bounds took 118.37 seconds, and embedded tests took 440.22 seconds.
[Publish container](https://github.com/dotwaffle/podsim/actions/runs/37181963901) passed from 06:09:10 to 06:13:18 UTC.
The published image is `ghcr.io/dotwaffle/podsim@sha256:9ba6db79cd91444dd7d3bcb37145256e7cd27297c6c88f5aaeb8e74291e87d9b`.
Package version 1333303197 and publication logs identify the complete commit tag and `latest` at this digest.
Use the digest because `latest` can move.

The earlier `12848c9` Check and publication passed.
Its race task took 926.51 seconds, bounds took 89.22 seconds, and embedded tests took 311.26 seconds.
These source-specific observations remain archived in the measurement record.
Tasks shared the hosted runner, and their durations overlap.
They do not establish a causal speedup against earlier runs or qualify the later source.
Publication remains independent of Check, so both workflow conclusions matter.

The audit did not pull or execute the image, inspect an OCI manifest, or test an ARM runtime.
Publication success does not establish hosted health or deployment readiness.
The existing deployed runtime was not contacted or changed.

## Preserved CI failure and coverage

The earlier `a13bbae` Check failed in `TestStreamSlowWriteBudget/slow_reader`.
It reported `slow write budget 3.926958098s <nil>`.
This was an assertion failure, not a timeout or reported data race.
The old elapsed sample included client reading and result-observation delay after server write completion.

The test repair captures completion time with the writer result.
An actual compiled caller control passed with a 919.839-millisecond write and result observation after 5.314 seconds.
A mutant that restored the old sample compiled and failed after 5.308 seconds.
The source overlays and original worktree hashes were verified afterward.
The legal 65 MiB payload, production 30-second deadline, stalled-reader error, and cancellation controls remain.

The required plain `test:bounds` task retains every maximum checkpoint shape assertion.
The unchanged 80 MiB guard rejects the 1,265,591,604-byte counting envelope and accepts the 72,104,395-byte fitting envelope.
Only this serial shape proof excludes race instrumentation in its package.
Four maximum session codec proofs, the bounded-scan checks of the maximum stream encoding, and the maximum gzip application proof run without race instrumentation in the required `test:embedded` task.
Bounded application tests and concurrency controls remain under the complete race command.
The source audit and qualification retain exact assertion-body comparisons and the prior tool failures.

## Express operating limits

The [Express browser qualification](express-browser-qualification.md) covers the opt-in `express-v1` contract.
The [native](express-native-qualification.md) and [wire](express-wire-qualification.md) records preserve foundation behavior and physical limits.
They do not certify every curve, service workload, browser, or hardware configuration.
Express remains opt-in with project version 4, saved-state version 7, and stream version 4.
Its party capacity remains 20, outstanding-order limit 8,600, and fleet and registry limit 300.
Current defaults and physical constants remain unchanged.
The saved-state limit remains 80 MiB.
Express HTTP and raw stream limits remain 64 MiB.
The stream binary limit remains 65 MiB.
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

The `2fba224` source introduced the phase-1 coupling geometry, profile, and body layer.
The `12848c9` source added the qualified inactive typed-owner migration.
Pod and inactive group owner tags are distinct internally.
Recorded ordinary, virtual-link, compact-queue, and Express parity fixtures preserve their observed bytes.
The current `8181495` source adds six qualified private reservation files.
Focused tests, race checks, vet, lint, and the 78.712-second full native suite passed.
Two original mutation survivors remain disclosed.
A later distinct identity case killed the guard bypass.

These helpers have no live caller and create no physical groups.
Live staging reachability and drainage remain unproved.
Train motion, native group export and atomic restore, activation, and public adapters remain pending.
The proposed project 5, save 8, and stream 5 family is not implemented.
The full item-16 implementation is not qualified or complete.
The landed geometry and ownership work does not establish runtime coupling support.

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
The version-1 foundation contract does not qualify Express car plans.
The published image builds the server, not the car CLI.

Hosted Check and publication passed for the identified source.
Browser performance work closed with rejection.
The full item-16 coupling implementation remains incomplete.
Maximum Express costs limit responsive operating claims within the opt-in qualification.
Forecast and archived service failures still block experimental adoption and capacity claims.
No acceptance waiver follows from a successful build or image publication.

The README records the existing 10 MiB project limit and 21 MiB editor import limit.
This update changes neither runtime limit.
The [dependency review](dependencies.md) retains its source-specific advisory and filesystem trust-boundary notes.
The current hosted vulnerability task passed without replacing those notes with a security exception.

Deployment remains separately authorized.
Target health, storage, origins, resource limits, and rollback need a target-specific review.
No deployment, credential, default, or cap change occurred in this update.
