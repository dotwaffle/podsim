# Restore speed and platoon link proposal

Status: decisions made, not built.
Slice 0 ran, and a stop of section 8 fired (section 6.1); the trace of section 6.2 answers it.
On 2026-11-22, slice 1 fired a stop on the parity goldens, and the maintainer amended sections 5.3, 5.4, and 6.2.
On 2026-11-21, the maintainer decided RL-D1 to RL-D7 and answered Q3 (section 7 and section 9).
No row of section 5 is applied, and no code changes before the build.
Date: 2026-11-21.
Line references are for `4c1eaf23`.
No test, build, or measurement ran for this draft.
Each count and each byte number below is an estimate from the source, and slice 0 measures it.

## Summary

The maintainer asked for a restore that keeps the speed of each pod and its platoon link.

The source does not support the premise that a clean physical restore drops platoon links:

- A save keeps each link in the `platoon` member of the follower (`internal/sim/state.go:147` to `:150`, `:542`, `:559` to `:569`).
- The physical tier links the pods again before it places them (`internal/sim/state_physical.go:643`, `:850` to `:857`).
- `TestPlatoonRestore` requires the same link count after a restore (`internal/sim/platoon_test.go:641`, `:653`).
- `docs/operations.md:281` says that a pod keeps its link.

What a restore loses is the speed of each pod and the forward grants past a derived frontier.
Section 2 lists the paths that can lose a link.
The link symptom is a separate track: the answer to Q1 and a report of slice 0.
It does not gate the speed change.

The speed reset is real and is approved contract text (`express-physical-encoding-contract-proposal.md:155` to `:164`).
A speed cannot come back alone.
The motion kernel sets the safe speed from the reserved distance, so a kept speed needs a kept frontier.

This proposal gives three options (decision RL-D1):

- A: save the speed and the reservation frontier, and prove the stopping frontier at the restore.
- B: save the speed only, and clamp it to what the rebuilt frontier can stop from.
- C: keep the current rule, and correct the record of the symptom.

The recommendation is A, on the speed request alone: each restart now stops all traffic.
It holds if slice 0 passes the stops of section 8.

## Scope

In scope: the physical tier of the restart restore, the saved pod members, the refusals and the link checks of that restore, the saved-state version, and the contract rows and evidence that assume speed 0.

Not in scope:

- The logical tier, which puts each pod at a berth.
- The stream, the HTTP state, and their versions.
- A rewind, which uses a clone.
- The trajectory after a restore: the proposal keeps speed and grants, not an identical future.
- Large virtual platoons, which stay prohibited.
- A migration of version 12 files.
  A version 12 file gets `unsupported_version`, and the server moves it aside and starts a new session (`docs/operations.md:376` to `:378`), as `AGENTS.md` allows until v1.

## Conventions

- Decision IDs have the prefix RL-D, and amendment IDs have the prefix RL-A.
- `<date>` is the date of the approval.
- "The frontier" is `reservedThrough` of a traveling pod: the last block that it reserved.
- "The derived frontier" is the `through` that the restore derives today: `reservationEnd(&v.blocks, current)` (`state_physical.go:825`), after the depot step of `:830` to `:836`.
  The depot step can lower it to the depot entry block (`:834`), so it can be below `reservationEnd(&v.blocks, current)`.
- `b` is `maximumBraking` of the class (`internal/sim/motion_envelope.go:13` to `:15`).

## 1. Current behavior

### 1.1 Where the speed becomes zero

No code sets the speed to zero at a restore.
`SavedPod` has no speed member (`internal/sim/state.go:97` to `:160`).
`decodePod` builds each vehicle without a speed, so it is zero (`state_physical.go:345`, `:351` to `:357`).
`placeTravelingPod` then sets the frontier to the derived frontier (`state_physical.go:825` to `:836`), claims the footprint, and keeps the past resources.
The logical tier puts each pod at a berth (`internal/sim/state_logical.go:34`).

A faulted traveling pod restores with `faultCap = distance` (`internal/sim/state_faults.go:328`).
Thus a braking fault restores as stopped, as the incident suspension contract records (`incident-suspension-contract-proposal.md:1161`).

### 1.2 Which restores this affects

Only the restart restore of the saved state calls `RestoreState` in production: `restoreSimulation` (`internal/session/persist.go:267` to `:271`), from `:568`.
A search of the non-test Go files, with the worktrees left out, finds no other caller, also under `cmd/`.
A rewind installs a clone of the save point, so it keeps speed and grants (`internal/session/checkpoint.go:61`).
The stream carries `speed` for display only (`internal/session/stream_motion.go:12` to `:16`), and no client restores a simulation from it.
Thus this proposal changes the saved state, and it does not change the stream or the HTTP state.

### 1.3 Why the contract chose zero speed

