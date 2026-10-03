# Group and mixed-fleet service screen

The bounded Group screen improves completion counts but exceeds individual service limits.
It does not qualify sustained capacity or default adoption.

The frozen source is `5a57e4c96114030be5e15721e70a24c82a68895f`.
The fixture uses the qualified straight large-motion network with two berths at each station.
All admitted lanes meet the existing large-body geometry and resource-cell rules.
Authored lane speeds remain 14 m/s.
Group bodies remain six meters long with eight seats.
Every arm prohibits large or mixed virtual links and compact certificates.

Each timing variation offers 48 whole parties at stations `a`, `b`, and `c`.
Each origin offers parties of one, two, three, and four passengers in each wave.
The first three parties consent to sharing.
The four-person party requests private service.
Consent and event identities match across each comparison.
The waves start at zero, 120, 240, and 360 seconds.
The second variation adds zero, seven, thirteen, and nineteen seconds to these waves.
All 120 passengers remain in their original whole parties.

The observation cap is 2,400 seconds and the live queue limit is 200.
The sharing limit stays at four parties with drop-offs and three intermediate stops.
Two extra offers at ticks 144,000 and 144,001 remain future events in the half-open observation window.
No arm skips or refuses a service offer.

| Fleet | Vehicles | Nominal seats | Peak passengers in one pod | Completed in variation 1 | Completed in variation 2 |
| --- | ---: | ---: | ---: | ---: | ---: |
| Two Legacy, four Compact | 6 | 32 | 4 | 40 / 48 | 40 / 48 |
| Six Group | 6 | 48 | 8 | 48 / 48 | 48 / 48 |
| Two of each class | 6 | 40 | 6 | 45 / 48 | 45 / 48 |
| Four Group | 4 | 32 | 8 | 46 / 48 | 46 / 48 |
| Two Group, one Legacy, two Compact | 5 | 32 | 6 | 42 / 48 | 42 / 48 |

The first two candidates match vehicle count, with unequal seats.
The last two match nominal seats, with unequal vehicle count and the declared placement prefix.
Nominal seat matching does not imply equal effective sharing capacity or equal initial placement.
Legacy pods have eight seats but admit new singleton parties only.
Baseline Compact pods admit each offered party through four.
Measured Group occupancy reaches eight passengers under the unchanged four-party limit.

Every arm checks native separation, speed, owned stopping bounds, ownership, and request conservation on all 144,000 ticks.
All checks pass.
A full 120-second plain-versus-observed pilot matches exported state, metrics, and request timings.
Completed shared detours remain within the existing 1.5 limit.
CPU comparisons do not run while other agents work.

Each arm restores at the first occupied movement with retained origin ownership, then at 300 and 900 seconds.
Every restore uses the physical tier without demotions, requeues, or drops.
Initial discrete state, party identities, consent, and distance baselines match.
Position and distance changes stay within one micrometer.
The restored fleet checks safety and accounting on every continuation tick.
Every request present at each checkpoint finishes within its separate frozen 2,400-second continuation cap.
The service fixture produces no natural intermediate unloading.
The existing native Group intermediate-unloading regression covers that separate restore phase.

Two private observer setup failures remain in the evidence.
The first required all owned nodes to survive restore, including a future `parking-exit` grant.
The second treated every future route-release entry as a past resource.
The corrected check requires current and unexpired past resources plus retained origin and berth ownership.
Future track and node grants can change because saves omit speed.
The observer counts these changes and retains live and cold ownership traces.
Guard tests reject current-node, origin-berth, position, party, and consent changes.

Aggregate gains do not remove individual failures.
Both timing variations exceed pickup wait and journey limits in all four comparisons.

| Candidate | Pickup exceedances, variations 1 / 2 | Journey exceedances, variations 1 / 2 |
| --- | ---: | ---: |
| Six Group | 4 / 4 | 4 / 4 |
| Two of each class | 11 / 11 | 8 / 8 |
| Four Group | 16 / 16 | 11 / 11 |
| Two Group, one Legacy, two Compact | 20 / 20 | 16 / 17 |

The six-Group arm adds 485.25 seconds of wait for event `wave-3-b-0` in both variations.
Its allowed increase is 30 seconds.
The [measurement record](measurements/group-fleet-service-screen.json) retains every event, matched cohort, censored pair, and exceedance.
Unfinished baseline or candidate requests cannot pass a no-harm gate by omission.

A separate capability census offers private parties of one through eight to each fleet.
It records actual admission, whole-party completion, refusal, and censoring at the same frozen cap.
The baseline completes parties one through four and refuses parties five through eight.
Every Group and mixed fleet completes all eight whole parties.
These refusals remain capability differences, not service-gain evidence.
Two timing variations and finite bursts do not qualify a demand rate or a later source revision.
