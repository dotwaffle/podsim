# Pickup reassignment safeguards

A 30-second request-age guard and a last-idle-pod reserve guard do not pass the selected service gates.
Both change pickup assignments safely in the tested cases, but neither protects all individual journeys.
Keep the existing implementation and leave pickup reassignment off by default.

## Independent candidates

The age guard requires each affected request to be at least 30 seconds old.
Paired swaps require both requests to meet this limit.
Transfers require the original request to meet it.

The reserve guard blocks remote transfers that recruit the last free idle pod at a passenger station.
It permits local pickups, transfers from parking-only stations, and swaps between assigned pods.
It also permits a remote transfer when another free idle pod remains at that station.

Each guard runs after existing eligibility checks and before cooldown and route-budget checks.
Rejected pairs consume the scan budget without consuming route pairs.
The reserve guard also scans the fleet for another available idle pod.
Both retain physical prefixes, request ownership, redirect preparation, cooldowns, and existing scan and route budgets.
Neither uses a station name, pod ID, tested seed, or request ID as a policy condition.
The candidates remain separate private study overlays.
No production code changes.

Focused tests check unchanged saved state and owners after a rejected pair.
They also check physical restore and safe completion after a permitted redirect, including the parking exemption.
Both candidate suites pass.

## Frozen LondonFull screen

The screen uses source `57d31bb`, whose Go code matches `943fd74`.
All arms use the same current LondonFull geometry, fleet of 287 pods, and AM demand band.
Primary arms use seeds 1 and 4, ten offers per minute, six hours of arrivals, and an eight-hour cap.
The default queue limit remains 200.
Sharing, positioning, and station buffers remain off.
Routing uses free-flow costs and virtual platoons permit four pods.
Each candidate runs independently against both reassignment off and current reassignment on.

Six dense pilots pass separation, per-lane speed, ownership, conservation, and full physical restore checks.
Each pilot matches its nil-observer result exactly.
Both candidate-off pilots match all ten measured evidence fields in the current off pilot.
The short pilots record no reassignments.
Focused tests and primary runs supply the active-decision evidence.

All eight primary arms complete the same 3,599 offers with no skips or unfinished requests.
Journey time starts at the request and ends at alighting.
Mean pickup wait includes elapsed waits for pending requests, although none remain at these endpoints.

| Seed | Arm | Mean pickup, seconds | Mean journey, seconds | Actual end, seconds |
| --- | --- | ---: | ---: | ---: |
| 1 | Off | 301.87 | 1,020.82 | 25,209 |
| 1 | Current on | 301.04 | 1,020.15 | 25,219 |
| 1 | Age guard | 298.75 | 1,017.84 | 25,209 |
| 1 | Reserve guard | 298.71 | 1,017.77 | 25,234 |
| 4 | Off | 310.69 | 1,041.99 | 24,922 |
| 4 | Current on | 307.48 | 1,038.96 | 24,894 |
| 4 | Age guard | 306.66 | 1,037.90 | 24,847 |
| 4 | Reserve guard | 310.59 | 1,041.94 | 24,871 |

No arm supplies the required five-percent mean service gain.
Every comparison passes the aggregate two-percent limits and selected late-backlog limit.
Every comparison fails individual pickup and journey limits.

## Individual service

The analysis matches complete offered populations by offered-event index, tick, origin, and destination.
The pickup increase limit is the larger of 30 seconds and five percent of the baseline wait.
The journey increase limit is the larger of 60 seconds and five percent of baseline request-to-alighting time.

| Seed | Comparison | Pickup exceedances | Journey exceedances | Recovery gate |
| --- | --- | ---: | ---: | --- |
| 1 | Current on against off | 449 | 290 | Fail, 10 seconds later |
| 1 | Age against off | 462 | 272 | Pass |
| 1 | Age against current on | 505 | 321 | Pass |
| 1 | Reserve against off | 382 | 235 | Fail, 25 seconds later |
| 1 | Reserve against current on | 492 | 310 | Fail, 15 seconds later |
| 4 | Current on against off | 479 | 319 | Pass |
| 4 | Age against off | 521 | 338 | Pass |
| 4 | Age against current on | 597 | 403 | Pass |
| 4 | Reserve against off | 512 | 319 | Pass |
| 4 | Reserve against current on | 589 | 407 | Pass |

The age guard records no reassignment before a request reaches 30 seconds.
It produces 86 and 106 request records for seeds 1 and 4.
The reserve guard produces 41 and 70 records, against 88 and 111 with current reassignment on.
A paired swap creates two records.
A transfer creates one.
Fewer reassignment records do not establish better service.

The age guard uses more pair scans and fewer route pairs than current reassignment on in both seeds.
The reserve guard also increases pair scans and adds fleet scans.
Concurrent functional jobs provide no CPU comparison.
No candidate qualifies for a combination, broader qualification, or default adoption.

## Evidence limits

Primary arms check safety, per-lane speed, ownership, and conservation once per simulated second.
Physical restores at three and six hours retain full saved pod fields, routes, poses, riders, and admission records.
Each restored copy passes a separate 60-second continuation with dense checks and new buffers and swaps disabled.
Those copies do not replay future demand or establish full drainage.

Reassignment records verify request identity, changed pod identity, and request age.
They do not record spare idle counts at each transfer.
Source inspection and focused state tests provide the reserve-guard evidence.
The two seeds do not establish a full operating envelope.

Independent candidate, helper, runner, analyzer, and evidence reviews pass.
[Measurements](measurements/pickup-guards.json) retain results, records, input hashes, all affected offer indices, and worst individual regressions.
The [supply screen](pickup-supply.md) records separate fleet trials.
The [experimental gates](experimental-adoption.md) define broader qualification requirements.
