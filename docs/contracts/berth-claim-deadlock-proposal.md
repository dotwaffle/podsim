# Berth claim deadlock proposal

Status: drafted on November 8, 2026.
The maintainer chose option A on November 8, 2026.

## Failure

The [Express London adoption screen](express-london-adoption-screen-proposal.md#two-stalls-in-d) found a permanent stall at Bank bank `b` in project `D`, in `pm-peak` seed 1.
No safety, contract, or accounting check reports it.
Commit `1f721a5c` removes the geometry that caused it in `D`, but the simulation rule that turns it into a deadlock stays.

A trace of the run on the geometry before `1f721a5c` shows the sequence:

1. At 7,549 s and 7,595 s, dispatch sends 3 empty Express pods from Waterloo as pickups for hub orders at Bank.
   Each pickup goes to berth `b01` of Bank, and no pickup claims the berth.
2. At 7,719 s, a Compact pod needs berth `b01` of Waterloo, where the empty pod `express-03` is idle.
   `clearBlockedBerths` finds no parking for it and sends it to berth `b01` of Bank, and the move claims that berth.
   The trace does not show which of the two clear paths, `guardedClear` or `clearToPassengerBerth`, chose the berth.
3. The 3 pickups reach the single access lane of berth `b01` first, and `express-03` queues behind them.
   The first pickup waits for the berth, which `express-03` owns, and `express-03` waits for the pickups in front of it.
   Nothing moves after that, and a Compact pod with a rider waits behind the queue.

## Cause

An empty move that claims its berth checks only that the berth has no owner (`prepareEmptyMove`).
It does not check for a moving pod that goes to the berth with no claim, such as a pickup.
`berthAvailable` makes that second check, but only the first pass of `clearToPassengerBerth` uses it.
The guarded clear and the second pass of `clearToPassengerBerth`, which came with `f1351d6b`, do not.
A claim gives the berth to the claimant, but the order on a single access lane is the order of arrival, so a claimant behind those pickups makes a circular wait.

The deadlock needs 3 conditions together: no parking for the blocker, a berth that only unclaimed pods go to, and a claimant whose route to that berth goes behind them.
The old geometry of `D` gave the first condition at each Waterloo bank `b` berth, because the Express class had no route to parking.
After `1f721a5c`, the presets have no known case of it, but the parking check of `54c5b635` does not exclude a full parking.

## Options

| Option | Change | For | Against |
| --- | --- | --- | --- |
| A | A claiming empty move also refuses a berth that a moving pod goes to, as `berthAvailable` does. | Small and in one place, for each caller that claims. | A blocker waits more often with no place to go, until a berth frees. The second pass of `clearToPassengerBerth` then has no use, and the tests of `f1351d6b` need a review. |
| B | Refuse only a berth that a moving pod goes to on the same access lanes as the route of the claimant. | Keeps more clears. | Needs a lane-order test on routes, which is new code on a hot path. |
| C | Make a pickup claim its berth when dispatch sends it. | Removes unclaimed moves to a berth, for each caller. | Changes dispatch and the berth choice of all pickups, so it needs soaks, the parity tests, and new saved-state rules. |
| D | Detect the cycle, and give the claim to the first pod of the queue. | Recovers from any such cycle. | A new liveness rule in the motion stage, with state that a restore must keep. |

The recommendation is option A, with a test that builds the 3 conditions on a small network, fails before the change, and passes after it.
Then the routine soak, the CI soak, and the screen rerun of `D` give the effect on service.
If option A gives longer waits in those runs, option B is the next candidate.

## Decisions for the maintainer

1. Choose an option: A (recommended), B, C, D, or no change.
2. Approve the test and the evidence runs above.

## Result of option A

`prepareEmptyMove` now refuses a claim on a berth that another moving pod goes to, and `clearToPassengerBerth` has one pass.
`TestClaimingMoveRefusesTargetedBerth` fails without the new refusal.
On the geometry before `1f721a5c`, the `pm-peak` seed 1 run no longer gets the cycle at Bank.
It stalls at Waterloo instead, where an idle Express pod has no parking and no free berth to go to, which is the second stall of the screen.
So the two fixes are both necessary: `1f721a5c` gives the pod a way to parking, and option A keeps a clear from making a cycle.

At `ead9846c`, `test:race:sim`, the routine soak (50 runs, 0 late, 0 demoted), and `TestIncidentSoakCI` pass.
The full adoption screen gives the same counts in each arm and each gate as at `1f721a5c`, so option A changes no result of those runs.
