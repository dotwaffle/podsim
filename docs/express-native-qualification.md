# Express native qualification

The native simulator and project version 4 passed this bounded qualification.
The explicit `express-v1` marker enables the approved Express profile.
The foundation APIs and project versions 1 through 3 retained their existing behavior.
The current project format has one version, 1, and the `orderContract` marker selects Express (see [operations](operations.md)).
This change does not activate a default or change a physical number.
The [approved contract](express-physical-encoding-contract-proposal.md) defines the complete encoding and consumer requirements.

Status 2026-10-07: the fleet limit is now 600 pods, the Express order bound is 17,000 orders, and the checkpoint cap is 100 MiB.
This record describes the tested source.

## Native and project behavior

`OrderContract` is immutable after construction.
The explicit constructors, prepared constructors, validation helpers, snapshots, saved states, and restore inputs carry this marker.
Reset and Clone preserve it.
The foundation `LookupVehicleClass` still reports Express as physically unsupported.
Contract-aware lookup permits its 20 seats, 10-meter body, and new whole parties from 1 through 20 passengers.
New parties of 9 through 20 passengers do not receive a legacy-party flag.

Express stores at most 20 rider records, including completed history.
An authored service can pool 20 whole parties with the same service ID and directed pair.
The authored party limit and 20-passenger capacity still apply.
On-demand service retains its eight-party and eight-stop bounds.
Consent and accepted party facts remain immutable.
History retirement removes the oldest completed rider and aligned boarding record together.
It preserves each retained boarding origin and distance baseline.

Pending orders and aggregate outstanding orders each have an 8,600-order limit.
The aggregate includes pending and noncompleted onboard orders.
Completed history uses the per-pod storage limit and does not count toward that aggregate.
Overflow refusals preserve counters and accepted state.
Logical recovery retains whole parties and their order IDs, options, and boarded status.

Explicit native constructors reject bounded-input violations before geometry preparation.
The native bounds match the approved project budgets, including UTF-8 IDs, curve controls, lane degree, junction pairs, and total blocks.
Foundation constructors retain their existing validation and error receipts.
Waiting routes require a known, uniquely assigned pod and at most 300 cached bindings.
Dispatch clears cached routes when it clears or changes their pod assignment.
Large vehicles reject virtual, buffer, and compact links in either class order and either restore tier.

At the tested source, project version 4 required the exact marker.
Older versions rejected marker presence, including null and empty strings.
Raw decoding rejects duplicate or unknown markers.
The separate `DecodeCanonicalJSON` prerequisite retains the typed decoder and validation path.
It permits an 80 MiB raw checkpoint component with a 10 MiB canonical project limit.
Ordinary project decoding keeps its 10 MiB raw object limit.
Root committed this prerequisite separately as `d95137591209f9f279546b9b200e5b950b1c9193`.

## Bounded native evidence

The final run contains 84 independent native simulation ledgers and 985,214 checked ticks.
It saves 178 raw state, snapshot, and ledger files.
Each ledger preserves observed order IDs, party sizes, consent, service identity, and available timing facts.
The [measurement record](measurements/express-native-qualification.json) lists each simulation's observation endpoint and unresolved order IDs.

| Workload | Result and fixed bound |
| --- | --- |
| Seven mixed class pairs, both curves and buffer choices | 28 following and merge arms finish both parties within 36,000 ticks |
| Curved following, both class orders | Seven arms finish within 60,000 ticks |
| Bank and parking fetch | The 20-passenger party finishes within 36,000 ticks |
| Mixed parties of 12, 9, and 8 passengers | The 12 and 8 parties share all 20 seats. The 9 party waits whole. All three finish within 36,000 ticks |
| Authored 20-singleton service with one pending party | All 21 orders finish across native and cold continuation within 36,000 ticks |
| Repeated occupied pickups | Twenty stored records retire in order through 22 additional pickup cycles. All 26 accepted orders finish |
| Stationary endpoint, stopped curve, and compact neighbor | Blocked phase endpoints retain unresolved orders. These are safety observations, not completed-service results |
| Body-plane transitions and low-speed lane entry | Phase prefixes check the required physical transitions. They do not measure completion |
| Foundation and observation parity | Saved states match for 12,000 old-only ticks and 40 offers. Plain and observed states match for another 1,200 ticks |

