# Compact-v1 retained physical state

Status: approved by the user on October 2, 2026.
Numeric compact-v1 approval remains unchanged.
This proposal adds bounded saved-state fields needed to preserve actual recovery through a cold restore.
It changes no project, save, or stream version.
Older versions reject these fields and certificate kinds.

## Saved representation

Add `SavedPod.CompactQueue *SavedCompactQueue` with JSON member `compactQueue`, omitted when nil.
Only the head stores this certificate.
Followers use the existing `SavedPlatoonLink` with `kind: "compact-buffer-v1"`.
Their leader IDs define exactly the head certificate's member order.
The head has no predecessor link.
An anticipatory unlinked head can store a one-member certificate.

`SavedCompactQueue` contains these exact members:

| Go field | JSON member | Type | Bound and meaning |
| --- | --- | --- | --- |
| Kind | kind | string | Exactly `compact-buffer-v1` |
| Phase | phase | string | Exactly `compact` or `recovering` |
| Lane | lane | string | Existing fixed-entry lane ID, within existing identifier bounds |
| Members | members | []string | One through four distinct pod IDs, head first |
| Start | start | float64 | Finite lane-local interior boundary derived from the entry plan |
| Frontier | frontier | float64 | Finite lane-local plain-entry frontier derived from the entry plan |
| StopCells | stopCells | []int | One relative entry-cell index per member, actual owned reservation boundary |
| Speeds | speeds | []float64 | One exact physical speed per member, finite and within 0 through 2.5 m/s |
| Targets | targets | []float64 | One fixed lane-local recovery destination per member |
| LandingSpeeds | landingSpeeds | []float64 | One frozen positive landing proof per unfinished destination |

All per-member arrays must have exactly `len(members)` items.
No field accepts null, an omitted value, an unknown member, or a value outside its bounds.
The existing saved lane and route distances remain authoritative for member positions.
The certificate adds no vehicle length, clearance, reaction, braking, or tunable profile fields.
The kind pins the approved four-meter profile and numeric envelope.
Follower links retain their existing lane indexes and terminal-cell index.
Their turn must be exactly zero, lanes exactly one, and draining false while the compact certificate exists.

## Phase validation

A `compact` certificate with two through four members must pass constructive admission at its saved physical state.
Its targets and landing speeds must equal the deterministic admission proof for that exact state and stop boundaries.
A one-member compact certificate must pass anticipatory holding at the selected valid platoon limit.
Its stored recovery proof must equal the deterministic singleton braking proof.

A `recovering` certificate must pass retained-phase validation with its fixed targets and landing speeds.
Restore must not recompute these destinations or reserve another six meters per pair.
The controller keeps the certificate until every member is stopped at its target with ordinary 12.01-meter gaps.
Resource safety must also permit removal.
A recovering singleton must retain its exact discrete braking destination.

## Physical and resource checks

Every member must remain traveling and buffered on the same straight fixed-entry lane and destination station.
All members must use the same supported four-meter physical profile and uniform remaining-route speed limit at most 2.5 m/s.
The lane shape must have zero turn.
The existing entry endpoints, conflict zones, berth resources, and ordinary release tails remain protected.
Only certified plain interior track cells can have shared owners.
A stop-cell value must describe actual retained reservations inside the certified plain region.
Restore must reclaim the full saved stopping track before validating the proof.
A certificate cannot grant track through an unrelated owner.
Outside traffic and nonadjacent members retain the ordinary 12-meter separation check.

Certificates are disjoint and complete.
Each ID appears in at most one certificate, every follower link appears in exactly one certificate, and no ordinary link enters a compact group.
Invalid physical placement, profile, ownership, topology, speed, arrays, phase, or proof makes restore fail.
This also applies to `LogicalOnly` requests.
Restore cannot demote or logically requeue a malformed compact certificate in the same epoch.
Restored state preserves exact member speeds and fixed proof values.
Runtime policy off restores the certificate in recovering mode before ordinary movement resumes.

## Bounded bytes and consumers

At most 300 members can appear across all certificates, with at most four members in each certificate.
The maximum field sizes use existing bounded identifiers and finite numeric encodings.
The conservative byte estimate replaces each head link with its certificate and retains follower links.
It includes 300 members, escaped 64-byte IDs, longest finite numeric encodings, and all group sizes from one through four.
The largest estimate is 83,829,396 bytes, below the 83,886,080-byte save cap by 56,684 bytes.
These independent maxima bound encoding size.
They do not describe a physically reachable state.
Root must extend the permanent saved-state fixture and validate its encoded shape before landing this representation.
The 80 MiB save and 64 MiB stream caps remain unchanged.
The stream publishes only the approved spacing policy and ordinary pod observations.
It does not grant an oracle exception from a browser field.
The native oracle obtains exceptions only from physically validated live certificates.

Root owns project, session, remote, view, and byte-format consumers.
Native integration may proceed privately before field approval.
Wire edits, cold-restore integration, and downstream consumer changes depend on approval of this exact representation.
