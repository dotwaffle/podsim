package sim

import (
	"errors"
	"maps"
	"math"
	"reflect"
	"slices"
	"testing"
)

func TestCouplingAlignedIndividualFootprints(t *testing.T) {
	t.Parallel()
	input := couplingReservationFixture(t, false)
	count := input.Prepared.laneCells["ab"].count()
	end := float64(2) * 200 / float64(count)
	front, rear := input.Members[0], input.Members[1]
	if rear.Distance != end || front.Distance != end+12 || rear.BlockIndex != rear.ReservedThrough {
		t.Fatal("fixture does not stop rear at its own cell end")
	}
	for i, member := range input.Members {
		for cell := 0; cell <= member.ReservedThrough; cell++ {
			releaseAt := float64(cell+1)*200/float64(count) + 12
			if releaseAt <= member.Distance {
				continue
			}
			r := resource{kind: trackResource, id: "ab", cell: cell}
			if input.Owners[r] != (resourceOwner{kind: podOwnerKind, id: member.Vehicle.Pod.ID}) {
				t.Fatalf("member %d has no independent current owner at cell %d", i, cell)
			}
		}
	}
	plan, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	if plan.DrainageProved {
		t.Fatal("inactive certificate claimed a drainage motion proof")
	}
}

func TestCouplingReservationCompleteRawUnion(t *testing.T) {
	t.Parallel()
	for _, occupied := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "passenger"}[occupied], func(t *testing.T) {
			t.Parallel()
			input := couplingReservationFixture(t, occupied)
			before := cloneCouplingInput(input)
			plan, err := planCouplingReservation(input)
			if err != nil {
				t.Fatal(err)
			}
			want := make(map[resource]bool)
			for _, id := range []string{"ab", "bc"} {
				cells := input.Prepared.laneCells[id]
				for cell := range cells.count() {
					for _, r := range cells.cell(cell) {
						want[r] = true
					}
				}
			}
			for _, id := range []string{"goal-front-in", "goal-rear-in"} {
				cells := input.Prepared.laneCells[id]
				for _, r := range cells.cell(0) {
					want[r] = true
				}
			}
			want[resource{kind: couplingSiteResourceKind(), id: "assembly"}] = true
			want[resource{kind: couplingSiteResourceKind(), id: "split"}] = true
			if len(plan.Claims) != len(plan.Dependencies) {
				t.Fatal("claim and dependency counts differ")
			}
			for i, claim := range plan.Claims {
				if claim.Resource != plan.Dependencies[i].Resource {
					t.Fatalf("claim/dependency key mismatch at %d: %+v / %+v", i, claim.Resource, plan.Dependencies[i].Resource)
				}
			}
			if len(plan.Claims) != len(want) {
				t.Fatalf("claims=%d want raw union=%d", len(plan.Claims), len(want))
			}
			for _, claim := range plan.Claims {
				if !want[claim.Resource] {
					t.Fatalf("unexpected claim %+v", claim)
				}
			}
			if len(plan.PreservedClaims) != 4 {
				t.Fatal("distinct existing receiving berth and node claims were not preserved")
			}
			for _, claim := range plan.PreservedClaims {
				if input.Owners[claim.Resource] != claim.Expected || claim.Expected.kind != podOwnerKind {
					t.Fatal("receiving claim lost its exact individual pod tag")
				}
			}
			if !couplingInputsEqual(before, input) {
				t.Fatal("planner changed its input")
			}
			claims, err := revalidateCouplingReservation(plan, input)
			if err != nil {
				t.Fatal(err)
			}
			claims[0].Expected = resourceOwner{kind: groupOwnerKind, id: "changed"}
			if reflect.DeepEqual(claims, plan.Claims) {
				t.Fatal("write set aliases stored plan")
			}
			input.Members[0].Vehicle.Route[0].SpeedLimit = 1
			input.Members[0].Retained[resource{kind: trackResource, id: "ab", cell: 2}] = 0
			if occupied {
				input.Members[0].Vehicle.Riders[0].SharingConsent = SharedConsent
			}
			if reflect.DeepEqual(input.Members, plan.members) {
				t.Fatal("plan aliases mutable member facts")
			}
		})
	}
}

