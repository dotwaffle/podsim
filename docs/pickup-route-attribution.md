# Pickup route and assignment attribution

Status: bounded Rail Hub study on `f3d0980`, October 3, 2026.
A private Go overlay observes the existing simulation.
The [measurement record](measurements/pickup-route-attribution.json) pins the sources, fixtures, outputs, and validation receipts.

## Result

Committed arrival maneuvers account for every failed candidate route probe in these four heavy arms.
No sampled candidate fails because it lacks a directed, class-compatible pickup route.
The probe uses the production diversion decision.
It does not remove resource commitments or change dispatch.

| Layout and seed | Committed arrival samples | Reachable samples | Other candidate causes |
| --- | ---: | ---: | ---: |
| Baseline, 1 | 24,070 | 210 | 0 |
| Independent banks, 1 | 9,845 | 42 | 0 |
| Baseline, 2 | 28,927 | 66 | 0 |
| Independent banks, 2 | 29,223 | 113 | 0 |

Each second, the observer probes each candidate pod for each origin with an unassigned order.
One pod can contribute samples at several origins in the same second.
These counts are origin-candidate observations, not fleet pod-seconds or request wait durations.
They exclude occupied and assigned pods before probing routes.
They test pickup-origin reachability, not complete request eligibility or seat fit.

The earlier [availability diagnosis](pickup-availability-diagnosis.md) shows that passenger travel and assigned empty pickups occupy almost all conditional fleet time.
This study separates the remaining failed candidate probes and binds assigned waits to observed assignment episodes.

## Assigned wait

The observer records the assigned pod, initial source berth, route version, route lanes, and planned remaining distance.
It counts each observed phase, lane, maneuver station, and stationary wait reason until assignment ends.

| Layout and seed | Completed / offered | Assigned movement or dwell, mean per accepted order | Assigned stationary hold, mean per accepted order |
| --- | ---: | ---: | ---: |
| Baseline, 1 | 363 / 1,440 | 213.162 s | 7.683 s |
| Independent banks, 1 | 365 / 1,440 | 209.982 s | 6.126 s |
| Baseline, 2 | 366 / 1,440 | 214.623 s | 7.731 s |
| Independent banks, 2 | 360 / 1,440 | 227.775 s | 6.200 s |

All accepted orders finish within the unchanged three-hour cap.
The queue limit remains 200, with 30 legacy pods and unchanged offers, speeds, and clearances.
Different accepted populations prevent these means from isolating a causal layout effect.
The earlier diagnosis retains the exact common-offer decomposition.

The baseline seed-2 run records 449 assignment episodes.
Only 105 end with the originally observed pod boarding that order.
Another 234 end when a different pod boards the order.
The remaining 110 end with an assignment change or release before boarding.
Native dispatch can release a remote pickup and board a local pod in the same tick.
The observer binds the boarding record to the pod that carries that rider.

The largest baseline seed-2 source-berth pair is `station-02-01` to `station-01`.
Its 41 observed episodes account for 7,626.85 assigned seconds, including 35.63 seconds of stationary holds.
The retained initial plans total 169,397.77 meters.
That total is planned distance across assignment starts, not measured empty travel.
Canceled assignments remain in these counts.

An episode can retain the same pod while its route changes.
Route lanes, version, and remaining distance describe the initial plan only.
Current lane and maneuver samples continue to describe each observed tick.
An origin-idle timestamp of `-1` means the observer saw no idle sample there.
Arrival and boarding can occur between samples, so that value does not prove a failed arrival.

## Validation

Both heavy seeds reproduce every prior result field and every passenger timing exactly in both layouts.
Seed 2 also compares each observed arm with a plain run on current sources.
Aggregate results and every request timing match exactly.
The revised light pilot completes 12 of 12 offers and passes the same plain-run comparison.
That pilot has no pending wait, so the heavy comparisons provide the route-probe evidence.

For every accepted order, pending phase ticks equal boarding tick minus submission tick.
Assigned episode phase ticks equal assigned pending ticks, including assignment changes and local substitutions.
Each closed episode's phase total equals its end tick minus its first observed tick.
Boarding IDs, ticks, and pod bindings reconcile with native records and live riders.
Fleet ticks equal 30 times the observed tick count.
These runs have no censored accepted orders and need no unresolved endpoint convention.

The four heavy arms perform 1,819,860 live safety checks and 100 planned physical restore probes.
All pass the existing separation, speed, contract, and restore checks.
The minimum observed separation is 13.9631 meters.
Saved-contract checks do not prove every live owner-map invariant.
Existing restore reservation-holder changes remain recorded in the raw evidence.

Focused tests compare state before and after observation, then compare measured and plain clone continuations for 60 ticks.
They cover current, strict, and disabled finishing-pod rules.
Additional tests cover assignment release, immediate boarding, committed arrivals, and same-tick local substitution.
The revised focused suite, race check, and vet pass.
Four compiled mutations fail assertions for omitted episodes, merged causes, shifted end ticks, and incorrect boarding attribution.
No build failure or panic counts as a mutation kill.

## Next comparison

Test more passenger service per fleet cycle on the baseline layout.
Use explicit shared consent, four-party drop-off sharing, and three intermediate stops.
Compare occupied pickups off and on with identical offers and consent.
Keep the private results as context, not a matched consent comparison.
Retain the existing [adoption gates](experimental-adoption.md), including skipped offers, individual regressions, and censored orders.
This attribution study qualifies no capacity increase or default change.
