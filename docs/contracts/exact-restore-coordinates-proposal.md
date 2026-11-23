# Exact restore coordinates proposal

Status: Proposed on 2026-11-23, not approved.
Date: 2026-11-23.
Line references are for `691f1f6b`.
Sections 1.4, 2.3, 2.4, and 2.5 hold the measurements of this draft.
No code changed for this draft.

## Summary

The [restore speed proposal](restore-speed-proposal.md) keeps the speed and the frontier of each traveling pod at a restore.
Its section 4.1 records three owner cases that are older than that proposal, and the maintainer moved them to this follow-up.
All three come from one cause: the restore does not rebuild the route coordinates of the live pod bit for bit.

The export trims the saved route of most traveling pods to start at a later lane, and it saves the distance from the start of that lane.
The restore sums the lane lengths of the trimmed route from 0, so its lane starts can differ from the live lane starts in the last bits.
A block-end snap then hides most of the difference, and it causes two of the three cases.

This proposal gives five options (decision EC-D1):

- A: save the live start of the first saved lane, and keep that base in the live coordinates of the pod.
- B: as A, but keep the base in a separate member, so that the ridden meters do not change.
- C: save the owner decisions of the restore, so that the restore copies them.
- D: save the whole live route when it fits the route limit, and remove the snap for such a route.
- E: keep the bounded residual.

The recommendation is D.
It fixes the three cases on each whole route, it changes no live-path code, and its routes add no byte to the byte proofs.
On a 35-minute LondonFull run, the export trims 98.6 percent of the traveling-pod samples, and no whole route passes the route limit (section 2.3).
The main risk of D is the restore block budget.
Whole routes took 68 percent of it at the busiest tick of that run, against 37 percent for trimmed routes, and no pod went over it.
Decision EC-D7 chooses how the budget counts a whole route.

## Scope

In scope: the saved route of a traveling pod, its route distance and ridden meters, the block-end snap of the restore, the residual tests of the restore speed proposal, and the saved-state version.

Not in scope:

- The speed and the frontier rules of the restore speed proposal, which do not change.
- The fallback to the restore before that proposal (its section 4.2), which stays as the lower bound of each outcome.
- The logical tier, the stream, and the HTTP state.
- The trajectory after a restore that has a pod with a trimmed route.
- A migration of version 13 files.
  A version 13 file gets `unsupported_version`, and the server moves it aside and starts a new session, as `AGENTS.md` allows until v1.

## Conventions

- Decision IDs have the prefix EC-D, and amendment IDs have the prefix EC-A.
- "The trim" is the choice of the first saved lane by `savedStart` (`internal/sim/state.go:664` to `:680`).
- "A trimmed route" is a saved route that starts after route index 0 of the live route.
  "A whole route" is a saved route that starts at route index 0.
- "The live frame" is the route distance of the live pod: the sum of the lane lengths in route order from 0 (`routeBlocks`, `internal/sim/traffic.go:522` to `:560`).
- "The saved frame" is the same sum over the saved route, which the restore builds.
- "The snap" is the block-end snap of `restoredDistance` (`internal/sim/state_physical.go:1054` to `:1070`).

## 1. Problem

### 1.1 The trim

`exportPod` saves a traveling pod from the route index `start` of `savedStart` (`state.go:601` to `:615`):

- `pod.Route` holds `v.Route[start:]`, and `pod.RouteIndex` is `current - start`.
- `pod.Distance` is `v.distance - offset`, where `offset` is the live start of lane `start`.
- `pod.RiddenMeters` is `v.riddenBase + offset` for a pod that carries passengers.

`savedStart` takes the first lane that can still hold a resource (`savedRouteStart`, `state.go:723` to `:734`).
Then it moves the start back for three rules:

- The origin tail: the pod must be at least the origin tail of the first saved lane past its start (`originSavedStart`, `state.go:689` to `:695`, from `95dbeb9b`).
- The maneuver group: a pod in a group run keeps the last lane before the run (`groupSavedStart`, `state.go:702` to `:713`, from `e63c8721`).
- The follower: a predecessor keeps the first lane of the saved run of its follower (`state.go:668` to `:679`).

The trim came with the first persistence (`43cb0782`, 2026-09-23).
Its commit message gives the reason: "Live routes can grow without a bound, because a diversion or a loop around a full station adds lanes."
A saved route has at most `limits.pod` lanes: the lanes plus the nodes of the network, but at most `MaxSavedRouteLanes`, 1,024 (`state.go:738` to `:750`).
`routeLaneIndexes` returns nil for a longer route (`state.go:762` to `:764`), and the restore moves such a pod to a free berth (`docs/operations.md:219` to `:222`).

### 1.2 The frames

Each live route comes from `setVehicleRoute` (`traffic.go:585` to `:590`) or from `routeBlocks` directly (`internal/sim/berth_choice.go:122` to `:129`).
Both sum the lane lengths of the whole route from 0.
A rebuild that keeps the distance keeps the route prefix, so the lane starts of the prefix do not change.

The restore calls `setVehicleRoute` on the saved route (`state_physical.go:609`), so it sums the trimmed lanes from 0.
In floating point, `(a + b) + c - a` is not always `b + c`.
So the saved frame and the live frame less `offset` can differ by a few float steps.
`restoredDistance` takes the saved distance when it is within `restoreTolerance` of the lane start plus the lane distance (`state_physical.go:1056` to `:1058`).
Then it moves the distance to a block end within the snap (`:1059` to `:1068`).
The snap is `restoreDriftTolerance`, 1e-9 m, on the primary path (`state_physical.go:26`, `:108`).
It is `restoreTolerance`, 1e-6 m, in the restore before the saved motion (`state_physical.go:16`, `state.go:426` to `:431`).

