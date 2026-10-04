package remote

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// couplingSimulation restores the native coupling fixture of a cohort in
// phase. leg selects the second draining frame.
func couplingSimulation(t *testing.T, cohort string, phase sim.CouplingPhase, leg int) (*sim.Simulation, sim.Network) {
	t.Helper()
	raw, err := os.ReadFile("../session/testdata/coupling_native_phases.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Cohorts map[string]sim.RestoreStateInput `json:"cohorts"`
		Frames  []struct {
			Cohort string         `json:"cohort"`
			State  sim.SavedState `json:"state"`
		} `json:"frames"`
	}
	if decodeErr := json.Unmarshal(raw, &fixtures); decodeErr != nil { //nolint:musttag // Preserve the frozen native API fixture field names.
		t.Fatal(decodeErr)
	}
	input := fixtures.Cohorts[cohort]
	for _, frame := range fixtures.Frames {
		group := frame.State.CouplingGroups[0]
		if frame.Cohort == cohort && group.Phase == phase && (leg == 0 || group.Progress.Leg == leg) {
			input.State = frame.State
			s, result, err := sim.RestoreState(input)
			if err != nil || result.Tier != sim.RestorePhysical {
				t.Fatal("fixture restore", err)
			}
			s.SetPaused(false)
			return s, input.Network
		}
	}
	t.Fatalf("missing %s %s fixture", cohort, phase)
	return nil, sim.Network{}
}

// couplingState returns the checked snapshot of s as a session state.
func couplingState(t *testing.T, s *sim.Simulation, network sim.Network) session.State {
	t.Helper()
	snapshot, err := s.CheckedSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	return session.State{Epoch: "coupling", Revision: uint64(snapshot.Tick) + 1, Speed: 1, Network: network, Simulation: snapshot}
}

// couplingFrames returns two server states of one group, steps ticks apart.
func couplingFrames(t *testing.T, cohort string, phase sim.CouplingPhase, leg, steps int) (session.State, session.State) {
	t.Helper()
	s, network := couplingSimulation(t, cohort, phase, leg)
	first := couplingState(t, s, network)
	for range steps {
		s.Step()
	}
	second := couplingState(t, s, network)
	if len(second.Simulation.CouplingGroups) != 1 || second.Simulation.CouplingGroups[0].Phase != phase {
		t.Fatalf("fixture left phase %s after %d steps", phase, steps)
	}
	return first, second
}

// couplingTransitionFrames steps until the groups change and returns the last
// state before the change and the first state after it.
func couplingTransitionFrames(t *testing.T, cohort string, phase sim.CouplingPhase, leg int) (session.State, session.State) {
	t.Helper()
	s, network := couplingSimulation(t, cohort, phase, leg)
	before := couplingState(t, s, network)
	for range 4000 {
		s.Step()
		after := couplingState(t, s, network)
		if len(after.Simulation.CouplingGroups) != 1 || after.Simulation.CouplingGroups[0].Phase != phase {
			return before, after
		}
		before = after
	}
	t.Fatalf("%s %s did not change", cohort, phase)
	return before, before
}

func cabin(t *testing.T, state sim.Snapshot, id string) sim.Vehicle {
	t.Helper()
	for _, vehicle := range state.Vehicles {
		if vehicle.Pod.ID == id {
			return vehicle
		}
	}
	t.Fatalf("missing cabin %q", id)
	return sim.Vehicle{}
}

func near(a, b sim.Point) bool { return math.Hypot(a.X-b.X, a.Y-b.Y) <= 1e-9 }

// moved returns p turned by turn about pivot, with pivot moved to to.
func moved(p, pivot, to sim.Point, turn float64) sim.Point {
	return rigidMove{pivot: pivot, to: to, turn: turn}.apply(p)
}

