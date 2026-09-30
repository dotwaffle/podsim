# LondonFull focused comparison at nominal 13/min

Baseline and combined controllers retain nearly flat late backlogs in both tested seeds.
All requests board, although one to three parties remain aboard at the seven-hour cap.
The combined policy lowers mean waits by about 21 to 22 seconds, with individual increases above 1,600 seconds.
These finite runs do not establish indefinite stability or justify default adoption.

## Scope and checks

This follows the [12/min four-seed comparison](london-full-controller-rate12.md).
It tests baseline and both controllers together at nominal 13 requests per minute, seeds 1 and 2.
It does not test either controller alone at this rate.
The tick schedule uses 4.6-second intervals, about 13.043 requests per minute, and contains 4,695 requests per arm.
All arms receive identical ordered schedules within each seed.

The frozen `bab8559` source, Full geometry, AM Peak profile, and qualification helpers match the [original controller study](london-full-controller-sustained.md).
The project has 269 passenger stations, three Parking sites, and 287 initial pods.
Arrivals continue for six hours, followed by up to one hour without arrivals.
The study queue limit is 1,000,000, while the live limit remains 200.
Sharing and redistribution stay off, routing is free-flow, and virtual platoons have a four-pod limit.

All four arms pass once-per-second safety, speed, and conservation checks.
Each restores physically at three and six hours, then checks a 60-second disabled-controller continuation every tick.
No party is demoted, requeued, or dropped during restore.
The original equivalence pilots cover the unchanged source and helper, with no additional pilot at this rate.
Concurrent functional jobs do not support CPU timing claims.

## Results

Late backlog change measures outstanding orders from hour three to hour six, divided by 180 minutes.
All means are exact boarded waits because no request remains unboarded.
Completion includes recovery and excludes parties still aboard.

| Seed | Policy | Completed / accepted | Late backlog orders/min | Mean wait, seconds |
| ---: | --- | ---: | ---: | ---: |
| 1 | Baseline | 4,693 / 4,695 | -0.044 | 371.58 |
| 1 | Both | 4,694 / 4,695 | -0.033 | 349.55 |
| 2 | Baseline | 4,692 / 4,695 | +0.011 | 378.33 |
| 2 | Both | 4,692 / 4,695 | -0.028 | 357.49 |

Mean matched wait improves by 22.03 seconds in seed 1 and 20.85 in seed 2.
The largest individual increases are 1,638.73 and 2,309.92 seconds.
There are 136 and 134 requests with increases above 300 seconds.
That threshold describes outcomes and is not an approved adoption limit.

These late windows remain nearly flat, unlike every tested arm at 15 and 20/min.
They do not establish a sustainable capacity envelope, other demand bands, or effects of each controller separately at this rate.
Buffers and pickup reassignment remain off by default.

## Evidence

[Arm totals](measurements/london-full-controller-rate13.csv) retain service, waits, backlog, activity, and stopped-time measurements.
[Matched summaries and metadata](measurements/london-full-controller-rate13.json) retain raw-result hashes, run outcomes, and unfinished aboard IDs.
Per-request deltas and raw logs remain in `~/.cache/agents/podsim/sustained-full-20260930/rate13/`.
