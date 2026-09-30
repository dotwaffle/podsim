# LondonFull sustained-load screen

The six-hour AM peak screen finds growing backlogs at 15 and 20 requests per minute.
Station approach buffers do not resolve this overload.
At 10/minute, buffers lower observed average waits but increase empty travel and leave more requests unfinished.
Buffers remain disabled by default.
The preset and other defaults remain unchanged.

## Method

The measured production source is `fbe0332`.
The frozen study binary's production-file hashes match that commit.
The project is the unchanged 287-pod LondonFull baseline from the [post-fix study](london-full-postfix.md).
It has 269 passenger sites, three Parking facilities, and weighted passenger berth capacity.

Each arm receives six hours of 2024 AM peak endpoint demand.
It then has at most one additional hour to finish.
The study stops early only after every request finishes and arrivals have ended.
Rates are 10, 15, and 20 requests per minute, with seeds 1 and 2.
Each schedule runs with buffers off and on.
Sharing, redistribution, and pickup swaps are disabled.
Routing is free-flow, with virtual platoons limited to four pods.

Every buffers-off result field matches the earlier sustained baseline exactly.
The comparison uses identical projects and demand schedules.
Seed 2 at 10/minute drains with buffers off at 25,199 seconds, one second before the cutoff.
Every other arm reaches 25,200 seconds with unfinished work.

## Results

Each pair below gives buffers off / on.
Backlog growth measures outstanding requests at hour six minus those at hour three.
Throughput covers that same final three-hour arrival period.

| Requests/minute | Seed | Unfinished at cutoff | Late backlog growth | Late completions/minute | Average wait | Empty-distance ratio, on/off |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 10 | 1 | 1 / 8 | +9 / +19 | 9.950 / 9.894 | 301.9 / 281.5 s | 1.0942 |
| 10 | 2 | 0 / 62 | -22 / +20 | 10.122 / 9.889 | 307.1 / 285.6 s | 1.0762 |
| 15 | 1 | 363 / 328 | +531 / +533 | 12.050 / 12.039 | 2,095.0 / 2,030.0 s | 0.9998 |
| 15 | 2 | 417 / 367 | +588 / +566 | 11.733 / 11.856 | 2,185.8 / 2,053.7 s | 0.9940 |
| 20 | 1 | 2,201 / 2,187 | +1,448 / +1,437 | 11.956 / 12.017 | 5,265.3 / 5,191.6 s | 0.9995 |
| 20 | 2 | 2,243 / 2,218 | +1,485 / +1,462 | 11.750 / 11.878 | 5,309.8 / 5,274.6 s | 0.9990 |

The schedules contain 3,599, 5,399, or 7,199 requests at their respective rates.
No arm skips a request.
Average waits include each unboarded request's elapsed wait at the cutoff.
These elapsed waits can still increase before boarding.
Final completion counts alone do not establish sustained capacity.

At 15 and 20/minute, late completion rates remain near 12/minute while backlog grows in every arm.
At 10/minute, the two seeds give different backlog trends with buffers off.
This screen does not establish indefinitely sustainable operation at 10/minute or a universal capacity boundary.

## Buffer diagnosis and validation

The first buffer candidate had a circular wait between a berthless head pod and an assigned pickup behind it.
The head rejected a physically free berth because the following pickup had that berth as its destination.
The pickup could not pass the head to reach it.
Commit `fbe0332` changes head admission to check physical resource ownership and retain the following assignment.
Atomic complete-path grants still protect berths, track cells, and conflict regions.
Regression tests cover eventual completion, protected resources, competing approaches, and immediate physical restore.

Before this fix, seed 1 at 10/minute left 1,225 requests unfinished and seed 2 left 2,373.
Both pre-fix 15/minute arms hit the Go test timeout.
They are incomplete runs, not completed capacity results.
No pre-fix 20/minute arm started.
The table above uses only the completed post-fix study.

All twelve post-fix processes passed 302,399 safety and order-accounting observations, one per simulated second.
The final census checks unique pending requests and rider timings against submitted and completed totals.
These observations do not prove every-tick safety for untested schedules or starvation freedom.
The stopped-pod metric outside eligible buffer lanes includes station access and does not isolate mainline delay.
Wall time includes instrumentation and concurrent work, so it does not measure maximum server playback speed.

[Per-arm measurements](measurements/london-full-sustained.csv) retain all comparison result fields.
[Study metadata](measurements/london-full-sustained.json) records source identity, the project hash, and validation boundaries.
The local artifact bundle is `~/.cache/agents/podsim/station-buffer-20260929/fixed-sustained/`.
