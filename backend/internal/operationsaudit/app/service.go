package app

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/operationsaudit/domain"
	"github.com/vgoats/goatos/backend/internal/operationsaudit/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

type Service struct {
	repo ports.Repository
	now  func() time.Time
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo, now: func() time.Time { return time.Now().In(biztime.DefaultLocation()) }}
}

func (s *Service) List(ctx context.Context, q domain.Query, traceID string) (domain.ListResponse, error) {
	q = s.defaults(q)
	rows, next, err := s.repo.List(ctx, q)
	if err != nil {
		return domain.ListResponse{}, err
	}
	var encoded *string
	if next != nil {
		value, err := domain.EncodeCursor(*next)
		if err != nil {
			return domain.ListResponse{}, err
		}
		encoded = &value
	}
	return domain.ListResponse{Items: rows, NextCursor: encoded, TraceID: traceID}, nil
}

func (s *Service) Summary(ctx context.Context, q domain.Query, traceID string) (domain.SummaryResponse, error) {
	q = s.defaults(q)
	summary, err := s.repo.Summary(ctx, q)
	if err != nil {
		return domain.SummaryResponse{}, err
	}
	summary.TraceID = traceID
	return summary, nil
}

func (s *Service) defaults(q domain.Query) domain.Query {
	if q.Limit <= 0 {
		q.Limit = 100
	}
	if q.Limit > 500 {
		q.Limit = 500
	}
	now := s.now()
	if q.To == nil {
		q.To = &now
	}
	if q.From == nil {
		from := q.To.Add(-24 * time.Hour)
		q.From = &from
	}
	return q
}