The rule came with the first persistence on 2026-09-23 (`c94111b8`): "The pods keep their lane positions and start again at speed 0."
The Express contract (`27981fe3`, 2026-10-03) kept it and forbade a new member: "No new ordinary speed or stopping-ownership certificate enters the saved state" (`:164`).
The group design records the effect: "Future track grants can differ because saves do not retain speed" (`docs/design/group-physics-design.md:129`).
No document records a longer argument.
The source gives this one:

1. `move` limits a pod to the end of its frontier, or to its platoon cap when that is less (`internal/sim/traffic.go:891` to `:895`).
2. `moveStep` commands at most `sqrt(b² dt² + 2 b a) - b dt`, where `a` is the distance to that limit (`internal/sim/move_step.go:16` to `:20`).
   After each step, `a ≥ v² / 2b` holds for the new speed `v`.
   This step is my derivation from the kernel formula, and no document states it.
   That is the rule "A pod always owns track to stop at this rate" (`motion_envelope.go:13` to `:14`).
3. At speed 0 the stop point is the position.
   The restore then needs only the footprint at the position, which it derives from route geometry.
   That proof is local: it does not depend on the order of placement or on grants of other pods.
4. With a speed above 0 and the derived frontier, `a` can be less than `v² / 2b`.
   The kernel then commands a speed below `v - b dt`, which brakes harder than the class rate.
   The contract forbids that (`:162`, and "No boundary speed clamp, position snap, or reduced authored speed substitutes for controlled braking", `:233`).
5. The link check depends on speed 0: "A restored pod has no speed, so its stop point is its position, and its cap is not behind it" (`state_physical.go:909` to `:911`).
   `platoonCaps` caps a follower at the stop point of its predecessor less the clearance (`internal/sim/platoon.go:735` to `:752`).
   At speed 0 the position gap check of `savedLink` (`state_physical.go:920`) is the same as the stop-point rule of `planLink` (`platoon.go:705` to `:706`).
   At a speed above 0 it is not.

### 1.4 Evidence that assumes zero speed

- E4 of the Express contract: "Speed resets and rebuilt forward grants are reported apart" (`express-physical-encoding-contract-proposal.md:524`).
- SX4 of the shared-track qualification: "Each restore sets the speed of each pod to zero" (`express-shared-track-qualification-proposal.md:233`), and the table of speed resets and forward grants (`:239` to `:244`).
  At the first platoon link, 22 pods reset, 38 forward grants stayed, and 51 were not rebuilt.
- The same E4 text in `express-london-qualification-proposal.md:145` and `shared-track-amendment-proposal.md:565`.
- The incident soak restores through the session path.
  Its warm restore requires the physical tier (`internal/session/incident_soak_test.go:377` to `:420`), and its Persistence gate restores each save (`:591`).
  Neither compares pod speeds, so the change should not move them, but slice 4 runs them.
  `incident_soak_test.go:2129` checks that a restored faulted pod does not move, which RL-D3 A keeps.
- `restoreEmergencies` (`internal/sim/state_emergencies.go:263`) reads no speed, so the incident emergency contract gets no row.
- `docs/studies/experimental-policy-restarts.md:35` says "restored speed", but that is the playback speed (`internal/statestore/policy_restart_test.go:385`), not a pod speed.
- About 30 test files in `internal/sim` and `internal/session` call `RestoreState`.
  Slice 1 reads each one for a speed 0 assumption after the restore, for example `internal/sim/express_speed_test.go:73`.

## 2. Paths that can lose a link

Each path below is in the source.
None of them is the speed reset.

| Path | Source | Effect |
| --- | --- | --- |
| The save has no link | `state.go:575` to `:581` | The predecessor does not travel, or the saved route holds no lane of the run. The follower then holds no resource of a pod ahead. |
| The predecessor is demoted | `state_physical.go:781` to `:783` | The follower is placed without a link. |
| The follower cannot claim its blocks with the link | `state_physical.go:853` to `:855`, `:789` | The follower is demoted to a berth. |
| The follower is closer than the clearance | `state_physical.go:920` | The physical tier fails, and the logical tier puts every pod at a berth. |
| The project has platooning off | `internal/session/persist.go:572` to `:576` | The link drains and ends. |
| The link was draining | `platoon.go:418` to `:445` | It drains after the restore too, as `docs/model.md:303` says. |

There is also one effect in the other direction, which is my reading of the source and not a measurement.
At speed 0 each traveling pod is slow, so `canLink` accepts it (`platoon.go:551`).
A restore can then form links on its first ticks that the live run does not form.

The loss was seen one tick after a restore, not at the restore itself (Q1, section 9).
Slice 0 reproduces it before any format change.

## 3. Options

### 3.1 Option A: keep the speed and the frontier

The save adds two members to each traveling pod, both omitted at zero:

- `speed`: the speed in m/s.
- `reservedAhead`: the frontier less the derived frontier, in blocks (decision RL-D2).

The restore sets `through` to the derived frontier plus `reservedAhead`.
It claims the footprint to `through` with the current rules, and a conflict demotes the pod, as today.
Then it checks the stopping frontier of the pod:

- The limit is the end of block `through`, the platoon cap, and the reversal limit, as in `move`.
- The pod must stop before that limit at rate `b`: `distance + v² / 2b ≤ limit`, within `restoreTolerance`.
- The pod must reach each lower speed limit ahead at rate `b`.
- A pod that fails a check is demoted, as a pod with a resource conflict is.

A restored link also checks the stop points, with the rule of `planLink`.
The follower stop point plus the clearance must not pass the stop point of its predecessor.
The position check of `savedLink` stays first, with its text, and the new check comes after it (decision RL-D5).

A malformed value is a refusal of the whole file, not a demotion (decision RL-D4).
A faulted pod keeps the current rule (decision RL-D3).
A pod at a berth must have neither member.

The coast ceiling and the platoon cap need no member, because each tick derives them again (`internal/sim/coast.go`, `platoon.go:735`).
The motion has no acceleration state: the kernel reads only the speed and the limit.
A waiting pod keeps `pending = through + 1`, as today.

The trajectory after a restore can still differ from the live one.
`restoredDistance` can move a distance by up to `restoreTolerance` (`state_physical.go:962`).
Slice 1 review found that its snap to a block end within `restoreTolerance` can put a moving pod past a release distance that the live pod has not reached, so that the restore drops a cell that the live pod owns.
On 2026-11-22, the maintainer approved a separate drift tolerance of 1e-9 m for that snap, 500 times the largest drift measured (2.1e-12 m).
The other comparisons keep `restoreTolerance`.
The lane-limit demotion runs before the stop-point check of a link, after its position check, so that an overspeed follower demotes alone and does not fail the physical tier.
The proposal claims the same speed and the same frontier, and not an identical trajectory.

Costs and risks:

- Two new members, a new refusal check, a new link error text, and saved-state version 13.
- The depot rules change `through` at a depot entry (`state_physical.go:826` to `:836`).
  A saved frontier must agree with them, and slice 0 checks every depot entry of the qualification runs.
- If some live pod has a frontier below the derived frontier, `reservedAhead` is negative, and RL-D2 changes.
- The shared-track and Express restore evidence (E4, SX4) runs again.
- Five approved contract files get rows or notes (section 5).

### 3.2 Option B: keep the speed, and clamp it

The save adds `speed` only.
The restore keeps the derived frontier.
It sets the speed to the smallest of the saved speed, the lane limit, the speed that can stop before the limit of section 3.1 at rate `b`, and the speed that reaches each lower limit ahead.
The proof holds by construction, and no frontier is saved.

Costs and risks:

- The derived frontier ends at most one lane boundary and one junction run ahead of the pod.
  A small pod at 14 m/s needs 49 m to stop, and an Express pod needs about 85 m at 1.15 m/s².
  So most fast pods are clamped, and many to a low speed.
- The clamp is a jump in speed at the restore boundary, as the reset is.
  It needs the same contract rows as A.
- Forward grants are still rebuilt by admission after the restore, as SX4 reports today.
- It needs one member and one refusal, and saved-state version 13.

The variant in the brief, "keep links only and restore followers at the leader speed", is not a separate option.
The links are kept today.
A follower at the speed of its predecessor with the derived frontier fails the stop check of section 3.1, so that variant is B for followers only.

### 3.3 Option C: keep the current rule

No format, contract, or pin changes.
Slice 0 still runs, to find the link loss of section 2, and its result goes to the maintainer.
A note on SX4 can record that a restore keeps each saved link.

Costs:

- At each server restart, all traffic stops and starts again.
  An Express pod needs about 11 s to reach 14 m/s at 1.3 m/s².
- Forward grants are rebuilt, and new links can form at the restore.

### 3.4 Effect by option

| Effect | A | B | C |
| --- | --- | --- | --- |
| Saved pod members | `speed`, `reservedAhead` | `speed` | None |
| `stateVersion` | 13 (RL-D6) | 13 (RL-D6) | 12 |
| `StreamVersion`, `StateMediaType` | No change | No change | No change |
| New refusals | Section 4.2 | Section 4.2 rows 1 to 3 and 5 | None |
| New physical-tier error text | Stop-point link check | None | None |
| Saved-state growth, widest, estimate | About 56 bytes for each traveling pod: 33,600 bytes for 600 Express pods, 56,000 bytes for 1,000 plain pods | About 33 bytes for each traveling pod | None |
| Speed after a valid restore | The live speed | The clamped speed | 0 |
| Forward grants after a valid restore | The live grants | Minimal frontier | Minimal frontier |

