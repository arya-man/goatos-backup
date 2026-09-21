package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
)

// The diagnosis-register half of Health Config.
//
// It mirrors protocol_authoring.go deliberately -- same draft/publish/retire shape,
// same ledger contract, same one-draft lock -- because the two tabs of one screen must
// not behave differently, and an author who has learned one editor has learned both.

// Every query this file runs, hoisted to a named constant.
//
// Not tidiness: a query assembled by concatenation inside a function cannot be reached by
// `validate-sqlc-plans` or by any test that wants to EXPLAIN it, so the plan for a read on the
// diagnosis path would be unprovable. These are small, indexed reads today -- four registers per
// tenant -- but "small today" is how an unprovable query gets written.
const (
	registerColumns = `
  health_diagnosis_register_version_id::text, animal_class, version, status,
  register_label, document, published_at, updated_at`

	sqlListRegisters = `
SELECT ` + registerColumns + `
FROM health_diagnosis_register_versions
WHERE tenant_id=$1::uuid AND status IN ('published','draft')
ORDER BY animal_class, status DESC, version DESC`

	sqlRegisterByID = `
SELECT ` + registerColumns + `
FROM health_diagnosis_register_versions
WHERE tenant_id=$1::uuid AND health_diagnosis_register_version_id=$2::uuid`

	// The SERVING read, once per observation. Deliberately the narrowest query here; it is
	// covered by health_diagnosis_register_one_published_uq.
	sqlPublishedRegister = `
SELECT ` + registerColumns + `
FROM health_diagnosis_register_versions
WHERE tenant_id=$1::uuid AND animal_class=$2 AND status='published'`

	sqlDraftIDForClass = `
SELECT health_diagnosis_register_version_id::text
FROM health_diagnosis_register_versions
WHERE tenant_id=$1::uuid AND animal_class=$2 AND status='draft'`

	sqlPublishedDocumentForClass = `
SELECT register_label, document FROM health_diagnosis_register_versions
WHERE tenant_id=$1::uuid AND animal_class=$2 AND status='published'`

	sqlPublishedIDForClass = `
SELECT health_diagnosis_register_version_id::text FROM health_diagnosis_register_versions
WHERE tenant_id=$1::uuid AND animal_class=$2 AND status='published'`

	sqlNextRegisterVersion = `
SELECT COALESCE(MAX(version), 0) + 1 FROM health_diagnosis_register_versions
WHERE tenant_id=$1::uuid AND animal_class=$2`

	sqlInsertRegisterDraft = `
INSERT INTO health_diagnosis_register_versions
  (tenant_id, animal_class, version, status, register_label, document, content_hash, created_by, updated_by)
VALUES ($1::uuid, $2, $3, 'draft', $4, $5::jsonb, $6, $7::uuid, $7::uuid)
RETURNING health_diagnosis_register_version_id::text`

	sqlInsertSeededRegister = `
INSERT INTO health_diagnosis_register_versions
  (tenant_id, animal_class, version, status, register_label, document, content_hash,
   created_by, updated_by, published_by, published_at)
VALUES ($1::uuid, $2, $3, 'published', $4, $5::jsonb, $6, $7::uuid, $7::uuid, $7::uuid, now())
RETURNING health_diagnosis_register_version_id::text`

	sqlDraftForSave = `
SELECT health_diagnosis_register_version_id::text, content_hash, version
FROM health_diagnosis_register_versions
WHERE tenant_id=$1::uuid AND animal_class=$2 AND status='draft'`

	sqlUpdateRegisterDraft = `
UPDATE health_diagnosis_register_versions
SET document=$3::jsonb, content_hash=$4, register_label=$5, updated_by=$6::uuid, updated_at=now()
WHERE tenant_id=$1::uuid AND health_diagnosis_register_version_id=$2::uuid AND status='draft'`

	sqlRetirePublishedRegister = `
UPDATE health_diagnosis_register_versions
SET status='retired', updated_at=now()
WHERE tenant_id=$1::uuid AND animal_class=$2 AND status='published'
RETURNING health_diagnosis_register_version_id::text`

	sqlPublishRegister = `
UPDATE health_diagnosis_register_versions
SET status='published', published_by=$3::uuid, published_at=now(), updated_at=now()
WHERE tenant_id=$1::uuid AND health_diagnosis_register_version_id=$2::uuid`

	sqlDeleteRegisterDraft = `
DELETE FROM health_diagnosis_register_versions
WHERE tenant_id=$1::uuid AND health_diagnosis_register_version_id=$2::uuid AND status='draft'`

	sqlLockRegisterVersion = `
SELECT animal_class, status, version FROM health_diagnosis_register_versions
WHERE tenant_id=$1::uuid AND health_diagnosis_register_version_id=$2::uuid FOR UPDATE`

	sqlTreatsCoverage = `
SELECT disease_key,
       count(*) FILTER (WHERE status='published' AND age_band='adult') AS adult_published,
       count(*) FILTER (WHERE status='published' AND age_band='kid')   AS kid_published
FROM health_protocol_versions
WHERE tenant_id=$1::uuid AND disease_key = ANY($2::text[])
GROUP BY disease_key`

	sqlReadRegisterLedger = `
SELECT request_fingerprint, outcome, animal_class, result_version_id::text, retired_version_id::text
FROM health_register_write_log WHERE tenant_id=$1::uuid AND idempotency_key=$2`

	sqlWriteRegisterLedger = `
INSERT INTO health_register_write_log
  (tenant_id, write_kind, idempotency_key, request_fingerprint, outcome, animal_class,
   result_version_id, retired_version_id, actor_ref)
VALUES ($1::uuid, $2, $3, $4, $5, $6, NULLIF($7,'')::uuid, NULLIF($8,'')::uuid, $9)`

	// ONE bigint, not two. Postgres has pg_advisory_xact_lock(bigint) and
	// pg_advisory_xact_lock(int, int) -- there is no two-bigint overload, and
	// hashtextextended returns bigint, so the two-argument form fails at RUNTIME with
	// "function does not exist" rather than at compile time. Hashing one combined key
	// keeps the lock tenant-scoped and needs no overload that does not exist.
	sqlLockRegisterIdentity = `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`
)

