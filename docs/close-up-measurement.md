# Close-up following: measured benefit

This record measures how much layer B of the clearance design (close-up following) could gain on the presets.
It is a measurement before a design decision, and no close-up code exists.

## Method

A temporary probe at commit `95dbeb9` counted each tick in which a traveling pod's request was refused by track cells of one other traveling pod ahead, with platooning off.
It then asked whether close-up following could have granted the request, and estimated the saved pod time.
Thirteen `cmd/compare` arms ran with seed 1: the `small`, `busy`, `parking-constrained`, depot, and `compact-station` presets at 45 s and 10 s hotspot demand, `rail-hub`, `scale100`, and `london-central`.
The 10 s runs are overloaded.

## Results

Track-only holds took 0.02% to 2.27% of traveling pod time at the documented loads and 13.1% in compact-10.
Refusals by nodes, junctions, berths, or two pods took more pod time than the holds in every run.
The analytic upper bounds on the saving were 703 pod-s (compact-10), 816 pod-s (scale100), and 567 pod-s (rail-hub), or 0.3% to 2.4% of traveling time.
The leader is usually held too, and junction waits set the rate in compact-10 and scale100.

## Conclusion

Layer B is not worth building for the current presets.
At the documented loads, track-only holds take at most 2.3 % of traveling pod time, and London takes 0.11 %.
The estimated saving is at most about 1.2 % of pod time, and refusals by junctions and nodes, which close-up cannot share, are larger than the holds.
The ring depot lanes have almost no holds.
The only material case is a station entry queue on one approach lane at overload (compact station at 10 s), where 10.6 % of traveling time is a feasible hold, but the entry junction limits the rate.
Revisit layer B only for a station or depot design whose throughput depends on long, slow queues on one lane, and measure that design with this probe first.

## Caveats

Each preset has one seed and one or two loads, so the shares show the order of size only.
Platooning is off, and the estimate does not model queue spillback.