// checkMovedGroup checks that every corner of got is a corner of want moved
// with move for its cabin. Body i belongs to cabin i, and the other shapes
// belong to the nearer cabin of want.
func checkMovedGroup(t *testing.T, got, want sim.CouplingGroupView, pins [2]sim.Point, move [2]func(sim.Point) sim.Point) {
	t.Helper()
	for i := range want.Bodies {
		for c, corner := range want.Bodies[i].Corners {
			if !near(got.Bodies[i].Corners[c], move[i](corner)) {
				t.Fatalf("body %d corner %d = %v, want %v", i, c, got.Bodies[i].Corners[c], move[i](corner))
			}
		}
	}
	pairs := [][2]*sim.CouplingRectangle{{got.Connector, want.Connector}, {got.ManeuverEnvelope, want.ManeuverEnvelope}}
	for _, pair := range pairs {
		if (pair[0] == nil) != (pair[1] == nil) {
			t.Fatal("interpolation changed the group shapes")
		}
		if pair[1] == nil {
			continue
		}
		for c, corner := range pair[1].Corners {
			nearer := 0
			if math.Hypot(corner.X-pins[1].X, corner.Y-pins[1].Y) < math.Hypot(corner.X-pins[0].X, corner.Y-pins[0].Y) {
				nearer = 1
			}
			if !near(pair[0].Corners[c], move[nearer](corner)) {
				t.Fatalf("shape corner %d = %v, want %v", c, pair[0].Corners[c], move[nearer](corner))
			}
		}
	}
}

// TestCouplingMotionMovesTrainAsOneUnit interpolates two server frames of
// each phase. A connected train keeps the exact offsets of frame a. Before
// and after the latch, each body moves with its own cabin. Frame b is the
// result at the end of the step.
func TestCouplingMotionMovesTrainAsOneUnit(t *testing.T) {
	t.Parallel()
	// moves tells which cabins travel more than 0.5 m in steps ticks.
	tests := []struct {
		cohort string
		phase  sim.CouplingPhase
		leg    int
		steps  int
		moves  [2]bool
	}{
		{"occupied", sim.CouplingConnected, 0, 60, [2]bool{true, true}},
		{"empty", sim.CouplingConnected, 0, 60, [2]bool{true, true}},
		{"empty", sim.CouplingClosing, 0, 120, [2]bool{false, true}},
		{"empty", sim.CouplingOpening, 0, 120, [2]bool{true, false}},
		{"empty", sim.CouplingDraining, 4, 60, [2]bool{false, true}},
	}
	for _, test := range tests {
		t.Run(test.cohort+" "+string(test.phase), func(t *testing.T) {
			t.Parallel()
			a, b := couplingFrames(t, test.cohort, test.phase, test.leg, test.steps)
			before, after := a.Simulation.CouplingGroups[0], b.Simulation.CouplingGroups[0]
			from := [2]sim.Vehicle{cabin(t, a.Simulation, "front"), cabin(t, a.Simulation, "rear")}
			to := [2]sim.Vehicle{cabin(t, b.Simulation, "front"), cabin(t, b.Simulation, "rear")}
			pins := [2]sim.Point{from[0].Pod.Position, from[1].Pod.Position}
			for i, moves := range test.moves {
				if distance := math.Hypot(to[i].Pod.Position.X-pins[i].X, to[i].Pod.Position.Y-pins[i].Y); (distance > .5) != moves {
					t.Fatalf("cabin %d moved %g m, want movement %v", i, distance, moves)
				}
			}
			for _, fraction := range []float64{0, .25, .5, 1} {
				got := interpolate(a, b, fraction)
				if len(got.CouplingGroups) != 1 {
					t.Fatalf("fraction %v: %d groups", fraction, len(got.CouplingGroups))
				}
				group := got.CouplingGroups[0]
				if group.ID != before.ID || group.Phase != before.Phase || group.Members != before.Members {
					t.Fatalf("fraction %v: group identity changed: %+v", fraction, group.SavedCouplingGroup)
				}
				cabins := [2]sim.Vehicle{cabin(t, got, "front"), cabin(t, got, "rear")}
				var move [2]func(sim.Point) sim.Point
				for i := range cabins {
					want := sim.Point{X: pins[i].X + (to[i].Pod.Position.X-pins[i].X)*fraction, Y: pins[i].Y + (to[i].Pod.Position.Y-pins[i].Y)*fraction}
					if !near(cabins[i].Pod.Position, want) {
						t.Fatalf("fraction %v: cabin %d at %v, want %v", fraction, i, cabins[i].Pod.Position, want)
					}
					move[i] = func(p sim.Point) sim.Point { return moved(p, pins[i], cabins[i].Pod.Position, 0) }
				}
				if test.phase == sim.CouplingConnected {
					offset := sim.Point{X: cabins[1].Pod.Position.X - cabins[0].Pod.Position.X, Y: cabins[1].Pod.Position.Y - cabins[0].Pod.Position.Y}
					if !near(offset, sim.Point{X: pins[1].X - pins[0].X, Y: pins[1].Y - pins[0].Y}) {
						t.Fatalf("fraction %v: connected cabins drifted apart: %v", fraction, offset)
					}
					move[1] = move[0]
					speed := *before.CommonSpeed + (*after.CommonSpeed-*before.CommonSpeed)*fraction
					if *group.CommonSpeed != speed || cabins[0].Pod.Speed != speed || cabins[1].Pod.Speed != speed {
						t.Fatalf("fraction %v: speeds %v %v %v, want %v", fraction, *group.CommonSpeed, cabins[0].Pod.Speed, cabins[1].Pod.Speed, speed)
					}
				}
				checkMovedGroup(t, group, before, pins, move)
				if fraction == 1 {
					identity := [2]func(sim.Point) sim.Point{func(p sim.Point) sim.Point { return p }, func(p sim.Point) sim.Point { return p }}
					checkMovedGroup(t, group, after, [2]sim.Point{to[0].Pod.Position, to[1].Pod.Position}, identity)
				}
			}
		})
	}
}

