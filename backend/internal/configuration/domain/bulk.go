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
	// A workbook tab: the bundle it belongs to, its place in the order, and its worksheet name.
	BundleID    string `json:"bundle_id,omitempty"`
	BundleOrder int    `json:"bundle_order,omitempty"`
	SheetName   string `json:"sheet_name,omitempty"`
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

// ONBOARDING WORKBOOK (maintainer instruction 2026-09-19). One Excel file carries one tab per
// register and is imported as a BUNDLE of ordinary import jobs, one per matched tab, worked in
// WorkbookOrder so a tab may name rows an earlier tab creates. A cross-tab reference is stored
// on the validated row as a token naming the sibling job and row (BundleRowToken) and resolves
// to that row's result id when its tab has been applied.

// Bundle (workbook) states; the same words as a job's.
const (
	ImportJobQueued = "queued" // a workbook tab waiting for the tabs before it
)

// ImportBundle is one uploaded workbook: its tabs as jobs, in order.
type ImportBundle struct {
	ID            string      `json:"id"`
	FileName      string      `json:"file_name"`
	Status        string      `json:"status"`
	UnknownSheets []string    `json:"unknown_sheets"`
	Error         string      `json:"error,omitempty"`
	CreatedBy     string      `json:"created_by,omitempty"`
	CreatedAt     string      `json:"created_at"`
	UpdatedAt     string      `json:"updated_at"`
	FinishedAt    string      `json:"finished_at,omitempty"`
	Jobs          []ImportJob `json:"jobs"`
}

// Totals sums the tabs' counters (the screen's headline figures).
func (b ImportBundle) Totals() (total, valid, invalid, applied, failed int) {
	for _, j := range b.Jobs {
		total += j.TotalRows
		valid += j.ValidRows
		invalid += j.InvalidRows
		applied += j.AppliedRows
		failed += j.FailedRows
	}
	return
}

// WorkbookOrder is the order tabs are validated and applied in: every register before the
// registers that point at it. It is asserted against the ref columns by a test, so a register
// added with a new ref cannot be listed ahead of its target by accident. Registers not named
// here take no tab: the read-only ones (Roles, Breeds, Feed items, Status definitions --
// authored elsewhere) and the hidden register OF reference lists; a tenant's own reference lists (`ref:*`) follow the
// static registers, each independent of the others.
var WorkbookOrder = []string{
	RegSpecies, RegSexes, RegStages, // a pen names the stage it is kept for
	RegParks, RegPens, RegPartitions,
	RegCategories, RegItems,
	RegSOPCategories, RegTaskTypes,
	RegAnimals,
}

// WorkbookDependencies names the registers a tab's rows may reference, beyond its own ref
// columns: the animals sheet names park and pen by text and species, gender and stage by code.
var WorkbookDependencies = map[string][]string{
	RegAnimals: {RegParks, RegPens, RegSpecies, RegSexes, RegStages},
}

// WorkbookDependenciesOf is every register a tab of reg may reference: its ref columns' targets
// plus WorkbookDependencies, plus itself when a column points back at the same register (a
// category under a category on the same sheet).
func WorkbookDependenciesOf(reg Register) []string {
	seen := map[string]bool{}
	var out []string
	add := func(key string) {
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, key)
	}
	for _, c := range reg.Columns {
		if c.Type == TypeRef {
			add(c.Ref)
		}
	}
	for _, key := range WorkbookDependencies[reg.Key] {
		add(key)
	}
	return out
}

// MatchSheetName resolves a worksheet's name to a register: the register key, label or singular
// noun, compared the way headers are (case, spaces and punctuation ignored). "" when no register
// matches. A dynamic reference list matches by its own name or its key.
func MatchSheetName(name string, registers []Register) string {
	want := normalizeHeaderCell(name)
	if want == "" {
		return ""
	}
	for _, reg := range registers {
		if !reg.Importable {
			continue
		}
		candidates := []string{reg.Key, reg.Label, reg.One}
		if reg.ListKey != "" {
			candidates = append(candidates, reg.ListKey)
		}
		for _, c := range candidates {
			if normalizeHeaderCell(c) == want {
				return reg.Key
			}
		}
	}
	return ""
}

// NameScope names the ref column within which a register's name (or label) is unique, matching
// the store's own rule: a pen name is unique per park, a partition label per pen, a category
// name per parent. Registers not listed are unique across the tenant. The in-sheet duplicate
// check keys on this, so an onboarding sheet may carry "Castro" for both parks.
var NameScope = map[string]string{
	RegPens:       "park_id",
	RegPartitions: "pen_id",
	RegCategories: "parent_id",
}

// BundleRowTokenPrefix marks a validated ref that names a row of a sibling tab rather than a
// stored row; the job id inside it is a uuid, so a token can never collide with a real id.
const BundleRowTokenPrefix = "bundle-row:"

