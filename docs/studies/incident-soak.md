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
- The maintainer approved rule A of amendment A8.
  The section "Run of rule A" has its run on `rail-hub`, and the soak at 30 Hz decides the Progress row.
- Amendment A10 changes the cost and the shape of the soak.
  The section "First soak in the branch shape" has its first run.
- Express has a soak, with no report, and with no row of the contract as its gate.
  The section "First soak of Express" has its numbers.
- The Performance row for suspension meets amendment A7 at 30 Hz in the section "Readings from a quiet host".
- The Performance row for emergencies meets the median rule of amendment A11 at `8ffb20b6`, and missed the rule for the slowest tick on `rail-hub` in 2 of 24 runs, so A11 now has a floor of 1 ms for that rule.
  The row passes at `5979debb` by the ruling in the section "Readings from a quiet host", with one miss in 56 runs of `london-central` on record.
  The readings at 30 Hz missed the earlier rule, on `rail-hub` in 3 of 4 runs and on `london-central` in 1 of 4 runs.
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
`mise run qualify` also runs `TestSampledTickLatency`, in about 3 seconds.
`mise run qualify:limits` runs `TestSampledTickNoCandidateLatency`, which took 386 seconds at a load average of about 15.

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

## Run of rule A

This run tests rule A of amendment A8 at `c4a5b65`, on `rail-hub`, with arms `incidents` and `platoons`.
It has 5 seeds of 6 simulated hours and the clear of 30 simulated minutes, with the every-tick checks on.
All 10 runs completed, and a temporary read-only probe ran with them.
`london-central` did not run.

"Over 10 min" counts the records whose total length passes 10 minutes, the rule of the row before A8.
This column also counts platoon members, which the gate before A8 skipped.

| Seed | Arm | Records | Late under rule A | Longest unblocked, min | Completed orders | Over 10 min |
| ---: | --- | ---: | ---: | ---: | ---: | ---: |
| 1 | `incidents` | 74 | 0 | 3.7 | 691 | 4 |
| 2 | `incidents` | 97 | 0 | 3.3 | 656 | 4 |
| 3 | `incidents` | 88 | 0 | 4.7 | 618 | 14 |
| 4 | `incidents` | 85 | 0 | 5.2 | 635 | 9 |
| 5 | `incidents` | 85 | 0 | 4.6 | 639 | 15 |
| 1 | `platoons` | 77 | 0 | 3.5 | 689 | 9 |
| 2 | `platoons` | 87 | 0 | 3.1 | 672 | 8 |
| 3 | `platoons` | 95 | 0 | 4.5 | 653 | 13 |
| 4 | `platoons` | 99 | 0 | 4.2 | 717 | 13 |
| 5 | `platoons` | 90 | 0 | 4.1 | 695 | 7 |

No run gave a Safety, Contract, Conservation, or Rail report.
Each run passed its 6 saves, with no demotion, and its 6 rewinds, and each record ended.
The preset has no rail service, so the Rail row has no record.
No record passed the limit on a platoon member.
The `incidents` counts of completed orders and of records over 10 minutes equal the section "Fixes of the two findings", so the classifier and the probe did not change the run.
This run marks no row as passed.

### The report of a failed berth choice

The stopped pod has no test entry, because `admit` writes the report at each tick on which the berth choice fails while the blocked set is not empty.
That report does not show that a fault caused the failed search, so the probe repeated each search with the blocked set empty.
An episode is a run of readings of one record whose chain stops at one pod with the report.
A late candidate is a record over 10 minutes.

| Report on the chain of a late candidate | Episodes | Readings | Episodes that fail with an empty blocked set |
| --- | ---: | ---: | ---: |
| `No forward route`, failed berth choice | 2 | 1023 | 0 |
| `No forward route`, failed next leg | 0 | 0 | 0 |
| `Blocked by incident`, failed berth choice | 1 | 238 | 0 |

So the classifier hid no real wait through this report in this run, but 3 episodes on one preset are a small sample.
The probe also saw 74 other `Blocked by incident` episodes on pods with no berth, in which the berth search succeeded, so a fault on the track ahead wrote that report, and the table leaves them out.
In 42 readings, a chain ended at a traveling pod with no report and no berth, and the search of the probe failed.
The probe did not check whether `admit` searched for that pod, or whether the pod stood still.
Each of these readings counts as not blocked.
The section "Chains that end at a traveling pod with no berth" answers both questions: `admit` did not search for these pods, and they did not stand still.

### The idle pod

The entry named an idle pod in 1160 to 5728 readings of each run, and it found no idle pod at a chain end with an order that can board.
Only the unit tests cover that case.
A run of `incidents` seed 3 with the entry turned off gave 4 late records, and a longest unblocked time of 17.0 min, against 0 and 4.7 min.
Its other numbers stayed the same: 88 records, 618 completed orders, and 14 records over 10 minutes.
Each of the 4 chains ended at an idle pod.

