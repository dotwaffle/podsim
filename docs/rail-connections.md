# Scheduled train connections

Use **Rail departures** in the editor to define outbound trains, weighted origins, request windows, and transfer times.
Select **Rail arrivals and departures** and enable passenger generation to use both plans.
This fixed-volume pattern disables the rate field.
Manual orders retain ordinary journey behavior.

## Project plan

The optional `railDepartures` member contains events such as:

```json
{
  "railDepartures": [
    {
      "id": "outbound-1",
      "station": "harbor",
      "atSeconds": 600,
      "walkingSeconds": 60,
      "requestFromSeconds": 120,
      "requestUntilSeconds": 300,
      "passengers": 120,
      "origins": [
        { "station": "market", "weight": 3 },
        { "station": "garden", "weight": 1 }
      ]
    }
  ],
  "demand": { "enabled": true, "perMinute": 2, "pattern": "rail-services", "seed": 1 }
}
```

Times are integer simulated seconds after reset.
Requests include both window endpoints and spread evenly through integer ticks.
For passenger index `i` from zero and count `n > 1`, the release tick is `start + floor(i * (end - start) / (n - 1))`.
One passenger requests at the window start.
Equal endpoints produce a burst.
Tick zero releases after the first physical simulation tick.
A valid window does not guarantee that a pod journey fits before the train departs.

Departure times range from 1 through 86,400 seconds.
Transfer times range from 0 through 3,600 seconds.
Require `0 <= requestFromSeconds <= requestUntilSeconds` and `requestUntilSeconds + walkingSeconds < atSeconds`.
Each event permits 1-200 passengers and 1-16 distinct passenger origins other than its hub.
Weights range from 1 through 1,000,000.
Event IDs contain 1-64 bytes and must be unique within each plan kind.
Arrival and departure events can share an ID.
Their random streams remain independent, and existing arrival destination choices do not change.

Combined plans permit 256 events, 10,000 passengers, and 200 offers at any normalized release tick.
Outbound plans also have a separate 3,000-passenger cap.
Arrival-only plans retain their 10,000-passenger cap.
The outbound cap keeps the conservative version 4 saved fixture at 83,301,572 bytes, below the existing 83,886,080-byte limit.
No file or stream cap increased.

The `rail-services` pattern releases offers after the physics of their exact tick.
Simultaneous arrivals precede departures, followed by authored event and passenger order.
Disabling generation stops future offers.
Re-enabling skips past releases.
Changing the seed or pattern retains issued outbound identities and outcomes until reset or project apply.
Undo, project import/export, and saved sessions retain the plans.
Deleting a station removes its hub events and origin references.
An event with no remaining origins is removed.

## Outcomes and restart

An accepted passenger is ready after actual alighting finishes, plus the event's transfer time.
Readiness at or before train departure counts as **made**.
An accepted passenger who is not ready counts as **missed**.
A rejected offer counts as **unserved**.
Before departure, accepted passengers remain **unresolved**, including passengers who have already alighted.
The demand inspector shows all four counts.
Its counts cover retained outbound offers since reset, independently of reconfigured Generated and Skipped counters.

A pending passenger whose order ends interrupted counts as **unserved**, with reason `interrupted`.
The session gives the interruption to the ledger before the departure of that tick counts, so the passenger does not count as missed.
A missed passenger whose order ends interrupted stays missed.
Missed passengers continue their pod journey.
The feature does not cancel, rebook, or prioritize their orders.
A generation switch does not stop existing connections from resolving.

Saved sessions and checkpoints retain the bounded connection ledger.
Physical restart keeps issued identities and actual alighting times without replaying requests.
The existing saved format resets moving-pod velocity and reconstructs track reservations.
Physical restore preserves saved positions, but does not promise identical future trajectories.
A pending passenger with unknown alighting time becomes unserved when restore explicitly drops the journey or logical restore auto-completes it.
The reasons are `restore-drop` and `restore-degraded`.
Known alighting times and terminal outcomes remain unchanged.
The loader rejects inconsistent counts, identities, bindings, or plans.
It does not infer arrival when a request disappears.

The ledger is an optional session-save member and does not change physical simulation or request formats.
Version 2, 3, and 4 loading remains supported.
Saved demand and recurring state omit connection counts when all four values are zero.
Existing arrival-only and ordinary demand retain their output.

## Comparison and forecast control

```sh
mise run compare -- -project rail-project.json -pattern rail-services -duration 3h -arrivals-for 1h -format json
mise run compare -- -project rail-project.json -pattern rail-services -duration 3h -arrivals-for 1h -rail-forecast -format json
```

The half-open offer window excludes releases exactly at its end.
The original run cap includes departure scoring after its final physical step.
With `-stop-when-drained`, issued connections keep the run open through deadlines within that cap.
Later deadlines remain unresolved.
The command never extends the cap to resolve them.
Existing arrival-only comparison timing and hashes remain unchanged.

JSON adds optional `rail_connections` with four counts and ordered `offers`.
Each offer contains `event`, `passenger`, `requestedTick`, `from`, `to`, `requestID`, `alightedTick`, `outcome`, and optional `reason`.
An unknown alighting tick is `-1`.
A rejected offer has request ID zero.
Restore failures retain the original positive request ID.
Service hashes include kind, event, passenger, release, endpoints, departure time, and transfer time.
Skipped indices retain rejected identities.
CSV and table columns remain unchanged, so detailed connection results require JSON.

