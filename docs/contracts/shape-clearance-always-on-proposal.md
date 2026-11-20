# Shape clearance always-on proposal

Status: Approved by the maintainer on 2026-11-20, with decisions AO-D1 to AO-D10.
The build has started: slices 0 and 1 landed, and section 7.2 records the decisions on the slice 2 stops.
Date: 2026-11-20.
Revised on 2026-11-20 after an adversarial review: 6 major findings and 1 minor finding are folded in.
Line references are for `b2114516`.
Survey numbers are from a scratch study of 2026-11-20 at `151416ae`, and the probe files of that study are deleted.

## Summary

On 2026-11-20, the maintainer chose to make the [shape clearance](../design/shape-clearance.md) rules apply to every network.
This proposal removes the network fields `shapeClearance` and `lateralMerges`, and it applies the rules that `shapeClearance` gates to each network.
It reverses decision D7 of the design ("the field, so that D, the digests, and the goldens stay").
The isotropic 12 m rule refuses lateral geometry that cannot collide: two lanes whose swept bodies are disjoint must still be 12 m apart.
D3-A already gives such lateral pairs the side-by-side body test, but only behind the field.
There is no backward compatibility before v1, so this proposal adds no migration for old projects, saves, or protocol versions.

The change is larger than the merge rule and the audit.
The field also selects the envelope of decision S1 of the [shared-track](../design/shared-track-preset.md) design, rule A1, and berth rule 2 (section 1.2).
Project D, `london-full-express`, gets all of these for the first time, so its evidence runs again (section 5).
The all-pairs audit on each network also reverses lane-end decision D for plain projects.
In a forced-on test run, the audit refused 41 path pairs of test fixtures, and no preset (section 9.1).
Authored projects and drafts that are valid today can become invalid, also without either removed member (section 2.2).

## Conventions

- Each amendment ID has the prefix AO, except the notes for the two shared-track amendment records.
  These continue the ST numbers of those records (ST25 and ST26), and they do not use the SC terms of the shape clearance records.
- AO38 and AO39 are not used.
  An earlier draft gave them to the two shared-track notes.
- A row of section 4 that changes a live target document gives the new text.
- A row for an applied amendment record (the shape clearance amendment proposal and the two shared-track amendment proposals) adds a "Note of <date>" line, as those files already do, and does not rewrite the quoted blocks.
- `<date>` is the date of the approval.
  Each target document with an "Amended through" line changes it to that date.
- Each target document gets one line: "Amended <date>, by maintainer decision: the shape clearance rule applies to each network, and the network fields `lateralMerges` and `shapeClearance` are removed."
  The rows of section 4 do not repeat this line.
  The earlier "Amended" lines stay as records.

## 1. The rule, for each network

### 1.1 Pairs and distances

| Pair | Definition | Distance | Source |
| --- | --- | --- | --- |
| Following | Two pods on one lane, or on two lanes where one ends at the node where the other starts | 12 m, or 20 m with a large pod. Layer A on a qualifying lane | Design part 2. Service contract `:53` to `:54` |
| Merge or split | Two lanes that meet clauses 1, 2, 4, and 5 of the [lateral merge zones](../design/lateral-merge-zones.md) design. Clause 3 does not apply, so large lanes and lanes with tails can form a pair | Side-by-side distance of the classes of the two pods: 3.1 m, 3.55 m, or 4.0 m. The junction orders the pods. The static extent uses the union body of the admitted classes | Design part 3. `newMergeLateral`, `internal/sim/lateral_merges.go:47` |
| Shape | Two lanes that share no node, and whose grown union bodies the sweep proves disjoint at each pair of positions that are not separated | Same side-by-side distance. No resource orders the pods | Design part 4. `shapePairs`, `internal/sim/shape_pairs.go:201` |
| Not certified | The sweep gives `sweepCollision`, `sweepPotential`, or `sweepUnresolved` | 12 m, or 20 m when either lane admits a large class | Design section 3.2, tangency |
| Berth path | A pair with a berth (`shapeCandidate`, `shape_pairs.go:255`) | 12 m or 20 m | Service contract `:70` |
| Depot lateral | Certified depot lane pair | `W(v) = 3.1 + v²/2` | Service contract `:45`, `:52` |
| Maneuver group | Certified pairs and skip rules | Unchanged, and they come first | Service contract `:51` |

Rules that also apply to each network:

- Planes (D1-B): the audit takes the planes of a large path by position, from the envelope reach.
  The oracle envelope of a fleet with a large pod comes from the compiled envelope reach, with the anchor front of each lane and the small-pod rules of the [group physics design](../design/group-physics-design.md).
- Rule A1 of the model, and rule 2 for the locations of a berth ([station banks](../station-banks.md) `:63`).
- Audit coverage (D12-A): the bank audit checks each pair, small pairs included, in each network.
  The early return at `internal/sim/bank_geometry.go:141` and the small-pair skip at `:206` go.
- Oracle (D6-B): `Check` uses the side-by-side distance of the classes for merge, split, and shape pairs.
  The independent body-and-ownership oracle (`LateralBodyCheckForTest`) runs in the qualification tests and the soak driver of each project.
- Debris: debris on a lane of a merge, split, or shape pair also blocks the cells of the partner lane within 12 m.

### 1.2 What the field gates in the code

| Place | With the field | Without the field |
| --- | --- | --- |
| Merge table, `lateral_merges.go:48` to `:59`, `:142`, `:152` | All classes, union body | Small lanes only, with `lateralMerges` |
| Shape table, `shape_pairs.go:202` | Built | None |
| Audit scope, `bank_geometry.go:77`, `:140` to `:142`, `:206` | Each pair of each network | Only with banks, large classes, groups, or depots |
| Envelope reach in the audit, `shape_pairs.go:373` | Built on demand | Whole-lane plane reach |
| Envelope front, `envelopeFront`, `profile_geometry.go:191`, read at `traffic.go:211`, `:497`, and `envelope_reach.go:223` | Anchor front of each lane | Largest front bound of the route |
| Compiled reach and oracle envelope, `prepared.go:232`, `:264`, `envelope_safety.go` | Compiled, S1 envelope with A1 | Route envelope of design section 1.1 |
| Soak driver, `internal/session/incident_soak_test.go:295` | D6 oracle at each check | No D6 oracle |
| Editor, `internal/editormodel/edit_collision.go:127`, `edit_geometry.go:102` to `:107`, `checks.go:233`, `edit_banks.go:121` | Server network check on each edit. No local lane gap check | Local lane gap check unless the draft has banks, large classes, groups, or depots |

### 1.3 Why `lateralMerges` is redundant

The code agrees with the design: no behavior of `lateralMerges` is outside the shape clearance rules.

- `LateralMerges` has one reader, `newMergeLateral` (`lateral_merges.go:48`).
- With `ShapeClearance`, eligibility drops clause 3 only (`:59`), so the candidate pairs are a superset.
- For a lane that admits only small classes, `laneShapeBody` gives the grown body 3.0 m behind, 4.2 m ahead, and 1.55 m to each side.
  `lateralBody(0)` gives the same body (`depot_lateral.go:39`, `shape_pairs.go:45`).
  An empty class set loads as Legacy and Compact (`class_set.go:60`), so a plain lane gets the small body.
