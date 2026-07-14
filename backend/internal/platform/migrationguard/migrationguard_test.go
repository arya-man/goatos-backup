package migrationguard

import (
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	tests := []struct {
		name              string
		dbVersion         string
		binaryVersion     string
		wantErr           bool
		wantDBAhead       bool
		wantBinaryAhead   bool
		wantErrorContains string
	}{
		{
			name:          "equal versions proceed",
			dbVersion:     "000188",
			binaryVersion: "000188",
			wantErr:       false,
		},
		{
			name:              "db ahead of binary fails - the incident direction",
			dbVersion:         "000188",
			binaryVersion:     "000186",
			wantErr:           true,
			wantDBAhead:       true,
			wantErrorContains: "database migrated to 000188 but this binary only knows 000186",
		},
		{
			name:              "binary ahead of db fails - AGENTS.md discipline now enforced",
			dbVersion:         "000186",
			binaryVersion:     "000188",
			wantErr:           true,
			wantBinaryAhead:   true,
			wantErrorContains: "database is at migration 000186 but this binary requires 000188",
		},
		{
			name:              "never-migrated database (empty db version) is binary-ahead",
			dbVersion:         "",
			binaryVersion:     "000188",
			wantErr:           true,
			wantBinaryAhead:   true,
			wantErrorContains: "database has no migrations applied but this binary requires 000188",
		},
		{
			name:          "whitespace-only versions are trimmed before comparing",
			dbVersion:     "  000188 ",
			binaryVersion: " 000188",
			wantErr:       false,
		},
		{
			name:          "missing binary version is a hard error",
			dbVersion:     "000188",
			binaryVersion: "",
			wantErr:       true,
		},
		{
			name:          "malformed db version is a hard error",
			dbVersion:     "not-a-version",
			binaryVersion: "000188",
			wantErr:       true,
		},
		{
			name:          "malformed binary version is a hard error",
			dbVersion:     "000188",
			binaryVersion: "not-a-version",
			wantErr:       true,
		},
		{
			name:            "leading-zero widths compare numerically, not lexicographically",
			dbVersion:       "000099",
			binaryVersion:   "000100",
			wantErr:         true,
			wantBinaryAhead: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, err := Check(tt.dbVersion, tt.binaryVersion)
			if tt.wantErr && err == nil {
				t.Fatalf("Check(%q, %q) = nil error, want error", tt.dbVersion, tt.binaryVersion)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Check(%q, %q) = %v, want nil error", tt.dbVersion, tt.binaryVersion, err)
			}
			if tt.wantErrorContains != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErrorContains)) {
				t.Fatalf("Check(%q, %q) error = %v, want to contain %q", tt.dbVersion, tt.binaryVersion, err, tt.wantErrorContains)
			}
			if status.DBAhead != tt.wantDBAhead {
				t.Fatalf("Check(%q, %q) Status.DBAhead = %v, want %v", tt.dbVersion, tt.binaryVersion, status.DBAhead, tt.wantDBAhead)
			}
			if status.BinaryAhead != tt.wantBinaryAhead {
				t.Fatalf("Check(%q, %q) Status.BinaryAhead = %v, want %v", tt.dbVersion, tt.binaryVersion, status.BinaryAhead, tt.wantBinaryAhead)
			}
		})
	}
}
