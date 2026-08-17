package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

// videoLogDayWindow cuts the requested business day ONCE, as a half-open timestamptz range.
//
// It must never become `(vi.captured_at AT TIME ZONE 'Asia/Kolkata')::date = $2::date`. That is a
// function of the column, so no index can drive it and verification_items -- an append-only table
// growing with animals x modules x years -- is read in full for a one-day answer. This is the same
// rule the vaccination live tracker had to learn (migration 000158); the index that pairs with the
// shape below is verification_items_video_log_day_idx (migration 000165).
const videoLogDayWindow = `
day_window AS (
  SELECT ($2::date::timestamp AT TIME ZONE 'Asia/Kolkata') AS day_start,
         (($2::date + 1)::timestamp AT TIME ZONE 'Asia/Kolkata') AS day_end
)`

// videoLogUUIDPattern guards the media_refs -> proof_id cast.
//
// media_refs is a jsonb array of STRINGS with no foreign key to proof_artifacts, so a malformed or
// legacy ref is structurally possible. `ref.proof_id::uuid` on such a value raises
// `invalid input syntax for type uuid` and takes the whole panel down with a 500. Filtering to
// well-formed uuids drops the bad ref and keeps the rest of the shed's day readable.
//
// The cast stays on the REF side, never the column side: `pa.proof_id::text = ref.proof_id` would
// put a cast on the indexed column and give up the primary key (docs/decisions/scale-anti-patterns.md
// -> "column-side type cast in a predicate").
const videoLogUUIDPattern = `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`