func cloneCouplingInput(input couplingReservationInput) couplingReservationInput {
	input.Owners = maps.Clone(input.Owners)
	input.Waiting = slices.Clone(input.Waiting)
	for i := range input.Members {
		input.Members[i] = cloneCouplingMember(input.Members[i])
	}
	return input
}

func TestCouplingReservationDenialsDoNotMutate(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*couplingReservationInput)
	}{
		{name: "mixed occupancy", change: func(input *couplingReservationInput) {
			input.Members[1].Vehicle.Riders = nil
			input.Members[1].Vehicle.Stops = nil
			input.Members[1].Vehicle.Pod.Occupied = false
			input.Members[1].Vehicle.Pod.Activity = Traveling
			input.Members[1].Vehicle.RelocatingTo = "goal"
		}},
		{name: "large class", change: func(input *couplingReservationInput) { input.Members[0].Vehicle.Pod.Class = GroupClass }},
		{name: "virtual link", change: func(input *couplingReservationInput) { input.Members[0].VirtualLeader = true }},
		{name: "queue certificate", change: func(input *couplingReservationInput) { input.Members[0].CompactQueue = true }},
		{name: "pending pickup", change: func(input *couplingReservationInput) { input.Waiting = []Request{{ID: 3, PodID: "front"}} }},
		{name: "capacity cannot pool", change: func(input *couplingReservationInput) { input.Members[0].Vehicle.Riders[0].PartySize = 5 }},
		{name: "staging speed", change: func(input *couplingReservationInput) { input.Members[0].Vehicle.Pod.Speed = .1 }},
		{name: "staging coordinate", change: func(input *couplingReservationInput) { input.Members[1].Distance -= .001 }},
		{name: "impossible current tag", change: func(input *couplingReservationInput) {
			input.Owners[resource{kind: trackResource, id: "ab", cell: 1}] = resourceOwner{kind: groupOwnerKind, id: "rear"}
		}},
		{name: "missing current grant", change: func(input *couplingReservationInput) { input.Members[1].ReservedThrough = -1 }},
		{name: "missing retention", change: func(input *couplingReservationInput) { clear(input.Members[0].Retained) }},
		{name: "invalid chronology", change: func(input *couplingReservationInput) { input.Members[0].Vehicle.Riders[0].BoardedTick = input.Tick + 1 }},
		{name: "boarding origin", change: func(input *couplingReservationInput) {
			input.Members[0].Vehicle.Riders[0].SharingConsent = SharedConsent
			input.Members[0].Vehicle.Boardings = []RiderBoarding{{BerthID: "unknown"}}
		}},
		{name: "receiving claim lost", change: func(input *couplingReservationInput) {
			delete(input.Owners, resource{kind: berthResource, id: "goal-front-1"})
		}},
		{name: "unproved individual claim", change: func(input *couplingReservationInput) {
			input.Owners[resource{kind: trackResource, id: "unknown", cell: 0}] = resourceOwner{kind: podOwnerKind, id: "front"}
		}},
		{name: "missing stop", change: func(input *couplingReservationInput) { input.Members[0].Vehicle.Stops = nil }},
		{name: "duplicate party identity", change: func(input *couplingReservationInput) { input.Members[1].Vehicle.Riders[0].ID = 1 }},
		{name: "no receiving intent", change: func(input *couplingReservationInput) { input.Members[0].DestinationStation = "unknown" }},
		{name: "route changed at same ID", change: func(input *couplingReservationInput) { input.Members[0].Vehicle.Route[1].SpeedLimit = 1 }},
		{name: "route lacks split", change: func(input *couplingReservationInput) {
			input.Members[0].Vehicle.Route = input.Members[0].Vehicle.Route[:1]
		}},
		{name: "late foreign exit", change: func(input *couplingReservationInput) {
			input.Owners[resource{kind: trackResource, id: "bc", cell: 6}] = resourceOwner{kind: podOwnerKind, id: "foreign"}
		}},
		{name: "same ID group exit", change: func(input *couplingReservationInput) {
			input.Owners[resource{kind: trackResource, id: "bc", cell: 6}] = resourceOwner{kind: groupOwnerKind, id: "front"}
		}},
		{name: "unknown exit owner", change: func(input *couplingReservationInput) {
			input.Owners[resource{kind: trackResource, id: "bc", cell: 6}] = resourceOwner{kind: 255, id: "front"}
		}},
		{name: "site already held", change: func(input *couplingReservationInput) {
			input.Owners[resource{kind: couplingSiteResourceKind(), id: "split"}] = resourceOwner{kind: groupOwnerKind, id: "front"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := couplingReservationFixture(t, true)
			test.change(&input)
			before := cloneCouplingInput(input)
			_, err := planCouplingReservation(input)
			if err == nil {
				t.Fatal("invalid formation candidate passed")
			}
			if !couplingInputsEqual(before, input) {
				t.Fatal("denial changed owner, route, party, or member facts")
			}
		})
	}
}

