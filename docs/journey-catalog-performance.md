# Journey catalog cache measurements

The client now builds its sorted station catalog once for each network identity.
It caches normalized names and codes with that catalog.
The separate passenger-station slice retains network order for default selections.
The change preserves Unicode ID matching, exact-match precedence, station codes, and the Archway selection fix.
Network revisions, rewinds, epochs, and server restarts rebuild the catalog through the existing display index.
No project, protocol, saved-state, or UI contract changes.

## Native benchmark

The benchmark uses LondonFull and compares the cached lookup with the retained original sort and match algorithm.
Both implementations return identical results in the independent oracle tests.
Index construction occurs before timing for the cached cases.
These measurements describe repeated catalog access, not whole-frame rendering or initial topology load.

| Query | Original allocations | Cached allocations | Original allocated bytes | Cached allocated bytes |
| --- | ---: | ---: | ---: | ---: |
| Empty | 4,055 | 0 | 124,992 | 0 |
| arc | 4,593 | 2 | 131,512 | 288 |
| CHX | 4,594 | 3 | 131,424 | 200 |

The final benchmark runs twice after all validation processes finish.
The machine is an AMD Ryzen 5 3600 with Go 1.27.1 and GOMAXPROCS 2.

## Chrome comparison

The comparison freezes the server at `c93bc9a` and restores the saved LondonFull and LondonCentral traffic fixtures at tick 216000, which the [live admission comparison](admission-live-performance.md) also uses.
Only the browser WASM build changes.
GOGC is 100 for the server.
Each case runs twice, with baseline and candidate order reversed for the second repetition.
Cases are LondonFull with one client, and LondonCentral with one or three clients.
Each server window lasts about 60 seconds at requested speed 60x.

All twelve cases pass the protocol checks.
Every client uses one socket and one initial full snapshot, then shared deltas.
No client polls HTTP state after startup.
The three-client cases add 300 and 600 ms round-trip delays to two WebSocket connections.
There are no extra snapshots or reconnects from that delay.

Each value below gives baseline / candidate.
Script and capture values are per-client means across the two repetitions.
Three-client values also average the three client captures in each run.
Playback values average the two server runs.

| Preset | Clients | Script time | Client capture duration | Actual playback |
| --- | ---: | --- | --- | --- |
| LondonFull | 1 | 40.73 s / 30.36 s | 60.38 s / 60.32 s | 59.97x / 59.97x |
| LondonCentral | 1 | 24.25 s / 23.32 s | 60.29 s / 60.29 s | 35.93x / 35.93x |
| LondonCentral | 3 | 9.74 s / 8.95 s | 61.02 s / 61.00 s | 31.35x / 35.89x |

The Full script-time reduction is about 25%, with both variants retaining 60x.
Their median animation-frame intervals stay near 33.3 milliseconds.
The browser process tree still uses about 477-478 CPU seconds per 60-second server window.
Software rendering remains a substantial cost, so lower script time does not imply the same reduction in total browser CPU.

All eight Central runs reduce from 60x to 15x.
One baseline three-client repetition reduces earlier and averages 26.82x.
The other Central repetitions average about 35.9x.
Different achieved playback produces different traffic and completion cohorts.
The Central values therefore do not isolate a rendering improvement for identical simulated work.
The catalog change does not resolve Central speed reductions.

The measurement retains the earlier limits: active profiling, software rendering, restored traffic with cold route caches, and proxy publication timestamps.
Client captures occur sequentially after pausing, so they extend beyond the server window.
The table records those capture durations.
Two repetitions do not establish a general frame-rate, capacity, or sustained-playback guarantee.

## Validation

The full Go suite, view race suite, native and WASM builds, fresh gopls checks, and static analysis pass.
The new oracle tests cover Unicode IDs and names, ambiguous codes, duplicate names, empty networks, and identity invalidation.
Tests also preserve default passenger order and the existing Archway blur regression.
Independent review found no ownership or behavior blocker.
A direct Chrome interaction check selected Archway after typing `arc` and blurring onto its result.
It also resolved `CHX` to Charing Cross without browser errors.

[Arm measurements](measurements/journey-catalog-arms.csv) preserve per-run CPU, memory, and playback results.
[Client measurements](measurements/journey-catalog-pages.csv) preserve capture duration, script, ACK, inflate, and rendering figures.
[Metadata](measurements/journey-catalog.json) records frozen inputs and raw hashes.
