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

// FeedBalancesKg reads the stock once for a sale, with no cross-request cache.
// Keeping this snapshot request-local preserves immediate close/reopen readback.
func (r *Repository) FeedBalancesKg(ctx context.Context, tenantID, farmLabel string) (map[string]float64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	items, err := r.stockBalances(ctx, tenantID, nil)
	if err != nil {
		return nil, fmt.Errorf("feed balances for sale: %w", err)
	}
	balances := make(map[string]float64)
	for _, it := range items {
		if !strings.EqualFold(it.FarmLabel, farmLabel) {
			continue
		}
		kg, err := strconv.ParseFloat(it.BalanceKg, 64)
		if err != nil {
			return nil, fmt.Errorf("feed balance %q for sale: %w", it.BalanceKg, err)
		}
		key, _ := r.FeedStockIdentity(it.FeedItemLabel)
		balances[key] = kg
	}
	return balances, nil
}

// FeedBalanceKg is the single-feed adapter for callers that need just one balance.
func (r *Repository) FeedBalanceKg(ctx context.Context, tenantID, farmLabel, feedItemLabel string) (float64, bool, error) {
	balances, err := r.FeedBalancesKg(ctx, tenantID, farmLabel)
	if err != nil {
		return 0, false, err
	}
	key, _ := r.FeedStockIdentity(feedItemLabel)
	kg, known := balances[key]
	return kg, known, nil
}
