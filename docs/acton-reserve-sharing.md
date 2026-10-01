# Acton initial reserve and burst sharing comparison

Four-party sharing drains both selected Acton bursts within the 90-minute cap.
The combined local-reserve and sharing arms reduce average pickup waits versus default placement without sharing.
Adding reserves to an already-sharing fleet also creates large individual wait regressions.
These are startup placement experiments, not a sustained reserve controller or a capacity recommendation.

## Fixed workload and placement

The study freezes production source `b4cfefe` and the mirrored LondonFull project.
Each seed offers 239 outbound parties from Acton Town, with bursts of 120 at five seconds and 119 at 605 seconds.
Arrivals span a declared 20-minute window, followed by recovery under a 90-minute cap.
The queue limit is one million, and no cell skips an offer.
Station buffers, pickup reassignment, and redistribution remain off.
Free-flow routing and four-pod virtual platoons remain enabled.

The eight cells cross two seeds, two initial placements, and sharing limits of one or four parties.
Sharing uses the existing drop-offs mode, three-stop default, and 1.5 detour guard.
The one-party arms record destination mode because no joining occurs.
Every cell has the same 287 pods and identical network geometry and demand.

The reserve fixture moves eight initially parked pods into existing empty second berths at Acton Town, Gunnersbury, Ealing Common, Chiswick Park, West Acton, North Ealing, Turnham Green, and South Ealing.
It selects those berths by distance from Acton, not by a directed travel-time guarantee.
It empties six west-Parking berths and two north-Parking berths.
It adds no vehicle, berth, lane, or parking capacity.
No authored project changes.
The recorded empty travel excludes the unmodeled earlier moves needed to create that initial placement.

## Aggregate service

Times use simulated seconds.
All 239 accepted parties board in every arm.
Journey means include completed parties only, so the incomplete arms have different journey cohorts.

| Seed | Placement | Sharing | Completed | Aboard at cap | Mean wait | Mean journey | Empty km | Joined parties | Run ends |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | Baseline | 1 | 235 | 4 | 1,312.6 | 2,536.0 | 3,641.3 | 0 | 5,400 |
| 1 | Baseline | 4 | 239 | 0 | 1,204.5 | 2,471.0 | 3,379.3 | 12 | 5,392 |
| 1 | Reserve | 1 | 235 | 4 | 1,265.9 | 2,488.2 | 3,566.5 | 0 | 5,400 |
| 1 | Reserve | 4 | 239 | 0 | 1,123.4 | 2,393.9 | 3,235.4 | 15 | 5,319 |
| 2 | Baseline | 1 | 238 | 1 | 1,307.3 | 2,651.3 | 3,823.0 | 0 | 5,400 |
| 2 | Baseline | 4 | 239 | 0 | 1,218.2 | 2,570.4 | 3,609.7 | 9 | 5,311 |
| 2 | Reserve | 1 | 238 | 1 | 1,283.5 | 2,740.0 | 3,730.7 | 0 | 5,400 |
| 2 | Reserve | 4 | 239 | 0 | 1,157.4 | 2,512.0 | 3,514.9 | 12 | 5,303 |

Compared with default placement without sharing, combined arms reduce mean pickup wait by 14.4% and 11.5% for seeds 1 and 2.
Every matched pickup wait is unchanged or lower in those two comparisons.
They complete all parties, including the four and one parties still aboard in their respective baselines.
No baseline-completed party becomes unfinished under the combined arms.

Reserves alone reduce mean wait by 3.6% and 1.8%.
Seed 2's mean journey increases by 3.3%, and its journey p95 increases by about 5.0%.
The same number of parties complete in each reserve-only pair, but their travel times change.
Sharing alone improves mean wait in both seeds and drains the burst.
A lower average does not establish that every party improved.

## Individual service and comparison choice

The analysis joins accepted parties by offered-event index and checks requested ticks.
All compared arms accept and board the same 239 events.
Journeys compare only jointly completed parties, with unfinished parties retained as censored observations.
No comparison leaves a baseline-completed party unfinished.
The pair records count candidate completions of previously unfinished parties separately.

The provisional pickup limit is `max(30 seconds, 5% of baseline wait)`.
The provisional journey limit is `max(60 seconds, 5% of baseline journey)`.
These are proposals, not agreed adoption thresholds.
The table counts failures among the matched measurable cohorts.

| Change | Seed | Pickup-limit failures | Journey-limit failures | Largest added wait | Largest added journey |
| --- | ---: | ---: | ---: | ---: | ---: |
| Reserves, sharing 1 | 1 | 0 | 0 | 0.0 | 15.8 |
| Reserves, sharing 4 | 1 | 6 | 6 | 1,812.0 | 1,811.1 |
| Sharing, default placement | 1 | 1 | 0 | 55.0 | 171.6 |
| Sharing, reserve placement | 1 | 1 | 2 | 73.9 | 165.0 |
| Combined versus default | 1 | 0 | 0 | 0.0 | 57.1 |
| Reserves, sharing 1 | 2 | 0 | 81 | 100.6 | 338.4 |
| Reserves, sharing 4 | 2 | 5 | 5 | 1,904.8 | 1,906.0 |
| Sharing, default placement | 2 | 0 | 2 | 0.0 | 134.2 |
| Sharing, reserve placement | 2 | 0 | 0 | 0.0 | 71.7 |
| Combined versus default | 2 | 0 | 0 | 0.0 | 0.1 |

Request 200 in seed 1, Acton to Rayners Lane, changes from zero wait to 1,812 seconds when reserves supplement sharing.
Request 205 in seed 2, Acton to Vauxhall, changes from 18.9 seconds to 1,923.8 seconds.
Both finish in both arms.
The combination's improvement against the no-sharing baseline does not waive these regressions against an already-sharing baseline.
The timing records identify the parties and retain their sharing bindings for further diagnosis.
They do not establish a dispatch defect or justify a default change.

## Validation and limits

Four dense pilots pass safety, speed, request conservation, and exact production-aggregate parity.
All eight primary cells pass the same checks once per simulated second.
Each passes two physical restores and a separate dense 60-second continuation.
The 16 primary restores retain request bindings, counters, poses, and saved certificate fields without degraded recovery.
The largest completed shared detour ratio is below 1.15.
Independent review checks the helper, frozen source, placement changes, and sharing settings.

An indented reserve fixture initially exceeded the existing 10 MiB file limit.
Compact encoding preserves the same data and fits the unchanged limit.
The failed attempt remains recorded, and the two passed baseline pilots were reused only after exact job, source, binary, and project checks.
No size limit increased.

[Arm results](measurements/acton-reserve-sharing-arms.csv), [matched pairs](measurements/acton-reserve-sharing-pairs.csv), [selected tails](measurements/acton-reserve-sharing-tails.csv), and [metadata](measurements/acton-reserve-sharing.json) retain the evidence.
Raw timings, schedules, source overlays, fixture placements, and failed-attempt records remain in `~/.cache/agents/podsim/reserve-sharing-20261001/`.
Concurrent cells provide no CPU comparison.
The fixture has no inbound passenger burst or general background demand.
It cannot measure the effect of occupied reserve berths on busy inbound service or establish sustainable station throughput.
See the [terminus measurements](terminus-flow.md) and [proposed adoption gates](experimental-adoption.md).
