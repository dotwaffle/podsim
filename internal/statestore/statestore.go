// Package statestore keeps the saved session state in a gocloud.dev blob
// bucket. The server links only the fileblob driver, so only file:// URLs
// open. The browser build must not import this package.
package statestore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"gocloud.dev/blob"
	"gocloud.dev/blob/fileblob"
	"gocloud.dev/gcerrors"

	"github.com/dotwaffle/podsim/internal/session"
)

const (
	// StateKey is the key of the saved session state.
	StateKey = "session.json.gz"
	// PreviousKey is the key of the copy that Backup makes.
	PreviousKey = "session.previous.json.gz"

	// rejectedPrefix and rejectedSuffix go around the time stamp of a
	// rejected key.
	rejectedPrefix = "session.rejected."
	rejectedSuffix = ".json.gz"
	// rejectedLayout gives time stamps with a fixed width. Their lexical
	// order is their time order.
	rejectedLayout = "20060102T150405.000000000Z"
	// keepRejected is the number of rejected keys that Reject keeps.
	keepRejected = 3

	// sessionPrefix starts each key that the store writes.
	sessionPrefix = "session."
	// temporarySuffix ends the name of each temporary file of fileblob.
	temporarySuffix = ".tmp"

	contentType = "application/gzip"
	fileMode    = 0o600
	dirMode     = 0o700
)

// Store keeps the saved session state in a blob bucket. The session calls
// it from one goroutine at a time.
type Store struct {
	bucket   *blob.Bucket
	location string
	// root is the directory of the bucket. prefix is the key prefix from
	// the URL.
	root   string
	prefix string
	// existing is the deepest directory of root and its parents that
	// existed before Open.
	existing string
	// parentsSynced is true after a write synced each directory from the
	// file up to existing.
	parentsSynced atomic.Bool
	now           func() time.Time
	syncDir       func(dir string) error
	logger        *slog.Logger
}

var _ session.StateStore = (*Store)(nil)

// Open opens the store at rawURL.
//
// A file:// URL needs an empty host and an absolute path. Its only query
// parameter is prefix, a key prefix. prefix=podsim/ puts the files in the
// podsim directory, and prefix=podsim- puts podsim- in front of each file
// name. Open makes the directory with mode 0700 when it does not exist.
//
// Other schemes go to blob.DefaultURLMux, which has no drivers in this
// build. The error text holds the URL without user information and query
// values. For a scheme with a driver, the text of the cause can hold the
// full URL.
func Open(ctx context.Context, rawURL string) (*Store, error) {
	store, err := open(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("open state store %s: %w", Redact(rawURL), err)
	}
	return store, nil
}

// open does the work of Open.
func open(ctx context.Context, rawURL string) (*Store, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		// The text of a *url.Error holds the full URL.
		if urlErr, ok := errors.AsType[*url.Error](err); ok {
			return nil, urlErr.Err
		}
		return nil, err
	}
	switch u.Scheme {
	case "":
		return nil, errors.New("the URL needs a scheme, as in file:///var/lib/podsim")
	case fileblob.Scheme:
		return openFile(ctx, u)
	}
	// The error of OpenBucketURL for a scheme with no driver holds the
	// full URL, so check the scheme first.
	mux := blob.DefaultURLMux()
	if !mux.ValidBucketScheme(u.Scheme) {
		return nil, fmt.Errorf("no driver registered for scheme %q", u.Scheme)
	}
	bucket, err := mux.OpenBucketURL(ctx, u)
	if err != nil {
		return nil, err
	}
	return newStore(bucket, Redact(rawURL)), nil
}

