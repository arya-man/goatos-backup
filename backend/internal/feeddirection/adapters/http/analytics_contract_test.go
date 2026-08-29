package http

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The analytics DTOs are checked field-by-field against the OpenAPI schemas they claim to
// implement, because a contract that declares a field the backend never emits is invisible
// everywhere else: Go compiles, the generated client types compile, admin-web typechecks against
// the LIE, and only a consumer that validates the response finds out.
//
// That is not hypothetical. `FeedAnalyticsStockFarmItem` required `expected_stock_kg`,
// `stock_variance_kg` and `stock_check_status` for two days after the stock-check concept was
// deliberately removed from the backend and the UI (92bffd3d2, "Show feed stock days left"), which
// dropped the Go fields and left the schema behind. Nothing caught it.
//
// The check runs both ways on purpose. Missing-in-JSON catches that drift; missing-in-schema
// catches its mirror -- a field shipped on the wire that no client can see because it was never
// declared.
func TestAnalyticsDTOsMatchTheirOpenAPISchemas(t *testing.T) {
	for _, tc := range []struct {
		schema string
		dto    any
	}{
		{"FeedAnalyticsStockResponse", stockAnalyticsDTO{}},
		{"FeedAnalyticsStockFarmItem", stockFarmItemDTO{}},
		{"FeedAnalyticsStockForecastItem", stockForecastItemDTO{}},
		{"FeedAnalyticsExecutionResponse", executionAnalyticsDTO{}},
		{"FeedAnalyticsConsumptionTrendDay", feedConsumptionTrendDayDTO{}},
		{"FeedAnalyticsPackingVarianceRow", packingVarianceRowDTO{}},
	} {
		t.Run(tc.schema, func(t *testing.T) {
			required, properties := openAPISchemaFields(t, tc.schema)
			emitted := emittedJSONKeys(t, tc.dto)
			for _, field := range required {
				if !emitted[field] {
					t.Errorf("%s declares %q required, but %T never emits it",
						tc.schema, field, tc.dto)
				}
			}
			for field := range emitted {
				if !properties[field] {
					t.Errorf("%T emits %q, which %s does not declare -- no client can see it",
						tc.dto, field, tc.schema)
				}
			}
		})
	}
}

// emittedJSONKeys marshals a DTO with EVERY field set to a non-zero value, so a field carrying
// `omitempty` is still present. A zero-valued struct would report an omitempty field as missing
// and turn this check into noise.
func emittedJSONKeys(t *testing.T, dto any) map[string]bool {
	t.Helper()
	value := reflect.New(reflect.TypeOf(dto)).Elem()
	fillNonZero(value)
	raw, err := json.Marshal(value.Interface())
	if err != nil {
		t.Fatalf("marshal %T: %v", dto, err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal %T: %v", dto, err)
	}
	keys := map[string]bool{}
	for key := range decoded {
		keys[key] = true
	}
	return keys
}

func fillNonZero(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Int, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Slice:
		// One element, itself fully populated, so a nested schema is exercised too.
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fillNonZero(s.Index(0))
		v.Set(s)
	case reflect.Ptr:
		p := reflect.New(v.Type().Elem())
		fillNonZero(p.Elem())
		v.Set(p)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanSet() {
				fillNonZero(v.Field(i))
			}
		}
	}
}

// openAPISchemaFields reads one `components.schemas` entry's `required` list and `properties`
// keys. The spec is scanned as text rather than parsed: the backend carries no YAML dependency,
// and the sibling contract check in cmd/mcp reads it the same way. Both `required:` spellings the
// file uses are handled -- a block list of `- name` lines and an inline `[a, b, c]` that may wrap
// across lines.
func openAPISchemaFields(t *testing.T, schema string) ([]string, map[string]bool) {
	t.Helper()
	raw, err := os.ReadFile("../../../../../contracts/openapi/app-api.yaml")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, line := range lines {
		if line == "    "+schema+":" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("schema %s not found in contracts/openapi/app-api.yaml", schema)
	}
	var required []string
	properties := map[string]bool{}
	section := ""
	inlineRequired := ""
	for _, line := range lines[start:] {
		// A line at the schema's own indent ends this schema.
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "      ") {
			break
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "required:":
			section = "required"
			continue
		case strings.HasPrefix(trimmed, "required:"):
			section = "required-inline"
			inlineRequired = strings.TrimPrefix(trimmed, "required:")
			if strings.Contains(inlineRequired, "]") {
				required = append(required, parseInlineList(inlineRequired)...)
				section = ""
			}
			continue
		case trimmed == "properties:":
			section = "properties"
			continue
		}
		switch section {
		case "required":
			if !strings.HasPrefix(trimmed, "- ") {
				section = ""
				continue
			}
			required = append(required, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
		case "required-inline":
			inlineRequired += " " + trimmed
			if strings.Contains(trimmed, "]") {
				required = append(required, parseInlineList(inlineRequired)...)
				section = ""
			}
		case "properties":
			// Property names sit at exactly 8 spaces; anything deeper is that property's own body.
			if !strings.HasPrefix(line, "        ") || strings.HasPrefix(line, "         ") {
				continue
			}
			name := strings.TrimSuffix(trimmed, ":")
			if name != trimmed {
				properties[name] = true
			}
		}
	}
	if len(properties) == 0 {
		t.Fatalf("schema %s parsed with no properties -- the scanner has drifted from the spec's shape", schema)
	}
	return required, properties
}

func parseInlineList(raw string) []string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(strings.TrimSpace(raw), "]")
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if name := strings.TrimSpace(part); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func TestCompletionDayForDTOFallsBackWhenSectionSkipped(t *testing.T) {
	fallback := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	if got := completionDayForDTO("", fallback); got != "2026-08-25" {
		t.Fatalf("completionDayForDTO(empty, fallback) = %q, want fallback date", got)
	}
	if got := completionDayForDTO("2026-08-24", fallback); got != "2026-08-24" {
		t.Fatalf("completionDayForDTO(result, fallback) = %q, want repository result", got)
	}
}
