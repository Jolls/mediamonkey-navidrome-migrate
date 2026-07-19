// Package web holds the embedded frontend assets served by the app.
package web

import "embed"

// FS is the embedded web UI: config form -> user pick -> scope pick ->
// scan/dry-run review -> commit, wired to the /api/* endpoints in app.js.
//
//go:embed index.html app.js style.css
var FS embed.FS
