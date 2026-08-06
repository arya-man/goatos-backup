package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
)

const (
	maxProtocolsPerImport = 200
	maxStepsPerProtocol   = 2_000
)

// ReplacePublishedProtocols publishes one immutable version of every supplied disease/age
// protocol in one transaction. It is intentionally a deployment/import seam, not a request path.
//
// # IT IS ALSO A BOOTSTRAP, NOT AN ONGOING SYNC
//
// Maintainer decision 2026-08-06 moved AUTHORSHIP of treatment protocols from the Google Sheet to
// `/health/config`. This function retires EVERY published protocol and republishes the supplied
// set, so running it after an author has edited a dosage in the app would silently discard that
// edit -- and, because it publishes a new version rather than mutating one, it would do so without
// any constraint violation to notice.
//
// So it now fails closed once the app has taken authorship: any version carrying the authored
// source ref means the web owns these protocols, and the import returns ErrImportAfterAuthoring.
// `allowOverwriteAuthored` is the deliberate break-glass for a maintainer re-bootstrapping from a
// corrected sheet; it is a parameter rather than an env var so the caller has to say it in code
// that gets reviewed.
//
// The check reads AUTHORED SOURCE REF rather than "any version > 1" because the importer itself
// legitimately produces version 2, 3, ... on repeated pre-authoring imports; the question is not
// how many versions exist but whether a human authored one in the app.
func (r *Repository) ReplacePublishedProtocols(ctx context.Context, tenantID, actorID, sourceRef, contentHash string, protocols []domain.SourceProtocol) error {
	return r.replacePublishedProtocols(ctx, tenantID, actorID, sourceRef, contentHash, protocols, false)
}

// ReplacePublishedProtocolsOverwritingAuthored is the break-glass form. It discards app-authored
// protocol versions in favour of the supplied sheet snapshot. Reserved for a maintainer
// re-bootstrap; never wire it to a request path or a scheduled job.
func (r *Repository) ReplacePublishedProtocolsOverwritingAuthored(ctx context.Context, tenantID, actorID, sourceRef, contentHash string, protocols []domain.SourceProtocol) error {
	return r.replacePublishedProtocols(ctx, tenantID, actorID, sourceRef, contentHash, protocols, true)
}

