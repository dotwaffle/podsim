# Fleet histories behind selected policy failures

The remaining platoon and sharing witnesses change later pickup availability.
Some aggregate P95 regressions select a passenger whose own timing does not worsen.
The selected traces do not establish a safety or dispatch defect, or support a default change.

## Replay and observation limits

Ten selected arms repeat the [policy failure study](policy-failures.md) on frozen source `e285e69`.
They retain 30 minutes of arrivals, a 65-minute cap, and the original policy settings.
All accept their identical paired schedules and complete every request.
Result, schedule, accepted requests, timings, pending records, station reports, and all safety/restore counts match the earlier arms exactly.
The unused zero-valued forecast observer is absent from these free-flow replays.

Each tick records assignment, activity, route, and admission-pending-index changes for all pods.
Selected pods also record lane and wait changes.
These are event histories, not continuous pose records or full dispatcher decision logs.
A prior event's position must not be treated as a later exact position.
The `Pending` field is an admission reservation index, not a request ID.

All primary arms retain once-per-second safety, speed, and unique-order checks.
Physical restores at 15 and 30 minutes retain bindings, poses, admission ages, and saved platoon records.
Each restored copy passes 60 seconds of dense continuation checks.
Two short pilots retain every-tick checks and match production aggregates.

## Platoon recovery

London198 AM24 seed 2 ends at 3,588 seconds with platoons off and 3,767 seconds with virtual platoons.
The last completed request changes from 699 to 683.
Request 683 gains 292.47 seconds of pickup wait with identical ride duration.
Its boarding pod changes from 150 to 131.
Request 699 improves by 182.47 seconds.
The later cutoff failure is therefore a changed tail, not a common delay applied to all journeys.

Pod 131 first differs at tick 80,978 after unloading earlier request 349 at Green Park.
It travels empty toward Whitechapel in the off arm and Victoria in the virtual arm.
At request 683's arrival, it carries an earlier passenger in the virtual arm.
Pod 150 also has a different earlier empty destination.
These changes precede the tail assignment and explain why a different pickup pod is available later.
They do not isolate a single platoon rule or justify disabling a safety constraint.

## Sharing pickup chains

Interpeak11 seed 3's candidate P95 request is 211, from Tower Hill to Finchley Road.
Pod 090 carries it in both arms with identical ride duration and 40.87 seconds of extra pickup wait.
Its prior passenger 104 completes at tick 65,999 in both arms.

In the baseline, pod 090 departs Aldgate East toward Bank at tick 66,546 for an earlier pickup.
That pickup boards pod 083 instead.
Pod 090 changes its empty destination to Tower Hill at tick 67,202.
When request 211 arrives at tick 68,997, the pod already travels toward its origin.
In the candidate, pod 090 remains at Aldgate East until request 211 arrives.
It then starts the Tower Hill pickup and boards 2,452 ticks later than the baseline.
An earlier incidental empty move helps the baseline's later request.
This is an indirect fleet effect, not sharing on request 211's own ride.

Interpeak request 240 gains 138.03 seconds of pickup wait with unchanged ride duration.
Its boarding pod changes from 004 to 105.
Request 226 retains identical timing in both arms.

Late13 seed 2's recovery witness is request 388.
Its boarding pod changes from 092 to 098, adding 178.70 seconds of pickup wait with unchanged ride duration.
Their earlier histories differ in resource waits and passenger assignments well before request 388 arrives.
At that arrival, the candidate's pod 092 carries another passenger and cannot provide the baseline pickup.
These observations identify changed fleet availability, without proving one local assignment was incorrect.

## P95 ranks and same-request changes

P95 compares different ranked requests in each arm.
It is not the change in one fixed passenger's journey.

| Pair | Baseline / candidate P95 request | Candidate P95 request's own journey change | Last request, baseline / candidate |
| --- | --- | ---: | --- |
| London198 AM24, seed 2 | 594 / 663 | +87.98 seconds | 699 / 683 |
| Sharing Interpeak11, seed 3 | 226 / 211 | +40.87 seconds | 227 / 227 |
| Sharing Late9, seed 1 | 192 / 112 | -5.47 seconds | 267 / 267 |
| Sharing Morning4, seed 1 | 86 / 9 | 0.00 seconds | 117 / 117 |
| Sharing Late13, seed 2 | 385 / 386 | +342.78 seconds | 388 / 388 |

Late9's aggregate journey P95 worsens although its candidate ranked request improves against its own baseline.
Morning4's candidate ranked request has identical timing in both arms.
Their changed distributions put different passengers at the percentile boundary.
The metadata uses the production nearest-rank calculation on integer journey ticks.
Candidate sharing P95 requests in these selected pairs are neither reassigned parties nor shared hosts.

## Evidence and conclusion

[Metadata](measurements/policy-fleet-history.json) retains exact replay identities, P95 ranks, tail timings, and selected pod histories.
Its owned unit terminated successfully and its scratch binary was removed.
Concurrent functional runs do not provide CPU comparisons.

The failure witnesses persist, but their fleet chains provide no general safe fix in this selected sample.
Keep the existing policies and defaults.
Broader adoption still requires workload service gains and explicit individual-delay limits.
