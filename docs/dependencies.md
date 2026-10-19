# Dependency review notes

## gRPC advisory

Podsim uses gRPC 1.84.0 through its server-side dependencies.
The [upstream advisory](https://github.com/grpc/grpc-go/security/advisories/GHSA-2v4p-qf9q-27wj) lists 1.84.0 as patched.
[GO-2026-6443](https://pkg.go.dev/vuln/GO-2026-6443) first included that stable release in a development-version range from 1.84.0-dev to a 1.85.0-dev pseudo-version.
The corrected [Go record](https://vuln.go.dev/ID/GO-2026-6443.json) splits that range, and stable 1.84.0 is outside it.

The downloaded 1.84.0 source contains both missing-header guards:

- The HTTP/2 transport rejects requests without either `:authority` or `Host`.
- The xDS routing code checks the authority list before indexing it.

The upstream missing-header regression test passed against the unmodified module.
Removing the transport guard in an isolated copy caused that test to fail.
Podsim does not create a gRPC or xDS server.
No scanner exclusion, vulnerability-database override, or version override is installed.
Recheck the advisory and source before changing this dependency again; a clean symbol scan alone is not the reason for accepting it.

## Toolchain and module versions

The tool-version selectors keep their existing pin styles.
Go is at 1.27.2, Ebitengine at 2.10.5, Go Cloud at 0.47.0, and x/image at 0.47.0.
The graphics dependencies require x/sync 0.24.0, x/sys 0.49.0, and x/text 0.43.0.
Node stays on its 24.x line at 24.22.0; govulncheck is at 1.9.0, rumdl at 0.2.79, and golangci-lint at 2.15.0.

The update sweep of October 19, 2026 found every direct module, Go, govulncheck, actionlint, and the workflow actions at their latest versions.
It raised golangci-lint to 2.15.0 and five indirect modules: purego 0.11.2, grpc-gateway 2.32.0, google.golang.org/api 0.304.0, and the two genproto modules.
gRPC stays at 1.84.0, so the advisory analysis above still applies.
Node 26 and rumdl 0.3 are new major lines outside the current selectors.

Go 1.27.2 fixes CVE-2026-78667 in the asset-serving ServeContent path.
The default ignores a Range header with more than 200 ranges.

x/net is at 0.61.0.
Version 0.60.0 fixes GO-2026-6603, GO-2026-6610, GO-2026-6611, GO-2026-6612, and GO-2026-6617.
Podsim has no configured inbound HTTP/2 server or reverse proxy, but the indirect HTTP/2 implementation remains in the native dependency graph.
The update removes the affected version instead of adding a scanner suppression.

Go Cloud's fileblob hardening preserves the permitted keys because Podsim already refuses unsafe prefixes and `__` escape sequences.
The store keeps its acquisition-only BeforeWrite hook and cancel-before-close protection.
Cloud storage stays out of the browser WASM modules.

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
