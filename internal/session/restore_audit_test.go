package session

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestRestoreRefusesAuditedProject pins the restore of a saved state whose
// project the all-pairs audit refuses: two small lanes 3.0 m apart that
// share no node. The project check comes before the checks of the other
// session members, so a speed that only the state check refuses does not
// hide the project refusal.
func TestRestoreRefusesAuditedProject(t *testing.T) {
	t.Parallel()
	s, err := NewWithProject(project.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	base := sessionStateFile(t, s)
	audited := project.Clone(base.Project)
	for _, row := range []struct {
		lane string
		y    float64
	}{{"pair-a", 0}, {"pair-b", 3}} {
		from, to := row.lane+"-from", row.lane+"-to"
		audited.Network.Nodes = append(audited.Network.Nodes, sim.Node{ID: from, Position: sim.Point{X: 9000, Y: row.y}}, sim.Node{ID: to, Position: sim.Point{X: 9100, Y: row.y}})
		audited.Network.Lanes = append(audited.Network.Lanes, sim.Lane{ID: row.lane, From: from, To: to, SpeedLimit: 14})
	}
	const refusal = `invalid_state: saved project: invalid project scenario: nonincident paths "pair-a" and "pair-b" are less than 12 meters apart`
	const speed = "invalid_state: speed 3 is not 1, 2, 5, 15, 30 or 60"
	for _, test := range []struct {
		name    string
		project project.Config
		speed   int
		want    string
	}{
		{"control", base.Project, base.Speed, ""},
		{"speed only", base.Project, 3, speed},
		{"audited project", audited, base.Speed, refusal},
		{"audited project and speed", audited, 3, refusal},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			file := base
			file.Project, file.Speed = test.project, test.speed
			data := encodeTestState(t, file)
			if _, err := decodeStateFile(data); err != nil {
				t.Fatalf("the state decode refused the file: %v", err)
			}
			restored := newTestSession(t)
			t.Cleanup(restored.Close)
			_, err := restored.loadState(loadInput{data: data, steps: realRestoreSteps()})
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || stateReason(err) != reasonInvalidState || err.Error() != test.want {
				t.Fatalf("restore error = %v (%s), want %s %s", err, stateReason(err), reasonInvalidState, test.want)
			}
		})
	}
}
