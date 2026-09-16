package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/alerts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/platform/outbox"
)

// ---- user-defined event rules ---------------------------------------------------------

const eventRuleIdempotencyScope = "alerts.event_rule"

// Package-level SQL so the scale guard and a query-plan test can name each statement.
const listEventRulesSQL = `
SELECT r.alert_event_rule_id::text, r.label, r.event_kind, r.severity, r.enabled,
       COALESCE(creator.display_name, ''), COALESCE(setter.display_name, ''),
       to_char(r.updated_at AT TIME ZONE 'Asia/Kolkata', 'DD Mon YYYY, HH24:MI') || ' IST'
FROM alert_event_rules r
LEFT JOIN LATERAL (
  SELECT wm.display_name FROM workforce_members wm
  WHERE wm.tenant_id = r.tenant_id AND (wm.workforce_member_id = r.created_by OR wm.user_id = r.created_by)
  ORDER BY (wm.workforce_member_id = r.created_by) DESC, (wm.status = 'active') DESC, wm.updated_at DESC, wm.workforce_member_id
  LIMIT 1
) creator ON true
LEFT JOIN LATERAL (
  SELECT wm.display_name FROM workforce_members wm
  WHERE wm.tenant_id = r.tenant_id AND (wm.workforce_member_id = r.updated_by OR wm.user_id = r.updated_by)
  ORDER BY (wm.workforce_member_id = r.updated_by) DESC, (wm.status = 'active') DESC, wm.updated_at DESC, wm.workforce_member_id
  LIMIT 1
) setter ON true
WHERE r.tenant_id = $1::uuid
ORDER BY r.created_at, r.alert_event_rule_id`

const insertEventRuleSQL = `
INSERT INTO alert_event_rules (alert_event_rule_id, tenant_id, label, event_kind, severity, enabled, created_by, updated_by)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7::uuid, $7::uuid)
RETURNING alert_event_rule_id::text`

const updateEventRuleSQL = `
UPDATE alert_event_rules
SET label = $3, event_kind = $4, severity = $5, enabled = $6, updated_by = $7::uuid, updated_at = now()
WHERE tenant_id = $1::uuid AND alert_event_rule_id = $2::uuid
RETURNING alert_event_rule_id::text`

const deleteEventRuleSQL = `
DELETE FROM alert_event_rules WHERE tenant_id = $1::uuid AND alert_event_rule_id = $2::uuid`

// ListEventRules reads the tenant's composed alerts; a row naming a kind the catalog no longer
// carries is dropped so config can never invent a reader.
func (r *Repository) ListEventRules(ctx context.Context, tenantID string) ([]domain.EventRule, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, listEventRulesSQL, tenantID)
	if err != nil {
		return nil, fmt.Errorf("alerts: list event rules: %w", err)
	}
	defer rows.Close()
	out := []domain.EventRule{}
	for rows.Next() {
		var er domain.EventRule
		var kind, sev string
		if err := rows.Scan(&er.ID, &er.Label, &kind, &sev, &er.Enabled, &er.CreatedBy, &er.UpdatedBy, &er.UpdatedAt); err != nil {
			return nil, fmt.Errorf("alerts: event rule scan: %w", err)
		}
		info, ok := domain.EventKindByKey(domain.EventKind(kind))
		if !ok {
			continue
		}
		er.Kind, er.KindLabel, er.Severity = info.Key, info.Label, domain.Severity(sev)
		out = append(out, er)
	}
	return out, rows.Err()
}