- The pair clearance is then 1.55 m + 1.55 m = 3.1 m, which equals `lateralClearance(0)`.
- So on a network without large lanes, the merge table is the same with either field.
  The survey agrees: on each ring preset and on D, the `lateralMerges`-only arm gave the same state hash as the `shapeClearance` arm.

### 1.4 Code that stays

- The whole-lane plane reach (`newPlaneReach`, `plane_reach.go:45`) stays.
  `planes` falls back to it when the envelope reach cannot be built, because a tail bound is not finite (`shape_pairs.go:294`, `:384`).
  Preparation refuses such a network after the audit, so the fallback keeps the order of the refusals and the `PlaneReachError` text.
- The refusal text of the audit (`planeGapText`, `bank_geometry.go:258`) does not change.

## 2. Format and protocol

| Item | Today | Proposed |
| --- | --- | --- |
| `sim.Network` | `LateralMerges` (`digest:"ext=16"`) and `ShapeClearance` (`digest:"ext=17"`), `internal/sim/network.go:129`, `:135` | Both fields removed. |
| Project, command, topology, or editor draft with either member | Accepted | Refused by the unknown-member checks that exist today. The text depends on the entry point (section 2.1). Decision AO-D1. |
| Saved state with either member | Accepted | `decodeSavedProject` refuses the unknown member (`internal/session/state_file.go:675`). The file gets `invalid_state`, moves aside, and the server starts a new session (section 2.1). |
| Saved state without the members | Restores under the old rules | Restores under the new rules. `restoreProject` validates the saved project before the session members (`internal/session/persist.go:488`, `:583` to `:584`). When the all-pairs audit refuses its network, the file gets `invalid_state` with the text `saved project: ...`, moves aside, and the server starts a new session (section 2.2). |
| `stateVersion` | 12 (`state_file.go:29`) | Stays 12. `docs/operations.md:374`: "Until the first release, the removal of a member keeps the version." A narrower set of accepted values also keeps it (`:380`). Decision AO-D2. |
| `StreamVersion` | 10 (`internal/session/stream_codec.go:24`) | Stays 10. The topology decoder refuses an unknown member (`internal/session/topology_decode.go:69`). Clients ship with the server. `docs/protocol.md` states no version rule for a removed member. |
| `web/` | `editor_test.cjs:892` to `:906` checks `lateralMerges` on the London presets | The check goes in slice 3. No other file of `web/` names a member. |
| Digest registry | Entries 16 and 17 (`internal/session/digest_extension_test.go:455` to `:456`) | Both entries removed. The comments at `digest_extension_test.go:438` and `internal/project/config.go:194` to `:196` list 1, 2, 10, 16, and 17 as retired. Only these comments enforce the retirement. |
| Command digest golden | `testdata/command_digests.txt` sets neither field | No change. An unset extension field adds nothing to a digest. |
| Saved-state member golden | `testdata/state_v12_members.txt:254` to `:255` | Both lines removed. |
| London options | `LondonOptions.NoLateralMerges` (`internal/scenarios/london.go:205`), `capacity.lateralMerges` (`:289`, `:355`, `:393`, `:549`) | Removed. D sets it at `london_full_express.go:64`. |
| `cmd/scenario` | `-lateral-merges` flag (`cmd/scenario/main.go:46`, `:66`, `:247`) | Removed. |
| Shared preset | `n.ShapeClearance = true` (`internal/scenarios/london_full_shared.go:172`) | Removed. |

Byte bounds of AGENTS.md:

- The worst-case plain project sets both fields (`internal/session/plain_byte_proof_test.go:192` to `:193`), and the Express proofs build on it.
- The proof pads the project name to exactly `project.MaxFileBytes` (`:197` to `:200`), so the saved-state bound measures the same size.
- A topology without the name can be at most 43 bytes smaller (`,"lateralMerges":true,"shapeClearance":true`).
- No cap changes.
  `mise run test:embedded` runs the four proofs again, and the record gives their sizes.

The version text of `docs/operations.md:303` and `docs/protocol.md:98` does not change, and the golden files keep their names.

### 2.1 Refusal of the removed members

The table gives the text of each entry point for a removed member `<m>`.
It was measured on 2026-11-20 at `b2114516`, with a stand-in member name at the start of `network`, because the real members are still known today.
The project file, command, saved state, and topology rows ran at the tip.
The editor rows ran on the forced-on tree of section 9.1, which has the editor behavior of slice 2.
Slice 3 repeats each row with both real names and pins the text in a test.

| Entry point | Code | Text | Result |
| --- | --- | --- | --- |
| Native project file | `cmd/serve/main.go:456` to `:459` (`DefaultOptionsV1`, `RejectUnknownMembers`) | `read project <path>: json: unknown field "<m>"` | The server does not load the file. |
| Project command | `internal/session/http.go:193` to `:197` | The decoder gives `json: unknown field "<m>"`, and the reply is `invalid command JSON` | HTTP 400, and the session does not change. |
| Saved state, restore | `internal/session/state_file.go:675` | `decode session state: json: cannot unmarshal JSON string into Go sim.Network: unknown object member name "<m>" within "/network"` | `invalid_state`. The file moves aside, and the server starts a new session. |
| Topology (stream, HTTP state, Go client) | `internal/session/topology_decode.go:69` | `json: unknown field "<m>"` | The client refuses the topology. |
| Editor engine, `validate` | `internal/editormodel/engine.go:273` | `decode project branch network: json: cannot unmarshal JSON string into Go sim.Network: unknown object member name "<m>" within "/network"` | Refused. |
| Editor call, `validate` | `internal/editormodel/model.go:142` | `decode editor project: json: invalid JSON object at /network/<m>: unknown object member name "<m>"` | Refused. |
| Editor `checks` | `internal/editormodel/model.go:117`, `checks.go:233` | Check error `decode bank geometry: json: cannot unmarshal JSON string into Go sim.Network: unknown object member name "<m>"` | At the tip, a plain draft with the member gets no check error. From slice 2, each draft runs the server check, which refuses it. |
| Editor geometry edit | `internal/editormodel/edit_geometry.go:84` | `decode bank geometry: json: invalid JSON object at /<m>: unknown object member name "<m>"` | The edit is refused. |
| Editor field edit, for example `name` | `internal/editormodel/model.go:124` | The unknown-member text of the network decode | Refused, by decision AO-D10. Today the edit is accepted, because its patch does not read the network. Slice 3 makes each editor operation refuse a draft with a removed member, and a test pins it. |

Precedence:

- The values `true`, `false`, and `null` give the same text at each entry point.
- With both members, each entry point names the first member in document order.
- A project file with a member and `"version":2` gets the unknown-member text, because the typed decode comes before `validateVersion` (`internal/project/banks.go:42` to `:48`).
- From the code order, not measured: the scans before a typed decode come first: `scanProjectBanks` and `scanProjectFields` for a project (`internal/project/banks.go:34` to `:39`), the scans of `decodeStateJSON` (`state_file.go:467` to `:477`) for a save, and `scanContractMarkers` and `scanStreamServiceMembers` for a topology.
  An error that a scan finds is reported before the member.
- The versions stay (decision AO-D2).

### 2.2 Authored projects and drafts

Projects and drafts that are valid today can become invalid, also when they have neither removed member.
The all-pairs audit runs on each network, and a plain network did not run it before.
`TestShapeAuditCoverage` (`internal/sim/shape_pairs_test.go:434`) shows the case: two small lanes 3.0 m apart are accepted without the field and refused with it.
There is no migration and no exemption.