// TestCouplingMotionTurnsConnectedTrain turns frame b of a connected train
// about its front cabin. The front cabin moves along its route, and the group
// turns in proportion.
func TestCouplingMotionTurnsConnectedTrain(t *testing.T) {
	t.Parallel()
	a, _ := couplingFrames(t, "occupied", sim.CouplingConnected, 0, 0)
	b := a
	b.Revision, b.Simulation.Tick = a.Revision+60, a.Simulation.Tick+60
	b.Simulation.Vehicles = slices.Clone(a.Simulation.Vehicles)
	b.Simulation.CouplingGroups = cloneGroups(a.Simulation.CouplingGroups)
	group := &b.Simulation.CouplingGroups[0]
	front, rear := cabin(t, a.Simulation, "front"), cabin(t, a.Simulation, "rear")
	heading := bodyHeading(group.Bodies[0])
	const travel, turn = 2.0, 0.2
	end := sim.Point{X: front.Pod.Position.X + travel*math.Cos(heading), Y: front.Pod.Position.Y + travel*math.Sin(heading)}
	turnAll(group, rigidMove{pivot: front.Pod.Position, to: end, turn: turn})
	for i := range b.Simulation.Vehicles {
		pod := &b.Simulation.Vehicles[i].Pod
		switch pod.ID {
		case "front":
			pod.LaneDistance += travel
			pod.Position = end
		case "rear":
			pod.Position = moved(pod.Position, front.Pod.Position, end, turn)
		}
	}
	got := interpolate(a, b, .5)
	middle := sim.Point{X: front.Pod.Position.X + travel/2*math.Cos(heading), Y: front.Pod.Position.Y + travel/2*math.Sin(heading)}
	move := func(p sim.Point) sim.Point { return moved(p, front.Pod.Position, middle, turn/2) }
	if !near(cabin(t, got, "front").Pod.Position, middle) || !near(cabin(t, got, "rear").Pod.Position, move(rear.Pod.Position)) {
		t.Fatalf("cabins at %v and %v", cabin(t, got, "front").Pod.Position, cabin(t, got, "rear").Pod.Position)
	}
	checkMovedGroup(t, got.CouplingGroups[0], a.Simulation.CouplingGroups[0], [2]sim.Point{front.Pod.Position, rear.Pod.Position}, [2]func(sim.Point) sim.Point{move, move})
}

