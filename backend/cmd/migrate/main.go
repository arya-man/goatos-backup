package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

type cliConfig struct {
	MigrationsDir           string
	Timeout                 time.Duration
	DryRun                  bool
	AllowLocalChecksumDrift bool
}

type migrationFile struct {
	Version  string
	Filename string
	Path     string
	Checksum string
	SQL      string
	NoTx     bool
}

func main() {
	log := observability.New(observability.Config{Service: "migrate"})
	if err := run(context.Background(), os.Args[1:], log); err != nil {
		log.Error("migration_failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, log *slog.Logger) error {
	cfg, err := parseFlags(args)
	if err != nil {
		return err
	}
	migrations, err := loadMigrations(cfg.MigrationsDir)
	if err != nil {
		return err
	}
	if len(migrations) == 0 {
		return fmt.Errorf("no migrations found in %s", cfg.MigrationsDir)
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	pgCfg := platformpg.ConfigFromEnv()
	if err := validateMigrationTarget(pgCfg.DatabaseURL); err != nil {
		return err
	}
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	allowChecksumDrift := false
	if cfg.AllowLocalChecksumDrift {
		if err := validateLocalChecksumDriftTarget(pgCfg.DatabaseURL); err != nil {
			return err
		}
		allowChecksumDrift = true
	}

	return applyMigrations(ctx, pool, migrations, cfg.DryRun, allowChecksumDrift, log)
}

func validateMigrationTarget(databaseURL string) error {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("GOATOS_ENV")))
	if env == "stg" {
		return localtarget.ValidateStagingCloudSQLDatabaseTarget("migrate", env, databaseURL)
	}
	return localtarget.ValidateLocalDatabaseTarget("migrate", env, databaseURL, "local", "dev")
}

func validateLocalChecksumDriftTarget(databaseURL string) error {
	if strings.ToLower(strings.TrimSpace(os.Getenv("GOATOS_ENV"))) != "local" {
		return errors.New("-allow-local-checksum-drift requires GOATOS_ENV=local")
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL for local checksum drift allowance: %w", err)
	}
	if !localtarget.IsLocalHost(cfg.ConnConfig.Host) {
		return fmt.Errorf("-allow-local-checksum-drift requires a loopback/local database host, got %q", cfg.ConnConfig.Host)
	}
	return nil
}

func parseFlags(args []string) (cliConfig, error) {
	cfg := cliConfig{
		MigrationsDir: defaultMigrationsDir(),
		Timeout:       10 * time.Minute,
	}
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.StringVar(&cfg.MigrationsDir, "migrations-dir", cfg.MigrationsDir, "directory containing postgres migration .sql files")
	fs.DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "migration timeout")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "list pending migrations without applying them")
	fs.BoolVar(&cfg.AllowLocalChecksumDrift, "allow-local-checksum-drift", false, "local-only: continue past historical checksum drift so a throwaway E2E DB can reach head")
	if err := fs.Parse(args); err != nil {
		return cliConfig{}, err
	}
	if strings.TrimSpace(cfg.MigrationsDir) == "" {
		return cliConfig{}, errors.New("migrations-dir is required")
	}
	if cfg.Timeout <= 0 {
		return cliConfig{}, errors.New("timeout must be positive")
	}
	return cfg, nil
}

func defaultMigrationsDir() string {
	if raw := strings.TrimSpace(os.Getenv("GOATOS_MIGRATIONS_DIR")); raw != "" {
		return raw
	}
	candidates := []string{
		filepath.Join("backend", "migrations", "postgres"),
		filepath.Join("migrations", "postgres"),
		filepath.Join("/app", "backend", "migrations", "postgres"),
		filepath.Join("/app", "migrations", "postgres"),
	}
	for _, candidate := range candidates {
		if stat, err := os.Stat(candidate); err == nil && stat.IsDir() {
			return candidate
		}
	}
	return filepath.Join("backend", "migrations", "postgres")
}

