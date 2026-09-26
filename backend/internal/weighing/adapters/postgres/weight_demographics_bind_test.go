package postgres

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// weightDemographicsBaseArgs is the fixed bind list GetWeightDemographics hands sqlbind: $1..$36.
// The month variant adds $37 (weightDemographicsAnchorParam). Kept as a count, not values, because this test proves the SHAPE.
const weightDemographicsBaseArgs = weightDemographicsAnchorParam - 1

// TestWeightDemographicsEverySectionSubsetBindsItsPlaceholders is the regression for the
// /weighing/analytics 500: every ADG Analytics tab asks for ONE section, the pruner then replaces
// the other sections' SELECT arms with literals, and when the last reader of a placeholder is
// pruned away the query names fewer placeholders than it is handed arguments. pgx refuses that
// ("expected 30 arguments, got 31") and the page lost every chart. The anchor in _param_types
// keeps each placeholder alive whatever is pruned; this test walks all 2^7 section subsets in both
// bucket variants so a future arm that becomes the sole reader of a parameter is caught here, not
// by a leadership page going blank.
func TestWeightDemographicsEverySectionSubsetBindsItsPlaceholders(t *testing.T) {
	keys := []string{"composition", "dimensions", "origin", "shed_type", "weight_bands", "weekly_gain", "gain_thresholds"}
	tmpl := demographicsTemplateFromSource(t)
	for _, bucket := range []string{domain.GainBucketWeek, domain.GainBucketMonth} {
		argc := weightDemographicsBaseArgs
		if bucket == domain.GainBucketMonth {
			argc++
		}
		args := make([]any, argc)
		for mask := 0; mask < 1<<len(keys); mask++ {
			sections := map[string]bool{}
			var names []string
			for i, k := range keys {
				if mask&(1<<i) != 0 {
					sections[k] = true
					names = append(names, k)
				}
			}
			query := weightDemographicsPruneInactiveSectionSelects(bucketedQuery(tmpl, bucket, weightDemographicsAnchorParam), sections)
			if err := sqlbind.ValidatePositional(query, args); err != nil {
				t.Fatalf("bucket=%s sections=%q: %v", bucket, strings.Join(names, ","), err)
			}
		}
	}
}

// The single-tab reads the page actually issues, named so a failure reads as the tab that broke.
func TestWeightDemographicsWeightBandsPrunedStillBindsBandEdges(t *testing.T) {
	tmpl := demographicsTemplateFromSource(t)
	for _, tab := range []string{"dimensions", "origin", "shed_type", "weekly_gain", "composition,dimensions,gain_thresholds"} {
		query := weightDemographicsPruneInactiveSectionSelects(bucketedQuery(tmpl, domain.GainBucketWeek, weightDemographicsAnchorParam), weightDemographicsSectionSet(tab))
		if err := sqlbind.ValidatePositional(query, make([]any, weightDemographicsBaseArgs)); err != nil {
			t.Fatalf("tab sections %q: %v", tab, err)
		}
	}
}
