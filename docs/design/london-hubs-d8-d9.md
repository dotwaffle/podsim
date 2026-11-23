# Express London hubs

Status: Proposed on 2026-11-23, not approved.
Nothing in this design is built.
Code references are for `691f1f6b`.
The measurements of section 3 ran on 2026-11-23 at `691f1f6b`.

This design is for two open decisions of the approved [shape clearance](shape-clearance.md) design: D8, Express Parking, and D9, the first hub pairs.
The [shared-track](shared-track-preset.md) design built the first stage of each as `london-full-shared-express`: Waterloo and Bank on shared track, with a parking comb beside Waterloo.
This design asks which hub pairs come next, where their Express pods park, what a build changes, and how to measure the result.
The decisions for the maintainer are in section 6.

In this document, D8 and D9 are the decisions of the shape clearance design.
Other documents use the same labels for other items.
The [series berths](series-berths.md) design has its own D8 and D9 (`series-berths.md:570` to `:571`).
"The D9 geometry" of the [Express London qualification](../contracts/express-london-qualification-proposal.md) is a layout of the [hub fixture](../studies/express-hub-fixture.md) study (`express-london-qualification-proposal.md:31`, `express-hub-fixture.md:562`).

## Summary

- The records do not define "demand hubs".
  The phrase is only in the status lines of the shape clearance design (`shape-clearance.md:10`, `:12`).
  The decision table calls D9 "First hub pairs", and the maintainer chose the order "by demand and residuals" (`:807`, `:825`).
  This design reads D9 as stages 2 to 4 of section 4.1 of that design, because stage 1 is built (`shared-track-preset.md:60`).
  Decision HB-D1 asks the maintainer to confirm this reading.
- D8 at another hub depends on D9.
  Project validation refuses a berth that admits Express when it has no Express route to and from an Express parking (`internal/project/parking_gaps.go:85` to `:106`).
  So each new hub pair needs a comb of its own, or Express track to the Waterloo comb.
- The probe of the shape clearance design gives each count of its sections 2.1 to 2.3 again on the wide network without fillets.
  On the default LondonFull network, which is tight and filleted, the counts change by a few pairs (section 3.2).
- With the preset as the base, the probe finds one pair under 4.0 m on each stage 2 route.
  It is the same pair on both routes: `940GZZLULNB-road-out-01` crosses `london-link-089-ab-2` (section 3.3).
  The built audit refuses at least this pair.
- Recommendation: London Bridge and Canary Wharf first, as a new preset on top of `london-full-shared-express`, with a comb beside London Bridge.
  Waterloo and Canary Wharf come next, and they reuse the London Bridge and Canary Wharf changes.
- A comb beside a hub makes the start of the Express fleet shorter.
  The route from the first berth of the comb to the first Waterloo berth is 654.0 m (`shared-track-preset.md:785`).
  From the exit of the parking of D to the Waterloo entry, the route is 7,063.6 m (`express-hub-fixture.md:145` to `:147`).
  The two distances have different end points.
  During service, no rule sends an idle Express pod back to parking (`express-hub-fixture.md:529`).
  So the comb has no other effect until the demand drains, except for a bumped pod (inferred from section 2.2).

## Scope

In scope:

- The order of the next hub pairs on shared track, and the candidate set.
- The Express parking of each new pair (D8).
- The builder changes, the presets, the qualification rows, and the measurements of a build.

Not in scope:

- D8-C, a large depot.
  It changes the depot rules, and the Express order contract refuses a depot with a large berth (`internal/sim/order_contract_bounds.go:80` to `:81`).
- Changes to LondonFull, D (`london-full-express`), or `london-full-shared-express`.
- Batch 2 of the study plan of the shape clearance design, which is slice 7 of the shared-track design and has its own plan.
- The section 7 row of the shape clearance design, "routes of section 2.1 with residual geometry fixed" (`shape-clearance.md:10`).
  Each stage of this design gives the evidence for its own routes only.

## 1. D8 and D9 in the records

### 1.1 D8, Express Parking

The decision row (`shape-clearance.md:806`):

> D8 | Express Parking | A: a comb of 40 m by 40 m beside each origin hub. B: keep the parking of D. C: a large depot. | A for the first stages. C later, with its own design, because it changes the depot rules.

On 2026-11-16, the maintainer chose A: "D8 is a parking comb beside each origin hub" (`:825`).
Section 4.2 gives the proposal (`:676` to `:694`):

- "One parking beside each origin hub of the first set: Waterloo first, then London Bridge and Paddington." (`:683`)
- "Each parking is a comb at a pitch of 40 m with berth lanes of 40 m, sized to the fleet of its services." (`:684`)
- "The site of each comb is not checked against the layout audit, so its position is inferred." (`:688`)

The reason is the tidal AM peak, "so each empty Express pod goes back to the origin hub" (`:678`).
The comb beside Waterloo is built at the site P4 of section 3.7 of the shared-track design (`shared-track-preset.md:827`).
The open part of D8 is the comb at each other origin hub.

### 1.2 D9, the first hub pairs

The decision row (`shape-clearance.md:807`):

> D9 | First hub pairs | By demand. By demand and residuals. | Under D1-B, by demand and residuals, as in section 4.1. Under D1-A, by demand.