func (r *Repository) ListRegisters(ctx context.Context, tenantID string) ([]domain.RegisterSummary, error) {
	rows, err := r.pool.Query(ctx, sqlListRegisters, tenantID)
	if err != nil {
		return nil, fmt.Errorf("health: list diagnosis registers: %w", err)
	}
	defer rows.Close()

	out := []domain.RegisterSummary{}
	for rows.Next() {
		detail, err := scanRegister(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, detail.RegisterSummary)
	}
	return out, rows.Err()
}

func (r *Repository) GetRegister(ctx context.Context, tenantID, registerVersionID string) (domain.RegisterDetail, error) {
	row := r.pool.QueryRow(ctx, sqlRegisterByID, tenantID, registerVersionID)
	detail, err := scanRegister(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RegisterDetail{}, ports.ErrRegisterNotFound
	}
	if err != nil {
		return domain.RegisterDetail{}, err
	}
	detail.Problems = detail.Document.Validate()
	return detail, nil
}

// PublishedRegister is the SERVING read. It is deliberately the narrowest query in this
// file: it runs on the diagnosis path, once per observation, and must not widen.
func (r *Repository) PublishedRegister(ctx context.Context, tenantID, animalClass string) (domain.RegisterDetail, error) {
	row := r.pool.QueryRow(ctx, sqlPublishedRegister, tenantID, animalClass)
	detail, err := scanRegister(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RegisterDetail{}, ports.ErrRegisterNotFound
	}
	return detail, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRegister(row rowScanner) (domain.RegisterDetail, error) {
	var (
		d           domain.RegisterDetail
		raw         []byte
		publishedAt *time.Time
	)
	if err := row.Scan(&d.RegisterVersionID, &d.AnimalClass, &d.Version, &d.Status,
		&d.RegisterLabel, &raw, &publishedAt, &d.UpdatedAt); err != nil {
		return domain.RegisterDetail{}, err
	}
	// The stored document is read with the SAME strict loader the publish used, so a
	// row that somehow no longer parses fails loudly here rather than being served as
	// a register with silently missing rules.
	doc, err := diagnosis.LoadAuthored(raw)
	if err != nil {
		return domain.RegisterDetail{}, fmt.Errorf("health: stored register %s is unreadable: %w", d.RegisterVersionID, err)
	}
	d.Document = *doc
	d.QuestionCount = len(doc.Questions)
	d.RuleCount = len(doc.Rules)
	d.PublishedAt = publishedAt
	return d, nil
}

// GetDraftForEdit opens the editor: it returns the open draft, creating one as a copy
// of the live register when there is none.
func (r *Repository) GetRegisterDraftForEdit(ctx context.Context, cmd domain.RegisterVersionCommand, animalClass string) (domain.RegisterDetail, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.RegisterDetail{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := lockRegisterIdentity(ctx, tx, cmd.TenantID, animalClass); err != nil {
		return domain.RegisterDetail{}, err
	}

	var draftID string
	err = tx.QueryRow(ctx, sqlDraftIDForClass, cmd.TenantID, animalClass).Scan(&draftID)

	switch {
	case err == nil:
		// Already open. Returning it rather than refusing is what lets the same author
		// close the tab and come back.
	case errors.Is(err, pgx.ErrNoRows):
		draftID, err = openRegisterDraft(ctx, tx, cmd, animalClass)
		if err != nil {
			return domain.RegisterDetail{}, err
		}
	default:
		return domain.RegisterDetail{}, fmt.Errorf("health: read register draft: %w", err)
	}

	detail, err := loadRegisterInTx(ctx, tx, cmd.TenantID, draftID)
	if err != nil {
		return domain.RegisterDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.RegisterDetail{}, err
	}
	committed = true
	detail.Problems = detail.Document.Validate()
	return detail, nil
}

// openRegisterDraft copies the live register into a new draft.
//
// A class with NO published register cannot be drafted here. That is not an oversight:
// the first version of every class is written by the seed command, which transcribes
// the committed rule table, and a blank register published by accident would leave a
// whole class of animals diagnosable against nothing.
func openRegisterDraft(ctx context.Context, tx pgx.Tx, cmd domain.RegisterVersionCommand, animalClass string) (string, error) {
	var (
		label string
		doc   []byte
		next  int
	)
	err := tx.QueryRow(ctx, sqlPublishedDocumentForClass, cmd.TenantID, animalClass).Scan(&label, &doc)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ports.ErrRegisterNotFound
	}
	if err != nil {
		return "", fmt.Errorf("health: read published register: %w", err)
	}

	if err := tx.QueryRow(ctx, sqlNextRegisterVersion, cmd.TenantID, animalClass).Scan(&next); err != nil {
		return "", fmt.Errorf("health: next register version: %w", err)
	}

	hash, err := hashRegisterBytes(doc)
	if err != nil {
		return "", err
	}

	var id string
	err = tx.QueryRow(ctx, sqlInsertRegisterDraft,
		cmd.TenantID, animalClass, next, label, doc, hash, nullUUID(cmd.ActorID)).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
			strings.Contains(pgErr.ConstraintName, "health_diagnosis_register_one_draft") {
			return "", ports.ErrRegisterDraftExists
		}
		return "", fmt.Errorf("health: open register draft: %w", err)
	}
	return id, nil
}

