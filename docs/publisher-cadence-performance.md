# Publisher capture cadence measurements

The publisher now schedules each capture from the previous capture's 50 ms deadline.
Measured proxy-arrival rate increases from about 12.7 to 19.6 updates per second in these fixtures.
All twelve runs retain approximately 60x playback, including three-client high-latency cases.
More frequent publications use 5% to 6.5% more server CPU and 37% to 40% more compressed traffic.

## Cause and change

The previous loop combined a 50 ms ticker with a second 50 ms elapsed-time check.
If one capture started slightly late, the next ticker could arrive before that check allowed another capture.
The loop then skipped that event and waited for another ticker or recovery wake.
This phase error explains a source-level contributor to the observed low publication rate.
It does not prove that every delivery delay had that cause.

A deadline timer now waits for the remaining interval after publication work finishes.
Early recovery wakes retain the original capture deadline.
Overdue work schedules the next capture immediately, after the previous capture interval has elapsed.
Inactive passes wait 50 ms and release publisher state under the existing rules.
Initial capture, recovery snapshots, cancellation, credits, history bounds, and compression remain unchanged.
The change preserves the capture-rate ceiling.
It does not add a minimum interval between socket writes, which encoding and queues can delay.
No project, saved-state, wire, or operating default changes.

Deterministic tests cover initial, early, exact, overdue, and future deadlines, plus repeated early recovery wakes.
Existing real-stream tests cover initial delivery, recovery, idle reconnection, and shutdown.
The full plain suite, session race checks, vet, lint, native and WASM builds, and fresh gopls checks pass.
Independent implementation review finds no timer, lifecycle, or recovery blocker.

## Method

The baseline server contains the admission storage change from `bd2bd7c`.
The candidate adds only the private deadline-timer change.
Both variants use the same frozen `703a166` label-cache client, other browser assets, and paused physical-state fixtures.
Each case runs twice with reversed variant order.
The matrix has twelve server arms and twenty pages: Central with one client, and Full with one or three clients.

Each process restores tick 216000 and requests 60x playback for approximately 60 wall-clock seconds.
Demand generates 12 requests per simulated minute with seed 20260929.
GC is 100 and GOMAXPROCS is 4.
CPU profiles and browser probes remain enabled.
Small simulated-work differences remain, so these are equal-wall comparisons, not exact equal-work controls.

Three-client Full cases use 0, 300, and 600 ms round-trip delays with ordered 35 ms jitter.
Tiles use local placeholders, external requests are blocked, and service workers are disabled.
Chrome uses SwiftShader software rendering.
No other agent CPU-heavy work runs during timing.
Unrelated user processes remain running.

## Results

Each row gives means across two processes per variant.
Traffic is the mean per page in three-client cases.
Update counts use proxy upstream arrivals in the server window, before downstream delay.

| Preset | Clients | Original updates/s | New updates/s | Original CPU | New CPU | Original traffic/page | New traffic/page |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Central | 1 | 12.64 | 19.61 | 19.400 s | 20.655 s | 164.4 KiB/s | 229.6 KiB/s |
| Full | 1 | 12.73 | 19.66 | 40.975 s | 43.005 s | 242.5 KiB/s | 332.2 KiB/s |
| Full | 3 | 12.71 | 19.65 | 41.885 s | 43.960 s | 242.1 KiB/s | 332.1 KiB/s |

The measured frequency approaches the existing 20 Hz target without removing the capture interval.
This improves cadence by doing more publication work.
It is not a CPU or bandwidth reduction.
Whole-browser CPU remains roughly unchanged in these short software-rendering comparisons.
The compression method and wire shape are unchanged.
The earlier [server/GC report](admission-live-performance.md) used the previous cadence and remains a separate record.

All arms retain approximately 59.8x to 60.0x playback without speed transitions.
Each page receives one initial full snapshot and subsequent deltas without reconnection or a second compression layer.
Shared sequence hashes agree across all three clients.
Final captures have zero unacknowledged messages.
Proxy-recorded outstanding counts range from two to nine.
Those counts exclude the delayed return leg from the proxy to the server and are not server credit measurements.
Gzip inflation p95 ranges from 0.5 to 0.6 milliseconds.
Local processing-to-ACK-submission p95 ranges from 14.0 to 22.8 milliseconds and excludes return network delay.

## Limits and evidence

Page captures last 60.25 to 61.74 seconds because the collector reads pages sequentially after pausing the server.
Browser and ACK observations extend beyond the server window.
Proxy arrival times do not prove a minimum interval between server capture starts or socket writes.
Per-page quantiles describe individual capture windows, not pooled distributions.

Two repetitions, one seed, and short windows do not qualify every demand rate, longer sessions, or slower clients.
The result does not guarantee delivery at 20 updates per second under overload.
It does not qualify Fly hosting or physical-GPU rendering.
Higher update frequency can increase traffic cost on remote connections.
Existing credit, history, and message limits remain the backpressure controls.

The raw measurement data is in git history.
