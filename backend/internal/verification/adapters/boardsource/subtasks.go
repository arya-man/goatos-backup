package boardsource

import (
	"context"
	"fmt"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// Subtasks of a verification item, for the board's issue view: one per PROOF the item
// carries (its media_refs, in order), each with the one step a proof has here -- review --
// in the item's own status. An item that carries no media reference at all still drills
// into itself as one proof, so a live row never shows an empty list. Reads
// verification_items plus proof_artifacts (proof plumbing, allowed) for the media kind and
// workforce_members for the operator's name.
//
// READ-ONLY and REPORTING-ONLY: nothing here samples, relabels or casts a verdict.

// verificationSubtaskRankSQL is the SQL twin of domain.RankFor, stated once. Every proof of
// an item shares the item's status, so the rank is the item's.
//
//	rejected -> 0 needs attention
//	approved -> 4 done
//	pending  -> 3 in review
const verificationSubtaskRankSQL = `CASE
  WHEN v.status = 'rejected' THEN 0
  WHEN v.status = 'approved' THEN 4
  ELSE 3
END`

// projection-review: membership=the ONE verification_items row named by (tenant_id, park_id, item_id) whose captured_at falls in the requested IST business day with withdrawn excluded, then one unit per element of its media_refs array (WITH ORDINALITY) or one synthetic unit when the array is empty; group_key=(tenant_id, item_id, ordinal) so one proof reference is one subtask; join_cardinality=jsonb_array_elements_text over ONE row's array is a bounded unnest that cannot repeat a proof, proof_artifacts is joined on its primary key only when the reference parses as a uuid (at most 1), and workforce_members filtered to status='active' on the partial-unique (tenant_id,user_id) index (at most 1), so nothing fans a proof out and the window total counts each proof once; pagination=keyset on (rank, ordinal key) ASC after ($6, $7) with LIMIT $8 and the whole count carried by count(*) OVER () computed before the keyset cut; scope=tenant_id, park_id, the captured_at day range and item_id, the same predicate the row read binds on verification_items.
const verificationSubtasksSQL = `
WITH item AS (
  SELECT v.item_id, v.status, COALESCE(v.verdict_reason, '') AS verdict_reason,
         COALESCE(v.subject_label, '') AS subject_label, v.operator_id,
         CASE WHEN jsonb_typeof(v.media_refs) = 'array' AND jsonb_array_length(v.media_refs) > 0
              THEN v.media_refs ELSE '[""]'::jsonb END AS refs,
         ` + verificationSubtaskRankSQL + ` AS rank
  FROM verification_items v
  WHERE v.tenant_id = $1::uuid
    AND v.captured_at >= $2::timestamptz
    AND v.captured_at < $3::timestamptz
    AND v.park_id = $4::uuid
    AND v.item_id = $5::uuid
    AND ` + notOnBoardSQL + `
    AND v.status <> 'withdrawn'
),
units AS (
  SELECT i.item_id, i.status, i.verdict_reason, i.subject_label, i.operator_id, i.rank,
         r.ordinal::int AS ordinal, r.ref,
         lpad(r.ordinal::text, 3, '0') AS unit_id,
         count(*) OVER () AS total
  FROM item i
  CROSS JOIN LATERAL jsonb_array_elements_text(i.refs) WITH ORDINALITY AS r(ref, ordinal)
)
SELECT u.unit_id, u.ordinal, u.status, u.verdict_reason, u.subject_label, u.rank, u.total,
       COALESCE(pa.proof_type, ''), COALESCE(pa.mime_type, ''),
       COALESCE(u.operator_id::text, ''), COALESCE(m.workforce_member_id::text, ''), COALESCE(m.display_name, '')
FROM units u
LEFT JOIN proof_artifacts pa
  ON pa.tenant_id = $1::uuid
 AND u.ref ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'
 AND pa.proof_id = CASE WHEN u.ref ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$' THEN u.ref::uuid END
LEFT JOIN workforce_members m
  ON m.tenant_id = $1::uuid AND m.user_id = u.operator_id AND m.status = 'active'
WHERE (u.rank, u.unit_id) > ($6::int, $7::text)
ORDER BY u.rank, u.unit_id
LIMIT $8`

// ListSubtasks implements ports.SubtaskSource.
func (s *Source) ListSubtasks(ctx context.Context, q ports.SubtaskQuery) (domain.SubtaskPage, error) {
	start, end, err := dayBounds(q.BusinessDate)
	if err != nil {
		return domain.SubtaskPage{}, err
	}
	afterRank, afterID, err := domain.ParseSubtaskKey(q.AfterKey)
	if err != nil {
		return domain.SubtaskPage{}, err
	}
	if q.AfterKey == "" {
		afterRank = -1
	}
	limit := domain.BoundSubtaskLimit(q.Limit)
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	bound := sqlbind.MustBind(verificationSubtasksSQL, q.TenantID, start, end, q.ParkID, q.SourceID, afterRank, afterID, limit+1)
	rows, err := s.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("verification boardsource subtasks: %w", err)
	}
	defer rows.Close()
	page := domain.SubtaskPage{Subtasks: []domain.Subtask{}}
	for rows.Next() {
		st, total, err := scanVerificationSubtask(rows)
		if err != nil {
			return domain.SubtaskPage{}, err
		}
		page.Total = total
		if len(page.Subtasks) == limit {
			page.NextCursor = page.Subtasks[limit-1].Key
			break
		}
		page.Subtasks = append(page.Subtasks, st)
	}
	if err := rows.Err(); err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("verification boardsource subtasks rows: %w", err)
	}
	return page, nil
}

func scanVerificationSubtask(rows pgx.Rows) (domain.Subtask, int, error) {
	var (
		unitID, status, reason, subject       string
		ordinal, rank, total                  int
		proofType, mimeType                   string
		ownerUserID, ownerMemberID, ownerName string
	)
	if err := rows.Scan(&unitID, &ordinal, &status, &reason, &subject, &rank, &total, &proofType, &mimeType,
		&ownerUserID, &ownerMemberID, &ownerName); err != nil {
		return domain.Subtask{}, 0, fmt.Errorf("verification boardsource subtask scan: %w", err)
	}
	name := proofKindLabel(proofType, mimeType)
	if total > 1 {
		name += " " + strconv.Itoa(ordinal)
	}
	review := domain.Step{Name: "Review", State: domain.StepInReview}
	state := domain.WorkStateVerificationPending
	attention := false
	switch status {
	case "approved":
		review.State = domain.StepDone
		state = domain.WorkStateCompleted
	case "rejected":
		review.State, review.Detail = domain.StepRework, reason
		state, attention = domain.WorkStateRejected, true
	}
	return domain.Subtask{
		Key: domain.SubtaskKey(rank, unitID), Name: name, Subtitle: subject,
		WorkState: state, NeedsAttention: attention,
		Owner: domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName},
		Steps: []domain.Step{review},
	}.Finalize(), total, nil
}

// proofKindLabel names a proof by what it is: a video, a photo, or -- when the artifact is
// not resolvable -- just a proof. Never the reference string, which is an id.
func proofKindLabel(proofType, mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "video/"), strings.Contains(strings.ToLower(proofType), "video"):
		return "Video"
	case strings.HasPrefix(mimeType, "image/"), strings.Contains(strings.ToLower(proofType), "photo"), strings.Contains(strings.ToLower(proofType), "image"):
		return "Photo"
	case strings.HasPrefix(mimeType, "audio/"), strings.Contains(strings.ToLower(proofType), "audio"):
		return "Audio"
	}
	return "Proof"
}
