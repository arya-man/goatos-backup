package postgres

import "testing"

// The sex scope and the origin scope resolve a whole-shed bucket by ONE rule. The rule is spelled
// out twice only because the weighing isolation guard reads each exempt file on its own; this is
// what keeps the two spellings from drifting apart.
func TestOriginScopeShedTargetsCTEIsTheSexScopeOne(t *testing.T) {
	if scopeShedTargetsCTE != originScopeShedTargetsCTE {
		t.Fatal("originScopeShedTargetsCTE must be byte-identical to scopeShedTargetsCTE; edit both or neither")
	}
}
