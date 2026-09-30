# Live Chrome, latency, and GC measurements

All 18 cases passed the protocol checks.
Every connected client used one initial full snapshot, then deltas, with no reconnect or HTTP polling.
Three LondonCentral cases reduced speed from 60x to 15x.
The results do not establish sustained 60x operation.

## Method

The production source is `c93bc9a`, with cached pickup bounds and reusable route search storage.
Each case restores a separate saved LondonFull or LondonCentral simulation at tick 216000.
Demand is 12 requests per simulated minute, with seed 20260929 and one simulated hour of prior traffic.
Restore uses the physical tier.
Route caches start cold after restore.
Buffers, pickup swaps, sharing, and redistribution are off.

Each case measures about 60 wall seconds at requested speed 60x.
The matrix covers GOGC 100, 200, and 400, with zero, one, or three Chrome clients.
GOMAXPROCS is 4.
Each cell runs once, in a fixed order.
No other agent CPU-heavy work runs during measurement.
File-backed persistence uses the normal save policy.

Chrome 143 runs headless with SwiftShader software rendering.
The probe uses Playwright 1.57 and ws 8.18.3 in its own artifact directory.
Automated image requests receive a local placeholder.
The browser blocks other external requests and service workers.

One-client cases use a local connection.
Three-client cases use WebSocket round-trip delays of 0, 300, and 600 ms, with ordered jitter of up to 35 milliseconds.
The proxy does not negotiate WebSocket compression.
It forwards the production gzip binary messages.
HTTP commands remain local and do not receive the added delay.

## Playback, CPU, and memory

Each table cell lists values for zero, one, and three clients, in that order.
CPU is process CPU seconds during the server measurement window.
RSS is the server resident memory at the end of that window, not peak heap or post-collection retention.

| Preset | GOGC | Actual playback, 0 / 1 / 3 clients | Server CPU seconds | Server end RSS, MiB |
| --- | ---: | --- | --- | --- |
| LondonFull | 100 | 59.98x / 59.97x / 59.97x | 33.69 / 50.21 / 51.56 | 251.9 / 241.1 / 232.9 |
| LondonFull | 200 | 59.99x / 59.98x / 59.98x | 34.32 / 49.44 / 50.19 | 334.5 / 348.7 / 344.9 |
| LondonFull | 400 | 59.98x / 59.96x / 59.97x | 33.69 / 48.63 / 49.56 | 507.6 / 563.2 / 564.2 |
| LondonCentral | 100 | 59.47x / 35.93x / 35.92x | 16.61 / 16.65 / 17.34 | 99.0 / 89.6 / 92.2 |
| LondonCentral | 200 | 59.63x / 50.25x / 58.50x | 15.16 / 19.86 / 22.88 | 128.9 / 143.4 / 148.3 |
| LondonCentral | 400 | 59.73x / 58.86x / 58.73x | 14.40 / 20.67 / 21.99 | 211.1 / 256.7 / 244.5 |

LondonCentral with one and three clients at GOGC 100 reduced to 15x.
The one-client case at GOGC 200 also reduced to 15x, near wall second 49.
The other connected cases retained 60x.
Zero-client cases ended at 60x, but have no socket-derived speed-change timeline.

The software-rendered browser process tree used about 477-508 CPU seconds per 60-second server window.
That shared-machine load and active profiling can affect server scheduling.
One repetition per cell cannot identify the cause of a reduction or establish a reliable GC ranking.

GOGC 100 used less end RSS than 200 or 400 in both presets.
The comparison command still defaults to 400 when GOGC is unset.
The server retains Go's default of 100 unless its environment overrides it.
Neither default changes from these measurements.
The lower-memory results support retaining 100 for now and comparing again after client CPU improvements.

## Stream and client measurements

All 24 client observations used one socket and one initial full snapshot.
No client requested polling state after startup.
Three-client cases shared hundreds of sequences with identical compressed hashes.
Added RTT alone caused no extra full snapshot or reconnect.
The maximum number of outstanding messages ranged from 2 to 7.
All clients had equal received and submitted-ACK counts at capture time.
This equality does not test messages still in flight on a network.

The proxy received about 141-248 kB of gzip envelopes per second per client.
It observed about 12.5-12.9 updates per second.
These rates use server-to-proxy timestamps before added delay.
They exclude the initial snapshot and WebSocket, TCP, and TLS overhead.
They do not measure browser-delivered throughput or guarantee 20 delivered updates per second.
The lower rates include cases that reduced speed.

Browser-native inflate p95 was 0.6-0.7 milliseconds.
Receipt-to-ACK submission p95 was 14.1-23.9 ms, with a maximum of 67.7 milliseconds.
Those intervals exclude the ACK return path.
Local pause commands took at most 15.1 milliseconds.
The command result does not predict command latency across the injected RTT.

Single-client animation-frame intervals had a median near 33.3 milliseconds.
Three-client medians reached 100-116.7 milliseconds.
Those results include software-rendering contention across the browser process tree.
Client captures occur sequentially after the server pauses.
Later captures include earlier profile retrieval and screenshots, so client windows are longer than the server window.
Use the client timing figures as diagnostics, not equal-duration CPU comparisons.
Summed browser RSS can count shared pages more than once.

The pilot CPU profile attributes 4.586 seconds to `strings.ToLower` in one client capture.
The journey picker repeatedly sorts station names and filters names and codes.
Caching the catalog in the existing network index is the next bounded client optimization.
Native decompression is not the dominant measured client cost.

[Arm measurements](measurements/live-chrome-arms.csv) preserve playback, CPU, memory, and command results.
[Client measurements](measurements/live-chrome-pages.csv) preserve stream, ACK, inflate, and rendering diagnostics.
[Metadata](measurements/live-chrome.json) records frozen inputs, source, raw hashes, and measurement limits.
The local artifact bundle is `~/.cache/agents/podsim/live-client-20260930/`.
