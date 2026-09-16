package kernelstages

import (
	"testing"

	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

// The kernel-worker's scheduled lifecycle (issue at the dispatch clock, the 14:00 correction, the
// lock) must be composed with BOTH the published feed SOP cards (the pin) and the packing store
// (the correction's reopen of every bag packed against the old head count). Without the store the
// amend silently reopened nothing (2026-09-17 E2E on the QA clone: Gandhi 1 gained an animal after
// both bags were packed, the sheet amended to 43, both bags stayed done).
func TestFeedDirectionLifecycleServiceReopensPackingAndPinsTheCard(t *testing.T) {
	svc := NewFeedDirectionLifecycleService(Deps{PgCfg: platformpg.Config{}}, "test")
	if !svc.ReopensPackingOnCorrection() {
		t.Fatal("scheduled lifecycle service has no packing store: the 14:00 correction would never reopen a packed bag")
	}
	if !svc.PinsPublishedFeedSOP() {
		t.Fatal("scheduled lifecycle service has no SOP rules source: every sheet would pin the seed")
	}
}