The widest `speed` is `,"speed":4.9406564584124654e-324`, 33 bytes.
The widest `reservedAhead` is assumed to have 6 digits, because the restore budget is at most 256,000 cells (`docs/operations.md:276`), so `,"reservedAhead":255999` is 23 bytes.
The Express proof has about 29 MB of headroom (`docs/operations.md:226`), so the growth is small.
The plain headroom is not in the documents, and slice 1 measures it.

## 4. Format and refusals (option A)

### 4.1 Saved members

| Member | Type | Rule |
| --- | --- | --- |
| `simulation.pods[].speed` | number | Omitted at 0. Finite, at least 0, at most the speed limit of the lane of the pod. Only a traveling pod can have it. The export omits it for a faulted pod, and the restore ignores it for a faulted pod (RL-D3, Q3). |
| `simulation.pods[].reservedAhead` | number | Omitted at 0. An integer from 0 to the number of blocks of the saved route. Only a traveling pod can have it. |

The members list `state_v13_members.txt` gains both paths.

On 2026-11-22, a boundary sweep found that the export and the restore of a trimmed route can disagree by one float step.
The live route sums its lane lengths in order from 0 (`routeBlocks`), and the restore sums the trimmed lanes from 0.
A based route frame would remove this, but it changes about 20 live-path sites, so the maintainer chose a bounded residual instead.
The sweep found the restore differs from the live owners in two ways:

- A moving pod within 1e-9 m of a block end restores on the block end, and releases what the live pod releases there.
- A pod exactly on a release distance on a trimmed route can keep a cell that the live pod has released, for one more tick or until it moves.

`TestRestoreBoundarySweep` counts both kinds and refuses any other difference on its fixtures.

Review round 4 found three more cases on built geometry, outside the fixtures of the sweep:

- A pod at rest exactly on a release distance keeps a cell, so the restore can demote another pod with a valid grant through that cell, or hold a follower while the pod stays at rest.
- A backward snap to a block end can give a moving pod a cell that it has released, also on a route that is not trimmed.
- A snap can move a pod back across its origin tail on a trimmed route, so the restore demotes it.

The trim, the snap, and the origin-tail check are older than this proposal, and the snap was then 1e-6 m wide, so these cases are pre-existing.
This proposal narrows them and does not fix them.
On 2026-11-22, the maintainer moved them to a separate follow-up for exact restore coordinates.
The speed and the frontier stay exact, as section 3.1 claims.
A traveling pod at speed 0 with no grant past the derived frontier saves neither member, so its saved form does not change.

### 4.2 Refusals

The new check `checkSavedMotion` runs after `restoreInputChecks` (`state.go:345` to `:359`) and after `checkSavedFaultFootprints`.
Thus each existing refusal keeps its place and its text, and `TestRestoreInputCheckOrder` (`internal/sim/state_test.go:258`) does not change.
The session decoder refuses only a `null` member.
It gives the sim the presence of each member, so that rule 2 also refuses an explicit 0, in the order of this table.
The check reads the pods in saved order, and for each pod it applies these rules in this order.
The texts are drafts for approval.

| # | Condition | Error text (draft) |
| ---: | --- | --- |
| 1 | `speed` is not finite, or is negative | `pod <id> has speed <v>, not a finite value of 0 or more` |
| 2 | A pod that is not traveling has `speed` or `reservedAhead` | `pod <id> is not traveling and has a speed or a reservation ahead` |
| 3 | `reservedAhead` is negative | `pod <id> reserves <n> blocks ahead, not 0 or more` |

A speed above the speed limit of the lane of the pod is not a refusal.
A live lane crossing can publish a speed above the limit of the new lane by rounding: review found 8e-15 m/s on a 5 m/s lane and 3.25e-6 m/s on a slow lane at a long route distance.
The physical tier demotes a pod whose speed is more than the limit of the lane at its saved route index plus 1e-3 m/s.
That margin is more than 10 times the largest rounding excess for any practical route length, and less than one tick of braking of any class (Express: 1.15 m/s² / 30 = 0.038 m/s).
The first tick after the restore commands at most the lane limit, so an accepted excess costs one tick of deceleration below the braking rate.
The maintainer approved this change of rule 4 on 2026-11-22.
A `reservedAhead` past the saved route, a failed stop check, or a conflict demotes the pod in the physical tier.
These are not refusals, because the derived frontier and the route come from the network, as the existing demotions do.

