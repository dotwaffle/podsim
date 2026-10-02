package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
)

// backgroundChange distinguishes a removal from a project-only proposal.
type backgroundChange struct {
	Value any `json:"value"`
}

func editBackground(draft any, raw jsontext.Value) (projectChange, error) {
	var command map[string]any
	if raw.Kind() != '{' || json.Unmarshal(raw, &command) != nil {
		return projectChange{}, errors.New("a background edit needs a command object")
	}
	action := text(command["action"])
	allowed := map[string]bool{"action": true, "background": true}
	var fields []string
	switch action {
	case "initialize":
		fields = []string{"imageKey", "width", "height"}
	case "place":
		fields = []string{"imageKey", "frame", "choice"}
	case "calibrate":
		fields = []string{"a", "b", "meters"}
	case "opacity":
		fields = []string{"opacity"}
	case "detach", "remove":
	default:
		return projectChange{}, errors.New("unknown background action")
	}
	for _, field := range fields {
		allowed[field] = true
	}
	for key := range command {
		if !allowed[key] {
			return projectChange{}, fmt.Errorf("the background action does not accept %s", key)
		}
	}
	change := projectChange{Patch: make(map[string]any), Background: &backgroundChange{}}
	if action == "remove" {
		return change, nil
	}
	background := object(command["background"])
	if command["background"] != nil && background == nil {
		return projectChange{}, editorMessageError("The background must be an object.")
	}
	var next map[string]any
	var err error
	switch action {
	case "initialize", "place":
		next, err = newBackground(draft, command, &change)
	case "detach":
		if background != nil {
			next = object(cloneEditValue(background))
		}
		if member(next, "frameState") == "attached" {
			next["frameState"] = "detached"
		}
	case "opacity", "calibrate":
		if background == nil {
			return projectChange{}, errors.New("the draft has no background to edit")
		}
		next = object(cloneEditValue(background))
		if action == "opacity" {
			next["opacity"] = command["opacity"]
		} else {
			err = calibrateBackground(next, command)
		}
	}
	if err != nil {
		return projectChange{}, err
	}
	if next != nil {
		if err := backgroundPlacementError(next); err != nil {
			return projectChange{}, err
		}
		change.Background.Value = next
	}
	return change, nil
}

func newBackground(draft any, command map[string]any, change *projectChange) (map[string]any, error) {
	key := text(command["imageKey"])
	if len(key) != 32 {
		return nil, errors.New("a background needs a valid image key")
	}
	for _, digit := range key {
		if (digit < '0' || digit > '9') && (digit < 'a' || digit > 'f') {
			return nil, errors.New("a background needs a valid image key")
		}
	}
	opacity := .45
	if command["background"] != nil {
		value := member(command["background"], "opacity")
		if !finite(value) {
			return nil, editorMessageError("The background opacity value is invalid.")
		}
		opacity = number(value)
	}
	next := map[string]any{"imageKey": key, "x": 0.0, "y": 0.0, "opacity": opacity, "frameState": "none"}
	if command["action"] == "initialize" {
		width, height := command["width"], command["height"]
		if !finite(width) || !finite(height) || number(width) <= 0 || number(height) <= 0 || math.Trunc(number(width)) != number(width) || math.Trunc(number(height)) != number(height) {
			return nil, errors.New("the decoded image needs positive integer dimensions")
		}
		if number(width) > 16384 || number(height) > 16384 || number(width)*number(height) > 64*1024*1024 {
			return nil, errors.New("the decoded image exceeds the existing pixel limits")
		}
		next["width"], next["height"] = width, height
		return next, nil
	}
	frame := command["frame"]
	if err := backgroundFrameError(frame); err != nil {
		return nil, err
	}
	geo, note, err := backgroundReference(draft, frame, command["choice"])
	if err != nil {
		return nil, err
	}
	setMapBranch(change, draft, "geo", cloneEditValue(geo))
	change.Note = note
	x, y := referencePoint(geo, number(member(frame, "north")), number(member(frame, "west")))
	farX, farY := referencePoint(geo, number(member(frame, "south")), number(member(frame, "east")))
	next["x"], next["y"], next["width"], next["height"] = x, y, farX-x, farY-y
	next["frameState"] = "attached"
	return next, nil
}

