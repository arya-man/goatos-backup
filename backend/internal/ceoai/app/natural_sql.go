package app

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

var activeAnimalQuestion = regexp.MustCompile(`(?i)\b(how many|count|present|active|do we have|headcount|census|split|breakdown|break down)\b.*\b(animal|animals|goat|goats|sheep)\b|\b(animal|animals|goat|goats|sheep)\b.*\b(how many|count|present|active|do we have|headcount|census|split|breakdown|break down)\b`)
var farmBornQuestion = regexp.MustCompile(`(?i)\b(farm[- ]?born|born in (the )?farm|born at (the )?farm|home[- ]?bred|own farms?|our farms?|birth origin)\b`)
var breedQuestion = regexp.MustCompile(`(?i)\b(breed|breeds)\b`)
var weighingQuestion = regexp.MustCompile(`(?i)\b(weighing|weighed|weight|avg wt|avg weight|average weight|lowest avg|lowest average|low average weight|lump[- ]?sum|per animal|weight[- ]?wise|weight band)\b`)
var mortalityQuestion = regexp.MustCompile(`(?i)\b(mortality|death|deaths|dead|died|kid deaths|adult deaths|cause established|first week|7 days of birth)\b`)
var vaccinationQuestion = regexp.MustCompile(`(?i)\b(vaccination|vaccinations|vaccinate|vaccine|overdue|missed)\b`)
var feedQuestion = regexp.MustCompile(`(?i)\b(feed|fed|feeding)\b`)
var feedByWeightBandQuestion = regexp.MustCompile(`(?i)\b(feed by weight band|matched animals)\b`)
var sourceEntryHealthQuestion = regexp.MustCompile(`(?i)\b(source[- ]?entry health|source health|import health|procurement health|arrival health)\b`)
var opsRiskQuestion = regexp.MustCompile(`(?i)\b(worry|risk|exception|exceptions|gap|gaps|action center)\b`)

type knownParkScope struct {
	code  string
	label string
	id    string
}

var knownParkScopes = []knownParkScope{
	{code: "CPT", label: "Channapatna", id: "00000000-0000-4000-8000-000000003002"},
	{code: "CBE", label: "Coimbatore", id: "00000000-0000-4000-8000-000000003001"},
}

func naturalSQLPlan(q domain.Question, mem []domain.ResolvedEntities) (domain.SubQuestion, bool) {
	normalizedText := normalizeNaturalSQLText(q.Text)
	if asksForSales(normalizedText) {
		return naturalSalesPlan(q, normalizedText, mem), true
	}
	if feedByWeightBandQuestion.MatchString(normalizedText) {
		return naturalFeedWeightBandPlan(q, normalizedText, mem), true
	}
	if mortalityQuestion.MatchString(normalizedText) || weighingQuestion.MatchString(normalizedText) {
		if sub, ok := naturalOperationalSQLPlan(q, normalizedText, mem); ok {
			return sub, true
		}
	}
	isActiveQuestion := activeAnimalQuestion.MatchString(normalizedText)
	isFarmBornQuestion := farmBornQuestion.MatchString(normalizedText)
	isBreedQuestion := breedQuestion.MatchString(normalizedText)
	if isActiveQuestion || isFarmBornQuestion || isBreedQuestion {
		if scopes, ok := resolveKnownParkScopes(normalizedText, mem, isFarmBornQuestion); ok {
			groupBy := "species"
			if isBreedQuestion {
				groupBy = "breed"
			} else if wantsPenBreakdown(normalizedText) {
				groupBy = "shed_label"
			} else if len(scopes) > 1 {
				groupBy = "park_label"
			}
			sql := activeAnimalsSQL(q.Actor.TenantID, scopes, groupBy, normalizedText)
			return domain.SubQuestion{
				ID:          "0",
				Text:        q.Text,
				IntentClass: "active_animals_live_sql",
				Route:       domain.RouteSQL,
				ToolName:    "sql_fallback",
				Params: map[string]any{
					"sql":         sql,
					"park_label":  joinedParkLabels(scopes),
					"park_code":   joinedParkCodes(scopes),
					"natural_sql": "active_animals",
					"group_by":    groupBy,
				},
			}, true
		}
	}
	if sub, ok := naturalOperationalSQLPlan(q, normalizedText, mem); ok {
		return sub, true
	}
	return domain.SubQuestion{}, false
}

