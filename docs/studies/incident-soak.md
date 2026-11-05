# Incident soak timing

This record holds the timing run that decision 16 of the [incident stage 7 contract](../contracts/incident-stage7-contract-proposal.md) asks for.
The full soak of section 5.1.9 has not run, and this record holds no result for it.
The maintainer chooses the size from these numbers.
The code of the congestion, queue, and predictive routing policies was removed on November 5, 2026, so the rows for these policies are history.

## Open items

- The soak of amendment A6 ran and failed.
  The Progress row missed on both presets.
  Each failure of the Persistence row was a false alarm of the check, and the rerun of the next section had none.
  The section "Soak of amendment A6" has the numbers.
- The Performance row for suspension meets amendment A7 in the section "Readings from a quiet host".
- The Performance row for emergencies misses its ratio rule on `rail-hub`, by about 0.4 µs.
- The Choice row is reported and not gated, and no driver reports it, because the sim does not expose the estimate.

## Method

`TestIncidentSoak` in `internal/session` drives each run.
The driver sets 10 faults and 10 emergencies an hour, faults of 600 seconds, a debris share of 0.25, and debris of 2 meters.
It does not read the `rail-hub-incidents` preset, which has faults of 120 seconds.
The status line under decision 7 of the contract keeps the soak at 600 seconds, because section 5.1.9 names no duration.

Each rate is ticks for each CPU second of the process, from `/proc/self/stat`, in simulated windows of 5 minutes.
One run has 24.5 simulated hours, which is 5,292,000 ticks.
All runs used seed 1, one process for each arm, on a host of 12 threads.
Other sessions used the host, with a load average from 5 to 90.
CPU time changes less than wall time under load, but these rates are still not from a quiet machine.
Each timed process used 57 to 150 MB of resident memory.
`TestIncidentSoak` runs its groups in parallel, so a matrix run needs `-parallel` and a memory cap.

## Measured rates

The queue is at its limit of 200 orders in each row, unless the row states another size.

| Preset | Arm | Every-tick checks | Ticks for each CPU second | CPU hours for a run |
| --- | --- | --- | ---: | ---: |
| rail-hub | reference | Off | 6,000 | 0.25 |
| rail-hub | emergencies | On | 1,170 | 1.26 |
| rail-hub | incidents | On | 1,050 | 1.40 |
| rail-hub | platoons | On | 1,080 | 1.36 |
| london-central | reference | Off | 3,800 | 0.39 |
| london-central | emergencies | On | 450 | 3.27 |
| london-central | incidents | Off, queue 190 | 157 | Not applicable |
| london-central | incidents | On, queue 143 | 187 | Not applicable |
| london-central | incidents | On, projected | 125 | 11.8 |

The rates did not drift in a run with no incident: 4 simulated hours on rail-hub, and 2 on london-central.
No london-central run with faults reached a full queue with the checks on in the 5 minutes of a scratch run.
So the projected row adds the cost of the checks to the rate at queue 190.
The checks cost 1.4 to 2.4 ms for each tick on london-central, and about 0.7 ms on rail-hub.

The london-central preset already has a platoon limit of 4, so its platoons arm is the same project as its incidents arm.
The two arms give the same run, seed for seed.

## Cost of a fault on london-central

The fault arms cost 25 times the reference arm, with the checks off.
A profile of 10 simulated minutes shows 61 percent of the CPU time in `keepTripPod`, through `pickupAccess` and `searchRoute`.
This path does not use the memo of failed pickup searches.
At 157 ticks for each CPU second, the fastest speed that one core can hold is about 2.6 times real time.

## Extrapolated matrix

| Size | Runs | CPU hours | Wall hours on 12 threads |
| --- | ---: | ---: | ---: |
| Section 5.1.9, as approved | 200 | 640 | 54 |
| R1: no london-central platoons arm or its reference | 160 | 400 | 33 |
| R2: R1, and 5 seeds for the london-central incidents arm | 145 | 222 | 19 |
| R3: R1, and a dispatch change that reaches the emergencies rate | 160 | 228 | 19 |

R1 loses no coverage, because each dropped run repeats another run.
R2 needs an amendment, because the Safety row asks for at least 20 seeds.
R3 is a target and not a measurement, because no such change exists.

## CI soak

