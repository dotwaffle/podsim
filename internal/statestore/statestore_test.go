//go:build unix

// The tests check Unix file modes and the umask, so they run on Unix only.

package statestore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/session"
)

// clockStart is the first time of each fake clock. Its UTC time is
// 2026-09-23 09:00:00.123456789.
var clockStart = time.Date(2026, 9, 23, 11, 0, 0, 123456789, time.FixedZone("CEST", 2*60*60))

var errSync = errors.New("sync failed")

// openStore opens the store at rawURL with a fake clock that starts at
// clockStart. The store closes at the end of the test.
func openStore(t *testing.T, rawURL string) *Store {
	t.Helper()
	store, err := Open(t.Context(), rawURL)
	if err != nil {
		t.Fatal(err)
	}
	store.now = fakeClock(clockStart)
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return store
}

// fakeClock returns a clock that gives start, then moves on by one second
// at each call.
func fakeClock(start time.Time) func() time.Time {
	next := start
	return func() time.Time {
		now := next
		next = next.Add(time.Second)
		return now
	}
}

// rejectedKeyAt returns the key that Reject gives at the nth call of a
// fake clock, counted from 0.
func rejectedKeyAt(n int) string {
	stamp := clockStart.Add(time.Duration(n) * time.Second).UTC().Format(rejectedLayout)
	return rejectedPrefix + stamp + rejectedSuffix
}

// mustWrite writes data as the saved state.
func mustWrite(t *testing.T, store *Store, data []byte) {
	t.Helper()
	if err := store.Write(t.Context(), data); err != nil {
		t.Fatal(err)
	}
}

// mustRead returns the saved state.
func mustRead(t *testing.T, store *Store) []byte {
	t.Helper()
	data, err := store.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// files returns the slash paths of the regular files under dir, in
// lexical order.
func files(t *testing.T, dir string) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			relative, err := filepath.Rel(dir, name)
			if err != nil {
				return err
			}
			names = append(names, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// permissions returns the permission bits of name.
func permissions(t *testing.T, name string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

// readFile returns the content of name.
func readFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMissingStateIsNotExist(t *testing.T) {
	t.Parallel()
	operations := []struct {
		name string
		run  func(*Store, context.Context) error
	}{
		{name: "read", run: func(store *Store, ctx context.Context) error {
			_, err := store.Read(ctx)
			return err
		}},
		{name: "reject", run: (*Store).Reject},
		{name: "backup", run: (*Store).Backup},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			store := openStore(t, "file://"+dir)
			if err := operation.run(store, t.Context()); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("%s = %v, want an error that wraps fs.ErrNotExist", operation.name, err)
			}
			if names := files(t, dir); len(names) != 0 {
				t.Fatalf("files = %q, want none", names)
			}
		})
	}
}

func TestWriteAndRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := openStore(t, "file://"+dir)
	for _, data := range [][]byte{[]byte("first state"), {}, []byte("third state")} {
		mustWrite(t, store, data)
		if got := mustRead(t, store); !bytes.Equal(got, data) {
			t.Fatalf("Read = %q, want %q", got, data)
		}
		// No .attrs sidecar and no .tmp file stays.
		if names := files(t, dir); !slices.Equal(names, []string{StateKey}) {
			t.Fatalf("files = %q, want only %s", names, StateKey)
		}
	}
}

