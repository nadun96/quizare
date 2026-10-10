// Package migrations embeds the tutoring service's schema (tutoring.*). It
// has its own migration list, apart from the platform's (TS-NFR-50).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
