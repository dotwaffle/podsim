# Express London qualification proposal

Status: proposed on November 7, 2026.
On November 7, 2026, the maintainer approved rows E1 to E6, E8, S1, A15 to A18, and B1.
Row E7 is held until the cause of the extra server CPU of D is known.
D is not qualified until row E7 is approved and the open checks of rows E6 and E7 pass.

## Scope

This proposal qualifies project D of the [Express hub fixture](../studies/express-hub-fixture.md) as an opt-in LondonFull preset.
Project D has banks at Waterloo and Bank, dedicated Express lanes, separate Express-only parking that is not a depot, and 16 Express pods.
Qualification covers the rows of the approved contracts on this project.
It does not change ordinary LondonFull, its fleet, or its defaults.
It does not approve runtime adoption, a default change, deployment, or cloud spending.

## Decisions on record

On November 7, 2026, the maintainer decided these points:

1. Qualify D now.
   The adoption comparison comes back later as a separate proposal, inside the study brief cap of 6 arms and 3 seeds.
   The Compact split-event mapping of the adoption comparison waits with it.
2. Add a separate opt-in preset, `london-full-express`, that freezes the D9 geometry.
3. Index the construction of the safety envelopes, with exact parity against the old construction.
4. Prove the Express byte bounds before D counts as qualified.
   A proof that exceeds a cap stops for a new decision, and no cap changes without one.
5. The Rail row of stage 7 does not apply to D, because D has no rail records.
   The rail evidence stays the `rail-hub` row of amendment A14.
6. Run the cheap risk checks first, then size the soak from a timed pilot.

## Evidence before the amendments

These results come from the risk checks of November 7, 2026, on project D (`express-D-attempt-9.json`).

- Commit `bf0dd48b` indexes the berth and lane scans of the safety envelopes.
  On D the scans took 98 percent of the oracle time.
  A scratch run compared the indexed and the reference locations for every pod at every tick for 15 simulated minutes, with all 16 Express pods away from parking: 9,261,000 comparisons were identical.
  The oracle took 26 s, and the reference construction took 416 s.
- A scratch probe saved D through the encoded session path at 70 checkpoints over 25 simulated minutes.
  Each checkpoint was the first sight of an Express state, or a periodic save.
  All 70 restored at the physical tier with no demoted pod, requeued or dropped request, or interrupted order.
  The states included a fault on a moving Express pod with 14 riders, a fault at a Waterloo berth, debris on an Express lane, and an emergency unload at Bank.
  Fourteen restored copies ran for 10 s with the oracle at each tick and had no failure.
- The restored route distance can differ from the saved value by less than `restoreTolerance` (1e-6 m).
  `restoredDistance` moves a pod to a block end of the rebuilt route on purpose, so that it releases the same resources as the live pod.
  The probe did not reach the second bank at Bank or an Express return to parking.
- A pod chooses the bank with the lower free-flow approach cost, and berth load only breaks a tie.
  From Waterloo the first bank at Bank always costs less, so in 30 simulated minutes of hub parties no Express pod used the second bank.
  `TestLondonExpressPhysicalCheckpoints` puts debris on the Express approach to the first bank, which sends Express pods to the second bank.
  It saves and restores D at the first Express pod at a second-bank berth, the first Express pod on its way to parking, and the first Express pod in a parking maneuver.
  Each save restores at the physical tier with equal pods, and each restored copy passes the oracle for 10 s.
- `TestLondonExpressIncidentFixtures` supplies the directed fixtures of the London soak row on D, with background demand.
  When more Express pods are free, dispatch gives each singleton its own pod, so faults hold 15 Express pods in parking while 20 singletons pool in the last one.
  A fault on that pod at 10.1 m/s with 20 riders gives a largest speed drop of 0.03826 m/s per tick, inside 1.15 m/s² at 30 Hz, and a stop after 44.3 m.
  An emergency on a pod with standees runs to its emergency unload.
  The test saves and restores at boarding with standees, at speed with 20 parties, at rest after the fault, on the emergency journey, and at the emergency unload, and checks the oracle at each tick of each incident.
- The soak row ran on November 7, 2026, at commit `bf965c54`, with the platoon arms, and passed in 1,471 s of wall time.
  No branch had a demoted pod, a late record, or a refused hub party.
  Each branch logged a peak load of 20 riders, and the incident branches had faults and emergencies on pods with standees.
  The refused incident commands were invalid debris segments, debris on a reserved resource, an unsupported fault target, and an emergency on a pod that already had one.
  The longest record was emergency `i6.20` on `express-14` in branch 3 of `incidents`, with 532 s that rule A gives to a fault.
  A trace shows that `express-08` stopped on the last lane of the Express trunk to the first bank at Bank, after the branch to the second bank, with `express-14` behind it.
  The wait ends at the tick at which the fault on `express-10` ends, which agrees with rule A.
  The trace did not record the position of `express-10`.
