# Independent station banks

A bank owns a nonempty set of station berths and its own entry and exit gates.
One station can contain up to eight banks.
Each berth belongs to exactly one bank.
Passenger station IDs and the flat berth list stay unchanged.
Separate `parkingOnly` stations provide storage.

Each bank has a direct through lane and isolated arrival and departure paths.
The validator checks every legal local path, including alternate paths.
It rejects shared local lanes, shared interior nodes, cross-bank paths, and unused local lanes.
External roads can share a merge.
Gates, track cells, berths, and merges retain ordinary resource control.

An approach selects the reachable bank with the shortest free-flow external travel time.
Exact ties use the minimum berth load, then bank order.
A fixed destination berth uses its owning bank.
Terminal berth choice retains the entry already installed in the route.
An eligible empty pickup can change banks at its existing safe diversion anchor.
It keeps every reserved lane.

## Browser editing example

Generate the optional fixture:

```sh
mise exec -- go run ./cmd/scenario -preset independent-banks -output banks.json
```

Open **Edit scenario** and import `banks.json`.
Select **Hub**, then bank **b**.
Add a berth to that bank.
Set its row pitch, mouth spacing, approach length, or departure length.
Preview the edit and inspect **Checks** before applying it.
Export the project to retain topology and membership together.
Undo and redo restore both.

Imported topology or existing node and lane tools create new bank gates.
The bank membership control assigns existing berths to those gates.
It changes metadata and alias gates without generating a whole station.
The **Use legacy station gates** action removes metadata only when the resulting single-entry layout validates.

Row pitch must be at least 25 meters.
Mouth spacing must be at least 48 meters.
Setback must be positive.
Approach and departure lengths must be at least 24 meters.
Length edits need one straight lane and a dedicated external anchor with two incident lanes.
Shared anchors and curves reject the edit.
All changed lanes remain at least 24 meters long.
A slow, straight small-class lane that meets the retention rules in `internal/sim/retention.go` needs only twice its reduced retention.
Banked imports, edits, and simulation admission check the full network with the bank audit (`ValidateBankGeometry`).
The audit compares the positions of each pair of nonincident paths.
It requires 12 meters between two paths, or less on a lane that the retention rules certify.
Maneuver groups and depots (`internal/sim/depot.go`) use the same audit.
With a depot and no bank, the audit checks only the pairs with a depot lane or a slot.
The editor uses the same index (`sim.NewLaneClearance`), so its checks match the server.

## Depot lanes

Two paths of different lanes of one depot can be closer than 12 meters.
Only the lane paths of a depot, from the entry to the exit, and its slots can qualify.
A pair qualifies when its paths share no node, admit no large class, do not touch the node of a large berth, and have speed limits below 3.742 m/s (`reducedClearanceSpeed`).
A slot counts as a path at speed 0.
The audit sweeps a body along both paths, including the turns at their ends.
The body extends 2.5 meters behind the reference of the pod, 3.7 meters ahead, and 1.05 meters to each side, and it grows on each side for the higher speed limit `v` of the pair.
When the two bodies never meet, the pair needs `W(v) = 3.1 + v²/2` meters along its whole length.
Otherwise the pair needs 12 meters, and the audit error does not change.
`W` is a 1.6-meter body width, 0.25 meters of lateral tracking on each side, the 1-meter margin of the retention rules, and twice the stop distance at `v`.
At the depot speed of a 6-meter pitch (1.41 m/s), `W` is 4.09 meters.
The safety check requires `W(v)` between two pods on such a pair.
Debris on a depot lane also blocks each track cell of a qualifying neighbor that comes closer than 12 meters to the debris (section 7.2 of the incident suspension contract).

The rule assumes a guideway-bound pod.
The guideway keeps the reference of a pod within 0.25 meters of its lane and its heading at the heading of the lane.
Thus a guidance fault cannot turn a body out of its swept envelope, and a fault stops the pod on its lane.
This is an assumption of the model, not a derived result.
A pod that could turn freely would need a disk of radius 2.15 meters, and `W` would be `5.81 + v²/2` meters.
Every pair that is not a depot pair keeps its rule.

The ring depots of the generated scenarios put the first lane 12 meters from the through lane, because the through lane is fast, and each next lane 6 meters further out.
Their run lanes and throats run at 2 m/s.
This is the highest 0.1 m/s step at or below 2.3 m/s that the audit accepts for every legal layout.

A follow-up tried to win back depot exit time by letting the throats run fast.
Nested throats converge on the exit and never get 12 meters apart.
Only the first and the outer lane can run fast, and only after their run lanes climb outward.
On 8 lanes, lane 1 went from 44 to 32 s and lane 8 from 62 to 43 s, while the six middle lanes got 3 to 4 s slower.
The average gain is about 1.3 s.
The outer lane moved out from 54 to 66 meters, and the overload results did not change.
The change was not adopted.

## Formats and restore

Projects use version 1 with or without `banks`.
Projects without `banks` keep their existing behavior.
An explicit null or empty `banks` member is invalid.
The decoder refuses project versions 2 through 5 and does not migrate them.
Both project readers and nested transport decoders bound banks and berth membership before typed allocation.

Banked sessions write saved-state version 9, as every other session does.
Version 9 stores a version 1 project.
No saved pod bank field is added.
Restore infers the bank from retained gates, local lanes, and berth assignments.
A retained route that disagrees with its bank rejects the file before either restore tier.
Existing missing-route and route-budget demotions remain.
Physical restore retains position tolerances, resets speed, and rebuilds ordinary reservations.

Sessions send stream hello version 7 for every project kind.
Clients reject every other hello version.
Geometry changes start a new stream chain and send the full network.

This fixture does not change any default layout or controller setting.
Independent gates do not establish a service improvement.
A diagnosis of the earlier Rail Hub workload (24 passengers for each event, 288 offers) shows that 77% to 79% of sampled pending request time has no eligible pod selected.
The earlier two-bank failures came from an unconnected road and a bank near-crossing.
A matched screen on the heavy Rail Hub workload (120 passengers for each event, 1,440 offers for each seed) completes 725 journeys against 729 for the baseline, summed over two seeds.
Pickup p95 increases in both seeds, so defaults stay unchanged.
