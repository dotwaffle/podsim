# Express standees evidence

This record holds the research and the scratch measurements for the [Express standees contract proposal](../contracts/express-standees-contract-proposal.md).
All runs used scratch overlays on `b2262f6`, and the overlays did not land.
The results authorize no change.

## Vehicle sizes

| Vehicle | Length x width (m) | Seats | Total | Seats per m | Total per m |
| --- | --- | ---: | ---: | ---: | ---: |
| Group (simulation) | 6 x 2.5 | 8 | 8 | 1.33 | 1.33 |
| Express (simulation) | 10 x 2.5 | 20 | 20 | 2.00 | 2.00 |
| Morgantown | 4.72 x 2.03 | 8 | 21 | 1.69 | 4.45 |
| Rivium Gen 2 | 6.0 x 2.1 | 12 | 24 | 2.00 | 4.00 |
| GRT Gen 3, standard | 6.04 x 2.10 | 8 | 22 | 1.32 | 3.64 |
| GRT Gen 3, most seats | 6.04 x 2.10 | 12 | 18 | 1.99 | 2.98 |

A real vehicle for 20 to 24 passengers is 4.7 to 6.0 m long only because passengers stand.
Rivium Gen 2 has 12 seats and 12 standee places, and its planning load is 20.
Rivium Gen 1 had 8 seats and 2 standee places, and Gen 3 has 8 and 14, or 12 and 6.
The Gen 3 specification page does not name Rivium, so that link is an inference.
Morgantown has 8 seats and 13 standee places, and some sources say 12.
It has a door time of 20 s and an acceleration of 2 ft/s².

Not found: Rivium Gen 1 dimensions, achieved flow and dwell at Rivium, and the Morgantown guideway width, crush rating, and measured headway.

## Acceleration and braking

| Stock | Acceleration (m/s²) | Service braking (m/s²) | Emergency braking (m/s²) | Quality |
| --- | ---: | ---: | ---: | --- |
| LU 1992 Central | 1.3 | 1.15 maximum | 1.4 | Powell and Palacin 2015 |
| LU S Stock | 1.3 | 0.2 to 1.15 | 1.4 | Braking from TfL, acceleration from Wikipedia (weak) |
| LU 2009 | 1.3 | 1.14 | 1.4 | Weak |
| LU 2024 | 1.4 | Not found | Not found | Weak |
| DLR B2007 | 1.4 maximum in operation | 0.80 full ATO service | 1.44 | TfL |
| DLR B23 | Not found | Not found | 1.26 guaranteed | TfL |

No source gives an achieved in-service peak acceleration, a jerk limit, or a speed range for any stock.
The figures are design values or maxima.

No credible 1996 Jubilee figure exists in the sources that this search could open.
A 2011 forum post says that the 1996 stock has a lower initial acceleration than the 1992 stock, because it has fewer motored cars, and it gives no number.
So the working values stay.
An earlier research note suggested 1.0 m/s² for a short vehicle, as an inference, and the maintainer chose 1.3 m/s² from named stock.

Standing tolerance, as cited by Powell and Palacin: a guideline of 1.1 to 1.5 m/s², and loss of balance at a mean of 1.28 m/s² forward when unsupported, 2.26 m/s² with a strap, and 2.65 m/s² with a grab rail.
Krasna 2021 gives a starting point below 1.5 m/s² for acceleration and below 1.0 m/s² for braking.

## Door flow

TCRP Report 100, Part 5, Exhibit 5-15 gives the time for each passenger in a single door stream.
For level boarding the range is 1.11 s (PATH) to 2.61 s (BART).
For level alighting the range is 1.38 s (SkyTrain) to 2.03 s (Edmonton).
Steps about double these times.
A flow of about 2 s in each stream, with two streams, gives the 1 s for each passenger that the proposal uses.
That gives 22 s to board 20 passengers, and Morgantown has a door time of 20 s for its load of 21.
This check is order of magnitude only.

## Curve tails

The tail candidates of this table are for one lane.
`Old` is the formula at `b2262f6`, `range` is the range of the tangent angle, `bisect` is 20 / cos(range / 2), and `scan` is the bound of section 4 of the proposal.

