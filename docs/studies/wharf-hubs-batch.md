# Wharf hubs batch

This study compares `london-full-shared-express-wharf-hubs` (`H`) with the same network and no Express (`H0`), with `london-full-shared-express-wharf` (`W`), and with `W` and no Express (`W0`), on 2026-11-25.
The rules are in [section 13.7 of the Express London hubs design](../design/london-hubs-d8-d9.md#137-study-rules), which was committed before any run.
It qualifies no preset.

## Result

The third check of the rules is not met.

| Check | Result |
| --- | --- |
| No safety, contract, or accounting failure | Met, 12 of 12 runs |
| Each arm finishes each accepted order | Met, 12 of 12 runs |
| On each line, the mean hub wait of `H` is at most that of `H0` | Not met in any seed |

On the third check, `H` has the longer mean hub wait on 4 of 4 lines in seed 1, 1 of 4 in seed 2 (London Bridge and Canary Wharf), and 4 of 4 in seed 3.

- The third check is not a comparison of equal parties.
  `H0`, `W`, and `W0` reach the queue limit of 200 and refuse 578 to 745 events in each seed, and their waits cover only the parties that boarded whole.
  `H0` carries 1,771 to 1,924 hub passengers, and `H` carries 7,172 to 7,363 of the 7,341 to 8,141 offered.
- The hub waits of `H` are long on each line: 1,406 to 2,021 s.
  The 32 Express pods serve 61 to 68 hub passengers a minute on four lines, so the fleet is a limit (inferred, not measured).
- On each other measure, `H` is better than each other arm, in each seed.
  It refuses 50 to 161 events, against 578 to 745.
  Its background mean wait is 191 to 212 s, against 676 to 879 s.
  Compact pods are blocked by Compact pods for 8,181 to 11,108 pod-seconds in the window, against 772,514 to 901,460.
  So the other arms are near gridlock on this workload, and `H` is not.

## Method

The pilot was `H`, seed 1: 115 s of wall time with no failure.
The batch is 12 runs, with 3 workers, nice 10, and a memory cap.
Each run took 117 to 208 s of wall time.

```sh
go run ./cmd/compare screen -configs W0,W,H0,H -bands am-peak -seeds 1,2,3 -hub-mean 120s -workers 3 -output batch.json
```

The source is `c0bfd968`, with the arms `H` and `H0` and the two new hub streams added to a scratch copy of `cmd/compare`.
The command exits with 4, because the screen has no pair of `D`.

The workload is that of section 12: 2 hours of arrivals, 10 background requests a minute, a queue limit of 200, and at most 2 hours of clear.
The Waterloo and Bank stream has a mean of 120 s, and the London Bridge and Canary Wharf stream has the rates of HX-D3.
Waterloo and London Bridge has one party each 62 s, 76 in 100 from Waterloo.
Waterloo and Canary Wharf has one party each 24.6 s, 91 in 100 from Waterloo.
Each arm gets the same events, and a party on a line that an arm does not serve splits into Compact orders.
The safety oracle runs at each simulated second, and the D6 oracle does not run.

## Service

Each cell gives seeds 1, 2, and 3.
"Wait" is the passenger-weighted mean wait of the hub parties that boarded whole, in seconds.
"Carried" is the hub passengers that finished, of the offered passengers of the line.

| Measure | `W0` | `W` | `H0` | `H` |
| --- | --- | --- | --- | --- |
| Wait, Waterloo and Bank | 1,461, 1,609, 1,327 | 895, 881, 993 | 1,565, 1,587, 1,318 | 1,736, 1,406, 1,538 |
| Wait, London Bridge and Canary Wharf | 426, 538, 525 | 206, 274, 203 | 509, 625, 524 | 1,518, 1,551, 1,882 |
| Wait, Waterloo and London Bridge | 1,438, 1,408, 1,433 | 2,042, 1,683, 2,009 | 1,435, 1,598, 1,353 | 1,635, 1,444, 1,701 |
| Wait, Waterloo and Canary Wharf | 1,580, 1,697, 1,762 | 2,011, 2,027, 2,282 | 1,629, 1,700, 1,770 | 1,818, 1,572, 2,021 |
| Carried, Waterloo and Bank, of 632, 550, 553 | 135, 174, 161 | 397, 369, 367 | 134, 194, 164 | 597, 529, 450 |
| Carried, London Bridge and Canary Wharf, of 2,625, 2,699, 3,021 | 655, 550, 677 | 1,804, 1,827, 2,081 | 676, 608, 636 | 2,440, 2,614, 2,756 |
| Carried, Waterloo and London Bridge, of 1,304, 1,184, 1,264 | 358, 287, 354 | 414, 316, 378 | 313, 267, 363 | 1,240, 1,170, 1,219 |
| Carried, Waterloo and Canary Wharf, of 3,182, 2,908, 3,303 | 740, 829, 738 | 749, 961, 853 | 801, 702, 725 | 2,957, 2,859, 2,938 |
| Refused events | 687, 703, 725 | 605, 578, 616 | 679, 745, 730 | 127, 50, 161 |
| Background, mean wait | 879, 831, 826 | 771, 726, 676 | 837, 878, 830 | 212, 200, 191 |
| Background, p95 wait | 2,195, 2,026, 2,247 | 2,242, 2,086, 2,521 | 2,178, 2,189, 2,195 | 860, 728, 771 |
| Background requests that boarded | 980, 961, 976 | 956, 962, 960 | 987, 926, 970 | 1,120, 1,168, 1,119 |
| Queue peak | 200, 200, 200 | 200, 200, 200 | 200, 200, 200 | 200, 200, 200 |
| Empty distance in the window (km) | 9,563, 9,040, 9,505 | 8,339, 8,371, 7,736 | 9,728, 9,052, 9,485 | 5,859, 5,683, 5,357 |
| Compact pod-seconds blocked by Express, window | 0, 0, 0 | 65,299, 66,873, 69,505 | 0, 0, 0 | 3,830, 4,197, 4,988 |
| Compact pod-seconds blocked by Compact, window | 803,935, 822,562, 780,620 | 901,460, 860,590, 840,253 | 772,514, 859,029, 787,648 | 8,181, 11,108, 11,074 |

## Matched background waits

Each cell gives seeds 1, 2, and 3, over the background requests that boarded in both arms.

| Pair | Mean change (s) | Longer by more than the limit | Shorter by more than the limit |
| --- | --- | --- | --- |
| `H` and `H0` | -627, -679, -637 | 15, 12, 17 | 837, 815, 792 |
| `H` and `W` | -555, -510, -490 | 27, 23, 38 | 755, 785, 713 |
| `W` and `W0` | -106, -110, -127 | 238, 210, 205 | 455, 425, 441 |

## Limits

- At the AM peak rates of four lines, each arm reaches the queue limit, `H` included.
  So the batch does not show the effect of the Express pods below saturation.
- The hub waits of `H` and of the other arms cover different parties, so the third check compares the waits of different sets of passengers.
- The fleet of `H` is the 32 Express pods of `W` (HB-D21 A).
  A larger Express fleet is not measured.
- The batch does not measure the queue of link 100 toward Waterloo, which failed HX11 on `W`.

## Fleet study

On 2026-11-26 the maintainer granted a study of the fleet of `H` (HB-D21 B): combs of 16, 24, and 32 berths and pods, so 32, 48, and 64 Express pods, on the workload of this batch.
The four Express hubs have 15 berths together, which Compact pods also use, so a larger Express fleet needs larger combs.
The study stopped before its pilot, as its brief required, because the larger combs fail the layout audit.
With 24 or 32 rows, the comb beside Waterloo (`express-parking`) crosses the Blackfriars guideway: `xp-20-out` crosses `940GZZLUBKF-move-02-01`, with 49 and 50 hard conflicts.
The comb of 16 rows passes the audit, and the audit names row 20 first, so the comb cannot grow to 24 rows at its site.
The study grew both combs, so it did not test the comb beside London Bridge alone.
The comb survey below does.

## Comb survey

On 2026-11-26 the maintainer granted a static survey of comb sites for a larger fleet of `H`.
It ran in a scratch copy, with no simulation run.
Each check is the layout audit with no exemption for the new comb (HB-D12 B), project validation, the bank audit, and the parking gaps, on `H` with the Express pods of the combs.

The rows that each comb of `H` can have at its site:

| Comb | Site | 16 rows | 17 rows | 18 rows | 32 rows | 48 rows |
| --- | --- | --- | --- | --- | --- | --- |
| `express-parking`, beside Waterloo | P4 | Pass | Pass | Fail | Fail | Fail |
| `express-parking-lnb`, beside London Bridge | LB3 | Pass | Pass | Pass | Pass | Pass |

- At 18 rows, the Waterloo comb is nearer than 36 m to `940GZZLUBKF-access-in`, and at 19 rows `940GZZLUBKF-road-in-01` crosses it.
- The London Bridge comb passes each check with 32 and 48 rows, with 67 shape pairs and no parking gap.
  So a fleet of 48 or 64 Express pods needs no new site, if the extra pods park beside London Bridge.

A screen of sites for a comb of 32 rows in place of the Waterloo comb used the portals of links 100 and 350 at Waterloo.
It took the 4,000 sites with the shortest roads at each portal, from a grid of centers 100 to 800 m from the portal, headings in steps of 10 degrees, and both sides.

| Portal | Sites that pass | Best site: center, heading | Roads (m) | Comb to Waterloo, and back (m) | Shape pairs |
| --- | ---: | --- | ---: | --- | ---: |
| `london-link-100-b` | 0 | | | | |
| `london-link-350-b` | 6 | (1033.2, 514.7), 0 degrees, not mirrored | 204 | 362 and 456 | 62 |

- At link 100, 3 sites pass the layout audit, and the bank audit refuses each one, because a road of the comb is nearer than 20 m to a Waterloo move or to link 100.
- At link 350, the other passing sites are near (1047.9, 494.4), (1046.4, 536.8), (1062.9, 517.6), and (1075.2, 495.5), with roads of 205 to 254 m.
- From the best site, the comb is 362 m from the first berth of Waterloo and 456 m back, by chord.
  The Waterloo comb at P4 is 654 m from it (section 13.2 of the design).
- The best site with 32 rows, together with the London Bridge comb of 16 or 32 rows, passes each check: 48 or 64 Express pods, and the two combs have Express routes between them.
- Link 350 carries the new route between London Bridge and Waterloo, so the roads of the comb join that route.
  The effect of that on the service is not measured.

### Decisions for the maintainer

| ID | Question | Options | Recommendation |
| --- | --- | --- | --- |
| FS-D1 | Where a larger fleet of `H` parks | A: the London Bridge comb grows to 32 or 48 rows, and the Waterloo comb stays. B: a comb of 32 rows on link 350 replaces the Waterloo comb, with the London Bridge comb of 16 or 32 rows. C: no larger fleet. | Screen A and B, because three of the four lines start at Waterloo in the AM peak, and A parks the extra pods 2 km from it. |
| FS-D2 | The next step | A: a fleet screen of `H` with the workload of this batch: 32 pods as now, 48 and 64 pods under FS-D1 A, and 48 and 64 pods under FS-D1 B, seeds 1 to 3, 15 runs, in a scratch copy. B: no screen. | A. It measures if the fleet is the limit, which this batch only infers. |

## Fleet screen

On 2026-11-26 the maintainer chose to screen both layouts of FS-D1 (FS-D2 A).
The screen ran `H` with the workload of this batch in a scratch copy, seeds 1 to 3, with five fleets:

| Arm | Waterloo side comb | London Bridge comb | Express pods |
| --- | --- | --- | ---: |
| `H32` | P4, 16 rows | LB3, 16 rows | 32 |
| `A48` | P4, 16 rows | LB3, 32 rows | 48 |
| `A64` | P4, 16 rows | LB3, 48 rows | 64 |
| `B48` | Link 350 site, 32 rows | LB3, 16 rows | 48 |
| `B64` | Link 350 site, 32 rows | LB3, 32 rows | 64 |

`H32` is `H` as built, and its runs repeat those of the batch above.
The pilot was `B64`, seed 1: 112 s of wall time with no failure.
Each run had no safety, contract, or accounting failure, and finished each accepted order.

Each cell gives seeds 1, 2, and 3.
"Wait" is the passenger-weighted mean wait of the hub parties that boarded whole, in seconds.

| Measure | `H32` | `A48` | `A64` | `B48` | `B64` |
| --- | --- | --- | --- | --- | --- |
| Wait, Waterloo and Bank | 1,736, 1,406, 1,538 | 1,490, 1,147, 1,261 | 1,589, 1,282, 1,303 | 1,625, 971, 1,213 | 1,630, 1,066, 1,460 |
| Wait, London Bridge and Canary Wharf | 1,518, 1,551, 1,882 | 1,104, 1,010, 1,129 | 909, 1,013, 953 | 1,167, 862, 1,115 | 872, 716, 1,086 |
| Wait, Waterloo and London Bridge | 1,635, 1,444, 1,701 | 1,389, 1,101, 1,159 | 1,424, 1,378, 1,288 | 1,485, 935, 1,162 | 1,380, 1,110, 1,418 |
| Wait, Waterloo and Canary Wharf | 1,818, 1,572, 2,021 | 1,567, 1,290, 1,411 | 1,634, 1,563, 1,549 | 1,699, 1,061, 1,391 | 1,633, 1,185, 1,747 |
| Refused events | 127, 50, 161 | 48, 0, 26 | 10, 13, 13 | 68, 0, 24 | 15, 0, 57 |
| Background, mean wait | 212, 200, 191 | 219, 213, 202 | 220, 213, 205 | 214, 211, 202 | 226, 214, 204 |
| Queue peak | 200, 200, 200 | 200, 193, 200 | 200, 200, 200 | 200, 172, 200 | 200, 168, 200 |
| Empty distance in the window (km) | 5,859, 5,683, 5,357 | 5,891, 5,944, 5,797 | 6,075, 5,878, 5,796 | 5,926, 5,998, 5,752 | 6,092, 6,046, 5,617 |
| Compact pod-seconds blocked by Express, window | 3,830, 4,197, 4,988 | 17,876, 16,394, 12,770 | 28,383, 26,173, 21,241 | 16,644, 11,329, 12,411 | 30,617, 23,711, 28,338 |

- 16 more Express pods cut the hub waits by 6 to 44 percent on each line, and the larger fleets refuse fewer events and carry 96 to 100 percent of the hub passengers.
- 32 more pods do not cut the waits much more than 16 more.
  On the three Waterloo lines, `A64` is longer than `A48` in most cells, and `B64` is not consistently shorter than `B48`; London Bridge and Canary Wharf gains in most seeds.
- Hub waits stay 12 to 29 minutes in the fleets of 48 and 64.
  So the Express fleet is a limit only up to about 48 pods, and another limit holds the Waterloo lines (inferred, not measured: the [Waterloo junctions](waterloo-junctions.md) study found that the arrival side of Waterloo holds the Express heads).
- `B48` has shorter waits than `A48` on each line in seed 2 and on three lines of four in seed 3, and longer waits on each line in seed 1, so the screen does not choose between the layouts.
- More Express pods block Compact pods for longer, 2.5 to 8 times the pod-seconds of `H32`, and the background waits change by less than 20 s.

### Decisions for the maintainer

| ID | Question | Options | Recommendation |
| --- | --- | --- | --- |
| FS-D3 | The fleet of `H` | A: keep 32 pods. B: build `A48`, which only adds rows to the London Bridge comb. C: build `B48`, which moves the Waterloo comb to link 350. | A. A larger fleet helps, but the hub waits stay over 12 minutes, and the next limit is at Waterloo, which a larger fleet does not change. |