The export finds the restored block with the same code on the saved frame (`savedMotion`, `internal/sim/state_motion.go:93` to `:95`).

### 1.3 The three cases

Review round 4 of the restore speed proposal found these cases on built geometry (`restore-speed-proposal.md:268` to `:276`):

1. A pod at rest exactly on a release distance, on a trimmed route, keeps a cell that the live pod has released.
   The restore can then demote another pod with a valid grant through that cell, or hold a follower while the pod stays at rest.
   Cause: the trim.
   The comparison `release > distance` of the held resources (`state_physical.go:919` to `:925`) reads the saved frame.
2. A backward snap to a block end gives a moving pod a cell that it has released.
   This also occurs on a route that is not trimmed.
   Cause: the snap.
3. A snap moves a pod back across its origin tail on a trimmed route, and the restore demotes it.
   Cause: the snap and the trim together.
   The origin-tail check is `state_physical.go:874`.
   On a whole route of a pod with an origin berth it does not apply, because the route starts at the origin node.

`TestRestoreExactCoordinatesFollowUpReleaseAtRest` (`internal/sim/restore_motion_test.go:1545`) is a fixture of case 1, and it skips.
At `691f1f6b`, its `-v` log is: "saved distance 36.39999999999998, restore {Tier:physical ...}, owners that the restore adds [{2 l1 0}]".
The live pod is at 336.4 m, exactly on the release of the first track cell of `l1` (300 + 122/5 + 12).
The trimmed save has 36.39999999999998 m, so the restore holds that track cell again.

The fallback of `restore-speed-proposal.md:304` to `:315` makes each outcome no worse than the restore before that proposal.
So the three cases are bounded, but they are not fixed.

### 1.4 The residual of the boundary sweep

`TestRestoreBoundarySweep` (`internal/sim/restore_boundary_sweep_test.go:188`) puts a pod near each block end and each release distance of five fixture routes.
It accepts two residual kinds only (`classifyResidual`, `:461` to `:497`):

- (a) A moving pod at most 1e-9 m short of a block end restores on the block end, and drops the owners that the live pod releases there.
- (b) A pod on a trimmed route, exactly on a release distance, holds more resources than the live pod.

These two kinds are not the three cases of section 1.3, which are outside the sweep fixtures.
Kind (b) has the cause of case 1, and kind (a) is the forward form of case 2.

This draft ran the sweep at `691f1f6b` with `PODSIM_QUALIFY=1 go test -run '^TestRestoreBoundarySweep$' -v ./internal/sim`, under `nice` with a memory limit.
It passed in 69 s.
`restore-speed-proposal.md` does not record these counts.

| Fixture | Checks | Trimmed | At rest | Residual (a) | Residual (b) |
| --- | ---: | ---: | ---: | ---: | ---: |
| boundary | 5,112 | 3,516 | 1,254 | 49 | 9 |
| depot | 4,940 | 4,277 | 1,254 | 99 | 18 |
| express | 4,788 | 3,669 | 1,216 | 39 | 0 |
| maneuver group | 4,665 | 4,154 | 1,178 | 138 | 0 |
| release | 4,636 | 2,916 | 1,178 | 21 | 0 |
| Total | 24,141 | 18,532 | 6,080 | 346 | 27 |

The largest move of a residual (a) was 9.999894245993346e-10 m, inside the snap.

### 1.5 Other values in the saved frame

These values also read the saved frame after a restore.
They are not owner cases, so the restore speed proposal does not list them.

- The ridden meters: `riddenMeters` is `riddenBase + distance` (`internal/sim/riders.go:65` to `:70`), and the restore sets `riddenBase` from the saved `RiddenMeters` (`state_physical.go:401`).
  The detour checks add `riddenBase` to `lanesMeters` of the route (`internal/sim/drop_offs.go:126`, `:140`, `:181`, `internal/sim/endpoint_reroute.go:126`).
- The platoon cap maps the position of a predecessor through the lane starts of both pods (`leaderPosition`, `internal/sim/platoon.go:720` to `:725`).
- The route presentation reads the route origin and the route occurrences (`internal/sim/presentation.go:70` to `:92`).
  After a restore with a trimmed route, the restored pod shows a later origin node than the live pod.

## 2. The trim on built networks

### 2.1 Why the trim exists

| Reason | Source | Effect of a whole route |
| --- | --- | --- |
| A live route can grow without a bound | `43cb0782`. `diversion.go:99`, `released.go:110`, `depot.go:194`, and `berth_choice.go:44` append to the live route. | A whole route can pass `limits.pod` when the trimmed route fits. On a small network the limit is low: the lanes plus the nodes. |
| The restore block budget | `state_physical.go:323` | Each lane of a saved route counts. A whole route costs more blocks, and the pods with the longest routes go to a berth first (`docs/operations.md:278`). |
| The origin tail | `originSavedStart`, `95dbeb9b` | Not needed. The whole route starts at the origin, so `originTail` is the live value. |
| The maneuver group phase | `groupSavedStart`, `e63c8721` | Not needed. The whole route holds the lanes that give the live phase. |
| The follower | `savedStart`, `state.go:668` to `:679` | Not needed. Each lane of each run is in the saved route. |
| The bytes | `internal/session/plain_byte_proof_test.go:120`, `:127` | None. The byte proofs fill each pod route with `MaxSavedRouteLanes` lanes already. |

