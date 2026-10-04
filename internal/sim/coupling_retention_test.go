package sim

import (
	"maps"
	"math"
	"testing"
)

func TestCouplingOwnedFrontierOracle(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		change    func(*couplingFrontierInput)
		wantError bool
	}{
		{name: "connected stop"},
		{name: "rear current lower limit", wantError: true, change: func(input *couplingFrontierInput) {
			input.Distances = [2]float64{204, 199.5}
			input.Speeds = [2]float64{8, 8}
		}},
		{name: "rear remains on prior lane", wantError: true, change: func(input *couplingFrontierInput) {
			input.Distances = [2]float64{204, 199.5}
			input.Speeds = [2]float64{7.01, 7.01}
		}},
		{name: "insufficient discrete room", wantError: true, change: func(input *couplingFrontierInput) {
			input.Phase = couplingDraining
			end := input.Plan.routes[0].at(input.Plan.Exits[0].Through).end
			input.Distances = [2]float64{end - 1.5, end - 13.5}
			input.Speeds = [2]float64{3, 3}
		}},
		{name: "missing far exit claim", wantError: true, change: func(input *couplingFrontierInput) {
			delete(input.Owners, resource{kind: trackResource, id: "bc", cell: 6})
		}},
		{name: "site flag cannot bypass body bounds", wantError: true, change: func(input *couplingFrontierInput) {
			r := resource{kind: couplingSiteResourceKind(), id: "assembly"}
			delete(input.Owners, r)
			input.SitesCleared = map[resource]bool{r: true}
		}},
		{name: "kind collision", wantError: true, change: func(input *couplingFrontierInput) {
			input.Owners[resource{kind: trackResource, id: "bc", cell: 6}] = resourceOwner{kind: podOwnerKind, id: "group"}
		}},
		{name: "different common speed", wantError: true, change: func(input *couplingFrontierInput) { input.Speeds[1] = .4 }},
		{name: "closing head moves", wantError: true, change: func(input *couplingFrontierInput) { input.Phase = couplingClosing }},
		{name: "opening rear moves", wantError: true, change: func(input *couplingFrontierInput) { input.Phase = couplingOpening }},
		{name: "moving dwell", wantError: true, change: func(input *couplingFrontierInput) { input.Phase = couplingLatching }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			formation := couplingReservationFixture(t, false)
			plan, err := planCouplingReservation(formation)
			if err != nil {
				t.Fatal(err)
			}
			owner := resourceOwner{kind: groupOwnerKind, id: "group"}
			input := couplingFrontierInput{Plan: plan, Phase: couplingConnected, Owner: owner, Speeds: [2]float64{.5, .5},
				Distances: [2]float64{100, 95.5}, Owners: make(map[resource]resourceOwner)}
			for _, claim := range plan.Claims {
				input.Owners[claim.Resource] = owner
			}
			if test.change != nil {
				test.change(&input)
			}
			before := maps.Clone(input.Owners)
			frontiers, err := couplingOwnedFrontier(input)
			if (err != nil) != test.wantError {
				t.Fatalf("frontier error=%v want error %v", err, test.wantError)
			}
			if !maps.Equal(before, input.Owners) {
				t.Fatal("frontier changed owner facts")
			}
			if err == nil {
				for i, speed := range input.Speeds {
					required := speed*speed/4 + speed/60
					if input.Distances[i]+required > frontiers[i] {
						t.Fatal("independent discrete stop leaves claimed frontier")
					}
				}
			}
		})
	}
}

func TestCouplingRearLaneLimit(t *testing.T) {
	t.Parallel()
	formation := couplingReservationFixture(t, false)
	plan, err := planCouplingReservation(formation)
	if err != nil {
		t.Fatal(err)
	}
	// Change a detached test route only. No prepared or public lane changes.
	for i := range plan.routes {
		plan.routes[i].route = cloneLanes(plan.routes[i].route)
		plan.routes[i].route[0].SpeedLimit = 3
		plan.routes[i].route[1].SpeedLimit = 14
	}
	owner := resourceOwner{kind: groupOwnerKind, id: "pair"}
	input := couplingFrontierInput{Plan: plan, Phase: couplingConnected, Distances: [2]float64{203, 198.5}, Speeds: [2]float64{4, 4}, Owner: owner, Owners: make(map[resource]resourceOwner)}
	for _, claim := range plan.Claims {
		input.Owners[claim.Resource] = owner
	}
	if _, err := couplingOwnedFrontier(input); err == nil {
		t.Fatal("front-only current lane limit admitted an overspeed rear")
	}
	input.Speeds = [2]float64{3, 3}
	if _, err := couplingOwnedFrontier(input); err != nil {
		t.Fatal(err)
	}
}

