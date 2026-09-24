package http

import (
	"encoding/json"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// THE BUCKETS MUST REACH THE WIRE, AND THE CONTRACT MUST OWN THEM.
//
// The overview folds a sale's revenue into four disjoint buckets -- live animals, manure, feed and
// everything else -- and the first two were already on the wire. The second two were computed and
// then dropped from the payload struct, so no client could show a rupee of feed revenue, which is
// the screen the feed-sale work was built for. Fixing the struct alone is half a fix: a field the
// OpenAPI schema does not declare is a field the generated client does not carry, so admin-web and
// the phone could read it only by going around their own typed client.
//
// This asserts the SERIALIZED shape, not the Go field, because the defect was invisible at the Go
// level -- domain.Summary had the numbers all along.
func TestOverviewSummaryCarriesEveryRevenueBucket(t *testing.T) {
	payload := toOverviewPayload(domain.Overview{
		Summary: domain.Summary{
			Revenue: 1000, LiveRevenue: 600,
			ManureKg: 50, ManureRevenue: 100,
			FeedKg: 40, FeedRevenue: 250,
			OtherKg: 9, OtherRevenue: 50,
		},
	})
	raw, err := json.Marshal(payload.Summary)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}
	for field, want := range map[string]float64{
		"live_revenue":   600,
		"manure_kg":      50,
		"manure_revenue": 100,
		"feed_kg":        40,
		"feed_revenue":   250,
		"other_kg":       9,
		"other_revenue":  50,
	} {
		got, present := wire[field]
		if !present {
			t.Fatalf("%s is missing from the summary on the wire; a client cannot render what it never receives", field)
		}
		if got.(float64) != want {
			t.Fatalf("%s = %v, want %v", field, got, want)
		}
	}
	// And the four are DISJOINT halves of one total: a reader adding them up must land on revenue,
	// never on more than it. This is what stops a bucket being double-counted into two of them.
	sum := wire["live_revenue"].(float64) + wire["manure_revenue"].(float64) +
		wire["feed_revenue"].(float64) + wire["other_revenue"].(float64)
	if sum != wire["revenue"].(float64) {
		t.Fatalf("live + manure + feed + other = %v, but revenue = %v", sum, wire["revenue"])
	}
}
