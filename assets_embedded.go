//go:build embed_assets

package podsim

import (
	"embed"
	"io/fs"
)

//go:embed dist
var webAssets embed.FS

// WebAssets returns the browser files embedded in this executable.
func WebAssets() (fs.FS, bool) {
	assets, err := fs.Sub(webAssets, "dist")
	if err != nil {
		panic(err)
	}
	return assets, true
}
