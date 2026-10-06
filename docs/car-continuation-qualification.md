# Offline car continuation qualification

The version-1 checkpoint implementation uses deterministic replay of an owned finite plan.
It does not restore a native saved state as an exact continuation.
The [measurement receipt](measurements/car-continuation-qualification.json) records the tested executable, inputs, costs, and source pins.
The [approved design](car-continuation-contract-proposal.md) defines the file and acceptance rules.
This qualification covers the selected foundation projects and schedules below.
It does not establish cross-build replay or physical admission for every supported project.

## Use

Build `cmd/parkride` from a clean, identified checkout with Go 1.27.1 and `GOEXPERIMENT=jsonv2`.
Keep the same executable for resume.
Checkpoint operations reject dirty or unidentified builds.
Ordinary reports retain their existing version-1 contract.

```sh
parkride -project project.json -plan car-plan.json -duration 20m \
  -queue-limit 200 -stop-at 1800 -checkpoint-output car-checkpoint.json \
  -output car-partial-report.json
parkride -checkpoint-input car-checkpoint.json -replay-timeout 45s \
  -checkpoint-output car-final-checkpoint.json -output car-final-report.json
```

`-stop-at` is an absolute native tick, with 60 ticks per second.
The origin horizon and queue limit cannot change on resume.
Resume requires a positive wall-clock timeout.
Replay first validates a private candidate, then advances the accepted run.
SIGINT cancels parsing or replay without publishing a candidate report or checkpoint.
Structured stderr messages distinguish `Replay candidate` progress from `Replay accepted`.

The Go interface is `Run.EncodeCheckpoint(context.Context, io.Writer) error` and `DecodeCheckpoint(context.Context, io.Reader, ResumeInput) (*Run, error)`.
A new run must opt into continuation with a verified implementation identity at creation.
An ordinary old run cannot acquire checkpoint eligibility later.
The CLI obtains source revision, clean-build facts, and the SHA-256 of its executable.
An internal caller must supply an independently verified identity.
Checkpoint hashing detects corruption; it does not authenticate an author.

## Evidence and limits

The principal executable came from clean revision `d52e31d0b5192998984086a1021252e264003040`.
Its SHA-256 is `c285d53e41475cd96159982cbba3e218bb66bf3d1650c58caaff4fb2735d2ce7`.
It used Go 1.27.1, jsonv2, linux/amd64, `GOGC=400`, and `GOMAXPROCS=2`.
Inputs and executable hashes were recorded before each study.
The tested binary is retained in the durable evidence export.
Later main integration needs its own identified-process bridge; an old checkpoint is not migrated to that new identity.

Independent CLI processes compared complete checkpoint bytes and report bytes.
Each checkpoint carries separate native, ledger, observation, and rolling trace hashes.
The observation hash covers native `Snapshot`, effective `Policies`, and the car report.
It includes observable pod speed that native `SavedState` omits.
The trace includes tick zero and every completed tick through the saved boundary.
Unit tests also compare replay with an uninterrupted run's exact `Clone` and future native/controller observations.
Same executable identity alone is not the determinism argument.
The source audit checks ordered dispatch, positioning, compact-group, and pickup-controller paths.
Maps on these selected paths supply lookups or an existential answer; they do not select a competing pod identity through iteration order.
This audit and these tests do not prove all possible schedules.

