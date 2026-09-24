package diagnosis

import (
	"fmt"
	"strconv"
	"strings"
)

// THE REGISTER AS A SHEET: download a type's whole rulebook, edit it in Excel, upload it back.
//
// Maintainer instruction 2026-09-23: "if it's very hard to enter everything right, I will create
// a template, they will upload and submit". Forty questions and thirty illnesses typed one field
// at a time through a web form is how a new type stays half-authored for a month.
//
// THE SHEET CARRIES EVERY FIELD, and two tests keep it that way, because a lossy export becomes a
// lossy IMPORT the moment someone uploads it back -- a rule that quietly lost its gate would fire,
// or fail to fire, on animals nobody could explain.
//
// TestSheetRoundTripsEverySeededRegister encodes each shipped register and decodes it back, and the
// stored JSON must match. That proves the fields the farm ACTUALLY USES survive. It cannot prove
// anything about a field none of the four happens to set -- dropping `gate_excluded` from the
// encoder passes it, because no seeded rule excludes a gate -- so TestEverySheetFieldIsCarried
// asserts that every json field on Rule, Question, Option, Band, Clause, SeverityModifier,
// Correction and AuthoredRegister is named in this file. A field added to the engine tomorrow
// fails that test until it is carried here, which is the protection the round trip cannot give.
//
// ONE FLAT SHEET, NOT FIVE. A register is a tree -- questions own answers, illnesses own the signs
// that recognise them -- and a tree does not fit a grid. The shape here is one row per NODE with a
// `row` column saying what kind it is and a `parent` naming its owner, which is the shape a
// spreadsheet user already understands from any export they have seen. The alternative, a workbook
// of five linked sheets, moves the same problem into tab-switching and makes a CSV impossible.

// SheetColumns is the header, in order. It is the union of every row kind's fields; a given row
// fills the handful that apply to it and leaves the rest blank.
//
// The order is READING order, not struct order: what a row IS, who it belongs to, then what it
// says. An author scanning the first three columns can follow the tree without reading the rest.
var SheetColumns = []string{
	"row", "id", "parent",
	// questions
	"title", "kind", "section", "hint", "unit", "min", "max",
	"only_if_sex", "only_if_stage", "only_if_question", "only_if_in",
	// answers and bands
	"value", "label", "emits", "conflicts_with", "gt", "gte", "lt", "lte",
	// illnesses
	"applies_species", "applies_sex", "applies_status",
	"gate_required", "gate_excluded", "human_selected_only",
	"severity_base", "acute_actionable", "treatment_risk", "exit_type",
	"sop_ref", "containment", "explains_findings", "suppresses",
	"recheck_id_possible", "adjunct_when", "treats",
	// signs, severity modifiers, corrections, and the register header
	"tier", "findings", "residual", "finding", "severity",
	"when", "remove", "add", "note",
	"register_version", "applies_class", "non_specific", "vocabulary",
}

// listSep separates repeated values inside one cell.
//
// A SEMICOLON, not a comma: a cell holding `cmt:positive, udder:swollen_hard` in a CSV is a
// quoting problem waiting to happen, and every spreadsheet on the farm will re-quote it its own
// way. A semicolon survives a round trip through Excel, Numbers and a text editor unchanged.
const listSep = ";"

func joinList(v []string) string { return strings.Join(v, listSep) }

func splitList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, listSep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func boolCell(b bool) string {
	if b {
		return "yes"
	}
	return ""
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "yes", "y", "true", "1":
		return true
	}
	return false
}

func floatCell(f *float64) string {
	if f == nil {
		return ""
	}
	return strconv.FormatFloat(*f, 'g', -1, 64)
}

func parseFloatPtr(s string) (*float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, fmt.Errorf("%q is not a number: %w", s, err)
	}
	return &v, nil
}

type sheetRow map[string]string

func (r sheetRow) cells() []string {
	out := make([]string, len(SheetColumns))
	for i, col := range SheetColumns {
		out[i] = r[col]
	}
	return out
}

