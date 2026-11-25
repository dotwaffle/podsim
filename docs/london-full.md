# LondonFull

LondonFull (`london-full`) maps 269 normalized Tube sites and 40 additional DLR sites onto a PRT network.
It includes the full DLR except West India Quay, omitted for layout clearance.
It does not include Overground.
It has three additional Parking facilities, each a depot, and eight Compact stations.
Pods use independent guideways, not train schedules or Tube service frequencies.
[LondonCentral](london.md) retains the smaller qualification network and its 2019 demand.

Generate a project with:

```sh
mise run scenario -- -preset london-full -output /tmp/podsim-london-full.json
```

The generator uses compact JSON when indented output would exceed the project file limit.
The output can then be supplied to `serve -project` or opened in the editor.
Its display name is `LondonFull`.

## Source and demand

The topology contains 272 Tube source stops and 45 DLR source stops.
After merges and the West India Quay omission, it has 309 passenger sites and 358 undirected links.
Monument merges into Bank, Paddington H&C into Paddington, and Hammersmith H&C into Hammersmith.
Bank, Canning Town, Stratford, and West Ham each merge their Tube and DLR stops.
The two Edgware Road sites remain separate.
Canary Wharf DLR remains separate from Canary Wharf Jubilee.
Coordinates come from the TfL Unified API, including Nine Elms and Battersea Power Station.

Demand contains 76,567 directed pairs from the 2024 Tuesday-to-Thursday network matrix.
Both endpoints must map to a retained Tube or DLR site.
Those journeys can use other modes between their endpoints.
This differs from LondonCentral's 2019 LU-specific matrix.
The full preset offers Morning, AM peak, Interpeak, PM peak, Evening, and Late.
The source has no Early or Night demand, so those selections are absent.

The [offline converter](../tools/londonfull/README.md) documents all source hashes, aliases, exclusions, and exact totals.
It preserves all eight source columns, including the two empty columns.
It does not truncate pairs or synthesize weights.
The preset rounds each weight to four significant digits when it reads the matrix.
The converter and provenance retain exact totals before rounding.

## Capacity and geometry

