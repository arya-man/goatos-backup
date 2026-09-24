package postgres

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestShedSummaryAggregatesAreMaterialized pins the /vaccination/sheds plan fix
// (docs/perf/2026-09-24-stg-latency/audit/part-vaccination.md N9).
//
// Inlined, due_agg and drive_ops sat on the inner side of nested-loop LEFT JOINs under a rows=1
// misestimate on the shed list, so Postgres re-sorted and re-aggregated ~5.8k obligation rows once
// PER SHED (loops=102): 489 ms of a 500 ms statement on the OCI clone. MATERIALIZED computes each
// pre-grouped set once and the joins read the result: 49 ms, same rows.
func TestShedSummaryAggregatesAreMaterialized(t *testing.T) {
	for _, cte := range []string{"due_agg AS MATERIALIZED (", "drive_ops AS MATERIALIZED ("} {
		if !strings.Contains(shedSummaryCanonicalReadSQL, cte) {
			t.Errorf("shedSummaryCanonicalReadSQL lost %q: the per-shed aggregate re-runs once per shed row", cte)
		}
	}
}

// TestShedSummaryOrderIsTotal: every sort ends on the row's identity (shed_id, partition_label).
// The sorts stopped at shed_name, and a partitioned shed is one row PER PARTITION with the same
// name, so the partitions of one shed tied: their order -- and, at a LIMIT/OFFSET boundary, which
// of them landed on the page -- was up to the plan. Found by the old-vs-new output diff on the OCI
// clone: the same statement returned "Part 1" or "Part 4" in the 50th slot depending on the plan.
func TestShedSummaryOrderIsTotal(t *testing.T) {
	for _, sort := range []domain.ShedSummarySort{"", domain.ShedSortParkShed, domain.ShedSortDueDesc, domain.ShedSortAnimalDesc, domain.ShedSortNextDue} {
		if got := shedSummaryOrderBy(sort); !strings.HasSuffix(got, "shed_id ASC, partition_label ASC NULLS FIRST") {
			t.Errorf("shedSummaryOrderBy(%q) = %q: not a total order, partitions of one shed tie", sort, got)
		}
	}
}