// EncodeSheet turns a register into rows, header first.
//
// The error is part of the signature rather than in use: every field is carried today (see the
// header), so there is nothing to refuse. It stays so that a future field the sheet genuinely
// cannot express -- a nested shape, say -- has somewhere to fail loudly instead of being dropped.
func EncodeSheet(doc AuthoredRegister) ([][]string, error) {
	rows := [][]string{append([]string{}, SheetColumns...)}
	add := func(r sheetRow) { rows = append(rows, r.cells()) }

	add(sheetRow{
		"row":              "register",
		"register_version": doc.RegisterVersion,
		"applies_class":    joinList(doc.AppliesClass),
		"non_specific":     joinList(doc.NonSpecific),
		"vocabulary":       joinList(doc.Vocabulary),
	})

	// The pages, before the questions that sit on them -- the sheet is read top-down, and a
	// question naming a page defined below it reads as a forward reference.
	for _, sec := range doc.Sections {
		add(sheetRow{"row": "page", "id": sec.ID, "title": sec.Title, "hint": sec.Hint})
	}

	for _, q := range doc.Questions {
		r := sheetRow{
			"row": "question", "id": q.ID, "title": q.Title, "kind": q.Kind,
			"section": q.Section, "hint": q.Hint, "unit": q.Unit,
			"min": floatCell(q.Min), "max": floatCell(q.Max),
			"only_if_sex": q.OnlyIfSex, "only_if_stage": joinList(q.OnlyIfStage),
		}
		if q.OnlyIf != nil {
			r["only_if_question"] = q.OnlyIf.QuestionID
			r["only_if_in"] = joinList(q.OnlyIf.In)
		}
		add(r)
		for _, o := range q.Options {
			add(sheetRow{
				"row": "answer", "parent": q.ID, "value": o.Value, "label": o.Label,
				"emits": joinList(o.Emits), "conflicts_with": joinList(o.ConflictsWith),
			})
		}
		for _, b := range q.Bands {
			add(sheetRow{
				"row": "band", "parent": q.ID,
				"gt": floatCell(b.Gt), "gte": floatCell(b.Gte),
				"lt": floatCell(b.Lt), "lte": floatCell(b.Lte),
				"emits": joinList(b.Emits),
			})
		}
	}

	for _, c := range doc.Corrections {
		add(sheetRow{
			"row": "correction", "id": c.ID, "when": joinList(c.When),
			"remove": joinList(c.Remove), "add": joinList(c.Add), "note": c.Note,
		})
	}

	for _, rule := range doc.Rules {
		add(sheetRow{
			"row": "illness", "id": rule.ID, "kind": string(rule.Kind),
			"applies_species":     joinList(rule.AppliesSpecies),
			"applies_sex":         joinList(rule.AppliesSex),
			"applies_status":      joinList(rule.AppliesStatus),
			"gate_required":       joinList(rule.GateRequired),
			"gate_excluded":       joinList(rule.GateExcluded),
			"human_selected_only": boolCell(rule.HumanSelectedOnly),
			"severity_base":       strconv.Itoa(rule.SeverityBase),
			"acute_actionable":    boolCell(rule.AcuteActionable),
			"treatment_risk":      rule.TreatmentRisk,
			"exit_type":           rule.ExitType,
			"sop_ref":             rule.SOPRef,
			"containment":         rule.Containment,
			"explains_findings":   joinList(rule.ExplainsFindings),
			"suppresses":          joinList(rule.Suppresses),
			"recheck_id_possible": rule.RecheckIDPossible,
			"adjunct_when":        joinList(rule.AdjunctWhen),
			"treats":              rule.Treats,
		})
		// Tiers are written in CONFIDENCE order rather than map order, because the sheet is read
		// by a person and "confirmed by" belongs above "possible when". Ranging a map would also
		// make the export non-deterministic, which would turn every re-download into a diff.
		for _, tier := range []struct {
			name    string
			clauses []Clause
		}{
			{"pathognomonic", rule.Pathognomonic},
			{"probable", rule.Probable},
			{"possible", rule.Possible},
		} {
			for _, cl := range tier.clauses {
				add(sheetRow{
					"row": "sign", "parent": rule.ID, "tier": tier.name,
					"findings": joinList(cl.Findings), "residual": boolCell(cl.Residual),
				})
			}
		}
		for _, m := range rule.SeverityModifiers {
			add(sheetRow{
				"row": "severity", "parent": rule.ID,
				"finding": m.Finding, "severity": strconv.Itoa(m.Severity),
			})
		}
	}

	return rows, nil
}

