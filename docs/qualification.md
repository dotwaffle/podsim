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

## Rail-hub burst experiment

The `rail-hub` preset has six passenger stations and one parking station.
Its 30-pod fleet starts with three pods at each passenger station and 12 pods
in parking. Each passenger station has six berths.

The recorded experiment sends five train-like bursts from the Rail Hub during
the first five minutes, then allows the network to drain for the rest of a
30-minute run. The first four bursts contain 12 requests. The last contains 11,
for 59 requests per run. Five seeds use identical off/on request schedules.

```sh
mise run scenario -- -preset rail-hub -output /tmp/podsim-rail-hub.json
mise run compare -- -project /tmp/podsim-rail-hub.json -pattern hub-burst -duration 30m -arrivals-for 5m -request-every 5s -burst-size 12 -seeds 1,2,3,4,5 -format csv -output docs/measurements/rail-hub.csv
```

[Recorded rail-hub runs](measurements/rail-hub.csv) include the station queue,
berth, wait, clearance, passenger-distance, empty-distance, and positioning
measurements for every arm. Station peaks are sampled once per simulated second
and immediately after each request burst.

| Five-seed mean | Redistribution off | Redistribution on | On minus off |
| --- | ---: | ---: | ---: |
| Served requests | 59 | 59 | 0 |
| Average pickup wait | 532.29 s | 528.32 s | -3.97 s |
| Maximum pickup wait | 1053.65 s | 1056.02 s | +2.37 s |
| Queue clearance after final arrival | 1054.0 s | 1056.4 s | +2.4 s |
| Passenger distance | 230.5 km | 230.4 km | -0.1 km |
| Empty distance | 271.0 km | 316.2 km | +45.2 km |
| Loaded distance | 45.96% | 42.15% | -3.81 points |
| Positioning moves | 0.0 | 16.2 | +16.2 |

Both policies served every request. Redistribution reduced mean pickup wait by
about four seconds, but it slightly increased mean maximum wait and queue-clearance
time. It also added 45.2 km of empty travel and reduced the loaded share of
distance by 3.81 percentage points. This workload does not justify enabling
redistribution by default.

A ten-minute arrival window scheduled 119 requests in the same 30-minute run.
Both policies served 69 and left 50 pending. That overload probe shows why the
recorded experiment uses a finite five-minute arrival window and reports drain
time instead of treating an undrained queue as completed capacity.

### Same-destination sharing

The rail-hub schedule also compares the default one-party policy with a limit
of four parties per pod. A party can join only while a pod is still boarding at
the same origin for the same destination. It never diverts an assigned pickup
pod, delays departure to wait for another party, or adds an intermediate stop.
The limit counts parties separately from passenger `PartySize`.

```sh
mise run compare -- -project /tmp/podsim-rail-hub.json -pattern hub-burst -duration 30m -arrivals-for 5m -request-every 5s -burst-size 12 -seeds 1,2,3,4,5 -sharing-limits 1,4 -format csv -output docs/measurements/rail-hub-sharing.csv
```

| Five-seed mean | Limit 1, redistribution off | Limit 4, redistribution off | Limit 4, redistribution on |
| --- | ---: | ---: | ---: |
| Served requests | 59 | 59 | 59 |
| Parties joining a boarding pod | 0 | 26.4 | 25.8 |
| Average pickup wait | 532.29 s | 186.27 s | 189.38 s |
| Maximum pickup wait | 1053.65 s | 537.31 s | 543.24 s |
| Queue clearance after final arrival | 1054.0 s | 492.0 s | 514.4 s |
| Peak pending requests | 48.0 | 35.2 | 35.8 |
| Occupied-pod distance | 230.5 km | 125.0 km | 128.3 km |
| Empty distance | 271.0 km | 137.6 km | 212.2 km |
| Loaded distance | 45.96% | 47.65% | 37.69% |

With redistribution off, sharing reduced mean wait by 65%, queue-clearance
time by 53%, and empty travel by 49%. All demand still completed. The lower
occupied-pod distance records physical pod movement, not passenger-kilometers;
several parties now use one movement. Redistribution again added empty travel
and slightly worsened wait and clearance, so it remains off by default.

