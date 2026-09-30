package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// PHONE TABS (docs/decisions/simple-task-phone-tabs.md): the authoring half of where a routine
// appears on the phone. The bar half is PhoneTabsFor, read by the bootstrap.

// ListTabs lists the tenant's phone tabs with their routines.
func (s *AuthoringService) ListTabs(ctx context.Context, tenantID string) ([]domain.Tab, error) {
	return s.repo.ListTabs(ctx, tenantID)
}

// CreateTab validates and writes a new phone tab.
func (s *AuthoringService) CreateTab(ctx context.Context, w ports.WriteParams, t domain.Tab) (domain.Tab, error) {
	if strings.TrimSpace(w.IdempotencyKey) == "" {
		return domain.Tab{}, ErrIdempotencyKeyRequired
	}
	t, err := prepareTab(t)
	if err != nil {
		return domain.Tab{}, err
	}
	return s.repo.CreateTab(ctx, w, t)
}

// UpdateTab validates and rewrites a phone tab and its routines.
func (s *AuthoringService) UpdateTab(ctx context.Context, w ports.WriteParams, t domain.Tab) (domain.Tab, error) {
	if strings.TrimSpace(w.IdempotencyKey) == "" {
		return domain.Tab{}, ErrIdempotencyKeyRequired
	}
	if !uuidutil.IsUUIDString(t.TabID) {
		return domain.Tab{}, ports.ErrTabNotFound
	}
	t, err := prepareTab(t)
	if err != nil {
		return domain.Tab{}, err
	}
	return s.repo.UpdateTab(ctx, w, t)
}

// SetTabStatus retires or restores a phone tab.
func (s *AuthoringService) SetTabStatus(ctx context.Context, w ports.WriteParams, tabID, status string, rowVersion int) (domain.Tab, error) {
	if strings.TrimSpace(w.IdempotencyKey) == "" {
		return domain.Tab{}, ErrIdempotencyKeyRequired
	}
	if !uuidutil.IsUUIDString(tabID) {
		return domain.Tab{}, ports.ErrTabNotFound
	}
	status = strings.TrimSpace(status)
	if status != domain.TabStatusActive && status != domain.TabStatusRetired {
		return domain.Tab{}, fmt.Errorf("%w: unknown status %q", domain.ErrInvalidTab, status)
	}
	return s.repo.SetTabStatus(ctx, w, tabID, status, rowVersion)
}

func prepareTab(t domain.Tab) (domain.Tab, error) {
	t = domain.NormalizeTab(t)
	for _, id := range t.RoutineIDs {
		if !uuidutil.IsUUIDString(id) {
			return domain.Tab{}, fmt.Errorf("%w: a routine on this tab is not valid", domain.ErrInvalidTab)
		}
	}
	if err := domain.ValidateTab(t); err != nil {
		return domain.Tab{}, err
	}
	return t, nil
}

// PhoneTabSource is what the bootstrap asks: which tabs one person's bar carries. A read error
// is the caller's to degrade (the bar is served without the tabs, never refused).
type PhoneTabSource struct {
	repo ports.Repository
}

// NewPhoneTabSource wires the bar read.
func NewPhoneTabSource(repo ports.Repository) *PhoneTabSource { return &PhoneTabSource{repo: repo} }

// PhoneTabsFor lists the tabs one person's bar carries.
func (s *PhoneTabSource) PhoneTabsFor(ctx context.Context, tenantID, userID string) ([]domain.PhoneTab, error) {
	if !uuidutil.IsUUIDString(tenantID) || !uuidutil.IsUUIDString(userID) {
		return nil, nil
	}
	return s.repo.PhoneTabsFor(ctx, tenantID, userID)
}
