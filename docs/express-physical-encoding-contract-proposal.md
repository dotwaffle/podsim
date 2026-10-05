# Express physical and encoding contract proposal

Status: **approved, implemented, and qualified on October 3, 2026**.
The user approved item 8b and the contract below with the 6b/8b batch.
Deployment, runtime enablement, and default changes require a separate batch.
The [native](express-native-qualification.md), [wire](express-wire-qualification.md), and [browser](express-browser-qualification.md) records contain qualification evidence.

The source descriptions below refer to the reviewed baseline, `53321e4c5decc6c661df64a40378ee37614dcffc`.
The [large-body proposal](large-body-physics-proposal.md) approved the dimensions and common conservative candidate.
The [service contract](service-contract-proposals.md) and [byte contract](service-byte-contract-proposal.md) approved metadata and parser recognition.
Group passed its applicable gates in [qualification](qualification.md).
The qualification records cover the opt-in contract and retain its resource limits.
The combined Go suite, web tests, static checks, and production builds passed.
See [the integration measurement](measurements/continuation-express-integration.json).

## Recommended decisions

The approved batch covers implementation and qualification of the following contract.

| Decision | Proposed bound or rule |
| --- | --- |
| Opt-in contract | `express-v1` in a version 1 project. Without the coupling marker, it selects save 7 and stream hello 4 |
| Express profile under that contract | 20 seats, new whole-party size 1 through 20, centered 10 m body, maximum width 2.5 m |
| Large interaction candidate | Existing 6 m envelope radius, 20 m separation and retention, lanes at least 40 m, actual cells at least 20 m |
| Stored Express records | At most 20 rider records, including completed history, with at most 20 aligned boarding records |
| Express service pooling | At most 20 active parties, subject to the authored service limit and total of 20 passengers |
| On-demand pooling | Existing maximum of eight active parties and eight stops, also for an Express pod |
| Express-contract storage | At most 8,600 pending records and 8,600 outstanding records in aggregate |
| Wire encoding | Canonical padded base64 for five bounded order-text fields, with an explicit discriminator |
| Byte caps | Existing save 80 MiB, raw stream 64 MiB, binary stream 65 MiB, and topology 10 MiB plus 4 KiB |
| Other caps | Existing fleet 300, service registry 300, and manual admission 200 |

Outstanding records mean pending orders plus noncompleted onboard orders.
Completed onboard history counts toward the per-pod storage bound, but not the outstanding aggregate.
The aggregate is a new Express-contract admission and restore rule.
It prevents repeated recovery from increasing the number of accepted outstanding records beyond the reviewed bound.

The foundation contract retains eight new passengers per party, 2,600 pending records, and eight stored riders per pod.
Legacy and compact capacities remain unchanged.
Group remains eight seats with its qualified 6 m profile.
No common small-class arithmetic, authored speed, or protected compact rule changes.

## Existing recognition is not operating approval

[`vehicle_class.go`](../internal/sim/vehicle_class.go) currently reports Express as 20 seats, maximum new party size eight, body length zero, and physically unsupported.
Placement, startup, and both restore tiers reject that unsupported profile.
[`order_state.go`](../internal/session/order_state.go) recognizes 6,200 waiting records and 20 rider records in save 6.
Those parser limits do not authorize such native operating states.
[`state.go`](../internal/sim/state.go) and [`state_contract.go`](../internal/sim/state_contract.go) still enforce 2,600 pending records and eight stored riders.
Stream publication also retains those operating bounds.

The current session derives its recovery bound from `200 + 300 * 8 = 2,600` total orders.
The independent native maxima, 2,600 waiting plus 2,400 onboard records, total 5,000.
That shape fits parser recognition of 6,200, but does not prove a reachable session state or lossless repeated native recovery.
This proposal does not change the foundation contract to resolve that distinction.

