package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"reflect"

	"github.com/dotwaffle/podsim/internal/project"
)

// editorMessageError preserves the existing browser message for a rejected edit.
type editorMessageError string

func (e editorMessageError) Error() string { return string(e) }

func editMap(draft any, raw jsontext.Value) (projectChange, error) {
	var command map[string]any
	if raw.Kind() != '{' || json.Unmarshal(raw, &command) != nil {
		return projectChange{}, errors.New("a map edit needs a command object")
	}
	action := text(command["action"])
	allowed := map[string]bool{"action": true}
	switch action {
	case "enable":
		for _, key := range []string{"latitude", "longitude", "opacity", "choice"} {
			allowed[key] = true
		}
	case "opacity":
		allowed["opacity"] = true
	case "remove":
	default:
		return projectChange{}, errors.New("unknown map action")
	}
	for key := range command {
		if !allowed[key] {
			return projectChange{}, fmt.Errorf("the map action does not accept %s", key)
		}
	}
	change := projectChange{Patch: make(map[string]any)}
	if action == "remove" {
		if has(object(draft), "map") {
			// The browser deletes the map branch when it accepts this action.
			change.Patch["map"] = nil
		}
		return change, nil
	}
	opacity := command["opacity"]
	if action == "enable" && opacity == nil {
		opacity = .45
	}
	if !finite(opacity) || number(opacity) < 0 || number(opacity) > 1 {
		return projectChange{}, editorMessageError("The map settings are invalid.")
	}
	if action == "opacity" {
		background := object(member(draft, "map"))
		if background == nil {
			return projectChange{}, errors.New("the draft has no map to edit")
		}
		owned := object(cloneEditValue(background))
		owned["opacity"] = opacity
		setMapBranch(&change, draft, "map", owned)
		return change, nil
	}
	geo := member(draft, "geo")
	if !editorTruthy(geo) {
		var err error
		geo, err = mapReference(draft, command)
		if err != nil {
			return projectChange{}, err
		}
		setMapBranch(&change, draft, "geo", geo)
	}
	if draftGeoError(geo) != "" {
		return projectChange{}, editorMessageError("The map settings are invalid.")
	}
	setMapBranch(&change, draft, "map", map[string]any{"provider": "osm", "opacity": opacity})
	return change, nil
}

func setMapBranch(change *projectChange, draft any, key string, value any) {
	if !reflect.DeepEqual(member(draft, key), value) {
		change.Patch[key] = value
	}
}

func makeEditorGeo(latitude, longitude float64) map[string]any {
	return map[string]any{"latitude": latitude, "longitude": longitude, "projection": project.GeoProjection, "radius": float64(project.GeoRadius)}
}

func mapReference(draft any, command map[string]any) (map[string]any, error) {
	choice := object(command["choice"])
	mode := text(choice["mode"])
	if len(items(member(member(draft, "network"), "Nodes"))) == 0 || mode == "adopt" && choice["confirmed"] == true {
		geo := makeEditorGeo(number(command["latitude"]), number(command["longitude"]))
		if problem := draftGeoError(geo); problem != "" {
			return nil, errors.New(problem)
		}
		return geo, nil
	}
	if mode != "anchor" {
		return nil, editorMessageError("Anchor two nodes, or confirm the map origin. Network positions will not change.")
	}
	a, err := mapAnchor(draft, choice["a"])
	if err != nil {
		return nil, err
	}
	b, err := mapAnchor(draft, choice["b"])
	if err != nil {
		return nil, err
	}
	geo, err := anchoredReference(a, b)
	if err != nil {
		return nil, err
	}
	// The existing tile anchor checks a small frame around the first anchor.
	latitude, longitude := number(member(choice["a"], "latitude")), number(member(choice["a"], "longitude"))
	south, north := max(-80, latitude-.00001), min(80, latitude+.00001)
	west, east := max(-180, longitude-.00001), min(180, longitude+.00001)
	if err := referenceFrameBounds(south, north, west, east); err != nil {
		return nil, err
	}
	if err := referencePlacement(geo, south, north, west, east); err != nil {
		return nil, err
	}
	return geo, nil
}

