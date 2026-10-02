# Pickup tails and stopped-queue headway

The banked pickup-tail regression mainly reflects longer waits without an assigned pod in seed 2.
Entrance pacing is not established as its cause.
Existing virtual platooning compacts queues, but this two-seed Rail Hub screen gives no consistent service gain.
Keep defaults, braking, junction reservations, and the 12-meter clearance unchanged.

## Why the pickup tails changed

Match passengers by their complete offered event identity, not by request ID across layouts.
The 200-request cap changes which offers each layout accepts.
The [measurement record](measurements/station-queue-headway.json) retains common and changed populations, sample contributions, and raw artifact hashes.

| Common accepted passengers | Seed 1 | Seed 2 |
| --- | ---: | ---: |
| Count | 309 | 312 |
| Baseline-only accepted offers | 54 | 54 |
| Banks-only accepted offers | 56 | 48 |
| Mean pickup change, seconds | -15.82 | +111.76 |
| Mean unassigned sample change, seconds | -12.18 | +99.26 |
| Mean assigned moving/dwell sample change, seconds | -2.06 | +14.07 |
| Mean assigned stationary traffic change, seconds | -1.52 | -1.57 |
| Common-passenger pickup p95, baseline | 3,851.93 | 3,768.27 |
| Common-passenger pickup p95, banks | 3,854.77 | 3,897.05 |

In seed 2, unassigned samples explain about 89% of the common-passenger mean increase.
One inbound passenger waits 1,046.62 seconds longer, with sampled unassigned time increasing from 2,493 to 3,539 seconds.
Stationary traffic time decreases in both seeds.
Seed 1 has a lower common-passenger mean but a slightly higher p95, with regressions concentrated in some remote-origin requests.

The common inbound post-boarding duration increases by 16.81 seconds in seed 1 and 15.26 seconds in seed 2.
The common outbound duration decreases by 9.16 and 4.19 seconds.
These changes are consistent with changed fleet circulation and subsequent pickup availability.
They do not isolate a route or bank-selection decision as the cause.
The samples occur once per second and combine assigned movement with dwell.
They do not give exact assignment or gate-entry timestamps.

## Current spacing rules

The ordinary controller uses exclusive track cells sized around 30 meters.
Its braking reservation uses the greater of current pod speed and lane speed limit.
At 14 m/s, its default reservation horizon is about 49.47 meters.
That horizon is not a required pod-to-pod gap.
The global physical clearance remains 12 meters, combining a four-meter pod length and an eight-meter gap.

Virtual platooning allows a certified follower to use track cells held by its predecessor.
Junction reservations remain protected against other traffic, while berths remain exclusive.
Following caps use both pods' current stopping distances, a 0.5-second reaction allowance, and curve-adjusted clearance.
A straight certified link retains a 12.01-meter minimum.
Ordinary links exclude station-entry and berth-access lanes.
Fixed-entry station links need an eligible buffer and retain exclusive discharge paths.

Stock recruitment begins below half the lane speed limit, which is 25.2 km/h on a 14 m/s lane.
Every remaining route lane and each tagged lane of the destination station must have the same speed limit.
A straight new link must also start within `12.01 + speedLimit²/4` meters.
At a uniform 9 km/h limit, that ceiling is only 13.5725 meters.
A queue stopped 30 meters apart cannot recruit under that rule.
Lowering the speed limit can therefore prevent compaction.

Car-following research separates standstill spacing from speed-dependent following distance.
Its desired-gap models do not replace this simulator's resource and certificate checks.
See the [IDM model definition](https://mtreiber.de/MicroApplet/IDM.html) and [traffic-light approach study](https://www.mtreiber.de/publications/2014_Qingdao.pdf).

## Matched service screen

All heavy arms offer 1,440 passengers per seed with identical schedules, fleet, queue cap, and speed limits.
Geometry stays fixed within each layout's policy comparisons.
The banked layout is the same candidate as the [bank screen](station-banks-screen.md).
Only the platoon policy or private new-link formation restriction changes.
No arm lowers physical clearance.

The below-10-km/h restriction only permits a new link when the follower is below 10 km/h.
It neither checks leader speed nor imposes a persistent speed cap.
The station-local restriction only permits new links on a station-tagged lane.
Existing links continue under their ordinary certified caps and drainage rules.
Neither restriction introduces a new minimum gap or a station-boundary release rule.

| Banked policy | Completed, seed 1 / 2 | Pickup p95, seconds, seed 1 / 2 | Total completed / offered |
| --- | ---: | ---: | ---: |
| Platoons off | 365 / 360 | 3,877.88 / 3,915.82 | 725 / 2,880 |
| Existing virtual platoons | 362 / 359 | 3,928.97 / 3,933.67 | 721 / 2,880 |
| Form below 10 km/h | 360 / 365 | 3,976.35 / 3,825.72 | 725 / 2,880 |
| Form on station-local lanes | 358 / 363 | 3,991.38 / 3,859.40 | 721 / 2,880 |

The shared-entry baseline also gives no consistent gain: virtual completes 364/365, compared with 363/366 with platoons off.
All heavy arms finish every accepted request and make no outbound train connections.
They have no future offers or censored accepted requests.
Conditional tails exclude skipped offers.
The record retains skipped counts, connection outcomes, journey tails, queue clearance, empty running, and resource waits.

Existing virtual platooning has measurable activity in both banked seeds.
It couples 29/28 distinct pods, reaching four-pod platoons.
The smallest sampled slow same-lane gap falls from 26.67 meters in the off control to 12.01 meters.
This establishes compaction, without establishing a service improvement.
The pilot arms each complete 12 journeys and make six train connections.
Observer-on and observer-off pilot results match.

## Held station queue

A separate synthetic fixture holds four pods behind an unavailable berth on a straight eligible station entry.
Queue span is the distance between the first and last pod reference points.
The fixture waits for five seconds of stable standstill before measuring.

| Controller | Uniform speed limit | Queue span, meters | Coupled pods | Settled time, seconds |
| --- | ---: | ---: | ---: | ---: |
| Ordinary | 14 m/s | 90.00 | 0 | 92.75 |
| Existing virtual | 14 m/s | 54.02 | 3 | 92.10 |
| Ordinary | 9 km/h | 90.00 | 0 | 144.28 |
| Existing virtual | 9 km/h | 90.00 | 0 | 144.28 |
| Wider recruitment prototype | 9 km/h | 54.02 | 3 | 161.03 |

The private prototype raises only the recruitment ceiling to `max(existingCeiling, 60 meters)`.
It retains the same stopping inequalities, curve clearance, resource ownership, route restrictions, and four-pod cap.
The minimum physical gap remains 12.01 meters after compaction.
The compacted low-speed queue settles 16.75 seconds later than the ordinary queue.
This is a queue-storage result, not a discharge-headway or capacity result.

The fixture's synthetic berth reservation is not part of saved state.
Its restore probe validates certificate placement, then runs one second with platooning and buffers disabled.
It does not qualify persistence of the same blocker or full discharge.
The existing corridor tests separately reproduce better headways for initially packed platoons under their controlled corridor assumptions.
For example, straight-lane mean headway changes from 6.011 seconds to 2.415 seconds with packed four-pod platoons.
That result does not predict this Rail Hub workload.

## Further proposals

The approved follow-up implements [one-cell recruitment inside fixed station buffers](station-buffer-recruitment.md).
It qualifies real blockers, full discharge, live ownership, and restore continuation while retaining the 12-meter floor.
The global 60-meter prototype below remains a historical experiment.

First investigate wider recruitment only for certified fixed-entry buffer queues.
The 60-meter ceiling is an experimental choice, not a justified production threshold.
A production rule should use the eligible queue geometry and retain all current stopping and ownership checks.
Earlier coupling can lock routes and disable diversion or pickup reassignment.
Measure those effects together with queue span, discharge headway, and offered-population service.

Before implementation, test a real occupied berth and blocked departure, competing junction traffic, and complete drain after release.
Check physical separation, stopping inequalities, braking bounds, no overtaking, and resource retention throughout.
Restore at wide-gap recruitment, compacted waiting, berth-suffix commitment, and drainage.
Continue each restored state to completion with policies enabled and disabled.
Test recruitment boundaries, mixed speed limits, curves, station-role exclusions, and platoon sizes.

A lower standstill gap below 12 meters is a separate contract proposal.
One candidate for evaluation is six-meter reference spacing, comprising four-meter pod length and two-meter standstill gap.
Restrict it to a certified same-path station queue with both pods capped below 10 km/h.
Following distance must also include reaction time, relative braking distance, and curve allowance.
A speed band must not abruptly raise the required gap while the follower lacks space to comply.
Retain the compact certificate until ordinary clearance is restored before discharge or a route change.
Unrelated paths, junction reservations, and nonincident geometry retain current clearance.
This needs reviewed safety and saved-certificate semantics before any below-12-meter simulation.
No such exception has been implemented or tested here.

## Validation

Fourteen final service arms run sequentially with Go 1.27.1, `GOMAXPROCS=1`, and `GOGC=400`.
They use clean native source `d86c254` with private observer overlays.
The two refreshed off controls reproduce frozen result fields and receipts, excluding the explicit policy label.
Every service tick checks physical separation and speed, with saved-state route, phase, and request contracts each simulated second and regular physical restore probes.
The pilot arms also check observer parity.
Raw manifests retain binary, fixture, source, output, and watchdog provenance.

The held-queue prototype passes focused native and race tests, existing platoon and fixed-entry regression tests, and vet.
Independent review finds no concrete safety hole in the isolated recruitment change.
The qualification limits above remain open.
No production Go, JavaScript, project/save format, default, or safety constant changes.
No push, deployment, or preserved demo restart occurred.
