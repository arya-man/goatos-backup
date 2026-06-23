package sopbridge

import (
	"context"
	"encoding/json"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// CompletionLister is the slice of the vaccination service this bridge needs: the still-recorded
// completion ids captured under a SOP task's submissions.
type CompletionLister interface {
	RecordedCompletionsByTask(ctx context.Context, tenantID, taskID string) ([]string, error)
}

// VerifyFanout translates a task-level SOP verify outcome into per-completion vaccination verify
// events. A SOP task covers a drive batch (many goats); each recorded completion under the task gets
// its own vaccination.verify.accepted/rejected event so the vaccination module applies the SM-5
// outcome per goat. Downstream handlers are idempotent, so re-running a fan-out is safe. It is a
// composition-layer adapter (cmd wiring), keeping the SOP and vaccination modules decoupled.
type VerifyFanout struct {
	vacc CompletionLister
	bus  eventbus.Bus
}

// NewVerifyFanout constructs the bridge over the vaccination completion lister and the bus.
func NewVerifyFanout(vacc CompletionLister, bus eventbus.Bus) *VerifyFanout {
	return &VerifyFanout{vacc: vacc, bus: bus}
}

// OnTaskVerified publishes a vaccination.verify.accepted event for every still-recorded completion
// under the verified task. Returns the count of completions fanned out.
func (f *VerifyFanout) OnTaskVerified(ctx context.Context, tenantID, taskID, verifiedBy string) (int, error) {
	return f.fanout(ctx, tenantID, taskID, vaccapp.EventVaccinationVerifyAccepted,
		vaccapp.VerificationEvent{VerifiedBy: verifiedBy})
}

// OnTaskReworked publishes a vaccination.verify.rejected event for every still-recorded completion
// under the reworked task. Returns the count of completions fanned out.
func (f *VerifyFanout) OnTaskReworked(ctx context.Context, tenantID, taskID, verifiedBy, reason string) (int, error) {
	return f.fanout(ctx, tenantID, taskID, vaccapp.EventVaccinationVerifyRejected,
		vaccapp.VerificationEvent{VerifiedBy: verifiedBy, Reason: reason})
}

func (f *VerifyFanout) fanout(ctx context.Context, tenantID, taskID, eventType string, base vaccapp.VerificationEvent) (int, error) {
	ids, err := f.vacc.RecordedCompletionsByTask(ctx, tenantID, taskID)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		ev := base
		ev.CompletionID = id
		payload, err := json.Marshal(ev)
		if err != nil {
			return 0, err
		}
		if err := f.bus.Publish(ctx, eventbus.Event{
			Type:     eventType,
			TenantID: tenantID,
			Key:      id,
			Payload:  payload,
		}); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}
