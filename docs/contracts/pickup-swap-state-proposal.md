# Pickup swap state proposal

Status: draft for the maintainer, 2026-11-24.
Nothing is approved or built.
Decision EC-D9 of the [exact restore coordinates](exact-restore-coordinates-proposal.md) left this as a separate follow-up.

## The problem

`pickupSwapController` (`internal/sim/pickup_swaps.go`) holds the pair cursors, the alternative cursor, the time of the next scan, the budget of the second, the cooldown of each pod, and the counters.
The saved state has none of it, so a restored session starts the controller again.
Pickup reassignment is on in the default project.
So a restored session can make a swap that the live session does not make, or make it at another time.
`docs/operations.md` already says that future reassignment decisions can differ after a restart.
This is a difference of behavior and not of safety: a swap uses the same checks in both.

## Proposal

Save the part of the controller that changes the later decisions, when the policy is enabled.

| Field | Saved | Reason |
| --- | --- | --- |
| `nextTick` | Yes | It decides when the scan runs. |
| `left`, `right`, `alternative` | Yes | They decide which pairs the scan checks first. |
| `budgetSecond`, `scans`, `routes` | Yes | A save can be in the middle of a second. |
| `cooldown` | Yes, only the entries with a tick after the saved tick | An older entry has no effect. |
| `stats` | No | The counters do not change a decision, and a restored session starts them at zero today. |
| `enabled` | No | The project sets it. |

The restore checks the fields before the tiers:

- the cursors are in the range of the fleet, in the form that `nextPair` gives, or the start values 0 and 1, which a fleet of one pod also has;
- `nextTick` is at most one second after the saved tick, and `budgetSecond` is at most the saved second;
- the counts are in the range of the budgets, also when `budgetSecond` is before the saved second, because the controller resets them only at its next scan;
- each cooldown names a pod of the fleet one time, in the order of the pod IDs, with a tick after the saved tick and at most 30 s after it.

A value that fails is a damaged file, and the project rule for such a file applies (it moves aside).
The order of these refusals and their text are a part of the format, and a test pins them.
At the logical tier and for a demoted pod, the cooldown stays and the cursors stay, because both refer to the fleet and not to the route.

## Cost

- The state version goes from 14 to 15.
  The golden files, the version text of `docs/operations.md`, and the byte proofs change with it.
- The largest entry set is 1,000 cooldowns.
  At about 100 bytes each, it adds about 100 kB (estimate) to a plain state with 8,172,163 bytes of headroom, and 600 of them to an Express state with 29,000,505.
- The stream does not change.
- `ExportState` runs under the session lock, and the cooldown copy is one pass over the map with a sort.

## Test plan

1. Lockstep of a saved and a live session with the policy on, as the lockstep test of the exact coordinates with swaps off today.
   The test must reach a swap after the restore, and it must fail with each field left out.
2. Round trip of each field, and a refusal of each bound, in order.
3. A save of an idle session with counts of an earlier second, and one with a fleet of one pod.
4. A save in the middle of a second after a swap, and one with an expired cooldown.
5. The byte proofs.

## Open questions

- A restore that requeues a trip changes the assignments that the cursors walk.
  The proposal keeps the cursors, because they name pod indexes and not trips.
- The cooldown map in the simulation has no removal, so it keeps one entry for each pod that ever swapped.
  The proposal does not change that.

## Decisions for the maintainer

| ID | Question | Options | Recommendation |
| --- | --- | --- | --- |
| PS-D1 | Save the state | A: build this proposal. B: keep the loss, as `docs/operations.md` states it. C: save only the cooldown. | B until another change needs a new state version, because the loss is not a safety matter and its size is not measured |
| PS-D2 | The counters | A: not saved. B: saved. | A |
| PS-D3 | If A, when | A: with the next state version change. B: alone. | A |
