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
var breedQuestion = regexp.MustCompile(`(?i)\b(breed|breeds)\b`)
var weighingQuestion = regexp.MustCompile(`(?i)\b(weighing|weight|avg wt|avg weight|average weight|lowest avg|lowest average|low average weight)\b`)
var vaccinationQuestion = regexp.MustCompile(`(?i)\b(vaccination|vaccinations|vaccinate|vaccine|overdue|missed)\b`)
var feedQuestion = regexp.MustCompile(`(?i)\b(feed|fed|feeding)\b`)
var healthQuestion = regexp.MustCompile(`(?i)\b(health|sick|issue|issues|blocker|blockers|source entry health)\b`)
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
	isActiveQuestion := activeAnimalQuestion.MatchString(normalizedText)
	isBreedQuestion := breedQuestion.MatchString(normalizedText)
	if isActiveQuestion || isBreedQuestion {
		if scope, ok := resolveKnownParkScope(normalizedText, mem); ok {
			groupBy := "species"
			if isBreedQuestion {
				groupBy = "breed"
			} else if wantsPenBreakdown(normalizedText) {
				groupBy = "shed_label"
			}
			sql := activeAnimalsSQL(q.Actor.TenantID, scope, groupBy, normalizedText)
			return domain.SubQuestion{
				ID:          "0",
				Text:        q.Text,
				IntentClass: "active_animals_live_sql",
				Route:       domain.RouteSQL,
				ToolName:    "sql_fallback",
				Params: map[string]any{
					"sql":         sql,
					"park_label":  scope.label,
					"park_code":   scope.code,
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
	case weighingQuestion.MatchString(normalizedText):
		return sqlSubQuestion(q, "weighing_live_sql", weighingSQL(q.Actor.TenantID, scope, hasScope, wantsPenBreakdown(normalizedText) || wantsLowestWeightRanking(normalizedText)), "weighing"), true
	case vaccinationQuestion.MatchString(normalizedText) && (strings.Contains(normalizedText, "overload") || strings.Contains(normalizedText, "over capacity")):
		return sqlSubQuestion(q, "vaccination_operator_live_sql", vaccinationOperatorSQL(q.Actor.TenantID, scope, hasScope), "vaccination_operator"), true
	case feedQuestion.MatchString(normalizedText):
		return sqlSubQuestion(q, "feed_live_sql", feedSQL(q.Actor.TenantID, scope, hasScope, q.AsOf), "feed"), true
	case healthQuestion.MatchString(normalizedText):
		return sqlSubQuestion(q, "health_issue_live_sql", healthIssueSQL(q.Actor.TenantID), "health_issue"), true
	case opsRiskQuestion.MatchString(normalizedText):
		return sqlSubQuestion(q, "ops_risk_live_sql", opsRiskSQL(q.Actor.TenantID, scope, hasScope), "ops_risk"), true
	default:
		return domain.SubQuestion{}, false
	}
}

func sqlSubQuestion(q domain.Question, intentClass, sql, natural string) domain.SubQuestion {
	return domain.SubQuestion{
		ID:          "0",
		Text:        q.Text,
		IntentClass: intentClass,
		Route:       domain.RouteSQL,
		ToolName:    "sql_fallback",
		Params: map[string]any{
			"sql":         sql,
			"natural_sql": natural,
		},
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

func knownParkScopeMatches(low string, scope knownParkScope) bool {
	return strings.Contains(low, strings.ToLower(scope.code)) || strings.Contains(low, strings.ToLower(scope.label))
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
	for _, kw := range []string{"sale", "sales", "sold", "selling"} {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func activeAnimalsSQL(tenantID string, scope knownParkScope, groupBy, text string) string {
	groupCol := "species"
	switch groupBy {
	case "shed_label":
		groupCol = "shed_label"
	case "breed":
		groupCol = "breed"
	}
	where := fmt.Sprintf(
		"tenant_id = %s AND park_id = %s AND lifecycle_status = 'alive'",
		sqlStringLiteral(tenantID),
		sqlStringLiteral(scope.id),
	)
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
	return fmt.Sprintf(
		"SELECT 'Active animals' AS label, CAST(count(*) AS text) AS value, %s AS scope FROM ceo_ai.animal_current_scope WHERE %s GROUP BY %s ORDER BY count(*) DESC LIMIT 50",
		groupCol,
		where,
		groupCol,
	)
}

func weighingSQL(tenantID string, scope knownParkScope, hasScope, byPen bool) string {
	where := "tenant_id = " + sqlStringLiteral(tenantID) + " AND shed_weight_avg_kg IS NOT NULL"
	if hasScope {
		where += " AND park_id_text_not_used = park_id_text_not_used"
		where = strings.Replace(where, " AND park_id_text_not_used = park_id_text_not_used", " AND park_label = "+sqlStringLiteral(scope.label), 1)
	}
	if byPen {
		return fmt.Sprintf(
			"SELECT 'Average weight kg' AS label, CAST(shed_weight_avg_kg AS text) AS value, shed_label AS scope FROM ceo_ai.weighing_capture_activity WHERE %s ORDER BY shed_weight_avg_kg ASC LIMIT 50",
			where,
		)
	}
	return fmt.Sprintf(
		"SELECT 'Average weight kg' AS label, CAST(avg(shed_weight_avg_kg) AS text) AS value, 'selected scope' AS scope FROM ceo_ai.weighing_capture_activity WHERE %s LIMIT 50",
		where,
	)
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

func healthIssueSQL(tenantID string) string {
	return fmt.Sprintf(
		"SELECT source_label AS label, CAST(health_blockers AS text) AS value, load_label AS scope FROM ceo_ai.source_entry_health_status WHERE tenant_id = %s AND health_blockers > 0 ORDER BY health_blockers DESC LIMIT 50",
		sqlStringLiteral(tenantID),
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
