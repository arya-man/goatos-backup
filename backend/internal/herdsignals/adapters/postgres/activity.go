package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
)

// GetTagActivityScope resolves one tag to the animal behind it, that animal's shed, and the
// instant the tag became that animal's tag.
//
// The monitoring boundary is read from goat_identifiers.smart_tag_mapped_at FIRST, because the
// identifier carries the authoritative stamp (migration 000197: mapping is an identifier-level
// fact -- an animal can carry several identifiers, and re-tagging starts a NEW monitoring period
// for the new tag while the old one stops). herd_signal_tag_latest.animal_monitoring_since is
// the denormalised copy and is used only as a fallback, so a row that has not been backfilled
// yet still resolves rather than silently reporting "no boundary".
func (r *Repository) GetTagActivityScope(ctx context.Context, tenantID, tagID string) (*ports.TagActivityScope, error) {
	const query = `
		SELECT tl.tag_id,
		       COALESCE(tl.tag_mac, ''),
		       COALESCE(mapped.goat_id::text, ''),
		       COALESCE(g.shed_id::text, ''),
		       COALESCE(shed_loc.name, ''),
		       COALESCE(park_loc.name, ''),
		       COALESCE(mapped.smart_tag_mapped_at, tl.animal_monitoring_since)
		FROM public.herd_signal_tag_latest tl
		LEFT JOIN LATERAL (
			SELECT gi.goat_id, gi.smart_tag_mapped_at
			FROM public.goat_identifiers gi
			WHERE gi.tenant_id = tl.tenant_id
			  AND gi.status = 'active' AND gi.smart_tag_capable IS TRUE
			  AND gi.normalized_value IN (UPPER(BTRIM(tl.tag_id)), UPPER(BTRIM(COALESCE(tl.tag_mac, ''))))
			LIMIT 1
		) mapped ON true
		LEFT JOIN public.goats g ON g.tenant_id = tl.tenant_id AND g.goat_id = mapped.goat_id
		LEFT JOIN public.locations shed_loc ON shed_loc.tenant_id = tl.tenant_id AND shed_loc.location_id = g.shed_id
		LEFT JOIN public.locations park_loc ON park_loc.tenant_id = tl.tenant_id AND park_loc.location_id = g.park_id
		WHERE tl.tenant_id = $1 AND tl.tag_id = $2
	`
	var scope ports.TagActivityScope
	err := r.db.QueryRow(ctx, query, tenantID, tagID).Scan(
		&scope.TagID, &scope.TagMAC, &scope.GoatID, &scope.ShedID,
		&scope.ShedName, &scope.ParkName, &scope.MonitoringSince,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("herd signals tag activity scope: %w", err)
	}
	return &scope, nil
}

