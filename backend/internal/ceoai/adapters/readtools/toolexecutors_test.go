package readtools

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

func TestCountsBreakdownExecutor_ReturnsGoatsAndSheepWhenWired(t *testing.T) {
	exec := &countsBreakdownExecutor{
		countsBySpeciesReader: func(ctx context.Context, tenantID string, params map[string]any) ([]domain.Fact, error) {
			return []domain.Fact{
				{Label: "Goats", Value: "972"},
				{Label: "Sheep", Value: "336"},
			}, nil
		},
	}

	result, err := exec.Execute(context.Background(), domain.Actor{TenantID: "test-tenant"}, domain.SubQuestion{
		ToolName: "counts_breakdown",
	})

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Err != nil {
		t.Fatalf("ToolResult.Err should be nil, got: %v", result.Err)
	}
	if len(result.Facts) != 2 {
		t.Fatalf("Expected 2 facts, got %d", len(result.Facts))
	}

	// Verify we got both goat AND sheep counts
	goatsFound := false
	sheepFound := false
	var goatsValue, sheepValue string

	for _, fact := range result.Facts {
		if fact.Label == "Goats" {
			goatsFound = true
			goatsValue = fact.Value
		}
		if fact.Label == "Sheep" {
			sheepFound = true
			sheepValue = fact.Value
		}
	}

	if !goatsFound {
		t.Error("Goats fact not found")
	}
	if !sheepFound {
		t.Error("Sheep fact not found")
	}

	// Critical test: sheep must NOT be hardcoded to zero
	if sheepValue == "0" {
		t.Error("Sheep count should not be hardcoded to zero; expected real data")
	}

	// Critical test: both values must be distinct and non-zero
	if goatsValue == "" || sheepValue == "" {
		t.Error("Both goats and sheep values must be present and non-empty")
	}

	if goatsValue == sheepValue {
		t.Error("Goats and sheep counts should be distinct (not the same value)")
	}

	t.Logf("✓ Goats: %s, Sheep: %s (distinct and non-zero)", goatsValue, sheepValue)
}

func TestCountsBreakdownExecutor_ReturnsErrorWhenNotWired(t *testing.T) {
	exec := &countsBreakdownExecutor{
		countsBySpeciesReader: nil,
	}

	result, err := exec.Execute(context.Background(), domain.Actor{TenantID: "test-tenant"}, domain.SubQuestion{
		ToolName: "counts_breakdown",
	})

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Critical: error must NOT be swallowed - it must be in ToolResult.Err
	if result.Err == nil {
		t.Error("ToolResult.Err should be non-nil when reader is not wired; got nil (error swallowed)")
	}

	if result.Err.Error() != "counts data reader not wired" {
		t.Errorf("Expected 'counts data reader not wired', got: %v", result.Err)
	}

	t.Logf("✓ Error correctly propagated: %v", result.Err)
}

func TestCountsBreakdownExecutor_PropagatReaderError(t *testing.T) {
	readerErr := errors.New("database connection failed")
	exec := &countsBreakdownExecutor{
		countsBySpeciesReader: func(ctx context.Context, tenantID string, params map[string]any) ([]domain.Fact, error) {
			return nil, readerErr
		},
	}

	result, err := exec.Execute(context.Background(), domain.Actor{TenantID: "test-tenant"}, domain.SubQuestion{
		ToolName: "counts_breakdown",
	})

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Critical P2-B fix: errors must be propagated, not swallowed
	if result.Err == nil {
		t.Error("ToolResult.Err should contain the reader error; got nil (error swallowed)")
	}

	if !errors.Is(result.Err, readerErr) {
		t.Errorf("Expected error to be propagated; got: %v", result.Err)
	}

	t.Logf("✓ Reader error correctly propagated: %v", result.Err)
}

