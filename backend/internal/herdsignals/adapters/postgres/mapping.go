package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
)

// THE MAPPING WRITES.
//
// Until these existed the whole module was read-only: three buttons on the Tag Mapping tab were
// rendered disabled because there was no write endpoint behind them, and on staging NOTHING is
// mapped -- so mapping was simultaneously the only action that makes the module useful and the
// one thing that could not be done.
//
// EVERY write here must produce rows that satisfy the READ path's own mapping predicate exactly:
//
//	goat_identifiers.normalized_value matches the tag id OR the tag MAC,
//	same tenant_id, status = 'active', smart_tag_capable IS TRUE
//
// (see resolveTagMapping / ResolveTagsBatch). That is not asserted by inspection -- the
// integration tests bind and then call the read path to prove the row is actually resolvable.
//
// normalized_value ALWAYS goes through domain.NormalizeTagIdentifier, which is the identity
// module's canonical normalizer (strings.ToUpper(strings.TrimSpace(v)),
// backend/internal/identity/app/service.go). No case handling is hand-rolled here: a device that
// reports a lowercase MAC must match the uppercase-stored identifier, and the normalizer is the
// single place that rule lives.
//
// AND EVERY WRITE STAMPS THE MONITORING BOUNDARY. goat_identifiers.smart_tag_mapped_at is the
// instant animal monitoring starts (migration 000197); herd_signal_tag_latest
// .animal_monitoring_since is its denormalised copy on the hot read path. Packets before it are
// device telemetry -- a tag rattling in a box on a bench -- and must never reach an animal's
// baseline, pattern window, or correlation. A REPLACE starts a NEW period for the new tag and
// ends the old one.

// moduleSourceSystem is stamped on goat_identifiers.source_system for every row THIS module
// invents to carry a BLE binding. It is the provenance release depends on: a row carrying it was
// created for a binding and is deleted when the binding ends; any other row is the animal's own
// identity, which this module flags and unflags but never destroys.
const moduleSourceSystem = "herd_signals"

// bindValues returns the normalized identifier values one physical tag claims: its id, plus its
// MAC when the tag reports a distinct one. Both are claimed because the read path matches a
// packet by tag_id OR tag_mac while a single identifier row carries exactly one
// normalized_value -- claiming only one of them would leave half the tag's packets resolving to
// no animal.
func bindValues(tagID, tagMAC string) (values []string, normTagID string, normTagMAC *string, err error) {
	normTagID = domain.NormalizeTagIdentifier(tagID)
	if normTagID == "" {
		return nil, "", nil, fmt.Errorf("tag_id is required: %w", domain.ErrValidation)
	}
	values = []string{normTagID}
	if n := domain.NormalizeTagIdentifier(tagMAC); n != "" && n != normTagID {
		values = append(values, n)
		normTagMAC = &n
	}
	return values, normTagID, normTagMAC, nil
}

func validateIdentifierType(in string) (string, error) {
	if in == "" {
		return domain.DefaultSmartTagIdentifierType, nil
	}
	for _, allowed := range domain.SmartTagIdentifierTypes {
		if in == allowed {
			return in, nil
		}
	}
	return "", fmt.Errorf("identifier_type %q must be one of %v: %w", in, domain.SmartTagIdentifierTypes, domain.ErrValidation)
}

// existingIdentifier is one goat_identifiers row already holding a value this write wants.
type existingIdentifier struct {
	IdentifierID    string
	GoatID          string
	NormalizedValue string
	Status          string
	SmartTagCapable *bool
	// ModuleCreated is true when THIS module invented the row to carry a BLE binding
	// (source_system = 'herd_signals'), as opposed to a pre-existing identity row -- the
	// animal's real ear tag -- that a bind merely flagged as smart-tag capable.
	//
	// The distinction decides what RELEASE means, and getting it wrong is destructive in one
	// direction: retiring or deleting an animal's real ear-tag identity because a BLE binding
	// ended would destroy identity this module does not own.
	ModuleCreated bool
}

