//go:build linux

package statestore

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	interruptedWriteDir = "PODSIM_STATESTORE_INTERRUPTED_DIR"
	interruptedReady    = "partial state write ready"
)

// interruptedReader stops after the writer consumes its first payload.
// The parent kills the process before the copy or writer can finish.
type interruptedReader struct {
	wait    func() error
	payload []byte
}

func (r *interruptedReader) Read(data []byte) (int, error) {
	if len(r.payload) > 0 {
		n := copy(data, r.payload)
		r.payload = r.payload[n:]
		return n, nil
	}
	if _, err := fmt.Fprintln(os.Stdout, interruptedReady); err != nil {
		return 0, err
	}
	return 0, r.wait()
}

func TestInterruptedWriteKeepsOldState(t *testing.T) {
	t.Parallel()
	if dir := os.Getenv(interruptedWriteDir); dir != "" {
		store := openStore(t, "file://"+dir)
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		t.Cleanup(cancel)
		reader := &interruptedReader{wait: func() error { <-ctx.Done(); return ctx.Err() }, payload: bytes.Repeat([]byte{0xab}, 1<<20)}
		if err := store.writeFile(ctx, StateKey, reader); err != nil {
			t.Fatal(err)
		}
		t.Fatal("interrupted writer completed before the parent killed it")
	}
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "first-write", true: "replacement"}[existing], func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			old := []byte("last complete state")
			if existing {
				mustWrite(t, openStore(t, "file://"+dir), old)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			t.Cleanup(cancel)
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestInterruptedWriteKeepsOldState$", "-test.count=1", "-test.v")
			child.Env = append(os.Environ(), interruptedWriteDir+"="+dir)
			stdout, err := child.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if startErr := child.Start(); startErr != nil {
				t.Fatal(startErr)
			}
			waited := false
			t.Cleanup(func() {
				if !waited {
					_ = child.Process.Kill()
					_ = child.Wait()
				}
			})
			ready := make(chan error, 1)
			go func() {
				scanner := bufio.NewScanner(stdout)
				for scanner.Scan() {
					if scanner.Text() == interruptedReady {
						ready <- nil
						return
					}
				}
				if scanErr := scanner.Err(); scanErr != nil {
					ready <- fmt.Errorf("read child progress: %w", scanErr)
					return
				}
				ready <- errors.New("child exited before the partial write")
			}()
			select {
			case readyErr := <-ready:
				if readyErr != nil {
					t.Fatal(readyErr)
				}
			case <-ctx.Done():
				t.Fatal("child did not reach the partial write")
			}
			var partial string
			for _, name := range files(t, dir) {
				if strings.HasPrefix(name, StateKey+".") && strings.HasSuffix(name, temporarySuffix) {
					if partial != "" {
						t.Fatal("writer created multiple temporary files")
					}
					partial = name
				}
			}
			info, err := os.Stat(filepath.Join(dir, partial))
			if err != nil || partial == "" || info.Size() != 1<<20 || info.Mode().Perm() != fileMode {
				t.Fatalf("partial file = %q, info=%v, error=%v", partial, info, err)
			}
			if killErr := child.Process.Kill(); killErr != nil {
				t.Fatal(killErr)
			}
			err = child.Wait()
			waited = true
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatalf("child exit = %v, want signal exit", err)
			}
			status, ok := exit.Sys().(syscall.WaitStatus)
			if !ok || status.Signal() != syscall.SIGKILL {
				t.Fatalf("child exit = %v, want SIGKILL", err)
			}
			store := openStore(t, "file://"+dir)
			data, err := store.Read(t.Context())
			if existing {
				if err != nil || !bytes.Equal(data, old) {
					t.Fatalf("interrupted replacement lost old state: data=%q error=%v", data, err)
				}
			} else if !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("interrupted first write became saved state: %v", err)
			}
			removed, err := store.RemoveTemporaries(t.Context())
			if err != nil || removed != 1 {
				t.Fatalf("cleanup removed=%d error=%v, want one partial file", removed, err)
			}
			if existing && !slices.Equal(files(t, dir), []string{StateKey}) || !existing && len(files(t, dir)) != 0 {
				t.Fatal("cleanup changed the committed state or left a partial file")
			}
			complete := []byte("next complete state")
			mustWrite(t, store, complete)
			if !bytes.Equal(mustRead(t, store), complete) {
				t.Fatal("writer did not recover after partial-file cleanup")
			}
		})
	}
}