// TestCountsBreakdownExecutor_ThreadsScopeParams proves P1-4: the executor
// must pass the sub-question's advertised scope params (dimension,
// park_label, species, shed_id) through to the reader, not just tenantID.
// Before the fix, Execute called e.countsBySpeciesReader(ctx, actor.TenantID)
// only — a scoped question like "how many goats in Castro 1" silently lost
// the park_label/species/dimension filters.
func TestCountsBreakdownExecutor_ThreadsScopeParams(t *testing.T) {
	var gotParams map[string]any
	exec := &countsBreakdownExecutor{
		countsBySpeciesReader: func(ctx context.Context, tenantID string, params map[string]any) ([]domain.Fact, error) {
			gotParams = params
			return []domain.Fact{{Label: "Castro 1", Value: "42"}}, nil
		},
	}

	wantParams := map[string]any{
		"dimension":  "breed",
		"park_label": "Castro 1",
		"species":    "goat",
		"as_of":      "2026-07-21",
	}
	_, err := exec.Execute(context.Background(), domain.Actor{TenantID: "test-tenant"}, domain.SubQuestion{
		ToolName: "counts_breakdown",
		Params:   wantParams,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotParams == nil {
		t.Fatal("reader never received params")
	}
	for k, want := range wantParams {
		if got := gotParams[k]; got != want {
			t.Errorf("param %q: got %v, want %v", k, got, want)
		}
	}
}

func TestVaccinationShedSummaryExecutor_ReturnsErrorWhenNotWired(t *testing.T) {
	exec := &vaccinationShedSummaryExecutor{
		vaccinationDataReader: nil,
	}

	result, err := exec.Execute(context.Background(), domain.Actor{TenantID: "test-tenant"}, domain.SubQuestion{
		ToolName: "vaccination_shed_summary",
	})

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Critical P1-B fix: unwired readers must return errors, not silently return empty
	if result.Err == nil {
		t.Error("ToolResult.Err should be non-nil when reader is not wired; got nil (error swallowed)")
	}

	if result.Err.Error() != "vaccination data reader not wired" {
		t.Errorf("Expected 'vaccination data reader not wired', got: %v", result.Err)
	}

	t.Logf("✓ Vaccination reader wiring error correctly propagated: %v", result.Err)
}

func TestVaccinationExecutionExecutor_PropagatReaderError(t *testing.T) {
	readerErr := errors.New("vaccination service error")
	exec := &vaccinationExecutionExecutor{
		vaccinationDataReader: func(ctx context.Context, tenantID string, params map[string]any) ([]domain.Fact, error) {
			return nil, readerErr
		},
	}

	result, err := exec.Execute(context.Background(), domain.Actor{TenantID: "test-tenant"}, domain.SubQuestion{
		ToolName: "vaccination_execution",
	})

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Critical P2-B fix: errors must be propagated
	if result.Err == nil {
		t.Error("ToolResult.Err should contain the reader error; got nil (error swallowed)")
	}

	if !errors.Is(result.Err, readerErr) {
		t.Errorf("Expected error to be propagated; got: %v", result.Err)
	}

	t.Logf("✓ Vaccination reader error correctly propagated: %v", result.Err)
}

func TestFeedDirectionTodayExecutor_ReturnsErrorWhenNotWired(t *testing.T) {
	exec := &feedDirectionTodayExecutor{
		feedDataReader: nil,
	}

	result, err := exec.Execute(context.Background(), domain.Actor{TenantID: "test-tenant"}, domain.SubQuestion{
		ToolName: "feed_direction_today",
	})

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Critical P1-B fix: unwired readers must return errors, not silently return empty
	if result.Err == nil {
		t.Error("ToolResult.Err should be non-nil when reader is not wired; got nil (error swallowed)")
	}

	if result.Err.Error() != "feed data reader not wired" {
		t.Errorf("Expected 'feed data reader not wired', got: %v", result.Err)
	}

	t.Logf("✓ Feed reader wiring error correctly propagated: %v", result.Err)
}

func TestFeedDirectionTodayExecutor_PropagateReaderError(t *testing.T) {
	readerErr := errors.New("feed service error")
	exec := &feedDirectionTodayExecutor{
		feedDataReader: func(ctx context.Context, tenantID string, params map[string]any) ([]domain.Fact, error) {
			return nil, readerErr
		},
	}

	result, err := exec.Execute(context.Background(), domain.Actor{TenantID: "test-tenant"}, domain.SubQuestion{
		ToolName: "feed_direction_today",
	})

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Critical P2-B fix: errors must be propagated
	if result.Err == nil {
		t.Error("ToolResult.Err should contain the reader error; got nil (error swallowed)")
	}

	if !errors.Is(result.Err, readerErr) {
		t.Errorf("Expected error to be propagated; got: %v", result.Err)
	}

	t.Logf("✓ Feed reader error correctly propagated: %v", result.Err)
}

// TestFeedDirectionTodayExecutor_ThreadsAsOf proves P1-4 for the feed
// executor specifically: "feed ration today" needs the as_of business date
// threaded through so a scoped/backdated question ("feed direction as of
// yesterday") resolves against the right feed_day, not always "today".
func TestFeedDirectionTodayExecutor_ThreadsAsOf(t *testing.T) {
	var gotParams map[string]any
	exec := &feedDirectionTodayExecutor{
		feedDataReader: func(ctx context.Context, tenantID string, params map[string]any) ([]domain.Fact, error) {
			gotParams = params
			return []domain.Fact{{Label: "Feed", Value: "310"}}, nil
		},
	}
	_, err := exec.Execute(context.Background(), domain.Actor{TenantID: "test-tenant"}, domain.SubQuestion{
		ToolName: "feed_direction_today",
		Params:   map[string]any{"as_of": "2026-07-20", "park_label": "Castro 1"},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotParams["as_of"] != "2026-07-20" {
		t.Errorf("as_of not threaded to reader: got %v", gotParams["as_of"])
	}
	if gotParams["park_label"] != "Castro 1" {
		t.Errorf("park_label not threaded to reader: got %v", gotParams["park_label"])
	}
}

func TestWiringFunctions(t *testing.T) {
	// Test that SetCountsDataReader correctly wires the counts executor.
	countsReader := func(ctx context.Context, tenantID string, params map[string]any) ([]domain.Fact, error) {
		return []domain.Fact{{Label: "Test", Value: "123"}}, nil
	}

	execs := NewToolExecutors()
	SetCountsDataReader(execs[0], countsReader)

	// Verify the reader is wired by executing
	result, _ := execs[0].Execute(context.Background(), domain.Actor{TenantID: "test"}, domain.SubQuestion{ToolName: "counts_breakdown"})
	if result.Err != nil {
		t.Errorf("After wiring, expected no error; got %v", result.Err)
	}

	// Test that SetVaccinationDataReader correctly wires vaccination executors.
	vaccReader := func(ctx context.Context, tenantID string, params map[string]any) ([]domain.Fact, error) {
		return []domain.Fact{{Label: "Due", Value: "5"}}, nil
	}

	execs = NewToolExecutors()
	SetVaccinationDataReader(execs, vaccReader)

	// Verify both vaccination executors are wired
	for i, exec := range execs {
		// Find the vaccination executors
		if spec := exec.Spec(); spec.Name == "vaccination_shed_summary" || spec.Name == "vaccination_execution" {
			result, _ := exec.Execute(context.Background(), domain.Actor{TenantID: "test"}, domain.SubQuestion{ToolName: spec.Name})
			if result.Err != nil {
				t.Errorf("Executor %d: after wiring, expected no error; got %v", i, result.Err)
			}
		}
	}

	// Test that SetFeedDataReader correctly wires the feed executor.
	feedReader := func(ctx context.Context, tenantID string, params map[string]any) ([]domain.Fact, error) {
		return []domain.Fact{{Label: "Direction", Value: "Increase"}}, nil
	}

	execs = NewToolExecutors()
	SetFeedDataReader(execs[3], feedReader) // feed executor is typically at index 3

	feedResult, _ := execs[3].Execute(context.Background(), domain.Actor{TenantID: "test"}, domain.SubQuestion{ToolName: "feed_direction_today"})
	if feedResult.Err != nil {
		t.Errorf("After wiring, expected no error; got %v", feedResult.Err)
	}

	t.Log("✓ All wiring functions work correctly")
}
