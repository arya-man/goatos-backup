package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
)

// The /health-config/* persistence: reads for the Health Config screen and the four authored
// writes behind it. Canonical contract: docs/decisions/health-config-authoring.md.
//
// THE INVARIANT EVERYTHING HERE PROTECTS
// ---------------------------------------------------------------------------
// A published protocol version is IMMUTABLE. Goats are being treated from it, `health_cases` pins
// the version each was diagnosed under, and `health_session_steps` holds that case's own copy of
// the steps. So no statement in this file ever updates a published version's content or its steps;
// an edit builds a draft, and publishing SWAPS which version is live. A goat mid-treatment finishes
// on the dosages it started on, and the version it was actually treated from stays readable.
//
// WHY THE WRITES TAKE A LOCK BEFORE READING
// ---------------------------------------------------------------------------
// Each write locks the disease's existing version rows FOR UPDATE before deciding what to do.
// Without it, two concurrent publishes of the same disease both read "published version is v3",
// both retire v3 and both insert a published v4 -- and only the unique index
// health_protocol_versions_one_published_uq stops the second, as a raw constraint violation the
// caller cannot interpret. The lock turns that into an ordinary serialization: the second
// transaction waits, re-reads, and either publishes v5 or reports that the draft is gone.

const (
	// sourceRefAuthored marks a version written by the web rather than by the sheet importer. The
	// importer's lock reads this to answer "has the app taken authorship of this tenant's
	// protocols" -- see ReplacePublishedProtocols.
	sourceRefAuthored = "health-config:app"

	// maxCatalogPageSize bounds one catalog page. The catalog is 54 rows today and is authored
	// config, not herd data, but the bound is what keeps it a page rather than a table dump.
	maxCatalogPageSize     = 50
	defaultCatalogPageSize = 20
)

var _ ports.ProtocolAuthoring = (*Repository)(nil)

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// projection-review: membership=health_protocol_versions restricted to status IN ('published','draft'), unique on (tenant_id, disease_key, age_band, version) with at most ONE row per (tenant, disease, age band) in EACH of those two statuses, enforced by health_protocol_versions_one_published_uq and health_protocol_versions_one_draft_uq; group_key=(disease_key, age_band) -- the FULL OUTER JOIN below matches published to draft on exactly that key pair, and because each side is unique on it the join is strictly 1:1 and cannot fan a disease into several rows; join_cardinality=step_counts is PRE-AGGREGATED to one row per health_protocol_version_id before it is joined, so a 28-step protocol stays one catalog row rather than 28, and the counts are therefore counts of steps per version rather than of joined rows; pagination=keyset on (display_name, disease_key, age_band) with LIMIT n+1 and no total, so page size changes rows only and never the meaning of a count; scope=tenant_id on every arm of the join plus the caller's age_band/search/draft_only filters applied identically to both arms

