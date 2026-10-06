# Label cache admission measurements

The cache retains useful entries when more than 1,024 distinct labels repeat.
The 1,500-label native probe takes about 64% less time than the previous cache.
The Chrome WASM probe takes about 67% less time.
These measurements cover label dimensions, without rendering or network work.

## Cache behavior

The previous cache clears all entries on the first miss at capacity.
A recurring 1,500-label cycle therefore repeats measurement and insertion for every label.

The new cache stops inserting at capacity and retains its existing entries.
It observes each group of 1,024 lookups while full.
More than 768 misses clear obsolete entries so a changed scene can populate the cache.
Exactly 768 misses retain the entries.
Font replacement clears both entries and observation counters.

Each Game still owns its cache.
Keys retain text, effective font size, and line spacing.
Text longer than 512 bytes bypasses the cache.
Inserted text still uses an independent copy.
Label dimensions and rectangle rounding remain unchanged.
The added counters do not change the 1,024-entry storage limit.

## Measurements

Both variants use the same fixed multiline strings and changing screen positions.
Setup and dimension checks remain outside the timed loops.
The previous cache and the new admission rule run in baseline, candidate, candidate, baseline order.
Native runs use GOMAXPROCS 2 and GC 100, with three 300 ms repetitions per arm.
The table reports the median of six samples per variant.

| Distinct labels | Previous cache | New admission rule |
| ---: | ---: | ---: |
| 1 | 48 ns | 47 ns |
| 600 | 51 ns | 51 ns |
| 1,500 | 489 ns | 177 ns |

The small working sets remain allocation-free after warmup.
At 1,500 labels, native allocation bytes decrease from about 152 to 45 per lookup.
Native allocation counts round down and do not imply zero allocations for that case.

Chrome 143.0.7499.4 runs the same WASM probe in four separate browser contexts, with 150,000 lookups per case.
The 1,500-label mean decreases from 310.7 to 103.8 milliseconds.
Allocated bytes decrease from 22,800,032 to 6,854,400 per case.
Allocation counts decrease from 300,000 to 47,600.
All dimension checks and checksums match, and the candidate retains at most 1,024 entries.
The 1-label and 600-label cases show no evident regression.

The probes run sequentially after the owned capacity job stops.
No other agent CPU-heavy job overlaps these timings.
Unrelated user processes remain outside the experiment's control.
Two browser samples per variant do not establish a general rendering or whole-browser CPU gain.

## Checks and evidence

Tests cover recurring 1,500-label cycles, scene replacement, font invalidation, and the 768/769-miss threshold.
They retain the original dimension, oversized-text, and separate-Game ownership checks.
Independent review finds no correctness or measurement blocker.
The full plain suite and sim/session/view/statestore race suites pass.
Vet, lint, fresh gopls, and native/WASM builds pass.

The raw measurement data is in git history.
The threshold tests were added after the performance binaries were frozen.
Production code remains identical to the measured candidate.
An earlier label measure cache study is in git history.
