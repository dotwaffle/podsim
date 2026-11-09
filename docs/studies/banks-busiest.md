# Banks at the busiest stations

This study asks whether the busiest LondonFull stations need banks, and what the layouts cost.
On the preset AM demand, they do not need banks.
King's Cross, Waterloo, Paddington, and Bank take 0.24 to 0.54 entries a minute over minutes 20 to 180, and no 10-minute window passes 1.2 entries a minute.
The [station study](station-limits.md) places the first limit near 7 entries a minute, so these stations run at less than a tenth of it on the mean, and at about a sixth in the busiest window.
Banking all four gives no gain in service on this demand, and it adds 15 to 27 s to the mean journey of an order that touches them.

Under a flood, the result depends on the station.
Two banks raise entries from 5.50 to 8.47 a minute at Bank and from 6.53 to 8.32 at Waterloo, on seed 1.
At Paddington they lower entries in all three seeds, from 5.60 / 6.10 / 5.78 to 5.15 / 5.60 / 5.40.

This is a screen, with one to three seeds and one demand band.
It does not meet the [adoption gates](../design/experimental-adoption.md), which require seeds 1 through 10 and all supported demand bands.
It qualifies no setting or layout.
No default, preset, contract, simulation rule, or layout changes with this study.

## Method

The source is `545b95b6` plus the probe of the station study.
The probe, its additions, and the scripts are scratch code, and they do not land.
The additions are a list of stations, a window end, and the origin and destination of each offer.
The non-document difference between the station study's `1932147` and `545b95b6` is the incident soak tests and one test entry that no production code calls.
The hooks in the probe are the station study's, and no arm sets them.

The project comes from `scenario -preset london-full -output base.json`.
It has 327 pods and 734 passenger berths.
Each arm ran with the separation oracle at each tick, and each arm is clean, with a smallest gap of 6.000 m.

The flood is the command of the station study:

```sh
compare -project base.json -pattern destination -focus 940GZZLUPAC \
  -loads 3s -duration 60m -arrivals-for 60m -seeds 1 \
  -redistribution-policies on -platoon-policies virtual \
  -pickup-reassignment on -workers 1 -format json
```

Rates cover minutes 20 to 60.
The AM arm is the no-sharing arm of the [onboard pickup study](sharing-onboard.md), at the preset rate of 10 orders a minute:

```sh
compare -project base.json -pattern profile -bands am-peak -loads 6s \
  -seeds 1 -duration 4h -arrivals-for 3h -sharing-consent shared \
  -sharing-limits 1 -redistribution-policies off \
  -platoon-policies virtual -queue-limit 1000000 -workers 1 -format json
```

The AM rates cover minutes 20 to 180, so they exclude the drain hour.
The probe gives the request records, because `-request-ledger` is not in this tree.
Each arm has the schedule of its baseline: 1,199 offers in a flood and 1,799 on the AM profile, with the same schedule ID in each pair.

## Ranking

The table ranks the passenger stations by the AM peak weights of the preset profile, origins plus destinations.
The last column is the number of trip ends a minute at the preset rate.

| Rank | Station | NaPTAN ID | Origin, % | Destination, % | Sum, % | Berths | Trip ends a minute |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: |
| 1 | Waterloo | `940GZZLUWLO` | 5.25 | 1.76 | 7.02 | 5 | 0.70 |
| 2 | Bank and Monument | `940GZZLUBNK` | 0.41 | 5.12 | 5.54 | 2 | 0.55 |
| 3 | King's Cross St. Pancras | `940GZZLUKSX` | 2.46 | 2.60 | 5.06 | 5 | 0.51 |
| 4 | Victoria | `940GZZLUVIC` | 2.60 | 2.08 | 4.69 | 4 | 0.47 |
| 5 | Liverpool Street | `940GZZLULVT` | 1.77 | 2.81 | 4.59 | 4 | 0.46 |
| 6 | London Bridge | `940GZZLULNB` | 2.08 | 2.36 | 4.44 | 4 | 0.44 |
| 7 | Paddington | `940GZZLUPAC` | 2.27 | 1.61 | 3.88 | 4 | 0.39 |
| 8 | Oxford Circus | `940GZZLUOXC` | 0.23 | 3.23 | 3.47 | 4 | 0.35 |
| 9 | Canary Wharf | `940GZZLUCYF` | 0.61 | 2.81 | 3.42 | 4 | 0.34 |
| 10 | Farringdon | `940GZZLUFCN` | 0.69 | 2.48 | 3.17 | 3 | 0.32 |

