# Pickup tails

Two LondonFull workloads show long pickup waits for selected requests with pickup reassignment.
Each workload has a descriptive case study and a follow-up intervention in its own reproduced histories.
The shared conclusion is at the end.

| Workload | Rate and seed | Frozen source | Selected requests | Follow-up |
| --- | --- | --- | --- | --- |
| Service-tail cases | 10/min, seed 1 | `69845f7`, `bab8559` | 1366, 1379, 1565, 1845, 2062 | Selected exclusions |
| Post-routing tails | 12/min, seed 4 | `a54281e`, `e285e69` | 1324, 3651, 2175, 3421 | One-swap suppression |

## Service-tail cases, 10 requests per minute, seed 1

These five LondonFull cases show why a faster predicted pickup does not guarantee a shorter wait against another policy's history.
The candidate often has a different fleet assignment before the recorded swap.
Observed stopped samples explain little of these selected long waits.
This diagnosis does not establish an optimal assignment or justify default adoption.

### Method

The cases use the [sustained-load study](london-full-controller-sustained.md) at 10 requests per minute, seed 1.
Buffers remain off in both arms.
Only the candidate enables pickup reassignment.
Both receive the same ordered requests over six hours and run to a seven-hour cap.
All five selected requests complete in both arms, so their wait differences are exact.

Selection followed the outcomes: two directly reassigned regressions, one transfer case, one large improvement, and the largest indirectly affected regression.
This is a descriptive case study, not a representative sample or a population estimate.

Two replays add read-only observations of the existing once-per-second snapshot.
Each records the selected request's assigned pod, lane, activity, wait reason, blocker, station phase, and platoon.
Contiguous identical observations form episodes.
A third candidate replay records the boarding pod from snapshot riders and their exact `BoardedTick`.
The observation tick can follow boarding by less than one second.

The source is `69845f7`, including the separately qualified performance changes.
Both initial replays reproduce every original result, schedule, and qualification-check field from the frozen `bab8559` study.
The boarding replay also reproduces all earlier tail observations.
Original safety, census, and restore checks remain enabled.
These equivalence checks establish unchanged measured outcomes for these runs.
They do not establish a counterfactual for suppressing one selected swap.

### Exact waits

| Request | Baseline wait, seconds | Candidate wait, seconds | Change, seconds | Candidate boarding pod |
| ---: | ---: | ---: | ---: | --- |
| 1366 | 538.13 | 1,730.97 | +1,192.83 | 089 |
| 1379 | 474.67 | 1,511.63 | +1,036.97 | 088 |
| 1565 | 0.00 | 716.28 | +716.28 | 006 |
| 1845 | 2,599.25 | 1,158.80 | -1,440.45 | 137 |
| 2062 | 224.63 | 1,681.70 | +1,457.07 | 236 |

Pod numbers abbreviate the full `london-pod-NNN` IDs.
The candidate assigns different pickup pods from baseline in all five cases with sampled pending assignments.
Request 1565 boards immediately in baseline, so it has no baseline pending sample.

### Predictions and earlier divergence

Predictions below measure remaining travel from the recorded decision, not total request age.
Actual remaining time ends at boarding, which can occur in a different pod.

| Request | Old pod to new pod | Old estimate, seconds | New estimate, seconds | Age at decision, seconds | Actual remaining to boarding, seconds |
| ---: | --- | ---: | ---: | ---: | ---: |
| 1366 | 243 to 089 | 2,025.21 | 1,004.88 | 726.02 | 1,004.95 |
| 1379 | 201 to 088 | 1,774.33 | 1,367.54 | 144.00 | 1,367.63 |
| 1565 | 107 to 181 | 492.19 | 389.80 | 652.02 | 64.27 |
| 1845 | 070 to 137 | 1,763.02 | 304.71 | 854.02 | 304.78 |

Request 1366 already exceeds its complete baseline wait before its recorded reassignment.
The replacement estimate closely matches its remaining candidate wait.
Attributing the entire baseline difference to that swap would include delay that occurred before the decision.

