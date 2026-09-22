package postgres

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// ---------------------------------------------------------------------------
// Feed follow-up (did the sheet react when animals entered or left a pen?)
// ---------------------------------------------------------------------------
//
// TWO reads, composed in Go. They are deliberately not one query: the sheet
// side is the frozen feed rows and the cause side is the herd register, they
// share no key beyond (pen, business date), and joining them in SQL would mean
// choosing which side's pen-days survive an outer join before the rule has been
// applied. Composing after both are in hand keeps "the pen had an event but no
// sheet" and "the pen had a sheet but no event" as the distinct states they
// are. Both reads are bounded by the same window and park scope, so this is two
// round trips, never a per-pen fan-out.
//
// THE JOIN KEY IS THE PARTITION LABEL, NOT partition_key, and that is not a
// preference -- it is what the live data forces. `feed_direction_issue_rows`
// stores `part 1` (a lowercased label, spaces intact) while feed_config_norm()
// and domain.PartitionMatchKey() both produce `part_1`, so normalising the herd
// side with either one matches NOTHING: on 2026-09-22 every partitioned pen on
// the live database fell into a phantom "this pen had no sheet" bucket and read
// as pending. The two tables DO share the human label exactly (`Part 1`, `1`),
// so both sides derive the key from that with one expression, penJoinKey.
// Do not "tidy" either side back onto partition_key or feed_config_norm without
// checking what the rows actually hold.
//
// The sheet read's shape is the pen-day half of shedFeedAnalyticsSQL: the same
// iss membership, the same pen grain, and the same MAX(head_count) per
// (shed tag x breed) rule -- head_count repeats per session and per item cell by
// generation, so SUMming cells would inflate the mouths ~6x. If that rule ever
// changes there, it must change here, or this tab and the overview table will
// disagree about how many animals a pen was fed for.

// Producer natural key: (tenant_id, feed_direction_issue_id, shed_id, partition_key,
// session_no, shed_tag_key, breed_key, feed_item_key). Consumer group keys:
// pen_grain_day at (park_id, shed_id, pen_key, feed_day, shed_tag_key, breed_key), then
// the outer query at (park_id, shed_id, pen_key, feed_day) -- one row per pen-day, the
// grain the rule compares. Ratio check: none, this read divides nothing; kg and mouths
// are reported side by side.
//
// projection-review: membership=feed_direction_issue_rows of the window's live issues, the SAME set shedFeedAnalyticsSQL reads (states issued/amended/locked, workflows normal+experiment, at most one live issue per tenant+park+feed_day by feed_direction_issues_live_uidx), narrowed to the sheds the causes read found; group_key=(park_id, shed_id, pen_key, feed_day) at the served grain, one row per pen-day, with pen_key derived from the partition LABEL by penJoinKey on both sides; join_cardinality=iss to feed_direction_issue_rows is 1:N on feed_direction_issue_id and is exactly the grain being aggregated, joined once per day through the live-issue partial unique index, and no other table is joined so count and SUM cannot be inflated; pagination=none, a bounded window of at most 92 days over the pens that had an event (~20), never the farm's whole pen catalog; scope=tenant_id on both tables plus the caller's authorized park set via park_id = ANY($2), the window via feed_day BETWEEN $3 AND $4, and the event sheds via shed_id = ANY($5)
//
// scale-guard:ignore: 5k-50k-envelope -- bounded windowed aggregate over <=92
// days x <=2 parks x ~1 live issue/day, rows reached via the issue-id
// natural-key prefix; result grain is the pen catalog x the window's days.
// penJoinKey renders the ONE expression both reads group by, so the two sides
// cannot drift into two spellings of the same pen. Given the column holding a
// partition LABEL it yields 'whole' for the undivided sentinel (empty, NULL or
// the literal) and the lowercased trimmed label otherwise -- which is exactly
// the form feed_direction_issue_rows already stores in partition_key.
func penJoinKey(labelCol string) string {
	return "CASE WHEN COALESCE(btrim(" + labelCol + "), '') IN ('', 'whole') THEN 'whole'" +
		" ELSE lower(btrim(" + labelCol + ")) END"
}

