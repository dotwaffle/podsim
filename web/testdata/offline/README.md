# Offline workflow fixtures

These reports come from the existing CLI commands at base revision `5a57e4c96114030be5e15721e70a24c82a68895f`.
The browser workflow change does not modify either CLI.
The analytic energy coefficients are test inputs, not physical defaults.

The project uses the editor's two-station local example with one authored legacy pod at `station-1`.
The car plan uses passenger hubs `station-1` and `station-2`.
The fixture sets each capacity, time, seat count, and refusal policy explicitly.
Consent omission retains the existing private behavior.

Run these commands from the repository with Go 1.27.1 and `GOEXPERIMENT=jsonv2`.

```sh
go run ./cmd/parkride -project web/testdata/offline/project.json -plan web/testdata/offline/car-report-plan.json -duration 20m -queue-limit 200 -output web/testdata/offline/car-report.json

go run ./cmd/parkride -project web/testdata/offline/project.json -plan web/testdata/offline/car-plan.json -duration 1s -queue-limit 200 -output web/testdata/offline/car-censored-report.json

go run ./cmd/parkride -project web/testdata/offline/project.json -plan web/testdata/offline/car-refused-report-plan.json -duration 20m -queue-limit 1 -output web/testdata/offline/car-refused-report.json

go run ./cmd/parkride -project web/testdata/offline/project.json -plan web/testdata/offline/car-stranded-report-plan.json -duration 20m -queue-limit 1 -output web/testdata/offline/car-stranded-report.json

go run ./cmd/parkride -project web/testdata/offline/project.json -plan web/testdata/offline/car-empty-report-plan.json -duration 1s -queue-limit 200 -output web/testdata/offline/car-empty-report.json

go run ./cmd/compare -project web/testdata/offline/project.json -energy-file web/testdata/offline/energy.json -duration 2s -request-every 1s -redistribution-policies off -format json -output web/testdata/offline/energy-report.json
```

The full report includes a completed round trip and a zero-capacity full-lot refusal.
The one-second report keeps a held slot and a censored pod journey.
The three-car, one-pod report includes outward queue refusal, recovered refusal, and terminal stranding.
The delayed second arrival also causes a terminal return refusal with a held slot.
The empty-plan report contains no car outcomes or lots.

The Node tests import these files without changing outcomes or event clocks.
They also export authored inputs and run them through both actual CLI decoders.
The browser check covers error display with a modified report field.
It does not claim that a fixture command produced a native submission error.
