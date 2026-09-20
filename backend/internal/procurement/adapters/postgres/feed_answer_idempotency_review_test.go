package postgres

import (
	"context"
	"errors"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
	"testing"
	"time"
)

func TestFeedAuthoredAnswerReplayConflict(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFeedPurchaseFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	w := feedWrite()
	w.SOPAnswers = map[string]string{"storage": "covered"}
	w.QuestionnaireVersion = 1
	if _, err := repo.CreateFeedPurchase(ctx, testTenant, w, "", "authored-replay"); err != nil {
		t.Fatal(err)
	}
	w.SOPAnswers = map[string]string{"storage": "outside"}
	p, err := repo.CreateFeedPurchase(ctx, testTenant, w, "", "authored-replay")
	if !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("changed authored answer accepted as replay: got %v err=%v", p.SOPAnswers, err)
	}
	w.SOPAnswers = map[string]string{"storage": "covered"}
	w.QuestionnaireVersion = 2
	if _, err := repo.CreateFeedPurchase(ctx, testTenant, w, "", "authored-replay"); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("changed form version: %v", err)
	}
	w.QuestionnaireVersion = 1
	if _, err := repo.CreateFeedPurchase(ctx, testTenant, w, "", "authored-replay"); err != nil {
		t.Fatalf("exact replay refused: %v", err)
	}

}
