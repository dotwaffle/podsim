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

A later six-hour sustained-load screen finds growing AM peak backlogs at 15 and 20 requests per minute.
Station approach buffers do not resolve that overload and remain disabled by default.
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

The project limits admit 5,000 nodes, 300 stations and pods, 65,000 flows, and 10 MiB of project JSON.
The saved-state cap is 80 MiB, including the conservative case with JSON-escaped IDs and diagnostic text.
Stream caps are 65 MiB of JSON and 66 MiB of gzip data.
The lane, junction-pair, and track-cell limits remain unchanged.

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
