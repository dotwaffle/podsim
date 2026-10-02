package session

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// Stream limits are independent of the smaller delivery and history windows.
const (
	StreamVersion    = 2
	MaxStreamJSON    = 64 << 20
	MaxStreamMessage = 65 << 20
)

// StreamSource identifies one coherent authoritative state.
type StreamSource struct {
	ServerStart     string `json:"serverStart"`
	Epoch           string `json:"epoch"`
	ProjectRevision uint64 `json:"projectRevision"`
	Generation      uint64 `json:"generation"`
	Revision        uint64 `json:"revision"`
}

// StreamFrame contains bounded presentation routes instead of complete routes.
type StreamFrame struct {
	State  StateFrame              `json:"state"`
	Routes []sim.RoutePresentation `json:"routes"`
}

// Replacement makes presence distinct from null, zero and empty values.
type Replacement[T any] struct {
	Value T `json:"value"`
}

// VehicleDelta replaces independent groups of one vehicle.
type VehicleDelta struct {
	ID       string                              `json:"id"`
	Pod      *Replacement[sim.Pod]               `json:"pod,omitempty"`
	Route    *Replacement[sim.RoutePresentation] `json:"route,omitempty"`
	Riders   *Replacement[[]sim.Request]         `json:"riders,omitempty"`
	Stops    *Replacement[[]string]              `json:"stops,omitempty"`
	Metadata *Replacement[vehicleMetadata]       `json:"metadata,omitempty"`
}
type vehicleMetadata struct {
	RelocatingTo string `json:"RelocatingTo"`
	Rebalancing  bool   `json:"Rebalancing"`
	PlatoonID    string `json:"PlatoonID"`
	PlatoonIndex int    `json:"PlatoonIndex"`
}

type streamStatistics struct {
	Wait                    sim.WaitStats    `json:"Wait"`
	Journey                 sim.JourneyStats `json:"Journey"`
	PassengerDistanceMeters float64          `json:"PassengerDistanceMeters"`
	RiderDistanceMeters     float64          `json:"RiderDistanceMeters"`
	DirectDistanceMeters    float64          `json:"DirectDistanceMeters"`
	MaxDetourRatio          float64          `json:"MaxDetourRatio"`
	SharedParties           int              `json:"SharedParties"`
	SharedRidePartyLimit    int              `json:"SharedRidePartyLimit"`
	EmptyDistanceMeters     float64          `json:"EmptyDistanceMeters"`
	RebalanceMoves          int              `json:"RebalanceMoves"`
}

func statisticsOf(s SimulationFrame) streamStatistics {
	return streamStatistics{s.Wait, s.Journey, s.PassengerDistanceMeters, s.RiderDistanceMeters, s.DirectDistanceMeters, s.MaxDetourRatio, s.SharedParties, s.SharedRidePartyLimit, s.EmptyDistanceMeters, s.RebalanceMoves}
}
func (v streamStatistics) replace(s *SimulationFrame) {
	s.Wait, s.Journey = v.Wait, v.Journey
	s.PassengerDistanceMeters, s.RiderDistanceMeters, s.DirectDistanceMeters = v.PassengerDistanceMeters, v.RiderDistanceMeters, v.DirectDistanceMeters
	s.MaxDetourRatio, s.SharedParties, s.SharedRidePartyLimit = v.MaxDetourRatio, v.SharedParties, v.SharedRidePartyLimit
	s.EmptyDistanceMeters, s.RebalanceMoves = v.EmptyDistanceMeters, v.RebalanceMoves
}

// StreamDelta uses named replacement groups. Vehicle and berth order stays fixed.
type StreamDelta struct {
	Groups   map[string]json.RawMessage `json:"groups"`
	Vehicles []VehicleDelta             `json:"vehicles"`
	Berths   []sim.BerthState           `json:"berths"`
}

// StreamEnvelope is one publication. Sequences use decimal strings on the wire.
type StreamEnvelope struct {
	Kind     string       `json:"kind"`
	Stream   string       `json:"stream"`
	Sequence uint64       `json:"sequence,string"`
	Base     uint64       `json:"base,string,omitempty"`
	Build    string       `json:"build"`
	Source   StreamSource `json:"source"`
	Full     *StreamFrame `json:"full,omitempty"`
	Delta    *StreamDelta `json:"delta,omitempty"`
}

