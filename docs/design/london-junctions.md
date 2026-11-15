# London junction design

Status: proposed on 2026-11-15.
The maintainer made the decisions below on 2026-11-15.

## Goal

Each London station joins its links with chords today.
Every arrival portal has a movement lane to every departure portal, U-turns included.
All chords of a portal start or end at one node, so Bank has 8-way splits and 8-way merges.
Chords that cross with no common node do not conflict, as if each crossing were a flyover.

This design replaces the chords with junctions that a real guideway could have.
The simulator runs the new junctions, so the measured capacity and the conflicts match the drawn shape.
A study compares the two junction forms before the presets change.

## Decisions

| Topic | Decision |
| --- | --- |
| Scope | The generated network changes, not only the ride page. |
| Through stations | The line goes straight through. An off-slip goes to the station, and an on-slip comes back. |
| Junctions | A station with 3 or more links gets one directional ramp from each arm to each other arm. Ramps that cross have no common node, as in a stack interchange. |
| U-turns | There are no U-turn ramps. A pod that must reverse uses the through lane of the station. |
| Splits and merges | A line splits one ramp at a time, at split nodes along the arrival, and takes ramps in one at a time, at join nodes along the departure. Each node is a 2-way split or merge. |
| No room | The spacing between the nodes of a chain shrinks from 40 m to 24 m. Where a chain still does not fit, ramps stage: ramps to one departure merge with each other before they join the line, and ramps from one arrival split from a shared ramp. Where staging also does not fit, the build fails with a layout conflict that names the station. |
| Ramp speed | The speed limit of a ramp is `sqrt(2 m/s² × r)`, where `r` is the smallest radius of the ramp, and at most 14 m/s. The lines keep 14 m/s. |
| Rollout | A generator option and a `-junctions chords\|ramps` flag of `cmd/scenario`. The presets keep chords until the maintainer adopts ramps from the study. |
| Express | `london-full-express` keeps chords, because its approved contract fixes its geometry. |
| Ride page | The ride page draws the ramps at flyover levels only after adoption. |

## Junction shape

A station with `n` links has `n` arrivals and `n` departures.
Each arrival has `n` exits: `n - 1` ramps and the off-slip to the station.
Each departure has `n` entries: `n - 1` ramps and the on-slip from the station.
So an arrival has `n - 1` split nodes, and a departure has `n - 1` join nodes.
A station adds `2n(n - 1)` nodes.
On LondonFull, this adds about 2,600 nodes to the 5,740 of the chord network.

- A terminus has 1 link.
  The arrival leads to the off-slip, and the on-slip leads to the departure.
  It has no split or join node.
- A through station has 2 links.
  Each arrival has one split node: the off-slip leaves, and the line continues to the other departure.
  Each departure has one join node for the on-slip.
- A junction has 3 or more links.
  The ramps leave each arrival in the order of their turn, so that ramps of one arrival do not cross each other.
  The ramp across the gap of the station, beside the berth rows, takes the split and the join nearest to the station center.
  Thus it stays away from the station core, which starts 80 m out.

A Parking facility at a gateway station adds one more exit to each arrival and one more entry to each departure, as its road lanes do today.

## Geometry

The split nodes lie on the arriving carriageway, and the join nodes on the departing carriageway, from the portal outward.
The spacing between two nodes of a chain is 40 m, which is the shortest lane for the large classes.
On a short link, the spacing shrinks to 24 m, the shortest lane.
A chain must stay in the half of the link at its station.

A fit check of the source data gives these results:

- At 40 m, 8 LondonFull stations do not fit: Bank and Monument, Baker Street, West Ham, Stratford, Liverpool Street, Leicester Square, Embankment, and Canary Wharf.
- At 24 m, Bank and Monument (8 links, one of them 182 m long) and Canary Wharf (a 165 m link) still do not fit.
  These two stations use staged ramps.
- LondonCentral needs staged ramps at Bank and Monument only.

A ramp is a quadratic curve from its split node to its join node.
Its control point starts at the station position, as the control point of a short chord does today, and moves to keep the curve on its side of the station.
The generator samples the curve to find its smallest radius and sets its speed limit.
The simulator already brakes ahead of a lane with a lower speed limit.

## Generator changes

- `LondonOptions` gets a junction option.
  Its zero value is chords, so the presets and Express stay byte-identical.
- `londonCapacity.equal` compares the option, so a ramps build is not the cached preset.
- `checkLondonSize` counts the nodes and lanes of the ramps, the chains, and the staged ramps.
- The carriageway lanes that a chain splits keep the `london-link-` prefix and the separation group of their link, because the layout audit and the heading search sort lanes by this prefix.
- Each ramp has its own separation group, as each chord has today.
- New node IDs do not use `<station>-merge`, which is the station merge node.
  The design uses `<station>-split-AA-K`, `<station>-join-DD-K`, and `<station>-ramp-AA-DD`.
- The heading search gets the ramp segments in place of the chord segments, so it keeps station cores away from the ramps.
- The layout audit treats ramps as movement lanes.
  A core lane that crosses a ramp is a hard conflict, as today.

These readers use the portal shape, and the slice checks them:

- The view anchors and the label ranks of collapsed stations, which use the approach lanes.
- The ride page, which finds hubs by mainline crossings and gives the station lanes their levels.
- The layout audit, the London size check, and the tests that pin London hashes and counts.

## Failure modes

- A chain or a staged ramp that does not fit fails the build with a layout conflict that names the station and the link.
- A ramp that crosses a station core fails the layout audit.
- A network over the node or lane limit fails the size check, as today.
- A pod can need to reverse where no U-turn exists.
  It then passes through a station and adds load to its core.
  The study counts the U-turn chords that the chord network uses, to show how much traffic moves.

## Tests

- A through station has one split and one join for each direction, and no U-turn.
- A terminus has no split, no join, and no U-turn.
- A junction has `n(n - 1)` ramps, and the ramps of one arrival do not cross each other.
- The spacing is 40 m where a chain fits, and shrinks to 24 m on a short link.
- Bank and Monument and Canary Wharf build with staged ramps.
- A constructed short link that staging cannot fit fails with the named station.
- The ramp speed follows the smallest radius, and the lines keep 14 m/s.
- `checkLondonSize` matches the built counts.
- Both presets build with ramps and have no hard conflicts.
- The chord presets, Express, and their pins do not change.
- The `-junctions` flag refuses a ring preset and the Express preset.

Each new test is checked by mutation.

## Study and adoption

The study runs LondonFull with chords and with ramps.
It uses the method of the [station scale study](../studies/station-scale.md), at 15 requests per minute and at one higher rate, with seeds 1 to 3.
A timed pilot comes first.
The study also counts the U-turn chords that the chord network uses.

Ramps are adopted when these hold:

- At 15 requests per minute, ramps drain at least as many seeds as chords.
- The mean wait and the mean journey are at most 5% longer than with chords, as a mean of the paired changes.

Three seeds can turn one drained run into an undrained run, as the Bank re-check of the station scale study showed.
A drain difference of one seed therefore leads to three more seeds before the decision.

## Slices

1. The generator option, the flag, through stations, termini, junctions with chains, staged ramps, and the tests.
2. The study, and the adoption decision.
3. After adoption: the presets use ramps, and the ride page draws flyover levels in place of the hub decks.
