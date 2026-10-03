# Forecast donor guard candidate screen

The private donor guard does not qualify for adoption in this screen.
The candidate changes one forecast rule.
It does not change shipped code, defaults, physical values, or public contracts.
This report completes the approved bounded candidate screen.

## Frozen hypothesis and candidate

The current forecast policy preserves current idle reserves at donor stations.
It can still take a pod from a donor that has demand in the same 300-second forecast.
The hypothesis was that this move removes supply needed by the donor's known future offers.

The candidate masks a donor when `supply - 1 < min(Passengers, 4)` for that donor's forecast target.
It uses the current native supply count: guarded idle pods plus unassigned incoming empty pods.
The candidate includes ordinary incoming empty moves, as the current policy does.
It does not introduce a new supply, capacity, route, or arrival-time model.
Stations with no forecast target retain their current donor rules.
All current admission, reserve, cooldown, search, route-fit, berth, load, and incoming limits remain in the private copy.

The candidate exists only in five private Go files used by this study.
The durable export retains those sources and their hashes.
The final patch contains only this report and its measurement JSON.
A future implementation requires a separate reviewed change and qualification.

## Workload and limits

The screen uses the archived Rail Hub Banks baseline project and native demand generator.
Each seed has the same 1,440 immutable offer identities in all three policies.
Each offer is one whole party of one passenger with shared consent.
The fleet has 30 Legacy pods.
Sharing permits four parties, drop-offs, and three stops.
The pending queue limit is 200.
Occupied pickups, platoons, and general positioning stay off.
Routing uses free-flow routes.

The six arms are seeds 1 and 2 with forecast off, current forecast, and the private donor guard.
The forecast horizon is 300 seconds and the call interval is five seconds.
The arrival window ends at tick 216001.
The original cap is tick 648000, or 10,800 seconds.
The native drain rule can stop an arm earlier after all offers and rail connections resolve.
No arm or disabled continuation gets a longer cap.

The native source is `53321e4c5decc6c661df64a40378ee37614dcffc`.
It includes the qualified current-lane speed fix from `30d8e34`.
`source-pins.json` freezes native files, all five private files, the project, generated offers, native inputs, plan, and binary.
The binary SHA-256 is `80be2ef777f97d15bd2cf0a63c4c736157a7d9f9ce780598a5aae8f02a810912`.
The plan declared both seeds, all policies, caps, restore probes, and exact gates before the pilots or full arms.
It declared completed whole parties as the primary service measure before measurement.

## Results

Wait and journey columns use the native nearest-rank 95th percentile, in seconds.
The raw native result uses `policy=off` for the separate adaptive controller.
The study policy field records the forecast choice.
All full arms process every offer and complete their accepted parties before the original cap.
The full identity ledger retains every queue skip, refusal, future offer, boarded party, and completion.

| Seed | Policy | Completed | Queue skips | Wait p95 (s) | Journey p95 (s) | End tick | Forecast moves |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | off | 862 | 578 | 1407.55 | 1800.95 | 342780 | 0 |
| 1 | current | 877 | 563 | 1301.02 | 1728.45 | 307860 | 7 |
| 1 | donor-guard | 908 | 532 | 1288.65 | 1689.83 | 312960 | 4 |
| 2 | off | 920 | 520 | 1176.43 | 1583.67 | 320340 | 0 |
| 2 | current | 916 | 524 | 1198.42 | 1615.93 | 304860 | 7 |
| 2 | donor-guard | 847 | 593 | 1533.25 | 1906.50 | 334200 | 4 |

The candidate must complete at least 105/100 of the off-policy parties, separately for each seed.
It must also retain baseline completions and satisfy every individual wait and journey limit.
For wait, `20 * deltaTicks <= max(36000, baselineWaitTicks)`.
For journey, `20 * deltaTicks <= max(72000, baselineJourneyTicks)`.
The wait allowance is 30 seconds or 5%, whichever is larger.
The journey allowance is 60 seconds or 5%, whichever is larger.
Means and p95 must not increase above 102/100 of baseline.
The analysis checks both the matched identity cohort and the full native observed cohort.
It uses integer sums, counts, and percentile ticks, with no rounded threshold comparisons.

| Seed | Comparison | Completed delta | Lost baseline completions | New skips or refusals | Wait exceedances | Journey exceedances | Primary 5% benefit |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | off to current | +15 | 154 | 154 | 165 | 142 | fail |
| 1 | off to donor-guard | +46 | 113 | 113 | 141 | 118 | pass |
| 1 | current to donor-guard | +31 | 126 | 126 | 227 | 200 | fail |
| 2 | off to current | -4 | 178 | 178 | 342 | 293 | fail |
| 2 | off to donor-guard | -73 | 228 | 228 | 385 | 357 | fail |
| 2 | current to donor-guard | -69 | 238 | 238 | 346 | 313 | fail |

The durable analysis JSON retains before and after skip sets, lost and gained completion identities, and all unmatched censor sets.
It also retains every matched wait and journey delta.
The repository measurement JSON gives each set count and its durable evidence location.
A party with no observed wait or journey has an undefined value for that measure.
The analysis does not give that party a zero wait, zero journey, or inferred benefit.
A larger completion count does not cancel an individual loss or limit violation.

