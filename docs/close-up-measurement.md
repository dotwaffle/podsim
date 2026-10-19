# Close-up following: measured benefit

This record measures how much layer B of the clearance design (close-up following) could gain on the presets.
It is a measurement before a design decision, and no close-up code exists.

## Method

- Commit measured: `95dbeb9` (`sim: Keep origin-covered lanes in saved routes`), with layer A on main.
- A temporary probe (not committed) runs in `grant` before the ownership check, on the same state that the grant sees.
- A hold is one tick in which a traveling pod's request is refused, and each refusing resource is a track cell of one other traveling pod that is ahead on the follower's route.
- A refusal that includes a node, a junction, a berth, a fault owner, or two pods is not a hold, and the record counts it as another refusal.
- Linked platoon pods are excluded, and platooning is off, which is the `cmd/compare` default.
- `P` is the path distance from the follower to the leader along the follower's route.
- The turn is a fixed bound over the shared run, as the review of the design requires: the total turn of the follower's route from its current lane through the last lane that both routes share, within 300 m of the follower (the platoon horizon).
- With `c = cos(turn/2)` and a body of 4 m, the hold is feasible when `P - 5/c - 0.01 >= v²/(2c)`.
- The extra distance is `min(x + P - 5/c - 0.01, end of the requested cells) - end of the reserved cells`, at zero or more, with `x` the follower position.
- A hold is binding when the reserved end is within the stopping distance of the next tick, so the boundary limits motion.
- The share columns divide by the traveling pod-seconds of the run.
- Each run is one `cmd/compare` arm with seed 1, `-redistribution-policies off`, and the default routing.
- Each run took less than 10 s of wall time, with `GOMAXPROCS=8` and `ulimit -v 16000000`.

## Presets and demand

| Run | Project | Demand arm | Simulated time |
| --- | --- | --- | --- |
| small | `small` | hotspot, 45 s (default) | 30 min |
| small-10 | `small` | hotspot, 10 s | 30 min |
| busy | `busy` | hotspot, 45 s | 30 min |
| busy-10 | `busy` | hotspot, 10 s | 30 min |
| parking | `parking-constrained` | hotspot, 45 s | 30 min |
| parking-10 | `parking-constrained` | hotspot, 10 s | 30 min |
| depot | `busy -parking-lanes 2 -parking-berths 5` | hotspot, 45 s | 30 min |
| depot-10 | same depot ring | hotspot, 10 s | 30 min |
| compact | `compact-station` | hotspot, 45 s | 30 min |
| compact-10 | `compact-station` | hotspot, 10 s | 30 min |
| rail-hub | `rail-hub` | hub-burst, 5 s, burst 12, arrivals 5 min | 30 min |
| scale100 | `scale100` | hub-burst, 5 s, burst 12, arrivals 5 min | 30 min |
| london | `london-central` | profile am-peak, 6 s, arrivals 30 min, focus `940GZZLUEUS` | 65 min |

The ring and compact presets had few holds at 45 s, so each got one more run at 10 s.
At 10 s, these runs are overloaded: they served 52 to 95 of 179 requests.

## Results

| Run | Traveling pod-s | Hold pod-s (share) | Other refusals (share) | Feasible of holds | Feasible, binding, extra > 0 (share) | Holds at 2 m/s or less (pod-s) | Speed p50/p90 (m/s) | `P` p50/p90 (m) | Extra p50 (m) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| small | 10,144 | 12 (0.12 %) | 0.81 % | 69 % | 0.08 % | 3 | 10.3/13.6 | 96/130 | 57 |
| small-10 | 20,378 | 46 (0.22 %) | 0.98 % | 54 % | 0.12 % | 12 | 8.0/13.4 | 99/154 | 57 |
| busy | 12,728 | 11 (0.08 %) | 0.46 % | 83 % | 0.07 % | 2 | 11.4/13.6 | 100/114 | 57 |
| busy-10 | 50,901 | 338 (0.67 %) | 1.99 % | 68 % | 0.43 % | 90 | 6.7/13.2 | 67/128 | 52 |
| parking | 14,024 | 21 (0.15 %) | 0.88 % | 47 % | 0.07 % | 2 | 9.6/13.6 | 96/147 | 56 |
| parking-10 | 33,201 | 205 (0.62 %) | 1.55 % | 65 % | 0.37 % | 54 | 7.9/13.2 | 67/119 | 52 |
| depot | 12,727 | 14 (0.11 %) | 0.50 % | 86 % | 0.09 % | 2 | 11.3/13.1 | 104/125 | 57 |
| depot-10 | 50,953 | 449 (0.88 %) | 2.33 % | 71 % | 0.60 % | 130 | 7.1/12.9 | 68/122 | 53 |
| compact | 3,701 | 1 (0.02 %) | 14.7 % | 0 % | 0 % | 1 | 0/0 | 116/121 | none |
| compact-10 | 6,622 | 868 (13.1 %) | 21.5 % | 81 % | 10.6 % | 666 | 0/8.8 | 25/45 | 19 |
| rail-hub | 37,184 | 832 (2.24 %) | 4.45 % | 76 % | 1.52 % | 549 | 0/10.2 | 34/114 | 22 |
| scale100 | 56,128 | 1,274 (2.27 %) | 2.76 % | 72 % | 1.45 % | 308 | 7.6/12.7 | 62/100 | 29 |
| london | 182,533 | 199 (0.11 %) | 0.62 % | 87 % | 0.09 % | 54 | 9.5/13.4 | 103/191 | 75 |

