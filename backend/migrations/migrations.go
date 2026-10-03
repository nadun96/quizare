// Package migrations embeds the SQL schema. Each module owns one PostgreSQL
// schema (auth, content, quiz, live, eval, results, audit); modules never
// query another module's schema directly (architecture §1.1 dependency rule).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