func cloneGroups(groups []sim.CouplingGroupView) []sim.CouplingGroupView {
	groups = slices.Clone(groups)
	for i := range groups {
		if groups[i].Connector != nil {
			groups[i].Connector = new(*groups[i].Connector)
		}
		if groups[i].ManeuverEnvelope != nil {
			groups[i].ManeuverEnvelope = new(*groups[i].ManeuverEnvelope)
		}
		if groups[i].CommonSpeed != nil {
			groups[i].CommonSpeed = new(*groups[i].CommonSpeed)
		}
	}
	return groups
}

// turnAll applies move to every corner of group.
func turnAll(group *sim.CouplingGroupView, move rigidMove) {
	for i := range group.Bodies {
		for c := range group.Bodies[i].Corners {
			group.Bodies[i].Corners[c] = move.apply(group.Bodies[i].Corners[c])
		}
	}
	for _, shape := range []*sim.CouplingRectangle{group.Connector, group.ManeuverEnvelope} {
		if shape != nil {
			for c := range shape.Corners {
				shape.Corners[c] = move.apply(shape.Corners[c])
			}
		}
	}
}

// TestCouplingMotionFallsBackToLatestFrame checks the frames that do not show
// one motion of one group. The group and its cabins then show frame b, and
// interpolation never mixes the two frames for them.
func TestCouplingMotionFallsBackToLatestFrame(t *testing.T) {
	t.Parallel()
	member := func(state *session.State, id string) *sim.Vehicle {
		for i := range state.Simulation.Vehicles {
			if state.Simulation.Vehicles[i].Pod.ID == id {
				return &state.Simulation.Vehicles[i]
			}
		}
		return nil
	}
	type transition struct {
		cohort string
		phase  sim.CouplingPhase
		leg    int
	}
	tests := []struct {
		name string
		// transition selects two real frames around a change of the group.
		// Otherwise edit changes two frames of a connected train.
		transition *transition
		edit       func(a, b *session.State)
		// whole is true when the complete snapshot must be frame b.
		whole bool
	}{
		{name: "phase change", transition: &transition{"empty", sim.CouplingClosing, 0}},
		{name: "group disappears", transition: &transition{"empty", sim.CouplingDraining, 4}},
		{name: "group appears", edit: func(a, _ *session.State) {
			a.Simulation.CouplingGroups = nil
			for i := range a.Simulation.Vehicles {
				a.Simulation.Vehicles[i].CouplingID = ""
			}
		}},
		{name: "member order change", edit: func(_, b *session.State) {
			group := &b.Simulation.CouplingGroups[0]
			group.Members[0], group.Members[1] = group.Members[1], group.Members[0]
		}},
		{name: "member exchange", edit: func(a, b *session.State) {
			// Cabin "spare" takes the place of the rear cabin in frame b.
			spare := *member(a, "rear")
			spare.Pod.ID, spare.CouplingID = "spare", ""
			spare.Pod.Position.Y += 50
			a.Simulation.Vehicles = append(a.Simulation.Vehicles, spare)
			spare = *member(b, "rear")
			spare.Pod.ID = "spare"
			b.Simulation.Vehicles = append(b.Simulation.Vehicles, spare)
			member(b, "rear").CouplingID = ""
			b.Simulation.CouplingGroups[0].Members[1] = "spare"
		}},
		{name: "group identity change", edit: func(_, b *session.State) {
			group := &b.Simulation.CouplingGroups[0]
			group.ID, group.OwnerID = "other-pair", "other-pair"
			member(b, "front").CouplingID, member(b, "rear").CouplingID = "other-pair", "other-pair"
		}},
		{name: "phase label change", edit: func(_, b *session.State) { b.Simulation.CouplingGroups[0].Phase = sim.CouplingUnlatching }},
		{name: "invalid common speed", edit: func(_, b *session.State) { *b.Simulation.CouplingGroups[0].CommonSpeed = math.Inf(1) }},
		{name: "formation change", edit: func(_, b *session.State) { b.Simulation.CouplingGroups[0].FormationTick++ }},
		{name: "connector missing", edit: func(_, b *session.State) { b.Simulation.CouplingGroups[0].Connector = nil }},
		{name: "invalid geometry", edit: func(_, b *session.State) {
			b.Simulation.CouplingGroups[0].Bodies[1].Corners[2].X = math.Inf(-1)
		}},
		{name: "invalid earlier geometry", edit: func(a, _ *session.State) {
			a.Simulation.CouplingGroups[0].Connector.Corners[0].Y = math.NaN()
		}},
		{name: "connector moved off the train", edit: func(_, b *session.State) {
			b.Simulation.CouplingGroups[0].Connector.Corners[1].X++
		}},
		{name: "rear cabin left the train", edit: func(_, b *session.State) { member(b, "rear").Pod.Position.Y++ }},
		{name: "front route unknown", edit: func(a, b *session.State) {
			member(a, "front").Route, member(b, "front").Route = nil, nil
		}},
		{name: "member unbound in frame a", edit: func(a, _ *session.State) { member(a, "rear").CouplingID = "" }},
		{name: "missing member", whole: true, edit: func(_, b *session.State) {
			b.Simulation.Vehicles = slices.DeleteFunc(b.Simulation.Vehicles, func(v sim.Vehicle) bool { return v.Pod.ID == "rear" })
		}},
		{name: "member new in frame b", whole: true, edit: func(a, _ *session.State) {
			a.Simulation.CouplingGroups = nil
			a.Simulation.Vehicles = slices.DeleteFunc(a.Simulation.Vehicles, func(v sim.Vehicle) bool { return v.Pod.ID == "rear" })
			member(a, "front").CouplingID = ""
		}},
		{name: "member unbound in frame b", whole: true, edit: func(_, b *session.State) { member(b, "rear").CouplingID = "" }},
		{name: "contract change", whole: true, edit: func(a, _ *session.State) { a.Simulation.CouplingContract = "compact-pair-v0" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var a, b session.State
			if test.transition != nil {
				a, b = couplingTransitionFrames(t, test.transition.cohort, test.transition.phase, test.transition.leg)
			} else {
				a, b = connectedFrames(t)
				test.edit(&a, &b)
			}
			got := interpolate(a, b, .5)
			if test.whole {
				if !reflect.DeepEqual(got, b.Simulation) {
					t.Fatal("snapshot is not the latest frame")
				}
				return
			}
			if !reflect.DeepEqual(got.CouplingGroups, b.Simulation.CouplingGroups) {
				t.Fatalf("groups = %+v, want the latest groups", got.CouplingGroups)
			}
			for _, id := range []string{"front", "rear"} {
				if !reflect.DeepEqual(cabin(t, got, id), cabin(t, b.Simulation, id)) {
					t.Fatalf("cabin %s = %+v, want its latest state", id, cabin(t, got, id).Pod)
				}
			}
		})
	}
}

