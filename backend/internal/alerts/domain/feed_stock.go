package domain

import "fmt"

// LowStockFeed is one (farm, feed) running out, as the feed module reports it. Mirrors
// feeddirection/domain.LowStockFeed field for field so the service can copy it across
// without this package importing the feed module.
type LowStockFeed struct {
	ParkID        string
	FarmLabel     string
	FeedItemLabel string
	FeedItemKey   string
	BalanceKg     string
	AvgDailyKg    string
	DaysLeft      int64
}

// DetectLowStock turns the feed module's low-stock rows into alerts. The feed read is
// already bounded to `withinDays`, so this only shapes the rows; a feed with under two
// days left is critical, the rest warning.
func DetectLowStock(businessDate string, parkNames map[string]string, feeds []LowStockFeed) []Alert {
	rule, _ := RuleByKey(RuleFeedLowStock)
	out := make([]Alert, 0, len(feeds))
	for _, f := range feeds {
		park := f.FarmLabel
		if n := parkNames[f.ParkID]; n != "" {
			park = n
		}
		sev := SeverityWarning
		if f.DaysLeft < 2 {
			sev = SeverityCritical
		}
		out = append(out, Alert{
			Key:          fmt.Sprintf("%s:%s:%s:%s", rule.Key, businessDate, f.ParkID, f.FeedItemKey),
			RuleKey:      rule.Key,
			RuleLabel:    rule.Label,
			Severity:     sev,
			Title:        fmt.Sprintf("%s: %s lasts %d more day(s)", park, f.FeedItemLabel, f.DaysLeft),
			Detail:       fmt.Sprintf("%s kg in store against about %s kg a day. Raise a purchase before it runs out.", f.BalanceKg, f.AvgDailyKg),
			ParkID:       f.ParkID,
			ParkLabel:    park,
			BusinessDate: businessDate,
			Href:         "/feed/analytics?tab=items",
		})
	}
	SortAlerts(out)
	return out
}
