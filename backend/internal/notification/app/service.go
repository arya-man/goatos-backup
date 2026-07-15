// Package app implements durable notification dispatch.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"runtime/debug"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/notification/domain"
	"github.com/vgoats/goatos/backend/internal/notification/ports"
	"github.com/vgoats/goatos/backend/internal/platform/kmetrics"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

const (
	defaultLimit        = 50
	defaultMaxAttempts  = 5
	defaultLeaseTimeout = 2 * time.Minute
	defaultBackoffBase  = 30 * time.Second
	defaultBackoffMax   = 15 * time.Minute
	maxErrorLength      = 240
)

type Config struct {
	Limit        int
	MaxAttempts  int
	LeaseTimeout time.Duration
	BackoffBase  time.Duration
	BackoffMax   time.Duration
	Now          func() time.Time
}

type Service struct {
	repo    ports.Repository
	gateway ports.Gateway
	config  Config
	log     *slog.Logger
}

func NewService(repo ports.Repository, gateway ports.Gateway, config Config, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, gateway: gateway, config: normalizeConfig(config), log: log}
}

func (s *Service) RunOnce(ctx context.Context, tenantID string) (domain.DispatchResult, error) {
	tenantID = strings.TrimSpace(tenantID)
	if !uuidutil.IsUUIDString(tenantID) {
		return domain.DispatchResult{}, fmt.Errorf("tenant-id is required")
	}
	now := s.now()
	reclaimed, err := s.repo.ReclaimStaleSending(ctx, tenantID, now, s.config.LeaseTimeout)
	if err != nil {
		return domain.DispatchResult{}, err
	}
	requests, err := s.repo.ClaimDue(ctx, ports.ClaimParams{
		TenantID:     tenantID,
		Limit:        s.config.Limit,
		MaxAttempts:  s.config.MaxAttempts,
		Now:          now,
		LeaseTimeout: s.config.LeaseTimeout,
	})
	if err != nil {
		return domain.DispatchResult{}, err
	}
	result := domain.DispatchResult{ReclaimedStaleCount: reclaimed, ClaimedCount: len(requests)}
	for _, request := range requests {
		if err := s.dispatchOne(ctx, request, &result); err != nil {
			return result, err
		}
	}

	// Export the post-drain backlog age for the 1-minute fast-lane SLO alert.
	// Best-effort: a metrics read must never fail the dispatch run. Records 0
	// when the backlog is empty so the gauge resets once delivery catches up.
	if age, found, ageErr := s.repo.OldestDuePendingAge(ctx, tenantID, s.now()); ageErr != nil {
		s.log.Warn("notification_backlog_age_query_failed", "error", ageErr.Error())
	} else if found {
		kmetrics.RecordNotifyBacklogAge(ctx, int64(age.Seconds()))
	} else {
		kmetrics.RecordNotifyBacklogAge(ctx, 0)
	}

	return result, nil
}

func (s *Service) dispatchOne(ctx context.Context, request domain.Request, result *domain.DispatchResult) error {
	now := s.now()
	sendStart := time.Now()
	err := s.sendSafely(ctx, request)
	kmetrics.RecordNotifySend(ctx, request.Channel, time.Since(sendStart).Seconds())
	if err == nil {
		if markErr := s.repo.MarkSent(ctx, request.TenantID, request.NotificationRequestID, request.LeaseToken, s.gateway.Name(), now); markErr != nil {
			return fmt.Errorf("mark notification sent: %w", markErr)
		}
		result.SentCount++
		return nil
	}

	var nextAttempt *time.Time
	if !errors.Is(err, ports.ErrChannelNotConfigured) && request.DeliveryAttempts < s.config.MaxAttempts {
		next := now.Add(s.backoff(request.DeliveryAttempts))
		nextAttempt = &next
	}
	if markErr := s.repo.MarkFailed(ctx, request.TenantID, request.NotificationRequestID, request.LeaseToken, s.gateway.Name(), sanitizeError(err), nextAttempt, now); markErr != nil {
		return fmt.Errorf("mark notification failed: %w", markErr)
	}
	if nextAttempt == nil {
		result.ExhaustedCount++
		kmetrics.RecordNotifyExhausted(ctx, request.Channel)
	} else {
		result.FailedCount++
		kmetrics.RecordNotifyFailure(ctx, request.Channel)
	}
	return nil
}

func (s *Service) sendSafely(ctx context.Context, request domain.Request) (err error) {
	defer func() {
		if p := recover(); p != nil {
			s.log.ErrorContext(ctx, "notification_gateway_panic",
				slog.Any("panic", p),
				slog.String("stack", string(debug.Stack())),
				slog.String("notification_request_id", request.NotificationRequestID),
				slog.String("calendar_event_id", request.CalendarEventID),
				slog.String("channel", request.Channel),
			)
			err = errors.New("notification gateway panic")
		}
	}()
	return s.gateway.Send(ctx, request)
}

func (s *Service) backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Duration(float64(s.config.BackoffBase) * math.Pow(2, float64(attempt-1)))
	if delay <= 0 || delay > s.config.BackoffMax {
		return s.config.BackoffMax
	}
	return delay
}

func (s *Service) now() time.Time {
	return s.config.Now().UTC()
}

func normalizeConfig(config Config) Config {
	if config.Limit <= 0 {
		config.Limit = defaultLimit
	}
	if config.Limit > 500 {
		config.Limit = 500
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = defaultMaxAttempts
	}
	if config.LeaseTimeout <= 0 {
		config.LeaseTimeout = defaultLeaseTimeout
	}
	if config.BackoffBase <= 0 {
		config.BackoffBase = defaultBackoffBase
	}
	if config.BackoffMax <= 0 {
		config.BackoffMax = defaultBackoffMax
	}
	if config.BackoffMax < config.BackoffBase {
		config.BackoffMax = config.BackoffBase
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return config
}

func sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		msg = "notification_delivery_failed"
	}
	if len(msg) > maxErrorLength {
		return msg[:maxErrorLength]
	}
	return msg
}