// openFile checks the rules for a file URL, then opens the bucket through
// a private URLMux with safe fileblob options.
func openFile(ctx context.Context, u *url.URL) (*Store, error) {
	if u.User != nil || u.Host != "" {
		return nil, errors.New("a file URL needs an empty host, as in file:///var/lib/podsim")
	}
	root := filepath.FromSlash(u.Path)
	if u.Opaque != "" || !filepath.IsAbs(root) {
		return nil, errors.New("a file URL needs an absolute path, as in file:///var/lib/podsim")
	}
	if u.Fragment != "" {
		return nil, errors.New("a file URL cannot have a fragment")
	}
	prefix, err := filePrefix(u.RawQuery)
	if err != nil {
		return nil, err
	}
	root = filepath.Clean(root)
	existing := existingAncestor(root)
	mux := new(blob.URLMux)
	mux.RegisterBucket(fileblob.Scheme, &fileblob.URLOpener{Options: fileblob.Options{
		CreateDir:   true,
		DirFileMode: dirMode,
		NoTempDir:   true,
		Metadata:    fileblob.MetadataDontWrite,
	}})
	// The URLMux does not remove an empty prefix parameter, and fileblob
	// rejects it. So the opener gets a URL with no query.
	bucket, err := mux.OpenBucketURL(ctx, &url.URL{Scheme: fileblob.Scheme, Path: u.Path})
	if err != nil {
		return nil, err
	}
	if prefix != "" {
		bucket = blob.PrefixedBucket(bucket, prefix)
	}
	separator := string(filepath.Separator)
	store := newStore(bucket, strings.TrimSuffix(root, separator)+separator+filepath.FromSlash(prefix))
	store.root = root
	store.prefix = prefix
	store.existing = existing
	return store, nil
}

// newStore returns a store for bucket with the default clock and hooks.
func newStore(bucket *blob.Bucket, location string) *Store {
	return &Store{
		bucket:   bucket,
		location: location,
		now:      time.Now,
		syncDir:  syncDirectory,
		logger:   slog.Default(),
	}
}

// filePrefix returns the key prefix from the query of a file URL. It
// accepts no other query parameter.
func filePrefix(rawQuery string) (string, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", fmt.Errorf("parse query: %w", err)
	}
	for _, name := range slices.Sorted(maps.Keys(query)) {
		if name != "prefix" {
			return "", fmt.Errorf("the only query parameter is prefix, not %q", name)
		}
	}
	values := query["prefix"]
	if len(values) > 1 {
		return "", errors.New(`query parameter "prefix" is set more than once`)
	}
	if len(values) == 0 {
		return "", nil
	}
	return values[0], checkPrefix(values[0])
}

// checkPrefix checks that prefix is a clean relative slash path with a
// small set of characters. fileblob then keeps each key as it is in the
// file path, so the store knows which directory to sync, and List gives
// the keys back unchanged.
func checkPrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	if strings.ContainsFunc(prefix, notPrefixRune) {
		return fmt.Errorf("prefix %q can have only A-Z, a-z, 0-9, '.', '_', '-' and '/'", prefix)
	}
	// fileblob reads "__" in a listed path as the start of an escape, so
	// List would not find the keys.
	for _, part := range []string{"..", "__"} {
		if strings.Contains(prefix, part) {
			return fmt.Errorf("prefix %q cannot have %q", prefix, part)
		}
	}
	for part := range strings.SplitSeq(strings.TrimSuffix(prefix, "/"), "/") {
		if part == "" || part == "." {
			return fmt.Errorf("prefix %q is not a clean relative path", prefix)
		}
	}
	return nil
}

// notPrefixRune reports whether r cannot be in a key prefix.
func notPrefixRune(r rune) bool {
	switch {
	case 'a' <= r && r <= 'z', 'A' <= r && r <= 'Z', '0' <= r && r <= '9':
		return false
	}
	return !strings.ContainsRune("._-/", r)
}

// existingAncestor returns dir when it exists. Otherwise it returns the
// deepest parent of dir that exists.
func existingAncestor(dir string) string {
	for {
		_, err := os.Lstat(dir)
		parent := filepath.Dir(dir)
		if !errors.Is(err, fs.ErrNotExist) || parent == dir {
			return dir
		}
		dir = parent
	}
}

// Location returns the path that the store puts in front of each key, for
// example /var/lib/podsim/ or /var/lib/podsim/podsim-. For a bucket that is
// not a directory, it returns the redacted URL.
func (s *Store) Location() string {
	return s.location
}

