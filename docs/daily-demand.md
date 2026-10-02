# Repeating daily demand

Select **Daily OD profile** in the editor to repeat a weighted demand profile every 1,440 simulated minutes.
The clock uses simulation time, not the browser clock or a calendar date.
Set **Time at reset** to choose the initial minute.
Tick one selects that minute.
Reset restarts the clock, random stream, and offer budget.

## Bands and rates

Each band has a start minute from 0 through 1,439 and a duration from 1 through 1,440 minutes.
A band can cross midnight.
Bands must not overlap.
The end of a band belongs to the next band or gap.
A gap generates no requests and disables proactive empty-pod positioning.
The daily pattern ignores the manually selected band.

An optional band `perMinute` overrides the project demand rate.
Its range is 0 through 120.
Zero makes the band idle.
Omitting the field uses `demand.perMinute`, which retains its range of 1 through 120.
Each band still needs positive total flow weight, including an idle band.
Existing manual profiles retain their rates and selection behavior.

The optional `demand.dailyStartMinute` ranges from 0 through 1,439.
Zero selects midnight and is omitted from canonical JSON.
A nonzero value requires the `profile-daily` pattern.

The offer budget resets at each band occurrence.
The random stream continues across ordinary band boundaries.
Disabling generation stops offers.
Re-enabling starts from the current simulated minute and does not replay the disabled interval.
Physical saves and checkpoints retain the budget and random stream.

## Park-and-ride authoring

Expand **Create a park-and-ride profile** in the editor.
Select a passenger-service hub and one or more distinct passenger destinations with positive weights.
Set the morning and evening times, durations, and rates, then select **Create profile**.
Morning flows go from the hub to the destinations.
Evening flows return to the hub.
The editor selects the new daily profile and records one undo step.
It preserves the generation switch, seed, and fallback rate.
Select **Pause and apply** to use the edited project in the simulation.
The simulation labels the pattern **Daily / profile ID**.
Switching to an ordinary pattern clears the daily clock setting.

Creation uses Go/WASM validation in a browser worker.
An unavailable worker blocks apply and keeps export available.
A response cannot overwrite a draft that changed while the worker ran.
The creator respects the existing eight-profile and 299-destination limits.

## Comparisons

```sh
mise run compare -- -project daily-project.json -pattern profile-daily -daily-start-minute 420 -duration 3h -arrivals-for 2h -seeds 1,2 -format json
```

Omit `-daily-start-minute` to use the project value.
Record the initial minute with the invocation when comparing results.
The comparison uses the live Go clock, band rates, and weighted flow sampler.
Its half-open offer window excludes requests exactly at the window end.
The selected positioning policy stops during gaps and zero-rate bands.
The guarded policy also stays off after the run exceeds its queue limit.

Daily comparisons reject explicit `-request-every`, `-loads`, `-burst-size`, `-adaptive-limit`, `-past-limit`, and `-bands` flags.
Volume comes from the authored band rates.
`request_every_seconds` and `burst_size` are zero for daily results.
`offered_per_minute` measures the full window, including idle periods.

JSON adds `daily_start_minute` only to daily results, including an explicit zero.
CSV adds that column only when the report contains a daily arm.
Table output lists each daily arm's initial minute above the existing table.
Ordinary reports and schedule hashes retain their existing output.
Before simulation starts, a daily matrix applies a conservative 256 MiB offer-storage bound.
It charges 512 bytes per scheduled offer for every arm copy.
This limits retained offer storage, not total simulation memory.
