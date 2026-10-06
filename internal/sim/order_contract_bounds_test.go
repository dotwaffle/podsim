package sim

import (
	"slices"
	"strings"
	"testing"
)

// TestValidateContractNetworkRecordsRefusalOrder pins the error that a
// network with two invalid records gets. validateContractNetworkRecords
// checks the nodes, the lanes, and then each station with its berths and
// its banks. Each case breaks two adjacent checks, and the earlier check
// gives the refusal.
func TestValidateContractNetworkRecordsRefusalOrder(t *testing.T) {
	t.Parallel()
	const (
		node      = "invalid Express node"
		lane      = "invalid Express lane record"
		station   = "invalid Express station record"
		berth     = "invalid Express berth record"
		bank      = "invalid Express bank record"
		bankBerth = "invalid Express bank berth ID"
	)
	hubIndex := func(n Network) int {
		return slices.IndexFunc(n.Stations, func(st Station) bool { return st.ID == "hub" })
	}
	if h := hubIndex(BankExample()); h < 0 || h+1 >= len(BankExample().Stations) || len(BankExample().Stations[h].Banks) < 2 {
		t.Fatal("the bank fixture does not have the hub station that the cases change")
	}
	for _, base := range []Network{Example(), BankExample()} {
		if err := validateContractNetworkRecords(base); err != nil {
			t.Fatalf("base network records: %v", err)
		}
	}
	for _, test := range []struct {
		name string
		base func() Network
		edit func(*Network)
		want string
	}{
		{"node_before_lane", Example, func(n *Network) {
			n.Nodes[1].ID = n.Nodes[0].ID
			n.Lanes[0].From = "missing"
		}, node},
		{"lane_before_station", Example, func(n *Network) {
			n.Lanes[0].SeparationGroup = strings.Repeat("x", 65)
			n.Stations[0].Name = ""
		}, lane},
		{"station_before_its_berth", Example, func(n *Network) {
			n.Stations[0].Name = ""
			n.Stations[0].Berths[0].Node = "missing"
		}, station},
		{"berth_before_next_station", Example, func(n *Network) {
			n.Stations[0].Berths[0].Node = "missing"
			n.Stations[1].Name = ""
		}, berth},
		{"berth_before_bank", BankExample, func(n *Network) {
			hub := &n.Stations[hubIndex(*n)]
			hub.Berths[1].ID = ""
			hub.Banks[0].ID = ""
		}, berth},
		{"bank_before_its_berth_ids", BankExample, func(n *Network) {
			hub := &n.Stations[hubIndex(*n)]
			hub.Banks[0].ID = ""
			hub.Banks[0].BerthIDs[0] = ""
		}, bank},
		{"bank_berth_id_before_next_bank", BankExample, func(n *Network) {
			hub := &n.Stations[hubIndex(*n)]
			hub.Banks[0].BerthIDs[0] = ""
			hub.Banks[1].ID = ""
		}, bankBerth},
		{"bank_before_next_station", BankExample, func(n *Network) {
			h := hubIndex(*n)
			n.Stations[h].Banks[1].BerthIDs[0] = ""
			n.Stations[h+1].Name = ""
		}, bankBerth},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			n := test.base()
			test.edit(&n)
			if err := validateContractNetworkRecords(n); err == nil || err.Error() != test.want {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
}