`TestIncidentSoakCI` took 285 seconds at a load average of about 30: a reference run of 51 seconds, and a checked run of 233 seconds.
The checked run had 9 faults and 9 emergencies, and it passed.
It uses rail-hub only, because the CI row names no preset and a london-central run takes an estimated 20 to 30 CPU minutes.
`mise run qualify` also runs `TestSampledTickLatency`, in about 3 seconds, and `TestSampledTickNoCandidateLatency`, in 386 seconds at a load average of about 15.

## Soak of amendment A6

The soak ran at `cbbdb18` on 2026-11-05: 5 seeds of 6 simulated hours and a clear of 30 simulated minutes, with a limit of 2 hours of wall time.
Rail-hub ran each arm, and `london-central` ran `incidents`, `emergencies`, and `reference`.
The host load was 15 to 38, because other work ran at the same time.
Rail-hub finished in about 1 h 15 min, and the limit stopped `london-central` at 1 h 58 min.

| Preset and arm | Runs finished | Rewind | Late emergencies, of those seen | Longest emergency (min) |
| --- | ---: | --- | --- | ---: |
| `rail-hub` `reference`, `reference-platoons` | 10 of 10 | None run | None | None |
| `rail-hub` `emergencies` | 3 of 5; seed 2 did not start | Failed at seed 5, tick 1001450 | 0 of 275 | 3.6 |
| `rail-hub` `incidents` | 4 of 5 | Failed at seed 2, tick 633325 | 50 of 335 | 35.7 |
| `rail-hub` `platoons` | 4 of 5 | Failed at seed 2, tick 633325 | 39 of 336 | 39.5 |
| `london-central` `reference` | 5 of 5 | None run | None | None |
| `london-central` `incidents` | 5 of 5 | Passed, 6 for each run | 1 of 470 | Not extracted |
| `london-central` `emergencies` | 0 of 5, cut at 5 to 65 simulated minutes | No reading | No reading | No reading |

A late emergency is one of an ordinary pod that takes more than the 10 simulated minutes of the Progress row.
Most late records are in the phases `bound` and `deferred`, with the wait reasons `Pod ahead`, `Blocked by incident`, `Berth occupied`, or `Fault stopped`.
Faults last 600 s in this soak, so an emergency behind a fault can use the whole limit before it moves.
That explanation is not proved.

The log shows no report from the Safety, Contract, Conservation, or Rail rows in any run.
A run that fails the rewind check stops there, so those runs have no later reading.
Each rewind failure was a false alarm of the check, and `083a487` corrects it.
When a sampler makes a record in the replay window, the replay gives the record the new generation, as the contract permits, and the check compared that field too.
Across 24 instrumented rewinds, only `Generation` differed, on 5 windows.
Two same-seed runs gave identical state hashes.
With the correction, `platoons` seed 2 and `emergencies` seed 5 pass all 6 rewinds, and `platoons` seed 2 has 7 late emergencies.
The `emergencies` arm of a seed runs after `incidents`, so the failure of `incidents` at seed 2 also stopped `emergencies` at seed 2.

`london-central` with faults ran at 219 to 239 ticks a second of wall time, against 2,148 to 2,262 for its reference.

## Rerun of the soak at `083a487`

The rerun used the corrected check, on the code of `083a487`, with the same size and limit.
Rail-hub ran each arm, and `london-central` ran `emergencies` with its reference.
The load average was 30 to 45, because other work ran at the same time, and the limit of 2 hours stopped rail-hub.

| Preset and arm | Runs finished | Rewinds | Late emergencies |
| --- | ---: | --- | ---: |
| `london-central` `emergencies` | 5 of 5 | 30 of 30 replay | 0 of 454 |
| `rail-hub` `emergencies` | 4 of 5 (seeds 1, 2, 4, 5) | 24 of 24 replay | 0 of 374 |
| `rail-hub` `incidents` | 5 of 5 | 30 of 30 replay | 56 of 427 |
| `rail-hub` `platoons` | 3 of 5 (seeds 2, 3, 4) | 18 of 18 replay | 32 of 268 |

The longest emergency on `london-central` `emergencies` is 3.4 simulated minutes.
The log shows no report from the Safety, Contract, Conservation, or Rail rows in any run.
Each arm and seed that finished in both runs has the same completed count, so `internal/sim` changes between `cbbdb18` and `083a487` did not change these runs.
Seeds 1 and 5 of `platoons` finished in the first run.
Seed 3 of `emergencies` also finished only in the first run.
Seed 2 of `incidents` and `emergencies` ran by itself after the limit, so each arm of each seed has one finished run.