The maintainer chose D1-B, and "D9 orders the pairs by demand and residuals" (`:823`, `:825`).
D9 depends on D1, because "The residuals set the order under D1-B only." (`:839`).
Section 4.1 gives four stages (`:660` to `:672`):

1. Waterloo and Bank.
2. London Bridge and Canary Wharf, and Waterloo and Canary Wharf.
3. Victoria and Oxford Circus, London Bridge and Waterloo, and King's Cross St. Pancras and Liverpool Street.
4. Paddington and Liverpool Street, Paddington and King's Cross St. Pancras, King's Cross St. Pancras and Victoria, and Tottenham Court Road and Liverpool Street.
   King's Cross St. Pancras and Waterloo, and Euston and Waterloo, can come before these.

The study plan puts stage 2 after batch 2: "If batch 2 passes the adoption gates, a later batch adds London Bridge and Canary Wharf, and Waterloo and Canary Wharf, as one arm." (`:930`).
Batch 2 is slice 7 of the shared-track design, and it is open (`shared-track-preset.md:11`, `:1020`).
The status lines say "The D9 demand hubs and the section 7 row stay open." (`shape-clearance.md:12`).

### 1.3 Gaps and disagreements in the records

| Item | Record | Gap or disagreement |
| --- | --- | --- |
| "Demand hubs" | `shape-clearance.md:10`, `:12` | Not defined. The decision is "First hub pairs" (`:807`). This design reads it as stages 2 to 4 of section 4.1 (HB-D1). |
| Part of a comb | 40 m in D8-A (`:684`, `:806`) | Built at 41 m. A part of 40 m gives lanes that validation refuses by rounding (`shared-track-preset.md:646`, `:754`). The maintainer accepted 41 m on 2026-11-16 (`:750`). |
| Site of a comb | Inferred, "outward from the through lane of its hub, as a London depot does" (`shape-clearance.md:687` to `:688`) | Section 3.7 of the shared-track design measured six sites at Waterloo, and none passed without a rule that the designs leave open (`shared-track-preset.md:816`). The maintainer chose P4 with an exemption in the layout audit of the preset (`:827`). The reference `london.go:895` is now `addLondonStation` at `internal/scenarios/london.go:919`. |
| Order of the combs | Waterloo, London Bridge, Paddington (`shape-clearance.md:683`) | Paddington is an origin hub only of stage 4 routes (`:670`). The comb order follows the demand of section 2.10, and the pair order of section 4.1 follows the demand and the residuals. |
| Origin hub | "each origin hub" (`:806`) | Defined by the AM peak only (`:678`). The PM peak reverses each tidal pair (section 3.1). Euston and Waterloo has 3.6 passengers a minute in each direction in the AM peak (`:412`), so it has no origin hub. |
| Size of a comb | "sized to the fleet of its services" (`:684`) | The fleet of a service is not defined. The code has one constant, `expressPods`, which is 16 (`internal/scenarios/london_full_express.go:22`). |
| Shape of a new hub | "The new hubs use the default LondonFull at scale 0.5, with the hub pitch of section 4.1." (`shape-clearance.md:739`) | Decision S2-A chose tight hubs with 41 m parts (`shared-track-preset.md:902`, `:909`), and a tight station does not use the station scale (`:63`). |
| Candidate pairs | The 12 pairs of section 2.10 (`shape-clearance.md:399` to `:415`) | The table states its rule for the last six rows only: "the largest pairs of two termini after Paddington and Liverpool Street" (`:414`). It does not say why it keeps pairs of two termini. Pairs outside the table have more journeys than some rows (section 3.1). |
| Measurements | Sections 2.1 to 2.4 on the wide network without fillets. Sections 2.5 to 2.10 from deleted probes (`:120`, `:128` to `:129`). | The default network is tight since 2026-11-16 and filleted since 2026-11-20. Section 3 runs the probe of sections 2.1 to 2.3 again, and it recounts the demand of section 2.10. |

## 2. Current code

### 2.1 The builders

`GenerateLondonFullSharedExpress` (`internal/scenarios/london_full_shared.go:62`) builds LondonFull.
Then `londonFullSharedExpress` (`:164`) sets the classes, the hub geometry, and the services.
`addSharedTrackParking` (`:131`) adds the comb, its two roads, and the 16 Express pods.
The layout audit then runs with `sharedTrackLayoutInput` (`:95`), which makes the comb a Parking facility of Waterloo (`:97`).
`sharedTrackGeometry` (`:16` to `:33`) holds the throat depth and the turn of each hub by station, one curved lane, and a list of removed lanes.

These parts of the code assume one hub pair and one comb:

| Assumption | Code |
| --- | --- |
| Two hubs in the hub loop of `apply` | `london_full_shared.go:281` |
| Two hubs in the class setter | `:206` |
| Two hubs in the road reset of FL-D5 | `:193` |
| One route | `sharedTrackRoute`, `:52` |
| Two services | `:176` |
| One curved lane | the fields `curve` and `offset`, `:29` to `:30` |
| One pair of portals for the comb roads | `:102` to `:105` |
| One comb: the station `express-parking`, the nodes `xp-entry`, `xp-exit`, and `xp-NN`, the separation group `xp-parking`, and 16 berths | `london_full_express.go:18`, `:22`, `:285` to `:298` |
| 16 pods `express-NN` at the berths `xp-NN` | `london_full_shared.go:154` to `:155` |

