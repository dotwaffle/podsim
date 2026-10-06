package view

import "github.com/dotwaffle/podsim/internal/sim"

// platoonLinkWidth is the width in layout units of the line that joins
// two pods of a platoon.
const platoonLinkWidth = 3

// platoonLink joins a linked pod to the pod ahead of it in its platoon.
// follower and ahead are indexes into the vehicles of a snapshot.
type platoonLink struct{ follower, ahead int }

// platoonMember is the platoon ID and the platoon index of a pod.
type platoonMember struct {
	id    string
	index int
}

// platoonLinks returns a link for each pod that has a pod ahead of it in its
// platoon, in vehicle order. The pod ahead has the same platoon ID and the
// index before. A pod whose pod ahead is not in vehicles gets no link. It
// returns nil when no pod is linked.
func platoonLinks(vehicles []sim.Vehicle) []platoonLink {
	var members map[platoonMember]int
	for index, vehicle := range vehicles {
		if vehicle.PlatoonID == "" {
			continue
		}
		if members == nil {
			members = make(map[platoonMember]int)
		}
		members[platoonMember{vehicle.PlatoonID, vehicle.PlatoonIndex}] = index
	}
	var links []platoonLink
	for index, vehicle := range vehicles {
		if vehicle.PlatoonID == "" || vehicle.PlatoonIndex < 2 {
			continue
		}
		if ahead, ok := members[platoonMember{vehicle.PlatoonID, vehicle.PlatoonIndex - 1}]; ok {
			links = append(links, platoonLink{follower: index, ahead: ahead})
		}
	}
	return links
}
