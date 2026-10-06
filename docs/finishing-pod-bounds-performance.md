# Finishing-pod travel bounds

Dispatch now excludes some busy pods before searching for their next empty route.
LondonFull uses 15-18% less CPU in these equal-work comparisons.
Final physical state hashes match for every paired case.
Server GC 100 remains the selected setting.

## Change and correctness

Dispatch can hold a request for a pod that will finish its passenger journey soon.
The previous scan computes that pod's empty route when its remaining journey time could beat the current choice.
Some distant pods pass that first test but cannot reach the pickup station soon enough.

The scan now also uses existing immutable shortest-travel bounds to the pickup station.
Each bound can end at any berth and omits acceleration and braking costs.
The exact estimate still targets the original first berth.
These differences make the bound conservative.
The scan excludes a pod only when the bound exceeds the best ETA or strict hold cutoff by the existing numerical margin.
Ties and near-ties retain the original exact estimate and fleet order.
Missing bounds also retain the exact estimate.

In one equal-work Full CPU profile, inclusive finishing-pod wait samples decrease from 5.17 to 1.10 seconds.
This supports the route-search explanation for the measured reduction.

No new shared cache, worker, simulation setting, or saved-state field is added.
The route cache can contain fewer unused searches, but assignment and movement decisions retain their original rules.

## Equal-work measurements

Each run warms the simulation for 3,600 simulated seconds, then measures another 3,600 seconds.
Demand generates 12 requests per simulated minute with seed 20260929 and the preset's AM Peak profile.
Both variants use GOMAXPROCS 4 and identical geometry, initial fleet, and demand.
Every group runs baseline, candidate, candidate, baseline.

Each advance computes 60 simulation ticks.
Publishing groups capture and gzip one delta after every three advances, for 1,200 publications.
This matches the requested 20 Hz publication rate at 60x playback.
The initial full snapshot and warmup remain outside the measurement window.
The table reports the mean of two runs per variant.

| Preset | Publications | GC | Previous CPU | New CPU | CPU reduction |
| --- | --- | ---: | ---: | ---: | ---: |
| Central | Off | 100 | 10.638 s | 10.392 s | 2.3% |
| Full | Off | 100 | 24.597 s | 20.150 s | 18.1% |
| Full | On | 100 | 28.895 s | 24.657 s | 14.7% |
| Full | Off | 400 | 24.012 s | 19.688 s | 18.0% |
| Full | On | 400 | 27.508 s | 22.993 s | 16.4% |

Full allocation bytes decrease by 5.5% without publishing and about 2.8% with publishing.
Allocation counts decrease by about 1.3% and 0.3%, respectively.
Group means of per-run advance p99 decrease from about 28.6-28.8 milliseconds to 8.1-8.9 milliseconds.
The target budget for these 60-tick advances at 60x is 16.67 milliseconds.
These quantiles do not bound every stall or prove indefinite 60x operation.

All 20 runs reach the same final physical state within each case, including request assignments and passenger distance.
Central generates 1,359 requests and skips 81 under its unchanged live queue limit.
Full generates 1,440 and skips none.
Native compressed byte totals vary slightly and do not establish a network saving.

## GC and live playback

For the candidate, sampled peak heap averages about 193-194 MB at GC 100 and 480-486 MB at GC 400.
The publisher case uses about 7% less CPU at GC 400 than GC 100 in this matrix.
That tradeoff does not justify changing the selected setting.
Sampled peaks are not process RSS or exact maximum heap measurements.

The separate live comparison uses a frozen Chrome client and the same physically restored Full starting state.
Each one-client run requests 60x playback for 60 wall-clock seconds, with two samples per variant and GC setting.
Tiles use placeholders, service workers remain off, and external requests do not reach their providers.
Live runs can end on different ticks and do not provide exact matched service results.
All eight live arms achieve 59.97-59.99x playback without speed reductions or client errors.
At GC 100, mean server CPU decreases from 42.635 to 36.855 seconds per run.
At GC 400, it decreases from 41.345 to 35.490 seconds.
These are reductions of about 14% for the measured live windows.

## Checks and evidence

Tests compare the complete original scan with the optimized scan and verify identical hold state.
They cover distant and near finishes, multiple berths, strict cutoffs, ties, and conservative rounding.
The distant cases also verify that the unused empty-route search does not occur.
Independent static review accepts the lower-bound argument.
The full plain suite and sim/session/view/statestore race suites pass.
Vet, lint, fresh gopls, and native/WASM builds pass.
Independent review verifies the raw hashes, matched state digests, and bounded performance claims.

[Native samples](measurements/finishing-pod-bounds-native.csv), [live samples](measurements/finishing-pod-bounds-live.csv), and [metadata](measurements/finishing-pod-bounds.json) retain inputs and results.
Native runs and live runs execute sequentially without other agent CPU-heavy work.
Unrelated user processes remain outside the experiment's control.
An initial live setup attempt lacked its manifest and stopped before startup.
Its logs remain separate from the measured runs.
