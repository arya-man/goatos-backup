package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

func main() {
	var tenantID string
	var userID string
	var role string
	flag.StringVar(&tenantID, "tenant-id", "", "tenant UUID for the tenant-scope grant")
	flag.StringVar(&userID, "user-id", "", "user UUID for the grant")
	flag.StringVar(&role, "role", "", "required role: admin, verifier, park_head, operator, or ceo_internal")
	flag.Parse()

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if err := validateLocalTarget(os.Getenv("GOATOS_ENV"), databaseURL); err != nil {
		fail("%v", err)
	}
	if !validRole(role) {
		fail("invalid or missing role: %q", role)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		fail("open postgres pool: %v", err)
	}
	defer pool.Close()

	var grantID string
	if err := pool.QueryRow(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1, $2, $3, 'tenant', $1, 'active', now())
RETURNING grant_id::text
`, tenantID, userID, role).Scan(&grantID); err != nil {
		fail("insert local dev grant: %v", err)
	}
	fmt.Printf("seeded tenant grant %s for user %s role %s tenant %s\n", grantID, userID, role, tenantID)
}

func validateLocalTarget(env, databaseURL string) error {
	env = strings.ToLower(strings.TrimSpace(env))
	dbLower := strings.ToLower(databaseURL)
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if strings.Contains(env, "prod") || strings.Contains(env, "stage") || strings.Contains(dbLower, "prod") || strings.Contains(dbLower, "stage") {
		return fmt.Errorf("refusing to seed grants for production/staging-looking target")
	}
	if env != "local" && env != "dev" && env != "test" {
		return fmt.Errorf("GOATOS_ENV must be local, dev, or test for seed-dev-grant")
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if !isLocalHost(cfg.ConnConfig.Host) {
		return fmt.Errorf("refusing to seed grants against non-local database host %q", cfg.ConnConfig.Host)
	}
	return nil
}

func isLocalHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return true
	}
	if strings.HasPrefix(host, "/") {
		return isAllowedLocalSocketHost(host)
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isAllowedLocalSocketHost(host string) bool {
	socketHost := path.Clean(host)
	allowedDirs := [...]string{
		"/tmp",
		"/private/tmp",
		"/run/postgresql",
		"/var/run/postgresql",
	}
	for _, dir := range allowedDirs {
		if socketHost == dir || strings.HasPrefix(socketHost, dir+"/") {
			return true
		}
	}
	return false
}

func validRole(role string) bool {
	switch role {
	case permissions.RoleAdmin, permissions.RoleVerifier, permissions.RoleParkHead, permissions.RoleOperator, permissions.RoleCEOInternal:
		return true
	default:
		return false
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
