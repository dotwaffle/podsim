# Service contract proposals

Status: approved October 2, 2026.
Amended through November 16, 2026.
Git history holds the dated amendment notes and the earlier text.
The compact station queue section went with station buffers and compact station queues on October 7, 2026.
The opt-in Compact station added in October 2026 (maneuver groups, `internal/sim/maneuver_*.go` and `internal/sim/station_maneuvers.go`) is a different feature, and this removal does not cover it.
Implementation landed: order consent, vehicle classes, Express, and the Go editor helpers are in the code.
The user approved roadmap items 1 through 10 and continued local work on October 2, 2026.
The standing grant requires approval of concrete contract changes before implementation.
Browser-facing JavaScript, existing defaults, and ordinary safety limits stay in force.

## Order consent and vehicle compatibility

Approve these data and compatibility rules as one foundation:

- A trip accepts `partySize` from 1 through 8, or through 20 with order contract `express-v1`, `sharingConsent` as `private` or `shared`, and a service choice.
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
  Shared admission requires explicit consent from every active party and enough passenger capacity.
  Retain the existing on-demand party limit, stop limits, and 1.5 detour limit.
- Separate immutable vehicle class from the current service.
  Use `legacy`, `compact`, `group`, and `express` class IDs.
  Omitted class means legacy.
  Legacy, Compact, and Group have 8, 4, and 8 seats.
  Express has 12 seats and 8 standee places.
  Legacy preserves the current four-meter point/body model.
  Its logical seats are not a physical capacity certification.
  Legacy keeps size-one party admission, including the existing explicitly consented singleton sharing experiment.
  Larger new parties need an explicitly authored class that fits them.
  Compact uses the current four-meter simulation profile and current acceleration and speed rules.
  Clearance layer A (`internal/sim/retention.go`) reduces the clearance between two small pods, legacy or compact, on a qualifying lane.
  A qualifying lane has no control, is straight, and has a speed limit below about 3.7 meters per second.
  Its window of lanes has no lane that admits a large class and no geometry tail, one incoming and one outgoing lane at each inner node, a total turn of at most 90 degrees, and a reduced retention below 12 meters.
  The window runs from the upstream braking reach to twice the clearance past the end of the lane.
  There, the follower keeps `4 + min(8, 1 + 2u)` meters behind the pod ahead, where `u` is the braking distance of the follower at its speed.
  Two small pods on a certified lateral pair of depot lanes (`internal/sim/depot_lateral.go`) keep `W(v) = 3.1 + v²/2` meters, where `v` is the higher lane limit of the pair.
  That rule assumes guideway-bound pods: a pod stays within 0.25 meters of its lane and follows its heading.
  When the network sets `lateralMerges`, two small pods on a lateral merge pair keep 3.1 meters, under the same guideway-bound assumption.
  A lateral merge pair is two small lanes that both end at one node or both start at it, as the [lateral merge zones](../design/lateral-merge-zones.md) design defines.
  The junction of the node orders the two pods, so this rule has no speed term.
  When the network sets `shapeClearance`, the [shape clearance](../design/shape-clearance.md) rule applies to pods of each class.
  The certified pairs and the skip rules of the maneuver groups come first and do not change.
  The depot lateral pairs keep `W(v)`, and clearance layer A does not change.
  A following pair is two pods on one lane, or on two lanes where one ends at the node where the other starts.
  A following pair keeps its current rule: layer A on a qualifying lane, and otherwise 12 meters, or 20 meters with a large pod.
  A merge or split pair is two lanes that meet clauses 1, 2, 4, and 5 of the [lateral merge zones](../design/lateral-merge-zones.md) design.
  The two lanes both end at one node or both start at it, neither lane is a loop, and the two lanes do not share both endpoints.
  Neither lane is in a maneuver group or a depot, and the body range does not reach the far end of either lane.
  A lane that admits a large class or has a geometry tail can be part of a merge or split pair.
  The junction of the node orders the two pods of a merge or split pair.
  Its extent is the body range with the union body, and the node range of the last or first 12 meters of the lane.
  A position on a lane has the plane of the lane when neither lane of a pair admits a large class.
  Otherwise it has the planes of its envelope reach, as the [group physics design](../design/group-physics-design.md) defines it under the field.
  Two positions, one on each of two paths, are separated when each plane of one and each plane of the other are different planes.
  At separated positions, the plane exemption holds as before.
  Shape certification sweeps the grown body of each of two lanes that share no node over the lane and over the join turns at both of its ends.
  It certifies the two lanes as a shape pair when the grown bodies are disjoint at each pair of positions that are not separated.
  Two lanes on one plane have no separated positions, so their two swept regions must be disjoint.
  When the grown bodies meet at such a pair of positions, or the sweep does not resolve, the two lanes are not a shape pair.
  The network audit then requires 12 meters, or 20 meters when either lane admits a large class, at each pair of positions that are not separated.
  Shape certification is for two lanes only, and a pair with a berth keeps its current rule.
  The network audit checks each pair of such a network, small pairs included, also without banks, large classes, maneuver groups, or depots.
  No resource orders the two pods of a shape pair.
  Two pods on a merge, split, or shape pair keep the side-by-side distance of their classes: 3.1 meters for two small pods, 3.55 meters for a small pod and a large pod, and 4.0 meters for two large pods.
  The grown body of a lane is the union of the bodies of the classes that the lane admits.
  When the fleet has a large pod, the oracle keeps an envelope of planes for each pod, from the envelope reach at the published lane position, with the small-pod rules of the [group physics design](../design/group-physics-design.md).
  A pair with at least one large pod keeps the plane exemption only when each cross-pair of the two envelopes is on different planes.
  A pair of two small pods keeps the plane exemption when the locations of the two pods are on different planes, as before.
  Under rule A1 of the [shape clearance](../design/shape-clearance.md) design, the envelope holds each lane under the body, for the bodies and within the limits that the group physics design states.
  The envelope can drop a plane of the largest front bound of the route that is not under the body.
  The qualification of the field, for pods of each class, runs the body-and-ownership oracle of the design at each tick of a merge test and of a restored continuation.
  In such a network, `lateralMerges` has no further effect.
  Merge, split, and shape pairs assume guideway-bound pods at line speed: the reference of a pod stays within 0.25 meters of its lane, its heading follows the lane, and a fault stops it on its lane.
  Their distance has no speed term.
  Every other pair of small pods keeps 12 meters, and every other pair with a large pod keeps 20 meters.
  Debris clearance stays 12 meters, and debris on a depot lane also blocks the cells of the neighbor depot lanes within 12 meters.
  Debris on a lane of a lateral merge pair also blocks the cells of the partner lane within 12 meters.
  When the network sets `shapeClearance`, debris on a lane of a merge, split, or shape pair also blocks the cells of the partner lane within 12 meters.
  Amended 2026-11-15, by maintainer decision: the lateral merge pair and its debris rule, behind the `lateralMerges` network field.
  Amended 2026-11-16, by maintainer decision: the following, merge, split, and shape pairs of each class, their guideway-bound assumption, their debris rule, and the envelope planes of the audit, behind the `shapeClearance` network field.
  Amended 2026-11-16, by maintainer decision: the local front and the small-pod rules of the oracle envelope, and rule A1, behind the `shapeClearance` network field.
  Amended 2026-11-16, by maintainer decision: the oracle envelope from the compiled envelope reach, in place of the local front, behind the `shapeClearance` network field.
  Group has an approved physical profile (`internal/sim/vehicle_class.go`).
  Express needs an approved physical profile, through the `express-v1` contract, before placement, simulation start, or restore.
  Do not reuse ordinary legacy links for a larger body without a separately qualified class certificate.