func connectedFrames(t *testing.T) (session.State, session.State) {
	t.Helper()
	a, b := couplingFrames(t, "occupied", sim.CouplingConnected, 0, 30)
	a.Simulation.Vehicles, b.Simulation.Vehicles = slices.Clone(a.Simulation.Vehicles), slices.Clone(b.Simulation.Vehicles)
	a.Simulation.CouplingGroups, b.Simulation.CouplingGroups = cloneGroups(a.Simulation.CouplingGroups), cloneGroups(b.Simulation.CouplingGroups)
	return a, b
}

// TestCouplingMotionKeepsAuthoritativeFrames checks that interpolation of a
// connected train copies all geometry it changes.
func TestCouplingMotionKeepsAuthoritativeFrames(t *testing.T) {
	t.Parallel()
	a, b := connectedFrames(t)
	wantA, wantB := cloneGroups(a.Simulation.CouplingGroups), cloneGroups(b.Simulation.CouplingGroups)
	vehiclesA, vehiclesB := slices.Clone(a.Simulation.Vehicles), slices.Clone(b.Simulation.Vehicles)
	_ = interpolate(a, b, .5)
	if !reflect.DeepEqual(a.Simulation.CouplingGroups, wantA) || !reflect.DeepEqual(b.Simulation.CouplingGroups, wantB) ||
		!reflect.DeepEqual(a.Simulation.Vehicles, vehiclesA) || !reflect.DeepEqual(b.Simulation.Vehicles, vehiclesB) {
		t.Fatal("interpolation mutated authoritative state")
	}
}

