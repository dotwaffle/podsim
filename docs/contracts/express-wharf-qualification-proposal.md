# Express wharf qualification proposal

Status: approved by the maintainer on 2026-11-24, rows HX1 to HX13.
The preset is not qualified while a row is open, and no evidence run has started.
The maintainer chose option A of HX-D1 and HX-D3 to HX-D6, and option C of HX-D2.

## Scope

This proposal qualifies the opt-in preset `london-full-shared-express-wharf` of the [Express London hubs](../design/london-hubs-d8-d9.md) design.
Slice 3 of that design built the preset in `59fe0e43`.
Decision HB-D7 chose new rows for each new preset, and section 4.4 of the design lists them.
The rows follow SX1 to SX13 of the [Express shared-track qualification](express-shared-track-qualification-proposal.md), and they do not change that contract.
The evidence of D and of `london-full-shared-express` does not transfer.

The rows apply to the network with SHA-256 `d68a69b8c326f292f7c3a7839b51bf9837c5e714287f60470436781705217359`.
A change of the network opens each row again.

The proposal does not approve runtime adoption, a default change, deployment, or cloud spending.
A pass is not adoption evidence.

## Geometry

The preset is `london-full-shared-express` with a second line.

- The route between London Bridge and Canary Wharf uses the lanes of links 089, 088, and 154 in each direction, with one junction lane at Bermondsey and one at Canada Water.
- Decision HB-D13 curves `940GZZLULNB-road-out-01` by +30 m, and no lane is removed.
- Decision HB-D14 puts a comb of 16 berths, `express-parking-lnb`, at site LB3, with no exemption in the layout audit.
- The network has 5,840 nodes and 9,156 lanes, and the project has 359 pods: 327 Compact and 32 Express.
- The bank audit has 53 shape pairs and no refusal, and the only tail warning is the one of `london-full-shared-express`.
- The two lines share no Express route.

## HX rows

Each row keeps the limits, the pass rules, and the carried clauses of its SX row, with the changes below.
In a carried clause, "the preset" is `london-full-shared-express-wharf`, and "both directions" is both directions of each of the 4 services.

| Row | From | Pass, in addition to the SX row | Evidence that the row needs |
| --- | --- | --- | --- |
| HX1 | SX1 | LondonFull, D, and `london-full-shared-express` keep their network hashes, their fleets, and their defaults. | The pinned hashes of the preset test. The behavior parity tests of D and of `london-full-shared-express`. |
| HX2 | SX2 | Express runs on the route of each line, shared with Compact pods, with no dedicated lane. Each hub berth and each comb berth has an Express route to and from the berths of its line, and no Express route joins the lines. The audit refuses no pair, preparation refuses no lane, and the layout audit finds no conflict with no exemption for the new comb. | The preset tests, `projectcheck`, and the sweep cases of SC6 on the 53 shape pairs and on each merge and split pair with a lane that admits Express. |
| HX3 | SX3 | The oracle, the D6 oracle, and the two envelope constructions run at each tick with both lines in use. Ticks with an Express and a Compact pod together occur on each of links 100, 089, 088, and 154. A fault stop and an emergency occur on each line. | A run of the preset with the counts of these ticks, and the tests of SX3 with the preset among their networks. |
| HX4 | SX4 | The checkpoints include a pod with riders on the new route with a Compact pod, a faulted pod there, a pod on its way to `express-parking-lnb`, and a pod in a maneuver of that comb. | A checkpoint test on the preset. The checkpoints of SX4 on the first line run too. |
| HX5 | SX5 | The byte bounds hold for 9,156 lanes, 359 pods, and 4 services. | A proof that the worst-case project bounds the preset, or the proofs with the topology of the preset. The measured sizes and their margins. |
| HX6 | SX6 | Loads of 12, 13, and 20 riders run on each line. | Remote load and recovery tests, and the Chrome check of the maintainer. |
| HX7 | SX7 | The cost is against LondonFull, D, `london-full-shared-express`, and the preset with no Express pod. | The server measurement of the five cases, and the maintainer's acceptance. |
| HX8 | SX8 | Directed incident and recovery coverage on the new services. | Incident fixtures on the new line. |
| HX9 | SX9 | The standee rows run on each line. | The runs of the rows of section 7.2 of the standee contract. |
| HX10 | SX10 | The preset is a named project of the London soak row, and the driver offers parties on both lines (decision HX-D3). | A soak run of the row with its log. |
| HX11 | SX11 | A CI case of one simulated hour, and 2 seeds of 6 simulated hours with the full check and the D6 oracle at each tick, before a release tag, in the form of decision HX-D2. | The CI case, and the logs of the release runs. |
| HX12 | SX12 | The limits of A7 and A11 and the latency limits stay. | The measurements of the preset, recorded beside those of D and `london-full-shared-express`. |
| HX13 | SX13 | The formats byte proof runs on the preset, with its source and its margins. | The proof of HX5. |

