# Pickup reassignment research

Status: research and proposed follow-up, not an approved dispatch change.
Evidence captured on September 29, 2026.

## Observed problem

The live LondonFull session had Pod01 traveling from Whitechapel toward a pickup at West Ruislip.
At tick 752776, project revision 16, its assigned order was 2423, from West Ruislip to Archway.
That order had already waited about 7.3 simulated minutes.
The snapshot had 200 pending orders, 165 with a pod assigned, and no idle pods.

A directed-route screening found nine possible swaps that reduced the estimated remaining travel time for both pickups.
The largest estimated combined saving paired Pod01 with Pod25, which had a pickup at Canary Wharf.

| Pickup | Current pod | Current remaining time | Swapped pod | Swapped remaining time |
| --- | --- | --- | --- | --- |
| West Ruislip | Pod01 | 31.6 min | Pod25 | 21.1 min |
| Canary Wharf | Pod25 | 18.4 min | Pod01 | 7.5 min |

These are free-flow estimates to station entries, not measured pickup times.
The calculation respects lane direction, speed limits, and curved lane geometry.
It excludes routes through station berths and excludes occupied or platooned candidate pods.
It compares the current route with a new route from the end of the current lane.
It does not have the internal reservation frontier, destination berth claims, or future traffic state.
Thus it identifies a candidate for an exact feasibility check, not a swap that can be applied as calculated.
The two API captures were sequential.
Their project revisions matched.

Raw state, project, analysis script, and candidate results are retained outside the repository in `~/.cache/agents/podsim/pickup-swap-20260929/`.
The live session was not changed.

## Existing behavior

`internal/sim/dispatch.go` considers requests in submission order.
It chooses an available pod using estimated pickup travel time.
An idle pod at the origin can replace a pod that is still traveling to that pickup.
The opt-in `reassign-existing` sharing policy can transfer a waiting party into a compatible boarding pod at its origin.
That policy is distinct from swapping two empty pickup pods.

`pickupCandidate` in `internal/sim/diversion.go` excludes a pod assigned to another waiting order.
There is no general reassignment of two en-route pickups.
`divertStart` already defines where a moving empty pod may change its route:

- Preserve every lane touched by reserved track.
- Do not change a committed berth inlet or parking arrival chain.
- Do not divert a pod in a platoon.

A swap must also transfer the two order assignments and update berth claims as one operation.
A failed second route must not leave the first order or pod changed.

## Prior art

### Elevator control

[Elevator Saga's documentation](https://play.elevatorsaga.com/documentation.html) allows destination-queue edits, events before passing a floor, and in-transit rescheduling.
It provides an environment for control algorithms, not one recommended optimal dispatch algorithm.

[Smith and Peters, ETD Algorithm with Destination Dispatch and Booster Options (2002)](https://www.joomla.peters-research.com/index.php/support/articles-and-papers/42-etd-algorithm-with-destination-dispatch-and-booster-options) compares estimated pickup time with estimated total passenger journey time.
Its assignment cost includes the delay imposed on passengers already served by that car.
This supports evaluating both affected orders, rather than sending the apparently closest pod to one request.

[Ruokokoski et al., Assignment formulation for the Elevator Dispatching Problem (2016)](https://www.sciencedirect.com/science/article/pii/S0377221716000485) distinguishes immediate fixed assignments from delayed assignments that can be revised as the system changes.
This is the relevant elevator analogy for Podsim.
A rule based only on travel direction does not transfer to a directed network with branches and reserved junctions.

### Taxi dispatch

[Billhardt et al., Taxi Dispatching Strategies with Compensations (2019)](https://arxiv.org/abs/2401.11553) is a closer match to the reported behavior.
It considers reassignment of taxis already traveling to pickups and evaluates globally improved assignments before boarding.
The paper also addresses compensation for independent drivers.
Podsim does not need that compensation mechanism because its fleet has one operator.
The publication is from 2019.
The linked manuscript was uploaded to arXiv in 2024.

### Personal rapid transit

[Lees-Miller and Wilson, Sampling of Redistribution of Empty Vehicles for Personal Rapid Transit (2011)](https://journals.sagepub.com/doi/10.3141/2216-19) separates reactive dispatch for known requests from proactive moves for predicted requests.
Its sampling-and-voting approach concerns proactive empty-vehicle positioning.
That is relevant to the broader fleet policy, but it does not replace a correction to existing pickup assignments.

## Recommended follow-up

Start with pairwise swaps of empty, assigned pickup pods.
This is smaller than periodic global matching and directly addresses the observed candidate.
Use an observation-only implementation first, then a disabled-by-default experiment if approved.

For each pair:

1. Check both diversion frontiers and exclude pods with incompatible commitments.
2. Build both replacement routes and berth claims without changing the simulation.
3. Estimate pickup times from the committed frontiers, not straight-line distance or heading.
4. Require neither pickup to become later, and require a useful reduction in total remaining pickup time.
5. Apply both assignments and routes together in deterministic order.

A minimum improvement and a reassignment cooldown should prevent repeated small changes.
Their values remain design decisions.
Do not let reassignment reset an order's original request time or queue priority.
Preserve passenger accounting and all track, berth, and platoon safety checks.

| Approach | Benefit | Limitation |
| --- | --- | --- |
| Pairwise swaps | Small change, direct explanation, can require both orders to benefit | Can miss improvements that need three or more pods |
| Periodic minimum-cost matching | Considers the eligible fleet and pending orders together | More route-cost work, more assignment changes, needs explicit fairness constraints |
| Predictive dispatch and repositioning | Can prepare for future demand | Depends on forecasts and addresses a broader problem |

Use pairwise swaps as the first experiment.
Consider global matching if observation shows that pairwise swaps leave substantial avoidable pickup travel.

## Validation and measurements

The saved HTTP snapshot cannot be loaded as a complete physical simulation state.
Build a reproducible fixture for the Pod01/Pod25 route pattern and capture a full saved state in a controlled replay.
Test one-way detours, reserved junctions, berth approach commitments, platoon membership, and rejected partial swaps.
Test repeated reassignment, restore, and deterministic replay with unchanged passenger demand.

Compare existing dispatch and the candidate with the same seeds and demand schedules.
Measure mean, p95, and maximum wait, request-to-alight time, empty distance, completed parties, queue recovery, and CPU cost.
Record candidate counts, rejection reasons, swaps, predicted savings, and realized pickup times.
A predicted saving for two orders is not proof of higher network capacity.
The experiment must also check later congestion and effects on other waiting parties.