No document gives a byte reason for the trim.
The byte proofs and the decoder caps assume a full route for each pod (`docs/operations.md:224`).

### 2.2 Restore checks that read the saved route

A whole route holds lanes that the pod passed, so the restore checks read more lanes:

- `checkSavedBankRoutes` checks each bank lane of each pod route, and it refuses the whole file before either tier (`internal/sim/bank_restore.go:9` to `:46`).
  A whole route starts at the origin, so a departure part starts at the route start, as `checkPartStart` requires (`:112` to `:123`).
  A passed bank part of a long route is a new input to this check.
- The depot rules keep "the lanes of its route that its origin still holds" (`docs/operations.md:449`).
- The origin-tail demotion of `state_physical.go:874` does not apply to a whole route whose first lane leaves the origin node.

LondonFull has no station banks, so `checkSavedBankRoutes` returns at once there (`bank_restore.go:10` to `:12`).
Project D has station banks, and section 2.4 runs the check on each whole route of a 20-minute run.
No check refused a whole route in either run.
Slice 0 checks the other presets (section 6.1).

### 2.3 Measurement on LondonFull

This draft ran a scratch test, which it deleted after the run.
The test runs `LondonFull` with 900 requests of the `Interpeak` band of `LondonFullDemand`, one request each 2 s, for 35 simulated minutes.
LondonFull has 327 pods, 9,024 lanes, and 5,740 nodes, so `limits.pod` is 1,024 and the block budget is 256,000.
At each second, the test reads each traveling pod.
At each minute, it restores the save as `ExportState` writes it.
It also restores a copy of the save with the whole route of each traveling pod, with the snap and with no snap.
The test writes the whole routes into the `SavedState` in memory, with no JSON or session codec round trip.
It ran under `nice` with a memory limit.

The run completed 535 of the 900 requests, and it formed no platoon link.

| Count | Value |
| --- | ---: |
| Traveling-pod samples | 544,656 |
| Samples with a trimmed route | 536,966 (98.6 percent) |
| Trimmed samples whose saved lane start differs from the live start less `offset` | 20,266 (3.8 percent), at most 2.36e-12 m |
| Trimmed samples where the snap moves the restored distance | 1,701 |
| Samples with a whole route today (7,690) where the snap moves the restored distance | 0 |
| Trimmed samples whose ridden meters differ from the live value | 963 |
| Samples where `routeBlocks(v.Route)` differs from the live lane starts | 0 |
| Longest route, whole and trimmed | 115 lanes and 102 lanes |
| Routes over `limits.pod`, whole and trimmed | 0 and 0 |
| Most blocks of one route, whole and trimmed | 1,746 and 1,540 |
| Most blocks of all pod routes at one tick, whole and trimmed | 174,858 and 94,204, against the budget of 256,000 |
| Route time of the export, whole and trimmed | 0.78 µs and 1.58 µs for each traveling pod |

The trimmed time is `savedStart` and `routeLaneIndexes`, and the whole time is `routeLaneIndexes` only.
The test timed them one after the other on the same lanes, in one run.
Neither time includes the copy of the state or the saved prefix that `savedMotion` builds for a trimmed route, so they are not a measurement of `ExportState`.

Each of the 35 restores of each kind kept the physical tier and demoted no pod.
Each restore owned the same resources as the live run, with the same owners.
The restore of whole routes with no snap put each traveling pod at its live distance, bit for bit.
None of the 35 restore ticks held a pod on a boundary of section 1.3, so the run shows the drift and the snap, not the cases.

The block sum is the important result.
Whole routes take 68 percent of the budget at the busiest tick, against 37 percent for trimmed routes.
When the sum passes the budget, the restore moves the pods with the longest routes to a berth (`limitWork`, `state_physical.go:519`).
The fallback reads the same file, so it does not prevent such a demotion (decision EC-D7).
This run did not reach the budget, and it does not show how close a busier band or a larger fleet comes.

### 2.4 Measurement on project D

A second scratch test runs project D, the banked LondonFull Express project of `internal/sim/testdata/express_project.json`, for 20 simulated minutes.
It submits the Express parties and the trips between Waterloo and Bank of `TestLondonExpressEnvelopeRun`, with no fault and no emergency.
At each 30 s, it restores the save, the copy with whole routes, and the copy with whole routes and no snap, as section 2.3 does.
It ran under `nice` and `timeout`, with no memory limit, because `systemd-run` does not run in the sandbox of this draft.

| Count | Value |
| --- | ---: |
| Traveling-pod samples | 56,156 |
| Samples with a platoon link | 7,909 |
| Samples with a trimmed route | 53,472 (95.2 percent) |
| Routes over `limits.pod`, whole | 0 |
| Restores of each kind | 40 |
| `checkSavedBankRoutes` refusals of whole routes | 0 |
| Pods demoted, in each kind of restore | 0 |
| Owner differences against the live run, in each kind of restore | 0 |
| Traveling pods whose whole-route restore with no snap differs from the live distance | 0 |

The copy with whole routes moves the lane indexes of each saved platoon link by the trimmed lanes of each pod.
Each saved link restored with these indexes in the 40 restores, so the saved link of a whole route needs no new rule.

### 2.5 The fixture of case 1 with a whole route

The scratch test also saved the fixture of `TestRestoreExactCoordinatesFollowUpReleaseAtRest` with the whole route.
The trimmed save has the route `[7 8 9]`, the route index 0, and 36.39999999999998 m.
The restore adds 1 owner.
The whole save has the route `[6 7 8 9]`, the route index 1, and 336.4 m.
The restore adds no owner and drops no owner.