func loadMigrations(dir string) ([]migrationFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	files := make([]migrationFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		rawSQL := string(data)
		upSQL, err := extractGooseUp(rawSQL)
		if err != nil {
			return nil, fmt.Errorf("parse migration %s: %w", entry.Name(), err)
		}
		sum := sha256.Sum256(data)
		files = append(files, migrationFile{
			Version:  strings.TrimSuffix(entry.Name(), ".sql"),
			Filename: entry.Name(),
			Path:     path,
			Checksum: "sha256:" + hex.EncodeToString(sum[:]),
			SQL:      upSQL,
			NoTx:     hasGooseNoTransaction(rawSQL),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Filename < files[j].Filename
	})
	return files, nil
}

func extractGooseUp(sql string) (string, error) {
	lines := strings.Split(sql, "\n")
	inUp := false
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "-- +goose Up"):
			inUp = true
			continue
		case strings.HasPrefix(trimmed, "-- +goose Down"):
			inUp = false
			continue
		case inUp:
			out = append(out, line)
		}
	}
	upSQL := strings.TrimSpace(strings.Join(out, "\n"))
	if upSQL == "" {
		return "", errors.New("missing -- +goose Up section")
	}
	return upSQL, nil
}

func hasGooseNoTransaction(sql string) bool {
	for _, line := range strings.Split(sql, "\n") {
		if strings.EqualFold(strings.TrimSpace(line), "-- +goose NO TRANSACTION") {
			return true
		}
	}
	return false
}

func applyMigrations(ctx context.Context, pool *pgxpool.Pool, migrations []migrationFile, dryRun bool, allowChecksumDrift bool, log *slog.Logger) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtext('goatos:postgres:migrate'))`); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtext('goatos:postgres:migrate'))`)
	}()

	if _, err := conn.Exec(ctx, `
CREATE TABLE IF NOT EXISTS public.goatos_schema_migrations (
  version text PRIMARY KEY,
  filename text NOT NULL,
  checksum text NOT NULL,
  applied_at timestamptz NOT NULL DEFAULT now()
)`); err != nil {
		return fmt.Errorf("ensure migrations table: %w", err)
	}

	for _, migration := range migrations {
		appliedChecksum, err := appliedMigrationChecksum(ctx, conn, migration.Version)
		if err != nil {
			return err
		}
		if appliedChecksum != "" {
			if appliedChecksum != migration.Checksum {
				if isAllowedHistoricalChecksum(migration, appliedChecksum) {
					log.Warn("migration_historical_checksum_accepted",
						slog.String("version", migration.Version),
						slog.String("filename", migration.Filename),
						slog.String("applied_checksum", appliedChecksum),
						slog.String("current_checksum", migration.Checksum))
					continue
				}
				if allowChecksumDrift {
					log.Warn("migration_checksum_drift_ignored_local",
						slog.String("version", migration.Version),
						slog.String("filename", migration.Filename),
						slog.String("applied_checksum", appliedChecksum),
						slog.String("current_checksum", migration.Checksum))
					continue
				}
				return fmt.Errorf("migration %s was already applied with checksum %s, current %s", migration.Version, appliedChecksum, migration.Checksum)
			}
			// Guard against BUG-M11(b): `CREATE INDEX CONCURRENTLY IF NOT EXISTS`
			// matches by name only. If a prior run of this migration aborted
			// mid-build (killed process, deploy timeout, etc.), it can leave an
			// INVALID index under that name; a later run that reaches this
			// checksum-matched branch would otherwise skip re-checking it forever,
			// silently recording the migration as applied with zero protection
			// from the index it was supposed to add. Re-validate (and, if the
			// migration's SQL is idempotent-safe to rerun, repair) every time we
			// see this migration again, not just the first time it is applied.
			if err := ensureConcurrentIndexesValid(ctx, conn, migration, log); err != nil {
				return err
			}
			continue
		}
		if dryRun {
			log.Info("migration_pending", slog.String("version", migration.Version), slog.String("filename", migration.Filename))
			continue
		}
		log.Info("migration_applying", slog.String("version", migration.Version), slog.String("filename", migration.Filename), slog.Bool("no_transaction", migration.NoTx))
		if migration.NoTx {
			if err := execMigrationSQL(ctx, conn, migration.SQL); err != nil {
				return fmt.Errorf("apply migration %s: %w", migration.Version, err)
			}
			if err := ensureConcurrentIndexesValid(ctx, conn, migration, log); err != nil {
				return err
			}
			if err := recordMigration(ctx, conn, migration); err != nil {
				return err
			}
			continue
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", migration.Version, err)
		}
		if err := execMigrationSQL(ctx, tx, migration.SQL); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", migration.Version, err)
		}
		if err := recordMigration(ctx, tx, migration); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", migration.Version, err)
		}
	}
	return nil
}