func backgroundFrameError(frame any) error {
	values := object(frame)
	if values == nil {
		return editorMessageError("The frame must be an object.")
	}
	for key := range values {
		switch key {
		case "south", "north", "west", "east", "source":
		default:
			return editorMessageError("The frame has an unknown member.")
		}
	}
	if source := values["source"]; source != "equirectangular" && source != "web-mercator" {
		return editorMessageError(`The frame source must be "equirectangular" or "web-mercator".`)
	}
	for _, key := range []string{"south", "north", "west", "east"} {
		if !finite(values[key]) {
			return editorMessageError("The frame bounds must be numbers.")
		}
	}
	return referenceFrameBounds(number(values["south"]), number(values["north"]), number(values["west"]), number(values["east"]))
}

func backgroundReference(draft, frame, choice any) (any, string, error) {
	geo := member(draft, "geo")
	note := ""
	if !editorTruthy(geo) {
		mode := text(member(choice, "mode"))
		switch {
		case len(items(member(member(draft, "network"), "Nodes"))) == 0 || mode == "adopt" && member(choice, "confirmed") == true:
			geo = makeEditorGeo((number(member(frame, "south"))+number(member(frame, "north")))/2, (number(member(frame, "west"))+number(member(frame, "east")))/2)
		case mode == "adopt":
			return nil, "", editorMessageError("Confirm that the image center becomes the reference, and that the network does not move.")
		case mode == "anchor":
			a, err := mapAnchor(draft, member(choice, "a"))
			if err != nil {
				return nil, "", err
			}
			b, err := mapAnchor(draft, member(choice, "b"))
			if err != nil {
				return nil, "", err
			}
			geo, err = anchoredReference(a, b)
			if err != nil {
				return nil, "", err
			}
			x, y := referencePoint(geo, b.latitude, b.longitude)
			note = fmt.Sprintf("The second anchor is %.1f m from its node.", math.Hypot(x-b.x, y-b.y))
		default:
			return nil, "", editorMessageError("The project has nodes and no geographic reference. Anchor two nodes, or adopt the image center.")
		}
	}
	if problem := draftGeoError(geo); problem != "" {
		return nil, "", errors.New(problem)
	}
	if err := referencePlacement(geo, number(member(frame, "south")), number(member(frame, "north")), number(member(frame, "west")), number(member(frame, "east"))); err != nil {
		return nil, "", err
	}
	return geo, note, nil
}

func backgroundPlacementError(background any) error {
	for _, key := range []string{"x", "y", "width", "height", "opacity"} {
		if !finite(member(background, key)) {
			return editorMessageError(fmt.Sprintf("The background %s value is invalid.", key))
		}
	}
	if number(member(background, "width")) <= 0 || number(member(background, "height")) <= 0 || number(member(background, "opacity")) < 0 || number(member(background, "opacity")) > 1 {
		return editorMessageError("The background dimensions or opacity are invalid.")
	}
	return nil
}

func calibrateBackground(background, command map[string]any) error {
	a, b, meters := command["a"], command["b"], command["meters"]
	if !finite(member(a, "X")) || !finite(member(a, "Y")) || !finite(member(b, "X")) || !finite(member(b, "Y")) || !finite(meters) || number(meters) <= 0 {
		return editorMessageError("Enter a positive distance and select two different points.")
	}
	distance := math.Hypot(number(member(b, "X"))-number(member(a, "X")), number(member(b, "Y"))-number(member(a, "Y")))
	if distance <= 0 || math.IsInf(distance, 0) {
		return editorMessageError("Enter a positive distance and select two different points.")
	}
	if background["frameState"] == "attached" {
		return editorMessageError("Detach the frame before you calibrate the scale.")
	}
	if err := backgroundPlacementError(background); err != nil {
		return err
	}
	factor := number(meters) / distance
	background["x"] = number(member(a, "X")) + (number(background["x"])-number(member(a, "X")))*factor
	background["y"] = number(member(a, "Y")) + (number(background["y"])-number(member(a, "Y")))*factor
	background["width"], background["height"] = number(background["width"])*factor, number(background["height"])*factor
	return nil
}