var feedFollowUpSheetSQL = `
WITH iss AS (
    SELECT feed_direction_issue_id, feed_day
    FROM feed_direction_issues
    WHERE tenant_id = $1
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR park_id = ANY ($2::uuid[]))
      AND feed_day BETWEEN $3 AND $4
      AND state IN ('issued', 'amended', 'locked')
      AND workflow IN ('normal', 'experiment')
),
pen_grain_day AS (
    SELECT r.park_id, r.shed_id,
           -- Derived from the LABEL; see penJoinKey and the header.
           ` + penJoinKey("r.partition_label") + ` AS pen_key,
           i.feed_day, r.shed_tag_key, r.breed_key,
           MAX(r.park_label)      AS park_label,
           MAX(r.shed_label)      AS shed_label,
           MAX(r.partition_label) AS partition_label,
           SUM(r.quantity_kg)     AS grain_kg,
           -- MAX, not SUM: head_count is repeated on every session and item
           -- cell of the grain by generation.
           MAX(r.head_count) FILTER (WHERE r.quantity_kg IS NOT NULL) AS grain_heads
    FROM iss i
    JOIN feed_direction_issue_rows r
      ON r.tenant_id = $1
     AND r.feed_direction_issue_id = i.feed_direction_issue_id
    -- ONLY the sheds that actually had a purchase, sale or death in the
    -- window. The causes read runs FIRST and hands its shed set in, which is
    -- what keeps this read proportional to the pens the tab reports on (~20)
    -- instead of the farm's whole pen catalog: the unnarrowed shape aggregated
    -- every pen-day in the window and put the endpoint's p90 over budget.
    -- Never empty when this query runs -- the caller skips the read entirely
    -- when nothing happened.
     AND r.shed_id = ANY ($5::uuid[])
    GROUP BY r.park_id, r.shed_id, ` + penJoinKey("r.partition_label") + `,
             i.feed_day, r.shed_tag_key, r.breed_key
)
SELECT park_id::text,
       COALESCE(MAX(park_label), ''),
       shed_id::text,
       COALESCE(MAX(shed_label), ''),
       pen_key,
       CASE WHEN pen_key = 'whole' THEN ''
            ELSE COALESCE(MAX(partition_label), '') END,
       feed_day::text,
       COALESCE(SUM(grain_heads), 0)::int,
       round(SUM(grain_kg), 1)::text
FROM pen_grain_day
GROUP BY park_id, shed_id, pen_key, feed_day
-- A day where every cell was blocked has NULL kg: the pen has no reading that
-- day, so it is ABSENT rather than compared as a zero.
HAVING SUM(grain_kg) IS NOT NULL
ORDER BY park_id, shed_id, pen_key, feed_day`

