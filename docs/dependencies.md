# Dependency review notes

## gRPC advisory, September 29, 2026

Podsim uses gRPC 1.84.0 through its server-side dependencies.
The [upstream advisory](https://github.com/grpc/grpc-go/security/advisories/GHSA-2v4p-qf9q-27wj) lists 1.84.0 as patched.
However, [GO-2026-6443](https://pkg.go.dev/vuln/GO-2026-6443) includes that release in an affected development-version range.
Its published range begins at 1.84.0-dev and ends at a 1.85.0-dev pseudo-version.
Under semantic version ordering, that range includes the stable 1.84.0 release.

The downloaded 1.84.0 source contains both missing-header guards:

- The HTTP/2 transport rejects requests without either `:authority` or `Host`.
- The xDS routing code checks the authority list before indexing it.

The upstream missing-header regression test passed against the unmodified module.
Removing the transport guard in an isolated copy caused that test to fail.
Podsim does not create a gRPC or xDS server.
Its native symbol-level vulnerability scan reports no called vulnerable symbols.
The scanner still reports the imported-package finding, and gopls reports GO-2026-6443.
No scanner exclusion or vulnerability-database override is installed.

This review treats the finding for 1.84.0 as a version-range metadata error.
Recheck the advisory and source before changing this dependency again.
A clean symbol scan alone is not the reason for accepting this version.

## State-store path analysis

Golangci-lint 2.14.0 adds G703 findings for two state-store filesystem calls.
Both paths originate in the administrator's `-state` option.
Browser commands and project files cannot select the storage directory.
The store validates its relative prefix and generates its write keys internally.

The directory checks and syncs deliberately include ancestors of the configured root.
They preserve newly created directories after a power loss.
Two line-specific G703 annotations document this trust boundary.
They do not disable G703 elsewhere or change storage behavior.
Do not pass a browser-controlled URL to `statestore.Open`.
