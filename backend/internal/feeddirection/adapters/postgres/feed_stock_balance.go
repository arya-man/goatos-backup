package postgres

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

// feedConfigNorm is the Go twin of the SQL feed_config_norm(): trim, casefold, collapse runs of
// whitespace/underscore/hyphen to one underscore. It exists because the stock read returns rows
// already keyed that way and a caller asking about "Mesha Kids Goat Concentrate" must match them.
//
// A hand-written twin of a SQL function is a drift risk, so it is PINNED: an integration test
// asserts the two agree label for label against the real database function. If that test ever goes
// red, this is what changed, and the answer is to fix this rather than to loosen the test.
var feedConfigNormRuns = regexp.MustCompile(`[\s_-]+`)

func feedConfigNorm(value string) string {
	return strings.ToLower(feedConfigNormRuns.ReplaceAllString(strings.TrimSpace(value), "_"))
}

// FeedStockIdentity resolves a sold feed to the same family key the stock cards use.
// The label lets a combined shortage name the shared store rather than one sibling.
func (r *Repository) FeedStockIdentity(label string) (key, stockLabel string) {
	key = feedConfigNorm(label)
	for _, row := range domain.StockFamilyMerge {
		if key == row.MemberKey || key == row.FamilyKey {
			return row.FamilyKey, row.FamilyLabel
		}
	}
	return key, strings.TrimSpace(label)
}

// FeedBalanceKg answers how many kilograms of one feed one farm holds, for the sales module's
// short-sale confirmation (sales/ports.FeedStockReader, maintainer decision 2026-09-23).
//
// It REUSES the Stock tab's own read rather than asking the ledger a second way. The balance is
// purchased less fed less already sold, with the transitional concentrate fold folded in, and a
// second implementation of that here would be a number that disagrees with the card the desk is
// looking at the moment either side changes. The cost is reading a farm's whole item list to
// answer about one feed, which is a handful of rows and the same read the Stock tab makes.
//
// known is false when the store has no ledger for that (farm, feed) at all. That is a DIFFERENT
// fact from a balance of zero and the caller must not render it as "none left": a feed nobody has
// ever bought through the ledger has no opinion to offer about a sale.
func (r *Repository) FeedBalanceKg(ctx context.Context, tenantID, farmLabel, feedItemLabel string) (float64, bool, error) {
	items, err := r.stockItems(ctx, tenantID, nil)
	if err != nil {
		return 0, false, fmt.Errorf("feed balance for sale: %w", err)
	}
	// The card's family key, so a feed folded into a successor is answered at the balance the tab
	// shows for it -- selling from a merged store draws on the family, not on one retired sack.
	want, _ := r.FeedStockIdentity(feedItemLabel)
	if want == "" {
		return 0, false, nil
	}
	for _, it := range items {
		if !strings.EqualFold(it.FarmLabel, farmLabel) {
			continue
		}
		if feedConfigNorm(it.FeedItemLabel) != want && it.FeedItemKey != want {
			continue
		}
		kg, err := strconv.ParseFloat(it.BalanceKg, 64)
		if err != nil {
			return 0, false, fmt.Errorf("feed balance %q for sale: %w", it.BalanceKg, err)
		}
		return kg, true, nil
	}
	return 0, false, nil
}