// lockIdentifiersByValue locks every row in the tenant holding any of these normalized values.
// The lock matters: goat_identifiers_lifetime_value_unique is UNIQUE (tenant_id,
// normalized_value) for the LIFETIME of the value (retired rows included), so two concurrent
// binds of the same tag must serialise here rather than race into a constraint violation.
func lockIdentifiersByValue(ctx context.Context, tx pgx.Tx, tenantID string, values []string) ([]existingIdentifier, error) {
	rows, err := tx.Query(ctx, `
		SELECT identifier_id::text, goat_id::text, normalized_value, status, smart_tag_capable,
		       COALESCE(source_system = 'herd_signals', false) AS module_created
		FROM public.goat_identifiers
		WHERE tenant_id = $1::uuid AND normalized_value = ANY($2)
		ORDER BY identifier_id
		FOR UPDATE
	`, tenantID, values)
	if err != nil {
		return nil, fmt.Errorf("lock identifiers by value: %w", err)
	}
	defer rows.Close()
	var out []existingIdentifier
	for rows.Next() {
		var e existingIdentifier
		if err := rows.Scan(&e.IdentifierID, &e.GoatID, &e.NormalizedValue, &e.Status, &e.SmartTagCapable, &e.ModuleCreated); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// liveSmartTagsForGoat returns the animal's currently-bound smart tag identifiers.
func liveSmartTagsForGoat(ctx context.Context, tx pgx.Tx, tenantID, goatID string) ([]existingIdentifier, error) {
	rows, err := tx.Query(ctx, `
		SELECT identifier_id::text, goat_id::text, normalized_value, status, smart_tag_capable,
		       COALESCE(source_system = 'herd_signals', false) AS module_created
		FROM public.goat_identifiers
		WHERE tenant_id = $1::uuid AND goat_id = $2::uuid
		      AND status = 'active' AND smart_tag_capable IS TRUE
		ORDER BY identifier_id
		FOR UPDATE
	`, tenantID, goatID)
	if err != nil {
		return nil, fmt.Errorf("lock live smart tags: %w", err)
	}
	defer rows.Close()
	var out []existingIdentifier
	for rows.Next() {
		var e existingIdentifier
		if err := rows.Scan(&e.IdentifierID, &e.GoatID, &e.NormalizedValue, &e.Status, &e.SmartTagCapable, &e.ModuleCreated); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func goatExists(ctx context.Context, tx pgx.Tx, tenantID, goatID string) error {
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM public.goats WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, tenantID, goatID).Scan(&one)
	if err == pgx.ErrNoRows {
		return fmt.Errorf("goat %s not found in this tenant: %w", goatID, domain.ErrMappingNotFound)
	}
	if err != nil {
		return fmt.Errorf("look up goat: %w", err)
	}
	return nil
}

// claimValues writes the identifier rows for one physical tag and returns their ids. An existing
// ACTIVE row for this same animal is upgraded in place (never duplicated); a value with no row
// yet gets a new one carrying the module's canonical scope_key/normalizer_version and the
// caller's identifier_type.
//
// smart_tag_mapped_at is COALESCEd on the update path: re-binding a tag that is ALREADY bound is
// idempotent and must not silently restart the animal's monitoring period (which would discard
// real history). A genuinely NEW period is started only by an insert, or by REPLACE, which
// unbinds first and therefore clears the stamp before re-stamping it.
func claimValues(ctx context.Context, tx pgx.Tx, tenantID, goatID, identifierType string, rawByNorm map[string]string, values []string, mappedAt time.Time, existing []existingIdentifier) ([]string, error) {
	byValue := make(map[string]existingIdentifier, len(existing))
	for _, e := range existing {
		byValue[e.NormalizedValue] = e
	}
	ids := make([]string, 0, len(values))
	for _, v := range values {
		if e, ok := byValue[v]; ok {
			// RECLAIM path. An ACTIVE row is simply flagged. A NON-ACTIVE row is reclaimed only
			// when THIS module created it: a leftover of an earlier binding (a release whose
			// delete was blocked, or a row an operator retired by hand) must never become a dead
			// end that no endpoint can clear -- that is exactly the state that made an animal
			// impossible to re-tag through the product. A non-active row this module did NOT
			// create is left alone and refused upstream: reactivating someone else's retired
			// identity is the identity module's decision, not ours.
			var id string
			// scale-guard:ignore: 2 = fixed upper bound; values = [normalized_tag_id, optional normalized_tag_mac] from bindValues()
			if err := tx.QueryRow(ctx, `
				UPDATE public.goat_identifiers
				SET status = 'active',
				    valid_to = NULL,
				    smart_tag_capable = true,
				    smart_tag_mapped_at = COALESCE(smart_tag_mapped_at, $3),
				    updated_at = now()
				WHERE tenant_id = $1::uuid AND identifier_id = $2::uuid
				RETURNING identifier_id::text
			`, tenantID, e.IdentifierID, mappedAt).Scan(&id); err != nil {
				return nil, fmt.Errorf("claim existing identifier %s for this binding: %w", e.IdentifierID, err)
			}
			ids = append(ids, id)
			continue
		}
		raw := rawByNorm[v]
		if raw == "" {
			raw = v
		}
		// source_system stamps PROVENANCE, and it is load-bearing, not decoration: it is the only
		// way release can tell a row this module invented to carry a BLE binding from the
		// animal's real ear-tag identity row that a bind merely flagged. Release deletes the
		// former and must never touch the latter.
		var id string
		// scale-guard:ignore: 2 = fixed upper bound; values = [normalized_tag_id, optional normalized_tag_mac] from bindValues()
		if err := tx.QueryRow(ctx, `
			INSERT INTO public.goat_identifiers (
				tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
				scope_key, is_primary_for_goat, status, valid_from, normalizer_version,
				smart_tag_capable, smart_tag_mapped_at, source_system, source_record_id
			) VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, false, 'active', $7, $8, true, $7, $9, $5)
			RETURNING identifier_id::text
		`, tenantID, goatID, identifierType, raw, v, domain.SmartTagScopeKey, mappedAt, domain.SmartTagNormalizerVersion, moduleSourceSystem).Scan(&id); err != nil {
			return nil, fmt.Errorf("claim identifier value %q: %w", v, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// releaseBinding ends a smart-tag binding COMPLETELY, and the definition of "completely" is the
// whole point of this function.
//
// A release that merely cleared the smart_tag_capable flag left the identifier row behind, still
// ACTIVE, still claiming its value under goat_identifiers_lifetime_value_unique (which is
// lifetime-scoped: a value stays claimed even after the row is retired). Those leftovers then
// blocked every future write on the animal -- MAP refused with mapping_conflict, REPLACE refused
// with "held by a retired identifier ... must be reactivated through the identity module" -- and
// nothing in the product could clear them, because release had already reported success. It took
// hand-written SQL against the database to get an animal out. There is no equivalent escape on a
// real farm.
//
// THE DECISION, made deliberately: rows THIS MODULE CREATED are DELETED, not retired.
//
//   - They are not identity history. The animal never "had" that identifier as an identity fact;
//     it had a device attached to it for a while. That period is fully recorded in
//     herd_signal_packets, which release never touches.
//   - Retiring keeps the value claimed forever, and a claimed value is a dead end unless the
//     write path also learns to reactivate. Deleting removes the dead end outright -- including
//     the case retiring cannot fix at all, the same physical tag later going onto a DIFFERENT
//     animal, which is ordinary for reusable BLE hardware.
//   - Retire remains available and honest for identity, which is the identity module's verb on
//     its own rows -- not this module's, on rows it invented.
//
// Rows this module did NOT create are a different thing entirely: they are the animal's real
// ear-tag identity, which a bind merely FLAGGED as smart-tag capable. For those, release clears
// only the flag and the stamp. Deleting or retiring one because a BLE binding ended would destroy
// identity this module does not own.
func releaseBinding(ctx context.Context, tx pgx.Tx, tenantID string, rows []existingIdentifier) error {
	var deletable, flagOnly []string
	for _, e := range rows {
		if e.ModuleCreated {
			deletable = append(deletable, e.IdentifierID)
			continue
		}
		flagOnly = append(flagOnly, e.IdentifierID)
	}
	if len(flagOnly) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE public.goat_identifiers
			SET smart_tag_capable = false, smart_tag_mapped_at = NULL, updated_at = now()
			WHERE tenant_id = $1::uuid AND identifier_id = ANY($2::uuid[])
		`, tenantID, flagOnly); err != nil {
			return fmt.Errorf("clear smart-tag flag on pre-existing identity rows: %w", err)
		}
	}
	if len(deletable) > 0 {
		if _, err := tx.Exec(ctx, `
			DELETE FROM public.goat_identifiers
			WHERE tenant_id = $1::uuid AND identifier_id = ANY($2::uuid[])
		`, tenantID, deletable); err != nil {
			return fmt.Errorf("delete the identifier rows this binding created: %w", err)
		}
	}
	return nil
}

// syncTagLatestMonitoring pushes the mapping decision onto the hot read path: the denormalised
// monitoring boundary and the mapping_state the live view reads. Without this the live view
// would keep reporting the tag as unmapped until its next packet arrived, and every
// animal-attributed read would keep using the OLD boundary in the meantime.
//
// The predicate mirrors the read path's own matching rule (normalized tag_id OR tag_mac).
func syncTagLatestMonitoring(ctx context.Context, tx pgx.Tx, tenantID string, values []string, since *time.Time) error {
	if len(values) == 0 {
		return nil
	}
	mappingState := "unmapped"
	if since != nil {
		mappingState = "mapped"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE public.herd_signal_tag_latest
		SET animal_monitoring_since = $3, mapping_state = $4, updated_at = now()
		WHERE tenant_id = $1::uuid
		      AND (UPPER(BTRIM(tag_id)) = ANY($2) OR UPPER(BTRIM(tag_mac)) = ANY($2))
	`, tenantID, values, since, mappingState); err != nil {
		return fmt.Errorf("sync tag_latest monitoring boundary: %w", err)
	}
	return nil
}

// BindTagMapping implements ports.Repository.
func (r *Repository) BindTagMapping(ctx context.Context, tenantID string, req domain.BindTagMappingRequest) (domain.TagMappingResponse, error) {
	var out domain.TagMappingResponse
	identifierType, err := validateIdentifierType(req.IdentifierType)
	if err != nil {
		return out, err
	}
	values, normTagID, normTagMAC, err := bindValues(req.TagID, req.TagMAC)
	if err != nil {
		return out, err
	}
	rawByNorm := map[string]string{normTagID: req.TagID}
	if normTagMAC != nil {
		rawByNorm[*normTagMAC] = req.TagMAC
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return out, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	if err := goatExists(ctx, tx, tenantID, req.GoatID); err != nil {
		return out, err
	}

	existing, err := lockIdentifiersByValue(ctx, tx, tenantID, values)
	if err != nil {
		return out, err
	}
	mine := make([]existingIdentifier, 0, len(existing))
	for _, e := range existing {
		if e.GoatID != req.GoatID {
			return out, fmt.Errorf("tag value %q is already claimed by another animal (%s): %w", e.NormalizedValue, e.GoatID, domain.ErrMappingConflict)
		}
		if e.Status != "active" && !e.ModuleCreated {
			// A non-active row this module did NOT create is someone else's retired identity.
			// Reactivating it is the identity module's decision, not a herd-signals one, so this
			// refuses loudly instead of quietly resurrecting it.
			return out, fmt.Errorf("tag value %q is held by a %s identifier on this animal that this module did not create; it must be reactivated through the identity module before it can carry a smart tag: %w", e.NormalizedValue, e.Status, domain.ErrMappingConflict)
		}
		// A non-active row this module DID create is a leftover of an earlier binding, and it is
		// reclaimed rather than refused. Refusing was a dead end no endpoint could clear: the
		// operator saw "must be reactivated through the identity module" with no way to do it.
		mine = append(mine, e)
	}

	// One live smart tag per animal. Two would make every animal-attributed number ambiguous
	// (which tag's motion is this animal's?) and is exactly the state a botched re-tag produces.
	live, err := liveSmartTagsForGoat(ctx, tx, tenantID, req.GoatID)
	if err != nil {
		return out, err
	}
	inBinding := make(map[string]struct{}, len(values))
	for _, v := range values {
		inBinding[v] = struct{}{}
	}
	for _, e := range live {
		if _, ok := inBinding[e.NormalizedValue]; !ok {
			return out, fmt.Errorf("animal %s already carries a live smart tag (%s); use the replace endpoint to swap it: %w", req.GoatID, e.NormalizedValue, domain.ErrMappingConflict)
		}
	}

	mappedAt := time.Now().UTC()
	ids, err := claimValues(ctx, tx, tenantID, req.GoatID, identifierType, rawByNorm, values, mappedAt, mine)
	if err != nil {
		return out, err
	}
	// Read the stamp back rather than assuming it: on an idempotent re-bind the COALESCE kept
	// the ORIGINAL instant, and the response must report the boundary that is actually in force.
	effective, err := effectiveMappedAt(ctx, tx, tenantID, ids)
	if err != nil {
		return out, err
	}
	if err := syncTagLatestMonitoring(ctx, tx, tenantID, values, &effective); err != nil {
		return out, err
	}
	if err := tx.Commit(ctx); err != nil {
		return out, fmt.Errorf("commit bind tag mapping: %w", err)
	}

	return domain.TagMappingResponse{
		GoatID:               req.GoatID,
		TagID:                normTagID,
		TagMAC:               normTagMAC,
		IdentifierIDs:        ids,
		MappingState:         "mapped",
		MonitoringSince:      &effective,
		UnboundIdentifierIDs: []string{},
	}, nil
}

// effectiveMappedAt returns the earliest smart_tag_mapped_at actually stored on the rows this
// write touched -- the boundary in force, not the one the caller hoped for.
func effectiveMappedAt(ctx context.Context, tx pgx.Tx, tenantID string, ids []string) (time.Time, error) {
	var at time.Time
	err := tx.QueryRow(ctx, `
		SELECT min(smart_tag_mapped_at)
		FROM public.goat_identifiers
		WHERE tenant_id = $1::uuid AND identifier_id = ANY($2::uuid[]) AND smart_tag_mapped_at IS NOT NULL
	`, tenantID, ids).Scan(&at)
	if err != nil {
		return time.Time{}, fmt.Errorf("read back monitoring boundary: %w", err)
	}
	return at.UTC(), nil
}

// UnmapTagMapping implements ports.Repository: release a binding with no replacement.
//
// IT RELEASES THE WHOLE BINDING, NOT THE VALUE THE CALLER NAMED. A MAP creates one identifier row
// per value the physical tag reports -- its printed id AND its MAC -- so a release that only
// unbinds the rows matching the caller's supplied values leaves the other half of the binding
// live. That was a real defect found by driving the API on the live stack, not by these tests:
//
//	MAP   {tag_id: A0003B, tag_mac: f0c990a0003b}  -> created TWO identifiers
//	UNMAP {tag_id: A0003B}                         -> released ONE, reported success
//
// The tag then READ as unmapped, so every API surface looked correct, while the animal stayed
// pinned to the MAC row of a tag that no longer existed on it. Nothing in the product could
// release that row -- unmap had already reported success and the tag no longer looked mapped --
// so the animal could never be re-tagged through the UI again. On a farm that is an animal
// permanently stuck, recoverable only with hand-written SQL.
//
// So: the supplied values IDENTIFY the animal, and then the animal's ENTIRE live smart-tag
// binding is released in the one transaction. That also makes unmap idempotent in the useful
// direction -- releasing by id or by MAC does the same complete thing.
//
// It clears smart_tag_capable and smart_tag_mapped_at WITHOUT retiring the identity rows: a row
// may also be the animal's ordinary ear-tag identity, and the physical value stays claimed for
// the tenant's lifetime either way. Clearing the flag is what makes the read path stop resolving
// the tag to this animal; clearing the stamp is what makes NULL mean "device telemetry only"
// again. The tag's packets keep flowing and its stored history stays intact.
func (r *Repository) UnmapTagMapping(ctx context.Context, tenantID string, req domain.UnmapTagMappingRequest) (domain.TagMappingResponse, error) {
	var out domain.TagMappingResponse
	values, normTagID, normTagMAC, err := bindValues(req.TagID, req.TagMAC)
	if err != nil {
		return out, err
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return out, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	// Step 1: the caller's values identify WHICH ANIMAL is being unmapped.
	existing, err := lockIdentifiersByValue(ctx, tx, tenantID, values)
	if err != nil {
		return out, err
	}
	goatID := ""
	for _, e := range existing {
		if e.Status != "active" || e.SmartTagCapable == nil || !*e.SmartTagCapable {
			continue
		}
		goatID = e.GoatID
		break
	}
	if goatID == "" {
		// Unmapping something that is not mapped is a caller mistake worth naming, not a silent
		// no-op: the operator believes they just released a binding.
		return out, fmt.Errorf("tag %q is not mapped to an animal: %w", normTagID, domain.ErrMappingConflict)
	}

	// Step 2: release that animal's ENTIRE live binding -- every identifier row, whichever value
	// it holds -- so no half-bound remainder can survive this call.
	live, err := liveSmartTagsForGoat(ctx, tx, tenantID, goatID)
	if err != nil {
		return out, err
	}
	ids := make([]string, 0, len(live))
	releasedValues := make([]string, 0, len(live))
	for _, e := range live {
		ids = append(ids, e.IdentifierID)
		releasedValues = append(releasedValues, e.NormalizedValue)
	}
	if err := releaseBinding(ctx, tx, tenantID, live); err != nil {
		return out, err
	}
	// Clear the hot-read boundary for every released value, not only the caller's: a tag_latest
	// row keyed on the MAC alone must stop claiming an animal too.
	if err := syncTagLatestMonitoring(ctx, tx, tenantID, releasedValues, nil); err != nil {
		return out, err
	}
	if err := tx.Commit(ctx); err != nil {
		return out, fmt.Errorf("commit unmap tag mapping: %w", err)
	}

	return domain.TagMappingResponse{
		GoatID:               goatID,
		TagID:                normTagID,
		TagMAC:               normTagMAC,
		IdentifierIDs:        []string{},
		MappingState:         "unmapped",
		MonitoringSince:      nil,
		UnboundIdentifierIDs: ids,
	}, nil
}

// ReplaceTagMapping implements ports.Repository: unbind the old tag and bind the new one in ONE
// transaction, so the animal is never left carrying two live smart tags (ambiguous telemetry) or
// none (silently unmonitored).
func (r *Repository) ReplaceTagMapping(ctx context.Context, tenantID string, req domain.ReplaceTagMappingRequest) (domain.TagMappingResponse, error) {
	var out domain.TagMappingResponse
	identifierType, err := validateIdentifierType(req.IdentifierType)
	if err != nil {
		return out, err
	}
	values, normTagID, normTagMAC, err := bindValues(req.NewTagID, req.NewTagMAC)
	if err != nil {
		return out, err
	}
	rawByNorm := map[string]string{normTagID: req.NewTagID}
	if normTagMAC != nil {
		rawByNorm[*normTagMAC] = req.NewTagMAC
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return out, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	if err := goatExists(ctx, tx, tenantID, req.GoatID); err != nil {
		return out, err
	}

	live, err := liveSmartTagsForGoat(ctx, tx, tenantID, req.GoatID)
	if err != nil {
		return out, err
	}
	if len(live) == 0 {
		return out, fmt.Errorf("animal %s has no live smart tag to replace; bind one instead: %w", req.GoatID, domain.ErrMappingConflict)
	}

	existing, err := lockIdentifiersByValue(ctx, tx, tenantID, values)
	if err != nil {
		return out, err
	}
	mine := make([]existingIdentifier, 0, len(existing))
	for _, e := range existing {
		if e.GoatID != req.GoatID {
			return out, fmt.Errorf("new tag value %q is already claimed by another animal (%s): %w", e.NormalizedValue, e.GoatID, domain.ErrMappingConflict)
		}
		if e.Status != "active" && !e.ModuleCreated {
			return out, fmt.Errorf("new tag value %q is held by a %s identifier on this animal that this module did not create; it must be reactivated through the identity module first: %w", e.NormalizedValue, e.Status, domain.ErrMappingConflict)
		}
		// Same as bind: a leftover this module created is reclaimed, never a dead end.
		mine = append(mine, e)
	}

	inBinding := make(map[string]struct{}, len(values))
	for _, v := range values {
		inBinding[v] = struct{}{}
	}
	unbindIDs := make([]string, 0, len(live))
	oldValues := make([]string, 0, len(live))
	releasing := make([]existingIdentifier, 0, len(live))
	for _, e := range live {
		if _, ok := inBinding[e.NormalizedValue]; ok {
			// The "new" tag is one the animal already carries. Nothing to end for that value.
			continue
		}
		unbindIDs = append(unbindIDs, e.IdentifierID)
		oldValues = append(oldValues, e.NormalizedValue)
		releasing = append(releasing, e)
	}
	if len(unbindIDs) == 0 {
		return out, fmt.Errorf("animal %s already carries exactly this tag; there is nothing to replace: %w", req.GoatID, domain.ErrMappingConflict)
	}

	// END the old monitoring period FIRST, then START the new one. Order matters for the
	// tag_latest sync: if the two tags ever shared a value, the bind must win.
	// The release half of a replace is the SAME complete release an unmap performs -- rows this
	// module created are deleted, the animal's own identity rows are only unflagged. Anything
	// less and an animal accumulates a dead binding on every re-tag, and the second swap is
	// refused by the leftovers of the first.
	if err := releaseBinding(ctx, tx, tenantID, releasing); err != nil {
		return out, err
	}
	if err := syncTagLatestMonitoring(ctx, tx, tenantID, oldValues, nil); err != nil {
		return out, err
	}

	mappedAt := time.Now().UTC()
	ids, err := claimValues(ctx, tx, tenantID, req.GoatID, identifierType, rawByNorm, values, mappedAt, mine)
	if err != nil {
		return out, err
	}
	effective, err := effectiveMappedAt(ctx, tx, tenantID, ids)
	if err != nil {
		return out, err
	}
	if err := syncTagLatestMonitoring(ctx, tx, tenantID, values, &effective); err != nil {
		return out, err
	}
	if err := tx.Commit(ctx); err != nil {
		return out, fmt.Errorf("commit replace tag mapping: %w", err)
	}

	return domain.TagMappingResponse{
		GoatID:               req.GoatID,
		TagID:                normTagID,
		TagMAC:               normTagMAC,
		IdentifierIDs:        ids,
		MappingState:         "mapped",
		MonitoringSince:      &effective,
		UnboundIdentifierIDs: unbindIDs,
	}, nil
}

// RecordGatewayHeartbeat implements ports.Repository.
//
// A heartbeat is proof the GATEWAY is alive, which is a different fact from "we heard a tag".
// Both advance last_seen_at; only this advances last_heartbeat_at, which is what lets an
// operator tell "gateway up but hearing no tags" from "gateway down".
//
// ticks_cnt going BACKWARDS is a reboot -- the same counter-reset discipline the motion counter
// and pkt_sn already use: re-anchor and count the reboot, never record a negative.
func (r *Repository) RecordGatewayHeartbeat(ctx context.Context, tenantID string, req domain.GatewayHeartbeatRequest, at time.Time) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	// The PREVIOUS ticks_cnt is read (and locked) before the upsert rather than derived from a
	// RETURNING clause: inside ON CONFLICT DO UPDATE, a table-qualified column in RETURNING is
	// the NEW row, so a RETURNING-based comparison would compare the incoming value against
	// itself and never see a reboot. An integration test caught exactly that.
	var previousTicks *int64
	err = tx.QueryRow(ctx, `
		SELECT last_ticks_cnt FROM public.herd_signal_gateways
		WHERE tenant_id = $1::uuid AND gateway_id = $2
		FOR UPDATE
	`, tenantID, req.GatewayID).Scan(&previousTicks)
	if err != nil && err != pgx.ErrNoRows {
		return false, fmt.Errorf("lock gateway for heartbeat: %w", err)
	}

	rebootDetected := req.TicksCnt != nil && previousTicks != nil && *req.TicksCnt < *previousTicks
	rebootIncrement := 0
	if rebootDetected {
		rebootIncrement = 1
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO public.herd_signal_gateways (
			tenant_id, gateway_id, status, last_seen_at, last_heartbeat_at, last_ticks_cnt,
			heartbeat_reboot_count, updated_at
		) VALUES ($1::uuid, $2, 'active', $3, $3, $4::bigint, $5, now())
		ON CONFLICT (tenant_id, gateway_id) DO UPDATE
		SET last_seen_at = GREATEST(public.herd_signal_gateways.last_seen_at, $3),
		    last_heartbeat_at = $3,
		    last_ticks_cnt = COALESCE($4::bigint, public.herd_signal_gateways.last_ticks_cnt),
		    heartbeat_reboot_count = public.herd_signal_gateways.heartbeat_reboot_count + $5,
		    updated_at = now()
	`, tenantID, req.GatewayID, at, req.TicksCnt, rebootIncrement); err != nil {
		return false, fmt.Errorf("record gateway heartbeat: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit gateway heartbeat: %w", err)
	}
	return rebootDetected, nil
}
