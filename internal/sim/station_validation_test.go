package sim

import (
	"fmt"
	"slices"
	"testing"
)

// referenceStationValidation starts each ordered berth search with fresh
// work, as station validation did before call-local workspace reuse.
func referenceStationValidation(n Network, graph routeGraph) error {
	forbidden := n.stationForbidden()
	for _, station := range n.Stations {
		if station.Banks != nil {
			continue
		}
		valid := n.connected(station.Entry, station.Exit)
		if valid {
			for _, berth := range station.Berths {
				if _, err := n.routeIndexed(networkRouteInput{class: topologyClass, from: station.Entry, to: berth.Node, forbidden: forbidden}, graph); err != nil {
					valid = false
					break
				}
				if _, err := n.routeIndexed(networkRouteInput{class: topologyClass, from: berth.Node, to: station.Exit, forbidden: forbidden}, graph); err != nil {
					valid = false
					break
				}
			}
		}
		if !valid {
			return fmt.Errorf("station %q needs entry, exit, and through lanes", station.ID)
		}
	}
	return nil
}

func TestStationValidationWorkMatchesFreshSearches(t *testing.T) {
	t.Parallel()
	for name, base := range map[string]Network{"example": Example(), "banks": BankExample()} {
		for _, damage := range []string{"none", "through", "arrival", "departure", "two stations"} {
			t.Run(name+"/"+damage, func(t *testing.T) {
				t.Parallel()
				n := base.clone()
				for index, station := range n.Stations {
					if station.Banks != nil || damage == "none" {
						continue
					}
					n.Lanes = slices.DeleteFunc(n.Lanes, func(lane Lane) bool {
						switch damage {
						case "through":
							return lane.From == station.Entry && lane.To == station.Exit
						case "arrival":
							return lane.To == station.Berths[len(station.Berths)-1].Node
						case "departure":
							return lane.From == station.Berths[len(station.Berths)-1].Node
						case "two stations":
							return lane.To == station.Berths[0].Node
						default:
							panic("unknown damage")
						}
					})
					if damage != "two stations" || index > 0 {
						break
					}
				}
				graph := newRouteGraph(n)
				got, want := n.validateStationRoutes(graph), referenceStationValidation(n, graph)
				if fmt.Sprint(got) != fmt.Sprint(want) {
					t.Fatalf("station validation = %v, want %v", got, want)
				}
				if (got == nil) != (damage == "none") {
					t.Fatalf("damage %s: validation = %v", damage, got)
				}
			})
		}
	}
}

func BenchmarkStationRouteValidation(b *testing.B) {
	n := Example()
	for index := range 2000 {
		n.Nodes = append(n.Nodes, Node{ID: fmt.Sprintf("unused-%d", index)})
	}
	graph := newRouteGraph(n)
	for _, test := range []struct {
		name string
		run  func(Network, routeGraph) error
	}{
		{"reused", Network.validateStationRoutes},
		{"fresh", referenceStationValidation},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := test.run(n, graph); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
