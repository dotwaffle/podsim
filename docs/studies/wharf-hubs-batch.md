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
