# Mirrored station layout load and restore checks

Mirroring reduces access-road crossings, but it does not consistently improve passenger service.
All 24 runs passed the sampled safety, order-accounting, and physical-restore checks.
LondonCentral Early demand had 14.3-17.6% higher average waits with the mirrored layout.
LondonFull still accumulated backlog at 15 and 20 requests per minute.
These results do not qualify a higher demand setting.

## Method

The frozen simulation source is `b488de2`.
The layout change is `f4eb618`.
Each pair uses the same runtime and one of two generated projects.
The earlier project disables only the station mirror pass.
The other project includes that pass.

Fixture checks permit differences only in node positions and lane control points.
Connectivity, station IDs, berth positions, fleet, demand weights, and runtime settings match.
LondonCentral has 114 pods and 99 sites, including three Parking facilities.
LondonFull has 287 pods and 272 sites, including three Parking facilities.

Both Full fixtures normalize inactive demand and zero sharing fields to the historical study project.
The earlier Full fixture equals that historical project after JSON decoding.
All six earlier-layout Full result records match the [sustained-load controls](london-full-sustained.md) exactly.

Each run receives six hours of endpoint demand, then at most one hour to finish.
Seeds are 1 and 2.
Full uses 2024 AM peak weights at nominal rates of 10, 15, and 20 requests per minute.
Central uses 2019 Early, AM peak, and PM peak weights at nominal rates of 10, 12, and 14.
Routing is free-flow, with virtual platoons limited to four pods.
Sharing, redistribution, station buffers, and pickup swaps are disabled.
Every pair has the same ordered request times, origins, and destinations.

## Results

Each unfinished count gives earlier / mirrored geometry.
Wait change compares average waits, including elapsed waits for requests still unboarded at the cutoff.
Empty-distance ratios compare mirrored / earlier geometry.
Late backlog growth covers hours three through six of the arrival window.

| Preset and band | Nominal requests/minute | Seed | Unfinished | Wait change | Empty-distance ratio | Late backlog growth |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| LondonFull AM | 10 | 1 | 1 / 1 | -0.49% | 0.9913 | +9 / +7 |
| LondonFull AM | 10 | 2 | 0 / 0 | -0.19% | 1.0039 | -22 / -21 |
| LondonFull AM | 15 | 1 | 363 / 403 | +6.36% | 1.0102 | +531 / +548 |
| LondonFull AM | 15 | 2 | 417 / 357 | -6.76% | 0.9865 | +588 / +561 |
| LondonFull AM | 20 | 1 | 2201 / 2209 | -0.14% | 1.0049 | +1448 / +1462 |
| LondonFull AM | 20 | 2 | 2243 / 2250 | +0.39% | 1.0014 | +1485 / +1493 |
| LondonCentral Early | 10 | 1 | 706 / 939 | +17.59% | 0.9327 | +514 / +592 |
| LondonCentral Early | 10 | 2 | 703 / 839 | +14.32% | 0.9495 | +500 / +575 |
| LondonCentral AM | 12 | 1 | 370 / 350 | -2.01% | 0.9981 | +433 / +440 |
| LondonCentral AM | 12 | 2 | 371 / 369 | +0.45% | 1.0016 | +442 / +430 |
| LondonCentral PM | 14 | 1 | 603 / 588 | +1.62% | 1.0001 | +611 / +592 |
| LondonCentral PM | 14 | 2 | 664 / 629 | -1.66% | 0.9891 | +619 / +620 |

Neither layout drains any Central arm within the cutoff.
Both Full layouts drain only seed 2 at 10/minute.
The earlier layout drains at 25,199 seconds and the mirrored layout at 24,329 seconds.
Every arm at 15 or 20/minute has growing late backlog.
No run skips a request.

Central Early demand leaves more unfinished requests with mirroring, despite less empty travel.
The study does not establish the cause of that regression.
Full at 15/minute improves for seed 2 and regresses for seed 1.
Fewer visual crossings therefore do not establish a throughput benefit.
These six-hour runs do not replace the historical finite-arrival qualification envelope.

## Restore checks and limits

The 24 parent runs passed 603,928 safety and order-accounting observations, one per simulated second.
A final census checks unique pending requests and rider timings against submitted and completed totals.
Parent runs continue without save or restore interruptions.

Separate copies restore at simulated hours one and five, for 48 physical restores.
Each restore preserves pose, bindings, pending routes, request ages, counters, and saved settings without demotion or order loss.
The check normalizes speed and transient wait and blocker fields.
It reapplies settings that the saved format does not retain.

Each copy then advances 60 simulated seconds without new arrivals.
All 172,800 continuation ticks pass safety and order-accounting checks.
Twenty checkpoints contain coupled pods, with a maximum of 21 coupled pods at one checkpoint.
This checks active platoon restoration without asserting identical future trajectories.

The two seeds per band do not establish starvation freedom or an indefinitely sustainable demand rate.
Nominal request intervals quantize to simulation ticks.
The CSV records actual offered rates and scheduled request counts.
Instrumentation and concurrent workers prevent server playback-speed conclusions from these run times.

[Per-arm measurements](measurements/station-mirror-load.csv) retain every comparison result field.
[Metadata](measurements/station-mirror-load.json) records fixtures, request schedule hashes, restore coverage, and validation limits.
The local artifact bundle is `~/.cache/agents/podsim/mirrored-load-20260930/`.
