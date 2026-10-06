# Paddington shared-resource clearance samples

Mirrored geometry produces more selected waiting samples, with similar resource-release distances.
The measurements give no basis for weakening the predecessor-coverage guard.
They cover one tick after each sampled rejection, not the full wait before a cell clears.
No production rule changes.

## Method and validation

This extends the [movement probe](paddington-motion.md) with paired ownership observations.
The observer uses historical source `9cb066f` to preserve those runs.
It excludes the later [berth routing fixes](berth-route-preference.md).

Two short pilots match production results exactly.
Four longer runs use earlier or mirrored Central geometry, seeds 1 and 2, and virtual platoons.
Each offers 10 requests per minute for six hours, with a seven-hour cap.
Buffers, reassignment, sharing, and redistribution remain off.
Every result, ordered schedule, and earlier progress and movement measurement matches its frozen history exactly.
Safety, speed, and request accounting pass once per simulated second.
Final request IDs reconcile, with no skipped requests.
These runs add no physical-restore qualification.

The observer samples every sixth simulation tick, using the earlier probe's selected stopped-follower rejection condition.
This fixed phase can bias the sample.
Counts represent weighted rejection or resource observations, not unique pods, complete waiting episodes, or independent observations.

At rejection, the observer reads the requested span and older retained resources that the follower does not own.
Those older holdings matter because `holdsPending` can block an independent reservation.
It deduplicates resource IDs within each observation and records ownership and the owner's release threshold.
It then checks predecessor coverage after all admissions and ownership after the same tick's movement and releases.
That final sample precedes the next tick's admissions.

Pure lookups leave routes, physical state, ownership, exports, and lookup cursors unchanged in the regression.
The regression also checks deduplication, admission coverage, and explicit route-version changes.
Independent review checks the observer and measurement limits.

## Retained shared resources

Release distance means the owner's signed `releaseAt - distance` at rejection.
The mean weights resource observations, including repeated observations of the same cell or owner.
It does not measure travel time or require constant speed.

| Seed | Geometry | Sampled rejections | Immediate predecessor resources | Earlier ancestor resources | Predecessor mean release distance, m |
| --- | --- | ---: | ---: | ---: | ---: |
| 1 | Earlier | 74,423 | 51,914 | 35,688 | 11.97 |
| 1 | Mirrored | 856,213 | 591,049 | 443,867 | 11.49 |
| 2 | Earlier | 71,535 | 49,600 | 34,150 | 12.25 |
| 2 | Mirrored | 650,431 | 445,764 | 341,143 | 11.64 |

Every retained shared resource has an immediate predecessor or an earlier platoon ancestor as owner.
Every immediate predecessor has a recorded release threshold more than one meter ahead.
Most thresholds lie within 12 meters, with some between 12 and 30 meters and few beyond 30 meters.
All retained shared resources keep the same owner through the sampled tick.
No observed route-version or link changes occur within those paired samples.
This short horizon cannot establish how long those resources remain owned afterward.

## Requested cells

Requested resources form a separate denominator from the retained shared resources above.
Most have an owner outside the follower's current predecessor chain at rejection.
That classification does not identify the owner's position or its role in a downstream reservation dependency.

| Seed | Geometry | Requested resources | Owner mean release distance, m | Same owner after tick | Newly unowned after tick |
| --- | --- | ---: | ---: | ---: | ---: |
| 1 | Earlier | 74,882 | 8.94 | 74,324 | 97 |
| 1 | Mirrored | 856,213 | 8.42 | 854,643 | 878 |
| 2 | Earlier | 71,535 | 8.68 | 71,353 | 121 |
| 2 | Mirrored | 650,431 | 8.50 | 649,162 | 734 |

The mean excludes resources that have no owner at rejection.
Every owned requested resource has a recorded release threshold.
Some requested resources change owners during the tick.
The unowned counts describe an ownership outcome, not its release mechanism or a successful next-tick admission.

After all admissions, the predecessor covers the requested frontier in 13, 112, 16, and 93 sampled rejections, respectively.
Coverage alone does not prove that the follower's earlier rejection becomes a grant or that its older shared holdings clear.

## Limits and remaining work

The sampled release distances remain similar across the two geometries despite the large increase in rejection counts.
This agrees with the earlier evidence of moving queues and ordinary track ownership downstream.
It does not isolate a geometric parameter or establish a persistent reservation defect.

A longer observation would need complete resource-identity histories through release, ownership transfer, and the next admission.
It must separate route and certificate changes from continued waiting and retain censored observations.
That extension remains separate work.
Fixed station-entry platoons have a separate [contract](station-entry-platoons.md).
The buffer and reassignment defaults remain off.

[Arm totals](measurements/paddington-clearance-arms.csv), [resource bins](measurements/paddington-clearance-rows.csv), and [metadata](measurements/paddington-clearance.json) retain the measurements.
Concurrent diagnostic jobs make their wall times unsuitable for CPU comparisons.
