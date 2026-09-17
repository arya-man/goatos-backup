// Package app is the notification centre's use cases: read one page of the caller's own
// notifications, and mark some of the caller's own notifications read.
//
// There is no authorization decision in this package beyond "you get your own rows". The
// route table already established that the caller is an authenticated person
// (permissions.AppBootstrap), and the repository's predicate does the rest: the caller's
// own resolved workforce_member_id is the only address the feed is ever filtered by, so
// there is no scope, no role and no parameter that could widen it.
package app

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/notificationcentre/domain"
	"github.com/vgoats/goatos/backend/internal/notificationcentre/ports"
)

// Service is the module's use cases.
type Service struct {
	repo ports.Repository
}

// NewService constructs the service.
func NewService(repo ports.Repository) *Service { return &Service{repo: repo} }

// ListRequest is the transport's read request.
type ListRequest struct {
	TenantID string
	ActorID  string
	Cursor   string
	Limit    int
}

// List serves one page of the caller's own notifications, newest first.
func (s *Service) List(ctx context.Context, req ListRequest) (domain.Page, error) {
	limit, err := domain.ClampLimit(req.Limit)
	if err != nil {
		return domain.Page{}, err
	}
	return s.repo.ListNotifications(ctx, ports.ListParams{
		TenantID:       strings.TrimSpace(req.TenantID),
		MemberOrUserID: strings.TrimSpace(req.ActorID),
		Cursor:         strings.TrimSpace(req.Cursor),
		Limit:          limit,
	})
}

// MarkReadRequest is the transport's write request.
type MarkReadRequest struct {
	TenantID       string
	ActorID        string
	IDs            []string
	IdempotencyKey string
}

// MarkRead marks the caller's own notifications read and answers how many notifications
// actually moved from unread to read.
//
// Ids the caller does not own are refused at the STORAGE predicate, not here: this method
// cannot check ownership without a query, and a pre-check would be a second, divergent
// copy of the rule. What this method does own is shape -- a non-empty, bounded, well-formed
// uuid list -- so a malformed id never reaches the id array and never silently matches
// nothing for the wrong reason.
func (s *Service) MarkRead(ctx context.Context, req MarkReadRequest) (int, error) {
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return 0, domain.ErrIdempotencyKeyRequired
	}
	ids, err := normalizeIDs(req.IDs)
	if err != nil {
		return 0, err
	}
	return s.repo.MarkRead(ctx, ports.MarkReadParams{
		TenantID:       strings.TrimSpace(req.TenantID),
		MemberOrUserID: strings.TrimSpace(req.ActorID),
		IDs:            ids,
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
	})
}

// normalizeIDs trims, validates and DEDUPES the requested ids. A duplicate id in one batch
// is a client retry artefact, not an error, and must not be counted twice in read_count.
// A malformed id IS an error: silently dropping it would turn "mark these 5 read" into
// "mark 4 read" with a success response, and the fifth card would stay lit forever.
func normalizeIDs(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, domain.ErrNoIDs
	}
	if len(in) > domain.MaxMarkReadIDs {
		return nil, domain.ErrTooManyIDs
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		id := strings.TrimSpace(raw)
		if _, err := uuid.Parse(id); err != nil {
			return nil, domain.ErrInvalidID
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, domain.ErrNoIDs
	}
	return out, nil
}
