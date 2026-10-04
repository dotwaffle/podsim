package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

type couplingEdit struct {
	Action string         `json:"action"`
	ID     string         `json:"id,omitempty"`
	Field  string         `json:"field,omitempty"`
	Value  jsontext.Value `json:"value,omitempty"`
	LaneID string         `json:"laneId,omitempty"`
	Index  *int           `json:"index,omitempty"`
	Count  *int           `json:"count,omitempty"`
}

// couplingEditFields lists the command members of each action, after the action.
var couplingEditFields = map[string][]string{
	"addSite":            {"laneId"},
	"setSite":            {"id", "field", "value"},
	"setSiteLane":        {"id", "laneId"},
	"removeSite":         {"id"},
	"addCorridor":        {},
	"setCorridor":        {"id", "field", "value"},
	"addCorridorLane":    {"id", "laneId"},
	"removeCorridorLane": {"id", "index", "count", "laneId"},
	"removeCorridor":     {"id"},
}

// editCoupling proposes one change to the sites or corridors of a version 5
// project. It does not check the geometry. The draft checks show the native
// geometry verdict.
func editCoupling(draft any, raw jsontext.Value) (projectChange, error) {
	command, err := decodeCouplingEdit(raw)
	if err != nil {
		return projectChange{}, err
	}
	if number(member(draft, "version")) != project.CouplingVersion {
		return projectChange{}, errors.New("coupling sites need project version 5, convert the project to trains first")
	}
	sites, corridors := member(draft, "couplingSites"), member(draft, "couplingCorridors")
	if _, valid := sites.([]any); has(draft, "couplingSites") && !valid {
		return projectChange{}, errors.New("coupling sites must be an array")
	}
	if _, valid := corridors.([]any); has(draft, "couplingCorridors") && !valid {
		return projectChange{}, errors.New("coupling corridors must be an array")
	}
	edit := couplingRegistries{draft: draft, sites: items(cloneEditValue(sites)), corridors: items(cloneEditValue(corridors))}
	if err := edit.apply(command); err != nil {
		return projectChange{}, err
	}
	change := projectChange{Patch: make(map[string]any)}
	if !reflect.DeepEqual(items(sites), edit.sites) {
		change.Patch["couplingSites"] = edit.sites
	}
	if !reflect.DeepEqual(items(corridors), edit.corridors) {
		change.Patch["couplingCorridors"] = edit.corridors
	}
	return change, nil
}