The ranking names stations other than the four.
Waterloo, Bank, and King's Cross are the top three, and Victoria, Liverpool Street, and London Bridge all rank above Paddington.
None of the four is a Compact station, and none refuses banks.
The eight default Compact stations rank 139th or lower.
The station study says that the project has 5 berths at each station, but that holds only for its two stations.

The largest share of one station in any band is 7.20%, at King's Cross in the interpeak.
At that share, 7 trip ends a minute need about 97 orders a minute.
This is arithmetic from the weights, not a run.

## Layouts

Each layout uses the coordinates of the two-bank King's Cross of the station study, or their mirror image.
The script gives the same lanes, nodes, and stations as the station study's generator for King's Cross with 3 + 2 berths.
The road lanes go to a bank by the side of their portal, as they do at King's Cross.
The side at each station is the one that crosses fewer network links.

Berths stay at the station's count where a bank can hold them: 3 + 2 at King's Cross and Waterloo, and 2 + 2 at Paddington.
Bank has 2 berths, and 1 + 1 would give each bank one berth, so its layout is 2 + 2 and it gains 2 berths.
Thus the result at Bank does not separate the second path from the added berths.

The validators refused no layout, and no layout had to bend or move after its first placement.
No station reached the 60-minute limit on layout effort.

| Measure | King's Cross | Waterloo | Paddington | Bank |
| --- | ---: | ---: | ---: | ---: |
| Berths, bank a + bank b | 3 + 2 | 3 + 2 | 2 + 2 | 2 + 2 |
| Side of bank b | left | right | right | right |
| Road lanes to bank b, in and out | 4 of 7 | 4 of 6 | 2 of 5 | 4 of 8 |
| Lanes added / removed / net | 11 / 8 / +3 | 11 / 8 / +3 | 11 / 8 / +3 | 11 / 0 / +11 |
| Nodes added / removed / net | 10 / 6 / +4 | 10 / 6 / +4 | 10 / 6 / +4 | 10 / 0 / +10 |
| Total length of the changed road lanes, base to banks, m | 887 to 1,357 | 817 to 1,248 | 371 to 549 | 876 to 1,273 |
| Second comb from the first, m | 272 | 272 | 272 | 272 |
| Farthest station node from the comb center, base to banks, m | 403 to 425 | 403 to 425 | 330 to 425 | 193 to 425 |
| Links crossed, base to banks | 2 to 5 | 2 to 4 | 1 to 3 | 2 to 6 |
| Lane crossings, base to banks | 2 to 14 | 6 to 19 | 1 to 6 | 6 to 23 |
| Links crossed on the other side | 10 | 10 | 6 | 10 |
| Nearest lane of another station, m | 381, Euston | 157, Southwark | 69, Edgware Road (Circle Line) | 10, Moorgate |

The station study gives 13 lane crossings at King's Cross, and its script gives 14 on the same layout.
At Waterloo and Bank, the second comb itself crosses a link in both directions: `london-link-100` and `london-link-097`.
The lanes of the second comb at Bank come within 10 m of Moorgate, and this is below the 12 m of the audit.
The validator accepts such a pair because the paths are on different planes, so each one needs a flyover.
These flyovers have no cost or fit check against the map.
The generator's layout audit does not run on a hand-built project, so it did not check these layouts.

The all-banked project has 8 banks and 736 passenger berths.

| Limit | Base | All four banked | Maximum |
| --- | ---: | ---: | ---: |
| Stations | 312 | 312 | 600 |
| Nodes | 5,740 | 5,762 | 12,000 |
| Lanes | 9,024 | 9,044 | 20,000 |
| Blocks | 55,803 | 55,970 | 64,000 |
| Lane pairs at nodes | 49,440 | 49,316 | 250,000 |
| Compact JSON, bytes | 5,608,578 | 5,615,932 | 33,554,432 |

## Flood

Paddington and Bank have a baseline in this tree.
Waterloo has the baseline of the station study, because the production code is the same.
Paddington landed within about 1 entry a minute of its baseline, so it has seeds 1, 2, and 3.
Bank and Waterloo have seed 1 only, and one seed cannot show the spread.

