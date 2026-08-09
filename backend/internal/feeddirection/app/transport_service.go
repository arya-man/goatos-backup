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
	ShedName, PartitionLabel                                                  string // used internally by enqueuer; compose display via shared primitive
	CapturedAt                                                                time.Time
}
type SubmitTransportInput struct {
	TenantID, TaskID, ProofRef, OperatorID, IdempotencyKey, ActorID, ActorType, TraceID string
	// AuthorizedParkIDs is the caller's own park scope, resolved at the HTTP boundary. EMPTY means
	// unrestricted -- a tenant-wide principal, or an internal/service context with no grants at all
	// (the same escape hatch ResolveAuthorizedParkScopeForCapabilities has always had).
	//
	// It is checked against the TASK's park because this route names no park of its own: the id in
	// the path is the only input, so without this a park-scoped operator holding one valid task id
	// could submit against any shed in the tenant.
	AuthorizedParkIDs []string
}

type ListTransportTasksInput struct {
	TenantID, ActorID, Date, ParkID, ShedID, Status, Cursor string
	Limit                                                   int
}

func (s *Service) MaterializeTransportTasks(ctx context.Context, tenantID string, asOf time.Time) (ports.MaterializeTransportResult, error) {
	if s.transports == nil {
		return ports.MaterializeTransportResult{}, ports.ErrTransportTaskNotActionable
	}
	return s.transports.MaterializeTransportTasks(ctx, ports.MaterializeTransportParams{TenantID: strings.TrimSpace(tenantID), AsOf: asOf})
}

func (s *Service) ListTransportTasks(ctx context.Context, in ListTransportTasksInput) (ports.FeedTransportTaskPage, error) {
	if s.transports == nil {
		return ports.FeedTransportTaskPage{}, ports.ErrTransportTaskNotActionable
	}
	day, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(in.Date), biztime.DefaultLocation())
	if err != nil {
		return ports.FeedTransportTaskPage{}, ports.ErrInvalidTargetDate
	}
	status := strings.TrimSpace(in.Status)
	if status != "" && status != "due" && status != "verification_due" && status != "rework" && status != "completed" {
		return ports.FeedTransportTaskPage{}, ports.ErrInvalidTransportStatus
	}
	return s.transports.ListTransportTasks(ctx, ports.ListTransportTasksParams{
		TenantID: strings.TrimSpace(in.TenantID),
		ActorID:  strings.TrimSpace(in.ActorID),
		Day:      day,
		ParkID:   strings.TrimSpace(in.ParkID),
		ShedID:   strings.TrimSpace(in.ShedID),
		Status:   status,
		Cursor:   strings.TrimSpace(in.Cursor),
		Limit:    in.Limit,
	})
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
	// Park clamp, BEFORE the proof is validated or anything is written.
	if !transportParkAllowed(in.AuthorizedParkIDs, task.ParkID) {
		return ports.SubmitTransportResult{}, ports.ErrTransportParkForbidden
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
		err = s.transportEnqueuer.EnqueueFeedTransportVerification(ctx, FeedTransportVerificationEnqueueRequest{TenantID: in.TenantID, AttemptID: res.AttemptID, ParkID: res.ParkID, ShedID: res.ShedID, ShedName: res.ShedName, PartitionLabel: res.PartitionLabel, ProofRef: in.ProofRef, OperatorID: in.OperatorID, CapturedAt: s.now().UTC(), IdempotencyKey: "feed-transport-verification:" + res.AttemptID + ":" + strconv.Itoa(int(res.AttemptNo))})
	}
	return res, err
}

// transportParkAllowed reports whether the task's park is inside the caller's own scope. An empty
// authorized set means unrestricted (tenant-wide principal or internal context); a non-empty set
// must contain the task's park exactly.
func transportParkAllowed(authorizedParkIDs []string, taskParkID string) bool {
	if len(authorizedParkIDs) == 0 {
		return true
	}
	taskParkID = strings.TrimSpace(taskParkID)
	if taskParkID == "" {
		// A task with no park cannot be proven in scope, so a scoped caller is refused rather than
		// admitted by an absent value.
		return false
	}
	for _, parkID := range authorizedParkIDs {
		if strings.TrimSpace(parkID) == taskParkID {
			return true
		}
	}
	return false
}
