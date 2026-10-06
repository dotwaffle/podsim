# Express wire qualification

This record covers session persistence, public state adapters, and Go remote consumers for the approved `express-v1` contract.
It does not qualify the physical engine or parkride continuation.
See [the approved contract](express-physical-encoding-contract-proposal.md) and [the measurements](measurements/express-wire-qualification.json).
Browser evidence is also recorded in `docs/express-browser-qualification.md`.
The tested source starts at `27981fe35c9f2a7644327033405fcbf1a1130338`, with native dependency v6 and the owned session and remote patch.
The widest byte and browser receipts pin dependency v3.
A final v6 bridge reruns real 20-person save, boarding restore, full/HTTP adapters, remote streams, and foundation byte controls.
The native v3-to-v6 delta changes error text, comments, unused private wrappers, and the separately reviewed canonical project decoder helper.
It preserves runtime semantics.
No cap, default, dependency, or physical number changes in this patch.

## Versions and boundaries

The tested source used these separate formats.

| Consumer | Foundation | Express |
| --- | --- | --- |
| Project | Versions 1 through 3, raw UTF-8 | Version 4, explicit `orderContract` |
| Session save | Versions 2 through 6, raw order text | Version 7, contract and text markers |
| Stream | Hello 1 through 3, original fields | Hello 4, contract and text markers |
| HTTP state | Original `application/json` state | Explicit Express media type and packed topology/frame envelope |

Later changes merged these formats.
The current formats are project version 1 with the `orderContract` marker, saved-state version 9, and hello version 6.
The HTTP state has one media type, `application/vnd.podsim.state-6+json`, for every project kind.
See [the protocol](protocol.md) and [operations](operations.md).

The Express contract marker is `express-v1`.
The tested source also had the text marker `order-text-base64-v1`.
All five order fields, From, To, PodID, DispatchReason, and ServiceID, used canonical padded RFC 4648 base64 in Express saves, full frames, and replacement deltas.
The current formats have no text marker and pack the order text for every project kind.
There is no raw-text fallback or encoding inference from a string's appearance.
Native values remain decoded UTF-8 strings.

The parser checks encoded token lengths before typed order allocation.
The limits are 88 encoded characters for 64 decoded bytes and 1,368 for 1,024 decoded bytes.
It rejects escaped base64 tokens, bad padding bits, invalid UTF-8, unknown or contradictory markers, duplicate fields, and markers in older versions.
Saved boarding records retain source-bound tuples.
Stream boarding records retain named raw berth IDs and distance baselines.

Topology contains the contract, registry, server identity, epoch, and project revision.
Its entire encoded response, including the registry, must fit 10 MiB plus 4 KiB.
Startup, project activation, and restore preflight this shape before accepting a new state.
The same source identity binds the HTTP topology and frame.
At the tested source, an Express HTTP request without `Accept: application/vnd.podsim.express-v1+json` received 406.
The Go remote client rejected Express marker presence under a missing or different response media type before it mutated the accepted state.
The current server and remote client use `application/vnd.podsim.state-6+json` for every project kind.

Trip commands bind the explicit order contract to the existing epoch.
A connection must negotiate another hello when the active contract changes.
The remote client publishes and acknowledges a candidate only after the qualified decoder and topology assembler accept it.
Numeric fields pass through Go decoding.
Browser JavaScript handles bytes and control messages.

## Semantic qualification

| Gate | Evidence |
| --- | --- |
| Real 20-person party | Native Express session trip, boarding, save, public full frame, HTTP, physical restore, and remote command/stream |
| Immutable party and storage | Accepted 20 stored records; rejected record 21, seat overflow, mixed private/shared riders, bad registry ID or directed pair |
| Class and route | Rejected changed class, incompatible station or route, historical Express rider, boarding origin mismatch, and large virtual link |
| Aggregate and prescan | Rejected 8,601 pending records and 8,600 pending plus an active rider; arrays checked before typed decoding |
| Negotiation | Rejected missing, null, unknown, duplicate, and older-version contract markers; explicit version 4 assembler required |
| Atomic rejection | Rejected candidate preserves prior assembler state; invalid remote publication has no ACK and preserves the last accepted state |
| Retained state | Actual encoded successor delta changes pending, rider, and boarding fields; mutation of returned containers does not change prior state or assembler cache |
| Numeric fidelity | Exact integers above 2^53, decimal-string sequences, exponent boundaries, maximum finite float, and minimum subnormal through public native and Go/WASM adapters |
| Foundation parity | Existing save golden files unchanged; old package behavior and raw encoder paths exercised separately from save 7 and hello 4 |

