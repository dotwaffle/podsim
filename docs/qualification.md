# Qualification results

These measurements use Go 1.27.1 on a Ryzen 5 3600 with 12 logical CPUs and Debian 13.
They compare the accepted `0f8f10f` baseline with the changes documented here.
Run the commands below to repeat the checks on another machine.
[Recorded performance samples](measurements/performance.json) retain the individual timing and size measurements.

## Scale and safety

The generated `scale100` scenario has 20 stations, including one parking station, and 100 pods in 138 berths.
The network has 316 lanes, including 276 curves.

| Check | Result |
| --- | --- |
| Scale progress, 20 seeded local trips | 20 completed, average wait 88.73 s, maximum 131.87 s |
| Dense traffic, 50 submitted trips | All 100 pods checked every tick for 1,800 ticks |
| Busy scenario, repeated schedule | Both runs completed 10 trips with identical final snapshots |
| Full parking, 40 submitted trips | 25 completed and 15 remained after 10 simulated minutes |

The long scale run checks separation, berth ownership, and speed once per simulated second.
The dense 30-second window checks every tick.
Neither test proves progress for every saturated network.

An initial fixture placed separate berth approaches too close before their modeled merge.
The dense check detected an 11.953 m gap, below the required 12 m.
The final fixture separates those approaches.
Crossing or nearby lanes do not create shared conflict resources automatically. Add explicit junctions where lanes must conflict.

```sh
mise exec -- go test ./internal/scenarios -run 'TestScale100|TestBusy|TestParking' -count=1 -v
mise exec -- go test ./internal/scenarios -run '^$' -bench BenchmarkScale100StepActiveTraffic -benchtime=6000x -count=3 -benchmem
```

The step benchmark starts 50 requests and measures exactly 6,000 steps per sample.
Three baseline samples took 553 to 567 microseconds per step, with 53.3 KB and 141 allocations per step.
Three optimized samples took 205 to 217 microseconds, with 26.4 KB and 59 allocations per step.
These are native simulation measurements, not browser frame rates.

Profiles identified repeated geometry allocation, route searches, and ownership-map scans.
The changes reuse immutable routes and lane lengths, use stack storage for curve samples, and scan ownership once per tick.
The route cache holds at most 4,096 entries. Snapshot copies remain detached from cached routes.

## Redistribution

Redistribution remains optional and off by default.
The comparison covers ten seeds, four demand patterns, and request intervals of 30, 45, and 60 seconds.
Each pair uses identical requests, initial state, and a 15-minute measurement window.
The example network starts pod 01 in Parking and pod 02 in Garden.
These policy experiments use two pods. The separate scale tests use 100 pods.
The report contains 120 pairs and 240 runs.

```sh
mise run compare -- -duration 15m -loads 30s,45s,60s -seeds 1,2,3,4,5,6,7,8,9,10 -patterns all -format csv -output /tmp/redistribution.csv
```

[All recorded runs](measurements/redistribution.csv) include served and remaining trips, skipped arrivals, pickup waits, empty distance, and positioning moves.
Wait measurements include elapsed waits for requests still pending at the window end.
They are not completed-trip-only averages.

The sweep found a deadlock in remote berth reservations.
A pickup pod could reach an inlet before the empty pod that reserved its berth, blocking both pods.
Remote redistribution claims now yield to passenger traffic before final-block admission.
Tests protect admitted resources, unrelated berths, and another pod's ownership.

The original sweep had ten enabled runs with zero completed trips. The corrected sweep has none.
The corrected policy reduced average wait in 75 pairs and increased it in 45 pairs.
Across all pairs, the mean wait change was -0.48 s, with six more completed trips and 936 m more empty travel per run.
These averages do not justify enabling the policy by default.

| Pattern, 30 pairs each | Mean average-wait change | Total completed-trip change | Mean empty-distance change |
| --- | ---: | ---: | ---: |
| Balanced | -4.92 s | +8 | +505 m |
| Destination | -16.65 s | +12 | +437 m |
| Hotspot | +6.26 s | -1 | +1,551 m |
| Bursty hotspot | +13.40 s | -13 | +1,253 m |

Changes are enabled minus disabled. Means weight each paired run equally.
Negative wait is better. Positive empty distance is additional travel.
The hotspot forecast describes pickup demand. The destination pattern concentrates arrivals instead.
No policy reads future requests from the generated schedule.

