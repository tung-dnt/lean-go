// Package files holds what the binary serves of the repository: the built static files.
package files

import (
	"embed"
)

// Static is the built site: main.css, the scripts and the images, below static/. It is made by make css and make js.
//
//go:embed static
var Static embed.FS
