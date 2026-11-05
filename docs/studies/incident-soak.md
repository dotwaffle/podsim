# Incident soak timing

This record holds the timing run that decision 16 of the [incident stage 7 contract](../contracts/incident-stage7-contract-proposal.md) asks for.
The full soak of section 5.1.9 has not run, and this record holds no result for it.
The maintainer chooses the size from these numbers.

## Method

`TestIncidentSoak` in `internal/session` drives each run.
The driver sets 10 faults and 10 emergencies an hour, faults of 600 seconds, a debris share of 0.25, and debris of 2 meters.
It does not read the `rail-hub-incidents` preset, which has faults of 120 seconds.
The status line under decision 7 of the contract keeps the soak at 600 seconds, because section 5.1.9 names no duration.

Each rate is ticks for each CPU second of the process, from `/proc/self/stat`, in simulated windows of 5 minutes.
One run has 24.5 simulated hours, which is 5,292,000 ticks.
All runs used seed 1, one process for each arm, on a host of 12 threads.
Other sessions used the host, with a load average from 5 to 90.
CPU time changes less than wall time under load, but these rates are still not from a quiet machine.
Each timed process used 57 to 150 MB of resident memory.
`TestIncidentSoak` runs its groups in parallel, so a matrix run needs `-parallel` and a memory cap.

## Measured rates

The queue is at its limit of 200 orders in each row, unless the row states another size.

| Preset | Arm | Every-tick checks | Ticks for each CPU second | CPU hours for a run |
| --- | --- | --- | ---: | ---: |
| rail-hub | reference | Off | 6,000 | 0.25 |
| rail-hub | emergencies | On | 1,170 | 1.26 |
| rail-hub | incidents | On | 1,050 | 1.40 |
| rail-hub | platoons | On | 1,080 | 1.36 |
| london-central | reference | Off | 3,800 | 0.39 |
| london-central | emergencies | On | 450 | 3.27 |
| london-central | incidents | Off, queue 190 | 157 | Not applicable |
| london-central | incidents | On, queue 143 | 187 | Not applicable |
| london-central | incidents | On, projected | 125 | 11.8 |

The rates did not drift in a run with no incident: 4 simulated hours on rail-hub, and 2 on london-central.
No london-central run with faults reached a full queue with the checks on in the 5 minutes of a scratch run.
So the projected row adds the cost of the checks to the rate at queue 190.
The checks cost 1.4 to 2.4 ms for each tick on london-central, and about 0.7 ms on rail-hub.

The london-central preset already has a platoon limit of 4, so its platoons arm is the same project as its incidents arm.
The two arms give the same run, seed for seed.

## Cost of a fault on london-central

The fault arms cost 25 times the reference arm, with the checks off.
A profile of 10 simulated minutes shows 61 percent of the CPU time in `keepTripPod`, through `pickupAccess` and `searchRoute`.
This path does not use the memo of failed pickup searches.
At 157 ticks for each CPU second, the fastest speed that one core can hold is about 2.6 times real time.

## Extrapolated matrix

| Size | Runs | CPU hours | Wall hours on 12 threads |
| --- | ---: | ---: | ---: |
| Section 5.1.9, as approved | 200 | 640 | 54 |
| R1: no london-central platoons arm or its reference | 160 | 400 | 33 |
| R2: R1, and 5 seeds for the london-central incidents arm | 145 | 222 | 19 |
| R3: R1, and a dispatch change that reaches the emergencies rate | 160 | 228 | 19 |

R1 loses no coverage, because each dropped run repeats another run.
R2 needs an amendment, because the Safety row asks for at least 20 seeds.
R3 is a target and not a measurement, because no such change exists.

## CI soak

`TestIncidentSoakCI` took 285 seconds at a load average of about 30: a reference run of 51 seconds, and a checked run of 233 seconds.
The checked run had 9 faults and 9 emergencies, and it passed.
It uses rail-hub only, because the CI row names no preset and a london-central run takes an estimated 20 to 30 CPU minutes.
`mise run qualify` also runs `TestSampledTickLatency`, in about 3 seconds, and `TestSampledTickNoCandidateLatency`, in more than 275 seconds.

## Readings from a busy host

These readings are not results of a gate.
The Performance rows need a quiet machine (A3).

| Reading | Value | Load average |
| --- | --- | ---: |
| Sampled tick, each policy, cold and warm, both networks | Slowest 0.95 ms, limit 50 ms | 18 |
| Sampled tick at the project limits, cold | 260 to 390 ms | 5 |
| Sampled tick at the project limits, warm, 3 of 4 policies | 14 to 31 s | 5 |
| Timed run, rail-hub, 3 seeds, 0.1 simulated hours | Median 81 to 87 µs against 88 to 98 µs | 16 |
| Timed run, london-central, seed 1, 0.1 simulated hours | Median 154 µs against 145 µs | 16 |
| 64 debris and 300 pod faults | Median 121 µs against 6.6 µs, slowest 1.25 ms against 1.71 ms | 7 |
| 4 emergencies, london-central | Median 21 µs against 14 µs, slowest 2.9 ms against 3.5 ms | 7 |
| 4 emergencies, rail-hub | Median 2.5 µs against 2.3 µs, slowest 27 µs against 48 µs | 7 |
| 64 debris and 1,000 pod faults | Median 983 µs against 20 µs, slowest 5.0 ms against 0.62 ms | 7 |

The sampled-tick fixture has little traffic, so its step is cheap.
`TestSampledTickNoCandidateLatency` did not finish in the 275 seconds of its scratch run, so the predictive warm row has no reading.
The row of 300 pod faults misses the rule of 10 percent, against a reference step that does almost no work.
