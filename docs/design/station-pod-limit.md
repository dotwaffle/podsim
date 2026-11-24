# Station pod limit

Status: the maintainer decided SP-D1 A, SP-D2 A, and SP-D3 A on 2026-11-24.
Nothing is built, and the pilot of C1 is not recorded yet.

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

## Decisions for the maintainer

| ID | Question | Options | Recommendation |
| --- | --- | --- | --- |
| SP-D1 | What to do about the `LF` block | A: pilot C1. B: option E as a layout study. C: option F. | A |
| SP-D2 | What counts as bound | A: each pod whose destination is the station, pickups and loaded. B: empty pickups only. | A, because the loop holds both |
| SP-D3 | How the pilot is done | A: an uncommitted patch, as the pilot of fix A. B: a policy flag in the project format. | A |
