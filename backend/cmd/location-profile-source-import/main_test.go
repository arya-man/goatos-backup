package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	locationapp "github.com/vgoats/goatos/backend/internal/locations/app"
	locationdomain "github.com/vgoats/goatos/backend/internal/locations/domain"
)

func TestParseLocationProfileRowsDefaultsShedsDBAndDerivesKeys(t *testing.T) {
	input := strings.NewReader(strings.Join([]string{
		`{"kind":"location_alias","location_id":"00000000-0000-4000-8000-000000000101","alias_code":"Gandhi 1 - Part 1","source_ref":"Sheds DB.xlsx#DB!A6"}`,
		`{"kind":"location_capacity","location_id":"00000000-0000-4000-8000-000000000101","capacity_value":25,"effective_from":"2026-06-30","source_ref":"Sheds DB.xlsx#DB!D6"}`,
	}, "\n") + "\n")
	rows, err := parseImportRows(input, config{TenantID: "00000000-0000-4000-8000-000000000001", SourceContext: defaultSource, CapacitySource: defaultSource})
	if err != nil {
		t.Fatalf("parseImportRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%d, want 2", len(rows))
	}
	alias := rows[0].Alias
	if alias.SourceContext != "sheds_db" || !strings.Contains(alias.Notes, "Sheds DB.xlsx#DB!A6") {
		t.Fatalf("alias row did not default source/notes: %#v", alias)
	}
	capacity := rows[1].Capacity
	if capacity.Source != "sheds_db" || capacity.CapacityKind != "goat_occupancy" {
		t.Fatalf("capacity row did not default source/kind: %#v", capacity)
	}
	if !rows[0].DerivedIdempotency || !rows[1].DerivedIdempotency {
		t.Fatalf("expected idempotency keys to be derived")
	}
}

func TestParseLocationReviewItemDerivesEvidenceHash(t *testing.T) {
	input := strings.NewReader(`{"kind":"location_review_item","tenant_id":"00000000-0000-4000-8000-000000000001","source_label":"Unmapped Shed Tag","normalized_source_label":"unmapped shed tag"}` + "\n")
	rows, err := parseImportRows(input, config{SourceContext: defaultSource})
	if err != nil {
		t.Fatalf("parseImportRows: %v", err)
	}
	review := rows[0].Review
	if review.ReviewType != "unknown_alias" || review.SourceContext != "sheds_db" {
		t.Fatalf("review defaults not applied: %#v", review)
	}
	if !strings.HasPrefix(review.EvidenceHash, "sha256:") || !rows[0].DerivedEvidenceHash {
		t.Fatalf("expected derived evidence hash, got %q derived=%t", review.EvidenceHash, rows[0].DerivedEvidenceHash)
	}
	var evidence map[string]any
	if err := json.Unmarshal(review.Evidence, &evidence); err != nil {
		t.Fatalf("evidence json: %v", err)
	}
	if evidence["source_context"] != "sheds_db" {
		t.Fatalf("evidence=%v", evidence)
	}
}

func TestLocationProfileDryRunDoesNotNeedDatabase(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"-dry-run", "-tenant-id", "00000000-0000-4000-8000-000000000001"}, strings.NewReader(`{"kind":"location_capacity","location_id":"00000000-0000-4000-8000-000000000101","capacity_value":25,"effective_from":"2026-06-30"}`+"\n"), &out)
	if err != nil {
		t.Fatalf("run dry-run: %v", err)
	}
	if !strings.Contains(out.String(), "dry-run ok rows=1") {
		t.Fatalf("output=%s", out.String())
	}
}

func TestImportRowsWritesThroughLocationService(t *testing.T) {
	rows, err := parseImportRows(strings.NewReader(strings.Join([]string{
		`{"kind":"location_alias","tenant_id":"00000000-0000-4000-8000-000000000001","location_id":"00000000-0000-4000-8000-000000000101","alias_code":"Gandhi 1"}`,
		`{"kind":"location_capacity","tenant_id":"00000000-0000-4000-8000-000000000001","location_id":"00000000-0000-4000-8000-000000000101","capacity_value":50,"effective_from":"2026-06-30"}`,
		`{"kind":"location_review_item","tenant_id":"00000000-0000-4000-8000-000000000001","source_label":"Unknown Shed"}`,
	}, "\n")+"\n"), config{SourceContext: defaultSource, CapacitySource: defaultSource})
	if err != nil {
		t.Fatalf("parseImportRows: %v", err)
	}
	service := &fakeLocationImportService{}
	var out bytes.Buffer
	summary, err := importRows(context.Background(), service, config{ActorID: "00000000-0000-4000-8000-000000000010"}, rows, &out)
	if err != nil {
		t.Fatalf("importRows: %v", err)
	}
	if summary.Aliases != 1 || summary.Capacities != 1 || summary.Reviews != 1 {
		t.Fatalf("summary=%+v", summary)
	}
	if len(service.aliases) != 1 || len(service.capacities) != 1 || len(service.reviews) != 1 {
		t.Fatalf("service writes aliases=%d capacities=%d reviews=%d", len(service.aliases), len(service.capacities), len(service.reviews))
	}
	if !bytes.Contains(service.capacities[0].RawBody, []byte(`"source":"sheds_db"`)) {
		t.Fatalf("capacity raw body=%s", service.capacities[0].RawBody)
	}
}

type fakeLocationImportService struct {
	aliases    []locationapp.CreateLocationAliasInput
	capacities []locationapp.CreateLocationCapacityInput
	reviews    []locationapp.CreateLocationReviewItemInput
}

func (f *fakeLocationImportService) CreateLocationAlias(_ context.Context, in locationapp.CreateLocationAliasInput) (*locationdomain.LocationAliasResponse, error) {
	f.aliases = append(f.aliases, in)
	return &locationdomain.LocationAliasResponse{}, nil
}

func (f *fakeLocationImportService) CreateLocationCapacity(_ context.Context, in locationapp.CreateLocationCapacityInput) (*locationdomain.LocationCapacityResponse, error) {
	f.capacities = append(f.capacities, in)
	return &locationdomain.LocationCapacityResponse{}, nil
}

func (f *fakeLocationImportService) CreateLocationReviewItem(_ context.Context, in locationapp.CreateLocationReviewItemInput) (*locationdomain.LocationReviewItemResponse, error) {
	f.reviews = append(f.reviews, in)
	return &locationdomain.LocationReviewItemResponse{}, nil
}
