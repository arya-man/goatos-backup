package app

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"
)

// WorkerSettings are the real backpressure knobs. Concurrency bounds how many
// goat transitions run at once; RowsPerSecond throttles the per-run dispatch
// rate so a 1M-row job cannot swamp vaccination recompute (each applied row
// emits an event that fans out). BatchSize bounds the claim size (index-tight),
// MaxRetries caps transient retries before a row is parked as 'error', and
// MaxIterations bounds the drain loop as a safety stop.
type WorkerSettings struct {
	Concurrency    int
	RowsPerSecond  float64
	Burst          int
	BatchSize      int
	MaxRetries     int
	MaxIterations  int
	ClaimLease     time.Duration
	IterationPause time.Duration
}

// DefaultWorkerSettings are conservative defaults suited to shared infrastructure.
func DefaultWorkerSettings() WorkerSettings {
	return WorkerSettings{
		Concurrency:    8,
		RowsPerSecond:  200,
		Burst:          50,
		BatchSize:      200,
		MaxRetries:     5,
		MaxIterations:  1_000_000,
		ClaimLease:     5 * time.Minute,
		IterationPause: 0,
	}
}

func (s WorkerSettings) normalized() WorkerSettings {
	def := DefaultWorkerSettings()
	if s.Concurrency <= 0 {
		s.Concurrency = def.Concurrency
	}
	if s.Burst <= 0 {
		s.Burst = def.Burst
	}
	if s.BatchSize <= 0 {
		s.BatchSize = def.BatchSize
	}
	if s.MaxRetries <= 0 {
		s.MaxRetries = def.MaxRetries
	}
	if s.MaxIterations <= 0 {
		s.MaxIterations = def.MaxIterations
	}
	if s.ClaimLease < 0 {
		s.ClaimLease = 0
	}
	return s
}

// PassStats is the tally for a single claim+apply pass.
type PassStats struct {
	Claimed int
	Applied int
	Skipped int
	Retried int
	Errored int
}

func (p *PassStats) add(other PassStats) {
	p.Claimed += other.Claimed
	p.Applied += other.Applied
	p.Skipped += other.Skipped
	p.Retried += other.Retried
	p.Errored += other.Errored
}

// DrainStats is the accumulated tally across a full drain plus the final job counts.
type DrainStats struct {
	Iterations int
	PassStats
	FinalCounts JobCounts
}

// WorkerService claims and applies bulk_status_job_row rows.
type WorkerService struct {
	repo     BulkStatusRepository
	applier  GoatTransitionApplier
	settings WorkerSettings
	log      *slog.Logger
}

func NewWorkerService(repo BulkStatusRepository, applier GoatTransitionApplier, settings WorkerSettings, log *slog.Logger) *WorkerService {
	if log == nil {
		log = slog.Default()
	}
	return &WorkerService{repo: repo, applier: applier, settings: settings.normalized(), log: log}
}

func (w *WorkerService) limiter() *rate.Limiter {
	if w.settings.RowsPerSecond <= 0 {
		return rate.NewLimiter(rate.Inf, 1)
	}
	return rate.NewLimiter(rate.Limit(w.settings.RowsPerSecond), w.settings.Burst)
}