The section "Coverage of the rare paths" replaces the second sentence of this part.

### Chains that end at a traveling pod with no berth

A temporary read-only probe, now removed, repeated the case of the 42 readings at `08c3b538`, at 30 Hz, with the check stride of A10.
The probe did not change the run: each count of the branch shape equals the table of the section "First soak in the branch shape".
The earlier run was at `c4a5b65`, at 60 Hz, so its 42 readings are not comparable one to one.
A reading counts here when the chain has at least one blocker and ends at a traveling pod with no report and no berth, and when the search of the probe fails with the active faults.

| Shape | Arm | Readings | Records | Readings on a record over 10 min | Longest not blocked of these records, s |
| --- | --- | ---: | ---: | ---: | ---: |
| 5 seeds, 6 h | `rail-hub` `incidents` | 92 | 5 | 66 | 226.8 |
| 5 seeds, 6 h | `rail-hub` `platoons` | 56 | 3 | 22 | 108.7 |
| Branches, 1 seed | `rail-hub` `incidents` | 11 | 1 | 0 | 128.6 |
| Branches, 1 seed | `rail-hub` `platoons` | 23 | 3 | 12 | 162.7 |
| Branches, 1 seed | `london-central` `incidents` | 0 | 0 | 0 | none |

The count at this tip is 148 readings in 13 episodes for the shape of the 42 readings, and 88 of them are on a record over 10 minutes.
An episode is a run of readings of one record whose chain ends at one pod.

- `admit` did not search for any of these pods.
  In each of the 182 `rail-hub` readings, `assignTerminalBerth` returned at the test of `internal/sim/berth_choice.go:27-29`, because the next reservation does not reach the end of the route, and `earlyBerthChoice` is false.
  The search of the probe predicts a later choice, and it is not a failure that the pod has met.
- No pod stood still.
  Each pod moved between the reading and the previous reading, and its speed was from 0.07 to 14 m/s.
- The search succeeds with an empty blocked set in all 182 readings, so only the faults make it fail.
  A pod that reaches its terminal lane while they last reports `No forward route` or `Blocked by incident`, and the classifier reads that report.
- A queue that drains holds the emergency pod.
  Each pod between it and the chain end reports `Pod ahead` or `Junction traffic`, and the speeds rise from one reading to the next.
  In 10 of the 13 episodes, the last blocked reading of the record was `Blocked by incident`, from 1 to 30 s earlier.
  The other 3 episodes have no blocked reading before them.

A typical chain, at tick 430890 of `platoons` seed 3: `pod-030` reports `Pod ahead` `pod-014` at speed 0, and `pod-014` moves at 0.4 m/s with no report and no berth.
One second earlier, `pod-030` reported `Blocked by incident` `i5.122`.

Amendment A8 names a stopped pod whose berth choice fails.
These pods do not stop, and they make no choice, so each reading is correctly not blocked.
This is not a gap of the classifier and not a defect of the simulation.
This time has little effect: one record has at most 26 of these readings, and the longest not-blocked time of these records is 226.8 s, against the limit of 600 s.
If all of the readings counted as blocked, no late count would change, because each count is already 0.

### Coverage of the rare paths

`internal/session/incident_soak_paths_test.go` reaches these paths on the example network, with scripted commands, through the session, the simulation, and `watchEmergencies`.
Pod 02 is idle at the only berth of `market`, debris on `market-out` keeps it there, and pod 01 has an emergency and needs that berth.

| Path | Fixture | A run of the soak can reach it |
| --- | --- | --- |
| `Idle pod, leg free`, no order | Only `TestSoakReadingPaths`, since A13 | Before A13, yes, see below. Since A13, only when the pod has no move with no faults too, which no run showed |
| `Idle pod, leg free`, order with a free leg | `TestSoakPathLegFreeWithOrder`, read at a command boundary | No |
| `Fault at the end of a chain`, pod with a fault | `TestSoakPathFaultAtChainEnd`, read at a command boundary | No |
| `Fault at the end of a chain`, fault ID | None | No, dead |
| Entry false, leg also fails with an empty blocked set | None, only `TestBoardingLegBlocked` | No, with no class restrictions |

The driver reads only state that `admit` has written.
`watchEmergencies` runs after the step, or from `check` when the count of ended records changes, and a command does not change that count.
The sampler starts a fault in `faultStage`, before `admit` in the same step.

- An order with a free leg: `boardOrWait` boards the order in the dispatch of that tick, before the reading.
  After a clear at a command boundary, the entry returns false for one read, and the next tick boards the order.
