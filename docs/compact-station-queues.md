# Compact station queues

`compact-v1` is an experimental project setting.
Ordinary spacing remains the default.
The pilot reduces stopped station queue spacing to 6.01 meters for supported four-meter pods.
It retains the reaction and braking allowance while the pods move.

Enable station buffers and a platoon limit from two to four before applying the compact setting.
The editor checks these dependencies.
Native validation accepts the setting with or without the `orderContract` marker.
Selecting the setting does not change the project version.
It does not change lane speeds, geometry, buffers, or the platoon limit.

```json
{
  "version": 1,
  "stationBuffers": true,
  "platoonLimit": 4,
  "stationQueueSpacing": "compact-v1"
}
```

This fragment contains only the settings.
A complete project must also contain its network, fleet, and demand settings.
Omit `stationQueueSpacing`, or select `ordinary`, to keep ordinary spacing.
An explicit `null` queue field fails validation.

The controller admits a group only on a straight station entry with a speed limit at most 2.5 m/s, or 9 km/h.
Both members of each adjacent pair must stay within that speed band.
The group needs owned stopping cells and enough space to recover ordinary spacing.
Roads, junctions, outsiders, and nonadjacent pods retain the ordinary rules.
The controller also retains ordinary resource-release distances.

Before departure, the group stops at ordinary spacing.
Disabling the compact policy retains the recovery certificate until recovery finishes.
The saved state keeps membership, stopping cells, exact speeds, and recovery targets.
Restore rejects an invalid certificate without logical fallback.
A valid compact certificate cannot convert to a logical-only restore.
A controller fault pauses all pod movement and produces one server error record before new demand offers.

Native tests form four-member queues from real berth departures.
Their stopped positions span 18.03 meters from the first pod to the fourth.
Separate departure and cold-restore tests complete all five passenger trips.
These checks establish formation and recovery behavior.
They do not establish a passenger throughput gain or qualify default adoption.

The typed save fixture retains the current limits of 300 pods, 2,600 queued records, and eight stored riders per pod.
It serializes every field, checks the bounded encoder against raw JSON, and runs the scanner and decoder.
Its independent field maxima do not describe a physically reachable state.

| Members per certificate | Encoded bytes | Space below the 83,886,080-byte cap |
| --- | ---: | ---: |
| 1 | 55,378,502 | 28,507,578 |
| 2 | 55,371,302 | 28,514,778 |
| 3 | 55,368,902 | 28,517,178 |
| 4 | 55,367,702 | 28,518,378 |

These sizes are for saved-state version 9, which packs the order text as base64.
With raw order text in save 6, the worst case was 83,698,502 bytes.
That exceeded the earlier estimate by 6,905 bytes but fit the existing cap.
The complete typed certificate adds 502 bytes per head, compared with the estimated 483 bytes.
Wider existing float encodings and compact class metadata account for the remaining 1,205 bytes.
No queue, rider, save, or stream limit changes.

Compare ordinary and compact policies with explicit dependencies:

```sh
go run ./cmd/compare -station-queue-spacing ordinary,compact-v1 \
  -station-buffers on -platoon-policies virtual
```

The selector adds queue policy provenance to JSON and a policy column to table or CSV output.
Omitting it keeps the existing ordinary comparison and report columns.
Compact arms require buffers and virtual platoons in every arm.
The comparison rejects invalid combinations before loading a project or creating offers.
It stops immediately when the controller retains a fault.
The selector does not change authored lane speeds or enable other policies.