Manual admission retains the 200-request queue limit.
Restored pending work can exceed 200.
The session rejects new manual requests until that queue falls below the admission limit.
Fleet and registry limits remain 300.
Express wire storage accepts the native recovery bound of 8,600 pending records and at most 20 stored riders for an Express pod.
Active orders must also satisfy the native aggregate bound.
Completed display history is not counted as an active party.
The Go assembler uses contract-aware class, consent, service, route, station, and boarding checks.

The physical restore test preserves saved native facts and obtains the physical tier during real boarding.
Ordinary motion speed is reset by physical reconstruction, and forward grants are rebuilt.
This wire result does not claim speed or trajectory continuity.
Mixed-body geometry, stopping ownership, recovery identity conservation, and the independent physical oracle belong to native qualification.

## Measured encoded shapes

The tests use the real session save encoder and decoder, full/replacement stream adapters, topology parser and preflight, and public HTTP adapters.
They encode actual JSON and gzip bytes.
They cover all bounded collections, escaped text, raw defer-pod and berth fields, maximum-width numerics, registry entries, and a mixed compact certificate.
A second run replaces every order reason with a distinct deterministic 1,024-byte string that includes multibyte UTF-8, quotes, and backslashes.

| Asset | Raw bytes | Gzip bytes | Raw cap |
| --- | ---: | ---: | ---: |
| Modern save 7 | 77,043,290 | 818,840 | 83,886,080 |
| Mixed save 7 | 77,020,430 | 817,967 | 83,886,080 |
| Independent full frame | 54,641,672 | 574,732 | 67,108,864 |
| Independent replacement delta | 54,774,741 | 568,027 | 67,108,864 |
| Qualified topology | 10,399,903 | Separate response | 10,489,856 |
| Reference full frame | 52,879,647 | Not used by browser driver | 67,108,864 |
| Reference successor delta | 16,909,033 | Not used by browser driver | 67,108,864 |
| Packed HTTP envelope | 63,279,337 | 10,689,140 | 67,108,864 |
| Save with distinct text | 77,043,290 | 15,628,684 | 83,886,080 |
| Full with distinct text | 54,641,672 | 15,255,169 | 67,108,864 |
| Text replacement delta | 31,412,372 | 14,976,785 | 67,108,864 |
| HTTP with distinct text | 63,279,337 | 25,381,458 | 67,108,864 |

The next topology escape step produces 10,637,898 bytes and is rejected.
The save cap applies to both raw JSON and the gzip file.
At the tested source, the stream cap was 64 MiB raw and 65 MiB binary, and this patch did not change the caps.
The current stream caps are 65 MiB raw and 66 MiB binary.

These are independent wire-field shapes, not a proof that every combination is a reachable native state.
The reference HTTP shape passes public class, route, registry, and reference checks, but repeats order IDs and physical occupancy for width coverage.
The full storage shape includes 8,600 pending and 6,000 completed display records.
It does not claim 14,600 simultaneous active parties or unique accepted identities.
Native conservation is tested separately.
The approved analytic bounds and producer size checks remain necessary.

## CPU, memory, and browser limits

On pinned Go 1.27.1 with jsonv2, GOGC 400, and GOMAXPROCS 2:

| Native gate | Wall time | Peak RSS, KiB |
| --- | ---: | ---: |
| Widest adapters | 23.35 s | 1,623,612 |
| Retained predecessor, replacement, and HTTP | 7.09 s | 2,684,492 |
| Distinct text and production gzip | 20.72 s | 1,695,560 |

Root ran the public adapters in actual Chromium 143.0.7499.4 with Go/WASM and GOGC 100.
Topology took 2.05 s, full decode and assembly took 9.39 s, replacement took 2.36 s, and HTTP took 14.43 s.
Peak WASM memory was 2,457,337,856 bytes.
The sum of browser process RSS reached 2,760,204 KiB.
The largest gap in a 20 ms JavaScript heartbeat was 14.136 s.
No JavaScript error or wire-number conversion occurred.

This is a functional consumer pass with substantial blocking cost.
It is not a responsive-use claim.
The Node Go/WASM retained-state test also passed at GOGC 100 in 29.204 s.
The same broader retention test failed at GOGC 400 with an out-of-memory error at 4,251,254,784 bytes during HTTP assembly.
That test retains raw full and generated successor bytes plus several owned states.
The Chromium driver fetches a preencoded successor.
These results do not justify a deployment, default enablement, larger cap, or memory guarantee for another environment.

The session artifact manifest records exact fixture, source, gate, and mutation hashes.

