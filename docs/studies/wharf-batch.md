# Wharf batch

This study compares `london-full-shared-express-wharf` (`W`) with the same network and no Express (`W0`), with `london-full-shared-express` (`S`), and with LondonFull (`LF`), on 2026-11-24.
The rules are in [section 12 of the Express London hubs design](../design/london-hubs-d8-d9.md#12-wharf-study-rules), which was committed before any run.
It qualifies no preset.
The [qualification](../contracts/express-wharf-qualification-proposal.md) is open, and its row HX11 failed.

## Result

The three checks of the rules are met in each seed.

| Check | Result |
| --- | --- |
| No safety, contract, or accounting failure | Met, 12 of 12 runs |
| Each arm finishes each accepted order | Met, 12 of 12 runs |
| On each line, the mean hub wait of `W` is at most that of `W0` | Met, 3 of 3 seeds on both lines |

- The third check is not a comparison of equal parties.
  `W0`, `S`, and `LF` reach the queue limit of 200 and refuse 181 to 207 events in each seed, and `W` refuses none.
  Their waits cover the parties that boarded whole.
- On the new line, `W` carries each of the 2,625 to 3,021 hub passengers, and `W0` carries 1,447 to 1,521 of them.
- The waits of `W` on the new line are long: 386, 459, and 548 s.
  The line has 16 Express pods for about 22.5 passengers a minute, so its fleet is a limit (inferred, not measured).

## Method

The pilot was `W`, seed 1: 104 s of wall time with no failure.
The batch is 12 runs, with 3 workers, nice 10, and an address space cap.
It took 505 s.

```sh
go run ./cmd/compare screen -configs LF,S,W0,W -bands am-peak -seeds 1,2,3 -hub-mean 120s -workers 3 -output batch.json
```

The source is `5fa9fa48`, and the code of the screen and of the simulation is that of `201ffd17`.
The command exits with 4, because the screen has no pair of `D`.
The host had a high load from other work, which changes the wall times only.

The workload is that of the screen: 2 hours of arrivals, 10 background requests a minute, a queue limit of 200, and at most 2 hours of clear.
The Waterloo and Bank stream has a mean of 120 s.
The London Bridge and Canary Wharf stream has the rates of HX-D3: a mean of 28 s, 17 in 20 from London Bridge.
Each arm gets the same events, and a party on a line that an arm does not serve splits into Compact orders.
The safety oracle runs at each simulated second, and the D6 oracle does not run.

## Service

Each cell gives seeds 1, 2, and 3.
"Wait" is the passenger-weighted mean wait of the hub parties that boarded whole, in seconds.

| Measure | `LF` | `S` | `W0` | `W` |
| --- | --- | --- | --- | --- |
| Wait, Waterloo and Bank | 528, 564, 390 | 42, 50, 43 | 339, 507, 300 | 41, 50, 53 |
| Wait, London Bridge and Canary Wharf | 1,154, 1,357, 1,185 | 1,336, 1,329, 1,169 | 1,236, 1,237, 1,247 | 386, 459, 548 |
| London Bridge and Canary Wharf passengers finished, of 2,625, 2,699, and 3,021 | 1,517, 1,397, 1,547 | 1,519, 1,474, 1,579 | 1,521, 1,447, 1,471 | 2,625, 2,699, 3,021 |
| Refused events | 181, 206, 207 | 199, 201, 206 | 196, 194, 207 | 0, 0, 0 |
| Background, mean wait | 666, 618, 637 | 653, 627, 650 | 654, 625, 669 | 211, 202, 198 |
| Background, p95 wait | 1,967, 1,973, 1,916 | 2,156, 2,033, 1,997 | 1,860, 1,804, 1,866 | 858, 789, 809 |
| Background requests that boarded, of 1,199 | 1,122, 1,119, 1,120 | 1,097, 1,103, 1,108 | 1,119, 1,121, 1,128 | 1,199, 1,199, 1,199 |
| Queue peak | 200, 200, 200 | 200, 200, 200 | 200, 200, 200 | 73, 81, 87 |
| Empty distance in the window (km) | 8,100, 7,956, 7,438 | 7,666, 7,309, 7,479 | 8,426, 8,164, 8,136 | 5,433, 5,274, 5,130 |
| Compact pod-seconds blocked by Express, window | 0, 0, 0 | 616, 646, 1,041 | 0, 0, 0 | 2,459, 1,540, 1,772 |
| Compact pod-seconds blocked by Compact, window | 721,032, 682,903, 739,331 | 767,399, 737,352, 729,369 | 649,732, 622,539, 668,767 | 7,866, 9,109, 9,224 |

The mean wait of a party of `W` on the new line is 357, 410, and 488 s from London Bridge, and 105, 213, and 309 s from Canary Wharf.

## Matched background waits

Each cell gives seeds 1, 2, and 3, over the background requests that boarded in both arms.

| Pair | Mean change (s) | Longer by more than the limit | Shorter by more than the limit |
| --- | --- | --- | --- |
| `W` and `W0` | -447, -424, -473 | 28, 28, 17 | 894, 879, 922 |
| `W` and `S` | -450, -430, -453 | 51, 30, 28 | 814, 802, 821 |
| `W` and `LF` | -457, -418, -448 | 44, 24, 22 | 875, 859, 862 |
| `W0` and `LF` | -5, 3, 26 | 394, 378, 428 | 342, 312, 310 |

## Limits

- The rates of the new line are those of the AM peak, and at that load the arms with no Express pod on the line are saturated.
  So the batch does not show the effect of the Express pods below saturation.
- The stream of Waterloo and Bank is 1/8 of the heavy stream.
- The wait of a hub party ends when its last part boards, and the arms with no Express service split the party.
- `W` does not pass the individual limits against any arm, because at least 17 matched background requests of each pair are over them.
