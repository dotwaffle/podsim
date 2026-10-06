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
The route-selection diagnosis below identifies the forecast and guard outcomes for all six cells.
It does not prove that congestion routing cannot help another topology or workload.
These cells provide no basis for reducing selection guards or enabling the policy by default.
An isolated CPU comparison must measure the cost of these unchanged outcomes separately.

The raw measurement data is in git history.
The owned study stopped successfully, and its scratch binaries were removed.

## Route-selection diagnosis

The six service pairs forecast little avoidable traffic delay.
Their unchanged routes follow from the forecast costs and existing guards.
A congested Acton trial returns alternatives, but completes fewer journeys.

Counter-only observers replay all six cells on frozen production source `e285e69`.
Every result, schedule, accepted request, timing, pending record, terminus report, and restore/check count matches its historical predictive arm exactly.
The observer records costs after the original routing decisions without running extra searches.

| Cell | Evaluations | No forecast delay | Same route | Distinct candidate below savings guard | Largest predicted saving, seconds |
| --- | ---: | ---: | ---: | ---: | ---: |
| Central Early10, seed 1 | 5,460 | 5,139 | 320 | 1 | 0.706 |
| Central Early10, seed 2 | 5,645 | 5,311 | 334 | 0 | 0.000 |
| Full Morning13, seed 1 | 7,447 | 7,301 | 144 | 2 | 1.075 |
| Full Morning13, seed 2 | 7,359 | 7,208 | 145 | 6 | 2.795 |
| Full Morning14, seed 1 | 7,472 | 7,322 | 149 | 1 | 1.603 |
| Full Morning14, seed 2 | 7,411 | 7,220 | 183 | 8 | 2.282 |

Of 40,794 evaluations, 39,501 forecast no delay.
Another 1,275 searches return the same route.
The remaining 18 distinct candidates save less than 2.8 seconds.
None passes the savings guard: at least 15 seconds and 5% of the original route's forecast cost.
The detour guard does not reject these distinct candidates.

The [forecast model](predictive-routing.md) uses observed queues and planned arrivals within a 90-second horizon.
Its FIFO model assumes three-second discharge headways.
A queue can disappear from the predicted cost before a distant pod reaches it.
A delayed station exit shared by both paths also provides no avoidable route cost.
These counts describe the model's predictions.
They do not show that actual traffic delays are small or that the forecasts capture persistent blocking accurately.
Reducing the guard would admit tiny predicted gains without addressing those limits.

The overloaded LondonFull AM15 seed 3 capacity control produces 10,379 evaluations and three distinct candidates with savings below 1.5 seconds.
Its free-flow and predictive arms have identical accepted requests, timings, pending records, terminus reports, and check counts.

The Acton mixed-burst fixture supplies substantial physical congestion.
Both arms accept the same 1,920 requests over two hours and allow a three-hour cutoff.
Buffers, sharing, pickup reassignment, and positioning stay off.
Both use virtual platoons of four and a queue limit of one million.

| Routing | Completed | Unfinished | Pickup-wait P95, seconds | Journey P95, seconds |
| --- | ---: | ---: | ---: | ---: |
| Free-flow | 1,029 | 891 | 6,000.00 | 6,511.88 |
| Predictive | 1,012 | 908 | 5,970.80 | 7,023.15 |

Acton forecasts delays up to 324.07 seconds and returns 418 alternative routes from 2,496 evaluations.
The largest predicted saving is 136.94 seconds.
These counters record returned proposals, not committed diversions.
Caller constraints can discard a proposal.
The predictive arm completes 17 fewer journeys and has a larger unfinished cohort.
Its smaller completed pickup-wait P95 does not offset the completion loss or larger journey P95.
Both arms remain overloaded.
This positive route-selection control provides no service benefit or basis for default adoption.
Free-flow routing remains the default.
