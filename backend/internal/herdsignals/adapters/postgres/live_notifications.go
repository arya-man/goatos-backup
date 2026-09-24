package postgres

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LiveNotificationSource LISTENs on herd_signals_live over its OWN dedicated pgx.Conn.
//
// It used to Acquire a main-pool connection and hold it forever inside WaitForNotification,
// permanently shrinking the request pool by one on every API instance (and on db-g1-small's
// small pool that is a real share of capacity). The dedicated conn is built from a COPY of the
// pool's ConnConfig so it inherits the same auth/dialer (Cloud SQL connector, IAM, TLS) without a
// second DSN, reconnects with capped exponential backoff, and is closed when ctx is cancelled
// (bootstrap cancels it before closePools on shutdown).
type LiveNotificationSource struct {
	connect func(ctx context.Context) (*pgx.Conn, error)
	log     *slog.Logger
}

func NewLiveNotificationSource(pool *pgxpool.Pool, log *slog.Logger) *LiveNotificationSource {
	if log == nil {
		log = slog.Default()
	}
	if pool == nil {
		return &LiveNotificationSource{log: log}
	}
	base := pool.Config().ConnConfig
	return &LiveNotificationSource{
		connect: func(ctx context.Context) (*pgx.Conn, error) {
			return pgx.ConnectConfig(ctx, base.Copy())
		},
		log: log,
	}
}

func (s *LiveNotificationSource) Start(ctx context.Context, publish func(tenantID string), publishAll func()) {
	if s == nil || s.connect == nil || publish == nil {
		return
	}
	go s.listenLoop(ctx, publish, publishAll)
}

func (s *LiveNotificationSource) listenLoop(ctx context.Context, publish func(tenantID string), publishAll func()) {
	backoff := time.Second
	for {
		connected, err := s.listenOnce(ctx, publish, publishAll)
		if connected {
			backoff = time.Second
		}
		if err != nil && ctx.Err() == nil {
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

// listenOnce owns one dedicated connection for its whole life. connected reports whether LISTEN
// succeeded, so the caller can reset its backoff after a healthy session.
func (s *LiveNotificationSource) listenOnce(ctx context.Context, publish func(tenantID string), publishAll func()) (connected bool, err error) {
	conn, err := s.connect(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()

	if _, err := conn.Exec(ctx, "LISTEN herd_signals_live"); err != nil {
		return false, err
	}
	s.log.Info("herd_signals_live_notify_listener_started", "channel", liveNotifyChannel)
	if publishAll != nil {
		publishAll()
	}

	for {
		// scale-guard:ignore: blocking LISTEN receive owner=herd-signals issue=perf/herd-live reason=WaitForNotification blocks on one dedicated connection for the next async notification; it is a receive loop, not a per-row query expiry=2027-06-30
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			return true, err
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