type checksumPair struct {
	current string
	applied string
}

// These pairs are deliberately exact in both directions. They preserve checksum protection
// while allowing audited local databases created during the July 2026 branch convergence to
// reach the current forward-only schema. Never add a version here without first comparing a
// restored database against a clean migration-to-head database and proving the data invariants.
//
// BOTH sides must carry the "sha256:" prefix that loadMigrations writes. The applied side is
// compared verbatim against the checksum column this runner recorded, so a bare-hex value can
// never match and silently turns the entry into a dead allowance. Package-level so the format
// invariant can be asserted against the real map instead of a hand-copied test literal.
var allowedHistoricalChecksums = map[string]checksumPair{
	"000001_goatos_clean_slate_baseline": {
		current: "sha256:2ecaf35d57ff448fcd2f293e502074c1fe5c36e509a807fa482ab609659d6bb0",
		applied: "sha256:b29305e89e75b2ef15bb80a79c704720941d1d3b8cc8e10de085655b6290d349",
	},
	"000035_death_upload_before_approval": {
		current: "sha256:cf443ab9807a012553d04ad764577f1885ea990e0fd4b370b58bd9f8f1a95efb",
		applied: "sha256:daca76c460dba159de9d9dff35c1694638e093e91ae7773ce13a6a9b1d6a15f5",
	},
	"000036_death_two_operator_actions": {
		current: "sha256:306c83248d800baba7d47b39681e8133a4f3d4fe989402f6ebf74461b4a0752a",
		applied: "sha256:fa64a6e997e28a0a0222271ad7862f9afd522192e42b002cc3ac21f3feb91abd",
	},
	"000038_counts_submitter_pending_index": {
		current: "sha256:99fa47f84cc38475acc609ee20926cbb5a1f7f3cf8f43a8fae436dbd2377e6bd",
		applied: "sha256:7ec8991c8283f36c55fc87025512f671d1378e93df7b021bf353b46056dc4c3c",
	},
	"000041_birth_mother_video_medicine": {
		current: "sha256:764c18b63e8dcb6cdacfbfad22f91aed7f718b0b43aa56466385620d6b7c56b6",
		applied: "sha256:b1350ee397b966168b3b27e188530de94d50d2e89114b76dfe98842b4b79751b",
	},
	"000042_birth_litter_video_contract": {
		current: "sha256:064fae166174f4397d8baedfc318a5adf9e94ff01df69db3bae174ae639e7ae6",
		applied: "sha256:3bfea97c8be106a3b2a2acaa155c39f569b989cdcd497ce7f5dd4e5aadfcbd2e",
	},
	"000044_birth_ors_second_round_gate": {
		current: "sha256:36ba3dbc1043da7f2aa99d92bd0b558007469f321ecc3e1f47765e29f20d859b",
		applied: "sha256:e069f6eb6a7bcd1db8c57cb0d50e4b34e5a439cee2a4122723b2319bba35e59a",
	},
	"000045_birth_ors_reopened_card_sync": {
		current: "sha256:e97e70e0a86531a451f21ae3cc65f9ff774404c2b4b8a9873f8aeba7061d0464",
		applied: "sha256:dce6a89bff449645c04e0de43ff1bcdb60fe52aba1eb9658b3d7ef4b30658fc5",
	},
	"000046_birth_weight_and_colostrum_repair": {
		current: "sha256:0f0873a5149c5582ccfd96d830674669cd343fbf1efb29a4168c96b5eb0a8d06",
		applied: "sha256:ef8eb3e8f4ab306b9270831d79aaaa910b646a08ecc70d3988ac5ac073d5e0d7",
	},
	"000047_birth_colostrum_card_counts": {
		current: "sha256:0abe9e413b9793a3b0a133c09e828adac0e8d7ac8f57f974d880a3c62ddbdacf",
		applied: "sha256:15053660bb0686a60e496ed645bad7db72d1cab7915aa29e7e859cf3fe9e6274",
	},
	"000052_shifting_management_stage_selection": {
		current: "sha256:a0b12a06829e63aed9204b5755f522d86c265be46d32778c4efdd22c13070662",
		applied: "sha256:65e4e4b2dc1cde852eadd602f06a6baa8b306a54f0538cbbf14ee327bea8be64",
	},
}

