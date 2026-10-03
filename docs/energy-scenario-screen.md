# Analytic energy scenario screen

The mixed fleet changes its total-energy ranking when the authored class-mass assumptions change.
This screen applies authored `flat-v1` coefficients to six native trajectories.
The coefficients do not describe measured vehicles.
The screen does not qualify physical energy use, sustained capacity, or a default policy.
The [measurement record](measurements/energy-scenario-screen.json) retains the frozen plan, ledgers, and estimates.

## Fixed inputs

Four Group arms reuse variation 1 of the [Group service screen](group-fleet-service-screen.md).
The arms use six baseline pods, six Group pods, two pods of each class, or four Group pods.
Each arm offers the same 48 whole parties and 120 passengers.
The cap stays at 2,400 seconds.
Two offers at the cap and the next tick remain future events.

Two occupied-pickup arms reuse the two-pod [service fixture](occupied-pickup-multipod-screen.md).
Both arms offer the same 16 shared singleton parties before the 900-second cap.
A seventeenth offer at 901 seconds remains future.
Occupied pickups are the only policy difference.

The live queue limit remains 200.
Sharing uses four parties, drop-offs, and three intermediate stops.
Party sizes and consent stay immutable.
Geometry, lane speeds, spacing, acceleration, braking, and native physical numbers stay unchanged.
No compact queue fixture enters this screen.

Six Group pods and the mixed six-pod fleet match the baseline vehicle count.
Four Group pods match the baseline's 32 nominal seats.
These comparisons do not match effective sharing capacity or initial placement.
The baseline includes two Legacy pods and four Compact pods.
Legacy pods have eight nominal seats but admit new singleton parties only.

## Authored coefficients

Every mass is a fixed effective mass that includes assumed payload.
The model does not infer a vehicle weight, a passenger weight, or a class coefficient.
The equal-mass reference separates trajectory differences from class-mass assumptions.
The class-mass sensitivity applies a second, explicit assumption to the same trajectories.

| Profile | Fixed mass, Legacy / Compact / Group | Constant resistance | Quadratic resistance | Drive efficiency | Recovery fraction | Auxiliary power |
| --- | --- | --- | --- | --- | --- | --- |
| Reference | 1,000 / 1,000 / 1,000 kg | 100 N | 1 N/(m/s)^2 | 0.8 | 0.5 | 100 W |
| Class mass | 1,000 / 800 / 1,500 kg | 100 N | 1 N/(m/s)^2 | 0.8 | 0.5 | 100 W |
| No recovery | 1,000 / 1,000 / 1,000 kg | 100 N | 1 N/(m/s)^2 | 0.8 | 0 | 100 W |
| Double resistance | 1,000 / 1,000 / 1,000 kg | 200 N | 2 N/(m/s)^2 | 0.8 | 0.5 | 100 W |
| High auxiliary | 1,000 / 1,000 / 1,000 kg | 100 N | 1 N/(m/s)^2 | 0.8 | 0.5 | 500 W |
| Higher mass | 1,250 / 1,250 / 1,250 kg | 100 N | 1 N/(m/s)^2 | 0.8 | 0.5 | 100 W |

Each profile changes only its named assumption from the reference.
These round values define analytic sensitivities, not physical defaults or empirical claims.
The [model design](energy-model-design.md) defines the native tick calculation.

## Measurement method

Each arm runs one recorded native simulation and one plain native simulation.
Six independent meters consume the same actual native motion frames.
Each normalized class also has independent meters with its own fleet counts.
Full-fleet auxiliary power applies to idle and blocked pods on every advanced tick.
The study retains compressed frames and their digest outside the repository.

Each profile reports gross traction draw, recovered energy, auxiliary energy, and signed net energy.
The windows cover tick zero through the cap, zero through 300 seconds, and 300 seconds through the cap.
Window boundaries use successful advanced ticks.
The late window starts with the native speeds at 300 seconds.
A late window can recover kinetic energy acquired before its start.

The record divides each energy component by vehicles, nominal seats, completed parties, and completed passengers.
Class denominators use the class that carried each completed party.
Window denominators include only completions in that window.
A zero denominator produces `null`, which means undefined.
These ratios divide all fleet work by completions.
They do not assign energy to individual parties or omit work for unfinished requests.

The observer checks native separation, current-lane speed, acceleration, braking, owned stopping bounds, ownership, and request conservation on each tick.
`SafetyObservation.Check` does not check lane speed.
The private observer reads the current native lane and applies its authored limit independently.
An unsafe arm stops at its first failed check and cannot support an energy conclusion.

The plain simulation keeps the energy model and motion recorder disabled.
Every tick must match native metrics.
Final saved bytes, request timings, and metrics must also match.
The observer checks whole parties and their original consent on every tick.

