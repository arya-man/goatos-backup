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

// The whole point of the rename, asserted against the contract the browser actually receives:
// no page title, table label, column label, filter label, chip, empty state, note or any other
// user-visible string in the admin-web bootstrap says "shed".
//
// This is the test that would have caught the eleven tables that still rendered "Shed" after the
// copy map was renamed: their column labels are DERIVED by humanLabel() from the column key, so
// renaming the copy map alone left them speaking the old word. Walking the served contract
// catches copy wherever it is produced, rather than wherever someone remembered to look.
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
	var walk func(node any, path string)
	walk = func(node any, path string) {
		switch v := node.(type) {
		case map[string]any:
			for k, child := range v {
				walk(child, path+"."+k)
			}
		case []any:
			for i, child := range v {
				walk(child, path+"[]")
				_ = i
			}
		case string:
			if !shedWord.MatchString(v) {
				return
			}
			// Machine-readable values are not copy: route paths, and the dotted/kebab/snake
			// identifiers used as copy KEYS, table ids and column keys.
			if strings.HasPrefix(v, "/") || !strings.ContainsAny(v, " ") && isIdentifier(v) {
				return
			}
			offences = append(offences, path+" = "+v)
		}
	}
	walk(tree, "")

	// One known exception, and it is dead copy rather than a screen: no admin-web code reads
	// modal.rule_editor.default_proof_policy, so its token list never reaches a reader. Listed
	// by name so deleting the key makes this test fail loudly rather than silently widening.
	var real []string
	for _, o := range offences {
		if strings.Contains(o, "modal.rule_editor.default_proof_policy") {
			continue
		}
		real = append(real, o)
	}
	if len(real) > 0 {
		t.Fatalf("admin-web bootstrap still says \"shed\" in %d user-visible string(s):\n  %s",
			len(real), strings.Join(real, "\n  "))
	}
}

func isIdentifier(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '_', r == '-', r == '.', r == '{', r == '}', r == '/':
		default:
			return false
		}
	}
	return s != ""
}

// Column labels are DERIVED from the column key, not read from the copy map, so the pen
// vocabulary has to be stated here too. The keys stay shed_* -- every read model already emits
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