For Express, the reachable session bound becomes `200 + 300 * 20 = 6,200`.
An accepted native seed with 2,600 pending records and 6,000 active singleton parties can require 8,600 pending records after recovery.
Keeping parser recognition at 6,200 would reject that recovered state.
The proposed 8,600 storage and aggregate bounds cover that native case explicitly.
They are new limits requiring approval, not consequences of the existing parser limit.

## Immutable orders and admission

The opt-in normalizer accepts new whole parties from one through 20.
Omitted options still select one passenger, private consent, and on-demand service.
Explicit null, empty, unknown, fractional, negative, or oversized values fail before admission.
Endpoint, pod, and service IDs retain their existing 64-byte bounds.
Diagnostic text retains its 1,024-byte bound.
The party size, consent, service choice, service ID, and request identity remain immutable.

An exact retry must include the effective contract and all immutable options.
The engine never splits an accepted party, removes active orders to fit a cap, or adds sharing consent.

Admission checks normalized class identity, whole-party fit, consent, service eligibility, and a class-compatible path.
A party of nine through 20 needs a certified Express pod even for private on-demand service.
Private orders cannot join another party or accept another party onboard.
Shared on-demand orders retain the existing party, stop, join, and occupied-pickup policies.
The eight-stop bound does not become 20.
Retain the existing bounded completed-history lifecycle in [`onboard_pickups.go`](../internal/sim/onboard_pickups.go).

At the Express 20-record bound, a pickup may replace the oldest completed record and its aligned boarding record.
It must refuse when no completed record exists.
Preserve completed journey accounting and the immutable options of every retained record.
This ordinary lifecycle rule does not permit encoding-time history eviction to fit a byte cap.

An Express-service order needs shared consent and an existing directed registry entry.
Its origin and destination must match that entry exactly.
The registry entry requires normalized Express class and an authored party limit from one through 20.
All pooled parties must name the same service ID and directed pair.
Their passenger sum must fit 20 seats.
An Express service has one passenger destination, not 20 independent stops.

No timetable, fill-before-departure delay, intermediate service stop, or fare policy enters this contract.

[`trip_admission.go`](../internal/sim/trip_admission.go) already checks passenger station roles, registry identity, compatible berths, and directed path reachability.
The extension must retain those checks at submission, dispatch, pickup, and restore.
Station, berth, every display route lane, and every native motion route lane must admit the effective class.
Omitted allowlists continue to admit legacy and compact only.
Parking roles do not become passenger endpoints.
Selecting a different berth cannot bypass an incompatible station, entry path, or lane.

Atomic registry replacement must still reject changes that invalidate accepted pending or onboard orders.

## Recovery and native API boundaries

Use one pending queue, not a separate recovery store.
Reject an Express-contract seed when pending exceeds 8,600, outstanding exceeds 8,600, or class-specific stored history exceeds its bound.
Do not truncate, deduplicate, or silently remove records to fit.
Duplicate request identities still fail the state contract.
A nonempty waiting route requires a unique existing target pod under `express-v1`.
At most one waiting route can bind each pod, so at most 300 pending routes can exist.

Reject duplicate route bindings and routes on unbound orders before native restore or save acceptance.
This is a new explicit opt-in check, not a claim that every current native route cache satisfies it.
Current release or reassignment paths can retain a cached route after clearing `PodID`.
The later opt-in implementation must clear that derived cache when it clears or changes the assignment.
It must qualify every assignment, release, capacity refusal, and reassignment transition.
Incoming contradictory records reject without removing accepted party facts.

Foundation native restore retains its current treatment of route bindings.
The session keeps its existing refusal when the pending queue reaches 200.
That refusal also applies while a restored backlog exceeds 200.
Native submission retains its current queue policy outside the opt-in contract.
Within `express-v1`, a submission that would exceed 8,600 outstanding records must refuse before creating an accepted request.

Logical recovery uses the existing stable request-ID ordering in [`state_logical.go`](../internal/sim/state_logical.go).
It preserves immutable options and original request timing.
It retains the existing already-boarded marker so recovery does not count waiting or boarding twice.
It clears physical pod, route, and deferral-check bindings using the existing recovery rules.
Completed history does not requeue.
Existing valid destination arrivals retain the existing accounted logical-completion behavior.