A network with two independent refused pairs cannot be repaired one pair at a time in the editor.
A geometry edit that repairs one pair still fails the server network check on the other pair, and the editor discards that patch (`edit_geometry.go:84` to `:86`).

Supported repair route, approved by the maintainer on 2026-11-20: repair in the project JSON.

1. Run `go run ./cmd/projectcheck -project <file>` to get the first refused pair.
2. In the project JSON, move or remove a lane of that pair.
3. Repeat until the project is valid.
4. Open the valid project in the editor.

An editor operation that accepts an edit that adds no refusal needs an audit that lists each refusal, and it is not in this proposal.
Acceptance cases, in slice 2, use one plain project with two independent pairs of small lanes 3.0 m apart:

- `TestRepairTwoRefusedPairsInJSON` in `internal/project`: the project is refused with the first pair.
  After a JSON change that moves the first pair, it is refused with the second pair.
  After a JSON change that moves both pairs, it is valid.
- `TestRepairOnePairInEditorRefused` in `internal/editormodel`: an editor geometry edit that moves only the first pair is refused with the text of the second pair, and the test pins that text.

## 3. Network hashes

The survey hashes are for setting the field.
Under removal, a network JSON changes only when it sets a field today.

| Preset | Network SHA-256 | Behavior |
| --- | --- | --- |
| `london-central` | Changes, from `39899b27...` | Same, by the survey runs and the parity test of slice 0 |
| `london-full` | Changes, from `e194426a...` | Same |
| `london-full-shared-express` | Changes, from `652d75eb...` | Same |
| `london-full-express` (D) | Stays `3aa0a2eb...` | Changes: merge rule, S1 envelope, A1 |
| Ring presets, `independent-banks`, `compact-station` | Stay | Change through the merge rule, where they have merge pairs |

## 4. Contract and design text

### 4.1 Live target documents