func TestCouplingRepeatedRearAndOpeningRetention(t *testing.T) {
	t.Parallel()
	r := resource{kind: junctionResource, id: "shared"}
	dependencies := make(map[resource]couplingDependency)
	addCouplingDependency(dependencies, r, 0, 100, 0, "ab", []string{"ab"})
	addCouplingDependency(dependencies, r, 0, 200, 0, "ab", []string{"ab"})
	addCouplingDependency(dependencies, r, 1, 150, 0, "ab", []string{"ab"})
	dep := dependencies[r]
	if dep.MemberRelease != ([2]float64{200, 150}) || dep.AxisRelease != 200 {
		t.Fatal("repeated resource lost its longest use")
	}
	for _, test := range []struct {
		name  string
		input couplingClearanceInput
		want  bool
	}{
		{name: "front clear rear occupied", input: couplingClearanceInput{Phase: couplingConnected, Distances: [2]float64{205, 149}, FrontAxis: 205}},
		{name: "connector retained", input: couplingClearanceInput{Phase: couplingConnected, Distances: [2]float64{201, 151}, FrontAxis: 201}},
		{name: "connected clears", input: couplingClearanceInput{Phase: couplingConnected, Distances: [2]float64{205, 151}, FrontAxis: 205}, want: true},
		{name: "opening uses ordinary spacing", input: couplingClearanceInput{Phase: couplingOpening, Distances: [2]float64{205, 151}, FrontAxis: 205}},
		{name: "opening clears at twelve", input: couplingClearanceInput{Phase: couplingOpening, Distances: [2]float64{212, 151}, FrontAxis: 212}, want: true},
		{name: "draining uses local routes", input: couplingClearanceInput{Phase: couplingDraining, Distances: [2]float64{200, 150}}, want: true},
		{name: "nonfinite coordinate", input: couplingClearanceInput{Phase: couplingConnected, Distances: [2]float64{205, math.NaN()}, FrontAxis: 205}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := test.input
			input.Dependency = dep
			if got := couplingDependencyCleared(input); got != test.want {
				t.Fatalf("clear=%v want %v", got, test.want)
			}
		})
	}
}

func TestCouplingSiteRetainsIncompleteDrainage(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		phase   couplingReservationPhase
		cleared bool
		want    bool
	}{
		{name: "connected", phase: couplingConnected, cleared: true},
		{name: "opening", phase: couplingOpening, cleared: true},
		{name: "blocked draining", phase: couplingDraining},
		{name: "both tails cleared", phase: couplingDraining, cleared: true, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := couplingClearanceInput{Dependency: couplingDependency{Site: true, NotBefore: couplingDraining, MemberUse: [2]bool{true, true}, MemberRelease: [2]float64{100, 100}}, Distances: [2]float64{100, 100}, Phase: test.phase, SitesCleared: test.cleared}
			if got := couplingDependencyCleared(input); got != test.want {
				t.Fatalf("site clear=%v want %v", got, test.want)
			}
		})
	}
}

func TestCouplingOwnedFrontierAllowsClearedPastClaims(t *testing.T) {
	t.Parallel()
	formation := couplingReservationFixture(t, false)
	plan, err := planCouplingReservation(formation)
	if err != nil {
		t.Fatal(err)
	}
	owner := resourceOwner{kind: groupOwnerKind, id: "pair"}
	input := couplingFrontierInput{Plan: plan, Phase: couplingConnected, Distances: [2]float64{100, 95.5}, Speeds: [2]float64{.5, .5}, Owner: owner, Owners: make(map[resource]resourceOwner)}
	for _, claim := range plan.Claims {
		input.Owners[claim.Resource] = owner
	}
	delete(input.Owners, resource{kind: nodeResource, id: "a"})
	delete(input.Owners, resource{kind: trackResource, id: "ab", cell: 0})
	if _, err := couplingOwnedFrontier(input); err != nil {
		t.Fatal("cleared past claims blocked current owned stop", err)
	}
	delete(input.Owners, resource{kind: trackResource, id: "bc", cell: 6})
	if _, err := couplingOwnedFrontier(input); err == nil {
		t.Fatal("future exit claim disappeared before its use")
	}
}

func TestCouplingSiteFlagCannotClearEitherBodyEarly(t *testing.T) {
	t.Parallel()
	for _, distances := range [][2]float64{{99, 100}, {100, 99}} {
		input := couplingClearanceInput{Dependency: couplingDependency{Site: true, NotBefore: couplingDraining, MemberUse: [2]bool{true, true}, MemberRelease: [2]float64{100, 100}}, Distances: distances, FrontAxis: 100, Phase: couplingDraining, SitesCleared: true}
		if couplingDependencyCleared(input) {
			t.Fatal("site flag bypassed an unresolved member body bound")
		}
	}
}
