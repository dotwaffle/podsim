# Occupied pickup screen with shared rail offers

Status: bounded Rail Hub screen on `bb8f577`, October 3, 2026.
Enabling occupied pickups changes no outcome in either selected seed.
No occupied pickup or intermediate stop occurs.
The raw measurement data is in git history.

## Workload and result

Both arms use explicit shared consent for every offered one-person party.
Sharing allows four parties, drop-off mode, and three intermediate stops.
Only the occupied-pickup policy differs between arms.
The baseline Rail Hub layout, 30 legacy pods, queue limit 200, speeds, clearances, and three-hour cap remain fixed.
Forecast positioning and platoons remain off.

| Heavy seed | Completed / offered | Skipped | Pickup p95 | Request-to-completion p95 | Empty distance | Mean moving party occupancy |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1, policy off or on | 862 / 1,440 | 578 | 1,407.550 s | 1,800.950 s | 848,351.873 m | 3.33354 |
| 2, policy off or on | 920 / 1,440 | 520 | 1,176.433 s | 1,583.667 s | 638,710.358 m | 3.59442 |

Every result field matches after removing the policy label.
Every passenger record, including skipped offer identities and boarding and completion times, matches exactly.
All accepted orders complete before the unchanged cap.
Observed passenger occupancy never exceeds four.
The maximum completed detour ratio is 1.0 within floating-point rounding.

Seed 1 makes 8 outbound rail connections, misses 446, and leaves 266 offers unserved.
Seed 2 makes 5, misses 393, and leaves 322 unserved.
Neither seed has an unresolved outbound connection at the endpoint.
Both policies produce the same connection ledger.

The shared workload completes more offers than the earlier private attribution workload.
That comparison changes consent and sharing opportunities, so it does not isolate the occupied-pickup policy.
The shared arms combine parties at initial boarding and produce no intermediate stops.
They provide no occupied-pickup service evidence beyond the existing [manual screen](experimental-adoption.md#rejected-candidates).

## Validation and limits

The two light pilots each complete 12 of 12 offers without a skip or occupied pickup.
Each compares the observer with a plain run on the same sources.
All aggregate fields and every request timing match exactly.

The private observer counts current-tick occupied boarding admissions, with the actual rider, pod, source berth, ownership, and passenger-distance baseline.
A native positive fixture records the Garden pickup and compares measured and plain continuations for 60 ticks.
A negative fixture excludes initial boarding.
A corrupt positive boarding baseline fails the observer check.
Focused tests, the race check, and vet pass.

The four heavy arms perform 1,326,240 live safety checks and 72 planned physical restore probes.
All pass the existing separation, speed, contract, and restore checks.
The minimum observed separation is 13.9631 meters.
Cold restore explicitly permits save-6 boarding records and restores the requested runtime policy.
Because these arms accept no occupied pickups, these probes do not qualify recorded occupied cohorts under this workload.
Existing native tests retain that proof for their fixtures.

A failure-ledger regression preserves accepted, skipped, and future offer identities with original consent and party size.
First live-check failure capture also retains physical state, native timing records, and partial occupied-pickup events.
Three compiled mutations fail assertions for a missing occupied event, an incorrect positive baseline, and lost future offers.
No build failure or panic counts as a mutation kill.

The selected comparisons have no observed aggregate or individual no-harm exceedance.
The 578 and 520 offers skipped by both arms remain in the complete offered ledger.
Their omission from completed-request tails does not establish an individual service pass.
No active occupied-pickup comparison occurred, so this screen does not justify broader qualification or default adoption.

The [adoption gates](experimental-adoption.md) and current operating limits remain unchanged.
Do not repeat this workload to qualify occupied pickups without a changed, explicit intermediate-stop hypothesis.