Repeated recovery must conserve the same identities, effective options, aggregate, and accounting.
The pending bound therefore remains 8,600 after any number of requeues.

Retain the current ordinary physical reconstruction in [`state_physical.go`](../internal/sim/state_physical.go).
Ordinary `ExportState` does not save speed or the exact future reservation frontier.
A physical restore resets ordinary speed to zero and reconstructs forward grants from route geometry.
Preserve position, current footprint, retained past ownership, riders, timing, and accounting when their physical proof is valid.
Classify the speed reset and forward-grant differences in restore evidence.
Do not claim identical speed or trajectory across that boundary.

The restored zero-speed stopping frontier must be owned continuously.
Every subsequent tick must satisfy the unchanged acceleration, braking, speed-limit, and large-separation rules.
Existing compact speed certificates keep their separate approved behavior.
No new ordinary speed or stopping-ownership certificate enters save 7.

Unsupported classes, changed immutable classes, bad consent, incompatible endpoints, or invalid resource certificates must reject before logical fallback can erase evidence.
Recoverable physical placement failures may use the logical tier only after the shared semantic checks pass.
Valid compact certificates retain their existing physical-only recovery rules.
Express or mixed large certificates never become compact or virtual links during recovery.
Disabled policy settings may validate and retain an existing approved physical certificate under current rules.
They must not enable new buffer, occupied-pickup, compact, or platoon admissions.

Proposed additive native API choices require review with this contract:

| API | Proposed compatibility rule |
| --- | --- |
| `OrderContract` | Empty value means foundation. The only new value is `express-v1`. Unknown values fail. |
| `NewFleetWithOrderContract` and prepared equivalent | Accept an explicit contract. Existing constructors keep foundation behavior. |
| `RestoreStateInput` and `PreparedRestoreInput` | Add `OrderContract`. It must match the saved state's effective contract. |
| Native `SavedState`, `Snapshot`, and recurring simulation metadata | Add `OrderContract` with JSON name `orderContract`, omitted for foundation. Native and wire markers must agree. |
| Contract-aware lookup, normalization, and admission helpers | Resolve profile and bounds for the explicit contract. Existing helpers retain their foundation meaning. |

The proposed constructor accepts the existing network and fleet arguments plus `OrderContract`.
The contract remains immutable for that simulation and its reset lifecycle.
Opt-in native constructors and restore must enforce the same bounded fleet, IDs, network, and registry as project validation.
Do not globally flip the foundation Express registry entry to physically supported.
After qualification, the opt-in lookup can return 20 seats, party bound 20, body length 10 m, and physical support.
The original lookup continues to describe the unsupported foundation Express profile.

Existing submission method signatures need not change because the simulation owns its explicit contract.

A new party of nine through 20 is valid only under `express-v1`.
Saved state has no historical party marker and no unknown consent.
Restore rejects a party above the bound of its contract and a consent other than `private` or `shared`.
Restore cannot create a shared Express party or infer consent.

## Physical contract and limits of the proof

The Express position denotes the body center under the opt-in contract.
A centered 10-by-2.5-meter rectangle has a half-diagonal of about 5.154 m.
The existing 6 m circle contains that rectangle at every orientation.
This is a simulation envelope, not an axle, steering, seating, or manufactured-vehicle certification.
No new physical number is proposed.

[`profile_geometry.go`](../internal/sim/profile_geometry.go) already treats Group and Express as large for shared geometry preparation.
Retain 20 m interaction clearance when either participant is large.
Prepare resources for the largest admitted class even when no Express pod starts in the fleet.
Include small-only incident lanes and neighboring berths that share affected resources.
Retain lane minimum 40 m and check every actual cell's offsets for at least 20 m.
Lane length divided by a nominal cell count is not a substitute for that check.

