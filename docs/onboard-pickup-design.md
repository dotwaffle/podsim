# Onboard pickup design

Status: implemented and qualified as an opt-in feature under the user's autonomous decision grant.
Native analysis and independent contract review identified the phase and stream rules below.
A project accepts `onboardPickups: true` with compatible drop-off sharing.
The omitted policy remains false.
The [matched service screen](onboard-pickup-service-screen.md) records the bounded result and qualification scope.
The user approved this feature and permits reviewed, noncontroversial contract details.
The foundation requires each rider's boarding berth and distance baseline before multiple origins become valid.

## Behavior

Add an opt-in project `onboardPickups` boolean, false when omitted.
An explicit member must be a Boolean, so null is refused.
It requires sharing above one party and drop-off mode.
Keep the existing party, seat, stop, detour, queue, and byte limits.
Do not change default dispatch or enable this policy automatically.

Pickup occurs only at a compatible passenger berth while the pod is stationary.
Complete eligible alighting before admitting another party.
Keep the berth and its ordinary resource protection during the existing boarding interval.
Each active party and the new party must have explicit shared consent.
Private parties cannot admit another party.
Use whole parties and the actual vehicle's class, seats, berth, and onward route.

Keep the existing join policy's assignment restriction.
The default accepts only unassigned requests.
Reassign-existing mode can release another pickup only when its current release rules permit it.
An express service cannot add a different origin or destination pair.
Current express physical profiles remain unsupported.

An admitted pickup must preserve every existing rider's destination and detour cap.
Evaluate each rider from its own boarding berth and distance baseline.
Do not measure a new rider from the pod's first origin.
Compute a candidate before changing any route, request binding, rider list, or resource owner.
A rejected candidate leaves those values unchanged.

## Boarding records

Create native boarding records only during the first successful occupied-pickup admission.
An existing active modern chain must prove one exact journey-origin berth and zero baselines for all retained riders.
Those retained riders may include completed display history from the same active chain.
Never promote completed-only old history.
Prepare this promotion and the new rider's record in one transaction.
Ordinary no-record journeys retain their existing getters, exports, and bytes.

Native boarding records use a berth ID and the current passenger chain's cumulative distance at boarding.
The chain begins at zero when an empty pod starts a new passenger journey.
The pod retains that distance across intermediate stops and occupied pickups.
It resets the chain only when a later empty boarding replaces the prior completed history.
Each rider's traveled distance is the current cumulative distance minus its boarding baseline.
Direct distance uses that rider's boarding berth and actual alighting berth.
Record completion distance at alighting for aggregate and experiment metrics.
Do not derive a completed rider's distance from the pod's later movement.
Publish individual traveled distance only for active riders unless an exact completion record is available.

Three save representations were considered:

| Representation | Benefit | Cost |
| --- | --- | --- |
| Berth ID and distance on each saved request | Direct references, easy inspection | Repeated escaped IDs exceed the available save margin |
| Per-pod dictionary of berth IDs and per-rider references | Removes repeated berth IDs | Distinct origins can still require eight full IDs per pod |
| Per-pod aligned berth-index and distance tuples | Fits the existing byte cap | Requires the saved project's berth order and exact rider alignment |

Use aligned tuples at the session boundary.
The independent design review confirmed this choice with the restore rules below.
Native saved and live records retain berth IDs.
The session save-6 adapter encodes optional `boardings` tuples against the bound source project.
Each item is exactly `[berthIndex, cumulativeMetersAtBoarding]`.
The array aligns with `SavedPod.Riders`, including retained completed history.
`berthIndex` selects a berth in the saved network station named by that rider's existing `From` member.
The saved project supplies this ordering, as it already supplies indexes for saved routes.
Export converts IDs to indexes against the exact saved network.
Session decode resolves indexes against that source network before it calls native restore.
Native `RestoreState` never accepts index-based boarding records.
Reject a source-project mismatch before decoding these references.
Reordering a caller's target network cannot reinterpret a boarding berth.

When `boardings` is present, omit the redundant pod-level `journeyOrigin` reference.
Require exactly one tuple for each stored rider, with at most eight tuples.
Each tuple has exactly two nonnull numbers.
Its first number is an integer from zero through the referenced station's berth count minus one.
Its second number is finite, nonnegative, and no greater than the restored chain's cumulative distance.
Validate station membership, berth compatibility, consent, and the selected physical placement.
Reject unknown members, mismatched lengths, dangling references, and contradictory origin representations.
Old save versions reject `boardings` presence, including null.

Reuse the existing `riddenMeters` field with explicit phase rules.
For occupied traveling pods, restore cumulative distance as `riddenMeters + distance`.
Require both terms and their sum to be finite.
Export already stores the base plus the trimmed route offset in `riddenMeters`.
For stationary boarding, unloading, continuing, and idle history, normalize local distance to zero and save the full cumulative value.
At final alighting, compute completion metrics before changing rider flags.
Then freeze the full cumulative distance in the existing distance base for record-bearing chains.
Do not change the physical route distance or release tails.
Completed-only recorded history uses that frozen value during idle, empty departure, and empty travel.
Empty travel saves the full frozen value separately from its physical route distance.
It never adds the empty-route distance to passenger distance.
Retain that value for completed history when boarding records exist.
Require the actual implementation to test these phase formulas before accepting the representation.
Do not add a separate odometer field without another byte calculation.

