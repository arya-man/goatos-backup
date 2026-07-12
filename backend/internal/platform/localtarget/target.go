package localtarget

import (
	"fmt"
	"net"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	localDockerDatabaseHostsEnv  = "GOATOS_LOCAL_DOCKER_DATABASE_HOSTS"
	devCloudSQLAllowEnv          = "GOATOS_ALLOW_DEV_CLOUDSQL_TARGET"
	devCloudSQLConnectionNameEnv = "GOATOS_DEV_CLOUDSQL_CONNECTION_NAME"
	devCloudSQLProjectID         = "goatos-dev"
	devCloudSQLRegion            = "asia-south1"
	stgCloudSQLAllowEnv          = "GOATOS_ALLOW_STG_CLOUDSQL_TARGET"
	stgCloudSQLConnectionNameEnv = "GOATOS_STG_CLOUDSQL_CONNECTION_NAME"
	stgCloudSQLProjectID         = "goatos-stg"
	stgCloudSQLRegion            = "asia-south1"
)

// ValidateLocalDatabaseTarget rejects DB targets that are not safe local/dev
// Postgres endpoints. It is for local rehearsal and dev helper binaries only.
func ValidateLocalDatabaseTarget(commandName, env, databaseURL string, allowedEnvs ...string) error {
	commandName = normalizedCommandName(commandName)
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
	if looksSharedUnsafeTarget(env) || looksSharedUnsafeTarget(databaseURL) {
		return fmt.Errorf("refusing %s against production/staging-looking target", commandName)
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL for %s: %w", commandName, err)
	}
	if isCloudSQLTarget(databaseURL, cfg.ConnConfig.Host) {
		return validateDevCloudSQLTarget(commandName, env, databaseURL, cfg.ConnConfig.Host)
	}
	if !IsLocalHost(cfg.ConnConfig.Host) && !isExplicitLocalDockerHost(env, cfg.ConnConfig.Host) {
		return fmt.Errorf("refusing %s against non-local database host %q", commandName, cfg.ConnConfig.Host)
	}
	return nil
}

// isExplicitLocalDockerHost permits a named Compose-network Postgres service only
// for GOATOS_ENV=local. Container DNS names do not resolve to loopback inside the
// container, so local Docker callers must opt in to the exact hostname. This is
// deliberately not available in dev/stg/prod and shared-target tokens are still
// rejected before this function is reached.
func isExplicitLocalDockerHost(env, host string) bool {
	if strings.ToLower(strings.TrimSpace(env)) != "local" {
		return false
	}
	host = strings.ToLower(strings.TrimSpace(strings.Trim(host, "[]")))
	if host == "" || net.ParseIP(host) != nil || strings.ContainsAny(host, ". /:@") || !isDockerServiceName(host) {
		return false
	}
	for _, allowed := range strings.Split(os.Getenv(localDockerDatabaseHostsEnv), ",") {
		if host == strings.ToLower(strings.TrimSpace(allowed)) && !looksSharedUnsafeTarget(host) {
			return true
		}
	}
	return false
}