func sourceOf(f StreamFrame) StreamSource {
	s := f.State
	return StreamSource{s.ServerStart, s.Epoch, s.ProjectRevision, s.Generation, s.Revision}
}
func sameChain(a, b StreamFrame) bool {
	x, y := sourceOf(a), sourceOf(b)
	x.Revision, y.Revision = 0, 0
	if (a.Routes == nil) != (b.Routes == nil) || (a.State.Simulation.Vehicles == nil) != (b.State.Simulation.Vehicles == nil) || (a.State.Simulation.Berths == nil) != (b.State.Simulation.Berths == nil) {
		return false
	}
	if x != y || len(a.Routes) != len(b.Routes) || len(a.State.Simulation.Vehicles) != len(b.State.Simulation.Vehicles) || len(a.State.Simulation.Berths) != len(b.State.Simulation.Berths) {
		return false
	}
	for i, v := range a.State.Simulation.Vehicles {
		if v.Pod.ID != b.State.Simulation.Vehicles[i].Pod.ID {
			return false
		}
	}
	for i, v := range a.State.Simulation.Berths {
		if v.ID != b.State.Simulation.Berths[i].ID {
			return false
		}
	}
	return true
}
func meta(v VehicleFrame) vehicleMetadata {
	return vehicleMetadata{v.RelocatingTo, v.Rebalancing, v.PlatoonID, v.PlatoonIndex}
}
func changed[T any](a, b T) *Replacement[T] {
	if reflect.DeepEqual(a, b) {
		return nil
	}
	return &Replacement[T]{b}
}

func changedValue[T comparable](a, b T) *Replacement[T] {
	if a == b {
		return nil
	}
	return &Replacement[T]{b}
}
func changedSlice[T comparable](a, b []T) *Replacement[[]T] {
	if (a == nil) == (b == nil) && slices.Equal(a, b) {
		return nil
	}
	return &Replacement[[]T]{b}
}

// frameGroups assigns every non-vehicle field to one replacement group.
// JSON maps preserve clearing values and the existing decoder's null semantics.
func frameGroups(f StreamFrame) (map[string]json.RawMessage, error) {
	state := f.State
	values := map[string]any{
		"controls": struct {
			Speed          int
			Redistribution bool
			SpeedReduction SpeedReduction
		}{state.Speed, state.Redistribution, state.SpeedReduction},
		"demand": state.Demand, "restore": state.Restore, "checkpoints": f.State.Checkpoints, "pending": f.State.Simulation.Pending,
		"global": struct {
			Submitted int
			Tick      int64
			Paused    bool
			Completed int
			Demo      bool
			DemoError string
		}{state.Simulation.Submitted, state.Simulation.Tick, state.Simulation.Paused, state.Simulation.Completed, state.Simulation.Demo, state.Simulation.DemoError},
	}
	values["statistics"] = statisticsOf(state.Simulation)
	groups := make(map[string]json.RawMessage, len(values))
	for key, value := range values {
		var err error
		groups[key], err = json.Marshal(value)
		if err != nil {
			return nil, err
		}
	}
	return groups, nil
}
func makeDelta(a, b StreamFrame) (StreamDelta, error) {
	old, err := frameGroups(a)
	if err != nil {
		return StreamDelta{}, err
	}
	now, err := frameGroups(b)
	if err != nil {
		return StreamDelta{}, err
	}
	d := StreamDelta{Groups: map[string]json.RawMessage{}}
	for k, v := range now {
		if !bytes.Equal(v, old[k]) {
			d.Groups[k] = v
		}
	}
	for i, v := range b.State.Simulation.Vehicles {
		p := a.State.Simulation.Vehicles[i]
		item := VehicleDelta{ID: v.Pod.ID, Pod: changedValue(p.Pod, v.Pod), Route: changed(a.Routes[i], b.Routes[i]), Riders: changedSlice(p.Riders, v.Riders), Stops: changedSlice(p.Stops, v.Stops), Metadata: changedValue(meta(p), meta(v))}
		if item.Pod != nil || item.Route != nil || item.Riders != nil || item.Stops != nil || item.Metadata != nil {
			d.Vehicles = append(d.Vehicles, item)
		}
	}
	for i, v := range b.State.Simulation.Berths {
		if v != a.State.Simulation.Berths[i] {
			d.Berths = append(d.Berths, v)
		}
	}
	return d, nil
}

