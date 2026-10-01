# Current-build Chrome and WebSocket measurements

All eight corrected local runs sustain 59.95x to 59.99x playback without speed reductions or CPU-quota throttling.
Sixteen Chrome page captures receive one initial full snapshot and subsequent deltas, without reconnects or HTTP state polling.
The three-client runs share identical compressed payloads for each common stream sequence.
Update traffic is about 331 KiB per second per client.
The short live comparisons do not establish a useful server CPU improvement from snapshot preallocation.

## Sources and fixture

The baseline server is frozen `a54281e`.
The candidate is `2579876`, which adds six lines for [snapshot preallocation](snapshot-allocation.md).
Both binaries use explicit source overlays and have distinct recorded hashes.
Both serve the same freshly generated candidate Go/WASM client and static assets.
This controls the client when comparing server behavior.

Each server restores the same saved LondonFull physical-state fixture at tick 216,000, paused, with 287 pods.
The fixture generates AM-profile demand at 12 orders per simulated minute with seed 20260929.
Startup checks require physical restoration and requested speed 60x.
The saved fixture and its geometry remain unchanged.
These checks do not cover every fleet size or demand configuration.

One client uses local delivery.
Three clients use emulated round-trip delays of 0, 300, and 600 milliseconds, with bounded jitter and ordered delivery.
Each configuration has two repetitions, with reversed source order.
Every active server window lasts about 60 seconds.
Small achieved-work differences remain, so these are equal-wall comparisons, not exact equal-work CPU controls.

Servers use GC 100 and GOMAXPROCS 4.
The combined test group allows 12 logical CPUs and 8 GiB, with serial cases and a 90-minute watchdog.
All other owned heavy studies have stopped.
Per-window cgroup counters show zero throttled quota periods in all eight corrected runs.
Those counters do not rule out unrelated host or ancestor contention.

Chrome runs headless with SwiftShader software rendering.
External requests are blocked, tiles use local placeholders, and service workers are disabled.
CPU profiles and browser probes run in both variants.
The WebSocket proxy records publications and gzip payloads before downstream delivery.
Static downloads, topology responses, commands, and protocol overhead are outside the reported update-byte rates.

## Live server results

The table reports means of two processes per source and client count.
RSS is the endpoint sample before explicit post-window heap collection.
It is not peak RSS or retained live heap.

| Clients | Server | CPU, seconds | Endpoint RSS, MiB |
| ---: | --- | ---: | ---: |
| 1 | Baseline | 38.500 | 211.41 |
| 1 | Candidate | 38.435 | 210.85 |
| 3 | Baseline | 39.685 | 206.40 |
| 3 | Candidate | 39.505 | 199.73 |

Mean CPU differs by less than 0.5% between sources for both client counts.
Two short profiled repetitions do not establish a useful live CPU improvement.
Endpoint memory samples also do not establish a retention or leak improvement.
The [equal-work native measurements](snapshot-allocation.md) retain their separate allocation benefit.
The server keeps GC 100.

## Client and transport results

Each range spans individual page captures, not a pooled quantile distribution.

| Measurement | Observed range |
| --- | --- |
| Compressed update traffic | 330.6 to 331.1 KiB/s per client |
| Update rate | 19.60 to 19.65 messages/s |
| Gzip inflation p95 | 0.6 to 0.8 ms |
| Local receive-to-ACK-submission p95 | 16.5 to 19.9 ms |
| rAF gap p95 | 50.1 to 133.3 ms |
| Initial full snapshots | Exactly one per page |
| Reconnects and state polling | Zero |
| Unacknowledged messages at capture | Zero |

All four three-client runs retain 1,177 to 1,179 sequences with identical gzip payload hashes across clients.
High delay does not cause another full snapshot.
Neither WebSocket hop negotiates a second compression layer.
The same compressed update stream serves all three clients.
Small byte-rate differences do not show network savings from preallocation.

Browser captures last 60.27 to 61.94 seconds because collection follows the server pause.
Their quantiles include that collection tail.
Receive-to-ACK measurements cover local client processing and exclude the emulated return delay.
Proxy ACK accounting also ends before that return delay and does not measure the server's complete ACK debt.

Main-thread samples include WASM JSON decoding, map lookup, and drawing commands.
They omit much of the rendering-worker and GPU-process CPU.
Summed browser-process CPU is about 482 to 487 seconds per active window under software rendering.
This does not predict physical-GPU CPU use or frame rates.
These finite windows do not qualify longer sessions, other fleets, higher demand, or more clients.
They do not test Fly hosting.

## Initial quota-limited attempt

The first attempt allowed four CPUs for the combined server and software-rendered browsers.
More than 90% of its quota periods were throttled.
Both initial one-client cases reduced 60x to 15x after about five seconds.
Corrected cases with CPU headroom sustain 60x with zero quota throttling.
This supports the quota explanation for that attempt, without proving that every playback reduction has the same cause.
The incomplete attempt remains separate from the corrected matrix and supports no ordinary playback-capacity claim.
Throttled-period ratios count affected quota periods, not the fraction of wall time lost.

[Per-arm results](measurements/current-client-network-arms.csv) retain playback, CPU, RSS, speed changes, and cgroup counters.
[Per-page results](measurements/current-client-network-pages.csv) retain byte rates, processing, ACK, frame-gap, and heap fields.
[Metadata](measurements/current-client-network.json) retains source, assets, executable hashes, and raw-result hashes.
Profiles, Chrome samples, screenshots, helpers, and the quota-limited attempt remain in `~/.cache/agents/podsim/current-live-20261001/`.
The corrected unit stopped successfully with no remaining browser or server processes.
