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

The network does not yet model tunnel depth.
Therefore, unrelated lines can cross in the drawing without sharing a
junction.
A future geographic safety qualification must distinguish these
grade-separated crossings before it treats every close two-dimensional pod
position as a collision.

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

The generated project has 99 stations, 776 nodes, 1,261 lanes, and 114 pods.
Project validation checks directed reachability between all passenger berths.