Raw results are in
[`measurements/rail-hub-sharing.csv`](measurements/rail-hub-sharing.csv).

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

On September 21, 2026, one combined acceptance run imported a PNG, calibrated
two points to 200 meters, drew and undid a junction, changed a station, and
passed editor validation. It exported and reloaded the project with the
background intact, applied revision 2, resumed at 8x speed, submitted a journey
through the canvas controls, and observed completion. The run also toggled pod
following and reported no browser errors.

## Mesh and navigation follow-up

The revised scale preset has a four-row, five-column grid with directed streets and explicit junctions.
Station spurs keep stopped pods outside through traffic. Interior junctions provide alternate routes.
Capacity remains 100 pods and 138 berths, including one parking station with 24 berths.

Mesh qualification checks 20 scheduled journeys over a maximum of 20 simulated minutes.
A separate 180-second dense window checks pod separation every tick.
Geometry checks reject crossings between unrelated sampled lane segments.
These checks cover this fixture and demand schedule, not every possible traffic pattern.

Camera tests cover zoom anchoring, limits, drag thresholds, selection, follow centering, and cache invalidation.
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

A combined CPU profile of the current Station 19 queue-drain and dense-safety
tests took 25.95 wall seconds and collected 33.81 CPU-seconds. Resource release
used 32.0% of cumulative CPU. Its nested `retainResources` scan used 24.5%.
Route construction and search used 12.9%. The test-only safety oracle used
10.1%. The profile supports incremental held-resource release as the next
controller optimization. It does not support adding parallel sector workers.

Incremental release removed the full route-prefix and owner-map scans. Each pod
now records only its active route resources and their final release distances.
The same combined profile took 8.73 wall seconds and 16.04 CPU-seconds, 66% and
53% lower respectively. Resource release fell to 6.4% of cumulative CPU. The
complete non-race scenario package fell from 35.50 to 22.87 wall seconds. The
100-order Station 19 result remained exact: first delivery at 258.4 seconds,
last delivery at 1582.9 seconds, all pods idle at 2171.9 seconds, and a peak of
12 stopped pods.

