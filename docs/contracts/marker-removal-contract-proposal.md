# Contract marker removal proposal

Status: approved by the maintainer on November 4, 2026.
The approval covers the removal of the single-value markers, the format and contract changes that it needs, and the choices below.

Maintainer choices on November 4, 2026:

- Faults and emergencies are always on, with no project switch.
- Incident members have no presence rule, and the structural checks stay.
  A hold that can never be released must be refused.
- The **Fault** and **Emergency** buttons do not show during the traffic demo, or after it until a reset.
- The `faults` and `emergencies` settings stay, because stage 7 comes next and implements them.
  Marker removal comes first, and stage 7 gets its own contract.

## Problem

`incidentContract`, `faultContract`, and `emergencyContract` each have one valid value.
Each marker is an on or off switch that looks like a version.
`orderContract` selects plain or Express, so it stays.

The markers cost 271 references in 62 test files, three presence scanners for each of the stream, the HTTP state, and the saved state, and a rule that a marker and a settings member must agree.
`sim` also has six constructor forms because contracts were added one at a time.

## Measurements

These results come from a throwaway build that forced `faultsOn` and `emergenciesOn` on in every fleet but the demo.

| Question | Result |
| --- | --- |
| Do results change with no active fault? | No. `cmd/compare` JSON has the same SHA-256 in 3 off and 3 on runs, for each of Busy (2 h), Rail Hub (2 h), and London Central (20 min), at one request each 8 s. |
| Does run time change? | Not above noise. Rail Hub user time is 38.0 to 40.6 s off and 38.2 to 40.2 s on. Busy varies from 45.6 to 59.1 s in both modes. The fault stage adds two loops over the pods in each tick. |
| Do the tools need new code? | No. Only a session command starts a fault or an emergency. `perHour` must be 0 until stage 7. `cmd/compare` and `cmd/parkride` send no such command. |
| Can the sim default be on? | No. 25 `internal/sim` tests fail with it, for example with `E2: pod 01 has the emergency hold and no emergency record`. They use the stage 1 test entries, which need both switches off. Each failure read is a test of a switch or a stage 1 entry. `internal/scenarios`, `cmd/compare`, and `internal/parkride` pass. |

Plain byte bounds, with each incident, fault, and emergency member at its widest:

| Format | Today | Growth | With all members | Cap | Headroom |
| --- | ---: | ---: | ---: | ---: | ---: |
| Saved state | 125,412,377 | 566,948 | 125,979,325 | 134,217,728 | 8,238,403 |
| Full stream | 48,618,482 | 2,116,553 | 50,735,035 | 67,108,864 | 16,373,829 |
| HTTP state | 82,174,660 | 2,116,553 | 84,291,213 | 134,217,728 | 49,926,515 |

The growth uses 1,064 fault records, 4 emergency records, a leg origin on each of 16,200 orders, a hold and a purpose on each of 1,000 pods, and an excluded pod on each of 8,200 waiting trips.
The saved-state figure also counts 31,000 bytes for the operational tuples, and the stream figures count 433,186 bytes for the faults group.
The caps hold, so no count limit changes.

## Proposed design

### Project

Remove the three marker members from `project.Config`.
The strict decoder then refuses them as unknown members, and no migration exists (`AGENTS.md`).
`project.CurrentVersion` stays, because a project file has no other change.

A project with no `faults` member has an evacuation delay of 300 s.
`"faults": {}` also gives 300 s, and an explicit `"evacuationSeconds": 0` stays 0.
`null` at any level stays refused, in a whole project and in a partial project update.
The editor keeps the same round trip.

`faults` and `emergencies` stay as optional settings with their present members, and each member keeps its default.
`perHour` must still be 0, until stage 7 accepts a higher rate.
An absent `emergencies` member and `"emergencies": {}` both give the rate 0.

### Simulation switches

`s.faultsOn` and `s.emergenciesOn` stay as the gates of the operations, and a new fleet still starts with both off.
This keeps the stage 1 test entries, the tools, and the demo on one rule.
`SetFaults` and `SetEmergencies` stay.