// The cause side: every animal that entered or left a pen inside the window,
// tallied per (pen, business date, cause).
//
// THE THREE CAUSES ARE THE MAINTAINER'S THREE (2026-09-22): purchased, sold,
// died. Shifting between pens is NOT counted -- see the domain rule's header for
// why the remainder is reported as Unexplained instead.
//
// WHICH PEN AN EVENT BELONGS TO, and the one boundary worth knowing:
//
//	sold      -- goat_sale_allocations SNAPSHOTS the pen at the moment of sale
//	             and never updates it, so a sale always names where the animal
//	             actually stood.
//	died      -- the goat's own shed_id/partition. An exited animal stops
//	             moving, so this is equally the pen it stood in.
//	purchased -- the goat's CURRENT pen. There is no per-animal record of where
//	             intake placed it, so an animal shifted since its intake is
//	             counted against the pen it sits in now. Within a 30-92 day
//	             window that is the pen it landed in for all but a shifted few,
//	             and a wrong attribution surfaces as Unexplained on both pens
//	             rather than as a silent pass.
//
// Dates are IST business dates (AGENTS.md: India-business-calendar semantics --
// UTC never defines a Goat OS business day), taken from the instant the register
// records for the exit or the intake.
//
// WHICH SHEET HAS TO CARRY THE CHANGE IS DECIDED HERE, because this is where the
// event's TIME OF DAY is still in hand. The farm issues tomorrow's sheet this
// morning, corrects it at the park's correction_time, and packs it by 15:00, so
// an event BEFORE that cut-off is owed tomorrow's sheet and one AFTER it cannot
// reach tomorrow at all -- that feed is already bagged -- and is owed the day
// after. Judging a frozen sheet would accuse the farm of ignoring an animal it
// could not act on.
//
// The cut-off comes from feed_schedule_config for THAT park, effective-dated on
// the event's own day, for the NORMAL workflow -- the same row the generator and
// the afternoon correction run on. A park with no row falls back to $5, which
// the caller fills from domain.DefaultCorrectionTime. It is deliberately not a
// literal here: a farm that moves its correction to 13:00 moves this rule with
// it, with no code change.
//
// Identifiers are RFID / tag values, never goat ids (AGENTS.md -> Mesha / Goat
// OS RFID Language): the sale carries the string the operator read off the
// animal while tagging, and the other two resolve the register's own active
// identifiers. The array is capped in SQL and the true count travels beside it,
// so a truncated sample can say it is one.
//
// Producer natural keys: goats PK per animal; goat_sale_allocations one LIVE row per
// animal by its partial unique index; procurement_load_goats deduped to one row per goat
// by DISTINCT ON, the same rule counts' mortality `member` CTE uses. Ratio check: none,
// this read divides nothing.
//
// projection-review: membership=three disjoint per-ANIMAL event sets inside the window -- goats that died (the identical exit predicate counts' mortalityPopulationSQL flags died with, so the two screens cannot disagree about a death), goat_sale_allocations still status=tagged, and procurement_load_goats at current_state=accepted_herd_intake; group_key=(park_id, shed_id, pen_key, event_date, kind), with pen_key derived from the partition LABEL by penJoinKey exactly as the sheet read derives it; join_cardinality=goat_shed_partitions is PK (tenant_id, goat_id) so 1 to 0-or-1, the identifier lookup is a LATERAL returning at most one row by ORDER BY with LIMIT 1, and the two locations lookups are primary-key label joins -- none can multiply an animal, so count(*) counts ANIMALS; pagination=none, bounded by the window and the farm's pen catalog; scope=tenant_id on every branch plus the caller's authorized park set via park_id = ANY($2) and the window's dates via bounds
//
// scale-guard:ignore: 5k-50k-envelope -- three windowed per-animal scans over
// the herd at 5k-50k animals, each bounded by an event date inside a <=92 day
// window; the result grain is the pen catalog x the window's event days.
var feedFollowUpCausesSQL = `
WITH bounds AS (
    SELECT $3::date AS from_date, $4::date AS to_date
),
-- One row per animal per load-intake, newest intake wins: the same dedupe rule
-- counts' mortality read uses, so an animal re-recorded on a second load is one
-- animal here too.
intake AS (
    SELECT DISTINCT ON (plg.goat_id) plg.goat_id,
           plg.intake_accepted_at AT TIME ZONE 'Asia/Kolkata' AS event_at
    FROM procurement_load_goats plg
    CROSS JOIN bounds b
    WHERE plg.tenant_id = $1
      AND plg.current_state = 'accepted_herd_intake'
      AND plg.intake_accepted_at IS NOT NULL
      AND (plg.intake_accepted_at AT TIME ZONE 'Asia/Kolkata')::date BETWEEN b.from_date AND b.to_date
    ORDER BY plg.goat_id, plg.intake_accepted_at DESC, plg.load_goat_id DESC
),
events AS (
    -- DIED. Same predicate as counts' mortality read.
    SELECT '` + domain.FeedFollowUpDied + `'::text AS kind,
           g.goat_id,
           g.park_id,
           g.shed_id,
           COALESCE(gsp.partition_label, 'whole') AS partition_label,
           COALESCE(g.exited_at, g.updated_at) AT TIME ZONE 'Asia/Kolkata' AS event_at,
           ''::text AS snapshot_tag
    FROM goats g
    CROSS JOIN bounds b
    LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = $1 AND gsp.goat_id = g.goat_id
    WHERE g.tenant_id = $1
      AND g.merged_into_goat_id IS NULL
      AND (g.exit_reason = 'died' OR (g.exit_reason IS NULL AND g.lifecycle_status = 'dead'))
      AND COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,
                   (g.updated_at AT TIME ZONE 'Asia/Kolkata')::date) BETWEEN b.from_date AND b.to_date

    UNION ALL

    -- SOLD. The allocation's own snapshot of park/pen/tag: history, and the
    -- only reading that still describes where the animal was when it left.
    SELECT '` + domain.FeedFollowUpSold + `'::text,
           a.goat_id,
           a.park_id,
           a.shed_id,
           COALESCE(NULLIF(btrim(a.partition_label), ''), 'whole'),
           a.allocated_at AT TIME ZONE 'Asia/Kolkata',
           COALESCE(btrim(a.tag_number), '')
    FROM goat_sale_allocations a
    CROSS JOIN bounds b
    WHERE a.tenant_id = $1
      AND a.status = 'tagged'
      AND (a.allocated_at AT TIME ZONE 'Asia/Kolkata')::date BETWEEN b.from_date AND b.to_date

    UNION ALL

    -- PURCHASED. The animal's current pen; see the header for that boundary.
    SELECT '` + domain.FeedFollowUpPurchased + `'::text,
           g.goat_id,
           g.park_id,
           g.shed_id,
           COALESCE(gsp.partition_label, 'whole'),
           i.event_at,
           ''::text
    FROM intake i
    JOIN goats g ON g.tenant_id = $1 AND g.goat_id = i.goat_id
    LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = $1 AND gsp.goat_id = g.goat_id
    WHERE g.merged_into_goat_id IS NULL
),
tagged AS (
    SELECT e.kind,
           e.park_id,
           e.shed_id,
           ` + penJoinKey("e.partition_label") + ` AS pen_key,
           -- The HUMAN label, kept beside the matching key: 'whole' is a
           -- sentinel and must never reach a screen, so it blanks here.
           CASE WHEN COALESCE(btrim(e.partition_label), '') IN ('', 'whole') THEN ''
                ELSE btrim(e.partition_label) END AS partition_label,
           e.event_at::date AS event_date,
           -- At or after the cut-off, tomorrow's feed is already packed.
           (e.event_at::time >= COALESCE(sc.correction_time, $5::time)) AS after_cutoff,
           e.event_at::date
             + CASE WHEN e.event_at::time >= COALESCE(sc.correction_time, $5::time)
                    THEN 2 ELSE 1 END AS expected_day,
           to_char(COALESCE(sc.correction_time, $5::time), 'HH24:MI') AS cutoff_time,
           COALESCE(NULLIF(e.snapshot_tag, ''), id.identifier_value, '') AS tag
    FROM events e
    -- The park's clock as it stood ON THE EVENT'S OWN DAY, never today's: a
    -- correction time changed last week must not re-judge the weeks before it.
    LEFT JOIN feed_schedule_config sc
      ON sc.tenant_id = $1
     AND sc.park_id = e.park_id
     AND sc.workflow = 'normal'
     AND sc.valid_from <= e.event_at::date
     AND (sc.valid_to IS NULL OR sc.valid_to > e.event_at::date)
    LEFT JOIN LATERAL (
        SELECT gi.identifier_value
        FROM goat_identifiers gi
        WHERE gi.tenant_id = $1
          AND gi.goat_id = e.goat_id
          AND gi.status = 'active'
          AND gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')
        ORDER BY gi.identifier_type, gi.identifier_value
        LIMIT 1
    ) id ON e.snapshot_tag = ''
    WHERE e.shed_id IS NOT NULL
      AND (coalesce(cardinality($2::uuid[]), 0) = 0 OR e.park_id = ANY ($2::uuid[]))
)
SELECT t.park_id::text,
       t.shed_id::text,
       t.pen_key,
       -- Pen names for a pen the SHEET never reached in this window. Without
       -- them a pen that sold animals while no sheet covered it renders
       -- nameless, which is what the first live run showed.
       COALESCE(MAX(pk.name), ''),
       COALESCE(MAX(sh.name), ''),
       COALESCE(MAX(t.partition_label), ''),
       t.event_date::text,
       t.expected_day::text,
       t.after_cutoff,
       COALESCE(MAX(t.cutoff_time), ''),
       t.kind,
       count(*)::int                                                   AS animals,
       (array_agg(t.tag ORDER BY t.tag) FILTER (WHERE t.tag <> ''))[1:` + feedFollowUpTagCap + `] AS tags
FROM tagged t
LEFT JOIN locations sh ON sh.tenant_id = $1 AND sh.location_id = t.shed_id
LEFT JOIN locations pk ON pk.tenant_id = $1 AND pk.location_id = t.park_id
GROUP BY t.park_id, t.shed_id, t.pen_key, t.event_date, t.expected_day, t.after_cutoff, t.kind
ORDER BY t.park_id, t.shed_id, t.pen_key, t.expected_day, t.event_date, t.kind`

