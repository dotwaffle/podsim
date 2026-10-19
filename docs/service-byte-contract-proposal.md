# Service byte limits

Status: approved October 2, 2026.
The byte caps at approval remained unchanged.
Since 64b4f3f, the stream JSON cap is 65 MiB, and the gzip message cap is 66 MiB.
Status 2026-10-07: by maintainer decision, the saved-state and stream byte proofs that this contract requires were deleted.
At that decision, the fleet limit was 600 pods, the plain queue bound was 5,000 records, and the saved-state cap was 100 MiB.

## October 15, 2026 amendment

The maintainer approved separate binary byte budgets and restored the plain byte proofs.
Projects remain capped at 32 MiB.
Full-stream JSON is capped at 64 MiB.
HTTP-state JSON and saved-state JSON are each capped at 128 MiB.
The topology allowance remains the project cap plus 4 KiB for its envelope.
Gzip stream messages add 32 bytes per 65,535-byte input block plus 32 bytes for the wrapper and final block.
At the stream cap, that allowance is 32,832 bytes.
The saved-state cap also limits compressed files to 128 MiB.

`TestPlainStateFileWorstCaseSize` and `TestPlainStreamMaximumEncoding` bound the plain format through its production encoding rules.
Completed rider history stays outside the outstanding-order budget and remains present in the proofs and the running simulation.
Motion-only deltas use stream version 7 without changing full-frame or saved-state members.
Express retains its own 600-pod fleet and 17,000-order bound.
Increasing plain limits must not increase either Express limit implicitly.
Express still has no independent worst-case byte proof.

The approved fleet promotion requires matched London qualification at 327, 600, and 1,000 pods before the production plain limit changes.
That qualification passed, including an isolated baseline/current comparison of the overloaded cases after an unexplained timing miss.
The plain fleet and restore limits are now 1,000 pods and `200 + 1,000 * 8 = 8,200` outstanding orders.
The session admission queue remains 200, and completed history remains outside the outstanding-order budget.
The plain upper bounds are 125,380,377 saved JSON bytes, 48,618,482 full-stream JSON bytes, and 82,174,660 HTTP-state JSON bytes.
Those bounds include the independent project or topology cap as appropriate and fit the separate budgets above.
The qualification uses one geometry, normal and overloaded traffic, safety and progress checks, production encoders and client readers, physical restore, and continued movement.
`mise run qualify:london` repeats those cases explicitly.

## Earlier proposal

The approved foundation requires saved-state and stream byte proofs before the expanded counts land.
The existing conservative fixtures nearly fill their caps.
An isolated count projection fails both gates before it adds the new order fields.

| Fixture | Current bytes | Expanded count projection | Limit |
| --- | ---: | ---: | ---: |
| Saved JSON | 83,301,596 | 140,440,796 | 83,886,080 |
| Full stream JSON | 60,852,176 | 114,664,976 | 67,108,864 |

The projection uses 6,200 pending records and 20 stored riders per pod.
It retains existing text, route, project, fleet, and byte bounds.
These are conservative independent maxima.
They do not prove that a supported fleet can reach that combination.
Completed rider history does not count as an outstanding order, so an outstanding-order bound alone does not remove that history.

## Recommended staged contract

Approve this qualification scope:

- Complete project version 1 without contract markers, the saved state, the stream, order consent, whole parties, and class compatibility for supported legacy and compact profiles.
- Keep the current maximum saved queue of 2,600 records for states with supported legacy and compact profiles.
  Keep each such pod's existing bound of eight stored riders, including completed history.
  Compact admission still respects its four seats.
- Keep express registry and class capacity metadata at the approved values.
  Group and express profiles remain unavailable for physical placement, startup, and restore.
- Permit the saved-state parser to recognize the approved larger array shapes.
  Do not activate larger valid operating states until a separate physical-profile and byte qualification passes.
- Require the later large-profile proposal to include an encoding that fits the existing 80 MiB save and 64 MiB stream caps at approval.
  Text tables or another bounded encoding may require a separate reviewed wire contract.
- Preserve manual session admission at 200 and offline comparison queue settings.
  Add no new native submission ceiling and truncate no accepted orders or histories.

This stages the count amendment.
It does not increase byte caps or discard historical passenger records.
The current-profile fixtures must include all new order fields and migration markers before this foundation lands.

## Alternative

Design and approve the larger-state encoding now, before any foundation integration lands.
This keeps the count amendment in one delivery but delays the consent and compatibility work.
The staged contract lets those features finish while the physical and larger-state contracts remain under review.