func naturalFeedWeightBandPlan(q domain.Question, normalizedText string, mem []domain.ResolvedEntities) domain.SubQuestion {
	params := map[string]any{}
	if scope, ok := resolveKnownParkScope(normalizedText, mem); ok {
		params["park_label"] = scope.label
		params["park_code"] = scope.code
	}
	return domain.SubQuestion{
		ID:          "0",
		Text:        q.Text,
		IntentClass: "feed_weight_band_live_api",
		Route:       domain.RouteAPI,
		ToolName:    "feed_weight_band_summary",
		Params:      params,
	}
}

func naturalSalesPlan(q domain.Question, normalizedText string, mem []domain.ResolvedEntities) domain.SubQuestion {
	params := map[string]any{}
	if strings.Contains(normalizedText, "this month") || strings.Contains(normalizedText, "month") {
		params["month"] = "current"
	}
	if scope, ok := resolveKnownParkScope(normalizedText, mem); ok {
		params["farm"] = scope.code
	}
	return domain.SubQuestion{
		ID:          "0",
		Text:        q.Text,
		IntentClass: "sales_overview_live_api",
		Route:       domain.RouteAPI,
		ToolName:    "sales_overview",
		Params:      params,
	}
}

func naturalOperationalSQLPlan(q domain.Question, normalizedText string, mem []domain.ResolvedEntities) (domain.SubQuestion, bool) {
	scope, hasScope := resolveKnownParkScope(normalizedText, mem)
	switch {
	case mortalityQuestion.MatchString(normalizedText):
		return sqlSubQuestion(q, "mortality_live_sql", mortalitySQL(q.Actor.TenantID, scope, hasScope, normalizedText, q.AsOf), "mortality", scope, hasScope), true
	case weighingQuestion.MatchString(normalizedText):
		return sqlSubQuestion(q, "weighing_live_sql", weighingSQL(q.Actor.TenantID, scope, hasScope, normalizedText), "weighing", scope, hasScope), true
	case vaccinationQuestion.MatchString(normalizedText) && (strings.Contains(normalizedText, "overload") || strings.Contains(normalizedText, "over capacity")):
		return sqlSubQuestion(q, "vaccination_operator_live_sql", vaccinationOperatorSQL(q.Actor.TenantID, scope, hasScope), "vaccination_operator", scope, hasScope), true
	case feedQuestion.MatchString(normalizedText) && !feedByWeightBandQuestion.MatchString(normalizedText):
		return sqlSubQuestion(q, "feed_live_sql", feedSQL(q.Actor.TenantID, scope, hasScope, q.AsOf), "feed", scope, hasScope), true
	case sourceEntryHealthQuestion.MatchString(normalizedText):
		sub := sqlSubQuestion(q, "health_issue_live_sql", healthIssueSQL(q.Actor.TenantID, scope, hasScope), "health_issue", scope, hasScope)
		sub.TrustedSQL = true
		return sub, true
	case opsRiskQuestion.MatchString(normalizedText):
		return sqlSubQuestion(q, "ops_risk_live_sql", opsRiskSQL(q.Actor.TenantID, scope, hasScope), "ops_risk", scope, hasScope), true
	default:
		return domain.SubQuestion{}, false
	}
}

func sqlSubQuestion(q domain.Question, intentClass, sql, natural string, scope knownParkScope, hasScope bool) domain.SubQuestion {
	params := map[string]any{
		"sql":         sql,
		"natural_sql": natural,
	}
	if hasScope {
		params["park_label"] = scope.label
		params["park_code"] = scope.code
	}
	return domain.SubQuestion{
		ID:          "0",
		Text:        q.Text,
		IntentClass: intentClass,
		Route:       domain.RouteSQL,
		ToolName:    "sql_fallback",
		Params:      params,
	}
}

func normalizeNaturalSQLText(text string) string {
	low := strings.ToLower(text)
	replacer := strings.NewReplacer(
		"hw", "how",
		"wht", "what",
		"wat", "what",
		"abt", "about",
		"avg", "average",
		"animls", "animals",
		"anmls", "animals",
		"animlas", "animals",
		"animalz", "animals",
		"weigth", "weight",
		"weiging", "weighing",
		"vaccinatons", "vaccinations",
		"vaccinatn", "vaccination",
		"vax", "vaccination",
		"procurment", "procurement",
		"procuremnt", "procurement",
		"helth", "health",
	)
	return replacer.Replace(low)
}

func resolveKnownParkScope(text string, mem []domain.ResolvedEntities) (knownParkScope, bool) {
	low := strings.ToLower(text)
	for _, scope := range knownParkScopes {
		if knownParkScopeMatches(low, scope) {
			return scope, true
		}
	}
	for i := len(mem) - 1; i >= 0; i-- {
		park := strings.ToLower(strings.TrimSpace(mem[i].ParkLabel))
		if park == "" {
			continue
		}
		for _, scope := range knownParkScopes {
			if knownParkScopeMatches(park, scope) {
				return scope, true
			}
		}
	}
	return knownParkScope{}, false
}

