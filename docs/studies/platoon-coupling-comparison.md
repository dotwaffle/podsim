# Virtual platoons and coupled trains

This record compares virtual platoons with coupled trains on throughput and travel time.
The maintainer used it on October 6, 2026, to stop coupling work and to ask for a removal plan.
Physical coupling was removed on October 6, 2026.
This record stays as the reason for the removal.

## Setup

Source `ae39088` in a scratch copy.
Arm A had no platoons, B and C had virtual platoons with limits 2 and 4, D and E added coupling at limits 2 and 4, and F and G raised the limit to 6 and 8.
The probe network was built for coupling (70 Compact pods, 8 orders each minute).
The rail-hub and london-central presets have only legacy pods, which cannot couple, so D and E equal B and C there.
The main plan ran 3 seeds for 20 simulated minutes, and a supplement ran 5 seeds for 35 minutes.
Ten reruns with every per-tick check on gave the same results.

## Results

On the probe (mean of 3 seeds), A completed 98.3 of 160 orders, B 103.7, C 105.3, and D 93.3.
D against B completed 10.3 fewer orders, with mean travel 20.4 s longer, and it was worse in 3 of 3 seeds.
In the supplement, D against B completed 24.4 fewer orders and E against C 11.0 fewer, worse in 5 of 5 seeds each.
D also completed fewer orders than A, so the trains cost more than the platoons gain.
On the presets, platoons covered under 1% of traveling time in london-central and added 0.7 orders in rail-hub.
Limits 6 and 8 gave the same results as 4, because platoons form only among slow pods and rarely exceed 4.

## Causes

- Pods in an approach were stopped for 84% of the approach time at the assembly point.
- A formed train needed 78.7 s on average to pass its corridor, at a mean of 2.6 m/s against a limit of 14 m/s.
- The stopped time of traveling pods outside a platoon link rose from 1,511 to 2,774 pod-seconds per run.
  The cause is an inference, because the measurement does not identify the blocking pod.

## Limits

The signs rest on 3 to 5 seeds, trains form only on the probe network, and demand is one balanced pattern near fleet capacity.
The completed count is the primary signal, because travel times count completed orders only.
