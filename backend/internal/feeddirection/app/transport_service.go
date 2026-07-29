package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

var ErrTransportEnqueuerNotWired = errors.New("feeddirection: transport verification enqueuer is not wired")

type FeedTransportVerificationEnqueuer interface {
	EnqueueFeedTransportVerification(context.Context, FeedTransportVerificationEnqueueRequest) error
}
type FeedTransportVerificationEnqueueRequest struct {
	TenantID, AttemptID, ParkID, ShedID, ProofRef, OperatorID, IdempotencyKey string
	CapturedAt                                                                time.Time
}
type SubmitTransportInput struct{ TenantID, TaskID, ProofRef, OperatorID, IdempotencyKey, ActorID, ActorType, TraceID string }

func (s *Service) MaterializeTransportTasks(ctx context.Context, tenantID string, asOf time.Time) (ports.MaterializeTransportResult, error) {
	if s.transports == nil {
		return ports.MaterializeTransportResult{}, ports.ErrTransportTaskNotActionable
	}
	return s.transports.MaterializeTransportTasks(ctx, ports.MaterializeTransportParams{TenantID: strings.TrimSpace(tenantID), AsOf: asOf})
}

func (s *Service) ListTransportTasks(ctx context.Context, tenantID, actorID, date, cursor string, limit int) ([]ports.FeedTransportTask, string, error) {
	if s.transports == nil {
		return nil, "", ports.ErrTransportTaskNotActionable
	}
	day, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(date), biztime.DefaultLocation())
	if err != nil {
		return nil, "", ports.ErrInvalidTargetDate
	}
	return s.transports.ListTransportTasks(ctx, tenantID, day, actorID, limit, cursor)
}

func (s *Service) SubmitTransport(ctx context.Context, in SubmitTransportInput) (ports.SubmitTransportResult, error) {
	if s.transports == nil {
		return ports.SubmitTransportResult{}, ports.ErrTransportTaskNotActionable
	}
	if s.transportEnqueuer == nil {
		return ports.SubmitTransportResult{}, ErrTransportEnqueuerNotWired
	}
	in.ProofRef = strings.TrimSpace(in.ProofRef)
	if in.ProofRef == "" {
		return ports.SubmitTransportResult{}, ports.ErrTransportProofRequired
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return ports.SubmitTransportResult{}, ports.ErrIdempotencyRequired
	}
	task, err := s.transports.GetTransportTask(ctx, in.TenantID, in.TaskID)
	if err != nil {
		return ports.SubmitTransportResult{}, err
	}
	if s.proofs != nil {
		if err := s.proofs.ValidateLiveCameraVideo(ctx, in.TenantID, in.ProofRef, task.ShedID); err != nil {
			return ports.SubmitTransportResult{}, err
		}
	}
	res, err := s.transports.SubmitTransportAttempt(ctx, ports.SubmitTransportParams{TenantID: in.TenantID, TaskID: in.TaskID, ProofRef: in.ProofRef, OperatorID: in.OperatorID, IdempotencyKey: in.IdempotencyKey, ActorID: in.ActorID, ActorType: in.ActorType, TraceID: in.TraceID})
	if err != nil {
		return res, err
	}
	// Queue creation is idempotent. Re-enqueue an exact submit replay while the attempt is still
	// verification_due so a transient failure between the task commit and queue creation self-heals.
	if res.Status == "verification_due" {
		err = s.transportEnqueuer.EnqueueFeedTransportVerification(ctx, FeedTransportVerificationEnqueueRequest{TenantID: in.TenantID, AttemptID: res.AttemptID, ParkID: res.ParkID, ShedID: res.ShedID, ProofRef: in.ProofRef, OperatorID: in.OperatorID, CapturedAt: s.now().UTC(), IdempotencyKey: "feed-transport-verification:" + res.AttemptID + ":" + strconv.Itoa(int(res.AttemptNo))})
	}
	return res, err
}
