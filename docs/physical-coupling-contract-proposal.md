# Physical coupling contract proposal

Status: **approved contract, October 4, 2026**.
The user selected both empty and passenger trains for the first implementation.
The user also selected Compact pairs on straight certified corridors.
An explicit project option permits passenger train travel.
Current private and shared cabin rules remain unchanged.
There is no new per-party consent, pooled seat capacity, or transfer between cabins.

The user approved the complete numerical model, recovery rules, and opt-in formats on October 4, 2026.
Implementation starts with pure native profile and authored geometry helpers.
Later motion, ownership, and public-format phases need their stated qualification gates.
This contract authorizes no deployment, default change, or capacity claim.
The audited source is `a13bbae`, with its full revision and file hashes in the exported source inventory.

## Decisions before implementation

The recommended minimum is one powered, mechanically connected pair of Compact pods.
Both pods have the same physical profile and direction.
Pairs can carry passengers or move empty.
The first contract excludes pairs with mixed empty and occupied membership.
Each member keeps its original identity, cabin, riders, seats, and destination.

| Class and count option | Benefit | Additional proof | Recommendation |
| --- | --- | --- | --- |
| Two Compact pods | One body profile, one coupler, two fixed member positions | New body, docking, motion, ownership, and restoration proof | Selected first scope |
| Homogeneous Legacy and Compact trains, two through four members | More fleet participation | Distinct class eligibility, longer trains, more couplers and split cases | Defer |
| Group, Express, or mixed-class trains | Larger passenger capacity | Different bodies, dynamics, large-class interfaces, and coupler compatibility | Defer |

Equal body length does not prove compatible couplers.
Group and Express remain valid individual vehicles under their existing contracts.
They cannot join a mechanical pair under this proposal.
An existing virtual link or compact queue certificate also prevents recruitment.

### Candidate numerical model

These values define a proposed simulation model.
They are not manufacturer dimensions, measured dynamics, or ordinary physical defaults.
Only the existing Compact body length, seats, tick rate, and ordinary dynamics are established source values.

| Parameter | Proposed first value | Basis or decision |
| --- | --- | --- |
| Compact cabin capacity | Existing four seats per pod | Keep class capacity and whole-party admission |
| Body length | Existing 4 m | Keep the Compact profile length |
| Body width | 2.0 m | Approve a centered rectangular train-body envelope |
| Coupler pins | Longitudinal body ends, 2 m from each center | No lateral offset |
| Free gap between body ends | 0.5 m | New model choice |
| Coupler width | 0.3 m | New rectangular connector envelope |
| Connected center spacing | 4.5 m | Body length plus free gap |
| Connected occupied length | 8.5 m | Two body lengths plus free gap |
| Connected acceleration and braking | 2 m/s² each | Reuse the ordinary numerical bound for two powered members, subject to approval |
| Connected speed cap | Minimum applicable authored lane limit | Check every member and every lane crossed during the tick |
| Docking and opening speed cap | 0.5 m/s | New maneuver bound |
| Docking and opening acceleration and braking | 0.5 m/s² each | New maneuver bound |
| Latch dwell | 2 simulation seconds | New actuator delay, both members stopped |
| Unlatch dwell | 2 simulation seconds | New actuator delay, both members stopped |
| Maximum intentional partner wait | 5 simulation seconds | Abort an uncommitted candidate when the deadline expires |
| External ordinary separation | Existing requirement, never reduced | Derive a train bound from both bodies, the connector, and current resource geometry |

The model gives both members traction and brakes.
It assumes the stated acceleration and braking bounds hold with both cabins full.
It does not model mass, drag, coupler force, elasticity, mechanical failure, or traction limits.
Do not infer energy savings or hardware feasibility from this model.
Any later force or energy calculation needs an explicit mass and payload model.

The proposed spacing change closes and opens 7.5 m from the ordinary 12 m center spacing.
The stated maneuver limits imply about 16 seconds per rest-to-rest move in a continuous calculation.
Both moves and both dwells total about 36 seconds before partner wait or denied grants.
The discrete simulation must measure its actual times.
This calculation is not a passenger delay ceiling or a measured service result.

An alternative uses site-authored gap and dwell values within an approved bounded range.
That alternative needs additional geometry combinations and profile identities in every saved certificate.
The recommendation is a fixed immutable `compact-pair-v1` profile and site-authored geometry only.
The approved implementation uses these fixed values.

