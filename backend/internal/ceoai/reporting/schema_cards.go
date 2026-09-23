// Package reporting owns the repo-side description of the governed ceo_ai.*
// reporting schema the leadership assistant reads: the schema CARDS (this
// file) plus the Postgres-gated grain/identity proofs (the *_test.go files).
//
// A schema card is the single, repo-owned statement of what one ceo_ai.* view
// is FOR, at what GRAIN its rows sit, which column carries its business day
// (if any), which columns are group-by-able, and every column with its type.
// It is what the planner prompt renders (adapters/vertex/prompt.go), what the
// window guard consults (sqlguard.ValidateWindow), and what the repair loop
// hands back to the model with a rejection. Plan v3 D1.1.
//
// Contract, machine-checked:
//   - one card per `CREATE OR REPLACE VIEW ceo_ai.<name>` in
//     backend/migrations/postgres (tools/agent-hooks/check-ceo-ai-schema-cards.mjs,
//     `make ceo-ai-schema-card-guard`);
//   - card columns == information_schema.columns for the view, both directions
//     (TestSchemaCardsMatchInformationSchema, Postgres-gated);
//   - no card column is spelled like a sqlguard banned keyword
//     (TestSchemaCardsNoBannedKeywordColumns);
//   - every card carries tenant_id (TestEveryCardHasTenantIDColumn), because
//     the executor binds the session tenant to it on every read.
//
// Adding a view: add its card here (see docs/ceo-ai/schema-cards.md).
package reporting

import (
	"fmt"
	"sort"
	"strings"
)

// Column is one view column with its Postgres type in the short spelling
// PGTypeShort produces (uuid, text, date, timestamptz, bigint, integer,
// numeric, double, boolean, jsonb).
type Column struct {
	Name string
	Type string
}

// SchemaCard describes one ceo_ai.* view for the planner, the guard and the
// repair loop.
type SchemaCard struct {
	// Name is the bare view name (no ceo_ai. prefix).
	Name string
	// Purpose is one business sentence: what question this view answers.
	Purpose string
	// Grain is what ONE ROW is (e.g. "one row per park per business day").
	Grain string
	// DateColumn is the business-day column a period filter binds to, or ""
	// for a current-state view. sqlguard.ValidateWindow requires a resolved
	// window to be bound to this column; when it is "", a period question is
	// answered "as of now" instead.
	DateColumn string
	// ParkColumn is the park label/id column the planner may filter on, or "".
	ParkColumn string
	// TenantScopedColumns are the columns that carry tenant scope; tenant_id
	// is always present and is the one the executor binds.
	TenantScopedColumns []string
	// GroupByColumns are the dimension columns a leadership breakdown may
	// group on. Measures and free-text columns are deliberately excluded.
	GroupByColumns []string
	// Columns is the full ordered column list, matching information_schema.
	Columns []Column
	// AggregateOnly marks a per-entity base view (one row per animal, task,
	// load, obligation, completion): the planner must aggregate (count/sum
	// grouped by a dimension) rather than dump rows. Per-animal answers for the
	// caller's tenant are still allowed by the guard; this is a prompt hint.
	AggregateOnly bool
	// NeverAverage lists columns that are already ratios, averages, medians,
	// percentiles or per-entity caps and must not be re-averaged or summed
	// across rows.
	NeverAverage []string
	// Route is the admin-web href the composer may offer as the drill-down
	// page for this view.
	Route string
}

// CardName implements sqlguard.SchemaCardLike.
func (c SchemaCard) CardName() string { return c.Name }

// CardDateColumn implements sqlguard.SchemaCardLike.
func (c SchemaCard) CardDateColumn() string { return c.DateColumn }

// AlternateDateColumns implements sqlguard.AlternateDateColumnsCard: the
// card's other date-typed columns, on which a question naming THAT date
// ("loads purchased last quarter" -> purchase_date) may bind its period.
// Only on a view that has a business-day column: a current-state view still
// answers "as of now".
func (c SchemaCard) AlternateDateColumns() []string {
	if c.DateColumn == "" {
		return nil
	}
	var out []string
	for _, col := range c.Columns {
		if col.Type == dateT && !strings.EqualFold(col.Name, c.DateColumn) {
			out = append(out, col.Name)
		}
	}
	return out
}

// HasColumn reports whether the card lists a column with that name.
func (c SchemaCard) HasColumn(name string) bool {
	for _, col := range c.Columns {
		if strings.EqualFold(col.Name, name) {
			return true
		}
	}
	return false
}

// ColumnNames returns the ordered column names.
func (c SchemaCard) ColumnNames() []string {
	out := make([]string, len(c.Columns))
	for i, col := range c.Columns {
		out[i] = col.Name
	}
	return out
}