func resolveKnownParkScopes(text string, mem []domain.ResolvedEntities, defaultOwnFarms bool) ([]knownParkScope, bool) {
	low := strings.ToLower(text)
	var scopes []knownParkScope
	for _, scope := range knownParkScopes {
		if knownParkScopeMatches(low, scope) {
			scopes = append(scopes, scope)
		}
	}
	if len(scopes) > 0 {
		return scopes, true
	}
	if scope, ok := resolveKnownParkScope(text, mem); ok {
		return []knownParkScope{scope}, true
	}
	if defaultOwnFarms {
		return append([]knownParkScope(nil), knownParkScopes...), true
	}
	return nil, false
}

func knownParkScopeMatches(low string, scope knownParkScope) bool {
	return strings.Contains(low, strings.ToLower(scope.code)) || strings.Contains(low, strings.ToLower(scope.label))
}

func joinedParkLabels(scopes []knownParkScope) string {
	labels := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		labels = append(labels, scope.label)
	}
	return strings.Join(labels, "/")
}

func joinedParkCodes(scopes []knownParkScope) string {
	codes := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		codes = append(codes, scope.code)
	}
	return strings.Join(codes, "/")
}

func wantsPenBreakdown(text string) bool {
	low := strings.ToLower(text)
	for _, kw := range []string{"by pen", "per pen", "by shed", "per shed", "pen wise", "pen-wise", "shed wise", "shed-wise", "graph", "chart", "plot", "breakdown", "break down"} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

func wantsLowestWeightRanking(text string) bool {
	low := strings.ToLower(text)
	return strings.Contains(low, "low average weight") ||
		strings.Contains(low, "lowest average weight") ||
		strings.Contains(low, "lowest average") ||
		strings.Contains(low, "lowest weight")
}

func asksForSales(text string) bool {
	for _, kw := range []string{"sale", "sales", "sold", "selling", "buyer", "buyers", "customer", "customers", "purchased", "bought", "revenue"} {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func activeAnimalsSQL(tenantID string, scopes []knownParkScope, groupBy, text string) string {
	groupCol := "species"
	switch groupBy {
	case "shed_label":
		groupCol = "shed_label"
	case "breed":
		groupCol = "breed"
	case "park_label":
		groupCol = "park_label"
	}
	parkIDs := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		parkIDs = append(parkIDs, sqlStringLiteral(scope.id))
	}
	where := fmt.Sprintf(
		"tenant_id = %s AND park_id IN (%s) AND lifecycle_status = 'alive'",
		sqlStringLiteral(tenantID),
		strings.Join(parkIDs, ", "),
	)
	label := "Active animals"
	if farmBornQuestion.MatchString(text) {
		where += " AND origin_type = 'birth'"
		label = "Farm-born animals"
	}
	if strings.Contains(text, "goat") {
		where += " AND species = 'goat'"
	} else if strings.Contains(text, "sheep") {
		where += " AND species = 'sheep'"
	}
	if strings.Contains(text, "adult") {
		where += " AND age_days >= 365"
	} else if strings.Contains(text, "kid") || strings.Contains(text, "kids") {
		where += " AND age_days < 365"
	}
	// projection-review: membership=ceo_ai.animal_current_scope alive animal rows; group_key=requested species breed or shed label; join_cardinality=view already resolves animal scope one row per active animal; pagination=bounded top 50 after full grouped aggregate; scope=tenant plus explicit park filter.
	return fmt.Sprintf(
		"SELECT %s AS label, CAST(count(*) AS text) AS value, %s AS scope FROM ceo_ai.animal_current_scope WHERE %s GROUP BY %s ORDER BY count(*) DESC LIMIT 50",
		sqlStringLiteral(label),
		groupCol,
		where,
		groupCol,
	)
}

func weighingSQL(tenantID string, scope knownParkScope, hasScope bool, text string) string {
	where := "tenant_id = " + sqlStringLiteral(tenantID)
	if hasScope {
		where += " AND park_id_text_not_used = park_id_text_not_used"
		where = strings.Replace(where, " AND park_id_text_not_used = park_id_text_not_used", " AND park_label = "+sqlStringLiteral(scope.label), 1)
	}
	if strings.Contains(text, "lump") {
		return fmt.Sprintf(
			"SELECT 'Lump-sum animals weighed' AS label, CAST(sum(animals_weighed) AS text) AS value, park_label AS scope FROM ceo_ai.weighing_capture_activity WHERE %s AND weighing_category = 'per_shed_partition' GROUP BY park_label ORDER BY sum(animals_weighed) DESC LIMIT 50",
			where,
		)
	}
	if strings.Contains(text, "per animal") || strings.Contains(text, "individual") {
		return fmt.Sprintf(
			"SELECT 'Per-animal animals weighed' AS label, CAST(sum(animals_weighed) AS text) AS value, park_label AS scope FROM ceo_ai.weighing_capture_activity WHERE %s AND weighing_category = 'individual_animal' GROUP BY park_label ORDER BY sum(animals_weighed) DESC LIMIT 50",
			where,
		)
	}
	if strings.Contains(text, "how many") || strings.Contains(text, "count") || strings.Contains(text, "total") || strings.Contains(text, "animal") {
		return fmt.Sprintf(
			"SELECT 'Animals weighed' AS label, CAST(sum(animals_weighed) AS text) AS value, park_label AS scope FROM ceo_ai.weighing_capture_activity WHERE %s GROUP BY park_label ORDER BY sum(animals_weighed) DESC LIMIT 50",
			where,
		)
	}
	if wantsPenBreakdown(text) || wantsLowestWeightRanking(text) {
		avgWhere := where + " AND shed_weight_avg_kg IS NOT NULL"
		return fmt.Sprintf(
			"SELECT 'Average weight kg' AS label, CAST(shed_weight_avg_kg AS text) AS value, shed_label AS scope FROM ceo_ai.weighing_capture_activity WHERE %s ORDER BY shed_weight_avg_kg ASC LIMIT 50",
			avgWhere,
		)
	}
	avgWhere := where + " AND shed_weight_avg_kg IS NOT NULL"
	return fmt.Sprintf(
		"SELECT 'Average weight kg' AS label, CAST(avg(shed_weight_avg_kg) AS text) AS value, 'selected scope' AS scope FROM ceo_ai.weighing_capture_activity WHERE %s LIMIT 50",
		avgWhere,
	)
}

func mortalitySQL(tenantID string, scope knownParkScope, hasScope bool, text string, asOf time.Time) string {
	where := "tenant_id = " + sqlStringLiteral(tenantID)
	if hasScope {
		where += " AND park_label = " + sqlStringLiteral(scope.label)
	}
	if !asOf.IsZero() && (strings.Contains(text, "today") || strings.Contains(text, "this month") || strings.Contains(text, "month")) {
		day := asOf.In(biztime.DefaultLocation())
		if strings.Contains(text, "today") {
			where += " AND event_date = " + sqlStringLiteral(day.Format("2006-01-02"))
		} else {
			start := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, biztime.DefaultLocation())
			where += " AND event_date >= " + sqlStringLiteral(start.Format("2006-01-02")) + " AND event_date <= " + sqlStringLiteral(day.Format("2006-01-02"))
		}
	}
	switch {
	case strings.Contains(text, "kid"):
		return fmt.Sprintf("SELECT 'Kid deaths' AS label, CAST(sum(kid_deaths) AS text) AS value, park_label AS scope FROM ceo_ai.mortality_base WHERE %s GROUP BY park_label ORDER BY sum(kid_deaths) DESC LIMIT 50", where)
	case strings.Contains(text, "adult"):
		return fmt.Sprintf("SELECT 'Adult deaths' AS label, CAST(sum(adult_deaths) AS text) AS value, park_label AS scope FROM ceo_ai.mortality_base WHERE %s GROUP BY park_label ORDER BY sum(adult_deaths) DESC LIMIT 50", where)
	case strings.Contains(text, "cause established") || strings.Contains(text, "established cause") || strings.Contains(text, "diagnosed"):
		return fmt.Sprintf("SELECT 'Cause established deaths' AS label, CAST(sum(cause_established) AS text) AS value, park_label AS scope FROM ceo_ai.mortality_base WHERE %s GROUP BY park_label ORDER BY sum(cause_established) DESC LIMIT 50", where)
	case strings.Contains(text, "first week") || strings.Contains(text, "7 days of birth"):
		return fmt.Sprintf("SELECT 'Died within 7 days of birth' AS label, CAST(sum(first_week_deaths) AS text) AS value, park_label AS scope FROM ceo_ai.mortality_base WHERE %s GROUP BY park_label ORDER BY sum(first_week_deaths) DESC LIMIT 50", where)
	case strings.Contains(text, "rate"):
		return fmt.Sprintf("SELECT 'Mortality rate pct' AS label, CAST(round((sum(deaths)::numeric * 100) / NULLIF(max(active_population), 0), 1) AS text) AS value, park_label AS scope FROM ceo_ai.mortality_base WHERE %s GROUP BY park_label ORDER BY sum(deaths) DESC LIMIT 50", where)
	case strings.Contains(text, "month") || strings.Contains(text, "trend"):
		return fmt.Sprintf("SELECT 'Deaths by month' AS label, CAST(sum(deaths) AS text) AS value, to_char(date_trunc('month', event_date), 'Mon YYYY') AS scope FROM ceo_ai.mortality_base WHERE %s GROUP BY date_trunc('month', event_date) ORDER BY date_trunc('month', event_date) LIMIT 50", where)
	default:
		return fmt.Sprintf("SELECT 'Deaths' AS label, CAST(sum(deaths) AS text) AS value, park_label AS scope FROM ceo_ai.mortality_base WHERE %s GROUP BY park_label ORDER BY sum(deaths) DESC LIMIT 50", where)
	}
}

func vaccinationOperatorSQL(tenantID string, scope knownParkScope, hasScope bool) string {
	where := "tenant_id = " + sqlStringLiteral(tenantID)
	if hasScope {
		where += " AND park_id = " + sqlStringLiteral(scope.id)
	}
	return fmt.Sprintf(
		"SELECT 'Operator utilization' AS label, CAST(utilization AS text) AS value, operator_label AS scope FROM ceo_ai.vaccination_operator_status WHERE %s ORDER BY utilization DESC LIMIT 50",
		where,
	)
}

func feedSQL(tenantID string, scope knownParkScope, hasScope bool, asOf time.Time) string {
	where := "tenant_id = " + sqlStringLiteral(tenantID) + " AND (blocked = true OR variance_kg < 0)"
	if !asOf.IsZero() {
		day := asOf.In(biztime.DefaultLocation()).Format("2006-01-02")
		where += " AND feed_day = " + sqlStringLiteral(day)
	}
	if hasScope {
		where += " AND park_label = " + sqlStringLiteral(scope.label)
	}
	return fmt.Sprintf(
		"SELECT 'Feed variance kg' AS label, CAST(variance_kg AS text) AS value, shed_label AS scope FROM ceo_ai.feed_adherence WHERE %s ORDER BY variance_kg ASC LIMIT 50",
		where,
	)
}

func healthIssueSQL(tenantID string, scope knownParkScope, hasScope bool) string {
	where := "l.tenant_id = " + sqlStringLiteral(tenantID)
	if hasScope {
		where += " AND air.park_location_id = " + sqlStringLiteral(scope.id)
	}
	// projection-review: membership=procurement_source_health_checks rows joined to one procurement load; group_key=load plus purchase date plus source label; join_cardinality=health checks are the counted many side and arrival_intake_reviews is scoped by load; pagination=bounded top 50 after full grouped aggregate; scope=tenant plus explicit park location filter.
	return fmt.Sprintf(
		`SELECT COALESCE(pt.display_name, loc.name, 'Unknown source') AS label,
		       CAST(COUNT(*) FILTER (WHERE h.health_state IN ('blocked','failed','sick','quarantine')) AS text) AS value,
		       ('Load ' || left(l.load_id::text, 8) || COALESCE(' · ' || to_char(l.purchase_date, 'DD Mon'), '')) AS scope
		FROM procurement_loads l
		LEFT JOIN parties pt ON pt.party_id = l.source_party_id
		LEFT JOIN locations loc ON loc.location_id = l.source_location_id
		LEFT JOIN arrival_intake_reviews air ON air.tenant_id = l.tenant_id AND air.load_id = l.load_id
		JOIN procurement_source_health_checks h ON h.tenant_id = l.tenant_id AND h.load_id = l.load_id
		WHERE %s
		GROUP BY l.load_id, l.purchase_date, pt.display_name, loc.name
		HAVING COUNT(*) FILTER (WHERE h.health_state IN ('blocked','failed','sick','quarantine')) > 0
		ORDER BY COUNT(*) FILTER (WHERE h.health_state IN ('blocked','failed','sick','quarantine')) DESC
		LIMIT 50`,
		where,
	)
}

func opsRiskSQL(tenantID string, scope knownParkScope, hasScope bool) string {
	where := "tenant_id = " + sqlStringLiteral(tenantID)
	if hasScope {
		where += " AND park_label = " + sqlStringLiteral(scope.label)
	}
	return fmt.Sprintf(
		"SELECT title AS label, severity AS value, area AS scope FROM ceo_ai.action_center_current WHERE %s ORDER BY due_at ASC LIMIT 50",
		where,
	)
}

func sqlStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
