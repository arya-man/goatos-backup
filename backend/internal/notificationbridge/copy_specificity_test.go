package notificationbridge_test

// Regression guard for the confirmed maintainer defect: vaccination/obligation push copy was
// abstract ("Vaccination reminder test", "3 vaccinations due soon") -- no park, no shed, no vaccine,
// no date, nothing a farm operator can act on. These tests assert every rendered push in this
// package's scope carries the specifics it has available, and NEVER leaks an engineer-facing word
// or a raw identifier into user-facing Title/Body (AGENTS.md copy firewall).
//
// bannedCopyWords / assertCopySpecific are shared by every *_copy_test.go file in this package.

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// bannedCopyWords is the AGENTS.md user-facing copy firewall vocabulary: engineer words that must
// never appear in Title/Body shown to a farm worker.
var bannedCopyWords = []string{"obligation", "payload", "backend", "api", "route", "module", "null"}

var rawUUIDPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
var isoTimestampPattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)

// assertCopyClean fails the test if text contains a banned engineer word, a raw UUID, or a raw
// ISO-8601 timestamp -- the exact leaks the copy firewall exists to catch.
func assertCopyClean(t *testing.T, label, text string) {
	t.Helper()
	lower := strings.ToLower(text)
	for _, banned := range bannedCopyWords {
		if strings.Contains(lower, banned) {
			t.Errorf("%s contains banned engineer word %q: %q", label, banned, text)
		}
	}
	if rawUUIDPattern.MatchString(text) {
		t.Errorf("%s leaks a raw UUID: %q", label, text)
	}
	if isoTimestampPattern.MatchString(text) {
		t.Errorf("%s leaks a raw ISO-8601 timestamp: %q", label, text)
	}
}

// ---- fakes shared by this file's tests (self-contained: no dependency on other agents' in-flight
// test fixtures elsewhere in this package). ----

type fakeCompletionResolver struct {
	ctx calendarports.VaccinationCompletionContext
	err error
}

func (f fakeCompletionResolver) ResolveVaccinationCompletionContext(_ context.Context, _, _ string) (calendarports.VaccinationCompletionContext, error) {
	return f.ctx, f.err
}

type fakeMissedResolver struct {
	ctx calendarports.MissedObligationContext
	err error
}

func (f fakeMissedResolver) ResolveMissedObligationContext(_ context.Context, _, _ string) (calendarports.MissedObligationContext, error) {
	return f.ctx, f.err
}

type fakeCopyRecipients struct {
	member   []workforcedomain.NotificationRecipient
	position []workforcedomain.NotificationRecipient
}

func (f fakeCopyRecipients) ResolveModuleDutyRecipients(context.Context, string, string, string, string, string) ([]workforcedomain.NotificationRecipient, error) {
	return nil, nil
}
func (f fakeCopyRecipients) ResolveMemberRecipients(context.Context, string, string) ([]workforcedomain.NotificationRecipient, error) {
	return f.member, nil
}
func (f fakeCopyRecipients) ResolvePositionRecipients(context.Context, string, string, string, string) ([]workforcedomain.NotificationRecipient, error) {
	return f.position, nil
}

type capturedNotification struct {
	in calendarports.QueueRoleNotifications
}

type fakeCopyQueue struct {
	captured []capturedNotification
}

func (f *fakeCopyQueue) QueueRoleNotifications(_ context.Context, in calendarports.QueueRoleNotifications) (int, error) {
	f.captured = append(f.captured, capturedNotification{in: in})
	return len(in.Recipients), nil
}

var _ eventbus.Handler = (*notificationbridge.VerificationNotifier)(nil)

func discardLogger() *slog.Logger { return slog.Default() }