- A pod with a fault: `refuseGrant` writes `Blocked by incident` when the owner has a fault, so the walk stops at the waiting pod.
  The two other writers of a pod ID in `BlockedBy` are the platoon cases of `grant`, and `podFaultTarget` and `canLink` keep a fault out of a platoon.
  The fixture also shows that the same fault in the order of the driver gives the incident path.
- A fault ID: `reportFault` writes it on a pod with a fault, which the walk finds first, and `reportIncident` and `reportBlockedBerths` write it with `Blocked by incident`, which the walk also finds first.
  The writes of `WaitReason` alone in `publishVehicleTravel` and `clearBlockedBerths` do not meet a fault ID, because the first needs a pod that `admit` cleared at `internal/sim/traffic.go:579`, and the second needs `Berth occupied`.
- The empty blocked set: `validatePassengerRoutes` checks each pair of passenger berths, and `keepTripPod` takes the trip from a pod for which `podFitsRequest` is false.
  A network with class restrictions can have a berth with no route for the class of its pod, and this study did not build that case.

The part "The idle pod" says that the entry found no idle pod at a chain end with an order that can board, and that stays true.
The path also has a case with no order, which that part did not measure.
That case fired at this tip in the shape of amendment A6, on `platoons` seed 5, in 348 readings of one record, and in none of the 32 other runs of this section.
Record `i6.154` on `pod-018` has 469.9 s not blocked and no blocked time, so 348 s of it are this path, and no run has a closer approach to the limit of 600 s.

The chain at tick 554430: `pod-018` reports `Pod ahead` `pod-021`, `pod-021` reports `No parking available` `pod-023`, and `pod-023` is idle at `parking-01` with no order.
At each of the 348 readings, `moveBlocker` failed on a clone with the active faults and succeeded with an empty blocked set, and pod faults were the only active faults.
So the faults hold this record, but amendment A8 names only an idle pod with an order, so this time counts as not blocked.
Before amendment A13, a fixture on this path showed the result of a longer wait: the gate fired at 10 minutes on a wait that a fault causes.

This is a gap in the words of the rule, in the direction of a false alarm, and it does not hide a real wait.
It is not a defect of the simulation, because the section "Fixes of the two findings" records that such a pod keeps its berth.
The maintainer added this case to the rule as amendment A13: an idle pod with no order whose empty move fails only because of the active faults.
`TestSoakPathMoveBlocked` now shows such a wait as blocked in the driver loop, with no late record past the limit.
A run of `platoons` seed 5 at `fda1374f` gives `moveBlocked=348` and `legFree=0`, so the same 348 readings now count as blocked.
The longest not-blocked time of that run falls from 469.9 s to 247.0 s, with 0 late records.
The log does not list `i6.154` alone, so the arithmetic of about 122 s for that record is not measured.
The branch shape has 0 readings of this path, so its numbers do not change, and the late count stays 0 in all runs.
The entry is `EmptyMoveBlockedForTest` in `internal/sim`.

Before A13, the late count was 0 in all 10 runs of the shape of A6 at `08c3b538`, and the longest not-blocked time was 293.9 s on `incidents` and 469.9 s on `platoons`.
These runs used the check stride of A10, so they are not the soak with the full check at each tick that A12 requires before a release tag.

## Cost and shape of the soak

These readings support amendment A10.
All are at 30 Hz, on `rail-hub`, at a load average of about 4 to 12.
The rates of the sections above are at 60 Hz, so they do not compare.

### Cost of the checks before the change

The run is `incidents` seed 1, 0.5 simulated hours and the clear, at `c3c647b`, with one CPU profile.

| Reading | Value |
| --- | ---: |
| `soakDriver.check`, share of CPU samples | 53% |
| `ExportState`, share of CPU samples | 39% |
| `Simulation.Step`, share of CPU samples | 25% |
| Background garbage collection, share of CPU samples | 11% |
| Process CPU, `GOGC=100` | 62.6 s |
| Process CPU, `GOGC=400` | 47.1 s |

The driver called `ExportState` twice for each tick, once in `CheckContract` and once for the order balance.
The review gave about 80% for the checks and a factor of 2 for `GOGC=400`.
This run gives 53% in `check`, and about 64% if all the garbage collection belongs to it, and a fall of 25%.
`soakCPUSeconds` read the whole process, and `startChecks` draws from the driver stream, so a run with the checks off is not the same run as a run with them on.
A run with the rewinds off also moves each driver event against the simulation tick.

### Order balance

`CheckContract` does not prove the equation of the Conservation row.
It requires `RequestID - Completed - Interrupted - queued - aboard` to equal `unaccountedOrders`, and a restore that drops orders makes that count above 0.
So the driver computes the equation from the one export.
A temporary change that added 1 to both `requestID` and `unaccountedOrders` passed the contract check and failed the order balance.

### Trial of the stride

