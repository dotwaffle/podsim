# Station approach buffer proposal

Status: original design, superseded by the approved [version 3 contract](station-buffer-state-proposal.md).
The experimental controller keeps new admissions off by default.
The [station-entry platoon implementation](station-entry-platoons.md) adds fixed local entry links.
The discussion below records the original design and does not establish a service benefit.

## Problem and first case

At Acton Town, queues approaching from Turnham Green can occupy the mainline while the station approach has unused space.
A captured run had 27 pods on the final mainline lane, including 16 stationary pods.
The first pod waited for an admission reservation held by a pod arriving from another direction.
The entire 96.76-meter station feeder belongs to the station merge conflict zone.
This can prevent a pod from leaving the mainline before the merge becomes available.

The next lane, from the station merge to the entry branch, is 138.92 meters long.
Its geometric conflict regions cover the first 13 meters and roughly the last 15 meters.
The middle is potential holding space.
These measurements do not establish a safe pod capacity.
Track-cell boundaries, pod tails, stopping distance, and platoon clearance also matter.

Passenger routes approach a station without a berth assignment.
Ordinary pickup dispatch assigns a berth before departure.
With experimental buffers enabled, new pickups use an eligible berthless station approach.
Without buffers, the controller chooses a berth before reserving the final approach lane and can change an uncommitted berth branch.
That choice can name a busy berth.
The observed blockage therefore does not prove that berth selection alone causes the queue.

## Proposed first implementation

Use an off-mainline lane as a bounded physical queue, with berth selection at its head.
Start with generated London station entry lanes that have a measurable holding region.
Do not treat an arbitrary short lane as a buffer.
Stations without a safe holding region keep the existing admission behavior.

Derive holding positions from the same geometry, conflict regions, and clearance rules used by traffic admission.
Admit a pod only when it can stop inside the buffer, with its tail clear of the upstream conflict region.
Do not admit a pod whose only stopping position blocks the entry branch or a departure path.
Keep the downstream conflict region clear until a berth route can reserve its required resources.
Do not shorten or ignore an existing conflict region to create space.

A queued pod remains assigned to its passenger journey or pickup order.
Reaching its holding position must not count as arrival, unloading, boarding, or journey completion.
Only the queue head chooses and commits to a berth branch.
A free berth alone is insufficient: the controller must also acquire the safe path to it.
Do not reorder pods within a single lane.
Competing approaches retain passenger, pickup, and empty priority, with the existing 10-second aging override.
Existing reservations remain protected.

The first implementation absorbs finite bursts.
When the buffer fills, upstream pods wait under the existing traffic rules.
It does not promise that sustained excess demand cannot reach the mainline.
A later station-wide admission system could reserve queue capacity before departure, but must account for pods already underway and avoid cyclic waits between full stations.
Do not add that system to the first implementation.

## Platoons

Allow virtual platoons on the shared buffer approach only when their certified run and stopping positions fit.
Keep the current minimum separation, turn certificate, reaction allowance, and configured platoon limit.
Do not count platooning as a reduction in the required stopped-pod footprint.
Its expected benefit is shared-reservation admission and discharge.

The queue head must separate safely before it commits to a berth route that the followers do not share.
Followers must release or inherit shared resources under the current ownership rules before choosing their own berth routes.
Do not relax the rule against changing an active shared route.
Departure movements must remain able to clear occupied berths while arrivals wait.

## State and compatibility

A pod waiting in the buffer needs an explicit distinction from a pod that reached its final berth.
Specify that state and its restore invariants before implementation.
A save must retain the target station, queued journey or pickup assignment, physical route position, and any platoon certificate.
Restoration must rebuild valid reservations without converting a buffered pod into an idle or arrived pod.

Do not change the saved-state format or add project controls implicitly.
The implementation review must settle whether existing route and phase fields express this state safely or require a versioned extension.
Existing projects and unbuffered stations must retain a defined fallback.
Generated geometry changes do not rewrite a user's saved network.

## Validation and adoption

Build a repeatable Acton arrival burst with competing approaches and departures.
Include full berths, a full buffer, a blocked exit, mixed priorities, an aged empty pod, and platoons that split toward different berths.
Check per-tick separation, ownership, passenger accounting, and eventual departure progress.
Test graceful save and restore with both a stopped queue and a draining platoon.

Compare the existing controller and candidate with the same project, orders, seed, and junction-priority policy.
Report mainline blocked time, buffer occupancy, station throughput, and passenger wait and journey times.
Check a sustained-overload case to show where spillback starts rather than claiming that it disappears.
Keep the feature unadopted until these checks pass and its compatibility contract is approved.

## Decisions for review

1. Limit the first implementation to local buffering, with ordinary upstream waiting when full.
2. Derive capacity from safe geometry rather than adding a user-selected pod count initially.
3. Require a separate state/restore contract review before implementing buffered arrivals and station-entry platoons.
