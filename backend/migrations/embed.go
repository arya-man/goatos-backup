// Package migrations embeds the Goat OS Postgres migration SQL files at
// build time so any compiled binary - not just cmd/migrate, which reads them
// from disk - can answer "what migration version do you know about" without
// needing filesystem access to backend/migrations/postgres at runtime.
//
// This matters because the production Docker image (backend/Dockerfile)
// does not COPY backend/migrations into the image; it only copies the
// compiled binaries. Without an embed, cmd/api would have no way to learn
// its own migration ceiling in that image, which is exactly the information
// internal/platform/migrationguard needs to detect a stale binary running
// against a database that has been migrated forward past it.
package migrations

import "embed"

// Postgres embeds every *.sql migration file under postgres/. Do not use
// this to apply migrations - cmd/migrate still reads the directory from disk
// (backend/cmd/migrate/main.go) so operators can inspect or hand-run a
// migration without rebuilding a binary. Postgres exists so non-migrate
// binaries (starting with cmd/api) can learn the binary's own migration
// ceiling for the stale-binary drift guard in
// internal/platform/migrationguard.
//
//go:embed postgres/*.sql
var Postgres embed.FS