| Study | Selected evidence | Result |
| --- | --- | --- |
| Finite plans | Nine cases, 187 CLI calls: Compact and Group, private/shared whole parties, completed/full-lot, refused, stranded, censored, empty | Independent boundaries and complete future bytes match; ordinary and traced reports match. |
| Native compact queue | Actual `compact-buffer-v1` certificate at tick 53,321; seven CLI calls | Independent files and resumed future match. Active native replay cancellation exits in 5.14 ms without acceptance or output. |
| Pickup cooldown | Two authored private trips trigger replacement at tick 2,221; 12 CLI calls | Independent boundaries at 2,221, 3,000, and 4,021 match. Public replay and Clone compare 1,200 future ticks with nonzero cooldown guard calls. |
| Wide finite plan | 29,873 records, canonical plan 10,485,519 bytes; next record would exceed 10 MiB | Exact encode and replay match. Checkpoint size is 20,377,304 bytes. |
| Escaped identifiers | 14,462 records with 64-byte escaped itinerary and car IDs | Exact encode and replay match. Checkpoint size is 15,275,790 bytes. |
| Offline pending queue | 2,999 pending native offers | Encode and replay match; no live-session 2,600 ceiling is imposed. |
| Canonical transport | Interior project whitespace above 10 MiB, near-cap plan plus whitespace, reordered outer members | Accepted and re-encoded bytes match the original; source file bytes stay unchanged. |
| Maximum validated origin | 5,000 nodes, 8,000 lanes, 300 stations and pods, one 200-berth/eight-bank station, eight profiles, each with 24 bands and 2,450 flows; 14,705 future private itineraries | Public tick-zero encode is 25,974,736 bytes. This is an encoder qualification without traffic or replay claims. |
| Foreign identity | Checkpoint produced by the prior identified executable | Rejected before replay; no report is emitted. |
| File boundary | 80 MiB of whitespace and one additional byte | The bounded file reaches the JSON EOF check; the larger file fails the 80 MiB reader guard. |

The nine-case process study took 80.33 seconds across its calls.
The compact and cooldown studies took 29.68 and 9.44 seconds.
These totals include process startup and serialization, not only native replay.
The largest measured CLI resident set was 396,044 KiB for the 80 MiB parser-boundary case.
This is observed process memory, not a promised memory ceiling.
SIGINT during wide tick-zero validation exited in 20.36 milliseconds.
Cancellation takes effect between indivisible native steps and encoding operations.
No cancellation latency bound is claimed for every supported project.

A prior clean executable at `93a556903c4ea0826a4a5a8ee55365554027071e` ran an ordinary, traced, and replayed 24-hour fixture through 5,184,000 ticks.
Complete reports and final checkpoint bytes matched.
A stranded held car survived midnight; an arrival at the horizon held its slot but made no pod offer.
The original receipt assumed itinerary input order; the corrected receipt looks up source IDs after native sorting.
No runtime rerun or source change supplied that correction.
This is historical 93a5569 temporal evidence, not a fresh d52e31d 24-hour measurement.
The `Run` source hash is unchanged between those implementations.
A separate ordinary-run timing probe on that prior source measured maximum indivisible run steps of 13.40 microseconds for 5,024 singleton steps and 2.59 milliseconds for 2,000 steps with a 3,000-order origin.
Those values include ledger work and do not isolate native `StepResult` time.

## Boundaries and failures

Complete files are limited to 80 MiB.
Canonical origin project and normalized plan are each limited to 10 MiB.
Transport whitespace does not consume either canonical component allowance.
The project adapter preserves authoritative native presence, historical null, and typed validation rules.
Public project decoders retain their existing raw 10 MiB limit.
The retained-ledger charge is `4096*N + 512*L`, limited to 256 MiB.
The complete-file preflight is `P + A + S + 1024*N + 128*L + 8192`, limited to 80 MiB.
`P`, `A`, and `S` are the measured canonical origin-project, plan, and native snapshot bytes.
`N` and `L` are itinerary and lot counts.
The separate observation encoding limit `O <= 80 MiB` applies while streaming into SHA-256.
The file stores only fixed-size observation and trace hashes, so it does not duplicate the full report or snapshot.
No bound truncates retained state or changes native admission.

The widest ledger record has 13 integer fields, including both new boarding receipts.
The positive-extrema named-type test measured a 596-byte record, a 60-byte lot record, and a 3,503-byte escaped envelope.
The independent codec test uses all 13 signed 20-byte integer extrema, measuring a 609-byte record, a 62-byte lot record, and a 3,491-byte envelope.
Its negative clocks, request IDs, and occupancy are width probes, not admitted state.
Those values fit the 1,024, 128, and 8,192 byte charges.
The maximum validated origin measures `P=10,484,518`, `A=10,485,129`, native snapshot 135,018, and ledger 4,867,415 bytes.
A separate named-type codec fixture combines maximum route, boarding, rider, stop, compact-group, escaped-ID, and signed-integer widths.
Its fitting complete encoding is 72,104,395 bytes.
Four larger envelopes measure 297,507,537; 1,265,591,604; 514,549,468; and 884,203,971 bytes and fail the 80 MiB bounded writer.
This fixture measures encoding and rejection, not physical state reachability or replay.
The measurement receipt records each exact shape and byte count.
Independent codec maxima must not be read as reachable physical states.
Some independent maxima cannot coexist: 60,000 berth nodes exceed the 5,000-node limit, and all maximum flow or fully escaped network-ID collections exceed the 10 MiB component cap.

