# LondonFull

LondonFull (`london-full`) maps all 269 normalized London Underground passenger sites onto a PRT network.
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

The topology contains 272 source stops merged into 269 passenger sites and 313 undirected adjacencies.
Monument merges into Bank, Paddington H&C into Paddington, and Hammersmith H&C into Hammersmith.
The two Edgware Road sites remain separate.
Coordinates come from the TfL Unified API, including Nine Elms and Battersea Power Station.

Demand contains all 60,996 directed pairs from the 2024 Tuesday-to-Thursday network matrix whose endpoints map to the Tube roster.
Those journeys can use other modes between their endpoints.
This differs from LondonCentral's 2019 LU-specific matrix.
The full preset offers Morning, AM peak, Interpeak, PM peak, Evening, and Late.
The source has no Early or Night demand, so those selections are absent.

The [offline converter](../tools/londonfull/README.md) documents all source hashes, aliases, exclusions, and exact totals.
It preserves all eight source columns, including the two empty columns.
It does not truncate pairs or synthesize weights.

## Capacity and geometry

The default has 287 pods, 674 berths, 4,988 nodes, and 7,778 lanes.
Each passenger site starts with one pod and two berths.
The three existing Parking facilities each have six pods and twelve berths.

The generator assigns 100 extra passenger berths from the largest boarding-plus-alighting share of each site across the six observed bands.
It uses the largest-remainder method, with station ID as the tie-breaker.
Bank keeps two berths because more rows cause a hard layout conflict.
Its two extra berths are reassigned by the same allocation rule.
Bank and Mansion House use full-network headings of 234 and 56 degrees.
LondonCentral's headings and capacity remain unchanged.

The layout audit reports no hard conflicts and three soft conflicts.
Soft conflicts describe overlap or proximity to source station positions.
They do not establish that a running simulation is safe or that a demand rate is sustainable.

An explicit `-station-berths` value replaces the weighted allocation with a uniform base.
A `-berths ID=N` override changes that site and preserves the remaining defaults.
When both flags are present, the site override applies over the uniform base.
All generated capacity variants must pass normal project and layout validation.

## Validation boundary

The initial demand setting is 10 requests per minute, with AM peak weights and seed 20260929.
Automatic demand and redistribution start disabled.
Bounded AM peak trials used 20 minutes of arrivals and three seeds.
All requests drained at 10 requests per minute, within 64 simulated minutes.
Mean pickup waits ranged from 63 to 84 seconds.
Higher rates increased waiting times in the sampled runs.
These finite-arrival trials do not establish a sustainable rate.

A separate one-hour run checked safety each second and completed all 199 requests.
A physical restore at 20 minutes preserved the 287-pod fleet and its pending requests.
The restored continuation passed safety checks on every tick for 30 seconds.
Chromium143 loaded the active saved session in three Go/WASM clients at 0, 300, and 600 ms simulated RTT.
Each client kept one full baseline and acknowledged subsequent deltas without HTTP state polling.
The editor imported and exported all 60,996 demand pairs and applied the full project through a gzip command.
The exported file was 8,540,140 bytes.
These checks used software rendering.
They do not qualify physical GPUs, Firefox, Safari, or a Fly deployment.

At 10 requests per minute, uniform two-berth stations and zero reserve pods did not worsen the seed-1 completion result.
Increasing each Parking reserve from six to ten pods did not improve it.
The preset retains demand-weighted berth space and the existing six-pod Parking reserves.
These trials do not prove a performance benefit from that extra capacity.
A passing layout audit does not qualify a demand rate.
The full qualification envelope has not been run.

The project limits admit 5,000 nodes, 300 stations and pods, 65,000 flows, and 10 MiB of project JSON.
The saved-state cap is 80 MiB, including the conservative case with JSON-escaped IDs and diagnostic text.
Stream caps remain 64 MiB of JSON and 65 MiB of gzip data.
The lane, junction-pair, and track-cell limits remain unchanged.