`project.ConfigureExperiments(simulation, config, operations bool)` sets both switches to `operations`.
The argument replaces the marker reads, so `demoProject` goes.

| Caller | `operations` |
| --- | --- |
| Session: start, reset, project apply | `true` |
| Session: demo start | `false` |
| Session: restore | `true`, or `false` for the demo fleet (`demoFleet`) |
| `cmd/parkride` | `false`, because it sends no command |
| `cmd/compare` | Does not call it, as today |

`IncidentForTest` and `InterruptRider` lose their marker gate.

### Restore input

`RestoreStateInput` loses the three markers, and gains `FaultsOn bool` and `EmergenciesOn bool`.
`Faults` stays as the settings.
`PreparedRestoreInput` has only `IncidentContract` today.
It loses that member, and gains `FaultsOn`, `EmergenciesOn`, and `Faults`, and its adapter forwards them.

`session` sets both to `true` for every restore, and then turns them off for a demo fleet, as it does today for a marked project.
Each production restore therefore runs the on rules of F11, E2, and E4.
A `sim` test that passes neither gets the off rules that it has today.

### Holds that cannot be released

With the switches on, these holds have an owner that can release them:

- `releaseFaultHolds` releases a fault hold that has no fault and no fault-owned purpose, in the next fault stage.
- A fault-owned empty recovery ends at its berth, and that releases the hold.
- `endEmergency` releases the emergency hold of a record.
- E2 and E4 refuse an emergency hold or an emergency unload with no record, before either tier.

Three cases have no owner, and `CheckIncidentPolicy` refuses them.
It keeps its name and its place: `checkPolicy` runs before either tier, so a refused save has no logical fallback and moves aside.

- A refuge purpose, with any owner.
  Only the test entry `IncidentForTest` sets or ends a refuge, so a pod with this purpose in a save holds forever.
- An empty recovery owned by the emergency hold, with or without a fault hold or a faulted pod.
  Only a fault sets an empty recovery.
  The emergency stage ends the record while the purpose still owns the hold, so `restoreService` refuses, and arrival then clears the purpose and keeps the hold.
- A demo save with a hold, a purpose, an owner, an interrupt, a fault member, or an emergency member.
  The demo fleet has its switches off after the restore, and it starts with zero counters, so a valid demo save has none of these.

A save is a demo save when `State.Demo` is present or `State.Pods` is longer than `Fleet`, the rule of `demoFleet`.
The check reads the saved state, because the logical tier removes the demo fleet and clears its holds and purposes.
No code clears a hold from a damaged file.

### Wire

`sim.Snapshot` and `session.SimulationFrame` lose the three markers, and gain `operationsOff` (bool, `omitzero`).
The value is true when either switch is off, and in production that means the demo fleet.
`TopologySnapshot` loses the three markers and gains nothing.

The view shows the two buttons when `operationsOff` is false.
This also hides them after the demo ends and until a reset, because the fleet still has the parked demo pods and each press gets `faults are not enabled`.

`operationsOff` changes only with a new fleet, and a new fleet starts a new generation.
`sameChain` also compares it, so a change of the value cannot pass as a delta.
A frame with `operationsOff` and a fault or emergency record or counter is refused at the encoder and the assembler, by `checkFaultFrame` and `checkEmergencyFrame`.

The encoder writes the `incident`, `faults`, and `emergencies` groups for each frame.
A delta sends only a changed group, and each full-frame member stays `omitzero`, so the plain bytes do not change.

`web/shell.js` and `web/editor.js` lose `INCIDENT_MARKER`, `FAULT_MARKER`, and `EMERGENCY_MARKER`, and their checks.
`REMOVED_MEMBERS` gains the three marker names, with tests.

### Presence and values

The presence rules go, and the rules on values stay.

| Rule | Today | Proposed |
| --- | --- | --- |
| Incident member needs its marker | Refused | Rule goes |
| Fault or emergency member in a save needs its marker | Refused | Rule goes |
| Fault or emergency member in a stream or HTTP frame needs its marker | Refused | Refused when `operationsOff` is true, for typed values only |
| `null` incident, fault, or emergency member | Refused | Stays, in saves, full frames, HTTP states, and replacement groups |
| Empty string incident member | Refused | Stays |
| Empty `faults` or `emergencies` member in a save | Refused | Stays |
| Negative counter, unknown hold bit, unknown purpose, purpose with no hold | Refused | Stays, for each frame and save |

