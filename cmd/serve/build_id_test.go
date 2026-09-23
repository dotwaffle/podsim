package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

var buildIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

var errBroken = errors.New("broken file system")

// mapFS makes a file system from name and content pairs.
func mapFS(pairs ...string) fstest.MapFS {
	files := fstest.MapFS{}
	for pair := range slices.Chunk(pairs, 2) {
		files[pair[0]] = &fstest.MapFile{Data: []byte(pair[1])}
	}
	return files
}

func withEntry(files fstest.MapFS, name string, file *fstest.MapFile) fstest.MapFS {
	files[name] = file
	return files
}

func mustBuildID(t *testing.T, files fs.FS) string {
	t.Helper()
	id, err := browserBuildID(files)
	if err != nil {
		t.Fatal(err)
	}
	if !buildIDPattern.MatchString(id) {
		t.Fatalf("build ID %q is not 16 lowercase hex characters", id)
	}
	return id
}

func TestBrowserBuildIDHashesPathSizeAndContent(t *testing.T) {
	t.Parallel()
	// WalkDir visits the directory "a" and its file before "a.b".
	stream := "a/c\x00\x00\x00\x00\x00\x00\x00\x00\x02yz" + "a.b\x00\x00\x00\x00\x00\x00\x00\x00\x01x"
	sum := sha256.Sum256([]byte(stream))
	want := hex.EncodeToString(sum[:])[:16]
	if got := mustBuildID(t, mapFS("a.b", "x", "a/c", "yz")); got != want {
		t.Fatalf("build ID = %s, want %s", got, want)
	}
}

func TestBrowserBuildIDComparesFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		left, right fstest.MapFS
		same        bool
	}{
		{name: "same files", left: mapFS("a", "ab", "b", "c"), right: mapFS("a", "ab", "b", "c"), same: true},
		{name: "changed byte", left: mapFS("a", "ab", "b", "c"), right: mapFS("a", "ab", "b", "d")},
		{name: "changed name", left: mapFS("a", "ab", "b", "c"), right: mapFS("a", "ab", "d", "c")},
		{name: "moved split between files", left: mapFS("a", "ab", "b", "c"), right: mapFS("a", "a", "b", "bc")},
		{name: "moved split between name and content", left: mapFS("a", "bc"), right: mapFS("ab", "c")},
		{name: "moved file to a directory", left: mapFS("a", "ab", "b", "c"), right: mapFS("a", "ab", "d/b", "c")},
		{name: "added empty file", left: mapFS("a", "ab"), right: mapFS("a", "ab", "b", "")},
		{
			name:  "added empty directory",
			left:  mapFS("a", "ab"),
			right: withEntry(mapFS("a", "ab"), "d", &fstest.MapFile{Mode: fs.ModeDir}),
			same:  true,
		},
		{
			name:  "link to a regular file",
			left:  mapFS("a", "ab", "b", "ab"),
			right: withEntry(mapFS("a", "ab"), "b", &fstest.MapFile{Data: []byte("a"), Mode: fs.ModeSymlink}),
			same:  true,
		},
		{
			name:  "added link to a directory",
			left:  mapFS("d/a", "ab"),
			right: withEntry(mapFS("d/a", "ab"), "e", &fstest.MapFile{Data: []byte("d"), Mode: fs.ModeSymlink}),
			same:  true,
		},
		{
			name:  "added link to a missing file",
			left:  mapFS("a", "ab"),
			right: withEntry(mapFS("a", "ab"), "b", &fstest.MapFile{Data: []byte("missing"), Mode: fs.ModeSymlink}),
			same:  true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			left, right := mustBuildID(t, test.left), mustBuildID(t, test.right)
			if (left == right) != test.same {
				t.Fatalf("build IDs %s and %s, want same = %t", left, right, test.same)
			}
		})
	}
}

// TestBrowserBuildIDSkipsBrokenLinksOnDisk uses a real directory because
// fstest.MapFS does not stop at a link loop.
func TestBrowserBuildIDSkipsBrokenLinksOnDisk(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "a"), []byte("ab"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := mustBuildID(t, os.DirFS(directory))
	// Each pair is a link target and a link name. "c" and "d" make a loop.
	links := [][2]string{{"missing", "b"}, {"d", "c"}, {"c", "d"}}
	for _, link := range links {
		if err := os.Symlink(link[0], filepath.Join(directory, link[1])); err != nil {
			t.Skipf("make link: %v", err)
		}
	}
	if got := mustBuildID(t, os.DirFS(directory)); got != want {
		t.Fatalf("build ID with broken links = %s, want %s", got, want)
	}
}

// openFunc is a file system that has only an Open method.
type openFunc func(name string) (fs.File, error)