## Gates and failures

Focused semantic and remote tests, scoped races, vet, lint, and fresh Go diagnostics passed.
The relevant package suites passed in 108.833 s for session and 1.005 s for remote on dependency v3.
They include the original foundation size and golden checks.
Fresh diagnostics, vet, lint, focused Express adapters, and foundation byte controls also pass on dependency v6.
Go diagnostics report only existing hints and information, with no errors or warnings.
Two fuzz campaigns passed: 68,904 canonical-text cases and 69,502 public versioned decoder cases.
No full-repository race is claimed.

Compiled caller mutations bypass contract selection, literal packed-text validation, whole-party admission, aggregate validation, class/link checks, HTTP media checks, and acceptance before ACK.
Tests rejected nine compiled caller mutations with assertion failures.
The mutation table distinguishes assertion kills, survivors, and build failures.
An initial ACK test mutation survived because its binary payload lacked gzip and failed before assembly.
The test was corrected to send valid gzip and require the registry semantic rejection.
The same compiled caller mutation then failed its assertion.

The first package run exposed schema-test assumptions about new optional fields and future versions, plus rejected-hello build visibility.
Those were corrected without changing foundation golden files.
Early compile and lint failures are retained with their terminal metadata.
An interrupted widest run has exit 130 and a host process scan that found no remaining child before edits.
Final gate statuses and source pins are in the measurement JSON and durable manifest.

## Requalification on the merged formats

This section requalifies the "Save bytes and shape" and "Stream and public consumers" rows of the approved contract on the current formats.
The earlier sections of this record are historical.
The current formats are project version 1 with the `orderContract` marker, saved-state version 9, hello version 6, and the HTTP media type `application/vnd.podsim.state-6+json`.
The tested source is `e7d653d` with two new test files, `internal/session/express_requal_test.go` and `internal/remote/express_requal_test.go`.
No production code changed.
The data is under `requalification_merged_formats` in [the measurement record](measurements/express-wire-qualification.json).

### Save bytes and shape

| Case | Tests | Status |
| --- | --- | --- |
| Widest-shape Express save files | `TestExpressWidestSaveAdapters`, modern and mixed | Pass |
| Escaped and multibyte text | `TestExpressRequalSaveShapes`, `TestExpressOrderText`, `TestExpressIncompressibleAssetAdapters` | Pass |
| Both boarding forms | `TestExpressRequalSaveShapes` for 20 source-bound tuples and the `journeyOrigin` form; `TestBoardingStateMalformed`; `TestExpressSaveStreamHTTPRoundTrip` for named stream records | Pass |
| Optional history | `TestExpressRequalSaveShapes`: 19 completed records with one active party, completed riders without tuples, and a pod without riders | Pass |
| Exponents | `TestExpressRequalSaveShapes`: both sides of `1e-7` and `1e+21`, the largest finite float, the smallest subnormal, and integers above 2^53; `TestExpressPublicNumericRoundTrip` | Pass |
| Malformed base64 | `TestExpressRequalSavePackedTextRefusals`, `TestExpressOrderText`, `TestExpressPublicTextAndShapeGuards`, `TestDecodeStateFileRejects` | Pass |
| UTF-8 and decoded limits | `TestExpressRequalSavePackedTextRefusals`: 64 and 1,024 decoded bytes pass; one more decoded byte, the next encoded length, and invalid UTF-8 fail | Pass |
| Exact gzip caps | `TestDecodeStateFileSizeBoundary`, `TestDecodeStateFileRejects`, `TestExpressWidestSaveAdapters` | Pass |
| Parser prescan | `TestExpressRequalSavePrescanBounds`, `TestStateJSONLimits`, `TestDecodeStateFileBombs`, `TestBoardingStateLimits` | Pass |
| Atomic rejection | `TestExpressRequalSaveRejectedAtomically`, `TestSavedInvalidMovedAside` | Pass |

The packed text refusals also pin the refusal order.
Each changed save has a speed that only the typed decode refuses, and that member comes first.
The packed text scan must refuse the file first, with its own error text.
The prescan test refuses 8,601 waiting trips, 21 riders, and 21 boarding tuples under the Express marker, and 2,601 trips and 9 riders without it.
A refused Express save moves aside, and the new session has no Express marker and no waiting trip.

### Stream and public consumers