Request 1379 swaps assigned pods with request 1403 at the same tick.
Both remaining estimates improve: 1379 from 1,774.33 to 1,367.54 seconds, and 1403 from 415.62 to 368.58 seconds.
Request 1379 still boards later than in the independent baseline history.
The baseline used pod 210, which is neither side of this swap.

Request 1845 boards about 305 seconds after replacement, close to its new estimate.
Its exact wait improves by 1,440.45 seconds against baseline.
Request 2062 has no recorded reassignment decision but receives pod 236 instead of baseline pod 096.
It shows an indirect service regression after the fleet histories diverge.

### Local pickup during an incoming assignment

Request 1565 boards pod 006 at tick 606377.
The last pending sample, tick 606360, still assigns incoming pod 181 on lane `london-link-276-ba-1`.
The rider observation at tick 606420 confirms the different boarding pod and retains the exact boarding tick.
Existing local pickup service collects the request before the incoming assigned pod arrives.
The 64.27-second remaining wait therefore does not measure pod 181's arrival or establish an estimate defect.

### Sampled stopping

The observer counts a traveling pod as stopped when its sampled speed is below 0.01 meters per second.
These counts are one-second boundary observations, not exact stopped durations.
They can miss stops between samples and partial first or last seconds.

| Request | Baseline pending samples | Candidate pending samples | Baseline stopped-traveling samples | Candidate stopped-traveling samples |
| ---: | ---: | ---: | ---: | ---: |
| 1366 | 538 | 1,730 | 0 | 1 |
| 1379 | 474 | 1,511 | 0 | 1 |
| 1565 | 0 | 716 | 0 | 1 |
| 1845 | 2,599 | 1,158 | 1 | 8 |
| 2062 | 224 | 1,681 | 0 | 1 |

Every selected candidate has 29 unassigned samples before its sampled pickup trip.
Baseline request 1845 also has 29 unassigned samples.
The remaining assigned samples predominantly show moving pickup pods over different routes.
These observations support further investigation of earlier dispatch and fleet availability, not a station-reservation change.

### Limits and evidence

The study does not identify which earlier decision caused every later fleet difference.
It does not replay a single suppressed swap (the exclusion follow-up below does) or prove that the previous assigned pod would arrive at its predicted time.
Broader demand coverage and numeric individual-tail limits remain necessary before adopting the experimental controller.
Pickup reassignment and buffers remain off by default.

### Selected exclusions

Refusing selected reassignment trials increases the chosen requests' waits in three reproduced histories.
Two requests then board their original assigned pods hundreds of seconds later.
The third boards an ordinary local pod, so refusal changes its wait by only 6.37 seconds.
These local results do not reverse the broader policy's individual-tail regressions.

#### Intervention and checks

This follows the service-tail cases above at 10 requests per minute, seed 1.
The temporary study overlay refuses beneficial experimental reassignment involving one selected request from its recorded decision tick onward.
It refuses both sides of an assigned-pair swap before either assignment changes.
Ordinary dispatch, local pickup, reservation rules, demand, and safety checks remain active.
Refused trials still consume normal search work and budget.
No production source, project default, or saved format changes.

One unmodified control and three exclusion runs receive the same 3,599 requests.
The control reproduces every prior result, schedule, safety-check field, tail observation, and boarding observation.
Each exclusion's first prospective target trial matches the control's tick, pods, estimates, and serialized `ExportState` hash.
The hash compares the saved-state projection, not every runtime field or owner map.
Earlier recorded decisions and journeys completed before the intervention also match exactly.
All runs pass once-per-second safety and census checks and the two physical-restore checks with checked continuations.
Each selected request completes in both arms, and all requests board before the cap.

The overlay keeps evidence in each controller and reads immutable process configuration.
Focused tests verify transfer and paired refusal before, at, and after the threshold, including unchanged physical state and owners on refusal.
Those tests pass under the race detector.
The normal pickup tests pass with exclusion disabled, and overlay compilation and vet pass.
The overlay's evidence slice follows the controller's existing shallow copy during `Clone`.
This experiment does not clone active controllers, so it does not establish independent evidence ownership for that extension.
Concurrent functional jobs do not support CPU claims.