Retain origin, track, cell, junction, crossing, and berth ownership until the applicable release frontier passes the retained tail.
The common tail is at least 20 m.
Existing curved-path projection bounds may require larger tails.
Do not shorten those bounds to 20 m.
Reject nonfinite or nonpositive projection proofs and incomplete route geometry.
Check merges, crossings, plane transitions, banks, parking, and neighboring berths using their actual paths and resource identities.

There is no blanket certification for curves or authored junctions.

Motion retains acceleration and braking at 2 m/s², 60 Hz ticks, and authored lane speed limits.
Check current-lane speed on every tick, including entry into lower-speed lanes.
Lookahead must reach every lower limit that can affect stopping, across multiple cells and lanes.
No boundary speed clamp, position snap, or reduced authored speed substitutes for controlled braking.
Require stopping bounds and continuous ownership when speed reaches zero.
An independent body and resource oracle must check the sampled journeys and restore continuation.

It must not call the production separation helper as its proof.

Large virtual platoons, buffer platoons, and compact links remain prohibited in both participant orders.
This includes mixed Group/Express/small links.
Adjacent compact groups retain their protected compact arithmetic and certificates.
Test the retained 20 m influence of a large neighbor without changing those compact contracts.

## Versions and lossless wire encoding

The project marker is `orderContract: "express-v1"` in a version 1 project.
The decoder refuses project versions 2 through 5 and does not migrate them.
Save 7 requires that marker and `textEncoding: "order-text-base64-v1"`.
Stream hello 4 advertises both values and binds them to its source epoch and topology.
Express topology carries `orderContract` and a bounded `expressServices` array, copied from the project registry.
It retains project version, epoch, and revision binding.
The entire topology, including the registry, must fit the existing 10 MiB plus 4 KiB cap.

The producer must encode and preflight it before activating an Express session.
A failure rejects activation without dropping registry entries or raising the cap.
Consumers must validate each service ID, normalized Express class, directed pair, party limit, and compatible path.
A registry change requires a matching topology revision before consumers accept orders using that change.
Full frames and replacement deltas carry the text-encoding discriminator.
Reject unsupported, absent, duplicate, contradictory, or mixed discriminators for those versions.

Every payload must validate before the assembler changes its current state or acknowledges it.
Recovery and a full-frame replacement must retain the negotiated contract.
A project change that changes the contract closes the current stream and requires a new hello.

A project without the Express or coupling marker uses save 6 and stream hello 3.
These keep raw text meanings and their current limits.
Reject new markers, packed fields, new operating Express certificates, or larger semantic states in save 6 and hello 3.
Do not infer base64 from a string's appearance.
A raw old ID such as `YWJj` still means those four characters.
Do not downgrade Express state to an old version, remove riders, or reinterpret valid private orders during export.
Foundation projects and ordinary fixtures keep their encoded bytes, with project version 1.

Encode five free-text fields inside each waiting or rider order record: `from`, `to`, `podID`, `dispatchReason`, and `serviceID`.
Saved order records use their existing lowercase field names.
Preserve field omission and empty-string rules exactly.
Other identifiers, enum values, booleans, numbers, route arrays, and structural names keep their current JSON representations.

Saved boarding records retain the existing source-bound `[berthIndex, meters]` tuple encoding.
Stream boarding records retain their named native berth-ID and distance fields.
Do not pack the embedded project, topology, or arbitrary other strings.

Use strict RFC 4648 standard-alphabet base64 with canonical padding and no whitespace.
Decode to valid UTF-8 bytes, then apply the existing decoded-native byte bounds and identity checks.
Re-encoding must reproduce the input spelling exactly.
Packed strings must use literal ASCII in JSON without JSON escape sequences.
Reject malformed padding, trailing-bit alternatives, invalid UTF-8, oversized decoded text, and nulls.
Treat all five fields as packed for the negotiated version.
Do not accept a raw-text fallback.
Check bounded encoded lengths before allocation, at most 88 characters for 64 bytes and 1,368 for 1,024 bytes.