func isAllowedHistoricalChecksum(migration migrationFile, appliedChecksum string) bool {
	pair, ok := allowedHistoricalChecksums[migration.Version]
	if !ok {
		return false
	}
	return migration.Checksum == pair.current && appliedChecksum == pair.applied
}

func appliedMigrationChecksum(ctx context.Context, conn *pgxpool.Conn, version string) (string, error) {
	var checksum string
	err := conn.QueryRow(ctx, `SELECT checksum FROM public.goatos_schema_migrations WHERE version = $1`, version).Scan(&checksum)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("check migration %s: %w", version, err)
	}
	return checksum, nil
}

// concurrentIndexNameRe extracts the target index name from
// `CREATE [UNIQUE] INDEX CONCURRENTLY [IF NOT EXISTS] <name> ON ...`. It
// intentionally does not attempt to parse `DROP INDEX CONCURRENTLY`: a
// missing-after-drop index is a normal, already-visible failure (the DROP or
// a subsequent statement errors), not the silent-skip failure mode this guard
// exists for.
var concurrentIndexNameRe = regexp.MustCompile(`(?is)CREATE\s+(?:UNIQUE\s+)?INDEX\s+CONCURRENTLY\s+(?:IF\s+NOT\s+EXISTS\s+)?("?[a-zA-Z_][\w]*"?)`)

// blank replaces a byte of blanked-out text with a space, preserving newlines
// so line-oriented reading of the result still lines up with the source.
func blank(c byte) byte {
	if c == '\n' {
		return c
	}
	return ' '
}