#### Exact target waits

Each run records one refused beneficial trial.
The intervention specifies ongoing exclusion, although no second beneficial target trial is refused in these runs.

| Request | Control wait, seconds | Excluded wait, seconds | Increase, seconds | Excluded boarding pod |
| ---: | ---: | ---: | ---: | --- |
| 1366 | 1,730.97 | 1,737.33 | 6.37 | 089 |
| 1379 | 1,511.63 | 1,918.42 | 406.78 | 201 |
| 1845 | 1,158.80 | 2,617.12 | 1,458.32 | 070 |

Pod numbers abbreviate `london-pod-NNN` IDs.
For 1379, refusing the swap retains pod 201 instead of replacement 088.
The extra wait closely matches the original remaining-estimate difference of 406.78 seconds.
For 1845, refusal retains pod 070 instead of replacement 137.
Its extra wait closely matches the estimated 1,458.32-second benefit.

For 1366, the last pending observations retain assigned pod 243.
Pod 089 still collects the request through ordinary local pickup.
The incoming pod's original 2,025.21-second estimate therefore does not determine the realized boarding time.
The unmodified control also uses pod 089.

These results reconcile a useful local swap with a worse outcome against the independent baseline policy.
The independent baseline already has different pods available when these requests arrive.
This study does not identify which earlier decisions produced those fleet differences.

#### Effects on other requests

Every global wait comparison includes all 3,599 boarded requests.
The exclusion changes later assignments and traffic, so other requests can improve or regress.

| Excluded request | Mean wait change, seconds | Largest individual increase, seconds | Requests increasing over 300 seconds | Completed, control / excluded |
| ---: | ---: | ---: | ---: | ---: |
| 1366 | -2.04 | 1,458.32 | 43 | 3,598 / 3,597 |
| 1379 | +0.49 | 1,629.27 | 46 | 3,598 / 3,598 |
| 1845 | -0.18 | 1,458.32 | 44 | 3,598 / 3,597 |

The small mean changes coexist with larger individual effects.
The 300-second threshold describes outcomes and is not an approved adoption limit.
These selected interventions do not estimate population benefit, prove optimal assignment, or establish a default policy.
Buffers and reassignment remain off by default.

## Post-routing tails, 12 requests per minute, seed 4

Four selected requests retain longer waits with pickup reassignment.
The captured pickups mostly move from different starting stations.
Every captured route avoids intermediate berths, and the earlier Stratford cycle does not appear.
These cases do not establish a new movement defect or justify a reservation change.

### Replay and limits

The frozen source is `a54281e`.
Both Full AM Peak arms use 12 requests per minute, seed 4, six hours of demand, and a seven-hour cap.
The queue limit is 1,000,000, and every scheduled request enters both arms.
Buffers, sharing, redistribution, and predictive routing remain off.
Virtual platoons use four pods.
Only one arm enables pickup reassignment.

Each replay exactly matches its prior `89e1e1d` result, schedule, and inherited qualification-check fields.
Read-only snapshots add four target histories without changing the measured simulation.
Checks retain once-per-second safety, speed, and unique-order accounting.
Both arms restore physically at three and six hours, with no demotion, requeue, or dropped orders.
Each restored copy runs 60 seconds of dense checks with new controller admissions disabled.
The saved platoon records also match their round trip.

Requests 3651, 2175, and 3421 are the three largest exact regressions in the prior post-routing pair.
Request 1324 retains the [earlier Stratford witness](berth-route-preference.md).
Selection follows outcomes and does not estimate population behavior.
All four requests complete in both arms.

### Matched waits

Times below use simulated seconds.
Stopped counts sample an assigned traveling pod below 0.01 meters per second once per second.
They are boundary observations, not exact blocked durations.

