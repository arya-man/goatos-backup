package oploc_test

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// This package gained a real-Postgres test (TestShedScopedLocationSQLActuallyExecutes), so it
// must own the shared container lifecycle or leak it. Enforced by TestLifecycleContractGuard.
func TestMain(m *testing.M) { pgtest.RunMain(m) }