Base64 preserves control characters, quotes, backslashes, and all valid multibyte UTF-8 without normalization.
It does not alter consent or permit a previously invalid ID.

The packed wire representation is separate from semantic native snapshots.
Native callers still observe original UTF-8 strings and integer or floating-point values.
The 80 MiB file cap applies to the session persistence envelope, not arbitrary external marshaling of native `SavedState`.
Persisted Express state must use the reviewed adapter rather than a raw native JSON dump.
The adapter must decode pending groups, full vehicle records, and replacement rider groups before semantic validation.
Each replacement is self-contained and needs no dictionary or prior text references.

Public admission and route checks must use contract-aware normalized classes and the topology registry.
The current Group-only lane cache in [`stream_frame.go`](../internal/session/stream_frame.go) cannot certify Express routes.
The current on-demand-only check in [`stream_service.go`](../internal/session/stream_service.go) cannot certify an Express-service order.
Extend those consumers explicitly rather than treating their current acceptance as qualification.
Request identities, ticks, sequence strings, exponents, and boarding distances must roundtrip without conversion through JavaScript numbers.

For Express HTTP state, require `Accept: application/vnd.podsim.express-v1+json`.
Return an envelope with `orderContract`, `textEncoding`, `topology`, and `frame`.
The topology and frame must identify the same source epoch and project revision.
The frame uses compact indexed-route `StreamFrame`, whose current members are state and routes.
Current stream frames do not contain topology.
WebSocket consumers continue to fetch the separate topology and verify its source binding.

The opt-in HTTP envelope carries both topology and frame, not raw repeated `StateFrame.RouteLaneIDs`.
Its packed orders use the same full-stream adapter and decoded semantic validation.
Apply the 64 MiB raw stream bound to this opt-in HTTP envelope.
This avoids multiplying escaped lane IDs across a large recurring route payload.
An Express session returns 406 to an unqualified state reader instead of a foundation-shaped partial response.
Foundation HTTP state remains unchanged.

Express trip commands require `orderContract: "express-v1"` and a matching project and epoch.
Plain project and topology responses identify project version 1 and its contract.
Existing clients must reject their unsupported version rather than operate an incomplete fleet.
These endpoint, constructor, and version changes require explicit approval before implementation.

## Byte evidence and alternatives

The source baseline uses committed Group measurements in [`group-physics-qualification.json`](measurements/group-physics-qualification.json).
The exact fixture constructors are `testGroupWorstCaseSize` in [`group_state_test.go`](../internal/session/group_state_test.go) and `maximumStreamFrame` in [`stream_test.go`](../internal/session/stream_test.go).
`TestGroupStreamMaximumEncoding` in [`stream_group_test.go`](../internal/session/stream_group_test.go) supplies the Group full and replacement-delta modifications.
Those fixtures encode independent typed maxima, including a project member at its 10 MiB cap.
They do not represent a jointly reachable physical placement.

A scratch Go 1.27.1 encoder measured exact `json/v2` order and boarding records from the pinned native declarations.
It uses the real enum spellings and source field tags.
It preserves worst integer widths, completed flags, historical markers, and optional service-ID storage.
Its maximum order includes combinations that semantic admission rejects, so it overestimates normal Express records.

| Existing Group fixture | Measured raw bytes |
| --- | ---: |
| Historical save | 83,270,697 |
| Full stream | 62,095,518 |
| Replacement delta | 62,228,597 |

The bounds below use the earlier baseline of 83,270,696, 62,173,518, and 62,306,597 bytes.
The earlier stream baseline included legacy order markers on its 2,600 pending requests.
The save fixture now stores playback speed 60, one byte wider than 8.