func applyGroups(f *StreamFrame, groups map[string]json.RawMessage) error {
	for key, raw := range groups {
		var target any
		switch key {
		case "controls":
			var v struct {
				Speed          int
				Redistribution bool
				SpeedReduction SpeedReduction
			}
			if err := decodeStreamJSON(raw, &v); err != nil {
				return err
			}
			f.State.Speed, f.State.Redistribution = v.Speed, v.Redistribution
			f.State.SpeedReduction = v.SpeedReduction
			continue
		case "global":
			var v struct {
				Submitted int
				Tick      int64
				Paused    bool
				Completed int
				Demo      bool
				DemoError string
			}
			if err := decodeStreamJSON(raw, &v); err != nil {
				return err
			}
			s := &f.State.Simulation
			s.Submitted, s.Tick, s.Paused, s.Completed, s.Demo, s.DemoError = v.Submitted, v.Tick, v.Paused, v.Completed, v.Demo, v.DemoError
			continue
		case "statistics":
			var v streamStatistics
			if err := decodeStreamJSON(raw, &v); err != nil {
				return err
			}
			v.replace(&f.State.Simulation)
			continue
		case "demand":
			f.State.Demand = DemandState{}
			target = &f.State.Demand
		case "restore":
			f.State.Restore = RestoreInfo{}
			target = &f.State.Restore
		case "checkpoints":
			f.State.Checkpoints = nil
			target = &f.State.Checkpoints
		case "pending":
			f.State.Simulation.Pending = nil
			target = &f.State.Simulation.Pending
		default:
			return fmt.Errorf("unknown stream group %q", key)
		}
		if err := decodeStreamJSON(raw, target); err != nil {
			return err
		}
	}
	return nil
}

// ApplyStream applies an envelope to exactly its stated predecessor.
// The returned containers do not mutate a previous accepted frame.
func ApplyStream(previous StreamFrame, stream string, sequence uint64, e StreamEnvelope) (StreamFrame, error) {
	if e.Stream == "" || e.Sequence == 0 || e.Source.ServerStart == "" || e.Source.Epoch == "" {
		return StreamFrame{}, errors.New("invalid stream identity")
	}
	var f StreamFrame
	switch e.Kind {
	case "full":
		if e.Full == nil || e.Delta != nil || e.Base != 0 {
			return f, errors.New("invalid full envelope")
		}
		f = *e.Full
		if sourceOf(f) != e.Source || f.State.Build != e.Build {
			return StreamFrame{}, errors.New("full identity mismatch")
		}
	case "delta":
		if e.Delta == nil || e.Full != nil || stream != e.Stream || sequence != e.Base || e.Sequence != sequence+1 {
			return f, errors.New("delta base mismatch")
		}
		f = previous
		f.State.Simulation.Vehicles = slices.Clone(previous.State.Simulation.Vehicles)
		f.State.Simulation.Berths = slices.Clone(previous.State.Simulation.Berths)
		f.Routes = slices.Clone(previous.Routes)
		if err := applyGroups(&f, e.Delta.Groups); err != nil {
			return StreamFrame{}, err
		}
		vehicles := map[string]int{}
		berths := map[string]int{}
		for i, v := range f.State.Simulation.Vehicles {
			vehicles[v.Pod.ID] = i
		}
		for i, v := range f.State.Simulation.Berths {
			berths[v.ID] = i
		}
		seen := map[string]bool{}
		for _, v := range e.Delta.Vehicles {
			i, ok := vehicles[v.ID]
			if !ok || seen[v.ID] {
				return StreamFrame{}, errors.New("invalid delta vehicle")
			}
			seen[v.ID] = true
			dst := &f.State.Simulation.Vehicles[i]
			if v.Pod != nil {
				if v.Pod.Value.ID != v.ID {
					return StreamFrame{}, errors.New("changed pod ID")
				}
				dst.Pod = v.Pod.Value
			}
			if v.Route != nil {
				f.Routes[i] = v.Route.Value
			}
			if v.Riders != nil {
				dst.Riders = v.Riders.Value
			}
			if v.Stops != nil {
				dst.Stops = v.Stops.Value
			}
			if v.Metadata != nil {
				m := v.Metadata.Value
				dst.RelocatingTo, dst.Rebalancing, dst.PlatoonID, dst.PlatoonIndex = m.RelocatingTo, m.Rebalancing, m.PlatoonID, m.PlatoonIndex
			}
		}
		clear(seen)
		for _, v := range e.Delta.Berths {
			i, ok := berths[v.ID]
			if !ok || seen[v.ID] {
				return StreamFrame{}, errors.New("invalid delta berth")
			}
			seen[v.ID] = true
			f.State.Simulation.Berths[i] = v
		}
		f.State.ServerStart, f.State.Epoch, f.State.ProjectRevision, f.State.Generation, f.State.Revision = e.Source.ServerStart, e.Source.Epoch, e.Source.ProjectRevision, e.Source.Generation, e.Source.Revision
		f.State.Build = e.Build
		if !sameChain(previous, f) {
			return StreamFrame{}, errors.New("delta crosses source boundary")
		}
	default:
		return f, errors.New("unknown state envelope")
	}
	if len(f.Routes) != len(f.State.Simulation.Vehicles) || len(f.Routes) > project.MaxPods || len(f.State.Simulation.Berths) > project.MaxNodes {
		return StreamFrame{}, errors.New("invalid presentation counts")
	}
	for i, v := range f.State.Simulation.Vehicles {
		if len(v.RouteLaneIDs) != 0 || len(f.Routes[i].Display) > project.MaxLanes || len(f.Routes[i].Lanes) > sim.MotionRouteLimit {
			return StreamFrame{}, errors.New("unbounded stream route")
		}
	}
	return f, nil
}

