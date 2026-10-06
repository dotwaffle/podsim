package sim

import (
	"cmp"
	"errors"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"
)

// couplingFormationSpec places a stopped pair on the network of
// couplingMotionFixtureWith. A zero field keeps the fixture value.
type couplingFormationSpec struct {
	edit func(*Network)
	// rearStage is the rear staging coordinate on the assembly lane.
	rearStage float64
	// spacing is the front staging coordinate minus rearStage.
	spacing float64
	// prefix holds the lanes before the assembly lane on each route.
	prefix [2][]string
	// ulps moves each member that many steps of one ulp from its staging
	// point, inside the staging tolerance.
	ulps [2]int
}

// couplingFormationFixture gives the stopped pair of spec with the
// ordinary footprint of each member: each resource of a cell up to its
// current cell that it has not passed.
func couplingFormationFixture(t *testing.T, spec couplingFormationSpec) couplingReservationInput {
	t.Helper()
	base := couplingMotionFixtureWith(t, false, false, spec.edit)
	p := base.Prepared
	sites := []CouplingSite{base.Network.sites["assembly"], base.Network.sites["split"]}
	sites[0].RearStagingMeters = cmp.Or(spec.rearStage, sites[0].RearStagingMeters)
	sites[0].FrontStagingMeters = sites[0].RearStagingMeters + cmp.Or(spec.spacing, Clearance)
	sites[0].EndMeters = max(sites[0].EndMeters, sites[0].FrontStagingMeters+30)
	n, err := prepareCouplingReservations(p, CouplingGeometryInput{Contract: CompactPairV1CouplingContract, Network: p.Network(),
		Sites: sites, Corridors: []CouplingCorridor{base.Network.corridors["corridor"]}})
	if err != nil {
		t.Fatal(err)
	}
	input := base
	input.Network, input.Owners = n, make(map[resource]resourceOwner)
	for i := range input.Members {
		m := cloneCouplingMember(base.Members[i])
		m.Retained = make(map[resource]float64)
		var route []Lane
		for _, id := range spec.prefix[i] {
			route = append(route, n.lanes[id].lane)
		}
		m.Vehicle.Route = append(route, m.Vehicle.Route...)
		blocks, err := n.routeBlocks(m.Vehicle.Route)
		if err != nil {
			t.Fatal(err)
		}
		origin := blocks.lanes[len(spec.prefix[i])].start
		stage := sites[0].FrontStagingMeters
		if i == 1 {
			stage = sites[0].RearStagingMeters
		}
		m.Distance = origin + stage
		for range max(spec.ulps[i], -spec.ulps[i]) {
			m.Distance = math.Nextafter(m.Distance, float64(spec.ulps[i])*math.Inf(1))
		}
		m.Vehicle.Pod.LaneDistance = m.Distance - origin
		m.Vehicle.Pod.Position = couplingOffset(n.lanes["ab"].from, n.lanes["ab"].direction, m.Vehicle.Pod.LaneDistance)
		m.BlockIndex = 0
		for blocks.at(m.BlockIndex).end < m.Distance {
			m.BlockIndex++
		}
		m.ReservedThrough = m.BlockIndex
		for _, b := range blocks.span(0, m.BlockIndex+1) {
			for _, r := range b.resources {
				release := resourceReleaseDistance(b, r)
				if release <= m.Distance {
					continue
				}
				if owner := input.Owners[r]; !owner.isZero() && !owner.isPod(m.Vehicle.Pod.ID) {
					t.Fatal("staged footprints overlap")
				}
				input.Owners[r] = podResourceOwner(m.Vehicle.Pod.ID)
				m.Retained[r] = max(m.Retained[r], release)
			}
		}
		for _, r := range berthResources(m.Destination) {
			input.Owners[r] = podResourceOwner(m.Vehicle.Pod.ID)
		}
		input.Members[i] = m
	}
	return input
}

