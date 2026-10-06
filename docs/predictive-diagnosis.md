# Predictive route-selection diagnosis

The six earlier service pairs forecast little avoidable traffic delay.
Their unchanged routes follow from the forecast costs and existing guards.
A congested Acton trial returns alternatives, but completes fewer journeys.
Free-flow routing remains the default.

## Rejection counts

Counter-only observers replay all six [service cells](predictive-service.md) on frozen production source `e285e69`.
Every result, schedule, accepted request, timing, pending record, terminus report, and restore/check count matches its historical predictive arm exactly.
The observer records costs after the original routing decisions without running extra searches.
Observer purity and controlled guard tests pass, including a positive alternative-route case.

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

## Capacity and congestion controls

The overloaded LondonFull AM15 seed 3 trial supplies a separate capacity control.
Its 10,379 evaluations produce three distinct candidates with savings below 1.5 seconds.
Free-flow and predictive arms have identical accepted requests, timings, pending records, terminus reports, and check counts.
Their result fields differ only in the routing label.

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
The changed service outcomes do not establish that each proposed route was used.

The predictive arm completes 17 fewer journeys and has a larger unfinished cohort.
Its smaller completed pickup-wait P95 does not offset the completion loss or larger journey P95.
Both arms remain overloaded.
This positive route-selection control provides no service benefit or basis for default adoption.

## Checks and evidence

All primary arms retain once-per-second safety, speed, and unique-order checks.
Each physical restore retains poses, bindings, admission ages, and saved platoon records.
Each restored copy passes a separate 60-second continuation with dense checks.
Short pilots check every tick and match production aggregates exactly.
Concurrent functional runs do not provide CPU comparisons.

[Counts](measurements/predictive-diagnosis-arms.csv) retain all six service-cell rejection totals.
[Metadata](measurements/predictive-diagnosis.json) retains costs, examples, exact historical checks, and source/helper/binary identities.
The studies stopped successfully and their scratch binaries were removed.
