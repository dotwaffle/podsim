# Virtual platoons and coupled trains

This record compares virtual platoons with coupled trains on throughput and travel time.
The maintainer used it on October 6, 2026, to stop coupling work and to ask for a removal plan.
Physical coupling was removed on October 6, 2026.
The physical coupling code, its tests, and its contract that this record names no longer exist.
This record stays as the reason for the removal.

## Setup

- Source: `ae39088`, in a scratch copy.
  The only source change was `MaxPlatoonLimit` raised to 8 for arms F and G.
- Arms: A has no platoons.
  B and C have virtual platoons with limits 2 and 4.
  D has platoons with limit 2 and coupling.
  E has platoons with limit 4 and coupling.
  F and G have platoons with limits 6 and 8.
- Networks:
  - The probe is the tailored network of `TestCouplingNaturalMultiPairProbe` (removed with the coupling feature), with 70 Compact pods and 8 orders each minute.
  - The rail-hub preset has 30 legacy pods and 6 orders each minute.
  - The london-central preset has 114 legacy pods and 12 or 20 orders each minute.
- Demand: the balanced pattern of the probe, with the same demand in each arm of a seed.
- Main plan: arms A, B, C, D, F, and G, with 3 seeds and 20 simulated minutes.
  Probe seeds 4, 5, and 8, and preset seeds 1, 2, and 3.
- Supplement: all arms, with 5 seeds and 35 simulated minutes.
- The per-tick contract, safety, member motion, and platoon checks were off in the arms.
  `CouplingError` (removed with the coupling feature) and `CompactQueueError` (removed with station buffers and compact queues) ran each tick, and the contract and safety checks ran once each simulated minute.
  Ten reruns with every per-tick check on passed and gave the same results.
- Probe seed 4 reproduced the pinned 95 of 160 orders for B and 84 of 160 for D.

The presets have no coupling corridors, and their fleets are legacy class, which cannot couple.
So D equals B and E equals C on the presets, and those arms did not run there.

## Results

Probe, main plan, mean of 3 seeds:

| Arm | Completed of 160 | Travel mean, s | Travel p90, s | Wait mean, s | Trains |
| --- | ---: | ---: | ---: | ---: | ---: |
| A | 98.3 | 384.2 | 546.4 | 35.5 | 0 |
| B | 103.7 | 371.9 | 523.0 | 33.7 | 0 |
| C | 105.3 | 368.5 | 519.8 | 29.6 | 0 |
| D | 93.3 | 392.2 | 575.9 | 38.6 | 5.0 |
| F, G | 105.3 | 368.5 | 519.8 | 29.6 | 0 |

D against B on the probe: 10.3 fewer completed orders, and mean travel 20.4 s longer, worse in 3 of 3 seeds.
In the supplement, D against B gave 24.4 fewer completed orders and E against C gave 11.0 fewer, worse in 5 of 5 seeds each.
D also completed fewer orders than A, so the trains cost more than the platoons of B gain.

On rail-hub, C against B gave 0.7 more completed orders.
On london-central, platoons of 2 covered less than 1% of traveling time, and the arms did not differ measurably.

## Causes

- Pods in an approach were stopped for 84% of the approach time at the assembly point.
- A formed train needed 78.7 s on average to pass its corridor, at a mean of 2.6 m/s against a limit of 14 m/s, and it was stopped for 43% of that time.
- Stopped time on the assembly lanes doubled against B, and pods stopped on the split lanes, which does not occur in B.
- The stopped time of traveling pods outside a platoon link went from 1,511 to 2,774 pod-seconds per run.
  The measurement does not identify the pod that blocked them, so blocking by the pairs is an inference.
- Most pairs at rest at the assembly point were refused before an approach started.

In the 20-minute runs, platoon limits 6 and 8 gave the same results as 4, because no platoon had more than 4 pods.
In the 35-minute supplement, platoons of 5 or 6 were rare: the mean completed orders differed by 0.2, and one seed completed one order fewer.
Platoons form only among slow pods, so a higher limit gives few longer platoons.

## Limits of this evidence

- The main plan has 3 seeds and 20 minutes, so its per-seed signs are weak evidence.
  The supplement agrees in sign for each coupling comparison.
- Trains form only on the probe network, which was built for them.
- The demand is the balanced pattern only, at rates near fleet capacity.
- Travel times count completed orders only, so the completed count is the primary signal.
- The run did not time the phases of a train separately.
- The limits 6 and 8 runs did not test compact station queues, saved states, or project validation at those limits.
