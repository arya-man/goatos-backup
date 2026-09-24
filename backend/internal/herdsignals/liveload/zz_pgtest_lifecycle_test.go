package liveload

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestMain tears down the package-scoped pgtest container once after all tests run.
func TestMain(m *testing.M) { pgtest.RunMain(m) }
