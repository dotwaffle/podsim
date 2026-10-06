# Station entry platoon proposal

Historical design, September 30, 2026.
The approved fixed-entry implementation and version 4 fields are documented in [the current contract](station-entry-platoons.md).
Berth-access platoons remain excluded.
New buffer admissions and pickup reassignment remain off by default.

## Recommendation and alternatives

Start with a fixed certificate inside one eligible buffer entry lane.
Keep berth-access lanes, upstream merge conflicts, and branch conflicts outside that certificate.
Retain current clearance, reaction time, braking, complete reservation groups, and platoon limits.
Do not promise more stopped pods per meter.
The possible benefit is shared admission while a queue moves, followed by faster discharge.

| Approach | Benefit | Cost or limit |
| --- | --- | --- |
| Keep current exclusions | Existing safety and restore behavior | No shared admission inside the buffer |
| Continue the current lane certificate through berth branches | Reuses parts of the current link logic | Needs early berth routes, can block route changes, and conflicts with deferred berth choice |
| Add a fixed buffer certificate | Preserves deferred berth choice and bounds shared ownership | Needs a new terminal endpoint, head-discharge transition, and saved-state contract |

The fixed buffer certificate is the recommended next design.
Its implementation needs separate approval and independent safety review.
Shorter stopped gaps would be a separate design.

## Constraints before implementation

`sharedLane` in `internal/sim/platoon.go` excludes station-entry and berth-access lanes.
It also requires both routes to continue after every shared lane.
A berthless buffer route ends at `Station.Entry`, so removing the role exclusion does not make that route eligible.

`linkEnds` derives its endpoint from complete lanes and excludes resources whose release distance reaches that endpoint.
`maintainLink` retains a draining link while the follower holds resources owned by an ancestor.
`releaseRouteResource` passes ownership to the first follower that still holds the resource.
These rules prevent a split from exposing resources that a follower still needs.

`grantBufferedHead` in `internal/sim/station_buffer.go` rejects every coupled pod.
A head with a follower is coupled even when it has no predecessor.
Allowing buffer links without changing this discharge rule would make the head wait for followers that cannot advance past it.

The current saved certificate contains route lane indexes, lane count, turn, and draining state.
It has no terminal cell endpoint.
Version 3 buffer membership alone does not authorize the proposed links.

## Formation and certificate endpoint

Initially form a buffer link only when both pods physically occupy the same eligible station-entry lane.
Both pods must have explicit buffer membership and the same target station.
The follower must be slow and immediately behind its predecessor.
Keep existing limits on member count, compatible speeds, path turn, and safe stopping separation.
Retain `oneSpeedLimit` across both remaining routes and both destination stations' lanes, including future exclusive berth suffixes.
Reject unequal-speed fixtures unless a separate braking proof permits them.
Do not merge an upstream platoon into a buffer platoon in this first design.

Use the existing `bufferPlan` interior and its downstream stopping frontier as the geometric endpoint.
Keep every upstream and downstream conflict group outside the shared span.
Every shared resource must have its complete release distance, plus the existing drain margin, before that endpoint.
A complete reservation group that crosses the endpoint is ineligible for shared admission.
If no additional complete group fits, reject the link and use ordinary buffer admission.

Record the terminal cell on the entry lane, rather than a global route block index.
Route prefixes can be omitted when a pod saves its state, but the saved network preserves lane identity and cell geometry.
The certificate must retain its original turn and derived clearance.
Use a conservative turn bound for the certified path, including curved entry geometry.
Do not recompute a smaller clearance when a save omits an earlier route prefix.
Every new endpoint cap must be ahead of the follower's existing stopping point when the link forms.

This local certificate does not grow onto the berth branch.
An existing mainline certificate drains under its current rules before a new local link forms.
It must not become a buffer certificate by changing a flag.

## Head discharge and splitting

Treat head discharge as an atomic route extension with retained ancestor ownership.
The head may append an exclusive berth suffix only after the complete path and berth grant succeeds.
Every lane, block resource, release point, and reservation in the certified prefix must remain unchanged.
Appending a suffix is not permission to replace a reserved approach or choose a different entry lane.
If rebuilding the route changes a shared reservation group, deny the transition.
Before formation, require at least one structurally valid exclusive berth suffix for every prospective queue head.
Reject a link if geometry or speed constraints would prevent all discharge paths even with empty berths and free tracks.