// couplingFormationNeeds returns each resource that the train needs at
// formation. It reads the route cells of each member through the given
// cell: a member has not passed one occurrence, or the front is less than
// Clearance past the axis release of a corridor occurrence. Sites are
// always needed.
func couplingFormationNeeds(p *couplingReservationPlan, through [2]int) map[resource]bool {
	corridor := p.network.corridors[p.corridorID]
	front := p.members[0].Distance - p.axisOrigins[0]
	needs := make(map[resource]bool)
	for i := range p.routes {
		for _, b := range p.routes[i].span(0, through[i]+1) {
			for _, r := range b.resources {
				release := resourceReleaseDistance(b, r)
				if release > p.members[i].Distance || slices.Contains(corridor.LaneIDs, b.lane.ID) && release-p.axisOrigins[i]+Clearance > front {
					needs[r] = true
				}
			}
		}
	}
	needs[resource{kind: couplingSiteResourceKind(), id: corridor.AssemblySiteID}] = true
	needs[resource{kind: couplingSiteResourceKind(), id: corridor.SplitSiteID}] = true
	return needs
}

// couplingFormationAdopt runs the plan, the motion context, and the
// initial motion of input, as adoption does.
func couplingFormationAdopt(input couplingReservationInput) (*couplingMotionContext, couplingMotionStep, error) {
	plan, err := planCouplingReservation(input)
	if err != nil {
		return nil, couplingMotionStep{}, err
	}
	c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: plan, Current: input, GroupID: "pair"})
	if err != nil {
		return nil, couplingMotionStep{}, err
	}
	step, err := initialCouplingMotion(c, input)
	return c, step, err
}

