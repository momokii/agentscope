// Package web exposes the production-built SPA (web/dist) as an embedded
// filesystem for the single-binary deployment (Wave W4-1).
//
// Why this file exists: go:embed patterns forbid `..` elements, so the
// directive cannot live in cmd/dashboard and still name web/dist. This file
// is the packaging seam only — it owns no routes and no logic. All serving
// (API precedence, index.html hash-route fallback) lives in
// cmd/dashboard/spa.go. Rebuild the SPA with `pnpm --dir web build` before
// `go build`; the embedded copy is only as fresh as the last web build.
package web

import "embed"

// Dist is the Vite production build (index.html + assets/). Served at /
// by cmd/dashboard; API routes take precedence (see cmd/dashboard/main.go).
//
//go:embed dist
var Dist embed.FS
