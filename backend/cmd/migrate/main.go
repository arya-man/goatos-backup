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
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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
	return localtarget.ValidateLocalDatabaseTarget("migrate", os.Getenv("GOATOS_ENV"), databaseURL, "local", "dev")
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
		upSQL, err := extractGooseUp(string(data))
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
CREATE TABLE IF NOT EXISTS goatos_schema_migrations (
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
			continue
		}
		if dryRun {
			log.Info("migration_pending", slog.String("version", migration.Version), slog.String("filename", migration.Filename))
			continue
		}
		log.Info("migration_applying", slog.String("version", migration.Version), slog.String("filename", migration.Filename))
		if err := execMigrationSQL(ctx, conn, migration.SQL); err != nil {
			return fmt.Errorf("apply migration %s: %w", migration.Version, err)
		}
		if _, err := conn.Exec(ctx, `
INSERT INTO goatos_schema_migrations (version, filename, checksum)
VALUES ($1, $2, $3)`,
			migration.Version, migration.Filename, migration.Checksum,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", migration.Version, err)
		}
	}
	return nil
}

func appliedMigrationChecksum(ctx context.Context, conn *pgxpool.Conn, version string) (string, error) {
	var checksum string
	err := conn.QueryRow(ctx, `SELECT checksum FROM goatos_schema_migrations WHERE version = $1`, version).Scan(&checksum)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("check migration %s: %w", version, err)
	}
	return checksum, nil
}

func execMigrationSQL(ctx context.Context, conn *pgxpool.Conn, sql string) error {
	statements, err := splitSQLStatements(sql)
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if _, err := conn.Exec(ctx, statement); err != nil {
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
