# Large pod qualification proposal

Status: approved by the user on October 3, 2026.
Dimensions, the conservative safety candidate, and group-first delivery are approved.
Group and express pods remain metadata until their applicable qualification gates pass.
The first implementation will model a conservative simulation envelope.
It will not certify real steering, seating, or road vehicle dimensions.

## Dimensions and position

The current route position is an abstract point.
For new large classes, define this point as the center of the body envelope.
Keep existing small-class position behavior unchanged.

| Class | Logical seats | Proposed length | Proposed maximum width | Initial envelope radius |
| --- | ---: | ---: | ---: | ---: |
| Group | 8 | 6 m | 2.5 m | 6 m |
| Express | 20 | 10 m | 2.5 m | 6 m |

These dimensions are simulation assumptions, not measured vehicle specifications.
A centered 10-by-2.5-meter rectangle has a half-diagonal of about 5.154 meters.
The six-meter circle encloses it for every orientation.
The first candidate uses the same circle for both large classes to reduce mixed-class proof cases.
A later per-class envelope can use less space after separate qualification.

## Separation and geometry

Use these rules only where a large class can operate or affect a shared resource.
Do not replace the global small-pod clearance constant.

| Rule | Proposed large-class candidate |
| --- | --- |
| Reference-point separation when either pod is large | At least 20 m |
| Relevant node, track, and origin resource retention tail | At least 20 m |
| Lane length where large pods are admitted | At least 40 m |
| Actual cell length on those lanes | At least 20 m |
| Acceleration and braking | Existing 2 m/s² |
| Tick rate and authored speeds | Existing 60 Hz and authored lane limits |
| Large virtual platoons and compact queues | Prohibited, including mixed links |

Two six-meter envelopes at 20-meter center separation leave eight meters between their circles.
This arithmetic does not prove junction ownership, following behavior, or plane transitions.
Shared-resource preparation must use the largest admitted class, including affected small-only neighboring lanes.
Old-only lanes, pairs, and certified compact queues retain their current rules.
Station and berth allowlists still require explicit large-class admission.
No automatic station expansion or speed adjustment is proposed.

| Approach | Benefit | Cost |
| --- | --- | --- |
| Uniform conservative envelope, recommended first | Fewer mixed bounds to prove | More space than a class-specific model |
| Per-class and per-pair envelopes | Less wasted space | More geometry, following, and restore cases |
| Metadata until a detailed steering model | Defers numeric assumptions | No physical large-pod service yet |

## Delivery order

Recommend group pods first, with the current 2,600 saved waiting records and eight stored riders per pod.
Express retains its route, station, capacity, and service metadata during that stage.
Full express-20 operation needs both physical qualification and a separately reviewed larger-state encoding.
The 80 MiB save and 64 MiB stream caps remain unchanged.
Do not truncate accepted requests or retained rider history to fit those caps.

A joint group-and-express release is possible but needs more encoding and mixed-state work before either class becomes usable.
Group-first delivery does not authorize larger operating counts.
The current manual admission, fleet, registry, party, and stop limits remain unchanged.

## Qualification before enablement

Audit every geometry, reservation, release-tail, trim, restore, editor, and safety consumer.
The current point-separation observer does not establish a swept-body proof.
Add independent envelope and ownership evidence for curves, junctions, and plane transitions.

Require real journeys through following, mixed merges, neighboring berths, parking, and bank and ordinary stations.
Check both leader and follower class orders, including large neighbors beside compact groups.
Assert speed, braking, continuously owned stopping bounds, release tails, and no position snap at every tick.
Cold restores must retain physical state, ownership, identities, consent, timing, and conservation.
Reject short geometry and unsupported large links before admission.
Keep old-only traces and ordinary encoded fixtures byte-identical.
Measure actual save, full-frame, and replacement-delta maximum fixtures before enabling a profile.
Independent review and compiled guard mutations are required.

The reviewed source inventory is in the current service evidence cache under large-body-physics-analysis.md.
Its pinned consumer list separates physical constants from display units and scan optimizations.

## Decisions requested

1. Approve the proposed centered simulation envelopes and dimensions, or provide replacement dimensions.
2. Approve the conservative 20-meter separation and retention, 40-meter lane, and 20-meter cell candidate, or defer for per-class bounds.
3. Choose group-first delivery or a joint group-and-express release.

Approval authorizes implementation and qualification of the candidate.
Runtime enablement requires the applicable safety and byte gates to pass.
It does not authorize default adoption, deployment, real vehicle certification, or a detailed axle and steering model.
