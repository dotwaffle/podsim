# Compact queue follow-up screen

The loaded follow-up stopped on a native lane-speed failure before a service comparison.
It does not establish compact queue service gains or qualify default adoption.
The existing idle-clearing correction stays unchanged.

The frozen source is `5a57e4c96114030be5e15721e70a24c82a68895f`.
The fixture starts seven Compact pods at their original berths.
It adds the example return lane to the occupied-buffer network.
Four selected pods receive private singleton market trips at time zero.
Twenty-four queued trips arrive from 180 seconds at 60-second intervals.
The second timing variation adds zero, seven, or fourteen seconds by event index.
Each ordinary and compact-v1 pair has the same offers, fleet, limits, and 2,400-second cap.
Two more offers arrive exactly at the cap and one tick after it.
The half-open offer window retains both as future events.

Road lanes run at 14 m/s.
Station lanes run at 2.5 m/s.
Buffers and virtual platoons remain enabled with limit four.
The live queue limit is 200.
The compact pilot has a separate frozen 1,000-second cap.

The observer checks native separation, ownership, accounting, stopping bounds, lane speeds, and motion on every tick.
It also checks each party's identity and consent.
A plain-versus-observed clone check passes at 120 ticks.
The pilot stops at tick 28,576, or 476.2667 seconds.
It has accepted nine requests, completed none, and retained all nine pending or aboard.
The other frozen service arms do not run after this unsafe stop.

Pod `02` moves from `garden-merge` into `market-approach` at 14 m/s.
The new lane permits 2.5 m/s.
Native `traffic.move` selects speed with the old lane limit before it advances across the lane boundary.
It then publishes the new lane with that speed.
`SafetyObservation.Check` excludes lane speed limits, so the independent speed check detects this failure.
This behavior affects ordinary movement and does not depend on compact membership.

One authorized diagnostic replay records the last five motion ticks.
It reproduces the same boundary and stops before another tick.
The trace retains old and new pods, actual speeds, route blocks, ownership, and reserved frontiers.
It does not count as a service rerun.
No native source, speed, cap, or policy changes follow the result.

The pilot does not reach natural loaded compact formation.
It cannot measure individual no-harm gates or planned service restore probes for this fixture.
The earlier [compact queue screen](compact-queue-service-screen.md) remains separate evidence for natural queue formation and idle clearing.
A later loaded comparison requires a reviewed lane-transition correction or a separately declared fixture.

The [measurement record](measurements/compact-queue-followup-screen.json) pins the frozen fixtures, sources, binary, rejected pilot, and diagnostic trace.
Its durable evidence directory retains refusal, future, pending, and onboard event identities.
