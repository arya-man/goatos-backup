package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// reportRelPath is relative to this package's directory (go test's working directory is always
// the package under test), so it always lands at backend/tests/e2e/report/index.html.
const reportRelPath = "report/index.html"

// TestMain runs the kernel-story tests, then always renders whatever was recorded into the HTML
// report -- even a partial report from a setup failure is more useful than none -- and prints its
// path before propagating the real test exit code.
func TestMain(m *testing.M) {
	code := m.Run()

	if err := globalReport.WriteHTML(reportRelPath); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: failed to write kernel story report: %v\n", err)
		if code == 0 {
			code = 1
		}
		os.Exit(code)
	}
	if abs, err := filepath.Abs(reportRelPath); err == nil {
		fmt.Printf("\nKernel story report: %s\n", abs)
	} else {
		fmt.Printf("\nKernel story report: %s\n", reportRelPath)
	}
	os.Exit(code)
}
