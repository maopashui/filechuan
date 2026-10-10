package webembed

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distEmbed embed.FS

func FS() (fs.FS, error) {
	return fs.Sub(distEmbed, "dist")
}
