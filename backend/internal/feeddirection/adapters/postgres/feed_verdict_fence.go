package postgres

import "fmt"

// THE VERDICT FENCE. A feed verdict applies only to the submission its verification item judged.
//
// A re-delivered or late verdict for an EARLIER item must not move the crew's next submission: the
// rework verdict of a bag's first video, delivered again after the re-shoot, bounced the new,
// never-judged video to rework (E2E 2026-09-17), and a late approve of a first video (the afternoon
// correction reopens the bag, the crew re-shoots, the approve lands) would complete a bag nobody
// looked at. Two checks, both required when the event carries them:
//
//   - evidence: the row still holds the capture the verdict judged (source.evidence_id);
//   - round: no NEWER verification item exists for the same completion (payload item_id). This is
//     the check that holds when the resubmit names the SAME captures again -- which deploy-day
//     parity still allows -- because each submission round queues its own item
//     (the enqueue key carries the completion's row_version).
//
// The feed adapter already writes verification_items for its own completions (the afternoon
// reopen withdraws them); this is the read twin, scoped to feed's own source rows.

// verdictEvidenceHeldSQL is true when no evidence id was given or the completion row holds it.
func verdictEvidenceHeldSQL(param int) string {
	return fmt.Sprintf("($%[1]d::text = '' OR EXISTS (SELECT 1 FROM jsonb_each_text(sop_proofs) e WHERE e.value = $%[1]d::text))", param)
}

// verdictItemCurrentSQL is true when no item id was given or no newer item exists for this
// completion (table.completion_id) under the given source ref type.
func verdictItemCurrentSQL(param int, table, refType string) string {
	return fmt.Sprintf(`(nullif($%[1]d::text, '') IS NULL OR NOT EXISTS (
    SELECT 1 FROM verification_items judged
    JOIN verification_items newer ON newer.tenant_id = judged.tenant_id
      AND newer.source_module = judged.source_module AND newer.source_ref_type = judged.source_ref_type
      AND newer.source_ref_id = judged.source_ref_id AND newer.created_at > judged.created_at
    WHERE judged.tenant_id = %[2]s.tenant_id AND judged.item_id = nullif($%[1]d::text, '')::uuid
      AND judged.source_module = 'feed' AND judged.source_ref_type = '%[3]s'
      AND judged.source_ref_id = %[2]s.completion_id))`, param, table, refType)
}
