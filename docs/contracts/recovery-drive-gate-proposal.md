# Recovery drive gate proposal

Status: draft for the maintainer, 2026-11-25.
Nothing of it is in the code, and no gate is relaxed by this note.

## The problem

The gate "Progress after the clear" ([suspension contract](incident-suspension-contract-proposal.md), section 17) asks that no pod has `faultHold` within 30 simulated minutes of the last clear.
Decision S2 and stage 1, section 9.5, make an evacuated pod on a lane drive its original route, empty.
Its `faultHold` ends only at the arrival.
So the gate fails for a pod with more than 30 minutes of route left at the clear, however well the traffic moves.

The [trace](../studies/link-100-queue.md#the-progress-miss) has such a pod.
`london-pod-056` had 31,527 m left, which takes at least 2,252 s at 14 m/s, and the gate has 1,800 s.
It drove at 14 m/s in 177 of 180 readings and was never stopped in a reading.
It had 6,625 m left in the last one, so its arrival was not seen.
[N](../studies/emergency-claim-pilot.md#the-settle-of-n) had a wait of the same kind, 771 s long.
The gate compares a property of the last faults of a run with a fixed time, so the result can change with any change of the trajectory.
The study has one miss of this kind in 24 runs of 6 hours (4 presets, 2 arms, and 3 seeds), so a rate is not known.

## Options

| Option | Rule | For | Against |
| --- | --- | --- | --- |
| A | The harness gives each empty recovery that has no fault record after the last clear command a frozen deadline of its own, defined below. All other parts of the gate keep 1,800 s. | A pod that does not arrive by its deadline fails. No change of the simulation and no constant other than the 1,800 s. | It amends the gate. A run is longer when such a pod exists. |
| B | The gate does not count a fault hold of a pod that moves with no wait reason. | Shortest change. | It checks one reading. A pod that moves and then stops for good, or that never arrives, passes. |
| C | An evacuated pod goes to the nearest berth that it can use, and not to the old destination. | The pod returns to service sooner, with less empty distance. | It amends S2, decision 11, and section 9.5, and it changes every run with a lane evacuation. Not measured. It makes the miss rarer and does not remove it. |
| D | The hold of an evacuated pod ends at the clear. | No long hold. | The pod is on a lane with a purpose and no hold, against W5. It needs a new rule for service from a lane. |
| E | No change. | No work. | A run can fail for a reason that no rule of the traffic can change, and each one needs a trace. |

## Option A in detail

1. After all clear commands of the settle, the harness makes a list of the pods with purpose 3, owner `faultHold`, and no active record.
   The list and the deadlines do not change after that.
   A reroute or a change of state gives no more time.
2. The baseline of a pod is the sum, over the lanes left, of the rest of the lane length divided by its speed limit.
   For a route that ends at a station entry, it includes the shortest reachable and compatible berth suffix at that time.
   The baseline is rounded up to ticks.
   It is a lower bound, and not a guaranteed time: acceleration, braking, waits, and later changes use the 1,800 s.
3. The deadline is the baseline plus 1,800 s.
4. The obligation of a pod ends with its arrival at a berth and the release of its hold after that.
   A change of purpose or owner is not an arrival.
5. The existing predicates, without the holds of the listed pods, are latched together by 1,800 s, as today.
   The open-emergency check stays at 1,800 s.
   The holds of other pods and every incident wait reason, also of a listed pod, stay in these predicates.
6. A missed deadline is recorded, and no later result removes a miss.
7. The samplers are off, so a new incident in the settle makes the run invalid.

## Recommendation

The recommendation is option A.
Option C is a product question of its own, with its own pilot.

The change must not turn an earlier failure into a pass.
The failed `far` run stays failed under the gate that it ran with, and HX11 does not change, because its miss is a late record and not a hold.

## What a build must check

- Unit tests of the deadline: no such pod, one pod, a pod that stops for good, a pod that arrives late, a reroute, a change of purpose, and a new incident.
- Tests that an emergency that is open at 1,800 s and ends later still fails, and that a late arrival does not remove another miss.
- A mutation that removes the deadline, and one that gives it to a pod with another purpose.
- The log line gives the deadline and the arrival of each such pod.
- The queue check and the incident wait reason still use 1,800 s, also while the run continues.
- The wall-time projection of the soaks in [operations](../operations.md).

## Decisions for the maintainer

| ID | Question | Options | Recommendation |
| --- | --- | --- | --- |
| PG-D1 | Which option | A to E above | A |
| PG-D2 | When | A: after the HX11 runs of the [distant berth target](distant-berth-target-proposal.md). B: before them. | A, so that no gate changes while a rule is under test |
