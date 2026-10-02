# Comparison party consent

New compare offers are private singletons unless you set `--sharing-consent shared`.
The same consent applies to every policy arm.
Pooling limits, join policies, and routing modes do not supply party consent.

Use explicit shared consent for a sharing study:

```sh
go run ./cmd/compare --sharing-consent shared --sharing-limits 1,8
```

Use `--sharing-consent private` for a labeled private control.
Private parties do not pool, even with a party limit of eight.
Offer timing, endpoints, schedule IDs, and queue admission limits stay unchanged.

Each new JSON result records its effective `sharing_consent` under the existing report version.
Table and CSV add the consent column only when you supply the flag.
The default table and CSV columns stay unchanged.
Record the command with each study, especially when the consent column is absent.
Historical reports without recorded consent do not establish willingness to share.