| Arm | Entries a minute | Exits a minute | Offered | Skipped | Accepted | Boarded | Completed |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Paddington, baseline | 5.60 / 6.10 / 5.78 | 5.40 / 5.80 / 5.65 | 1,199 | 477 / 459 / 480 | 722 / 740 / 719 | 522 / 540 / 519 | 310 / 329 / 316 |
| Paddington, 2 banks | 5.15 / 5.60 / 5.40 | 4.75 / 5.30 / 5.05 | 1,199 | 482 / 464 / 479 | 717 / 735 / 720 | 517 / 535 / 520 | 285 / 307 / 294 |
| Bank, baseline | 5.50 | 5.30 | 1,199 | 475 | 724 | 524 | 297 |
| Bank, 2 banks | 8.47 | 8.18 | 1,199 | 407 | 792 | 594 | 443 |
| Waterloo, baseline (station study) | 6.53 | 5.85 | 1,199 | 444 | 755 | 555 | 341 |
| Waterloo, 2 banks | 8.32 | 8.12 | 1,199 | 407 | 792 | 593 | 429 |

| Arm | Free berth while a pod waits, % | Linked entries, % | Entry headway p50, s | Tail mean, m | Stopped, 1000 pod-seconds |
| --- | ---: | ---: | ---: | ---: | ---: |
| Paddington, baseline | 98.1 / 94.1 / 98.2 | 91 / 89 / 82 | 2.8 / 2.7 / 4.9 | 253 / 166 / 137 | 247 / 255 / 219 |
| Paddington, 2 banks | 99.6 / 99.4 / 100.0 | 13 / 16 / 13 | 10.7 / 9.9 / 11.3 | 387 / 337 / 320 | 286 / 279 / 246 |
| Bank, baseline | 65.5 | 80 | 10.6 | 922 | 338 |
| Bank, 2 banks | 94.8 | 45 | 7.2 | 461 | 194 |
| Waterloo, baseline (station study) | 99.5 | 66 | 10.2 | 657 | 261 |
| Waterloo, 2 banks | 100.0 | 23 | 7.7 | 350 | 155 |

The free-berth share does not fall in any arm.
It rises from 65.5 to 94.8% at Bank, where no berth is free in about a third of the baseline samples with a queue.
Banks at Bank have 4 berths, against 2.
So the arm at Bank does not show how much of its 2.97 entries a minute comes from the second path.

The linked share falls in each 2-bank arm.
At Paddington, it falls from 82 to 91% to 13 to 16%, the median entry gap grows to about 10 s, and entries fall.
This agreement does not prove a cause, and this study did not measure why links fail there.
The preset split at Paddington is also uneven.
Free-flow routes from uniform origins put 73% of the pods on `road-in-03`, and no split of lanes can divide one lane.
In the run, bank b takes 165 / 180 / 177 entries and bank a takes 128 / 133 / 121.

Matched on request, against the baseline of the same seed:

| Arm | Jointly boarded | Wait, s | Over the wait limit | Jointly completed | Journey, s | Over the journey limit |
| --- | ---: | --- | ---: | ---: | --- | ---: |
| Paddington, seed 1 | 502 | 491 to 497 | 84 | 275 | 1,226 to 1,269 | 119 |
| Paddington, seed 2 | 519 | 479 to 498 | 104 | 303 | 1,268 to 1,298 | 114 |
| Paddington, seed 3 | 494 | 528 to 520 | 75 | 287 | 1,281 to 1,375 | 144 |
| Bank, seed 1 | 522 | 462 to 371 | 24 | 295 | 1,350 to 936 | 4 |
| Waterloo, seed 1 | not matched | not matched | not matched | not matched | not matched | not matched |

The limits are the individual limits of the gates: 30 s or 5% for a wait, and 60 s or 5% for a journey.
Requests that board or finish in one arm only are censored, and these comparisons do not count them.
At Bank, 145 requests complete only with banks and 2 only without.
At Paddington, 35 / 26 / 27 complete only without banks and 9 / 4 / 3 only with them.

The Waterloo baseline has no request records, because the station study's output is no longer on this host and its rerun was not in this screen.
So the Waterloo arm has aggregates only.
Against the station study's seed 1, its wait mean goes from 670 to 639 s, and its journey mean from 1,348 to 1,293 s.
Those are means over different sets of requests, so they are not a matched result.

In a flood, each order goes to the focus station, so other stations appear only as origins.
Most origins have fewer than 5 matched requests, which is too few to compare.
No origin skips more than 3 more offers.
At Bank, 97 origins skip fewer and 47 skip more.
At Paddington, 79 / 79 / 73 skip more and 73 / 70 / 68 skip fewer.

## AM profile

All arms offer and accept 1,799 orders and skip none.
Each cell gives seeds 1 / 2 / 3.

