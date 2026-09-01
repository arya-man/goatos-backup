package main

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Required by pgtest's lifecycle guard for every package that calls pgtest.StartPostgres.
func TestMain(m *testing.M) { pgtest.RunMain(m) }
