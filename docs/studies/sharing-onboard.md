# Onboard pickups with drop-off sharing

This study asks whether onboard pickup helps drop-off sharing on LondonFull.
It changes no default, preset, contract, simulation code, or layout.

The study stopped early on request, so its matrix is partial.
See [what did not run](#what-did-not-run).

## Result

Onboard pickup does not help at the rates where the service is at supply.
At AM 10 and 15/min and uniform 10/min, no occupied pickup occurs, and the on and off arms give identical per-offer records.

Above supply, it gives a small mean gain and many individual regressions.
At AM 20/min, each seed has 118 to 139 occupied pickups.
Mean wait falls by 2.23% to 4.11%, and mean journey falls by 0.98% to 2.29%.
From 685 to 915 matched requests exceed the individual wait limit, and from 603 to 831 exceed the individual journey limit.
At uniform 15/min, the single pair shows no mean gain: wait falls by 0.77%, and journey rises by 0.51%.
Uniform 20/min has no finished on arm.

Sharing itself has a much larger effect than onboard pickup, and its gain shows only above supply in these runs.
At AM 20/min, drop-off sharing lowers mean wait by 43.0% to 45.1%, and late backlog growth falls from about +6.4 to about +2.5 orders/min.
At uniform 10/min, sharing raises mean wait by 10.88% in seed 2 and by 5.14% in seed 3.

A 3-seed screen does not meet the [adoption gates](../design/experimental-adoption.md).
The recommendation is against any wider use of onboard pickup on this evidence.

## Method

The source is `4bffb6f7`.
The project is `scenario -preset london-full`: 327 Compact pods, 734 passenger berths, and 36 parking berths.

Compare has `-pattern profile` and `-pattern balanced`, and both accept any rate through `-loads`.
The preset demand is the AM peak band at 10/min.
Thus the AM arm runs at all three rates, beside a uniform arm at the same rates.
`balanced` is the uniform demand.

All arms use these settings:

- Consent `-sharing-consent shared`, positioning off, and queue limit 1,000,000.
- Virtual platoons, because the preset sets a platoon limit of 4 and compare ignores it without the flag.
- Seeds 1, 2, and 3, with one schedule ID for all arms of a schedule and seed.

| Arm | Flags |
| --- | --- |
| No sharing | `-sharing-limits 1` |
| Drop-offs, onboard off | `-sharing-limits 4 -sharing-modes drop-offs -onboard-pickups off` |
| Drop-offs, onboard on | `-sharing-limits 4 -sharing-modes drop-offs -onboard-pickups on` |

The party limit of 4 is the seat count of a Compact pod and the limit of the old records.
The stop limit is the default of 3.

The project comes from `scenario -preset london-full -output london-full.json`.
Each arm has this command, with one seed and one arm in each process:

```sh
compare -project london-full.json -pattern profile -bands am-peak -loads 6s -seeds 1 -duration 7h -arrivals-for 6h -sharing-consent shared -sharing-limits 4 -sharing-modes drop-offs -onboard-pickups on -redistribution-policies off -platoon-policies virtual -queue-limit 1000000 -workers 1 -format csv -request-ledger ledger -output arm.csv
```

The other arms change `-pattern`, `-bands`, `-loads` (6s, 4s, or 3s), `-seeds`, and the sharing flags in the table.
A 3 h arm uses `-duration 4h -arrivals-for 3h`.
Late growth covers the second half of the arrival window: hours 3 to 6 of a 6 h arm, and hours 1.5 to 3 of a 3 h arm.

### Matched requests

The compare report has only arm aggregates, so it cannot give a per-request match.
The local branch `study/sharing-onboard` has three scratch commits for this study, and they do not land:

| Commit | Change |
| --- | --- |
| `c45f5c93` | `-request-ledger` writes one row per offer: status, boarding tick, and completion tick. |
| `afc05e32` | `cmd/onboardbridge` repeats the old Example-network screens. |
| `341b7a89` | Counters at the occupied pickup gates. |

The 6 h arms use `c45f5c93`, and the 3 h arms use `341b7a89`.
The patches do not change the reports.
A 30-minute run of each patched binary gives the same CSV bytes as the binary at `4bffb6f7`.
Two 7 h arms with the counters give the same CSV bytes as the same arms without them.

A match uses the offer index.
Waits compare requests that boarded in both, and journeys compare requests that completed in both.
The other requests are censored, and the column of completions in only one arm counts them.
An occupied pickup in the tables is a joined party whose origin differs from the origin of its host.
This count equals the gate counter in each arm that has both.
The study does not use the `occupancy` column, because its definition assumes that all parties of a pod journey board at one berth.

## Demand against supply

Each cell gives seeds 1 / 2 / 3, and `-` means that the arm did not run.
No arm skips an offer, because the queue limit is 1,000,000.

| Schedule | Arm | Late growth (orders/min) | Late rate (orders/min) | Outstanding at cap | Drained |
| --- | --- | ---: | ---: | ---: | ---: |
| AM, 10/min, 6 h | No sharing | -0.05 / +0.01 / +0.00 | 10.05 / 9.99 / 10.00 | 2 / 0 / 0 | no / yes / yes |
| AM, 10/min, 6 h | Drop-offs, onboard off | -0.09 / +0.02 / +0.03 | 10.09 / 9.98 / 9.97 | 2 / 0 / 0 | no / yes / yes |
| AM, 10/min, 6 h | Drop-offs, onboard on | -0.09 / +0.02 / +0.03 | 10.09 / 9.98 / 9.97 | 2 / 0 / 0 | no / yes / yes |
| AM, 15/min, 6 h | No sharing | -0.10 / - / - | 15.10 / - / - | 1 / - / - | no / - / - |
| AM, 15/min, 6 h | Drop-offs, onboard on | -0.06 / - / - | 15.06 / - / - | 1 / - / - | no / - / - |
| AM, 15/min, 3 h | No sharing | +0.27 / -0.28 / -0.01 | 14.73 / 15.28 / 15.01 | 2 / 0 / 3 | no / yes / no |
| AM, 15/min, 3 h | Drop-offs, onboard off | +0.20 / -0.24 / -0.08 | 14.80 / 15.24 / 15.08 | 2 / 0 / 3 | no / yes / no |
| AM, 15/min, 3 h | Drop-offs, onboard on | +0.20 / -0.24 / -0.08 | 14.80 / 15.24 / 15.08 | 2 / 0 / 3 | no / yes / no |
| AM, 20/min, 3 h | No sharing | +6.57 / +6.11 / +6.53 | 13.43 / 13.89 / 13.47 | 392 / 375 / 408 | no / no / no |
| AM, 20/min, 3 h | Drop-offs, onboard off | +2.76 / +2.14 / +2.52 | 17.24 / 17.86 / 17.48 | 38 / 31 / 53 | no / no / no |
| AM, 20/min, 3 h | Drop-offs, onboard on | +2.50 / +1.94 / +2.33 | 17.50 / 18.06 / 17.67 | 35 / 25 / 46 | no / no / no |
| Uniform, 10/min, 6 h | No sharing | +0.18 / +0.76 / +0.01 | 9.82 / 9.24 / 9.99 | 1 / 45 / 3 | no / no / no |
| Uniform, 10/min, 6 h | Drop-offs, onboard off | +0.19 / +0.89 / +0.01 | 9.81 / 9.11 / 9.99 | 3 / 55 / 2 | no / no / no |
| Uniform, 10/min, 6 h | Drop-offs, onboard on | +0.19 / +0.89 / +0.01 | 9.81 / 9.11 / 9.99 | 3 / 55 / 2 | no / no / no |
| Uniform, 15/min, 6 h | No sharing | +6.92 / - / - | 8.08 / - / - | 1991 / - / - | no / - / - |
| Uniform, 15/min, 6 h | Drop-offs, onboard off | +5.03 / +5.30 / - | 9.97 / 9.70 / - | 1470 / 1575 / - | no / no / - |
| Uniform, 15/min, 3 h | No sharing | +6.13 / - / - | 8.87 / - / - | 769 / - / - | no / - / - |
| Uniform, 15/min, 3 h | Drop-offs, onboard off | +5.42 / - / - | 9.58 / - / - | 642 / - / - | no / - / - |
| Uniform, 15/min, 3 h | Drop-offs, onboard on | +5.59 / - / - | 9.41 / - / - | 612 / - / - | no / - / - |
| Uniform, 20/min, 3 h | No sharing | +11.60 / - / - | 8.40 / - / - | 1694 / - / - | no / - / - |
| Uniform, 20/min, 3 h | Drop-offs, onboard off | +9.63 / - / - | 10.37 / - / - | 1371 / - / - | no / - / - |

| Demand | 10/min | 15/min | 20/min |
| --- | --- | --- | --- |
| AM | At or below supply: late growth is within 0.09 orders/min of zero with and without sharing. | At or below supply in these windows: late growth is -0.10 to -0.06 orders/min in seed 1 for 6 h, and within 0.28 of zero for 3 h. | Above supply: late growth is at least +6.11 orders/min with no sharing, and at least +1.94 with sharing. |
| Uniform | At the boundary: late growth is about +0.01 orders/min in seed 3, +0.18 in seed 1, and +0.76 to +0.89 in seed 2. | Above supply: late growth is at least +5.03 orders/min. | Above supply: late growth is at least +9.63 orders/min, from seed 1 only. |

Longer runs can change these readings, as the [adoption gates](../design/experimental-adoption.md#numerical-limits) say.

## Results

### Offers

| Schedule | Arm | Offered | Skipped | Accepted | Boarded | Completed |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| AM, 10/min, 6 h | No sharing | 3599 / 3599 / 3599 | 0 / 0 / 0 | 3599 / 3599 / 3599 | 3599 / 3599 / 3599 | 3597 / 3599 / 3599 |
| AM, 10/min, 6 h | Drop-offs, onboard off | 3599 / 3599 / 3599 | 0 / 0 / 0 | 3599 / 3599 / 3599 | 3599 / 3599 / 3599 | 3597 / 3599 / 3599 |
| AM, 10/min, 6 h | Drop-offs, onboard on | 3599 / 3599 / 3599 | 0 / 0 / 0 | 3599 / 3599 / 3599 | 3599 / 3599 / 3599 | 3597 / 3599 / 3599 |
| AM, 15/min, 6 h | No sharing | 5399 / - / - | 0 / - / - | 5399 / - / - | 5399 / - / - | 5398 / - / - |
| AM, 15/min, 6 h | Drop-offs, onboard on | 5399 / - / - | 0 / - / - | 5399 / - / - | 5399 / - / - | 5398 / - / - |
| AM, 15/min, 3 h | No sharing | 2699 / 2699 / 2699 | 0 / 0 / 0 | 2699 / 2699 / 2699 | 2699 / 2699 / 2699 | 2697 / 2699 / 2696 |
| AM, 15/min, 3 h | Drop-offs, onboard off | 2699 / 2699 / 2699 | 0 / 0 / 0 | 2699 / 2699 / 2699 | 2699 / 2699 / 2699 | 2697 / 2699 / 2696 |
| AM, 15/min, 3 h | Drop-offs, onboard on | 2699 / 2699 / 2699 | 0 / 0 / 0 | 2699 / 2699 / 2699 | 2699 / 2699 / 2699 | 2697 / 2699 / 2696 |
| AM, 20/min, 3 h | No sharing | 3599 / 3599 / 3599 | 0 / 0 / 0 | 3599 / 3599 / 3599 | 3370 / 3410 / 3360 | 3207 / 3224 / 3191 |
| AM, 20/min, 3 h | Drop-offs, onboard off | 3599 / 3599 / 3599 | 0 / 0 / 0 | 3599 / 3599 / 3599 | 3595 / 3599 / 3597 | 3561 / 3568 / 3546 |
| AM, 20/min, 3 h | Drop-offs, onboard on | 3599 / 3599 / 3599 | 0 / 0 / 0 | 3599 / 3599 / 3599 | 3597 / 3597 / 3594 | 3564 / 3574 / 3553 |
| Uniform, 10/min, 6 h | No sharing | 3599 / 3599 / 3599 | 0 / 0 / 0 | 3599 / 3599 / 3599 | 3599 / 3597 / 3599 | 3598 / 3554 / 3596 |
| Uniform, 10/min, 6 h | Drop-offs, onboard off | 3599 / 3599 / 3599 | 0 / 0 / 0 | 3599 / 3599 / 3599 | 3599 / 3599 / 3599 | 3596 / 3544 / 3597 |
| Uniform, 10/min, 6 h | Drop-offs, onboard on | 3599 / 3599 / 3599 | 0 / 0 / 0 | 3599 / 3599 / 3599 | 3599 / 3599 / 3599 | 3596 / 3544 / 3597 |
| Uniform, 15/min, 6 h | No sharing | 5399 / - / - | 0 / - / - | 5399 / - / - | 3605 / - / - | 3408 / - / - |
| Uniform, 15/min, 6 h | Drop-offs, onboard off | 5399 / 5399 / - | 0 / 0 / - | 5399 / 5399 / - | 4167 / 4055 / - | 3929 / 3824 / - |
| Uniform, 15/min, 3 h | No sharing | 2699 / - / - | 0 / - / - | 2699 / - / - | 2119 / - / - | 1930 / - / - |
| Uniform, 15/min, 3 h | Drop-offs, onboard off | 2699 / - / - | 0 / - / - | 2699 / - / - | 2273 / - / - | 2057 / - / - |
| Uniform, 15/min, 3 h | Drop-offs, onboard on | 2699 / - / - | 0 / - / - | 2699 / - / - | 2284 / - / - | 2087 / - / - |
| Uniform, 20/min, 3 h | No sharing | 3599 / - / - | 0 / - / - | 3599 / - / - | 2110 / - / - | 1905 / - / - |
| Uniform, 20/min, 3 h | Drop-offs, onboard off | 3599 / - / - | 0 / - / - | 3599 / - / - | 2458 / - / - | 2228 / - / - |

### Service

Wait includes the elapsed wait of pending requests, and journey has only completed requests.
These columns are arm aggregates, so they are not matched.

| Schedule | Arm | Wait mean (s) | Wait p95 (s) | Journey mean (s) | Journey p95 (s) |
| --- | --- | ---: | ---: | ---: | ---: |
| AM, 10/min, 6 h | No sharing | 286 / 276 / 280 | 997 / 949 / 979 | 1003 / 991 / 1003 | 2340 / 2286 / 2369 |
| AM, 10/min, 6 h | Drop-offs, onboard off | 286 / 276 / 278 | 1006 / 950 / 973 | 1004 / 991 / 1001 | 2327 / 2286 / 2358 |
| AM, 10/min, 6 h | Drop-offs, onboard on | 286 / 276 / 278 | 1006 / 950 / 973 | 1004 / 991 / 1001 | 2327 / 2286 / 2358 |
| AM, 15/min, 6 h | No sharing | 376 / - / - | 1155 / - / - | 1097 / - / - | 2480 / - / - |
| AM, 15/min, 6 h | Drop-offs, onboard on | 368 / - / - | 1151 / - / - | 1091 / - / - | 2499 / - / - |
| AM, 15/min, 3 h | No sharing | 357 / 332 / 347 | 1136 / 1011 / 1070 | 1079 / 1044 / 1072 | 2439 / 2317 / 2421 |
| AM, 15/min, 3 h | Drop-offs, onboard off | 348 / 331 / 344 | 1120 / 1026 / 1091 | 1072 / 1044 / 1069 | 2439 / 2315 / 2420 |
| AM, 15/min, 3 h | Drop-offs, onboard on | 348 / 331 / 344 | 1120 / 1026 / 1091 | 1072 / 1044 / 1069 | 2439 / 2315 / 2420 |
| AM, 20/min, 3 h | No sharing | 2047 / 1984 / 2109 | 4231 / 4134 / 4258 | 2493 / 2437 / 2548 | 4753 / 4717 / 4781 |
| AM, 20/min, 3 h | Drop-offs, onboard off | 1130 / 1089 / 1202 | 2685 / 2534 / 2824 | 1834 / 1794 / 1896 | 3748 / 3624 / 3837 |
| AM, 20/min, 3 h | Drop-offs, onboard on | 1084 / 1064 / 1154 | 2628 / 2545 / 2743 | 1794 / 1776 / 1852 | 3731 / 3658 / 3806 |
| Uniform, 10/min, 6 h | No sharing | 337 / 611 / 317 | 936 / 1909 / 880 | 1721 / 1970 / 1723 | 3222 / 3758 / 3282 |
| Uniform, 10/min, 6 h | Drop-offs, onboard off | 337 / 677 / 334 | 935 / 2117 / 942 | 1719 / 2029 / 1740 | 3220 / 3909 / 3302 |
| Uniform, 10/min, 6 h | Drop-offs, onboard on | 337 / 677 / 334 | 935 / 2117 / 942 | 1719 / 2029 / 1740 | 3220 / 3909 / 3302 |
| Uniform, 15/min, 6 h | No sharing | 5732 / - / - | 10304 / - / - | 6049 / - / - | 11067 / - / - |
| Uniform, 15/min, 6 h | Drop-offs, onboard off | 4780 / 5061 / - | 9108 / 9395 / - | 5380 / 5642 / - | 10082 / 10364 / - |
| Uniform, 15/min, 3 h | No sharing | 3091 / - / - | 5731 / - / - | 3676 / - / - | 6478 / - / - |
| Uniform, 15/min, 3 h | Drop-offs, onboard off | 2779 / - / - | 5412 / - / - | 3497 / - / - | 6327 / - / - |
| Uniform, 15/min, 3 h | Drop-offs, onboard on | 2757 / - / - | 5351 / - / - | 3515 / - / - | 6333 / - / - |
| Uniform, 20/min, 3 h | No sharing | 4558 / - / - | 7773 / - / - | 4558 / - / - | 8217 / - / - |
| Uniform, 20/min, 3 h | Drop-offs, onboard off | 4016 / - / - | 7286 / - / - | 4277 / - / - | 7931 / - / - |

### Distance and stopped pods

| Schedule | Arm | Passenger (km) | Empty (km) | Loaded % | Stopped pod-s |
| --- | --- | ---: | ---: | ---: | ---: |
| AM, 10/min, 6 h | No sharing | 35,502 / 35,266 / 35,682 | 20,400 / 20,133 / 19,986 | 63.5 / 63.7 / 64.1 | 10,852 / 13,702 / 15,392 |
| AM, 10/min, 6 h | Drop-offs, onboard off | 35,416 / 35,171 / 35,612 | 20,422 / 20,052 / 19,809 | 63.4 / 63.7 / 64.3 | 11,491 / 12,589 / 15,623 |
| AM, 10/min, 6 h | Drop-offs, onboard on | 35,416 / 35,171 / 35,612 | 20,422 / 20,052 / 19,809 | 63.4 / 63.7 / 64.3 | 11,491 / 12,589 / 15,623 |
| AM, 15/min, 6 h | No sharing | 53,405 / - / - | 29,708 / - / - | 64.3 / - / - | 6,984 / - / - |
| AM, 15/min, 6 h | Drop-offs, onboard on | 53,111 / - / - | 29,360 / - / - | 64.4 / - / - | 6,912 / - / - |
| AM, 15/min, 3 h | No sharing | 26,817 / 26,333 / 26,950 | 14,472 / 14,230 / 14,095 | 64.9 / 64.9 / 65.7 | 3,269 / 3,947 / 3,697 |
| AM, 15/min, 3 h | Drop-offs, onboard off | 26,690 / 26,198 / 26,758 | 14,290 / 14,090 / 13,994 | 65.1 / 65.0 / 65.7 | 3,411 / 3,468 / 4,023 |
| AM, 15/min, 3 h | Drop-offs, onboard on | 26,690 / 26,198 / 26,758 | 14,290 / 14,090 / 13,994 | 65.1 / 65.0 / 65.7 | 3,411 / 3,468 / 4,023 |
| AM, 20/min, 3 h | No sharing | 31,831 / 31,882 / 31,747 | 29,451 / 29,437 / 29,512 | 51.9 / 52.0 / 51.8 | 8,523 / 9,136 / 9,100 |
| AM, 20/min, 3 h | Drop-offs, onboard off | 31,125 / 30,963 / 31,046 | 25,480 / 25,152 / 26,170 | 55.0 / 55.2 / 54.3 | 8,250 / 7,592 / 7,424 |
| AM, 20/min, 3 h | Drop-offs, onboard on | 30,992 / 30,801 / 30,938 | 25,102 / 25,214 / 25,851 | 55.2 / 55.0 / 54.5 | 8,228 / 8,240 / 7,979 |
| Uniform, 10/min, 6 h | No sharing | 68,905 / 69,055 / 70,089 | 17,468 / 26,570 / 16,554 | 79.8 / 72.2 / 80.9 | 5,601 / 7,314 / 5,309 |
| Uniform, 10/min, 6 h | Drop-offs, onboard off | 68,883 / 68,695 / 70,030 | 17,483 / 28,226 / 17,247 | 79.8 / 70.9 / 80.2 | 5,511 / 7,488 / 5,013 |
| Uniform, 10/min, 6 h | Drop-offs, onboard on | 68,883 / 68,695 / 70,030 | 17,483 / 28,226 / 17,247 | 79.8 / 70.9 / 80.2 | 5,511 / 7,488 / 5,013 |
| Uniform, 15/min, 6 h | No sharing | 66,602 / - / - | 43,924 / - / - | 60.3 / - / - | 10,415 / - / - |
| Uniform, 15/min, 6 h | Drop-offs, onboard off | 67,008 / 66,420 / - | 43,481 / 44,015 / - | 60.6 / 60.1 / - | 9,887 / 9,993 / - |
| Uniform, 15/min, 3 h | No sharing | 37,911 / - / - | 23,756 / - / - | 61.5 / - / - | 5,710 / - / - |
| Uniform, 15/min, 3 h | Drop-offs, onboard off | 38,049 / - / - | 23,603 / - / - | 61.7 / - / - | 5,877 / - / - |
| Uniform, 15/min, 3 h | Drop-offs, onboard on | 38,252 / - / - | 23,399 / - / - | 62.0 / - / - | 5,762 / - / - |
| Uniform, 20/min, 3 h | No sharing | 37,568 / - / - | 25,143 / - / - | 59.9 / - / - | 5,834 / - / - |
| Uniform, 20/min, 3 h | Drop-offs, onboard off | 38,620 / - / - | 24,047 / - / - | 61.6 / - / - | 6,315 / - / - |

### Sharing activity

| Schedule | Arm | Joined parties | Occupied pickups | Intermediate stops | Largest detour |
| --- | --- | ---: | ---: | ---: | ---: |
| AM, 10/min, 6 h | No sharing | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 | 1.000 / 1.000 / 1.000 |
| AM, 10/min, 6 h | Drop-offs, onboard off | 13 / 17 / 16 | 0 / 0 / 0 | 12 / 13 / 15 | 1.107 / 1.156 / 1.331 |
| AM, 10/min, 6 h | Drop-offs, onboard on | 13 / 17 / 16 | 0 / 0 / 0 | 12 / 13 / 15 | 1.107 / 1.156 / 1.331 |
| AM, 15/min, 6 h | No sharing | 0 / - / - | 0 / - / - | 0 / - / - | 1.000 / - / - |
| AM, 15/min, 6 h | Drop-offs, onboard on | 57 / - / - | 0 / - / - | 46 / - / - | 1.344 / - / - |
| AM, 15/min, 3 h | No sharing | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 | 1.000 / 1.000 / 1.000 |
| AM, 15/min, 3 h | Drop-offs, onboard off | 26 / 26 / 30 | 0 / 0 / 0 | 24 / 19 / 22 | 1.344 / 1.202 / 1.244 |
| AM, 15/min, 3 h | Drop-offs, onboard on | 26 / 26 / 30 | 0 / 0 / 0 | 24 / 19 / 22 | 1.344 / 1.202 / 1.244 |
| AM, 20/min, 3 h | No sharing | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 | 1.000 / 1.000 / 1.000 |
| AM, 20/min, 3 h | Drop-offs, onboard off | 679 / 658 / 701 | 0 / 0 / 0 | 543 / 539 / 576 | 1.398 / 1.381 / 1.396 |
| AM, 20/min, 3 h | Drop-offs, onboard on | 746 / 730 / 759 | 118 / 128 / 139 | 577 / 557 / 572 | 1.379 / 1.473 / 1.423 |
| Uniform, 10/min, 6 h | No sharing | 0 / 0 / 0 | 0 / 0 / 0 | 0 / 0 / 0 | 1.000 / 1.000 / 1.000 |
| Uniform, 10/min, 6 h | Drop-offs, onboard off | 1 / 20 / 5 | 0 / 0 / 0 | 1 / 19 / 5 | 1.062 / 1.080 / 1.126 |
| Uniform, 10/min, 6 h | Drop-offs, onboard on | 1 / 20 / 5 | 0 / 0 / 0 | 1 / 19 / 5 | 1.062 / 1.080 / 1.126 |
| Uniform, 15/min, 6 h | No sharing | 0 / - / - | 0 / - / - | 0 / - / - | 1.000 / - / - |
| Uniform, 15/min, 6 h | Drop-offs, onboard off | 699 / 623 / - | 0 / 0 / - | 619 / 547 / - | 1.349 / 1.226 / - |
| Uniform, 15/min, 3 h | No sharing | 0 / - / - | 0 / - / - | 0 / - / - | 1.000 / - / - |
| Uniform, 15/min, 3 h | Drop-offs, onboard off | 195 / - / - | 0 / - / - | 173 / - / - | 1.293 / - / - |
| Uniform, 15/min, 3 h | Drop-offs, onboard on | 210 / - / - | 14 / - / - | 186 / - / - | 1.290 / - / - |
| Uniform, 20/min, 3 h | No sharing | 0 / - / - | 0 / - / - | 0 / - / - | 1.000 / - / - |
| Uniform, 20/min, 3 h | Drop-offs, onboard off | 405 / - / - | 0 / - / - | 349 / - / - | 1.246 / - / - |

### Onboard off against onboard on

The limits are those of the gates: added wait over the larger of 30 s and 5%, and added journey over the larger of 60 s and 5%.
Arm columns are the aggregate changes, and the unlabeled mean columns are matched.

| Schedule | Seed | Boarded in both | Wait mean | Completed in both | Journey mean | Arm wait mean | Arm journey mean | Arm journey p95 | Wait over limit | Journey over limit | Largest added wait (s) | Completed only in first / only in second | Late growth change |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| AM, 10/min, 6 h | 1 | 3599 | +0.00% | 3597 | +0.00% | +0.00% | +0.00% | +0.00% | 0 | 0 | 0 | 0 / 0 | +0.00 |
| AM, 10/min, 6 h | 2 | 3599 | +0.00% | 3599 | +0.00% | +0.00% | +0.00% | +0.00% | 0 | 0 | 0 | 0 / 0 | +0.00 |
| AM, 10/min, 6 h | 3 | 3599 | +0.00% | 3599 | +0.00% | +0.00% | +0.00% | +0.00% | 0 | 0 | 0 | 0 / 0 | +0.00 |
| AM, 15/min, 3 h | 1 | 2699 | +0.00% | 2697 | +0.00% | +0.00% | +0.00% | +0.00% | 0 | 0 | 0 | 0 / 0 | +0.00 |
| AM, 15/min, 3 h | 2 | 2699 | +0.00% | 2699 | +0.00% | +0.00% | +0.00% | +0.00% | 0 | 0 | 0 | 0 / 0 | +0.00 |
| AM, 15/min, 3 h | 3 | 2699 | +0.00% | 2696 | +0.00% | +0.00% | +0.00% | +0.00% | 0 | 0 | 0 | 0 / 0 | +0.00 |
| AM, 20/min, 3 h | 1 | 3594 | -4.08% | 3553 | -2.30% | -4.11% | -2.22% | -0.44% | 685 | 603 | 3143 | 8 / 11 | -0.26 |
| AM, 20/min, 3 h | 2 | 3597 | -2.26% | 3563 | -1.14% | -2.23% | -0.98% | +0.93% | 915 | 831 | 2842 | 5 / 11 | -0.20 |
| AM, 20/min, 3 h | 3 | 3593 | -4.09% | 3534 | -2.49% | -4.00% | -2.29% | -0.79% | 850 | 752 | 3284 | 12 / 19 | -0.19 |
| Uniform, 10/min, 6 h | 1 | 3599 | +0.00% | 3596 | +0.00% | +0.00% | +0.00% | +0.00% | 0 | 0 | 0 | 0 / 0 | +0.00 |
| Uniform, 10/min, 6 h | 2 | 3599 | +0.00% | 3544 | +0.00% | +0.00% | +0.00% | +0.00% | 0 | 0 | 0 | 0 / 0 | +0.00 |
| Uniform, 10/min, 6 h | 3 | 3599 | +0.00% | 3597 | +0.00% | +0.00% | +0.00% | +0.00% | 0 | 0 | 0 | 0 / 0 | +0.00 |
| Uniform, 15/min, 3 h | 1 | 2236 | -0.69% | 2022 | +0.05% | -0.77% | +0.51% | +0.09% | 379 | 289 | 4203 | 35 / 65 | +0.17 |

The on and off ledgers are byte-identical in each row with +0.00%.

### No sharing against onboard off

| Schedule | Seed | Boarded in both | Wait mean | Completed in both | Journey mean | Arm wait mean | Arm journey mean | Arm journey p95 | Wait over limit | Journey over limit | Largest added wait (s) | Completed only in first / only in second | Late growth change |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| AM, 10/min, 6 h | 1 | 3599 | +0.07% | 3597 | +0.03% | +0.07% | +0.03% | -0.57% | 401 | 254 | 1293 | 0 / 0 | -0.04 |
| AM, 10/min, 6 h | 2 | 3599 | -0.15% | 3599 | -0.04% | -0.15% | -0.04% | +0.00% | 315 | 183 | 1972 | 0 / 0 | +0.02 |
| AM, 10/min, 6 h | 3 | 3599 | -0.92% | 3599 | -0.24% | -0.92% | -0.24% | -0.44% | 344 | 219 | 1954 | 0 / 0 | +0.03 |
| AM, 15/min, 3 h | 1 | 2699 | -2.38% | 2697 | -0.69% | -2.38% | -0.69% | -0.01% | 381 | 273 | 1368 | 0 / 0 | -0.07 |
| AM, 15/min, 3 h | 2 | 2699 | -0.23% | 2699 | -0.03% | -0.23% | -0.03% | -0.08% | 382 | 268 | 1093 | 0 / 0 | +0.03 |
| AM, 15/min, 3 h | 3 | 2699 | -0.95% | 2696 | -0.26% | -0.95% | -0.26% | -0.04% | 385 | 286 | 1374 | 0 / 0 | -0.07 |
| AM, 20/min, 3 h | 1 | 3370 | -43.54% | 3207 | -29.86% | -44.77% | -26.43% | -21.14% | 253 | 207 | 1882 | 0 / 354 | -3.81 |
| AM, 20/min, 3 h | 2 | 3410 | -43.76% | 3224 | -29.63% | -45.14% | -26.39% | -23.18% | 227 | 196 | 2357 | 0 / 344 | -3.97 |
| AM, 20/min, 3 h | 3 | 3360 | -42.11% | 3191 | -29.17% | -43.02% | -25.59% | -19.75% | 301 | 264 | 2458 | 0 / 355 | -4.01 |
| Uniform, 10/min, 6 h | 1 | 3599 | -0.07% | 3596 | -0.04% | -0.07% | -0.09% | -0.09% | 58 | 43 | 1085 | 2 / 0 | +0.01 |
| Uniform, 10/min, 6 h | 2 | 3597 | +11.09% | 3533 | +3.26% | +10.88% | +3.03% | +4.01% | 1227 | 1056 | 3377 | 21 / 11 | +0.14 |
| Uniform, 10/min, 6 h | 3 | 3599 | +5.14% | 3596 | +0.95% | +5.14% | +0.98% | +0.62% | 947 | 755 | 1401 | 0 / 1 | +0.00 |
| Uniform, 15/min, 6 h | 1 | 3605 | -19.47% | 3408 | -14.33% | -16.60% | -11.06% | -8.91% | 391 | 349 | 4854 | 0 / 521 | -1.88 |
| Uniform, 15/min, 3 h | 1 | 2111 | -11.26% | 1918 | -6.31% | -10.10% | -4.86% | -2.33% | 349 | 287 | 4854 | 12 / 139 | -0.71 |
| Uniform, 20/min, 3 h | 1 | 2102 | -14.64% | 1896 | -9.15% | -11.90% | -6.16% | -3.48% | 354 | 313 | 5074 | 9 / 332 | -1.97 |

At AM 15/min with 6 h of arrivals, the off arm did not run.
The on arm has no occupied pickup, and against no sharing it gives -1.97% wait, -0.60% journey, and +0.76% journey p95.
That pair has 1,041 wait exceedances and 722 journey exceedances.

Small mean changes come with many individual changes.
For example, AM 10/min and AM 15/min have mean journey changes under 1%, and hundreds of matched requests exceed the limits.
The study does not trace this movement to a mechanism.

## Occupied pickup gates

The counters read every waiting party with no pod at each dispatch.
A pair is one such party and one occupied, stopped pod at its station, and a pair repeats on each tick.
The stop count is the calls to continue a journey, so it can exceed the intermediate stop column.
At 10/min, only the two seeds below ran with counters, and the ledgers of the others show no pickup.

| Schedule | Seed | Stops with riders staying aboard | Stops with a party with no pod waiting | Parties seen | Pairs seen | Not ready | Candidate refused | Pickups |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| AM, 10/min, 6 h | 1 | 12 | 0 | 0 | 0 | 0 | 0 | 0 |
| AM, 15/min, 3 h | 1 | 24 | 0 | 0 | 0 | 0 | 0 | 0 |
| AM, 15/min, 3 h | 2 | 19 | 1 | 1 | 1 | 0 | 1 | 0 |
| AM, 15/min, 3 h | 3 | 22 | 0 | 0 | 0 | 0 | 0 | 0 |
| AM, 20/min, 3 h | 1 | 612 | 330 | 552 | 27,238 | 7,261 | 19,859 | 118 |
| AM, 20/min, 3 h | 2 | 600 | 353 | 553 | 25,026 | 4,784 | 20,114 | 128 |
| AM, 20/min, 3 h | 3 | 605 | 335 | 558 | 26,804 | 6,168 | 20,497 | 139 |
| Uniform, 10/min, 6 h | 2 | 19 | 2 | 2 | 2 | 0 | 2 | 0 |
| Uniform, 15/min, 3 h | 1 | 190 | 154 | 287 | 3,069 | 30 | 3,025 | 14 |

These counts are the measured reason for few pickups at the lower rates.
A pod seldom stops with riders aboard, and at such a stop a party with no pod seldom waits.
In the measured arms above supply, most pairs fail at the candidate check.
The counters do not split that check into the stop limit, the detour cap, and the route.

## Adoption gates

A 3-seed screen on two demand patterns does not meet the [evidence requirements](../design/experimental-adoption.md#evidence-required).
The gates need all London demand bands, seeds 1 to 10, the qualified load range, mixed Acton arrivals, rail-hub bursts, and both queue limits.
This verdict compares onboard on with onboard off.

| Gate | Verdict | Reading |
| --- | --- | --- |
| Finite recovery | Fails | At the 4 h cap, 5 to 12 requests that complete with onboard off do not complete with it on at AM 20/min, and 35 at uniform 15/min. |
| Aggregate service | Passes where read | The largest increases are +0.93% journey p95 at AM 20/min and +0.51% mean journey at uniform 15/min. |
| Individual pickup wait | Fails | 685 to 915 requests at AM 20/min, and 379 at uniform 15/min. The largest added wait is 4,203 s. |
| Individual journey | Fails | 603 to 831 requests at AM 20/min, and 289 at uniform 15/min. |
| Sustained backlog | Fails | Late growth is +1.94 to +2.50 orders/min at AM 20/min, and onboard on lowers it by 0.19 to 0.26. Uniform 15/min adds +0.17 orders/min. These windows are shorter than the six hours of the gate. |
| Shared detour | Passes | The largest ratio in an on arm is 1.473. |
| Benefit | Fails | The largest mean gains are 4.11% in wait and 2.29% in journey, under the 5% limit. The capacity part of this gate is not read. |

These gates cannot be read here:

- Safety and accounting: compare runs no separation or speed oracle, and the ledgers account for every offer.
- Restore fidelity: the arms do not save or restore.
- Live queue loss: all arms use the study queue limit, so no arm skips an offer.
- Capacity: the study does not establish a qualified rate.
- Disabled-path and active-path cost: the study does not measure CPU.

The on arms took about three times the wall time of the off arms at AM 20/min on a loaded host, for example 1,787 s against 530 s.
That is not a CPU measurement, and the host load changed during the runs.

## Old records

The study found no old record of onboard pickup on a London network.
The [rejected candidates](../design/experimental-adoption.md#rejected-candidates) table has only Example-network screens, and qualification has London sharing without onboard pickup.

### Example-network screens

These reproduce closely at the current source, with `go run ./cmd/onboardbridge` on the study branch.
The one-pod record came with `4f65b5bd`, the multi-pod screen names `5a57e4c`, and the records are `docs/onboard-pickup-service-screen.md` and `docs/occupied-pickup-multipod-screen.md` in git history.
The settings differ in the tick rate, 60 Hz then and 30 Hz now, and in the source.
The study does not measure how much of each difference the tick rate causes.
The bridge does not repeat the old safety and restore checks.

| Fixture and measure | Old off | Current off | Old on | Current on |
| --- | ---: | ---: | ---: | ---: |
| One pod, shared, maximum wait (s) | 225.57 | 225.73 | 46.05 | 46.10 |
| One pod, shared, empty distance (m) | 1,844.21 | 1,844.21 | 0 | 0 |
| Two pods, wait p95 (s) | 201.867 | 202.333 | 36.050 | 36.100 |
| Two pods, journey p95 (s) | 317.383 | 317.900 | 116.733 | 116.800 |
| Two pods, empty distance (m) | 8,181.493 | 8,181.493 | 0 | 0 |
| Three pods, wait p95 (s) | 184.850 | 185.267 | 121.250 | 121.400 |
| Three pods, journey p95 (s) | 327.850 | 328.200 | 266.333 | 266.533 |
| Three pods, empty distance (m) | 8,018.726 | 8,013.010 | 7,256.686 | 7,256.183 |

The private one-pod case still has no change.
The two-pod fixture still passes the individual limits, with a largest added wait of 13.767 s against 14.183 s.
The three-pod fixture still fails on the same two final Garden parties.
Their added wait and journey are 106.133 s and 93.700 s, against 106.400 s and 94.033 s, and 30.833 s and 44.100 s, against 31.200 s and 44.267 s.

The London result does not extend these screens.
They force the pickup with fixed offers, and on LondonFull the conditions for a pickup are rare until the service is above supply.

### Drop-offs sharing on LondonCentral

The [drop-offs record](qualification.md#drop-offs-sharing-in-london) reproduces in direction and not in value.
The bridge ran the AM peak row at the 13 rates from 1 to 13/min with seeds 1 to 3, and the arms of the cap table with the same flags and their own band and interval.

```sh
compare -project london-central.json -pattern profile -bands am-peak -duration 65m -arrivals-for 30m -loads 60s,30s,20s,15s,12s,10s,8.571429s,7.5s,6.666667s,6s,5.454545s,5s,4.615385s -seeds 1,2,3 -redistribution-policies off -focus 940GZZLUEUS -queue-limit 1000000 -stop-when-drained -sharing-consent shared -sharing-limits 4 -sharing-modes destination,drop-offs -workers 2 -format csv
```

The settings differ in these ways:

- The source is `4bffb6f7`, against `a06afef` and later for the record, with the project generated at each source.
- The tick rate is 30 Hz, against 60 Hz, which also changes the truncation of the intervals.
- The bridge gives `-sharing-consent shared`, a flag that the old commands do not have.
- The bridge omits `-adaptive-limit` and the other bands of the table.
- The study did not check that the old project and the current project have the same fleet class.

| AM peak, 1 to 13/min | Old destination | Current destination | Old drop-offs | Current drop-offs |
| --- | ---: | ---: | ---: | ---: |
| Mean journey (s) | 526.8 | 530.4 | 520.8 | 523.5 |
| Mean wait (s) | 129.3 | 133.2 | 122.4 | 125.2 |
| Mean maximum wait (s) | 572.0 | 583.0 | 553.3 | 567.0 |
| Empty distance per served request (m) | 2,953 | 2,712 | 2,862 | 2,639 |
| Occupancy | 1.002 | 1.003 | 1.011 | 1.012 |
| Largest detour | 1.000 | 1.000 | 1.509 | 1.438 |

The record has 1.509 before the detour cap and 1.443 over the later 300 arms with the cap.
The current largest detour is under the cap of 1.5.

The destination arm at 13/min seed 1 now ends at 3,900 s, so its 60-minute limit at these rates is 12/min, against 13/min in the record.
No current drop-offs arm from 1 to 13/min ends after 3,600 s.
The study did not run 14/min, so it does not confirm the drop-offs limit of 14/min.

| Cap table arm | Old destination end (s) | Current destination end (s) | Old drop-offs end (s) | Current drop-offs end (s) | Same late drop-offs result |
| --- | ---: | ---: | ---: | ---: | --- |
| PM peak, 15/min, seed 1 | 3,303 | 3,436 | 3,707 | 3,299 | No |
| PM peak, 15/min, seed 2 | 3,439 | 3,416 | 3,611 | 3,614 | Yes |
| Late, 13/min, seed 1 | 3,279 | 3,301 | 3,627 | 3,749 | Yes |
| Night, 12/min, seed 1 | 3,267 | 3,496 | 3,680 | 3,634 | Yes |
| Night, 15/min, seed 1 | 3,538 | 3,714 | 3,742 | 3,397 | No |

### LondonFull with no sharing

The old sustained rows do not reproduce at 15/min, and this baseline is not a repeat of them.
The [mirrored layout study](station-mirror-load.md) and the [sustained swap study](pickup-swap-sustained.md) use 287 pods, and the current preset has 327.
The [controller record](../design/experimental-adoption.md#rejected-candidates) uses the same fleet at `bab8559`, and its rows below are in `git show a3ce698:docs/london-full-controller-sustained.md`.
Their passenger site count is 269, against 309 now.
They also ran at 60 Hz, at their own sources, and with seeds 1 and 2 only.
The platoon limit, the queue limit, and the AM peak demand band name are the same, and the old runs give no consent flag.

| AM rate | Old late growth, seeds 1 / 2, 6 h | Current late growth, no sharing |
| ---: | ---: | --- |
| 10/min | +0.04 / -0.12 | -0.05 / +0.01 / +0.00, 6 h |
| 15/min | +3.04 / +3.12 | -0.10, seed 1, 6 h |
| 20/min | +8.12 / +8.29 | +6.57 / +6.11 / +6.53, 3 h |

The study measures the changed result and does not assign it to one difference.

## What did not run

The sweep was cut to lower the host load and then stopped on request, and these cells did not run:

- AM 15/min with 6 h of arrivals: the off arm, and seeds 2 and 3.
- AM 20/min and uniform 20/min with 6 h of arrivals: all arms, and one probed on arm of each reached the 3,000 s process limit.
- Uniform 15/min with 6 h of arrivals: all on arms, the no-sharing arms of seeds 2 and 3, and the off arm of seed 3, and the on arm of seed 1 reached the 3,000 s limit.
- Uniform 15 and 20/min with 3 h of arrivals: seeds 2 and 3.
- Uniform 20/min with 3 h of arrivals: the on arm of seed 1, which was stopped before it finished.
- Any arm at the default queue limit of 200, so this study has no skipped offers to read.

The 3 h arms replaced the 6 h arms at 20/min, and at the remaining 15/min arms, when the two probed arms reached that limit.
The 10/min results and two uniform 15/min off arms existed by then, and no on arm above supply had finished.
A 3 h arm does not give the six-hour window of the backlog gate.