// TestFileAndDirectoryModes changes the umask of the process, so it does
// not run in parallel.
func TestFileAndDirectoryModes(t *testing.T) {
	for _, mask := range []int{0, 0o022} {
		t.Run(fmt.Sprintf("umask %#o", mask), func(t *testing.T) {
			previous := syscall.Umask(mask)
			t.Cleanup(func() { syscall.Umask(previous) })
			parent := filepath.Join(t.TempDir(), "new")
			root := filepath.Join(parent, "state")
			store := openStore(t, "file://"+root+"?prefix=a/")
			mustWrite(t, store, []byte("rejected state"))
			if err := store.Backup(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := store.Reject(t.Context()); err != nil {
				t.Fatal(err)
			}
			mustWrite(t, store, []byte("saved state"))
			// fileblob would use 0666 for files and 0777 for directories.
			for _, key := range []string{StateKey, PreviousKey, rejectedKeyAt(0)} {
				if got := permissions(t, filepath.Join(root, "a", key)); got != 0o600 {
					t.Errorf("mode of %s = %#o, want 0600", key, got)
				}
			}
			want := fs.FileMode(0o700 &^ mask)
			for _, dir := range []string{parent, root, filepath.Join(root, "a")} {
				if got := permissions(t, dir); got != want {
					t.Errorf("mode of %s = %#o, want %#o", dir, got, want)
				}
			}
		})
	}
}

// TestTemporaryFileInBucket sets TMPDIR, so it does not run in parallel.
// Without NoTempDir, fileblob makes the temporary file in os.TempDir. The
// rename then fails when os.TempDir is on a different file system, and
// RemoveTemporaries cannot find a file that a killed write left.
func TestTemporaryFileInBucket(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	store := openStore(t, "file://"+dir)
	mustWrite(t, store, []byte("saved state"))
	if names := files(t, dir); !slices.Equal(names, []string{StateKey}) {
		t.Fatalf("files = %q, want only %s", names, StateKey)
	}
}

func TestCanceledWriteKeepsOldState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := openStore(t, "file://"+dir)
	old := []byte("old state")
	mustWrite(t, store, old)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := store.Write(ctx, []byte("new state")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Write with a canceled context = %v, want context.Canceled", err)
	}
	if got := mustRead(t, store); !bytes.Equal(got, old) {
		t.Fatalf("Read = %q, want %q", got, old)
	}
	if names := files(t, dir); !slices.Equal(names, []string{StateKey}) {
		t.Fatalf("files = %q, want only %s", names, StateKey)
	}
}

func TestRejectKeepsNewestThree(t *testing.T) {
	t.Parallel()
	if got, want := rejectedKeyAt(0), "session.rejected.20260923T090000.123456789Z.json.gz"; got != want {
		t.Fatalf("first rejected key = %s, want %s", got, want)
	}
	tests := []struct {
		name string
		// planted are rejected keys from earlier runs and their content.
		planted map[string]string
		// rejects is the number of Write and Reject pairs. Write n writes
		// "state n".
		rejects int
		// want are the rejected keys that stay and their content.
		want map[string]string
	}{
		{
			name:    "clock moves on",
			rejects: 4,
			want:    map[string]string{rejectedKeyAt(1): "state 1", rejectedKeyAt(2): "state 2", rejectedKeyAt(3): "state 3"},
		},
		{
			// A host with no real-time clock can start before its clock is
			// set. Reject must keep the key that it wrote.
			name:    "clock behind older keys",
			planted: map[string]string{rejectedKeyAt(1001): "older 1", rejectedKeyAt(1002): "older 2", rejectedKeyAt(1003): "older 3"},
			rejects: 1,
			want:    map[string]string{rejectedKeyAt(0): "state 0", rejectedKeyAt(1002): "older 2", rejectedKeyAt(1003): "older 3"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			store := openStore(t, "file://"+dir)
			// A temporary file and a renamed file are not rejected keys, so
			// the count skips them and Reject keeps them.
			others := []string{rejectedKeyAt(0) + ".18a2b3c4d5e6f708.tmp", "session.rejected.manual.json.gz"}
			planted := map[string]string{}
			maps.Copy(planted, test.planted)
			for _, name := range others {
				planted[name] = name
			}
			for name, data := range planted {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for n := range test.rejects {
				mustWrite(t, store, fmt.Appendf(nil, "state %d", n))
				if err := store.Reject(t.Context()); err != nil {
					t.Fatal(err)
				}
				if _, err := store.Read(t.Context()); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("Read after Reject = %v, want fs.ErrNotExist", err)
				}
			}
			want := slices.Sorted(slices.Values(append(slices.Collect(maps.Keys(test.want)), others...)))
			if names := files(t, dir); !slices.Equal(names, want) {
				t.Fatalf("files = %q, want %q", names, want)
			}
			for name, data := range test.want {
				if got := readFile(t, filepath.Join(dir, name)); string(got) != data {
					t.Errorf("content of %s = %q, want %q", name, got, data)
				}
			}
		})
	}
}

// copyOperations are the operations that copy the saved state.
var copyOperations = []struct {
	name       string
	run        func(*Store, context.Context) error
	target     string
	keepSource bool
}{
	{name: "reject", run: (*Store).Reject, target: rejectedKeyAt(0)},
	{name: "backup", run: (*Store).Backup, target: PreviousKey, keepSource: true},
}

