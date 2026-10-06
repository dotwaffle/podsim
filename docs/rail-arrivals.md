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
A 256 MiB conservative storage preflight also counts full schedule, skipped-index, and outbound-record capacities across all arm copies.
See [scheduled train connections](rail-connections.md) for mixed plans, connection outcomes, and the explicit forecast comparison control.

## Equal-volume timing study

The October 1 study compared six trains with evenly spaced demand at Rail Hub and LondonFull's Paddington.
Each train released 120 passengers after a 30-second walking delay, at simulated seconds 30, 630, 1,230, 1,830, 2,430, and 3,030.
The control released the same 720 ordered passenger identities and endpoints every five seconds, through second 3,600.
Both arms used seeds 1 and 2, a 200-order queue, and a fixed three-hour recovery cap.
The arrival window ended at tick 216,001 so it included the control's final passenger.
Rail Hub used 30 pods.
LondonFull used its existing 287-pod fleet.
Redistribution, sharing, platoons, station buffers, and pickup reassignment were off.
Routing used free-flow costs.

| Fixture | Seed | Completed, train / regular | Skipped, train / regular | Drain time, train / regular (s) | Empty distance, train / regular (km) |
| --- | --- | --- | --- | --- | --- |
| Rail Hub | 1 | 337 / 366 | 383 / 354 | 7,795 / 8,388 | 1,580 / 1,731 |
| Rail Hub | 2 | 336 / 367 | 384 / 353 | 7,709 / 8,392 | 1,636 / 1,759 |
| LondonFull | 1 | 412 / 443 | 308 / 277 | 7,784 / 8,304 | 6,559 / 6,749 |
| LondonFull | 2 | 412 / 449 | 308 / 271 | 7,503 / 8,419 | 6,815 / 7,255 |

All eight arms recovered within the original cap, with no remaining orders.
The train arms rejected 29 to 37 more passengers than their controls.
Their earlier drain times and lower total empty distance also reflect their earlier last offers and fewer accepted passengers.
Neither establishes better service or capacity.
The queue-clear delay starts at each arm's last offer, at second 3,030 for trains and second 3,600 for controls.
The measurement record includes both that delay and the absolute queue-clear time.

Each pair preserved all 720 offered identities, including skipped passengers.
Matching passengers completed in both arms gives a different result from averaging each arm's accepted population:

| Fixture | Seed | Matched passengers | Mean additional pickup wait with trains (s) | Mean additional journey time with trains (s) |
| --- | --- | --- | --- | --- |
| Rail Hub | 1 | 273 | 195.7 | 197.7 |
| Rail Hub | 2 | 272 | 207.3 | 210.2 |
| LondonFull | 1 | 330 | 108.2 | 111.8 |
| LondonFull | 2 | 328 | 133.4 | 135.7 |

The bursts increased pickup waits and journey times for those matched passengers on average.
The study retained each skipped offer and each individual timing comparison.
These are selected timing experiments, not sustained capacity measurements or policy qualification.
Release times differ, so the individual service limits serve as diagnostics here.
No default changed.

An observation-only test overlay checked separation, lane speed, and berth use after all 3,857,640 simulation ticks.
It checked the saved-state contract each simulated second and verified request accounting and actual injection timestamps.
The overlay did not change movement, dispatch, or release schedules.

The endpoint test used an isolated localhost server and the current editor and Chrome WASM client.
Editor Apply saved the plan and paused the new run before its first release.
The client received native gzip binary WebSocket updates without page errors.
A graceful stop and physical restore retained four generated requests at tick 105.
The next event generated three more passengers without replaying the earlier event.
Fly hosting and the stopped Tailscale demo were outside this test.