## Attribution of the late emergencies

This section attributes the time of the late records of the section "Soak of amendment A6".
It tests the explanation that section leaves open, that an emergency behind a fault can use the whole limit before it moves.

### Late records at the moment of the limit

The miss lines of the soak logs give one snapshot of each record, at the moment it passes 10 simulated minutes.

| Phase and wait reason | `rail-hub` `incidents` | `rail-hub` `platoons` | `london-central` `incidents` |
| --- | ---: | ---: | ---: |
| `bound`, `Pod ahead` | 24 | 16 | 0 |
| `bound`, `Blocked by incident` | 7 | 2 | 1 |
| `bound`, `Berth occupied` | 4 | 3 | 0 |
| `bound`, no wait | 4 | 4 | 0 |
| `bound`, `Fault stopped` | 2 | 2 | 0 |
| `deferred`, `Berth occupied` | 5 | 4 | 0 |
| `deferred`, `Pod ahead` | 2 | 3 | 0 |
| `deferred`, `Blocked by incident` | 1 | 2 | 0 |
| `deferred`, `Fault stopped` | 0 | 3 | 0 |
| `unloading`, no wait | 1 | 0 | 0 |
| Total | 50 | 39 | 1 |

The snapshot names a fault as the blocker for 10 of 50, 9 of 39, and 1 of 1 records.
A blocker that is a pod does not show whether a fault is the cause, so the snapshot does not decide the explanation.

### Method

A temporary change, now removed, ran at `63a4868` with the soak driver.
It ran `incidents` and `platoons` on `rail-hub` for seeds 1, 3, 4, and 5, and `emergencies` for seeds 1 and 3.
It ran `london-central` `incidents` seed 3 until record `i5.136` ended.
Seed 2 is not in the 50 or the 39, because its failed rewind stops the run before the driver reports its misses.
The change turned off the every-tick checks and the save checks and kept the rewinds, so the driver draws the same random values.
All 89 `rail-hub` late records came back with the same seed, ID, pod, and start tick as the soak at `cbbdb18`, and so did `i5.136`.

The change reads each active record once a simulated second, and it gives each reading the ticks since the last reading.
It walks the `BlockedBy` chain of the pod at each reading and puts the reading in one bucket:

- Own fault: the pod itself has an active fault.
- Direct: the pod reports `Blocked by incident` or `No forward route`.
- Chain: the walk reaches a fault ID, or a pod that reports `Blocked by incident` or `No forward route`.
- Idle pod: the walk ends at an idle pod at its berth, with an order that cannot board.
  The order must fail to board with the active faults and succeed with the blocked set empty, in a probe of the simulation at that reading.
- Queue: the walk ends at any other pod, with no fault in the chain.
- Moving or dwelling: the pod has no wait report.

### Attribution

The table gives simulated minutes over the late records, and the share of the total.

| Bucket | `rail-hub` `incidents`, 50 records | `rail-hub` `platoons`, 39 records | `london-central` `incidents`, 1 record |
| --- | ---: | ---: | ---: |
| Own fault | 15.0 (2%) | 30.0 (5%) | 0 |
| Direct | 141.4 (17%) | 66.7 (12%) | 9.2 (82%) |
| Chain | 400.2 (49%) | 272.0 (48%) | 0 |
| Idle pod | 153.7 (19%) | 121.5 (21%) | 0 |
| Queue | 50.6 (6%) | 30.9 (5%) | 0.3 (2%) |
| Moving or dwelling | 60.4 (7%) | 47.3 (8%) | 1.8 (16%) |
| Total | 821.0 | 568.0 | 11.2 |

The four fault buckets hold 86% of the `incidents` minutes and 86% of the `platoons` minutes.
The arm with faults off does not show the delay: 0 of 198 records were late in the two control runs, with a longest record of 3.5 min, against 0 of 275 and 3.6 min in the soak.

### Verdict on the explanation

Faults cause the late records, and the explanation is confirmed in that part.
The fault buckets alone reach 10 min for 34 of 50 `incidents` records and 25 of 39 `platoons` records.
The words "before it moves" fit only part of the records: the pod first moves after the limit, or not at all, for 18 of 50 and 11 of 39 records.
The other records move first and then stop.
Faults also overlap, so one record can wait longer than the 600 s of one fault.
Record `i2.27` of seed 5 waited while 2 to 9 faults were active.

