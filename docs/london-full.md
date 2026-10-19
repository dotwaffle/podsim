# LondonFull

LondonFull (`london-full`) maps 269 normalized Tube sites and 40 additional DLR sites onto a PRT network.
It includes the full DLR except West India Quay, omitted for layout clearance.
It does not include Overground.
It has three additional Parking facilities.
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

The default has 327 pods, 754 berths, 5,658 nodes, and 8,844 lanes.
Each passenger site starts with one pod and two berths.
The three existing Parking facilities each have six pods and twelve berths.

The generator assigns 100 extra passenger berths from the largest boarding-plus-alighting share of each site across the six observed bands.
It uses the largest-remainder method, with station ID as the tie-breaker.
Bank keeps two berths because more rows cause a hard layout conflict.
Its two extra berths are reassigned by the same allocation rule.
Bank, Mansion House, and Canary Wharf Jubilee use full-network headings of 234, 56, and 330 degrees.
The Canary Wharf heading clears the North Greenwich guideway.
West India Quay leaves insufficient clearance beside Canary Wharf Jubilee and the Poplar delta.
Its guideways connect through Canary Wharf DLR, and its demand endpoints are excluded.
Poplar uses the shared heading search without an override.
LondonCentral's headings and capacity remain unchanged.

The layout audit reports no hard conflicts and four soft conflicts.
Soft conflicts describe overlap or proximity to source station positions.
They do not establish that a running simulation is safe or that a demand rate is sustainable.

An explicit `-station-berths` value replaces the weighted allocation with a uniform base.
A `-berths ID=N` override changes that site and preserves the remaining defaults.
When both flags are present, the site override applies over the uniform base.
All generated capacity variants must pass normal project and layout validation.

## Depots and Compact stations

Two flags of the scenario command change the station layouts of `london-full` and `london-central`.
Both are opt-in, and the presets without them do not change.
A changed layout gives the project name the suffix " (custom capacity)", also when the berth counts stay the same.

```sh
mise run scenario -- -preset london-full -parking-lanes 2 -parking-berths 6 \
  -compact-stations 940GZZLURGP,940GZZLULBN,940GZZLULGT,940GZZLUERB,940GZZLUBWT,940GZZLUHPC,940GZZLUBOR,940GZZDLTWG \
  -output /tmp/podsim-london-full-flagged.json
```

### Depots

