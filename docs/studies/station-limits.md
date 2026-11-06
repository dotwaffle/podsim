# Station limits on the LondonFull flood

This study asks what limits King's Cross (`940GZZLUKSX`) and Waterloo (`940GZZLUWLO`) under a flood of orders to one station, and what each change buys.
The answer is a stack of junction conflict zones, not berths.
Berths are free in at least 99.4% of the samples with a queue, and more berths do not raise the rate.
One bank gives 7.10 to 7.47 entries a minute at King's Cross and 6.25 to 6.85 at Waterloo in the flood window.
The sustained rates are 6.45 to 6.77 and 5.48 to 5.83.
The merge limits both stations first, and the next limit differs: the comb at King's Cross and the diverge at Waterloo.
On one bank, every safe arm stays at or below 9.03 at King's Cross and 7.58 at Waterloo.
A second bank at King's Cross gives 9.45 to 10.38 with the stock rules.
Three banks with a doubled fleet give 14.65 to 15.18.
Waterloo has no bank arm.

This is a three-seed screen.
It does not meet the adoption gates of [experimental-adoption.md](../design/experimental-adoption.md), which require seeds 1 through 10 and all supported demand bands.
It qualifies no setting, layout, or rule.
No default, preset, contract, simulation rule, or layout changes with this study.

## Method