## 3. Options

### 3.1 Option A: a based route frame in live coordinates

The save adds `routeBase`, the live start of the first saved lane.
The restore builds the lane starts of the saved route from that base, so they equal the live starts bit for bit.
The restored pod then keeps the based frame in live ticks: its route index 0 starts at `routeBase`, not at 0.

Each live site that assumes that the route starts at 0 must read the base.
The inventory of section 4 has 37 sites, and 30 of them run in live ticks.
A journey start sets the base to 0, so the base lives only from a restore to the end of the journey.

Costs and risks:

- 30 live-path sites, where the base is 0 in each live run that does not restore.
  A missed site is a bug that only a restored run shows, after a diversion or an extension.
- One member, about 36 bytes for each traveling pod at its widest: 36,000 bytes for 1,000 plain pods and 21,600 bytes for 600 Express pods.
  The headroom is 8,259,163 bytes plain and 29,052,705 bytes Express (`restore-speed-proposal.md:480`).
- `RiddenMeters` changes meaning: the saved value is `riddenBase` again, and each detour check must subtract the base from `lanesMeters`.
- The snap must go, or case 2 stays.
- New gate: the lockstep test of section 5.3, on a trimmed route.

### 3.2 Option B: a separate exact-distance member

The restored pod keeps the saved frame for `distance` and the blocks, as today.
It gets a second member, the base, and each comparison that must match the live run reads the distance plus the base against a release distance in the live frame.

Costs and risks:

- The same frame sites as A, except the ridden meters.
  `riddenBase` and `distance` stay in the saved frame, so `riddenMeters` keeps its arithmetic.
- Two distances for each pod, and each site must choose the correct one.
  The release distances of the blocks are in the saved frame, so a comparison in the live frame needs a second release value or a conversion.
- The same member and bytes as A.
- New gate: the lockstep test of section 5.3.

### 3.3 Option C: restore-side decisions in the save

The save records the owner decisions that the restore recomputes, and the restore copies them:

- The resources that the pod holds behind its position, as pairs of a saved block index and a resource kind.
- Whether the pod released its origin (`originReleased`).
- The current block, so that the snap no longer picks it.

No live site changes.

Costs and risks:

- A new count limit.
  The resources behind a pod have no cap today: a lane has at least 2 cells, so a retention tail over short lanes can hold many cells (`laneBlockCount`, `traffic.go:51` to `:53`).
  With a cap of 64 entries of `[255999,3],`, a pod has about 720 more bytes at its widest.
  That is about 720,000 bytes for 1,000 plain pods and 432,000 bytes for 600 Express pods, inside both headrooms.
- New refusals for an entry past the saved route, a duplicate entry, and an entry past the cap, each with its place in the refusal order.
- The restored pod stays in the saved frame.
  The restore tick matches the live owners, but a later tick can still differ at a block end or a release distance, and the ridden meters keep their drift.
- It fixes cases 1 and 3, and case 2 when the restore also copies the current block.
- New gate: the boundary sweep with no residual kind.

### 3.4 Option D: save the whole route when it fits

The export saves `v.Route` from index 0 when it fits `limits.pod`.
Then `RouteIndex` is the live route index, `Distance` is `v.distance`, and `RiddenMeters` is `riddenMeters` with no offset.
The restore builds the same lane starts as the live pod, because it runs `routeBlocks` on the same lanes in the same order.
The restore of a whole route uses no snap: a snap of 0 returns the saved distance unchanged (`state_physical.go:1056` to `:1069`).
`savedMotion` reads the blocks of the live pod for a whole route, and builds no saved prefix.

A route that does not fit keeps the trim of today, with the snap and the residual (decision EC-D2).
The restore must know which kind of route it reads (decision EC-D3).

Costs and risks:

- No live-path site changes.
  The changes are in `exportPod`, `savedMotion`, and the snap choice of the restore.
- No byte in the byte proofs for the route.
  A marker member, if EC-D3 chooses one, adds `,"routeTrimmed":true`, 20 bytes for each pod: 20,000 bytes plain and 12,000 bytes Express.
- A saved route is longer, so a save and a restore read more lanes.
  The route part of the export was not slower in the run of section 2.3, but that run did not time `ExportState`.
- The restore block budget counts each lane of each saved route (`state_physical.go:488` to `:493`).
  On LondonFull, whole routes took 174,858 of the 256,000 blocks at the busiest tick, and trimmed routes 94,204.
  Past the budget, the restore moves pods to a berth that the restore of today keeps, and the fallback does not help, because it reads the same routes.
  Decision EC-D7 chooses between this risk, a new count rule, and a larger budget.
- A whole route is a new input to the restore checks of section 2.2.
  A refusal before the tiers would be worse than today, so slice 0 checks each whole route of the qualification presets.
- New gates: the boundary sweep with no residual on a whole route, the fixture of case 1 as a pass, and the lockstep test of section 5.3.

### 3.5 Option E: keep the bounded residual

No change.
The fallback keeps each outcome no worse than the restore before the restore speed proposal.

Costs:

- The three cases stay, also the residual kinds of section 1.4.
- The restored run can leave the live run at a block end or a release distance, and at the next detour check.

### 3.6 Effect by option

