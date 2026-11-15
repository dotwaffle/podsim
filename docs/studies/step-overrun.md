# Step overrun on LondonFull

The [incident soak timing](incident-soak.md#redistribution-and-reassignment-on-by-default) records a largest step of 71.9 to 72.9 ms on LondonFull at 120 orders/min with both policies off.
The tick budget is 33.3 ms at 30 Hz.
This study finds the work in those steps.

## Method

A scratch probe makes a new session from `scenarios.LondonFull()` with its 327 pods, sets `Demand.PerMinute` to 120, and runs 40 simulated minutes (72,000 ticks).
It times each session step with `time.Now`, as the earlier table did, and it times the `sim.Simulation.Step` call inside it separately.
The session step has no publication.
It reads the GC cycle count from `runtime/metrics` before and after each step.
The arms are both policies off and both on, at the preset seed 20260929 and at seeds 1 and 2: one run per arm.
The source is `c32be4e5` with Go 1.27.3 on 12 cores.
The host load was 1.8 to 3.2, with a media transcode on one core.
The runs were not niced, and each ran under a 24 GiB memory cap.
A second build adds scratch timers and counters to the phases of `Step`, to the dispatch functions, to `searchRoute`, and to the station route batch, and it names the work in each slow step.
The probe did not land.

The pilot took 21 s of wall time.
A repeat of the pilot gave the same slow ticks, 11425, 18235, and 35113, so the cost is work and not host noise.

## Steps over the budget

Each row times the session step with the build that has no scratch timers.

| Both | Seed | Steps over 33.3 ms | Largest step | Its tick | GC in that step | p50 | p99.9 |
|---|---|---|---|---|---|---|---|
| Off | 20260929 | 4 | 73.0 ms | 18235 | No | 239 µs | 8.1 ms |
| On | 20260929 | 6 | 86.5 ms | 18235 | No | 242 µs | 9.5 ms |
| Off | 1 | 3 | 42.1 ms | 15416 | No | 242 µs | 7.1 ms |
| On | 1 | 5 | 70.6 ms | 13291 | No | 243 µs | 8.9 ms |
| Off | 2 | 5 | 120.9 ms | 13171 | Yes | 242 µs | 7.2 ms |
| On | 2 | 6 | 131.9 ms | 13171 | No | 245 µs | 10.0 ms |

The preset rows agree with the earlier readings: 71.9 to 72.9 ms off, and 82.3 to 83.0 ms on.
A GC cycle ends in 6 of the 29 steps over the budget.
At seed 2, tick 13171 takes 131.9 ms with both on and no GC, so GC does not cause the overrun.
The demand step and the delivery of interruptions take less than 6 ms in each slow step.
The build with scratch timers has 40 steps over the budget in the same six arms.
`Simulation.dispatch` takes 83 to 100 percent of each of these steps, and 95 percent or more in 36 of them.

## Cause

Each slow step is a dispatch pass in which a free pod is offered to many trips with no pod, and many trips in turn decline it and take a hold for a finishing pod.

The scratch counters show the same state before the slow steps in the six arms.
The queue holds 198 to 200 trips, at or near the session `QueueLimit` of 200.
Up to 100 of them have no pod.
At the start of 35 of the 40 slow steps, no pod in the fleet passes `pickupCandidate`, because each pod is occupied, claimed, or withdrawn, or cannot divert.
At the start of 3 of them, one pod passes.
The other 2 are tick 5250 at seed 2, while the queue fills, with 26 candidates.
While no pod is a candidate, `pickupPodForRequest` returns at once, and a step takes less than 1 ms.

In the slow step, at least one pod becomes a pickup candidate.
Dispatch then works through the trips with no pod in queue order:

1. `cachedPickupPod` keeps one result for each dispatch key, so each origin calls `pickupPodForRequest`.
   It needs the route from the free pod to that origin.
2. The pod is away from the origin, so `assignPickupPod` calls `waitForFinishingPod`.
   For each busy pod that the bound does not prune, it calls `stationRouteByLoad` from the node where the pod becomes free to the origin of the trip.
3. If a busy pod is expected to arrive more than 2 s sooner, within the hold limit, the trip takes a hold, and the free pod stays a candidate for the next trip.
4. Most of the node and station pairs are new.
   Each one is a miss of the route cache, and `cacheStationRoutesForClass` runs a search to all the berths of the station.
   A new origin also misses `stationPickupBounds`, which runs a reverse search.

At tick 18235 with both off, the instrumented step takes 81 ms:

| Work | Calls | Time |
|---|---|---|
| Station route batches under `waitForFinishingPod` | 217 | 33.5 ms |
| Route searches under `pickupPodForRequest` | 142 | 16.8 ms |
| Station route batches under `pickupPodForRequest` | 58 | 14.0 ms |
| Misses of `stationPickupBounds` | 15 | 6.0 ms |

The searches start from 125 different nodes and go to 85 different nodes.
The pass makes 84 checks for a finishing pod, and 83 trips get a new hold.
The free pod is still a candidate at the start of the next tick.

The new holds set `deferCheck` to the same tick, one second later.
At tick 18265, the 84 checks run again with warm routes and take 20.7 ms, with 1 route search and 60 batches.
At tick 18295, they take 6.0 ms in total.
Such synchronized checks also make some of the steps of 15 to 25 ms.

With both on, the same pass has more new pairs: at tick 18235, 274 batches under `waitForFinishingPod` and 149 route searches under `pickupPodForRequest`.
Tick 49321 also spends 3.7 ms in `swapPickups`.
At seed 2, tick 13171 makes 332 batches under `waitForFinishingPod`, which take 70.4 ms of the step.

A free pod after a period with none is common, and a burst is rare.
A full trace of the preset seed with both off has 435 steps in which a pod becomes a candidate while none was before.
In 400 of them, the first trip takes the pod after one check for a finishing pod, and each step takes 4.7 ms or less.
In 6 of them, 2 to 4 trips check, and each step takes 6.3 ms or less.
In 29 of them, 6 to 84 trips check in turn.
These 29 steps include the 4 steps over the budget, and 23 of them take more than 8 ms each.
A burst needs a full queue with many trips that have no pod, and a free pod that a busy pod beats by more than 2 s at many of their origins.
At 120 orders/min, each arm has 3 to 6 steps over the budget in 40 minutes.
The earlier table has no step over the budget at 10 orders/min.

The route cache does not cause it.
A run with `routeCacheLimit` raised from 8,192 to 1,048,576 makes 3,806 route searches in place of 5,543.
Tick 18235 still makes 152 route searches, and the step takes 74 ms in that run.
The misses are pairs that the run has not used before.

## Fix options

1. Scan the busy pods of `waitForFinishingPod` in order of their lower bound.
   The function scans the fleet in fleet order and prunes a pod whose bound cannot beat the best so far.
   A scan in order of `remaining` plus the station bound finds a good pod early and prunes more pods before their route search.
   The choice must stay the minimum time with fleet order as the tie-break.
   `pickupCannotImprove` prunes only above a small margin, so a pod with an equal time is not pruned.
   The change is then expected to alter cost only, and no dispatch choice, route, or timing, but a parity test must show the same choices.
   It does not touch the work of `pickupPodForRequest` and the station bounds, about 37 ms at tick 18235, because one free pod has no other pod to prune.
   So this option alone leaves that step over the budget.
2. Spread the burst over ticks.
   For example, run the check for a finishing pod for at most a set number of trips in each tick, or give each hold check a different tick.
   This bounds the step.
   It changes the tick at which a trip gets a hold or a pod, so it changes dispatch choices and timing, and the soak and qualification readings.
   It needs maintainer approval.

## Result of option 1

The commit "sim: Scan finishing pods by lower bound" lands option 1.
`finishingPod` sorts the busy pods by their bound and stops at the first pod whose bound cannot improve on the best time.
An equal time goes to the earlier pod in fleet order, so the scan holds each trip for the same pod as the scan in fleet order.
`TestFinishingPodMatchesFleetOrder` compares the two scans on a busy London network, with constructed ties.
In each of the six arms, the saved state after 40 minutes has the same SHA-256 digest before and after the change.

The probe ran each arm 5 times with each build, interleaved, at a host load of 3.2 to 10.4, except for one pair at 20.
The table gives the median of the 5 runs.

| Both | Seed | Largest step before | Largest step after | Steps over 33.3 ms before | Steps over 33.3 ms after |
|---|---|---|---|---|---|
| Off | 20260929 | 81.6 ms | 49.2 ms | 5 | 2 |
| On | 20260929 | 91.2 ms | 58.7 ms | 6 | 4 |
| Off | 1 | 44.2 ms | 40.5 ms | 3 | 2 |
| On | 1 | 73.4 ms | 57.7 ms | 6 | 6 |
| Off | 2 | 112.3 ms | 50.3 ms | 6 | 3 |
| On | 2 | 137.5 ms | 62.4 ms | 9 | 7 |

Tick 18235 with both off takes 80.7 ms before and 49.2 ms after.
The work of `pickupPodForRequest` and the station bounds stays, so each arm still has steps over the budget.

## Pass reset after a refused board

`boardOrWait` resets the dispatch pass before `board`, also when `board` then refuses the trip.
A study of a flood with faults found about 44 such resets in each tick of a slow window, and proposed to reset only after a board that succeeds.
`board` refuses before it changes a pod, a trip, or the queue, so a refusal needs no reset.

A probe on November 15, 2026, at `06af2151`, measured this change on the LondonFull flood with faults, seed 3.
The project is `scenario -preset london-full -fault-rate 10 -emergency-rate 10 -fault-duration fixed:600 -debris-share 0.25 -debris-meters 2`.
The run is `compare -pattern destination -focus 940GZZLUKSX -loads 3s -arrivals-for 60m -duration 60m -platoon-policies virtual -format json -workers 1`.

- The compare reports of seeds 1 to 3 are byte-equal with and without the change.
- The resets from `boardOrWait` fall from 29,501 to 623 in the hour, and the board refusals stay at 28,878.
- The `pickupPodForRequest` calls stay at 3,704,839, so trips with the same key share no more pickup results.
- The refusals fall in minutes 8 to 18, where the pickup work is small, and minutes 54 to 60 have none.
- The median wall time of three interleaved runs is 83.9 s without the change and 83.1 s with it, inside the spread of each build.

The change gives no measurable gain, and it breaks the rule of `readyTrips` that the pass resets before each change.
So it does not land.

## Step time on November 15, 2026

A probe on November 15, 2026, at `f761c98e` with Go 1.27.4, ran the six arms of the method again, one run per arm, at a host load of 2.5 to 4.2, except for one run that ended at 7.8.
Since the [result of option 1](#result-of-option-1), the station pickup bounds are built with the prepared network, and the London stations are at scale 0.5.

| Both | Seed | Largest step | Tick | Steps over 33.3 ms | p99.9 |
|---|---|---|---|---|---|
| Off | 20260929 | 28.1 ms | 13127 | 0 | 5.83 ms |
| On | 20260929 | 31.7 ms | 13127 | 0 | 6.61 ms |
| Off | 1 | 14.7 ms | 12814 | 0 | 3.86 ms |
| On | 1 | 21.2 ms | 12814 | 0 | 5.34 ms |
| Off | 2 | 23.1 ms | 13487 | 0 | 4.88 ms |
| On | 2 | 31.3 ms | 13487 | 0 | 5.16 ms |

No arm has a step over the budget.
Tick 18235 takes 0.2 to 0.5 ms.

### Route tree memo

The memo of station route trees keeps 512 trees and replaces the oldest tree first.
With both policies off, no tree is replaced in 40 minutes.
With both on, each run builds about 5,100 trees, of which about 2,900 were replaced before, and the swap pass asks for about 25,000 of the about 33,000 trees.
At tick 13127 with both on, 22 of the 30 trees that the step builds were replaced before, which is about 8 to 11 ms of the step.

A replay of the logged tree requests of each arm compared other policies at 512 slots.
The replay of the current policy gave the same hits and misses as the runs.

- A second-chance bit or a protected segment changes the builds in the largest steps by 0 to 3 trees, because the swap pass asks for more than 512 other trees before a slow step asks again.
- A tree only on the second request, with a targeted search on the first, adds work with both policies off, where no tree is replaced.
- A limit of 128 slots for the trees of the swap pass saves 3 to 11 builds in the largest steps, about 1 to 4 ms, which is inside the run-to-run spread, and adds 17 to 21% more builds in each run.
- With no limit, the largest steps build 12 to 19 trees in a run, against 65 to 121.

No policy at 512 slots gives a gain worth its cost, so none was built.

### Reverse reachability set

A check of the route from a berth onward could use a static set of the nodes that reach the berth, in place of a route search.
A scratch build found the set exact on the London, Express, and compact station networks when the set models each bank in stages, and the search stays in use while a lane is blocked and for a trip within one station.
In a 40-minute run, the set gave the same answers as the search for 269,858 requests.
At a host load of 13 to 25, tick 13127 took 10 to 21 ms with the set, against 25 to 35 ms without it, in three runs of each build.
The set changes the meaning of the pickup bounds and caches that the incident suspension contract names, so it needs a contract change.
No step is over the budget, so the maintainer parked this option on November 15, 2026.