func (r *Repository) SaveRegisterDraft(ctx context.Context, cmd domain.SaveRegisterDraftCommand) (domain.RegisterAuthoringResult, error) {
	return runRegisterTx(ctx, r, cmd.TenantID, cmd.ActorID, cmd.IdempotencyKey, cmd.RequestFingerprint,
		func(tx pgx.Tx) (domain.RegisterAuthoringResult, registerLedgerEntry, error) {
			var zero domain.RegisterAuthoringResult

			if _, err := lockRegisterIdentity(ctx, tx, cmd.TenantID, cmd.AnimalClass); err != nil {
				return zero, registerLedgerEntry{}, err
			}
			var draftID, storedHash string
			var version int
			err := tx.QueryRow(ctx, sqlDraftForSave,
				cmd.TenantID, cmd.AnimalClass).Scan(&draftID, &storedHash, &version)
			if errors.Is(err, pgx.ErrNoRows) {
				return zero, registerLedgerEntry{}, ports.ErrRegisterNotADraft
			}
			if err != nil {
				return zero, registerLedgerEntry{}, fmt.Errorf("health: read register draft: %w", err)
			}

			hash, err := domain.RegisterContentHash(cmd.Document)
			if err != nil {
				return zero, registerLedgerEntry{}, err
			}
			// Identical content is NOT a new revision. Without this, an editor that
			// saves as the author types produces a version history a reviewer cannot
			// read -- and the version history is how a dosage or a rule change is
			// audited.
			if hash == storedHash {
				res := domain.RegisterAuthoringResult{
					Outcome: "unchanged", AnimalClass: cmd.AnimalClass,
					RegisterVersionID: draftID, Version: version,
				}
				return res, registerLedgerEntry{
					WriteKind: domain.RegisterWriteKindDraftSave, Outcome: "unchanged",
					AnimalClass: cmd.AnimalClass, ResultVersionID: draftID,
				}, nil
			}

			body, err := json.Marshal(cmd.Document)
			if err != nil {
				return zero, registerLedgerEntry{}, err
			}
			if _, err := tx.Exec(ctx, sqlUpdateRegisterDraft,
				cmd.TenantID, draftID, body, hash, cmd.Document.RegisterVersion, nullUUID(cmd.ActorID)); err != nil {
				return zero, registerLedgerEntry{}, fmt.Errorf("health: save register draft: %w", err)
			}

			res := domain.RegisterAuthoringResult{
				Outcome: "saved", AnimalClass: cmd.AnimalClass,
				RegisterVersionID: draftID, Version: version,
			}
			return res, registerLedgerEntry{
				WriteKind: domain.RegisterWriteKindDraftSave, Outcome: "saved",
				AnimalClass: cmd.AnimalClass, ResultVersionID: draftID,
			}, nil
		})
}