| Record contribution | Measured bytes |
| --- | ---: |
| Old saved request, private party of eight | 7,524 |
| Old stream pending historical request | 7,570 |
| Old stream Group rider, shared singleton | 7,521 |
| Conservative new saved request, raw / packed | 7,971 / 2,011 |
| Conservative new stream request, raw / packed | 7,972 / 2,012 |
| Extra saved pending wrapper, without its request | 495 |
| Saved boarding tuple, index 4,999 and widest positive fixed decimal | 31 |
| Stream boarding object, raw 64-byte control-character berth ID | 442 |

The save upper bound starts with 83,270,696 bytes and removes 5,000 old request contributions.
It adds 14,600 new requests, 9,600 additional separators, and 6,000 extra pending wrappers.
It also adds 6,000 boarding tuples with separators and 64 bytes per pod for new optional boarding and distance members.
That per-pod allowance includes the longer Express class name.
The pending wrappers retain raw `DeferPodID` text and its worst escaping.
The tuple index allowance of 4,999 exceeds the current per-station berth index maximum of 199.

It overestimates the existing source-bound tuple.
Only 300 waiting routes can hold the old maximum assigned pickup path, which the baseline already includes.
The explicit unique pending-route binding rule prevents a native seed from multiplying those assigned routes.

Each stream bound removes 2,400 old Group rider contributions and 2,600 old pending contributions.
It adds 14,600 new requests, 9,600 separators, and 3,600 extra named boarding objects with separators.
It retains the original route, berth, checkpoint, stop, counter, and diagnostic allowances.
The embedded save project already includes its registry within the 10 MiB project cap.
The recurring stream frame does not duplicate that registry.
The separate topology response must also pass its existing bounds with all 300 service entries.

The save projection does not add the registry twice.
Each bound also includes an 8,192-byte analysis reserve for marker fields, class-name growth, and numeric-width differences.
That reserve is a conservative sizing allowance, not a new semantic header limit or approval for unspecified fields.

| Proposed independent shape: 8,600 pending plus 6,000 stored riders | Raw JSON upper bound | Packed upper bound | Existing cap |
| --- | ---: | ---: | ---: |
| Save | 165,226,288 | 78,210,288 | 83,886,080 |
| Full stream | 142,444,910 | 55,428,910 | 67,108,864 |
| Replacement delta | 142,577,989 | 55,561,989 | 67,108,864 |

The all-20-record shape also covers mixed small-class certificates conservatively.
A small-class pod retains at most eight records, freeing at least 24,132 packed saved-request bytes.
Measured widest saved platoon and compact objects occupy 600 and 2,525 bytes.
Both objects plus a 128-byte outer metadata allowance total 3,253 bytes, below the freed record budget.
Counting both together overestimates a valid compact head, which cannot also hold a predecessor link.
Stream link metadata is smaller than that combined allowance.

Actual mixed widest-shape assets still need the final qualification gate.

The aggregate outstanding rule prevents all 14,600 records from being active together.
Completed history still needs storage, so this analysis conservatively includes all 14,600 records.
The packed save margin is 5,675,792 bytes.
The packed replacement-delta margin is 11,546,875 bytes.

The encoder also measures a maximum-width native service entry with three escaped 64-byte IDs.
Its normalized class is `express`, and its party limit is 20.
The entry occupies 1,213 bytes, and a 300-entry array occupies 364,201 bytes.
Repeated widest entries overestimate valid unique service identities without changing their encoded lengths.

| Public identity and topology budget | Bytes |
| --- | ---: |
| Contract and text-encoding marker object | 68 |
| Marked hello, with conservative generated-identity and build allowances | 1,107 |
| Topology header with two null placeholders | 1,354 |
| HTTP envelope header with two null placeholders | 97 |
| Independent topology projection: 10 MiB core plus registry and header | 10,851,307 |
| Existing whole-topology cap, registry included | 10,489,856 |
| Combined qualified topology and packed HTTP frame bound | 65,918,855 |
| Combined HTTP margin below 64 MiB | 1,190,009 |