// RunOnce claims one batch for the job and applies it, bounded by concurrency and
// the per-tenant dispatch rate. It returns the pass tally; Claimed == 0 means the
// job currently has no claimable rows.
func (w *WorkerService) RunOnce(ctx context.Context, tenantID, jobID string, limiter *rate.Limiter) (PassStats, error) {
	leaseSeconds := int(w.settings.ClaimLease / time.Second)
	rows, err := w.repo.ClaimRows(ctx, tenantID, jobID, w.settings.BatchSize, leaseSeconds)
	if err != nil {
		return PassStats{}, mapRepoErr(err)
	}
	stats := PassStats{Claimed: len(rows)}
	if len(rows) == 0 {
		return stats, nil
	}
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(w.settings.Concurrency)
	for i := range rows {
		row := rows[i]
		if err := limiter.Wait(gctx); err != nil {
			// Context canceled/deadline: stop dispatching more work this pass.
			break
		}
		g.Go(func() error {
			outcome := w.applyRow(gctx, tenantID, jobID, row)
			mu.Lock()
			switch outcome {
			case OutcomeApplied:
				stats.Applied++
			case OutcomeSkipped:
				stats.Skipped++
			case OutcomeRetry:
				stats.Retried++
			case OutcomeError:
				stats.Errored++
			}
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return stats, err
	}
	return stats, nil
}

// applyRow applies a single claimed row and persists its terminal (or retry)
// state. It never returns an error to the caller; failures are recorded on the
// row ledger so the drain loop can resume from it.
func (w *WorkerService) applyRow(ctx context.Context, tenantID, jobID string, row ClaimedRow) RowOutcome {
	// A row with no captured expected_row_version means the goat was absent at
	// enqueue; there is nothing to no-clobber against, so skip.
	if row.ExpectedRowVersion == nil {
		w.markSkipped(ctx, tenantID, row.RowID, "goat_not_found_at_enqueue")
		return OutcomeSkipped
	}
	result, err := w.applier.ApplyBulkStatusRow(ctx, ApplyRowRequest{
		TenantID:           tenantID,
		ActorID:            row.ActorID,
		JobID:              jobID,
		GoatID:             row.GoatID,
		Axis:               row.Axis,
		Target:             row.Target,
		Reason:             row.Reason,
		ExpectedRowVersion: *row.ExpectedRowVersion,
		TraceID:            "bulk-status-job:" + jobID,
	})
	if err != nil {
		// Unexpected/transport error: treat as transient and retry (capped).
		return w.markRetry(ctx, tenantID, row.RowID, truncateReason(err.Error()))
	}
	switch result.Outcome {
	case OutcomeApplied:
		if e := w.repo.MarkRowApplied(ctx, tenantID, row.RowID, result.EventID); e != nil {
			w.log.Error("bulk_status_mark_applied_failed", slog.String("row_id", row.RowID), slog.String("goat_id", row.GoatID), slog.String("error", e.Error()))
			return w.markRetry(ctx, tenantID, row.RowID, "mark_applied_failed")
		}
		return OutcomeApplied
	case OutcomeSkipped:
		w.markSkipped(ctx, tenantID, row.RowID, result.Reason)
		return OutcomeSkipped
	case OutcomeRetry:
		return w.markRetry(ctx, tenantID, row.RowID, result.Reason)
	default:
		if e := w.repo.MarkRowError(ctx, tenantID, row.RowID, truncateReason(result.Reason)); e != nil {
			w.log.Error("bulk_status_mark_error_failed", slog.String("row_id", row.RowID), slog.String("goat_id", row.GoatID), slog.String("error", e.Error()))
		}
		return OutcomeError
	}
}

func (w *WorkerService) markSkipped(ctx context.Context, tenantID, rowID, reason string) {
	if err := w.repo.MarkRowSkipped(ctx, tenantID, rowID, truncateReason(reason)); err != nil {
		w.log.Error("bulk_status_mark_skipped_failed", slog.String("row_id", rowID), slog.String("error", err.Error()))
	}
}

func (w *WorkerService) markRetry(ctx context.Context, tenantID, rowID, reason string) RowOutcome {
	outcome, err := w.repo.MarkRowRetry(ctx, tenantID, rowID, truncateReason(reason), w.settings.MaxRetries)
	if err != nil {
		w.log.Error("bulk_status_mark_retry_failed", slog.String("row_id", rowID), slog.String("error", err.Error()))
		return OutcomeRetry
	}
	return outcome
}

// RunUntilDrained repeatedly claims and applies batches for a job until no
// claimable rows remain (or the iteration/context bound is hit). Resume truth is
// the row_state ledger: each pass re-scans pending|retry (plus lease-expired
// claimed), never a positional cursor. It refreshes the job counts on exit.
func (w *WorkerService) RunUntilDrained(ctx context.Context, tenantID, jobID string) (DrainStats, error) {
	limiter := w.limiter()
	var drain DrainStats
	for iteration := 0; iteration < w.settings.MaxIterations; iteration++ {
		if err := ctx.Err(); err != nil {
			break
		}
		pass, err := w.RunOnce(ctx, tenantID, jobID, limiter)
		drain.Iterations++
		drain.PassStats.add(pass)
		if _, err2 := w.repo.RefreshJobCounts(ctx, tenantID, jobID); err2 != nil {
			w.log.Error("bulk_status_refresh_counts_failed", slog.String("job_id", jobID), slog.String("error", err2.Error()))
		}
		if err != nil {
			return drain, err
		}
		if pass.Claimed == 0 {
			break
		}
		// A pass that only produced retries would otherwise hot-spin; a small
		// pause (when configured) yields between retry-heavy passes.
		if pass.Applied == 0 && pass.Skipped == 0 && pass.Errored == 0 && w.settings.IterationPause > 0 {
			select {
			case <-ctx.Done():
				return drain, ctx.Err()
			case <-time.After(w.settings.IterationPause):
			}
		}
	}
	counts, err := w.repo.RefreshJobCounts(ctx, tenantID, jobID)
	if err != nil {
		return drain, mapRepoErr(err)
	}
	drain.FinalCounts = counts
	return drain, nil
}

// RunTenant drains every job in the tenant that currently has claimable rows.
// Useful as a standalone worker entrypoint that is not told a specific job id.
func (w *WorkerService) RunTenant(ctx context.Context, tenantID string, maxJobs int) (DrainStats, error) {
	if maxJobs <= 0 {
		maxJobs = 100
	}
	jobIDs, err := w.repo.ListJobIDsWithClaimableRows(ctx, tenantID, maxJobs)
	if err != nil {
		return DrainStats{}, mapRepoErr(err)
	}
	var total DrainStats
	for _, jobID := range jobIDs {
		if err := ctx.Err(); err != nil {
			return total, ctx.Err()
		}
		drain, err := w.RunUntilDrained(ctx, tenantID, jobID)
		total.Iterations += drain.Iterations
		total.PassStats.add(drain.PassStats)
		total.FinalCounts = drain.FinalCounts
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func truncateReason(reason string) string {
	reason = strings.TrimSpace(reason)
	const max = 480
	if len(reason) > max {
		return reason[:max]
	}
	return reason
}