Before a matrix starts, preflight limits retained skipped indices and outbound records to 1,000,000 each.
It also applies a 256 MiB conservative retained-storage budget.
Charges are 1,024 bytes per outcome, eight per skipped index, and 512 per scheduled offer.
The calculation includes full allocated capacities and every arm copy, including shared schedules.
This bounds charged retained report storage, not total simulation heap or peak JSON memory.

`-rail-forecast` is an explicit comparison control for known rail origins within the next 300 simulated seconds.
Record the flag in the invocation or measurement manifest.
The report does not add a separate forecast field.
The controller checks every five simulated seconds and can start at most one empty move.
It tries at most three targets, source searches, and complete route starts.
It reserves neither passengers nor pods.

Targets seek at most four idle or incoming unassigned pods.
The controller leaves one free target berth and the last idle pod at each passenger source.
All rebalancing moves count toward two incoming pods per target and `max(1, fleetSize / 10)` globally.
Unassigned orders or a working fleet share above 40% stop new moves.
An actual selected route must fit its remaining lead time under free-flow cost.
That estimate does not guarantee timely arrival.
Normal route claims, movement priorities, clearance, and physical restore still apply.
Existing empty moves finish when forecast control stops.

Live/editor exposure requires a useful candidate that passes the selected service and cost gates.
The comparison flag does not change project defaults or enable live forecasting.

## Mixed-service forecast screen

The [service feasibility study](rail-service-feasibility.md) tests lighter demand and longer transfer leads, plus a rejected station-bank layout.
Its results are separate from the fixed heavy-demand forecast comparison below.

The October 1 screen used six inbound trains and six outbound trains at Rail Hub and LondonFull's Paddington.
Each event contained 120 passengers, for 1,440 offers and 720 outbound connections per arm.
Inbound releases occurred at seconds 330, 930, 1,530, 2,130, 2,730, and 3,330.
Outbound windows ran from seconds 360-600, then repeated every 600 seconds.
Trains departed 300 seconds after each window ended, with a 60-second transfer time.
The last offer occurred at second 3,600.

Both forecast arms retained every offered identity, endpoint, release time, seed, and original limit.
The half-open window ended at tick 216,001, and the recovery cap remained three hours.
Each fixture used seeds 1 and 2, a 200-order queue, free-flow routing, and one party per pod.
Redistribution, station buffers, and pickup reassignment were off.
Rail Hub retained 30 pods without platoons.
LondonFull retained its existing 287-pod fleet and virtual platoons.
No station, pod, or seed was excluded.

An earlier short-lead pilot produced zero moves and identical results.
The final workload gave every event five additional minutes of advance notice before demand started.
Both arms used that same authored timing, fleet, and cap.
The earlier result remains in the evidence cache.

| Fixture | Seed | Completed, baseline / forecast | Skipped, baseline / forecast | Forecast moves | Baseline completions lost to skips |
| --- | --- | --- | --- | --- | --- |
| Rail Hub | 1 | 363 / 364 | 1,077 / 1,076 | 7 | 52 |
| Rail Hub | 2 | 366 / 367 | 1,074 / 1,073 | 7 | 56 |
| LondonFull | 1 | 616 / 616 | 824 / 824 | 0 | 0 |
| LondonFull | 2 | 624 / 624 | 816 / 816 | 0 | 0 |

Every accepted journey finished within the original cap.
The changed Rail Hub arms completed one extra passenger each, but rejected 52 and 56 passengers whom their baselines completed.
They exceeded individual pickup limits for two and seven matched passengers, and journey limits for two and five.
Maximum matched pickup increases were 326.6 and 909.5 seconds.
Mean wait and journey gains remained below the required 5% benefit threshold.
Both changed pairs fail the [adoption gates](experimental-adoption.md).

Every outbound record resolved in these overloaded workloads, but no passenger made a train connection.
The counts retain rejected offers separately from accepted passengers who missed:

| Fixture | Seed | Made | Missed, baseline / forecast | Unserved, baseline / forecast |
| --- | --- | --- | --- | --- |
| Rail Hub | 1 | 0 | 151 / 152 | 569 / 568 |
| Rail Hub | 2 | 0 | 151 / 158 | 569 / 562 |
| LondonFull | 1 | 0 | 279 / 279 | 441 / 441 |
| LondonFull | 2 | 0 | 274 / 274 | 446 / 446 |

The LondonFull arms remain identical and provide no forecast benefit.
A separate pre-release diagnostic sampled 65 checks and 160 nearest-source searches.
None found an eligible source within the remaining lead time, so none started a route.
This describes those sampled states, not every possible LondonFull workload.

The observation-only study checked separation, berth use, and lane speed after all 3,666,540 simulation ticks.
It checked the saved-state contract each simulated second and tested physical restore at five-minute intervals.
Restore probes retained exact discrete saved values and berth occupants, with at most 1e-7-meter tolerance for distance reconstruction.
Separate restored continuations checked safety, speed, completion receipts, and conservation.
They did not claim identical trajectories after velocity and reservation reconstruction.
Clone continuations retained exact state parity.

All 11,520 offers and 5,760 connection records reconcile across the eight arms.
The analysis retains skipped identities, individual limit failures, and censored timing observations.
The [measurement record](measurements/rail-connections.json) records source hashes, limits, outcomes, and validation scope.
These selected workloads are not a full qualification envelope or a sustained capacity study.
Their failures stop isolated cost studies, broader qualification, and live forecast exposure for this candidate.
The comparison control remains available.
Defaults and safety rules stay unchanged.