func decodeCouplingEdit(raw jsontext.Value) (couplingEdit, error) {
	if raw.Kind() != '{' {
		return couplingEdit{}, errors.New("a coupling edit needs a command object")
	}
	var command couplingEdit
	if err := json.Unmarshal(raw, &command, json.RejectUnknownMembers(true)); err != nil {
		return command, fmt.Errorf("decode coupling edit: %w", err)
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(raw, &fields); err != nil {
		return command, fmt.Errorf("decode coupling edit fields: %w", err)
	}
	allowed, known := couplingEditFields[command.Action]
	if !known {
		return command, errors.New("unknown coupling edit action")
	}
	for key, value := range fields {
		if key != "action" && !slices.Contains(allowed, key) {
			return command, fmt.Errorf("the coupling action does not accept %s", key)
		}
		if value.Kind() == 'n' {
			return command, fmt.Errorf("coupling edit field %s must not be null", key)
		}
	}
	if len(fields) != len(allowed)+1 {
		return command, errors.New("the coupling edit misses a field")
	}
	return command, nil
}

type couplingRegistries struct {
	draft            any
	sites, corridors []any
}

func (r *couplingRegistries) apply(command couplingEdit) error {
	switch command.Action {
	case "addSite":
		return r.addSite(command.LaneID)
	case "addCorridor":
		return r.addCorridor()
	case "removeSite":
		index, err := r.find(r.sites, command.ID)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(r.corridors, func(corridor any) bool {
			return text(member(corridor, "assemblySiteId")) == command.ID || text(member(corridor, "splitSiteId")) == command.ID
		}) {
			return errors.New("a corridor uses this coupling site, change or remove the corridor first")
		}
		r.sites = slices.Delete(r.sites, index, index+1)
		return nil
	case "removeCorridor":
		index, err := r.find(r.corridors, command.ID)
		if err != nil {
			return err
		}
		r.corridors = slices.Delete(r.corridors, index, index+1)
		return nil
	}
	if command.Action == "setSite" || command.Action == "setSiteLane" {
		index, err := r.find(r.sites, command.ID)
		if err != nil {
			return err
		}
		return r.setSite(object(r.sites[index]), command)
	}
	index, err := r.find(r.corridors, command.ID)
	if err != nil {
		return err
	}
	return r.changeCorridor(object(r.corridors[index]), command)
}

func (r *couplingRegistries) find(rows []any, id string) (int, error) {
	index := slices.IndexFunc(rows, func(row any) bool { return text(member(row, "id")) == id })
	if index < 0 || object(rows[index]) == nil {
		return 0, errors.New("the coupling row no longer exists")
	}
	return index, nil
}

// checkLane gives an error when the draft has no lane with the ID.
func (r *couplingRegistries) checkLane(id string) error {
	for _, lane := range items(member(member(r.draft, "network"), "Lanes")) {
		if text(member(lane, "ID")) == id && id != "" {
			return nil
		}
	}
	return errors.New("the selected guideway no longer exists")
}

// addSite adds a site on the lane with the positions of couplingSiteDefaults.
// The draft checks report a lane that is too short.
func (r *couplingRegistries) addSite(laneID string) error {
	if len(r.sites) >= sim.MaxCouplingSites {
		return fmt.Errorf("a project can have at most %d coupling sites", sim.MaxCouplingSites)
	}
	if err := r.checkLane(laneID); err != nil {
		return err
	}
	room, err := sim.CouplingSiteRoom(sim.CompactPairV1CouplingContract)
	if err != nil {
		return err
	}
	site := couplingSiteDefaults(room)
	site["id"], site["laneId"] = nextCouplingID(r.sites, "site-"), laneID
	r.sites = append(r.sites, site)
	return nil
}

// couplingSiteDefaults gives the positions of a new site that starts at 0 m.
// The rear staging point and the end are the smallest values in steps of
// 0.01 m that room allows, so the site rows show short decimals. The front
// staging point is the staging spacing after the rear staging point.
func couplingSiteDefaults(room sim.CouplingRoom) map[string]any {
	const start = 0.0
	rear := ceilCentimeter(start + room.BoundaryMarginMeters)
	front := math.Round((rear+room.StagingSpacingMeters)*100) / 100
	end := ceilCentimeter(max(start+room.RequiredLengthMeters, front+room.OpeningTravelMeters+room.BoundaryMarginMeters))
	return map[string]any{"startMeters": start, "endMeters": end, "frontStagingMeters": front, "rearStagingMeters": rear}
}

// ceilCentimeter gives the smallest value in steps of 0.01 m that is not
// less than meters.
func ceilCentimeter(meters float64) float64 {
	return math.Ceil(meters*100) / 100
}

// addCorridor adds a corridor from the first site to the second site. Its
// path holds the lane of the first site. The editor then adds the next
// lanes of the path in order.
func (r *couplingRegistries) addCorridor() error {
	if len(r.corridors) >= sim.MaxCouplingCorridors {
		return fmt.Errorf("a project can have at most %d coupling corridors", sim.MaxCouplingCorridors)
	}
	if len(r.sites) < 2 {
		return errors.New("add two coupling sites before a corridor")
	}
	first, second := r.sites[0], r.sites[1]
	r.corridors = append(r.corridors, map[string]any{
		"id": nextCouplingID(r.corridors, "corridor-"), "assemblySiteId": text(member(first, "id")),
		"splitSiteId": text(member(second, "id")), "laneIds": []any{text(member(first, "laneId"))},
	})
	return nil
}

func (r *couplingRegistries) setSite(site map[string]any, command couplingEdit) error {
	if command.Action == "setSiteLane" {
		if err := r.checkLane(command.LaneID); err != nil {
			return err
		}
		site["laneId"] = command.LaneID
		return nil
	}
	if !slices.Contains([]string{"startMeters", "endMeters", "frontStagingMeters", "rearStagingMeters"}, command.Field) {
		return errors.New("unknown coupling site field")
	}
	var value any
	if err := json.Unmarshal(command.Value, &value); err != nil {
		return fmt.Errorf("decode coupling site value: %w", err)
	}
	if input, ok := value.(string); ok && trimEditorSpace(input) == "" {
		return errors.New("a coupling site position must not be empty")
	}
	x, err := editNumber(value)
	if err != nil {
		return err
	}
	site[command.Field] = x
	return nil
}

func (r *couplingRegistries) changeCorridor(corridor map[string]any, command couplingEdit) error {
	lanes, valid := corridor["laneIds"].([]any)
	if !valid && command.Action != "setCorridor" {
		return errors.New("the corridor path must be an array")
	}
	switch command.Action {
	case "setCorridor":
		if command.Field != "assemblySiteId" && command.Field != "splitSiteId" {
			return errors.New("unknown coupling corridor field")
		}
		var id string
		if err := json.Unmarshal(command.Value, &id); err != nil {
			return errors.New("a corridor site must be text")
		}
		if _, err := r.find(r.sites, id); err != nil {
			return err
		}
		corridor[command.Field] = id
	case "addCorridorLane":
		if len(lanes) >= project.MaxLanes {
			return fmt.Errorf("a corridor path can have at most %d guideways", project.MaxLanes)
		}
		if err := r.checkLane(command.LaneID); err != nil {
			return err
		}
		corridor["laneIds"] = append(lanes, command.LaneID)
	case "removeCorridorLane":
		// The count and the guideway ID are those that the row shows.
		if *command.Count != len(lanes) || *command.Index < 0 || *command.Index >= len(lanes) || lanes[*command.Index] != command.LaneID {
			return errors.New("the corridor path changed, enter the edit again")
		}
		corridor["laneIds"] = slices.Delete(lanes, *command.Index, *command.Index+1)
	}
	return nil
}

func nextCouplingID(rows []any, prefix string) string {
	for next := 1; ; next++ {
		candidate := prefix + strconv.Itoa(next)
		if !slices.ContainsFunc(rows, func(row any) bool { return text(member(row, "id")) == candidate }) {
			return candidate
		}
	}
}