Every motion tick checks the current authored lane limit independently of `SafetyObservation.Check`.
The oracle also checks literal 2 m/s² acceleration and braking, position increments, owned stopping distance, and actual resource-owner IDs.
Large pairs must retain 20 meters of separation on these common-plane fixtures.
Large origin and track tails retain their owners for 20 meters.
Small owner delegation requires an existing native certificate or leader chain.
The native conservation checks run on every tick.
Following and merge arms report a minimum large-pair center gap of 53.694440 meters.
This number is specific to those arms, not a universal network minimum.

Cold restore covers each observed occupied and empty production phase, origin and trimmed route tails, and intermediate unloading.
The Continuing case uses an authored accepted-state fixture.
It is not an observed live phase.
Ordinary restore resets speed to zero and reconstructs forward grants.
It preserves checked pose, current and past ownership, whole-party facts, and distance totals.
Cold and warm twins disable new buffers, platooning, and occupied pickups.
They continue accepted cohorts to the original absolute 36,000-tick cap.
All those accepted cohorts finish.
The twins do not claim identical live futures or timing.
Past completions without a saved per-order alighting tick remain classified as historical completions with unavailable timing.
A native-to-cold handoff has separate ledgers.
An unresolved prefix is not an additional lost or completed party.

The recovery fixture uses 300 Express pods and 300 authored services on the existing valid 300-station loop.
It contains 2,600 pending orders and 6,000 active singleton orders.
Three logical recoveries retain all 8,600 order identities and options.
New submissions, selected-pod requests, and an 8,601-order restore reject overflow.
The raw native saved state uses 1,319,069 bytes.
One isolated precompiled recovery sample takes 0.30 seconds and reaches 129,488 KiB peak process RSS.
This receipt does not measure packed checkpoint size or browser retention.

## Compiled caller controls

All caller controls compile and pass.
No compile failure counts as a mutation kill.
The frozen native source remains unchanged after each overlay test.

| Actual caller mutation | Observed result |
| --- | --- |
| Foundation constructor selects Express | Root's compiled assertion fails on changed foundation bytes. The caller is byte-identical in the final source |
| `canJoin` bypasses whole-party capacity | Compiled assertion fails on an overfilled 12/9/8 cohort |
| Admission bypasses aggregate bound | Compiled assertion fails on the 8,601st outstanding order |
| Pod fit bypasses the class path | Compiled assertions fail on station, berth, and lane allowlists |
| Resource release uses the small origin tail | Compiled retained-owner assertion fails at tick 209 |
| Motion bypasses lane-entry braking | Independent lane assertions fail at 14 m/s on the 2.5 m/s lane |
| Restore caller alone bypasses large-link rejection | The mutation survives because saved-contract validation still rejects the link |
| Restore and saved-contract callers both bypass large-link rejection | Compiled assertions fail before the virtual-link restore tier. Other buffer and compact certificates still reject their cases |

## Gates and retained failures

Fresh gopls references cover changed symbols and actual mutation callers.
Final diagnostics have no errors.
Lint reports zero issues.
Vet passes.
The full native suite passes in 221.932 seconds.
The full project suite passes in 13.461 seconds.
Scoped race passes in 139.462 seconds for native tests and 1.013 seconds for project tests.
It excludes the 8,600-order shape and large raw JSON case.
The standalone canonical prerequisite also passes the full foundation project suite.

Earlier failures remain in the export.
The first qualification found foundation pending and restore-error regressions, an over-budget fixture, and an incomplete small-owner oracle.
The corrections preserve foundation behavior and unchanged physical bounds.
A second recovery fixture crossed a nonincident arrival lane and failed geometry validation.
The final fixture uses the existing valid 300-station loop.
A missing evidence directory stopped the first final-source launch.
The identical source passed after the directory existed.
The mutation classifier initially expected a later tail assertion.
The earlier retained-owner assertion had already killed that mutation.

Each gate records its PID namespace, command, directory, source pins, log, exit, deadline, stop command, and relaunch command.
The durable export includes sources, native states, ledgers, scripts, mutations, and hashes.
`MANIFEST.sha256` verifies that export.
It contains no binaries.
Session, packed encoding, browser, and checkpoint consumer proofs remain in their separate qualification reports.
