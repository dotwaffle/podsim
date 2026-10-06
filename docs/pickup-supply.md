# LondonFull pickup supply screen

Initial fleet placement improves short-run waits, but the gain does not qualify the tested fleets.
All changed fleets exceed the approved individual pickup and journey limits in six-hour runs.
Keep the existing 287-pod fleet and experimental defaults.

## Frozen comparison

The four fixtures use current source `943fd74` and the current `london-full` scenario output.
They preserve geometry, demand profiles, settings, and the 269 original passenger-station pods.
The baseline also has six pods in each of three parking facilities.
The 300-pod fixtures stay within the existing fleet limit.

| Fixture | Initial pods | Change from baseline |
| --- | ---: | --- |
| Baseline | 287 | None |
| Placed 287 | 287 | Move the 18 parking pods to spare passenger berths |
| Parking 300 | 300 | Add 13 pods in spare parking berths |
| Placed 300 | 300 | Move 18 parking pods and add 13 pods at passenger stations |

Placement uses each station's maximum origin share across the six demand bands.
It divides that share by the number of pods already at the station after each allocation.
Station IDs and berth order break ties.
This rule uses neither tested seeds nor individual requested trips.

The primary screen uses AM demand at 12 offers per minute, seeds 1 and 4, and the default queue limit of 200.
Each arm offers 4,319 requests over six hours and has a fixed eight-hour cap.
Sharing, buffers, swaps, and positioning stay off.
Routing uses free-flow costs and virtual platoons permit four pods.
Every primary arm completes all offers without skips.

## Service outcomes

Journey time includes pickup wait, from request to alighting.
Each row compares with its own seed's baseline.
Extra drain time measures the time after the six-hour arrival window.

| Seed | Fixture | Mean pickup, seconds | Mean journey, seconds | Extra drain, seconds | Pickup limit exceedances | Journey limit exceedances |
| ---: | --- | ---: | ---: | ---: | ---: | ---: |
| 1 | Baseline | 348.40 | 1068.71 | 4342 | 0 | 0 |
| 1 | Placed 287 | 347.09 | 1067.10 | 4419 | 759 | 493 |
| 1 | Parking 300 | 333.12 | 1053.29 | 4088 | 585 | 397 |
| 1 | Placed 300 | 329.36 | 1049.66 | 4342 | 624 | 415 |
| 4 | Baseline | 357.09 | 1088.11 | 3112 | 0 | 0 |
| 4 | Placed 287 | 360.67 | 1091.52 | 3083 | 839 | 539 |
| 4 | Parking 300 | 341.88 | 1072.89 | 3745 | 653 | 451 |
| 4 | Placed 300 | 336.75 | 1067.82 | 3149 | 691 | 453 |

The individual limits permit the larger of 30 seconds or 5% more pickup wait.
They permit the larger of 60 seconds or 5% more request-to-alighting time.
Each changed fleet exceeds both limits for hundreds of matched offers.
The largest pickup regressions range from 1,432.63 to 2,289.10 seconds.

Placed 300 reduces mean pickup wait by 5.46% for seed 1 and 5.70% for seed 4.
Its largest individual pickup regressions still exceed 1,400 seconds.
Placed 287 extends seed 1's recovery by 77 seconds.
For seed 4, Parking 300 extends recovery by 633 seconds and Placed 300 extends it by 37 seconds.
All arms remain inside the eight-hour cap, but these increases fail the baseline recovery deadline gate.

Every changed arm passes the aggregate 2% service limit and the selected late-backlog limits.
Those passes do not waive individual regressions or recovery failures.
No primary requests are censored because every offered event completes.
The analyzer still rejects omitted unfinished cohorts and checks accepted identities against timing and pending records.

Moving parking pods reduces mean pickup wait by 6.65% in the short 287-pod pilot.
The short 300-pod placement pilot improves it by 13.32%.
Parking additions alone show less than 5% benefit.
These 20-minute arrival pilots do not establish sustained service gains.

## Empty travel

| Seed | Fixture | Empty distance, kilometers | Change from baseline |
| ---: | --- | ---: | ---: |
| 1 | Baseline | 23975.75 | +0.00% |
| 1 | Placed 287 | 23721.19 | -1.06% |
| 1 | Parking 300 | 24287.29 | +1.30% |
| 1 | Placed 300 | 24248.91 | +1.14% |
| 4 | Baseline | 23371.97 | +0.00% |
| 4 | Placed 287 | 23547.15 | +0.75% |
| 4 | Parking 300 | 23638.93 | +1.14% |
| 4 | Placed 300 | 23515.89 | +0.62% |

Placement does not consistently reduce empty travel.
The additional 13 pods increase empty travel in both tested seeds.

## Checks and scope

The comparison overlay adds three private, per-input observation hooks to the current CLI source.
Four dense pilots retain every-tick safety checks and exact nil-observer result equality.
The baseline pilot also matches all 79 result fields from the current production CLI.
That separate bridge excludes an observer-induced service change in the tested pilot.

Primary arms check separation, per-lane speed, ownership, and request conservation once per simulated second.
They restore physically at three and six hours without demotions, requeues, or drops.
Restore checks compare full saved pod records, poses, riders, stops, route bindings, admission ages, and platoon records.
Only the existing one-micrometer distance rounding tolerance applies to saved distance comparisons.
Each restored copy passes a separate 60-second continuation with every-tick checks.
These continuations omit future offered demand and do not establish paired workload equivalence.

Fixture, helper, analyzer, runner, and evidence reviews pass.
Concurrent functional runs provide no CPU comparison.
These finite selected screens do not establish indefinite capacity or full demand-band qualification.
The failed individual gates stop broader unchanged runs for these candidates.

The raw measurement data is in git history.
The [experimental gates](experimental-adoption.md) define the separate full-qualification requirements.
