package kernelstages

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestMain tears down the package-scoped pgtest container once after all tests run. Required for
// every package that calls pgtest.StartPostgres (enforced by TestLifecycleContractGuard).
func TestMain(m *testing.M) { pgtest.RunMain(m) }
