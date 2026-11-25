# Berth displacement

Status: design note of 2026-11-25, for a decision of the maintainer.
Nothing of it is approved or built.
It follows the [distant berth target proposal](../contracts/distant-berth-target-proposal.md) and the [study of link 100](../studies/link-100-queue.md).

## The question

At its terminal branch, a pod refuses a berth when another pod that is not idle has it as its destination (`berthAvailableFor`).
A rule that ignores a distant pod passed the release form of HX11 on an uncommitted tree, and three reviews each found a pod that the rule leaves waiting:

| Round | Case |
| --- | --- |
| R1 | The ignored pod is an ordinary empty move that gave up its claim. It cannot choose again. |
| R2 | The ignored pod has only one berth that it can use. |
| R3 | 01 is at the branch for A. 02 behind it targets B and can use A. 03 behind both is a claimless empty move to A. There is no other place. 01 takes B, and 02 then refuses A because of 03. |

A scratch test with two berths ran R3.
With the rule, the passenger pod 02 waits for good.
Without it, the empty pod 03 waits for good, and both passengers arrive.

## Findings

- The root is that a destination is a plan and also a veto.
  To change its weight moves an opportunity from one pod to another, with no way out for the pod that loses it.
- R1 is the life of an empty move: a surrender keeps its route, and only a released pod in service is parked again (`parkUnclaimedReleased`).
- R2 is one pod's own limits, and R3 is competition for the same berths.
- In the code at `8a0b50bb`, the veto on the pod's own berth starts a search.
  If the search finds nothing, the pod keeps its route and can still get the grant.
  The veto on an alternate removes that alternate.
  So in R3 the pod 01 keeps A, and the rule makes the bad switch.

## The soundness line

The property to ask for is that a new switch causes no stranding that the present rule avoids.
A contested switch is right only when the pod can advance, and each pod whose berth it takes keeps or gets a berth that it can reach, in the order of the lane.
It does not promise an end for three pods that need two berths that no pod leaves, a bound on waiting with new demand, or the capacity of the junctions.

## Shapes

A blind review (`gpt-6.2-astra`) proposed S7 and S8, and an adversarial review (`gpt-6.2-sol`) attacked all shapes.
Each "breaks" below comes from a trace of the code by the reviewer, and no test has run it.

| Shape | Rule | Result |
| --- | --- | --- |
| S0 | Keep the present rule. | It can also strand: A stays occupied, 01 at the branch may not take B because 02 behind it targets B, and only 02 can use C. HX11 stays failed. |
| S1 | A claimless empty move can choose again. | Breaks: the empty move takes the free berth that a rider needs. A repeated claim and surrender can churn. |
| S1 and the rule without its guards | S1, then ignore distant plans. | Breaks on R2 with a temporary owner of A. |
| S2 | The plan of a claimless empty move does not exclude a rider or a pickup. | Ends the stated R3. Breaks when the empty move heads a lane with a rider behind it. |
| S3 | Only a pod ahead on the same approach excludes. | Ends the stated R3. Breaks across two approaches. |
| S6 | Ignore a distant plan only when the own berth has an owner. | Breaks: that owner leaves after 01 takes B, and R3 follows. |
| S7 | Two pods exchange berths in one tick, with both grants or neither. | No break found. It rejects the case where the second pod is still far, which is the usual case. |
| S8 | Reserve a replacement berth before the switch. | Needs saved state, and a rule for each berth selector, clearing, and transit. The most to prove. |

The first view of the author was S1 as its own step.
The trace against S1 refutes it as a safe first step, because the present rule serves that rider.

## A test that would find these cases

Fork one state at a changed decision, run both rules on the same inputs, stop the arrivals, and compare the orders that complete.
It can find R2 and R3.
R1 needs an order that depends on the empty move, so a test of riders alone can miss it.
A timeout shows a delay only, so a stranding needs a closed wait cycle or a repeated state.

## Recommendation

| ID | Question | Options | Recommendation |
| --- | --- | --- | --- |
| BD-D1 | The uncommitted rule | A: discard it, and keep the record. B: keep it for another round. | A. Its stranding is shown by a run, and a guard for each case did not end them. |
| BD-D2 | The next step on berths | A: none now. B: a read-only probe on wharf seed 2, from the common start through the 563 s wait, that counts the decisions where S7 applies. | B, with its own grant. Zero decisions from the start rule out S7 for that wait, and berth choice then needs S8 or nothing. |
| BD-D3 | HX11 | A: it stays failed. B: study the junctions, which hold the head for about two thirds of its stopped time. | A now, and B as its own grant. |

S7 in the wait itself cannot show that an earlier exchange would not prevent it, so the probe must start at the common start.

## What this does not show

- How often R3 or any other case happens in a preset.
  The release form had none that the Progress gate counted.
- The layouts of the table above, in a run.
- A cause of the longer settle and the more delayed riders in the HX11 runs with the rule.
