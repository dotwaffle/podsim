package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"log/slog"
)

// buildIDLength is the number of hex characters in a build ID.
const buildIDLength = 16

// browserBuildID identifies the browser files that the server sends. It
// hashes each regular file in lexical fs.WalkDir order with SHA-256. Each
// file adds its slash path, a NUL byte, its size as 8 big-endian bytes, and
// its content. A link to a regular file counts as that file. The hash skips
// a link that it cannot follow, such as a link to a missing file or a link
// loop, because the server cannot send it. The ID is the first 16 hex
// characters of the sum.
func browserBuildID(files fs.FS) (string, error) {
	browser := browserHash{files: files, sum: sha256.New()}
	err := fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		return browser.add(name, entry)
	})
	if err != nil {
		return "", fmt.Errorf("hash browser files: %w", err)
	}
	return hex.EncodeToString(browser.sum.Sum(nil))[:buildIDLength], nil
}

// browserHash adds browser files from files to sum.
type browserHash struct {
	files fs.FS
	sum   hash.Hash
}

// add adds the file name to the sum. It skips a name that is not a regular
// file and a link that it cannot follow. The size check makes sure that the
// size in the sum is the size of the content in the sum.
func (h browserHash) add(name string, entry fs.DirEntry) error {
	info, err := fs.Stat(h.files, name)
	if err != nil {
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	size := info.Size()
	if size < 0 {
		return fmt.Errorf("stat %s: negative size %d", name, size)
	}
	file, err := h.files.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	header := make([]byte, 0, len(name)+9)
	header = append(header, name...)
	header = append(header, 0)
	header = binary.BigEndian.AppendUint64(header, uint64(size))
	h.sum.Write(header)
	copied, err := io.Copy(h.sum, file)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if copied != size {
		return fmt.Errorf("read %s: got %d bytes, want %d", name, copied, size)
	}
	return nil
}

// buildIDOrRandom returns the build ID of the browser files. If the hash
// fails, it logs a warning and returns a random ID. Open pages then reload
// after each restart.
func buildIDOrRandom(logger *slog.Logger, files fs.FS) string {
	id, err := browserBuildID(files)
	if err == nil {
		return id
	}
	var random [buildIDLength / 2]byte
	rand.Read(random[:]) // Read never returns an error.
	id = hex.EncodeToString(random[:])
	logger.Warn("Browser build ID unavailable", slog.String("build", id), slog.Any("error", err))
	return id
}
