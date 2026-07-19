// Package web holds the embedded frontend assets served by the app.
package web

import "embed"

// FS is the embedded web UI. For now it is a single placeholder page.
// TODO(sonnet): build the real UI (config form, scope picker, dry-run review,
// commit) — add files under this directory and they embed automatically.
//
//go:embed index.html
var FS embed.FS