| ID | File and line | Current text (short) | Proposed text |
| --- | --- | --- | --- |
| AO1 | `service-contract-proposals.md:47` to `:49` | "When the network sets `lateralMerges`, two small pods on a lateral merge pair keep 3.1 meters..." | Delete. The merge rule of `:55` to `:60` covers these pairs. |
| AO2 | `service-contract-proposals.md:50` | "When the network sets `shapeClearance`, the shape clearance rule applies to pods of each class." | "The [shape clearance](../design/shape-clearance.md) rule applies to pods of each class, in each network." |
| AO3 | `service-contract-proposals.md:62` | "...as the group physics design defines it under the field." | "...as the group physics design defines it." |
| AO4 | `service-contract-proposals.md:71` | "The network audit checks each pair of such a network, small pairs included, also without banks, ..." | "The network audit checks each pair of each network, small pairs included." |
| AO5 | `service-contract-proposals.md:80` to `:81` | "The qualification of the field, for pods of each class, runs...", and "In such a network, `lateralMerges` has no further effect." | "The qualification of a project, for pods of each class, runs the body-and-ownership oracle of the design at each tick of a merge test and of a restored continuation." Delete `:81`. |
| AO6 | `service-contract-proposals.md:86` to `:87` | "Debris on a lane of a lateral merge pair...", and "When the network sets `shapeClearance`, debris on a lane of a merge, split, or shape pair..." | "Debris on a lane of a merge, split, or shape pair also blocks the cells of the partner lane within 12 meters." |
| AO7 | `express-physical-encoding-contract-proposal.md:35` | "(the side-by-side distance for merge, split, and shape pairs when the network sets `shapeClearance`)" | "(the side-by-side distance for merge, split, and shape pairs)" |
| AO8 | `express-physical-encoding-contract-proposal.md:207`, `:210` | "When the network sets `shapeClearance`, a merge, split, or shape pair with a large pod keeps...", and "When the network sets `shapeClearance`, the static extents..." | Drop the condition from each sentence. |
| AO9 | `express-physical-encoding-contract-proposal.md:539` | "They apply when the network sets `shapeClearance`, as the shape clearance design defines." | "They apply to each network, as the [shape clearance](../design/shape-clearance.md) design defines." |
| AO10 | `express-standees-contract-proposal.md:74` | "When the network sets `shapeClearance`, a merge, split, or shape pair keeps..." | Drop the condition. |
| AO11 | `express-standees-contract-proposal.md:220`, `:222` | "When the network sets `shapeClearance`, the set of locations of a pod is its envelope...", and "...sections 4.4 and 4.5 each give a branch for the field." | "The set of locations of a pod is its envelope...", and "...sections 4.4 and 4.5 each give a branch for merge, split, and shape pairs." |
| AO12 | `express-standees-contract-proposal.md:253` | "When the network sets `shapeClearance`, this case holds as written for a through pair..." | "This case holds as written for a through pair..." |
| AO13 | `express-standees-contract-proposal.md:268`, `:272` | "When the network does not set `shapeClearance`, it reads whole lanes...", and "When the network sets `shapeClearance`, the audit takes the envelope reach..." | Replace `:268` with "When a tail bound of the network is not finite, it reads whole lanes, and preparation then refuses the network." Drop the condition from `:272`. |
| AO14 | `express-standees-contract-proposal.md:309` to `:313` | The recorded front bound gap, then "When the network sets `shapeClearance`, the oracle takes its envelope from the envelope reach..." | Start `:309` with "Before <date>, without the field,". Drop the condition from `:313`. |
| AO15 | `express-standees-contract-proposal.md:391`, `:393` | "...except for the merge, split, and shape pairs of a network that sets `shapeClearance`...", and "...with the branches of sections 4.4 and 4.5 when the network sets `shapeClearance`." | "...except for the merge, split, and shape pairs...", and "...with the branches of sections 4.4 and 4.5." |
| AO16 | `tick-rate-contract-proposal.md:95` to `:96`, `:98`, `:100` | "A lateral merge pair of a network that sets `lateralMerges` keeps `lateralClearance(0)`...", and "A merge or split pair of a network that sets `shapeClearance`...", and "A shape pair of such a network..." | Delete `:95` and `:96`. "A merge or split pair keeps...", and "A shape pair keeps the same distance, with no speed term." |
| AO17 | `incident-suspension-contract-proposal.md:507` to `:508` | "When the network sets `lateralMerges`, a lateral neighbor is also...", and "When the network sets `shapeClearance`, a lateral neighbor is also a lane that forms a merge or split pair or a shape pair..." | "A lateral neighbor is also a lane that forms a merge or split pair or a shape pair with `l` ([shape clearance](../design/shape-clearance.md))." |
| AO18 | `express-london-qualification-proposal.md:151` to `:156` | "D does not set `shapeClearance`, and the field does not change the text of a row above." and the parity condition | "Since <date>, the shape clearance rule applies to D. It does not change the text of a row above. D is not qualified again until the evidence of section 5 of the [always-on proposal](shape-clearance-always-on-proposal.md) passes, with the Chrome checks of E6 and E7 on the final build (decision AO-D4)." Keep `:156` on the shared-track preset. |
| AO19 | `express-shared-track-qualification-proposal.md:28` | "LondonFull, D, their fleets, and their defaults do not change, and their network hashes stay." | Add: "Since <date>, the network hashes of LondonFull and the preset changed when the network fields left the format, and their behavior did not change (parity test of slice 0 of the always-on proposal). D changed by the shape clearance rule, and its evidence ran again." |
| AO20 | `express-shared-track-qualification-proposal.md:31` to `:32`, `:152` to `:156` | The field-off test, the parity test of D, `TestRunPresetsTurnOnPolicies`, `TestEnvelopeFieldOffMatchesBuilt` | Add after `:156`: "Note of <date>: these tests are removed with the fields. The parity test of the always-on proposal replaces them for the preset." |
| AO21 | `express-shared-track-qualification-proposal.md:254`, `:285`, `:396` | "...it sets `shapeClearance` and `lateralMerges`, as the worst-case project does.", and "...with no banks, `shapeClearance` set, and...", and "On a project that sets `shapeClearance`, each check of the soak driver now also runs the D6 oracle" | These are records of evidence. Add one note after `:396`: "Note of <date>: the fields are removed, and the soak driver runs the D6 oracle on each project." |
| AO22 | `group-physics-design.md:31`, `:38` | "When the network sets `shapeClearance`, a merge, split, or shape pair with a large pod keeps...", and "When the network sets `shapeClearance`, the static extents..." | Drop the condition from each sentence. |
| AO23 | `group-physics-design.md:76` to `:80` | Whole-lane audit rule, and "When the network does not set `shapeClearance`, the route window of a pod can reach further..." | Start `:76` with "When a tail bound of the network is not finite, the network audit applies the same rule before a run, and preparation then refuses the network:". Delete `:79` to `:80`. |
| AO24 | `group-physics-design.md:81`, `:86`, `:118` | "When the network sets `shapeClearance`, the audit checks each pair...", and "When the network sets `shapeClearance` and the fleet has a large pod...", and "...so the case above stays a gap only in a network without the field." | "The audit checks each pair, small pairs included, in each network.", and "When the fleet has a large pod, the envelope...", and "...so the case above is not a gap." |
| AO25 | `lateral-merge-zones.md:6` to `:8` | "The rule is built behind the `lateralMerges` network field." ... "`london-full-express` does not set it, so its behavior does not change." | Add: "On <date>, the field was removed, and part 3 of the shape clearance design applies this rule to each network, D included." |
| AO26 | `lateral-merge-zones.md:73`, `:82`, `:98`, `:217` | "With `shapeClearance`, this clause does not apply...", and "When the network sets `shapeClearance`, ... and `lateralMerges` has no further effect.", and "When the network sets `shapeClearance`, the union body...", and "With `shapeClearance`, large pairs and lanes with tails can form a pair" | "This clause does not apply (see the amendment of section 2).", and "The shape clearance design applies this rule to each class.", and "The union body and the side-by-side distance of the two classes replace this body and this distance.", and "Large pairs and lanes with tails can form a pair." |
| AO27 | `lateral-merge-zones.md:155`, `:172` | Table row "Holds without `shapeClearance`...". Option B "Express, digests, and goldens stay" | Records of the decision. Add one note after the table of section 4: "Note of <date>: the field is removed (always-on proposal), so option B no longer applies." |
| AO28 | `station-banks.md:55` to `:57`, `:60` to `:63` | Whole-lane locations, "With a depot and no bank, the audit checks only the pairs with a depot lane or a slot.", and "When the network sets `shapeClearance`, the audit checks each pair...", and "When the network sets `shapeClearance`, for a pair with a large class, the locations of a berth..." | `:55` to `:57`: state them as the rule when a tail bound is not finite. Delete `:60`. "The audit checks each pair, small pairs included, in each network.". `:62`: drop "then". `:63`: drop the condition. |
| AO29 | `shape-clearance.md:7` | Build record | Add: "Note of <date>: the field is removed, and parts 1 to 5 apply to each network (always-on proposal)." |
| AO30 | `shape-clearance.md:64`, `:72` | "When the network sets `shapeClearance`, rule A1 of the model applies.", and "A network without the field, project D included, keeps the envelope of this section and its requirements, and none of its rules depends on A1." | "Rule A1 of the model applies to each network.", and "Since <date>, D also uses the envelope reach and A1, and its evidence runs again (always-on proposal, section 5)." |
| AO31 | `shape-clearance.md:519`, `:547` | "When the network sets the field, the bank audit runs on each pair...", and "When the network sets `shapeClearance`, the envelope comes from the envelope reach..." | "The bank audit runs on each pair of each network, small pairs included (decision D12, option A).". Drop the condition. |
| AO32 | `shape-clearance.md:579` to `:583` (section 3.6, Gate) and `:793` (D7) | "The rule goes behind a network field...", and "The field, so that D, the digests, and the goldens stay." | Add after `:814`: "On <date>, the maintainer reversed D7: the rule has no gate, and the fields `shapeClearance` and `lateralMerges` are removed. D, the ring presets, and their goldens change (always-on proposal)." Leave section 3.6 and the table as records. |
| AO33 | `shape-clearance.md:804` to `:806` (D12 note) | "Option A of D12 runs the all-pairs audit only in a network that sets `shapeClearance`. A network without the field does not change, so option A does not reverse decision D." | Add: "Since <date>, D12-A runs the all-pairs audit in each network, so it reverses lane-end decision D for plain projects (decision AO-D7)." |
| AO34 | `shape-clearance.md:827` (D11 dependency), section 5.1 (`:753` to `:781`), section 7 rows `:860`, `:862` | "D stays unchanged only behind the field.". The evidence of D with the field unset. The gate-off and parity tests | Add one note after `:781`: "Note of <date>: without the field, D changes, and the rows of section 5 of the always-on proposal run again. The gate-off and parity tests of section 7 are removed." |
| AO35 | `lane-end-protection.md:392` | "The maintainer chose D." | Add: "Note of <date>: the always-on proposal runs the all-pairs audit with shape pairs in each network, so the gap of decision D is closed (decision AO-D7)." |
| AO36 | `shared-track-preset.md:25`, `:48`, `:52`, `:213`, `:330` to `:331`, `:832` to `:833`, `:850` to `:851`, `:934` to `:936` | "behind the `shapeClearance` field", and "A1 applies only when the network sets `shapeClearance`. A network without the field, project D included, keeps its envelope". The comparison table. The field-off evidence | Add one note after the status lines: "Note of <date>: the fields are removed, and S1, A1, and the compiled reach apply to each network. The text below records the design with the field." |
| AO40 | `shape-clearance.md:154` | "When the network sets `shapeClearance`, rule 2 of the group physics design also adds each lane at a node of the cluster..." | "Rule 2 of the group physics design also adds each lane at a node of the cluster..." `:155` stays: the counts of section 2.1 do not use rule 2. |
| AO41 | `shape-clearance.md:164` to `:165` | "When the network does not set `shapeClearance`, the envelope takes the largest front bound of the route, as it did when the probe ran.", and "With the field, the envelope comes from the envelope reach..." | "Before <date>, a network without the field had an envelope with the largest front bound of the route, as when the probe ran.", and "Since <date>, the envelope of each network comes from the envelope reach..." |
| AO42 | `shape-clearance.md:170` | "When the network sets `shapeClearance`, the index of decision S1 ... takes the anchor front of each lane ... in place of this bound." | "Since <date>, the index of decision S1 ... takes the anchor front of each lane ... in each network, in place of this bound." |
| AO43 | `shape-clearance.md:563` to `:572` (section 3.5, Places that change) | Each row has a branch "when the field is set" and a branch without the field | Add before the table: "Note of <date>: this table records the change behind the field. Since <date>, each branch for a network with the field applies to each network. Each branch without the field is removed, except the whole-lane reach of `validateBankGeometry`, which stays as the fallback when a tail bound is not finite." |
| AO44 | `shape-clearance.md:599` (section 3.7) | "The audit refuses fewer pairs, and its text for a refused pair does not change." | "In a network that ran the all-pairs audit before <date>, the audit refuses fewer pairs. In a plain network, it refuses more pairs, and the first refusal of an invalid network can change. Its text for a refused pair does not change." |

