# LondonFull unfinished-request diagnosis

The targeted reruns found one parking-diversion deadlock and three journeys that exceeded the study cutoff.
The simulation and preset defaults are unchanged.

## Scope and reproduction

The source is `352cdaa`, after the README limit correction.
The simulation and comparison code are unchanged from the capacity study source, `677e62b`.
Four arms reused the archived projects, demand schedules, policies, and seeds.
Each reproduced every original result field exactly before continuing with no new arrivals.
The original observation window remains 60 minutes of arrivals plus 60 minutes of drain.
Continuations allowed up to two more simulated hours.

| Configuration | Band | Rate | Seed | Unfinished at cutoff | Diagnosis |
| --- | --- | ---: | ---: | ---: | --- |
| Baseline, 287 pods | AM peak | 10 | 8 | 2 | One blocked pickup and one long journey |
| Baseline, 287 pods | AM peak | 15 | 8 | 0 | All 899 requests complete by 7,164 seconds |
| `fleet300` | PM peak | 15 | 6 | 1 | Final journey completes at 7,422.20 seconds |
| `fleet300` | Evening | 15 | 8 | 1 | Final journey completes at 7,820.98 seconds |

Rates are nominal requests/minute.
Times are simulated seconds since the start of each arm.
The original cutoff is 7,200 seconds.

## Long journeys

The PM peak remainder is request 788, Richmond to Epping, on pod 012.
Its route is 45.09 km.
It boards at 4,189.52 seconds and completes 222.20 seconds after the cutoff.

The Evening remainder is request 853, Heathrow Terminal 5 to Epping, on pod 109.
Its route is 58.00 km.
It boards at 3,665.88 seconds and completes 620.98 seconds after the cutoff.

The AM peak remainder aboard pod 265 is request 586, Hillingdon to Canary Wharf.
Its route is 35.28 km.
It boards at 4,932.50 seconds and completes at 7,466.57 seconds, 266.57 seconds after the cutoff.

All three pods are moving at 14 m/s at the cutoff, with no recorded resource wait at that instant.
Their ridden and direct route distances match.
These are finite-window misses, not permanent stalls.
They do not change the original recovery criterion or establish a sustained-load capacity bound.

## Parking-diversion deadlock

At 3,462 seconds, request 577 asks to travel from Wood Green to Chancery Lane.
Dispatch assigns pod `london-pod-100`, which is traveling to North London Parking berth 02.
The committed route prefix ends at `parking-north-01-arrival`.
The diversion accepts a new suffix through berth 01 to leave Parking and reach the pickup.
The new suffix does not pass through the original destination, berth 02.

Pod 196 reserves berth 01 for its own parking move.
Pod 100 stops 75 meters along `parking-north-01-in`, waiting for pod 196's berth reservation.
Pod 196 stops 50 meters along that lane, waiting for pod 100's track occupancy.
The two pods remain at those positions through 14,400 seconds.
Request 577 remains pending and assigned to pod 100.
The extra drain completes request 586 but cannot clear this cycle.

The source path is:

- [`divertStart`](../internal/sim/diversion.go) preserves committed lanes and rejects entry into the original destination berth.
  It accepts this earlier point on the station access chain.
- `candidateRoute` chooses a suffix from that point through a different berth of the same station.
- `redirect` releases the old destination claims and installs the new route.
- [`startEmptyMove`](../internal/sim/parking.go) reserves a parking berth before the incoming pod reaches it.
- [`grant`](../internal/sim/traffic.go) honors both the berth reservation and the occupied track cells, which leaves the two pods waiting for each other.

A guard against crossing only the original destination berth does not prevent the deadlock.
A fix must account for the station access chain and other berths, not only the old destination node.

A scratch-only counterfactual rejected candidate suffixes through any berth of the pod's current destination station.
The guard applied only to pod 100 during simulated seconds 3,300 through 3,600.
Request 577 then boarded at 3,841.40 seconds and completed at 4,649.55 seconds.
All 599 requests completed by the 7,467-second observation.
The long Hillingdon journey still exceeded the original cutoff, so this counterfactual does not make the original recovery test pass.
This confirms the diversion mechanism but does not qualify the temporary guard as a production fix.

## Why the higher rate passes

The first 599 origin/destination pairs are identical between the two AM peak schedules.
At rate 15 they arrive every four seconds, instead of every six seconds, and 300 more requests follow.
Request 577 arrives at 2,308 seconds and completes at 3,875.58 seconds in that run.
The changed timing and fleet history avoid the observed deadlock.
This result does not show that higher demand improves capacity or that the tested rates form a monotonic capacity boundary.

## Validation and next step

Every rerun and continuation checks separation, speed, berth ownership, and request conservation once per simulated second.
The existing parking-diversion tests pass, including the committed-inlet test.
Their small scenario does not cover this multi-berth access-chain case.
The diagnostic helper uses a Go overlay and does not change checked-in simulation code.
No production fix or fleet change is included.

The next implementation should prevent a pickup diversion from trapping a pod inside a committed parking access maneuver.
Completing the parking maneuver before reassignment is the conservative option.
A regression must cover two different parking berths, a reserved berth ahead, and a following pod.
It must check eventual completion as well as separation and reservation ownership.
Rerun these four arms after the fix, then the relevant qualification checks.

Local evidence is retained in `~/.cache/agents/podsim/londonfull-diagnosis-20260929/`.
It includes the helper and overlays, commands, exit markers, cutoff and extended snapshots, request timings, and five-minute traces.
The capacity-study bundle retains the input projects and schedules.