Two tools also name the pair.
The study screen of `cmd/compare` makes its hub parties between Waterloo and Bank (`cmd/compare/screen.go:34` to `:35`, `:286` to `:288`).
One soak test takes the first Express service (`internal/session/incident_soak_express_test.go:330`).

### 2.2 Where an Express pod parks

- Each berth that admits a class of the fleet needs a route to and from a parking of that class (`internal/sim/parking_gaps.go:54` to `:105`).
  Validation refuses a project with a gap (`internal/project/parking_gaps.go:85` to `:106`).
- Under the Express order contract, a depot cannot have a large berth (`internal/sim/order_contract_bounds.go:80` to `:81`).
  So an Express pod parks only in a comb.
- `park` (`internal/sim/parking.go:153`) takes the first `ParkingOnly` station in network order with a free berth and a route.
  It does not look for the nearest comb.
  It runs for an idle pod that blocks a berth (`moveBlocker`, `:45` to `:50`).
- Guarded positioning sends a bumped pod to the parking berth with the lowest route cost (`guardedBumpToParking`, `internal/sim/positioning.go:574`).
  It runs only while the request rate is low (`:17` to `:21`).
  The presets turn it on (`docs/reference.md:38`).
- No general rule sends an idle Express pod home (`express-hub-fixture.md:529` to `:530`).

So, on one connected Express network with two combs, a bumped pod at a high request rate goes to the comb that is first in the network.
That comb can be the far one (inferred from the code, not measured).
When two Express networks share no lane, a pod reaches only its own comb, and the order has no effect.

## 3. Measurements

### 3.1 Demand of the hub pairs

The recount reads the embedded source file `internal/scenarios/data/london-full-dlr-od-2024.csv`.
It sums the six observed bands in both directions, and it divides the AM peak weight by its 180 minutes.
The source has the raw weights, and the profile rounds them (`roundDemandWeight`, `internal/scenarios/london_demand.go:34`).
The recount gives each journey count of section 2.10 within 1 journey, and each AM peak rate within 0.1 passengers a minute.

The AM and PM peak rates of the pairs of stages 1 to 3, in passengers a minute:

| Pair | Journeys | AM peak, each direction | PM peak, each direction |
| --- | ---: | --- | --- |
| Waterloo to Bank, and back | 23,931 | 54.8 and 0.6 | 2.2 and 40.5 |
| London Bridge to Canary Wharf, and back | 11,121 | 19.6 and 3.1 | 4.0 and 13.9 |
| Waterloo to Canary Wharf, and back | 11,766 | 23.4 and 2.2 | 4.0 and 15.9 |
| Paddington to Liverpool Street, and back | 12,346 | 15.6 and 4.4 | 5.6 and 14.5 |
| Victoria to Oxford Circus, and back | 11,565 | 12.3 and 0.9 | 5.7 and 12.9 |
| Waterloo to London Bridge, and back | 9,821 | 7.8 and 2.4 | 6.5 and 7.9 |

The AM origin of each stage 2 pair is London Bridge or Waterloo.
In the PM peak, Canary Wharf is the origin of both stage 2 pairs, with 29.8 passengers a minute together.

Pairs that the table of section 2.10 does not hold, with more journeys than its row 8 or row 12:

| Pair | Journeys |
| --- | ---: |
| King's Cross St. Pancras and Oxford Circus | 7,946 |
| Paddington and Tottenham Court Road | 7,944 |
| Farringdon and Paddington | 7,413 |
| Piccadilly Circus and Waterloo | 7,280 |
| Oxford Circus and Waterloo | 7,184 |
| Tottenham Court Road and Waterloo | 7,064 |

The first two have more journeys than row 8, King's Cross St. Pancras and Victoria, with 7,675.
Over all LondonFull sites, 14 pairs outside the table have more journeys than row 12, Euston and Waterloo, with 5,648.
The first seven rows of the table are the seven largest pairs over all sites.

### 3.2 Residuals on the current networks

The probe is `TestShapeClearanceStudyRoutes` and `TestShapeClearanceStudyHubs` (`internal/sim/shape_clearance_study_test.go:467`, `:501`), with the commands of section 3.5.
It counts the pairs that the envelope reach of D1-B does not separate, by their smallest distance.
It takes the front bound of each lane, and not the anchor front of decision S1, and it runs no body sweep.
So its counts are a screen, and they are not refusals of the built audit.
On Waterloo and Bank, the built audit refuses 7 pairs on the tight network without fillets and 9 pairs with fillets (`shared-track-preset.md:84` to `:86`).

The two inputs:

- Wide, without fillets (`-wide-stations -road-fillets=false`), the input of section 2.11 of the shape clearance design: network SHA-256 `c8a1cbb11fd6880c1da86f3a4b365370693660bb13e23566e81890cff4174726`, which the shared-track study pins (`internal/sim/shared_track_study_test.go:65`).
  The probe gives each count of the tables of sections 2.1, 2.2, and 2.3 of that design again, for all 12 routes and all 15 hubs.
