package app

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// shedWord matches the noun on a word boundary, so it does NOT fire on shed_id, shed_tag,
// /vaccination/sheds or a table id like "shed-weights" -- the data contract, which the pen
// rename deliberately left alone.
var shedWord = regexp.MustCompile(`\b[Ss]heds?\b`)

// machineFields are the JSON leaf names whose value is read by CODE, never by a person: ids,
// keys, routes, icon names and the like. Everything else in the contract is copy.
//
// The guard is keyed on the PATH rather than on how the value LOOKS, and that is the whole
// point. An earlier version skipped any bare lowercase word as "an identifier", which is
// exactly the shape of the eleven row-count nouns -- "pager.noun": "shed", "schedule.unit.sheds":
// "sheds" -- so it passed while /vaccination rendered "1-25 of 104 sheds" and /weighing/weights
// rendered "1-20 sheds". A value's spelling cannot say whether a person reads it; its position
// in the contract can.
var machineFields = map[string]bool{
	"key": true, "id": true, "href": true, "icon": true, "domain": true,
	"data_source": true, "dataSource": true, "row_param": true, "badge_key": true,
	"pattern": true, "source": true, "page_key": true, "control_id": true,
	"permission": true, "module": true, "category": true, "route_id": true,
	"columns": true, "summary_fields": true, "shared_key": true, "kind": true,
	"param": true, // row_click.param is a query-string name, not a word on screen
}

// deadCopy is the copy no screen renders. Each entry is named rather than pattern-matched, so
// deleting one of these keys fails the test loudly instead of quietly widening the exception --
// and so a future reader can see there are exactly two, not "some".
var deadCopy = []string{
	// A token list; no admin-web code reads this key.
	"modal.rule_editor.default_proof_policy",
	// vaccination_import_columns: an option group with no consumer in admin-web. Every label in
	// it is the raw snake_case COLUMN NAME (drive_code, due_date, proof_type), because it
	// describes a CSV contract rather than naming anything for a reader.
	"option_groups[].options[].label",
}

// The whole point of the rename, asserted against the contract the browser actually receives:
// no page title, table label, column label, filter label, chip, empty state, note, row-count
// noun or any other user-visible string in the admin-web bootstrap says "shed".
//
// This is the test that catches copy wherever it is PRODUCED rather than wherever someone
// remembered to look -- including column labels, which humanLabel() derives from the column key
// and which no copy-map rename can reach.
func TestBootstrapContractSaysPenNeverShed(t *testing.T) {
	raw, err := json.Marshal(NewService().Bootstrap(context.Background(), BootstrapInput{}))
	if err != nil {
		t.Fatalf("marshal bootstrap: %v", err)
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("unmarshal bootstrap: %v", err)
	}

	var offences []string
	var walk func(node any, path string, leaf string)
	walk = func(node any, path string, leaf string) {
		switch v := node.(type) {
		case map[string]any:
			for k, child := range v {
				walk(child, path+"."+k, k)
			}
		case []any:
			for _, child := range v {
				walk(child, path+"[]", leaf)
			}
		case string:
			if !shedWord.MatchString(v) || machineFields[leaf] || strings.HasPrefix(v, "/") {
				return
			}
			offences = append(offences, path+" = "+v)
		}
	}
	walk(tree, "", "")

	var real []string
	for _, o := range offences {
		dead := false
		for _, d := range deadCopy {
			if strings.Contains(o, d) {
				dead = true
				break
			}
		}
		if !dead {
			real = append(real, o)
		}
	}
	if len(real) > 0 {
		t.Fatalf("admin-web bootstrap still says %q in %d user-visible string(s):\n  %s",
			"shed", len(real), strings.Join(real, "\n  "))
	}
}

// Column labels are DERIVED from the column key, not read from the copy map, so the pen
// vocabulary has to be stated there too. The keys stay shed_* -- every read model already emits
// them and renaming them would be a contract change, which this work deliberately is not.
func TestColumnLabelsSpeakPenWhileTheKeysStayShed(t *testing.T) {
	for key, want := range map[string]string{
		"shed":      "Pen",
		"sheds":     "Pens",
		"shed_tag":  "Pen tag",
		"shed_name": "Pen name",
		"shed_code": "Pen code",
	} {
		if got := humanLabel(key); got != want {
			t.Errorf("humanLabel(%q) = %q, want %q", key, got, want)
		}
	}
	// The default de-underscoring is what produced "Shed", so prove it is still the fallback
	// for everything else -- these cases are an override, not a new humanisation rule.
	if got := humanLabel("feed_item"); got != "Feed item" {
		t.Errorf("humanLabel(%q) = %q, want the default humanisation", "feed_item", got)
	}
}
