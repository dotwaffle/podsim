# Disabled compact continuation diagnosis

Source: `53321e4c5decc6c661df64a40378ee37614dcffc`.
This report completes roadmap item 2c.
The [loaded screen](compact-queue-speed-fixed-screen.md) retained ten incomplete disabled continuations at a fixed 144,000-tick cap.
They show finite-window exhaustion, without evidence of a frozen fleet or a retained compact controller at the endpoint.
No native fix is identified by this diagnosis.
The original bounded drainage checks remain failed.

## Evidence and limits

The diagnosis reads all ten archived endpoints and traces two representative original continuations.
It does not extend a cap, change a workload, or repeat the full restore matrix.
The ordinary trace starts at tick 18,000.
The compact trace starts at the first certificate, tick 30,159.
Both traces use the original fixture, physical restore, and disabling sequence.
They turn spacing ordinary, platooning off, and station buffers off.
They submit no new offers.

| Trace | Endpoint tick | Completed / accepted | Last completion | Last boarding | Occupied pods moving at endpoint |
| --- | ---: | ---: | ---: | ---: | --- |
| Variation 1 ordinary, fixed 300 seconds | 162,000 | 4 / 6 | 132,971 | 110,716 | 05, 06 |
| Variation 1 compact, first certificate | 174,159 | 5 / 10 | 165,640 | 165,640 | 02, 04, 05, 06 |

All active occupied pods moved on the final traced tick.
During the final 120 seconds, passenger distance increased by 600 meters in the ordinary trace and 1,200 meters in the compact trace.
Empty distance increased by 2,013.008 and 600 meters, respectively.
The compact trace completed and boarded a request only 8,519 ticks before its endpoint.
Neither trace has an endpoint resource blocker on an occupied pod.
Their remaining routes and authored low-speed station lanes explain continuing travel within the finite window.

The compact certificate remains through tick 30,232 and disappears at tick 30,233.
It does not remain for the full continuation.
Existing buffer membership also drains, by ticks 110,163 and 110,558 in the ordinary and compact traces.
Neither trace retains a virtual platoon at the endpoint.
Disabling new admissions therefore preserves recovery without freezing these accepted orders.

## Check of the other archived endpoints

Every incomplete endpoint has an occupied traveling pod strictly inside its current lane cell.
The [measurement](measurements/compact-disabled-continuation-diagnosis.json) identifies a witness for each of the ten endpoints.
Its current-cell margin is positive, and it has no saved compact certificate, station-buffer membership, or platoon link.
The original screen checked owner and stopping bounds on every continuation tick.

The native [cell builder](../internal/sim/traffic.go) gives these straight lanes equal cells using `laneBlockCount`.
The native `move` function keeps the current owned cell when a future grant fails.
Inside that cell, positive acceleration permits a positive move even from zero speed.
A future resource denial cannot revoke that current cell.
The endpoint witness therefore refutes an all-pod static deadlock at each original cap.
This source argument does not execute another tick or assume a saved speed field.

The two native traces establish their complete movement and blocker histories.
The remaining endpoints use the saved-cell witness and prior owner checks, not a new full-length trace.
Neither method proves eventual completion of every order, per-order fairness, or acceptable service delay.
Those claims remain unqualified.

## Verification and disposition

The traces check 288,000 native ticks for safety, lane speed, acceleration, owned stopping, resource ownership, conservation, and seat bounds.
They reproduce both archived final `SavedState` objects exactly.
An additional 7,200-tick observed/control comparison matches exported state and the complete native snapshot on every tick.
Fresh Go diagnostics and package vet pass for the private observer.
No production Go file changes.

The first trace run reached both exact endpoints but failed when JSON tried to encode private link fields.
The observer now exports named link values.
The corrected run passed, and both logs remain in the evidence archive.
This correction changed observation output, not native behavior.

Keep the original service and bounded-drain failures visible.
Do not extend these caps to seek a passing result or weaken the drainage requirement.
A later service change needs a separate hypothesis and a frozen matched workload.
This diagnosis supplies no bounded native fix proposal because it finds no native fault to fix.

Private source, hashes, gate receipts, sampled resource traces, and analysis reside in `/home/dotwaffle/.cache/agents/podsim/forecast-contract-batch-20261003/root`.
The archived fixture and restore receipts remain under the preceding speed and energy evidence directory.
