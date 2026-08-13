// Command seed-feed-ration loads the authored feed ration configuration created by migration
// 000003 (feed_ration_groups, feed_shed_tags, feed_item_catalog, feed_ration_rates,
// feed_session_templates) from the extracted source ration grid.
//
// WHAT IT SEEDS
//
//	feed_ration_groups     -- the 6 live breeds mapped to their ration group, carrying the
//	                          Beetal + Sirohi -> 'Beetal/Sirohi' merge. The 'Kid' group is NOT a
//	                          breed and is deliberately absent: kids resolve by age band and never
//	                          consult the breed map.
//	feed_shed_tags         -- the 31-tag vocabulary, with applies_to derived from the grid itself
//	                          (a tag under the 'Kid' ration group is a kid tag). The kid and adult
//	                          tag sets are disjoint in the source; the seed asserts that rather
//	                          than assuming it.
//	feed_item_catalog      -- the 10 feed items, under their real FEED NAMES. The source columns
//	                          are validation-sheet headers carrying a "Per Goat" unit descriptor
//	                          ("Concentrate Per Goat"); that suffix is not part of any feed's name
//	                          and is stripped by feedItemName, the single place the mapping lives.
//	                          Nutritional attributes are left NULL: the source grid carries rates,
//	                          not energy values, and inventing them would be worse than an honest
//	                          gap.
//	feed_ration_rates      -- 77 group/tag rows x 10 feed items x 2 parks = 1540 rates.
//	feed_session_templates -- the default 2 sessions per park at a 50/50 split.
//	feed_session_template_items
//	                       -- (migration 000005) the workbook's `Template` tab: the 5 feed slots
//	                          each park x session actually consists of, in packing order. This is
//	                          the RECIPE; feed_item_catalog is only the tenant's feed VOCABULARY.
//	                          Without it the generator has no declared item list to walk and falls
//	                          back to the whole catalog, asking every shed for the four roughages
//	                          that are substitution alternatives to one another.
//	feed_schedule_config   -- (migration 000004) the per-park dispatch clock for BOTH workflows:
//	                          normal issues its direction at 07:00, experiment at 14:00, and both
//	                          batch emergency-shifting corrections at 14:00 against a 15:45
//	                          transport cutoff. Seeded for both workflows in every park because a
//	                          park with no clock has no dispatch time at all, and (like a missing
//	                          rate) that gap only surfaces later as a direction that never went out.
//
//	feed_experiment_config -- (migration 000003) the 34 hand-entered EXPERIMENT sheds, 17 per park,
//	                          from the separate experiment workbook. Their quantities are ABSOLUTE
//	                          kg for the whole shed and are NEVER multiplied by head count; the
//	                          head count travels alongside as informational context only.
//	                          Membership in this table IS what makes a shed an experiment shed --
//	                          there is no separate flag -- so an unseeded row does not merely lose a
//	                          label, it silently feeds that shed off the per-head ration grid. That
//	                          was measured: it inflated CBE's 2026-07-20 concentrate total from the
//	                          workbook's 182.0 kg to 398.8 kg.
//
// NOT seeded, because there is no source data for them: feed_shed_factors (absence reads as the
// safe 1.0) and feed_conversions. These are authored in the app, and the seed says so on stdout
// rather than fabricating rows.
//
// PARK RESOLUTION -- CBE and CPT are resolved by looking up locations rows with
// location_type='park' and the matching location_code. Park UUIDs are never hardcoded. A missing
// or inactive park is a hard failure: silently skipping one would seed a database where half the
// herd has no ration configuration at all, and (per the migration's zero-vs-missing rule) that
// gap only shows up later as a blocked feed sheet.
//
// IDEMPOTENCY -- re-runnable. Vocabulary rows are inserted only when absent (matched on the
// normalized key, so a label whitespace/case variant does not create a duplicate). Rates are
// reconciled against the currently-open row:
//
//	no open row              -> INSERT
//	open row, same rate      -> no-op
//	open row, different rate -> supersede: close the old row (valid_to = today) and insert the new
//	                            one, preserving the audit trail. If the open row was itself
//	                            authored today, valid_to > valid_from cannot hold, so the same-day
//	                            row is corrected in place instead -- it is a same-day re-author,
//	                            not a historical change worth a window.
//
// Gated to local/dev/test (or staging Cloud SQL via GOATOS_ENV=stg), same guard as the other
// seed commands. It writes only feed_* configuration tables and never touches goats, locations,
// protocols, obligations, or grants.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

//go:embed data/ration.json
var rationGridJSON []byte

//go:embed data/experiment-config.json
var experimentConfigJSON []byte

const defaultTenantID = "00000000-0000-4000-8000-000000000001"

// kidRationGroup is the ration group every kid uses regardless of breed. The source workbook
// literally stores this string in its breed column, which is why it is a group label and not a
// breed in feed_ration_groups.
const kidRationGroup = "Kid"

// breedRationGroups maps each of the 6 live breed labels to its ration group. Beetal and Sirohi
// share one group -- that merge is the whole reason this mapping is data.
var breedRationGroups = []struct {
	BreedLabel string
	GroupLabel string
}{
	{"Anantapur Sheep", "Anantapur Sheep"},
	{"Beetal", "Beetal/Sirohi"},
	{"Boer", "Boer"},
	{"Malai", "Malai"},
	{"Osmanabadi", "Osmanabadi"},
	{"Sirohi", "Beetal/Sirohi"},
	{"Sojat", "Sojat"},
}

// defaultSessions is the default feeding-session template applied to every park: two sessions,
// half the daily quantity each. split_fraction across a park must sum to 1.0 -- validated below
// rather than assumed, since the table cannot express a cross-row CHECK.
var defaultSessions = []struct {
	SessionNo int
	Label     string
	Split     float64
}{
	{1, "Morning", 0.5},
	{2, "Evening", 0.5},
}

// defaultSchedules is the per-workflow dispatch clock applied to every park (migration 000004).
//
// The two workflows deliberately differ on direction_time -- that difference is the whole reason
// feed_schedule_config is keyed by workflow. They agree on correction_time because the batching
// decision is a business rule about how many amended directions a shed may receive in a day (one),
// not a property of either workflow.
//
// Times are LOCAL Asia/Kolkata wall-clock values, stored without an offset. See the migration.
var defaultSchedules = []struct {
	Workflow      string
	DirectionTime string
	// CorrectionTime is when approved emergency-shifting corrections are BATCHED and the amended
	// direction is reissued -- a fixed time, not fire-on-approval, so a shed gets at most one
	// amended sheet per day instead of several racing ones.
	CorrectionTime string
	// TransportTime is the cutoff after which a correction can no longer reach the shed.
	//
	// 15:30, matching the hard 15:30 IST gate in MaterializeTransportTasks (adapters/postgres/
	// transport.go) that creates the day's one-task-per-shed transport work. These were 15:45 and
	// 15:30 respectively, which read as one rule but were two: the sheet stayed amendable for
	// 15 minutes after the transport tasks had already been cut. Maintainer decision 2026-07-31:
	// one time, 15:30 -- the sheet locks exactly when transport is raised.
	TransportTime string
}{
	{"normal", "07:00:00", "14:00:00", "15:30:00"},
	{"experiment", "14:00:00", "14:00:00", "15:30:00"},
}

// parkCodes maps the source grid's farm key to the locations.location_code it must resolve to.
var parkCodes = map[string]string{
	"CBE": "CBE",
	"CPT": "CPT",
}

// ---- source header -> feed name (THE ONE PLACE THIS MAPPING LIVES) ----

// perGoatSuffix matches the source VALIDATION sheet's unit descriptor at the END of a column
// header: "Concentrate Per Goat", "Baking Soda Per Goat", and so on.
//
// "Per Goat" tells a reader of that sheet that the column holds a per-head figure. It is NOT part
// of any feed's name -- nobody at the farm orders "Baking Soda Per Goat" -- and the unit is already
// carried separately and correctly by the app contract ("Grams / head / day"). A packing sheet must
// read the name on the sack.
//
// Anchored to end-of-string and requiring whitespace before "per", so the three genuine names in
// the same header row survive untouched: "Mesha Adult Concentrate Goat" and "Mesha Adult
// Concentrate Sheep" do not END in "Per Goat", and "Toor Dal Bhusa Pellet" contains neither word.
var perGoatSuffix = regexp.MustCompile(`(?i)\s+per\s+goat\s*$`)

