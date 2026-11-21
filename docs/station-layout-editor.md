# Station layout controls

Select a station in the editor to show its layout controls.
The controls support straight rectangular berth chains, including generated London and scale100 stations.
They derive dimensions from the node coordinates already in the project.
They do not add project fields or change default geometry.

| Control | Effect | Limit |
| --- | --- | --- |
| Berth pitch | Keep the first row fixed. Move later complete rows along the existing station axis. | At least two rows and 25 m pitch. |
| Entry/exit spacing | Move the entry, exit, arrival column, and departure column symmetrically. Keep berth centers fixed. | At least 48 m across the mouth. |
| Approach setback | Move the station mouth and all berth rows away from the fixed mainline throat. | Positive distance and a paired, aligned throat. |

Enter one or more dimensions, then select **Preview in draft**.
The map shows the change, and one undo restores all changed dimensions.
The running simulation changes only after **Pause and apply**.
Export and import store ordinary node coordinates.
The edit retains lane IDs, station roles, separation groups, speed limits, berth IDs, and fleet placements.

Curved lanes, irregular rows, shared row nodes, and nonuniform pitch require manual node editing.
A one-row station has no berth-pitch control.
A station without a recognized throat has no approach-setback control.
The controls show the reason when the layout is unsupported.

On a station with [independent banks](station-banks.md), select a bank in the **Bank** list first.
The controls then change only that bank, and the editor refuses a layout change for the whole station.
A bank also has **Approach length** and **Departure length** controls, each at least 24 m.
See [independent station banks](station-banks.md#browser-editing-example) for these controls and the bank membership controls.

A rejected preview leaves the draft unchanged.
Every lane with a moved endpoint must remain at least 24 m long.
A slow, straight small-class lane that the retention rules of `internal/sim/retention.go` certify needs only twice its reduced retention.
The preview runs the network check of the server, which checks each pair of lanes for crossings and gaps, including the approach lanes.
The clearance of each pair comes from the [shape clearance](reference.md#shape-clearance) rule, so a certified pair can come closer than 12 m.
A station that a depot uses has fixed geometry and berths, so the controls refuse it.
Lanes that share a node use the existing junction control.
Distinct nonempty separation groups represent separate planes when the lanes share no node, as in the simulator.
A lane pair with a large class also needs distinct planes for their incident lanes and for the berths at their From nodes, as in the server bank audit.
The preview reports a conflict that only this rule gives after its other server checks, as the server does.
The edit never changes a separation group to avoid a rejected crossing.
All coordinates must remain within the existing project limit.
The full project also passes through the editor's asynchronous validation before application.

Static checks do not qualify arbitrary saturated layouts.
Shorter paths can reduce service time while worsening junction contention or individual waits.
Use matched demand studies before adopting a layout for a workload.

## Validation record

Editor tests cover rotated and mirrored chains, one-row controls, atomic rejection, mutation ownership, and exact 24 m and 12 m boundaries.
Chrome checks cover selection changes, combined preview, focus, undo/redo, and import/export.
Native pilots use edited LondonCentral, LondonFull, and scale100 projects with buffers off and on (station buffers were removed on October 7, 2026).
Each arm has 40 initial mixed station orders and 12 balanced offers per simulated minute.
All six arms pass separation, lane-speed, and berth checks on every tick for 120 simulated seconds.
Each arm also passes three physical restore checkpoints with no demotions, requeues, or drops.
The pilots verify traffic on the edited station's berth-access and departure lanes.