```sh
mise exec -- go test -count=1 -run 'Station19QueueDrains|DenseSafety' -cpuprofile /tmp/podsim-scenarios-cpu.out -o /tmp/podsim-scenarios.test ./internal/scenarios
mise exec -- go tool pprof -top -nodecount=40 /tmp/podsim-scenarios-cpu.out
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
Passenger routes end at the station entry until the pod enters the final access lane.
The controller then chooses the reachable berth with the least assigned demand.
Late berth changes preserve admitted track.
Empty relocations yield unadmitted claims when local passenger traffic needs the same berth.
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

### Deferred berth assignment

A live demand run exposed a temporary 20/4/1/1/1/1 split across Station 19's six berth routes.
The first reachable berth received most trips after all berths had one assigned arrival.
One capture had 20 active trips for berth 1 while its departing pod still held the clearance resource.
The resource wait was correct, but the early berth assignments created the queue.

Passenger journeys now target the station entry instead of a berth.
The controller assigns a berth when the pod gets access to the final station lane.
Current occupants, reservations, and local assignments contribute to the berth load.
Berth order breaks equal-load ties, so the result remains deterministic.
An idle berth occupant still moves if an admitted passenger arrival needs its berth.

The comparison below uses the separate-access layout and the same fixed workloads.
The deferred runs retained every-tick separation and berth ownership checks.

| Orders | Orders/min | Assignment | First delivery | Last delivery | All idle | Peak stopped pods | Minimum separation (m) |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: |
| 40 | 4 | Journey start | 540.4 | 1271.8 | 1829.3 | 3 | 28.29 |
| 40 | 4 | Station access | 531.5 | 1271.8 | 1829.3 | 1 | 28.11 |
| 40 | 12 | Journey start | 363.1 | 964.2 | 1521.7 | 2 | 25.20 |
| 40 | 12 | Station access | 418.7 | 965.9 | 1523.5 | 2 | 27.24 |
| 100 | 12 | Journey start | 373.8 | 1763.1 | 2192.3 | 17 | 22.87 |
| 100 | 12 | Station access | 418.7 | 1778.1 | 2202.8 | 17 | 22.87 |

The 100-order deferred run used the six berths 27/26/14/17/8/8 times.
The earlier fixed run used them 37/26/7/14/1/15 times.
Final settlement changed by less than one percent, and peak stopped pods stayed at 17.
The first delivery took 45 seconds longer because more arrivals used deeper berths.

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

### Reservation lookahead

The Station 19 burst also compared the current two-tick reservation lookahead
with 0.25, 0.5, 1, and 2-second buffers. Each arm submitted 100 orders at 12
orders per minute. The run checked separation and berth ownership every tick.
It kept the 12-meter physical clearance unchanged.

```sh
mise exec -- go test -run '^$' -bench '^BenchmarkReservationLookahead$' -benchtime=1x -count=1 ./internal/scenarios
```

[Recorded lookahead results](measurements/reservation-lookahead.csv) contain the
completion, station-boundary, safety, and berth-use measurements for all five
arms.

| Lookahead | Last delivery | All idle | Entry stops | Exit stops | Entry blocked | Exit blocked | Berth spread |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0.033 s | 1583 s | 2172 s | 1 | 45 | 443.1 s | 1068.0 s | 28 |
| 0.250 s | 1583 s | 2172 s | 14 | 37 | 491.7 s | 975.5 s | 43 |
| 0.500 s | 1582 s | 2171 s | 1 | 44 | 444.7 s | 1154.0 s | 34 |
| 1.000 s | 1593 s | 2171 s | 2 | 49 | 459.9 s | 1316.0 s | 34 |
| 2.000 s | 1589 s | 2170 s | 1 | 117 | 478.3 s | 2435.0 s | 32 |

Every arm kept at least 22.87 meters between pod centers and had the same
12-pod peak stopped queue. The 0.25-second arm moved some blocking from the
exit to the entrance and increased total stop events. The 0.5-second arm was
effectively neutral. Longer buffers increased exit blocking. No arm provided a
clear flow improvement, so the production default remains two ticks.

## London AM peak sample

The London preset uses 2019 midweek NUMBAT OD weights for 94 modeled stations.
The test normalizes the 8,474 retained OD pairs within each of eight source
time bands.

The fixed AM peak run submitted 40 OD-weighted requests at five-second
intervals.
All 40 completed by 1,083.0 simulated seconds.
Average pickup wait was 58.744 seconds, and maximum pickup wait was 235.133
seconds.
The schedule SHA-256 is
`02d3b6086d3ee5f58c9cbdb5bb574c042e0b8cd911656ed2c98099ea595aa024`.

The London project also carries all eight bands as a portable OD profile.
A live-session test enables the project demand and verifies that the first
generated request has a positive weight in the selected AM peak band.
The profile is available from the project endpoint and is not repeated in
simulation state responses.

### London CPU profile

The same 40-request AM peak qualification was profiled before and after two
measured indexing changes. The simulation now reuses its immutable route
graph and lane geometry, indexes stations, and records each route lane's first
resource block. These indexes do not change routing, arbitration, or movement
ordering.

Wall time fell from 4.50 to 2.13 seconds. CPU samples fell from 4.72 to 1.97
seconds. The qualification retained the same completion and wait assertions.
The final profile had no remaining avoidable hotspot above 15 percent
cumulative CPU, so no parallel simulation path was added.

Raw measurements are in
[`measurements/london-profile.csv`](measurements/london-profile.csv).

```sh
mise exec -- go test -count=1 -run '^TestLondonAMPeakSampleCompletes$' -v ./internal/scenarios
```

This run checks completion, queue accounting, terminal berth assignment, and
pod separation once per simulated second.
London links and station paths have explicit separation groups.
The oracle excludes only pairs in different groups that do not share a
junction.
Unlabeled projects retain the original two-dimensional all-pairs check.
See [the London network notes](london.md) for the boundary and source details.