func (f openFunc) Open(name string) (fs.File, error) { return f(name) }

// failingRead is a file whose reads fail.
type failingRead struct{ fs.File }

func (failingRead) Read([]byte) (int, error) { return 0, errBroken }

// resizedFile is a file whose Stat reports size, not the size of its content.
type resizedFile struct {
	fs.File
	size int64
}

func (f resizedFile) Stat() (fs.FileInfo, error) {
	info, err := f.File.Stat()
	return resizedInfo{FileInfo: info, size: f.size}, err
}

type resizedInfo struct {
	fs.FileInfo
	size int64
}

func (i resizedInfo) Size() int64 { return i.size }

// openFailsFS is a file system whose Open fails for the file named "b". Its
// Stat and ReadDir methods come from the MapFS and do not fail.
type openFailsFS struct{ fstest.MapFS }

func (f openFailsFS) Open(name string) (fs.File, error) {
	if name == "b" {
		return nil, errBroken
	}
	return f.MapFS.Open(name)
}

// faultyFS returns a file system that changes the file named "b" with wrap.
// It has no Stat method, so fs.Stat also gets the changed file.
func faultyFS(wrap func(fs.File) (fs.File, error)) fs.FS {
	files := mapFS("a", "ab", "b", "c")
	return openFunc(func(name string) (fs.File, error) {
		file, err := files.Open(name)
		if err != nil || name != "b" {
			return file, err
		}
		return wrap(file)
	})
}

func TestBrowserBuildIDRejectsFailingFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		files  fs.FS
		broken bool
		text   string // The error message must contain text.
	}{
		{name: "root open fails", files: openFunc(func(string) (fs.File, error) { return nil, errBroken }), broken: true},
		{name: "file stat fails", files: faultyFS(func(fs.File) (fs.File, error) { return nil, errBroken }), broken: true},
		{name: "file open fails", files: openFailsFS{mapFS("a", "ab", "b", "c")}, broken: true},
		{name: "file read fails", files: faultyFS(func(file fs.File) (fs.File, error) { return failingRead{file}, nil }), broken: true, text: "read b: "},
		{
			name:  "size differs from content",
			files: faultyFS(func(file fs.File) (fs.File, error) { return resizedFile{file, 2}, nil }),
			text:  "read b: got 1 bytes, want 2",
		},
		{
			name:  "negative size",
			files: faultyFS(func(file fs.File) (fs.File, error) { return resizedFile{file, -1}, nil }),
			text:  "stat b: negative size -1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			id, err := browserBuildID(test.files)
			if err == nil {
				t.Fatalf("browserBuildID = %q, want an error", id)
			}
			if errors.Is(err, errBroken) != test.broken {
				t.Fatalf("error = %v, want errBroken = %t", err, test.broken)
			}
			if !strings.Contains(err.Error(), test.text) {
				t.Fatalf("error = %v, want text %q", err, test.text)
			}
		})
	}
}

// warnRecord holds the log attributes that TestBuildIDOrRandom checks.
type warnRecord struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
	Build string `json:"build"`
	Error string `json:"error"`
}

func TestBuildIDOrRandom(t *testing.T) {
	t.Parallel()
	working := mapFS("a", "ab", "b", "c")
	failing := openFunc(func(string) (fs.File, error) { return nil, errBroken })
	tests := []struct {
		name  string
		files fs.FS
		want  string // Empty means a random ID and one warning.
	}{
		{name: "working files", files: working, want: mustBuildID(t, working)},
		{name: "failing files", files: failing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var buffer bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buffer, nil))
			id := buildIDOrRandom(logger, test.files)
			if !buildIDPattern.MatchString(id) {
				t.Fatalf("build ID %q is not 16 lowercase hex characters", id)
			}
			if test.want != "" {
				if id != test.want || buffer.Len() != 0 {
					t.Fatalf("build ID = %s with log %q, want %s and no log", id, buffer.String(), test.want)
				}
				return
			}
			if again := buildIDOrRandom(slog.New(slog.DiscardHandler), test.files); again == id {
				t.Fatalf("two random build IDs are both %s", id)
			}
			var records []warnRecord
			for line := range bytes.Lines(buffer.Bytes()) {
				var record warnRecord
				if err := json.Unmarshal(line, &record); err != nil {
					t.Fatalf("decode log record %q: %v", line, err)
				}
				records = append(records, record)
			}
			want := warnRecord{
				Level: slog.LevelWarn.String(),
				Msg:   "Browser build ID unavailable",
				Build: id,
				Error: "hash browser files: " + errBroken.Error(),
			}
			if len(records) != 1 || records[0] != want {
				t.Fatalf("log records = %+v, want %+v", records, want)
			}
		})
	}
}
