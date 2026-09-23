package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/session"
)

func TestBrowserFilesUsesSelectedDirectory(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "index.html"), []byte("podsim"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := browserFiles(directory)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := fs.ReadFile(files, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "podsim" {
		t.Fatalf("index contents = %q", contents)
	}
}

func TestBrowserFilesRejectsMissingBuild(t *testing.T) {
	t.Parallel()
	if _, err := browserFiles(t.TempDir()); err == nil {
		t.Fatal("accepted a browser directory without index.html")
	}
}

func TestPprofHandlerIsScoped(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		path string
		want int
	}{{path: "/debug/pprof/", want: http.StatusOK}, {path: "/", want: http.StatusNotFound}} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.path, http.NoBody)
		response := httptest.NewRecorder()
		pprofHandler().ServeHTTP(response, request)
		if response.Code != test.want {
			t.Fatalf("%s status = %d", test.path, response.Code)
		}
	}
}

func TestRunServersStopsAfterCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	server := &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
	var stopping int
	input := serveInput{servers: []namedServer{{name: "test", server: server, listener: loopbackListener(t)}}, stopping: func() { stopping++ }}
	if err := runServers(ctx, input); err != nil {
		t.Fatal(err)
	}
	if stopping != 1 {
		t.Fatalf("stopping calls = %d, want 1", stopping)
	}
}

func TestRunServersStopsSessionBeforeDrain(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		cancel  bool
		failing bool
		wantErr error
	}{
		{name: "signal", cancel: true},
		{name: "server failure", failing: true, wantErr: net.ErrClosed},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var (
				mu     sync.Mutex
				events []string
			)
			record := func(event string) {
				mu.Lock()
				defer mu.Unlock()
				events = append(events, event)
			}
			drained := make(chan struct{})
			application := &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
			application.RegisterOnShutdown(func() {
				record("drain")
				close(drained)
			})
			servers := []namedServer{{name: "application", server: application, listener: loopbackListener(t)}}
			if test.failing {
				// Serve fails at once on a closed listener.
				closed := loopbackListener(t)
				_ = closed.Close()
				failing := &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
				servers = append(servers, namedServer{name: "pprof", server: failing, listener: closed})
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancel {
				cancel()
			}
			// Shutdown runs the hook above in a new goroutine and does not wait for it.
			// So the stopping hook also asks the server directly if Shutdown started.
			stopping := func() {
				if shutdownStarted(t, application) {
					record("drain")
				}
				record("stopping")
			}
			err := runServers(ctx, serveInput{servers: servers, stopping: stopping})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("runServers error = %v, want %v", err, test.wantErr)
			}
			<-drained
			mu.Lock()
			defer mu.Unlock()
			if want := []string{"stopping", "drain"}; !slices.Equal(events, want) {
				t.Fatalf("events = %v, want %v", events, want)
			}
		})
	}
}

// shutdownStarted reports whether Shutdown has started on server. After Shutdown,
// Serve returns ErrServerClosed at once. Before Shutdown, Serve returns the
// Accept error from the closed listener.
func shutdownStarted(t *testing.T, server *http.Server) bool {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = listener.Close()
	return errors.Is(server.Serve(listener), http.ErrServerClosed)
}

