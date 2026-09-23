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
	if err := run(); err != nil {
		slog.Error("Run Podsim", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	game, err := view.New(ctx, serverURL(), view.WithReload(pageReloader()))
	if err != nil {
		return err
	}
	ebiten.SetWindowSize(1100, 760)
	ebiten.SetWindowTitle("Podsim")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetTPS(sim.TicksPerSecond)
	return ebiten.RunGame(game)
}
