//go:build !embed_assets

package podsim

import "io/fs"

// WebAssets reports that this build needs browser files from the filesystem.
func WebAssets() (fs.FS, bool) {
	return nil, false
}
