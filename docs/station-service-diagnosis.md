# Busy-station pickup delay and access safety

Heavy Rail Hub pickup delay comes mainly from requests without an assigned eligible pod.
The earlier two-bank failures came from an unconnected road/bank near-crossing.
A coordinate correction passes the selected safety screen but does not improve service.
No layout or controller default changes.

## Pickup delay attribution

The diagnostic observer uses main `7306058` with private comparison hooks.
The simulation and station-observation sources match the frozen study at `ed2ba64`.
Both diagnostic seeds reproduce every frozen result field and passenger receipt exactly.
The [measurement record](measurements/station-service-diagnosis.json) retains source and artifact hashes.
Final arms run sequentially with `GOMAXPROCS=1` and `GOGC=400`, matching the frozen worker settings.
Their result files match the initial functional runs byte-for-byte.
Elapsed times do not support CPU comparisons.

This diagnosis uses the earlier 24-passenger workload, with 288 offers and no skips.
All 288 journeys finish in each seed.
The outbound train lead remains 900 seconds after each request window ends.
Fleet, routing, queue, speed, clearance, and disabled experimental policies remain fixed.

The observer samples existing pending requests and their assigned pods once per simulated second.
Counts below cover each run through its own end time, including startup and drain.
They measure pending request-seconds, not fleet pod-seconds.
One pod can affect later requests that do not yet have assignments.

| Pending state | Seed 1, request-seconds | Share | Seed 2, request-seconds | Share |
| --- | ---: | ---: | ---: | ---: |
| No eligible pod selected | 137,047 | 77.20% | 140,164 | 78.74% |
| Finishing-pod advisory hold | 2,538 | 1.43% | 2,342 | 1.32% |
| Assigned pod moving or in dwell | 35,662 | 20.09% | 32,790 | 18.42% |
| Assigned pod stopped in traffic | 2,283 | 1.29% | 2,714 | 1.52% |
| Total sampled pickup wait | 177,530 | 100% | 178,010 | 100% |

The exact receipt totals are 177,450.08 and 177,934.02 pickup-wait seconds.
Sampling adds 79.92 and 75.98 seconds, each less than one second per passenger.
Assignment changes between samples remain unobserved.
These counts do not give exact assignment timestamps.
The traffic category requires speed below 0.01 m/s and a nonempty wait reason.
Slow movement remains in the moving-or-dwell category.

`dispatch` reports no available pod when `pickupPod` finds no eligible candidate.
Occupied pods and pods assigned to another pickup cannot take the request.
After assignment, boarding waits until that pod becomes idle at the origin.
Existing local substitution can replace the assigned pod.
These rules explain why free destination berths do not establish available pickup supply.

Delay grows at both hub and remote origins.
Hub-origin pickup means are 518.99 and 528.37 seconds.
Remote-origin pickup means are 713.30 and 707.28 seconds.
Their mean boarding-to-completion times remain 288.61/296.75 and 386.61/370.27 seconds, respectively.
Most extra heavy-workload journey delay occurs before boarding.

The first hub burst contains 24 simultaneous requests and only three local idle pods.
Three passengers board immediately.
The fourth starts after 116.13 seconds, and the last after 488.27 seconds in both seeds.
All 30 pods become active at peak.
Peak unboarded requests reach 73 and 79.
By the observation hour, 97 and 100 accepted journeys remain unfinished.
The default advisory hold has a 30-second lifetime per request.
It cannot directly explain the much longer mean wait.

The earlier approaching-pod metric counts distant empty and loaded pods whose routes end at a hub berth.
It does not measure only the local entrance queue.
Mean free hub berths also mix inbound pickups, outbound deliveries, startup, and drain.
Neither metric establishes that berth admission dominates pickup delay.
This diagnosis supports fleet circulation as the main measured constraint, without proving a required fleet size or policy.

## Captured access failures

Both rejected shared-entry bank arms fail on the same pair of unconnected lanes.
`link-07-01` approaches the hub entry from the parking station.
`bank-b-2-arrival` reaches the third row of the second bank.
The paths come within 3.98417 meters, without a literal segment intersection.
The adjacent inlet has the same hazard.

| Seed | Tick | Road pod | Bank pod | Actual pod gap, meters |
| --- | ---: | --- | --- | ---: |
| 1 | 88,160 | pod-008 | pod-017 | 11.71552 |
| 2 | 115,238 | pod-029 | pod-027 | 11.85891 |

