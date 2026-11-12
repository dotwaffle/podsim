# Generated-order consent contract proposal

Status: approved under the maintainer's grant of November 4, 2026.
Status 2026-11-05: `stateVersion` is now 11, `StreamVersion` is 9, and the member list is `state_v11_members.txt`, so the values of 10, 8, and `state_v10_members.txt` below are history.
On November 12, 2026, the boarding record decision raised the saved-state version to 12 and the stream version to 10, and the member list is now `state_v12_members.txt`, so text about `stateVersion` 11, `StreamVersion` 9, and `state_v11_members.txt` describes the earlier design.
The maintainer asked for this setting with private as the default, and asked for a second review after the first one found a blocker.

## Goal

Generated passengers cannot share a pod today, whatever the shared ride settings say.
`internal/session/demand.go:226` and `:313` call `RequestTrip` and `SubmitTrip`, and an order without consent is private (`internal/sim/order_options.go:63-64`).
A shared order needs consent from each party (`internal/sim/order_options.go:161`).
This proposal adds one project setting so that a project can make its generated orders shared.
A project that does not set it keeps today's behavior and bytes.

## Setting

The project gains one optional top-level member, beside the shared ride settings.

| Member | Values | Absent means | Applies to |
|---|---|---|---|
| `generatedOrderConsent` | `private`, `shared` | `private` | Each order that the demand run offers: all patterns, including `rail-arrivals` and `rail-services` |

The Go field is `Config.GeneratedOrderConsent sim.SharingConsent`, with the tags `json:"generatedOrderConsent,omitempty" digest:"ext=15"`.

The member is not in `DemandConfig`, for two reasons.
The command digest hashes untagged fields even when they are empty, and `DemandConfig` occurs at two command paths, so one extension tag cannot serve both (`internal/session/receipt.go:127`, `internal/session/digest_extension_test.go:48`).
`validateWidestSize` measures each project with `widestDemand`, so a wider `DemandConfig` would refuse a valid project within 27 bytes of the cap (`internal/project/config.go:422`).
A top-level member has one digest path, and it counts only when a project sets it.

## Rules

- `validateSharedRideSettings` refuses any other value with `generated order consent must be "private" or "shared"`.
  This check runs last in that function, so the order of the existing checks does not change.
- A setting of `shared` with a party limit of one is valid and pools nothing, as a shared manual order does today.
  It is not an error, because the editor would then refuse an edit order that passes through this state.
- The setting changes with the project, so it takes effect when an apply rebuilds the demand run.
  The live `demand` command does not carry it, and a demand run that the command rebuilds keeps the project's consent.
  An apply that changes only this member resets the run, as any project apply does, so waiting and riding orders end (`internal/session/session.go:909`).
- Demo orders stay private.
  The demo is a fixed script (`internal/sim/demo.go:92,100`), and the setting does not apply to it.
- `cmd/compare` keeps its own `--sharing-consent` flag and ignores this member.
  A study names its consent in its flags and in its report, so a project file cannot change a study without a trace.

## Data flow

`demandInput` gains `consent`, which each of the seven `newDemand` sites supplies from the project.
`demandRun.offer` and the connection branch of `releaseServices` call one helper that builds `sim.TripOptions{From, To, SharingConsent}` and calls `SubmitTripOptions`.
An empty consent stays empty, and `NormalizeTripOptionsWithOrderContract` makes it private, so a private project gets the same result.
No random draw is added, so the seed stream does not change.
The saved demand state does not carry the consent, because restore reads it from the saved project.

## Editor

The shared ride section gains a select, **Generated passengers**, with **Private** and **Shared**.
Shared writes the member, and Private deletes it, so that an exported project has today's bytes.
The browser merges worker patches with an object spread (`web/editor.js:3662`), so a Private edit must delete the key explicitly, as the map removal does at `web/editor.js:3741`.
`internal/editormodel` adds a check in the `check_demand.go` table and a case in `engine.go` and `edit.go`.
Normalization leaves an absent member absent, unlike the other shared ride settings.
The native client gets no new control.

## Format

| Item | Effect |
|---|---|
| `stateVersion`, `StreamVersion`, `StateMediaType` | No change. Versions 10, 8, and `state-7` are not yet pushed, and they land in the same push. |
| `internal/session/testdata/state_v10_members.txt` | Gains `project.generatedOrderConsent`. |
| Other goldens, `command_digests.txt` | No change, because an unset extension field writes nothing. A test run proves this. |
| Digest extension table | Gains N 15 at `Project.GeneratedOrderConsent`. |
| `widestDemand`, `validateWidestSize` | No change. A project with the member is larger by the bytes that it writes. |
| `TestPlainStateFileWorstCaseSize`, `TestPlainStreamMaximumEncoding` | No fixture change is needed. The scalar fill does not reach the saved project, and that project fills its own cap. Stream state has no project. |

## Failure modes

- A rider of a `rail-services` connection can share a pod, so a detour can make that rider miss the connection.
  The connection counts already report a miss, so this is a result of the setting and not an error.
- Pooling under generated load has run only through `cmd/compare`, which does not use `demandRun`.
  A session test runs the path end to end.
  A refused offer takes the existing route: `Skipped` and `Error`.

## Test plan

| Test | Proves | Mutation that must fail it |
|---|---|---|
| `Validate` table | Accepted values, refusal text, refusal order | Remove the check; move it before the party limit check |
| Session run, party limit 8, drop-offs, two generated orders on one route | Shared consent pools, private does not, with the same seed | `offer` drops the consent |
| Same, with `rail-services` | The connection branch carries the consent | The connection branch drops the consent |
| Same, with a full queue and with a refused request | `Skipped` and `Error` stay right on both branches | None: this pins the counts |
| Session run, party limit 1 | Shared and private give the same pod positions and order counts | None: this states the limit-one rule |
| Round trip, goldens, command digests | An absent member stays absent; digests and goldens stay | `omitempty` removed; the `digest` tag removed |
| A command digest with the member set | The digest differs from the digest without it | The `digest` tag removed |
| Lifecycle table: start, consent-only apply, save and restore, restore then a live `demand` change, reset, demo exit then enable, rewind | After each step, a new generated order has the project's consent, read from the decoded HTTP and stream orders | Each `newDemand` site, and the `configure` caller at `internal/session/session.go:871`, that drops the consent |
| Editor model, normalization, export and import, web tests | The select writes the member; Private deletes it from the draft and the export; a refused value; absent stays absent | Private leaves the old key; normalization adds the member |

Run `mise run lint`, `go fix -diff`, the `internal/session`, `internal/project`, `internal/editormodel`, and `internal/view` tests, and `mise run test:web`.

## Documents

The README project format section lists the member.
The README says that generated passengers ride alone unless the project sets shared consent, and it loses the claim at its line 428 that a party can join a pod in the demo.
`docs/compare-sharing-consent.md` says that compare ignores the member.

## Alternatives rejected

- A `sharingConsent` member in `DemandConfig`, the first draft: the review found that it changes every command digest and takes 27 bytes from the admission cap of every project.
- A shared fraction: it needs a random draw for each offer, which changes the seed stream of a shared project, and no study needs it.
- A consent for each demand flow: it adds members to a capped table, for a use that no preset has.
- Consent derived from the party limit: the sim keeps consent and capacity apart on purpose (`PartyAdmissionInput` in `internal/sim/order_options.go`), and the compare study depends on that.
