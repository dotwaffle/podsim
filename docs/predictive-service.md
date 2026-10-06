# Selected predictive-routing service comparisons

The predictive policy selects no alternative route in these six matched pairs.
Its 40,794 evaluations leave every accepted request, boarding tick, completion tick, and final pending record unchanged.
This screen establishes no service or capacity benefit.
Free-flow routing remains the default.

## Inputs and checks

The study compares frozen source `a54281e` with predictive routing off and on.
It uses two seeds for LondonCentral Early10/min and LondonFull Morning13/min and Morning14/min.
Morning is a separate demand band from AM peak.
Every arm offers six hours of demand and allows a seven-hour cap.
The queue limit is 1,000,000, virtual platoons allow four pods, and sharing, buffers, pickup reassignment, and positioning remain off.

Three dense pilots check observer results against production aggregates.
The two free-flow pilots also require exact result, schedule, and check parity between `b4cfefe` and `a54281e`.
All 12 primary arms check safety, speed, and unique-order accounting once per simulated second.
Physical restores at three and six hours retain bindings, poses, admission ages, and saved platoon records.
Each restored copy runs 60 seconds of dense safety checks with its routing policy retained and its forecast history reset.
This checks physical continuation, not identical future experimental route choices after a restart.

The observer counts route evaluations and returned alternatives without changing simulation decisions.
A returned alternative would still be a proposed route, which a caller could discard.
These counters do not measure committed diversions.
Concurrent functional runs do not provide CPU comparisons.

## Matched outcomes

All result and check fields match exactly within each pair, except the routing label and prediction counters.
This includes complete schedules, accepted-request records, individual timing records, pending ages, and the terminus report.
Each arm accepts every offered request without skips.
Every arm reaches the seven-hour cap with unfinished requests.

| Fixture and band | Rate/min | Seed | Evaluations | Completed | Aboard | Pending | Late backlog growth, requests/min |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Central Early | 10 | 1 | 5,460 | 2,661 | 54 | 884 | 3.283 |
| Central Early | 10 | 2 | 5,645 | 2,758 | 48 | 793 | 3.222 |
| Full Morning | 13 | 1 | 7,447 | 3,559 | 154 | 982 | 4.306 |
| Full Morning | 13 | 2 | 7,359 | 3,533 | 138 | 1,024 | 4.372 |
| Full Morning | 14 | 1 | 7,472 | 3,572 | 152 | 1,318 | 5.217 |
| Full Morning | 14 | 2 | 7,411 | 3,564 | 134 | 1,344 | 5.289 |

Late backlog growth measures the change between three and six hours, divided by 180 minutes.
Positive growth and unfinished requests prevent treating these rates as sustained capacity.
Reported service statistics also have unfinished-request censoring.
Exact paired timing and censoring records prevent interpreting completed-only averages as an improvement.

## Interpretation and evidence

The [prototype](predictive-routing.md) exercises route selection in synthetic tests, but none of these service cells selects a changed route.
The later [route-selection diagnosis](predictive-diagnosis.md) identifies the forecast and guard outcomes for all six cells.
It does not prove that congestion routing cannot help another topology or workload.
These cells provide no basis for reducing selection guards or enabling the policy by default.
An isolated CPU comparison must measure the cost of these unchanged outcomes separately.

[Arm totals](measurements/predictive-service-arms.csv) retain service statistics, backlog growth, and check counts.
[Pair records](measurements/predictive-service-pairs.csv) retain exact-parity results and every unfinished request ID.
[Metadata](measurements/predictive-service.json) retains source, project, helper, and binary hashes, pilot results, and all 15 run outcomes.
The owned study stopped successfully, and its scratch binaries were removed.