// PublishDraft promotes a draft and retires the version it replaces, in ONE transaction.
//
// It re-validates what is actually STORED rather than trusting the service's check on
// what was submitted, because the draft may have been saved by a different request --
// and it adds the one check that can only be made with a database in hand: every rule's
// treats against the protocol catalog.
func (r *Repository) PublishRegisterDraft(ctx context.Context, cmd domain.RegisterVersionCommand) (domain.RegisterAuthoringResult, error) {
	return runRegisterTx(ctx, r, cmd.TenantID, cmd.ActorID, cmd.IdempotencyKey, cmd.RequestFingerprint,
		func(tx pgx.Tx) (domain.RegisterAuthoringResult, registerLedgerEntry, error) {
			var zero domain.RegisterAuthoringResult

			animalClass, status, version, err := lockRegisterVersionRow(ctx, tx, cmd.TenantID, cmd.RegisterVersionID)
			if err != nil {
				return zero, registerLedgerEntry{}, err
			}
			if status != "draft" {
				return zero, registerLedgerEntry{}, ports.ErrRegisterNotADraft
			}
			if _, err := lockRegisterIdentity(ctx, tx, cmd.TenantID, animalClass); err != nil {
				return zero, registerLedgerEntry{}, err
			}

			detail, err := loadRegisterInTx(ctx, tx, cmd.TenantID, cmd.RegisterVersionID)
			if err != nil {
				return zero, registerLedgerEntry{}, err
			}
			problems := detail.Document.Validate()
			if problems.Fatal() {
				return zero, registerLedgerEntry{}, &domain.ValidationError{
					Errors: registerFieldErrors(problems),
				}
			}
			// The register must claim the class it is stored under. A document that
			// does not is the one mistake the whole class split exists to prevent:
			// diagnosing a milk kid against the adult table.
			if !claimsClass(detail.Document.AppliesClass, animalClass) {
				return zero, registerLedgerEntry{}, &domain.ValidationError{Errors: []domain.FieldError{{
					Field:   "applies_class",
					Message: fmt.Sprintf("This register does not claim the %s class it is stored under.", animalClass),
				}}}
			}

			treatsProblems, err := checkTreatsAgainstCatalog(ctx, tx, cmd.TenantID, detail.Document.Rules)
			if err != nil {
				return zero, registerLedgerEntry{}, err
			}
			if treatsProblems.Fatal() {
				return zero, registerLedgerEntry{}, &domain.ValidationError{
					Errors: registerFieldErrors(treatsProblems),
				}
			}

			var retired string
			err = tx.QueryRow(ctx, sqlRetirePublishedRegister, cmd.TenantID, animalClass).Scan(&retired)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return zero, registerLedgerEntry{}, fmt.Errorf("health: retire register: %w", err)
			}

			if _, err := tx.Exec(ctx, sqlPublishRegister,
				cmd.TenantID, cmd.RegisterVersionID, nullUUID(cmd.ActorID)); err != nil {
				return zero, registerLedgerEntry{}, fmt.Errorf("health: publish register: %w", err)
			}

			warnings := append(problems, treatsProblems...)
			res := domain.RegisterAuthoringResult{
				Outcome: "published", AnimalClass: animalClass,
				RegisterVersionID: cmd.RegisterVersionID, Version: version,
				RetiredVersionID: retired, Warnings: warnings,
			}
			return res, registerLedgerEntry{
				WriteKind: domain.RegisterWriteKindDraftPublish, Outcome: "published",
				AnimalClass: animalClass, ResultVersionID: cmd.RegisterVersionID,
				RetiredVersionID: retired,
			}, nil
		})
}

