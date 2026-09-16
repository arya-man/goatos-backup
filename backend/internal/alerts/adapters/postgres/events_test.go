package postgres

import (
	"fmt"
	"github.com/vgoats/goatos/backend/internal/alerts/domain"
	"strings"
	"testing"
)

func TestEventSQLBoundsBeforeEnrichmentAndKeepsTotal(t *testing.T) {
	for name, q := range map[string]string{"birth": birthEventsSQL, "exit": goatExitEventsSQL, "added": goatAddedEventsSQL, "shift": shiftingEventsSQL, "purchase": feedPurchaseEventsSQL} {
		t.Run(name, func(t *testing.T) {
			bound := strings.Index(q, fmt.Sprintf("LIMIT %d", domain.MaxEventRowsPerRule))
			outer := strings.Index(q, "FROM matched")
			if bound < 0 || outer < bound || !strings.Contains(q[:bound], "count(*) OVER () AS event_total") {
				t.Fatal("must bound source rows and preserve same-snapshot total before enrichment")
			}
			if strings.Contains(q, "g.display_id") {
				t.Fatal("internal ID cannot be presented as tag")
			}
		})
	}
	if !strings.Contains(shiftingEventsSQL, "shifting_event_id IN (SELECT shifting_event_id FROM matched)") {
		t.Fatal("impact aggregation must stay within the preview")
	}
}
func TestAnimalTagLookupExcludesNonRFIDIdentifiers(t *testing.T) {
	for _, q := range []string{goatTagSQL, birthEventsSQL} {
		if !strings.Contains(q, "gi.identifier_type IN ('animal_identifier_1', 'animal_identifier_2')") {
			t.Fatal("RFID lookup must exclude BLE and temporary identifiers")
		}
	}
	if animalSubject("") != "animal with no tag on record" {
		t.Fatal("missing tag must be explicit")
	}
}
