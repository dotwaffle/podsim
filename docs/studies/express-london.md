# Express in London

This study asks whether a mixed fleet can run on LondonFull, with Compact pods as today and Express pods on hub services.
It also asks whether Express carries more passengers between the hubs, what that costs other riders, and whether depots need an upgrade.

## Result

A mixed fleet does not run on LondonFull under the current rules.
It starts only with the depot refusal lifted, one lane removed, and the plane reach refusal of the bank audit bypassed.
With that bypass, the oracle reports separation failures in each arm with 16 or more Express pods.
All failures are at the Bank end of link 100, on lane pairs that the reach refusal names.
The smallest gap is 17.73 m against the 20 m rule.

The service result is large where the arm ran.
Waterloo to Bank carries 31.4 to 32.1 passengers a minute with 16 Express pods, against 12.3 to 13.2 for Compact pods.
That is about 2.5 times the passengers and about 3 times the whole parties, with fewer station entries.
Other riders do better in each Express arm, but the queue limit and the small fleet both change, and these runs do not separate the two.
Depots need no change, because Express pods never use them.

The recommendation is against landing this shape.
The next work is the junction geometry at a hub, not the depots.

This is a screen of 5 arms, with 3 seeds on 2 of them.
It does not meet the [adoption gates](../design/experimental-adoption.md#evidence-required), which need seeds 1 to 10 and all demand bands.
Express has no [byte proof](express-qualification.md#express-has-no-byte-proof), and this study adds none.
No default, preset, contract, simulation rule, or layout changes with this study.

## Method

The source is `be7cff2e` plus scratch commits, which do not land.
The run scripts were in `tools/study-express-london` in those commits.
Each arm is LondonFull with AM peak background demand at 10 orders a minute, the preset rate, for 60 simulated minutes.
Virtual platoons are on, and redistribution and pickup reassignment are off, as in the preset.
The queue limit is 200.
Rates cover minutes 20 to 60.

### Hub pairs

The Tuesday-to-Thursday matrix of the demand data ranks the pairs as follows, for both directions over all bands.
The AM peak rate is the band total divided by its 180 minutes.

| Pair | Journeys in the matrix | AM peak passengers a minute, each way |
| --- | ---: | --- |
| Waterloo and Bank | 23,931 | 54.8 and 0.6 |
| Paddington and Liverpool Street | 12,346 | 15.6 and 4.4 |
| Waterloo and Canary Wharf | 11,766 | 23.4 and 2.2 |
| Tottenham Court Road and Liverpool Street | 11,583 | 1.8 and 7.2 |
| Victoria and Oxford Circus | 11,565 | 12.3 and 0.9 |
| London Bridge and Canary Wharf | 11,121 | 19.6 and 3.1 |
| King's Cross and Waterloo, rank 17 | 6,688 | 6.2 and 2.2 |

The first service pair is Waterloo and Bank, the largest in the data.
The second is London Bridge and Canary Wharf, which has the largest AM peak direction of the pairs that share no station with the first.
King's Cross and Waterloo has no arm.

### The same people in each arm

All arms offer the same parties on both pairs at the same ticks, for each seed.
Parties arrive at random at the AM peak passenger rate of each direction in the demand data.
The background schedule of a seed is the same in each arm.

| Item | Choice | Reason |
| --- | --- | --- |
| Party size | 1 to 20 passengers, uniform | The range of the [Express soak](incident-soak.md#first-soak-of-express). |
| On an Express service | One order with shared consent, and a pooling limit of 20 parties | The service setting of the soak. |
| On Compact pods | One private order for each 4 passengers or fewer, at the same tick | A Compact pod takes at most 4. |

A service order rides only an Express pod, so that party has no fallback to a Compact pod.
A Compact party can lose some of its orders and keep others at the queue limit.

Two limits apply to this comparison.
The hub rate is the real rate, and the background is about 1/450 of the 4,540 journeys a minute of the data, so the hubs are a flood in the style of the [station limits study](station-limits.md).
The queue limit counts orders, and a split party has about 3 orders for each order of an Express party, so the skipped counts differ for that reason too.

### Arms

Each arm removes the lane `940GZZLUBNK-move-08-02`, also in the baseline.
Each Express arm has the `express-v1` marker, and Express only on the lanes of the shortest route of each direction and on the lanes of its hubs.
The route from Waterloo to Bank is 2,336 m, and the reverse is 2,352 m.

| Arm | Express pods | Other change |
| --- | ---: | --- |
| Baseline | 0 | No marker and no service. |
| Control | 0 | The layout of the `more` arm and the marker. All hub parties split. |
| `few` | 4 | 4 Waterloo and 1 Bank berths shared with Compact pods. |
| `more` | 16 | 15 Express-only berths at Waterloo, 20 in all, and 2 shared at Bank. |
| `two` | 22 | `more`, plus 6 pods on the second pair, with 3 shared berths at each of its hubs. |

The Waterloo layout of 20 berths also moves 72 nodes at Waterloo, Holborn, Lambeth North, and Temple, and it changes 27 existing lanes at Lambeth North.
Its soft conflicts go from 4 to 6.
The control row has this change too, and the baseline does not.

No arm has a dedicated bank at a hub, because the [station limits study](station-limits.md) has no bank arm at Waterloo, so that branch does not make one cheap.

`SafetyObservation.Check` ran at each tick of each run.
For a pair with a large pod, it compares the planes that each pod occupies at that tick, so it covers a pod near a plane join.
Service comparisons follow the [matched rules](../design/experimental-adoption.md#evidence-required).
The `few` run is the timed pilot, and its binary predates the reading of the smallest Express gap.

## Refusals

| Order | Check | Text | Resolution |
| --- | --- | --- | ---  |
| 1 | `validateContractNetworkRecords` | `the Express order contract does not allow depots` | Removed in the scratch commits. |
| 2 | `prepareNetwork` | `lane "940GZZLUBNK-move-08-02" has an unsupported large-vehicle path shape` | The lane removed in all arms. |
| 3 | `validateBankGeometry`, `PlaneReachError` | `nonincident paths ... are less than 20 meters apart` | Bypassed by `STUDY_NO_REACH`. Not resolved. |

Refusal 2 is the only folded lane of the 9,024 lanes of the project.
It is not on the route, and it refuses because `indexGeometryTails` also covers small lanes that touch a node of a large lane.

Refusal 3 blocks each pair that I tried.
Each listed pair is on two planes, so the plane test of the pair passes and only the reach denies the exemption.
Some of the pairs cross, with a gap of 0 m on different planes.

| Pair | Pairs refused by reach | Pairs refused by plane |
| --- | ---: | ---: |
| London Bridge and Canary Wharf | 18 | 0 |
| Waterloo and Canary Wharf | 85 | 0 |
| Waterloo and Bank | 108 | 0 |
| King's Cross and Waterloo | 160 | 0 |
| Paddington and Liverpool Street | 295 | 0 |

These checks did not refuse the arms:

- Lane classes on the route, the lane length of 40 m, and the cell length of 20 m.
  The shortest route lane is 48.0 m.
- Compact bays, because no chosen hub is a Compact station and no route lane is in a maneuver group.
- The 600-pod bound, with 349 pods at most, and the node, lane, junction pair, and block budgets of the contract.
- The depot rule that a slot refuses a large class, because no route lane or berth is in a depot.

Berths limit the fleet.
Each Express pod starts at a free berth that admits it, and all 3 parking stations are depots, so the project has no other parking.
Bank has 2 berths, and 3 berths at Bank is a hard layout conflict.
Waterloo builds with 20 berths, and refuses 10 and 30 berths with hard layout conflicts.

## Depots

Depots need no upgrade for this fleet.
The Compact pods use them as today, and the Express pods cannot enter them.
In these runs, no oracle failure names a depot lane.
A larger Express fleet needs berths or a new non-depot parking station, not a change to a depot.
The commit that added the refusal gives no reason, and this screen does not show that the blanket refusal protects anything on LondonFull.
It covers only redistribution off, so it does not show that the refusal can go.

## Oracle

| Arm | Seed | Ticks with a failure, of 108,000 | Smallest Express gap (m) | Lane pairs |
| --- | ---: | ---: | ---: | --- |
| Baseline | 1, 2, 3 | 0, 0, 0 | No Express pod | None |
| Control | 1 | 0 | No Express pod | None |
| `few` | 1 | 0 | Not recorded | None |
| `more` | 1 | 14 | 17.76 | `road-in-08` with `road-out-08` or `london-link-100-ab-1` |
| `more` | 2 | 10 | 18.16 | The same |
| `more` | 3 | 15 | 17.73 | The same |
| `two` | 1 | 5 | 18.00 | The same |

All lanes named `road-*` are those of Bank.
The failures include Express with Express and Express with Compact, and each pair is in the list of refusal 3.
A gap of 17.73 m is above the 12 m of two envelope radii of 6 m, so this is a break of the 20 m rule and not a contact of bodies.
Waterloo and the second pair have no failure in these runs.
A clean run elsewhere is not a proof, because [random motion](express-standees.md#random-curved-networks) does not detect every short bound.

## Service

The cells of `more` and the baseline give seeds 1 / 2 / 3, and the other arms have seed 1.
Passengers a minute count the passengers of orders that complete in the window.
A whole party has each of its orders complete, so one party has the same meaning in each arm.

| Measure | Baseline | Control | `few` | `more` | `two` |
| --- | ---: | ---: | ---: | ---: | ---: |
| Waterloo to Bank, passengers a minute | 13.2 / 12.5 / 12.3 | 12.3 | 9.9 | 32.1 / 31.9 / 31.4 | 32.2 |
| Waterloo to Bank, whole parties a minute | 0.88 / 1.20 / 1.15 | 0.95 | 1.15 | 3.12 / 3.38 / 3.33 | 3.12 |
| London Bridge to Canary Wharf, passengers a minute | 7.4 / 7.7 / 7.8 | 6.6 | 15.8 | 14.5 / 14.5 / 16.1 | 7.3 |
| All 4 directions, passengers a minute | 20.9 / 21.5 / 20.5 | 19.5 | 28.2 | 49.7 / 51.4 / 50.3 | 41.1 |
| Waterloo entries a minute, Compact | 5.67 / 5.83 / 5.45 | 4.70 | 0.33 | 0.47 / 0.35 / 0.47 | 0.47 |
| Waterloo entries a minute, Express | 0 | 0 | 0.47 | 1.60 / 1.65 / 1.60 | 1.62 |
| Bank entries a minute, Compact | 3.85 / 3.83 / 3.92 | 3.70 | 0.53 | 0.47 / 0.35 / 0.50 | 0.45 |
| Bank entries a minute, Express | 0 | 0 | 0.50 | 1.60 / 1.65 / 1.57 | 1.62 |
| Passengers for each Express entry at Bank | None | None | 19.9 | 19.5 / 19.4 / 19.6 | 19.6 |

Each Express pod at Bank is almost full, with 19.4 to 19.9 passengers of 20.
A queue of parties waits at Waterloo in these arms: `more` accepts 3,141 to 3,392 passengers and boards 1,748 to 1,854.
A Compact pod at Bank in the baseline has 3.1 to 3.4 passengers for each entry, counting all entries.
An Express pod makes 0.10 to 0.125 Bank entries a minute in the `few` and `more` arms, and neither arm reaches the offered 54.8 passengers a minute.
I did not run a fleet that shows a station limit for Express.
The `few` arm carries fewer passengers between the hubs than the baseline, so 4 pods are too few.
On the second pair, the 6 pods of the `two` arm carry 7.3 passengers a minute, against 14.5 to 16.1 when those parties ride Compact pods in the `more` arm.

Express pods do not link, so their entries have no short gap of a platoon.
Linked Compact entries at Waterloo are 107 / 126 / 120 of 227 / 233 / 218 in the baseline, 148 of 188 in the control, and 0 in each Express arm, which has 13 to 19 Compact entries there.
Platoon time falls from 31.7 to 33.4% in the baseline to 6.5 to 7.9% in `few` and `more`, and the control keeps 35.7%.
So the fall follows the Compact pods that leave the hub, and not the lane classes.

### Waterloo to Bank passengers over the run

| Measure | Baseline | Control | `few` | `more` | `two` |
| --- | ---: | ---: | ---: | ---: | ---: |
| Offered | 3,206 / 3,516 / 3,145 | 3,206 | 3,206 | 3,206 / 3,516 / 3,145 | 3,206 |
| Skipped | 1,854 / 2,062 / 1,792 | 1,941 | 552 | 22 / 124 / 4 | 182 |
| Accepted | 1,352 / 1,454 / 1,353 | 1,265 | 2,654 | 3,184 / 3,392 / 3,141 | 3,024 |
| Boarded | 1,074 / 1,089 / 1,083 | 946 | 575 | 1,748 / 1,847 / 1,854 | 1,750 |
| Completed | 686 / 692 / 692 | 650 | 535 | 1,574 / 1,694 / 1,655 | 1,575 |
| Parties with an order skipped, of 305 / 339 / 310 | 202 / 230 / 205 | 208 | 53 | 5 / 12 / 1 | 18 |
| Mean wait of whole parties boarded (s) | 886 / 924 / 926 | 907 | 996 | 677 / 599 / 662 | 690 |
| Mean journey of whole parties completed (s) | 1,259 / 1,488 / 1,604 | 1,390 | 1,194 | 950 / 870 / 920 | 965 |

The means in this table are not matched, and an arm that accepts more parties also waits for more of them.
The wait of a party ends when its last order boards, and its journey ends when its last order completes.
Against the baseline, on parties that board or finish in both arms, `more` has these means.

| Seed | Matched for wait | Baseline wait (s) | `more` wait (s) | Matched for journey | Baseline journey (s) | `more` journey (s) |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 121 | 730 | 327 | 95 | 1,111 | 705 |
| 2 | 133 | 793 | 320 | 109 | 1,293 | 697 |
| 3 | 114 | 816 | 302 | 88 | 1,409 | 664 |

## Other riders

Background riders are the 599 background orders of each seed.
They include 4 orders between Waterloo and Bank at seed 1, which either class can carry.

| Measure | Baseline | Control | `few` | `more` | `two` |
| --- | ---: | ---: | ---: | ---: | ---: |
| Skipped | 176 / 189 / 182 | 190 | 89 | 8 / 17 / 4 | 24 |
| Accepted | 423 / 410 / 417 | 409 | 510 | 591 / 582 / 595 | 575 |
| Boarded | 317 / 314 / 312 | 309 | 490 | 526 / 521 / 536 | 539 |
| Completed | 252 / 251 / 238 | 244 | 414 | 429 / 418 / 418 | 453 |
| Stopped, 1000 pod-seconds | 235 / 236 / 234 | 261 | 68 | 87 / 83 / 67 | 2 |
| Platoon time, % | 33.4 / 32.9 / 31.7 | 35.7 | 6.5 | 7.7 / 7.9 / 7.3 | 0.1 |

| Against the baseline | Control | `few` | `more`, seed 1 / 2 / 3 | `two` |
| --- | ---: | ---: | ---: | ---: |
| Matched for wait | 245 | 302 | 315 / 312 / 311 | 317 |
| Mean wait, baseline to arm (s) | 475 to 493 | 522 to 196 | 528 to 215 / 551 to 213 / 606 to 197 | 530 to 99 |
| Worse by the wait limit | 69 | 22 | 29 / 16 / 16 | 6 |
| Better by the same limit | 60 | 202 | 208 / 222 / 237 | 248 |
| Matched for journey | 197 | 248 | 252 / 249 / 237 | 252 |
| Mean journey, baseline to arm (s) | 1,116 to 1,131 | 1,149 to 851 | 1,145 to 861 / 1,142 to 842 / 1,180 to 817 | 1,145 to 739 |
| Worse by the journey limit | 47 | 17 | 24 / 12 / 10 | 4 |
| Better by the same limit | 31 | 156 | 150 / 157 / 171 | 189 |
| Completed only in the baseline | 55 | 4 | 0 / 2 / 1 | 0 |
| Completed only in the arm | 47 | 166 | 177 / 169 / 181 | 201 |

The limits are the individual limits of the adoption gates: 30 s or 5% for a wait, and 60 s or 5% for a journey.
Each Express arm has riders over them, so each fails those gates.
Background riders with an end at Waterloo or Bank also gain in `more`, with a mean matched wait of 580 to 239, 828 to 238, and 764 to 190 s.

The gain has two possible sources, and these runs do not separate them.
The hub parties no longer use the Compact fleet, and they hold fewer places in the queue of 200.
The improvement is on a flood baseline, so it does not show a gain at demand below supply.

The control arm has no Express pod, and it reads as a cost of the layout, the classes, and the marker together, with one seed.
Its Waterloo entries are 4.70 a minute against 5.45 to 5.83, and its stopped pod-seconds are 261,000 against 234,000 to 236,000.
Its hub passengers and matched background means are near the baseline.

## Empty running

Each distance is the speed of an Express pod summed each second over the 60 minutes, so it is approximate.

| Arm | Loaded, Waterloo to Bank (km) | Empty, Bank to Waterloo (km) | Loaded, Bank to Waterloo (km) | Inside the stations, loaded and empty (km) |
| --- | ---: | ---: | ---: | --- |
| `few` | 66 | 66 | 0 | 18 and 17 |
| `more`, seed 1 | 222 | 206 | 5 | 108 and 88 |
| `more`, seed 2 | 230 | 217 | 0 | 108 and 95 |
| `more`, seed 3 | 223 | 206 | 2 | 107 and 91 |
| `two` | 221 | 205 | 5 | 127 and 104 |

The AM peak is tidal, with 54.8 against 0.6 passengers a minute, so nearly all the return running on this pair is empty.
On the second pair in `two`, London Bridge to Canary Wharf is 122 km loaded, and the return is 22 km loaded and 98 km empty.
Over the whole fleet, the empty distance is 5,818 / 5,793 / 6,072 km in the baseline and 3,704 / 3,560 / 3,670 km in `more`.

## Not verified

- A fleet that reaches a station limit, because berths cap the fleet.
- Any other band, and the opposite tide of the PM peak.
- Parties of one that pool, which the contract allows and the size range here does not test.
- A high queue limit, which would separate the queue effect.
- King's Cross and Waterloo, or any hub pair with a clean reach audit.
- Seeds 2 and 3 of the control, `few`, and `two` arms.
- Pods blocked by an idle Express pod at a shared berth.
  The idle time is at most 2,963 pod-seconds, and I did not read the blockers.
- Save, restore, stream, browser, faults, emergencies, redistribution, reassignment, and sharing of the background orders.

## What a landed version would need

- A contract change with maintainer approval: remove or narrow the depot refusal of `express-v1`, with its test in `internal/sim/depot_network_test.go`, and name the mixed fleet.
- A geometry answer for each hub on a service: 20 m between the reach pairs of refusal 3, or a narrower rule that the maintainer approves, proven against this oracle.
- A fix for the folded lane at Bank, if Bank is a service hub.
- Berths or a non-depot parking station for the Express fleet.
- A contract or soak row that names a London Express project, because the [first soak](incident-soak.md#first-soak-of-express) has no London row.
- A byte proof, or a decision that Express goes without one on this network.
- The adoption gates: seeds 1 to 10, all bands, six-hour runs, and the individual limits.
