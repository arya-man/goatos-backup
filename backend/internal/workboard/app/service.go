// Package app composes the registered sources into one board read.
package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// Service is the board read. It holds the source registry in board order and walks it
// with a global keyset.
type Service struct {
	sources []ports.Source
}

// NewService builds the registry. Sources are sorted into board order (module order,
// then source type) once, here, so the keyset is stable regardless of registration order.
func NewService(sources ...ports.Source) *Service {
	s := &Service{sources: append([]ports.Source(nil), sources...)}
	sort.SliceStable(s.sources, func(i, j int) bool {
		a, b := s.sources[i], s.sources[j]
		ai, bi := moduleIndex(a.Module()), moduleIndex(b.Module())
		if ai != bi {
			return ai < bi
		}
		return a.SourceType() < b.SourceType()
	})
	return s
}

// RegisteredModules returns the distinct modules with at least one source, in board order.
func (s *Service) RegisteredModules() []domain.Module {
	seen := map[domain.Module]struct{}{}
	out := []domain.Module{}
	for _, src := range s.sources {
		if _, ok := seen[src.Module()]; ok {
			continue
		}
		seen[src.Module()] = struct{}{}
		out = append(out, src.Module())
	}
	return out
}

// List serves one page. The cursor names the source the previous page stopped in and the
// last source_id it emitted; that source is asked for rows after it, and every later
// source from its start, until the page is full. Earlier sources are never touched.
func (s *Service) List(ctx context.Context, q domain.Query) (domain.Page, error) {
	q, err := q.Normalize()
	if err != nil {
		return domain.Page{}, err
	}
	page := domain.Page{Rows: []domain.Row{}}
	start := 0
	after := ""
	if !q.Cursor.IsZero() {
		start = -1
		for i, src := range s.sources {
			if src.Module() == q.Cursor.Module && src.SourceType() == q.Cursor.SourceType {
				start = i
				after = q.Cursor.SourceID
				break
			}
		}
		if start < 0 {
			return domain.Page{}, domain.ErrInvalidCursor
		}
	}
	// scale-guard:ignore: bounded walk over the fixed source registry (one entry per module
	// source type, ~10), not a per-row fan-out; each call is one indexed keyset read bounded
	// to a single tenant, park and business date.
	for i := start; i < len(s.sources); i++ {
		src := s.sources[i]
		if !q.WantsModule(src.Module()) {
			after = ""
			continue
		}
		need := q.Limit - len(page.Rows)
		if need <= 0 {
			break
		}
		rows, err := src.ListRows(ctx, ports.SourceQuery{
			TenantID: q.TenantID, ParkID: q.ParkID, BusinessDate: q.BusinessDate,
			OwnerUserID: q.OwnerUserID, WorkStates: q.WorkStates,
			AfterSourceID: after, Limit: need + 1,
		})
		if err != nil {
			// A bad cursor is the client's error and fails the read; any other source failure
			// (a timeout on the heavy process-integrity read, a transient DB blip) must NOT
			// blank the whole board. Record the module as degraded and serve the rest: one slow
			// source shows as "couldn't load", never an empty board (maintainer decision
			// 2026-09-12).
			if errors.Is(err, domain.ErrInvalidCursor) {
				return domain.Page{}, err
			}
			page.Degraded = appendModule(page.Degraded, src.Module())
			after = ""
			continue
		}
		after = ""
		more := len(rows) > need
		if more {
			rows = rows[:need]
		}
		page.Rows = append(page.Rows, rows...)
		if more {
			last := rows[len(rows)-1]
			page.NextCursor = domain.Cursor{Module: last.Module, SourceType: last.SourceType, SourceID: last.SourceID}.String()
			return page, nil
		}
	}
	// The page filled exactly at a source boundary, or the registry ran out. A cursor is
	// only emitted when a later source might still hold rows; probing that costs as much
	// as serving it, so the next request simply returns an empty page.
	if len(page.Rows) == q.Limit && start < len(s.sources) {
		last := page.Rows[len(page.Rows)-1]
		page.NextCursor = domain.Cursor{Module: last.Module, SourceType: last.SourceType, SourceID: last.SourceID}.String()
	}
	return page, nil
}

// Summary serves the whole-filter aggregate. One aggregate query per source in scope.
func (s *Service) Summary(ctx context.Context, q domain.Query) (domain.Summary, error) {
	q, err := q.Normalize()
	if err != nil {
		return domain.Summary{}, err
	}
	modules := q.Modules
	if q.NoModules {
		modules = []domain.Module{}
	} else if len(modules) == 0 {
		modules = s.RegisteredModules()
	}
	sum := domain.NewSummary(modules)
	// The eight sources are read CONCURRENTLY, each with the request deadline, so the summary
	// takes as long as the SLOWEST source, not the sum of all eight. A source that errors or
	// times out (the heavy process-integrity read is the one that does) is recorded as degraded
	// and its counts are simply absent; the summary never fails whole for one slow source, so
	// the board never blanks (maintainer decision 2026-09-12).
	type result struct {
		module   domain.Module
		counts   map[domain.WorkState]int
		degraded bool
	}
	// scale-guard:ignore: bounded fan-out over the fixed source registry (~10 entries), each
	// a single indexed aggregate bounded to one tenant, park and business date.
	scoped := make([]ports.Source, 0, len(s.sources))
	for _, src := range s.sources {
		if q.WantsModule(src.Module()) {
			scoped = append(scoped, src)
		}
	}
	results := make([]result, len(scoped))
	var wg sync.WaitGroup
	for i, src := range scoped {
		wg.Add(1)
		go func(i int, src ports.Source) {
			defer wg.Done()
			counts, err := src.CountByState(ctx, ports.SourceQuery{
				TenantID: q.TenantID, ParkID: q.ParkID, BusinessDate: q.BusinessDate,
				OwnerUserID: q.OwnerUserID, WorkStates: q.WorkStates,
			})
			if err != nil {
				results[i] = result{module: src.Module(), degraded: true}
				return
			}
			results[i] = result{module: src.Module(), counts: counts}
		}(i, src)
	}
	wg.Wait()
	for _, r := range results {
		if r.degraded {
			sum.Degraded = appendModule(sum.Degraded, r.module)
			continue
		}
		sum.Add(r.module, r.counts)
	}
	return sum, nil
}