- The default LondonFull, tight and filleted: network SHA-256 `03d04786f72780fe4e0e1a65ee90eefcb8e20fc78ddd0c2b0600556cbea79ec5` (`internal/scenarios/london_full_shared_test.go:261`).

Envelope reach pairs at 20 m on the default network, by the smallest distance:

| Route | Stage | Total | Under 4.0 m | 4.0 to 9.31 m | 9.31 to 20 m | Wide network, under 4.0 m and 4.0 to 9.31 m |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| Waterloo and Bank | 1 | 48 | 7 | 9 | 32 | 5 and 8 |
| London Bridge and Canary Wharf | 2 | 5 | 1 | 1 | 3 | 1 and 1 |
| Waterloo and Canary Wharf | 2 | 24 | 5 | 2 | 17 | 4 and 3 |
| Victoria and Oxford Circus | 3 | 46 | 11 | 3 | 32 | 10 and 2 |
| London Bridge and Waterloo | 3 | 21 | 4 | 1 | 16 | 3 and 2 |
| King's Cross St. Pancras and Liverpool Street | 3 | 30 | 2 | 4 | 24 | 1 and 4 |
| Paddington and Liverpool Street | 4 | 132 | 31 | 15 | 86 | 30 and 14 |
| Paddington and King's Cross St. Pancras | 4 | 105 | 30 | 10 | 65 | 30 and 9 |
| King's Cross St. Pancras and Victoria | 4 | 92 | 23 | 7 | 62 | 23 and 6 |
| Tottenham Court Road and Liverpool Street | 4 | 62 | 21 | 6 | 35 | 21 and 4 |
| King's Cross St. Pancras and Waterloo | 4 | 57 | 7 | 10 | 40 | 5 and 6 |
| Euston and Waterloo | 4 | 36 | 6 | 10 | 20 | 4 and 4 |

The stage order of section 4.1 holds on the default network.
London Bridge and Canary Wharf has the fewest pairs of any route.
Its two pairs under 9.31 m are `940GZZLULNB-road-out-01` and `london-link-089-ab-2` at 0.00 m, and `940GZZLUCYF-road-out-01` and `london-link-158-ba-2` at 7.05 m.

Each hub alone, with the classes on all its lanes, roads included, on the default network:

| Hub | Total | Under 4.0 m | 4.0 to 9.31 m | 9.31 to 20 m | Large lanes under 40 m |
| --- | ---: | ---: | ---: | ---: | ---: |
| London Bridge | 12 | 2 | 0 | 10 | 18 |
| Canary Wharf | 3 | 0 | 2 | 1 | 18 |
| Paddington | 12 | 3 | 1 | 8 | 18 |
| Victoria | 13 | 2 | 1 | 10 | 18 |
| Liverpool Street | 18 | 0 | 4 | 14 | 18 |
| King's Cross St. Pancras | 37 | 3 | 8 | 26 | 24 |
| Oxford Circus | 36 | 10 | 3 | 23 | 18 |

At each of these hubs, the large lanes under 40 m are lanes of the tight shape.
The access lanes are 25.5 m, and the berth lanes and links are 25.0 m.
King's Cross St. Pancras also has `road-in-02` (36.1 m) and `road-out-04` (38.8 m).
The hub parts of 41 m of decision S2-A remove the short station lanes (`shared-track-preset.md:643` to `:647`).
At London Bridge alone, the pairs under 4.0 m are `road-in-02` with `london-link-095-ba-1` and `road-out-01` with `london-link-089-ab-2`, both at 0.00 m.

### 3.3 Residuals with the preset as the base

The same probe ran on `london-full-shared-express`, network SHA-256 `cba69149281760dbba334323497b9d94cb1d3a980a631b68de0be45e78aac6bb` (`internal/scenarios/london_full_shared_test.go:266`).
On this input, the lanes of Waterloo and Bank and the comb already admit Express.
`TestShapeClearanceStudyBase` fails on the preset with 128 pairs, because it counts the whole reach of the audit before decision D1-B (`internal/sim/shape_clearance_study_test.go:460` to `:461`).
Under the envelope reach, the Waterloo and Bank row of the preset gives 47 pairs.
The built audit refuses no pair on the preset and has 47 shape pairs, which a test pins (`internal/scenarios/london_full_shared_test.go:39`).
So the pairs of the probe from 4.0 m to 20 m include pairs that the body sweep clears.
Only the bin under 4.0 m compares with the audit, because no body test clears a pair under 4.0 m on two lanes that admit Express (`shape-clearance.md:204`).
The bin under 4.0 m is a lower bound of the refusals (section 4.5).

| Route | Under 4.0 m, the preset as the base | Under 4.0 m, the default network |
| --- | ---: | ---: |
| Waterloo and Bank | 0 | 7 |
| London Bridge and Canary Wharf | 1 | 1 |
| Waterloo and Canary Wharf | 1 | 5 |
| London Bridge and Waterloo | 0 | 4 |
| King's Cross St. Pancras and Waterloo | 6 | 7 |
| Euston and Waterloo | 5 | 6 |

The pair under 4.0 m of both stage 2 routes is `940GZZLULNB-road-out-01` and `london-link-089-ab-2`, at 0.00 m.
The changes of the preset at Waterloo remove the other four pairs under 4.0 m of Waterloo and Canary Wharf.
The probe does not tell which change removes each pair.
The route lanes between London Bridge and Canary Wharf are not compared with the route lanes of Waterloo and Canary Wharf.
So it is not known whether the two stage 2 routes share lanes.

