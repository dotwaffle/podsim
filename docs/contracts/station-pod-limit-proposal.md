# Station pod limit proposal

Status: draft for the maintainer, 2026-11-24.
Nothing is approved or built.
A Codex review found that the first progress argument was not proved, and this version removes it.
The [design note](../design/station-pod-limit.md) has the block, the options, and the pilots of option C1.

## Goal

A project can limit the pods that dispatch sends to a station for pickups.
With no setting, behavior and bytes do not change.

The pilots have 8 patched runs, 5 at a quarter of the hub stream and 3 at the full stream, and each finishes with no safety failure.
Only seed 1 of the quarter stream has a run with no limit on the same code, and it blocks.
The full-stream runs with no limit are of older code, and quarter-stream seeds 2 and 3 have none.
The limit moves 5.5 to 5.7 passengers a minute from Waterloo to Bank at the full stream, against 24.8 to 29.0 for `D` and `S` in the batch.
This describes the runs, and it does not show that the limit prevents blocks in general.

## Rule

`S` is `request.legOrigin()`, which is `LegFrom` for a continuation, and `k` is the limit.
`bound(S)` is the number of pods with `destinationStation` equal to `S`, with no other test.
Thus a pod that a reset made idle has an empty destination and does not count, and a pod that arrived keeps the destination and counts while it is idle.
A clear of a hold and an evacuation change the count only when they change the destination.

After `cachedPickupPod` gives a pod `v`, `assignPickupPod` refuses when all of these hold:

```text
k > 0
v.Pod.StationID != S or v.Pod.Activity != Idle
v.destinationStation != S
bound(S) >= k * len(station(S).Berths)
```

The refusal is before `waitForFinishingPod`, as in the pilot, and it does not search for another pod.
It leaves the deferral fields, so a deadline of a hold still expires as time passes.

- Joins, promotions, existing assignments, loaded drop-off continuations, swaps, and pickup transfers do not read the limit.
  A pickup transfer is not bounded: the released pod keeps its destination until it parks or arrives, so a station can exceed the limit for an unknown time, and repeated transfers add to it.
- An emergency rider transfer makes a continuation that reads the limit when it is assigned.
- An emergency, a fault recovery, guarded positioning, parking, and berth clearing do not read the limit.
- `keepHold` runs before the check, so a held trip keeps its finishing-pod reason until the next check.

### Progress

Liveness is not proved.
A refused pickup leaves its pod in its origin berth, and that pod can block a loaded arrival whose station is then full.
Berth clearing needs a blocked arrival and a place to go, and it skips withdrawn pods.
Guarded positioning stops its sends while a trip has no pod.
The count reads all classes and banks, so it says nothing about usable berths.

Queue position is kept, and allocation is not FIFO: a later trip in a pass can use a place that a boarding freed after an earlier trip was refused, and the earlier trip waits for the next pass.
Sustained mixed-class traffic can pass an older trip again and again.

### Vehicle classes

The berths and the pods of all classes count.
So Compact pods can fill a station for an Express pickup.
`LF` has no Express pod, so no pilot shows the effect.
Decision PL-D2 asks for this.

### Reason text

A refused trip has the reason `Waiting for station space` (25 bytes), inside `MaxExpressDispatchReasonBytes`.
The review found no larger byte bound and no version change necessary.
With no candidate pod, the reason is as today, and lack of a fitting class overrides the available-pod reason.
With a candidate, the limit comes before the finishing-pod hold and the pickup access.

## Setting

| Member | Values | Absent means |
| --- | --- | --- |
| `stationPodLimit` | 0 (off), 2 to 8 | Off |

- The Go field is `Config.StationPodLimit int` with `json:"stationPodLimit,omitzero"` and a new digest extension.
  18 is free today, and the registry test covers it.
- Validation goes after the platoon limit, with the text `station pod limit must be 2 to 8, or 0 for no limit`.
  The cached check indexes of the validator move with it, and a null is refused as for other numbers.
- A value of 1 is refused: an idle pod in each berth then refuses every pickup from elsewhere that those pods do not fit.
- No preset sets it (PL-D3).
- The editor worker copies each project branch by hand, so it needs support for the member whatever PL-D4 says: import, unrelated edits, history, validation, and export keep it.
  The visible control is optional.
- The simulation setting is applied at startup, reset, project replacement, and store restore, as `SetPlatoonLimit` is, and the demo keeps it off.

## Cost

The pilot counts the pods for each send, 10.9 to 23.1 million times in a quarter-stream run, and a full-stream run takes 660 to 770 s.
That the count and the repeated route searches cause the cost is a hypothesis, and no profile exists.

A cached count updates at committed fleet changes only.
`onboardPickupCandidate` and pickup transfers write a copied vehicle first, so a setter at each of the 13 writes of `destinationStation` would count a speculative or a double change.
Fleet replacement and restore rebuild the count, and a clone has its own.
The count must equal a scan before each decision of the limit.

The first version had an earlier refusal before the pod search.
It is removed, because it can refuse a local idle pod or hide the availability reason.

## Saved state and restore

The count is derived, so there is no saved member and no state version change.
Physical and logical restores and a rewind rebuild it, and a rewind uses a clone.

## Compatibility

The project format gets an optional member, the saved project and the command digest get it only when it is set, and the stream does not change.
The golden files of the state and stream do not change with the setting absent.

## Tests

1. With the setting absent: `TestPresetBehaviorParity` and `TestLondonExpressBehaviorParity` have no change.
2. A station at the limit refuses a send, keeps the place of the trip in the queue, and gives the reason.
3. A boarding that changes the destination frees a place, with exact results for the same pass and the next pass, also with sustained mixed-class traffic that passes an older trip.
4. A pod idle at the station boards at the limit.
5. A pod that is already bound is not refused.
6. A loaded pod and an emergency pass the limit.
7. The count equals a scan at each tick, in a run with faults, emergencies, and rewinds, and after a restore.
8. A mutation of each rule fails a test.
9. Validation: the limits 0, 1, 2, 8, and 9, and the order of errors.
10. Liveness fixtures: a circular wait through origin berths, incompatible idle pods, bank and class limits, no parking, and withdrawn pods.
    They cannot prove that no block exists.
11. The 8 patched pilot runs repeated with the cached count give the same orders, and a routine soak with the limit on.
12. The editor worker keeps the member in each path.

## Evidence that is missing

- The individual limits: each `LF` run with no limit blocks at the pilot rates, so no pair compares waits.
  One pair with no hub stream is a screen of that workload only, and the adoption gates need more.
- A run of any preset with Express.
- Any value of `k` at the full stream other than 2.

## Decisions for the maintainer

| ID | Question | Options | Recommendation |
| --- | --- | --- | --- |
| PL-D1 | Build | A: build this, off by default. B: measure the individual limits first. C: do not build, and keep the pilots as a record. | B, as a bounded screen: liveness is not proved, and the cost on normal traffic is unknown |
| PL-D2 | Classes | A: all classes count. B: a count for each class and its berths. | A for a first build, because no run with Express exists |
| PL-D3 | Presets | A: none. B: `LondonFull`. | A |
| PL-D4 | Editor | A: no control. B: a number field. | A |
