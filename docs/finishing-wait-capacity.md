# Strict finishing-pod waits at the AM capacity boundary

The existing strict finishing-pod wait rule reduces completions in all four selected LondonFull capacity cells.
It also increases pickup-wait P95 and empty distance.
Keep the current rule.

## Rules and trials

The current rule can wait for a finishing pod whose estimated arrival beats an idle pod.
That estimate can exceed the 30-second hold budget.
The hold expires after 30 seconds if the pod does not become available.
The strict rule considers the finishing pod only when its estimate fits the remaining hold budget.
Both rules retain the same safety and admission checks.

All arms use frozen source `e285e69`, the existing 287-pod LondonFull project, and 2024 endpoint demand.
They offer six hours of AM demand and permit a seven-hour cutoff.
AM14 and AM15 each use seeds 3 and 4.
Matched arms accept identical requests at identical ticks.
Only the existing finishing-wait rule changes.
Buffers, sharing, pickup reassignment, and positioning remain off.
Both use free-flow routing and virtual platoons of four.

| Rate/min | Seed | Current / strict completions | Current / strict unfinished | Current / strict pickup-wait P95, seconds | Strict empty-distance increase |
| ---: | ---: | --- | --- | --- | ---: |
| 14 | 3 | 5,000 / 4,951 | 42 / 91 | 2,757.67 / 3,160.13 | 5.12% |
| 14 | 4 | 5,028 / 4,983 | 14 / 59 | 1,855.57 / 2,724.15 | 12.34% |
| 15 | 3 | 5,022 / 4,958 | 377 / 441 | 4,414.17 / 4,656.00 | 1.83% |
| 15 | 4 | 4,973 / 4,920 | 426 / 479 | 4,595.25 / 4,824.00 | 1.52% |

Each strict arm also has larger late backlog growth and journey P95.
The tests do not establish that every finishing hold is useful.
They show that removing these holds does not improve the selected capacity workload.

## Individual cohorts

At AM14, the strict arms complete no request that the current arms leave unfinished.
They lose 49 and 45 completions.
At AM15, they gain five and ten completions but lose 69 and 63.
Unfinished requests remain separate from completed-request statistics.

The mean completion delay on the jointly completed cohort increases by 186, 369, 195, and 137 seconds.
Maximum individual increases are 2,848, 2,772, 3,518, and 2,741 seconds.
These changes include later fleet and assignment effects.
They are not direct measurements of the initial hold alone.

## Checks and evidence

All primary arms retain once-per-second safety, speed, and unique-order checks.
Physical restores at three and six hours retain poses, bindings, admission ages, and saved platoon records.
The restored copies retain their selected wait rule and pass 60 seconds of dense continuation checks.
Two short current/strict pilots check every tick and match production aggregates.

[Metadata](measurements/finishing-wait-capacity.json) retains paired results, completed-cohort changes, gained and lost request IDs, checks, and source identities.
Raw timing and pending records remain in `~/.cache/agents/podsim/finishing-wait-capacity-20261001/`.
Its owned unit terminated successfully and its scratch binary was removed.
These selected cells do not qualify other demand bands or support a default change.