type editorAnchor struct {
	x, y, latitude, longitude float64
}

func mapAnchor(draft, value any) (editorAnchor, error) {
	id := text(member(value, "id"))
	for _, node := range items(member(member(draft, "network"), "Nodes")) {
		if member(node, "ID") != id {
			continue
		}
		if !finite(member(value, "latitude")) || !finite(member(value, "longitude")) {
			return editorAnchor{}, editorMessageError("Each anchor needs a latitude and a longitude in degrees.")
		}
		position := member(node, "Position")
		if !finite(member(position, "X")) || !finite(member(position, "Y")) {
			return editorAnchor{}, errors.New("the anchor node needs finite coordinates")
		}
		return editorAnchor{number(member(position, "X")), number(member(position, "Y")), number(member(value, "latitude")), number(member(value, "longitude"))}, nil
	}
	return editorAnchor{}, editorMessageError(fmt.Sprintf("The anchor node %.40q is not in the project.", id))
}

func anchoredReference(a, b editorAnchor) (map[string]any, error) {
	degree := math.Pi / 180
	distance := math.Hypot(b.x-a.x, b.y-a.y)
	latitude := a.latitude + a.y/(project.GeoRadius*degree)
	geo := makeEditorGeo(latitude, a.longitude-a.x/(project.GeoRadius*math.Cos(latitude*degree)*degree))
	x, y := referencePoint(geo, b.latitude, b.longitude)
	residual := math.Hypot(x-b.x, y-b.y)
	if problem := draftGeoError(geo); problem != "" {
		return nil, errors.New(problem)
	}
	if distance < 100 {
		return nil, editorMessageError("The anchor nodes must be at least 100 m apart.")
	}
	if residual > .02*distance {
		return nil, editorMessageError(fmt.Sprintf("The second anchor is %.1f m from its node, more than 2%% of the %.1f m between the nodes.", residual, distance))
	}
	return geo, nil
}

func referencePoint(geo any, latitude, longitude float64) (float64, float64) {
	degree := math.Pi / 180
	lat, lon, radius := number(member(geo, "latitude")), number(member(geo, "longitude")), number(member(geo, "radius"))
	return radius * math.Cos(lat*degree) * (longitude - lon) * degree, -radius * (latitude - lat) * degree
}

func referenceFrameBounds(south, north, west, east float64) error {
	if !finiteRange(south, -80, 80) || !finiteRange(north, -80, 80) {
		return editorMessageError("The frame latitudes must be from -80 to 80 degrees.")
	}
	if south >= north {
		return editorMessageError("The south edge of the frame must be south of the north edge.")
	}
	if !finiteRange(west, -180, 180) || !finiteRange(east, -180, 180) {
		return editorMessageError("The frame longitudes must be from -180 to 180 degrees.")
	}
	if west >= east {
		return editorMessageError("The west edge of the frame must be west of the east edge. The frame cannot cross the antimeridian.")
	}
	return nil
}

func referencePlacement(geo any, south, north, west, east float64) error {
	edges := []float64{south, north}
	if south < 0 && north > 0 {
		edges = append(edges, 0)
	}
	latitude, worst := number(member(geo, "latitude")), 0.0
	for _, edge := range edges {
		worst = max(worst, math.Abs(math.Cos(latitude*math.Pi/180)/math.Cos(edge*math.Pi/180)-1))
	}
	if worst > .005 {
		return editorMessageError(fmt.Sprintf("The frame is too far north or south of the reference latitude %v. The east-west scale differs by %.2f%%, and the limit is 0.5%%.", latitude, worst*100))
	}
	x, y := referencePoint(geo, north, west)
	x2, y2 := referencePoint(geo, south, east)
	for _, coordinate := range []float64{x, y, x2, y2} {
		if !finiteRange(coordinate, -100000, 100000) {
			return editorMessageError("The frame is more than 100000 m from the reference of the project.")
		}
	}
	return nil
}