// RenderCompact renders the card as the prompt block the planner sees:
// name — purpose — grain — date/park columns — column list. Column types are
// abbreviated (see TypeLegend) so 28 cards stay within a bounded token budget.
func (c SchemaCard) RenderCompact() string {
	var b strings.Builder
	b.WriteString("- ceo_ai.")
	b.WriteString(c.Name)
	b.WriteString(": ")
	b.WriteString(c.Purpose)
	b.WriteString(" Row=")
	b.WriteString(c.Grain)
	b.WriteString(".")
	if c.DateColumn != "" {
		b.WriteString(" date_col=")
		b.WriteString(c.DateColumn)
		b.WriteString(".")
	} else {
		b.WriteString(" current-state, no date col.")
	}
	if c.AggregateOnly {
		b.WriteString(" Aggregate only.")
	}
	if len(c.NeverAverage) > 0 {
		b.WriteString(" Never avg/sum: ")
		b.WriteString(strings.Join(c.NeverAverage, ","))
		b.WriteString(".")
	}
	b.WriteString(" Cols: ")
	parts := make([]string, len(c.Columns))
	for i, col := range c.Columns {
		parts[i] = col.Name + typeSuffix(col.Type)
	}
	b.WriteString(strings.Join(parts, ","))
	return b.String()
}

// TypeLegend explains the column type suffixes RenderCompact emits. It is
// printed once at the top of the card block.
const TypeLegend = "Column suffixes: (none)=text #=integer/bigint/numeric/double ~=date @=timestamptz ?=boolean $=uuid {}=jsonb"

func typeSuffix(t string) string {
	switch t {
	case "text":
		return ""
	case "integer", "bigint", "numeric", "double":
		return "#"
	case "date":
		return "~"
	case "timestamptz", "timestamp":
		return "@"
	case "boolean":
		return "?"
	case "uuid":
		return "$"
	case "jsonb", "json":
		return "{}"
	default:
		return ":" + t
	}
}

// RenderCardBlock renders every card, in name order, as one prompt block.
func RenderCardBlock() string {
	cards := Cards()
	lines := make([]string, 0, len(cards)+1)
	lines = append(lines, TypeLegend)
	for _, c := range cards {
		lines = append(lines, c.RenderCompact())
	}
	return strings.Join(lines, "\n")
}

// Cards returns every schema card sorted by Name. The slice is a fresh copy.
func Cards() []SchemaCard {
	out := make([]SchemaCard, len(schemaCards))
	copy(out, schemaCards)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// CardByName looks a card up by bare view name (case-insensitive). ok=false
// when no card exists for that name.
func CardByName(name string) (SchemaCard, bool) {
	name = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(name), "ceo_ai."))
	for _, c := range schemaCards {
		if strings.ToLower(c.Name) == name {
			return c, true
		}
	}
	return SchemaCard{}, false
}

// CardForSQL finds the card of the single ceo_ai.<view> relation a validated
// fallback statement reads (the first `ceo_ai . <ident>` reference after FROM).
// ok=false when no ceo_ai reference is present or the view has no card.
func CardForSQL(sql string) (SchemaCard, bool) {
	name := ViewNameFromSQL(sql)
	if name == "" {
		return SchemaCard{}, false
	}
	return CardByName(name)
}

