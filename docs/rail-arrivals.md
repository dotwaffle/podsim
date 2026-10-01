# Scheduled rail arrivals

The editor's **Rail arrivals** section defines train passenger releases at passenger stations.
Add an arrival, select its hub, and set the arrival time, walking delay, and passenger count.
Add weighted destinations, then select the **Rail arrivals** demand pattern and enable passenger generation.
The pattern uses the plan's fixed volume and disables the rate field.
Other demand patterns retain their existing behavior.
Manual journey orders remain available.

The optional project `railArrivals` member contains events such as:

```json
{
  "railArrivals": [
    {
      "id": "train-1",
      "station": "harbor",
      "atSeconds": 600,
      "walkingSeconds": 30,
      "passengers": 120,
      "destinations": [
        { "station": "market", "weight": 3 },
        { "station": "garden", "weight": 1 }
      ]
    }
  ]
}
```

Times are integer simulated seconds after reset.
The event releases after its arrival time plus walking delay.
A zero-time event releases after the first simulation tick.
The project permits 256 events, 10,000 passengers, and 200 passengers per event or combined release tick.
An event permits 16 distinct destinations other than its hub.
Arrival times end at 86,400 seconds, and walking delays end at 3,600 seconds.
Their sum must not exceed 86,400 seconds.
Weights are integers from 1 through 1,000,000.

The seed and event ID determine each passenger's destination.
An unrelated event does not change that sequence.
The plan retains event order for simultaneous releases.
A full live queue rejects the offered passenger once and increments **Skipped**.
It does not retry the offer or reserve a pod for a future train.
Disabling generation stops releases.
Re-enabling generation skips events at or before the current tick.
Reset restarts the plan, and physical restore continues after the saved tick without replaying releases.
Project import/export, editor undo, and saved sessions retain the plan.
Deleting a station removes its arrivals and destination references, including events with no remaining destinations.

## Comparison command

Use a project with a nonempty valid plan:

```sh
mise run compare -- -project rail-project.json -pattern rail-arrivals -duration 2h -arrivals-for 1h -format json
```

The arrival window excludes releases exactly at its end.
Explicit `-request-every`, `-loads`, `-burst-size`, `-adaptive-limit`, and `-past-limit` flags are invalid for this fixed-volume pattern.
The command's synthetic `all` selection retains its existing patterns.
Rail rows report zero for the unused request interval and burst-size fields.
Their offered rate comes from the actual scheduled count and arrival window.

Rail schedule hashes include the event ID, passenger ordinal, release tick, origin, and destination.
When offers are skipped, JSON rows add `rail_skipped_offers`, their increasing zero-based indices in the offered schedule.
The project, seed, window, and hash reproduce the rejected event identities.
The skipped count equals the index count.
CSV and table output retain their existing columns.
Existing non-rail JSON reports and schedule hashes retain their encoding.

Before a simulation starts, the command limits retained rail offer indices to 1,000,000 across the expanded matrix.
This includes redistribution and experimental policy copies.
Each index slice has capacity at most its scheduled count.
One million 64-bit indices require about 8 MiB, excluding the other report fields and JSON encoding.