On 2026-11-22, review round 5 found that a saved frontier can conflict with the extra cell of the pre-existing residual, so that the restore demoted a pod that the earlier restore kept.
The maintainer approved a fallback for each pod.
Wherever this proposal says that a motion check demotes a pod (a saved frontier that is not valid, a conflict through the saved frontier, the stop check, or the lane limit), the restore first places the pod as before this proposal, at speed 0 with the derived frontier.
It demotes the pod only when that placement also fails.
A pod placed earlier can still take a cell from a later pod, and a leader that falls back can fail the stop-point check of its moving follower, so a fallback for each pod alone is not enough.
Thus, when the restore still demotes a pod that the restore without the saved speeds and leads keeps, or fails the physical tier where that restore passes, the whole save restores without the saved speeds and leads, as before this proposal.
The second restore runs only when the first demotes a pod or fails the physical tier.
On 2026-11-22, after review round 6, the maintainer approved that the second restore is the restore before this proposal: no saved speeds or leads, and the block-end snap at `restoreTolerance`.
When both restores fail, the outcome of the second restore decides between a refusal and the logical tier.
Thus no pod restores worse than before this proposal.

The new physical-tier error is the stop-point check of section 3.1.
Like the gap check of `state_physical.go:920`, it fails the first physical restore.
The second restore has no saved speeds, so it never fails this check, and its outcome decides the tier.
Thus the text below appears only inside the first restore, and `RestoreState` gives it to no caller.
Its text is `the stop point of the pod is <d> m behind the stop point of its platoon predecessor, less than the clearance <c> m`.

## 5. Contract text, documents, pins, and gates

### 5.1 Contract text

Each row needs maintainer approval, as `AGENTS.md` requires for an approved contract.
A row on an applied record adds a "Note of <date>" line, and does not rewrite the record.

| ID | File and line | Current text | Proposed text |
| --- | --- | --- | --- |
| RL-A1 | `express-physical-encoding-contract-proposal.md:155` to `:156` | "Ordinary `ExportState` does not save speed or the exact future reservation frontier. A physical restore resets ordinary speed to zero and reconstructs forward grants from route geometry." | "Ordinary `ExportState` saves the speed and the reservation frontier of each traveling pod. A physical restore keeps both when it proves the stopping frontier of the pod. A faulted pod restores at speed zero." |
| RL-A2 | Same file, `:158` to `:159` | "Classify the speed reset and forward-grant differences in restore evidence. Do not claim identical speed or trajectory across that boundary." | "Report each pod that the restore demotes. Do not claim an identical trajectory across that boundary." |
| RL-A3 | Same file, `:161` | "The restored zero-speed stopping frontier must be owned continuously." | "The restored stopping frontier, at the maximum braking rate of the class from the restored speed, must be owned continuously." |
| RL-A4 | Same file, `:164` | "No new ordinary speed or stopping-ownership certificate enters the saved state." | "The saved speed and frontier are the only new ordinary motion members. The restore checks them against the network, the class rates, and the other pods." |
| RL-A5 | Same file, `:488` (Motion and ownership) | "zero-speed stopping frontier" | "stopping frontier, also at a restore" |
| RL-A6 | Same file, `:490` (Physical restore) | "Classify speed reset and reconstructed forward grants." | "Keep speed and forward grants, except for a faulted pod." |
| RL-A7 | Same file, `:524` (E4) | "Speed resets and rebuilt forward grants are reported apart." | Add: "Note of <date>: a restore keeps speed and forward grants (restore speed proposal). The report counts the kept speeds and finds no rebuilt grant." |
| RL-A8 | Same file, `:33`, `:62`, `:248` | "saved-state version 12" | "saved-state version 13" (RL-D6) |
| RL-A9 | `express-shared-track-qualification-proposal.md:233` (SX4) | "Each restore sets the speed of each pod to zero, ..." | Add the note of RL-A7 after `:244`, with the rerun table. |
| RL-A10 | `express-london-qualification-proposal.md:145`, `shared-track-amendment-proposal.md:565` | The E4 text of RL-A7 | Add the note of RL-A7. |
| RL-A11 | `incident-suspension-contract-proposal.md:189`, `:1161` | "A traveling pod restores at speed 0 ..." and "... as every traveling pod restores at speed 0 ..." | Add: "Note of <date>: other traveling pods keep their speed and frontier at a restore (restore speed proposal). A faulted pod still restores at speed 0 with `faultCap = distance`." Under RL-D3 B, this row is a rewrite of F-rows instead, and needs its own review. |

Option B changes RL-A1, RL-A3, RL-A6, and RL-A7 to say "clamped speed" and "derived frontier", and drops RL-A4's frontier.
Option C has no row.

### 5.2 Documents