The earlier seed-7, 10-minute, 60-second hotspot example still performs worse with redistribution:

| Metric | Disabled | Enabled |
| --- | ---: | ---: |
| Average pickup wait | 86.35 s | 91.91 s |
| Maximum pickup wait | 139.07 s | 160.73 s |
| Empty travel | 4,047 m | 5,619 m |

## WASM loading

The build now creates `podsim.wasm.gz` with maximum gzip compression.
The server serves this artifact directly when the browser accepts gzip.
Missing or older artifacts fall back to compression during the request.
Snapshot compression remains at the fast setting. Range responses remain uncompressed.

| Baseline artifact variant | Raw bytes | Transferred bytes |
| --- | ---: | ---: |
| Standard Go build, request-time gzip | 28,737,787 | 8,154,231 |
| Same build, precompressed gzip | 28,737,787 | 6,630,784 |
| Debug-stripped build, request-time gzip | 28,047,963 | 7,995,685 |

Precompression reduced transferred bytes by 18.7% without removing debug data.
At an emulated 10 Mbps and 50 ms latency, median startup fell from 7.86 to 6.61 seconds.
Each variant used three fresh Chromium processes in alternating order.
Startup ends when the canvas exists and the loader status disappears.

Debug stripping saved only 1.9% of transferred bytes at the same compression setting.
The build retains debug data. TinyGo was not adopted or qualified.
Browser measurements used headless Chromium with SwiftShader software rendering, not a physical client GPU or a real Tailscale link.

```sh
mise run web
wc -c dist/podsim.wasm dist/podsim.wasm.gz
```

## Browser rendering

The original 100-pod map exhausted SwiftShader WebGL buffers before the first frame-rate sample.
Thousands of antialiased curve segments queued stencil data before rendering.

Networks above 100 lanes now use fewer screen-space curve segments and disable map antialiasing.
Small networks retain their original rendering detail.
A reusable 1100 by 760 image stores neutral tracks, arrows, and nodes.
The selected route, berths, pods, and status remain dynamic.
Server epoch or simulation generation changes rebuild the cached image.

On the paused scale fixture, the crash fix rendered at a median of about 14.9 FPS.
Static-track caching increased that median to 18.9 FPS in fresh SwiftShader runs.
Moving 100-pod runs at 1x and 8x sampled about 11 to 19 FPS.
The server advanced at about 59 and 445 simulation ticks per wall second, respectively.
These software-rendered results do not establish frame rates on a physical client GPU.
The 100-pod fixture still has overlapping map labels. Sidebar inspection and paged station controls remain available.
Pod selectors and map labels use compact fleet numbers, with the full ID retained in inspection.

Browser tests compared an applied project's cached image with a fresh render of that project.
Disabling the cache-refresh call caused the comparison to fail. Restoring it passed.
Additional checks covered reset, server restart, moving pods at 1x and 8x, and the original small-network editor workflow.

The [Ebitengine performance tips](https://ebitengine.org/en/documents/performancetips.html) describe draw batching and source-image reuse.
The static cache follows that approach and changes only when the network generation or server epoch changes.
The renderer does not read pixels back from the GPU.
For further diagnosis, use the `ebitenginedebug` build tag to inspect actual draw commands and batch boundaries.
That diagnostic was not part of these measurements.

## Shared-server traffic

A busy 100-pod snapshot measured 182,933 raw bytes and 27,042 gzip bytes.
At 20 snapshots per second, that is about 541 KB/s of compressed response bodies per browser.
A lighter sample measured 143,251 raw bytes and 21,270 gzip bytes.
These sizes include routes, queued requests, and the full network. They change during a run.

The optimized server reached about 414 simulation ticks per wall second during the 120-arrival-per-minute stress sample at 8x playback.
The configured target was 480 ticks per second.
A 20-arrival-per-minute sample reached about 461 ticks per second.
These short loopback samples used software rendering experiments on the same host and do not establish a maximum supported load.
The fixed-step benchmark above provides the controlled core comparison.

Snapshot deltas were not introduced in this change.
Static network data and route repetition remain possible targets if bandwidth becomes the next measured limit.

## Validation limits

The automated gate runs race tests, editor tests, lint, vet, vulnerability checks, and native and WASM builds.
Browser acceptance also covers editing, undo and redo, background calibration, import and export, apply, stale conflicts, and visible WASM rendering.
A known stale save no longer pauses another browser's running simulation.
Malformed imports preserve the current draft.