// Read returns the saved state. The error wraps fs.ErrNotExist when there
// is none, and session.ErrStateTooLarge when the state is larger than
// session.MaxStateBytes.
func (s *Store) Read(ctx context.Context) ([]byte, error) {
	reader, err := s.openKey(ctx, StateKey)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, session.MaxStateBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", StateKey, err)
	}
	if len(data) > session.MaxStateBytes {
		return nil, fmt.Errorf("read %s: %w", StateKey, session.ErrStateTooLarge)
	}
	return data, nil
}

// Write replaces the saved state with data. The file gets mode 0600. Write
// syncs the file and its directory before it returns. A failed write keeps
// the old state. The one exception is a failed sync of the directory: the
// new state is then in place, but a power loss can undo the rename.
func (s *Store) Write(ctx context.Context, data []byte) error {
	return s.put(ctx, StateKey, bytes.NewReader(data))
}

// Reject moves the saved state to a key with the UTC time in its name, for
// example session.rejected.20260923T090000.000000000Z.json.gz. It deletes
// the saved state only after the copy is durable. Then it deletes the
// oldest rejected keys until 3 stay. It always keeps the new key, also when
// the clock is behind the times of the older keys. Reject logs a failure of
// this last step to the default slog logger and does not return it,
// because the move worked. The next Reject tries again.
func (s *Store) Reject(ctx context.Context) error {
	key := rejectedPrefix + s.now().UTC().Format(rejectedLayout) + rejectedSuffix
	if err := s.copyKey(ctx, key, StateKey); err != nil {
		return fmt.Errorf("reject saved state: %w", err)
	}
	if err := s.bucket.Delete(ctx, StateKey); err != nil {
		return fmt.Errorf("reject saved state: delete %s: %w", StateKey, err)
	}
	if err := s.pruneRejected(ctx, key); err != nil {
		s.logger.Warn("Prune rejected session state", slog.Any("error", err))
	}
	return nil
}

// Backup copies the saved state to PreviousKey and replaces an earlier
// backup. The saved state stays.
func (s *Store) Backup(ctx context.Context) error {
	if err := s.copyKey(ctx, PreviousKey, StateKey); err != nil {
		return fmt.Errorf("back up saved state: %w", err)
	}
	return nil
}

// RemoveTemporaries deletes the temporary files that interrupted writes
// left, and returns how many it deleted. Call it before the first write,
// when no write is in progress.
func (s *Store) RemoveTemporaries(ctx context.Context) (int, error) {
	keys, err := s.keys(ctx, sessionPrefix)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, key := range keys {
		if !strings.HasSuffix(key, temporarySuffix) {
			continue
		}
		if err := s.bucket.Delete(ctx, key); err != nil {
			return removed, fmt.Errorf("delete %s: %w", key, err)
		}
		removed++
	}
	return removed, nil
}

// Close closes the bucket.
func (s *Store) Close() error {
	if err := s.bucket.Close(); err != nil {
		return fmt.Errorf("close state store: %w", err)
	}
	return nil
}

// Redact returns rawURL without user information, fragment and query
// values. It keeps the scheme, the host, the path and the names of the
// query parameters.
func Redact(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "(invalid URL)"
	}
	query := url.Values{}
	for name := range u.Query() {
		query.Set(name, "")
	}
	redacted := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawQuery: query.Encode()}
	return redacted.String()
}

// openKey opens key for reading. The error wraps fs.ErrNotExist when the
// key does not exist.
func (s *Store) openKey(ctx context.Context, key string) (*blob.Reader, error) {
	reader, err := s.bucket.NewReader(ctx, key, nil)
	if gcerrors.Code(err) == gcerrors.NotFound {
		return nil, fmt.Errorf("read %s: %w", key, fs.ErrNotExist)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", key, err)
	}
	return reader, nil
}

// copyKey copies src to dst through put. It streams the data, so it can
// also move aside a file that is larger than session.MaxStateBytes.
func (s *Store) copyKey(ctx context.Context, dst, src string) error {
	reader, err := s.openKey(ctx, src)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	return s.put(ctx, dst, reader)
}