These lanes have no common endpoint and use the same physical plane.
Junction admission compares incident lanes only, so it creates no shared junction resource for this pair.
The physical safety check correctly rejects gaps below 12 meters.
The captured saves omit resource owners and reservation frontiers.
Exact live reservation ownership cannot be reconstructed from them.
No unchanged unsafe arm was rerun for this diagnosis.

## Corrected shared-entry candidate

The candidate changes three node coordinates from Y=-320 to Y=-290: `bank-b-arrival-2`, `s01-berth-06`, and `bank-b-departure-2`.
It keeps lane IDs, topology, fleet, schedules, speed, and clearance.
The affected path gap becomes 17.00068 meters.

A full static audit uses the same straight and 64-segment quadratic polylines as pod movement.
It examines 6,831 eligible nonincident lane pairs across 122 lanes.
The original has two gaps below 12 meters.
The correction has none.
Both layouts have no nonincident berth/lane hazard and a minimum berth-pair gap of 30 meters.
The smallest corrected lane remains at least 24 meters long.
Incident lanes still require ordinary controller admission and dynamic safety checks.
The geometry regression retains both hazardous lanes and the corrected row.

The light pilot completes all 12 offers and makes all six trains.
Its observed result equals an ordinary comparison without observation hooks.
Only after that pilot did the changed candidate run both heavy seeds.

The heavy screen retains the earlier overload schedule: 120 passengers per event and 1,440 planned offers.
Its train lead remains 300 seconds after each request window ends.
It is separate from the 24-passenger delay attribution above.
Both corrected seeds finish within the original three-hour cap.
Each accepted journey completes.
No future offer or unfinished accepted journey remains.
Rejected offers remain in the passenger census.

| Seed / layout | Completed / offered | Skipped | Pickup mean / p95, seconds | Journey mean / p95, seconds | Made / missed / unserved outbound connections |
| --- | ---: | ---: | ---: | ---: | ---: |
| 1 baseline | 363 / 1,440 | 1,077 | 2,688.47 / 3,862.03 | 3,013.25 / 4,250.82 | 0 / 151 / 569 |
| 1 corrected | 364 / 1,440 | 1,076 | 2,715.65 / 3,892.90 | 3,043.76 / 4,273.18 | 0 / 148 / 572 |
| 2 baseline | 366 / 1,440 | 1,074 | 2,645.46 / 3,768.27 | 2,960.85 / 4,157.88 | 0 / 151 / 569 |
| 2 corrected | 363 / 1,440 | 1,077 | 2,710.11 / 3,900.82 | 3,040.26 / 4,318.18 | 0 / 151 / 569 |

| Seed / layout | Queue-clear time, seconds | Empty distance, km | Track / junction holds, sampled pod-seconds |
| --- | ---: | ---: | ---: |
| 1 baseline | 3,566 | 1,170.97 | 1,468 / 2,682 |
| 1 corrected | 3,461 | 1,152.62 | 1,079 / 1,621 |
| 2 baseline | 3,434 | 1,185.63 | 1,480 / 2,727 |
| 2 corrected | 3,425 | 1,168.59 | 1,153 / 1,740 |

Queue-clear time uses the comparison command's time after the final offer.
Hold samples cover each arm's own duration.
They are not a common-window contention comparison.
The corrected layout lowers sampled holds but worsens pickup and journey means in both seeds.
Accepted counts change by +1 and -3.
No outbound passenger makes a train.
These results do not support adoption or broader service qualification.

The observer checks separation and lane speed on every tick: 450,240 and 453,120 checks.
It checks simulation contracts once per second and performs 25 physical restores per seed.
Each restore retains discrete state and positions within existing tolerances, followed by 60 safe continuation ticks and receipt checks.
Two ordinary clones retain identical continuations.
Restored velocity resets, so restored futures need not match uninterrupted futures.

## Independent access design

Both current banks still share the same entry, exit, and external access lanes.
Approach routing targets `Station.Entry`, and terminal berth assignment starts its suffix there.
Independent physical gates therefore need a station contract change.

The proposed bounded design adds explicit bank gates and flat-berth membership while retaining one passenger station ID.
Each bank uses ordinary approach, departure, track, junction, and berth resources.
Long approaches use existing optional buffer cells.
Separate parking stations provide storage through explicit links.
No shared gate or external merge gains a safety exemption.
The project/save/editor proposal requires a concrete user decision before implementation.

Raw receipts, frozen observer sources, static audit, corrected fixtures, and analysis remain in `~/.cache/agents/podsim/service-access-20261002/`.
The repository measurement is a summary, not a standalone study runner.
The original failed evidence remains in the earlier service-study cache.
