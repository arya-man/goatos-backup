// Package migrationguard fails a backend process fast when the compiled
// binary and the Postgres database it is about to serve disagree about which
// migrations have been applied.
//
// # The incident this prevents
//
// A long-lived `go run ./cmd/api` process kept running while migrations
// 000187/000188 dropped tables it still queried (e.g.
// vaccination_shed_projection_rows). The DB moved forward; the running
// binary did not. Every affected screen returned 500/503 with no signal that
// the real cause was a stale binary, not a code bug. Nothing compared the
// binary's migration ceiling against the database's applied level, either at
// startup or while the process kept running.
//
// # Both directions
//
// AGENTS.md already documents (as a human/process discipline, not code) that
// app code must never start against a database behind that build's
// migrations - apply migrations first, then start the app. That is the
// "binary knows about migrations the DB doesn't have yet" direction
// (BinaryAhead below). This package enforces it in code for the first time,
// alongside the previously-unguarded reverse direction that caused the
// incident above: the database has migrations the binary doesn't know about
// (DBAhead below). Check fails fast on either direction; only an exact
// version match proceeds silently.
package migrationguard

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Status reports how the database's applied migration level compares to the
// level this compiled binary knows about (see BinaryVersion). Both versions
// are goatos_schema_migrations-style migration version strings, e.g.
// "000188" (see backend/migrations/postgres file naming).
type Status struct {
	DBVersion     string
	BinaryVersion string

	// DBAhead is true when the database has been migrated further than this
	// binary's embedded migration set knows about - the "stale binary kept
	// running past a forward migration" incident this package exists to
	// catch.
	DBAhead bool

	// BinaryAhead is true when this binary knows about migrations that have
	// not been applied to the database yet, including a never-migrated
	// database (DBVersion == ""). This is the direction already documented
	// in AGENTS.md ("do not start app code against a database that is
	// behind that build's migrations"); Check now enforces it too.
	BinaryAhead bool
}

// Check compares dbVersion (the highest version recorded in
// goatos_schema_migrations, or "" for a never-migrated database - see
// AppliedVersion) against binaryVersion (this binary's own ceiling - see
// BinaryVersion) and reports their relationship.
//
// It returns a nil error only when the two versions are equal. Every drift
// direction returns a non-nil, actionable error naming both versions, so a
// caller that only wants the primary "stale binary" guard can simply do:
//
//	if _, err := migrationguard.Check(dbVersion, binaryVersion); err != nil {
//	    return err // refuse to start / mark not-ready
//	}
//
// Status is returned alongside the error so callers (the /version endpoint)
// can report which direction drifted without re-parsing the error text.
func Check(dbVersion, binaryVersion string) (Status, error) {
	binaryVersion = strings.TrimSpace(binaryVersion)
	dbVersion = strings.TrimSpace(dbVersion)

	if binaryVersion == "" {
		return Status{}, errors.New("migrationguard: binary migration version is required")
	}
	binaryNum, err := parseVersion(binaryVersion)
	if err != nil {
		return Status{}, fmt.Errorf("migrationguard: parse binary migration version %q: %w", binaryVersion, err)
	}

	if dbVersion == "" {
		status := Status{DBVersion: dbVersion, BinaryVersion: binaryVersion, BinaryAhead: true}
		return status, fmt.Errorf(
			"database has no migrations applied but this binary requires %s — apply pending migrations first (go run ./cmd/migrate)",
			binaryVersion,
		)
	}

	dbNum, err := parseVersion(dbVersion)
	if err != nil {
		return Status{}, fmt.Errorf("migrationguard: parse database migration version %q: %w", dbVersion, err)
	}

	status := Status{DBVersion: dbVersion, BinaryVersion: binaryVersion}
	switch {
	case dbNum > binaryNum:
		status.DBAhead = true
		return status, fmt.Errorf(
			"database migrated to %s but this binary only knows %s — rebuild the backend",
			dbVersion, binaryVersion,
		)
	case dbNum < binaryNum:
		status.BinaryAhead = true
		return status, fmt.Errorf(
			"database is at migration %s but this binary requires %s — apply pending migrations first (go run ./cmd/migrate)",
			dbVersion, binaryVersion,
		)
	default:
		return status, nil
	}
}

func parseVersion(v string) (int64, error) {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fmt.Errorf("must be a non-negative integer, got %d", n)
	}
	return n, nil
}
