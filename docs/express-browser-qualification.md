# Express browser qualification

The opt-in `express-v1` browser path passed functional checks on October 3, 2026.
The largest accepted wire fixtures use substantial memory and block the browser event loop.
These results do not authorize deployment or default enablement.

The operating limits and encoding rules are in [the approved Express contract](express-physical-encoding-contract-proposal.md).
Native motion and restore evidence is in [the native qualification](express-native-qualification.md).
Packed save, stream, and HTTP evidence is in [the wire qualification](express-wire-qualification.md).
The corresponding data is in [the measurement record](measurements/express-browser-qualification.json).

## Actual browser controls

An isolated production server and the production Go/WASM canvas ran in headless Chromium.
Twenty-five presses of the party increase control stopped at 20.
The browser submitted one private party of 20 with the `express-v1` marker.
The server accepted the complete order.

The same browser then received a foundation project.
The stream changed from hello version 4 to version 3.
The party control stopped at eight and the next command omitted the marker.
The server accepted that private party of eight.
The captured earlier command still contained party 20 and its marker.

The actual Go editor worker imported and exported a version 1 wrapper with a version 4 scenario.
Undo returned to the foundation draft; redo restored the Express draft.
The marker, fleet class, and service registry survived these operations.
A version 4 import with a null marker was rejected.
The accepted history and exported draft stayed unchanged.

The local server and browser stopped after the checks.
The stopped London demo was not used.

## Public Go/WASM adapters

The public stream decoder, stream application, native assembler, and qualified HTTP decoder preserved integer values above `2^53`, the largest `uint64`, the largest finite `float64`, a subnormal value, and bounded control and multibyte text.
Production JavaScript did not parse the wire values into JavaScript numbers.

The browser decoded independently populated maximum-size wire fields.
It retained the previous accepted state while it decoded a replacement delta and a qualified HTTP response.
Changes to returned pending orders, riders, boardings, and pod classes did not alter the previous state or assembler cache.
These fixtures prove encoding and container behavior.
They do not represent a reachable native traffic state or prove native order identity conservation.

## Browser resource cost

The resource run used Chromium 143.0.7499.4, Go 1.27.1, `jsonv2`, and the production `GOGC=100` setting.
The scratch driver called the production public adapters.
JavaScript handled asset transport and small measurement records.

| Stage | Input bytes | Elapsed seconds |
| --- | ---: | ---: |
| Topology and assembler creation | 10,399,903 | 2.05 |
| Full stream and native assembly | 52,879,647 | 9.39 |
| Replacement delta and retained-state checks | 16,909,033 | 2.36 |
| HTTP decode with the previous state retained | 63,279,337 | 14.43 |

Elapsed times include local asset reads.
The WASM memory reached 2,457,337,856 bytes.
The summed browser process RSS reached 2,760,204 KiB.
A 20 ms heartbeat had a maximum gap of 14.136 seconds.
The browser had no JavaScript errors and decoded all four stages.
This is functional acceptance with a measured blocking cost.
It does not establish responsive operation for these maximum-size fixtures.

The public Go/WASM test also passed with `GOGC=100` in 29.204 seconds.
That test keeps additional generated successor, delta, and native-state containers.
With the native benchmark setting `GOGC=400`, it exhausted WASM memory at 4,251,254,784 bytes during HTTP assembly.
The failed receipt is retained.
The successful Chromium run does not qualify that configuration.

## Review and source boundaries

Compiled mutations of the actual editor normalization guard and the remote HTTP marker guard caused targeted assertion failures.
The controls passed and the original sources were restored by hash.
The editor and view package tests, race checks, static checks, and Go diagnostics passed.

Browser runs used the frozen native v3 and wire v2 implementations named in the measurement record.
The final native v6 dependency adds the canonical checkpoint project decoder and changes comments, error capitalization, and unused private wrappers.
The editor and view checks passed against that final dependency.
The wire qualification records its final dependency checks.
Source pins keep these claims separate.

The measurement record names each evidence directory.

## Requalification on the merged formats

