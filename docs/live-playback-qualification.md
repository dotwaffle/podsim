# LondonFull playback and multiple clients

All ten measured cases retained their requested 15x or 60x setting.
The windows lasted 90 seconds, with two repetitions per case.
No client error, reconnect, extra full snapshot, or automatic speed reduction occurred.
These results do not establish indefinite playback stability or passenger capacity.

## Method

The source base is `24aaa69`.
Runtime code and browser assets match `160dc9f`.
The intervening commits change tests and study records.
Each arm restores the same version 2 physical fixture at tick 216000, with 287 pods.
Demand uses the AM profile, 12 requests per simulated minute, and seed 20260929.
The fixture already has 720 accepted requests and no skipped offers.
Buffers, pickup swaps, sharing, and redistribution remain off.

The matrix covers one and three clients at 15x and 60x, plus a zero-client 60x control.
The second repetition reverses the first repetition's order.
Every arm uses Go 1.27.1, GOMAXPROCS 4, and GOGC 100.
No other owned CPU study runs during measurement.
The server uses embedded browser assets and normal file-backed persistence.

Chrome 143 runs headless with SwiftShader software rendering and active CPU and timing probes.
One client uses a local connection.
Three clients use WebSocket round-trip delays of 0, 300, and 600 milliseconds.
The proxy preserves message order and adds bounded jitter of up to 35 milliseconds.
HTTP commands remain local.
External map images receive a local placeholder, and other external requests are blocked.

Two completed 15x pilot arms are retained in the first repetition.
Their server, fixture, browser assets, and probe hashes match the final matrix.
The pilot runner stopped at an arm boundary after its source hash changed to add stop-marker handling.
This infrastructure stop did not invalidate either completed arm.
The plan records their result hashes and reuse.

## Server measurements

Each row gives the mean of two repetitions.
CPU seconds cover the server process during its approximately 90-second window.
RSS is endpoint resident memory, not peak memory or retained heap after collection.

| Requested speed | Clients | Achieved speed | Server CPU seconds | Endpoint RSS, MiB |
| --- | ---: | ---: | ---: | ---: |
| 15x | 1 | 14.998x | 21.48 | 184.8 |
| 15x | 3 | 14.997x | 22.63 | 183.0 |
| 60x | 0 | 59.993x | 32.17 | 206.5 |
| 60x | 1 | 59.985x | 56.51 | 215.7 |
| 60x | 3 | 59.985x | 58.18 | 220.0 |

Individual arms use 0.238-0.648 server CPU cores on average.
Software-rendered Chrome adds shared-machine load, so differences between zero and connected clients are not pure publication costs.
The local recorded cgroups have unlimited CPU quotas and zero recorded throttling deltas.
These counters do not exclude ancestor limits or unrelated host contention.

Published-state clock progress remains near the requested speed in one-second samples.
Those samples use proxy arrival times and do not measure individual physics-step latency or browser display delay.
Zero-client controls have no publication samples.
Their final state and server reduction logs supply the aggregate playback evidence.

## Stream and client measurements

All sixteen pages use one socket and one initial full snapshot, followed by deltas.
No page polls the HTTP state endpoint after startup.
The clients receive identical gzip payload hashes for each shared sequence.
They negotiate no second WebSocket compression layer.
Added RTT alone does not trigger another full snapshot.

| Speed | Clients | Compressed KiB/s per page | Updates/s | Inflate p95, ms | Receive-to-ACK p95, ms | Animation gap p95, ms |
| --- | ---: | --- | --- | --- | --- | --- |
| 15x | 1 | 253.9-254.5 | 19.59-19.63 | 0.5 | 12.4 | 49.9 |
| 15x | 3 | 253.9-254.0 | 19.61-19.62 | 0.5-0.6 | 13.5-14.1 | 100.1 |
| 60x | 1 | 339.1-339.3 | 19.66 | 0.6 | 16.0-16.7 | 50.0 |
| 60x | 3 | 339.2-339.4 | 19.66-19.67 | 0.6-0.7 | 18.0-20.7 | 116.6-133.3 |

Payload rates exclude WebSocket, TCP, and TLS overhead, initial snapshots, and other HTTP traffic.
The timings end when the browser submits an ACK after applying state.
They exclude drawing and the delayed ACK return path.
Equal receive and submitted-ACK counts do not prove final server receipt of every ACK.
Proxy outstanding-message counts also exclude that return path.

Mean whole-browser CPU use spans about 8.8-9.3 cores across the connected groups.
Individual arms span 8.56-9.31 cores.
These process-tree totals include software rendering and instrumentation.
They do not attribute all browser CPU to graphics or predict hardware-rendered Chrome behavior.
Process-tree samples can omit CPU from processes that exit between samples.

Three-client animation gaps are longer despite similar total browser CPU.
Long tasks increase at 60x, reaching 3.27-4.36 seconds total per three-client page capture.
Client profiles stop sequentially after the server pauses, so their capture windows differ slightly.
Summed process RSS can count shared pages more than once.

## CPU profile and follow-up

In the first 60x repetition, station-phase classification consumes 19.67%, 15.21%, and 14.00% of sampled server CPU.
These are cumulative samples for zero, one, and three clients, respectively.
They identify a private cache trial, not a predicted speedup.
The calculation repeats route lookups while a pod stays in the same block.

The one-client profile attributes 10.75% of sampled CPU to publication, including its descendants.
The demand step consumes 2.89% in that profile.
The server advances physics at approximately the requested rate in all measured cases.
The instrumented clients show larger animation gaps than the nominal publication interval.
Neither native inflate timing nor this profile identifies a universal bottleneck for other workloads.

This matrix holds GOGC at 100 and cannot rank GC settings.
Server GOGC 100 and comparison-command GOGC 400 remain unchanged.
A separate matched fixed-work replay and live follow-up evaluate the private cache.

## Evidence

Machine-readable results are [live-playback.json](measurements/live-playback.json).
The [arm table](measurements/live-playback-arms.csv) and [page table](measurements/live-playback-pages.csv) retain individual observations.
The artifact manifest retains server, fixture, probe, runner, and browser-asset hashes.
Raw records and independent review remain in the external artifact directory named by the manifest paths.

The analyzer checks unique active request IDs and request conservation against each final state.
Completed riders retained in pod records do not count as active riders.
It also checks gzip publication counts, submitted ACKs, requested settings, and final clocks.
It records raw-result hashes, which the independent review verifies.
These endpoint checks complement the protocol probes but do not replace every-tick passenger-capacity qualification.
