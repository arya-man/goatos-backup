package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/authallow"
	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

type emailFlags []string

func (e *emailFlags) String() string {
	return strings.Join(*e, ",")
}

func (e *emailFlags) Set(value string) error {
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			*e = append(*e, part)
		}
	}
	return nil
}

func main() {
	var tenantID string
	var role string
	var source string
	var emails emailFlags
	flag.StringVar(&tenantID, "tenant-id", "", "tenant UUID for the tenant-scope pending email grants")
	flag.StringVar(&role, "role", permissions.RoleCEOInternal, "role: admin, verifier, park_head, pc_director, operator, or ceo_internal")
	flag.StringVar(&source, "source", "manual_dev_seed", "audit/source label for the pending email grants")
	flag.Var(&emails, "email", "approved email; may be repeated or comma-separated")
	flag.Parse()

	normalizedEmails, err := normalizeEmails(emails)
	if err != nil {
		fail("%v", err)
	}
	if !uuidutil.IsUUIDString(tenantID) {
		fail("invalid or missing tenant id: %q", tenantID)
	}
	if !validRole(role) {
		fail("invalid role: %q", role)
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if err := validateTarget(os.Getenv("GOATOS_ENV"), databaseURL); err != nil {
		fail("%v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		fail("open postgres pool: %v", err)
	}
	defer pool.Close()

	for _, email := range normalizedEmails {
		var pendingGrantID string
		if err := pool.QueryRow(ctx, `
INSERT INTO auth_pending_email_grants (
  tenant_id,
  email,
  normalized_email,
  role,
  scope_type,
  scope_id,
  status,
  valid_from,
  source
) VALUES (
  $1,
  $2,
  $2,
  $3,
  'tenant',
  $1,
  'active',
  now(),
  $4
)
ON CONFLICT (tenant_id, normalized_email, role, scope_type, scope_id)
  WHERE status = 'active' AND valid_to IS NULL
DO UPDATE SET
  email = EXCLUDED.email,
  source = EXCLUDED.source,
  updated_at = now()
RETURNING pending_grant_id::text`, tenantID, email, role, source).Scan(&pendingGrantID); err != nil {
			fail("upsert pending email grant for %s: %v", email, err)
		}
		fmt.Printf("pending email grant %s active for %s role %s tenant %s\n", pendingGrantID, email, role, tenantID)
	}
}

func normalizeEmails(emails []string) ([]string, error) {
	if len(emails) == 0 {
		return nil, errors.New("at least one -email is required")
	}
	if _, err := authallow.NewEmailSet(emails); err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(emails))
	for _, email := range emails {
		value := authallow.NormalizeEmail(email)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	if len(normalized) == 0 {
		return nil, errors.New("at least one valid -email is required")
	}
	return normalized, nil
}

func validateTarget(env, databaseURL string) error {
	if strings.EqualFold(strings.TrimSpace(env), "stg") {
		return localtarget.ValidateStagingCloudSQLDatabaseTarget("seed-dev-email-grants", env, databaseURL)
	}
	return localtarget.ValidateLocalDatabaseTarget("seed-dev-email-grants", env, databaseURL, "local", "dev", "test")
}

func validRole(role string) bool {
	switch role {
	case permissions.RoleAdmin, permissions.RoleVerifier, permissions.RoleParkHead, permissions.RolePCDirector, permissions.RoleOperator, permissions.RoleCEOInternal:
		return true
	default:
		return false
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
