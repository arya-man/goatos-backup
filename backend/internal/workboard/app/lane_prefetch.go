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

// batchedCounts reads every batchable source's count in ONE round trip, then every populated
// prefetch lane page of those sources in ONE more, seeding the request memo the lane reads
// consume. A failed batch falls back to the per-source reads, so a single failing source still
// degrades alone exactly as before.
func (s *Service) batchedCounts(ctx context.Context, q domain.Query, idx []int, scoped []ports.Source, sourceIndexes []int, done func(i int, counts map[domain.WorkState]int, degraded bool)) {
	sourceCtx, cancel := context.WithTimeout(ctx, maxSummarySourceDuration)
	defer cancel()
	sq := ports.SourceQuery{TenantID: q.TenantID, ParkID: q.ParkID, BusinessDate: q.BusinessDate, OwnerUserID: q.OwnerUserID, WorkStates: q.WorkStates}
	counts := make([]map[domain.WorkState]int, len(idx))
	stmts := make([]ports.Statement, 0, len(idx))
	ok := true
	for k, i := range idx {
		st, err := scoped[i].(ports.BatchSource).CountStatement(sq, &counts[k])
		if err != nil {
			ok = false
			break
		}
		stmts = append(stmts, st)
	}
	start := time.Now()
	if ok && s.batch.RunBatch(sourceCtx, stmts) == nil {
		recordTiming(ctx, "source_count_batch", start)
		for k, i := range idx {
			done(i, counts[k], false)
		}
		s.batchedPrefetch(sourceCtx, q, idx, scoped, sourceIndexes, counts)
		return
	}
	// Fallback: the per-source reads, sequential inside this one worker's budget.
	for _, i := range idx {
		src := scoped[i]
		c, err := src.CountByState(sourceCtx, sq)
		if err != nil {
			done(i, nil, true)
			continue
		}
		done(i, c, false)
		s.prefetchSourceLanes(sourceCtx, sourceIndexes[i], src, q, c)
	}
}

func (s *Service) batchedPrefetch(ctx context.Context, summary domain.Query, idx []int, scoped []ports.Source, sourceIndexes []int, counts []map[domain.WorkState]int) {
	intents, _ := ctx.Value(lanePrefetchContextKey{}).([]domain.Query)
	if len(intents) == 0 {
		return
	}
	type pending struct {
		index int
		query ports.SourceQuery
		rows  []domain.Row
	}
	var work []*pending
	for k, i := range idx {
		src := scoped[i]
		for _, query := range intents {
			if query.TenantID != summary.TenantID || query.ParkID != summary.ParkID || query.BusinessDate != summary.BusinessDate || query.OwnerUserID != summary.OwnerUserID || !query.WantsModule(src.Module()) {
				continue
			}
			populated := false
			for state, count := range counts[k] {
				if count > 0 && query.WantsState(state) {
					populated = true
					break
				}
			}
			if populated {
				work = append(work, &pending{index: i, query: ports.SourceQuery{TenantID: query.TenantID, ParkID: query.ParkID, BusinessDate: query.BusinessDate, OwnerUserID: query.OwnerUserID, WorkStates: query.WorkStates, Limit: query.Limit + 1}})
			}
		}
	}
	stmts := make([]ports.Statement, 0, len(work))
	for _, w := range work {
		st, err := scoped[w.index].(ports.BatchSource).ListStatement(w.query, &w.rows)
		if err != nil {
			return
		}
		stmts = append(stmts, st)
	}
	start := time.Now()
	// A failed prefetch batch seeds nothing; the lane reads run their own reads as before.
	if len(stmts) == 0 || s.batch.RunBatch(ctx, stmts) != nil {
		return
	}
	recordTiming(ctx, "source_prefetch_batch", start)
	for _, w := range work {
		rows := w.rows
		_, _ = s.sourceRows(ctx, sourceIndexes[w.index], seededSource{scoped[w.index], rows}, w.query)
	}
}

// seededSource answers ListRows with rows a batch already read, so sourceRows stores them in
// the request memo under the exact key the lane read looks up.
type seededSource struct {
	ports.Source
	rows []domain.Row
}

func (s seededSource) ListRows(context.Context, ports.SourceQuery) ([]domain.Row, error) {
	return s.rows, nil
}
