package app

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

type fakeDuties struct {
	keys  []string
	err   error
	calls int
}

func (f *fakeDuties) ListGrantedModuleKeys(_ context.Context, _, _ string) ([]string, error) {
	f.calls++
	return f.keys, f.err
}

// dutyTestService registers the four categories the live composition root registers, so
// the category -> module resolution under test is the real registry mapping and not a
// test-local restatement of it.
func dutyTestService() *Service {
	svc := NewService(newFakeRepo(), fakeMedia{})
	_ = svc.RegisterCategory(domain.CategoryDefinition{Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof"})
	_ = svc.RegisterCategory(domain.CategoryDefinition{Vertical: "preventive_care", Module: "weighing", Category: "weighing_proof"})
	_ = svc.RegisterCategory(domain.CategoryDefinition{Vertical: "counts", Module: "counts", Category: "shifting_move"})
	_ = svc.RegisterCategory(domain.CategoryDefinition{Vertical: "feed", Module: "feed", Category: "feed_distribution"})
	return svc
}

func forbiddenCode(t *testing.T, err error) string {
	t.Helper()
	var appErr *Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *app.Error, got %T (%v)", err, err)
	}
	if appErr.HTTPStatus != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", appErr.HTTPStatus)
	}
	return appErr.Code
}

func TestAuthorizeQueueModuleAllowsOwnModule(t *testing.T) {
	cases := []struct {
		name     string
		duties   []string
		category string
	}{
		// The namespaced duty spellings are the ones seed-position-duties actually writes.
		{"vaccination verifier", []string{"pc.vaccination"}, "vaccination_proof"},
		{"weighing verifier", []string{"weighing"}, "weighing_proof"},
		{"counts verifier", []string{"counts"}, "shifting_move"},
		{"feed verifier", []string{"feed.direction"}, "feed_distribution"},
		// The seeded reality today: one video_verifier seat holding every notified module.
		{"all-module verifier", []string{"pc.vaccination", "weighing", "counts", "feed.direction"}, "vaccination_proof"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := dutyTestService().WithModuleDutyReader(&fakeDuties{keys: tc.duties})
			if err := svc.AuthorizeQueueModule(context.Background(), "t", "a", tc.category, ""); err != nil {
				t.Fatalf("a verifier holding duty for this module must be unaffected, got %v", err)
			}
		})
	}
}

func TestAuthorizeQueueModuleRefusesOtherModule(t *testing.T) {
	svc := dutyTestService().WithModuleDutyReader(&fakeDuties{keys: []string{"counts"}})
	err := svc.AuthorizeQueueModule(context.Background(), "t", "a", "vaccination_proof", "")
	if err == nil {
		t.Fatal("a counts verifier must not be able to read vaccination proofs")
	}
	if code := forbiddenCode(t, err); code != "module_not_verified" {
		t.Fatalf("unexpected code %q", code)
	}
}

// The preventive_care department is granted vaccination + counts + feed_direction
// (cmd/seed-roster-real defaultDepartmentModules), so its members keep all three queues.
// Weighing is granted by no department at all, which makes it the one module a
// department-scoped verifier can now be refused. Pinned here so the day someone decides a
// PC manager should review weighing proofs, this test is what tells them where to grant it
// rather than a field report of a 403.
func TestAuthorizeQueueModulePreventiveCareDepartmentKeepsItsThreeQueues(t *testing.T) {
	pc := []string{"vaccination", "counts", "feed_direction"}
	for _, category := range []string{"vaccination_proof", "shifting_move", "feed_distribution"} {
		svc := dutyTestService().WithModuleDutyReader(&fakeDuties{keys: pc})
		if err := svc.AuthorizeQueueModule(context.Background(), "t", "a", category, ""); err != nil {
			t.Fatalf("%s must stay open for a preventive_care member, got %v", category, err)
		}
	}
	svc := dutyTestService().WithModuleDutyReader(&fakeDuties{keys: pc})
	if err := svc.AuthorizeQueueModule(context.Background(), "t", "a", "weighing_proof", ""); err == nil {
		t.Fatal("weighing is outside the preventive_care grant and must be refused")
	}
}

// The regression that matters most: every path where duty data cannot positively exclude
// the request must behave exactly as it did before this gate existed. A too-narrow rule
// here 403s working verifiers out of their own park.
func TestAuthorizeQueueModuleFailsOpen(t *testing.T) {
	cases := []struct {
		name   string
		duties *fakeDuties
		wire   bool
		// category the caller asks for
		category string
		module   string
	}{
		{name: "reader not wired", wire: false, category: "vaccination_proof"},
		{name: "no duty rows at all", wire: true, duties: &fakeDuties{keys: nil}, category: "vaccination_proof"},
		{name: "department-level verification grant only", wire: true, duties: &fakeDuties{keys: []string{"verification"}}, category: "vaccination_proof"},
		{name: "duty lookup errors", wire: true, duties: &fakeDuties{err: errors.New("boom")}, category: "vaccination_proof"},
		{name: "unregistered category selects no module", wire: true, duties: &fakeDuties{keys: []string{"counts"}}, category: "not_a_category"},
		{name: "no category and no module named", wire: true, duties: &fakeDuties{keys: []string{"counts"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := dutyTestService()
			if tc.wire {
				svc = svc.WithModuleDutyReader(tc.duties)
			}
			if err := svc.AuthorizeQueueModule(context.Background(), "t", "a", tc.category, tc.module); err != nil {
				t.Fatalf("must fail open, got %v", err)
			}
		})
	}
}

// module= is a caller-supplied filter too, so it cannot be a way around the category gate.
func TestAuthorizeQueueModuleGatesRawModuleFilter(t *testing.T) {
	svc := dutyTestService().WithModuleDutyReader(&fakeDuties{keys: []string{"counts"}})
	if err := svc.AuthorizeQueueModule(context.Background(), "t", "a", "", "vaccination"); err == nil {
		t.Fatal("module filter must be gated on duty as well")
	}
	if err := svc.AuthorizeQueueModule(context.Background(), "t", "a", "", "counts"); err != nil {
		t.Fatalf("own module via module filter must pass, got %v", err)
	}
}

// The duty catalog spells feed "feed.direction" while the feed module registers its
// categories under module "feed". They are one module; treating them as two would 403 the
// feed verifier out of the only queue they have.
func TestModuleKeysMatchToleratesNamespaceDrift(t *testing.T) {
	if !moduleKeysMatch("feed_direction", "feed") {
		t.Fatal("feed.direction duty must cover the feed module")
	}
	if moduleKeysMatch("counts", "vaccination") {
		t.Fatal("unrelated modules must not match")
	}
	if moduleKeysMatch("feed_direction", "feed_packing") {
		t.Fatal("sibling keys that share no prefix boundary must not match")
	}
}
