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
	shell := shellLink()
	game, err := view.New(ctx, serverURL(), view.WithReload(pageReloader(shell)), view.WithShell(shell))
	if err != nil {
		return err
	}
	ebiten.SetWindowSize(1100, 728)
	ebiten.SetWindowTitle("Podsim")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetTPS(sim.TicksPerSecond)
	// Draw fills the full screen in each frame. While the shell page hides
	// the game, Draw does not draw, and the screen keeps the last frame.
	// Thus the game shows no empty frame when it shows again.
	ebiten.SetScreenClearedEveryFrame(false)
	return ebiten.RunGame(game)
}
