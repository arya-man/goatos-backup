package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/permissions"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
)

func main() {
	var tenantID string
	var userID string
	var externalSubject string
	var authIssuer string
	var role string
	flag.StringVar(&tenantID, "tenant-id", "", "tenant UUID for the tenant-scope grant")
	flag.StringVar(&userID, "user-id", "", "user UUID for the grant")
	flag.StringVar(&externalSubject, "external-subject", "", "non-UUID IdP subject to map into the grant user UUID")
	flag.StringVar(&authIssuer, "auth-issuer", os.Getenv("GOATOS_AUTH_ISSUER"), "issuer used when mapping an external IdP subject")
	flag.StringVar(&role, "role", "", "required role: admin, verifier, park_head, operator, or ceo_internal")
	flag.Parse()

	var err error
	userID, err = resolveGrantUserID(userID, externalSubject, authIssuer)
	if err != nil {
		fail("%v", err)
	}
	if !isUUID(tenantID) {
		fail("invalid or missing tenant id: %q", tenantID)
	}
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
SELECT grant_id::text
FROM user_scope_grants
WHERE tenant_id = $1
  AND user_id = $2
  AND role = $3
  AND scope_type = 'tenant'
  AND scope_id = $1
  AND status = 'active'
  AND valid_from <= now()
  AND (valid_to IS NULL OR valid_to > now())
ORDER BY valid_from DESC, grant_id DESC
LIMIT 1
`, tenantID, userID, role).Scan(&grantID); err == nil {
		fmt.Printf("dev grant already active %s for user %s role %s tenant %s\n", grantID, userID, role, tenantID)
		return
	} else if !isNoRows(err) {
		fail("query dev grant: %v", err)
	}

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
	return localtarget.ValidateLocalDatabaseTarget("seed-dev-grant", env, databaseURL, "local", "dev", "test")
}

func isLocalHost(host string) bool {
	return localtarget.IsLocalHost(host)
}

func resolveGrantUserID(userID, externalSubject, authIssuer string) (string, error) {
	userID = strings.TrimSpace(userID)
	externalSubject = strings.TrimSpace(externalSubject)
	authIssuer = strings.TrimSpace(authIssuer)
	if userID != "" && externalSubject != "" {
		return "", errors.New("set either -user-id or -external-subject, not both")
	}
	if userID == "" {
		if externalSubject == "" {
			return "", errors.New("missing -user-id or -external-subject")
		}
		if authIssuer == "" {
			return "", errors.New("GOATOS_AUTH_ISSUER or -auth-issuer is required with -external-subject")
		}
		userID = platformauth.StableSubjectID(authIssuer, externalSubject)
	}
	if !isUUID(userID) {
		return "", fmt.Errorf("invalid user id %q", userID)
	}
	return userID, nil
}

func isUUID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

func validRole(role string) bool {
	switch role {
	case permissions.RoleAdmin, permissions.RoleVerifier, permissions.RoleParkHead, permissions.RoleOperator, permissions.RoleCEOInternal:
		return true
	default:
		return false
	}
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