func (r *Repository) ListProtocolCatalog(ctx context.Context, q domain.ProtocolCatalogQuery) (domain.ProtocolCatalogPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	limit := q.Limit
	if limit <= 0 {
		limit = defaultCatalogPageSize
	}
	if limit > maxCatalogPageSize {
		limit = maxCatalogPageSize
	}
	curName, curDisease, curBand, err := decodeCatalogCursor(q.Cursor)
	if err != nil {
		return domain.ProtocolCatalogPage{}, ports.ErrConflict
	}
	search := strings.TrimSpace(q.Search)

	// scale-guard:ignore: authored-config catalog, one keyset page capped at 50 over 54 tenant rows, served by health_protocol_versions_catalog_idx and its draft twin; step_counts is pre-aggregated per version before the join
	rows, err := r.pool.Query(ctx, `
WITH step_counts AS (
  SELECT s.health_protocol_version_id AS vid,
         count(*)::int AS step_count,
         count(*) FILTER (WHERE s.record_type='medication')::int AS medication_count,
         count(*) FILTER (WHERE s.record_type='critical_action')::int AS critical_count
  FROM health_protocol_steps s
  JOIN health_protocol_versions v
    ON v.tenant_id=s.tenant_id AND v.health_protocol_version_id=s.health_protocol_version_id
  WHERE s.tenant_id=$1::uuid AND v.status IN ('published','draft')
  GROUP BY s.health_protocol_version_id
),
pub AS (
  SELECT v.health_protocol_version_id, v.disease_key, v.display_name, v.age_band, v.version,
         v.duration_days, v.published_at, coalesce(v.source_ref,'') AS source_ref,
         coalesce(c.step_count,0) AS step_count,
         coalesce(c.medication_count,0) AS medication_count,
         coalesce(c.critical_count,0) AS critical_count
  FROM health_protocol_versions v
  LEFT JOIN step_counts c ON c.vid=v.health_protocol_version_id
  WHERE v.tenant_id=$1::uuid AND v.status='published'
),
drf AS (
  SELECT v.health_protocol_version_id, v.disease_key, v.display_name, v.age_band, v.version,
         v.duration_days, v.updated_at,
         coalesce(c.step_count,0) AS step_count,
         coalesce(c.medication_count,0) AS medication_count
  FROM health_protocol_versions v
  LEFT JOIN step_counts c ON c.vid=v.health_protocol_version_id
  WHERE v.tenant_id=$1::uuid AND v.status='draft'
),
merged AS (
  SELECT
    coalesce(pub.disease_key, drf.disease_key)   AS disease_key,
    coalesce(drf.display_name, pub.display_name) AS display_name,
    coalesce(pub.age_band, drf.age_band)         AS age_band,
    coalesce(pub.health_protocol_version_id::text,'') AS published_version_id,
    coalesce(pub.version,0)          AS published_version,
    coalesce(pub.duration_days,0)    AS duration_days,
    coalesce(pub.step_count,0)       AS step_count,
    coalesce(pub.medication_count,0) AS medication_count,
    coalesce(pub.critical_count,0)   AS critical_count,
    pub.published_at,
    coalesce(pub.source_ref,'')      AS source_ref,
    (drf.health_protocol_version_id IS NOT NULL) AS has_draft,
    coalesce(drf.health_protocol_version_id::text,'') AS draft_version_id,
    coalesce(drf.version,0)          AS draft_version,
    coalesce(drf.duration_days,0)    AS draft_duration_days,
    coalesce(drf.step_count,0)       AS draft_step_count,
    coalesce(drf.medication_count,0) AS draft_medication_count,
    drf.updated_at                   AS draft_updated_at
  FROM pub
  FULL OUTER JOIN drf
    ON drf.disease_key=pub.disease_key AND drf.age_band=pub.age_band
)
SELECT disease_key, display_name, age_band, published_version_id, published_version, duration_days,
       step_count, medication_count, critical_count, published_at, source_ref,
       has_draft, draft_version_id, draft_version, draft_duration_days, draft_step_count,
       draft_medication_count, draft_updated_at
FROM merged
WHERE ($2='' OR age_band=$2)
  AND ($3='' OR display_name ILIKE '%' || $3 || '%')
  AND ($4::bool IS NOT TRUE OR has_draft)
  AND ($5='' OR (display_name, disease_key, age_band) > ($5, $6, $7))
ORDER BY display_name, disease_key, age_band
LIMIT $8`,
		q.TenantID, q.AgeBand, search, q.DraftOnly, curName, curDisease, curBand, limit+1)
	if err != nil {
		return domain.ProtocolCatalogPage{}, fmt.Errorf("health: list protocol catalog: %w", err)
	}
	defer rows.Close()

	page := domain.ProtocolCatalogPage{Items: make([]domain.ProtocolCatalogItem, 0, limit)}
	for rows.Next() {
		var it domain.ProtocolCatalogItem
		if err := rows.Scan(&it.DiseaseKey, &it.DisplayName, &it.AgeBand, &it.PublishedVersionID,
			&it.PublishedVersion, &it.DurationDays, &it.StepCount, &it.MedicationCount,
			&it.CriticalActionCount, &it.PublishedAt, &it.SourceRef, &it.HasDraft,
			&it.DraftVersionID, &it.DraftVersion, &it.DraftDurationDays, &it.DraftStepCount,
			&it.DraftMedicationCount, &it.DraftUpdatedAt); err != nil {
			return domain.ProtocolCatalogPage{}, err
		}
		page.Items = append(page.Items, it)
	}
	if err := rows.Err(); err != nil {
		return domain.ProtocolCatalogPage{}, err
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		page.Items = page.Items[:limit]
		cursor := encodeCatalogCursor(last.DisplayName, last.DiseaseKey, last.AgeBand)
		page.NextCursor = &cursor
	}
	return page, nil
}