`scanIncidentPaths`, `scanFaultPaths`, the emergency scan, and the saved scans lose the `found` result and the `marked` input, and keep their refusals of values.
`checkIncidentPresence`, `checkFaultPresence`, `checkEmergencyPresence`, `hasIncidentFrameFields`, `validateIncidentValues`, and the member flags on `StreamEnvelope` go.

### Refusal order and text

`AGENTS.md` makes the order and the text of refusals part of the format.
Each table lists one sequence, in its present order, with the checks that change.
Each check that is not listed keeps its place and its text.
Tests pin the new order with inputs that have more than one error.

Project decode (`decodeJSON`, and `banks.go` for a partial update):

| Step | Proposed |
| --- | --- |
| `scanProjectBanks`, `scanProjectFields`, `topMember` marker cases | The three marker cases go. |
| `topMember` `faults`, `emergencies` | Same, so `null` is refused here. |
| Strict decode | Same place. It now refuses each marker name as an unknown member, after these scans. |
| `validateVersion` when present | Same |
| `ValidateIncidentContract` | Goes |
| `validateFaultContract` | Becomes `validateFaults`, for a present `faults` member. |
| `validateEmergencyContract` | Becomes `validateEmergencies`, for a present `emergencies` member. `emergencies perHour must be 0` stays. |

Direct `Validate` (`projectChecks`):

| Step | Proposed |
| --- | --- |
| `validateVersion` | Same |
| `validateIncidentContract` | Goes |
| `validateFaultContract` | Becomes `validateFaults`. The three `requires` refusals and `fault contract must be fault-v1` go. Range refusals stay. |
| `validateEmergencyContract` | Becomes `validateEmergencies`, as in the decode. |
| `validateWidestSize` | Measures the widest `faults` and `emergencies` for every project. |

Editor (`engine.go`):

| Step | Proposed |
| --- | --- |
| Branch strict decode | Each marker name is an unknown key. |
| `copyDecodedBranch` marker cases | Go |
| `checkMetadataBranches` | Same. The native decoder gives the same refusals as above. |

Saved state decode (`decodeStateJSON`, in order):

| Step | Proposed |
| --- | --- |
| `prescanJSON` | Same |
| `scanContractMarkers`, feature names | Goes. The strict decode refuses each name as an unknown member. |
| `scanPackedOrders`, `scanStateOrderFields`, strict decode | Same |
| `checkIncidentMembers` | Same place. `saved state without the incident marker contains an incident member` goes. `saved incident member %s is null` stays. |
| `checkFaultMembers`, `checkEmergencyMembers` | Same place, with the same change. |
| `validateWireContract`: `saved order contract markers disagree` | Same |
| `validateIncidentValues` | Goes |
| `validateFaultValues`, `validateEmergencyValues` | The marker branch goes. `saved faults member is empty` and its emergency form stay. |

Saved state encode: `validateWireContract` has the same change, and it is the only check.

Saved state restore, before either tier (`persist.go`, then `restoreInputChecks`):

| Step | Proposed |
| --- | --- |
| `checkPolicy` | Stays first. `saved pod %s has the emergency hold without the emergency contract` and `saved pod %s has an emergency unload without the emergency contract` go. New, in this order for each pod: `saved pod %s has a refuge`, then `saved pod %s has an empty recovery with the emergency owner`. Then `saved demo pod %s has a service hold or an operational purpose`, then `saved demo fleet has fault or emergency state`. |
| `checkRestoreOrderContract` | Same |
| `checkRestoreIncidentFields` | Goes, with `saved incident serial needs the incident contract` and `saved interrupted orders need the incident contract`. |
| `checkOperationalFields` | `saved service holds and operational destinations need the incident contract` goes. The exclusion and station checks stay. |
| `checkSavedFaults` | `saved faults need the fault contract` becomes `saved faults need faults on`. `the fault contract requires the incident contract` goes. |
| `checkSavedEmergencies` | `saved emergencies need the emergency contract` becomes `saved emergencies need emergencies on`. `the emergency contract requires the incident contract` goes. |