// ListFarmActivity reads the farm records that may be shown beside this tag's movement history.
//
// SHAPE: one small, indexed, tenant-scoped, time-bounded, LIMITed query PER SOURCE, merged and
// sorted in Go. Deliberately NOT one cross-module CTE (AGENTS.md scale anti-patterns): the six
// sources have six different grains and six different indexes, and a single join would both
// plan badly and blur the fact that a feed row is about a shed while a weighing row is about a
// scanned string.
//
// Every query is bounded by `from`/`to`, which the caller has ALREADY clamped to the monitoring
// boundary -- there is no code path here that can read a record from before this tag was mapped
// to this animal.
func (r *Repository) ListFarmActivity(ctx context.Context, tenantID string, scope ports.TagActivityScope, from, to time.Time, limit int) ([]domain.ActivityEvent, error) {
	if scope.GoatID == "" {
		// No animal behind the tag: there is nothing to attribute. The caller reports this as a
		// reason, not as an error.
		return nil, nil
	}
	if limit <= 0 {
		return nil, nil
	}

	events := make([]domain.ActivityEvent, 0, limit)

	collect := func(kind domain.ActivityEventKind, grain domain.ActivityGrain, label func(name *string) string, query string, args ...any) error {
		rows, err := r.db.Query(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("herd signals activity %s: %w", kind, err)
		}
		defer rows.Close()
		for rows.Next() {
			var at time.Time
			var name *string
			if err := rows.Scan(&at, &name); err != nil {
				return fmt.Errorf("herd signals activity %s scan: %w", kind, err)
			}
			events = append(events, domain.ActivityEvent{Kind: kind, At: at, Label: label(name), Grain: grain})
		}
		return rows.Err()
	}

	// Vaccination -- ANIMAL grain. Accepted completions only: a recorded-but-rejected capture is
	// not a farm record of anything having happened.
	// Index: vaccination_completions_goat_history_idx (tenant_id, goat_id, administered_at DESC).
	if err := collect(domain.ActivityKindVaccination, domain.GrainAnimal,
		func(*string) string { return "Vaccination recorded" }, `
		SELECT vc.administered_at, NULL::text
		FROM public.vaccination_completions vc
		WHERE vc.tenant_id = $1 AND vc.goat_id = $2::uuid
		  AND vc.status = 'accepted'
		  AND vc.administered_at >= $3 AND vc.administered_at <= $4
		ORDER BY vc.administered_at
		LIMIT $5
	`, tenantID, scope.GoatID, from, to, limit+1); err != nil {
		return nil, err
	}

	// Treatment -- ANIMAL grain. A health treatment session that was actually completed. This is
	// a record a person made; it is never a diagnosis this module produced and never something
	// inferred from movement.
	// Index: health_sessions_goat_open_idx (tenant_id, goat_id, ...).
	if err := collect(domain.ActivityKindTreatment, domain.GrainAnimal,
		func(*string) string { return "Treatment session recorded" }, `
		SELECT hts.completed_at, NULL::text
		FROM public.health_treatment_sessions hts
		WHERE hts.tenant_id = $1 AND hts.goat_id = $2::uuid
		  AND hts.completed_at IS NOT NULL
		  AND hts.completed_at >= $3 AND hts.completed_at <= $4
		ORDER BY hts.completed_at
		LIMIT $5
	`, tenantID, scope.GoatID, from, to, limit+1); err != nil {
		return nil, err
	}

	// Shed move -- ANIMAL grain. The animal's own recorded location history.
	// Index: goat_location_history_goat_timeline_idx (goat_id, occurred_at DESC).
	if err := collect(domain.ActivityKindShedMove, domain.GrainAnimal,
		func(name *string) string {
			if name != nil && strings.TrimSpace(*name) != "" {
				return "Moved to " + *name
			}
			return "Pen move recorded"
		}, `
		SELECT glh.occurred_at, to_loc.name
		FROM public.goat_location_history glh
		LEFT JOIN public.locations to_loc
		  ON to_loc.tenant_id = glh.tenant_id AND to_loc.location_id = glh.to_location_id
		WHERE glh.tenant_id = $1 AND glh.goat_id = $2::uuid
		  AND glh.occurred_at >= $3 AND glh.occurred_at <= $4
		ORDER BY glh.occurred_at
		LIMIT $5
	`, tenantID, scope.GoatID, from, to, limit+1); err != nil {
		return nil, err
	}

	// Feed given -- SHED grain, and the response says so. The record is that feed was directed
	// to the shed this animal is in. It is NOT an observation of this animal, and no client may
	// render it as one.
	// Index: feed_direction_completions_shed_history_idx (tenant_id, shed_id, fed_at DESC).
	if scope.ShedID != "" {
		shedLabel := "Feed given to this pen"
		if strings.TrimSpace(scope.ShedName) != "" {
			shedLabel = "Feed given to pen " + scope.ShedName
		}
		if err := collect(domain.ActivityKindFeedGiven, domain.GrainShed,
			func(*string) string { return shedLabel }, `
			SELECT fdc.fed_at, NULL::text
			FROM public.feed_direction_completions fdc
			WHERE fdc.tenant_id = $1 AND fdc.shed_id = $2::uuid
			  AND fdc.status IN ('recorded', 'accepted')
			  AND fdc.fed_at >= $3 AND fdc.fed_at <= $4
			ORDER BY fdc.fed_at
			LIMIT $5
		`, tenantID, scope.ShedID, from, to, limit+1); err != nil {
			return nil, err
		}
	}

	// Weighing -- SCANNED-IDENTIFIER grain. Weighing is free-flow and ISOLATED: migration 000078
	// dropped weighing_observations.animal_id outright and no path in either direction resolves
	// a scan to an animal. This correlates the RAW scanned string against this tag's own id/MAC,
	// exactly as the insights card does, with NO goat_identifiers join anywhere -- reintroducing
	// that resolution from the herd-signals side would be the same banned join wearing a
	// different hat.
	// Index: weighing_observations_export_window_idx (tenant_id, accepted_at DESC) INCLUDE
	// (..., scanned_identifier, ...).
	tagValues := normalizedTagValues(scope)
	if len(tagValues) > 0 {
		if err := collect(domain.ActivityKindWeighing, domain.GrainScannedIdentifier,
			func(*string) string { return "Weight recorded" }, `
			SELECT wo.accepted_at, NULL::text
			FROM public.weighing_observations wo
			WHERE wo.tenant_id = $1
			  AND wo.accepted_at >= $2 AND wo.accepted_at <= $3
			  AND UPPER(BTRIM(wo.scanned_identifier)) = ANY($4::text[])
			ORDER BY wo.accepted_at
			LIMIT $5
		`, tenantID, from, to, tagValues, limit+1); err != nil {
			return nil, err
		}

		// Hoof trimming -- SCANNED-IDENTIFIER grain, same discipline as weighing: PC-care records
		// a raw scanned string per animal row (pc_care_task_animals.scanned_identifier) and this
		// read never resolves it to an animal either.
		// Driven from pc_care_tasks (indexed on tenant_id, category, ... , planned_business_date;
		// one row per category per pen per day, so the task side is small) and joined to its
		// animal rows on (tenant_id, task_id).
		if err := collect(domain.ActivityKindHoofTrimming, domain.GrainScannedIdentifier,
			func(*string) string { return "Hoof trimming recorded" }, `
			SELECT pca.scanned_at, NULL::text
			FROM public.pc_care_tasks pct
			JOIN public.pc_care_task_animals pca
			  ON pca.tenant_id = pct.tenant_id AND pca.task_id = pct.task_id
			WHERE pct.tenant_id = $1
			  AND pct.category = 'hoof_trimming'
			  AND pct.planned_business_date BETWEEN ($2::timestamptz - interval '2 days')::date
			                                    AND ($3::timestamptz + interval '2 days')::date
			  AND pca.scanned_at >= $2 AND pca.scanned_at <= $3
			  AND UPPER(BTRIM(pca.scanned_identifier)) = ANY($4::text[])
			ORDER BY pca.scanned_at
			LIMIT $5
		`, tenantID, from, to, tagValues, limit+1); err != nil {
			return nil, err
		}
	}

	sort.SliceStable(events, func(i, j int) bool {
		if events[i].At.Equal(events[j].At) {
			return events[i].Kind < events[j].Kind
		}
		return events[i].At.Before(events[j].At)
	})

	// limit+1 rows survive so the caller can say "there are more" honestly rather than showing a
	// silently clipped window as if it were the whole one.
	if len(events) > limit+1 {
		events = events[:limit+1]
	}
	return events, nil
}

// normalizedTagValues is the tag's id and MAC in the SAME canonical form the scanned-identifier
// sources are compared in (UPPER(BTRIM(...))). Kept as a helper so the two scanned-identifier
// queries can never normalize differently from each other.
func normalizedTagValues(scope ports.TagActivityScope) []string {
	values := make([]string, 0, 2)
	seen := map[string]struct{}{}
	for _, raw := range []string{scope.TagID, scope.TagMAC} {
		v := strings.ToUpper(strings.TrimSpace(raw))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		values = append(values, v)
	}
	return values
}