### 3.4 What the Waterloo comb measured

No study measured the effect of the comb beside Waterloo on service, because slice 7 of the shared-track design is open.
The qualification of the preset checked the comb in its route and restore rows (SX2 and SX4), with no comparison of arms.
These are the measures that a new comb can reuse:

| Measure | Source |
| --- | --- |
| Project validation, bank-audit refusals, shape pairs, hard and soft layout conflicts, tail warnings, and parking gaps of each site | `shared-track-preset.md:768` to `:775` |
| Express route lengths from the first comb berth to the first berth of each hub, and back | `shared-track-preset.md:779` to `:786` |
| An Express route from each comb berth to each hub berth, and back, 224 routes on the preset | SX2 evidence, `express-shared-track-qualification-proposal.md:186` to `:188` |
| Restore checkpoints with a pod on its way back to the comb and a pod in a parking maneuver | SX4 evidence, `express-shared-track-qualification-proposal.md:231` |
| First hub boarding, first sample with an empty parking, parking occupancy by minute, and Express parties completed | `express-hub-fixture.md:340` to `:383` |
| Returns to parking in a drain, and the pods that stay idle at a hub | `express-hub-fixture.md:522` to `:530`, `:726` to `:746` |
| Compact pod-seconds blocked by Express pods | `express-hub-fixture.md:377` to `:383` |

### 3.5 Commands

The project files are about 15 MB each, so the commands write them to a scratch directory.

```sh
D=$(mktemp -d)
go run ./cmd/scenario -preset london-full -output "$D/lf.json"
go run ./cmd/scenario -preset london-full -wide-stations -road-fillets=false -output "$D/lfw.json"
go run ./cmd/scenario -preset london-full-shared-express -output "$D/lfs.json"
for f in lf lfw; do
  PODSIM_SHAPE_STUDY="$D/$f.json" go test ./internal/sim -run 'TestShapeClearanceStudy(Routes|Hubs)$' -count=1 -v
done
PODSIM_SHAPE_STUDY="$D/lfs.json" go test ./internal/sim -run 'TestShapeClearanceStudy(Base|Routes)$' -count=1 -v
rm -r "$D"
```

Each test takes less than 10 s.
The last command fails in `TestShapeClearanceStudyBase`, as section 3.3 explains, and `TestShapeClearanceStudyRoutes` still logs its counts.
The demand recount of section 3.1 was a scratch `awk` script over the CSV file, and it is deleted.

## 4. D9: the next hub pairs

### 4.1 Options

| Option | Next stage | For | Against |
| --- | --- | --- | --- |
| A | Stage 2 as one stage: London Bridge and Canary Wharf, and Waterloo and Canary Wharf | Follows section 4.1 and the study plan (`shape-clearance.md:930`). One preset and one set of rows for two pairs. | Two new hubs, two routes, and a comb at once. Waterloo and Canary Wharf joins the Express network of Waterloo and Bank, so the order of `park` matters from the start (section 2.2). |
| B | London Bridge and Canary Wharf first, then Waterloo and Canary Wharf | The fewest residuals of any route (section 3.2). Its residual under 4.0 m is also the one residual under 4.0 m of Waterloo and Canary Wharf on the preset base (section 3.3). It does not touch the hub geometry of the preset at Waterloo. | Two presets and two sets of rows for stage 2. A comb at London Bridge is necessary, because the pair has no Express track to the Waterloo comb (section 2.2). |
| C | Waterloo and Canary Wharf first | The second largest AM direction of the table, 23.4 passengers a minute. It can use the Waterloo comb, with no new comb. | It needs the London Bridge fix and the Canary Wharf changes, and its route passes 4 stations. |

The recommendation is option B.
The first build then has at least one crossing to fix, and the hub shapes at two new hubs.
It also has one new comb, and the Canary Wharf pair at 7.05 m for the body sweep.
Waterloo and Canary Wharf then adds only its route and the pairs that the first build does not fix.

### 4.2 What a build changes

- `internal/scenarios`: the builder takes a list of hubs, routes, services, curved lanes, and removed lanes, in place of the fixed values of section 2.1.
  `GenerateLondonFullSharedExpress` keeps its network hash, and a test pins it, as `TestLondonFullSharedExpressKeepsPresets` pins LondonFull and D (`internal/scenarios/london_full_shared_test.go:239`).
- The new preset builds on the preset of D11 (HB-D6).
  It adds the hub shape of S2-A and the straight hub roads of FL-D5 at London Bridge and Canary Wharf (HB-D8).
  It also adds the changes that remove each refusal of the built audit, and a comb (section 5).
- `cmd/scenario`: one more preset name, with no capacity flags, as decision S5-A gives for D11.
- `cmd/compare`: the screen takes its hub pair from the Express services of the project, if the study uses it (section 2.1).
- The qualification rows of section 4.4.

### 4.3 How to measure it

Static measures, as sections 2 and 3 of the shared-track design give them:

- The built audit on the new routes with the anchor front of S1: refusals, shape pairs, and the cause of each refusal.
  The study copy `TestSharedTrackStudyRefusals` names Waterloo and Bank only (`internal/sim/shared_track_study_test.go:41`), so it needs the pair as a parameter.
