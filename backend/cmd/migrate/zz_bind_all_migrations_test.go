package main

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// Every statement execMigrationSQL runs goes through sqlbind.Bind with no
// arguments; a migration with a stray $N outside quotes would now fail at
// deploy time, so prove the whole shipped set binds cleanly.
func TestEveryMigrationStatementBindsWithoutArguments(t *testing.T) {
	migrations, err := loadMigrations("../../migrations/postgres")
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) == 0 {
		t.Fatal("no migrations loaded")
	}
	for _, m := range migrations {
		statements, err := splitSQLStatements(m.SQL)
		if err != nil {
			t.Fatalf("%s: split: %v", m.Filename, err)
		}
		for _, statement := range statements {
			if _, err := sqlbind.Bind(statement); err != nil {
				t.Errorf("%s: %v", m.Filename, err)
			}
		}
	}
}
