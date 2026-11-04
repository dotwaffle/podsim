# LondonFull pickup supply screen

Initial fleet placement improves short-run waits, but the gain does not qualify the tested fleets.
All changed fleets exceed the approved individual pickup and journey limits in six-hour runs.
Keep the existing 287-pod fleet and experimental defaults.

## Comparison

Source `943fd74` on the `london-full` scenario, AM demand at 12 offers per minute, seeds 1 and 4, queue limit 200, six hours of arrivals, eight-hour cap.
Sharing, buffers, swaps, and positioning are off.
Four fixtures: the 287-pod baseline, Placed 287 (the 18 parking pods move to spare passenger berths), Parking 300 (13 added pods in parking berths), and Placed 300 (both changes).
Placement follows each station's maximum origin share over the six demand bands.

## Results

Every arm completes all offers.
Mean pickup wait changes from the baseline by -5.7% to +1.0%, and Placed 300 gives the largest gain (5.46% for seed 1, 5.70% for seed 4).
Every changed fleet exceeds the individual pickup and journey limits for hundreds of matched offers (585 to 839 pickup and 397 to 539 journey exceedances per arm).
The largest pickup regressions range from 1,432.63 s to 2,289.10 s.
Recovery after the arrival window lengthens in some arms (up to 633 s for Parking 300, seed 4), which fails the baseline recovery gate.
Placement does not consistently reduce empty travel, and the 13 added pods increase it in both seeds.
The aggregate 2% limits pass and do not waive the individual failures.
The failed gates stop broader unchanged runs for these candidates.
