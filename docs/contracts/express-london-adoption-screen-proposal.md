# Express London adoption screen proposal

Status: drafted and approved on November 8, 2026.
The maintainer approved decisions 1 to 6 as recommended, with runner option A.
The maintainer also approved the build of the runner and the timed pilot.
The full screen waits for the maintainer after the pilot.

## Scope

Decision 1 of the [Express London qualification proposal](express-london-qualification-proposal.md) asks for the adoption comparison of project D as a separate proposal, inside the study brief cap of 6 arms and 3 seeds.
The [adoption gates](../design/experimental-adoption.md#evidence-required) need all six London bands, seeds 1 to 10, two queue limits, and isolated policies.
The study plan estimated that full matrix at 84 to 88 days of serial time before optimization.
A screen of 6 arms and 3 seeds cannot meet these gates, and the adoption document says that a screen cannot replace this coverage.

So this proposal is a screen with two possible results:

- A failure of a screen gate stops the work on D adoption, with no full matrix.
- A pass justifies a separate proposal for the full matrix, with a measured cost.

The screen does not adopt D, change a default, or change ordinary LondonFull.

## Configurations

| Name | Project |
| --- | --- |
| `LF` | `london-full`, the baseline. |
| `D0` | `london-full-express` without its 16 Express pods and Express services. It is a diagnostic control for the geometry. |
| `D` | `london-full-express`. |

The comparison of `D` against `LF` decides each gate.
The comparison against `D0` only explains a result, and it cannot hide harm against `LF`.
Redistribution and pickup reassignment stay on, as in the current defaults.
The screen does not isolate the policies.

## Arms and seeds

| Arm | Configuration | Workload |
| --- | --- | --- |
| 1 | `LF` | Busy band |
| 2 | `D0` | Busy band |
| 3 | `D` | Busy band |
| 4 | `LF` | Quiet band |
| 5 | `D0` | Quiet band |
| 6 | `D` | Quiet band |

Each arm runs seeds 1, 2, and 3, for 18 runs.
The busy band and the quiet band are the bands of `LondonFullDemand` with the most and the fewest observed journeys.
The runner prints both names before the pilot, and they do not change after it.

Each workload adds the hub stream of the D soak: shared hub parties of 1 to 20 passengers between Waterloo and Bank, one each 15 s on average, three in four from Waterloo.
That gives about 42 hub passengers a minute.
Every arm has the same offered events, seed for seed.

Each run offers events for 2 simulated hours, and then it runs without new events until all accepted requests finish, for at most 2 more hours.
The live queue limit is the default of 200.
The study limit of 10,000 is out of scope.

## Split-event mapping

`LF` and `D0` have no Express pod, so they cannot accept a hub party of more than 8 passengers (`sim.MaxNewPartySize`) as one order.
The qualification proposal deferred this mapping to this proposal.

The recommended mapping:

- Before admission, the runner splits a hub event into orders of at most 8 passengers.
  Each order keeps the identity of its event.
- The runner never splits an order that a session accepted.
- The wait of an event ends when its last passenger boards.
  Its journey ends when its last passenger arrives.
- An event with a refused or unfinished part is partial or censored.
  The report keeps it and counts it apart.
- The report gives request counts and passenger counts apart.

## Screen gates

A screen gate compares `D` with `LF` for the same seed and workload.

| Gate | Pass rule |
| --- | --- |
| Safety and accounting | No separation, speed, ownership, conservation, lost-order, or duplicate-order failure. The oracle runs at each simulated second. |
| Finite recovery | Each run finishes every accepted request inside the 2-hour clear. |
| Live queue loss | `D` refuses no offered event that `LF` accepts. |
| Background pickup wait | Each matched background request that boards in both runs waits at most max(30 s, 5% of its `LF` wait) longer in `D`. |
| Background journey | Each matched background request that finishes in both runs takes at most max(60 s, 5% of its `LF` journey) longer in `D`. |
| Hub benefit | `D` completes more hub passengers than `LF` in each seed and workload. |

These rules come from the [numerical limits](../design/experimental-adoption.md#numerical-limits) of the adoption gates.
The screen reports the aggregate service, per-direction hub results, censored events, and CPU per simulated hour, with no gate on them.
The sustained backlog gate needs 6-hour arrival runs, so it stays out of the screen.

The earlier [hub fixture screen](../studies/express-hub-fixture.md) failed the background pickup gate in seed 2.
The maintainer waived that failure for that screen only, and this screen gets no waiver.

## Runner

The study runner of the hub fixture is on the unmerged branch `study/express-fixture`, at `fa587966`, so its source does not match `main`.

| Option | For | Against |
| --- | --- | --- |
| A. Add the screen to `cmd/compare` | Reproducible from `main`, with source hashes, matched demand schedules, and existing tests. | The largest patch: hub parties, the split mapping, matched-request gates, and censored events are new in `cmd/compare`. |
| B. Rebase the study runner onto `main` | Less new code. | A tool outside the checked tools, with Python analysis, no tests, and no conservation check. |

The recommendation is option A, as one patch for the hub stream, and one for the split mapping and the matched-request report, each with tests.

## Pilot and budget

First, a timed pilot runs one seed of arms 1, 2, and 3 for 30 simulated minutes.
The pilot result gives the cost of the 18 runs.
If the projection exceeds 6 hours of wall time on this host, the screen stops and comes back with a smaller shape.
The runs use nice, a memory cap, and one study at a time.

## Decisions for the maintainer

1. Approve the screen as a stop or continue gate, and not an adoption pass.
2. Approve the six arms: `LF`, `D0`, and `D`, on the busy and the quiet band, with seeds 1 to 3.
3. Approve the split-event mapping above.
4. Approve the screen gates, with no inherited waiver.
5. Choose the runner: option A (recommended) or option B.
6. Approve the pilot rule and the 6-hour budget.
