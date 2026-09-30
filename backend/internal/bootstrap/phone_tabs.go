package bootstrap

import (
	"context"

	penroutinesapp "github.com/vgoats/goatos/backend/internal/penroutines/app"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// penRoutineModuleTabs hands the bootstrap the web-defined phone tabs pen routines own
// (docs/decisions/simple-task-phone-tabs.md) without either module importing the other.
type penRoutineModuleTabs struct {
	src *penroutinesapp.PhoneTabSource
}

func (a penRoutineModuleTabs) ModuleTabs(ctx context.Context, tenantID, userID string) ([]workforceapp.ModuleTab, error) {
	tabs, err := a.src.PhoneTabsFor(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	out := make([]workforceapp.ModuleTab, 0, len(tabs))
	for _, t := range tabs {
		out = append(out, workforceapp.ModuleTab{ModuleKey: t.ModuleKey, Key: t.Key, Label: t.Label, Href: t.Href(), Icon: t.IconKey})
	}
	return out, nil
}
