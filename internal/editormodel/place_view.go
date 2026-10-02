package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"math"

	"github.com/dotwaffle/podsim/internal/project"
)

type mapView struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Scale float64 `json:"scale"`
}

type geographicBounds struct {
	South float64 `json:"south"`
	North float64 `json:"north"`
	West  float64 `json:"west"`
	East  float64 `json:"east"`
}

type placeViewport struct {
	Latitude  *float64          `json:"latitude"`
	Longitude *float64          `json:"longitude"`
	Width     float64           `json:"width"`
	Height    float64           `json:"height"`
	Bounds    *geographicBounds `json:"bounds,omitempty"`
}

func placeView(geo *project.Geo, raw jsontext.Value) (mapView, error) {
	if geo == nil {
		return mapView{}, errors.New("set a geographic reference before navigating to a place")
	}
	if !finiteRange(geo.Latitude, -project.MaxGeoLatitude, project.MaxGeoLatitude) || !finiteRange(geo.Longitude, -180, 180) || geo.Projection != project.GeoProjection || geo.Radius != project.GeoRadius {
		return mapView{}, errors.New("the project geographic reference is invalid")
	}
	var target placeViewport
	if err := json.Unmarshal(raw, &target, json.RejectUnknownMembers(true)); err != nil || target.Latitude == nil || target.Longitude == nil || !finiteRange(*target.Latitude, -project.MaxGeoLatitude, project.MaxGeoLatitude) || !finiteRange(*target.Longitude, -180, 180) || !finiteRange(target.Width, 1, 100000) || !finiteRange(target.Height, 1, 100000) {
		return mapView{}, errors.New("place navigation needs valid coordinates and viewport dimensions")
	}
	latitude, longitude := *target.Latitude, *target.Longitude
	width, height := 1000.0, 1000.0
	degree := math.Pi / 180
	xScale := geo.Radius * math.Cos(geo.Latitude*degree) * degree
	yScale := geo.Radius * degree
	if box := target.Bounds; box != nil && finiteRange(box.South, -80, 80) && finiteRange(box.North, -80, 80) && finiteRange(box.West, -180, 180) && finiteRange(box.East, -180, 180) && box.South < box.North && box.West < box.East {
		latitude, longitude = (box.South+box.North)/2, (box.West+box.East)/2
		width, height = max(80, (box.East-box.West)*xScale), max(80, (box.North-box.South)*yScale)
	}
	scale := min(3, max(1, target.Width-100)/width, max(1, target.Height-100)/height)
	x, y := (longitude-geo.Longitude)*xScale, -(latitude-geo.Latitude)*yScale
	return mapView{X: target.Width/2 - x*scale, Y: target.Height/2 - y*scale, Scale: scale}, nil
}

func finiteRange(value, low, high float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= low && value <= high
}
