# Durable offline car continuation proposal

Status: **approved on October 3, 2026; implemented and qualified**.
The user approved item 6b and the decisions below with the 6b/8b batch.
Item 6a used source revision `53321e4c5decc6c661df64a40378ee37614dcffc`.
That revision identifies the original audit, not a checkpoint-capable executable.
The [qualification](car-continuation-qualification.md) records the tested implementation, scope, and limits.
The [measurement receipt](measurements/car-continuation-qualification.json) records its source and executable identities.
The approved design below preserves the original proposal and acceptance conditions.
The project, session, stream, plan, and report formats retain their existing contracts.

Station buffers and compact station queues were removed on October 7, 2026, because measurements showed that they lowered station entry throughput.
Clauses that name buffer or compact certificates, policies, or queues now describe cases that cannot occur.
The checkpoint payload is the saved simulation state, which no longer has their members.
A checkpoint that still has one fails as an unknown member.
The checkpoint version and the native encoding name are unchanged.

## Recommendation and source constraints

Add one local, finite-plan checkpoint format to `internal/parkride` and `cmd/parkride`.
Resume by deterministic replay from the original inputs, then compare the joint checkpoint before advancing it.
Use Go standard-library JSON, SHA-256, file, and cancellation facilities.
Do not restore the native snapshot as an exact continuation.

[`Run.Clone`](../internal/parkride/run.go) copies the simulation and car ledger together.
[`Simulation.Clone`](../internal/sim/clone.go) preserves mutable motion and policy state.
[`ExportState`](../internal/sim/state.go) exports a recovery state, not every field that `Clone` copies.
Ordinary saved pods have no speed field.
The simulation also retains pickup cooldowns and predictive policy history outside `SavedState`.
This finite-plan run can enable pickup reassignment but does not select predictive routing.
Export can omit routes that exceed native saved-route bounds.
[`RestoreState`](../internal/sim/state.go) can use logical recovery, demote pods, drop requests, or report untimed logical completions.
Even physical restoration can demote pods.
These behaviors cannot prove the same future car release ticks or lot admissions.

Replay proposes to reconstruct this state through the same `NewRun` and `Step` calls.
Same executable identity alone does not prove determinism.
Qualification must audit map-order-sensitive dispatch and policy paths, then compare independent fresh processes.
Only qualified new version-1 runs can publish checkpoints.
The origin must include the complete project, normalized plan, absolute horizon, queue limit, and executable identity.
`NewRun` configures sharing, platoons, experiments, itinerary demand weights, positioning, and zero declared demand rate.
The native guarded rate fallback reads retained requests, not a separately saved random stream.
This run has no daily or rail order generator.
Its finite plan supplies every offer and both refusal policies.
No caller may inject another native request, policy change, pause, demo, or fleet edit into a checkpointable run.

Replay has a cost: it executes every original tick through the checkpoint.
The existing 24-hour horizon bounds this work at 5,184,000 native steps, plus tick-zero events.
There is no claim that this cost fits a short wall-clock interval.
Direct snapshot continuation would need a separate exact-state contract and qualification.

The earlier [service proposal](service-contract-proposals.md) describes Group as unavailable.
At this source revision, [`LookupVehicleClass`](../internal/sim/vehicle_class.go) permits Group physical operation and still rejects Express physical operation.
Use current native project, class, geometry, and safety validation.
Do not restore the earlier proposal's obsolete Group restriction or activate Express.

## Proposed version 1 file

Use uncompressed UTF-8 JSON with format `podsim-car-continuation` and version `1`.
The outer object has exactly `format`, `version`, `checkpointID`, and `payload`.
All four members are required and nonnull.
`checkpointID` is lowercase SHA-256 of the canonical payload bytes.
Hash the typed payload with `encoding/json/v2` and `json.Deterministic(true)`.
Accept JSON whitespace and member order differences, but reject duplicate decoded names and unknown members.
Check each present scalar type before typed decoding can convert null to a zero value.
Only the origin project retains its authoritative historical null exceptions.
Native and ledger required values cannot use null.
Do not hash the transport whitespace.
The hash detects damage and accidental mixing.
It does not authenticate a file or its author.

The payload has these required, nonnull members:

