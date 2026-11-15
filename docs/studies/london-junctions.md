# London ramp junctions

This study compares LondonFull with chord junctions and with ramp junctions.
The [junction design](../design/london-junctions.md) sets the method and the adoption criteria.
The ramps network is the `-junctions ramps` option of `cmd/scenario`.

## Result

At 15 requests per minute, chords drain 5 of 6 seeds and ramps drain 4 of 6.
Ramps therefore fail the drain criterion by one seed, after three more seeds.
The mean wait is 2.2% longer and the mean journey 1.5% longer, so ramps pass the 5% criterion.
Junction waits, track waits, and stopped pod time are about twice as long with ramps.
At 20 requests per minute, neither network drains, and ramps are 12.2% worse in mean wait and 5.2% worse in mean journey.

Ramps put each split and each merge on a 2-way node along the carriageway, so pods queue at more nodes.
The ramps of the dropped turns are not there, and 5.6% of the AM peak demand uses such a turn on its chords route.
This study did not measure how each of these adds to the longer waits.
On 2026-11-15, the maintainer kept chords and asked for a follow-up on the cause of the longer junction waits.

## Service

Each value is the mean over the seeds.
A change is the mean of the paired changes from chords for each seed.

| Rate | Seeds | Network | Drained | Mean wait | p95 wait | Mean journey | p95 journey | Junction wait | Track wait | Stopped pod time | Empty distance |
| ---: | ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 15/min | 1 to 6 | chords | 5/6 | 205 s | 758 s | 892 s | 2,036 s | 1,140 s | 274 s | 1,700 s | 3,734 km |
| 15/min | 1 to 6 | ramps | 4/6 | +2.2% | +2.6% | +1.5% | +1.5% | +117% | +121% | +95% | +0.3% |
| 20/min | 1 to 3 | chords | 0/3 | 381 s | 1,255 s | 1,078 s | 2,447 s | 1,987 s | 404 s | 2,545 s | 6,601 km |
| 20/min | 1 to 3 | ramps | 0/3 | +12.2% | +8.0% | +5.2% | +5.0% | +176% | +254% | +176% | +6.1% |

At 15 requests per minute, each arm schedules 899 requests.
Chords leave one request at minute 120 with seed 4.
Ramps leave one request with seeds 1 and 4.
At 20 requests per minute, each arm schedules 1,199 requests, and 4 to 6 requests remain at minute 120.

## Turns without a ramp

The ramps network has no U-turn ramps, and 186 of 1,288 turns have no ramp, because they are too tight for 5 m/s.
The shortest route of each AM peak flow on the chords network, from the exit of its origin to the entry of its destination, shows how much demand uses these turns:

- No route uses a U-turn chord.
- 8,392 of 67,666 flows, with 5.6% of the demand weight, use a chord whose turn has no ramp.

This count covers passenger routes only.
It does not include the empty moves of the pods, and it uses free-flow routes.

## Method

The measured source is `b3d79964`.
Each arm uses the AM peak band of the LondonFull profile, with 60 minutes of arrivals and at most 60 more minutes to finish.
Sharing and redistribution are off, routing is free-flow, and virtual platoons are on, as in the [station scale study](station-scale.md).
Both networks use the same demand schedules.
The pilot run of one seed took 32 s and 795 MB.

```sh
mise run scenario -- -preset london-full -output /tmp/podsim-chords.json
mise run scenario -- -preset london-full -junctions ramps -output /tmp/podsim-ramps.json
mise run compare -- -project /tmp/podsim-ramps.json -pattern profile -bands am-peak -loads 4s,3s -seeds 1,2,3 -duration 120m -arrivals-for 60m -stop-when-drained -redistribution-policies off -platoon-policies virtual -queue-limit 1000000 -workers 3 -format csv
```

Seeds 4 to 6 ran at 15 requests per minute only, because the drain difference at seeds 1 to 3 was one seed.
