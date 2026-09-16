package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// ONE CLIP PROVES ONE PIECE OF FEED WORK, and a clip a verifier sent back proves nothing.
//
// Two locks had no executable check on the write path until 2026-09-17 (E2E on the QA clone):
//   - "one clip cannot prove two bags" (AGENTS.md feed packing grain) -- pen A's captures submitted
//     for pen B, or a packing video submitted as a distribution video, were both accepted and each
//     queued its own verifier item;
//   - "every rework requires a new video" (feed-transport-verification.md; the same re-shoot rule for
//     packing, distribution and wastage) -- a resubmit carrying the very capture the verifier had
//     rejected was accepted and queued to the verifier again.
//
// Both are refused here, inside the write transaction, naming the slot, so the phone points at the
// capture to redo. The refusal is the 422 feed_proof_slot_invalid every other slot problem uses.

const (
	proofSentBackMessage   = "This capture was sent back. Record a new one."
	proofUsedElsewhereText = "This capture already proves other feed work. Record a new one here."
)

// reuseWindowDays bounds the cross-work lookup to the feed days a capture could plausibly be
// re-filed under: a transport trip on business day D-1 serves feed day D, and a sheet's packing,
// distribution and wastage all carry feed day D.
const reuseWindowDays = 2

func slotError(slot, message string) error {
	return fmt.Errorf("%w: %w", ports.ErrSOPProofSlotInvalid, &authored.ProofError{SlotKey: slot, Message: message})
}

// sortedSlots is the refs' slot keys in a stable order, so the slot a refusal names is deterministic.
func sortedSlots(refs authored.ProofRefs) []string {
	keys := make([]string, 0, len(refs))
	for k := range refs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// refuseSentBackProofs refuses a rework resubmit that carries any capture of the set the work was
// sent back with (a verifier rejection or the afternoon correction's reopen).
func refuseSentBackProofs(refs, sentBack authored.ProofRefs) error {
	spent := map[string]bool{}
	for _, v := range sentBack {
		if v = strings.TrimSpace(v); v != "" {
			spent[v] = true
		}
	}
	for _, slot := range sortedSlots(refs) {
		if spent[strings.TrimSpace(refs[slot])] {
			return slotError(slot, proofSentBackMessage)
		}
	}
	return nil
}

// refuseProofsUsedElsewhere refuses a capture that already proves ANOTHER piece of feed work of the
// same park within the feed-day window -- another pen-session's distribution, another bag, another
// pen's wastage, or another shed's transport trip. workID is the completion (or transport task) this
// write belongs to; its own rows are never "elsewhere".
func refuseProofsUsedElsewhere(ctx context.Context, tx pgx.Tx, tenantID, parkID, day, workID string, refs authored.ProofRefs) error {
	values := make([]string, 0, len(refs))
	for _, v := range refs {
		if v = strings.TrimSpace(v); v != "" {
			values = append(values, v)
		}
	}
	if len(values) == 0 {
		return nil
	}
	// scale-guard:ignore: write-path guard over ONE park's feed work within a +/-2 feed-day window, each branch on its serving index (tenant_id, park_id, target_date) or, for transport, (tenant_id, business_date) then the task's attempts by (tenant_id, task_id); bounded by the park's pen catalog x sessions x 5 days, never by herd size; binds carry the casts and indexed columns stay bare.
	rows, err := tx.Query(ctx, `
SELECT DISTINCT e.value
FROM (
  SELECT c.sop_proofs FROM feed_distribution_completions c
  WHERE c.tenant_id = $1::uuid AND c.park_id = $2::uuid
    AND c.target_date BETWEEN $3::date - $6::int AND $3::date + $6::int AND c.completion_id <> $4::uuid
  UNION ALL
  SELECT c.sop_proofs FROM feed_packing_completions c
  WHERE c.tenant_id = $1::uuid AND c.park_id = $2::uuid
    AND c.target_date BETWEEN $3::date - $6::int AND $3::date + $6::int AND c.completion_id <> $4::uuid
  UNION ALL
  SELECT c.sop_proofs FROM feed_wastage_completions c
  WHERE c.tenant_id = $1::uuid AND c.park_id = $2::uuid
    AND c.target_date BETWEEN $3::date - $6::int AND $3::date + $6::int AND c.completion_id <> $4::uuid
  UNION ALL
  SELECT a.sop_proofs FROM feed_transport_tasks t
  JOIN feed_transport_attempts a ON a.tenant_id = t.tenant_id AND a.task_id = t.task_id
  WHERE t.tenant_id = $1::uuid AND t.park_id = $2::uuid
    AND t.business_date BETWEEN $3::date - $6::int AND $3::date + $6::int AND t.task_id <> $4::uuid
) s
CROSS JOIN LATERAL jsonb_each_text(s.sop_proofs) e
WHERE e.value = ANY($5::text[])`, tenantID, parkID, day, workID, values, reuseWindowDays)
	if err != nil {
		return fmt.Errorf("feeddirection: check feed proof reuse: %w", err)
	}
	defer rows.Close()
	used := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return fmt.Errorf("feeddirection: scan feed proof reuse: %w", err)
		}
		used[v] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("feeddirection: read feed proof reuse: %w", err)
	}
	for _, slot := range sortedSlots(refs) {
		if used[strings.TrimSpace(refs[slot])] {
			return slotError(slot, proofUsedElsewhereText)
		}
	}
	return nil
}

var _ ports.DistributionSentBackProofReader = (*Repository)(nil)

// sentBackDistributionProofsSQL is one natural-key read on feed_distribution_completions_natural_uq.
const sentBackDistributionProofsSQL = `
SELECT sop_proofs FROM feed_distribution_completions
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shed_id = $3::uuid AND partition_key = $4
  AND session_no = $5 AND target_date = $6::date AND workflow = $7 AND status = 'rework'`

// SentBackDistributionProofs returns the stored captures of ONE pen-session while it is in 'rework'
// -- the set a verifier rejected (or nothing, for any other status). One indexed natural-key read.
func (r *Repository) SentBackDistributionProofs(ctx context.Context, q ports.PenSessionCaptureQuery) (authored.ProofRefs, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var refs authored.ProofRefs
	err := r.pool.QueryRow(ctx, sentBackDistributionProofsSQL,
		q.TenantID, q.ParkID, q.ShedID, partitionColumnKey(q.PartitionLabel), q.SessionNo, q.TargetDate.Format("2006-01-02"), q.Workflow).Scan(&refs)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("feeddirection: read sent-back distribution proofs: %w", err)
	}
	return refs, nil
}