// The released resources of the fixture are those of cell 0 of the
// assembly lane: node a at 12 m, the junction of a at 30 m, and the track
// at 42 m. The rear is at 60 m and the front at 72 m.
func TestCouplingFormationForeignOwner(t *testing.T) {
	t.Parallel()
	foreign, other := podResourceOwner("third"), podResourceOwner("fourth")
	for _, test := range []struct {
		name  string
		r     resource
		owner resourceOwner
		want  string
	}{
		{"node of the first cell", resource{kind: nodeResource, id: "a"}, foreign, ""},
		{"junction of the first cell", resource{kind: junctionResource, id: "a"}, foreign, ""},
		{"track of the first cell", resource{kind: trackResource, id: "ab"}, foreign, ""},
		{"foreign group", resource{kind: trackResource, id: "ab"}, resourceOwner{kind: groupOwnerKind, id: "other"}, ""},
		{"first needed track ahead", resource{kind: trackResource, id: "ab", cell: 3}, foreign, "foreign owner at resource {kind:2 id:ab cell:3}"},
		{"rear on a released track", resource{kind: trackResource, id: "ab"}, podResourceOwner("rear"), "member owns resource {kind:2 id:ab cell:0} that formation releases"},
		{"front on a released node", resource{kind: nodeResource, id: "a"}, podResourceOwner("front"), "member owns resource {kind:1 id:a cell:0} that formation releases"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := couplingMotionFixture(t, false, false)
			input.Owners[test.r] = test.owner
			before := cloneCouplingInput(input)
			c, step, err := couplingFormationAdopt(input)
			if !couplingInputsEqual(before, input) {
				t.Fatal("formation changed its input")
			}
			if test.want != "" {
				if !errors.Is(err, errCouplingReservationDenied) || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("got %v, want refusal %q", err, test.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			index := slices.IndexFunc(c.claims, func(claim couplingClaim) bool { return claim.Resource == test.r })
			if index < 0 || !c.claims[index].Expected.isZero() || !c.dependencyOwner(c.dependencies[index], step.State).isZero() {
				t.Fatal("released resource lost its claim entry or has an expected owner")
			}
			if slices.ContainsFunc(step.Writes, func(w couplingOwnerWrite) bool { return w.Resource == test.r }) {
				t.Fatal("formation wrote a resource that it releases")
			}
			owners := maps.Clone(input.Owners)
			applyCouplingTestWrites(t, owners, step.Writes)
			if owners[test.r] != test.owner {
				t.Fatal("formation changed the foreign owner")
			}
			// A change of the foreign owner after the plan keeps the certificate.
			plan, err := planCouplingReservation(input)
			if err != nil {
				t.Fatal(err)
			}
			for _, owner := range []resourceOwner{other, {}} {
				changed := cloneCouplingInput(input)
				changed.Owners[test.r] = owner
				if _, err := revalidateCouplingReservation(plan, changed); err != nil {
					t.Fatalf("owner %+v changed the certificate: %v", owner, err)
				}
				c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: plan, Current: changed, GroupID: "pair"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := initialCouplingMotion(c, changed); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// A resource that the plan releases can occur again in the exit closure,
// which then raises its release threshold. The exit closure must check
// the owner with the complete threshold. No authored straight corridor
// reaches its first cell again within the closure, so the test adds the
// first track cell of the assembly lane to the closure cell of the rear.
func TestCouplingFormationExitReneedsReleasedResource(t *testing.T) {
	t.Parallel()
	r := resource{kind: trackResource, id: "ab"}
	for _, test := range []struct {
		name   string
		inject bool
		owner  resourceOwner
		want   string
	}{
		{"released", false, podResourceOwner("third"), ""},
		{"needed by the exit, free", true, resourceOwner{}, ""},
		{"needed by the exit, foreign", true, podResourceOwner("third"), "complete closure has a foreign typed owner"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := couplingMotionFixture(t, false, false)
			plan, err := planCouplingReservation(input)
			if err != nil {
				t.Fatal(err)
			}
			if test.inject {
				c, prepareErr := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: plan, Current: input, GroupID: "pair"})
				if prepareErr != nil {
					t.Fatal(prepareErr)
				}
				// The closure cell of the rear past the plan exit.
				b := c.reservation.routes[1].at(c.through[1])
				if c.through[1] <= plan.Exits[1].Through || b.cell != 1 {
					t.Fatal("fixture has no closure cell past the plan exit")
				}
				cells := *input.Prepared.laneCells[b.lane.ID]
				cells.resources = slices.Clone(cells.resources)
				cells.ends = slices.Clone(cells.ends)
				cells.resources = slices.Insert(cells.resources, cells.ends[b.cell], r)
				for k := b.cell; k < len(cells.ends); k++ {
					cells.ends[k]++
				}
				input.Prepared.laneCells[b.lane.ID] = &cells
			}
			input.Owners[r] = test.owner
			plan, err = planCouplingReservation(input)
			if err != nil {
				t.Fatal("plan must release the first track cell", err)
			}
			if index, _ := slices.BinarySearchFunc(plan.Claims, r, func(c couplingClaim, r resource) int { return compareCouplingResource(c.Resource, r) }); !plan.Claims[index].Expected.isZero() {
				t.Fatal("plan expects an owner on a released resource")
			}
			c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: plan, Current: input, GroupID: "pair"})
			if test.want != "" {
				if !errors.Is(err, errCouplingReservationDenied) || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("got %v, want refusal %q", err, test.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			step, err := initialCouplingMotion(c, input)
			if err != nil {
				t.Fatal(err)
			}
			written := slices.ContainsFunc(step.Writes, func(w couplingOwnerWrite) bool { return w.Resource == r && w.Next == c.owner })
			if written != test.inject {
				t.Fatalf("formation write of the reneeded resource is %t, want %t", written, test.inject)
			}
		})
	}
}

// The matrix checks the formation claim rule against an oracle that reads
// the route cells. In each case, a foreign pod owns each released resource
// that no member owns. Exclusive ownership with the 12 m staging spacing
// puts the rear at a cell end, or a few ulps before it, so a staging point
// farther inside a cell is not a valid formation state.
func TestCouplingFormationClaimGeometry(t *testing.T) {
	t.Parallel()
	compact := classBit(string(CompactClass))
	// A lane from a at about 13 degrees puts the junction of a in cells 0
	// and 1 of the assembly lane, so its release is 60 m, at the rear.
	fork := func(n *Network) {
		n.Nodes = append(n.Nodes, Node{ID: "fork", Position: Point{X: 200, Y: 45}})
		n.Lanes = append(n.Lanes, Lane{ID: "a-fork", From: "a", To: "fork", SpeedLimit: 7, VehicleClasses: compact})
	}
	// A 64 m lane into a gives the rear a later route origin.
	prefix := func(n *Network) {
		n.Nodes = append(n.Nodes, Node{ID: "pre", Position: Point{X: -64}})
		n.Lanes = append(n.Lanes, Lane{ID: "pre-a", From: "pre", To: "a", SpeedLimit: 7, VehicleClasses: compact})
	}
	// The assembly lane is 224 m long, with 8 cells of 28 m.
	long := func(n *Network) {
		for i := range n.Nodes {
			if n.Nodes[i].ID == "b" {
				n.Nodes[i].Position.X = 224
			}
		}
	}
	junction := resource{kind: junctionResource, id: "a"}
	track := func(cell int) resource { return resource{kind: trackResource, id: "ab", cell: cell} }
	for _, test := range []struct {
		name     string
		spec     couplingFormationSpec
		released []resource
		needed   []resource
		// planOnly marks a state that the motion legs refuse: a member
		// that is not exactly at its staging point.
		planOnly bool
	}{
		{name: "aligned", spec: couplingFormationSpec{},
			released: []resource{{kind: nodeResource, id: "a"}, junction, track(0)}, needed: []resource{track(1)}},
		{name: "later cell end", spec: couplingFormationSpec{rearStage: 90}, released: []resource{track(0), track(1)}, needed: []resource{track(2)}},
		{name: "shorter cells", spec: couplingFormationSpec{edit: long, rearStage: 56}, released: []resource{junction, track(0)}, needed: []resource{track(1)}},
		{name: "rear one ulp before the cell end", spec: couplingFormationSpec{ulps: [2]int{0, -1}}, released: []resource{junction, track(0)}, needed: []resource{track(1)}, planOnly: true},
		{name: "front ulps past the axis release", spec: couplingFormationSpec{ulps: [2]int{3, 0}}, released: []resource{junction, track(0)}, needed: []resource{track(1)}, planOnly: true},
		{name: "unequal route origins", spec: couplingFormationSpec{edit: prefix, prefix: [2][]string{nil, {"pre-a"}}}, released: []resource{junction, track(0)}, needed: []resource{track(1)}},
		{name: "repeated junction at its release", spec: couplingFormationSpec{edit: fork}, released: []resource{junction, track(0)}},
		{name: "repeated junction one ulp before", spec: couplingFormationSpec{edit: fork, ulps: [2]int{0, -1}}, released: []resource{track(0)}, needed: []resource{junction}, planOnly: true},
		{name: "repeated junction, front ulps past", spec: couplingFormationSpec{edit: fork, ulps: [2]int{2, 0}}, released: []resource{junction}, planOnly: true},
		{name: "repeated junction at a later cell end", spec: couplingFormationSpec{edit: fork, rearStage: 90}, released: []resource{junction, track(1)}, needed: []resource{track(2)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := couplingFormationFixture(t, test.spec)
			plan, err := planCouplingReservation(input)
			if err != nil {
				t.Fatal("control:", err)
			}
			through := [2]int{plan.Exits[0].Through, plan.Exits[1].Through}
			c, _, err := couplingFormationAdopt(input)
			if test.planOnly != (err != nil) {
				t.Fatalf("control adoption error %v, want an error %t", err, test.planOnly)
			}
			if err == nil {
				through = c.through
			}
			needs := couplingFormationNeeds(&plan, through)
			third := podResourceOwner("third")
			for _, claim := range plan.Claims {
				if !needs[claim.Resource] && input.Owners[claim.Resource].isZero() {
					input.Owners[claim.Resource] = third
				}
			}
			for _, r := range test.released {
				if needs[r] || input.Owners[r] != third || !slices.ContainsFunc(plan.Claims, func(claim couplingClaim) bool { return claim.Resource == r }) {
					t.Fatalf("oracle needs %+v, or no foreign pod holds it, or the claim list lacks it", r)
				}
			}
			for _, r := range test.needed {
				if !needs[r] {
					t.Fatalf("oracle releases %+v", r)
				}
			}
			plan, err = planCouplingReservation(input)
			if err != nil {
				t.Fatal(err)
			}
			for _, claim := range plan.Claims {
				if expected := input.Owners[claim.Resource]; needs[claim.Resource] && claim.Expected != expected || !needs[claim.Resource] && !claim.Expected.isZero() {
					t.Fatalf("plan claim %+v, needed %t", claim, needs[claim.Resource])
				}
			}
			if test.planOnly {
				return
			}
			c, step, err := couplingFormationAdopt(input)
			if err != nil {
				t.Fatal(err)
			}
			written := make(map[resource]bool)
			for _, w := range step.Writes {
				if written[w.Resource] || w.Next.isZero() {
					t.Fatalf("write %+v repeats or releases at formation", w)
				}
				written[w.Resource] = true
			}
			if !maps.Equal(written, needs) {
				t.Fatalf("writes differ from the oracle needs:\nwrites %v\nneeds  %v", written, needs)
			}
			for i, claim := range c.claims {
				released := c.dependencyOwner(c.dependencies[i], step.State).isZero()
				expected := input.Owners[claim.Resource]
				if released {
					expected = resourceOwner{}
				}
				if released == needs[claim.Resource] || claim.Expected != expected {
					t.Fatalf("claim %+v: released %t, needed %t", claim, released, needs[claim.Resource])
				}
			}
			owners := maps.Clone(input.Owners)
			applyCouplingTestWrites(t, owners, step.Writes)
			for r, owner := range input.Owners {
				if owner == third && owners[r] != third {
					t.Fatalf("formation changed the foreign owner of %+v", r)
				}
			}
		})
	}
}

// The shared release rule at one ulp around each threshold. The front
// origin is 10 m, the member thresholds are 50 m, and the axis threshold
// is 40 m, so the front axis term is at 62 m.
func TestCouplingDependencyReleasedBoundaries(t *testing.T) {
	t.Parallel()
	below := func(x float64) float64 { return math.Nextafter(x, math.Inf(-1)) }
	above := func(x float64) float64 { return math.Nextafter(x, math.Inf(1)) }
	member := couplingDependency{MemberUse: [2]bool{true, true}, MemberRelease: [2]float64{50, 50}}
	axis := member
	axis.AxisUse, axis.AxisRelease = true, 40
	site := member
	site.Site = true
	rear := couplingDependency{MemberUse: [2]bool{false, true}, MemberRelease: [2]float64{0, 50}}
	for _, test := range []struct {
		name      string
		d         couplingDependency
		distances [2]float64
		draining  bool
		want      bool
	}{
		{"members at their thresholds", member, [2]float64{50, 50}, false, true},
		{"rear one ulp before", member, [2]float64{50, below(50)}, false, false},
		{"front one ulp before", member, [2]float64{below(50), 50}, false, false},
		{"rear one ulp past", member, [2]float64{50, above(50)}, false, true},
		{"unused member far behind", rear, [2]float64{0, 50}, false, true},
		{"front axis at its threshold", axis, [2]float64{62, 50}, false, true},
		{"front axis one ulp before", axis, [2]float64{below(62), 50}, false, false},
		{"front axis one ulp past", axis, [2]float64{above(62), 50}, false, true},
		{"axis while draining", axis, [2]float64{50, 50}, true, true},
		{"site before draining", site, [2]float64{100, 100}, false, false},
		{"site while draining", site, [2]float64{50, 50}, true, true},
		{"site while draining, rear one ulp before", site, [2]float64{50, below(50)}, true, false},
	} {
		if got := couplingDependencyReleased(test.d, test.distances, 10, test.draining); got != test.want {
			t.Errorf("%s: released %t, want %t", test.name, got, test.want)
		}
	}
}

// After formation, the axis term keeps a corridor resource until the
// front is Clearance past its axis release, although both members passed
// their member thresholds earlier.
func TestCouplingFormationAxisReleaseTiming(t *testing.T) {
	t.Parallel()
	input := couplingMotionFixture(t, false, false)
	c, _, err := couplingFormationAdopt(input)
	if err != nil {
		t.Fatal(err)
	}
	r := resource{kind: trackResource, id: "ab", cell: 1}
	index, _ := slices.BinarySearchFunc(c.dependencies, r, func(d couplingDependency, r resource) int { return compareCouplingResource(d.Resource, r) })
	d := c.dependencies[index]
	// The ordinary release of cell 1 is at its end, 60 m, plus Clearance.
	if d.Resource != r || d.MemberRelease != [2]float64{72, 72} || d.AxisRelease != 72 {
		t.Fatalf("fixture dependency differs: %+v", d)
	}
	var axis, members uint64
	for elapsed := uint64(1); elapsed <= c.ticks && axis == 0; elapsed++ {
		state, err := c.stateAt(elapsed)
		if err != nil {
			t.Fatal(err)
		}
		if members == 0 && state.Distances[0] >= 72 && state.Distances[1] >= 72 {
			members = elapsed
		}
		if state.Distances[0]-c.reservation.axisOrigins[0] >= 72+Clearance {
			axis = elapsed
		}
	}
	event := slices.IndexFunc(c.events, func(e couplingOwnerEvent) bool { return e.write.Resource == r })
	if members == 0 || axis <= members || event < 0 {
		t.Fatalf("fixture lacks separate release ticks: members %d, axis %d", members, axis)
	}
	if got := c.events[event]; got.tick != axis || got.write.Expected != c.owner || !got.write.Next.isZero() {
		t.Fatalf("cell 1 releases at tick %d, want the axis tick %d, not the member tick %d", got.tick, axis, members)
	}
	t.Logf("member tick %d, axis tick %d", members, axis)
}