The source is `1932147` plus scratch hooks and a probe in `cmd/compare`.
The hooks and the probe do not land.
The local branch `study/station-limits` holds them.
A control arm of the same tree without the probe gives identical result rows (378 / 370 / 386 completed at King's Cross).

The project comes from `scenario -preset london-full -output base.json`.
It has 327 pods, 5 berths at each station, and platoon limit 4.
The flood is 20 orders a minute to one station, for 60 simulated minutes:

```sh
compare -project base.json -pattern destination -focus 940GZZLUKSX \
  -loads 3s -duration 60m -arrivals-for 60m -seeds 1,2,3 \
  -redistribution-policies on -platoon-policies virtual \
  -pickup-reassignment on -workers 1 -format json
```

The same command with `-focus 940GZZLUWLO` is the Waterloo flood.
Each cell gives seeds 1 / 2 / 3.
The rates, the headways, the tails, and the shares cover minutes 20 to 60, unless a row says otherwise.
The probe samples each second.
A tail is the chain of slow pods bound for the station, with gaps of at most 60 m, from a head within 250 m of the access lane.
Each arm has the schedule of its baseline: 1,199 offers, with the same schedule ID in each pair of arms.

Each arm has one of four labels.

| Label | Meaning |
| --- | --- |
| Buildable | A layout in the project format, with the stock simulation rules. |
| Sim rule | A scratch change to a simulation rule. The separation oracle ran at each tick. |
| Clearance | A scratch change to the `Clearance` value. The oracle threshold moves with it. |
| Upper bound | The arm removes a conflict in the simulation. It is not a buildable layout, and it has no safety claim. |

"Oracle clean" means that `SafetyObservation.Check` found no failure in 108,000 ticks.
The smallest gap in each clean arm is 6.000 m, which is the certified depot pair.

The table gives the scratch setting of each arm that is not a plain layout.

| Arm | Setting |
| --- | --- |
| Exit free | `STUDY_FREE=exit`. The departure and exit lanes of the station hold no node, junction, or track resource. The same applies to the node and junction resources at the start of the roads that these lanes feed. Pods still follow each other. |
| Exit local | `STUDY_FREE=exit-local`. As exit free, but the road starts keep their resources. |
| Entry free | `STUDY_FREE=entry`. The `road-in` lanes, `access-in`, and the arrival lanes hold only berth resources. |
| In-road | `STUDY_FREE=in-road`. The `road-in` lanes and `access-in` hold only berth resources, except that `access-in` keeps the entry node. |
| In-comb | `STUDY_FREE=in-comb`. The arrival lanes hold only berth resources, and `access-in` gives up the entry node. |
| Out-road | `STUDY_FREE=out-road`. The `road-out` lanes and `access-out` hold nothing, except that `access-out` keeps the exit node. |
| Out-comb | `STUDY_FREE=out-comb`. The departure lanes hold nothing, and `access-out` gives up the exit node. |
| Limit 8 | `STUDY_PLATOON_LIMIT=8`. The tree caps the limit at 4. |
| Any speed | `STUDY_SLOW_FRACTION=1`. A pod links at any speed below the lane limit. |
| Comb run | `STUDY_RUN_COMB=1`. A link run passes the entry node into the arrival links. |
| Pair | `STUDY_PAIR=1`. Each conflicting pair of lanes at the station gets its own junction resource, instead of one for each node. |
| Cell 10 | `STUDY_CELL=10`. Track cells at the station are 10 m long, not 30 m. The block limit of the project does not count them. |
| Turn 155 | `STUDY_MAX_TURN_DEG=155`. The link turn cap is 155 degrees, not 120. The implied link clearance is about 55 m. |
| Clearance 8 | `Clearance` is 8 m, not 12 m, through a Go overlay of `internal/sim/simulation.go`. |
| Fleet doubled | `scenario -preset london-full -station-pods 2` gives 636 pods. |

Service comparisons follow the [matched rules](../design/experimental-adoption.md#evidence-required).
Skipped, accepted, boarded, and completed counts stay separate.
A wait comparison uses only requests that board in both arms.
A journey comparison uses only requests that finish in both arms.

## Baseline

| Measure | King's Cross | Waterloo |
| --- | ---: | ---: |
| Entries a minute | 7.12 / 7.10 / 7.47 | 6.53 / 6.25 / 6.85 |
| Exits a minute | 6.70 / 6.50 / 6.67 | 5.85 / 5.88 / 6.12 |
| Entry headway p50, s | 2.7 / 3.3 / 2.4 | 10.2 / 10.8 / 10.8 |
| Entry headway p90, s | 18.8 / 19.6 / 19.9 | 14.1 / 14.7 / 13.7 |
| Exit headway p50, s | 4.3 / 4.2 / 4.3 | 4.2 / 4.3 / 4.3 |
| Exit headway p90, s | 22.9 / 24.7 / 24.3 | 27.4 / 29.1 / 26.3 |
| Completed | 378 / 370 / 386 | 341 / 323 / 349 |
| Skipped | 430 / 425 / 416 | 444 / 460 / 443 |
| Wait mean, s | 641 / 645 / 650 | 670 / 671 / 659 |
| Wait p95, s | 1,815 / 1,893 / 1,790 | 1,893 / 1,953 / 1,881 |
| Journey mean, s | 1,329 / 1,351 / 1,350 | 1,348 / 1,326 / 1,379 |
| Journey p95, s | 2,515 / 2,626 / 2,580 | 2,634 / 2,451 / 2,451 |
| Stopped, 1000 pod-seconds | 219 / 243 / 163 | 261 / 329 / 254 |
| Tail mean, m | 521 / 442 / 360 | 657 / 673 / 703 |
| Tail p95, m | 1,029 / 1,150 / 762 | 1,188 / 1,260 / 1,469 |
| Tail mean, pods | 50 / 39 / 31 | 48 / 47 / 51 |
| Late backlog change | -17 / -11 / 12 | 4 / -11 / -19 |
| Samples with a queue, % | 100 / 100 / 100 | 100 / 100 / 100 |
| Free berth while a pod waits, % | 99.5 / 100.0 / 99.8 | 99.5 / 99.9 / 100.0 |
| Linked entries, % | 91 / 91 / 93 | 66 / 65 / 62 |

The flood saturates both stations.
About 36% of the offers at King's Cross and 37% at Waterloo skip at the queue limit of 200, and that limit also holds the late backlog near zero.
Late backlog therefore says little in this study.
A pod queues at the station for all of the window.

Entries exceed exits in this window because the stock of pods in the station grows.
Two 120-minute runs, with the window at minutes 60 to 120, show the sustained rate.
There, entries equal exits.

| Arm | Entries a minute | Exits a minute | Entry headway p50, s | Completed | Skipped | Tail mean, m | Free berth, % | Oracle |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| King's Cross, 120 min | 6.77 / 6.45 / 6.60 | 6.68 / 6.53 / 6.73 | 2.5 / 3.3 / 2.6 | 782 / 764 / 788 | 1,214 / 1,230 / 1,218 | 616 / 546 / 524 | 99 / 100 / 99 | off |
| Waterloo, 120 min | 5.78 / 5.83 / 5.48 | 5.77 / 5.73 / 5.57 | 12.0 / 12.8 / 10.9 | 679 / 671 / 683 | 1,301 / 1,314 / 1,305 | 357 / 323 / 582 | 100 / 100 / 100 | off |
| King's Cross, platoons off | 4.35 / 4.38 / 4.35 | 4.38 / 4.40 / 4.38 | 16.5 / 16.4 / 16.4 | 240 / 239 / 239 | 485 / 494 / 503 | 1,595 / 1,719 / 1,295 | 100 / 100 / 100 | off |
| Waterloo, platoons off | 4.75 / 4.55 / 4.62 | 4.72 / 4.58 / 4.60 | 12.5 / 12.5 / 12.5 | 255 / 242 / 246 | 477 / 502 / 495 | 1,058 / 1,495 / 1,274 | 100 / 100 / 100 | off |

The mean entry gap at King's Cross is about 8 s, but the median is 2.4 to 3.3 s and the p90 is 19 to 20 s.
Pods enter in linked groups, with long gaps between the groups.
Platoons already give the short gap inside a group, so the limit is the gap between groups.
At Waterloo, 62 to 66% of entries are linked, against 91 to 93% at King's Cross, and the median gap is 10.2 to 10.8 s.

## Stage ceilings

The table calculates each stage from the dump of lanes and cells of the project.
The speed is 14 m/s, the acceleration is 2 m/s², and the stopping distance is 49 m.
A solo pod holds a junction zone of length Z for (Z + 49) / 14 seconds.
A group of four is one restart plus three linked gaps of 2.7 s, which is the measured median.
Berth time is 2 s of unload plus the claim and the clearing, about 15.2 s for each visit.

| Stage | King's Cross, pods a minute | Waterloo, pods a minute | Basis |
| --- | --- | --- | --- |
| Diverge, solo | 4.2 to 7.4 | 3.8 to 6.5 | Z of 64 to 150 m, and 80 to 169 m |
| Diverge, groups of 4 | 10.3 to 13.9 | 9.7 to 13.0 | The same Z |
| Diverge, groups of 8 | 14.0 to 17.1 | 13.5 to 16.4 | The same Z |
| Entry node, solo | 7.9 | 8.5 | Z of 57.8 m, and 50.3 m |
| Comb row node, solo | 8.1 | 8.7 | Z of 55.0 m, and 47.5 m |
| Exit node, solo | 7.9 | 8.5 | Z of 57.8 m, and 50.3 m |
| Merge, solo | 5.2 to 7.3 | 3.9 to 6.8 | Z of 67 to 112 m, and 74 to 166 m |
| Plain following | 11.1 | 11.1 | 5.4 s |
| Linked following at 120 degrees | 27.1 | 27.1 | 2.2 s |
| Berths | about 20 | about 20 | 5 berths at 15.2 s |

Each row is a stage alone, and linked pods can pass the solo rows.
With groups at the diverge, the merge has the lowest calculated ceiling at both stations.
The exit rate of the long runs falls in the merge range: 6.53 to 6.73 at King's Cross, and 5.57 to 5.77 at Waterloo.
The `road-in` lanes hold the diverge junction over nearly their whole length, because the portals are about 12 m apart and near parallel.
The lane reservation joins contiguous junction zones into one movement, and this makes Z long.
Berths are not close to any other stage.

The fleet is a separate ceiling.
With all conflicts removed, the 327-pod fleet gives 10.65 to 12.12 entries a minute with no tail.
A doubled fleet gives 16.10 to 17.12 in the same arm.

## Upper bounds

These arms are not buildable.

| Arm | Entries a minute | Exits a minute | Entry headway p50, s | Completed | Skipped | Tail mean, m | Free berth, % | Oracle |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| King's Cross, exit free | 7.40 / 7.30 / 7.35 | 7.55 / 7.40 / 7.35 | 3.2 / 3.0 / 2.4 | 396 / 384 / 389 | 390 / 387 / 398 | 508 / 513 / 416 | 100 / 100 / 100 | off |
| King's Cross, exit local | 7.40 / 7.30 / 7.35 | 7.55 / 7.40 / 7.35 | 3.2 / 3.0 / 2.4 | 396 / 384 / 389 | 390 / 387 / 398 | not rerun | not rerun | off |
| King's Cross, out-road | 7.47 / 7.30 / 7.42 | 7.53 / 7.38 / 7.40 | 2.8 / 2.9 / 2.4 | 396 / 384 / 390 | 378 / 399 / 397 | 367 / 512 / 341 | 100 / 100 / 100 | off |
| King's Cross, out-comb | 7.45 / 7.25 / 7.42 | 6.60 / 6.65 / 6.92 | 2.6 / 3.3 / 2.6 | 397 / 388 / 391 | 422 / 426 / 420 | 513 / 579 / 366 | 100 / 100 / 100 | off |
| King's Cross, entry free | 9.07 / 9.20 / 8.45 | 6.72 / 6.75 / 6.67 | 4.8 / 5.6 / 5.5 | 407 / 403 / 403 | 416 / 413 / 416 | not rerun | not rerun | off |
| King's Cross, in-road | 9.00 / 9.03 / 8.57 | 6.47 / 6.55 / 6.75 | 5.1 / 5.3 / 5.9 | 373 / 372 / 376 | 431 / 413 / 416 | not rerun | not rerun | off |
| King's Cross, in-comb | 9.18 / 8.75 / 8.97 | 6.62 / 6.62 / 6.72 | 1.9 / 2.2 / 2.2 | 393 / 389 / 402 | 411 / 422 / 420 | 272 / 357 / 166 | 42 / 50 / 29 | off |
| King's Cross, in-road and exit | 9.45 / 9.40 / 8.82 | 7.15 / 7.03 / 7.05 | 4.5 / 4.9 / 5.5 | 381 / 376 / 377 | 405 / 399 / 405 | 0 / 0 / 0 | 0 / 0 / 0 | off |
| King's Cross, in-comb and exit | 9.07 / 9.15 / 9.60 | 9.00 / 9.18 / 9.62 | 2.0 / 2.0 / 1.8 | 470 / 464 / 488 | 353 / 345 / 330 | 253 / 373 / 208 | 98 / 99 / 99 | off |
| King's Cross, entry and exit free | 11.55 / 12.12 / 10.65 | 11.45 / 12.25 / 10.68 | 4.1 / 4.0 / 4.7 | 579 / 611 / 545 | 264 / 237 / 289 | not rerun | not rerun | off |
| Waterloo, exit free | 7.15 / 7.05 / 6.95 | 7.17 / 7.22 / 6.95 | 10.7 / 10.8 / 10.8 | 369 / 366 / 358 | 416 / 402 / 426 | 733 / 662 / 716 | 100 / 100 / 100 | off |
| Waterloo, exit local | 7.15 / 7.05 / 6.95 | 7.17 / 7.22 / 6.95 | 10.7 / 10.8 / 10.8 | 369 / 366 / 358 | 416 / 402 / 426 | not rerun | not rerun | off |
| Waterloo, out-road | 7.08 / 6.92 / 6.80 | 7.03 / 7.03 / 6.80 | 10.7 / 10.8 / 10.8 | 363 / 360 / 350 | 422 / 414 / 418 | 768 / 647 / 740 | 100 / 100 / 100 | off |
| Waterloo, out-comb | 7.12 / 6.80 / 6.80 | 6.25 / 6.67 / 6.20 | 10.4 / 10.8 / 10.8 | 363 / 354 / 355 | 445 / 435 / 442 | 592 / 545 / 683 | 100 / 100 / 100 | off |
| Waterloo, entry free | 8.35 / 8.12 / 7.85 | 5.42 / 5.42 / 5.80 | 6.1 / 5.9 / 6.2 | 333 / 335 / 347 | 462 / 460 / 453 | not rerun | not rerun | off |
| Waterloo, in-road | 8.50 / 8.25 / 7.72 | 6.05 / 5.80 / 5.80 | 5.7 / 5.5 / 5.9 | 337 / 332 / 341 | 448 / 450 / 450 | not rerun | not rerun | off |
| Waterloo, in-comb | 7.03 / 6.83 / 6.75 | 5.97 / 5.55 / 5.95 | 10.8 / 10.9 / 10.8 | 351 / 330 / 344 | 449 / 462 / 452 | not rerun | not rerun | off |
| Waterloo, in-road and exit | 9.60 / 9.43 / 9.15 | 7.62 / 7.62 / 7.67 | 5.1 / 5.2 / 5.7 | 399 / 394 / 399 | 392 / 391 / 390 | 0 / 0 / 0 | 0 / 0 / 0 | off |
| Waterloo, in-comb and exit | 7.25 / 7.00 / 6.88 | 7.17 / 7.08 / 6.90 | 10.7 / 10.8 / 10.8 | 370 / 365 / 357 | 409 / 401 / 414 | 770 / 583 / 683 | 100 / 100 / 100 | off |
| Waterloo, entry and exit free | 11.32 / 11.82 / 10.78 | 11.38 / 12.03 / 11.05 | 4.3 / 4.0 / 4.8 | 571 / 610 / 563 | 271 / 232 / 284 | not rerun | not rerun | off |

| Arm | Entries a minute | Exits a minute | Entry headway p50, s | Completed | Skipped | Tail mean, m | Free berth, % | Oracle |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| King's Cross, baseline | 7.10 / 7.20 / 7.20 | 6.55 / 6.80 / 6.58 | 9.3 / 5.7 / 3.7 | 384 / 378 / 380 | 149 / 139 / 119 | 1,673 / 1,356 / 1,083 | 61 / 67 / 59 | off |
| King's Cross, exit free | 7.15 / 7.22 / 7.30 | 7.28 / 7.38 / 7.47 | 8.6 / 5.2 / 3.4 | 388 / 380 / 392 | 135 / 125 / 106 | 1,633 / 1,509 / 1,097 | 52 / 60 / 51 | off |
| King's Cross, entry free | 15.38 / 15.28 / 15.57 | 6.75 / 6.83 / 6.83 | 3.1 / 3.0 / 2.9 | 415 / 412 / 426 | 126 / 120 / 101 | 0 / 1 / 0 | 0 / 0 / 0 | off |
| King's Cross, in-road and exit | 15.20 / 15.18 / 15.40 | 7.17 / 7.12 / 7.15 | 3.0 / 3.1 / 2.9 | 380 / 376 / 378 | 133 / 118 / 117 | 0 / 0 / 0 | 100 / 0 / 100 | off |
| King's Cross, in-comb and exit | 7.88 / 8.15 / 8.47 | 8.18 / 8.25 / 8.75 | 9.3 / 9.3 / 2.4 | 442 / 427 / 460 | 88 / 73 / 68 | 1,327 / 1,174 / 876 | 47 / 52 / 39 | off |
| King's Cross, entry and exit free | 16.38 / 17.12 / 16.73 | 16.32 / 17.00 / 16.60 | 2.9 / 2.7 / 2.8 | 785 / 821 / 797 | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 14 / 0 | off |
| Waterloo, baseline | 6.00 / 5.80 / 6.17 | 5.67 / 5.53 / 5.58 | 10.8 / 10.1 / 10.3 | 326 / 310 / 326 | 193 / 196 / 195 | 1,713 / 1,670 / 1,801 | 71 / 82 / 80 | off |
| Waterloo, exit free | 6.85 / 7.05 / 7.17 | 6.97 / 7.25 / 7.28 | 10.8 / 10.1 / 10.1 | 364 / 370 / 376 | 140 / 118 / 126 | 1,611 / 1,699 / 1,727 | 53 / 57 / 51 | off |
| Waterloo, entry and exit free | 16.15 / 16.85 / 16.10 | 15.97 / 16.48 / 16.32 | 3.1 / 3.0 / 3.2 | 771 / 813 / 790 | 2 / 0 / 0 | 1 / 6 / 1 | 0 / 2 / 0 | off |

The second table uses the doubled fleet.
The arms show these limits.
The merge is the lowest at both stations, and the fleet is the highest.
The order of the two entry stages differs between the stations: the comb is lower at King's Cross, and the diverge is lower at Waterloo.

| Limit | King's Cross, a minute | Waterloo, a minute | Evidence |
| --- | --- | --- | --- |
| Merge zone | 6.53 to 6.73 | 5.57 to 5.77 | Long runs. Out-road raises exits more than out-comb does. |
| Comb and entry node | 7.03 to 7.17 | 7.62 to 7.67 | Exits of in-road and exit free. |
| Diverge with platoons | 9.07 to 9.60 | 6.88 to 7.25 | Entries of in-comb and exit free. |
| Fleet of 327 | 10.65 to 12.12 | 10.78 to 11.82 | Entry and exit free, with no tail. |

Exit free alone gives 0.12 entries a minute on the mean at King's Cross, because the comb limit is next.
At Waterloo it gives 0.5, up to the diverge limit.
Entry free alone gives more entries but no more exits.
Exit free and exit local have equal rates at both stations, so the road starts do not limit exits.
With a doubled fleet, in-comb and exit free gives 7.88 to 8.47 entries, less than with 327 pods, and this study did not measure why.

## More berths

These layouts come from `scenario -berths`.
Each is buildable, but the generator changes the station heading at some berth counts, so these arms have a layout difference other than berths.
Waterloo generates at 8 berths (heading 10 degrees) and 15 berths (heading -150 degrees), against a base heading of 20 degrees.
The generator refuses 7, and 9 to 14, with hard conflicts.
King's Cross generates at 7, 8, 10, and 15, with heading 140 degrees at 10 against 130 degrees at the base.

| Arm | Entries a minute | Exits a minute | Entry headway p50, s | Completed | Skipped | Tail mean, m | Free berth, % | Oracle |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| King's Cross, 8 berths | 7.47 / 7.15 / 7.38 | 6.65 / 6.58 / 6.65 | 2.4 / 2.7 / 2.4 | 390 / 365 / 387 | 428 / 430 / 433 | 411 / 449 / 354 | 100 / 100 / 100 | off |
| King's Cross, 10 berths | 7.42 / 7.53 / 7.38 | 6.42 / 6.47 / 6.47 | 2.3 / 2.5 / 2.3 | 383 / 380 / 387 | 443 / 435 / 443 | not rerun | not rerun | off |
| King's Cross, 15 berths | 7.50 / 7.45 / 7.40 | 6.55 / 6.50 / 6.45 | 2.3 / 2.7 / 2.3 | 391 / 390 / 387 | 439 / 440 / 458 | 237 / 267 / 254 | 100 / 100 / 100 | off |
| Waterloo, 8 berths | 6.90 / 6.70 / 6.67 | 5.95 / 6.10 / 6.38 | 11.7 / 12.0 / 11.8 | 347 / 348 / 348 | 457 / 446 / 450 | 652 / 735 / 680 | 100 / 100 / 100 | off |
| Waterloo, 15 berths | 6.85 / 7.50 / 7.42 | 4.80 / 4.75 / 4.80 | 2.7 / 2.8 / 2.5 | 337 / 350 / 354 | 512 / 495 / 492 | 530 / 480 / 440 | 100 / 100 / 100 | off |

The curve is flat from the start.
Even with both conflicts removed, 15 berths do not add entries at either station (King's Cross 10.28 to 11.90, Waterloo 10.82 to 11.28).
Exits at Waterloo with 15 berths are 4.75 to 4.80, and the changed heading may explain it, but this study did not separate the two.

## Platoons and tolerances on one bank

The table gives the safe rule arms against the baseline of about 7.4 entries a minute, a mean gap of about 8 s.

| Arm | Entries a minute | Exits a minute | Entry headway p50, s | Completed | Skipped | Tail mean, m | Free berth, % | Oracle |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| King's Cross, limit 8 | 7.22 / 7.12 / 7.08 | 6.60 / 6.62 / 6.62 | 3.4 / 3.7 / 4.1 | 378 / 374 / 382 | 426 / 411 / 417 | 640 / 791 / 549 | 100 / 100 / 100 | clean |
| King's Cross, any speed | 7.10 / 7.25 / 7.30 | 6.58 / 6.58 / 6.72 | 2.7 / 3.4 / 2.5 | 376 / 376 / 384 | 423 / 424 / 420 | 517 / 453 / 379 | 100 / 99 / 100 | clean |
| King's Cross, comb run | 7.00 / 7.30 / 7.17 | 6.53 / 6.65 / 6.47 | 2.2 / 2.2 / 2.2 | 374 / 384 / 379 | 423 / 420 / 423 | 446 / 449 / 405 | 98 / 98 / 100 | clean |
| King's Cross, pair | 7.42 / 7.35 / 7.42 | 6.95 / 7.22 / 7.28 | 2.7 / 3.1 / 2.6 | 393 / 388 / 390 | 417 / 401 / 415 | 492 / 543 / 340 | 100 / 100 / 100 | clean |
| King's Cross, cell 10 | 7.70 / 8.07 / 8.43 | 7.12 / 7.53 / 7.33 | 2.6 / 2.7 / 2.5 | 413 / 420 / 426 | 404 / 393 / 404 | 339 / 275 / 238 | 89 / 96 / 94 | clean |
| King's Cross, cell 10 and pair | 8.60 / 8.45 / 8.60 | 7.95 / 7.95 / 8.03 | 2.5 / 2.6 / 2.5 | 446 / 436 / 442 | 385 / 389 / 379 | 252 / 359 / 199 | 98 / 99 / 100 | clean |
| King's Cross, cell 10, pair, comb run, limit 8 | 8.70 / 8.68 / 9.03 | 8.03 / 7.72 / 8.15 | 2.1 / 2.6 / 2.3 | 449 / 435 / 464 | 383 / 380 / 376 | 308 / 398 / 148 | 91 / 88 / 92 | clean |
| Waterloo, limit 8 | 6.58 / 6.25 / 6.53 | 5.92 / 5.72 / 6.03 | 10.8 / 10.8 / 10.8 | 339 / 333 / 340 | 448 / 457 / 443 | 815 / 628 / 749 | 99 / 99 / 100 | clean |
| Waterloo, any speed | 6.50 / 6.28 / 6.70 | 5.83 / 5.60 / 6.03 | 10.8 / 10.8 / 10.8 | 338 / 323 / 345 | 451 / 464 / 455 | 717 / 652 / 678 | 100 / 99 / 100 | clean |
| Waterloo, pair | 7.05 / 6.80 / 6.83 | 6.80 / 6.92 / 6.88 | 10.7 / 10.8 / 10.8 | 360 / 356 / 358 | 428 / 429 / 422 | 709 / 607 / 700 | 100 / 100 / 100 | clean |
| Waterloo, cell 10 | 7.47 / 7.05 / 7.15 | 6.15 / 6.28 / 6.22 | 10.2 / 10.3 / 10.3 | 364 / 361 / 371 | 435 / 447 / 434 | 508 / 450 / 443 | 96 / 89 / 97 | clean |
| Waterloo, cell 10 and pair | 7.58 / 7.00 / 7.15 | 6.88 / 6.65 / 7.12 | 10.2 / 10.3 / 10.2 | 384 / 371 / 375 | 432 / 441 / 416 | 532 / 449 / 530 | 100 / 96 / 100 | clean |
| Waterloo, cell 10, pair, comb run, limit 8 | 7.42 / 7.12 / 7.25 | 7.15 / 6.58 / 7.28 | 10.2 / 10.3 / 10.3 | 383 / 374 / 372 | 413 / 425 / 409 | 605 / 443 / 558 | 99 / 96 / 100 | clean |
| Waterloo, turn 155 | 6.12 / 6.20 / 6.30 | 5.80 / 5.70 / 5.92 | 5.8 / 10.1 / 10.1 | 333 / 327 / 342 | 448 / 457 / 445 | 883 / 775 / 828 | 99 / 98 / 99 | clean |

A higher platoon limit buys nothing at either station.
Any speed and comb run also buy nothing.
Shorter cells and pair resources shorten the hold of the access lane and the junction zones.
Together they give 8.45 to 8.60 entries a minute at King's Cross and 7.00 to 7.58 at Waterloo.
All rule arms together give 8.68 to 9.03 and 7.12 to 7.42.
These arms are oracle clean, but they change the resource model, and the restore and proof checks did not run on them.

Turn 155 raises linked entries at Waterloo from 62 to 66% to 72 to 84%, and it lowers entries to 6.12 to 6.30.
The longer link clearance at the sharp turn may cost the gain, but this study did not measure that.
Corners at the Waterloo `road-in` lanes are at most 85 degrees in chord geometry, below the turn cap.
So these corners do not explain why links fail, and the unlinked share at Waterloo has no measured cause.
The median of 10.2 to 10.8 s is close to the solo hold of the busy Waterloo lanes (Z of 80 to 85 m, 9.2 to 9.6 s).
This agreement does not prove a cause.

Clearance 8 gives the numbers below, but it has no safety claim.

| Arm | Entries a minute | Exits a minute | Entry headway p50, s | Completed | Skipped | Tail mean, m | Free berth, % | Oracle |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| King's Cross, Clearance 8 | 7.65 / 7.60 / 7.65 | 7.45 / 7.58 / 7.35 | 2.3 / 2.7 / 2.4 | 408 / 397 / 407 | 395 / 391 / 400 | 384 / 383 / 333 | 100 / 100 / 100 | clean |
| Waterloo, Clearance 8 | 7.45 / 7.72 / 7.50 | 7.10 / 7.45 / 7.08 | 2.9 / 7.9 / 7.9 | 402 / 405 / 398 | 414 / 402 / 413 | 331 / 253 / 194 | 99 / 100 / 98 | clean |

The oracle uses `Clearance` as its threshold, so it checks these arms against 8 m, not 12 m.
A pass does not show that the 12 m separation holds.
A change to this value also changes the contract and the proof.
The arm with `Clearance` 6 did not run.

## Banks at King's Cross

The layouts are hand-built in the project format, with the stock rules.
Bank `a` keeps the first rows of the original comb.
Bank `b` has its own diverge at (-130, 140), merge at (-85, 130), and comb at x from -360 to -160, in the station frame.
`road-in` and `road-out` lanes 01, 02, 05, and 06 go to bank `b`, and lanes 03, 04, and 07 stay at bank `a`.
Bank `c` has a diverge at (60, 95), a merge at (115, 75), and a comb at x from 125 to 225, and it takes lanes 03.
Bank selection takes the nearest bank by free-flow time, so the split of lanes sets the split of pods.

The bank validator and `ValidateBankGeometry` refused no variant.
The generator's layout audit does not run on a hand-built project, so a crossing check ran instead.
The base project has 2 crossings between a station lane and a network link.
The two-bank layout crosses 5 links in 13 lane crossings, near the portals.
The three-bank layout crosses 7 links, and one is `c-access-out` with `link-194-ba-1`.
The validator accepts these crossings.
They rely on plane separation, as other crossings in the network do, so each one is a flyover that the layout needs.

| Arm | Entries a minute | Exits a minute | Entry headway p50, s | Completed | Skipped | Tail mean, m | Free berth, % | Oracle |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 2 banks, 3 + 2 berths | 10.03 / 9.50 / 9.45 | 9.60 / 9.25 / 9.32 | 2.3 / 2.5 / 3.5 | 510 / 493 / 497 | 336 / 337 / 349 | 155 / 219 / 112 | 97 / 97 / 96 | clean |
| 2 banks, 5 + 5 berths | 10.38 / 9.72 / 9.55 | 9.75 / 9.45 / 9.40 | 2.8 / 2.5 / 3.4 | 526 / 500 / 500 | 344 / 356 / 345 | 117 / 185 / 101 | 98 / 98 / 100 | clean |
| 3 banks, 5 + 5 + 2 berths | 10.93 / 11.55 / 10.00 | 11.15 / 11.78 / 10.35 | 3.4 / 3.6 / 4.7 | 556 / 585 / 521 | 302 / 282 / 314 | 56 / 50 / 46 | 100 / 99 / 100 | clean |
| 2 banks, 3 + 2 berths, platoons off | 6.53 / 6.58 / 6.50 | 6.62 / 6.60 / 6.53 | 9.5 / 9.3 / 9.6 | 352 / 350 / 351 | 426 / 422 / 418 | 911 / 845 / 762 | 100 / 100 / 100 | clean |
| 2 banks, 3 + 2 berths, cell 10 and pair (sim rule) | 10.47 / 10.70 / 10.05 | 10.30 / 9.95 / 10.15 | 2.8 / 2.2 / 3.1 | 537 / 534 / 522 | 308 / 307 / 317 | 107 / 134 / 88 | 96 / 87 / 97 | clean |
| 2 banks, 3 + 2 berths, doubled fleet | 9.90 / 8.55 / 9.85 | 9.15 / 7.70 / 8.93 | 2.3 / 2.3 / 2.5 | 507 / 454 / 509 | 89 / 125 / 60 | 373 / 671 / 279 | 70 / 68 / 73 | clean |
| 2 banks, 5 + 5 berths, doubled fleet | 10.65 / 10.47 / 10.85 | 9.12 / 8.72 / 9.20 | 2.1 / 2.2 / 2.2 | 527 / 516 / 533 | 109 / 79 / 69 | 280 / 316 / 305 | 76 / 79 / 80 | clean |
| 3 banks, 5 + 5 + 2 berths, doubled fleet | 14.65 / 15.18 / 15.05 | 13.62 / 13.82 / 13.65 | 2.0 / 2.4 / 2.3 | 701 / 720 / 700 | 29 / 0 / 8 | 175 / 140 / 168 | 50 / 59 / 55 | clean |

All of these are oracle clean.
Two banks with the same 5 berths give about 9.5 to 10 entries a minute, so the gain comes from the second path, not from berths.
Banks without platoons give 6.50 to 6.58, so platoons and banks add.
Three banks with 327 pods reach the fleet limit of the upper bounds, with a tail of about 50 m.
Three banks with 636 pods reach 14.65 to 15.18 entries and 13.62 to 13.82 exits.
The three busy `road-in` lanes (03, 04, and 05) carry about the same load, and in this arm each is on a different bank.
The free berth share falls to 50 to 59% in that arm, so the berths start to matter there.
The doubled fleet gives two banks no clear gain over 327 pods, and the exits fall to 7.70 to 9.20, and this study did not measure why.

A doubled fleet is not the preset.
It also adds traffic on the plexus, and the study did not measure the effect at other stations.

## Two lanes without banks

These layouts split the road lanes by the side of their portal, with a second diverge and a second merge.
The two entry lanes end at the same entry node, and the two exit lanes start at the same exit node.
They are buildable, and the project loads with the stock rules.

| Arm | Entries a minute | Exits a minute | Entry headway p50, s | Completed | Skipped | Tail mean, m | Free berth, % | Oracle |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| King's Cross, two lanes each way | 5.78 / 5.88 / 5.45 | 5.47 / 5.28 / 5.10 | 3.4 / 3.5 / 3.7 | 316 / 311 / 295 | 463 / 471 / 497 | 577 / 671 / 622 | 100 / 100 / 100 | clean |
| Waterloo, two lanes each way | 6.12 / 6.15 / 6.15 | 6.33 / 6.17 / 6.40 | 6.8 / 7.0 / 7.8 | 327 / 325 / 332 | 444 / 432 / 432 | 646 / 732 / 830 | 100 / 100 / 100 | clean |

Two lanes make King's Cross worse, with 5.45 to 5.88 entries.
Waterloo has no clear change in entries, and its exits move from 5.85 to 6.12 to 6.17 to 6.40.
This study did not measure the cause at either station.

## Best safe rate against the baseline

| Layout and rule | Label | Entries a minute | Mean gap, s |
| --- | --- | ---: | ---: |
| King's Cross baseline | Buildable | 7.10 to 7.47 | 8.0 to 8.5 |
| Cell 10 and pair, one bank | Sim rule | 8.45 to 8.60 | 7.0 to 7.1 |
| All rule arms, one bank | Sim rule | 8.68 to 9.03 | 6.6 to 6.9 |
| Two banks, 5 + 5 berths | Buildable | 9.55 to 10.38 | 5.8 to 6.3 |
| Two banks, 3 + 2 berths, cell 10 and pair | Sim rule | 10.05 to 10.70 | 5.6 to 6.0 |
| Three banks, 5 + 5 + 2 berths | Buildable | 10.00 to 11.55 | 5.2 to 6.0 |
| Three banks, 5 + 5 + 2 berths, 636 pods | Buildable | 14.65 to 15.18 | 4.0 to 4.1 |
| Waterloo baseline | Buildable | 6.25 to 6.85 | 8.8 to 9.6 |
| Waterloo, all rule arms | Sim rule | 7.12 to 7.42 | 8.1 to 8.4 |

"Buildable" here means that the project format and the validator accept the layout.
The bank layouts also need the flyovers above.

## Matched service

| Arm against its baseline | Accepted | Boarded | Completed | Jointly boarded | Wait, s | Over the wait limit | Jointly completed | Journey, s | Over the journey limit |
| --- | --- | --- | --- | ---: | --- | ---: | ---: | --- | ---: |
| King's Cross, 15 berths | 769 to 760 / 774 to 759 / 783 to 741 | 569 to 560 / 574 to 560 / 584 to 541 | 378 to 391 / 370 to 390 / 386 to 387 | 538 / 540 / 517 | 441 to 508 / 482 to 514 / 502 to 552 | 136 / 120 / 122 | 369 / 359 / 369 | 1317 to 1308 / 1329 to 1296 / 1329 to 1335 | 66 / 62 / 87 |
| King's Cross, exit free (upper bound) | 769 to 809 / 774 to 812 / 783 to 801 | 569 to 610 / 574 to 612 / 584 to 602 | 378 to 396 / 370 to 384 / 386 to 389 | 557 / 563 / 552 | 480 to 441 / 525 to 468 / 561 to 526 | 31 / 12 / 19 | 371 / 364 / 373 | 1310 to 1276 / 1334 to 1316 / 1316 to 1314 | 32 / 38 / 52 |
| King's Cross, both free (upper bound) | 769 to 935 / 774 to 962 / 783 to 910 | 569 to 735 / 574 to 763 / 584 to 710 | 378 to 579 / 370 to 611 / 386 to 545 | 562 / 568 / 565 | 490 to 360 / 531 to 369 / 572 to 460 | 11 / 6 / 24 | 378 / 370 / 386 | 1329 to 873 / 1351 to 876 / 1350 to 966 | 0 / 0 / 1 |
| King's Cross, pair | 769 to 782 / 774 to 798 / 783 to 784 | 569 to 584 / 574 to 598 / 584 to 586 | 378 to 393 / 370 to 388 / 386 to 390 | 556 / 561 / 548 | 479 to 463 / 518 to 495 / 555 to 546 | 40 / 28 / 35 | 375 / 368 / 378 | 1324 to 1292 / 1348 to 1325 / 1338 to 1336 | 18 / 25 / 39 |
| King's Cross, cell 10 and pair | 769 to 814 / 774 to 810 / 783 to 820 | 569 to 614 / 574 to 610 / 584 to 621 | 378 to 446 / 370 to 436 / 386 to 442 | 554 / 561 / 559 | 476 to 433 / 520 to 470 / 568 to 526 | 30 / 20 / 27 | 377 / 369 / 384 | 1327 to 1134 / 1351 to 1196 / 1348 to 1202 | 5 / 6 / 22 |
| King's Cross, all rule arms | 769 to 816 / 774 to 819 / 783 to 823 | 569 to 616 / 574 to 619 / 584 to 623 | 378 to 449 / 370 to 435 / 386 to 464 | 553 / 559 / 548 | 471 to 435 / 513 to 456 / 551 to 515 | 47 / 26 / 38 | 374 / 365 / 379 | 1323 to 1065 / 1338 to 1103 / 1343 to 1105 | 15 / 22 / 11 |
| King's Cross, 2 banks, 3 + 2 | 769 to 863 / 774 to 862 / 783 to 850 | 569 to 663 / 574 to 663 / 584 to 650 | 378 to 510 / 370 to 493 / 386 to 497 | 562 / 564 / 548 | 490 to 411 / 526 to 427 / 558 to 494 | 26 / 17 / 26 | 378 / 369 / 382 | 1329 to 971 / 1351 to 1016 / 1349 to 1024 | 5 / 1 / 5 |
| King's Cross, 2 banks, 5 + 5 | 769 to 855 / 774 to 843 / 783 to 854 | 569 to 657 / 574 to 643 / 584 to 654 | 378 to 526 / 370 to 500 / 386 to 500 | 561 / 561 / 559 | 488 to 414 / 520 to 439 / 569 to 500 | 28 / 29 / 39 | 378 / 368 / 383 | 1329 to 947 / 1345 to 996 / 1350 to 1025 | 1 / 2 / 4 |
| King's Cross, 3 banks | 769 to 897 / 774 to 917 / 783 to 885 | 569 to 699 / 574 to 717 / 584 to 686 | 378 to 556 / 370 to 585 / 386 to 521 | 562 / 567 / 559 | 491 to 381 / 531 to 403 / 565 to 477 | 16 / 15 / 35 | 378 / 370 / 384 | 1329 to 895 / 1351 to 902 / 1348 to 988 | 1 / 0 / 3 |
| King's Cross, 3 banks, both 636 pods | 1050 to 1170 / 1060 to 1199 / 1080 to 1191 | 850 to 970 / 860 to 1018 / 880 to 992 | 384 to 701 / 378 to 720 / 380 to 700 | 849 / 857 / 877 | 174 to 140 / 181 to 141 / 206 to 162 | 29 / 30 / 25 | 379 / 375 / 379 | 1186 to 725 / 1238 to 705 / 1177 to 719 | 12 / 3 / 3 |
| King's Cross, two lanes | 769 to 736 / 774 to 728 / 783 to 702 | 569 to 536 / 574 to 529 / 584 to 502 | 378 to 316 / 370 to 311 / 386 to 295 | 530 / 518 / 496 | 425 to 515 / 434 to 498 / 462 to 570 | 148 / 135 / 147 | 309 / 294 / 286 | 1166 to 1408 / 1150 to 1348 / 1144 to 1387 | 213 / 135 / 167 |
| Waterloo, exit free (upper bound) | 755 to 783 / 739 to 797 / 756 to 773 | 555 to 583 / 539 to 598 / 556 to 573 | 341 to 369 / 323 to 366 / 349 to 358 | 545 / 535 / 544 | 555 to 496 / 510 to 419 / 532 to 497 | 7 / 5 / 12 | 340 / 323 / 345 | 1347 to 1314 / 1326 to 1252 / 1376 to 1367 | 29 / 15 / 22 |
| Waterloo, both free (upper bound) | 755 to 928 / 739 to 967 / 756 to 915 | 555 to 729 / 539 to 771 / 556 to 716 | 341 to 571 / 323 to 610 / 349 to 563 | 550 / 537 / 552 | 562 to 386 / 514 to 313 / 543 to 373 | 2 / 5 / 7 | 341 / 323 / 349 | 1348 to 892 / 1326 to 857 / 1379 to 893 | 0 / 0 / 0 |
| Waterloo, pair | 755 to 771 / 739 to 770 / 756 to 777 | 555 to 571 / 539 to 570 / 556 to 577 | 341 to 360 / 323 to 356 / 349 to 358 | 543 / 535 / 544 | 551 to 508 / 512 to 477 / 531 to 495 | 4 / 14 / 13 | 339 / 321 / 344 | 1346 to 1303 / 1324 to 1266 / 1372 to 1347 | 2 / 2 / 11 |
| Waterloo, cell 10 and pair | 755 to 767 / 739 to 758 / 756 to 783 | 555 to 569 / 539 to 559 / 556 to 583 | 341 to 384 / 323 to 371 / 349 to 375 | 541 / 529 / 541 | 547 to 512 / 500 to 435 / 526 to 481 | 25 / 8 / 13 | 340 / 321 / 345 | 1346 to 1236 / 1324 to 1180 / 1376 to 1263 | 15 / 3 / 5 |
| Waterloo, all rule arms | 755 to 786 / 739 to 774 / 756 to 790 | 555 to 588 / 539 to 574 / 556 to 590 | 341 to 383 / 323 to 374 / 349 to 372 | 538 / 529 / 546 | 542 to 496 / 499 to 446 / 534 to 491 | 11 / 12 / 16 | 340 / 321 / 347 | 1347 to 1271 / 1324 to 1177 / 1376 to 1272 | 55 / 5 / 12 |
| Waterloo, turn 155 | 755 to 751 / 739 to 742 / 756 to 754 | 555 to 551 / 539 to 542 / 556 to 554 | 341 to 333 / 323 to 327 / 349 to 342 | 535 / 527 / 538 | 537 to 534 / 492 to 479 / 517 to 514 | 60 / 44 / 44 | 305 / 305 / 316 | 1260 to 1227 / 1285 to 1282 / 1312 to 1236 | 102 / 88 / 113 |
| Waterloo, two lanes | 755 to 755 / 739 to 767 / 756 to 767 | 555 to 555 / 539 to 568 / 556 to 568 | 341 to 327 / 323 to 325 / 349 to 332 | 535 / 535 / 544 | 537 to 512 / 511 to 458 / 530 to 516 | 21 / 10 / 44 | 321 / 315 / 329 | 1266 to 1343 / 1295 to 1370 / 1324 to 1355 | 137 / 158 / 133 |

The limits are the individual limits of the adoption gates: 30 s or 5% for a wait, and 60 s or 5% for a journey.
The bank arms still put tens of requests over the wait limit, so they do not pass the no-harm gate in this screen.
Requests that board or finish in one arm only are censored, and these comparisons do not count them.

| Arm | Wait mean, s | Wait p95, s | Journey mean, s | Journey p95, s | Stopped, 1000 pod-seconds | Late backlog change |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| King's Cross baseline | 641 / 645 / 650 | 1,815 / 1,893 / 1,790 | 1,329 / 1,351 / 1,350 | 2,515 / 2,626 / 2,580 | 219 / 243 / 163 | -17 / -11 / 12 |
| Cell 10 and pair | 605 / 614 / 620 | 1,773 / 1,760 / 1,699 | 1,271 / 1,330 / 1,295 | 2,311 / 2,467 / 2,437 | 148 / 173 / 107 | -26 / -29 / 6 |
| 2 banks, 3 + 2 | 569 / 576 / 596 | 1,644 / 1,657 / 1,717 | 1,139 / 1,198 / 1,132 | 2,096 / 2,254 / 2,152 | 79 / 104 / 47 | -21 / -3 / 6 |
| 2 banks, 5 + 5 | 575 / 591 / 594 | 1,687 / 1,710 / 1,708 | 1,125 / 1,178 / 1,137 | 2,065 / 2,176 / 2,136 | 71 / 107 / 51 | -31 / -24 / 8 |
| 3 banks | 545 / 539 / 572 | 1,599 / 1,561 / 1,619 | 1,067 / 1,089 / 1,100 | 2,003 / 2,065 / 2,102 | 26 / 21 / 13 | -13 / -23 / 18 |
| Baseline, 636 pods | 279 / 276 / 285 | 1,107 / 1,035 / 1,043 | 1,193 / 1,239 / 1,176 | 2,485 / 2,583 / 2,581 | 499 / 555 / 484 | 240 / 245 / 272 |
| 3 banks, 636 pods | 232 / 210 / 228 | 1,017 / 906 / 972 | 961 / 935 / 972 | 1,877 / 1,900 / 2,002 | 151 / 153 / 128 | 142 / 168 / 151 |

## Earlier records

The table compares this study with each earlier record and gives each difference in settings.
This study ran on the preset project, with depots, at 30 Hz, through `compare`, with the tip of the tree and seeds 1 to 3.

| Record | Then | Now | Differences | Result |
| --- | --- | --- | --- | --- |
| [Buffers off, King's Cross](buffer-late-claim.md#station-entry-throughput) | 4.48 entries a minute, headway p50 / p90 of 11.0 / 17.1 s | 7.40 / 7.40 / 7.25, p50 of 2.9 / 3.4 / 2.4 s, p90 of 19.1 / 19.5 / 21.1 s | See the next row. The record also predates the three platoon entry changes. | Superseded. The next row replaces it. |
| [After the platoon entry changes](buffer-late-claim.md#station-entry-throughput) | King's Cross 6.61 and 6.69, Waterloo 7.11 and 6.73, with 174 to 179 completed | King's Cross 7.40 / 7.40 / 7.25, 186 / 179 / 186 completed. Waterloo 6.75 / 7.20 / 6.75, 171 / 167 / 171 completed | The record used the user's project, a scratch harness, and two seed pairs. Both use 60 orders a minute, 30 minutes, the first 10 excluded, limit 4, and reassignment on. | King's Cross is slightly higher. Waterloo is in range. |
| Free berth in every sample | Every sample | 99.4 to 100% | As above | Reproduces, with small exceptions. |
| [`8bbae7a` trace](platoon-followup.md), entries in 60 minutes | King's Cross 368 to 390, Waterloo 309 to 321 | King's Cross 381 / 391 / 368, Waterloo 321 / 315 / 326 | The tip against `8bbae7a`, 30 Hz, and the standee code. Both use 20 minutes of arrivals and 60 minutes in total. | Reproduces, with Waterloo slightly higher. |
| Same trace, exits | King's Cross 361 to 374, Waterloo 283 to 293 | King's Cross 375 / 360 / 362, Waterloo 288 / 290 / 291 | As above | Reproduces. |
| Same trace, `road-out-01` share at Waterloo | 51% | 53 / 46 / 49% | As above | Reproduces. In the 60-minute flood, `road-out-02` leads instead. |
| Same trace, Waterloo limits in order | Exit first, approach turn second | Exit first holds. A turn cap of 155 degrees gives no gain. | The record used 180 degrees and 20 minutes of arrivals. This study did not run 180 degrees, because the link clearance grows without limit there. | Exit first and approach second reproduce: in-road raises Waterloo entries to 7.72 to 8.50. The turn cap as the means does not reproduce at 155 degrees. |
| Same trace, exit arm at King's Cross | No change | Exits 7.35 to 7.55 against 6.50 to 6.70, entries 7.30 to 7.40 against 7.10 to 7.47 | 60 minutes of arrivals, not 20, and a sampling window. | Differs: exits rise in the flood. |
| [Tick-rate contract](../contracts/tick-rate-contract-proposal.md) `ksxv` | 138 / 167 / 145 completed | 173 / 166 / 169 | The contract ran at an earlier tree. This arm has reassignment off and 30 minutes on the tip. | Does not reproduce. |

## Station buffers

The arm that restores station buffers did not run.
The commit `932eb6a2` removed 4,016 lines in 73 files, so a restore is not small.
The arms above also show that the limit is not the occupancy of the access lane alone.
The splits show the junction zones at the merge, the comb, and the diverge.
The [buffer record](buffer-late-claim.md#station-entry-throughput) measured lower rates with buffers on.

## What each change buys

| Change | Result at King's Cross | Result at Waterloo |
| --- | --- | --- |
| More berths | Nothing | Nothing |
| Higher platoon limit | Nothing | Nothing |
| Two lanes without banks | Worse | No clear change |
| Shorter cells and pair resources | 1.13 to 1.50 entries a minute | 0.30 to 1.05 entries a minute |
| Two banks | 1.98 to 3.26 entries a minute | Not built |
| Three banks | Up to the fleet limit, and 15 a minute with 636 pods | Not built |
| Exit free (upper bound) | About 0.8 exits a minute | About 1.1 exits a minute |

Of these, banks are the first choice if King's Cross must carry more than about 7 entries a minute.
Shorter cells and pair resources are a second choice.
They change the resource model, and at 10 m on all lanes they give 155,717 blocks against a limit of 64,000.
More berths, a higher platoon limit, and two lanes without banks are not worth a change.
Waterloo needs its own bank layout and run before the same choice applies there.

## Not verified

- The screen has three seeds, one flood pattern, and one queue limit, with no demand band, mixed arrivals, or rail-hub arm.
- No bank layout exists for Waterloo, so bank results apply to King's Cross only.
- The flyovers of the bank layouts have no cost or fit check against the map, and the layout audit of the generator does not cover them.
- The bank arms cover a 60-minute run, with no 120-minute sustained check.
- Service at other stations was not compared, and a doubled fleet changes it.
- The rule arms have no restore, stream, or bound checks, and no contract covers them.
- No arm reduced the restart from standstill at the diverge, and the coast-in of the tree is the only such behavior that ran.
- The cause of the low linked share at Waterloo is open.
- The causes of the two-lane results, and of the lower doubled-fleet rates with two banks, are open.
- The tails and the free-berth shares marked "not rerun" came from an earlier probe, so the tables omit them.
- The crossing check reads lane geometry only, and it does not replace the layout audit.
