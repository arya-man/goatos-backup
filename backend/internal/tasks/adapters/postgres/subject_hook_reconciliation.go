package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

type subjectHookFact struct {
	ready bool
	key   string
	at    time.Time
}

// Receipt facts are applied by the same transaction that owns action/branch
// writes. A branch becoming eligible never has to wait for another source event.
func (r *Repository) reconcileSubjectHookActions(ctx context.Context, tx pgx.Tx, w *domain.WorkflowInstance, actions []domain.WorkflowAction) ([]domain.WorkflowAction, error) {
	if w.State == domain.WorkflowStateCanceled || (w.TemplateKey != domain.TemplateKeyAnimalPurchaseIntake && w.TemplateKey != domain.TemplateKeyFeedPurchaseIntake && w.TemplateKey != domain.TemplateKeySalesDeal) {
		return nil, nil
	}
	var subjectRef *string
	if err := tx.QueryRow(ctx, `SELECT subject_ref_id::text FROM workflow_instances WHERE tenant_id=$1::uuid AND workflow_id=$2::uuid`, w.TenantID, w.WorkflowID).Scan(&subjectRef); err != nil {
		return nil, err
	}
	if subjectRef == nil {
		return nil, nil
	}
	facts := map[string]subjectHookFact{}
	switch w.TemplateKey {
	case domain.TemplateKeyAnimalPurchaseIntake:
		var pending, decided int
		var revision int64
		var at time.Time
		err := tx.QueryRow(ctx, `SELECT pending,decided,revision,occurred_at FROM workflow_animal_purchase_decisions WHERE tenant_id=$1::uuid AND load_id=$2::uuid FOR UPDATE`, w.TenantID, *subjectRef).Scan(&pending, &decided, &revision, &at)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			facts[domain.EngineHookAnimalPurchaseDecision] = subjectHookFact{pending == 0 && decided > 0, fmt.Sprintf("animal-decision:%s:%d", *subjectRef, revision), at}
		}
	case domain.TemplateKeyFeedPurchaseIntake, domain.TemplateKeySalesDeal:
		rows, err := tx.Query(ctx, `SELECT hook,completed_at FROM workflow_subject_hook_receipts WHERE tenant_id=$1::uuid AND template_key=$2 AND subject_ref_id=$3::uuid`, w.TenantID, w.TemplateKey, *subjectRef)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var hook string
			var at time.Time
			if err := rows.Scan(&hook, &at); err != nil {
				rows.Close()
				return nil, err
			}
			facts[hook] = subjectHookFact{true, hook + ":" + *subjectRef + ":" + at.UTC().Format(time.RFC3339Nano), at}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	default:
		return nil, nil
	}
	var changed []domain.WorkflowAction
	for i := range actions {
		fact, ok := facts[actions[i].EngineHook]
		if !ok || actions[i].Status == domain.ActionStatusCanceled {
			continue
		}
		a := actions[i]
		eligible := true
		branchStatus := ""
		if a.AnswerGate != nil {
			eligible = false
			for _, q := range actions {
				if q.ActionKey != a.AnswerGate.Step {
					continue
				}
				if q.Status == domain.ActionStatusSkipped {
					branchStatus = domain.ActionStatusSkipped
					break
				}
				if (q.Status == domain.ActionStatusCompleted || q.Status == domain.ActionStatusInReview || q.Status == domain.ActionStatusRework) && q.AnswerValue != nil {
					eligible = a.AnswerGate.Satisfied(*q.AnswerValue)
					if !eligible {
						branchStatus = domain.ActionStatusSkipped
					}
				}
				break
			}
		}
		if !eligible {
			if branchStatus == "" {
				branchStatus = domain.ActionStatusPending
			}
			if a.Status != branchStatus {
				resetSubjectHook(&a, branchStatus)
			} else {
				continue
			}
		} else if fact.ready {
			if a.Status == domain.ActionStatusCompleted {
				continue
			}
			if a.Status == domain.ActionStatusSkipped {
				resetSubjectHook(&a, domain.ActionStatusPending)
			}
			key := fact.key + ":" + a.ActionID
			updated, replay, err := domain.ApplyComplete(a, domain.CompleteActionCommand{TenantID: w.TenantID, WorkflowID: w.WorkflowID, ActionID: a.ActionID, CompletedAt: fact.at, IdempotencyKey: key, RequestFingerprint: key})
			if err != nil {
				return nil, err
			}
			if replay {
				continue
			}
			a = updated
		} else {
			if a.Status != domain.ActionStatusCompleted {
				continue
			}
			resetSubjectHook(&a, domain.ActionStatusPending)
		}
		actions[i] = a
		changed = append(changed, a)
	}
	return changed, nil
}

func resetSubjectHook(a *domain.WorkflowAction, status string) {
	a.Status = status
	a.CompletedAt = nil
	a.CompletedBy = nil
	a.IdempotencyKey = nil
	a.RequestFingerprint = nil
	a.RowVersion++
}