The hello allowance uses 100-byte escaped generated identity text and a 64-byte escaped build value.
Those are conservative producer allowances, not permission for longer consumer identities.
The topology projection subtracts eight placeholder bytes and adds the complete registry and 10 MiB core.
That core allowance uses the entire project byte cap for network, geography, and map fields.
Adding the registry separately overestimates a project whose registry already shares the 10 MiB cap.
The resulting independent topology projection exceeds the existing topology cap.

It therefore does not qualify every project that fits its own file cap.
Producer preflight and real widest-shape topology assets must establish the accepted set.
Do not assume a 10 MiB project proves canonical topology fit.

The combined HTTP bound uses the qualified whole-topology cap and the larger full-stream envelope bound.
It adds the 97-byte wrapper and removes its two four-byte null placeholders.
The full-stream bound already includes the 8,192-byte reserve for its discriminators and source identity.
It deliberately overestimates the embedded frame by retaining full-envelope overhead.
WebSocket full and delta frames do not duplicate the registry.
Their registry bytes belong to the separately fetched, source-bound topology.

Compression ratios do not establish either margin.
The save must fit both its uncompressed and compressed 80 MiB bounds.
The stream must fit its raw 64 MiB and binary 65 MiB bounds.
Later qualification must measure actual gzip output, including incompressible payloads.

A 1,024-byte control-character string occupies 6,146 JSON bytes including quotes, versus 1,370 with base64.
Quotes and backslashes occupy 2,050 raw JSON bytes at that same decoded byte count.
Four-byte UTF-8 characters and U+2028 also roundtrip in the measured encoder.
Use byte limits, not character counts.
The float `0.0000010000000000000002` occupies 24 bytes and drives the boarding allowance.
The helper also checks fixed/exponent boundaries, subnormal values, and maximum finite floats.

Stream `completed=false` costs one more byte than `true`, which the stream record maximum includes.
The existing baseline retains wider signed counters and other scalar allowances.
Do not replace these checks with six bytes per character or short decimal examples.

The evidence cache contains `byte-evidence.json`, the encoder source, run metadata, source hashes, and an arithmetic receipt.
Its location is `~/.cache/agents/podsim/forecast-contract-batch-20261003/express/`.
These results justify a contract choice, not an implementation size certificate.
Before enablement, encode and decode real widest-shape save, full-frame, and replacement-delta assets using the final adapters.
Compare actual sizes against these projections and both unchanged byte caps.

| Alternative | Benefit | Cost and decision |
| --- | --- | --- |
| Five packed text fields and explicit aggregate, recommended | Lossless bounded encoding under existing byte caps | Requires new versions and adapters. Ordinary ASCII text can grow. |
| Keep canonical raw text and increase caps | Less adapter work | These bounds need at least 165,226,288 save bytes and 142,577,989 stream bytes, plus reviewed margins and consumer memory limits. |
| Separate recovery store or text dictionary | Can reduce repeated text or isolate backlog | Adds reference lifetime, ordering, idempotence, admission, and byte rules. No such contract is proposed. |
| Retain Express metadata only | No new operating or encoding contract | Defers physical 20-person operation. |

Do not silently raise a cap if a later asset fails.
Return the exact failure, encoded size, and proposed revised contract for review.

## Qualification required before opt-in profile support

Implement qualification before enabling the Express-specific registry entry.
Use native simulation, save adapters, public stream consumers, and the browser decoder.
Parser acceptance alone cannot pass a physical or operating gate.

