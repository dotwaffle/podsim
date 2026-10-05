package sim

// ownerKind separates physical group ownership from individual pod identity.
type ownerKind uint8

const (
	podOwnerKind ownerKind = iota + 1
	// groupOwnerKind is reserved. No current simulation creates a group owner.
	groupOwnerKind
	// faultOwnerKind owns the resources of a fault. Its ID is the fault ID.
	faultOwnerKind
)

// resourceOwner is comparable. Only its zero value represents a free resource.
type resourceOwner struct {
	kind ownerKind
	id   string
}

func podResourceOwner(id string) resourceOwner {
	if id == "" {
		return resourceOwner{}
	}
	return resourceOwner{kind: podOwnerKind, id: id}
}

func (o resourceOwner) isZero() bool { return o == (resourceOwner{}) }

func (o resourceOwner) isPod(id string) bool {
	return o.kind == podOwnerKind && o.id != "" && o.id == id
}

// podID returns no identity for a group, a fault, or an unknown owner kind.
func (o resourceOwner) podID() string {
	if o.kind != podOwnerKind {
		return ""
	}
	return o.id
}

// String preserves ordinary owner names in diagnostics and blocking signals.
func (o resourceOwner) String() string {
	switch {
	case o.isZero():
		return ""
	case o.kind == podOwnerKind && o.id != "":
		return o.id
	case o.kind == groupOwnerKind:
		return "mechanical group " + o.id
	case o.kind == faultOwnerKind:
		return o.id
	default:
		return "unknown resource owner " + o.id
	}
}

func (s *Simulation) ownerVehicle(owner resourceOwner) *vehicle {
	id := owner.podID()
	if id == "" {
		return nil
	}
	return s.findVehicle(id)
}

func (s *Simulation) ownerAheadInPlatoon(v *vehicle, owner resourceOwner) bool {
	id := owner.podID()
	return id != "" && s.aheadInPlatoon(v, id)
}