// ViewNameFromSQL returns the bare view name of the first `ceo_ai.<ident>`
// reference in sql, or "". It is a plain scan (case-insensitive, whole
// identifier) — the statement is expected to have passed sqlguard.Validate,
// which guarantees a single ceo_ai.* relation.
func ViewNameFromSQL(sql string) string {
	low := strings.ToLower(sql)
	const prefix = "ceo_ai."
	idx := 0
	for {
		i := strings.Index(low[idx:], prefix)
		if i < 0 {
			return ""
		}
		start := idx + i
		// The character before must not be an identifier part (so a column
		// literally named x_ceo_ai.y is not matched).
		if start > 0 && isIdentByte(low[start-1]) {
			idx = start + len(prefix)
			continue
		}
		j := start + len(prefix)
		k := j
		for k < len(low) && isIdentByte(low[k]) {
			k++
		}
		if k > j {
			return low[j:k]
		}
		idx = k
	}
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// PGTypeShort maps an information_schema.columns (data_type, udt_name) pair
// to the short type spelling the cards use. The pg-gated diff test applies it
// to the live catalog so both sides speak the same vocabulary.
func PGTypeShort(dataType, udtName string) string {
	switch strings.ToLower(strings.TrimSpace(dataType)) {
	case "uuid":
		return "uuid"
	case "text", "character varying", "character":
		return "text"
	case "date":
		return "date"
	case "timestamp with time zone":
		return "timestamptz"
	case "timestamp without time zone":
		return "timestamp"
	case "bigint":
		return "bigint"
	case "integer", "smallint":
		return "integer"
	case "numeric":
		return "numeric"
	case "double precision", "real":
		return "double"
	case "boolean":
		return "boolean"
	case "jsonb":
		return "jsonb"
	case "json":
		return "json"
	case "array":
		return "array:" + strings.TrimPrefix(strings.ToLower(udtName), "_")
	default:
		return fmt.Sprintf("%s(%s)", strings.ToLower(dataType), strings.ToLower(udtName))
	}
}

// --- column shorthands -------------------------------------------------------

func col(name, typ string) Column { return Column{Name: name, Type: typ} }

var (
	uuidT  = "uuid"
	textT  = "text"
	dateT  = "date"
	tstzT  = "timestamptz"
	bigT   = "bigint"
	intT   = "integer"
	numT   = "numeric"
	dblT   = "double"
	boolT  = "boolean"
	jsonbT = "jsonb"
)

// schemaCards is the authoritative card list. Keep one card per ceo_ai view;
// the guard fails on any CREATE VIEW without a card.
var schemaCards = []SchemaCard{
	{
		Name:                "action_center_current",
		Purpose:             "Open cross-module action items (what needs a decision now) with owner and due time.",
		Grain:               "one row per open action item",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"area", "severity", "park_label", "shed_label", "owner_label", "status"},
		Columns: []Column{
			col("tenant_id", uuidT), col("area", textT), col("severity", textT), col("park_label", textT),
			col("shed_label", textT), col("title", textT), col("owner_label", textT), col("backup_label", textT),
			col("due_at", tstzT), col("status", textT),
		},
		Route: "/action-center",
	},
	{
		Name:                "animal_current_scope",
		Purpose:             "Live per-animal census scope: species, breed, sex, stage, age, park/shed/pen, lifecycle; use for headcount and breakdowns.",
		Grain:               "one row per animal (current state)",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"park_label", "shed_label", "partition_label", "species", "breed", "sex", "management_stage", "lifecycle_status", "origin_type"},
		Columns: []Column{
			col("tenant_id", uuidT), col("animal_id", uuidT), col("park_id", uuidT), col("park_label", textT),
			col("shed_id", uuidT), col("shed_label", textT), col("species", textT), col("management_stage", textT),
			col("lifecycle_status", textT), col("sex", textT), col("breed", textT), col("age_days", intT),
			col("partition_label", textT), col("origin_type", textT),
		},
		AggregateOnly: true,
		Route:         "/counts/herd",
	},
	{
		Name:                "animals_base",
		Purpose:             "Cube base of every animal with entry and exit business days (the census/mortality denominators).",
		Grain:               "one row per animal (all-time, includes exited)",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"species", "lifecycle_status", "management_stage", "park_label", "shed_label", "exit_reason"},
		Columns: []Column{
			col("goat_id", uuidT), col("tenant_id", uuidT), col("species", textT), col("lifecycle_status", textT),
			col("management_stage", textT), col("park_id", uuidT), col("park_label", textT), col("shed_id", uuidT),
			col("shed_label", textT), col("entry_date", dateT), col("exit_business_day", dateT), col("exit_reason", textT),
		},
		AggregateOnly: true,
		Route:         "/counts/herd",
	},
	{
		Name:                "audit_activity_summary",
		Purpose:             "Who did what, how often, per business day and area (audit activity rollup).",
		Grain:               "one row per business day per area per actor per action per result",
		DateColumn:          "business_date",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"business_date", "area", "actor_label", "action_label", "result"},
		Columns: []Column{
			col("tenant_id", uuidT), col("business_date", dateT), col("area", textT), col("actor_label", textT),
			col("action_label", textT), col("result", textT), col("count", bigT), col("last_activity_at", tstzT),
		},
		Route: "/operations/audit",
	},
	{
		Name:                "counts_movement_daily",
		Purpose:             "Daily herd movement: births, deaths, transfers out, shifts in/out and pending approvals per shed/pen.",
		Grain:               "one row per business day per shed per pen (pen is NULL on shift/transfer rows)",
		DateColumn:          "event_date",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"event_date", "park_label", "shed_label", "partition_label"},
		Columns: []Column{
			col("tenant_id", uuidT), col("event_date", dateT), col("park_label", textT), col("shed_label", textT),
			col("births", bigT), col("deaths", bigT), col("transfers_out", bigT), col("shifts_in", bigT),
			col("shifts_out", bigT), col("approvals_pending", bigT), col("partition_label", textT),
		},
		Route: "/counts/analytics",
	},
	{
		Name:                "feed_adherence",
		Purpose:             "Directed vs actually fed kg per feed day and shed, with variance and blocked flag.",
		Grain:               "one row per feed day per shed",
		DateColumn:          "feed_day",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"feed_day", "park_label", "shed_label", "blocked"},
		Columns: []Column{
			col("tenant_id", uuidT), col("feed_day", dateT), col("park_label", textT), col("shed_label", textT),
			col("directed_kg", numT), col("fed_kg", numT), col("variance_kg", numT), col("blocked", boolT),
		},
		Route: "/feed/analytics",
	},
	{
		Name:                "feed_completions_base",
		Purpose:             "Cube base of feed distribution completions with quantity fed and head count per shed and business day.",
		Grain:               "one row per feed distribution completion",
		DateColumn:          "fed_business_day",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"fed_business_day", "park_label", "shed_label", "status"},
		Columns: []Column{
			col("completion_id", uuidT), col("tenant_id", uuidT), col("shed_id", uuidT), col("shed_label", textT),
			col("park_id", uuidT), col("park_label", textT), col("quantity_fed", numT), col("head_count", intT),
			col("status", textT), col("fed_business_day", dateT),
		},
		AggregateOnly: true,
		Route:         "/feed/analytics",
	},
	{
		Name:                "feed_direction_current",
		Purpose:             "Issued feed-direction cells: what each shed is directed to be fed per session and feed item, with blocked reasons.",
		Grain:               "one row per feed day per shed per session per feed item",
		DateColumn:          "feed_day",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"feed_day", "park_label", "shed_label", "workflow", "session_no", "feed_item_label", "blocked_reason", "amended"},
		Columns: []Column{
			col("tenant_id", uuidT), col("feed_day", dateT), col("park_label", textT), col("shed_label", textT),
			col("workflow", textT), col("session_no", intT), col("feed_item_label", textT), col("quantity_kg", numT),
			col("blocked_reason", textT), col("amended", boolT),
		},
		Route: "/feed/direction",
	},
	{
		Name:                "growth_adg_pairs",
		Purpose:             "Per-animal gain between consecutive weighs. Never avg adg_g_per_day: sum gain_kg / sum days_between.",
		Grain:               "one row per consecutive pair of weighs",
		DateColumn:          "weighed_on",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"park_label", "shed_label", "partition_label", "breed", "sex", "management_stage", "weighed_on"},
		Columns: []Column{
			col("tenant_id", uuidT), col("animal_key", textT), col("weighed_on", dateT),
			col("days_between", intT), col("weight_kg", numT), col("gain_kg", numT),
			col("adg_g_per_day", numT), col("park_label", textT), col("shed_id", uuidT), col("shed_label", textT),
			col("partition_label", textT), col("breed", textT), col("sex", textT), col("management_stage", textT),
		},
		AggregateOnly: true,
		NeverAverage:  []string{"adg_g_per_day"},
		Route:         "/weighing/analytics",
	},
	{
		Name:                "inventory_stock_position",
		Purpose:             "Current stock on hand per inventory item and park with reorder flag.",
		Grain:               "one row per item per park (current state)",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"item_label", "category", "unit", "park_label", "reorder_flag"},
		Columns: []Column{
			col("tenant_id", uuidT), col("item_label", textT), col("category", textT), col("stock_on_hand", numT),
			col("unit", textT), col("park_label", textT), col("reorder_flag", boolT), col("last_reconciled_at", tstzT),
		},
		Route: "/feed/analytics",
	},
	{
		Name:                "mortality_base",
		Purpose:             "Deaths per business day and park with the active population denominator, kid/adult/first-week splits and cause-established count.",
		Grain:               "one row per business day per park",
		DateColumn:          "event_date",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"event_date", "park_label"},
		Columns: []Column{
			col("tenant_id", uuidT), col("event_date", dateT), col("park_label", textT), col("deaths", bigT),
			col("active_population", bigT), col("kid_deaths", bigT), col("adult_deaths", bigT),
			col("first_week_deaths", bigT), col("cause_established", bigT),
		},
		NeverAverage: []string{"active_population"},
		Route:        "/counts/mortality",
	},
	{
		Name:                "notification_delivery_health",
		Purpose:             "Push/reminder delivery health per business day, channel and notification type (requested/sent/failed/pending).",
		Grain:               "one row per business day per channel per notification type",
		DateColumn:          "business_date",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"business_date", "channel", "notification_type"},
		Columns: []Column{
			col("tenant_id", uuidT), col("business_date", dateT), col("channel", textT), col("notification_type", textT),
			col("requested", bigT), col("sent", bigT), col("failed", bigT), col("pending", bigT), col("oldest_pending_at", tstzT),
		},
		Route: "/alerts",
	},
	{
		Name:                "ops_exception_queue",
		Purpose:             "Open operational exceptions across modules with severity, status, scope and owner.",
		Grain:               "one row per open exception",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"area", "severity", "status", "park_label", "shed_label", "owner_label"},
		Columns: []Column{
			col("tenant_id", uuidT), col("area", textT), col("severity", textT), col("status", textT),
			col("park_label", textT), col("shed_label", textT), col("title", textT), col("opened_at", tstzT),
			col("owner_label", textT), col("source_id", textT),
		},
		Route: "/operations/dlq",
	},
	{
		Name:                "procurement_loads_base",
		Purpose:             "Cube base of procurement loads with status, expected count, source and purchase/entered business days.",
		Grain:               "one row per procurement load",
		DateColumn:          "entered_business_day",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"status", "source_label", "purchase_date", "entered_business_day"},
		Columns: []Column{
			col("load_id", uuidT), col("tenant_id", uuidT), col("status", textT), col("expected_count", intT),
			col("source_location_id", uuidT), col("source_label", textT), col("purchase_date", dateT),
			col("entered_business_day", dateT),
		},
		AggregateOnly: true,
		Route:         "/procurement/source-entry",
	},
	{
		Name:                "procurement_pipeline",
		Purpose:             "Current procurement pipeline: one row per load with stage, animal count, vaccination pending and rejected counts.",
		Grain:               "one row per load (current stage)",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"source_label", "batch_label", "current_stage"},
		Columns: []Column{
			col("tenant_id", uuidT), col("source_label", textT), col("batch_label", textT), col("current_stage", textT),
			col("animals", bigT), col("vaccination_pending", bigT), col("rejected", bigT), col("entered_at", tstzT),
		},
		Route: "/procurement",
	},
	{
		Name:                "sales_buyer_summary",
		Purpose:             "One row per buyer over closed deals: revenue, received, outstanding, first/last sale, repeat.",
		Grain:               "one row per buyer (closed deals)",
		DateColumn:          "last_sale_date",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"buyer_key", "buyer_label", "is_repeat_buyer"},
		Columns: []Column{
			col("tenant_id", uuidT), col("buyer_key", textT), col("buyer_label", textT),
			col("deals", bigT), col("is_repeat_buyer", boolT), col("animals", numT),
			col("revenue_rupees", numT), col("payment_received_rupees", numT),
			col("outstanding_rupees", numT), col("first_sale_date", dateT), col("last_sale_date", dateT),
		},
		Route: "/sales/buyer-analytics",
	},
	{
		Name: "sales_deal_lines_closed",
		// line_no is REQUIRED, not optional prose: it is the view's unique key at
		// its own grain and the keyset every consumer pages on (migration 000393).
		// The planner prompt had 3 bytes of headroom (TestPlanPromptByteBound), so
		// this card PAYS for its own column instead of raising the bound -- the
		// Purpose dropped ", never avg(price_per_kg)" and the Grain its "synthesised"
		// clause, because RenderCompact already emits "Never avg/sum: price_per_kg,..."
		// from NeverAverage below, verbatim, two lines further down the same card.
		// Rendered: 20469 of 20480 bytes. Trim here before widening the bound.
		Purpose:             "Closed deals, keyed (deal_id,line_no); page on it, never group it. Rupees/kg = sum(sales_value)/sum(total_weight_kg) where is_animal_line. deal_* repeats per line: sum only FILTER (WHERE is_deal_primary_line).",
		Grain:               "one line of a closed deal; line_no 1 if it has none",
		DateColumn:          "sale_date",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"sale_date", "farm", "buyer_key", "buyer_label", "product_type", "breed", "is_animal_line"},
		Columns: []Column{
			col("tenant_id", uuidT), col("deal_id", uuidT), col("line_no", intT), col("sale_date", dateT),
			col("farm", textT), col("buyer_label", textT), col("buyer_key", textT),
			col("product_type", textT), col("breed", textT),
			col("is_animal_line", boolT), col("animal_count", numT), col("total_weight_kg", numT),
			col("sales_value", numT), col("price_per_kg", numT), col("deal_sales_value", numT),
			col("deal_payment_received", numT), col("deal_outstanding_rupees", numT), col("is_deal_primary_line", boolT),
		},
		AggregateOnly: true,
		NeverAverage:  []string{"price_per_kg", "deal_sales_value", "deal_payment_received", "deal_outstanding_rupees"},
		Route:         "/sales/sold",
	},
	{
		Name:                "shed_capacity_current",
		Purpose:             "Occupancy vs capacity per shed/pen right now, with variance, status and owner/backup.",
		Grain:               "one row per shed per pen (current state)",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"park_label", "shed_label", "partition_label", "status", "owner_label"},
		Columns: []Column{
			col("tenant_id", uuidT), col("park_label", textT), col("shed_label", textT), col("animals", bigT),
			col("capacity", intT), col("variance", bigT), col("status", textT), col("owner_label", textT),
			col("backup_label", textT), col("partition_label", textT),
		},
		NeverAverage: []string{"capacity"},
		Route:        "/counts/herd",
	},
	{
		Name:                "sop_execution_status",
		Purpose:             "SOP task execution status with due/completed times and verifier per task.",
		Grain:               "one row per SOP task (current state)",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"area", "park_label", "shed_label", "status", "verifier_label"},
		Columns: []Column{
			col("tenant_id", uuidT), col("area", textT), col("park_label", textT), col("shed_label", textT),
			col("task_label", textT), col("status", textT), col("due_at", tstzT), col("completed_at", tstzT),
			col("verifier_label", textT),
		},
		Route: "/tasks",
	},
	{
		Name:                "source_entry_health_status",
		Purpose:             "Per-load intake variance (expected/received/accepted/rejected animals) with health blockers and evidence status.",
		Grain:               "one row per procurement load (current state)",
		ParkColumn:          "park_location_id",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"load_label", "source_label", "evidence_status", "park_location_id"},
		Columns: []Column{
			col("tenant_id", uuidT), col("load_label", textT), col("source_label", textT), col("animals_expected", intT),
			col("animals_received", intT), col("animals_accepted", intT), col("animals_rejected", intT),
			col("health_blockers", bigT), col("evidence_status", textT), col("park_location_id", uuidT),
		},
		Route: "/procurement/source-entry",
	},
	{
		Name:                "vaccination_dose_pickup",
		Purpose:             "Vaccine doses to pick per business day, park, shed/pen and vaccine, with animals due/overdue and next action.",
		Grain:               "one row per business day per shed per pen per vaccine",
		DateColumn:          "business_date",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"business_date", "park_label", "shed_label", "partition_label", "vaccine_label", "owner_label", "next_action"},
		Columns: []Column{
			col("tenant_id", uuidT), col("business_date", dateT), col("park_label", textT), col("shed_label", textT),
			col("vaccine_label", textT), col("doses_to_pick", numT), col("animals_due", bigT), col("animals_overdue", bigT),
			col("owner_label", textT), col("backup_label", textT), col("next_action", textT), col("partition_label", textT),
		},
		Route: "/vaccination/plan",
	},
	{
		Name:                "vaccination_obligations_base",
		Purpose:             "Cube base of every vaccination obligation with status, due/completed business days and shed/park/species.",
		Grain:               "one row per vaccination obligation",
		DateColumn:          "due_business_day",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"status", "due_business_day", "completed_business_day", "park_label", "shed_label", "species"},
		Columns: []Column{
			col("obligation_id", uuidT), col("tenant_id", uuidT), col("status", textT), col("due_at", tstzT),
			col("completed_at", tstzT), col("shed_id", uuidT), col("shed_label", textT), col("park_id", uuidT),
			col("park_label", textT), col("species", textT), col("due_business_day", dateT), col("completed_business_day", dateT),
		},
		AggregateOnly: true,
		Route:         "/vaccination",
	},
	{
		Name:                "vaccination_operator_status",
		Purpose:             "Per-operator vaccination drive load by planned day and shed/pen: assigned, due, done, overdue, daily capacity and utilization.",
		Grain:               "one row per operator per planned day per shed per pen",
		DateColumn:          "planned_date",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"operator_label", "planned_date", "park_label", "shed_label", "partition_label", "next_action"},
		Columns: []Column{
			col("tenant_id", uuidT), col("operator_id", uuidT), col("operator_label", textT), col("park_id", uuidT),
			col("park_label", textT), col("shed_id", uuidT), col("shed_label", textT), col("planned_date", dateT),
			col("assigned_animals", bigT), col("due", bigT), col("done", bigT), col("overdue", bigT),
			col("daily_capacity", intT), col("operator_day_assigned", numT), col("utilization", numT),
			col("next_action", textT), col("partition_label", textT),
		},
		NeverAverage: []string{"daily_capacity", "operator_day_assigned", "utilization"},
		Route:        "/vaccination/live-tracker",
	},
	{
		Name:                "vaccination_prearrival_history_review",
		Purpose:             "Review outcome of pre-arrival vaccination history claims per reviewed day, source, schedule path, status and rejection reason.",
		Grain:               "one row per reviewed day per source system per schedule path per review status per rejection reason",
		DateColumn:          "reviewed_date_ist",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"reviewed_date_ist", "source_system", "schedule_path", "review_status", "rejection_reason", "vaccine_label"},
		Columns: []Column{
			col("tenant_id", uuidT), col("reviewed_date_ist", dateT), col("source_system", textT), col("schedule_path", textT),
			col("review_status", textT), col("rejection_reason", textT), col("claims", bigT), col("distinct_animals", bigT),
			col("earliest_administered_date", dateT), col("latest_administered_date", dateT), col("vaccine_label", textT),
		},
		NeverAverage: []string{"distinct_animals"},
		Route:        "/procurement/source-entry",
	},
	{
		Name:                "vaccination_shed_status",
		Purpose:             "Vaccination due/done per shed/pen right now with planned sessions, next due date, manager/backup and status.",
		Grain:               "one row per shed per pen (current state)",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"park_label", "shed_label", "partition_label", "status", "manager_label", "next_due_date"},
		Columns: []Column{
			col("tenant_id", uuidT), col("park_label", textT), col("shed_label", textT), col("animals", bigT),
			col("due", bigT), col("done", bigT), col("planned_sessions", bigT), col("next_due_date", dateT),
			col("manager_label", textT), col("backup_label", textT), col("status", textT), col("partition_label", textT),
		},
		Route: "/vaccination",
	},
	{
		Name:                "verification_queue_status",
		Purpose:             "Verification queue counts per area, park and shed: pending, accepted, rejected, withdrawn, oldest pending.",
		Grain:               "one row per area per park per shed (current state)",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"area", "park_label", "shed_label", "owner_label"},
		Columns: []Column{
			col("tenant_id", uuidT), col("area", textT), col("park_label", textT), col("shed_label", textT),
			col("pending", bigT), col("rejected", bigT), col("accepted", bigT), col("oldest_pending_at", tstzT),
			col("owner_label", textT), col("total", bigT), col("withdrawn", bigT), col("total_including_withdrawn", bigT),
		},
		Route: "/verify",
	},
	{
		Name:                "verifier_review_integrity",
		Purpose:             "Verifier watch-telemetry per verifier, park, category and business day: videos reviewed, time-to-verdict, watch fraction, reject rate.",
		Grain:               "one row per verifier per park per category per business day",
		DateColumn:          "business_day",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"verifier_id", "park_label", "category", "business_day"},
		Columns: []Column{
			col("tenant_id", uuidT), col("verifier_id", uuidT), col("park_id", uuidT), col("park_label", textT),
			col("category", textT), col("business_day", dateT), col("videos_reviewed", bigT),
			col("median_time_to_verdict_seconds", dblT), col("p90_time_to_verdict_seconds", dblT),
			col("median_watch_fraction", dblT), col("below_watch_threshold_count", bigT),
			col("missing_review_telemetry_count", bigT), col("rejected_count", bigT), col("reject_rate", numT),
			col("reject_reason_breakdown", jsonbT),
		},
		NeverAverage: []string{"median_time_to_verdict_seconds", "p90_time_to_verdict_seconds", "median_watch_fraction", "reject_rate"},
		Route:        "/verification",
	},
	{
		Name:                "weighing_capture_activity",
		Purpose:             "Weighing bucket activity: per campaign shed/pen the planned/due dates, work state, scan counts, weight stats and animals weighed.",
		Grain:               "one row per weighing bucket (campaign shed/pen)",
		DateColumn:          "planned_business_date",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"park_label", "shed_label", "weighing_category", "bucket_status", "cadence_type", "work_state", "planned_business_date", "due_business_date"},
		Columns: []Column{
			col("tenant_id", uuidT), col("park_label", textT), col("shed_label", textT), col("weighing_category", textT),
			col("bucket_status", textT), col("period_start_date", dateT), col("period_end_date", dateT), col("cadence_type", textT),
			col("work_state", textT), col("planned_business_date", dateT), col("due_business_date", dateT),
			col("delayed_since_business_date", dateT), col("rolled_forward_count", intT), col("scan_count", bigT),
			col("scan_weight_avg_kg", numT), col("scan_weight_min_kg", numT), col("scan_weight_max_kg", numT),
			col("shed_animal_count", intT), col("shed_weight_avg_kg", numT), col("shed_total_weight_kg", numT),
			col("animals_weighed", bigT),
		},
		NeverAverage: []string{"scan_weight_avg_kg", "scan_weight_min_kg", "scan_weight_max_kg", "shed_weight_avg_kg"},
		Route:        "/weighing/weights",
	},
	{
		Name:                "weighing_latest_individual_weight",
		Purpose:             "Each animal's latest-ever weigh; animal_key is its ear tag. Over-30/35 kg = sale-ready. Lump-sum pens: weighing_capture_activity.",
		Grain:               "one row per animal identity (latest individual weigh)",
		DateColumn:          "weighed_on",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"park_label", "shed_label", "partition_label", "breed", "sex", "management_stage", "weighed_on"},
		Columns: []Column{
			col("tenant_id", uuidT), col("animal_key", textT), col("weighed_on", dateT),
			col("weight_kg", numT), col("is_over_30_kg", boolT),
			col("is_over_35_kg", boolT), col("park_label", textT), col("shed_id", uuidT),
			col("shed_label", textT), col("partition_label", textT),
			col("breed", textT), col("sex", textT), col("management_stage", textT),
		},
		AggregateOnly: true,
		Route:         "/weighing/weights",
	},
	{
		Name:                "weighing_verification_status",
		Purpose:             "Weighing proof verification counts per park and shed: total, pending, rework, verified, withdrawn, oldest pending.",
		Grain:               "one row per park per shed (current state)",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"park_label", "shed_label"},
		Columns: []Column{
			col("tenant_id", uuidT), col("park_label", textT), col("shed_label", textT), col("total", bigT),
			col("pending", bigT), col("rework", bigT), col("verified", bigT), col("oldest_pending_at", tstzT),
			col("withdrawn", bigT), col("total_including_withdrawn", bigT),
		},
		Route: "/weighing/weights",
	},
	{
		Name:                "workforce_coverage_status",
		Purpose:             "Workforce coverage per park and role: owner/backup, coverage status, active and overdue work counts.",
		Grain:               "one row per park per role (current state)",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"park_label", "role_label", "owner_label", "coverage_status"},
		Columns: []Column{
			col("tenant_id", uuidT), col("park_label", textT), col("role_label", textT), col("owner_label", textT),
			col("backup_label", textT), col("coverage_status", textT), col("active_work_count", bigT), col("overdue_work_count", bigT),
		},
		Route: "/people",
	},
	{
		Name:                "workforce_tasks_base",
		Purpose:             "Cube base of workforce tasks with state, type, operator, scope and due business day.",
		Grain:               "one row per workforce task",
		DateColumn:          "due_business_day",
		ParkColumn:          "park_label",
		TenantScopedColumns: []string{"tenant_id"},
		GroupByColumns:      []string{"state", "task_type", "operator_id", "park_label", "shed_label", "due_business_day"},
		Columns: []Column{
			col("task_id", uuidT), col("tenant_id", uuidT), col("state", textT), col("task_type", textT),
			col("operator_id", uuidT), col("scope_id", uuidT), col("shed_label", textT), col("park_id", uuidT),
			col("park_label", textT), col("verified_at", tstzT), col("due_business_day", dateT),
		},
		AggregateOnly: true,
		Route:         "/work-board",
	},
}

