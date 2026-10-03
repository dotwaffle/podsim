# Group pod physics

The group class supports eight seats, parties through eight, and a six-meter body length.
The model uses the centered six-meter-radius envelope approved in [the large pod proposal](large-body-physics-proposal.md).
Express retains its metadata and remains unavailable for physical operation.

This model does not certify a manufactured vehicle, steering geometry, or seating arrangement.
No station, fleet, speed, or default policy changes automatically.

## Geometry and ownership

An interaction with a large class requires at least 20 meters between reference positions on overlapping physical planes.
Large-admitting lanes require at least 40 meters of length and actual cells of at least 20 meters.
Station and berth masks must both admit the class.
Network preparation checks the largest admitted class, even when the fleet contains only small pods.
It also checks small lanes that meet a large-admitting lane or berth node.

A 20-meter path gap on a curve can give less than 20 meters of physical separation.
Preparation derives a larger retention tail from the actual movement polyline.
Let `c` be the minimum projection of a unit segment tangent onto the unit lane chord.
For curved paths, the tail is `20 / (c * (1 - conflictSlack))`.
Straight paths retain a 20-meter tail.
Every relevant nonzero segment must advance on the chord axis.
Preparation rejects folded paths, zero chords, and nonfinite bounds.
The tail can exceed the lane length and is never reduced to fit that length.

Each immutable lane-cell index stores its tail and its independent start-node tail.
Endpoint ownership covers every cell that overlaps the applicable endpoint tail.
Reservation, release, route trimming, origin retention, and cold restore use these same bounds.
An unrelated destination node cannot enlarge the retained origin.
Junction conflict intervals already contain their clearance expansion, so release does not add another tail.

Old-only lanes retain the existing 24-meter minimum, 12-meter clearance, and release arithmetic.
The compact queue formula and its protected constants stay unchanged.
Large and mixed virtual links and compact certificates are invalid.
An ordinary compact group beside a large pod must satisfy the larger outside separation bound.

## Body planes

The native safety observation captures private, owned plane arrays when the fleet contains a large pod.
It binds each array to the complete pod value from that observation.
The route window includes the retained rear and a front bound that encloses the body.
A stationary berth includes its raw plane and every incident lane plane.
A departing pod retains its raw origin berth plane until origin release.

A large pair receives a plane exemption only when every cross-pair of occupied locations proves separation.
Missing arrays, changed pod values, or overlapping incident planes refuse the exemption.
Public center-only locations cannot grant this exemption.
The observation uses the existing tick rate, acceleration, braking, and authored speed limits.

## Restore and public consumers

Physical restore reconstructs the current footprint and all unexpired past resources with the larger tails.
It preserves retained berth, node, and junction ownership.
Future track grants can differ because saves do not retain speed.
Restore rejects large links and compact certificates before either restore tier runs.
Recorded fallback preserves request identities, consent, original times, and distance baselines.

Native saves have no geometry fingerprint.
Native restore validates the supplied geometry, classes, routes, and origin.
The session save resolver binds the state to its saved source project before native restore.
No new project, save, or stream version is required.

The editor checks class-specific lane lengths and pair clearances.
Large drafts also run native preparation, including drafts without station banks.
A failed edit returns no patch.
The stream assembler checks group station and berth admission and all current, motion, and display lane masks.
It rejects group links before class caches or published frames change.

## Qualification evidence

The [measurement record](measurements/group-physics-qualification.json) pins native sources, checks, trace hashes, and byte maxima.

The focused tests include actual journeys with group, compact, and legacy leaders and followers.
They cover straight and curved motion, stopped following, merges, ordinary and bank stations, parking, and buffer policies.
Every observed tick checks physical separation, speed changes, owned stopping bounds, position continuity, and order conservation.
Cold checkpoints cover passenger service, empty fetch, origin retention, route trimming, and intermediate shared unloading.
Actual consumers exercise save-6 restore and hello-3 full and replacement-delta frames.

Independent review found a missing raw origin plane during departure.
The corrected observer retains that plane until origin release.
Compiled mutations that remove endpoint extent or derived constructor bounds cause actual motion to violate the 20-meter bound.
Both endpoint regressions measure 19.9939634194 meters at tick 11946.
The tests fail on that measured gap before their resource-cell assertions.

Four complete 36,000-tick ordinary snapshot traces match the pre-change baseline exactly.
Existing ordinary encoded fixtures remain unchanged.
Separate maximum encoding fixtures contain actual group class values and use the existing count limits.
They do not claim that every combined maximum is physically reachable.

| Encoding fixture | Raw bytes | Gzip bytes | Cap in bytes |
| --- | ---: | ---: | ---: |
| Historical save | 83,270,696 | 802,330 | 83,886,080 |
| Modern save | 83,240,396 | 806,632 | 83,886,080 |
| Mixed save | 83,255,546 | 805,668 | 83,886,080 |
| Full stream | 62,173,518 | Not applicable | 67,108,864 |
| Replacement delta | 62,306,597 | Not applicable | 67,108,864 |

The save fixtures pass gzip, inflate, decode, source-index resolution, and re-encode checks.
The queue, stored rider, manual submission, fleet, registry, save-byte, and stream-byte limits stay unchanged.
Group qualification does not qualify express operation, a larger encoding, service capacity, or default adoption.

The full repository functional suite, full native race suite, and coupled group save and stream race tests pass.
Vet, lint, fresh Go diagnostics, browser tests, and WASM compilation also pass.
The full save race suite reaches its 14-minute timeout inside the existing maximum-size compact archive decoder.
The regular suite checks those maximum encodings.
The record preserves the timeout and the successful scoped race checks.