| Member | Proposed meaning and validation |
| --- | --- |
| `runID` | SHA-256 of the canonical origin object. Same inputs and executable produce the same run ID. |
| `origin` | Exactly `project`, `plan`, `projectHash`, `planHash`, `horizonTicks`, `queueLimit`, `reportBuild`, and `implementation`. |
| `tick` | Nonnegative integer at most `origin.horizonTicks`. Must equal the native tick and ledger tick. |
| `phase` | Literal `post-tick`. No partial phase can be saved. |
| `nativeEncoding` | Literal `sim-saved-state-v1`. This freezes the native field shape at the source pin. |
| `native` | The canonical JSON shape of `sim.SavedState`, including native boarding records and compact certificates. |
| `nativeHash` | SHA-256 of canonical `native`. |
| `ledger` | Exactly `lastTick`, `records`, and `lots`, as defined below. |
| `ledgerHash` | SHA-256 of canonical `ledger`. |
| `observationHash` | SHA-256 of canonical `{snapshot, policies, report}` from the native full `Snapshot`, effective `Policies`, and owned car `Report`. |
| `traceHash` | Fixed-size rolling hash of per-tick native, ledger, and observation evidence. |

The file embeds only the origin, native snapshot, and ledger objects.
It stores `observationHash` and `traceHash` as two 64-digit lowercase SHA-256 strings.
It never embeds a full `Snapshot`, `Policies`, `Report`, observation object, or per-tick record array.
The decoder rejects those additional payload members.

The origin project has version 1 without `orderContract`.
Validate it with `project.Validate` and the current raw presence rules.
The origin plan uses existing lot and itinerary members.
Sort both arrays by ID and materialize effective private or shared consent before hashing.
Persist all explicit seats, capacities, durations, party sizes, and refusal policies.
Do not infer a physical value or replace an explicit zero.
The project and plan hashes use the existing canonical hash rules from [`NewRun`](../internal/parkride/run.go).
`reportBuild` preserves the original `RunInput.Build` string, with a proposed 256-byte UTF-8 bound for checkpoint eligibility.
It affects report provenance, not physical state.
The horizon and queue limit remain the original controls.
They cannot change during resume.

`implementation` contains exactly `sourceRevision`, `executableSHA256`, `goVersion`, `goExperiment`, `goOS`, and `goArch`.
Use the full source revision, lowercase executable SHA-256, and exact compiled Go build facts.
Require a clean identified build and `goExperiment` equal to `jsonv2` for version 1.
Reject an unidentified or modified executable when checkpointing or resuming.
Match all implementation members before replay.
If a supported project fails deterministic replay, reject its checkpoint instead of installing a close approximation.
The existing human report build string does not meet this identity requirement.
Cross-build and cross-platform replay require later qualification, even when a revision matches.
No timestamp or file path participates in run identity.

Seed the trace with SHA-256 of ASCII `podsim-car-trace-v1` followed by the raw 32-byte run ID.
After each stable boundary, hash the previous trace, the unsigned big-endian 64-bit tick, and three raw 32-byte evidence hashes.
The three hashes are `nativeHash`, `ledgerHash`, and `observationHash`, in that order.
Include tick zero and every successful tick through the checkpoint.
Retain only the rolling hash, not an unbounded list of tick records.
Use canonical JSON for the three evidence objects, with the original report build string.
Generate the observation object from the candidate runtime, not the supplied native snapshot or saved ledger.
Stream its canonical encoding through a byte counter into SHA-256.
Do not retain its encoded bytes.
Bound each observation encoding at 80 MiB as a proposed checkpoint-eligibility rule, separate from complete-file storage.
An exceeded bound invalidates checkpoint eligibility without truncating the observation or changing native admission.
Full `Snapshot` evidence includes ordinary pod speed that `SavedState` omits.
The trace makes intermediate observable differences detectable, even if the endpoint later matches.
These observables still omit private controller state.
Future-continuation qualification against an uninterrupted run and its exact `Clone` must cover that remaining gap.

`nativeEncoding` is a new offline encoding identifier, not a session saved-state version.
Native `RiderBoarding` records use their native named fields.
The session saved-state adapter instead encodes source-berth index tuples.
Do not send this object to the session decoder or treat those tuples as native records.
Freeze a member list and canonical fixture for this encoding during item 6b.
A later incompatible native field requires a new car file version or a separately approved migration.

