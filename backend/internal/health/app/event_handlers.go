package app

import (
	"context"
	"encoding/json"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"strings"
)

const (
	EventCountsDeathReported = "counts.death.reported"
	EventCountsDeathRejected = "counts.death.rejected"
	EventGoatExited          = "goat.exited"
)

type deathPayload struct {
	GoatID     string `json:"goat_id"`
	ExitReason string `json:"exit_reason"`
	// The coded cause the operator named, carried on the exit event so this consumer can
	// mark WHICH of the animal's open cases was the cause. Empty for a normal death, and
	// for every death recorded before causes existed.
	DeathCauseKey  string `json:"death_cause_key"`
	DeathCauseKind string `json:"death_cause_kind"`
}
type deathStore interface {
	HoldForDeathReview(context.Context, string, string) error
	ResumeAfterDeathRejected(context.Context, string, string) error
	CloseForApprovedDeath(context.Context, string, string, domain.DeathCause) error
}
type DeathLifecycleHandler struct{ repo deathStore }

func NewDeathLifecycleHandler(repo deathStore) *DeathLifecycleHandler {
	return &DeathLifecycleHandler{repo: repo}
}
func (h *DeathLifecycleHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventCountsDeathReported, eventbus.HandlerFunc(h.handleReported))
	bus.Subscribe(EventCountsDeathRejected, eventbus.HandlerFunc(h.handleRejected))
	bus.Subscribe(EventGoatExited, eventbus.HandlerFunc(h.handleExited))
}
func decodeDeath(e eventbus.Event) (deathPayload, error) {
	var p deathPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return p, eventbus.PermanentError(err)
		}
	}
	if strings.TrimSpace(p.GoatID) == "" {
		p.GoatID = strings.TrimSpace(e.Key)
	}
	return p, nil
}
func (h *DeathLifecycleHandler) handleReported(ctx context.Context, e eventbus.Event) error {
	p, err := decodeDeath(e)
	if err != nil || p.GoatID == "" || e.TenantID == "" {
		return err
	}
	return h.repo.HoldForDeathReview(ctx, e.TenantID, p.GoatID)
}
func (h *DeathLifecycleHandler) handleRejected(ctx context.Context, e eventbus.Event) error {
	p, err := decodeDeath(e)
	if err != nil || p.GoatID == "" || e.TenantID == "" {
		return err
	}
	return h.repo.ResumeAfterDeathRejected(ctx, e.TenantID, p.GoatID)
}
func (h *DeathLifecycleHandler) handleExited(ctx context.Context, e eventbus.Event) error {
	p, err := decodeDeath(e)
	if err != nil || p.GoatID == "" || e.TenantID == "" {
		return err
	}
	if strings.ToLower(strings.TrimSpace(p.ExitReason)) != "died" {
		return nil
	}
	// EVERY open case still closes; the cause only decides which one is MARKED as the
	// cause. A death with no coded cause closes exactly as it always did.
	return h.repo.CloseForApprovedDeath(ctx, e.TenantID, p.GoatID, domain.DeathCause{
		Key:  strings.TrimSpace(p.DeathCauseKey),
		Kind: strings.TrimSpace(p.DeathCauseKind),
	})
}
