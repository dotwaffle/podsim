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
Banked imports, edits, and simulation admission check the full network against the existing 12-meter nonincident clearance.

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

Sessions send stream hello version 6 for every project kind.
Clients reject every other hello version.
Geometry changes start a new stream chain and send the full network.

This fixture does not change any default layout or controller setting.
Independent gates do not establish a service improvement.
A diagnosis of the earlier Rail Hub workload (24 passengers for each event, 288 offers) shows that 77% to 79% of sampled pending request time has no eligible pod selected.
The earlier two-bank failures came from an unconnected road and a bank near-crossing.
A matched screen on the heavy Rail Hub workload (120 passengers for each event, 1,440 offers for each seed) completes 725 journeys against 729 for the baseline, summed over two seeds.
Pickup p95 increases in both seeds, so defaults stay unchanged.