## Existing source boundaries

The [project brief](../PROJECT_BRIEF.md#platoons-and-coupled-pod-trains) distinguishes coordinated pods from physically connected trains.
It requires formation and split costs, internal spacing, external spacing, and merge obstruction tests.

| Existing source | Audited behavior | Consequence for mechanical trains |
| --- | --- | --- |
| [platoon.go](../internal/sim/platoon.go), `SetPlatooning`, `planLink`, `platoonCaps` | Individual pods use follower caps and fixed shared-path certificates | A virtual link is not a mechanical constraint |
| [station_buffer_platoon.go](../internal/sim/station_buffer_platoon.go), `planBufferLink` | A fixed entry certificate shares selected interior cells | Its berth discharge does not certify a connected train |
| [vehicle_class.go](../internal/sim/vehicle_class.go), `LookupVehicleClass` | Compact has four seats and a 4 m body | Train capacity remains two separate four-seat cabins |
| [order_contract.go](../internal/sim/order_contract.go) | `express-v1` enables the approved Express profile and order bounds | A coupling marker must remain independent of the order marker |
| [traffic.go](../internal/sim/traffic.go), `move`, `speedBeforeLane`, `releaseDistance` | Each vehicle moves and releases its own route resources | A train needs one update and rear-aware resource release |
| [simulation.go](../internal/sim/simulation.go), `Step` | Admission precedes movement, and released resources cannot be reused during that tick | Keep the same tick boundary for train transactions |
| [profile_geometry.go](../internal/sim/profile_geometry.go) and [prepared.go](../internal/sim/prepared.go) | Large-class masks impose geometry, cell, and retention bounds | Do not borrow those bounds as proof of a Compact train |
| [safety.go](../internal/sim/safety.go), `SafetyObservation.Check` | Current separation uses centers and retained compact certificates | Add an independent body and connector check, not an unconditional linked-pair exemption |
| [state_physical.go](../internal/sim/state_physical.go), `footprint` | Physical restore reconstructs retained resources and forward grants | Train certificates need an atomic reconstruction rule |
| [state_logical.go](../internal/sim/state_logical.go), `restoreLogical` | Logical recovery relocates pods and requeues riders | Do not apply this automatically to a connected pair |
| [state_file.go](../internal/session/state_file.go), [stream_codec.go](../internal/session/stream_codec.go), [service.go](../internal/project/service.go) | Current project, save, and stream families end at 4, 7, and 4 | Proposed new opt-in families are 5, 8, and 5 |

Current mainline and station-entry virtual link planners reject Group and Express members.
Their existing certificates, turn rules, and small-class arithmetic remain unchanged.
The new feature cannot turn a virtual link into a latch or alter `platoonLimit` semantics.

## Authored corridors and protected sites

A coupling project declares straight corridors and protected assembly and split sites.
Each corridor names an ordered, directed lane path and its two sites.
All corridor segments must be straight, collinear, forward, and on one validated plane.
Zero-length segments, reverse travel, curves, and heading changes reject corridor eligibility.
Ordinary lanes outside the corridor can retain curves and other vehicle classes.

Each site declares an ID, lane, start distance, end distance, and staging positions.
Distances use meters along the directed lane.
The entire protected site must fit the authored lane.
Its staging and maneuver envelopes must fit without entering a merge, berth branch, or unreserved resource.
Ordinary site entry and exit still use the existing lane, node, junction, and class checks.

Eligible corridor lanes and sites explicitly admit Compact vehicles.
The first implementation rejects Group or Express admission on the certified corridor and its protected sites.
Foreign traffic at adjacent junctions still needs the existing class-aware conflict and retention checks.
Reject a corridor when those external interactions cannot be proved safe.
An authored separation group cannot waive a conflict at a shared node.

The site validator computes required room from the approved body and maneuver profile.
It includes ordinary staging separation, connector spacing, closing travel, opening travel, and braking distance.
It also includes the one-tick motion term and external clearance at each protected boundary.
No fixed berth count, lane length, or point clearance substitutes for that calculation.
Reject short sites before allocating mutable simulation state.

Site capacity is one pair, under an exclusive site reservation.
A site is not a passenger berth or an extra parking berth.
Passengers board and alight only at existing compatible passenger berths.
The feature adds no boarding inside a moving train or during a site maneuver.

Site and corridor arrays each have a proposed maximum equal to the existing fleet bound of 300.
Every ID uses the existing bounded UTF-8 ID rule.
Corridor paths retain the existing route and project byte bounds.
The approved site and corridor arrays each retain this bound.
Existing coordinate, network block, node degree, junction-pair, and fleet limits do not increase.

## Formation and splitting

Both members must already follow routes that pass the assembly site and the complete certified corridor.
Do not add a coupling detour, reverse move, or destination change.
The common path must reach the split site before either route diverges or reaches a passenger stop.
Each member needs a structurally valid, class-compatible continuation after splitting.
An empty member needs a valid receiving berth under its existing relocation intent.

Passenger formation keeps each accepted party in its assigned cabin.
All active parties in either cabin retain their options, origin, destination, and timing.
Shared riders still need existing shared consent and per-cabin capacity.
Private riders remain private within their cabin.
The selected project option permits mechanical train travel without changing these cabin rules.

An empty pair has no active rider, assigned pickup, or pending order bound to either member.
A passenger pair has active passengers in both members.
Recruit neither mixed occupancy nor a member with a committed incompatible maneuver.
Dispatch, pickup reassignment, occupied pickups, and route replanning cannot alter a committed member until the group drains.
Pending orders remain pending under their existing admission and accounting rules.

| Phase | Required state and action |
| --- | --- |
| Candidate | Both members retain ordinary separation and ownership. Wait only within the declared partner deadline. |
| Closing | Atomically reserve the assembly site, corridor commitment, split site, and both exit holding positions. The head stops while the rear advances under the docking plan. |
| Latching | Both members stop at the approved pin spacing. Hold the complete site and both physical footprints for the dwell. |
| Connected | Commit immutable front-to-rear membership. Move the two bodies and connector in one train update. |
| Unlatching | Stop the complete train inside the reserved split site. Retain the latch geometry for the full dwell. |
| Opening | The front member advances under the opening plan while the rear holds. Keep exclusive site ownership until ordinary separation returns. |
| Draining | Transfer current and retained resources to the correct individual members. Release the site only after both tails and all dependencies clear. |

The corridor commitment covers the entire selected shared path before closing starts.
This conservative first design does not promise headway improvement on a long occupied corridor.
A later incremental grant design needs a separate stopping and deadlock proof.
Formation failure changes no accepted order, route, or external owner.
Before closing starts, a timeout returns the candidate to ordinary travel.
After closing starts, cancellation must finish a safe site maneuver before removing the group record.

Recruitment uses stable order by intent tick and pod ID after all eligibility checks.
A group keeps its ordered member IDs and formation tick until draining finishes.
Its identity is scoped to the existing simulation epoch.
There is no growth, member exchange, follower takeover, or in-motion split.
Do not clear membership while any protected site or train ownership dependency remains.

## Body, motion, and ownership proof

Each body is a centered rectangle with the approved length and width.
The connector is a rectangle between the two body-end pins, with the approved connector width.
The connected pair has fixed center spacing and one common heading.
The validator checks body nonpenetration and the intended connector attachment regions.
All external checks include both bodies and the connector.

Connected motion uses one longitudinal coordinate, one speed, and one braking decision.
Derive both member positions from that coordinate and the immutable spacing.
Plan from the same previous-tick state, then publish both member positions atomically.
Do not move the head and then let the follower chase its new position.
Docking and opening use separate bounded member trajectories inside the exclusive site.

The common speed cannot exceed any member's current lane limit.
The speed planner also checks every lower-speed lane that either member can enter before stopping.
Retain the discrete stopping term used by ordinary motion.
Do not snap a moving train to rest because a frontier or split site is too short.
Reject an uncommitted train plan without changing either member's motion state or existing owners.
A committed moving train brakes within its owned stopping frontier when a future grant is denied.
It releases resources only under the rear-aware release rules.
An invalid stopping frontier is an invariant failure, not a safe pause.
Stop qualification and report the violated invariant.
Do not publish an unproved next-tick motion or disguise that failure as a safe pause.

A train has a typed internal resource owner distinct from an individual pod owner.
The fleet still contains the original two pods, not an extra synthetic passenger vehicle.
Reserve the union of body, connector, braking, node, junction, and protected-site requirements.
Admission either grants the complete requested union or changes nothing.
An ordinary pod cannot borrow a train resource through the virtual predecessor rules.

For a straight pair, start with each ordinary release threshold plus the rear member's center offset.
Recompute the union and retain the maximum threshold for a resource used more than once.
The independent swept-body oracle must verify that this bound protects the connector and both bodies.
Reject a geometry case that the bound cannot cover.
Do not release a junction when only the front member clears it.
Preserve existing larger bounds at mixed-class interfaces.

At opening, transfer resources only after validating each member's current footprint and owned stopping frontier.
Retain a group record for any resource that still requires joint ownership.
Keep resource release after all movement, with no reuse during the releasing tick.
Train membership alone never permits foreign body overlap or owner substitution.

## Orders, metrics, and lifecycle

The train keeps the existing request IDs and per-pod request bindings.
It preserves party size, current private/shared consent, service ID, boarding records, and completed history.
Keep all orders whole and enforce capacity separately for each cabin.
Do not pool the eight combined seats to admit a party that cannot fit a single Compact pod.
Neither connecting nor splitting completes, boards, transfers, or requeues a party.

Distance meters follow each actual member trajectory.
Passenger, rider, empty, wait, and journey metrics retain their existing denominators and event definitions.
Formation wait and both dwells count as elapsed journey time for passengers already aboard.
Report intentional formation wait, latch time, split time, blocked split exits, and incomplete parties separately.
An aggregate benefit cannot hide individual delay or censoring.

The immutable coupling contract survives Clone and Reset, as the existing order contract does.
Clone deep-copies mutable groups, owners, timers, and maneuver plans.
Reset replaces the complete simulation epoch and clears its groups under existing reset semantics.
Reset is not an in-place uncoupling operation.

Disabling new coupling prevents recruitment without deleting current groups.
Connected groups continue under the approved profile to their reserved split sites.
Closing and opening groups finish their safe protected maneuvers.
Existing dwell timers and owner dependencies remain valid when the policy is off.
Do not teleport members, enlarge cabin capacity, or release shared resources on disable.

## Proposed opt-in formats and restoration

The proposed marker is `couplingContract:"compact-pair-v1"`.
It selects immutable physical rules independently of `orderContract:"express-v1"`.
The project exposes one explicit train option for both empty and passenger operation.
Omission keeps current projects and request semantics unchanged.
Existing projects do not migrate to the new family automatically.
Omit all new native, project, save, and stream members on current ordinary paths.
Preserve exact existing foundation and Express golden bytes, including empty and disabled controls.
Keep `PlatoonID`, `PlatoonIndex`, and current virtual-platoon counts tied to virtual links.
Expose mechanical membership, phases, and counts separately.

| Boundary | Proposed new family | Required distinction |
| --- | --- | --- |
| Project | Version 5 | Coupling marker, enabled option, bounded site and corridor descriptors. Express order selection remains independent. |
| Native constructors and restore | Additive contract-aware inputs | Validate both markers before geometry. Preserve all current foundation and Express helpers. |
| Native saved state | Optional marker and bounded group records | No new per-party option. Existing pods and orders keep their identities. |
| Session save | Version 8 | Strict group recognition and atomic physical reconstruction. Preserve versions 2 through 7. |
| Stream and browser assembler | Hello version 5 | Coherent group membership, phase, geometry profile, common speed, and ownership presentation. Preserve hello 3 and 4. |
| Offline car checkpoint | Existing version 1 stays unchanged | Reject the new project family. Its frozen native member list does not silently accept train fields. |

Version numbers are proposed next allocations at the audited source.
Freeze them again at implementation review if another approved format has landed.
Older families reject any new marker or train field, including explicit null or empty values.
Unknown markers, contradictory profile IDs, and unknown phases reject before state replacement.
The new writer never downgrades while a coupling project or retained group requires the new family.
Keep foundation raw order text and Express packed order text under their existing independent discriminator.
The coupling marker does not reinterpret either order encoding.

Full and delta frames publish the group registry and both member states as one coherent update.
The assembler validates membership and the profile before replacing its previous frame.
An invalid or incomplete update preserves the previous frame and requests normal stream recovery.
The browser draws the approved connector at the body pins, separately from the existing virtual-platoon line.
It shows phase and membership from authoritative state rather than distance-based visual inference.
Project validation must agree between native and WASM consumers.
This proposal adds no rail workflow or new passenger consent control.

A saved group contains its ordered member IDs, formation tick, phase, site and corridor IDs, and remaining dwell.
It also contains the bounded maneuver progress needed to validate saved member positions.
Derive connected offsets from the immutable profile rather than accepting arbitrary offset vectors.
Existing pod routes carry their continuations without a duplicate group route array.
Reject cycles, repeated members, reversed order, missing pods, incompatible paths, invalid positions, and conflicting owners.
The proposed group count is at most half the existing fleet count, with two members per group.

Physical restore validates every group and all current ownership atomically.
One invalid member rejects the entire incoming state without partial demotion or logical fallback.
Reconstruct the full current body footprint, retained past resources, site locks, and required forward grants.
Restore both connected members at common speed zero, following ordinary reconstruction semantics.
Docking and opening also restart from validated saved positions at zero speed under the remaining bounded maneuver.
Preserve membership, party facts, phase, and dwell time.
This proposal does not claim exact speed, future grants, or future trajectory parity after restore.

The first contract rejects explicit logical recovery while any group or group owner dependency remains.
It can use existing logical recovery after all groups drain and ordinary separation returns.
A future lossless train recovery path needs separate approval and repeated-recovery tests.
Never drop parties, detach close members, or ignore a malformed train certificate to recover a file.

Existing limits stay unchanged: 10 MiB project, 80 MiB raw and compressed save, 64 MiB raw stream, and 65 MiB binary stream.
Topology keeps its current 10 MiB plus 4 KiB cap.
Native foundation and Express order bounds remain their existing independent limits.
Measure combined widest encodings with the new records before calling any new family qualified.
Reject an oversized state atomically without trimming groups, routes, orders, or history.

## Qualification required before shipping

No test or service measurement for mechanical coupling has run in this design task.
Freeze the approved profile, final source, fixture hashes, and native/public binaries before qualification.
Each job records its command, working directory, PID namespace, watchdog, log, exit, and stop procedure.
Use the pinned Go toolchain and scoped race checks for the changed ownership and lifecycle code.

| Gate | Required positive and negative evidence | Compiled actual-caller mutation |
| --- | --- | --- |
| Contract and class | Old constructors and bytes match. Only opted-in Compact pairs recruit. | Bypass marker, pair-count, class, or existing-link eligibility at formation |
| Body and site | Real bodies, connector, closing, and opening fit the protected site. Curves and short sites reject. | Bypass site or connector-envelope validation at admission |
| Route commitment | Different destinations split before divergence. Denied grants preserve all routes and owners. | Remove common-path, split-site, or complete-union grant check |
| Common motion | One update preserves exact offsets. Every current and crossed lane obeys speed and stopping bounds. | Read only the head lane, move a member twice, or bypass braking at the motion caller |
| Ownership | Rear body and connector retain node, junction, track, and site resources. Foreign traffic cannot acquire them. | Release on head clearance or substitute an individual owner at release |
| Passenger cabin | Private/shared rules, per-cabin capacity, whole parties, request identities, and boarding history survive every phase. | Pool seats, bypass consent, or alter a member request binding |
| Disable and restore | Off at every phase drains safely. Every valid saved phase restores atomically. Invalid groups reject. | Clear group on disable, bypass restore proof, or enable partial logical fallback |
| Public formats | Native, session, save, topology, full/delta, HTTP, remote stream, WASM, and browser agree. Old families reject new fields. | Drop the new discriminator or apply a group delta before its coherent membership frame |
| Size and cost | Actual combined worst-case raw/gzip encodings, bounded scans, atomic failures, heap retention, and cancellation receipts | Bypass the real parser or writer cap at its caller |

Each mutation needs a passing control, successful mutant compilation, assertion failure, and restored source hashes.
A compilation failure or a preemptive unrelated rejection is not a killed mutation.
Classify surviving mutations and unresolved proof gaps explicitly.

Observe every native tick with an independent body and connector oracle.
Also check current-lane speed, acceleration, braking, owned stopping room, external separation, retained owners, and request conservation.
Test foreign crossings, blocked split exits, occupied berths, simultaneous candidates, and cancellation before and after latching.
Test both empty and passenger pairs, private cabins, shared cabins, different destinations, and mixed-class recruitment refusals.
Stop an unsafe arm and retain its complete failure receipt.

Before a service screen, freeze matched ordinary and coupling controls with identical offers, seeds, fleets, and simulated caps.
Compare assembly costs, passenger completions, individual waits and journeys, empty running, blocked junctions, and unresolved identities.
Record disabled-policy trajectory parity when no train forms.
Separate safety qualification from measured usefulness.
Do not infer throughput or energy benefit from the 4.5 m connected spacing.
