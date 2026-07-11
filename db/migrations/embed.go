package migrations

import "embed"

// Files contains the SQL migrations used by the service migrate command.
//
//go:embed *.sql
var Files embed.FS