The trial has `incidents` seed 1, one warm save, and 2 branches, at parallel 2, with the body of the soak task.
Each column of a row has the same run: the two modes have the same commands, completed orders, interrupted orders, and settle times.
The queue reads 200 at the warm save.
The rate is the harmonic mean of the windows of 5 simulated minutes with a queue of at least 190, in ticks for each second of wall time.

| Mode | `GOGC` | Rate, full queue | Slowest and fastest window | Rate, whole branch | Wall time of a branch | Peak resident memory |
| --- | ---: | ---: | --- | ---: | ---: | ---: |
| Stride | 400 | 4,068 | 2,227 to 9,690 | 4,650 | 46 s | 231 MB |
| Each tick | 400 | 2,686 | 1,629 to 4,215 | 2,860 | 75 s | 231 MB |
| Stride | 100 | 3,313 | 1,492 to 7,973 | 3,845 | 56 s | 208 MB |

A branch has 216,000 ticks, and about 7,500 full checks with the stride.
The review estimated 6,000 ticks a second.
This reading is 4,068 at a full queue.
The gain from `GOGC=400` is 23% in this rate and 25% in CPU, not the factor of 2, so the maintainer kept the default `GOGC=100` on 2026-11-05.

| Branch | Completed | Interrupted | Commands | Records | Late under rule A | Longest unblocked, min | Over 10 min | Settle, s |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 318 | 37 | 34 | 23 | 0 | 2.1 | 0 | 1 |
| 2 | 243 | 39 | 56 | 16 | 0 | 2.8 | 3 | 228 |

No run gave a Safety, Contract, Conservation, or Rail report, and each branch passed its 9 rewinds.
One of the 2 saves of branch 2, at tick 80810, restored in the logical tier, and the driver counts it and goes on.
The physical error reads "the restore would demote pod pod-021, which has emergency i6.16".
Later work found the cause: pod-021 rested in the first cell of its final lane, because debris made its berth choice fail, and the physical tier demoted each pod with no berth in that state.
The tier now keeps such a pod, and it demotes a pod with no berth only when no block is left to reserve.
Before that change, a run with a save at each simulated minute had 24 logical fallbacks in 1,281 saves over 14 branches, in 3 episodes, and 4 in 732 saves over 6 runs with no restore.
At 60 Hz, the same 3 episodes occurred, so the tick rate and the branch path are not the cause.
A warm save that demotes in this way stops its arm, because a branch must start with the physical tier.

### Divergence of the branches

A temporary probe compared 3 branches of each of two arms, by simulated tick.
The state of a pair first differs 18 to 61 s after the warm save, at the first driver command of one branch.

| Minutes after the warm save | `incidents`, pods at another place | `emergencies`, pods at another place |
| ---: | ---: | ---: |
| 5 | 10% to 60% | 17% to 30% |
| 15 | 57% to 100% | 50% to 57% |
| 20 | 83% to 100% | 57% to 63% |
| 60 | 87% to 93% | 77% to 90% |

So the state of the branches diverges in 15 to 20 of their 120 simulated minutes.
These numbers are from the time when only the driver reseeded.
Then, 13 to 17 of the 45 to 51 `incidents` records of a pair had the same start tick and kind, and 10 to 15 also had the same pod or lane.
On `emergencies`, the numbers were 5 to 7 of 15 to 22, all with the same pod.

Each branch now also reseeds the fault sampler, the emergency sampler, and demand.
On seed 1, 2 of the 52 to 59 `incidents` records of each pair have the same start tick and kind, and on `emergencies`, 0 of 16 to 22.
In the one pair read, the 2 records are faults of the warm time, which each branch inherits.
All 10 branches still share the warm state.

### Estimate of the wall time

This is an estimate, and the new soak has not run.
The `rail-hub` routine soak has 3 armed warm saves, 30 branches, and 2 reference runs: 6.6 million ticks at about 4,400 a second, and 0.54 million at about 5,600.
That is about 1,600 s of one thread, and about 9 min at parallel 3.
The `london-central` part has no reading at 30 Hz or with the stride.
With the rates of 60 Hz under load, about 265 ticks a second for `incidents` and 500 for `emergencies`, it is about 3,200 s of one thread.
The whole routine soak is then about 30 min at parallel 3 on a free host, and up to about 1 h at a high load.
So the limit of 2 hours stays.
`mise run soak:full` needs about 8 hours of one thread by the same rates, or about 2.7 h at parallel 3, so it needs a longer `PODSIM_SOAK_LIMIT` to complete each seed.

### First soak in the branch shape

`mise run soak` ran at `2cf61db` on 2026-11-05 at 30 Hz, with the default `GOGC`, in 868 s of wall time, at a load average from 10 to 31.
This is one seed in the shape of amendment A10, so `rail-hub` has 10 branches for each arm, and `london-central` has 3.