## Ledger records and whole-party conservation

`records` contains exactly one entry per normalized itinerary, in the same order.
Each record uses the current `Record` members except `itinerary`.
Its array position identifies the immutable origin itinerary without duplicating it.
Each leg contains exactly `requestID`, `offeredTick`, `boardedTick`, `alightedTick`, and `reason`.
Use an empty reason when none exists.
`boardedTick` is a proposed checkpoint-only receipt, not a report-v1 extension.
`doorToDoorTicks` is required and uses `-1` until a completed round trip has its duration.
`lots` contains exactly one entry per normalized lot, in the same order.
Each entry has exactly `occupancy` and `peak`.
Lot definitions remain in the origin plan.
`lastTick` must equal the joint tick.

Keep current stage values: `planned`, `car-out`, `at-hub`, `outward-pod`, `activity`, `return-pod`, `retrieval`, `car-home`, and `terminal`.
Keep outcome empty for an unfinished record.
Terminal outcomes are `full-lot`, `stranded`, `recovered-refusal`, and `completed`.
Reject `error` outcomes and any run fault as resumable checkpoints.
The current report substitutes `censored` for unfinished outcomes in an owned copy.
A checkpoint must preserve the unfinished state, not import that report substitution.
Censoring does not discard an accepted request, release a slot, or create a successful outcome.

A leg has no attempt when its request ID is zero, its ticks are `-1`, and its reason is empty.
A queue refusal has request ID zero, a real offer tick, no boarding or unloading tick, and reason `queue-limit`.
Acceptance has a positive unique request ID and a real offer tick with no refusal reason.
Before boarding, its boarding and unloading ticks remain `-1`.
A boarded leg retains its actual boarding tick, including zero.
An unloaded leg retains both actual ticks.
Require `offeredTick <= boardedTick <= alightedTick <= tick` when those receipts exist.
A completion must never imply a boarding tick from an offer tick.

The current native `BoardedTick == 0` sentinel also represents real tick-zero boarding.
`SubmitTripOptions` can dispatch and board synchronously.
Checkpoint support must record boarding from native rider membership, not test whether `BoardedTick > 0`.
Only a new run that captures these receipts from initialization is checkpoint-eligible.
A historical run or report cannot acquire eligibility by guessing missing receipts.
Observe native rider membership after a native step and after each synchronous submission.
Retain the receipt in the bounded leg record before native completed display history changes.
This observer must not enable unbounded experiment histories or change admission.
Qualification must prove that it captures same-tick dispatch, occupied pickup, and subsequent completion.

The itinerary party size and consent apply unchanged to both legs.
For every retained native request, compare ID, hubs, party size, effective consent, and on-demand service with its leg.
Allow no express service ID or historical consent marker in this new finite-plan lineage.
Completed leg identities remain in the ledger even after native display history disappears.
No restore may split a party, truncate seats, invent consent, repeat an accepted offer, or convert boarding into completion.
Car admission holds one car slot, independent of party size.

Recompute held-car counts by lot and require equality with saved occupancy.
Require `0 <= occupancy <= peak <= capacity`.
A stranded car remains held and terminal.
A full-lot refusal never holds a slot and has neither pod offer.
A release needs a previously held slot and a valid retrieval chain.
A completed round trip needs both actual pod unloadings and actual home arrival.
An outward queue refusal can end as `recovered-refusal`, never `completed`.
Replay must reproduce peak occupancy as well as final occupancy.
Native `ParkingOnly` stations store pods.
They never supply car capacity, count car occupancy, or own held car slots.

## Joint boundary and deterministic order

Save only after tick-zero initialization or after a successful complete `Run.Step`.
The native simulation and ledger must be owned by the same goroutine during capture.
Capture their owned copies before either state advances again.
The checkpoint contains neither partially consumed completion receipts nor an event phase cursor.
A runtime submission or compact-queue failure can leave partial evidence.
Keep its existing error report, but refuse a new checkpoint from that state.
The optional checkpoint writer must not change successful ordinary run output when no checkpoint flags are present.

