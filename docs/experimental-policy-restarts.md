# Experimental policy file restarts

The file-store integration tests preserve physical assignments with pickup reassignment enabled.
The option remains off by default.
These tests extend the in-memory store tests in `internal/session/experimental_test.go`.

## Fixture and checks

`internal/statestore/policy_restart_test.go` starts with two valid crossed pickup assignments on the scaled example network.
The simulation must swap those assignments before the test exports its state.
The fixture checks safety before export.

The test packs the order text as base64 and inserts that physical state into a session-generated version 9 file.
It keeps the session wrapper, demand state, and project fields from the production encoder.
Each case owns a real `file://` store in a temporary directory.

The tests check these paths:

- The caller-selected reassignment control.
- The saved control with no caller-supplied project.
- Rejection of a version 2 file at startup with `unsupported_version`: the server moves the file aside and starts a new session.
- Startup, final save, second physical restore, and request completion through the session clock.
- Unchanged physical saved state after restart, including request bindings.

The clock runs inside `testing/synctest`, which avoids a wall-clock wait for simulated travel.
The session creates and stops its clock goroutine inside that test.
Reassignment history and cooldowns still reset on file restore, as specified by the approved control contract.
These tests do not promise identical future experimental decisions after restart.

## Failed saves

A canceled save must leave the previous compressed file unchanged.
A failed directory sync can occur after the new file replaces the previous file.
The test uses the existing store fault hook to exercise that second case.
It checks the error metric, physical state, restored speed, and recovery after a successful retry.

The canceled-write case first completes a periodic save to reset the crash-loop counter.
Repeated startup without a successful periodic or final save still invokes the existing logical-restore guard.

The tests exercise file writes, renames, sync errors, gzip decoding, and physical restore.
They do not simulate a power loss or establish durability across a filesystem failure.
For the file-store guarantees, see [operations](operations.md).
