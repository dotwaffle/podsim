package session

import (
	"context"
	"errors"
)

// MaxStateBytes limits the compressed and the decompressed saved state.
const MaxStateBytes = 16 << 20

// ErrStateTooLarge means that the saved state is larger than MaxStateBytes.
var ErrStateTooLarge = errors.New("saved session state is too large")

// StateStore keeps one saved session state. The session calls it from
// one goroutine at a time.
type StateStore interface {
	// Read returns the saved state. The error wraps fs.ErrNotExist when
	// there is none, and ErrStateTooLarge above MaxStateBytes.
	Read(ctx context.Context) ([]byte, error)
	// Write replaces the saved state. A failed write keeps the old state.
	Write(ctx context.Context, data []byte) error
	// Reject moves the saved state aside. A later Read gets fs.ErrNotExist.
	Reject(ctx context.Context) error
	// Backup copies the saved state to a backup key and replaces an earlier backup.
	Backup(ctx context.Context) error
}