- A first server measurement for row E7 ran on November 7, 2026, at commit `4d5fb5c9`.
  Each run started `serve` with background demand on (10 orders/min, seed 20260929), GOMAXPROCS 4, and one Go client through a byte-counting proxy, and measured 60 s of wall time at speed 60.
  It ran each case twice, with the order reversed.
  A Plex transcoder used about one core on the host.

  | Case | Achieved speed | Server CPU | GC cycles | Peak RSS | Stream rate |
  | --- | ---: | ---: | ---: | ---: | ---: |
  | Ordinary LondonFull | 59.9, 59.9 | 25.3 s, 24.5 s | 57, 57 | 225 MiB, 226 MiB | 128 kB/s |
  | D without Express pods | 59.9, 59.9 | 38.9 s, 39.0 s | 19, 20 | 225 MiB, 225 MiB | 130 kB/s |
  | D | 59.9, 59.9 | 40.2 s, 40.5 s | 20, 20 | 223 MiB, 224 MiB | 129 kB/s |
  | D with hub parties | 45.2, 59.9 | 36.6 s, 47.7 s | 35, 43 | 234 MiB, 232 MiB | 138 kB/s, 162 kB/s |

  The hub parties were 240 orders of 12 to 20 riders, one each 15 simulated seconds, and none was refused.
  The D geometry without Express pods costs about 55 percent more server CPU than ordinary LondonFull.
  A CPU profile of 20 simulated minutes without the server gives 4.8 s for LondonFull and 9.0 s for D without Express pods.
  When a network has a station bank, each route search goes through `(Network).bankRoute`, which runs its graph searches in `(Network).bankPathNodes` with no cache.
  A network with no bank uses the indexed and cached targets of `(Network).routeTargets`.
  On D, `bankPathNodes` takes 54 percent of the profile, so the extra cost comes from the banks at Waterloo and Bank, not from Express.
  In one of the two runs with hub parties, the server did not keep speed 60.
- Commit `86684aec` gives a network with banks the multi-target batch of station routes, with exact parity against the reference routes.
  The same measurement at that commit, on November 8, 2026, gave these readings.
  A test process of another project used about one core on the host.

  | Case | Achieved speed | Server CPU | GC cycles | Peak RSS | Stream rate |
  | --- | ---: | ---: | ---: | ---: | ---: |
  | Ordinary LondonFull | 60.0, 59.9 | 22.8 s, 21.6 s | 57, 57 | 229 MiB, 229 MiB | 128 kB/s |
  | D without Express pods | 60.0, 60.0 | 27.5 s, 26.5 s | 19, 20 | 224 MiB, 227 MiB | 130 kB/s |
  | D | 59.9, 59.9 | 28.4 s, 28.5 s | 21, 21 | 223 MiB, 225 MiB | 130 kB/s |
  | D with hub parties | 59.9, 59.9 | 39.4 s, 37.5 s | 44, 43 | 233 MiB, 238 MiB | 162 kB/s, 161 kB/s |

  D costs about 28 percent more server CPU than ordinary LondonFull, and the hub parties add about 35 percent more.
  Most of the rest of the bank cost is in `(Network).bankNearest` of the redistribution, which this change does not touch.
  Peak RSS includes the project load.
  Retained frames, decoded client memory, and browser responsiveness are not measured yet.
- `TestLondonExpressRemoteLoads` runs the Go client against a live D session over HTTP and WebSocket.
  The client reads the D topology, and its stream shows Express pods with parties of 12, 13, and 20 aboard.
  `TestLondonExpressRemoteRecovery` runs the sequence recovery, the topology replacement of a new epoch, and the atomic rejection of the Go client on D frames.
  The WASM client and desktop Chrome are not checked yet.
- With the index, D runs at about 170 s of wall time per simulated hour, with the oracle at each tick and without saves.
- The Express byte proofs pass under the current caps.
  `TestExpressStateFileWorstCaseSize` measures a saved state of 105,174,023 bytes against the 128 MiB cap.
  `TestExpressStreamMaximumEncoding` measures a full stream of 37,270,755 bytes and a replacement delta of 37,316,293 bytes against the 64 MiB cap, and an HTTP state of 70,826,933 bytes against the 128 MiB cap.
  The stream bound holds because an Express restore refuses a dispatch reason longer than the longest reason that the simulation writes (`sim.MaxExpressDispatchReasonBytes`, 90 bytes).

## Amendments

Each row needs its own answer.

