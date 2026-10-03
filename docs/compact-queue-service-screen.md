# Compact queue service screen

The first bounded screen passed every physical safety check but completed fewer trips with compact spacing.
It does not qualify passenger service gains or default adoption.

The runtime was `a01ec3e`.
The fixture starts seven pods at their original berths, with no saved-pose staging.
It uses the occupied-buffer test network plus the example return lane.
Four private singleton journeys start at time zero with selected pods.
A fifth unassigned market-to-harbor request arrives at 900 seconds.
Each arm retains the same 2,000-second observation cap and all unfinished riders.

Station lanes run at 2.5 m/s.
The paired road speeds are 2.5 and 14 m/s.
Buffers and virtual platoons remain enabled with limit four.
The queue policy is the only difference within each pair.

| Road speed | Queue policy | Completed / offered | Riders still aboard | Largest compact group |
| --- | --- | ---: | ---: | ---: |
| 2.5 m/s | ordinary | 2 / 5 | 3 | 0 |
| 2.5 m/s | compact-v1 | 1 / 5 | 4 | 3 |
| 14 m/s | ordinary | 3 / 5 | 2 | 0 |
| 14 m/s | compact-v1 | 2 / 5 | 3 | 4 |

Every arm conserves all five requests.
No pending request or skipped offer remains at the endpoint.
Unfinished onboard riders remain censored.
The fast-road compact arm forms a natural four-member stopped queue spanning 18.03 meters.

The single queued offer waits 148.03 seconds in the fast-road ordinary arm and zero seconds in its compact pair.
That pickup differs because the destination's original idle pod remains available longer in the compact arm.
It does not establish an aggregate pickup-tail improvement.
Journey completion is worse at the unchanged endpoint.

The captured slow-road endpoint identifies a berth-clearing defect.
An idle pod owns the destination berth while a compact head waits well before its normal admission horizon.
The head does not set the occupied-berth signal that `clearBlockedBerths` requires.
A cold restore reproduces the missing signal.
Setting only that normal signal starts the existing idle departure.
The correction supplies that signal through a read-only legal berth and suffix probe.
The existing clearing path moves idle blockers.
The compact head retains its stopping ownership and certificate until ordinary recovery allows suffix admission.

The result file is [compact-queue-service-screen.json](measurements/compact-queue-service-screen.json).
The source and captured states are in the current service evidence cache under `compact-service-screen`.
The setup development logs record invalid initial fixture assumptions separately from physical results.
The final screen retains its original observation cap.

The same four arms were rerun with the clearing correction.
Only output paths changed in the screen source.
The result file records the candidate base commit and exact production source hashes.
Every tick passed the same safety and conservation checks.
The ordinary arms retained exactly the same per-request timings.

| Road speed | Queue policy | Corrected completed / offered | Riders still aboard | Largest compact group |
| --- | --- | ---: | ---: | ---: |
| 2.5 m/s | ordinary | 2 / 5 | 3 | 0 |
| 2.5 m/s | compact-v1 | 2 / 5 | 3 | 2 |
| 14 m/s | ordinary | 3 / 5 | 2 | 0 |
| 14 m/s | compact-v1 | 3 / 5 | 2 | 3 |

The correction recovers one completion in each compact arm.
Compact completion counts now match ordinary spacing within this fixture.
The fifth offer waits 525.45 seconds with slow roads and 136.98 seconds with fast roads in the corrected compact arms.
Its ordinary waits remain zero and 148.03 seconds.
These single-offer differences do not establish an aggregate pickup-tail gain.
The corrected arms do not form a four-member stopped queue.
The earlier 18.03-meter span remains evidence of storage formation, with the idle-clearing defect present.

Independent cold restores passed with both queue policies and retained physical ownership during recovery.
Busy or assigned berth occupants remained protected.
Removing the clearing call compiled and failed the endpoint eviction assertion with both policies.
The correction fixes the liveness regression.
A useful passenger service gain and default adoption remain unqualified.