func TestRunServersDoesNotWaitForBusySession(t *testing.T) {
	t.Parallel()
	saving, release := make(chan struct{}), make(chan struct{})
	releaseSaver := sync.OnceFunc(func() { close(release) })
	shared, err := session.NewWithProject(project.Default(), session.WithProjectSaver(func(project.Config) error {
		close(saving)
		<-release
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	epoch := shared.State().Epoch
	var busy sync.WaitGroup
	t.Cleanup(func() {
		releaseSaver()
		busy.Wait()
	})
	busy.Go(func() {
		demand := session.Command{
			Client: "editor", Sequence: 1, Epoch: epoch, Action: "demand",
			Demand: session.DemandConfig{Enabled: true, PerMinute: 12, Pattern: "balanced", Seed: 1},
		}
		if reply := shared.Apply(demand); reply.Error != "" {
			t.Errorf("demand command in progress: %s", reply.Error)
		}
	})
	<-saving
	clockContext, stopClock := context.WithCancel(t.Context())
	defer stopClock()
	var clock sync.WaitGroup
	clock.Go(func() { shared.Run(clockContext) })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	server := &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
	listener := loopbackListener(t)
	returned := make(chan error, 1)
	go func() {
		returned <- runServers(ctx, serveInput{servers: []namedServer{{name: "application", server: server, listener: listener}}, stopping: func() {
			shared.Close()
			stopClock()
		}})
	}()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runServers waited for a command that holds the session lock")
	}
	releaseSaver()
	busy.Wait()
	if !waitFor(waitInput{group: &clock, name: "Simulation clock", timeout: 5 * time.Second}) {
		t.Fatal("clock did not stop after the command finished")
	}
	pause := session.Command{Client: "editor", Sequence: 2, Epoch: epoch, Action: "pause", Paused: true}
	if reply := shared.Apply(pause); reply.ErrorCode != session.ServerStopping {
		t.Fatalf("command after stopping = %+v", reply)
	}
}

func TestWaitForIsBounded(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		blocked bool
		want    bool
	}{
		{name: "stopped", want: true},
		{name: "blocked", blocked: true, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				release := make(chan struct{})
				var group sync.WaitGroup
				group.Go(func() {
					if test.blocked {
						<-release
					}
				})
				got := waitFor(waitInput{group: &group, name: "Test group", timeout: 5 * time.Second})
				close(release)
				group.Wait()
				if got != test.want {
					t.Fatalf("waitFor = %t, want %t", got, test.want)
				}
			})
		})
	}
}

// loopbackListener opens a listener on a free loopback port. The test closes
// it at the end.
func loopbackListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

// browserDirectory returns a directory that browserFiles accepts.
func browserDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	for _, name := range []string{"index.html", "game.html", "podsim.wasm"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

func TestRunServesUntilCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	addresses := make(chan string, 1)
	returned := make(chan error, 1)
	input := runInput{
		args:  []string{"-addr", "127.0.0.1:0", "-dir", browserDirectory(t)},
		ready: func(addr string) { addresses <- addr },
	}
	go func() { returned <- run(ctx, input) }()
	var address string
	select {
	case address = <-addresses:
	case err := <-returned:
		t.Fatalf("run returned before ready: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("run did not call ready")
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+address+"/healthz", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(body) != "ok\n" {
		t.Fatalf("GET /healthz = %d %q, want 200 \"ok\\n\"", response.StatusCode, body)
	}
	cancel()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("run after cancellation = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after cancellation")
	}
}

func TestRunStopsAfterEarlyCancellation(t *testing.T) {
	t.Parallel()
	directory := browserDirectory(t)
	// A host name needs a lookup, which a canceled context stops.
	for _, address := range []string{"127.0.0.1:0", "localhost:0"} {
		t.Run(address, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			// main gives no ready function.
			if err := run(ctx, runInput{args: []string{"-addr", address, "-dir", directory}}); err != nil {
				t.Fatalf("run with a canceled context = %v, want nil", err)
			}
		})
	}
}

func TestRunFailsBeforeServing(t *testing.T) {
	t.Parallel()
	occupied := loopbackListener(t).Addr().String()
	directory := browserDirectory(t)
	for _, test := range []struct {
		name    string
		args    []string
		wantErr error
	}{
		{name: "unknown flag", args: []string{"-unknown"}, wantErr: errFlags},
		{name: "bad flag value", args: []string{"-addr"}, wantErr: errFlags},
		{name: "help", args: []string{"-h"}, wantErr: flag.ErrHelp},
		{name: "missing browser files", args: []string{"-dir", t.TempDir()}, wantErr: fs.ErrNotExist},
		{name: "application address in use", args: []string{"-addr", occupied, "-dir", directory}, wantErr: syscall.EADDRINUSE},
		{
			name:    "pprof address in use",
			args:    []string{"-addr", "127.0.0.1:0", "-pprof-addr", occupied, "-dir", directory},
			wantErr: syscall.EADDRINUSE,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var readyCalls int
			err := run(t.Context(), runInput{args: test.args, ready: func(string) { readyCalls++ }})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("run error = %v, want %v", err, test.wantErr)
			}
			if readyCalls != 0 {
				t.Fatalf("ready calls = %d, want 0", readyCalls)
			}
		})
	}
}