func isDockerServiceName(host string) bool {
	for _, char := range host {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

// ValidateDevCloudSQLDatabaseTarget rejects every target except the explicitly
// opted-in goatos-dev Cloud SQL instance. Use it for shared dev bring-up jobs
// where a local fallback would be unsafe.
func ValidateDevCloudSQLDatabaseTarget(commandName, env, databaseURL string) error {
	commandName = normalizedCommandName(commandName)
	env = strings.ToLower(strings.TrimSpace(env))
	if env != "dev" {
		return fmt.Errorf("GOATOS_ENV must be dev for %s", commandName)
	}
	if strings.TrimSpace(databaseURL) == "" {
		return fmt.Errorf("DATABASE_URL is required for %s", commandName)
	}
	if looksSharedUnsafeTarget(env) || looksSharedUnsafeTarget(databaseURL) {
		return fmt.Errorf("refusing %s against production/staging-looking target", commandName)
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL for %s: %w", commandName, err)
	}
	if !isCloudSQLTarget(databaseURL, cfg.ConnConfig.Host) {
		return fmt.Errorf("refusing %s against non-Cloud SQL database host %q", commandName, cfg.ConnConfig.Host)
	}
	return validateDevCloudSQLTarget(commandName, env, databaseURL, cfg.ConnConfig.Host)
}

// ValidateStagingCloudSQLDatabaseTarget rejects every target except the
// explicitly opted-in goatos-stg Cloud SQL instance. Use it for shared staging
// bring-up jobs that must never silently point at local/dev/prod.
func ValidateStagingCloudSQLDatabaseTarget(commandName, env, databaseURL string) error {
	commandName = normalizedCommandName(commandName)
	env = strings.ToLower(strings.TrimSpace(env))
	if env != "stg" {
		return fmt.Errorf("GOATOS_ENV must be stg for %s", commandName)
	}
	if strings.TrimSpace(databaseURL) == "" {
		return fmt.Errorf("DATABASE_URL is required for %s", commandName)
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL for %s: %w", commandName, err)
	}
	if !isCloudSQLTarget(databaseURL, cfg.ConnConfig.Host) {
		return fmt.Errorf("refusing %s against non-Cloud SQL database host %q", commandName, cfg.ConnConfig.Host)
	}
	return validateStagingCloudSQLTarget(commandName, env, databaseURL, cfg.ConnConfig.Host)
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

func normalizedCommandName(commandName string) string {
	commandName = strings.TrimSpace(commandName)
	if commandName == "" {
		return "local command"
	}
	return commandName
}

func looksSharedUnsafeTarget(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	blocked := [...]string{
		"prod",
		"production",
		"stage",
		"staging",
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

func isCloudSQLTarget(databaseURL, host string) bool {
	value := strings.ToLower(databaseURL + " " + host)
	if strings.Contains(value, "cloudsql") || strings.Contains(value, "/cloudsql/") {
		return true
	}
	_, ok := cloudSQLConnectionNameFromHost(host)
	return ok
}

func validateDevCloudSQLTarget(commandName, env, databaseURL, host string) error {
	if env != "dev" {
		return fmt.Errorf("refusing %s against Cloud SQL target without GOATOS_ENV=dev", commandName)
	}
	if !truthy(os.Getenv(devCloudSQLAllowEnv)) {
		return fmt.Errorf("refusing %s against Cloud SQL target without %s=true", commandName, devCloudSQLAllowEnv)
	}
	expected := strings.TrimSpace(os.Getenv(devCloudSQLConnectionNameEnv))
	if expected == "" {
		return fmt.Errorf("refusing %s against Cloud SQL target without %s=goatos-dev:asia-south1:<instance>", commandName, devCloudSQLConnectionNameEnv)
	}
	if !isExpectedDevConnectionName(expected) {
		return fmt.Errorf("refusing %s against Cloud SQL target because %s must be goatos-dev:asia-south1:<instance>", commandName, devCloudSQLConnectionNameEnv)
	}
	actual, ok := cloudSQLConnectionNameFromHost(host)
	if !ok {
		actual, ok = cloudSQLConnectionNameFromText(databaseURL)
	}
	if !ok {
		return fmt.Errorf("refusing %s against Cloud SQL target without exact %s or /cloudsql/%s socket path", commandName, expected, expected)
	}
	if actual != expected {
		return fmt.Errorf("refusing %s against Cloud SQL target %q; expected exact %s", commandName, actual, expected)
	}
	return nil
}

func isExpectedDevConnectionName(value string) bool {
	return isExpectedConnectionName(value, devCloudSQLProjectID, devCloudSQLRegion) &&
		!looksSharedUnsafeTarget(value)
}

func validateStagingCloudSQLTarget(commandName, env, databaseURL, host string) error {
	if env != "stg" {
		return fmt.Errorf("refusing %s against Cloud SQL target without GOATOS_ENV=stg", commandName)
	}
	if !truthy(os.Getenv(stgCloudSQLAllowEnv)) {
		return fmt.Errorf("refusing %s against Cloud SQL target without %s=true", commandName, stgCloudSQLAllowEnv)
	}
	expected := strings.TrimSpace(os.Getenv(stgCloudSQLConnectionNameEnv))
	if expected == "" {
		return fmt.Errorf("refusing %s against Cloud SQL target without %s=goatos-stg:asia-south1:<instance>", commandName, stgCloudSQLConnectionNameEnv)
	}
	if !isExpectedStagingConnectionName(expected) {
		return fmt.Errorf("refusing %s against Cloud SQL target because %s must be goatos-stg:asia-south1:<instance>", commandName, stgCloudSQLConnectionNameEnv)
	}
	actual, ok := cloudSQLConnectionNameFromHost(host)
	if !ok {
		actual, ok = cloudSQLConnectionNameFromText(databaseURL)
	}
	if !ok {
		return fmt.Errorf("refusing %s against Cloud SQL target without exact %s or /cloudsql/%s socket path", commandName, expected, expected)
	}
	if actual != expected {
		return fmt.Errorf("refusing %s against Cloud SQL target %q; expected exact %s", commandName, actual, expected)
	}
	return nil
}

// isExpectedStagingConnectionName intentionally does NOT apply looksSharedUnsafeTarget the way
// isExpectedDevConnectionName does: the staging instance's own connection name (goatos-stg:...)
// contains the "goatos-stg"/"stage" tokens that guard blocks, so running it here would reject
// every legitimate staging target. Safety comes instead from the exact goatos-stg project+region
// prefix here plus the exact-match against GOATOS_STG_CLOUDSQL_CONNECTION_NAME in
// validateStagingCloudSQLTarget, which keeps the target pinned inside the goatos-stg project.
func isExpectedStagingConnectionName(value string) bool {
	return isExpectedConnectionName(value, stgCloudSQLProjectID, stgCloudSQLRegion)
}

func isExpectedConnectionName(value, projectID, region string) bool {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return false
	}
	return parts[0] == projectID &&
		parts[1] == region &&
		strings.TrimSpace(parts[2]) != "" &&
		!strings.Contains(parts[2], "/")
}

func cloudSQLConnectionNameFromHost(host string) (string, bool) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", false
	}
	if strings.HasPrefix(host, "/cloudsql/") {
		return cloudSQLConnectionNameFromPath(host)
	}
	if isConnectionNameShape(host) {
		return host, true
	}
	return "", false
}

func cloudSQLConnectionNameFromPath(value string) (string, bool) {
	cleaned := path.Clean(strings.TrimSpace(value))
	connectionName := strings.TrimPrefix(cleaned, "/cloudsql/")
	if connectionName == cleaned {
		return "", false
	}
	if !isConnectionNameShape(connectionName) {
		return "", false
	}
	return connectionName, true
}

func cloudSQLConnectionNameFromText(value string) (string, bool) {
	for _, field := range strings.Fields(value) {
		if idx := strings.Index(field, "/cloudsql/"); idx >= 0 {
			candidate := field[idx:]
			for _, delimiter := range []string{"&", "?", "#"} {
				if cut := strings.Index(candidate, delimiter); cut >= 0 {
					candidate = candidate[:cut]
				}
			}
			if connectionName, ok := cloudSQLConnectionNameFromPath(candidate); ok {
				return connectionName, true
			}
		}
	}
	return "", false
}

func isConnectionNameShape(value string) bool {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" || strings.ContainsAny(part, "/\\") {
			return false
		}
	}
	return true
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "y", "yes":
		return true
	default:
		return false
	}
}
