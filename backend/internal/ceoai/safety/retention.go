package safety

import (
	"context"
	"log/slog"
	"time"
)

// ConversationPurger hard-deletes stored assistant conversations (and their
// messages/traces) whose retention_expires_at has passed. It is a port: the
// Postgres adapter implements it against ceo_ai_conversations; tests use a fake.
//
// Implementations MUST scope deletes to expired rows only and return the number
// of conversations purged. They MUST be idempotent (safe to run repeatedly).
type ConversationPurger interface {
	// PurgeExpired hard-deletes conversations whose retention_expires_at <=
	// asOf, bounded by limit rows per call to keep transactions small.
	PurgeExpired(ctx context.Context, asOf time.Time, limit int) (purged int, err error)
}

// RetentionConfig tunes the cleanup job.
type RetentionConfig struct {
	// Interval is how often the cleanup runs.
	Interval time.Duration
	// BatchLimit bounds rows deleted per PurgeExpired call (keeps locks/txn
	// small; the job loops until a batch comes back short).
	BatchLimit int
	// MaxBatchesPerTick caps total batches per tick so one run can't monopolize
	// the DB.
	MaxBatchesPerTick int
}

// DefaultRetentionConfig returns hourly cleanup in bounded batches.
func DefaultRetentionConfig() RetentionConfig {
	return RetentionConfig{
		Interval:          time.Hour,
		BatchLimit:        500,
		MaxBatchesPerTick: 20,
	}
}

// RetentionCleaner runs the periodic purge loop.
type RetentionCleaner struct {
	purger ConversationPurger
	cfg    RetentionConfig
	clock  Clock
	log    *slog.Logger
}

// NewRetentionCleaner builds the cleaner.
func NewRetentionCleaner(purger ConversationPurger, cfg RetentionConfig, clock Clock, log *slog.Logger) *RetentionCleaner {
	if cfg.Interval <= 0 {
		cfg.Interval = time.Hour
	}
	if cfg.BatchLimit <= 0 {
		cfg.BatchLimit = 500
	}
	if cfg.MaxBatchesPerTick <= 0 {
		cfg.MaxBatchesPerTick = 20
	}
	return &RetentionCleaner{purger: purger, cfg: cfg, clock: clock, log: log}
}

// RunOnce performs a single cleanup pass: it purges expired conversations in
// bounded batches until a batch returns fewer than BatchLimit rows or the
// per-tick batch cap is hit. Returns the total purged.
func (c *RetentionCleaner) RunOnce(ctx context.Context) (int, error) {
	if c == nil || c.purger == nil {
		return 0, nil
	}
	total := 0
	asOf := c.clock.now()
	for i := 0; i < c.cfg.MaxBatchesPerTick; i++ {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := c.purger.PurgeExpired(ctx, asOf, c.cfg.BatchLimit)
		if err != nil {
			return total, err
		}
		total += n
		if n < c.cfg.BatchLimit {
			break
		}
	}
	if total > 0 && c.log != nil {
		// Non-sensitive: a count only. No conversation content, no actor id.
		c.log.Info("ceoai.retention.purged", slog.Int("conversations", total))
	}
	return total, nil
}

// Run starts the periodic loop and blocks until ctx is cancelled. It is designed
// to be launched in a single dedicated goroutine (bounded — one loop, not
// per-request).
func (c *RetentionCleaner) Run(ctx context.Context) {
	if c == nil || c.purger == nil {
		return
	}
	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()
	// Run once promptly on startup.
	if _, err := c.RunOnce(ctx); err != nil && c.log != nil && ctx.Err() == nil {
		c.log.Warn("ceoai.retention.error", slog.String("error", err.Error()))
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := c.RunOnce(ctx); err != nil && c.log != nil && ctx.Err() == nil {
				c.log.Warn("ceoai.retention.error", slog.String("error", err.Error()))
			}
		}
	}
}

// RetentionExpiry computes a retention_expires_at from a creation time and a
// retention window (days). It is the single helper both the write path and the
// docs reference so the policy is defined once. window <= 0 returns zero time
// (no expiry — caller should treat as "keep", but production config must set a
// window).
func RetentionExpiry(createdAt time.Time, windowDays int) time.Time {
	if windowDays <= 0 {
		return time.Time{}
	}
	return createdAt.AddDate(0, 0, windowDays)
}
