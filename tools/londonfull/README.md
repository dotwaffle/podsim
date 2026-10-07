# LondonFull source conversion

This converter prepares data for the LondonFull preset.
It does not enable a preset or change project limits.
The existing central topology and 2019 demand files remain unchanged.

Run with Python 3.9 or later from the repository root:

```sh
python3 -I tools/londonfull/convert.py SOURCE_DIRECTORY OUTPUT_DIRECTORY
python3 -I tools/londonfull/convert.py --dlr SOURCE_DIRECTORY internal/scenarios/data
python3 -I -m unittest discover -s tools/londonfull -v
```

`SOURCE_DIRECTORY` must contain the files listed in `sources.json`.
The manifest records their source URLs, byte counts, and SHA-256 hashes.
The converter reads local files and makes no network requests.
The Tube conversion uses the 24 files other than the two DLR route files.
The NUMBAT files must match their pinned hashes.
Tube stops, hub IDs, and ordered sequences must match `london-full-provenance.json`.
The source audit retrieved these files on September 28, 2026.
The [DLR conversion](#dlr-conversion) also uses the two DLR route files.

## Topology

The 22 TfL Unified API route responses cover both directions of all 11 Tube lines.
They contain 272 distinct source stops.
The normalized topology has 269 passenger sites and 313 undirected links.
The three approved merges are:

| Source | Site |
| --- | --- |
| Monument, `940GZZLUMMT` | Bank and Monument, `940GZZLUBNK` |
| Paddington H&C, `940GZZLUPAH` | Paddington, `940GZZLUPAC` |
| Hammersmith H&C, `940GZZLUHSC` | Hammersmith, `940GZZLUHSD` |

Edgware Road keeps two distinct sites.
The generator will use bidirectional PRT guideways, not Tube service frequencies.
`london-full-provenance.json` retains all source stops, site mappings, line directions, branch IDs, and ordered stop sequences.
Station coordinates come from the API, including Nine Elms and Battersea Power Station.
Their positive longitude values in the NUMBAT definitions are not used.

## Demand

Demand comes from the 2024 Tuesday-to-Thursday network OD matrix.
The filter retains journeys whose endpoints map to the Tube roster.
Journeys can use other modes between those endpoints.
This is different from the central preset's 2019 LU-specific demand.

`aliases.json` lists the 39 mixed-mode stations whose primary ID is a National Rail NaPTAN ID.
For each alias, conversion checks the NUMBAT code, exact workbook name, and primary ID.
It also checks the selected Tube ID and normalized station name against the API roster.
West India Quay, code 866, is marked LU in the definitions but is outside the Tube roster.
It is the only reviewed definition exclusion.

The normalized data contains 60,996 directed OD pairs across all 269 sites.
Decimal addition preserves the published weights when source codes merge into one site.
Same-site rows and rows with endpoints outside the roster are excluded.
Duplicate source OD/time-band rows cause an error instead of double counting.
Weights must be finite and positive in the source.
An absent weight becomes zero in the normalized CSV.
Rows and columns have a fixed order, and no pair truncation occurs.

The CSV preserves all eight source time-band columns.
The source has no Early or Night rows, so those columns are zero.
Only bands 2 through 7 have positive demand: Morning, AM peak, Interpeak, PM peak, Evening, and Late.
The full preset must expose these six selections and must not invent Early or Night demand.
Central time-band selections remain unchanged.

The provenance file records exact band totals and exclusion counts.
The converter fails if the expected roster, alias, endpoint, or pair counts change.
Review new source files and mappings before updating the pinned manifest.

## DLR conversion

The `--dlr` mode writes `london-full-dlr.json`, `london-full-dlr-od-2024.csv`, and `london-full-dlr-provenance.json`.
LondonFull embeds its topology and demand.
The Tube-only topology and demand files are no longer stored in the repository.
The Tube provenance remains the content pin for both modes.
Plain Tube mode still reproduces its original topology and demand bytes.
The DLR route files were retrieved on October 7, 2026.
The DLR and NUMBAT files must match their pinned hashes.

On October 7, 2026, TfL served different bytes for 20 of the 22 Tube route files.
Their stops and sequences were unchanged.
Both modes require the Tube stops, hub IDs, and sequences in `london-full-provenance.json`.
Neither mode requires the Tube file hashes.
Fields that conversion does not read can change.

The 45 DLR source stops give 40 new sites after merges and one omission.
The topology has 309 sites and 358 links.
The 45 DLR links have the line `dlr`.
`dlr.json` lists the reviewed site decisions.
A DLR stop merges into a Tube site only when one NUMBAT code has both the LU and DLR modes and the stops share a NaPTAN hub.
Bank, Canning Town, Stratford, and West Ham merge.
Canary Wharf DLR shares hub `HUBCAW` with the Jubilee station, but NUMBAT gives it a separate code, so it stays a separate site.
A DLR stop that shares a hub with a Tube stop must have a merge or separate entry.
The Tube provenance pins each `topMostParentId` as `hub`.
Conversion rejects inconsistent hub IDs across route files.

The DLR demand keeps journeys between Tube and DLR endpoints, including journeys on other modes.
It maps 310 NUMBAT codes to the 309 sites.
West India Quay, code 866, is excluded from demand.
The `omitted` entry in `dlr.json` records the layout reason and the guideway bypass through Canary Wharf DLR.
The omission clears the space beside Canary Wharf Jubilee and the Poplar delta.
Provenance retains the source stop and sequences, with a null site ID for West India Quay.
Greenwich, code 928, and Woolwich Arsenal, code 573, have rail primary NaPTAN IDs, so `dlr.json` gives their DLR stops as aliases.
The normalized CSV has 76,567 directed OD pairs with exact source weights.
The provenance file records the merges, the nearest Tube site of each DLR stop, the counts, and the band totals.

## Validation boundary

These data checks do not establish layout safety, fleet capacity, or a request rate.
A generated project must pass normal project validation and layout checks.
Resource-limit changes also require saved-state and stream maximum-size checks.
The stream JSON and compressed limits are 65 MiB and 66 MiB.