The preallocation scanner rejects invalid UTF-8, duplicate decoded names, unknown fields, null required values, wrong scalar types, excess nesting, and excess path-specific arrays before typed allocation.
It checks native pending plus retained rider records against two offers per itinerary.
Routes use actual origin node/lane counts, and compact memberships must be disjoint.
Native field lists are frozen under `sim-saved-state-v1`; future fields are not accepted implicitly.
Foundation-only checks reject the `orderContract`, `couplingContract`, and `incidentContract` markers in ordinary runs and checkpoint operations.
Car held-slot accounting remains separate from native pod parking storage.
Group operation follows current native validation.
This implementation does not activate Express.

| Actual compiled caller mutation | Control | Intended assertion kill |
| --- | --- | --- |
| Exact executable comparison omitted | Identity assertion passes | Error changes to the later origin-hash guard; the early identity assertion fails. |
| Independent native comparison omitted | Native metric assertion passes | Repaired native metric is accepted; the required rejection assertion fails. |
| Held-slot conservation omitted | Conservation assertion passes | Error changes to later replay-ledger comparison; the conservation assertion fails. |
| Half-open offer condition changed to include the horizon | Endpoint offer tick -1; submitted 0 | Actual ordinary CLI offers at tick 60; submitted 1. |

All mutants compiled.
Each control passed, the intended assertion failed under mutation, and exact source hashes were restored.
Layered rejection in the identity and held-slot cases is retained; those kills qualify early rejection semantics.
They do not claim the full decoder accepts the damaged file.

Atomic publication writes a sibling temporary file with mode 0600, syncs and closes it, checks aliases, renames it, then syncs the containing directory.
Tests inject write, file-sync, close, rename, and directory-sync failures.
Before rename, the previous target remains intact.
After rename, a directory-sync failure reports durability uncertainty.
Source, report, and checkpoint targets cannot alias through hard links, symlinks, or a symlinked parent of an absent leaf.
An exclusive target lock rejects concurrent publication.
A crash can leave a stale lock or temporary file; the operator must inspect and clear it before retrying.
No live server, session persistence, project migration, network access, or physical default changes are included.

## Gates and reproduction

Fresh diagnostics reported no diagnostics.
Relevant parkride, CLI, and project suites, scoped race tests, vet, lint, and bounded parser fuzzing passed.
After the cooldown test addition, parkride and CLI suites passed in 13.18 and 0.51 seconds; the cooldown race test passed in 5.26 seconds.
The separate final shape package suite passed in 27.67 seconds, with no diagnostics and passing vet and lint.
The prior full continuation race suite passed before that test-only addition.
The receipt records each gate and its exact exit marker.
Earlier unsuccessful controller fixtures and an initial missing queue-limit harness failure remain in the evidence export.
They are excluded from qualification claims.

The durable evidence manifests verify source copies, patches, input pins, actual outputs, binary hashes, run metadata, logs, and exit markers.
Measured controller scripts are archived before parameterization.
The final reproduction scripts accept explicit paths:

```sh
GOGC=400 GOMAXPROCS=2 python3 process-study-v2.py \
  --binary /absolute/path/parkride --source /absolute/path/clean-checkout \
  --output /absolute/path/evidence/process
GOGC=400 GOMAXPROCS=2 python3 cooldown-study-v2.py \
  --binary /absolute/path/parkride --source /absolute/path/clean-checkout \
  --output /absolute/path/evidence/cooldown
GOGC=400 GOMAXPROCS=2 python3 controller-study-v2.py \
  --binary /absolute/path/parkride --source /absolute/path/clean-checkout \
  --output /absolute/path/evidence/compact
```

Keep the three scripts and `experiment-project-export.log` together.
Use assigned scratch for outputs and `TMPDIR`.
The final runners record exact PID namespace, process group, command, input and binary pins, deadline, log, exit marker, and stop/relaunch instructions.
The watchdog supports shared-file cancellation when another process cannot see its PID namespace.
