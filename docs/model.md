# Simulation model

This document tells how the simulation behaves.

## Time, geometry, and routing

The Go core uses fixed 60 Hz steps and world coordinates in meters.
Routing chooses the shortest free-flow travel time on directed lanes.
Equal-cost routes use scenario order for deterministic results.

The compare command has three experimental routing policies.
They change only the route that a pod gets when it starts a journey, a pickup, or an empty move.
Estimates and the choices of dispatch, positioning, and parking use free-flow times.
A route from these policies does not go through the berths of a third station.
When each other route goes through such berths, the pod gets the free-flow route.
The `congestion` policy adds 6 s for each owned track cell of a lane and 20 s for each waiting pod on a lane, and it keeps these costs for 5 s.
The `queue` policy gives each stopped pod on a lane 3 s of queue time.
It adds only the queue time that remains when the pod gets to the lane.
It changes the free-flow route only when the saving is at least 15 s and at least 5%.
The new route must also take at most 1.2 times the free-flow time of the free-flow route.
The [predictive policy](studies/predictive-routing.md) adds smoothed observed queues and planned lane arrivals over a 90-second horizon.
It retains the same saving and detour guards, and remains experimental.
The [selected service screen](design/experimental-adoption.md#rejected-candidates) returns no alternative routes across six matched pairs and establishes no service gain.
The [route-selection diagnosis](design/experimental-adoption.md#rejected-candidates) explains the guard outcomes and records a congested trial with worse service.

Lanes can be straight or quadratic curves.
The simulator and browser measure each curve along the same sampled path.
Each lane has an explicit speed limit.
Bends do not impose extra speed limits.

Automatic demand is optional.
It starts disabled unless the loaded project enables it.
With `-project`, **Start demand** saves this setting in the file, so demand starts again after a server restart.

## Passenger service and dispatch

### Parties

A party is the group of passengers of one order, from 1 to 8 passengers, or from 1 to 20 with the Express contract.
Generated and demo orders have one passenger and are private.
A generated order is shared only when the project sets `generatedOrderConsent` to `shared`, and a manual order is shared when the party allows it.
By default, each party uses one pod.
An optional limit of two to eight lets waiting parties join a pod that is still boarding at their origin, when each party in the pod has shared consent.
The project setting `sharedRideMode` selects the parties that can join:

| Mode | Parties that can join |
| --- | --- |
| `drop-offs` | The default. Parties for a stop of the pod, or for a station that the pod can add as a stop. |
| `destination` | Parties for the destination of the pod. |

In `drop-offs` mode, a boarding pod adds a stop for a new party in one of these conditions:

- The free-flow route from the boarding berth to the last stop passes the start of an approach lane of the station.
  The new stop goes between the other stops in route order.
- The free-flow route from the boarding berth to the station passes each stop in the same order.
  The station becomes the last stop.

The pod adds the stop only when the planned detour ratio of each party, new or aboard, is at most 1.5.
The ratio of a party is the planned distance from the boarding berth to the berth where the party alights, over the free-flow distance to that berth.
The compare column `detour_ratio_max` measures the same ratio.
The pod does not know its berth at each stop before it arrives, so the plan uses the berth that gives the largest ratio.
The conditions and the plan use the free-flow routes with each routing policy.
Each leg of the pod takes the route of the routing policy when that route keeps each party aboard within 1.5.
Otherwise, the leg takes the free-flow route, which the plan used.
The pod changes to another berth at its next stop only when the new route keeps each party aboard within 1.5.
Thus, the measured ratio of each party that boards with a limit of two or more in the `drop-offs` mode is at most 1.5, while these settings do not change.
A new stop before the first stop changes the route of the pod, so the pod adds a stop only before it gets track.
The project setting `sharedRideMaxStops` sets the maximum number of stops before the last stop, from 1 to 7.
The default is 3.
The stops do not change after the pod departs.
At each stop, the parties for that stop alight, and the pod continues to its next stop with the other parties.
While it waits for track at such a stop, its activity is **Continuing**.
Sharing does not wait for more parties.
A pod with passengers picks up parties on its way only with the project setting `onboardPickups`, which needs a limit above one and the `drop-offs` mode (see [onboard pickup design](design/onboard-pickup-design.md)).

The project setting `sharedRideJoin` selects the waiting parties that can join by their pickup state:

| Policy | Parties that can join |
| --- | --- |
| `unassigned` | The default. Parties that have no pod. |
| `reassign-existing` | Also parties that have an empty pod on its way, when the boarding pod already stops at their destination. |

With `reassign-existing`, dispatch releases the pod of a party that joins, as it does when an idle local pod takes a trip.
The stops of the boarding pod do not change, so the parties aboard get no new stop.
A party whose pod is idle at the origin boards that pod.

### Dispatch order

Passengers request travel between stations independently of pod selection.
Dispatch considers requests in submission order.
An idle local pod serves the oldest waiting passenger.
Otherwise, dispatch compares idle pods and empty pods that can divert.
An empty pod can divert when it goes to parking, when it moves for redistribution, or when dispatch released it from a pickup.
Dispatch uses the estimated pickup time, then the pod ID, as the tie-breaker.
An idle pod at the pickup station has an estimate of zero, because it boards at its own berth.

### Wait rules

Dispatch can wait for a busy pod if it should reach the pickup more than two seconds earlier.
The estimate includes travel, acceleration, braking, boarding, and unloading.
It does not predict traffic delays.
Dispatch waits for at most 30 simulated seconds before it uses an available pod.

| Rule | Behavior |
| --- | --- |
| `current` | Waits as described above. The server always uses this rule. |
| `strict` | Waits only when the busy pod can finish and reach the pickup before the 30 seconds end. Compare command only. |
| `none` | Never waits for a busy pod. Compare command only. |

### Pickup

Assigned pickup pods can depart while the berth is occupied and queue on its approach.
They claim the berth through local admission, not a remote reservation.
Passengers board only at a berth.

If pickup pods arrive out of order, the first available pod takes the oldest passenger at that station.
The other pod retains a pickup at the same station for the later order.

### Released pods

A pod that becomes idle at the pickup station can take a trip from a pod that is still on its way to the pickup.
Dispatch then releases the other pod, and the released pod can divert for a pickup at once.
A later trip in the same dispatch pass or in a later pass can take it.
If no trip takes it in the same pass, it goes to the nearest free berth.

A free berth has no pod, no claim, and no other pod or waiting trip that goes to it.
The pod keeps its destination when that berth is the nearest free berth.
When two berths have the same route cost, the current destination berth wins.
After it come the other berths of its station, then the berths of the other stations in network order.
The pod reserves the chosen berth, as a pod on its way to parking does.
A pod that has not left its berth can choose that berth and stays there.
A moving pod cannot choose its origin berth until it is at clearance distance from it.

A restore that removes the pod binding of a waiting trip also releases the pod on its way to that pickup.
The next dispatch pass sends that pod to the nearest free berth if no trip takes it.
When a released pod gives its berth claim to a passenger pod, it goes to the nearest free berth at once.

### Empty travel

An empty pod heading to parking can divert for a pickup.
It preserves all committed track and releases its unused parking claim.
A pod already committed to the parking inlet finishes that maneuver before returning to service.
A released pod also keeps all committed track and finishes a committed inlet first.
Empty pickup travel retains the original request ID and does not count as a passenger journey.
The fixed demo can still submit journeys directly to specific pods.

## Track admission and junctions

Pods accelerate and brake within their admitted track distance.
They extend reservations to cover the stopping distance at the lane speed plus two simulation ticks of travel.
On clear track, this allows steady cruising at 14 m/s across block boundaries.

The local controller divides lanes into exclusive blocks of up to 30 meters.
It retains trailing blocks until a four-meter pod and an eight-meter gap have cleared.

On a qualifying slow lane, the blocks are retained for a distance R of less than 12 meters.
A lane qualifies when it is straight and its speed limit is less than the square root of 14 (about 3.74 m/s).
The track near the lane must also be one chain of lanes without large classes, with a total turn of at most 90 degrees.
This track starts where a pod that can stop on the lane can be, and it ends 24 meters after the lane.
R is (5 + 2u) / cos(T/2) - u meters, where u is the largest stopping distance on that track, but not more than 3.5 meters, and T is its total turn.
Two small pods on this track must be at least 5 + v²/2 meters apart, but not more than 12, where v is the speed of the rear pod.
The station bank check uses the same rule for the parts of two paths that are on this track.
Large classes keep their 20-meter clearance.

Each junction has one conflict resource for nearby sections of its incident lanes.
The controller derives these sections from the same geometry used for movement, including the 12-meter clearance.
It acquires each continuous conflict section together, including the downstream cells needed to cross lane endpoints.
It releases the conflict resource after the pod reaches the end of that section.

Admission follows these rules:

- Requests that waited at least 10 simulated seconds take priority, oldest first.
- Younger requests prioritize pods with passengers, then assigned pickups, then other empty movements.
- Within each class, the oldest local admission request wins.
  Pod ID breaks a tie.
- Existing reservations remain protected.
- Admission uses the state before movement.
- Released resources become available on the next tick.

With platoons on, a pod that waits for the conflict section of a station diverge can coast.
The station diverge is the node where the entry lane of a station starts.
The pod coasts only when a pod outside its platoon waits for the same section at the same tick, and the pods that hold the section move.
Its hold point is before the end of its reservation by the square of the lane speed limit over four times the acceleration (24.5 m at 14 m/s), but not more than the gap to that end.
Coast does not brake a pod harder than its usual stop, so a pod that arrives at the speed limit still stops at the end of its reservation.
A pod that moves up slowly, such as the first pod of a queue, stops at the hold point.
A platoon member that must first own the cells that it shares also waits as a head, and it can coast.
It starts again so that it rolls into the section when the section frees.
The pod that admission will take first starts early enough to reach the section at speed.
Coast lowers only the commanded speed.
It changes no admission order, and the stop point of the pod does not move back.
A pod does not coast while it holds a junction or a node that it releases before the end of its reservation.
Pods of a large class and pods with an emergency do not coast.

The block model is conservative.
It does not model continuous car-following or optimized junction capacity.
The traffic model requires lanes at least 24 meters long, or 2R meters long for a qualifying slow lane.

### Virtual platoons

The project setting `platoonLimit` turns on virtual platoons.
Its value is the maximum number of pods in one platoon, from 2 to 4.
A value of 0, or no value, turns platoons off.
The **Platoons** field of the editor sets the limit.
The compare command does not read this setting.
It uses `-platoon-policies`, and the `virtual` policy has a limit of 4 pods.

With platoons on, a slow pod in a queue can link to the pod ahead on the same lane.
A mainline link needs shared succeeding lanes, and each route must have one speed limit.
A run can hold the entry lane of a station when both pods go to a berth of that station.
No run of such a pair continues past the entry node of that station, and no run starts on an entry lane.
A lane after the entry node of the destination station of either pod, and the entry lane of a station that is not the destination of both pods, never join a run.

Each link certifies a run of lanes that both routes share, from the lane of the follower.
The total turn along the run must be at most 120 degrees.
This total counts each turn in a curved lane and at each lane join one time, with its size and not its sign.
The link clearance is 12 m divided by the cosine of half of the turn, plus 0.01 m.
On such a run, two pods that are at least the clearance apart along the path are at least 12 m apart.
All links of one platoon have the same turn and clearance, and a link keeps them until it ends.
A link forms only when the two pods and their stop points are at least the clearance apart.

A linked pod can reserve the blocks and junction sections that the pods ahead of it in its platoon hold.
It shares only the blocks whose resources each pod releases at least 1 m before the end of the run.
Thus each pod that holds a shared resource is on the run.
It stops at least the clearance behind the stop point of the pod ahead, less the distance of 0.5 s of travel at its speed.
When a pod ahead releases a shared resource, the next pod in the platoon owns it.
Berths are not shared.
A mainline link can add the next shared lane while its total turn stays within the turn of the platoon.
After the last block that it can share, the link drains: the follower waits until the pods ahead pass the shared resources and hand them to it.
Then the link ends.
A link also drains when platooning stops, when one of its pods stops traveling, or when a different pod comes between the two pods.
While a linked pod holds a cell of a pod ahead, it reserves only the cells that its predecessor reserved.
A pod ahead in a platoon does not reserve a resource again while a pod behind it holds that resource.
A linked empty pod cannot divert.
The 12 m separation check does not change.

A saved state keeps each link in the `platoon` field of the follower.
The field gives the predecessor, the run as indexes into the two saved routes, the turn, and whether the link drains.
The restore checks the run, the turn, the speed limits, and the clearance against the network and the pods, and it does not plan the link again.
An invalid complete-lane link fails the physical tier under the existing recovery rules.
A link that drains before the save also drains after the restore.
The saved state does not keep the platoon limit.
The restore uses the `platoonLimit` of the project.

## Stations and parking

### Berth choice

Passenger journeys route to the station entry without a berth assignment.
The controller chooses the least-assigned reachable berth when the next reservation of the pod starts on the final lane of the road route.
It also chooses the berth when that reservation reaches the last block of the road route, as it can on a short final lane.
With platoons on, the controller also tries the choice for a pod that is not of a large class when the entry lane starts within 300 m, or when the next reservation reaches that lane.
Then the run of a platoon can grow onto the entry lane.
If the early try finds no berth, the pod continues and tries again at the usual point.
The controller can change this choice before it reserves a berth branch.
A passenger or pickup pod can choose a free alternate berth before it reserves the next station branch.
A route change preserves all admitted track.

A pod reserves its berth before it enters the final inlet block.
It keeps the berth through unloading and idle time, until its departure clears the resource.
Other pods can queue on the inlet while through traffic uses the separate through lane.

Station maneuver roles describe this existing movement and reservation behavior.
They do not add a second station controller or change admission priority.
The controller makes local reservations, not a whole-journey timetable.

### Station layout

Station entry, berth, and exit connections have stable identities.
Passenger and parking stations can have multiple berths.
Berths can connect through intermediate arrival and departure lanes, separate from through traffic.
Station-local paths cannot cross another berth or station.
Each station retains a direct entry-to-exit through lane.

### Compact bays

The optional network member `maneuverGroups` adds angled bays for Compact pods to a station.
Each group has an `id`, a `stationID`, and the `laneIDs`, `berthIDs`, and `reverseLaneIDs` of the group.
A pod on a reverse lane faces against its direction of travel.
Each lane and berth of a group admits only Compact pods.
Each berth of a group needs a closed bay chain.
This is a chain of group lanes from a branch node to the berth, and a chain of reverse lanes back over the same nodes.
A node inside a chain has only the four lanes of its chain, and the berth node has only its two lanes.
The reverse lanes of the group are the lanes of these chains back to the branch nodes.
A lane of a group has no minimum length.

Only one pod at a time moves on the lanes of a group.
All cells of the group lanes share one junction resource.
A pod in a virtual platoon cannot share this resource, or a lane of the group, with the pod ahead.
Each cell of a bay chain also holds the berth of the chain, so a pod in a bay chain always owns that berth.
When the reverse flag changes between two lanes of a route, the pod stops for 1 second at the start of the second lane.
Pickup and emergency estimates do not include this stop.

When the server prepares the network, it sweeps the body of a Compact pod along each lane of a group.
The sweep includes the turns in place at the nodes and at the berths.
Each lane must be clear of the parked envelope of each berth of the group, except the berth of its own chain.
The envelope is 5 m long and 2.1 m wide, with its center 1.2 m ahead of the berth node.
Each two envelopes of a group must also be clear of each other.
Otherwise the network is not valid, and the error names the lane and the berth, for example `maneuver group "compact-bays" lane "bay-2-in-3" is not clear of berth "bay-3"`.
The lane gap audit and the separation check skip each pair that the sweep clears.

A pod that waits for a full bay can wait for a pod that waits for the exit.
As at an ordinary station, a custom network with more pods than its bays and roads can hold can stall.
The `compact-station` preset has enough room for its pods.
The map shows a pod in a bay as a round mark, with no heading.

The London presets put these bays at 7 or 8 stations by default, and each of their pods is Compact.
The `-compact-stations` flag of the scenario command sets these stations.
One visit holds the aisle for about 30 to 43 seconds.
Use a Compact station only where its demand share times the request rate is below about 0.7 trip ends per minute (see [LondonFull Compact stations](london-full.md#compact-stations)).

### Parking depots

The optional network member `depots` makes a Parking station a depot of queue lanes.
Each depot has a `station` and its `lanes`.
Each lane lists the `berthIDs` of its slots from the head to the tail.
A depot has 1 to 8 lanes, and it lists each berth of its station one time.
The station must be a Parking station with no banks.
Its slots need one class set with no large class.

Each lane is a straight line of nodes.
A lead-in goes to the tail slot, a chain lane goes from each slot to the slot ahead, and a lead-out leaves the head slot.
A fan path goes from the station entry to each lead-in, and a throat path goes from each lead-out to the station exit.
The entry has exactly one incoming lane.
The lanes of a depot are parallel.
The lane gap audit checks each pair of paths that includes a depot lane or a slot.
Two slow paths of different lanes of one depot need only 3.1 + v²/2 meters, where v is the higher speed limit of the two, when a swept-body check clears them.
At the depot speed, this is 4.09 meters.
Other pairs keep their usual clearance.
The rule assumes that the guideway keeps a pod on its lane and at the heading of its lane (see [station banks](station-banks.md#depot-lanes)).
A chain lane needs only its reduced retention, because each of its cells holds the slot ahead and both of its nodes.
At about 1.41 m/s, slots can be 6 meters apart.
A depot lane cannot have a separation group, and a virtual platoon run cannot hold a depot lane.

A pod that goes to park at a depot gets the entry as its destination.
It can divert until it binds a slot.
It binds when it owns the last cell of the incoming lane.
It takes the slot behind the rearmost taken slot of the lane with the fewest pods, and the lowest lane index on a tie.
A slot is taken when a pod is idle at it or goes to it.
A lane with a faulted or withdrawn pod, a full lane, and a lane that is closed or does not admit the class of the pod take no pod.
When no lane takes the pod, it looks for an open berth at another station, and it waits at the entry when it finds none.
A pod at the entry with no slot shows **No parking available**.

A pod in service starts to move up when the slot ahead of it is free.
A move-up keeps the parked tick of the pod, and a new arrival sets it.
Only the head of a lane, at its first slot, can leave the depot.
Dispatch, pickup transfer, and guarded positioning take the head that parked first, and then the lowest lane index.
They skip a head that is faulted, withdrawn, or not eligible for the request, and a head whose lead-out or throat path is blocked.
A faulted or withdrawn pod keeps its slot, and the pods behind it wait.
An operational target cannot be a depot station.

The browser editor keeps the depots of a project, but it cannot make a depot.
It refuses each station action that changes the geometry or the berths of a depot station.
A project with a depot gets the full network validation of the server in the editor.
The editor runs the depot validation of the server first, so an invalid depot draft gets the server depot error, not a lane gap or bank error.
The London presets have depots by default, and the `-parking-lanes` flag of the scenario command sets the depot lanes of a ring or a London preset (see [generated scenarios](reference.md#generated-scenarios)).
A London depot is at most 200 meters long (see [LondonFull depots](london-full.md#depots)).
In a LondonFull run at 60 requests per minute, the depots served within four requests of ordinary Parking facilities, and empty pods stopped inside the depots for 1,225 more pod-seconds while other pods of the same depot moved.

### Idle pods

An idle empty pod clears its berth when it blocks a passenger arrival, an assigned pickup pod, or an empty relocation.
It also clears its berth for a pod that must pass through that berth to leave the station.
It first reserves a free reachable parking berth and retains its origin until physical clearance.
If parking is full or unreachable, it reserves reachable passenger space instead.
It prefers local space that no request targets.
The pod shows **No parking available** only when no reachable physical space exists.

### Empty moves and parking

Empty relocations yield unadmitted destination claims when a local passenger or pickup needs the same berth.
Physical ownership and admitted destination resources remain protected.
Empty moves have no boarding or unloading delay and do not count as passenger journeys.
Optional redistribution moves an idle empty pod to a busy station that has no pod.

Parking serves no passengers.
Parked pods return to service automatically when assigned to a pickup request.
After the demo, request a trip from Harbor or Garden to see an available pod return for pickup.

The tests establish progress for feasible supplied scenarios, not for every saturated network.

## Redistribution

Redistribution is off by default.
When it is on, the simulation runs guarded positioning.
Guarded positioning moves an idle empty pod to a busy station that has no pod.
The demand weights of the stations select the busy stations.
The policy moves pods only while the demand rate is below one request per minute for each 20 pods of the fleet.
While generated demand runs, the server uses the configured rate of the demand.
Otherwise, it uses the mean rate of the requests since the last reset.
Thus a fleet of 20 pods or fewer makes no move while generated demand runs.
The policy stops moves to busy stations when the newest request is more than 180 seconds old, or when a waiting request has no pod.
It also stops these moves when more than two fifths of the fleet works, or when the positioning moves reach the boardings.
While the demand rate is low, the policy also moves an idle pod that blocks a berth.
The pod goes to a free berth of a busy station within 180 seconds of travel, or else to the nearest free Parking berth.
Passenger assignments take priority over empty positioning.
Before a pod moves, the policy reserves a free destination berth, and it leaves another free berth at the station.
A cooldown limits repeated moves.
A remote reservation yields to passenger traffic until the empty pod enters the final admitted block.
Admitted track and physical berth ownership remain protected.

## Faults

A session can stop a pod with a pod fault, or block a lane segment with debris.
Faults are off during the traffic demo, and after the demo until a reset or a project apply.
A pod fault brakes the pod to rest, and its pending pickups go to other pods.
After the evacuation delay, the riders leave the pod at rest, and their orders end interrupted.
Other pods route around a fault, or wait.
The **Fault** button of the [pod inspector](user-guide.md#inspect-a-pod) and the `fault` command start a pod fault.
Only the `fault` command starts debris, and the map does not draw debris.
See [faults](operations.md#faults) for the settings and the [fault commands](protocol.md#fault-commands) for the protocol.

## Server and browser

### Shared state

One server owns the simulation clock, commands, and demand settings.
Browsers receive shared gzip state deltas over WebSocket and show the connection status.
The simulation view requires WebSocket and native gzip decompression, with no HTTP polling fallback.
They fetch the network topology on connection, and after the session epoch, the project revision, or the server start ID changes.
Controls wait for server confirmation.
During the wait, the line below the panels shows **Shared session / waiting for command confirmation**.
**From** and **To** send no command, so you can change them during the wait.

Pod selection, origin, destination, and the open inspection panel stay local to each browser.
Imported background images stay in the editor, in browser IndexedDB storage, and in the exported project file.
Live map settings travel with the project and topology.
Each viewer fetches its visible tiles directly.
The shared simulation receives the network geometry and settings.

### Motion

The map buffers 150 ms of snapshots and interpolates movement along lanes between updates.
Controls and order status use the latest server state.
Pauses, speed changes, resets, rewinds, the start and end of the traffic demo, and long connection gaps clear buffered motion.
Rendering never predicts movement beyond the latest received position.
