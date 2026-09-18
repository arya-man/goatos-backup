package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// BULK SHEETS (maintainer instruction 2026-09-18). Every register can be DOWNLOADED as a sheet
// and every importable one UPLOADED from the same shape, so the round trip is: download, fix in
// a spreadsheet, upload. The shape is the register's own columns, keyed by column KEY in the
// header row (stable, machine-matched; a label such as "Gender" is accepted too), plus:
//
//	id      first column. Blank = create a new row. Set = update THAT row (from a download).
//	status  last column on a download; ignored on upload (archive on screen, where the usage
//	        check runs).
//
// A ref column ("park_id") is written as the target's LABEL on download -- a person reads
// "Coimbatore", not a uuid -- and on upload accepts either the label (matched once, case-
// insensitively, among the active options) or the id itself. A label two options share is
// refused with the ids named, never guessed.

// Sheet column keys the importer owns beside the register's.
const (
	SheetColumnID         = "id"
	SheetColumnRowVersion = "row_version"
	SheetColumnStatus     = "status"
)

// Import row states.
const (
	ImportRowStaged   = "staged"
	ImportRowValid    = "valid"
	ImportRowApplying = "applying"
	ImportRowInvalid  = "invalid"
	ImportRowApplied  = "applied"
	ImportRowFailed   = "failed"
	ImportRowSkipped  = "skipped"
)

// Import job states.
const (
	ImportValidating = "validating"
	ImportPreviewed  = "previewed"
	ImportApplying   = "applying"
	ImportApplied    = "applied"
	ImportFailed     = "failed"
	ImportCancelled  = "cancelled"
)

// Sheet formats.
const (
	FormatCSV  = "csv"
	FormatXLSX = "xlsx"
)