// legacyInterpolate is the cabin interpolation before physical coupling. An
// unmarked state must give the same snapshot.
func legacyInterpolate(a, b session.State, fraction float64) sim.Snapshot {
	first, second, geometry := newMotionFrame(a, time.Time{}), newMotionFrame(b, time.Time{}), newMotionGeometry(a)
	snapshot := first.state.Simulation
	snapshot.Vehicles = slices.Clone(snapshot.Vehicles)
	maxTravel := geometry.maxSpeed*float64(second.state.Simulation.Tick-first.state.Simulation.Tick)/sim.TicksPerSecond + 0.1
	for i := range snapshot.Vehicles {
		before := &snapshot.Vehicles[i]
		afterIndex, ok := second.vehicles[before.Pod.ID]
		if !ok {
			continue
		}
		after := second.state.Simulation.Vehicles[afterIndex]
		if position, ok := geometry.interpolatePosition(*before, after, fraction, maxTravel); ok {
			before.Pod.Position = position
			before.Pod.Speed += (after.Pod.Speed - before.Pod.Speed) * fraction
		}
	}
	return snapshot
}

// TestCouplingMotionUnmarkedControl checks that unmarked states interpolate
// exactly as before physical coupling, including a platoon, a cabin that
// leaves, and a route window.
func TestCouplingMotionUnmarkedControl(t *testing.T) {
	t.Parallel()
	platoon := func() (session.State, session.State) {
		a, b := motionState(0, 2), motionState(12, 4)
		for _, state := range []*session.State{&a, &b} {
			lead := state.Simulation.Vehicles[0]
			lead.Pod.ID, lead.PlatoonID, lead.PlatoonIndex = "02", "02", 1
			lead.Pod.LaneDistance += 6
			lead.Pod.Position.X += 6
			state.Simulation.Vehicles[0].PlatoonID, state.Simulation.Vehicles[0].PlatoonIndex = "02", 2
			state.Simulation.Vehicles = append(state.Simulation.Vehicles, lead)
		}
		return a, b
	}
	window := func() (session.State, session.State) {
		a, b := motionState(0, 9), motionState(12, 9)
		before, after := &a.Simulation.Vehicles[0], &b.Simulation.Vehicles[0]
		after.Pod.LaneID, after.Pod.LaneDistance, after.Pod.Position = "bc", 1, sim.Point{X: 10, Y: 1}
		before.Presentation = &sim.RoutePresentation{Identity: 1, Current: 0, Motion: before.Route}
		after.Presentation = &sim.RoutePresentation{Identity: 1, Current: 1, Motion: after.Route}
		return a, b
	}
	leaves := func() (session.State, session.State) {
		a, b := platoon()
		b.Simulation.Vehicles = b.Simulation.Vehicles[:1]
		return a, b
	}
	for name, frames := range map[string]func() (session.State, session.State){
		"straight": func() (session.State, session.State) { return motionState(0, 0), motionState(6, 1.4) },
		"platoon":  platoon, "window": window, "cabin leaves": leaves,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			a, b := frames()
			for _, fraction := range []float64{0, .1, .5, .9, 1} {
				if got, want := interpolate(a, b, fraction), legacyInterpolate(a, b, fraction); !reflect.DeepEqual(got, want) {
					t.Fatalf("fraction %v: %+v, want %+v", fraction, got, want)
				}
			}
		})
	}
}