// DecodeSheet rebuilds a register from rows, header first.
//
// Every problem is collected and returned TOGETHER, each naming its sheet row, because a person
// who has just filled in three hundred rows and is told about one mistake at a time will be at it
// all afternoon. The row numbers are 1-based and count the header, so they match what the
// spreadsheet's own row numbers say.
func DecodeSheet(rows [][]string) (*AuthoredRegister, []string) {
	var problems []string
	if len(rows) == 0 {
		return nil, []string{"the sheet is empty"}
	}

	index := map[string]int{}
	for i, col := range rows[0] {
		index[strings.ToLower(strings.TrimSpace(col))] = i
	}
	for _, required := range []string{"row", "id", "parent"} {
		if _, ok := index[required]; !ok {
			return nil, []string{fmt.Sprintf("the sheet has no %q column -- start from the template", required)}
		}
	}

	cell := func(r []string, col string) string {
		i, ok := index[col]
		if !ok || i >= len(r) {
			return ""
		}
		return strings.TrimSpace(r[i])
	}

	doc := &AuthoredRegister{}
	questionAt := map[string]int{}
	ruleAt := map[string]int{}

	for n, r := range rows[1:] {
		line := n + 2 // 1-based, and the header is row 1
		kind := strings.ToLower(cell(r, "row"))
		if kind == "" {
			continue // a blank separator row is how people space a sheet out
		}
		num := func(col string) *float64 {
			v, err := parseFloatPtr(cell(r, col))
			if err != nil {
				problems = append(problems, fmt.Sprintf("row %d: %s %v", line, col, err))
			}
			return v
		}
		intOf := func(col string) int {
			s := cell(r, col)
			if s == "" {
				return 0
			}
			v, err := strconv.Atoi(s)
			if err != nil {
				problems = append(problems, fmt.Sprintf("row %d: %s %q is not a whole number", line, col, s))
				return 0
			}
			return v
		}

		switch kind {
		case "register":
			doc.RegisterVersion = cell(r, "register_version")
			doc.AppliesClass = splitList(cell(r, "applies_class"))
			doc.NonSpecific = splitList(cell(r, "non_specific"))
			doc.Vocabulary = splitList(cell(r, "vocabulary"))

		case "page":
			id := cell(r, "id")
			if id == "" {
				problems = append(problems, fmt.Sprintf("row %d: a page needs an id", line))
				continue
			}
			doc.Sections = append(doc.Sections, Section{
				ID: id, Title: cell(r, "title"), Hint: cell(r, "hint"),
			})

		case "question":
			id := cell(r, "id")
			if id == "" {
				problems = append(problems, fmt.Sprintf("row %d: a question needs an id", line))
				continue
			}
			q := Question{
				ID: id, Kind: cell(r, "kind"), Title: cell(r, "title"),
				Hint: cell(r, "hint"), Section: cell(r, "section"), Unit: cell(r, "unit"),
				Min: num("min"), Max: num("max"),
				OnlyIfSex: cell(r, "only_if_sex"), OnlyIfStage: splitList(cell(r, "only_if_stage")),
			}
			if oq := cell(r, "only_if_question"); oq != "" {
				q.OnlyIf = &Condition{QuestionID: oq, In: splitList(cell(r, "only_if_in"))}
			}
			questionAt[id] = len(doc.Questions)
			doc.Questions = append(doc.Questions, q)

		case "answer", "band":
			parent := cell(r, "parent")
			at, ok := questionAt[parent]
			if !ok {
				problems = append(problems, fmt.Sprintf(
					"row %d: %s belongs to question %q, which is not in this sheet above it", line, kind, parent))
				continue
			}
			if kind == "answer" {
				doc.Questions[at].Options = append(doc.Questions[at].Options, Option{
					Value: cell(r, "value"), Label: cell(r, "label"),
					Emits:         splitList(cell(r, "emits")),
					ConflictsWith: splitList(cell(r, "conflicts_with")),
				})
			} else {
				doc.Questions[at].Bands = append(doc.Questions[at].Bands, Band{
					Gt: num("gt"), Gte: num("gte"), Lt: num("lt"), Lte: num("lte"),
					Emits: splitList(cell(r, "emits")),
				})
			}

		case "correction":
			doc.Corrections = append(doc.Corrections, Correction{
				ID: cell(r, "id"), When: splitList(cell(r, "when")),
				Remove: splitList(cell(r, "remove")), Add: splitList(cell(r, "add")),
				Note: cell(r, "note"),
			})

		case "illness":
			id := cell(r, "id")
			if id == "" {
				problems = append(problems, fmt.Sprintf("row %d: an illness needs an id", line))
				continue
			}
			rule := Rule{
				ID: id, Kind: Kind(cell(r, "kind")),
				AppliesSpecies:    splitList(cell(r, "applies_species")),
				AppliesSex:        splitList(cell(r, "applies_sex")),
				AppliesStatus:     splitList(cell(r, "applies_status")),
				GateRequired:      splitList(cell(r, "gate_required")),
				GateExcluded:      splitList(cell(r, "gate_excluded")),
				HumanSelectedOnly: parseBool(cell(r, "human_selected_only")),
				SeverityBase:      intOf("severity_base"),
				AcuteActionable:   parseBool(cell(r, "acute_actionable")),
				TreatmentRisk:     cell(r, "treatment_risk"),
				ExitType:          cell(r, "exit_type"),
				SOPRef:            cell(r, "sop_ref"),
				Containment:       cell(r, "containment"),
				ExplainsFindings:  splitList(cell(r, "explains_findings")),
				Suppresses:        splitList(cell(r, "suppresses")),
				RecheckIDPossible: cell(r, "recheck_id_possible"),
				AdjunctWhen:       splitList(cell(r, "adjunct_when")),
				Treats:            cell(r, "treats"),
			}
			ruleAt[id] = len(doc.Rules)
			doc.Rules = append(doc.Rules, rule)

		case "sign", "severity":
			parent := cell(r, "parent")
			at, ok := ruleAt[parent]
			if !ok {
				problems = append(problems, fmt.Sprintf(
					"row %d: %s belongs to illness %q, which is not in this sheet above it", line, kind, parent))
				continue
			}
			if kind == "severity" {
				doc.Rules[at].SeverityModifiers = append(doc.Rules[at].SeverityModifiers,
					SeverityModifier{Finding: cell(r, "finding"), Severity: intOf("severity")})
				continue
			}
			cl := Clause{Findings: splitList(cell(r, "findings")), Residual: parseBool(cell(r, "residual"))}
			switch strings.ToLower(cell(r, "tier")) {
			case "pathognomonic":
				doc.Rules[at].Pathognomonic = append(doc.Rules[at].Pathognomonic, cl)
			case "probable":
				doc.Rules[at].Probable = append(doc.Rules[at].Probable, cl)
			case "possible":
				doc.Rules[at].Possible = append(doc.Rules[at].Possible, cl)
			default:
				problems = append(problems, fmt.Sprintf(
					"row %d: %q is not one of pathognomonic, probable or possible", line, cell(r, "tier")))
			}

		default:
			// A row kind nobody reads is an accept-and-discard: it looks authored and changes
			// nothing. On a clinical table that is the worst failure mode, so it is named.
			problems = append(problems, fmt.Sprintf("row %d: %q is not a kind of row this sheet has", line, kind))
		}
	}

	return doc, problems
}

