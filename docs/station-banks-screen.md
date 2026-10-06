# Independent station bank service screen

Independent gates reduce local traffic waits but do not give a consistent service gain in this two-seed screen.
The banked candidate completes 725 journeys across both seeds, compared with 729 for the baseline.
Pickup p95 increases in both seeds.
Keep defaults unchanged and do not advance this candidate to broader qualification.

## Matched workload

The heavy Rail Hub workload offers 1,440 passengers per seed, including 720 outbound train connections.
Each train event has 120 passengers.
Both layouts use the same offers, 30 pods, 200-request queue cap, schedules, speed limits, and 12-meter clearance.
Routing is free-flow.
Sharing, redistribution, buffers, and virtual platooning remain off.
The observation cap is three simulated hours, with offers ending after one hour.
Each arm stops after all accepted requests finish.

The candidate starts from the corrected access layout in the [station diagnosis](station-service-diagnosis.md).
It gives the hub two independent entry and exit gate pairs, each with three berths.
The existing external fork and merge remain shared.
Separate parking stations remain unchanged.
The candidate has 128 lanes and 48 berths across the network.
A full static audit finds no nonincident lane or berth hazards below the existing clearance.
It checks 7,566 eligible lane pairs using the exact movement polylines.

The light pilot completes 12 of 12 journeys and makes all six outbound connections.
Observer-on and observer-off results match exactly.
The final heavy baseline reproduces every frozen result field and passenger receipt in both seeds.
The raw measurement data is in git history.

## Service results

Times are seconds unless the table gives another unit.
Pickup and journey means and tails cover completed accepted passengers.
They exclude skipped offers.

| Metric | Baseline seed 1 | Banks seed 1 | Baseline seed 2 | Banks seed 2 |
| --- | ---: | ---: | ---: | ---: |
| Completed / offered | 363 / 1,440 | 365 / 1,440 | 366 / 1,440 | 360 / 1,440 |
| Skipped offers | 1,077 | 1,075 | 1,074 | 1,080 |
| Pickup mean | 2,688.47 | 2,667.03 | 2,645.46 | 2,758.49 |
| Pickup p95 | 3,862.03 | 3,877.88 | 3,768.27 | 3,915.82 |
| Pickup maximum | 4,135.50 | 4,153.00 | 4,217.33 | 4,232.02 |
| Journey mean | 3,013.25 | 2,999.03 | 2,960.85 | 3,087.08 |
| Journey p95 | 4,250.82 | 4,253.22 | 4,157.88 | 4,349.97 |
| Journey maximum | 4,479.83 | 4,437.18 | 4,439.23 | 4,514.32 |
| Connections made / missed / unserved | 0 / 151 / 569 | 0 / 151 / 569 | 0 / 151 / 569 | 0 / 145 / 575 |
| Pending queue clears after final offer | 3,566 | 3,537 | 3,434 | 3,642 |
| All accepted requests finish at | 7,601 | 7,513 | 7,541 | 7,676 |
| Empty distance, km | 1,170.97 | 1,116.41 | 1,185.63 | 1,223.99 |
| Track wait, pod-seconds | 1,468 | 1,087 | 1,480 | 1,166 |
| Junction wait, pod-seconds | 2,682 | 1,624 | 2,727 | 1,731 |

Every arm reaches the 200-request queue cap and uses all 30 pods at peak.
All accepted journeys finish, with no future offers or censored accepted requests.
No heavy arm makes an outbound connection.
The six fewer missed connections in banked seed 2 correspond to six more unserved departures.

## Access and pickup observations

Hub entrance-stopped samples fall from 2,104 to 24 pod-seconds in seed 1, and from 2,134 to four in seed 2.
Hub exit-stopped samples fall from 1,023 to 216, and from 1,176 to 149.
Mean free hub berths change from 4.733 to 4.808, and from 4.818 to 4.830, out of six.
Both banks receive local traffic.
Bank A/B local lane occupancy totals 13,756/7,522 pod-seconds in seed 1 and 14,070/7,727 in seed 2.
Gate-attributed pending samples total 5,422/3,165 request-seconds in seed 1 and 5,663/3,305 in seed 2.
These gate samples can follow a distant committed route.
They do not establish simultaneous gate service.
Focused native tests separately check independent gate admission and waiting at a shared merge under all four routing policies.

Requests without an assigned pod still dominate sampled pickup wait.
Their share changes from 91.785% to 91.896% in seed 1, and from 91.595% to 91.518% in seed 2.
This includes finishing-pod advisory deferrals.
Assigned stationary traffic accounts for only 0.285% to 0.229%, and 0.293% to 0.224%, respectively.
Sampling occurs once per simulated second and does not give exact assignment times.
The record retains per-origin counts and the difference from exact receipt wait totals.

## Validation and limits

All five final arms use the same observer binary, Go 1.27.1, `GOMAXPROCS=1`, and `GOGC=400`.
They run sequentially.
The binary was built from dirty `847911a` with native sources that later landed unchanged in `7707dc2`.
A separate source proof checks those hashes against the committed native code.
The observer sources and overlays remain in the cache artifact directory named by the measurement record.
Elapsed times do not support CPU comparisons.

Every simulation tick checks physical separation and lane speed.
Each simulated second checks saved-state route, phase, and request contracts.
Each heavy arm passes 25 physical restore probes and 60 continuation ticks per probe.
The pilot passes 13 restore probes.
Restore preserves discrete state and berth occupants within existing position tolerances.
It resets speed and can rebuild reservations.
No identical-future claim applies to physical restore.

This screen supports the independent-bank implementation as an optional layout capability.
It does not establish a capacity improvement or justify default adoption.
The [bank guide](station-banks.md) describes the optional browser fixture and editing controls.