| Effect | A | B | C | D | E |
| --- | --- | --- | --- | --- | --- |
| Case 1 (at rest on a release distance) | Fixed | Fixed | Fixed at the restore tick | Fixed on a whole route | Bounded |
| Case 2 (backward snap) | Fixed with no snap | Fixed with no snap | Fixed with the current block | Fixed on a whole route | Bounded |
| Case 3 (snap across the origin tail) | Fixed with no snap | Fixed with no snap | Fixed | Fixed on a whole route | Bounded |
| Residual (a) and (b) of the sweep | None | None | None at the restore tick | None on a whole route | As section 1.4 |
| Later ticks in the live frame | Yes | Yes, where a site reads the base | No | Yes, on a whole route | No |
| Live-path sites | 30 | 22 | None | None | None |
| New saved members | `routeBase` | `routeBase` | 3, with a new cap | None, or the marker of EC-D3 | None |
| Byte growth, widest | 36,000 plain, 21,600 Express | As A | About 720,000 plain, 432,000 Express | 0, or 20,000 plain and 12,000 Express | 0 |
| `stateVersion` | 14 | 14 | 14 | 14 (EC-D4) | 13 |
| New refusals | 1 | 1 | 3 or more | None, or 1 for the marker | None |
| New gate | Lockstep on a trimmed route | Lockstep on a trimmed route | Sweep with no residual | Sweep, fixture, and lockstep on a whole route | None |

A route over the limit keeps the residual under D.
Sections 2.3 and 2.4 found no such route.

For D, case 1 is measured on its fixture (section 2.5).
Cases 2 and 3 follow from the code, and no fixture of this draft shows them.
With a snap of 0, `restoredDistance` returns the saved distance, and on a whole route that is the live distance.
A whole route of a pod with an origin berth starts at the origin node, so the origin-tail check of `state_physical.go:874` does not apply.
Slice 0 adds a fixture of each case (section 6.1, item 8).
The rows for A, B, and C come from the code only.

## 4. Site inventory for options A and B

The slice-1 agent of the restore speed proposal wrote the first inventory.
This draft checked each site at `691f1f6b`, and the table gives the corrections.
Options C, D, and E change no site of this table.

### 4.1 Rebuilds that keep the distance

| Site | What it does | Correction |
| --- | --- | --- |
| `depot.go:194` | Appends the depot suffix to the route | None |
| `berth_choice.go:44` | Appends the berth suffix to the route | None |
| `berth_choice.go:122` to `:129` | Builds the blocks of a new berth route with `routeBlocks`, and installs them with no `setVehicleRoute` | The site has two lines: the build at `:122` and the install at `:126` to `:129` |
| `diversion.go:198` | `redirect` installs `to.route` | The route is built with the prefix at `diversion.go:99` and `released.go:110` |
| `endpoint_reroute.go:146` | Installs the endpoint route | The route is built with the prefix at `endpoint_reroute.go:94` |
| `operational.go:142` | Installs the operational route | The route is built with the prefix at `operational.go:133` |

Each of these sites keeps the route prefix, so under A the base carries over with no rule.
The sites that build the routes (`diversion.go:99`, `released.go:110`, `endpoint_reroute.go:94`, `operational.go:133`) take `v.Route[:prefix]`, where `prefix` comes from `divertStart`.

### 4.2 Journey starts and restore paths

| Site | What it does | Correction |
| --- | --- | --- |
| `dispatch.go:596` | Sets the route of a dispatched pod | The distance becomes 0 at `dispatch.go:600` |
| `parking.go:224` | Sets the route of an empty move | The distance becomes 0 at `parking.go:231` |
| `riders.go:190` | Sets the route of the next leg | The distance becomes 0 at `riders.go:194`, after `riddenBase += v.distance` at `:188` |
| `drop_offs.go:294` | Sets the route of a boarding pod when its first stop changes | It does not reset the distance: the pod is at a berth, after an earlier reset |
| `onboard_pickups.go:44` | Sets the route of a pod at a berth that takes a new party | It does not reset the distance, as `drop_offs.go:294` |
| `operational.go:374` | `settleIdleAtBerth` sets the distance to 0 and clears the route (`:382`) | Missing from the first inventory |
| `state_physical.go:609` | The restore installs the saved route | None |
| `state_physical.go:1317` | A demoted pod gets a boarding route at a berth | None |
| `state_motion.go:94` | `savedMotion` builds the saved prefix to find the restored block | Missing from the first inventory. It is an export site, not a live site. |

Under A, each site that resets the distance also resets the base.
`drop_offs.go:294` and `onboard_pickups.go:44` replace the route of a pod at a berth.
This draft did not check that each path to them passes a reset first, and a build of A must check it.

### 4.3 Consumers that measure from the first route lane

| Site | What it reads | Correction |
| --- | --- | --- |
| `depot.go:342` | `originTail` in `releaseDepotOrigin` | None |
| `claim_kinds.go:83` | `originTail` for an origin berth claim | None |
| `ownership.go:63` | `originTail` for the held resources | None |
| `traffic.go:1075` | `originTail` in the motion release | None |
| `state_physical.go:874`, `:927`, `:1117` | `originTail` in the restore | Three lines, not one |
| `state.go:690` | `originTailFrom` in `originSavedStart` | Missing from the first inventory. An export site. |
| `diversion.go:124` to `:135` | `divertStart` sums the lane lengths from 0 and compares with a lane end, with its own 1e-9 m margin at `:133` | The loop is at `:125`, and the margin is a second tolerance |
| `pickup_bounds.go:77` to `:89` | Walks `v.Route[:prefix]` from `v.distance` | None |
| `pickup_estimate.go:279` | `routeSecondsWith` from `v.distance`. The walk is at `pickup_estimate.go:355` to `:370` | The walker line added |
| `emergency_choice.go:134` | `chooseStation` advances a cursor from `v.distance`. The walk is at `emergency_choice.go:287` to `:293` | The walker line added |
| `diversion.go:153` | `pickupSeconds` from `v.distance` | None |
| `platoon.go:720` to `:725` | `leaderPosition` maps a position through the lane starts of two pods | Missing from the first inventory. It uses differences of lane starts, so a base in both pods cancels, but a base in one pod only does not. |

