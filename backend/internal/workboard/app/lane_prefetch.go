package app

import (
	"context"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

type lanePrefetchContextKey struct{}

// WithPageLanePrefetch attaches cursor-free lane intents only to the main page
// summary context. Vocabulary summaries use the undecorated parent context.
func WithPageLanePrefetch(ctx context.Context, queries []domain.Query) context.Context {
	if !ports.HasRequestReadMemo(ctx) {
		return ctx
	}
	intents := make([]domain.Query, 0, len(queries))
	for _, query := range queries {
		normalized, err := query.Normalize()
		if err != nil || !normalized.Cursor.IsZero() {
			continue
		}
		normalized.WorkStates = append([]domain.WorkState(nil), normalized.WorkStates...)
		normalized.Modules = append([]domain.Module(nil), normalized.Modules...)
		intents = append(intents, normalized)
	}
	return context.WithValue(ctx, lanePrefetchContextKey{}, intents)
}

type sourceRowsKey struct {
	service                                 *Service
	sourceIndex                             int
	tenant, park, day, owner, states, after string
	limit                                   int
}

func (s *Service) sourceRows(ctx context.Context, index int, src ports.Source, query ports.SourceQuery) ([]domain.Row, error) {
	states := make([]string, len(query.WorkStates))
	for i, state := range query.WorkStates {
		states[i] = string(state)
	}
	key := sourceRowsKey{s, index, query.TenantID, query.ParkID, query.BusinessDate, query.OwnerUserID, strings.Join(states, "\x00"), query.AfterSourceID, query.Limit}
	return ports.RequestRead(ctx, key, func(ctx context.Context) ([]domain.Row, error) {
		start := time.Now()
		defer recordTiming(ctx, "source_read_"+string(src.Module())+"_"+src.SourceType()+"_"+laneTimingName(query.WorkStates), start)
		return src.ListRows(ctx, query)
	})
}

// Called inside the existing summary worker's permit, sequentially. It creates
// no additional source fanout and never treats a failed row read as a count error.
func (s *Service) prefetchSourceLanes(ctx context.Context, index int, src ports.Source, summary domain.Query, counts map[domain.WorkState]int) {
	intents, _ := ctx.Value(lanePrefetchContextKey{}).([]domain.Query)
	// scale-guard:ignore: at most four requested lane intents, sequential inside the existing bounded summary source worker; not per-row fanout.
	for _, query := range intents {
		if ctx.Err() != nil {
			return
		}
		if query.TenantID != summary.TenantID || query.ParkID != summary.ParkID || query.BusinessDate != summary.BusinessDate || query.OwnerUserID != summary.OwnerUserID || !query.WantsModule(src.Module()) {
			continue
		}
		populated := false
		for state, count := range counts {
			if count > 0 && query.WantsState(state) {
				populated = true
				break
			}
		}
		if !populated {
			continue
		}
		// Errors are evicted by RequestRead; normal lane assembly retries and retains
		// its existing degradation/recovery behavior.
		_, _ = s.sourceRows(ctx, index, src, ports.SourceQuery{TenantID: query.TenantID, ParkID: query.ParkID, BusinessDate: query.BusinessDate, OwnerUserID: query.OwnerUserID, WorkStates: query.WorkStates, Limit: query.Limit + 1})
	}
}