// kgUnitSuffix matches the EXPERIMENT workbook's unit descriptor at the end of a column header:
// "Mesha Adult Concentrate Sheep (kg)", "RGS Concentrate (kg)", and so on.
//
// It is the same class of thing as perGoatSuffix and is stripped for the same reason. The two
// workbooks annotate their columns differently because they hold different KINDS of number -- the
// ration grid stores a per-head rate, the experiment workbook stores an absolute shed total -- but
// in both cases the annotation describes the COLUMN, not the feed. "Dry Masoor Bhusa" appears in
// both workbooks and must resolve to ONE catalog row; if only one of the two suffixes were stripped
// it would key as 'dry_masoor_bhusa' from one source and 'dry_masoor_bhusa_(kg)' from the other, and
// per migration 000003's rule a lookup that misses BLOCKS the shed rather than feeding it 0.
//
// Anchored to end-of-string so a feed whose real name contained a parenthesis elsewhere survives.
var kgUnitSuffix = regexp.MustCompile(`(?i)\s*\(\s*kg\s*\)\s*$`)

// feedItemName converts one raw source column header into the feed's actual name.
//
// THIS IS THE ONLY PLACE THE MAPPING HAPPENS. data/ration.json is a faithful extraction of the
// workbook and deliberately keeps the raw headers -- re-extracting it must not require re-applying
// a naming decision. Every consumer of a header (the catalog in seedItems, the rate labels in
// seedRates) routes through here instead, so the catalog label and the rate label are the same
// string by construction and their GENERATED feed_item_key columns therefore join.
//
// Scattering the strip -- or applying it in one consumer and not the other -- would key the catalog
// on 'concentrate' and the rates on 'concentrate_per_goat'. Per migration 000003's rule a rate
// lookup that misses BLOCKS the shed, so that drift would not surface as a naming bug; it would
// surface as every shed in both parks refusing to generate.
//
// Migration 000005 performs the identical strip on rows already in the database. A fresh seed and a
// migrated database therefore converge on the same labels.
//
// BOTH source workbooks route through here -- the ration grid's "... Per Goat" headers and the
// experiment workbook's "... (kg)" headers. That is the point of keeping it one function rather than
// adding a second naming path for the experiment loader: the two workbooks share four feed names
// between them, and a second mapping is exactly how the two copies drift apart.
func feedItemName(header string) string {
	name := strings.TrimSpace(perGoatSuffix.ReplaceAllString(header, ""))
	name = strings.TrimSpace(kgUnitSuffix.ReplaceAllString(name, ""))
	if name == "" {
		// Refuse to invent a name. A header that is nothing but the unit descriptor is a broken
		// extraction, and seeding a blank label would violate the catalog's not-blank CHECK anyway --
		// better to say why.
		return strings.TrimSpace(header)
	}
	return name
}

// normalWorkflowFeedSlots is the source workbook's `Template` tab for the NORMAL workflow: the five
// numbered feed slots each park+session actually consists of, in packing order.
//
// Both live parks (CBE and CPT) declare this same set in both sessions, which is why one list
// serves the whole seed rather than a per-park table. Session-level modelling is still real -- see
// migration 000005 -- this is just what the two parks currently author.
//
// WHAT IS DELIBERATELY ABSENT AND WHY. Hybrid, COFS, Hedge Lucerne and Dry Maize are in the catalog
// and have rates, but are NOT slots: they are inter-feed SUBSTITUTION alternatives to one another
// (that is what feed_conversions models) and exactly one roughage is fed. Toor Dal Bhusa Pellet is
// likewise catalogued but not declared. Before these slots existed the generator had no recipe to
// consult, walked the whole catalog, and asked every shed for all of them at once.
//
// The EXPERIMENT workbook's Template tab declares a different five (Mesha Adult Concentrate Goat |
// Mesha Adult Concentrate Sheep | RGS Concentrate | Vijay Concentrate | Dry Masoor Bhusa). It is
// not seeded:
// feed_experiment_config has no source rows yet, so a recipe for it would be configuration nobody
// can trace back to a farm decision. Experiment sheds do not consult this table in any case -- their
// hand-entered cells are their complete item list.
var normalWorkflowFeedSlots = []string{
	"Concentrate",
	"Dry Masoor Bhusa",
	"Mesha Adult Concentrate Goat",
	"Mesha Adult Concentrate Sheep",
	"Baking Soda",
}

type farmGrid struct {
	Headers []string  `json:"headers"`
	Rows    []gridRow `json:"rows"`
}

type gridRow struct {
	Group string             `json:"group"`
	Tag   string             `json:"tag"`
	Rates map[string]float64 `json:"rates"`
}

// experimentShed is one row of the experiment workbook: one shed, one experiment arm, and the
// ABSOLUTE kg it is fed of each item.
//
// COUNT IS INFORMATIONAL AND MUST NEVER BE MULTIPLIED. Kg holds a shed TOTAL, already inclusive of
// however many animals are in the shed, so multiplying by Count would overfeed the shed by a factor
// of its entire population. It is carried only so the operator reading the direction sheet can see
// what population the hand-entered figure was authored against. The schema says the same thing
// (feed_experiment_config.head_count) and so does ExperimentPlanner, which ignores the projected
// count for quantity purposes entirely.
type experimentShed struct {
	Farm string `json:"farm"`
	Shed string `json:"shed"`
	// Count is a float in the source because the workbook stores it in a numeric column; it is a
	// head count and is validated as a non-negative whole number below.
	Count float64 `json:"count"`
	// Category is the experiment ARM ("Sheep M NEW", "B+S Goat F OLD", ...). 14 distinct values
	// across the 34 sheds. It stands in for the shed tag on the direction sheet, because an
	// experiment shed has no ration grain and so no authored tag to report.
	Category string `json:"category"`
	// Kg is keyed by the workbook's raw column header ("RGS Concentrate (kg)"); feedItemName maps
	// each to the feed's real name, the same one the catalog and the ration grid use.
	Kg map[string]float64 `json:"kg"`
}