This section covers the browser/WASM roundtrip case of the "Stream and public consumers" row on the current formats.
The earlier sections of this record are historical.
The current formats are saved-state version 9, hello version 6, and the HTTP media type `application/vnd.podsim.state-6+json`.
The Node runs tested `e7d653d` with the new requalification tests.
The Chromium run tested `94683aa` before a review fold that only clears a restore mark in compact queue motion.
The wire record lists the other cases in [its requalification section](express-wire-qualification.md#requalification-on-the-merged-formats).
The data is under `requalification_merged_formats` in [the measurement record](measurements/express-browser-qualification.json).

### Node Go/WASM

The session test binary was built with `GOOS=js GOARCH=wasm` and run in Node 24.21.0 with `GOGC=100`.
These public adapter tests passed in 10.0 s:

- `TestExpressPublicNumericRoundTrip`, `TestExpressOrderText`, `TestExpressPublicTextAndShapeGuards`, `TestExpressMarkersAndAtomicAssembly`, `TestExpressPublicOrderGuards`, and `TestExpressPublicClassBindings`.
- `TestExpressRequalSaveShapes`, `TestExpressRequalSavePackedTextRefusals`, `TestExpressRequalSavePrescanBounds`, `TestExpressRequalChainRecovery`, `TestExpressRequalTopologyBinding`, `TestExpressRequalClassLaneCache`, and `TestExpressRequalStreamCaps`.

`TestExpressRequalCost` also passed in Node Go/WASM with the widest assets.
It decoded the widest save, full frame, replacement delta, reference frame, and HTTP state.
It kept the accepted reference frame while it applied the successor delta.
It kept the successor state and the assembler while it decoded the HTTP state.

| Stage | Seconds | Peak heap, MiB |
| --- | ---: | ---: |
| Save decode and boarding resolution | 19.468 | 483.4 |
| Full frame inflate and decode | 10.915 | 244.6 |
| Replacement delta inflate and decode | 10.143 | 196.9 |
| Reference full decode, apply, and assembly | 17.272 | 1,989.8 |
| Successor delta with the predecessor kept | 3.278 | 859.6 |
| HTTP decode with the stream state kept | 20.534 | 2,285.0 |

The Node process had a peak RSS of 2,556,872 KiB.
WASM runs on one thread, so the 1 ms heap sampler adds work to the measured stages.
These numbers do not show responsive browser operation.

### Headless Chromium

The headless Chromium roundtrip passed on October 6, 2026.
The `web` task built the browser files.
`cmd/serve` served them on 127.0.0.1 with the project of `expressProject` in `internal/project/express_test.go`.
That project has one Express pod and the `harbor` to `market` service with a party limit of 20.
`chrome-headless-shell` 143.0.7499.4 opened the production game page at 1100 by 728 CSS pixels.
A Node 24.21.0 driver used the DevTools protocol through the built-in WebSocket of Node.

Chromium ran under `prlimit --data=8000000000:8000000000`, a data-segment limit of 8,000,000,000 bytes.
The server and the driver ran under `ulimit -v 16000000`.
All processes ran with `nice -n 19`.
Chromium reserves large address ranges that it does not use.
In an earlier attempt on the same day, `chrome-headless-shell` exited with status 133 (SIGTRAP) under `ulimit -v 16000000` before it opened the debugging port, with no log.
In that attempt, the full `chrome` binary stopped at startup with `FATAL:chrome/browser/process_singleton_posix.cc:292] Check failed: . socket() failed: Operation not permitted (1)`, because the command sandbox refuses the Unix socket of the process singleton.

The run saw these results:

- The hello had version 6 and the `express-v1` order marker.
- The first acknowledgement came 1,124 ms after the navigation started.
- The driver pressed the party increase control 25 times and then pressed Enter.
- The page sent one `trip` command from `harbor` to `market` with `partySize` 20, `sharingConsent` `private`, and `orderContract` `express-v1`.
- The server accepted the command with HTTP 200 and order ID 1.
- Delta 109 contained order 1 with a party of 20, and the client acknowledged that delta.
- The client received 241 frames on one stream: 1 full frame and 240 deltas.
  It sent 241 acknowledgements, one with the stream and sequence of each frame.
- Each frame had the `express-v1` marker.
- The largest frame was the full frame, with 1,708 bytes of JSON and 796 bytes of gzip.
- The page had no exceptions and no console API messages.
- The status element was hidden and kept the load text "Downloading Podsim… 30.6 MB".

The widest-fixture browser resource run belongs to the "Resource and consumer cost" row, and this requalification did not repeat it.
