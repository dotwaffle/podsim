# Agent guide

This file gives coding agents the project rules that the code does not show.
Read the documents below for details.
Keep this file short, and point to other documents instead of copying them.

## Read first

- [README.md](README.md): how to run the server, the clients, and the editor.
- [docs/operations.md](docs/operations.md): server operation, the session state file, and the restore rules.
- [docs/protocol.md](docs/protocol.md): the client protocol.
- [PROJECT_BRIEF.md](PROJECT_BRIEF.md): product goals and architecture.
  It is long, so read only the section that you need.
- The contract documents in `docs/*-contract-proposal.md`.
  An approved contract is binding.

## Layout

- `internal/sim`: the deterministic simulation.
  It has no display or I/O dependencies.
- `internal/session`: the shared session, the saved state, the stream codec, and the HTTP state.
- `internal/statestore`: the saved-state store in gocloud.dev blob storage.
  `podsim.wasm` must not link it.
- `internal/project`: the project format and its limits.
- `internal/remote` and `internal/view`: the presentation client.
- `internal/editormodel` and `web/`: the browser editor and shell.
  `cmd/podsim` and `cmd/editormodel` also build for `js/wasm`.
- `cmd/serve`: the server.
  `cmd/compare`, `cmd/scenario`, and `cmd/projectcheck` are tools for experiments and checks.

## Checks

The `mise` tasks are the source of truth.
CI runs `test:race:*`, `qualify`, and `check:static`.
`mise run check` runs all of them.

- Quick loop: `go build ./...`, `go vet ./...`, and `mise run test:quick`.
  `test:quick` runs `go test -short ./...`, which skips the long tests.
- Before you hand back a change: `mise run lint` (actionlint, rumdl, vet, golangci-lint, and the wasm dependency check), `go test ./...`, and `mise run test:web`.
- The race tasks take a long time.
  Run the one for the package that you changed.
- Some tests have a wall-clock latency gate, for example `TestEmergencyChoiceLatency`.
  Under heavy host load they can fail.
  Run such a test again by itself before you treat the failure as real.
- Do not call `Draw` in a loop on an offscreen Ebiten image in a test.
  Ebiten does not flush the queued commands outside its game loop, and such a test grew to 39 GB.
- `mise` does not load its configuration in a worktree outside the repository directory.
  There, call the tools by the paths that `mise which <tool>` gives in the repository.

## Rules

- There is no backward compatibility until v1.
  Do not write migrations for old saves, project files, or protocol versions.
- The order of the refusals in restore, scan, and stream checks, and their error text, are part of the format.
  A refactor keeps both, and a test pins them.
- A saved state that is damaged or invalid moves aside, and the server starts a new session.
  Do not add code that recovers part of such a file.
  A file of more than 100 MiB is the only file that the server keeps.
- Do not change an approved contract, or its document, without maintainer approval.
- `TestPlainStateFileWorstCaseSize` and `TestPlainStreamMaximumEncoding` prove the plain saved-state, full-stream, and HTTP-state byte bounds.
  Express has no byte proof.
  After a change to a count or byte limit in `internal/project/config.go`, check the effect on these sizes.
  `web/editor.js` has a copy of `MaxLanes`.
- A change to the saved-state version (`stateVersion`) or the stream version (`StreamVersion`) also changes the golden files in `internal/session/testdata` and the version text in `docs/operations.md` and `docs/protocol.md`.
- Use the standard library first.
  JSON uses `encoding/json/v2`.
- `podsim.wasm` must not link cloud storage.
  `mise run lint` checks this.

## Writing

- Markdown has one sentence on each source line (rumdl MD013 in `.rumdl.toml`).
- Use US English.
  Do not use an em dash.
- Use short, direct sentences.
  Do not repeat what the code or the diff already shows.

## Commits

- The subject has the form `subsystem: Summary`, for example `session: Write one saved-state version`.
  Do not use Conventional Commits.
- Make one logical change in each commit.
  Each commit must build and pass the tests by itself.
- Stage files by explicit path.
  Do not use `git add -A` or `git add .`.
- The body is plain text, wrapped at about 74 columns, with no links.
  It tells why the change is necessary.