`originTail` itself (`traffic.go:1004` to `:1022`) returns a length from route distance 0, so under A each of its callers compares `distance - base` or the function adds the base.

### 4.4 The ridden-meters arithmetic

| Site | What it does | Correction |
| --- | --- | --- |
| `riders.go:65` to `:70` | `riddenMeters` is `riddenBase + distance` | None |
| `riders.go:188` | `riddenBase += v.distance` at a leg end | Missing from the first inventory |
| `operational.go:289`, `:337` | Sets `riddenBase` at a stop | Missing from the first inventory |
| `drop_offs.go:126`, `:140` | `cappedStops` adds `riddenBase` to `lanesMeters(v.Route)` | The first inventory gave `:126` only |
| `drop_offs.go:181` | `rerouteKeepsDetours` | None |
| `drop_offs.go:289` | `legRoute` gets `ridden: v.riddenBase` | Missing from the first inventory |
| `endpoint_reroute.go:126` | The endpoint detour check | None |
| `state.go:610`, `state_physical.go:401` | The export adds `offset`, and the restore reads the saved value | Restore sites, not live sites |

### 4.5 Count

| Group | First inventory | This draft | Live path |
| --- | ---: | ---: | ---: |
| Rebuilds that keep the distance | 6 | 6 | 6 |
| Journey starts | 5 | 6 | 6 |
| Restore and export paths | 2 | 7 | 0 |
| Consumers | 12 | 10 | 10 |
| Ridden meters | 4 | 8 | 8 |
| Total | About 30, about 20 live | 37 | 30 |

The restore and export paths are `state_physical.go:609`, `:1317`, `state_motion.go:94`, `state.go:690`, `state.go:610`, `state_physical.go:401`, and the origin-tail lines `state_physical.go:874`, `:927`, `:1117` as one site.
The 6 rebuilds need no edit of their own if `setVehicleRoute` sums from the base of the pod.
`berth_choice.go:122` calls `routeBlocks` directly, so it needs its own edit.
Each journey start needs its own reset, because `setVehicleRoute` cannot tell a journey start from a rebuild.

## 5. Recommendation

### 5.1 Option D

The recommendation is D, with EC-D2 B, EC-D3 A, EC-D4 A, EC-D5 A, EC-D6 A, and EC-D7 B.

- It fixes the three cases and both residual kinds on each whole route, because the restore then reads the live frame bit for bit.
- It changes no live-path site.
  A and B change 30 and 22 sites that run in each live tick, for a defect that only a restore shows.
- It adds no byte to the route in the byte proofs, because they fill each route to `MaxSavedRouteLanes` already.
- It removes the drift of the ridden meters, of the platoon cap, and of the route presentation of a restored pod (section 1.5) on each whole route.
  C does not, because it leaves the restored pod in the saved frame.
- A whole route needs no `savedStart` walk and no saved prefix in `savedMotion`.
  Slice 0 item 7 times `ExportState` to show that the longer routes do not make it slower.

D has two costs.
A route over the limit keeps the trim and the residual.
Sections 2.3 and 2.4 found none, but a small network has a lower limit, so slice 0 measures the presets and the test networks.
Whole routes use more of the restore budget, and a busy save can lose pods to it (EC-D7).
The recommended rule for EC-D7 changes the restore walks, but not the live path, and this draft did not measure it.

### 5.2 When to choose another option

- If slice 0 finds that whole routes often pass the limit or the budget on a preset, D fixes too few pods, and A is the option that fixes all of them.
- If a whole route of a live pod fails a restore check of section 2.2, D needs a change to that check first, or C.
- E is the choice if the maintainer accepts the bounded residual and wants no format change.

### 5.3 The lockstep test

The lockstep test restores a save at a tick and steps the live and the restored simulations together.
At each tick it compares each pod bit for bit: the distance, the speed, the lane starts, the frontier, the waiting state, the platoon link, and the ridden meters.
It also compares the owners.
The run passes through a diversion, a depot extension (`depot.go:194`), and a berth-suffix extension (`berth_choice.go:44` or `:122`).

Under D, each pod of the restore has a whole route, so each compare is exact.
Under A and B, the test also needs a trimmed route, because the base exists only there.

## 6. Slices

| # | Commit subject (draft) | Packages | Evidence |
| ---: | --- | --- | --- |
| 0 | None: a scratch measurement | Scratch tree under `/tmp/build/podsim/` | Section 6.1. Each stop of section 9 goes to the maintainer before slice 1. |
| 1 | `sim: Save the whole route of a traveling pod` | `internal/sim`, `internal/session`, `docs/operations.md` | The export, the snap choice, the marker of EC-D3, the budget rule of EC-D7, version 14, the goldens, the byte proofs, and the version text. One commit, because the version changes the session goldens. Each new rule is checked by mutation. |
| 2 | `docs: Describe whole saved routes` | `docs/` | The rows of section 7.2. `mise run lint`. |
| 3 | `contracts: Apply the exact restore coordinates amendments` | `docs/contracts/` | The EC-A rows of section 7.1, after approval. |
| 4 | `contracts: Record the exact restore coordinates evidence` | `docs/contracts/` | The gates of section 7.5. |