### What each measure can meet

The table gives the late records that stay above 10 min under each measure, and the longest value in minutes.
Each row from the second to the fifth removes one more kind of time from the record.

| Measure | `rail-hub` `incidents`, of 50 | `rail-hub` `platoons`, of 39 | `london-central` `incidents`, of 1 |
| --- | ---: | ---: | ---: |
| Total time, the approved rule | 50, 35.7 | 39, 39.5 | 1, 11.2 |
| Without own fault and direct | 37, 32.4 | 28, 29.5 | 0, 2.0 |
| Also without chain | 11, 24.1 | 7, 25.0 | 0, 2.0 |
| Also without idle pod | 1, 11.0 | 0, 4.9 | 0, 2.0 |
| Also without the stop of `i2.27` | 0, 7.2 | 0, 4.9 | 0, 2.0 |
| After the last reading blocked by a fault | 0, 3.2 | 0, 4.0 | 0, 1.2 |
| Moving or dwelling only | 0, 2.9 | 0, 3.0 | 0, 1.8 |
| With no active fault in the network | 0, 1.4 | 0, 1.3 | 0, 0 |

The last row meets the limit because faults are almost always active in this soak, so it tests little.
The chain walk alone is not enough: it leaves 11 and 7 late records, because two causes do not appear as a fault in the reports.

### Records that stay late

One record stayed above 10 min after the idle pod bucket: `i2.27` on `pod-027`, seed 5, start tick 182869, 17.1 min long.

| Ticks from the start | Minutes | Phase | Reading |
| ---: | ---: | --- | --- |
| 0 | 8.5 | `deferred` | `Pod ahead` `pod-028`, and the walk ends at `pod-013`, stopped with no wait |
| 30551 | 0.2 | `deferred` | `Pod ahead` and `Junction traffic`, and the queue moves |
| 31271 | 0.7 | `bound` | Moving |
| 33911 | 6.1 | `bound` | `Pod ahead`, and the walk reaches `Blocked by incident` |
| 55751 | 1.6 | `bound` | `Pod ahead` and moving, as the queue drains, then the unload |

A second run of the seed probed `pod-013`.
It is occupied and in service on `link-05-06`, and it has not chosen a berth at `station-06`.
Its berth choice fails with `destination is unreachable` while the faults are active, and it succeeds with the blocked set empty.
So this time also comes from a fault, and the record has 2.5 min that no fault explains.
The probe covered only this stop.
Records `i6.168` (4.4 min) and `i4.107` (3.7 min) have similar stops with no probe, and they count as time not blocked in the table.

### Two findings for the maintainer

Neither finding is fixed here, and neither is a contract violation that this study shows.

An idle pod with an order that cannot board keeps its berth, and an emergency pod waits for that berth.
`clearBlockedBerths` skips a blocker that a waiting trip names, and a failed leg keeps the trip with `Waiting for destination access`, as the suspension contract approves.
The fault can be anywhere on the route of that order.
This cost 153.7 and 121.5 min in 11 and 13 late records.
In record `i6.176` of seed 1, `pod-025` waited 19.7 min for the berth of `pod-020`, which boarded only when the last fault cleared at tick 1299600.
No contract row names this combination, and this study did not check whether the emergency pod had another free berth.

A pod whose terminal berth choice fails reports no wait when no berth of its station is blocked, as `reportBlockedBerths` states.
It stops on the running lane with an empty report, so the chain walk does not find a fault.
`pod-013` held `pod-027` and `pod-028` in this way for 8.5 min.

### Fixes of the two findings

Commits `cafe57b` and `29a3964` fix the two findings, after the maintainer approved the contract rows on 2026-11-05.
The table compares `f604fab` with the fixes on `rail-hub` `incidents`, 6 simulated hours, every-tick checks on.

| Seed | Late before | Late after | Completed before | Completed after |
| ---: | ---: | ---: | ---: | ---: |
| 1 | 11 | 4 | 626 | 691 |
| 2 | 6 | 4 | 759 | 656 |
| 3 | 15 | 14 | 589 | 618 |
| 4 | 13 | 9 | 620 | 635 |
| 5 | 11 | 15 | 641 | 639 |
| Total | 56 | 46 | 3235 | 3239 |

