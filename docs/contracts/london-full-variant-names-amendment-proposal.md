# LondonFull variant names amendment

Status: approved by the maintainer on 2026-11-26, with decision VN-D1 B, and applied to the approved contracts below.

## Change

On 2026-11-26 the maintainer merged the five LondonFull presets into one preset with a variant flag.
Each variant keeps its network, its project, and its hashes.
Only the names change, and the old names are removed.

| Old name | Command | Key | Task |
| --- | --- | --- | --- |
| `london-full` | `-preset london-full` | `london-full` | |
| `london-full-express` | `-preset london-full -express dedicated` | `london-full:dedicated` | `test:express-dedicated`, before `test:london-express` |
| `london-full-shared-express` | `-preset london-full -express shared` | `london-full:shared` | `test:express-shared`, before `test:shared-express` |
| `london-full-shared-express-wharf` | `-preset london-full -express wharf` | `london-full:wharf` | `test:express-wharf`, before `test:wharf-express` |
| `london-full-shared-express-wharf-hubs` | `-preset london-full -express hubs` | `london-full:hubs` | |

The key is the name of the project in subtest names and golden files.
`PODSIM_SOAK_PRESETS` takes the keys `london-full:dedicated`, `london-full:shared`, and `london-full:wharf`.
The exported generators `GenerateLondonFullExpress`, `GenerateLondonFullSharedExpress`, `GenerateLondonFullSharedExpressWharf`, and `GenerateLondonFullSharedExpressWharfHubs` become `GenerateLondonFullVariant` with a variant.

## Contracts that name the old presets

These approved contracts name an old preset, a task, or a generator:

- [Express physical encoding](express-physical-encoding-contract-proposal.md)
- [Express London qualification](express-london-qualification-proposal.md) and [adoption screen](express-london-adoption-screen-proposal.md)
- [Express shared-track qualification](express-shared-track-qualification-proposal.md), [shared-track amendment](shared-track-amendment-proposal.md)
- [Express wharf qualification](express-wharf-qualification-proposal.md)
- [Shape clearance always on](shape-clearance-always-on-proposal.md), [London fillets](london-fillets-proposal.md)
- [Incident emergencies](incident-emergency-contract-proposal.md), [incident stage 7](incident-stage7-contract-proposal.md)
- [Restore speed](restore-speed-proposal.md), [exact restore coordinates](exact-restore-coordinates-proposal.md), [bank load choice](bank-load-choice-proposal.md)

The withdrawn [distant berth target](distant-berth-target-proposal.md) also names them, and is not approved.

## Decision

| ID | Question | Options | Decision |
| --- | --- | --- | --- |
| VN-D1 | How the approved contracts take the new names | A: this amendment gives the mapping, and each contract gets one line that points to it. B: each occurrence in each contract is changed to the new name. | B |

Each old preset name in these contracts is now its key, each old task is its new task, and each old generator is `GenerateLondonFullVariant`.
No row, gate, or hash of a contract changes.
Commit subjects, commit IDs, quotations of other documents, and the names at file:line references are not changed.
A command in a record of an earlier run gives the new names.
The run itself used the old names of the table above, because the new names start at the commit of this change.
