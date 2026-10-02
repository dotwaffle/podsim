package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"math"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

func TestPlaceViewProjectionAndBounds(t *testing.T) {
	t.Parallel()
	geo := &project.Geo{Latitude: 51.5, Longitude: -0.12, Radius: project.GeoRadius, Projection: project.GeoProjection}
	for _, row := range []struct {
		name, bounds string
		lat, lon     float64
		width        float64
	}{
		{"point", "", 51.5, -0.12, 1000},
		{"bounds", `,"bounds":{"south":51.4,"north":51.6,"west":-0.3,"east":0.1}`, 51.5, -0.1, .4 * project.GeoRadius * math.Cos(51.5*math.Pi/180) * math.Pi / 180},
		{"degenerate fallback", `,"bounds":{"south":51.5,"north":51.5,"west":0,"east":0}`, 51.5, -0.12, 1000},
		{"antimeridian fallback", `,"bounds":{"south":51,"north":52,"west":170,"east":-170}`, 51.5, -0.12, 1000},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			raw := jsontext.Value(`{"latitude":51.5,"longitude":-0.12,"width":900,"height":900` + row.bounds + `}`)
			view, err := placeView(geo, raw)
			if err != nil {
				t.Fatal(err)
			}
			x := (row.lon - geo.Longitude) * project.GeoRadius * math.Cos(geo.Latitude*math.Pi/180) * math.Pi / 180
			y := -(row.lat - geo.Latitude) * project.GeoRadius * math.Pi / 180
			if math.Abs(view.X+x*view.Scale-450) > 1e-9 || math.Abs(view.Y+y*view.Scale-450) > 1e-9 || math.Abs(view.Scale*row.width-800) > 1e-9 {
				t.Fatalf("view does not center and fit target: %+v", view)
			}
		})
	}
}

func TestPlaceViewWorkerInputs(t *testing.T) {
	t.Parallel()
	geo := `{"geo":{"latitude":0,"longitude":0,"projection":"equirectangular","radius":6371000}}`
	valid := `{"latitude":0,"longitude":0,"width":900,"height":800}`
	for _, row := range []struct {
		name, config, view, extra string
		ok                        bool
	}{
		{"zero coordinates", geo, valid, "", true},
		{"no reference", `{}`, valid, "", false},
		{"missing latitude", geo, `{"longitude":0,"width":900,"height":800}`, "", false},
		{"null longitude", geo, `{"latitude":0,"longitude":null,"width":900,"height":800}`, "", false},
		{"outside projection", geo, `{"latitude":81,"longitude":0,"width":900,"height":800}`, "", false},
		{"empty viewport", geo, `{"latitude":0,"longitude":0,"width":0,"height":800}`, "", false},
		{"unknown member", geo, `{"latitude":0,"longitude":0,"width":900,"height":800,"extra":1}`, "", false},
		{"mixed operation", geo, valid, `,"parkRide":{}`, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			input := `{"op":"place-view","project":` + row.config + `,"view":` + row.view + row.extra + `}`
			var result response
			if err := json.Unmarshal([]byte(Call(input)), &result); err != nil {
				t.Fatal(err)
			}
			if (result.View != nil && result.Error == "") != row.ok {
				t.Fatalf("result %+v, want success %t", result, row.ok)
			}
		})
	}
}
