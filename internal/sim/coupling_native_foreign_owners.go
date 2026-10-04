package sim

// Only currently retained body and granted-ahead intervals are visited.
// maxTail was computed once from the whole immutable canonical route.
func (frame *nativeForeignTick) sealOwners(index int, path *couplingForeignPath) (*couplingForeignOwnerView, error) {
	fact := &frame.facts[index]
	view := &couplingForeignOwnerView{path: path, through: fact.through, distance: fact.distance, owners: make(map[resource]resourceOwner)}
	if path.parked {
		view.through, view.distance = -1, 0
		for _, r := range berthResources(path.berth) {
			if !frame.owners[r].isPod(path.id) {
				return nil, couplingMotionInvariant("native stationary berth lacks exact pod ownership")
			}
			view.owners[r] = frame.owners[r]
		}
		return view, nil
	}
	blocks := &path.blocks
	if fact.through < 0 || fact.through >= blocks.len() || fact.blockIndex < 0 || fact.blockIndex > fact.through || fact.distance > blocks.at(fact.through).end {
		return nil, couplingMotionInvariant("native retained grant interval is invalid")
	}
	start := nativeForeignFirstRetained(blocks, fact.distance-frame.fleet.entries[index].maxTail)
	for block, b := range blocks.span(start, fact.through+1) {
		for _, r := range b.resources {
			release := resourceReleaseDistance(b, r)
			if release <= fact.distance {
				continue
			}
			if retained, exists := fact.retained[r]; !exists || !finite(retained) || retained < release {
				return nil, couplingMotionInvariant("native body lacks its actual retained grant")
			}
			if err := frame.checkResourceOwner(index, block, r); err != nil {
				return nil, err
			}
			view.owners[r] = frame.owners[r]
		}
	}
	originTail := max(Clearance, blocks.lanes[0].cells.fromTail)
	if largeVehicleClass(fact.pod.Class) {
		originTail = max(originTail, largeClearance)
	}
	if fact.distance < originTail {
		for _, r := range berthResources(fact.origin) {
			if !frame.owners[r].isPod(path.id) {
				return nil, couplingMotionInvariant("native origin body lost its berth or node")
			}
			view.owners[r] = frame.owners[r]
		}
	}
	return view, nil
}

func nativeForeignFirstRetained(blocks *blockList, distance float64) int {
	lo, hi := 0, blocks.len()
	for lo < hi {
		mid := lo + (hi-lo)/2
		if blocks.at(mid).end < distance {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

func (frame *nativeForeignTick) checkResourceOwner(index, block int, r resource) error {
	fact := &frame.facts[index]
	owner := frame.owners[r]
	if owner.isPod(fact.pod.ID) {
		return nil
	}
	if owner.podID() == "" || r.kind == berthResource || fact.link.leader == 0 {
		return couplingMotionInvariant("native grant has an unsupported foreign owner kind")
	}
	if fact.link.compact && (r.kind != trackResource || block < fact.link.first || block > fact.link.end) {
		return couplingMotionInvariant("native compact borrowed resource is outside its track run")
	}
	seen := make(map[int]bool)
	follower := index
	for ahead := fact.link.leader; ahead != 0; {
		leader := ahead - 1
		if leader < 0 || leader >= len(frame.facts) || seen[leader] || frame.facts[leader].follower != follower+1 {
			return couplingMotionInvariant("native predecessor chain is invalid")
		}
		seen[leader] = true
		current := &frame.facts[follower]
		predecessor := &frame.facts[leader]
		followerPath := frame.fleet.entries[follower].path
		leaderPath := frame.fleet.entries[leader].path
		if followerPath == nil || leaderPath == nil || current.pod.Activity != Traveling || predecessor.pod.Activity != Traveling {
			return couplingMotionInvariant("native borrowed owner left its actual moving run")
		}
		link := current.link
		lane := followerPath.blocks.locate(block, 0)
		if lane < link.lane || lane >= link.lane+link.lanes || link.leaderLane < 0 || link.leaderLane+link.lanes > len(leaderPath.blocks.route) || block > link.end {
			return couplingMotionInvariant("native borrowed resource is outside the shared run")
		}
		leaderLane := lane - link.lane + link.leaderLane
		if followerPath.blocks.route[lane].ID != leaderPath.blocks.route[leaderLane].ID {
			return couplingMotionInvariant("native borrowed run maps another canonical lane")
		}
		mapped := leaderPath.blocks.laneFirst(leaderLane) + block - followerPath.blocks.laneFirst(lane)
		if mapped < 0 || mapped > predecessor.through || mapped >= leaderPath.blocks.len() {
			return couplingMotionInvariant("native predecessor did not reserve the borrowed cell")
		}
		if release, held := predecessor.retained[r]; !held || !finite(release) || release <= predecessor.distance {
			return couplingMotionInvariant("native predecessor no longer retains the borrowed resource")
		}
		if owner.isPod(predecessor.pod.ID) {
			return nil
		}
		follower, block = leader, mapped
		ahead = predecessor.link.leader
	}
	return couplingMotionInvariant("native ledger owner is not an actual predecessor")
}