The report fix alone changed neither count on seeds 1 and 5, so the change comes from the move of the bound idle pod.
The run diverges after the first such move, so most late records differ between the two columns.
Five seeds do not prove the fall of 10 records.
The rise on seed 5, with a longest record of 34.1 min against 24.0 min, is not attributed.
No run gave a Safety, Contract, Conservation, or Rail report, and each seed passed its 6 rewinds on the runs that recorded them (seeds 2 to 4).

The fix acts only when the emergency pod itself waits on the idle pod.
An emergency pod behind an ordinary pod that waits on the idle pod still waits: on seed 1 this wait rose from 191 s to 1630 s, while the direct wait fell from 4043 s to 361 s.
An idle pod with no route out keeps its berth, and the emergency pod reports `No parking available`.

## Readings from a quiet host

These readings ran at `184d525` on 2026-11-05, after the full gate, with a load average from 2 to 6.
Each worst-case row ran 4 times, and the table gives the range of each value.

| Row | Median | Reference median | Slowest | Reference slowest |
| --- | ---: | ---: | ---: | ---: |
| 64 debris and 300 pod faults, limit 50 µs (A7) | 10.86 to 10.98 µs | 6.59 to 6.71 µs | 31 to 74 µs | 82 to 92 µs |
| 4 emergencies, `rail-hub` | 2.42 to 2.46 µs | 2.03 to 2.06 µs | 25 to 26 µs | 24 to 25 µs |
| 4 emergencies, `london-central` | 14.3 to 14.6 µs | 13.4 to 13.9 µs | 1.04 to 1.11 ms | 1.06 to 1.41 ms |
| 64 debris and 1,000 pod faults, reported | 34.4 to 34.8 µs | 20.2 to 20.3 µs | 95 to 122 µs | 411 to 743 µs |

The suspension row meets A7, and no tick passes twice the slowest reference tick.
The `rail-hub` emergency row is 18 to 20 percent above its reference median, so it misses the rule of 10 percent, by about 0.4 µs.
The `london-central` emergency row is 3 to 8 percent above its reference median.

`TestIncidentSoakTimed` ran 3 seeds of 0.1 simulated hours on each preset, with faults and emergencies on.

| Preset | Median | Reference median | Slowest | Reference slowest |
| --- | ---: | ---: | ---: | ---: |
| `rail-hub` | 76 to 82 µs | 81 to 93 µs | 2.0 to 4.0 ms | 1.5 to 3.0 ms |
| `london-central` | 142 to 145 µs | 133 to 143 µs | 13.7 to 19.1 ms | 16.1 to 18.2 ms |

Each median is at most 7.2 percent above its reference.
The slowest tick of `rail-hub` seed 1 is 3.97 ms against 1.49 ms, which is 2.7 times its reference, so that seed misses the rule of twice.
The other five seeds meet it.

## Readings from a busy host

These readings are not results of a gate.
The Performance rows need a quiet machine (A3).

| Reading | Value | Load average |
| --- | --- | ---: |
| Sampled tick, each policy, cold and warm, both networks | Slowest 0.95 ms, limit 50 ms | 18 |
| Sampled tick at the project limits, cold | 260 to 429 ms, reported against 50 ms | 15 |
| Sampled tick at the project limits, warm | 22.2 s free flow, 15.6 s congestion, 32.5 s queue, 13.5 s predictive, reported against 50 ms | 15 |
| Timed run, rail-hub, 3 seeds, 0.1 simulated hours | Median 81 to 87 µs against 88 to 98 µs | 16 |
| Timed run, london-central, seed 1, 0.1 simulated hours | Median 154 µs against 145 µs | 16 |
| 64 debris and 300 pod faults | Median 121 µs against 6.6 µs, slowest 1.25 ms against 1.71 ms | 7 |
| 4 emergencies, london-central | Median 21 µs against 14 µs, slowest 2.9 ms against 3.5 ms | 7 |
| 4 emergencies, rail-hub | Median 2.5 µs against 2.3 µs, slowest 27 µs against 48 µs | 7 |
| 64 debris and 1,000 pod faults | Median 983 µs against 20 µs, slowest 5.0 ms against 0.62 ms | 7 |

The sampled-tick fixture has little traffic, so its step is cheap.
The two rows at the project limits come from `TestSampledTickNoCandidateLatency` at `3c61bcb`, are reported, and are not gated (P23).
The row of 300 pod faults misses the rule of 10 percent, against a reference step that does almost no work.
