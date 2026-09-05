package notificationbridge

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// notification_requests.notification_type IS A CLOSED ENUM, and a notifier that writes a value
// outside it fails 23514 on every tick -- QUIETLY, in exactly the way that matters: the write errors
// into a cadence log, nothing is queued, and the alert never arrives, which is indistinguishable
// from "there was nothing to alert about". A daily alert that has never once fired looks identical
// to a farm with no low stock, no overdue load and no missing feed captures.
//
// That is not hypothetical. feed_low_stock (2026-08-24) and procurement_load_overdue (2026-09-01)
// both shipped outside the enum and could only ever have been failing; migration 000248 widened it
// for them and for feed_proof_times_daily together.
//
// So this test reads the two sides and compares them: every NotificationType* constant this package
// declares must appear in the LAST migration that writes the CHECK. It is a cheap structural guard
// for a defect whose real proof needs a database, and it is the one that would have caught both
// earlier misses at the moment they were written.
func TestEveryNotificationTypeIsAllowedByTheCheckConstraint(t *testing.T) {
	declared := declaredNotificationTypes(t)
	if len(declared) == 0 {
		t.Fatal("no NotificationType constants found -- the scan is broken, not the code")
	}
	allowed := allowedNotificationTypes(t)

	for _, notificationType := range declared {
		if !allowed[notificationType] {
			t.Errorf("notification type %q is written by this package but is NOT in "+
				"notification_requests_type_check; every insert of it fails 23514 and the alert "+
				"silently never arrives. Widen the enum in a forward migration.", notificationType)
		}
	}
}

var (
	notificationTypeConstRe = regexp.MustCompile(`(?m)^const\s+NotificationType\w*\s*=\s*"([^"]+)"`)
	checkValueRe            = regexp.MustCompile(`'([a-z_]+)'::text`)
)

// declaredNotificationTypes collects the notification_type values this package writes.
func declaredNotificationTypes(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, match := range notificationTypeConstRe.FindAllStringSubmatch(string(source), -1) {
			seen[match[1]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// allowedNotificationTypes reads the enum out of the LAST migration that writes the constraint --
// migrations are applied in order, so the highest-numbered writer is the live definition.
func allowedNotificationTypes(t *testing.T) map[string]bool {
	t.Helper()
	migrations, err := filepath.Glob(filepath.Join("..", "..", "migrations", "postgres", "*.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(migrations)

	var upSection string
	var source string
	for _, path := range migrations {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(raw)
		if !strings.Contains(text, "notification_requests_type_check") {
			continue
		}
		// Only the Up section defines what the deployed database allows; a Down section's narrower
		// list would otherwise read as the live enum.
		up := text
		if idx := strings.Index(text, "-- +goose Down"); idx >= 0 {
			up = text[:idx]
		}
		if strings.Contains(up, "ADD CONSTRAINT\n  notification_requests_type_check") ||
			strings.Contains(up, "ADD CONSTRAINT notification_requests_type_check") ||
			strings.Contains(up, "CONSTRAINT notification_requests_type_check CHECK") {
			upSection, source = up, path
		}
	}
	if upSection == "" {
		t.Fatal("no migration defines notification_requests_type_check -- the scan is broken")
	}

	// The last ADD in that file is the definition that survives it. Anchoring on the constraint NAME
	// instead would land on the trailing VALIDATE, which carries no values at all.
	idx := strings.LastIndex(upSection, "ADD CONSTRAINT")
	if idx < 0 {
		idx = strings.LastIndex(upSection, "CONSTRAINT notification_requests_type_check CHECK")
	}
	allowed := map[string]bool{}
	for _, match := range checkValueRe.FindAllStringSubmatch(upSection[idx:], -1) {
		allowed[match[1]] = true
	}
	if len(allowed) == 0 {
		t.Fatalf("parsed no allowed values out of %s -- the scan is broken", source)
	}
	return allowed
}