| Lane | Length (m) | Chord (m) | Range (degrees) | Old (m) | Bisect (m) | Scan (m) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `rail-hub` `s01-in-01` | 189.3 | 144.2 | 121.7 | 299.1 | 41.1 | At most 20.75 |
| `rail-hub` `s01-in-02` | 253.5 | 170.0 | 132.3 | 1,933.1 | 49.4 | At most 20.75 |
| `rail-hub` `s01-in-04` | 388.0 | 224.7 | 140.3 | 763.5 | 58.8 | At most 20.75 |
| `rail-hub` `s01-in-06` | 525.2 | 281.6 | 143.6 | 340.1 | 63.9 | At most 20.75 |
| Gentle arc, control 40 m off the chord | 205.2 | 200.0 | 43.0 | 21.5 | 21.5 | 20.38 |
| Hairpin, chord 30 m, control at 200 m | 204.2 | 30.0 | 171.3 | 263.3 | 263.3 | 94.09 |
| Hairpin, chord 22 m, control at 320 m | 321.7 | 22.0 | 176.0 | 573.1 | 573.1 | 269.72 |
| Narrow arc, chord 10 m, control at 100 m | 101.0 | 10.0 | 174.2 | 394.3 | 394.3 | 101.29 |

The bisect value equals the old value on a symmetric arc, because the chord is then the bisector.
So the bisect bound does not help a hairpin.

### All presets

| Preset | Lanes | Curved | Old maximum (m) | Scan maximum (m) | Old over length | Scan over length | Prepares with all classes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| `small` | 50 | 40 | 1,933.1 | 20.75 | 40 | 0 | Yes |
| `busy` | 106 | 90 | 1,933.1 | 20.75 | 80 | 0 | Yes |
| `parking-constrained` | 54 | 42 | 1,933.1 | 20.75 | 42 | 0 | Yes |
| `rail-hub` | 110 | 96 | 1,933.1 | 20.75 | 70 | 0 | Yes |
| `rail-hub-incidents` | 110 | 96 | 1,933.1 | 20.75 | 70 | 0 | Yes |
| `scale100` | 714 | 0 | None | None | 0 | 0 | Yes |
| `independent-banks` | 29 | 0 | None | None | 0 | 0 | Yes |
| `compact-station` | 51 | 33 | 1,933.1 | 20.75 | 32 | 26 | No, compact bays |
| `london-central` | 3,248 | 237 | 2,789.0 | 67.0 | 218 | 208 | No, compact bays |
| `london-full` | 9,024 | 291 | 11,636.7 | 66.74 | 271 | 258 | No, compact bays |

The over-length lanes of the scan bound are short.
Of the 208 in `london-central`, 182 are under 24 m, and of the 258 in `london-full`, 208 are under 24 m.
All but one of them have a chord under 20 m, so the scan gives the lane length plus the margin.
The other 26 and 50 lanes are at least 40 m long, and they are U-turn bends with a tangent range up to 179.8 degrees.

### Independent checks

An independent scan over each curved lane of each preset, with its own polyline code, found no pair at or past the tail with a chord under 20 m.
The smallest chord was 20.166 m.

| Check, all classes | `small` | `busy` | `parking-constrained` | `rail-hub` | `independent-banks` |
| --- | ---: | ---: | ---: | ---: | ---: |
| Lane pairs | 1,225 | 5,565 | 1,431 | 5,995 | 406 |
| Close samples on lanes that share a node | 309,024 | 902,758 | 314,076 | 1,077,252 | 53,628 |
| Close samples outside a held junction section | 0 | 0 | 0 | 0 | 0 |
| Close samples on lanes that share no node | 0 | 0 | 0 | 0 | 0 |
| Smallest gap of lanes that share no node (m) | 28.33 | 28.33 | 28.33 | 28.33 | 60.00 |
| Route pairs at or past the tail, up to 3 further lanes | 32,678,758 | 98,672,808 | 31,927,863 | 127,441,809 | 2,703,900 |
| Of these, under 20 m on the next lane | 1,639 | 5,757 | 2,114 | 5,492 | 1,346 |
| Smallest such chord (m) | 10.17 | 7.29 | 9.22 | 7.73 | 10.40 |
| Of these, under 20 m on a later lane | 0 | 0 | 0 | 0 | 0 |