// feedFollowUpTagCap renders domain.FeedFollowUpMaxTags into the SQL slice
// bound, so the cap has ONE definition and the payload cannot advertise a
// different one than the query applies.
const feedFollowUpTagCap = "12"

func init() {
	if domain.FeedFollowUpMaxTags != 12 {
		panic("feedFollowUpTagCap is out of step with domain.FeedFollowUpMaxTags")
	}
}

// feedFollowUpSheetLookahead is how far past the event window the sheet read
// reaches: the furthest an event can push its expected day (after the cut-off
// on the window's last day -> two days later).
const feedFollowUpSheetLookahead = 2

type followUpSheetDay struct {
	penKey    penIdentity
	FeedDay   string
	HeadCount int
	Kg        string
}

type penIdentity struct {
	ParkID       string
	ShedID       string
	PartitionKey string
}

// FeedFollowUp answers, per pen, whether the feed sheet moved after animals
// were purchased, sold or died inside the window.
func (r *Repository) FeedFollowUp(ctx context.Context, tenantID string, q domain.DirectedAnalyticsQuery) (domain.FeedFollowUp, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	from, to := domain.ClampAnalyticsWindow(q.DateFrom, q.DateTo)

	// CAUSES FIRST, deliberately. The sheet read is then narrowed to the sheds
	// that actually had an event, and a window where nothing happened skips it
	// altogether rather than aggregating the farm's whole pen catalog to
	// discover there is nothing to report.
	labels := map[penIdentity]penLabels{}
	// The park's cut-off that decided each check, keyed by expected day + shed
	// so the screen can name the actual clock instead of assuming one.
	cutoffs := map[string]string{}
	causes, err := r.feedFollowUpCauses(ctx, tenantID, q.ParkIDs, from, to, labels, cutoffs)
	if err != nil {
		return domain.FeedFollowUp{}, err
	}
	if len(causes) == 0 {
		return domain.FeedFollowUp{Rows: []domain.FeedFollowUpPenRow{}}, nil
	}
	sheds := shedIDsOf(causes)
	// The SHEET window reaches PAST the event window. An event on the last day
	// before the cut-off is owed the next day's sheet, and one after it is owed
	// the day after that -- both outside the range the user asked for. Reading
	// two extra days means a late event at the edge is judged on the sheet that
	// actually answers it instead of reporting pending forever.
	sheetTo := to.AddDate(0, 0, feedFollowUpSheetLookahead)
	sheet, err := r.feedFollowUpSheet(ctx, tenantID, q.ParkIDs, from, sheetTo, sheds, labels)
	if err != nil {
		return domain.FeedFollowUp{}, err
	}
	return composeFeedFollowUp(sheet, labels, causes, cutoffs), nil
}

