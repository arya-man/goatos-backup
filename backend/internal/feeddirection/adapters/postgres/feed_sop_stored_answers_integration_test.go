package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

func answer(v string) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// SOP VERIFIER PARITY: the store hands back the ROW's answers on a fresh write, an already-pending
// natural-key retry and an idempotent replay, so the service can queue the verifier item from what
// the crew stored rather than from a retry's request.
func TestDistributionAndTransportReturnTheStoredSOPAnswers(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)

	first := distributionParams()
	first.SOPAnswers = authored.Answers{"clean": answer("yes")}
	res, err := repo.CompleteDistribution(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if string(res.SOPAnswers["clean"]) != `"yes"` {
		t.Fatalf("fresh write answers = %v", res.SOPAnswers)
	}
	retry := distributionParams()
	retry.IdempotencyKey = "feed-distribution-key-answers-retry"
	retry.SOPAnswers = authored.Answers{"clean": answer("no")}
	again, err := repo.CompleteDistribution(ctx, retry)
	if err != nil {
		t.Fatal(err)
	}
	if again.NewlyPending || string(again.SOPAnswers["clean"]) != `"yes"` {
		t.Fatalf("pending retry answers = %v (newly=%v), want the stored yes", again.SOPAnswers, again.NewlyPending)
	}
	replay, err := repo.CompleteDistribution(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if string(replay.SOPAnswers["clean"]) != `"yes"` {
		t.Fatalf("idempotent replay answers = %v", replay.SOPAnswers)
	}

	day := time.Date(2026, 7, 29, 0, 0, 0, 0, biztime.DefaultLocation())
	if _, err := repo.MaterializeTransportTasks(ctx, ports.MaterializeTransportParams{TenantID: fdTenant, AsOf: day.Add(16 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListTransportTasks(ctx, ports.ListTransportTasksParams{TenantID: fdTenant, Day: day, ActorID: transportOperator, Limit: 20})
	if err != nil || len(page.Items) == 0 {
		t.Fatalf("transport tasks: %v (%d)", err, len(page.Items))
	}
	submit := ports.SubmitTransportParams{
		TenantID: fdTenant, TaskID: page.Items[0].TaskID, ProofRef: "proof-live-answers", OperatorID: transportOperator,
		IdempotencyKey: "transport-submit-answers", ActorID: transportOperator, ActorType: "operator",
		SOPProofs:  authored.ProofRefs{domain.SlotTransportVideo: "proof-live-answers"},
		SOPAnswers: authored.Answers{"clean": answer("yes")},
	}
	got, err := repo.SubmitTransportAttempt(ctx, submit)
	if err != nil {
		t.Fatal(err)
	}
	if got.SOPProofs[domain.SlotTransportVideo] != "proof-live-answers" || string(got.SOPAnswers["clean"]) != `"yes"` {
		t.Fatalf("transport submit row = %+v", got)
	}
	submit.SOPAnswers = authored.Answers{"clean": answer("no")}
	replayed, err := repo.SubmitTransportAttempt(ctx, submit)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.AttemptID != got.AttemptID || string(replayed.SOPAnswers["clean"]) != `"yes"` {
		t.Fatalf("transport replay = %+v, want the stored attempt's yes", replayed)
	}
}