// SheetExampleRows is one worked row of each kind, for the blank template.
//
// The examples matter more than the header does. Forty blank columns do not tell a person that an
// answer belongs UNDER its question, that `parent` is how it says so, or that a semicolon
// separates findings inside one cell. One filled row of each kind shows the whole shape at a
// glance.
//
// Each is marked `#` in the `row` column, which DecodeSheet reports as a kind it does not know.
// That is deliberate: a template uploaded unchanged is refused with a sentence naming the example
// rows, rather than quietly importing a register made of examples.
func SheetExampleRows() [][]string {
	at := func(col string) int {
		for i, c := range SheetColumns {
			if c == col {
				return i
			}
		}
		return 0
	}
	row := func(pairs map[string]string) []string {
		r := make([]string, len(SheetColumns))
		for col, v := range pairs {
			r[at(col)] = v
		}
		return r
	}
	return [][]string{
		row(map[string]string{"row": "# register", "register_version": "my-type-1",
			"applies_class": "my_type", "non_specific": "eating:not_eating;activity:weak"}),
		row(map[string]string{"row": "# page", "id": "chest", "title": "Chest",
			"hint": "What the animal sounds like"}),
		row(map[string]string{"row": "# question", "id": "breathing", "title": "Breathing",
			"kind": "choice", "section": "chest"}),
		row(map[string]string{"row": "# answer", "parent": "breathing", "value": "cough",
			"label": "Coughing", "emits": "breathing:cough"}),
		row(map[string]string{"row": "# band", "parent": "temperature", "gte": "104",
			"emits": "HIGH_FEVER"}),
		row(map[string]string{"row": "# illness", "id": "PNEUMONIA", "kind": "problem",
			"applies_species": "goat;sheep", "applies_sex": "M;F", "applies_status": "any",
			"severity_base": "4", "treats": "pneumonia"}),
		row(map[string]string{"row": "# sign", "parent": "PNEUMONIA", "tier": "probable",
			"findings": "breathing:cough;FEVER"}),
	}
}
