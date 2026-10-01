# Paddington spacing sensitivity

A narrower coherent Paddington layout completes more journeys in both selected seeds.
It still has a growing backlog and some longer individual journeys.
The trial does not change the scenario default or edited projects.

## Geometry and safety

The study freezes production source `e285e69` and the current LondonCentral project.
The four paired groups are diverge/merge, entry/exit, and each berth's arrival/departure nodes.
The study scales each pair's offsets around its midpoint.
Berth nodes stay fixed.
All other decoded project fields remain equal, including topology, separation groups, fleet, and demand.
Positive factors preserve the arrival/departure orientation.
The factors are 0.75, 1.25, and 1.50 relative to the current layout.

The [earlier partial layouts](paddington-layout.md) swap only the throat or berth-link endpoints.
They connect arrival and departure links across each other without a common node.
Those lanes share a separation group.
The junction conflict builder handles incident lanes, so it does not give these unconnected crossings a shared junction resource.
The physical separation check correctly rejects them.

Short replays on current source reproduce both failures with pods 037 and 057:

| Partial change | Failure tick | Separation, meters |
| --- | ---: | ---: |
| Throats only | 31,860 | 11.74649 |
| Berth links only | 32,760 | 11.40091 |

Both occur before the first historical restore checkpoint.
Changing separation groups, planes, or clearance would hide the geometry problem.
The coherent spacing fixtures preserve those fields and pass the existing checks.

## Matched service results

Each primary arm offers six hours of Early10/min demand and allows a seven-hour cap.
Seeds 1 and 2 use the same schedules and accepted requests as the current-layout baselines.
Each accepts all 3,599 offered requests.
Buffers, pickup reassignment, sharing, and positioning stay off.
Routes use free-flow costs and virtual platoons allow four pods.

Three short pilots check every tick and match production aggregates exactly.
All six primary arms pass safety, speed, and unique-order checks once per simulated second.
Physical restores at three and six hours retain poses, bindings, admission ages, and platoon records.
Each restored copy passes a separate 60-second continuation with dense checks.

| Spacing factor | Seed | Completed | Change from current | Unfinished | Mean completion-time change for jointly completed requests, seconds | Largest added completion time, seconds |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0.75 | 1 | 2,731 | +70 | 868 | -297.91 | 1,004.87 |
| 0.75 | 2 | 2,807 | +49 | 792 | -255.97 | 1,302.20 |
| 1.25 | 1 | 2,599 | -62 | 1,000 | +304.49 | 2,103.22 |
| 1.25 | 2 | 2,707 | -51 | 892 | +264.79 | 1,608.75 |
| 1.50 | 1 | 2,571 | -90 | 1,028 | +412.79 | 2,323.77 |
| 1.50 | 2 | 2,681 | -77 | 918 | +341.88 | 1,623.88 |

The 0.75 fixtures retain every request that completed in the current-layout baseline.
Their extra completions account for the full increase.
Every wider fixture loses some baseline completions and gains none at this cutoff.
The timing columns compare only requests that complete in both arms.
Unfinished cohorts remain separate because completed-only timing statistics can conceal later outcomes.

These results support testing shorter station paths.
They do not isolate one junction or prove that spacing alone sets station capacity.
The paired node changes also affect path lengths, conflict regions, and route costs.
The narrower layout adds about 17 and 22 minutes to the worst jointly completed journey in the two seeds.
Aggregate gains therefore do not establish acceptable individual service.

## Evidence

[Arm measurements](measurements/paddington-spacing-arms.csv) retain throughput, cutoff cohorts, individual timing changes, and check counts.
[Metadata](measurements/paddington-spacing.json) retains source and binary identities, fixture audits, and run results.
Raw outputs, immutable helpers, geometry fixtures, and rejected-partial logs remain in `~/.cache/agents/podsim/paddington-spacing-20261001/`.
The study stopped successfully and its scratch binary was removed.

These two seeds do not qualify other demand bands or authorize a default layout change.