The sample step was the larger of 0.5 m and 1/400 of the lane length for lane pairs, and about 1 m for route pairs.
`scale100` did not finish this check inside the 5-minute limit.
The first run reported 302 uncovered samples on `busy`.
They came from the oracle: a sample at the lane end was above the last cell bound by a rounding error, and the corrected oracle reports 0.

The nonzero row shows that a path gap of the tail does not by itself give 20 m when the front pod is on the next lane.
Junction ownership gives the separation in that case.

## Rail-hub runs

Each row is `cmd/compare` with `-pattern hotspot`, `-request-every 5s`, `-duration 30m`, `-redistribution-policies off`, at seeds 1, 2, and 3.
The oracle called `SafetyObservation().Check()` on each tick, and an independent loop measured each pair of centers with a large pod.
There were no violations in any run.
Fleets with a standee envelope use the Group class as a proxy, because the large-class arithmetic is common and the marker is not necessary for this measurement.

| Project | Tail | Motion (accel / plan / max) | Served | Stopped pod-seconds | Peak stopped | Smallest large center gap (m) |
| --- | --- | --- | --- | --- | --- | ---: |
| Plain, Legacy fleet | Old | 2 / 2 / 2 | 102, 104, 106 | 1,337, 1,323, 1,209 | 7, 6, 6 | No large pod |
| All lanes, Legacy fleet | Old | 2 / 2 / 2 | 1, 15, 3 | 49,033, 40,845, 48,293 | 30, 28, 30 | No large pod |
| All lanes, Legacy fleet | Scan | 2 / 2 / 2 | 98, 99, 102 | 2,565, 1,967, 2,038 | 10, 8, 8 | No large pod |
| All classes, Group fleet | Old | 2 / 2 / 2 | 1 (seed 1 only) | 49,033 | 30 | 26.67 |
| All classes, Group fleet | Scan | 2 / 2 / 2 | 98, 99, 102 | 2,565, 1,967, 2,038 | 10, 8, 8 | 20.877 |
| All classes, Group fleet | Bisect | 2 / 2 / 2 | 99, 101, 107 | 2,617, 2,202, 1,794 | 10, 8, 8 | 21.25 |
| All classes, Group fleet | Scan | 1.3 / 2 / 2 | 97, 103, 98 | 3,029, 2,332, 2,263 | 12, 8, 8 | 20.877 |
| All classes, Group fleet | Scan | 1.3 / 1.15 / 1.15 | 96, 98, 99 | 2,718, 2,004, 1,857 | 10, 8, 8 | 26.10 |
| All classes, Group fleet | Scan | 1.3 / 0.8 / 0.8 | 98, 93, 95 | 2,698, 1,674, 1,689 | 10, 7, 8 | 20.877 |
| All classes, Group fleet | Scan | 1.3 / 0.8 / 1.15 | 94, 95, 100 | 2,824, 2,507, 1,966 | 10, 7, 8 | 20.877 |
| All classes, mixed fleet | Scan | 1.3 / 0.8 / 1.15 | 98, 99, 98 | 2,530, 1,844, 1,856 | 10, 7, 8 | 20.877 |

The rows with acceleration 1.3 and plan and maximum braking of 2, of 1.15, and of 0.8 ran before a 0.25 m margin went into the scan, so their tails are 0.25 m shorter.
The mixed fleet is one Group pod, one Legacy pod, and one Compact pod in turn.
The differences between the motion rows are in the seed noise of these runs.
Served orders do not separate the motion rows, because this demand is above the supply of the fleet.

One straight lane with all classes served 43 at seed 1 with the old tail and 102 with the scan tail, against 102 for the plain preset.
The [Express qualification](express-qualification.md) gives 41 at `4d20d6d`.

| Long run, `-request-every 2s` | Duration | Served | Stopped pod-seconds | Peak stopped |
| --- | --- | ---: | ---: | ---: |
| Plain, old tail | 2 h | 429 | 4,102 | 8 |
| Group, scan tail, 2 / 2 / 2 | 2 h | 426 | 4,537 | 9 |
| Group, bisect tail, 2 / 2 / 2 | 2 h | 413 | 5,375 | 9 |
| Plain, old tail | 1 h | 211 | 2,883 | 8 |
| Group, scan tail, 2 / 2 / 2 | 1 h | 213 | 3,606 | 9 |
| Group, scan tail, 1.3 / 0.8 / 1.15 | 1 h | 201 | 3,482 | 10 |

