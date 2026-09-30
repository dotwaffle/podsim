# Stream decoder follow-up probe

This report records the initial native experiment before implementation.
The compatible direct decoder later landed after [native and Chrome qualification](stream-decoder-qualification.md).
The fastest one-pass variant changes accepted input and must not replace the production decoder.

## Profile evidence

A Full one-client profile after the cadence fix records 9.42 seconds inside stream JSON decoding over a 60.09-second capture.
The same profile records 14.47 seconds inside receiveStream and 14.17 seconds inside drawNetwork.
These are inclusive sampled times, with overlap between callers and children.
They are not additive process CPU measurements.
The three-client local page records 10.01 seconds inside decoding over 60.12 seconds.
These observations make decoding a concrete follow-up target.

The corresponding Full server profile samples 43.54 seconds of CPU.
Simulation stepping accounts for 29.08 inclusive seconds, dispatch for 16.23, and waiting for a finishing pod for 8.44.
Cached routing accounts for 6.56 inclusive seconds and overlaps other operations.
Future server work should investigate those calls without caching mutable berth or reservation decisions incorrectly.
These single profiles identify candidates, not controlled optimization results.

## Variants and compatibility

The baseline decoder checks the message size, validates strict JSON, and uses a legacy streaming Decoder with unknown-field rejection.
That Decoder reads and buffers a complete value before typed decoding.
The Go 1.27 implementation then calls json/v2.Unmarshal with DefaultOptionsV1 and RejectUnknownMembers.

The first experiment combines validation and typed decoding into one call.
It retains legacy options and adds strict duplicate-name, invalid-UTF8, and unknown-member checks.
It rejects this input, which production currently accepts:

```json
{"kind":"full","Kind":"delta"}
```

The JSON keys differ, so the existing strict text validator permits them.
Legacy case-insensitive decoding assigns the second value to the same field.
The one-pass strict decoder treats that repeated target field as a duplicate.
Its faster valid-input measurements do not justify this compatibility change.

The direct-legacy alternative retains the exact existing strict text validation before typed decoding.
It calls the same Go 1.27 typed decoder and options directly, avoiding the streaming Decoder's extra read and buffer.
The finite corpus matches acceptance, decoded values, partially decoded error targets, and error text against production.
Existing stream tests also pass under a test-only codec overlay.
This evidence does not establish all input compatibility.
Later qualification added outer-whitespace trimming to preserve typed error offsets.

## Native benchmark

Each variant decodes the same bytes within each case.
Central and Full restore the same physical fixture at tick 216000.
The benchmark uses a full envelope and a delta after three simulated seconds, corresponding to 50 wall milliseconds at 60x.
It excludes loading, simulation advance, capture, and JSON encoding.
GOMAXPROCS is 2, with two 200 ms repetitions per case.
Variant order is fixed, so timing can include order and warm-cache effects.
No quiet browser comparison runs during these native tests.

| Preset | Envelope | Original | Direct legacy | Original bytes allocated | Direct legacy bytes allocated |
| --- | --- | ---: | ---: | ---: | ---: |
| Central | Full | 1.72 ms | 1.47 ms | 558 KB | 295 KB |
| Central | Delta | 0.69 ms | 0.56 ms | 201 KB | 69 KB |
| Full | Full | 3.83 ms | 3.21 ms | 1,619 KB | 569 KB |
| Full | Delta | 1.06 ms | 0.84 ms | 363 KB | 100 KB |

Direct legacy uses approximately 14% to 21% less native decode time in these cases.
Allocated bytes decrease by approximately 47% to 72%.
KB means decimal thousands of bytes here.
These results do not measure WASM, whole-page CPU, retained heap, network traffic, or simulation capacity.

## Follow-up

The direct decoder preserves the case-key counterexample and strict lexical validation.
The follow-up report records the completed compatibility, native, WASM, and live Chrome checks.
A one-pass migration requires an explicit acceptance-contract decision or a decoder that preserves the existing behavior.

[All native measurements](measurements/stream-decoder-probe.csv) retain all three variants, including the incompatible one-pass results.
[Metadata](measurements/stream-decoder-probe.json) retains fixture, helper, and result hashes.
Local helpers, overlays, profiles, and logs remain in `~/.cache/agents/podsim/stream-decoder-probe-20260930/` and `stream-cadence-20260930/`.
