# Selected policy failures after the routing fixes

The current-source rerun still fails one platoon recovery comparison, three sharing journey-P95 comparisons, and one sharing recovery comparison.
Several other historical failures disappear in these selected cells.
Individual request regressions remain in most active-policy pairs.
These results do not qualify full demand bands or change operating defaults.

## Source and checks

All arms use frozen source `a54281e`, free-flow routing, and the archived projects.
The study contains four dense pilots and 32 primary arms.
The pilots compare observer and production result aggregates exactly.
Each primary arm offers 30 minutes of demand, permits a 65-minute cap, and stops when every request finishes.
The queue limit is 1,000,000.
All primary arms drain without skipped requests.
Each pair has identical schedules and requested ticks.

Every primary arm checks safety, speed, and unique-order accounting once per simulated second.
Physical restores at 15 and 30 minutes retain bindings, poses, admission ages, and saved platoon records.
No restore demotes, requeues, or drops a request.
Each copied continuation runs 60 seconds of dense checks with new buffer, pickup-reassignment, and positioning admissions disabled.
The restored copy retains its arm's platoon mode.
Concurrent functional runs do not provide CPU comparisons.

The rerun preserves recorded request intervals, including their historical rounding.
It selects prior failures and boundary controls, so it is not a representative sample.
Historical results remain separate from current-source comparisons.
No individual improvement proves that one earlier patch caused it.

## Virtual platoons

