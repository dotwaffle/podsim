# Platoon qualification follow-up

This follow-up adds the missing London seeds 4 through 10 and the rail-hub checks.
It also repeats London seeds 1 through 3 on the current simulation source and checks default-London AM demand.
The measured source is `c804c000282937a9e432d728ff24f126fee595b1`.
The study contains 162 arms: 140 London198, ten rail-hub, and twelve London114 controls.
Each demand schedule runs with platooning off and with virtual platoons limited to four pods.
Sharing and redistribution remain off, and routing remains free-flow.
The study does not change presets or operating defaults.

One pair fails the one-hour recovery comparison.
Three of eleven designated control pairs fail at least one no-harm threshold.
The expanded study therefore fails the recovery and no-harm adoption rules.
These failures do not change operating defaults.

## London198 results

LondonCentral has two pods per passenger station and two per Parking facility, for 198 pods.
The seven historical band/rate pairs use seeds 1 through 10.
Each arm has 30 minutes of arrivals and at most 65 simulated minutes in total.
It stops when all requests finish after the arrival window closes.
The historical recovery comparison uses a separate 60-minute threshold.

| Band | Requests/minute | Off recovered by 60 minutes | Virtual recovered by 60 minutes | Mean wait, off / virtual | Mean journey, off / virtual | Virtual coupled time |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| early | 9 | 10/10 | 10/10 | 291.4 / 284.9 s | 801.0 / 762.3 s | 9.49% |
| early | 10 | 10/10 | 10/10 | 338.8 / 324.1 s | 871.7 / 805.4 s | 12.73% |
| early | 11 | 7/10 | 10/10 | 395.7 / 368.0 s | 949.8 / 849.6 s | 15.16% |
| early | 12 | 1/10 | 9/10 | 456.0 / 413.6 s | 1032.8 / 896.4 s | 18.07% |
| am-peak | 20 | 10/10 | 10/10 | 153.6 / 154.2 s | 555.1 / 555.7 s | 0.78% |
| am-peak | 24 | 7/10 | 6/10 | 304.4 / 302.9 s | 706.8 / 704.8 s | 1.40% |
| pm-peak | 24 | 10/10 | 10/10 | 189.9 / 189.5 s | 565.5 / 564.9 s | 1.18% |

Across all ten seeds, the highest tested Early rate with one-hour recovery rises from 10/minute off to 11/minute virtual.
Virtual recovers only nine of ten seeds at 12/minute.
These finite-window results do not establish sustained capacity or recovery between tested rates.
Every Early virtual arm must have nonzero coupling activity, and every off arm must have zero coupled time.

## Recovery regressions

A regression occurs when the off arm finishes by 3,600 seconds and its virtual counterpart does not.

| Project | Band | Interval | Seed | Off end | Virtual end | Virtual unfinished |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| london198.json | am-peak | 2.5s | 2 | 3547 s | 3696 s | 0 |

AM peak at 24 requests/minute, seed 2, changes the historical recovery comparison.
The current off arm finishes at 3,547 seconds, compared with 3,681 seconds in the historical measurement.
The virtual arm remains at 3,696 seconds and matches every common historical measurement field.
The improved off baseline now crosses the one-hour threshold. The virtual arm does not.
This pair fails the recovery rule on the current source.

## Rail-hub and default-London controls

Rail-hub uses seeds 1 through 5, hub-burst demand, a five-second request interval, and a burst size of twelve.
It has five minutes of arrivals and a 30-minute total cap.
Each rail-hub arm schedules 59 requests. The burst size groups these requests and does not multiply their number.
Default LondonCentral has 114 pods and uses AM peak rates of 12 and 13 requests/minute with seeds 1 through 3.
These London controls use the same 30-minute arrival window and 65-minute total cap as the main matrix.

Each paired control must serve at least as many requests with virtual platoons.
Mean pickup wait may increase by at most two seconds, and empty travel may increase by at most 2%.

| Project | Band | Interval | Seed | Wait change | Empty-distance ratio | No-harm pass |
| --- | --- | ---: | ---: | ---: | ---: | --- |
| london114.json | am-peak | 4.615385s | 1 | -3.97 s | 0.9841 | yes |
| london114.json | am-peak | 4.615385s | 2 | +2.31 s | 1.0261 | no |
| london114.json | am-peak | 4.615385s | 3 | +0.53 s | 0.9960 | yes |
| london114.json | am-peak | 5s | 1 | +4.29 s | 1.0074 | no |
| london114.json | am-peak | 5s | 2 | +0.93 s | 1.0529 | no |
| london114.json | am-peak | 5s | 3 | -1.00 s | 1.0068 | yes |
| rail-hub.json | hub-burst | 5s | 1 | +0.52 s | 1.0005 | yes |
| rail-hub.json | hub-burst | 5s | 2 | +0.43 s | 0.9990 | yes |
| rail-hub.json | hub-burst | 5s | 3 | +0.13 s | 1.0002 | yes |
| rail-hub.json | hub-burst | 5s | 4 | +0.21 s | 1.0000 | yes |
| rail-hub.json | hub-burst | 5s | 5 | -0.55 s | 1.0040 | yes |

All five rail-hub pairs pass these thresholds with nonzero virtual coupling activity.
Three default-London pairs fail: AM12 seed 1 on wait, AM12 seed 2 on empty travel, and AM13 seed 2 on both.
The CSV records coupling activity for each arm, and the metadata records all paired results.

## Validation and limits

All 162 arms have confirmed successful process exits.
The instrumented runs completed 31,184,760 per-tick observations.
Each tick checks finite state, separation, and berth ownership.
The platoon monitor checks certificates, braking limits, acceleration of coupled pods, predecessor ownership, and overtaking.
The full retention scan also checks incremental resource ownership on each tick.
Request conservation runs each simulated second, followed by a final unique-ID timing census.

All 42 repeated historical arms match their original schedule IDs, request counts, seeds, and demand bands.
Every off/virtual pair uses the same schedule.
41 of the 42 repeated controls match every common historical measurement field exactly.
The metadata records the comparison for each repeated control.
Four controls compare every instrumented result field against the production comparison function.
The first two pilot results are reused only with matching job, project, and binary hashes.

This follow-up does not repeat the historical pre-platoon snapshot equivalence test or the uninstrumented CPU comparison.
It therefore does not claim a new pass for all seven original adoption rules.
Journey statistics include completed requests only. The dataset retains unfinished requests and failed recovery outcomes.

[Per-arm measurements](measurements/platoon-followup.csv) and [metadata](measurements/platoon-followup.json) retain the full results.
The [historical A/B](qualification.md#platoon-ab) remains a separate measurement record.
Local evidence: `~/.cache/agents/podsim/platoon-followup-20260929/`.
It includes generated projects, hashes, helper overlays, commands, logs, manifests, and exit markers.
