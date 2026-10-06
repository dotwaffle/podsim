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

// This hook proves an owned stop. It does not plan or publish a motion step.
func couplingOwnedFrontier(input couplingFrontierInput) ([2]float64, error) {
	var frontiers [2]float64
	if input.Plan.network == nil || input.Owner.id == "" || (input.Owner.kind != podOwnerKind && input.Owner.kind != groupOwnerKind) {
		return frontiers, couplingDenied("invalid proposed frontier owner")
	}
	if len(input.Plan.Claims) == 0 || len(input.Plan.Claims) != len(input.Plan.Dependencies) {
		return frontiers, couplingDenied("invalid frontier certificate")
	}
	for i, claim := range input.Plan.Claims {
		if claim.Resource != input.Plan.Dependencies[i].Resource {
			return frontiers, couplingDenied("frontier dependency keys differ")
		}
	}
	for i, route := range input.Plan.routes {
		if len(route.route) == 0 || len(route.lanes) != len(route.route)+1 || input.Plan.Exits[i].Through < 0 || input.Plan.Exits[i].Through >= route.len() {
			return frontiers, couplingDenied("invalid member frontier route")
		}
	}
	for _, dependency := range input.Plan.Dependencies {
		if input.Owners[dependency.Resource] == input.Owner {
			continue
		}
		clearance := couplingClearanceInput{Dependency: dependency, Phase: input.Phase, Distances: input.Distances, FrontAxis: input.Distances[0] - input.Plan.axisOrigins[0], SitesCleared: input.SitesCleared[dependency.Resource]}
		if !couplingDependencyCleared(clearance) {
			return frontiers, couplingDenied("unresolved frontier dependency lacks an owner")
		}
	}
	profile, ok := LookupCouplingProfile(input.Plan.network.contract)
	if !ok {
		return frontiers, couplingDenied("invalid frontier model")
	}
	if err := couplingPhasePositions(input, profile); err != nil {
		return frontiers, err
	}
	braking, speedCap, err := couplingPhaseBounds(input.Phase, profile, input.Speeds)
	if err != nil {
		return frontiers, err
	}
	for i := range input.Distances {
		blocks := input.Plan.routes[i]
		distance, speed := input.Distances[i], input.Speeds[i]
		if !finite(distance) || !finite(speed) || speed < 0 || speed > speedCap || distance < input.Plan.members[i].Distance {
			return frontiers, couplingDenied("invalid current phase motion facts")
		}
		through := input.Plan.Exits[i].Through
		if through < 0 || through >= blocks.len() {
			return frontiers, couplingDenied("invalid exit grant bounds")
		}
		limit := blocks.at(through).end
		switch input.Phase {
		case couplingClosing, couplingLatching:
			limit = min(limit, input.Plan.ClosingStops[i])
		case couplingConnected, couplingUnlatching:
			limit = min(limit, input.Plan.SplitStops[i])
		case couplingOpening:
			limit = min(limit, input.Plan.OpeningStops[i])
		case couplingDraining:
			// Draining uses the ordinary actual-exit grant frontier.
		}
		frontiers[i] = limit
		stop := speed*speed/(2*braking) + speed/TicksPerSecond
		if distance+stop > limit+conflictSlack {
			return frontiers, couplingDenied("owned frontier is shorter than discrete stopping room")
		}
		if err := couplingLaneSpeedProof(&blocks, distance, speed, braking); err != nil {
			return frontiers, err
		}
	}
	return frontiers, nil
}

func couplingPhaseBounds(phase couplingReservationPhase, profile CouplingProfile, speeds [2]float64) (float64, float64, error) {
	switch phase {
	case couplingClosing:
		if speeds[0] != 0 {
			return 0, 0, couplingDenied("closing head is not stopped")
		}
		return profile.ManeuverBraking, profile.ManeuverSpeed, nil
	case couplingOpening:
		if speeds[1] != 0 {
			return 0, 0, couplingDenied("opening rear is not stopped")
		}
		return profile.ManeuverBraking, profile.ManeuverSpeed, nil
	case couplingLatching, couplingUnlatching:
		if speeds != ([2]float64{}) {
			return 0, 0, couplingDenied("dwell members are not stopped")
		}
		return profile.ManeuverBraking, 0, nil
	case couplingConnected:
		if speeds[0] != speeds[1] {
			return 0, 0, couplingDenied("connected speeds differ")
		}
		return profile.Braking, math.Inf(1), nil
	case couplingDraining:
		return profile.Braking, math.Inf(1), nil
	}
	return 0, 0, couplingDenied("unknown reservation phase")
}

func couplingLaneSpeedProof(blocks *blockList, distance, speed, braking float64) error {
	current := -1
	for i := range blocks.route {
		if distance <= blocks.lanes[i].start+blocks.lanes[i].length {
			current = i
			break
		}
	}
	if current < 0 || speed > blocks.route[current].SpeedLimit {
		return couplingDenied("current member lane speed exceeded")
	}
	reach := speed*speed/(2*braking) + speed/TicksPerSecond
	for i := current + 1; i < len(blocks.route); i++ {
		remaining := blocks.lanes[i].start - distance
		if remaining > reach {
			break
		}
		limit := blocks.route[i].SpeedLimit
		if limit < speed && speed*speed/(2*braking)+speed/TicksPerSecond > remaining+limit*limit/(2*braking)+conflictSlack {
			return couplingDenied("lower-speed lane lies inside unproved braking reach")
		}
	}
	return nil
}

type couplingClearanceInput struct {
	Dependency   couplingDependency
	Phase        couplingReservationPhase
	Distances    [2]float64
	FrontAxis    float64
	SitesCleared bool
}

// Dependencies remain joint until both local and connector bounds pass.
func couplingDependencyCleared(input couplingClearanceInput) bool {
	if input.Phase < couplingClosing || input.Phase > couplingDraining || !finite(input.FrontAxis) {
		return false
	}
	for i, used := range input.Dependency.MemberUse {
		if !finite(input.Distances[i]) || !finite(input.Dependency.MemberRelease[i]) || used && input.Distances[i] < input.Dependency.MemberRelease[i] {
			return false
		}
	}
	if input.Dependency.Site {
		return input.Dependency.NotBefore != 0 && input.Phase >= input.Dependency.NotBefore && input.SitesCleared
	}
	if input.Dependency.AxisUse && input.Phase != couplingDraining {
		spacing := Clearance
		if input.Phase == couplingConnected || input.Phase == couplingUnlatching {
			profile, _ := LookupCouplingProfile(CompactPairV1CouplingContract)
			spacing = profile.CenterSpacingMeters
		}
		if !finite(input.Dependency.AxisRelease) {
			return false
		}
		if input.FrontAxis < input.Dependency.AxisRelease+spacing {
			return false
		}
	}
	return true
}