### 4.2 Applied amendment records

| ID | File and line | Proposed note |
| --- | --- | --- |
| AO37 | `shape-clearance-amendment-proposal.md:19` to `:20` ("Each new rule applies only when the network sets `shapeClearance`, and a network without the field does not change.") | After `:3`: "Note of <date>: the [always-on proposal](shape-clearance-always-on-proposal.md) removes the field, and each rule of SC1 to SC16 applies to each network. The conditional clauses below are records. The target documents carry the text of AO1 to AO36 and AO40 to AO44." |
| ST25 | `shared-track-amendment-proposal.md:45` ("Each changed rule applies only when the network sets `shapeClearance`.") | After the status lines: "Note of <date>: the [always-on proposal](shape-clearance-always-on-proposal.md) removes the `shapeClearance` field. Each changed rule of ST1 to ST15, with rule A1, applies to each network, as the second shared-track amendment proposal amends it. The conditions on the field below are records." |
| ST26 | `shared-track-amendment-2-proposal.md:39` ("Each changed rule applies only when the network sets `shapeClearance`, as in the first proposal.") | After the status lines: "Note of <date>: the [always-on proposal](shape-clearance-always-on-proposal.md) removes the `shapeClearance` field. The anchor front of each lane, the compiled envelope reach, and the oracle that reads it (ST16 to ST24) apply to each network. The conditions on the field below are records." |

### 4.3 User documents to update

These are not contracts, so slice 5 updates them:

- `docs/reference.md:35`, `:57` to `:88`, `:476`, `:518`, `:561`
- `docs/model.md:38` to `:39`, `:217`, `:232`
- `docs/operations.md:375` to `:379` (add the two members to the removed members), `:422` to `:426`, `:1007`
- `docs/user-guide.md:530`
- `docs/london-full.md:62` to `:64`, `:116` to `:122`, `:343`
- `docs/london.md:51`
- `docs/design/london-junctions.md:6` to `:7`, and `docs/studies/london-junctions.md:149` (the `-lateral-merges=false` flag)
- `README.md:169` to `:171`, `:201`, and a new index row for this proposal

## 5. Evidence to run again

"Routine" means a golden or a pin that moves in the commit that changes behavior, with the reason in the commit message.
"Qualification" means contract evidence that the maintainer reads before D is qualified again.

| Item | Why it moves | Command | Kind |
| --- | --- | --- | --- |
| Fixture tests that the all-pairs audit refuses | Section 9.1 | `go test -short ./...` | Routine (slice 1) |
| `TestTripBaseline` (`internal/sim/trip_baseline_test.go:90` to `:91`), `TestIncidentBaseline` | They passed in the forced-on run (section 9.1), so they do not move | `go test ./internal/sim -run '^(TestTripBaseline|TestIncidentBaseline)$'` | Routine |
| `TestGateOffTables` (`internal/sim/testdata/gate_off_tables.json`) | The `lateralMerges` false tables have no meaning. It failed in the forced-on run | Becomes a pin of the one table per network, or is deleted | Routine |
| `cmd/compare` identity tests (`TestNoRateOutputIsPinned`, `TestNoRateFormsArePinned`, `TestNoRatePlanOutputIsPinned`) | Behavior of the compare runs. The first two failed in the forced-on run | `go test ./cmd/compare` | Routine |
| `cmd/scenario` pins (`main_test.go:116` to `:130`, `:162` to `:173`) | The pins hash the generated project file, not behavior: the output and network hashes of `london-central` and `london-full` move in slice 3. The "only the shared preset sets the field" check goes | `go test ./cmd/scenario` | Routine |
| Network SHA-256 pins: `layout_audit_test.go:24` to `:32`, `london_full_test.go:188` to `:189`, `london_options_test.go:94` to `:95`, `london_full_shared_test.go:262` to `:273` | Section 3 | `go test ./internal/scenarios` | Routine |
| `london_express_parity.json` | D behavior changes | Repinned in slice 2 as the D behavior golden (AO-D4) | Routine |
| Parity of LondonCentral, LondonFull, and the shared preset | Must not move (slice 0) | `go test ./internal/session -run '^TestPresetBehaviorParity$'` | Qualification (SX1, E1) |
| Byte proofs (E5, A18, SX5, SX13) | Worst-case project loses two members | `mise run test:embedded` | Qualification record, sizes only |
| D audit and geometry (E2) | All-pairs audit with envelope reach on D | `go test ./internal/scenarios -run '^(TestLondonFullExpress|TestLondonFullExpressRoutes)$'` and `go run ./cmd/projectcheck` on D | Qualification |
| D oracle and envelope parity (E3) | S1 envelope, A1, berth rule 2, and the D6 oracle on D | The new and changed tests of section 5.1: `TestLondonExpressEnvelopeRun`, the D case of `TestEnvelopeSupportMixedSmall`, and `TestEnvelopeMatchesReference` | Qualification, with the coverage counts of section 5.1 |
| D checkpoints (E4) | Motion and envelope change | `go test ./internal/session -count=1 -run '^TestLondonExpressPhysicalCheckpoints$'`, with the D6 oracle of section 5.1 | Qualification |
| D consumers (E6) | The D topology JSON does not change, but changed motion can change the publication traffic and the work of the consumers | `go test ./internal/remote -count=1 -run '^(TestLondonExpressRemoteLoads|TestLondonExpressRemoteRecovery)$'`, and the Chrome check of E6 (`express-london-qualification-proposal.md:141`) on the final build | Qualification, decision AO-D4 |
| D cost (E7) | Validation and oracle cost, and the same consumer change as E6 | The server measurement of the E7 record, 4 cases, and the Chrome responsiveness check (`express-london-qualification-proposal.md:113`) on the final build | Qualification, decision AO-D4 |
| D incidents, standees (E8, S1) | Motion | `go test ./internal/session -count=1 -run '^(TestLondonExpressIncidentFixtures|TestSoakLondonExpressDriver)$'`, with the D6 oracle of section 5.1 | Qualification |
| London soak row (A15) | D motion, and the D6 oracle on D | `PODSIM_SOAK_PRESETS=london-full-express mise run soak` | Qualification |
| CI soak (A16) | Same | `mise run qualify` (`TestIncidentSoakCI`) | Qualification |
| Latency (A17, SX12) | Validation and oracle cost | `mise run qualify`, and the timed runs of the SX12 record on D and the preset | Qualification |
| Release soak (A16, A19, SX11) | D6 oracle on each preset. Ring and D motion | One timing seed per preset first, then `mise run soak:full` | Qualification |
| `rail-hub` rows (stage 7 A14, standee rows) | `rail-hub` motion changes (survey: 24 trips with the field, 22 without) | The release soak and `mise run qualify` | Qualification |
| Shared preset (SX rows) | No behavior change | `mise run test:shared-express`, with no golden change in slice 2 | Routine. Evidence that SX holds |