| # | Document and row | Proposed text |
| ---: | --- | --- |
| E1 | Express physical contract, Foundation parity | Qualification includes the opt-in preset `london-full-express`. Ordinary LondonFull, its fleet, and its defaults do not change. Idle Express pods keep their current behavior. |
| E2 | Express physical contract, Route and station eligibility, and Physical geometry | D has banks at Waterloo and Bank, dedicated Express lanes, and separate Express-only parking that is not a depot. Qualification covers every bank berth and both parking directions. The current geometry and class rules stay. |
| E3 | Express physical contract, Motion and ownership, and Independent safety oracle | The oracle checks the mixed fleet of D at each tick, including fault stops and emergency journeys. The indexed envelope construction must give the same locations, in the same order, as the reference construction in the tests. |
| E4 | Express physical contract, Physical restore, and Rejection and disabled policies | Each planned D checkpoint restores at the physical tier with no demotion, requeue, drop, or interruption. A restored route distance may differ from the saved value by at most `restoreTolerance`, and all other saved pod members are equal. Speed resets and rebuilt forward grants are reported apart. After a restore with the policies off, the current members finish and no new member forms. |
| E5 | Express physical contract, Save bytes and shape | Use the current formats and Express limits. Prove the bounds of the save, the full stream, the replacement delta, the HTTP state, the topology, and the gzip expansion with the current adapters. Include mixed-fleet, bank, incident, and emergency members. A bound over a cap needs a new decision. |
| E6 | Express physical contract, Stream and public consumers | Qualify D through the Go remote client, HTTP, WebSocket, WASM, and desktop Chrome. Cover loads of 12, 13, and 20 riders, topology replacement, sequence recovery, and atomic rejection. The maintainer does the Chrome check. |
| E7 | Express physical contract, Resource and consumer cost | Measure D, ordinary LondonFull, and D without its Express pods. Report CPU, peak resident memory, GC, retained frames, network rate, and browser responsiveness. Wire size does not bound decoded memory. |
| E8 | Express physical contract, Orders and registry, and Storage and recovery | Use the current limits: 600 pods under `express-v1`, 17,000 pending and outstanding orders, and 20 stored Express riders for each pod. Keep whole-party admission, immutable service identity, history rollover, and conservation over repeated recovery. |
| S1 | Standee contract, section 7.2, the Fault, Dwell, and Restore and stream rows | Run these rows again on D with standees, mixed traffic, both hub banks, and emergencies. The current class motion and dwell bounds apply. The `rail-hub` evidence does not qualify London. |
| A15 | Stage 7, section 5.1.9, the Safety, Contract, Conservation, Progress, Persistence, and Sampler soak rows | Add D as a named project, with the branch shape in "London soak row" below. Apply the A10 checks and rule A with A13. The Rail row does not apply to D. |
| A16 | Stage 7, CI soak, and A12 | Add a CI case of one simulated hour on D with the full check at each tick. Before a release tag, run D for 5 seeds of 6 simulated hours and a clear of 30 minutes, with the full check at each tick. |
| A17 | Stage 7, Performance, Sampled-tick latency, Worst choice tick, and Web | Add D to the measurements that apply. A7, A11, and the current latency limits stay. |
| A18 | Stage 7, Formats, and A4 | Express stays unproved until the Express byte proof of E5 passes. When it passes, record its source and its margins next to the plain proofs. |
| B1 | Station banks, Formats and restore | D is an opt-in qualification preset. Its bank members and routes survive the project, save, and stream round trips. No new saved bank member and no format version change. |

## London soak row

The row uses the London branch length and keeps redistribution and pickup reassignment on in every arm.

| Arm | Branches | Configuration |
| --- | ---: | --- |
| `incidents` | 3 | Commands and sampler, faults and emergencies |
| `emergencies` | 3 | Emergencies, faults off |
| `incidents-low-demand` | 3 | Faults and emergencies, low demand |
| References | 1 each | Ordinary and low-demand references |

LondonFull sets a platoon limit of 4, so platoons are on in each arm.
The `platoons` and `reference-platoons` arms set the same limit, and in the first run they gave the same results as `incidents` and `reference`.
So the row has no separate platoon arm.

Each arm has a 30-minute warm save.
Each branch restores through the encoded save path, reseeds its streams, runs one simulated hour, clears its incidents, and runs 30 more minutes.
A rewind runs every 10 simulated minutes.
The row has 9 incident branches, 2 reference branches, and 5 warm runs, about 19 simulated hours.

Demand is the London background demand and a fixed hub-party schedule in both directions.
The low-demand arm starts at 1 background order/min and 1 hub-party offer/min, split between the directions.
If its reference queue reaches 200, a lower rate is chosen and frozen before the row runs.
Directed fixtures, not random coverage, supply 20 pooled singletons and the fault and emergency states with standees.
The soak logs the reason for each refused incident command.

## Not in scope

- The adoption comparison and its gates in [experimental adoption](../design/experimental-adoption.md).
- A rail companion fixture for D.
- A change to idle Express behavior, to a cap, or to a default.
