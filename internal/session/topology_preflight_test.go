package session

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestTopologyPreflightAtCallers sends a valid plain project with the
// widest topology of its shape to each point that installs a project:
// session creation and project replace. Each ID has only ID characters,
// so the topology of a valid project is under the topology cap, whatever
// "<" its station names hold. A project whose station names take its
// topology to the cap is not valid, so project.Validate refuses it before
// the preflight. The preflight itself measures the topology at the
// largest project revision and the widest epoch.
func TestTopologyPreflightAtCallers(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("the cap projects run without -short and without the race detector")
	}
	const refusal = "topology exceeds supported limit"
	widest := topologyProject(t, 0)
	size := escapedTopologySize(t, widest)
	if size > project.MaxFileBytes+4096 {
		t.Fatalf("the widest topology has %d bytes, more than the cap %d", size, project.MaxFileBytes+4096)
	}
	t.Logf("widest topology of the shape: %d bytes, cap %d, headroom %d", size, project.MaxFileBytes+4096, project.MaxFileBytes+4096-size)
	atCap := topologyProject(t, project.MaxFileBytes+4096)
	over := topologyProject(t, project.MaxFileBytes+4096+1)

	t.Run("session creation", func(t *testing.T) {
		// A demand change increases the project revision without a
		// preflight. The preflight measured the largest revision, so the
		// session serves its HTTP state at each revision.
		s, err := NewWithProject(widest)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		client := newTestClient(s, "demand")
		for {
			if _, decodeErr := DecodeStateJSON(stateHTTPReply(t, s, widest, http.StatusOK)); decodeErr != nil {
				t.Fatal("the client refused the HTTP state of the widest topology", decodeErr)
			}
			if s.Topology().ProjectRevision == 10 {
				break
			}
			client.mustApply(t, Command{Action: "demand", Demand: widest.Demand})
		}
		s, err = NewWithProject(over)
		if s != nil {
			s.Close()
		}
		wantError(t, err, fmt.Sprintf("station name must contain 1 to %d characters", project.MaxNameLength))
	})

	t.Run("project replace", func(t *testing.T) {
		s, err := NewWithProject(project.Default())
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		client := newTestClient(s, "editor")
		client.mustApply(t, Command{Action: "pause", Paused: true})
		client.mustApply(t, Command{Action: "project", Project: &widest, ProjectRevision: 1})
		if _, err := DecodeStateJSON(stateHTTPReply(t, s, widest, http.StatusOK)); err != nil {
			t.Fatal("the client refused the HTTP state of the widest topology", err)
		}
	})

	t.Run("epoch width", func(t *testing.T) {
		// A restore keeps the saved epoch or makes a new epoch of 26
		// characters, so the preflight measures the wider of the two.
		server := strings.Repeat("0", 16)
		if err := preflightTopology(atCap, server, "E"); err != nil {
			t.Fatal("refused a short epoch at the cap", err)
		}
		wantError(t, preflightTopology(over, server, strings.Repeat("E", 25)), refusal)
		wantError(t, preflightTopology(atCap, server, strings.Repeat("E", 27)), refusal)
	})

	t.Run("markers", func(t *testing.T) {
		// Each contract marker adds bytes to the topology, so a project at
		// the cap goes over it with any marker.
		server, epoch := strings.Repeat("0", 16), strings.Repeat("0", 26)
		if err := preflightTopology(atCap, server, epoch); err != nil {
			t.Fatal("refused the unmarked project at the cap", err)
		}
		for name, mark := range map[string]func(*project.Config){
			"Express":   func(c *project.Config) { c.OrderContract = sim.ExpressOrderContract },
			"incident":  func(c *project.Config) { c.IncidentContract = sim.IncidentV1Contract },
			"fault":     func(c *project.Config) { c.FaultContract = sim.FaultV1Contract },
			"emergency": func(c *project.Config) { c.EmergencyContract = sim.EmergencyV1Contract },
		} {
			marked := atCap
			mark(&marked)
			if err := preflightTopology(marked, server, epoch); err == nil || err.Error() != refusal {
				t.Errorf("%s marker at the cap: got %v, want %q", name, err, refusal)
			}
		}
	})
}
