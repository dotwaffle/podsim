# LondonFull after the parking diversion fix

The post-fix study contains 308 arms: all 302 historical comparisons and six newly selected AM peak cases.
Six historical cases now recover, and none changes from successful recovery to failed recovery.
Twenty historical arms change at least one result field.
No fleet candidate achieves all-ten recovery at its next tested rate.
The preset and its defaults remain unchanged.

A recovery pass requires every scheduled request to finish by minute 120.
Passing safety and accounting assertions does not imply successful recovery.

## Changed recovery outcomes

Each case below previously ended with one unfinished request.
All now complete within the original two-hour cutoff.

| Candidate | Band | Requests/minute | Seed | Completion time |
| --- | --- | ---: | ---: | ---: |
| fleet300-local | Morning | 5 | 2 | 6,834 s |
| fleet287-local | AM peak | 10 | 1 | 6,769 s |
| fleet300-local | AM peak | 10 | 1 | 6,307 s |
| fleet300 | AM peak | 10 | 1 | 6,307 s |
| fleet287-local | AM peak | 10 | 3 | 7,095 s |
| fleet300-local | AM peak | 10 | 3 | 7,095 s |

## Method

The measured source is `c804c000282937a9e432d728ff24f126fee595b1`.
The historical source is `677e62bf6e108c2bdd00e2831948b323eec3bef3`.

The four project files, demand schedules, policies, arrival window, and recovery cutoff are unchanged.
Each arm has 60 minutes of arrivals and at most 60 additional minutes to finish.
Sharing and redistribution remain off, routing is free-flow, and virtual platooning remains enabled.
The baseline fleet has 287 pods.
The three candidate fleets and placements are unchanged.

The planner first replays all historical arms.
It then applies the original edge, boundary, candidate-screen, next-rate, and confirmation rules.
A historical-data check reproduced the original 302-arm selection exactly.
The new AM peak screens select fleet300 and fleet300-local for rate-15 tests with seeds 1 through 3.
fleet300 recovers in two of three seeds, and fleet300-local recovers in one.
Neither qualifies for seeds 4 through 10 at that rate.
This exploratory selection does not establish statistical significance or indefinitely sustainable demand.

## Baseline confirmation results

| Band | Requests/minute | Historical recovery | Post-fix recovery | Maximum unfinished |
| --- | ---: | ---: | ---: | ---: |
| Morning | 2 | 10/10 | 10/10 | 0 |
| Morning | 5 | 8/10 | 8/10 | 2 |
| AM peak | 10 | 9/10 | 9/10 | 1 |
| AM peak | 15 | 8/10 | 8/10 | 3 |
| Interpeak | 15 | 9/10 | 9/10 | 1 |
| Interpeak | 20 | 0/10 | 0/10 | 6 |
| PM peak | 10 | 10/10 | 10/10 | 0 |
| PM peak | 15 | 5/10 | 5/10 | 2 |
| Evening | 10 | 10/10 | 10/10 | 0 |
| Evening | 15 | 3/10 | 3/10 | 2 |
| Late | 10 | 10/10 | 10/10 | 0 |
| Late | 15 | 2/10 | 2/10 | 3 |

All 198 baseline arms retain their historical recovery classification.
Only selected ten-seed baseline groups appear above.
The CSV includes every initial rate, candidate, and confirmation arm.

At AM peak rate 10, seed 8 now ends with one unfinished request instead of two.
The parking fix lets request 577 finish, but the long Hillingdon journey still exceeds the cutoff.
See the [targeted diagnosis](london-full-diagnosis.md) for request timings.
Do not assume monotonic recovery between tested rates.

## Candidate selection

| Candidate | Band | Screen rate | Selected for next rate | Next rate | Recovered at next rate |
| --- | --- | ---: | --- | ---: | ---: |
| fleet300 | Morning | 2 | yes | 5 | 2/3 |
| fleet300-local | Morning | 2 | yes | 5 | 2/3 |
| fleet287-local | Morning | 2 | yes | 5 | 2/3 |
| fleet300 | AM peak | 10 | yes | 15 | 2/3 |
| fleet300-local | AM peak | 10 | yes | 15 | 1/3 |
| fleet287-local | AM peak | 10 | no | 15 | not selected |
| fleet300 | Interpeak | 15 | no | 20 | not selected |
| fleet300-local | Interpeak | 15 | yes | 20 | 1/3 |
| fleet287-local | Interpeak | 15 | no | 20 | not selected |
| fleet300 | PM peak | 10 | yes | 15 | 9/10 |
| fleet300-local | PM peak | 10 | yes | 15 | 1/3 |
| fleet287-local | PM peak | 10 | yes | 15 | 1/3 |
| fleet300 | Evening | 10 | yes | 15 | 9/10 |
| fleet300-local | Evening | 10 | yes | 15 | 1/3 |
| fleet287-local | Evening | 10 | yes | 15 | 1/3 |
| fleet300 | Late | 10 | no | 15 | not selected |
| fleet300-local | Late | 10 | yes | 15 | 1/3 |
| fleet287-local | Late | 10 | yes | 15 | 2/3 |

## Validation and evidence

All 308 arms have confirmed successful test-process exits.
They completed 2,032,514 safety and accounting observations, one per simulated second.
Each observation checks separation, finite state, speed, berth ownership, and request conservation.
The final census checks unique pending requests and rider timings against submitted and completed totals.
These sampled checks do not establish every-tick safety for the full matrix.

Each historical replay matches its original schedule ID, request count, seed, and demand band.
The same band, rate, and seed also have matching schedules across configurations.
The AM peak rate-10 seed-8 control matches every result field from the uninstrumented comparison function.

Pickup statistics include pending waits.
Journey statistics include completed journeys only.
For a failed recovery, a zero drain time is a sentinel, not an immediate completion.
The dataset retains failed recoveries and all measured fields.

[Per-arm measurements](measurements/london-full-postfix.csv) and [metadata](measurements/london-full-postfix.json) record the results and paired differences.
The original [capacity study](london-full.md#finite-arrival-study-september-29-2026) remains a historical record.
The [diagnosis](london-full-diagnosis.md) explains the parking diversion and long-journey cases.

Local evidence: `~/.cache/agents/podsim/londonfull-postfix-study-20260929/`.
It includes frozen projects, source, helper overlays, manifests, selection scripts, commands, logs, hashes, and exit markers.
