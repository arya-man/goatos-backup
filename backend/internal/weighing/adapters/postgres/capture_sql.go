package postgres

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// THE WEIGH CAPTURES ARE AUTHORED (maintainer decision 2026-09-16): the store's slot plumbing.
// Weighing stays ISOLATED -- everything here reads weighing_* tables and proof_artifacts only.

// animalBundle is the per-animal write's slot arrays, bound positionally into the CTE chain.
type animalBundle struct {
	ids         []string
	kinds       []string
	proofsJSON  []byte
	answersJSON []byte
}

func animalProofBundle(cmd domain.RecordAnimalObservation) animalBundle {
	refs := cmd.NormalizedProofs
	if len(refs) == 0 {
		// A caller that never ran the service's judgement (an older test path): the legacy
		// single video on the seeded slot.
		refs = domain.LegacyIndividualRefs(domain.SeededRules(), cmd.ProofArtifactID)
	}
	ordered := cmd.OrderedRefs
	if len(ordered) == 0 {
		ordered = orderedRefsOf(refs, cmd.ProofArtifactID)
	}
	keyByRef := map[string]string{}
	for key, ref := range refs {
		keyByRef[ref] = key
	}
	kinds := cmd.SlotKinds
	if kinds == nil {
		kinds = domain.SeededRules().IndividualSlotKinds()
	}
	b := animalBundle{ids: ordered, kinds: make([]string, 0, len(ordered))}
	for _, ref := range ordered {
		kind := kinds[keyByRef[ref]]
		if kind == "" {
			kind = domain.RemovalProofKindVideo
		}
		b.kinds = append(b.kinds, kind)
	}
	b.proofsJSON = mustJSON(refs)
	b.answersJSON = mustJSON(cmd.NormalizedAnswers)
	return b
}

// orderedRefsOf lists a slot map's refs deterministically, primary first.
func orderedRefsOf(refs domain.IndividualProofRefs, primary string) []string {
	keys := make([]string, 0, len(refs))
	for k := range refs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []string{}
	if primary != "" {
		out = append(out, primary)
	}
	for _, k := range keys {
		if refs[k] != primary {
			out = append(out, refs[k])
		}
	}
	return out
}

// shedBundle is the whole-pen write's slot arrays, positional against cmd.ProofArtifactIDs.
type shedBundle struct {
	slotKeys    []string
	kinds       []string
	slots       domain.LumpSumProofRefs
	answersJSON []byte
}

func shedProofBundle(cmd domain.RecordShedObservation) shedBundle {
	ordered := cmd.Ordered
	if len(ordered) == 0 {
		ordered = domain.LegacyLumpSumOrdered(domain.LegacyLumpSumRefs(domain.SeededRules(), cmd.ProofArtifactIDs))
	}
	kinds := cmd.SlotKinds
	if kinds == nil {
		kinds = domain.SeededRules().LumpSumSlotKinds()
	}
	keyByRef := map[string]string{}
	for _, c := range ordered {
		keyByRef[c.Ref] = c.SlotKey
	}
	b := shedBundle{slots: domain.LumpSumProofRefs{}}
	for _, ref := range cmd.ProofArtifactIDs {
		key := keyByRef[ref]
		kind := kinds[key]
		if kind == "" {
			kind = domain.RemovalProofKindVideo
		}
		b.slotKeys = append(b.slotKeys, key)
		b.kinds = append(b.kinds, kind)
		if key != "" {
			b.slots[key] = append(b.slots[key], ref)
		}
	}
	b.answersJSON = mustJSON(cmd.NormalizedAnswers)
	return b
}

func mustJSON(v any) []byte {
	switch x := v.(type) {
	case domain.IndividualProofRefs:
		if x == nil {
			return []byte("{}")
		}
	case domain.SOPAnswers:
		if x == nil {
			return []byte("{}")
		}
	}
	raw, err := json.Marshal(v)
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return []byte("{}")
	}
	return raw
}