| File and line | Change |
| --- | --- |
| `docs/operations.md:272` | "start again at speed 0" becomes "keep their speed and their track reservations" |
| `docs/operations.md:290` | Faulted pods: no change under RL-D3 A |
| `docs/operations.md:310`, `:376`, `:382` to `:394` | Version 12 becomes 13, in slice 1, because `AGENTS.md` ties this text to `stateVersion` |
| `docs/model.md:301` to `:303` | Add the stop-point check of a restored link |
| `docs/station-banks.md:119` | "resets speed, and rebuilds ordinary reservations" |
| `docs/design/group-physics-design.md:129` | "Future track grants can differ because saves do not retain speed" |

The study records `docs/studies/express-qualification.md:47` and `docs/studies/pickup-swap-sustained.md:69` stay as records.
`express-standees-contract-proposal.md:426` stays, because it describes files from before that contract.

### 5.3 Pins and goldens that move

| Pin | File and line | Change |
| --- | --- | --- |
| Zero speed after an Express restore | `internal/sim/express_restore_test.go:26` | Requires the live speed |
| Zero speed after a shared-track checkpoint | `internal/session/london_shared_express_test.go:54` to `:66` | Requires the live speed, and counts kept speeds |
| Speed reset and grant report | `internal/sim/shared_express_test.go:146` to `:151` | "not rebuilt" must be 0 |
| `comparablePod` zeroes `Speed` | `internal/sim/state_test.go:51` to `:54`, 15 callers of `checkRestoredMatches` | Compares `Speed` for a traveling pod that is not faulted |
| Refusal order | `internal/sim/state_test.go:219`, `:258` | One more check at the end |
| Bad links | `internal/sim/platoon_test.go:1095` | Adds a stop-point case |
| State golden | `internal/session/testdata/state_v12.json`, `order_state_test.go:27`, `:31`, `:333` | Becomes `state_v13.json` |
| Members golden | `internal/session/testdata/state_v12_members.txt`, `order_state_test.go:65` | Becomes `state_v13_members.txt` with two paths |
| Version constant | `internal/session/state_file.go:29` | 13 |
| Byte proofs | `internal/session/plain_byte_proof_test.go:26`, `:45`, `internal/session/express_byte_proof_test.go:40` | Fill both members at their widest |
| Saved-state digests of the behavior goldens | `internal/session/testdata/preset_behavior_parity.json`, `london_express_parity.json` | The `savedState` digests move, because the hashed JSON holds the version (approved 2026-11-22) |

### 5.4 Pins that must not move

| Pin | File and line |
| --- | --- |
| Stream version and media type | `internal/session/stream_codec.go:24`, `internal/session/state_http.go:17`, `web/shell.js:127`, `web/editor.js:1030` |
| Stream goldens | `internal/session/testdata/stream_*.json` |
| Stream byte proofs | `TestPlainStreamMaximumEncoding`, `TestExpressStreamMaximumEncoding` |
| Faulted pod restore | `internal/sim/fault_suspension_test.go:743` (under RL-D3 A) |
| Live behavior goldens | The stream digests of `internal/session/testdata/preset_behavior_parity.json` and `london_express_parity.json`, and `command_digests.txt` |

### 5.5 Gates

| Gate | Command |
| --- | --- |
| Each slice | `go build ./...`, `go vet ./...`, `mise run lint`, `go fix -diff` on the changed packages, `go test ./...`, `mise run test:web` |
| Byte bounds | `TestPlainStateFileWorstCaseSize`, `TestExpressStateFileWorstCaseSize` |
| E4 | `mise run test:london-express` |
| SX4 | `mise run test:shared-express` |
| Incident soak restores | The CI task that runs `internal/session/incident_soak_test.go` |
| Races | `mise run test:race:sim`, `mise run test:race:session`, once on the tip |
| CI | `mise run check` on the tip |

## 6. Slices

| # | Commit subject (draft) | Packages | Evidence |
| ---: | --- | --- | --- |
| 0 | None: a scratch measurement | Scratch tree under `/tmp/build/podsim/restore-link/` | See below. Each stop of section 8 goes to the maintainer before slice 1. |
| 1 | `sim: Restore the speed and the track reservations` | `internal/sim`, `internal/session`, `docs/operations.md` | The members, the export, the claim, the stop checks, the refusals, version 13, the goldens, the byte proofs, and the version text of `docs/operations.md`. One commit, because the members change the session golden. Each new check is checked by mutation. |
| 2 | `docs: Describe restores that keep speed` | `docs/` | The other rows of section 5.2. `mise run lint` |
| 3 | `contracts: Apply the restore speed amendments` | `docs/contracts/` | The RL-A rows, after approval. |
| 4 | `contracts: Record the restore speed evidence` | `docs/contracts/` | The gates of section 5.5, and the E4 and SX4 reruns. |

Slice 0 measures, on the `rail-hub` platoon arm, LondonFull, and the shared-track preset, at the SX4 checkpoints and the platoon corridor tests:

1. The link count of the live run, of a clone, and of a restore, at the restore tick and after 1, 10, and 60 s.
   This is the report for Q1, and it does not gate slice 1.