## Results

All six arms use qualified source `30d8e34c6368c3dbb382b314cecf6bff61cf40ac`.
The plan froze on `073b8d0` before this source became available.
No measurement uses the planning source.
Source and private binary hashes appear separately in the record.

All 684,000 advanced ticks pass the physical, ownership, accounting, and plain metric checks.
Every final saved byte, request timing, and metric matches its plain run.
No arm stops early, skips an offer, or refuses an offer.
The baseline, mixed six-pod, and four-Group arms retain eight, three, and two unfinished parties.
The six-Group arm and both occupied arms complete every offer before their caps.
Future identities remain separate in every ledger.

The reference table uses the equal-mass profile.
Energy values use megajoules.
The last two columns divide signed net energy by completed parties or passengers.

| Arm | Completed parties / passengers | Gross draw | Recovery | Auxiliary | Signed net | Net per party | Net per passenger |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Baseline six | 40 / 94 | 62.545 | 2.732 | 1.440 | 61.253 | 1.531 | 0.652 |
| Six Group | 48 / 120 | 50.122 | 2.349 | 1.440 | 49.212 | 1.025 | 0.410 |
| Mixed six | 45 / 109 | 61.653 | 2.474 | 1.440 | 60.619 | 1.347 | 0.556 |
| Four Group | 46 / 112 | 48.877 | 2.027 | 0.960 | 47.809 | 1.039 | 0.427 |
| Occupied off | 16 / 16 | 8.711 | 0.883 | 0.180 | 8.009 | 0.501 | 0.501 |
| Occupied on | 16 / 16 | 4.709 | 0.530 | 0.180 | 4.359 | 0.272 | 0.272 |

The six-Group arm matches vehicle count and reduces the reference total estimate by 12.040 MJ.
The four-Group arm matches nominal seats and reduces that estimate by 13.444 MJ.
Different completion cohorts prevent either difference from proving an energy saving for the same completed passengers.
The record retains per-class, per-vehicle, and per-seat ratios for every component and window.

The next table reports full-window signed net energy in megajoules.
Each row uses the same native trajectories and completion cohorts.

| Analytic profile | Baseline six | Six Group | Mixed six | Four Group | Occupied off | Occupied on |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Reference | 61.253 | 49.212 | 60.619 | 47.809 | 8.009 | 4.359 |
| Class mass | 60.436 | 51.180 | 61.363 | 49.630 | 8.009 | 4.359 |
| No recovery | 63.985 | 51.562 | 63.093 | 49.837 | 8.891 | 4.889 |
| Double resistance | 116.124 | 93.048 | 115.294 | 91.016 | 14.367 | 7.656 |
| High auxiliary | 67.013 | 54.972 | 66.379 | 51.649 | 8.729 | 5.079 |
| Higher mass | 62.488 | 50.196 | 61.745 | 48.720 | 8.376 | 4.579 |

The equal-mass profile gives the mixed fleet a 0.633 MJ lower total estimate than the baseline.
The class-mass profile gives it a 0.927 MJ higher total estimate.
The coefficients determine this reversal, with no change to native motion or completed parties.
This screen cannot select a physical class mass from these results.

Occupied pickups reduce the total estimate in all six authored profiles.
The reference reduction is 3.650 MJ for the same 16 completed singleton identities.
This result applies to the two-pod fixture and the declared coefficients.
It does not measure real vehicle efficiency or qualify larger fleets.

Energy differences do not waive the [service gates](experimental-adoption.md).
Six Group, mixed six, and four Group exceed individual pickup limits for four, eleven, and sixteen matched parties.
Their journey exceedance counts are four, eight, and eleven.
The occupied pair has zero individual exceedances.
The record retains all censored pairs and each affected event identity.

Full windows equal early plus late windows within an absolute tolerance of 0.00001 J plus a relative tolerance of 0.000000001.
Class totals equal fleet totals within the same tolerance.
The largest difference across these identities and the sensitivity checks is 0.000111491 joules.
The early Compact cohort in the mixed fleet completes zero parties.
Its per-completed-party and per-completed-passenger ratios remain undefined.

Focused private guard tests, race checks, vet, lint, and fresh diagnostics pass.
Four compiled mutations fail assertions for omitted frames, consumption with recording disabled, unchecked lane speeds, and changed consent.
An initial stopping-bound guard setup also changed resource retention.
The corrected test synchronizes ownership with its changed distance before testing the stopping bound.
Both setup results remain in the evidence.

The screen adds no restore, queue-saturation, CPU-cost, rail-load, sustained-capacity, or default-adoption qualification.