// TestCouplingMotionHistoryClears observes a train and then a state of a
// rewind, a reset, a new epoch, or a new server process without the train.
// The map never shows the old group or the old cabin positions.
func TestCouplingMotionHistoryClears(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"rewind", "reset", "epoch", "restart", "project"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			a, b := connectedFrames(t)
			later := motionState(b.Simulation.Tick+6, 3)
			later.Epoch, later.Revision = a.Epoch, b.Revision+1
			switch name {
			case "rewind":
				later.Generation = b.Generation + 1
				later.Simulation.Tick = a.Simulation.Tick - 100
			case "reset":
				later.Simulation.Tick = 0
			case "epoch":
				later.Epoch = "other"
			case "restart":
				later.ServerStart, later.Revision = "restarted", 1
			case "project":
				later.Generation, later.ProjectRevision = b.Generation+1, b.ProjectRevision+1
			}
			var motion Motion
			start := time.Unix(100, 0)
			motion.Observe(a, start)
			motion.Observe(b, start.Add(100*time.Millisecond))
			motion.Observe(later, start.Add(200*time.Millisecond))
			for _, ms := range []int{0, 260, 300, 400} {
				got := motion.Sample(start.Add(time.Duration(ms) * time.Millisecond))
				if got.CouplingContract != "" || len(got.CouplingGroups) != 0 || !reflect.DeepEqual(got, later.Simulation) {
					t.Fatalf("%dms: map kept coupling state %+v", ms, got.CouplingGroups)
				}
			}
		})
	}
}

// TestCouplingMotionGenerationSnaps checks that a new generation with the
// same train never interpolates from the earlier generation.
func TestCouplingMotionGenerationSnaps(t *testing.T) {
	t.Parallel()
	a, b := connectedFrames(t)
	b.Generation = a.Generation + 1
	var motion Motion
	start := time.Unix(100, 0)
	motion.Observe(a, start)
	motion.Observe(b, start.Add(100*time.Millisecond))
	if got := motion.Sample(start.Add(200 * time.Millisecond)); !reflect.DeepEqual(got, b.Simulation) {
		t.Fatal("generation change interpolated the train")
	}
}

// mutateSample changes every cabin position and all train geometry and
// speeds of a sample.
func mutateSample(sample sim.Snapshot) {
	for i := range sample.Vehicles {
		sample.Vehicles[i].Pod.Position.X += 100
	}
	for i := range sample.CouplingGroups {
		group := &sample.CouplingGroups[i]
		group.Bodies[0].Corners[0].X += 100
		for _, shape := range []*sim.CouplingRectangle{group.Connector, group.ManeuverEnvelope} {
			if shape != nil {
				shape.Corners[0].X += 100
			}
		}
		if group.CommonSpeed != nil {
			*group.CommonSpeed++
		}
	}
}

// TestCouplingMotionSamplesAreDetached changes each kind of marked sample
// and checks that no buffered frame changes: an interpolated train, a group
// fallback at a phase change, a fallback to the complete latest frame, and
// the samples before the first frame, after the last frame, and while
// paused.
func TestCouplingMotionSamplesAreDetached(t *testing.T) {
	t.Parallel()
	start := time.Unix(100, 0)
	tests := []struct {
		name string
		// transition selects the frames around a phase change instead of two
		// frames of a connected train. edit changes the frames, and sample
		// takes a sample of them.
		transition bool
		edit       func(a, b *session.State)
		sample     func(a, b session.State) sim.Snapshot
	}{
		{name: "interpolated", sample: func(a, b session.State) sim.Snapshot { return interpolate(a, b, .5) }},
		{name: "phase change", transition: true, sample: func(a, b session.State) sim.Snapshot { return interpolate(a, b, .5) }},
		{name: "complete fallback", edit: func(a, _ *session.State) { a.Simulation.CouplingContract = "compact-pair-v0" },
			sample: func(a, b session.State) sim.Snapshot { return interpolate(a, b, .5) }},
		{name: "before the first frame", sample: func(a, b session.State) sim.Snapshot {
			var motion Motion
			motion.Observe(a, start)
			motion.Observe(b, start.Add(100*time.Millisecond))
			return motion.Sample(start)
		}},
		{name: "after the last frame", sample: func(a, b session.State) sim.Snapshot {
			var motion Motion
			motion.Observe(a, start)
			motion.Observe(b, start.Add(100*time.Millisecond))
			return motion.Sample(start.Add(time.Second))
		}},
		{name: "paused", edit: func(_, b *session.State) { b.Simulation.Paused = true }, sample: func(a, b session.State) sim.Snapshot {
			var motion Motion
			motion.Observe(a, start)
			motion.Observe(b, start.Add(100*time.Millisecond))
			return motion.Sample(start.Add(200 * time.Millisecond))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var a, b session.State
			if test.transition {
				a, b = couplingTransitionFrames(t, "empty", sim.CouplingClosing, 0)
			} else {
				a, b = connectedFrames(t)
			}
			if test.edit != nil {
				test.edit(&a, &b)
			}
			wantVehicles := [2][]sim.Vehicle{slices.Clone(a.Simulation.Vehicles), slices.Clone(b.Simulation.Vehicles)}
			wantGroups := [2][]sim.CouplingGroupView{cloneGroups(a.Simulation.CouplingGroups), cloneGroups(b.Simulation.CouplingGroups)}
			sample := test.sample(a, b)
			if len(sample.CouplingGroups) == 0 {
				t.Fatal("sample has no train")
			}
			mutateSample(sample)
			for i, state := range []session.State{a, b} {
				if !reflect.DeepEqual(state.Simulation.Vehicles, wantVehicles[i]) || !reflect.DeepEqual(state.Simulation.CouplingGroups, wantGroups[i]) {
					t.Fatalf("changing the sample changed frame %d", i)
				}
			}
		})
	}
}