Retain the current native completion delivery order.
It follows native pod processing and rider order, not sorted request IDs.
Consume those receipts before the remaining car transitions at the tick.
Drain scheduled car events in `(tick, itinerary index, kind)` order.
The index comes from normalized itinerary ID order.
Keep the current kind comparison and same-tick zero-duration transitions.
Offer due returns in itinerary ID order, even when their original due times differ.
Process lot arrivals in `(arrival tick, itinerary ID)` order.
Offer the admitted outward leg immediately and drain its same-tick retrieval or home events before the next arrival.
Thus an outward refusal with zero retrieval can admit the next simultaneous car.
Persist no map iteration order, heap layout, or native pointer identity.
Replay recreates event queues, request bindings, arrival cursor, compiled times, and terminal counts.

The offer interval is half-open: `[0, horizonTicks)`.
Car transitions and actual completions include the endpoint `horizonTicks`.
At that endpoint, a car can enter a lot with no outward offer and retain its slot.
A return due there remains unissued and censored in the report.
A checkpoint at the endpoint remains inspectable but cannot extend the run.
A checkpoint below the endpoint resumes at `tick + 1`.
It must not repeat tick-zero initialization or the saved tick's events on the accepted continuation object.
Replay initialization occurs only in the isolated candidate used to reconstruct that object.
Midnight neither clears lots nor repeats the plan.
Early terminal stopping still includes full-lot refusal, recovered refusal, and stranding.

## Offer identity and retries

The externally authored offer key is `(runID, itineraryID, leg)`.
`leg` is exactly `outward` or `return`.
The car ID identifies the parked car, not the native request counter.
Keep one attempt and its queue refusal or accepted native request receipt per offer key.
The native request ID is a binding within this run.
Never use it alone as an identity across files or another simulation.

Version 1 accepts no dynamic external offers, replay log, or session command receipts.
The complete origin plan is its external offer source.
Adding an itinerary, changing consent, or retrying a refused leg creates a different plan and run identity.
Do not merge checkpoint records from another run, even when itinerary IDs match.
Repeated reads of the same checkpoint create the same continuation in separate local processes.
This is reproducible offline computation, not a distributed exactly-once claim.
No network delivery occurs during replay.
Future live offers need their own durable acceptance and retry contract before admission.

## Restore, failure, and file publication

The proposed CLI adds optional `-checkpoint-input`, `-checkpoint-output`, and `-stop-at` to `cmd/parkride`.
`-stop-at` is an absolute native tick boundary, including zero, bounded by the original horizon.
It requires checkpoint output and never replaces the horizon.
On resume, it cannot precede the checkpoint tick.
Without `-stop-at`, checkpoint output captures the successful terminal or horizon state.
Fresh runs keep the existing required project, plan, duration, and queue-limit flags.
Resume takes origin controls from the checkpoint.
Reject those four flags on resume rather than accepting contradictory overrides.
Keep the existing optional report output and report version 1.
Do not add live routes, savepoints, or browser checkpoint import in item 6b.

The reader checks the file bound, shape, required members, identities, hashes, and value relationships before replay.
It then constructs a private candidate with the original `NewRun` inputs.
Replay through `tick`, including every original offer, actual completion, and refusal.
An early terminal or fault before that tick is a mismatch.
Capture candidate native and ledger objects and compare each with its saved canonical object independently.
Then compute its endpoint observation hash from the candidate full Snapshot, effective Policies, and owned Report.
Compare that hash with `observationHash` independently of the native and ledger comparisons.
Finally compare the candidate rolling trace with `traceHash`.
The candidate must pass every check before publication.
Compare project hash, plan hash, effective policies, joint clock, and run identity separately.
Only a complete match publishes the candidate as the resumed run.
Do not install the supplied native or ledger object and then repair it.
Do not call permissive native restore as a substitute for replay.
Qualified successful replay supplies the reconstructed simulation, including motion and controller state.
Snapshot equality alone cannot establish equality of omitted private state.

Cancellation must stop decode, replay, capture, or publication without accepting a candidate.
Use signal cancellation and a monotonic wall-clock replay watchdog.
Require an explicitly authored positive `-replay-timeout` on resume.
No timeout default or empirical performance promise is part of this proposal.
Check cancellation between native steps and before replacing an output.
A single native step remains indivisible.
Measure its longest observed duration during qualification and disclose cancellation latency.
Log quoted file names, checkpoint identity, target tick, replay progress, elapsed time, and the failure category to stderr.
Progress must never claim that a loaded file is already a restored run.