// VideoLogShedSummary returns one row per OPERATIONAL LOCATION (shed + partition) that had proof
// arrive on the requested business day.
//
// projection-review: membership=proof_artifacts rows reachable from a media_ref of a
// verification_item whose captured_at falls in ONE Asia/Kolkata day for ONE tenant, park-clamped to
// the caller's authorized parks; group_key=(vi.shed_id, normalized partition);
// join_cardinality=jsonb_array_elements_text fans an item out to N refs BY DESIGN (that is the
// proof grain the count wants) and each ref then matches proof_artifacts on its PRIMARY KEY plus
// tenant so 1:0..1, meaning no proof can be counted twice, while the two locations joins are each
// 1:0..1 on (tenant_id, location_id); pagination=none, the result is one row per shed for one day;
// scope=explicit tenant_id on BOTH verification_items and proof_artifacts, plus the park clamp.
//
// GRAIN PROOF (producer vs consumer, mandatory per AGENTS.md):
//
//	producer unique key = proof_artifacts (proof_id) PRIMARY KEY, narrowed by tenant_id.
//	consumer match key  = ref.proof_id::uuid, one element of ONE item's media_refs array.
//	row multiplicity    = exactly one output row per (shed_id, normalized partition); the fan-out
//	                      to refs is the intended proof grain, and proof_count counts those refs
//	                      while item_count counts DISTINCT item_id over the same set -- so the two
//	                      numbers range over the SAME rows and item_count can never exceed
//	                      proof_count.
//	awaiting vs total   = awaiting_upload_count is a FILTER over that same set, never a second
//	                      query, so it cannot disagree with proof_count about what the day held.
//
// A shed whose refs ALL point at missing artifacts yields no row, which is correct: nothing
// arrived. An item with shed_id NULL is excluded -- it has no operational location to file under,
// and the milk categories are the only producers that leave it unset.
func (r *Repository) VideoLogShedSummary(ctx context.Context, params ports.VideoLogParams) ([]domain.VideoLogShed, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, `
WITH `+videoLogDayWindow+`,
items AS (
  SELECT vi.item_id, vi.shed_id, vi.partition_label, vi.park_id, vi.module, vi.media_refs
  FROM verification_items vi, day_window w
  WHERE vi.tenant_id = $1::uuid
    AND vi.captured_at >= w.day_start
    AND vi.captured_at <  w.day_end
    -- Every status EXCEPT withdrawn. A rejected or approved proof arrived just as much as a
    -- pending one, and the whole question is what arrived; only a withdrawn item (its source work
    -- was superseded) never really stood.
    AND vi.status <> 'withdrawn'
    AND vi.shed_id IS NOT NULL
    AND (NOT $3::boolean OR vi.park_id = ANY($4::uuid[]))
    AND ($5 = '' OR vi.park_id = $5::uuid)
),
refs AS (
  SELECT i.item_id, i.shed_id, i.partition_label, i.park_id, i.module, ref.proof_id AS proof_ref
  FROM items i
  JOIN LATERAL jsonb_array_elements_text(i.media_refs) AS ref(proof_id) ON true
  WHERE ref.proof_id ~ $6
),
arrivals AS (
  SELECT r.item_id, r.shed_id, r.partition_label, r.park_id, r.module, pa.uploaded_at
  FROM refs r
  JOIN proof_artifacts pa
    ON pa.tenant_id = $1::uuid
   AND pa.proof_id = r.proof_ref::uuid
)
SELECT a.shed_id::text,
       COALESCE(shed_loc.name, ''),
       -- The HUMAN label for the pen, chosen deterministically from the group. See the NORMALIZED
       -- grouping note below for why a group can hold more than one spelling of one pen.
       COALESCE(NULLIF(btrim(min(a.partition_label)), ''), ''),
       COALESCE(a.park_id::text, ''),
       COALESCE(park_loc.name, ''),
       count(*)::int,
       count(DISTINCT a.item_id)::int,
       count(*) FILTER (WHERE a.uploaded_at IS NULL)::int,
       min(a.uploaded_at),
       max(a.uploaded_at),
       array_agg(DISTINCT a.module)
FROM arrivals a
LEFT JOIN locations shed_loc ON shed_loc.tenant_id = $1::uuid AND shed_loc.location_id = a.shed_id
LEFT JOIN locations park_loc ON park_loc.tenant_id = $1::uuid AND park_loc.location_id = a.park_id
-- GROUP BY the NORMALIZED partition, never the raw label.
--
-- One pen is written more than one way in real data: this tenant's Castro carries BOTH '1' and
-- 'Part 1' on the same day, for the same pen. Grouping on the raw label split that pen into two
-- rows whose oploc.Key() -- which normalizes -- was IDENTICAL, so the summary listed the same pen
-- twice and React saw duplicate keys. Grouping on the raw label also separates NULL from '', which
-- are both "no partition".
--
-- The expression must stay byte-identical to shedPartitionPredicate (and therefore to
-- oploc.NormalizePartition), or a pen this groups is a pen the detail filter cannot find.
GROUP BY a.shed_id, shed_loc.name,
         regexp_replace(lower(COALESCE(NULLIF(btrim(a.partition_label), ''), 'whole')), '^part[[:space:]]+', ''),
         a.park_id, park_loc.name
ORDER BY COALESCE(park_loc.name, ''), COALESCE(shed_loc.name, ''),
         regexp_replace(lower(COALESCE(NULLIF(btrim(a.partition_label), ''), 'whole')), '^part[[:space:]]+', '')`,
		params.TenantID, params.BusinessDate, params.ScopeRestricted, params.ParkIDs, params.ParkID, videoLogUUIDPattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.VideoLogShed
	for rows.Next() {
		var shed domain.VideoLogShed
		var modules []string
		if err := rows.Scan(&shed.ShedID, &shed.ShedLabel, &shed.PartitionLabel, &shed.ParkID, &shed.ParkLabel,
			&shed.ProofCount, &shed.ItemCount, &shed.AwaitingUploadCount,
			&shed.FirstUploadAt, &shed.LastUploadAt, &modules); err != nil {
			return nil, err
		}
		// One composition site, via the canonical helper -- never a hand-rolled join. The sentinel
		// 'whole' can never reach a caller because Display() drops a non-partitioned label.
		shed.OperationalLocationDisplay = oploc.OperationalLocation{
			ShedID:         shed.ShedID,
			ShedName:       shed.ShedLabel,
			PartitionLabel: shed.PartitionLabel,
		}.Display()
		shed.Modules = modules
		out = append(out, shed)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// VideoLogShedRows returns ONE operational location's work for the day, with every proof and its
// arrival time.
//
// projection-review: membership=the same day/tenant/park-clamped verification_items as
// VideoLogShedSummary, narrowed to ONE shed and (when the filter carries it) ONE partition;
// group_key=item_id, with proofs carried as an ordered array per item; join_cardinality=refs fans
// to the proof grain and each ref matches proof_artifacts 1:0..1 on its primary key plus tenant,
// while the operator LATERAL is LIMIT 1 so it cannot multiply a row; pagination=bounded by the
// limit bind with an explicit truncation flag, never a silent cap; scope=explicit tenant_id on both
// tables, plus the park clamp and the shed/partition narrowing.
//
// GRAIN PROOF (producer vs consumer, mandatory per AGENTS.md):
//
//	producer unique key = verification_items (tenant_id, item_id) UNIQUE for the row grain;
//	                      proof_artifacts (proof_id) PRIMARY KEY for the proof grain.
//	consumer match key  = item_id for the row, ref.proof_id::uuid for each proof under it.
//	row multiplicity    = one row per item; the proofs array holds exactly the item's own
//	                      media_refs that resolved, in the producer's declared order.
//
// WITH ORDINALITY is load-bearing, not decoration: media_refs order is semantic. Feed distribution
// writes [weight photo, distribution video, water video] and the registry's MediaLabels are
// POSITIONAL against that order, so losing the ordinal mislabels every proof on the item.
func (r *Repository) VideoLogShedRows(ctx context.Context, params ports.VideoLogParams) ([]domain.VideoLogRow, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	shedID, partitionKey := splitShedFilter(params.ShedID)
	if params.AllSheds {
		// The whole-day export drops BOTH shed predicates rather than passing a wildcard: an empty
		// shedID would still be bound as ''::uuid below and match nothing.
		shedID, partitionKey = "", ""
	}
	// Read one more than asked for: if the extra row comes back the caller's list is incomplete and
	// says so, rather than showing a truncated day that looks like the whole day.
	probeLimit := params.Limit + 1

	rows, err := r.pool.Query(ctx, `
WITH `+videoLogDayWindow+`,
items AS (
  SELECT vi.item_id, vi.module, vi.category, vi.source_ref_type, vi.subject_label, vi.status,
         vi.operator_id, vi.captured_at, vi.media_refs,
         vi.shed_id, vi.partition_label, vi.park_id
  FROM verification_items vi, day_window w
  WHERE vi.tenant_id = $1::uuid
    AND vi.captured_at >= w.day_start
    AND vi.captured_at <  w.day_end
    AND vi.status <> 'withdrawn'
    AND vi.shed_id IS NOT NULL
    -- Both shed predicates are no-ops for the whole-day export, which passes '' for each.
    AND ($6 = '' OR vi.shed_id = $6::uuid)
    AND ($7 = '' OR `+shedPartitionPredicate+` = $7)
    AND (NOT $3::boolean OR vi.park_id = ANY($4::uuid[]))
    AND ($5 = '' OR vi.park_id = $5::uuid)
),
valid_refs AS (
  SELECT i.item_id, ref.proof_id, ref.ord
  FROM items i
  JOIN LATERAL jsonb_array_elements_text(i.media_refs) WITH ORDINALITY AS ref(proof_id, ord) ON true
  WHERE ref.proof_id ~ $9
),
proofs AS (
  SELECT i.item_id,
         ref.ord,
         pa.proof_id::text AS proof_id,
         COALESCE(NULLIF(btrim(pa.metadata->>'verification_label'), ''), '') AS proof_label,
         pa.proof_type,
         pa.uploaded_at,
         pa.created_at
  FROM items i
  JOIN valid_refs ref ON ref.item_id = i.item_id
  JOIN proof_artifacts pa
    ON pa.tenant_id = $1::uuid
   AND pa.proof_id = ref.proof_id::uuid
)
SELECT i.item_id::text,
       i.module,
       i.category,
       i.source_ref_type,
       COALESCE(i.subject_label, ''),
       i.status,
       COALESCE(operator.display_name, ''),
       i.captured_at,
       i.shed_id::text,
       COALESCE(shed_loc.name, ''),
       COALESCE(NULLIF(btrim(i.partition_label), ''), ''),
       COALESCE(park_loc.name, ''),
       COALESCE(
         jsonb_agg(
           jsonb_build_object(
             'proof_id', p.proof_id,
             'label', p.proof_label,
             'media_kind', p.proof_type,
             'uploaded_at', p.uploaded_at,
             'registered_at', p.created_at,
             'ord', p.ord
           )
           ORDER BY p.ord
         ) FILTER (WHERE p.proof_id IS NOT NULL),
         '[]'::jsonb
       )
FROM items i
LEFT JOIN proofs p ON p.item_id = i.item_id
LEFT JOIN LATERAL (
  SELECT wm.display_name
  FROM workforce_members wm
  WHERE wm.tenant_id = $1::uuid
    AND (wm.workforce_member_id = i.operator_id OR wm.user_id = i.operator_id)
  ORDER BY
    (wm.workforce_member_id = i.operator_id) DESC,
    (wm.status = 'active') DESC,
    wm.updated_at DESC,
    wm.workforce_member_id
  LIMIT 1
) operator ON true
LEFT JOIN locations shed_loc ON shed_loc.tenant_id = $1::uuid AND shed_loc.location_id = i.shed_id
LEFT JOIN locations park_loc ON park_loc.tenant_id = $1::uuid AND park_loc.location_id = i.park_id
GROUP BY i.item_id, i.module, i.category, i.source_ref_type, i.subject_label, i.status,
         operator.display_name, i.captured_at, i.shed_id, shed_loc.name, i.partition_label,
         park_loc.name
-- Shed first, then earliest arrival: one shed's day stays contiguous in the whole-day export, and
-- within it the day reads as it happened. For a single-shed read the leading key is constant, so
-- the ordering is identical to arrival order and nothing changes on screen.
--
-- An item whose proofs have not landed sorts last (NULLS LAST) rather than jumping to the top of
-- the morning.
ORDER BY COALESCE(park_loc.name, ''), COALESCE(shed_loc.name, ''),
         regexp_replace(lower(COALESCE(NULLIF(btrim(i.partition_label), ''), 'whole')), '^part[[:space:]]+', ''),
         min(p.uploaded_at) NULLS LAST, i.captured_at, i.item_id
LIMIT $8`,
		params.TenantID, params.BusinessDate, params.ScopeRestricted, params.ParkIDs, params.ParkID,
		shedID, partitionKey, probeLimit, videoLogUUIDPattern)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var out []domain.VideoLogRow
	for rows.Next() {
		var row domain.VideoLogRow
		var refType string
		var proofsJSON []byte
		if err := rows.Scan(&row.ItemID, &row.Module, &row.Category, &refType, &row.SubjectLabel,
			&row.Status, &row.OperatorName, &row.CapturedAt,
			&row.ShedID, &row.ShedLabel, &row.PartitionLabel, &row.ParkLabel, &proofsJSON); err != nil {
			return nil, false, err
		}
		row.Grain = videoLogGrainFor(refType)
		proofs, err := decodeVideoLogProofs(proofsJSON)
		if err != nil {
			return nil, false, err
		}
		row.Proofs = proofs
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(out) > params.Limit {
		return out[:params.Limit], true, nil
	}
	return out, false, nil
}

// videoLogGrainFor maps a producer's declared source_ref_type to the row grain.
//
// It keys on source_ref_type -- the producing module's OWN statement of what its ref_id points at
// -- and never on the label text. Parsing "12 animals" out of a composed subject label would break
// the moment a producer rewords it, and the copy firewall means those words are free to change.
//
// The animal set is exactly the ref types whose work is about specific animals:
//
//	vaccination_goat        one goat's dose clip (ref_id IS the goat_id)
//	animal                  weighing's individual observation
//	workflow_birth_signoff  one dam/kid birth workflow
//	workflow_death_signoff  one goat's death workflow
//	shifting                a shed move; the animals are named in the label as a count
//
// Everything else -- feed, packing, transport, weighing's lump-sum shed observation, vaccination's
// group clip -- is location work. An UNKNOWN ref type falls to shed grain deliberately: a new
// producer that has not been classified here shows up under its shed rather than claiming to be
// about an animal it cannot name.
func videoLogGrainFor(sourceRefType string) domain.VideoLogGrain {
	switch sourceRefType {
	case "vaccination_goat", "animal", "workflow_birth_signoff", "workflow_death_signoff", "shifting":
		return domain.VideoLogGrainAnimal
	default:
		return domain.VideoLogGrainShed
	}
}

// videoLogProofRow is the jsonb shape aggregated per item above.
type videoLogProofRow struct {
	ProofID      string     `json:"proof_id"`
	Label        string     `json:"label"`
	MediaKind    string     `json:"media_kind"`
	UploadedAt   *time.Time `json:"uploaded_at"`
	RegisteredAt time.Time  `json:"registered_at"`
	Ord          int        `json:"ord"`
}

// decodeVideoLogProofs turns the per-item jsonb aggregate into domain proofs, preserving the
// producer's declared ordinal so the service can resolve each proof's registry label positionally.
//
// The aggregate is always a JSON array (the query COALESCEs to '[]'), so an item whose refs all
// failed to resolve decodes to an empty slice rather than nil-vs-empty ambiguity downstream.
func decodeVideoLogProofs(raw []byte) ([]domain.VideoLogProof, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var decoded []videoLogProofRow
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("verification: unmarshal video log proofs: %w", err)
	}
	if len(decoded) == 0 {
		return nil, nil
	}
	out := make([]domain.VideoLogProof, 0, len(decoded))
	for _, p := range decoded {
		out = append(out, domain.VideoLogProof{
			ProofID:      p.ProofID,
			Ordinal:      p.Ord,
			Label:        p.Label,
			MediaKind:    p.MediaKind,
			UploadedAt:   p.UploadedAt,
			RegisteredAt: p.RegisteredAt,
		})
	}
	return out, nil
}
