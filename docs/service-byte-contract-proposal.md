# Service byte limits

Status: approved October 2, 2026.
Current byte caps remain unchanged.

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

- Complete project 3, save 6, hello 3, order consent, whole parties, and class compatibility for supported legacy and compact profiles.
- Keep the current maximum saved queue of 2,600 records for states with supported legacy and compact profiles.
  Keep each such pod's existing bound of eight stored riders, including completed history.
  Compact admission still respects its four seats.
- Keep express registry and class capacity metadata at the approved values.
  Group and express profiles remain unavailable for physical placement, startup, and restore.
- Permit the save-6 parser to recognize the approved larger array shapes.
  Do not activate larger valid operating states until a separate physical-profile and byte qualification passes.
- Require the later large-profile proposal to include an encoding that fits the existing 80 MiB save and 64 MiB stream caps.
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

Evidence: `~/.cache/agents/podsim/roadmap-service-20261002/service-byte-proof-receipt.json`.
The cache also retains baseline and projection logs and the exact projection patch at base `ff43bcb`.
