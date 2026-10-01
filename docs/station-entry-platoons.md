# Fixed station-entry platoons

Station buffers can form a local virtual platoon inside one eligible entry lane.
Both experimental station buffers and virtual platoons must be enabled.
Station buffers remain off by default.
This implementation does not reduce stopped clearance or establish a capacity benefit.

## Formation and discharge

Both pods must physically occupy the same entry lane and target the same station.
Each pod retains explicit buffer membership and a berthless route that ends on that lane.
The follower must satisfy the existing slow-queue, nearest-predecessor, member-count, speed, turn, and stopping-separation checks.
A local platoon cannot join an upstream mainline platoon.

The certificate has a fixed stopping frontier from `bufferPlan`.
Only complete ordinary track cells inside its interior can be shared.
Upstream merge conflicts, downstream branch conflicts, berth access, and departure resources remain exclusive.
Each shared cell must release before the frontier, with the existing clearance and drain margin.
The endpoint cannot grow onto a berth branch.
Curved lanes retain their conservative turn and clearance.
Unequal remaining-route or station speed limits prevent formation.

Before formation, every member needs at least one structurally valid exclusive berth path.
The head selects a berth only when the complete suffix and berth grant succeeds.
Appending that suffix must preserve every prefix cell, resource, release point, and reservation.
A denied grant preserves the route and ownership graph.

The discharged head retains its follower until shared ownership drains.
Its resources transfer to the first follower that still holds them.
The affected link becomes draining and permits no new coupled grants.
The next head can select its own exclusive berth suffix after its predecessor relation ends.
Disabling buffers or virtual platoons also keeps existing ownership dependencies until they drain.
A full buffer retains ordinary upstream waiting.

## Saved state version 4

Version 4 adds two optional fields to a saved platoon link:

| Field | Meaning |
| --- | --- |
| `kind` | Empty or omitted for an existing complete-lane certificate. `buffer` selects a fixed entry certificate. |
| `terminalCell` | Integer cell index on the certified entry lane. Its downstream end is the fixed stopping frontier. |

A buffer certificate requires `lanes:1` and a present, nonnegative `terminalCell`.
The cell must equal the validated buffer frontier for the saved network and route.
It is not the last shareable cell.
A complete-lane certificate cannot contain `terminalCell`.
Unknown kinds, null fields, wrong types, invalid indexes, cycles, duplicate followers, and mixed certificate kinds are invalid.
Versions 2 and 3 reject any occurrence of either new field, including empty or null values.
Their existing member sets and restore behavior remain unchanged.

Save trimming retains the certified entry lane, original turn, and terminal cell until the link drains.
A follower must retain buffer membership and physically occupy its certified entry.
A discharged predecessor may have cleared membership only with a valid exclusive berth suffix and a draining affected link.
Restore validates both member positions and their lane and route distances within the existing restore tolerance.
It rebuilds the certificate and required ancestor holdings without choosing a berth or enabling new admissions.
The session applies project settings after physical restoration.

Invalid buffer certificates fail restoration without a logical fallback or partial member demotion.
An explicit logical recovery first validates those certificates physically, then requeues the orders under the existing logical recovery rules.
It does not retain their physical links.
This rule prevents logical recovery from accepting malformed version 4 certificates.

The writer uses version 4 while a fixed certificate or its draining ownership dependency remains.
After those links drain, the existing buffer rules select version 3 or version 2.
The project, command, and WebSocket formats do not change.
Keep a copy of the state file before using an older server.

## Qualification

Deterministic tests cover two to four pods, curved entries, unequal speeds, blocked berth departures, different berth choices, and failed suffix rollback.
Transition tests restore every saved field, check required ancestor holdings, and observe ownership transfer.
Session tests cover gzip JSON, native restoration, disabled policies, and version downgrade after drain.
The existing version 2 golden member list remains unchanged.

Matched Acton, LondonCentral, and LondonFull service trials are still required before adoption.
Report moving admission, spillback, departure progress, throughput, individual waits, tails, and unfinished requests.
Keep the feature experimental if it has no measured benefit or makes departure progress worse.
See the [original design](station-entry-platoons-proposal.md) and [buffer membership contract](station-buffer-state-proposal.md).
