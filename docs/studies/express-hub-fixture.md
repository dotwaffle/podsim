# Express hub fixture

## Decision

The first reading was not to advance this fixture to the remaining qualification.
On 2026-11-06 the maintainer waived the single rider who missed the pickup limit, for this screen only, and asked for the gaps to be closed.
The section "Follow-up" has the results.
Express improves hub throughput in every seed with current policies on, but background seed 2 exceeds the individual pickup limit.
The oracle reports no failure in any arm.
The logs do not establish reproducibility, full request conservation, or return-to-parking behavior.
This is an 18-arm feasibility screen, not an adoption qualification or a default change.

| Design clause | Result |
| --- | --- |
| Stop on safety failure | No observed failure: initial check and 108,000 tick checks pass in each of 18 arms. |
| Stop on conservation failure | No recorded accounting inconsistency found. Full conservation was not checked. |
| Stop on reproducibility failure | Not checked. No arm ran twice. |
| Stop on unresolved parking deadlock | No deadlock identified, but parking return was not exercised in the logged samples. This clause is not established. |
| Improve hub throughput in every seed with policies on | Pass for aggregate hub passengers and whole parties, against both baseline and control. Reverse-direction service remains sparse. |
| Assess background harm against individual gates | Fail for pickup wait against baseline at seed 2. Against control, seed 2 fails pickup and journey gates. |

For minutes 20 to 60, values below give seeds 1 / 2 / 3.
The direction with no reverse completion must not be read as a reverse-direction improvement.

| Policies on, minutes 20 to 60 | Baseline | Control | Express |
| --- | --- | --- | --- |
| Waterloo to Bank passengers/min | 11.550 / 9.925 / 8.000 | 10.450 / 9.700 / 9.500 | 38.125 / 38.375 / 37.775 |
| Waterloo to Bank whole parties/min | 0.800 / 1.100 / 0.850 | 0.750 / 0.975 / 0.900 | 3.575 / 4.025 / 3.875 |
| Bank to Waterloo passengers/min | 0.225 / 0 / 0 | 0.225 / 0 / 0 | 0.275 / 0 / 0 |
| Background completed | 160 / 161 / 153 | 164 / 158 / 152 | 376 / 373 / 351 |
| Full-cap matched background wait exceedances against baseline | Reference | 45 / 52 / 43 | 0 / 1 / 0 |
| Full-cap matched background journey exceedances against baseline | Reference | 27 / 32 / 36 | 0 / 0 / 0 |

