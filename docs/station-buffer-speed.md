# Acton buffer discharge and station speed

The Acton buffer trial claims berths earlier, but each incoming claim lasts longer.
Lower station speeds further reduce completions in this fixture.
These results support neither buffer default adoption nor a lower station speed.

## Matched fixture

All arms use frozen production source `e285e69` and the Acton mixed-burst fixture.
They accept the same 1,920 requests over two hours and permit a three-hour cutoff.
Sharing, pickup reassignment, and positioning remain off.
Routing uses free-flow costs and virtual platoons permit four pods.
The queue limit is one million.

The 14 m/s diagnostic reproduces the earlier buffer arms exactly.
Results, schedules, accepted requests, timings, pending records, station reports, and check counts match.
Observers count calls and grants without making extra admission decisions.
Purity tests and dense pilots pass.

The speed trials change only the limits on Acton's 19 station-tagged lanes.
They include approaches, berth links, the bypass, and exits.
Other decoded project fields remain identical.
These are station-wide sensitivity trials, not isolated tests of following gaps.

## Incoming berth claims

The observer samples incoming berth claims once per simulated second.
An episode starts when a pod holds an incoming claim and ends when that claim changes or becomes occupancy.
The table excludes right-censored episodes from duration statistics and reports their count separately.
Short episodes can escape sampling.

| Speed, m/s | Buffers | Completed | Unfinished | Completed claim episodes | Censored episodes | Mean claim, seconds | Claim P95, seconds | Buffer grants |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 14 | Off | 1,029 | 891 | 622 | 0 | 7.51 | 9 | 0 |
| 14 | On | 907 | 1,013 | 559 | 1 | 25.01 | 28 | 557 |
| 10 | Off | 815 | 1,105 | 495 | 0 | 7.14 | 8 | 0 |
| 10 | On | 797 | 1,123 | 488 | 2 | 30.44 | 35 | 486 |
| 7 | Off | 621 | 1,299 | 389 | 0 | 7.10 | 8 | 0 |
| 7 | On | 613 | 1,307 | 389 | 1 | 39.79 | 46 | 388 |

At 14 m/s, the buffer makes 557 grants from 384,727 calls.
The grant requires a safe complete suffix beyond the fixed stopping frontier.
The mean remaining free-flow travel at a grant is 18.75 seconds.
It increases to 25.55 and 36.52 seconds at 10 and 7 m/s.
Berth ownership therefore covers substantial travel before boarding or unloading.
The longer sampled claims accompany fewer completions.
This association does not isolate all causes of the buffer regression.

Complete-path denials name arrival cells, the arrival node, and berth nodes.
These resources protect intersecting paths.
They are not evidence that the clearance or ownership rules can be removed.
The counters classify post-call wait labels, which do not identify every early-return cause.
The shared first-30-event limit can omit later grant examples.
Aggregate counters are complete.

## Lower-speed assumption

Lower speeds reduce braking distance and the speed-dependent part of the following gap.
The trial retains the track-cell geometry, 12-meter physical clearance, and the fixed stopping frontier.
Those unchanged constraints can still determine admission spacing.
Lower limits also increase approach and exit travel time and change route costs.
Mixed speed limits can affect ordinary platoon eligibility.

At 10 m/s, completions decrease by 214 with buffers off and 110 with buffers on.
At 7 m/s, the decreases are 408 and 294.
All six arms remain overloaded.
A smaller gap at lower speed does not establish higher throughput for this geometry and safety model.
Keep the existing speed limits and reservations.

## Checks and evidence

All primary arms retain once-per-second safety, speed, and unique-order checks.
Physical restores retain poses, route bindings, admission ages, and saved platoon records.
Each restored copy passes a separate 60-second continuation with dense checks.
Short pilots check every tick and match production aggregates.
Concurrent functional runs do not provide CPU comparisons.

The raw measurement data is in git history.
Their owned units terminated successfully and their scratch binaries were removed.

A future buffer change must preserve the complete-path safety contract and demonstrate better service on matched individual cohorts.
These selected trials do not justify a geometry change or weaker reservations.
