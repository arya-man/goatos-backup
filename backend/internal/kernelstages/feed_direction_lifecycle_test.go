package kernelstages

import (
	"testing"

	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

// The kernel-worker's scheduled lifecycle (issue at the dispatch clock, the 14:00 correction, the
// lock) pins the published feed SOP cards, and -- exactly as on origin/main -- is composed WITHOUT
// the packing store, so the scheduled amend reopens no packed bag. Wiring it would change daily
// operations on deploy without any SOP edit; that is a pending maintainer decision (AGENTS.md
// "AFTERNOON FEED CORRECTION"; 2026-09-17 parity revert).
func TestFeedDirectionLifecycleServicePinsTheCardAndKeepsMainsCorrection(t *testing.T) {
	svc := NewFeedDirectionLifecycleService(Deps{PgCfg: platformpg.Config{}}, "test")
	if svc.ReopensPackingOnCorrection() {
		t.Fatal("scheduled lifecycle service has the packing store: the 14:00 correction would reopen packed bags, which origin/main does not do")
	}
	if !svc.PinsPublishedFeedSOP() {
		t.Fatal("scheduled lifecycle service has no SOP rules source: every sheet would pin the seed")
	}
}
