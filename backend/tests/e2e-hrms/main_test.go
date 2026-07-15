package e2ehrms

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// reportRelPath is relative to this package's directory (go test's working directory is always
// the package under test), so it always lands at backend/tests/e2e-hrms/report/index.html.
const reportRelPath = "report/index.html"

// TestMain runs the HRMS kernel-story tests, then always renders whatever was recorded into the HTML
// report -- even a partial report from a setup failure is more useful than none -- and prints its
// path before propagating the real test exit code.
func TestMain(m *testing.M) {
	if !pgtest.Enabled() {
		// Keep default no-Postgres runs read-only; report generation belongs to the explicit DB run.
		os.Exit(m.Run())
	}
	code := m.Run()
	// Tear down the shared pgtest container before report writing / os.Exit (os.Exit skips defers).
	pgtest.Shutdown()

	if err := globalReport.WriteHTML(reportRelPath); err != nil {
		fmt.Fprintf(os.Stderr, "e2e-hrms: failed to write kernel story report: %v\n", err)
		if code == 0 {
			code = 1
		}
		os.Exit(code)
	}
	if abs, err := filepath.Abs(reportRelPath); err == nil {
		fmt.Printf("\nHRMS Kernel story report: %s\n", abs)
	} else {
		fmt.Printf("\nHRMS Kernel story report: %s\n", reportRelPath)
	}
	os.Exit(code)
}
