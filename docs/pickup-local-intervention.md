# Selected pickup reassignment exclusions

Refusing selected reassignment trials increases the chosen requests' waits in three reproduced histories.
Two requests then board their original assigned pods hundreds of seconds later.
The third boards an ordinary local pod, so refusal changes its wait by only 6.37 seconds.
These local results do not reverse the broader policy's individual-tail regressions.

## Intervention and checks

This follows the [service-tail diagnosis](pickup-tail-cases.md) at 10 requests per minute, seed 1.
The temporary study overlay refuses beneficial experimental reassignment involving one selected request from its recorded decision tick onward.
It refuses both sides of an assigned-pair swap before either assignment changes.
Ordinary dispatch, local pickup, reservation rules, demand, and safety checks remain active.
Refused trials still consume normal search work and budget.
No production source, project default, or saved format changes.

One unmodified control and three exclusion runs receive the same 3,599 requests.
The control reproduces every prior result, schedule, safety-check field, tail observation, and boarding observation.
Each exclusion's first prospective target trial matches the control's tick, pods, estimates, and serialized `ExportState` hash.
The hash compares the saved-state projection, not every runtime field or owner map.
Earlier recorded decisions and journeys completed before the intervention also match exactly.
All runs pass once-per-second safety and census checks and the two physical-restore checks with checked continuations.
Each selected request completes in both arms, and all requests board before the cap.

The overlay keeps evidence in each controller and reads immutable process configuration.
Focused tests verify transfer and paired refusal before, at, and after the threshold, including unchanged physical state and owners on refusal.
Those tests pass under the race detector.
The normal pickup tests pass with exclusion disabled, and overlay compilation and vet pass.
The overlay's evidence slice follows the controller's existing shallow copy during `Clone`.
This experiment does not clone active controllers, so it does not establish independent evidence ownership for that extension.
Concurrent functional jobs do not support CPU claims.

## Exact target waits

Each run records one refused beneficial trial.
The intervention specifies ongoing exclusion, although no second beneficial target trial is refused in these runs.

| Request | Control wait, seconds | Excluded wait, seconds | Increase, seconds | Excluded boarding pod |
| ---: | ---: | ---: | ---: | --- |
| 1366 | 1,730.97 | 1,737.33 | 6.37 | 089 |
| 1379 | 1,511.63 | 1,918.42 | 406.78 | 201 |
| 1845 | 1,158.80 | 2,617.12 | 1,458.32 | 070 |

Pod numbers abbreviate `london-pod-NNN` IDs.
For 1379, refusing the swap retains pod 201 instead of replacement 088.
The extra wait closely matches the original remaining-estimate difference of 406.78 seconds.
For 1845, refusal retains pod 070 instead of replacement 137.
Its extra wait closely matches the estimated 1,458.32-second benefit.

For 1366, the last pending observations retain assigned pod 243.
Pod 089 still collects the request through ordinary local pickup.
The incoming pod's original 2,025.21-second estimate therefore does not determine the realized boarding time.
The unmodified control also uses pod 089.

These results reconcile a useful local swap with a worse outcome against the independent baseline policy.
The independent baseline already has different pods available when these requests arrive.
This study does not identify which earlier decisions produced those fleet differences.

## Effects on other requests

Every global wait comparison includes all 3,599 boarded requests.
The exclusion changes later assignments and traffic, so other requests can improve or regress.

| Excluded request | Mean wait change, seconds | Largest individual increase, seconds | Requests increasing over 300 seconds | Completed, control / excluded |
| ---: | ---: | ---: | ---: | ---: |
| 1366 | -2.04 | 1,458.32 | 43 | 3,598 / 3,597 |
| 1379 | +0.49 | 1,629.27 | 46 | 3,598 / 3,598 |
| 1845 | -0.18 | 1,458.32 | 44 | 3,598 / 3,597 |

The small mean changes coexist with larger individual effects.
The 300-second threshold describes outcomes and is not an approved adoption limit.
These selected interventions do not estimate population benefit, prove optimal assignment, or establish a default policy.
Buffers and reassignment remain off by default.

## Evidence

The raw measurement data is in git history.
