# Pickup availability route-copy measurements

Skipping route assembly for availability-only pickup checks reduces measured Central process CPU by 5.3-8.2% in these probes.
It saves about 1.4 GiB of allocations per simulated hour.
LondonFull saves about 1.2 GiB, with a smaller CPU reduction of 0.9-1.2%.
All 24 arms match their preset's final serialized simulation-state hash.
A separate matched Chrome test keeps Central at 60x in both default-GOGC repetitions where the baseline reduces to 15x.

## Change and equivalence

Held pickup requests call `pickupAvailable` to check whether dispatch can find a pod.
Previously, this boolean check used `candidateRoute`, which joined a moving pod's committed prefix and replacement suffix into a new slice.
The availability check discarded that route.

The private `candidateRouteParts` helper now returns borrowed, read-only prefix and suffix slices.
It runs the same commitment checks and station-route queries in the same order.
The normal `candidateRoute` wrapper still returns the same assembled route for a moving pod.
Idle results and error results retain their existing behavior.
Moving results remain independent of the vehicle route and cached suffix, including moves with an empty prefix.

No eligibility, berth-load, route-cache, assignment, or platoon rule changes.
The availability check skips only the final clone and append.
Tests compare routes, berths, return values, and complete simulation cache state with a frozen original function.
They cover idle and moving pods, load functions, unknown and unreachable stations, committed parking inlets, coupled rejection, and result ownership.
The earlier full-fleet hold oracle also passes.
Simulation and session suites, race checks, static checks, and native and WASM builds pass.
Independent review found no equivalence or ownership blocker.

## Method and results

The baseline source is `e12cf4a`.
The candidate contains only the private route-parts change.
Each process generates LondonCentral or LondonFull with demand at 12 requests per simulated minute and seed 20260929.
It warms up for one simulated hour, then measures another hour without the live playback clock.
GOMAXPROCS is 2.
Each mode and GOGC setting has two repetitions, with candidate order reversed in the second repetition.
No other agent CPU-heavy work runs during measurement.
CPU profiles are off.

Stream mode encodes shared gzip deltas at the modeled equivalent of 20 updates per second at 60x.
It runs no browser, network connection, or persistence writer.
The table gives means of two runs.

| Preset | Mode | GOGC | Baseline CPU | Candidate CPU | CPU reduction | Saved allocations |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| Central | Ticks | 100 | 13.156 s | 12.073 s | 8.23% | 1,416 MiB |
| Central | Ticks | 200 | 12.442 s | 11.647 s | 6.39% | 1,416 MiB |
| Central | Stream | 100 | 15.926 s | 14.837 s | 6.83% | 1,419 MiB |
| Central | Stream | 200 | 14.678 s | 13.896 s | 5.33% | 1,415 MiB |
| Full | Ticks | 200 | 28.742 s | 28.402 s | 1.18% | 1,185 MiB |
| Full | Stream | 200 | 32.759 s | 32.466 s | 0.90% | 1,184 MiB |

The CPU difference for Full is small, and two repetitions do not establish a stable improvement across workloads.
Central collections decrease from 49 to 29 in GOGC 100 tick mode and from 72 to 51 in stream mode.
Sampled peak HeapAlloc does not consistently decrease.
It is neither RSS nor post-collection retained memory.
Different random `serverStart` values in each process prevent byte-identical gzip totals despite matching simulated results.
The probes make no network-rate claim from those small byte differences.

The server retains its default GOGC 100, and the comparison command retains GOGC 400 when unset.
The overload sensor, project format, protocol, and saved-state format remain unchanged.
The live Chrome results below remain separate from these unpaced measurements.

[Measurements](measurements/hold-route-parts.csv) retain CPU, allocation, collection, pause, heap, and gzip totals.
[Metadata](measurements/hold-route-parts.json) records frozen inputs and both exact saved-state hashes.
The local artifacts are in `~/.cache/agents/podsim/hold-route-20260930/`.

## Matched live Chrome follow-up

Twelve live arms compare the server before and after the route-parts change.
Both servers use the same catalog client, frozen physical state at tick 216000, and demand at 12 requests per simulated minute.
GOMAXPROCS is 4.
Each arm connects one local Chromium client, requests 60x, and measures one wall-clock minute with server CPU profiling enabled.
The second repetition reverses server order.
No other agent CPU-heavy work runs during the matrix.
The table gives means of two runs.

| Preset | GOGC | Baseline playback | Candidate playback | Baseline final speed | Candidate final speed |
| --- | ---: | ---: | ---: | --- | --- |
| Central | 100 | 35.94x | 59.75x | 15x in both | 60x in both |
| Central | 200 | 58.62x | 59.78x | 60x in both | 60x in both |
| Full | 200 | 59.97x | 59.97x | 60x in both | 60x in both |

The candidate has no speed reduction in these six runs.
This result supports a live improvement for this Central GOGC 100 fixture.
Two short repetitions do not establish performance across demand rates, machines, or longer runs.
Full already keeps 60x with either server in this fixture.

Central GOGC 100 server CPU rises from 16.64 to 21.38 seconds because the candidate advances more simulated ticks.
Those wall-window CPU totals do not measure the cost of equal simulated work.
The paired unpaced probes above provide that comparison.
At GOGC 200, both servers advance similar work: Central CPU is 22.12 / 20.46 seconds and Full CPU is 43.69 / 43.43 seconds.
No GC setting or overload threshold changes.

All pages use one WebSocket and one initial full snapshot.
Every publication during measurement is a delta, with no reconnect or second compression extension.
The maximum outstanding publication count is two.
Across all pages, ACK p95 is 14.7-21.4 milliseconds and gzip decompression p95 is 0.5-0.7 milliseconds.
These checks show no transport recovery event during the matrix.
They do not establish the cause of every live stall.

[Live arm measurements](measurements/hold-route-live-arms.csv), [page measurements](measurements/hold-route-live-pages.csv), and [metadata](measurements/hold-route-live.json) retain the results.
Raw probes, screenshots, and profiles remain in `~/.cache/agents/podsim/hold-route-live-20260930/`.
