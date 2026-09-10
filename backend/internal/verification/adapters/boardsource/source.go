// Package boardsource is Verification's contribution to the cross-module Work Board: one
// board row per verification item (one proof review), read from verification_items plus
// the org tables every module may read. It lives INSIDE the verification package so the
// isolation rule holds in both directions: verification reads only its own table here, and
// the board never reads a verification table at all.
//
// READ-ONLY and REPORTING-ONLY. Nothing here enqueues, samples, relabels or casts a verdict.
// The mapping from an item's status to a board work state is the ONLY business meaning
// this file adds, and it is stated once in workStateSQL.
//
// The row's OWNER is the OPERATOR whose work is under review, not the verifier: the board
// answers "whose work is where", and a proof awaiting a verdict is still that operator's
// work in the In review column. The verifier's own claim pool is the verifier queue, which
// has its own screen and is not a board source.
package boardsource

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/verificationcatalog"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// SourceType is the ref type carried on every verification board row.
const SourceType = "verification_item"

// Source implements ports.Source over verification's own item rows.
type Source struct {
	pool    *pgxpool.Pool
	timeout time.Duration
	labels  map[string]string
}

// New constructs the source. Category labels are resolved once from the verification
// catalog -- the same registry the verifier queue renders its tabs from -- so a board row
// and a queue tab never word the same category differently.
func New(pool *pgxpool.Pool, timeout time.Duration) *Source {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Source{pool: pool, timeout: timeout, labels: categoryLabels()}
}

func (s *Source) Module() domain.Module { return domain.ModuleVerification }
func (s *Source) SourceType() string    { return SourceType }

// workStateSQL is the one place a verification item status becomes a board work state.
//
//	approved -> completed            (verdict landed, human or not_sampled closeout alike)
//	rejected -> rejected             (sent back; the producer's rework path owns what follows)
//	pending  -> verification_pending (proof submitted; verdict outstanding)
//	withdrawn                        -> not on the board (the producer took the proof back,
//	                                    e.g. a packing reopened by the afternoon correction;
//	                                    nobody owes a verdict on it)
//
// Sampling is deliberately invisible here: an unsampled item is still pending until the
// closeout approves it, and the board reports the status the row actually holds.
const workStateSQL = `CASE
  WHEN v.status = 'approved' THEN 'completed'
  WHEN v.status = 'rejected' THEN 'rejected'
  ELSE 'verification_pending'
END`

// baseWhere binds every read to one tenant, one park and one business date.
//
// The business date is the day the proof was CAPTURED, in Asia/Kolkata. No index on
// verification_items covers a date-of-captured_at expression, so the day is bound as a
// half-open captured_at range [day start, next day start) computed in Go through biztime --
// the same shape the video log uses. Index: verification_items_video_log_day_idx
// (tenant_id, captured_at, item_id) from migration 000165 -- tenant equality then a
// captured_at range seek; park_id and the status filter are residual over one day's rows.
// (verification_items_pending_scope_idx is partial to status='pending' and carries no
// date; verification_items_created_pen_idx is keyed on created_at, not captured_at.)
const baseWhere = `
  v.tenant_id = $1::uuid
  AND v.captured_at >= $2::timestamptz
  AND v.captured_at < $3::timestamptz
  AND v.park_id = $4::uuid
  AND v.status <> 'withdrawn'
  AND ($5::uuid IS NULL OR v.operator_id = $5::uuid)`

