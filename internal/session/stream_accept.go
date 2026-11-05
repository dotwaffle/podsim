package session

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// streamScansAccept reports that data certainly passes each token scan of
// decodeMarkedJSON: both exact bounds, the root marker, the contract
// markers, the packed orders, the boarding and motion members, the service
// members, and the incident, fault and emergency members. It reads the
// tokens once and builds no path.
//
// False means only that the pass is not sure. The chain of scans then
// decides, so each refusal, its order and its text come from the chain.
// Thus a rule here can be stricter than its scan, but never more lenient.
// The rules are stricter in these ways:
//
//   - A member name with an escape is not sure.
//   - A rule of a member name applies at each path, not only at the paths
//     of its scan. The fault rule also refuses a null array element.
//   - An order rule applies below each member named riders, pending,
//     waiting or request.
//   - A marker is the Express marker only as the first root member.
//   - Each integer is in range. A seed has no exception.
func streamScansAccept(data []byte) bool {
	pass := acceptPass{decoder: jsontext.NewDecoder(bytes.NewBuffer(data))}
	if pass.decoder.PeekKind() != jsontext.KindBeginObject || !pass.value(acceptRoot, acceptOther, 0) {
		return false
	}
	for pass.depth > 0 {
		if !pass.next() {
			return false
		}
	}
	// A second root value passes a prescan, but the header decode refuses it.
	_, err := pass.decoder.ReadToken()
	return errors.Is(err, io.EOF)
}

// acceptNode is a known path of a stream document. A nil node is any other
// path, which has the general bounds and no shape.
type acceptNode struct {
	members []acceptMember
	// element is the node of the elements of an array. A node with an
	// element refuses an object, because the bound tables and the boarding
	// scan also match a member name, such as "*" or "0", at that position.
	element *acceptNode
	// limit holds the largest number of elements of each marker mode, when
	// bounded is true.
	limit   [2]int64
	bounded bool
	// shape is the kind of value that the node needs, or 0 for any kind.
	shape jsontext.Kind
	// atLeastOne refuses an empty array.
	atLeastOne bool
	// closed refuses a member other than members, and needs all of them.
	closed bool
	// id needs a literal of 1 to 64 bytes.
	id bool
	// number is the rule of a number.
	number acceptNumber
}

type acceptMember struct {
	name string
	node *acceptNode
}

// acceptNumber is the rule of a number at a node.
type acceptNumber uint8

const (
	acceptAnyNumber acceptNumber = iota
	acceptFinite
	acceptFiniteNonnegative
)

// The marker modes of a pass, which index acceptNode.limit.
const (
	acceptPlain = iota
	acceptExpress
)

// The largest number of boarding records of a vehicle in the boarding scan.
const (
	boardingRecordsPlain   = 8
	boardingRecordsExpress = 20
)

var (
	// acceptGeneral holds the bounds of a path with no bound of its own.
	// The general bounds of the two contract tables are the same.
	acceptGeneral = streamLimits(contractMarkers{})
	acceptRoot    = newAcceptRoot()
	// acceptVersion is the only literal of a project version.
	acceptVersion = strconv.Itoa(project.CurrentVersion)
)

// newAcceptRoot builds the nodes from the bound tables of both markers, so
// that a change of a bound also changes the pass. It then adds the shapes
// that the boarding scan needs.
func newAcceptRoot() *acceptNode {
	root := new(acceptNode)
	for mode, markers := range []contractMarkers{acceptPlain: {}, acceptExpress: {order: sim.ExpressOrderContract}} {
		for path, limit := range streamLimits(markers).arrays {
			node := root.at(path)
			node.limit[mode], node.bounded = limit, true
		}
	}
	for _, path := range []string{"full/state/simulation/vehicles/*", "frame/state/simulation/vehicles/*"} {
		vehicle := root.at(path)
		vehicle.at("riddenMeters").needNumber(acceptFiniteNonnegative)
		vehicle.at("boardings").needBoardingRecords(true)
	}
	delta := root.at("delta/vehicles/*")
	delta.at("metadata/value/riddenMeters").needNumber(acceptFiniteNonnegative)
	delta.at("boardings").needObject()
	delta.at("boardings/value").needBoardingRecords(false)
	delta.at("motion").needObject()
	delta.at("motion/value").needObject()
	delta.at("motion/value/position").needObject()
	delta.at("motion/value/position/x").needNumber(acceptFinite)
	delta.at("motion/value/position/y").needNumber(acceptFinite)
	delta.at("motion/value/laneDistance").needNumber(acceptFiniteNonnegative)
	delta.at("motion/value/speed").needNumber(acceptFiniteNonnegative)
	return root
}

// at returns the node at path, and adds each node that does not exist. The
// name "*" is an array element.
func (n *acceptNode) at(path string) *acceptNode {
	for name := range strings.SplitSeq(strings.TrimPrefix(path, "/"), "/") {
		if name == "*" {
			if n.element == nil {
				n.element = new(acceptNode)
			}
			n = n.element
			continue
		}
		child, _ := n.member([]byte(name))
		if child == nil {
			child = new(acceptNode)
			n.members = append(n.members, acceptMember{name, child})
		}
		n = child
	}
	return n
}