Saved state restore, after the tier: `SetFaults(false)` and `SetEmergencies(false)` for a demo fleet stay, with their text.

Stream and HTTP decode (`decodeMarkedJSONLimit`, in order):

| Step | Proposed |
| --- | --- |
| Size limit, `scanRootMarkers` | Same |
| `scanContractMarkers`, feature names | Goes, with its null, empty, and unknown refusals. The strict decode then refuses each name as an unknown member, so that refusal moves after `scanPackedOrders`, the boarding and service scans, and the value scans. |
| `scanIncidentMembers`, `scanFaultMembers`, `scanEmergencyMembers` | Same place. They keep the refusals of null and empty values, and return no presence flag. The marker names leave their path tables, so a `null` marker reaches the strict decode as an unknown member. |
| Strict decode | Same |

Stream and HTTP checks:

| Step | Proposed |
| --- | --- |
| `validateEnvelopeContract`, `validateEncodedContract`, `NewStreamAssembler`, `FrameState`: marker validation | Goes. The order contract checks stay. |
| Topology and frame marker agreement (`protocol.go`, `fault_stream.go`, `emergency_stream.go`) | Goes |
| `checkStreamEnvelope`: three presence checks | Go |
| `DecodeStateJSON`: three presence checks | Go |
| Replacement `incident` group with no marker | Goes |
| Replacement `faults` and `emergencies` groups with no marker | Refused when the base frame has `operationsOff`. |
| `checkIncidentFrame` | The marker branch goes. |
| `checkFaultFrame`, `checkEmergencyFrame` | The marker validation goes. The off branch reads `operationsOff`. |
| `stream state without the fault marker contains a fault member` | Becomes `stream state with operations off contains a fault member`. |
| `stream state without the emergency marker contains an emergency member` | Becomes `stream state with operations off contains an emergency member`. |

Browser (`markersAgree`, `frameMarker` in `web/shell.js` and `web/editor.js`):

| Step | Proposed |
| --- | --- |
| Reply has a topology object | Same |
| Feature marker value, topology and simulation equality, root absence, and the incident dependency | Go |
| `CONTRACT_MARKERS` comparison of the order contract | Same |
| `plainStateTree` | `REMOVED_MEMBERS` gains the three names, so each name refuses the reply whatever its value. |

Tools:

| Check | Proposed |
| --- | --- |
| `car runs do not support incidentContract` | Goes. Decoding the project refuses the name as an unknown member. |
| `errFoundationProject` | Becomes `car runs require a foundation project: version 1 without orderContract`. |
| `foundationProject` | Reads the version and `OrderContract`. |
| `comparisons do not support incidentContract` | Goes, as for car runs. |

### Versions

`stateVersion` goes from 9 to 10, `StreamVersion` from 7 to 8, and `StateMediaType` from `state-6` to `state-7`, in Go and in the browser constants.
The golden files in `internal/session/testdata` and the version text in `docs/operations.md` and `docs/protocol.md` change with them.
An older save moves aside, and the server starts a new session.

The plain stream golden files must not change, because they have no version member and no marker.
`TestStreamHelloBytes` and the hello-version assertions show the new version.

### Byte proofs

`TestPlainStateFileWorstCaseSize` and `TestPlainStreamMaximumEncoding` now bound every plain file, so they must include each incident, fault, and emergency member at its widest.
The existing scalar filler cannot do this, because the encoder refuses a negative counter and a frame with `operationsOff` and a record.
So the fixtures get dedicated members that pass the encoder: positive counters of `sim.MaxCounter`, records in serial order, `operationsOff` false, and known holds and purposes.
A fault record that cannot pass the frame check is measured apart and added to the total, as the table above does.
`fillPlainProofScalars` keeps its other rules.

The proofs do not bound a delta, today or after this change.
A delta adds vehicle IDs and replacement wrappers, and the encoder refuses a result above the cap.
This change adds about 2.1 MB to a full frame against 16.4 MB of headroom.
A delta proof is separate work.
Express still has no byte proof.

