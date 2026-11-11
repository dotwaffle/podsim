# Wait rules in the Express screen

This study asks how much of the failure of `D` against `LF` in the [Express London adoption screen](../contracts/express-london-adoption-screen-proposal.md) comes from the [wait rule](../model.md#wait-rules), and how the results of `LF` move under the other rules.
The [study of the dispatch choice in `D`](../contracts/express-london-adoption-screen-proposal.md#study-of-the-dispatch-choice-in-d) ran `D` with the rules `current`, `strict`, and `none`, but it ran `LF` with `current` only.

This is a diagnosis under the decision of November 9, 2026, which stopped the work on D adoption.
It changes no gate, policy, default, or simulation code, and it does not reopen the screen result.

## Result

No rule makes `D` pass the background wait or journey gate in any seed.

- Against `LF` with `current`, which is the gate pairing of the screen, `D` with `strict` or `none` has 22 to 31 requests over the wait limit, against 27 to 33 with `current`.
  The count is lower by 1 to 9 in each seed with either rule, and by only 1 in seed 3.
  Almost all of the change is at the stations other than Bank and Waterloo.
  The study does not compare which requests fail, and a change of rule alone moves 239 to 325 requests of `D` over the wait limit against `D` with `current`.
- When both arms use the same rule, `D` has 13 to 25 requests over the wait limit.
  This lower count comes partly from the change to `LF`, so it does not measure the effect of the rule on `D`.
- With `strict` or `none`, the matched requests of `LF` wait longer than with `current`, by a mean shift of 10 to 40 s and a p95 shift of 520 to 589 s.
  Other measures of `LF` move both ways: it refuses 201 to 285 events against 217 to 287, and it finishes 23 fewer to 139 more hub passengers.
  Against `LF` with `current`, 421 to 477 requests are over the wait limit.
  This count is about the size of the effect of one move of one pod in the [sensitivity study](../contracts/express-london-adoption-screen-proposal.md#sensitivity-of-the-individual-limits-to-one-pod-move), so the count shows the sensitivity of the limits, not only the harm of the rule.

## Method

The runs used `3f09202c` with a scratch patch to `cmd/compare`, which did not land.
The patch adds a `-wait-rule` option to `compare screen`, which calls `SetFinishingPodWait` in `screenConfigure` after the other policies, as `cmd/compare/run.go` does.
The patch also writes the outcome of each event, and the offered events, to the JSON report.
No file under `internal/sim` changed.

The 6 arms are `LF` and `D`, each with the rules `current`, `strict`, and `none`.
Each arm ran the busy band `pm-peak` with seeds 1, 2, and 3, with the screen defaults: 2 hours of arrivals, at most 2 hours of clear, a queue limit of 200, and the oracle at each simulated second.
The runs used 3 workers, nice 10, and a memory cap of 24 GiB.

A scratch script compared the arms in pairs with the rule of `screenCompare`.
For each pair of the same rule, its counts are the same as the gate output of `compare screen`.
A matched request is a background request that boarded in both arms.
The mean and p95 wait of an arm cover the background requests that boarded in that arm, and the refusals of `LF` remove requests from this set.
The p95 is the nearest-rank value.

### Control

The runs of `D` and `LF` with `current` are the control.
They do not give the counts of the first screen at `e41c6610`, because `1f721a5c` (the Express links of Waterloo bank `b`), the berth claim refusal (`ead9846c`), and the [bank load choice](../contracts/bank-load-choice-proposal.md) (`4b151fbd`) came later.

| Seed | Wait over, `e41c6610` | Wait over, `3f09202c` | Journey over, `e41c6610` | Journey over, `3f09202c` |
| ---: | ---: | ---: | --- | ---: |
| 1 | 31 | 27 | 22, and 1 not finished | 15 |
| 2 | 34 | 33 | 27 | 27 |
| 3 | 31 | 32 | 24 | 26 |

The control has the same counts as the table of [over-limit requests by origin](../contracts/express-london-adoption-screen-proposal.md#over-limit-requests-by-origin) at `785b3968` in each cell: matched requests, wait over, and journey over at Bank, Waterloo, and the other stations.
In seed 2, `D` has 21, 13, and 11 requests over the wait limit at the other stations with `current`, `strict`, and `none` against `LF` with `current`, as in the study of the dispatch choice.
`LF` refuses 217, 223, and 287 events, and its mean wait in seed 1 is 696 s, as in the earlier probes.

## Each arm

`D` refuses no event, and each arm finishes each accepted order in the clear.

| Arm | Mean wait (s), seeds 1 to 3 | p95 wait (s) | Refused events | Hub passengers finished |
| --- | --- | --- | --- | --- |
| `LF`, `current` | 696, 723, 734 | 1,327, 1,498, 1,401 | 217, 223, 287 | 2,638, 2,658, 2,531 |
| `LF`, `strict` | 740, 741, 746 | 1,535, 1,465, 1,388 | 201, 250, 255 | 2,658, 2,660, 2,670 |
| `LF`, `none` | 731, 764, 755 | 1,468, 1,517, 1,414 | 232, 257, 285 | 2,615, 2,669, 2,630 |
| `D`, `current` | 182, 172, 194 | 440, 426, 468 | 0, 0, 0 | 4,393, 4,788, 4,898 |
| `D`, `strict` | 181, 164, 194 | 434, 397, 468 | 0, 0, 0 | 4,393, 4,788, 4,898 |
| `D`, `none` | 179, 160, 192 | 434, 398, 458 | 0, 0, 0 | 4,393, 4,788, 4,898 |

## `D` against `LF`

In each pair below, `D` passes the safety, finite recovery, live queue loss, and hub gates, and no request is censored.
Each cell gives the requests over the wait limit and over the journey limit.
The last two columns split the requests over the wait limit by origin.
No request from Waterloo is over a limit in any pair.

| `D` rule | `LF` rule | Seed 1 | Seed 2 | Seed 3 | Wait over from Bank | Wait over from other stations |
| --- | --- | --- | --- | --- | --- | --- |
| `current` | `current` | 27, 15 | 33, 27 | 32, 26 | 5, 12, 4 | 22, 21, 28 |
| `strict` | `current` | 25, 17 | 24, 21 | 31, 22 | 7, 11, 5 | 18, 13, 26 |
| `none` | `current` | 22, 14 | 24, 19 | 31, 24 | 5, 13, 7 | 17, 11, 24 |
| `strict` | `strict` | 13, 7 | 22, 17 | 25, 19 | 6, 14, 7 | 7, 8, 18 |
| `none` | `none` | 16, 13 | 19, 11 | 18, 13 | 4, 14, 7 | 12, 5, 11 |

## Each rule against `current`

In this table, the arm with `strict` or `none` is the candidate, and the same configuration with `current` is the reference.
The shift is the wait of the candidate minus the wait of the reference, on the matched requests.
The queue loss counts the events that the candidate refuses and the reference accepts.
The rows of `D` give the same measure for `D`.

| Arm | Seed | Wait over | Journey over | Queue loss | Matched | Mean shift (s) | p95 shift (s) | Faster |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `LF`, `strict` | 1 | 451 | 398 | 106 | 1,068 | +36.7 | +589 | 467 |
| `LF`, `strict` | 2 | 458 | 416 | 138 | 1,056 | +19.9 | +520 | 432 |
| `LF`, `strict` | 3 | 421 | 370 | 125 | 1,040 | +9.5 | +535 | 497 |
| `LF`, `none` | 1 | 435 | 380 | 133 | 1,046 | +31.7 | +571 | 457 |
| `LF`, `none` | 2 | 477 | 438 | 138 | 1,053 | +40.4 | +555 | 432 |
| `LF`, `none` | 3 | 427 | 374 | 153 | 1,026 | +27.6 | +570 | 455 |
| `D`, `strict` | 1 | 239 | 184 | 0 | 1,199 | -1.0 | +158 | 449 |
| `D`, `strict` | 2 | 261 | 191 | 0 | 1,199 | -7.4 | +154 | 414 |
| `D`, `strict` | 3 | 325 | 275 | 0 | 1,199 | +0.2 | +197 | 427 |
| `D`, `none` | 1 | 247 | 189 | 0 | 1,199 | -2.7 | +154 | 421 |
| `D`, `none` | 2 | 255 | 171 | 0 | 1,199 | -11.2 | +144 | 430 |
| `D`, `none` | 3 | 300 | 241 | 0 | 1,199 | -2.4 | +208 | 468 |

No request is censored in these pairs.

## Reading

The wait rule of `D` is a small part of its failure against `LF` with `current`.
With `strict` or `none`, `D` still has 22 to 31 requests over the wait limit, and the count is lower by at most 9 in a seed, mostly at the stations other than the hubs.
From Bank, 5 to 13 requests are over the wait limit with the other rules, against 4 to 12 with `current`, which agrees with the [study of the Compact pods at Bank](../contracts/express-london-adoption-screen-proposal.md#study-of-the-compact-pods-at-bank).
For `LF`, the other rules give the matched requests 10 to 40 s more mean wait in each seed.
They accept 104 to 157 events that `current` refuses, and refuse 106 to 153 events that `current` accepts, so the matched set changes too.
A change of rule moves 239 to 477 requests of either configuration over the wait limit against the same configuration with `current`.
So a lower count against `LF` with `current` is a net change, not a set of requests that the rule fixes.
The lower counts of the pairs with the same rule do not show a share of the failure that comes from the rule either.
They come from a changed reference, in a workload where each change of dispatch moves hundreds of requests.

## Limits

- The study has one band, 3 seeds, and one queue limit, as the study brief cap allows.
  It does not meet the [adoption gates](../design/experimental-adoption.md#evidence-required).
- The control is `3f09202c`, not the screen commit `e41c6610`.
- The study does not trace why a rule changes a given request.
- The changed `D` arms are not candidates for adoption, and the result of the screen stands.