// IdentityKeyColumns returns the card's IDENTITY-KEY text columns: the columns
// whose values are a canonicalised name or tag (`animal_key`, `buyer_key`) and
// which the views therefore store folded — `lower(btrim(...))` — rather than in
// the spelling a reader types or reads off an ear tag.
//
// They exist so that two spellings of one animal or one buyer collapse to one
// row. That folding is invisible to a question: a reader asks about
// `MG-100001`, which is exactly how `goat_identifiers.normalized_value` stores
// it and how it is printed on the tag, and an `=` against the folded column
// misses — silently, and reported to the reader as "no records found". The
// filter path (app.normalizeIdentityFilters) uses this list to compare such a
// column case-insensitively instead.
//
// Only text columns qualify; a `*_id` uuid is not an identity KEY in this
// sense, and no tenant-scoped column is ever rewritten.
func (c SchemaCard) IdentityKeyColumns() []string {
	var out []string
	for _, col := range c.Columns {
		if col.Type != textT || !isIdentityKeyName(col.Name) {
			continue
		}
		if c.isTenantScoped(col.Name) {
			continue
		}
		out = append(out, col.Name)
	}
	return out
}

// isIdentityKeyName reports the naming convention the ceo_ai views use for a
// folded identity key: a `_key` or `_tag` suffix, or a bare `tag`.
func isIdentityKeyName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "tag" || strings.HasSuffix(n, "_key") || strings.HasSuffix(n, "_tag")
}

func (c SchemaCard) isTenantScoped(name string) bool {
	if strings.EqualFold(name, "tenant_id") {
		return true
	}
	for _, t := range c.TenantScopedColumns {
		if strings.EqualFold(t, name) {
			return true
		}
	}
	return false
}
