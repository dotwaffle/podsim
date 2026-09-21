# Qualification results

These measurements use Go 1.27.1 on a Ryzen 5 3600 with 12 logical CPUs and Debian 13.
They compare the accepted `0f8f10f` baseline with the changes documented here.
The recorded scale measurements use the ring fixture from `fe8fbe7`, before the mesh and map-navigation changes.
Run the commands below to repeat the checks on another machine.
[Recorded performance samples](measurements/performance.json) retain the individual timing and size measurements.

## Scale and safety

The measured ring fixture has 20 stations, including one parking station, and 100 pods in 138 berths.
That network has 316 lanes, including 276 curves.
The `scale100` preset now uses a directed mesh. The performance benchmark retains the ring fixture for comparison.

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
The later navigation update adds pointer-centered zoom, drag pan, and Fit controls.
At overview scale, crowded stations use one marker and an occupancy count. Individual berths appear when their screen spacing permits.
Map drawing stays inside the viewport. Camera changes invalidate cached tracks and do not change the shared session.
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

## Mesh and navigation follow-up

The revised scale preset has a four-row, five-column grid with directed streets and explicit junctions.
Station spurs keep stopped pods outside through traffic. Interior junctions provide alternate routes.
Capacity remains 100 pods and 138 berths, including one parking station with 24 berths.

Mesh qualification checks 20 scheduled journeys over a maximum of 20 simulated minutes.
A separate 180-second dense window checks pod separation every tick.
Geometry checks reject crossings between unrelated sampled lane segments.
These checks cover this fixture and demand schedule, not every possible traffic pattern.

Camera tests cover zoom anchoring, limits, drag thresholds, selection, and cache invalidation.
Browser checks cover wheel zoom, drag pan, Fit, clipping, and unchanged shared-session revision.
The original ring performance measurements above do not measure the new mesh or camera implementation.

## Safety observation cost

Qualification reads pod values, berth state, completion counts, and the clock through a separate observation method.
It does not copy routes or passenger requests on every tick.
Full snapshots remain available for final results and diagnostics.
Both methods index berth occupants once instead of scanning the fleet for each berth.

Lifecycle tests compare observations with snapshots during pickup, boarding, travel, unloading, and completion.
An independent berth scan checks the shared berth-state builder.
Mutating an observation does not change the simulation.
Every-tick qualification still checks all 4,950 pod pairs and berth ownership.
The checks generate pairs in memory. They do not store a pair corpus.

Active-traffic race benchmarks measured median observation cost at 73.3 microseconds, versus 1,043 microseconds for the previous snapshot implementation.
The indexed snapshot implementation measured 169.4 microseconds in a separate sample set.
These measurements cover state observation, not simulation steps or the safety checks themselves.

The original-layout Station 19 regression still settled at exactly 1,963 simulated seconds.
Its race run took 297.85 wall seconds, compared with 475.6 seconds in the earlier qualification run.
Those wall times came from separate runs and include scheduling differences.
The complete scenario race suite passed in 394.32 seconds.

```sh
mise exec -- go test -race ./internal/scenarios -run '^$' -bench BenchmarkScale100SafetyState -count=3 -benchmem
mise exec -- go test -race ./internal/scenarios -count=1 -timeout=20m -v
```

## Separate station access

The scale preset now separates each station's road divergence and merge by 600 meters.
Arrival and departure lanes serve berth rows at 75-meter intervals.
Parking extends outside the grid, so its access lanes do not cross unrelated roads.
The longest parking inlet conflict section fell from about 680 meters to 13 meters.
The fleet remains 100 pods with 138 berths, 14 m/s lane speeds, and a 12-meter clearance requirement.
Road-only route lengths between the original road nodes remain unchanged.

The layout requires station-local paths through intermediate nodes.
Validation rejects paths through another station, another berth, or the wrong station entry or exit.
Late berth choice preserves admitted track.
Empty relocations yield remote claims to competing passenger trips until destination admission.
An empty relocation can also clear an idle pod that later occupies its destination.
Regression tests cover both claim orderings and eventual settlement.

The comparison below uses the same final controller for both layouts.
Orders target Station 19, with origins cycling through the other 18 passenger stations.
Each run checks every pod pair and berth ownership every tick, through final empty-pod settlement.
All runs completed every order and left all pods idle.
Times are simulated seconds from the start of each run.

| Orders | Orders/min | Layout | First delivery | Last delivery | All idle | Peak stopped pods | Minimum separation (m) |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: |
| 40 | 4 | Previous | 411.7 | 1313.8 | 1963.0 | 8 | 13.58 |
| 40 | 4 | Separate access | 540.4 | 1271.8 | 1829.3 | 3 | 28.29 |
| 40 | 12 | Previous | 281.7 | 1207.4 | 1832.9 | 14 | 13.58 |
| 40 | 12 | Separate access | 363.1 | 964.2 | 1521.7 | 2 | 25.20 |
| 100 | 12 | Previous | 281.7 | 2630.4 | 3184.6 | 52 | 13.58 |
| 100 | 12 | Separate access | 373.8 | 1763.1 | 2192.3 | 17 | 22.87 |

In the 100-order burst, last delivery improved by 33% and final settlement improved by 31%.
Arrivals used all six Station 19 berths.
First delivery took longer in all three cases because station access locations and travel paths changed.
These results establish progress for the tested workloads. They do not establish capacity under unlimited demand.

The automated suite retains the 40-order regression and adds the 100-order burst.
Both preserve every-tick physical checks.
The race task permits 30 minutes for the expanded suite.
CI permits 40 minutes for tests and the remaining build checks.
The final combined race run reached its earlier 20-minute limit after the 100-order burst and four other qualification tests passed.
The remaining tests passed in a separate 374.85-second race run, without repeating completed qualification work.
Together, the two runs cover every test in the suite.

A short ring benchmark measured median step cost at 192 microseconds, versus 173 microseconds before the controller changes.
The three samples used 1,000 steps each. They show a possible controller cost and do not measure the new mesh workload.
The observation savings above apply separately.

```sh
mise exec -- go test -race ./internal/scenarios -run 'TestScale100Station19' -count=1 -timeout=30m -v
mise run check
```
