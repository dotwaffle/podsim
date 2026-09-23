package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, runInput{args: os.Args[1:]})
	stop()
	if code := exitCode(slog.Default(), err); code != 0 {
		os.Exit(code)
	}
}

// errFlags marks an error from the command-line flags.
var errFlags = errors.New("parse flags")

// exitCode returns the exit status for the error from run. It keeps the
// statuses of flag.ExitOnError: 0 after -h and 2 after a bad flag. The flag
// set already printed the usage text in both cases. For any other error,
// exitCode logs the error and returns 1.
func exitCode(logger *slog.Logger, err error) int {
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errFlags):
		return 2
	default:
		logger.Error("Serve prototype", slog.Any("error", err))
		return 1
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

// runInput holds the command-line arguments for run, without the program
// name. If ready is not nil, run calls it with the bound application address
// after it opens the listeners and before it serves requests.
type runInput struct {
	args  []string
	ready func(addr string)
}

// run parses the flags in input.args. Then it serves the browser files and
// the session until ctx is done or a server fails. When ctx is done, run
// stops the session, drains the servers and returns nil. With -state, run
// restores the saved session at startup, saves it while it serves, and
// saves it a last time after the clock stops, or when startup fails after
// the restore. When ctx is done while run restores the saved session, run
// returns nil and does not serve. A bad flag gives an error that wraps
// errFlags. -h gives an error that wraps errFlags and flag.ErrHelp. In both
// cases the flag set already printed the usage text to standard error. run
// never exits the process.
func run(ctx context.Context, input runInput) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	address := flags.String("addr", "127.0.0.1:8080", "HTTP listen address")
	directory := flags.String("dir", "", "Browser build directory; overrides embedded assets")
	pprofAddress := flags.String("pprof-addr", "", "Separate pprof listen address; disabled when empty")
	projectPath := flags.String("project", "", "Project JSON file to load and save")
	stateURL := flags.String("state", "", "Bucket URL for the saved session state, for example file:///var/lib/podsim. Off when empty.")
	if err := flags.Parse(input.args); err != nil {
		return fmt.Errorf("%w: %w", errFlags, err)
	}
	files, err := browserFiles(*directory)
	if err != nil {
		return err
	}
	build := buildIDOrRandom(slog.Default(), files)
	config := project.Default()
	options := []session.Option{session.WithLogger(slog.Default()), session.WithBuildID(build)}
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
	shared, closeStore, err := openSession(ctx, openInput{
		config: config, projectSet: *projectPath != "", stateURL: *stateURL, logger: slog.Default(), options: options,
	})
	if err != nil {
		// NewFromStore stops when ctx ends during the read of the saved
		// state. This is a clean stop, as it is in runServers.
		if ctx.Err() != nil {
			slog.Info("Stop during startup", slog.String("cause", "signal"), slog.Any("error", err))
			return nil
		}
		return err
	}
	defer func() {
		if closeErr := closeStore(); closeErr != nil {
			slog.Warn("Close session state store", slog.Any("error", closeErr))
		}
	}()
	// Until the clock starts, the session does not change and no client
	// sees it. When run fails before that, it saves the state as final. The
	// failed start then does not count as a restore, and repeated failed
	// starts do not reject the saved state with reason restore_loop.
	clockStarted := false
	defer func() {
		if !clockStarted && *stateURL != "" {
			shared.Close()
			saveFinal(ctx, shared, slog.Default())
		}
	}()
	telemetryProvider, err := telemetry.New(ctx, buildVersion(), shared.Metrics)
	if err != nil {
		return err
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := telemetryProvider.Shutdown(shutdown); err != nil {
			slog.Warn("Shut down telemetry", slog.Any("error", err))
		}
	}()
	handler := telemetryProvider.HTTPHandler(shared.HandlerFS(files))
	application := &http.Server{
		Addr:              *address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
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
	}
	if err := openListeners(ctx, servers); err != nil {
		return err
	}
	if input.ready != nil {
		input.ready(servers[0].listener.Addr().String())
	}
	slog.Info("Open Podsim in your browser", slog.String("url", "http://"+servers[0].logAddress()), slog.String("build", build))
	if *pprofAddress != "" {
		slog.Info("Enabled pprof", slog.String("address", servers[1].logAddress()))
	}
	clockContext, stopClock := context.WithCancel(ctx)
	defer stopClock()
	var clock sync.WaitGroup
	clockStarted = true
	clock.Go(func() { shared.Run(clockContext) })
	saverContext, stopSaver := context.WithCancel(ctx)
	defer stopSaver()
	var saver sync.WaitGroup
	if *stateURL != "" {
		saver.Go(func() { shared.RunStateSaver(saverContext, stateSaveInterval) })
	}
	serveErr := runServers(ctx, serveInput{servers: servers, stopping: func() {
		shared.Close()
		stopClock()
	}})
	clockStopped := waitFor(waitInput{group: &clock, name: "Simulation clock", timeout: 5 * time.Second, logger: slog.Default()})
	if *stateURL != "" {
		stopStateSaving(ctx, stopInput{
			session: shared, clockStopped: clockStopped, saver: &saver, stopSaver: stopSaver, logger: slog.Default(),
		})
	}
	return serveErr
}

// waitInput names a goroutine group for the log and limits the wait for it.
// logger gets the warning when the wait stops.
type waitInput struct {
	group   *sync.WaitGroup
	name    string
	timeout time.Duration
	logger  *slog.Logger
}

// waitFor waits for input.group. It stops waiting after input.timeout, so a
// blocked goroutine cannot keep the process alive. Then it logs a warning
// with input.name to input.logger. It returns true if the group stopped.
func waitFor(input waitInput) bool {
	done := make(chan struct{})
	go func() {
		input.group.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(input.timeout):
		input.logger.Warn(input.name+" did not stop", slog.Duration("timeout", input.timeout))
		return false
	}
}

// serveInput holds the servers and a hook that runs once before the HTTP drain starts.
type serveInput struct {
	servers  []namedServer
	stopping func()
}

// namedServer is an HTTP server, its name for the logs and the listener that
// it serves.
type namedServer struct {
	name     string
	server   *http.Server
	listener net.Listener
}

// logAddress returns the address for the startup logs. This is the
// configured address. If the configured port is empty or 0, the system picks
// the port, so logAddress returns the bound address.
func (s namedServer) logAddress() string {
	_, port, err := net.SplitHostPort(s.server.Addr)
	if err != nil || (port != "" && port != "0") {
		return s.server.Addr
	}
	return s.listener.Addr().String()
}

// openListeners opens a TCP listener at the Addr of each server and sets the
// listener of that server. If one listener fails, openListeners closes the
// listeners that it opened. Like net.Listen, it ignores the cancellation of
// ctx. Thus a signal during startup gives a clean shutdown, also for an
// address with a host name.
func openListeners(ctx context.Context, servers []namedServer) error {
	var config net.ListenConfig
	listenContext := context.WithoutCancel(ctx)
	for i := range servers {
		// ListenAndServe uses ":http" for an empty address.
		listener, err := config.Listen(listenContext, "tcp", cmp.Or(servers[i].server.Addr, ":http"))
		if err != nil {
			for _, opened := range servers[:i] {
				_ = opened.listener.Close()
			}
			return fmt.Errorf("serve %s HTTP: %w", servers[i].name, err)
		}
		servers[i].listener = listener
	}
	return nil
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
			results <- serverResult{name: current.name, err: current.server.Serve(current.listener)}
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
