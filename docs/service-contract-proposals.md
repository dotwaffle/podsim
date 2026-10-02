# Service contract proposals

Status: approved October 2, 2026.
Runtime implementation and qualification are in progress.
The user approved roadmap items 1 through 10 and continued local work on October 2, 2026.
The standing grant requires approval of concrete contract changes before implementation.
Browser-facing JavaScript, existing defaults, and ordinary safety limits stay in force.

## Order consent and vehicle compatibility

Approve these data and compatibility rules as one foundation:

- A trip accepts `partySize` from 1 through 8, `sharingConsent` as `private` or `shared`, and a service choice.
  Use `service` values `on-demand` or `express`.
  Express requires a known `serviceID`.
  On-demand forbids `serviceID`.
  Reject explicit null, empty enums, wrong types, unknown values, and contradictory service fields.
  Omitted values mean one passenger, private, on-demand.
  Store effective values on the request through assignment, riding, and restore.
  They are immutable after acceptance.
  Preserve exact-command retry behavior.
- A party remains together.
  A private active party excludes every other active party from its pod.
  Shared admission requires explicit consent from every active party and enough passenger seats.
  Retain the existing on-demand party limit, stop limits, and 1.5 detour limit.
- Separate immutable vehicle class from the current service.
  Use `legacy`, `compact`, `group`, and `express` class IDs.
  Omitted class means legacy.
  Proposed logical seats are 8, 4, 8, and 20 respectively.
  Legacy preserves the current four-meter point/body model.
  Its logical seats are not a physical capacity certification.
  Legacy keeps size-one party admission, including the existing explicitly consented singleton sharing experiment.
  Larger new parties need an explicitly authored class that fits them.
  Compact uses the current four-meter simulation profile, ordinary 12-meter clearance, and current acceleration and speed rules.
  Group and express need an approved physical profile before placement, simulation start, or restore.
  Do not reuse ordinary legacy links for a larger body without a separately qualified class certificate.
- Stations, berths, and lanes have optional `VehicleClasses` allowlists.
  Omission permits legacy and compact.
  Explicit lists contain 1 through 4 distinct known classes.
  Every placement, stop, passenger leg, empty move, reroute, diversion, swap, and restore must satisfy compatibility.
  A station name or crowd size never grants access.
- An explicit express service names a directed hub pair, express class, and party limit through 20.
  Both hubs require compatible berths and paths.
  Express orders require shared consent.
  Do not wait to fill seats or invent a departure interval.
  A large pod can separately serve one private on-demand group when all compatibility checks pass.
  Fare calculation and a fixed timetable require later contracts.
- Use project version 3, save version 6, and stream hello version 3.
  Project 3 permits banks and service metadata independently.
  Browser file wrapper version 1 stays unchanged.
  Old versions reject new members instead of ignoring them.
  Save 6 can contain an old project with new effective request state.
- Raise only the new express rider array bound to 20 and save-6 restore queue bound to 6,200.
  Keep on-demand at 8 parties, stops at 8, manual admission at 200, fleet at 300, and current byte and history caps.
  Bound the express registry at 300 services.
  Prove worst-case saves and frames fit existing byte limits before landing.
- Legacy pending orders become private.
  Preserve validated historical onboard shared parties as a closed cohort with unknown recorded consent.
  Add no riders or pickup stops to that cohort.
  A logical requeue makes its parties private.
  Persist the closed-cohort marker in save 6 so a second restart retains the same restriction.
  New trip commands cannot create legacy markers or unknown consent.
  Never invent historical consent, split a party, or truncate passenger counts.
  Legacy pending or requeued parties without a suitable certified class remain private and unassigned.
  This includes sizes 2 through 8 in a legacy-only fleet, and historical sizes above 8.
  Preserve their identity, timing, rail binding, and conservation counts.
  Report why assignment cannot proceed.
  A persisted legacy-size marker permits this historical record, not a new oversized command.
  Do not drop such orders or reject a whole otherwise valid save silently.

Observable and saved requests add `SharingConsent`, `Service`, and `ServiceID` with the effective values above.
Historical onboard riders alone may use `legacy-unknown` consent with a validated closed cohort.
Saved migration markers are `LegacyCohort` on the vehicle and `LegacyPartySize` on the historical request.
A closed cohort retains its recorded route and stops and cannot admit another party.
Observable pods and placements add `Class`.
Topology preserves the project version and class allowlists.
New express records use `ID`, `From`, `To`, `Class`, and `PartyLimit`.
Keep completed rider display history bounded separately from active riders.
No continuously served pod may grow an unbounded request array.
Onboard pickup requires per-rider boarding berth and distance baseline in save 6 before multiple origins become valid.
It occurs only at a compatible passenger berth while stationary, after eligible alighting.

The detailed read-only audit lists all consumers, compatibility paths, migration exceptions, and required validation: `~/.cache/agents/podsim/roadmap-service-20261002/consent-vehicle-contract-audit.md`.
Approval of this foundation does not approve arbitrary large-body dimensions or weaker physical separation.

## Go editor helpers

Approve these private worker extensions, with current acceptance rules preserved:

- `backgroundMetadata` takes bounded placement, frame state, frame, and license facts.
  It returns an owned metadata verdict before import or stored-background publication.
  It works without a valid synchronized simulation project.
  Go owns character bounds, unknown-member checks, timestamp pattern, placement, frame, and state relationships.
  Browser URL parsing supplies protocol facts so this move does not replace current URL acceptance with a stricter Go parser.
  Go retains original attribution text.
  Browser link construction still checks HTTPS.
- `importCompatibility` repairs only historical missing berth placements and the old market demand form before raw Go validation.
  It does not apply full defaults before validation.
  Preserve JavaScript truthiness where the old repair used it, null/member distinctions, and import rejection behavior.