// projection-review: membership=verification_items rows of ONE tenant and park whose captured_at falls in the half-open IST business day [day start, next day start), one row per item (primary key); group_key=(tenant_id, item_id) for the list and the derived board_state for the count; join_cardinality=locations park/shed on their primary key (1:1) and workforce_members filtered to status='active' on the partial-unique (tenant_id,user_id) index (at most 1), so no join fans an item out; pagination=keyset on item_id ASC after the cursor with LIMIT, state filter inside WHERE; scope=tenant_id, park_id, the captured_at day range and the optional operator predicate, repeated verbatim in countSQL.
const listSQL = `
WITH items AS (
  SELECT v.item_id, v.category, v.park_id, v.shed_id, COALESCE(v.partition_label, '') AS partition_label,
         v.captured_at, v.status, v.operator_id, COALESCE(v.subject_label, '') AS subject_label,
         ` + workStateSQL + ` AS board_state
  FROM verification_items v
  WHERE ` + baseWhere + `
    AND ($6::uuid IS NULL OR v.item_id > $6::uuid)
)
SELECT x.item_id::text, x.category, x.park_id::text, COALESCE(park.name, ''),
       COALESCE(x.shed_id::text, ''), COALESCE(shed.name, ''), x.partition_label,
       x.captured_at, x.status, x.board_state, x.subject_label,
       COALESCE(x.operator_id::text, ''), COALESCE(m.workforce_member_id::text, ''), COALESCE(m.display_name, '')
FROM items x
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = x.park_id
LEFT JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = x.shed_id
LEFT JOIN workforce_members m
  ON m.tenant_id = $1::uuid AND m.user_id = x.operator_id AND m.status = 'active'
WHERE ($7::text[] IS NULL OR x.board_state = ANY($7::text[]))
ORDER BY x.item_id
LIMIT $8`

// projection-review: membership=verification_items rows of ONE tenant and park whose captured_at falls in the half-open IST business day [day start, next day start), one row per item (primary key); group_key=(tenant_id, item_id) (the count query groups by the SAME derived board_state over the SAME membership); join_cardinality=locations park/shed on their primary key (1:1) and workforce_members filtered to status='active' on the partial-unique (tenant_id,user_id) index (at most 1), so no join fans an item out; pagination=keyset on item_id ASC after the cursor with LIMIT, state filter inside WHERE; scope=tenant_id, park_id, the captured_at day range and the optional operator predicate, repeated verbatim in countSQL.
const countSQL = `
SELECT board_state, count(*)
FROM (
  SELECT ` + workStateSQL + ` AS board_state
  FROM verification_items v
  WHERE ` + baseWhere + `
) x
WHERE ($6::text[] IS NULL OR board_state = ANY($6::text[]))
GROUP BY board_state`

// dayBounds turns the ISO business date into the [start, next start) captured_at window in
// the operational calendar.
func dayBounds(businessDate string) (time.Time, time.Time, error) {
	start, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(businessDate), biztime.DefaultLocation())
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("verification boardsource: %w", domain.ErrInvalidQuery)
	}
	return start, start.AddDate(0, 0, 1), nil
}

// ListRows implements ports.Source.
func (s *Source) ListRows(ctx context.Context, q ports.SourceQuery) ([]domain.Row, error) {
	start, end, err := dayBounds(q.BusinessDate)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	limit := q.Limit
	if limit <= 0 {
		limit = domain.DefaultLimit
	}
	rows, err := s.pool.Query(ctx, listSQL,
		q.TenantID, start, end, q.ParkID, nullUUID(q.OwnerUserID), nullUUID(q.AfterSourceID), statesArg(q.WorkStates), limit)
	if err != nil {
		return nil, fmt.Errorf("verification boardsource list: %w", err)
	}
	defer rows.Close()
	out := make([]domain.Row, 0, limit)
	for rows.Next() {
		r, err := s.scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("verification boardsource list rows: %w", err)
	}
	return out, nil
}

// CountByState implements ports.Source.
func (s *Source) CountByState(ctx context.Context, q ports.SourceQuery) (map[domain.WorkState]int, error) {
	start, end, err := dayBounds(q.BusinessDate)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.pool.Query(ctx, countSQL, q.TenantID, start, end, q.ParkID, nullUUID(q.OwnerUserID), statesArg(q.WorkStates))
	if err != nil {
		return nil, fmt.Errorf("verification boardsource count: %w", err)
	}
	defer rows.Close()
	out := map[domain.WorkState]int{}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, fmt.Errorf("verification boardsource count scan: %w", err)
		}
		out[domain.WorkState(state)] = n
	}
	return out, rows.Err()
}

