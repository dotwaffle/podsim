# Incident stage 7 contract proposal

Status: approved by the maintainer on November 4, 2026, as option B with amendments A1 to A4, after two review rounds.
Status 2026-11-05: `stateVersion` is now 11, `StreamVersion` is 9, and the member list is `state_v11_members.txt`, so `state_v10_members.txt` below is history.
It was drafted against `57902a7`.
Each `file:line` reference is for that tree, unless it names a commit.
References to the old `README.md` and `PROJECT_BRIEF.md` name `485f2f5`.
On November 5, 2026, the maintainer removed the congestion, queue, and predictive routing policies, so the latency gates run only free-flow routing.

## 1. Goal

A project can set how often random faults and rider emergencies happen.
The simulation then starts them by itself, and a run stays deterministic through a save, a restore, a rewind, and a reset.
The map shows debris, the user can place and clear debris, and the numbers show what the incidents cost the service.
A project with both rates at 0 behaves as it does today, with the exceptions that section 5.1.5 names.

The recommendation is option B of section 7, with the answers of section 8.

These rules are settled, and this proposal does not reopen them:

| Rule | Source |
| --- | --- |
| Faults and emergencies are always on, with no project switch. | Marker removal contract, maintainer choices |
| `faults` and `emergencies` keep all their members. `perHour` must be 0 until stage 7 accepts more. | Marker removal contract, Project |
| Stages 4 to 6 stay dropped. This proposal has no reverse motion, train interruption, or group exit. | Maintainer, November 4, 2026 |
| The sampler mechanism is fixed: own PCG streams, the seed rule, the interval formula, the draw count, and skip with draws kept. So `perHour` counts attempts, and a skip lowers the real rate. | Suspension contract section 12.2, emergency contract section 10.2 |
| The rate unit is events for each simulated hour, from 0 to 60, with `fixed`, `uniform`, and `exponential` durations. | Suspension contract 12.1 and P6, emergency contract 10.1 |
| No backward compatibility until v1, and no migrations. | `AGENTS.md` |
| The order and the text of refusals are format. | `AGENTS.md` |

## 2. Current state

### 2.1 By element

| Element | What exists | What is missing | Evidence |
| --- | --- | --- | --- |
| Sampler | Nothing runs. `internal/sim` imports no random source. | The sampler, its state, its saved members, and its counters. | No `math/rand` import in non-test `internal/sim` files. `internal/project/faults.go:90`, `internal/project/emergencies.go:37` |
| Fault start | `startPodFault`, `startDebris`, and the `Fault` command. Each refusal changes nothing. | A start in ticks. Both starts take whole seconds. | `internal/sim/faults.go:122`, `internal/sim/debris.go:20`, `internal/sim/fault_commands.go:26` |
| Emergency start | `Emergency`, public, with its checks and a final `observe`. | An internal start with no `observe`, which emergency contract section 13 names. | `internal/sim/emergencies.go:82`, `internal/sim/emergencies.go:119` |
| Stages | `faultStage` and `emergencyStage` run in `Step` behind their switches. | No sampler call. | `internal/sim/simulation.go:853`, `internal/sim/simulation.go:859`, `internal/sim/faults.go:453`, `internal/sim/emergencies.go:146` |
| Policy | Withdrawal, evacuation, endpoint reroute, the emergency tier and the station choice. | No document defines "the full policy" (section 3, G2). Refuge code exists, and only a test entry reaches it. | `docs/contracts/incident-suspension-contract-proposal.md:68`, `internal/sim/operational.go:246`, `internal/sim/incident_entry.go:34` |
| Scenarios | Nine presets and capacity flags. | No preset or flag sets `faults` or `emergencies`. | `cmd/scenario/main.go:25`, no `Faults` in `internal/scenarios` or `cmd/scenario` |
| Tools | `cmd/compare` and `internal/parkride` accept a project with `faults` and run with the operations off. | A rule for a rate above 0. `loadScenario` drops both settings (`cmd/compare/schedule.go:85`). | `internal/parkride/run.go:100`, `cmd/compare/schedule.go:113`, `docs/operations.md:542` |
| Controls | Inspector buttons **Fault**, **Clear fault**, and **Emergency**. Both hide with `operationsOff`. | A debris control, a lane pick, and a debris clear. The editor has no form for these settings. | `internal/view/fault.go:40`, `internal/view/emergency.go:41`, `internal/view/mappick.go:66`, `docs/operations.md:541` |
| Metrics | Five fault counters and three emergency counters in frames and saves. | Skip counts, effect on service, and any OpenTelemetry series for incidents. | `internal/sim/fault_view.go:32`, `internal/sim/state_emergencies.go:31`, `internal/session/session.go:77`, `internal/telemetry/telemetry.go:166`, `docs/operations.md:559` |
| Debris drawing | `simulation.faults.active` sends the lane and the segment to the client. | The map draws nothing. | `internal/view/draw_network.go:24` has no read of `state.Faults`, `docs/operations.md:549` |
| Qualification | The suspension and emergency contracts list soak gates. | I found no fault or emergency soak driver, and no record that the gates ran. The composed fixtures of the Formats gates do not exist. | `docs/contracts/incident-suspension-contract-proposal.md:1601`, `docs/contracts/incident-emergency-contract-proposal.md:1427`, no such test in `internal` or `cmd`, no incident file in `docs/measurements` |

### 2.2 Settings accepted and unused

| Member | Accepted values today | Read by | Evidence |
| --- | --- | --- | --- |
| `faults.evacuationSeconds` | 0 to 3,600 | The fault stage. This is the only member in use. | `internal/project/experimental.go:44` |
| `faults.perHour` | 0, and `-0` | Validation only | `internal/project/faults.go:32`, `internal/project/faults.go:90` |
| `faults.debrisShare`, `debrisMeters`, `duration` | 0 to 1, 0.5 to 50, and three kinds with their values | Validation only | `internal/project/faults.go:35`, `:38`, `:40` |
| `emergencies.perHour` | 0, and `-0` | Validation only | `internal/project/emergencies.go:37` |

### 2.3 Parts that stage 7 reuses without a change

- Demand has the pattern for saved random state: `rand.PCG` in a `[]byte` member, checked at decode and restored with `UnmarshalBinary` (`internal/session/state_file.go:208`, `internal/session/state_file.go:755`, `internal/session/persist.go:611`).
- `depguard` allows every standard library package in `internal/sim` (`.golangci.yml:130`), so `math/rand/v2` needs no lint change.
- `Clone` copies `Simulation` by value and then deep-copies the slices (`internal/sim/clone.go:12`).
- `faultTicksFit` already refuses a fault too near the tick limit, so such a sampled event is a skip (`internal/sim/faults.go:129`, `internal/sim/debris.go:27`).

## 3. Gaps and conflicts in the approved text

Each row needs a rule before code.
The approved contracts stay as written, and approval of this document adds a dated status entry to each one.
A change to an approved gate is an amendment in section 5.1.9, and it needs its own answer.

| # | Gap | Evidence | Proposed rule |
| --- | --- | --- | --- |
| G1 | The sampler members were "saved under the fault marker". The marker is gone. | Suspension 12.2 | The rate decides presence (section 4.2). |
| G2 | "The full policy" has no definition in any document. | Suspension `:68`, transitions `:66-67` | Section 5.2 lists the candidates, and decision 2 chooses. |
| G3 | Durations are "rounded up to whole ticks", and both starts take whole seconds. | Suspension 12.2, `internal/sim/faults.go:122`, `internal/sim/debris.go:24` | Keep the contract. Add an inner start that takes ticks, and keep the seconds form as a wrapper. About 70 test calls use the seconds form. `record.end` is in ticks already, so the format does not change. |
| G4 | The seed text cites `RailSchedule` as its model, with a big-endian seed. `RailSchedule` uses little-endian. | Suspension 12.2, `internal/project/rail_arrivals.go:121` | Keep the contract's big-endian seed prefix. The citation names the method only. |
| G5 | The byte order of the two `uint64` seed words is not stated. | Suspension 12.2, emergency 10.2 | Little-endian, as `internal/project/rail_arrivals.go:123`. A golden test pins the first draws. |
| G6 | A very small rate gives an interval above `MaxCounter` or `+Inf`, and a save then fails the integer scan. | Formula of 12.2, `internal/session/state_file.go:587` | The terminal rule of section 5.1.1. |
| G7 | `duration` is "required when `perHour > 0`", and no refusal exists. | Suspension 12.1, `internal/project/faults.go:99` | New refusal, section 4.3. |
| G8 | "Skipped and counted" names no counter. | Suspension 12.2 and 12.5, emergency 10.2 and 10.5 | `sampled` and `skipped` in each counter group. |
| G9 | "The internal start" of an emergency does not exist. | Emergency section 13, `internal/sim/emergencies.go:119` | Split `Emergency` into `startEmergency` and `observe`. The order and text of its refusals stay. So the `Emergency` of the F11 transition row (emergency `:530`) is `startEmergency`. |
| G10 | `ConfigureExperiments` also runs after a restore, so a sampler that starts there would replace the restored stream. | `internal/session/persist.go:531` | `SetFaults` and `SetEmergencies` never draw. A separate idempotent entry starts the samplers (section 5.1.2). |
| G11 | The contracts ask for pod ID order, and they do not say how to get it. | Suspension 12.2, emergency 10.2 | `s.vehicles` is already in `Pod.ID` byte order: `newFleet` sorts the placements (`internal/sim/prepared.go:271`), `Reset` builds the vehicles in that order (`internal/sim/simulation.go:593`), and a restore refuses saved pods out of that order (`internal/sim/state_contract.go:317`). The sampler walks `s.vehicles` and sorts nothing. A test pins the order. |
| G12 | The capability matrices list train, approach, and compact queue rows that have no code. Faults refuse platoon members only. Emergencies accept platoon members and defer them. | `internal/sim/faults.go:363`, `TestEmergencyPlatoonDrain` in `internal/sim/emergency_groups_test.go`, emergency 1.4, `23d9381`, no `coupling` text in non-test `internal/sim` files | The sampler uses the code as it is. Decision 3. |
| G13 | The widest rate for the size check is `-0`, because 0 is the only accepted value. | `internal/project/faults.go:67`, `internal/project/emergencies.go:20` | Decision 15 chooses between the widest value and a measure of the real rate. |
| G14 | The Performance gate gives 300 pod faults as the worst case, and the fleet limit is now 1,000. | Suspension section 17, `:1324` status lines | The gate stays at 300. Section 5.1.9 adds a report at 1,000. |
| G15 | A sampled emergency advances twice in its start tick: in the start and in the stage loop. | Emergency 10.2 places the sampler before the records. `internal/sim/emergency_choice.go:97` | Keep the place. The second call does nothing for a bound pod, and the no-candidate memo limits a failed search. A test pins one search for each start. |
| G16 | The sampler text does not cover a demo save that the logical tier turns into an ordinary fleet. | `internal/sim/state_logical.go:32`, `internal/session/persist.go:531` | The start rule of section 5.1.3. |

Documents that stage 7 makes false are listed in section 9.

## 4. Format

This section is shared by all elements.

### 4.1 Effect by option

