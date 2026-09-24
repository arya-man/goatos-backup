package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// executionRows intentionally exposes only the methods the canonical decoder uses.
type executionRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}
type executionCardSummaryRecord struct {
	ShedID             string   `json:"shed_uuid"`
	PartitionLabel     *string  `json:"partition_label"`
	AssignmentID       *string  `json:"assignment_id"`
	TaskID             *string  `json:"sop_task_id"`
	BatchID            *string  `json:"batch_id"`
	DriveID            *string  `json:"drive_id"`
	ObligationCount    int64    `json:"obligation_count"`
	DoneCount          int64    `json:"done_count"`
	OpenCount          int64    `json:"open_count"`
	HasMissed          bool     `json:"has_missed"`
	HasDeferred        bool     `json:"has_deferred"`
	HasOverdue         bool     `json:"has_overdue"`
	HasReviewPending   bool     `json:"has_review_pending"`
	HasRejected        bool     `json:"has_rejected"`
	VaccineLabels      []string `json:"vaccine_labels"`
	VaccineLabelCounts []string `json:"vaccine_label_counts"`
}

func addExecutionCardSummary(summaries map[string]*domain.ShedCardSummary, record executionCardSummaryRecord) {
	shedID := record.ShedID
	partLabel := record.PartitionLabel
	assignmentID := record.AssignmentID
	taskID := record.TaskID
	batchID := record.BatchID
	driveID := record.DriveID
	obligationCount := record.ObligationCount
	doneCount := record.DoneCount
	openCount := record.OpenCount
	hasMissed := record.HasMissed
	hasOverdue := record.HasOverdue
	hasReviewPending := record.HasReviewPending
	hasRejected := record.HasRejected
	vaccineLabels := record.VaccineLabels
	vaccineLabelCounts := record.VaccineLabelCounts
	// Compute status: DONE | DELAYED | SENT_BACK | PENDING
	var status domain.WorkState
	if hasRejected {
		status = domain.WorkStateRejected // SENT_BACK
	} else if hasOverdue || hasMissed {
		status = domain.WorkStateOverdue // DELAYED
	} else if hasReviewPending {
		status = domain.WorkStateVerificationPending
	} else if openCount == 0 && obligationCount > 0 {
		status = domain.WorkStateCompleted // DONE
	} else {
		status = domain.WorkStateDue // PENDING
	}

	// Build vaccine group summaries
	countByLabel := make(map[string]int64, len(vaccineLabelCounts))
	for _, entry := range vaccineLabelCounts {
		protocolName, doseCode, ok := strings.Cut(entry, "\x1f")
		if !ok {
			doseCode = entry
		}
		label := domain.VaccinationDoseDisplayLabel(protocolName, doseCode)
		if label == "" {
			continue
		}
		countByLabel[label]++
	}
	displayLabels := make([]string, 0, len(countByLabel))
	seenDisplayLabel := make(map[string]struct{}, len(countByLabel))
	for label := range countByLabel {
		if _, exists := seenDisplayLabel[label]; exists {
			continue
		}
		seenDisplayLabel[label] = struct{}{}
		displayLabels = append(displayLabels, label)
	}
	if len(displayLabels) == 0 {
		for _, label := range vaccineLabels {
			displayLabel := domain.VaccinationDoseDisplayLabel("", label)
			if displayLabel == "" {
				continue
			}
			if _, exists := seenDisplayLabel[displayLabel]; exists {
				continue
			}
			seenDisplayLabel[displayLabel] = struct{}{}
			displayLabels = append(displayLabels, displayLabel)
		}
	}
	sort.Strings(displayLabels)
	vaccineGroups := make([]domain.VaccineGroupSummary, 0, len(displayLabels))
	for _, label := range displayLabels {
		countLabel := ""
		if count := countByLabel[label]; count > 0 {
			countLabel = fmt.Sprintf("%d doses", count)
		}
		vaccineGroups = append(vaccineGroups, domain.VaccineGroupSummary{
			Label:      label,
			CountLabel: countLabel,
			Full:       openCount == 0,
		})
	}

	// The operator-day card is an operational location, not a source task/batch. Date overrides
	// may legitimately combine obligations from several source batches into one assignment.
	cardID := domain.BuildAssignmentCardID(shedID, domain.StringOrEmpty(partLabel), domain.StringOrEmpty(assignmentID), "", "", "")
	summaries[cardID] = &domain.ShedCardSummary{
		ShedID:         shedID,
		PartitionLabel: partLabel,
		AssignmentID:   assignmentID,
		TaskID:         taskID,
		BatchID:        batchID,
		DriveID:        driveID,
		Status:         status,
		DoneCount:      int(doneCount),
		TargetCount:    int(obligationCount),
		OpenCount:      int(openCount),
		NeedsRedo:      hasRejected,
		VaccineGroups:  vaccineGroups,
	}
}

// firstPageSummaryRows consumes the one extra JSON column without presenting a
// misleading pgx.Rows interface whose Values/FieldDescriptions differ from Scan.
type firstPageSummaryRows struct {
	rows      executionRows
	summaries []byte
}

func (r *firstPageSummaryRows) Next() bool { return r.rows.Next() }
func (r *firstPageSummaryRows) Err() error { return r.rows.Err() }
func (r *firstPageSummaryRows) Scan(dest ...any) error {
	var summary []byte
	if err := r.rows.Scan(append(dest, &summary)...); err != nil {
		return err
	}
	if len(summary) > 0 {
		r.summaries = summary
	}
	return nil
}