| Station | Arm | Entries a minute | Exits a minute | Highest 10-minute entries a minute | Samples with a free berth, % |
| --- | --- | ---: | ---: | ---: | ---: |
| King's Cross | Baseline | 0.34 / 0.39 / 0.36 | 0.34 / 0.40 / 0.33 | 0.7 / 0.8 / 0.8 | 100.0 / 90.9 / 87.4 |
| King's Cross | 2 banks | 0.33 / 0.42 / 0.34 | 0.33 / 0.41 / 0.33 | 0.7 / 0.9 / 0.8 | 100.0 / 98.3 / 99.2 |
| Waterloo | Baseline | 0.54 / 0.53 / 0.51 | 0.54 / 0.51 / 0.51 | 0.9 / 1.1 / 0.9 | 100.0 / 100.0 / 100.0 |
| Waterloo | 2 banks | 0.51 / 0.51 / 0.45 | 0.51 / 0.50 / 0.46 | 0.9 / 1.1 / 0.9 | 100.0 / 100.0 / 100.0 |
| Paddington | Baseline | 0.24 / 0.25 / 0.34 | 0.26 / 0.24 / 0.34 | 0.6 / 0.6 / 0.8 | 99.7 / 100.0 / 100.0 |
| Paddington | 2 banks | 0.26 / 0.24 / 0.33 | 0.27 / 0.23 / 0.33 | 0.6 / 0.5 / 0.7 | 100.0 / 100.0 / 100.0 |
| Bank | Baseline | 0.49 / 0.50 / 0.47 | 0.51 / 0.49 / 0.48 | 1.1 / 1.0 / 1.0 | 25.0 / 13.4 / 12.0 |
| Bank | 2 banks | 0.50 / 0.49 / 0.47 | 0.51 / 0.47 / 0.49 | 1.2 / 0.9 / 1.0 | 37.7 / 28.6 / 24.8 |

The 10-minute windows start in minutes 20 to 179, so the last of them reach into the drain hour.
A tail exists in at most 1% of the samples at any of the four stations, so the share of samples with a queue and a free berth has too few samples to read.
The last column therefore covers all samples of the window.
Bank is a destination station at this hour, and its berths are often taken, with 3.3 to 3.7 of its 4 berths occupied in the banked arm.
This arm has positioning off, as the onboard pickup study has.

At the preset rate, the busiest station is at 0.54 entries a minute.
Linear scaling gives about 1.1 at 20 orders a minute, a rate that the [onboard pickup study](sharing-onboard.md) finds above AM supply.
That scaling is arithmetic, not a run.

| Measure | Baseline | All four banked |
| --- | ---: | ---: |
| Completed | 1,798 / 1,798 / 1,798 | 1,797 / 1,798 / 1,798 |
| Boarded | 1,799 / 1,799 / 1,799 | 1,799 / 1,799 / 1,799 |
| Wait mean, s | 263 / 245 / 251 | 267 / 247 / 250 |
| Wait p95, s | 980 / 877 / 938 | 976 / 866 / 934 |
| Journey mean, s | 984 / 966 / 983 | 992 / 972 / 987 |
| Journey p95, s | 2,346 / 2,244 / 2,241 | 2,265 / 2,223 / 2,243 |
| Peak pending | 65 / 70 / 64 | 69 / 66 / 62 |
| Late backlog change | 14 / -13 / 11 | 16 / -20 / 10 |

Matched on request, against the baseline of the same seed, with each cell in seconds:

| Orders | Jointly boarded | Wait | Over the wait limit | Jointly completed | Journey | Over the journey limit |
| --- | ---: | --- | ---: | ---: | --- | ---: |
| All | 1,799 / 1,799 / 1,799 | 263 to 267 / 245 to 247 / 251 to 250 | 200 / 177 / 154 | 1,797 / 1,798 / 1,798 | 984 to 992 / 966 to 972 / 983 to 987 | 196 / 165 / 137 |
| Touching the four | 350 / 349 / 363 | 170 to 170 / 173 to 168 / 186 to 178 | 28 / 29 / 26 | 350 / 349 / 363 | 712 to 739 / 754 to 773 / 781 to 796 | 89 / 74 / 65 |
| Other stations | 1,449 / 1,450 / 1,436 | 285 to 291 / 263 to 266 / 267 to 269 | 172 / 148 / 128 | 1,447 / 1,449 / 1,435 | 1,049 to 1,053 / 1,017 to 1,019 / 1,034 to 1,036 | 107 / 91 / 72 |

Banks do not pass the no-harm gate on this demand, and the wait and journey means gain nothing.

The journey of an order from a banked station rises in each seed, and the wait shows no such rise.

