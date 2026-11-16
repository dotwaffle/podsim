# Draft: evidence of the Express shared-track qualification

Status: draft of slice 6a, for maintainer review.
The maintainer moves this text into the "Evidence" section of the [Express shared-track qualification](../contracts/express-shared-track-qualification-proposal.md).
The evidence was built on commit base `38b1d03e`, with the changes of slice 6a not yet committed.
This slice covers SX1, SX2, SX3, SX4, SX5, SX6 except the browser checks, SX8, SX9 except the browser check, and SX13.

| Row | Status |
| --- | --- |
| SX1, SX2, SX3, SX4, SX5, SX13 | Evidence complete on the preset. |
| SX6 | Go client evidence complete. Open: the WASM client and the desktop Chrome check, which the maintainer does. |
| SX8 | Evidence complete, except history rollover, which the preset geometry cannot reach (see SX8). |
| SX9 | Fault, Dwell, and Restore and stream evidence on the preset. Open: the browser game page, which the maintainer checks. |
| SX7, SX10, SX11, SX12 | Open. SX7 includes a browser check by the maintainer. |

## Fixture

`TestSharedExpressFixture` in `internal/scenarios` pins `internal/sim/testdata/shared_express_project.json.gz`.
The file holds the network, the fleet, the Express services, the platoon limit, and the fault settings of the preset.
Package `sim` cannot import the preset, so its SX tests read this file.
Run the test with `-update-shared-express` after a change to the preset.

## Rows

Each test below passed on the commit base with the changes of this slice.
The long tests skip with `-short` and with `-race`, as the tests of D do.
The new task `mise run test:shared-express` runs them without the race detector, and CI runs it as a job of its own.
`mise run check` runs it after `test:long`.
The long tests of D in `internal/sim`, `internal/session`, and `internal/remote` are in no CI task, because they also skip with `-race`.

### SX1, Foundation parity

All evidence items of the row exist, and this slice adds the preset to the field-off test.

- `TestLondonFullSharedExpressKeepsPresets` pins the network SHA-256 of LondonFull (`e194426a...`), of D (`3aa0a2eb...`), and of the preset (`652d75eb...`).
- `TestRunPresetsTurnOnPolicies` in `cmd/scenario` checks that only `london-full-shared-express` sets `shapeClearance`.
- `TestEnvelopeFieldOffMatchesBuilt` runs on D, on each scenario network, and now on the preset network with the field clear.
  On the preset network it compares 9,774 envelopes.
- `TestBerthEnvelopeMatchesReference` passes.
- `TestLondonExpressBehaviorParity` passes, so the saved-state and stream bytes of D at each checkpoint do not change.
  `TestGateOffTables` passes.

### SX2, Route and station eligibility, and Physical geometry

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

### SX3, Motion and ownership, and Independent safety oracle

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

### SX4, Physical restore, and Rejection and disabled policies

- `TestLondonSharedExpressPhysicalCheckpoints` saves the preset through the encoded session path at 8 states, as `TestLondonExpressPhysicalCheckpoints` does for D.
  The states are an Express pod with riders on link 100 with a Compact pod (tick 3,150), a fault on that pod at speed on link 100 (tick 3,180), the faulted pod at rest (tick 3,570), an Express pod on its way back to the parking from outside the comb (tick 3,480), that pod in a parking maneuver (tick 4,830), an emergency journey on link 100 (tick 5,040), an Express pod at a Bank berth (tick 10,350), and the emergency unload at Bank (tick 12,210).
  Each save restores at the physical tier with no demotion, requeue, drop, interruption, or change to the pods.
  Each restored copy runs for 10 s with the oracle and the D6 oracle at each tick.
  The session tests call the D6 oracle through `LateralBodyCheckForTest`, a test entry in `internal/sim/merge_oracle.go`, because `lateralBodyCheck` is not exported.
  It is the one production change of this slice, and no production code calls it.
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

### SX5 and SX13, Save bytes and shape, and Formats

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

The proofs at the commit base:

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

### SX6, Stream and public consumers

- `TestLondonSharedExpressRemoteLoads` runs the Go client against a live session of the preset over HTTP and WebSocket.
  The client reads the topology of the preset, with no banks, `shapeClearance` set, and the 16 parking berths.
  Its stream shows Express pods with parties of 12, 13, and 20 aboard.
- `TestLondonSharedExpressRemoteRecovery` runs the sequence recovery, the topology replacement of a new epoch, and the atomic rejection of the Go client on frames of the preset.
  The recovery replaces the topology of the preset (9,089 lanes) with the topology of D (9,399 lanes, 320 of them not in the preset).
  The test requires the network of the client to be the network of D, each pod to be the pod of the recovery frame, and each lane, berth, and station of a pod to be in the network of D.
- Open: the WASM client and desktop Chrome, which the maintainer checks.

### SX8, Orders and registry, and Storage and recovery

- `TestLondonSharedExpressIncidentFixtures` runs the directed fixtures of `TestLondonExpressIncidentFixtures` on the preset, with background demand and a Compact trip between the hubs each 10 s.
  20 pooled singletons board one Express pod, with standees past the 12 seats.
  Whole-party admission at the capacity: a party of 21 is refused ("party size must be 1 to 20: invalid trip options"), no pod carries more than 20 passengers, and a 21st singleton waits while the pod with 20 departs.
  `TestSharedExpressDwell` also admits a party of 7 to a pod with 13 passengers, and keeps a party of 8 out.
- `TestLondonSharedExpressRecoveryChain` saves and restores the session 4 times, and each restored session is the source of the next save.
  The saves are with Express and Compact pods on link 100 (34 orders), with a fault on an Express pod with riders (35 orders), with an emergency on an Express pod with riders (71 orders), and at the emergency unload (131 orders).
  Each restore is physical with no demotion, requeue, drop, or unaccounted order.
  The counters, the passengers aboard, and each pending order and active rider, with its service, party, and stations, do not change.
  At each save and each restore, each accepted order is complete, interrupted, pending, or an active rider of one pod.
- History rollover is not reached on the preset.
  A pod retires its oldest rider only at an occupied pickup (`internal/sim/onboard_pickups.go`), and only Waterloo and Bank are passenger stations with Express berths, so an Express pod has no intermediate stop.
  `TestSharedExpressDwell` pins the two stations.
  `TestExpressRepeatedHistoryRetirement` covers the rollover on its fixture.
- An Express pod can board an ordinary on-demand trip right after its emergency unload, so the incident test waits for the riders of the emergency journey to leave, not for an empty pod as the test of D does.
  `podFitsRequest` in `internal/sim/trip_admission.go` admits an on-demand request to a pod of each class that fits the party, and the Express physical contract keeps the on-demand pooling limits for an Express pod.
  D shows the same behavior.
  In a scratch run of 25 minutes with Express parties and a Compact trip each 10 s, 15 of the 16 Express pods of D and 6 of the 16 Express pods of the preset carried on-demand riders.
- The limits of E8 (600 pods, 17,000 orders, 20 stored riders for each pod) are the limits of the byte proofs of SX5, and the preset is inside them.

### SX9, the standee Fault, Dwell, and Restore and stream rows

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

## Wall times

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

The commands of `test:shared-express` took 292 s together, run by hand in the worktree.