2. The path of section 2 for each lost link.
3. The number of live pods whose frontier is below the derived frontier.
4. The number of live pods that fail the stop check of section 3.1, and of links that fail the stop-point check.
5. The new links in the first 10 s after a restore, against a clone.
6. For option B, the distribution of the clamped speeds.
7. Each depot entry and maneuver group lane where a saved frontier differs from what `placeTravelingPod` derives.
8. The number of live pods above the speed limit of their current lane, for rule 4 of section 4.2.

Slice 0 runs under `nice`, with a memory cap, and the scratch tree is deleted after the record.

### 6.1 Result of slice 0

Slice 0 ran on 2026-11-21 at `fd1f7d17`, with 153 restores in 8 runs, under `nice` with a memory limit.
The runs were the setups of `TestPlatoonRestore`, `TestPlatoonCorridorLinks` at platoon limits 2 and 4, `TestEmergencyQueueRestore`, the 4 sim and 15 session SX4 checkpoints of the shared-track preset, 60 minutes of the `rail-hub` platoon arm with 70 restores, and 60 minutes of LondonFull with 30 restores.
The frontier that the scratch code derives matched `reservedThrough` of the real restore in 7,554 of 7,554 comparisons.

- Items 1, 2, and 5: no restore lost a link at the restore tick.
  The loss of Q1 reproduces on the fixture of `TestEmergencyQueueRestore`: after a restore at tick 151, the links of `p03` to `p02` and of `p04` to `p03` end at tick 152, and the clone keeps them.
  Both links were draining.
  The restored follower holds no resource of a pod ahead, because the restore keeps grants only to the derived frontier (`reservedThrough` 5 and 4 against 7 and 6 in the clone), so `maintainLink` ends the link at once through `over && !holdsPending` (`platoon.go:442`).
  `maintainLink` has no speed term, so the cause is the lost forward grants and not the speed reset.
  Row 6 of section 2 is the matching row, but the link ends at once and does not drain.
  In the first 10 s after a restore, the restored copies formed 8 new links and the clones 10, so the extra links of section 2 did not appear.
  LondonFull formed no link in 60 minutes.
- Item 3: no live pod had a frontier below the derived frontier, in 641,360 samples of traveling pods at one tick in 30.
  The largest lead was 19 blocks.
- Item 4: with `restoreTolerance`, no pod failed the stop check, and no link failed the stop-point check, in 44,633 link samples.
  With an exact comparison, 59 frontier samples and 653 lower-limit samples fail by at most 1.76e-12 m, so the checks of slice 1 need `restoreTolerance`.
- Item 6: of 7,146 moving pods, option B keeps the speed of 791 and clamps none to 0, and it keeps 54 to 76 percent of the summed speed.
- Item 7: only LondonFull has differences, and each live frontier is ahead of the derived frontier.
  18 pods at a depot entry are 1 or 2 blocks ahead in 33 samples: the live frontier is the entry block, and the pod does not claim its destination.
  6 pods on the lanes of a maneuver group (`<station>-bay-N-in-*`) are 13 to 19 blocks ahead in 25 samples, and the derived frontier ends at the end of `access-in`.
  No restore checkpoint held such a pod, and the run did not trace the rule that grants these blocks.
- Item 8: no live pod was above the speed limit of its lane.
- `Check` ran at each tick for 60 s on each restored copy, and the D6 oracle on the shared-track copies, with no violation.
  The envelope parity and E4 did not run.

The stop "A depot or maneuver frontier differs from the derived one" fires for item 7, so slice 1 waits for the maintainer.
Each difference is ahead of the derived frontier, so `reservedAhead` stays 0 or more.
On 2026-11-22, the maintainer asked for a trace of the rule before the build.

### 6.2 The grant rule of item 7

A grant takes the blocks from its request to `reservationEnd` of the request (`traffic.go:759` to `:761`).
`reservationEnd` adds the first block of the next lane at the end of a lane, and the whole run of blocks that share a junction resource (`traffic.go:851` to `:889`).
Each cell of a maneuver group lane holds the junction resource of its group (`maneuverIndex.everyCell`, `maneuver_groups.go:114`), so one grant takes the whole group run.
The restore derives its frontier from the current block of the pod (`state_physical.go:825`), so a run that a later grant took is ahead of it.
A pod bound for a depot slot has a frontier at the depot entry until it claims its slot, and the restore keeps that rule (`state_physical.go:830` to `:836`).