type penLabels struct {
	ParkLabel      string
	ShedLabel      string
	PartitionLabel string
}

// shedIDsOf is the narrowing the sheet read is given: the sheds that had a
// purchase, sale or death. Sheds, not pens -- a shed's partitions are a handful
// of rows and filtering on the shed keeps the predicate on an indexed column.
func shedIDsOf(causes map[penIdentity][]domain.FeedFollowUpCause) []uuid.UUID {
	seen := map[string]struct{}{}
	out := make([]uuid.UUID, 0, len(causes))
	for pen := range causes {
		if _, dup := seen[pen.ShedID]; dup {
			continue
		}
		seen[pen.ShedID] = struct{}{}
		id, err := uuid.Parse(pen.ShedID)
		if err != nil {
			continue
		}
		out = append(out, id)
	}
	return out
}

func (r *Repository) feedFollowUpSheet(ctx context.Context, tenantID string, parkIDs []uuid.UUID, from, to time.Time, sheds []uuid.UUID, labels map[penIdentity]penLabels) (map[penIdentity][]domain.FeedFollowUpSheetDay, error) {
	rows, err := r.pool.Query(ctx, feedFollowUpSheetSQL, tenantID, parkIDs,
		from.Format("2006-01-02"), to.Format("2006-01-02"), sheds)
	if err != nil {
		return nil, fmt.Errorf("feed follow-up sheet: %w", err)
	}
	defer rows.Close()
	days := map[penIdentity][]domain.FeedFollowUpSheetDay{}
	for rows.Next() {
		var (
			pen penIdentity
			lbl penLabels
			d   domain.FeedFollowUpSheetDay
		)
		if err := rows.Scan(&pen.ParkID, &lbl.ParkLabel, &pen.ShedID, &lbl.ShedLabel,
			&pen.PartitionKey, &lbl.PartitionLabel, &d.FeedDay, &d.HeadCount, &d.Kg); err != nil {
			return nil, fmt.Errorf("feed follow-up sheet scan: %w", err)
		}
		// The query orders by day within a pen, so appending preserves the
		// ascending order the rule relies on.
		days[pen] = append(days[pen], d)
		// The SHEET's words win wherever it covered the pen: they are what the
		// rest of the page prints. The causes read only fills the gaps.
		labels[pen] = lbl
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("feed follow-up sheet rows: %w", err)
	}
	return days, nil
}