// fleetFrames returns two frames with the largest fleet. Each pair of cabins
// is a copy of the connected train. Unmarked frames have no train data.
func fleetFrames(t *testing.T, marked bool) (session.State, session.State) {
	t.Helper()
	a, b := connectedFrames(t)
	for _, state := range []*session.State{&a, &b} {
		simulation := &state.Simulation
		pair, cabins := simulation.CouplingGroups[0], simulation.Vehicles
		simulation.CouplingGroups, simulation.Vehicles = nil, nil
		for k := range project.MaxPods / 2 {
			group := detachCouplingGroup(pair)
			group.ID = fmt.Sprintf("pair-%03d", k)
			group.OwnerID = group.ID
			for i, id := range pair.Members {
				cabin := cabins[slices.IndexFunc(cabins, func(v sim.Vehicle) bool { return v.Pod.ID == id })]
				cabin.Pod.ID = fmt.Sprintf("%s-%03d", id, k)
				cabin.CouplingID, group.Members[i] = group.ID, cabin.Pod.ID
				if !marked {
					cabin.CouplingID = ""
				}
				simulation.Vehicles = append(simulation.Vehicles, cabin)
			}
			simulation.CouplingGroups = append(simulation.CouplingGroups, group)
		}
		if !marked {
			simulation.CouplingContract, simulation.CouplingEnabled, simulation.CouplingGroups = "", false, nil
		}
	}
	return a, b
}

// TestCouplingMotionSampleAllocations reports the allocations of one map
// sample with the largest fleet. An unmarked sample allocates only its
// vehicle slice between frames and nothing after the latest frame, as before
// physical coupling.
func TestCouplingMotionSampleAllocations(t *testing.T) {
	start := time.Unix(100, 0)
	for _, marked := range []bool{false, true} {
		a, b := fleetFrames(t, marked)
		var motion Motion
		motion.Observe(a, start)
		motion.Observe(b, start.Add(100*time.Millisecond))
		at := start.Add(200 * time.Millisecond)
		sample := motion.Sample(at)
		if len(sample.Vehicles) != project.MaxPods || marked != (len(sample.CouplingGroups) == project.MaxPods/2) {
			t.Fatalf("sample has %d cabins and %d trains", len(sample.Vehicles), len(sample.CouplingGroups))
		}
		if marked && reflect.DeepEqual(sample.CouplingGroups[0].Bodies, b.Simulation.CouplingGroups[0].Bodies) {
			t.Fatal("trains did not interpolate")
		}
		allocations := testing.AllocsPerRun(100, func() { motion.Sample(at) })
		t.Logf("marked %v: %d cabins, %d trains, %g allocations for each sample", marked, len(sample.Vehicles), len(sample.CouplingGroups), allocations)
		latest := testing.AllocsPerRun(100, func() { motion.Sample(at.Add(time.Second)) })
		t.Logf("marked %v: %g allocations for each sample of the latest frame", marked, latest)
		if !marked && (allocations != 1 || latest != 0) {
			t.Fatalf("unmarked samples have %g and %g allocations, want 1 and 0", allocations, latest)
		}
	}
}