| Case | Tests | Status |
| --- | --- | --- |
| Full and replacement delta assets | `TestExpressWidestStreamAdapters`, `TestExpressIncompressibleAssetAdapters` | Pass |
| Pending replacement groups | `TestExpressRequalChainRecovery`, `TestStreamMarkersSelectSections` | Pass |
| Sequence and epoch recovery | `TestExpressRequalChainRecovery`, `TestExpressRequalRemoteRecovery`, `TestApplyStreamRefusalOrder` | Pass |
| Topology and registry binding | `TestExpressRequalTopologyBinding`, `TestExpressPublicOrderGuards`, `TestStateEnvelopeMarkersMatchTopology` | Pass |
| Normalized class lane caches | `TestExpressRequalClassLaneCache`, `TestExpressPublicClassBindings` | Pass |
| Raw and gzip caps | `TestExpressRequalStreamCaps`, `TestStreamGzipExpansionBound`, `TestExchangeBoundsResponseBodies` | Pass |
| Go remote consumer | `TestExpressRemoteStream`, `TestExpressRemoteHTTPAndTripMarker`, `TestExpressRemoteInvalidStateHasNoACK`, `TestExpressRequalRemoteRecovery` | Pass |
| Compact HTTP negotiation | `TestStateHTTPNegotiation`, `TestAcceptsStateMedia`, `TestStateHTTPMediaFailClosed` | Pass |
| Browser/WASM roundtrip | Node Go/WASM runs of the public adapter tests and `TestExpressRequalCost`; see [the browser record](express-browser-qualification.md) | Node pass; Chromium open |
| Fail-closed old readers | `TestStreamHelloRefusesOtherVersions`, `TestStreamBuildBeforeIncompatiblePayload`, `TestSavedVersionRefusals`, `TestExpressRequalSaveRejectedAtomically`, `TestAcceptsStateMedia` | Pass |

The chain test applies an Express full frame and a delta that replaces the pending group.
A delta after a gap, a delta of another stream, and a delta of another epoch fail with their own error text.
They leave the accepted frame and the assembler unchanged.
A full frame of the new epoch starts a new chain, and only an assembler of the new topology accepts it.
The remote test runs the Go client over two connections.
The client acknowledges the first full frame, refuses the delta after a gap without an acknowledgement, and keeps the accepted state.
On the second connection it reads the topology of the new epoch and acknowledges the new full frame.

The class lane cache refuses a lane that admits Express by itself when the lane ends at a berth of a station that refuses Express, or belongs to such a station.
A vehicle without a class uses the legacy lanes.
The stream caps test decodes exactly 65 MiB of JSON and refuses one more byte.
It refuses a gzip message of 66 MiB plus one byte, a second gzip member, and an inflated size past the JSON cap.
The HTTP state uses one media type for every project kind.
The Express and compact-pair media types of earlier servers get HTTP 406, and the client refuses a reply of another media type.

### Measured sizes on the merged formats

The widest assets are the independent wire-field shapes of the historical record, encoded with the current formats.
The runs used Go 1.27.1, `GOMAXPROCS=2`, and the default `GOGC=100`.

| Asset | Raw bytes | Gzip bytes | Raw cap |
| --- | ---: | ---: | ---: |
| Modern save 9 | 77,043,253 | 819,058 | 83,886,080 |
| Mixed save 9 | 77,020,393 | 818,484 | 83,886,080 |
| Independent full frame | 54,641,634 | 573,247 | 68,157,440 |
| Independent replacement delta | 54,774,703 | 564,014 | 68,157,440 |
| Qualified topology | 10,399,903 | Separate response | 10,489,856 |
| Reference full frame | 52,879,609 | Not measured | 68,157,440 |
| Reference successor delta | 16,908,995 | Not measured | 68,157,440 |
| Packed HTTP state | 63,279,281 | 10,795,815 | 68,157,440 |
| Save with distinct text | 77,043,253 | 15,626,367 | 83,886,080 |
| Full with distinct text | 54,641,634 | 15,256,651 | 68,157,440 |
| Text replacement delta | 31,412,334 | 14,975,170 | 68,157,440 |
| HTTP with distinct text | 63,279,281 | 25,495,065 | 68,157,440 |

The next topology escape step produces 10,637,898 bytes, and the decoder and the producer preflight refuse it.
The save cap applies to the raw JSON and to the gzip file.
The stream caps are 65 MiB of JSON and 66 MiB of gzip.

### Time and heap on the merged formats

`TestExpressRequalCost` reads the exported assets.
It logs the time of each stage and the largest heap object size that a 1 ms sampler sees.
The live heap is the heap after a full collection.
The test keeps the accepted reference frame while it decodes and applies the successor delta.
It then releases the reference frame, and it keeps the successor state and the assembler while it decodes the HTTP state.

