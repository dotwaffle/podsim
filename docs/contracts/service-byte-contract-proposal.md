# Service byte limits

Status: approved October 2, 2026.
Amended through November 4, 2026.
Git history holds the dated amendment notes and the earlier text.
The byte caps at approval remained unchanged.
The October 15 amendment below sets the current caps and limits.

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
Motion-only deltas change no full-frame or saved-state member.
Express retains its own 600-pod fleet and 17,000-order bound.
Increasing plain limits must not increase either Express limit implicitly.
`TestExpressStateFileWorstCaseSize` and `TestExpressStreamMaximumEncoding` bound the Express format in the same way.

The approved fleet promotion requires matched London qualification at 327, 600, and 1,000 pods before the production plain limit changes.
That qualification passed, including an isolated baseline/current comparison of the overloaded cases after an unexplained timing miss.
The plain fleet and restore limits are now 1,000 pods and `200 + 1,000 * 8 = 8,200` outstanding orders.
The session admission queue remains 200, and completed history remains outside the outstanding-order budget.
The plain upper bounds are 125,380,377 saved JSON bytes, 48,618,482 full-stream JSON bytes, and 82,174,660 HTTP-state JSON bytes.
Those bounds include the independent project or topology cap as appropriate and fit the separate budgets above.
The qualification uses one geometry, normal and overloaded traffic, safety and progress checks, production encoders and client readers, physical restore, and continued movement.
`mise run qualify:london` repeats those cases explicitly.

## Recommended staged contract

This section is the scope that the approval of October 2, 2026 covers.
The amendment above replaces its queue bound of 2,600 records and its 80 MiB save cap.

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