The head keeps its follower relation while it still owns resources held by that follower.
Those resources pass to followers through the existing ownership transfer rule as the head advances.
The appended branch, berth, and exit resources cannot become shared resources.
Each follower remains bounded by its original terminal certificate and its own safe buffer stopping frontier.

Mark each affected local link as draining when its predecessor begins exclusive discharge.
No further coupled grants may extend that link.
Remove a link only after its follower holds no resource owned by an ancestor.
Do not remove links at berth selection, at a fixed distance, or because the predecessor leaves the lane.
Then the next physical queue head can choose and reserve its own berth suffix.

Keep the existing lane order and passenger, pickup, empty, and aging priorities between competing approaches.
An idle berth owner must still be able to depart through an exclusive path.
No buffer platoon may reserve departure conflict resources only to hold a queue position.

This transition requires a proof that prefix rebuilding and resource transfer preserve the existing ownership graph.
The current generic route-change checks do not establish that proof.

## Braking and fallback

Keep the current 12-meter physical clearance, turn-derived path clearance, 0.5-second reaction allowance, and acceleration limit.
Compute all follower caps from the state before movement.
Also cap each follower at its buffer stopping frontier and fixed certificate endpoint.
A predecessor that enters a branch must not cause a follower's cap to move beyond that endpoint.

Check each predecessor's stopping point and the complete ancestor chain.
The follower must never need stronger braking because a link forms, drains, or splits.
If the head cannot acquire a berth path, it retains its original stopping frontier.
Followers stop under the same constraints, and a full buffer retains ordinary upstream waiting.

Disabling new buffer or platoon admissions must preserve existing links until they drain safely.
Geometry without a valid local certificate uses the existing uncoupled buffer controller.
Do not weaken a conflict group, use a smaller gap, or force a route change to recover progress.

## Saved-state contract for review

Recommend an explicit version 4 extension for terminal buffer certificates.
Keep version 2 and version 3 validation and physical restore behavior unchanged.
Do not reinterpret their existing lane-only certificates as terminal buffer links.

A proposed buffer certificate needs a distinct kind and a terminal cell on its final certified entry lane.
Existing leader, route offsets, lane count, fixed turn, and draining state remain necessary.
The exact field names and limits require approval before implementation.
Project, command, and stream contracts would remain unchanged unless a separate proposal adds a control.

Version 4 restore must validate the certificate against both saved routes and the saved network.
It must restore stopped followers, a head with an appended berth suffix, and repeated saves during ownership transfer.
Rebuild the same certificate and valid reservations without admitting a new link or choosing a new berth.
Save trimming must retain the predecessor's certified entry lane and original turn, as existing lane certificates require.
Reject cycles, duplicate followers, invalid terminal cells, conflict-spanning groups, and insufficient stopping separation.
Do not widen a certificate during restore to make an invalid saved position fit.

Write version 4 while a terminal certificate or its draining ownership remains.
After those links drain, use the existing version 3 or version 2 rules as appropriate.
No downgrade may discard ownership or buffer membership.

## Implementation gates

First prove the terminal endpoint and immutable-prefix extension on a small deterministic fixture.
Exercise two to four pods, curved entries, unequal speed limits, complete groups near both endpoints, and repeated resource IDs.
Include a head with no predecessor but an active follower, blocked berths, blocked departures, and different eventual berth choices.

Check separation, stopping points, ancestor ownership, admission groups, request accounting, and progress after every tick and command.
Save and restore before formation, while stopped, during head extension, during ownership transfer, and after each split.
Retain the existing version 2 and version 3 restore oracles.
Require independent review before any production behavior lands.

Then compare identical Acton bursts, LondonCentral traffic, and sustained LondonFull demand with local links off and on.
Report moving admission, queue occupancy, mainline blocking, discharge rate, waits, tails, and unfinished requests.
A lower completed maximum does not establish a benefit for every request.
Keep the feature unadopted if it has no measured benefit or makes departure progress worse.

The current contract records the approved fields and implementation.
Capacity qualification and default adoption remain separate.