func (r *Repository) DiscardRegisterDraft(ctx context.Context, cmd domain.RegisterVersionCommand) (domain.RegisterAuthoringResult, error) {
	return runRegisterTx(ctx, r, cmd.TenantID, cmd.ActorID, cmd.IdempotencyKey, cmd.RequestFingerprint,
		func(tx pgx.Tx) (domain.RegisterAuthoringResult, registerLedgerEntry, error) {
			var zero domain.RegisterAuthoringResult
			animalClass, status, _, err := lockRegisterVersionRow(ctx, tx, cmd.TenantID, cmd.RegisterVersionID)
			if err != nil {
				return zero, registerLedgerEntry{}, err
			}
			if status != "draft" {
				return zero, registerLedgerEntry{}, ports.ErrRegisterNotADraft
			}
			if _, err := tx.Exec(ctx, sqlDeleteRegisterDraft,
				cmd.TenantID, cmd.RegisterVersionID); err != nil {
				return zero, registerLedgerEntry{}, fmt.Errorf("health: discard register draft: %w", err)
			}
			res := domain.RegisterAuthoringResult{Outcome: "discarded", AnimalClass: animalClass}
			return res, registerLedgerEntry{
				WriteKind: domain.RegisterWriteKindDraftDiscard, Outcome: "discarded",
				AnimalClass: animalClass,
			}, nil
		})
}

