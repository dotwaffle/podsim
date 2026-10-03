# Forecast positioning with shared rail offers

Status: bounded Rail Hub screen on `9113bb3`, October 3, 2026.
Both forecast candidates fail the selected service gates.
Live integration stays closed.
The [measurement record](measurements/forecast-shared-service-screen.json) pins the sources, fixtures, outputs, and complete comparison ledger.

## Workload

Each seed offers 1,440 one-person parties with explicit shared consent.
Sharing permits four parties, drop-off mode, and three intermediate stops.
The baseline Rail Hub layout, 30 legacy pods, queue limit 200, speeds, clearances, and three-hour cap remain fixed.
Occupied pickups and platoons remain off.
Only forecast positioning differs between arms.

The existing forecast reads the authored future rail schedule.
It has exact schedule knowledge, a 300-second horizon, and a five-second call interval.
Existing supply, berth, route, and fleet movement guards remain unchanged.
This screen adds no prediction algorithm or physical default.

The hypothesis was that shared service could reduce the forecast harm found in the earlier private workload.
The selected shared workload does not support that hypothesis under the no-harm gates.

## Results

| Seed / forecast | Completed | Skipped | Pickup p95 | Request-to-completion p95 | Empty distance | Forecast moves |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 / off | 862 | 578 | 1,407.550 s | 1,800.950 s | 848,351.873 m | 0 |
| 1 / on | 877 | 563 | 1,301.017 s | 1,728.450 s | 666,913.028 m | 7 |
| 2 / off | 920 | 520 | 1,176.433 s | 1,583.667 s | 638,710.358 m | 0 |
| 2 / on | 916 | 524 | 1,198.417 s | 1,615.933 s | 648,150.841 m | 7 |

All accepted orders complete before the cap.
Net completion counts hide changes in which offers complete.
The seed 1 candidate skips 154 offers that the baseline completes.
The seed 2 candidate skips 178 such offers.
Their full identities remain in the evidence ledger.

Pickup delay increases exceed `max(30 seconds, 5% of baseline pickup delay)` for 165 and 342 common boarded offers.
Request-to-completion increases exceed `max(60 seconds, 5% of baseline duration)` for 142 and 293 common completed offers.
The comparison does not assume a pass for skipped or unfinished offers.

For seed 1, common-offer pickup delay decreases by 48.414 seconds on average.
Common-offer request-to-completion duration decreases by 46.263 seconds.
For seed 2, these averages increase by 36.209 and 38.629 seconds.
Seed 2 also exceeds the 2% aggregate limits for mean pickup delay, mean request-to-completion duration, and its p95.

Seed 1 makes 8 outbound rail connections without the forecast and 12 with it.
Its missed counts change from 446 to 411, and unserved counts change from 266 to 297.
Seed 2 makes 5 connections in each arm.
Its missed counts change from 393 to 438, and unserved counts change from 322 to 277.
No outbound connection remains unresolved at the endpoint.

## Validation and limits

Both light pilots compare the observer with a plain run on the same sources.
All aggregate fields and every request timing match exactly.
The off arms also reproduce the previous shared screen, including every passenger record.
The study verifies 475 Go source hashes and all overlay hashes before each arm.
The study build and vet pass.

The native observer and its tests retain their previous hashes.
Their [shared pickup validation](onboard-pickup-rail-screen.md) remains the test, race, and mutation evidence.
This batch does not claim new observer test runs.

The heavy arms perform 1,275,840 live safety checks, 21,264 contract checks, and 69 planned physical restore probes.
All pass the existing checks.
The minimum observed separation is 13.9631 meters.
No occupied pickup occurs.
These checks cover the selected runs and do not prove all possible reservation states.

This forecast is not a useful qualified candidate for live integration.
The live rail generator also uses private consent, so a shared-only screen could not qualify that path.
The [adoption gates](experimental-adoption.md), physical limits, and defaults remain unchanged.
Do not repeat this matrix without a new, concrete forecast hypothesis.