func TestCouplingReservationStaleAndForgedPlan(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*couplingReservationInput, *couplingReservationPlan)
	}{
		{name: "route version", change: func(input *couplingReservationInput, _ *couplingReservationPlan) { input.Members[0].RouteVersion++ }},
		{name: "saturated route identity", change: func(input *couplingReservationInput, _ *couplingReservationPlan) {
			input.Members[0].Vehicle.Route[1].SpeedLimit = 1
		}},
		{name: "consent", change: func(input *couplingReservationInput, _ *couplingReservationPlan) {
			input.Members[0].Vehicle.Riders[0].SharingConsent = SharedConsent
		}},
		{name: "bound pickup", change: func(input *couplingReservationInput, _ *couplingReservationPlan) {
			input.Waiting = []Request{{ID: 3, PodID: "front"}}
		}},
		{name: "current grant", change: func(input *couplingReservationInput, _ *couplingReservationPlan) { input.Members[0].ReservedThrough++ }},
		{name: "position", change: func(input *couplingReservationInput, _ *couplingReservationPlan) {
			input.Members[0].Vehicle.Pod.Position.Y = .01
		}},
		{name: "forged missing exit", change: func(_ *couplingReservationInput, plan *couplingReservationPlan) {
			plan.Claims = plan.Claims[:len(plan.Claims)-1]
		}},
		{name: "owner changed", change: func(input *couplingReservationInput, _ *couplingReservationPlan) {
			input.Owners[resource{kind: trackResource, id: "bc", cell: 6}] = resourceOwner{kind: groupOwnerKind, id: "front"}
		}},
		{name: "prepared identity", change: func(input *couplingReservationInput, _ *couplingReservationPlan) {
			preparedCopy := *input.Prepared
			input.Prepared = &preparedCopy
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := couplingReservationFixture(t, true)
			input.Members[0].RouteVersion = ^uint64(0)
			plan, err := planCouplingReservation(input)
			if err != nil {
				t.Fatal(err)
			}
			test.change(&input, &plan)
			before := cloneCouplingInput(input)
			if _, err := revalidateCouplingReservation(plan, input); err == nil {
				t.Fatal("stale or forged certificate passed")
			}
			if !couplingInputsEqual(before, input) {
				t.Fatal("revalidation denial changed its input")
			}
		})
	}
}

