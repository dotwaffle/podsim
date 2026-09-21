package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
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
	projectPath := flag.String("project", "", "Project JSON file to load and save")
	flag.Parse()
	if _, err := os.Stat(filepath.Join(*directory, "index.html")); err != nil {
		return fmt.Errorf("build the browser files with mise run web first: %w", err)
	}
	config := project.Default()
	var options []session.Option
	if *projectPath != "" {
		loaded, loadErr := loadProject(*projectPath)
		if loadErr != nil {
			return loadErr
		}
		config = loaded
		options = append(options, session.WithProjectSaver(func(config project.Config) error {
			return saveProject(*projectPath, config)
		}))
	}
	shared, err := session.NewWithProject(config, options...)
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
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	slog.Info("Open Podsim in your browser", slog.String("url", "http://"+*address))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen: %w", err)
	}
	return nil
}

func loadProject(path string) (project.Config, error) {
	file, err := os.Open(path) // #nosec G304 -- The operator selects this local file with -project.
	if err != nil {
		return project.Config{}, fmt.Errorf("open project %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return project.Config{}, fmt.Errorf("stat project %s: %w", path, err)
	}
	if info.Size() > 2<<20 {
		return project.Config{}, fmt.Errorf("read project %s: file exceeds 2 MiB", path)
	}
	decoder := json.NewDecoder(io.LimitReader(file, 2<<20))
	decoder.DisallowUnknownFields()
	var config project.Config
	if err := decoder.Decode(&config); err != nil {
		return project.Config{}, fmt.Errorf("read project %s: %w", path, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return project.Config{}, fmt.Errorf("read project %s: expected one JSON value", path)
	}
	if err := project.Validate(config); err != nil {
		return project.Config{}, fmt.Errorf("validate project %s: %w", path, err)
	}
	return config, nil
}

func saveProject(path string, config project.Config) error {
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".podsim-project-*.tmp")
	if err != nil {
		return fmt.Errorf("create project file: %w", err)
	}
	temporary := file.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(temporary)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("set project permissions: %w", err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		_ = file.Close()
		return fmt.Errorf("encode project: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync project: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close project: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("replace project: %w", err)
	}
	remove = false
	return nil
}
