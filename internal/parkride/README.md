# Offline car itineraries

Run a finite plan with an existing project:

```sh
go run ./cmd/parkride -project project.json -plan cmd/parkride/testdata/plan.json -duration 20m -queue-limit 200
```

Use `-output report.json` to write the JSON report to a file.
The command requires all four input and control flags.
The horizon cannot exceed 24 hours.
Fractional horizons use the existing native tick conversion.

The fixture lists every required plan field.
Each itinerary owns one car and one whole party.
Car capacity belongs to the named car lot, independent of native pod berths.
Omitting `sharingConsent` selects private consent for both pod legs.
Explicit shared consent permits native pooling under the project's policy.

The command uses the project's sharing, platoon, and experimental settings.
Project redistribution selects native guarded positioning.
Pickup weights count one outward and one return origin per itinerary.
Guarded positioning uses its existing observed-rate fallback.
Project daily and rail demand do not issue requests.
Native experiment histories remain off.

The car arrives after its authored outward duration.
Actual outward unloading and the minimum activity duration constrain the return offer.
Actual return unloading starts retrieval.
The car releases its slot after retrieval, before homebound travel.

Full lots refuse an itinerary without fallback travel.
Outward queue refusal follows the required `drive-home` policy, including retrieval.
Return queue refusal follows `retain-car` and leaves a terminal stranded car in its lot.
Recovered refusals do not count as completed round trips.
Native submission errors stop the command with a partial report and an error exit.

At the horizon, car arrivals can occupy a lot or receive a full-lot refusal.
The command processes unloading, retrieval, and home arrivals at that boundary.
It issues no new pod offer there.
Unfinished stages remain censored, with parked slots retained.
Midnight does not clear lots or repeat itineraries.

Clone a `Run` to copy its native simulation and car ledger together.
The native saved state does not contain the car ledger.
Durable continuation, road traffic, batteries, and travel-time estimation are outside this command.