// stripSQLNoise removes `-- line` and `/* block */` comments so the
// index-name regex cannot match PROSE. The 000001 baseline contains the line
//
//	-- Lock-safe on hot table outbox_messages: CREATE UNIQUE INDEX CONCURRENTLY with the NEW predicate
//
// which matched as an index literally named "with". No such index exists, so
// ensureConcurrentIndexesValid's repair path fired and re-ran the whole
// baseline, which then died on `CREATE SCHEMA analytics` already existing --
// i.e. `migrate up` could not bring up ANY fresh database. Six migrations in
// this repo carry such prose.
//
// It blanks three kinds of non-DDL text, leaving only executable statement
// text for the regex to match:
//
//   - `--` line and `/* */` block comments (removed entirely);
//   - the interior of single-quoted string literals (blanked, delimiters kept)
//     -- prose in a literal must not be read as an index name either;
//   - the interior of dollar-quoted `$tag$ ... $tag$` bodies (blanked) --
//     CREATE INDEX CONCURRENTLY cannot run inside a function or DO block, so
//     nothing there is ever a real target.
//
// Double-quoted identifiers are copied through byte-for-byte, because THEY are
// the index names. Statement text is never altered, so this cannot mangle a
// real name.
//
// Dollar-tag tracking (the same lexical state splitSQLStatements keeps) is
// load-bearing: these migrations use `$$` bodies heavily and such a body may
// contain an apostrophe that is NOT a string delimiter (`$$ ... can't ... $$`).
// Without it that apostrophe would open a phantom string literal and suppress
// comment stripping for the rest of the file, reintroducing this bug.
func stripSQLNoise(sql string) string {
	var b strings.Builder
	b.Grow(len(sql))
	var dollarTag string
	inLine, inBlock, inSingle, inDouble := false, false, false, false
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		next := byte(0)
		if i+1 < len(sql) {
			next = sql[i+1]
		}
		switch {
		case inLine:
			if c == '\n' {
				inLine = false
				b.WriteByte(c)
			}
		case inBlock:
			if c == '*' && next == '/' {
				inBlock = false
				i++
			}
		case dollarTag != "":
			if strings.HasPrefix(sql[i:], dollarTag) {
				b.WriteString(dollarTag)
				i += len(dollarTag) - 1
				dollarTag = ""
				break
			}
			b.WriteByte(blank(c))
		case inSingle:
			if c == '\'' {
				inSingle = false
				b.WriteByte(c)
				break
			}
			b.WriteByte(blank(c))
		case inDouble:
			b.WriteByte(c)
			if c == '"' {
				inDouble = false
			}
		case c == '-' && next == '-':
			inLine = true
			i++
		case c == '/' && next == '*':
			inBlock = true
			i++
		case c == '\'':
			inSingle = true
			b.WriteByte(c)
		case c == '"':
			inDouble = true
			b.WriteByte(c)
		case c == '$':
			if tag, ok := readDollarTag(sql[i:]); ok {
				b.WriteString(tag)
				i += len(tag) - 1
				dollarTag = tag
				continue
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// extractConcurrentIndexNames returns the bare (unquoted) names of every index
// a migration's Up SQL builds with CREATE INDEX CONCURRENTLY / CREATE UNIQUE
// INDEX CONCURRENTLY. Comments are stripped first so prose cannot be mistaken
// for an index name -- see stripSQLNoise.
func extractConcurrentIndexNames(sql string) []string {
	matches := concurrentIndexNameRe.FindAllStringSubmatch(stripSQLNoise(sql), -1)
	if len(matches) == 0 {
		return nil
	}
	names := make([]string, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		name := strings.Trim(m[1], `"`)
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// ensureConcurrentIndexesValid is the general guard for BUG-M11(b): any
// migration that builds an index with CREATE [UNIQUE] INDEX CONCURRENTLY is
// checked for pg_index.indisvalid after it runs (or, for a migration this
// runner has already recorded as applied, every time migrate sees it again).
// `CREATE INDEX CONCURRENTLY IF NOT EXISTS` only matches by name -- if an
// earlier build aborted partway through and left an INVALID index under that
// name, a retry silently no-ops the CREATE and this migration would otherwise
// be recorded as applied with zero enforcement from the index it exists to
// add. On finding an invalid index this guard drops it and re-runs the
// migration's own SQL once to rebuild it (the migrations in this repo that
// build a CONCURRENTLY index are written to be idempotent/re-runnable), then
// fails loudly if it is still invalid.
func ensureConcurrentIndexesValid(ctx context.Context, conn *pgxpool.Conn, migration migrationFile, log *slog.Logger) error {
	names := extractConcurrentIndexNames(migration.SQL)
	if len(names) == 0 {
		return nil
	}
	rebuiltOnce := false
	for _, name := range names {
		valid, exists, err := concurrentIndexValidity(ctx, conn, name)
		if err != nil {
			return fmt.Errorf("check index validity for %s (migration %s): %w", name, migration.Version, err)
		}
		if exists && valid {
			continue
		}
		log.Warn("concurrent_index_invalid_repairing",
			slog.String("version", migration.Version),
			slog.String("filename", migration.Filename),
			slog.String("index", name),
			slog.Bool("index_existed", exists))
		if exists {
			if _, err := conn.Exec(ctx, fmt.Sprintf("DROP INDEX CONCURRENTLY IF EXISTS %s", pgQuoteIdent(name))); err != nil {
				return fmt.Errorf("drop invalid index %s before rebuild (migration %s): %w", name, migration.Version, err)
			}
		}
		if !rebuiltOnce {
			if err := execMigrationSQL(ctx, conn, migration.SQL); err != nil {
				return fmt.Errorf("rebuild concurrent index %s (migration %s): %w", name, migration.Version, err)
			}
			rebuiltOnce = true
		}
		validAfter, existsAfter, err := concurrentIndexValidity(ctx, conn, name)
		if err != nil {
			return fmt.Errorf("re-check index validity for %s (migration %s): %w", name, migration.Version, err)
		}
		if !existsAfter || !validAfter {
			return fmt.Errorf("concurrent index %s (migration %s) is still invalid after a rebuild attempt -- manual intervention required", name, migration.Version)
		}
		log.Info("concurrent_index_repaired", slog.String("version", migration.Version), slog.String("index", name))
	}
	return nil
}

// pgIdentRe restricts index names this guard will interpolate into a DDL
// statement to the identifier shape Postgres migrations in this repo
// actually use (the same shape concurrentIndexNameRe already matched them
// with), so this is not treating attacker-controlled input as SQL -- these
// names only ever come from migration files this repo's own developers wrote.
var pgIdentRe = regexp.MustCompile(`^[a-zA-Z_][\w]*$`)

func pgQuoteIdent(name string) string {
	if !pgIdentRe.MatchString(name) {
		// Defensive fallback; concurrentIndexNameRe cannot actually produce
		// a name that fails this, but never interpolate an unvalidated string.
		return pgx.Identifier{name}.Sanitize()
	}
	return "public." + name
}

func concurrentIndexValidity(ctx context.Context, conn *pgxpool.Conn, name string) (valid bool, exists bool, err error) {
	err = conn.QueryRow(ctx, `
SELECT i.indisvalid
FROM pg_class c
JOIN pg_index i ON i.indexrelid = c.oid
WHERE c.relname = $1`, name).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return valid, true, nil
}

type migrationExecutor interface {
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
}

func recordMigration(ctx context.Context, exec migrationExecutor, migration migrationFile) error {
	if _, err := exec.Exec(ctx, `
INSERT INTO public.goatos_schema_migrations (version, filename, checksum)
VALUES ($1, $2, $3)`,
		migration.Version, migration.Filename, migration.Checksum,
	); err != nil {
		return fmt.Errorf("record migration %s: %w", migration.Version, err)
	}
	return nil
}

func execMigrationSQL(ctx context.Context, exec migrationExecutor, sql string) error {
	statements, err := splitSQLStatements(sql)
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if _, err := exec.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func splitSQLStatements(sql string) ([]string, error) {
	var statements []string
	var current strings.Builder
	var dollarTag string
	inSingleQuote := false
	inDoubleQuote := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		next := byte(0)
		if i+1 < len(sql) {
			next = sql[i+1]
		}

		current.WriteByte(ch)

		switch {
		case inLineComment:
			if ch == '\n' {
				inLineComment = false
			}
			continue
		case inBlockComment:
			if ch == '*' && next == '/' {
				current.WriteByte(next)
				i++
				inBlockComment = false
			}
			continue
		case dollarTag != "":
			if strings.HasPrefix(sql[i:], dollarTag) {
				for j := 1; j < len(dollarTag); j++ {
					current.WriteByte(sql[i+j])
				}
				i += len(dollarTag) - 1
				dollarTag = ""
			}
			continue
		case inSingleQuote:
			if ch == '\'' && next == '\'' {
				current.WriteByte(next)
				i++
				continue
			}
			if ch == '\'' {
				inSingleQuote = false
			}
			continue
		case inDoubleQuote:
			if ch == '"' && next == '"' {
				current.WriteByte(next)
				i++
				continue
			}
			if ch == '"' {
				inDoubleQuote = false
			}
			continue
		}

		if ch == '-' && next == '-' {
			current.WriteByte(next)
			i++
			inLineComment = true
			continue
		}
		if ch == '/' && next == '*' {
			current.WriteByte(next)
			i++
			inBlockComment = true
			continue
		}
		if ch == '\'' {
			inSingleQuote = true
			continue
		}
		if ch == '"' {
			inDoubleQuote = true
			continue
		}
		if ch == '$' {
			if tag, ok := readDollarTag(sql[i:]); ok {
				for j := 1; j < len(tag); j++ {
					current.WriteByte(sql[i+j])
				}
				i += len(tag) - 1
				dollarTag = tag
				continue
			}
		}
		if ch == ';' {
			statement := strings.TrimSpace(current.String())
			if statement != "" {
				statements = append(statements, statement)
			}
			current.Reset()
		}
	}
	if inSingleQuote || inDoubleQuote || inBlockComment || dollarTag != "" {
		return nil, errors.New("unterminated SQL quote or comment")
	}
	if statement := strings.TrimSpace(current.String()); statement != "" {
		statements = append(statements, statement)
	}
	return statements, nil
}

func readDollarTag(sql string) (string, bool) {
	if !strings.HasPrefix(sql, "$") {
		return "", false
	}
	for i := 1; i < len(sql); i++ {
		ch := sql[i]
		if ch == '$' {
			return sql[:i+1], true
		}
		if !(ch == '_' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9') {
			return "", false
		}
	}
	return "", false
}
