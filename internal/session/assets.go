package session

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// staticCacheControl makes browsers revalidate a cached browser file before they use it.
// A reload after a server upgrade then loads the new files.
const staticCacheControl = "no-cache"

// staticFiles serves the browser files with staticCacheControl.
func staticFiles(files fs.FS) http.Handler {
	server := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", staticCacheControl)
		server.ServeHTTP(w, r)
	})
}

// compressedWASMName is the only copy of the WASM module in a current build.
const compressedWASMName = "podsim.wasm.gz"

// precompressedWASM serves /podsim.wasm from podsim.wasm.gz.
// A client that accepts gzip gets the compressed bytes.
// A client without gzip and a Range request get the decompressed module, so
// byte offsets refer to the module. The handler decompresses the module on
// the first such request and keeps it until podsim.wasm.gz changes.
// A tree without podsim.wasm.gz, such as an older dist directory with only
// podsim.wasm, goes to fallback. A tree with a podsim.wasm that is newer
// than podsim.wasm.gz also goes to fallback, because the raw module is then
// the current build.
// The responses have an ETag from the module content and no Last-Modified
// header. A replacement can keep the modification time, so only the ETag
// can show that the module changed.
func precompressedWASM(files fs.FS, fallback http.Handler) http.Handler {
	var decoded decodedWASM
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/podsim.wasm" || r.Method != http.MethodGet && r.Method != http.MethodHead {
			fallback.ServeHTTP(w, r)
			return
		}
		compressed, err := files.Open(compressedWASMName)
		if err != nil {
			fallback.ServeHTTP(w, r)
			return
		}
		defer func() { _ = compressed.Close() }()
		info, err := compressed.Stat()
		if err != nil || !info.Mode().IsRegular() || rawIsNewer(files, info) {
			fallback.ServeHTTP(w, r)
			return
		}
		seeker, ok := compressed.(io.ReadSeeker)
		if !ok {
			serveWASMError(w, fmt.Errorf("%s cannot seek", compressedWASMName))
			return
		}
		id, err := identify(seeker, info)
		if err != nil {
			serveWASMError(w, err)
			return
		}
		header := w.Header()
		header.Set("Content-Type", "application/wasm")
		header.Set("Cache-Control", staticCacheControl)
		header.Add("Vary", "Accept-Encoding")
		if r.Header.Get("Range") == "" && acceptsGzip(r.Header.Get("Accept-Encoding")) {
			header.Set("Content-Encoding", "gzip")
			header.Set("ETag", id.etag("-gzip"))
			http.ServeContent(w, r, "podsim.wasm", time.Time{}, seeker)
			return
		}
		module, err := decoded.module(seeker, id)
		if err != nil {
			serveWASMError(w, err)
			return
		}
		header.Set("ETag", id.etag(""))
		http.ServeContent(w, r, "podsim.wasm", time.Time{}, bytes.NewReader(module))
	})
}

func serveWASMError(w http.ResponseWriter, err error) {
	slog.Error("Serve WASM module", slog.Any("error", err))
	http.Error(w, "WASM module unavailable", http.StatusInternalServerError)
}

// rawIsNewer reports whether files has a podsim.wasm that is newer than the
// compressed module described by compressed.
func rawIsNewer(files fs.FS, compressed fs.FileInfo) bool {
	raw, err := fs.Stat(files, "podsim.wasm")
	return err == nil && raw.Mode().IsRegular() && raw.ModTime().After(compressed.ModTime())
}

// wasmIdentity identifies one podsim.wasm.gz. The gzip trailer holds the
// CRC-32 and the size of the module, so a replacement with the same file
// size and modification time but a different module has a different
// identity.
type wasmIdentity struct {
	size    int64
	modTime time.Time
	trailer [8]byte
}

// etag returns an entity tag for the module. The tag comes from the content,
// so a conditional request gets the new module after a replacement that
// keeps the modification time. suffix separates the gzip representation
// from the decompressed one.
func (id wasmIdentity) etag(suffix string) string {
	return fmt.Sprintf(`"wasm-%x%s"`, id.trailer, suffix)
}

func (id wasmIdentity) equal(other wasmIdentity) bool {
	return id.size == other.size && id.modTime.Equal(other.modTime) && id.trailer == other.trailer
}

// identify returns the identity of compressed and moves its offset back to
// the start.
func identify(compressed io.ReadSeeker, info fs.FileInfo) (wasmIdentity, error) {
	id := wasmIdentity{size: info.Size(), modTime: info.ModTime()}
	if _, err := compressed.Seek(-int64(len(id.trailer)), io.SeekEnd); err != nil {
		return id, fmt.Errorf("read %s trailer: %w", compressedWASMName, err)
	}
	if _, err := io.ReadFull(compressed, id.trailer[:]); err != nil {
		return id, fmt.Errorf("read %s trailer: %w", compressedWASMName, err)
	}
	if _, err := compressed.Seek(0, io.SeekStart); err != nil {
		return id, fmt.Errorf("rewind %s: %w", compressedWASMName, err)
	}
	return id, nil
}

// decodedWASM keeps the decompressed module for clients without gzip and
// for Range requests. A wasmIdentity identifies the kept module. A new file
// in a -dir tree gives a new module.
type decodedWASM struct {
	mu   sync.Mutex
	id   wasmIdentity
	data []byte
}

// module returns the decompressed content of compressed, which has the
// identity id. It decompresses only when it has no module for id.
// The lock makes concurrent first requests wait for one decompression.
func (d *decodedWASM) module(compressed io.Reader, id wasmIdentity) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.data != nil && d.id.equal(id) {
		return d.data, nil
	}
	data, err := decompress(compressed)
	if err != nil {
		return nil, err
	}
	d.id, d.data = id, data
	return data, nil
}

// decompress returns the gzip-decoded content of compressed.
func decompress(compressed io.Reader) ([]byte, error) {
	reader, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", compressedWASMName, err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", compressedWASMName, err)
	}
	if err := reader.Close(); err != nil {
		return nil, fmt.Errorf("read %s: %w", compressedWASMName, err)
	}
	return data, nil
}