Shared preset: its checksum changes when the fields leave the JSON in slice 3.
Slice 0 adds a behavior golden that hashes the saved-state and stream JSON with the members `network.lateralMerges` and `network.shapeClearance` removed.
That golden must not move in slice 2 or in slice 3.

### 5.1 Test changes for the first qualification of D under the rule

D has not run the S1 envelope, rule A1, berth rule 2, or the D6 oracle before, so a rerun of its tests is not enough.
Slice 4 makes these changes:

| Test | Today | Change |
| --- | --- | --- |
| `TestLondonExpressPhysicalCheckpoints` (`internal/session/london_express_checkpoint_test.go:90`) | Restored continuation runs `Check` only. `f.tick` is not set. | Set `f.tick` to `LateralBodyCheckForTest` in the D fixture, as the shared fixture does (`london_shared_express_test.go:68`). The incident fixtures use the same fixture, so they get the oracle too. Today the script states are `bank-b`, `return`, and `parking-berth` (`:119`), and the incident fixtures add a fault at rest and an emergency journey. The restored continuations must include a stopped pod, a faulted pod, an emergency journey, and a pod on a join turn at a lane end. Add a checkpoint for each state that is missing. This is SC7 (`express-physical-encoding-contract-proposal.md:547`). |
| `TestLondonExpressIncidentFixtures` (`internal/session/london_express_incident_test.go`) | `Check` at each tick of the shared session (`:44`), and the checkpoints of the D fixture | Add the D6 oracle at each tick of the shared session. The restored copies get it from the fixture change above. |
| `TestLondonExpressEnvelopeRun` (new) | None for D | A mixed-fleet run of D with faults, emergencies, saves, and restored continuation. At each tick, for each pod, it compares the indexed and the reference constructions of the envelope, as `TestSharedExpressEnvelopeRun` does for the shared preset. |
| `TestEnvelopeSupportMixedSmall` (`internal/sim/envelope_support_test.go:176`) | Hook and compact station | Add a D case. Body-support containment is a separate test from the envelope equality of `TestEnvelopeMatchesReference` (`:753`), which already has D (`:706`). |

Coverage record: `TestLondonExpressEnvelopeRun` logs three counts.
These are the pod ticks where A1 drops a plane of the largest front bound, the envelopes where rule 2 adds a cluster lane, and the berth envelopes that hold a rule 2 lane.
The record gives each count.
A count of 0 for a case that the D geometry has stops the build (section 9.2).
A count of 0 for a case that the D geometry does not have is recorded as such.
A passing soak does not replace a count.
Slice 4 adds a `test:london-express` task that runs these tests, as `test:shared-express` does for the shared preset.

## 6. Editor

Today a draft with banks, large classes, maneuver groups, or depots already runs the server network check on each edit (`edit_geometry.go:107`, `checks.go:233`).
The London presets and D have maneuver groups and depots, so they already run it.
From slice 2, their check also builds the shape table and the envelope reach.
Validation took up to 1.25 times as long with the field (`docs/operations.md:426`), and 0.7 s natively on LondonFull (survey).
Plain drafts lose the local lane gap check (`laneConflict`, `edit_collision.go:127`) and run the server check on each edit.
These are the example, the ring presets, and authored projects up to 20,000 lanes (`project.MaxLanes`).
A failed server check rejects the edit (`edit_geometry.go:84`).

### 6.1 Measurement protocol

Slice 0 adds `TestDraftEditTimes` to `internal/editormodel`.
It skips unless `PODSIM_EDIT_TIMES=1` is set.

| Item | Definition |
| --- | --- |
| Networks | The example, `small`, `busy`, `parking-constrained`, `rail-hub`, `rail-hub-incidents`, `scale100`, `independent-banks`, `compact-station`, `london-central`, `london-full`, `london-full-express`, `london-full-shared-express`, and a plain network at the project limits |
| Edit | One geometry `moveNode` call. An accepted edit moves a node 0.5 m and keeps the network valid. A refused edit moves a lane to 3.0 m from another lane that shares no node. |
| Modes | Cold: a new engine for each edit, with no kept network. Cached: one engine, timed after a first edit. |
| Samples | 20 edits for each network, mode, and verdict. Each sample is the time of one call. |
| Statistics | The maximum and the p95 (nearest rank, the 19th of 20 values) of each set of samples. No benchmark aggregate. |
| Runtimes | Native, and wasm through `GOOS=js GOARCH=wasm go test -exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec" -run '^TestDraftEditTimes$' ./internal/editormodel` |
| Browser | In desktop Chrome on the editor page, 20 accepted and 20 refused edits on the example, `scale100`, `london-full`, and D, with the DevTools Performance panel. A sample is the time from the request message to the worker to its reply message. |
| Builds | The tip of slice 0, and the tip of slice 3 |
| Record | Host CPU model, core count, `GOMAXPROCS`, load average before and after, Go version, Node version, and Chrome version. Timed runs do not use `nice`. |

The forced-on audit refuses the limit network of `TestValidateAcceptsNetworkLimits` (section 9.1), so that network uses the slice 1 geometry.

### 6.2 Limits

The maintainer approved these limits on 2026-11-20.
Each limit is for one edit, in each mode and for each verdict, in wasm and in the browser.

| Network | p95 | Maximum |
| --- | ---: | ---: |
| The example, `small`, `busy`, `parking-constrained`, `rail-hub`, `rail-hub-incidents`, `independent-banks`, `compact-station` | 100 ms | 250 ms |
| `scale100` | 250 ms | 500 ms |
| `london-central`, `london-full`, `london-full-express`, `london-full-shared-express` | 1.25 times the tip value | 1.25 times the tip value |
| Plain network at the project limits | 5 s | 8 s |

A cold edit has 2 times the limit of a cached edit.
A browser sample has the wasm limit plus 50 ms.

When any network, the London presets and D included, is over a limit, the build stops (decision AO-D9).
Deferred validation then becomes its own design, and it is not part of this proposal.
This proposal has no debounce fallback.
A debounce lowers the number of checks, not the length of one check.
Also, a failed check rejects the edit today (`edit_geometry.go:84`), so a deferred check needs a new rule for an edit that the editor accepts and the server later refuses.

## 7. Build slices

Each slice builds and passes `go build ./...`, `go vet ./...`, `mise run lint`, `go fix -diff` on the changed packages, `go test ./...`, and `mise run test:web` on its own.

