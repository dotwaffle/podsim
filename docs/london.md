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
The Parking facilities connect at Hammersmith (west), Finsbury Park (north),
and Mile End (east).
The initial fleet has 114 pods: one at each passenger station and six at each
Parking facility.

Paddington's two Underground stop records are one PRT station.
Bank and Monument are also one station because they form one interchange
complex.
Other lines connect only where the source uses the same station or where this
normalization explicitly joins an interchange.
A crossing on the two-dimensional drawing does not create a connection.

## Geometry

The checked-in source keeps the TfL latitude and longitude for each station.
The generator uses a local meter projection centered on Charing Cross.
This keeps the distance and density differences across the selected area.
It adds no mapping dependency.

Guideway links are straight between station areas.
Their two directions are offset by 18 meters from the centerline.
Each link has a separate arrival portal and departure portal at each end.
The portals prevent opposite directions from sharing one station node.

Short movement lanes connect each arrival portal to each departure portal.
These lanes let pods continue through the station area, change corridors, or
reverse direction.
Each movement has a separate separation group.
Movements conflict when they share a portal.
Unconnected movement crossings represent grade-separated paths.

Station access branches from every arrival portal and rejoins every departure
portal.
Each station has distinct diverge, entry, exit, and merge nodes.
Berths use separate arrival and departure spines with a 75-meter pitch.
This layout keeps access lanes away from occupied berths.

Each station lane has one maneuver role: approach, entry, berth access, through,
departure, or exit.
At the end of each tick, the simulation sets each pod's station phase from its
berth, its current lane, or its next lane.
The pod inspector shows phases such as `Approaching station`, `Accessing berth`,
and `Departing berth` with the station name.
When a pod has no station phase, the inspector shows `Main network`.
These roles describe the existing movement and reservation flow.
They do not change route selection, admission priority, or resource ownership.

The network does not store tunnel depth.
Instead, each guideway, movement lane, and station path has an explicit
separation group.
Different groups declare that unrelated paths can cross at different physical
levels.
Only the separation oracle uses these groups.
The qualification tests run this oracle.
With `-state`, a `physical` or `logical` restore also checks the restored pods
with it.
The oracle still checks paths that share a junction, even when their groups
differ.
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
This keeps the source station and OD ratios.
It does not claim that the PRT system carries the complete Underground volume.

The portable London project includes the raw OD weights and all eight bands.
Its demand settings select the AM peak band at 20 requests per simulated
minute by default.
This rate is above the AM peak recovery limit of 12 requests per minute in the
[London capacity envelope](qualification.md#london-capacity-envelope).
The shared session samples those weights when automatic demand runs.
The editor can select another band before it applies the project.
State frames contain only the selected profile and band IDs, not the complete
OD matrix.

A deterministic AM peak qualification submits 40 OD-weighted requests at
five-second intervals.
All 40 completed by 1,420.0 simulated seconds.
Average pickup wait was 57.021 seconds, and maximum pickup wait was 394.217
seconds.
At the end of the run, the test also rejects any pod at a station without an
assigned berth.
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

The preset includes no map tiles or background image.
An optional background must use a permitted source, preserve attribution, and
avoid dependence on public tile servers during tests.

## Generate

```sh
mise run scenario -- -preset london -output /tmp/podsim-london.json
mise run serve -- -project /tmp/podsim-london.json
```

The generated project has 99 stations, 1,842 nodes, 3,101 lanes, and 114 pods.
Project validation checks directed reachability between all passenger berths.
The preset starts with automatic demand and redistribution disabled.

The station-count limit of 100 permits one added station in the editor.
The limits of 2,000 nodes and 4,000 lanes also apply to that edit.
The `serve` and `compare` commands read a `-project` file of at most 4 MiB.
The generated file is indented, so it is about 3.3 MiB.
Project validation also limits the compact JSON form of a project to 4 MiB,
with room for the widest demand settings.
In that form, the London project is about 1.5 MiB (1,590,971 bytes).
The server writes the `-project` file in this form when it saves it.
The editor sends about 1.5 MiB when it applies the project, and the server
accepts a command of at most 2 MiB.
Change a limit only after measured editor validation.

The view treats this project as a dense map because it has more than 30
stations.
Until the berths of a station separate on the screen, the station shows one
marker.
The marker of a passenger station is at its junction, at the mean position of
its arrival portals.
This point is within about 60 meters of the TfL station position.
The berths are about 250 to 450 meters away, and their rings replace the
marker when you zoom in.
A Parking marker stays at the center of its berths, so it does not cover the
marker of its gateway station.
The project also has more than 100 lanes, so the map sizes change with the
zoom.
A marker has a radius of 150 meters on the map, but at least 3 and at most 10
CSS pixels.
A lane has a width of 30 meters on the map, but at least 2 and at most 5 CSS
pixels.
A lane shorter than 24 CSS pixels on the screen has no direction arrow.
In a window smaller than 1100 by 760 CSS pixels, these sizes become smaller
with the rest of the view.
While a station shows one marker, its lanes are thinner and dimmer than the
other lanes.
A station lane that connects two lanes outside stations keeps the normal
style.
Because of this, the rings of the `busy` and `rail-hub` presets stay
continuous at each station.
Node dots show only when you zoom in far enough for the berths of a station to
separate.
Networks with 100 lanes or fewer, such as the example, keep each marker at the
center of its berths.
They keep fixed marker and lane sizes, and they show all direction arrows and
node dots at all zoom levels.
On a dense map, the view omits an overview label that overlaps another visible
overview label.
Labels for the `From` and `To` stations and the selected pod's stations stay
visible.
Only the selected pod has a map label until you zoom in to four times the `Fit`
scale.