In each envelope run, the largest speed rise was 1.3 m/s² in each tick, and the largest speed drop was 1.149 m/s².
The stop point at maximum braking was at most 2e-12 m past the reservation end.

## Random curved networks

Each network is a ring from `scenarios.Config` with all classes, about 70 percent Group pods, and 8 simulated minutes.
The generator changes the ring links and the station fan curves.
The rows with no hairpin have 4 to 11 pods and one trip each 2 s, and the other rows have 8 to 15 pods and one trip each second.
The first three hairpin rows ran before the 0.25 m margin went into the scan.
An independent loop checks 20 m on each tick, and the run of a network stops at its first violation.

| Generator | Tail | Networks | Ticks | Networks with a violation | Smallest large center gap (m) |
| --- | --- | ---: | ---: | ---: | ---: |
| Opposed bends, no hairpin | Bisect | 70 | 2,016,000 | 0 | 20.57 |
| Opposed bends, no hairpin | Scan | 71 | 2,044,800 | 0 | 20.57 |
| Opposed bends, no hairpin | 20 m | 72 | 2,073,600 | 0 | 20.57 |
| Opposed bends, no hairpin | 0.7 x scan | 71 | 2,044,800 | 0 | 20.57 |
| Hairpin in each link | Scan | 35 | 1,008,000 | 0 | 20.55 |
| Hairpin in each link | 20 m | 49 | 1,091,825 | 18 | 19.74 |
| Hairpin in each link | 0.7 x scan | 35 | 1,008,000 | 0 | 20.55 |
| Hairpin in each link, 1.3 / 0.8 / 1.15 | Scan | 22 | 633,600 | 0 | 20.78 |
| Sharp straight apex in each link | Scan | 31 | 892,800 | 0 | 20.60 |
| Sharp straight apex in each link | 20 m | 31 | 892,800 | 0 | 20.60 |

The first run of this fuzz had 0 accepted networks, so its result was void and it is not in the table.
The runs with 20 m and no hairpin show that the generator without hairpins cannot detect a short tail.
The run with 0.7 x scan shows that random motion does not detect a tail that is short by 30 percent, because cells are about 30 m long and a follower seldom stops at the critical position.
Thus a static scan must gate the bound, and the motion oracle only adds to it.
The sharp apex is the case in which the same-lane tail is exactly 20 m in both builds, so the separation there comes only from junction ownership.

The scan with no margin failed `TestLargeGeometryPathProjection/narrow_monotone_arc` with a gap of 10 m at arc 0.
The bound must be above the supremum, because a pair at the supremum is itself under 20 m.

## Plane transitions

A blind Codex review of section 4 of the proposal found that the bank audit gives each lane only its own plane, while a pod near a join occupies two planes.
A check at `9b13e19` reproduced this in the real simulation, and `Check` reported `SeparationError`.
Lane `A` is on the upper plane and joins a lane on the lower plane, and lane `Q` is on the lower plane at the given offset.

| Network | Smallest center gap (m) |
| --- | ---: |
| Quadratic `A`, `Q` at 24 m | 19.256 |
| Straight `A`, `Q` at 24 m | 18.147 |
| Quadratic `A`, `Q` at 20 m | 15.257 |
| Straight `A`, `Q` at 20 m | 14.147 |
| Mirror, `Q` behind the join | 14.188 |
| Control, `Q` at 30 m, one plane | 24.147, no reports |

These gaps are within the body window of 6 m, and the result is the same at the old tail and at a tail of exactly 20 m.
The envelopes of radius 6 m do not touch at these gaps, so a run breaks the 20 m rule and not the bodies.

No preset ships a large class on these planes, and no project JSON in the repository has `separationGroup`.
The editor cannot author a separation group, and a JSON import can.
London has separated lanes that join other planes, but a forced large class stops at an earlier refusal.
Small classes have the same hole, unobserved, with a body gap of 6.15 m at `Q` of 12 m and no reports.
The shipped London project had 0 hits at a window of 6 m.

The first candidate front of `max(6, tail)` made the observer report far more: gaps of 5.85 m and 0.15 m, and 35 of 61 control runs.
The proposal keeps the front on the old bound for that reason.

## Tests that pin the old tail

