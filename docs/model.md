# Simulation model

This document explains how pods, orders, stations, and traffic control behave, so that you can read a run or a report and know why it came out that way.
The simulation is deterministic: the same project, seed, and commands give the same run.
The [user guide](user-guide.md) covers the controls, and the [reference](reference.md) covers the project format and the tools.

Podsim is a hobby simulator, so the model does not certify a real vehicle or network.
The tests establish progress for feasible supplied scenarios, not for every saturated network.

## Time, geometry, and routing

The Go core advances in fixed 60 Hz steps and uses world coordinates in meters.
A lane is straight or a quadratic curve, and the simulator and the browser measure each curve along the same sampled path.
Each lane has an explicit speed limit, and bends add no other speed limits.

A pod takes the route with the shortest free-flow travel time on the directed lanes.
Equal-cost routes use scenario order, so the result is deterministic.
New routes avoid the berths of other stations when a compatible path exists (see [berth route preference](studies/berth-route-preference.md)).

The code once had three other routing policies: `congestion`, `queue`, and `predictive`.
They were removed on November 5, 2026, because none had a service gain on record.
The [selected service screen](design/experimental-adoption.md#rejected-candidates) returned no alternative routes across six matched pairs, and the [route-selection diagnosis](design/experimental-adoption.md#rejected-candidates) explained the guard outcomes and recorded a congested trial with worse service.

## Vehicle classes

Each pod has a class, which sets how many passengers it carries and how much room it needs.
A project that names no class gets Legacy pods, and the London presets use Compact pods.

| Class | Seats | Largest new party | Body length | Large class |
| --- | ---: | ---: | ---: | --- |
| `legacy` | 8 | 1 | 4 m | No |
| `compact` | 4 | 4 | 4 m | No |
| [`group`](design/group-physics-design.md) | 8 | 8 | 6 m | Yes |
| `express` | 20 | 8, or 20 with `express-v1` | None, or 10 m with `express-v1` | Yes |

A large class needs more room, and the rest of this document refers to it by that name.
A pair of pods with a large class keeps 20 meters of clearance, where other pairs keep 12 meters, and a lane that admits a large class must be at least 40 meters long.
The route position of a large class is the center of its body envelope, which has a 6-meter radius.

A lane, a station, or a berth can list the classes that can use it.
A lane with no list allows Legacy and Compact pods, so a Group or Express pod needs lanes that list its class.
Each lane that touches a node of a lane with a large class, or of a berth where the berth and its station both allow one, gets a large-class tail, and a curve makes this tail longer.
When the tail exceeds the length of the lane, a pod can hold a station exit past the next station, and this can stop the network.
`projectcheck` and the editor checks give a warning for such a lane, and they accept the project.

The Express class has no physical operation unless the project sets the `orderContract` marker `express-v1`, which is opt-in.
That marker sets the limits of its own format: 600 pods and 17,000 orders (see the [Express qualification](studies/express-qualification.md) and the [approved contract](contracts/express-physical-encoding-contract-proposal.md)).
Express has no byte proof.

## Passenger service and dispatch

An order goes through dispatch, which picks a pod, and then the pod collects the party and carries it.
Passengers order travel between stations whatever pod a browser has selected.

### Parties

A party is the group of passengers of one order, and it never splits.
A party has 1 to 8 passengers, or 1 to 20 with the Express contract.
Generated and demo orders have one passenger.
A pod takes a party only when the party fits the [class](#vehicle-classes) of the pod and its free seats.

A party rides alone unless it consents to share, and the consent comes from different places:

| Source of the order | Consent |
| --- | --- |
| Manual order | **Private party** or **Share allowed** in the order controls, or `sharingConsent` in the `trip` command. |
| Generated order | Private, unless the project sets `generatedOrderConsent` to `shared`. This covers each demand pattern, including `rail-arrivals` and `rail-services`. |
| Traffic demo | Always private. The demo is a fixed script. |
| Compare command | The `-sharing-consent` flag, which ignores `generatedOrderConsent`. |

Consent alone does not pool anyone, because the project must also allow it.
By default, each party uses one pod.
The project setting `sharedRidePartyLimit`, from 2 to 8, lets waiting parties join a pod that is still boarding at their origin, when each party in the pod has shared consent.
A setting of `shared` with a party limit of 1 is valid and pools nothing.
Sharing does not wait for more parties.

The project setting `sharedRideMode` selects the parties that can join:

| Mode | Parties that can join |
| --- | --- |
| `drop-offs` | The default. Parties for a stop of the pod, or for a station that the pod can add as a stop. |
| `destination` | Parties for the destination of the pod. |

In `drop-offs` mode, a boarding pod adds a stop for a new party in one of two cases.
In the first, the free-flow route from the boarding berth to the last stop passes the start of an approach lane of the station, and the new stop goes between the other stops in route order.
In the second, the free-flow route from the boarding berth to the station passes each stop in the same order, and the station becomes the last stop.

A detour cap protects the parties aboard, because a new stop can take them out of their way.
The pod adds a stop only when the planned detour ratio of each party, new or aboard, is at most 1.5.
The ratio of a party is the planned distance from the boarding berth to the berth where the party alights, over the free-flow distance to that berth, and the compare column `detour_ratio_max` measures the same ratio.
The pod does not know its berth at each stop before it arrives, so the plan uses the berth that gives the largest ratio.
The conditions and the plan use the free-flow routes.

The pod keeps to this cap while it travels.
Each leg takes the free-flow route that the plan used.
The pod changes to another berth at its next stop only when the new route keeps each party aboard within 1.5.
Thus the measured ratio of each party that boards with a limit of two or more in the `drop-offs` mode is at most 1.5, while these settings do not change.

The project setting `sharedRideMaxStops` sets the maximum number of stops before the last stop, from 1 to 7, with a default of 3.
A new stop before the first stop changes the route of the pod, so the pod adds a stop only before it gets track.
The stops do not change after the pod departs.
At each stop, the parties for that stop alight and the pod continues with the others, and while it waits for track at such a stop, its activity is **Continuing**.

A pod with passengers picks up parties on its way only with the project setting `onboardPickups`, which needs a limit above one and the `drop-offs` mode.
The pod then takes a party at a passenger berth while it stands there (see [onboard pickup design](design/onboard-pickup-design.md)).

Only a waiting party with no pod can join a boarding pod.
A party whose pod is idle at the origin boards that pod.

### Dispatch order

Dispatch considers orders in submission order.
An idle pod at the origin serves the oldest waiting party.
Otherwise, dispatch compares idle pods and empty pods that can divert, which are pods going to parking, pods moving for redistribution, and pods that dispatch released from a pickup.
It chooses by the estimated pickup time, then by pod ID.
An idle pod at the pickup station has an estimate of zero, because it boards at its own berth.

### Wait rules

Dispatch can wait for a busy pod if that pod should reach the pickup more than two seconds earlier.
The estimate includes travel, acceleration, braking, boarding, and unloading, and it does not predict traffic delays.
Dispatch waits for at most 30 simulated seconds before it uses an available pod.

| Rule | Behavior |
| --- | --- |
| `current` | Waits as described above. The server always uses this rule. |
| `strict` | Waits only when the busy pod can finish and reach the pickup before the 30 seconds end. Compare command only. |
| `none` | Never waits for a busy pod. Compare command only. |

### Pickup

An assigned pickup pod can depart while its berth is occupied, and it queues on the approach.
It claims the berth through local admission and not through a remote reservation.
Passengers board only at a berth.

If pickup pods arrive out of order, the first available pod takes the oldest passenger at that station, and the other pod keeps a pickup at the same station for the later order.

### Released pods

A pod that becomes idle at the pickup station can take a trip from a pod that is still on its way there.
Dispatch then releases the other pod, which can divert for a pickup at once.
A later trip in the same dispatch pass or in a later pass can take it, and if no trip takes it in the same pass, it goes to the nearest free berth.

A free berth has no pod, no claim, and no other pod or waiting trip going to it.
The pod reserves the chosen berth, as a pod on its way to parking does.
When two berths have the same route cost, the order of preference is the current destination berth, the other berths of its station, and then the berths of the other stations in network order.
A pod that has not left its berth can choose that berth and stays there.
A moving pod cannot choose its origin berth until it is at clearance distance from it.

Two other events release a pod.
A restore that removes the pod binding of a waiting trip releases the pod on its way to that pickup, and the next dispatch pass sends that pod to the nearest free berth if no trip takes it.
A released pod that gives its berth claim to a passenger pod goes to the nearest free berth at once.

### Empty travel

An empty pod on its way to parking can divert for a pickup.
It keeps all committed track and releases its unused parking claim.
A pod already committed to the parking inlet finishes that maneuver before it returns to service, and a released pod does the same.
Empty pickup travel keeps the original order ID and does not count as a passenger journey.
The fixed demo can still give journeys directly to specific pods.

## Track admission and junctions

Pods reserve track ahead of them, and a pod moves only within what it has reserved.
A pod accelerates and brakes within its admitted distance, and it extends its reservation to cover the stopping distance at the lane speed plus two simulation ticks of travel.
On clear track, this allows steady cruising at 14 m/s across cell boundaries.

The local controller divides lanes into exclusive track cells of up to 30 meters.
It keeps the cells behind a pod until a four-meter pod and an eight-meter gap have cleared.
The track cell model is conservative, and it does not model continuous car-following or optimized junction capacity.
The traffic model needs lanes at least 24 meters long, or 2R meters long for a qualifying slow lane, as below.

### Slow lanes

A slow queue can pack closer than the usual 12 meters, because a slow pod stops in a short distance.
A lane qualifies when it is straight and its speed limit is less than the square root of 14, about 3.74 m/s.
The track near the lane must also be one chain of lanes without large classes, with a total turn of at most 90 degrees.
This track starts where a pod that can stop on the lane can be, and it ends 24 meters after the lane.

| Value | Rule |
| --- | --- |
| Retention distance R | (5 + 2u) / cos(T/2) - u meters, less than 12 meters. u is the largest stopping distance on that track, at most 3.5 meters, and T is its total turn. |
| Gap between two small pods on the track | 5 + v²/2 meters, at most 12, where v is the speed of the rear pod. |

The station bank check uses the same rule for the parts of two paths that are on this track.

### Junctions

Each junction has one conflict resource for nearby sections of its incident lanes.
The controller derives these sections from the same geometry that it uses for movement, including the 12-meter clearance.
It acquires each continuous conflict section together, including the downstream cells needed to cross lane endpoints, and it releases the resource after the pod reaches the end of that section.

Admission follows these rules:

- Requests that waited at least 10 simulated seconds take priority, oldest first.
- Younger requests prefer pods with passengers, then assigned pickups, then other empty movements.
- Within each class, the oldest local admission request wins, and pod ID breaks a tie.
- Existing reservations stay protected.
- Admission uses the state before movement.
- Released resources become available on the next tick.

### Coasting at a station diverge

With platoons on, a pod that waits for a contested station diverge can slow down early, so that it rolls into the section when the section frees and does not stop.
The station diverge is the node where the entry lane of a station starts.
A pod coasts only when a pod outside its platoon waits for the same section at the same tick, and the pods that hold the section move.

Its hold point is before the end of its reservation by the square of the lane speed limit over four times the acceleration, which is 24.5 m at 14 m/s, but not more than the gap to that end.
Coast does not brake a pod harder than its usual stop, so a pod that arrives at the speed limit still stops at the end of its reservation.
A pod that moves up slowly, such as the first pod of a queue, stops at the hold point.
A platoon member that must first own the cells that it shares also waits as a head, and it can coast.
The pod that admission will take first starts early enough to reach the section at speed.

Coast lowers only the commanded speed, so it changes no admission order, and the stop point of the pod does not move back.
A pod does not coast while it holds a junction or a node that it releases before the end of its reservation.
Pods of a large class and pods with an emergency do not coast.

### Virtual platoons

A virtual platoon lets a slow pod in a queue follow the pod ahead at a short gap and share its track cells.
The project setting `platoonLimit` turns platoons on, with a value from 2 to 4 for the largest number of pods in one platoon.
A value of 0, or no value, turns platoons off, and the **Platoons** field of the editor sets it.
The compare command does not read this setting, and it uses `-platoon-policies`, where the `virtual` policy has a limit of 4 pods.

A link needs a run of lanes that both routes share, starting at the lane of the follower.
A mainline link needs shared succeeding lanes, and each route must have one speed limit.
A run can hold the entry lane of a station when both pods go to a berth of that station.
No run of such a pair continues past the entry node of that station, and no run starts on an entry lane.
A lane after the entry node of the destination station of either pod, and the entry lane of a station that is not the destination of both pods, never join a run.

The turn of the run sets the clearance of the link.
The total turn along the run must be at most 120 degrees, counting each turn in a curved lane and at each lane join one time, by size and not by sign.
The link clearance is 12 m divided by the cosine of half of the turn, plus 0.01 m, so two pods at least that far apart along the path are at least 12 m apart.
All links of one platoon have the same turn and clearance, and a link keeps them until it ends.
A new platoon takes at least the turn from the lane of the follower to the end of the entry lane of its destination station, when that turn is at most 120 degrees, at any distance from that lane.
Then its run can grow onto the entry lane, and a queue longer than 300 m crosses the diverge as a group.
A link forms only when the two pods and their stop points are at least the clearance apart.

A linked pod can reserve the track cells and junction sections that the pods ahead of it in its platoon hold.
It shares only the cells whose resources each pod releases at least 1 m before the end of the run, so each pod that holds a shared resource is on the run.
It stops at least the clearance behind the stop point of the pod ahead, less the distance of 0.5 s of travel at its speed.
When a pod ahead releases a shared resource, the next pod in the platoon owns it.
While a linked pod holds a cell of a pod ahead, it reserves only the cells that its predecessor reserved, and a pod ahead does not reserve a resource again while a pod behind it holds that resource.
Berths are not shared, and a linked empty pod cannot divert.
The 12 m separation check does not change.

A link can grow, and it ends by draining.
A mainline link can add the next shared lane while its total turn stays within the turn of the platoon.
After the last cell that it can share, the follower waits until the pods ahead pass the shared resources and hand them to it, and then the link ends.
A link also drains when platooning stops, when one of its pods stops traveling, or when a different pod comes between the two pods.

A saved state keeps each link in the `platoon` field of the follower, with the predecessor, the run as indexes into the two saved routes, the turn, and whether the link drains.
The restore checks the run, the turn, the speed limits, and the clearance against the network and the pods, and it does not plan the link again.
A link that drains before the save also drains after the restore.
See [session state](operations.md#session-state) for the restore tiers.

## Stations and parking

A station has berths outside its through lane, so that a stopping pod does not block the pods that pass.

### Station layout

Station entry, berth, and exit connections have stable identities.
Passenger and parking stations can have multiple berths, and berths can connect through intermediate arrival and departure lanes, separate from through traffic.
Station-local paths cannot cross another berth or station.
Each station keeps a direct entry-to-exit through lane.
A station can also have [independent banks](station-banks.md), which are groups of berths with their own gates.

Station lanes can name their role: approach, entry, berth access, through, departure, or exit.
The pod inspector shows the current maneuver in **Station phase**, with the station name on the line below it.
Projects without this optional data still load, and the simulator infers the through, berth access, and departure roles from the station paths.
These roles describe the movement and reservations and add no second station controller and no change in admission priority.
The controller makes local reservations and no whole-journey timetable.

### Berth choice

A passenger pod routes to the station entry with no berth, and the controller chooses the berth late, when it knows more.
It chooses the least-assigned reachable berth when the next reservation of the pod starts on the final lane of the road route, or when that reservation reaches the last cell of the road route, as it can on a short final lane.
With platoons on, the controller also tries for a pod that is not of a large class when the entry lane starts within 300 m, or when the next reservation reaches that lane, so that the run of a platoon can grow onto the entry lane.
If that early try finds no berth, the pod continues and tries again at the usual point.

The controller can change this choice before it reserves a berth branch.
A passenger or pickup pod can choose a free alternate berth before it reserves the next station branch.
A route change keeps all admitted track.

A pod reserves its berth before it enters the final inlet cell, and it keeps the berth through unloading and idle time, until its departure clears the resource.
Other pods can queue on the inlet while through traffic uses the separate through lane.

### Compact bays

Compact bays are angled parking places for Compact pods, which fit more berths into a short station.
Only one pod at a time moves on the lanes of a group, so the bays hold pods in little space but serve them slowly.
One visit holds the aisle for about 30 to 43 seconds.
Use a Compact station only where its demand share times the order rate is below about 0.7 trip ends per minute (see [LondonFull Compact stations](london-full.md#compact-stations)).
The London presets put these bays at 7 or 8 stations by default, each of their pods is Compact, and the `-compact-stations` flag of the scenario command sets the stations.

The optional network member `maneuverGroups` defines the bays.
Each group has an `id`, a `stationID`, and the `laneIDs`, `berthIDs`, and `reverseLaneIDs` of the group.
A pod on a reverse lane faces against its direction of travel.
Each lane and berth of a group admits only Compact pods, and a lane of a group has no minimum length, but its length must be more than 0.
A pod on a lane of a group takes its station phase from its route: `Departing berth` when each earlier lane of the route is a lane of a group, and `Accessing berth` when each later lane is.

Each berth of a group needs a closed bay chain, which is a chain of group lanes from a branch node to the berth and a chain of reverse lanes back over the same nodes.
A node inside a chain has only the four lanes of its chain, and the berth node has only its two lanes.
The reverse lanes of the group are the lanes of these chains back to the branch nodes.

All cells of the group lanes share one junction resource.
A pod in a virtual platoon cannot share this resource, or a lane of the group, with the pod ahead.
Each cell of a bay chain also holds the berth of the chain, so a pod in a bay chain always owns that berth.
When the reverse flag changes between two lanes of a route, the pod stops for 1 second at the start of the second lane, and pickup and emergency estimates do not include this stop.

The server certifies the geometry when it prepares the network.
It sweeps the body of a Compact pod along each lane of a group, including the turns in place at the nodes and at the berths.
Each lane must be clear of the parked envelope of each berth of the group, except the berth of its own chain, and each two envelopes of a group must be clear of each other.
The envelope is 5 m long and 2.1 m wide, with its center 1.2 m ahead of the berth node.
Otherwise the network is not valid, and the error names the lane and the berth, for example `maneuver group "compact-bays" lane "bay-2-in-3" is not clear of berth "bay-3"`.
The lane gap audit and the separation check skip each pair that the sweep clears.

A pod that waits for a full bay can wait for a pod that waits for the exit.
As at an ordinary station, a custom network with more pods than its bays and roads can hold can stall, and the `compact-station` preset has enough room for its pods.
The map shows a pod in a bay as a round mark, with no heading.

### Parking depots

A depot is a parking station made of queue lanes, so it stores many pods in a small space and releases them in order.
The optional network member `depots` makes a parking station a depot, and each depot has a `station` and its `lanes`.
Each lane lists the `berthIDs` of its slots from the head to the tail.
A depot has 1 to 8 lanes, and it lists each berth of its station one time.
The station must be a parking station with no banks, and its slots need one class set with no large class.
A London depot is at most 200 meters long (see [LondonFull depots](london-full.md#depots)).

Each lane is a straight line of nodes, and the lanes of a depot are parallel.
A lead-in goes to the tail slot, a chain lane goes from each slot to the slot ahead, and a lead-out leaves the head slot.
A fan path goes from the station entry to each lead-in, a throat path goes from each lead-out to the station exit, and the entry has exactly one incoming lane.
A depot lane cannot have a separation group, and a virtual platoon run cannot hold a depot lane.

Slots can be close together because the pods move slowly.
A chain lane needs only its reduced retention, because each of its cells holds the slot ahead and both of its nodes, and at about 1.41 m/s, slots can be 6 meters apart.
The lane gap audit checks each pair of paths that includes a depot lane or a slot.
When a swept-body check clears them, two slow paths of different lanes of one depot need only 3.1 + v²/2 meters, where v is the higher speed limit of the two, which is 4.09 meters at the depot speed.
Other pairs keep their usual clearance.
The rule assumes that the guideway keeps a pod on its lane and at the heading of its lane (see [station banks](station-banks.md#depot-lanes)).

A pod going to a depot gets the entry as its destination, and it can divert until it binds a slot.
It binds when it owns the last cell of the incoming lane, and it takes the slot behind the rearmost taken slot of the lane with the fewest pods, with the lowest lane index on a tie.
A slot is taken when a pod is idle at it or goes to it.
A lane with a faulted or withdrawn pod, a full lane, and a lane that is closed or does not admit the class of the pod take no pod.
When no lane takes the pod, it looks for an open berth at another station, and it waits at the entry when it finds none, showing **No parking available**.

A pod in service starts to move up when the slot ahead of it is free.
A move-up keeps the parked tick of the pod, and a new arrival sets it.
Only the head of a lane, at its first slot, can leave the depot.
Dispatch, pickup transfer, and guarded positioning take the head that parked first, and then the lowest lane index.
They skip a head that is faulted, withdrawn, or not eligible for the order, and a head whose lead-out or throat path is blocked.
A faulted or withdrawn pod keeps its slot, and the pods behind it wait.
An operational target cannot be a depot station.

The browser editor keeps the depots of a project, but it cannot make one, and it refuses each station action that changes the geometry or the berths of a depot station.
A project with a depot gets the full network validation of the server in the editor, and the depot validation runs first, so an invalid depot draft gets the server depot error and not a lane gap or bank error.
The London presets have depots by default, and the `-parking-lanes` flag of the scenario command sets the depot lanes of a ring or a London preset (see [generated scenarios](reference.md#generated-scenarios)).
In a LondonFull run at 60 orders per minute, the depots served within four orders of ordinary parking stations, and empty pods stopped inside the depots for 1,225 more pod-seconds while other pods of the same depot moved.

### Idle pods

An idle empty pod does not block other traffic, so it clears its berth when a passenger arrival, an assigned pickup pod, or an empty relocation needs it, and for a pod that must pass through that berth to leave the station.
It first reserves a free reachable parking berth and keeps its origin until physical clearance.
If parking is full or unreachable, it reserves reachable passenger space, and it prefers local space that no order targets.
The pod shows **No parking available** only when no reachable physical space exists.

### Empty moves and parking

An empty move gives way to passengers where it safely can.
Empty relocations yield unadmitted destination claims when a local passenger or pickup needs the same berth, and physical ownership and admitted destination resources stay protected.
Empty moves have no boarding or unloading delay and do not count as passenger journeys.

Parking serves no passengers, and a parked pod returns to service by itself when dispatch assigns it to a pickup.

## Redistribution

Redistribution moves idle empty pods toward busy stations before anyone orders them, and it is off by default.
When it is on, the simulation runs guarded positioning, which moves an idle empty pod to a busy station that has no pod.
The demand weights of the stations select the busy stations.

The guards limit it to times when pods are spare, because a positioning move can otherwise take a pod from a passenger.
The policy moves pods only while the demand rate is below one order per minute for each 20 pods of the fleet.
While generated demand runs, the server uses the configured rate, and otherwise it uses the mean rate of the orders since the last reset, so a fleet of 20 pods or fewer makes no move while generated demand runs.
The policy stops moves to busy stations in any of these cases:

- The newest order is more than 180 seconds old.
- A waiting order has no pod.
- More than two fifths of the fleet works.
- The positioning moves reach the boardings.

While the demand rate is low, the policy also moves an idle pod that blocks a berth.
That pod goes to a free berth of a busy station within 180 seconds of travel, or else to the nearest free parking berth.

Passenger assignments take priority over empty positioning.
Before a pod moves, the policy reserves a free destination berth, and it leaves another free berth at the station.
A cooldown limits repeated moves.
A remote reservation yields to passenger traffic until the empty pod enters the final admitted cell, and admitted track and physical berth ownership stay protected.

## Faults

A fault stops a pod or blocks a lane, and the rest of the network must work around it.
A session can stop a pod with a pod fault, or block a lane segment with debris.
Faults are on in each session with no project switch, except that they are off during the traffic demo, and after the demo until a reset or a project apply.

A pod fault brakes the pod to rest, and its pending pickups go to other pods.
After the evacuation delay, which is 300 simulated seconds by default, the riders leave the pod at rest, and their orders end interrupted.
Other pods route around a fault, or wait, and they do not reverse.

The **Fault** button of the [pod inspector](user-guide.md#inspect-a-pod) and the `fault` command start a pod fault.
**Place debris** in the [**Incidents** panel](user-guide.md#incidents-panel) and the `fault` command start debris, and the map draws debris as a red line and a red ring.
A project can also set a [fault rate](operations.md#faults), and then a sampler starts faults with a limited duration on random pods and lanes.
The sampler has its own random stream, from the demand seed, so a run stays deterministic through a save, a restore, and a rewind on one CPU type.
An event that cannot start is skipped, and the skip uses the same draws, so later events do not depend on it.
See [faults](operations.md#faults) for the settings and the [fault commands](protocol.md#fault-commands) for the protocol.

## Emergencies

An emergency gets a party out of a pod as soon as possible.
It starts for one party aboard a pod, from the **Emergency** button of the [pod inspector](user-guide.md#inspect-a-pod) or the `emergency` command, and no command cancels it.
Emergencies are on in each session with no project switch, with the same exception for the traffic demo as faults.
A project can also set an [emergency rate](operations.md#emergencies), and then a sampler with its own random stream picks a random pod and party for each event.
An event on a pod with no active rider is skipped.

The pod leaves service, and its pending pickups go to other pods.
A pod at a berth unloads there.
A traveling pod goes to the passenger station with the soonest estimated arrival, which is not always the nearest, because the estimate counts queues as well as distance.
While the emergency is active, the pod gets a free junction, track cell, or berth before other pods that request it in the same tick, but it never takes an owned one.

Every party leaves the pod at the unload.
The order of the party that started the emergency ends interrupted.
Each other party completes when the station is its destination, and otherwise it waits at the station for its next leg.
The pod returns to service when no party is aboard, unless it has a fault.
See [emergencies](operations.md#emergencies) for the settings and the [emergency commands](protocol.md#emergency-commands) for the protocol.

## Server and browser

One server owns the simulation clock, the commands, and the demand settings, and each browser shows a copy of its state.
The [client protocol](protocol.md) gives the details.

### Shared state

Browsers receive shared gzip state deltas over WebSocket.
The simulation view needs WebSocket and native gzip decompression, and it has no HTTP polling fallback.
They fetch the network topology on connection, and after the session epoch, the project revision, or the server start ID changes.

Pod selection, origin, destination, and the open panel stay local to each browser.
Imported background images stay in the editor, in browser IndexedDB storage, and in the exported project file.
Live map settings travel with the project and topology, and each viewer fetches its visible tiles directly.
The shared simulation receives the network geometry and settings.

### Motion

The map buffers 250 ms of snapshots and interpolates movement along lanes between updates.
Controls and order status use the latest server state.
Pauses, speed changes, resets, rewinds, the start and end of the traffic demo, and long connection gaps clear buffered motion.
Rendering never predicts movement beyond the latest received position.
