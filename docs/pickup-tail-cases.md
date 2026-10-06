# Pickup reassignment service-tail cases

These five LondonFull cases show why a faster predicted pickup does not guarantee a shorter wait against another policy's history.
The candidate often has a different fleet assignment before the recorded swap.
Observed stopped samples explain little of these selected long waits.
This diagnosis does not establish an optimal assignment or justify default adoption.

## Method

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

## Exact waits

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

## Predictions and earlier divergence

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

## Local pickup during an incoming assignment

Request 1565 boards pod 006 at tick 606377.
The last pending sample, tick 606360, still assigns incoming pod 181 on lane `london-link-276-ba-1`.
The rider observation at tick 606420 confirms the different boarding pod and retains the exact boarding tick.
Existing local pickup service collects the request before the incoming assigned pod arrives.
The 64.27-second remaining wait therefore does not measure pod 181's arrival or establish an estimate defect.

## Sampled stopping

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

## Limits and evidence

The study does not identify which earlier decision caused every later fleet difference.
It does not replay a single suppressed swap or prove that the previous assigned pod would arrive at its predicted time.
Broader demand coverage and numeric individual-tail limits remain necessary before adopting the experimental controller.
Pickup reassignment and buffers remain off by default.

[Case totals](measurements/pickup-tail-cases.csv) retain exact waits, predictions, boarding pods, and sampled counts.
[Detailed evidence](measurements/pickup-tail-cases.json) retains timings, lane episodes, same-tick decisions, hashes, source manifests, and replay outcomes.
The [selected-exclusion follow-up](pickup-local-intervention.md) compares three local interventions within reproduced histories.
The separate [12/min seed-4 case](berth-route-preference.md) captures reciprocal waits at an intermediate berth, unlike the mostly moving assignments sampled here.
