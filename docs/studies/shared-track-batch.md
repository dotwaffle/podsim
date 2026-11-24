# Shared track batch

This study is batch 2 of the [shape clearance study plan](../design/shape-clearance.md#81-batch-2-amended-2026-11-23).
It compares the shared-track preset `S` with LondonFull (`LF`), project `D`, and `S0`, which is `S` without Express.
It is not adoption evidence.

## Result

The checks of BT-D1 are not all met.

| Check of BT-D1 | Result |
| --- | --- |
| No arm has a safety or accounting failure | Met, 12 of 12 runs |
| Each arm finishes each accepted order | Not met. `D` and `S` finish in each seed. `LF` has 343, 327, and 342 orders at the end, and `S0` has 331, 0, and 309. |
| `S` finishes more hub passengers than `S0` | Met in each seed: 4,393, 4,788, and 4,898 against 290, 1,109, and 276 |

`LF` and `S0` nearly stop.
In the 2 hours of clear, a mean of 318 to 322 of the 327 pods of `LF` have a wait report that names another pod, and 293 to 298 of `S0` in seeds 1 and 3.
The cause in `LF` is a closed wait on link 100 (see [The block in `LF`](#the-block-in-lf)).
So a comparison with `LF` or `S0` shows the harm of this workload to a network without Express more than the effect of `S`.

## Method

The source is `df5832b8`, with the rules at `5099280d`.
The pilot was `S`, seed 1: 58 s of wall time with no failure.
The batch is 12 runs, with 3 workers, nice 10, and a memory cap.
The longest run took 416 s.

```sh
go run ./cmd/compare screen -configs LF,D,S0,S -bands am-peak -seeds 1,2,3 -workers 3 -output batch.json
```

The workload is the workload of the screen: 2 hours of arrivals, 10 background requests a minute, the hub stream, a queue limit of 200, and at most 2 hours of clear.
The safety oracle runs at each simulated second.
The D6 oracle does not run.
The pod-seconds come from one sample at each simulated second, and a pod that only slows behind another pod is not in them.
The command exits with 3, because its result is the pair of `D` and `LF`, and `D` has 11, 17, and 21 requests over the wait limit.

## Service

Each cell gives seeds 1, 2, and 3.
The values of the window stop at the end of the arrivals.
Hub passengers are out from Waterloo and in from Bank.

| Measure | `LF` | `D` | `S0` | `S` |
| --- | --- | --- | --- | --- |
| Refused events | 1,063, 1,040, 1,141 | 0, 0, 0 | 914, 642, 953 | 0, 0, 0 |
| Largest queue | 200, 200, 200 | 73, 68, 76 | 200, 200, 200 | 73, 91, 98 |
| Queue at the end of the window | 200, 200, 200 | 56, 59, 58 | 200, 200, 200 | 60, 85, 93 |
| Out, passengers a minute in the window | 0.5, 0.8, 0.5 | 25.2, 29.0, 28.9 | 1.9, 4.3, 1.5 | 24.8, 26.0, 25.9 |
| In, passengers a minute in the window | 0.3, 0.3, 0.6 | 9.1, 9.4, 9.5 | 0.6, 1.3, 0.8 | 8.7, 8.6, 8.6 |
| Out and in, whole parties a minute in the window | 0.1, 0.1, 0.1 | 3.4, 3.7, 3.9 | 0.3, 0.6, 0.3 | 3.4, 3.4, 3.6 |
| Out, mean wait (s) | 1,085, 1,227, 1,096 | 208, 282, 229 | 1,403, 2,428, 1,238 | 166, 473, 438 |
| In, mean wait (s) | 489, 527, 638 | 118, 163, 169 | 243, 264, 303 | 71, 338, 291 |
| Background, requests boarded | 426, 476, 398 | 1,199, 1,199, 1,199 | 554, 878, 580 | 1,199, 1,199, 1,199 |
| Background, mean wait (s) | 1,234, 1,156, 1,057 | 238, 225, 222 | 1,201, 1,027, 1,295 | 219, 206, 202 |
| Background, p95 wait (s) | 4,396, 4,093, 3,809 | 907, 836, 835 | 3,362, 2,748, 3,718 | 882, 789, 804 |
| Compact pod-seconds blocked by Express, window | 0, 0, 0 | 1,241, 1,501, 1,489 | 0, 0, 0 | 8,096, 9,158, 8,971 |
| Compact pod-seconds blocked by Compact, window | 1,601,076, 1,547,545, 1,619,216 | 6,308, 8,041, 7,371 | 1,371,672, 1,098,319, 1,334,324 | 8,724, 11,615, 11,809 |
| Empty distance in the window (km) | 4,318, 4,669, 4,309 | 5,820, 5,524, 5,249 | 4,921, 6,947, 5,194 | 5,063, 4,979, 4,732 |

The waits of `LF` and `S0` cover only the requests that boarded, so they are not comparable with those of `D` and `S`.

## Matched background waits

A change is the wait in the first arm less the wait in the second arm.
The limit is the wait limit of the screen.

| Pair | Matched | Mean change (s) | Median change (s) | Longer by more than the limit | Shorter by more than the limit | Largest change (s) |
| --- | --- | --- | --- | --- | --- | --- |
| `S` and `LF` | 426, 476, 398 | -1,082, -1,009, -933 | -580, -606, -483 | 0, 3, 3 | 336, 379, 302 | 2, 382, 179 |
| `S` and `S0` | 554, 878, 580 | -1,039, -834, -1,137 | -775, -752, -864 | 1, 5, 4 | 453, 779, 483 | 35, 382, 100 |
| `S` and `D` | 1,199, 1,199, 1,199 | -19, -19, -20 | -16, -13, -17 | 161, 140, 125 | 462, 442, 440 | 703, 1,180, 721 |

## `S` against `D`

Only `D` and `S` finish in each seed, so this is the pair with a meaning, but `D` and `S` differ in the network too.

- Both finish each hub party.
- In seeds 2 and 3, `S` finishes 10 percent fewer hub passengers in the window from Waterloo and 9 to 10 percent fewer from Bank, and the mean hub wait is 1.7 to 2.1 times that of `D`.
  In seed 1, `S` finishes 1 and 4 percent fewer, and the mean hub wait is shorter.
- The mean background wait of `S` is 19 to 20 s shorter, and 125 to 161 requests wait longer by more than the limit.
- Express pods block Compact pods 6 to 7 times as long, and the empty distance in the window is 10 to 13 percent less.

## The block in `LF`

The maintainer asked for a diagnosis (BT-D5).
A scratch probe ran the same workload in 6 runs at `84f2d115`, with no change to the simulation, and followed the `BlockedBy` value of each stopped pod once a minute.
The probe did not land.

| Run | Hub events kept | Result |
| --- | --- | --- |
| `LF`, seed 1, 180 minutes | All | A closed wait from minute 48. 320 of 327 pods blocked at minute 180. |
| `LF`, seed 1, 180 minutes | 1 in 4 | 792 of 1,116 orders complete by minute 90, then a closed wait from minute 94. 299 blocked at minute 180. |
| `LF`, seed 1, 180 minutes | None | No closed wait, and at most 4 pods blocked in the samples at each tenth minute. All 1,199 requests finish. |
| `LF`, seed 1, 62 minutes | All | The first run again, with the state of the two stations at minute 60. |
| `LF`, seed 2, 180 minutes | All | A closed wait from minute 52. |
| `S0`, seed 1, 180 minutes | All | No closed wait found. 322 pods travel, and 302 are blocked at minute 180. |

The closed wait has 127 to 130 pods, and almost all of them are on the 4 lanes of link 100, with 31 pods on each lane.
It has these parts:

1. The pods toward Bank fill link 100, because Bank has 2 berths and the head of the queue waits for a berth.
2. A Bank berth holds a pod that boarded for Waterloo, and it cannot leave, because the lanes toward Waterloo are full.
3. The pods toward Waterloo wait in the same way, and the Waterloo berths hold pods that boarded for Bank.

Each queue needs the other to move, so the loop does not open.
In the first run the loop stayed to the end of the probe, and other pods joined the queues behind it.
No rule limits the pods that go to a station to the space that it has.

The links of the loop are track and occupied berths, not only claims, so it is a different loop from that of the [berth claim deadlock](../contracts/berth-claim-deadlock-proposal.md), but a claim can have a part in it (see below).
It has the same shape as the mutual block of `D0` between Bank and Cannon Street in the adoption screen.

One fact has no explanation here.
At minute 60 of the first run, the head pod toward Waterloo waits for berth `02`, and no pod is in berths `03` to `05`.
The probe does not show if those berths are claimed by pods that are behind it, or if the head pod has no free choice of berth.
If one of these berths can take the head pod, the loop opens.

## Not tested

- A workload that `LF` and `S0` can carry, so the effect of the Express pods in `S` at such a load is not known.
- The cause of the stop of `S0`, and if the block of `LF` is older than the November 8 screen, where `LF` completed 2,531 to 2,658 hub passengers in `pm-peak`.
  That screen used a different band and an older network.
- Other bands, seeds 4 to 10, and six-hour runs.
