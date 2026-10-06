# Shorter Paddington paths on LondonFull

Two coherent shorter layouts pass the tested safety and restore checks but fail individual service limits.
The 10% layout reduces mean pickup wait by 16% in the focused Paddington workload.
Three requests still exceed the individual pickup limit, and the LondonFull screens show larger regressions.
Keep authored geometry and operating defaults unchanged.

## Frozen layouts and workloads

The study uses source `2cfe9e4`, whose Go code matches `943fd74`, and the current builtin LondonFull fleet of 287 pods.
Paddington has four berths in this fixture.
Each candidate scales six paired arrival/departure node positions around their pair midpoint, by 0.90 or 0.80.
The pairs cover diverge/merge, entry/exit, and each berth's arrival/departure nodes.
Berth positions, curve controls, topology, separation groups, fleet, demand settings, and all other decoded project fields remain unchanged.
These are new LondonFull candidates, separate from the earlier LondonCentral position trials.

The sum of all 29 Paddington-tagged lane lengths decreases from 3,066.82 meters to 2,957.06 and 2,848.56 meters.
Five connecting lanes lengthen in each candidate.
The pair scale does not mean every route becomes 10% or 20% shorter.
All fixtures retain the existing project size limit, clearance, speed limits, and ownership checks.

Six dense pilots precede nine primary arms.
All pilots match their nil-observer result exactly and pass physical restore, separation, speed, ownership, and request-conservation checks.
The unchanged AM baseline also matches the previous supply screen's complete result in both seeds.

Primary AM12 arms use seeds 1 and 4, six hours of arrivals, and an eight-hour cap.
The focused workload uses seed 3, two hours of arrivals, and a three-hour cap.
It offers four simultaneous requests each minute, with a seeded random permutation within each wave.
Two travel from Waterloo and King's Cross to Paddington.
Two travel from Paddington to Waterloo and Victoria.

All arms retain the default queue limit of 200, free-flow routing, and virtual platoons with four pods.
Sharing, positioning, station buffers, and pickup reassignment remain off.
Journey time starts at the request and ends at alighting.
All primary offers complete with no skips or unfinished requests.

## Service results

| Workload | Layout | Completed | Mean pickup, seconds | Mean journey, seconds | Actual end, seconds |
| --- | --- | ---: | ---: | ---: | ---: |
| AM12 seed 1 | Baseline | 4,319 | 348.40 | 1,068.71 | 25,942 |
| AM12 seed 1 | 0.90 pairs | 4,319 | 346.57 | 1,066.66 | 25,942 |
| AM12 seed 1 | 0.80 pairs | 4,319 | 348.56 | 1,068.44 | 25,942 |
| AM12 seed 4 | Baseline | 4,319 | 357.09 | 1,088.11 | 24,712 |
| AM12 seed 4 | 0.90 pairs | 4,319 | 359.60 | 1,090.65 | 25,297 |
| AM12 seed 4 | 0.80 pairs | 4,319 | 358.53 | 1,089.44 | 24,708 |
| Paddington mixed | Baseline | 480 | 52.06 | 475.03 | 7,678 |
| Paddington mixed | 0.90 pairs | 480 | 43.68 | 464.96 | 7,618 |
| Paddington mixed | 0.80 pairs | 480 | 51.62 | 472.54 | 7,665 |

The focused 0.90 layout reduces mean pickup wait by 8.38 seconds, or 16.10%.
It finishes 60 seconds earlier than its baseline.
The same layout finishes 585 seconds later in AM12 seed 4.
Neither layout supplies a five-percent mean service gain in either AM screen.
Every comparison passes the aggregate two-percent limits.
All AM comparisons pass the selected late-backlog limits.
These finite outcomes do not establish indefinite capacity.

## Individual service limits

The analysis matches full offered populations by offered-event index, tick, origin, and destination.
It retains all affected offer indices and selected worst regressions.
No primary pair has censored endpoints.

| Workload | Layout | Pickup exceedances | Journey exceedances | Largest added pickup, seconds | Recovery gate |
| --- | --- | ---: | ---: | ---: | --- |
| AM12 seed 1 | 0.90 pairs | 446 | 280 | 1,214.87 | Pass |
| AM12 seed 1 | 0.80 pairs | 605 | 391 | 1,188.02 | Pass |
| AM12 seed 4 | 0.90 pairs | 697 | 451 | 2,240.83 | Fail |
| AM12 seed 4 | 0.80 pairs | 624 | 401 | 1,680.28 | Pass |
| Paddington mixed | 0.90 pairs | 3 | 0 | 51.37 | Pass |
| Paddington mixed | 0.80 pairs | 1 | 0 | 51.37 | Pass |

The pickup increase limit is the larger of 30 seconds and five percent of baseline wait.
The journey increase limit is the larger of 60 seconds and five percent of baseline request-to-alighting time.
The local average gain does not waive these limits.
No changed candidate passes the selected screen, so broader qualification does not start.
No exception or default change is approved by this study.

## Sampled station histories

The observer records berth states and stationary pod holds once per simulated second.
It covers Paddington-tagged lanes and pods at Paddington, with a fixed 200,000-episode limit.
The observer does not expose hidden junction ownership or mainline queues outside those lanes.
Samples start at the first one-second check and can miss shorter changes.

| Workload | Layout | Occupied berth-seconds | Reserved-empty berth-seconds | Sampled junction-hold pod-seconds |
| --- | --- | ---: | ---: | ---: |
| AM12 seed 1 | Baseline | 10,330 | 2,077 | 52 |
| AM12 seed 1 | 0.90 pairs | 11,376 | 2,040 | 54 |
| AM12 seed 1 | 0.80 pairs | 11,756 | 2,200 | 51 |
| AM12 seed 4 | Baseline | 7,494 | 2,527 | 30 |
| AM12 seed 4 | 0.90 pairs | 8,772 | 2,403 | 78 |
| AM12 seed 4 | 0.80 pairs | 7,140 | 2,347 | 36 |
| Paddington mixed | Baseline | 8,411 | 3,261 | 41 |
| Paddington mixed | 0.90 pairs | 11,765 | 3,407 | 41 |
| Paddington mixed | 0.80 pairs | 8,936 | 3,343 | 38 |

Observation endpoints differ when an arm drains earlier or later.
Activity and rider changes split state intervals, even when the same reservation continues.
Reserved-empty state intervals are not complete reservation lifetimes.
These samples do not isolate the cause of the focused gain or the individual AM regressions.
They do not justify weaker reservations, independent berth banks, or a default geometry change.

## Validation and evidence limits

All 15 arms pass their planned safety, ownership, accounting, and physical restore checks.
Primary checks sample once per simulated second.
Each arm has two physical restore checkpoints with full saved pod fields, routes, poses, riders, and admission records.
Each restored copy passes a separate 60-second continuation with dense checks and new buffers and swaps disabled.
These copies do not replay future demand or prove full per-tick safety throughout the longer live arm.
Stage-labeled failure capture would preserve the failing live or restored-copy state.
No safety failure occurs in these trials.

Concurrent functional workers provide no CPU comparison.
Two AM seeds and one focused workload do not establish a full operating envelope.
Independent fixture, helper, runner, analyzer, and evidence reviews pass.
The raw measurement data is in git history.
The [earlier position trials](paddington-layout.md) retain their separate source and fixture boundaries.
The [experimental gates](experimental-adoption.md) define qualification requirements.
