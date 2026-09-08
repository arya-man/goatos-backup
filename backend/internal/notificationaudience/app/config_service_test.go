package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/vgoats/goatos/backend/internal/notificationaudience/domain"
	"github.com/vgoats/goatos/backend/internal/notificationaudience/ports"
)

type configRepoFake struct {
	stored   map[string]ports.Audience
	replaced *ports.ReplaceAudienceCommand
	reset    string
}

func (r *configRepoFake) ListAudiences(context.Context, string) (map[string]ports.Audience, error) {
	return r.stored, nil
}
func (r *configRepoFake) LoadAudience(_ context.Context, _, key string) (ports.Audience, bool, error) {
	a, ok := r.stored[key]
	return a, ok, nil
}
func (r *configRepoFake) ReplaceAudience(_ context.Context, cmd ports.ReplaceAudienceCommand) (ports.Audience, error) {
	r.replaced = &cmd
	return ports.Audience{AlertKey: cmd.AlertKey, DesignationCodes: cmd.DesignationCodes, RowVersion: cmd.ExpectedRowVersion + 1}, nil
}
func (r *configRepoFake) ResetAudience(_ context.Context, _, _, key string) error {
	r.reset = key
	return nil
}
func (r *configRepoFake) ListDesignations(context.Context) ([]ports.Designation, error) {
	return []ports.Designation{{Code: "ceo_internal", Label: "CEO / CXO", Grade: "cxo"}, {Code: "park_head", Label: "Park Head"}}, nil
}

func TestMatrixCarriesEveryCatalogRowWithEffectiveAudience(t *testing.T) {
	repo := &configRepoFake{stored: map[string]ports.Audience{
		domain.AlertFeedLowStock: {AlertKey: domain.AlertFeedLowStock, DesignationCodes: []string{"park_head"}, RowVersion: 3},
	}}
	m, err := NewConfigService(repo).Matrix(context.Background(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Alerts) != len(domain.Catalog()) {
		t.Fatalf("matrix rows = %d want every catalog row (%d)", len(m.Alerts), len(domain.Catalog()))
	}
	if len(m.Designations) != 2 || m.Designations[0].Label != "CEO / CXO" {
		t.Fatalf("designations = %+v", m.Designations)
	}
	var low, overdue AlertRow
	for _, row := range m.Alerts {
		switch row.Key {
		case domain.AlertFeedLowStock:
			low = row
		case domain.AlertProcurementLoadOverdue:
			overdue = row
		}
		if row.ModuleLabel == "" || row.Label == "" {
			t.Fatalf("row %s must carry farm labels: %+v", row.Key, row)
		}
	}
	if !low.Customised || low.RowVersion != 3 || !reflect.DeepEqual(low.Designations, []string{"park_head"}) {
		t.Fatalf("customised row = %+v", low)
	}
	if !reflect.DeepEqual(low.DefaultDesignations, []string{"ceo_internal", "feed_director", "procurement_director"}) {
		t.Fatalf("customised row must still show its default: %+v", low)
	}
	if overdue.Customised || overdue.RowVersion != 0 || !reflect.DeepEqual(overdue.Designations, overdue.DefaultDesignations) {
		t.Fatalf("default row = %+v", overdue)
	}
}

func TestSaveNormalisesCodesAndFencesOnTheVersion(t *testing.T) {
	repo := &configRepoFake{}
	row, err := NewConfigService(repo).Save(context.Background(), tenant, "actor", domain.AlertFeedLowStock, SaveAudienceRequest{
		DesignationCodes: []string{" Park_Head ", "ceo_internal", "park_head", ""},
		RowVersion:       2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.replaced == nil || repo.replaced.ExpectedRowVersion != 2 || repo.replaced.ActorID != "actor" {
		t.Fatalf("replace cmd = %+v", repo.replaced)
	}
	if !reflect.DeepEqual(repo.replaced.DesignationCodes, []string{"ceo_internal", "park_head"}) {
		t.Fatalf("codes must be trimmed, lowercased, deduped and sorted: %v", repo.replaced.DesignationCodes)
	}
	if !row.Customised || row.RowVersion != 3 {
		t.Fatalf("saved row = %+v", row)
	}
}

func TestSaveUseDefaultsResetsAndReportsTheDefault(t *testing.T) {
	repo := &configRepoFake{}
	row, err := NewConfigService(repo).Save(context.Background(), tenant, "actor", domain.AlertProcurementLoadOverdue, SaveAudienceRequest{
		UseDefaults: true, DesignationCodes: []string{"park_head"}, RowVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.reset != domain.AlertProcurementLoadOverdue || repo.replaced != nil {
		t.Fatalf("use_defaults must reset, not replace: reset=%q replaced=%+v", repo.reset, repo.replaced)
	}
	if row.Customised || !reflect.DeepEqual(row.Designations, []string{"ceo_internal"}) {
		t.Fatalf("row after reset = %+v", row)
	}
}

func TestSaveRefusesUnknownAlertAndNegativeVersion(t *testing.T) {
	svc := NewConfigService(&configRepoFake{})
	if _, err := svc.Save(context.Background(), tenant, "a", "nosuch.alert", SaveAudienceRequest{}); !errors.Is(err, ports.ErrUnknownAlert) {
		t.Fatalf("unknown alert err = %v", err)
	}
	if _, err := svc.Save(context.Background(), tenant, "a", domain.AlertFeedLowStock, SaveAudienceRequest{RowVersion: -1}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("negative version err = %v", err)
	}
}
