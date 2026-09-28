package frontend

import "embed"

// DistFS embeds all frontend web assets.
//
//go:embed all:dist
var DistFS embed.FS
