# Smaller London stations

The ride page showed that a London station is large.
The maintainer asked (November 14, 2026) for station paths at least 50% shorter.
This study measures LondonFull with smaller stations, made by the `-station-scale` flag of `cmd/scenario`.
LondonCentral and LondonFull use scale 0.5 since this study.
The LondonFull network at that scale is the network that this study measures.
Since November 16, 2026, the passenger stations of these presets that are not Compact stations are tight, as [Tight stations](#tight-stations) describes.
`-wide-stations` gives the scale 0.5 shape.
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

## Bank berths

At scale 0.5, a third and a fourth berth row at Bank and Monument cause no layout conflict.
With its share of the extra berths, Bank has 4 berths, and Harrow-on-the-Hill and Heathrow Terminals 2 & 3 have 2 in place of 3.
This layout builds at scales 0.4 to 0.6, 0.8, and 0.85, and has a hard conflict at Bank at 0.9.
On November 14, 2026, a paired run at scale 0.5 with the method below compared it with the measured network.
With 4 berths at Bank, one seed of three drains, against all three.
The other two seeds each have one unfinished request at minute 120.
The mean wait is 0.9% to 1.7% longer, the empty distance 0.6% to 1.1% longer, and the mean journey changes by at most 0.2%.
The presets keep 2 berths at Bank.

## Scale 0.4 with six seeds

On November 15, 2026, a paired run at `68a213f1` compared scale 0.4 with scale 0.5 over seeds 1 to 6, with the method below.
At this source, the presets set lateral merges.

| Scale | Drained | Mean wait | p95 wait | Mean journey | Junction wait | Track wait | Stopped pod time | Empty distance |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 0.5 | 6/6 | 205 s | 761 s | 892 s | 663 s | 240 s | 1,144 s | 3,736 km |
| 0.4 | 5/6 | -1.7% | -0.5% | -1.1% | +3.5% | +20.4% | +5.5% | -1.5% |

Each change is the mean of the paired changes for each seed.
At scale 0.4, seed 1 has one unfinished request at minute 120.
The mean wait is 1.1% to 2.6% shorter for each seed, and the mean journey 0.9% to 1.2% shorter.
The track wait changes by -56% to +104% for each seed, and the junction wait by -36% to +50%.

Scale 0.4 shortens the median path from the diverge to a berth from 196 m to 155 m.
The berth pitch and the width of the berth lanes stay large, because lanes that are not joined stay at least the 12 m clearance apart.
The maintainer kept scale 0.5.

## Tight stations

On November 16, 2026, a study at `b054fedc` measured a tight shape of LondonFull at station scale 0.5.
Tight sets each part of a passenger station past the throat to the 25 m minimum berth pitch.
The throat stays 80 m from the junction, and the diverge and the merge stay 30 m to each side of the axis.
Compact stations and Parking facilities keep their shapes.

| Part | Scale 0.5 | Tight |
| --- | ---: | ---: |
| Diverge to entry (`access-in`) | 63.2 m | 25.5 m |
| Entry to the first berth row | 45 m | 25 m |
| Arrival and departure lanes from the axis | 50 m | 25 m |
| Berth pitch | 37.5 m | 25 m |

The through lane is then 50 m long, against 100 m.
The keep-access arm keeps the entry of scale 0.5 and sets the other three parts to 25 m.
The pitch 25 arm sets only the berth pitch to 25 m.

| Arm | Diverge to berth, median |
| --- | ---: |
| Scale 0.5 | 195.7 m |
| Pitch 25 | 183.2 m |
| Keep-access | 135.2 m |
| Tight | 100.5 m |

With tight stations, the p90 path from the diverge to a berth is 125.5 m.
The pitch 25 path comes from section 2.7 of the [shape clearance design](../design/shape-clearance.md#27-stations).

The tight network has no hard and no soft layout conflicts.
The study built it with scratch options, and its network SHA-256 is `acba7958cf8bdaa25a67d24d891cdcfd04ac99586c9ad4918ddb4856d92845fe`.
In the study, a scratch row gap also moved the search shapes of the Compact stations, and a pitch of 50 m at scale 1 moved the search shapes of the Parking facilities.
The adopted generator keeps both shapes, so `-berth-pitch 50` gives the study network, and the default network differs from it only at the three Parking facilities.
With the adopted generator, these LondonFull variants validate with no soft conflicts: the default, `-berth-pitch 50`, `-station-scale 1`, `-station-scale 1 -berth-pitch 50`, and `-station-scale 0.4`.

Each change is the mean of the paired changes from scale 0.5 over seeds 1 to 6, with the method below.

| Arm | Drained | Mean wait | Mean journey | Junction wait | Stopped pod time |
| --- | ---: | ---: | ---: | ---: | ---: |
| Pitch 25 | 5/6 | -0.4% | -0.2% | +1.3% | -0.7% |
| Keep-access | 5/6 | -2.4% | -1.4% | +20% | +14% |
| Tight | 5/6 | -4.3% | -2.4% | +49% | +32% |

At scale 0.5, the means over the six seeds are 205.2 s of wait, 663 s of junction wait, and 1,144 s of stopped pod time.
Pitch 25 alone saves 12.5 m of path and gives no service change.
With tight stations, the empty distance is 2.5% shorter, and seed 1 has one unfinished request at minute 120.

The study recommended tight stations.
On November 16, 2026, the maintainer made tight stations the default of LondonCentral and LondonFull.
The default LondonFull network SHA-256 was then `e194426aa95104424a6365370f014c166f25d0572199f9b875e07bb58f2e880e`, with no hard and no soft layout conflicts.
Since November 20, 2026, it is `69baad6a8077b3f22c1e250023b4eaeefba10f1ff9bb0812b773634951d84225`, because the shape clearance rule applies to each network (see [LondonFull](../london-full.md#station-shape)).
`london-full-express` keeps wide stations at scale 1.

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
Since November 16, 2026, the presets have tight stations, so at a later source, add `-wide-stations` to `cmd/scenario` to build the scaled networks of this study.