func TestCopyIsDurableBeforeSourceChanges(t *testing.T) {
	t.Parallel()
	for _, operation := range copyOperations {
		t.Run(operation.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			store := openStore(t, "file://"+dir)
			data := []byte("saved state")
			mustWrite(t, store, data)
			if err := os.WriteFile(filepath.Join(dir, PreviousKey), []byte("earlier backup"), 0o600); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(dir, StateKey)
			target := filepath.Join(dir, operation.target)
			syncs := 0
			store.syncDir = func(synced string) error {
				// The target is complete and the source is in place when
				// the directory sync makes the target durable.
				syncs++
				if synced != dir {
					t.Errorf("synced directory %s, want %s", synced, dir)
				}
				if got := readFile(t, target); !bytes.Equal(got, data) {
					t.Errorf("target at the directory sync = %q, want %q", got, data)
				}
				if got := permissions(t, target); got != 0o600 {
					t.Errorf("target mode at the directory sync = %#o, want 0600", got)
				}
				if got := readFile(t, source); !bytes.Equal(got, data) {
					t.Errorf("source at the directory sync = %q, want %q", got, data)
				}
				return syncDirectory(synced)
			}
			if err := operation.run(store, t.Context()); err != nil {
				t.Fatal(err)
			}
			if syncs != 1 {
				t.Fatalf("directory syncs = %d, want 1", syncs)
			}
			_, err := os.Stat(source)
			if operation.keepSource && err != nil {
				t.Fatalf("source after %s: %v", operation.name, err)
			}
			if !operation.keepSource && !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("source after %s: %v, want fs.ErrNotExist", operation.name, err)
			}
		})
	}
}

func TestFailedCopyKeepsSource(t *testing.T) {
	t.Parallel()
	failures := []struct {
		name    string
		prepare func(t *testing.T, store *Store, target string) context.Context
		want    error
	}{
		{
			name: "canceled context",
			prepare: func(t *testing.T, _ *Store, _ string) context.Context {
				t.Helper()
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
			want: context.Canceled,
		},
		{
			name: "directory at the target",
			prepare: func(t *testing.T, _ *Store, target string) context.Context {
				t.Helper()
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatal(err)
				}
				return t.Context()
			},
		},
		{
			name: "directory sync error",
			prepare: func(t *testing.T, store *Store, _ string) context.Context {
				t.Helper()
				store.syncDir = func(string) error { return errSync }
				return t.Context()
			},
			want: errSync,
		},
	}
	for _, operation := range copyOperations {
		for _, failure := range failures {
			t.Run(operation.name+"/"+failure.name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				store := openStore(t, "file://"+dir)
				data := []byte("saved state")
				mustWrite(t, store, data)
				ctx := failure.prepare(t, store, filepath.Join(dir, operation.target))
				err := operation.run(store, ctx)
				if err == nil || (failure.want != nil && !errors.Is(err, failure.want)) {
					t.Fatalf("%s = %v, want an error that wraps %v", operation.name, err, failure.want)
				}
				if got := mustRead(t, store); !bytes.Equal(got, data) {
					t.Fatalf("Read = %q, want %q", got, data)
				}
				for _, name := range files(t, dir) {
					if strings.HasSuffix(name, temporarySuffix) {
						t.Fatalf("temporary file %s stays", name)
					}
				}
			})
		}
	}
}

func TestSyncedDirectories(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// root and prefix make the URL. root is relative to a directory
		// that exists.
		root, prefix string
		// file is the path of the state file. first and later are the
		// directories that the first and a later Write sync.
		file         string
		first, later []string
	}{
		{name: "existing root", root: "", file: StateKey, first: []string{""}, later: []string{""}},
		{name: "new root", root: "a/b", file: "a/b/" + StateKey, first: []string{"a/b", "a", ""}, later: []string{"a/b"}},
		{name: "key prefix", root: "", prefix: "a", file: "a" + StateKey, first: []string{""}, later: []string{""}},
		{name: "directory prefix", root: "", prefix: "a/", file: "a/" + StateKey, first: []string{"a", ""}, later: []string{"a"}},
		{
			name: "new root and nested prefix", root: "r", prefix: "x/y/", file: "r/x/y/" + StateKey,
			first: []string{"r/x/y", "r/x", "r", ""}, later: []string{"r/x/y"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			base := t.TempDir()
			rawURL := "file://" + filepath.Join(base, test.root)
			if test.prefix != "" {
				rawURL += "?prefix=" + test.prefix
			}
			store := openStore(t, rawURL)
			var synced []string
			store.syncDir = func(dir string) error {
				relative, err := filepath.Rel(base, dir)
				if err != nil {
					return err
				}
				synced = append(synced, strings.TrimPrefix(filepath.ToSlash(relative), "."))
				return syncDirectory(dir)
			}
			for _, want := range [][]string{test.first, test.later} {
				synced = nil
				mustWrite(t, store, []byte("saved state"))
				if !slices.Equal(synced, want) {
					t.Fatalf("synced directories = %q, want %q", synced, want)
				}
			}
			if names := files(t, base); !slices.Equal(names, []string{test.file}) {
				t.Fatalf("files = %q, want %q", names, test.file)
			}
		})
	}
}

func TestRemoveTemporaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, prefix string
		// planted are the files to make, and removed are the files that
		// RemoveTemporaries deletes.
		planted, removed []string
	}{
		{
			name: "no prefix",
			planted: []string{
				StateKey, StateKey + ".18a2b3c4d5e6f708.tmp", PreviousKey + ".18a2b3c4d5e6f709.tmp",
				rejectedKeyAt(0), "other.tmp", "notes.txt",
			},
			removed: []string{StateKey + ".18a2b3c4d5e6f708.tmp", PreviousKey + ".18a2b3c4d5e6f709.tmp"},
		},
		{
			name:    "directory prefix",
			prefix:  "a/",
			planted: []string{"a/" + StateKey, "a/" + StateKey + ".18a2b3c4d5e6f708.tmp", StateKey + ".18a2b3c4d5e6f709.tmp"},
			removed: []string{"a/" + StateKey + ".18a2b3c4d5e6f708.tmp"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for _, name := range test.planted {
				path := filepath.Join(dir, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			store := openStore(t, "file://"+dir+"?prefix="+test.prefix)
			removed, err := store.RemoveTemporaries(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if removed != len(test.removed) {
				t.Fatalf("RemoveTemporaries = %d, want %d", removed, len(test.removed))
			}
			want := slices.DeleteFunc(slices.Clone(test.planted), func(name string) bool {
				return slices.Contains(test.removed, name)
			})
			slices.Sort(want)
			if names := files(t, dir); !slices.Equal(names, want) {
				t.Fatalf("files = %q, want %q", names, want)
			}
		})
	}
}

func TestReadSizeLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		size int
		want error
	}{
		{name: "at the limit", size: session.MaxStateBytes},
		{name: "above the limit", size: session.MaxStateBytes + 1, want: session.ErrStateTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			store := openStore(t, "file://"+dir)
			data := bytes.Repeat([]byte{'x'}, test.size)
			if err := os.WriteFile(filepath.Join(dir, StateKey), data, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := store.Read(t.Context())
			if !errors.Is(err, test.want) {
				t.Fatalf("Read = %v, want %v", err, test.want)
			}
			if test.want == nil && len(got) != test.size {
				t.Fatalf("Read gave %d bytes, want %d", len(got), test.size)
			}
			// Reject streams the file, so it can move aside a file of any size.
			if err := store.Reject(t.Context()); err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, filepath.Join(dir, rejectedKeyAt(0))); !bytes.Equal(got, data) {
				t.Fatalf("rejected copy has %d bytes, want %d", len(got), len(data))
			}
		})
	}
}

func TestOpenAcceptsFileURLs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// path and query make the URL. path is relative to a directory
		// that exists.
		path, query string
		// location is relative to the same directory. file is the path of
		// the state file.
		location, file string
	}{
		{name: "existing directory", location: "/", file: StateKey},
		{name: "new directory", path: "/new/state", location: "/new/state/", file: "new/state/" + StateKey},
		{name: "empty prefix", query: "?prefix=", location: "/", file: StateKey},
		{name: "key prefix", query: "?prefix=a", location: "/a", file: "a" + StateKey},
		{name: "directory prefix", query: "?prefix=a/", location: "/a/", file: "a/" + StateKey},
		{name: "nested prefix", query: "?prefix=a/b-c_d.E9/", location: "/a/b-c_d.E9/", file: "a/b-c_d.E9/" + StateKey},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			store := openStore(t, "file://"+dir+test.path+test.query)
			if got, want := store.Location(), dir+test.location; got != want {
				t.Fatalf("Location = %s, want %s", got, want)
			}
			mustWrite(t, store, []byte("saved state"))
			if names := files(t, dir); !slices.Equal(names, []string{test.file}) {
				t.Fatalf("files = %q, want %q", names, test.file)
			}
		})
	}
}

