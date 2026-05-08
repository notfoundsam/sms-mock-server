package main

import "embed"

// templatesFS holds the JSON response/error and HTML UI templates baked into
// the binary. main.go consumes this via fs.Sub("templates").
//
//go:embed all:templates
var templatesFS embed.FS

// staticFS holds CSS, JS, favicon, and the asset manifest. Served at /static/
// and read at startup for the cache-bust map.
//
//go:embed all:static
var staticFS embed.FS