func TestExitCode(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		err    error
		want   int
		logged bool
	}{
		{name: "success", want: 0},
		{name: "help", err: fmt.Errorf("%w: %w", errFlags, flag.ErrHelp), want: 0},
		{name: "bad flag", err: fmt.Errorf("%w: %w", errFlags, errors.New("flag provided but not defined: -x")), want: 2},
		{name: "failure", err: errors.New("listen"), want: 1, logged: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			got := exitCode(slog.New(slog.NewTextHandler(&output, nil)), test.err)
			if got != test.want {
				t.Fatalf("exitCode = %d, want %d", got, test.want)
			}
			if logged := strings.Contains(output.String(), `msg="Serve prototype"`); logged != test.logged {
				t.Fatalf("logged = %t, want %t: %s", logged, test.logged, output.String())
			}
		})
	}
}

// addressListener is a listener that reports a fixed address.
type addressListener struct {
	net.Listener
	address net.Addr
}

func (l addressListener) Addr() net.Addr { return l.address }

func TestLogAddressShowsBoundPortOnlyWhenPicked(t *testing.T) {
	t.Parallel()
	bound := addressListener{address: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 43210}}
	for _, test := range []struct {
		configured string
		want       string
	}{
		{configured: "127.0.0.1:8080", want: "127.0.0.1:8080"},
		{configured: "localhost:8080", want: "localhost:8080"},
		{configured: ":8080", want: ":8080"},
		{configured: "", want: ""},
		{configured: "127.0.0.1:0", want: "127.0.0.1:43210"},
		{configured: "127.0.0.1:", want: "127.0.0.1:43210"},
	} {
		server := namedServer{name: "application", server: &http.Server{Addr: test.configured}, listener: bound}
		if got := server.logAddress(); got != test.want {
			t.Errorf("logAddress(%q) = %q, want %q", test.configured, got, test.want)
		}
	}
}

func TestOpenListenersClosesOpenedListenersOnFailure(t *testing.T) {
	t.Parallel()
	occupied := loopbackListener(t)
	servers := []namedServer{
		{name: "application", server: &http.Server{Addr: "127.0.0.1:0"}},
		{name: "pprof", server: &http.Server{Addr: occupied.Addr().String()}},
	}
	err := openListeners(t.Context(), servers)
	if !errors.Is(err, syscall.EADDRINUSE) || !strings.HasPrefix(err.Error(), "serve pprof HTTP: ") {
		t.Fatalf("openListeners error = %v, want serve pprof HTTP: EADDRINUSE", err)
	}
	if servers[1].listener != nil {
		t.Fatal("failed server has a listener")
	}
	// Close does not block. It gives ErrClosed if openListeners closed the listener.
	if err := servers[0].listener.Close(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("application listener Close error = %v, want %v", err, net.ErrClosed)
	}
}

func TestProjectFileRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "scenario.json")
	want := project.Default()
	want.Name = "Saved scenario"
	want.Demand = project.DemandConfig{Enabled: true, PerMinute: 20, Pattern: "destination", Seed: 4, Destination: "garden"}
	if err := saveProject(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("saved project changed\n got: %#v\nwant: %#v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestLondonProjectFileRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "london.json")
	want := scenarios.London()
	if err := saveProject(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("saved London project changed")
	}
}

func TestLoadProjectRejectsInvalidFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{"malformed", "{"},
		{"unknown field", `{"version":1,"unknown":true}`},
		{"trailing", `{}` + `{}`},
		{"oversize", strings.Repeat(" ", project.MaxFileBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "scenario.json")
			if err := os.WriteFile(path, []byte(test.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadProject(path); err == nil {
				t.Fatal("accepted invalid project file")
			}
		})
	}
}
