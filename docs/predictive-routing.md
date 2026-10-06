# Predictive route-assignment experiment

The `predictive` comparison policy adds observed queues and near-term planned lane arrivals to route costs.
It remains experimental, with free-flow routing retained by default.
The project editor and live controls do not select this policy.

Run a matched comparison with `-routing-policies free-flow,predictive` in `cmd/compare`.
The simulation API also accepts `sim.PredictiveRouting` through `SetRoutingPolicy`.
Existing dispatch estimates, berth selection, positioning decisions, and parking selection still use free-flow costs.
The policy changes assigned routes and eligible uncommitted suffixes.
It preserves committed track, intermediate-berth avoidance, authored-layout fallback, and the shared-ride detour checks.
It does not reroute pods when they reach a junction.

## Forecast model

Each observed stopped pod contributes the existing three-second queue headway.
The policy tracks a five-second exponential moving average in simulated time.
It retains each pod's contributions by lane, so an assignment can exclude that pod's current and historical congestion.
The larger of current and smoothed queue discharge initializes the lane forecast.
The history updates at most once per queried tick, using the elapsed ticks.
A queue can therefore retain a modeled delay after its actual pods move.

Every assignment rebuilds planned lane-entry events from other pods' current routes.
Traveling pods contribute future entries, excluding their current lane.
Boarding, continuing, and departing empty pods also contribute their assigned route, after their known phase delay.
This includes routes assigned earlier in the same tick.
The forecast stops adding arrivals after 90 seconds.
It can retain known discharge beyond that horizon, but cannot predict unknown later arrivals.

Remaining-route estimates use lane lengths and speed limits.
They omit acceleration, unknown upstream admission delays, and future passenger legs that have no assigned route.
These events are planned arrivals, not granted admissions or guaranteed arrival times.

Each lane processes its sorted arrivals with a three-second headway after the later of arrival time and previous discharge.
A candidate waits for earlier planned arrivals and conservatively includes simultaneous events.
One frozen forecast evaluates both routes and the route search.
Its lane exit-time function never decreases with entry time, preserving the Dijkstra search assumption.
Moving diversions include free-flow travel along the retained prefix before evaluating the replacement suffix.

## Selection and state

A changed route must save at least the larger of 15 seconds and 5% of the modeled baseline route cost.
Its free-flow lane time must be at most 1.2 times the baseline route's time.
Otherwise the pod retains the free-flow route.
These guards bound modeled changes, but cannot guarantee an actual service improvement.
Reservations, junction arbitration, braking, and physical clearance remain authoritative.

The simulation owns the delay history, and clones copy it independently.
A reset or routing-policy change clears it.
The file format does not save this experimental history or routing selection.
A file restart preserves physical state but does not promise identical future experimental route choices.
No project, command, WebSocket, or saved-state fields change.

## Validation boundary

Focused tests cover forecast boundaries, monotone exit times, exhaustive small-graph route costs, self exclusion, and same-tick route updates.
They also cover fleet-order independence, clone storage and continuation, settings reset, berth fallback, committed-prefix diversion, and sharing detour guards.
The [selected service comparisons](experimental-adoption.md#rejected-candidates) retain identical outcomes with zero returned alternatives across six matched pairs.
The [isolated cost comparison](predictive-cost.md) adds 1.9% to 2.7% median CPU with prediction enabled and GC 100 in its two workloads.
These measurements do not establish an adoption envelope or a live playback-speed limit.
This implementation does not establish greater London capacity or faster actual routes.
See the [existing routing qualification](qualification.md#queue-routing-screen) and [proposed adoption gates](experimental-adoption.md).
