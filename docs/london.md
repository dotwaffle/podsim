# London qualification network

The `london` preset maps the central London Underground topology onto a PRT
network.
Pods use the guideways as independent vehicles.
The preset does not simulate Underground trains or service patterns.

## Initial boundary

The preset contains 96 passenger stations and three Parking facilities.
It includes all Underground stations in Zone 1 and Zone 1+2, the first
adjacent stations outside that boundary, and contiguous extensions to selected
radial gateways.
The gateways include Brixton, Canada Water, Finsbury Park, Hammersmith, Mile
End, Shepherd's Bush, and Willesden Green.

The source has 127 unique station adjacencies.
Shared Underground corridors produce one PRT connection, not duplicate
overlapping guideways.
Each connection has two offset, directed guideways.
Each passenger station has two off-line berths.
West, north, and east Parking facilities each have 12 berths.
The initial fleet has 114 pods, including 18 pods in Parking.

Paddington's two Underground stop records are one PRT station.
Bank and Monument are also one station because they form one interchange
complex.
Other lines connect only where the source uses the same station or where this
normalization explicitly joins an interchange.
A crossing on the two-dimensional drawing does not create a connection.

## Geometry

The checked-in source keeps the TfL latitude and longitude for each station.
The generator uses a local meter projection centered on Charing Cross.
This preserves useful distance and density differences across the selected
area without adding a mapping dependency.

Guideway links are straight between station locations in this first version.
Their two directions are offset by 18 meters from the centerline at the link
midpoint.
Station berths are separate from the through junction.
Each station has distinct road, diverge, entry, exit, and merge nodes.
This prevents one junction reservation from consuming the complete terminal
access lane before the controller assigns a berth.
Each station lane has one maneuver role: approach, entry, berth access, through,
departure, or exit.
The simulation snapshot derives each pod's station phase from its current lane.
The pod inspector shows phases such as `Approaching station`, `Accessing berth`,
and `Departing berth` with the station name.
These roles describe the existing movement and reservation flow.
They do not change route selection, admission priority, or resource ownership.

The network does not model tunnel depth.
Instead, each link and station path has an explicit separation group.
Different groups declare that unrelated paths can cross at different physical
levels.
Paths that share a junction remain subject to the separation check, even when
their groups differ.
Projects without separation groups keep the original two-dimensional check.

## Demand

The checked-in demand snapshot is
[`internal/scenarios/data/london-od-2019.csv`](../internal/scenarios/data/london-od-2019.csv).
It comes from TfL's 2019 midweek NUMBAT Underground OD matrix:
<https://crowding.data.tfl.gov.uk/NUMBAT/NUMBAT%202019/NBT19_OD_data/NBT19MTT2b_od__LU_tb_wf.csv>.

The snapshot contains 8,474 directed OD pairs whose endpoints are both in the
modeled network.
It covers 94 of the 96 passenger stations.
Nine Elms and Battersea Power Station have no rows because they opened after
the 2019 source data.

The source defines eight bands:

| Band | Time |
| --- | --- |
| Early | 03:00-05:00 |
| Morning | 05:00-07:00 |
| AM peak | 07:00-10:00 |
| Interpeak | 10:00-16:00 |
| PM peak | 16:00-19:00 |
| Evening | 19:00-22:00 |
| Late | 22:00-00:30 |
| Night | 00:30-03:00 |

`LondonDemand` normalizes the retained OD weights separately for each band.
The caller can apply one scale factor to choose the simulated request rate.
This keeps the source station and OD ratios while avoiding a claim that the
PRT system carries the complete Underground volume.

The portable London project includes the raw OD weights and all eight bands.
Its demand settings select the AM peak profile by default.
The shared session samples those weights when automatic demand runs.
The editor can select another band before it applies the project.
The recurring simulation state contains only the selected profile and band
IDs, not the complete OD matrix.

A deterministic AM peak qualification submits 40 source-weighted requests at
five-second intervals.
All 40 completed by 1,083.0 simulated seconds.
Average pickup wait was 58.744 seconds, and maximum pickup wait was 235.133
seconds.
The test also rejects any stationary pod without an assigned berth.
It runs the geometric separation oracle once per simulated second.
The oracle skips only pod pairs in distinct separation groups that do not
share a junction.

## Sources

The normalized topology snapshot is
[`internal/scenarios/data/london-tube.json`](../internal/scenarios/data/london-tube.json).
It was captured on 2026-09-22 from TfL Unified API line route-sequence
responses.
TfL describes these responses as line branches, ordered stops, station
locations, and interchange information:

- <https://tfl.gov.uk/info-for/open-data-users/unified-api>
- <https://api.tfl.gov.uk/line/central/route/sequence/outbound>

Data provided by Transport for London.
Use of the source data remains subject to the TfL transport data terms:
<https://tfl.gov.uk/corporate/terms-and-conditions/transport-data-service>.

No map tiles or background image are included.
An optional background must use a permitted source, preserve attribution, and
avoid dependence on public tile servers during tests.

## Generate

```sh
mise run scenario -- -preset london -output /tmp/podsim-london.json
mise run serve -- -project /tmp/podsim-london.json
```

The generated project has 99 stations, 974 nodes, 1,459 lanes, and 114 pods.
Project validation checks directed reachability between all passenger berths.
The supported project limit is 100 stations, so this preset permits one added
station in the editor. Change the limit only with measured editor validation.
The preset starts with automatic demand and redistribution disabled.
