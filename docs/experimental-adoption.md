# Experimental policy adoption gates

Status: qualification limits approved October 1, 2026.
These thresholds do not authorize a default change.
Pickup reassignment and sharing remain opt-in.
Free-flow routing remains the default.
Existing qualification rules and documented exceptions remain in force.
Station buffers and compact station queues were removed on October 7, 2026, because measurements showed that they lowered station entry throughput.
Gates and rows that name them are history.
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
See [dispatch qualification](dispatch-policy-qualification.md), [post-routing service](berth-route-preference.md#stratford-request-1324), and [sharing rules](qualification.md).
The [expanded controller screen](#rejected-candidates) retains live queue loss, censored requests, and individual-limit exceedances.
The [selected policy rerun](policy-failures.md) separates historical failures from these qualification limits.
The [Paddington trials](#rejected-candidates) reject unsafe partial fixtures while preserving individual regressions in the complete layouts.
The [predictive screen](#rejected-candidates) finds no service gain in its six matched pairs.

## Later bounded candidate screens

The [pickup supply screen](pickup-supply.md) tests four current LondonFull fleet placements and sizes.
The [later buffer claims](buffer-late-claim.md) reduce sampled claim duration without a service gain.
The [pickup safeguards](#rejected-candidates) test request age and local idle-pod reserves independently.
The [shorter Paddington paths](#rejected-candidates) include a focused mean gain that still exceeds individual limits.
No changed candidate passes its selected service screen.
These results do not trigger broader qualification or authorize default changes.

## Rejected candidates

Each row is a closed study that changed no default.
The full record is in git history, for example `git show a3ce698:docs/paddington-layout.md`.
The records that the buffer removal of October 7, 2026 retired are at `0f5ae61`, for example `git show 0f5ae61:docs/compact-queue-speed-fixed-screen.md`.

| Study | Source | Workload | Measured result | Decision | Record |
| --- | --- | --- | --- | --- | --- |
| Paddington position trials | `a54281e`; earlier intervention `7cd5fe9` | LondonCentral Early 10/min, 6 h, 7 h cap, seeds 1 and 2, mirrored against restored positions | Restoring all eight positions: 2,895 against 2,661 completed (seed 1) and 2,901 against 2,758 (seed 2); late backlog growth stays positive at 2.839 and 2.761 requests/min; individual wait limits flag 36 and 54 matched requests; both partial fixtures fail a live safety check at 531 s and 546 s (pods 11.74649 m and 11.40091 m apart) | Reject both partial fixtures; no geometry change and no independent berth banks; the full restoration keeps individual regressions | `docs/paddington-layout.md` |
| Shorter Paddington paths | `2cfe9e4` (Go code matches `943fd74`) | LondonFull 287 pods; AM12 seeds 1 and 4, 6 h, 8 h cap; focused Paddington mixed 4/min waves, seed 3, 2 h, 3 h cap; pair scale 0.90 and 0.80 | The 0.90 layout cuts focused mean pickup wait by 8.38 s (16.10%) but finishes 585 s later in AM12 seed 4; 3 focused pickup exceedances; AM12 pickup exceedances 446 to 697 | No five-percent gain in either AM screen and individual limits exceeded; keep authored geometry | `docs/paddington-short-path.md` |
| Paddington reservation and resource histories | `bd2bd7c`, `bbcb9e8`, `9cb066f` | LondonCentral Early 10/min, 6 h, 7 h cap, seeds 1 and 2, earlier against mirrored geometry | Mirrored geometry raises shared-guard attempts by factors of 5.08 and 4.18; predecessor-frontier failures are 81.7% and 78.0% of them; owners keep moving in 99.25% to 99.60% of samples; mean release distance of the immediate predecessor is 11.49 to 12.25 m across the four rows; all 128 selected histories reach their original frontier within 24 s | Diagnosis only: no basis to weaken clearance or the predecessor-coverage guard; the geometric cause is not isolated; the historical parity check applies to the four virtual arms | `docs/paddington-resource-history.md` |
| Expanded controller and capacity screen | `a54281e`; supply replay `e285e69` | LondonFull Morning10, AM12, PM14 seed 3, 3 h, 4 h cap, 24 arms; Acton mixed 16/min seed 3, 2 h, 3 h cap; AM13 to AM15 seeds 3 and 4, 6 h, 7 h cap | Acton at queue 200: reassignment alone completes 83 more requests, buffers alone 109 fewer, both 132 fewer; with queue 1,000,000 late backlog grows 9.48 to 10.67 requests/min; Full arms keep 259 to 881 wait-limit exceedances; AM14 and AM15 late backlog growth +1.200 to +3.267 requests/min | No default adoption and no capacity-limit increase; AM14 and AM15 are limited by pickup supply | `docs/controller-capacity.md` |
| LondonFull controller sustained load | `bab8559` | LondonFull AM Peak, 10, 15, 20/min and intermediate 12, 13, 14/min, 6 h, 1 h recovery, queue 1,000,000, seeds 1 and 2 (12/min seeds 1 to 4) | At 10/min late backlog stays within -0.12 to +0.04 orders/min; at 15 and 20/min every policy grows a backlog (+2.14 to +3.12 and +7.28 to +8.33 orders/min); 12/min seed 4 request 1324 with reassignment alone: +5,404.73 s (481.02 to 5,885.75 s), an indirect effect of different fleet histories; pre-routing 14/min seed-2 baseline backlog +1.683 orders/min | Buffers and reassignment stay off; no sustainable-rate claim | `docs/london-full-controller-sustained.md` |
| LondonFull unfinished-request diagnosis | `352cdaa` (simulation code as `677e62b`) | Four archived arms, 60 min of arrivals plus 60 min of drain, continued up to 2 h | One parking-diversion deadlock (request 577) and three journeys that finish 222.20, 266.57, and 620.98 s after the 7,200 s cutoff | Fix applied to the diversion guard (entry-node check); no preset or fleet change; the long journeys are finite-window misses | `docs/london-full-diagnosis.md` |
| Pickup reassignment safeguards | `57d31bb` (Go code matches `943fd74`) | LondonFull 287 pods, AM 10/min, 6 h, 8 h cap, seeds 1 and 4; age guard (30 s) and last-idle-pod reserve guard against off and current on | Mean pickup 298.75 s (age) and 298.71 s (reserve) against 301.87 s off (seed 1); pickup exceedances 382 to 597 and journey exceedances 235 to 407 in every comparison; reserve guard fails the recovery gate by 25 s and 15 s in seed 1 | No five-percent gain and individual limits fail; reassignment stays off | `docs/pickup-guards.md` |
| Pickup tails | `69845f7`, `bab8559` (10/min seed 1); `a54281e`, `e285e69` (12/min seed 4) | LondonFull AM, reassignment against off, selected requests 1366, 1379, 1565, 1845, 2062 and 1324, 3651, 2175, 3421 | Added waits +716.28 to +1,457.07 s (10/min) and +217.80 to +1,629.73 s (12/min); excluding one swap raises request 1845 wait by 1,458.32 s; suppressing swap pair 136/239 leaves request 3651 1,168.85 s worse than swaps off | Long waits follow earlier fleet-availability changes; the observations do not establish a station-reservation or movement defect; no production fix; reassignment and buffers stay off | `docs/pickup-postroute-tail.md` |
| Selected predictive-routing service comparisons | `a54281e`; diagnosis `e285e69` | LondonCentral Early 10/min and LondonFull Morning 13 and 14/min, seeds 1 and 2, 6 h, 7 h cap; Acton mixed burst, 2 h, 3 h cap | 40,794 evaluations change no request, boarding tick, or completion tick (39,501 forecast no delay, 18 distinct candidates save less than 2.8 s); Acton completes 1,012 against 1,029 journeys with 418 alternatives returned | No service or capacity benefit; free-flow routing stays the default | `docs/predictive-service.md` |
| Occupied pickup service screen with competing pods | `5a57e4c` | Example network, 2 and 3 legacy 4 m pods, 4 waves, 900 s horizon | 2 pods: pickup p95 201.867 s to 36.050 s; 3 pods: 184.850 s to 121.250 s; the final Garden-to-Harbor party waits 106.400 s longer and completes 94.033 s later | The 2-pod fixture passes; the 3-pod fixture fails the individual pickup and journey limits; the policy stays opt-in | `docs/occupied-pickup-multipod-screen.md` |
| Onboard pickup service screen | Not named in the record | Example network, one 4 m pod, three manual orders, 600 s | Maximum pickup wait 225.57 s to 46.05 s and empty distance 1,844.21 m to 0 m with shared consent; no change with private consent | Bounded result; no network capacity or default adoption claim | `docs/onboard-pickup-service-screen.md` |
| Station buffers and compact station queues | `2a9d6b9`, `0f5ae61` | LondonFull user project, platoon limit 4, pickup reassignment on; 60 orders/min to one station, 30 min, first 10 min excluded | King's Cross entries/min: 4.48 buffers off (121 completed), 3.99 buffers on with compact spacing, 3.87 with ordinary spacing (105 completed); Waterloo off 4.40 to 4.50; two pods on the entry lane in 5 of 18,000 samples with compact spacing | Removed on October 7, 2026: buffers lowered station entry throughput and links almost never formed | `docs/buffer-late-claim.md` (kept as the decision record) |
| Compact queue speed screen after the lane-speed fix | `30d8e34` | Seven compact pods, 28 accepted requests, 2,400 s cap; two ordinary and compact-v1 pairs with buffers and virtual platoons on (limit 4), queue limit 200 | All four arms complete 4 of 28 requests by the cap; compact groups first appear at 502.65 s and keep three occupied members; censored requests and failed disabled-policy drainage | No compact queue service benefit; feature removed on October 7, 2026 | `docs/compact-queue-speed-fixed-screen.md`, with `docs/measurements/compact-queue-speed-fixed-screen.json` |
| Station buffer contract and fixed entry platoons | `0f5ae61` | Saved-state membership contract (approved September 29 and 30, 2026), fixed station-entry platoons, and recruitment inside stopped buffers | Closed records of the removed feature | Retired with the feature on October 7, 2026 | `docs/station-buffer-state-proposal.md`, `docs/station-entry-platoons.md`, `docs/station-buffer-recruitment.md` |
| Compact-v1 contracts | `0f5ae61` | Retained physical recovery state (approved October 2, 2026), the compact pilot note, and the compact section of the service contract proposals | Closed records of the removed feature | Retired with the feature on October 7, 2026; the section of `docs/service-contract-proposals.md` is at `0f5ae61` | `docs/compact-recovery-state-contract-proposal.md`, `docs/compact-station-queues.md` |
