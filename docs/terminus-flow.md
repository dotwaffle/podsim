# Terminus burst service and reservation measurements

Train-sized bursts increase pickup waits at Euston and Paddington in this Central fixture.
Station buffers increase waits in all eight unlimited-queue comparisons, including steady demand.
The buffer default remains off.

## Fixture and validation

The fixture uses source `4e36e9f` and the frozen mirrored LondonCentral project.
Each station has two passenger berths.
Both seeds offer 239 outbound parties during a 20-minute arrival window, with a 90-minute run cap.
Steady arrivals occur every five seconds.
Burst arrivals deliver 120 parties at five seconds and 119 parties at 605 seconds.
Each steady/burst pair uses the same ordered origins and destinations, with different arrival timestamps.

The 32 cells vary station, seed, buffers, queue limit, and burst size independently.
The live queue limit is 200 pending requests.
The study queue limit is one million.
Sharing, reassignment, and redistribution remain off.
Routing uses free-flow costs and virtual platoons have a four-pod limit.

All cells pass safety, speed, and request conservation checks once per simulated second.
Each passes physical restores at 600 and 1,200 seconds, followed by a separate dense 60-second continuation.
Those continuations disable new buffer admissions.
Four shorter pilots pass checks every tick and match production result aggregates exactly.
The observer's per-request waiting ticks reconcile with boarding records and censored pending ages.
Every berth and station conflict resource has one classification per sampled tick.
Final station transitions are counted without adding another tick sample.
Independent review and regression tests check observer purity, lookup cursors, and final-transition accounting.

## Pickup service

All unlimited-queue cells serve all 239 parties and drain within the cap.
The table compares burst cells with identical accepted requests.
Times use simulated seconds.

| Station | Seed | Buffers | Mean pickup wait | P95 pickup wait | Run ends |
| --- | ---: | --- | ---: | ---: | ---: |
| Euston | 1 | Off | 940.5 | 1,810.1 | 3,235 |
| Euston | 1 | On | 1,919.2 | 3,556.5 | 4,840 |
| Euston | 2 | Off | 977.7 | 1,806.1 | 3,535 |
| Euston | 2 | On | 1,919.2 | 3,556.5 | 5,017 |
| Paddington | 1 | Off | 1,660.0 | 3,151.1 | 4,566 |
| Paddington | 1 | On | 1,947.1 | 3,657.9 | 5,094 |
| Paddington | 2 | Off | 1,658.4 | 3,144.4 | 4,592 |
| Paddington | 2 | On | 1,940.9 | 3,645.4 | 5,107 |

With buffers off, steady mean waits range from 701.1 to 789.0 seconds at Euston and about 1,407 seconds at Paddington.
Buffers also increase these steady waits.
This fixture does not establish a sustainable station capacity.
It measures finite outbound demand with no inbound passenger service or background traffic.

The bounded queue skips nine burst requests in every buffer-on cell.
Buffer-off cells skip zero at Euston, four at Paddington seed 1, and three at Paddington seed 2.
Steady cells skip none.
Do not compare bounded-queue wait means as though every arm served the same requests.

## Berth reservations and local waits

The observer separates incoming claims from boarding, unloading, idle pods, blocked departures, and departing footprints.
An incoming claim is a destination-berth claim held by a pod that is not yet at that berth.
It does not mean that the berth is physically occupied.

In unlimited burst cells, incoming claims account for 24.7–27.1% of Euston berth samples with buffers off and 59.9–62.1% with buffers on.
Paddington changes from 22.4–22.6% to 58.4–58.7%.
These fractions use each arm's full run duration, which differs across arms.
They describe reservation states, not directly comparable occupancy rates over one common window.

Free berths also coexist with pending requests for much of each run.
This does not prove that a local idle pod can serve those requests.
Pending parties can have assigned pickups elsewhere, and a free berth provides no pod by itself.
The lane records separate upstream mainline, approach, entry, berth access, departure, and exit samples.
Stopped counts include pods with no wait reason.
Station conflict counts measure deduplicated resource ownership, not physical conflict use.

The measurements make incoming berth claims and pickup supply the next diagnostic targets.
They do not establish a cause or justify a clearance change.
The approved follow-ups compare empty reserves, sharing, and fixed station-entry platoons under matched demand.
Geometry trials need a demonstrated local bottleneck before changing access or exit layouts.

## Evidence and limits

The raw measurement data is in git history.
The production oracle compares result aggregates, not individual timing parity.
The census retains individual identities and checks their timing totals separately.
Two stations and two seeds do not qualify a default change or a LondonFull capacity claim.
Concurrent runs provide no CPU comparison.
