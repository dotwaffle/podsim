# Emergency claim pilot

A matched pilot of 2026-11-24 on the HX11 failure.
It is a pilot of 4 runs of one seed, and 4 more runs of seeds 2 and 3 for N, and not a qualification.
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

## N on seeds 2 and 3

Four more runs of 2026-11-24 compare the control and N on seeds 2 and 3, at `42061c68`, in the same form.
The table includes seed 1 of the first pilot.

| Measure | Seed 1, control and N | Seed 2, control and N | Seed 3, control and N |
| --- | --- | --- | --- |
| Late records | 1, 0 | 1, 1 | 0, 0 |
| Longest record not blocked (s) | 653.0, 406.3 | 699.9, 699.9 | 484.6, 402.0 |
| Records | 71, 74 | 88, 87 | 89, 88 |
| Median time of a record (s) | 74.8, 94.5 | 68.2, 72.8 | 68.3, 80.4 |
| p95 time of a record (s) | 417.3, 361.8 | 247.7, 248.2 | 264.2, 315.2 |
| Completed orders | 4,497, 4,549 | 5,608, 5,484 | 5,062, 5,006 |
| Mean order wait (s) | 786.7, 789.8 | 634.9, 654.2 | 647.2, 666.9 |
| Longest order wait (s) | 5,174, 4,735 | 3,370, 3,632 | 4,906, 4,270 |
| Empty distance (km) | 17,289, 14,771 | 21,201, 19,200 | 20,575, 18,687 |
| Fault wait (pod-ticks) | 284,564, 314,699 | 317,065, 289,951 | 479,070, 494,687 |
| Delayed riders (rider-ticks) | 233,437, 330,079 | 272,501, 269,714 | 422,671, 427,924 |
| Settle (s) | 1, 702 | 1, 1 | 1, 771 |

- N lowers the empty distance by 14.6, 9.4, and 9.2 percent.
- In each seed the median time of a record is longer with N, by 4.6 to 19.7 s.
- In seeds 2 and 3, N completes 2.2 and 1.1 percent fewer orders, and the mean wait is 3.0 percent longer in each.
- The +41 percent of delayed riders in seed 1 is not there in seeds 2 and 3.
- The settle is 702 and 771 s in two seeds, and 1 s in the other and in each control.
- No run has a demotion, and each run completes its clear.

So N saves empty distance, and it costs some service in two of three seeds.
The maintainer decided on 2026-11-24 to keep the record and not to pursue N.
The cause of the long settle in seed 3 is [below](#the-settle-of-n).

## A second late record

The control of seed 2 is late on record `i1.17`.
`express-08` was bound, with the wait "Junction traffic", and blocked by the traveling `london-pod-148`, from tick 50804.
It waited 699.9 s not blocked by a fault, against the 600 s limit.
N does not change it.
A trace of 2026-11-24 ran seed 2 at `755c6455` to 45 simulated minutes, with and without option A.
The two traces of the record, its blocker chain, and the owners of the next request of each pod, each 20 s, are the same, so option A does not act on this record.

| After the start | Chain from `express-08` to its head |
| --- | --- |
| 7 s | 12 pods on link 100 toward Waterloo, each with "Pod ahead" of the pod in front |
| 87 s | 12 pods, the head in a station road |
| 167 s | 11, the head waits for a Waterloo berth where a pod has boarded for Bank |
| 247 s | 9 |
| 327 s | 8 |
| 407 s | 6 |
| 487 s | 4 |
| 567 s | 2 |
| 687 s | Unloading at Waterloo |

- The emergency pod is in the queue of link 100 toward Waterloo from its start, with 12 pods in its chain at the first sample, and the chain is down to 2 pods 560 s later.
- The owners of the next request of the emergency pod are occupied track of the pod ahead, at each sample.
  No claim is in the chain of the samples.
- The head of the chain waits for the junction of the Waterloo entry or for a berth that a loaded pod holds.
- The soak reported the last wait, "Junction traffic" behind `london-pod-148`.

So this is a capacity queue, about 1 pod a minute through the entry of Waterloo, and it is the same kind of wait as record i1.18 of the shared-track preset, which led to queue promotion.
Queue promotion is in the code of this run, and it did not shorten the queue enough.
The [qualification](../contracts/express-wharf-qualification-proposal.md) names this queue of link 100 as a known risk of HX10 and HX11.

## The settle of N

A trace of N on seed 3, with the same seed and the same 771 s, lists each remaining wait each 30 s.
The only one is the fault hold of `london-pod-025`, which drives its recovery route to Liverpool Street at 14 m/s and is never blocked in the 25 samples.
The gate waits for that hold, so the settle is the length of that drive.
It is not a block, and the gate has 1,800 s.
Seed 1 was not traced.

## The release form

The release form ran on seeds 1 and 2 at `ac75dd5f`, with option A and the full check at each tick.
`i4.93` is not late, seed 1 passed, and `i1.17` of seed 2 is late with the same 699.9 s.
The numbers are in the [qualification](../contracts/express-wharf-qualification-proposal.md#evidence).

## What it does not show

- Another seed, another preset, or another incident arm.
- Any contract question: option A narrows the decision of October 4, 2026, and clauses of the suspension contract.
- A test that a claimant that yields also makes progress in each case.

## Reading

Option A is sufficient for `i4.93` in this run.
The wait of A is one that the existing yield already ends for an ordinary trip.
N is a separate change of ordinary traffic with a clear benefit in empty distance and an unexplained cost in three measures.
It needs its own seeds before a decision.
