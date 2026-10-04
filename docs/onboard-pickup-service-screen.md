# Onboard pickup service screen

An occupied pickup reduced waiting for three explicit manual orders in the example network.
All parties retained their original consent and completed in both policy arms.
This screen uses one 4 m legacy pod and does not qualify network capacity or default adoption.

## Matched offers

The pod starts at Harbor.
At tick zero, submit shared parties from Harbor to Market and Harbor to Garden.
Submit a third party from Garden to Market at the same tick.
Run a separate case with the third party private.
Each policy pair uses the same ordered offers and consent.
Sharing permits four parties, drop-off mode, and three intermediate stops.
The horizon is 600 seconds.

Free-flow, congestion, queue, and predictive routing produced the same values in this fixture.
There is no competing pod or congestion in this network run.
With three orders, the nearest-rank 95th percentile is the maximum.

| Pickup party consent | Occupied pickups | Completed | Maximum pickup wait | Maximum request-to-completion time | Empty distance | Maximum detour ratio |
| --- | --- | --- | --- | --- | --- | --- |
| Shared | Off | 3 of 3 | 225.57 s | 269.28 s | 1,844.21 m | 1.19862 |
| Shared | On | 3 of 3 | 46.05 s | 89.77 s | 0 m | 1.19862 |
| Private | Off | 3 of 3 | 225.57 s | 269.28 s | 1,844.21 m | 1.19862 |
| Private | On | 3 of 3 | 225.57 s | 269.28 s | 1,844.21 m | 1.19862 |

The shared pickup joins after alighting at Garden and waits through the accepted boarding interval.
Both remaining parties then travel to Market.
Moving party occupancy increases from 1.34646 to 2.0.
The private pickup retains the ordinary dispatch path.
Its complete result matches the policy-off result after removing the policy label.

## Qualification scope

All 16 runs completed every order with no pending or unaccounted request.
The runs performed 576,000 safety checks and 576,000 contract and conservation checks.
No party exceeded the vehicle's four seats or the 1.5 detour cap.
Only the shared policy-on runs produced boarding records.
A single pod cannot test separation between vehicles.

Native tests cover actual repeated pickups, oldest completed-history retirement at eight records, positive baselines, per-party distances, boarding clocks, rejection atomicity, and phase restores.
Public save, stream, editor, and comparison tests passed on the combined implementation.
The full Go suite, focused race checks, lint, vet, browser tests, and both WASM builds passed.
Actual native save/load tests preserve partial dwell, identities, baselines, and completion metrics.
Logical retries requeue the original shared orders and retain an authored rail connection exactly once.
Current generated rail orders remain private and cannot join an occupied pod.

The [measurement](measurements/onboard-pickup-service-screen.json) pins the fixture and native source hashes, ordered offer hashes, every result, and the scope of this screen.
The fixture and run logs are retained with the local implementation evidence.

## Encoding and policy

The full typed save maxima include current compact certificates and eight retained riders.
Historical, modern recorded, and mixed saves remain below the unchanged 80 MiB cap.
The corresponding full and replacement-delta frames remain below the unchanged 64 MiB cap.
These independent encoding maxima do not describe reachable physical placements.

| Representation | Maximum saved JSON | Maximum full frame | Maximum replacement delta |
| --- | ---: | ---: | ---: |
| Historical | 83,698,502 bytes | 61,192,276 bytes | 61,312,774 bytes |
| Modern recorded | 83,612,702 bytes | 62,226,376 bytes | 62,349,874 bytes |
| Mixed | 83,655,602 bytes | 61,709,326 bytes | 61,831,324 bytes |

Saved records use source-bound berth-index tuples.
The equivalent widest direct-ID save needs 84,722,402 bytes and exceeds the cap.
The 2,600 saved waiting records, eight stored riders, and 300-pod bounds remain unchanged.

The project-3 policy defaults to false and requires sharing above one party in drop-off mode.
Comparison accepts `-onboard-pickups off,on` as an independent dimension with explicit policy provenance.
A policy-off restore retains accepted riders and boarding time while refusing new occupied pickups.
