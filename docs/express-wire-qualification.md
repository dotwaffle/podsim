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

| Consumer | Foundation | Express |
| --- | --- | --- |
| Project | Versions 1 through 3, raw UTF-8 | Version 4, explicit `orderContract` |
| Session save | Versions 2 through 6, raw order text | Version 7, contract and text markers |
| Stream | Hello 1 through 3, original fields | Hello 4, contract and text markers |
| HTTP state | Original `application/json` state | Explicit Express media type and packed topology/frame envelope |

The Express contract marker is `express-v1`.
The text marker is `order-text-base64-v1`.
All five order fields, From, To, PodID, DispatchReason, and ServiceID, use canonical padded RFC 4648 base64 in Express saves, full frames, and replacement deltas.
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
An Express HTTP request without `Accept: application/vnd.podsim.express-v1+json` receives 406.
The Go remote client rejects Express marker presence under a missing or different response media type before it mutates the accepted state.

Trip commands bind the explicit order contract to the existing epoch.
A connection must negotiate another hello when the active contract changes.
The remote client publishes and acknowledges a candidate only after the qualified decoder and topology assembler accept it.
Numeric fields pass through Go decoding.
Browser JavaScript handles bytes and control messages.

## Semantic qualification

| Gate | Evidence |
| --- | --- |
| Real 20-person party | Native Express session trip, boarding, save, public full frame, HTTP, physical restore, and remote command/stream |
| Immutable party and storage | Accepted 20 stored records. rejected record 21, seat overflow, mixed private/shared riders, bad registry ID or directed pair |
| Class and route | Rejected changed class, incompatible station or route, historical Express rider, boarding origin mismatch, and large virtual link |
| Aggregate and prescan | Rejected 8,601 pending records and 8,600 pending plus an active rider. arrays checked before typed decoding |
| Negotiation | Rejected missing, null, unknown, duplicate, and older-version contract markers. explicit version 4 assembler required |
| Atomic rejection | Rejected candidate preserves prior assembler state. invalid remote publication has no ACK and preserves the last accepted state |
| Retained state | Actual encoded successor delta changes pending, rider, and boarding fields. mutation of returned containers does not change prior state or assembler cache |
| Numeric fidelity | Exact integers above 2^53, decimal-string sequences, exponent boundaries, maximum finite float, and minimum subnormal through public native and Go/WASM adapters |
| Foundation parity | Existing save golden files unchanged. old package behavior and raw encoder paths exercised separately from save 7 and hello 4 |

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
The stream cap is 64 MiB raw and 65 MiB binary.
These caps remain unchanged.

These are independent wire-field shapes, not a proof that every combination is a reachable native state.
The reference HTTP shape passes public class, route, registry, and reference checks, but repeats order IDs and physical occupancy for width coverage.
The full storage shape includes 8,600 pending and 6,000 completed display records.
It does not claim 14,600 simultaneous active parties or unique accepted identities.
Native conservation is tested separately.
The measured assets are concrete fixtures, not a proof of one reachable worst case.
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
