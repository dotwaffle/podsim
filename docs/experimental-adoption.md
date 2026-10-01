# Experimental policy adoption gates

Status: qualification limits approved October 1, 2026.
These thresholds do not authorize a default change.
Station buffers, pickup reassignment, and sharing remain opt-in.
Free-flow routing remains the default.
Existing qualification rules and documented exceptions remain in force.
These gates add individual service limits and current-source coverage requirements.

## Evidence required

Freeze the candidate, baseline, projects, schedules, limits, and observation windows before each comparison.
Record source and binary hashes independently.
Use identical offered origins, destinations, arrival times, seeds, and party sizes.
Keep skipped offers, accepted requests, boarded requests, and completed requests separate.
Compare waits only when both requests board, and journeys only when both requests finish.
Keep all other matched requests as censored observations.
A censored request cannot pass an individual no-harm gate by omission.

Require all supported London demand bands, seeds 1 through 10, and the current baseline's qualified load range.
Include mixed Acton arrivals and departures, rail-hub bursts, the default live queue limit, and the study queue limit.
Test independent policies before their combinations.
Two selected seeds or a finite burst screen cannot replace this coverage.
Historical results remain evidence for their recorded source and fixtures.
They do not qualify a later implementation without a reproducibility bridge or a new matched run.

## Numerical limits

Apply every service gate to each matched arm pair, not only to pooled averages.
Times below are simulated seconds.

| Gate | Approved limit |
| --- | --- |
| Safety and accounting | No separation, speed, ownership, or request-conservation failure. No lost or duplicate order. |
| Restore fidelity | Physical tier with no demotions, requeues, or drops for every planned checkpoint. Existing members drain with policies disabled. |
| Finite recovery | Complete every request that the baseline completes by the same predeclared cap. Do not cross an existing baseline recovery deadline. |
| Live queue loss | Skip no additional offers compared with baseline. Report affected offered-event identities. |
| Aggregate service | Mean pickup wait, mean journey, and journey p95 increase by at most 2% in each pair. |
| Individual pickup wait | Additional wait is at most the larger of 30 seconds and 5% of that request's baseline wait. |
| Individual journey | Additional journey time is at most the larger of 60 seconds and 5% of that request's baseline journey. |
| Sustained backlog | Late growth is at most 0.1 orders/minute and increases by at most 0.05 orders/minute over baseline in each six-hour arrival run. |
| Capacity | No lower qualified rate in any demand band or seed. A finite recovery limit is not an indefinite capacity claim. |
| Shared detour | Retain the existing 1.5 maximum detour ratio for every completed shared party. |
| Disabled-path cost | Mean CPU increases by at most 2% in paired equal-work runs without concurrent studies. |
| Active-path cost | Retain the existing less-than-10% CPU increase gate. Report resident memory, GC pauses, and network rate separately. |
| Benefit | At least one qualified load or recovery-rate increase, or at least 5% lower mean pickup wait or journey time in a target workload. |

The individual limits permit small ordering changes while rejecting the hundreds-of-seconds regressions already observed.
The historical 300-second diagnostic counts are not adoption thresholds.
A lower completed maximum does not prove that each request improved.
A throughput gain does not waive a service, restore, or safety failure.
Do not extend the observation cap after seeing a failed result.

The backlog limits describe the selected late window.
They cannot prove stability beyond the measured duration.
Use a longer fixed confirmation run before any sustained capacity claim.
Report initial fleet placement and any station reserves because they can improve startup service without increasing sustained capacity.

## Decision and exceptions

Passing these limits does not authorize a default change.
Request separate approval before enabling an experimental policy by default.
Record each exception with its request IDs, mechanism, affected workloads, and separate approval.
An aggregate improvement or a historical exception does not approve a new exception.
If a candidate fails, retain its measured results and experimental control.
Fix a reproduced defect or choose a different candidate, then rerun the affected comparisons.

Current reports retain unresolved individual tails and overload cases.
See [dispatch qualification](dispatch-policy-qualification.md), [post-routing service](berth-routing-service.md), and [sharing rules](qualification.md).
The [fixed entry contract](station-entry-platoons.md) defines its additional save and ownership checks.
The [expanded controller screen](controller-capacity.md) retains live queue loss, censored requests, and individual-limit exceedances.
The [selected policy rerun](policy-failures.md) separates historical failures from these qualification limits.
The [Paddington trials](paddington-layout.md) reject unsafe partial fixtures while preserving individual regressions in the complete layouts.
The [predictive screen](predictive-service.md) finds no service gain in its six matched pairs.

## Later bounded candidate screens

The [pickup supply screen](pickup-supply.md) tests four current LondonFull fleet placements and sizes.
The [later buffer claims](buffer-late-claim.md) reduce sampled claim duration without a service gain.
The [pickup safeguards](pickup-guards.md) test request age and local idle-pod reserves independently.
The [shorter Paddington paths](paddington-short-path.md) include a focused mean gain that still exceeds individual limits.
No changed candidate passes its selected service screen.
These results do not trigger broader qualification or authorize default changes.