- Lanes under 40 m, preparation, the layout audit, and `projectcheck`.
- The cost of each removed lane to the station routes, in free-flow seconds.
  It covers all pairs of station exit and station entry, as `shared-track-preset.md:720` to `:729` gives it.

Simulation measures, with the method of batch 2 of the shape clearance design (`shape-clearance.md:916` to `:933`):

- Arms: the preset of D11, the new preset, and the new preset with no Express pods as a control.
- AM peak, seeds 1 to 3, a timed pilot first, inside the cap of 6 arms and 3 seeds.
- Hub passengers a minute in each direction, whole parties a minute, and the measures of section 3.4.
- Matched background waits and their exceedances, Compact pod-seconds blocked by Express pods, and empty distance.

### 4.4 Qualification rows

A new preset is a new project, and the evidence of D11 does not transfer (`shape-clearance.md:772` to `:773`).
The rows follow SX1 to SX13 of the [Express shared-track qualification](../contracts/express-shared-track-qualification-proposal.md).
The prefix HX is a working name.

| Row | Based on | What is new for the preset |
| --- | --- | --- |
| HX1 | SX1 | LondonFull, D, and `london-full-shared-express` do not change, and their network hashes stay. |
| HX2 | SX2 | The new route lanes and hub lanes, shared with Compact pods. The built audit refuses no pair, preparation refuses no lane, and the layout audit finds no conflict. Every hub berth, both directions of each service, and both directions between each hub and each comb. |
| HX3 | SX3 | The oracle and the D6 oracle at each tick with the new shape table, and the reference construction of the envelope. |
| HX4 | SX4 | Physical restore at checkpoints that include a pod on the new route with a Compact pod, a faulted pod there, a pod on its way to the new comb, and a pod in a maneuver of the new comb. |
| HX5 | SX5 | The byte proofs on the new geometry. |
| HX6 | SX6 | The stream and public consumers. The maintainer does the Chrome check. |
| HX7 | SX7 | The cost of the preset, against LondonFull, D, the preset of D11, and the new preset with no Express pods. |
| HX8 | SX8 | The current limits, with directed incident and recovery coverage on the new services. |
| HX9 | SX9 | The standee rows with the new services. |
| HX10 | SX10 | A named project of the London soak row. |
| HX11 | SX11 | A CI case of one simulated hour, and the release soak. |
| HX12 | SX12 | The performance and latency measurements that apply. |
| HX13 | SX13 | The formats byte proof. |

### 4.5 Risks

- Batch 2 can fail its adoption gates.
  Then the study plan does not add stage 2 (`shape-clearance.md:930`), and a build of D9 has no study to follow.
- The built audit can refuse more pairs than the bin under 4.0 m of the probe shows.
  On Waterloo and Bank, the probe had 5 pairs under 4.0 m.
  The audit with a front reach of 6 m refused 7 pairs, 2 of them at 4.55 m and 6.26 m (`shape-clearance.md:252`).
  With fillets, the built audit refuses 9 pairs on that route (`shared-track-preset.md:85`).
- A removed junction lane makes some station routes longer.
  On the preset of D11, the worst case is +21.4 s (`shared-track-preset.md:727`).
- `soak:full` took 6,398 s of its limit of 7,200 s with the current presets (`shape-clearance-always-on-proposal.md:450`).
  One more preset can go over the limit, and decision AO-D8 gives that choice to the maintainer (`:515`).
- The straight hub roads of FL-D5 change two more hubs from the filleted LondonFull.

### 4.6 Size

One pair is about the size of slices 0 and 2 to 6 of the D11 build (`shared-track-preset.md:1004` to `:1020`).
The sim change of slice 1 of D11 is not needed again.
The evidence slice is the largest, because it has the soaks and the Chrome checks.

## 5. D8: a comb at the next origin hubs

### 5.1 Candidate hubs

| Hub | Stage | AM origin of | Comb now |
| --- | ---: | --- | --- |
| Waterloo | 1 to 4 | Bank, Canary Wharf, London Bridge, King's Cross St. Pancras | Built, site P4 |
| London Bridge | 2 | Canary Wharf, 19.6 passengers a minute | None |
| Victoria | 3 | Oxford Circus, 12.3 passengers a minute | None |
| King's Cross St. Pancras | 3 and 4 | Liverpool Street, 5.6 passengers a minute, and Victoria, 4.8 passengers a minute | None |
| Paddington | 4 | Liverpool Street, 15.6 passengers a minute, and King's Cross St. Pancras, 4.4 passengers a minute | None |
| Liverpool Street | 4 | Tottenham Court Road, 7.2 passengers a minute | None |

Section 4.2 of the shape clearance design names London Bridge and Paddington after Waterloo.
With the pair order of section 4.1, London Bridge is the next comb, and Paddington comes at stage 4.

### 5.2 Geometry constraints

- Parts: each berth lane, row link, and row pitch is 41 m.
  The 16-berth comb is 82 m by 656 m (`shared-track-preset.md:752` to `:753`).
  A comb of 8 berths is about 82 m by 328 m (inferred).