- Stations, berths, and lanes have optional `vehicleClasses` allowlists.
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
- Use project version 1, saved-state version 12, and stream hello version 10.
  A project permits banks and service metadata independently, by their presence.
  Browser file wrapper version 1 stays unchanged.
  The decoder refuses project versions 2 through 5 and does not migrate them.
  The saved state can contain a project with new effective request state.
- The express rider array bound is 20.
  The restore queue bound is 8,200 orders for a plain state and 17,000 orders for an Express state.
  The fleet limit is 1,000 pods for a plain project and 600 pods for an Express project.
  Keep on-demand at 8 parties, stops at 8, manual admission at 200, and the current history caps.
  Bound the express registry at 300 services.
  The [service byte limits](service-byte-contract-proposal.md) set the byte caps, with a save cap of 128 MiB.
  `TestPlainStateFileWorstCaseSize`, `TestPlainStreamMaximumEncoding`, `TestExpressStateFileWorstCaseSize`, and `TestExpressStreamMaximumEncoding` prove that worst-case saves and frames fit those caps.
- Each saved and streamed order has explicit `private` or `shared` consent.
  Restore and new trip commands reject all other consent values.
  The server does not migrate saves of an earlier version.
  Never invent consent, split a party, or truncate passenger counts.
  Pending or requeued parties without a suitable certified class remain unassigned.
  This includes sizes 2 through 8 in a legacy-only fleet.
  Preserve their identity, timing, rail binding, and conservation counts.
  Report why assignment cannot proceed.
  Do not drop such orders or reject a whole otherwise valid save silently.

