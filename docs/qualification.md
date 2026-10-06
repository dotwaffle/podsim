# Qualification results

The historical London results below refer to LondonCentral (`london-central`).
The recorded measurements and source data have not changed.

These measurements use Go 1.27.1 on a Ryzen 5 3600 with 12 logical CPUs and Debian 13.
They compare the accepted `0f8f10f` baseline with the changes documented here.
The recorded scale measurements use the ring fixture from `fe8fbe7`, before the mesh and map-navigation changes.
Run the commands below to repeat the checks on another machine.
Code changes after a measurement can give different results.
[Recorded performance samples](measurements/performance.json) retain the individual step benchmark, WASM loading, and moving-browser measurements.

The [selected current-source policy rerun](policy-failures.md) records later platoon, sharing, and positioning outcomes separately.
The [expanded controller and capacity screen](controller-capacity.md) preserves queue-limit effects and unfinished long-run requests.
Neither report replaces the historical qualification matrices below or authorizes a default change.

## Scale and safety

The measured ring fixture has 20 stations, including one parking station, and 100 pods in 138 berths.
That network has 316 lanes, including 276 curves.
The `scale100` preset now uses a directed mesh.
The performance benchmark retains the ring fixture for comparison.

| Check | Result |
| --- | --- |
| Scale progress, 20 seeded local trips | 20 completed, average wait 88.73 s, maximum 131.87 s |
| Dense traffic, 50 submitted trips | All 100 pods checked every tick for 1,800 ticks |
| Busy scenario, repeated schedule | Both runs completed 10 trips with identical final snapshots |
| Full parking, 40 submitted trips | 25 completed and 15 remained after 10 simulated minutes |

The long scale run checks separation, berth ownership, and speed once per simulated second.
The recorded dense window checked every tick for 30 seconds.
Neither test proves progress for every saturated network.
The current scale tests use the mesh and a 180-second dense window.
The scale progress, busy, and parking tests now run for at most 20 simulated minutes.
The first command below therefore does not repeat the table results.

An initial fixture placed separate berth approaches too close before their modeled merge.
The dense check detected an 11.953 m gap, below the required 12 m.
The final fixture separates those approaches.
Crossing or nearby lanes do not create shared conflict resources automatically.
Add explicit junctions where lanes must conflict.

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
The route cache holds at most 8,192 entries.
Snapshot copies remain detached from cached routes.

## Redistribution

Redistribution remains optional and off by default.
The comparison covers ten seeds, four demand patterns, and request intervals of 30, 45, and 60 seconds.
Each pair uses identical requests, initial state, and a 15-minute measurement window.
The report contains 120 pairs and 240 runs.
The code at commit `b61bdb9` gives the recorded values.
In these results, `on` is the weighted redistribution policy, which the current code replaces with guarded positioning.
The command below gives different values with the current code.

Without `-project`, the compare command uses the example network with pod 01 in Parking and pod 02 in Garden.
These policy experiments use two pods.
The separate scale tests use 100 pods.

```sh
mise run compare -- -duration 15m -loads 30s,45s,60s -seeds 1,2,3,4,5,6,7,8,9,10 -patterns balanced,destination,hotspot,bursty-hotspot -format csv -output /tmp/redistribution.csv
```

Wait measurements include elapsed waits for requests still pending at the window end.
They are not completed-trip-only averages.

The sweep found a deadlock in remote berth reservations.
A pickup pod could reach an inlet before the empty pod that reserved its berth, blocking both pods.
Remote redistribution claims now yield to passenger traffic before final-block admission.
Tests protect admitted resources, unrelated berths, and another pod's ownership.

The original sweep had ten enabled runs with zero completed trips.
The corrected sweep has none.
The corrected policy reduced average wait in 75 pairs and increased it in 45 pairs.
Across all pairs, the mean wait change was -0.48 s.
Enabled runs completed six more trips in total and averaged 936 m more empty travel.
These averages do not justify enabling the policy by default.

| Pattern, 30 pairs each | Mean average-wait change | Total completed-trip change | Mean empty-distance change |
| --- | ---: | ---: | ---: |
| Balanced | -4.92 s | +8 | +504 m |
| Destination | -16.65 s | +12 | +437 m |
| Hotspot | +6.26 s | -1 | +1,551 m |
| Bursty hotspot | +13.40 s | -13 | +1,253 m |

Changes are enabled minus disabled.
Means weight each paired run equally.
Negative wait is better.
Positive empty distance is additional travel.

The hotspot forecast describes pickup demand.
The destination pattern concentrates arrivals instead.
No policy reads future requests from the generated schedule.

The earlier seed-7, 10-minute, 60-second hotspot example still performs worse with redistribution:

| Metric | Disabled | Enabled |
| --- | ---: | ---: |
| Average pickup wait | 86.35 s | 91.91 s |
| Maximum pickup wait | 139.07 s | 160.73 s |
| Empty travel | 4,045 m | 5,618 m |

## Rail-hub burst experiment

The `rail-hub` preset has six passenger stations and one parking station.
Its 30-pod fleet starts with three pods at each passenger station and 12 pods in parking.
Each passenger station has six berths.

The recorded experiment sends five train-like bursts from the Rail Hub during the first five minutes.
The network then drains for the rest of a 30-minute run.
The first four bursts contain 12 requests.
The last contains 11, for 59 requests per run.
Five seeds use identical off/on request schedules.
The code at commit `51a8105` gives the recorded values, and `on` is the weighted redistribution policy, which the current code replaces with guarded positioning.
The command below gives different values with the current code.

```sh
mise run scenario -- -preset rail-hub -output /tmp/podsim-rail-hub.json
mise run compare -- -project /tmp/podsim-rail-hub.json -pattern hub-burst -duration 30m -arrivals-for 5m -request-every 5s -burst-size 12 -seeds 1,2,3,4,5 -format csv -output docs/measurements/rail-hub.csv
```

The compare command samples station peaks once per simulated second and immediately after each request burst.

| Five-seed mean | Redistribution off | Redistribution on | On minus off |
| --- | ---: | ---: | ---: |
| Served requests | 59 | 59 | 0 |
| Average pickup wait | 532.29 s | 528.32 s | -3.97 s |
| Maximum pickup wait | 1053.65 s | 1056.02 s | +2.37 s |
| Queue clearance after final arrival | 1054.0 s | 1056.4 s | +2.4 s |
| Occupied-pod distance | 230.5 km | 230.4 km | -0.1 km |
| Empty distance | 271.0 km | 316.2 km | +45.2 km |
| Loaded distance | 45.96% | 42.15% | -3.81 points |
| Positioning moves | 0.0 | 16.2 | +16.2 |

Both policies served every request.
Redistribution reduced mean pickup wait by about four seconds, but it slightly increased mean maximum wait and queue-clearance time.
It also added 45.2 km of empty travel and reduced the loaded share of distance by 3.81 percentage points.
This workload does not justify enabling redistribution by default.

A ten-minute arrival window scheduled 119 requests in the same 30-minute run.
With seed 1, both policies served 69 and left 50 pending.
That overload probe shows why the recorded experiment uses a finite five-minute arrival window.
It also shows why the report gives drain time and does not count an undrained queue as completed capacity.

### Same-destination sharing

The rail-hub schedule also compares the default one-party policy with a limit of four parties per pod.
A party can join only while a pod is still boarding at the same origin for the same destination.
It never diverts an assigned pickup pod, delays departure to wait for another party, or adds an intermediate stop.
The limit counts parties separately from passenger `PartySize`.
The code at commit `ab4f7bc` gives the recorded values, and `on` is the weighted redistribution policy, which the current code replaces with guarded positioning.
The command below gives different values with the current code.

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

With redistribution off, sharing reduced mean wait by 65%, queue-clearance time by 53%, and empty travel by 49%.
All demand still completed.
The lower occupied-pod distance records physical pod movement, not passenger-kilometers.
Several parties now use one movement.
Redistribution again added empty travel and slightly worsened wait and clearance, so it remains off by default.

