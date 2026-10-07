// Package parkride tracks offline car journeys around native pod trips.
package parkride

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"

	"github.com/dotwaffle/podsim/internal/sim"
)

// MaxPlanBytes is the largest plan input, and the largest canonical plan
// of a checkpoint.
const MaxPlanBytes = 10 << 20

const (
	storageLimit   = 256 << 20
	itineraryBytes = 4096
	lotBytes       = 512
	// MaxHorizonTicks retains the existing 24-hour offline run bound.
	MaxHorizonTicks = 86400 * sim.TicksPerSecond
	// MaxQueueLimit retains the existing offline pending-order bound.
	MaxQueueLimit = 1_000_000
)

// Lot declares car capacity independently of pod berths.
type Lot struct {
	ID       string `json:"id"`
	Hub      string `json:"hub"`
	Capacity int64  `json:"capacity"`
}

// Itinerary describes one car and one immutable party's paired pod legs.
type Itinerary struct {
	ID                     string             `json:"id"`
	CarID                  string             `json:"carID"`
	Lot                    string             `json:"lot"`
	Destination            string             `json:"destination"`
	CarSeats               int64              `json:"carSeats"`
	PartySize              int                `json:"partySize"`
	SharingConsent         sim.SharingConsent `json:"sharingConsent"`
	DepartureSeconds       int64              `json:"departureSeconds"`
	OutwardSeconds         int64              `json:"outwardSeconds"`
	ReturnNotBeforeSeconds int64              `json:"returnNotBeforeSeconds"`
	ActivitySeconds        int64              `json:"activitySeconds"`
	RetrievalSeconds       int64              `json:"retrievalSeconds"`
	HomeboundSeconds       int64              `json:"homeboundSeconds"`
	OutwardRefusal         string             `json:"outwardRefusal"`
	ReturnRefusal          string             `json:"returnRefusal"`
}

// Plan is a finite batch. It never repeats at a daily boundary.
type Plan struct {
	Lots        []Lot       `json:"lots"`
	Itineraries []Itinerary `json:"itineraries"`
}

func (p Plan) clone() Plan {
	p.Lots, p.Itineraries = slices.Clone(p.Lots), slices.Clone(p.Itineraries)
	return p
}

// DecodePlan rejects ambiguous input before allocating typed plan records.
func DecodePlan(data []byte, network sim.Network) (Plan, error) {
	if len(data) > MaxPlanBytes {
		return Plan{}, errors.New("plan exceeds the 10 MiB input bound")
	}
	if err := scanStorage(data); err != nil {
		return Plan{}, err
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(data, &fields); err != nil {
		return Plan{}, err
	}
	if err := requireFields(fields, "lots", "itineraries"); err != nil {
		return Plan{}, err
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan, json.RejectUnknownMembers(true)); err != nil {
		return Plan{}, err
	}
	for _, name := range []string{"lots", "itineraries"} {
		var entries []map[string]jsontext.Value
		if err := json.Unmarshal(fields[name], &entries); err != nil {
			return Plan{}, err
		}
		for _, entry := range entries {
			var required []string
			if name == "lots" {
				required = []string{"id", "hub", "capacity"}
			} else {
				required = []string{"id", "carID", "lot", "destination", "carSeats", "partySize", "departureSeconds", "outwardSeconds", "returnNotBeforeSeconds", "activitySeconds", "retrievalSeconds", "homeboundSeconds", "outwardRefusal", "returnRefusal"}
			}
			if err := requireFields(entry, required...); err != nil {
				return Plan{}, err
			}
			if value, ok := entry["sharingConsent"]; ok && (bytes.Equal(bytes.TrimSpace(value), []byte("null")) || bytes.Equal(bytes.TrimSpace(value), []byte(`""`))) {
				return Plan{}, errors.New("explicit sharing consent must be private or shared")
			}
		}
	}
	return normalizePlan(plan, network)
}

func requireFields(fields map[string]jsontext.Value, names ...string) error {
	for _, name := range names {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("plan needs nonnull %s", name)
		}
	}
	return nil
}

func scanStorage(data []byte) error {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	lots, itineraries := int64(0), int64(0)
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		path := strings.Split(string(decoder.StackPointer()), "/")
		if len(path) == 3 && token.Kind() != jsontext.KindEndObject && token.Kind() != jsontext.KindEndArray {
			switch path[1] {
			case "lots":
				lots++
			case "itineraries":
				itineraries++
			}
			if !fitsStorage(lots, itineraries) {
				return errors.New("plan exceeds the 256 MiB retained storage bound")
			}
		}
	}
}

