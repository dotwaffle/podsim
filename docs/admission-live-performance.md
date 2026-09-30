# Live admission storage and GC measurements

The admission storage change uses 3.3% to 6.3% less server CPU across four measured group means.
All 16 one-client Chrome runs sustain approximately 60x playback without speed changes.
GC 100 uses 54% to 58% less endpoint server RSS than GC 400 in the candidate runs.
These fixtures support retaining the server's current GC 100 default.
The comparison command still uses GC 400 when unset.

## Method

The baseline server is `3a708e4`.
The candidate binary comes from `a4ac4ca`, with the admission change from `bd2bd7c`.
Those later view changes do not affect server runtime.
Both servers use the same frozen `a4ac4ca` journey-cache client and other browser assets.
The private label-dimension cache is absent from this comparison.

Each Central or Full process restores the same paused physical-state fixture at tick 216000.
Demand generates 12 requests per simulated minute with seed 20260929.
GOMAXPROCS is 4, with GC 100 or 400.
Each case runs twice with reversed variant order.
The requested speed is 60x for a 60-second server window.
Small achieved-work differences remain, so these are equal-wall measurements, not exact equal-work controls.

One local Chrome client receives gzip WebSocket updates through the instrumented proxy.
The proxy adds no network delay.
Tiles use local placeholders, external requests are blocked, and service workers are disabled.
CPU profiles and browser probes remain enabled in both variants.
Chrome uses SwiftShader software rendering.
No other agent CPU-heavy work runs during the matrix.
Unrelated user processes, including Plex, remain running.

## Results

Each row reports the mean of two processes per variant.
RSS is the server endpoint sample before the explicit post-window heap collection.
It is not peak RSS or retained Go heap.

| Preset | GC | Baseline CPU | Candidate CPU | CPU reduction | Baseline RSS | Candidate RSS |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Central | 100 | 21.390 s | 20.040 s | 6.31% | 105.24 MiB | 107.79 MiB |
| Central | 400 | 20.210 s | 19.305 s | 4.48% | 237.64 MiB | 233.88 MiB |
| Full | 100 | 44.465 s | 42.680 s | 4.01% | 229.51 MiB | 234.51 MiB |
| Full | 400 | 42.880 s | 41.445 s | 3.35% | 559.67 MiB | 556.46 MiB |

The admission cache does not consistently lower endpoint RSS.
Its allocation savings come from the separate [equal-work study](admission-work-performance.md).
Candidate GC 100 uses 3.8% more CPU than GC 400 for Central and 3.0% more for Full.
Both settings sustain the requested playback speed in these windows.
Lower GC 100 RSS makes the current server default appropriate for these fixtures.
This result does not establish a universal GC optimum or justify changing comparison defaults.

Playback ranges from 59.71x to 59.98x across all arms.
Each page receives one initial full snapshot and subsequent deltas without reconnection or a second compression layer.
Final captures have zero unacknowledged messages, with at most two outstanding messages.
Gzip inflation p95 ranges from 0.6 to 0.7 milliseconds.
Local processing-to-ACK-submission p95 ranges from 15.9 to 21.3 milliseconds.
That interval excludes return network delay.

Observed update rates range from 12.35 to 12.76 per second.
Observed compressed traffic ranges from 162.2 to 242.7 KiB per second across presets and variants.
The change preserves wire content and compression behavior.
Small differences in byte rates do not show network savings.
The runs do not establish delivery at 20 updates per second.

## Limits and evidence

Two repetitions, one seed, and one local client do not qualify longer sessions, higher demand, or multiple-client sharing.
Earlier [journey-cache measurements](journey-page-cache-performance.md) cover separate high-latency and three-client cases.
These runs do not test Fly hosting or physical-GPU rendering.
Software rendering dominates total browser-process CPU.
No whole-browser CPU gain follows from the server result.

Page captures last 60.26 to 60.33 seconds because collection follows the server pause.
Browser metrics and ACK samples therefore extend beyond the server window.
Proxy publication timestamps precede downstream delivery.
Per-page quantiles describe individual capture windows, not pooled distributions.
Endpoint RSS does not measure long-term retention.
Ambient host contention and active profiling can affect timing.

[Per-arm measurements](measurements/admission-live-arms.csv) and [per-page measurements](measurements/admission-live-pages.csv) retain all runs.
[Metadata](measurements/admission-live.json) records frozen inputs, run times, and raw-result hashes.
Local profiles, probes, screenshots, and helpers remain in `~/.cache/agents/podsim/server-admission-live-20260930/`.
