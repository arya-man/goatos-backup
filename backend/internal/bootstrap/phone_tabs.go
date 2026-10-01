package bootstrap

import (
	"context"

	"github.com/jackc/pgx/v5"

	penroutinespg "github.com/vgoats/goatos/backend/internal/penroutines/adapters/postgres"
	penroutinesapp "github.com/vgoats/goatos/backend/internal/penroutines/app"
	penroutinesdomain "github.com/vgoats/goatos/backend/internal/penroutines/domain"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	sopports "github.com/vgoats/goatos/backend/internal/sop/ports"
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

// phoneTaskSOPHook is the publish / retire hook a phone-task SOP drives
// (docs/decisions/simple-task-phone-tabs.md): it writes the tab and one routine per park inside
// the SOP publish transaction, and turns a refusal into the SOP module's 422 so the publish rolls
// back with the reason in farm words.
func phoneTaskSOPHook(repo *penroutinespg.Repository) soppg.VersionStatusHook {
	return func(ctx context.Context, tx pgx.Tx, e soppg.VersionStatusEvent) error {
		err := repo.SyncPhoneTaskSOP(ctx, tx, penroutinespg.PhoneTaskSOPEvent{
			TenantID: e.TenantID, ActorID: e.ActorID, SOPCode: e.SOPCode, SOPName: e.SOPName,
			Status: e.Status, StillPublished: e.StillPublished, FormDSL: e.FormDSL,
		})
		if err == nil {
			return nil
		}
		appErr := penroutinesapp.HTTPError(err)
		if appErr.HTTPStatus >= 500 {
			return err
		}
		return &sopports.PublishRefusedError{Code: appErr.Code, Message: appErr.Message}
	}
}

// phoneTaskSOPContract checks a phone-task document when its SOP version is SAVED, so the author
// hears about a bad document on save rather than first on publish. Who works at which park is
// checked on publish, under the write's lock.
func phoneTaskSOPContract(sopCode string, formDSL map[string]any, report *sopdomain.ValidationReport) {
	if !penroutinesdomain.HasPhoneTask(formDSL) {
		return
	}
	// The SOP's name is the tab label; it is checked on publish, where the name is known.
	if _, err := penroutinesdomain.ParsePhoneTask(sopCode, "Task", formDSL); err != nil {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{
			Field:   "form_dsl." + penroutinesdomain.PhoneTaskSection,
			Code:    "invalid_phone_task",
			Message: penroutinesapp.HTTPError(err).Message,
		})
	}
}
