# Emergency claim yield amendment proposal

Status: draft for the maintainer, 2026-11-24.
The maintainer approved option A of the [emergency berth claim proposal](emergency-berth-claim-proposal.md) in principle on 2026-11-24, and asked to approve this text at the end.
The text of the approved contracts does not change before that.
The code is on the local branch with this draft and is not pushed.

## The rule

A pod that travels to the berth of an emergency unload counts as a passenger arrival at that berth.
So a relocating pod in service gives up a service claim on it, as it does for an ordinary passenger.
The yield is the existing one: `yieldRelocationClaims`, in `redistribute`, before admission.

The emergency pod counts when all of these hold:

- Its purpose is an emergency unload (purpose 1).
  In a valid state, such a pod travels with active riders until it unloads.
- Its only hold is the emergency hold.
  A fault adds its hold, and the pod does not count until the clear.
- The berth is its destination.

A deferred record has no purpose, so its pod counts for nothing, also when it keeps an ordinary destination.
A refuge, an empty recovery, and each other withdrawn pod also count for nothing.

What stays:

- A withdrawn relocating pod gives up no claim.
- A resource that is occupied, stopping, retained, or lent stays.
- No grant is revoked, and admission still takes no owned resource.
  The claim is gone before admission, and another pod can still get the berth first.
- The yield gives the released or guarded-rebalancing claimant the existing search for another place (`parkReleased`), which can change its destination and its route.
  When the search finds no place, the pod keeps its route, and it does not get the claim back.
  A plain claimant keeps its route and has no claim.

## Text to change

The places are those of the contract documents on 2026-11-24.

### Service transitions

| Place | Now | New |
| --- | --- | --- |
| Section 1.3, decision 2 | It never revokes the claims of other vehicles. | It never revokes a grant or a protected claim of another vehicle. By the maintainer's approval of 2026-11-24, a revocable service claim of a relocating pod in service yields to a traveling pod that is bound to an emergency unload at that berth and whose only hold is the emergency hold. |
| Section 4.3, row "Passenger arrivals" | A withdrawn pod with riders is not a passenger arrival, so it cannot make a relocation claim yield | A withdrawn pod with riders is not a passenger arrival, except a pod bound to an emergency unload whose only hold is the emergency hold (the exception) |
| Section 4.3, row "Claim yield to passengers" | Skips a withdrawn relocating pod. | No change. |
| Section 4.3, after the table | Section 6.2 gives the reason: decision 2 allows no yield for emergency demand. | Section 6.2 gives the reason: decision 2 allows a yield only for the exception. |
| Section 4.5, W3 | no withdrawn pod authorizes a claim yield | no withdrawn pod authorizes a claim yield, other than the exception |
| Section 6.2, row `yieldRelocationClaims` | The conflict test uses `passengerArrivals`, which no longer counts withdrawn pods | The conflict test uses `passengerArrivals`, which counts no withdrawn pod other than the exception |
| Section 6.2 | Decision 2 means that emergency demand never authorizes a yield of another pod's claim. One existing path gives that authority to passenger demand, and it excludes withdrawn pods: | Decision 2 means that emergency demand authorizes a yield only under the exception. One existing path gives that authority, and it excludes each other withdrawn pod: |
| Section 6.2, the bullet | A withdrawn pod with riders is not a passenger arrival [...] so it cannot make a relocating pod release its destination claims | Add: "other than the exception" |
| Test row "No yield for emergency demand" | A withdrawn pod with riders goes to that berth, as a traveling arrival. The claim stays. | Split: a withdrawn pod with no emergency binding keeps the claim. A bound emergency unload with only the emergency hold makes it yield. |
| Section 14.6, mutation "Count a withdrawn pod as a passenger arrival" | Must fail a test | Keep for each pod other than the exception |

### Suspension

| Place | Now | New |
| --- | --- | --- |
| Section 9.1, routes | The route of a healthy pod changes for an incident only through the endpoint reroute (section 9.3). | Add: "and through the parking search of a claimant that yields to the exception of the service transitions contract". |
| Section 9.1, claims | A healthy pod gives up a claim for an incident only through its own surrender of an unused service claim (section 9.5). | Add: "or through the yield to that exception". |
| Section 9.5 | A withdrawn head still makes no other pod yield: a berth becomes free only by the choice of its own owner. | "The surrender makes no other pod yield. A bound emergency unload can make one yield before it, by the exception." The surrender does not change. |

### Emergency

| Place | Now | New |
| --- | --- | --- |
| Section 1.2, last row | The revocation of a grant or a claim of another pod: Never (decision 1) | The revocation of a grant of another pod: never (decision 1). The exception of the service transitions contract yields a service claim before admission. |
| Section 1.3, decision 1 | It never revokes the grants of other pods. | No change |
| Section 18.1, R1 | The emergency pod gets free resources first and never takes an owned one. | Add: "A service claim of a relocating pod can end before admission, by the exception, and then it is not owned." |

The berth claim deadlock contract, P13, and the saved format do not change.

## Documents to update on approval

- `docs/model.md`: the sentence that an emergency never takes an owned resource keeps its admission rule and names the exception.
- The comment of `surrenderWaitingClaims`.

## Evidence

The tests are in `internal/sim/emergency_yield_test.go`.
Each test that steps uses the full `Step`.

| Test | Shows |
| --- | --- |
| `TestEmergencyYieldsRemoteClaim` | A plain, a rebalancing, and a released claimant give up the claim, the emergency pod never waits on the far pod, and both end in a berth. |
| `TestEmergencyYieldTwoBerths` | The HX11 arrangement: the claim on one berth and an idle pod in the other. |
| `TestEmergencyYieldCompetingPickup`, `TestEmergencyYieldTwoEmergencies` | All of them finish. The second has the serial order against the pod order. |
| `TestEmergencyYieldNoParking` | The claimant gives up the claim and keeps its route, and it gets a place after the blocked places clear. |
| `TestEmergencyYieldLinkedClaimant` | A linked claimant gives up the claim and keeps its route. |
| `TestEmergencyYieldFaultClear`, `TestEmergencyYieldLimits` | The fault lifecycle, and each pod that does not count. |
| `TestEmergencyYieldKeepsRetainedNode`, `TestEmergencyYieldKeepsStoppingNode` | Protected resources stay. |
| `TestEmergencyYieldRestore` | A physical restore before and after the yield gives equal exported states for 120 s, with a released claimant and with a rebalancing one. |

Mutations, each reverted after it failed a test: no exception (9 tests), no hold test, purpose other than 1, no purpose test, no in-service test of the claimant, no revocable test, and no activity test.
`TestEmergencyYieldCompetingPickup` also passes with no exception, because a pickup makes the claim yield.

The [pilot](../studies/emergency-claim-pilot.md) ended the HX11 wait at 59.7 s and not 653.0 s.
It had no test of the fault hold.
`TestPresetBehaviorParity` and `TestLondonExpressBehaviorParity` have no change, and one of their scripts has an emergency.

The cycle of a claimant behind a pickup is not in these tests.
The refusal of a claim on a targeted berth, which the berth claim deadlock contract added, stays, and its own test covers it.

## Not shown

- The encoded session state before and after a yield, and the restore of a linked claimant.
- The late record `i1.17` of seed 2, in a run without this code ([pilot](../studies/emergency-claim-pilot.md#a-second-late-record)).
- The release seeds of HX11, the soaks of the other presets, and the CI case of the wharf preset with this code.
- That a claimant always makes progress at full parking.
- A run with no emergency is unchanged by the predicate.
  This is from the code, and no run compares it other than the parity tests.
