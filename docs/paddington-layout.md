# Current-source Paddington position trials

Restoring all eight earlier Paddington node positions improves selected service outcomes in both seeds, but leaves growing backlogs and individual regressions.
Neither partial position group passes the live safety checks.
These trials do not justify adopting different geometry or implementing independent berth banks.
Authored projects, operating defaults, and physical safety rules remain unchanged.

## Fixtures and validation

The trial uses frozen source `a54281e` and the mirrored LondonCentral fixture.
The full-restoration donor differs only in eight internal Paddington node positions.
The generated fixtures retain every other decoded project field, including controls, connections, fleet, demand, and berth positions.
Authored node and lane IDs remain unchanged.
Computed track cells, conflict resources, incident lane lengths, and turn geometry can change.

The first arrival/departure split fails project validation before simulation.
It moves only one endpoint of mirrored coordinate pairs, placing entry and exit at the same location and collapsing the bypass lane.
The revised groups keep those endpoint pairs together.
The throat group changes diverge, merge, entry, and exit positions.
The berth-link group changes both arrival and departure positions for each of two berths.
Each group changes four positions and cannot isolate one coordinate, lane, or conflict resource.

Four dense pilots pass production-result parity, safety, and restore checks for the revised fixtures.
The primary workload offers Central Early10/min demand for six hours, with a seven-hour cap and seeds 1 and 2.
It uses a queue limit of 1,000,000, free-flow routing, and virtual platoons with four pods.
Sharing, buffers, pickup reassignment, and positioning remain off.

The four complete baseline/full-restoration arms pass safety, speed, and unique-order checks once per simulated second.
Physical restores at three and six hours retain bindings, poses, admission ages, and saved platoon records.
Each copied continuation passes 60 seconds of dense checks.
Both mirrored arms match the [predictive-study free-flow baselines](predictive-service.md) exactly in results, schedules, requests, pending records, and terminus reports.

## Safety rejections

The throat-only seed-1 arm fails a live safety check at tick 31,860, or 531 seconds.
Pods 037 and 057 are 11.74649 meters apart on Paddington's first arrival and departure links.
Pod 037 is empty and accessing a berth.
Pod 057 carries a passenger and leaves the station.
A shortened replay reproduces the same tick and pair and captures their positions and saved state.

The berth-link-only seed-1 arm fails at tick 32,760, or 546 seconds.
The same pod pair is 11.40091 meters apart.
Both failures precede the first 10,800-second restore checkpoint.
The helper's generic error prefix says "restored continuation safety", but these failures occur in the live simulation.
They do not establish a restore defect.

The study rejects both partial fixtures and stops their remaining seed-2 arms.
Short passing pilots do not qualify those layouts.
The comparisons below use only the four complete primary arms.
No clearance or validation check changes to accommodate the rejected fixtures.

## Complete position-restoration comparisons

All four arms accept 3,599 requests without skips and reach the seven-hour cap.
The table combines realized pickup waits with elapsed ages of requests that still await boarding.
Those mean wait-or-age values are lower bounds on eventual pickup waits.
Late backlog growth measures the change between three and six hours, divided by 180 minutes.

| Seed | Layout | Completed | Aboard | Pending | Mean wait-or-age, seconds | Late backlog growth, requests/min |
| ---: | --- | ---: | ---: | ---: | ---: | ---: |
| 1 | Mirrored | 2,661 | 54 | 884 | 4,965.76 | 3.283 |
| 1 | Restore all eight positions | 2,895 | 50 | 654 | 4,204.04 | 2.839 |
| 2 | Mirrored | 2,758 | 48 | 793 | 4,709.31 | 3.222 |
| 2 | Restore all eight positions | 2,901 | 65 | 633 | 4,094.30 | 2.761 |

Seed 1 completes 234 additional requests, and seed 2 completes 143 additional requests.
The full restoration also reduces wait among requests that board in both arms.
Those matched means decrease by 926.84 and 752.00 seconds.
These paired comparisons retain identical offered and accepted requests, avoiding a skipped-demand explanation for the gains.
Unfinished requests still limit service conclusions.
Positive late backlog growth prevents treating either layout as sustained capacity at this rate.

The largest matched pickup-wait increases are 478.82 and 1,252.52 seconds.
The individual wait limits flag 36 and 54 matched boarded requests.
The journey limits flag 19 and 35 jointly completed requests.
The user approved these [limits](experimental-adoption.md) on October 1.
These selected comparisons do not establish full qualification.
Aggregate improvements do not erase those individual regressions.

## Mechanism boundary and evidence

The full intervention reproduces a Paddington geometry effect on current source.
It does not identify one responsible resource or show that a departure holding lane or independent berth bank would help.
The partial groups cannot supply that evidence because they fail safety checks.
The [selected resource histories](paddington-resource-history.md) retain their separate source boundaries.
An earlier intervention at `7cd5fe9` also moved only the mirrored Paddington positions into the earlier network, which raised average wait by 17.30% and 14.72% in seeds 1 and 2.
This result supplies no basis for weaker reservations or automatic changes to authored geometry.

[Arm totals](measurements/paddington-layout-arms.csv) retain service, backlog, station-resource observations, and check counts.
[Pair totals](measurements/paddington-layout-pairs.csv) retain matched cohorts and individual increases.
[Metadata](measurements/paddington-layout.json) retains fixture hashes, exact baseline checks, rejected witnesses, status transitions, and unfinished-request records.
All owned geometry units stopped, and their scratch binaries were removed.
