package postgres

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type LiveNotificationSource struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

func NewLiveNotificationSource(pool *pgxpool.Pool, log *slog.Logger) *LiveNotificationSource {
	if log == nil {
		log = slog.Default()
	}
	return &LiveNotificationSource{pool: pool, log: log}
}

func (s *LiveNotificationSource) Start(ctx context.Context, publish func(tenantID string)) {
	if s == nil || s.pool == nil || publish == nil {
		return
	}
	go s.listenLoop(ctx, publish)
}

func (s *LiveNotificationSource) listenLoop(ctx context.Context, publish func(tenantID string)) {
	backoff := time.Second
	for {
		if err := s.listenOnce(ctx, publish); err != nil && ctx.Err() == nil {
			s.log.Warn("herd_signals_live_notify_listener_failed", "error", err.Error())
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 10*time.Second {
			backoff *= 2
		}
	}
}

func (s *LiveNotificationSource) listenOnce(ctx context.Context, publish func(tenantID string)) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+liveNotifyChannel); err != nil {
		return err
	}
	s.log.Info("herd_signals_live_notify_listener_started", "channel", liveNotifyChannel)

	for {
		notification, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		if notification.Channel != liveNotifyChannel {
			continue
		}
		tenantID := tenantIDFromLiveNotification(notification.Payload)
		if tenantID == "" {
			s.log.Warn("herd_signals_live_notify_invalid_payload")
			continue
		}
		publish(tenantID)
	}
}

func tenantIDFromLiveNotification(payload string) string {
	var parsed struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return ""
	}
	return parsed.TenantID
}
