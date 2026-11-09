# Positioning under load proposal

Status: drafted on November 9, 2026.
The maintainer approved the pilot of A and B on November 9, 2026.
No simulation change on `main` comes from the pilot.

## Problem

The [Express London adoption screen](express-london-adoption-screen-proposal.md) fails the individual wait and journey limits in each pair of `D` against `LF`, although `D` serves 85 to 92 percent of the matched requests faster.
The studies of that screen trace the failures to the place of the idle pods:

- In `D`, about 180 Compact pods are idle at most times, but the mean count within 3 km of Charing Cross is 1.1, against 133 at more than 10 km.
  43 percent of the background requests start within 3 km.
- A failing request gets a pod from a median 3.7 km away, against 1.0 km in `LF`.
- [Guarded positioning](../model.md#redistribution) made no move in `D` or `LF`.
  Its gate was open in 2 percent of the samples of `D`, because a waiting trip had no pod, or more than 40 percent of the fleet worked.
  A trip that dispatch holds for a finishing pod has no pod, so the hold also keeps the gate closed.
- Guarded positioning never moves a lone idle pod at a passenger station, and a move has a route cost of at most 180 s.

So no current policy moves an idle pod toward demand when the fleet has spare pods but is not at a low load.
`D` has this state because Express pods carry the hub parties, but any project with spare pods far from its demand can have it.

## Earlier decisions

A new policy must not repeat these results:

- The removed weighted redistribution policy cut the wait at 1 request a minute by 33 to 71 percent, but it multiplied the empty distance by 2.3 to 15.0 and lowered the AM peak and Night limits by one rate ([qualification](../studies/qualification.md)).
- Rail forecast positioning and the shared forecast screen failed the gates, and the forecast control was removed on November 5, 2026.
  The record says not to repeat that matrix without a new, concrete forecast hypothesis.
- The [pickup supply](../studies/pickup-supply.md) study moved parked pods to passenger berths by origin share, and each changed fleet failed the individual limits.
- The [policy failures](../studies/policy-failures.md) traces show that a pod that moves empty toward a station is not free for a later request elsewhere, so each move has a cost.
- Guarded positioning is on by default by a maintainer exception of November 6, 2026, with no gate qualification.

The [adoption gates](../design/experimental-adoption.md#numerical-limits) apply to any new policy, with all London bands, seeds 1 to 10, the default and the study queue limits, Acton, and the rail hub.
A pass does not change a default without a separate approval.

## Options

| Option | Change | For | Against |
| --- | --- | --- | --- |
| A. Wider guarded gate | Keep guarded positioning, but count a held trip as a trip with a pod, replace the 40 percent working limit with a minimum number of idle pods, and let a lone idle pod at a station with less than the mean weight move. Keep the 180 s reach. | Small change to a policy that already has a sweep, a restore rule, and a deadlock fix. | A route of 180 s is short, and the idle pods of `D` are mostly more than 10 km from the center. It can move pods in steps only if each step finds a deficit station. |
| B. Surplus return | A new check, each 30 s, while at least a fixed share of the fleet is idle. It moves one idle pod from a station with less than the mean weight to the deficit station with the highest weight in a longer reach, for example 600 s. Each move leaves the idle count above the minimum. | Moves the pods that `D` leaves at the edge, and stops when spare pods run out, so it does not act at saturation. | New code, new parameters, and new saved state. It risks the empty distance of the weighted policy, so it needs a cap on moves. |
| C. Return at trip end | When a pod becomes idle at a station with less than the mean weight, and no request waits within its reach, it goes to the nearest deficit station within 300 s. | One decision for each pod, at the time when it becomes idle, with no global scan. | Moves each pod after each trip, so the empty distance can grow most of the three. It acts also at saturation, where a pod finishing a trip is soon needed nearby. |

The recommendation is a pilot of A and B before any implementation.
A is the smallest change, and the pilot shows if its 180 s reach is the limit.
B is the option that matches the cause of the studies, and its move cap and minimum idle count answer the result of the weighted policy.
C is not recommended, because it acts at saturation and it has no cap.

## Pilot

The pilot is a scratch study with no commit to `internal/sim`.
It has at most 6 arms and 3 seeds, and one arm runs first to time it.

- Arms: `LF` and `D` with the current policy, `D` with A, `D` with B at a reach of 600 s, and `LF` with B, because B is a shared policy.
- Workload: the screen workload, `pm-peak` seeds 1 to 3.
- Readings: the counts over the individual limits against `LF` with the current policy, the mean and p95 waits, the empty distance, the positioning moves, and the idle pods by distance from the center.

The pilot result does not adopt anything.
If A or B lowers the failures with an empty distance that the gates can accept, a second proposal gives the implementation, its restore rule, its tests, and the full matrix of the adoption gates.

## Restore and determinism

Guarded positioning keeps `RebalanceMoves` in the saved state and `RebalanceAfter` for each pod.
Option A needs no new saved state.
Option B needs a saved count of its moves, or it must read the existing count, and a restore must give the same next check.
Each option must skip a withdrawn pod as `guardedSupply` and `guardedCandidates` do.

## Decisions for the maintainer

1. Approve the pilot of A and B, or choose other arms.
2. Confirm that the pilot is a scratch study with no change to `internal/sim` on `main`.
3. Confirm that any adoption then needs the full gates, and does not use the screen alone.

## Pilot result

The pilot ran on November 9, 2026, from `main` at `8c77fc2b` with the scratch code on the branch `study/loaded-positioning` at `4937a13a`.
Option A is as above, with a minimum of a tenth of the fleet idle.
Option B checks each 30 s, acts while at least a fifth of the fleet is idle, keeps more than a tenth idle, and moves no Express pod.
Each arm ran the screen workload of `pm-peak` with the oracle at each simulated second, and no arm had a safety or accounting failure or an unfinished order.
The `D` arm gives the same counts as the screen, so the scratch code changes nothing with the pilot off.

The counts over the limits compare each arm with `LF` on the matched background requests.

| Arm | Seed | Wait over | Journey over | Mean wait (s) | p95 wait (s) | Empty distance (km) | Moves | Idle pods within 3 km |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `D` | 1 | 27 | 15 | 182 | 440 | 4,733 | 1 | 1.3 |
| `D` + A | 1 | 19 | 15 | 179 | 459 | 4,684 | 26 | 1.4 |
| `D` + B | 1 | 9 | 7 | 146 | 378 | 4,867 | 254 | 2.8 |
| `D` | 2 | 33 | 27 | 172 | 426 | 4,181 | 4 | 1.1 |
| `D` + A | 2 | 32 | 24 | 167 | 429 | 4,109 | 26 | 1.0 |
| `D` + B | 2 | 16 | 11 | 122 | 311 | 4,167 | 262 | 4.1 |
| `D` | 3 | 32 | 26 | 194 | 468 | 4,587 | 0 | 0.6 |
| `D` + A | 3 | 36 | 27 | 190 | 468 | 4,527 | 12 | 0.6 |
| `D` + B | 3 | 17 | 10 | 159 | 390 | 4,866 | 265 | 1.6 |

`LF` with B against `LF`:

| Seed | Matched | Wait over | Journey over | Mean wait (s), `LF` and `LF` + B | Refused, `LF` and `LF` + B | Moves |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | 1,057 | 433 | 359 | 696, 718 | 217, 220 | 68 |
| 2 | 1,068 | 441 | 370 | 723, 739 | 223, 238 | 59 |
| 3 | 1,031 | 355 | 308 | 734, 714 | 287, 268 | 61 |

- Option A changes little.
  It makes 12 to 26 moves, and the failures change by -8 to +4 in each seed.
  The 180 s reach limits it, as expected.
- Option B halves the failures of `D` and lowers its mean wait by 18 to 29 percent, with -0.3 to +6.1 percent of empty distance.
  It still leaves 9 to 17 requests over the wait limit in each seed, so `D` with B still fails the screen.
  Most idle pods stay more than 10 km from the center, because they are more than 600 s from a deficit station.
- Option B harms `LF`.
  `LF` has spare pods only in the first minutes, and B makes about 60 moves then.
  These change the later service of the saturated fleet, so 355 to 441 requests go over the wait limit, and the mean wait changes by -2.7 to +3.2 percent.

So neither option is a candidate for adoption as it is.
B helps a fleet with spare pods and harms a saturated one, and the adoption gates apply it to each project.
A next step can be a B that acts only on a sustained surplus, for example after the idle share stays above the limit for some minutes, with a new pilot.
