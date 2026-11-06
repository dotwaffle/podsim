# Default flips on LondonFull

This study asks whether default-off features can be flipped to default on: redistribution, pickup reassignment, and sharing.
It also applies the old platoon recovery and no-harm rules to virtual platoons at this tip.
It changes no default, preset, contract, or simulation code.

## Result

The recommendation is against each flip and against the combination.
The screen has 3 seeds and 3 hours on one band and rate, and one flood seed.
The [adoption gates](../design/experimental-adoption.md#evidence-required) need seeds 1 to 10, all bands, and six-hour runs, so a flip cannot be approved from this study.

At the preset demand of AM peak 10/min, no arm changes service by more than 3% in any mean.
All arms complete the same requests, or one more.
Each arm has from 37 to 241 matched requests over an individual limit in each seed, and about as many that improve by the same limit.

On the King's Cross flood, the arms split in this way:

- Redistribution makes no move, and its result equals the current default in each field except the policy name.
- Reassignment completes 378 against 383 orders, and it enters at 7.12 against 7.20 entries a minute.
- Sharing completes 397 orders and accepts 59 more, with mean wait 7.67% lower, and it enters at 7.28 entries a minute.
- Sharing with reassignment completes 404 orders, with mean wait 7.69% lower, and it enters at 7.20 entries a minute.
- Platoons off completes 240 orders at 4.35 entries a minute.

Only sharing has a benefit reading above the 5% limit, and only on the flood.
It still fails the individual limits, the finite recovery gate at the 60-minute cap, and the live queue gate by offer identity.
Virtual platoons are already the preset, and turning them off has no support in these runs.

## What each default is

The three sources of settings differ.

| Setting | `scenario -preset london-full` | `cmd/compare` with no flag | The flood command of the [station limits study](station-limits.md) |
| --- | --- | --- | --- |
| Redistribution | Off: `redistribution: false` | Both arms: `-redistribution-policies off,on` | On |
| Pickup reassignment | Off: the member is absent | Off | On |
| Sharing | Off: the party limit is absent, and that gives 1 | Off: `-sharing-limits 1` | Off |
| Sharing consent | Private: `generatedOrderConsent` is absent | Private | Private |
| Virtual platoons | On: `platoonLimit: 4` | Off: `-platoon-policies off` | On |
| Queue limit | No member | 200 | 200 |
| Demand | AM peak, 10/min, generated demand not enabled | Set by flags | One destination, 20/min |

Compare [ignores the experimental settings](../reference.md#compare-command) of the input project, and it ignores `platoonLimit`, so each arm below gives each flag.
`-platoon-policies virtual` keeps the initial simulation limit, which is `MaxPlatoonLimit`, 4.
Compare `on` is guarded positioning, the same mode as `redistribution: true` in the server.
Compare gives the gate no configured rate, as the [reference](../reference.md#compare-command) says, and it turns the policy off for the rest of an arm at the first skipped arrival.

"Current defaults" here means the preset settings, so arm 1 has virtual platoons, and arm 5 is off against virtual.
Thus the flood command is not the current default: it adds redistribution and reassignment.
A sharing flip would also need the consent member, because private orders do not share.

## Method

The source is `545b95b6` plus scratch commits on the local branch `study/default-flips`, which does not land.
No test, lint, or race task ran on them.

| Commit | Change |
| --- | --- |
| `ccb44e46` | `c45f5c93` of `study/sharing-onboard`: `-request-ledger` writes one row per offer. |
| `9b4d258e` | A counter of pod entries and exits at the focus station, with the definitions of the station limits probe. |
| `c7159235` | The run and table scripts, in `tools/study-default-flips`. |

A flood of 10 simulated minutes with the two code commits gives the same report bytes as the binary at `545b95b6`.
The study did not repeat this check on the AM workload or on the full flood.

The project comes from `scenario -preset london-full`: 327 pods, 734 passenger berths, and 36 parking berths.
All arms use free-flow routing and `-workers 1`, with one process for each arm and seed.

| Arm | Flags |
| --- | --- |
| 1 Current | `-redistribution-policies off -platoon-policies virtual -pickup-reassignment off` |
| 2 Redistribution | As arm 1, with `-redistribution-policies on` |
| 3 Reassignment | As arm 1, with `-pickup-reassignment on` |
| 4 Sharing | As arm 1, with `-sharing-consent shared -sharing-limits 4 -sharing-modes drop-offs -onboard-pickups off` |
| 5 Platoons off | As arm 1, with `-platoon-policies off` |
| 6 Sharing and reassignment | As arm 4, with `-pickup-reassignment on` |

```sh
compare -project base.json -pattern profile -bands am-peak -loads 6s -duration 4h -arrivals-for 3h -seeds 1 <arm flags> -workers 1 -format json -request-ledger ledger -output arm.json
compare -project base.json -pattern destination -focus 940GZZLUKSX -loads 3s -duration 60m -arrivals-for 60m -seeds 1 <arm flags> -workers 1 -format json -request-ledger ledger -output arm.json
```

Each schedule ID is the same in all arms of a workload and seed: `7ae6bc54997d2b00`, `2856765bf877fce4`, and `7885c400afebb028` for AM, and `b5aaf67d23406d17` for the flood.
Both workloads use the default queue limit of 200.
The AM arms peak at 62 to 71 pending orders and skip no offer.

A match uses the offer index.
Wait is the boarding tick less the offer tick, and journey is the completion tick less the offer tick.
Waits compare only requests that board in both, and journeys compare only requests that complete in both.
The limits are those of the [gates](../design/experimental-adoption.md#numerical-limits): added wait over the larger of 30 s and 5%, and added journey over the larger of 60 s and 5%.
Late growth covers the second half of the arrival window.
Flood rates cover minutes 20 to 60, as in the station limits study.

### Choice of arm 6

The choice was fixed after arms 1 to 5 and before arm 6 ran: sharing and reassignment, with virtual platoons, and redistribution off.

- Virtual platoons stay, because platoons off loses 143 flood completions.
- Sharing joins, because it is the only flip with a benefit reading: flood mean wait falls by 7.67%.
- Reassignment joins, because it stays within the aggregate limit in each pair and sharing alone repeats arm 4.
- Redistribution stays out: AM seed 2 mean wait rises by 2.93%, and the flood has no move.

### Arm 4 against the sharing-onboard record

The [sharing-onboard study](sharing-onboard.md) has no matching result, so arm 4 ran at this rate.
Its AM 10/min rows differ in the arrival window, and this difference alone stops the reuse.

| Setting | Sharing-onboard, AM 10/min | Arm 4 |
| --- | --- | --- |
| Arrivals and cap | 6 h and 7 h | 3 h and 4 h |
| Queue limit | 1,000,000 | 200, and no arm reaches it |
| Consent of the no-sharing baseline | Shared | Private |
| Source | `4bffb6f7` | `545b95b6`: the only change to non-test Go code is a test entry that no production code calls |

Its ledgers are not in the repository, so its first 3 hours cannot be read.
[Earlier records](#earlier-records) compare the two results.

## AM peak, 10/min, 3 h

Each cell gives seeds 1 / 2 / 3.

| Arm | Offered | Skipped | Accepted | Boarded | Completed | Unfinished at cap |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 Current | 1,799 / 1,799 / 1,799 | 0 / 0 / 0 | 1,799 / 1,799 / 1,799 | 1,799 / 1,799 / 1,799 | 1,798 / 1,798 / 1,798 | 1 / 1 / 1 |
| 2 Redistribution | 1,799 / 1,799 / 1,799 | 0 / 0 / 0 | 1,799 / 1,799 / 1,799 | 1,799 / 1,799 / 1,799 | 1,798 / 1,798 / 1,798 | 1 / 1 / 1 |
| 3 Reassignment | 1,799 / 1,799 / 1,799 | 0 / 0 / 0 | 1,799 / 1,799 / 1,799 | 1,799 / 1,799 / 1,799 | 1,799 / 1,798 / 1,798 | 0 / 1 / 1 |
| 4 Sharing | 1,799 / 1,799 / 1,799 | 0 / 0 / 0 | 1,799 / 1,799 / 1,799 | 1,799 / 1,799 / 1,799 | 1,799 / 1,798 / 1,798 | 0 / 1 / 1 |
| 5 Platoons off | 1,799 / 1,799 / 1,799 | 0 / 0 / 0 | 1,799 / 1,799 / 1,799 | 1,799 / 1,799 / 1,799 | 1,798 / 1,798 / 1,798 | 1 / 1 / 1 |
| 6 Sharing and reassignment | 1,799 / 1,799 / 1,799 | 0 / 0 / 0 | 1,799 / 1,799 / 1,799 | 1,799 / 1,799 / 1,799 | 1,798 / 1,798 / 1,798 | 1 / 1 / 1 |

| Arm | Wait mean (s) | Wait p95 (s) | Journey mean (s) | Journey p95 (s) | Late growth (orders/min) | Backlog at arrival end |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 Current | 263 / 245 / 251 | 980 / 877 / 938 | 984 / 966 / 983 | 2,346 / 2,244 / 2,241 | +0.16 / -0.14 / +0.12 | 179 / 152 / 185 |
| 2 Redistribution | 266 / 252 / 251 | 974 / 919 / 940 | 987 / 972 / 983 | 2,301 / 2,266 / 2,239 | +0.19 / -0.11 / +0.09 | 179 / 151 / 182 |
| 3 Reassignment | 261 / 244 / 253 | 975 / 877 / 932 | 985 / 964 / 985 | 2,323 / 2,237 / 2,245 | +0.13 / -0.22 / +0.10 | 177 / 145 / 182 |
| 4 Sharing | 263 / 244 / 249 | 994 / 877 / 935 | 987 / 965 / 981 | 2,340 / 2,242 / 2,263 | +0.19 / -0.19 / +0.08 | 181 / 148 / 181 |
| 5 Platoons off | 264 / 246 / 251 | 970 / 889 / 935 | 985 / 965 / 982 | 2,296 / 2,219 / 2,246 | +0.17 / -0.13 / +0.14 | 178 / 150 / 184 |
| 6 Sharing and reassignment | 261 / 241 / 251 | 963 / 855 / 928 | 983 / 962 / 984 | 2,268 / 2,208 / 2,240 | +0.12 / -0.20 / +0.08 | 176 / 146 / 179 |

| Arm | Passenger (km) | Empty (km) | Loaded % | Platoon time % | Positioning moves | Swaps | Transfers | Joined parties | Intermediate stops | Largest detour |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 Current | 17,866 / 17,817 / 18,103 | 9,851 / 9,471 / 9,207 | 64.5 / 65.3 / 66.3 | 0.14 / 0.05 / 0.06 | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 | 1.000 / 1.000 / 1.000 |
| 2 Redistribution | 17,866 / 17,800 / 18,100 | 9,739 / 9,334 / 9,112 | 64.7 / 65.6 / 66.5 | 0.12 / 0.04 / 0.13 | 24 / 12 / 21 | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 | 1.000 / 1.000 / 1.000 |
| 3 Reassignment | 17,870 / 17,803 / 18,103 | 9,744 / 9,378 / 9,303 | 64.7 / 65.5 / 66.1 | 0.05 / 0.11 / 0.09 | 0 / 0 / 0 | 3 / 4 / 3 | 24 / 28 / 31 | 0 / 0 / 0 | 0 / 0 / 0 | 1.000 / 1.000 / 1.000 |
| 4 Sharing | 17,820 / 17,791 / 18,070 | 9,817 / 9,369 / 9,165 | 64.5 / 65.5 / 66.3 | 0.05 / 0.12 / 0.04 | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 | 8 / 5 / 7 | 8 / 4 / 6 | 1.090 / 1.146 / 1.140 |
| 5 Platoons off | 17,864 / 17,786 / 18,098 | 9,890 / 9,452 / 9,258 | 64.4 / 65.3 / 66.2 | 0.00 / 0.00 / 0.00 | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 | 1.000 / 1.000 / 1.000 |
| 6 Sharing and reassignment | 17,815 / 17,773 / 18,072 | 9,890 / 9,231 / 9,180 | 64.3 / 65.8 / 66.3 | 0.09 / 0.12 / 0.13 | 0 / 0 / 0 | 2 / 2 / 5 | 21 / 28 / 30 | 6 / 7 / 7 | 6 / 5 / 7 | 1.061 / 1.146 / 1.151 |

The report field `drain_seconds` gives only the cap in these runs, because they do not use `-stop-when-drained`.
The recovery reading is the last completion in the ledger, less the end of the 3 h arrival window.

| Arm | Last completion after arrivals (s) | Queue clear (s) | Peak pending | Drained |
| --- | ---: | ---: | ---: | ---: |
| 1 Current | 3,202 / 3,158 / 3,060 | 1,248 / 1,229 / 2,308 | 65 / 70 / 64 | no / no / no |
| 2 Redistribution | 3,201 / 3,158 / 3,042 | 1,245 / 2,067 / 2,446 | 68 / 71 / 63 | no / no / no |
| 3 Reassignment | 3,363 / 3,239 / 2,971 | 1,188 / 1,856 / 2,349 | 71 / 66 / 62 | yes / no / no |
| 4 Sharing | 3,363 / 3,158 / 3,043 | 1,137 / 1,229 / 1,802 | 68 / 69 / 64 | yes / no / no |
| 5 Platoons off | 3,204 / 3,158 / 3,052 | 1,246 / 2,746 / 2,439 | 68 / 67 / 62 | no / no / no |
| 6 Sharing and reassignment | 3,243 / 3,075 / 3,144 | 1,248 / 1,019 / 1,566 | 68 / 65 / 63 | no / no / no |

The unfinished request is aboard at the cap in each case.
It is one offer in each seed, from `940GZZLUAMS`: offer 1443 to `940GZZLUBMY`, offer 1797 to `940GZZLUWSM`, and offer 1725 to `940GZZLUMGT`.
Arms 3 and 4 complete it in seed 1.
The study did not trace why it finishes late.

### Matched requests

"Over" counts requests past the individual limit, and "better" counts requests that improve by the same limit.
The arm columns are changes in the aggregates.
The last column counts offers whose boarding and completion ticks do not change.

| Arm against baseline | Seed | Arm wait | Arm journey | Arm journey p95 | Boarded in both | Wait mean | Completed in both | Journey mean | Wait over / better | Journey over / better | Largest added wait (s) | Largest added journey (s) | Completed only baseline / only arm | Newly skipped / newly accepted | Late growth change | Empty change | Offers unchanged |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 2 Redistribution | 1 | +1.07% | +0.26% | -1.93% | 1799 | +1.07% | 1798 | +0.26% | 199 / 176 | 113 / 108 | 1,111 | 1,129 | 0 / 0 | 0 / 0 | +0.03 | -1.13% | 587 |
| 2 Redistribution | 2 | +2.93% | +0.66% | +0.97% | 1799 | +2.93% | 1798 | +0.66% | 241 / 201 | 153 / 133 | 1,645 | 1,648 | 0 / 0 | 0 / 0 | +0.03 | -1.46% | 542 |
| 2 Redistribution | 3 | +0.24% | +0.05% | -0.10% | 1799 | +0.24% | 1798 | +0.05% | 168 / 148 | 88 / 86 | 552 | 552 | 0 / 0 | 0 / 0 | -0.03 | -1.03% | 640 |
| 3 Reassignment | 1 | -0.71% | +0.08% | -0.97% | 1799 | -0.71% | 1798 | -0.17% | 137 / 162 | 89 / 105 | 995 | 1,019 | 0 / 1 | 0 / 0 | -0.02 | -1.08% | 958 |
| 3 Reassignment | 2 | -0.47% | -0.19% | -0.35% | 1799 | -0.47% | 1798 | -0.19% | 129 / 175 | 86 / 104 | 1,175 | 632 | 0 / 0 | 0 / 0 | -0.08 | -0.99% | 839 |
| 3 Reassignment | 3 | +0.81% | +0.21% | +0.15% | 1799 | +0.81% | 1798 | +0.21% | 130 / 114 | 81 / 74 | 821 | 829 | 0 / 0 | 0 / 0 | -0.02 | +1.04% | 914 |
| 4 Sharing | 1 | -0.13% | +0.28% | -0.26% | 1799 | -0.13% | 1798 | +0.02% | 136 / 141 | 89 / 83 | 1,123 | 1,125 | 0 / 1 | 0 / 0 | +0.03 | -0.34% | 864 |
| 4 Sharing | 2 | -0.49% | -0.13% | -0.12% | 1799 | -0.49% | 1798 | -0.13% | 67 / 91 | 37 / 56 | 760 | 763 | 0 / 0 | 0 / 0 | -0.04 | -1.08% | 1150 |
| 4 Sharing | 3 | -0.75% | -0.15% | +0.97% | 1799 | -0.75% | 1798 | -0.15% | 98 / 99 | 67 / 60 | 688 | 690 | 0 / 0 | 0 / 0 | -0.04 | -0.46% | 1014 |
| 6 Sharing and reassignment | 1 | -0.59% | -0.14% | -3.34% | 1799 | -0.59% | 1798 | -0.14% | 175 / 192 | 104 / 116 | 1,010 | 1,041 | 0 / 0 | 0 / 0 | -0.03 | +0.40% | 789 |
| 6 Sharing and reassignment | 2 | -1.65% | -0.41% | -1.62% | 1799 | -1.65% | 1798 | -0.41% | 131 / 178 | 81 / 112 | 740 | 742 | 0 / 0 | 0 / 0 | -0.06 | -2.54% | 847 |
| 6 Sharing and reassignment | 3 | +0.15% | +0.11% | -0.07% | 1799 | +0.15% | 1798 | +0.11% | 153 / 146 | 113 / 100 | 1,285 | 1,273 | 0 / 0 | 0 / 0 | -0.04 | -0.30% | 835 |
| Virtual against off | 1 | -0.23% | -0.04% | +2.18% | 1799 | -0.23% | 1798 | -0.04% | 130 / 131 | 78 / 73 | 971 | 959 | 0 / 0 | 0 / 0 | -0.01 | -0.40% | 817 |
| Virtual against off | 2 | -0.31% | +0.05% | +1.16% | 1799 | -0.31% | 1798 | +0.05% | 128 / 101 | 78 / 63 | 652 | 657 | 0 / 0 | 0 / 0 | -0.01 | +0.21% | 830 |
| Virtual against off | 3 | +0.05% | +0.03% | -0.21% | 1799 | +0.05% | 1798 | +0.03% | 121 / 109 | 69 / 59 | 919 | 916 | 0 / 0 | 0 / 0 | -0.02 | -0.55% | 792 |

Small changes move many requests.
Arm 2 makes 12 to 24 moves, and arm 5 changes a platoon time of 0.05 to 0.14%, and in each pair more than half of the offers change their ticks.
The study has no null arm, so it does not give the count that any small change produces, and it does not trace this movement to a mechanism.

## King's Cross flood, seed 1

| Arm | Offered | Skipped | Accepted | Boarded | Completed | Unfinished at cap |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 Current | 1,199 | 426 | 773 | 574 | 383 | 390 |
| 2 Redistribution | 1,199 | 426 | 773 | 574 | 383 | 390 |
| 3 Reassignment | 1,199 | 430 | 769 | 569 | 378 | 391 |
| 4 Sharing | 1,199 | 367 | 832 | 632 | 397 | 435 |
| 5 Platoons off | 1,199 | 484 | 715 | 516 | 240 | 475 |
| 6 Sharing and reassignment | 1,199 | 367 | 832 | 632 | 404 | 428 |

| Arm | Wait mean (s) | Wait p95 (s) | Journey mean (s) | Journey p95 (s) | Late growth (orders/min) | Backlog at arrival end |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 Current | 638 | 1,852 | 1,322 | 2,476 | -0.67 | 390 |
| 2 Redistribution | 638 | 1,852 | 1,322 | 2,476 | -0.67 | 390 |
| 3 Reassignment | 641 | 1,815 | 1,329 | 2,515 | -0.57 | 391 |
| 4 Sharing | 589 | 1,788 | 1,299 | 2,472 | +0.60 | 435 |
| 5 Platoons off | 691 | 1,995 | 1,440 | 2,878 | +0.50 | 475 |
| 6 Sharing and reassignment | 589 | 1,790 | 1,326 | 2,491 | +0.37 | 428 |

| Arm | Passenger (km) | Empty (km) | Loaded % | Platoon time % | Positioning moves | Swaps | Transfers | Joined parties | Intermediate stops | Largest detour |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 Current | 5,760 | 4,226 | 57.7 | 29.78 | 0 | 0 | 0 | 0 | 0 | 1.000 |
| 2 Redistribution | 5,760 | 4,226 | 57.7 | 29.78 | 0 | 0 | 0 | 0 | 0 | 1.000 |
| 3 Reassignment | 5,760 | 4,150 | 58.1 | 30.42 | 0 | 13 | 1 | 0 | 0 | 1.000 |
| 4 Sharing | 5,792 | 4,210 | 57.9 | 28.96 | 0 | 0 | 0 | 57 | 0 | 1.000 |
| 5 Platoons off | 5,010 | 2,944 | 63.0 | 0.00 | 0 | 0 | 0 | 0 | 0 | 1.000 |
| 6 Sharing and reassignment | 5,778 | 4,107 | 58.5 | 30.21 | 0 | 13 | 1 | 57 | 0 | 1.000 |

| Arm | Entries a minute | Exits a minute | Entry headway p50 (s) | Linked entries % |
| --- | ---: | ---: | ---: | ---: |
| 1 Current | 7.20 | 6.70 | 2.7 | 90 |
| 2 Redistribution | 7.20 | 6.70 | 2.7 | 90 |
| 3 Reassignment | 7.12 | 6.70 | 2.7 | 91 |
| 4 Sharing | 7.28 | 6.78 | 2.7 | 92 |
| 5 Platoons off | 4.35 | 4.38 | 16.5 | 0 |
| 6 Sharing and reassignment | 7.20 | 6.62 | 2.6 | 90 |

Arms 2, 3, 4, and 6 are within 0.08 entries a minute of arm 1.
The station limits record gives 7.10 to 7.47 over 3 seeds for one setting, so this one seed does not separate them.
The grid used all 24 runs, so no flood seed was added.
No conclusion below depends on such a difference.

The flood has no time after its arrivals, so it gives no recovery reading.
The queue limit bounds its backlog, so its late growth says little, as the station limits study also found.
Sharing has 57 joined parties and no intermediate stop, because all orders have one destination.

### Matched requests

| Arm against baseline | Seed | Arm wait | Arm journey | Arm journey p95 | Boarded in both | Wait mean | Completed in both | Journey mean | Wait over / better | Journey over / better | Largest added wait (s) | Largest added journey (s) | Completed only baseline / only arm | Newly skipped / newly accepted | Late growth change | Empty change | Offers unchanged |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 2 Redistribution | 1 | +0.00% | +0.00% | +0.00% | 574 | +0.00% | 383 | +0.00% | 0 / 0 | 0 / 0 | 0 | 0 | 0 / 0 | 0 / 0 | +0.00 | +0.00% | 1199 |
| 3 Reassignment | 1 | +0.52% | +0.52% | +1.56% | 554 | -0.89% | 373 | +1.95% | 31 / 42 | 62 / 10 | 615 | 479 | 10 / 5 | 98 / 94 | +0.10 | -1.80% | 889 |
| 4 Sharing | 1 | -7.67% | -1.74% | -0.19% | 558 | -8.46% | 368 | -0.42% | 31 / 81 | 31 / 39 | 1,177 | 446 | 15 / 29 | 108 / 167 | +1.27 | -0.38% | 747 |
| 6 Sharing and reassignment | 1 | -7.69% | +0.30% | +0.58% | 559 | -10.77% | 374 | +0.38% | 36 / 83 | 47 / 37 | 793 | 827 | 9 / 30 | 110 / 169 | +1.03 | -2.82% | 742 |
| Virtual against off | 1 | -7.70% | -8.19% | -13.94% | 513 | -20.04% | 240 | -36.91% | 17 / 122 | 6 / 174 | 962 | 129 | 0 / 143 | 95 / 153 | -1.17 | +43.53% | 660 |

## Adoption gates

Each cell is passed, failed, or not read, for the arm against its baseline in this screen.
"Passed" means only that the limit holds in the pairs of this screen.
The virtual column reads virtual against off, with off as the baseline.

| Gate | Redistribution | Reassignment | Sharing | Sharing and reassignment | Virtual against off |
| --- | --- | --- | --- | --- | --- |
| Safety and accounting | Not read | Not read | Not read | Not read | Not read |
| Restore fidelity | Not read | Not read | Not read | Not read | Not read |
| Finite recovery | Passed | Failed: flood, 10 requests | Failed: flood, 15 requests | Failed: flood, 9 requests | Passed |
| Live queue loss | Passed | Failed: flood, 98 newly skipped, net +4 | Failed: flood, 108 newly skipped, net -59 | Failed: flood, 110 newly skipped, net -59 | Failed: flood, 95 newly skipped, net -58 |
| Aggregate service | Failed: AM seed 2 wait +2.93% | Passed: at most +1.95%, matched flood journey | Passed: at most +0.97% | Passed: at most +0.58% | Failed: AM seed 1 journey p95 +2.18% |
| Individual pickup wait | Failed: 168 to 241 | Failed: 31 to 137 | Failed: 31 to 136 | Failed: 36 to 175 | Failed: 17 to 130 |
| Individual journey | Failed: 88 to 153 | Failed: 62 to 89 | Failed: 31 to 89 | Failed: 47 to 113 | Failed: 6 to 78 |
| Sustained backlog | Not read | Not read | Not read | Not read | Not read |
| Capacity | Not read | Not read | Not read | Not read | Not read |
| Shared detour | Does not apply | Does not apply | Passed: at most 1.146 | Passed: at most 1.151 | Does not apply |
| Disabled-path cost | Not read | Not read | Not read | Not read | Not read |
| Active-path cost | Not read | Not read | Not read | Not read | Not read |
| Benefit | Failed | Failed | Passed: flood wait -7.67% | Passed: flood wait -7.69% | Passed: flood wait -7.70%, journey -8.19% |

The flood cap is 60 minutes, while arrivals continue, so the finite recovery failures are requests that complete by that cap in the baseline only.
On AM, no candidate loses a request at the 4 h cap.
Skips happen only on the flood, so the live queue reading counts offers by identity, and the net count falls with sharing.
This record does not list the identities, so that part of the gate is not met, and the ledgers were not kept.
The commands and schedule IDs above give them again.

The sustained backlog gate needs six-hour runs, so it has no reading here.
The nearest readings are AM late growth from -0.22 to +0.19 orders/min over 90 minutes.
Arm 1 is itself above +0.1 in seeds 1 and 3, at +0.16 and +0.12.
The largest increase over arm 1 on AM is +0.03.

These runs do not measure the cost gates.
The processes shared the host with another study and the load changed, so the times below are not a CPU comparison.
Arm 3 on AM used 86 to 107 s of user time, against 60 to 74 s for arm 1, and 585 to 601 MiB of peak memory, against 445 to 456 MiB.

## Platoon rules

The [platoon follow-up](platoon-followup.md) used these rules on other projects, so this section is the first reading on LondonFull at this tip, not a repeat.
Virtual platoon time is 0.05 to 0.14% on AM, against 0.78 to 18.07% in the London198 rows, so AM is a weak test of these rules.
The flood has 29.78%.

| Rule | AM, seeds 1 / 2 / 3 | Flood, seed 1 |
| --- | --- | --- |
| Serves at least as many as off | Passed: 1,798 each | Passed: 383 against 240 |
| Mean wait rises by at most 2 s | Passed: -0.62 / -0.76 / +0.13 s | Passed: -53.2 s |
| Empty distance rises by at most 2% | Passed: ratio 0.996 / 1.002 / 0.995 | Failed: ratio 1.435, with 143 more completions |
| No request finishes by the cap with off and not with virtual | Passed: both arms leave the same request unfinished | Not read: no time after arrivals |

The last completions after arrivals are 3,202 / 3,158 / 3,060 s with virtual and 3,204 / 3,158 / 3,052 s with off.
The old recovery rule uses 60 minutes from the start of a 30-minute arrival window, and this reading uses 60 minutes after a 3 h window, so the two are not equal.
Per completed order, the flood empty distance is 11.03 km with virtual and 12.27 km with off.

## Earlier records

| Record | Then | Now | Differences | Result |
| --- | --- | --- | --- | --- |
| [Station limits](station-limits.md) flood, seed 1 | 378 completed, 430 skipped, 7.12 entries, 6.70 exits, headway 2.7 s, 91% linked, wait 641 s, journey 1,329 s | Arm 3 gives each of these values | `1932147` against the tip. Redistribution on against off. A new counter against the old probe. | Reproduces. Arm 2 makes no move on this flood, and the flood command itself did not run. |
| [Routing flood](routing-flood.md) baseline, seed 1 | 5,760 km passenger, 4,150 km empty, 13 swaps | Arm 3 gives the same | `aaedb7eb` against the tip. Redistribution on against off. | Reproduces. |
| Station limits, King's Cross, platoons off, seed 1 | 4.35 entries, 4.38 exits, headway 16.5 s, 240 completed, 485 skipped | Arm 5: the same, with 484 skipped | Reassignment and redistribution on against off. The record's table also holds 120-minute rows, and this row agrees with a 60-minute arm. | Reproduces, with one more accepted offer. |
| [Guarded positioning](qualification.md#guarded-positioning-in-the-london-sweep), rows above the rate gate | Equal to off except the policy | Flood arm 2 equals arm 1 except the policy | 327 pods against 114, so a gate of 16.35/min against 5.7/min. The flood also skips offers, and the study did not separate the rate gate from the stop at the first skip. | Reproduces. |
| Guarded positioning, below the rate gate | Mean wait lower than off at 1 to 5/min, empty distance lower in seven of eight bands | AM 10/min wait +0.24% to +2.93%, empty distance -1.03% to -1.46% | Project, rate, and 3 h against 30 minutes of arrivals. | The empty distance direction reproduces. The wait direction does not carry over. |
| [Pickup reassignment](pickup-reassignment.md), AM 10/min, 30 minutes | No swaps, 298 of 299 completed in both | 3 / 4 / 3 swaps in 3 h | 3 h against 30 minutes, 327 pods, and the current controller. | Not comparable. |
| [Pickup safeguards](../design/experimental-adoption.md#rejected-candidates), AM 10/min | No 5% gain, and 382 to 597 pickup exceedances | No 5% gain, and 129 to 137 | 287 against 327 pods, 6 h against 3 h, seeds 1 and 4 against 1 to 3, and the old source. The safeguards themselves did not run. | The direction reproduces. The count is lower with the shorter window. |
| [Sustained swaps](pickup-swap-sustained.md), 15 and 20/min | Mean wait 9.00% to 29.42% lower | Not run | The rate. | Not tested. This study does not reject that gain. |
| [Sharing-onboard](sharing-onboard.md#no-sharing-against-onboard-off), AM 10/min, no sharing against onboard off | Wait +0.07 / -0.15 / -0.92%, 401 / 315 / 344 over the limit, 13 / 17 / 16 joined, detour up to 1.331 | Wait -0.13 / -0.49 / -0.75%, 136 / 67 / 98 over the limit, 8 / 5 / 7 joined, detour up to 1.146 | See [arm 4](#arm-4-against-the-sharing-onboard-record). | The direction reproduces. Counts are lower with the shorter window. |
| Sharing-onboard, AM 20/min and uniform 10/min | Wait -43.0% to -45.1%, and +10.88% in uniform seed 2 | Not run | Rate and pattern. | Not tested. The uniform result stays a failure of the aggregate limit for a sharing flip. |
| [Platoon follow-up](platoon-followup.md) failures | London198 recovery, and three London114 no-harm pairs | Not run | Other projects. | Not tested. See [platoon rules](#platoon-rules). |

## Recommendation

| Flip | Recommendation | Reading |
| --- | --- | --- |
| Redistribution | Against | Benefit and aggregate service failed, individual limits failed, and the rest not read. It has no effect on the flood. |
| Pickup reassignment | Against | Benefit failed, individual limits failed, and finite recovery and live queue failed on the flood. The gain of the sustained record is at rates that this study did not run. |
| Sharing | Against, on this evidence | Benefit passed on the flood only. Individual limits, finite recovery, and live queue failed, and the sharing-onboard record fails aggregate service at uniform 10/min. Its gain shows above supply. |
| Virtual platoons | No change: the preset already has them | Against off: the individual limits, the live queue gate, and AM journey p95 failed, and the benefit passed on the flood. The old no-harm rules pass on AM. |
| Sharing and reassignment | Against | Benefit passed on the flood only, and the same gates fail as for sharing. It adds 7 completions on the flood, and no AM reading shows a 5% gain. |

These are screen results.
Passing the gates would still [not authorize a default change](../design/experimental-adoption.md#decision-and-exceptions), and no flip can be approved from this study.

## What the full gates need

The gates need 6 LondonFull bands, seeds 1 to 10, the qualified load range, six-hour runs, both queue limits, mixed Acton arrivals, and rail-hub bursts.
With 6 arms, that is 720 runs for each rate.
The AM arms here took 54 to 111 s, with a mean of 76 s for a 4 h run on a shared host.
Scaled by simulated time to a 7 h run, that is about 133 s, so about 27 process-hours for each rate at or below supply.
With 2 processes, that is about 13 hours for each rate.
This assumes that other bands cost the same, and the study did not measure them.

Rates above supply cost much more.
A [sharing-onboard](sharing-onboard.md#adoption-gates) arm with onboard off took 530 s for 3 h of arrivals at AM 20/min on a loaded host.
Scaled by simulated time to a 7 h run, that is about 1,240 s, so about 250 process-hours for each such rate, or 125 hours with 2 processes.
The study did not measure a 7 h run at such a rate.
The safety and restore checks, and a controlled CPU comparison on a quiet host, add time that this study did not measure.

## Proposed arms

These did not run, because the study had a limit of 24 runs.

- The flood command itself, redistribution on with reassignment on, seed 1, to confirm the reading of the first row of [earlier records](#earlier-records).
- Flood seeds 2 and 3 for arms 1, 3, 4, and 6, to read the differences of under 1 entry a minute.
- AM 15 and 20/min for arms 1, 3, and 6, because sharing and reassignment show their gains above supply in the earlier records.
- A null arm with one small change, such as one offer moved by one tick, to measure how many requests any change moves past the individual limits.
- The Waterloo flood, and a flood with time after its arrivals for a recovery reading.

## Not verified

- No arm ran the safety oracle, a restore, or a CPU comparison.
- The screen has one band, one rate, one queue limit, and no uniform, Acton, or rail-hub arm.
- The flood has one seed.
- Arm 1 uses private consent, and the sharing-onboard baseline uses shared consent, and no arm tests whether consent changes a run with a party limit of 1.
- The report byte check covers a 10-minute flood only.
- The cause of each change between arms, and of the late unfinished request.
- Compare gives the positioning gate no configured rate, so the server may differ.
