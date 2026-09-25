// Package pgtemplate owns the REUSABLE migrated template database that the Postgres test harness
// (backend/internal/platform/pgtest) and the SQLC query-plan gate
// (backend/tests/integration/validate-sqlc-query-plans.sh) clone from when they run against an
// operator-supplied server (GOATOS_PGTEST_ADMIN_DSN / GOATOS_SQLC_PLAN_ADMIN_DSN, i.e. the OCI
// Postgres reached through the SSH tunnel).
//
// Why: applying all ~430 migrations to a fresh database over the tunnel took 15-25 minutes per
// package run. The template's CONTENT is fully determined by the migration files, so it is keyed by
// a hash of them and reused until a migration changes:
//
//	goatos_pgtest_template_<16 hex>   completed, connection-locked, commented with its hash + build time
//	goatos_pgtest_build_<16 hex>_<unix>   in-progress build; renamed to the template name only when complete
//	goatos_pgtest_clone_<unix>_<tag>      per-test / per-run clone, dropped by its owner
//
// Invariants:
//   - A template name is only ever produced by RENAMING a fully migrated build database, and the
//     rename is the last step, so a crash can never leave a half-migrated database under a template
//     name.
//   - Builders of the same hash serialize on a session advisory lock; the loser reuses the winner's
//     template.
//   - Cleanup only ever considers names matching the three exact patterns above. It can never match
//     the stg clone database `goatos` or any other database on the server.
//
// This package does not import "testing" so a small CLI (cmd/pgtest-template) can drive it from shell.
package pgtemplate

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HarnessSchemaVersion is folded into the hash. Bump it whenever the way the template is BUILT
// changes (extraction of goose Up sections, psql flags, post-build ALTERs), so an old template built
// the old way is never reused.
const HarnessSchemaVersion = "1"

const (
	templatePrefix = "goatos_pgtest_template_"
	buildPrefix    = "goatos_pgtest_build_"
	clonePrefix    = "goatos_pgtest_clone_"
	markerPrefix   = "goatos-pgtest-template"

	// StaleCloneAge: a clone older than this is a leftover of a killed run (the longest gate has a
	// 30 minute timeout).
	StaleCloneAge = 6 * time.Hour
	// StaleTemplateAge: templates for other hashes older than this are dropped, except the most
	// recent previous one (kept for a branch that is one migration behind).
	StaleTemplateAge = 3 * 24 * time.Hour
)

var (
	templateRE = regexp.MustCompile(`^goatos_pgtest_template_([0-9a-f]{16})$`)
	buildRE    = regexp.MustCompile(`^goatos_pgtest_build_([0-9a-f]{16})_([0-9]{10})$`)
	cloneRE    = regexp.MustCompile(`^goatos_pgtest_clone_([0-9]{10})_[0-9a-z_]{1,40}$`)
	markerRE   = regexp.MustCompile(`^goatos-pgtest-template hash=([0-9a-f]{16}) built=([0-9]{10})$`)
)

// Migration is one migration file: its base name and full content.
type Migration struct {
	Name    string
	Content []byte
}

// LoadMigrations reads backend/migrations/postgres/*.sql in filename order.
func LoadMigrations(repoRoot string) ([]Migration, error) {
	paths, err := filepath.Glob(filepath.Join(repoRoot, "backend", "migrations", "postgres", "*.sql"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no migrations found under %s", repoRoot)
	}
	sort.Strings(paths)
	out := make([]Migration, 0, len(paths))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Name: filepath.Base(p), Content: b})
	}
	return out, nil
}