func fitsStorage(lots, itineraries int64) bool {
	return lots >= 0 && itineraries >= 0 && lots <= storageLimit/lotBytes && itineraries <= (storageLimit-lots*lotBytes)/itineraryBytes
}

func normalizePlan(plan Plan, network sim.Network) (Plan, error) {
	if !fitsStorage(int64(len(plan.Lots)), int64(len(plan.Itineraries))) {
		return Plan{}, errors.New("plan exceeds the retained storage bound")
	}
	plan = plan.clone()
	stations := make(map[string]bool)
	for _, station := range network.Stations {
		stations[station.ID] = !station.ParkingOnly
	}
	lots := make(map[string]Lot, len(plan.Lots))
	for _, lot := range plan.Lots {
		if _, duplicate := lots[lot.ID]; !validID(lot.ID) || duplicate || !stations[lot.Hub] || lot.Capacity < 0 {
			return Plan{}, fmt.Errorf("invalid or duplicate lot %q", lot.ID)
		}
		lots[lot.ID] = lot
	}
	ids, cars := make(map[string]bool), make(map[string]bool)
	for index := range plan.Itineraries {
		itinerary := &plan.Itineraries[index]
		lot, exists := lots[itinerary.Lot]
		if !validID(itinerary.ID) || ids[itinerary.ID] || !validID(itinerary.CarID) || cars[itinerary.CarID] || !exists || !stations[itinerary.Destination] || itinerary.Destination == lot.Hub {
			return Plan{}, fmt.Errorf("invalid itinerary %q or car identity", itinerary.ID)
		}
		if itinerary.CarSeats < 1 || itinerary.PartySize < 1 || itinerary.PartySize > sim.MaxNewPartySize || int64(itinerary.PartySize) > itinerary.CarSeats {
			return Plan{}, fmt.Errorf("itinerary %q party must fit its car and native admission limits", itinerary.ID)
		}
		if itinerary.SharingConsent == "" {
			itinerary.SharingConsent = sim.PrivateConsent
		}
		if itinerary.SharingConsent != sim.PrivateConsent && itinerary.SharingConsent != sim.SharedConsent {
			return Plan{}, errors.New("sharing consent must be private or shared")
		}
		if itinerary.OutwardRefusal != "drive-home" || itinerary.ReturnRefusal != "retain-car" {
			return Plan{}, errors.New("author outward drive-home and return retain-car refusal policies")
		}
		if _, err := compileTimes(*itinerary); err != nil {
			return Plan{}, fmt.Errorf("itinerary %q: %w", itinerary.ID, err)
		}
		ids[itinerary.ID], cars[itinerary.CarID] = true, true
	}
	slices.SortFunc(plan.Lots, func(a, b Lot) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(plan.Itineraries, func(a, b Itinerary) int { return strings.Compare(a.ID, b.ID) })
	return plan, nil
}

// validID reports whether id has 1 to 64 bytes, each one of
// sim.IDCharacters.
func validID(id string) bool { return id != "" && len(id) <= 64 && sim.ValidIDText(id) }

type times struct{ departure, arrival, notBefore, activity, retrieval, homebound int64 }

func compileTimes(itinerary Itinerary) (times, error) {
	values := []int64{itinerary.DepartureSeconds, itinerary.OutwardSeconds, itinerary.ReturnNotBeforeSeconds, itinerary.ActivitySeconds, itinerary.RetrievalSeconds, itinerary.HomeboundSeconds}
	for index, seconds := range values {
		if seconds < 0 || seconds > math.MaxInt64/sim.TicksPerSecond {
			return times{}, errors.New("time cannot convert to simulation ticks")
		}
		values[index] *= sim.TicksPerSecond
	}
	arrival, err := addTicks(values[0], values[1])
	if err != nil {
		return times{}, err
	}
	// Prove every dynamic chain remains representable through the run horizon.
	latest, err := addTicks(max(int64(MaxHorizonTicks), arrival), values[3])
	if err != nil {
		return times{}, err
	}
	latest, err = addTicks(max(latest, values[2]), values[4])
	if err != nil {
		return times{}, err
	}
	if _, err = addTicks(latest, values[5]); err != nil {
		return times{}, err
	}
	return times{values[0], arrival, values[2], values[3], values[4], values[5]}, nil
}

func addTicks(a, b int64) (int64, error) {
	if a < 0 || b < 0 || b > math.MaxInt64-a {
		return 0, errors.New("simulation tick sum overflows")
	}
	return a + b, nil
}

func canonicalHash(value any) (string, error) {
	data, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