// checkTreatsAgainstCatalog is the check the pure validator cannot make: whether the
// disease a rule opens actually exists, and whether its course has been written.
//
// EVERYTHING HERE IS A WARNING, and that was a correction rather than the first design.
// The first version refused a treats key matching NO disease, on the reasoning that it
// could only be a typo. Running the seeded register against the real catalog disproved
// it: EIGHT of the adult register's own rules -- metritis, tetanus, laminitis and five
// more -- name diseases that have no row in this farm's 27-disease catalog at all. That
// is the state `SOPRefToDiseaseKey`'s own comment describes, and it made the farm's
// OWN SHIPPED RULEBOOK unpublishable on the first press of Publish.
//
// Nothing mechanical separates "you mistyped it" from "nobody has authored that disease
// yet", because they produce the identical row: no match. So both are reported and
// neither refuses, and the sentence says what the consequence is -- the diagnosis will
// fire and open no course -- which is the part an author actually needs.
//
// Both age bands are required for a card to count as written, because the phone picks
// the band from the goat and a missing one fails at diagnosis time reading like a bug.
func checkTreatsAgainstCatalog(ctx context.Context, tx pgx.Tx, tenantID string, rules []diagnosis.Rule) (diagnosis.Problems, error) {
	wanted := map[string][]int{}
	for i, rule := range rules {
		if rule.Treats == "" {
			continue
		}
		wanted[rule.Treats] = append(wanted[rule.Treats], i)
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(wanted))
	for k := range wanted {
		keys = append(keys, k)
	}

	rows, err := tx.Query(ctx, sqlTreatsCoverage, tenantID, keys)
	if err != nil {
		return nil, fmt.Errorf("health: read protocol catalog: %w", err)
	}
	defer rows.Close()

	type coverage struct{ adult, kid int }
	found := map[string]coverage{}
	for rows.Next() {
		var key string
		var c coverage
		if err := rows.Scan(&key, &c.adult, &c.kid); err != nil {
			return nil, err
		}
		found[key] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var ps diagnosis.Problems
	for key, ruleIdx := range wanted {
		c, exists := found[key]
		for _, i := range ruleIdx {
			path := fmt.Sprintf("rules.%d.treats", i)
			switch {
			case !exists:
				ps = append(ps, diagnosis.Problem{Path: path, Fatal: false, Message: fmt.Sprintf(
					"%q is not a disease on this farm yet, so %s will be diagnosed and open no course until someone authors it",
					key, rules[i].ID)})
			case c.adult == 0 || c.kid == 0:
				ps = append(ps, diagnosis.Problem{Path: path, Fatal: false, Message: fmt.Sprintf(
					"%q has no published treatment for %s yet, so %s will be diagnosed with no course to start",
					key, missingBands(c.adult, c.kid), rules[i].ID)})
			}
		}
	}
	return ps, nil
}

func missingBands(adult, kid int) string {
	switch {
	case adult == 0 && kid == 0:
		return "adults or kids"
	case adult == 0:
		return "adults"
	default:
		return "kids"
	}
}

func claimsClass(classes []string, want string) bool {
	if len(classes) == 0 {
		return false
	}
	for _, c := range classes {
		if c == want {
			return true
		}
	}
	return false
}

func registerFieldErrors(ps diagnosis.Problems) []domain.FieldError {
	out := make([]domain.FieldError, 0, len(ps))
	for _, p := range ps {
		if !p.Fatal {
			continue
		}
		out = append(out, domain.FieldError{Field: p.Path, Message: p.Message})
	}
	return out
}

func loadRegisterInTx(ctx context.Context, tx pgx.Tx, tenantID, versionID string) (domain.RegisterDetail, error) {
	row := tx.QueryRow(ctx, sqlRegisterByID, tenantID, versionID)
	detail, err := scanRegister(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RegisterDetail{}, ports.ErrRegisterNotFound
	}
	return detail, err
}

// lockRegisterIdentity serialises every write against one class so the
// read-then-write sequences in this file cannot interleave.
func lockRegisterIdentity(ctx context.Context, tx pgx.Tx, tenantID, animalClass string) (bool, error) {
	if _, err := tx.Exec(ctx, sqlLockRegisterIdentity, "health_register:"+tenantID+":"+animalClass); err != nil {
		return false, fmt.Errorf("health: lock register identity: %w", err)
	}
	return true, nil
}

func lockRegisterVersionRow(ctx context.Context, tx pgx.Tx, tenantID, versionID string) (animalClass, status string, version int, err error) {
	err = tx.QueryRow(ctx, sqlLockRegisterVersion,
		tenantID, versionID).Scan(&animalClass, &status, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", 0, ports.ErrRegisterNotFound
	}
	if err != nil {
		return "", "", 0, fmt.Errorf("health: lock register version: %w", err)
	}
	return animalClass, status, version, nil
}

func hashRegisterBytes(doc []byte) (string, error) {
	parsed, err := diagnosis.LoadAuthored(doc)
	if err != nil {
		return "", err
	}
	return domain.RegisterContentHash(*parsed)
}

func nullUUID(id string) any {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	return id
}

type registerLedgerEntry struct {
	WriteKind        string
	Outcome          string
	AnimalClass      string
	ResultVersionID  string
	RetiredVersionID string
}

// runRegisterTx applies the same idempotency contract the protocol writes use:
//
//	exact replay (same key, same fingerprint)  -> the ledger's stored result, no side effects
//	same key, DIFFERENT fingerprint            -> ErrConflict
//	new key                                    -> body runs, ledger written in the SAME tx
func runRegisterTx(
	ctx context.Context,
	r *Repository,
	tenantID, actorID, idempotencyKey, fingerprint string,
	body func(pgx.Tx) (domain.RegisterAuthoringResult, registerLedgerEntry, error),
) (domain.RegisterAuthoringResult, error) {
	if strings.TrimSpace(idempotencyKey) == "" || strings.TrimSpace(fingerprint) == "" ||
		strings.TrimSpace(actorID) == "" {
		return domain.RegisterAuthoringResult{}, ports.ErrConflict
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.RegisterAuthoringResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if replay, found, err := readRegisterLedger(ctx, tx, tenantID, idempotencyKey, fingerprint); err != nil {
		return domain.RegisterAuthoringResult{}, err
	} else if found {
		if err := tx.Commit(ctx); err != nil {
			return domain.RegisterAuthoringResult{}, err
		}
		committed = true
		return replay, nil
	}

	res, entry, err := body(tx)
	if err != nil {
		return domain.RegisterAuthoringResult{}, err
	}
	if err := writeRegisterLedger(ctx, tx, tenantID, actorID, idempotencyKey, fingerprint, entry); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
			strings.Contains(pgErr.ConstraintName, "health_register_write_log_idempotency") {
			// A concurrent request with the same key committed first; return the
			// winner's result, which is what an idempotent replay must do.
			_ = tx.Rollback(ctx)
			committed = true
			return readCommittedRegisterLedger(ctx, r, tenantID, idempotencyKey, fingerprint)
		}
		return domain.RegisterAuthoringResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.RegisterAuthoringResult{}, err
	}
	committed = true
	return res, nil
}

func readRegisterLedger(ctx context.Context, tx pgx.Tx, tenantID, key, fingerprint string) (domain.RegisterAuthoringResult, bool, error) {
	var storedFingerprint, outcome, animalClass string
	var resultID, retiredID *string
	err := tx.QueryRow(ctx, sqlReadRegisterLedger,
		tenantID, key).Scan(&storedFingerprint, &outcome, &animalClass, &resultID, &retiredID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RegisterAuthoringResult{}, false, nil
	}
	if err != nil {
		return domain.RegisterAuthoringResult{}, false, fmt.Errorf("health: read register ledger: %w", err)
	}
	// Same key, different content is a CLIENT BUG, not a replay. Returning the
	// original result would tell the caller their edit landed when it never ran.
	if storedFingerprint != fingerprint {
		return domain.RegisterAuthoringResult{}, false, ports.ErrConflict
	}
	return domain.RegisterAuthoringResult{
		Outcome:           outcome,
		AnimalClass:       animalClass,
		RegisterVersionID: strOrEmpty(resultID),
		RetiredVersionID:  strOrEmpty(retiredID),
		IdempotentReplay:  true,
	}, true, nil
}

func readCommittedRegisterLedger(ctx context.Context, r *Repository, tenantID, key, fingerprint string) (domain.RegisterAuthoringResult, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.RegisterAuthoringResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	res, found, err := readRegisterLedger(ctx, tx, tenantID, key, fingerprint)
	if err != nil {
		return domain.RegisterAuthoringResult{}, err
	}
	if !found {
		return domain.RegisterAuthoringResult{}, ports.ErrConflict
	}
	return res, nil
}

func writeRegisterLedger(ctx context.Context, tx pgx.Tx, tenantID, actorID, key, fingerprint string, e registerLedgerEntry) error {
	_, err := tx.Exec(ctx, sqlWriteRegisterLedger,
		tenantID, e.WriteKind, key, fingerprint, e.Outcome, e.AnimalClass,
		e.ResultVersionID, e.RetiredVersionID, actorID)
	return err
}

// SeedPublishedRegisters publishes the committed rule table as version 1 for every
// animal class a tenant does not already have one for.
//
// It is how a farm's rulebook is BORN, and it runs once. A class that already has a
// published register is left alone -- re-seeding would retire a register the farm has
// since edited and replace it with the shipped one, silently discarding clinical work.
// That is the same failure ReplacePublishedProtocols fails closed on for treatment
// cards, and it is why this is additive rather than a replace.
//
// Every seeded register is validated before it is written, so a defect in the committed
// YAML or in the transcribed form fails the seed instead of publishing a rule table
// nobody can diagnose against.
func (r *Repository) SeedPublishedRegisters(ctx context.Context, tenantID, actorID string) ([]domain.RegisterAuthoringResult, error) {
	out := []domain.RegisterAuthoringResult{}

	for _, class := range diagnosis.Classes {
		doc, err := diagnosis.SeedAuthored(class, domain.SOPRefToDiseaseKey)
		if err != nil {
			return nil, fmt.Errorf("health: seed %s register: %w", class, err)
		}
		res, err := r.seedOneRegister(ctx, tenantID, actorID, class, *doc)
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, nil
}

func (r *Repository) seedOneRegister(
	ctx context.Context, tenantID, actorID, class string, doc diagnosis.AuthoredRegister,
) (domain.RegisterAuthoringResult, error) {
	var zero domain.RegisterAuthoringResult

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return zero, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := lockRegisterIdentity(ctx, tx, tenantID, class); err != nil {
		return zero, err
	}

	var existingID string
	err = tx.QueryRow(ctx, sqlPublishedIDForClass, tenantID, class).Scan(&existingID)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return zero, err
		}
		committed = true
		return domain.RegisterAuthoringResult{
			Outcome: "unchanged", AnimalClass: class, RegisterVersionID: existingID,
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return zero, fmt.Errorf("health: read published register: %w", err)
	}

	body, err := json.Marshal(doc)
	if err != nil {
		return zero, err
	}
	hash, err := domain.RegisterContentHash(doc)
	if err != nil {
		return zero, err
	}

	var next int
	if err := tx.QueryRow(ctx, sqlNextRegisterVersion, tenantID, class).Scan(&next); err != nil {
		return zero, fmt.Errorf("health: next register version: %w", err)
	}

	var id string
	if err := tx.QueryRow(ctx, sqlInsertSeededRegister,
		tenantID, class, next, doc.RegisterVersion, body, hash, nullUUID(actorID)).Scan(&id); err != nil {
		return zero, fmt.Errorf("health: seed register: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return zero, err
	}
	committed = true
	return domain.RegisterAuthoringResult{
		Outcome: "published", AnimalClass: class, RegisterVersionID: id, Version: next,
		Warnings: doc.Validate(),
	}, nil
}