B1 does not apply, because the preset has no banks.

## Known risks

- A `LondonFull` network with no Express gets a closed wait on link 100 at 1/4 of the hub stream of the screen and more ([study](../studies/shared-track-batch.md#the-block-in-lf)).
  `london-full-shared-express` finished each order in the batches of that study, but the runs have 2 hours of arrivals, no incidents, and no demand on the new line.
  In the release soak of 2026-11-17, before the queue promotion, one record passed the Progress limit in the queues of link 100.
  So a failure of HX10 or HX11 can come from link 100 and not from the new line.
- No simulation of the preset has run.
- `mise run soak:full` takes 6,277 to 6,427 s of its limit of 7,200 s with four presets.
  From the 3,111 s of run-slot time that one seed of `london-full-shared-express` took in the timing run of 2026-11-19, 2 seeds of one more preset add about 2,100 s with 3 full slots, so five presets do not fit (inferred).
- `test:shared-express` took 292 s by hand.
  A copy of it for the preset adds about the same time (inferred).

## Decisions

| ID | Question | Options | Recommendation |
| --- | --- | --- | --- |
| HX-D1 | The rows | A: approve HX1 to HX13 as they are. B: approve them with changes. C: no qualification, and the preset stays a study preset. | A |
| HX-D2 | The release soak of HX11 | A: a separate run of `soak:full` with `PODSIM_SOAK_PRESETS=london-full-shared-express-wharf`, with its own limit of 2 hours. The default presets and amendments A16 and A19 do not change. B: add the preset to the defaults and raise the limit, which needs an amendment of the stage 7 contract. C: replace `london-full-shared-express` in the defaults, which changes the evidence of SX11. | A |
| HX-D3 | The hub parties of the soak driver | A: the stream of D on Waterloo and Bank, and a second stream on London Bridge and Canary Wharf with one party each 28 s, 17 in 20 from London Bridge. Party sizes average 10.5, so it is about 22.5 passengers a minute, 19.1 from London Bridge and 3.4 from Canary Wharf, near the 19.6 and 3.1 of the AM peak in section 3.1 of the design. B: the stream of D on both lines, about 42 passengers a minute and three in four from the first hub. C: no parties on the new line, which does not test it. | A |
| HX-D4 | History rollover in HX8 | A: the exception that the maintainer accepted for SX8 applies if the directed runs show that the preset does not reach rollover. B: the row needs rollover. | A |
| HX-D5 | Where the long tests run | A: a new task `test:wharf-express` with its own CI job. B: more patterns in `test:shared-express`. | A, because a job has its own limit and the tests run in parallel. |
| HX-D6 | The Chrome checks of HX6, HX7, and HX9 | A: one session by the maintainer on one commit, after all the other rows pass. B: one session for each row. | A |

For HX-D2, option C puts the preset in the place of `london-full-shared-express` in the defaults of `mise run soak:full`.
The rule of amendment A19 applies to each default preset and lists none by name, so its text does not change.
SX11 does not change either: it still needs 2 seeds of `london-full-shared-express` before a release tag, and `soak:full` does not run them after the swap.
For HX-D7, the maintainer chose to amend SX11, and the draft is [below](#draft-amendment-of-sx11).
Until the amendment is approved and HX11 passes, the defaults of `soak:full` stay as they are.

Approval of a row does not authorize a push.
Each long run needs a grant when it starts.

## Order of evidence

1. Static and unit rows: HX1, HX2, HX5, HX13, with the fixture of the preset.
2. Simulation rows: HX3, HX4, HX6 (without Chrome), HX8, HX9.
3. Soak and timed rows: HX10, HX11, HX12, HX7, on a quiet machine and with a timing seed first.
4. The Chrome session of HX-D6.

A failure stops the order and goes back to the maintainer.

## Draft amendment of SX11

Status: proposed on 2026-11-24, not approved.
The [shared-track contract](express-shared-track-qualification-proposal.md) does not change until the maintainer approves this text.

The pass rule of SX11 becomes: a CI case of one simulated hour on `london-full-shared-express`, with the full check and the D6 oracle at each tick.
The release soak before a release tag runs on `london-full-shared-express-wharf` under HX11, in place of `london-full-shared-express`.
The amendment takes effect when HX11 passes, together with the change of the default presets of `soak:full`.
The release soak that passed on `london-full-shared-express` stays as its record.

- For: the new preset has the Waterloo and Bank line with the same lanes, the same comb, and the same hub stream, and the release soak stays inside its limit of 2 hours.
- Against: the evidence of one preset does not transfer to the other, so a later change that breaks only `london-full-shared-express` over 6 simulated hours can reach a release.
  The preset keeps the CI case and the London soak row of SX10, and SX10 is not a CI task.