### 6.1 Slice 0

Slice 0 extends the measurement of section 2.3 to the LondonFull Express project D, the shared-track preset, `rail-hub`, and each `roundTripCase` and sweep fixture.
For each run it reports:

1. The share of traveling-pod samples with a trimmed route.
2. The number of whole routes over `limits.pod` whose trimmed route fits.
3. The largest sum of the blocks of the saved routes at one tick, for whole routes and for trimmed routes, against the budget.
4. Each restore check that refuses or demotes a whole route that the trimmed route passes.
5. The owner differences of a restore of whole routes with no snap, against the live run.
6. The bit differences of the lockstep test of section 5.3, on 60 s after each restore.
7. The time of `ExportState` with whole routes and with trimmed routes, on the busiest tick.
8. A fixture of case 2 and a fixture of case 3, each restored with the trimmed route and with the whole route.
9. Under EC-D7 B, the budget sum of each restore of whole routes, against the sum of the trimmed routes of the same tick.

Slice 0 runs under `nice`, with a memory limit, and the scratch tree is deleted after the record.

## 7. Contract text, documents, pins, and gates

### 7.1 Contract text

The restore speed proposal is approved and applied, so each row adds a "Note of `<date>`" line, and does not rewrite the record.

| ID | File and line | Current text | Proposed note |
| --- | --- | --- | --- |
| EC-A1 | `restore-speed-proposal.md:179` to `:185` | "`restoredDistance` can move a distance by up to `restoreTolerance` ..." and the drift tolerance of 1e-9 m | "Note of `<date>`: a whole saved route restores with no block-end snap (exact restore coordinates proposal). The drift tolerance applies only to a trimmed route." |
| EC-A2 | `restore-speed-proposal.md:258` to `:278` | The bounded residual and the three cases of review round 4 | "Note of `<date>`: the export saves the whole route when it fits the route limit, so the residual and the three cases stay only for a route over the limit." |
| EC-A3 | `restore-speed-proposal.md:311` | "the block-end snap at `restoreTolerance`" in the second restore | "Note of `<date>`: the second restore keeps the snap at `restoreTolerance`, also on a whole route (EC-D5)." |
| EC-A4 | `restore-speed-proposal.md:266` | "`TestRestoreBoundarySweep` counts both kinds and refuses any other difference on its fixtures." | "Note of `<date>`: on a whole route the sweep accepts no residual." |

### 7.2 Documents

| File and line | Change |
| --- | --- |
| `docs/operations.md:219` to `:222` | A pod route is the whole route of the journey when it fits, and the lanes that can still hold a resource when it does not |
| `docs/operations.md:276` to `:278` | The budget counts the blocks from the first lane of each route that can still hold a resource (EC-D7 B) |
| `docs/operations.md:274`, `:311`, `:383` to `:398` | Version 13 becomes 14, in slice 1. A row for version 14 says that a whole route restores with no snap. |
| `docs/operations.md:449` | A pod that left a slot keeps its whole route, or the lanes that its origin still holds on a trimmed route |
| `internal/sim/state.go:134` to `:138` | The comment of `SavedPod.Route` |
| `README.md:176` | The row of the restore speed proposal names this proposal, and a new row lists it |

### 7.3 Pins and goldens that move

| Pin | File and line | Change |
| --- | --- | --- |
| Skipped fixture of case 1 | `internal/sim/restore_motion_test.go:1545` | Passes: the restore adds no owner |
| Boundary sweep | `internal/sim/restore_boundary_sweep_test.go:283`, `:329`, `:363`, `:398` | A fixture with a whole route requires residual 0. The check of trimmed checks moves to a fixture with a route over the limit. |
| Round-trip "later route start" | `internal/sim/restore_roundtrip_test.go:124`, `:184`, `:376` to `:382` | Each fixture saves whole routes, so the trimmed count needs a fixture over the limit, or the rule goes |
| Fallback tests on `trimLanes` | `internal/sim/restore_motion_test.go:1129`, `:1175`, `:1208`, `:1405` | Their fallback comes from the extra cell of a trimmed route. Each needs a route over the limit, or another cause of the fallback. |
| Tests of the trim | `internal/sim/state_test.go:133`, `internal/sim/platoon_test.go:759`, `internal/sim/check_lookup_reference_test.go:33` | They read `savedStart`, which stays for a route over the limit |
| `restoredDistance` search | `internal/sim/restore_motion_test.go` (`TestRestoredPositionSearch`) | Adds the snap 0 |
| State golden | `internal/session/testdata/state_v13.json`, `order_state_test.go` | Becomes `state_v14.json` |
| Members golden | `internal/session/testdata/state_v13_members.txt` | Becomes `state_v14_members.txt`, with the marker path of EC-D3 |
| Version constant | `internal/session/state_file.go:29` | 14 |
| Byte proofs | `internal/session/plain_byte_proof_test.go:127`, `internal/session/express_byte_proof_test.go:40` | Fill the marker at its widest |
| Saved-state digests of the behavior goldens | `internal/session/testdata/preset_behavior_parity.json`, `london_express_parity.json` | The `savedState` digests move, because the saved routes and the version change |

### 7.4 Pins that must not move

| Pin | File and line |
| --- | --- |
| Stream version and media type | `internal/session/stream_codec.go:24`, `internal/session/state_http.go:17`, `web/shell.js:127`, `web/editor.js:1030` |
| Stream goldens | `internal/session/testdata/stream_*.json` |
| Stream byte proofs | `TestPlainStreamMaximumEncoding`, `TestExpressStreamMaximumEncoding` |
| Live behavior goldens | The stream digests of `preset_behavior_parity.json` and `london_express_parity.json`, and `command_digests.txt` |
| Refusal order | `TestRestoreInputCheckOrder` (`internal/sim/state_test.go:258`), except a marker refusal at the end under EC-D3 A |