| Preset and arm | Branches | Records | Late under rule A | Longest not blocked, s | Over 10 min in all | Completed | Interrupted | Rewinds | Saves | Demoted |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `rail-hub` `incidents` | 10 | 216 | 0 | 230.5 | 29 | 2,524 | 409 | 90 | 16 | 0 |
| `rail-hub` `platoons` | 10 | 248 | 0 | 230.4 | 27 | 2,819 | 425 | 90 | 16 | 0 |
| `rail-hub` `emergencies` | 10 | 233 | 0 | 219.6 | 0 | 5,057 | 323 | 90 | 16 | 0 |
| `london-central` `incidents` | 3 | 50 | 0 | 197.9 | 0 | 3,153 | 116 | 18 | 3 | 0 |
| `london-central` `emergencies` | 3 | 34 | 0 | 238.7 | 0 | 3,374 | 58 | 18 | 3 | 0 |

Each branch passed, and no run gave a Safety, Contract, Conservation, Rail, or Persistence report.
Rail has no record on these presets.

| Rule A readings | `rail-hub` `incidents` | `rail-hub` `platoons` | `london-central` `incidents` |
| --- | ---: | ---: | ---: |
| Own fault | 3,873 | 2,933 | 0 |
| `Blocked by incident` | 26,737 | 23,232 | 65 |
| `No forward route` | 654 | 397 | 0 |
| Fault at the end of a chain | 0 | 0 | 0 |
| Idle pod, leg blocked | 7,376 | 1,769 | 0 |
| Idle pod, leg free | 0 | 0 | 0 |
| Not blocked | 18,166 | 19,894 | 3,761 |
| Failed berth choice, and the retry with no faults succeeds | 13,245 of 13,245 | 11,117 of 11,117 | 65 of 65 |

Each arm with faults off has only `Not blocked` readings.
The retry shows that no failed berth choice in this run had a cause other than the faults.
The paths `Idle pod, leg free` and `Fault at the end of a chain` did not fire, so only unit tests cover them.
The section "Coverage of the rare paths" now has fixtures for these paths.

`mise run soak` ran again at `44030345` on 2026-11-06, after the warm tick work, in 795 s of wall time, at a load average from 9 to 21.
Every count in the two tables above is the same in this run, and each branch passed with no report.
That is the expected result for the same seed, because the warm tick work claims no change of result, and these presets have no banks.

### First soak of Express

Express had no soak before this one, by the choice of the maintainer on 2026-11-06.
No preset can run an Express pod, so a test builds each project.
`NewFleet` and a project with no marker refuse the class with "vehicle physical profile is not supported" on each network.
With the marker, each ring and mesh preset refuses the pods at their berths, `compact-station` refuses at its maneuver group, and both London presets refuse because the Express contract allows no depot.
So this soak has no London row.
No contract rule refuses an Express pod from a fault or an emergency.

| Project | Built from `rail-hub` with |
| --- | --- |
| `rail-hub-express` | The `express-v1` marker, each class on each lane, station, and berth, and 30 Express pods. Each other setting stays, so demand is 12 orders a minute. |
| `rail-hub-express-parties` | The same, with demand at 1 order a minute, a pooling limit of 4 parties, two service pairs, and a shared party order of 1 to 20 passengers each minute from the driver, also in the reference arm. |

Generated demand submits parties of one, so the first project never has a standee or a load dwell above the base value.
The second project exists for that reason.
`PODSIM_SOAK_PRESETS` selects each project, and the default of `mise run soak` holds neither, so its wall time does not change.

The shape is the `rail-hub` shape of amendment A10, with the same checks, rule A as it stands, and the same seed policy.
A large class forms no platoon link, so neither project has the `platoons` arm.
Seed 1 is the run of that policy, and seed 2 is a second reading.
The projects are at `a10d8561` for `rail-hub-express` seed 1, and at `0debd796` for the other rows.
The maintainer asked during this work that the later jobs run under `nice -n 10`, so the wall times of seed 2 are not comparable with seed 1.
A wall time is the time of the whole run, and the load is the load average at its start and its end.