| Gate | Required cases and evidence |
| --- | --- |
| Orders and registry | Sizes 1, 8, 9, 20, and 21. Twenty singleton parties, one party of 20, mixed sizes, private exclusion, directed service identity, service replacement, 300 entries, and immutable retries. |
| Storage and recovery | 300 Express pods, manual 200, session total 6,200, native total 8,600, aggregate overflow, completed-history rollover, aligned 20 boardings, repeated ID-ordered logical requeue, and conservation without double accounting. Qualify cached waiting-route cleanup on every assignment or release path. |
| Route and station eligibility | Omitted and explicit allowlists, wrong normalized class, parking endpoints, incompatible source and destination berths, alternate berth selection, every display and motion lane, unreachable paths, and malformed native masks. |
| Physical geometry | Centered 10 m body within the 6 m envelope, 40 m lane boundary, every 20 m actual cell boundary, positive curve projection, short or nonfinite paths, crossings, merges, banks, parking, and all occupied planes. |
| Motion and ownership | Each tick checks current lane speed, acceleration, braking, zero-speed stopping frontier, continuous ownership, at least 20 m release tails, lower limits across several cells and lanes, and no snap. |
| Independent safety oracle | Following and crossing bodies, retained resource identities, both class orders, Express/Group mixtures, small neighbors, and compact groups. Oracle failures must identify tick, route location, bodies, and resource owner. |
| Physical restore | Idle, departing, boarding, braking, stopped, traveling, unloading, and continuing. Preserve Group/Express position, current and retained past ownership, routes, riders, timing, distance, and policy settings. Classify speed reset and reconstructed forward grants. Check safe mixed continuation. |
| Rejection and disabled policies | Unsupported foundation Express, mismatched class or contract, large virtual and buffer links, compact links, bad consent, invalid physical certificates, logical-only misuse, and no new admissions under disabled policies. |
| Save bytes and shape | Actual save-7 widest-shape files, escaped and multibyte text, both boarding forms, optional history, exponents, malformed base64, UTF-8 and decoded limits, exact gzip caps, parser prescan, and atomic rejection. |
| Stream and public consumers | Full and replacement delta assets, pending replacement groups, sequence and epoch recovery, topology and registry binding, normalized class lane caches, raw and gzip caps, Go remote consumer, compact HTTP negotiation, browser/WASM roundtrip, and fail-closed old readers. |
| Resource and consumer cost | Measure encoding and decode time, actual raw and gzip sizes, peak resident memory, previous-frame retention during replacement, and browser responsiveness with widest-shape assets. Do not infer decoded-memory bounds from wire size. |
| Foundation parity | Old-only trajectories, Group journeys, compact protected arithmetic, ordinary save and stream bytes, default settings, and unsupported Express behavior under foundation APIs. |
| Independent mutations | Compiled actual-caller mutations of contract selection, whole-party capacity, aggregate bound, allowlist/path validation, ownership release, current speed, large-link rejection, and packed discriminator checks. A compile failure is not a kill. |

Run the relevant package suites, scoped races, vet, lint, and fresh Go diagnostics for the later implementation.
Report assertion failures separately from parser rejection, byte overflow, watchdog expiration, and unavailable external gates.
Do not claim a completed full-repository race from scoped checks.
Keep source pins and the mutation table with the resulting qualification evidence.

## Browser and offline authoring

Keep the browser file wrapper `format: "podsim", version: 1`.
Its scenario member identifies project version 1 and the explicit contract.
An authoring roundtrip must retain class, allowlists, service entries, consent, party size, and the contract without rewriting omitted foundation fields.
Show Express operation as unavailable until the opt-in capability and applicable qualification exist.
The browser must use the matching native/WASM decoder for packed state and reject unknown capabilities.
Do not let a cached old worker accept partial topology or lose rider records.

[`web/offline.js`](../web/offline.js) still limits its separate car itinerary contract to parties of eight.
This proposal does not change car-plan, car-report, energy-report, comparison, or CLI schemas.
Offline project authoring may preserve Express metadata and explicitly select the Express marker after approval.
Do not automatically upgrade old projects or saves to the opt-in contract.
Offline native validation must report unsupported physical operation until qualification enables the opt-in profile.
Parkride and car checkpoint version 1 remain foundation-only and must reject an Express-contract project.

Their exact continuation uses replay and does not change the physical restore behavior proposed here.
A future 20-person car itinerary or report extension needs its own explicit approval.

Review must approve the version and additive API plan, order and storage bounds, and packed text contract together.
Implementation, runtime enablement, default adoption, and deployment remain separate decisions.