The default has 327 pods, 770 berths, 5,740 nodes, and 9,024 lanes.
Each passenger site starts with one pod and two berths.
A Compact station has four bays in place of its berths.
The three existing Parking facilities are depots, each with six pods and 12 slots.
See [Depots and Compact stations](#depots-and-compact-stations).

The generator assigns 100 extra passenger berths from the largest boarding-plus-alighting share of each site across the six observed bands.
It uses the largest-remainder method, with station ID as the tie-breaker.
Bank keeps two berths, because more rows cause a hard layout conflict with wide stations at station scale 1.
Its two extra berths are reassigned by the same allocation rule.
Bank, Mansion House, and Canary Wharf Jubilee use full-network headings of 234, 56, and 330 degrees.
The Canary Wharf heading clears the North Greenwich guideway.
West India Quay leaves insufficient clearance beside Canary Wharf Jubilee and the Poplar delta.
Its guideways connect through Canary Wharf DLR, and its demand endpoints are excluded.
Poplar uses the shared heading search without an override.
LondonCentral's headings and capacity remain unchanged.

Two lanes that merge or split at a shallow angle form a [lateral merge pair](model.md#junctions) with shorter junction zones.
The [shape clearance](reference.md#shape-clearance) rule gives these pairs to each network.

The layout audit reports no hard conflicts and no soft conflicts.
Soft conflicts describe overlap or proximity to source station positions.
They do not establish that a running simulation is safe or that a demand rate is sustainable.

An explicit `-station-berths` value replaces the weighted allocation with a uniform base.
A `-berths ID=N` override changes that site and preserves the remaining defaults.
When both flags are present, the site override applies over the uniform base.
All generated capacity variants must pass normal project and layout validation.

### Station shape

Each passenger station that is not a Compact station is tight.
The diverge and the merge are 80 meters from the junction and 30 meters to each side of the station axis.
The arrival and departure lanes are 25 meters to each side of the axis, so the through lane is 50 meters long.
The entry is 25 meters past the throat, so the lane from the diverge to the entry is 25.5 meters long.
The first berth row is 25 meters past the entry, and the berth pitch is 25 meters.
Each of these parts is equal to the 25-meter minimum berth pitch of the generator.
A Compact station keeps the shape that the station scale sets, and the Parking facilities keep their shape.
The maintainer made the tight shape the default on November 16, 2026, after the [tight station study](studies/station-scale.md#tight-stations).

`-wide-stations` gives each such station the shape that `-station-scale` and `-berth-pitch` set.
The network SHA-256 is the `network_sha256` value of the summary line.

| Flags | Shape | Network SHA-256 | Soft conflicts |
| --- | --- | --- | ---: |
| none, the default | Tight | `03d04786f72780fe4e0e1a65ee90eefcb8e20fc78ddd0c2b0600556cbea79ec5` | 0 |
| `-wide-stations` | Station scale 0.5, the default before November 16, 2026 | `413119c9bd469ccc50ab755683a6bcc1f27eb78ac5d6b1105b142a482f48b9d3` | 1 |
| `-berth-pitch 50` | Tight, the flags of the study | `9503e65cd30cbe9b861552ec9d96e76b939b0be9e744b7ff85105ddd9ea2e0ec` | 0 |

The hashes are for the build of November 20, 2026, when LondonFull began to fillet its station roads (see [Station roads](#station-roads)).
The network of `london-full-express` did not change.

A tight station keeps its 25-meter pitch, so `-berth-pitch` sets only the wide stations, the Compact stations in the heading search, and the Parking facilities.
The study network differs from the default only at two of the three Parking facilities, `parking-east` and `parking-west`, because a pitch of 50 meters changes their shape.
Their Parking roads also change, because the fillets follow the new headings of their access lanes.
`-station-scale` sets only the wide stations and the Compact stations.
`london-full-express` keeps wide stations at station scale 1, and its network SHA-256 stays `3aa0a2eb9e99eab4ea80ecfad481c7718b57cd38e0b35876b44b5ab5dc1d22e5`.

### Station roads

Since November 20, 2026, LondonFull fillets its station roads, by the [London road fillets proposal](contracts/london-fillets-proposal.md).
A road is a lane `<station>-road-in-NN` or `<station>-road-out-NN` of a passenger station or of a Parking facility.
A road-in lane goes from an arrival portal to the diverge node of its station, and a road-out lane goes from the merge node to a departure portal.
The rule gives a road a quadratic curve that leaves the lane before it at its heading and joins the lane after it at its heading.
The speed limit of a curved road comes from its smallest radius r: it is the smaller of 14 m/s and the square root of 2r, with r in meters.
This is the ramp speed of the [London junction](design/london-junctions.md) design.
A road keeps its chord and 14 m/s when an end is already tangent, when the two headings do not meet ahead of both ends, or when its speed would be below 5 m/s.

Of the 1,456 roads of LondonFull, 676 are curved: 320 road-in lanes, 340 road-out lanes, and 16 Parking roads.
The median speed of a curved road is about 11 m/s.
280 of the 309 passenger stations have a road slower than 14 m/s.
The movement lanes at the junctions keep their chords.
LondonCentral and `london-full-express` keep unfilleted roads.
`london-full-shared-express` builds on the filleted LondonFull, with straight roads at Waterloo and Bank.
`-road-fillets=false` gives LondonFull with straight roads, with the network SHA-256 `69baad6a8077b3f22c1e250023b4eaeefba10f1ff9bb0812b773634951d84225` (see [capacity flags](reference.md#capacity-flags)).

A virtual platoon needs one speed limit on its route and on each lane of its destination station (see [virtual platoons](model.md#virtual-platoons)).
So few platoons form on LondonFull.
In 60-minute runs at 15 requests per minute, LondonFull with straight roads formed 10, 7, and 4 platoon links on seeds 1, 2, and 3, and the filleted LondonFull formed none.

The maintainer chose the ramp speed after a compare study at 15 requests per minute, with seeds 1 to 6.
Against the straight roads, the filleted roads drain 6 seeds against 5, with a mean wait 2.83% longer and a mean journey 1.50% longer.
The time that pods wait for junction traffic is about 2.1 times longer.
Section 8.1 of the proposal gives the method and the results.

### Shared Express preset

`london-full-shared-express` is decision D11 of the [shape clearance](design/shape-clearance.md) design.
It runs Express on the link 100 track between Waterloo and Bank, shared with Compact pods, in place of the dedicated trunks of `london-full-express`.
It is a builder over LondonFull: it changes the two hubs and adds the Express fleet, and the generator, LondonFull, and `london-full-express` do not change.
It keeps the filleted roads of LondonFull, except the roads of Waterloo and Bank, which keep their chords and a speed limit of 14 m/s (decision FL-D5 of the fillets proposal).
The [shared-track design](design/shared-track-preset.md) gives the measurements and the build record.

```sh
mise run scenario -- -preset london-full-shared-express -output /tmp/podsim-london-full-shared-express.json
```

| | LondonFull | `london-full-express` | `london-full-shared-express` |
| --- | --- | --- | --- |
| Stations | Tight | Wide, scale 1, two banks at each hub | Tight, with parts of 41 m at Waterloo and Bank |
| Express track | None | Dedicated trunks, gate stubs, and a bend at Bank | Link 100, shared with Compact pods |
| Express parking | None | `express-parking`, 6.8 km from Waterloo | `express-parking`, a comb beside Waterloo |
| Junction lanes | All chords | All chords | Two chords removed |
| Station roads | Filleted | Straight | Filleted, with straight hub roads and a curve on `940GZZLUWLO-road-in-05` |
| Network SHA-256 | `03d04786...` | `3aa0a2eb...` | `cba69149...` |

The hub geometry changes are a Waterloo throat at 90 m, Bank turned by 5 degrees, a curve on `940GZZLUWLO-road-in-05`, and the removal of `940GZZLUWLO-move-05-01` and `940GZZLUBNK-move-08-06`.
Without them, the bank audit refuses 7 pairs on the Waterloo and Bank throats, and 33 hub lanes are under the 40 m of a large lane.
On the straight roads before the fillets, Express free-flow routes grow by about 45 m from Waterloo to Bank and 48 m back, and 1,249 station pairs get a longer route.
The worst of them is 21.4 s longer, from Waterloo to St. Paul's.
Wait and journey times are not measured.

The preset has 5,790 nodes, 9,089 lanes, and 343 pods: the 327 Compact pods of LondonFull and 16 Express pods.
The comb has 16 berths, parts of 41 m, and two roads to the link 100 portals at Waterloo.
The layout audit of the preset treats the comb as a Parking facility of Waterloo, because its road crosses the curved `road-in-05`.
The preset has one tail warning, for `940GZZLUBNK-move-08-02`, whose tail of 39.5 m is longer than its length of 39.1 m, so it holds one pod.
The capacity flags do not apply to it.
Its network SHA-256 is `cba69149281760dbba334323497b9d94cb1d3a980a631b68de0be45e78aac6bb`.

### Shared Express Wharf preset

`london-full-shared-express-wharf` is the first pair of the [Express London hubs](design/london-hubs-d8-d9.md) design.
It is `london-full-shared-express` with a second Express pair on shared track, London Bridge and Canary Wharf, and a comb beside London Bridge.
The two pairs share no Express route.
The changes of the first pair do not change.

```sh
mise run scenario -- -preset london-full-shared-express-wharf -output /tmp/podsim-london-full-shared-express-wharf.json
```

| | `london-full-shared-express` | `london-full-shared-express-wharf` |
| --- | --- | --- |
| Express pairs | Waterloo and Bank | Waterloo and Bank, and London Bridge and Canary Wharf |
| Shared track | Link 100 | Links 100, 089, 088, and 154 |
| Hubs with 41 m parts and straight roads | Waterloo, Bank | Also London Bridge and Canary Wharf |
| Curved hub roads | `940GZZLUWLO-road-in-05` | Also `940GZZLULNB-road-out-01`, by +30 m |
| Express parking | `express-parking`, beside Waterloo | Also `express-parking-lnb`, beside London Bridge |
| Express services | 2 | 4 |
| Pods | 327 Compact, 16 Express | 327 Compact, 32 Express |
| Nodes and lanes | 5,790 and 9,089 | 5,840 and 9,156 |
| Shape pairs | 47 | 53 |
| Network SHA-256 | `cba69149...` | `d68a69b8...` |

The curve removes the one refusal of the bank audit, between `940GZZLULNB-road-out-01` and link 089.
The second comb has 16 berths and parts of 41 m, like the first, and no exemption in the layout audit.
The preset has the one tail warning of `london-full-shared-express`.
The capacity flags do not apply to it.
Its network SHA-256 is `d68a69b8c326f292f7c3a7839b51bf9837c5e714287f60470436781705217359`.
The preset is opt-in and is not qualified: the [Express wharf qualification](contracts/express-wharf-qualification-proposal.md) gives the rows and their status.

### Shared Express Wharf Hubs preset

`london-full-shared-express-wharf-hubs` is decision HB-D19 C of the [Express London hubs](design/london-hubs-d8-d9.md) design (section 13).
It is `london-full-shared-express-wharf` with Express services between London Bridge and Waterloo, and between Waterloo and Canary Wharf.
It changes no geometry, because the three hubs have their Express shape already.

```sh
mise run scenario -- -preset london-full-shared-express-wharf-hubs -output /tmp/podsim-london-full-shared-express-wharf-hubs.json
```

| | `london-full-shared-express-wharf` | `london-full-shared-express-wharf-hubs` |
| --- | --- | --- |
| Express pairs | Waterloo and Bank, and London Bridge and Canary Wharf | Also London Bridge and Waterloo, and Waterloo and Canary Wharf |
| Shared track | Links 100, 089, 088, and 154 | Also links 280 and 350 |
| Express services | 4 | 8 |
| Express networks | 2, one for each comb | 1 |
| Pods | 327 Compact, 32 Express | The same |
| Nodes and lanes | 5,840 and 9,156 | The same |
| Shape pairs | 53 | 67 |
| Network SHA-256 | `d68a69b8...` | `fa153ec8...` |

London Bridge to Waterloo is 1,991 m by chord, and Waterloo to London Bridge is 2,055 m.
The route between Waterloo and Canary Wharf passes through London Bridge on `940GZZLULNB-move-04-01` and `940GZZLULNB-move-01-04`.
The free-flow cost of each station route does not change.
Decision HB-D20 A keeps `park`, so an idle Express pod goes to `express-parking`, the first Express parking in network order, also from London Bridge.
Decision HB-D21 A keeps the 16 and 16 Express pods.
The preset has two tail warnings: the one of `london-full-shared-express`, and `940GZZLULNB-move-04-03`, whose tail of 66.5 m is longer than its length of 66.2 m.
The capacity flags do not apply to it.
Its network SHA-256 is `fa153ec8f8c85e98d8e056155e8915240ccca405efa62d0e5fad22909f660196`.
The preset is opt-in and is not qualified, and no qualification is proposed for it.

## Depots and Compact stations

Both London presets have depots and Compact stations by default.
Two flags of the scenario command change these layouts on `london-full` and `london-central`.
A layout other than the default gives the project name the suffix " (custom capacity)", also when the berth counts stay the same.
These opt-out flags give the presets with ordinary Parking facilities and ordinary stations:

```sh
mise run scenario -- -preset london-full -parking-lanes 0 -compact-stations none \
  -output /tmp/podsim-london-full-ordinary.json
```

Before October 19, 2026, this layout was the default.
These flags keep the tight passenger stations.
Add `-wide-stations` for the [wide stations](#station-shape) of the station scale.

### Depots

Each of the three Parking facilities is a [depot](model.md#parking-depots) with 2 queue lanes of 6 slots.
`-parking-lanes N` sets the lanes of each depot.
`-parking-berths` sets the slots of each lane, and `-berths parking-north=M` sets the slots of each lane of one facility.
`-parking-lanes 0` gives ordinary Parking facilities with 12 berths each.
With `-parking-lanes 0`, `-parking-berths` and `-berths parking-north=M` set the berths of the ordinary facilities.
The depot lies outward from the through lane of the facility.
Its entry and exit are 200 meters from the junction along the station axis, and half the depot length to each side of the axis.
The heading search uses the shape of an ordinary Parking facility with 12 berths, so the other stations keep their headings.

A London depot is at most 200 meters long.
The command refuses a longer depot and names the facility.
This table gives the most slots in each lane of a depot:

| Lanes | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Slots | 16 | 15 | 15 | 13 | 12 | 11 | 10 | 9 |

### Compact stations

The default Compact stations of LondonFull are eight zone 1 stations with 2 berths and low demand: Regent's Park, Lambeth North, Lancaster Gate, Edgware Road (Bakerloo), Bayswater, Hyde Park Corner, Borough, and Tower Gateway.
LondonCentral has the first seven and not Tower Gateway.
Each Compact station has the four [Compact bays](model.md#compact-bays) of the `compact-station` preset.
`-compact-stations ID,ID` replaces the list with at most 8 passenger stations, and `-compact-stations none` gives no Compact stations.
The command refuses an unknown ID, a Parking facility, a repeated ID, more than 8 IDs, and a `-berths` item for a Compact station, also when the item has the preset value.
For a default Compact station, a `-berths` item needs `-compact-stations none` or a list without the station.
A Compact station has 4 berths and at most 4 initial pods.
Its berth count in the capacity options still sets its shape in the heading search, so the other stations keep their headings.
The compact layout replaces the through lane as a rigid body.
Its aisle is at the entry depth of the station: 140 meters out along the station axis at the preset station scale of 0.5, and 200 meters at station scale 1.
Its bays face outward, and its bypass curves toward the station throat.
With a Compact station, each pod of the project is Compact, because only a Compact pod can use the bays.

One visit to a bay holds the aisle for about 30 to 43 seconds, so a Compact station serves about 1.4 to 2 visits per minute.
Each trip end is about one visit.
Use a Compact station only where the demand share of the station times the request rate is below about 0.7 trip ends per minute.
The share is the largest share of the station across the demand bands.
In each band, the shares of all stations add to 2, but the largest shares of all stations add to about 2.75.

The default Compact stations have shares from 0.0025 to 0.0041.
At 120 requests per minute, they have 0.29 to 0.49 trip ends per minute.
With the opt-out flags, LondonFull has 5,658 nodes, 8,844 lanes, and no soft conflicts, and its indented export has 14,901,130 bytes.
With `-wide-stations` also, it has 1 soft conflict and 14,980,120 bytes.

### Measurements, October 19, 2026

The candidates are LondonFull projects from the scenario command.
The network SHA-256 is the `network_sha256` value of its summary line.
The runs were before the Both arm became the default.
The defaults changed after these runs, so the flags in the table do not give these networks now.

| Arm | Flags | Network SHA-256 |
| --- | --- | --- |
| Ordinary | `-parking-lanes 0 -compact-stations none` | `4c65870bff2d41c1b849d6b11b2c6bfb3b0a6cb4c8e686136221f137c2bab8f6` |
| Depots | `-compact-stations none` | `e44e6d07c57daea8e7f15d7f8829e792f95b3ce73167cd92ff270b92e3806ba6` |
| Compact stations | `-parking-lanes 0` | `f287ee6a0bdddc0431a7c6b55de268469f28506e851403890e3ab1c88204fc93` |
| Both | none, the default | `4a7efa573c6af08d96de0ceeb60287176422f2bf93bc4ea2e5e2a19a21f83374` |

Two more arms set `"class": "compact"` on each fleet placement of the Ordinary and Depots projects, with no other change.
They give the same results as Ordinary and Depots in every run below, so the tables leave them out.

The smoke runs each arm with a fixed 30-minute window:

```sh
mise run compare -- -project ARM.json -seed 1 -arrivals-for 20m -duration 30m -loads 6s,1s \
  -pattern profile -redistribution-policies off -platoon-policies virtual
```

Each arm gets the same request schedule.
For seed 1, the schedule ID is `2f47adff4fa5d2f7` at 10 requests per minute (199 requests) and `d0e1f87e74785b19` at 60 requests per minute (1,199 requests).
For seeds 2 and 3 at 60 requests per minute, the schedule IDs are `a38852cc1e08ae5a` and `fa529e7ef5e9bc92`.
Without `-platoon-policies virtual`, the Ordinary arm gives 360 served, 607 skipped, a 470.5 s mean wait, and 845 stopped pod-seconds at 60 requests per minute.

To divide the stopped pod-seconds, a local overlay of the compare command sorted the samples that make up `stopped_pod_seconds`.
The compare command takes a sample at each whole simulated second.
Each pod that has a wait reason and a speed below 0.1 m/s gives one stopped pod-second.
The overlay gives the sample to the maneuver station of the pod, or else to the station of its lane, or else, at a berth, to the station of the pod.
A sample at a Parking facility is in the Parking area, which is the depot when the facility is a depot.
A sample at a passenger station is in the station area, and a sample with no station is on the guideway.
The overlay also records the wait reason and the activity of the pod that `BlockedBy` names.

Seed 1, per run:

| Arm | Load | Served | Skipped | Wait mean (s) | Stopped pod-s | Parking | Guideway | Stations |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Ordinary | 10/min | 152 | 0 | 55.4 | 42 | | | |
| Depots | 10/min | 152 | 0 | 55.9 | 43 | | | |
| Compact stations | 10/min | 152 | 0 | 55.9 | 42 | | | |
| Both | 10/min | 152 | 0 | 56.4 | 43 | | | |
| Ordinary | 60/min | 363 | 602 | 464.9 | 954 | 33 | 816 | 105 |
| Depots | 60/min | 359 | 610 | 472.2 | 2,107 | 1,258 | 749 | 100 |
| Compact stations | 60/min | 364 | 599 | 461.0 | 941 | 23 | 835 | 83 |
| Both | 60/min | 364 | 607 | 464.1 | 2,273 | 1,268 | 828 | 177 |

Seeds 2 and 3 at 60 requests per minute, per run, not summed:

| Seed | Arm | Served | Skipped | Wait mean (s) | Stopped pod-s | Parking | Guideway | Stations |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 2 | Ordinary | 365 | 615 | 465.5 | 876 | 11 | 754 | 111 |
| 2 | Depots | 369 | 616 | 467.3 | 1,580 | 704 | 768 | 108 |
| 2 | Compact stations | 363 | 611 | 467.6 | 975 | 11 | 791 | 173 |
| 2 | Both | 365 | 612 | 466.9 | 1,771 | 704 | 929 | 138 |
| 3 | Ordinary | 345 | 626 | 477.6 | 1,126 | 39 | 933 | 154 |
| 3 | Depots | 346 | 629 | 483.6 | 1,591 | 626 | 830 | 135 |
| 3 | Compact stations | 354 | 618 | 472.1 | 1,462 | 39 | 1,160 | 263 |
| 3 | Both | 349 | 623 | 478.0 | 1,986 | 626 | 1,164 | 196 |

The depots more than double the stopped time at 60 requests per minute, and the time is in the Parking areas.
With seed 1, the depot interiors have 1,258 stopped pod-seconds against 33 at the ordinary facilities.
These are empty pods in the depots.
They wait for a slot whose pod moves up or starts to leave (359 s: 276 s behind a pod that departs empty from its slot, 83 s behind a pod that travels), for junction traffic of another pod in the same depot (569 s against 29 s), and for a pod ahead (330 s against 4 s).
With seeds 2 and 3, the depot interiors add 693 and 587 pod-seconds.
The guideway and station times change in both directions from seed to seed and arm to arm.
In the Compact stations and Both runs at 60 requests per minute, the eight Compact stations have no stopped time.

A separate service screen uses seeds 1 to 3, 60 minutes of arrivals, and a window that ends when the arm drains or at 120 minutes:

```sh
mise run compare -- -project ARM.json -seeds 1,2,3 -arrivals-for 60m -duration 120m -stop-when-drained -loads 6s,4s \
  -pattern profile -redistribution-policies off -platoon-policies virtual -workers 3
```

The schedule IDs are `f8b0e224cb7ab4ee`, `6cbd0c4c6bd1b023`, and `d766c53f0dec233d` at 10 requests per minute, and `a752dc5ce2ced72e`, `d3120e74bb38bfc0`, and `06e57a6e6dc09108` at 15 requests per minute.
The windows of an arm can end up to 11 s apart from those of another arm.
The stopped pod-seconds are the sum of the three seeds, and the wait is the mean of the three seed means.

| Arm | Load | Served | Drained | Wait mean (s) | Stopped pod-s | Parking |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| Ordinary | 10/min | 1,797 | 3 of 3 | 156.7 | 1,416 | 0 |
| Depots | 10/min | 1,797 | 3 of 3 | 158.0 | 3,064 | 1,737 |
| Compact stations | 10/min | 1,797 | 3 of 3 | 156.9 | 1,726 | |
| Both | 10/min | 1,797 | 3 of 3 | 157.6 | 3,033 | |
| Ordinary | 15/min | 2,695 | 1 of 3 | 242.6 | 3,274 | 10 |
| Depots | 15/min | 2,695 | 1 of 3 | 243.5 | 4,937 | 1,919 |
| Compact stations | 15/min | 2,695 | 1 of 3 | 243.1 | 3,034 | |
| Both | 15/min | 2,695 | 1 of 3 | 242.9 | 4,431 | |

At 15 requests per minute, 2 requests remain in each arm.
These runs do not establish a sustainable demand rate.

## Validation boundary

The initial demand setting is 10 requests per minute, with AM peak weights and seed 20260929.
Automatic demand starts disabled, and redistribution and pickup reassignment start on.
A ten-minute AM peak smoke run on October 7, 2026, with ordinary Parking facilities and stations, generated 100 requests and completed 15, with no skipped requests.
Safety checks passed each simulated second.
The physical restore test preserved active requests and passed safety checks during its 30-second continuation.
A separate test checks that simultaneous trips around Poplar and both Canary Wharf sites drain safely.
These checks do not establish a sustainable demand rate.

The project limits admit 12,000 nodes, 20,000 lanes, 600 stations, 1,000 pods, 400,000 flows, and 32 MiB of project JSON.
Express projects retain their separate 600-pod limit.
The default compact project JSON has 5,661,974 bytes.
The default indented export has 14,991,173 bytes.
The saved-state cap is 128 MiB.
The plain and Express saved-state and full-stream byte proofs include completed history and the independent project and topology caps.
Stream JSON is capped at 64 MiB, with 32,832 bytes of gzip overhead; HTTP state has a separate 128 MiB cap.
The junction-pair limit is 250,000, and the track-cell limit remains 64,000.

### Matched fleet qualification

`mise run qualify:london` runs 327, 600, and 1,000 pods on one capacity variant, at 10 and 120 requests per minute.
The variant has 6,463 nodes, 9,968 lanes, 312 stations, and 1,045 berths.
Passenger sites have at least three berths except Bank, which keeps two.
The eight Compact stations of the default have four bays each, and each pod is Compact.
Each parking site is a depot of 4 lanes of 8 slots.
The variant has station scale 1 and a berth pitch of 37.5 m, which set its Compact stations and its Parking facilities.
The other passenger stations are tight, as in the preset.
The default project does not change.
These qualification cases turn redistribution and pickup reassignment off.
Smaller fleets sample the same 1,000 starting placements evenly.

Each case warms up for 600 simulated seconds, measures 180 seconds of ticks, and checks safety and simulation contracts each second.
It exercises production full and delta publications, gzip inflation, client validation and assembly, HTTP state, and physical restore.
It then checks another 60 seconds of operation and requires pods to move after restore.
Mean and p95 tick times must fit the 33.33 ms native 1x budget.
The test also records the p50, p99, and largest tick.
The p99 tick depends on the load of the host, and the client smooths a late tick.
The task is opt-in because all six traffic cases take several minutes.
The overloaded cases fill the 200-order session queue; passing them does not establish a sustainable arrival rate.
The Rate button of the client goes up to 60 requests per minute, so only a project file sets 120.

One run of all six cases on Go 1.27.2, on October 19, 2026, at 60 Hz, passed without restore demotions.
The network SHA-256 of the variant is `d17ae5c5a2c7743e07a572b8402190c8caab33ed2653dad7d8cd30276514edeb`.

| Pods | Requests/minute | Mean tick, ms | p50 tick, ms | p95 tick, ms | p99 tick, ms | Pods moving after restore |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 327 | 10 | 0.087 | 0.072 | 0.112 | 0.157 | 110 |
| 327 | 120 | 0.346 | 0.298 | 0.466 | 0.758 | 327 |
| 600 | 10 | 0.094 | 0.081 | 0.126 | 0.165 | 109 |
| 600 | 120 | 0.500 | 0.374 | 0.694 | 3.770 | 573 |
| 1,000 | 10 | 0.147 | 0.126 | 0.202 | 0.286 | 129 |
| 1,000 | 120 | 0.931 | 0.747 | 1.248 | 5.928 | 809 |

The variant starts from the preset options, so it has had lateral merge pairs since November 15, 2026.
One run of all six cases on Go 1.27.4, on November 15, 2026, at 30 Hz, passed without restore demotions.
The network SHA-256 of the variant is `db3816c9353321ca1ab0af9b22f0f9a3c7a4ec00c730cf59557a86afe7763e68`.

| Pods | Requests/minute | Mean tick, ms | p50 tick, ms | p95 tick, ms | p99 tick, ms | Pods moving after restore |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 327 | 10 | 0.066 | 0.058 | 0.073 | 0.162 | 111 |
| 327 | 120 | 0.259 | 0.228 | 0.332 | 0.708 | 327 |
| 600 | 10 | 0.086 | 0.079 | 0.094 | 0.202 | 110 |
| 600 | 120 | 0.462 | 0.337 | 1.520 | 2.927 | 573 |
| 1,000 | 10 | 0.130 | 0.120 | 0.140 | 0.265 | 130 |
| 1,000 | 120 | 0.809 | 0.566 | 2.965 | 5.212 | 825 |

The variant starts from the preset options, so it has tight stations since November 16, 2026.
The two runs above are for the wide stations of the earlier preset.
One run of all six cases on Go 1.27.4, on November 16, 2026, at 30 Hz, at `212c1b68`, passed without restore demotions.
The network SHA-256 of the variant with tight stations is `1d65b3d547b3656355f3d37767b3e1181c557e146ccafe29debad731d398ff36`.

| Pods | Requests/minute | Mean tick, ms | p50 tick, ms | p95 tick, ms | p99 tick, ms | Pods moving after restore |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 327 | 10 | 0.064 | 0.058 | 0.072 | 0.153 | 106 |
| 327 | 120 | 0.245 | 0.232 | 0.287 | 0.536 | 327 |
| 600 | 10 | 0.088 | 0.080 | 0.096 | 0.209 | 109 |
| 600 | 120 | 0.509 | 0.364 | 1.730 | 3.342 | 578 |
| 1,000 | 10 | 0.130 | 0.124 | 0.149 | 0.318 | 131 |
| 1,000 | 120 | 0.977 | 0.613 | 4.012 | 6.673 | 827 |

The variant starts from the preset options, so it has filleted station roads since November 20, 2026.
`mise run qualify:london` passed on the filleted roads at `1188c43e`.
The numbers above come from the earlier straight roads.

Before two dispatch changes of the same day, the overloaded 1,000-pod case had a p99 of 15 to 20 ms on this variant and on the earlier variant with ordinary stations and 32-berth Parking sites.
At 60 Hz, its slowest ticks fell each 30 ticks, which is each half second, when orders arrive.
Then dispatch checks again many trips that wait for a busy pod, and each check estimated the finish of each pod of the fleet.
The Compact bays make each estimate slower, because a class restriction turns off the shortcut to the first berth of the station.
A Compact fleet on the ordinary layout gave the same tick times and the same service as the default fleet, so the cost came from the restriction and not from the smaller pods.
Dispatch now keeps the estimate of each pod for one dispatch pass, and it checks the fit of a request once for each vehicle class.
The saved state, the snapshot, and the safety observation stay identical at each 10 s of a 780 s run.
Three interleaved runs of the overloaded case gave a p99 of 5.1 to 6.8 ms after the changes, against 15.7 to 19.7 ms before.

### Earlier Tube-only checks

The remaining measurements on this page used the 269-site Tube-only preset.
They do not qualify the DLR network.

Bounded AM peak trials used 20 minutes of arrivals and three seeds.
All requests drained at 10 requests per minute, within 64 simulated minutes.
Mean pickup waits ranged from 63 to 84 seconds.
Higher rates increased waiting times in the sampled runs.
These finite-arrival trials do not establish a sustainable rate.

A later six-hour sustained-load screen finds growing AM peak backlogs at 15 and 20 requests per minute.
Station approach buffers, since removed, did not resolve that overload.
The [mirrored layout study](studies/station-mirror-load.md) also finds growing backlogs at 15 and 20 requests per minute.

A separate one-hour run checked safety each second and completed all 199 requests.
A physical restore at 20 minutes preserved the 287-pod fleet and its pending requests.
The restored continuation passed safety checks on every tick for 30 seconds.
Chromium 143 loaded the active saved session in three Go/WASM clients at 0, 300, and 600 ms simulated RTT.
Each client kept one full baseline and acknowledged subsequent deltas without HTTP state polling.
The editor imported and exported all 60,996 demand pairs and applied the full project through a gzip command.
The exported file was 8,540,140 bytes.
These checks used software rendering.
They do not qualify physical GPUs, Firefox, Safari, or a Fly deployment.

At 10 requests per minute, uniform two-berth stations and zero reserve pods did not worsen the seed-1 completion result.
Increasing each Parking reserve from six to ten pods did not improve it.
The preset retains demand-weighted berth space and the existing six-pod Parking reserves.
These trials do not prove a performance benefit from that extra capacity.
These initial checks preceded the finite-arrival study below.
They do not establish a complete qualification envelope.

## Finite-arrival study, September 29, 2026

The measurements below precede the parking diversion fix.
The [308-arm post-fix study](studies/london-full-postfix.md) repeats this matrix and its conditional selection rules.

The study used 60 minutes of arrivals and at most 60 additional minutes to complete them.
A recovery pass means all scheduled requests completed by minute 120, with no skipped demand or sampled safety/accounting failure.
This finite recovery test does not establish indefinitely sustainable demand.
A run can pass its safety assertions and still fail to recover.

The baseline used the unchanged 287-pod preset, with sharing and redistribution off, free-flow routing, and virtual platooning.
The source and study binary were frozen at `677e62b`.
Later dependency and client changes through `04af058` did not change the simulation, scenario generator, or comparison command.
All six demand bands used nominal rates 5, 10, 15, 20, 30, and 40 requests/minute with seeds 1 through 3.
Morning also used rates 1 and 2 because none of its initial rates recovered in all three seeds.
The selected lower and next higher tested rates then used seeds 4 through 10.
The baseline contains 198 distinct arms: 108 initial, 6 edge, and 84 confirmation arms.

Each schedule has `60 * rate - 1` requests, so its actual offered rate is nominal rate minus `1/60`.
The same band/rate/seed uses the same request times and OD pairs across configurations.
The archived 198 gzip rosters contain 189,582 requests.
Independent decoding reproduced each schedule ID and verified every arrival tick and positive-weight OD pair.

| Band | Nominal rate | Recovered, seeds 1-10 | Late throughput | Backlog change | Max unfinished |
| --- | ---: | ---: | ---: | ---: | ---: |
| Morning | 2 | 10/10 | 1.67 to 2.13 | -4 to 10 | 0 |
| Morning | 5 | 8/10 | 4.33 to 4.93 | 2 to 20 | 2 |
| AM peak | 10 | 9/10 | 8.77 to 9.67 | 10 to 37 | 2 |
| AM peak | 15 | 8/10 | 12.80 to 14.33 | 20 to 66 | 3 |
| Interpeak | 15 | 9/10 | 13.90 to 15.37 | -11 to 33 | 1 |
| Interpeak | 20 | 0/10 | 15.43 to 17.23 | 83 to 137 | 6 |
| PM peak | 10 | 10/10 | 9.17 to 10.13 | -4 to 25 | 0 |
| PM peak | 15 | 5/10 | 12.40 to 13.93 | 32 to 78 | 2 |
| Evening | 10 | 10/10 | 8.90 to 9.67 | 10 to 33 | 0 |
| Evening | 15 | 3/10 | 12.50 to 13.70 | 39 to 75 | 2 |
| Late | 10 | 10/10 | 8.43 to 9.33 | 20 to 47 | 0 |
| Late | 15 | 2/10 | 11.53 to 12.43 | 77 to 104 | 3 |

Late throughput counts completions during arrival minutes 30 through 60, in requests/minute.
Backlog change covers the same interval and can be negative.
Ranges span the ten seeds, and unfinished counts show the largest final remainder.
AM peak at 10 and Interpeak at 15 recovered in the first three seeds, but not all ten.
Their lower tested rates have only three seeds, so they do not establish ten-seed recovery bounds.
AM peak seed 8 failed at 10 and recovered at 15 requests/minute.
Do not assume monotonic results or passing rates between tested points.

### Fleet candidates

The study screened three configurations with unchanged geometry, demand, and operating policies.
`fleet300` added 13 pods and retained the 18 Parking pods.
`fleet287-local` moved those 18 Parking pods to passenger stations without changing the fleet size.
`fleet300-local` combined local placement with a 300-pod fleet.
Each placement retained at least one free berth at its selected passenger station.
The extra-pod allocation differs between the two 300-pod candidates, so their difference does not isolate the Parking relocation.

Baseline Interpeak at 20 and Evening/Late at 15 used all 287 pods in the initial seeds.
Empty travel was 36.8% to 44.9%, with peak stopped counts of 3 to 6.
The placement score used each station's largest origin-demand share across bands divided by its proposed local pod count plus one.
These observations motivated the candidates but did not identify a causal station bottleneck.
The study did not change routes, dispatch rules, berths, defaults, or implementation limits.

All three candidates screened all six bands at the initial baseline recovery boundary, using seeds 1 through 3.
The predeclared score compared recovery count, unfinished demand, longest elapsed run, then mean seed request-to-alight time.
Every candidate with three recoveries and a better score received a next-rate test.
Every next-rate candidate with three recoveries received seeds 4 through 10 at that rate.
This exploratory selection does not establish statistical significance.

| Candidate | Band | Screen rate: recovered / 3 | Next rate: recovered / tested |
| --- | --- | ---: | ---: |
| `fleet300` | Morning | 2: 3/3 | 5: 2/3 |
| `fleet300` | AM peak | 10: 2/3 | not selected |
| `fleet300` | Interpeak | 15: 3/3 | not selected |
| `fleet300` | PM peak | 10: 3/3 | 15: 9/10 |
| `fleet300` | Evening | 10: 3/3 | 15: 9/10 |
| `fleet300` | Late | 10: 3/3 | not selected |
| `fleet300-local` | Morning | 2: 3/3 | 5: 1/3 |
| `fleet300-local` | AM peak | 10: 1/3 | not selected |
| `fleet300-local` | Interpeak | 15: 3/3 | 20: 1/3 |
| `fleet300-local` | PM peak | 10: 3/3 | 15: 1/3 |
| `fleet300-local` | Evening | 10: 3/3 | 15: 1/3 |
| `fleet300-local` | Late | 10: 3/3 | 15: 1/3 |
| `fleet287-local` | Morning | 2: 3/3 | 5: 2/3 |
| `fleet287-local` | AM peak | 10: 1/3 | not selected |
| `fleet287-local` | Interpeak | 15: 3/3 | not selected |
| `fleet287-local` | PM peak | 10: 3/3 | 15: 1/3 |
| `fleet287-local` | Evening | 10: 3/3 | 15: 1/3 |
| `fleet287-local` | Late | 10: 3/3 | 15: 2/3 |

A next-rate denominator of 3 contains the initial seeds only.
A denominator of 10 includes the seven confirmation seeds.
"Not selected" means the screen did not meet the predeclared rule, not that the next rate passed.

The complete study contains 302 distinct arms: 198 baseline, 54 screens, 36 next-rate tests, and 14 confirmation tests.
All planned and conditional arms have confirmed successful test-process exits.
`fleet300` recovered in 9 of 10 seeds at rate 15 in both PM peak and Evening, compared with baseline counts of 5 and 3.
Each of these two failed confirmation seeds left one unfinished request.
The [targeted diagnosis](design/experimental-adoption.md#rejected-candidates) found long journeys in these two cases and a parking-diversion deadlock in baseline AM peak seed 8.
Neither result meets the all-ten recovery criterion.
No candidate established an all-ten recovery improvement at its next tested rate.
The preset and its defaults remain unchanged.

### Metrics, checks, and retained failures

[Post-fix per-arm measurements](measurements/london-full-postfix.csv) repeat this matrix on later source, with unfinished requests and the wait, journey, backlog, throughput, fleet, distance, and drain metrics.
Rows identify the project, study phase, seed group, source commit, schedule ID, and raw artifact.
[Post-fix metadata](measurements/london-full-postfix.json) records provenance, changes, and selection decisions.
Pickup statistics include elapsed pending waits.
Request-to-alight statistics include completed requests only.
`peak_active_vehicles` counts vehicles with assigned work.
For failed recovery, `drain_seconds=0` is a sentinel, not a successful zero-duration drain.

The local artifact bundle retains full configurations, request rosters, frozen source, helpers, binary hashes, commands, logs, and incomplete historical inventory.
The first baseline run stopped at Morning, rate 15, seed 1 because its final oracle omitted two pending requests.
The corrected census checks boarded timings plus pending requests against submitted requests, with uniqueness and completion checks.
The original 40 passing rows had no pending requests and remain valid.
A later supervisor stopped without a final marker.
Its 15 unconfirmed or unstarted rows were rerun, including four inner test passes without outer exit markers.
The 83 superseded historical inventory entries are not 83 executed attempts.

All confirmed matrix arms checked separation, speed, berth ownership, and request accounting once per simulated second.
Selected Interpeak boundary continuations also checked every tick, including physical restores.
Baseline rates 15 and 20, plus `fleet300-local` rate 15, each used seed 1 at minute 60.
Each live clone and restore continued for 60 seconds with no new arrivals, with 3,601 observations, because these runs stepped at 60 Hz.
These continuations do not prove every-tick safety for the full matrix or identical restored trajectories.

An isolated implementation-limit feasibility probe changed only the 300-pod limit and its editor mirror to 400.
The existing conservative payload fixtures failed.
No 400-pod scenario or demand trial existed.
Its worst-case full JSON was 78,625,591 bytes, above 64 MiB.
Its saved-state JSON was 101,378,967 bytes, above 80 MiB.
The limit change was rejected, and the production limits remained unchanged.
On 2026-10-07, the maintainer raised the pod limit to 600 and the saved-state cap to 100 MiB, and dropped the worst-case byte proofs.
The October 15 byte-budget amendment later set the saved-state cap to 128 MiB, and the pod limit of non-Express projects is now 1,000.