| Project, arm, and seed | Branches | Records | Late under rule A | Longest not blocked, s | Over 10 min in all | Completed | Interrupted | Rewinds | Saves | Demoted | Wall, s | Load |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| `rail-hub-express` `incidents` 1 | 10 | 217 | 0 | 307.4 | 32 | 2,500 | 417 | 90 | 16 | 0 | 735 | 15 to 22 |
| `rail-hub-express` `emergencies` 1 | 10 | 243 | 0 | 208.8 | 0 | 5,094 | 323 | 90 | 16 | 0 | 735 | 15 to 22 |
| `rail-hub-express` `incidents` 2 | 10 | 241 | 0 | 380.0 | 24 | 2,951 | 436 | 90 | 15 | 0 | 1,315 | 39 to 40 |
| `rail-hub-express` `emergencies` 2 | 10 | 266 | 0 | 216.1 | 0 | 4,903 | 367 | 90 | 14 | 0 | Under 900 | 29 to 37 |
| `rail-hub-express-parties` `incidents` 1 | 10 | 223 | 0 | 306.8 | 20 | 2,195 | 371 | 90 | 16 | 0 | 586 | 37 to 34 |
| `rail-hub-express-parties` `emergencies` 1 | 10 | 188 | 0 | 217.0 | 0 | 2,301 | 268 | 90 | 16 | 0 | 586 | 37 to 34 |
| `rail-hub-express-parties` `incidents` 2 | 10 | 203 | 0 | 297.4 | 20 | 2,208 | 369 | 90 | 15 | 0 | 573 | 29 |
| `rail-hub-express-parties` `emergencies` 2 | 10 | 204 | 0 | 199.6 | 0 | 2,348 | 234 | 90 | 14 | 0 | 573 | 29 |

Each branch passed, and no run gave a Safety, Contract, Conservation, Rail, or Persistence report.
Rail has no record on these projects.
The driver had 27 to 42 commands refused in each `incidents` arm, and 1 to 4 in each `emergencies` arm.
It keeps no reason for a refusal of an incident command, so these reasons are not known, and no party order was refused.
The scratch control below has 33 refusals, so the count is not special to Express.
Each warm save restored in the physical tier, and a test shows that a branch keeps the marker and the class.
The first attempt at `rail-hub-express` seed 2 stopped at the 900 s limit of my job, with its `emergencies` arm complete, so that arm is from that run and its `incidents` arm is from a later run with a limit of 2,200 s.

| Rule A readings, `incidents` | `rail-hub-express` 1 | `rail-hub-express` 2 | `rail-hub-express-parties` 1 | `rail-hub-express-parties` 2 |
| --- | ---: | ---: | ---: | ---: |
| Own fault | 3,161 | 6,104 | 2,943 | 1,984 |
| `Blocked by incident` | 33,553 | 27,994 | 18,609 | 18,799 |
| `No forward route` | 768 | 441 | 1,179 | 606 |
| Fault at the end of a chain | 0 | 0 | 0 | 0 |
| Idle pod, leg blocked | 4,523 | 4,195 | 5,791 | 3,608 |
| Idle pod, leg free | 0 | 0 | 90 | 333 |
| Not blocked | 19,089 | 20,927 | 20,579 | 19,451 |
| Failed berth choice, and the retry with no faults succeeds | 18,777 of 18,777 | 13,507 of 13,507 | 10,033 of 10,033 | 10,187 of 10,187 |

| Reach of the party orders | Seed 1 `incidents` | Seed 1 `emergencies` | Seed 2 `incidents` | Seed 2 `emergencies` |
| --- | ---: | ---: | ---: | ---: |
| Orders accepted, none refused | 913 | 913 | 907 | 907 |
| Largest load of a pod | 20 | 20 | 20 | 20 |
| Most parties aboard a pod | 3 | 2 | 3 | 2 |
| Pod readings with standees, each second | 170,483 | 127,313 | 180,086 | 131,766 |
| Fault records on a pod with standees | 20 | 0 | 16 | 0 |
| Emergency records on a pod with standees | 45 | 41 | 46 | 39 |

A replay of a rewind can count a record of the last two rows again with a new ID, so they are upper bounds.

On `rail-hub-express`, the reference queue reaches the limit of 200 orders, so the queue check after the clear cannot fail there.
The same holds for the existing arms: a short run of `rail-hub` and `london-central` has a reference queue that reaches 200 in each arm.
On `rail-hub-express-parties`, the reference queue reaches 8 and 7, and each `incidents` branch passes that check, with a longest time of 1,036 s and 1,134 s of the 30 minutes.

A scratch control, which did not land, ran plain `rail-hub` at 2 orders a minute with seed 1 and 10 branches of `incidents`.
Its reference queue reaches 6, and 4 of the 10 branches miss the queue check, with 9 to 15 orders left after 30 minutes and a queue of 65 to 70 in the branch.
So this check can fail once demand is below the fleet supply, and the pass of the Express party project does not show that Express drains better than Legacy, because the two projects differ in pooling and in party size.
I read these misses as a backlog that drains at the rate of the fleet, but I did not measure the drain.

These runs were before amendment A13.
The path `Idle pod, leg free` fired here, as it did on `platoons` seed 5 in the section "Coverage of the rare paths".
A logging copy of seed 1 branch 3 and of seed 2 branch 9 gives one record for each: 90 s and 237 s.
In each, an occupied pod with an emergency reports `No parking available` and names as its blocker an empty pod that is ready at a berth.
Rule A counts this time as not blocked, so it counts against the limit, and the longest of those records stayed under the limit.
A rerun of seed 2 branch 9 at `58014d25`, with A13, gives `moveBlocked=237` and `legFree=0`, so that branch is the case of A13.
The longest not-blocked time of that branch is 203.7 s.
The other 96 readings of seed 2 have no logging run.
The same control has this path too, with 3 readings of the same form on a Legacy pod, so it is not special to Express.
The runs at the preset demand have no such reading, and I did not test why.