Existing same-origin saves without this array retain their current interpretation.
Do not invent historical boarding offsets.
Derive a baseline from an older same-origin save only when its recorded semantics prove the value.
Otherwise, refuse occupied extension until that passenger chain ends.
New records become necessary when riders have different boarding berths or distance baselines.
Canonical old-form export requires every retained baseline to be exactly zero and one proven exact boarding berth.
The old phase and origin fields must also preserve the same semantics.
Keep records when idle history cannot preserve that proof.
Do not rebase positive baselines after history retirement to shorten the encoding.
Canonical encoding does not remove authoritative native records.
A logical requeue retains the original order's consent and whole party.
It clears physical boarding metadata before a future boarding records a new baseline.

The combined rider and history array remains bounded at eight.
Before a new admission needs space, retire only completed display history, oldest first.
Remove each retired rider's aligned boarding record with it.
Retain completion counters, timings, and request conservation.
Never retire an active rider or modify an accepted party's consent.
Choose a sharing host from active riders, not the first retained history item.
Completed history must not forbid a later rider's legitimate destination revisit.
Apply that distinction only with validated per-rider boarding records.

An occupied pickup uses the current full boarding interval after alighting finishes.
Start the interval once per berth visit.
Additional admissions do not reset its clock.
It retains existing riders and their baselines while admitting the whole new party.
The boarding phase must accept different original stations only with validated boarding records.
Prepare rider, baseline, stop, route, and binding changes as one admission result.
Rejection must not retire history, release a pickup, or change ownership.
A policy-off restore preserves an accepted boarding interval and its records while refusing new occupied pickups.

## Byte and stream checks

A Go JSON-v2 overlay measured the widest eight-tuple representation against the existing save-6 fixture.
It removes `journeyOrigin` and the mutually exclusive closed-cohort marker of that fixture.
Saved state no longer has that marker.
The hypothetical pod is 169 bytes smaller than the historical maximum pod.
The final compact typed fixture measures 83,698,502 bytes, leaving 187,578 bytes.
The tuple estimate does not replace a combined typed fixture.
The existing cap is 83,886,080 bytes.
These independent maxima bound encoding size.
They do not describe a physically reachable state.
The evidence is `onboard-boarding-byte-design.log` in the current service evidence cache.

This estimate covers the tuple shape only.
It does not qualify the native state or its phase rules.
It does not replace permanent format and byte tests.
Before implementation lands, compare historical and modern maxima with the actual encoded types.
Include compact certificates and retained rider history in that test.
Keep the 2,600 saved-queue and eight stored-rider operating bounds.
Add optional hello-3 vehicle fields `Boardings` and `RiddenMeters`.
`Boardings` contains aligned native records with `BerthID` and `MetersAtBoarding`.
These records use berth IDs, so stream decoding does not interpret positional indexes.
`RiddenMeters` holds the passenger chain's cumulative distance.
Omit its zero value.
A boarding record proves the zero baseline when the cumulative value is omitted.
Publish these fields together only when the canonical old same-origin representation cannot preserve the records.
Without boarding records, retain the existing vehicle representation.

Require one boarding record per stored rider, with at most eight records.
Resolve each `BerthID` against that frame's bound topology and the rider's `From` station.
Reject unknown, missing, duplicate, or null object members and invalid numeric values.
Require finite, nonnegative baselines no greater than the cumulative distance.
Repeated berth IDs are valid for separate parties at the same berth.
Unknown historical cohorts cannot carry these records.
A completed record does not supply a current traveled distance.
The browser shows a derived distance only for active riders.

Require paired replacements when either aligned array changes and the previous or resulting vehicle has boarding records.
Ordinary riders-only updates remain valid when neither side has records.
Use `boardings: {"value": []}` to clear records.
Reject null or missing replacement values and unknown wrapper members.
Preserve the existing rider-clearing syntax.
Normalize a boarding clear to an absent full-frame field.
Entering or leaving the record representation requires both aligned replacements.
The vehicle metadata replacement carries `RiddenMeters` with the other bounded metadata.
Metadata-only cumulative updates do not require unchanged arrays again.
A nonzero cumulative value requires boarding records.
Clearing records must also clear a stale nonzero cumulative value.
Validate the reconstructed complete vehicle before publishing either replacement.
A full frame supplies both arrays in the same vehicle.
A reconnect or topology revision invalidates prior berth bindings.
Record the effective occupied-pickup policy in comparison provenance.
Older stream versions reject new field presence before topology fetch or state publication.
These guards must check the proper vehicle, delta, and boarding-record paths.
Do not classify `BerthID` globally as a new field because older pod fields already use that name.
The saved `riddenMeters` field also predates boarding records.
Replace riders and their boarding metadata together in a delta.
Resolve berth references against the topology for that frame before publication.
Measure both full and delta frames against the existing 64 MiB cap.

## Required tests

Test two actual origins, intermediate alighting, per-rider distances, detour rejection, and eventual completion.
Test private orders, class-incompatible berths, full seats, and assignment restrictions.
Check route, binding, ownership, and rider arrays before and after a rejected candidate.
Test eight stored riders with completed history and repeated pickups without history growth.
Restore during boarding, travel, and intermediate unloading, then compare continued runs.
Test reordered or changed projects, invalid indexes, mismatched tuples, null values, and old-version presence.
Verify rail binding, request identity, completion timings, and conservation across logical fallback.
Run matched offers with the policy off and on, recording effective consent in each result.
Report completion, pickup and journey tails, detours, occupancy, and empty running separately.
No service or capacity claim precedes those checks.
