package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

func TestRemovalDisablePreservesConcurrentPartialSubmit(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	// Schema migration time is setup, not part of the bounded submit/delete race.
	pool := pgtest.StartPostgres(t, context.Background())
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	task := seedFastingFixture(t, ctx, pool, "2026-09-18")
	submit, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer submit.Rollback(ctx)
	var operator, campaign string
	var complete bool
	if err := submit.QueryRow(ctx, fastingShedLockSQL, repoTenant, task).Scan(&operator, &campaign, &complete); err != nil {
		t.Fatal(err)
	}
	// A partial submit writes its pen but does not update the parent task.
	var evidence string
	var version int
	if err := submit.QueryRow(ctx, fastingShedUpsertSQL, repoTenant, task, repoAnimalScope, "Test pen", fastingFeedProof, fastingWaterProof, []byte(`{}`), []byte(`{}`)).Scan(&evidence, &version); err != nil {
		t.Fatal(err)
	}
	disable, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer disable.Rollback(ctx)
	var pid int
	if err := disable.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- NewRepository(pool, 10*time.Second).deleteUnsubmittedFastingTaskTx(ctx, disable, repoTenant, repoCampaign)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, "SELECT COALESCE(wait_event_type = 'Lock', false) FROM pg_stat_activity WHERE pid=$1", pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("disable never waited on submit lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := submit.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ports.ErrRemovalChangeLocked) {
		t.Fatalf("disable after concurrent partial submit: got %v; want ErrRemovalChangeLocked (evidence must survive)", err)
	}
	var survives bool
	if err := disable.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM weighing_fasting_shed_proofs WHERE fasting_shed_id=$1::uuid
)`, evidence).Scan(&survives); err != nil {
		t.Fatal(err)
	}
	if !survives {
		t.Fatal("concurrent partial submission evidence was deleted")
	}
}

func TestRemovalDisableDeletesEmptyRound(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	task := seedFastingFixture(t, ctx, pool, "2026-09-18")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	repo := NewRepository(pool, 10*time.Second)
	if err := repo.deleteUnsubmittedFastingTaskTx(ctx, tx, repoTenant, repoCampaign); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM weighing_fasting_tasks WHERE fasting_task_id=$1::uuid)", task).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("empty round was not removed")
	}
	if err := repo.deleteUnsubmittedFastingTaskTx(ctx, tx, repoTenant, repoCampaign); err != nil {
		t.Fatalf("absent round must be a no-op: %v", err)
	}
}
