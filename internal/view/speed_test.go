package view

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/session"
)

func TestSpeedReductionNotice(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, epoch, start string
		sequence           uint64
		want               bool
	}{
		{"new reduction", "a", "server", 0, true},
		{"repeated frame", "a", "server", 1, false},
		{"first connection", "", "", 0, false},
		{"new process", "a", "old", 0, false},
		{"new epoch", "old", "server", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := &Game{state: session.State{Epoch: "a", ServerStart: "server", Speed: 15, SpeedReduction: session.SpeedReduction{Sequence: 1, From: 60, To: 15}}}
			g.announceSpeedReduction(session.State{Epoch: tc.epoch, ServerStart: tc.start, SpeedReduction: session.SpeedReduction{Sequence: tc.sequence}})
			if (g.notice.text != "") != tc.want {
				t.Fatalf("notice=%q, want shown=%t", g.notice.text, tc.want)
			}
			if tc.want && g.notice.text != "Speed reduced from 60x to 15x: the simulation could not keep up." {
				t.Fatal(g.notice.text)
			}
		})
	}
}