// feedFollowUpCauses reads the herd side and FILLS IN labels for any pen the
// sheet read did not cover, writing into the shared `labels` map rather than
// returning a second one -- one pen must resolve to one set of words.
func (r *Repository) feedFollowUpCauses(ctx context.Context, tenantID string, parkIDs []uuid.UUID, from, to time.Time, labels map[penIdentity]penLabels, cutoffs map[string]string) (map[penIdentity][]domain.FeedFollowUpCause, error) {
	rows, err := r.pool.Query(ctx, feedFollowUpCausesSQL, tenantID, parkIDs,
		from.Format("2006-01-02"), to.Format("2006-01-02"), domain.DefaultCorrectionTime)
	if err != nil {
		return nil, fmt.Errorf("feed follow-up causes: %w", err)
	}
	defer rows.Close()
	out := map[penIdentity][]domain.FeedFollowUpCause{}
	for rows.Next() {
		var (
			pen    penIdentity
			lbl    penLabels
			c      domain.FeedFollowUpCause
			cutoff string
			tags   []string
		)
		if err := rows.Scan(&pen.ParkID, &pen.ShedID, &pen.PartitionKey,
			&lbl.ParkLabel, &lbl.ShedLabel, &lbl.PartitionLabel,
			&c.EventDate, &c.ExpectedDay, &c.AfterCutoff, &cutoff,
			&c.Kind, &c.Animals, &tags); err != nil {
			return nil, fmt.Errorf("feed follow-up causes scan: %w", err)
		}
		// Written FIRST (this read runs first now); the sheet read overwrites
		// any pen it covers, so the sheet's own words still win.
		if _, seen := labels[pen]; !seen {
			labels[pen] = lbl
		}
		c.TagsTotal = c.Animals
		c.Tags = tags
		cutoffs[c.ExpectedDay+"|"+pen.ShedID] = cutoff
		if c.Tags == nil {
			c.Tags = []string{}
		}
		out[pen] = append(out[pen], c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("feed follow-up causes rows: %w", err)
	}
	return out, nil
}

// composeFeedFollowUp applies the domain rule pen by pen and totals the window.
//
// A pen appears when it had a CAUSE in the window. A pen the sheet fed and
// nothing happened in has nothing to follow up and is absent -- this tab
// answers for the pens where animals moved, and listing the other ~180 with a
// row of zeroes would bury them.
func composeFeedFollowUp(
	sheet map[penIdentity][]domain.FeedFollowUpSheetDay,
	labels map[penIdentity]penLabels,
	causes map[penIdentity][]domain.FeedFollowUpCause,
	cutoffs map[string]string,
) domain.FeedFollowUp {
	out := domain.FeedFollowUp{Rows: []domain.FeedFollowUpPenRow{}}
	// The sheet's own word for each park, collected before any row is built.
	parkWords := map[string]string{}
	for pen := range sheet {
		if lbl, ok := labels[pen]; ok && lbl.ParkLabel != "" {
			parkWords[pen.ParkID] = lbl.ParkLabel
		}
	}
	// The window's two ends, accumulated across pens as each row is built.
	windowStart, windowEnd := 0, 0
	startKg, endKg := []string{}, []string{}
	for pen, penCauses := range causes {
		penSheet := sheet[pen]
		lbl := labels[pen]
		// ONE SPELLING PER PARK. The sheet carries its own denormalized park
		// label ("CBE") while locations.name is the full name ("Coimbatore"),
		// and the first live run printed both in one column. Wherever the
		// sheet named this park, its word wins for every row of that park;
		// locations only fills a park the sheet never reached.
		if fromSheet, ok := parkWords[pen.ParkID]; ok && fromSheet != "" {
			lbl.ParkLabel = fromSheet
		}
		row := domain.FeedFollowUpPenRow{
			ParkID:         pen.ParkID,
			ParkLabel:      lbl.ParkLabel,
			ShedID:         pen.ShedID,
			ShedLabel:      lbl.ShedLabel,
			PartitionLabel: lbl.PartitionLabel,
			OperationalLocationDisplay: oploc.OperationalLocation{
				ShedName: lbl.ShedLabel, PartitionLabel: lbl.PartitionLabel,
			}.Display(),
			Days: []domain.FeedFollowUpDay{},
		}
		// Grouped by the day the feed was SUPPOSED to change, not the day the
		// animal moved: two events either side of a cut-off can be owed the
		// same sheet, and that sheet has to account for both.
		byDate := map[string][]domain.FeedFollowUpCause{}
		dates := []string{}
		for _, c := range penCauses {
			if _, seen := byDate[c.ExpectedDay]; !seen {
				dates = append(dates, c.ExpectedDay)
			}
			byDate[c.ExpectedDay] = append(byDate[c.ExpectedDay], c)
			switch c.Kind {
			case domain.FeedFollowUpPurchased:
				row.Purchased += c.Animals
			case domain.FeedFollowUpSold:
				row.Sold += c.Animals
			case domain.FeedFollowUpDied:
				row.Died += c.Animals
			}
		}
		sort.Strings(dates)
		for _, d := range dates {
			check := domain.ResolveFeedFollowUpDay(d, byDate[d], penSheet)
			check.CutoffTime = cutoffs[d+"|"+pen.ShedID]
			row.Days = append(row.Days, check)
		}
		row.Status = domain.RollUpFeedFollowUpStatus(row.Days)
		// The row carries THE COMPARISON ITS VERDICT CAME FROM, so the two
		// arrows beside the chip always explain the chip.
		if deciding, ok := domain.DecidingFeedFollowUpDay(row.Days); ok {
			row.FirstDay, row.HeadBefore, row.KgBefore = deciding.BeforeDay, deciding.HeadBefore, deciding.KgBefore
			row.LastDay, row.HeadAfter, row.KgAfter = deciding.AfterDay, deciding.HeadAfter, deciding.KgAfter
		}
		// The WALK's two ends are a different question from the row's: "how
		// many mouths did the farm feed at each end of the range", which is
		// the pen's own first and last sheet, not whichever check decided its
		// verdict. Summing the deciding checks put 261 -> 258 under a range
		// that really ran 344 -> 291.
		if len(penSheet) > 0 {
			windowStart += penSheet[0].HeadCount
			windowEnd += penSheet[len(penSheet)-1].HeadCount
			startKg = append(startKg, penSheet[0].Kg)
			endKg = append(endKg, penSheet[len(penSheet)-1].Kg)
		}
		out.Rows = append(out.Rows, row)
	}
	// A stable order the client can render as served: the pens to act on
	// first, then by park and pen name. Status is the whole point of the
	// table, so it leads.
	statusRank := map[string]int{
		domain.FeedFollowUpNotFollowed: 0,
		domain.FeedFollowUpPending:     1,
		domain.FeedFollowUpFollowed:    2,
	}
	sort.SliceStable(out.Rows, func(i, j int) bool {
		a, b := out.Rows[i], out.Rows[j]
		if statusRank[a.Status] != statusRank[b.Status] {
			return statusRank[a.Status] < statusRank[b.Status]
		}
		if a.ParkLabel != b.ParkLabel {
			return a.ParkLabel < b.ParkLabel
		}
		return a.OperationalLocationDisplay < b.OperationalLocationDisplay
	})

	out.Totals.StartAnimals, out.Totals.EndAnimals = windowStart, windowEnd
	for _, row := range out.Rows {
		out.Totals.Purchased += row.Purchased
		out.Totals.Sold += row.Sold
		out.Totals.Died += row.Died
		switch row.Status {
		case domain.FeedFollowUpNotFollowed:
			out.Totals.NotFollowed++
		case domain.FeedFollowUpPending:
			out.Totals.Pending++
		default:
			out.Totals.Followed++
		}
	}
	// The walk need not close: Start and End are sheet head counts, the causes
	// are register events, and a shift between pens moves one without the
	// other. The remainder is CARRIED, never absorbed.
	out.Totals.Unexplained = out.Totals.EndAnimals -
		(out.Totals.StartAnimals + out.Totals.Purchased - out.Totals.Sold - out.Totals.Died)
	out.Totals.StartKg, out.Totals.EndKg = sumFollowUpKg(startKg), sumFollowUpKg(endKg)
	return out
}

// sumFollowUpKg adds one end of the walk across pens. Quantities travel as
// strings all the way from the sheet (they are decimal kg, and parsing them
// into a float at every hop is how a total starts disagreeing with its rows);
// this is the one place a total is needed, so it parses once, adds, and renders
// back at the sheet's own one decimal place.
func sumFollowUpKg(values []string) string {
	total := 0.0
	any := false
	for _, raw := range values {
		if raw == "" {
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			// A quantity the sheet holds that will not parse is a data fault,
			// not a zero: skip it rather than understate the total.
			continue
		}
		total += v
		any = true
	}
	if !any {
		return ""
	}
	return strconv.FormatFloat(total, 'f', 1, 64)
}