// needObject makes n a closed object.
func (n *acceptNode) needObject() { n.shape, n.closed = jsontext.KindBeginObject, true }

// needNumber makes n a number with the rule.
func (n *acceptNode) needNumber(rule acceptNumber) { n.shape, n.number = jsontext.KindNumber, rule }

// needBoardingRecords makes n an array of boarding records.
func (n *acceptNode) needBoardingRecords(atLeastOne bool) {
	n.shape, n.atLeastOne = jsontext.KindBeginArray, atLeastOne
	n.limit[acceptPlain] = min(n.limit[acceptPlain], boardingRecordsPlain)
	n.limit[acceptExpress] = min(n.limit[acceptExpress], boardingRecordsExpress)
	record := n.at("*")
	record.needObject()
	berth := record.at("berthID")
	berth.shape, berth.id = jsontext.KindString, true
	record.at("metersAtBoarding").needNumber(acceptFiniteNonnegative)
}

// member returns the node of a member name and its position in n, or nil
// and -1. n can be nil.
func (n *acceptNode) member(name []byte) (*acceptNode, int) {
	if n == nil {
		return nil, -1
	}
	for i := range n.members {
		if n.members[i].name == string(name) {
			return n.members[i].node, i
		}
	}
	return nil, -1
}

// acceptClass is the rule of a member name at each path.
type acceptClass uint8

const (
	acceptOther          acceptClass = iota
	acceptMarker                     // orderContract
	acceptRefused                    // textEncoding
	acceptOrderID                    // in an order, an ID
	acceptOrderText                  // in an order, free text
	acceptServiceID                  // nonempty text; in an order, an ID
	acceptLegFrom                    // an incident member; in an order, an ID
	acceptProjectVersion             // the current project version
	acceptVehicleClass               // a known class
	acceptSharingConsent             // a known consent
	acceptServiceChoice              // a known service
	acceptIncidentMember             // not null and not empty text
	acceptFaultMember                // not null, and nothing null below it
	acceptOrderContainer             // an order is below it
)

func acceptClassOf(name []byte) acceptClass {
	switch string(name) {
	case "orderContract":
		return acceptMarker
	case "textEncoding":
		return acceptRefused
	case "from", "to", "podID":
		return acceptOrderID
	case "dispatchReason":
		return acceptOrderText
	case "serviceID":
		return acceptServiceID
	case "legFrom":
		return acceptLegFrom
	case "projectVersion":
		return acceptProjectVersion
	case "class":
		return acceptVehicleClass
	case "sharingConsent":
		return acceptSharingConsent
	case "service":
		return acceptServiceChoice
	case "incident", "interrupted", "interruptedPassengers", "withdrawn", "operational":
		return acceptIncidentMember
	case "faults", "emergencies":
		return acceptFaultMember
	case "riders", "pending", "waiting", "request":
		return acceptOrderContainer
	}
	return acceptOther
}

// The flags of a container, which it passes to the containers in it.
const (
	acceptInOrder uint8 = 1 << iota
	acceptNoNull
)

// acceptFrame is an open object or array.
type acceptFrame struct {
	node   *acceptNode
	count  int64
	seen   uint64
	flags  uint8
	object bool
}

// acceptPass is the state of one pass. The depth bound limits its open
// containers, so the frames are in a fixed array.
type acceptPass struct {
	decoder *jsontext.Decoder
	frames  [65]acceptFrame
	depth   int
	mode    int
}

// next reads the next member or element of the innermost container, or its
// end.
func (p *acceptPass) next() bool {
	frame := &p.frames[p.depth]
	if kind := p.decoder.PeekKind(); kind == jsontext.KindEndObject || kind == jsontext.KindEndArray {
		if _, err := p.decoder.ReadToken(); err != nil || !p.finish(frame) {
			return false
		}
		p.depth--
		return true
	}
	frame.count++
	if !frame.object {
		var element *acceptNode
		if frame.node != nil {
			element = frame.node.element
		}
		return p.value(element, acceptOther, frame.flags)
	}
	raw, err := p.decoder.ReadValue()
	if err != nil {
		return false
	}
	name := raw[1 : len(raw)-1]
	if bytes.IndexByte(name, '\\') >= 0 {
		return false
	}
	child, index := frame.node.member(name)
	if frame.node != nil && frame.node.closed {
		if index < 0 {
			return false
		}
		frame.seen |= 1 << index
	}
	class := acceptClassOf(name)
	if class == acceptMarker && p.depth == 1 && frame.count == 1 {
		p.mode = acceptExpress
	}
	return p.value(child, class, frame.flags)
}

