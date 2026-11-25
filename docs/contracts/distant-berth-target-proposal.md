# Distant berth target proposal

Status: draft for the maintainer, 2026-11-25.
Nothing of it is in the code, and the approval of this note is necessary before a build.
It starts from BT-D8 option A, called `far` in the [study of link 100](../studies/link-100-queue.md), and it narrows that rule.
A [screen of the narrowed rule](../studies/link-100-queue.md#the-narrowed-rule) has six runs.

## The problem

Row HX11 fails on record `i1.17`, which waits 699.9 s in the queue of link 100 toward Waterloo, and the limit is 600 s.
Junctions hold the head of that queue for about two thirds of its stopped time, and a berth for most of the rest.
In 80 to 93 percent of the seconds at a berth, another berth has no owner, and the head does not take it.

`berthAvailableFor` (`internal/sim/berth_choice.go`) refuses a berth when any other pod that is not idle has it as its destination.
It does not ask where that pod is.
Its only caller is `reevaluateTerminalBerth`, which uses it for the berth that the pod has and for each alternate.

## The rule

`berthAvailableFor` ignores another pod for which all of these hold:

1. Its activity is `Traveling`.
2. It owns neither the berth nor the berth node of its destination.
3. `reservedThrough` is a block of its route, and the lane of that block is not a lane of its destination station.
4. `choosesBerth` holds for it: it has riders aboard, it is an empty recovery, or a waiting trip names it.

`far`, the measured rule, has only clauses 1 to 3.

`berthAvailableTo` runs first and does not change, so a berth that another pod owns is still refused.
The rule reads live state of the tick and has no constant, no new field, and no draw.

## What the contracts say

| Text | Place | Effect |
| --- | --- | --- |
| Reevaluation moves a pod "to another free berth of the same station", and a pod waits with "Blocked by incident" only when no reachable, compatible berth "is free". | [Suspension](incident-suspension-contract-proposal.md), section 9.3 | "Free" has no definition there. The rule makes more berths free. A clause is necessary, in the text below. |
| A blocked berth is refused, because the fault owns its resources. | Suspension, section 9.3 | Unchanged, by `berthAvailableTo`. |
| The berth order of the station choice of an emergency uses `berthAvailableTo`. | [Emergency](incident-emergency-contract-proposal.md), section 9.1 | Unchanged. |
| A bound emergency can move to "another free, reachable berth". | Emergency, section 9.2 | It uses the same reevaluation, so the new meaning of "free" applies. |
| An empty recovery runs "on its original route, with no search for a free berth", and a route that ends at a berth is "unchanged". | Suspension, S2, and [service transitions](incident-service-transitions-contract-proposal.md), section 9.5 | `reevaluateTerminalBerth` already moves such a pod, so the text and the code differ today. Clause 4 relies on the code. |
| A passenger or pickup pod can choose a free alternate berth. | [Model](../model.md), berth choice | One sentence on what "free" means. |

Proposed text for section 9.3 of the suspension contract:

> A berth is free for reevaluation when it passes the test of `berthAvailableTo`, which refuses a berth or a berth node that another owner holds, and no other pod that is not idle and that counts goes to it.
> A traveling pod does not count when it owns neither resource of its destination berth, its reservation has not reached a lane of its destination station, and it can choose its berth again.
> Reachability, compatibility, and the detour check still apply.

Proposed text for S2 and for section 9.5 of service transitions:

> Evacuation keeps the current route and destination at first.
> Endpoint reroutes and terminal reevaluation stay permitted, and they do not change the destination station.

## What it can do to the pod that it ignores

- The ignored pod has no claim, so it loses no resource, only a place that it planned.
- It can choose again at its approach, as today, when another berth is available.
  When none is, it waits, as a pod at a full station does today.
- `far` also ignores an ordinary empty move that gave up its claim (`surrenderWaitingClaims`, or a yield without a release).
  Such a pod fails `choosesBerth`, and `parkUnclaimedReleased` moves only a released pod.
  Then it can wait for its berth for good.
  The review of this note gave a cycle from the code, with no run: the empty pod waits for berth B, which the taker has; the only other berth C is the target of a pickup behind it; berth clearing refuses C because of the pickup, and the empty pod cannot choose C.
  Clause 4 keeps such a pod counted, so the rule does not make this cycle.
- Two near pods still exclude each other, so the rule gives no new right at the station itself.

## Evidence

These numbers are for `far`.
The narrowed rule ran on two presets, and all six runs passed: wharf +4.5, +4.6, and +9.5 percent completed orders, and `london-full-shared-express` -3.6, +5.2, and +14.1 percent, with a longest record of 571.5 s against the limit of 600 s.
It did not run on `london-central` and `rail-hub`.

| Preset, incidents arm, seeds 1 to 3, 6 h | Late records, control and `far` | Completed orders | Mean wait |
| --- | --- | --- | --- |
| Wharf | 1, 0 | +9.6, +2.8, +6.5% | -10.8, -18.5, -9.1% |
| `london-full-shared-express` | 0, 0 | +4.5, +4.8, +14.1% | -4.7, -43.5, -25.3% |
| `london-central` | 0, 0 | 0.0, +2.1, -0.1% | +0.1, -1.9, -0.2% |
| `rail-hub` | 0, 0 | +0.5, +5.6, -8.4% | 0.0, -4.1, +5.4% |

- One `far` run fails: seed 1 of `london-full-shared-express` misses the Progress gate after the clear, and its control passes.
  The trace shows an empty recovery that drives for longer than the gate, and not a block.
  The pod still had 6,625 m left in the last reading, so the trace does not show its arrival.
  The [gate proposal](recovery-drive-gate-proposal.md) treats it.
- In the same run the fault wait rises by 50.2 percent and the delayed riders by 42.6 percent, with no known cause.
- Fault wait and delayed riders go both ways on each preset.
- In an earlier pilot on `LF` with the batch workload, the same rule completed fewer orders: 350 against 368, 397 against 431, and 350 against 361.
- The runs do not show that the rule ends `i1.17`, because they differ from the control from minute 3.
- Junctions are the larger limit, and the rule does not change them.
- No run used the release form, the `emergencies` arm, or a reference queue.

## Options

| Option | Rule | For | Against |
| --- | --- | --- | --- |
| A | The narrowed rule, clauses 1 to 4, for every preset. | No constant. One function. No pod that cannot choose again loses its place. | Seed 1 of `london-full-shared-express` got worse. It can change trajectories and goldens where the exclusion applies. |
| A0 | `far`, clauses 1 to 3. | Measured. | The cycle above, unless a rule for displaced empty moves comes first. `rail-hub` seed 3 and `LF` got worse. |
| B | A, only when the pod that chooses is an Express pod or goes to a station of an Express service. | It can limit the change to the presets that gained. | Not measured. A special case by class, and the queue of link 100 also has Compact pods. |
| C | A, but a pod within a time of its station counts. | It protects a pod that is about to arrive. | A constant with no measurement. |
| D | No change. | No cost. | It leaves HX11 unresolved by this proposal. |

## What a build must check

- Unit tests of each clause, with a mutation that removes it: the activity, each of the two resources, the lane test, the bounds of `reservedThrough`, and `choosesBerth`.
- The own-berth use of `berthAvailableFor`: a pod keeps its berth when only distant pods go to it.
- Full-`Step` tests: an ignored passenger pod, pickup, empty recovery, and bound emergency, each after its berth is taken, with progress of both pods.
- A full-`Step` test of the cycle above: the empty move with no claim keeps its berth, and a mutation that removes clause 4 makes it fail.
- The cycle of the berth claim deadlock does not come back.
- A save and restore while the rule changes a choice.
  The frontier survives through `ReservedAhead`.
  A test must show the same eligibility and choice after an exact whole-route restore, and other tests cover a trimmed route and the accepted exceptions of EC-D10 to EC-D12.
- `TestPresetBehaviorParity`, the pinned digests, and the latency gates, with a cause for each golden that moves.
- The release form of HX11 on both seeds, and the `emergencies` arm.
- The soaks of the other presets with their reference runs, so that the queue check runs.
- `golangci-lint` and `go fix -diff` on the changed packages, and the model text.

## Decisions for the maintainer

The maintainer chose BT-D13 A and BT-D14 A on 2026-11-25.
BT-D12 is open.

| ID | Question | Options | Recommendation |
| --- | --- | --- | --- |
| BT-D12 | Which rule | A, A0, B, C, or D above | A |
| BT-D13 | Order of work | A: a scratch screen of option A first (wharf and `london-full-shared-express`, incidents, seeds 1 to 3, 6 h, 6 runs against the recorded controls), then ask again. B: build in the repository, unpushed, and run the release form of HX11 first. | A, because the narrowed rule had no run |
| BT-D14 | Stop rule of a build | A: each gate that passes now and fails with the rule prevents the landing. After a gate change of PG-D1 is approved and checked, an exception needs the observed arrival of the pod inside its frozen deadline, and no other miss. B: judge the results after the runs. | A |
