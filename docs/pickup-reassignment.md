# Pickup reassignment research

Status: approved local experiment, disabled by default.
The project setting `pickupReassignment` enables the controller.
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

The live session was not changed.

## Existing behavior

`internal/sim/dispatch.go` considers requests in submission order.
It chooses an available pod using estimated pickup travel time.
An idle pod at the origin can replace a pod that is still traveling to that pickup.
The opt-in `reassign-existing` sharing policy can transfer a waiting party into a compatible boarding pod at its origin.
That policy is distinct from swapping two empty pickup pods.

`pickupCandidate` in `internal/sim/diversion.go` excludes a pod assigned to another waiting order.
The experimental controller below can reassign en-route pickups.
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

## Bounded experiment

The September 30 extension checks assigned pickups against eligible empty alternatives.
Alternatives include idle pods, parking moves, repositioning moves, and other assigned pickups.
An unassigned replacement must have a strictly faster predicted pickup.
A swap must predict strictly faster pickups for both requests.
The controller retains a 10-second minimum combined saving and a 30-second pod cooldown.
These limits reduce small repeated route changes.

The controller checks each new remote pickup assignment and runs periodically before track admission.
Periodic checks run at most once each simulated second.
Both triggers share a budget of 256 examined fleet pairs and eight route pairs per simulated second.
Each trigger commits at most one change.
The periodic pair cursor and assignment alternative cursor advance in fleet order.
The budgets bound attempts, not CPU time.
Route-search cost still depends on the network and cache state.
Only free-flow routing supports the experiment.
Congestion and queue routing retain existing dispatch behavior.

For each candidate:

1. Check the diversion frontiers and exclude incompatible commitments.
2. Prepare replacement routes before changing assignments.
3. Compare each request's old pod with that request's replacement pod.
4. Require strictly faster predicted pickups and the minimum saving.
5. Apply routes and assignments in deterministic order.

Occupied or linked pods cannot take replacements.
Assigned pickups must have one valid binding and matching pickup targets.
Released and rebalancing pods cannot participate as assigned pickups.
An upstream buffer member can divert before it reserves the station entry lane.
A member committed to that entry keeps its queue position.
Estimates extend a berthless route to a reachable berth, so entry-only and berth-ending costs are not compared directly.
Future traffic and queue delays remain unknown.

A swap prepares both routes before either redirect commits.
An idle replacement prepares its departure before the old pickup is released.
Moving replacements preserve every reserved lane, position, speed, and origin ownership.
A released old pickup uses the ordinary parking controller.
The redirect retains admission wait age only when the complete pending resource group stays the same.
Changed groups start a new wait on their next admission attempt.
Each request keeps its ID, request time, queue position, and sharing census flags.
The controller clears cached onward routes and obsolete deferral fields.

Reset keeps enablement but clears cursors, budgets, cooldowns, and counters.
Clone copies controller storage.
File restore keeps valid routes and bindings but disables the policy until its owner configures it.
Controller history is not saved, so restart does not promise identical future experimental decisions.
Project files and WebSocket frames do not change in this controller slice.

Counters include assignment checks, transfers, swaps, route attempts, and rejection reasons.
With experiment recording enabled, each changed request records its old and new pod and predicted pickup time.
These records can be joined to actual boarding and completion records by request ID.
Normal server sessions retain no decision history.
The September 30 extension requires new matched-request and sustained-load qualification.
The measurements below describe the earlier periodic assigned-pair controller.

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

The first LondonFull comparison used seed 1, AM peak demand at 10 requests per minute, and 30 minutes of arrivals.
Both modes completed 298 of 299 orders within one simulated hour.
Their complete comparison results matched, including mean wait, p95 wait, and empty distance.
The enabled controller evaluated 928 pairs and made no swaps.
The instrumented arms took 14.08 seconds disabled and 14.39 seconds enabled.
They include once-per-second safety and order checks and ran alongside another study.
These times do not establish a production playback limit or a reliable overhead estimate.
A second comparison used two seeds, 20 requests per minute, one hour of arrivals, and a two-hour observation cap.
Each pair used the same demand schedule and passed once-per-second safety and order-accounting checks.

| Seed | Completed, off/on | Mean wait, off/on | p95 wait, off/on | Empty distance change |
| --- | --- | --- | --- | --- |
| 1 | 1184/1186 of 1199 | 785.5/716.0 s | 2176.0/1959.3 s | -6.6% |
| 2 | 1181/1187 of 1199 | 813.9/764.8 s | 2172.5/1952.4 s | -6.7% |

Both enabled arms reduced mean and p95 pickup wait.
Neither mode completed every order by the cap.
Seed 1 maximum request-to-alight time increased from 4900.0 to 5208.0 seconds.
This comparison does not establish a no-harm envelope or sustained capacity.
The instrumented enabled arms took more wall time, but concurrent work prevents a reliable production overhead estimate.
The [six-hour load screen](pickup-swap-sustained.md) compares two seeds at 15 and 20 requests per minute.
It improves mean and p95 wait, but increases maximum journey time in both seed 2 cases.
The controller remains disabled by default.
The [post-routing tail traces](experimental-adoption.md#rejected-candidates) retain selected individual regressions without identifying a new movement defect.
The [expanded controller screen](experimental-adoption.md#rejected-candidates) improves selected Acton throughput with reassignment alone, but retains individual wait increases.
Broader individual-service coverage and controlled CPU overhead remain qualification work.