- Site: at Waterloo, a comb along the axis of the rows crossed link 182 from row 10, with 137 hard conflicts.
  The wedges W1 and W2 also failed (`shared-track-preset.md:797` to `:813`).
  Of the six sites, P4, W1, and W2 have a short route to Waterloo.
  W1 is refused, W2 has 102 hard layout conflicts, and P4 has one (`shared-track-preset.md:774` to `:776`, `:785` to `:787`).
- Layout audit: two roads of a Parking facility and of its gateway station can cross (`internal/scenarios/layout_audit.go:458`, `:466`).
  The gateway map has one entry for each facility (`:337`), so each comb needs its own station ID and its own entry.
- Shape clearance: the comb and its roads add large lanes near the hub, with new tails, fronts, and reach overlaps.
  At Waterloo, W1 had 4 refusals, and P4 had 47 shape pairs, against 38 before the comb (`shared-track-preset.md:710`, `:774` to `:775`).
- Roads: a comb road from a portal of the hub gives that portal a second lane (`shared-track-preset.md:824`).
  The Express parking roads are Express lanes, and the fillet rule leaves them straight (`london-fillets-proposal.md:82`).
- Scale: a tight station does not use the station scale (`shared-track-preset.md:63`), so scale 0.5 does not change a comb or a hub.
- The comb must not use the hub rows as its way to another station.
  At P2, a pod could leave Waterloo through the comb with no stop at a Waterloo berth (`shared-track-preset.md:820` to `:821`).

### 5.3 Options

| Option | Parking of the new pair | For | Against |
| --- | --- | --- | --- |
| A | A comb beside the AM origin hub of each new pair, London Bridge for the first pair | D8-A as approved. A short start for the fleet. If the pair shares no Express lane with Waterloo and Bank (not checked), `park` has one comb to choose. | A new site study, and probably an audit exemption, as at Waterloo. |
| B | Express track from the new pair to the Waterloo comb, on the route of London Bridge and Waterloo | No new comb, so `park` has one comb to choose. The route has no pair under 4.0 m with the preset as the base (section 3.3). | It builds a stage 3 route early, with more route lanes and shape pairs, and with rows that cover a third service. A start of about 2.6 km to 2.7 km from the comb to London Bridge (inferred from the routes of 1,948 m and 2,050 m and the 654 m from P4). Two hubs share one comb of 16 berths. |
| C | A comb at each hub of the pair | A comb at the PM origin too. | Twice the site work. No rule sends an idle Express pod home, so the second comb is used only at the start and in a drain (section 2.2). |

Recommendation: A, with a site study first (slice 0 of section 7).
If no site at London Bridge passes, Canary Wharf is the second candidate, because it is the PM origin of both stage 2 pairs.

### 5.4 Expected effect and how to measure it

Expected effect (inferred, not measured):

- The fleet of the new services starts within about 1 km of its origin hub, as at P4.
- During service, a comb has no effect, except for a pod that `moveBlocker` or guarded positioning bumps (section 2.2).
- In a drain, the pods go back to the comb.
  In the drain of D, 14 of 16 pods went back, and 2 stayed idle at Bank berths (`express-hub-fixture.md:745` to `:746`).

Measures: the static measures of section 3.4 for each site, and in the study of section 4.3, the startup and drain columns of the hub fixture.

### 5.5 Risks

- No site passes without an exemption, as at Waterloo.
  The London Bridge hub alone has two crossings at 0.00 m (section 3.2), so the space near its portals is already tight (inferred).
- With two combs on one Express network, `park` takes the first comb in network order (section 2.2).
  This applies from the stage that joins the new pair to Waterloo and Bank, for example Waterloo and Canary Wharf.
- A comb of 16 berths for a pair with less demand than Waterloo and Bank keeps more pods than the pair uses (inferred).

### 5.6 Qualification rows and size

The comb is part of the preset of its pair, so the rows of section 4.4 cover it.
HX2 covers the routes between each hub and each comb, and HX4 covers the restore of a pod in the comb.
The comb is about the size of slice 3 of the D11 build: a site study and one builder change.

## 6. Decisions

