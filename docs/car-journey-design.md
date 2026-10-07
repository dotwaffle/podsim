# Offline car journeys and parking

Status: implemented and checked on October 3, 2026, under the October 2 compatible-contract grant.
The [qualification record](measurements/car-journey-qualification.json) identifies the source and verification evidence.
This adds an offline `cmd/parkride` command and a separate car ledger.
Project, session, saved simulation, and stream schemas stay at their current versions.

## Authored inputs

The command requires a validated project and a finite JSON itinerary plan.
It starts an empty car ledger at simulation tick zero.
It does not generate daily repetitions or road traffic.
The project supplies the pod fleet, geometry, sharing policy, and experiments.

Each lot has a unique ID, a passenger hub, and an explicit nonnegative integer car capacity.
Car capacity does not use pod berth counts or `ParkingOnly` stations.
Each itinerary has unique itinerary and car IDs, one lot, one destination, and one immutable party.
Car seats and party size are explicit positive integers.
Party size must fit the authored car and existing native admission limits.
Both native pod legs retain ordinary certified class, route, berth, and consent checks.

Each itinerary requires these nonnegative integer-second inputs:

- Departure time and outward car travel duration.
- Return-not-before time and minimum destination activity duration.
- Retrieval delay and homebound car travel duration.

Zero durations are explicit authored values.
The parser rejects missing required values, explicit null, unknown members, duplicate identities, and overflowing tick sums.
Missing sharing consent means private, consistent with new native orders.
Explicit consent must be private or shared and applies to both pod legs.
The command has no global consent override.

Queue refusal policies are required authored values.
The first model supports outward `drive-home` and return `retain-car`.
It rejects omitted or unsupported policies.
These values describe the plan and do not become session defaults.
The reader uses the 32 MiB project-file bound for each input, and the plan decoder keeps its 10 MiB bound.
Before allocation, charge 4,096 bytes per itinerary and 512 bytes per lot against the existing 256 MiB offline storage bound.
The charge includes two offers, native experiment records, request bindings, events, and owned report copies.
This resource bound is not a physical lot or traveler limit.

## State and timing

Cars reach lots at departure plus outward travel time.
Simultaneous arrivals use itinerary ID order.
A full lot refuses the itinerary once and generates neither pod order.
The report records a full-lot refusal without inventing fallback travel.

A parked car holds one slot through both pod legs and destination activity.
Return eligibility is `max(returnNotBefore, actualOutboundAlighted + minimumActivity)`.
The return cannot precede actual outward unloading.
Actual return unloading starts the retrieval delay.
The car releases its slot when retrieval ends and the homebound car leg starts.
Home arrival completes the round trip.

Each pod offer gets one queue admission attempt.
An outward refusal starts retrieval, then the authored homebound car leg.
A return refusal leaves the car parked and the party away.
The ledger records the refusal and does not retry or release that car.
Preflight validates both native legs against the configured fleet.
Unexpected native submission errors stop the run with partial ledger evidence.
They do not create recovery travel or a successful outcome.

Process initial events at tick zero before the first native step.
Process each advanced tick in this order: actual pod completions, due car releases and home arrivals, eligible return offers, then lot arrivals.
Eligible returns use itinerary ID order, as do simultaneous lot arrivals.
An offer is queue-refused when native pending orders are at least the authored command queue limit.
The limit must fit the existing offline queue bound.
Outward offers follow their lot admissions.
Zero retrieval and car durations finish their transitions in the same tick.
An outward refusal with zero retrieval can release space for the next simultaneous arrival.
Same-tick release permits another car to enter the lot.
Car occupancy continues across midnight and the report horizon.
Stopping requires completion of car travel and activity, not only an empty native order queue.
Home arrival after outward refusal records a recovered refusal, not a completed round trip.
A return refusal records a terminal stranded outcome with its slot held.
Early stopping requires that every itinerary has a terminal outcome.
It does not label stranded or refused itineraries as successful round trips.
The existing 24-hour offline horizon bounds a run.
Process completions and car transitions at the horizon, but admit no new pod offers there.
Due car arrivals at the horizon can enter a lot or receive a full-lot refusal.
An admitted car holds its slot with the outward pod leg unissued.
Due pod offers at the horizon remain censored.
Unfinished stages remain censored with their identities and held slots.

The ledger owns its plan and records.
An in-memory clone copies the ledger independently alongside `Simulation.Clone`.
Durable car continuation was outside this first implementation; see the later [continuation qualification](car-continuation-qualification.md).
Restoring the simulation alone cannot recover car release events.

## Report and gates

Itineraries are the sole offer source.
Project daily and rail plans do not inject orders into this run.
The JSON report includes canonical project and plan hashes, build provenance, horizon, actual endpoint, and every itinerary outcome.
The plan hash includes effective consent and authored refusal policies.
The report identifies effective native pod policies.
It records both pod request IDs, offer and completion ticks, refusal reasons, car arrival and release ticks, and home arrival.
Each lot reports peak and final car occupancy.
Completed round trips report door-to-door duration.
The report keeps native pod metrics separate from car outcomes.

Tests cover zero and one-slot lots, simultaneous arrivals, release before arrival, delayed outward completion, and both queue refusals.
They also cover duplicate completion delivery, overnight retention, horizon censoring, consent on both legs, overflow rejection, and clone isolation.
Tests also cover tick-zero events, same-tick refusal release, return/outward queue competition, and exact-horizon completion.
Real native journeys must complete both pod legs and preserve car identity.
Physical checks and clone continuations cover the selected native fixtures.
The first model makes no road-capacity, battery, or empirical travel-time claim.

Use the [command instructions](../internal/parkride/README.md) and supplied plan fixture to run this model.
The command honors project redistribution with itinerary-origin weights and the existing observed-rate fallback.
Native experiment histories remain off.
The ledger retains its own bounded request bindings and actual unloading times.
