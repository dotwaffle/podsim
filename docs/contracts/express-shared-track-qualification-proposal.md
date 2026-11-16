# Express shared-track qualification proposal

Status: approved by the maintainer on 2026-11-16, rows SX1 to SX13.
The [shared-track amendment proposal](shared-track-amendment-proposal.md) gave the rows, and decision S6-A of the [shared-track](../design/shared-track-preset.md) design chose a new contract for them.
No simulation has run on the preset, so the preset is not qualified.
Amended 2026-11-16, by maintainer decision: SX3 compares the compiled envelope reach with an independent reference, in amendment ST22 of the [second shared-track amendment proposal](shared-track-amendment-2-proposal.md).

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
No simulation has run on the preset, so each evidence item below is evidence that the row needs, and none exists yet.

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