| ID | Decision | Options | Recommendation |
| --- | --- | --- | --- |
| HB-D1 | Meaning of "the D9 demand hubs" | A: stages 2 to 4 of section 4.1 of the shape clearance design, in the order of demand and residuals. B: another meaning that the maintainer gives. | A. It is the only reading that the decision row and section 4.1 support. |
| HB-D2 | Candidate pairs | A: the 12 pairs of section 2.10. B: all hub pairs over a journey threshold, for example the 14 more pairs over 5,648 journeys (section 3.1). | A for stages 2 and 3. B as a screen before stage 4, because it adds sites that are not hubs today. |
| HB-D3 | Residual measure for the order | A: the study probe of section 3.2. B: the built audit copy with the anchor front, for each route. | A to rank the pairs, because it is cheap. B to accept a stage, because only B gives refusals. |
| HB-D4 | Next stage | A: stage 2 as one stage. B: London Bridge and Canary Wharf first, then Waterloo and Canary Wharf. C: Waterloo and Canary Wharf first. | B (section 4.1). |
| HB-D5 | Timing against batch 2 | A: wait for the result of batch 2, as `shape-clearance.md:930` says. B: do the static slices now, and wait for batch 2 before the preset and its rows. C: build all now. | B. The static slices do not change a preset. |
| HB-D6 | Preset shape | A: change `london-full-shared-express`. It moves its hash and opens SX1 to SX13 again, as the fillets did. B: a new preset for each stage, built on the preset of D11, which does not change. C: one preset with a flag for each stage. | B. D11 stays qualified and stays the comparison with D. C has the problem of decision S5-B: each combination is a new geometry with no evidence. |
| HB-D7 | Qualification | A: new rows for each new preset, from SX1 to SX13 (section 4.4). B: SX1 to SX13 extended to name the new preset. | A, as decision S6-A chose for D11. B changes approved contract text. |
| HB-D8 | Shape and roads of a new hub | A: tight with 41 m parts (S2-A), and straight hub roads (FL-D5). B: 41 m parts with filleted roads, measured first. | A. FL-D5 keeps the hub roads straight so that the lanes of a hub keep one speed limit for platoon links (`london-fillets-proposal.md:304` to `:308`). |
| HB-D9 | Parking of a new pair (D8) | A: a comb beside its AM origin hub. B: Express track to the Waterloo comb. C: a comb at each hub of the pair. | A (section 5.3). |
| HB-D10 | Size of a comb | A: 16 berths and 16 pods, as D and D11. B: a size from the AM peak rate of the services. | A for the first new comb, so the study compares like fleets. B after the study measures the use of the fleet. |
| HB-D11 | Choice of comb on one connected Express network | A: keep `park` as it is, and measure the empty distance to parking. B: `park` takes the comb with the lowest route cost. | A. B changes the behavior of `park` for each project with more than one parking, so it needs its own design. The question starts at the stage that joins two Express networks. |
| HB-D12 | Layout audit exemption for a comb | A: a Parking facility entry for the comb, in the audit input of its preset only, as `sharedTrackLayoutInput` does. B: the site passes the full audit, with no exemption. | B where a site passes. A only when no site passes, as at Waterloo (`shared-track-preset.md:827`). |

### 6.1 Dependencies

| Decision | Depends on | Why |
| --- | --- | --- |
| HB-D2, HB-D4 | HB-D1 | The stages exist only under reading A. |
| HB-D4 | HB-D3 | The accepted order needs the refusals of the built audit. |
| HB-D9 | HB-D4 | The new pair sets the origin hub. Under HB-D4 C, option B of HB-D9 needs no new track. |
| HB-D11 | HB-D4, HB-D9 | The order of `park` matters only when one Express network has two combs. |
| HB-D7 | HB-D6 | New rows go with a new preset. |
| HB-D6 | HB-D4 | Under option B of both, stage 2 is two presets, two rounds of evidence, and two more soak presets. |
| HB-D12 | HB-D9 | Only a new comb needs a site. |

## 7. Slices of a build

Each slice is one commit, and it builds and passes the tests by itself.
The sizes are rough, and they compare with the slices of D11 (`shared-track-preset.md:1004` to `:1020`).
S is a docs or tool slice, as slice 5 of D11.
M is a builder or study slice, as slices 2 and 3 of D11.
L is an evidence slice, as slice 6 of D11.

| Slice | Area | Work | Size |
| --- | --- | --- | --- |
| 0 | `sim`, study tests | The built audit copy on the London Bridge and Canary Wharf route, with the preset as the base: each refusal and its cause. A site study of the comb at London Bridge with the measures of section 3.4, and at Canary Wharf if no London Bridge site passes. The tests skip unless an environment variable is set. | M |
| 1 | `docs` | A revision of this design with the geometry changes that remove each refusal, as section 3 of the shared-track design does, and the decisions that they raise. | M |
| 2 | `scenarios` | The builder of section 4.2, with lists in place of the fixed hubs, route, services, curve, and comb. The hashes of LondonFull, D, and `london-full-shared-express` do not move. | M |
| 3 | `scenarios`, `cmd/scenario` | The new preset and its comb, with tests that pin the audit result, the layout audit, the parking, and the routes of HX2. | M |
| 4 | `cmd/compare` | The screen takes its hub pair from the Express services. Only if the study needs it. | S |
| 5 | `contracts` | The qualification proposal of section 4.4, for maintainer approval. | S |
| 6 | Evidence | The evidence of each HX row, after approval. The Chrome checks of HX6 need the maintainer. | L |
| 7 | Study | The batch of section 4.3, after batch 2 of the shape clearance design. | M |
| 8 | `docs` | `docs/reference.md`, `docs/london-full.md`, the soak presets of `docs/operations.md`, the README index, and the status lines of the shape clearance design. | S |

Waterloo and Canary Wharf repeats slices 0 to 8 on top of the first new preset, with no new comb under HB-D9 A.

## 8. Open questions

1. Is "the D9 demand hubs" the stages of section 4.1, or did the maintainer mean another set (HB-D1)?
2. The table of section 2.10 keeps pairs of two termini for its last six rows.
   Is that rule a requirement for D9, or a choice of the deleted probe?
3. Does a PM peak study need a comb at the PM origin, Canary Wharf?
   In D, the Express pods stay at the hubs during service.
4. How many Express pods does each new service need, and does each comb keep 16 berths (HB-D10)?
5. Does the study of a new stage use `cmd/compare`, which names Waterloo and Bank today, or the soak driver?
