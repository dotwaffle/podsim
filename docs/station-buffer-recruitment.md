# Recruitment for stopped station buffers

Adjacent stopped pods can now recruit inside a certified station buffer at low speed.
The rule applies only when station buffers and virtual platooning are enabled.
Defaults, physical clearance, braking, and junction reservations remain unchanged.

## Rule

The recruitment ceiling is the greater of the existing ceiling and one holding-cell pitch.
The existing ceiling is curve-adjusted link clearance plus stopping distance at the lane speed limit.
The pitch comes from the entry lane length and cell count.
Both pod positions must be inside the certified holding region.
The controller maps the leader position into the follower's route coordinates.
A one-nanometer tolerance handles rounding in recruitment comparisons.
It does not change clearance or stopping checks.

The controller still checks speed limits, route compatibility, curvature, stopping points, and the platoon size limit.
Ordinary road recruitment does not change.
Entry endpoint groups, junction conflicts, and berth discharge paths remain exclusive.
Existing links still drain before their certificates end.
No API, project, save, or stream format changes.
Earlier recruitment can restrict route reassignment while a certificate persists.

## Storage result

Four staged arrivals wait behind a real occupied berth at a uniform 9 km/h limit.
Every reachable destination berth is occupied, so the blocker cannot clear into another berth.
The fixture requires five seconds of stable standstill before measurement.
Queue span measures the distance between the first and last pod reference points.

| Controller | Span, meters | Coupled pods | Settled after staging, seconds |
| --- | ---: | ---: | ---: |
| Platooning off | 90.00 | 0 | 84.28 |
| Previous virtual recruitment | 90.00 | 0 | 84.28 |
| One-cell buffer recruitment | 54.02 | 3 | 101.78 |

Span decreases by 40%, but settling takes 17.50 seconds longer.
The front pair remains 30 meters apart because the certificate protects the discharge frontier.
The rear three pods compact with 12.01-meter straight-link spacing.
This result measures storage, not station throughput.

Restores retain the real berth blocker and queue with new admissions enabled or disabled.
This saturated fixture does not test full discharge.

## Discharge and restore

A separate fixture has four adjacent arrivals, a real departing berth occupant, and reachable storage.
Market station lanes have a 9 km/h limit.
Other lanes retain 14 m/s.
All five accepted trips finish with the default lookahead.
The controlled off/virtual comparison gives identical completion ticks.
The four arrivals finish within 184.35 seconds after staging in both arms.
Their completion intervals are 33.02 seconds in both arms.
The smallest observed separation is 30.00 meters with platooning off and 12.01 meters with virtual platooning.

Physical restores cover wide recruitment, shared cells, compaction, draining, and linked-leader berth-path commitment.
Each restored run finishes all five trips with both policies enabled or disabled.
The linked-leader commitment case uses the existing 10-second lookahead option to request the suffix before shared cells drain.
Default-lookahead discharge is a separate test.
The previous controller also restores all five captured checkpoints and finishes their trips under both policy settings.
The saved certificate contract remains unchanged.

Each tick checks physical separation, lane speed, stopping points, braking, and overtaking.
The resource monitor checks incremental ownership against the full retention scan and validates owner transfers and certificate bounds.
Boundary tests preserve the exclusive frontier and reject recruitment outside the holding region or beyond one pitch.
A fractional-pitch test fails when the recruitment rounding tolerance is removed.
Existing curved-entry, mixed-speed, size-limit, and ordinary-road tests remain in the regression suite.

The raw measurement data is in git history.
An earlier two-seed Rail Hub screen on the banked layout found that existing virtual platoons compacted queues to the 12.01-meter minimum gap without a consistent service gain.
It did not enable this fixed-entry buffer mechanism.
No broader service gain or default adoption is established.
