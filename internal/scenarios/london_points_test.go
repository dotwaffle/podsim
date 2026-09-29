package scenarios

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

var updateLondonPoints = flag.Bool("update-london-points", false, "write testdata/london_points.json again")

// londonPointsPath holds the world positions of three London stations. The
// editor test of the projection in web/editor_test.cjs reads it.
const londonPointsPath = "testdata/london_points.json"

// londonPointStations are the IDs of the stations in londonPointsPath:
// Aldgate, Angel and Holborn.
var londonPointStations = []string{"940GZZLUALD", "940GZZLUAGL", "940GZZLUHBN"}

// londonPointFile is the form of londonPointsPath.
type londonPointFile struct {
	Geo    *project.Geo         `json:"geo"`
	Points []londonStationPoint `json:"points"`
}

// londonStationPoint is one station of londonPointsPath: its latitude and
// longitude in degrees, and its world position in meters.
type londonStationPoint struct {
	ID        string  `json:"id"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
}

// TestLondonPointsGolden checks that the London preset has the reference
// of londonPoint, and compares the positions that londonPoint gives with
// londonPointsPath. Run it with -update-london-points to write the file
// again.
func TestLondonPointsGolden(t *testing.T) {
	t.Parallel()
	geo := LondonCentral().Geo
	want := project.Geo{Latitude: londonReferenceLatitude, Longitude: londonReferenceLongitude, Projection: project.GeoProjection, Radius: project.GeoRadius}
	if geo == nil || *geo != want {
		t.Fatalf("London geo = %+v, want %+v", geo, want)
	}
	var source londonSource
	if err := decodeLondonSource(&source); err != nil {
		t.Fatal(err)
	}
	file := londonPointFile{Geo: geo}
	for _, id := range londonPointStations {
		for _, station := range source.Stations {
			if station.ID != id {
				continue
			}
			at := londonPoint(station.Latitude, station.Longitude)
			file.Points = append(file.Points, londonStationPoint{ID: station.ID, Latitude: station.Latitude, Longitude: station.Longitude, X: at.X, Y: at.Y})
		}
	}
	if len(file.Points) != len(londonPointStations) {
		t.Fatalf("found %d of the %d stations", len(file.Points), len(londonPointStations))
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if *updateLondonPoints {
		if err = os.WriteFile(londonPointsPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile(londonPointsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(golden, data) {
		t.Fatalf("%s differs from londonPoint. Run the test with -update-london-points.\n%s", londonPointsPath, data)
	}
}
