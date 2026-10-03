# Offline car and energy workflow

Open `web/offline.html` directly, or select **Offline car and energy** in the scenario editor.
The browser reads local files and downloads authored inputs.
It does not run a simulation or contact a server.
Download your files before you close the page.
The page does not save drafts.

## Author a car plan

1. Associate the project JSON that you will supply to `parkride`.
2. Add a car lot with an ID, passenger hub, and explicit car capacity.
3. Add an itinerary with unique itinerary and car IDs.
4. Select its lot and a different passenger destination.
5. Enter car seats, whole-party size, consent, and all six times in integer seconds.
6. Select outward `drive-home` and return `retain-car` refusal policies.
7. Download `car-plan.json`.

The page starts numeric fields empty.
Enter zero when the intended duration or capacity is zero.
A loaded plan can omit consent, which means private under the existing contract.
The page preserves that omission until you select explicit consent.
Consent applies to both pod legs.

A full lot refuses the itinerary without fallback travel.
An outward queue refusal starts retrieval and homebound car travel.
A return queue refusal retains the car and leaves the party stranded.
Car lot capacity does not use pod berth counts.

## Author energy profiles

1. Associate the project to check normalized fleet class coverage.
2. Add one `flat-v1` profile for each fleet class.
3. Enter every coefficient, including explicit zeros.
4. Include assumed payload in the fixed effective mass.
5. Download `energy.json`.

Supported classes are `legacy`, `compact`, and `group`.
An omitted project fleet class normalizes to `legacy`.
The page rejects unsupported classes, including `express`.
You can load existing plan and energy files, select entries, edit fields, and export them.
Invalid imports retain the previous data.

## Run and import

Place the exported files beside `project.json`.
Run the commands from the repository.
Replace `HORIZON` and `QUEUE_LIMIT` with your authored controls.
The horizon must be positive and at most `24h`.
The car queue limit must be an integer from one through 1,000,000.

```sh
go run ./cmd/parkride -project project.json -plan car-plan.json -duration HORIZON -queue-limit QUEUE_LIMIT -output car-report.json

go run ./cmd/compare -project project.json -energy-file energy.json -duration HORIZON -queue-limit QUEUE_LIMIT -format json -output energy-report.json
```

Compare uses its own demand and policy flags.
Use `go run ./cmd/compare -h` to select them.
Compare does not consume the car plan.
These commands run separate models.
Keep the inputs and command with each report.
The CLI validates project geometry, native admission, and input contracts.
Browser project association supplies suggestions and coverage checks only.

Import the resulting JSON into **View a report**.
Car version 1 reports show every outcome, lot peak and final occupancy, held slots, and refusal reasons.
Select an itinerary to inspect its authored inputs, pod requests, and event ticks.
The car report identifies its build, project hash, plan hash, queue limit, native policies, and pod metrics.
A run error appears above the outcomes.

Energy schema 15 reports show gross traction draw, recovery, auxiliary energy, and signed net energy in joules.
Select a comparison result to inspect class counts, profiles, mass assumptions, policy settings, and its full result.
Negative net energy remains negative.
Energy reports do not contain build or project hashes.
The viewer states that provenance gap.

Report clocks use 60 ticks per second.
A car event tick of -1 means unrecorded.
A missing door-to-door duration means the itinerary did not complete.
The viewer does not replace missing values with zero.
The full imported JSON remains available below the report.

## Browser limits

The page accepts authored inputs and projects up to 10 MiB and reports up to 32 MiB.
JSON depth cannot exceed 32.
The parser accepts at most 300,000 JSON values.
Integers must fit JavaScript's exact range, from -9,007,199,254,740,991 through 9,007,199,254,740,991.
Use the CLI for files outside these browser limits.
These are browser resource limits, not new native operating limits.

The parser rejects duplicate members, nonfinite numbers, and inexact integers before rendering.
The page renders imported strings as text.
Download names and displayed command paths are fixed.
Project association does not add members to exported plan or energy files.
No project, save, stream, or report contract changes.

See [car journey design](car-journey-design.md) and [energy model design](energy-model-design.md) for model assumptions.
Test fixtures under `web/testdata/offline` use analytic authored coefficients, not physical defaults.
Their generation commands are in [the fixture record](../web/testdata/offline/README.md).
