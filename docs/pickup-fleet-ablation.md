# Pickup swaps and later fleet availability

An early pickup swap changes later assignments, berth choices, and parking destinations.
Suppressing that one swap improves four selected tails relative to normal swaps.
One tail still regresses against swaps off.
This intervention does not establish a general dispatch fix or justify enabling swaps by default.

## Exact history and intervention

The study repeats LondonFull AM12 seed 4 with six hours of arrivals and a seven-hour cutoff.
It uses frozen production source `e285e69` and the existing 287-pod project.
Sharing, buffers, and positioning stay off.
Routing uses free-flow costs and virtual platoons allow four pods.
All three arms accept and complete the same 4,319 requests.

The observer records assignment, activity, route, and admission-pending-index changes on every tick for every pod.
Selected pods also record lane and wait changes.
It copies route IDs and riders, and purity and ownership tests pass.
The original off/on arms reproduce the historical timing, pending, safety, and restore outputs exactly.
Their changed focus station explains the different focus-specific result fields.

Off and on histories first diverge at tick 74,761, about 1,246 seconds into the run.
Normal swaps exchange requests 136 and 239 between pods 047 and 107.
The intervention suppresses only that request pair and leaves all other decisions enabled.
It suppresses one pair check and preserves the exact observed prefix through tick 74,760.
This is a causal policy intervention, not a passive observer.

## Selected pickup waits

| Request | Swaps off, seconds | Normal swaps, seconds | Suppress pair 136/239, seconds |
| ---: | ---: | ---: | ---: |
| 1324 | 446.30 | 664.10 | 248.48 |
| 3651 | 1,191.95 | 2,821.68 | 2,360.80 |
| 2175 | 557.63 | 2,115.52 | 557.63 |
| 3421 | 0.00 | 1,453.35 | 0.00 |

Requests 1324, 3651, and 2175 are not directly swapped in the normal arm.
Their pickup delays follow earlier changes in fleet availability.
For example, pod 112 first acquires a different Blackfriars berth while carrying earlier request 347.
Other selected pods diverge on earlier pickup assignments or parking moves.
The histories narrow the chain to earlier fleet decisions, but do not identify one faulty local decision.

Request 3421 has a direct later swap, but first loses a local baseline pod.
With swaps off, pod 223 boards it immediately at Rickmansworth.
With normal swaps, pod 113 travels from Oxford Circus before pod 235 takes the pickup.
That later swap improves its predicted remaining pickup time from about 1,223 to 539 seconds.
The local improvement does not recover the original immediate pickup.

Suppressing the first pair restores the baseline waits for requests 2175 and 3421 and improves request 1324 further.
Request 3651 improves relative to normal swaps but remains 1,168.85 seconds worse than swaps off.
The intervention still performs later swaps, starting with requests 239 and 135 at tick 75,241.
It does not isolate every later change or support banning this specific pair in production.

## Whole workload and checks

Pickup-wait P95 is 1,116.47 seconds with swaps off, 1,095.20 with normal swaps, and 1,092.12 under the intervention.
Journey P95 is 2,513.50, 2,518.67, and 2,508.68 seconds respectively.
All arms drain, but these aggregate results do not remove individual regressions.

All primary arms retain once-per-second safety, speed, and unique-order checks.
Physical restores retain poses, bindings, admission ages, and saved platoon records.
Each restored copy passes 60 seconds of dense continuation checks.
Short pilots check every tick and match production aggregates.
Concurrent functional runs do not provide CPU comparisons.

[Measurements](measurements/pickup-fleet-ablation.json) retain selected timings, boarding histories, the intervention counter, checks, and source identities.
Raw histories remain in `~/.cache/agents/podsim/fleet-history-20261001/` and `pickup-first-swap-ablation-20261001/`.
In history events, `Pending` names an admission reservation index, not a waiting request.
Recorded positions are event-time observations and must not be interpolated as exact later positions.

The selected intervention establishes sensitivity to an upstream swap.
A general policy change would need matched workload gains and acceptable individual delays across broader cells.
