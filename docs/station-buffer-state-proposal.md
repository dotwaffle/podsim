# Station buffer saved-state proposal

Status: original bounded v3 proposal approved September 29, 2026.
The user approved the membership amendment below on September 29, 2026.
The experimental controller keeps new admissions off by default.
It has no project, command, or editor control.

## Required contract change

Version 2 physical restore rejects a berthless pod whose reservation reaches its final route lane.
See `internal/sim/state_physical.go`, `placeTravelingPod`.
London's proposed holding lane is that final lane, ending at `Station.Entry`.
Thus version 2 cannot restore a pod stopped in this buffer without physical demotion.

Use session saved-state version 3 for buffered arrivals.
Keep version 2 loading, validation, and physical restore behavior unchanged.
The existing version 2 reader rejects version 3 before it decodes the body.
The new reader must accept both versions explicitly and pass the selected contract to simulation restore.
Do not relabel version 2 payloads before validating them.
HTTP commands, WebSocket frames, and project files do not change under this proposal.

## Representation

Use the optional `stationBuffered` field as the authoritative membership flag.
A physical buffer member remains Traveling and has no destination berth.
A pending member can still be boarding or continuing at its origin berth.
Each member retains its destination station, connected entry-ending route, physical position, riders or pickup assignment, and wait age.
Derive ordering and holding frontiers from the saved network, route, and position.
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
A destination assignment does not grant exclusive berth ownership.
The buffer head can compete for an unowned berth, including one targeted by a pickup behind it.
Otherwise the head and following pickup can block each other indefinitely.
The complete-path grant must acquire the berth, its node, and every required track or conflict resource before committing.
A failed trial changes no assignment, route, or ownership.
Do not revoke existing reservations or assignments to make buffer space.
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

## Approved membership amendment

Independent review found that existing pod fields cannot preserve pending membership when admissions are disabled.
An old buffer member can keep the session at version 3 while a new ordinary trip remains upstream.
Both routes can end at the same station entry without a berth.
Inferring membership from every such route incorrectly admits the new trip after restart.

Add one optional saved-pod Boolean, `stationBuffered`.
Write it only for a current member, including a pending upstream admission.
Under version 3, validate its route, destination, passenger or pickup binding, and eligible holding geometry before restoring membership.
An empty member whose pickup was released keeps drain membership until redirection, arrival, or demotion.
The existing `released` flag identifies that case.
This also covers orders dropped during restore and trip routes omitted during export.
A false or absent value gives no pending membership.
Physically berthless entry occupancy must have the flag and pass the existing position, ownership, and separation checks.
Version 2 rejects the flag instead of interpreting it.
Ordinary version 2 output remains unchanged.
Project files and WebSocket frames remain unchanged.

Keep admissions off after restore.
Existing flagged members drain under the buffer controller.
Unflagged ordinary trips retain early berth assignment.
Test mixed flagged and unflagged routes through a save and restart, including boarding and upstream travel.
Require independent review and the original endpoint checks before a local commit.

## Implementation limits and initial measurements

The controller accepts entry-ending routes that have not received a berth assignment.
Existing pickup routes generally have a berth assignment at dispatch and retain it.
The controller does not remove these assignments to create a queue.
Flagged pickup routes from saved states can drain when they meet the version 3 checks.

Each eligible lane needs at least two contiguous interior cells with no conflict resources.
The cells must be at least 12 meters long.
The controller preserves complete endpoint reservation groups and the ordinary stopped-pod clearance.
These cells describe conservative stopping positions, not extra berth capacity.
Only the physical head can commit a complete, available berth path.
An occupied berth's idle owner can leave under the existing clearing controller.

The LondonFull study project has 269 eligible entry lanes.
Acton Town has three stopping cells, with front positions from 55.57 to 111.14 meters along its entry lane.
The short comparison used seed 1, AM peak demand at 10 requests per minute, and 30 minutes of arrivals.
Both modes completed 298 of 299 requests within one simulated hour.
Mean pickup wait was 87.89 seconds with buffers off and 88.91 seconds with buffers on.
Stopped time on other travel lanes increased from 296 to 437 pod-seconds.
That counter includes station access lanes and does not isolate mainline blocking.
Neither arm observed a stopped pod in a buffer at its once-per-second samples.
This arm does not establish a congestion benefit or a capacity limit.

The instrumented run took 13.35 wall seconds with buffers off and 15.45 seconds with buffers on.
It includes safety and order checks each simulated second.
It does not measure the running server's maximum playback speed.
Raw profiles and the frozen source manifest are in `~/.cache/agents/podsim/station-buffer-20260929/profile/`.
Longer load comparisons remain separate qualification work.
