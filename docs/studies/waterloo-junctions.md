# Waterloo junctions on the wharf preset

A scratch study of 2026-11-25 for BD-D3 of [berth displacement](../design/berth-displacement.md).
It asks which junctions hold the head of the [link 100 queue](link-100-queue.md), and what freeing them gives.
It qualifies nothing, and no code, layout, rule, or gate changes with it.

## Method

The runs are the `incidents` arm on `london-full-shared-express-wharf`, 6 simulated hours and the clear, seeds 1, 2, and 3, with no reference run, on the code of `58368811`.

- JS-1 is the control with a read-only probe once a second, for the 21,600 s before the clear.
  The head is the pod bound for Waterloo on link 100 or on its station road that is nearest to the station.
  A stopped head has a speed below 0.05 m/s.
  The probe records the first resource of the next reservation that another owner has, after the grants of the tick.
- JS-2 frees one side of the station.
  Entry free leaves only berth resources on the `road-in` lanes, `access-in`, and the arrival lanes.
  Exit free does the same for the departure lanes, `access-out`, and the `road-out` lanes, and drops the node and junction resources at the start of the links that they feed.
  These arms are unsafe: the Safety observation and the D6 oracle are off in them, so they are not buildable.

The JS-1 runs have the completed orders, the longest record, and the late counts of the recorded controls.
A run takes 12 to 24 minutes.

## What holds the head

| Measure | Seed 1 | Seed 2 | Seed 3 |
| --- | ---: | ---: | ---: |
| Head stopped (s) | 10,740 | 10,017 | 10,753 |
| Held by another owner (s) | 9,683 | 9,983 | 10,265 |
| First owner holds a junction or node (s) | 7,087 | 7,330 | 7,238 |
| First owner holds a berth (s) | 2,490 | 2,547 | 2,914 |
| First owner holds track (s) | 106 | 106 | 113 |
| Express heads, of the stopped seconds | 94% | 88% | 93% |
| Entries a minute through `access-in` | 1.75 | 1.92 | 1.91 |

- The junctions and nodes of the entry side hold the head.
  In order, the main ones are `01-arrival` (2,501, 2,041, 2,111 s), `02-arrival` (1,219, 1,328, 1,230 s), `entry`, `diverge`, and `03-arrival`.
- In 91 to 93 percent of these seconds, the owner goes to Waterloo.
  Most owners are on the arrival links and the berth inlets, and the exit lanes are owners in few of them.
- In seed 1, a pod with a fault on `02-arrival-link` is the owner for 588 of the 7,087 s.
- The probe names the first owned resource in route order, so a later one that is also owned does not count.

## What freeing them gives

Each cell has seeds 1, 2, and 3.
The change is against the control of the same seed, in percent.

| Arm | Late | Completed orders | Change | Mean wait (s) | Longest record not blocked (s) | Head stopped (s) | Entries a minute |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Control | 0, 1, 0 | 4,640, 5,608, 5,062 | - | 762.3, 634.9, 647.2 | 462.1, 699.9, 484.6 | 10,740, 10,017, 10,753 | 1.75, 1.92, 1.91 |
| Entry free | 0, 0, 0 | 4,713, 5,834, 5,692 | +1.6, +4.0, +12.4 | 766.5, 501.6, 509.5 | 324.4, 325.5, 410.7 | 505, 0, 0 | 1.80, 2.09, 2.09 |
| Exit free | 0, 0, 0 | 4,679, 5,780, 5,612 | +0.8, +3.1, +10.9 | 749.6, 471.1, 512.4 | 410.1, 452.2, 427.9 | 7,955, 6,206, 8,156 | 1.78, 2.02, 2.06 |
| Both free | 0, 0, 0 | 5,145, 5,654, 5,060 | +10.9, +0.8, 0.0 | 649.9, 542.1, 680.4 | 464.6, 337.8, 513.3 | 0, 1,077, 1,070 | 1.93, 2.02, 1.88 |

- Entry free removes nearly all stops of the head, and entries rise by 3 to 9 percent.
  So the junctions make the stops, but they are not shown to limit the rate of the station at this load.
- Both free completes less than entry free alone in seeds 2 and 3, and more in seed 1.
  Removing more conflicts cannot cause that by itself, so the spread of trajectories is about as large as the effects, and the completed orders show no clear gain from an arm.
- All nine runs have no late record, and so do seeds 1 and 3 of the control.
  The control has one late record in three runs, so the absence of it in an arm does not show that the arm removes the cause.
- Entry free has a lower longest record than the control in each seed, which is the clearest effect.
  It is an upper bound that no buildable layout can give.

## What it does not show

- A buildable change, its gain, or its cost.
- The cause of the stops, in the resources of one reservation: whether an Express pod reserves the arrival comb as one movement was not measured.
- A second bank at Waterloo under this load.
  The bank study has one seed of a Compact flood, and a layout conflict with link 100.
- The spread of the control: each seed has one run, and the runs of one seed are deterministic.
- The release form, the `emergencies` arm, or another preset.
