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
// instant animal monitoring starts (migration 000196); herd_signal_tag_latest
// .animal_monitoring_since is its denormalised copy on the hot read path. Packets before it are
// device telemetry -- a tag rattling in a box on a bench -- and must never reach an animal's
// baseline, pattern window, or correlation. A REPLACE starts a NEW period for the new tag and
// ends the old one.

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
}

// lockIdentifiersByValue locks every row in the tenant holding any of these normalized values.
// The lock matters: goat_identifiers_lifetime_value_unique is UNIQUE (tenant_id,
// normalized_value) for the LIFETIME of the value (retired rows included), so two concurrent
// binds of the same tag must serialise here rather than race into a constraint violation.
func lockIdentifiersByValue(ctx context.Context, tx pgx.Tx, tenantID string, values []string) ([]existingIdentifier, error) {
	rows, err := tx.Query(ctx, `
		SELECT identifier_id::text, goat_id::text, normalized_value, status, smart_tag_capable
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
		if err := rows.Scan(&e.IdentifierID, &e.GoatID, &e.NormalizedValue, &e.Status, &e.SmartTagCapable); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// liveSmartTagsForGoat returns the animal's currently-bound smart tag identifiers.
func liveSmartTagsForGoat(ctx context.Context, tx pgx.Tx, tenantID, goatID string) ([]existingIdentifier, error) {
	rows, err := tx.Query(ctx, `
		SELECT identifier_id::text, goat_id::text, normalized_value, status, smart_tag_capable
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
		if err := rows.Scan(&e.IdentifierID, &e.GoatID, &e.NormalizedValue, &e.Status, &e.SmartTagCapable); err != nil {
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
			var id string
			if err := tx.QueryRow(ctx, `
				UPDATE public.goat_identifiers
				SET smart_tag_capable = true,
				    smart_tag_mapped_at = COALESCE(smart_tag_mapped_at, $3),
				    updated_at = now()
				WHERE tenant_id = $1::uuid AND identifier_id = $2::uuid
				RETURNING identifier_id::text
			`, tenantID, e.IdentifierID, mappedAt).Scan(&id); err != nil {
				return nil, fmt.Errorf("mark existing identifier %s smart-tag capable: %w", e.IdentifierID, err)
			}
			ids = append(ids, id)
			continue
		}
		raw := rawByNorm[v]
		if raw == "" {
			raw = v
		}
		var id string
		if err := tx.QueryRow(ctx, `
			INSERT INTO public.goat_identifiers (
				tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
				scope_key, is_primary_for_goat, status, valid_from, normalizer_version,
				smart_tag_capable, smart_tag_mapped_at
			) VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, false, 'active', $7, $8, true, $7)
			RETURNING identifier_id::text
		`, tenantID, goatID, identifierType, raw, v, domain.SmartTagScopeKey, mappedAt, domain.SmartTagNormalizerVersion).Scan(&id); err != nil {
			return nil, fmt.Errorf("claim identifier value %q: %w", v, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// unbindIdentifiers ends the smart-tag binding on these identifiers WITHOUT retiring the
// identity row itself. That distinction is deliberate: the row may also be the animal's ordinary
// ear-tag identity, and a tag that fell off is still a physical object whose value stays claimed
// for the tenant's lifetime (goat_identifiers_lifetime_value_unique). Clearing
// smart_tag_capable is exactly what makes the read path stop resolving the tag to this animal,
// which is the whole requirement; clearing smart_tag_mapped_at is what makes NULL mean
// "unmapped: device telemetry only" again.
func unbindIdentifiers(ctx context.Context, tx pgx.Tx, tenantID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE public.goat_identifiers
		SET smart_tag_capable = false, smart_tag_mapped_at = NULL, updated_at = now()
		WHERE tenant_id = $1::uuid AND identifier_id = ANY($2::uuid[])
	`, tenantID, ids); err != nil {
		return fmt.Errorf("unbind identifiers: %w", err)
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
		if e.Status != "active" {
			// The value is claimed for the tenant's lifetime by a non-active row. Reactivating a
			// retired identity is an identity-module decision, not a herd-signals one, so this
			// refuses loudly instead of quietly resurrecting it.
			return out, fmt.Errorf("tag value %q is held by a %s identifier on this animal; it must be reactivated through the identity module before it can carry a smart tag: %w", e.NormalizedValue, e.Status, domain.ErrMappingConflict)
		}
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
// It clears smart_tag_capable and smart_tag_mapped_at on the identifiers holding this tag's
// value(s) WITHOUT retiring the identity row -- the row may also be the animal's ordinary ear-tag
// identity, and the physical value stays claimed for the tenant's lifetime either way. Clearing
// the flag is exactly what makes the read path stop resolving the tag to this animal; clearing
// the stamp is what makes NULL mean "device telemetry only" again. The tag's packets keep
// flowing and its stored history stays intact.
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

	existing, err := lockIdentifiersByValue(ctx, tx, tenantID, values)
	if err != nil {
		return out, err
	}
	ids := make([]string, 0, len(existing))
	goatID := ""
	for _, e := range existing {
		if e.Status != "active" || e.SmartTagCapable == nil || !*e.SmartTagCapable {
			continue
		}
		ids = append(ids, e.IdentifierID)
		goatID = e.GoatID
	}
	if len(ids) == 0 {
		// Unmapping something that is not mapped is a caller mistake worth naming, not a silent
		// no-op: the operator believes they just released a binding.
		return out, fmt.Errorf("tag %q is not mapped to an animal: %w", normTagID, domain.ErrMappingConflict)
	}

	if err := unbindIdentifiers(ctx, tx, tenantID, ids); err != nil {
		return out, err
	}
	if err := syncTagLatestMonitoring(ctx, tx, tenantID, values, nil); err != nil {
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
		if e.Status != "active" {
			return out, fmt.Errorf("new tag value %q is held by a %s identifier on this animal; it must be reactivated through the identity module first: %w", e.NormalizedValue, e.Status, domain.ErrMappingConflict)
		}
		mine = append(mine, e)
	}

	inBinding := make(map[string]struct{}, len(values))
	for _, v := range values {
		inBinding[v] = struct{}{}
	}
	unbindIDs := make([]string, 0, len(live))
	oldValues := make([]string, 0, len(live))
	for _, e := range live {
		if _, ok := inBinding[e.NormalizedValue]; ok {
			// The "new" tag is one the animal already carries. Nothing to end for that value.
			continue
		}
		unbindIDs = append(unbindIDs, e.IdentifierID)
		oldValues = append(oldValues, e.NormalizedValue)
	}
	if len(unbindIDs) == 0 {
		return out, fmt.Errorf("animal %s already carries exactly this tag; there is nothing to replace: %w", req.GoatID, domain.ErrMappingConflict)
	}

	// END the old monitoring period FIRST, then START the new one. Order matters for the
	// tag_latest sync: if the two tags ever shared a value, the bind must win.
	if err := unbindIdentifiers(ctx, tx, tenantID, unbindIDs); err != nil {
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
