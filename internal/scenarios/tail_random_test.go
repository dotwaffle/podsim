package scenarios_test

import (
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// tailGenerator changes the links and the fan curves of a ring.
type tailGenerator struct {
	name       string
	tripSecond int
	change     func(network *sim.Network, random *rand.Rand)
}

func randomPositions(network *sim.Network) map[string]sim.Point { return certNodes(*network) }

// bendFans moves the control point of each fan lane nearer to the middle of
// its chord, by a random fraction.
func bendFans(network *sim.Network, random *rand.Rand) {
	positions := randomPositions(network)
	for index := range network.Lanes {
		lane := &network.Lanes[index]
		if lane.Control == nil {
			continue
		}
		from, to, nearer := positions[lane.From], positions[lane.To], 0.5*random.Float64()
		lane.Control = &sim.Point{
			X: lane.Control.X + nearer*((from.X+to.X)/2-lane.Control.X),
			Y: lane.Control.Y + nearer*((from.Y+to.Y)/2-lane.Control.Y),
		}
	}
}

// linkFrame returns the middle of a link, the unit vector along it, and the
// unit vector away from the center of the ring.
func linkFrame(from, to sim.Point) (middle, along, out sim.Point, length float64) {
	middle = sim.Point{X: (from.X + to.X) / 2, Y: (from.Y + to.Y) / 2}
	length = math.Hypot(to.X-from.X, to.Y-from.Y)
	along = sim.Point{X: (to.X - from.X) / length, Y: (to.Y - from.Y) / length}
	out = sim.Point{X: along.Y, Y: -along.X}
	if out.X*middle.X+out.Y*middle.Y < 0 {
		out = sim.Point{X: -out.X, Y: -out.Y}
	}
	return middle, along, out, length
}

// opposedBends gives alternate links a curve to each side.
func opposedBends(network *sim.Network, random *rand.Rand) {
	bendFans(network, random)
	positions := randomPositions(network)
	side := 1.0
	for index := range network.Lanes {
		lane := &network.Lanes[index]
		if strings.HasPrefix(lane.ID, "link-") {
			middle, _, out, length := linkFrame(positions[lane.From], positions[lane.To])
			depth := side * length * (0.1 + 0.3*random.Float64())
			lane.Control = &sim.Point{X: middle.X + depth*out.X, Y: middle.Y + depth*out.Y}
			side = -side
		}
	}
}

// splitLinks replaces each link by the lanes that shape makes.
func splitLinks(network *sim.Network, shape func(link sim.Lane, positions map[string]sim.Point) ([]sim.Node, []sim.Lane)) {
	positions := randomPositions(network)
	lanes := make([]sim.Lane, 0, len(network.Lanes))
	for _, lane := range network.Lanes {
		if !strings.HasPrefix(lane.ID, "link-") {
			lanes = append(lanes, lane)
			continue
		}
		nodes, parts := shape(lane, positions)
		network.Nodes = append(network.Nodes, nodes...)
		lanes = append(lanes, parts...)
	}
	network.Lanes = lanes
}

// hairpinLinks puts a hairpin with a chord above 20 m in each link.
func hairpinLinks(network *sim.Network, random *rand.Rand) {
	splitLinks(network, func(link sim.Lane, positions map[string]sim.Point) ([]sim.Node, []sim.Lane) {
		middle, along, out, _ := linkFrame(positions[link.From], positions[link.To])
		half := 11 + 9*random.Float64()
		depth := 120 + 280*random.Float64()
		in, back := link.ID+"-in", link.ID+"-back"
		control := sim.Point{X: middle.X + depth*out.X, Y: middle.Y + depth*out.Y}
		return []sim.Node{
			{ID: in, Position: sim.Point{X: middle.X - half*along.X, Y: middle.Y - half*along.Y}},
			{ID: back, Position: sim.Point{X: middle.X + half*along.X, Y: middle.Y + half*along.Y}},
		}, []sim.Lane{
			{ID: link.ID + "-a", From: link.From, To: in, SpeedLimit: link.SpeedLimit, VehicleClasses: link.VehicleClasses},
			{ID: link.ID + "-hairpin", From: in, To: back, SpeedLimit: link.SpeedLimit, VehicleClasses: link.VehicleClasses, Control: &control},
			{ID: link.ID + "-b", From: back, To: link.To, SpeedLimit: link.SpeedLimit, VehicleClasses: link.VehicleClasses},
		}
	})
	bendFans(network, random)
}

// sharpApexLinks puts a sharp straight corner in each link.
func sharpApexLinks(network *sim.Network, random *rand.Rand) {
	splitLinks(network, func(link sim.Lane, positions map[string]sim.Point) ([]sim.Node, []sim.Lane) {
		middle, _, out, length := linkFrame(positions[link.From], positions[link.To])
		depth := length * (0.8 + 0.7*random.Float64())
		apex := link.ID + "-apex"
		return []sim.Node{{ID: apex, Position: sim.Point{X: middle.X + depth*out.X, Y: middle.Y + depth*out.Y}}}, []sim.Lane{
			{ID: link.ID + "-a", From: link.From, To: apex, SpeedLimit: link.SpeedLimit, VehicleClasses: link.VehicleClasses},
			{ID: link.ID + "-b", From: apex, To: link.To, SpeedLimit: link.SpeedLimit, VehicleClasses: link.VehicleClasses},
		}
	})
	bendFans(network, random)
}

var tailGenerators = []tailGenerator{
	{"opposed bends", 2, opposedBends},
	{"hairpin", 1, hairpinLinks},
	{"sharp apex", 1, sharpApexLinks},
}

// tailRandomSeeds is the fixed seed list of the random networks.
var tailRandomSeeds = []uint64{1, 2, 3, 5, 8, 13, 21, 34}

// TestTailRandomCurvedNetworks runs rings with changed links and fan curves,
// with all classes and about 70 percent Group pods, for 8 simulated minutes.
// An independent loop checks 20 m between centers at each tick.
func TestTailRandomCurvedNetworks(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long test")
	}
	for _, generator := range tailGenerators {
		t.Run(generator.name, func(t *testing.T) {
			t.Parallel()
			var accepted atomic.Int32
			t.Cleanup(func() {
				if count := int(accepted.Load()); count < len(tailRandomSeeds)/2 {
					t.Errorf("only %d networks of %d prepared, so the run is void", count, len(tailRandomSeeds))
				}
			})
			for _, seed := range tailRandomSeeds {
				t.Run(strconv.FormatUint(seed, 10), func(t *testing.T) {
					t.Parallel()
					if runRandomNetwork(t, generator, seed) {
						accepted.Add(1)
					}
				})
			}
		})
	}
}

