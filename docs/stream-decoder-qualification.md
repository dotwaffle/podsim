# Compatible stream decoder qualification

The compatible direct decoder landed in `bcac887`.
It keeps strict lexical validation, legacy decoding options, unknown-member rejection, and existing size limits.
It removes the streaming decoder's extra input buffer.
Outer whitespace is trimmed before typed decoding to preserve legacy error offsets.
The faster one-pass variant remains excluded because it changes accepted case-alias inputs.

## Compatibility

The regression corpus compares the old decoder with all nine stream target shapes.
It checks acceptance, partial targets, error types, typed fields, offsets, and nested errors.
Cases include outer whitespace, duplicate keys, invalid UTF-8, unknown fields, case aliases, malformed sequence strings, and nested raw groups.
The corpus is finite and does not prove equivalence for every possible input.

Full plain tests, session and remote race checks, static checks, native and WASM builds, and independent review passed.
Existing stream caps and browser requirements remain unchanged.

## Native and Chrome probes

Go 1.27.1 native benchmarks decode identical bytes without applying a frame.
Each case has five 200 ms repetitions with `GOMAXPROCS=2` and `GOGC=100`.
Variant order is fixed, so these measurements can include warm-cache effects.
The table gives medians.

| Preset and envelope | Original ms | Direct ms | Original bytes/op | Direct bytes/op |
| --- | ---: | ---: | ---: | ---: |
| Central full | 1.6925 | 1.4330 | 558,381 | 295,161 |
| Central delta | 0.6559 | 0.5492 | 200,712 | 68,901 |
| Full full | 3.7560 | 3.1044 | 1,620,868 | 569,148 |
| Full delta | 1.0038 | 0.8399 | 363,167 | 100,373 |

The Chrome 143.0.7499.4 WASM probe measures decoding plus frame application.
Each arm processes the same bytes 100 times, in baseline-candidate-candidate-baseline order.
Conversion, warmup, garbage collection, and result hashing occur outside the timer.
Applied-frame hashes match across variants.

| Preset and envelope | Original ms/op | Direct ms/op | Original bytes/op | Direct bytes/op |
| --- | ---: | ---: | ---: | ---: |
| Central full | 7.1760 | 6.0830 | 558,367 | 295,727 |
| Central delta | 4.1745 | 3.5185 | 429,299 | 229,447 |
| Full full | 15.7415 | 13.3010 | 1,618,839 | 569,699 |
| Full delta | 5.3950 | 4.6420 | 678,973 | 380,809 |

This isolated Chrome workload takes about 14% to 16% less time.
Allocated bytes decrease, but these measurements do not establish retained-heap savings.
They exclude rendering and network work.

## Live Chrome checks

Twelve sixty-second runs cover Central with one client, Full with one client, and Full with three clients.
Each group uses baseline-candidate-candidate-baseline order, the same fixed server, and `GOGC=100`.
Both client variants restore the same physical fixture at tick 216000.
Full's three clients use ordered proxy delays with 0, 300, and 600 ms RTT plus jitter.
Tiles use local placeholders and the checks reject external requests.

Every client receives one initial full snapshot.
No client reconnects or receives another full snapshot because of RTT.
Clients receive identical compressed bytes for each shared stream sequence.
There are no unexpected errors or automatic speed reductions.
Achieved playback remains about 59.8 to 60 times real time.

| Group | Original browser CPU seconds | Direct browser CPU seconds |
| --- | ---: | ---: |
| Central, one client | 549.77 | 549.46 |
| Full, one client | 535.69 | 536.28 |
| Full, three clients | 538.85 | 537.13 |

These are means of two process-tree observations per variant.
They include multiple cores and the SwiftShader software GPU process.
Whole-browser CPU is effectively unchanged in this environment.
The live profile's inclusive decoder samples decrease from 7.76 to 6.58 seconds for Central and 9.53 to 8.16 for Full.
Those sampled call times overlap their callers and cannot be added to process CPU.
The optimization does not change the wire format or claim lower network traffic.

## Evidence

[Native rows](measurements/stream-decoder-qualified-native.csv), [Chrome probe rows](measurements/stream-decoder-qualified-chrome.csv), and [live rows](measurements/stream-decoder-qualified-live.csv) retain every observation.
[Inclusive profile rows](measurements/stream-decoder-qualified-profiles.csv) retain page-specific sampled times.
[Metadata and hashes](measurements/stream-decoder-qualified.json) record inputs, source, toolchain, and validation boundaries.
Raw helpers, logs, profiles, and browser artifacts remain in `~/.cache/agents/podsim/decoder-qualification-20260930/`.