func TestOpenRejectsURLs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	regular := filepath.Join(dir, "regular")
	if err := os.WriteFile(regular, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Open must check each URL before it makes this directory.
	newDir := dir + "/new"
	tests := []struct {
		name, url, want string
	}{
		{name: "host", url: "file://var/lib/podsim", want: "empty host"},
		{name: "host only", url: "file://podsim-state", want: "empty host"},
		{name: "localhost", url: "file://localhost" + newDir, want: "empty host"},
		{name: "user information", url: "file://user:hunter2@" + newDir, want: "empty host"},
		{name: "opaque path", url: "file:podsim-state", want: "absolute path"},
		{name: "no path", url: "file://", want: "absolute path"},
		{name: "fragment", url: "file://" + newDir + "#hunter2", want: "fragment"},
		{name: "key", url: "file://" + newDir + "?key=k", want: `"key"`},
		{name: "directory mode", url: "file://" + newDir + "?dir_file_mode=0700", want: `"dir_file_mode"`},
		{name: "metadata", url: "file://" + newDir + "?metadata=", want: `"metadata"`},
		{name: "base URL", url: "file://" + newDir + "?base_url=x", want: `"base_url"`},
		{name: "secret key path", url: "file://" + newDir + "?secret_key_path=secret", want: `"secret_key_path"`},
		{name: "create directory", url: "file://" + newDir + "?create_dir=1", want: `"create_dir"`},
		{name: "no temporary directory", url: "file://" + newDir + "?no_tmp_dir=1", want: `"no_tmp_dir"`},
		{name: "prefix twice", url: "file://" + newDir + "?prefix=a/&prefix=b/", want: "more than once"},
		{name: "parent prefix", url: "file://" + newDir + "?prefix=../a/", want: `".."`},
		{name: "inner parent prefix", url: "file://" + newDir + "?prefix=a/../b/", want: `".."`},
		{name: "dots in a name", url: "file://" + newDir + "?prefix=a../", want: `".."`},
		{name: "escape in prefix", url: "file://" + newDir + "?prefix=a__0x2f__", want: `"__"`},
		{name: "absolute prefix", url: "file://" + newDir + "?prefix=/a/", want: "clean relative path"},
		{name: "empty segment", url: "file://" + newDir + "?prefix=a//b/", want: "clean relative path"},
		{name: "dot segment", url: "file://" + newDir + "?prefix=./a/", want: "clean relative path"},
		{name: "NUL in prefix", url: "file://" + newDir + "?prefix=a%00b", want: "only A-Z"},
		{name: "space in prefix", url: "file://" + newDir + "?prefix=a%20b", want: "only A-Z"},
		{name: "semicolon in query", url: "file://" + newDir + "?prefix=a;b", want: "parse query"},
		{name: "invalid escape", url: "file://user:hunter2@" + newDir + "%zz", want: "invalid URL escape"},
		{name: "no scheme", url: newDir, want: "needs a scheme"},
		{name: "unregistered scheme", url: "s3://user:hunter2@bucket/state", want: `scheme "s3"`},
		{name: "regular file", url: "file://" + regular, want: "not a directory"},
		{name: "path under a regular file", url: "file://" + regular + "/state", want: "not a directory"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store, err := Open(t.Context(), test.url)
			if err == nil {
				_ = store.Close()
				t.Fatalf("Open(%q) succeeded", test.url)
			}
			if prefix := "open state store " + Redact(test.url) + ": "; !strings.HasPrefix(err.Error(), prefix) {
				t.Errorf("error %q does not start with %q", err, prefix)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("error %q does not contain %q", err, test.want)
			}
			if strings.Contains(err.Error(), "hunter2") {
				t.Errorf("error %q holds a secret", err)
			}
		})
	}
	t.Cleanup(func() {
		if _, err := os.Stat(newDir); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Open made %s: %v", newDir, err)
		}
	})
}

func TestRedact(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, url, want string
	}{
		{name: "file", url: "file:///var/lib/podsim", want: "file:///var/lib/podsim"},
		{name: "query values", url: "file:///var/lib/podsim?prefix=a/", want: "file:///var/lib/podsim?prefix="},
		{name: "user information", url: "file://user:hunter2@/var/lib/podsim", want: "file:///var/lib/podsim"},
		{
			name: "bucket",
			url:  "s3://key:hunter2@bucket/state?region=eu-west-2&token=hunter2#hunter2",
			want: "s3://bucket/state?region=&token=",
		},
		{name: "invalid", url: "s3://user:hunter2@bucket/%zz", want: "(invalid URL)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := Redact(test.url); got != test.want {
				t.Fatalf("Redact(%q) = %q, want %q", test.url, got, test.want)
			}
		})
	}
}
