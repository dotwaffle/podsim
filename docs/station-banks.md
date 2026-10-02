# Independent station banks

A bank owns a nonempty set of station berths and its own entry and exit gates.
One station can contain up to eight banks.
Each berth belongs to exactly one bank.
Passenger station IDs and the flat berth list stay unchanged.
Separate `ParkingOnly` stations provide storage.

Each bank has a direct through lane and isolated arrival and departure paths.
The validator checks every legal local path, including alternate paths.
It rejects shared local lanes, shared interior nodes, cross-bank paths, and unused local lanes.
External roads can share a merge.
Gates, track cells, berths, and merges retain ordinary resource control.

An approach selects the reachable bank with the shortest free-flow external travel time.
Exact ties use the minimum berth load, then bank order.
A fixed destination berth uses its owning bank.
Terminal berth choice and station buffers retain the entry already installed in the route.
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

Projects without `Banks` retain version 1 and their existing behavior.
Banked projects require version 2.
An explicit null or empty `Banks` member is invalid.
Version 1 rejects any explicit `Banks` member.
Both project readers and nested transport decoders bound banks and berth membership before typed allocation.

Banked sessions write saved-state version 5.
Versions 2, 3, and 4 require project version 1.
Version 5 requires project version 2 and supports existing buffer fields and fixed entry certificates.
No saved pod bank field is added.
Restore infers the bank from retained gates, local lanes, and berth assignments.
A retained route that disagrees with its bank rejects the file before either restore tier.
Existing missing-route and route-budget demotions remain.
Physical restore retains position tolerances, resets speed, and rebuilds ordinary reservations.

All sessions send stream hello version 2.
Updated clients accept hello versions 1 and 2.
Version 1 topology cannot contain banks.
Older clients reject hello version 2.
Geometry changes start a new stream chain and send the full network.

This fixture does not change any default layout or controller setting.
Independent gates do not establish a service improvement.
See the [station diagnosis](station-service-diagnosis.md) for the measured pickup delays and prior geometry failures.

The [matched bank service screen](station-banks-screen.md) measures the implemented gates against the frozen heavy workload.
