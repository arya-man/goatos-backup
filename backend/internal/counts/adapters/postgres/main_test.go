package postgres

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestMain tears down the package-scoped pgtest container once after all tests run. Every package
// that calls pgtest.StartPostgres needs this (or a custom TestMain that calls pgtest.Shutdown).
func TestMain(m *testing.M) { pgtest.RunMain(m) }