func decodeStreamJSON(data []byte, target any) error {
	if len(data) > MaxStreamJSON {
		return errors.New("state JSON too large")
	}
	if !jsontext.Value(data).IsValid() {
		return errors.New("invalid or duplicate state JSON")
	}
	// The streaming decoder removes outer whitespace before typed decoding.
	// Keep its offsets and legacy options without the extra input buffer.
	return jsonv2.Unmarshal(bytes.TrimSpace(data), target, json.DefaultOptionsV1(), jsonv2.RejectUnknownMembers(true))
}

// DecodeStreamJSON validates one inflated envelope.
func DecodeStreamJSON(data []byte) (StreamEnvelope, error) {
	var e StreamEnvelope
	err := decodeStreamJSON(data, &e)
	return e, err
}

func encodeStream(e StreamEnvelope) ([]byte, error) {
	return new(streamEncoder).encode(e)
}

// streamEncoder belongs to one publication goroutine.
// The writer keeps the buffer address, but each call releases its backing array.
type streamEncoder struct {
	writer *gzip.Writer
	output bytes.Buffer
}

func (c *streamEncoder) encode(e StreamEnvelope) ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxStreamJSON {
		return nil, errors.New("state JSON exceeds supported limit")
	}
	return c.compressJSON(data)
}

func compressStreamJSON(data []byte) ([]byte, error) {
	return new(streamEncoder).compressJSON(data)
}

func (c *streamEncoder) compressJSON(data []byte) ([]byte, error) {
	if len(data) > MaxStreamJSON {
		return nil, errors.New("state JSON exceeds supported limit")
	}
	defer func() { c.output = bytes.Buffer{} }()
	if c.writer == nil {
		c.writer, _ = gzip.NewWriterLevel(&c.output, gzip.BestSpeed)
	} else {
		c.writer.Reset(&c.output)
	}
	if _, err := c.writer.Write(data); err != nil {
		return nil, err
	}
	if err := c.writer.Close(); err != nil {
		return nil, err
	}
	if c.output.Len() > MaxStreamMessage {
		return nil, errors.New("compressed state exceeds supported limit")
	}
	return slices.Clone(c.output.Bytes()), nil
}

// InflateStream accepts exactly one gzip member with bounded output.
func InflateStream(data []byte) ([]byte, error) {
	if len(data) > MaxStreamMessage {
		return nil, errors.New("compressed state too large")
	}
	input := bytes.NewReader(data)
	z, err := gzip.NewReader(input)
	if err != nil {
		return nil, err
	}
	defer func() { _ = z.Close() }()
	z.Multistream(false)
	output, err := io.ReadAll(io.LimitReader(z, MaxStreamJSON+1))
	if err != nil {
		return nil, err
	}
	if len(output) > MaxStreamJSON || input.Len() != 0 {
		return nil, errors.New("invalid state gzip size or trailing member")
	}
	return output, nil
}

// ParseStreamSequence accepts the canonical decimal sequence representation.
func ParseStreamSequence(s string) (uint64, error) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || strconv.FormatUint(n, 10) != s {
		return 0, errors.New("invalid stream sequence")
	}
	return n, nil
}
