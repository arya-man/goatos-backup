package app

import (
	"context"
	fwrdomain "github.com/vgoats/goatos/backend/internal/feedwaterremoval/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"testing"
)

type countingCampaignCutoff struct {
	calls int
	value string
}

func (r *countingCampaignCutoff) FeedWaterRemovalCutoff(context.Context, string) (fwrdomain.Cutoff, error) {
	r.calls++
	return fwrdomain.ParseCutoff(r.value)
}
func TestCampaignPageReadsFarmCutoffOncePerRequest(t *testing.T) {
	reader := &countingCampaignCutoff{value: "20:00"}
	service := &Service{cutoffs: reader}
	for _, count := range []int{20, 100} {
		reader.calls = 0
		rows := make([]domain.Campaign, count)
		if err := service.decorateCampaignRules(context.Background(), "tenant", rows); err != nil {
			t.Fatal(err)
		}
		if reader.calls != 1 {
			t.Fatalf("%d campaigns caused %d cutoff reads; want 1", count, reader.calls)
		}
		for _, row := range rows {
			if row.SOP.FeedWaterRemoval.CutoffTime != reader.value {
				t.Fatal("stale cutoff")
			}
		}
		reader.value = "21:30"
	}
}

func TestCampaignCutoffKeepsPinnedEveningAndDoesNotMutateCache(t *testing.T) {
	reader := &countingCampaignCutoff{value: "20:00"}
	farm := domain.SeededRules()
	farm.Version = 1
	own := domain.SeededRules()
	own.Version = 2
	own.FeedWaterRemoval.CutoffTime = "21:30"
	off := domain.SeededRules()
	off.Version = 3
	off.FeedWaterRemoval.Mode = domain.RemovalModeOff
	service := (&Service{cutoffs: reader}).WithSOPRules(versionedRules{1: farm, 2: own, 3: off}, nil)
	rows := []domain.Campaign{{SOPVersion: 1}, {SOPVersion: 2}, {SOPVersion: 3}, {SOPVersion: 0}, {SOPVersion: 1}}
	if err := service.decorateCampaignRules(context.Background(), "tenant", rows); err != nil {
		t.Fatal(err)
	}
	if reader.calls != 1 {
		t.Fatalf("mixed pins read farm cutoff %d times", reader.calls)
	}
	for i, want := range []string{"20:00", "21:30", "", "20:00", "20:00"} {
		if rows[i].SOP.FeedWaterRemoval.CutoffTime != want {
			t.Fatalf("row %d cutoff %q, want %q", i, rows[i].SOP.FeedWaterRemoval.CutoffTime, want)
		}
	}
	cached, err := service.rulesForVersion(context.Background(), "tenant", 1)
	if err != nil || cached.FeedWaterRemoval.CutoffTime != "" {
		t.Fatal("effective cutoff contaminated pinned cache", err)
	}
	reader.calls = 0
	if err := service.decorateCampaignRules(context.Background(), "tenant", rows[1:3]); err != nil {
		t.Fatal(err)
	}
	if reader.calls != 0 {
		t.Fatal("own evening/off must not read farm config")
	}
}