| Effect | Option A | Option B | Option C |
| --- | --- | --- | --- |
| `stateVersion`, `StreamVersion`, `StateMediaType` | By the version rule below | The same | The same |
| Saved-state growth, widest | 244 bytes | 371 bytes | Not sized |
| Full stream and HTTP growth, widest | 108 bytes | 235 bytes | Not sized |
| New command action or digest extension | None | None | Yes, for a new setting. Extension 15 is taken, so 16 or later. |
| Command digest baseline | Same | Same | Changes for a project with the new setting |

Version rule: the consent contract keeps a version that is not yet pushed (`docs/contracts/generated-order-consent-contract-proposal.md:64`).
`origin/main` at `f8bedf3` has state 9 and stream 7, and `57902a7` has state 10, stream 8, and `state-7`, so these three are not pushed today.
When the format patch lands, each version that has been pushed moves up by one, once, and a version that has not been pushed stays.
With a move, `internal/session/state_http.go:17`, `web/shell.js:127`, `web/editor.js:1030`, the golden files, and the version text of `docs/operations.md` and `docs/protocol.md` change, as `AGENTS.md` says.
In both cases `internal/session/testdata/state_v10_members.txt`, or its next version, gains the new paths.
The plain stream golden files have no version member, so they must not change.

### 4.2 Saved-state members

| Path | Go type | Presence |
| --- | --- | --- |
| `/simulation/faults/random`, `nextTick` | `[]byte`, base64 of the 20 bytes of `rand.PCG.MarshalBinary`. `*int64`, so `"nextTick":0` is present. | Both, exactly when the fault sampler is live. |
| `/simulation/emergencies/random`, `nextTick` | As for faults | Both, exactly when the emergency sampler is live. |
| `counters/sampled`, `skipped` in each group | `int64` | Each one is omitted at 0. |
| `/simulation/faults/counters/clearedTicks`, `delayedRiderTicks` | `int64` | Option B. Each one is omitted at 0. |
| `/simulation/emergencies/counters/endedTicks`, `maxTicks` | `int64` | Option B. Each one is omitted at 0. |

A sampler is live when its project rate is above 0 and the fleet is not a demo fleet.
A project with a rate has `simulation.faults` from tick 0, with `random` and `nextTick` and no `records` or `counters`.
`validateFaultValues` and `validateEmergencyValues` treat such a member as not empty, and their text stays (`internal/session/fault_state.go:69`, `internal/session/emergency_state.go:28`).
A demo fleet has no sampler, and `CheckIncidentPolicy` keeps refusing a demo save with either member (`internal/sim/state_emergencies.go:206`).

No prescan limit changes: no new array exists, and `nextTick` passes the integer scan of `internal/session/state_file.go:587`.
`checkFaultMembers` and `checkEmergencyMembers` refuse `null` on the new paths, because each path is below the scanned member.

Stream and HTTP frames get the same counters, in the existing `faults` and `emergencies` groups, each omitted at 0.
`random` and `nextTick` never go in a frame, because a client has no use for the future of the stream.
A frame with either one has an unknown member, and the strict decoder refuses it.

### 4.3 Refusals

Each check that is not listed keeps its place and its text.
Tests pin each order with inputs that have more than one error.

Project (`validateFaultSettings`, `internal/project/faults.go:85`, and `validateEmergencies`, `internal/project/emergencies.go:37`):

| Order | Check | Text |
| ---: | --- | --- |
| 1 | `evacuationSeconds` out of range | Same |
| 2 | `perHour` not from 0 to 60, also NaN | `faults perHour must be 0 to 60`. This replaces `faults perHour must be 0`. `-0` stays valid and means 0. |
| 3 | `debrisShare`, then `debrisMeters`, then each `duration` check, in their present order | Same |
| 4 | New: `perHour` above 0 and no `duration` | `faults perHour above 0 needs a duration` |
| 1 of `validateEmergencies` | `perHour` not from 0 to 60, also NaN | `emergencies perHour must be 0 to 60`. This replaces `emergencies perHour must be 0`. |

The decode order of the marker removal contract stays, and the editor uses the same validators.

Saved state, in `checkSavedFaults` (`internal/sim/state_faults.go:86`), which runs before either tier.
A demo save is the rule of `CheckIncidentPolicy` (`internal/sim/state_emergencies.go:198`), moved into one helper.

| Order | Check | Text |
| ---: | --- | --- |
| 1 | The whole `simulation.faults` member is absent, the project rate is above 0, faults are on, and it is not a demo save | New: `invalid saved fault: a rate above 0 needs the sampler members` |
| 2 | The whole `simulation.faults` member is absent, otherwise | Accepted, as today |
| 3 | Faults off | `saved faults need faults on`, as today |
| 4 | Negative counter, now with the new counters | `invalid saved fault: a fault counter is negative`, as today |
| 5 | New: exactly one of `random` and `nextTick` | `invalid saved fault: random and nextTick need each other` |
| 6 | New: both members and a rate of 0 | `invalid saved fault: the sampler members need a rate above 0` |
| 7 | New: `simulation.faults` is present with neither sampler member, a rate above 0, and it is not a demo save | `invalid saved fault: a rate above 0 needs the sampler members`, the text of row 1 |
| 8 | New: `random` fails `UnmarshalBinary` | `invalid saved fault: the random source is not valid` |
| 9 | New: `nextTick` is not `sim.MaxCounter`, and it is not from the saved tick + 1 to `sim.MaxCounter` | `invalid saved fault: nextTick %d is outside %d to %d` |
| 10 | The record checks, in their present order | Same |

Saved state, in `checkSavedEmergencies` (`internal/sim/state_emergencies.go:82`), with emergencies on:

| Order | Check | Text |
| ---: | --- | --- |
| 1 | `savedEmergencyParties`, with the new counters in its counter check | Same |
| 2 | New: exactly one of `random` and `nextTick` | `invalid saved emergency: random and nextTick need each other` |
| 3 | New: both members and a rate of 0 | `invalid saved emergency: the sampler members need a rate above 0` |
| 4 | New: neither sampler member, also with the whole `simulation.emergencies` member absent, a rate above 0, and it is not a demo save | `invalid saved emergency: a rate above 0 needs the sampler members` |
| 5 | New: `random` fails `UnmarshalBinary` | `invalid saved emergency: the random source is not valid` |
| 6 | New: `nextTick` is not `sim.MaxCounter`, and it is not from the saved tick + 1 to `sim.MaxCounter` | `invalid saved emergency: nextTick %d is outside %d to %d` |
| 7 | `checkSavedEmergencyPod` for each pod | Same |

Presence does not depend on value.
`random` is present when the decoded slice is not nil, so an empty string is present and fails at `UnmarshalBinary`.
`nextTick` is present when the decoded pointer is not nil, as `FaultView.EvacuateTick` does (`internal/sim/fault_view.go:27`).
So tests pin these explicit-zero cases:

| Save | Result |
| --- | --- |
| Rate 0, a counter, and only `"nextTick":0` | Row 5 |
| Rate 0, a valid `random`, and `"nextTick":0` | Row 6 |
| Rate above 0, a valid `random`, and `"nextTick":0` | Row 9 |
| Rate above 0, `"random":""`, and a valid `nextTick` | Row 8 |

A save that fails any of these moves aside with `invalid_state`, and the server starts a new session.

A file cannot fail the upper bound of fault row 9 or emergency row 6, because the integer scan (`internal/session/state_file.go:587`) first refuses a value above `sim.MaxCounter` with its own error.
No row refuses `skipped` above `sampled`.
F13 and E9 refuse such a save after each tier, and `TestSamplerCountersAtRestore` pins this.

Stream and HTTP:

| Check | Text |
| --- | --- |
| Negative counter in `checkFaultFrame`, with the new counters | `negative fault counter`, as today |
| Negative counter in `checkEmergencyFrame`, with the new counters | `negative emergency counter`, as today |
| `operationsOff` with a nonzero counter | `stream state with operations off contains a fault member` and its emergency form, as today. Both checks compare the whole struct, so they cover the new counters. |

### 4.4 Byte bounds

Each counter is `sim.MaxCounter`, which has 16 digits.

| Member | Bytes | Option |
| --- | ---: | --- |
| `"random":"<28 base64 characters>",` | 40, in each of 2 groups, saves only | A, B |
| `"nextTick":9007199254740991,` | 28, in each of 2 groups, saves only | A, B |
| `"sampled":9007199254740991,` | 27, in each of 2 groups | A, B |
| `"skipped":9007199254740991,` | 27, in each of 2 groups | A, B |
| `"clearedTicks":9007199254740991,` | 32 | B |
| `"delayedRiderTicks":9007199254740991,` | 37 | B |
| `"endedTicks":9007199254740991,` | 30 | B, decision 11 |
| `"maxTicks":9007199254740991,` | 28 | B |

| Format | Marker contract total | Measured before stage 7 | Cap | Option B growth | Measured after stage 7 | Headroom after stage 7 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Saved state | 125,979,325 | 125,979,358 | 134,217,728 | 371 | 125,979,729 | 8,237,999 |
| Full stream | 50,735,035 | 50,734,862 | 67,108,864 | 235 | 50,735,097 | 16,373,767 |
| HTTP state | 84,291,213 | 84,291,040 | 134,217,728 | 235 | 84,291,275 | 49,926,453 |

The growth figures are arithmetic, the two proofs confirm them, and no cap or count limit changes.
The measured totals come from the two proofs, at the commit before `2c0a833` and at `3c61bcb`.
They differ from the marker contract totals by 33 bytes in a save and by 173 bytes fewer in a stream, from changes outside stage 7 that this document does not trace.

| Proof | Change |
| --- | --- |
| `TestPlainStateFileWorstCaseSize` | `addWidestSavedMembers` (`internal/session/plain_byte_proof_test.go:45`) sets `Random` to a valid 20-byte state, `NextTick` to `sim.MaxCounter`, and each new counter to `sim.MaxCounter`. The scalar filler cannot build a valid PCG state. |
| `TestPlainStreamMaximumEncoding` | `widestPlainStreamRecordBytes` (`internal/session/plain_byte_proof_test.go:204`) sets the new counters. |

The project member is unchanged in these proofs, because the fixture pads the project to its independent cap.
A delta still has no byte proof, and Express still has no byte proof.
Stage 7 adds the same few bytes to an Express file, and its headroom is not known.

### 4.5 Command digests

Stage 7 adds no command action and no `Command` field.
The debris control uses the existing `fault` action, with extensions 5, 6, and 7.
`faults` and `emergencies` are already extensions 3 and 11 (`internal/project/config.go:195`), so a project with a rate gets a new digest by its value, as any settings change does.
Options A and B add no `FaultConfig` member, so `internal/session/testdata/command_digests.txt` stays the same, and a test pins it.

## 5. Elements

### 5.1 Sampler

#### 5.1.1 Mechanism

The approved mechanism stays.
The table adds the rules of section 3.