// ImportJob is one uploaded sheet on its way through validate -> preview -> apply.
type ImportJob struct {
	ID            string `json:"id"`
	Register      string `json:"register"`
	FileName      string `json:"file_name"`
	Format        string `json:"format"`
	Status        string `json:"status"`
	TotalRows     int    `json:"total_rows"`
	ValidRows     int    `json:"valid_rows"`
	InvalidRows   int    `json:"invalid_rows"`
	AppliedRows   int    `json:"applied_rows"`
	FailedRows    int    `json:"failed_rows"`
	ProgressRowNo int    `json:"progress_row_no"`
	Error         string `json:"error,omitempty"`
	CreatedBy     string `json:"created_by,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	FinishedAt    string `json:"finished_at,omitempty"`
}

// ImportRow is one staged line of a sheet with what validation or apply said about it.
type ImportRow struct {
	RowNo    int            `json:"row_no"`
	Fields   map[string]any `json:"fields"`
	State    string         `json:"state"`
	Errors   []FieldError   `json:"errors"`
	ResultID string         `json:"result_id,omitempty"`
}

// SheetColumns is the column list a register's sheet carries, in order: id, the register's
// columns, status. Derived columns are included so a download reads complete; the importer
// drops them.
func SheetColumns(reg Register) []Column {
	out := make([]Column, 0, len(reg.Columns)+2)
	if !reg.ImportCreateOnly {
		out = append(out, Column{Key: SheetColumnID, Label: "Id", Type: TypeText, Hint: "Leave blank for a new row; keep the downloaded value to update that row."})
		out = append(out, Column{Key: SheetColumnRowVersion, Label: "Row version", Type: TypeNumber, Hint: "Keep the downloaded value so imports refuse stale updates."})
	}
	out = append(out, reg.Columns...)
	out = append(out, Column{Key: SheetColumnStatus, Label: "Status", Type: TypeText, Derived: true})
	return out
}

// MatchHeader maps a sheet's header row onto the register's sheet columns: each cell matches a
// column KEY or LABEL, case-insensitively, with spaces and dashes read as underscores. It
// returns the column key per header cell ("" for an unrecognised cell) and the list of
// unrecognised headers so the preview can name them. A required column missing from the
// header is reported too, because every row would fail the same way -- unless the sheet
// carries an id column, since a sheet of updates legitimately names only the columns it
// changes (a create row in it is then refused row by row).
func MatchHeader(reg Register, header []string) (keys []string, unknown []string, missing []string) {
	cols := SheetColumns(reg)
	byKey := make(map[string]string, len(cols)*2)
	for _, c := range cols {
		byKey[normalizeHeaderCell(c.Key)] = c.Key
		byKey[normalizeHeaderCell(c.Label)] = c.Key
	}
	keys = make([]string, len(header))
	seen := map[string]bool{}
	for i, h := range header {
		key := byKey[normalizeHeaderCell(h)]
		if key == "" || seen[key] {
			if strings.TrimSpace(h) != "" && key == "" {
				unknown = append(unknown, strings.TrimSpace(h))
			}
			continue
		}
		seen[key] = true
		keys[i] = key
	}
	if seen[SheetColumnID] && !reg.ImportCreateOnly {
		return keys, unknown, nil
	}
	for _, c := range reg.Columns {
		if c.Required && !c.Derived && !seen[c.Key] {
			missing = append(missing, c.Key)
		}
	}
	return keys, unknown, missing
}

func normalizeHeaderCell(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer(" ", "_", "-", "_", "/", "_", "(", "", ")", "", ".", "").Replace(s)
	return s
}

// SheetRow turns one data line into the fields map the importer stages, keyed by column key.
// Blank cells are dropped (a blank means "not sent": leave the value alone on an update, use
// the default on a create). Cells under an unrecognised header are ignored.
func SheetRow(keys []string, record []string) map[string]any {
	out := make(map[string]any, len(keys))
	for i, key := range keys {
		if key == "" || i >= len(record) {
			continue
		}
		v := strings.TrimSpace(record[i])
		if v == "" {
			continue
		}
		out[key] = v
	}
	return out
}

// IsBlankRecord reports a line with nothing in it (spreadsheets pad with these).
func IsBlankRecord(record []string) bool {
	for _, v := range record {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}

// SheetCell renders one field for a download: refs as their label, bools as yes/no, numbers
// plainly, everything else as text.
func SheetCell(c Column, row Row) string {
	switch c.Key {
	case SheetColumnID:
		return row.ID
	case SheetColumnRowVersion:
		return strconv.Itoa(row.RowVersion)
	case SheetColumnStatus:
		return row.Status
	}
	if c.Type == TypeRef {
		if label, ok := row.Labels[c.Key]; ok && label != "" {
			return label
		}
	}
	v, ok := row.Fields[c.Key]
	if !ok || v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "yes"
		}
		return "no"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	default:
		return fmt.Sprint(x)
	}
}

// RefIndex is the active options of one ref target, indexed for sheet resolution.
type RefIndex struct {
	byID    map[string]string
	byLabel map[string][]string
}

// NewRefIndex indexes options (id, label).
func NewRefIndex(ids, labels []string) RefIndex {
	idx := RefIndex{byID: make(map[string]string, len(ids)), byLabel: make(map[string][]string, len(ids))}
	for i := range ids {
		idx.byID[ids[i]] = labels[i]
		k := strings.ToLower(strings.TrimSpace(labels[i]))
		idx.byLabel[k] = append(idx.byLabel[k], ids[i])
	}
	return idx
}

// Resolve turns a sheet cell into the option id: the id itself, or a label carried by exactly
// one active option. ok is false with a message for an unknown or ambiguous value.
func (idx RefIndex) Resolve(cell string) (id string, msg string, ok bool) {
	cell = strings.TrimSpace(cell)
	if _, found := idx.byID[cell]; found {
		return cell, "", true
	}
	ids := idx.byLabel[strings.ToLower(cell)]
	switch len(ids) {
	case 1:
		return ids[0], "", true
	case 0:
		return "", "no active row is called " + strconv.Quote(cell) + ".", false
	default:
		return "", strconv.Quote(cell) + " names more than one row; use its id from a download (" + strings.Join(ids, ", ") + ").", false
	}
}
