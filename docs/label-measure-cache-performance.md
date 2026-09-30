# Label dimension cache measurements

The client reuses measurements for repeated label text.
Chrome page script time decreases by 1.6% for Central and 3.0% for Full in these short comparisons.
Whole-browser CPU remains roughly unchanged.
A cyclic workload above the cache limit is slower and allocates more than the original path.

## Change and checks

Each Game owns a cache of at most 1,024 text-dimension entries.
Each key includes text, actual face size, and line spacing.
The cache tracks the font source separately and clears entries when it changes.
Only the font source and size affect the faces that these labels currently create.
Text longer than 512 bytes bypasses the cache.
Inserted keys clone the text to avoid retaining large substring backing storage.
At capacity, the cache clears its entries before adding the next measurement.

Positions, colors, visibility, queue counts, preferred stations, and pod selection remain outside the cache.
Rectangle rounding and rendering still use their original rules.
Map-label and display-unit font sizes retain separate calculations.
The storage limit permits at most 512 KiB of inserted text, plus bounded map storage and one font reference.
This limit excludes existing font-library caches.
No project, saved-state, wire, or operating default changes.

Tests compare the original bounds across four layouts, fractional positions, multiline tags, Unicode, empty text, and spacing changes.
They cover font replacement, oversized bypass, capacity rollover, and separate Game ownership.
The full plain suite, view/remote race checks, vet, lint, and fresh gopls checks pass.
Native and WASM builds pass.
Independent review finds no ownership, invalidation, or rendering blocker.

## Native measurements

The benchmark excludes font and label setup and checks the original bounds before timing.
It cycles through fixed sets of multiline pod tags while changing their screen positions.
Both variants use the same function-call mechanism.
GOMAXPROCS is 2, with two 200 ms repetitions per variant and set size.

| Distinct labels | Original time | Cached time | Original allocation | Cached allocation |
| ---: | ---: | ---: | ---: | ---: |
| 1 | 390-404 ns | 48-49 ns | 144 bytes, 1 allocation | 0 bytes, 0 allocations |
| 600 | 382-404 ns | 51-52 ns | 144 bytes, 1 allocation | 0 bytes, 0 allocations |
| 1,500 | 399-404 ns | 520-521 ns | 144 bytes, 1 allocation | 152 bytes, 2 allocations |

The 1,500-label cycle exceeds the 1,024-entry limit and repeatedly clears the cache.
It costs approximately 30% more per measurement and adds one allocation.
The entry limit bounds memory, with this explicit workload tradeoff.
Native hits do not measure complete rendering or browser responsiveness.

## Chrome comparison

Eight arms cover Central and Full with one local client and two reversed-order repetitions.
Both variants use the same frozen admission server, physical starting state, and other browser assets.
The baseline restores the `769a4a2` Game code and removes the new cache implementation through a build overlay.
The candidate changes only label-dimension reuse.
Both clients retain the earlier journey-page cache.
The subsequent publisher-cadence fix is absent from this matrix.

The server uses GC 100 and GOMAXPROCS 4.
It generates 12 requests per simulated minute with seed 20260929 at requested 60x speed.
Each server window lasts approximately 60 seconds.
Tiles use local placeholders, external requests are blocked, and service workers are disabled.
Chrome uses SwiftShader software rendering and active CPU profiles.
No other agent CPU-heavy work runs during timing.
Unrelated user processes remain running.

Each row reports the mean of two processes per variant.
Browser CPU sums process CPU, including software rendering, and can exceed elapsed wall time.

| Preset | Original script | Cached script | Script reduction | Original browser CPU | Cached browser CPU |
| --- | ---: | ---: | ---: | ---: | ---: |
| Central | 21.590 s | 21.252 s | 1.56% | 501.655 s | 504.875 s |
| Full | 28.146 s | 27.306 s | 2.99% | 512.510 s | 507.755 s |

The small whole-browser changes run in opposite directions and do not establish a general CPU reduction.
Frame p95 remains approximately 50 milliseconds.
Full long-task totals do not improve consistently.
The result supports cheaper repeated measurements without claiming broad rendering or frame-rate improvements.

All arms achieve 59.75x to 59.98x playback without speed changes.
Every page receives one initial full snapshot and subsequent deltas, without reconnection or a second compression layer.
Final captures have zero unacknowledged messages and at most two outstanding messages.
Gzip inflation p95 ranges from 0.6 to 0.7 milliseconds.
Local processing-to-ACK-submission p95 ranges from 16.3 to 21.7 milliseconds and excludes return network delay.
The cache preserves wire content and compression behavior.

## Limits and evidence

Page captures last 60.27 to 60.32 seconds because collection follows the server pause.
Browser and ACK metrics extend beyond the server window.
Per-page quantiles describe individual capture windows, not pooled distributions.
Two repetitions and one client do not qualify hardware-GPU rendering, large custom label sets, or every queue-label churn pattern.
The above-capacity native regression remains part of the result.
No network or server performance claim follows from this client cache.

[Native measurements](measurements/label-measure-cache-benchmark.csv), [per-arm measurements](measurements/label-measure-cache-arms.csv), and [per-page measurements](measurements/label-measure-cache-pages.csv) retain all results.
[Metadata](measurements/label-measure-cache.json) records frozen inputs, source hashes, and raw-result hashes.
Local profiles, screenshots, probes, and helpers remain in `~/.cache/agents/podsim/label-measure-cache-20260930/`.