| Origin | Orders | Journey, s | Over the journey limit | Static free-flow to the destinations, s |
| --- | ---: | --- | ---: | --- |
| King's Cross | 58 / 51 / 35 | 458 to 516 / 514 to 575 / 477 to 547 | 28 / 21 / 16 | 354 to 401 |
| Waterloo | 88 / 90 / 83 | 441 to 488 / 442 to 472 / 451 to 475 | 37 / 29 / 25 | 288 to 331 |
| Paddington | 39 / 36 / 50 | 635 to 673 / 628 to 660 / 680 to 710 | 15 / 10 / 16 | 442 to 489 |
| Bank | 8 / 7 / 5 | 356 to 387 / 553 to 604 / 449 to 494 | 2 / 4 / 2 | 363 to 385 |

Orders to a banked station show no such rise.
A bank uses only its own road lanes, so a pod that leaves it can reach only those portals.
The last column calculates the AM-weighted free-flow time from the merge of each bank, with about the bank shares of the seed 1 entries.
The rises in the journey are of the same size as this calculation.
This agreement does not prove a cause, and the calculation ignores the route rules at other stations.

Effects at other stations go in both directions, with more stations higher than lower in most rows.
The table counts stations with at least 5 matched orders and without banks, by the mean change.

| Grouping | Stations | Wait higher / lower | Journey higher / lower |
| --- | ---: | --- | --- |
| By origin, seed 1 | 127 | 67 / 52 | 76 / 51 |
| By origin, seed 2 | 116 | 57 / 52 | 58 / 58 |
| By origin, seed 3 | 136 | 70 / 50 | 77 / 58 |
| By destination, seed 1 | 89 | 53 / 35 | 62 / 26 |
| By destination, seed 2 | 86 | 39 / 45 | 47 / 39 |
| By destination, seed 3 | 82 | 43 / 37 | 52 / 29 |

The largest changes are at most 143 s in the mean, on groups of 5 to 23 orders.
Among the five largest wait rises of each row, only South Woodford by origin repeats between seeds.
This study did not separate these changes from the fleet histories that differ between any two arms.

## Bank searches

The all-banked project is a larger bank network, so the shared search tree of `9a27c19a` could use it.
With any bank in the network, each preferred nearest search goes through `bankNearest`.

| Measure | Seed 1 | Seed 2 | Seed 3 |
| --- | ---: | ---: | ---: |
| `bankNearest` calls in 432,000 ticks | 116 | 114 | 130 |
| Goals | 63,652 | 62,691 | 71,656 |
| Goals for each call | 549 | 550 | 551 |
| Most calls in one tick | 1 | 1 | 1 |

The baseline has no such calls.
The flood arms have 0 at Waterloo and Paddington and 2 at Bank.

A call here has the size of the branch's fixture: about 550 goals, against about 560 middle-part searches there.
The network is about half as large, with 5,762 nodes against 12,000.
So the project is a fair test of the cost of one call on a real layout.
It is not a fair test of the branch's tick claim, for these reasons:

- The claim is about a tick that parks four released pods, and here no tick has more than one call.
- Only about 0.03% of the ticks have a call.
- At the branch's old cost of 32 ms, the calls of a run sum to about 4 s, in a run of 241 to 259 s with the oracle and the probe on.
- The sampled warm tick needs the fault and emergency samplers, and this arm has no incidents.

A whole-run time on this project can show little of the gain.
A timer on a single call can show it.
The search count inside each call is not measured here, because the counters read calls and goals.

## King's Cross at this tip

This study did not rerun the King's Cross flood, so it does not confirm those numbers on a stock binary at this tip.
The production code is the same, and the layout is the same, but this is not a run.
The King's Cross layout ran only on the AM profile, inside the all-banked arm.

## Not verified

- Waterloo has no matched wait or journey, because its baseline was not rerun.
- Bank and Waterloo have one flood seed.
- The Bank arm does not separate the second path from the added berths, because no 1 + 1 arm ran.
- No arm covers a band other than AM, a rate other than 10 orders a minute on the profile, mixed arrivals, or a rail-hub event.
- The flood arms cover 60 minutes, with no 120-minute sustained check.
- The cause of the low linked share with banks at Paddington, Waterloo, and Bank is open.
- This screen tried one layout at each station, and it did not try a different split at Paddington.
- The flyovers have no cost or fit check against the map, and the layout audit did not run on these layouts.
- The arms use the hooked and probed binary, with the hooks unset, and no control arm without the probe ran at this tip.
- The restore, stream, and bound checks did not run on the banked projects, beyond the project validators and simulation admission.
- Victoria, Liverpool Street, London Bridge, and the rest of the top 10 have no entry rate and no layout.