Write one complete checkpoint through a same-directory temporary file with mode `0600`.
Bound the encoder, sync and close the temporary file, then atomically rename it and sync its parent directory.
Protect project, plan, checkpoint input, and report paths against output replacement, including hard-link aliases.
Keep checkpoint input and output distinct in version 1.
Refuse unsupported file types and concurrent publishers to the same target.
Use one operator-owned local output target per invocation.
No cloud store, dependency, or shared live writer is proposed.

Before rename, any write, cancellation, sync, or close failure leaves the previous checkpoint untouched.
After rename, directory sync failure has uncertain crash durability.
Return an explicit publication error and do not claim that the previous checkpoint still occupies the target.
The operator can validate the complete target by identity and hashes before use.
A crash must expose either the previous complete file or the new complete file, subject to local filesystem rename guarantees.
Never select a leftover temporary file as a checkpoint.
A report and checkpoint are separate artifacts, not one atomic transaction.
A report write failure cannot invalidate a successfully published checkpoint.

Reject corruption, unsupported version, implementation mismatch, changed origin, mismatched clocks, invalid consent, duplicate offers, or replay mismatch.
Keep the rejected input unchanged for diagnosis.
Do not reset to an empty ledger, archive and overwrite the input, release stranded cars, or continue with only native state.
Resume from an earlier verified checkpoint or rerun the original inputs with the matching executable.
Require a new review for migration to a different executable.

## Compatibility and byte qualification

A version 1 project without a contract marker is a valid origin when native validation accepts it.
It keeps the current service, bank, class, and experiment rules.
The scan and the origin check refuse `orderContract` and project versions 2 through 5.
The frozen checkpoint member list does not change, because it does not name project members.
Car continuation does not import session saves.
Bare native snapshots, session saves of any version, report version 1, and plans are not car checkpoints.
Reject them with a format-specific error.
No automatic migration can recover missing car events from those files.
An original project and plan can start a new run, but that action is not recovery of an old ledger.

Reuse the existing 80 MiB saved-file ceiling for the complete uncompressed checkpoint.
Status 2026-10-07: by maintainer decision, the checkpoint cap is 100 MiB, the same as the saved-state cap.
Keep each canonical project and canonical normalized plan within the existing 10 MiB input ceiling.
The normalized plan can exceed its original bytes because it materializes consent.
Then checkpoint export must fail without changing the valid run or its original plan contract.
Retain the current ledger charge: `4096 * itineraries + 512 * lots <= 256 MiB`.
This charge is a retained-ledger rule, not an encoded-file proof or a peak-process-memory claim.

Before typed allocation, use a bounded token scan with path-specific array limits and a fixed member list.
Use the existing session scanner depth of 64 and object-member bound of 256.
Reuse the native saved-state route and project shape bounds.
Retain native identifier, refusal-text, boarding, and certificate bounds at their frozen paths.
Preserve native historical project null semantics, including a valid omitted geographic reference or `geo: null`.
Reject null checkpoint-required members without treating missing native omitzero members as missing car receipts.
Car record and lot counts must equal their origin counts.
Native pending and accepted records must fit the two-offers-per-itinerary conservation bound.
Do not copy the live session's 2,600 operating queue limit into offline admission.
Do not treat its saved-state parser limits, 2,600 orders or 8,600 with `express-v1`, as offline permission or qualification.
Reject oversized native arrays before replay, without dropping accepted parties.
Keep the existing offline queue control through 1,000,000.
These rules impose checkpoint eligibility, not a new native submission ceiling.

