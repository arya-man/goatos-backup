package postgres

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// A successor can be sold before its first purchase: the physical stock is held
// by legacy members. Every balance must charge it once, even with several members.
func TestSuccessorFeedSaleUsesLegacyFamilyStock(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	const member = "Mesha Adult Concentrate Goat"
	const successor = "Mesha Adult Concentrate"
	for _, label := range []string{member, successor, "Mesha Adult Concentrate Sheep"} {
		if _, err := pool.Exec(ctx, `INSERT INTO feed_item_catalog (tenant_id,feed_item_label) VALUES ($1,$2) ON CONFLICT (tenant_id,feed_item_key) DO UPDATE SET status='active'`, fdiTenant, label); err != nil {
			t.Fatal(err)
		}
	}
	purchase := func(label string, batch int, qty int, day string) {
		t.Helper()
		_, err := pool.Exec(ctx, `INSERT INTO feed_purchases (tenant_id,park_id,farm_label,feed_item_label,batch_no,purchase_date,quantity_kg,per_kg_cost,total_cost,consumed_at_import_kg,depletes_from,vendor,payment_status,delivery_status,reached_on)
VALUES ($1,$2,'CBE',$3,$4,$6::date,$5::numeric,20,$5::numeric*20,0,$6::date,'Navaladi','Paid','reached',$6::date)`, fdiTenant, fdiPark, label, batch, qty, day)
		if err != nil {
			t.Fatal(err)
		}
	}
	purchase(member, 801, 5000, "2026-08-01")
	// Feeding before this member's arrival must remain excluded.
	for _, day := range []string{"2026-07-31", "2026-08-02"} {
		at, err := time.Parse("2006-01-02", day)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt: at, Fingerprint: "family-" + day, IdempotencyKey: "family-" + day, GeneratedBy: "test",
			Cells: []domain.StoredCell{{ParkID: fdiPark, ParkLabel: "CBE", ShedID: fdiShedA, ShedLabel: "Castro", PartitionLabel: "1", ShedTag: "Non-Pregnant", Breed: "Beetal", RationGroup: "Beetal/Sirohi", SessionNo: 1, SessionLabel: "Morning", HeadCount: 10, Workflow: domain.WorkflowNormal, FeedItemLabel: member, FeedItemKey: "mesha_adult_concentrate_goat", QuantityKg: kg("100.000"), SessionTotalKg: "100.000"}},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.LockIssue(ctx, ports.LockIssueCommand{TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day, Workflow: domain.WorkflowNormal, LockedAt: at}); err != nil {
			t.Fatal(err)
		}
	}

	var dealID string
	if err := pool.QueryRow(ctx, `INSERT INTO sales_deals (tenant_id,sale_date,farm,buyer_name,product_type,breed,sales_value) VALUES ($1,'2026-08-03','CBE','Family stock buyer','Feed',$2,20000) RETURNING id::text`, fdiTenant, successor).Scan(&dealID); err != nil {
		t.Fatal(err)
	}
	// Two same-feed lines guard against multiplying or dropping sales at the join.
	for line := 1; line <= 2; line++ {
		var lineID string
		if err := pool.QueryRow(ctx, `INSERT INTO sales_deal_lines (tenant_id,deal_id,line_no,product_type,product_code,product_kind,breed,quantity,unit,rate_per_unit,sales_value) VALUES ($1,$2,$3,'Feed','feed','feed',$4,500,'kg',20,10000) RETURNING line_id::text`, fdiTenant, dealID, line, successor).Scan(&lineID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO feed_sale_depletions (tenant_id,deal_id,line_id,park_id,farm_label,feed_item_label,feed_day,quantity_kg) VALUES ($1,$2,$3,$4,'CBE',$5,'2026-08-03',500)`, fdiTenant, dealID, lineID, fdiPark, successor); err != nil {
			t.Fatal(err)
		}
	}
	check := func(want int) {
		t.Helper()
		items, err := repo.stockItems(ctx, fdiTenant, []uuid.UUID{uuid.MustParse(fdiPark)})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, it := range items {
			if it.FeedItemKey == "mesha_adult_concentrate" {
				found = true
				if it.BalanceKg != fmt.Sprintf("%d.0", want) || it.AvgDailyKg != "100.0" || it.DaysLeft == nil || *it.DaysLeft != int64(want/100) {
					t.Errorf("card want %d kg at 100 kg/day: %+v", want, it)
				}
			}
		}
		if !found {
			t.Error("family card missing")
		}
		balance, known, err := repo.FeedBalanceKg(ctx, fdiTenant, "CBE", successor)
		if err != nil || !known || balance != float64(want) {
			t.Errorf("sale confirmation balance=%v known=%v err=%v", balance, known, err)
		}
		farms, err := repo.stockFarmItems(ctx, fdiTenant, nil)
		if err != nil {
			t.Fatal(err)
		}
		found = false
		for _, it := range farms {
			if it.FeedItemKey == "mesha_adult_concentrate" {
				found = true
				if it.LedgerStockKg != fmt.Sprintf("%d.0", want) {
					t.Errorf("farm balance: %+v", it)
				}
			}
		}
		if !found {
			t.Error("farm card missing")
		}
		alerts, err := repo.LowStockFeeds(ctx, fdiTenant, 100)
		if err != nil {
			t.Fatal(err)
		}
		found = false
		for _, it := range alerts {
			if it.FeedItemKey == "mesha_adult_concentrate" {
				found = true
				if it.DaysLeft != int64(want/100) {
					t.Errorf("alert: %+v", it)
				}
			}
		}
		if !found {
			t.Error("alert missing")
		}
		page, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{})
		if err != nil {
			t.Fatal(err)
		}
		var left, consumed float64
		for _, row := range page.Rows {
			n, err := strconv.ParseFloat(row.LeftKg, 64)
			if err != nil {
				t.Fatal(err)
			}
			left += n
			n, err = strconv.ParseFloat(row.ConsumedKg, 64)
			if err != nil {
				t.Fatal(err)
			}
			consumed += n
		}
		if left != float64(want) || consumed != 100 {
			t.Errorf("load totals left=%v consumed=%v, want %d and 100: %+v", left, consumed, want, page.Rows)
		}
	}
	check(3900)
	purchase("Mesha Adult Concentrate Sheep", 802, 2000, "2026-08-02")
	check(5900)
	purchase(successor, 803, 1000, "2026-08-05")
	check(6900)
	page, err := repo.StockLoads(ctx, fdiTenant, nil, domain.StockLoadsQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range page.Rows {
		if row.BatchNo == 801 && row.LeftKg != "3900.0" {
			t.Errorf("later successor delivery moved the prior sale: %+v", row)
		}
		if row.BatchNo == 803 && row.LeftKg != "1000.0" {
			t.Errorf("later successor load charged for prior sale: %+v", row)
		}
	}
	// A sale after the successor arrives charges it, once, beside prior legacy sales.
	var laterLine string
	if err := pool.QueryRow(ctx, `INSERT INTO sales_deal_lines (tenant_id,deal_id,line_no,product_type,product_code,product_kind,breed,quantity,unit,rate_per_unit,sales_value) VALUES ($1,$2,3,'Feed','feed','feed',$3,500,'kg',20,10000) RETURNING line_id::text`, fdiTenant, dealID, successor).Scan(&laterLine); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO feed_sale_depletions (tenant_id,deal_id,line_id,park_id,farm_label,feed_item_label,feed_day,quantity_kg) VALUES ($1,$2,$3,$4,'CBE',$5,'2026-08-06',500)`, fdiTenant, dealID, laterLine, fdiPark, successor); err != nil {
		t.Fatal(err)
	}
	check(6400)
	// Small-fixture SQL latency smoke only; this does not certify production HTTP latency.
	for name, read := range map[string]func() error{
		"stock_items": func() error { _, err := repo.stockItems(ctx, fdiTenant, nil); return err },
		"stock_loads": func() error { _, err := repo.loadStockLoads(ctx, fdiTenant, nil, "", "", 50, 0); return err },
	} {
		samples := make([]time.Duration, 20)
		for i := range samples {
			start := time.Now()
			if err := read(); err != nil {
				t.Fatal(err)
			}
			samples[i] = time.Since(start)
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		t.Logf("%s SQL fixture: 3 purchases, 3 sale lines, 2 feed days; p90=%s p95=%s p99=%s", name, samples[17], samples[18], samples[19])
	}
	if _, err := pool.Exec(ctx, `DELETE FROM feed_sale_depletions WHERE tenant_id=$1 AND deal_id=$2`, fdiTenant, dealID); err != nil {
		t.Fatal(err)
	}
	check(7900)
	items, err := repo.stockItems(ctx, fdiTenant, []uuid.UUID{uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("stock leaked outside park: %+v", items)
	}
}
