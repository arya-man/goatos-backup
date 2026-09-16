package policy

import (
	"encoding/json"
	"github.com/jmespath-community/go-jmespath"
	"os/exec"
	"testing"
)

// Grafana 11.3 uses this Go JMESPath implementation on raw ID-token/userinfo JSON.
func TestDeployedRoleExpression(t *testing.T) {
	out, err := exec.Command("python3", "-c", `import runpy,json; print(json.dumps(runpy.run_path('../stg-grafana-sso.py')['ROLE']))`).Output()
	if err != nil {
		t.Fatal(err)
	}
	var expression string
	if err = json.Unmarshal(out, &expression); err != nil {
		t.Fatal(err)
	}
	allowed := []string{"ravi@mesha.sg", "manohark@mesha.sg", "manju@mesha.sg", "aryaman@mesha.sg"}
	for _, email := range allowed {
		for _, verified := range []any{true, false, nil, "true", 1} {
			t.Run(email+"/"+stringMustJSON(verified), func(t *testing.T) {
				got, err := jmespath.Search(expression, map[string]any{"email": email, "email_verified": verified})
				if err != nil {
					t.Fatal(err)
				}
				want := ""
				if verified == true {
					want = "Viewer"
				}
				if got != want {
					t.Fatalf("role=%v want=%s", got, want)
				}
			})
		}
	}
	for _, claims := range []map[string]any{
		{}, {"groups": []any{"Admin"}}, {"email_verified": true},
		{"email": "stranger@mesha.sg", "email_verified": true},
		{"email": "outsider@gmail.com", "email_verified": true},
		{"email": "ravi@mesha.sg.attacker.test", "email_verified": true},
		{"email": "RAVI@mesha.sg", "email_verified": true},
		{"email": "ravi+alias@mesha.sg", "email_verified": true},
		{"email": " ravi@mesha.sg", "email_verified": true},
		{"email": "ravi@mesha.sg", "verified_email": true},
	} {
		got, err := jmespath.Search(expression, claims)
		if err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Fatalf("unexpected access %v => %v", claims, got)
		}
	}
}
func stringMustJSON(value any) string { b, _ := json.Marshal(value); return string(b) }