| Item | Fault sampler | Emergency sampler |
| --- | --- | --- |
| Stream | Own `rand.PCG`, a value member of `Simulation`. | Own `rand.PCG`, a value member of `Simulation`. |
| Seed | `SHA-256("podsim-faults-v1" \|\| bigEndian(demand seed))` | `SHA-256("podsim-emergencies-v1" \|\| bigEndian(demand seed))` |
| PCG arguments | The first 16 bytes, as two little-endian `uint64` values. | The same. |
| Uniform draw | `float64(pcg.Uint64()<<11>>11) / (1 << 53)`. This text fixes the form, so a change of `rand.Rand` cannot change a run. | The same. |
| Interval | `tick + max(1, ceil(-ln(1-u) × (3600 × TicksPerSecond) / perHour))`, by the terminal rule below. | The same. |
| Place | The fault stage, before the clears (`internal/sim/faults.go:454`). | The emergency stage, before the records. |
| Due | `nextTick != sim.MaxCounter && s.tick >= nextTick`. `max(1, ...)` gives at most one event in a tick. | The same. |
| Draws for each event, in order | Kind, target, position, duration, then the next interval. | Pod, party, then the next interval. |
| Target | Debris when `kind < debrisShare`, else a pod fault. Pod: the index `floor(u × n)` over the pods without a fault, in `s.vehicles` order (G11). Lane: the first lane in `Network.Lanes` order whose running sum of lengths is above `u × total`. | Pod: the index `floor(u × n)` over all pods, in `s.vehicles` order. Party: the index `floor(u × k)` over the active riders in `Riders` order. |
| Segment | `from = u × (length - debrisMeters)`, `to = from + debrisMeters`. | None |
| Duration | As in 12.2, then `ceil(seconds × TicksPerSecond)` ticks. | None |
| Skip | Any refusal of the start, or `n == 0`. All draws still happen. | Any refusal of the start, or `k == 0`. All draws still happen. |

Each event uses all its draws, whatever the kind, the duration rule, or the outcome.
A pod fault event still uses the position draw, and a `fixed` duration still uses the duration draw.
A segment on a lane shorter than `debrisMeters` has a negative `from`, so the start refuses it and the event is a skip.
At `debrisMeters` 50, rounding can make `to - from` exceed 50, and then the event is also a skip.

Terminal rule, one rule for scheduling, invariants, and restore:

| Case | Rule |
| --- | --- |
| Computed next tick | A result at or above `sim.MaxCounter`, also `+Inf`, is `sim.MaxCounter`. The arithmetic compares in `float64` before any conversion to `int64`. |
| `nextTick == sim.MaxCounter` | The terminal value. It means no more events, it never comes due, and no draw follows. This holds also when the tick reaches `sim.MaxCounter`. |
| Invariant | `nextTick == sim.MaxCounter || nextTick > tick`. |
| Restore | `sim.MaxCounter` is valid at each saved tick. Any other value needs the saved tick + 1 or more (section 4.3). |
| Very small rate | Any rate above 0 is valid. At 60 Hz, the largest interval is about 7.93e6 / `perHour` ticks, so the terminal value comes first below a rate of about 8.8e-10. The largest interval in simulated time does not depend on the rate. |
| A fault near the tick limit | `faultTicksFit` refuses it, so the event is a skip, and the record checks at `internal/sim/state_faults.go:124` stay valid. |

Status 2026-11-06: at 30 Hz, the largest interval is about 3.97e6 / `perHour` ticks, so the terminal value comes first below a rate of about 4.4e-10.

#### 5.1.2 API

```go
// internal/sim
type FaultDurationRule struct {
	Kind                                         string
	Seconds, MinSeconds, MaxSeconds, MeanSeconds int
}

type FaultRate struct {
	PerHour, DebrisShare, DebrisMeters float64
	Duration                           FaultDurationRule
}

// IncidentRates holds the scenario rates of a project.
type IncidentRates struct {
	Faults             FaultRate
	EmergenciesPerHour float64
	FaultSeed          [2]uint64
	EmergencySeed      [2]uint64
}

// StartIncidentSampling starts each sampler that has a rate above 0, its
// switch on, and no live sampler. Its first interval starts at the current
// tick. A live sampler keeps its stream.
func (s *Simulation) StartIncidentSampling(rates IncidentRates) error

// internal/project
func EffectiveIncidentRates(config Config) sim.IncidentRates
func ConfigureIncidentSampling(simulation *sim.Simulation, config Config) error
```

`ConfigureIncidentSampling` does nothing when both rates are 0, so no PCG exists and no draw happens.
`incidentSeeds` is private to `project`, because `RailSchedule` also hashes in that package.
`SetFaults(false, settings)` and `SetEmergencies(false)` stop their sampler, and neither ever draws.
`Reset` stops both samplers.
A test entry, with no build tag, stops both samplers for the soak (section 5.1.9).
`sim` keeps one check for a direct caller: a rate outside 0 to 60 gives `incident rate %g is outside 0 to 60`.
A rate with its switch off starts nothing, so a call for a demo fleet needs no special case.
`RestoreStateInput` and `PreparedRestoreInput` gain `Rates IncidentRates`, for the presence checks and for the restored samplers.
`setRestoredFaults` and `setRestoredEmergencies`, which both tiers call, install the rates, the saved stream, and `nextTick`.

#### 5.1.3 Data flow and start rule

One rule covers each path: each `ConfigureExperiments(..., true)` is followed by `ConfigureIncidentSampling`.

| Caller | Call | Result |
| --- | --- | --- |
| Session start (`internal/session/session.go:351`) | Both, with `true` | Samplers from tick 0. |
| Session reset (`:781`) | Both, with `true` | `Reset` stopped the old samplers, so new ones start from tick 0. |
| Project apply (`:919`) | Both, with `true` | New fleet, new samplers from tick 0. |
| Session demo start (`:807`) | `ConfigureExperiments(..., false)` | No sampler. |
| Session restore (`internal/session/persist.go:531`) | `ConfigureExperiments(..., operations)`. With `operations` true, `ConfigureIncidentSampling` follows, before `restoreDemand` (`:546`). | See below. |

Restore cases, with a rate above 0:

| Saved state | Tier | Fleet after the tier | Sampler |
| --- | --- | --- | --- |
| Ordinary save, with the members | Physical or logical | Ordinary, operations on | The saved stream and `nextTick` continue. The start call finds a live sampler and does nothing. |
| Ordinary save, without the members | None | None | Refused before either tier (section 4.3). |
| Demo save | Physical | Demo, operations off | None. A later reset starts one from tick 0. |
| Demo save | Logical | Ordinary, operations on | The start call starts one. This is the only path that starts a sampler in a restore. |

A demo save has no sampler members, because `CheckIncidentPolicy` refuses them, and its demo fleet is gone after the logical tier (`internal/sim/state_logical.go:32`).
The sampler for that fleet is as follows:

| Item | Choice | Reason |
| --- | --- | --- |
| Seed | `loaded.config.Demand.Seed`, the demand seed of the saved project (`internal/session/persist.go:577` keeps it also with a project file). | The same rule as a reset of that session, and the file alone gives it. |
| Scheduling tick | The restored tick, `simulation.Tick()`, which the logical tier keeps (`internal/sim/state_physical.go:328`). | `nextTick` is then above the saved tick, so it passes the range check of the next restore. |
| First value | The first uniform draw of the fresh stream gives `nextTick = tick + interval`. | The stream is a fresh start, offset by the tick. Two restores of one file give the same stream. |
| Counters | Zero, because a demo save has none. | The demo fleet has its operations off. |
| A later `projectDemand` from the project file | Does not change the stream. | The rule of the `demand` command in section 5.1.4. |

Sequence, restore, step, save, restore, each step pinned by a test:

| Step | State |
| --- | --- |
| 1. Logical restore of a demo save at tick T | Ordinary fleet, tick T, both samplers live, `nextTick` above T, no records. |
| 2. Step | Events come due by the stream. Saved and frame counters move. |
| 3. Save | Not a demo save: `state.Demo` is absent and the pods match the fleet. `random` and `nextTick` are present. `simulation.faults` is present with no record. |
| 4. Physical or logical restore | Rows 5 to 9 pass, the tier installs the stream, and the start call does nothing. |
| 5. Step | The same events as a run with no second restore, within what the tier keeps. |

Each other path reads the seed from `s.project.Demand.Seed`, so the samplers follow the project and not the live demand stream.

#### 5.1.4 Determinism through session events

| Event | Sampler |
| --- | --- |
| Pause | No tick, so no draw. A command never draws. |
| `fault`, `clearFault`, `emergency` command | No draw. A later event can skip or pick another pod, because the state differs. The draw count stays. |
| `demand` command with a new seed | The sampler keeps its stream. The next reset or project apply uses the new seed. |
| Demo | Off. It stays off after the demo until a reset, by the rule of the marker removal contract. |
| Project apply, same project | No change. |
| Checkpoint and rewind | `Clone` copies the value members, so a rewind replays the same draws. A new generation changes only the record IDs. |
| Logical restore | Records end and counters stay, as today. |
| Save moves aside | New session, new samplers from tick 0. |

Determinism holds on one architecture.
`math.Log` has an assembly form on `amd64` and `s390x` only (`src/math/log_asm.go:5` of Go 1.27.3), and the images also target `linux/arm64` (`docs/operations.md:76`), so `ceil` can give another tick.
A saved `nextTick` stays exact, and the next interval can differ.
I found no document that claims identity across architectures, and nobody measured it (decision 4).

#### 5.1.5 A run with both rates at 0

The comparison is a stage 7 build against the commit before patch 1, with one project, one seed, and one command list.
A rate of 0 means an absent member, `{}`, `0`, or `-0`.
The command list can hold `fault`, `clearFault`, and `emergency`.

| Item | Result |
| --- | --- |
| Simulation state | Same at every tick. No PCG exists, no draw happens, and `Step` has one more branch in each stage. |
| New counters | Stay 0 and are omitted, also after a manual fault, clear, or emergency, by the counter gate below. |
| Saved bytes | Same, apart from exception 1. `simulation.faults` stays omitted when no record and no counter exists. |
| Stream bytes | Same in the plain stream golden files, apart from exception 1 in the hello. |
| Command digests | Same baseline. |
| `cmd/compare` output | Same SHA-256. |
| A project with `"perHour": 0` | Same as no member in the simulation part. The project member keeps its own bytes, as today. |

Counter gate: `clearedTicks`, `delayedRiderTicks`, `endedTicks`, and `maxTicks` move only while the sampler of their kind is live.
`sampled` and `skipped` move only in a sampler.
With a live sampler, a manual record counts as a sampled one does, so this needs no origin bit on a record.
The cost is that a project with no rate shows no service-effect numbers after a manual fault.
Decision 14 asks for acceptance.

Named exceptions:

| # | Exception | Condition |
| ---: | --- | --- |
| 1 | The version member of a save, the version of the stream hello, and the media type of HTTP state. | Only for a version that the rule of section 4.1 moves. |
| 2 | A rate-0 project within 44 bytes of `project.MaxFileBytes` (32 MiB) is refused with the existing size error. | Decision 15, answer a. Answer b removes this exception. |
| 3 | `state_v10_members.txt` lists the new paths. No save of a rate-0 run writes them. | Always. |

The 44 bytes: `Validate` measures each project with `widestFaults` and `widestEmergencies` (`internal/project/config.go:430`).
With rates to 60, the widest rate is `1.0000000000000002e-06`, 24 bytes in place of 2 for `-0` (`internal/project/faults.go:61`), in two groups.
The consent contract avoided a widest change because its member is outside the substituted structs (`docs/contracts/generated-order-consent-contract-proposal.md:68`), and `perHour` is inside them.
Decision 15 keeps the unconditional widest settings, so the widest rate takes effect for each project.
The rejected answer kept `-0` for a rate of 0 and measured a rate above 0 at its own bytes, which amends suspension 12.1, emergency `:947`, and the marker removal table (`docs/contracts/marker-removal-contract-proposal.md:181`).