type stats struct {
	GroupsInserted   int
	TagsInserted     int
	ItemsInserted    int
	SessionsInserted int
	// SessionItems counters mirror the rate counters because seedSessionItems runs the SAME
	// effective-dated three-way reconciliation: a changed slot supersedes yesterday's row rather
	// than overwriting it. A re-run of an unchanged recipe reports 0 inserted / all unchanged.
	SessionItemsInserted   int
	SessionItemsSuperseded int
	SessionItemsCorrected  int
	SessionItemsUnchanged  int
	RatesInserted          int
	RatesSuperseded        int
	RatesCorrected         int
	RatesUnchanged         int
	// RatesSkipped counts blank grid cells, which write no row. A feed item with no rate row is
	// "not offered for this (group, tag)" -- distinct from an authored 0. Reported so a genuine
	// authoring gap cannot hide behind the expected alternative-roughage blanks.
	RatesSkipped int
	// Schedule counters mirror the rate counters because seedSchedules runs the SAME effective-dated
	// three-way reconciliation: a changed clock supersedes yesterday's row rather than overwriting it.
	SchedulesInserted   int
	SchedulesSuperseded int
	SchedulesCorrected  int
	SchedulesUnchanged  int
	// Experiment counters have NO "superseded" member, and that absence is the schema speaking:
	// feed_experiment_config is not effective-dated. An experiment quantity is a hand-entered figure
	// for a running trial, corrected in place, not a standing rule whose past values must stay
	// reconstructable to explain an old feed sheet. See migration 000006.
	ExperimentShedsResolved int
	ExperimentRowsInserted  int
	ExperimentRowsUpdated   int
	ExperimentRowsUnchanged int
	// ExperimentRowsOutsideSource counts ACTIVE experiment rows that this source no longer names.
	// They are REPORTED, never retired: the same table is authored in-app from /feed/config, and a
	// seed that quietly withdrew a hand-authored experiment shed would move it back onto the per-head
	// ration grid -- a change in what animals are fed, made by a data load nobody asked to do it.
	ExperimentRowsOutsideSource int
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("seed-feed-ration", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID", defaultTenantID), "tenant id")
	timeout := fs.Duration("timeout", 300*time.Second, "seed timeout")
	// DEFAULT FALSE, and it must stay that way. The 34 experiment sheds are named for PARTITIONS
	// ("Castro 1", "Godel 1 - Part 3"), and resolveExperimentSheds needs an active shed row of that
	// exact name. In an environment that stores partitions the canonical way -- physical shed
	// "Castro" plus partition "1", with the partition-named locations row held inactive -- none of
	// them resolve and the seed correctly fails closed.
	//
	// This escape hatch seeds the REST of the config (groups, tags, catalog, rates, sessions,
	// template items, schedule) and skips experiments only. It is a deliberate, temporary state:
	// feed_experiment_config membership IS what marks a shed an experiment shed, so while those
	// rows are absent those 34 sheds are fed off the PER-HEAD ration grid instead of their authored
	// absolute kg. That is measured, not theoretical -- it read CBE's 2026-07-20 concentrate as
	// 398.8 kg against the workbook's 182.0 kg.
	//
	// The real fix is partition-aware experiment config: feed_experiment_config has no
	// partition_label column and its natural key is (tenant, park, shed, feed_item), so three
	// partitions of one shed cannot be stored at all today. That needs a migration widening the key
	// plus a generator that matches experiment cells by (shed_id, partition). Until then, a run with
	// this flag prints what it skipped and why.
	skipExperiments := fs.Bool("skip-experiments", false,
		"seed everything EXCEPT feed_experiment_config; those sheds then feed off the per-head grid until partition-aware experiment config lands")
	if err := fs.Parse(args); err != nil {
		return err
	}

	grid, err := loadGrid()
	if err != nil {
		return fmt.Errorf("load ration grid: %w", err)
	}
	experiments, err := loadExperiments()
	if err != nil {
		return fmt.Errorf("load experiment config: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	pgCfg := platformpg.ConfigFromEnv()
	if err := validateTarget(os.Getenv("GOATOS_ENV"), pgCfg.DatabaseURL); err != nil {
		return err
	}
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	if err := checkSchemaReady(ctx, pool); err != nil {
		return err
	}

	parks, err := resolveParks(ctx, pool, *tenantID, grid)
	if err != nil {
		return err
	}

	// Resolved BEFORE the transaction opens, for the same reason parks are: an unresolvable shed is
	// a source/data problem the operator must fix, not a half-applied write to roll back. A shed name
	// that does not resolve is a HARD FAILURE -- see resolveExperimentSheds.
	experimentSheds := map[string]string{}
	if *skipExperiments {
		fmt.Printf("SKIPPING feed_experiment_config: %d experiment shed(s) across both parks are NOT seeded.\n", len(experiments))
		fmt.Printf("  Those sheds will be fed from the PER-HEAD ration grid instead of their authored absolute kg,\n")
		fmt.Printf("  which overstates their feed (measured: CBE 2026-07-20 concentrate 398.8 kg vs the workbook's 182.0 kg).\n")
		fmt.Printf("  Re-run without -skip-experiments once partition-aware experiment config lands.\n")
	} else {
		experimentSheds, err = resolveExperimentSheds(ctx, pool, *tenantID, parks, experiments)
		if err != nil {
			return err
		}
	}

	// The seed's business day, not the database clock's UTC day: an effective-dated config row is
	// business truth and must be anchored to the India business calendar (AGENTS.md time
	// semantics), otherwise a late-evening IST run would date a rate to the previous day.
	businessDate := biztime.BusinessDate(time.Now())

	st := stats{}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := seedGroups(ctx, tx, *tenantID, &st); err != nil {
		return fmt.Errorf("seed ration groups: %w", err)
	}
	if err := seedTags(ctx, tx, *tenantID, grid, &st); err != nil {
		return fmt.Errorf("seed shed tags: %w", err)
	}
	// The catalog is loaded from BOTH workbooks. RGS Concentrate and Vijay Concentrate exist only in
	// the experiment workbook and would otherwise be absent from the tenant's feed vocabulary, so an
	// experiment row naming one would author a quantity for a feed the catalog has never heard of.
	if err := seedItems(ctx, tx, *tenantID, catalogItemLabels(grid, experiments), &st); err != nil {
		return fmt.Errorf("seed feed items: %w", err)
	}
	if err := seedSessions(ctx, tx, *tenantID, parks, &st); err != nil {
		return fmt.Errorf("seed session templates: %w", err)
	}
	// After seedSessions: a slot row FKs (tenant, park, session_no) back to the session it belongs
	// to, so the sessions must exist first.
	if err := seedSessionItems(ctx, tx, *tenantID, parks, grid, businessDate, &st); err != nil {
		return fmt.Errorf("seed session template items: %w", err)
	}
	if err := seedRates(ctx, tx, *tenantID, parks, grid, businessDate, &st); err != nil {
		return fmt.Errorf("seed ration rates: %w", err)
	}
	if err := seedSchedules(ctx, tx, *tenantID, parks, businessDate, &st); err != nil {
		return fmt.Errorf("seed schedule config: %w", err)
	}
	// Last, and after seedItems: an experiment row names a feed by label, and that label must already
	// resolve to a catalog row of the same tenant or the direction sheet would carry a feed nobody
	// can order.
	if !*skipExperiments {
		if err := seedExperiments(ctx, tx, *tenantID, experiments, experimentSheds, &st); err != nil {
			return fmt.Errorf("seed experiment config: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	fmt.Printf("seeded feed ration config (tenant=%s business_date=%s):\n", *tenantID, businessDate)
	fmt.Printf("  ration_groups_inserted=%d shed_tags_inserted=%d feed_items_inserted=%d session_templates_inserted=%d\n",
		st.GroupsInserted, st.TagsInserted, st.ItemsInserted, st.SessionsInserted)
	fmt.Printf("  session_template_items_inserted=%d superseded=%d corrected_same_day=%d unchanged=%d (the workbook's Template tab: %d declared feed slots per park x session)\n",
		st.SessionItemsInserted, st.SessionItemsSuperseded, st.SessionItemsCorrected, st.SessionItemsUnchanged, len(normalWorkflowFeedSlots))
	fmt.Printf("  rates_inserted=%d rates_superseded=%d rates_corrected_same_day=%d rates_unchanged=%d\n",
		st.RatesInserted, st.RatesSuperseded, st.RatesCorrected, st.RatesUnchanged)
	fmt.Printf("  rates_skipped_blank_cells=%d (no row written; item not offered for that group/tag -- a lookup for one BLOCKS the shed, it never feeds 0)\n",
		st.RatesSkipped)
	fmt.Printf("  schedules_inserted=%d schedules_superseded=%d schedules_corrected_same_day=%d schedules_unchanged=%d (local Asia/Kolkata times, per park x workflow)\n",
		st.SchedulesInserted, st.SchedulesSuperseded, st.SchedulesCorrected, st.SchedulesUnchanged)
	fmt.Printf("  experiment_sheds_resolved=%d experiment_rows_inserted=%d updated=%d unchanged=%d (ABSOLUTE kg per shed; head_count is informational and is NEVER multiplied in)\n",
		st.ExperimentShedsResolved, st.ExperimentRowsInserted, st.ExperimentRowsUpdated, st.ExperimentRowsUnchanged)
	if st.ExperimentRowsOutsideSource > 0 {
		fmt.Printf("  experiment_rows_outside_source=%d (active rows this source does not name; left untouched -- retiring one would move that shed back onto the per-head grid)\n",
			st.ExperimentRowsOutsideSource)
	}
	fmt.Printf("  not seeded (no source data, authored in-app): feed_shed_factors, feed_conversions\n")
	fmt.Printf("  not seeded (by design): experiment session slots -- ExperimentPlanner does not consult feed_session_template_items; a shed's authored cells ARE its complete item list\n")
	return nil
}

// ---- source grid ----

func loadGrid() (map[string]farmGrid, error) {
	var grid map[string]farmGrid
	if err := json.Unmarshal(rationGridJSON, &grid); err != nil {
		return nil, err
	}
	if len(grid) == 0 {
		return nil, fmt.Errorf("embedded ration grid is empty")
	}
	for farm := range grid {
		if _, ok := parkCodes[farm]; !ok {
			return nil, fmt.Errorf("ration grid contains farm %q with no park-code mapping", farm)
		}
	}
	for farm, g := range grid {
		if len(g.Headers) < 3 {
			return nil, fmt.Errorf("%s: expected at least 3 headers (Breed, Tag, >=1 feed item), got %d", farm, len(g.Headers))
		}
		if len(g.Rows) == 0 {
			return nil, fmt.Errorf("%s: ration grid has no rows", farm)
		}
	}
	return grid, nil
}

// feedItems returns the feed-item columns of the grid (headers after Breed and Tag) as FEED NAMES,
// in source order so display_order matches the workbook the operator knows.
//
// Dedup is on the mapped NAME, not the raw header, so two headers that differ only by the unit
// descriptor could not produce two catalog rows for one feed.
func feedItems(grid map[string]farmGrid) []string {
	seen := map[string]bool{}
	var out []string
	for _, farm := range sortedKeys(grid) {
		for _, h := range grid[farm].Headers[2:] {
			name := feedItemName(h)
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

// ---- experiment workbook ----

// loadExperiments reads and validates the embedded experiment workbook extract.
//
// Every check below fails the whole seed rather than skipping the offending row, and that is the
// same rule the park resolution uses: a SKIPPED experiment shed does not stay unconfigured and
// visibly blocked -- it silently falls through to NormalPlanner and gets fed off the per-head ration
// grid, which is the exact defect this data exists to fix. A quiet skip would therefore produce a
// clean-looking feed sheet with the wrong quantities on it.
func loadExperiments() ([]experimentShed, error) {
	var rows []experimentShed
	if err := json.Unmarshal(experimentConfigJSON, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("embedded experiment config is empty")
	}

	// A (farm, shed) pair may appear only once: the shed's arm and head count describe the SHED and
	// are repeated across its item rows, so two rows for one shed could disagree about them and the
	// last writer would silently win.
	seen := map[string]bool{}
	for i, row := range rows {
		if _, ok := parkCodes[row.Farm]; !ok {
			return nil, fmt.Errorf("experiment row %d: farm %q has no park-code mapping", i, row.Farm)
		}
		if strings.TrimSpace(row.Shed) == "" {
			return nil, fmt.Errorf("experiment row %d (%s): shed name is blank", i, row.Farm)
		}
		if strings.TrimSpace(row.Category) == "" {
			// experiment_category is NOT NULL and blank-checked by the schema, but the reason it must
			// be present is operational: it is what the direction sheet prints in the shed-tag column
			// for an experiment shed, which is the operator's only cue that this shed's numbers are
			// hand-entered rather than computed.
			return nil, fmt.Errorf("experiment row %d (%s / %s): experiment category is blank", i, row.Farm, row.Shed)
		}
		if row.Count < 0 || row.Count != float64(int64(row.Count)) {
			return nil, fmt.Errorf("experiment row %d (%s / %s): head count %v is not a non-negative whole number", i, row.Farm, row.Shed, row.Count)
		}
		if len(row.Kg) == 0 {
			return nil, fmt.Errorf("experiment row %d (%s / %s): declares no feed quantities", i, row.Farm, row.Shed)
		}
		for header, kg := range row.Kg {
			if feedItemName(header) == "" {
				return nil, fmt.Errorf("experiment row %d (%s / %s): feed column %q maps to a blank feed name", i, row.Farm, row.Shed, header)
			}
			// 0 kg is a REAL authored value here, exactly as an authored 0 g is in the ration grid: an
			// arm that gets none of an item is a deliberate part of the experiment design. Negative is
			// not representable as a quantity and the schema rejects it, so it is caught here with the
			// row named.
			if kg < 0 {
				return nil, fmt.Errorf("experiment row %d (%s / %s): %q is %v kg; a quantity cannot be negative", i, row.Farm, row.Shed, header, kg)
			}
		}
		key := row.Farm + "\x1f" + configNormKey(row.Shed)
		if seen[key] {
			return nil, fmt.Errorf("experiment config names shed %q in farm %s more than once; its arm and head count describe the shed and cannot differ between rows", row.Shed, row.Farm)
		}
		seen[key] = true
	}
	return rows, nil
}

// catalogItemLabels is the tenant's complete feed vocabulary: every item named by either workbook.
//
// Grid items keep their existing positions so display_order does not shuffle on an existing database
// (the catalog's order is what the direction sheet's columns are rendered in); experiment-only items
// are appended after them. Dedup is on the NORMALIZED name, so "Dry Masoor Bhusa" arriving from both
// workbooks -- once as "Dry Masoor Bhusa Per Goat", once as "Dry Masoor Bhusa (kg)" -- is one row.
func catalogItemLabels(grid map[string]farmGrid, experiments []experimentShed) []string {
	out := feedItems(grid)
	seen := map[string]bool{}
	for _, name := range out {
		seen[configNormKey(name)] = true
	}
	// Sorted for determinism: map iteration over Kg is random, and an unstable display_order would
	// reorder the direction sheet's columns between two runs of the same seed.
	for _, row := range experiments {
		headers := make([]string, 0, len(row.Kg))
		for header := range row.Kg {
			headers = append(headers, header)
		}
		sort.Strings(headers)
		for _, header := range headers {
			name := feedItemName(header)
			if key := configNormKey(name); !seen[key] {
				seen[key] = true
				out = append(out, name)
			}
		}
	}
	return out
}

// resolveExperimentSheds maps each source (farm, shed name) to a REAL locations shed row, keyed
// "<farm>\x1f<normalized shed name>".
//
// SHED UUIDS ARE NEVER HARDCODED, for the same reason park UUIDs are not: they differ per database.
//
// AN UNRESOLVED SHED IS A HARD FAILURE. This is the single most important property of this function.
// Membership in feed_experiment_config IS the experiment flag, so dropping one shed here does not
// leave it visibly unconfigured -- it leaves it being fed from the per-head ration grid, producing a
// complete-looking feed sheet with roughly twice the right quantity on it. Every unresolvable shed is
// collected and reported together, so an operator fixing a naming drift sees the whole list rather
// than one name per re-run.
//
// Matching is on the NORMALIZED name (feed_config_norm's Go twin), so 'Godel 1 - Part 3' and
// 'Godel 1 -Part 3' are the same shed, and a duplicate normalized name within one park is itself a
// failure: with two candidates there is no non-arbitrary answer, and guessing would author an
// experiment against the wrong animals.
func resolveExperimentSheds(
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	parks map[string]string,
	experiments []experimentShed,
) (map[string]string, error) {
	farms := map[string]bool{}
	for _, row := range experiments {
		farms[row.Farm] = true
	}
	parkIDs := make([]string, 0, len(farms))
	parkFarmByID := map[string]string{}
	for farm := range farms {
		parkID, ok := parks[farm]
		if !ok {
			return nil, fmt.Errorf("experiment config names farm %q, which the ration grid did not resolve to a park", farm)
		}
		parkIDs = append(parkIDs, parkID)
		parkFarmByID[parkID] = farm
	}
	sort.Strings(parkIDs)

	// One set-based read for every involved park's sheds, then resolve each source row against the
	// result in memory. Never one query per shed.
	// scale-guard:ignore: one bounded set-based read of the ACTIVE sheds of the two live parks (~120 rows), executed once before seeding; it does not grow with herd size and is not in a request path.
	rows, err := pool.Query(ctx, `
SELECT parent_location_id::text, location_id::text, name
FROM locations
WHERE tenant_id = $1::uuid
  AND location_type = 'shed'
  AND status = 'active'
  AND parent_location_id = ANY($2::uuid[])`, tenantID, parkIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve experiment sheds: %w", err)
	}
	defer rows.Close()

	resolved := map[string]string{}
	ambiguous := map[string][]string{}
	for rows.Next() {
		var parkID, shedID, name string
		if err := rows.Scan(&parkID, &shedID, &name); err != nil {
			return nil, fmt.Errorf("resolve experiment sheds: %w", err)
		}
		key := parkFarmByID[parkID] + "\x1f" + configNormKey(name)
		if prev, exists := resolved[key]; exists {
			ambiguous[key] = append(ambiguous[key], prev, shedID)
			continue
		}
		resolved[key] = shedID
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve experiment sheds: %w", err)
	}

	out := map[string]string{}
	var missing, conflicting []string
	for _, row := range experiments {
		// The authored name is an OPERATIONAL LOCATION ("Castro 1", "Godel 1 - Part 3"), while the
		// catalog holds the PHYSICAL shed ("Castro", "Godel 1") with the partition stored per animal.
		// Resolve against the physical shed and carry the partition onto the row; matching the raw
		// name demanded an active partition-named shed row, which the canonical model does not have,
		// and made every one of the 34 experiment sheds unresolvable (2026-08-07).
		physicalShed, _ := oploc.SplitShedPartitionName(row.Shed)
		key := row.Farm + "\x1f" + configNormKey(physicalShed)
		if ids, bad := ambiguous[key]; bad {
			conflicting = append(conflicting, fmt.Sprintf("%s / %s -> %v", row.Farm, row.Shed, ids))
			continue
		}
		shedID, ok := resolved[key]
		if !ok {
			missing = append(missing, fmt.Sprintf("%s / %s", row.Farm, row.Shed))
			continue
		}
		out[key] = shedID
	}
	if len(conflicting) > 0 {
		sort.Strings(conflicting)
		return nil, fmt.Errorf(
			"experiment shed name(s) match more than one active shed in their park, so the experiment cannot be attached to a specific one: %s",
			strings.Join(conflicting, "; "))
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf(
			"experiment shed(s) %s do not resolve to an active locations row of location_type='shed' in their park; seeding without them would leave those sheds fed from the per-head ration grid instead of their authored absolute kg",
			strings.Join(missing, ", "))
	}
	fmt.Printf("resolved %d experiment sheds across %d park(s)\n", len(out), len(parkIDs))
	return out, nil
}

// seedExperiments loads the hand-entered experiment quantities.
//
// RECONCILIATION IS TWO-WAY, NOT THREE. feed_experiment_config is not effective-dated (see migration
// 000006), so there is no supersede branch:
//
//	no row                    -> INSERT
//	row, same values          -> no-op                     (the idempotent re-run: 0 inserted)
//	row, different values     -> UPDATE in place
//
// The UPDATE also forces status back to 'active'. A shed present in this source IS an experiment
// shed, and leaving a previously-withdrawn row retired while rewriting its quantity would store a
// number that nothing reads -- the direction path filters on status = 'active'.
//
// NOTHING IS RETIRED HERE. Rows this source does not name are counted and reported, never withdrawn:
// /feed/config authors the same table, and silently retiring a hand-authored experiment shed would
// move it back onto the per-head grid -- a change in what animals are fed, performed by a data load.
func seedExperiments(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	experiments []experimentShed,
	shedIDs map[string]string,
	st *stats,
) error {
	type cell struct {
		shedID    string
		partition string
		item      string
		kg        string
		count     int32
		category  string
	}
	var cells []cell
	locations := map[string]bool{}
	for _, row := range experiments {
		// Same split as the resolver: the authored name is an operational location, the catalog key
		// is the physical shed, and the partition rides onto the row so two partitions of one shed
		// are two rows rather than a unique-key collision.
		physicalShed, partitionLabel := oploc.SplitShedPartitionName(row.Shed)
		shedID, ok := shedIDs[row.Farm+"\x1f"+configNormKey(physicalShed)]
		if !ok {
			// Unreachable: resolveExperimentSheds fails closed on a missing shed before this runs. It
			// is still checked rather than indexed blindly, because the consequence of a silent zero
			// value here is a row written against the nil UUID.
			return fmt.Errorf("experiment shed %q in farm %s was not resolved", row.Shed, row.Farm)
		}
		// The stat the operator reads is "how many operational locations were seeded" -- 34
		// partitions across 8 physical sheds, not 8.
		locations[shedID+"\x1f"+partitionLabel] = true

		headers := make([]string, 0, len(row.Kg))
		for header := range row.Kg {
			headers = append(headers, header)
		}
		sort.Strings(headers)
		for _, header := range headers {
			cells = append(cells, cell{
				shedID:    shedID,
				partition: partitionLabel,
				item:      feedItemName(header),
				kg:        formatNumeric(row.Kg[header], 3),
				count:     int32(row.Count),
				category:  strings.TrimSpace(row.Category),
			})
		}
	}
	st.ExperimentShedsResolved = len(locations)
	if len(cells) == 0 {
		return nil
	}

	b := &pgx.Batch{}
	// 1. Update any existing row whose quantity, head count, arm or status differs.
	for _, c := range cells {
		b.Queue(`
UPDATE feed_experiment_config
SET absolute_kg = $4::numeric,
    head_count = $5,
    experiment_category = $6,
    status = 'active',
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND shed_id = $2::uuid
  -- partition_key is GENERATED, so comparing against the same expression the column is built from
  -- keeps Go and SQL on one normalization; a bare partition_label compare would treat "Part 3" and
  -- "part 3" as different partitions of the same shed.
  AND partition_key = CASE
        WHEN $7::text IS NULL OR btrim($7::text) = '' THEN 'whole'
        ELSE feed_config_norm($7::text)
      END
  AND feed_item_key = feed_config_norm($3)
  AND (absolute_kg, head_count, experiment_category, status)
      IS DISTINCT FROM ($4::numeric, $5::integer, $6::text, 'active'::text)`,
			tenantID, c.shedID, c.item, c.kg, c.count, c.category, c.partition)
	}
	// 2. Insert wherever no row exists. park_id is read from the shed's own parent rather than passed
	//    in, so the row's park can never disagree with the shed's actual placement -- the pair is the
	//    natural key the direction path filters on.
	for _, c := range cells {
		b.Queue(`
INSERT INTO feed_experiment_config (tenant_id, park_id, shed_id, partition_label, feed_item_label, absolute_kg, head_count, experiment_category, status)
SELECT $1::uuid, l.parent_location_id, l.location_id, NULLIF(btrim($7::text), ''), $3, $4::numeric, $5, $6, 'active'
FROM locations l
WHERE l.tenant_id = $1::uuid AND l.location_id = $2::uuid
  AND NOT EXISTS (
    SELECT 1 FROM feed_experiment_config e
    WHERE e.tenant_id = $1::uuid
      AND e.shed_id = $2::uuid
      AND e.partition_key = CASE
            WHEN $7::text IS NULL OR btrim($7::text) = '' THEN 'whole'
            ELSE feed_config_norm($7::text)
          END
      AND e.feed_item_key = feed_config_norm($3)
  )`, tenantID, c.shedID, c.item, c.kg, c.count, c.category, c.partition)
	}

	// scale-guard:ignore: this IS the batched form -- one SendBatch for the whole experiment cell set (170 rows today, bounded by the authored experiment size), never one round trip per cell.
	br := tx.SendBatch(ctx, b)
	var firstErr error
	updated, inserted := 0, 0
	for i := 0; i < len(cells)*2; i++ {
		// scale-guard:ignore: drains results already returned by the single SendBatch above; each call reads a buffered result, it does not issue a query.
		tag, err := br.Exec()
		if err != nil && firstErr == nil {
			firstErr = err
			continue
		}
		if i < len(cells) {
			updated += int(tag.RowsAffected())
		} else {
			inserted += int(tag.RowsAffected())
		}
	}
	if cerr := br.Close(); cerr != nil && firstErr == nil {
		firstErr = cerr
	}
	if firstErr != nil {
		return firstErr
	}

	st.ExperimentRowsInserted += inserted
	st.ExperimentRowsUpdated += updated
	st.ExperimentRowsUnchanged += len(cells) - inserted - updated

	// Count, do not touch. See the function comment.
	locationList := make([]string, 0, len(locations))
	for location := range locations {
		parts := strings.SplitN(location, "\x1f", 2)
		partition := ""
		if len(parts) == 2 {
			partition = domainPartitionKey(parts[1])
		}
		locationList = append(locationList, parts[0]+"\x1f"+partition)
	}
	sort.Strings(locationList)
	// scale-guard:ignore: one bounded aggregate over this tenant's hand-authored experiment rows (170 today); runs once per seed, not in a request path.
	if err := tx.QueryRow(ctx, `
SELECT count(*)
FROM feed_experiment_config
WHERE tenant_id = $1::uuid
  AND status = 'active'
  AND NOT ((shed_id::text || E'\x1f' || partition_key) = ANY($2::text[]))`, tenantID, locationList).Scan(&st.ExperimentRowsOutsideSource); err != nil {
		return fmt.Errorf("count experiment rows outside source: %w", err)
	}
	return nil
}

func domainPartitionKey(partitionLabel string) string {
	normalized := configNormKey(partitionLabel)
	if normalized == "" {
		return "whole"
	}
	return normalized
}

// ---- guards ----

func checkSchemaReady(ctx context.Context, pool *pgxpool.Pool) error {
	var missing []string
	for _, table := range []string{
		"feed_ration_groups",
		"feed_shed_tags",
		"feed_item_catalog",
		"feed_ration_rates",
		"feed_session_templates",
		// Migration 000004. Listed here for the same reason as the 000003 tables: a seed that runs
		// against a database missing it would report success while every park's dispatch clock stayed
		// absent.
		"feed_schedule_config",
		// Migration 000005. Without it the seed would report success while every park's session
		// recipe stayed absent -- and an absent recipe blocks every shed, so the gap is loud but
		// only much later.
		"feed_session_template_items",
		// Migration 000003's table, but only loaded from this seed as of the experiment workbook
		// import. Listed for the usual reason: without it the seed would report success while all 34
		// experiment sheds stayed absent -- and absent here means "fed from the per-head grid", which
		// is a wrong quantity rather than a visible gap.
		"feed_experiment_config",
	} {
		var reg *string
		// scale-guard:ignore: fixed literal table list (the feed config migrations' own tables), not data-driven and never grows with tenants/rows; runs once at startup before any seeding.
		if err := pool.QueryRow(ctx, `SELECT to_regclass('public.'||$1)::text`, table).Scan(&reg); err != nil {
			return fmt.Errorf("check %s: %w", table, err)
		}
		if reg == nil {
			missing = append(missing, table)
		}
	}
	var fn *string
	if err := pool.QueryRow(ctx, `SELECT to_regprocedure('public.feed_config_norm(text)')::text`).Scan(&fn); err != nil {
		return fmt.Errorf("check feed_config_norm: %w", err)
	}
	if fn == nil {
		missing = append(missing, "feed_config_norm(text)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("seed-feed-ration requires migrations 000003, 000004 and 000005, which have not been fully applied (missing: %s)", strings.Join(missing, ", "))
	}
	return nil
}

func validateTarget(env, databaseURL string) error {
	if strings.EqualFold(strings.TrimSpace(env), "stg") {
		return localtarget.ValidateStagingCloudSQLDatabaseTarget("seed-feed-ration", env, databaseURL)
	}
	return localtarget.ValidateLocalDatabaseTarget("seed-feed-ration", env, databaseURL, "local", "dev", "test")
}

// resolveParks maps each source farm key to a REAL locations park row. Park UUIDs are never
// hardcoded, and a missing park fails loudly rather than skipping half the herd's configuration.
func resolveParks(ctx context.Context, pool *pgxpool.Pool, tenantID string, grid map[string]farmGrid) (map[string]string, error) {
	farms := sortedKeys(grid)
	codes := make([]string, 0, len(farms))
	for _, farm := range farms {
		codes = append(codes, parkCodes[farm])
	}

	// One set-based read for every park code, then resolve each farm against the result. The
	// per-farm validation below is unchanged: a missing or inactive park still fails loudly, in
	// the same sorted-farm order as before.
	type park struct{ id, status string }
	resolved := make(map[string]park, len(codes))
	rows, err := pool.Query(ctx, `
SELECT location_code, location_id::text, status
FROM locations
WHERE tenant_id = $1::uuid AND location_type = 'park' AND location_code = ANY($2::text[])`, tenantID, codes)
	if err != nil {
		return nil, fmt.Errorf("resolve parks %v: %w", codes, err)
	}
	defer rows.Close()
	for rows.Next() {
		var code, id, status string
		if err := rows.Scan(&code, &id, &status); err != nil {
			return nil, fmt.Errorf("resolve parks %v: %w", codes, err)
		}
		resolved[code] = park{id: id, status: status}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve parks %v: %w", codes, err)
	}

	out := map[string]string{}
	for _, farm := range farms {
		code := parkCodes[farm]
		p, ok := resolved[code]
		if !ok {
			return nil, fmt.Errorf("park %q (source farm %q) has no locations row of location_type='park' for tenant %s -- seed the park before loading ration config", code, farm, tenantID)
		}
		if p.status != "active" {
			return nil, fmt.Errorf("park %q resolved to location %s with status=%q, expected 'active'", code, p.id, p.status)
		}
		out[farm] = p.id
		fmt.Printf("resolved park %s -> %s (%s)\n", farm, p.id, code)
	}
	return out, nil
}

// ---- vocabulary seeds (insert-if-missing, matched on the NORMALIZED key) ----

// The vocabulary seeds below are one set-based statement each rather than a statement per row.
// Two properties of the per-row loop they replace are preserved deliberately:
//
//   - insert-if-missing on the NORMALIZED key, so a label whitespace/case variant of an existing
//     row is not re-inserted (the WHERE NOT EXISTS);
//   - first-one-wins if two INPUT labels normalize to the same key. The loop got this for free
//     (the second iteration saw the first's row); a single statement does not, because NOT EXISTS
//     cannot see rows the same statement is inserting. DISTINCT ON (feed_config_norm(...)) ordered
//     by WITH ORDINALITY restores it, keeping the first label in the same order the loop walked
//     instead of failing on the natural-key unique index.
func seedGroups(ctx context.Context, tx pgx.Tx, tenantID string, st *stats) error {
	breeds := make([]string, 0, len(breedRationGroups))
	groups := make([]string, 0, len(breedRationGroups))
	for _, g := range breedRationGroups {
		breeds = append(breeds, g.BreedLabel)
		groups = append(groups, g.GroupLabel)
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO feed_ration_groups (tenant_id, breed_label, ration_group_label)
SELECT $1::uuid, v.breed_label, v.group_label
FROM (
  SELECT DISTINCT ON (feed_config_norm(breed_label)) breed_label, group_label
  FROM unnest($2::text[], $3::text[]) WITH ORDINALITY AS t(breed_label, group_label, ord)
  ORDER BY feed_config_norm(breed_label), ord
) AS v
WHERE NOT EXISTS (
  SELECT 1 FROM feed_ration_groups g
  WHERE g.tenant_id = $1::uuid AND g.breed_key = feed_config_norm(v.breed_label)
)`, tenantID, breeds, groups)
	if err != nil {
		return err
	}
	st.GroupsInserted += int(tag.RowsAffected())
	return nil
}

func seedTags(ctx context.Context, tx pgx.Tx, tenantID string, grid map[string]farmGrid, st *stats) error {
	kind, order, err := deriveTagKinds(grid)
	if err != nil {
		return err
	}
	labels := sortedKeys(kind)
	appliesTo := make([]string, 0, len(labels))
	orders := make([]int32, 0, len(labels))
	for _, label := range labels {
		appliesTo = append(appliesTo, kind[label])
		orders = append(orders, int32(order[label]))
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO feed_shed_tags (tenant_id, shed_tag_label, applies_to, display_order)
SELECT $1::uuid, v.shed_tag_label, v.applies_to, v.display_order
FROM (
  SELECT DISTINCT ON (feed_config_norm(shed_tag_label)) shed_tag_label, applies_to, display_order
  FROM unnest($2::text[], $3::text[], $4::int[]) WITH ORDINALITY AS t(shed_tag_label, applies_to, display_order, ord)
  ORDER BY feed_config_norm(shed_tag_label), ord
) AS v
WHERE NOT EXISTS (
  SELECT 1 FROM feed_shed_tags s
  WHERE s.tenant_id = $1::uuid AND s.shed_tag_key = feed_config_norm(v.shed_tag_label)
)`, tenantID, labels, appliesTo, orders)
	if err != nil {
		return err
	}
	st.TagsInserted += int(tag.RowsAffected())
	return nil
}

// deriveTagKinds classifies every tag as 'kid' or 'adult' from the grid itself: a tag appearing
// under the 'Kid' ration group is a kid tag. It ASSERTS the two sets are disjoint rather than
// assuming it -- a tag used by both courses would make applies_to (a single-valued column)
// unrepresentable, and silently picking one would mis-tag live animals.
func deriveTagKinds(grid map[string]farmGrid) (map[string]string, map[string]int, error) {
	kid := map[string]bool{}
	adult := map[string]bool{}
	for _, farm := range sortedKeys(grid) {
		for _, row := range grid[farm].Rows {
			if row.Group == kidRationGroup {
				kid[row.Tag] = true
			} else {
				adult[row.Tag] = true
			}
		}
	}
	var overlap []string
	for tag := range kid {
		if adult[tag] {
			overlap = append(overlap, tag)
		}
	}
	if len(overlap) > 0 {
		sort.Strings(overlap)
		return nil, nil, fmt.Errorf("shed tag(s) %v appear under both the Kid ration group and an adult group; applies_to cannot represent both", overlap)
	}
	kinds := map[string]string{}
	for tag := range adult {
		kinds[tag] = "adult"
	}
	for tag := range kid {
		kinds[tag] = "kid"
	}
	order := map[string]int{}
	for i, tag := range sortedKeys(kinds) {
		order[tag] = i
	}
	return kinds, order, nil
}

// seedItems loads the tenant's feed VOCABULARY. It takes the label list rather than deriving it, so
// the caller decides which sources contribute -- today the ration grid plus the experiment workbook.
func seedItems(ctx context.Context, tx pgx.Tx, tenantID string, labels []string, st *stats) error {
	orders := make([]int32, 0, len(labels))
	for i := range labels {
		orders = append(orders, int32(i))
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO feed_item_catalog (tenant_id, feed_item_label, display_order)
SELECT $1::uuid, v.feed_item_label, v.display_order
FROM (
  SELECT DISTINCT ON (feed_config_norm(feed_item_label)) feed_item_label, display_order
  FROM unnest($2::text[], $3::int[]) WITH ORDINALITY AS t(feed_item_label, display_order, ord)
  ORDER BY feed_config_norm(feed_item_label), ord
) AS v
WHERE NOT EXISTS (
  SELECT 1 FROM feed_item_catalog c
  WHERE c.tenant_id = $1::uuid AND c.feed_item_key = feed_config_norm(v.feed_item_label)
)`, tenantID, labels, orders)
	if err != nil {
		return err
	}
	st.ItemsInserted += int(tag.RowsAffected())
	return nil
}

func seedSessions(ctx context.Context, tx pgx.Tx, tenantID string, parks map[string]string, st *stats) error {
	// The table cannot CHECK a cross-row sum, so the writer must. A template that does not sum to
	// 1.0 would silently under- or over-feed every shed in the park.
	total := 0.0
	for _, s := range defaultSessions {
		total += s.Split
	}
	if total < 0.9999 || total > 1.0001 {
		return fmt.Errorf("default session template split_fraction sums to %v, must sum to 1.0", total)
	}
	// The park x session cross product is expanded into parallel arrays and written by one
	// statement. Natural key here is (tenant_id, park_id, session_no), so the same first-one-wins
	// dedup the per-row loop had is expressed as DISTINCT ON (park_id, session_no) by ordinality.
	n := len(parks) * len(defaultSessions)
	parkIDs := make([]string, 0, n)
	sessionNos := make([]int32, 0, n)
	sessionLabels := make([]string, 0, n)
	splits := make([]string, 0, n)
	for _, farm := range sortedKeys(parks) {
		for _, s := range defaultSessions {
			parkIDs = append(parkIDs, parks[farm])
			sessionNos = append(sessionNos, int32(s.SessionNo))
			sessionLabels = append(sessionLabels, s.Label)
			splits = append(splits, formatNumeric(s.Split, 4))
		}
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO feed_session_templates (tenant_id, park_id, session_no, session_label, split_fraction, display_order)
SELECT $1::uuid, v.park_id, v.session_no, v.session_label, v.split_fraction::numeric, v.session_no
FROM (
  SELECT DISTINCT ON (park_id, session_no) park_id, session_no, session_label, split_fraction
  FROM unnest($2::uuid[], $3::int[], $4::text[], $5::text[]) WITH ORDINALITY AS t(park_id, session_no, session_label, split_fraction, ord)
  ORDER BY park_id, session_no, ord
) AS v
WHERE NOT EXISTS (
  SELECT 1 FROM feed_session_templates fst
  WHERE fst.tenant_id = $1::uuid AND fst.park_id = v.park_id AND fst.session_no = v.session_no
)`, tenantID, parkIDs, sessionNos, sessionLabels, splits)
	if err != nil {
		return err
	}
	st.SessionsInserted += int(tag.RowsAffected())
	return nil
}

// ---- session template items (the workbook's `Template` tab) ----

// seedSessionItems loads the five declared feed slots for every park x session.
//
// It reconciles against the currently-open row with the SAME three-way behaviour as seedRates and
// seedSchedules, because what a session consists of is an effective-dated farm decision like any
// other: a changed slot closes yesterday's row rather than overwriting it, so "what were we packing
// for CBE session 2 in March" stays answerable.
//
//	no open row                 -> INSERT
//	open row, same feed item    -> no-op          (this is the idempotent re-run: 0 inserted)
//	open row, different item    -> supersede, or correct in place if authored TODAY
//
// The closes are queued ahead of the inserts in one batch, which is what makes a slot SWAP legal:
// the partial unique index forbidding one feed item in two live slots of a session would reject the
// new rows if the old ones were still open.
func seedSessionItems(ctx context.Context, tx pgx.Tx, tenantID string, parks map[string]string, grid map[string]farmGrid, businessDate string, st *stats) error {
	// A declared slot naming a feed with no rates would author a recipe that BLOCKS every shed in
	// the park -- migration 000003's rule is that a requested item with no rate blocks rather than
	// feeding 0. Assert the slots resolve against the catalog this same seed is loading, rather than
	// discovering it later as an unexplained wall of blocked cells.
	known := map[string]bool{}
	for _, name := range feedItems(grid) {
		known[configNormKey(name)] = true
	}
	var unknown []string
	for _, slot := range normalWorkflowFeedSlots {
		if !known[configNormKey(slot)] {
			unknown = append(unknown, slot)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf(
			"declared session feed slot(s) %v are not in the source grid's feed-item columns; seeding them would author a recipe that blocks every shed",
			unknown)
	}

	type pending struct {
		parkID    string
		sessionNo int32
		slotNo    int32
		item      string
	}
	var rows []pending
	for _, farm := range sortedKeys(parks) {
		for _, session := range defaultSessions {
			for i, item := range normalWorkflowFeedSlots {
				rows = append(rows, pending{parks[farm], int32(session.SessionNo), int32(i + 1), item})
			}
		}
	}
	if len(rows) == 0 {
		return nil
	}

	b := &pgx.Batch{}
	// 1. Close any open slot whose feed item changed and that was authored on an EARLIER day.
	for _, p := range rows {
		b.Queue(`
UPDATE feed_session_template_items
SET valid_to = $5::date, updated_at = now()
WHERE tenant_id = $1::uuid AND park_id = $2::uuid
  AND session_no = $3 AND slot_no = $4
  AND valid_to IS NULL
  AND valid_from < $5::date
  AND feed_item_key IS DISTINCT FROM feed_config_norm($6)`,
			tenantID, p.parkID, p.sessionNo, p.slotNo, businessDate, p.item)
	}
	// 2. Correct in place any open slot authored TODAY whose feed item changed -- a same-day
	//    re-author cannot be given a window without violating valid_to > valid_from.
	for _, p := range rows {
		b.Queue(`
UPDATE feed_session_template_items
SET feed_item_label = $6, updated_at = now()
WHERE tenant_id = $1::uuid AND park_id = $2::uuid
  AND session_no = $3 AND slot_no = $4
  AND valid_to IS NULL
  AND valid_from = $5::date
  AND feed_item_key IS DISTINCT FROM feed_config_norm($6)`,
			tenantID, p.parkID, p.sessionNo, p.slotNo, businessDate, p.item)
	}
	// 3. Insert wherever no open slot remains.
	for _, p := range rows {
		b.Queue(`
INSERT INTO feed_session_template_items (tenant_id, park_id, session_no, slot_no, feed_item_label, valid_from)
SELECT $1::uuid, $2::uuid, $3, $4, $6, $5::date
WHERE NOT EXISTS (
  SELECT 1 FROM feed_session_template_items
  WHERE tenant_id = $1::uuid AND park_id = $2::uuid
    AND session_no = $3 AND slot_no = $4
    AND valid_to IS NULL
)`, tenantID, p.parkID, p.sessionNo, p.slotNo, businessDate, p.item)
	}

	// scale-guard:ignore: this IS the batched form -- one SendBatch for the whole parks x sessions x slots set (20 rows today, bounded by park count), never one round trip per slot.
	br := tx.SendBatch(ctx, b)
	var firstErr error
	closed, corrected, inserted := 0, 0, 0
	for i := 0; i < len(rows)*3; i++ {
		// scale-guard:ignore: drains results already returned by the single SendBatch above; each call reads a buffered result, it does not issue a query.
		tag, err := br.Exec()
		if err != nil && firstErr == nil {
			firstErr = err
			continue
		}
		switch {
		case i < len(rows):
			closed += int(tag.RowsAffected())
		case i < 2*len(rows):
			corrected += int(tag.RowsAffected())
		default:
			inserted += int(tag.RowsAffected())
		}
	}
	if cerr := br.Close(); cerr != nil && firstErr == nil {
		firstErr = cerr
	}
	if firstErr != nil {
		return firstErr
	}

	st.SessionItemsSuperseded += closed
	st.SessionItemsCorrected += corrected
	// An insert that followed a close is a supersede, not a newly declared slot.
	st.SessionItemsInserted += inserted - closed
	st.SessionItemsUnchanged += len(rows) - closed - corrected - (inserted - closed)
	return nil
}

// configNormKey is the Go twin of the database's feed_config_norm(), used only to compare declared
// slots against the grid's feed items before either reaches SQL. It must stay identical to the
// migration's definition: trim, casefold, collapse runs of whitespace/underscore/hyphen to one '_'.
func configNormKey(value string) string {
	return strings.ToLower(configNormRuns.ReplaceAllString(strings.TrimSpace(value), "_"))
}

var configNormRuns = regexp.MustCompile(`[\s_-]+`)

// ---- rates ----

// seedRates reconciles the grid against the currently-open rate rows. See the package comment for
// the three-way behaviour. Batched so 1540 rates are a handful of round trips, not 1540.
func seedRates(ctx context.Context, tx pgx.Tx, tenantID string, parks map[string]string, grid map[string]farmGrid, businessDate string, st *stats) error {
	for _, farm := range sortedKeys(grid) {
		parkID := parks[farm]
		g := grid[farm]
		items := g.Headers[2:]

		type pending struct {
			group, tag, item string
			grams            string
		}
		var batchRows []pending
		for _, row := range g.Rows {
			for _, item := range items {
				grams, ok := row.Rates[item]
				if !ok {
					// A blank grid cell is NOT a zero, and it is NOT an error either -- it means
					// this feed item is not offered for this (group, tag). The alternative
					// roughages (Hybrid / COFS / Hedge Lucerne / Dry Maize) are inter-feed
					// SUBSTITUTION options: the sheet prices each one, but only one roughage is
					// ever actually fed, so ~12 of 77 rows legitimately leave them blank.
					//
					// Writing 0 here would be the silent-starvation collapse the schema exists to
					// prevent, so we write NO ROW. Absence stays the single representation of
					// "not configured", and the safety rule is enforced where it matters: at
					// LOOKUP time, a requested item with no rate row BLOCKS the shed rather than
					// feeding it 0 kg. Skips are counted so a genuine gap is still visible.
					st.RatesSkipped++
					continue
				}
				// The rate carries the FEED NAME, the same string seedItems put in the catalog.
				// Both sides route through feedItemName, so their GENERATED feed_item_key columns
				// are equal by construction and the grid cannot orphan itself.
				batchRows = append(batchRows, pending{row.Group, row.Tag, feedItemName(item), formatNumeric(grams, 3)})
			}
		}

		const size = 200
		for start := 0; start < len(batchRows); start += size {
			end := start + size
			if end > len(batchRows) {
				end = len(batchRows)
			}
			chunk := batchRows[start:end]

			// 1. Close any open row whose rate changed and that was authored on an EARLIER
			//    business day. valid_to = today keeps valid_to > valid_from and preserves the old
			//    rate as history instead of overwriting it.
			// 2. Correct in place any open row authored TODAY whose rate changed -- a same-day
			//    re-author, which cannot be given a window without violating valid_to > valid_from.
			// 3. Insert a row wherever no open row remains.
			b := &pgx.Batch{}
			for _, p := range chunk {
				b.Queue(`
UPDATE feed_ration_rates
SET valid_to = $6::date, updated_at = now()
WHERE tenant_id = $1::uuid AND park_id = $2::uuid
  AND ration_group_key = feed_config_norm($3)
  AND shed_tag_key = feed_config_norm($4)
  AND feed_item_key = feed_config_norm($5)
  AND valid_to IS NULL
  AND valid_from < $6::date
  AND grams_per_head IS DISTINCT FROM $7::numeric`,
					tenantID, parkID, p.group, p.tag, p.item, businessDate, p.grams)
			}
			for _, p := range chunk {
				b.Queue(`
UPDATE feed_ration_rates
SET grams_per_head = $7::numeric, updated_at = now()
WHERE tenant_id = $1::uuid AND park_id = $2::uuid
  AND ration_group_key = feed_config_norm($3)
  AND shed_tag_key = feed_config_norm($4)
  AND feed_item_key = feed_config_norm($5)
  AND valid_to IS NULL
  AND valid_from = $6::date
  AND grams_per_head IS DISTINCT FROM $7::numeric`,
					tenantID, parkID, p.group, p.tag, p.item, businessDate, p.grams)
			}
			for _, p := range chunk {
				b.Queue(`
INSERT INTO feed_ration_rates (tenant_id, park_id, ration_group_label, shed_tag_label, feed_item_label, grams_per_head, valid_from, source_system)
SELECT $1::uuid, $2::uuid, $3, $4, $5, $7::numeric, $6::date, 'source_ration_grid'
WHERE NOT EXISTS (
  SELECT 1 FROM feed_ration_rates
  WHERE tenant_id = $1::uuid AND park_id = $2::uuid
    AND ration_group_key = feed_config_norm($3)
    AND shed_tag_key = feed_config_norm($4)
    AND feed_item_key = feed_config_norm($5)
    AND valid_to IS NULL
)`, tenantID, parkID, p.group, p.tag, p.item, businessDate, p.grams)
			}

			// scale-guard:ignore: this IS the batched form -- the enclosing loop walks 200-row chunks, so it costs one round trip per chunk (~8 for 1540 rates), never one per rate row.
			br := tx.SendBatch(ctx, b)
			var firstErr error
			closed, corrected, inserted := 0, 0, 0
			for i := 0; i < len(chunk)*3; i++ {
				// scale-guard:ignore: drains results already returned by the single SendBatch above; each call reads a buffered result, it does not issue a query.
				tag, err := br.Exec()
				if err != nil && firstErr == nil {
					firstErr = err
					continue
				}
				switch {
				case i < len(chunk):
					closed += int(tag.RowsAffected())
				case i < 2*len(chunk):
					corrected += int(tag.RowsAffected())
				default:
					inserted += int(tag.RowsAffected())
				}
			}
			if cerr := br.Close(); cerr != nil && firstErr == nil {
				firstErr = cerr
			}
			if firstErr != nil {
				return firstErr
			}

			st.RatesSuperseded += closed
			st.RatesCorrected += corrected
			// An insert that followed a close is a supersede, not a fresh rate.
			st.RatesInserted += inserted - closed
			st.RatesUnchanged += len(chunk) - closed - corrected - (inserted - closed)
		}
	}
	return nil
}

// ---- schedule config ----

// seedSchedules reconciles the default per-park, per-workflow dispatch clock against the currently
// open feed_schedule_config rows, using the SAME three-way behaviour as seedRates:
//
//	no open row               -> INSERT
//	open row, same clock      -> no-op
//	open row, different clock -> supersede (close yesterday's row, open a new one), unless that open
//	                             row was itself authored TODAY, which is corrected in place because
//	                             valid_to > valid_from cannot hold for a zero-length window.
//
// It is one batch: parks x workflows is 4 rows here and stays a handful even at full farm count, so
// the chunking seedRates needs for 1540 rates would be noise.
func seedSchedules(ctx context.Context, tx pgx.Tx, tenantID string, parks map[string]string, businessDate string, st *stats) error {
	type pending struct {
		parkID, workflow      string
		direction, correction string
		transport             string
	}
	var rows []pending
	for _, farm := range sortedKeys(parks) {
		for _, s := range defaultSchedules {
			rows = append(rows, pending{parks[farm], s.Workflow, s.DirectionTime, s.CorrectionTime, s.TransportTime})
		}
	}
	if len(rows) == 0 {
		return nil
	}

	b := &pgx.Batch{}
	// 1. Close any open row whose clock changed and that was authored on an EARLIER business day.
	for _, p := range rows {
		b.Queue(`
UPDATE feed_schedule_config
SET valid_to = $3::date, updated_at = now()
WHERE tenant_id = $1::uuid AND park_id = $2::uuid
  AND workflow = $4
  AND valid_to IS NULL
  AND valid_from < $3::date
  AND (direction_time, correction_time, transport_time)
      IS DISTINCT FROM ($5::time, $6::time, $7::time)`,
			tenantID, p.parkID, businessDate, p.workflow, p.direction, p.correction, p.transport)
	}
	// 2. Correct in place any open row authored TODAY whose clock changed.
	for _, p := range rows {
		b.Queue(`
UPDATE feed_schedule_config
SET direction_time = $5::time, correction_time = $6::time, transport_time = $7::time, updated_at = now()
WHERE tenant_id = $1::uuid AND park_id = $2::uuid
  AND workflow = $4
  AND valid_to IS NULL
  AND valid_from = $3::date
  AND (direction_time, correction_time, transport_time)
      IS DISTINCT FROM ($5::time, $6::time, $7::time)`,
			tenantID, p.parkID, businessDate, p.workflow, p.direction, p.correction, p.transport)
	}
	// 3. Insert wherever no open row remains.
	for _, p := range rows {
		b.Queue(`
INSERT INTO feed_schedule_config (tenant_id, park_id, workflow, direction_time, correction_time, transport_time, valid_from)
SELECT $1::uuid, $2::uuid, $4, $5::time, $6::time, $7::time, $3::date
WHERE NOT EXISTS (
  SELECT 1 FROM feed_schedule_config
  WHERE tenant_id = $1::uuid AND park_id = $2::uuid
    AND workflow = $4
    AND valid_to IS NULL
)`, tenantID, p.parkID, businessDate, p.workflow, p.direction, p.correction, p.transport)
	}

	// scale-guard:ignore: this IS the batched form -- one SendBatch for the whole parks x workflows set (4 rows today, bounded by park count), never one round trip per row.
	br := tx.SendBatch(ctx, b)
	var firstErr error
	closed, corrected, inserted := 0, 0, 0
	for i := 0; i < len(rows)*3; i++ {
		// scale-guard:ignore: drains results already returned by the single SendBatch above; each call reads a buffered result, it does not issue a query.
		tag, err := br.Exec()
		if err != nil && firstErr == nil {
			firstErr = err
			continue
		}
		switch {
		case i < len(rows):
			closed += int(tag.RowsAffected())
		case i < 2*len(rows):
			corrected += int(tag.RowsAffected())
		default:
			inserted += int(tag.RowsAffected())
		}
	}
	if cerr := br.Close(); cerr != nil && firstErr == nil {
		firstErr = cerr
	}
	if firstErr != nil {
		return firstErr
	}

	st.SchedulesSuperseded += closed
	st.SchedulesCorrected += corrected
	// An insert that followed a close is a supersede, not a fresh clock.
	st.SchedulesInserted += inserted - closed
	st.SchedulesUnchanged += len(rows) - closed - corrected - (inserted - closed)
	return nil
}

// ---- helpers ----

// formatNumeric renders a float as a fixed-precision decimal string so it is bound as ::numeric
// rather than travelling through float64 rounding on the way into an exact numeric column.
func formatNumeric(v float64, places int) string {
	return strconv.FormatFloat(v, 'f', places, 64)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
