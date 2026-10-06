# Fixed station-entry platoon service screen

The fixed entry links change local ownership and movement without changing request service in these eight matched pairs.
Every accepted event, boarding tick, and completion tick matches its baseline.
The feature remains experimental, and station buffers remain off by default.
This screen finds no pickup or journey benefit.

## Sources and workload

The baseline is `f351a38` and the candidate is `7c8cc73`.
The only production changes are the fixed entry implementation and its version 4 persistence support.
Frozen source overlays, binary hashes, and mirrored project hashes identify both candidates independently.
All primary cells enable station buffers and four-pod virtual platoons.
Sharing, redistribution, and pickup reassignment remain off, with free-flow routing.
No authored geometry or physical clearance changes.

Two seeds run each workload:

- Acton Town in LondonFull: 239 outbound offers, split into bursts of 120 and 119, over a 20-minute arrival window.
  The cap is 90 minutes, with queue limits of 200 and one million.
- LondonCentral Early: approximately 10 offers/minute, one hour of arrivals, and a two-hour cap, with queue limit 200.
- LondonFull Morning: approximately 14 offers/minute, three hours of arrivals, and a four-hour cap, with queue limit 200.

Acton uses LondonFull because LondonCentral does not include Acton Town.
The profile rates come from discretized simulation ticks.
The measurement records retain the actual offered rates and schedule identities.
The buffer-enabled baseline is the comparator, not a buffer-off arm.

## Service and unfinished requests

Each row has identical baseline and candidate values.
Times use simulated seconds.
Boarding means and completed-journey means retain their existing distinct request cohorts.

| Workload | Seed | Queue | Completed | Aboard at cap | Skipped offers | Mean pickup wait | Mean journey |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Acton burst | 1 | 1,000,000 | 217 | 22 | 0 | 1,972.5 | 2,999.6 |
| Acton burst | 1 | 200 | 212 | 14 | 13 | 1,872.9 | 2,965.7 |
| Central Early | 1 | 200 | 545 | 0 | 54 | 1,205.2 | 1,680.3 |
| Full Morning | 1 | 200 | 1,856 | 11 | 654 | 1,050.9 | 2,042.9 |
| Acton burst | 2 | 1,000,000 | 214 | 25 | 0 | 1,972.5 | 3,072.2 |
| Acton burst | 2 | 200 | 211 | 15 | 13 | 1,872.9 | 3,049.7 |
| Central Early | 2 | 200 | 587 | 0 | 12 | 1,061.7 | 1,527.9 |
| Full Morning | 2 | 200 | 1,787 | 16 | 718 | 1,123.6 | 2,125.9 |

Accepted requests match by offered-event index, including schedules with skipped offers.
Matched timing records also match exactly.
Every joint boarded request has zero added pickup wait.
Every joint completed request has zero added journey time.
The pair records count unfinished requests separately.
All remaining accepted requests are already aboard in these cells.
The overloaded queue-limited profiles skip offers in both versions.
Their observed completion rates do not establish sustainable offered-demand capacity.

## Local links and movement

Fixed entry links form in all four candidate Acton cells.
They form in none of the selected Central or Full profile cells.
A zero-difference profile with no local links does not qualify active-path behavior in that band.
The Acton records show moving followers, draining links, and inherited resource ownership.
Their counters count linked followers, excluding platoon heads.
Shared-resource counters count follower/resource observations, not unique resources.

Acton stopped-pod and wait-reason samples change slightly, while boarding and completion ticks remain unchanged.
Coupled-time fractions also include the new local links.
Tiny floating-point distance differences do not change request timing.
These observations show the mechanism executing without a measured service gain.
They do not establish smaller gaps, greater station capacity, or an improvement under sustained overload.

## Validation and evidence

All 16 primary cells pass safety, speed, and request conservation checks once per simulated second.
Each passes two physical restores and a separate dense 60-second continuation with new buffer admissions disabled.
The 32 primary restores retain positions, counters, bindings, and certificate fields.
Six shorter pilots check every tick and match production aggregates exactly.
Their matched request timings also remain identical across versions.
Pure observer tests and independent review check the measurement helper.

[Arm measurements](measurements/station-entry-service-arms.csv), [matched pairs](measurements/station-entry-service-pairs.csv), and [metadata](measurements/station-entry-service.json) retain the screen.
Concurrent runs provide no CPU comparison.
See the [fixed entry contract](station-entry-platoons.md) for the safety and saved-state rules.
Broader mixed arrival/departure coverage and individual-service gates remain necessary before adoption.