### Command digests

The digest extension numbers 1, 2, and 10 go with the markers, and stay retired.
Extensions 3 and 11 stay, for `faults` and `emergencies`.
The digest of a command with no marker and no settings member does not change, and a test pins it.
The digest of a command that had a marker changes, and no compatibility applies.

### Constructors

`FleetContracts`, `NewFleetWithContracts` (both forms), and `New` go, in the format patch.
`NewFleet` and `NewFleetWithOrderContract`, each with its prepared form, stay.
`ValidateIncidentContract`, `ValidateFaultContracts`, `ValidateEmergencyContracts`, and the three contract types go.

## Stage 7

Stage 7 starts after this change, under its own contract.
It changes three results of this document, and this change must not block them:

- `perHour` above 0 starts faults and emergencies without a command, so `cmd/compare` and `cmd/parkride` need rules for such a project.
- The sampler members add bytes to the saved state, so the byte proofs grow.
- A sampler needs the switches on, so the tool paths of the `operations` argument need review.

## Alternatives

| Alternative | Why not |
| --- | --- |
| `sim` default on, off only for the demo | 25 stage 1 tests need a rewrite, and the tools run two more loops in each tick for nothing. |
| No frame member: hide the buttons on `state.Demo` | No format member. After the demo ends, the buttons show and each press gets `faults are not enabled`, until a reset. |
| One constructor, `NewFleet(network, placements, contract)` | About 260 call sites gain an empty argument. |

## Contract documents

The three incident contracts get a dated status entry that names this document.
Their sections stay as written, and text about the markers then describes the earlier design.
`docs/operations.md`, `docs/protocol.md`, and `README.md` change to the current behavior.

## Patches

Each patch builds and passes the tests by itself.

1. Remove the markers across `sim`, `project`, `session`, `editormodel`, `view`, `web`, and the tools, with `operationsOff`, the restore input, the restore policy, the constructors, the versions, the golden files, the byte proofs, and the docs.
   The change is one patch because the format must change at one time.
2. `docs`: Add the status entries to the incident contracts.

## Test plan

- Identity: a plain project gives the same saved-state bytes with the operations on and off, in a run with no command.
- Refusals: pinned order and text for the tables above, with inputs that have more than one error.
- Values: `null` and empty members in saves, full frames, HTTP states, and replacement groups.
- Switches: `operationsOff` and the buttons through the demo start, the demo end, a reset, a rewind, a project apply, and a restore of a demo save.
- Commands: a fault, a clear, and an emergency in an ordinary project, and the refusal in a demo fleet.
- Holds: a save with a refuge or an emergency-owned empty recovery moves aside, in both tiers.
  A test runs each surviving combination of hold, purpose, owner, fault record, and emergency record through its release.
  A demo save with a hold, a purpose, a record, or a counter moves aside, in both tiers.
  A restored ordinary fleet with an orphan fault hold releases it.
- Project: absent, `{}`, explicit 0, and `null`, each removed marker as an unknown member, in whole projects, partial updates, and the editor.
- Tools: `cmd/compare` and `cmd/parkride` run a project with `faults`, and refuse a marker name as an unknown member.
- Golden files: plain and Express stream bytes do not change, and the hello has the new version.
- Mutation: each new or moved guard, by the `verify-by-mutation` rule.
- Gate: `mise run check` on the tip, because saved state and stream code change.

## Failure modes

- A demo or rewind path changes a switch without a new generation.
  `sameChain` then forces a full frame, and the switch test covers the path.
- A fault command in a project shape that no test covers trips an invariant at run time.
  The project format never refused any combination of a marker with other settings, so this risk exists today for a marked project, and this change exposes it in every project.
  The existing `TestCompactStationFaults`, depot, Express save, and banks tests cover part of it.
- A marker check that remains in `web/editor.js` or `web/shell.js` refuses each new reply.
  `mise run test:web` and the shell tests cover this.

## Observability

No metric changes.
The fault and emergency counters stay in the frames and the saved state.
A save that fails a new check takes the existing move-aside path, with its existing reason.