| Request | Journey | Baseline wait | Reassignment wait | Added wait | Stopped pickup samples, baseline / reassignment |
| ---: | --- | ---: | ---: | ---: | ---: |
| 1324 | Leyton to High Street Kensington | 446.30 | 664.10 | 217.80 | 0 / 1 |
| 3651 | Amersham to Farringdon | 1,191.95 | 2,821.68 | 1,629.73 | 1 / 1 |
| 2175 | Rickmansworth to Green Park | 557.63 | 2,115.52 | 1,557.88 | 0 / 0 |
| 3421 | Rickmansworth to Waterloo | 0.00 | 1,453.35 | 1,453.35 | 0 / 1 |

No selected pending sample reports an occupied-berth wait.
Each reassignment case has 29 unassigned boundary samples before its sampled pickup assignment.
The bounded finishing-pod hold can explain that interval, but it does not explain the remaining long moving pickup.
Snapshots retain the hold's diagnostic text without observing its exact decision event.

### Pickup routes and local decisions

Request 3651 uses pod 215 from Northwood in baseline and pod 174 from Great Portland Street with reassignment.
The captured routes contain 22 and 52 lanes, respectively.
Request 2175 uses pod 216 from Moor Park in baseline and pod 063 from Warren Street with reassignment.
Their captured routes contain 22 and 49 lanes.
Neither request has a direct experimental reassignment record.
Their changed fleet histories precede these pickups.

Request 1324 boards pod 044 in baseline and pod 112 with reassignment.
The candidate's pickup starts at Tower Hill.
Its captured route includes only the endpoint berth nodes, and the previous intermediate-Stratford wait is absent.
This preserves the narrower [post-routing result](berth-route-preference.md), without claiming that all future cycles are impossible.

Request 3421 boards local pod 223 immediately in baseline.
The reassignment arm first sends pod 113 from Oxford Circus, then replaces it with pod 235 from Watford.
The recorded replacement occurs when the request is already 914.02 seconds old.
Its remaining estimate decreases from 1,222.82 to 539.26 seconds.
The request boards 539.33 seconds later.
The replacement therefore has a useful local prediction, while the independent baseline already supplied an immediate pickup.
This replay does not establish when the original assigned pod would have arrived without replacement.

The moving histories support further dispatch and fleet-availability work.
They do not prove an optimal assignment, identify every earlier divergence, or establish that empty-pod platoon links have no effect.
No production fix follows from this diagnosis.
Pickup reassignment and buffers remain off by default.
The owned run stopped successfully, and its scratch binary was removed.
Concurrent functional runs provide no CPU comparison.
The one-swap suppression below confirms that an early swap changes selected later pickup tails.
Suppressing that pair does not establish a general dispatch fix.

### One-swap suppression

An early pickup swap changes later assignments, berth choices, and parking destinations.
Suppressing that one swap improves four selected tails relative to normal swaps.
One tail still regresses against swaps off.
This intervention does not establish a general dispatch fix or justify enabling swaps by default.

#### Exact history and intervention

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

#### Selected pickup waits

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

#### Whole workload and checks

Pickup-wait P95 is 1,116.47 seconds with swaps off, 1,095.20 with normal swaps, and 1,092.12 under the intervention.
Journey P95 is 2,513.50, 2,518.67, and 2,508.68 seconds respectively.
All arms drain, but these aggregate results do not remove individual regressions.

All primary arms retain once-per-second safety, speed, and unique-order checks.
Physical restores retain poses, bindings, admission ages, and saved platoon records.
Each restored copy passes 60 seconds of dense continuation checks.
Short pilots check every tick and match production aggregates.
Concurrent functional runs do not provide CPU comparisons.

In history events, `Pending` names an admission reservation index, not a waiting request.
Recorded positions are event-time observations and must not be interpolated as exact later positions.

The selected intervention establishes sensitivity to an upstream swap.
A general policy change would need matched workload gains and acceptable individual delays across broader cells.

## Shared conclusion

Both workloads show that a faster predicted pickup at a swap does not guarantee a shorter wait against another policy's history.
The long waits follow earlier changes in fleet availability, not a station-reservation or movement defect.
Refusing or suppressing the selected swap does not reverse the broader policy's individual-tail regressions.
No production fix, default change, or reservation change follows.
Pickup reassignment and buffers remain off by default.
The raw measurement data of these studies is in git history.
