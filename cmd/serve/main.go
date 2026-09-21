package main

import (
	"context"
	"errors"
	"os/signal"
	"syscall"

	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"github.com/dotwaffle/podsim/internal/session"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("Serve prototype", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	address := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	directory := flag.String("dir", "dist", "Directory containing the browser build")
	flag.Parse()
	if _, err := os.Stat(filepath.Join(*directory, "index.html")); err != nil {
		return fmt.Errorf("build the browser files with mise run web first: %w", err)
	}
	shared, err := session.New()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go shared.Run(ctx)
	server := &http.Server{
		Addr:              *address,
		Handler:           shared.Handler(*directory),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	slog.Info("Open Podsim in your browser", slog.String("url", "http://"+*address))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen: %w", err)
	}
	return nil
}
