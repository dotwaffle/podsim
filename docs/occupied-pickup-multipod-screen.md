# Occupied pickup service screen with competing pods

The two-pod fixture passes the selected service gates.
The three-pod fixture fails the approved individual pickup and journey limits.
Occupied pickups improve aggregate waits in both fixtures, but some parties wait longer.
The policy remains opt-in, with no default adoption claim.
The [measurement record](measurements/occupied-pickup-multipod-screen.json) retains every authored offer, both outcomes, and individual timing changes.

## Fixed workload

The screen uses the existing Example network at `5a57e4c` with two or three legacy 4 m pods.
Pod 01 starts at Harbor and pod 02 starts at Market.
The three-pod fixture also starts pod 03 at Garden.
All offers have explicit shared consent and one passenger.
Both arms use four-party sharing, drop-off mode, three intermediate stops, and free-flow routing.
Occupied pickups are the only policy difference.

Four waves start at 0, 180, 360, and 540 seconds.
Each wave submits Harbor to Market, Harbor to Garden, and Market to Harbor, in that order.
The three-pod fixture then submits Garden to Harbor.
At ten seconds after each wave, both fixtures submit Garden to Market.
The other pods have initial orders when the first occupied pod reaches Garden.
This creates an actual intermediate pickup with competing vehicles.

The queue limit remains 200 and the horizon remains 900 seconds.
An authored Harbor-to-Market offer at 901 seconds remains future in both arms.
The fixtures retain geometry, speeds, clearances, ordinary spacing, and all current operating limits.
Buffers, platoons, positioning, and pickup reassignment remain off.
The plan froze offers, caps, restore triggers, and source pins before the pilots.
The study made no timing or workload change after the results.

## Aggregate and individual results

The [approved gates](experimental-adoption.md) permit pickup increases of up to the larger of 30 seconds and 5% of baseline.
They permit journey increases of up to the larger of 60 seconds and 5% of baseline.
The score separates any positive regression from an approved-limit exceedance.
A result equal to its limit passes.

| Fleet | Occupied pickups | Completed / offered before cap | Pickup p95 | Request-to-completion p95 | Empty distance |
| --- | --- | ---: | ---: | ---: | ---: |
| 2 pods, off | 0 | 16 / 16 | 201.867 s | 317.383 s | 8,181.493 m |
| 2 pods, on | 4 | 16 / 16 | 36.050 s | 116.733 s | 0 m |
| 3 pods, off | 0 | 20 / 20 | 184.850 s | 327.850 s | 8,018.726 m |
| 3 pods, on | 5 | 20 / 20 | 121.250 s | 266.333 s | 7,256.686 m |

Each policy-on fixture records actual occupied pickups on two distinct pods.
The observer checks the admission tick, source berth ownership, native boarding binding, and positive passenger-distance baseline.
Maximum completed detour ratio remains 1.19862 in every arm.
Neither fixture exceeds four passengers or four parties aboard a pod.

The two-pod fixture's last Garden-to-Market party waits 14.183 seconds longer with occupied pickups.
Its request-to-completion time also increases by 14.183 seconds.
The 14.183-second increases remain below the approved pickup and journey limits.
The first Harbor-to-Market party completes three seconds later because the occupied pickup adds boarding dwell.
Other incumbent parties also have longer completion times.
Every measured two-pod party remains within its approved individual limits.

The three-pod fixture's final Garden-to-Harbor party waits 106.400 seconds longer and completes 94.033 seconds later.
The final Garden-to-Market party waits 31.200 seconds longer and completes 44.267 seconds later.
Both final Garden parties exceed their 30-second additional pickup limits.
Only the Garden-to-Harbor party exceeds its 60-second additional journey limit.
The complete ledger includes these parties and all other individual changes.
The three-pod aggregate gains do not waive these individual failures.

The aggregate gate limits mean pickup wait, mean journey, and journey p95 increases to 2% in each matched pair.
Both fixtures reduce all three measures and meet the separate 5% mean service benefit gate.

| Measure | 2 pods, off | 2 pods, on | 3 pods, off | 3 pods, on |
| --- | ---: | ---: | ---: | ---: |
| Mean pickup wait | 107.314 s | 9.013 s | 78.753 s | 26.791 s |
| Mean journey | 178.048 s | 83.079 s | 168.473 s | 117.298 s |
| Journey p95 | 317.383 s | 116.733 s | 327.850 s | 266.333 s |

Both fixtures complete every baseline-completed identity within the unchanged 900-second cap.
The plan declares no earlier recovery deadline.
Both fixtures refuse zero offers and pass the zero-additional-queue-loss gate.
All screen offers before the cap complete, with zero refusals and zero censored orders.
Each complete ledger also retains its unsubmitted 901-second offer.
The pilots retain accepted but unfinished parties and all future identities.
No unfinished, refused, or future party enters the completed-request tails.
The synthetic refusal test checks ledger retention but does not qualify queue saturation.

## Physical checks and restore scope

The four screen arms pass 216,004 separation and speed checks, 216,004 contract checks, and 216,004 identity-conservation checks.
Minimum observed separation is 23.708 meters.
The screen performs 62 planned physical restore probes and 3,720 restored continuation checks.
All restore receipts retain the physical tier without demotion, requeue, dropped orders, or unaccounted identities.
All saved discrete fields and positions match immediately after restoration.
The permitted position tolerance is 0.0000001 meter, with zero observed difference.

Fixed probes occur at 0, 30, 180, 540, and 899 seconds.
Event probes cover each pod's first occupied admission, partial dwell after 37 ticks, travel, and alighting.
Thirty-four probes retain accepted occupied cohorts.
Seventeen of those probes disable new occupied pickups while preserving accepted riders, boarding baselines, and clocks.
Their 60-tick continuation checks do not prove that every restored cohort drains with pickups disabled.
Later admissions have live checks and complete timing records, but do not each receive all event probes.

Physical restore resets velocity.
An initial setup assertion incorrectly compared its future trajectory with an uninterrupted clone.
The corrected probe compares two independent restores from the same snapshot and policy.
Their next 60 states and completion receipts match exactly, with separate safety, contract, and conservation checks.
The setup failure and correction remain in the evidence.

Every pilot and screen arm matches an ordinary run in all aggregate fields, native request timings, and saved state.
Focused observer and ledger tests, race checks, vet, lint, and fresh gopls diagnostics pass.
Five compiled mutations fail assertions for omitted events, wrong baselines, lost future offers, duplicate riders, and lost censored counts.

Root review corrected an initial scoring error that treated every positive individual delta as a failure.
The correction uses the approved numerical limits and retains all original native measurements.
Boundary tests cover below, equal to, and above each individual limit and the 2% aggregate limit.
The correction requires no native simulation rerun.

This screen covers two authored workloads with two or three pods.
It does not qualify rail workloads, larger fleets, queue saturation, sustained capacity, all networks, or default adoption.
CPU, memory, GC pauses, network rate, and full policy-off restored-cohort recovery remain outside this screen.
The [adoption gates](experimental-adoption.md) remain in force.