This soak does not show the following:

- It is no gate.
  No row of the stage 7 contract names an Express project, so it marks no row as passed.
- It has no London row, no bank, no mixed fleet, and no platoon.
- The party orders are one rate and one size range, chosen for this soak, and they stop at the clear.
  A pod carried at most 3 parties, so the pooling limit of 4 was not reached.
- The fleet is all Express, so the run does not reach a stranded service order of the kind in `TestIncidentSaveStranded`.
- The full check runs at the stride of amendment A10, and not at each tick.
- Express still has no byte proof, and these warm saves of 8 to 12 KB are not a worst case.
- The wall times come from a busy host, so they are no Performance reading.

## What the soaks found

The maintainer asked on 2026-11-06 whether long runs find more.
No finding needed a long run, but the study did not record the simulated time of each first miss, so the earliest time of the late emergencies is not known.

| Finding | Kind | When or how |
| --- | --- | --- |
| Rewind failures | False alarm of the check | 2.9 and 4.6 simulated hours, at 60 Hz |
| Late emergencies, and two defects behind them | Defects | First soak, with 50 of 335 records late in the `rail-hub` `incidents` arm |
| Restore demotion | Defect | About 1.9 percent of saves, in a run that saves often |
| Idle pod entry wiring | Test coverage | A soak of 13 minutes of wall time shows it |
| A queue check after the clear that cannot fail at a full queue | Limit of the check | The first soak of Express, whose reference queue reaches 200, as the existing arms do |

The Safety, Contract, Conservation, and Rail rows have no report in any soak.
Amendment A12 keeps `soak:full` only for a release tag.
A run of it at `4512864` stopped on 2026-11-06 by that decision, after 31 minutes of wall time, with no finished seed and no report, so it did not pass.

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

### Readings at 30 Hz

These readings ran at `b9730fb` on 2026-11-06, with a load average from 2 to 3.
Each worst-case row ran 4 times, and each run has 300 measured ticks with the records and 300 without.

| Row | Median | Reference median | Slowest | Reference slowest |
| --- | ---: | ---: | ---: | ---: |
| 64 debris and 300 pod faults, limit 50 µs (A7) | 10.72 to 10.99 µs | 6.70 to 6.87 µs | 32 to 41 µs | 87 to 98 µs |
| 4 emergencies, `rail-hub` | 2.77 to 3.81 µs | 2.40 to 2.97 µs | 26 to 30 µs | 25 to 77 µs |
| 4 emergencies, `london-central` | 14.8 to 16.4 µs | 14.1 to 14.3 µs | 1.02 to 1.46 ms | 1.06 to 1.33 ms |
| 64 debris and 1,000 pod faults, reported | 34.1 to 34.7 µs | 20.4 to 20.5 µs | 87 to 104 µs | 560 to 572 µs |

The suspension row meets A7.
No tick passes twice the slowest reference tick of its own run.
The `rail-hub` emergency medians are -0.4, +15, +58, and +16 percent against the reference of the same run, so 3 of 4 runs miss the rule of 10 percent.
The `london-central` emergency medians are +7, +4, +15, and +7 percent, so 1 of 4 runs misses it.
The third run has the highest ratio on both presets, and the load average rose from 2.2 to 3.0 during it.
These readings miss the rule of 10 percent.
Amendment A11 replaced that rule on 2026-11-06 with a rule on 4 runs, after these readings, so the median differences of +0.38 µs and +0.95 µs do not confirm it.

`TestIncidentSoakTimed` ran 3 seeds of 0.1 simulated hours and the 30 minutes of clear on each preset, with faults and emergencies on.

| Preset | Median | Reference median | Slowest | Reference slowest |
| --- | ---: | ---: | ---: | ---: |
| `rail-hub` | 76 to 82 µs | 82 to 93 µs | 2.0 to 4.1 ms | 2.4 to 2.8 ms |
| `london-central` | 147 to 149 µs | 140 to 149 µs | 16.3 to 23.2 ms | 12.3 to 17.5 ms |

Each median is at most 7.0 percent above its reference.
Each slowest tick is at most 1.7 times its reference, on `rail-hub` seed 1.
So all 6 seeds meet both rules.
The miss of `rail-hub` seed 1 at 60 Hz, 2.7 times its reference, does not occur in this run.
This is one run of each seed, so it does not show that the rate removed the miss.

### Readings of amendment A11 at `8ffb20b6`

