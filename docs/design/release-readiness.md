# Release readiness

Status: release readiness on October 19, 2026, trimmed on November 4, 2026.
The report pinned `201031792a4c52aad621953b2f55ea897865ec55`.
Limit tables, version pins, hosted check results, and the CI failure history were removed.
Git history keeps them, and the [reference](../reference.md) and the [operations guide](../operations.md) give the current limits and versions.
This report does not authorize deployment, default adoption, or a cap change.
Physical coupling was removed on October 6, 2026, and the [platoon coupling comparison](../studies/platoon-coupling-comparison.md) is the decision record.
Express remains opt-in; see the [Express qualification](../studies/express-qualification.md).

## Browser candidates remain unaccepted

Four bounded browser performance candidates were screened on LondonFull (287 pods, 12 arrivals per minute, requested 60x playback, SwiftShader, two repeats per arm).
All four were rejected, and browser item 19 closed without an accepted optimization.

- The ordinary decoder cut CPU per message by 3.43%, but median RAF p95 rose from 50.10 to 58.35 ms and the WASM backing store rose from 238.60 to 247.67 MiB.
- The white-stamp label candidate failed exact pixel equality in 70 channels, by one level at most, and no appearance exception was granted.
- The original-glyph cache passed its pixel controls and cut label CPU by 28.65% and 26.01%, but median RAF p95 was 66.65 ms in both arms and long-task time rose from 2,147.5 to 2,260.5 ms.
- The strict decoder fusion cut native decode time from 3.47 to 1.94 ms, but both browser pairs improved RAF p95 by only 0.1 ms, which failed the frozen precision gate.
- The combination of strict fusion and the glyph cache lowered median RAF p95 from 66.6 to 58.4 ms, but the paired RAF p95 changes were +0.1 ms and -16.5 ms, and acceptance needed both to improve by more than 0.1 ms.

No candidate landed, no maximum candidate screen followed, and no unchanged repeat or gate exception occurred.
Software-renderer screens do not qualify physical GPUs, network delays, or broader workloads.

## Incident redesign

The maintainer stopped the incident redesign after stage 3.
Stages 1, 2, and 3 landed and are on main.
Stages 4 to 7 were dropped and have no contracts.

- Stage 1 is the [service transitions contract](../contracts/incident-service-transitions-contract-proposal.md).
  The maintainer approved it on October 5, 2026, in `482d93d`.
  It defines the service withdrawal and pickup release that vehicle faults and rider emergencies share.
- Stage 2 is the [suspension contract](../contracts/incident-suspension-contract-proposal.md).
  It adds pod faults and debris.
- Stage 3 is the [emergency contract](../contracts/incident-emergency-contract-proposal.md).
  It adds rider emergencies on ordinary pods.

## Forecast rejection and service boundaries

The private parking-only forecast candidate failed its frozen `3d9534e` seed-1 screen.
The three arms used one identified private executable:

| Arm | Completed requests | Queue skips |
| --- | ---: | ---: |
| Off | 363 | 1,077 |
| Current forecast | 364 | 1,076 |
| Parking-only candidate | 361 | 1,079 |

All arms passed native integrity checks, 1,355,220 total safety ticks, and eight physical restore probes.
The candidate lost 53 off-baseline completed identities and 60 current-forecast completed identities.
It added the same numbers of skips or refusals for those identities.
Identity, individual no-harm, and the required 5% benefit gates failed despite passing aggregate 2% comparisons.

Items 10 and conditional 11 closed with rejection.
No seed-2 study arm, repeated study, tuning, live integration, or adoption followed this failure.
The earlier prefix work remains separate evidence.
Restore probes cover accepted-cohort physical and twin controls, not the original run's complete offered future.

The archived shared forecast and Group service screens (see the [rejected candidates](experimental-adoption.md#rejected-candidates)) and the [policy reruns](../studies/policy-failures.md) retain failed service gates.
They do not qualify sustained capacity or default adoption.
The [adoption gates](experimental-adoption.md) still require matched service, loss, recovery, safety, and cost evidence.
Successful safety checks or aggregate results do not waive individual service failures.

## Car runs and release decision

`cmd/parkride` runs finite car itineraries.
Car checkpoint and resume were removed on November 4, 2026, with their executable identity checks.
Car runs refuse a project with the Express marker.
The container build covers the server, not the car CLI.

Browser performance work closed with rejection.
The incident redesign ended at stage 3.
Maximum Express costs limit responsive operating claims within the opt-in qualification.
Forecast and archived service failures still block experimental adoption and capacity claims.
No acceptance waiver follows from a successful build or a passing Check.

The [reference](../reference.md) records the 32 MiB project limit, and the [user guide](../user-guide.md) records the 43 MiB editor import limit.
The [dependency review](dependencies.md) retains its source-specific advisory and filesystem trust-boundary notes.

Deployment remains separately authorized.
Target health, storage, origins, resource limits, and rollback need a target-specific review.