The raw data of a later run of this schedule at limits 1 and 4 is in git history.
For the London result, see [same-destination sharing in the London sweep](#same-destination-sharing-in-the-london-sweep).

## Congestion-aware routing experiment

The `scale100` mesh provides alternate paths between the Station 19 focus and the other passenger stations.
The experimental policy adds six seconds per owned track cell and 20 seconds for a stopped pod when it assigns a new route.
It holds one cost snapshot for five simulated seconds.
It does not change a route after departure, and it does not change movement or safety arbitration.

The final comparison fixes redistribution off and uses the same 59-request hub-burst schedule for each routing policy.
Three seeds run for 30 simulated minutes so free-flow traffic drains completely.

```sh
mise run scenario -- -preset scale100 -output /tmp/podsim-scale100.json
mise run compare -- -project /tmp/podsim-scale100.json -pattern hub-burst -duration 30m -arrivals-for 5m -request-every 5s -burst-size 12 -seeds 1,2,3 -redistribution-policies off -routing-policies free-flow,congestion -format csv -output docs/measurements/routing-policy.csv
```

| Three-seed mean | Free-flow | Congestion snapshot | Snapshot minus free-flow |
| --- | ---: | ---: | ---: |
| Served requests | 59.00 | 57.67 | -1.33 |
| Remaining requests | 0.00 | 1.33 | +1.33 |
| Average pickup wait | 326.66 s | 328.14 s | +1.48 s |
| Peak pending requests | 47.0 | 47.0 | 0.0 |
| Empty distance | 382.2 km | 388.2 km | +6.0 km |
| Peak focus entrance stops | 1.00 | 1.33 | +0.33 |

The snapshot policy performed worse.
Its lower maximum wait and earlier pending-queue clearance are not benefits because fewer active journeys finish within the window.
Free-flow remains the default.
Keep the experimental arm for future work with measured lane travel times or junction-level delay, not as a user-facing routing mode.

The code at commit `b82b788` gives the values in the table, and the current compare command can give different values.
The raw data of a later run of the free-flow and congestion arms on this schedule is in git history.
The current `congestion` arm also has the two guards of the [queue routing screen](#queue-routing-screen).

## Queue routing screen

This screen compares free-flow routing, the `congestion` policy, and the `queue` policy.
The code at commit `d8393c1` gives the recorded values.

Both costed policies now have two guards.
A costed route does not go through the berths of a station other than the stations at its ends.
Costs apply only to the route that a pod gets when it boards, starts an empty move, diverts to a pickup, or parks after a release.
Estimates and the choices of dispatch, positioning, and parking use free-flow times.
Thus the `congestion` arm is different from the arm in the section above.

The `queue` policy counts the stopped pods with a wait reason on each lane when it assigns a route.
Each stopped pod adds 3 s to the time that the queue on its lane needs to clear.
The route search adds only the part of that time that remains when the pod gets to the start of the lane.
The policy changes the free-flow route only when the saving is at least 15 s and at least 5%.
The new route must also take at most 1.2 times the free-flow time of the free-flow route.
A screen with 5 s for each stopped pod ran on rail-hub, scale100, and the London sets with seeds 1 and 3.
Each of its rows was equal to the row with 3 s, so the full sweeps use 3 s.

The London-192 project has 192 pods, two at each passenger station, and no pods in parking at the start.
It is the first London arm in which track congestion limits service.

```sh
mise run scenario -- -preset rail-hub -output /tmp/podsim-rail-hub.json
mise run scenario -- -preset scale100 -output /tmp/podsim-scale100.json
mise run scenario -- -preset london-central -output /tmp/podsim-london.json
mise run scenario -- -preset london-central -station-berths 3 -berths 940GZZLUEMB=2 -parking-berths 24 -station-pods 2 -parking-pods 0 -output /tmp/podsim-london-192.json
mise run compare -- -project /tmp/podsim-rail-hub.json -pattern hub-burst -duration 30m -arrivals-for 5m -request-every 5s -burst-size 12 -seeds 1,2,3,4,5 -redistribution-policies off -routing-policies free-flow,congestion,queue -stop-when-drained -format csv -output docs/measurements/routing-queue-rail-hub.csv
mise run compare -- -project /tmp/podsim-scale100.json -pattern hub-burst -duration 30m -arrivals-for 5m -request-every 5s -burst-size 12 -seeds 1,2,3 -redistribution-policies off -routing-policies free-flow,congestion,queue -stop-when-drained -format csv -output docs/measurements/routing-queue-scale100.csv
mise run compare -- -project /tmp/podsim-london-192.json -pattern profile -bands early,am-peak -duration 65m -arrivals-for 30m -loads 6s,5s,4s,3s -seeds 1,2,3,4,5,6,7,8,9,10 -redistribution-policies off -routing-policies free-flow,congestion,queue -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -workers 5 -format csv -output docs/measurements/routing-queue-london-192.csv
mise run compare -- -project /tmp/podsim-london.json -pattern profile -bands all -duration 65m -arrivals-for 30m -loads 60s,30s,20s,15s,12s,10s,8.571429s,7.5s,6.666667s,6s,5.454545s,5s,4.615385s,4.285714s,4s -seeds 1,2,3 -redistribution-policies off -routing-policies free-flow,congestion,queue -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -adaptive-limit -past-limit 1 -workers 5 -format csv -output docs/measurements/routing-queue-london-envelope.csv
```

The two London files join the output of one run for each policy, so their row order is different from the order of one run.
Each `queue` row of these runs was equal to its free-flow row apart from the `routing_policy` column.
Thus the two London files keep only the free-flow and `congestion` rows.
The full raw files, with the `queue` rows, are kept outside the repository.

### Rail-hub and scale100

In each rail-hub row, the `congestion` and `queue` arms are equal to free-flow.
At commit `d64c3e0`, before the guards, the `congestion` arm served a mean of 57.4 requests and drained 1 of 5 seeds, because its routes went through the berths of other stations.
In each scale100 row, the `queue` arm is equal to free-flow.

| Mean | Rail-hub, each arm | Scale100 free-flow and queue | Scale100 congestion |
| --- | ---: | ---: | ---: |
| Served requests | 59.00 | 59.00 | 59.00 |
| Mean wait | 539.0 s | 330.9 s | 332.2 s |
| 95th percentile wait | 1,025.0 s | 547.3 s | 554.1 s |
| Mean journey | 838.0 s | 766.9 s | 770.2 s |
| Empty distance | 249.5 km | 322.1 km | 323.8 km |
| Total distance | 480.3 km | 668.1 km | 671.4 km |
| End of the last journey | 1,698 s | 1,408 s | 1,414 s |
| Seeds that drain within 30 minutes | 5 of 5 | 3 of 3 | 3 of 3 |

### London-192

Each of the 80 `queue` rows is equal to the free-flow row.
The table gives the ten-seed means.
Distance is the total of passenger and empty distance, as a multiple of free-flow.

| Band and rate | Served, free-flow and queue | Served, congestion | Mean journey, free-flow and queue | Mean journey, congestion | Distance, congestion |
| --- | ---: | ---: | ---: | ---: | ---: |
| Early 10/min | 299.0 | 299.0 | 881.0 s | 874.6 s | 1.048 |
| Early 12/min | 358.9 | 359.0 | 1,041.1 s | 985.7 s | 1.062 |
| Early 15/min | 406.9 | 431.5 | 1,197.0 s | 1,141.1 s | 1.121 |
| Early 20/min | 441.3 | 495.3 | 1,403.7 s | 1,362.3 s | 1.216 |
| AM peak 10/min | 299.0 | 299.0 | 485.8 s | 491.0 s | 1.018 |
| AM peak 12/min | 359.0 | 359.0 | 501.4 s | 506.3 s | 1.011 |
| AM peak 15/min | 449.0 | 449.0 | 520.8 s | 527.3 s | 1.012 |
| AM peak 20/min | 599.0 | 599.0 | 572.4 s | 589.8 s | 1.026 |

With free-flow, 9 of 10 Early seeds drain at 12/min, and no seed drains at 15 or 20/min.
With `congestion`, 10 of 10 Early seeds drain at 12/min.

### London envelope

Each of the 345 `queue` rows is equal to the free-flow row, so each band keeps its limit.

| NUMBAT band | Free-flow and queue limit | Congestion limit |
| --- | ---: | ---: |
| Early | 7/min | 7/min |
| Morning | 9/min | 9/min |
| AM peak | 13/min | 13/min |
| Interpeak | 14/min | 14/min |
| PM peak | 13/min | 13/min |
| Evening | 14/min | 13/min |
| Late | 12/min | 12/min |
| Night | 9/min | 8/min |

At rates at or below the free-flow limits, the `congestion` arm has band mean waits up to 18.6 s above free-flow.
Its 95th percentile waits are up to 13.2% above free-flow.

### Adoption

The adoption rule for a routing policy has these conditions:

1. Each free-flow result is equal to the result before the change.
2. Each London band keeps its limit.
3. No arm ends after 3,600 s when free-flow ends by 3,600 s, and each rail-hub and scale100 seed drains within 30 minutes.
4. No arm serves fewer requests than free-flow.
5. The policy shows a gain: in London-192, the mean journey time falls by at least 2%, or the served requests increase.
   A higher London band limit also counts.
6. At rates at or below each band limit, no band mean wait is more than 2 s above free-flow, and no 95th percentile wait or journey time is more than 5% above free-flow.
7. Where both policies drain, the total distance is at most 1.05 times free-flow.
   Where free-flow does not drain, the total distance is at most 1.20 times free-flow and the served requests increase.
8. The wall time of the London envelope sweep is at most 1.15 times free-flow.

The `queue` policy is not adopted, because it shows no gain (condition 5).
It meets each other condition.
A replay of ten free-flow comparison groups, with a snapshot hash at each simulated second, gives identical results against commit `4f2ad0a`.
The two replay groups with `congestion` change, as the guards expect.
The envelope sweep with `queue` used 1.0% more user CPU time and 1.10 times the wall time of the free-flow sweep.
The London-192 sweep with `queue` used 2.1% less user CPU time.
Other jobs shared the host, so the wall times are approximate.

The `congestion` control arm serves more requests in London-192 Early.
At 20/min, its distance is 1.22 times free-flow, which is more than condition 7 permits.
It also fails conditions 2, 3, 4, and 6 in the London envelope.
It lowers the Evening and Night limits, some arms end after 3,600 s where free-flow does not, and Night at 9/min serves 1.67 fewer requests.
Free-flow remains the default.

### Why the queue policy changes no route

A probe counted the decisions of the policy in London-192 Early at 20/min with seed 1.
The policy assigned 1,087 routes.
For 75 routes, no pod was in a queue.
For 935 routes, each queue cleared in the model before the pod got to it.
For the other 77 routes, the only delay was on the first lane of the route, which is the departure lane of the pod in 75 cases.
Each alternative starts on that lane, so each search gave the free-flow route.

The model clears a queue of n pods in 3n s, but a queue in the simulation stays because more pods join it.
Scratch probes with 15 s and 30 s for each stopped pod in Early at 15 and 20/min changed the served requests by at most one.
A cost that comes from the planned routes of the pods can predict these queues, and it is the next candidate policy.

The raw results of the rail-hub, scale-100, and London 192 sweeps are in git history.
The raw results of the London envelope sweep are in [`measurements/routing-queue-london-envelope.csv`](measurements/routing-queue-london-envelope.csv).

## WASM loading

The build now creates `podsim.wasm.gz` with maximum gzip compression.
The server serves this artifact directly when the browser accepts gzip.
Later, the build stopped keeping the raw `podsim.wasm`.
The server decompresses `podsim.wasm.gz` in memory for a client without gzip.
Snapshot compression remains at the fast setting.
Range responses remain uncompressed.

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
The build retains debug data.
The project did not adopt or qualify TinyGo.
Browser measurements used headless Chromium with SwiftShader software rendering, not a physical client GPU or a real Tailscale link.

```sh
mise run web
wc -c dist/podsim.wasm.gz
gzip -dc dist/podsim.wasm.gz | wc -c
```

## Browser rendering

The original 100-pod map exhausted SwiftShader WebGL buffers before the first frame-rate sample.
Thousands of antialiased curve segments queued stencil data before rendering.

Networks above 100 lanes now use fewer screen-space curve segments and disable antialiasing for tracks, lane arrows, nodes, the selected route line, and the scale bar.
Small networks retain their original rendering detail.
Ebitengine 2.10 keeps the stencil data of antialiased vector drawing in an image that can be twice as wide as the screen.
It does not keep that image within the GPU texture limit.
A 2560 by 1440 window at a device pixel ratio of 2 has a 5120 by 2880 screen.
There, the image was 10240 pixels wide, and the game stopped in SwiftShader, which has an 8192 pixel limit.
Small networks now draw without antialiasing when that image would not fit in the GPU texture limit.
A screen wider or taller than the GPU texture limit stopped Ebitengine before the view drew, for example a 4200 by 2400 window at a device pixel ratio of 2.
The view now lowers the pixel ratio of the screen so that its longer side is one pixel smaller than the limit.
Ebitengine then scales the screen to the window.
Chrome can also make the WebGL drawing buffer smaller than the canvas.
It keeps each side within the GPU limit and the area at about 33.2 million pixels or less.
A 4000 by 2400 window at a device pixel ratio of 2 has an 8000 by 4800 canvas and gets a 7436 by 4461 drawing buffer.
Ebitengine 2.10 draws into the smaller drawing buffer, but it maps the cursor and touch positions with the full canvas size.
In that window, clicks and taps missed the controls, but the keyboard worked.
The browser build now multiplies each cursor and touch position by the drawing buffer size divided by the canvas size, on each axis.
When Chrome limits a side and then the area, the drawing buffer does not have the proportions of the screen, and Ebitengine adds a letterbox offset.
The browser build also corrects the positions for that offset.
For example, a 5000 by 2400 window at a device pixel ratio of 2 gets a 7524 by 4409 drawing buffer.
Without the offset correction, clicks in that window landed about 22 CSS pixels too low, and a click on Speed missed.
The screen still had the proportions of the window, so Ebitengine showed black bars above and below it.
For example, a 4200 by 2400 window at a device pixel ratio of 2 also gets a 7524 by 4409 drawing buffer.
The screen was 8191 by 4681 pixels, and each bar was about 30 CSS pixels high.
When the drawing buffer is smaller than the canvas, the view now gives the screen the size of the drawing buffer.
When a side is then larger than the GPU texture limit, both sides become smaller by the same factor.
Thus the screen fills the window and there are no black bars.
The browser stretches the drawing buffer to the window as before.
In that window, the horizontal scale is about 2.5 percent larger than the vertical scale.
Networks above 100 lanes also store neutral tracks, arrows, and nodes in a reusable image.
The measured runs used a fixed 1100 by 760 image.
The image now covers the map viewport and a margin around it, in device pixels.
The margin is a quarter of the shorter side of the viewport.
The margin becomes smaller when the image with the full margin would not fit in the GPU texture limit.
The selected route, berths, pods, and status remain dynamic.
A new server epoch, simulation generation, project revision, or server start ID draws the cached image again.
Pods, pod rings, berth rings, station markers, and route arrows are antialiased on all networks.
The view draws each of these shapes once into a small image for the current scale.
In each frame, it copies these images to the map and does not fill or stroke the shapes again.
It turns the route arrow image to the direction of each lane.
A change of the display unit or of the station marker radius draws the images again.

On the paused scale fixture, the crash fix rendered at a median of about 14.9 FPS.
Static-track caching increased that median to 18.9 FPS in fresh SwiftShader runs.
Moving 100-pod runs at 1x and 8x sampled about 11 to 19 FPS.
The server advanced at about 59 and 445 simulation ticks per wall second, respectively.
These software-rendered results do not establish frame rates on a physical client GPU.

The later navigation update adds pointer-centered zoom, drag pan, zoom buttons, and a Fit control.
At overview scale, crowded stations use one marker and an occupancy count.
Individual berths appear when their screen spacing permits.
Map drawing stays inside the viewport.
Camera changes do not change the shared session.
A zoom draws the cached tracks again.
A pan or Follow moves the cached image on the screen.
The view draws the image again only when the pan passes the margin of the image.
Pod buttons and map labels use compact fleet numbers, and inspection also shows the full pod ID.

Browser tests compared an applied project's cached image with a fresh render of that project.
Disabling the cache-refresh call caused the comparison to fail.
With the call restored, the comparison passed.
Additional checks covered reset, server restart, moving pods at 1x and 8x, and the original small-network editor workflow.

The [Ebitengine performance tips](https://ebitengine.org/en/documents/performancetips.html) describe draw batching and source-image reuse.
The static cache follows that approach.
It changes only when the server epoch, the simulation generation, the project revision, the server start ID, the map scale, the display unit, or the map viewport changes, or when a pan passes its margin.
The renderer does not read pixels back from the GPU.
For further diagnosis, use the `ebitenginedebug` build tag to inspect draw commands and batch boundaries.
That diagnostic was not part of these measurements.

The view builds a display index once for each server epoch, simulation generation, project revision, and server start ID.
It holds the node positions by node ID, the collapsed station anchors, the shortest berth spacing of each station, the station line lanes, and the label ranks.
Lane drawing, station text, and map picks then do not scan the 1,842 London nodes for each node lookup.
A SwiftShader check on London at 1100 by 760 CSS pixels compared the view before and after the moving image and the display index.
Other jobs used the host at the same time, so only the difference between the two builds is useful.
Three alternating runs of each build gave these mean frame rates, with the zoomed view at 3.8 times the `Fit` scale:

| Map | Before | After |
| --- | ---: | ---: |
| `Fit`, no input | 9.5 FPS | 9.8 FPS |
| `Fit`, drag | 9.5 FPS | 9.9 FPS |
| `Fit`, Follow | 9.0 FPS | 10.0 FPS |
| Zoomed, no input | 9.4 FPS | 8.7 FPS |
| Zoomed, drag | 5.8 FPS | 8.6 FPS |
| Zoomed, Follow of a moving pod | 5.8 FPS | 8.5 FPS |

At `Fit`, the camera cannot pan, so a drag and Follow do not move the map.

## Shared-server traffic

A busy 100-pod snapshot measured 182,933 raw bytes and 27,042 gzip bytes.
At 20 snapshots per second, that was about 541 KB/s of compressed response bodies per browser.
A lighter sample measured 143,251 raw bytes and 21,270 gzip bytes.
These sizes included routes, queued requests, and the full network.
They changed during a run.

The optimized server reached about 414 simulation ticks per wall second during the 120-arrival-per-minute stress sample at 8x playback.
The configured target was 480 ticks per second.
A 20-arrival-per-minute sample reached about 461 ticks per second.
These short loopback samples came from the software rendering experiments on the same host.
They do not establish a maximum supported load.
The fixed-step benchmark above provides the controlled core comparison.

This change did not add snapshot deltas.
The normalized protocol later removed the network and complete lane objects from each state response.
See [the client protocol](protocol.md) for the normalized frame measurements.

## Validation limits

The automated gate runs workflow validation, Markdown checks, race tests, the tests that skip under the race detector, the `test:web` tests, vet, lint, vulnerability checks, native and WASM builds, and embedded server tests.
Browser acceptance also covers editing, undo and redo, background calibration, import and export, apply, stale conflicts, and visible WASM rendering.
A known stale apply no longer pauses another browser's running simulation.
Malformed imports preserve the current draft.

On September 21, 2026, one combined acceptance run imported a PNG and calibrated two points to 200 meters.
It drew and undid a junction, changed a station, and passed editor validation.
It exported and reloaded the project with the background intact and applied revision 2.
It resumed at 8x speed, submitted a journey through the canvas controls, and observed completion.
The run also toggled pod following and reported no browser errors.

Rewind tests make a save point, record a reference run, and then rewind twice.
Each replay must match the reference state, safety observation, and demand stream at every simulated second.
The cases cover balanced demand, profile demand, a demo that ends in the replay, and London with redistribution.
The London case runs at 5 requests per minute, so that the replay window holds guarded moves.
A branch with one more journey must differ from the reference.

The London restore test runs AM peak demand at 20 requests per minute with guarded positioning.
At this rate, the gate is not active, so the test makes no guarded move.
From 70 simulated seconds, it saves and restores the simulation state five times at 5-second intervals.
Each `physical` restore must keep each pod in place and keep the order queue.
A `logical` restore of the last state must put each pod at its initial berth, with the expected queue and completion counts.
The last `physical` copy then runs for 30 simulated seconds, and it must pass the separation oracle each second and complete an order.
A simulation test restores a guarded run while a guarded move is under way.
A session test also saves a London session and restores it with the `physical` tier.
A London test with `drop-offs` sharing and a limit of 4 parties requests 120 AM peak journeys, four each second.
It checks the restore contract after each request and each tick, and it runs until pods unload at intermediate stops.

## Mesh and navigation follow-up

The revised scale preset has a four-row, five-column grid with directed streets and explicit junctions.
Station spurs keep stopped pods outside through traffic.
Interior junctions provide alternate routes.
Capacity remains 100 pods and 138 berths, including one parking station with 24 berths.

Mesh qualification checks 20 scheduled journeys over a maximum of 20 simulated minutes.
A separate 180-second dense window checks pod separation every tick.
Geometry checks reject crossings between unrelated sampled lane segments.
These checks cover this fixture and demand schedule, not every possible traffic pattern.

Camera tests cover zoom anchoring, limits, drag thresholds, selection, and follow centering.
Cache tests check when a pan moves the cached tracks and when the view draws them again.
They also check that rewinds, resets, and demand edits on the same network keep the zoom, pan, and pod following.
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
The checks generate pairs in memory.
They do not store a pair corpus.

Active-traffic race benchmarks measured median observation cost at 73.3 microseconds, versus 1,043 microseconds for the previous snapshot implementation.
The indexed snapshot implementation measured 169.4 microseconds in a separate sample set.
These measurements cover state observation, not simulation steps or the safety checks themselves.

The original-layout Station 19 regression still settled at exactly 1,963 simulated seconds.
Its race run took 297.85 wall seconds, compared with 475.6 seconds in the earlier qualification run.
Those wall times came from separate runs and include scheduling differences.
The complete scenario race suite passed in 394.32 seconds.

A combined CPU profile of the current Station 19 queue-drain and dense-safety tests took 25.95 wall seconds and collected 33.81 CPU-seconds.
Resource release used 32.0% of cumulative CPU.
Its nested `retainResources` scan used 24.5%.
Route construction and search used 12.9%.
The test-only safety oracle used 10.1%.

The profile supports incremental held-resource release as the next controller optimization.
It does not support adding parallel sector workers.

Incremental release removed the full route-prefix and owner-map scans.
Each pod now records only its active route resources and their final release distances.
The same combined profile took 8.73 wall seconds and 16.04 CPU-seconds, 66% and 53% lower respectively.
Resource release fell to 6.4% of cumulative CPU.
The complete non-race scenario package fell from 35.50 to 22.87 wall seconds.

Incremental release did not change the 100-order Station 19 burst result.
First delivery was at 258.4 seconds, last delivery at 1582.9 seconds, all pods were idle at 2171.9 seconds, and the peak was 12 stopped pods.
These values predate the release of pickup pods for new work and the zero pickup estimate for an idle pod at the pickup station, so the current run can give different values.

```sh
mise exec -- go test -count=1 -run 'Station19QueueDrains|DenseSafety' -cpuprofile /tmp/podsim-scenarios-cpu.out -o /tmp/podsim-scenarios.test ./internal/scenarios
mise exec -- go tool pprof -top -nodecount=40 /tmp/podsim-scenarios-cpu.out
mise exec -- go test -race ./internal/scenarios -run '^$' -bench BenchmarkScale100SafetyState -count=3 -benchmem
mise exec -- go test ./internal/scenarios -count=1 -timeout=30m -v
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
Passenger routes end at the station entry until the pod asks for a track reservation on the final access lane.
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
These results establish progress for the tested workloads.
They do not establish capacity under unlimited demand.

### Deferred berth assignment

A live demand run exposed a temporary 20/4/1/1/1/1 split across Station 19's six berth routes.
The first reachable berth received most trips after all berths had one assigned arrival.
One capture had 20 active trips for berth 1 while its departing pod still held the clearance resource.
The resource wait was correct, but the early berth assignments created the queue.

Passenger journeys now target the station entry instead of a berth.
The controller assigns a berth when the pod asks for a track reservation on the final access lane.
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
They skip under the race detector, and the `qualify` task runs them without it, with a 30-minute limit.
CI runs each check task in its own job and permits 40 minutes for each job.

The final combined race run reached its earlier 20-minute limit after the 100-order burst and four other qualification tests passed.
The remaining tests passed in a separate 374.85-second race run, without repeating completed qualification work.
Together, the two runs cover every test in the suite.

A short ring benchmark measured median step cost at 192 microseconds, versus 173 microseconds before the controller changes.
The three samples used 1,000 steps each.
They show a possible controller cost and do not measure the new mesh workload.
The observation savings above apply separately.

```sh
mise exec -- go test ./internal/scenarios -run 'TestScale100Station19' -count=1 -timeout=30m -v
mise run check
```

### More Station 19 berths

The `-berths` and `-berth-pitch` options of the scenario command can give Station 19 more berths.
At the 75-meter pitch, a 7th berth row at Station 19 comes within the 12-meter clearance of the Station 13 arrival chain.
With 8 berths, 68 meters is the largest pitch, in steps of 0.5 meters, that passes the layout audit.
The pitch is one value for all stations of the preset.

A scratch copy of `TestScale100Station19BurstDrainsSafely` ran the 100-order burst at 12 orders per minute on three layouts.
It checked separation and berth ownership at each tick, and all orders completed.
These runs use the current code, so the 6-berth row is different from the rows above.
Times are simulated seconds from the start of each run.

| Station 19 berths | Pitch | First delivery | Last delivery | All idle | Peak stopped pods |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 6 | 75 m | 285.2 | 1540.4 | 1995.9 | 16 |
| 6 | 68 m | 282.7 | 1553.4 | 2017.1 | 15 |
| 8 | 68 m | 282.7 | 1563.1 | 2017.1 | 16 |

With 8 berths, berths 6, 7, and 8 took 6 of the 100 arrivals.
The berth count of Station 19 does not limit this burst.

```sh
mise run scenario -- -preset scale100 -berths station-19=8 -berth-pitch 68 -output /tmp/podsim-station19-8.json
```

### Reservation lookahead

The Station 19 burst also compared the current two-tick reservation lookahead with 0.25, 0.5, 1, and 2-second buffers.
Each arm submitted 100 orders at 12 orders per minute.
The run checked separation and berth ownership every tick.
It kept the 12-meter physical clearance unchanged.

```sh
mise exec -- go test -run '^$' -bench '^BenchmarkReservationLookahead$' -benchtime=1x -count=1 ./internal/scenarios
```

The lookahead measurements cover the completion, station-boundary, safety, and berth-use results for all five arms.
The raw data is in git history.

| Lookahead | Last delivery | All idle | Entry stops | Exit stops | Entry blocked | Exit blocked | Berth spread |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0.033 s | 1583 s | 2172 s | 1 | 45 | 443.1 s | 1068.0 s | 28 |
| 0.250 s | 1583 s | 2172 s | 14 | 37 | 491.7 s | 975.5 s | 43 |
| 0.500 s | 1582 s | 2171 s | 1 | 44 | 444.7 s | 1154.0 s | 34 |
| 1.000 s | 1593 s | 2171 s | 2 | 49 | 459.9 s | 1316.0 s | 34 |
| 2.000 s | 1589 s | 2170 s | 1 | 117 | 478.3 s | 2435.0 s | 32 |

Every arm kept at least 22.87 meters between pod centers and had the same 12-pod peak stopped queue.
The 0.25-second arm moved some blocking from the exit to the entrance and increased total stop events.
The 0.5-second arm was close to neutral.
Longer buffers increased exit blocking.
No arm provided a clear flow improvement, so the production default remains two ticks.

## London AM peak sample

The London preset uses 2019 midweek NUMBAT OD weights for 94 of its 96 passenger stations.
`LondonCentralDemand` normalizes the 8,474 retained OD pairs within each of eight source time bands.

The fixed AM peak sample is the schedule of `TestLondonAMPeakSampleCompletes`.
It submits 40 OD-weighted requests from the AM peak band at five-second intervals, with seed 20260922.
The schedule SHA-256 is `02d3b6086d3ee5f58c9cbdb5bb574c042e0b8cd911656ed2c98099ea595aa024`.
This sample is not an arm of the capacity sweep below.

The code at commit `3b02de8` gives the recorded values.
All 40 requests completed by 1,411.0 simulated seconds.
Average pickup wait was 56.203 seconds, and maximum pickup wait was 389.950 seconds.
Later dispatch changes release pickup pods for new work and give an idle pod at the pickup station a zero pickup estimate.
Thus the current code gives different values, so do not compare these values with current runs.

The London project also carries all eight bands as a portable OD profile.
A live-session test enables the project demand and verifies that the first generated request has a positive weight in the selected AM peak band.
The `/api/project` endpoint provides the profile.
State frames do not repeat it.

### London CPU profile

CPU profiles measured the same 40-request AM peak qualification before and after two indexing changes in commit `49bc70a`.
The simulation now reuses its immutable route graph and lane geometry, indexes stations, and records each route lane's first resource block.
These indexes do not change routing, arbitration, or movement ordering.

Wall time fell from 4.50 to 2.13 seconds.
CPU samples fell from 4.72 to 1.97 seconds.
The qualification gave the same completion time and wait values before and after the changes.
The final profile had no remaining avoidable hotspot above 15 percent cumulative CPU, so the project did not add a parallel simulation path.

The raw measurements are in git history.

```sh
mise exec -- go test -count=1 -run '^TestLondonAMPeakSampleCompletes$' -v ./internal/scenarios
```

This run checks pod separation and berth ownership once per simulated second.
At the end, it checks completion, queue accounting, and terminal berth assignment.
London guideways, movement lanes, and station paths have explicit separation groups.
The oracle excludes only pairs in different groups that do not share a junction.
Unlabeled projects retain the original two-dimensional all-pairs check.
See [the London network notes](london.md) for the boundary and source details.

#### Route search and dispatch scans

Two later commits made London compare runs faster, and the output did not change.
The heavy arm below is a London compare arm with the PM peak band, one request every 5 seconds, and seed 1.
It accepts requests for 30 minutes and stops when all requests complete, or at 65 minutes.
Redistribution is off.

Commit `40fc98f` finds the missing routes to all berths of a station with one route search.
It also raises the route cache limit from 4,096 to 8,192 routes.
In a replay of the route requests of the heavy arm, the number of route searches fell from 24,446 to about 13,000.
The user CPU time of the heavy arm fell by 2.7 to 3.3 percent.
The cache of 8,192 London routes uses about 26 MB, against about 19 MB for 4,096 routes.
The peak RSS of the heavy arm went from about 80 MB to about 106 MB.

Commit `80c47dc` skips two dispatch scans at a station with no idle pod.
Each dispatch pass makes a 256-bit Bloom filter of the stations of the idle pods, and the filter has no false negatives.
The two scans fell from 10 percent to less than 1 percent of the CPU profile of the heavy arm.
The filter uses about 1 percent.
In three alternate runs of the heavy arm, the mean user CPU time fell from 13.3 to 11.4 seconds.

The last A/B check compared commit `80c47dc` with commit `40fc98f`, so it includes the four commits between them.
In all 11 compare arms of the check, the CSV rows and a snapshot hash at each simulated second were identical.
The heavy arm took 7.1 seconds of wall time, against 10.1 seconds before.
The two builds ran at the same time on a shared host.
Thus these wall times do not compare with the profile times above.

## London portal comparison

The first London network joined all guideways and station access at one node per station.
Waterloo had 14 lanes on one junction resource.
Embankment had 10 lanes on one junction resource.
This design serialized unrelated directions and corridors.

The portal network gives each adjacency a separate arrival portal and departure portal.
Local movement lanes connect the portals.
Pods now share a portal only where their paths diverge or merge.

The comparison used one AM peak schedule with seed `20260922`.
It submitted 199 requests during a 10-minute window at one request every three seconds.
Both runs used the same schedule ID, `f13d587848b9176e`.
Redistribution and same-destination sharing were off.
The run stopped after the queue drained or after 60 simulated minutes.

| Network | Served | Remaining | Peak stopped pods | Waterloo entrance stopped | Waterloo exit stopped | Average wait | Maximum wait | Recovery after arrivals |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Shared station junction | 194 | 5 | 60 | 2 | 5 | 503.2 s | 3,039.1 s | Did not drain |
| Directional portals | 199 | 0 | 3 | 0 | 1 | 298.8 s | 1,152.9 s | 1,719 s |

This comparison tests one high-load demand pulse.
It confirms that the shared station node caused most stopped traffic in this run.
The multi-band sweep below measures the capacity envelope of the portal network.

## London capacity envelope

The sweep uses the London project's NUMBAT origin-destination profile on the directional-portal network at commit `cee7863`.
It covers all eight demand bands, 15 offered rates, and seeds 1, 2, and 3.
Each arm accepts requests for 30 simulated minutes, then runs until it finishes them or until 65 simulated minutes.
Redistribution and same-destination sharing are off.
Free-flow routing is on.
The queue limit is high enough that the compare command skips no request.
Two more sweeps use the same bands, rates, and seeds to compare the finishing-pod wait rules and redistribution.
See the subsections below.
The code at commit `d64c3e0` gives each row of the free-flow envelope again.

```sh
mise run scenario -- -preset london-central -output /tmp/podsim-london-capacity.json
mise run compare -- -project /tmp/podsim-london-capacity.json -pattern profile -bands all -duration 65m -arrivals-for 30m -loads 60s,30s,20s,15s,12s,10s,8.571429s,7.5s,6.666667s,6s,5.454545s,5s,4.615385s,4.285714s,4s -seeds 1,2,3 -redistribution-policies off -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -adaptive-limit -past-limit 1 -workers 5 -format csv -output docs/measurements/london-capacity.csv
```

The schedule starts after the first interval and excludes the arrival-window endpoint.
The compare command also truncates each interval to a whole 1/60-second tick.
This makes the 8.571429 s, 5.454545 s, 4.615385 s, and 4.285714 s intervals slightly shorter than their nominal values.
The report therefore gives these offered rates, rounded to three decimal places: 0.967, 1.967, 2.967, 3.967, 4.967, 5.967, 7.000, 7.967, 8.967, 9.967, 11.000, 11.967, 13.033, 14.000, and 14.967 requests per minute.
The table rounds these values to whole requests per minute.

The recovery limit is the highest tested rate at which all three seeds finish every accepted request within 60 minutes.
All three seeds must also finish at every lower tested rate.
In the CSV, `drained` is true when an arm finishes within the 65-minute run.
The limit uses `actual_end_seconds` instead, and an arm finishes within 60 minutes when this value is 3,600 or less.
With `-adaptive-limit`, each band stops one rate after its first rate at which a seed does not finish within 65 minutes.
The report omits the higher rates.
Because the adaptive stop uses the 65-minute run, each band also has rows above its 60-minute limit.

The rule for lower rates changes the Morning and PM peak limits.
Morning finishes all three seeds at 11/min and 12/min, but seed 1 at 10/min finishes at 3,642 seconds, so its limit is 9/min.
PM peak finishes all three seeds at 15/min, but seed 2 at 14/min finishes at 3,621 seconds, so its limit is 13/min.

Each metric column gives the mean of the three seeds at the limit rate.
Maximum wait is the mean of the three per-seed maxima.
Late throughput measures completions per minute during the second half of the arrival window.
Late backlog change compares outstanding requests at the midpoint and end of that window.

| NUMBAT band | Recovery limit | Late throughput | Late backlog change | Recovery after arrivals | Average wait | Maximum wait | Loaded distance | Next rate drained |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Early | 7/min | 5.13/min | +28.0 | 1,347 s | 377.0 s | 814.8 s | 46.0% | 2/3 at 8/min |
| Morning | 9/min | 8.02/min | +14.7 | 1,151 s | 173.5 s | 599.4 s | 64.1% | 2/3 at 10/min |
| AM peak | 13/min | 10.33/min | +41.0 | 1,665 s | 317.1 s | 1,140.3 s | 57.8% | 2/3 at 14/min |
| Interpeak | 14/min | 11.82/min | +32.7 | 1,555 s | 255.8 s | 938.1 s | 60.5% | 2/3 at 15/min |
| PM peak | 13/min | 11.02/min | +30.7 | 1,529 s | 250.5 s | 917.5 s | 58.9% | 2/3 at 14/min |
| Evening | 14/min | 10.33/min | +55.0 | 1,588 s | 356.3 s | 1,085.7 s | 59.2% | 2/3 at 15/min |
| Late | 12/min | 9.82/min | +32.7 | 1,439 s | 300.6 s | 1,031.7 s | 58.1% | 2/3 at 13/min |
| Night | 9/min | 7.40/min | +24.0 | 1,419 s | 213.3 s | 718.3 s | 53.4% | 2/3 at 10/min |

The `peak_active_vehicles` column counts the pods with assigned work.
A pod has assigned work when it has a trip that is not complete, or when a pending request names it as the pickup pod.
This includes boarding, travel with passengers, unloading, and travel to a pickup.
An empty move to parking or for redistribution is not work.
The `peak_passenger_vehicles` column counts only the pods with passengers aboard, so it is never more than `peak_active_vehicles`.

Most compare CSV files in `docs/measurements` come from reports before `schema_version` 6.
The platoon screening and queue routing files come from version 6, and the drop-offs files come from version 8.
The drop-offs envelope and Evening files come from version 9.
Thus the older files do not have the columns that version 6 adds, such as `wait_p95_seconds`, `journey_average_seconds`, `occupancy`, `stopped_pod_seconds`, and `peak_node`.
The platoon A/B file has the columns of version 6, and the `platoon_policy` and `coupled_time_percent` columns that version 9 adds.
The platoon capacity file comes from version 9, so it also has the rider columns of version 8.
The seat screen files come from version 10, so they also have the seat screen columns.
See [report columns](../README.md#report-columns) for the definitions.

At the limit rate, every seed has all 114 pods with work at the same time in AM peak, Interpeak, PM peak, Evening, and Late.
The highest seed peak at the limit rate is 104 pods in Early, 99 pods in Morning, and 105 pods in Night.
At the first rate above each limit, each seed that does not finish within 60 minutes has 112 to 114 pods with work.
The highest per-seed peak stopped pods at the limit rate is 9 in Early and 3 to 4 in the other bands.
Across all arms, peak stopped pods reach 19 in Early and 4 to 7 in the other bands.
For the seven bands other than Early, these results suggest that the 114-pod fleet, not track congestion, sets the recovery limit.
Early has more stopped pods, so congestion can also contribute to its limit.

OD mix explains most of the lowest limits.
At the lowest load, an Early journey uses 6.24 km of passenger travel and 7.06 km of empty travel on average.
A Night journey uses 5.23 km and 3.51 km.
The other bands use 4.71 to 5.73 km of passenger travel and 1.35 to 1.81 km of empty travel.
Early and Night need the most empty travel per journey.
Early has the lowest limit, and Night shares the second-lowest limit with Morning.
The Morning limit comes from one seed at 10/min that finishes 42 seconds after the cap.

These limits describe a finite 30-minute demand pulse with up to 30 minutes of recovery.
They are not continuous steady-state limits.
Backlog still grows in the second half of every limit-rate arm, so an operating target needs headroom.
The closest limit-rate arm finishes 5 seconds before the cap in AM peak.
The sweep does not prove that any tested rate can run indefinitely.

A cap a little after 60 minutes gives higher limits in some bands.
This table gives the limit for four caps.
The 65-minute column uses the full run.
The highest tested rate is 15/min, so a limit of 15/min is a lower bound.

| NUMBAT band | 60 min | 60 min 30 s | 61 min | 65 min |
| --- | ---: | ---: | ---: | ---: |
| Early | 7/min | 7/min | 7/min | 10/min |
| Morning | 9/min | 9/min | 12/min | 13/min |
| AM peak | 13/min | 13/min | 14/min | 14/min |
| Interpeak | 14/min | 14/min | 14/min | 15/min |
| PM peak | 13/min | 15/min | 15/min | 15/min |
| Evening | 14/min | 15/min | 15/min | 15/min |
| Late | 12/min | 13/min | 13/min | 14/min |
| Night | 9/min | 9/min | 9/min | 11/min |

Three limits rise when the cap is 30 seconds later, and five limits rise when it is 60 seconds later.
Compare the limits of two sweeps only when they use the same cap.

The previous sweep at commit `3b02de8` gave the same limits in Early, Morning, Late, and Night.
It gave lower limits in AM peak (11/min), Interpeak (13/min), PM peak (12/min), and Evening (12/min).
Later dispatch changes release pickup pods for new work.
They also give an idle pod at the pickup station a zero pickup estimate.

The shared-junction network at commit `5e556e0` had recovery limits of 2/min in Early, 3/min in Night, and 6 to 7/min in the other bands.
Across all arms, its peak stopped pods reached 44 to 91 per band.
In this sweep, no arm has more than 19 peak stopped pods.
Commit `ed5d782` also changed the berth layout, commit `3b02de8` changed the station headings, and later commits changed dispatch.
Thus this comparison does not isolate the portal change.
In Morning at the lowest load, empty travel per journey is 1.81 km, against 1.75 km on the shared-junction network.

The three sweeps ran at the same time on the qualification host, each with five workers.
The capacity sweep took 5,375 wall seconds for 345 arms.
The wait-rule sweep took 6,243 seconds for 675 arms, and the redistribution sweep took 5,610 seconds for 342 arms.
Each arm is independent and deterministic, so the number of workers does not change the rows.
Raw results are the free-flow rows of [`measurements/routing-queue-london-envelope.csv`](measurements/routing-queue-london-envelope.csv), which the [queue routing screen](#london-envelope) recorded again with more columns.
This document calls these rows the free-flow envelope.

### With virtual platoons

The London preset sets `platoonLimit` to 4, so the server runs London with virtual platoons.
A second sweep measures the envelope with platoons on.
It uses the bands, rates, seeds, and settings of the capacity sweep, with `-platoon-policies virtual` and ten workers.
The compare command does not read `platoonLimit` from the project, so the flag turns platoons on, with a limit of 4 pods.
The off baseline is the free-flow envelope above.
The sweep ran before the platoon option landed, and its simulation and compare code is the code of commit `9b89b07`.
These commands give the rows of the platoon CSV again:

```sh
mise run scenario -- -preset london-central -output /tmp/podsim-london-capacity.json
mise run compare -- -project /tmp/podsim-london-capacity.json -pattern profile -bands all -duration 65m -arrivals-for 30m -loads 60s,30s,20s,15s,12s,10s,8.571429s,7.5s,6.666667s,6s,5.454545s,5s,4.615385s,4.285714s,4s -seeds 1,2,3 -redistribution-policies off -platoon-policies virtual -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -adaptive-limit -past-limit 1 -workers 10 -format csv -output docs/measurements/london-capacity-platoons.csv
```

The sweep took 319 wall seconds for 345 arms.
The limits use the 60-minute rule of the capacity sweep.
The platoon time and the average waits are the means of the three seeds at the rate of the limit with platoons.

| NUMBAT band | Limit, off | Limit, platoons | Platoon time | Average wait, off | Average wait, platoons |
| --- | ---: | ---: | ---: | ---: | ---: |
| Early | 7/min | 10/min | 10.8% | 597.7 s | 545.3 s |
| Morning | 9/min | 9/min | 0.2% | 173.5 s | 174.6 s |
| AM peak | 13/min | 13/min | 0.2% | 317.1 s | 316.8 s |
| Interpeak | 14/min | 15/min | 0.4% | 319.9 s | 322.3 s |
| PM peak | 13/min | 13/min | 0.3% | 250.5 s | 246.5 s |
| Evening | 14/min | 14/min | 0.3% | 356.3 s | 353.5 s |
| Late | 12/min | 12/min | 0.3% | 300.6 s | 300.1 s |
| Night | 9/min | 9/min | 1.6% | 213.3 s | 213.4 s |

Platoons raise the Early limit from 7/min to 10/min and the Interpeak limit from 14/min to 15/min.
No band limit falls.
Early is the band where queues on the track set the limit, and its pods spend 10.8% of their travel time in a platoon at 10/min.
In the other bands, the fleet sets the limit, and the pods spend at most 2.3% of their travel time in a platoon in any arm.

The envelope rule is the rule of the drop-offs record.
At or below the platoon limit of each band, no arm with platoons ends after 3,600 seconds when the arm without platoons ends by 3,600 seconds.
Above the limits, 4 arms regress in this way:

| NUMBAT band | Rate | Seed | End, off | End, platoons |
| --- | ---: | ---: | ---: | ---: |
| AM peak | 15/min | 3 | 3,597 s | 3,610 s |
| AM peak | 14/min | 2 | 3,570 s | 3,777 s |
| Evening | 15/min | 1 | 3,516 s | 3,735 s |
| Late | 15/min | 3 | 3,596 s | 3,804 s |

In these 4 arms, the pods spend less than 0.5% of their travel time in a platoon.
Before the record, the 4 arms and two arms near them ran again without platoons at the code of the sweep.
Their rows without platoons are equal to the free-flow envelope in each column of the capacity sweep, so the platoons cause the regressions.
In 8 other arms, the arm with platoons ends by 3,600 seconds and the arm without platoons does not.
They are Early at 8/min with seed 1, at 9/min with seed 1, and at 10/min with seeds 1 and 3, Morning at 14/min with seed 2, Interpeak at 15/min with seed 3, and Night at 11/min with seeds 1 and 2.

The limits also change with a later cap.
This table gives the limits with platoons for the four caps of the capacity sweep.

| NUMBAT band | 60 min | 60 min 30 s | 61 min | 65 min |
| --- | ---: | ---: | ---: | ---: |
| Early | 10/min | 10/min | 10/min | 10/min |
| Morning | 9/min | 9/min | 13/min | 13/min |
| AM peak | 13/min | 13/min | 13/min | 14/min |
| Interpeak | 15/min | 15/min | 15/min | 15/min |
| PM peak | 13/min | 13/min | 15/min | 15/min |
| Evening | 14/min | 14/min | 14/min | 15/min |
| Late | 12/min | 12/min | 13/min | 13/min |
| Night | 9/min | 9/min | 9/min | 11/min |

With a cap of 60 minutes and 30 seconds, platoons give lower limits in PM peak (13/min against 15/min), Evening (14/min against 15/min), and Late (12/min against 13/min).
With a cap of 61 minutes, they give lower limits in AM peak (13/min against 14/min) and Evening (14/min against 15/min), and a higher limit in Morning (13/min against 12/min).
With both caps, the Early and Interpeak limits are higher with platoons, as with the 60-minute cap.
With the 65-minute cap, an arm must also drain, because each arm that does not drain stops at 3,900 seconds.
With this cap, platoons lower the Late limit from 14/min to 13/min, and the other limits are the same.
At Late 14/min with seed 2, the arm without platoons finishes its 420 requests at 3,856 seconds.
With platoons, the arm serves 419 of them by 3,900 seconds.
Raw results are in [`measurements/london-capacity-platoons.csv`](measurements/london-capacity-platoons.csv).

### Finishing-pod wait rules

Dispatch can hold a request for up to 30 seconds when a busy pod should reach the pickup more than two seconds before the available pod.
The compare command runs three rules for this hold with `-wait-rules`:

- `current` holds for a busy pod whose estimated finish plus empty travel to the pickup beats the available pod.
  The estimate can be longer than the hold.
  The server always uses this rule, and it is the default.
- `strict` holds only when that estimate is not more than the hold time that remains, and still beats the available pod.
- `none` never holds, and dispatch sends the available pod at once.

The report has a `wait_rule` column only when the command gets `-wait-rules`.
Without the flag, the CSV columns are the same as before the option.

The wait-rule sweep uses the bands, rates, and seeds of the capacity sweep with the `strict` and `none` rules.
Its CSV has no `current` rows, because the free-flow envelope gives them.

```sh
mise run compare -- -project /tmp/podsim-london-capacity.json -pattern profile -bands all -duration 65m -arrivals-for 30m -loads 60s,30s,20s,15s,12s,10s,8.571429s,7.5s,6.666667s,6s,5.454545s,5s,4.615385s,4.285714s,4s -seeds 1,2,3 -redistribution-policies off -wait-rules strict,none -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -adaptive-limit -past-limit 1 -workers 5 -format csv -output docs/measurements/london-wait-rules.csv
```

A new rule becomes the default only if it keeps or raises every band limit.
It must also not raise the average wait or the empty distance at the `current` limit rates.
The limits use the 60-minute rule from the capacity sweep.

| NUMBAT band | `current` limit | `strict` limit | `none` limit |
| --- | ---: | ---: | ---: |
| Early | 7/min | 8/min | 8/min |
| Morning | 9/min | 11/min | 11/min |
| AM peak | 13/min | 10/min | 12/min |
| Interpeak | 14/min | 14/min | 14/min |
| PM peak | 13/min | 12/min | 13/min |
| Evening | 14/min | 14/min | 10/min |
| Late | 12/min | 12/min | 12/min |
| Night | 9/min | 9/min | 9/min |

The next table gives the means of the three seeds at the `current` limit rate.

| NUMBAT band | Rate | Average wait, `current` | Average wait, `strict` | Average wait, `none` | Empty distance, `current` | Empty distance, `strict` | Empty distance, `none` |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Early | 7/min | 377.0 s | 365.9 s | 364.8 s | 1,558.4 km | 1,568.6 km | 1,568.7 km |
| Morning | 9/min | 173.5 s | 173.6 s | 174.3 s | 863.7 km | 899.2 km | 918.2 km |
| AM peak | 13/min | 317.1 s | 334.2 s | 345.9 s | 1,518.6 km | 1,578.3 km | 1,633.3 km |
| Interpeak | 14/min | 255.8 s | 297.5 s | 312.4 s | 1,363.2 km | 1,514.6 km | 1,543.4 km |
| PM peak | 13/min | 250.5 s | 280.0 s | 287.9 s | 1,362.3 km | 1,503.9 km | 1,533.3 km |
| Evening | 14/min | 356.3 s | 377.0 s | Not run | 1,538.0 km | 1,657.8 km | Not run |
| Late | 12/min | 300.6 s | 306.3 s | 307.9 s | 1,408.3 km | 1,474.1 km | 1,497.9 km |
| Night | 9/min | 213.3 s | 210.7 s | 216.6 s | 1,354.5 km | 1,402.4 km | 1,440.0 km |

Neither rule meets these conditions, so `current` stays the default.
Both rules raise the Early and Morning limits.
`strict` lowers the AM peak and PM peak limits, and `none` lowers the AM peak and Evening limits.
The adaptive run stops the Evening `none` group at 12/min, so the table has no `none` values for Evening at 14/min.
At the `current` limit rates, each rule adds empty distance in every band that has values.
Each rule also adds average wait in six bands.
The raw results are in git history.

### Redistribution in the London sweep

The redistribution sweep uses the bands, rates, and seeds of the capacity sweep with redistribution on.
The code at commit `ab59228` gives the recorded values, and `on` is the weighted redistribution policy, which the current code replaces with guarded positioning.
Redistribution uses the origin demand of the band as the station weights.
The `positioning_moves` column counts the redistribution moves.
The command below gives different values with the current code.

```sh
mise run compare -- -project /tmp/podsim-london-capacity.json -pattern profile -bands all -duration 65m -arrivals-for 30m -loads 60s,30s,20s,15s,12s,10s,8.571429s,7.5s,6.666667s,6s,5.454545s,5s,4.615385s,4.285714s,4s -seeds 1,2,3 -redistribution-policies on -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -adaptive-limit -past-limit 1 -workers 5 -format csv -output docs/measurements/london-redistribution.csv
```

The limits use the same 60-minute rule.
The other columns give the means of the three seeds at 1/min.

| NUMBAT band | Limit, off | Limit, on | Average wait, off | Average wait, on | Empty distance, off | Empty distance, on | Positioning moves |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Early | 7/min | 7/min | 124.2 s | 58.6 s | 204.6 km | 467.6 km | 51.0 |
| Morning | 9/min | 9/min | 38.2 s | 11.2 s | 52.6 km | 577.0 km | 73.0 |
| AM peak | 13/min | 12/min | 30.6 s | 11.7 s | 52.4 km | 607.9 km | 78.7 |
| Interpeak | 14/min | 14/min | 29.6 s | 10.4 s | 46.5 km | 606.4 km | 74.3 |
| PM peak | 13/min | 15/min | 30.7 s | 18.2 s | 47.0 km | 599.1 km | 74.0 |
| Evening | 14/min | 15/min | 32.1 s | 21.6 s | 39.2 km | 590.0 km | 76.7 |
| Late | 12/min | 12/min | 45.4 s | 16.0 s | 49.7 km | 610.7 km | 76.0 |
| Night | 9/min | 8/min | 49.2 s | 32.3 s | 101.7 km | 790.4 km | 96.7 |

At 1/min, redistribution cuts the average wait by 33 to 71 percent.
It also increases the empty distance by a factor of 2.3 to 15.0.
At the limit rates without redistribution, it changes the average wait by -16.5 to +1.2 seconds.
At these rates, it adds 8 to 29 percent to the empty distance.
It raises the PM peak and Evening limits to 15/min, which is the highest tested rate.
It lowers the AM peak and Night limits by one rate.
Redistribution stays off by default, because it lowers two band limits and adds much empty travel.

### Guarded positioning in the London sweep

Guarded positioning moves an idle empty pod to a demand station that has no pod.
It moves pods only while the request rate is low.
Its gate is active below one request per minute for each 20 pods of the fleet.
The London fleet has 114 pods, so the gate is active below 5.7/min.
At 6/min and more, a guarded arm runs as an off arm.

The gate also closes when the newest request is more than 180 seconds old, or when the positioning moves reach the boardings.
It closes when a waiting trip has no pod, or when more than two fifths of the fleet works.
The policy does not claim a berth that the route of a moving pod crosses after its claims.
A pickup diversion can start inside the berth access of a station, and a claim on that berth would make the two pods wait for each other.
An earlier version of the policy had this deadlock, and one arm with seed 6 did not drain.

The server runs the policy for a project with `redistribution: true`.
While generated demand runs, the server gives the gate the configured rate of the demand.
Otherwise, the gate reads the mean rate of the requests since the last reset.
The compare command runs the policy as `-redistribution-policies on`.
In `on` arms, `positioning_moves` counts the moves to demand stations.
It does not count the moves of an idle pod that blocks a berth.
In compare, the gate reads the mean rate of the accepted requests.
So compare turns the policy off for the rest of an arm at the first skipped arrival.

The guarded sweep uses the bands, rates, and seeds of the capacity sweep.

```sh
mise run compare -- -project /tmp/podsim-london-capacity.json -pattern profile -bands all -duration 65m -arrivals-for 30m -loads 60s,30s,20s,15s,12s,10s,8.571429s,7.5s,6.666667s,6s,5.454545s,5s,4.615385s,4.285714s,4s -seeds 1,2,3 -redistribution-policies on -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -adaptive-limit -past-limit 1 -workers 4 -format csv -output docs/measurements/london-guarded.csv
```

The code at commit `461b1c1` gives the recorded values.
At that commit, the policy had the name `guarded`, so the recorded `policy` column says `guarded`.

The policy had to meet these rules against the capacity sweep before it became a project setting:

1. Each guarded row at 6/min or more is equal to the off row in each column except `policy`.
2. Each band limit is not lower than the limit without the policy.
   The limits use the 60-minute rule.
3. No arm ends after 3,600 seconds when the off arm ends by 3,600 seconds.
4. At 1/min to 5/min, the mean wait of all arms is lower than off.
   No band mean is more than 2 seconds above off.
5. At 1/min to 5/min, the empty distance of each band is not more than 1.25 times off.
   No mean of a band and rate is more than 1.5 times off.
6. Each arm at 1/min to 5/min drains within 65 minutes.

The policy meets all six rules.
The 225 rows at 6/min or more are equal to the off rows, and no band limit changes.
The next table gives the means of the 15 arms of each band at 1/min to 5/min.

| NUMBAT band | Limit | Average wait, off | Average wait, guarded | Empty distance, off | Empty distance, guarded | Positioning moves |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Early | 7/min | 202.4 s | 196.5 s | 620.5 km | 621.7 km | 11.3 |
| Morning | 9/min | 85.7 s | 76.2 s | 243.9 km | 207.7 km | 11.5 |
| AM peak | 13/min | 65.1 s | 59.5 s | 233.3 km | 200.2 km | 13.2 |
| Interpeak | 14/min | 46.6 s | 36.1 s | 148.9 km | 140.8 km | 19.4 |
| PM peak | 13/min | 50.8 s | 39.6 s | 176.7 km | 145.1 km | 19.1 |
| Evening | 14/min | 62.4 s | 58.5 s | 193.8 km | 178.0 km | 10.2 |
| Late | 12/min | 81.9 s | 76.8 s | 215.6 km | 197.5 km | 8.3 |
| Night | 9/min | 92.9 s | 74.1 s | 397.3 km | 386.8 km | 24.0 |

Over the 120 arms at 1/min to 5/min, the mean wait falls from 85.96 seconds to 77.17 seconds.
The wait falls in each band, by 3.9 seconds in Evening to 18.8 seconds in Night.
The empty distance falls in seven bands and increases by 0.2 percent in Early.
The highest ratio for a band and rate is 1.039, at Interpeak 1/min.
Redistribution cuts the wait at 1/min more, but it multiplies the empty distance by 2.3 to 15.0.
The arm nearest to 3,600 seconds is Night 5/min with seed 3, which ends at 3,358 seconds, and at 3,384 seconds without the policy.

A second run compares off and guarded with seeds 4 to 10 at 1/min to 5/min, in 280 pairs.
All arms drain.
The mean wait falls from 82.20 seconds to 73.99 seconds, and the empty distance falls by 7.0 percent.

```sh
mise run compare -- -project /tmp/podsim-london-capacity.json -pattern profile -bands all -duration 65m -arrivals-for 30m -loads 60s,30s,20s,15s,12s -seeds 4,5,6,7,8,9,10 -redistribution-policies off,on -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -workers 4 -format csv -output /tmp/podsim-london-guarded-seeds.csv
```

A project with `redistribution: true` now runs guarded positioning.
The raw results are in git history.

### Same-destination sharing in the London sweep

This sweep uses the bands, rates, and seeds of the capacity sweep with the party limits 4 and 8.
The code at commit `d64c3e0` gives the recorded values.
At that commit, the compare command writes report `schema_version` 5.
Thus the CSV does not have the columns that version 6 adds.

```sh
mise run compare -- -project /tmp/podsim-london-capacity.json -pattern profile -bands all -duration 65m -arrivals-for 30m -loads 60s,30s,20s,15s,12s,10s,8.571429s,7.5s,6.666667s,6s,5.454545s,5s,4.615385s,4.285714s,4s -seeds 1,2,3 -redistribution-policies off -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -adaptive-limit -past-limit 1 -sharing-limits 4 -workers 6 -format csv -output /tmp/podsim-london-sharing-4.csv
mise run compare -- -project /tmp/podsim-london-capacity.json -pattern profile -bands all -duration 65m -arrivals-for 30m -loads 60s,30s,20s,15s,12s,10s,8.571429s,7.5s,6.666667s,6s,5.454545s,5s,4.615385s,4.285714s,4s -seeds 1,2,3 -redistribution-policies off -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -adaptive-limit -past-limit 1 -sharing-limits 8 -workers 6 -format csv -output /tmp/podsim-london-sharing-8.csv
```

The compare command accepts at most 1,000 arms in one run.
One run with the limits 1, 4, and 8 has 1,080 arms, so each limit has its own run.
The rate groups of `-adaptive-limit` include the party limit, so this split does not change the arms that run.
A run with limit 1 gives each row of the free-flow envelope again.

The limits use the 60-minute rule of the capacity sweep.
The 65-minute columns use the full run.
The highest tested rate is 15/min, so a limit of 15/min is a lower bound.

| NUMBAT band | Limit 1, 60 min | Limit 4, 60 min | Limit 8, 60 min | Limit 1, 65 min | Limit 4, 65 min | Limit 8, 65 min |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Early | 7/min | 9/min | 9/min | 10/min | 15/min | 15/min |
| Morning | 9/min | 9/min | 9/min | 13/min | 14/min | 14/min |
| AM peak | 13/min | 13/min | 13/min | 14/min | 15/min | 15/min |
| Interpeak | 14/min | 14/min | 14/min | 15/min | 15/min | 15/min |
| PM peak | 13/min | 13/min | 13/min | 15/min | 15/min | 15/min |
| Evening | 14/min | 15/min | 15/min | 15/min | 15/min | 15/min |
| Late | 12/min | 12/min | 12/min | 14/min | 13/min | 13/min |
| Night | 9/min | 11/min | 11/min | 11/min | 15/min | 15/min |

Sharing raises three 60-minute limits.
Early goes from 7/min to 9/min, Evening from 14/min to 15/min, and Night from 9/min to 11/min.
No 60-minute limit falls.
Limit 8 gives the same limits as limit 4 in all bands.
The 65-minute Late limit falls from 14/min to 13/min, because seed 2 at 14/min does not finish within 65 minutes with limit 4 or 8.

In Early with limit 4, all seeds finish within 60 minutes at 12/min to 15/min.
Seed 1 finishes at 3,700 seconds at 10/min and at 3,614 seconds at 11/min.
The rule for lower rates thus stops the Early limit at 9/min.
Morning stays at 9/min, because seed 1 at 10/min still finishes at 3,642 seconds.

The next table gives the means of the three seeds at the rate of the limit 1 envelope.
All arms in this table finish every request.
Maximum wait is the mean of the three per-seed maxima.
Shared parties is `shared_parties`, the parties that joined a boarding pod.

| NUMBAT band | Rate | Average wait, limit 1 / 4 / 8 | Maximum wait, limit 1 / 4 / 8 | Empty distance, limit 1 / 4 / 8 | Shared parties, limit 4 / 8 |
| --- | ---: | ---: | ---: | ---: | ---: |
| Early | 7/min | 377.0 / 302.0 / 302.0 s | 814.8 / 634.5 / 634.5 s | 1,558.4 / 1,336.0 / 1,336.0 km | 20.0 / 20.0 |
| Morning | 9/min | 173.5 / 173.2 / 173.2 s | 599.4 / 599.4 / 599.4 s | 863.7 / 852.1 / 852.1 km | 1.0 / 1.0 |
| AM peak | 13/min | 317.1 / 297.0 / 297.0 s | 1,140.3 / 1,077.4 / 1,077.4 s | 1,518.6 / 1,449.6 / 1,449.6 km | 9.0 / 9.0 |
| Interpeak | 14/min | 255.8 / 250.7 / 250.7 s | 938.1 / 975.7 / 975.7 s | 1,363.2 / 1,352.1 / 1,352.1 km | 4.0 / 4.0 |
| PM peak | 13/min | 250.5 / 235.8 / 235.8 s | 917.5 / 951.8 / 951.8 s | 1,362.3 / 1,293.9 / 1,293.9 km | 6.3 / 6.3 |
| Evening | 14/min | 356.3 / 333.1 / 333.1 s | 1,085.7 / 1,184.1 / 1,184.1 s | 1,538.0 / 1,512.0 / 1,512.0 km | 11.0 / 11.0 |
| Late | 12/min | 300.6 / 286.9 / 286.9 s | 1,031.7 / 949.7 / 949.7 s | 1,408.3 / 1,372.3 / 1,372.3 km | 4.7 / 4.7 |
| Night | 9/min | 213.3 / 158.7 / 154.9 s | 718.3 / 554.9 / 536.9 s | 1,354.5 / 1,165.4 / 1,150.6 km | 23.0 / 24.0 |

The average wait falls in each band.
The maximum wait increases in Interpeak, PM peak, and Evening.
Over the 345 arms that run at all three limits, limit 4 lowers the mean wait from 196.7 seconds to 171.1 seconds and the empty distance by 6.8 percent.
About 5 percent of the served parties share a pod at limit 4 or 8.
From 1/min to the limit rate, 5.2 percent of Early parties and 5.9 percent of Night parties share.
In the other six bands, 0.2 to 0.9 percent of the parties share.
In 331 of 360 arms, limit 8 gives the same row as limit 4, because no pod takes a fifth party.

Four arms at limit 4 end after 3,600 seconds when the limit 1 arm ends by 3,600 seconds.
They are Morning at 13/min with seeds 2 and 3, AM peak at 14/min with seed 2, and Late at 15/min with seed 3.
All four arms run above the band limit.

Sharing stays off by default.
The destination rows of the London drop-off sweep give the limit 4 rows again, and the free-flow envelope gives the limit 1 rows.

### Drop-offs sharing in London

In the `drop-offs` mode, a party can join a boarding pod that passes its destination or that can add it as a stop.
See [parties](../README.md#parties).
This measurement compares the mode with same-destination sharing at a party limit of 4, with the default stop limit of 3.
The code at commit `a06afef` gives the recorded values.
The compare command writes report `schema_version` 8.

```sh
mise run compare -- -project /tmp/podsim-london-capacity.json -pattern profile -bands all -duration 65m -arrivals-for 30m -loads 60s,30s,20s,15s,12s,10s,8.571429s,7.5s,6.666667s,6s,5.454545s,5s,4.615385s,4.285714s,4s -seeds 1,2,3 -redistribution-policies off -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -adaptive-limit -past-limit 1 -sharing-limits 4 -sharing-modes destination,drop-offs -workers 4 -format csv -output docs/measurements/london-drop-offs.csv
```

A run with `-sharing-limits 1` at the same commit gives each row of the free-flow envelope again.
The destination rows give each limit 4 row of the [same-destination sweep](#same-destination-sharing-in-the-london-sweep) again, and they add the columns of the later schema versions.
Thus the CSV of this measurement has only the limit 4 rows.

The measurement plan also has seeds 4 to 10 at the limit rates.
These seeds did not run.
After the sweep of seeds 1 to 3, the scope went down to the bands where sharing has the largest effect: Early, AM peak, Evening, and Night.
The confirmation with guarded positioning runs only these bands with seeds 1 to 3.
It uses the limit 1 recovery limit of each band, the two rates below it, and the two rates above it, up to 15/min.

The limits use the 60-minute rule of the capacity sweep.
The 65-minute columns use the full run.

| NUMBAT band | Limit 1, 60 min | Destination 4, 60 min | Drop-offs 4, 60 min | Limit 1, 65 min | Destination 4, 65 min | Drop-offs 4, 65 min |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Early | 7/min | 9/min | 15/min | 10/min | 15/min | 15/min |
| Morning | 9/min | 9/min | 14/min | 13/min | 14/min | 15/min |
| AM peak | 13/min | 13/min | 14/min | 14/min | 15/min | 15/min |
| Interpeak | 14/min | 14/min | 15/min | 15/min | 15/min | 15/min |
| PM peak | 13/min | 13/min | 14/min | 15/min | 15/min | 15/min |
| Evening | 14/min | 15/min | 15/min | 15/min | 15/min | 15/min |
| Late | 12/min | 12/min | 12/min | 14/min | 13/min | 13/min |
| Night | 9/min | 11/min | 11/min | 11/min | 15/min | 15/min |

The drop-offs mode raises five 60-minute limits and lowers none.
Early goes from 9/min to 15/min, and Morning from 9/min to 14/min.
It also raises the 65-minute Morning limit from 14/min to 15/min, and it lowers no 65-minute limit.

The next table gives the mean over the arms from 1/min to the 60-minute limit of the destination mode, with all three seeds.
All arms in this table finish every request.
Each cell gives the destination value, then the drop-offs value.
The empty distance is for each served request.
The detour ratios and the intermediate stops are for the drop-offs mode.
The destination mode has a detour ratio of 1 and no intermediate stop.
The pod journeys are the served parties less the shared parties.

| NUMBAT band | Rates | Average journey | Average wait | Maximum wait | Empty distance | Occupancy | Detour ratio, mean / maximum | Intermediate stops per pod journey |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Early | 1 to 9/min | 711.6 / 678.5 s | 246.4 / 208.1 s | 539.4 / 492.1 s | 6,547 / 5,881 m | 1.069 / 1.144 | 1.008 / 1.294 | 0.106 |
| Morning | 1 to 9/min | 531.9 / 529.8 s | 116.3 / 113.6 s | 471.9 / 468.3 s | 2,971 / 2,903 m | 1.002 / 1.008 | 1.001 / 1.289 | 0.014 |
| AM peak | 1 to 13/min | 526.8 / 520.8 s | 129.3 / 122.4 s | 572.0 / 553.3 s | 2,953 / 2,862 m | 1.002 / 1.011 | 1.002 / 1.509 | 0.021 |
| Interpeak | 1 to 14/min | 465.2 / 462.1 s | 96.6 / 92.9 s | 474.6 / 464.1 s | 2,209 / 2,153 m | 1.002 / 1.007 | 1.001 / 1.448 | 0.013 |
| PM peak | 1 to 13/min | 471.8 / 468.9 s | 106.1 / 102.8 s | 413.4 / 394.6 s | 2,477 / 2,431 m | 1.003 / 1.007 | 1.001 / 1.390 | 0.011 |
| Evening | 1 to 15/min | 552.1 / 544.3 s | 162.1 / 153.2 s | 553.9 / 534.3 s | 3,037 / 2,976 m | 1.006 / 1.017 | 1.002 / 1.390 | 0.027 |
| Late | 1 to 12/min | 555.4 / 552.2 s | 149.7 / 145.9 s | 468.5 / 456.1 s | 3,235 / 3,204 m | 1.002 / 1.008 | 1.001 / 1.411 | 0.015 |
| Night | 1 to 11/min | 547.8 / 547.3 s | 123.7 / 123.1 s | 478.2 / 477.4 s | 4,229 / 4,202 m | 1.068 / 1.072 | 1.001 / 1.237 | 0.009 |

Over these 288 arms, 1.8 percent of the served parties share a pod in the destination mode, and 3.6 percent in the drop-offs mode.
The journey, the wait, and the empty distance fall in each band.
The mean of the peak occupied berths at Euston changes by 0.07 or less in each band.

Five arms in the drop-offs mode end after 3,600 seconds when the destination arm ends by 3,600 seconds.
They are PM peak at 15/min with seeds 1 and 2, Late at 13/min with seed 1, and Night at 12/min and 15/min with seed 1.
All five arms run above the 60-minute limit of the drop-offs mode.
In 20 other arms, the drop-offs arm ends by 3,600 seconds and the destination arm does not.

The largest detour ratio in the sweep is 1.561, in Interpeak at 15/min with seed 3.
In the rates of the table, the largest is 1.509, in AM peak at 12/min with seed 2.

The guarded positioning confirmation gives the same result.

```sh
mise run compare -- -project /tmp/podsim-london-capacity.json -pattern profile -bands early -loads 12s,10s,8.571429s,7.5s,6.666667s -duration 65m -arrivals-for 30m -seeds 1,2,3 -redistribution-policies on -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -sharing-limits 4 -sharing-modes destination,drop-offs -workers 4 -format csv
```

The other bands use the same command with `-bands am-peak -loads 5.454545s,5s,4.615385s,4.285714s,4s`, `-bands evening -loads 5s,4.615385s,4.285714s,4s`, and `-bands night -loads 8.571429s,7.5s,6.666667s,6s,5.454545s`.
In the four bands, the drop-offs mode lowers the average journey and the average wait.
No drop-offs arm ends after 3,600 seconds when the destination arm ends by 3,600 seconds.
The largest detour ratio is 1.509 again.

The rail-hub schedule and a Scale100 check give no difference between the modes.

```sh
mise run compare -- -project /tmp/podsim-rail-hub.json -pattern hub-burst -duration 30m -arrivals-for 5m -request-every 5s -burst-size 12 -seeds 1,2,3,4,5 -sharing-limits 1,4 -sharing-modes destination,drop-offs -redistribution-policies off -workers 4 -format csv -output docs/measurements/rail-hub-drop-offs.csv
mise run compare -- -project /tmp/podsim-scale100.json -pattern destination -focus station-19 -duration 30m -arrivals-for 5m -request-every 5s -seeds 1,2,3 -sharing-limits 1,4 -sharing-modes destination,drop-offs -redistribution-policies off -workers 4 -format csv -output docs/measurements/scale100-drop-offs.csv
```

In the rail-hub schedule, the drop-offs rows are equal to the destination rows, and no pod adds a stop.
Limit 4 lowers the five-seed mean wait from 539.0 seconds to 227.6 seconds in both modes.
In the Scale100 check, no party joins a pod in either mode, because each request finds an idle pod.

The heavy arm is PM peak at 12/min with seed 1.
In three runs of each arm with one worker, the median user time is 8.08 seconds with limit 1, 8.61 seconds in the destination mode, and 8.20 seconds in the drop-offs mode.
The drop-offs arm ends at 2,695 seconds and the destination arm at 3,018 seconds.
For each simulated second, the drop-offs mode uses 6.7 percent more CPU time than the destination mode.

The design sets seven rules for the drop-offs mode against the destination mode with the same limit:

| Rule | Result |
| --- | --- |
| 1. With limit 1, each row and each snapshot hash is equal to the earlier code. | Met. The limit 1 and destination rows are equal to the recorded rows, and a snapshot hash check at each simulated second finds no difference. |
| 2. No London band limit is lower. | Met. No 60-minute or 65-minute limit is lower. |
| 3. No arm ends after 3,600 seconds when the destination arm ends by 3,600 seconds. | Not met. Five of 360 arms end after 3,600 seconds. |
| 4. The mean journey from 1/min to the limit rate is lower, and no band mean is more than 2 percent higher. | Met. The mean falls from 538.1 seconds to 531.5 seconds, and each band mean falls. |
| 5. The mean detour ratio is at most 1.15 in each band, and the maximum is at most 1.5. | Not met. The means are at most 1.008, but the maximum is 1.509 in AM peak. |
| 6. The empty distance for each served request is at most 1.05 times the destination value in each band. | Met. The ratio is 0.90 to 0.99. |
| 7. The CPU time of the heavy arm grows by less than 10 percent. | Met. The total falls by 4.8 percent. |

The drop-offs mode does not meet rules 3 and 5, so it does not become the mode that the editor offers first.
Sharing stays off by default, and this record keeps `destination` as the default mode.
The raw results are in git history.

#### Drop-offs with a detour cap

Since commit `b6d2b1d`, a pod in the `drop-offs` mode adds a stop only when the planned detour ratio of each party, new or aboard, is at most 1.5.
See [parties](../README.md#parties).
The plan uses the berth at each stop that gives the largest ratio, so the cap also refuses some joins that end with a measured ratio below 1.5.
A leg on a costed route and a change to another berth get the same check.
When a costed leg fails the check, the pod takes the free-flow leg, which the plan used.
When a berth change fails the check, the pod keeps its berth.
Thus the measured ratio of each party is at most 1.5 with each routing policy.
The cap changes only the `drop-offs` mode with a party limit of two or more.
The A/B harness ran the limit, shared, congestion example, and congestion London arms at commit `b6d2b1d` and at commit `afc2d65`.
It found equal rows and an equal snapshot hash at each simulated second.

The rule 3 decision changed after the first measurement.
Rule 3 now uses the envelope of the modes:

- In the reliable range of the destination mode, no drop-offs arm ends after 3,600 seconds when the destination arm ends by 3,600 seconds.
- The new higher limits of the drop-offs mode hold with more seeds.
- The record gives the regressions above the limits separately.

The six arms that did not meet rules 3 and 5 ran again with and without the cap.
Each end is in seconds.

| Arm | Destination end | Drop-offs end without the cap | Drop-offs end with the cap | Largest detour without / with the cap |
| --- | ---: | ---: | ---: | ---: |
| PM peak, 15/min, seed 1 | 3,303 | 3,707 | 3,707 | 1.219 / 1.219 |
| PM peak, 15/min, seed 2 | 3,439 | 3,611 | 3,611 | 1.367 / 1.367 |
| Late, 13/min, seed 1 | 3,279 | 3,627 | 3,627 | 1.288 / 1.288 |
| Night, 12/min, seed 1 | 3,267 | 3,680 | 3,680 | 1.189 / 1.189 |
| Night, 15/min, seed 1 | 3,538 | 3,742 | 3,742 | 1.360 / 1.360 |
| AM peak, 12/min, seed 2 | 3,017 | 3,004 | 3,004 | 1.509 / 1.258 |

The cap changes only the AM peak arm.
Its largest detour goes from 1.509 to 1.258, and its end does not change.
The cap does not change the five late arms.
In each of them, the last order to complete is a party that boarded its own pod.
It waits 980 to 1,230 seconds for a pod, and then it rides 710 to 970 seconds, for example from Walthamstow Central to Canary Wharf in the Night arms.
In four of these arms, all 114 pods travel at 1,800 seconds.
Thus the end depends on when a pod becomes free near the stations of the last orders.
The drop-offs mode ends journeys at other stations, and this changes that time.
The mean wait of the orders from the last 10 minutes of arrivals is lower in the drop-offs mode in four of the five arms.
All five arms run above the 60-minute limit of the drop-offs mode.

A targeted sweep then ran each band at the 60-minute limit of each mode from the first measurement, and at one rate below and above each limit, up to 15/min.
It used seeds 1 to 10.
The destination rows of seeds 1 to 3 and the drop-offs rows without the cap of seeds 1 to 3 come from the London drop-off sweep.
A run at commit `5371085` gives these rows again.

```sh
mise run compare -- -project /tmp/podsim-london-capacity.json -pattern profile -bands early -loads 7.5s,6.666667s,6s,4.285714s,4s -duration 65m -arrivals-for 30m -seeds 1,2,3,4,5,6,7,8,9,10 -redistribution-policies off -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -sharing-limits 4 -sharing-modes drop-offs -workers 10 -format csv
```

The other bands use the same command with these loads:

| NUMBAT band | Loads | Rates |
| --- | --- | --- |
| Early | `7.5s,6.666667s,6s,4.285714s,4s` | 8, 9, 10, 14, and 15/min |
| Morning | `7.5s,6.666667s,6s,4.615385s,4.285714s,4s` | 8, 9, 10, 13, 14, and 15/min |
| AM peak | `5s,4.615385s,4.285714s,4s` | 12 to 15/min |
| Interpeak | `4.615385s,4.285714s,4s` | 13 to 15/min |
| PM peak | `5s,4.615385s,4.285714s,4s` | 12 to 15/min |
| Evening | `4.285714s,4s` | 14 and 15/min |
| Late | `5.454545s,5s,4.615385s` | 11 to 13/min |
| Night | `6s,5.454545s,5s` | 10 to 12/min |

The command gave the drop-offs rows with the cap at the first version of the cap, which checked only the plan.
The A/B harness ran three of these arms at that version and at commit `b6d2b1d`: AM peak 14/min seed 1, Morning 13/min seed 2, and Night 11/min seed 1.
It found equal rows and an equal snapshot hash at each simulated second.
At commit `5371085`, with `-seeds 4,5,6,7,8,9,10 -sharing-modes destination,drop-offs`, the command gives the destination rows and the drop-offs rows without the cap.
The destination rows are the same with and without the cap.

The next table gives the number of the 10 seeds that finish every request by 3,600 seconds.

| NUMBAT band | Rate | Destination | Drop-offs without the cap | Drop-offs with the cap |
| --- | ---: | ---: | ---: | ---: |
| Early | 8/min | 10 | 10 | 10 |
| Early | 9/min | 10 | 10 | 10 |
| Early | 10/min | 9 | 10 | 10 |
| Early | 14/min | 10 | 10 | 10 |
| Early | 15/min | 10 | 10 | 10 |
| Morning | 8/min | 10 | 10 | 10 |
| Morning | 9/min | 10 | 10 | 10 |
| Morning | 10/min | 9 | 10 | 10 |
| Morning | 13/min | 5 | 9 | 9 |
| Morning | 14/min | 1 | 7 | 7 |
| Morning | 15/min | 0 | 5 | 5 |
| AM peak | 12/min | 9 | 10 | 10 |
| AM peak | 13/min | 8 | 10 | 10 |
| AM peak | 14/min | 4 | 9 | 7 |
| AM peak | 15/min | 3 | 5 | 5 |
| Interpeak | 13/min | 10 | 10 | 10 |
| Interpeak | 14/min | 9 | 9 | 9 |
| Interpeak | 15/min | 7 | 9 | 9 |
| PM peak | 12/min | 10 | 10 | 10 |
| PM peak | 13/min | 9 | 10 | 10 |
| PM peak | 14/min | 9 | 10 | 10 |
| PM peak | 15/min | 8 | 8 | 8 |
| Evening | 14/min | 9 | 8 | 8 |
| Evening | 15/min | 10 | 10 | 10 |
| Late | 11/min | 10 | 10 | 10 |
| Late | 12/min | 10 | 10 | 10 |
| Late | 13/min | 9 | 9 | 9 |
| Night | 10/min | 10 | 10 | 10 |
| Night | 11/min | 8 | 9 | 9 |
| Night | 12/min | 8 | 5 | 5 |

With 10 seeds, a limit is the highest tested rate at which all seeds finish by 3,600 seconds, with all seeds finishing at each lower tested rate.
The sweep tests only the rates near the limits, so some limits have a range.

| NUMBAT band | Destination, 3 seeds | Destination, 10 seeds | Drop-offs without the cap, 3 seeds | Drop-offs without the cap, 10 seeds | Drop-offs with the cap, 10 seeds |
| --- | ---: | ---: | ---: | ---: | ---: |
| Early | 9/min | 9/min | 15/min | 15/min | 15/min |
| Morning | 9/min | 9/min | 14/min | 10 to 12/min | 10 to 12/min |
| AM peak | 13/min | below 12/min | 14/min | 13/min | 13/min |
| Interpeak | 14/min | 13/min | 15/min | 13/min | 13/min |
| PM peak | 13/min | 12/min | 14/min | 14/min | 14/min |
| Evening | 15/min | below 14/min | 15/min | below 14/min | below 14/min |
| Late | 12/min | 12/min | 12/min | 12/min | 12/min |
| Night | 11/min | 10/min | 11/min | 10/min | 10/min |

In the Early band, the drop-offs limit of 15/min uses the rates from 11 to 13/min with seeds 1 to 3 only.
With 10 seeds, some limits are lower than with 3 seeds in both modes, because a rate near the limit finishes with some seeds and not with others.
The cap does not change a 10-seed limit.
It changes 12 of the 300 drop-offs arms.
At AM peak 14/min, two more seeds end after 3,600 seconds with the cap, and one seed ends earlier.

Over the 300 drop-offs arms with the cap, the largest detour ratio is 1.443.
Without the cap, it is 1.677.

The next table gives the mean over the arms at or below the 10-seed destination limit, with all 10 seeds.
Each cell gives the destination value, then the value with the cap.
AM peak and Evening have no tested rate at or below the destination limit.

| NUMBAT band | Rates | Average journey | Average wait | Empty distance | Detour ratio, mean / maximum |
| --- | ---: | ---: | ---: | ---: | ---: |
| Early | 8 and 9/min | 808.3 / 725.3 s | 335.4 / 240.0 s | 6,375 / 5,229 m | 1.020 / 1.365 |
| Morning | 8 and 9/min | 587.3 / 581.5 s | 164.4 / 157.4 s | 3,206 / 3,099 m | 1.003 / 1.294 |
| Interpeak | 13/min | 557.6 / 544.3 s | 191.5 / 177.0 s | 2,920 / 2,798 m | 1.003 / 1.320 |
| PM peak | 12/min | 549.6 / 540.5 s | 179.5 / 169.4 s | 3,000 / 2,923 m | 1.002 / 1.342 |
| Late | 11 and 12/min | 678.0 / 663.6 s | 270.3 / 253.9 s | 3,856 / 3,781 m | 1.004 / 1.369 |
| Night | 10/min | 620.2 / 617.9 s | 191.8 / 188.7 s | 4,472 / 4,492 m | 1.002 / 1.251 |

Over all 300 pairs of arms, 9 drop-offs arms with the cap end after 3,600 seconds when the destination arm ends by 3,600 seconds.
Each of the 9 arms runs above the 10-seed limits of both modes.
They are Morning at 13/min with seed 4, AM peak at 14/min with seed 5, PM peak at 15/min with seeds 1 and 2, Evening at 14/min with seed 6, Late at 13/min with seed 1, and Night at 12/min with seeds 1, 5, and 10.
In 35 other pairs, the drop-offs arm ends by 3,600 seconds and the destination arm does not.

The heavy arm is PM peak at 12/min with seed 1.
Another job used some of the host during the three runs of each arm with one worker.
These runs used a later build than the first measurement.
Do not compare their times with the times of the first measurement.
The median user time is 5.78 seconds with limit 1, 5.94 seconds in the destination mode, 6.01 seconds in the drop-offs mode with the cap, and 5.90 seconds without the cap.
The drop-offs arm ends at 2,695 seconds with and without the cap, and the destination arm at 3,018 seconds.
Thus the total grows by 1.2 percent, and the time for each simulated second grows by 2.8 percent.

| Rule | Result with the cap |
| --- | --- |
| 1. With limit 1, each row and each snapshot hash is equal to the earlier code. | Met. The A/B harness finds equal rows and hashes for limit 1 and for the destination mode. |
| 2. No London band limit is lower. | Met where the tested rates give the limits. No 10-seed drop-offs limit is lower than the destination limit. In Evening, both modes fail at 14/min and finish at 15/min, so the tested rates do not give the limit. |
| 3. Envelope rule. | Met. At or below the drop-offs limit of each band, no drop-offs arm ends after 3,600 seconds when the destination arm ends by 3,600 seconds. The 9 regressions above the limits are given above. |
| 4. The mean journey from 1/min to the limit rate is lower, and no band mean is more than 2 percent higher. | Met on the tested rates. The mean falls from 652.7 seconds to 627.1 seconds, and each band mean falls. |
| 5. The mean detour ratio is at most 1.15 in each band, and the maximum is at most 1.5. | Met. The means are at most 1.027, and the maximum is 1.443. |
| 6. The empty distance for each served request is at most 1.05 times the destination value in each band. | Met. The ratio is 0.82 to 1.004. |
| 7. The CPU time of the heavy arm grows by less than 10 percent. | Met. The total grows by 1.2 percent. |

With the cap and the envelope rule, the drop-offs mode meets the seven rules on this evidence.
Rule 4 uses only the tested rates near the limits, and rule 2 does not decide the Evening limit.
The next subsection closes these two points.
A change of the default mode changes the project contract, so this record does not change it.
Sharing stays off by default.
The raw results are in git history.
It has the drop-offs rows with the cap for seeds 1 to 10 and the destination rows for seeds 4 to 10.

#### Drop-offs envelope with the cap

Two more sweeps at commit `9b89b07` close the two open points of the cap record.
The first sweep runs the drop-offs mode with the cap over all bands, rates, and seeds of the first measurement, with the same flags.
Thus rule 4 uses each rate from 1/min to the destination limit.

```sh
mise run compare -- -project /tmp/podsim-london-capacity.json -pattern profile -bands all -duration 65m -arrivals-for 30m -loads 60s,30s,20s,15s,12s,10s,8.571429s,7.5s,6.666667s,6s,5.454545s,5s,4.615385s,4.285714s,4s -seeds 1,2,3 -redistribution-policies off -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -adaptive-limit -past-limit 1 -sharing-limits 4 -sharing-modes drop-offs -workers 10 -format csv -output docs/measurements/london-drop-offs-cap-envelope.csv
```

The second sweep runs Evening at 10 to 13/min with seeds 1 to 10 in both modes.

```sh
mise run compare -- -project /tmp/podsim-london-capacity.json -pattern profile -bands evening -duration 65m -arrivals-for 30m -loads 6s,5.454545s,5s,4.615385s -seeds 1,2,3,4,5,6,7,8,9,10 -redistribution-policies off -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -sharing-limits 4 -sharing-modes destination,drop-offs -workers 10 -format csv -output docs/measurements/london-drop-offs-evening.csv
```

The destination rows of the London drop-off sweep are the baseline.
At commit `9b89b07`, the destination arms of Early, AM peak, Evening, and Night at 7/min and 12/min with seed 1 give the recorded rows again.
The 12 destination rows of seeds 1 to 3 in the Evening sweep also give the recorded rows again.
The 90 drop-offs rows of seeds 1 to 3 in the capped London drop-off sweep are equal to the rows of the envelope.
The new files come from report `schema_version` 9, so they also have the `coupled_time_percent` column.

The next table gives the 60-minute limits with seeds 1 to 3.
The destination limits come from the first measurement.

| NUMBAT band | Destination | Drop-offs without the cap | Drop-offs with the cap |
| --- | ---: | ---: | ---: |
| Early | 9/min | 15/min | 15/min |
| Morning | 9/min | 14/min | 14/min |
| AM peak | 13/min | 14/min | 14/min |
| Interpeak | 14/min | 15/min | 15/min |
| PM peak | 13/min | 14/min | 14/min |
| Evening | 15/min | 15/min | 15/min |
| Late | 12/min | 12/min | 12/min |
| Night | 11/min | 11/min | 11/min |

The cap changes no limit with seeds 1 to 3.

The next table gives the number of the 10 Evening seeds that finish every request by 3,600 seconds.
The rows at 14/min and 15/min come from the targeted sweep.

| Rate | Destination | Drop-offs with the cap |
| ---: | ---: | ---: |
| 10/min | 10 | 10 |
| 11/min | 10 | 10 |
| 12/min | 10 | 10 |
| 13/min | 10 | 10 |
| 14/min | 9 | 8 |
| 15/min | 10 | 10 |

Thus the 10-seed Evening limit is 13/min in both modes.
With this limit, the 10-seed limits are:

| NUMBAT band | Destination | Drop-offs with the cap |
| --- | ---: | ---: |
| Early | 9/min | 15/min |
| Morning | 9/min | 10 to 12/min |
| AM peak | below 12/min | 13/min |
| Interpeak | 13/min | 13/min |
| PM peak | 12/min | 14/min |
| Evening | 13/min | 13/min |
| Late | 12/min | 12/min |
| Night | 10/min | 10/min |

The next table gives the mean over the arms from 1/min to the 60-minute destination limit, with all three seeds.
It uses the same arms and columns as the table of the first measurement.
All arms in this table finish every request.
Each cell gives the destination value, then the value with the cap.

| NUMBAT band | Rates | Average journey | Average wait | Maximum wait | Empty distance | Occupancy | Detour ratio, mean / maximum | Intermediate stops per pod journey |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Early | 1 to 9/min | 711.6 / 678.5 s | 246.4 / 208.1 s | 539.4 / 492.1 s | 6,547 / 5,881 m | 1.069 / 1.144 | 1.008 / 1.294 | 0.106 |
| Morning | 1 to 9/min | 531.9 / 529.8 s | 116.3 / 113.6 s | 471.9 / 468.3 s | 2,971 / 2,903 m | 1.002 / 1.008 | 1.001 / 1.289 | 0.014 |
| AM peak | 1 to 13/min | 526.8 / 520.8 s | 129.3 / 122.4 s | 572.0 / 553.1 s | 2,953 / 2,862 m | 1.002 / 1.011 | 1.002 / 1.333 | 0.021 |
| Interpeak | 1 to 14/min | 465.2 / 462.1 s | 96.6 / 92.9 s | 474.6 / 464.1 s | 2,209 / 2,153 m | 1.002 / 1.007 | 1.001 / 1.448 | 0.013 |
| PM peak | 1 to 13/min | 471.8 / 468.9 s | 106.1 / 102.8 s | 413.4 / 394.6 s | 2,477 / 2,431 m | 1.003 / 1.007 | 1.001 / 1.390 | 0.011 |
| Evening | 1 to 15/min | 552.1 / 544.3 s | 162.1 / 153.2 s | 553.9 / 534.3 s | 3,037 / 2,976 m | 1.006 / 1.017 | 1.002 / 1.390 | 0.027 |
| Late | 1 to 12/min | 555.4 / 552.2 s | 149.7 / 146.0 s | 468.5 / 452.2 s | 3,235 / 3,199 m | 1.002 / 1.008 | 1.001 / 1.291 | 0.014 |
| Night | 1 to 11/min | 547.8 / 547.3 s | 123.7 / 123.1 s | 478.2 / 477.4 s | 4,229 / 4,202 m | 1.068 / 1.072 | 1.001 / 1.237 | 0.009 |

Over these 288 arms, the mean journey falls from 538.1 seconds to 531.5 seconds, and each band mean falls by 0.08 to 4.65 percent.
The cap changes 7 of the 360 arms of the first measurement: four in Late, and one each in Early, AM peak, and Interpeak.
In this table, the values change only in AM peak and Late.
In AM peak, the largest detour ratio goes from 1.509 to 1.333.
Over all 15 rates, each band mean journey also falls, by 0.13 to 6.36 percent.
The largest rise at one rate is 1.75 percent, in Night at 12/min.

In the envelope, the largest detour ratio is 1.448, in Interpeak at 11/min with seed 3.
The largest mean detour ratio of one arm is 1.041.

For rule 3, the three sources give 598 pairs of arms: the envelope with seeds 1 to 3, the targeted sweep with seeds 4 to 10, and the Evening sweep with seeds 4 to 10.
In 10 pairs, the drop-offs arm ends after 3,600 seconds when the destination arm ends by 3,600 seconds.
They are the 9 arms of the targeted sweep, and Night at 15/min with seed 1 from the first measurement.
Each of the 10 arms runs above the 10-seed limits of both modes.
In 41 other pairs, the drop-offs arm ends by 3,600 seconds and the destination arm does not.

| Rule | Result with the cap |
| --- | --- |
| 1. With limit 1, each row and each snapshot hash is equal to the earlier code. | Met. The A/B harness finds equal rows and hashes for limit 1 and for the destination mode, and 20 destination rows at commit `9b89b07` are equal to the recorded rows. |
| 2. No London band limit is lower. | Met. No 60-minute limit with seeds 1 to 3 and no 10-seed limit is lower. Evening has the limit 13/min in both modes. |
| 3. Envelope rule. | Met. At or below the drop-offs limit of each band, no drop-offs arm ends after 3,600 seconds when the destination arm ends by 3,600 seconds. The 10 regressions above the limits are given above. |
| 4. The mean journey from 1/min to the limit rate is lower, and no band mean is more than 2 percent higher. | Met on each rate. The mean falls from 538.1 seconds to 531.5 seconds, and each band mean falls. |
| 5. The mean detour ratio is at most 1.15 in each band, and the maximum is at most 1.5. | Met. The band means are at most 1.008, and the maximum is 1.448. |
| 6. The empty distance for each served request is at most 1.05 times the destination value in each band. | Met. The ratio is 0.90 to 0.99. |
| 7. The CPU time of the heavy arm grows by less than 10 percent. | Met. The targeted sweep measured a growth of 1.2 percent. Later changes to the cap add checks only at a change to another berth and at a restore. |

With the cap, the drop-offs mode meets the seven rules on the full envelope with seeds 1 to 3 and on 10 seeds near the limits.
A change of the default mode changes the project contract, so the user decided it.
On 2026-09-28, the user adopted `drop-offs` as the default mode, on the rule table above.
Commit `14e4ed7` makes the change.
Sharing stays off by default, because the default party limit stays 1.
The raw results are in git history.

### More London berths

The scenario command can give London more berths.
A third berth fits at 95 of the 96 passenger stations.
At Embankment, a third berth row crosses a guideway.
This project also has 24 berths at each Parking facility, 2,235 nodes, and the same 114 pods.
The AM peak band ran with seeds 1, 2, and 3, in 30-minute arrival windows, with redistribution off.

| Offered rate | Average wait, preset | Average wait, 3 berths | Seeds that finish in 65 minutes, preset | Seeds that finish in 65 minutes, 3 berths |
| ---: | ---: | ---: | ---: | ---: |
| 12/min | 236.3 s | 241.4 s | 3 | 3 |
| 13/min | 317.1 s | 315.9 s | 3 | 3 |
| 15/min | 456.8 s | 463.7 s | 2 | 2 |

The added berths do not change the result, so the berths do not limit the AM peak with this fleet.

```sh
mise run scenario -- -preset london-central -station-berths 3 -berths 940GZZLUEMB=2 -parking-berths 24 -output /tmp/podsim-london-3.json
mise run compare -- -project /tmp/podsim-london-3.json -pattern profile -bands am-peak -loads 5s,4.615385s,4s -seeds 1,2,3 -duration 65m -arrivals-for 30m -stop-when-drained -redistribution-policies off -focus 940GZZLUEUS -queue-limit 1000000 -workers 4
```

### Platoon screening

A platoon lets pods run closer together, so it can add capacity only where track or junction flow limits the service.
The first two parts of this screening looked for such a load before the platoon code existed.
They are a synthetic corridor that gives the line capacity of the current rules, and a London sweep with more pods.
The third part is an A/B of virtual platoons on that load.

#### Corridor headway

`TestMergeCorridorHeadway` in `internal/sim` restores a stopped queue of 20 pods on each feed lane of a synthetic corridor.
All lanes have a 14 m/s limit and cells of up to 30 m.
The test gives the mean interval between pods from the sixth pod to the last.
It measures the merge cases with the node pass records that give `peak_node_throughput_per_minute`.

| Case | Headway | Pods per hour | Pods per minute |
| --- | ---: | ---: | ---: |
| Straight lane | 6.011 s | 599 | 9.98 |
| One stream through a lane boundary, or through a 30 degree merge | 7.217 s | 499 | 8.31 |
| Two streams, 90 or 30 degree merge | 8.650 s | 416 | 6.94 |
| Two streams, 15 degree merge | 10.783 s | 334 | 5.56 |

A pod reserves the last cell of a lane and the first cell of the next lane in one step.
Thus one stream through a lane boundary has a longer headway than a straight lane, also without a second stream.
The test pins these values.
A platoon or cell change must give its headway against them.

`TestMergeCorridorPlatoonHeadway` pins the same cases with virtual platoons of 2 and of 4 pods.
Each queue starts as linked platoons: the pods of a platoon are 18 m apart, and two platoons are 45 m apart.
The test checks the separation, the berths, the owners, the certificate, and the control rule of each link at each tick.

| Case | Off | Platoons of 2 | Platoons of 4 | Gain with platoons of 4 |
| --- | ---: | ---: | ---: | ---: |
| Straight lane | 6.011 s | 3.758 s | 2.415 s | 2.49 times |
| One stream through a lane boundary | 7.217 s | 4.120 s | 2.517 s | 2.87 times |
| Two streams, 90 degree merge | 8.650 s | 5.014 s | 3.131 s | 2.76 times |
| Two streams, 30 degree merge | 8.650 s | 4.925 s | 2.991 s | 2.89 times |
| Two streams, 15 degree merge | 10.783 s | 6.017 s | 3.511 s | 3.07 times |

The design estimated 2.4 times on a lane and 2.7 times at a 30 degree merge for platoons of 4.
The measured gains are 4% to 7% larger than these estimates.
Platoons of 2 give 1.60 to 1.79 times.

#### London with 198 pods

The sweep gives each London passenger station and each Parking facility two initial pods, 198 pods in total.
It uses the Early, AM peak, and PM peak bands with seeds 1, 2, and 3, and the settings of the capacity sweep.
The load list adds 17, 20, 24, 30, and 40 requests per minute, because the larger fleet finishes more rates.
The compare command at commit `4f2ad0a` gives the recorded values.
The sweep took 540 wall seconds with four workers.

```sh
mise run scenario -- -preset london-central -station-pods 2 -parking-pods 2 -output /tmp/podsim-london-198.json
mise run compare -- -project /tmp/podsim-london-198.json -pattern profile -bands early,am-peak,pm-peak -duration 65m -arrivals-for 30m -loads 60s,30s,20s,15s,12s,10s,8.571429s,7.5s,6.666667s,6s,5.454545s,5s,4.615385s,4.285714s,4s,3.5s,3s,2.5s,2s,1.5s -seeds 1,2,3 -redistribution-policies off -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -adaptive-limit -past-limit 1 -workers 4 -format csv -output docs/measurements/london-platoon-screening.csv
```

The design gives this rule for a load that track flow limits.
The off arms have peak stopped pods of at least 10, or junction wait of at least 10% of travel time.
The report has no travel time of the pods.
The table therefore divides each wait by the sum of the junction wait, the track wait, and the moving time at 14 m/s for the passenger and empty distance.
That moving time is a lower bound, so each share is an upper bound.

The limits use the 60-minute rule of the capacity sweep.
With 198 pods, the limit is 10/min in Early, 20/min in AM peak, and 24/min in PM peak.
With 114 pods, the limits are 7/min, 13/min, and 13/min.
Each value in the table is the mean of the three seeds, except the maxima of peak stopped pods, peak active pods, and peak node throughput.

| NUMBAT band | Rate | Served | Seeds that finish in 60 minutes | Peak stopped pods | Peak active pods | Junction wait share | Track wait share | Peak node throughput | Average wait | Average journey |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Early | 6/min | 179.0 | 3 | 4 | 74 | 0.2% | 0.1% | 8/min | 207.3 s | 677.9 s |
| Early | 7/min | 210.0 | 3 | 8 | 92 | 0.5% | 0.4% | 8/min | 231.8 s | 710.9 s |
| Early | 8/min | 239.0 | 3 | 11 | 113 | 0.6% | 1.1% | 8/min | 258.2 s | 751.9 s |
| Early | 9/min | 269.0 | 3 | 17 | 127 | 0.9% | 2.2% | 9/min | 290.0 s | 804.4 s |
| Early | 10/min | 299.0 | 3 | 24 | 143 | 1.1% | 4.4% | 8/min | 340.3 s | 879.3 s |
| Early | 11/min | 330.0 | 2 | 36 | 162 | 1.2% | 6.9% | 8/min | 393.5 s | 954.3 s |
| Early | 12/min | 359.0 | 0 | 49 | 184 | 1.3% | 9.9% | 9/min | 459.8 s | 1,034.1 s |
| AM peak | 15/min | 449.0 | 3 | 4 | 136 | 0.2% | 0.1% | 6/min | 102.0 s | 495.6 s |
| AM peak | 17/min | 514.0 | 3 | 5 | 159 | 0.2% | 0.1% | 7/min | 121.4 s | 517.3 s |
| AM peak | 20/min | 599.0 | 3 | 5 | 195 | 0.3% | 0.1% | 7/min | 143.7 s | 543.5 s |
| AM peak | 24/min | 719.0 | 2 | 7 | 198 | 0.4% | 0.2% | 7/min | 278.9 s | 678.7 s |
| AM peak | 30/min | 894.0 | 0 | 9 | 198 | 0.4% | 0.1% | 8/min | 583.0 s | 977.5 s |
| PM peak | 17/min | 514.0 | 3 | 5 | 146 | 0.3% | 0.1% | 5/min | 84.9 s | 458.6 s |
| PM peak | 20/min | 599.0 | 3 | 6 | 178 | 0.3% | 0.1% | 6/min | 110.6 s | 486.3 s |
| PM peak | 24/min | 719.0 | 3 | 10 | 198 | 0.4% | 0.1% | 7/min | 195.3 s | 573.8 s |
| PM peak | 30/min | 899.0 | 0 | 11 | 198 | 0.4% | 0.2% | 8/min | 423.2 s | 802.5 s |

The rule applies to each arm, so the next table gives each seed of the arms near the limit.
The junction wait share uses the same upper bound.

| NUMBAT band | Rate | Peak stopped pods, seeds 1, 2, 3 | Junction wait share, seeds 1, 2, 3 | Seeds that meet the rule |
| --- | ---: | ---: | ---: | ---: |
| Early | 7/min | 8, 7, 6 | 0.56%, 0.47%, 0.35% | 0 |
| Early | 8/min | 11, 6, 8 | 0.80%, 0.54%, 0.56% | 1 |
| Early | 9/min | 17, 13, 15 | 1.05%, 0.85%, 0.85% | 3 |
| Early | 10/min | 23, 21, 24 | 1.33%, 1.05%, 0.98% | 3 |
| Early | 11/min | 34, 33, 36 | 1.38%, 1.05%, 1.04% | 3 |
| Early | 12/min | 47, 44, 49 | 1.41%, 1.14%, 1.21% | 3 |
| PM peak | 24/min | 10, 7, 7 | 0.36%, 0.38%, 0.49% | 1 |
| PM peak | 30/min | 11, 7, 7 | 0.52%, 0.44%, 0.38% | 1 |

Early meets the rule for all three seeds at 9/min to 12/min.
At 8/min, only seed 1 meets it.
At 9/min and 10/min, all seeds finish within 60 minutes with 13 to 24 peak stopped pods.
No arm meets the junction wait part of the rule, because the junction wait share is 1.41% or less.

In Early, track and junction flow, not the fleet, sets the limit.
At 11/min, seed 3 finishes at 3,609 seconds with a peak of 162 active pods.
At 12/min, no seed finishes within 60 minutes, and no seed has more than 184 active pods.
In AM peak and PM peak, every seed at the first rate that does not finish has all 198 pods with work.
The capacity sweep with 114 pods also found that congestion can contribute to the Early limit.

In PM peak, only seed 1 meets the rule, at 24/min and 30/min.
All 198 pods have work at these rates, so the fleet explains the stopped pods, and PM peak is not a second regime.

In each arm, junction and track wait make up more than 99% of `stopped_pod_seconds`.
Thus this ratio does not separate the bands.
Peak node throughput does not separate them either.
Early reaches 8 or 9 passes a minute, which is near the 8.31/min of one stream in the corridor test, and more than the 6.94/min of two streams.
AM peak and PM peak also reach 7 or 8 passes a minute at 24/min and 30/min, with fewer stopped pods.

The report does not give the location of a wait.
A scratch build of the compare command counted the stopped pods on each lane and followed `BlockedBy` to the pod at the head of each queue.
The repository has no command for this count.
In Early at 10/min with seed 1, most stopped pod-seconds are on the line lanes into Baker Street and King's Cross St. Pancras, and on the departure lanes at Paddington.
The pod at the head of most of these queues waits for junction traffic.
Berth waits are less than 1% of the stopped time.

Thus Early with 198 pods at 9/min to 12/min is a load that track flow limits.

#### Platoon A/B

This section records the historical three-seed A/B.
The [162-arm follow-up](platoon-followup.md) adds seeds 4 through 10, rail-hub, and default-London controls on source `c804c00`.
It finds one recovery-rule failure and three no-harm failures, despite passing all per-tick safety checks.
The historical measurements below remain unchanged.

The A/B runs Early at 9/min to 12/min, the load that the sweep found.
It also runs AM peak at 20/min and 24/min and PM peak at 24/min as controls, because the fleet limits these bands.
Each band uses seeds 1, 2, and 3, and the other settings of the sweep.
The platoon limit is 4 pods.
The off rows come from an earlier run of the same arms.
With platooning off, the simulation gives the same results as the base build, so these rows stay valid.
The virtual rows come from the platoon build on base `ab9b847`.
After the rebase onto `6b47a34`, the compare command at commit `6d21c72` gives the same off rows and virtual rows for Early at 12/min.
The three commands took 107 wall seconds with ten workers.
Each seed drains within 65 minutes at each rate, so `-adaptive-limit` skips no rate.

```sh
mise run scenario -- -preset london-central -station-pods 2 -parking-pods 2 -output /tmp/podsim-london-198.json
mise run compare -- -project /tmp/podsim-london-198.json -pattern profile -bands early -loads 6.666667s,6s,5.454545s,5s -seeds 1,2,3 -redistribution-policies off -platoon-policies virtual -focus 940GZZLUEUS -queue-limit 1000000 -duration 65m -arrivals-for 30m -stop-when-drained -adaptive-limit -workers 10 -format csv
mise run compare -- -project /tmp/podsim-london-198.json -pattern profile -bands am-peak -loads 3s,2.5s -seeds 1,2,3 -redistribution-policies off -platoon-policies virtual -focus 940GZZLUEUS -queue-limit 1000000 -duration 65m -arrivals-for 30m -stop-when-drained -adaptive-limit -workers 10 -format csv
mise run compare -- -project /tmp/podsim-london-198.json -pattern profile -bands pm-peak -loads 2.5s -seeds 1,2,3 -redistribution-policies off -platoon-policies virtual -focus 940GZZLUEUS -queue-limit 1000000 -duration 65m -arrivals-for 30m -stop-when-drained -adaptive-limit -workers 10 -format csv
```

Each value is the mean of the three seeds, except the maxima of peak stopped pods.
Each cell gives the off value and then the virtual value.
The junction wait and the track wait are in pod-seconds.

| NUMBAT band | Rate | Seeds that finish in 60 minutes | Average wait | Average journey | Junction wait | Track wait | Peak stopped pods | Platoon time |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Early | 9/min | 3, 3 | 290.0 s, 285.0 s | 804.4 s, 763.8 s | 2,537, 1,899 | 6,101, 1,687 | 17, 12 | 9.6% |
| Early | 10/min | 3, 3 | 340.3 s, 328.5 s | 879.3 s, 812.7 s | 3,541, 2,745 | 13,878, 5,011 | 24, 24 | 12.9% |
| Early | 11/min | 2, 3 | 393.5 s, 371.0 s | 954.3 s, 853.3 s | 4,120, 3,572 | 24,368, 9,873 | 36, 32 | 15.4% |
| Early | 12/min | 0, 3 | 459.8 s, 418.0 s | 1,034.1 s, 904.3 s | 5,065, 4,496 | 39,893, 16,613 | 49, 39 | 18.3% |
| AM peak | 20/min | 3, 3 | 143.7 s, 144.0 s | 543.5 s, 543.9 s | 977, 1,063 | 353, 225 | 5, 6 | 0.6% |
| AM peak | 24/min | 2, 2 | 278.9 s, 277.6 s | 678.7 s, 677.2 s | 1,764, 1,867 | 754, 489 | 7, 9 | 1.5% |
| PM peak | 24/min | 3, 3 | 195.3 s, 194.2 s | 573.8 s, 572.2 s | 1,699, 1,523 | 577, 360 | 10, 7 | 1.4% |

In Early, the pods spend 10% to 18% of their travel time in a platoon.
The track wait falls by 58% to 72%, and the average journey falls by 5% to 13%.
Each seed at 12/min now finishes within 60 minutes, at 3,295 s to 3,539 s.
The sweep did not run rates above 12/min, so the Early limit with platoons is 12/min or more.
In AM peak and PM peak, the pods spend about 1% of their travel time in a platoon, because the fleet and not the track limits these bands.
At AM peak 24/min, seed 2 ends at 3,681 s without platoons and at 3,696 s with them, so the AM peak limit stays at 20/min.

The design gives seven adoption rules.

1. With platooning off, the A/B harness gives the same rows and the same snapshot hash at each simulated second as the base build `ab9b847`.
   All 12 arms are identical, apart from the new column `coupled_time_percent`, which is 0 in each row.
   After the rebase, the 6 arms that were run are also identical to `6b47a34`.
2. The corridor tests and the platoon tests check the separation, the berths, and the owners at each tick.
   The platoon tests also check the certificate of each link and the speed change of each linked pod at each tick.
   Their paths include a hairpin, two opposite turns, a curved lane, a platoon of 4 pods that folds back past its own lane, a sharp turn after the run, a long merge, and a leader that brakes at its limit.
   In a close fold, the return lane ends 5 m from the first lane, so pods come within 12 m of each other also without platoons.
   That test checks the distance from each follower to each pod ahead that it shares with.
   A scratch run of London with 198 pods and random trips at 30/min for 30 minutes, with seeds 1 and 2, ran the safety observation check at each tick with virtual platoons.
   It found no failure, and a physical restore of its state each 60 s kept all pods in place.
   A restore test forms links on a path that turns 120 degrees and on a path that turns a little more, where the run ends before the bend.
   Each link restores in place.
   Another restore test saves a link that drains, and the restored link grows to the same run as the live link.
3. At the 30 degree merge, platoons of 4 give 2.89 times the flow of single pods, more than the 50% that the rule requires.
4. No band limit falls.
   The Early limit rises from 10/min to at least 12/min.
   The AM peak limit stays at 20/min, and at PM peak 24/min, the off limit, each seed finishes in both arms.
   At the off limit rate, the mean wait falls by 3.5% in Early, rises by 0.3% in AM peak, and falls by 0.6% in PM peak.
   Thus the rule holds because the Early limit rises, not because the wait falls by 10%.
5. No virtual arm ends after 3,600 s when its off arm ends by 3,600 s.
6. At the control rates within the off limits, each virtual arm serves the same requests.
   The mean wait rises by 0.4 s or less, and the empty distance rises by 0.3% or less.
7. The heavy London arm, 114 pods in PM peak at 12/min with seed 1, used 7.76 s of user CPU without platoons and 7.90 s with them.
   That is 1.8% more, as the mean of three runs of each arm.
   Its pods spend 0.3% of their travel time in a platoon, and its served requests and end time do not change.

The screening thus meets all seven rules.
The design also gives seeds 4 to 10 and the rail-hub hub-burst schedule as further measurements before adoption.
This historical A/B did not run them.
The [follow-up](platoon-followup.md) completes those measurements and records the expanded study's failures.
The historical results supported a project option for virtual platoons, off by default, and the defaults do not change.
The `platoonLimit` project setting now gives this option.
The [follow-up rows](measurements/platoon-followup.csv) repeat these arms on later source.

## Seat screen for larger pods

A pod with more seats can help only when the parties that a pod could take are more than its seats.
This screen measures that demand, and it compares 4-seat and 8-seat pods with all other settings equal.
It has no physical model of a larger pod.
Each pod keeps its 4 m body, its acceleration, and its dwell, so the 8-seat arms give an optimistic gain.
The compare command at commit `d434de5` gives the recorded values, with report `schema_version` 10.

The arms differ only in the party limit, 4 or 8.
Both use the drop-offs mode, with the stop limit of 3 and the detour cap of 1.5.
Dispatch, dwell, speed, platoons, placements, and seeds are equal.
The London arms use the current preset, which has a platoon limit of 4.
The compare command does not read the platoon limit of a project, so the London command gives `-platoon-policies virtual`.
The rail-hub preset has no platoons.
The London sweep took 254 wall seconds with ten workers.

```sh
mise run scenario -- -preset london-central -output /tmp/podsim-london.json
mise run compare -- -project /tmp/podsim-london.json -pattern profile -bands early,night,am-peak -duration 65m -arrivals-for 30m -loads 60s,30s,20s,15s,12s,10s,8.571429s,7.5s,6.666667s,6s,5.454545s,5s,4.615385s,4.285714s,4s -seeds 1,2,3 -redistribution-policies off -platoon-policies virtual -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -adaptive-limit -past-limit 1 -sharing-limits 4,8 -sharing-modes drop-offs -workers 10 -format csv -output docs/measurements/london-seat-screen.csv
mise run scenario -- -preset rail-hub -output /tmp/podsim-rail-hub.json
mise run compare -- -project /tmp/podsim-rail-hub.json -pattern hub-burst -duration 30m -arrivals-for 5m -request-every 5s -burst-size 12 -seeds 1,2,3 -sharing-limits 4,8 -sharing-modes drop-offs -redistribution-policies off -workers 10 -format csv -output docs/measurements/rail-hub-seat-screen.csv
```

The screen uses these columns.
See [report columns](../README.md#report-columns) for the full definitions.

- `full_pod_refusals` counts the parties that found a full boarding pod at their origin that could take them, and that joined no pod.
- `departures_over_four_aboard` counts the pod journeys that use a fifth seat or more.
- `departure_backlog` counts, at each departure of a boarding pod, the waiting parties at the origin that the pod could take with a free seat.
  It also counts parties that have another pod on its way.

The limits use the 60-minute rule of the capacity sweep.
With 4 seats and with 8 seats, the limits are equal: 15/min in Early, 11/min in Night, and 14/min in AM peak.
15/min is the highest tested rate, so the Early limit is a lower bound in both arms.

The next table gives the arms from 1/min to the 4-seat limit of each band, with all three seeds, and the three rail-hub seeds.
All arms in this table finish every request.
The refusals, the departures, and the served requests are totals.
The waits and journeys are the mean of the arms.
Each pair of cells gives the 4-seat value, then the 8-seat value.

| Regime | Arms | Served | Refusals, 4 seats | Refusals per served request | Departures with more than 4 aboard, 8 seats | Average wait | Average journey | Journey p95 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Early, 1 to 15/min | 45 | 10,770 | 398 | 3.70% | 140 | 226.3 / 224.1 s | 707.1 / 705.5 s | 1,167.1 / 1,163.7 s |
| Night, 1 to 11/min | 33 | 5,913 | 1 | 0.02% | 1 | 122.9 / 122.6 s | 547.0 / 546.7 s | 1,209.7 / 1,208.7 s |
| AM peak, 1 to 14/min | 42 | 9,423 | 0 | 0.00% | 0 | 133.2 / 133.2 s | 531.6 / 531.6 s | 997.3 / 997.3 s |
| Rail-hub | 3 | 177 | 5 | 2.82% | 3 | 229.3 / 223.8 s | 526.1 / 520.7 s | 957.6 / 909.0 s |

In AM peak, the 4-seat and 8-seat rows are equal at each rate up to the limit, apart from one full departure at 14/min.
In Night, one party is refused, at 9/min.
In Early, refusals start at 7/min and grow with the rate.
In the 8-seat arms, 140 pod journeys depart with more than four parties aboard.

The next table gives Early at each rate with a refusal.

| Rate | Refusals, 4 / 8 seats | Departures with more than 4 aboard, 8 seats | Average wait | Average journey | Journey p95 |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 7/min | 1 / 0 | 1 | 242.1 / 242.1 s | 720.6 / 720.9 s | 1,150.0 / 1,148.3 s |
| 8/min | 3 / 0 | 3 | 238.6 / 238.9 s | 721.9 / 722.1 s | 1,159.2 / 1,163.2 s |
| 9/min | 3 / 0 | 2 | 247.5 / 247.4 s | 732.4 / 732.3 s | 1,199.8 / 1,199.8 s |
| 10/min | 15 / 0 | 10 | 240.0 / 241.2 s | 730.9 / 733.2 s | 1,225.0 / 1,236.8 s |
| 11/min | 15 / 0 | 13 | 249.3 / 251.4 s | 742.9 / 744.4 s | 1,279.8 / 1,279.1 s |
| 12/min | 36 / 0 | 16 | 263.2 / 254.6 s | 758.3 / 749.9 s | 1,330.5 / 1,336.8 s |
| 13/min | 53 / 0 | 22 | 260.3 / 258.0 s | 758.9 / 757.5 s | 1,359.9 / 1,338.1 s |
| 14/min | 108 / 5 | 33 | 253.1 / 243.0 s | 754.4 / 747.8 s | 1,342.9 / 1,325.8 s |
| 15/min | 164 / 15 | 40 | 254.4 / 239.6 s | 758.6 / 745.6 s | 1,401.6 / 1,368.6 s |

The largest fall of the Early mean journey is 1.71%, at 15/min.
At 10/min and 11/min, the mean journey with 8 seats is 0.20% to 0.32% longer, because the extra joins change later dispatch.

The backlog is much larger than the refusals.
In the Early arms of the first table, 3,547 of the 7,869 pod journeys depart with more than four parties aboard plus backlog.
The backlog of these arms is 45,615 parties with 4 seats and 43,464 with 8 seats.
Most of these parties already have another pod, on its way or at the station, when the boarding pod departs.
Only a party with no pod tries to join a boarding pod, so more seats cannot take them.

The design of the screen gives three rules.

| Rule | Result |
| --- | --- |
| 1. Demand: in one regime, the 4-seat arms at or below the 4-seat limit refuse a fifth party for at least 1% of the served requests. | Met in Early, 3.70%, and in rail-hub, 2.82%. Not met in Night and AM peak. |
| 2. Gain: the 8-seat arms raise a London band limit by one rate step, or they lower the mean journey by at least 5% in rail-hub or 2% in a London band. | Not met. No limit rises. The mean journey falls by 0.24% in Early, 0.05% in Night, 0% in AM peak, and 1.01% in rail-hub. |
| 3. Guard: in each pair of arms with the same band, rate, and seed, the 8-seat journey p95 is at most 2% higher, and no 8-seat arm at or below the limit ends after 3,600 s when its 4-seat arm ends by 3,600 s. | Not met. The mean p95 of each regime falls, but three Early pairs have a p95 that is more than 2% higher. No arm ends late. |

The next table gives each pair of arms at or below the 4-seat limit where the 8-seat journey p95 is more than 2% higher.
No Night, AM peak, or rail-hub pair has such a rise.

| Band | Rate | Seed | Journey p95, 4 seats | Journey p95, 8 seats | Change |
| --- | ---: | ---: | ---: | ---: | ---: |
| Early | 10/min | 3 | 1,198.2 s | 1,238.3 s | +3.35% |
| Early | 14/min | 3 | 1,339.8 s | 1,372.0 s | +2.40% |
| Early | 15/min | 1 | 1,356.7 s | 1,413.5 s | +4.18% |

Thus the demand for a fifth seat exists in Early and at the rail hub, but seats 5 to 8 give almost no gain.
In some Early arms, the journey tail becomes longer.
The failed guard makes the result against the physical model stronger.
This result uses the optimistic model, so a pod with a longer body and slower acceleration would give less.
The screen does not justify the physical model of a larger pod for these regimes.
The Early limit is at the highest tested rate, so rates above 15/min did not run.

The A/B harness ran its 12 arms at `43f10a9` and at `d434de5`.
All 12 arms give identical rows and snapshot hashes.
The harness writes no seat screen column, and its replay does not turn on the experiment records.
A second run of the shared arm with the records on in the replay also gives identical hashes.
The raw results are in git history.

## Assigned-party sharing

The `reassign-existing` join policy remains opt-in.
It improves average journeys, but fails the all-band adoption rules.
Late and Morning each lose one capacity step, from 13/min to 12/min.
The default remains `unassigned`, and sharing remains off unless the party limit exceeds one.
See [parties](../README.md#parties) for the policy semantics.

These experiments used four parties per pod, drop-offs mode, three intermediate stops, a 1.5 detour cap, virtual platoons, free-flow routing, and redistribution off.
London arms admitted requests for 30 minutes and ran for up to 65 minutes, stopping when drained.
Each rate used seeds 1, 2, and 3.

The capacity limit is the last consecutive rate that every seed drained by 3,600 seconds without skipped requests.
A passing rate above an earlier failure does not raise that limit.
Rates in the tables are nominal rates from the request interval.
The CSV's `offered_per_minute` uses the realized arrival count, so it can differ slightly.

The original measurements were captured on September 28, 2026.
The census used `1141f43`, and the paired implementation used `39eaf54`.
The tables below were recomputed from those CSVs.
The full matrices were not rerun for this record.
Two targeted request-level replays at `025fff4` reproduced the original arm summaries exactly.

### Census and capacity extension

The London and rail-hub census counted assigned waiting parties that a boarding pod could take.
The all-band matrix and the rail-hub pairs hold the census rows again.
The London totals use rates at or below the original band limit.

| Regime | Served parties | Eligible assigned parties | Eligible share | Added-stop-only share of eligible |
| --- | ---: | ---: | ---: | ---: |
| Early | 10,770 | 5,709 | 53.01% | 14.03% |
| Night | 5,913 | 1,898 | 32.10% | 6.90% |
| AM peak | 9,423 | 1,238 | 13.14% | 76.82% |
| Rail hub | 177 | 85 | 48.02% | 0.00% |

Early and rail hub exceeded the 2% eligibility screen.
Their added-stop-only shares were below 20%, which selected existing-stop reassignment for implementation.
The initial London paired screen covered Early, Night, and AM peak from 1/min through 15/min, and the all-band matrix holds its rows again.
The rail-hub pairs reduced mean journey time from 526.1 to 438.6 seconds, a 16.62% reduction.
Their mean journey p95 fell from 957.6 to 733.5 seconds.

The 16 to 24/min extension and higher-rate Early extension found consecutive limits of 22 to 27/min in Early and 11 to 21/min in Night.
Early also passed 32/min after failing 28/min, and Night passed 23/min after failing 22/min.
Those isolated passes do not change the consecutive limits.

### All-band result

The all-band matrix contains 720 arms, or 360 policy pairs.
It tests 1/min through 15/min in all eight bands.
The journey columns below average arm statistics across pairs at or below the `unassigned` limit.
They are not pooled request quantiles.
A `15+` limit means every tested rate passed, so this matrix gives only a lower bound.
The extended Early and Night limits above come from separate runs.

| Band | Pairs within baseline limit | Limit, unassigned / reassign | Mean journey, unassigned / reassign | Mean change | Mean journey p95, unassigned / reassign |
| --- | ---: | --- | ---: | ---: | ---: |
| Early | 45 | 15+ / 15+ | 707.1 / 593.9 s | -16.01% | 1,167.1 / 876.3 s |
| Morning | 39 | 13 / 12 | 570.8 / 558.9 s | -2.09% | 1,072.8 / 1,049.8 s |
| AM peak | 42 | 14 / 15+ | 531.6 / 521.8 s | -1.85% | 997.3 / 977.8 s |
| Interpeak | 45 | 15+ / 15+ | 473.1 / 469.0 s | -0.88% | 854.4 / 847.1 s |
| PM peak | 45 | 15+ / 15+ | 493.0 / 484.5 s | -1.72% | 860.9 / 851.3 s |
| Evening | 42 | 14 / 15+ | 529.0 / 521.9 s | -1.34% | 889.7 / 872.0 s |
| Late | 39 | 13 / 12 | 564.2 / 556.4 s | -1.37% | 918.8 / 909.3 s |
| Night | 33 | 11 / 15+ | 547.0 / 498.8 s | -8.80% | 1,209.7 / 1,074.8 s |

All 330 pairs within the baseline limits had matching schedule IDs, no skipped requests, and both arms drained.
No reassignment arm in that set exceeded the 1.5 detour cap.
Mean journey time fell in every band, but these aggregate gains do not pass the per-pair guards.

| Rule | Requirement | Result |
| --- | --- | --- |
| 1 | Matching schedules, no skipped arrivals, both arms drained | Pass for all 330 pairs within baseline limits. |
| 2 | Identity, determinism, and CPU gates | Historical gates passed. They were not rerun for this record. |
| 3 | No lower band capacity | Fail: Late and Morning fall from 13/min to 12/min. |
| 4 | Mean journey and journey p95 at most 2% higher in every pair | Six p95 failures. No mean-journey failure. See below. |
| 5 | Serve baseline requests without crossing the 3,600-second deadline | Fail: one Late pair and one Morning pair cross the deadline. All requests still finish. |
| 6 | Detour ratio at most 1.5 | Pass within the baseline limits. |
| 7 | Matched-request diagnostic and traces of flagged parties | Initial three-band and rail-hub diagnostics exist. All-band coverage remains incomplete. |
| 8 | A capacity gain or qualifying mean-journey gain | Pass, including Early and Night capacity gains and the rail-hub mean reduction. |
| 9 | AM peak mean journey at most 1% higher | Pass: the mean falls 1.85%. |

The historical A/B checks passed identity and determinism.
The archived CPU runs reported a 1.008 ratio on the disabled path and 0.747 on the active path.
Those CPU runs were not repeated for this record.
Matched-request diagnostics covered the initial three London bands and rail hub, not the complete all-band matrix.
The guarded-positioning follow-up remains unqualified for adoption.
None of these limits or exceptions supports a default change.

The next table lists all six p95 failures within baseline limits.
The previously traced AM peak pair has an explicit dispatch-order exception.
That exception does not cover the other five pairs.

| Band | Nominal rate | Seed | Mean journey change | Journey p95 change |
| --- | ---: | ---: | ---: | ---: |
| AM peak | 13/min | 1 | -1.71% | +2.76% |
| Interpeak | 11/min | 3 | +0.80% | +3.17% |
| Interpeak | 15/min | 3 | +0.96% | +6.11% |
| Late | 9/min | 1 | +0.42% | +2.93% |
| Morning | 4/min | 1 | +0.19% | +2.09% |
| PM peak | 11/min | 3 | -1.65% | +2.00245% |

### Deadline traces

The targeted replay used the same schedules and policies as the two failed deadline pairs.
Both policies reproduced served counts, reassignment counts, shared counts, journey means, journey p95 values, and final times exactly.
The replay also required every matched request to have the same requested tick.
The late completion comes mainly from longer pickup waiting in both cases.
The last party in each reassignment arm was neither reassigned nor a host for a reassigned party.

| Pair | Finish time, unassigned / reassign | Last party with reassign | Journey change for that party | Wait change | Ride change |
| --- | ---: | --- | ---: | ---: | ---: |
| Late, 13/min, seed 2 | 3,571 / 3,672 s | 388, SBC to BNK | +101.57 s | +96.20 s | +5.37 s |
| Morning, 13/min, seed 1 | 3,463 / 3,645 s | 383, WIG to MGT | +750.20 s | +755.52 s | -5.32 s |

Party 388 was last in both Late arms.
The Morning baseline finished with party 378, while party 383 became last under reassignment.
The finish times round to the next reporting second.
The request timings retain simulation-tick precision.

The raw data of the Late and Morning request pairs and arm summaries is in git history.
These traces locate the deadline regressions in pickup waiting.
They do not establish a specific dispatch defect or justify a policy change.
