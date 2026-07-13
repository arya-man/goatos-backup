package pgtest_test

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }
