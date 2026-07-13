package pgtest

import "testing"

// AdminDSNForTemplate returns a DSN pointing at the (locked) template database, for the harness's
// own tests to prove that direct connections are rejected. Returns "" if the harness has not
// started. Test-only.
func AdminDSNForTemplate(t *testing.T) string {
	t.Helper()
	if !pkg.started.Load() || pkg.port == "" || pkg.template == "" {
		return ""
	}
	return dsn(pkg.port, pkg.template)
}
