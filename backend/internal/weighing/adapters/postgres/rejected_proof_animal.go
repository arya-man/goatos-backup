package postgres

import "context"

// AnimalProofWasRejected reports whether this proof is already attached to an observation a
// verifier sent back for rework in this same bucket.
//
// The lump-sum write has carried a rejected_proof_guard CTE for a while; the individual-animal
// write had no equivalent, so re-recording an animal with the EXACT video the verifier had just
// rejected was accepted with a 200 -- and, because the tag already held a row, changed nothing at
// all. The operator saw success, the animal stayed in rework, and the shed still could not be
// submitted. Re-capturing means a NEW video; reusing the rejected one is the operator not
// actually redoing the work.
// proofRefs covers EVERY slot of the capture (2026-09-16): a rejected secondary capture
// (a scale photo) re-sent under a fresh video is still the operator not redoing the work. The
// rejected row's captures are its proof_artifact_id plus every value of its sop_proofs map.
func (r *Repository) AnimalProofWasRejected(ctx context.Context, tenantID, campaignShedID string, proofRefs []string, scannedIdentifier string) (bool, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	if len(proofRefs) == 0 {
		return false, nil
	}
	var exists bool
	if err := r.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM weighing_observations rejected
  WHERE rejected.tenant_id=$1::uuid
    AND rejected.campaign_shed_id=$2::uuid
    AND rejected.verification_status='rework'
    AND (
      rejected.proof_artifact_id=ANY($3::uuid[])
      OR EXISTS (
        SELECT 1 FROM jsonb_each_text(rejected.sop_proofs) slot
        WHERE slot.value = ANY($5::text[])
      )
    )
    AND lower(btrim(rejected.scanned_identifier))<>lower(btrim($4))
)`, tenantID, campaignShedID, proofRefs, scannedIdentifier, proofRefs).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}