| Stage | Native seconds | Native peak heap, MiB | Node WASM seconds | Node WASM peak heap, MiB |
| --- | ---: | ---: | ---: | ---: |
| Save decode and boarding resolution | 4.118 | 304.1 | 19.468 | 483.4 |
| Save encode and gzip | 0.402 | 264.1 | 1.621 | 264.1 |
| Full frame inflate and decode | 2.426 | 148.7 | 10.915 | 244.6 |
| Full frame encode and gzip | 0.334 | 240.6 | 1.065 | 248.1 |
| Replacement delta inflate and decode | 2.330 | 167.9 | 10.143 | 196.9 |
| Replacement delta encode and gzip | 0.298 | 261.0 | 0.881 | 261.6 |
| Reference full decode, apply, and assembly | 3.396 | 834.1 | 17.272 | 1,989.8 |
| Successor delta with the predecessor kept | 0.877 | 858.7 | 3.278 | 859.6 |
| HTTP decode with the stream state kept | 4.982 | 1,543.6 | 20.534 | 2,285.0 |

With the predecessor and the successor kept, the live heap was 499.8 MiB.
Without the predecessor, it was 486.3 MiB.
The predecessor kept the difference, 13.4 MiB.
The native run had a peak RSS of 1,771,824 KiB in 20.6 s.
The Node 24.21.0 run had a peak RSS of 2,556,872 KiB in 92.6 s.
WASM runs on one thread, so the sampler adds work to the measured stages.
Each value comes from one run on a shared host.
An earlier run of the same stages under more host load took up to 1.5 times as long.
The widest test runs had these peak RSS values: save 646,872 KiB, stream 599,956 KiB, topology and HTTP 1,138,912 KiB, and distinct text 1,267,260 KiB.
These numbers do not give a memory bound for another environment.

### Mutations

Each mutant replaced one source file through a `go test -overlay` file, so no tracked source changed.
A row is here only when the existing session and remote suites, including the tests that `-short` skips, pass with the mutant.
Each mutant compiled, and each killing test failed on an assertion.

| Source line at `e7d653d` | Mutation | Killing test | Result |
| --- | --- | --- | --- |
| `internal/session/state_file.go:435` | Ignore the error of `scanPackedOrders` | `TestExpressRequalSavePackedTextRefusals` | Killed |
| `internal/session/stream_codec.go:663` | Remove the gzip message cap of `InflateStream` | `TestExpressRequalStreamCaps` | Killed |
| `internal/session/stream_service.go:42` | Remove the JSON cap of `decodeMarkedJSON` | `TestExpressRequalStreamCaps` | Killed |
| `internal/session/express_orders.go:192` | Admit a lane whose start node refuses the class | `TestExpressRequalClassLaneCache` | Killed |
| `internal/session/express_orders.go:195` | Admit a lane whose end node refuses the class | `TestExpressRequalClassLaneCache` | Killed |
| `internal/session/express_orders.go:198` | Admit a lane of a station that refuses the class | `TestExpressRequalClassLaneCache` | Killed |
| `internal/session/express_orders.go:219` | Do not give an empty class the legacy class | `TestExpressRequalClassLaneCache` | Killed |

Existing tests already kill five other mutants of the guards that the new tests check.
`TestMarkerFormsSelectLimits` kills the Express header table at `state_file.go:398` and the waiting-trip bound at `format_limits.go:32`.
`TestExpressWidestSaveAdapters` and `TestComposedWorstCaseFormats` kill the rider and boarding bounds at `format_limits.go:33` and `format_limits.go:34`.
`TestStreamCodecRejectsInvalidEnvelope` kills the inflated size and trailing member check at `stream_codec.go:677`.
One earlier form of the waiting-trip mutant did not compile, and the table does not count it.

`TestExpressPublicAssetRetention` failed on `e7d653d`.
`TestExpressWidestTopologyHTTPAdapters` exports `reference-full.json` with the widest speed of the stream fixture, and the assembler refuses that speed, which is correct.
The test now sets the playback speed to 60 after the decode, as `TestExpressRequalCost` does, and it passes.
The test runs only with `PODSIM_EXPRESS_PUBLIC_ASSET_DIR`, so CI does not run it.

### Open items

- The headless Chromium roundtrip did not run.
  See [the browser record](express-browser-qualification.md) for the launch errors.
- The widest-fixture browser resource run belongs to the "Resource and consumer cost" row, and this requalification did not repeat it.
- The widest assets repeat order IDs and occupancy for width coverage.
  They do not show a reachable native state.
- No race run is claimed for this requalification.