func (r *Repository) GetProtocolDetail(ctx context.Context, tenantID, protocolVersionID string) (domain.ProtocolDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return domain.ProtocolDetail{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return loadProtocolDetail(ctx, tx, tenantID, protocolVersionID)
}

// projection-review: membership=ONE health_protocol_versions row addressed by its unique key (tenant_id, health_protocol_version_id), so the outer query returns exactly one row by construction; group_key=none -- there is no GROUP BY, and the two counts are CORRELATED SCALAR SUBQUERIES rather than joins, which is what keeps each a count of its own rows instead of a count of a joined product; join_cardinality=the open-case count ranges over health_cases matching (tenant_id, health_protocol_version_id) and restricted to the OPEN statuses active/continued/held_death_review because a recovered or dead animal is not being treated on this version, while the step count ranges over health_protocol_steps matching health_protocol_version_id, and neither subquery is joined to the outer row so neither can multiply it; pagination=none for the single addressed row, and the disease history read below is separately bounded at 20 versions ordered by version DESC; scope=tenant_id on the outer row, on the open-case count, and on the history query

func loadProtocolDetail(ctx context.Context, tx pgx.Tx, tenantID, protocolVersionID string) (domain.ProtocolDetail, error) {
	var d domain.ProtocolDetail
	err := tx.QueryRow(ctx, `
SELECT v.health_protocol_version_id::text, v.disease_key, v.display_name, v.age_band, v.version,
       v.status, v.duration_days, coalesce(v.source_ref,''), v.content_hash,
       v.published_at, v.created_at, v.updated_at,
       (SELECT count(*) FROM health_cases hc
         WHERE hc.tenant_id=v.tenant_id AND hc.health_protocol_version_id=v.health_protocol_version_id
           AND hc.status IN ('active','continued','held_death_review'))::int
FROM health_protocol_versions v
WHERE v.tenant_id=$1::uuid AND v.health_protocol_version_id=$2::uuid`,
		tenantID, protocolVersionID).Scan(&d.ProtocolVersionID, &d.DiseaseKey, &d.DisplayName,
		&d.AgeBand, &d.Version, &d.Status, &d.DurationDays, &d.SourceRef, &d.ContentHash,
		&d.PublishedAt, &d.CreatedAt, &d.UpdatedAt, &d.OpenCaseCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProtocolDetail{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.ProtocolDetail{}, fmt.Errorf("health: load protocol detail: %w", err)
	}

	steps, err := loadStepsForVersion(ctx, tx, protocolVersionID)
	if err != nil {
		return domain.ProtocolDetail{}, err
	}
	d.Steps = steps

	histRows, err := tx.Query(ctx, `
SELECT v.health_protocol_version_id::text, v.version, v.status, v.duration_days,
       coalesce(v.source_ref,''), v.published_at, v.created_at,
       (SELECT count(*) FROM health_protocol_steps s
         WHERE s.health_protocol_version_id=v.health_protocol_version_id)::int
FROM health_protocol_versions v
WHERE v.tenant_id=$1::uuid AND v.disease_key=$2 AND v.age_band=$3
ORDER BY v.version DESC
LIMIT 20`, tenantID, d.DiseaseKey, d.AgeBand)
	if err != nil {
		return domain.ProtocolDetail{}, fmt.Errorf("health: load protocol history: %w", err)
	}
	defer histRows.Close()
	for histRows.Next() {
		var h domain.ProtocolVersionSummary
		if err := histRows.Scan(&h.ProtocolVersionID, &h.Version, &h.Status, &h.DurationDays,
			&h.SourceRef, &h.PublishedAt, &h.CreatedAt, &h.StepCount); err != nil {
			return domain.ProtocolDetail{}, err
		}
		d.History = append(d.History, h)
	}
	return d, histRows.Err()
}

func loadStepsForVersion(ctx context.Context, tx pgx.Tx, protocolVersionID string) ([]domain.ProtocolStep, error) {
	rows, err := tx.Query(ctx, `
SELECT health_protocol_step_id::text, day_no, session, seq, record_type,
       medicine_name, dosage_text, dosage_denominator, medicine_route, instruction, critical_action_type
FROM health_protocol_steps
WHERE health_protocol_version_id=$1::uuid
ORDER BY seq`, protocolVersionID)
	if err != nil {
		return nil, fmt.Errorf("health: load protocol steps: %w", err)
	}
	defer rows.Close()
	out := make([]domain.ProtocolStep, 0, 32)
	for rows.Next() {
		var s domain.ProtocolStep
		if err := rows.Scan(&s.StepID, &s.DayNo, &s.Session, &s.Seq, &s.RecordType, &s.MedicineName,
			&s.DosageText, &s.DosageDenominator, &s.MedicineRoute, &s.Instruction, &s.CriticalActionType); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Writes
// ---------------------------------------------------------------------------

// CreateDisease opens drafts for BOTH age bands of a new disease, in one transaction.
//
// Both bands, because the phone picks the protocol from the GOAT's age band: a disease authored
// for adults only fails at diagnosis for a kid with "protocol not published", which reads as a bug
// rather than as a deliberate gap. The two drafts start identical and are edited apart afterwards
// -- which is exactly the shape of the imported data, where 25 of 27 diseases are identical across
// bands and 2 genuinely differ.
func (r *Repository) CreateDisease(ctx context.Context, cmd domain.CreateDiseaseCommand) (domain.AuthoringResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	return runAuthoringTx(ctx, r, cmd.TenantID, cmd.ActorID, cmd.IdempotencyKey, cmd.RequestFingerprint,
		func(tx pgx.Tx) (domain.AuthoringResult, ledgerEntry, error) {
			// Lock the disease identity across BOTH bands and every status. A disease key is the
			// join identity of every case ever opened under it, so "does this exist" must be
			// answered under a lock.
			//
			// Deliberately EXISTS-shaped rather than count(*): Postgres rejects FOR UPDATE with an
			// aggregate (SQLSTATE 0A000), so a counting form silently cannot take the lock at all.
			//
			// A row lock alone would also not be enough here even if it worked, because there is
			// nothing to lock when the disease does not yet exist — two concurrent creates would
			// both see nothing and both insert. The real serialization is the unique constraint on
			// (tenant, disease_key, age_band, version), which turns the loser's INSERT into the
			// 23505 that insertVersion maps to ErrDiseaseExists. This read is the fast, friendly
			// path for the overwhelmingly common case; the constraint is the guarantee.
			var exists bool
			if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM health_protocol_versions
  WHERE tenant_id=$1::uuid AND disease_key=$2
)`, cmd.TenantID, cmd.DiseaseKey).Scan(&exists); err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, fmt.Errorf("health: check disease: %w", err)
			}
			if exists {
				return domain.AuthoringResult{}, ledgerEntry{}, ports.ErrDiseaseExists
			}

			authored := domain.AuthoredProtocol{
				DisplayName:  cmd.DisplayName,
				DurationDays: cmd.DurationDays,
			}
			hash := domain.ContentHash(authored)
			ids := map[string]string{}
			for _, band := range domain.AgeBands {
				id, err := insertVersion(ctx, tx, cmd.TenantID, cmd.ActorID, versionInsert{
					DiseaseKey:   cmd.DiseaseKey,
					DisplayName:  cmd.DisplayName,
					AgeBand:      band,
					Version:      1,
					DurationDays: cmd.DurationDays,
					Status:       domain.ProtocolStatusDraft,
					ContentHash:  hash,
				})
				if err != nil {
					return domain.AuthoringResult{}, ledgerEntry{}, err
				}
				ids[band] = id
			}

			if err := recordAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "health.protocol.disease_created",
				ids[domain.AgeBandAdult], map[string]any{
					"disease_key": cmd.DiseaseKey, "display_name": cmd.DisplayName,
					"duration_days": cmd.DurationDays, "draft_version_ids": ids,
				}); err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, err
			}

			res := domain.AuthoringResult{
				Outcome:         domain.OutcomeCreated,
				DiseaseKey:      cmd.DiseaseKey,
				DraftVersionIDs: ids,
				Version:         1,
			}
			return res, ledgerEntry{
				WriteKind:  domain.WriteKindDiseaseCreate,
				Outcome:    domain.OutcomeCreated,
				DiseaseKey: cmd.DiseaseKey,
			}, nil
		})
}

// GetDraftForEdit returns the open draft, creating it as a copy of the published version if none
// is open. It is a WRITE despite the name, and it is the one authoring call with no idempotency
// key: opening an editor is naturally idempotent because at most one draft can exist
// (health_protocol_versions_one_draft_uq), so a repeated call returns the same draft rather than
// creating a second one.
func (r *Repository) GetDraftForEdit(ctx context.Context, cmd domain.ProtocolVersionCommand, diseaseKey, ageBand string) (domain.ProtocolDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.ProtocolDetail{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	src, err := lockProtocolIdentity(ctx, tx, cmd.TenantID, diseaseKey, ageBand)
	if err != nil {
		return domain.ProtocolDetail{}, err
	}
	if src.draftID != "" {
		detail, err := loadProtocolDetail(ctx, tx, cmd.TenantID, src.draftID)
		if err != nil {
			return domain.ProtocolDetail{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.ProtocolDetail{}, err
		}
		committed = true
		return detail, nil
	}
	if src.publishedID == "" {
		return domain.ProtocolDetail{}, ports.ErrNotFound
	}

	published, err := loadProtocolDetail(ctx, tx, cmd.TenantID, src.publishedID)
	if err != nil {
		return domain.ProtocolDetail{}, err
	}
	authored := domain.AuthoredProtocol{
		DisplayName:  published.DisplayName,
		DurationDays: published.DurationDays,
		Steps:        domain.FromProtocolSteps(published.Steps),
	}
	draftID, err := insertVersion(ctx, tx, cmd.TenantID, cmd.ActorID, versionInsert{
		DiseaseKey:   published.DiseaseKey,
		DisplayName:  published.DisplayName,
		AgeBand:      published.AgeBand,
		Version:      src.nextVersion,
		DurationDays: published.DurationDays,
		Status:       domain.ProtocolStatusDraft,
		ContentHash:  domain.ContentHash(authored),
	})
	if err != nil {
		return domain.ProtocolDetail{}, err
	}
	if err := replaceSteps(ctx, tx, cmd.TenantID, draftID, domain.ToProtocolSteps(authored.Steps)); err != nil {
		return domain.ProtocolDetail{}, err
	}
	detail, err := loadProtocolDetail(ctx, tx, cmd.TenantID, draftID)
	if err != nil {
		return domain.ProtocolDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ProtocolDetail{}, err
	}
	committed = true
	return detail, nil
}

// SaveDraft replaces a draft's whole content. Identical content is 'unchanged' and writes nothing.
func (r *Repository) SaveDraft(ctx context.Context, cmd domain.SaveDraftCommand) (domain.AuthoringResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	return runAuthoringTx(ctx, r, cmd.TenantID, cmd.ActorID, cmd.IdempotencyKey, cmd.RequestFingerprint,
		func(tx pgx.Tx) (domain.AuthoringResult, ledgerEntry, error) {
			src, err := lockProtocolIdentity(ctx, tx, cmd.TenantID, cmd.DiseaseKey, cmd.AgeBand)
			if err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, err
			}
			if src.draftID == "" {
				return domain.AuthoringResult{}, ledgerEntry{}, ports.ErrNotFound
			}

			hash := domain.ContentHash(cmd.Protocol)
			var currentHash, currentName string
			var currentDuration int
			if err := tx.QueryRow(ctx, `
SELECT content_hash, display_name, duration_days FROM health_protocol_versions
WHERE tenant_id=$1::uuid AND health_protocol_version_id=$2::uuid`,
				cmd.TenantID, src.draftID).Scan(&currentHash, &currentName, &currentDuration); err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, fmt.Errorf("health: read draft: %w", err)
			}

			base := ledgerEntry{
				WriteKind:       domain.WriteKindDraftSave,
				DiseaseKey:      cmd.DiseaseKey,
				AgeBand:         cmd.AgeBand,
				ResultVersionID: src.draftID,
			}
			if currentHash == hash {
				// Nothing changed. The ledger still records the write so the key is consumed and a
				// replay is answerable, but no step row is touched -- rewriting an identical step
				// list would churn created_at timestamps and make the audit trail lie about when
				// the content last changed.
				base.Outcome = domain.OutcomeUnchanged
				return domain.AuthoringResult{
					Outcome:           domain.OutcomeUnchanged,
					DiseaseKey:        cmd.DiseaseKey,
					AgeBand:           cmd.AgeBand,
					ProtocolVersionID: src.draftID,
					Version:           src.draftVersion,
				}, base, nil
			}

			if _, err := tx.Exec(ctx, `
UPDATE health_protocol_versions
SET display_name=$3, duration_days=$4, content_hash=$5, source_ref=$6, updated_by=$7::uuid, updated_at=now()
WHERE tenant_id=$1::uuid AND health_protocol_version_id=$2::uuid AND status='draft'`,
				cmd.TenantID, src.draftID, cmd.Protocol.DisplayName, cmd.Protocol.DurationDays,
				hash, sourceRefAuthored, cmd.ActorID); err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, fmt.Errorf("health: save draft: %w", err)
			}
			// A rename applies to the DISEASE, not to one band: the two bands are the same illness,
			// and letting them drift apart would show an operator "Foot rot" on an adult and
			// "Footrot" on a kid. Only the display name travels; duration and steps stay per band.
			if cmd.Protocol.DisplayName != currentName {
				if _, err := tx.Exec(ctx, `
UPDATE health_protocol_versions SET display_name=$3, updated_at=now()
WHERE tenant_id=$1::uuid AND disease_key=$2 AND status IN ('draft','published')`,
					cmd.TenantID, cmd.DiseaseKey, cmd.Protocol.DisplayName); err != nil {
					return domain.AuthoringResult{}, ledgerEntry{}, fmt.Errorf("health: propagate rename: %w", err)
				}
			}
			if err := replaceSteps(ctx, tx, cmd.TenantID, src.draftID, domain.ToProtocolSteps(cmd.Protocol.Steps)); err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, err
			}
			if err := recordAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "health.protocol.draft_saved", src.draftID,
				map[string]any{
					"disease_key": cmd.DiseaseKey, "age_band": cmd.AgeBand,
					"duration_days": cmd.Protocol.DurationDays, "step_count": len(cmd.Protocol.Steps),
					"content_hash": hash, "previous_content_hash": currentHash,
				}); err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, err
			}

			base.Outcome = domain.OutcomeSaved
			return domain.AuthoringResult{
				Outcome:           domain.OutcomeSaved,
				DiseaseKey:        cmd.DiseaseKey,
				AgeBand:           cmd.AgeBand,
				ProtocolVersionID: src.draftID,
				Version:           src.draftVersion,
			}, base, nil
		})
}

// PublishDraft promotes the draft and retires the version it replaces, atomically.
func (r *Repository) PublishDraft(ctx context.Context, cmd domain.ProtocolVersionCommand) (domain.AuthoringResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	return runAuthoringTx(ctx, r, cmd.TenantID, cmd.ActorID, cmd.IdempotencyKey, cmd.RequestFingerprint,
		func(tx pgx.Tx) (domain.AuthoringResult, ledgerEntry, error) {
			diseaseKey, ageBand, status, version, err := lockVersionRow(ctx, tx, cmd.TenantID, cmd.ProtocolVersionID)
			if err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, err
			}
			if status != domain.ProtocolStatusDraft {
				return domain.AuthoringResult{}, ledgerEntry{}, ports.ErrNotADraft
			}
			src, err := lockProtocolIdentity(ctx, tx, cmd.TenantID, diseaseKey, ageBand)
			if err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, err
			}

			// Publish-time validation runs against what is actually STORED, not against what the
			// last save was told. A draft can be saved incomplete on purpose; this is the gate.
			detail, err := loadProtocolDetail(ctx, tx, cmd.TenantID, cmd.ProtocolVersionID)
			if err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, err
			}
			authored := domain.AuthoredProtocol{
				DisplayName:  detail.DisplayName,
				DurationDays: detail.DurationDays,
				Steps:        domain.FromProtocolSteps(detail.Steps),
			}
			if err := domain.ValidateAuthoredProtocol(authored, true); err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, err
			}

			retired := ""
			if src.publishedID != "" {
				if _, err := tx.Exec(ctx, `
UPDATE health_protocol_versions SET status='retired', updated_at=now()
WHERE tenant_id=$1::uuid AND health_protocol_version_id=$2::uuid AND status='published'`,
					cmd.TenantID, src.publishedID); err != nil {
					return domain.AuthoringResult{}, ledgerEntry{}, fmt.Errorf("health: retire version: %w", err)
				}
				retired = src.publishedID
			}
			if _, err := tx.Exec(ctx, `
UPDATE health_protocol_versions
SET status='published', published_at=now(), published_by=$3::uuid, updated_by=$3::uuid, updated_at=now()
WHERE tenant_id=$1::uuid AND health_protocol_version_id=$2::uuid AND status='draft'`,
				cmd.TenantID, cmd.ProtocolVersionID, cmd.ActorID); err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, fmt.Errorf("health: publish draft: %w", err)
			}

			if err := recordAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "health.protocol.published", cmd.ProtocolVersionID,
				map[string]any{
					"disease_key": diseaseKey, "age_band": ageBand, "version": version,
					"duration_days": detail.DurationDays, "step_count": len(detail.Steps),
					"retired_version_id": retired, "source": sourceRefAuthored,
				}); err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, err
			}
			if err := insertProtocolOutbox(ctx, tx, cmd.TenantID, cmd.ActorID, cmd.ProtocolVersionID,
				domain.SourceProtocol{
					DiseaseKey: diseaseKey, DisplayName: detail.DisplayName, AgeBand: ageBand,
					DurationDays: detail.DurationDays, Steps: detail.Steps,
				}, version); err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, err
			}

			return domain.AuthoringResult{
					Outcome:           domain.OutcomePublished,
					DiseaseKey:        diseaseKey,
					AgeBand:           ageBand,
					ProtocolVersionID: cmd.ProtocolVersionID,
					Version:           version,
					RetiredVersionID:  retired,
				}, ledgerEntry{
					WriteKind:        domain.WriteKindDraftPublish,
					Outcome:          domain.OutcomePublished,
					DiseaseKey:       diseaseKey,
					AgeBand:          ageBand,
					ResultVersionID:  cmd.ProtocolVersionID,
					RetiredVersionID: retired,
				}, nil
		})
}

// DiscardDraft deletes a draft and its steps (ON DELETE CASCADE).
func (r *Repository) DiscardDraft(ctx context.Context, cmd domain.ProtocolVersionCommand) (domain.AuthoringResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	return runAuthoringTx(ctx, r, cmd.TenantID, cmd.ActorID, cmd.IdempotencyKey, cmd.RequestFingerprint,
		func(tx pgx.Tx) (domain.AuthoringResult, ledgerEntry, error) {
			diseaseKey, ageBand, status, _, err := lockVersionRow(ctx, tx, cmd.TenantID, cmd.ProtocolVersionID)
			if err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, err
			}
			if status != domain.ProtocolStatusDraft {
				return domain.AuthoringResult{}, ledgerEntry{}, ports.ErrNotADraft
			}
			// A draft is never referenced by a case (only published versions can be diagnosed
			// under), so this is belt-and-braces against a future path that pins a draft. The FK
			// would refuse the delete anyway; this turns that into an interpretable error.
			var cases int
			if err := tx.QueryRow(ctx, `
SELECT count(*)::int FROM health_cases
WHERE tenant_id=$1::uuid AND health_protocol_version_id=$2::uuid`,
				cmd.TenantID, cmd.ProtocolVersionID).Scan(&cases); err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, err
			}
			if cases > 0 {
				return domain.AuthoringResult{}, ledgerEntry{}, ports.ErrProtocolInUse
			}
			if _, err := tx.Exec(ctx, `
DELETE FROM health_protocol_versions
WHERE tenant_id=$1::uuid AND health_protocol_version_id=$2::uuid AND status='draft'`,
				cmd.TenantID, cmd.ProtocolVersionID); err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, fmt.Errorf("health: discard draft: %w", err)
			}
			if err := recordAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "health.protocol.draft_discarded",
				cmd.ProtocolVersionID, map[string]any{"disease_key": diseaseKey, "age_band": ageBand}); err != nil {
				return domain.AuthoringResult{}, ledgerEntry{}, err
			}
			return domain.AuthoringResult{
					Outcome:    domain.OutcomeDiscarded,
					DiseaseKey: diseaseKey,
					AgeBand:    ageBand,
				}, ledgerEntry{
					WriteKind:  domain.WriteKindDraftDiscard,
					Outcome:    domain.OutcomeDiscarded,
					DiseaseKey: diseaseKey,
					AgeBand:    ageBand,
				}, nil
		})
}

// ---------------------------------------------------------------------------
// Shared write machinery
// ---------------------------------------------------------------------------

type ledgerEntry struct {
	WriteKind        string
	Outcome          string
	DiseaseKey       string
	AgeBand          string
	ResultVersionID  string
	RetiredVersionID string
}

// runAuthoringTx applies the idempotency contract uniformly to all four writes.
//
//	exact replay (same key, same fingerprint)      -> the ledger's stored result, no side effects
//	same key, DIFFERENT fingerprint                -> ErrConflict
//	new key                                        -> body runs, ledger written in the SAME tx
//
// The ledger INSERT is what detects a concurrent duplicate: the second transaction violates
// health_config_write_log_idempotency_uidx, and this function re-reads the committed entry and
// returns its result rather than applying the edit twice.
func runAuthoringTx(
	ctx context.Context,
	r *Repository,
	tenantID, actorID, idempotencyKey, fingerprint string,
	body func(pgx.Tx) (domain.AuthoringResult, ledgerEntry, error),
) (domain.AuthoringResult, error) {
	if strings.TrimSpace(idempotencyKey) == "" || strings.TrimSpace(fingerprint) == "" {
		return domain.AuthoringResult{}, ports.ErrConflict
	}
	if strings.TrimSpace(actorID) == "" {
		// health_config_write_log_actor_check would reject the ledger row anyway; failing here
		// makes it an interpretable error instead of a constraint violation at commit.
		return domain.AuthoringResult{}, ports.ErrConflict
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.AuthoringResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if replay, found, err := readLedger(ctx, tx, tenantID, idempotencyKey, fingerprint); err != nil {
		return domain.AuthoringResult{}, err
	} else if found {
		if err := tx.Commit(ctx); err != nil {
			return domain.AuthoringResult{}, err
		}
		committed = true
		return replay, nil
	}

	res, entry, err := body(tx)
	if err != nil {
		return domain.AuthoringResult{}, err
	}
	if err := writeLedger(ctx, tx, tenantID, actorID, idempotencyKey, fingerprint, entry); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
			strings.Contains(pgErr.ConstraintName, "health_config_write_log_idempotency") {
			// A concurrent request with the same key committed first. Roll back this attempt and
			// return the winner's result, which is what an idempotent replay must do.
			_ = tx.Rollback(ctx)
			committed = true
			return readCommittedLedger(ctx, r, tenantID, idempotencyKey, fingerprint)
		}
		return domain.AuthoringResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AuthoringResult{}, err
	}
	committed = true
	return res, nil
}

func readLedger(ctx context.Context, tx pgx.Tx, tenantID, key, fingerprint string) (domain.AuthoringResult, bool, error) {
	var storedFingerprint, outcome, diseaseKey string
	var ageBand, resultID, retiredID *string
	err := tx.QueryRow(ctx, `
SELECT request_fingerprint, outcome, disease_key, age_band, result_version_id::text, retired_version_id::text
FROM health_config_write_log WHERE tenant_id=$1::uuid AND idempotency_key=$2`,
		tenantID, key).Scan(&storedFingerprint, &outcome, &diseaseKey, &ageBand, &resultID, &retiredID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AuthoringResult{}, false, nil
	}
	if err != nil {
		return domain.AuthoringResult{}, false, fmt.Errorf("health: read authoring ledger: %w", err)
	}
	if storedFingerprint != fingerprint {
		return domain.AuthoringResult{}, false, ports.ErrConflict
	}

	res := domain.AuthoringResult{
		Outcome:           outcome,
		DiseaseKey:        diseaseKey,
		AgeBand:           strOrEmpty(ageBand),
		ProtocolVersionID: strOrEmpty(resultID),
		RetiredVersionID:  strOrEmpty(retiredID),
		IdempotentReplay:  true,
	}
	if res.ProtocolVersionID != "" {
		// The version number is read from the row rather than stored on the ledger, so a replay
		// cannot report a stale number if that row was later published under a different one.
		_ = tx.QueryRow(ctx, `SELECT version FROM health_protocol_versions
WHERE tenant_id=$1::uuid AND health_protocol_version_id=$2::uuid`, tenantID, res.ProtocolVersionID).Scan(&res.Version)
	}
	if outcome == domain.OutcomeCreated {
		// A create addresses TWO version rows, so the ledger cannot name "the" result row. The
		// replay re-derives both drafts from the disease key, which is stable and unique.
		ids, err := draftIDsForDisease(ctx, tx, tenantID, diseaseKey)
		if err != nil {
			return domain.AuthoringResult{}, false, err
		}
		res.DraftVersionIDs = ids
		res.Version = 1
	}
	return res, true, nil
}

// draftIDsForDisease maps age band -> open draft id for one disease. Used only to rebuild a
// create's response on replay; an empty map is legitimate (both drafts may since have been
// published or discarded), and reporting that honestly is better than failing the replay.
func draftIDsForDisease(ctx context.Context, tx pgx.Tx, tenantID, diseaseKey string) (map[string]string, error) {
	rows, err := tx.Query(ctx, `
SELECT age_band, health_protocol_version_id::text FROM health_protocol_versions
WHERE tenant_id=$1::uuid AND disease_key=$2 AND status='draft'`, tenantID, diseaseKey)
	if err != nil {
		return nil, fmt.Errorf("health: read disease drafts: %w", err)
	}
	defer rows.Close()
	ids := map[string]string{}
	for rows.Next() {
		var band, id string
		if err := rows.Scan(&band, &id); err != nil {
			return nil, err
		}
		ids[band] = id
	}
	return ids, rows.Err()
}

func readCommittedLedger(ctx context.Context, r *Repository, tenantID, key, fingerprint string) (domain.AuthoringResult, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return domain.AuthoringResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	res, found, err := readLedger(ctx, tx, tenantID, key, fingerprint)
	if err != nil {
		return domain.AuthoringResult{}, err
	}
	if !found {
		return domain.AuthoringResult{}, ports.ErrConflict
	}
	return res, nil
}

func writeLedger(ctx context.Context, tx pgx.Tx, tenantID, actorID, key, fingerprint string, e ledgerEntry) error {
	_, err := tx.Exec(ctx, `
INSERT INTO health_config_write_log
 (tenant_id, write_kind, idempotency_key, request_fingerprint, outcome, disease_key, age_band,
  result_version_id, retired_version_id, actor_ref)
VALUES ($1::uuid,$2,$3,$4,$5,$6,nullif($7,''),nullif($8,'')::uuid,nullif($9,'')::uuid,$10)`,
		tenantID, e.WriteKind, key, fingerprint, e.Outcome, e.DiseaseKey, e.AgeBand,
		e.ResultVersionID, e.RetiredVersionID, actorID)
	return err
}

type protocolIdentity struct {
	publishedID  string
	draftID      string
	draftVersion int
	nextVersion  int
}

// lockProtocolIdentity takes a row lock over every version of one disease/age band and reports
// which is published, which is draft, and what the next version number is.
//
// FOR UPDATE on the whole identity, not just the row being changed: publish reads the published
// row and writes the draft row, and save reads the draft while a concurrent publish may be
// retiring the published one. Locking the identity is what makes "which version is live" a stable
// answer for the length of the transaction.
func lockProtocolIdentity(ctx context.Context, tx pgx.Tx, tenantID, diseaseKey, ageBand string) (protocolIdentity, error) {
	rows, err := tx.Query(ctx, `
SELECT health_protocol_version_id::text, status, version
FROM health_protocol_versions
WHERE tenant_id=$1::uuid AND disease_key=$2 AND age_band=$3
ORDER BY version
FOR UPDATE`, tenantID, diseaseKey, ageBand)
	if err != nil {
		return protocolIdentity{}, fmt.Errorf("health: lock protocol identity: %w", err)
	}
	defer rows.Close()
	out := protocolIdentity{nextVersion: 1}
	for rows.Next() {
		var id, status string
		var version int
		if err := rows.Scan(&id, &status, &version); err != nil {
			return protocolIdentity{}, err
		}
		switch status {
		case domain.ProtocolStatusPublished:
			out.publishedID = id
		case domain.ProtocolStatusDraft:
			out.draftID = id
			out.draftVersion = version
		}
		if version >= out.nextVersion {
			out.nextVersion = version + 1
		}
	}
	return out, rows.Err()
}

func lockVersionRow(ctx context.Context, tx pgx.Tx, tenantID, versionID string) (diseaseKey, ageBand, status string, version int, err error) {
	err = tx.QueryRow(ctx, `
SELECT disease_key, age_band, status, version FROM health_protocol_versions
WHERE tenant_id=$1::uuid AND health_protocol_version_id=$2::uuid FOR UPDATE`,
		tenantID, versionID).Scan(&diseaseKey, &ageBand, &status, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", 0, ports.ErrNotFound
	}
	if err != nil {
		return "", "", "", 0, fmt.Errorf("health: lock version: %w", err)
	}
	return diseaseKey, ageBand, status, version, nil
}

type versionInsert struct {
	DiseaseKey   string
	DisplayName  string
	AgeBand      string
	Version      int
	DurationDays int
	Status       string
	ContentHash  string
}

func insertVersion(ctx context.Context, tx pgx.Tx, tenantID, actorID string, v versionInsert) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `
INSERT INTO health_protocol_versions
 (tenant_id, disease_key, display_name, age_band, version, duration_days, status, source_ref,
  content_hash, created_by, updated_by)
VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10::uuid,$10::uuid)
RETURNING health_protocol_version_id::text`,
		tenantID, v.DiseaseKey, v.DisplayName, v.AgeBand, v.Version, v.DurationDays, v.Status,
		sourceRefAuthored, v.ContentHash, actorID).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if strings.Contains(pgErr.ConstraintName, "one_draft") {
				return "", ports.ErrDraftExists
			}
			if strings.Contains(pgErr.ConstraintName, "one_published") {
				return "", ports.ErrConflict
			}
			return "", ports.ErrDiseaseExists
		}
		return "", fmt.Errorf("health: insert protocol version: %w", err)
	}
	return id, nil
}

// replaceSteps rewrites a draft's whole ordered step list.
//
// DELETE-then-INSERT rather than a per-step diff, for a schema reason and a correctness reason.
// The schema reason: UNIQUE (health_protocol_version_id, seq) means renumbering in place collides
// with itself halfway through a reorder. The correctness reason: a protocol is read as one
// sequence, so "the steps as the author last saw them" is the only meaningful unit of save --
// a diff that applied 6 of 7 changes would leave a document no author authored.
func replaceSteps(ctx context.Context, tx pgx.Tx, tenantID, versionID string, steps []domain.ProtocolStep) error {
	if _, err := tx.Exec(ctx, `DELETE FROM health_protocol_steps
WHERE tenant_id=$1::uuid AND health_protocol_version_id=$2::uuid`, tenantID, versionID); err != nil {
		return fmt.Errorf("health: clear draft steps: %w", err)
	}
	if len(steps) == 0 {
		return nil
	}
	// One set-based INSERT over unnested arrays, not a loop of Execs: the N+1 anti-pattern is
	// banned in backend/internal/** and a 28-step protocol would otherwise be 28 round trips.
	dayNos := make([]int32, len(steps))
	sessions := make([]string, len(steps))
	seqs := make([]int32, len(steps))
	recordTypes := make([]string, len(steps))
	medicineNames := make([]*string, len(steps))
	dosageTexts := make([]*string, len(steps))
	denominators := make([]*string, len(steps))
	routes := make([]*string, len(steps))
	instructions := make([]*string, len(steps))
	criticals := make([]*string, len(steps))
	for i, s := range steps {
		dayNos[i] = int32(s.DayNo)
		sessions[i] = s.Session
		seqs[i] = int32(s.Seq)
		recordTypes[i] = s.RecordType
		medicineNames[i] = s.MedicineName
		dosageTexts[i] = s.DosageText
		denominators[i] = s.DosageDenominator
		routes[i] = s.MedicineRoute
		instructions[i] = s.Instruction
		criticals[i] = s.CriticalActionType
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO health_protocol_steps
 (tenant_id, health_protocol_version_id, day_no, session, seq, record_type,
  medicine_name, dosage_text, dosage_denominator, medicine_route, instruction, critical_action_type)
SELECT $1::uuid, $2::uuid, d.day_no, d.session, d.seq, d.record_type,
       d.medicine_name, d.dosage_text, d.dosage_denominator, d.medicine_route, d.instruction, d.critical_action_type
FROM unnest($3::int[], $4::text[], $5::int[], $6::text[], $7::text[], $8::text[], $9::text[], $10::text[], $11::text[], $12::text[])
  AS d(day_no, session, seq, record_type, medicine_name, dosage_text, dosage_denominator, medicine_route, instruction, critical_action_type)`,
		tenantID, versionID, dayNos, sessions, seqs, recordTypes, medicineNames, dosageTexts,
		denominators, routes, instructions, criticals); err != nil {
		return fmt.Errorf("health: insert draft steps: %w", err)
	}
	return nil
}

func recordAudit(ctx context.Context, tx pgx.Tx, tenantID, actorID, action, resourceID string, after map[string]any) error {
	return audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "admin",
		Action:       action,
		ResourceType: "health_protocol_version",
		ResourceID:   resourceID,
		AfterState:   after,
	})
}

// ---------------------------------------------------------------------------
// Cursor
// ---------------------------------------------------------------------------

// The catalog cursor carries the full sort key (display_name, disease_key, age_band). All three
// are needed: display_name repeats across the two age bands of one disease, and two diseases can
// legitimately share a name once a rename is in flight.
func encodeCatalogCursor(displayName, diseaseKey, ageBand string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Quote(displayName) + "\x1f" + diseaseKey + "\x1f" + ageBand))
}

func decodeCatalogCursor(cursor string) (displayName, diseaseKey, ageBand string, err error) {
	if strings.TrimSpace(cursor) == "" {
		return "", "", "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", "", err
	}
	parts := strings.Split(string(raw), "\x1f")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("health: malformed catalog cursor")
	}
	name, err := strconv.Unquote(parts[0])
	if err != nil {
		return "", "", "", err
	}
	return name, parts[1], parts[2], nil
}

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