// put writes src to key and syncs the directory of the file.
func (s *Store) put(ctx context.Context, key string, src io.Reader) error {
	if err := s.writeFile(ctx, key, src); err != nil {
		return fmt.Errorf("write %s: %w", key, err)
	}
	if err := s.syncDirs(key); err != nil {
		return fmt.Errorf("write %s: %w", key, err)
	}
	return nil
}

// writeFile writes src to key through a temporary file with mode 0600, and
// syncs that file before fileblob renames it to the key. On any error it
// cancels the write before it closes the writer. fileblob then removes the
// temporary file and keeps the old file. A Close with a live context would
// rename the partial file over the old one.
func (s *Store) writeFile(ctx context.Context, key string, src io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var file *os.File
	writer, err := s.bucket.NewWriter(ctx, key, &blob.WriterOptions{
		ContentType: contentType,
		// Only get the file here. fileblob keeps the temporary file open
		// when BeforeWrite returns an error.
		BeforeWrite: func(as func(any) bool) error {
			as(&file)
			return nil
		},
	})
	if err != nil {
		return err
	}
	if err := copySynced(writer, file, src); err != nil {
		cancel()
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

// copySynced sets the mode of file, copies src to writer, and syncs file.
// file is the temporary file under writer.
func copySynced(writer io.Writer, file *os.File, src io.Reader) error {
	if file == nil {
		return errors.New("the bucket gave no *os.File")
	}
	if err := file.Chmod(fileMode); err != nil {
		return err
	}
	if _, err := io.Copy(writer, src); err != nil {
		return err
	}
	return file.Sync()
}

// syncDirs syncs the directory of the file for key. Until the first sync
// succeeds, it also syncs each parent up to s.existing. The directories
// that Open or fileblob made then stay after a power loss.
func (s *Store) syncDirs(key string) error {
	dir := filepath.Dir(filepath.Join(s.root, filepath.FromSlash(s.prefix+key)))
	last := dir
	if !s.parentsSynced.Load() {
		last = s.existing
	}
	for {
		if err := s.syncDir(dir); err != nil {
			return fmt.Errorf("sync directory %s: %w", dir, err)
		}
		parent := filepath.Dir(dir)
		if dir == last || parent == dir {
			break
		}
		dir = parent
	}
	s.parentsSynced.Store(true)
	return nil
}

// syncDirectory makes the entries of dir durable.
func syncDirectory(dir string) error {
	file, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}

// pruneRejected keeps the rejected key keep and the newest keepRejected-1
// other rejected keys, and deletes the rest. keep is the key that Reject
// wrote. Its time can be older than the other keys when the clock went
// back. pruneRejected skips temporary files and names that Reject did not
// make.
func (s *Store) pruneRejected(ctx context.Context, keep string) error {
	keys, err := s.keys(ctx, rejectedPrefix)
	if err != nil {
		return err
	}
	keys = slices.DeleteFunc(keys, func(key string) bool { return !isRejectedKey(key) || key == keep })
	var errs []error
	for _, key := range keys[:max(0, len(keys)-(keepRejected-1))] {
		if err := s.bucket.Delete(ctx, key); err != nil {
			errs = append(errs, fmt.Errorf("delete %s: %w", key, err))
		}
	}
	return errors.Join(errs...)
}

// isRejectedKey reports whether key has the form that Reject gives.
func isRejectedKey(key string) bool {
	stamp, ok := strings.CutPrefix(key, rejectedPrefix)
	if !ok {
		return false
	}
	stamp, ok = strings.CutSuffix(stamp, rejectedSuffix)
	if !ok {
		return false
	}
	_, err := time.Parse(rejectedLayout, stamp)
	return err == nil
}

// keys returns the keys that start with prefix, in lexical order. It also
// returns temporary keys. pruneRejected skips them, and RemoveTemporaries
// deletes them.
func (s *Store) keys(ctx context.Context, prefix string) ([]string, error) {
	objects, listErr := s.bucket.List(&blob.ListOptions{Prefix: prefix}).All(ctx)
	var keys []string
	for object := range objects {
		keys = append(keys, object.Key)
	}
	if err := listErr(); err != nil {
		return nil, fmt.Errorf("list %s: %w", prefix, err)
	}
	slices.Sort(keys)
	return keys, nil
}