These four pairs repeat the selected [platoon failures](platoon-followup.md#targeted-diagnosis-after-junction-priority).
Only platooning changes, from off to virtual with four pods.
Sharing, buffers, pickup reassignment, and positioning remain off.
The historical recovery rule uses a 3,600-second threshold.
The designated London114 controls allow at most two additional seconds of mean wait and 2% additional empty distance.

| Fixture | AM rate/min | Seed | Off / virtual end, seconds | Mean wait change, seconds | Empty-distance ratio | Selected rule |
| --- | ---: | ---: | --- | ---: | ---: | --- |
| London198 | 24 | 2 | 3,588 / 3,767 | -1.16 | 1.0179 | Recovery fails |
| London114 | 12 | 1 | 3,746 / 3,734 | +0.99 | 1.0151 | Control passes |
| London114 | 12 | 2 | 3,136 / 3,139 | +0.14 | 1.0051 | Control passes |
| London114 | 13 | 2 | 3,273 / 3,292 | -0.22 | 1.0023 | Control passes |

London198 still crosses the recovery threshold only with virtual platoons.
The three designated controls pass their historical mean-wait and empty-distance limits in this rerun.
Both London114 AM12 seed-1 arms now exceed one hour, although both drain within 65 minutes.
Its virtual journey P95 also increases by 2.21%.
That additional comparison does not rewrite the historical control rule.
Current passing controls do not erase the remaining recovery failure.

## Assigned-party sharing

These eight pairs repeat the six P95 failures and two deadline failures in the [all-band record](qualification.md#all-band-result).
Both arms use four parties, drop-offs mode, three intermediate stops, a 1.5 detour cap, and virtual platoons.
Only the join policy changes from `unassigned` to `reassign-existing`.
Pickup reassignment, buffers, and positioning remain off.
The historical service guard permits at most a 2% increase in each paired mean journey and journey P95.

| Band | Nominal rate/min | Seed | Journey mean change | Journey P95 change | Unassigned / reassign end, seconds | Remaining failure |
| --- | ---: | ---: | ---: | ---: | --- | --- |
| AM peak | 13 | 1 | -2.134% | +1.465% | 3,513 / 3,313 | None of these two rules |
| Interpeak | 11 | 3 | +0.792% | +3.174% | 2,681 / 2,635 | P95 |
| Interpeak | 15 | 3 | -1.537% | -0.472% | 3,505 / 3,332 | None of these two rules |
| Late | 9 | 1 | +0.373% | +2.930% | 2,941 / 2,984 | P95 |
| Morning | 4 | 1 | +0.188% | +2.093% | 2,869 / 2,869 | P95 |
| PM peak | 11 | 3 | +0.141% | -0.283% | 2,620 / 2,612 | None of these two rules |
| Late | 13 | 2 | -4.786% | -2.648% | 3,477 / 3,655 | Recovery |
| Morning | 13 | 1 | -3.353% | -2.150% | 3,650 / 3,495 | None of these two rules |

Three prior P95 failures remain, and three pass on current source.
Late13 still loses the one-hour recovery comparison.
Morning13 no longer loses that comparison, but its current `unassigned` baseline itself ends after one hour.
A targeted passing pair cannot raise a band capacity limit.

Every candidate P95 request in these eight pairs belongs to neither the reassigned-party group nor its shared-journey host group.
This classification uses the candidate's timing records and shared lead IDs.
It does not establish that the broader policy had no indirect effect on those requests.
The metadata retains each P95 rank separately from same-request changes.

Interpeak11's candidate P95 is request 211.
It rides in pod 090 in both arms, with identical ride duration and 40.87 seconds of additional pickup wait.
Its prior carried request 104 completes at the same tick in both arms.
These records narrow the difference to the interval before request 211 boards.
They do not identify the exact later parking, diversion, or assignment decision.
Request 240 has 138.03 additional seconds of pickup wait with unchanged ride duration and a different boarding pod.
Request 226 retains identical timing as an unchanged comparison within this pair.

Late13 ends with request 388 in the candidate.
It gains 178.70 seconds of pickup wait, has unchanged ride duration, and boards a different pod.
It is neither reassigned nor a shared-journey host.
These records identify pickup waiting as the changed part of this deadline witness.
They do not identify a new dispatch defect.

## Guarded positioning controls

Four off/on pairs preserve platooning off and sharing off.
They cover selected low-rate boundary cases and a high-rate identity control.
All eight arms drain, and the six-per-minute AM pair has exactly equal result fields except the policy name.
Every request in that pair has identical boarding and completion ticks, with zero positioning moves.

| Band | Rate/min | Seed | Mean wait change, seconds | Empty-distance ratio | Off / on end, seconds | Positioning moves |
| --- | ---: | ---: | ---: | ---: | --- | ---: |
| Night | 5 | 3 | -5.83 | 0.9441 | 3,333 / 3,358 | 22 |
| Night | 4 | 1 | -21.88 | 0.9761 | 3,236 / 3,276 | 31 |
| Interpeak | 1 | 1 | -13.86 | 1.4583 | 2,269 / 2,269 | 10 |
| AM peak | 6 | 1 | 0.00 | 1.0000 | 2,689 / 2,689 | 0 |

The selected Interpeak cell trades more empty travel for a shorter mean pickup wait.
Its single-seed distance ratio cannot evaluate the historical rules for complete band and rate means.
These controls do not replace a full guarded-positioning qualification.

## Individual outcomes and evidence

[Pair totals](measurements/policy-failures-pairs.csv) retain maximum individual changes and the [adoption-limit](experimental-adoption.md) counts.
Every active sharing pair has requests that exceed those individual limits.
The active positioning controls and most platoon pairs also have individual increases.
The user approved the limits on October 1.
These selected counts do not establish full qualification or authorize a default change.

[Arm totals](measurements/policy-failures-arms.csv) retain recovery, safety checks, and policy activity.
[Metadata](measurements/policy-failures.json) retains group statistics, selected request tails, P95 ranks, source hashes, and run outcomes.
Complete per-request comparisons and raw one-second diagnostics remain in `~/.cache/agents/podsim/policy-failures-20261001/`.
Observed pickup pods, stopping, and coupling counts are samples, not complete transition or resource-grant histories.
The owned run stopped successfully, and its scratch binary was removed.
The [fleet-history follow-up](policy-fleet-history.md) traces earlier assignments and empty destinations behind selected pickup delays.
It separates percentile ranks from same-request changes and retains the remaining failures.