The design requires a background assessment, and a throughput gain does not waive the [individual gates](../design/experimental-adoption.md#numerical-limits).
The unresolved checks also prevent a claim that all stop clauses passed.

## Source and method

The source checkout is `fa587966` (`fa58796679c3f36cf70d7bf8b956d9d53f240f64`), plus the uncommitted `tools/study-express-fixture/` runner.
That commit permits small-only depots with Express orders.
The preceding Bank movement fix, `1591d355`, changed the network.
It preserved Bank's 64 movements and changed the folded lane instead of deleting it.
These runs therefore do not use the network of the earlier screen.

The project is `/tmp/build/podsim/cache-xfixture/out/A-attempt-6.json`.
Its SHA-256 is `ec10ff242ecb1b37c69ac5c26c2bcfb7f9bb1392b3846cad6c22c13a04eac595`.
This hash was computed for this report.
The logs contain no independent source, runner, or binary hashes captured at execution time.

The design is the last `codex` block of `/tmp/build/podsim/reviews/codex-xdesign.log`.
The fixture history is the last `codex` block of `/tmp/build/podsim/reviews/codex-xfixture.log`.
The measurements come from the 18 logs in `tools/study-express-fixture/results/`.
`/tmp/build/podsim/cache-xfixture/arms.done` records exit 0 for every arm.
Its wall seconds, seeds 1 / 2 / 3, are 127 / 120 / 127 for baseline off and 136 / 126 / 133 for baseline on.
They are 128 / 131 / 124 for control off and 131 / 134 / 132 for control on.
They are 1048 / 1045 / 1041 for Express off and 1054 / 1025 / 1012 for Express on.
These rounded process walls differ from the runner's internal walls below.

Each arm runs for 60 simulated minutes at 30 ticks per second.
Baseline uses current `LondonFull()` without the candidate changes.
Control retains A6 geometry, masks, parking, services, and the Express marker, but removes all 16 Express pods.
Express retains A6 and all 16 Express pods.
All arms retain the same 327 Compact placements.
Virtual platoons follow the preset.
Policies off means redistribution and pickup reassignment off together.
Policies on means guarded redistribution and pickup reassignment on together.
No arm isolates these policies from each other.

The runner offers 599 background orders, one every six seconds, from the AM peak flow weights.
The hub overlay offers Waterloo to Bank at 54.78 passengers/min and Bank to Waterloo at 0.6 passengers/min in expectation.
Independent PCG streams set background destinations, hub arrivals, and uniform party sizes from 1 to 20.
All six configurations of each seed have identical recorded party IDs, sizes, endpoints, and offer ticks.
Compact arms split each hub party into private orders of at most four passengers.
Express submits one shared service order for the entire party, with a service pooling limit of 20 parties.
An Express hub order has no Compact fallback.
The overlay covers only Waterloo and Bank.

The runner prechecks that the total split schedule fits within 10,000 orders.
It submits directly with `SubmitTripOptions`, which does not enforce a live queue cap.
Thus 10,000 is a schedule guard, not a configured queue limit.
No submission failed, and all recorded background offers were accepted.
Skipped is zero for every arm and window.
This experiment avoids the earlier queue-200 loss, but does not test live queue loss at the default cap.

Startup means minutes 0 to 20, excluding minute 20.
The late window starts at minute 20 and includes the final tick at minute 60.
Rates count completions in each window, including parties offered before that window.
Passenger rates count completed order passengers, including partial Compact parties.
A whole party completes only when all its split orders complete.
Whole-party wait ends at its last boarding, and journey ends at its last completion.
Both start at the offered tick.
Wait means select parties whose last boarding falls in the window.
Journey means select parties whose last completion falls in the window.
They are different cohorts, so journey can be shorter than wait in a table cell.
`NA` means no observation, not zero time.

Matched background rows use the party identity, not the arm's request ID.
Window matches require the event in both arms within that window.
The full-cap table includes cross-window matches and supplies the individual-gate assessment.
Wait exceedances add more than max(30 seconds, 5% of reference wait).
Journey exceedances add more than max(60 seconds, 5% of reference journey).
The completed-only columns count completions in the named window whose counterpart never completes by the full cap.
They exclude counterparts that complete in a different window.
Other unmatched records remain censored and cannot pass a gate by omission.

## Layout attempts

The following table records the fixture report, not new generation runs.
For geometry refusals, the text is `invalid project scenario: nonincident paths "<first>" and "<second>" are less than 20 meters apart`.

| Attempt | Change | Reach-only pairs | First refusal |
| --- | --- | --- | --- |
| A1 | Dedicated straight Express routes and 16-berth parking | 26 | `xp-route-1-001` / `940GZZLUWLO-01-in` |
| A2 | Split routes into approximately 80 m lanes | 14 | `940GZZLUWLO-01-arrival-link` / `xp-route-2-084` |
| A3 | Extend gates 120 m and restrict mixed masks to berth paths | 6 | `london-link-100-ba-1` / `940GZZLUWLO-exit-xp-stub` |
| A4 | Split gate extensions and add Bank departure bend | 1 | `lane "940GZZLUWLO-entry-xp-stub-0" must be at least 40 meters long` |
| A5 | Use 41 m segments and approach Bank from south | 1 | `london-link-098-ba-2` / `940GZZLUBNK-entry-xp-stub-0` |
| A6 | Approach Bank from west | 0 | Pass |
| B | A6 plus Waterloo banks, 3+2 berths | 6 | `xp-route-0-018` / `940GZZLUWLO-b-route-out-016` |
| C | A6 plus Bank banks, 2+2 berths | 9 | `xp-route-1-028` / `940GZZLUBNK-b-route-out-029` |

A4's reach count and its first length refusal are separate checks.
B and C first refused missing entry-lane roles on new gate connections.
Correcting those roles exposed the geometry refusals without changing reach counts.
All final candidates had zero direct large-path refusals.
Waterloo banks remain refused with six reach pairs, and Bank banks with nine.
Project D was not generated because B and C failed.
B also lacked an Express parking-to-Bank route through banked Waterloo.

| Project | Load | Nodes added / removed | Lanes added / removed | JSON bytes | Blocks |
| --- | --- | --- | --- | --- | --- |
| A6 | Pass | 283 / 0 | 302 / 0 | 5,690,382 | 56,697 |
| B | Refused | 338 / 6 | 360 / 8 | 5,705,494 | 56,870 |
| C | Refused | 349 / 0 | 371 / 0 | 5,711,635 | 56,929 |

A6 has 6,023 nodes, 9,326 lanes, and 50,166 junction pairs.
The project byte limit is 33,554,432, and the block limit is 64,000.
A6 adds Express-only routes in both directions between Waterloo and Bank and between Waterloo and parking.
The routes use separate nominal planes, split lanes, gate extensions, and a Bank departure bend.
Existing hub berths and selected berth paths admit Compact and Express pods.
There is no per-service lane entitlement.

Parking is a separate Express-only, non-depot station with 16 berths.
Its entry is 300 m from the north depot entry.
From the parking exit, Waterloo entry is 7,063.6 m away.
Bank entry is 9,243.3 m away, through Waterloo.
These distances exclude travel inside parking and to a destination berth.
Static acceptance retains the validators and the reach rule.

## Service measurements

The following tables report every seed instead of pooled means.
Hub directions use WLO for Waterloo and BNK for Bank.
The two counts in each hub timing cell are whole parties boarded and completed.

### Startup hub service

| Arm | Policies | Seed | WLO→BNK passengers / parties per minute | BNK→WLO passengers / parties per minute | WLO→BNK wait / journey s (n/n) | BNK→WLO wait / journey s (n/n) |
| --- | --- | --- | --- | --- | --- | --- |
| baseline | off | 1 | 8.300 / 1.200 | 0.000 / 0.000 | 401.9 / 637.7 (37/24) | 74.9 / NA (1/0) |
| baseline | off | 2 | 9.450 / 1.000 | 0.000 / 0.000 | 480.4 / 619.8 (34/20) | NA / NA (0/0) |
| baseline | off | 3 | 9.950 / 0.750 | 0.000 / 0.000 | 547.8 / 713.7 (33/15) | NA / NA (0/0) |
| control | off | 1 | 8.050 / 1.100 | 0.000 / 0.000 | 416.5 / 620.5 (37/22) | 55.4 / NA (1/0) |
| control | off | 2 | 9.050 / 1.000 | 0.000 / 0.000 | 477.1 / 633.9 (33/20) | NA / NA (0/0) |
| control | off | 3 | 9.550 / 0.750 | 0.000 / 0.000 | 557.1 / 726.1 (33/15) | NA / NA (0/0) |
| express | off | 1 | 15.150 / 2.100 | 0.000 / 0.000 | 447.3 / 676.5 (54/42) | NA / NA (0/0) |
| express | off | 2 | 15.850 / 2.250 | 0.000 / 0.000 | 446.2 / 661.0 (57/45) | NA / NA (0/0) |
| express | off | 3 | 15.850 / 2.000 | 0.000 / 0.000 | 482.5 / 724.6 (50/40) | NA / NA (0/0) |
| baseline | on | 1 | 8.300 / 1.200 | 0.000 / 0.000 | 403.0 / 637.7 (37/24) | 74.9 / NA (1/0) |
| baseline | on | 2 | 9.450 / 1.000 | 0.000 / 0.000 | 479.8 / 619.8 (34/20) | NA / NA (0/0) |
| baseline | on | 3 | 9.950 / 0.750 | 0.000 / 0.000 | 544.8 / 711.6 (33/15) | NA / NA (0/0) |
| control | on | 1 | 8.050 / 1.100 | 0.000 / 0.000 | 416.3 / 620.5 (37/22) | 70.8 / NA (1/0) |
| control | on | 2 | 9.050 / 1.000 | 0.000 / 0.000 | 476.3 / 633.9 (33/20) | NA / NA (0/0) |
| control | on | 3 | 9.550 / 0.750 | 0.000 / 0.000 | 562.1 / 725.4 (33/15) | NA / NA (0/0) |
| express | on | 1 | 15.150 / 2.100 | 0.000 / 0.000 | 447.3 / 676.5 (54/42) | NA / NA (0/0) |
| express | on | 2 | 15.850 / 2.200 | 0.000 / 0.000 | 454.3 / 667.9 (58/44) | NA / NA (0/0) |
| express | on | 3 | 15.850 / 2.000 | 0.000 / 0.000 | 488.3 / 724.6 (51/40) | NA / NA (0/0) |

### Startup background

| Arm | Policies | Seed | Offered | Completed | Skipped | Wait / journey s | Boarded / completed n |
| --- | --- | --- | --- | --- | --- | --- | --- |
| baseline | off | 1 | 199 | 58 | 0 | 116.0 / 514.5 | 126 / 58 |
| baseline | off | 2 | 199 | 46 | 0 | 139.4 / 555.4 | 119 / 46 |
| baseline | off | 3 | 199 | 56 | 0 | 164.5 / 615.0 | 129 / 56 |
| control | off | 1 | 199 | 57 | 0 | 117.2 / 518.0 | 126 / 57 |
| control | off | 2 | 199 | 46 | 0 | 142.2 / 556.3 | 119 / 46 |
| control | off | 3 | 199 | 55 | 0 | 167.0 / 611.4 | 129 / 55 |
| express | off | 1 | 199 | 79 | 0 | 47.3 / 452.3 | 185 / 79 |
| express | off | 2 | 199 | 72 | 0 | 53.1 / 497.8 | 180 / 72 |
| express | off | 3 | 199 | 86 | 0 | 52.0 / 505.1 | 177 / 86 |
| baseline | on | 1 | 199 | 58 | 0 | 120.8 / 515.1 | 129 / 58 |
| baseline | on | 2 | 199 | 47 | 0 | 145.6 / 557.1 | 124 / 47 |
| baseline | on | 3 | 199 | 57 | 0 | 160.2 / 609.2 | 128 / 57 |
| control | on | 1 | 199 | 57 | 0 | 120.0 / 518.5 | 129 / 57 |
| control | on | 2 | 199 | 47 | 0 | 144.7 / 556.9 | 123 / 47 |
| control | on | 3 | 199 | 54 | 0 | 164.9 / 613.0 | 131 / 54 |
| express | on | 1 | 199 | 79 | 0 | 47.3 / 452.3 | 185 / 79 |
| express | on | 2 | 199 | 72 | 0 | 51.2 / 496.1 | 179 / 72 |
| express | on | 3 | 199 | 86 | 0 | 52.8 / 504.7 | 175 / 86 |

### Minutes 20 to 60 hub service

| Arm | Policies | Seed | WLO→BNK passengers / parties per minute | BNK→WLO passengers / parties per minute | WLO→BNK wait / journey s (n/n) | BNK→WLO wait / journey s (n/n) |
| --- | --- | --- | --- | --- | --- | --- |
| baseline | off | 1 | 12.125 / 0.875 | 0.225 / 0.025 | 1613.8 / 1813.1 (66/35) | NA / 1099.7 (0/1) |
| baseline | off | 2 | 12.500 / 1.175 | 0.000 / 0.000 | 1564.6 / 1917.2 (75/47) | NA / NA (0/0) |
| baseline | off | 3 | 11.400 / 1.150 | 0.000 / 0.000 | 1549.2 / 1919.8 (68/46) | NA / NA (0/0) |
| control | off | 1 | 11.925 / 0.825 | 0.225 / 0.025 | 1602.2 / 1747.6 (62/33) | NA / 1070.3 (0/1) |
| control | off | 2 | 11.125 / 1.125 | 0.000 / 0.000 | 1591.1 / 1903.1 (74/45) | NA / NA (0/0) |
| control | off | 3 | 11.175 / 1.025 | 0.000 / 0.000 | 1552.6 / 1883.8 (67/41) | NA / NA (0/0) |
| express | off | 1 | 38.125 / 3.600 | 0.775 / 0.100 | 857.1 / 1024.6 (146/144) | 535.4 / 815.9 (4/4) |
| express | off | 2 | 38.475 / 4.000 | 0.000 / 0.000 | 815.9 / 992.2 (161/160) | NA / NA (0/0) |
| express | off | 3 | 37.825 / 3.900 | 0.000 / 0.000 | 864.4 / 1043.9 (163/156) | 307.9 / NA (1/0) |
| baseline | on | 1 | 11.550 / 0.800 | 0.225 / 0.025 | 1553.6 / 1809.7 (62/32) | NA / 1059.0 (0/1) |
| baseline | on | 2 | 9.925 / 1.100 | 0.000 / 0.000 | 1564.6 / 1855.5 (74/44) | NA / NA (0/0) |
| baseline | on | 3 | 8.000 / 0.850 | 0.000 / 0.000 | 1553.2 / 1803.3 (67/34) | NA / NA (0/0) |
| control | on | 1 | 10.450 / 0.750 | 0.225 / 0.025 | 1612.4 / 1620.4 (65/30) | NA / 1093.2 (0/1) |
| control | on | 2 | 9.700 / 0.975 | 0.000 / 0.000 | 1594.1 / 1862.2 (76/39) | NA / NA (0/0) |
| control | on | 3 | 9.500 / 0.900 | 0.000 / 0.000 | 1561.8 / 1904.0 (67/36) | NA / NA (0/0) |
| express | on | 1 | 38.125 / 3.575 | 0.275 / 0.050 | 872.3 / 1041.7 (145/143) | 732.4 / 696.7 (4/2) |
| express | on | 2 | 38.375 / 4.025 | 0.000 / 0.000 | 810.4 / 983.3 (161/161) | NA / NA (0/0) |
| express | on | 3 | 37.775 / 3.875 | 0.000 / 0.000 | 866.2 / 1042.8 (165/155) | NA / NA (0/0) |

### Minutes 20 to 60 background

| Arm | Policies | Seed | Offered | Completed | Skipped | Wait / journey s | Boarded / completed n |
| --- | --- | --- | --- | --- | --- | --- | --- |
| baseline | off | 1 | 400 | 151 | 0 | 1196.3 / 1448.3 | 125 / 151 |
| baseline | off | 2 | 400 | 153 | 0 | 1246.7 / 1465.4 | 115 / 153 |
| baseline | off | 3 | 400 | 146 | 0 | 1266.8 / 1427.6 | 109 / 146 |
| control | off | 1 | 400 | 154 | 0 | 1142.5 / 1465.4 | 116 / 154 |
| control | off | 2 | 400 | 158 | 0 | 1261.4 / 1495.9 | 114 / 158 |
| control | off | 3 | 400 | 143 | 0 | 1319.0 / 1413.3 | 103 / 143 |
| express | off | 1 | 400 | 375 | 0 | 169.8 / 826.2 | 366 / 375 |
| express | off | 2 | 400 | 374 | 0 | 175.1 / 824.9 | 389 / 374 |
| express | off | 3 | 400 | 352 | 0 | 175.4 / 818.1 | 384 / 352 |
| baseline | on | 1 | 400 | 160 | 0 | 1080.0 / 1422.0 | 129 / 160 |
| baseline | on | 2 | 400 | 161 | 0 | 1216.1 / 1458.5 | 122 / 161 |
| baseline | on | 3 | 400 | 153 | 0 | 1211.6 / 1437.6 | 112 / 153 |
| control | on | 1 | 400 | 164 | 0 | 1062.0 / 1415.6 | 127 / 164 |
| control | on | 2 | 400 | 158 | 0 | 1258.0 / 1442.5 | 124 / 158 |
| control | on | 3 | 400 | 152 | 0 | 1214.3 / 1406.9 | 104 / 152 |
| express | on | 1 | 400 | 376 | 0 | 173.2 / 824.8 | 371 / 376 |
| express | on | 2 | 400 | 373 | 0 | 173.4 / 829.3 | 390 / 373 |
| express | on | 3 | 400 | 351 | 0 | 177.4 / 824.9 | 389 / 351 |

### Startup matched background

| Comparison | Policies | Seed | Wait n: seconds | Wait exceedances | Journey n: seconds | Journey exceedances | Completed only first / second |
| --- | --- | --- | --- | --- | --- | --- | --- |
| baseline→control | off | 1 | 124: 115.3→114.1 | 1 | 57: 517.4→518.0 | 0 | 0 / 0 |
| baseline→control | off | 2 | 117: 137.5→138.8 | 3 | 46: 555.4→556.3 | 0 | 0 / 0 |
| baseline→control | off | 3 | 127: 161.2→163.9 | 3 | 55: 607.7→611.4 | 1 | 0 / 0 |
| baseline→express | off | 1 | 126: 116.0→33.9 | 0 | 57: 511.2→448.5 | 0 | 0 / 1 |
| baseline→express | off | 2 | 119: 139.4→34.6 | 1 | 46: 555.4→497.3 | 0 | 0 / 0 |
| baseline→express | off | 3 | 129: 164.5→42.6 | 0 | 56: 615.0→532.6 | 0 | 0 / 0 |
| control→express | off | 1 | 126: 117.2→34.6 | 0 | 56: 514.7→451.9 | 0 | 0 / 0 |
| control→express | off | 2 | 119: 142.2→33.2 | 1 | 46: 556.3→497.3 | 0 | 0 / 0 |
| control→express | off | 3 | 129: 167.0→44.9 | 0 | 55: 611.4→530.5 | 0 | 0 / 1 |
| baseline→control | on | 1 | 124: 113.7→114.3 | 2 | 57: 517.7→518.5 | 0 | 0 / 0 |
| baseline→control | on | 2 | 119: 139.7→140.1 | 2 | 47: 557.1→556.9 | 0 | 0 / 0 |
| baseline→control | on | 3 | 125: 156.4→161.8 | 5 | 54: 608.0→613.0 | 2 | 0 / 0 |
| baseline→express | on | 1 | 129: 120.8→34.7 | 0 | 57: 511.8→448.5 | 0 | 0 / 0 |
| baseline→express | on | 2 | 124: 145.6→34.3 | 1 | 47: 557.1→496.0 | 0 | 0 / 0 |
| baseline→express | on | 3 | 128: 160.2→43.4 | 0 | 57: 609.2→530.7 | 0 | 0 / 1 |
| control→express | on | 1 | 129: 120.0→35.0 | 0 | 56: 515.2→451.9 | 0 | 0 / 1 |
| control→express | on | 2 | 123: 144.7→35.5 | 1 | 47: 556.9→496.0 | 0 | 0 / 1 |
| control→express | on | 3 | 131: 164.9→45.0 | 0 | 54: 613.0→535.0 | 0 | 0 / 2 |

### Minutes 20 to 60 matched background

| Comparison | Policies | Seed | Wait n: seconds | Wait exceedances | Journey n: seconds | Journey exceedances | Completed only first / second |
| --- | --- | --- | --- | --- | --- | --- | --- |
| baseline→control | off | 1 | 109: 1132.1→1141.0 | 36 | 145: 1419.2→1447.0 | 34 | 6 / 8 |
| baseline→control | off | 2 | 105: 1229.1→1264.6 | 39 | 147: 1446.8→1471.5 | 29 | 6 / 11 |
| baseline→control | off | 3 | 94: 1248.2→1325.9 | 52 | 130: 1356.8→1405.2 | 37 | 16 / 12 |
| baseline→express | off | 1 | 70: 1400.6→132.0 | 0 | 130: 1496.3→957.2 | 0 | 0 / 244 |
| baseline→express | off | 2 | 58: 1394.4→206.4 | 0 | 127: 1519.8→918.3 | 0 | 0 / 247 |
| baseline→express | off | 3 | 66: 1316.1→166.0 | 0 | 116: 1526.3→932.9 | 0 | 0 / 236 |
| control→express | off | 1 | 59: 1347.3→133.5 | 0 | 131: 1514.9→960.5 | 0 | 0 / 243 |
| control→express | off | 2 | 56: 1419.3→231.5 | 1 | 132: 1559.0→911.1 | 1 | 0 / 242 |
| control→express | off | 3 | 58: 1410.9→168.4 | 1 | 113: 1502.8→896.0 | 0 | 0 / 239 |
| baseline→control | on | 1 | 119: 1070.3→1056.4 | 38 | 156: 1401.9→1386.7 | 26 | 4 / 7 |
| baseline→control | on | 2 | 109: 1196.7→1247.7 | 45 | 148: 1417.8→1415.7 | 32 | 13 / 10 |
| baseline→control | on | 3 | 97: 1185.9→1205.4 | 35 | 142: 1393.5→1375.3 | 31 | 11 / 7 |
| baseline→express | on | 1 | 73: 1247.6→143.2 | 0 | 138: 1471.9→937.4 | 0 | 0 / 237 |
| baseline→express | on | 2 | 67: 1413.3→221.7 | 0 | 136: 1512.7→924.1 | 0 | 0 / 237 |
| baseline→express | on | 3 | 66: 1393.7→157.1 | 0 | 125: 1530.0→897.5 | 0 | 0 / 226 |
| control→express | on | 1 | 71: 1259.9→134.9 | 0 | 142: 1466.9→941.1 | 0 | 0 / 233 |
| control→express | on | 2 | 69: 1464.4→200.3 | 1 | 134: 1496.3→919.4 | 1 | 0 / 239 |
| control→express | on | 3 | 61: 1356.4→171.8 | 0 | 122: 1513.9→923.5 | 0 | 0 / 229 |

### Full cap matched background

| Comparison | Policies | Seed | Wait n: seconds | Wait exceedances | Journey n: seconds | Journey exceedances | Completed only first / second |
| --- | --- | --- | --- | --- | --- | --- | --- |
| baseline→control | off | 1 | 237: 587.8→590.9 | 39 | 203: 1160.7→1182.2 | 35 | 6 / 8 |
| baseline→control | off | 2 | 226: 652.5→668.9 | 44 | 193: 1234.3→1253.4 | 29 | 6 / 11 |
| baseline→control | off | 3 | 225: 622.1→657.2 | 57 | 186: 1133.5→1169.2 | 39 | 16 / 12 |
| baseline→express | off | 1 | 251: 654.0→71.2 | 0 | 209: 1189.2→767.6 | 0 | 0 / 245 |
| baseline→express | off | 2 | 234: 683.6→92.0 | 1 | 199: 1255.0→766.1 | 0 | 0 / 247 |
| baseline→express | off | 3 | 238: 669.3→82.1 | 0 | 202: 1202.3→750.8 | 0 | 0 / 236 |
| control→express | off | 1 | 242: 608.6→68.7 | 0 | 211: 1209.4→769.1 | 0 | 0 / 243 |
| control→express | off | 2 | 233: 689.8→96.7 | 2 | 204: 1284.0→765.2 | 1 | 0 / 242 |
| control→express | off | 3 | 232: 678.5→80.2 | 1 | 198: 1190.5→728.8 | 0 | 0 / 240 |
| baseline→control | on | 1 | 253: 576.4→573.4 | 45 | 214: 1161.5→1152.0 | 27 | 4 / 7 |
| baseline→control | on | 2 | 237: 638.7→668.4 | 52 | 195: 1210.3→1208.7 | 32 | 13 / 10 |
| baseline→control | on | 3 | 231: 603.4→609.4 | 43 | 199: 1168.9→1159.2 | 36 | 11 / 7 |
| baseline→express | on | 1 | 258: 600.4→74.5 | 0 | 218: 1180.7→760.6 | 0 | 0 / 237 |
| baseline→express | on | 2 | 246: 676.5→97.6 | 1 | 208: 1254.8→776.0 | 0 | 0 / 237 |
| baseline→express | on | 3 | 240: 650.9→81.0 | 0 | 210: 1212.8→739.1 | 0 | 0 / 227 |
| control→express | on | 1 | 256: 587.3→71.6 | 0 | 221: 1184.2→767.5 | 0 | 0 / 234 |
| control→express | on | 2 | 247: 703.6→93.1 | 2 | 205: 1239.5→772.8 | 1 | 0 / 240 |
| control→express | on | 3 | 235: 629.3→83.1 | 0 | 206: 1198.8→753.8 | 0 | 0 / 231 |

## Motion and timing

| Arm | Policies | Seed | Step s | Oracle s | Runner wall s | Express empty km startup / late | All blocked pod-s startup / late | Express blocked pod-s startup / late | Compact by Express pod-s startup / late |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| baseline | off | 1 | 76.3 | 44.5 | 126.5 | 0.00 / 0.00 | 32483 / 419023 | 0 / 0 | 0 / 0 |
| baseline | off | 2 | 65.8 | 48.1 | 119.5 | 0.00 / 0.00 | 41113 / 414246 | 0 / 0 | 0 / 0 |
| baseline | off | 3 | 70.0 | 50.9 | 126.3 | 0.00 / 0.00 | 44273 / 435739 | 0 / 0 | 0 / 0 |
| control | off | 1 | 80.2 | 43.7 | 128.4 | 0.00 / 0.00 | 34052 / 429020 | 0 / 0 | 0 / 0 |
| control | off | 2 | 78.1 | 49.1 | 131.7 | 0.00 / 0.00 | 41596 / 427669 | 0 / 0 | 0 / 0 |
| control | off | 3 | 71.5 | 48.2 | 123.9 | 0.00 / 0.00 | 45441 / 434020 | 0 / 0 | 0 / 0 |
| express | off | 1 | 34.6 | 1009.2 | 1048.0 | 167.38 / 246.24 | 1861 / 5425 | 1448 / 2280 | 259 / 980 |
| express | off | 2 | 34.0 | 1006.4 | 1044.8 | 170.32 / 252.21 | 2210 / 4954 | 1345 / 2227 | 443 / 768 |
| express | off | 3 | 34.0 | 1002.9 | 1041.3 | 164.02 / 249.99 | 2694 / 5996 | 2008 / 2689 | 319 / 821 |
| baseline | on | 1 | 81.5 | 48.2 | 135.4 | 0.00 / 0.00 | 33380 / 434177 | 0 / 0 | 0 / 0 |
| baseline | on | 2 | 68.1 | 52.1 | 125.8 | 0.00 / 0.00 | 42017 / 440411 | 0 / 0 | 0 / 0 |
| baseline | on | 3 | 77.1 | 50.9 | 133.6 | 0.00 / 0.00 | 45508 / 453499 | 0 / 0 | 0 / 0 |
| control | on | 1 | 81.9 | 44.7 | 131.0 | 0.00 / 0.00 | 33539 / 446642 | 0 / 0 | 0 / 0 |
| control | on | 2 | 78.3 | 51.1 | 133.7 | 0.00 / 0.00 | 42434 / 446822 | 0 / 0 | 0 / 0 |
| control | on | 3 | 77.8 | 49.5 | 131.5 | 0.00 / 0.00 | 45409 / 466009 | 0 / 0 | 0 / 0 |
| express | on | 1 | 38.7 | 1010.2 | 1053.6 | 167.38 / 248.62 | 1849 / 5491 | 1448 / 2402 | 259 / 910 |
| express | on | 2 | 36.4 | 984.4 | 1025.6 | 170.66 / 251.88 | 2075 / 5556 | 1242 / 2294 | 372 / 701 |
| express | on | 3 | 36.6 | 970.0 | 1011.5 | 163.88 / 251.88 | 2579 / 6365 | 1981 / 2604 | 267 / 1097 |

## Parking and startup

| Policies | Seed | First to last departure sample s | First hub boarding s | First hub completion s | First zero parking sample min | Occupancy increases | Express parties completed 0 to 60 | Express passengers completed 0 to 60 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| off | 1 | 14.0–182.0 | 562.6 | 781.1 | 4.00 | [] | 190 | 1859 |
| off | 2 | 1.0–176.0 | 549.3 | 773.1 | 3.00 | [] | 205 | 1856 |
| off | 3 | 26.0–191.0 | 574.8 | 793.2 | 4.00 | [] | 196 | 1830 |
| on | 1 | 14.0–182.0 | 562.6 | 781.1 | 4.00 | [] | 187 | 1839 |
| on | 2 | 1.0–176.0 | 549.3 | 773.1 | 3.00 | [] | 205 | 1852 |
| on | 3 | 26.0–191.0 | 574.8 | 793.2 | 4.00 | [] | 195 | 1828 |

The motion table reports step and oracle time over the full hour.
The logs do not split these timers at minute 20, so startup and late step or oracle times cannot be computed separately.
These elapsed timers do not establish paired CPU-cost gates.
They exclude submission and most logging work.
The Express oracle takes 970.0 to 1010.2 seconds, against 43.7 to 52.1 seconds in the Compact configurations.
Express simulation stepping takes 34.0 to 38.7 seconds, against 65.8 to 81.9 seconds in the Compact configurations.
The changed completed workload and unrecorded host load prevent an equal-work cost claim.

Empty distance sums one-second speed samples for pods whose `Occupied` flag is false.
The minute-20 metric includes its boundary sample, and the late value subtracts that cumulative metric from the final metric.
Distances and blocked pod-seconds therefore have about one sample of boundary uncertainty.
Loaded distance is not logged and cannot be reconstructed from order timestamps or empty distance.
No table substitutes passenger route distance for pod loaded distance.
Express completes 40 to 45 parties carrying 303 to 317 passengers during startup.
It completes 145 to 161 parties carrying 1511 to 1556 passengers during the late window.
The hub tables give those completions by direction and seed.

All six Express arms start with 16 parked pods, and all 16 start moving.
Parking occupancy falls to zero by the minute-3 or minute-4 report and stays zero in every later minute report.
It is zero throughout the reported late window.
The baseline and control have zero Express parking occupancy throughout.
No minute sample shows a return to parking.
A return between reports cannot be excluded, because the runner prints occupancy only each minute.
The logs do not report pod destinations, parking reservations, or instantaneous blocker locations.
They cannot establish whether a pod attempted a return and became blocked en route.

Blocker counts sum one-second observations and can count the same pod under different blockers over time.
They do not measure uninterrupted blockage or prove deadlock.
The largest full-run pair totals, seeds 1 / 2 / 3, are 242 / 196 / 214 pod-seconds with policies off.
They are 221 / 242 / 178 pod-seconds with policies on.
These pairs all involve one Express pod blocked by another Express pod.
Compact pods also report Express blockers, as the motion table shows.
The samples do not locate those events at parking or at a shared hub berth.

## Background harm and accounting

With policies on, Express completes 455 / 445 / 437 background orders over the full hour.
Baseline completes 218 / 208 / 210, and control completes 221 / 205 / 206.
Every background order completed by either reference also completes in the corresponding Express arm by the cap.
This is a finite-window result, not a recovery or sustained-capacity qualification.

Seed 2 background party 240, Bank to Piccadilly Circus, exceeds the pickup gate against both references with policies on and off.
With policies on, baseline request 401 waits 3.53 seconds, control request 401 waits 5.47 seconds, and Express request 241 waits 88.57 seconds.
That adds 85.03 seconds against baseline, beyond the 30-second allowance.
The party boards during startup, so late-window matching alone hides this failure.

Against control, seed 2 party 381, Bethnal Green to Hainault, also exceeds the pickup and journey gates under both policy settings.
With policies on, wait increases from 362.37 to 500.63 seconds and journey from 971.40 to 1109.67 seconds.
With policies off, seed 3 party 661, Bank to Chancery Lane, also exceeds the pickup gate against control.
Its wait increases from 30.70 to 66.10 seconds.
No full-cap Express journey exceeds the individual gate against baseline.
The control itself exceeds individual gates against baseline in every seed and policy setting.
The tables retain this geometry cost instead of treating control as equivalent to baseline.

The analysis found contiguous unique request IDs, matching offer records, and order sizes that sum to each offered party size.
Recorded party counts equal the offered counts in all 18 log headers.
Every recorded completion has a recorded boarding, with offer, boarding, and completion ticks in order.
The runner rejects an unaccounted completion and stops at the first oracle error.
All 18 logs end normally at tick 108,000 with `oracle-failures=0`.
No safety or accounting failure is reported.
The runner does not compare every live request against a conservation ledger, and repeated completions can overwrite the same record.
It does not preserve completion-event multiplicity in the output.
These checks cannot rule out every lost or duplicate order.

## Comparison with the earlier London screen

The [earlier screen](express-london.md) used `be7cff2e` plus scratch commits, removed the folded Bank movement, and bypassed reach refusal.
Its 16-pod `more` arm reported 10 to 15 failing ticks per seed, with gaps down to 17.73 m.
A6 passes static validation without that bypass and has zero observed oracle failures in these runs.
This does not prove safety beyond the tested motion.

Earlier Waterloo to Bank throughput was 31.4 to 32.1 passengers/min and 3.12 to 3.38 whole parties/min for `more`.
A6 with policies off gives 37.825 to 38.475 passengers/min and 3.600 to 4.000 whole parties/min.
Those figures are not an isolated estimate of the geometry change.
The earlier layout enlarged Waterloo to 20 berths and started Express pods at hub berths.
A6 uses the original hub berth counts, dedicated routes, and remote parking.
Its Bank movement also differs because of the generator fix.
The earlier overlay included London Bridge and Canary Wharf, which this runner omits.

The earlier queue limit was 200, with order-count losses that penalized split Compact parties.
This runner has no live queue cap and records no skipped orders.
The earlier background schedules therefore cannot be treated as matched merely because both have 599 orders.
Their shared PCG construction does not establish an execution bridge across layouts and overlay changes.
The earlier policies were off, while the current-policy decision uses both policies on.
The off arms here give a closer policy comparison, but still change geometry, parking, and queue handling.
Earlier waits, background counts, empty distances, and throughput ratios cannot isolate an Express fleet effect in A6.
A6's empty-distance sample includes the long parking departure, and its logs have no loaded-distance counterpart to the earlier distance table.

## Runner limits that affect interpretation

- `SetDemandRate(10)` supplies the guarded redistribution gate with the background rate, despite the additional hub overlay and Compact splitting.
  These policy-on runs therefore do not establish behavior with a controller rate derived from total offered orders.
- The 10,000-order check is not a live queue setting.
  Zero skipped offers cannot qualify behavior at the default queue limit.
- Boarding timestamps come from one-second rider snapshots, while completions come from per-step events.
  A boarding absent from every sampled snapshot would remain zero.
  Every recorded completed order has a nonzero boarding in these logs, but incomplete orders have no independent boarding ledger.
- Empty distance and blockers are sampled, and occupancy is printed only each minute.
  Loaded distance, return destinations, and blocker locations are absent.
- Source and binary hashes were not captured independently in the run logs.
  Current checkout identity and the project hash do not prove which executable produced each log.
- The logs do not establish execution isolation or host load.
  Timing comparisons do not satisfy CPU qualification gates.

## Commands

The fixture report records this runner command shape for the 18 runs.
The completion ledger establishes that all 18 named arms finished, but does not preserve the batch launcher itself.
This report did not rerun generation or motion.

```bash
cd /tmp/build/podsim/wt/xfixture
export GOTOOLCHAIN=local GOPROXY=off GOMAXPROCS=2
export GOCACHE=/tmp/build/podsim/cache-xfixture
export GOTMPDIR="$GOCACHE/tmp" TMPDIR="$GOCACHE/tmp"
ulimit -v 16000000
GO=/home/dotwaffle/.local/share/mise/installs/go/1.27.3/bin/go
PROJECT="$GOCACHE/out/A-attempt-6.json"
RESULTS=tools/study-express-fixture/results
for configuration in baseline control express; do
  for policies in false true; do
    for seed in 1 2 3; do
      timeout --kill-after=5s 3600s nice -n 10 \
        "$GO" run -p 1 ./tools/study-express-fixture \
        -run "$PROJECT" -configuration "$configuration" \
        -policies="$policies" -seed "$seed" -duration 60m \
        >"$RESULTS/$configuration-$policies-$seed.log" 2>&1
    done
  done
done
```

The generator accepts `-attempt 6 -variant A -out /tmp/build/podsim/cache-xfixture/out` in place of the motion flags.
The report analysis parsed JSON lines, grouped orders by `Offer.Party`, and applied the window and matching rules above.
These report commands computed the project hash and generated the tables from the finished logs.
The Python scripts were temporary analysis files in the allowed cache directory.

```bash
sha256sum /tmp/build/podsim/cache-xfixture/out/A-attempt-6.json
python3 /tmp/build/podsim/cache-xfixture/analyze.py
python3 /tmp/build/podsim/cache-xfixture/inspect.py
/home/dotwaffle/.local/share/mise/installs/rumdl/0.3.2/rumdl check \
  docs/studies/express-hub-fixture.md README.md
git diff --check
```

## Not checked

No arm ran twice, so reproducibility was not checked.
No full request-conservation audit, parking-return trial, or parking-deadlock qualification ran.
Loaded distance and startup-versus-late step and oracle timers are unavailable.
The runs do not check other demand bands, seeds 4 to 10, six-hour backlog, recovery drainage, or a station capacity limit.
They do not isolate redistribution from pickup reassignment or test the default live queue cap.
No new fault, emergency, save, restore, stream, browser, shared-detour, or byte-proof qualification ran.
No controlled CPU, resident-memory, GC-pause, or network-rate qualification ran.
This document change did not run the Go, race, or web suites.

## Follow-up

These runs use project A at seed 1 with policies on, unless a line says otherwise.

**Reproducibility.**
A rerun with the same binary matches all 909 order records byte for byte.
A run at `GOMAXPROCS=1` matches them too.
The 61 state and metric records match in value, and 59 differ in the JSON key order of a map in the scratch logger.

**Conservation.**
All 630 checkpoints, at a stride of 300 ticks, pass, and so does the final check.
At minute 60 the balance is `909 = 642 completed + 0 interrupted + 150 queued + 117 aboard`.
At minute 105 it is `909 = 908 completed + 0 interrupted + 0 queued + 1 aboard`, and all 3,241 hub passengers have completed.

**Return to parking.**
Arrivals stop at minute 60, and the run goes on to minute 150.
The hub backlog is 1,244 passengers at minute 60, and it drains by minute 105.
Parking occupancy is 1 at minute 104, 8 at minute 108, and 14 at minute 112, and it stays at 14 until minute 150.
The oracle has no failure in 270,000 ticks, and no deadlock occurs.
At minute 105, 2 pods stand idle at Bank berths with no wait reason and no blocker.
The 45-minute run shows these 2 pods, and the 90-minute run was not traced per pod, so it is an inference that the same 2 stay out at minute 150.
No general rule sends an idle Express pod home.
`clearBlockedBerths` moves an idle pod only when another pod needs its berth.

**Party 240.**
At seed 2 this party, Bank to Piccadilly Circus, waits 88.57 s against 3.53 s in the baseline and 5.47 s in the control.
Dispatch sends an empty Compact pod from Mansion House, and Express 12 and Express 14 block it at the entrance of Bank.
Another Compact pod then finishes unloading at Bank and takes the order.
This is an interaction with Express traffic, and it is the only such exceedance in these seeds.
Every other matched wait equals or improves on the baseline.

| Seed | Matched | Baseline p95 and p99 (s) | Express p95 and p99 (s) | Exceedances |
|---|---|---|---|---|
| 1 | 258 | 1,785, 2,121 | 303, 473 | 0 |
| 2 | 246 | 1,898, 2,143 | 445, 821 | 1 |
| 3 | 240 | 1,922, 2,200 | 274, 477 | 0 |

The commands were scratch code that did not land, with the options `-drain` and `-trace-party-240`.
Not checked: a parking run at another seed, unlogged internal state, and an unmatched censored wait.

## Banks on the fixture

The maintainer asked for banks at Waterloo or Bank before qualification.
All validators stay on, and the 20 m rule does not change.
Project A does not change in this section, and the fleet placements do not change.

| Attempt | Change | Reach-only pairs | Result |
|---|---|---|---|
| B6 | The first Waterloo banks | 6 | Refused |
| C6 | The first Bank banks | 9 | Refused |
| B7, B8 | Shared trunks and a connection from parking to Bank | 0 | Pass, with no route from Bank to parking |
| C7 | Shared trunks | 3 | Refused |
| C8, C9 | A northward entry bend of 240 m at Bank | 0 | Pass |
| D8 | Both bank layouts | 0 | Pass, with no route from Bank to parking |
| B9, D9 | A return connection to parking | 0 | Pass |

| Project | Banks | Nodes added, removed | Lanes added, removed | Blocks of 64,000 | Bytes |
|---|---|---|---|---|---|
| A | None | 0, 0 | 0, 0 | 56,697 | 5,690,382 |
| B | Waterloo | 34, 6 | 39, 8 | 56,815 | 5,699,630 |
| C | Bank | 27, 0 | 30, 0 | 56,816 | 5,699,962 |
| D | Both | 61, 6 | 69, 8 | 56,934 | 5,709,210 |

These pilots use AM demand, policies on, seed 1, 60 simulated minutes, and the oracle at each tick.
The offers match the 909 records of A.
Hub rates cover minutes 20 to 60, and the other counts cover all 60 minutes.

| Project | Waterloo to Bank passengers/min | Background completed | Express pods started | Express parties completed | Oracle failures |
|---|---|---|---|---|---|
| A | 38.1 | 455 | 16 | 187 | 0 |
| B | 42.0 | 451 | 16 | 206 | 0 |
| C | 38.1 | 455 | 16 | 188 | 0 |
| D | 42.0 | 454 | 16 | 203 | 0 |

These flood runs use 20 orders/min to one station, seed 1, a queue cap of 200, 30 simulated minutes, and the oracle at each tick.
The rates cover minutes 20 to 30 only, which is a shorter window than `docs/studies/banks-busiest.md` uses.

| Station | Project A entries/min | With banks | Earlier study, minutes 20 to 60 |
|---|---|---|---|
| Waterloo | 6.80 | 8.50 in B and D | 6.53 to 8.32 |
| Bank | 5.70 | 9.50 in C | 5.50 to 8.47 |

The Bank gain includes a change from 2 berths to 4.
Banks at Waterloo add about 10 percent to the AM hub rate at one seed, and banks at Bank add none.
Not checked: other seeds, the individual gates, conservation, reproducibility, and the parking drain on the new layouts, save and restore, and whether the land and the flyovers are feasible.

## Screen of project D

Project D has banks at Waterloo and Bank, using `D-attempt-9.json` from "Banks on the fixture."
This screen uses policies on, seeds 1 to 3, 60 minutes of arrivals, and the same 327 Compact placements.
Express adds 16 Express pods, and control removes them while retaining D's layout.
Baseline uses the earlier `baseline-true-1.log` through `baseline-true-3.log` runs.
The schedules contain 909, 939, and 910 offered parties.
Their identities, ticks, endpoints, and sizes match across baseline, D control, D Express, and A Express.
Each schedule includes 599 background riders.
All eight D runs exit with 0.
The run logs and the code that makes them were scratch and are deleted.
Control wall seconds are 135 / 133 / 132, and Express wall seconds are 1138 / 1137 / 1129.
The repeat takes 1137 seconds, and the drain takes 3686 seconds.

Rates below cover minutes 20 to 60, including the final tick.
Passenger rates include completed parts of split Compact parties.
A whole party completes only when every part completes.
Completions can belong to parties offered before minute 20.

| Seed | Arm | Waterloo to Bank passengers/min | Waterloo to Bank whole parties/min | Bank to Waterloo passengers/min | Background completed |
| --- | --- | --- | --- | --- | --- |
| 1 | baseline | 11.550 | 0.800 | 0.225 | 160 |
| 1 | control | 11.125 | 0.775 | 0.225 | 164 |
| 1 | express | 41.950 | 3.975 | 0.275 | 376 |
| 2 | baseline | 9.925 | 1.100 | 0.000 | 161 |
| 2 | control | 11.450 | 1.000 | 0.000 | 159 |
| 2 | express | 41.575 | 4.350 | 0.000 | 375 |
| 3 | baseline | 8.000 | 0.850 | 0.000 | 153 |
| 3 | control | 8.850 | 0.950 | 0.000 | 148 |
| 3 | express | 42.675 | 4.425 | 0.000 | 350 |

Express improves Waterloo to Bank passenger and whole-party throughput against baseline and control in every seed.
Reverse-direction service remains sparse, with no late completion in seeds 2 and 3.
This does not establish a reverse-direction gain.

The individual pickup gate permits additional wait of at most `max(30 seconds, 5% of baseline wait)`.
The individual journey gate permits additional time of at most `max(60 seconds, 5% of baseline journey)`.
Both times start at the offered tick.
The following comparisons use all matched background events through minute 60, including cross-window matches.
Using only matches inside minutes 20 to 60 would omit some exceedances.

| Seed | Against baseline | Matched waits | Wait exceedances | Matched journeys | Journey exceedances |
| --- | --- | --- | --- | --- | --- |
| 1 | control | 249 | 57 | 213 | 41 |
| 1 | express | 258 | 0 | 218 | 0 |
| 2 | control | 234 | 51 | 197 | 35 |
| 2 | express | 246 | 0 | 208 | 0 |
| 3 | control | 231 | 44 | 197 | 32 |
| 3 | express | 240 | 0 | 210 | 0 |

No D Express rider exceeds either individual limit in the matched background cohort.
Seed 2 party 240, request 241, boards immediately in D, against 3.53 seconds in baseline and 88.57 seconds in A.
Its D journey takes 516.80 seconds against 1168.00 seconds in baseline.
The A screen's pickup exception is therefore not needed for D.
The logs do not trace this rider's D dispatch path, so they do not establish which layout change removes the delay.

The table below names every control rider who exceeds a gate, using the schedule's party ID.
Each background party has one rider.
For every listed pickup exceedance, the log establishes later boarding.
It does not record the dispatch or vehicle history needed to explain the delay.
For every listed journey exceedance, the log establishes later completion but does not identify its cause.
Some riders exceed both gates.
These control logs contain no per-rider trace, so no individual mechanism can be assigned to any listed rider.

| Seed | Control gate | Party IDs |
| --- | --- | --- |
| 1 | Pickup | 93, 126, 143, 151, 153, 172, 176, 178, 190, 199, 202, 205, 211, 213, 215, 217, 228, 239, 243, 245, 253, 266, 271, 276, 278, 281, 282, 287, 288, 294, 295, 300, 305, 310, 311, 315, 316, 319, 323, 324, 331, 333, 335, 336, 338, 339, 342, 343, 344, 351, 353, 357, 365, 370, 374, 385, 399 |
| 1 | Journey | 25, 59, 93, 95, 109, 149, 172, 176, 190, 194, 199, 202, 205, 211, 213, 215, 217, 228, 241, 243, 245, 253, 266, 276, 278, 281, 282, 287, 288, 300, 305, 311, 315, 324, 333, 335, 336, 338, 339, 342, 370 |
| 2 | Pickup | 33, 97, 141, 157, 159, 168, 170, 173, 177, 183, 187, 188, 197, 218, 233, 237, 239, 242, 246, 247, 250, 259, 261, 265, 267, 275, 276, 278, 280, 285, 297, 298, 303, 311, 314, 326, 327, 332, 334, 335, 337, 339, 343, 346, 347, 351, 355, 359, 371, 376, 382 |
| 2 | Journey | 96, 97, 133, 141, 150, 159, 168, 170, 177, 183, 187, 197, 218, 233, 237, 239, 240, 242, 250, 259, 261, 275, 280, 285, 298, 304, 334, 335, 339, 343, 346, 355, 371, 376, 382 |
| 3 | Pickup | 39, 41, 120, 134, 136, 138, 155, 162, 167, 183, 186, 200, 221, 223, 234, 238, 243, 247, 249, 251, 255, 261, 263, 264, 266, 268, 275, 289, 292, 301, 302, 313, 323, 326, 328, 332, 333, 337, 338, 343, 351, 353, 366, 371 |
| 3 | Journey | 6, 31, 39, 41, 43, 99, 108, 120, 136, 138, 144, 155, 162, 167, 186, 200, 223, 234, 238, 243, 247, 249, 251, 255, 261, 264, 266, 268, 301, 333, 338, 351 |

The wait percentiles below use the matched pickup cohort for each baseline comparison, with the nearest-rank method.
Times are simulated seconds.
The baseline cohort can differ between the control and Express comparisons.

| Seed | Comparison arm | Matched | Baseline p95 | Baseline p99 | Arm p95 | Arm p99 |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | control | 249 | 1710.53 | 2120.60 | 1862.70 | 2133.77 |
| 1 | express | 258 | 1785.07 | 2120.60 | 303.07 | 473.30 |
| 2 | control | 234 | 1773.27 | 2098.73 | 1787.37 | 2091.10 |
| 2 | express | 246 | 1898.33 | 2142.53 | 450.67 | 820.80 |
| 3 | control | 231 | 1839.57 | 2200.37 | 1898.70 | 2085.27 |
| 3 | express | 240 | 1921.73 | 2200.37 | 278.60 | 476.53 |

Censored riders remain outside these percentile and exceedance calculations.
The following counts give baseline-only events, candidate-only events, and events absent in both arms, respectively.
They cover all 599 background riders at minute 60.
An absent event cannot pass an individual gate by omission.

| Seed | Arm | Pickup: baseline only / arm only / neither | Completion: baseline only / arm only / neither |
| --- | --- | --- | --- |
| 1 | control | 9 / 3 / 338 | 5 / 8 / 373 |
| 1 | express | 0 / 296 / 45 | 0 / 236 / 145 |
| 2 | control | 12 / 8 / 345 | 11 / 10 / 381 |
| 2 | express | 0 / 322 / 31 | 0 / 239 / 152 |
| 3 | control | 9 / 8 / 351 | 13 / 9 / 380 |
| 3 | express | 0 / 322 / 37 | 0 / 226 / 163 |

Every background rider completed by baseline also completes in D Express by the same 60-minute cap.
This checks background finite recovery at these seeds, but does not qualify the full workload or the censored riders.

All eight D runs report zero oracle failures.
The seven 60-minute runs cover 108,000 ticks each, and the drain covers 270,000 ticks.
No separation, speed, or ownership failure is reported.
The reused baseline and A logs also report zero oracle failures.

`compare.py` finds all 909 Express seed 1 order records identical to the repeat, byte for byte.
All 61 state and metric records match by value.
All 61 differ in JSON bytes because map keys have different orders.
The 62 configuration and progress records match after removal of elapsed timers.
This establishes reproducibility of the logged records for this binary and seed.

The drain stops arrivals at minute 60 and ends at the fixed minute-150 cap.
Using `summarize.py`'s record reader, all 901 conservation records have consistent request and hub-passenger balances.
These records include 900 checks at 300-tick intervals and the final check.
The final order records have 909 unique request IDs and 909 unique party IDs.
All 909 orders complete, with no interrupted, lost, or duplicate order found.

| Checkpoint | Accepted requests = completed + interrupted + queued + aboard | Hub passengers = completed + waiting + aboard |
| --- | --- | --- |
| Minute 60 | 909 = 657 + 0 + 136 + 116 | 3241 = 1992 + 1091 + 158 |
| Minute 150 | 909 = 909 + 0 + 0 + 0 | 3241 = 3241 + 0 + 0 |

The final hub party completes at tick 163116, minute 90.620.
The final request completes at tick 199032, minute 110.573.
The minute-60 hub backlog is 1249 passengers, including 158 aboard.
The drain's first 60 minute-state samples match the Express seed 1 samples by value.
Its next state sample is tick 108001, whereas the 60-minute run ends at tick 108000.
Those boundary samples are not the same observation.

Parking occupancy starts at 16 and reaches zero at minute 3.028.
It stays at zero until the first return at minute 95.518.
The following values show the later minute samples.

| Minute | Express pods in parking |
| --- | --- |
| 60 | 0 |
| 90 | 0 |
| 95 | 0 |
| 96 | 2 |
| 97 | 3 |
| 98 | 5 |
| 99 | 7 |
| 100 | 9 |
| 101 | 12 |
| 102 | 14 |
| 110 | 14 |
| 150 | 14 |

The fourteenth return occurs at minute 101.833, and occupancy stays at 14 through minute 150.
Express 09 and Express 11 remain idle at Bank berths 02 and 01.
Their last state changes occur at minutes 90.620 and 90.208, respectively.
Every minute sample from minute 91 through minute 150 shows both ready, empty, with no destination, wait reason, or blocker.
The final per-pod records confirm both at Bank, with `Returned=0`.
Shorter idle samples also show Express 10 at minute 84, Express 14 at minutes 86 and 87, and Express 07 at minute 90.
No sampled idle Express pod stands at Waterloo during the drain.
All requests complete, so these idle Bank pods do not cause an unresolved deadlock in this run.

Compact blocking by Express remains visible in the cumulative counters.
The three Express runs record 922 / 559 / 665 sampled blocked pod-seconds over minutes 0 to 60.
The minutes-20-to-60 increments are 798 / 237 / 490 pod-seconds.
These counters name pod pairs but do not record the blocker's activity or station.
They cannot establish how much blocking occurs at Bank or involves an idle Express pod.
In the drain, no Compact-by-blocker counter increases during the minute intervals after any sampled idle Express pod at Bank.
The counters for Compact pods blocked by Express 09 and Express 11 also stay unchanged from minute 91 through minute 150.
This finds no logged Compact blockage by the two pods that remain idle at Bank.
It cannot exclude shorter idle blocks between samples or idle blocks before minute 60.
The D logs lack the Compact positions and per-tick blocker states needed for that check.

The following comparison uses the same policies-on windows and schedules for A and D.
Each A / D cell keeps the two projects separate.

| Seed | Waterloo to Bank passengers/min, A / D | Whole parties/min, A / D | Background completed, A / D | Full-cap pickup exceedances, A / D |
| --- | --- | --- | --- | --- |
| 1 | 38.125 / 41.950 | 3.575 / 3.975 | 376 / 376 | 0 / 0 |
| 2 | 38.375 / 41.575 | 4.025 / 4.350 | 373 / 375 | 1 / 0 |
| 3 | 37.775 / 42.675 | 3.875 / 4.425 | 351 / 350 | 0 / 0 |

Neither A nor D has a matched background journey exceedance against baseline in these seeds.
D raises Waterloo to Bank passenger throughput by 10.0%, 8.3%, and 13.0% over A.
D changes background completions by 0, +2, and -1 in the late window.

Design rule result: observed safety passes in all D runs.
Conservation passes in the seed 1 drain, and logged reproducibility passes at seed 1.
No unresolved deadlock occurs in that drain, and hub throughput improves against baseline and control in every seed.
D Express passes both individual gates for observed matched background riders, with censored riders unresolved.
D control fails both individual gates in every seed.
These results support further bounded qualification of D Express, not adoption or a default change.

Not checked: reproducibility or parking drains at seeds 2 and 3, and unlogged internal state.
Full per-tick Compact blockage by idle Express pods at Bank is also unchecked.
Also not checked: restore fidelity, all demand bands and seeds 1 to 10, mixed Acton traffic, and the default live queue.
Independent policy combinations are unchecked.
The six-hour backlog, qualified capacity, aggregate-service limits, shared-detour limit, CPU limits, memory, GC, and network gates are not assessed here.
The screen does not check land or flyover feasibility.

Comparison cautions: baseline and A are reused historical runs.
The logs lack independent execution-time source, runner, binary, and project hashes.
Matching offers does not establish a current-source reproducibility bridge.
D changes geometry and berth count together, so this screen cannot isolate their effects.
Control splits hub parties into Compact orders, while Express carries whole parties without a Compact fallback.
The direct submission path does not enforce the default live queue cap.
Neither the sparse reverse flow nor censored riders supports a full service-gate pass.

Verdict: D Express improves hub throughput in all three seeds and has no matched background individual exceedance.
Safety, seed 1 drain conservation, and seed 1 logged reproducibility pass the observed checks.
Two Express pods remain idle at Bank, with no unresolved drain deadlock or observed Compact counter increase during sampled idle intervals.
Proceed only to further qualification, with censored riders, historical provenance, and unsampled idle blocking unresolved.
