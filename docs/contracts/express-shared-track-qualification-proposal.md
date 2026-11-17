# Express shared-track qualification proposal

Status: approved by the maintainer on 2026-11-16, rows SX1 to SX13.
The [shared-track amendment proposal](shared-track-amendment-proposal.md) gave the rows, and decision S6-A of the [shared-track](../design/shared-track-preset.md) design chose a new contract for them.
The preset is not qualified while a row in the [Evidence](#evidence) section is open.
Amended 2026-11-16, by maintainer decision: SX3 compares the compiled envelope reach with an independent reference, in amendment ST22 of the [second shared-track amendment proposal](shared-track-amendment-2-proposal.md).
Amended 2026-11-17, by maintainer decision: the [Evidence](#evidence) section records the evidence, and the maintainer accepts that history rollover of SX8 is not reached on the preset.

## Scope

This contract qualifies the opt-in preset `london-full-shared-express` of the shared-track design.
The rows follow E1 to E8 of the [Express London qualification](express-london-qualification-proposal.md#amendments), S1, A15 to A18, and section 5.1 of the [shape clearance](../design/shape-clearance.md) design.
The approved E rows of D, and their copies in the physical, standee, and stage 7 contracts and the station banks document, do not change.
It does not approve runtime adoption, a default change, deployment, or cloud spending.

## SX rows

Each SX row keeps the limits and the requirements of the row that it follows, and the evidence of D does not transfer.
Where the short text of section 5 of the design omits a clause of that row, the row below carries the clause, and marks it "carried from".
The rows apply to the geometry whose network hash the preset test pins after slice 3 of the design.
That geometry is the network of section 3.5 of the design, with the Express parking of decision S4.
Each evidence item below is evidence that the row needs, and the [Evidence](#evidence) section records the evidence that exists.

- SX1, Foundation parity (from E1).
  Measures: that the preset is opt-in, and that it changes no other project.
  Pass: qualification includes the opt-in preset `london-full-shared-express`.
  LondonFull, D, their fleets, and their defaults do not change, and their network hashes stay.
  Carried from E1: idle Express pods keep their current behavior.
  Evidence: a test that pins the network hashes of LondonFull and D.
  The field-off test: on D and on each scenario network, the oracle and the reference construction give the envelopes of today, and `TestBerthEnvelopeMatchesReference` passes.
  The parity test of D: the saved-state and stream bytes of D at each checkpoint equal those of the build before the change.
- SX2, Route and station eligibility, and Physical geometry (from E2).
  Measures: the shared route, its geometry, and the class rules on it.
  Pass: Express runs on the lanes of link 100 and the four roads of section 1 of the design, shared with Compact pods, with no dedicated lane.
  The audit refuses no pair, preparation refuses no lane, and the layout audit of the London generator finds no conflict.
  Qualification covers every hub berth and both directions.
  New for decision S4-A, as E2 does for the parking of D: qualification also covers both directions between the hubs and the Express parking.
  A merge, split, or shape pair certifies the 6 m by 2.5 m class body only (SC6).
  Evidence: a test that pins the network hash of the preset and its audit result after slice 3.
  `projectcheck` reports the preset project valid.
  Route tests from each hub berth in both directions, and to and from each parking berth.
  The sweep cases of SC6 on the preset.
- SX3, Motion and ownership, and Independent safety oracle (from E3).
  Measures: the oracle on the mixed fleet of the preset, and the parity of the two envelope constructions.
  Pass: the oracle checks the mixed fleet at each tick, with fault stops and emergency journeys, the shape table, the envelope reach, and the D6 oracle, and it finds no failure.
  Under decision S1, the indexed construction reads the compiled envelope reach, and the reference construction computes the envelope again from the definition of the [group physics design](../design/group-physics-design.md), with the small-pod rules.
  The reference reads no span, item, or bound of the compiled reach.
  The two constructions give the same locations in the same order.
  Evidence: runs of the preset with the oracle and the D6 oracle at each tick, with faults and emergencies.
  A test that compares the indexed and the reference constructions for each pod at each tick of a run of the preset.
  `TestEnvelopeReachHoldsEnvelope` and the body support tests of section 7 of the design, with the preset among their networks.
- SX4, Physical restore, and Rejection and disabled policies (from E4).
  Measures: restore of the preset at the physical tier.
  Pass: each planned checkpoint restores at the physical tier with no demotion, requeue, drop, or interruption, with the shape table, and the D6 oracle runs in restored continuation.
  Carried from E4: a restored route distance may differ from the saved value by at most `restoreTolerance`, and all other saved pod members are equal.
  Speed resets and rebuilt forward grants are reported apart.
  After a restore with the policies off, the current members finish and no new member forms.
  Evidence: a checkpoint test on the preset, as `TestLondonExpressPhysicalCheckpoints` is for D, with a fault, an emergency, and a pod on the shared lanes among the states.
- SX5, Save bytes and shape (from E5).
  Measures: the byte bounds with the geometry of the preset.
  Pass: the byte proofs run on the new geometry, with mixed-fleet, incident, and emergency members.
  Carried from E5: use the current formats and Express limits.
  The bounds of the save, the full stream, the replacement delta, the HTTP state, the topology, and the gzip expansion hold with the current adapters.
  A bound over a cap needs a new decision.
  Evidence: `TestExpressStateFileWorstCaseSize` and `TestExpressStreamMaximumEncoding` pad a worst-case project to its cap, so they do not take the geometry of the preset.
  The row needs a proof that this worst-case project bounds the preset, or a run of the proofs with the topology of the preset.
  The record gives the measured sizes and their margins against the caps.
- SX6, Stream and public consumers (from E6).
  Measures: the preset through each consumer.
  Pass: the Go remote client, HTTP, WebSocket, WASM, and desktop Chrome, with loads of 12, 13, and 20 riders.
  Carried from E6: topology replacement, sequence recovery, and atomic rejection.
  Evidence: remote load and recovery tests on the preset, as `TestLondonExpressRemoteLoads` and `TestLondonExpressRemoteRecovery` are for D.
  The maintainer does the Chrome check.
- SX7, Resource and consumer cost (from E7).
  Measures: the cost of the preset against LondonFull, D, and the preset without its Express pods.
  Pass: the measurements are recorded, and the maintainer accepts them, as for E7.
  Carried from E7: report CPU, peak resident memory, GC, retained frames, network rate, and browser responsiveness.
  Wire size does not bound decoded memory.
  Evidence: the server measurement of the Express London qualification for each of the four cases, and a maintainer check of the browser in Chrome on the preset.
- SX8, Orders and registry, and Storage and recovery (from E8).
  Measures: the limits of orders and storage, and recovery, on the preset.
  Pass: the current limits, with directed incident and recovery coverage on the preset.
  Carried from E8: 600 pods under `express-v1`, 17,000 pending and outstanding orders, and 20 stored Express riders for each pod.
  Keep whole-party admission, immutable service identity, history rollover, and conservation over repeated recovery.
  Evidence: directed incident fixtures on the preset, as `TestLondonExpressIncidentFixtures` is for D.
- SX9, the standee Fault, Dwell, and Restore and stream rows (from S1).
  Measures: standee motion and dwell on the preset.
  Pass: the rows run on the preset with standees, mixed traffic, and emergencies.
  Carried from S1: the current class motion and dwell bounds apply.
  Adapted from S1, which says that the `rail-hub` evidence does not qualify London: the evidence of `rail-hub` and of D does not qualify the preset.
  Evidence: the runs of these rows of section 7.2 of the standee contract on the preset.
- SX10, the London soak row (from A15).
  Measures: the preset under the soak.
  Pass: the preset is a named project of the London soak row.
  Carried from A15: the branch shape of the London soak row, the A10 checks, and rule A with A13.
  Evidence: a soak run of the row with the preset, with its log.
- SX11, the CI soak and the release soak (from A16).
  Measures: long runs of the preset with the full check.
  Pass: a CI case of one simulated hour, and 5 seeds of 6 simulated hours before a release tag, with the full check and the D6 oracle at each tick.
  Carried from A16: the release runs end with a clear of 30 minutes.
  Evidence: the CI case in the CI tasks, and the logs of the release runs.
- SX12, Performance, Sampled-tick latency, Worst choice tick, and Web (from A17).
  Measures: the performance and latency of the preset.
  Pass: the preset is in the performance and latency measurements that apply.
  Carried from A17: A7, A11, and the current latency limits stay.
  Evidence: the measurements with the preset, recorded beside those of D.
- SX13, Formats (from A18).
  Measures: the byte proof of the formats on the preset.
  Pass: the formats byte proof runs on the preset.
  Carried from A18: its source and its margins are recorded next to the plain proofs.
  Evidence: the proof of SX5, with the same condition on the worst-case project, and with its source and margins.

B1 does not apply, because the preset has no banks.

## Evidence

The evidence moved here from its draft on 2026-11-17, by maintainer decision, with the open rows marked open.
Commits `933a9f12`, `2c20743a`, `256b7fd1`, and `ee9059e0` added the evidence.

| Row | Status |
| --- | --- |
| SX1, SX2, SX3, SX4, SX5, SX13 | Evidence complete on the preset. |
| SX6 | Go client evidence complete. Open: the WASM client and the desktop Chrome check, which the maintainer does. |
| SX8 | Evidence complete. History rollover, which the preset geometry cannot reach, is an exception that the maintainer accepted on 2026-11-17 (see SX8). |
| SX9 | Fault, Dwell, and Restore and stream evidence on the preset. Open: the browser game page, which the maintainer checks. |
| SX7 | Server measurement recorded for the maintainer. Open: retained frames and the browser in Chrome, which the maintainer checks. |
| SX10 | Evidence complete on the preset. |
| SX11 | CI case complete. Release soak failed: seed 1 misses the emergency Progress gate in the queues of link 100 (see SX11), and a whole run does not fit the 2-hour limit. |
| SX12 | Performance timed runs, sampled-tick and choice-tick latency, and the worst-case emergency row recorded for D and the preset. Open: a quiet run of the worst-case row, which ran at a load average of 5.7 to 9.0. |

### Fixture

`TestSharedExpressFixture` in `internal/scenarios` pins `internal/sim/testdata/shared_express_project.json.gz`.
The file holds the network, the fleet, the Express services, the platoon limit, and the fault settings of the preset.
Package `sim` cannot import the preset, so its SX tests read this file.
Run the test with `-update-shared-express` after a change to the preset.

### Rows

Each test below passed on the commit that added it.
The long tests skip with `-short` and with `-race`, as the tests of D do.
The new task `mise run test:shared-express` runs them without the race detector, and CI runs it as a job of its own.
`mise run check` runs it after `test:long`.
The long tests of D in `internal/sim`, `internal/session`, and `internal/remote` are in no CI task, because they also skip with `-race`.

#### SX1, Foundation parity

All evidence items of the row exist, and commit `933a9f12` adds the preset to the field-off test.

- `TestLondonFullSharedExpressKeepsPresets` pins the network SHA-256 of LondonFull (`e194426a...`), of D (`3aa0a2eb...`), and of the preset (`652d75eb...`).
- `TestRunPresetsTurnOnPolicies` in `cmd/scenario` checks that only `london-full-shared-express` sets `shapeClearance`.
- `TestEnvelopeFieldOffMatchesBuilt` runs on D, on each scenario network, and now on the preset network with the field clear.
  On the preset network it compares 9,774 envelopes.
- `TestBerthEnvelopeMatchesReference` passes.
- `TestLondonExpressBehaviorParity` passes, so the saved-state and stream bytes of D at each checkpoint do not change.
  `TestGateOffTables` passes.

#### SX2, Route and station eligibility, and Physical geometry

- `TestLondonFullSharedExpress` pins the audit result: no refusal, 47 shape pairs, and one tail warning for `940GZZLUBNK-move-08-02`.
  `TestLondonFullSharedExpressLayout` and `TestLondonFullSharedExpressParking` pin the layout audit and the parking.
- `projectcheck` reports the preset valid, with 313 stations, 9,089 lanes, 343 pods, and the same tail warning.
- `TestLondonFullSharedExpressRoutes` checks that each hub berth admits Express, and finds an Express route from the node of each of the 5 Waterloo berths to the node of each of the 2 Bank berths, and back.
  Each of these 20 routes takes the two lanes of link 100 of its direction.
  It also finds an Express route from each of the 16 parking berths to each hub berth, and back, 224 routes in total.
- The checkpoint and incident runs below used the berths `940GZZLUWLO-01`, `940GZZLUWLO-03`, `940GZZLUBNK-01`, and `940GZZLUBNK-02` with Express pods.
- `TestLondonFullSharedExpressRoutes` also finds the longest of these Express routes: 30 lanes, from `xp-16-out` to `940GZZLUBNK-02-in`.
  The route caps `MaxSavedRouteLanes` and `MotionRouteLimit` are 1,024 lanes.
- `TestSharedExpressShapePairSweep` runs the shape pair, merge pair, and split pair cases of SC6 on the preset.
  SC6 certifies the class body of 6 m by 2.5 m.
  For each pairing of the classes that the two lanes admit, in both class orders, the test places two pods at each pair of positions whose reference points are less than 12 m apart.
  The positions of a lane are each 0.5 m of a part, the ends of the part, and each polyline vertex in the part, where the oracle turns the body between the headings of a join.
  On a merge or split pair, they also include each bound of the junction extent and the positions 1 mm before and after it.
  At each placement the test requires the literal oracle bodies of `merge_oracle.go` (`oracleDisjoint`) to be disjoint at each pose, with no plane exemption.
  For each of the 47 shape pairs, it takes the certified parts from `(bankAudit).planes`, the function that `shapePairs` calls: the parts at which the envelope reach of the two lanes is not separated.
  It also requires the distance of the reference points to be at least the clearance of the shape table.
  The class pairs are Compact and Compact on 38 shape pairs, Compact and Express on 28, Express and Compact on 22, and Express and Express on 6.
  The shape pairs have 4,576 placements, 652 of them with a join pose.
  The least distance of the reference points is 7.24 m with a Compact pod, and 10.96 m for two Express pods, and the least margin over the clearance of the shape table is 3.69 m.
  For each of the 97 merge and split pairs with a lane that admits Express, the test places the pods where at least one pod is outside its junction extent.
  These pairs have 65,308 placements, 2,804 of them with a join pose and 3,802 with a pod at an extent bound or 1 mm from it.
  The least distance of the reference points is 4.05 m with a Compact pod.
  Some shape pairs cross on another plane, for example `940GZZLUBNK-road-in-08` and `940GZZLUBNK-road-out-08`, so the test does not place pods outside the certified parts.
  The SC6 cases on synthetic lanes (side by side, angles, the union body, tangency, disk bounds, and join turns) are not geometry of the preset, and their tests do not change.

#### SX3, Motion and ownership, and Independent safety oracle

- `TestSharedExpressEnvelopeRun` runs the preset for 4 simulated minutes (7,200 ticks), with the fleet of 343 pods.
  Express hub parties of 12 to 20 riders run in both directions, and a Compact trip between the hubs starts each 10 s.
  At each tick it runs the oracle (`Check`), the D6 oracle (`lateralBodyCheck`), and the parity of the indexed and the reference envelope construction for each pod, with the same locations in the same order.
  It made 2,469,600 envelope comparisons with no difference.
  6,942 ticks had two or more traveling pods on lanes of a merge or a shape pair, with an Express pod among them, and 4,067 ticks had Express and Compact pods on link 100 together.
  A fault stopped `express-01` on `london-link-100-ba-1` at 14.00 m/s with 13 riders, and an emergency started on `express-02` on the same lane at 11.12 m/s with 14 riders.
  1,799 ticks had an active fault and 3,450 ticks had an active emergency.
- The reference construction builds its tables of the network one time for each simulation (`referenceSafetyLocationsWith`), and it keeps the lanes of rule 2 of each plane after it computes them.
  The tables depend only on the network, and the reference still reads no span, item, or bound of the compiled reach.
  With a new build of the tables for each pod, the run takes about 2 s of wall time for each tick, and with the tables about 20 ms.
- `TestEnvelopeMatchesReference` compares 5,298 envelopes on the preset network.
- `TestEnvelopeReachHoldsEnvelope` checks 525,072 positions on 12 sampled routes and 786 berths of the preset.
  The front bound is 33.6 m, the longest tail is 57.5 m, and the reach holds 0.79 planes more than the envelope on average, and 40 at most.
- `TestEnvelopeSupportOneNode` and `TestEnvelopeSupportChain` have the preset among their networks.
- `TestEnvelopeSupportMixedSmall` has a case for the preset, which runs only without `-short` and `-race`.
  It drives small pods on routes through the 4 lanes of link 100, with the reference construction, and checks 2,406 body poses with no failure.

#### SX4, Physical restore, and Rejection and disabled policies

- `TestLondonSharedExpressPhysicalCheckpoints` saves the preset through the encoded session path at 8 states, as `TestLondonExpressPhysicalCheckpoints` does for D.
  The states are an Express pod with riders on link 100 with a Compact pod (tick 3,150), a fault on that pod at speed on link 100 (tick 3,180), the faulted pod at rest (tick 3,570), an Express pod on its way back to the parking from outside the comb (tick 3,480), that pod in a parking maneuver (tick 4,830), an emergency journey on link 100 (tick 5,040), an Express pod at a Bank berth (tick 10,350), and the emergency unload at Bank (tick 12,210).
  Each save restores at the physical tier with no demotion, requeue, drop, interruption, or change to the pods.
  Each restored copy runs for 10 s with the oracle and the D6 oracle at each tick.
  The session tests call the D6 oracle through `LateralBodyCheckForTest`, a test entry in `internal/sim/merge_oracle.go`, because `lateralBodyCheck` is not exported.
  It is the one production change of commit `933a9f12`, and no production code calls it.
- Each restore sets the speed of each pod to zero, and the test reports the speed resets apart: from 30 at the first state to 86 at the emergency unload.
- `TestSharedExpressEnvelopeRun` also restores the simulation at the first tick with Express and Compact pods on link 100, at the fault, at the emergency, and at the first platoon link (tick 7,110).
  Each restore is at the physical tier and matches the live simulation, and the restored copy runs for 10 s with the oracle, the D6 oracle, and the envelope parity at each tick.
  Each resource that a pod holds at its station, or behind its position on its route, keeps its owner.
  The forward grants, the resources of the blocks ahead of the pod, are reported apart:

| Restore | Speed resets | Forward grants kept | Not rebuilt at the restore | New at the restore |
| --- | ---: | ---: | ---: | ---: |
| Mixed traffic on link 100 | 13 | 29 | 39 | 0 |
| Fault on link 100 | 14 | 55 | 16 | 0 |
| Emergency on link 100 | 16 | 66 | 16 | 0 |
| First platoon link | 22 | 38 | 51 | 0 |

- The forward grants that a restore does not rebuild are taken again by admission in the next ticks, under the oracle and the D6 oracle.
- Policies off, platooning: at the restore with a platoon link, a copy turns platooning off, and runs with a Compact trip between the hubs each 10 s for 5 minutes.
  The one link ends, no new link forms, and the 64 orders complete or are interrupted (1 interrupted) in 1,456 s, with the oracle and the D6 oracle at each tick.
  An original link is exempt only until it ends, so a link that forms again later, also between the same two pods, is a failure.
  A control copy with platooning on makes up to 6 new links in the same 5 minutes, so the check can see a new member.
- Policies off, occupied pickups: the preset has occupied pickups off, so `TestSharedExpressOccupiedPickupPolicy` turns them on, with drop-offs sharing, for one Compact pod.
  The pod departs from Chancery Lane with a party for Bank and a party for St Paul's, the stop between them.
  The simulation saves and restores, and a party then orders a trip from St Paul's to Bank.
  With occupied pickups on, the pod takes that party aboard at St Paul's with the party for Bank aboard.
  With occupied pickups off in the restore input, it does not, and it serves the party when it is empty.
  In both copies the 3 accepted orders complete.

#### SX5 and SX13, Save bytes and shape, and Formats

The proofs pad a worst-case project to its caps, so they do not take the geometry of the preset.
`TestLondonSharedExpressByteBounds` shows that the worst-case project bounds the preset, and measures the preset.

| Input of the proofs | Preset | Worst-case project | Margin |
| --- | ---: | ---: | ---: |
| Nodes | 5,790 | 12,000 | 6,210 |
| Lanes | 9,089 | 20,000 | 10,911 |
| Stations | 313 | 600 | 287 |
| Berths, against a berth state for each node | 786 | 12,000 | 11,214 |
| Pods | 343 | 600 | 257 |
| Longest ID, bytes | 30 | 64 | 34 |
| Project JSON, bytes | 5,635,339 | 33,554,432 | 27,919,093 |

The preset is a valid `express-v1` project with 16 Express pods, and it sets `shapeClearance` and `lateralMerges`, as the worst-case project does.

The proofs at commit `933a9f12`:

| Bound | Worst case, bytes | Cap, bytes | Headroom, bytes |
| --- | ---: | ---: | ---: |
| Saved state, `TestExpressStateFileWorstCaseSize` | 105,124,223 | 134,217,728 | 29,093,505 |
| Full stream, `TestExpressStreamMaximumEncoding` | 37,246,155 | 67,108,864 | 29,862,709 |
| Replacement delta | 37,291,693 | 67,108,864 | 29,817,171 |
| HTTP state | 70,802,333 | 134,217,728 | 63,415,395 |

The largest sizes of the preset at the 8 states of the checkpoint script, with mixed-fleet, fault, and emergency members:

| Size | Preset, bytes | Cap, bytes | Margin, bytes |
| --- | ---: | ---: | ---: |
| Saved state JSON | 5,741,195 | 134,217,728 | 128,476,533 |
| Full stream | 284,455 | 67,108,864 | 66,824,409 |
| Delta between two states | 101,238 | 67,108,864 | 67,007,626 |
| HTTP state | 2,583,296 | 134,217,728 | 131,634,432 |
| Topology | 2,299,035 | 33,558,528 | 31,259,493 |

The largest gzip saved state is 1,689,610 bytes.
The largest compression ratio of one saved state is 3.4, and it is not an expansion bound.
`TestStreamGzipExpansionBound` in `internal/session/stream_test.go` is the gzip expansion bound of a stream message, and it does not depend on the geometry.
It compresses 64 MiB (`MaxStreamJSON`, 67,108,864 bytes) of incompressible data to 67,114,009 bytes, within the allowance `streamGzipOverhead` of 32,832 bytes, and under the message cap `MaxStreamMessage` of 67,141,696 bytes, with 27,687 bytes of headroom.
The full stream of the preset is at most 284,455 bytes of JSON, so its compressed message is at most 317,287 bytes, 66,824,409 bytes under the message cap.
The route caps of the proofs, `MaxSavedRouteLanes` and `MotionRouteLimit`, are 1,024 lanes for each project, and the proofs fill them.

#### SX6, Stream and public consumers

- `TestLondonSharedExpressRemoteLoads` runs the Go client against a live session of the preset over HTTP and WebSocket.
  The client reads the topology of the preset, with no banks, `shapeClearance` set, and the 16 parking berths.
  Its stream shows Express pods with parties of 12, 13, and 20 aboard.
- `TestLondonSharedExpressRemoteRecovery` runs the sequence recovery, the topology replacement of a new epoch, and the atomic rejection of the Go client on frames of the preset.
  The recovery replaces the topology of the preset (9,089 lanes) with the topology of D (9,399 lanes, 320 of them not in the preset).
  The test requires the network of the client to be the network of D, each pod to be the pod of the recovery frame, and each lane, berth, and station of a pod to be in the network of D.
- Open: the WASM client and desktop Chrome, which the maintainer checks.

#### SX8, Orders and registry, and Storage and recovery

- `TestLondonSharedExpressIncidentFixtures` runs the directed fixtures of `TestLondonExpressIncidentFixtures` on the preset, with background demand and a Compact trip between the hubs each 10 s.
  20 pooled singletons board one Express pod, with standees past the 12 seats.
  Whole-party admission at the capacity: a party of 21 is refused ("party size must be 1 to 20: invalid trip options"), no pod carries more than 20 passengers, and a 21st singleton waits while the pod with 20 departs.
  `TestSharedExpressDwell` also admits a party of 7 to a pod with 13 passengers, and keeps a party of 8 out.
- `TestLondonSharedExpressRecoveryChain` saves and restores the session 4 times, and each restored session is the source of the next save.
  The saves are with Express and Compact pods on link 100 (34 orders), with a fault on an Express pod with riders (35 orders), with an emergency on an Express pod with riders (71 orders), and at the emergency unload (131 orders).
  Each restore is physical with no demotion, requeue, drop, or unaccounted order.
  The counters, the passengers aboard, and each pending order and active rider, with its service, party, and stations, do not change.
  At each save and each restore, each accepted order is complete, interrupted, pending, or an active rider of one pod.
- History rollover is not reached on the preset, and the maintainer accepted this exception on 2026-11-17.
  A pod retires its oldest rider only at an occupied pickup (`internal/sim/onboard_pickups.go`), and only Waterloo and Bank are passenger stations with Express berths, so an Express pod has no intermediate stop.
  `TestSharedExpressDwell` pins the two stations.
  `TestExpressRepeatedHistoryRetirement` covers the rollover on its fixture.
- An Express pod can board an ordinary on-demand trip right after its emergency unload, so the incident test waits for the riders of the emergency journey to leave, not for an empty pod as the test of D does.
  `podFitsRequest` in `internal/sim/trip_admission.go` admits an on-demand request to a pod of each class that fits the party, and the Express physical contract keeps the on-demand pooling limits for an Express pod.
  D shows the same behavior.
  In a scratch run of 25 minutes with Express parties and a Compact trip each 10 s, 15 of the 16 Express pods of D and 6 of the 16 Express pods of the preset carried on-demand riders.
- The limits of E8 (600 pods, 17,000 orders, 20 stored riders for each pod) are the limits of the byte proofs of SX5, and the preset is inside them.

#### SX9, the standee Fault, Dwell, and Restore and stream rows

- Fault: a fault on the pooled pod at 14.00 m/s on `london-link-100-ba-1`, with 20 riders, gives a largest speed drop of 0.03828 m/s per tick, inside 1.15 m/s² at 30 Hz, and a stop after 85.22 m on the same lane.
  A Compact pod was on link 100 with the faulted pod.
  An emergency on a pod with standees at speed on link 100 runs to its emergency unload at Bank, and the test requires a Compact pod on link 100 with it.
  The test saves and restores at boarding with standees, at speed with 20 parties, at rest after the fault, on the emergency journey, and at the emergency unload, and it checks the oracle at each tick of each incident.
- Dwell: `TestSharedExpressDwell` runs the cases of `express_dwell_test.go` with one Express pod between Waterloo and Bank.
  Parties of 1, 13, and 20 board for 3, 15, and 22 s, and unload at Bank for 2, 14, and 21 s.
  A join of 3 passengers during boarding adds 3 s and does not restart the phase, a join at 21 s stops at the bound of 22 s, and a join ends a wait for track.
  An emergency unload of 13 passengers starts a phase of 14 s and ends.
  A restore accepts a boarding phase of 22 s and an unloading phase of 21 s, and refuses each phase one tick above.
  The preset has no occupied pickup and no multi-stop unload of an Express pod, because only Waterloo and Bank are passenger stations with Express berths, and the test pins that fact.
- Restore and stream: `TestLondonSharedExpressPhysicalCheckpoints` also saves and restores the session at the first sight of each activity of an Express pod: Ready, Departing empty, Boarding, Traveling while moving, braking, and stopped, and Unloading.
  At each of these states it encodes and decodes a full frame, and a delta from the frame of the state before, or from the frame of the session before its first tick for the first state.
  It requires the pod with its activity in the frame that each of the two gives, and it requires full frame and delta coverage of each activity apart.
  An Express pod has no Continuing activity on the preset, because it has no intermediate stop.
- Open: the browser game page, which the maintainer checks.

#### SX7, Resource and consumer cost

The server measurement uses the method of the E7 record of D.
Each run started `serve` from a project file with background demand on (10 orders/min, seed 20260929), with GOMAXPROCS 4.
One Go client (`internal/remote`) read the stream through a proxy that counted the bytes from the server.
The client set speed 60, and the run measured 60 s of wall time from 2 s after that command.
Server CPU is the user and system time of the server process in the 60 s, and GC cycles are the `gctrace` lines in the 60 s.
Peak RSS is `VmHWM` at the end of the run, so it includes the project load.
Each case ran twice, the second time in the reverse order, on 2026-11-16, with no `nice`, at a host load average of 2.7 to 8.3 from other work.
The scratch harness is not in the repository.
The fourth case is the preset with its 16 Express pods removed from the fleet, and `projectcheck` reports it valid with 327 pods.

| Case | Achieved speed | Server CPU | GC cycles | Peak RSS | Stream rate |
| --- | ---: | ---: | ---: | ---: | ---: |
| Ordinary LondonFull | 60.0, 60.0 | 15.2 s, 14.9 s | 14, 14 | 280 MiB, 279 MiB | 125 kB/s |
| D | 60.0, 59.9 | 16.0 s, 18.5 s | 16, 16 | 263 MiB, 264 MiB | 136 kB/s |
| The preset | 60.1, 60.0 | 18.3 s, 17.3 s | 13, 13 | 314 MiB, 311 MiB | 126 kB/s |
| The preset without Express pods | 60.1, 60.0 | 18.3 s, 18.2 s | 13, 13 | 309 MiB, 309 MiB | 127 kB/s |

Each case kept speed 60.
The preset costs about 18 percent more server CPU than ordinary LondonFull, and about the same as D, within the spread of the runs.
Its 16 Express pods add no measurable CPU, GC, or stream rate.
The peak RSS of the preset is about 33 MiB above LondonFull and about 49 MiB above D.
The readings of LondonFull and D do not match the record of D of November 8, 2026: the CPU is about 35 percent lower, the GC count of LondonFull is 14 and not 57, and the peak RSS of LondonFull is about 55 MiB higher.
The code changed after that record, and the harness is a new copy of the method, so compare these rows with each other and not with the record of D.
The runs had no hub parties.
Retained frames, decoded client memory, and browser responsiveness are the maintainer's Chrome check, and they stay open.

#### SX10, the London soak row

- `london-full-shared-express` is a named project of the soak (`PODSIM_SOAK_PRESETS`), with the shape of D: the arms `incidents`, `emergencies`, and `incidents-low-demand` with 3 branches each, and the references `reference` and `reference-low-demand`.
  Each arm has a warm save at 30 simulated minutes, and each branch runs one simulated hour, the clear, and 30 more minutes, with a rewind every 10 simulated minutes.
  The driver sends the hub parties of D between Waterloo and Bank, three in four outbound, and one a minute in the low-demand arms.
- The checks are the A10 checks at the stride of A10, with rule A and A13.
  The D6 oracle ran at each tick of each checked branch, 165,691 to 165,896 times in a branch.
- The row ran on 2026-11-16 by itself, as `mise run soak` runs it, with 3 runs at a time, under `nice`, at a host load average of 4.7 at the start and up to 14 from other work.
  It passed in 786 s of wall time.
- No branch had a check failure, a demoted save, a late record, or a refused hub party.
  Each branch saved one time and restored at the physical tier, and made 6 rewinds.
- The peak load was 20 riders in each branch and each reference.
  Faults on pods with standees were in 2 branches of `incidents`, and emergencies on pods with standees in 6 of the 9 branches.
- The refused incident command was one emergency on a pod that already had one, in branch 3 of `incidents-low-demand`.
  The soak of D had the same refusal.
- The longest record not blocked by a fault was 530.4 s, in branch 1 of `incidents-low-demand`.
  One record, in branch 1 of `incidents`, ended longer than the limit in total time, and rule A did not count it late.
- The reference queue was 31 to 95 at preset demand and 0 to 7 at low demand, under the cap of 200, so the queue check after the clear can fail.
  The longest settle time after the clear was 526 s, in branch 2 of `incidents`.

| Arm | Wall time of a branch | Faults | Emergencies | Accepted hub parties |
| --- | ---: | ---: | ---: | ---: |
| `incidents` | 325 to 360 s | 27 to 35 | 18 to 21 | 230 to 256 |
| `emergencies` | 208 to 218 s | 0 | 15 to 22 | 230 to 256 |
| `incidents-low-demand` | 97 to 100 s | 27 to 36 | 13 to 15 | 51 to 62 |

#### SX11, the CI soak and the release soak

- `TestIncidentSoakCI` has a subtest for the preset, `london-full-shared-express`, beside `rail-hub` and D, so `mise run qualify` runs it in CI.
  It runs a reference hour and a checked hour, each with the clear of 30 minutes.
  Only the checked hour and its clear run the full check and the D6 oracle at each tick.
  The reference hour runs neither.
- On a project that sets `shapeClearance`, each check of the soak driver now also runs the D6 oracle (`LateralBodyCheckForTest`).
  The checks run at each tick, so the D6 oracle runs at each tick.
  The CI case requires at least one run of the D6 oracle for each tick.
- `TestSoakLondonSharedExpressDriver` runs a short soak of the preset with the checks, and requires the same count.
  The task `test:shared-express` runs it.
- The subtest ran once by itself on 2026-11-16, under `nice`, at a host load average of 3.8 to 5.6, and passed in 268 s of wall time.
  The reference run took 32 s for 162,000 ticks.
  The checked run took 228 s, 710 ticks a second, with 162,848 full checks and 162,848 runs of the D6 oracle.
  It had 6 faults, 2 emergencies, 1 save with no demotion, 1 rewind, and no late record.
  The driver accepted 245 hub parties and refused none, the peak load was 20, and one emergency was on a pod with standees.
- The D case took 435 s in its pilot of November 8, 2026, so the qualify job grows by about 270 s at most, because the subtests run in parallel.
- The release soak is `mise run soak:full`, and its default presets now include the preset.
  On the preset, a seed runs `incidents` and `emergencies` with the full check at each tick, and `reference` with no check, each for 6 simulated hours and the clear of 30 minutes.
  This is a per-seed extrapolation from the wall time of the CI case.
  At those rates, a checked arm takes about 990 s and the reference about 140 s, so the arms of one seed take about 35 minutes one after another.
  The task shares 3 run slots across 5 seeds and its other presets, so the wall time of a whole release run is not verified, and it can reach the 2-hour limit of the task with seeds not complete.
- `mise run soak:full` ran on 2026-11-17 on commit `86441b8e`, under `nice`, from 03:00Z, at a host load average of 2.6 at the start and about 13 during the run.
  The 2-hour limit of the task cut it.
  Three seeds finished: seed 1 of D, and seeds 1 and 5 of the preset.
  No seed of `rail-hub` or `london-central` finished, so a whole release run does not fit the limit of the task.
- Seed 1 of the preset failed the Progress gate of the emergency contract in arm `incidents`.
  Record i1.18 on Express pod `express-07` had 611.6 s not blocked by a fault, and the limit is 600 s.
  A run of the arm alone gave the same counts and the same record, so the miss is deterministic.
- A trace of the record shows the cause.
  The record started at 103.6 simulated minutes, on the lane of link 100 toward Bank (about 956 m, limit 14 m/s).
  The pod spent about 508 s on that lane behind other pods ("Pod ahead" and "Junction traffic"), then waited 51 s for a Bank berth ("Berth occupied"), and started to unload at 593 s.
  No fault was on the chain of the pod at any reading, so rule A counts the whole time.
  Two more records of the arm, on Express pods toward Waterloo and toward Bank, ended after 806 s and 840 s in total, with 424 s and 399 s not blocked by a fault, in the same queues on link 100.
- Over 6 simulated hours, the preset is near its queue cap without incidents.
  The queue of the `reference` arm peaked at 166 to 200 across seeds 1 to 5 (the cap is 200), with a mean wait of 384 to 699 s.
  The same arm of D peaked at 84 to 94, with a mean wait of 232 to 280 s.
  The longest record not blocked by a fault was 611.6 s and 525.5 s on the preset (seed 1, `incidents` and `emergencies`), and 208.0 s and 317.2 s on D.
- The release soak of the preset fails until the maintainer decides how to treat the queues of link 100.

#### SX12, Performance, Sampled-tick latency, Worst choice tick, and Web

- The record of D has no measurement of A17.
  So `TestIncidentSoakTimed` ran D and the preset in the same session, with `PODSIM_SOAK_GATE=1`, at 3 seeds of 0.1 simulated hours and the clear.
  The runs had no `nice`, and the host load average was 0.9 to 1.5.

| Preset and seed | Median | Reference median | Slowest | Reference slowest | Gate |
| --- | ---: | ---: | ---: | ---: | --- |
| D, seed 1 | 97.9 µs | 96.5 µs | 8.75 ms | 8.40 ms | Passes |
| D, seed 2 | 102.6 µs | 90.4 µs | 9.87 ms | 6.38 ms | Misses the median rule, 13.5 percent |
| D, seed 3 | 106.8 µs | 99.1 µs | 11.44 ms | 6.39 ms | Passes |
| The preset, seed 1 | 76.6 µs | 74.1 µs | 6.29 ms | 6.25 ms | Passes |
| The preset, seed 2 | 77.8 µs | 75.3 µs | 5.60 ms | 4.41 ms | Passes |
| The preset, seed 3 | 82.4 µs | 80.2 µs | 7.57 ms | 5.41 ms | Passes |

- Each median of the preset is at most 3.4 percent above its reference, and each slowest tick is at most 1.40 times its reference.
- D seed 2 missed the median rule of 10 percent in each of 3 runs, at 10.8 to 13.5 percent, and the other D seeds passed each time.
  Commit `256b7fd1` does not change the path of D.
  The same run at commit `5d8afa00`, before the D11 build, also fails seed 2, by 11.8 and 10.8 percent, so the miss predates the build.
  The reproducer is `PODSIM_SOAK_TIMED=0.1 PODSIM_SOAK_GATE=1 PODSIM_SOAK_PRESETS=london-full-express go test ./internal/session -count=1 -v -timeout=0 -run '^TestIncidentSoakTimed$'`.
- `TestSampledTickLatency` and `TestEmergencyChoiceLatency` now also run on the networks of D and the preset.
  D's network is `internal/sim/testdata/express_network.json.gz`, which `TestExpressNetworkFixture` in `internal/scenarios` pins, and the preset's network is the SX fixture.
  The fixture places up to 600 legacy pods, so the cases measure the network and not the preset fleet.
  On both networks, 8 stations have only Compact bay berths, so the fixture does not use them as passenger stations.
  Compact pods fill adjacent bays 6 m apart, and the traffic check of the fixture refuses a gap below `Clearance`, so the fixture keeps legacy pods.
  The limit stays at 50 ms.
- The runs were alone, with no `nice`, on 2026-11-17.
  `TestSampledTickLatency` started at a load average of 2.9 and took 11 s, and `TestEmergencyChoiceLatency` started at 2.9 and took 14 s.

| Case | Sampled tick, slowest | Choice tick, slowest | Choice tick, median |
| --- | ---: | ---: | ---: |
| D, cold | 1.03 ms | 2.37 ms | 2.31 ms |
| D, warm | 5.04 ms | 2.88 ms | 2.38 ms |
| The preset, cold | 1.06 ms | 2.33 ms | 2.24 ms |
| The preset, warm | 1.53 ms | 2.50 ms | 2.41 ms |

- `TestIncidentWorstCaseStepTime` has the emergency row with `MaxEmergencies` records for D and for the preset, with the rule of A11.
  The suspension and fleet-limit rows do not apply, because they use generated fleets of 300 and 1,000 pods and not a preset.
  The test now stops when the active emergencies are not `MaxEmergencies` after the placement.
  On D and the preset, the driver placed 3 records, and the emergency sampler of the session had started the fourth in the warm time.
- The rows ran with `PODSIM_SOAK_WORST=1` and `PODSIM_SOAK_GATE=1`, with no `nice`.
  The load average rose from 5.7 to 9.0 from other work during the run, and the host had no quiet time before the deadline of the task.

| Preset | Run medians, against the reference | Run slowest steps, against the reference | A11 |
| --- | --- | --- | --- |
| D | 42.6, 39.5, 38.6, 38.9 µs, against 38.6, 38.2, 38.7, 50.0 µs | 2.49, 2.16, 2.63, 2.64 ms, against 2.31, 2.58, 2.18, 2.20 ms | Passes |
| The preset | 39.4, 39.9, 40.2, 39.4 µs, against 49.0, 39.3, 39.2, 37.7 µs | 2.11, 2.12, 2.19, 2.06 ms, against 2.47, 5.21, 2.00, 2.02 ms | Passes |

- In the same run, the london-central row failed the slowest-step rule in one run and the median rule of A11.
  It passed at a load average of 2.7 earlier the same morning, and this change does not touch its path.
- A quiet run of the worst-case rows of D and the preset is open.
- Web: `mise run test:web` passed.

### Wall times

Wall times of the long tests, each run alone, with a host load average of about 3 to 10 from other work:

| Test | Wall time |
| --- | ---: |
| `TestSharedExpressEnvelopeRun` | 199 s |
| `TestSharedExpressShapePairSweep` | 55 s |
| `TestSharedExpressDwell` | 3 s |
| `TestSharedExpressOccupiedPickupPolicy` | 3 s |
| `TestEnvelopeSupportMixedSmall`, the preset case | 39 s |
| `TestLondonSharedExpressPhysicalCheckpoints` | 43 s |
| `TestLondonSharedExpressIncidentFixtures` | 18 s |
| `TestLondonSharedExpressRecoveryChain` | 18 s |
| `TestLondonSharedExpressByteBounds` | 10 s |
| `TestLondonSharedExpressRemoteLoads` | 6 s |
| `TestLondonSharedExpressRemoteRecovery` | 9 s |

The commands of `test:shared-express` took 292 s together, run by hand.