// UpsertEventRule creates or updates one rule under an idempotency reservation.
func (r *Repository) UpsertEventRule(ctx context.Context, in domain.SetEventRule) (domain.EventRule, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.EventRule{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	fingerprint := requestFingerprint(in.ID, strings.TrimSpace(in.Label), string(in.Kind), string(in.Severity), strconv.FormatBool(in.Enabled))
	reservation, err := reserveIdempotency(ctx, tx, in.TenantID, eventRuleIdempotencyScope, in.IdempotencyKey, fingerprint)
	if err != nil {
		return domain.EventRule{}, err
	}
	var actor any
	if strings.TrimSpace(in.ActorID) != "" {
		actor = in.ActorID
	}
	id := strings.TrimSpace(in.ID)
	if !reservation.proceed {
		// Exact replay: the original write stands. A create replay reads back the row it made.
		if id == "" {
			id = reservation.resultID
		}
	} else if id == "" {
		// A NEW rule's id is derived from the idempotency key, so a retried create makes one row.
		id = outbox.DeterministicUUID("alert_event_rule:" + in.TenantID + ":" + in.IdempotencyKey)
		if err := tx.QueryRow(ctx, insertEventRuleSQL, id, in.TenantID, strings.TrimSpace(in.Label), string(in.Kind), string(in.Severity), in.Enabled, actor).Scan(&id); err != nil {
			return domain.EventRule{}, fmt.Errorf("alerts: create event rule: %w", err)
		}
	} else {
		if err := tx.QueryRow(ctx, updateEventRuleSQL, in.TenantID, id, strings.TrimSpace(in.Label), string(in.Kind), string(in.Severity), in.Enabled, actor).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.EventRule{}, domain.ErrEventRuleNotFound
			}
			return domain.EventRule{}, fmt.Errorf("alerts: update event rule: %w", err)
		}
	}
	if reservation.proceed {
		if err := completeIdempotency(ctx, tx, in.TenantID, eventRuleIdempotencyScope, in.IdempotencyKey, "alert_event_rule", id); err != nil {
			return domain.EventRule{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.EventRule{}, err
	}
	rules, err := r.ListEventRules(ctx, in.TenantID)
	if err != nil {
		return domain.EventRule{}, err
	}
	for _, rule := range rules {
		if rule.ID == id {
			return rule, nil
		}
	}
	return domain.EventRule{}, domain.ErrEventRuleNotFound
}

// DeleteEventRule removes one rule; deleting a rule the tenant does not have is not found.
func (r *Repository) DeleteEventRule(ctx context.Context, tenantID, ruleID string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.pool.Exec(ctx, deleteEventRuleSQL, tenantID, ruleID)
	if err != nil {
		return fmt.Errorf("alerts: delete event rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrEventRuleNotFound
	}
	return nil
}

// ---- event readers, one per catalog kind ----------------------------------------------
//
// Each reads the module that OWNS the fact for one park and one Asia/Kolkata business day and
// hands back worded rows. Every animal is named by its RFID tag (goat_identifiers
// animal_identifier_1, active), never a goat id; a pen is (shed name, partition label) for
// oploc.Display downstream.

// goatTagSQL is the LATERAL that names an animal by its primary active tag.
const goatTagSQL = `
LEFT JOIN LATERAL (
  SELECT gi.identifier_value AS tag FROM goat_identifiers gi
  WHERE gi.tenant_id = g.tenant_id AND gi.goat_id = g.goat_id AND gi.status = 'active'
    AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
  ORDER BY (gi.identifier_type = 'animal_identifier_1') DESC, gi.is_primary_for_goat DESC, gi.created_at
  LIMIT 1
) tag ON true`

// goatPenSQL is the LATERAL that names an animal's pen.
const goatPenSQL = `
LEFT JOIN locations shed ON shed.tenant_id = g.tenant_id AND shed.location_id = g.shed_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id`

// projection-review: membership=goat_births rows whose child is a live register row in the park, born on the business day (Asia/Kolkata); group_key=child goat id, one row per kid; join_cardinality=goats 1:1 on child, mother tag / child tag / pen are 1:{0,1} LATERAL or PK lookups, so no fan-out; pagination=25-row preview before enrichment plus snapshot total, one park-day; scope=tenant, park, business day, all bound.
const birthEventsSQL = `
WITH matched AS MATERIALIZED (
  SELECT gb.tenant_id, gb.child_goat_id, gb.mother_goat_id, gb.litter_size, gb.created_at, count(*) OVER () AS event_total
  FROM goat_births gb JOIN goats g ON g.tenant_id = gb.tenant_id AND g.goat_id = gb.child_goat_id
  WHERE gb.tenant_id = $1::uuid AND g.park_id = $2::uuid AND gb.created_at >= ($3::date::timestamp AT TIME ZONE 'Asia/Kolkata') AND gb.created_at < (($3::date + 1)::timestamp AT TIME ZONE 'Asia/Kolkata')
  ORDER BY gb.created_at, gb.child_goat_id
  LIMIT 25
)
SELECT gb.event_total, gb.child_goat_id::text, COALESCE(tag.tag, ''), COALESCE(mtag.tag, ''), gb.litter_size,
       COALESCE(g.shed_id::text, ''), COALESCE(shed.name, ''), COALESCE(gsp.partition_label, '')
FROM matched gb
JOIN goats g ON g.tenant_id = gb.tenant_id AND g.goat_id = gb.child_goat_id
` + goatTagSQL + `
LEFT JOIN LATERAL (
  SELECT gi.identifier_value AS tag FROM goat_identifiers gi
  WHERE gi.tenant_id = gb.tenant_id AND gi.goat_id = gb.mother_goat_id AND gi.status = 'active'
    AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
  ORDER BY (gi.identifier_type = 'animal_identifier_1') DESC, gi.is_primary_for_goat DESC, gi.created_at
  LIMIT 1
) mtag ON true
` + goatPenSQL + `
ORDER BY gb.created_at, gb.child_goat_id`

// projection-review: membership=goats in the park whose exit was stamped on the business day (Asia/Kolkata) with the given lifecycle status (dead / sold); group_key=goat id; join_cardinality=tag and pen are 1:{0,1}; pagination=25-row preview before enrichment plus snapshot total, one park-day; scope=tenant, park, business day, status, all bound.
const goatExitEventsSQL = `
WITH matched AS MATERIALIZED (
  SELECT g.tenant_id, g.goat_id, g.park_id, g.shed_id, g.created_at, g.exited_at, g.exit_reason, g.breed, g.sex, g.origin_type, count(*) OVER () AS event_total
  FROM goats g
  WHERE g.tenant_id = $1::uuid AND g.park_id = $2::uuid AND g.lifecycle_status = $4 AND g.exited_at >= ($3::date::timestamp AT TIME ZONE 'Asia/Kolkata') AND g.exited_at < (($3::date + 1)::timestamp AT TIME ZONE 'Asia/Kolkata')
  ORDER BY g.exited_at, g.goat_id
  LIMIT 25
)
SELECT g.event_total, g.goat_id::text, COALESCE(tag.tag, ''), COALESCE(g.exit_reason, ''), COALESCE(g.breed, ''), COALESCE(g.sex, ''),
       COALESCE(g.shed_id::text, ''), COALESCE(shed.name, ''), COALESCE(gsp.partition_label, '')
FROM matched g
` + goatTagSQL + goatPenSQL + `
ORDER BY g.exited_at, g.goat_id`

// projection-review: membership=goats created in the park on the business day (Asia/Kolkata) whose origin is not a birth; group_key=goat id; join_cardinality=tag and pen are 1:{0,1}; pagination=25-row preview before enrichment plus snapshot total, one park-day; scope=tenant, park, business day, all bound.
const goatAddedEventsSQL = `
WITH matched AS MATERIALIZED (
  SELECT g.tenant_id, g.goat_id, g.park_id, g.shed_id, g.created_at, g.exited_at, g.exit_reason, g.breed, g.sex, g.origin_type, count(*) OVER () AS event_total
  FROM goats g
  WHERE g.tenant_id = $1::uuid AND g.park_id = $2::uuid AND COALESCE(g.origin_type, '') <> 'birth' AND g.created_at >= ($3::date::timestamp AT TIME ZONE 'Asia/Kolkata') AND g.created_at < (($3::date + 1)::timestamp AT TIME ZONE 'Asia/Kolkata')
  ORDER BY g.created_at, g.goat_id
  LIMIT 25
)
SELECT g.event_total, g.goat_id::text, COALESCE(tag.tag, ''), COALESCE(g.origin_type, ''), COALESCE(g.breed, ''), COALESCE(g.sex, ''),
       COALESCE(g.shed_id::text, ''), COALESCE(shed.name, ''), COALESCE(gsp.partition_label, '')
FROM matched g
` + goatTagSQL + goatPenSQL + `
ORDER BY g.created_at, g.goat_id`

// projection-review: membership=shifting_events touching the park (source or destination) whose raised_at ($4 = raised) or authorized_at ($4 = approved) falls on the business day (Asia/Kolkata), not rejected/canceled; group_key=shifting_event_id; join_cardinality=impacts pre-aggregated to one head count per event, shed names are PK lookups; pagination=25-row preview before enrichment plus snapshot total, one park-day; scope=tenant, park, business day, stamp kind, all bound.
const shiftingEventsSQL = `
WITH matched AS MATERIALIZED (
  SELECT se.tenant_id, se.shifting_event_id, se.source_shed_id, se.destination_shed_id, se.source_partition_label, se.destination_partition_label, se.event_status, se.priority, se.raised_at, count(*) OVER () AS event_total
  FROM shifting_events se
  WHERE se.tenant_id = $1::uuid AND (se.destination_park_id = $2::uuid OR se.source_park_id = $2::uuid) AND se.event_status NOT IN ('rejected', 'canceled') AND (($4 = 'raised' AND se.raised_at >= ($3::date::timestamp AT TIME ZONE 'Asia/Kolkata') AND se.raised_at < (($3::date + 1)::timestamp AT TIME ZONE 'Asia/Kolkata')) OR ($4 = 'approved' AND se.authorized_at >= ($3::date::timestamp AT TIME ZONE 'Asia/Kolkata') AND se.authorized_at < (($3::date + 1)::timestamp AT TIME ZONE 'Asia/Kolkata')))
  ORDER BY se.raised_at, se.shifting_event_id
  LIMIT 25
)
SELECT se.event_total, se.shifting_event_id::text, COALESCE(hc.head_count, 0)::bigint, se.event_status, se.priority,
       COALESCE(src.name, ''), COALESCE(se.source_partition_label, ''),
       se.destination_shed_id::text, COALESCE(dst.name, ''), COALESCE(se.destination_partition_label, '')
FROM matched se
LEFT JOIN (
  SELECT shifting_event_id, sum(head_count) AS head_count FROM shifting_event_impacts
  WHERE tenant_id = $1::uuid AND shifting_event_id IN (SELECT shifting_event_id FROM matched) GROUP BY shifting_event_id
) hc ON hc.shifting_event_id = se.shifting_event_id
-- Name lookups by id for the two pens of an existing movement (not a selectable catalog).
LEFT JOIN locations src ON src.tenant_id = se.tenant_id AND src.location_id = se.source_shed_id
LEFT JOIN locations dst ON dst.tenant_id = se.tenant_id
  AND dst.location_id = se.destination_shed_id
ORDER BY se.raised_at, se.shifting_event_id`

// projection-review: membership=feed_purchases for the park dated the business day; group_key=feed_purchase_id; join_cardinality=none; pagination=25-row preview before enrichment plus snapshot total, one park-day; scope=tenant, park, purchase date, all bound.
const feedPurchaseEventsSQL = `
WITH matched AS MATERIALIZED (
  SELECT fp.feed_purchase_id, fp.feed_item_label, fp.quantity_kg, fp.vendor, fp.delivery_status, fp.created_at, count(*) OVER () AS event_total
  FROM feed_purchases fp
  WHERE fp.tenant_id = $1::uuid AND fp.park_id = $2::uuid AND fp.purchase_date = $3::date
  ORDER BY fp.created_at, fp.feed_purchase_id
  LIMIT 25
)
SELECT fp.event_total, fp.feed_purchase_id::text, fp.feed_item_label, fp.quantity_kg::float8, COALESCE(fp.vendor, ''), COALESCE(fp.delivery_status, '')
FROM matched fp
ORDER BY fp.created_at, fp.feed_purchase_id`

// Events reads one kind for a park-day.
func (r *Repository) Events(ctx context.Context, tenantID, parkID string, kind domain.EventKind, businessDate string) (domain.EventPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	switch kind {
	case domain.EventBirthRecorded:
		return r.birthEvents(ctx, tenantID, parkID, businessDate)
	case domain.EventDeathRecorded:
		return r.exitEvents(ctx, tenantID, parkID, businessDate, "dead", "Recorded dead", "/counts/breakdown")
	case domain.EventAnimalSold:
		return r.exitEvents(ctx, tenantID, parkID, businessDate, "sold", "Sold", "/sales/sold")
	case domain.EventAnimalAdded:
		return r.addedEvents(ctx, tenantID, parkID, businessDate)
	case domain.EventShiftingRaised:
		return r.shiftingEvents(ctx, tenantID, parkID, businessDate, "raised")
	case domain.EventShiftingApproved:
		return r.shiftingEvents(ctx, tenantID, parkID, businessDate, "approved")
	case domain.EventFeedPurchaseRecorded:
		return r.feedPurchaseEvents(ctx, tenantID, parkID, businessDate)
	default:
		return domain.EventPage{}, fmt.Errorf("alerts: event kind %q has no reader", kind)
	}
}

func (r *Repository) birthEvents(ctx context.Context, tenantID, parkID, day string) (domain.EventPage, error) {
	rows, err := r.pool.Query(ctx, birthEventsSQL, tenantID, parkID, day)
	if err != nil {
		return domain.EventPage{}, fmt.Errorf("alerts: birth events: %w", err)
	}
	defer rows.Close()
	out := domain.EventPage{Rows: []domain.Event{}}
	for rows.Next() {
		var e domain.Event
		var tag, mother string
		var litter int
		if err := rows.Scan(&out.Total, &e.Key, &tag, &mother, &litter, &e.ShedID, &e.ShedName, &e.PartitionLabel); err != nil {
			return domain.EventPage{}, fmt.Errorf("alerts: birth events scan: %w", err)
		}
		e.ParkID = parkID
		e.Subject = animalSubject(tag)
		detail := fmt.Sprintf("Litter of %d.", litter)
		if mother != "" {
			detail = fmt.Sprintf("Litter of %d, mother %s.", litter, mother)
		}
		e.Detail = detail
		e.Href = "/counts/breakdown"
		out.Rows = append(out.Rows, e)
	}
	return out, rows.Err()
}

func (r *Repository) exitEvents(ctx context.Context, tenantID, parkID, day, status, verb, href string) (domain.EventPage, error) {
	rows, err := r.pool.Query(ctx, goatExitEventsSQL, tenantID, parkID, day, status)
	if err != nil {
		return domain.EventPage{}, fmt.Errorf("alerts: %s events: %w", status, err)
	}
	defer rows.Close()
	out := domain.EventPage{Rows: []domain.Event{}}
	for rows.Next() {
		var e domain.Event
		var tag, reason, breed, sex string
		if err := rows.Scan(&out.Total, &e.Key, &tag, &reason, &breed, &sex, &e.ShedID, &e.ShedName, &e.PartitionLabel); err != nil {
			return domain.EventPage{}, fmt.Errorf("alerts: %s events scan: %w", status, err)
		}
		e.ParkID = parkID
		e.Subject = animalSubject(tag)
		e.Detail = strings.TrimSpace(fmt.Sprintf("%s. %s", verb, describeAnimal(breed, sex)))
		e.Href = href
		out.Rows = append(out.Rows, e)
	}
	return out, rows.Err()
}

func (r *Repository) addedEvents(ctx context.Context, tenantID, parkID, day string) (domain.EventPage, error) {
	rows, err := r.pool.Query(ctx, goatAddedEventsSQL, tenantID, parkID, day)
	if err != nil {
		return domain.EventPage{}, fmt.Errorf("alerts: added events: %w", err)
	}
	defer rows.Close()
	out := domain.EventPage{Rows: []domain.Event{}}
	for rows.Next() {
		var e domain.Event
		var tag, origin, breed, sex string
		if err := rows.Scan(&out.Total, &e.Key, &tag, &origin, &breed, &sex, &e.ShedID, &e.ShedName, &e.PartitionLabel); err != nil {
			return domain.EventPage{}, fmt.Errorf("alerts: added events scan: %w", err)
		}
		e.ParkID = parkID
		e.Subject = animalSubject(tag)
		how := "Joined the register"
		if origin == "procured" {
			how = "Purchased"
		}
		e.Detail = strings.TrimSpace(fmt.Sprintf("%s. %s", how, describeAnimal(breed, sex)))
		e.Href = "/counts/breakdown"
		out.Rows = append(out.Rows, e)
	}
	return out, rows.Err()
}

func (r *Repository) shiftingEvents(ctx context.Context, tenantID, parkID, day, stamp string) (domain.EventPage, error) {
	rows, err := r.pool.Query(ctx, shiftingEventsSQL, tenantID, parkID, day, stamp)
	if err != nil {
		return domain.EventPage{}, fmt.Errorf("alerts: shifting %s events: %w", stamp, err)
	}
	defer rows.Close()
	out := domain.EventPage{Rows: []domain.Event{}}
	for rows.Next() {
		var e domain.Event
		var head int64
		var status, priority, srcName, srcPart, dstName, dstPart string
		if err := rows.Scan(&out.Total, &e.Key, &head, &status, &priority, &srcName, &srcPart, &e.ShedID, &dstName, &dstPart); err != nil {
			return domain.EventPage{}, fmt.Errorf("alerts: shifting events scan: %w", err)
		}
		e.ParkID = parkID
		e.ShedName, e.PartitionLabel = dstName, dstPart
		from := "outside the register"
		if srcName != "" {
			from = penLabel(srcName, srcPart)
		}
		e.Subject = fmt.Sprintf("%d animal(s), %s → %s", head, from, penLabel(dstName, dstPart))
		statusLine := map[string]string{"pending": "Awaiting park head approval", "authorized": "Approved, awaiting the move", "pending_verification": "Moved, proof in review", "applied": "Moved", "unresolved": "Unresolved"}[status]
		if statusLine == "" {
			statusLine = "Recorded"
		}
		if priority == "high" || priority == "emergency" {
			statusLine += " · high priority"
		}
		e.Detail = statusLine + "."
		e.Href = "/approvals"
		out.Rows = append(out.Rows, e)
	}
	return out, rows.Err()
}

func (r *Repository) feedPurchaseEvents(ctx context.Context, tenantID, parkID, day string) (domain.EventPage, error) {
	rows, err := r.pool.Query(ctx, feedPurchaseEventsSQL, tenantID, parkID, day)
	if err != nil {
		return domain.EventPage{}, fmt.Errorf("alerts: feed purchase events: %w", err)
	}
	defer rows.Close()
	out := domain.EventPage{Rows: []domain.Event{}}
	for rows.Next() {
		var e domain.Event
		var feed, vendor, delivery string
		var kg float64
		if err := rows.Scan(&out.Total, &e.Key, &feed, &kg, &vendor, &delivery); err != nil {
			return domain.EventPage{}, fmt.Errorf("alerts: feed purchase events scan: %w", err)
		}
		e.ParkID = parkID
		e.Subject = fmt.Sprintf("%s, %.0f kg", feed, kg)
		parts := []string{}
		if vendor != "" {
			parts = append(parts, "from "+vendor)
		}
		if delivery != "" {
			parts = append(parts, strings.ReplaceAll(delivery, "_", " "))
		}
		e.Detail = strings.TrimSpace(strings.Join(parts, " · ") + ".")
		if e.Detail == "." {
			e.Detail = "Recorded on the feed purchase ledger."
		}
		e.Href = "/procurement/feed-purchases"
		out.Rows = append(out.Rows, e)
	}
	return out, rows.Err()
}

func animalSubject(tag string) string {
	if strings.TrimSpace(tag) == "" {
		return "animal with no tag on record"
	}
	return "tag " + tag
}

func describeAnimal(breed, sex string) string {
	parts := []string{}
	if breed != "" {
		parts = append(parts, breed)
	}
	if sex != "" {
		parts = append(parts, sex)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ") + "."
}

// penLabel names one pen for the movement subject line, which names TWO pens in one sentence
// and so cannot ride the row's single location fields; the composition is oploc's, never ours.
func penLabel(shed, partition string) string {
	return oploc.OperationalLocation{ShedName: shed, PartitionLabel: partition}.Display()
}