| # | Commit subject (draft) | Packages | Tests |
| ---: | --- | --- | --- |
| 0 | `session: Pin the behavior of the presets that keep it` | Tests of `internal/session` and `internal/editormodel` | New `TestPresetBehaviorParity` for LondonCentral, LondonFull, and the shared preset, with the two members removed before hashing. New `TestDraftEditTimes`. Record the tip values of section 6.1. |
| 1 | `sim: Move test fixtures clear of the all-pairs audit` | Tests of `internal/sim`, `internal/project`, `internal/editormodel`, `internal/scenarios`, `cmd/serve` | Each refused fixture of section 9.1 moves so that it passes the audit with shape pairs, and its pinned outputs change with it. Check each on a scratch tree with the field reads forced on. |
| 2 | `sim: Apply the shape clearance rule to each network` | `internal/sim`, `internal/editormodel`, `internal/session` (soak driver), tests of `internal/project`, `internal/scenarios`, `cmd/scenario`, `cmd/compare` | Each field read becomes unconditional, and the field-off branches go (section 1.2), except the whole-lane fallback (section 1.4). The fields stay in `sim.Network`, decoded and with no effect. The editor drops its local lane gap check. Field-off test arms, study arms, and `TestEnvelopeFieldOffMatchesBuilt` go. The routine goldens of section 5 move. The slice 0 parity golden and the shared preset tests do not move. Section 7.1 gives the refusal tests. `TestRepairTwoRefusedPairsInJSON` (section 2.2). |
| 3 | `sim: Remove the lateralMerges and shapeClearance fields` | `internal/sim`, `internal/session`, `internal/scenarios`, `internal/editormodel`, `cmd/scenario`, `web/editor_test.cjs`, tests of `internal/project` and `internal/remote` | Fields, digest entries 16 and 17, member golden lines, `NoLateralMerges`, `-lateral-merges`, byte proof lines. The members leave `internal/sim/testdata/scenario_networks.json.gz` (one network sets `lateralMerges`), `internal/sim/testdata/shared_express_project.json.gz` (both members), `internal/sim/testdata/gate_off_tables.json`, and `web/editor_test.cjs:892` to `:906`. Network hash pins of section 3. One test for each row of section 2.1, with both names and the values `true`, `false`, and `null`, pins the text and the precedence. The slice 0 parity golden does not move. |
| 4 | `session: Run the D6 oracle and the envelope checks on D` | Tests of `internal/sim` and `internal/session`, and `mise.toml` | The test changes of section 5.1, and the `test:london-express` task |
| 5 | `docs: Describe shape clearance for each network` | `docs/`, `README.md` | `mise run lint` (rumdl) |
| 6 | `contracts: Apply the always-on shape clearance amendments` | `docs/contracts/`, `docs/design/`, `docs/station-banks.md` | After maintainer approval of section 4. `mise run lint` |
| 7 | `contracts: Record the evidence of D under the always-on rule` | `docs/contracts/` | The qualification rows of section 5, the measurement of section 6 on the tip of slice 3, `mise run check` on the tip, and then the release soak. The records go in the D qualification proposal and in this proposal. |

Run the race task of each changed package once on the tip of slice 4: `mise run test:race:sim`, `test:race:session`, and `test:race:other`.

### 7.1 Refusal order in slice 2

Two things stay apart.

- The order of the decoders, scans, and checks does not change, and neither does their text.
  This is the order of restore, scan, and stream checks that AGENTS.md makes part of the format, and the order of the checks of project validation.
- Some validation outcomes change on purpose.
  A plain network that passed can fail the all-pairs audit, so its first refusal changes.
  A pair that the whole-lane reach refused can pass with the envelope reach.
  Each such change has a test that pins the new outcome.

| Test | Today | Slice 2 |
| --- | --- | --- |
| `TestValidateReachRefusalComesLast` (`internal/project/config_test.go:1006`) | The island crossing at 24 m from the join gives a held `PlaneReachError`, and three subtests show that it does not hide the Express registry, passenger station, or fleet refusal. | The forced-on rule gives no refusal for this fixture. Move `island-cross-east` and `island-cross-west` from `Y: 24` to `Y: 10`. On the forced-on tree on 2026-11-20, this gave the same held refusal text, and the three subtests passed. At 2 m to 6 m, the refusal is not held, and at 16 m or more there is no refusal. Keep each assertion. |
| `TestLargeDraftLaneReachConflict`, `TestLargeDraftLaneReachConflictOrder`, `TestLargeDraftLayoutReachConflictComesLast`, `TestLargeDraftServerReachRefusalComesLast` (`internal/editormodel/large_geometry_test.go:413` to `:536`) | Reach conflicts and their order in a draft | All four fail on the forced-on tree, because the envelope reach gives no conflict for their fixtures. Move each fixture so that the reach conflict or the held refusal stays, as for the project test, and keep each competing-error assertion. The fixture moves are not tested yet. When no move keeps a held refusal, stop (section 9.2). |
| `TestPresetWithCapacity`, invalid variants (`internal/scenarios`) | Each variant pins a later refusal | The audit refusal now comes first. Pin the new first refusal, and add a variant that has only the later fault, so that the later check keeps a test. |
| New `TestRestoreRefusesAuditedProject` (`internal/session`) | None | A saved state whose project has two small lanes 3.0 m apart that share no node. Restore gives `invalid_state` with `saved project: invalid project scenario: nonincident paths ...`. A second case adds a session member that passes the state decode and fails only a restore check after `restoreProject`, and the project refusal still comes first (`persist.go:488`). |

### 7.2 Decisions on the slice 2 stops

On 2026-11-20, slices 0 and 1 found section 9.2 stops for slice 2.
A study on a forced-on copy found no oracle stop: each failure near 12 m came from the 12 m test helper `checkTraffic`, a field-off arm, or a pinned trace, and `Check` and the D6 oracle stayed clean.
The maintainer delegated the decisions below to the agent on 2026-11-20.
Each one is decided by the agent on that delegation, and each one waits for maintainer review.

| ID | Stop | Decision |
| --- | --- | --- |
| AO-S1 | `TestRestoreSeparatesTravelingPods` and the "berths within clearance" case of `TestRestoreFailsForInvalidState`: no valid project gives two pods closer than the clearance on paths that share no node | Keep both tests. Reach each refusal through the existing `newFleet` parameter of `restorePhysical`, with a berth node that moves after preparation. The refusal order and text stay. |
| AO-S2 | `TestFaultAtDepartureSeparatePlane` | Rewrite it as an assertion of the location of the faulted pod. |
| AO-S3 | `laneEndNetwork`, the "effective mask exclusion" case of `TestLargeGeometryBerthIncidentAudit`, and `TestPlatoonGeometry/close_fold` | Use the fixture changes of the study, which pass on both trees, as a follow-up commit to slice 1. |
| AO-S4 | Section 7.1, editor row | Under the rule, the local reach check of the editor (`laneConflict`, `heldConflictError`) cannot fire, and slice 2 removes it. Slice 2 removes the local-check arms and `TestLargeDraftLaneReachConflictOrder`, keeps the server arm of `TestLargeDraftLaneReachConflict`, keeps `TestLargeDraftServerReachRefusalComesLast` with a moved fixture, and removes `TestLargeDraftLayoutReachConflictComesLast`, because with the rule it repeats the server order test. The held `PlaneReachError` path stays live for large pairs whose envelope reaches overlap, and the moved fixtures of the `TestLargeGeometry*Reach*` tests and of `TestNetworkCheckMatchesValidation` land in slice 2. The "same lane far from the join" case changes to no refusal, and a test pins that outcome. |
| AO-S5 | Editor goldens `geometry.json` and `checks.json` | Geometry: check the parity of the transform through `geometryDraft.change` with no server check, so the 49 fixtures need no golden change. Checks: one scripted control point on the return lane of the ring, and a targeted move for each fixture that it does not fix. |
| AO-S6 | A plain draft with a station structure defect | Accept that each geometry edit of such a draft is refused, as it is for the London presets today. The repair route of section 2.2 applies. |
| AO-S7 | `TestPlatoonEntryLaneRun/36` reaches no tick with two coasting heads | Slice 2 finds a fixture that reaches the case. If none does in a bounded search, it reaches the state through a test entry, as `TestRestore*` does with `newFleet`. Stop only if neither works. |
| AO-S8 | The 12 m test helper `checkTraffic` (49 call sites) | Replace it with `Check` and the D6 oracle in slice 2. |
| AO-S9 | New pins of slice 2 | Pin the corridor headways (8.633 s, 4.933 s, 2.993 s), the parallel chains in place, and the sampler trace. |