These readings ran on 2026-11-06 with `PODSIM_SOAK_GATE=1`, with a load average from 2 to 3.3.
The first run covered each row, and then the `rail-hub` row ran 5 more times by itself.

| Row | Runs | Median difference of A11 | Limit | Slowest rule |
| --- | ---: | --- | --- | --- |
| 4 emergencies, `rail-hub` | 24 | 0.34 to 0.42 µs in 6 gates | 1 µs | Misses in 2 of 24 runs |
| 4 emergencies, `london-central` | 4 | About 0.74 µs | 1.44 µs | Meets |
| 64 debris and 300 pod faults, limit 50 µs (A7) | 1 | Median 11.13 µs | 50 µs | Meets |

The median rule of A11 passes on both presets.
The `rail-hub` row fails the gate because of the rule for the slowest tick: 209.9 µs against 66.6 µs, and 241.3 µs against 20.2 µs.
The other 22 runs have a slowest tick from 21 to 59 µs.
Each run has the same seed and the same simulated work, so a slow tick that does not repeat is not a cost of the sim work in that tick.
That leaves the host or the collector, and these readings do not separate them.
The maintainer then added a floor of 1 ms to the rule for the slowest tick of this row, in A11, after these readings, so a later quiet-host run must confirm it.

The gated run at `5979debb`, with the floor, ran 3 times at a load average of 2.4 to 3.
All rows passed in 2 of the 3, and `rail-hub` passed in all 3.
The other run failed on `london-central`, with a slowest tick of 4.2 ms over its limit of 2.06 ms for that run.
That row then ran 40 more times with no gate, at a load average of 4 to 4.7, and 40 more with `GOGC=off`, and no run passed twice its reference.
With the collector, the slowest tick was 1.57 ms and the reference reached 2.37 ms, and without it these are 1.71 ms and 1.78 ms, so the arms do not differ.
So the rate of this miss is 1 in 56 runs of `london-central`, and these runs do not name its cause.
The maintainer ruled on 2026-11-06 that the row passes at `5979debb`, by the rule of `AGENTS.md` that a latency failure runs again by itself before it counts, with this miss on record.

`TestIncidentSoakTimed` ran again at `d7f36582` with `PODSIM_SOAK_GATE=1`, at a load average of 4.5 to 5.6, and all 6 seeds passed.
Each median is at most 6.5 percent above its reference, and each slowest tick is at most 1.34 times its reference.
The slowest ticks on `rail-hub` are 8 to 20 ms in both arms, against 2 to 4 ms in the earlier run at a lower load, so this run does not measure them closely.

## Readings from a busy host

These readings are not results of a gate.
The Performance rows need a quiet machine (A3).

| Reading | Value | Load average |
| --- | --- | ---: |
| Sampled tick, each policy, cold and warm, both networks | Slowest 0.95 ms, limit 50 ms | 18 |
| Sampled tick at the project limits, cold | 260 to 429 ms, reported against 50 ms | 15 |
| Sampled tick at the project limits, warm | 22.2 s free flow, 15.6 s congestion, 32.5 s queue, 13.5 s predictive, reported against 50 ms | 15 |
| Sampled tick at the project limits, warm, after `458f14a` at 30 Hz | 160 ms emergency part, 122 ms fault part, 150 ms plain first step, 57 ms next step, reported against 50 ms | 7 to 10 |
| Timed run, rail-hub, 3 seeds, 0.1 simulated hours | Median 81 to 87 µs against 88 to 98 µs | 16 |
| Timed run, london-central, seed 1, 0.1 simulated hours | Median 154 µs against 145 µs | 16 |
| 64 debris and 300 pod faults | Median 121 µs against 6.6 µs, slowest 1.25 ms against 1.71 ms | 7 |
| 4 emergencies, london-central | Median 21 µs against 14 µs, slowest 2.9 ms against 3.5 ms | 7 |
| 4 emergencies, rail-hub | Median 2.5 µs against 2.3 µs, slowest 27 µs against 48 µs | 7 |
| 64 debris and 1,000 pod faults | Median 983 µs against 20 µs, slowest 5.0 ms against 0.62 ms | 7 |

The sampled-tick fixture has little traffic, so its step is cheap.
The cold and first warm rows at the project limits come from `TestSampledTickNoCandidateLatency` at `3c61bcb`, are reported, and are not gated (P23).
That test reports the sum of its emergency and fault parts: 24.4 s before the route work fixes and 0.6 s after, in one run of each at a load average of 3 to 19.
At `8ffb20b6`, 3 runs at a load average of 2 to 3.3 report a cold sum of 5.9 to 6.0 ms and a warm sum of 86.4 to 87.7 ms, with 33.6 to 33.8 ms for emergency and 52.8 to 53.9 ms for fault, against 50 ms as a report and not a gate.
The row of 300 pod faults misses the rule of 10 percent, against a reference step that does almost no work.