// Hash returns the 16-hex-char content key over names + contents + HarnessSchemaVersion.
func Hash(migrations []Migration) string {
	h := sha256.New()
	fmt.Fprintf(h, "harness=%s\n", HarnessSchemaVersion)
	for _, m := range migrations {
		fmt.Fprintf(h, "%s\x00%d\x00", m.Name, len(m.Content))
		h.Write(m.Content)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// TemplateName is the reusable template database for a hash.
func TemplateName(hash string) string { return templatePrefix + hash }

func buildName(hash string, now time.Time) string {
	return fmt.Sprintf("%s%s_%010d", buildPrefix, hash, now.Unix())
}

// CloneName returns a clone database name carrying its creation time, so stale clones of killed
// runs can be recognised and reaped. tag must be [0-9a-z_], at most 40 chars.
func CloneName(now time.Time, tag string) string {
	return fmt.Sprintf("%s%010d_%s", clonePrefix, now.Unix(), tag)
}

func marker(hash string, now time.Time) string {
	return fmt.Sprintf("%s hash=%s built=%010d", markerPrefix, hash, now.Unix())
}

// lockKey derives the advisory-lock key for a hash.
func lockKey(hash string) int64 {
	b, _ := hex.DecodeString(hash)
	var buf [8]byte
	copy(buf[:], b)
	return int64(binary.BigEndian.Uint64(buf[:]))
}

// cleanupLockKey serializes cleanup across runs (independent of any hash).
const cleanupLockKey int64 = 0x676f61746f735047 // "goatosPG"

// ExtractGooseUp returns the goose Up section of a migration.
func ExtractGooseUp(sqlText string) string {
	var out []string
	inUp := false
	for _, line := range strings.Split(sqlText, "\n") {
		switch {
		case strings.HasPrefix(line, "-- +goose Up"):
			inUp = true
			continue
		case strings.HasPrefix(line, "-- +goose Down"):
			inUp = false
		}
		if inUp {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// MigrationScript concatenates every Up section into one psql script.
func MigrationScript(migrations []Migration) string {
	var script strings.Builder
	for _, m := range migrations {
		upSQL := strings.TrimSpace(ExtractGooseUp(string(m.Content)))
		if upSQL == "" {
			continue
		}
		fmt.Fprintf(&script, "\\echo applying %s\n", m.Name)
		script.WriteString(upSQL)
		script.WriteByte('\n')
	}
	return script.String()
}

// DSNFor swaps the database of a URL or libpq keyword DSN, preserving everything else.
func DSNFor(base, db string) string {
	if !strings.HasPrefix(base, "postgres://") && !strings.HasPrefix(base, "postgresql://") {
		return base + " dbname=" + db
	}
	// Manual path swap keeps query params and userinfo byte-identical.
	rest := base[strings.Index(base, "://")+3:]
	q := ""
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		q = rest[i:]
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	return base[:strings.Index(base, "://")+3] + rest + "/" + db + q
}

// QuoteIdent double-quotes a SQL identifier.
func QuoteIdent(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// Ensure returns the name of a completed template for the given migrations on the server behind
// admin (connected to a maintenance database) and baseDSN, building it if missing. log may be nil.
func Ensure(ctx context.Context, admin *pgxpool.Pool, baseDSN string, migrations []Migration, log io.Writer) (string, error) {
	if log == nil {
		log = io.Discard
	}
	hash := Hash(migrations)
	name := TemplateName(hash)

	conn, err := admin.Acquire(ctx)
	if err != nil {
		return "", fmt.Errorf("acquire admin conn: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockKey(hash)); err != nil {
		return "", fmt.Errorf("advisory lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, lockKey(hash))
	}()

	ok, err := templateComplete(ctx, admin, name, hash)
	if err != nil {
		return "", err
	}
	if ok {
		fmt.Fprintf(log, "pgtemplate: reusing %s\n", name)
		return name, nil
	}

	// We hold the lock for this hash, so any build database for it is a crashed leftover. A
	// template-named database without the marker cannot happen by construction, but drop it too
	// rather than trust it.
	if err := dropMatching(ctx, admin, func(db string) bool {
		m := buildRE.FindStringSubmatch(db)
		return (m != nil && m[1] == hash) || db == name
	}); err != nil {
		return "", err
	}

	now := time.Now()
	build := buildName(hash, now)
	fmt.Fprintf(log, "pgtemplate: building %s (%d migrations) -> %s\n", build, len(migrations), name)
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+QuoteIdent(build)); err != nil {
		return "", fmt.Errorf("create build database: %w", err)
	}
	if _, err := exec.LookPath("psql"); err != nil {
		return "", fmt.Errorf("psql is not on PATH: %w", err)
	}
	cmd := exec.CommandContext(ctx, "psql", "-q", "-v", "ON_ERROR_STOP=1", DSNFor(baseDSN, build))
	cmd.Stdin = strings.NewReader(MigrationScript(migrations))
	if out, err := cmd.CombinedOutput(); err != nil {
		_, _ = admin.Exec(context.Background(), `DROP DATABASE IF EXISTS `+QuoteIdent(build)+` WITH (FORCE)`)
		return "", fmt.Errorf("psql migrate %s failed: %v\n%s", build, err, tail(out, 4000))
	}
	for _, stmt := range []string{
		`ALTER DATABASE ` + QuoteIdent(build) + ` WITH ALLOW_CONNECTIONS false`,
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '` + build + `'`,
		`ALTER DATABASE ` + QuoteIdent(build) + ` RENAME TO ` + QuoteIdent(name),
		`COMMENT ON DATABASE ` + QuoteIdent(name) + ` IS '` + marker(hash, now) + `'`,
	} {
		if _, err := admin.Exec(ctx, stmt); err != nil {
			return "", fmt.Errorf("finalize template (%s): %w", stmt, err)
		}
	}
	fmt.Fprintf(log, "pgtemplate: built %s in %s\n", name, time.Since(now).Round(time.Second))
	return name, nil
}

func templateComplete(ctx context.Context, admin *pgxpool.Pool, name, hash string) (bool, error) {
	var comment *string
	err := admin.QueryRow(ctx,
		`SELECT shobj_description(oid, 'pg_database') FROM pg_database WHERE datname = $1`, name).Scan(&comment)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return false, nil
		}
		return false, fmt.Errorf("look up template: %w", err)
	}
	if comment == nil {
		return false, nil
	}
	m := markerRE.FindStringSubmatch(*comment)
	return m != nil && m[1] == hash, nil
}

// Candidate is a database on the server as seen by cleanup.
type Candidate struct {
	Name    string
	Comment string
}

// SelectStale decides which databases cleanup drops. It is pure so its name filtering can be tested
// exhaustively: only the three pgtest patterns can ever be returned.
func SelectStale(dbs []Candidate, currentHash string, now time.Time) []string {
	type tmpl struct {
		name  string
		built time.Time
	}
	var others []tmpl
	var drop []string
	for _, d := range dbs {
		if m := cloneRE.FindStringSubmatch(d.Name); m != nil {
			if ts, ok := unix(m[1]); ok && now.Sub(ts) > StaleCloneAge {
				drop = append(drop, d.Name)
			}
			continue
		}
		if m := buildRE.FindStringSubmatch(d.Name); m != nil {
			if ts, ok := unix(m[2]); ok && now.Sub(ts) > StaleCloneAge {
				drop = append(drop, d.Name)
			}
			continue
		}
		if m := templateRE.FindStringSubmatch(d.Name); m != nil {
			if m[1] == currentHash {
				continue
			}
			built := time.Time{} // unmarked: treat as ancient
			if mm := markerRE.FindStringSubmatch(d.Comment); mm != nil && mm[1] == m[1] {
				if ts, ok := unix(mm[2]); ok {
					built = ts
				}
			}
			others = append(others, tmpl{d.Name, built})
		}
	}
	// Keep the most recently built other template (the "previous" one).
	sort.Slice(others, func(i, j int) bool { return others[i].built.After(others[j].built) })
	for i, o := range others {
		if i == 0 && !o.built.IsZero() {
			continue
		}
		if now.Sub(o.built) > StaleTemplateAge {
			drop = append(drop, o.name)
		}
	}
	sort.Strings(drop)
	return drop
}

// Cleanup drops stale pgtest databases (see SelectStale). Errors on individual drops are ignored:
// a database still in use is simply left for the next run.
func Cleanup(ctx context.Context, admin *pgxpool.Pool, currentHash string, log io.Writer) {
	if log == nil {
		log = io.Discard
	}
	var got bool
	if err := admin.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, cleanupLockKey).Scan(&got); err != nil || !got {
		return
	}
	defer func() { _, _ = admin.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, cleanupLockKey) }()
	rows, err := admin.Query(ctx,
		`SELECT datname, coalesce(shobj_description(oid, 'pg_database'), '') FROM pg_database WHERE datname LIKE 'goatos\_pgtest\_%'`)
	if err != nil {
		return
	}
	var dbs []Candidate
	for rows.Next() {
		var c Candidate
		if rows.Scan(&c.Name, &c.Comment) == nil {
			dbs = append(dbs, c)
		}
	}
	rows.Close()
	for _, db := range SelectStale(dbs, currentHash, time.Now()) {
		if templateRE.MatchString(db) {
			_, _ = admin.Exec(ctx, `ALTER DATABASE `+QuoteIdent(db)+` WITH ALLOW_CONNECTIONS true`)
		}
		if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+QuoteIdent(db)+` WITH (FORCE)`); err == nil {
			fmt.Fprintf(log, "pgtemplate: dropped stale %s\n", db)
		}
	}
}

// dropMatching drops pgtest-pattern databases selected by keep. It re-checks the pattern so a
// predicate bug cannot reach a foreign database.
func dropMatching(ctx context.Context, admin *pgxpool.Pool, match func(string) bool) error {
	rows, err := admin.Query(ctx, `SELECT datname FROM pg_database WHERE datname LIKE 'goatos\_pgtest\_%'`)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			names = append(names, n)
		}
	}
	rows.Close()
	for _, n := range names {
		if !IsPgtestDatabase(n) || !match(n) {
			continue
		}
		_, _ = admin.Exec(ctx, `ALTER DATABASE `+QuoteIdent(n)+` WITH ALLOW_CONNECTIONS true`)
		if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+QuoteIdent(n)+` WITH (FORCE)`); err != nil {
			return fmt.Errorf("drop leftover %s: %w", n, err)
		}
	}
	return nil
}

// IsPgtestDatabase reports whether a database name belongs to this harness (and so may ever be
// dropped by it).
func IsPgtestDatabase(name string) bool {
	return templateRE.MatchString(name) || buildRE.MatchString(name) || cloneRE.MatchString(name)
}

func unix(s string) (time.Time, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(n, 0), true
}

func tail(b []byte, n int) []byte {
	if len(b) > n {
		return b[len(b)-n:]
	}
	return b
}