func TestCouplingReservationBodyAndRoomOracle(t *testing.T) {
	t.Parallel()
	input := couplingReservationFixture(t, false)
	plan, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	room, err := CouplingSiteRoom(CompactPairV1CouplingContract)
	if err != nil {
		t.Fatal(err)
	}
	stop := .5*.5/(2*.5) + .5/60
	margin := 2 + 12 + stop
	if math.Abs(room.RequiredLengthMeters-(12+7.5+2*margin)) > 1e-12 {
		t.Fatal("room differs from independent approved arithmetic")
	}
	for _, id := range []string{"assembly", "split"} {
		site := input.Network.sites[id]
		for _, spacing := range []float64{4.5, 8, 12} {
			for _, front := range []float64{site.FrontStagingMeters, site.FrontStagingMeters + 7.5} {
				bodyRear, bodyFront := front-spacing-2, front+2
				if bodyRear-12-stop < site.StartMeters || bodyFront+12+stop > site.EndMeters {
					t.Fatalf("independent maneuver body leaves protected site %s", id)
				}
				if front-spacing+2 > front-2 {
					t.Fatal("independent cabins penetrate")
				}
				connectorStart, connectorEnd := front-spacing+2, front-2
				if connectorStart < bodyRear || connectorEnd > bodyFront || .3/2 > 2.0/2 {
					t.Fatal("independent connector leaves conservative body bounds")
				}
			}
		}
	}
	if !plan.Exits[0].SplitCleared || !plan.Exits[1].SplitCleared {
		t.Fatal("roomy fixture did not provide both actual exit holds")
	}
	if math.Abs(plan.Exits[0].Distance-plan.Exits[1].Distance-12) > 1e-9 {
		t.Fatal("exit holding targets lack ordinary gap")
	}
}

func TestCouplingReservationErrorsAreTyped(t *testing.T) {
	t.Parallel()
	if _, err := planCouplingReservation(couplingReservationInput{}); !errors.Is(err, errCouplingReservationDenied) {
		t.Fatal("missing plan input lost denial identity")
	}
}

func TestCouplingSharedCabinFactsStayIndependent(t *testing.T) {
	t.Parallel()
	input := couplingReservationFixture(t, true)
	for i := range input.Members {
		v := &input.Members[i].Vehicle
		v.Riders[0].PartySize = 2
		v.Riders[0].SharingConsent = SharedConsent
		other := v.Riders[0]
		other.ID = i + 3
		other.PartySize = 1
		v.Riders = append(v.Riders, other)
		v.Boardings = []RiderBoarding{{BerthID: "origin-1"}, {BerthID: "origin-1"}}
		v.RiddenMeters = input.Members[i].Distance
	}
	before := cloneCouplingInput(input)
	plan, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	for i := range plan.members {
		if plan.members[i].Vehicle.PassengersAboard() != 3 || !reflect.DeepEqual(plan.members[i].Vehicle.Riders, input.Members[i].Vehicle.Riders) || !reflect.DeepEqual(plan.members[i].Vehicle.Boardings, input.Members[i].Vehicle.Boardings) {
			t.Fatal("connecting cabins changed whole party or boarding facts")
		}
	}
	if !couplingInputsEqual(before, input) {
		t.Fatal("shared cabin preparation changed input")
	}
}

func TestCouplingStopBoundaryAfterSplit(t *testing.T) {
	t.Parallel()
	input := couplingReservationFixture(t, true)
	plan, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	member := plan.members[0]
	blocks := &plan.routes[0]
	if err := plan.stopsAfterSplit(member, blocks, 300); err != nil {
		t.Fatal("actual goal approach after opening rejected", err)
	}
	if err := plan.stopsAfterSplit(member, blocks, 400); err == nil {
		t.Fatal("stop at the opening boundary passed")
	}
}

func couplingInputsEqual(a, b couplingReservationInput) bool {
	return a.Network == b.Network && a.Prepared == b.Prepared && a.CorridorID == b.CorridorID && a.OrderContract == b.OrderContract && a.Tick == b.Tick && reflect.DeepEqual(a.Members, b.Members) && reflect.DeepEqual(a.Waiting, b.Waiting) && maps.Equal(a.Owners, b.Owners)
}
