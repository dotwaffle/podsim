# Dependency review notes

## gRPC advisory

Podsim uses gRPC 1.85.0 through its server-side dependencies.
The [upstream advisory](https://github.com/grpc/grpc-go/security/advisories/GHSA-2v4p-qf9q-27wj) lists 1.84.0 as patched.
[GO-2026-6443](https://pkg.go.dev/vuln/GO-2026-6443) first included that stable release in a development-version range from 1.84.0-dev to a 1.85.0-dev pseudo-version.
The corrected [Go record](https://vuln.go.dev/ID/GO-2026-6443.json) splits that range, and stable 1.84.0 and 1.85.0 are outside it.

The downloaded 1.85.0 source contains both missing-header guards:

- The HTTP/2 transport rejects requests without either `:authority` or `Host`.
- The xDS routing code checks the authority list before indexing it.

On 1.84.0, the upstream missing-header regression test passed against the unmodified module.
Removing the transport guard in an isolated copy caused that test to fail.
The sweep to 1.85.0 repeated the source check, but not the test.
Podsim does not create a gRPC or xDS server.
No scanner exclusion, vulnerability-database override, or version override is installed.
Recheck the advisory and source before changing this dependency again; a clean symbol scan alone is not the reason for accepting it.

## Toolchain and module versions

The tool-version selectors keep their existing pin styles.
Go is at 1.27.4, Ebitengine at 2.10.6, Go Cloud at 0.47.0, and x/image at 0.48.0.
The graphics dependencies require x/sync 0.25.0, x/sys 0.50.0, and x/text 0.44.0.
Node stays on its 24.x line at 24.22.2; govulncheck is at 1.9.0, rumdl at 0.3.2, and golangci-lint at 2.15.0.

The update sweep of November 4, 2026 found the other direct modules, govulncheck, golangci-lint, actionlint, and the workflow actions at their latest versions.
It raised Go, Node, rumdl, automemlimit to 1.0.1, and eight indirect modules: purego 0.11.3, gax-go 2.27.0, grpc-gateway 2.33.0, x/net 0.62.0, google.golang.org/api 0.310.0, the two genproto modules, and gRPC 1.85.0.
Node 26 is a new major line outside the current selector, and Node stays on 24.

The update sweep of November 11, 2026 raised Ebitengine and x/image, which also raised x/sync, x/sys, and x/text.
It also raised google.golang.org/api to 0.311.0 and the two genproto modules; gRPC stays at 1.85.0.
Node 24.22.3 and rumdl 0.3.3 were less than 24 hours old, so they wait for the next sweep.

Ebitengine 2.10.6 fixes the tick clock: when the frame rate was near the TPS or an integer ratio of it, some frames ran zero or two updates instead of one.
The browser client runs at a TPS of 60, which is in the affected case.

Go 1.27.4 fixes GO-2026-6859, a race of `MaxBytesReader` and the request headers that can crash an HTTP/1 server in net/http.
`govulncheck` reports call paths from the server and the clients.
Go 1.27.3 fixes GO-2026-6809 (CVE-2026-111943), a denylist of distrusted intermediate certificates in crypto/x509.
`govulncheck` reports a call path to the affected function.
Go 1.27.2 fixes CVE-2026-78667 in the asset-serving ServeContent path.
The default ignores a Range header with more than 200 ranges.

x/net is at 0.62.0.
Version 0.62.0 fixes GO-2026-6860 and GO-2026-6866 in x/net/html, which `cmd/serve` does not link.
Version 0.60.0 fixes GO-2026-6603, GO-2026-6610, GO-2026-6611, GO-2026-6612, and GO-2026-6617.
Podsim has no configured inbound HTTP/2 server or reverse proxy, but the indirect HTTP/2 implementation remains in the native dependency graph.
The update removes the affected version instead of adding a scanner suppression.

automemlimit 1.0.1 stops `memlimit.Set` from failing when one duplicate cgroup mount is unreadable and another gives a limit.
Before this fix, the server logged a warning and ran without a limit in that environment.

grpc-gateway 2.33.0 changes how the gateway mux parses headers.
Podsim links the gateway runtime through its dependencies, but does not create a mux.

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
