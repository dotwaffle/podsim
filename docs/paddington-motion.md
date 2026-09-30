# Paddington movement during frontier waits

Downstream owners usually keep moving while selected Paddington followers wait for reservation coverage.
The mirrored geometry produces more such waits.
Most samples do not involve a stopped terminal owner, and the probe does not establish a persistent reservation cycle.
No production safety rule changes.

## Method and checks

This extends the [leader progress probe](paddington-leader-progress.md).
Two short pilots compare the observer with the production run exactly.
Four full runs use earlier or mirrored Central geometry, seeds 1 and 2, and virtual platoons.
Each offers 10 requests per minute for six hours, with a seven-hour cap.
Buffers, reassignment, sharing, and redistribution remain off.

The observer follows the existing same-tick denial chain to its terminal owner.
For each selected stationary follower rejection, it samples that owner's physical movement constraints.
It reads after berth clearing and platoon-cap updates, before movement.
The activity and lane are current.
Station phase retains the production value before its end-of-tick refresh.

Each sample represents one follower rejection, not one unique downstream pod or one independent observation.
Several followers can therefore sample the same owner in one tick.
The counts do not measure total downstream stopped time.

Pure block lookups leave the simulation's cursors unchanged.
A regression verifies unchanged vehicles, ownership, physical exports, and cursors.
Both pilots match production results.
All four full runs match every prior result, ordered schedule, and existing progress-report field exactly.
Safety and request accounting pass once per simulated second.
Motion samples reconcile with every selected stationary rejection tick.
These checks add no physical-restore qualification.

## Current downstream state

A moving owner has speed at least 0.01 m/s before movement.
Available distance ends at the tighter reservation or platoon stopping boundary.
The table reports rejection-weighted samples.

| Seed | Geometry | Samples | Moving owner | More than 12 m available | Platoon boundary tighter |
| --- | --- | ---: | ---: | ---: | ---: |
| 1 | Earlier | 446,911 | 99.60% | 411,078 | 36,223 |
| 1 | Mirrored | 5,137,347 | 99.25% | 4,053,304 | 1,088,857 |
| 2 | Earlier | 429,921 | 99.45% | 382,356 | 47,989 |
| 2 | Mirrored | 3,901,503 | 99.32% | 3,207,477 | 697,408 |

Every sampled terminal owner remains in Traveling activity.
Many occupy Paddington's incoming road, access lane, or adjacent mainline.
The calculated next-tick speed permits movement in more than 99% of samples.
That calculation predicts speed from current bounds before boundary snapping or arrival.
It does not record a later trajectory.

Every sampled owner with a traveling predecessor has a mapped predecessor gap greater than 12 meters.
This gap uses the existing platoon route coordinates, not straight-line distance.
It does not establish the same gap for every upstream follower.
Samples with available distance at most 0.00001 meters remain rare, including 8,989 and 5,946 in the mirrored runs.

## Interpretation and remaining work

The observations are consistent with moving queues that transmit reservation-frontier waits upstream.
They do not support weakening the predecessor-coverage guard or treating each denial as a deadlock.
The earlier geometry intervention remains the causal evidence for the service difference.
These weighted measurements do not isolate one geometric parameter.

A useful next probe would record when each disputed cell clears and how far each immediate predecessor must move before release.
That would distinguish normal clearance delay from changes in certificate endpoints or station approach geometry.
Station-entry platoons still require the separate [design contract](station-entry-platoons-proposal.md).
The current buffer and reassignment defaults remain off.

[Arm counts](measurements/paddington-motion-arms.csv), [binned observations](measurements/paddington-motion-rows.csv), and [metadata](measurements/paddington-motion.json) retain the evidence.
Raw outputs and frozen helpers remain in `~/.cache/agents/podsim/paddington-motion-20260930/`.
Functional jobs overlap other checks, so their wall times are not CPU measurements.
Independent review verifies the pure lookups, matched results, weighted counts, and stated limits.
