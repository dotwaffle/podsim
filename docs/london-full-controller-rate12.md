# LondonFull controllers at 12 requests per minute

All 16 runs pass the safety, request census, and physical-restore checks.
Late backlog changes range from -0.128 to +0.094 orders per minute across four seeds.
Every request boards before its run ends.
Some parties remain aboard at the seven-hour cap.
These finite windows do not establish indefinite stability or justify default adoption.

## Scope

This extends the [24-arm controller study](london-full-controller-sustained.md) with 12 requests per minute and seeds 1 through 4.
Each seed compares baseline, buffers, pickup reassignment, and both.
The source, frozen binary, Full geometry, AM Peak profile, and validation helpers are unchanged.
Source `bab8559` uses 269 passenger stations, three Parking sites, and 287 initial pods.
Each arm receives 4,319 identical ordered requests over six hours, with up to one hour for recovery.
The study queue limit is 1,000,000, while the live limit remains 200.
Sharing and redistribution stay off, routing is free-flow, and virtual platoons have a four-pod limit.

Safety, speed, and conservation checks run once per simulated second.
Physical restores at three and six hours retain assignments without demotion, requeue, or dropped parties.
Each restored copy continues for 60 seconds with both controllers disabled and checks every tick.
The original study's four production-equivalence pilots cover the unchanged helper and source.
No additional equivalence pilot runs at 12/min.
Concurrent functional jobs do not support CPU timing claims.

## Completion, backlog, and exact waits

Late backlog change measures outstanding orders from hour three to hour six, divided by 180 minutes.
All mean waits below are exact request-to-boarding waits, because no request remains unboarded.
Completion counts include recovery and do not count parties still aboard.

| Seed | Policy | Completed / accepted | Late backlog orders/min | Mean wait, seconds |
| ---: | --- | ---: | ---: | ---: |
| 1 | Baseline | 4,317 / 4,319 | -0.006 | 349.02 |
| 1 | Reassignment | 4,319 / 4,319 | -0.044 | 345.78 |
| 1 | Buffers | 4,317 / 4,319 | -0.056 | 329.04 |
| 1 | Both | 4,318 / 4,319 | -0.067 | 319.13 |
| 2 | Baseline | 4,315 / 4,319 | +0.039 | 363.25 |
| 2 | Reassignment | 4,315 / 4,319 | +0.039 | 355.04 |
| 2 | Buffers | 4,315 / 4,319 | +0.000 | 333.05 |
| 2 | Both | 4,315 / 4,319 | +0.033 | 331.51 |
| 3 | Baseline | 4,318 / 4,319 | +0.083 | 356.92 |
| 3 | Reassignment | 4,318 / 4,319 | +0.094 | 353.66 |
| 3 | Buffers | 4,318 / 4,319 | +0.089 | 332.22 |
| 3 | Both | 4,318 / 4,319 | +0.094 | 331.71 |
| 4 | Baseline | 4,319 / 4,319 | -0.100 | 358.31 |
| 4 | Reassignment | 4,319 / 4,319 | -0.094 | 357.73 |
| 4 | Buffers | 4,319 / 4,319 | -0.122 | 334.66 |
| 4 | Both | 4,319 / 4,319 | -0.128 | 331.96 |

Every seed-4 arm completes all requests.
The other arms finish with zero to four parties aboard, recorded by ID in the metadata.
The near-flat late backlogs distinguish this workload from the uniformly growing backlogs at 15 and 20/min.
They do not prove sustainable capacity under other demand bands or longer arrival windows.

## Matched individual outcomes

Each policy comparison includes all 4,319 boarded requests.
Buffer-only mean wait improvements range from 19.98 to 30.20 seconds across seeds.
Reassignment alone improves means by 0.58 to 8.21 seconds.
Both improve means by 25.21 to 31.74 seconds.

Every comparison still contains individual increases above 300 seconds.
Counts range from 83 to 119 requests per comparison.
The largest increase is 5,404.73 seconds for request 1324 in seed 4 with reassignment alone.
Its wait rises from 481.02 to 5,885.75 seconds, although it has no recorded direct reassignment.
It completes in both arms.
This is an indirect service difference between fleet histories, not a recorded swap's prediction error.
The 300-second threshold describes the data and is not an approved adoption limit.

Buffers and reassignment remain off by default.
Individual-tail investigation, broader demand coverage, and numeric adoption limits remain open.

## Evidence

[Arm totals](measurements/london-full-controller-rate12.csv) retain service, backlog, controller activity, and stopped-time measurements.
[Matched summaries and metadata](measurements/london-full-controller-rate12.json) retain all raw-result hashes, run outcomes, and unfinished aboard IDs.
