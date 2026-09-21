package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/dotwaffle/podsim/internal/sim"
	"github.com/dotwaffle/podsim/internal/view"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	game, err := view.New(ctx, serverURL())
	if err != nil {
		slog.Error("Create game", slog.Any("error", err))
		os.Exit(1)
	}
	ebiten.SetWindowSize(1100, 760)
	ebiten.SetWindowTitle("Podsim")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetTPS(sim.TicksPerSecond)
	if err := ebiten.RunGame(game); err != nil {
		slog.Error("Run game", slog.Any("error", err))
		os.Exit(1)
	}
}
