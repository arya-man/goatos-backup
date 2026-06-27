package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	outboxpg "github.com/vgoats/goatos/backend/internal/outbox/adapters/postgres"
	outboxdomain "github.com/vgoats/goatos/backend/internal/outbox/domain"
	outboxports "github.com/vgoats/goatos/backend/internal/outbox/ports"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("outbox-dlq", flag.ContinueOnError)
	mode := fs.String("mode", "list", "list or replay")
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	status := fs.String("status", outboxdomain.StatusDeadLetter, "status to list: dead_letter or failed")
	eventType := fs.String("event-type", "", "optional event_type filter")
	topic := fs.String("topic", "", "optional topic filter")
	outboxIDs := fs.String("outbox-id", "", "comma-separated outbox ids to replay")
	reason := fs.String("reason", "", "operator reason for replay")
	limit := fs.Int("limit", 50, "max rows to list")
	timeout := fs.Duration("timeout", 30*time.Second, "command timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*tenantID) == "" {
		return errors.New("tenant-id is required")
	}
	if *limit < 1 || *limit > 500 {
		return errors.New("limit must be between 1 and 500")
	}
	normalizedStatus := strings.ToLower(strings.TrimSpace(*status))
	if normalizedStatus != outboxdomain.StatusDeadLetter && normalizedStatus != outboxdomain.StatusFailed {
		return errors.New("status must be dead_letter or failed")
	}
	normalizedMode := strings.ToLower(strings.TrimSpace(*mode))
	var replayIDs []string
	if normalizedMode == "replay" {
		replayIDs = splitCSV(*outboxIDs)
		if len(replayIDs) == 0 {
			return errors.New("outbox-id is required for replay")
		}
		if strings.TrimSpace(*reason) == "" {
			return errors.New("reason is required for replay")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	repo := outboxpg.NewRepository(pool, pgCfg.QueryTimeout)
	switch normalizedMode {
	case "list":
		return list(ctx, repo, *tenantID, normalizedStatus, *eventType, *topic, *limit)
	case "replay":
		n, err := repo.ReplayDeadLetters(ctx, outboxports.ReplayDeadLettersParams{
			TenantID:  *tenantID,
			OutboxIDs: replayIDs,
			Reason:    *reason,
			Now:       time.Now().UTC(),
		})
		if err != nil {
			return err
		}
		fmt.Printf("outbox dlq replayed=%d tenant=%s\n", n, *tenantID)
		return nil
	default:
		return errors.New("mode must be list or replay")
	}
}

type deadLetterLister interface {
	ListDeadLetters(context.Context, outboxports.DeadLetterQuery) ([]outboxdomain.DeadLetterMessage, error)
}

func list(ctx context.Context, repo deadLetterLister, tenantID, status, eventType, topic string, limit int) error {
	messages, err := repo.ListDeadLetters(ctx, outboxports.DeadLetterQuery{
		TenantID:  tenantID,
		Status:    status,
		EventType: eventType,
		Topic:     topic,
		Limit:     limit,
	})
	if err != nil {
		return err
	}
	fmt.Println("outbox_id\tstatus\tattempts\tevent_type\ttopic\taggregate\tlast_error\tupdated_at")
	for _, message := range messages {
		fmt.Printf("%s\t%s\t%d\t%s\t%s\t%s/%s\t%s\t%s\n",
			message.OutboxID,
			message.Status,
			message.AttemptCount,
			message.EventType,
			message.Topic,
			message.AggregateType,
			message.AggregateID,
			message.LastError,
			message.UpdatedAt.UTC().Format(time.RFC3339),
		)
	}
	return nil
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}
