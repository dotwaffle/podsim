package main

import (
	"context"
	"errors"
	"io/fs"
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
	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
	var stopping int
	input := serveInput{servers: []namedServer{{name: "test", server: server}}, stopping: func() { stopping++ }}
	if err := runServers(ctx, input); err != nil {
		t.Fatal(err)
	}
	if stopping != 1 {
		t.Fatalf("stopping calls = %d, want 1", stopping)
	}
}

func TestRunServersStopsSessionBeforeDrain(t *testing.T) {
	t.Parallel()
	occupied, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = occupied.Close() })
	for _, test := range []struct {
		name    string
		cancel  bool
		failing bool
		wantErr error
	}{
		{name: "signal", cancel: true},
		{name: "server failure", failing: true, wantErr: syscall.EADDRINUSE},
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
			application := &http.Server{Addr: "127.0.0.1:0", Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
			application.RegisterOnShutdown(func() {
				record("drain")
				close(drained)
			})
			servers := []namedServer{{name: "application", server: application}}
			if test.failing {
				failing := &http.Server{Addr: occupied.Addr().String(), Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
				servers = append(servers, namedServer{name: "pprof", server: failing})
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
	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
	returned := make(chan error, 1)
	go func() {
		returned <- runServers(ctx, serveInput{servers: []namedServer{{name: "application", server: server}}, stopping: func() {
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
	if !joinClock(&clock, 5*time.Second) {
		t.Fatal("clock did not stop after the command finished")
	}
	pause := session.Command{Client: "editor", Sequence: 2, Epoch: epoch, Action: "pause", Paused: true}
	if reply := shared.Apply(pause); reply.ErrorCode != session.ServerStopping {
		t.Fatalf("command after stopping = %+v", reply)
	}
}

func TestJoinClockIsBounded(t *testing.T) {
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
				var clock sync.WaitGroup
				clock.Go(func() {
					if test.blocked {
						<-release
					}
				})
				got := joinClock(&clock, 5*time.Second)
				close(release)
				clock.Wait()
				if got != test.want {
					t.Fatalf("joinClock = %t, want %t", got, test.want)
				}
			})
		})
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
