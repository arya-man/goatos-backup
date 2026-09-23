package diagnosis

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// treatsFor is nil here on purpose: SeedAuthored takes the sop_ref -> disease key resolver as a
// PARAMETER precisely so this package stays pure, and importing health/domain to get the real one
// would be an import cycle. What this test proves is that the SHEET loses nothing; whether treats
// resolves to the right disease is domain's own test (authored_parity_test.go does it the same
// way, with the same nil).

// THE PROOF THAT THE SHEET LOSES NOTHING.
//
// A lossy export becomes a lossy IMPORT the moment someone uploads it back, and a rule that
// quietly lost its gate, its suppression list or one clause of its recognition would fire -- or
// fail to fire -- on animals nobody could explain. So every register this repo ships is encoded
// to rows and decoded back, and the result must equal the original field for field.
//
// It compares the JSON rather than the structs because the document is what is STORED: equality
// of the persisted form is the property that matters, and it reports a readable diff.
func TestSheetRoundTripsEverySeededRegister(t *testing.T) {
	for _, class := range Classes {
		t.Run(class, func(t *testing.T) {
			original, err := SeedAuthored(class, nil)
			if err != nil {
				t.Fatalf("seed %s: %v", class, err)
			}

			rows, err := EncodeSheet(*original)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			back, problems := DecodeSheet(rows)
			if len(problems) > 0 {
				t.Fatalf("decoding our own export reported problems: %v", problems)
			}

			want, _ := json.Marshal(original)
			got, _ := json.Marshal(back)
			if string(want) != string(got) {
				t.Errorf("the sheet lost or changed something.\nwant %s\n got %s", want, got)
			}
		})
	}
}

// A sheet a person edited is not a sheet we wrote: blank spacer rows are how people separate
// sections, and they must not become problems.
func TestBlankRowsAreNotProblems(t *testing.T) {
	doc, err := SeedAuthored(ClassAdult, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := EncodeSheet(*doc)
	if err != nil {
		t.Fatal(err)
	}
	spaced := [][]string{rows[0]}
	for _, r := range rows[1:] {
		spaced = append(spaced, r, make([]string, len(rows[0])))
	}
	if _, problems := DecodeSheet(spaced); len(problems) > 0 {
		t.Errorf("blank rows were reported as problems: %v", problems)
	}
}

// An answer whose question is not above it, and a row kind nobody reads, are both named rather
// than dropped. A row that parses to nothing looks authored and changes nothing -- the
// accept-and-discard this codebase refuses everywhere else.
func TestOrphansAndUnknownRowsAreNamedNotDropped(t *testing.T) {
	header := append([]string{}, SheetColumns...)
	col := func(name string) int {
		for i, c := range header {
			if c == name {
				return i
			}
		}
		t.Fatalf("no column %q", name)
		return -1
	}
	mk := func(kind, parent string) []string {
		r := make([]string, len(header))
		r[col("row")] = kind
		r[col("parent")] = parent
		return r
	}

	_, problems := DecodeSheet([][]string{header, mk("answer", "nosuch"), mk("wobble", "")})
	if len(problems) != 2 {
		t.Fatalf("want both problems named, got %v", problems)
	}
	if problems[0] == "" || problems[1] == "" {
		t.Errorf("problems must carry a sentence: %v", problems)
	}
}

// A sheet that is not the template at all fails on the first thing a person can act on, rather
// than reporting three hundred orphan rows.
func TestASheetWithoutTheTemplateColumnsIsRefusedPlainly(t *testing.T) {
	_, problems := DecodeSheet([][]string{{"animal", "disease", "notes"}})
	if len(problems) != 1 {
		t.Fatalf("want one plain refusal, got %v", problems)
	}
}

// EVERY FIELD IS CARRIED, including the ones no seeded register happens to set.
//
// The round trip above can only prove what the four shipped registers USE: deleting
// `gate_excluded` from the encoder passes it, because no seeded rule excludes a gate. This reads
// the engine's own structs and insists each json field is named in sheet.go, so a field added
// tomorrow fails here until the sheet carries it -- rather than being silently dropped the first
// time someone exports, edits and uploads a register that uses it.
func TestEverySheetFieldIsCarried(t *testing.T) {
	sheetSrc := readSource(t, "sheet.go")

	// The container fields are carried as their own ROW KINDS rather than as cells, which is what
	// makes the tree fit a grid at all. They are listed by name so this exemption cannot quietly
	// grow to cover a leaf field somebody forgot.
	carriedAsRows := map[string]bool{
		"options": true, "bands": true, "questions": true, "corrections": true, "rules": true,
		"sections":           true,                                     // its own `page` rows, one per page, written before the questions
		"severity_modifiers": true,                                     // its own `severity` rows, one per modifier
		"pathognomonic":      true, "probable": true, "possible": true, // its own `sign` rows, by tier
		"only_if": true, // flattened into only_if_question / only_if_in
	}

	for _, spec := range []struct{ file, typeName string }{
		{"register.go", "Rule"},
		{"register.go", "Clause"},
		{"register.go", "SeverityModifier"},
		{"authored.go", "Question"},
		{"authored.go", "Option"},
		{"authored.go", "Band"},
		{"authored.go", "Correction"},
		{"authored.go", "Section"},
		{"authored.go", "AuthoredRegister"},
		{"authored.go", "Condition"},
	} {
		for _, field := range jsonFieldsOf(t, spec.file, spec.typeName) {
			if carriedAsRows[field] {
				continue
			}
			if field == "question_id" || field == "in" {
				continue // Condition, flattened above
			}
			if !strings.Contains(sheetSrc, `"`+field+`"`) {
				t.Errorf("%s.%s is not carried by the sheet -- add it, or the first export that "+
					"uses it loses it", spec.typeName, field)
			}
		}
	}
}

func readSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func jsonFieldsOf(t *testing.T, file, typeName string) []string {
	t.Helper()
	src := readSource(t, file)
	start := strings.Index(src, "type "+typeName+" struct {")
	if start < 0 {
		t.Fatalf("no type %s in %s", typeName, file)
	}
	end := strings.Index(src[start:], "\n}")
	body := src[start : start+end]

	var out []string
	for _, line := range strings.Split(body, "\n") {
		i := strings.Index(line, `json:"`)
		if i < 0 {
			continue
		}
		rest := line[i+6:]
		name := rest[:strings.IndexAny(rest, `",`)]
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	return out
}
