// Package sopseed carries the SEEDED herd-operations SOP documents and the Task Type / Category
// registries as embedded JSON. It is the single source for three readers:
//
//   - migration 000308 (which must embed the same bytes verbatim -- pinned by
//     tasks/domain.TestMigrationEmbedsTheSeededDocuments);
//   - the golden test proving each seeded follow_up compiles to the legacy code template;
//   - the sop validator's fallback registry when a tenant has no registry rows yet.
//
// These files describe day-one behaviour. After migration the database rows are authoritative and
// the maintainer edits them on the web; do NOT "fix" a live SOP by editing this package.
package sopseed

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed *.json
var files embed.FS

// FollowUpDocuments maps sop_definitions.code -> the seeded follow_up JSON (raw bytes).
var FollowUpDocuments = map[string]string{
	"counts.birth":     "counts_birth.json",
	"counts.death":     "counts_death.json",
	"shifting":         "counts_shifting.json",
	"counts.reconcile": "counts_reconcile.json",
	// The first GENERAL SOP (maintainer decision 2026-09-18): farm-wide, started by hand, with an
	// answer-driven branch -- the shape phase 2 of the SOP studio adds.
	SOPCodeGateVisitorCheck: "general_gate_visitor_check.json",
	// The SALE (maintainer instruction 2026-09-19, docs/decisions/sales-sop.md): what happens
	// after a sale is recorded -- tagging, loading, the money -- with the designation that does
	// each step. Seeded by the sales-SOP migration; pinned by TestMigrationEmbedsTheSalesSeed.
	SOPCodeSalesDeal: "sales_deal.json",
}

// SOPCodeSalesDeal is the seeded sale SOP's code.
const SOPCodeSalesDeal = "sales.deal"

// TaskTypeFiles are the Task Type Registry seed files in the order their migrations shipped:
// task_types.json (000308) and task_types_sales.json (the sales-SOP migration). A registry row
// is never edited in an already-applied migration, so a later hook gets its own file.
var TaskTypeFiles = []string{"task_types.json", "task_types_sales.json"}

// SOPCodeGateVisitorCheck is the seeded general SOP's code.
const SOPCodeGateVisitorCheck = "general.gate_visitor_check"

// TaskType is one Task Type Registry row.
type TaskType struct {
	Key             string          `json:"key"`
	Name            string          `json:"name"`
	CategoryScope   []string        `json:"category_scope"`
	AnswerKind      string          `json:"answer_kind"`
	EngineHook      string          `json:"engine_hook"`
	Description     string          `json:"description"`
	ParameterSchema json.RawMessage `json:"parameter_schema"`
}

// Category is one Category Registry row.
type Category struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

// Raw returns the embedded bytes of one file.
func Raw(name string) ([]byte, error) { return files.ReadFile(name) }

// FollowUp returns the raw follow_up document for a SOP code.
func FollowUp(sopCode string) ([]byte, error) {
	name, ok := FollowUpDocuments[sopCode]
	if !ok {
		return nil, fmt.Errorf("sopseed: no seeded follow_up for %q", sopCode)
	}
	return files.ReadFile(name)
}

// TaskTypes decodes the seeded Task Type Registry: every TaskTypeFiles file, in order.
func TaskTypes() ([]TaskType, error) {
	var out []TaskType
	for _, name := range TaskTypeFiles {
		raw, err := files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		var rows []TaskType
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, fmt.Errorf("sopseed: %s: %w", name, err)
		}
		out = append(out, rows...)
	}
	return out, nil
}

// Categories decodes the seeded Category Registry.
func Categories() ([]Category, error) {
	raw, err := files.ReadFile("categories.json")
	if err != nil {
		return nil, err
	}
	var out []Category
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("sopseed: categories.json: %w", err)
	}
	return out, nil
}
