# LondonFull focused comparison at nominal 14/min

Baseline results differ between the two seeds at this rate.
Seed 1 retains a nearly flat late backlog, while seed 2 grows by 1.683 orders per minute.
The combined controller policy retains nearly flat late backlogs in both seeds.
Individual wait regressions remain, so these results do not justify default adoption or establish indefinite capacity.

## Scope and checks

This follows the [13/min focused comparison](london-full-controller-rate13.md).
It tests baseline and both controllers together at nominal 14 requests per minute, seeds 1 and 2.
It does not test either controller alone at this rate.
The tick schedule uses 257-tick intervals, about 4.283 seconds, and contains 5,042 requests per arm.
Ordered schedules match within each seed.

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

## Results and censoring

Late backlog change measures outstanding orders from hour three to hour six, divided by 180 minutes.
Completion includes recovery and excludes parties still aboard.
The wait-or-age mean includes exact boarded waits and elapsed lower-bound ages for requests still unboarded at the cap.

| Seed | Policy | Completed / accepted | Pending / aboard at cap | Late backlog orders/min | Mean wait-or-age, seconds |
| ---: | --- | ---: | ---: | ---: | ---: |
| 1 | Baseline | 5,040 / 5,042 | 0 / 2 | -0.050 | 398.74 |
| 1 | Both | 5,041 / 5,042 | 0 / 1 | -0.044 | 376.38 |
| 2 | Baseline | 5,017 / 5,042 | 3 / 22 | +1.683 | 843.09 |
| 2 | Both | 5,041 / 5,042 | 0 / 1 | -0.028 | 397.56 |

Seed 1 compares exact waits for all 5,042 requests.
The combined mean improves by 22.36 seconds, but 188 requests wait over 300 seconds longer.
The largest individual increase is 1,952.70 seconds.

Seed 2 compares exact waits only for the 5,039 requests boarded in both arms.
Their matched mean improves by 444.29 seconds, with 94 increases above 300 seconds and a largest increase of 1,374.18 seconds.
The three remaining baseline requests have censored ages, so their eventual baseline waits are unknown.
Their cohort and observed ages remain separate in the metadata.
The 300-second threshold describes outcomes and is not an approved adoption limit.

## Interpretation

The intermediate-rate studies distinguish nearly flat tested late windows from the uniformly growing backlogs at 15 and 20/min.
They do not establish a sustainable rate limit or performance under other demand bands.
The [Stratford intermediate-berth diagnosis](pickup-seed4-tail.md) shows a reciprocal resource wait in a separate 12/min seed-4 run.
This report does not attribute the 14/min seed-2 backlog to that same cycle.
Routing obstructions and individual service tails need further qualification before an adoption recommendation.
Buffers and pickup reassignment remain off by default.

## Evidence

[Arm totals](measurements/london-full-controller-rate14.csv) retain service, waits, backlog, activity, and stopped-time measurements.
[Matched summaries and metadata](measurements/london-full-controller-rate14.json) retain raw-result hashes, run outcomes, and unfinished pending and aboard IDs.
Per-request deltas and raw logs remain in `~/.cache/agents/podsim/sustained-full-20260930/rate14/`.
