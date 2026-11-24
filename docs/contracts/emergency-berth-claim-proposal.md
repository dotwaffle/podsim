# Emergency berth claim proposal

Status: draft for the maintainer, 2026-11-24.
Nothing is approved or built, and no run has tested an option of this note.
The maintainer chose this note as the next step after the HX11 failure.

## The problem

Record `i4.93` of the HX11 timing seed waited 653 s not blocked by a fault, and the Progress limit is 600 s.
The [trace](../studies/incident-soak.md#the-late-record-of-the-wharf-timing-seed) shows why.
The emergency pod was bound for Devons Road, which has 2 berths.
An empty pod 8.7 km away had a claim on berth 01, and an idle empty pod stood in berth 02.
The emergency pod waited at the station until the far pod arrived and left again.

If the claim of the far pod was a revocable relocation claim, which the trace suggests and does not log, an ordinary passenger pod would have made it yield.
A relocating pod in service yields such a claim when a pod in service brings a passenger to the berth (`yieldRelocationClaims`, `internal/sim/redistribution.go`).
An emergency pod is withdrawn, so `passengerArrivals` does not count it, and it gets less from this rule than an ordinary trip does.

## What the contracts say

| Rule | Place | Effect on the record |
| --- | --- | --- |
| A withdrawn pod is not a passenger arrival. | [Service transitions](incident-service-transitions-contract-proposal.md), sections 4.3 and 6.2, and decision 2 | The claim of the far pod does not yield. |
| An emergency pod wins free resources and never revokes a grant. | [Emergency](incident-emergency-contract-proposal.md), decision 1 and section 9.3 | It has priority for berth 01 only when the claim is gone. |
| The station choice has no berth term. | Emergency, P13 and section 9.1 | The claim costs nothing in the choice. The reason given is that terminal reevaluation takes the next free berth. |
| A waiting healthy pod surrenders its own service claim. | [Suspension](incident-suspension-contract-proposal.md), section 9.5 | The far pod was not waiting, so the rule does not apply to it. |
| A claim of a healthy pod ends only through its own surrender, and a withdrawn head makes no other pod yield. | Suspension, section 9.5 and the rules before it | Option A conflicts with this too, and with the withdrawal rule W3 of service transitions. |
| Berth clearing moves the pod that blocks the route of the arrival. | Suspension, section 9.4, and `clearBlockedBerths` | The blocker is the far pod, which is not idle. The idle pod in berth 02 is not the blocker. |

Section 9.1 of the emergency contract relies on a next free berth.
In this state no berth is free, and no rule makes one free before the far pod arrives.

## Options

| Option | Rule | For | Against |
| --- | --- | --- | --- |
| A | `passengerArrivals` also counts a withdrawn pod whose operational purpose is an emergency unload, at its bound berth. The relocating pod then yields its revocable claims as it does for a passenger. | Smallest change. It uses an existing yield path, which keeps admitted destinations and withdrawn pods safe. It gives an emergency the treatment of a trip. It covers a station with one berth. | It amends decision 2 of the service transitions contract. It can change the trajectory of a run with an emergency. It does not move an idle pod. It removes the claim, and does not reserve the berth or guarantee the limit. |
| A2 | As A, but only when the next reservation of the bound emergency pod needs the berth. | It leaves far claims alone until the pod is at the station. | The same amendment, and more arbitration in admission. Not measured. |
| B | The station choice adds a cost to a berth that is claimed or occupied. | It sends the pod elsewhere before it waits. | It amends P13, which rejected a berth term. The choice is made once, so a claim made after it has no effect. Its cost value has no measurement, and the candidate selection, the rounding, and the pruning equivalence need review. |
| C | For an emergency arrival that waits with "Berth occupied", `clearBlockedBerths` also moves an idle, unassigned, empty pod from another berth that the arrival can reach. | It needs no yield of a claim and no change of P13. | It is a direction and not a complete rule: it needs a candidate order, a parking place, a branch that is still open, and a rule for when the pod changes its target. The sibling berth is not a target until then, so another pod can take it. It helps only when a sibling berth has such a pod. The trace does not show whether the pod in berth 02 was unassigned. It amends section 9.4 of the suspension contract. |
| D | The claim surrender of section 9.5 also applies to a pod that does not wait but is far from its berth. | It keeps the wording that only the owner gives up its claim. | It depends on the distance of the owner and not on the demand of the emergency. It needs a distance constant, and it acts on pods that block nobody. |
| E | No change, and an exception for HX11. | No code. | The Progress gate of the release soak then has a known miss, on rules that each preset has. |

A larger change, that an empty move gets its berth at the approach, is in [late berth binding](../design/late-berth-binding.md), and it is closed and not pursued.

## Recommendation

Option A, as an amendment of decision 2 of service transitions and of the matching clauses of the suspension contract, limited to a revocable relocation claim and to purpose 1.
An amendment of decision 2 alone leaves contradictory text.
Decision 2 says that emergency priority "never revokes the claims of other vehicles", so option A narrows a decision of the maintainer of October 4, 2026, and needs their approval by name.
Decision 1 of the emergency contract says "grants", and a relocation claim is not a grant, so that text can stay.
Option C is a possible second step, only if a run with A still has a late record behind an idle pod.

The yield is limited to the cases where `yieldRelocationClaims` already yields.
It skips a withdrawn relocating pod, and a claim whose destination block is admitted is not revocable.
A deferred emergency pod has no emergency binding, so it makes nobody yield, also when it keeps an ordinary destination berth.

## What follows a yield

- Dispatch and pickup swaps run before the yield in a tick, guarded positioning after it, then admission, and then berth clearing.
- Guarded positioning, claiming empty moves, and released parking exclude a berth that a moving pod targets, so they do not claim the berth again while the emergency pod targets it.
- The emergency intent goes first, but it gets nothing when another resource of its span has an owner, and a later intent can then take the berth.
- A pickup can enter the berth in an earlier tick, and a claimant that is neither released nor rebalancing keeps its route and can win the berth later.

So A removes one obstacle, and only a run shows whether the record then ends in time.
A wider hold of a berth was measured and rejected in the emergency contract, and this note does not propose one.

## What a build must check

- Full-`Step` tests that end in an emergency unload and in progress of the claimant: a station with one berth, the two berths of the trace, a competing pickup, guarded, released, and plain relocations, and no other parking place.
- The yield in the same tick and in a later tick, with another resource of the span owned.
- A test that a fault recovery, a refuge, a withdrawn owner, and a deferred record with an ordinary destination make no claim yield.
- Each protected claim kind stays: occupied, stopping, retained, and lent, with linked claimants and more than one emergency serial.
- The cycle of a claimant behind a pickup of the [berth claim deadlock](berth-claim-deadlock-proposal.md) does not come back.
- Mutations of the new clause and of each protection.
- An encoded save and restore before and after a yield, also with a failed parking and with linked claimants, and a lockstep run with pickup swaps off.
  `ClaimsDestination` already records a yield, so no state version change is expected.
- The incidents arm of the HX11 timing seed, and then both release seeds, and the soaks of the other presets.
- Parity: `TestPresetBehaviorParity` has an emergency in its shared-track script, so its bytes can move, and each difference needs a cause before a golden update.
  The cases with no emergency must not move.

## Decisions for the maintainer

| ID | Question | Options | Recommendation |
| --- | --- | --- | --- |
| EB-D1 | Which rule | A to E above | A |
| EB-D2 | Before the amendment | A: pilot option A as an uncommitted patch on the incidents arm of seed 1, with the relocation, links, and owner of each berth resource in the trace. B: approve the amendment first. | A, because one run then shows whether the record still waits |
| EB-D3 | The rows to run again after a build | A: HX10 and HX11. B: also the soak rows of the other presets. | B, because the rule is not specific to the wharf |