// ErrNoSources is returned by NewService callers that forgot to register anything; a
// board with nothing on it is a wiring bug, not an empty day.
var ErrNoSources = errors.New("workboard: no sources registered")

func moduleIndex(m domain.Module) int {
	for i, x := range domain.Modules() {
		if x == m {
			return i
		}
	}
	return len(domain.Modules())
}

// maxFindPages bounds FindRow's walk: one source, one park, one day, at most this many
// keyset pages of MaxLimit. A board source holding more rows than that for one park-day is
// outside the 5k-50k envelope the board is sized for, and a flag on such a row is refused
// as not found rather than walking further.
const maxFindPages = 20

// FindRow looks one row up by key on the board the query describes: the same tenant, park,
// business date and module visibility as List, so a caller can only find what List would
// have shown them. It walks that ONE source's keyset in board order until the key matches.
func (s *Service) FindRow(ctx context.Context, q domain.Query, rowKey string) (domain.Row, bool, error) {
	q, err := q.Normalize()
	if err != nil {
		return domain.Row{}, false, err
	}
	key, err := domain.ParseCursor(rowKey)
	if err != nil || key.IsZero() {
		return domain.Row{}, false, domain.ErrInvalidRowKey
	}
	if !q.WantsModule(key.Module) {
		return domain.Row{}, false, nil
	}
	var src ports.Source
	for _, candidate := range s.sources {
		if candidate.Module() == key.Module && candidate.SourceType() == key.SourceType {
			src = candidate
			break
		}
	}
	if src == nil {
		return domain.Row{}, false, nil
	}
	after := ""
	// scale-guard:ignore: bounded keyset walk over ONE source for one tenant, park and
	// business date (maxFindPages pages of MaxLimit), on a rare director write, never a
	// per-row fan-out.
	for page := 0; page < maxFindPages; page++ {
		rows, err := src.ListRows(ctx, ports.SourceQuery{
			TenantID: q.TenantID, ParkID: q.ParkID, BusinessDate: q.BusinessDate,
			OwnerUserID: q.OwnerUserID, AfterSourceID: after, Limit: domain.MaxLimit,
		})
		if err != nil {
			return domain.Row{}, false, fmt.Errorf("workboard: %s/%s: %w", src.Module(), src.SourceType(), err)
		}
		for _, row := range rows {
			if row.SourceID == key.SourceID {
				return row, true, nil
			}
		}
		if len(rows) < domain.MaxLimit {
			return domain.Row{}, false, nil
		}
		after = rows[len(rows)-1].SourceID
	}
	return domain.Row{}, false, nil
}

// ErrRowNotFound is returned when a subtask read names a row the caller's board does not
// hold: a module outside the query's visibility, or a source that is not registered.
var ErrRowNotFound = errors.New("workboard: row not found")

// ListSubtasks serves one page of a row's subtasks from the source the row key names. The
// module must be inside q.Modules -- a caller cannot drill into a module they cannot see --
// and the page size is bounded to [DefaultSubtaskLimit, MaxSubtaskLimit]. The read is one
// bounded call on ONE source; the transport is expected to have resolved the row through
// FindRow first, so the owner clamp of an operator lens has already been applied.
func (s *Service) ListSubtasks(ctx context.Context, q domain.Query, rowKey, afterKey string, limit int) (domain.SubtaskPage, error) {
	q, err := q.Normalize()
	if err != nil {
		return domain.SubtaskPage{}, err
	}
	key, err := domain.ParseCursor(rowKey)
	if err != nil || key.IsZero() {
		return domain.SubtaskPage{}, domain.ErrInvalidRowKey
	}
	if !q.WantsModule(key.Module) {
		return domain.SubtaskPage{}, ErrRowNotFound
	}
	if _, _, err := domain.ParseSubtaskKey(afterKey); err != nil {
		return domain.SubtaskPage{}, err
	}
	var src ports.Source
	for _, candidate := range s.sources {
		if candidate.Module() == key.Module && candidate.SourceType() == key.SourceType {
			src = candidate
			break
		}
	}
	if src == nil {
		return domain.SubtaskPage{}, ErrRowNotFound
	}
	page, err := src.ListSubtasks(ctx, ports.SubtaskQuery{
		TenantID: q.TenantID, ParkID: q.ParkID, BusinessDate: q.BusinessDate,
		SourceID: key.SourceID, OwnerUserID: q.OwnerUserID,
		AfterKey: strings.TrimSpace(afterKey), Limit: domain.BoundSubtaskLimit(limit),
	})
	if err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("workboard: %s/%s subtasks: %w", src.Module(), src.SourceType(), err)
	}
	if page.Subtasks == nil {
		page.Subtasks = []domain.Subtask{}
	}
	return page, nil
}

// appendModule adds a module to a degraded list, keeping it distinct and stable so a client
// renders each module once.
func appendModule(list []domain.Module, m domain.Module) []domain.Module {
	for _, x := range list {
		if x == m {
			return list
		}
	}
	return append(list, m)
}
