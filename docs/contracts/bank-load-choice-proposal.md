# Bank load choice proposal

Status: drafted on November 8, 2026.
The maintainer chose option A with a margin of 60 s on November 8, 2026, and confirmed the margin after the pilot.

## Problem

The [study of the slow requests](express-london-adoption-screen-proposal.md#study-of-the-slow-requests) of the Express London screen found that Bank causes most of the failures of the per-request gates.
In `D`, 20.6 percent of the requests from Bank are over the wait limit, against 1.9 percent from the other stations.
Express pods hold berths `01` and `02` of bank `a` of Bank 39 and 38 percent of the time, and bank `b` is in use less than 1 percent of the time.
An approach takes the bank with the lowest free-flow approach cost (`stationBankEntryOn`), so bank `b` gets almost no pods.
The [station banks](../station-banks.md) rule uses berth load only to break an exact tie of cost, so a busy bank wins over an empty bank that costs a little more.

## Scope

The change is in `stationBankEntryOn`, which chooses the bank entry for a pickup, a trip, a detour, and a free-berth choice.
A fixed destination berth keeps its bank, as now.
The presets with banks are `london-full-express` and `independent-banks`, and the bank tests also use the rule.
Each route that the rule gives still uses the bank routing and the checks that it has now.

## Options

The load of a bank is the minimum `berthLoad` of its berths that the class and the filter accept, as in the tie break now.
`berthLoad` counts the moving pods that go to the berth, the waiting trips for it, and the owners of the berth and its node.

| Option | Rule | For | Against |
| --- | --- | --- | --- |
| A | Among the banks with a cost of at most the lowest cost plus a margin, the lowest load wins, then the lowest cost, then the bank order. | Keeps the near bank when it is free, and uses a second bank only when it is near. Easy to test at the margin. | Adds a margin to choose and to keep in the station banks rule. |
| B | The lowest load wins, then the lowest cost. | No new parameter. | Can send a pod to a far bank with one pod less, which costs more than the wait. |
| C | The lowest sum of cost and load times a fixed time per pod wins. | One score for each bank. | The time per pod depends on the dwell of the class, so one value fits Express and Compact badly. |

The recommendation is option A, with a margin of 60 s of free-flow time, the journey floor of the adoption limits.
The pilot below measures the cost difference between the banks of Bank and Waterloo, and the margin comes back for approval if 60 s does not cover it.

An idle pod at a berth with no claim does not count, because the blocker clear moves it when another pod needs the berth.
A pod that unloads or boards at a berth counts, because it is not idle.

## Evidence

1. Unit tests of the rule at the margin: a bank just inside the margin with a lower load wins, a bank just outside does not, and equal loads keep the lowest cost.
   Each test fails on the current rule.
2. The bank route parity tests and the station bank tests, with any change of their expected choices explained.
3. A pilot: the free-flow cost of each bank of Bank and Waterloo from the stations next to them.
4. The full Express London screen, the routine soak, the CI soak, and `test:race:sim`.

The screen result is for information.
It does not change the stop on D adoption by itself, and a new adoption step needs its own decision.

## Pilot

The pilot measured the free-flow approach cost of each bank on the `D` network at `0dafa9a9`, from the exit of each passenger station, with the cost rule of `stationBankEntryOn`.
The gap is the cost of the dearer bank less the cost of the cheaper bank.

| Hub | Class | Origins | Wins of `a` and `b` | Gap p25, p50, p75, max (s) | Gaps of at most 60 s |
| --- | --- | --- | --- | --- | --- |
| Bank | Compact | 308 | 175 and 133 | 18.6, 47.9, 77.8, 80.5 | 195 |
| Bank | Express | 1 (Waterloo) | 1 and 0 | 26.1 | 1 |
| Waterloo | Compact | 308 | 190 and 118 | 76.3, 76.3, 79.6, 87.8 | 3 |
| Waterloo | Express | 1 (Bank) | 0 and 1 | 16.0 | 1 |

A margin of 60 s covers the Express approach to Bank, where Express pods hold bank `a`.
It does not cover the 3 nearest Compact origins of Bank (Cannon Street, Mansion House, and St. Paul's), with gaps of 74.5 s to 80.5 s, or almost any Compact origin of Waterloo.
A margin of 90 s covers each origin of both hubs, but then a load difference of 1 can add up to 88 s of travel.

## Decisions for the maintainer

1. Choose an option: A (recommended), B, C, or no change.
2. For option A, approve a margin of 60 s, to confirm after the pilot.
3. Approve the change of the bank rule in `docs/station-banks.md` that the option needs.