## Donor evidence and causal limits

Current forecast commits 4 donor-under-goal moves for seed 1 and 4 for seed 2.
The candidate commits 0 and 0 such moves.
Every committed decision retains its tick, chosen pod, source supply, source goal, target, and full forecast targets.
Each forecast target also maps to the exact future offer identities inside the native 300-second horizon.
The analysis verifies the target count and first release tick against those offers.
Rejected candidates retain pod, source, target, supply, and goal.
Repeated rejection records count evaluations, not distinct pods or moves.

For seed 1, current forecast takes `pod-005` from station 05 at tick 9000 with supply 4 and goal 4.
It then takes `pod-010` from station 04 at tick 12000 with supply 2 and goal 4.
Both moves violate the candidate donor predicate.
These records confirm the proposed missing guard in this workload.
They do not prove which move caused a particular passenger's delay or skip.
The guard changes later dispatch and queue interactions throughout the native trajectory.
The matched screen tests the resulting service, and its failed gates limit the adoption claim.

## Safety, parity, and restore

The full arms pass 1,923,000 observed ticks.
The observer checks the actual native trajectory after each tick.
It checks the current lane speed separately from `SafetyObservation.Check`.
It also checks acceleration, braking, owned stopping distance, native separation, retained ownership, passenger conservation, party size, and consent.
An unsafe arm stops at its first failure and retains its partial result.
These six arms have no safety failures.

The three 120-second seed 1 pilots pass observed/plain metric, saved-state, and request-timing parity.
Off and current also pass the original native caller metric control.
Candidate parity compares the same private candidate with and without the observer.
It does not claim equality between the candidate and the unchanged native forecast policy.
The full off/current arms match all archived aggregates and all 1,440 offer identities and timing outcomes for each seed.
The archived controls use native base `9113bb3d1d931d46da8c00b652cc7e03fff30c54`.
The export retains their raw controls, fixture metadata, and source pins.
Historical equality is a source bridge check, not a mixed-source service comparison.
All reported candidate comparisons use the same current native source.
The pilots establish observer parity for their 120-second horizon.
The full arms do not claim a separate full-length plain replay.

The screen runs 16 physical restore probes and checks 665,916 continuation ticks.
Off probes use ticks 18000 and 108000.
Current and candidate also probe the first committed forecast move.
Every restore retains its committed empty moves and passes the physical receipt checks.
No restore demotes, requeues, drops, logically completes, or loses a party.
Immediate saved state uses exact discrete equality and a declared `1e-7` meter distance tolerance.
The maximum observed distance change is `1.5631940186722204e-13` meters.
The restore reconstructs ordinary speeds and future grants from saved physical state.
Saved-state equality checks serialized fields.
It does not establish equality with the original live speeds, future grants, or later trajectory.
Each probe compares 3,600 ticks from two independent restores, including saved state and completion records.
Both continuations use the same reconstruction, with further forecast calls disabled.
This twin check does not compare a restored future against the original live future.
It then disables further forecast calls and runs the accepted cohort to resolution or the original cap.
The probes with pending parties or active committed empty moves at the endpoint number 0.
The JSON retains each receipt, endpoint, accepted count, completed count, and censor count.
These checks do not qualify the live session or wire contract.

## Validation and evidence

Fresh gopls diagnostics, scoped race tests, `go vet`, and Go lint pass.
The first lint failure and the corrected lint receipt remain in the export.
Compiled mutations that disable the donor mask or block an exact surplus fail their intended assertions.
Caller tests cover disabled state parity, a donor deficit, genuine surplus, and native incoming supply semantics.
The independent root review kills a compiled mutation that passes `false` at the actual candidate selector call.
Its valid observer control passes.
Seven compiled corruptions fail: consent, ownership, separation, lane speed, acceleration, position, and conservation.
The root review records its own source pins and exact receipts.
Its evidence is `/home/dotwaffle/.cache/agents/podsim/forecast-contract-batch-20261003/root/forecast-root-review.json`.
The root also independently verifies all six full identity ledgers, exact integer gates, and accepted restore cohorts.
Its ledger receipt is `root/forecast-ledger-independent-review.json` in the same durable batch directory.

The study uses pinned Go 1.27.1 with matching `GOROOT`, `GOTOOLCHAIN=local`, and `GOEXPERIMENT=jsonv2`.
Runs use `GOGC=400` and `GOMAXPROCS=1`.
Every run retains its exact command, cwd, PID namespace, PID, log, exit marker, watchdog, and stop/relaunch commands.
Watchdogs do not exceed 900 seconds.
Sources stay fixed during compilation and measurement.
This screen reports service and safety, not CPU benchmark results.

[Machine-readable measurements](measurements/forecast-candidate-screen.json) retain the gates, identity-set counts, and full evidence locations.
The durable evidence directory is `/home/dotwaffle/.cache/agents/podsim/forecast-contract-batch-20261003/forecast`.
Its `artifact-manifest.json` and `SHA256SUMS` verify the raw arms, restore states, ledgers, frozen inputs, source hashes, scripts, run receipts, and report patch.
The export excludes binaries and repository clones.
