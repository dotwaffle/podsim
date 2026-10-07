package project

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strconv"
)

// MaxFlowValues is the largest number of values in the JSON array of one
// flow: the two station indexes and one weight for each band.
const MaxFlowValues = 2 + MaxBands

// demandProfileJSON is the JSON form of a DemandProfile. Stations lists
// each station that a flow names one time. Each flow is an array of the
// index in Stations of its origin, the index of its destination, and then
// its weights. The decoder reads flows as raw JSON (see decodeFlowRows).
type demandProfileJSON[F any] struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Bands    []DemandBand `json:"bands"`
	Stations []string     `json:"stations"`
	Flows    F            `json:"flows"`
}

// MarshalJSONTo writes the profile with the stations in the order of their
// first use: the origin and then the destination of each flow, in flow
// order. Thus the same profile always gives the same bytes.
func (profile DemandProfile) MarshalJSONTo(encoder *jsontext.Encoder) error {
	rows := flowRows{flows: profile.Flows, index: make(map[string]int)}
	stations := make([]string, 0)
	for _, flow := range profile.Flows {
		for _, id := range [2]string{flow.From, flow.To} {
			if _, ok := rows.index[id]; !ok {
				rows.index[id] = len(stations)
				stations = append(stations, id)
			}
		}
	}
	wire := demandProfileJSON[flowRows]{ID: profile.ID, Name: profile.Name, Bands: profile.Bands, Stations: stations, Flows: rows}
	return jsonv2.MarshalEncode(encoder, &wire)
}

// flowRows writes the flows of a profile as arrays. index gives the
// position of each station in the station list of the profile.
type flowRows struct {
	flows []DemandFlow
	index map[string]int
}

// MarshalJSONTo writes one array for each flow.
func (rows flowRows) MarshalJSONTo(encoder *jsontext.Encoder) error {
	if err := encoder.WriteToken(jsontext.BeginArray); err != nil {
		return err
	}
	for _, flow := range rows.flows {
		if err := writeFlowRow(encoder, rows.index[flow.From], rows.index[flow.To], flow.Weights); err != nil {
			return err
		}
	}
	return encoder.WriteToken(jsontext.EndArray)
}

func writeFlowRow(encoder *jsontext.Encoder, from, to int, weights []float64) error {
	if err := encoder.WriteToken(jsontext.BeginArray); err != nil {
		return err
	}
	if err := encoder.WriteToken(jsontext.Int(int64(from))); err != nil {
		return err
	}
	if err := encoder.WriteToken(jsontext.Int(int64(to))); err != nil {
		return err
	}
	for _, weight := range weights {
		if err := encoder.WriteToken(jsontext.Float(weight)); err != nil {
			return err
		}
	}
	return encoder.WriteToken(jsontext.EndArray)
}

// UnmarshalJSONFrom reads a profile and replaces the station indexes of
// its flows with the station IDs. It refuses a station list with a
// repeated ID, a flow that is not an array that starts with two valid
// station indexes, a weight that is not a number, and then a listed station
// that no flow names, in that order. Validate checks the stations, the
// pairs, the number of weights and the weight values of the flows.
func (profile *DemandProfile) UnmarshalJSONFrom(decoder *jsontext.Decoder) error {
	var wire demandProfileJSON[jsontext.Value]
	if err := jsonv2.UnmarshalDecode(decoder, &wire); err != nil {
		return err
	}
	id := quoteID(wire.ID)
	listed := make(map[string]bool, len(wire.Stations))
	for _, station := range wire.Stations {
		if listed[station] {
			return fmt.Errorf("demand profile %s lists station %s more than once", id, quoteID(station))
		}
		listed[station] = true
	}
	flows, used, err := decodeFlowRows(wire.Flows, wire.Stations, id, decoder.Options())
	if err != nil {
		return err
	}
	for index, station := range wire.Stations {
		if !used[index] {
			return fmt.Errorf("demand profile %s lists station %s that no flow names", id, quoteID(station))
		}
	}
	*profile = DemandProfile{ID: wire.ID, Name: wire.Name, Bands: wire.Bands, Flows: flows}
	return nil
}

// decodeFlowRows reads raw, the flows member of a profile. A missing or
// null member gives no flows. used reports the stations that the flows
// name. A refusal gives the index of the flow in the array.
func decodeFlowRows(raw jsontext.Value, stations []string, id string, options jsontext.Options) (flows []DemandFlow, used []bool, err error) {
	used = make([]bool, len(stations))
	if len(raw) == 0 || raw.Kind() == jsontext.KindNull {
		return nil, used, nil
	}
	if raw.Kind() != jsontext.KindBeginArray {
		return nil, nil, fmt.Errorf("demand profile %s flows must be an array", id)
	}
	decoder := jsontext.NewDecoder(bytes.NewReader(raw), options)
	if _, err := decoder.ReadToken(); err != nil {
		return nil, nil, err
	}
	for row := 0; decoder.PeekKind() != jsontext.KindEndArray; row++ {
		flow, err := decodeFlowRow(decoder, stations, used)
		if err != nil {
			return nil, nil, fmt.Errorf("demand profile %s flow %d %w", id, row, err)
		}
		flows = append(flows, flow)
	}
	return flows, used, nil
}

var (
	errFlowShape  = errors.New("must be an array that starts with two station indexes")
	errFlowIndex  = errors.New("has an invalid station index")
	errFlowWeight = errors.New("has a weight that is not a number")
)

// decodeFlowRow reads one flow array and marks its two stations in used.
func decodeFlowRow(decoder *jsontext.Decoder, stations []string, used []bool) (DemandFlow, error) {
	if decoder.PeekKind() != jsontext.KindBeginArray {
		return DemandFlow{}, errFlowShape
	}
	if _, err := decoder.ReadToken(); err != nil {
		return DemandFlow{}, err
	}
	var ends [2]int
	for end := range ends {
		if decoder.PeekKind() == jsontext.KindEndArray {
			return DemandFlow{}, errFlowShape
		}
		index, err := readStationIndex(decoder, len(stations))
		if err != nil {
			return DemandFlow{}, err
		}
		ends[end] = index
	}
	flow := DemandFlow{From: stations[ends[0]], To: stations[ends[1]], Weights: make([]float64, 0, 8)}
	for decoder.PeekKind() != jsontext.KindEndArray {
		value, err := decoder.ReadValue()
		if err != nil {
			return DemandFlow{}, err
		}
		// ParseFloat refuses each JSON value other than a number, and a
		// number out of the range of a float64.
		weight, err := strconv.ParseFloat(string(value), 64)
		if err != nil {
			return DemandFlow{}, errFlowWeight
		}
		flow.Weights = append(flow.Weights, weight)
	}
	if _, err := decoder.ReadToken(); err != nil {
		return DemandFlow{}, err
	}
	used[ends[0]], used[ends[1]] = true, true
	return flow, nil
}

// readStationIndex reads a station index: a JSON integer from 0 to
// count-1, with no fraction and no exponent.
func readStationIndex(decoder *jsontext.Decoder, count int) (int, error) {
	value, err := decoder.ReadValue()
	if err != nil {
		return 0, err
	}
	// Atoi refuses each JSON value other than an integer, and a number with
	// a fraction or an exponent. A JSON number has no plus sign, and the
	// check of the minus sign also refuses -0.
	index, err := strconv.Atoi(string(value))
	if err != nil || value[0] == '-' || index >= count {
		return 0, errFlowIndex
	}
	return index, nil
}