func runRandomNetwork(t *testing.T, generator tailGenerator, seed uint64) bool {
	t.Helper()
	random := rand.New(rand.NewPCG(seed, 0x7a11)) // #nosec G404 -- A fixed test seed.
	stations := 3 + random.IntN(3)
	config, err := scenarios.Config(scenarios.Parameters{
		Name: "Random curved ring", Stations: stations + 1, Pods: 4 + random.IntN(8),
		PassengerBerths: 4, ParkingBerths: 4, DemandPerMinute: 8, DemandSeed: seed,
	})
	if err != nil {
		t.Fatal(err)
	}
	network := allClassNetwork(t, config.Network)
	generator.change(&network, random)
	if _, refused := sim.PrepareNetwork(network); refused != nil {
		t.Logf("seed %d refused: %v", seed, refused)
		return false
	}
	fleet := slices.Clone(config.Fleet)
	for index := range fleet {
		fleet[index].Class = sim.LegacyClass
		if random.Float64() < 0.7 {
			fleet[index].Class = sim.GroupClass
		}
	}
	s, err := sim.NewFleet(network, fleet)
	if err != nil {
		t.Fatal(err)
	}
	passengers := make([]string, 0, stations)
	for _, station := range network.Stations {
		if !station.ParkingOnly {
			passengers = append(passengers, station.ID)
		}
	}
	for tick := range 8 * 60 * sim.TicksPerSecond {
		if tick%(generator.tripSecond*sim.TicksPerSecond) == 0 {
			from := random.IntN(len(passengers))
			to := (from + 1 + random.IntN(len(passengers)-1)) % len(passengers)
			// A full queue refuses a trip, and the demand is above the supply.
			_ = s.RequestTrip(passengers[from], passengers[to])
		}
		s.Step()
		observation := s.SafetyObservation()
		if _, err := observation.Check(); err != nil {
			t.Fatalf("seed %d tick %d: %v", seed, tick, err)
		}
		checkLargeCenters(t, observation.Pods, tick)
	}
	if s.Snapshot().Completed == 0 {
		t.Fatalf("seed %d completed no trip", seed)
	}
	return true
}

// checkLargeCenters checks 20 m between each pair with a large pod, less the 1
// micrometer that the observer permits.
func checkLargeCenters(t *testing.T, pods []sim.Pod, tick int) {
	t.Helper()
	for index, first := range pods {
		for _, second := range pods[index+1:] {
			if first.Class != sim.GroupClass && second.Class != sim.GroupClass {
				continue
			}
			if gap := math.Hypot(first.Position.X-second.Position.X, first.Position.Y-second.Position.Y); gap < 20-1e-6 {
				t.Fatalf("tick %d: pods %s and %s are %v m apart", tick, first.ID, second.ID, gap)
			}
		}
	}
}
