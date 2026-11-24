# Station pod limit

Status: the maintainer decided SP-D1 A, SP-D2 A, and SP-D3 A on 2026-11-24.
Nothing is built.
The [pilot of C1](#pilot-of-c1) is complete, and what follows from it is not decided.

This note is about the block on link 100 in the [shared track batch](../studies/shared-track-batch.md#the-block-in-lf).
The study calls it option C: a limit on the pods that go to a station.

## What the records show

- `LF` blocks at the full hub stream and at 1 in 4 of it, and does not block with no hub stream.
- The closed wait has 127 to 130 pods, almost all on the 4 lanes of link 100, with 31 on each lane.
- At minute 60, 106 to 110 pods had one of the 5 Waterloo berths as their destination.
  72 to 74 of them were not yet on link 100 toward Waterloo or in its station lanes.
- Bank has 2 berths, and it loses berths to pods that cannot leave for Waterloo.
- The pilot of fix A removed the refusal of free berths, and `LF` still blocked in each seed and completed fewer orders.
- `S0` ends in a different state, with no closed wait found, and its cause is unknown.
  This note does not cover `S0`.

The [station limits](../studies/station-limits.md) study found that junction zones, and not berths, limit King's Cross and Waterloo under a flood to one station.
A limit on pods does not raise that rate.
It can only stop more pods from entering a queue that cannot drain.

## Where a pod gets its destination

1. A pickup: `assignPickupPod` calls `sendTripPickup` (`internal/sim/dispatch.go`).
   It sets the pod and the destination at dispatch, with no claim and no count of the other pods that go there.
2. A loaded trip: `boardOrWait` and `board` set the station of the trip.
   The pod then waits in its berth for track.
3. Parking and guarded positioning: an empty pod that is idle moves to parking.
   These moves are not the cause in the records.

Only a pickup can be refused with no cost to a rider already aboard, because the trip can wait in the queue with no pod.

## Options

| Option | Rule | For | Against |
| --- | --- | --- | --- |
| C1 | Dispatch sends a pickup only while the pods bound for the station are fewer than `k` times its berths. A pod counts while its destination is the station and it has not left it. The trip stays in the queue with a new reason, and the next pass tries again. | Small. It is a count of existing state, so it adds no saved state and the restore needs no rule. It removes the 72 to 74 far pods from the queue. | It does not limit loaded trips. A loop of loaded pods can still close. The queue reaches its limit sooner, and the session then refuses orders. |
| C2 | A boarded pod departs only while the pods bound for its destination are fewer than `k` times its berths. | It limits the loaded side of the loop. | A pod that waits in its berth takes a berth from the station, so it can close a loop between two stations that wait for each other. It needs a proof that some pod can always move, and a rule for emergencies, faults, and the standee dwell. |
| C3 | C1 and C2 | Covers both sides. | Both costs, and the proof of C2. |
| D | The order is refused, or held, when its hub stream is over the rate that the hub has. | No pod state. | It changes the demand, not the traffic, so it hides unmet demand. The brief says that the simulation shows it. |
| E | Layout: more berths or a second bank at Bank. | It raises the capacity, and D does so with its two banks at each hub. | The preset geometry changes, and each row of its contract opens again. The batch shows that D finishes. |
| F | Accept the block as a limit of `LF` under this stream. | No code. Adding no vehicles and hiding nothing fits the brief. | Background orders block too, which is the cost that the study measured. |

A limit on the destinations of a station is close to the claim rules of the [berth claim deadlock](../contracts/berth-claim-deadlock-proposal.md), and C2 can make the same kind of loop.
So C2 needs the review of that proposal first.

## Recommendation

Pilot C1 alone, as an experimental policy that is off by default, and only on the `LF` workload.
It is the smallest change, and the records name the pickup pods as the largest part of the queue.
If C1 does not stop the block, the remaining loop is of loaded pods, and E or F is the honest choice.
Do not build C2 before its proof.

The pilot answers these questions:

1. Does `LF` finish at 1 in 4 of the hub stream, where it blocks at minute 94 today?
2. Does the number of pods bound for Waterloo stay at `k` times 5?
   The first `k` to try is 3.
   It is a guess, and the pilot tries 2 and 4 too.
3. What does it cost on the [individual limits](../contracts/individual-limits-amendment-proposal.md), and how many orders does the queue limit refuse?

A pilot of 6 runs or fewer fits the study brief cap: `k` of 2, 3, and 4 on seed 1, and the best of them on seeds 2 and 3, against the runs of the study.
A failure of any safety check stops it.

## Pilot of C1

The pilot of 2026-11-24 used 6 runs of `LF` at `21f5ec3a` and `60ec75d2` (the simulation code is the same), with a scratch patch that did not land.
The patch refuses a pickup when the pods whose destination is the station are at least `k` times its berths.
It does not refuse a pod that is at the pickup or that already goes to the station.
The workload is the screen in `am-peak` with `-hub-mean 60s`, a hub stream at 1/4 of the rate of the batch.
It is not the same event set as the 1 in 4 of the probe.
One run of seed 1 has no limit.

| Seed | `k` | Submitted | Completed | Outstanding | Refused events | Mean background wait (s) | Mean hub passenger wait (s) | Most pods bound for Waterloo | Blocked pod-seconds |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | None | 1,188 | 866 | 322 | 271 | 741 | 854 | 107 | 3,184,946 |
| 1 | 2 | 1,531 | 1,531 | 0 | 0 | 315 | 1,619 | 18 | 109,307 |
| 1 | 3 | 1,531 | 1,531 | 0 | 0 | 317 | 1,645 | 19 | 168,180 |
| 1 | 4 | 1,531 | 1,531 | 0 | 0 | 324 | 1,624 | 25 | 216,199 |
| 2 | 2 | 1,603 | 1,603 | 0 | 5 | 310 | 1,833 | 18 | 120,877 |
| 3 | 2 | 1,540 | 1,540 | 0 | 0 | 273 | 1,026 | 18 | 104,158 |

- No run has a safety failure.
- The first question has a yes in each patched run: `LF` finishes, and the run with no limit blocks in seed 1.
  Seeds 2 and 3 have no run with no limit at this rate.
- The second question has a no: the pods bound for Waterloo pass 10, 15, and 20, because loaded trips have no limit.
  The block does not form at these counts.
- The three values of `k` are too close to separate in one seed.
- The waits of the run with no limit cover only the requests that boarded, so they are not comparable with the others.
- The hub line from Waterloo to Bank stays short of capacity: its parties wait 1,317 to 2,250 s on average in the three seeds at `k` of 2.
  The limit stops the block and adds no capacity.
- The third question is open.
  The run with no limit blocks, so no pair gives the individual limits, and the full hub stream is in the next section.

The patch counts the pods for each refused send, 10.9 to 23.1 million sends in a run, so a build needs a cached count.

### The full hub stream

Three more runs of 2026-11-24 use the same patch with `k` of 2 and the full hub stream of the screen.
The `LF` runs of the [batch](../studies/shared-track-batch.md#service) are the runs with no limit, and they are at an older commit.

| Seed | `k` | Completed | Outstanding | Refused events | Background boarded | Mean background wait (s) | Out, mean party wait (s) | Out, passengers a minute in the window |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | None | 368 | 343 | 1,063 | 426 | 1,234 | 1,085 | 0.5 |
| 1 | 2 | 1,334 | 0 | 534 | 923 | 439 | 4,307 | 5.7 |
| 2 | None | 431 | 327 | 1,040 | 476 | 1,156 | 1,227 | 0.8 |
| 2 | 2 | 1,350 | 0 | 558 | 921 | 394 | 4,287 | 5.6 |
| 3 | None | 361 | 342 | 1,141 | 398 | 1,057 | 1,096 | 0.5 |
| 3 | 2 | 1,374 | 0 | 570 | 934 | 432 | 4,171 | 5.5 |

- No run has a safety failure, and `LF` finishes each accepted order in each seed.
- The session still refuses about half of the events that the run with no limit refuses, at the queue limit of 200.
- The waits of the runs with no limit cover only the requests that boarded.
- `D` and `S` move 24.8 to 29.0 passengers a minute out, and `LF` with the limit moves 5.5 to 5.7.
  So the limit does not replace an Express line.
- Each run took 660 to 770 s of wall time, with the uncached count of the patch.

## Decisions for the maintainer

| ID | Question | Options | Recommendation |
| --- | --- | --- | --- |
| SP-D1 | What to do about the `LF` block | A: pilot C1. B: option E as a layout study. C: option F. | A |
| SP-D2 | What counts as bound | A: each pod whose destination is the station, pickups and loaded. B: empty pickups only. | A, because the loop holds both |
| SP-D3 | How the pilot is done | A: an uncommitted patch, as the pilot of fix A. B: a policy flag in the project format. | A |
