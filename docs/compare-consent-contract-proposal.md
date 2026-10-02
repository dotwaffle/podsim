# Compare consent provenance

Status: approved October 2, 2026.
Implementation and qualification are in progress.

The approved order foundation makes new orders private by default.
Compare must supply the same explicit consent to matched offers in every policy arm.
A pooling policy does not supply consent.
The new optional `--sharing-consent=private|shared` flag uses that approved order contract.

Approve these additional output changes:

- Each JSON result adds `sharing_consent` with its effective `private` or `shared` value.
  Omission of the flag produces `private`.
- Table and CSV output add a `sharing_consent` column only when the user supplies the flag explicitly.
  The flag help describes this column.
  Default table and CSV columns stay unchanged.
- `schedule_id` continues to identify offer timing and endpoints.
  Consent is recorded separately, so matched policy arms retain the same offer identity.

Consent applies to every offer in the run and cannot vary with an arm's pooling limit or mode.
Explicit sharing studies must opt into `shared`; private control studies must remain private.
No queue limit, byte limit, fare, timetable, or physical vehicle profile changes.

Validation will cover omitted and explicit flag values, invalid choices, matched arms, private admission with pooling enabled, shared admission, and JSON/table/CSV provenance.
