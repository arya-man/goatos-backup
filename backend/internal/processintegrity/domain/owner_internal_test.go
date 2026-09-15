package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOperatorUserIdentityRemainsInternal(t *testing.T) {
	id := "private-user"
	b, err := json.Marshal(Owner{OperatorUserID: &id})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), id) || strings.Contains(string(b), "operator_user") {
		t.Fatalf("internal identity leaked: %s", b)
	}
}
