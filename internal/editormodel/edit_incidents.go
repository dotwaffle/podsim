package editormodel

import (
	"errors"
	"math"
	"reflect"
)

// incidentField is the place of a number of the Faults and emergencies form
// in the project. key is a member of the branch, or of its duration object.
// A whole number has an int member in the project.
type incidentField struct {
	branch, key     string
	whole, duration bool
}

// incidentFields holds each number field of the form.
var incidentFields = map[string]incidentField{
	"faultEvacuationSeconds":   {branch: "faults", key: "evacuationSeconds", whole: true},
	"faultPerHour":             {branch: "faults", key: "perHour"},
	"faultDebrisShare":         {branch: "faults", key: "debrisShare"},
	"faultDebrisMeters":        {branch: "faults", key: "debrisMeters"},
	"faultDurationSeconds":     {branch: "faults", key: "seconds", whole: true, duration: true},
	"faultDurationMinSeconds":  {branch: "faults", key: "minSeconds", whole: true, duration: true},
	"faultDurationMaxSeconds":  {branch: "faults", key: "maxSeconds", whole: true, duration: true},
	"faultDurationMeanSeconds": {branch: "faults", key: "meanSeconds", whole: true, duration: true},
	"emergencyPerHour":         {branch: "emergencies", key: "perHour"},
}

// faultDurationMembers holds the members of each duration kind.
var faultDurationMembers = map[string][]string{
	"fixed":       {"seconds"},
	"uniform":     {"minSeconds", "maxSeconds"},
	"exponential": {"minSeconds", "maxSeconds", "meanSeconds"},
}

// incidentNumber sets a number of the form. An empty value removes the member,
// so that the member keeps its default. The native validator decides the
// range, so that the checks show the refusals of the server.
func (c *projectChange) incidentNumber(draft any, field incidentField, value any) error {
	input, isText := value.(string)
	remove := isText && trimEditorSpace(input) == ""
	setting := 0.0
	if !remove {
		var err error
		if setting, err = editNumber(value); err != nil {
			return err
		}
		if field.whole {
			setting = math.Floor(setting)
		}
	}
	return c.updateIncidents(draft, field.branch, func(settings map[string]any) error {
		target := settings
		if field.duration {
			target = object(settings["duration"])
			if target == nil {
				return errors.New("choose a fault duration kind first")
			}
		}
		if remove {
			delete(target, field.key)
		} else {
			target[field.key] = setting
		}
		return nil
	})
}

// faultDurationKind sets the kind of the fault duration. It keeps only the
// members of the new kind, because the validator refuses a member of another
// kind. An empty value removes the duration.
func (c *projectChange) faultDurationKind(draft, value any) error {
	kind, ok := value.(string)
	members, known := faultDurationMembers[kind]
	if !ok || kind != "" && !known {
		return errors.New(`faults duration kind must be "fixed", "uniform", or "exponential"`)
	}
	return c.updateIncidents(draft, "faults", func(settings map[string]any) error {
		if kind == "" {
			delete(settings, "duration")
			return nil
		}
		previous := object(settings["duration"])
		duration := map[string]any{"kind": kind}
		for _, key := range members {
			if member, present := previous[key]; present {
				duration[key] = member
			}
		}
		settings["duration"] = duration
		return nil
	})
}

// updateIncidents applies update to an owned copy of the settings of branch.
// A change that leaves no member removes the branch: the browser deletes a
// member whose patch value is null, so that an exported project keeps the
// bytes of a project without the branch. A change that changes nothing
// proposes nothing, so that an untouched project keeps its bytes.
func (c *projectChange) updateIncidents(draft any, branch string, update func(map[string]any) error) error {
	original, present := object(draft)[branch]
	settings := object(cloneEditValue(original))
	if present && settings == nil {
		return errors.New("the " + branch + " settings must be an object")
	}
	if settings == nil {
		settings = make(map[string]any)
	}
	if err := update(settings); err != nil {
		return err
	}
	switch {
	case present && reflect.DeepEqual(original, any(settings)), !present && len(settings) == 0:
	case len(settings) == 0:
		c.Patch[branch] = nil
	default:
		c.Patch[branch] = settings
	}
	return nil
}