// proofKindsTx reads {ref: video | photo} from the register for the row's captures (at most
// ten), so every path -- write, replay, read -- names the same kind for an `either` slot.
func (r *Repository) proofKindsTx(ctx context.Context, tx pgx.Tx, tenantID string, refs []string) (map[string]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `SELECT proof_id::text, proof_type FROM proof_artifacts WHERE tenant_id=$1::uuid AND proof_id=ANY($2::uuid[])`, tenantID, refs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, kind string
		if err := rows.Scan(&id, &kind); err != nil {
			return nil, err
		}
		out[id] = kind
	}
	return out, rows.Err()
}

// decodeObservationSOP fills the row's slot map and answers from their jsonb text.
func decodeObservationSOP(obs *domain.Observation, proofsText, answersText string) {
	if proofsText != "" && proofsText != "{}" {
		var m domain.IndividualProofRefs
		if json.Unmarshal([]byte(proofsText), &m) == nil && len(m) > 0 {
			obs.Proofs = m
		}
	}
	if answersText != "" && answersText != "{}" {
		var a domain.SOPAnswers
		if json.Unmarshal([]byte(answersText), &a) == nil && len(a) > 0 {
			obs.Answers = a
		}
	}
}

func decodeKinds(text string) map[string]string {
	if text == "" || text == "{}" {
		return nil
	}
	var m map[string]string
	if json.Unmarshal([]byte(text), &m) != nil || len(m) == 0 {
		return nil
	}
	return m
}

func decodeSlots(text string) domain.LumpSumProofRefs {
	if text == "" || text == "{}" {
		return nil
	}
	var m domain.LumpSumProofRefs
	if json.Unmarshal([]byte(text), &m) != nil || len(m) == 0 {
		return nil
	}
	return m
}

// animalProofList is every capture of a per-animal row, primary first: the legacy column plus
// the slot map's other refs (a row written before slots existed has only the column).
func animalProofList(obs domain.Observation) []string {
	return orderedRefsOf(obs.Proofs, obs.ProofArtifactID)
}

// SQL fragments shared by the read paths. Each is ONE bounded scalar subquery per row: a
// per-animal row carries at most four captures, a whole-pen row at most ten.
//
// animalProofKindsSQL is the {ref: kind} map for the observation row aliased `o` (repository.go
// inlines the same form for `weighing_observations`, keeping its SQL a compile-time constant for
// the bind-contract check). The refs are the legacy column plus the slot map's values, matched
// with `= ANY(uuid[])` so every ref is a proof_artifacts primary-key probe. The old
// `proof_id = X OR proof_id::text IN (...)` form could not use the key and seq-scanned the register
// per row (leadership sheds: 5.5 s on stg). Slot values that are not lowercase canonical UUIDs
// never matched the old text compare (proof_id::text is always lowercase), so the case-sensitive
// `~` skips exactly those and the output is identical.
const animalProofKindsSQL = `COALESCE((SELECT jsonb_object_agg(pa.proof_id::text, pa.proof_type)
     FROM proof_artifacts pa
     WHERE pa.tenant_id=o.tenant_id
       AND pa.proof_id = ANY(array_append(ARRAY(
             SELECT e.value::uuid FROM jsonb_each_text(o.sop_proofs) e
             WHERE e.value ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'),
           o.proof_artifact_id))), '{}'::jsonb)::text`

const shedProofSlotsSQL = `COALESCE((SELECT jsonb_object_agg(g.slot_key, g.refs)
     FROM (SELECT p.slot_key, jsonb_agg(p.proof_artifact_id::text ORDER BY p.proof_position) AS refs
           FROM weighing_shed_observation_proofs p
           WHERE p.tenant_id=wso.tenant_id AND p.shed_observation_id=wso.shed_observation_id AND p.slot_key IS NOT NULL
           GROUP BY p.slot_key) g), '{}'::jsonb)::text`

const shedProofKindsSQL = `COALESCE((SELECT jsonb_object_agg(pa.proof_id::text, pa.proof_type)
     FROM proof_artifacts pa
     WHERE pa.tenant_id=wso.tenant_id
       AND (pa.proof_id=wso.proof_artifact_id
            OR pa.proof_id IN (SELECT p.proof_artifact_id FROM weighing_shed_observation_proofs p
                               WHERE p.tenant_id=wso.tenant_id AND p.shed_observation_id=wso.shed_observation_id))), '{}'::jsonb)::text`