- Other refusals are refusals by a node or junction resource, a berth, or two pods; junction resources are the largest part in every run.
- In every run, the other refusals take more pod time than the holds.
- Most holds on the ring, depot, and London runs occur at speed: a moving follower brakes for the retained cells of a leader 60 m to 100 m ahead.
- Holds at low speed are most of the hold time only in compact-10 (77 %) and rail-hub (66 %).
- In the other runs, they are 29 % of the hold time or less.

## Where holds occur

| Run | Locations, by hold pod-seconds |
| --- | --- |
| Ring and depot runs | Station through lanes before the station exit merge (51 % to 65 %), lanes before a station (13 % to 32 %), and station departure lanes (11 % to 29 %, never feasible) |
| depot-10 | Depot lanes: 3.6 pod-s in 30 min |
| compact-10 | The lane before the compact station entry (`loop-up`): 100 % |
| rail-hub | The through lane before the `s07` exit (47 %), the lane before `s01` (42 %), and departure lanes (11 %, never feasible) |
| scale100 | Mesh lanes before a merge junction (55 %), departure lanes (20 %), lanes before a station diverge (19 %), and exit lanes (5 %) |
| london | Lanes before a station (41 %), lanes before a merge (21 %), other trunk lanes (11 %), departure lanes (11 %), and exit lanes (10 %) |

## Estimate of the benefit

This section is an analytic estimate from the recorded holds, and no simulation ran with close-up following.

- The hard upper bound is the feasible, binding hold time: close-up cannot save more time than the pods spend in such holds.
- A closer bound uses one saving for each hold episode: the time to cover the largest extra distance of the episode, at the lane speed limit (low value) or from rest at 2 m/s² (high value), and at most the feasible hold time of the episode.

| Run | Hard bound (pod-s) | Episode estimate (pod-s) | Share of traveling time | Effect |
| --- | --- | --- | --- | --- |
| compact-10 | 703 | 59 to 160 | 0.9 % to 2.4 % | Served 52 requests in 30 min, overloaded; at most about 9 % more entries if each saved second shortened the entry cycle |
| scale100 | 816 | 548 to 687 | 1.0 % to 1.2 % | Mean journey 785 s; about 8 s to 10 s shorter if all saved pod time reached passengers |
| rail-hub | 567 | 101 to 178 | 0.3 % to 0.5 % | Mean journey 823 s; about 2 s to 4 s shorter on the same basis |

- These values are upper bounds for three reasons.
- The leader is usually held too, so a closer follower does not move the head of the queue.
- In compact-10, junction waits (4,792 pod-s) are 7.5 times the track waits (637 pod-s), so the entry junction, not the queue spacing, sets the rate.
- In scale100, 55 % of the holds wait behind a merge junction, where the same limit applies.

## Conclusion

Layer B is not worth building for the current presets.
At the documented loads, track-only holds take at most 2.3 % of traveling pod time, and London takes 0.11 %.
The estimated saving is at most about 1.2 % of pod time, and refusals by junctions and nodes, which close-up cannot share, are larger than the holds.
The ring depot lanes have almost no holds.
The only material case is a station entry queue on one approach lane at overload (compact station at 10 s), where 10.6 % of traveling time is a feasible hold, but the entry junction limits the rate.
Revisit layer B only for a station or depot design whose throughput depends on long, slow queues on one lane, and measure that design with this probe first.

## Caveats

- Each preset has one seed and one or two loads, so the shares show the order of size and are not precise.
- Platooning is off.
  With virtual platoons, slow-pod links already cover part of the low-speed holds.
- The feasibility test uses the fixed run turn of the review.
  A tighter turn bound would admit more holds, but the feasible share is already 47 % to 87 %, so the bound does not change the conclusion.
- The depot ring parks a pod only when an idle pod clears a berth, and these arms had few such moves.
  Thus the 3.6 pod-s on depot lanes is weak evidence about depot queues, which were the stated reason for layer B.
- The estimate does not model queue spillback: a shorter queue can free an upstream merge or berth, which this record does not count.
