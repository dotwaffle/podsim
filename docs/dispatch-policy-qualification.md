# Dispatch policy qualification

Both controllers remain opt-in.
The completed pickup reassignment reduces average waits in these schedules, but some individual requests take much longer.
Buffers now drain the two Acton bursts after a remote berth-claim fix, but increase their waits and upstream stopped time.
These results do not support default enablement.

## Inputs and checks

Commit `357d214` fixes remote claim yielding after the buffer and reassignment extensions in `3ffd3b3` and `a7f5fd0`.
Separate saved editor controls and comparison arms are available at `e916800`.
Each of five schedule groups runs baseline, buffers, reassignment, and both together, for 20 arms.
Within a group, ordered request schedules are identical.
Sharing and redistribution are off; routing is free-flow; virtual platoons have a four-pod limit.

The Acton groups contain 128 mixed requests in eight minutes, with seeds 1 and 2 and a 90-minute cap.
They include arrivals via Turnham Green, departures, and pickups.
LondonCentral and LondonFull use AM Peak demand at 15 requests per minute for 60 minutes, with a 120-minute cap and seed 1.
Each schedules 899 requests.
The rail-hub control schedules 119 hub-burst requests in ten minutes, with a 60-minute cap.
These are finite-arrival comparisons, not sustained capacity estimates.

All accepted request IDs reconcile with completed, aboard, or pending records.
No demand is skipped.
Safety, speed limits, and request conservation are checked once per simulated second.
Two checkpoints per arm restore physical state without demotions, drops, or requeues.
Each restored copy continues for 60 seconds with both policies disabled and checks every tick.
Four smaller Acton pilots also pass every-tick checks and exact production-result comparisons.
These checks do not prove indefinite deadlock freedom.

An initial job generator accidentally ran all four labels with policies off.
That matrix is retained as control-only evidence and excluded from qualification.
The corrected matrix verifies job labels against actual result settings.
Helpers, inputs, source hashes, and raw outputs are retained in the local cache.
Functional runs overlap other checks, so their wall times are not CPU measurements.

## Matched request results

Values below are changes from baseline, in seconds.
Negative values improve service.
Mean wait includes every accepted request; a request still waiting at the cap contributes its elapsed wait, a lower bound.
Journey comparisons use only requests completed in both arms.
Maximum regression is the largest increase for one matched completed journey, not the difference between two aggregate maxima.

| Group | Policy | Mean wait change | Mean matched journey change | Largest journey regression | Journeys more than 300 s worse |
| --- | --- | ---: | ---: | ---: | ---: |
| Acton seed 1 | Buffers | +62.45 | +102.94 | 239.30 | 0 |
| Acton seed 1 | Reassignment | -26.15 | -11.25 | 85.58 | 0 |
| Acton seed 1 | Both | +35.80 | +156.63 | 416.13 | 20 |
| Acton seed 2 | Buffers | +72.66 | +116.43 | 285.65 | 0 |
| Acton seed 2 | Reassignment | -12.91 | -3.05 | 42.68 | 0 |
| Acton seed 2 | Both | +54.04 | +179.54 | 470.97 | 25 |
| Central | Buffers | -50.29 | -52.57 | 887.17 | 16 |
| Central | Reassignment | -204.98 | -204.91 | 780.15 | 3 |
| Central | Both | -262.33 | -263.91 | 714.10 | 4 |
| Full | Buffers | -3.25 | -5.49 | 449.27 | 7 |
| Full | Reassignment | -0.59 | -0.44 | 655.97 | 6 |
| Full | Both | -8.13 | -10.42 | 407.67 | 4 |
| Rail hub | Buffers | 0.00 | 0.00 | 0.00 | 0 |
| Rail hub | Reassignment | -4.43 | -5.61 | 55.55 | 0 |
| Rail hub | Both | -4.43 | -5.61 | 55.55 | 0 |

Both Acton groups complete all requests in every arm.
Central completes all 899; Full completes 897 in baseline, 896 with reassignment, and 897 with buffers or both.
Rail hub completes all 119.
Full's unfinished requests prevent uncensored whole-cohort journey comparisons.

The controller requires both estimated pickup costs to improve for an assigned pair.
Those free-flow estimates do not account for later resource contention or effects on other requests.
In Central's reassignment arm, 454 distinct requests participate in decisions.
Their mean wait improves by 241.38 seconds, but 33 wait longer, including two by more than 300 seconds.
Among the other 445 requests, mean wait improves by 167.85 seconds; 23 wait longer, including one by more than 300 seconds.
Thus, tail regressions affect both changed assignments and surrounding traffic.
The current evidence supports continued experiments, not a realized per-request improvement guarantee.
Numeric adoption limits remain a decision after causal tail investigation and broader seeds.

## Actual buffer use and the stall fix

Empty buffered pods occupy eligible entry lanes, with a peak of three in the Acton groups and four in Full.
This sensor counts flagged entry pods without riders, including released empty members; it does not distinguish active pickups.
At Acton, buffer-only entry occupancy peaks at three pods.
This demonstrates use of the holding region without station-entry platoons.
It does not establish a smaller stopped separation or a new queue capacity.

Acton upstream stopped pod-seconds increase from 210 to 3,455 for seed 1 and from 184 to 3,882 for seed 2.
Network mainline stopped pod-seconds increase from 5,084 to 7,723 and from 3,889 to 8,371.
Moving pods into an entry lane does not by itself prevent the queue from reaching the mainline.

Before the fix, buffer-only seed 1 stopped at 99 completed requests out of 128; seed 2 stopped at 121.
The final state revealed a cycle at Turnham Green.
A berthless passenger head waited for a berth claimed by a released empty pod behind it.
The empty pod could not reach that berth through the queue.
Ordinary passenger arrivals can make such unadmitted empty claims yield, but berthless heads missed that path.

The fix applies that eligibility rule during a complete-path trial.
Physical holdings, admitted destination paths, and assigned pickups remain protected.
A denied trial restores the claims without changing the remote pod.
After a successful trial, released pods reroute in the next dispatch pass.
Rerouting inside admission could invalidate a queued intent; a regression reproduces that panic and verifies deferred rerouting with immediate physical restore.
After the fix, both Acton groups finish all 128 requests with buffers.

## Evidence

[Arm totals](measurements/dispatch-policy-qualification.csv) retain completion, waits, actual occupancy, and stopped-time counts.
[Metadata and matched summaries](measurements/dispatch-policy-qualification.json) retain source and raw-result hashes, cohorts, and direct versus other request results.
Raw request deltas, decisions, restore records, and frozen helpers are in `~/.cache/agents/podsim/dispatch-qualification-20260930/post-fix/`.
The pre-fix matrix and Turnham Green diagnostic are in its parent directory.