`-parking-lanes N` makes each of the three Parking facilities a [depot](../README.md#parking-depots) with N queue lanes.
`-parking-berths` then sets the slots of each lane, and `-berths parking-north=M` sets the slots of each lane of one facility.
`-parking-lanes 2 -parking-berths 6` keeps the 12 slots of the default.
The depot lies outward from the through lane of the facility, with its entry and exit 200 meters from the station axis.
The heading search uses the shape of an ordinary Parking facility with 12 berths, so the other stations keep their headings.

A London depot is at most 200 meters long.
The command refuses a longer depot and names the facility.
This table gives the most slots in each lane of a depot:

| Lanes | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Slots | 16 | 15 | 15 | 13 | 12 | 11 | 10 | 9 |

### Compact stations

`-compact-stations ID,ID` gives at most 8 passenger stations the four [Compact bays](../README.md#compact-bays) of the `compact-station` preset.
The command refuses an unknown ID, a Parking facility, a repeated ID, more than 8 IDs, and a `-berths` item for a listed station, also when the item has the preset value.
A listed station has 4 berths and at most 4 initial pods.
Its berth count without the flag still sets its shape in the heading search, so the other stations keep their headings.
The compact layout replaces the through lane as a rigid body.
Its aisle is 200 meters from the station axis, its bays face outward, and its bypass curves toward the station throat.
Each pod of the project is then Compact, because only a Compact pod can use the bays.

One visit to a bay holds the aisle for about 30 to 43 seconds, so a Compact station serves about 1.4 to 2 visits per minute.
Each trip end is about one visit.
Use a Compact station only where the demand share of the station times the request rate is below about 0.7 trip ends per minute.
The share is the largest share of the station across the demand bands.
In each band, the shares of all stations add to 2, but the largest shares of all stations add to about 2.75.

The suggested list has eight zone 1 stations with 2 berths and low demand, with shares from 0.0025 to 0.0041: Regent's Park, Lambeth North, Lancaster Gate, Edgware Road (Bakerloo), Bayswater, Hyde Park Corner, Borough, and Tower Gateway.
At 120 requests per minute, they have 0.29 to 0.49 trip ends per minute.
LondonCentral has the first seven and not Tower Gateway.
With the suggested flags, LondonFull has 5,740 nodes, 9,024 lanes, and 4 soft conflicts, and its indented export has 14,912,471 bytes.

### Measurements, October 19, 2026

The candidates are LondonFull projects from the scenario command.
The network SHA-256 is the `network_sha256` value of its summary line.

| Arm | Flags | Network SHA-256 |
| --- | --- | --- |
| Default | none | `61761e3b5281228dac678ade72be718b5406ea4a150dcc400ffe5f52e26976db` |
| Depots | `-parking-lanes 2 -parking-berths 6` | `4630e4984fd828ed4d04c7e3fbb03235e8407fbd63db7493608a68d7bb08548e` |
| Compact stations | `-compact-stations` with the eight suggested IDs | `9275645171f4c9e5cf34a36727bc11895e5026a0457424d70ee397d3a6d31807` |
| Both | both flag sets | `83d4a83f4f9931a1382e143ef645bd0a31fcb22f0314998087512ad01581ea39` |

Two more arms set `"class": "compact"` on each fleet placement of the Default and Depots projects, with no other change.
They give the same results as Default and Depots in every run below, so the tables leave them out.

The smoke runs each arm with a fixed 30-minute window:

```sh
mise run compare -- -project ARM.json -seed 1 -arrivals-for 20m -duration 30m -loads 6s,1s \
  -pattern profile -redistribution-policies off -platoon-policies virtual
```

Each arm gets the same request schedule.
For seed 1, the schedule ID is `2f47adff4fa5d2f7` at 10 requests per minute (199 requests) and `d0e1f87e74785b19` at 60 requests per minute (1,199 requests).
For seeds 2 and 3 at 60 requests per minute, the schedule IDs are `a38852cc1e08ae5a` and `fa529e7ef5e9bc92`.
Without `-platoon-policies virtual`, the Default arm gives 360 served, 607 skipped, a 470.5 s mean wait, and 845 stopped pod-seconds at 60 requests per minute.

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
| Default | 10/min | 152 | 0 | 55.4 | 42 | | | |
| Depots | 10/min | 152 | 0 | 55.9 | 43 | | | |
| Compact stations | 10/min | 152 | 0 | 55.9 | 42 | | | |
| Both | 10/min | 152 | 0 | 56.4 | 43 | | | |
| Default | 60/min | 363 | 602 | 464.9 | 954 | 33 | 816 | 105 |
| Depots | 60/min | 359 | 610 | 472.2 | 2,107 | 1,258 | 749 | 100 |
| Compact stations | 60/min | 364 | 599 | 461.0 | 941 | 23 | 835 | 83 |
| Both | 60/min | 364 | 607 | 464.1 | 2,273 | 1,268 | 828 | 177 |

Seeds 2 and 3 at 60 requests per minute, per run, not summed:

| Seed | Arm | Served | Skipped | Wait mean (s) | Stopped pod-s | Parking | Guideway | Stations |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 2 | Default | 365 | 615 | 465.5 | 876 | 11 | 754 | 111 |
| 2 | Depots | 369 | 616 | 467.3 | 1,580 | 704 | 768 | 108 |
| 2 | Compact stations | 363 | 611 | 467.6 | 975 | 11 | 791 | 173 |
| 2 | Both | 365 | 612 | 466.9 | 1,771 | 704 | 929 | 138 |
| 3 | Default | 345 | 626 | 477.6 | 1,126 | 39 | 933 | 154 |
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
| Default | 10/min | 1,797 | 3 of 3 | 156.7 | 1,416 | 0 |
| Depots | 10/min | 1,797 | 3 of 3 | 158.0 | 3,064 | 1,737 |
| Compact stations | 10/min | 1,797 | 3 of 3 | 156.9 | 1,726 | |
| Both | 10/min | 1,797 | 3 of 3 | 157.6 | 3,033 | |
| Default | 15/min | 2,695 | 1 of 3 | 242.6 | 3,274 | 10 |
| Depots | 15/min | 2,695 | 1 of 3 | 243.5 | 4,937 | 1,919 |
| Compact stations | 15/min | 2,695 | 1 of 3 | 243.1 | 3,034 | |
| Both | 15/min | 2,695 | 1 of 3 | 242.9 | 4,431 | |

At 15 requests per minute, 2 requests remain in each arm.
These runs do not establish a sustainable demand rate.

## Validation boundary

The initial demand setting is 10 requests per minute, with AM peak weights and seed 20260929.
Automatic demand and redistribution start disabled.
A ten-minute AM peak smoke run on October 7, 2026 generated 100 requests and completed 15, with no skipped requests.
Safety checks passed each simulated second.
The physical restore test preserved active requests and passed safety checks during its 30-second continuation.
A separate test checks that simultaneous trips around Poplar and both Canary Wharf sites drain safely.
These checks do not establish a sustainable demand rate.

The project limits admit 12,000 nodes, 20,000 lanes, 600 stations, 1,000 pods, 400,000 flows, and 32 MiB of project JSON.
Express projects retain their separate 600-pod limit.
The default compact project JSON has 5,562,812 bytes.
The default indented export has 14,823,549 bytes.
The saved-state cap is 128 MiB.
The plain saved-state and full-stream byte proofs include completed history and the independent project and topology caps.
Express has no byte proof.
Stream JSON is capped at 64 MiB, with 32,832 bytes of gzip overhead; HTTP state has a separate 128 MiB cap.
The junction-pair limit is 250,000, and the track-cell limit remains 64,000.

### Matched fleet qualification

`mise run qualify:london` runs 327, 600, and 1,000 pods on one capacity variant, at 10 and 120 requests per minute.
The variant has 6,507 nodes, 9,976 lanes, 312 stations, and 1,037 berths.
Passenger sites have at least three berths except Bank, which keeps two.
Each parking site has 32 berths, and the berth pitch is 37.5 m.
The default project and its parking behavior do not change.
Smaller fleets sample the same 1,000 starting placements evenly.

Each case warms up for 600 simulated seconds, measures 180 seconds of ticks, and checks safety and simulation contracts each second.
It exercises production full and delta publications, gzip inflation, client validation and assembly, HTTP state, and physical restore.
It then checks another 60 seconds of operation and requires pods to move after restore.
Mean and p99 tick times must fit the 16.67 ms native 1x budget.
The task is opt-in because all six traffic cases take several minutes.
The overloaded cases fill the 200-order session queue; passing them does not establish a sustainable arrival rate.

One run of all six cases on Go 1.27.2 passed without restore demotions.

| Pods | Requests/minute | Mean tick, ms | p99 tick, ms | Pods moving after restore |
| ---: | ---: | ---: | ---: | ---: |
| 327 | 10 | 0.051 | 0.080 | 110 |
| 327 | 120 | 0.249 | 0.481 | 327 |
| 600 | 10 | 0.059 | 0.093 | 109 |
| 600 | 120 | 0.588 | 8.228 | 581 |
| 1,000 | 10 | 0.085 | 0.154 | 129 |
| 1,000 | 120 | 1.104 | 14.736 | 838 |

Some earlier runs of the overloaded 1,000-pod case missed the p99 budget, at 18.947 to 19.624 ms, on both Go 1.27.1 and Go 1.27.2.
Later runs of that case passed, with a p99 of 14.1 to 14.9 ms in each run.
The cause of the earlier misses is not known.
If they occur again, keep the source, binary, and inputs of the failed run before you change the threshold.

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
The [mirrored layout study](station-mirror-load.md) also finds growing backlogs at 15 and 20 requests per minute.

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
The [308-arm post-fix study](london-full-postfix.md) repeats this matrix and its conditional selection rules.

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
The [targeted diagnosis](experimental-adoption.md#rejected-candidates) found long journeys in these two cases and a parking-diversion deadlock in baseline AM peak seed 8.
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
Each live clone and restore continued for 60 seconds with no new arrivals, with 3,601 observations at 60 Hz.
These continuations do not prove every-tick safety for the full matrix or identical restored trajectories.

An isolated implementation-limit feasibility probe changed only the 300-pod limit and its editor mirror to 400.
The existing conservative payload fixtures failed.
No 400-pod scenario or demand trial existed.
Its worst-case full JSON was 78,625,591 bytes, above 64 MiB.
Its saved-state JSON was 101,378,967 bytes, above 80 MiB.
The limit change was rejected, and the production limits remained unchanged.
On 2026-10-07, the maintainer raised the pod limit to 600 and the saved-state cap to 100 MiB, and dropped the worst-case byte proofs.
The October 15 byte-budget amendment later set the saved-state cap to 128 MiB, and the pod limit of non-Express projects is now 1,000.
