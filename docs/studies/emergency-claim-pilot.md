# Emergency claim pilot

A matched pilot of 2026-11-24 on the HX11 failure.
It is a pilot of 4 runs of one seed, and not a qualification.
The options are in the [emergency berth claim proposal](../contracts/emergency-berth-claim-proposal.md) and in [late berth binding](../design/late-berth-binding.md#93-the-nearest-passenger-berth).

## Method

Each arm is the `incidents` arm of the soak on `london-full-shared-express-wharf`, seed 1, 6 simulated hours and the clear, at `e186a891` with a scratch patch.
The full check, the saves, and the rewinds are on, and the full check runs once a second and not at each tick.
A reference run and its queue check are not in the runs.
The patch tags each claim with the code that made it, and it did not land.

| Arm | Change |
| --- | --- |
| Control | None. |
| A | `passengerArrivals` also counts a pod whose purpose is an emergency unload, at its bound berth. |
| N | `clearToPassengerBerth` keeps its local pass, and then takes the remote passenger berth with the lowest preferred route cost. The refusal of a targeted berth stays. |
| A+N | Both. |

## Results

| Measure | Control | A | N | A+N |
| --- | ---: | ---: | ---: | ---: |
| `i4.93`, time not blocked (s) | 653.0 | 59.7 | No such record | No such record |
| Late records | 1 | 0 | 0 | 0 |
| Longest record not blocked (s) | 653.0 | 462.1 | 406.3 | 406.3 |
| Records | 71 | 73 | 74 | 74 |
| Median and p95 time not blocked (s) | 74.8, 417.3 | 74.5, 396.0 | 94.5, 361.8 | 94.5, 361.8 |
| Completed orders | 4,497 | 4,640 | 4,549 | 4,549 |
| Interrupted orders | 93 | 97 | 95 | 95 |
| Mean order wait (s) | 786.7 | 762.3 | 789.8 | 789.8 |
| Longest order wait (s) | 5,174 | 4,844 | 4,735 | 4,735 |
| Empty distance (km) | 17,289 | 17,643 | 14,771 | 14,771 |
| Remote clears | 415 | 405 | 826 | 826 |
| Remote clear length, p50 and p95 (m) | 9,569, 19,686 | 9,510, 18,594 | 1,869, 3,855 | 1,869, 3,855 |
| Fault wait (pod-ticks) | 284,564 | 283,328 | 314,699 | 314,699 |
| Delayed riders (rider-ticks) | 233,437 | 238,719 | 330,079 | 330,079 |
| Settle after the clear (s), limit 1,800 | 1 | 15 | 702 | 702 |

- No arm has a safety, save, or rewind failure.
- The control gives the late record again, with the same start tick 346041.
- A+N gives the same records as N: no emergency made a claim yield in N, so option A does not act.

## What it shows

- The cause is now known.
  At each sample of the wait, the two resources of the berth have the owner `london-pod-113`, and the claim kind is a service claim, which is revocable.
  The claim has the tag of the remote pass of `clearToPassengerBerth`.
  The owner is in service, not released, not rebalancing, and not linked.
- Option A removes that wait.
  The records that ended before `i4.93` are the same in the control and in A, so the runs agree to that point, and the record then ends after 59.7 s and not 653.0 s.
- N removes the situation and not the mechanism.
  The trajectory diverges, and the record does not exist, so N does not show that an emergency pod no longer waits on a claim.
- N makes a cleared pod go a fifth as far, and it makes twice as many clears.
  The empty distance falls by 14.6 percent.
- N is worse in fault wait (+10.6 percent), in delayed riders (+41 percent), in the median time of a record (+20 s), and in the settle time (702 s, inside the limit).
  One seed cannot say if these are effects of N or of a different trajectory.
- In the control, `clear-remote` is the only tag with a claim that an emergency pod waited on, and it was the one claim.
  In A, one claim has an empty waiter for 511 sampled seconds.
  The pilot does not say what that pod waited for.

## What it does not show

- A run of the release form, with the full check at each tick.
- Another seed, another preset, or another incident arm.
- Any contract question: option A narrows the decision of October 4, 2026, and clauses of the suspension contract.
- A test that a claimant that yields also makes progress in each case.

## Reading

Option A is sufficient for `i4.93` in this run.
The wait of A is one that the existing yield already ends for an ordinary trip.
N is a separate change of ordinary traffic with a clear benefit in empty distance and an unexplained cost in three measures.
It needs its own seeds before a decision.