// BundleRowToken names row rowNo of the sibling job.
func BundleRowToken(jobID string, rowNo int) string {
	return BundleRowTokenPrefix + jobID + ":" + strconv.Itoa(rowNo)
}

// ParseBundleRowToken reads a token back; ok is false for anything else.
func ParseBundleRowToken(s string) (jobID string, rowNo int, ok bool) {
	if !strings.HasPrefix(s, BundleRowTokenPrefix) {
		return "", 0, false
	}
	rest := s[len(BundleRowTokenPrefix):]
	i := strings.LastIndexByte(rest, ':')
	if i <= 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(rest[i+1:])
	if err != nil || n < 1 {
		return "", 0, false
	}
	return rest[:i], n, true
}

// Add indexes one more option. Two options under one label stay ambiguous, as NewRefIndex
// leaves them.
func (idx RefIndex) Add(id, label string) {
	k := strings.ToLower(strings.TrimSpace(label))
	if k == "" {
		return
	}
	idx.byID[id] = label
	idx.byLabel[k] = append(idx.byLabel[k], id)
}

// AddPending indexes a sibling tab's would-be row under its label or code. A label the index
// already resolves is left alone: the stored row wins, and the sheet's own row will be refused
// as a duplicate when it is written.
func (idx RefIndex) AddPending(id, label string) {
	k := strings.ToLower(strings.TrimSpace(label))
	if k == "" || len(idx.byLabel[k]) > 0 {
		return
	}
	idx.byID[id] = label
	idx.byLabel[k] = append(idx.byLabel[k], id)
}

// ScopedRefIndex resolves a ref whose target belongs to a parent (a pen to a park) within the
// parent the row itself names, so "Castro" on a partitions row for park CBE is the CBE pen and
// never ambiguous with the CPT one. Options with no parent, and rows that name none, resolve
// through the flat index.
type ScopedRefIndex struct {
	flat     RefIndex
	byParent map[string]RefIndex
}

// NewScopedRefIndex builds the index from options (id, label, parent id; parent "" = unscoped).
func NewScopedRefIndex(ids, labels, parents []string) ScopedRefIndex {
	idx := ScopedRefIndex{flat: NewRefIndex(nil, nil), byParent: map[string]RefIndex{}}
	for i := range ids {
		idx.Add(ids[i], labels[i], parents[i])
	}
	return idx
}

// Flat is the unscoped index (every option, whatever its parent).
func (idx ScopedRefIndex) Flat() RefIndex { return idx.flat }

// Add indexes one option under its parent (and in the flat index).
func (idx ScopedRefIndex) Add(id, label, parent string) {
	idx.flat.Add(id, label)
	if parent != "" {
		idx.scope(parent).Add(id, label)
	}
}

// AddPending indexes a sibling tab's would-be row; a label already resolving under the same
// parent is left to the stored row (see RefIndex.AddPending).
func (idx ScopedRefIndex) AddPending(id, label, parent string) {
	if parent == "" {
		idx.flat.AddPending(id, label)
		return
	}
	scoped := idx.scope(parent)
	before := len(scoped.byLabel[strings.ToLower(strings.TrimSpace(label))])
	scoped.AddPending(id, label)
	if len(scoped.byLabel[strings.ToLower(strings.TrimSpace(label))]) > before {
		// Reachable by id from anywhere; by label only within its parent, since the same pen
		// name in another park is a different pen.
		idx.flat.byID[id] = label
	}
}

func (idx ScopedRefIndex) scope(parent string) RefIndex {
	scoped, ok := idx.byParent[parent]
	if !ok {
		scoped = NewRefIndex(nil, nil)
		idx.byParent[parent] = scoped
	}
	return scoped
}

// Resolve turns a cell into the option id within parent when one is named; an id is accepted
// from anywhere.
func (idx ScopedRefIndex) Resolve(parent, cell string) (id string, msg string, ok bool) {
	cell = strings.TrimSpace(cell)
	if _, found := idx.flat.byID[cell]; found {
		return cell, "", true
	}
	if parent == "" {
		return idx.flat.Resolve(cell)
	}
	scoped, has := idx.byParent[parent]
	if !has {
		return "", "no active row is called " + strconv.Quote(cell) + " under the named parent.", false
	}
	return scoped.Resolve(cell)
}

// ParentColumn names the ref column of reg that scopes a ref to target: the one column both
// registers carry that points at the same third register (partitions.park_id scopes
// partitions.pen_id because pens.park_id points at parks too). "" when there is none.
func ParentColumn(reg, target Register, refKey string) string {
	for _, tc := range target.Columns {
		if tc.Type != TypeRef || tc.Key == refKey {
			continue
		}
		for _, rc := range reg.Columns {
			if rc.Type == TypeRef && rc.Key == tc.Key && rc.Ref == tc.Ref && rc.Key != refKey {
				return rc.Key
			}
		}
	}
	return ""
}