A grant can end at a block F exactly when `reservationEnd(F) == F`.
At F, a last block of a lane or a junction run past F would carry a grant past F, and both also make `reservationEnd(F)` larger than F.
So a chain of grants from the derived frontier stops at each such block.
The restore accepts a saved frontier F, the derived frontier plus `reservedAhead`, when F is in the saved route, `reservationEnd(F) == F`, and F does not pass the depot entry of a pod that does not claim its destination.
Any other saved frontier demotes the pod, as a `reservedAhead` past the saved route does.
The footprint and link claims of `placeTravelingPod` then check the blocks to F, so a group junction that another pod owns demotes the pod, and no new claim rule is needed.
This answers the stop of item 7, and the build goes on.

Slice 1 found one exception, which the maintainer approved on 2026-11-22.
A pod bound for a depot slot that it does not claim has its live frontier at the depot entry, and there `reservationEnd(entry)` is past the entry.
The restore accepts F at the entry for such a pod.
Below the entry, F must satisfy `reservationEnd(F) == F`, and an F past the entry demotes the pod.

## 7. Decisions for the maintainer

| ID | Question | Options | Recommendation |
| --- | --- | --- | --- |
| RL-D1 | The restore rule | A: keep speed and frontier, with a proof at the restore. B: keep speed, clamped to the derived frontier. C: keep the current rule. | A, if slice 0 passes. It keeps the motion and the grants, and the proof is the same inequality that each live tick keeps. B keeps little speed, because the derived frontier is short. C costs nothing, but each restart stops all traffic. The link symptom does not decide this: its cause goes to the maintainer with Q1, and a fix for it is a separate change. |
| RL-D2 | The encoding of the frontier | A: `reservedAhead`, blocks past the derived frontier. B: the block index in the saved route. C: a route distance that the restore matches to a block end. | A. It is 0 for most pods, so it is omitted, and it does not depend on the trim of the route start. B moves with the trim. C needs a tolerance rule. |
| RL-D3 | Faulted pods | A: keep speed 0 and `faultCap = distance`. B: save the speed and the fault cap. | A. It keeps the incident suspension contract, and a braking fault lasts a few seconds. B reopens rows F4 and the restore rows of that contract. |
| RL-D4 | A malformed member | A: refuse the file (section 4.2). B: demote the pod, as `decodePod` does for a bad traveling value. | A. `AGENTS.md` says that a damaged file moves aside and that no code recovers part of it. The existing demotions stay as they are. |
| RL-D5 | The link check | A: keep the position check first, then add the stop-point check. B: replace the position check. | A. The existing text and its place stay, and the new check covers the speed. |
| RL-D6 | The version | A: 13. B: keep 12 with members omitted at 0. | A. Version 12 is on `origin/main`, and the rule of `incident-stage7-contract-proposal.md:108` moves a pushed version once. B lets an older build read a new file as `invalid_state` instead of `unsupported_version`. |
| RL-D7 | The evidence | A: rerun E4 and SX4 in full. B: rerun only the restore checkpoints of E4 and SX4. | B. The live runs do not change, and only the restored copies change. |

The maintainer decided on 2026-11-21: RL-D1 A, RL-D2 A, RL-D3 A, with the restore ignoring the speed of a faulted pod (Q3), RL-D4 A, RL-D5 A, RL-D6 A, and RL-D7 B.

## 8. Stops

| Condition | Action |
| --- | --- |
| Slice 0 cannot reproduce a lost link at a clean physical restore | Not a stop for A. Report the paths of section 2 that it found, with Q1. |
| A live pod has a speed above the speed limit of its lane plus 1e-3 m/s | Stop. The physical tier would demote a valid pod (section 4.2). |
| A live pod has a frontier below the derived frontier | Stop. RL-D2 needs a signed member or option B. |
| A live pod fails the stop check, or a live link fails the stop-point check | Stop. The live rule and the proof differ, and that is a finding on its own. |
| A depot or maneuver frontier differs from the derived one | Stop, and report the lane and the rule. |
| A demotion at an E4 or SX4 checkpoint | Stop. |
| An oracle violation: `Check`, the D6 oracle, or the envelope parity | Stop. |
| A pin of section 5.4 moves | Stop. |
| A byte bound over a cap | Stop. No cap changes without a decision. |

## 9. Open questions

- Q1, answered: the restore lockstep test of the emergency queue promotion (2026-11-18) found that a restored follower keeps its link at the restore and loses it one tick later, so the restored run and the live run differ from that tick.
  Slice 0 reproduced it and found the cause: the restore keeps grants only to the derived frontier, so a draining follower holds no resource of a pod ahead and its link ends (section 6.1).
- Q2, answered: "stream restore" was a wording of the brief, and it means nothing here.
  Only the restart restore calls `RestoreState`, a rewind uses a clone, and a client never restores from the stream (section 1.2).
- Q3, answered on 2026-11-21: the export writes no speed for a faulted pod, and the restore ignores a saved speed of a faulted pod.
  A speed that is not finite or is negative still refuses the file (section 4.2, rule 1).