With the scan tail, `go test -short` of `internal/sim`, `internal/session`, `internal/scenarios`, and `cmd/compare`, and of `internal/project`, `internal/editormodel`, and `cmd/projectcheck`, failed only these tests:

| Test | Reason |
| --- | --- |
| `TestPlatoonReleaseBoundedEnd/curved` | Pins a tail of 30.89 m. It gets 20.43 m. |
| `TestPlatoonRestoreOldRuleLink` | Its demotion needs the long tail, and the restore now stays in place. |
| `TestLaneTailOverruns` | Its curved lanes no longer overrun. |
| `TestYieldRelocationClaimsKeepsStoppingNode` | Its setup check needs a cell that holds the berth node, and the shorter tail changes that cell. |
| `TestTailWarningsCap`, `TestEngineTailWarnings`, `TestDraftTailWarnings`, `TestProjectCheckWarningWriteError`, `TestProjectCheckTailWarnings` | `rail-hub` no longer gives a warning. |

## Saturated headway

This table uses `mergeCorridor` with all classes and the fan replaced by isolated parking stations.
Its absolute values differ from the pins of `TestMergeCorridorHeadway`, because the lanes admit large classes and the tail is 20 m.
So only the ratios carry over.

| Case | Legacy, 2 / 2 / 2 (s) | Group, 2 / 2 / 2 (s) | 1.3 / 2 / 2 (s) | 1.3 / 1.15 / 1.15 (s) | 1.3 / 0.8 / 0.8 (s) | 1.3 / 0.8 / 1.15 (s) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Straight lane | 6.685 | 6.685 | 7.693 | 8.902 | 10.824 | 8.902 |
| Through a lane boundary | 7.665 | 7.665 | 8.762 | 8.827 | 10.264 | 8.827 |
| Two streams, 90 degree merge | 9.217 | 9.217 | Not run | 11.100 | 11.154 | 11.100 |

The plan rate has no effect here, because no pod in this measurement stops at its route end or meets a lower limit.

## Cost of the scan

A call on the development host, under a load of about 30, took 197 µs for a 200 m fan lane, 2.3 ms for a 5 km arc, and 16 ms for a 10 km hairpin.
`indexGeometryTails` has 8 call sites in product code, and it computes each lane again on each call.

## Sources

- [GRT specification](https://web.archive.org/web/20220119215715/https://www.2getthere.eu/technology/vehicle-types/grt-vehicle-automated-minibus/)
- [Rivium overview](https://web.archive.org/web/20200927034802/https://www.2getthere.eu/projects/rivium/)
- [Rivium 2.0](https://web.archive.org/web/20200920115357/https://www.2getthere.eu/projects/rivium/rivium-2-0/)
- [EVS31 paper](https://surfsharekit.nl/objectstore/5d0781fd-8535-47ce-b2d6-3367a43d2452)
- [ATRA characteristics table](https://www.advancedtransit.org/advanced-transit/prt-characteristics/)
- [Raney and Young 2004](https://rosap.ntl.bts.gov/view/dot/17792/dot_17792_DS1.pdf)
- [Morgantown system overview](https://rosap.ntl.bts.gov/view/dot/36306/dot_36306_DS1.pdf)
- [UMTA collision avoidance 1980](https://rosap.ntl.bts.gov/view/dot/11338/dot_11338_DS1.pdf)
- [Powell and Palacin 2015](https://link.springer.com/content/pdf/10.1007/s40864-015-0012-y.pdf)
- [TfL S Stock brakes](https://foi.tfl.gov.uk/FOI-0532-1718/S%20Stock%20Brakes.pdf)
- [TfL DLR B2007 reply](https://tfl.gov.uk/corporate/transparency/freedom-of-information/foi-request-detail?referenceId=FOI-2110-1718)
- [TfL rolling stock information sheets](https://foi.tfl.gov.uk/FOI-0237-2223/RS%20Info%20Sheets%204%20Edition.pdf)
- [Krasna 2021](https://www.frontiersin.org/articles/10.3389/fbioe.2021.680883/full)
- [District Dave's forum, 2011](https://districtdavesforum.co.uk/thread/16088/jubilee-line-preformance-upgrade)
- [TCRP Report 100, Part 5](https://onlinepubs.trb.org/onlinepubs/tcrp/docs/tcrp100/Part5.pdf)