## 8. Decisions for the maintainer

The maintainer decided each row on 2026-11-20.
The options stay for the record.

| ID | Question | Options | Decision |
| --- | --- | --- | --- |
| AO-D1 | Old projects, saves, topologies, and drafts with either member | A: refuse through the unknown-member checks that exist today, with the text of each entry point. B: a named refusal, for example `network member "shapeClearance" is removed`, from a scan before the typed decode. C: accept and ignore the members. | Decided 2026-11-20: A. Section 2.1 gives the text and the precedence of each entry point, and slice 3 pins each one in a test. B adds a refusal to the ordered list of each decoder. C breaks the unknown-member rule (`docs/operations.md:374` to `:379`). |
| AO-D2 | Version bumps | A: keep `stateVersion` 12 and `StreamVersion` 10. B: bump both, with the golden files and the version text of AGENTS.md. | Decided 2026-11-20: A, by `docs/operations.md:374`. |
| AO-D3 | Test-only switch for the old isotropic rule, for studies | A: none. B: an exported test entry that turns off parts 3 to 5. | Decided 2026-11-20: A. The forced-on run found no test that needs the old rule, other than field-off arms and study probes that go with the field. |
| AO-D4 | Name and status of D | A: D keeps its name, is not qualified from slice 2 until the rows of section 5 pass, and keeps its earlier Chrome checks. B: as A, and the maintainer repeats the Chrome checks of E6 and E7 on the final build. C: requalify D as a whole, as a new project. | Decided 2026-11-20: B. Changed motion can change the publication traffic and the work of the consumers, also with the same topology. `london_express_parity.json` stays as the behavior pin of D, repinned in slice 2. |
| AO-D5 | Order of evidence runs | A: slices 0 to 6 land locally. Then the D rows, `test:shared-express`, `test:london-express`, and `mise run check` on the tip. Then one timing seed per preset. Then `soak:full`. B: D rows after slice 2, before slice 3. | Decided 2026-11-20: A. |
| AO-D6 | Commit shape | A: slice 2 makes the rules unconditional with the members decoded and inert, and slice 3 removes the members. B: one commit. | Decided 2026-11-20: A. |
| AO-D7 | Lane-end decision D for plain projects | A: the all-pairs audit in each network, and slice 1 moves the refused fixtures. B: keep the audit scope of today for plain networks (D12-B there). C: stop. | Decided 2026-11-20: A. |
| AO-D8 | Release soak wall time | A: run one timing seed per preset first. If 2 seeds do not fit the 2-hour limit, the maintainer chooses fewer seeds, a higher limit, or a split run. B: run `soak:full` directly. | Decided 2026-11-20: A. The last release soak took 5,812 s of 7,200 s, and the D6 oracle now runs on `rail-hub`, `london-central`, and D too. |
| AO-D9 | Editor cost | A: measure with the protocol and the limits of section 6. When any limit fails, stop, and design deferred validation as a separate change. B: a debounce fallback in this proposal. | Decided 2026-11-20: A. |
| AO-D10 | Editor operations on a draft with a removed member | A: each editor operation refuses the draft, field edits included. B: only validate, checks, and geometry edits refuse it. | Decided 2026-11-20: A. |

## 9. Risks and stop conditions

### 9.1 Forced-on test run

On 2026-11-20 at `b2114516`, a scratch copy of the tree ran with each `ShapeClearance` read in `internal/sim`, and `hasShapeClearance` in `internal/editormodel`, forced to true.
The command was `go test -short -vet=off ./...`.
A `t.Fatal` in `TestRetentionSafetyCertification` stopped `internal/sim`, so `internal/sim` ran again with `-skip '^TestRetentionSafetyCertification$'`.
`cmd/compare` skips its identity tests under `-short`, so they ran again without it.

| Package | Top-level tests that failed | Of these, with a new audit refusal |
| --- | ---: | ---: |
| `internal/sim` | 165 | 99 |
| `internal/editormodel` | 20 | 6 |
| `internal/project` | 11 | 9 |
| `internal/scenarios` | 1 (`TestPresetWithCapacity`, invalid variants) | 1 |
| `cmd/serve` | 1 | 1 |
| `cmd/compare`, without `-short` | 2 (`TestNoRateOutputIsPinned`, `TestNoRateFormsArePinned`) | 0 |

- 116 of the 200 failures have a new audit refusal, with 41 distinct refused pairs, each at 12 m.
- The refused fixtures include berth fans, return lanes, the limit networks of `TestValidateAcceptsNetworkLimits` and `TestValidateLimitsEncodedSize`, and fixtures made from the example network.
- The other failures are field-off arms, tests of the local editor lane check, envelope comparisons with the old construction, 12 m helper checks, and moved behavior pins.
- One refusal went away: `TestValidateReachRefusalComesLast` expects a `PlaneReachError` that the envelope reach does not give for its fixture (section 7.1).
- `TestTripBaseline`, `TestIncidentBaseline`, `cmd/scenario`, and `internal/session` passed, so no preset was refused.
- This agrees with the C2 build of the [lane-end protection](../design/lane-end-protection.md#11-build-attempt-and-decision) design, which refused about 20 fixture builders.
- Design section 3.7 says "The audit refuses fewer pairs".
  That holds only for networks that already ran the all-pairs audit.
  On plain networks, the audit refuses more pairs, and the first refusal of some invalid networks changes (`TestPresetWithCapacity`).
  These are changed validation outcomes (section 7.1).
  The order of the decoders, scans, and checks, and their text, do not change.

### 9.2 Stop conditions

| Condition | Action |
| --- | --- |
| A preset or the example project fails validation with the rule | Stop. Report the pair and its sweep result. |
| A fixture of slice 1 cannot pass the audit without losing what its test checks, or a fixture of section 7.1 cannot keep a held refusal | Stop for that test, and bring it to the maintainer. |
| The slice 0 parity golden moves in slice 2 or 3 | Stop. A preset that must keep its behavior changed. |
| Any oracle violation: `Check`, the D6 oracle, or the envelope parity test, on any preset or run | Stop. |
| A late record in a soak, or a failed latency gate after one rerun alone | Stop, and report it with the trace, as for SX11. |
| The release soak does not fit the 2-hour limit | The maintainer chooses, by decision AO-D8. |
| An edit time above a limit of section 6.2, on any network, the London presets and D included | Stop. Deferred validation becomes its own design (decision AO-D9). |
| A byte proof over a cap | Stop. No cap changes without a decision. |
| The order of the decoders, scans, and checks of restore, scan, stream, or project validation changes, or their text changes | Stop. AGENTS.md makes the restore, scan, and stream order part of the format. A changed first refusal because the audit now refuses a network is a changed outcome, and section 7.1 pins it. |
| A D coverage count of section 5.1 is 0 for a case that the D geometry has | Stop, and add a fixture that reaches the case. |
