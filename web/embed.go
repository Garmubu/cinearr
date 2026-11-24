// Package web embeds the user interface.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var files embed.FS

// Files returns the interface files.
func Files() fs.FS {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
