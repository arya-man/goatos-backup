package app

import (
	"context"

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