// The same canonical classification feeds both outputs. The page still has
// limit+1/keyset/total_count and badges still aggregate the complete filtered set.
// Summary JSON is emitted once, so large badge sets are not repeated per row.
// projection-review: membership=the existing canonical classified rows and unchanged page/card filters; group_key=page sort_row_key and summary shed/partition/task/batch identity; join_cardinality=one scalar whole-filter summary attached only to the first page row; pagination=limit+1 and total_count remain in the original page query while badges remain unpaginated; scope=all original tenant/park/shed/date/owner/state/severity parameters are bound unchanged.
var executionFirstPageWithSummariesSQL = executionClassifiedCTE + `,
combined_page AS (
 WITH ` + strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(vaccinationExecutionSQL, executionClassifiedCTE)), ",")), ";") + `
), combined_summaries AS (
` + strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(cardSummariesSQL, executionClassifiedCTE)), ";") + `
)
SELECT combined_page.*,
 CASE WHEN row_number() OVER (ORDER BY sort_rank,sort_due_micros,sort_row_key)=1
 THEN (SELECT COALESCE(jsonb_agg(to_jsonb(combined_summaries)),'[]'::jsonb) FROM combined_summaries)
 ELSE NULL::jsonb END AS card_summaries
FROM combined_page
ORDER BY sort_rank,sort_due_micros,sort_row_key`

// ListVaccinationExecutionFirstPageWithSummaries is an uncached request read.
// Existing separate methods retain their cache keys; this never populates them
// with a partial or mismatched result.
func (r *Repository) ListVaccinationExecutionFirstPageWithSummaries(ctx context.Context, q domain.ExecutionQuery) (domain.ExecutionProjectionPage, map[string]*domain.ShedCardSummary, error) {
	if q.Cursor != nil {
		return domain.ExecutionProjectionPage{}, nil, fmt.Errorf("combined execution read requires first page")
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if q.Limit <= 0 {
		q.Limit = 200
	}
	if q.AsOf.IsZero() {
		q.AsOf = time.Now()
	}
	if q.DueBefore.IsZero() {
		q.DueBefore = q.AsOf.Add(defaultExecutionHorizon)
	}
	park, shed, work, severity, partition := "", "", "", "", ""
	if q.ParkID != nil {
		park = *q.ParkID
	}
	if q.ShedID != nil {
		shed = *q.ShedID
	}
	if q.WorkState != nil {
		work = string(*q.WorkState)
	}
	if q.Severity != nil {
		severity = string(*q.Severity)
	}
	if q.PartitionLabel != nil {
		partition = strings.TrimSpace(*q.PartitionLabel)
	}
	// Unlike the separate badge query, this statement uses every bind position.
	// Reuse its prepared plan instead of repeatedly parsing the canonical CTE;
	// this caches SQL planning only, never rows or the caller's fresh as-of value.
	// Keep Exec's text result decoding, including timestamp location/offsets.
	cacheKey := fmt.Sprintf("execution_first_page_with_summaries|%s|%s|%s|%d|%s|%s|%t|%s|%s|%s",
		park, shed, vaccinationCacheAsOfKey(q.DueBefore), q.Limit, work, vaccinationCacheAsOfKey(q.AsOf),
		q.OpenOnly, q.OperatorScopeActorID, partition, severity)
	res, err := vaccinationCached(ctx, r, q.TenantID, cacheKey, func(ctx context.Context) (executionFirstPageWithSummaries, error) {
		page, summaries, err := r.listVaccinationExecutionFirstPageWithSummaries(ctx, q, park, shed, work, severity, partition)
		return executionFirstPageWithSummaries{page: page, summaries: summaries}, err
	})
	return res.page, res.summaries, err
}

type executionFirstPageWithSummaries struct {
	page      domain.ExecutionProjectionPage
	summaries map[string]*domain.ShedCardSummary
}

func (r *Repository) listVaccinationExecutionFirstPageWithSummaries(ctx context.Context, q domain.ExecutionQuery, park, shed, work, severity, partition string) (domain.ExecutionProjectionPage, map[string]*domain.ShedCardSummary, error) {
	bound := sqlbind.MustBind(executionFirstPageWithSummariesSQL, q.TenantID, park, shed, q.DueBefore, q.Limit, work, q.AsOf, q.AsOf.Add(-defaultClosedHistoryAge), severity, q.OpenOnly, false, 0, int64(0), "", q.OperatorScopeActorID, partition)
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return domain.ExecutionProjectionPage{}, nil, fmt.Errorf("vaccination execution combined query: %w", err)
	}
	defer rows.Close()
	wrapped := &firstPageSummaryRows{rows: rows}
	page, err := scanExecutionProjectionPage(wrapped, q.Limit)
	if err != nil {
		return domain.ExecutionProjectionPage{}, nil, err
	}
	summaries := map[string]*domain.ShedCardSummary{}
	if len(wrapped.summaries) > 0 {
		var records []executionCardSummaryRecord
		if err := json.Unmarshal(wrapped.summaries, &records); err != nil {
			return domain.ExecutionProjectionPage{}, nil, fmt.Errorf("vaccination execution combined summaries: %w", err)
		}
		for _, record := range records {
			addExecutionCardSummary(summaries, record)
		}
	}
	return page, summaries, nil
}