func (r *Repository) replacePublishedProtocols(ctx context.Context, tenantID, actorID, sourceRef, contentHash string, protocols []domain.SourceProtocol, allowOverwriteAuthored bool) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if len(protocols) == 0 {
		return fmt.Errorf("health: protocol import is empty")
	}
	if len(protocols) > maxProtocolsPerImport {
		return fmt.Errorf("health: protocol import exceeds %d protocols", maxProtocolsPerImport)
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if !allowOverwriteAuthored {
		// One indexed count, inside the same transaction as the import it guards, so an author who
		// publishes while an import is running is still caught rather than raced past.
		var authored int
		if err := tx.QueryRow(ctx, `
SELECT count(*)::int FROM health_protocol_versions
WHERE tenant_id=$1::uuid AND source_ref=$2 AND status IN ('published','draft')`,
			tenantID, sourceRefAuthored).Scan(&authored); err != nil {
			return fmt.Errorf("health: check authored protocols: %w", err)
		}
		if authored > 0 {
			return fmt.Errorf("%w (%d app-authored protocol version(s) present)", ports.ErrImportAfterAuthoring, authored)
		}
	}

	seen := map[string]bool{}
	for _, p := range protocols {
		p.DiseaseKey = strings.TrimSpace(p.DiseaseKey)
		p.DisplayName = strings.TrimSpace(p.DisplayName)
		p.AgeBand = strings.ToLower(strings.TrimSpace(p.AgeBand))
		if p.DurationDays <= 0 {
			p.DurationDays = domain.DefaultDurationDays
		}
		key := p.AgeBand + ":" + p.DiseaseKey
		if p.DiseaseKey == "" || p.DisplayName == "" || (p.AgeBand != domain.AgeBandAdult && p.AgeBand != domain.AgeBandKid) || p.DurationDays > domain.MaxDurationDays || seen[key] {
			return fmt.Errorf("health: invalid or duplicate protocol %q", key)
		}
		seen[key] = true
		if len(p.Steps) > maxStepsPerProtocol {
			return fmt.Errorf("health: %s exceeds %d steps", key, maxStepsPerProtocol)
		}
		for _, s := range p.Steps {
			if s.DayNo < 1 || s.DayNo > p.DurationDays || s.Seq < 1 {
				return fmt.Errorf("health: %s step day/seq out of range", key)
			}
			if normalizeSession(s.Session) != s.Session {
				return fmt.Errorf("health: %s step has unknown session %q", key, s.Session)
			}
			if s.RecordType != "action" && s.RecordType != "medication" && s.RecordType != "critical_action" {
				return fmt.Errorf("health: %s step has unknown record type %q", key, s.RecordType)
			}
			if s.RecordType == "critical_action" && s.CriticalActionType == nil {
				return fmt.Errorf("health: %s critical step has no guarded handoff type", key)
			}
		}
		var version int
		if err := tx.QueryRow(ctx, `SELECT coalesce(max(version),0)+1 FROM health_protocol_versions WHERE tenant_id=$1::uuid AND disease_key=$2 AND age_band=$3`, tenantID, p.DiseaseKey, p.AgeBand).Scan(&version); err != nil { // scale-guard:ignore: deployment-only strict snapshot import, hard-capped at 200 protocols
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE health_protocol_versions SET status='retired',updated_at=now() WHERE tenant_id=$1::uuid AND disease_key=$2 AND age_band=$3 AND status='published'`, tenantID, p.DiseaseKey, p.AgeBand); err != nil { // scale-guard:ignore: deployment-only strict snapshot import, hard-capped at 200 protocols
			return err
		}
		var protocolID string
		err := tx.QueryRow(ctx, `INSERT INTO health_protocol_versions (tenant_id,disease_key,display_name,age_band,version,duration_days,status,source_ref,content_hash,published_at,published_by) -- scale-guard:ignore: deployment-only strict snapshot import, hard-capped at 200 protocols
VALUES ($1::uuid,$2,$3,$4,$5,$6,'published',$7,$8,now(),$9::uuid) RETURNING health_protocol_version_id::text`, tenantID, p.DiseaseKey, p.DisplayName, p.AgeBand, version, p.DurationDays, sourceRef, contentHash, actorID).Scan(&protocolID)
		if err != nil {
			return fmt.Errorf("health: publish %s: %w", key, err)
		}
		for _, s := range p.Steps {
			_, err = tx.Exec(ctx, `INSERT INTO health_protocol_steps (tenant_id,health_protocol_version_id,day_no,session,seq,record_type,medicine_name,dosage_text,dosage_denominator,medicine_route,instruction,critical_action_type) -- scale-guard:ignore: deployment-only strict snapshot import; total rows are fixed by the reviewed source file
VALUES ($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, tenantID, protocolID, s.DayNo, s.Session, s.Seq, s.RecordType, s.MedicineName, s.DosageText, s.DosageDenominator, s.MedicineRoute, s.Instruction, s.CriticalActionType)
			if err != nil {
				return fmt.Errorf("health: insert %s step %d: %w", key, s.Seq, err)
			}
		}
		if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{TenantID: tenantID, ActorID: actorID, ActorType: "admin", Action: "health.protocol.published", ResourceType: "health_protocol_version", ResourceID: protocolID, AfterState: map[string]any{"disease_key": p.DiseaseKey, "age_band": p.AgeBand, "version": version, "duration_days": p.DurationDays, "step_count": len(p.Steps)}, Metadata: map[string]any{"source_ref": sourceRef, "content_hash": contentHash}}); err != nil {
			return err
		}
		if err := insertProtocolOutbox(ctx, tx, tenantID, actorID, protocolID, p, version); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func insertProtocolOutbox(ctx context.Context, tx pgx.Tx, tenantID, actorID, protocolID string, p domain.SourceProtocol, version int) error {
	eventType := "health.protocol.published"
	eventID := platformoutbox.DeterministicUUID(eventType + ":" + tenantID + ":" + protocolID)
	idem := eventType + ":" + protocolID
	now := time.Now().UTC()
	payload := map[string]any{"protocol_version_id": protocolID, "disease_key": p.DiseaseKey, "age_band": p.AgeBand, "version": version, "duration_days": p.DurationDays, "step_count": len(p.Steps)}
	envelope := map[string]any{"event_id": eventID, "event_type": eventType, "schema_version": "1.0.0", "schema_ref": "contracts/jsonschema/domain-event-envelope.schema.json#" + eventType, "aggregate_type": "health_protocol_version", "aggregate_id": protocolID, "occurred_at": now.Format(time.RFC3339Nano), "recorded_at": now.Format(time.RFC3339Nano), "producer": map[string]any{"module": "health", "service": "health-sop-import"}, "idempotency_key": idem, "actor": map[string]any{"actor_type": "admin", "actor_id": actorID, "actor_ref": nil}, "subject_type": "health_protocol_version", "subject_id": protocolID, "visibility_scope": map[string]any{"tenant_id": tenantID}, "evidence_refs": []any{}, "payload": payload, "trace_id": ""}
	body, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	headers, _ := json.Marshal(map[string]any{"content_type": "application/json"})
	_, err = tx.Exec(ctx, `INSERT INTO outbox_messages (tenant_id,event_id,event_type,schema_version,aggregate_type,aggregate_id,topic,payload,headers,idempotency_key,trace_id,status,next_attempt_at)
VALUES ($1::uuid,$2::uuid,$3,'1.0.0','health_protocol_version',$4::uuid,$5,$6::jsonb,$7::jsonb,$8,'','pending',now()) ON CONFLICT DO NOTHING`, tenantID, eventID, eventType, protocolID, healthTopic, body, headers, idem)
	return err
}
