# Central Early queue diagnosis

The station mirror regression depends on the platoon setting in these two Central Early schedules.
With platoons off, mirroring reduces average wait by less than 1% and leaves eight or four fewer unfinished requests.
With virtual platoons, mirroring increases average wait by 17.6% and 14.3%.
The largest added stopped-pod observation count occurs on the approach to Paddington.
These results locate a workload and geometry interaction, without establishing its mechanism or a safety defect.

## Method

The study uses simulation source `7cd5fe9` and the frozen fixtures from the [mirrored-layout study](station-mirror-load.md).
Eight arms cover earlier and mirrored geometry, platoons off and virtual, and seeds 1 and 2.
Central Early endpoint demand runs at a nominal 10 requests per simulated minute for six hours.
Each arm then has one hour to finish.
All arms reach the seven-hour cutoff with unfinished requests.
Virtual platoons have a four-pod limit.
Sharing, redistribution, station buffers, and pickup swaps remain off, with free-flow routing.

Every geometry and platoon variant for one seed receives the same ordered request times, origins, and destinations.
All four virtual-platoon result records match the earlier mirrored-layout study exactly.
Short off and virtual pilots also match the production comparison loop across every result field.
These controls check that the added observations do not change the simulated results.

## Passenger results

Each unfinished count gives earlier / mirrored geometry.
Wait change compares average waits, including elapsed wait for requests still unboarded at the cutoff.

| Seed | Platoons | Unfinished | Average wait change | Track-wait samples, earlier / mirrored |
| --- | --- | ---: | ---: | ---: |
| 1 | Off | 961 / 953 | -0.48% | 180,716 / 189,391 |
| 1 | Virtual | 706 / 939 | +17.59% | 35,560 / 182,728 |
| 2 | Off | 894 / 890 | -0.19% | 147,078 / 149,901 |
| 2 | Virtual | 703 / 839 | +14.32% | 34,586 / 140,263 |

Virtual platoons leave fewer unfinished requests than platoons off for both layouts and both seeds.
The benefit is smaller with mirrored geometry.
Neither setting drains the requests in this window.
This study does not qualify a capacity increase or change the default platoon setting.

## Queue observations

The main added queue is on `london-link-067-ab-2`, toward Paddington approach lane `940GZZLUPAC-road-in-03`.
In virtual arms, stopped pickup pods with reason `Pod ahead` contribute 20,403 / 157,025 observations for seed 1.
The corresponding seed 2 counts are 19,769 / 123,170.
These are pod observations at one-second intervals, so several queued pods can contribute during the same second.
Every observation in these four rows belongs to an assigned pickup pod.

With platoons off, the largest queue instead occurs toward Baker Street on `london-link-035-ba-2`.
The longest consecutive sampled stop in any arm spans 58 observations.
The longest mirrored virtual episode spans 55 observations for each seed.
The observation interval can miss movement between samples, and blocker IDs can change during an episode.
These episodes therefore do not prove continuous deadlock or starvation freedom.

Sparse exported states at hours three and six retain the physical queue and platoon certificates for further diagnosis.
At hour six, earlier virtual geometry has two and three draining links for seeds 1 and 2.
Mirrored geometry has zero draining links at those two checkpoints.
These snapshots show different platoon states, without proving that drain behavior causes the queue.
The [Paddington geometry intervention](paddington-mirror-intervention.md) tests this location without changing platoon rules.

## Validation and limits

All 201,600 parent observations pass safety, speed-limit, and unique-request accounting checks.
The final request-timing census agrees with pending, submitted, and completed totals.
No request is skipped.
The stopped-pod histogram equals the comparison loop's stopped census, with disjoint occupied, assigned-pickup, and other classes.
Off arms have no coupled pods or saved platoon links.

The route-successor field searches the complete assigned route for a unique occurrence of the pod's current lane.
Repeated lane occurrences remain unresolved because the public snapshot does not expose the current route index.
The successor is not a measurement of the denied reservation resource.

This diagnosis adds no restore qualification.
It exports 16 sparse states and does not restore them during the parent runs.
The earlier [mirrored-layout restore checks](station-mirror-load.md#restore-checks-and-limits) remain separate evidence.
Two seeds and sampled safety checks do not establish an indefinitely sustainable demand rate.
No project, saved format, wire protocol, or default changes.

[Result fields](measurements/central-mirror-diagnosis.csv), [stopped-pod histograms](measurements/central-mirror-waits.csv), and [metadata](measurements/central-mirror-diagnosis.json) retain the measurements and hashes.
Local artifacts are in `~/.cache/agents/podsim/central-mirror-diagnosis-20260930/`.
