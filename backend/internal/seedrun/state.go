// Package seedrun defines the persisted seed-run state machine (VACC-REV-02) shared by the
// source-backed seed command and the closeout/promotion gate. A source-backed seed records its
// lifecycle so a half-seeded or never-verified database can never appear healthy or promotable.
package seedrun

import (
	"errors"
	"fmt"
)

// Seed-run lifecycle states. Verified is the READY/promotable state; Failed is the RESET_REQUIRED
// state that seed-closeout, CI, and deployment promotion must reject.
const (
	StateLoading    = "loading"
	StateGenerating = "generating"
	StateVerified   = "verified"
	StateFailed     = "failed"
)

// Gate modes.
const (
	ModeCloseout  = "closeout"
	ModePromotion = "promotion"
)

// EvaluateGate is the pure closeout/promotion decision, isolated so it is testable without a
// database. A nil result means the database is acceptable for the requested boundary; a non-nil
// error means reject it.
//
//   - closeout  rejects only a KNOWN-broken half-seed (latest run == failed). A legacy database
//     with no runs, or a mid-flight run, may proceed.
//   - promotion additionally requires a verified/ready run to exist; a never-verified database is
//     not promotable.
func EvaluateGate(mode string, hasRun bool, latestState string, hasVerified bool) error {
	if hasRun && latestState == StateFailed {
		return fmt.Errorf("database is RESET_REQUIRED: latest seed_run state is %q — reseed before %s", StateFailed, mode)
	}
	switch mode {
	case ModeCloseout:
		return nil
	case ModePromotion:
		if !hasVerified {
			return errors.New("database is NOT promotable: no verified/ready seed_run exists")
		}
		return nil
	default:
		return fmt.Errorf("invalid mode %q (want %s|%s)", mode, ModeCloseout, ModePromotion)
	}
}