#### 5.1.6 Invariants

| # | Rule |
| --- | --- |
| F13 | A fault sampler is live only with faults on and a rate above 0. At each boundary, `nextTick == sim.MaxCounter || nextTick > tick`. `sampled` is at least `skipped`. |
| E9 | The same for the emergency sampler, with emergencies on. |

`checkFaults` and `checkEmergencies` check them, so each `observe` covers them.

#### 5.1.7 Failure modes

| Failure | Effect | Handling |
| --- | --- | --- |
| A rate on a busy network is mostly skipped. | The real rate is below `perHour`. | `sampled` and `skipped` show it. |
| A high rate with long durations faults most of the fleet. | Service stops, and safety holds. Up to 60 faults an hour at 86,400 s gives 1,440 expected active faults, more than the fleet limit. | Allowed. The user asked for it, and each further pod fault event is a skip when no pod is free. |
| The reroute pass and the station choice run in `Step`, also with no command. | A slow tick. `liveBatch` holds `mu` for each whole `s.step()`, and its 4 ms check runs only between steps (`internal/session/speed.go:180`). So commands and publication wait for that tick. | The sampled-tick latency gate of section 5.1.9. |
| A sampled start returns an error that no refusal lists. | Counted as a skip with no write, because each start checks before its first write. | Test: a refused start changes no state. |
| A save with a rate and a sampler that the project does not match. | `invalid_state` by the checks of section 4.3, or `project_changed` when the project file differs. | Existing move-aside path. |
| A tier does not install the saved stream. | The idempotent start hides the loss by a fresh stream. | The restore trace test of section 5.1.8 fails. |
| A tool runs a project with a rate and ignores it. | Wrong results with no message. | Section 5.3 refusal. |

#### 5.1.8 Test plan

Session-level tests with a rate need the activation patch, because `Validate` refuses a rate before it.
Earlier patches test through `sim` entries.

| Test | Proves | Mutation that it must catch |
| --- | --- | --- |
| Golden first 8 raw draws for seed 1, each sampler (`TestIncidentSeedsGolden`), and golden first 8 uniform draws for the PCG arguments 1 and 2 (`TestIncidentUniformGolden`) | The seed rule, the byte orders, and the uniform form. | Swap a byte order. Change the uniform form. `rand.Rand.Float64` has the same form today, so a change to `rand.Rand` gives the same draws. |
| Event trace fixture, each sampler, with 20 events: for each draw its role and value, then the target, segment, end tick, outcome, and next tick | Draw roles and event meaning, which raw draws cannot show. | Draw the next interval before the event draws. Swap position and duration. Count over all pods in the fault sampler. Round a duration down. |
| Interval table: `u = 0`, the largest `u`, a rate of 60, and a rate of `1e-300` | `max(1, ...)` and the terminal value. | Drop `max(1, ...)`. Remove the saturation. |
| Terminal run: set the tick to `MaxCounter-1` with `nextTick` at `MaxCounter`, then step, save, restore, and check no event and valid F13 and E9 | One terminal rule in all three places. | Use `tick >= nextTick` alone. Remove the exemption from F13 or from the range check. |
| Boundary fixture: in one tick, a pod unloads, a fault end comes due, and both samplers come due | The sampler sees the state after the unloading loop (`internal/sim/simulation.go:833`) and before the clears (`internal/sim/faults.go:454`), and the emergency sampler runs before the records. Target and outcome differ for each wrong place. | Move the fault sampler after the clears. Move a sampler before the unloading loop. Move the emergency sampler after the records. |
| Draw count for each outcome: accepted, each refusal, and `n == 0` | Later events do not depend on outcomes. | Skip the unused draws on a refused event. |
| Vehicle order: placements in reverse ID order, then a new fleet, a reset, a clone, and both restore tiers | G11. | Remove the sort in `newFleet`. Remove the order check of a saved pod. |
| Demand pair sequence with the samplers on and off | Streams are separate. `nextPair` draws before `offer` looks at the state (`internal/session/demand.go`), so the sequence is comparable. | Draw an event from the demand PCG. |
| One rate at a time, each way | Each sampler uses its own stream. | Use one stream for both samplers. |
| Identity: rate absent, `{}`, `0`, and `-0`, on Busy and Rail Hub, with no command | Section 5.1.5. | Add a member at rate 0. |
| Command-driven parity: the same four forms with a scripted `fault`, `clearFault`, debris, and `emergency`, against bytes pinned from the commit before patch 1 | Manual commands write no new bytes at rate 0. | Remove the counter gate of `clearedTicks` or `delayedRiderTicks`. The gates of `endedTicks` and `maxTicks` need an ended emergency, which the script does not have, so `TestManualEmergencyEndCounters` covers them. |
| Size boundary: a rate-0 project at the cap, and 44 bytes below it, with decision 15's answer | Exception 2 is exact, and each accepted project encodes within the cap. | Keep `-0` as the widest rate with no measure of the real rate. |
| Restore trace, physically unchanged fixture: every pod at its berth with no motion, no queue, and a state that the physical tier keeps as it is. Compare the next 20 event traces with the run that did not stop. | The stream and each restored setting. Draws and due ticks alone cannot show a lost setting. | Lose `DebrisShare`, `DebrisMeters`, or `Duration` on restore. Drop `random` from the export. Seed in `SetFaults`. Restart the stream in a tier. |
| Save at a random tick on a live preset, restore, and compare the next 100 due ticks and draw values | The save carries the whole stream in a real state. The outcomes can differ, so this test does not compare them. | Drop `nextTick` from the export. |
| Demo lifecycle: the sequence of section 5.1.3, with `logicalOnly` forced by the restore attempts count, two restores of one file, and each rate alone | G16. | Remove the start call. Use the live demand seed. Schedule from tick 0. |
| Demo, physical tier, with a rate | No sampler, no member, the next save restores, and a reset starts one. | Start a sampler with the switch off. |
| Express project with both rates: run, save, and restore | The sampler works with the Express order contract, which has no byte proof. | None. It is a smoke test. |
| Rewind and reset replay | Section 5.1.4. | Hold the PCG behind a pointer in `Clone`. |
| Logical tier with a sampler | Stream kept, records dropped, counters kept. | Reset the stream in the logical tier. |
| Refusal tables, with inputs that have more than one error, and the explicit-zero table | Section 4.3. | Remove each new line. Read `nextTick` as absent at 0. |
| Frame members, and a counter round trip with different values | Section 4.2. | Add `random` to `FaultsView`. Swap `sampled` and `skipped` in the mapping. |
| One station search for a sampled emergency in its start tick | G15. | Search in each advance call. |

Status 2026-11-05: by the maintainer decision of 2026-11-04, patch 15 removed `internal/session/incident_identity_test.go`, the test of the Identity and Command-driven parity rows.
The saved-state golden files and the parity tests of `internal/session` stay.

#### 5.1.9 Qualification

The gates of suspension section 17 and emergency section 15 were written to justify enabling faults by default.
I found no record that they ran, and the marker removal enabled the operations without them.
A sampler puts the code on a path with no user command, and a preset with a rate is the default choice that both contracts leave to stage 7, so option B runs them.
Each approved gate below keeps its measurement and its pass rule.
A number in the Amendment column shows the one change that needs an answer.

| Gate | Source | Measurement | Pass | Amendment |
| --- | --- | --- | --- | --- |
| Off parity | Both | The off-state gates of suspension 14 and emergency 12, on every preset. | Byte identity, digest identity, and identical states at every 10 s of simulated time, with the exceptions of section 5.1.5. | |
| Marker-only parity | Both | The same runs with the marker and no incident. | Identical states apart from the marker. | A1 |
| Safety | Suspension | Soak on LondonCentral and the rail-hub preset: random pod faults and debris started and cleared through the commands, at about 10 for each simulated hour, for 24 simulated hours for each seed, over at least 20 seeds. | `CheckContract` holds after every tick and every command. No owner conflict. No pod passes its fault cap or enters a fault footprint. The transition helper holds. | A2 |
| Safety | Emergency | The same presets and size: emergencies on random occupied pods through the command, at about 10 an hour, with faults on and off. The approved header removes the coupling arms. | `CheckContract` holds after every tick and command. No owner conflict. | A2 |
| Contract | Suspension | The same soak, with F1 to F12, W1 to W5, X1 to X2, and the new F13 after each tick. | No violation. No pod is withdrawn without `faultHold`. | |
| Contract | Emergency | The same soak, with E1 to E8, F11, W1 to W5, and the new E9 after each tick. | No violation. | |
| Conservation | Both | The same soaks. | `RequestID = Completed + Interrupted + queued + aboard` at every tick. | |
| Rail | Suspension | The same soak. | Each interrupted order with a `pending` rail record ends `unserved` with reason `interrupted`. A `missed` record stays `missed`. An order with no record gets none. | |
| Progress after the clear | Suspension | At hour 24 the test entry sets `nextTick` of each sampler to `MaxCounter`, so no new event starts while the samplers stay live and their counters keep moving, and the driver clears each active fault by command. The 30 minutes start at the last clear, with demand running. | Within 30 simulated minutes, no pod has `faultHold`, no pod has an incident wait, and the waiting queue returns to the size range of a run without faults. A miss lists each remaining wait with its reason and blocker. | |
| Progress | Emergency | The same soak. | Each record ends, and each record of an ordinary pod ends within 10 simulated minutes. A miss lists each record with its phase and wait reason. | |
| Choice | Emergency | The estimate of the chosen station against the actual time to the unload. | Reported, not gated. | |
| Ordinary traffic | Emergency | Admission wait of ordinary intents against a run without emergencies. | Reported, not gated. | |
| Persistence | Both | Saves at random command boundaries and tick ends, restored in the physical tier, and rewinds to random checkpoints. | Every save restores and passes `CheckContract`, or fails the physical tier only by the demotion rule. Every rewind replays exactly. | |
| Performance | Suspension | Step time against a run without faults, with 64 debris faults and 300 pod faults as the worst case. | Median within 10 percent, and no tick above twice the slowest tick of the run without faults. | A3 |
| Performance | Emergency | Step time against a run without emergencies, with `MaxEmergencies` records in the cadence search. | Median within 10 percent, and no tick above twice the slowest tick of the run without emergencies. | A3 |
| Worst choice tick, presets | Emergency | As approved, `TestEmergencyChoiceLatency`. | At most 50 ms on the CI runner class. | |
| Worst choice tick, no candidate | Emergency | As approved, in `mise run qualify`. | Reported against 50 ms, never gated (P23). | |
| Worst choice tick, delayed routes | Emergency | As approved. | At most 50 ms on the CI runner class, cold and warm. | |
| Formats | Both | The composed fixtures of suspension 16.4 and emergency 14.4. | Every shape under its cap. | A4 |
| Web | Both | `mise run test:web`, and the marker checks of suspension 16.3 and emergency 14.3. | Passes. | A1 |
| Product | Both | The choices of suspension 20.2 and emergency 18.2. | Decided. The new choices are in section 8. | |

Gates that stage 7 adds:

| Gate | Measurement | Pass |
| --- | --- | --- |
| Sampled-tick latency | A whole `Step`, because a step holds the session lock. A fault event and an emergency event come due in one tick, so it includes the fault start, the reroute pass, the emergency start, and the station search. LondonCentral and the rail-hub preset, cold and warm. | At most 50 ms on the CI runner class, the limit of `internal/sim/emergency_latency_test.go:17`. |
| Sampled-tick latency, project limits | The same at the fixture of the no-candidate gate. | Reported, as P23 does. |
| Sampler soak | The sampler at 10 faults and 10 emergencies an hour. With A2, it runs in the approved soak. | The same pass rules as the soak. |
| CI soak | 1 seed for 1 simulated hour, with the sampler only. | The same pass rules, in `mise run qualify`. |
| Fleet-limit report | Step time at 64 debris and 1,000 pod faults. | Reported, not gated. |

Soak size with A2, by arithmetic, with 2 presets and 20 seeds: 40 runs of commands and sampler, 40 with emergencies only and faults off, 40 with platoons on, which decision 3 needs, and 80 reference runs with no incident.
This is 200 runs of 24.5 simulated hours, 4,900 simulated hours, or about 1.06e9 ticks at 60 Hz and about 5.3e8 ticks at 30 Hz, with every-tick checks on 120 of the runs.
I have no measured tick rate for this, so a timing run of 1 seed for 1 hour on each arm comes first, and its result goes to the maintainer.
Each run has a memory cap, by the project's rule for test jobs.

Status 2026-11-05: `docs/studies/incident-soak.md` holds the timing run.
The soak has not run, the maintainer has not chosen its size, and the Performance row for suspension is open.
This status marks no gate as passed.

Amendment A6, approved by the maintainer on 2026-11-05: the Safety, Contract, Conservation, Rail, Progress, and Persistence rows run for 5 seeds of 6 simulated hours, then the clear of 30 simulated minutes, on both presets, with a limit of 2 hours of wall time.
This size replaces 24 simulated hours and at least 20 seeds.
A run that the limit stops counts only its completed seeds.

Amendment A7, approved by the maintainer on 2026-11-05: on the Performance row for suspension, the median rule becomes a median of at most 50 µs at 64 debris and 300 pod faults.
One step at 60x has 278 µs on one core, and the reference step does almost no work, so a ratio does not measure the cost.
The row at 1,000 pod faults and a row with a full queue, against 278 µs, are reports.
The rule for the slowest tick stays.
A quiet-host run must confirm the margin before the row counts as passed.

Amendment A8, approved by the maintainer on 2026-11-05 for rule A: it changes the Progress row for emergencies, and it changes no other row.
The soak of A6 has 50 of 335 late records on `rail-hub` `incidents`, 39 of 336 on `rail-hub` `platoons`, and 1 of 470 on `london-central` `incidents`, and 0 of 275 on `rail-hub` `emergencies`.
`docs/studies/incident-soak.md`, section "Attribution of the late emergencies", holds the attribution: faults take 86 percent of the time of the late records in both `rail-hub` arms.

Rule A, the recommendation: each record of an ordinary pod ends with at most 10 simulated minutes of time not blocked by a fault, and each record ends.
Time is blocked by a fault when the pod has an active fault, when it reports `Blocked by incident` or `No forward route`, or when its `BlockedBy` chain reaches such a pod or a fault ID.
Time is also blocked by a fault when the chain ends at an idle pod whose order cannot board only because of the active faults, or at a stopped pod whose berth choice fails only because of the active faults.
With faults off, this time is the whole record, so the rule of the approved row applies without change.
Late records under this rule, in the rerun of those records at `63a4868`: 0 of 50, longest 7.2 min, 0 of 39, longest 4.9 min, and 0 of 1, 2.0 min.
Without the last two causes, 11 of 50 and 7 of 39 stay late.

Rule B, the alternative: the 10 minutes apply only with faults off, and with faults on the row reports the lengths, and each record still ends.
Late records under this rule: 0 of 275, longest 3.6 min, on `rail-hub`.
It has no `london-central` reading, because the limit stopped each run of that arm.
It needs a change of one condition, and it stops gating a long wait with faults on, including one that no fault causes.
The maintainer did not choose rule B.

Rule A keeps a gate with faults on, and rule B does not, so the draft recommended rule A.
The cost of rule A is a classifier in the test: if it names a fault in error, it hides a real wait.

Test change for rule A: `watchEmergencies` in `internal/session/incident_soak_test.go` adds the unblocked ticks of each record at each reading, and `noteLateEmergency` runs when that sum passes `soakEmergencyTime`, in place of the test of the record length.
The miss keeps the phase and the wait reason, and it adds the end of the chain.
The idle pod needs a minimal test entry from `internal/sim` that repeats the route search with the blocked set empty, as the probe of the study did.
The stopped pod needs no entry: since `cafe57b`, a pod whose berth choice fails while the blocked set is not empty reports `No forward route` or `Blocked by incident` at each tick, so the test reads the report.
The report does not show that the fault caused the failed search, and `docs/studies/incident-soak.md`, section "Run of rule A", measures this.
`DispatchReason` is not enough, because `Waiting for destination access` is the same text for each failed leg.
The study made one probe of the stopped pod, on `i2.27`, so a run of the changed test must confirm these numbers.
`TestIncidentSoakCI` and `TestIncidentSoakDriver` use the same driver.
This amendment marks no row as passed: the soak at 30 Hz decides the Progress row.

Amendment A9, approved by the maintainer on 2026-11-05 as decisions 1 and 4 of [the tick rate proposal](tick-rate-contract-proposal.md): the sampler interval, the duration in ticks, and off parity now use `TicksPerSecond` and seconds in place of 60 Hz literals.
Each rule has the same meaning at 60 Hz, and the soak size in simulated hours does not change.

Amendment A10, approved by the maintainer on 2026-11-05: the rows Safety, Contract, and Conservation keep their pass rules, and a routine soak checks them at a stride.
The cheap checks run at each tick: `checkFaults`, `checkEmergencies`, the transition rules, and `SafetyObservation().Check()`.
The full check is `ExportState`, `checkContract`, and the order balance, from one export, and it runs at each simulated second and at each event.
An event is a command of the driver, or a tick where a fault or an emergency starts or ends, a sampler event comes due, or an evacuation or a reroute occurs, as the incident counters show.
The risk accepted is a violation that lasts under 1 s between two full checks.
A soak with the full check at each tick is required before a release tag and after a change to dispatch, routing, or the fault and emergency state machines.
The size of A6 becomes the branch shape for routine soaks.
On `rail-hub`, each arm has one warm save at 30 simulated minutes, and then 10 branches of 1.5 simulated hours and the clear of 30 simulated minutes.
On `london-central`, the `incidents` and `emergencies` arms have 3 branches of 1 simulated hour and the clear.
Each branch restores the encoded warm state in a new session through the save path, and it reseeds the driver, the fault and emergency samplers, and the demand, by the maintainer decision of 2026-11-05.
Rewinds run each 10 simulated minutes in place of each hour.
A reference arm has no driver event, so it runs one branch of the same length from its own warm save, and each branch of its group uses that queue range.
Each branch reports the Progress rows by itself, and a run that the limit stops counts only its completed branches.
The 5 seeds of 6 simulated hours of A6 stay for the same triggers as the soak with the full check at each tick.
Soak jobs run one soak at a time under a lock, with the default `GOGC`, by the maintainer decision of 2026-11-05 after the trial in `docs/studies/incident-soak.md`.
`TestSampledTickNoCandidateLatency` moves from `mise run qualify` to `mise run qualify:limits`, because it only reports, and no gate changes.
This amendment changes no other rule and marks no row as passed.

Amendment A11, approved by the maintainer on 2026-11-06: on the Performance row for emergencies, in `TestIncidentWorstCaseStepTime`, each preset has 4 runs, and each run compares its medians with its own reference.
The median of the 4 differences must be at most 10 percent of the median reference, or 1 µs if that is larger.
The medians are about 2.5 µs on `rail-hub`, so a ratio of one run of 300 ticks measures noise.
The rule was chosen after the readings of 2026-11-06, which missed the rule of 10 percent, so those readings do not confirm it.
A later quiet-host run with `PODSIM_SOAK_GATE=1` must confirm it before the row counts as passed.
The rule for the slowest tick stays in each run, and the timed runs of A3 keep the rule of 10 percent.
Status 2026-11-06, by the maintainer: on this row, no tick may pass twice the slowest reference tick, or 1 ms if that is larger.
This floor is 3 percent of a tick at 30 Hz.
A gated run at `8ffb20b6` passed the median rule and missed the earlier rule for the slowest tick on `rail-hub` in 2 of 24 runs, so a later quiet-host run must confirm the floor, and a real rare tick under 1 ms now passes.

Amendment A12, approved by the maintainer on 2026-11-06: the soak with the full check at each tick, and the 5 seeds of 6 simulated hours of A6, are required only before a release tag.
This replaces the triggers of A10.
After a change to dispatch, routing, or the fault and emergency state machines, the routine soak in the branch shape and `TestIncidentSoakCI`, which runs the full check at each tick, are required.
No finding in `docs/studies/incident-soak.md` needed a long run, but that study did not record the simulated time of each first miss, so this is a judgment and not a measurement.

Amendments, each for a separate answer in section 8:

| # | Approved text | Change | Reason |
| ---: | --- | --- | --- |
| A1 | Marker-only parity rows, and the marker sentence of the Web rows | Remove them. The Web rows keep `mise run test:web`. | The markers no longer exist. The marker removal contract did not say what happens to these rows. |
| A2 | The soak starts incidents "through the commands" | One soak has the command driver at the approved rate and the sampler together, in place of two soaks. | The commands stay at the approved rate, and the sampler adds to the load. Two soaks double the run time and find the same classes of error. |
| A3 | Performance measures "the soak" | Timed runs with every-tick checks off, on a quiet machine, 3 seeds on each preset, against 3 reference runs. | The checks distort a step time, and `AGENTS.md` requires a quiet machine for latency. Timing every seed adds 80 runs of 24 hours, one after another, and adds little to a median. The maximum-tick rule stays. |
| A4 | Composed fixtures for every shape | `TestPlainStateFileWorstCaseSize` and `TestPlainStreamMaximumEncoding` take the place of the fixtures. Express stays unproved. | The fixtures no longer exist, and `AGENTS.md` records that Express has no byte proof. Stage 7 adds under 400 bytes to an Express file with unknown headroom. |
| A5 | Size of the soak, for option A only | One short soak in place of the approved size. | Option A exists to cost less. Options B and C do not need this. |

#### 5.1.10 Observability

`sampled` and `skipped` are in frames and saves, and option B adds them to `session.Metrics` and OpenTelemetry.
The simulation has no logger, so a sampled event has no log line.
`started` counts sampled and manual records, and `sampled - skipped` is the number of sampler starts.

### 5.2 Policy

No document defines the full policy (G2).
The transitions contract only says that stages 2, 3, and 7 own "the policies that decide when to call these operations" (`docs/contracts/incident-service-transitions-contract-proposal.md:66`).
These are the candidates that the approved contracts leave open:

| # | Candidate | Source | What it needs | Recommendation |
| --- | --- | --- | --- | --- |
| a | Sampler rules: targets, skips, limits, and that a sampled fault always has an end | 12.2, 10.2, 12.1 | Nothing beyond section 5.1 | In |
| b | Automatic emergency after a wait limit on an occupied pod | P11 alternative, emergency 18.2 | A new setting, a new digest value, and a rule for a rider who did not ask. It does not help a pod with no forward path, because the station choice then has no candidate. | Out, until a soak shows long occupied waits |
| c | Refuge travel and resume | P3, P4, emergency 1.2 | A new hold bit, amendments to F11 and W5, and the removal of `saved pod %s has a refuge` from the marker removal contract. | Out |
| d | Debris on a berth or berth node | S13 | A buffer claim rule | Out |
| e | Debris that waits for a claim to end | P1 alternative | A pending ownership rule | Out, until `skipped` shows that debris is mostly refused |
| f | Release of unused grants of a stopped pod | P9, stage 5 or later | A certified transition. Stage 5 is dropped. | Out |

`485f2f5:PROJECT_BRIEF.md:718` asks how routes, pickups, and berth reservations use a blocked lane, and suspension sections 8 and 9 answered this for pods.
Only "how the metrics show the effect on service" remains, and section 5.5 answers it.

Policy in option B is row a, with no refusal or format member beyond section 4, and its tests are in section 5.1.8.
Row b, c, d, or e needs its own table if the maintainer chooses it.

A pod behind a fault with no forward path keeps waiting and reporting, as P11 approved.
With a sampled fault, this wait ends with the fault, at 86,400 s at most.
The new `delayedRiderTicks` counter and the soak show how much this matters, and the maintainer can then revisit row b.

Failure mode: option C adds holds and purposes, and the marker removal contract refuses any hold that has no owner to release it, so each one needs its own release proof.

### 5.3 Scenarios and tools

#### 5.3.1 `cmd/scenario`

New flags, read with the existing `options.set` pattern (`cmd/scenario/main.go:67`):

| Flag | Writes |
| --- | --- |
| `-fault-rate` | `faults.perHour` |
| `-debris-share` | `faults.debrisShare` |
| `-debris-meters` | `faults.debrisMeters` |
| `-fault-duration` | `faults.duration`, as `fixed:600`, `uniform:60:600`, or `exponential:60:3600:600` |
| `-evacuation-seconds` | `faults.evacuationSeconds` |
| `-emergency-rate` | `emergencies.perHour` |

A flag that is not set writes nothing, so each preset keeps its bytes.
The output passes `project.Validate`, which gives the refusals of section 4.3.
The flag parser adds these, in this order:

| Order | Check | Text |
| ---: | --- | --- |
| 1 | `-fault-duration` has an unknown form | `-fault-duration must be fixed:S, uniform:MIN:MAX, or exponential:MIN:MAX:MEAN` |
| 2 | `-debris-share`, `-debris-meters`, or `-fault-duration` without `-fault-rate` | `-debris-share, -debris-meters, and -fault-duration need -fault-rate` |

Option B also adds one named preset, `rail-hub-incidents`: `rail-hub` plus the values of decision 7.
No existing preset changes, because each contract says that the default rate stays 0 until the maintainer chooses one.

#### 5.3.2 Tools

| Tool | Rule | Text |
| --- | --- | --- |
| `internal/parkride` | Refuse a rate above 0, in `validateFoundationConfig` after `errFoundationProject` (`internal/parkride/foundation.go:46`). The car ledger has no interruption path: no file in `internal/parkride` calls `DrainInterruptions`. | `car runs do not support a fault or emergency rate` |
| `cmd/compare`, option A | Refuse a rate above 0, right after `project.Validate` (`cmd/compare/schedule.go:113`). | `comparisons do not support a fault or emergency rate` |
| `cmd/compare`, option B | Run it, by the table below. | None |
| `cmd/projectcheck` | No change. It does not step a simulation. | None |

`cmd/compare` in option B:

| Part | Today | Stage 7 |
| --- | --- | --- |
| Scenario | `loadScenario` keeps neither setting (`cmd/compare/schedule.go:85`, struct at `:33`). | `scenario` carries `faults` and `emergencies`. |
| Arm setup | No `ConfigureExperiments` call, and the operations stay off. | An arm with a rate turns both switches on and starts the samplers, with `Demand.Seed` set to `uint64(input.seed)`, as `cmd/compare/run.go:262` does. Arms with one seed get one event schedule. An arm with no rate keeps its switches off. |
| Drains | `stepService` drains only with `arm.connections` (`cmd/compare/run.go:440`). The other `Step` site never drains (`:404`). | Each `Step` path drains and adds to a delivered count, so `undelivered` stays bounded. With rail connections, the drain stays before `Advance`. |
| Early stop | `drainedAfterArrivals` needs `Completed == Submitted` (`:551`), so one interruption stops an early finish forever. | `Completed + Interrupted == Submitted`. |
| Report | `drained` (`:582`), midpoint and arrival backlog (`:589`, `:590`), `Remaining` (`:613`), `BacklogAtArrivalEnd` (`:614`), and `peakOutstanding` (`:723`) treat an interrupted order as unfinished. | Each one uses `Submitted - Completed - Interrupted`. `Served` and the throughput figures stay on `Completed`, so an interrupted order is not served. |
| Columns | None for incidents. | `interrupted`, `fault_started`, `fault_skipped`, `emergency_started`, and `emergency_skipped`, only when the project has a rate. |

The terminal formula needs no rate gate.
`Interrupted` is always 0 in a run with no rate, because such a run has the operations off, so the output keeps its SHA-256.
Only the new columns need the gate.

A comparison is the only sound way to measure the delay that incidents cause, because it needs a run without them.
The user runs one project with a rate and one with none, with one seed list, so this adds no arm dimension.

The marker removal contract's table of `operations` callers keeps its values, and a tool with a rate is the one new path.

Failure mode: a preset with a rate reaches a tool test that expects no interruption, so the preset tests of `internal/scenarios/preset_options_test.go` must name the new preset.

Test plan:

| Test | Mutation that it catches |
| --- | --- |
| Each flag writes its member, and no flag writes no member | Write a default for an unset flag |
| Each refusal, with both errors present | Reverse the order |
| `parkride` refuses each of the two rates | Check the fault rate only |
| `compare` refuses, or runs, each of the two rates | Check the fault rate only |
| `compare` with no rate has the pinned SHA-256 | Add a column with no rate |
| `compare` scenario load keeps both settings, with a project file | Drop a member in `loadScenario` |
| `compare` with an interruption in the run stops early, and the row shows it as interrupted and not as remaining | Keep `Completed == Submitted` at each site of the table, one at a time |
| Delivery, both `Step` paths: the delivered count equals `Interrupted`, and a further `DrainInterruptions` is empty. `RequestID` conservation alone cannot show this, because `interruptRider` updates the counters at once (`internal/sim/interrupted.go:26`). | Remove each drain |
| Rail: an interruption on the departure tick of its connection ends `unserved` with reason `interrupted`, not `missed` | Move the drain after `Advance`. Remove the drain of `stepService`. |
| Two runs of one arm with a rate give the same report | Share a sampler between arms |

Observability: the output of `cmd/compare`, and the project member of `cmd/scenario`.

### 5.4 Controls

| Control | Option A | Option B |
| --- | --- | --- |
| Place debris | A **Debris** button arms one click. A click on the map picks the nearest lane. | The same, in the **Incidents** panel. |
| Clear debris | Click the debris mark, then **Clear debris**. | A **Clear** button on each row of the **Incidents** panel, for debris and for pod faults. |

#### 5.4.1 Place debris

| Step | Rule |
| --- | --- |
| Show | Only with `operationsOff` false, as the other buttons. |
| Arm | One press arms, and a second press disarms. While armed, a map click selects no pod and no station. |
| Pick | `pickLane(point, lanes, radius)` is a pure function. The radius is 12 design units. The nearest lane within the radius wins, and the first lane in network order wins a tie. |
| Segment | 2 m, centered on the click and clamped into the lane. `sim.Network.Length` gives the meters. |
| Command | `fault` with `laneID`, `fromMeters`, `toMeters`, and no duration, so the debris lasts until a clear. |
| Refusal | The existing message line shows the server error, for example `debris meets a reserved resource`. The view checks nothing itself, as P7 and `internal/view/fault.go:35` say. |

The length is a constant, because the topology does not carry `faults`.
The protocol keeps lengths up to 50 m and durations for scripted use.

#### 5.4.2 Incidents panel, option B

A third panel beside **Orders** and **Demand** (`internal/view/game.go:472`), in the same row, with paging for rows that do not fit.

| Row | Content |
| --- | --- |
| Summary | Active faults, active emergencies, `sampled`, `skipped`, interrupted orders, and the numbers of section 5.5. |
| Debris | Lane ID, and **Clear**. |
| Pod fault | Pod, phase, the end in simulated seconds or "no end", and **Clear**. |
| Emergency | Pod and phase, with no button: P22 has no cancel. |

**Clear** sends `clearFault` with the fault ID.
The panel hides with `operationsOff`.
A click uses the stored button of the last frame, as `frameControls` does for the inspector (`internal/view/fault.go:77`).

#### 5.4.3 Editor form, option B

A **Faults and emergencies** section beside the demand form (`web/editor.html:241`, `web/editor.js:3147`).
Its inputs bind through `bindScalarInput` (`web/editor.js:3628`), and the native model validates with the same refusals.
An untouched member stays absent, so a loaded project keeps its bytes (`internal/session` compares canonical JSON).
Clearing an input removes the member, which keeps its default.
A change is a project change, so the fleet is rebuilt on apply, as `docs/operations.md` says today.

Failure modes:

| Failure | Handling |
| --- | --- |
| A click arms and the demo starts. | The button hides, and the armed state ends with `operationsOff`. |
| A debris row is gone at the click. | The server replies `unknown fault`, and the message line shows it. |
| The lane under the click is a thin lane of a collapsed station. | The map draws such a lane, thin and dim, under the station marker, and it is not a target. A lane that continues a line through the station stays a target. A lane out of the viewport is not a target. |
| The form writes `0` for an untouched member. | Test: load, change nothing, and compare the project JSON. |

Test plan, with no `Draw` loop on an offscreen image (`AGENTS.md`):

| Test | Mutation that it catches |
| --- | --- |
| `pickLane` table: nearest, tie, out of reach | Reverse the tie rule |
| Segment table: middle, lane start, lane end, lane shorter than 2 m | Remove the clamp |
| Armed click sends `fault` with no duration, and disarms | Keep armed after the click |
| A stale row sends the ID of the last frame | Read the current frame |
| Buttons hide with `operationsOff` | Remove the hide |
| Editor round trip, and `mise run test:web` | Write a default on load |

Observability: the existing command errors and session log.
No format, digest, or version effect beyond section 4.

### 5.5 Metrics

P8 and P20 defer "the full set" and do not define it.
They name blocked seconds, delayed rider seconds, time from start to unload, and the delay of other riders as examples.
The `faults` group of a delta repeats `active` whenever a member of the group changes, so each new counter changes at an event, or only in a tick in which `faultWaitTicks` also changes.
Each counter except `sampled` and `skipped` has the gate of section 5.1.5.

