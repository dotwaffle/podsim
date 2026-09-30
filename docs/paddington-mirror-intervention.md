# Paddington geometry intervention

Changing only Paddington's node positions reproduces most of the Central Early wait regression with virtual platoons.
Restoring its earlier positions in the mirrored network nearly returns average wait to the earlier network's value.
This isolates a station geometry effect in two fixed schedules, without identifying the underlying platoon mechanism.

## Method

The study follows the [Central queue diagnosis](central-mirror-diagnosis.md).
It reuses the frozen diagnostic binary from source `7cd5fe9`.
Eight arms cover two seeds and four layouts:

- The earlier network.
- The fully mirrored network.
- The earlier network with only Paddington's mirrored positions.
- The mirrored network with only Paddington's earlier positions.

Each intervention transfers exactly eight Paddington internal node positions from its donor layout.
Every other decoded project field equals its base layout, including lane controls, station definitions, fleet, demand, and settings.
The position changes also affect incident lane geometry.
The fixtures do not change lane IDs, connections, or controls.

Central Early endpoint demand runs at a nominal 10 requests per simulated minute for six hours.
Each arm then has one hour to finish.
Virtual platoons have a four-pod limit.
Sharing, redistribution, station buffers, and pickup swaps remain off, with free-flow routing.
All four layouts for each seed receive byte-identical ordered request schedules.
Earlier and fully mirrored controls match every result field in the original mirrored-layout study.
Short intervention pilots also match the production comparison loop.

## Results

Average wait includes elapsed wait for requests still unboarded at the cutoff.
Every arm reaches the seven-hour cutoff with unfinished requests.

| Seed | Layout | Average wait | Unfinished requests | Track-wait observations |
| --- | --- | ---: | ---: | ---: |
| 1 | Earlier | 4,223.83 s | 706 | 35,560 |
| 1 | Fully mirrored | 4,966.62 s | 939 | 182,728 |
| 1 | Earlier, mirrored Paddington | 4,954.41 s | 939 | 181,328 |
| 1 | Mirrored, earlier Paddington | 4,225.57 s | 709 | 36,438 |
| 2 | Earlier | 4,106.69 s | 703 | 34,586 |
| 2 | Fully mirrored | 4,694.70 s | 839 | 140,263 |
| 2 | Earlier, mirrored Paddington | 4,711.21 s | 844 | 135,245 |
| 2 | Mirrored, earlier Paddington | 4,103.64 s | 699 | 38,986 |

Mirroring only Paddington increases average wait by 17.30% and 14.72% against the earlier controls.
Restoring only Paddington reduces average wait by 14.92% and 12.59% against the fully mirrored controls.
Those restored averages differ from the earlier controls by less than 0.1%.
The track-wait observation counts also move toward their donor layout's result.
These differences are not additive estimates of each station's contribution.

## Validation and limits

All 201,600 parent observations pass safety, speed-limit, and unique-request accounting checks.
Final request-timing census agrees with pending, submitted, and completed totals.
No request is skipped.
The stopped-pod histogram equals the comparison loop's stopped census, with disjoint occupied, assigned-pickup, and other classes.

The study exports 16 sparse physical states at hours three and six.
It adds no restore qualification.
One-second observations can miss movement between samples and do not prove continuous deadlock or starvation freedom.
The longest sampled stop spans 68 observations in the mirrored network with earlier Paddington, despite its lower average wait.

The result does not identify which lane geometry, certificate, or reservation rule causes the added queue.
The later [reservation diagnosis](paddington-reservation-diagnosis.md) records actual guard failures and predecessor-frontier limits without changing the rules.
It does not establish a platoon safety defect or support weaker clearance rules.
It also does not qualify sustained capacity, other demand bands, or LondonFull.
The project geometry, platoon defaults, protocol, and saved format remain unchanged.

[Results](measurements/paddington-mirror-intervention.csv), [stopped-pod histograms](measurements/paddington-mirror-waits.csv), and [metadata](measurements/paddington-mirror-intervention.json) retain the measurements and frozen hashes.
Local artifacts are in `~/.cache/agents/podsim/paddington-mirror-intervention-20260930/`.
