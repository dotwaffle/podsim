package view

import (
	"strconv"
	"testing"

	"github.com/dotwaffle/podsim/internal/session"
)

func TestPreviousSpeed(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ speed, want int }{{1, 60}, {2, 1}, {4, 2}, {5, 2}, {8, 5}, {15, 5}, {60, 15}} {
		t.Run(strconv.Itoa(test.speed), func(t *testing.T) {
			t.Parallel()
			if got := previousSpeed(test.speed); got != test.want {
				t.Fatalf("previous speed = %d, want %d", got, test.want)
			}
		})
	}
	for _, speed := range []int{1, 2, 5, 15, 60} {
		if previousSpeed(session.NextSpeed(speed)) != speed {
			t.Fatalf("forward/backward choices differ at %d", speed)
		}
	}
}

func TestRightClickSpeed(t *testing.T) {
	t.Parallel()
	game := sharedTestGame(t)
	game.state.Speed = 15
	game.rightClick(centerOfButton(findButton(t, game.buttons(), "demand")))
	if game.pending {
		t.Fatal("right-click outside Speed submitted a command")
	}
	game.rightClick(centerOfButton(findButton(t, game.buttons(), "speed")))
	syncGame(t, game, func() bool { return game.state.Speed == 5 })
	game.pending = true
	game.rightClick(centerOfButton(findButton(t, game.buttons(), "speed")))
	if game.state.Speed != 5 {
		t.Fatal("disabled right-click changed speed")
	}
}