Use compact canonical output, no indentation, and no redundant itinerary definitions in the ledger.
For this proposed named record shape, budget at most 1,024 encoded bytes per car record and 128 per lot record.
The record has five top-level integers and four integers in each of two legs, for 13 total.
The two proposed boarding ticks increase the current record count from 11 to 13.
Fixed keys, bounded stage/outcome text, and two bounded refusal reasons complete the record.
Twenty-byte signed integers, JSON punctuation, and the longest allowed enums fit these conservative charges.
A lot record has two integers and fixed keys.
Reserve 8,192 bytes for the envelope and identity metadata, including `reportBuild` and both observation and trace hash strings.
Bound Go version and experiment identity text at 128 UTF-8 bytes each, with JSON escaping included in the envelope charge.
Use fixed full source hashes, 64-digit SHA-256 values, and compiled ASCII OS/architecture labels of at most 32 bytes each.
These are proposed encoding bounds, not authored physical defaults.
Item 6b must replace this paper accounting with exact widest-value encoder fixtures.

Let `P`, `A`, and `S` be the canonical project, normalized plan, and native snapshot byte lengths.
Let `N` and `L` be itinerary and lot counts.
Require `P + A + S + 1024*N + 128*L + 8192 <= 83,886,080` for conservative preflight.
No observation object length `O` enters this storage formula because the file contains only its hash.
The independent observation encoder enforces `O <= 83,886,080` bytes while hashing.
The actual bounded checkpoint writer remains authoritative.
At the retained-ledger count extremes, `N <= 65,536` and `L <= 524,288` independently.
Either ledger charge alone can reach 64 MiB of encoded budget.
Two 10 MiB inputs already leave less than 60 MiB for native state and the ledger.
Status 2026-10-07: the canonical project limit is now 32 MiB (`project.MaxFileBytes`), and the canonical plan limit stays 10 MiB (`internal/parkride` `MaxPlanBytes`).
In the 100 MiB checkpoint, the two inputs leave less than 58 MiB for native state and the ledger.
The test that fitted a checkpoint at the count limits was deleted with the other worst-case byte proofs.
Not every valid offline run can fit this checkpoint format.
A size failure must preserve all parties and leave the old output unchanged.

The [service byte proposal](service-byte-contract-proposal.md) recorded 83,301,596 bytes for its earlier conservative saved-session fixture.
That fixture leaves only 584,484 bytes below the save cap before adding car data.
It does not prove this new native encoding, boarding representation, or combined checkpoint fits.
The stream cap stayed 64 MiB at approval and receives no car checkpoint data.
Since 64b4f3f, the stream JSON cap is 65 MiB, and the gzip message cap is 66 MiB.
Do not add a text table, change the save cap, compress around the raw cap, or prune history without separate approval.

Qualification must measure encoded size, temporary buffers, retained memory, replay CPU time, and cancellation latency separately.
Avoid duplicate whole-file buffers and duplicate native simulations during validation.
The 256 MiB ledger charge does not cover the native fleet, input buffers, decoder, or report copies.
The transient observation includes full native Snapshot data and a Report that duplicates itinerary definitions, records, and lot definitions.
Those copies and their per-tick encoding cost require separate memory and CPU measurements.
The observation byte bound does not prove a peak-memory bound.
Hash encoding must avoid an additional full observation byte buffer.
Publish peak memory measurements for the admitted checkpoint fixtures.
Do not claim a total-process memory limit from a file-size bound.

## Consumers and adversarial qualification

The proposed public consumers are `cmd/parkride` operators and Go callers of `internal/parkride` checkpoint functions.
Propose an optional `RunInput.Continuation` identity value to enable origin ownership, boarding receipts, and trace capture from tick zero.
A nil value keeps ordinary runs unchanged and ineligible for checkpoint export.
Propose `Run.EncodeCheckpoint(context.Context, io.Writer) error` and `DecodeCheckpoint(context.Context, io.Reader, ResumeInput) (*Run, error)`.
`ResumeInput` supplies the current implementation identity.
The caller supplies the deadline through the context.
The package owns bounded decoding and replay, while the CLI owns atomic local file publication.
These Go entry points were proposed at approval, and they now exist in `internal/parkride`.
Report-v1 readers, browser authoring, project loaders, `cmd/compare`, live session storage, and stream consumers stay unchanged.
The existing offline browser continues to read reports, not resumable state.
Current [`cmd/compare`](../cmd/compare/main.go) owns different schedules and rail ledgers.
A car checkpoint cannot restore a comparison arm or a rail session.
The rail ledger's strict binding checks inform validation, but its degraded restore outcomes cannot release a car slot.