// finish checks the bounds and the shape of a container at its end.
func (p *acceptPass) finish(frame *acceptFrame) bool {
	node := frame.node
	if frame.object {
		return frame.count <= acceptGeneral.members && (node == nil || !node.closed || frame.seen == 1<<len(node.members)-1)
	}
	limit := acceptGeneral.elements
	if node != nil && node.bounded {
		limit = node.limit[p.mode]
	}
	return frame.count <= limit && (node == nil || !node.atLeastOne || frame.count > 0)
}

// value reads a value. node and class are its rules, and flags are the
// flags of its container.
func (p *acceptPass) value(node *acceptNode, class acceptClass, flags uint8) bool {
	kind := p.decoder.PeekKind()
	if node != nil && node.shape != 0 && kind != node.shape {
		return false
	}
	var raw []byte
	if kind == jsontext.KindString || kind == jsontext.KindNumber {
		var err error
		if raw, err = p.decoder.ReadValue(); err != nil {
			return false
		}
	}
	if !p.memberRule(class, kind, raw, flags) {
		return false
	}
	switch kind {
	case jsontext.KindBeginObject, jsontext.KindBeginArray:
		return p.open(node, class, flags, kind)
	case jsontext.KindString:
		return node == nil || !node.id || acceptID(raw)
	case jsontext.KindNumber:
		return exactInteger(raw) && (node == nil || acceptNumberRule(node.number, raw))
	case jsontext.KindNull, jsontext.KindTrue, jsontext.KindFalse:
		if kind == jsontext.KindNull && flags&acceptNoNull != 0 {
			return false
		}
		_, err := p.decoder.ReadToken()
		return err == nil
	case jsontext.KindInvalid, jsontext.KindEndObject, jsontext.KindEndArray:
	}
	return false
}

// open reads the start of a container.
func (p *acceptPass) open(node *acceptNode, class acceptClass, flags uint8, kind jsontext.Kind) bool {
	object := kind == jsontext.KindBeginObject
	if object && node != nil && node.element != nil || p.depth == acceptGeneral.depth {
		return false
	}
	if _, err := p.decoder.ReadToken(); err != nil {
		return false
	}
	if class == acceptFaultMember {
		flags |= acceptNoNull
	}
	if class == acceptOrderContainer {
		flags |= acceptInOrder
	}
	p.depth++
	p.frames[p.depth] = acceptFrame{node: node, flags: flags, object: object}
	return true
}

// memberRule checks a value against the rule of its member name. raw is the
// literal of a string or a number, and is otherwise nil. A literal with an
// escape does not equal a known value.
func (p *acceptPass) memberRule(class acceptClass, kind jsontext.Kind, raw []byte, flags uint8) bool {
	var text []byte
	if kind == jsontext.KindString {
		text = raw[1 : len(raw)-1]
	}
	order := flags&acceptInOrder != 0
	presentAndNotEmpty := kind != jsontext.KindNull && (kind != jsontext.KindString || len(text) > 0)
	switch class {
	case acceptOther, acceptOrderContainer:
		return true
	case acceptMarker:
		return p.mode == acceptExpress && string(text) == string(sim.ExpressOrderContract)
	case acceptRefused:
		return false
	case acceptOrderID:
		return !order || acceptOrderLiteral(raw, false)
	case acceptOrderText:
		return !order || acceptOrderLiteral(raw, true)
	case acceptServiceID:
		return len(text) > 0 && (!order || acceptOrderLiteral(raw, false))
	case acceptLegFrom:
		return presentAndNotEmpty && (!order || acceptOrderLiteral(raw, false))
	case acceptProjectVersion:
		return kind == jsontext.KindNumber && string(raw) == acceptVersion
	case acceptVehicleClass:
		if len(text) == 0 {
			return false
		}
		_, known := sim.LookupVehicleClass(sim.VehicleClass(text))
		return known
	case acceptSharingConsent:
		return string(text) == string(sim.PrivateConsent) || string(text) == string(sim.SharedConsent)
	case acceptServiceChoice:
		return string(text) == string(sim.OnDemandService) || string(text) == string(sim.ExpressServiceChoice)
	case acceptIncidentMember:
		return presentAndNotEmpty
	case acceptFaultMember:
		return kind != jsontext.KindNull
	}
	return false
}

// acceptOrderLiteral uses the check of scanPackedOrders.
func acceptOrderLiteral(raw []byte, text bool) bool {
	_, err := unpackOrderText(raw, text)
	return err == nil
}

// acceptID checks the literal of a berth ID. An escape only makes the text
// shorter than its literal, and it never makes empty text.
func acceptID(raw []byte) bool {
	return len(raw) > 2 && len(raw)-2 <= 64
}

// acceptNumberRule checks the literal of a number against a rule. A literal
// out of the float64 range has no answer.
func acceptNumberRule(rule acceptNumber, raw []byte) bool {
	if rule == acceptAnyNumber {
		return true
	}
	value, err := strconv.ParseFloat(string(raw), 64)
	return err == nil && (rule == acceptFinite || value >= 0)
}
