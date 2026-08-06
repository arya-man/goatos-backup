package app

import (
	"context"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// The Colostrum lens service layer (docs/decisions/colostrum-milk-module.md).
//
// Reads only. There is deliberately NO colostrum write path: completing a feed from the Colostrum
// page goes through the very same AnswerAction / CompleteAction the Birth page uses, against the
// same workflow_actions row. That is what makes "done in Birth" and "done in Colostrum" one fact
// instead of two that have to be reconciled.

// ListColostrumDayInput is the transport-normalized colostrum list request.
type ListColostrumDayInput struct {
	TenantID string
	Date     string // YYYY-MM-DD; empty = today IST
	Filter   string
	PageSize int
	Cursor   string
}

// ListColostrumDay serves one keyset page of colostrum cards + that day's chips.
func (s *Service) ListColostrumDay(ctx context.Context, in ListColostrumDayInput) (domain.WorkflowListPage, error) {
	if strings.TrimSpace(in.TenantID) == "" {
		return domain.WorkflowListPage{}, domain.ErrMissingRequiredField
	}
	if !domain.ColostrumFilterAllowed(in.Filter) {
		return domain.WorkflowListPage{}, domain.ErrMissingRequiredField
	}
	now := s.now()
	date := strings.TrimSpace(in.Date)
	if date == "" {
		date = biztime.BusinessDate(now)
	}
	cursor, err := domain.DecodeWorkflowCursor(in.Cursor)
	if err != nil {
		return domain.WorkflowListPage{}, err
	}
	return s.repo.ListColostrumDay(ctx, domain.ColostrumDayQuery{
		TenantID:  in.TenantID,
		Date:      date,
		TodayDate: biztime.BusinessDate(now),
		Filter:    in.Filter,
		PageSize:  in.PageSize,
		Cursor:    cursor,
		Now:       now,
	})
}

// ColostrumDetail is one kid's colostrum feeds for one business date.
//
// It carries TWO action lists on purpose:
//
//	Detail.Actions -> the kid's COMPLETE action set, unfiltered
//	Visible        -> only the feeds due on the requested date, which is what the screen renders
//
// The full set has to survive to the transport layer because the blocked/enabled state of a feed is
// computed against its siblings, and its siblings are not all colostrum. `1st Colostrum` sits at
// seq 5 of section `main`, behind kid-clean, iodine dipping, front teeth and suck reflex
// (domain.OperatorActionBlocked). Handing the renderer a colostrum-only list would drop those four
// prerequisites from the sibling set and report the feed as READY, so a milk operator would tap it
// and receive a bare 409 action_out_of_sequence instead of an honest "earlier birth steps are
// pending". Scheduled sessions have the same dependency on `1st Colostrum` itself.
type ColostrumDetail struct {
	Detail  domain.WorkflowDetail
	Visible []domain.WorkflowAction
}

// GetColostrumDay serves one kid's colostrum detail for one business date.
//
// The returned card is re-counted to the DAY grain so the detail header cannot disagree with the
// list card the operator tapped: same total, same done, same next feed. Reusing the workflow-grain
// counters here would show "3/11" on a card that had just shown "1/5".
func (s *Service) GetColostrumDay(ctx context.Context, tenantID, workflowID, date string) (ColostrumDetail, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(workflowID) == "" {
		return ColostrumDetail{}, domain.ErrMissingRequiredField
	}
	now := s.now()
	if strings.TrimSpace(date) == "" {
		date = biztime.BusinessDate(now)
	}
	start, end, err := domain.ColostrumDayWindow(date)
	if err != nil {
		return ColostrumDetail{}, err
	}
	detail, err := s.repo.GetWorkflow(ctx, tenantID, workflowID, now)
	if err != nil {
		return ColostrumDetail{}, err
	}

	visible := make([]domain.WorkflowAction, 0, len(detail.Actions))
	for _, a := range detail.Actions {
		if !domain.IsColostrumAction(a.Section, a.ActionKey) || a.Status == domain.ActionStatusCanceled {
			continue
		}
		if a.DueAt == nil || a.DueAt.Before(start) || !a.DueAt.Before(end) {
			continue
		}
		visible = append(visible, a)
	}

	summary := domain.ColostrumDayCard(detail.Actions, start, end, now)
	detail.Card.Module = domain.ModuleColostrum
	detail.Card.ActionsTotal = summary.Total
	detail.Card.ActionsDone = summary.Done
	detail.Card.NextAction = summary.Next
	detail.Card.AwaitingVerification = false
	detail.Card.State = domain.WorkflowStateOpen
	if summary.Complete() {
		detail.Card.State = domain.WorkflowStateCompleted
	}
	if summary.Next != nil {
		detail.Card.NextDueAt = summary.Next.DueAt
	} else {
		detail.Card.NextDueAt = nil
	}

	return ColostrumDetail{Detail: detail, Visible: visible}, nil
}
