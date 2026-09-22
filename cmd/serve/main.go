package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/KimMachineGun/automemlimit/memlimit"

	"github.com/dotwaffle/podsim"
	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/telemetry"
)

func main() {
	configureMemoryLimit()
	if err := run(); err != nil {
		slog.Error("Serve prototype", slog.Any("error", err))
		os.Exit(1)
	}
}

func configureMemoryLimit() {
	limit, err := memlimit.Set()
	if err != nil {
		slog.Warn("Automatic memory limit unavailable", slog.Any("error", err))
		return
	}
	slog.Info("Configured Go memory limit", slog.Int64("bytes", limit))
}

func run() error {
	address := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	directory := flag.String("dir", "", "Browser build directory; overrides embedded assets")
	pprofAddress := flag.String("pprof-addr", "", "Separate pprof listen address; disabled when empty")
	projectPath := flag.String("project", "", "Project JSON file to load and save")
	flag.Parse()
	files, err := browserFiles(*directory)
	if err != nil {
		return err
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
	telemetryProvider, err := telemetry.New(ctx, buildVersion(), shared.Metrics)
	if err != nil {
		return err
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := telemetryProvider.Shutdown(shutdown); err != nil {
			slog.Warn("Shut down telemetry", slog.Any("error", err))
		}
	}()
	clockContext, stopClock := context.WithCancel(ctx)
	defer stopClock()
	var clock sync.WaitGroup
	clock.Go(func() { shared.Run(clockContext) })
	handler := telemetryProvider.HTTPHandler(shared.HandlerFS(files))
	application := &http.Server{
		Addr:              *address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	slog.Info("Open Podsim in your browser", slog.String("url", "http://"+*address))
	servers := []namedServer{{name: "application", server: application}}
	if *pprofAddress != "" {
		diagnostics := &http.Server{
			Addr:              *pprofAddress,
			Handler:           pprofHandler(),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      5 * time.Minute,
			IdleTimeout:       60 * time.Second,
		}
		servers = append(servers, namedServer{name: "pprof", server: diagnostics})
		slog.Info("Enabled pprof", slog.String("address", *pprofAddress))
	}
	serveErr := runServers(ctx, serveInput{servers: servers, stopping: func() {
		shared.Close()
		stopClock()
	}})
	joinClock(&clock, 5*time.Second)
	return serveErr
}

// joinClock waits for the clock goroutine. It stops waiting after the timeout,
// so a blocked tick cannot keep the process alive. It returns true if the clock stopped.
func joinClock(clock *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		clock.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		slog.Warn("Simulation clock did not stop", slog.Duration("timeout", timeout))
		return false
	}
}

// serveInput holds the servers and a hook that runs once before the HTTP drain starts.
type serveInput struct {
	servers  []namedServer
	stopping func()
}

type namedServer struct {
	name   string
	server *http.Server
}

type serverResult struct {
	name string
	err  error
}

// runServers serves until cancellation or the first server failure. Then it
// calls input.stopping once and drains all servers.
func runServers(ctx context.Context, input serveInput) error {
	results := make(chan serverResult, len(input.servers))
	for _, current := range input.servers {
		go func() {
			results <- serverResult{name: current.name, err: current.server.ListenAndServe()}
		}()
	}
	remaining := len(input.servers)
	var errs []error
	cause := "signal"
	select {
	case result := <-results:
		remaining--
		cause = result.name + " server failed"
		if err := unexpectedServerError(result); err != nil {
			errs = append(errs, err)
		}
	case <-ctx.Done():
	}
	slog.Info("Stop accepting commands", slog.String("cause", cause))
	input.stopping()
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for _, current := range input.servers {
		if err := current.server.Shutdown(shutdown); err != nil {
			errs = append(errs, fmt.Errorf("shut down %s server: %w", current.name, err))
		}
	}
	for range remaining {
		if err := unexpectedServerError(<-results); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func unexpectedServerError(result serverResult) error {
	if result.err == nil || errors.Is(result.err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("serve %s HTTP: %w", result.name, result.err)
}

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "devel"
	}
	return info.Main.Version
}

func browserFiles(directory string) (fs.FS, error) {
	if directory == "" {
		if embedded, ok := podsim.WebAssets(); ok {
			return embedded, nil
		}
		directory = "dist"
	}
	files := os.DirFS(directory)
	if _, err := fs.Stat(files, "index.html"); err != nil {
		return nil, fmt.Errorf("build the browser files with mise run web first: %w", err)
	}
	return files, nil
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
	if info.Size() > project.MaxFileBytes {
		return project.Config{}, fmt.Errorf("read project %s: file exceeds 4 MiB", path)
	}
	decoder := json.NewDecoder(io.LimitReader(file, project.MaxFileBytes))
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