| Counter | Group | Meaning | When it changes | Option |
| --- | --- | --- | --- | --- |
| `sampled` | Both | Events that came due. | At an event | A, B |
| `skipped` | Both | Events with no start. | At an event | A, B |
| `clearedTicks` | `faults` | The sum of `tick - start` of the faults that ended. | In `removeFault` (`internal/sim/faults.go:416`) | B |
| `delayedRiderTicks` | `faults` | The sum over ticks of the active riders aboard healthy pods with an incident wait. | In the loop of `internal/sim/faults.go:475`, only in a tick in which `faultWaitTicks` also changes | B |
| `endedTicks` | `emergencies` | The sum of `tick - start` of the records that ended. | In `endEmergency` (`internal/sim/emergencies.go:204`) | B, decision 11 |
| `maxTicks` | `emergencies` | The longest record that ended, in ticks. | In `endEmergency` | B |

Each counter stops at `sim.MaxCounter`, and each decoded value has the negative-value check.
A reset clears them, a logical restore keeps them, and a rewind restores them.

The mean record length is `endedTicks / ended`.
`emergencyTicks / ended` is not that mean: its numerator includes active records (`internal/sim/emergencies.go:157`), and a logical restore keeps the ticks of the records that it drops without a count in `ended` (`internal/sim/state_emergencies.go:266`).
A logical restore adds to neither `ended` nor `endedTicks`, so the new mean covers exactly the records that ended.

Not proposed:

- A histogram of the time to unload (decision 11), which needs a new array member and a limit.
- The delay of other riders as a live counter, which needs a run without the incident, so `cmd/compare` gives it.
- A skip count for each reason, until the share of skips shows a need.

`session.Metrics` and OpenTelemetry, option B, have these instruments:

`podsim.incident.fault.active`, `.started`, `.sampled`, `.skipped`, and `.delayed_rider_ticks`, and `podsim.incident.emergency.active`, `.started`, `.sampled`, and `.skipped`.

They are observable gauges, as the existing journey series are (`internal/telemetry/telemetry.go:236`).
They do not change a format or a digest.
`docs/operations.md:559` then changes.

Failure modes: a counter that a loop adds to outside the gate of an active blocked set costs time with no fault.
`delayedRiderTicks` calls `RidersAboard` only for a pod with an incident wait, and a pod has at most 8 parties, or 20 with Express.

Test plan:

| Test | Mutation that it catches |
| --- | --- |
| Each counter on a scripted fault and emergency, with a live sampler | Count at the wrong event |
| Each gated counter stays 0 with no sampler | Remove the gate |
| `endedTicks` mean: active records and a logical restore do not change it | Add the ticks of a dropped record |
| Saturation at `sim.MaxCounter` | Remove the stop |
| Round trip in saves, full frames, HTTP states, and groups | Map a counter to its neighbor |
| No new delta group change while no pod waits | Add a counter that changes in each tick |
| `session.Metrics` reads each value, and telemetry has each instrument | Omit one |

### 5.6 Debris drawing

```go
// debrisMark is the map form of one active debris fault.
type debrisMark struct {
	from, to, center sim.Point
	path             []sim.Point
}

// debrisMarks returns the marks of the active debris, in record order.
func (g *Game) debrisMarks(faults sim.FaultsView, detailed bool) []debrisMark
```

| Item | Rule |
| --- | --- |
| Input | `state.Faults.Active` with kind `debris`. A delta already updates this group. |
| Geometry | `laneGeometry.atMeters` at `fromMeters` and `toMeters`, over the table of `laneMeters`, which holds the distance of each lane point. A record with an unknown lane or a lane of no length has no mark. `StreamAssembler` already checks the lanes. |
| Stroke | `path`: a line between the two points on a straight lane. On a curved lane, also the lane points between them. |
| Mark | A fixed-size ring sprite at the center, because a 2 m segment can be below one pixel at **Fit**. |
| Color | A new `danger` color, so a mark differs from the amber wait ring. |
| Order | After the route and before the platoon lines and the pods (`internal/view/draw_network.go:113`). |
| Cache | Each frame draws it, and it is not in the cached base layer. |
| Footprint | The 12 m blocked cells are not drawn. |
| Hit test | Option A: a click within the pod pick radius of a mark selects it, for **Clear debris**. Option B: none. |

Decision 12 adds a `danger` ring for a faulted pod and an emergency pod, in place of the amber ring.
Today both have only the amber ring of any wait, and the brief asks how the game shows the event (`485f2f5:PROJECT_BRIEF.md:681`).

Failure modes:

| Failure | Handling |
| --- | --- |
| Many records. | 64 debris records at most, and each mark is one sprite and at most 33 lines. |
| A collapsed station hides the lane. | The mark still shows, so a blocked lane is never invisible. |
| A rewind or reset removes a record. | The frame replaces the group, and the marks follow it. |

Test plan: table tests of `debrisMarks` on a straight lane, a curved lane, and an unknown lane, with mutations of the fraction and of the kind filter.
The test does not call `Draw` in a loop, by `AGENTS.md`.

Observability: none beyond the mark.
No format, digest, or version effect.

## 6. The session wire package

The October note says only that a session wire package was queued "before stage 7".
I found no definition in the repository, the commit messages, or the archived handoffs.
This section is my reading of it, and the maintainer should correct it.

It would move the wire types and codecs out of `internal/session` into their own package: about 4,000 lines in `stream*.go`, `express_wire.go`, `express_text.go`, `fault_stream.go`, `emergency_stream.go`, `incident_stream.go`, `protocol.go`, `topology_decode.go`, `state_http.go`, `format_limits.go`, and parts of `compression.go`.
The reason is that `podsim.wasm` links `session` through `view` and `remote`, so it links `net/http`, the session runtime, and persistence code that it does not run.
`internal/wirename` is an existing start.
`view`, `remote`, `statestore`, `telemetry`, and `cmd/serve` import it, and `session.State` alone has 175 uses in `view` and `remote`, tests included.
The cost is one mechanical patch, about 6 to 12 hours by judgment, with a separate proposal, and I did not count the unexported helpers that the session and the codec share.
The benefit is a compile boundary, and perhaps a smaller `podsim.wasm`, which still links `sim` and `project`, and nobody measured the size.

Stage 7 does not need it.
Stage 7 adds counters to two existing structs and a few lines to `fault_stream.go` and `emergency_stream.go`, and these lines move with the files later at no extra cost.
The sampler members are in `sim` and in the save, which is not on the wire.

Recommendation: do not do it before stage 7.
After stage 7, measure the `podsim.wasm` size with a throwaway split, and drop it if the gain is small.
Decision 13.

## 7. Sized options

The estimates are my judgment in agent hours, and nobody measured them.
Each range includes the patch's tests, lint, and `go fix`, and the integration row holds full gate runs, review rounds, and their fixes.
Soak run time is machine time, and it is not in these figures.
The only recorded reference is the October note of 65 to 100 hours for stages 4 to 7 together, with no split.
Stages 4 to 6 in that figure are train, reverse, and group work, which stage 7 does not have, and a patch with repeated blockers is outside the range.

| Element | Option A: minimal | Option B: full, no new holds | Option C: B plus new policy |
| --- | --- | --- | --- |
| Sampler and format | Both samplers, terminal rule, restore lifecycle, members, refusals, proofs, event trace and boundary fixtures: 16 to 24 h | The same: 16 to 24 h | The same as B |
| Activation | Validators, session paths, command-driven parity, size boundary: 3 to 5 h | The same: 3 to 5 h | The same as B |
| Policy | Rules of 5.2 row a, in the sampler | The same | Rows b to e, with new holds, purposes, amendments to F11 and W5, and a change to a marker removal refusal: 20 to 40 h |
| Scenarios | Flags: 1 to 2 h | Flags and one preset: 2 to 3 h | The same as B |
| Tools | Two refusals: 1 h | `parkride` refusal: 1 h. `compare` with terminal accounting: 8 to 14 h | The same as B |
| Controls | **Debris** button, mark selection, **Clear debris**: 5 to 8 h | **Incidents** panel, **Debris** button, and editor form: 9 to 15 h | The same as B, with rows for new states: 1 to 2 h more |
| Metrics | `sampled`, `skipped`: included above | Service counters, gate, `session.Metrics`, OpenTelemetry: 4 to 6 h | The same as B |
| Debris drawing | Segment and mark: 2 to 3 h | The same, and the pod rings: 2 to 4 h | The same as B |
| Soak and gates | One short soak, with A5: 3 to 5 h, plus run time | Drivers for each approved gate, reference runs, clear-out for progress, timed runs, sampled-tick latency: 12 to 20 h, plus run time | B, again for each new state: 18 to 30 h |
| Integration | 4 to 6 h | 6 to 10 h | 10 to 16 h |
| Docs | 2 to 3 h | 3 to 4 h | 4 to 6 h |
| Patches | 12 | 15 | 20 to 25 |
| Effort | 37 to 57 h | 66 to 106 h | 98 to 166 h |

| Option | Gives | Leaves out |
| --- | --- | --- |
| A | Random incidents run, are saved, and show on the map. | Editor form, `cmd/compare`, the effect on service, a preset, and the approved gate size, by A5. "Stage 7 in full" would not be true. |
| B | Every scope element, within the approved rules and gates. | New holds and automatic rescues. |
| C | Also treats occupied pods that wait a long time. | It costs most, and nobody has evidence that it is needed. |

Recommendation: option B.
It is the option that meets "stage 7 in full" with no new transition.
The soak then gives the evidence for or against the rows of option C.

## 8. Decisions for the maintainer

The maintainer answered these on November 4, 2026, and each answer was the recommendation.
So this contract is option B, and amendments A1 to A4 apply.
A5 does not apply.
The rest of this section keeps the options, so that a reader can see what each answer excludes.

1. **Scope.**
   A: minimal, 37 to 57 h; B: full, no new holds, 66 to 106 h; C: B plus new policy, 98 to 166 h.
   Recommend B.
2. **What "full policy" means.**
   a: sampler rules only; b: also an automatic emergency after a long wait; c: also refuges.
   Recommend a, and revisit b after the soak.
3. **Sampled emergency on a platoon pod.**
   The October note says refused, and the approved contract and the code accept it and defer it until the link drains.
   a: keep the code; b: skip such a pod in the sampler only.
   Recommend a.
4. **CPU types.**
   `amd64` and `arm64` can schedule an event one tick apart.
   a: accept this; b: integer math, which changes an approved contract.
   Recommend a.
5. **`cmd/compare` with a rate.**
   a: run it, 8 to 14 h; b: refuse it, 1 h.
   Recommend a, because it is the only way to measure the delay that incidents cause.
6. **Car runs with a rate.**
   a: refuse; b: add interruption handling to the car ledger.
   Recommend a.
7. **Default rates and presets.**
   Existing presets stay at 0.
   a: add `rail-hub-incidents`; b: no new preset.
   Recommend a, with 10 faults an hour, 10 emergencies an hour, `fixed` 600 s, debris share 0.25, and debris length 2 m.
   The 10 and the 600 come from the contracts, and the 0.25 is a play value with no source.
   Status 2026-11-04: the maintainer approved `fixed` 120 s for the fault duration of `rail-hub-incidents`, and the rate stays at 10 an hour.
   The soak of section 5.1.9 keeps 10 an hour and 600 s.
8. **Debris button.**
   a: a click places 2 m until cleared; b: also ask for a length and a duration.
   Recommend a.
9. **Where to clear and read incidents.**
   a: a new **Incidents** panel; b: only a button on the selected debris mark.
   Recommend a.
10. **Editor form for rates.**
    a: yes; b: no, project file and `cmd/scenario` only.
    Recommend a.
