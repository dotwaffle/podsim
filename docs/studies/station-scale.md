# Smaller London stations

The ride page showed that a London station is large.
The maintainer asked (November 14, 2026) for station paths at least 50% shorter.
This study measures LondonFull with smaller stations, made by the `-station-scale` flag of `cmd/scenario`.
LondonCentral and LondonFull use scale 0.5 since this study.
The LondonFull network at that scale is the network that this study measures.
`london-full-express` keeps scale 1, because its approved contract fixes its geometry.

## Result

At scale 0.5, the median path from the diverge to a berth is 196 m, 51% shorter than at scale 1.
All three seeds drain at 0.5, and two of three drain at scale 1.
The mean wait is 8.5% shorter, the mean journey 4.7% shorter, and the empty distance 6.2% shorter, because each trip and each empty move drives less station track.
Track waits are 15% longer and stopped pod time 2% longer.
Shorter station lanes hold fewer queued pods, which can explain the longer track waits, but this study did not check the cause.
Three seeds give no statistical significance, and the differences between seeds are as large as most of the changes.

## Shape

The scale applies past the throat.
The diverge and the merge stay 80 m from the junction, because a nearer throat puts the access lanes across the guideways of the junction.
A first version that scaled from the junction center had 39 to 102 hard layout conflicts below scale 1.
The entry, the berth rows, the berth pitch, and the distance of the arrival and departure lanes from the station axis scale.
Parking facilities keep their depots, and the lanes of a Compact station stay outside its bays.

| Scale | Diverge to berth, median | Station lanes per station, median | Soft conflicts |
| ---: | ---: | ---: | ---: |
| 1.0 | 404 m | 1,650 m | 4 |
| 0.8 | 320 m | 1,403 m | 1 |
| 0.6 | 237 m | 1,158 m | 1 |
| 0.5 | 196 m | 1,036 m | 1 |
| 0.4 | 155 m | 912 m | 1 |

The station lanes shrink less than the paths, because the approach roads from the junction to the diverge keep their length.
Scales 0.7 and 0.75 have hard conflicts near Poplar and Canary Wharf, where the heading search puts two stations too near each other.
Scales 0.9, 0.65, 0.55, and 0.45 also build.

## Service

Each value is the mean of seeds 1 to 3.
A change is the mean of the paired changes from scale 1 for each seed.
Each arm schedules 899 requests.

| Scale | Drained | Mean wait | p95 wait | Mean journey | p95 journey | Junction wait | Track wait | Stopped pod time | Empty distance |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1.0 | 2/3 | 241 s | 847 s | 976 s | 2,173 s | 1,192 s | 279 s | 1,616 s | 4,125 km |
| 0.8 | 2/3 | -2.9% | 0.0% | -1.8% | -0.4% | -5.7% | +2.7% | -4.4% | -3.0% |
| 0.6 | 3/3 | -6.9% | -1.8% | -3.7% | -0.8% | -4.1% | +47.3% | +7.5% | -5.3% |
| 0.5 | 3/3 | -8.5% | -5.1% | -4.7% | -1.5% | -4.3% | +15.2% | +2.1% | -6.2% |
| 0.4 | 2/3 | -9.5% | -4.4% | -5.6% | -2.7% | +3.1% | +21.5% | +8.0% | -6.2% |

An arm that does not drain has one unfinished request at minute 120.
At scale 1, this rate drained 8 of 10 seeds in the [post-fix study](london-full-postfix.md#baseline-confirmation-results).
The track wait varies most between seeds: seed 2 has 296 s at scale 1 and 518 to 631 s at scales 0.6 to 0.4.

## Method

The measured source is `d8089c89`.
Each arm uses the AM peak band of the LondonFull profile at 15 requests per minute, with 60 minutes of arrivals and at most 60 more minutes to finish.
Sharing and redistribution are off, routing is free-flow, and virtual platoons are on, as in the post-fix study.
Each scale uses the same three demand schedules.
A run takes about 31 s and 650 MB.

```sh
mise run scenario -- -preset london-full -station-scale 0.5 -output /tmp/podsim-london-full-0.5.json
mise run compare -- -project /tmp/podsim-london-full-0.5.json -pattern profile -bands am-peak -loads 4s -seeds 1,2,3 -duration 120m -arrivals-for 60m -stop-when-drained -redistribution-policies off -platoon-policies virtual -queue-limit 1000000 -workers 3 -format csv
```

The path lengths are the shortest paths over the station lanes of each passenger station with berth rows, from its diverge node to each berth node, and from each berth node to its merge node.