Under D, a restored pod has the route of the live pod, so its route presentation (`Origin`, `Start`, and `Current`) changes after a restore.
The parity runs of `internal/session/preset_parity_test.go` and `internal/session/london_express_parity_test.go` take no restore, so their stream digests do not see this.
Slice 1 checks each other stream golden for a restore before it.

### 7.5 Gates

| Gate | Command |
| --- | --- |
| Each slice | `go build ./...`, `go vet ./...`, `mise run lint`, `go fix -diff` on the changed packages, `go test ./...`, `mise run test:web` |
| Byte bounds | `TestPlainStateFileWorstCaseSize`, `TestExpressStateFileWorstCaseSize` |
| Sweep and fixture | `mise run qualify` (`TestRestoreBoundarySweep`), and `TestRestoreExactCoordinatesFollowUpReleaseAtRest` |
| Lockstep | The new test of section 5.3 |
| E4 and SX4 | `mise run test:london-express`, `mise run test:shared-express` |
| Races | `mise run test:race:sim`, `mise run test:race:session`, once on the tip |
| CI | `mise run check` on the tip |

## 8. Decisions for the maintainer

| ID | Question | Options | Recommendation |
| --- | --- | --- | --- |
| EC-D1 | The fix | A: a based frame in live coordinates. B: a separate exact-distance member. C: restore-side decisions in the save. D: the whole route when it fits. E: keep the bounded residual. | D. It fixes the three cases on each whole route with no live-path change and no route byte in the proofs. A fixes each route, but it changes 30 live sites. C fixes only the restore tick and needs a new cap. |
| EC-D2 | A whole route over `limits.pod` | A: save no route, and the restore moves the pod to a berth, as an over-limit route today. B: save the trimmed route, as today. | B. A demotes a pod that the restore keeps today, and a small network has a low limit. |
| EC-D3 | How the restore knows a whole route | A: a marker `routeTrimmed` on a trimmed route, omitted when false. B: no marker. The restore uses no snap on any route. | A. Under B, a trimmed route over the limit restores with no snap, and a pod that stopped at a block end can restore before it, with other owners than today. |
| EC-D4 | The version | A: 14. B: keep 13. | A. Version 13 is on `origin/main`, and the version rule moves a pushed version once (`incident-stage7-contract-proposal.md:108`). B lets this build restore a version 13 file with trimmed routes and no marker, as whole routes. |
| EC-D5 | The second restore of a whole route | A: keep the snap at `restoreTolerance`, as the restore before the restore speed proposal. B: no snap there too. | A. The second restore is the lower bound of each outcome (`restore-speed-proposal.md:309` to `:315`), and it must stay the restore before that proposal. |
| EC-D6 | The residual classifier of the sweep | A: keep it, for the fixtures over the limit only. B: remove it, and require residual 0 on each fixture. | A, under EC-D2 B. A route over the limit keeps the residual, and the test must still bound it. |
| EC-D7 | The restore block budget for a whole route | A: count each lane of the saved route, as today. B: count the blocks from the first lane that can still hold a resource, and start the restore walks there. The restore finds that lane from the saved position with the rule of `savedRouteStart` (`state.go:723`). C: raise `budgetMaxBlocks` so that whole routes fit. | B. Under A, whole routes took 68 percent of the budget on LondonFull at Interpeak, and a busier save can demote pods that the restore of today keeps. On a whole route, the rule of B reads the live frame, so it finds the same lane as the live pod. The `savedStart` rules only move the start back, so the sum of B is at most the sum of the trimmed routes of today. B is a new restore rule that this draft did not measure. C changes a cap and the work bound of the restore (`state_physical.go:38` to `:58`). The text of `docs/operations.md:276` to `:278` changes under B and C. |

## 9. Stops

| Condition | Action |
| --- | --- |
| A whole route of a live pod gets a refusal before the tiers that the trimmed route does not get | Stop. D would refuse a file that the restore accepts today. |
| A whole route demotes a pod that the trimmed route keeps, by the limit, the budget, or a check | Stop, and report the preset, the route length, and the rule. |
| A restore of whole routes with no snap differs from the live owners | Stop. The live frame and the restore differ, and that is a finding on its own. |
| The lockstep test finds a bit difference on a whole route | Stop, and report the field and the tick. |
| More than 1 percent of traveling-pod samples on a preset have a whole route over the limit | Stop. D fixes too few pods there, and EC-D1 opens again. |
| `ExportState` with whole routes is slower than with trimmed routes on the busiest tick | Stop. The export runs under the session lock. |
| Under EC-D7 B, the budget sum of a restore of whole routes is more than the sum of the trimmed routes of the same tick | Stop. The count rule of the restore and the trim rule of the export disagree. |
| A pin of section 7.4 moves | Stop. |
| A byte bound over a cap | Stop. No cap changes without a decision. |

## 10. Open questions

- Q1: Must D also fix a route over the limit?
  Under EC-D2 B, such a pod keeps the residual, bounded by the fallback.
  A fix for it is option A for that pod only, with the live sites of section 4.
- Q2: Should the trim rules of `originSavedStart`, `groupSavedStart`, and the follower stay?
  Under EC-D2 B they serve only a route over the limit.
  Under EC-D2 A they have no use, and the export can delete them.