Observable and saved requests add `sharingConsent`, `service`, and `serviceID` with the effective values above.
Observable pods and placements add `class`.
Topology preserves the project version and class allowlists.
New express records use `id`, `from`, `to`, `class`, and `partyLimit`.
Keep completed rider display history bounded separately from active riders.
No continuously served pod may grow an unbounded request array.
Onboard pickup requires per-rider boarding berth and distance baseline in the saved state before multiple origins become valid.
It occurs only at a compatible passenger berth while stationary, after eligible alighting.

The detailed read-only audit lists all consumers, compatibility paths, migration exceptions, and required validation.
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
- `stationLayout` returns selected station/bank dimensions and separate unavailable reasons for each control.
  It tolerates incomplete drafts and does not require a startable simulation.
  Browser rendering rejects stale selection or geometry replies.
- `measurements` returns the ruler overlay of the draft for one selection and map scale.
  It tolerates a draft that breaks the project limits and does not require a startable simulation.
  The request has `project` and `measure`, and `measure` holds `selection` and `scale`.
  The map shows at most 200 rulers, and the summary text covers all measurements.
- `stationObservations` counts the station facts that one HTTP state reply supplies.
  The request has only `state`, a live state object, and no project.
  A missing or malformed source makes its counts null, not zero.
  Only a JSON syntax error is an error.
  Its request limit is the HTTP state limit plus 64 KiB, not the project request limit.
- `observationScope` returns the text that tells how a station observation relates to the draft.
  The request has only `scope`, with `identity`, `base`, and `unchanged`.

The private operation shapes are:

| Operation | Request members beyond `op` | Successful result |
| --- | --- | --- |
| `backgroundMetadata` | `metadata` with optional `placement`, optional `asset`, and bounded `urlFacts` | `valid: true` and owned effective `metadata` |
| `stationLayout` | Raw `project` and `layout` with `stationID` and optional `bankID` | `layout` with field-specific value and unavailable reason |
| `measurements` | Raw `project` and `measure` with `selection` and `scale` | `measurements` with `items`, `fontSize`, `radius`, and `summary` |
| `stationObservations` | `state`, a live HTTP state object | `observations` with `status` of `snapshot` or `unavailable`, `stations`, and the scope text |
| `observationScope` | `scope` with `identity`, `base`, and `unchanged` | `scope` text |

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
Projection and canvas resampling extraction have lower priority.
