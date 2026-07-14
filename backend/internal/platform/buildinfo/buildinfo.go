// Package buildinfo holds the build-time identity of the running binary so
// diagnostic surfaces (the /version endpoint, structured logs) can report
// which commit is actually deployed, alongside the migration-drift status
// from internal/platform/migrationguard.
package buildinfo

import "os"

// SHA is the git commit SHA this binary was built from. It defaults to
// "unknown" for `go run` or any build that does not pass the ldflag, and is
// stamped at build time via:
//
//	-ldflags "-X github.com/vgoats/goatos/backend/internal/platform/buildinfo.SHA=<sha>"
//
// See backend/Dockerfile's api build step for the wired example. CI/CD is
// not yet passing --build-arg GIT_SHA into that build (deliberately left as
// a follow-up; see docs/decisions/stale-binary-migration-drift-guard.md), so
// Current falls back to the GOATOS_BUILD_SHA environment variable, which an
// operator or deploy script can set without any image rebuild.
var SHA = "unknown"

// buildSHAEnvVar lets an operator or deploy script stamp the running build's
// identity (e.g. `docker run -e GOATOS_BUILD_SHA=$(git rev-parse HEAD) ...`)
// without requiring the ldflags-based build to have set SHA.
const buildSHAEnvVar = "GOATOS_BUILD_SHA"

// Current returns the best available build identity: the ldflags-stamped SHA
// if set, otherwise the GOATOS_BUILD_SHA environment variable, otherwise
// "unknown".
func Current() string {
	if SHA != "" && SHA != "unknown" {
		return SHA
	}
	if env := os.Getenv(buildSHAEnvVar); env != "" {
		return env
	}
	return "unknown"
}
