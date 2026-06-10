package localtarget

import (
	"fmt"
	"net"
	"path"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ValidateLocalDatabaseTarget rejects DB targets that are not safe local/dev
// Postgres endpoints. It is for local rehearsal and dev helper binaries only.
func ValidateLocalDatabaseTarget(commandName, env, databaseURL string, allowedEnvs ...string) error {
	commandName = strings.TrimSpace(commandName)
	if commandName == "" {
		commandName = "local command"
	}
	env = strings.ToLower(strings.TrimSpace(env))
	if len(allowedEnvs) == 0 {
		allowedEnvs = []string{"local", "dev"}
	}
	if !slices.Contains(allowedEnvs, env) {
		return fmt.Errorf("GOATOS_ENV must be one of %s for %s", strings.Join(allowedEnvs, ", "), commandName)
	}
	if strings.TrimSpace(databaseURL) == "" {
		return fmt.Errorf("DATABASE_URL is required for %s", commandName)
	}
	if looksProductionLike(env) || looksProductionLike(databaseURL) {
		return fmt.Errorf("refusing %s against production/staging-looking target", commandName)
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL for %s: %w", commandName, err)
	}
	if !IsLocalHost(cfg.ConnConfig.Host) {
		return fmt.Errorf("refusing %s against non-local database host %q", commandName, cfg.ConnConfig.Host)
	}
	return nil
}

// IsLocalHost allows loopback TCP hosts and known local Postgres socket dirs.
func IsLocalHost(host string) bool {
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

func looksProductionLike(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	blocked := [...]string{
		"prod",
		"production",
		"stage",
		"staging",
		"cloudsql",
		"goatos-stg",
		"goatos-prod",
	}
	for _, token := range blocked {
		if strings.Contains(value, token) {
			return true
		}
	}
	return false
}
