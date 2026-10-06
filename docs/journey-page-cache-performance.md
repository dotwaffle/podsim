# Journey picker page cache

The journey picker now reuses unchanged station pages.
Native repeated empty-query calls allocate no memory in the measured cases.
Chrome page script time decreases in all four measured groups.
Total browser-process CPU changes by less than 0.4%, so this study does not show a comparable whole-browser CPU gain.

## Change and checks

Each Game owns one cached page result.
The key includes network identity, font source, font size, layout width, and the effective normalized search query.
The original page builder remains unchanged.
Selection, disabled buttons, and current page navigation remain outside the cache.
A network, font, layout, or search change rebuilds the pages.
Ordinary simulation ticks reuse them.
No project, saved-state, wire, or operating default changes.

Tests compare original and cached pages for Central, Full, long names, search queries, and three layouts.
They also check cache hits and each invalidation input.
View and remote race checks, native and WASM builds, vet, lint, and fresh gopls checks pass.
An independent implementation review finds no cache ownership or invalidation blocker.
A separate Chrome interaction selects Archway after `arc`, then Charing Cross after `CHX`, with stable selections after blur and no browser errors.

## Native measurements

The benchmark excludes catalog and font setup.
It compares the frozen original page builder with repeated cache hits.
Both paths use the same method-call mechanism.
GOMAXPROCS is 2, with two 250 ms repetitions for each preset, query, and variant.

| Preset | Empty-query original | Empty-query cached | Original allocation | Cached allocation |
| --- | ---: | ---: | ---: | ---: |
| Central | 38.7-40.1 microseconds | 17.7-18.0 nanoseconds | 33,184 bytes, 81 allocations | 0 bytes, 0 allocations |
| Full | 106.5-106.8 microseconds | 17.6-17.8 nanoseconds | 94,224-94,225 bytes, 215 allocations | 0 bytes, 0 allocations |

Repeated lowercase `arc` queries also allocate no memory on cache hits.
Uppercase `CHX` hits still allocate 8 bytes once per call for query normalization.
These measurements cover page construction, not rendering or initial topology loading.

## Chrome comparison

The matrix has 16 arms and 32 pages.
Central and Full each run with one or three clients and two reversed-order repetitions.
Both variants use the same frozen `3a708e4` server, paused starting state, and other browser assets.
The baseline overlays the original view code from `0d4613d`.
Only the candidate WASM page cache differs.
The server uses GOGC 200 and GOMAXPROCS 4.
Each server measurement lasts approximately 60 wall-clock seconds at requested 60x speed and 12 generated requests per simulated minute.

Three-client groups use ordered 0, 300, and 600 ms round-trip delay with 35 ms jitter.
Tiles use local placeholders and external requests are blocked.
No other agent CPU-heavy work runs during the matrix.
An unrelated Plex transcode remains active.
Chrome uses SwiftShader software rendering on this machine.

The table reports means across two processes per variant.
Script time is the mean per page in three-client groups.
Browser CPU sums process CPU, including software rendering, and can exceed elapsed wall time.

| Preset | Clients | Original page script | Cached page script | Script reduction | Original browser CPU | Cached browser CPU |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Central | 1 | 23.102 s | 21.532 s | 6.8% | 497.47 s | 496.49 s |
| Central | 3 | 8.910 s | 8.007 s | 10.1% | 503.30 s | 501.45 s |
| Full | 1 | 30.957 s | 27.144 s | 12.3% | 483.88 s | 482.25 s |
| Full | 3 | 12.346 s | 10.030 s | 18.8% | 486.23 s | 485.12 s |

All arms achieve 59.7x to 60.0x playback without speed transitions.
Every page receives one initial full snapshot and subsequent deltas, with no reconnection or second compression layer.
All shared sequence hashes agree across clients.
The maximum outstanding count ranges from 2 to 7.
All final captures have zero unacknowledged messages.
Gzip inflation p95 ranges from 0.6 to 0.8 milliseconds.
Local processing-to-ACK-submission p95 ranges from 16.4 to 25.3 milliseconds.
This interval excludes the return network delay and is not full round-trip latency.
The cache does not change the wire payload or compression method.
Small byte-rate differences do not establish network savings.

## Limits and evidence

Page captures last 60.27 to 61.91 seconds because the collector reads clients sequentially after pausing the server.
Script, frame, and ACK observations therefore extend beyond the server window.
Reported page p95 values describe individual capture windows, not pooled distributions.
Proxy publication timestamps measure arrivals before downstream delay.
Summed browser RSS can count shared memory more than once.

Software rendering dominates total browser CPU here.
This study does not predict hardware-GPU performance, every browser, longer sessions, or every topology.
The measured script reduction and native allocation savings support this bounded cache.
Server and default-GC performance require separate measurements.

[Native benchmark](measurements/journey-page-cache-benchmark.csv), [per-arm measurements](measurements/journey-page-cache-arms.csv), and [per-page measurements](measurements/journey-page-cache-pages.csv) retain all results.
[Metadata](measurements/journey-page-cache.json) retains frozen inputs and raw-result hashes.