func (s *Source) scanRow(rows pgx.Rows) (domain.Row, error) {
	var (
		itemID, category, parkID, parkName, shedID, shedName, partitionLabel string
		capturedAt                                                           time.Time
		status, boardState, subjectLabel                                     string
		ownerUserID, ownerMemberID, ownerName                                string
	)
	if err := rows.Scan(&itemID, &category, &parkID, &parkName, &shedID, &shedName, &partitionLabel,
		&capturedAt, &status, &boardState, &subjectLabel,
		&ownerUserID, &ownerMemberID, &ownerName); err != nil {
		return domain.Row{}, fmt.Errorf("verification boardsource scan: %w", err)
	}
	loc := oploc.OperationalLocation{ParkID: parkID, ParkName: parkName, ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel}
	pen := domain.Pen{ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel, Display: loc.Display()}

	state := domain.WorkState(boardState)
	counts := domain.Counts{}
	switch state {
	case domain.WorkStateCompleted:
		counts.Done = 1
	case domain.WorkStateRejected:
		counts.NeedsAttention = 1
	default:
		counts.Pending = 1
	}
	owner := domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName}
	ownerState := domain.OwnerStateAssigned
	if ownerUserID == "" {
		ownerState = domain.OwnerStateMissing
	}
	title := rowTitle(s.categoryLabel(category), subjectLabel, pen.Display)
	captured := capturedAt
	return domain.Row{
		Module: domain.ModuleVerification, SourceType: SourceType, SourceID: itemID,
		ParkID: parkID, ParkName: parkName, Pen: pen,
		BusinessDate: biztime.BusinessDate(capturedAt), DueAt: &captured,
		ClockLabel: "Captured " + biztime.FarmDate(capturedAt),
		WorkState:  state, Severity: domain.SeverityOK,
		Owner: owner, OwnerState: ownerState,
		Title: title, Subtitle: "Proof review", Counts: counts,
	}.Finalize(), nil
}

// categoryLabels reads the human label of every registered category from the catalog. The
// page label is the wording the verifier already sees on that category's tab; where the tab
// label does not name its module ("Adults" under Health, "Shifting" under Counts) the module
// label is prefixed so the board row stands on its own.
func categoryLabels() map[string]string {
	out := map[string]string{}
	for _, def := range verificationcatalog.All() {
		label := strings.TrimSpace(def.PageLabel)
		module := strings.TrimSpace(def.NavigationModuleLabel)
		if label == "" {
			label = humanize(def.Category)
		}
		if module != "" && !strings.Contains(strings.ToLower(label), strings.ToLower(module)) {
			label = module + " " + label
		}
		out[def.Category] = label
	}
	return out
}

func (s *Source) categoryLabel(category string) string {
	if label, ok := s.labels[category]; ok {
		return label
	}
	return humanize(category)
}

// humanize is the last resort for a category the catalog does not know: a config token is
// never shown raw (the vaccine-label rule), so it is at least spaced and capitalised.
func humanize(token string) string {
	words := strings.Fields(strings.ReplaceAll(strings.TrimSpace(token), "_", " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

func nullUUID(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func statesArg(states []domain.WorkState) []string {
	if len(states) == 0 {
		return nil
	}
	out := make([]string, 0, len(states))
	for _, s := range states {
		out = append(out, string(s))
	}
	return out
}

// rowTitle is "<category> · <subject> · <pen>" with two repairs the raw labels need on a
// card. A subject label often restates its own category ("Hoof Trimming · Sumathi 2" under
// "Preventive Care Hoof Trimming"), so a leading segment the category label already ends
// with is dropped. And a subject that names no pen ("Session 1" for a feed distribution)
// gets the item's pen appended, so two pens' cards are never word-for-word the same.
func rowTitle(category, subjectLabel, penDisplay string) string {
	title := category
	subject := strings.TrimSpace(subjectLabel)
	if subject != "" {
		parts := strings.Split(subject, " · ")
		if len(parts) > 1 && strings.HasSuffix(strings.ToLower(category), strings.ToLower(strings.TrimSpace(parts[0]))) {
			parts = parts[1:]
		}
		subject = strings.Join(parts, " · ")
		title += " · " + subject
	}
	if penDisplay != "" && !strings.Contains(strings.ToLower(title), strings.ToLower(penDisplay)) {
		title += " · " + penDisplay
	}
	return title
}