11. **Emergency response numbers.**
    a: true mean and longest, with a new `endedTicks`, 30 more bytes; b: longest only; c: a full histogram, with a new array member and a limit.
    Recommend a.
12. **Red ring for a faulted pod and an emergency pod.**
    a: yes, a small view change; b: no, amber as for any wait.
    Recommend a.
13. **Session wire package.**
    a: not before stage 7, and measure the `podsim.wasm` size afterward; b: do it first; c: drop it.
    Recommend a, and please confirm that section 6 is what you meant.
14. **Service-effect numbers with no rate.**
    a: they count only while a sampler is live, so a manual fault at rate 0 writes no new bytes and shows no such numbers; b: they always count, so that fault writes new bytes, as a named exception.
    Recommend a.
    Status 2026-11-05: the maintainer confirmed that the OpenTelemetry series always exist and read 0 with no live sampler, as the journey gauges do.
15. **The 44 reserve bytes.**
    a: widest rate 24 bytes, as the approved text says, so a rate-0 project within 44 bytes of the 32 MiB cap is refused; b: measure a rate above 0 at its own bytes, with one more branch and an amendment to three approved lines.
    Recommend a, because no project that I know of is near the cap, and the rule stays one rule.
16. **Soak size.**
    a: after a timing run, the approved size once locally with the result recorded, and 1 seed for 1 hour in CI; b: approve a smaller size now.
    Recommend a, and if the timing run shows that a is too long, I bring a measured reduction as a new amendment.
17. **A1: drop the marker-only gates.**
    Recommend approve.
18. **A2: one soak with commands and sampler together, in place of two soaks.**
    Recommend approve.
19. **A3: time 3 seeds on each preset with checks off, in place of all 20 seeds.**
    Recommend approve.
20. **A4: plain proofs in place of the composed fixtures, which Express would otherwise need built, at a cost that I have not sized.**
    Recommend approve.

A5 goes with answer A to decision 1.

## 9. Patch order

Each patch builds and passes the tests by itself.
The identity tests of section 5.1.5 run after each one.
The `perHour must be 0` refusals stay until patch 6, so no project can set a rate before it.
Hours are for option B, by judgment, and they exclude the integration row of section 7.

| # | Subject | Needs | Option B contents | Option A contents | Hours |
| ---: | --- | --- | --- | --- | ---: |
| 1 | `sim: Add the incident sampler state` | None | The types, the idempotent start, the uniform draw, the interval, the terminal rule, `Clone`, `Reset`, F13, E9, and the vehicle order test. | The same. | 3 to 5 |
| 2 | `sim: Start faults from the sampler` | 1 | The start in ticks, targets, segments, durations, the stage hook, and the event trace fixture and a fault-only boundary fixture. `sampled`, `skipped`, and the gated fault counters. | The same, with `sampled` and `skipped` only. | 5 to 8 |
| 3 | `sim: Start emergencies from the sampler` | 1, 2 | `startEmergency`, targets, the event trace fixture, and the boundary fixture with both samplers. `sampled`, `skipped`, and the gated emergency counters. | The same, with `sampled` and `skipped` only. | 4 to 5 |
| 4 | `project: Derive the incident rates` | 1 | `EffectiveIncidentRates`, `incidentSeeds`, `ConfigureIncidentSampling`, and a golden seed test. No caller outside tests. Validators do not change. | The same. | 1 |
| 5 | `session: Save the incident samplers` | 2, 3, 4 | Saved members with pointer presence, exported counters, `Rates` in the restore input, the demo save helper, the refusals, the start calls at session start, reset, apply, and restore, the version rule, golden files, byte proofs, and browser constants if the versions move. | The same, with fewer counters. | 6 to 9 |
| 6 | `project: Accept incident rates` | 5 | Validators, the widest rate of decision 15, both tool refusals, session lifecycle and parity tests, size boundary, and the settings text of `docs/operations.md`. | The same. | 4 to 6 |
| 7 | `compare: Run incident rates` | 6 | Section 5.3.2: scenario settings, arm setup, drains, terminal accounting, columns, tests. The refusal goes. | Not in A. The refusal stays. | 8 to 14 |
| 8 | `scenario: Add incident flags` | 6 | Flags and the preset of decision 7. | Flags only. | 2 to 3 |
| 9 | `view: Draw debris` | None | `debrisMarks`, the mark, and the pod rings of decision 12. | `debrisMarks`, the mark, and its hit test, with no pod rings. | 2 to 4 |
| 10 | `view: Add the incidents panel` | 5, 9 | The panel, **Clear**, and the counters. | **Clear debris** on a selected mark, with no panel. | 4 to 7 |
| 11 | `view: Place debris from the map` | 10 | `pickLane`, arming, and the command, in the panel. | The same, with a **Debris** button in the inspector row. | 3 to 5 |
| 12 | `web: Add incident settings to the editor` | 6 | Form, binding, and round trip. | Not in A. | 2 to 3 |
| 13 | `telemetry: Report incident metrics` | 5 | `session.Metrics`, instruments, and `docs/operations.md:559`. | Not in A. | 1 to 2 |
| 14 | `scenarios: Add the incident soak` | 6 | Drivers for each gate of section 5.1.9, the stop entry, the sampled-tick gate, timed runs, and the record of the results under the project's records path. | One short soak, by A5. | 12 to 20 |
| 15 | `docs: Describe scenario incidents` | All | Each line below, and status entries in the three approved contracts. | The same, for what A has. | 3 to 4 |

Option C adds 5 to 10 patches after patch 13, one set for each row of section 5.2 that it chooses, and each set gets its own contract table first.

Patch 15 changes these lines, which stage 7 makes false.
`README.md` and `PROJECT_BRIEF.md` were rewritten, and their text moved to `docs/model.md`, `docs/reference.md`, and `docs/user-guide.md`.
So the patch must search the current text of all these files for each sentence at implementation time, and it must not trust the line numbers below.

| Location | Today |
| --- | --- |
| `docs/design/release-readiness.md:27`, `:29`, `:73` | Stages 4 to 7 dropped, "ended at stage 3" |
| `485f2f5:PROJECT_BRIEF.md:660`, `:687`, `:688` | No scenario rate, and stopped after stage 3 |
| `485f2f5:PROJECT_BRIEF.md:678`, `:715-716` | "The feature is off by default", and a refused command. This is already false after the marker removal. |
| `485f2f5:PROJECT_BRIEF.md:681`, `:718` | Open questions on how the game and the metrics show incidents |
| `docs/operations.md:523`, `:531`, `:541`, `:542`, `:549`, `:559`, `:574`, `:577`, `:582`, `:601` | Rate must be 0, no editor control, tools run off, no debris drawing, no OpenTelemetry |
| `docs/protocol.md:482`, and the counter tables | No debris drawing |
| `485f2f5:README.md:1723`, now `docs/model.md:425` | No debris drawing |
| `PROJECT_BRIEF.md:127` | Stage 7 "has no contract yet" |
| Status notes of the three approved contracts | "Stages 4 to 7 are out of scope". Each gets a dated entry that names this document, the rules of section 3, and each approved amendment. |

## 10. Alternatives rejected

| Alternative | Why not |
| --- | --- |
| Draw incidents from the demand PCG | Each contract forbids it, and it changes demand. |
| One PCG for both samplers | Emergency 10.2 forbids it, and a change of one rate would move the other's events. |
| A new `faults.seed` member | It adds a project member and a digest change. The demand seed already varies a run. |
| A live rate command | A new action, a digest extension, and a sampler change in mid-run. A project apply already covers it. |
| Refuse a rate on a platoon project | The skip rule already handles a target that is not supported. |
| A precomputed event list | It needs a horizon and a saved cursor, and it cannot look at live targets. |
| Rounding durations to whole seconds | It changes approved text and gains only a smaller change to one inner function. |
| Per-tick `blockedLaneTicks` | It resends the whole `faults` group in every delta while any fault is active. `clearedTicks` carries the same information at clears. |
| No sampler after a logical restore of a demo, until a reset | The next save breaks the presence rule, and the rate of the project stops with no message. |
| Sampler start from tick 0 after a logical restore of a demo | `nextTick` can be below the restored tick, and the next restore refuses it. |
| An origin bit on each record, so that the gate counts sampled records only | A new tuple member and more bytes. In a project with a rate, a manual fault also costs the service. |

## 11. Not verified, and what must be measured

Each section above marks its own unverified points.
These were to be measured before or during the work.
`docs/studies/incident-soak.md` holds the readings, which are not from a quiet host:

| Item | Status |
| --- | --- |
| The Express headroom | Not known. No proof exists. |
| The time of the soak | Timing run done. The maintainer rejected each extrapolated size and set a limit of 2 hours of wall time on 2026-11-05. The new size needs an amendment. |
| The share of skipped events on each preset | Must be measured. Debris refusal on a busy lane can be high, and I have no figure. |
| Sampled-tick time against 50 ms | Measured on a fixture with little traffic: slowest 0.95 ms. Reported at the project limits: 260 to 429 ms cold, and 13.5 to 32.5 s warm. |
| Step time at 64 debris and 1,000 pod faults | Measured: median 983 µs against 20 µs, slowest 5.0 ms against 0.62 ms. |
| Evidence that any real system has these rates | None. The preset values are for play and for the soak. |

Status 2026-11-06: after `458f14a` at 30 Hz, the warm reading at the project limits is about 160 ms for the emergency part and 122 ms for the fault part, and the test reports their sum (`docs/studies/incident-soak.md`).
So 13.5 to 32.5 s above is history, and the row stays a report against 50 ms and not a gate.
The maintainer asked on 2026-11-06 for more work on this time, and at `8ffb20b6` the warm sum is 86.4 to 87.7 ms.

## 12. Review result

| Finding | Result | Reason |
| ---: | --- | --- |
| 1 | Accepted | A logical restore of a demo gets a sampler by the idempotent start, with the saved project seed and the restored tick (section 5.1.3). |
| 2 | Accepted | The identity has named exceptions (section 5.1.5), the counters move only with a live sampler, and the 44 bytes go to decision 15. |
| 3 | Accepted | One terminal rule covers scheduling, invariants, and restore (section 5.1.1). |
| 4 | Accepted | Each approved gate is restored, with the clear-out for progress and a whole-step latency gate. Changes are amendments A1 to A5. |
| 5 | Accepted | Patch 7 carries the settings and the terminal accounting, at 8 to 14 h. |
| 6 | Accepted | Delivery and rail tests replace the claim that conservation detects a missing drain (section 5.3.2). |
| 7 | Accepted | Event traces, boundary fixtures, and physically unchanged restores are in section 5.1.8. |
| 8 | Accepted | Section 9 has prerequisites and per-option contents, and rate derivation comes before its restore caller. |
| 9 | Accepted | `nextTick` is a pointer, and the explicit-zero table is in section 4.3. |
| 10 | Accepted | G11 was wrong: `s.vehicles` is in ID order, and a test pins it. |
| 11 | Accepted | `emergencyTicks / ended` is not a completed mean, so `endedTicks` adds 30 bytes, by decision 11. |

The second round found no blocker, and all five of its findings are accepted: the soak stop keeps the samplers live, the fixture with both samplers moves to patch 3, the terminal fixture starts valid, the text of the rejected answer to decision 15 is marked, and the instruments list `sampled`.
