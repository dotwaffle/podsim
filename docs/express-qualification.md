# Express qualification

This record states the conclusions of the native, wire, and browser qualification of the opt-in `express-v1` order contract.
The [approved contract](express-physical-encoding-contract-proposal.md) defines the encoding and the consumer requirements.
The detailed runs, measurement data, and mutation tables are in git history.

## Authority

The results authorize no deployment and no default.
Express stays opt-in through the `orderContract` marker (see [operations](operations.md)).
No physical number changed.
Foundation behavior and byte output stayed the same with the marker absent.

## Limits

| Limit | Value |
| --- | --- |
| Express fleet | 600 pods |
| Express order bound | 17,000 orders |
| Saved state | 128 MiB, raw and gzip |
| Stream JSON | 64 MiB, plus a derived gzip allowance |
| Project JSON | 32 MiB |
| Project size | 12,000 nodes, 20,000 lanes, 600 stations |
| Stored riders for one Express pod | 20, including completed history |

The October 15 [byte-budget amendment](service-byte-contract-proposal.md#october-15-2026-amendment) sets the byte caps.
Earlier byte figures in the first runs used smaller caps and are no longer current.

## Express has no byte proof

The widest-shape byte proofs were deleted (`TestExpressWidestSaveAdapters`, `TestExpressWidestStreamAdapters`, `TestExpressWidestTopologyHTTPAdapters`, `TestExpressIncompressibleAssetAdapters`, `TestExpressRequalCost`, `TestExpressPublicAssetRetention`, and `TestComposedWorstCaseFormats`).
`TestPlainStateFileWorstCaseSize` and `TestPlainStreamMaximumEncoding` prove the plain formats only.
Express has no byte proof for the current limits.
After a change to a count or byte limit, the Express sizes are unchecked.

## Native behavior

The native simulator passed 84 independent ledgers and 985,214 checked ticks.
A pod class of 20 seats and a 10 m body runs through following, merging, banking, parking fetch, and occupied pickups.
Mixed parties share the 20 seats whole.
A party never splits.
Large pairs keep 20 m of separation on the common-plane fixtures.
Cold restore keeps whole parties, order IDs, options, boarded status, and distance totals.
It resets speed to zero and rebuilds forward grants.
It does not claim identical live futures after a restore.
Large vehicles reject virtual, buffer, and compact links.

An overflow refusal leaves counters and accepted state unchanged.
Manual admission keeps its 200-request queue limit.
A restored queue can exceed 200, and the session then refuses new manual requests until it falls below the limit.
On-demand service keeps its eight-party and eight-stop bounds.

## Wire behavior

Saves, full frames, replacement deltas, and the HTTP state carry the marker and the packed order text.
The parser checks encoded token lengths before it allocates typed orders.
It refuses escaped base64, bad padding bits, invalid UTF-8, unknown or duplicate markers, and a marker in an older version.
The topology response must fit 10 MiB plus 4 KiB.
The HTTP state has one media type for every project kind.
A candidate state that fails a check leaves the accepted state unchanged, and the remote client sends no acknowledgement for it.
Integers above 2^53, the largest finite float, and the smallest subnormal survive the Go and Go/WASM adapters.
The 2026-10-19 requalification on the merged formats passed with no production code change.
Its widest fixtures repeat order IDs and occupancy for width coverage, so they do not show a reachable native state.

## Browser behavior

Headless Chromium submitted one private party of 20 with the marker, and the server accepted it.
The party control stopped at 20 for Express and at eight for a foundation project.
The editor kept the marker, fleet class, and service registry through import, export, undo, and redo.
It rejected a null marker.
The browser roundtrip of the game page passed with 241 frames and no page errors.

The widest fixtures use heavy memory and block the browser event loop.
The widest-fixture run in Chromium on the earlier formats gave these values:

| Stage | Input bytes | Seconds |
| --- | ---: | ---: |
| Topology and assembler | 10,399,903 | 2.05 |
| Full stream and assembly | 52,879,647 | 9.39 |
| Replacement delta | 16,909,033 | 2.36 |
| HTTP decode with predecessor kept | 63,279,337 | 14.43 |

WASM memory reached 2.46 GB, and the summed browser RSS reached 2.76 GB.
A 20 ms heartbeat had a gap of 14.1 s.
This is a functional pass, not a claim of responsive use.
In Node Go/WASM the widest retained-state test passed at `GOGC=100` and ran out of memory at `GOGC=400`.
The result gives no memory guarantee for another environment.

## Open items

- No full-repository race run is claimed for the wire or browser work.
- The widest-fixture browser resource run was not repeated on the merged formats.
