//go:build linux

package statestore

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"slices"
	"syscall"
	"testing"
)

// fileSizeChildDir names the environment variable that makes the test
// binary run as the child of TestWriteOverFileSizeLimitKeepsOldState. Its
// value is the directory of the store.
const fileSizeChildDir = "PODSIM_STATESTORE_FSIZE_DIR"

// TestWriteOverFileSizeLimitKeepsOldState runs a 320 KiB Write in a child
// process with a file size limit of 64 KiB. The write fails part way, as it
// does on a full disk. The old state must stay, and no temporary file must
// stay.
func TestWriteOverFileSizeLimitKeepsOldState(t *testing.T) {
	t.Parallel()
	if dir := os.Getenv(fileSizeChildDir); dir != "" {
		writeOverFileSizeLimit(t, dir)
		return
	}
	dir := t.TempDir()
	store := openStore(t, "file://"+dir)
	old := []byte("old state")
	mustWrite(t, store, old)
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1", "-test.v")
	child.Env = append(os.Environ(), fileSizeChildDir+"="+dir)
	output, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("child process: %v\n%s", err, output)
	}
	if !bytes.Contains(output, []byte("--- PASS: "+t.Name())) {
		t.Fatalf("child process did not run the test:\n%s", output)
	}
	if got := mustRead(t, store); !bytes.Equal(got, old) {
		t.Fatalf("Read = %q, want %q", got, old)
	}
	if names := files(t, dir); !slices.Equal(names, []string{StateKey}) {
		t.Fatalf("files = %q, want only %s", names, StateKey)
	}
}

// writeOverFileSizeLimit is the child process of
// TestWriteOverFileSizeLimitKeepsOldState. The Go runtime ignores SIGXFSZ,
// so the write gets EFBIG.
func writeOverFileSizeLimit(t *testing.T, dir string) {
	t.Helper()
	limit := syscall.Rlimit{Cur: 64 << 10, Max: 64 << 10}
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		t.Fatal(err)
	}
	store := openStore(t, "file://"+dir)
	if err := store.Write(t.Context(), make([]byte, 320<<10)); !errors.Is(err, syscall.EFBIG) {
		t.Fatalf("Write of 320 KiB = %v, want EFBIG", err)
	}
}
