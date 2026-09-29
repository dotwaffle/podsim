# Station buffer saved-state proposal

Status: pending user approval. No buffer controller or saved-state change is implemented.

## Required contract change

The current physical restore rejects a berthless pod whose reservation reaches its final route lane.
See `internal/sim/state_physical.go`, `placeTravelingPod`.
London's proposed holding lane is that final lane, ending at `Station.Entry`.
Thus the existing contract cannot restore a pod stopped in the proposed buffer without physical demotion.

Use session saved-state version 3 for buffered arrivals.
Keep version 2 loading, validation, and physical restore behavior unchanged.
The existing version 2 reader rejects version 3 before it decodes the body.
The new reader must accept both versions explicitly and pass the selected contract to simulation restore.
Do not relabel version 2 payloads before validating them.
HTTP commands, WebSocket frames, and project files do not change under this proposal.

## Representation

Reuse the existing saved pod fields.
A buffered pod remains Traveling and has no destination berth.
It retains its destination station, connected entry-ending route, physical position, riders or pickup assignment, and wait age.
Derive queue membership, ordering, and holding frontiers from the saved network, route, and position.
Do not add a saved queue number or slot number.

New buffer admissions remain off by default until qualification and a separate adoption decision.
Write version 3 when the controller is enabled, buffered pods remain, or pending admissions depend on the buffer contract.
Version 3 restore must safely drain a saved buffer even when new admissions are disabled.
Disabled mode denies new berthless buffer admissions but retains discharge for existing buffer members.
Other arrivals and ineligible stations keep the existing early berth-assignment controller.
Existing unbuffered saves can retain version 2 output.

## Admission and movement

Initially accept only a directed final entry lane with a nonbranching holding region and a downstream berth-choice node.
Its existing resource groups must leave room outside upstream conflicts, downstream branch conflicts, and departure paths.
Derive stopping frontiers from complete reservation groups.
Never shorten a conflict region or split a reservation group to create capacity.
Check braking distance, tail clearance, occupied cells, and existing reservations before entry.
Use ordinary reservation-boundary braking.

A berthless pod must never reserve through the terminal route endpoint.
Arrival requires a real destination berth.
Reaching a holding position does not start boarding, unloading, or journey completion.

The physically furthest downstream pod is the head, including any already assigned pod ahead.
Within a buffer, only a berthless head can select its berth suffix.
Commit the destination and safe berth-path admission atomically.
Keep existing reservations and berth assignments.
Do not revoke them to make buffer space.
Preserve passenger, pickup, and empty priorities and the ten-second aging override.
Full buffers retain ordinary upstream waiting.

## Restore validation

For version 3 only, permit berthless occupancy of an eligible final entry lane at or before its holding frontier, strictly before the terminal endpoint.
Reconstruct complete reservation groups and pod tail footprints.
Validate connected routes, journey or pickup bindings, queue order, separation, and ownership before accepting physical restore.
Preserve wait age.

Reject physical restore when ordering is ambiguous, resources conflict, a position exceeds its frontier, or a berthless pod occupies the endpoint.
Keep the existing reported logical fallback and order reconciliation.
A logical fallback is not faithful physical buffer restoration.
Version 2 keeps its current rejection and demotion behavior.

## Platoons and validation

Keep station-entry and berth-access platoons excluded in this first implementation.
Their certificate endpoint, safe drain, and inherited ownership need a separate proposal and approval.
Ordinary upstream platoons must separate before entering the buffer under existing rules.
Do not reduce stopped-pod separation.

Required checks include version 2 golden loading, version 3 rejection by the old decoder, and stopped-queue version 3 round trips without demotion.
The same berthless final-lane payload must retain version 2 rejection or demotion but restore physically under version 3.
Also check disabled-mode drain, invalid route/frontier/ownership mutations, full buffers and berths, blocked exits, and competing aged arrivals.
Test grandfathered reservations, failed head admission, unchanged passenger accounting at holding points, and eventual departure progress.
Compare mainline blocking, buffer occupancy, throughput, passenger waits, and sustained spillback against the existing controller.

The implementation must pass relevant tests, race checks, and independent review before local merge.
If safe geometry or reservation frontiers cannot be established, preserve the proposal and switch to another approved overnight task.