- `stationLayout` returns selected station/bank dimensions and separate unavailable reasons for each control.
  It tolerates incomplete drafts and does not require a startable simulation.
  Browser rendering rejects stale selection or geometry replies.

The private operation shapes are:

| Operation | Request members beyond `op` | Successful result |
| --- | --- | --- |
| `backgroundMetadata` | `metadata` with optional `placement`, optional `asset`, and bounded `urlFacts` | `valid: true` and owned effective `metadata` |
| `importCompatibility` | Raw `project` object | `change.patch` with only changed `fleet` or `demand` branches |
| `stationLayout` | Raw `project` and `layout` with `stationID` and optional `bankID` | `layout` with field-specific value and unavailable reason |

Placement contains `x`, `y`, `width`, `height`, and `opacity`.
Asset contains `frameState`, `frame`, and `license`.
Missing asset means `none`, null frame, and null license.
Explicit null asset is invalid.
Missing optional frame and license members retain the current null defaults.
License members and code-point bounds stay unchanged: source 200, attribution 500, license 64, and each URL 2,048.
Retrieved stays at 40, method at 10,000, and notice at 2,000.
`urlFacts` is an object keyed only by `licenseURL` and `copyrightURL`.
Each supplied fact is exactly `{text: string, https: boolean}`.
A nonempty license URL requires its fact.
The fact text must equal that URL.
An empty URL needs no fact.
A supplied empty-URL fact must contain the same empty text.
Null or absent license permits no URL facts.
Unknown fact keys or members are errors.
For each nonempty URL, the boolean supplies the browser parser's HTTPS verdict.
Go checks the fact's shape and matching text before using the verdict.
It does not claim to implement browser URL parsing.
Metadata has a 256 KiB bound within the existing request limit.
No image bytes enter these operations.

Layout returns pitch, spacing, setback, approach length, and departure length independently.
Each field has a numeric value or null, plus an empty or explanatory unavailable reason.
The result is `{layout: {pitch, spacing, setback, approachLength, departureLength}}`.
Each named field is exactly `{value: number|null, reason: string}`.
Use an empty reason for an available value and an explanatory reason for null.
A missing bank ID requests whole-station layout inspection.
A banked station has unavailable approach/departure values until one bank is selected.
An unbanked station retains its current access-length behavior.
No summary changes the selected bank or proposes an edit.
An explicit unknown station or bank produces the existing JSON `error` result.
Metadata bypasses project synchronization.
Compatibility and layout operate on raw draft objects before typed project validation.
Each operation rejects unrelated parameters and unknown members before publication.
A rejection changes no synchronized project, revision, history, or image pins.

These operations use the existing private worker byte bound and strict request/response validation.
They change no public project, save, stream, or browser document version.
Keep file/envelope parsing, exports, bytes, decoding, DOM/SVG, storage, fetch, and worker lifecycle in JavaScript.
Exports remain available after worker failure.
Use native tests, real WASM calls, and browser import/restore/history tests to verify current acceptance and call order.
The detailed audit is `~/.cache/agents/podsim/roadmap-service-20261002/go-helper-audit.md`.
Projection and canvas resampling extraction have lower priority.

## Compact station queue

Approve an opt-in `compact-v1` profile with these limits:

- Straight fixed-entry station buffers, current four-meter profiles, and at most four linked pods only.
  Both speeds stay at or below 2.5 m/s, or 9 km/h.
  Existing remaining-route speed checks apply.
  Curved entries, larger bodies, mixed physical profiles, and unrelated pairs receive no exception.
- Standstill reference spacing is 6.01 meters, giving a 2.01-meter free gap between four-meter bodies.
  Moving spacing retains the 0.5-second reaction allowance and positive relative braking allowance: `gap >= 6.01 + 0.5*vf + max(0, (vf*vf - vl*vl)/4)`.
  Equal speeds of 9 km/h require at least 7.26 meters.
  This is not a promise of six-meter moving gaps.
- Retain the existing 2 m/s² acceleration/braking bound and owned-track stopping rules.
  A new controller must prove the planned next-state envelope without instant stops or stronger braking.
  Unsafe formation or extension fails before it changes membership.
- Ordinary roads, junctions, static station geometry, resource tails, and unrelated same-plane pairs retain the 12-meter rule.
  Shared admission remains inside certified plain entry cells.
  Conflict and endpoint resources stay exclusive.
- Reserve recovery room of `6*(members-1)` meters inside the existing entry frontier, at most 18 meters.
  Keep certificates and speed caps while recovering, including after feature disable.
  Recover every pair to stopped ordinary 12.01-meter spacing before suffix commitment or rerouting.
- Project 3 adds `stationQueueSpacing`, either `ordinary` or `compact-v1`, default ordinary.
  Compact requires station buffers and a valid platoon limit.
  It never changes lane speeds automatically.
  Save 6 uses distinct link kind `compact-buffer-v1` with phase `compact` or `recovering`.
  The kind fixes the numeric profile.
  Saved arbitrary clearance or braking values are invalid.
  Old schema versions reject new policy or certificate presence.
- The safety oracle grants exceptions only to validated direct neighbors inside the certified region.
  Nonadjacent members and outside traffic retain 12 meters.
  Browser flags cannot grant an exception.
  Physical restore validates the whole group, recovery room, geometry, and outside separation before placement.
  Invalid compact states fail physically, including LogicalOnly requests.
  They cannot silently requeue in the same epoch.

First qualify discrete braking, real blockers, outside traffic, disable/recovery, full discharge, and physical restore.
Measure stopped span separately from discharge headway and passenger completion.
A successful storage screen does not waive the existing service adoption gates.
The detailed audit is `~/.cache/agents/podsim/roadmap-service-20261002/slow-queue-contract-audit.md`.
