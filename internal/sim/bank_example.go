package sim

// BankExample returns a planar station with two independent gates and a
// separate parking station. It does not change Example or any default.
func BankExample() Network {
	n := Network{}
	addNode := func(id string, x, y float64) { n.Nodes = append(n.Nodes, Node{ID: id, Position: Point{X: x, Y: y}}) }
	addLane := func(id, from, to, station string, role StationLaneRole) {
		n.Lanes = append(n.Lanes, Lane{ID: id, From: from, To: to, SpeedLimit: 14, StationID: station, StationRole: role})
	}
	addStation := func(id string, x, y, direction float64) Station {
		addNode(id+"-entry", x, y)
		addNode(id+"-berth", x+100*direction, y-60*direction)
		addNode(id+"-exit", x+200*direction, y)
		addLane(id+"-through", id+"-entry", id+"-exit", id, StationThroughRole)
		addLane(id+"-in", id+"-entry", id+"-berth", id, StationBerthAccessRole)
		addLane(id+"-out", id+"-berth", id+"-exit", id, StationDepartureRole)
		return Station{ID: id, Name: id, Entry: id + "-entry", Exit: id + "-exit", Berths: []Berth{{ID: id + "-1", Node: id + "-berth"}}}
	}
	n.Stations = append(n.Stations, addStation("origin", 0, 0, 1))
	first, second := addStation("bank-a", 500, 0, 1), addStation("bank-b", 500, 240, 1)
	n.Stations = append(n.Stations, Station{ID: "hub", Name: "Hub", Entry: first.Entry, Exit: first.Exit, Berths: append(first.Berths, second.Berths...), Banks: []StationBank{{ID: "a", Entry: first.Entry, Exit: first.Exit, BerthIDs: []string{first.Berths[0].ID}}, {ID: "b", Entry: second.Entry, Exit: second.Exit, BerthIDs: []string{second.Berths[0].ID}}}})
	for i := range n.Lanes {
		if n.Lanes[i].StationID == "bank-a" || n.Lanes[i].StationID == "bank-b" {
			n.Lanes[i].StationID = "hub"
		}
	}
	for _, bank := range []struct {
		prefix string
		y      float64
	}{{"bank-a", 0}, {"bank-b", 240}} {
		addNode(bank.prefix+"-arrival", 500, bank.y-60)
		addNode(bank.prefix+"-departure", 700, bank.y-60)
		for i := range n.Lanes {
			if n.Lanes[i].ID == bank.prefix+"-in" {
				n.Lanes[i].From = bank.prefix + "-arrival"
			}
			if n.Lanes[i].ID == bank.prefix+"-out" {
				n.Lanes[i].To = bank.prefix + "-departure"
			}
		}
		addLane(bank.prefix+"-arrival", bank.prefix+"-entry", bank.prefix+"-arrival", "hub", StationBerthAccessRole)
		addLane(bank.prefix+"-departure", bank.prefix+"-departure", bank.prefix+"-exit", "hub", StationDepartureRole)
	}
	parking := addStation("parking", 600, 480, -1)
	parking.ParkingOnly = true
	n.Stations = append(n.Stations, parking)
	n.Nodes = append(n.Nodes, []Node{{ID: "split", Position: Point{300, 120}}, {ID: "merge", Position: Point{900, 120}}, {ID: "east", Position: Point{1000, 480}}, {ID: "west", Position: Point{-100, 480}}, {ID: "a-approach", Position: Point{420, 0}}, {ID: "b-approach", Position: Point{420, 240}}, {ID: "a-departure", Position: Point{780, 0}}, {ID: "b-departure", Position: Point{780, 240}}}...)
	addLane("origin-exit", "origin-exit", "split", "origin", StationExitRole)
	for _, bank := range []struct{ name, prefix string }{{"a", "bank-a"}, {"b", "bank-b"}} {
		addLane(bank.name+"-approach", "split", bank.name+"-approach", "hub", StationApproachRole)
		addLane(bank.name+"-entry", bank.name+"-approach", bank.prefix+"-entry", "hub", StationEntryRole)
		addLane(bank.name+"-exit", bank.prefix+"-exit", bank.name+"-departure", "hub", StationExitRole)
		addLane(bank.name+"-merge", bank.name+"-departure", "merge", "", "")
	}
	addLane("return-east", "merge", "east", "", "")
	addLane("parking-entry", "east", parking.Entry, "parking", StationEntryRole)
	addLane("return-west", parking.Exit, "west", "parking", StationExitRole)
	addLane("origin-entry", "west", "origin-entry", "origin", StationEntryRole)
	return n
}
