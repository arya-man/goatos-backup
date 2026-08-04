package postgres

import (
	"os"
	"strings"
	"testing"
)

// FINDING 7 (runtime-path guard): PublishCampaign MUST flip the campaign to
// 'published' BEFORE it materializes kernel work items via
// createWorkItemsForPublishTx. If that ordering is ever reversed, a crash or
// error between the two statements could leave the transaction rolled back
// on the campaign side (fine) - but worse, if a FUTURE refactor moves the
// work-item creation ahead of the status flip and something reads outside
// this transaction, or a later backfill migration ever runs against rows
// this order produced, draft-time work items become indistinguishable from
// published-time ones. See migration 000069's bug (Finding 7) for exactly
// what happens when a caller trusts weighing_campaign_sheds existing without
// checking the PARENT campaign's status.
//
// This is a structural regression test, not a DB integration test: it reads
// the actual PublishCampaign source and asserts the status='published'
// UPDATE textually precedes the createWorkItemsForPublishTx call inside the
// function body. It intentionally does NOT touch Postgres, so it runs in
// every environment (including where GOATOS_RUN_POSTGRES_TESTS is unset) and
// fails LOUDLY the moment someone reorders the two statements, rather than
// relying on a timing-sensitive integration scenario to catch it.
func TestPublishCampaignFlipsStatusBeforeCreatingWorkItems(t *testing.T) {
	src, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatalf("read repository.go: %v", err)
	}
	body := string(src)

	fnStart := strings.Index(body, "func (r *Repository) PublishCampaign(")
	if fnStart < 0 {
		t.Fatalf("could not locate PublishCampaign in repository.go")
	}
	// Bound the search to this one function: stop at the next top-level
	// `func (r *Repository)` declaration after PublishCampaign's own.
	rest := body[fnStart+len("func (r *Repository) PublishCampaign("):]
	nextFn := strings.Index(rest, "\nfunc (r *Repository) ")
	if nextFn < 0 {
		t.Fatalf("could not bound PublishCampaign function body")
	}
	fnBody := rest[:nextFn]

	statusIdx := strings.Index(fnBody, "SET status='published'")
	if statusIdx < 0 {
		t.Fatalf("PublishCampaign no longer contains the status='published' UPDATE - update this guard if the statement changed shape")
	}
	workItemsIdx := strings.Index(fnBody, "r.createWorkItemsForPublishTx(ctx, tx, tenantID, campaignID)")
	if workItemsIdx < 0 {
		t.Fatalf("PublishCampaign no longer calls createWorkItemsForPublishTx - update this guard if the call changed shape")
	}
	if statusIdx > workItemsIdx {
		t.Fatalf("REGRESSION: PublishCampaign now creates kernel work items BEFORE flipping the campaign to 'published' (status update at byte %d, work-item creation at byte %d). This resurrects the Finding 7 class of bug: a still-draft campaign could get published-looking work items. Restore the ordering: status flip first, work items second, in the same transaction.", statusIdx, workItemsIdx)
	}
}
