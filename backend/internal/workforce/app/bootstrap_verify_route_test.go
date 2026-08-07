package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// Regression test for the verifier navigation defect: the per-feature verifier bar shipped two
// links neither the app nor the verifier could use.
//
//  1. Verify carried only module=<feature>. The client scopes a queue by CATEGORY and its private
//     module->category table knows only weighing, so counts (and every feature added later) fell
//     through to VACCINATION -- a Counts verifier opened another module's proofs.
//  2. Alerts carried no category for anything the client could not guess, which is the same
//     scoping failure one tab over. (The route itself is "/verify/alerts", now a real client
//     destination; what is asserted here is that it stays feature-scoped.)
//
// Both are asserted per feature, because a per-feature bar that is right for one feature and wrong
// for the next is exactly the bug.
func TestVerifierBarLinksAreFeatureScopedAndClientResolvable(t *testing.T) {
	cases := []struct {
		feature      string
		wantCategory string
	}{
		{feature: "vaccination", wantCategory: "vaccination_proof"},
		{feature: "weighing", wantCategory: "weighing_proof"},
		// counts verification is shifting execution, never a "counts_proof" category.
		{feature: "counts", wantCategory: "shifting_move"},
		{feature: "feed_direction", wantCategory: "feed_distribution"},
	}

	for _, tc := range cases {
		t.Run(tc.feature, func(t *testing.T) {
			svc := NewService(&fakeRepo{
				profile:        profile("active"),
				grants:         []domain.GrantSummary{grantWithRole(permissions.RoleVerifier)},
				grantedModules: []string{tc.feature},
			})
			got, err := svc.Bootstrap(context.Background(), testTenant, testActor, "", "", "trace-1")
			if err != nil {
				t.Fatalf("Bootstrap() error=%v", err)
			}
			if len(got.Modules) != 1 {
				t.Fatalf("want exactly one verifier module for a single duty; got %#v", got.Modules)
			}
			module := got.Modules[0]

			wantVerify := "/verify?module=" + tc.feature + "&category=" + tc.wantCategory
			if module.Href != wantVerify {
				t.Fatalf("module href = %q, want %q (the drawer entry and the Verify tab must open the same queue)", module.Href, wantVerify)
			}
			byKey := map[string]string{}
			for _, item := range module.NavItems {
				byKey[item.Key] = item.Href
			}
			if byKey["verify"] != wantVerify {
				t.Fatalf("verify href = %q, want %q -- without the category the client scopes this queue by guessing the module", byKey["verify"], wantVerify)
			}
			if alerts := byKey["alerts"]; alerts != wantVerifierAlertsHref(tc.feature, tc.wantCategory) {
				t.Fatalf("alerts href = %q, want %s -- an alerts feed with no category is not feature-scoped", alerts, tc.wantCategory)
			}
		})
	}
}
