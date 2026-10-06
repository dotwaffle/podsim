# Pickup tails after the berth-routing fix

Four selected requests retain longer waits with pickup reassignment.
The captured pickups mostly move from different starting stations.
Every captured route avoids intermediate berths, and the earlier Stratford cycle does not appear.
These cases do not establish a new movement defect or justify a reservation change.

## Replay and limits

The frozen source is `a54281e`.
Both Full AM Peak arms use 12 requests per minute, seed 4, six hours of demand, and a seven-hour cap.
The queue limit is 1,000,000, and every scheduled request enters both arms.
Buffers, sharing, redistribution, and predictive routing remain off.
Virtual platoons use four pods.
Only one arm enables pickup reassignment.

Each replay exactly matches its prior `89e1e1d` result, schedule, and inherited qualification-check fields.
Read-only snapshots add four target histories without changing the measured simulation.
Checks retain once-per-second safety, speed, and unique-order accounting.
Both arms restore physically at three and six hours, with no demotion, requeue, or dropped orders.
Each restored copy runs 60 seconds of dense checks with new controller admissions disabled.
The saved platoon records also match their round trip.

Requests 3651, 2175, and 3421 are the three largest exact regressions in the prior post-routing pair.
Request 1324 retains the [earlier Stratford witness](berth-routing-service.md#stratford-request-1324).
Selection follows outcomes and does not estimate population behavior.
All four requests complete in both arms.

## Matched waits

Times below use simulated seconds.
Stopped counts sample an assigned traveling pod below 0.01 meters per second once per second.
They are boundary observations, not exact blocked durations.

| Request | Journey | Baseline wait | Reassignment wait | Added wait | Stopped pickup samples, baseline / reassignment |
| ---: | --- | ---: | ---: | ---: | ---: |
| 1324 | Leyton to High Street Kensington | 446.30 | 664.10 | 217.80 | 0 / 1 |
| 3651 | Amersham to Farringdon | 1,191.95 | 2,821.68 | 1,629.73 | 1 / 1 |
| 2175 | Rickmansworth to Green Park | 557.63 | 2,115.52 | 1,557.88 | 0 / 0 |
| 3421 | Rickmansworth to Waterloo | 0.00 | 1,453.35 | 1,453.35 | 0 / 1 |

No selected pending sample reports an occupied-berth wait.
Each reassignment case has 29 unassigned boundary samples before its sampled pickup assignment.
The bounded finishing-pod hold can explain that interval, but it does not explain the remaining long moving pickup.
Snapshots retain the hold's diagnostic text without observing its exact decision event.

## Pickup routes and local decisions

Request 3651 uses pod 215 from Northwood in baseline and pod 174 from Great Portland Street with reassignment.
The captured routes contain 22 and 52 lanes, respectively.
Request 2175 uses pod 216 from Moor Park in baseline and pod 063 from Warren Street with reassignment.
Their captured routes contain 22 and 49 lanes.
Neither request has a direct experimental reassignment record.
Their changed fleet histories precede these pickups.

Request 1324 boards pod 044 in baseline and pod 112 with reassignment.
The candidate's pickup starts at Tower Hill.
Its captured route includes only the endpoint berth nodes, and the previous intermediate-Stratford wait is absent.
This preserves the narrower [post-routing result](berth-routing-service.md), without claiming that all future cycles are impossible.

Request 3421 boards local pod 223 immediately in baseline.
The reassignment arm first sends pod 113 from Oxford Circus, then replaces it with pod 235 from Watford.
The recorded replacement occurs when the request is already 914.02 seconds old.
Its remaining estimate decreases from 1,222.82 to 539.26 seconds.
The request boards 539.33 seconds later.
The replacement therefore has a useful local prediction, while the independent baseline already supplied an immediate pickup.
This replay does not establish when the original assigned pod would have arrived without replacement.

The moving histories support further dispatch and fleet-availability work.
They do not prove an optimal assignment, identify every earlier divergence, or establish that empty-pod coupling has no effect.
No production fix follows from this diagnosis.
Pickup reassignment and buffers remain off by default.
The [exact fleet-history intervention](pickup-fleet-ablation.md) confirms that an early swap changes selected later pickup tails.
Suppressing that pair does not establish a general dispatch fix.

## Evidence

[Target totals](measurements/pickup-postroute-tail.csv) retain waits, boarding pods, and sample denominators.
[Metadata](measurements/pickup-postroute-tail.json) retains source hashes, exact replay checks, decisions, episodes, and captured route IDs.
Raw snapshots and frozen helpers remain in `~/.cache/agents/podsim/pickup-postroute-tail-20261001/`.
The owned run stopped successfully, and its scratch binary was removed.
Concurrent functional runs provide no CPU comparison.
