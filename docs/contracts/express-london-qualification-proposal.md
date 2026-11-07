# Express London qualification proposal

Status: proposed on November 7, 2026, and not approved.
The text of the approved contracts does not change until the maintainer approves the amendments below.

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
- With the index, D runs at about 170 s of wall time per simulated hour, with the oracle at each tick and without saves.

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
| `incidents` | 3 | Commands and sampler, faults and emergencies, platoons off |
| `emergencies` | 3 | Emergencies, faults off, platoons off |
| `platoons` | 3 | Faults and emergencies, Compact platoons on |
| `incidents-low-demand` | 3 | Faults and emergencies, low demand |
| References | 1 each | Ordinary, platoon, and low-demand references |

Each arm has a 30-minute warm save.
Each branch restores through the encoded save path, reseeds its streams, runs one simulated hour, clears its incidents, and runs 30 more minutes.
A rewind runs every 10 simulated minutes.
The row has 12 incident branches, 3 reference branches, and 7 warm runs, about 26 simulated hours.
At the pilot rate above, that is about 75 minutes of wall time before saves and rewinds.

Demand is the London background demand and a fixed hub-party schedule in both directions.
The low-demand arm starts at 1 background order/min and 1 hub-party offer/min, split between the directions.
If its reference queue reaches 200, a lower rate is chosen and frozen before the row runs.
Directed fixtures, not random coverage, supply 20 pooled singletons and the fault and emergency states with standees.
The soak logs the reason for each refused incident command.

## Not in scope

- The adoption comparison and its gates in [experimental adoption](../design/experimental-adoption.md).
- A rail companion fixture for D.
- A change to idle Express behavior, to a cap, or to a default.