Item 6b must qualify actual encoder, decoder, CLI, and resume call sites.
The existing real [offline fixtures](../web/testdata/offline/README.md) supply completed, full-lot, recovered-refusal, stranded, censored, and empty-plan cases.
They are reports from CLI execution, not resumable checkpoints.
Generate new checkpoints with the implemented command before claiming continuation evidence.
Run no hypothetical checkpoint tests for this proposal.

| Required qualification | Acceptance condition |
| --- | --- |
| Every lifecycle boundary | Independent process replay matches native, ledger, full Snapshot, Policies, and report evidence at every tick. Continue both restored and Clone runs against the uninterrupted run at every lifecycle stage. |
| Event competition | Returns, completion, same-tick release, arrivals, zero durations, and native completion order reproduce request IDs and lot peak exactly. |
| Endpoint and overnight | Offers use the half-open interval. Endpoint completions occur once. Held and stranded cars survive midnight. |
| Immutable parties | Private/shared singleton and whole-party compact/Group fixtures retain both leg identities and consent. Express remains rejected. |
| Hidden native state | Moving ordinary pods, compact certificates, pickup cooldown, and occupied pickups reproduce through replay. Predictive policies remain unavailable through this finite-plan run. No degraded restore enters the run. |
| Damage and mismatch | Change each hash, source identity, clock, record count, receipt, held flag, party, consent, event-derived time, or native binding. Decode or replay rejects without output replacement. |
| Parser attacks | Escaped duplicate names, unknown/null members, invalid UTF-8, deep nesting, multiplication of nested arrays, oversized strings, and exact byte boundaries fail at bounded allocation. |
| Native histories | Completed riders that disappeared from display history still retain car leg receipts. Tick-zero boarding remains distinct from not boarded. |
| Repeated resume | Repeated checkpoint reads and interruptions do not duplicate offers or completion accounting. Different run IDs never merge. |
| File failures | Inject read, short write, close, file sync, rename, directory sync, cancellation, and path-alias failures. Preserve complete artifacts and disclose uncertain publication after rename. |
| Widest encodings | Measure exact combined bytes with escaped 64-byte IDs, maximum integers, maximum project shapes, routes, boarding records, compact groups, and retained ledger counts. Oversized states never truncate. |
| Wall-clock bounds | Interrupt parse and replay at selected ticks. Watchdog returns failure without publishing a candidate. Record peak memory and longest step latency. |
| Caller mutations | Remove identity comparison, joint snapshot comparison, held-slot check, or half-open offer guard at its actual caller. A targeted compiled assertion must fail. Build failure is no evidence. |

Use package tests, race tests for ownership and cancellation, CLI round trips, fuzzed bounded decoding, vet, lint, and prose checks during implementation.
Pin Go, executable, source, input hashes, commands, logs, exit markers, watchdogs, and output hashes in its evidence.
A selected service or continuation fixture does not qualify physical defaults, every project, or live persistence.

## Approved decisions for item 6b

| Decision | Approved scope |
| --- | --- |
| Continuation method | Qualified same-executable replay with independent native, ledger, full observable, and per-tick trace comparisons. Differential future continuation against Clone. No permissive restore path. |
| Schema | Separate uncompressed `podsim-car-continuation` version 1 with the origin and snapshot members above. |
| Boarding receipt | Enable new version-1 run origin ownership, bounded boarding receipts, native rider observation, and rolling trace from initialization. Keep report version 1 unchanged. |
| CLI | Optional input/output and absolute stop tick, origin-owned controls on resume, explicit replay timeout, and strict path separation. |
| Bounds | Existing 80 MiB complete-file and 10 MiB component caps. Separate proposed 80 MiB observation-hashing ceiling. Optional save rejects ineligible runs. No truncation or increased operating limit. |
| Identity and migration | Match the identified executable and build facts. Reject historical non-car files and cross-build migration. |
| Failure policy | Preserve input and prior complete output before rename. Reject partial/faulted state. Disclose uncertain durability after rename. |
| Scope | Offline finite plans only. No live session, stream, browser checkpoint, comparison-arm, or dynamic-offer persistence. |

The user approved all rows on October 3, 2026, including replay cost and checkpoint eligibility limits.
Any request for fast snapshot restore, live offers, larger files, horizon extension, or cross-build migration requires a revised contract.
