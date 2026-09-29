# Station-summary fixtures

These are projections of physically restored simulation snapshots at tick 72000.
The extraction used simulation code from commit 50346e8.
The source projects match its LondonCentral and LondonFull preset networks.
The source demand seeds are recorded below.
Each fixture keeps all vehicles, their activity/speed/wait/current-lane fields, full ordered route lane IDs, and all berth states.
Tests resolve lane IDs against the unchanged preset network.
The projection omits fields that neither station-summary path reads.
It does not truncate or deduplicate routes.

Source sessions are retained in the external profiling evidence.
The extraction tool decoded each saved project and simulation, called sim.RestoreState,
required RestorePhysical, then projected Simulation.Snapshot.
Its source and execution log are archived in station-batch-20260929.

| Preset | Source session SHA256 | Demand seed | Vehicles | Berths | Fixture bytes |
| --- | --- | --- | --- | --- | --- |
| central | bc7e4fe5a27181720f8420ce528bc864700222034576ba8d6b207e875a8f273a | 20260922 | 114 | 228 | 5132 |
| full | 9fb7a07a02faf4fb5f501ddca28c78c9fc1bd99759ad47fb4a2a293fff91ea78 | 20260929 | 287 | 674 | 10107 |
